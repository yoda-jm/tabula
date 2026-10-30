package audio

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// sources hands out one stream per start of the recorder: nil for a start
// that fails (the card not back yet).
type sources struct {
	mu      sync.Mutex
	streams []func() (io.ReadCloser, error)
	events  []string
}

func (s *sources) next() (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.streams) == 0 {
		return nil, errors.New("no more streams")
	}
	f := s.streams[0]
	s.streams = s.streams[1:]
	if f == nil {
		return nil, errors.New("audio open error: No such device")
	}
	return f()
}

func (s *sources) notify(ev string) {
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
}

func shorten(t *testing.T) {
	stall, silence, wait := stallAfter, silenceAfter, restartWait
	stallAfter, silenceAfter, restartWait = 200*time.Millisecond, 200*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { stallAfter, silenceAfter, restartWait = stall, silence, wait })
}

// trackSamples reads the samples of a 24-bit track.
func trackSamples(t *testing.T, path string) []int32 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []int32
	for b := data[headerSize:]; len(b) >= 3; b = b[3:] {
		out = append(out, int32(uint32(b[0])<<8|uint32(b[1])<<16|uint32(b[2])<<24))
	}
	return out
}

// TestCaptureRecovers: the recorder stops mid-recording (the card gone);
// it is started again until it delivers, in the same files, silence
// standing for the gap; the session is told when the audio went and came
// back.
func TestCaptureRecovers(t *testing.T) {
	shorten(t)
	src := &sources{streams: []func() (io.ReadCloser, error){
		pipeSource(frames(2, 0, 1000), true),  // then the stream ends
		nil,                                   // the card is not back yet
		pipeSource(frames(2, 0, 1000), false), // back
	}}
	dir := t.TempDir()
	c, err := Start(Config{Recorder: "custom", Command: "x", Channels: 2}, dir, Options{Source: src.next, Notify: src.notify})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, "the audio back", func(st Status) bool { return st.State == Recording && len(st.Gaps) == 1 && st.Frames > 2000 })
	st := c.Stop()
	gap := st.Gaps[0]
	pad := int64(gap.Seconds*48000 + 0.5)
	if st.State != Stopped || st.Error != "" || gap.At != 1000.0/48000 || pad <= 0 || st.Frames != 2000+pad ||
		!strings.Contains(gap.Why, "the recorder stopped") {
		t.Fatalf("after: %+v, gap %+v", st, gap)
	}
	samples := trackSamples(t, filepath.Join(dir, "Track 02.wav"))
	if int64(len(samples)) != st.Frames || samples[999] != sampleAt(2, 999) || samples[1000] != 0 || samples[1000+pad-1] != 0 ||
		samples[1000+pad] != sampleAt(2, 0) || samples[len(samples)-1] != sampleAt(2, 999) {
		t.Errorf("track: %d samples (pad %d): %#x %#x %#x", len(samples), pad, samples[999], samples[1000], samples[1000+pad])
	}
	src.mu.Lock()
	defer src.mu.Unlock()
	if len(src.events) != 2 || !strings.HasPrefix(src.events[0], "audio lost (the recorder stopped") ||
		!strings.HasPrefix(src.events[1], "audio back after ") {
		t.Errorf("events: %q", src.events)
	}
}

// stalled sends some frames, then nothing, without ending (a recorder
// that hangs when the card goes away).
func stalled(data []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		go pw.Write(data)
		return pr, nil
	}
}

// TestCaptureStalls: a recorder that sends nothing mid-recording is
// stopped and started again.
func TestCaptureStalls(t *testing.T) {
	shorten(t)
	src := &sources{streams: []func() (io.ReadCloser, error){stalled(frames(2, 0, 1000)), stalled(frames(2, 0, 500))}}
	c, err := Start(Config{Recorder: "custom", Command: "x", Channels: 2}, t.TempDir(), Options{Source: src.next, Notify: src.notify})
	if err != nil {
		t.Fatal(err)
	}
	st := waitFor(t, c, "a start again", func(st Status) bool { return len(st.Gaps) == 1 })
	if !strings.Contains(st.Gaps[0].Why, "no audio for 200ms") || st.Gaps[0].Seconds < 0.15 {
		t.Errorf("gap: %+v", st.Gaps)
	}
	c.Stop()
}

// TestCaptureStopWhileRecovering: Stop while the card is not back ends
// the capture at once, its files finished.
func TestCaptureStopWhileRecovering(t *testing.T) {
	shorten(t)
	restartWait = time.Hour
	src := &sources{streams: []func() (io.ReadCloser, error){pipeSource(frames(2, 0, 1000), true)}}
	dir := t.TempDir()
	c, err := Start(Config{Recorder: "custom", Command: "x", Channels: 2}, dir, Options{Source: src.next})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, "recovering", func(st Status) bool { return st.State == Recovering && len(st.Warnings) > 0 })
	start := time.Now()
	st := c.Stop()
	if st.State != Stopped || time.Since(start) > time.Second || st.Frames != 1000 {
		t.Errorf("stopped: %+v after %v", st, time.Since(start))
	}
	checkTrack(t, filepath.Join(dir, "Track 01.wav"), 1, 1000)
}

// TestCaptureSilence: every track silent for a while is told (an alarm),
// and forgotten once a track has signal again.
func TestCaptureSilence(t *testing.T) {
	shorten(t)
	pr, pw := io.Pipe()
	defer pw.Close()
	c, err := Start(Config{Recorder: "custom", Command: "x", Channels: 2}, t.TempDir(),
		Options{Source: func() (io.ReadCloser, error) { return pr, nil }, refresh: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	// feed sends silence, and a signal when asked, until ok holds (3 s at
	// most: a slow machine processes the audio well after it is sent)
	feed := func(signal bool, ok func(Status) bool) Status {
		var st Status
		for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			pw.Write(make([]byte, 480*2*4))
			if signal {
				pw.Write(frames(2, 0, 480))
			}
			if st = c.Status(); ok(st) {
				break
			}
		}
		return st
	}
	alarm := func(st Status) bool {
		return st.Silent >= 0.2 && strings.Contains(strings.Join(st.Warnings, " "), "every track silent")
	}
	if st := feed(false, alarm); !alarm(st) {
		t.Errorf("silent: %+v", st)
	}
	if st := feed(true, func(st Status) bool { return st.Silent == 0 }); st.Silent != 0 {
		t.Errorf("signal again: silent %v", st.Silent)
	}
}
