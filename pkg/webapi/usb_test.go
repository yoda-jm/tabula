package webapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
)

func TestUSBKind(t *testing.T) {
	for name, want := range map[string][2]string{
		"[..]": {"parent", ".."}, "[Songs]": {"dir", "Songs"}, "[System Volume Information]": {"volume", "System Volume Information"},
		"Intro.WAV": {"wav", "Intro.WAV"}, "GVO.scn": {"scene", "GVO.scn"}, "a.snp": {"snippet", "a.snp"},
		"Wedges.mon": {"monitor", "Wedges.mon"}, "Gig.shw": {"show", "Gig.shw"}, "notes.txt": {"file", "notes.txt"},
	} {
		if kind, shown := usbKind(name); kind != want[0] || shown != want[1] {
			t.Errorf("%q: %s %q", name, kind, shown)
		}
	}
}

// copyDir copies the demo stick, so that a test can change it.
func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUSB(t *testing.T) {
	emuSrv, emuAddr := startEmu(t)
	stick := t.TempDir()
	copyDir(t, "../../testdata/usb", stick)
	scn, _ := os.ReadFile(filepath.Join(stick, "Demo.scn"))
	var lines []string
	for _, l := range strings.Split(string(scn), "\n") {
		if strings.HasPrefix(l, "/ch/01/config ") {
			_, rest, _ := strings.Cut(strings.TrimPrefix(l, "/ch/01/config "), `" `)
			l = `/ch/01/config "From USB" ` + rest
		}
		lines = append(lines, l)
	}
	os.WriteFile(filepath.Join(stick, "Demo.scn"), []byte(strings.Join(lines, "\n")), 0o644)
	if _, err := emuSrv.MountUSB(stick); err != nil {
		t.Fatal(err)
	}
	api := New(quiet)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()

	if code := post(t, ts.URL+"/api/usb", `{"op":"stop"}`); code != http.StatusConflict {
		t.Errorf("not connected: %d", code)
	}
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)

	var v usbView
	getJSON(t, ts.URL+"/api/usb", &v)
	want := []usbEntry{{1, "Breaks", "dir"}, {2, "01 Walk-in.wav", "wav"}, {3, "02 Opening.wav", "wav"}, {4, "03 Encore.wav", "wav"}, {5, "Demo.scn", "scene"}}
	if !v.Mounted || v.Path != "" || len(v.Entries) != len(want) {
		t.Fatalf("root: %+v", v)
	}
	for i, e := range want {
		if v.Entries[i] != e {
			t.Errorf("entry %d: %+v, want %+v", i, v.Entries[i], e)
		}
	}

	open := func(index int, wantCode int) usbView {
		t.Helper()
		resp, err := http.Post(ts.URL+"/api/usb", "application/json", strings.NewReader(`{"op":"open","index":`+strconv.Itoa(index)+`}`))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != wantCode {
			resp.Body.Close()
			t.Fatalf("open %d: %d, want %d", index, resp.StatusCode, wantCode)
		}
		var v usbView
		if wantCode == http.StatusOK {
			decode(t, resp, &v)
		} else {
			resp.Body.Close()
		}
		return v
	}
	open(1, http.StatusForbidden) // writes not armed
	post(t, ts.URL+"/api/arm", `{"armed":true}`)
	open(9, http.StatusBadRequest)

	v = open(1, http.StatusOK)
	if v.Path != "Breaks" || len(v.Entries) != 2 || v.Entries[0] != (usbEntry{1, "..", "parent"}) || v.Entries[1].Name != "Interval music.wav" {
		t.Errorf("in Breaks: %+v", v)
	}
	if v = open(1, http.StatusOK); v.Path != "" || len(v.Entries) != 5 {
		t.Errorf("back: %+v", v)
	}

	tape := func(want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		var nodes []stateNode
		for time.Now().Before(deadline) {
			nodes = nil
			getJSON(t, ts.URL+"/api/state?prefix=/-stat/tape", &nodes)
			if len(nodes) == 1 && strings.HasPrefix(nodes[0].Line, "/-stat/tape "+want) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Errorf("tape: %+v, want %s", nodes, want)
	}
	if v = open(3, http.StatusOK); v.Title != "02 Opening" {
		t.Errorf("playing: %+v", v)
	}
	tape(`PLAY "/02 Opening.wav" 0 204`)
	for op, want := range map[string]string{"pause": "PPAUSE", "play": "PLAY"} {
		if code := post(t, ts.URL+"/api/usb", `{"op":"`+op+`"}`); code != http.StatusNoContent {
			t.Errorf("%s: %d", op, code)
		}
		tape(want)
	}
	post(t, ts.URL+"/api/usb", `{"op":"next"}`)
	tape(`PLAY "/03 Encore.wav"`)
	post(t, ts.URL+"/api/usb", `{"op":"ff"}`)
	tape(`FF "/03 Encore.wav"`)
	post(t, ts.URL+"/api/usb", `{"op":"pause"}`)
	tape(`PPAUSE "/03 Encore.wav"`)
	post(t, ts.URL+"/api/usb", `{"op":"stop"}`)
	tape("STOP")
	if code := post(t, ts.URL+"/api/usb", `{"op":"eject"}`); code != http.StatusBadRequest {
		t.Errorf("unknown op: %d", code)
	}

	// A scene on the stick: the console loads it, tabula reads it back.
	open(5, http.StatusOK)
	if v, _, _ := emuSrv.Store().Get("/ch/01/config/name"); v.Str != "From USB" {
		t.Errorf("console: %q", v.Str)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var nodes []stateNode
		getJSON(t, ts.URL+"/api/state?prefix=/ch/01/config", &nodes)
		if len(nodes) == 1 && strings.Contains(nodes[0].Line, `"From USB"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("mirror: %+v", nodes)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The stick pulled out.
	emuSrv.Store().Set("/-stat/usbmounted", osc.Int(0), "test")
	getJSON(t, ts.URL+"/api/usb", &v)
	if v.Mounted || len(v.Entries) != 0 {
		t.Errorf("no stick: %+v", v)
	}
	open(1, http.StatusConflict)
}

// TestUSBBackup records the show on the console's stick too: started and
// stopped with the recording without writes armed, and noted in the
// session; not without a stick, nor over the player or a recording made on
// the console.
func TestUSBBackup(t *testing.T) {
	emuSrv, emuAddr := startEmu(t)
	api := New(quiet)
	api.SetSessionsDir(t.TempDir())
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)
	tape := func() int32 {
		v, _, _ := emuSrv.Store().Get("/-stat/tape/state")
		return v.Int
	}
	waitTape := func(want int32) {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); tape() != want; time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("console recorder %d, want %d", tape(), want)
			}
		}
	}
	record := func(what string) recordView {
		t.Helper()
		if err := api.StartRecording(""); err != nil {
			t.Fatalf("record start (%s): %v", what, err)
		}
		return waitRecord(t, ts.URL, what, func(v recordView) bool { return !v.USB.Starting })
	}

	if code := post(t, ts.URL+"/api/record/usb", `{"on":true}`); code != http.StatusOK {
		t.Fatalf("turn on: %d", code)
	}
	if v := record("no stick"); !strings.Contains(v.USB.Error, "no USB stick") || !v.USB.On {
		t.Errorf("without a stick: %+v", v.USB)
	}
	api.StopRecording()

	stick := t.TempDir()
	copyDir(t, "../../testdata/usb", stick)
	if _, err := emuSrv.MountUSB(stick); err != nil {
		t.Fatal(err)
	}
	v := record("the backup")
	if !regexp.MustCompile(`^/R_\d{8}-\d{6}\.wav$`).MatchString(v.USB.File) || v.USB.Error != "" || v.USB.Already {
		t.Fatalf("backup: %+v", v.USB)
	}
	waitTape(tapeRecord)
	if b := getRecord(t, ts.URL).Meta.USBBackup; b == nil || b.File != v.USB.File {
		t.Errorf("session meta: %+v", b)
	}
	if code := post(t, ts.URL+"/api/record/stop", `{}`); code != http.StatusOK {
		t.Fatalf("stop: %d", code)
	}
	waitTape(tapeStop)
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if v, _, _ := api.mirror.Get("/-stat/tape/state"); v.Int == tapeStop {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("tabula still sees the console recording")
		}
	}
	if v := getRecord(t, ts.URL); v.USB.File == "" || !v.USB.On {
		t.Errorf("after the stop: %+v", v.USB)
	}

	// walk-in music from the stick: not interrupted
	post(t, ts.URL+"/api/arm", `{"armed":true}`)
	if code := post(t, ts.URL+"/api/usb", `{"op":"open","index":2}`); code != http.StatusOK {
		t.Fatalf("play: %d", code)
	}
	waitTape(tapePlay)
	if v := record("the player in use"); !strings.Contains(v.USB.Error, "player is in use") {
		t.Errorf("over the player: %+v", v.USB)
	}
	api.StopRecording()
	if tape() != tapePlay {
		t.Errorf("the music stopped: %d", tape())
	}

	// a recording started on the console: left on
	post(t, ts.URL+"/api/usb", `{"op":"stop"}`)
	waitTape(tapeStop)
	api.client.Set("/-stat/tape/state", osc.Int(tapeRecord))
	waitTape(tapeRecord)
	if v := record("the console recording"); !v.USB.Already || v.USB.File == "" {
		t.Errorf("console recording: %+v", v.USB)
	}
	api.StopRecording()
	time.Sleep(100 * time.Millisecond)
	if tape() != tapeRecord {
		t.Errorf("the console's own recording was stopped: %d", tape())
	}

	if code := post(t, ts.URL+"/api/record/usb", `{"on":false}`); code != http.StatusOK || getRecord(t, ts.URL).USB.On {
		t.Errorf("turn off: %d", code)
	}
}
