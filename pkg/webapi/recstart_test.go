package webapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
)

// TestSlowAudioStart: while a recording starts its audio (32 files, the
// recorder launched: slow on a slow disk), tabula keeps answering.
func TestSlowAudioStart(t *testing.T) {
	_, emuAddr := startEmu(t)
	api := New(quiet)
	api.SetSessionsDir(t.TempDir())
	cfg := audio.DefaultConfig()
	cfg.Enabled, cfg.Device, cfg.Tracks = true, "hw:CARD=X32,DEV=0", "1-4"
	api.SetAudioConfig(cfg, "")
	release := make(chan struct{})
	live := liveSource(0)
	api.audio.source = func() (io.ReadCloser, error) {
		<-release // the recorder takes its time to start
		return live()
	}
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)

	started := make(chan int, 1)
	go func() { started <- post(t, ts.URL+"/api/record/start", ``) }()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		api.mu.Lock()
		starting := api.recStarting != nil
		api.mu.Unlock()
		if starting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the recording does not start")
		}
	}
	begin := time.Now()
	var st statusView
	getJSON(t, ts.URL+"/api/status", &st)
	if rv := getRecord(t, ts.URL); time.Since(begin) > time.Second || rv.Recording {
		t.Errorf("answers while the audio starts: %v, recording %v", time.Since(begin), rv.Recording)
	}
	if code := post(t, ts.URL+"/api/record/start", ``); code != http.StatusConflict {
		t.Errorf("a second start: %d", code)
	}
	close(release)
	if code := <-started; code != http.StatusOK {
		t.Fatalf("start: %d", code)
	}
	waitRecord(t, ts.URL, "the audio", func(v recordView) bool { return v.Recording && v.Audio != nil && v.Audio.Seconds > 0.1 })
	post(t, ts.URL+"/api/record/stop", ``)
}
