package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The test binary doubles as a fake recorder (TABULA_FAKE_RECORDER set):
// "fake CHANNELS RATE MODE" writes raw s32le at real-time pace (MODE: ok,
// overrun: reports an overrun; fail: cannot open the device; silent: sends
// nothing).
func TestMain(m *testing.M) {
	if os.Getenv("TABULA_FAKE_RECORDER") != "" {
		fakeRecorder(os.Args[len(os.Args)-3:])
		return
	}
	os.Exit(m.Run())
}

func fakeRecorder(args []string) {
	channels, _ := strconv.Atoi(args[0])
	rate, _ := strconv.Atoi(args[1])
	mode := args[2]
	switch mode {
	case "fail":
		fmt.Fprintln(os.Stderr, "arecord: main:850: audio open error: Device or resource busy")
		os.Exit(1)
	case "silent":
		fmt.Fprintln(os.Stderr, "waiting for the device")
		time.Sleep(time.Minute)
	}
	fmt.Fprintf(os.Stderr, "Recording raw data 'stdin' : Signed 32 bit Little Endian, Rate %d Hz, Channels %d\n", rate, channels)
	period := rate / 100
	start := time.Now()
	for n := 0; ; n += period {
		// a period is sent once captured, as a sound card does
		time.Sleep(time.Until(start.Add(time.Duration(n+period) * time.Second / time.Duration(rate))))
		os.Stdout.Write(frames(channels, n, period))
		if mode == "overrun" && n == 10*period {
			fmt.Fprintln(os.Stderr, "overrun!!! (at least 1.500 ms long)")
		}
	}
}

// sampleAt is the test signal: channel and frame number, so a file shows
// which channel and which frame each sample came from.
func sampleAt(ch, n int) int32 { return int32(ch<<24 | (n&0xffff)<<8) }

// frames builds count raw s32le frames of channels channels from frame n.
func frames(channels, n, count int) []byte {
	b := make([]byte, 0, channels*count*4)
	for k := n; k < n+count; k++ {
		for ch := 1; ch <= channels; ch++ {
			b = binary.LittleEndian.AppendUint32(b, uint32(sampleAt(ch, k)))
		}
	}
	return b
}

// checkTrack checks a 24-bit track file: whole, and holding channel ch.
func checkTrack(t *testing.T, path string, ch int, frames int64) {
	t.Helper()
	info, err := ReadWAV(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Frames != frames || info.DataSize != frames*3 || info.Channels != 1 || info.Bits != 24 || info.Rate != 48000 {
		t.Fatalf("%s: %+v, want %d frames", filepath.Base(path), info, frames)
	}
	data, _ := os.ReadFile(path)
	for _, n := range []int64{0, frames / 2, frames - 1} {
		b := data[headerSize+3*n:]
		got := int32(uint32(b[0])<<8 | uint32(b[1])<<16 | uint32(b[2])<<24)
		if want := sampleAt(ch, int(n)); got != want {
			t.Fatalf("%s frame %d: %#x, want %#x", filepath.Base(path), n, got, want)
		}
	}
}

// pipeSource is an in-process recorder: it writes data, then blocks until
// closed (or ends, if end is set).
func pipeSource(data []byte, end bool) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		go func() {
			for len(data) > 0 {
				n := min(len(data), 7777) // chunks cut through frames
				if _, err := pw.Write(data[:n]); err != nil {
					return
				}
				data = data[n:]
			}
			if end {
				pw.Close()
			}
		}()
		return pr, nil
	}
}

func waitFor(t *testing.T, c *Capture, what string, ok func(Status) bool) Status {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		st := c.Status()
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s: %+v", what, st)
		}
	}
}

func TestCapture(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "audio")
	const n = 30011
	cfg := Config{Recorder: "custom", Command: "unused", Channels: 4, Tracks: "1,3-4"}
	ref := time.Now()
	c, err := Start(cfg, dir, Options{Ref: ref, Names: []string{"Kick", "", "Vx Margot"}, Source: pipeSource(frames(4, 0, n), false),
		refresh: 50 * time.Millisecond, freeSpace: func(string) (uint64, error) { return 100e9, nil }})
	if err != nil {
		t.Fatal(err)
	}
	// (This source is faster than real time: its offset means nothing.)
	st := waitFor(t, c, "all frames", func(s Status) bool { return s.Frames == n })
	if st.State != Recording || st.Format != "raw s32le, 4 channels, 48000 Hz" {
		t.Errorf("recording: %+v", st)
	}
	// The headers are refreshed while recording.
	time.Sleep(150 * time.Millisecond)
	if info, err := ReadWAV(filepath.Join(dir, "01 Kick.wav")); err != nil || info.DataSize != n*3 {
		t.Errorf("refreshed: %+v %v", info, err)
	}
	st = c.Stop()
	if st.State != Stopped || st.Error != "" || st.Frames != n || len(st.Tracks) != 3 {
		t.Fatalf("stopped: %+v", st)
	}
	want := []Track{{1, "Kick", "audio/01 Kick.wav"}, {3, "Vx Margot", "audio/03 Vx Margot.wav"}, {4, "", "audio/Track 04.wav"}}
	for i, tr := range st.Tracks {
		if tr != want[i] {
			t.Errorf("track %d: %+v, want %+v", i, tr, want[i])
		}
		checkTrack(t, filepath.Join(filepath.Dir(dir), tr.File), tr.Channel, n)
	}
	if st.Start.IsZero() || st.Bytes != 3*n*3 || st.ByteRate != 3*48000*3 {
		t.Errorf("info: %+v", st)
	}
}

// The stream ends by itself before any audio (a wrong device): the capture
// fails, saying why; there is nothing to start again.
func TestCaptureStreamEnds(t *testing.T) {
	dir := t.TempDir()
	c, err := Start(Config{Recorder: "custom", Command: "x", Channels: 2}, dir, Options{Source: pipeSource(nil, true)})
	if err != nil {
		t.Fatal(err)
	}
	<-c.Done()
	st := c.Status()
	if st.State != Failed || !strings.Contains(st.Error, "the recorder stopped") || st.Frames != 0 {
		t.Errorf("%+v", st)
	}
	if info, err := ReadWAV(filepath.Join(dir, "Track 02.wav")); err != nil || info.Frames != 0 {
		t.Errorf("file: %+v %v", info, err)
	}
}

// A WAV stream is checked against the configuration.
func TestCaptureWAVStream(t *testing.T) {
	dir := t.TempDir()
	data := append(wavStream(4, 48000, 32), frames(4, 0, 500)...)
	c, err := Start(Config{Recorder: "ffmpeg", Channels: 4, Tracks: "2"}, dir, Options{Source: pipeSource(data, false)})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, "frames", func(s Status) bool { return s.Frames == 500 })
	if st := c.Stop(); st.State != Stopped || st.Format != "WAV s32le, 4 channels, 48000 Hz" {
		t.Errorf("%+v", st)
	}
	checkTrack(t, filepath.Join(dir, "Track 02.wav"), 2, 500)

	c, err = Start(Config{Recorder: "ffmpeg", Channels: 32}, t.TempDir(), Options{Source: pipeSource(data, false)})
	if err != nil {
		t.Fatal(err)
	}
	<-c.Done()
	if st := c.Status(); st.State != Failed || !strings.Contains(st.Error, "sends 4 channels, 32 expected") {
		t.Errorf("wrong channels: %+v", st)
	}
}

// failingFile accepts writes until the disk is "full".
type failingFile struct {
	*os.File
	mu   *sync.Mutex
	left *int
}

func (f failingFile) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(p) > *f.left {
		n, _ := f.File.Write(p[:*f.left])
		*f.left = 0
		return n, syscall.ENOSPC
	}
	*f.left -= len(p)
	return f.File.Write(p)
}

// A write fails (disk full): every file ends at the same whole sample.
func TestCaptureWriteError(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	left := 200 << 10
	create := func(path string) (file, error) {
		f, err := os.Create(path)
		return failingFile{f, &mu, &left}, err
	}
	c, err := Start(Config{Recorder: "custom", Command: "x", Channels: 2}, dir, Options{Source: pipeSource(frames(2, 0, 100000), false), create: create})
	if err != nil {
		t.Fatal(err)
	}
	<-c.Done()
	st := c.Status()
	if st.State != Failed || !strings.Contains(st.Error, syscall.ENOSPC.Error()) {
		t.Errorf("%+v", st)
	}
	if st.Frames == 0 || st.Frames >= 100000 {
		t.Fatalf("frames %d", st.Frames)
	}
	checkTrack(t, filepath.Join(dir, "Track 01.wav"), 1, st.Frames)
	checkTrack(t, filepath.Join(dir, "Track 02.wav"), 2, st.Frames)
}

// Low disk space: a warning, then a clean stop before the disk is full.
func TestCaptureDiskFull(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	free := uint64(10 << 30)
	freeSpace := func(string) (uint64, error) {
		mu.Lock()
		defer mu.Unlock()
		return free, nil
	}
	cfg := Config{Recorder: "custom", Command: "x", Channels: 2}
	c, err := Start(cfg, dir, Options{Source: pipeSource(frames(2, 0, 4800), false), freeSpace: freeSpace, refresh: 20 * time.Millisecond, reserve: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	st := waitFor(t, c, "frames", func(s Status) bool { return s.Frames == 4800 })
	if len(st.Warnings) != 0 || st.Remaining < 3600 {
		t.Errorf("plenty of space: %+v", st)
	}
	mu.Lock()
	free = 1<<20 + 10*uint64(cfg.Normalized().ByteRate())*60 // ten minutes
	mu.Unlock()
	st = waitFor(t, c, "a warning", func(s Status) bool { return len(s.Warnings) > 0 })
	if !strings.Contains(st.Warnings[0], "disk almost full: 10 min") || st.Remaining > 601 {
		t.Errorf("low: %+v", st)
	}
	mu.Lock()
	free = 1 << 19
	mu.Unlock()
	<-c.Done()
	st = c.Status()
	if st.State != Failed || !strings.Contains(st.Error, "disk full") {
		t.Errorf("full: %+v", st)
	}
	checkTrack(t, filepath.Join(dir, "Track 01.wav"), 1, 4800)

	// No capture starts on a full disk.
	if _, err := Start(cfg, dir, Options{Source: pipeSource(nil, false), freeSpace: freeSpace, reserve: 1 << 20}); err == nil {
		t.Error("started on a full disk")
	}
}

// A test of the input: levels, no files.
func TestCaptureProbe(t *testing.T) {
	c, err := Start(Config{Recorder: "custom", Command: "x", Channels: 4}, "", Options{Source: pipeSource(frames(4, 0, 4800), false), refresh: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	st := waitFor(t, c, "levels", func(s Status) bool { return len(s.Peaks) == 4 && s.Peaks[3] > 0 })
	c.Stop()
	// channel 4 peaks at 0x04ff_ff00 (frame 0xffff is not reached: 4800 frames)
	if p := st.Peaks[3]; p < float64(sampleAt(4, 4799))/(1<<31)-1e-6 || p > 0.04 {
		t.Errorf("peaks %v", st.Peaks)
	}
	if st.Bytes != 0 || len(st.Tracks) != 4 || st.Tracks[0].File != "" {
		t.Errorf("probe wrote files: %+v", st)
	}
}

func fakeCommand(t *testing.T, mode string) Config {
	t.Setenv("TABULA_FAKE_RECORDER", "1")
	return Config{Recorder: "custom", Command: QuoteArgs([]string{os.Args[0]}) + " fake {channels} {rate} " + mode, Channels: 8, Tracks: "1-2"}
}

// The whole path through a recorder process.
func TestCaptureCommand(t *testing.T) {
	dir := t.TempDir()
	cfg := fakeCommand(t, "overrun")
	ref := time.Now()
	c, err := Start(cfg, dir, Options{Ref: ref, refresh: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	st := waitFor(t, c, "half a second", func(s Status) bool { return s.Seconds >= 0.5 && s.Overruns == 1 })
	// The fake starts within tens of milliseconds, and its samples are timed.
	if st.State != Recording || st.Offset <= 0 || st.Offset > 0.5 || !strings.Contains(st.Warnings[0], "lost samples: 1 overrun(s), 2 ms at least") {
		t.Errorf("recording: %+v", st)
	}
	st = c.Stop()
	if st.State != Stopped || st.Error != "" {
		t.Fatalf("stopped: %+v", st)
	}
	checkTrack(t, filepath.Join(dir, "Track 01.wav"), 1, st.Frames)
	checkTrack(t, filepath.Join(dir, "Track 02.wav"), 2, st.Frames)
	if !strings.Contains(st.Command, " fake 8 48000 overrun") {
		t.Errorf("command %q", st.Command)
	}
}

// Stopped before any sample: said so, with what the recorder said.
func TestCaptureNothing(t *testing.T) {
	c, err := Start(fakeCommand(t, "silent"), "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	st := c.Stop()
	if st.State != Stopped || len(st.Warnings) != 1 || !strings.Contains(st.Warnings[0], "no audio received; the recorder said: waiting for the device") {
		t.Errorf("%+v", st)
	}
}

func TestCaptureCommandFails(t *testing.T) {
	c, err := Start(fakeCommand(t, "fail"), t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	<-c.Done()
	if st := c.Status(); st.State != Failed || !strings.Contains(st.Error, "Device or resource busy") {
		t.Errorf("%+v", st)
	}
	if _, err := Start(Config{Recorder: "custom", Command: "/nonexistent/arecord"}, t.TempDir(), Options{}); err == nil {
		t.Error("a missing recorder started")
	}
}
