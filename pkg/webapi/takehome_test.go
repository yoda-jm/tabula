package webapi

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/session"
	"github.com/yoda-jm/tabula/pkg/state"
)

// wav24 is a mono 24-bit WAV file of frames samples.
func wav24(frames int) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	b.WriteString("RIFF")
	binary.Write(&b, le, uint32(36+frames*3))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(48000), uint32(48000 * 3), uint16(3), uint16(24)} {
		binary.Write(&b, le, v)
	}
	b.WriteString("data")
	binary.Write(&b, le, uint32(frames*3))
	for i := range frames {
		b.Write([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
	}
	return b.Bytes()
}

const showName = "2026-10-25_203512"

// writeShow writes a recorded session of two tracks into the sessions
// directory.
func writeShow(t *testing.T, sessions string) {
	t.Helper()
	dir := filepath.Join(sessions, showName)
	os.MkdirAll(filepath.Join(dir, "audio"), 0o755)
	m := session.Meta{Format: 1, Start: time.Date(2026, 10, 25, 20, 35, 12, 0, time.UTC), Duration: 0.1,
		Audio: &audio.Info{Rate: 48000, Bits: 24, Channels: 32, Frames: 48000,
			Tracks: []audio.Track{{Channel: 1, Name: "Kick", File: "audio/01 Kick.wav"}, {Channel: 2, Name: "Snare", File: "audio/02 Snare.wav"}}}}
	data, _ := json.Marshal(m)
	for name, content := range map[string][]byte{
		"meta.json":          data,
		"events.jsonl":       []byte(`{"t":0.05,"marker":"Song 1"}` + "\n"),
		"start.scn":          []byte("#4.0# \"tabula\"\n/ch/01/config \"Kick\" 1 RD 1\n"),
		"audio/01 Kick.wav":  wav24(48000),
		"audio/02 Snare.wav": wav24(48000),
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(dir, "audio", "01 Kick.wav"), old, old)
}

func homeServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	api := New(quiet)
	sessions := t.TempDir()
	api.SetSessionsDir(sessions)
	writeShow(t, sessions)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	t.Cleanup(ts.Close)
	return api, ts.URL, sessions
}

// recordShow makes the show the session being recorded until the test ends.
func recordShow(t *testing.T, api *Server, sessions string) {
	t.Helper()
	rec, err := session.Start(filepath.Join(sessions, "2026-10-27_210000"), state.New(schema.Build("M32C")), "M32C", "")
	if err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.rec = rec
	api.mu.Unlock()
	t.Cleanup(func() {
		api.mu.Lock()
		api.rec = nil
		api.mu.Unlock()
		rec.Stop()
	})
}

func TestZipSession(t *testing.T) {
	api, url, sessions := homeServer(t)
	for _, flac := range []bool{false, true} {
		if flac && !audio.FLACAvailable() {
			t.Log("no ffmpeg: FLAC not tried")
			continue
		}
		q, folder := "", showName
		if flac {
			q, folder = "?flac=1", showName+" FLAC"
		}
		resp, err := http.Get(url + "/api/sessions/" + showName + "/zip" + q)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Disposition") != `attachment; filename="`+folder+`.zip"` {
			t.Fatalf("zip: %d %v %s", resp.StatusCode, resp.Header, body)
		}
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, f := range zr.File {
			got = append(got, f.Name)
			b := readZipped(t, f)
			switch {
			case strings.HasSuffix(f.Name, ".wav"):
				if f.Method != zip.Store || !bytes.Equal(b, wav24(48000)) {
					t.Errorf("%s: method %d, %d bytes", f.Name, f.Method, len(b))
				}
			case strings.HasSuffix(f.Name, ".flac"):
				if f.Method != zip.Store || !bytes.HasPrefix(b, []byte("fLaC")) || len(b) > 48000*3/2 {
					t.Errorf("%s: method %d, %d bytes", f.Name, f.Method, len(b))
				}
			case strings.HasSuffix(f.Name, "/meta.json"):
				if f.Method != zip.Deflate || !strings.Contains(string(b), `"rate":48000`) {
					t.Errorf("%s: %d %s", f.Name, f.Method, b)
				}
			}
		}
		ext := ".wav"
		if flac {
			ext = ".flac"
			got = slices.DeleteFunc(got, func(n string) bool { return strings.HasSuffix(n, ".rpp") }) // made for the FLAC files
		}
		want := []string{folder + "/events.jsonl", folder + "/meta.json", folder + "/start.scn", folder + "/audio/01 Kick" + ext, folder + "/audio/02 Snare" + ext}
		if !slices.Equal(got, want) {
			t.Errorf("zip holds %q, want %q", got, want)
		}
	}

	var detail sessionView
	getJSON(t, url+"/api/sessions/"+showName, &detail)
	if detail.Size < 2*48000*3 || detail.FLACSize == 0 || detail.FLACSize >= detail.Size {
		t.Errorf("sizes: %d, FLAC %d", detail.Size, detail.FLACSize)
	}
	if code := getCode(t, url+"/api/sessions/nope/zip"); code != http.StatusNotFound {
		t.Errorf("unknown session: %d", code)
	}
	recordShow(t, api, sessions)
	if code := getCode(t, url+"/api/sessions/2026-10-27_210000/zip"); code != http.StatusConflict {
		t.Errorf("session being recorded: %d", code)
	}
}

func readZipped(t *testing.T, f *zip.File) []byte {
	t.Helper()
	r, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("%s: %v", f.Name, err)
	}
	return b
}

func getCode(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func postJSON(t *testing.T, url, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func waitCopy(t *testing.T, url string) copyView {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var v *copyView
		getJSON(t, url+"/api/copy", &v)
		if v != nil && v.State != copyRunning {
			return *v
		}
		if time.Now().After(deadline) {
			t.Fatalf("copy not over: %+v", v)
		}
	}
}

func TestCopySession(t *testing.T) {
	api, url, sessions := homeServer(t)
	disk := t.TempDir()
	free := uint64(1000)
	api.home.disks = func() []diskView { return []diskView{{Path: disk, Name: "BAND", Writable: true}} }
	api.home.space = func(string) (uint64, uint64, error) { return free, 64 << 30, nil }
	copyURL := url + "/api/sessions/" + showName + "/copy"

	var disks struct {
		Disks []diskView `json:"disks"`
		FLAC  bool       `json:"flac"`
	}
	getJSON(t, url+"/api/disks", &disks)
	if len(disks.Disks) != 1 || disks.Disks[0].Free != 1000 || disks.Disks[0].Size != 64<<30 || disks.FLAC != audio.FLACAvailable() {
		t.Errorf("disks: %+v", disks)
	}
	if code, body := postJSON(t, copyURL, `{"disk":"/elsewhere"}`); code != http.StatusBadRequest {
		t.Errorf("a disk not plugged in: %d %s", code, body)
	}
	// no room: refused before anything is written
	if code, body := postJSON(t, copyURL, `{"disk":`+quote(disk)+`}`); code != http.StatusInsufficientStorage ||
		!strings.Contains(body, "not enough room on BAND") || !strings.Contains(body, "the disk has 1 kB free") {
		t.Errorf("no room: %d %s", code, body)
	}
	if entries, _ := os.ReadDir(disk); len(entries) != 0 {
		t.Errorf("written on a disk without room: %v", entries)
	}

	free = 1 << 30
	leftover := filepath.Join(disk, "tabula", showName+copyingSuffix)
	os.MkdirAll(leftover, 0o755)
	os.WriteFile(filepath.Join(leftover, "half.wav"), []byte("x"), 0o644)
	if code, body := postJSON(t, copyURL, `{"disk":`+quote(disk)+`}`); code != http.StatusAccepted {
		t.Fatalf("copy: %d %s", code, body)
	}
	v := waitCopy(t, url)
	dest := filepath.Join(disk, "tabula", showName)
	if v.State != copyDone || v.Folder != dest || v.Files != 5 || v.Of != 5 || v.Done != v.Total || v.Error != "" {
		t.Errorf("copy: %+v", v)
	}
	got, err := os.ReadFile(filepath.Join(dest, "audio", "01 Kick.wav"))
	if err != nil || !bytes.Equal(got, wav24(48000)) {
		t.Errorf("copied track: %v, %d bytes", err, len(got))
	}
	if fi, err := os.Stat(filepath.Join(dest, "audio", "01 Kick.wav")); err != nil || !fi.ModTime().Equal(time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)) {
		t.Errorf("date of the copied track: %v %v", fi.ModTime(), err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("the copying folder is left: %v", err)
	}
	if _, err := session.Open(dest); err != nil {
		t.Errorf("the copy is not a session: %v", err)
	}
	if code, body := postJSON(t, copyURL, `{"disk":`+quote(disk)+`}`); code != http.StatusConflict || !strings.Contains(body, "already on BAND") {
		t.Errorf("copied again: %d %s", code, body)
	}

	if audio.FLACAvailable() {
		if code, body := postJSON(t, copyURL, `{"disk":`+quote(disk)+`,"flac":true}`); code != http.StatusAccepted {
			t.Fatalf("FLAC copy: %d %s", code, body)
		}
		if v := waitCopy(t, url); v.State != copyDone || !v.FLAC {
			t.Errorf("FLAC copy: %+v", v)
		}
		b, err := os.ReadFile(filepath.Join(disk, "tabula", showName+" FLAC", "audio", "02 Snare.flac"))
		if err != nil || !bytes.HasPrefix(b, []byte("fLaC")) {
			t.Errorf("FLAC track: %v", err)
		}
	}

	// cancelled during a track: nothing left on the disk
	os.RemoveAll(dest)
	started, release := make(chan struct{}), make(chan struct{})
	api.home.beforeFile = func(name string) {
		if name == "audio/01 Kick.wav" {
			close(started)
			<-release
		}
	}
	if code, body := postJSON(t, copyURL, `{"disk":`+quote(disk)+`}`); code != http.StatusAccepted {
		t.Fatalf("copy: %d %s", code, body)
	}
	<-started
	if code, body := postJSON(t, copyURL, `{"disk":`+quote(disk)+`}`); code != http.StatusConflict || !strings.Contains(body, "one copy at a time") {
		t.Errorf("a second copy: %d %s", code, body)
	}
	if code, _ := postJSON(t, url+"/api/copy/stop", ``); code != http.StatusOK {
		t.Errorf("stop: %d", code)
	}
	close(release)
	if v := waitCopy(t, url); v.State != copyCancelled || v.Files != 3 {
		t.Errorf("cancelled copy: %+v", v)
	}
	for _, p := range []string{dest, leftover} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s left after a cancel: %v", p, err)
		}
	}
	api.home.beforeFile = nil

	recordShow(t, api, sessions)
	if code, body := postJSON(t, url+"/api/sessions/2026-10-27_210000/copy", `{"disk":`+quote(disk)+`}`); code != http.StatusConflict {
		t.Errorf("session being recorded: %d %s", code, body)
	}
	// another session during a recording: copied, and said
	if code, body := postJSON(t, copyURL, `{"disk":`+quote(disk)+`}`); code != http.StatusAccepted || !strings.Contains(body, `"recording":true`) {
		t.Errorf("copy during a recording: %d %s", code, body)
	}
	waitCopy(t, url)
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestUnescapeMount(t *testing.T) {
	if got := unescapeMount(`/media/yoda/BAND\040DISK`); got != "/media/yoda/BAND DISK" {
		t.Errorf("%q", got)
	}
	if got := unescapeMount(`/media/x\\`); got != `/media/x\\` {
		t.Errorf("%q", got)
	}
	if !within("/media/a/sessions", "/media/a") || within("/media/ab", "/media/a") || !within("/media/a", "/media/a") {
		t.Error("within")
	}
}
