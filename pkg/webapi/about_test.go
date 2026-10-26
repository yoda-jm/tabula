package webapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestAbout(t *testing.T) {
	dir := t.TempDir()
	api := New(quiet)
	api.SetBuild("v9.9.9", filepath.Join(dir, "tabula.log"))
	api.SetSessionsDir("sessions")
	if err := api.SetAccessFile(filepath.Join(dir, "access.json")); err != nil {
		t.Fatal(err)
	}
	api.SetListenAddr("127.0.0.1:8033")
	h := api.Handler(http.NotFoundHandler())

	w := local(h, "GET", "/api/about", "")
	var a aboutView
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil || w.Code != http.StatusOK {
		t.Fatalf("about: %d %s", w.Code, w.Body)
	}
	sessions, _ := filepath.Abs("sessions")
	if a.Version != "v9.9.9" || a.Log != filepath.Join(dir, "tabula.log") || a.Sessions != sessions ||
		a.Access != filepath.Join(dir, "access.json") || a.Listen != "127.0.0.1:8033" || time.Since(a.Started) > time.Minute {
		t.Errorf("about: %+v", a)
	}
	// the engineer's: it says where the files are
	if w := remote(h, "GET", "/api/about", "", ""); w.Code != http.StatusForbidden {
		t.Errorf("about from another device: %d", w.Code)
	}

	// every page sees the version in the status, to tell when tabula was updated
	var st statusView
	if err := json.Unmarshal(local(h, "GET", "/api/status", "").Body.Bytes(), &st); err != nil || st.Version != "v9.9.9" {
		t.Errorf("status: %+v %v", st, err)
	}
}
