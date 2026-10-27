package webapi

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/state"
)

// streamSink keeps what a soundcheck plays.
type streamSink struct {
	mu     sync.Mutex
	b      bytes.Buffer
	closed bool
}

func (s *streamSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	return s.b.Write(p)
}

func (s *streamSink) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

func TestVirtualSoundcheck(t *testing.T) {
	emuSrv, emuAddr := startEmu(t)
	api := New(quiet)
	sessions := t.TempDir()
	api.SetSessionsDir(sessions)
	cfg := audio.DefaultConfig()
	cfg.Enabled, cfg.Device, cfg.Tracks = true, "hw:CARD=X32,DEV=0", "1-4"
	api.SetAudioConfig(cfg, "")
	api.audio.source = liveSource(0)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)

	// a session without audio, then one with 4 tracks
	api.mu.Lock()
	api.audio.cfg.Enabled = false
	api.mu.Unlock()
	post(t, ts.URL+"/api/record/start", ``)
	time.Sleep(50 * time.Millisecond)
	silent := getRecord(t, ts.URL).Name
	post(t, ts.URL+"/api/record/stop", ``)
	api.mu.Lock()
	api.audio.cfg.Enabled = true
	api.mu.Unlock()
	time.Sleep(1100 * time.Millisecond) // another second: another session name
	post(t, ts.URL+"/api/record/start", ``)
	waitRecord(t, ts.URL, "audio", func(v recordView) bool { return v.Audio != nil && v.Audio.Seconds > 0.3 })
	rec := getRecord(t, ts.URL)
	// while recording, no soundcheck
	post(t, ts.URL+"/api/arm", `{"armed":true}`)
	body := func(name, extra string) string {
		return `{"session":"` + name + `","from":0,"audio":true` + extra + `}`
	}
	if code := post(t, ts.URL+"/api/replay/start", body(silent, "")); code != http.StatusConflict {
		t.Errorf("while recording: %d", code)
	}
	post(t, ts.URL+"/api/record/stop", ``)
	name := rec.Name
	var one sessionView
	getJSON(t, ts.URL+"/api/sessions/"+name, &one)
	info := one.Meta.Audio
	if info == nil || len(info.Tracks) != 4 || info.Frames == 0 {
		t.Fatalf("session audio: %+v", info)
	}

	for what, c := range map[string]struct {
		body string
		want int
	}{
		"no tracks":  {body(silent, ""), http.StatusConflict},
		"at 2×":      {body(name, `,"speed":2`), http.StatusBadRequest},
		"no session": {body("nope", ""), http.StatusNotFound},
	} {
		if code := post(t, ts.URL+"/api/replay/start", c.body); code != c.want {
			t.Errorf("%s: %d, want %d", what, code, c.want)
		}
	}

	// the inputs go to the card for the time of it, then back to the stage
	routing := func() int32 {
		v, _, _ := emuSrv.Store().Get(routswitch)
		return v.Int
	}
	var mu sync.Mutex
	var switched []string
	emuSrv.Store().Subscribe(func(ch []state.Change) {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range ch {
			if c.Leaf.Addr == routswitch {
				switched = append(switched, fmt.Sprint(c.Value.Int))
			}
		}
	})
	sink := &streamSink{}
	api.mu.Lock()
	api.sc.sink = func() (io.WriteCloser, error) { return sink, nil }
	api.mu.Unlock()
	if code := post(t, ts.URL+"/api/replay/start", body(name, `,"routing":true`)); code != http.StatusAccepted {
		t.Fatalf("soundcheck: %d", code)
	}
	sawAudio := false
	waitRecord(t, ts.URL, "the end", func(v recordView) bool {
		sawAudio = sawAudio || v.ReplayAudio
		return !v.Replaying
	})
	for deadline := time.Now().Add(time.Second); routing() != 0 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	mu.Lock()
	sw := strings.Join(switched, " ")
	mu.Unlock()
	if v := getRecord(t, ts.URL); v.Error != "" || !sawAudio || sw != "1 0" {
		t.Errorf("after: %+v, audio seen %v, routing switched to %q", v, sawAudio, sw)
	}
	// from 0, before the first sample: silence first, then the tracks on their channels
	sink.mu.Lock()
	raw := sink.b.Bytes()
	sink.mu.Unlock()
	silence := int64(-info.AudioTime(0) * 48000)
	if got, want := int64(len(raw)), (silence+info.Frames)*32*4; got != want {
		t.Fatalf("played %d bytes, want %d (%d frames of silence, %d of audio)", got, want, silence, info.Frames)
	}
	frame := func(i int64) string {
		var out []string
		for ch := range 6 {
			out = append(out, fmt.Sprint(int32(binary.LittleEndian.Uint32(raw[(i*32+int64(ch))*4:]))>>24))
		}
		return strings.Join(out, " ")
	}
	if f := frame(silence + 10); f != "1 2 3 4 0 0" {
		t.Errorf("a frame: %s", f)
	}
	if silence > 0 && frame(0) != "0 0 0 0 0 0" {
		t.Errorf("silence first: %s", frame(0))
	}

	// stopped while it plays: the tracks stop, the routing goes back
	r, w := io.Pipe() // never read: the player's buffer is full
	defer r.Close()
	api.mu.Lock()
	api.sc.sink = func() (io.WriteCloser, error) { return w, nil }
	api.mu.Unlock()
	if code := post(t, ts.URL+"/api/replay/start", body(name, `,"routing":true`)); code != http.StatusAccepted {
		t.Fatalf("soundcheck again: %d", code)
	}
	waitRecord(t, ts.URL, "the tracks", func(v recordView) bool { return v.ReplayAudio })
	if routing() != 1 { // the replay did not put the recorded REC back
		t.Errorf("routing while playing: %d", routing())
	}
	post(t, ts.URL+"/api/replay/stop", ``)
	waitRecord(t, ts.URL, "the stop", func(v recordView) bool { return !v.Replaying && !v.ReplayAudio })
	for deadline := time.Now().Add(time.Second); routing() != 0 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	if v := getRecord(t, ts.URL); v.Error != "" || routing() != 0 {
		t.Errorf("after the stop: %q, routing %d", v.Error, routing())
	}

	// a player that cannot play ends the soundcheck, saying why
	api.mu.Lock()
	api.sc.sink = nil
	api.audio.cfg.PlayCommand = `sh -c 'echo "aplay: main:850: audio open error: Device or resource busy" >&2; exit 1'`
	api.mu.Unlock()
	if code := post(t, ts.URL+"/api/replay/start", body(name, "")); code != http.StatusAccepted {
		t.Fatalf("soundcheck, failing player: %d", code)
	}
	v := waitRecord(t, ts.URL, "the failure", func(v recordView) bool { return !v.Replaying })
	if !strings.Contains(v.Error, "Device or resource busy") {
		t.Errorf("failing player: %q", v.Error)
	}
}
