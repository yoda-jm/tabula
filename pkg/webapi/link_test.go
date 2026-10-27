package webapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/state"
)

// logBuffer is a log the test reads while the server writes it.
type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestLink(t *testing.T) {
	defer func(d time.Duration) { linkReportEvery = d }(linkReportEvery)
	linkReportEvery = 20 * time.Millisecond
	_, emuAddr := startEmu(t)
	var log logBuffer
	api := New(slog.New(slog.NewTextHandler(&log, nil)))
	api.SetSessionsDir(t.TempDir())
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)

	// the read-back measures the link: the status tells it
	var st statusView
	for deadline := time.Now().Add(5 * time.Second); st.Console == nil || st.Console.Link == nil; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("no link quality: %+v", st.Console)
		}
		getJSON(t, ts.URL+"/api/status", &st)
	}
	if l := st.Console.Link; l.Asked == 0 || l.Lost != 0 {
		t.Errorf("link: %+v", l)
	}

	// a value the read-back found changed is logged
	api.mu.Lock()
	c, mirror := api.client, api.mirror
	api.mu.Unlock()
	api.publishChanges([]state.Change{{Leaf: mirror.Schema().Leaf("/ch/03/mix/fader"), Value: osc.Float(0.5), Origin: console.OriginReadback}})
	if out := log.String(); !strings.Contains(out, "a change lost on the network") || !strings.Contains(out, "addr=/ch/03/mix/fader") {
		t.Errorf("log:\n%s", out)
	}

	// the session being recorded tells when the console went and came back
	if code := post(t, ts.URL+"/api/record/start", ``); code != http.StatusOK {
		t.Fatalf("record: %d", code)
	}
	api.markLink(c)
	c.Close()
	for deadline := time.Now().Add(2 * time.Second); c.Status().Connected && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	api.markLink(c)
	var rv recordView
	getJSON(t, ts.URL+"/api/record", &rv)
	post(t, ts.URL+"/api/record/stop", ``)
	var one sessionView
	getJSON(t, ts.URL+"/api/sessions/"+rv.Name, &one)
	if len(one.Markers) != 2 || one.Markers[0].Marker != "console back" || one.Markers[1].Marker != "console lost" {
		t.Errorf("markers: %+v", one.Markers)
	}
}
