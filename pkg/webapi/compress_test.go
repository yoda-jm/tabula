package webapi

import (
	"bytes"
	"context"
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

func TestCompressSession(t *testing.T) {
	api := New(quiet)
	api.cmp.poll = 20 * time.Millisecond
	sessions := t.TempDir()
	api.SetSessionsDir(sessions)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	dir := filepath.Join(sessions, "show")
	localSession(t, dir, "/ch/01")
	wav := filepath.Join(dir, "audio", "01 Hang.wav")
	original, err := os.ReadFile(wav)
	if err != nil {
		t.Fatal(err)
	}
	view := func() compressView {
		var v compressView
		getJSON(t, ts.URL+"/api/compress", &v)
		return v
	}
	waitFor := func(what string, ok func(compressView) bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !ok(view()); time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: %+v", what, view())
			}
		}
	}
	if code := post(t, ts.URL+"/api/sessions/nope/compress", ``); code != http.StatusNotFound {
		t.Errorf("no such session: %d", code)
	}

	// a recording starting: it waits
	api.mu.Lock()
	api.recStarting = make(chan struct{})
	api.mu.Unlock()
	if code := post(t, ts.URL+"/api/sessions/show/compress", ``); code != http.StatusAccepted {
		t.Fatalf("compress: %d", code)
	}
	waitFor("paused", func(v compressView) bool { return v.Paused == "recording" && v.Session == "show" })
	if _, err := os.Stat(wav); err != nil {
		t.Fatal("the WAV went while waiting")
	}
	api.mu.Lock()
	api.recStarting = nil
	api.mu.Unlock()
	waitFor("done", func(v compressView) bool { return len(v.Queue) == 0 && v.Session == "" })
	if v := view(); v.Error != "" || v.Saved <= 0 {
		t.Errorf("after: %+v", v)
	}

	// meta.json names the FLAC file, the WAV is gone, the Reaper project follows
	ses, err := session.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if f := ses.Meta.Audio.Tracks[0].File; f != "audio/01 Hang.flac" {
		t.Errorf("meta: %q", f)
	}
	if _, err := os.Stat(wav); !os.IsNotExist(err) {
		t.Error("the WAV is still there")
	}
	if rpp, _ := os.ReadFile(filepath.Join(dir, "show.rpp")); !strings.Contains(string(rpp), "01 Hang.flac") || !strings.Contains(string(rpp), "SOURCE FLAC") {
		t.Errorf("Reaper project: %s", rpp)
	}
	// taken home as recorded: the WAV it was, byte for byte
	files, err := ses.HomeFiles(false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f.Name != "audio/01 Hang.wav" {
			continue
		}
		found = true
		var b bytes.Buffer
		if err := f.WriteTo(context.Background(), &b, nil); err != nil {
			t.Fatal(err)
		}
		// the samples recorded (tabula's own WAV header is 80 bytes, this test's 44)
		if got := b.Bytes(); f.Out != int64(len(got)) || len(got) < 80 || !bytes.Equal(got[80:], original[44:]) {
			t.Errorf("the WAV taken home: %d bytes (said %d), want the samples of the %d recorded", len(got), f.Out, len(original))
		}
	}
	if !found {
		t.Errorf("no WAV taken home: %+v", files)
	}
	// compressed again: nothing to do
	post(t, ts.URL+"/api/sessions/show/compress", ``)
	waitFor("again", func(v compressView) bool { return len(v.Queue) == 0 })

	// the local replay reads it: Hang at half level after 1 s
	if code, err := api.OpenLocal("show", 1.5); err != nil {
		t.Fatalf("local replay of the compressed session: %d %v", code, err)
	}
	defer api.CloseLocal(false)
	l := api.localNow()
	tracks := l.inputs()
	peak := 0.0
	for _, v := range tracks {
		peak = max(peak, v)
	}
	if peak < 0.49 || peak > 0.51 {
		t.Errorf("meters from the FLAC track: %v", tracks)
	}
}

// A track that cannot be compressed (its WAV gone) stays as it is, the
// others are compressed, and the error says which.
func TestCompressSkipsABrokenTrack(t *testing.T) {
	api := New(quiet)
	api.cmp.poll = 20 * time.Millisecond
	sessions := t.TempDir()
	api.SetSessionsDir(sessions)
	dir := filepath.Join(sessions, "show")
	localSession(t, dir, "/ch/01")
	writeMonoWAV(t, filepath.Join(dir, "audio", "02 Cajon.wav"), 96000, 0, 0.25)
	session.UpdateMeta(dir, func(m *session.Meta) {
		m.Audio.Tracks = append(m.Audio.Tracks[:1],
			audio.Track{Channel: 3, Name: "Gone", File: "audio/03 Gone.wav"},
			audio.Track{Channel: 2, Name: "Cajon", File: "audio/02 Cajon.wav"})
	})
	api.CompressSession("show")
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if v := api.compressView(); len(v.Queue) == 0 {
			if !strings.Contains(v.Error, "03 Gone.wav") {
				t.Errorf("error: %q", v.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not done: %+v", api.compressView())
		}
	}
	ses, err := session.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, tr := range ses.Meta.Audio.Tracks {
		files = append(files, tr.File)
	}
	if strings.Join(files, "|") != "audio/01 Hang.flac|audio/03 Gone.wav|audio/02 Cajon.flac" {
		t.Errorf("tracks: %q", files)
	}
}
