package audio

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// sinkBuffer keeps what a playback streams.
type sinkBuffer struct {
	mu     sync.Mutex
	b      bytes.Buffer
	closed bool
}

func (s *sinkBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	return s.b.Write(p)
}

func (s *sinkBuffer) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

// writeTrack writes a mono track whose sample i is (i+1)·step, full scale ±2³¹.
func writeTrack(t *testing.T, path string, bits, frames int, step int32) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	w, err := createWAV(createFile, path, 48000, bits, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range frames {
		b := make([]byte, w.bytes)
		putSample(b, int32(i+1)*step, w.bytes)
		w.buf = append(w.buf, b...)
	}
	if err := w.finish(int64(frames)); err != nil {
		t.Fatal(err)
	}
}

func playSession(t *testing.T) (string, Info) {
	dir := t.TempDir()
	writeTrack(t, filepath.Join(dir, "audio", "02 Kick.wav"), 24, 1000, 1<<8)
	writeTrack(t, filepath.Join(dir, "audio", "04 Bass.wav"), 16, 500, 1<<16)
	return dir, Info{Rate: 48000, Bits: 24, Channels: 4, Tracks: []Track{
		{Channel: 2, Name: "Kick", File: "audio/02 Kick.wav"},
		{Channel: 3, Name: "not recorded"}, // no file: silence
		{Channel: 4, Name: "Bass", File: "audio/04 Bass.wav"},
	}}
}

func play(t *testing.T, info Info, dir string, from float64) []int32 {
	t.Helper()
	sink := &sinkBuffer{}
	p, err := StartPlayback(DefaultConfig(), dir, info, PlayOptions{From: from, Sink: func() (io.WriteCloser, error) { return sink, nil }})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("playback did not end")
	}
	if p.Err() != nil {
		t.Fatal(p.Err())
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	raw := sink.b.Bytes()
	out := make([]int32, len(raw)/4)
	for i := range out {
		out[i] = int32(binary.LittleEndian.Uint32(raw[4*i:]))
	}
	if got, want := p.Sent(), float64(len(out)/4)/48000; got != want {
		t.Errorf("sent %v s, want %v", got, want)
	}
	return out
}

func TestPlaybackInterleaves(t *testing.T) {
	dir, info := playSession(t)
	// from frame 100: 900 frames, the bass ends after 400
	s := play(t, info, dir, 100.0/48000)
	if len(s) != 900*4 {
		t.Fatalf("%d samples, want %d", len(s), 900*4)
	}
	for k := range 900 {
		frame := s[4*k : 4*k+4]
		kick := int32(101+k) << 8
		bass := int32(0)
		if k < 400 {
			bass = int32(101+k) << 16
		}
		if frame[0] != 0 || frame[1] != kick || frame[2] != 0 || frame[3] != bass {
			t.Fatalf("frame %d: %v, want [0 %d 0 %d]", k, frame, kick, bass)
		}
	}

	// a session time before the audio started: silence first
	s = play(t, info, dir, -10.0/48000)
	if len(s) != 1010*4 || s[4*9+1] != 0 || s[4*10+1] != 1<<8 || s[4*10+3] != 1<<16 {
		t.Fatalf("with silence first: %d samples, %v", len(s), s[4*9:4*11])
	}
	// past the end: nothing to play
	if s := play(t, info, dir, 1); len(s) != 0 {
		t.Errorf("past the end: %d samples", len(s))
	}
}

func TestPlaybackRefuses(t *testing.T) {
	dir, info := playSession(t)
	sink := func() (io.WriteCloser, error) { return &sinkBuffer{}, nil }
	for name, bad := range map[string]Info{
		"no tracks":  {Rate: 48000, Channels: 4},
		"no files":   {Rate: 48000, Channels: 4, Tracks: []Track{{Channel: 1}}},
		"other rate": {Rate: 44100, Channels: 4, Tracks: info.Tracks},
		"missing":    {Rate: 48000, Channels: 4, Tracks: []Track{{Channel: 1, File: "audio/gone.wav"}}},
	} {
		if _, err := StartPlayback(DefaultConfig(), dir, bad, PlayOptions{Sink: sink}); err == nil {
			t.Errorf("%s: played", name)
		}
	}
}

// Stop ends a playback whose player takes no more.
func TestPlaybackStop(t *testing.T) {
	dir, info := playSession(t)
	r, w := io.Pipe() // never read: the stream blocks as on a full buffer
	defer r.Close()
	p, err := StartPlayback(DefaultConfig(), dir, info, PlayOptions{Sink: func() (io.WriteCloser, error) { return w, nil }})
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() {
		p.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return")
	}
	if p.Err() != nil {
		t.Errorf("stopped: %v", p.Err())
	}
}

func TestPlayCommandLine(t *testing.T) {
	for _, c := range []struct {
		cfg  Config
		goos string
		want string
	}{
		{Config{Recorder: "arecord", Device: "hw:CARD=X32,DEV=0"}, "linux",
			"aplay -D hw:CARD=X32,DEV=0 -c 32 -r 48000 -f S32_LE -t raw --buffer-time=200000 --period-time=10000 -"},
		{Config{Recorder: "parecord", Device: "alsa_input.usb-BEHRINGER_X-USB_FF-00.multichannel-input"}, "linux",
			"pacat --playback --raw --format=s32le --rate=48000 --channels=32 --no-remix --no-remap --latency-msec=50 -d alsa_output.usb-BEHRINGER_X-USB_FF-00.multichannel-output"},
		{Config{Recorder: "parecord"}, "linux", "-d @DEFAULT_SINK@"},
		{Config{Recorder: "ffmpeg", Device: "hw:2"}, "linux", "-f s32le -ar 48000 -ac 32 -i - -f alsa hw:2"},
		{Config{Recorder: "arecord", Device: "hw:2", PlayTo: "hw:3"}, "linux", "aplay -D hw:3 "},
		{Config{Recorder: "custom", Command: "rec {device}", PlayCommand: "play -t raw -c {channels} -r {rate} {device}", Device: "x"}, "linux",
			"play -t raw -c 32 -r 48000 x"},
	} {
		args, err := c.cfg.playCommandLine(c.goos, 32, 48000)
		if err != nil || !strings.Contains(QuoteArgs(args), c.want) {
			t.Errorf("%+v: %q %v, want %q", c.cfg, QuoteArgs(args), err, c.want)
		}
	}
	for _, cfg := range []Config{{Recorder: "custom", Command: "rec"}, {Recorder: "ffmpeg"}} {
		if _, err := cfg.playCommandLine("windows", 32, 48000); err == nil {
			t.Errorf("%+v on Windows: a player", cfg)
		}
	}
}

// The player program reads the stream on its standard input.
func TestPlaybackProgram(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir, info := playSession(t)
	out := filepath.Join(t.TempDir(), "played.raw")
	cfg := Config{Recorder: "arecord", PlayTo: out, PlayCommand: `sh -c 'cat > "$0"' {device}`}
	p, err := StartPlayback(cfg, dir, info, PlayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-p.Done()
	if p.Err() != nil || !strings.HasPrefix(p.Command(), "sh -c") {
		t.Fatalf("%v (%s)", p.Err(), p.Command())
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() != 1000*4*4 {
		t.Errorf("played: %v %v", fi, err)
	}

	// a player that fails says why (a long track: the stream outgrows the pipe)
	writeTrack(t, filepath.Join(dir, "audio", "02 Kick.wav"), 24, 200000, 1<<8)
	cfg.PlayCommand = `sh -c 'echo "aplay: main:850: audio open error: Device or resource busy" >&2; exit 1'`
	p, err = StartPlayback(cfg, dir, info, PlayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-p.Done()
	if p.Err() == nil || !strings.Contains(p.Err().Error(), "Device or resource busy") {
		t.Errorf("failing player: %v", p.Err())
	}
}

// A mix plays the chosen tracks in two channels, panned and scaled; the
// tracks left out are not read.
func TestPlaybackMix(t *testing.T) {
	dir, info := playSession(t)
	sink := &sinkBuffer{}
	p, err := StartPlayback(DefaultConfig(), dir, info, PlayOptions{
		Sink: func() (io.WriteCloser, error) { return sink, nil },
		Mix:  []MixTrack{{Channel: 2, Gain: 1, Pan: -1}, {Channel: 4, Gain: 0.5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("playback did not end")
	}
	if p.Err() != nil {
		t.Fatal(p.Err())
	}
	raw := sink.b.Bytes()
	if len(raw) != 1000*8 {
		t.Fatalf("%d bytes, want 1000 stereo frames", len(raw))
	}
	at := func(k, ch int) int32 { return int32(binary.LittleEndian.Uint32(raw[8*k+4*ch:])) }
	// the first block (all 1000 frames here) fades in: frame k at (k+1)/1000
	for _, k := range []int{0, 499, 500, 999} {
		fade := float64(k+1) / 1000
		wantL, wantR := 256*float64(k+1), 0.0
		if k < 500 {
			wantL += 32768 * float64(k+1)
			wantR = 32768 * float64(k+1)
		}
		wantL, wantR = math.Round(wantL*fade), math.Round(wantR*fade)
		if math.Abs(float64(at(k, 0))-wantL) > 1 || math.Abs(float64(at(k, 1))-wantR) > 1 {
			t.Errorf("frame %d: %d %d, want %v %v", k, at(k, 0), at(k, 1), wantL, wantR)
		}
	}
	// only the tracks of the mix are opened
	if _, err := StartPlayback(DefaultConfig(), dir, info, PlayOptions{
		Sink: func() (io.WriteCloser, error) { return &sinkBuffer{}, nil },
		Mix:  []MixTrack{{Channel: 3, Gain: 1}},
	}); err == nil {
		t.Error("a mix of a track without a file: no error")
	}
}

// gateSink takes one block at a time, when the test lets it.
type gateSink struct {
	blocks chan []byte
	next   chan struct{}
}

func (g *gateSink) Write(p []byte) (int, error) {
	g.blocks <- append([]byte(nil), p...)
	<-g.next
	return len(p), nil
}

func (g *gateSink) Close() error { return nil }

func writeConst(t *testing.T, path string, frames int, v int32) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	w, err := createWAV(createFile, path, 48000, 24, 0)
	if err != nil {
		t.Fatal(err)
	}
	for range frames {
		b := make([]byte, w.bytes)
		putSample(b, v, w.bytes)
		w.buf = append(w.buf, b...)
	}
	if err := w.finish(int64(frames)); err != nil {
		t.Fatal(err)
	}
}

// Changing the tracks heard crossfades in the stream (no gap, no jump),
// and stopping fades out.
func TestPlaybackMixCrossfade(t *testing.T) {
	dir := t.TempDir()
	const a, b = 1 << 18, 1 << 22
	writeConst(t, filepath.Join(dir, "audio", "01 A.wav"), 10*playFrames, a<<8)
	writeConst(t, filepath.Join(dir, "audio", "02 B.wav"), 10*playFrames, b<<8)
	info := Info{Rate: 48000, Bits: 24, Channels: 2, Tracks: []Track{
		{Channel: 1, File: "audio/01 A.wav"}, {Channel: 2, File: "audio/02 B.wav"}}}
	g := &gateSink{blocks: make(chan []byte), next: make(chan struct{})}
	p, err := StartPlayback(DefaultConfig(), dir, info, PlayOptions{
		Sink: func() (io.WriteCloser, error) { return g, nil },
		Mix:  []MixTrack{{Channel: 1, Gain: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	left := func(blk []byte, k int) float64 { return float64(int32(binary.LittleEndian.Uint32(blk[8*k:]))) }
	last := func(blk []byte) float64 { return left(blk, len(blk)/8-1) }
	in := <-g.blocks // fades in to A
	if left(in, 0) >= (a<<8)/100 || last(in) != a<<8 {
		t.Errorf("fade in: %v … %v", left(in, 0), last(in))
	}
	g.next <- struct{}{}
	steady := <-g.blocks
	p.SetMix([]MixTrack{{Channel: 2, Gain: 1}})
	g.next <- struct{}{}
	cross := <-g.blocks // from A to B, smoothly
	if left(steady, 0) != a<<8 || math.Abs(left(cross, 0)-a<<8) > (b<<8)/float64(playFrames)*2 || last(cross) != b<<8 {
		t.Errorf("crossfade: %v, then %v … %v", left(steady, 0), left(cross, 0), last(cross))
	}
	stopped := make(chan struct{})
	go func() { p.Stop(); close(stopped) }()
	g.next <- struct{}{}
	// the block in flight may still be B; the last one fades to silence
	var out []byte
	for out = range g.blocks {
		if last(out) == 0 {
			break
		}
		g.next <- struct{}{}
	}
	if last(out) != 0 || left(out, 0) < (b<<8)*0.9 {
		t.Errorf("fade out: %v … %v", left(out, 0), last(out))
	}
	g.next <- struct{}{}
	<-stopped
}
