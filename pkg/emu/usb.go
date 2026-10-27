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
	"github.com/yoda-jm/tabula/pkg/schema"
)

// The USB stick, emulated by a folder (-usb). As in Maillot's X32USB tool:
// /-stat/usbmounted tells whether a stick is there, /-usb/dir/maxpos is
// the number of entries of the current directory, /-usb/dir/001… their
// names ("[..]" for the parent, "[name]" for directories), and
// /-action/recselect n "selects and executes" entry n: enters a directory,
// plays a WAV file, loads a scene, a snippet, a preset or a show (see
// loadLocked). /-stat/tape/state stops (0), pauses (1) and resumes (2) the
// player, winds it forward (5) or back (6); /-action/playtrack -1 and 1
// play the previous and next WAV file. No sound comes out.
// /-stat/tape/state 4 records (3 pauses the recording): the file is named
// as the console names it, R_20261026-203512.wav in the current directory,
// and its time counts, but nothing is written to the stick.

// Tape states, as /-stat/tape/state numbers them.
const (
	tapeStop     = 0
	tapePause    = 1
	tapePlay     = 2
	tapeRecPause = 3
	tapeRecord   = 4
	tapeFF       = 5
	tapeRew      = 6
)

// windStep is how far fast forward and rewind move in a second of
// playback. A guess: the document does not say how fast the console winds.
const windStep = 10

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

func (s *Server) strAt(addr string) string {
	v, _, _ := s.store.Get(addr)
	return v.Str
}

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
	s.setInt("/-stat/tape/rtime", s.rtimeLocked())
}

// rtimeLocked is /-stat/tape/rtime: the time left to play, or while
// recording -(dropouts + 1), and no dropout happens here.
func (s *Server) rtimeLocked() int32 {
	u := &s.usb
	if recording(u.state) {
		return -1
	}
	return max(u.dur-u.etime, 0)
}

func recording(state int32) bool { return state == tapeRecord || state == tapeRecPause }

func winding(state int32) bool { return state == tapeFF || state == tapeRew }

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
			if recording(u.state) {
				u.file = "" // the recording is not on the emulated stick: nothing to play
			}
			u.state, u.etime = tapeStop, 0
		case !u.mounted:
			reject(ReasonRejected, "no USB stick in the console")
		case v.Int == tapeRecord && u.state == tapeRecPause:
			u.state = tapeRecord
		case v.Int == tapeRecord && u.state != tapeRecord:
			u.file = path.Join(u.dir, time.Now().Format("R_20060102-150405.wav"))
			u.dur, u.etime, u.state = 0, 0, tapeRecord
		case v.Int == tapeRecPause:
			if u.state == tapeRecord {
				u.state = tapeRecPause
			}
		case recording(u.state):
			// the player waits for the recording to stop
		case v.Int == tapePause:
			if u.state == tapePlay || winding(u.state) {
				u.state = tapePause
			}
		case v.Int == tapePlay:
			if u.file != "" {
				u.state = tapePlay // resumes where it was, or plays again from the start
			}
		case winding(v.Int):
			if u.file != "" {
				u.state = v.Int // from where it is, until play, pause or stop, or an end of the file
			}
		default:
			reject(ReasonRejected, fmt.Sprintf("no tape state %d (0 to 6)", v.Int))
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
	if recording(u.state) {
		reject(ReasonRejected, "the console is recording to the stick")
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
	if strings.EqualFold(path.Ext(e.name), ".wav") {
		s.playLocked(rel)
		return
	}
	if reason, detail := s.loadLocked(rel, origin); reason != "" {
		reject(reason, detail)
	}
}

// loadLocked loads a file of the stick, as the console does on its USB
// screen; it returns why not (a journal reason and its detail) when it
// cannot. Scenes (.scn) and snippets (.snp) change what they hold. The
// preset files are those of the library (as M32-Edit writes them, or the
// emulator's -library files): a channel preset (.chn) goes to the
// selected strip (/-stat/selidx), an effect preset (.efx) to the effect
// slot shown on the Effects screen (/-stat/screen/FX/page, slot 1 on its
// home page): both guesses, as the document does not say where the
// console puts them; routing (.rou) and AES50/DP48 (.mon) presets apply
// as they are. A show (.shw) replaces the show (loadShowLocked).
// Preference files (.prf) are refused: their format is not known.
func (s *Server) loadLocked(rel, origin string) (reason, detail string) {
	u := &s.usb
	file := filepath.Join(u.root, filepath.FromSlash(rel))
	ext := strings.ToLower(path.Ext(rel))
	switch ext {
	case ".scn", ".snp":
		f, err := os.Open(file)
		if err != nil {
			return ReasonRejected, err.Error()
		}
		defer f.Close()
		_, errs := scene.Load(f, s.store, origin)
		s.log.Info("usb stick: loaded", "file", rel, "problems", len(errs))
		return "", ""
	case ".shw":
		return s.loadShowLocked(rel, origin)
	case ".chn", ".efx", ".rou", ".mon":
	case ".prf":
		return ReasonUnimplemented, "loading preference files (.prf) is not emulated: their format is not known"
	case "":
		return ReasonUnimplemented, "loading files without an extension from the USB stick is not emulated"
	default:
		return ReasonUnimplemented, fmt.Sprintf("loading %s files from the USB stick is not emulated", ext)
	}
	e, err := readEntry(file)
	if err != nil {
		return ReasonRejected, err.Error()
	}
	base := ""
	switch ext {
	case ".chn":
		strip, ok := stripPath(int(s.intAt("/-stat/selidx")))
		if !ok {
			return ReasonRejected, "no strip selected"
		}
		base = strip
	case ".efx":
		slot := s.intAt("/-stat/screen/FX/page") // HOME, FX1…FX8
		base = fmt.Sprintf("/fx/%d", max(slot, 1))
		fxRelative(e)
	}
	var lines []string
	for _, l := range e.Lines {
		if s.store.Schema().Node(base+l.Suffix) != nil {
			lines = append(lines, base+l.Suffix+" "+l.Rest)
		}
	}
	to := base
	if to == "" {
		to = "the console"
	}
	if len(lines) == 0 {
		return ReasonRejected, fmt.Sprintf("%s: no line of the file is a parameter of %s", path.Base(rel), to)
	}
	errs := s.store.ApplyLines(lines, origin)
	s.log.Info("usb stick: loaded", "file", rel, "to", to, "lines", len(lines), "skipped", len(e.Lines)-len(lines), "problems", len(errs))
	return "", ""
}

// loadShowLocked loads a show file of the stick: the show (name, scene
// safes), its cues and the listings of its scenes and snippets, as
// M32-Edit writes a show (showfile/show, showfile/cue/000…, the /-show
// prefix left out); the scenes and snippets themselves come from the files
// saved next to it, Show.000.scn, Show.001.snp…, named as M32-Edit names
// them. A scene listed without its file is listed empty. The library
// directory (-library) is not changed: it keeps what was saved there.
func (s *Server) loadShowLocked(rel, origin string) (reason, detail string) {
	u := &s.usb
	dir := filepath.Join(u.root, filepath.FromSlash(path.Dir("/" + rel)[1:]))
	data, err := os.ReadFile(filepath.Join(u.root, filepath.FromSlash(rel)))
	if err != nil {
		return ReasonRejected, err.Error()
	}
	sch := s.store.Schema()
	var lines []string
	skipped := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "showfile/"):
			line = "/-show/" + line
		case strings.HasPrefix(line, "-show/"):
			line = "/" + line
		}
		p, _ := schema.SplitLine(line)
		if !strings.HasPrefix(p, "/-show/showfile/") {
			continue
		}
		if sch.Node(p) == nil {
			skipped++ // cues past the hundredth, as M32-Edit writes 500
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ReasonRejected, fmt.Sprintf("%s: no show in the file", path.Base(rel))
	}
	errs := s.store.ApplyLines(lines, origin)

	// the files of the directory, by lower-case name
	files := map[string]string{}
	if des, err := os.ReadDir(dir); err == nil {
		for _, de := range des {
			files[strings.ToLower(de.Name())] = de.Name()
		}
	}
	stem := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	var missing []string
	lib := s.lib
	lib.mu.Lock()
	for _, kind := range []string{"scene", "snippet"} {
		listing := libKinds[kind].listing
		for i := 0; i < libSlots; i++ {
			p := fmt.Sprintf("%s/%03d", listing, i)
			lib.items[kind][i] = nil
			if s.intAt(p+"/hasdata") != 1 {
				continue
			}
			name := fmt.Sprintf("%s.%03d.%s", stem, i, libKinds[kind].ext)
			var e *entry
			if real, ok := files[strings.ToLower(name)]; ok {
				if read, err := readEntry(filepath.Join(dir, real)); err == nil && len(read.Lines) > 0 {
					e = read
				}
			}
			if e == nil {
				missing = append(missing, name)
				s.store.Set(p+"/hasdata", osc.Int(0), origin)
				continue
			}
			e.Name, e.Note = s.strAt(p+"/name"), ""
			if kind == "scene" {
				e.Note = s.strAt(p + "/notes")
			}
			lib.items[kind][i] = e
		}
	}
	lib.mu.Unlock()
	s.log.Info("usb stick: show loaded", "file", rel, "lines", len(lines), "skipped", skipped, "problems", len(errs), "missing", len(missing))
	if len(missing) > 0 {
		return ReasonRejected, fmt.Sprintf("%s: loaded, but %d scenes or snippets it lists are not on the stick (%s…), listed empty", path.Base(rel), len(missing), missing[0])
	}
	return "", ""
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
	if !u.mounted || step == 0 || recording(u.state) {
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
	if !u.mounted {
		return
	}
	if u.state == tapeRecord {
		u.etime++
		s.setInt("/-stat/tape/etime", u.etime)
		return
	}
	switch u.state {
	case tapePlay:
		u.etime++
	case tapeFF:
		u.etime += windStep
	case tapeRew:
		if u.etime -= windStep; u.etime <= 0 {
			u.state, u.etime = tapeStop, 0 // back at the start
			s.tapeLocked()
			return
		}
	default:
		return
	}
	if u.etime < u.dur {
		s.setInt("/-stat/tape/etime", u.etime)
		s.setInt("/-stat/tape/rtime", u.dur-u.etime)
		return
	}
	// End of the file, played or wound to: the next one in folder mode
	// (autoplay), else stop.
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
