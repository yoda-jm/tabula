package webapi

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/session"
)

// liveSource is an in-process recorder: 32 channels of s32le at real-time
// pace (channel number in the high byte), until closed; it ends by itself
// after stopAfter frames if set.
func liveSource(stopAfter int) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		go func() {
			start := time.Now()
			for n := 0; stopAfter == 0 || n < stopAfter; n += 480 {
				time.Sleep(time.Until(start.Add(time.Duration(n+480) * time.Second / 48000)))
				b := make([]byte, 0, 480*32*4)
				for k := 0; k < 480; k++ {
					for ch := 1; ch <= 32; ch++ {
						b = binary.LittleEndian.AppendUint32(b, uint32(ch<<24))
					}
				}
				if _, err := pw.Write(b); err != nil {
					return
				}
			}
			pw.Close()
		}()
		return pr, nil
	}
}

func getRecord(t *testing.T, url string) recordView {
	t.Helper()
	var v recordView
	getJSON(t, url+"/api/record", &v)
	return v
}

func waitRecord(t *testing.T, url, what string, ok func(recordView) bool) recordView {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		v := getRecord(t, url)
		if ok(v) {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s: %+v (audio %+v)", what, v, v.Audio)
		}
	}
}

func TestAudioRecording(t *testing.T) {
	_, emuAddr := startEmu(t)
	api := New(quiet)
	sessions := t.TempDir()
	api.SetSessionsDir(sessions)
	api.SetAudioConfig(audio.DefaultConfig(), filepath.Join(sessions, "audio.json"))
	api.audio.source = liveSource(0)
	h := api.Handler(http.NotFoundHandler())
	ts := httptest.NewServer(h)
	defer ts.Close()

	if code := post(t, ts.URL+"/api/audio/config", `{"enabled":true,"recorder":"arecord","device":"hw:CARD=X32,DEV=0","tracks":"1-16"}`); code != http.StatusOK {
		t.Fatalf("config: %d", code)
	}
	if code := post(t, ts.URL+"/api/audio/config", `{"tracks":"1-40"}`); code != http.StatusBadRequest {
		t.Errorf("bad config: %d", code)
	}
	var av audioView
	getJSON(t, ts.URL+"/api/audio", &av)
	if !av.Config.Enabled || av.ByteRate != 16*48000*3 || !strings.HasPrefix(av.Command, "arecord -D hw:CARD=X32,DEV=0 -c 32") ||
		av.Free <= 0 || av.Remaining <= 0 || av.Templates["parecord"] == "" {
		t.Errorf("audio: %+v", av)
	}
	if saved, err := audio.LoadConfig(filepath.Join(sessions, "audio.json")); err != nil || saved.Tracks != "1-16" {
		t.Errorf("saved config: %+v %v", saved, err)
	}

	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)
	if code := post(t, ts.URL+"/api/record/start", `{"note":"set 1"}`); code != http.StatusOK {
		t.Fatalf("record start: %d", code)
	}
	v := waitRecord(t, ts.URL, "audio", func(v recordView) bool { return v.Audio != nil && v.Audio.Seconds > 0.3 })
	if v.Audio.State != audio.Recording || v.Audio.Command != "" || v.Audio.Device != "" || len(v.Audio.Tracks) != 16 {
		t.Errorf("record status: %+v", v.Audio)
	}
	// Busy input: no test while recording.
	if code := post(t, ts.URL+"/api/audio/test", ``); code != http.StatusConflict {
		t.Errorf("test while recording: %d", code)
	}
	post(t, ts.URL+"/api/record/mark", `{"label":"Song 2"}`)
	time.Sleep(100 * time.Millisecond)
	if code := post(t, ts.URL+"/api/record/stop", ``); code != http.StatusOK {
		t.Fatalf("stop: %d", code)
	}

	var list []sessionView
	getJSON(t, ts.URL+"/api/sessions", &list)
	if len(list) != 1 || list[0].Meta.Audio == nil {
		t.Fatalf("sessions: %+v", list)
	}
	name, a := list[0].Name, list[0].Meta.Audio
	if a.Frames < 0.3*48000 || a.Offset <= 0 || a.Offset > 1 || a.Error != "" || a.Tracks[0].File != "audio/01 Hang.wav" ||
		a.Tracks[11].File != "audio/12 Vx Remy.wav" || a.Tracks[10].File != "audio/Track 11.wav" {
		t.Errorf("audio meta: %+v", a)
	}
	dir := filepath.Join(sessions, name)
	for _, tr := range a.Tracks {
		info, err := audio.ReadWAV(filepath.Join(dir, tr.File))
		if err != nil || info.Frames != a.Frames || info.Bits != 24 {
			t.Errorf("%s: %+v %v", tr.File, info, err)
		}
	}
	for _, f := range []string{name + ".rpp", "labels.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("export in the session: %v", err)
		}
	}

	resp, err := http.Get(ts.URL + "/api/sessions/" + name + "/export?format=reaper")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `FILE "audio/01 Hang.wav"`) ||
		resp.Header.Get("Content-Disposition") != `attachment; filename="`+name+`.rpp"` {
		t.Errorf("reaper export: %d %s %s", resp.StatusCode, resp.Header, body)
	}
	resp, _ = http.Get(ts.URL + "/api/sessions/" + name + "/export?format=audacity")
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.HasSuffix(string(body), "\tSong 2\n") {
		t.Errorf("labels: %q", body)
	}
	resp, _ = http.Get(ts.URL + "/api/sessions/" + name + "/export?format=csv")
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.HasPrefix(string(body), "time,audio_time,clock,type") || !strings.Contains(string(body), ",marker,") {
		t.Errorf("csv: %q", body)
	}
	resp, _ = http.Get(ts.URL + "/api/sessions/" + name + "/export?format=midi")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown export: %d", resp.StatusCode)
	}

	// A test of the input: levels of every channel, nothing written.
	testFor = 1200 * time.Millisecond
	resp, err = http.Post(ts.URL+"/api/audio/test", "application/json", strings.NewReader(`{"enabled":true,"tracks":"1-4"}`))
	if err != nil {
		t.Fatal(err)
	}
	var st audio.Status
	decode(t, resp, &st)
	if len(st.Peaks) != 4 || st.Peaks[3] < 0.03 || st.Peaks[3] > 0.04 || st.Tracks[0].Name != "Hang" {
		t.Errorf("test: %+v", st)
	}

	// Recording, audio and exports are the engineer's.
	api.SetGuestRole(RoleViewer)
	for _, u := range []string{"/api/audio", "/api/audio/devices", "/api/sessions/" + name + "/export?format=csv"} {
		if w := remote(h, "GET", u, "", ""); w.Code != http.StatusForbidden {
			t.Errorf("viewer %s: %d", u, w.Code)
		}
	}
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", resp.StatusCode, data)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

// An audio capture that fails leaves the recording of the changes going.
func TestAudioFailure(t *testing.T) {
	_, emuAddr := startEmu(t)
	api := New(quiet)
	sessions := t.TempDir()
	api.SetSessionsDir(sessions)
	cfg := audio.DefaultConfig()
	cfg.Enabled = true
	api.SetAudioConfig(cfg, "")
	api.audio.source = func() (io.ReadCloser, error) { return nil, errors.New("device busy") }
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)

	if code := post(t, ts.URL+"/api/record/start", ``); code != http.StatusOK {
		t.Fatalf("record start: %d", code)
	}
	v := getRecord(t, ts.URL)
	if !v.Recording || v.Audio == nil || v.Audio.State != audio.Failed || !strings.Contains(v.Audio.Error, "device busy") {
		t.Errorf("start failed: %+v %+v", v, v.Audio)
	}
	post(t, ts.URL+"/api/record/stop", ``)

	// The recorder dies in the middle of the show.
	api.audio.source = liveSource(9600)
	time.Sleep(time.Second) // sessions are named by the second
	if code := post(t, ts.URL+"/api/record/start", ``); code != http.StatusOK {
		t.Fatalf("record start: %d", code)
	}
	v = waitRecord(t, ts.URL, "failure", func(v recordView) bool { return v.Audio != nil && v.Audio.State == audio.Failed })
	if !v.Recording || !strings.Contains(v.Audio.Error, "the recorder stopped") {
		t.Errorf("mid-show: %+v %+v", v, v.Audio)
	}
	post(t, ts.URL+"/api/record/mark", `{"label":"after"}`)
	post(t, ts.URL+"/api/record/stop", ``)
	var list []sessionView
	getJSON(t, ts.URL+"/api/sessions", &list)
	if len(list) != 2 || list[0].Meta.Markers != 1 || list[0].Meta.Audio == nil || list[0].Meta.Audio.Frames != 9600 ||
		!strings.Contains(list[0].Meta.Audio.Error, "the recorder stopped") || !strings.Contains(list[1].Meta.Audio.Error, "device busy") {
		t.Errorf("sessions: %+v", list)
	}
	ses, err := session.Open(filepath.Join(sessions, list[0].Name))
	if err != nil || len(ses.Markers()) != 1 {
		t.Errorf("session: %v", err)
	}
}
