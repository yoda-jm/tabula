package webapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSceneFileNames(t *testing.T) {
	for name, ok := range map[string]bool{
		"GVO-2026.scn": true, "Set 1.SCN": true, "a.scn": true,
		"": false, "x.txt": false, ".hidden.scn": false, "a/b.scn": false, `a\b.scn`: false, "a\x01.scn": false,
		strings.Repeat("a", 120) + ".scn": false,
	} {
		if err := validFileName(name); (err == nil) != ok {
			t.Errorf("%q: %v", name, err)
		}
	}
	for name, want := range map[string]string{
		"GVO 2026": "GVO 2026.scn", "a/b": "a-b.scn", "..": "scene.scn", " Set: 1? ": "Set- 1-.scn",
	} {
		if got := fileNameFor(name); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
	for line, want := range map[string][2]string{
		`#4.0# "GVO-2026" "Rappel 10 m" %000000000 1     `: {"GVO-2026", "Rappel 10 m"},
		`#2.1# "Old" %000000000 1`:                         {"Old", ""},
		`#4.0# "say \"hi\"" ""`:                            {`say "hi"`, ""},
		`/ch/01/config "x" 1 RD 1`:                         {"", ""},
	} {
		if n, note := parseHeader(line); n != want[0] || note != want[1] {
			t.Errorf("%q: %q %q", line, n, note)
		}
	}
}

func TestSceneFiles(t *testing.T) {
	emuSrv, emuAddr := startEmu(t)
	dir := filepath.Join(t.TempDir(), "scenes") // made by the first save
	api := New(quiet)
	api.SetScenesDir(dir)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()

	type listing struct {
		Dir   string      `json:"dir"`
		Files []sceneFile `json:"files"`
	}
	var l listing
	getJSON(t, ts.URL+"/api/scenefiles", &l)
	if l.Dir != dir || l.Files == nil || len(l.Files) != 0 {
		t.Fatalf("empty: %+v", l)
	}
	save := `{"name":"Demo set","note":"before the show"}`
	if code := post(t, ts.URL+"/api/scenefiles", save); code != http.StatusConflict {
		t.Errorf("save, not connected: %d", code)
	}
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)

	// The console's settings as a file, then the same name again.
	if code := post(t, ts.URL+"/api/scenefiles", save); code != http.StatusOK {
		t.Fatalf("save: %d", code)
	}
	if code := post(t, ts.URL+"/api/scenefiles", save); code != http.StatusConflict {
		t.Errorf("save again: %d", code)
	}
	if code := post(t, ts.URL+"/api/scenefiles", `{"name":"Demo set","note":"v2","replace":true}`); code != http.StatusOK {
		t.Errorf("replace: %d", code)
	}
	getJSON(t, ts.URL+"/api/scenefiles", &l)
	if len(l.Files) != 1 || l.Files[0].File != "Demo set.scn" || l.Files[0].Name != "Demo set" || l.Files[0].Note != "v2" || l.Files[0].Size < 10000 {
		t.Fatalf("listing: %+v", l.Files)
	}

	// A file from the browser: one known line, one this console does not know.
	demo, err := os.ReadFile("../../testdata/scenes/demo.scn")
	if err != nil {
		t.Fatal(err)
	}
	var config string
	for _, line := range strings.Split(string(demo), "\n") {
		if strings.HasPrefix(line, "/ch/01/config ") {
			config = line
		}
	}
	_, rest, _ := strings.Cut(strings.TrimPrefix(config, "/ch/01/config "), `" `)
	upload := "#4.0# \"Uploaded\" \"\" %000000000 1\n/ch/01/config \"Uploaded\" " + rest + "\n/no/such/node 1\n"
	put := func(name, body, query string) int {
		req, _ := http.NewRequest("PUT", ts.URL+"/api/scenefiles/"+name+query, strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, c := range []struct {
		name, body, query string
		want              int
	}{
		{"up.scn", upload, "", http.StatusNoContent},
		{"up.scn", upload, "", http.StatusConflict},
		{"up.scn", upload, "?replace=1", http.StatusNoContent},
		{"up.txt", upload, "", http.StatusBadRequest},
		{"empty.scn", "#4.0# \"x\"\n", "", http.StatusBadRequest},
	} {
		if code := put(c.name, c.body, c.query); code != c.want {
			t.Errorf("put %s%s: %d, want %d", c.name, c.query, code, c.want)
		}
	}

	resp, err := http.Get(ts.URL + "/api/scenefiles/up.scn")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != upload || !strings.Contains(resp.Header.Get("Content-Disposition"), `filename="up.scn"`) {
		t.Errorf("download: %q %v", body, resp.Header)
	}

	// Loading changes the console: writes must be armed.
	if code := post(t, ts.URL+"/api/scenefiles/up.scn/load", ``); code != http.StatusForbidden {
		t.Errorf("unarmed load: %d", code)
	}
	post(t, ts.URL+"/api/arm", `{"armed":true}`)
	resp, err = http.Post(ts.URL+"/api/scenefiles/up.scn/load", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var p sceneProgress
	decode(t, resp, &p)
	if !p.Done || p.Sent != 1 || p.Total != 1 || p.Error != "" || len(p.Skipped) != 1 || p.Skipped[0] != "/no/such/node 1" {
		t.Errorf("load: %+v", p)
	}
	if v, _, _ := emuSrv.Store().Get("/ch/01/config/name"); v.Str != "Uploaded" {
		t.Errorf("console: %q", v.Str)
	}
	var nodes []stateNode
	getJSON(t, ts.URL+"/api/state?prefix=/ch/01/config", &nodes)
	if len(nodes) != 1 || !strings.Contains(nodes[0].Line, `"Uploaded"`) {
		t.Errorf("mirror: %+v", nodes)
	}

	// The whole scene saved before goes back, every line known.
	resp, err = http.Post(ts.URL+"/api/scenefiles/Demo%20set.scn/load", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	p = sceneProgress{}
	decode(t, resp, &p)
	if !p.Done || p.Sent < 1000 || p.Sent != p.Total || len(p.Skipped) != 0 || p.Error != "" {
		t.Errorf("whole scene: %+v", p)
	}
	if v, _, _ := emuSrv.Store().Get("/ch/01/config/name"); v.Str == "Uploaded" {
		t.Errorf("console after the whole scene: %q", v.Str)
	}

	for path, want := range map[string]int{"up.scn": http.StatusNoContent, "gone.scn": http.StatusNotFound, "x.txt": http.StatusBadRequest} {
		req, _ := http.NewRequest("DELETE", ts.URL+"/api/scenefiles/"+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("delete %s: %d, want %d", path, resp.StatusCode, want)
		}
	}
	if code := post(t, ts.URL+"/api/scenefiles/up.scn/load", ``); code != http.StatusNotFound {
		t.Errorf("load of a deleted file: %d", code)
	}
}
