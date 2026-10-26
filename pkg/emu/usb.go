package emu

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/scene"
)

// The USB stick, emulated by a folder (-usb). As in Maillot's X32USB tool:
// /-stat/usbmounted tells whether a stick is there, /-usb/dir/maxpos is
// the number of entries of the current directory, /-usb/dir/001… their
// names ("[..]" for the parent, "[name]" for directories), and
// /-action/recselect n "selects and executes" entry n: enters a directory,
// plays a WAV file, loads a scene or a snippet. /-stat/tape/state stops (0),
// pauses (1) and resumes (2) the player; /-action/playtrack -1 and 1 play
// the previous and next WAV file. No sound comes out, and nothing is
// recorded to the stick.

// Tape states, as /-stat/tape/state numbers them.
const (
	tapeStop  = 0
	tapePause = 1
	tapePlay  = 2
)

// usbMaxEntries is the most entries a directory listing holds.
const usbMaxEntries = 999

type usbEntry struct {
	name string // as the console lists it: "[..]", "[Songs]", "Intro.wav"
	dir  bool
}

type usbStick struct {
	mu   sync.Mutex
	tick time.Duration // one second of playback

	root    string // the stick's folder; "" when there is none
	mounted bool
	dir     string // current directory, slash-separated from the root ("" at the root)
	entries []usbEntry
	listed  int // entries written to the store, to clear the ones no longer there

	file  string // the file loaded in the player, slash-separated from the root
	dur   int32  // its duration in seconds
	etime int32
	state int32
}

// MountUSB plugs in a USB stick: the folder dir stands for it. It returns
// the number of entries at the root.
func (s *Server) MountUSB(dir string) (int, error) {
	fi, err := os.Stat(dir)
	if err != nil {
		return 0, err
	}
	if !fi.IsDir() {
		return 0, fmt.Errorf("%s is not a directory", dir)
	}
	u := &s.usb
	u.mu.Lock()
	defer u.mu.Unlock()
	u.root = dir
	s.mountLocked()
	return len(u.entries), nil
}

func (s *Server) mountLocked() {
	u := &s.usb
	u.mounted = true
	u.dir = ""
	s.setInt("/-stat/usbmounted", 1)
	s.listLocked(0)
}

func (s *Server) unmountLocked() {
	u := &s.usb
	u.mounted = false
	u.dir, u.entries = "", nil
	u.file, u.dur, u.etime, u.state = "", 0, 0, tapeStop
	s.setInt("/-stat/usbmounted", 0)
	s.setStr("/-usb/path", "")
	s.setStr("/-usb/title", "")
	s.setInt("/-usb/dir/dirpos", 0)
	s.writeListLocked()
	s.tapeLocked()
}

func (s *Server) setStr(addr, v string) { s.store.Set(addr, osc.String(v), originConsole) }

// listLocked reads the current directory into the listing; dirpos is the
// selected entry.
func (s *Server) listLocked(dirpos int) {
	u := &s.usb
	u.entries = nil
	if u.dir != "" {
		u.entries = append(u.entries, usbEntry{name: "[..]", dir: true})
	}
	des, err := os.ReadDir(filepath.Join(u.root, filepath.FromSlash(u.dir)))
	if err != nil {
		s.log.Warn("usb stick", "dir", u.dir, "err", err)
	}
	var dirs, files []usbEntry
	for _, de := range des {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if isDir(filepath.Join(u.root, filepath.FromSlash(u.dir), name), de) {
			dirs = append(dirs, usbEntry{name: "[" + name + "]", dir: true})
		} else {
			files = append(files, usbEntry{name: name})
		}
	}
	for _, list := range [][]usbEntry{dirs, files} {
		sort.Slice(list, func(i, j int) bool {
			a, b := strings.ToLower(list[i].name), strings.ToLower(list[j].name)
			if a != b {
				return a < b
			}
			return list[i].name < list[j].name
		})
		u.entries = append(u.entries, list...)
	}
	if len(u.entries) > usbMaxEntries {
		u.entries = u.entries[:usbMaxEntries]
	}
	name := "" // the directory's own name, as the console shows it
	if u.dir != "" {
		name = path.Base(u.dir)
	}
	s.setStr("/-usb/path", name)
	s.setInt("/-usb/dir/dirpos", int32(dirpos))
	s.writeListLocked()
}

// isDir follows symbolic links, as a folder of links stands for a stick too.
func isDir(full string, de os.DirEntry) bool {
	if de.IsDir() {
		return true
	}
	if de.Type()&os.ModeSymlink == 0 {
		return false
	}
	fi, err := os.Stat(full)
	return err == nil && fi.IsDir()
}

func (s *Server) writeListLocked() {
	u := &s.usb
	s.setInt("/-usb/dir/maxpos", int32(len(u.entries)))
	for i, e := range u.entries {
		s.setStr(fmt.Sprintf("/-usb/dir/%03d/name", i+1), e.name)
	}
	for i := len(u.entries); i < u.listed; i++ {
		s.setStr(fmt.Sprintf("/-usb/dir/%03d/name", i+1), "")
	}
	u.listed = len(u.entries)
}

// tapeLocked writes the player's state to /-stat/tape.
func (s *Server) tapeLocked() {
	u := &s.usb
	file := ""
	if u.file != "" {
		file = "/" + u.file
	}
	s.setInt("/-stat/tape/state", u.state)
	s.setStr("/-stat/tape/file", file)
	s.setInt("/-stat/tape/etime", u.etime)
	s.setInt("/-stat/tape/rtime", max(u.dur-u.etime, 0))
}

// usbControl runs a client's set of /-stat/usbmounted or /-stat/tape/state.
func (s *Server) usbControl(addr string, v osc.Arg, origin string) {
	u := &s.usb
	u.mu.Lock()
	defer u.mu.Unlock()
	reject := func(reason, detail string) {
		s.Unhandled.Record(osc.NewMessage(addr, v), strings.TrimPrefix(origin, "udp "), reason, detail)
	}
	switch addr {
	case "/-stat/usbmounted":
		switch {
		case v.Int == 0 && u.mounted:
			s.unmountLocked()
		case v.Int == 1 && !u.mounted && u.root != "":
			s.mountLocked()
		case v.Int == 1 && u.root == "":
			reject(ReasonRejected, "no USB stick (start tabula-emu with -usb dir)")
			s.setInt("/-stat/usbmounted", 0)
		}
	case "/-stat/tape/state":
		switch {
		case v.Int == tapeStop:
			u.state, u.etime = tapeStop, 0
		case !u.mounted:
			reject(ReasonRejected, "no USB stick in the console")
		case v.Int == tapePause:
			if u.state == tapePlay {
				u.state = tapePause
			}
		case v.Int == tapePlay:
			if u.file != "" {
				u.state = tapePlay // resumes, or plays again from the start
			}
		default:
			reject(ReasonUnimplemented, "recording to the USB stick, fast forward and rewind are not emulated")
		}
		s.tapeLocked() // what the player does, whatever was asked
	}
}

// recselect runs /-action/recselect n: entry n (from 1) of the current
// directory.
func (s *Server) recselect(n int, origin string) {
	u := &s.usb
	u.mu.Lock()
	defer u.mu.Unlock()
	defer s.setInt("/-action/recselect", 0)
	reject := func(reason, detail string) {
		s.Unhandled.Record(osc.NewMessage("/-action/recselect", osc.Int(int32(n))), strings.TrimPrefix(origin, "udp "), reason, detail)
	}
	if !u.mounted {
		reject(ReasonRejected, "no USB stick in the console")
		return
	}
	if n < 1 || n > len(u.entries) {
		reject(ReasonRejected, fmt.Sprintf("no entry %d in the current directory (1 to %d)", n, len(u.entries)))
		return
	}
	e := u.entries[n-1]
	switch {
	case e.name == "[..]":
		u.dir = path.Dir("/" + u.dir)[1:]
		s.listLocked(0)
		return
	case e.dir:
		u.dir = path.Join(u.dir, strings.TrimSuffix(strings.TrimPrefix(e.name, "["), "]"))
		s.listLocked(0)
		return
	}
	rel := path.Join(u.dir, e.name)
	s.setInt("/-usb/dir/dirpos", int32(n))
	s.setStr("/-usb/title", strings.TrimSuffix(e.name, path.Ext(e.name)))
	switch ext := strings.ToLower(path.Ext(e.name)); ext {
	case ".wav":
		s.playLocked(rel)
	case ".scn", ".snp":
		f, err := os.Open(filepath.Join(u.root, filepath.FromSlash(rel)))
		if err != nil {
			reject(ReasonRejected, err.Error())
			return
		}
		defer f.Close()
		_, errs := scene.Load(f, s.store, origin)
		s.log.Info("usb stick: loaded", "file", rel, "problems", len(errs))
	default:
		if ext == "" {
			ext = "these"
		}
		reject(ReasonUnimplemented, fmt.Sprintf("loading %s files from the USB stick is not emulated", ext))
	}
}

// playLocked starts playing a WAV file of the stick from its beginning.
func (s *Server) playLocked(rel string) {
	u := &s.usb
	dur, err := wavSeconds(filepath.Join(u.root, filepath.FromSlash(rel)))
	if err != nil {
		s.log.Warn("usb stick: not a WAV file the player reads", "file", rel, "err", err)
	}
	u.file, u.dur, u.etime, u.state = rel, dur, 0, tapePlay
	s.setStr("/-usb/title", strings.TrimSuffix(path.Base(rel), path.Ext(rel)))
	s.tapeLocked()
}

// playtrack runs /-action/playtrack: -1 the previous WAV file of the
// player's directory, 1 the next. Nothing changes at either end; with no
// file in the player, 1 plays the first WAV file of the current directory.
func (s *Server) playtrack(step int) {
	u := &s.usb
	u.mu.Lock()
	defer u.mu.Unlock()
	defer s.setInt("/-action/playtrack", 0)
	if !u.mounted || step == 0 {
		return
	}
	if next := s.siblingLocked(step); next != "" {
		s.playLocked(next)
	}
}

// siblingLocked is the WAV file step files away from the one in the
// player, in its directory, or "".
func (s *Server) siblingLocked(step int) string {
	u := &s.usb
	dir, cur := u.dir, ""
	if u.file != "" {
		dir, cur = path.Dir("/" + u.file)[1:], path.Base(u.file)
	}
	des, _ := os.ReadDir(filepath.Join(u.root, filepath.FromSlash(dir)))
	var wavs []string
	for _, de := range des {
		if n := de.Name(); !de.IsDir() && !strings.HasPrefix(n, ".") && strings.EqualFold(path.Ext(n), ".wav") {
			wavs = append(wavs, n)
		}
	}
	sort.Slice(wavs, func(i, j int) bool { return strings.ToLower(wavs[i]) < strings.ToLower(wavs[j]) })
	i := -1
	for k, w := range wavs {
		if w == cur {
			i = k
		}
	}
	switch {
	case cur == "" && step > 0 && len(wavs) > 0:
		return path.Join(dir, wavs[0])
	case i < 0 || i+step < 0 || i+step >= len(wavs):
		return ""
	}
	return path.Join(dir, wavs[i+step])
}

// runUSB advances the player every second until ctx is done.
func (s *Server) runUSB(ctx context.Context) {
	t := time.NewTicker(s.usb.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tickUSB()
		}
	}
}

func (s *Server) tickUSB() {
	u := &s.usb
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.mounted || u.state != tapePlay {
		return
	}
	u.etime++
	if u.etime < u.dur {
		s.setInt("/-stat/tape/etime", u.etime)
		s.setInt("/-stat/tape/rtime", u.dur-u.etime)
		return
	}
	// End of the file: the next one in folder mode (autoplay), else stop.
	if s.intAt("/config/tape/autoplay") == 1 {
		if next := s.siblingLocked(1); next != "" {
			s.playLocked(next)
			return
		}
	}
	u.state, u.etime = tapeStop, 0
	s.tapeLocked()
}

// wavSeconds is the duration of a WAV file, rounded up to the second, from
// its header: the size of the data chunk over the byte rate.
func wavSeconds(file string) (int32, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var riff [12]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return 0, err
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return 0, fmt.Errorf("not a RIFF/WAVE file")
	}
	byteRate := uint32(0)
	for {
		var h [8]byte
		if _, err := io.ReadFull(f, h[:]); err != nil {
			return 0, fmt.Errorf("no data chunk")
		}
		id, size := string(h[0:4]), binary.LittleEndian.Uint32(h[4:8])
		switch id {
		case "fmt ":
			var fmtc [16]byte
			if size < 16 {
				return 0, fmt.Errorf("short fmt chunk")
			}
			if _, err := io.ReadFull(f, fmtc[:]); err != nil {
				return 0, err
			}
			byteRate = binary.LittleEndian.Uint32(fmtc[8:12])
			size -= 16
		case "data":
			if byteRate == 0 {
				return 0, fmt.Errorf("no format before the data")
			}
			pos, _ := f.Seek(0, io.SeekCurrent)
			if fi, err := f.Stat(); err == nil && int64(size) > fi.Size()-pos {
				size = uint32(fi.Size() - pos) // a recording cut short
			}
			return int32(math.Ceil(float64(size) / float64(byteRate))), nil
		}
		if _, err := f.Seek(int64(size+size&1), io.SeekCurrent); err != nil {
			return 0, err
		}
	}
}
