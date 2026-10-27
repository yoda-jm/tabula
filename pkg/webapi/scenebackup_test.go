package webapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSceneBackups(t *testing.T) {
	_, emuAddr := startEmu(t)
	dir := filepath.Join(t.TempDir(), "scenes")
	api := New(quiet)
	api.SetScenesDir(dir)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)
	backups := func() []string {
		t.Helper()
		l, err := listBackups(dir)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}

	// not armed: the recall is refused, nothing is saved
	if code := post(t, ts.URL+"/api/set", `{"addr":"/-action/goscene","value":0}`); code != http.StatusForbidden || len(backups()) != 0 {
		t.Fatalf("unarmed recall: %d, %v", code, backups())
	}
	post(t, ts.URL+"/api/arm", `{"armed":true}`)
	if code := post(t, ts.URL+"/api/set", `{"addr":"/-action/goscene","value":0}`); code != http.StatusNoContent {
		t.Fatalf("recall: %d", code)
	}
	l := backups()
	if len(l) != 1 {
		t.Fatalf("after a recall: %v", l)
	}
	b, _ := os.ReadFile(filepath.Join(dir, l[0]))
	if !strings.HasPrefix(string(b), `#4.0# "Backup before recalling scene 00" "saved by tabula `) || !strings.Contains(string(b), "\n/ch/12/config \"Vx Theo\"") {
		t.Errorf("backup:\n%.300s", b)
	}

	// the same settings again: not saved twice; changed ones are
	post(t, ts.URL+"/api/set", `{"addr":"/-action/goscene","value":0}`)
	if l := backups(); len(l) != 1 {
		t.Errorf("same settings: %v", l)
	}
	post(t, ts.URL+"/api/set", `{"addr":"/ch/01/mix/fader","text":"-12"}`)
	post(t, ts.URL+"/api/library", `{"op":"load","kind":"snippet","index":4}`)
	if l := backups(); len(l) != 2 || !strings.Contains(read(t, filepath.Join(dir, l[1])), "Backup before recalling snippet 04") {
		t.Errorf("after a snippet: %v", l)
	}
	// other writes, and library commands that do not load, save nothing
	post(t, ts.URL+"/api/set", `{"addr":"/ch/01/mix/fader","text":"-6"}`)
	post(t, ts.URL+"/api/set", `{"addr":"/-action/initall","value":0}`)
	post(t, ts.URL+"/api/library", `{"op":"rename","kind":"scene","index":0,"name":"x"}`)
	if l := backups(); len(l) != 2 {
		t.Errorf("writes that overwrite nothing: %v", l)
	}

	// a scene file load says where the settings before it went
	resp, err := http.Post(ts.URL+"/api/scenefiles/"+strings.ReplaceAll(l[0], " ", "%20")+"/load", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var p sceneProgress
	decode(t, resp, &p)
	if l := backups(); !p.Done || p.Backup == "" || p.Backup != l[len(l)-1] {
		t.Errorf("scene file load: %+v, backups %v", p, l)
	}

	// the newest keepBackups are kept
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range keepBackups {
		name := "Backup " + old.Add(time.Duration(i)*time.Minute).Format("2006-01-02 15.04.05") + ".scn"
		os.WriteFile(filepath.Join(dir, name), []byte("#4.0# \"x\"\n/ch/01/config\n"), 0o644)
	}
	os.WriteFile(filepath.Join(dir, "Backup of mine.scn"), []byte("#4.0# \"x\"\n/ch/01/config\n"), 0o644)
	post(t, ts.URL+"/api/set", `{"addr":"/ch/01/mix/fader","text":"-20"}`)
	post(t, ts.URL+"/api/set", `{"addr":"/-action/doundo","value":1}`)
	l = backups()
	if len(l) != keepBackups || l[0] != "Backup 2020-01-01 00.04.00.scn" || !strings.Contains(read(t, filepath.Join(dir, l[len(l)-1])), "undoing") {
		t.Errorf("kept: %d, first %s", len(l), l[0])
	}
	if _, err := os.Stat(filepath.Join(dir, "Backup of mine.scn")); err != nil {
		t.Errorf("a file of the user's was deleted: %v", err)
	}
}

func TestSceneBackupFails(t *testing.T) {
	_, emuAddr := startEmu(t)
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, nil, 0o644)
	api := New(quiet)
	api.SetScenesDir(filepath.Join(blocker, "scenes"))
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)
	post(t, ts.URL+"/api/arm", `{"armed":true}`)
	// the recall still goes: a full disk should not keep a scene from loading
	if code := post(t, ts.URL+"/api/set", `{"addr":"/-action/goscene","value":2}`); code != http.StatusNoContent {
		t.Errorf("recall without a backup: %d", code)
	}
	api.mu.Lock()
	msg := api.backupErr
	api.mu.Unlock()
	if !strings.Contains(msg, "the backup before recalling scene 02 failed") {
		t.Errorf("backup error: %q", msg)
	}
	// two in the same second get their own names
	dir := t.TempDir()
	api.SetScenesDir(dir)
	now := time.Now()
	a, err1 := api.saveBackup("a", now)
	post(t, ts.URL+"/api/set", `{"addr":"/ch/02/mix/fader","text":"-30"}`)
	b, err2 := api.saveBackup("b", now)
	if err1 != nil || err2 != nil || a == b || b != strings.TrimSuffix(a, ".scn")+"-2.scn" {
		t.Errorf("same second: %q %q %v %v", a, b, err1, err2)
	}
	if l, _ := listBackups(dir); fmt.Sprint(l) != fmt.Sprint([]string{a, b}) {
		t.Errorf("order: %v", l)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
