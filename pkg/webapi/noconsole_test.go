package webapi

import (
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/osc"
)

// TestRecordWithoutConsole: with the Wi-Fi down at the start, the audio is
// recorded alone; the console joins the same session once it is synced.
func TestRecordWithoutConsole(t *testing.T) {
	_, emuAddr := startEmu(t)
	api := New(quiet)
	api.SetSessionsDir(t.TempDir())
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()

	// nothing to record: no console, no audio
	if code := post(t, ts.URL+"/api/record/start", ``); code != http.StatusConflict {
		t.Errorf("no console, no audio: %d", code)
	}
	cfg := audio.DefaultConfig()
	cfg.Enabled, cfg.Device, cfg.Tracks = true, "hw:CARD=X32,DEV=0", "1-4"
	api.SetAudioConfig(cfg, "")
	api.audio.source = liveSource(0)
	api.RecordAtStart() // tabula -record, the console out of reach
	v := waitRecord(t, ts.URL, "the audio alone", func(v recordView) bool {
		return v.Recording && v.NoConsole && v.Audio != nil && v.Audio.Seconds > 0.1
	})
	if !v.AudioOn || v.Meta == nil || v.Meta.Console != "" {
		t.Errorf("audio alone: %+v", v)
	}
	// disconnecting (nothing connected) does not end it
	post(t, ts.URL+"/api/disconnect", ``)
	if v := getRecord(t, ts.URL); !v.Recording {
		t.Fatal("ended by a disconnect")
	}

	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)
	waitRecord(t, ts.URL, "the console", func(v recordView) bool { return v.Recording && !v.NoConsole })
	other, _ := net.Dial("udp", emuAddr)
	defer other.Close()
	// a change made on the console, another value each time (the same value
	// again changes nothing to push) until tabula follows the console (/xremote)
	for n, deadline := 0, time.Now().Add(10*time.Second); ; n++ {
		want := 0.2 + 0.01*float64(n%20)
		other.Write(osc.NewMessage("/ch/12/mix/fader", osc.Float(float32(want))).Bytes())
		time.Sleep(50 * time.Millisecond)
		var nodes []stateNode
		getJSON(t, ts.URL+"/api/state?prefix=/ch/12/mix", &nodes)
		if got, ok := nodes[0].Values[1].Value.(float64); ok && math.Abs(got-want) < 0.003 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the console's changes do not reach tabula")
		}
	}
	post(t, ts.URL+"/api/record/stop", ``)

	var one sessionView
	getJSON(t, ts.URL+"/api/sessions/"+v.Name, &one)
	m := one.Meta
	if m.Console == "" || m.ConsoleFrom <= 0 || m.Events < 1 || one.Snapshot < 1000 || m.Audio == nil || m.Audio.Frames == 0 ||
		len(one.Markers) != 1 || one.Markers[0].Marker != "console connected" {
		t.Errorf("session: %+v, markers %+v, snapshot %d", m, one.Markers, one.Snapshot)
	}
}
