package emu

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

// writeWAV writes a silent WAV file of the given length: 100 one-byte
// samples a second, so the files stay small.
func writeWAV(t *testing.T, file string, seconds int) {
	t.Helper()
	data := make([]byte, 100*seconds)
	for i := range data {
		data[i] = 0x80
	}
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(data)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)   // PCM
	binary.LittleEndian.PutUint16(h[22:], 1)   // mono
	binary.LittleEndian.PutUint32(h[24:], 100) // rate
	binary.LittleEndian.PutUint32(h[28:], 100) // byte rate
	binary.LittleEndian.PutUint16(h[32:], 1)   // block align
	binary.LittleEndian.PutUint16(h[34:], 8)   // bits
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(data)))
	mkfile(t, file, string(append(h, data...)))
}

func mkfile(t *testing.T, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// startUSB starts an emulator with dir as its USB stick ("" for none) and
// a second of playback lasting 10 ms.
func startUSB(t *testing.T, dir string) (*Server, *peer) {
	t.Helper()
	s := New(Config{Model: "M32C", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, state.New(schema.Build("M32C")))
	s.usb.tick = 10 * time.Millisecond
	if dir != "" {
		if _, err := s.MountUSB(dir); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.Serve(ctx, conn)
	return s, dial(t, conn.LocalAddr().(*net.UDPAddr))
}

func (p *peer) str(addr string) string {
	p.t.Helper()
	return p.ask(addr).Args[0].Str
}

func (p *peer) int(addr string) int32 {
	p.t.Helper()
	return p.ask(addr).Args[0].Int
}

func eventuallyStr(t *testing.T, p *peer, addr, want string) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if p.str(addr) == want {
			return
		}
	}
	t.Errorf("%s never became %q (last %q)", addr, want, p.str(addr))
}

// listing is the names of the current directory, from /-usb/dir.
func listing(p *peer) []string {
	n := p.int("/-usb/dir/maxpos")
	out := make([]string, n)
	for i := range out {
		out[i] = p.str(fmt.Sprintf("/-usb/dir/%03d/name", i+1))
	}
	return out
}

func sameList(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %q, want %q", what, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s: %q, want %q", what, got, want)
			return
		}
	}
}

func unhandled(s *Server, addr, reason string) bool {
	for _, u := range s.Unhandled.List() {
		if u.Address == addr && u.Reason == reason {
			return true
		}
	}
	return false
}

func TestUSBListing(t *testing.T) {
	dir := t.TempDir()
	writeWAV(t, filepath.Join(dir, "b.wav"), 1)
	writeWAV(t, filepath.Join(dir, "A.wav"), 1)
	writeWAV(t, filepath.Join(dir, "song.WAV"), 1)
	mkfile(t, filepath.Join(dir, ".hidden"), "")
	mkfile(t, filepath.Join(dir, "show.shw"), "")
	writeWAV(t, filepath.Join(dir, "Zed", "inner.wav"), 1)
	writeWAV(t, filepath.Join(dir, "Zed", "Deeper", "deep.wav"), 1)
	if err := os.Mkdir(filepath.Join(dir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, p := startUSB(t, dir)

	if p.int("/-stat/usbmounted") != 1 || p.str("/-usb/path") != "" {
		t.Fatalf("not mounted at the root: %v %q", p.ask("/-stat/usbmounted"), p.str("/-usb/path"))
	}
	root := []string{"[alpha]", "[Zed]", "A.wav", "b.wav", "show.shw", "song.WAV"}
	sameList(t, "root", listing(p), root...)

	p.send("/-action/recselect", osc.Int(2)) // [Zed]
	eventuallyStr(t, p, "/-usb/path", "Zed")
	sameList(t, "Zed", listing(p), "[..]", "[Deeper]", "inner.wav")
	if got := p.str("/-usb/dir/004/name"); got != "" {
		t.Errorf("entry 4 left from the root: %q", got)
	}
	if got := p.int("/-action/recselect"); got != 0 {
		t.Errorf("recselect stays %d", got)
	}
	p.send("/-action/recselect", osc.Int(2)) // [Deeper]
	eventuallyStr(t, p, "/-usb/path", "Deeper")
	sameList(t, "Deeper", listing(p), "[..]", "deep.wav")
	p.send("/-action/recselect", osc.Int(1)) // [..]
	eventuallyStr(t, p, "/-usb/path", "Zed")
	p.send("/-action/recselect", osc.Int(1))
	eventuallyStr(t, p, "/-usb/path", "")
	sameList(t, "back to the root", listing(p), root...)
}

func TestUSBPlayer(t *testing.T) {
	dir := t.TempDir()
	writeWAV(t, filepath.Join(dir, "Songs", "Long.wav"), 1000)
	s, p := startUSB(t, dir)
	p.send("/-action/recselect", osc.Int(1)) // [Songs]
	eventuallyStr(t, p, "/-usb/path", "Songs")

	p.send("/-action/recselect", osc.Int(2))
	eventually(t, p, "/-stat/tape/state", tapePlay)
	if f, title := p.str("/-stat/tape/file"), p.str("/-usb/title"); f != "/Songs/Long.wav" || title != "Long" {
		t.Errorf("file %q, title %q", f, title)
	}
	if got := p.int("/-usb/dir/dirpos"); got != 2 {
		t.Errorf("dirpos %d", got)
	}
	for deadline := time.Now().Add(2 * time.Second); p.int("/-stat/tape/etime") < 3 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}

	p.send("/-stat/tape/state", osc.Int(tapePause))
	eventually(t, p, "/-stat/tape/state", tapePause)
	e, r := p.int("/-stat/tape/etime"), p.int("/-stat/tape/rtime")
	if e < 3 || e+r != 1000 {
		t.Errorf("paused at %d s, %d s left", e, r)
	}
	time.Sleep(50 * time.Millisecond)
	if got := p.int("/-stat/tape/etime"); got != e {
		t.Errorf("time runs while paused: %d then %d", e, got)
	}
	p.send("/-stat/tape/state", osc.Int(tapePlay))
	eventually(t, p, "/-stat/tape/state", tapePlay)

	p.send("/-stat/tape/state", osc.Int(tapeStop))
	eventually(t, p, "/-stat/tape/state", tapeStop)
	if e, r := p.int("/-stat/tape/etime"), p.int("/-stat/tape/rtime"); e != 0 || r != 1000 {
		t.Errorf("stopped at %d s, %d s left", e, r)
	}
	if got := p.str("/-stat/tape/file"); got != "/Songs/Long.wav" {
		t.Errorf("the stopped player lost its file: %q", got)
	}
	p.send("/-stat/tape/state", osc.Int(tapePause)) // nothing to pause
	eventually(t, p, "/-stat/tape/state", tapeStop)
	p.send("/-stat/tape/state", osc.Int(tapePlay)) // from the start again
	eventually(t, p, "/-stat/tape/state", tapePlay)

	p.send("/-stat/tape/state", osc.Int(5)) // fast forward
	eventually(t, p, "/-stat/tape/state", tapePlay)
	if !unhandled(s, "/-stat/tape/state", ReasonUnimplemented) {
		t.Errorf("fast forward not reported as not emulated: %+v", s.Unhandled.List())
	}
}

// TestUSBRecord records to the stick: the file is named and its time
// counts, nothing is written.
func TestUSBRecord(t *testing.T) {
	dir := t.TempDir()
	writeWAV(t, filepath.Join(dir, "Intro.wav"), 10)
	s, p := startUSB(t, dir)

	p.send("/-stat/tape/state", osc.Int(tapeRecord))
	eventually(t, p, "/-stat/tape/state", tapeRecord)
	if f := p.str("/-stat/tape/file"); !regexp.MustCompile(`^/R_\d{8}-\d{6}\.wav$`).MatchString(f) {
		t.Errorf("recorded file %q", f)
	}
	if r := p.int("/-stat/tape/rtime"); r != -1 {
		t.Errorf("rtime while recording %d, want -1 (no dropout)", r)
	}
	for deadline := time.Now().Add(2 * time.Second); p.int("/-stat/tape/etime") < 3 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if e := p.int("/-stat/tape/etime"); e < 3 {
		t.Errorf("recording time %d", e)
	}
	p.send("/-action/recselect", osc.Int(1)) // the player waits
	p.int("/-stat/tape/state")               // answered once the recselect is handled
	if !unhandled(s, "/-action/recselect", ReasonRejected) {
		t.Errorf("playing while recording not refused: %+v", s.Unhandled.List())
	}

	p.send("/-stat/tape/state", osc.Int(tapeRecPause))
	eventually(t, p, "/-stat/tape/state", tapeRecPause)
	e := p.int("/-stat/tape/etime")
	time.Sleep(50 * time.Millisecond)
	if got := p.int("/-stat/tape/etime"); got != e {
		t.Errorf("time runs while the recording is paused: %d then %d", e, got)
	}
	p.send("/-stat/tape/state", osc.Int(tapeRecord))
	eventually(t, p, "/-stat/tape/state", tapeRecord)

	p.send("/-stat/tape/state", osc.Int(tapeStop))
	eventually(t, p, "/-stat/tape/state", tapeStop)
	if f := p.str("/-stat/tape/file"); f != "" {
		t.Errorf("player file after the recording: %q", f)
	}
	if des, _ := os.ReadDir(dir); len(des) != 1 {
		t.Errorf("files written to the stick: %v", des)
	}
}

func TestUSBEndOfFile(t *testing.T) {
	dir := t.TempDir()
	writeWAV(t, filepath.Join(dir, "1.wav"), 2)
	writeWAV(t, filepath.Join(dir, "2.wav"), 2)
	_, p := startUSB(t, dir)

	p.send("/-action/recselect", osc.Int(1))
	eventuallyStr(t, p, "/-stat/tape/file", "/1.wav")
	eventually(t, p, "/-stat/tape/state", tapeStop)
	if got := p.str("/-stat/tape/file"); got != "/1.wav" {
		t.Errorf("single mode played on: %q", got)
	}

	p.send("/config/tape/autoplay", osc.Int(1)) // folder mode
	p.send("/-action/recselect", osc.Int(1))
	eventuallyStr(t, p, "/-stat/tape/file", "/2.wav")
	eventually(t, p, "/-stat/tape/state", tapeStop) // no third file
	if got := p.str("/-stat/tape/file"); got != "/2.wav" {
		t.Errorf("after the last file: %q", got)
	}
}

func TestUSBPlaytrack(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.wav", "b.wav", "c.wav"} {
		writeWAV(t, filepath.Join(dir, n), 1000)
	}
	mkfile(t, filepath.Join(dir, "notes.txt"), "")
	_, p := startUSB(t, dir)

	p.send("/-action/playtrack", osc.Int(-1)) // nothing in the player
	time.Sleep(20 * time.Millisecond)
	if got := p.str("/-stat/tape/file"); got != "" {
		t.Errorf("previous with nothing loaded played %q", got)
	}
	for _, step := range []struct {
		n    int32
		want string
	}{{1, "/a.wav"}, {1, "/b.wav"}, {1, "/c.wav"}, {1, "/c.wav"}, {-1, "/b.wav"}, {-1, "/a.wav"}, {-1, "/a.wav"}} {
		p.send("/-action/playtrack", osc.Int(step.n))
		eventuallyStr(t, p, "/-stat/tape/file", step.want)
		eventually(t, p, "/-action/playtrack", 0)
	}
	if got := p.str("/-usb/title"); got != "a" {
		t.Errorf("title %q", got)
	}
}

func TestUSBLoadFiles(t *testing.T) {
	dir := t.TempDir()
	data, err := os.ReadFile("../../testdata/scenes/demo.scn")
	if err != nil {
		t.Fatal(err)
	}
	mkfile(t, filepath.Join(dir, "Demo.scn"), string(data))
	mkfile(t, filepath.Join(dir, "Show.shw"), "")
	s, p := startUSB(t, dir)

	p.send("/-action/recselect", osc.Int(1))
	eventuallyStr(t, p, "/ch/01/config/name", "Hang")
	if got := p.str("/-usb/title"); got != "Demo" {
		t.Errorf("title %q", got)
	}
	if got := p.int("/-stat/tape/state"); got != tapeStop {
		t.Errorf("a scene started the player: %d", got)
	}

	p.send("/-action/recselect", osc.Int(2))
	p.send("/-action/recselect", osc.Int(9))
	eventually(t, p, "/-action/recselect", 0)
	if !unhandled(s, "/-action/recselect", ReasonUnimplemented) || !unhandled(s, "/-action/recselect", ReasonRejected) {
		t.Errorf("show file or missing entry not reported: %+v", s.Unhandled.List())
	}
}

func TestUSBUnmount(t *testing.T) {
	dir := t.TempDir()
	writeWAV(t, filepath.Join(dir, "Songs", "Long.wav"), 1000)
	_, p := startUSB(t, dir)
	p.send("/-action/recselect", osc.Int(1))
	eventuallyStr(t, p, "/-usb/path", "Songs")
	p.send("/-action/recselect", osc.Int(2))
	eventually(t, p, "/-stat/tape/state", tapePlay)

	p.send("/-stat/usbmounted", osc.Int(0))
	eventually(t, p, "/-usb/dir/maxpos", 0)
	if st, path, name, file := p.int("/-stat/tape/state"), p.str("/-usb/path"), p.str("/-usb/dir/001/name"), p.str("/-stat/tape/file"); st != tapeStop || path != "" || name != "" || file != "" {
		t.Errorf("after unmounting: state %d, path %q, entry 1 %q, file %q", st, path, name, file)
	}
	p.send("/-action/recselect", osc.Int(1))
	p.send("/-stat/tape/state", osc.Int(tapePlay))
	eventually(t, p, "/-stat/tape/state", tapeStop)

	p.send("/-stat/usbmounted", osc.Int(1))
	eventually(t, p, "/-usb/dir/maxpos", 1)
	if got := p.str("/-usb/dir/001/name"); got != "[Songs]" {
		t.Errorf("mounted again: %q", got)
	}

	// No stick at all.
	s, q := startUSB(t, "")
	q.send("/-stat/usbmounted", osc.Int(1))
	eventually(t, q, "/-stat/usbmounted", 0)
	if !unhandled(s, "/-stat/usbmounted", ReasonRejected) {
		t.Errorf("mounting no stick not reported")
	}
}

// The player's time is pushed to /xremote clients, as the console does
// every second.
func TestUSBPushesTime(t *testing.T) {
	dir := t.TempDir()
	writeWAV(t, filepath.Join(dir, "Long.wav"), 1000)
	_, p := startUSB(t, dir)
	p.send("/xremote")
	p.send("/-action/recselect", osc.Int(1))
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if m, ok := p.recv(time.Second); ok && m.Address == "/-stat/tape/etime" && m.Args[0].Int >= 2 {
			return
		}
	}
	t.Error("no /-stat/tape/etime pushed")
}

func TestWavSeconds(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.wav")
	writeWAV(t, file, 3)
	if got, err := wavSeconds(file); err != nil || got != 3 {
		t.Errorf("3 s file: %d, %v", got, err)
	}

	// A chunk before the format, odd-sized; the data cut short; 2.5 s rounds up.
	data, _ := os.ReadFile(file)
	odd := append([]byte{}, data[:12]...)
	odd = append(odd, "LIST\x03\x00\x00\x00abc\x00"...)
	odd = append(odd, data[12:]...)
	odd = odd[:len(odd)-50]
	mkfile(t, file, string(odd))
	if got, err := wavSeconds(file); err != nil || got != 3 {
		t.Errorf("odd chunk, cut short: %d, %v", got, err)
	}

	mkfile(t, file, "not a wav file at all")
	if _, err := wavSeconds(file); err == nil {
		t.Error("no error for a text file")
	}
}
