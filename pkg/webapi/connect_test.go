package webapi

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/emu"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

// emuOn starts an emulator on addr.
func emuOn(t *testing.T, addr string) {
	t.Helper()
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", ua)
	if err != nil {
		t.Fatal(err)
	}
	s := emu.New(emu.Config{Model: "M32C", Logger: quiet}, state.New(schema.Build("M32C")))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.Serve(ctx, conn)
}

// TestConnectRetries connects to a console that is not there yet (the
// laptop started first): tabula asks again until it answers.
func TestConnectRetries(t *testing.T) {
	old := dialRetryMax
	dialRetryMax = 200 * time.Millisecond
	t.Cleanup(func() { dialRetryMax = old })
	free, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	addr := free.LocalAddr().String()
	free.Close()

	api := New(quiet)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	if code := post(t, ts.URL+"/api/connect", `{"addr":"`+addr+`"}`); code != http.StatusAccepted {
		t.Fatalf("connect: %d", code)
	}
	var st statusView
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		getJSON(t, ts.URL+"/api/status", &st)
		if strings.Contains(st.Error, "asking again") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no retry said: %+v", st)
		}
	}
	if !st.Connecting || st.Connected {
		t.Errorf("status while asking again: %+v", st)
	}
	emuOn(t, addr) // the console is up
	connected(t, ts.URL)
	var now statusView // a fresh one: an empty error is left out of the JSON
	getJSON(t, ts.URL+"/api/status", &now)
	if now.Error != "" {
		t.Errorf("error left once connected: %q", now.Error)
	}
	// connecting elsewhere replaces a connection being made
	if code := post(t, ts.URL+"/api/connect", `{"addr":"`+addr+`"}`); code != http.StatusAccepted {
		t.Errorf("connect again: %d", code)
	}
	connected(t, ts.URL)
}

// slowClose is a recorder whose end takes a while (files being finished).
type slowClose struct {
	io.ReadCloser
	wait time.Duration
}

func (s slowClose) Close() error {
	time.Sleep(s.wait)
	return s.ReadCloser.Close()
}

// TestDisconnectWhileRecording disconnects during a recording: the answer
// comes at once while the session is finished in the background, and the
// next recording waits for it to free the card.
func TestDisconnectWhileRecording(t *testing.T) {
	_, emuAddr := startEmu(t)
	api := New(quiet)
	sessions := t.TempDir()
	api.SetSessionsDir(sessions)
	api.SetAudioConfig(audio.DefaultConfig(), "")
	live := liveSource(0)
	api.audio.source = func() (io.ReadCloser, error) {
		r, err := live()
		return slowClose{r, 800 * time.Millisecond}, err
	}
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	if code := post(t, ts.URL+"/api/audio/config", `{"enabled":true,"recorder":"arecord","device":"hw:CARD=X32,DEV=0","tracks":"1-2"}`); code != http.StatusOK {
		t.Fatalf("config: %d", code)
	}
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)
	if code := post(t, ts.URL+"/api/record/start", `{}`); code != http.StatusOK {
		t.Fatalf("record start: %d", code)
	}
	waitRecord(t, ts.URL, "audio", func(v recordView) bool { return v.Audio != nil && v.Audio.Seconds > 0.2 })

	start := time.Now()
	if code := post(t, ts.URL+"/api/disconnect", ``); code != http.StatusOK {
		t.Fatalf("disconnect: %d", code)
	}
	if d := time.Since(start); d > 400*time.Millisecond {
		t.Errorf("disconnect took %v: it waited for the recording's files", d)
	}
	if getRecord(t, ts.URL).Recording {
		t.Error("still recording after disconnect")
	}

	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)
	if code := post(t, ts.URL+"/api/record/start", `{}`); code != http.StatusOK {
		t.Fatalf("record start again: %d", code)
	}
	if d := time.Since(start); d < 800*time.Millisecond {
		t.Errorf("the new recording started %v after the disconnect, before the last one was finished", d)
	}
	if err := api.StopRecording(); err != nil {
		t.Fatal(err)
	}
	dirs, _ := os.ReadDir(sessions)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		ses, err := api.openSession(filepath.Join(sessions, d.Name()))
		if err != nil || ses.Meta.Audio == nil || ses.Meta.Audio.Frames == 0 {
			t.Errorf("session %s not finished: %+v %v", d.Name(), ses, err)
		}
	}
}

// TestRecordWaitsForTheInputTest starts a recording while the card is being
// tested: it waits for the test to end rather than find the card busy.
func TestRecordWaitsForTheInputTest(t *testing.T) {
	_, emuAddr := startEmu(t)
	api := New(quiet)
	api.SetSessionsDir(t.TempDir())
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)

	api.mu.Lock()
	api.audio.testing = true
	api.mu.Unlock()
	go func() {
		time.Sleep(300 * time.Millisecond)
		api.mu.Lock()
		api.audio.testing = false
		api.mu.Unlock()
	}()
	start := time.Now()
	if err := api.StartRecording(""); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 300*time.Millisecond {
		t.Errorf("recording started after %v, during the test", d)
	}
	api.StopRecording()

	old := audioFreeWait
	audioFreeWait = 100 * time.Millisecond
	t.Cleanup(func() { audioFreeWait = old })
	api.mu.Lock()
	api.audio.testing = true
	api.mu.Unlock()
	if err := api.StartRecording(""); err == nil || !strings.Contains(err.Error(), "being tested") {
		t.Errorf("recording during a test that does not end: %v", err)
	}
}

// TestSlowBrowserResyncs drops events for a browser that does not keep up,
// then tells it to load the state again before the next ones.
func TestSlowBrowserResyncs(t *testing.T) {
	var h hub
	h.init()
	ch, _ := h.add(nil, caller{}, "192.0.2.1")
	for i := 0; i < cap(ch)+10; i++ {
		h.publish("change", i)
	}
	for len(ch) > 0 {
		<-ch
	}
	h.publish("change", "next")
	if got := string(<-ch); !strings.HasPrefix(got, "event: resync") {
		t.Fatalf("first event after the lost ones: %q", got)
	}
	if got := string(<-ch); !strings.Contains(got, `"next"`) {
		t.Fatalf("then: %q", got)
	}
	h.publish("change", "after")
	if got := string(<-ch); !strings.Contains(got, `"after"`) {
		t.Fatalf("no resync once caught up: %q", got)
	}
}
