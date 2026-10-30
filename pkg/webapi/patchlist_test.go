package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putPatchlist(t *testing.T, url, body string) (int, patchlistView) {
	t.Helper()
	req, _ := http.NewRequest("PUT", url+"/api/patchlist", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v patchlistView
	json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

func TestPatchlist(t *testing.T) {
	scenes := t.TempDir()
	api := New(quiet)
	api.SetScenesDir(scenes)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()

	var v patchlistView
	getJSON(t, ts.URL+"/api/patchlist", &v)
	if v.Paper != "A4" || !v.Watermark || v.Notes == nil || v.Rows == nil || v.Title != "" {
		t.Fatalf("before anything is typed: %+v", v)
	}

	// spaces tidied, an empty note dropped; kept next to the scene files
	code, v := putPatchlist(t, ts.URL, `{"title": "  The  Band ", "details": "Le Zénith · 2 Nov", "box_a": "DL32",
		"paper": "Letter", "watermark": false,
		"notes": {"/ch/01": " SM57  on a short boom ", "/ch/02": "  "}, "rows": {"/ch/11": true, "AES50A:9": false}}`)
	if code != http.StatusOK {
		t.Fatalf("put: %d", code)
	}
	if v.Title != "The Band" || v.Details != "Le Zénith · 2 Nov" || v.BoxA != "DL32" || v.Paper != "Letter" || v.Watermark ||
		len(v.Notes) != 1 || v.Notes["/ch/01"] != "SM57 on a short boom" || !v.Rows["/ch/11"] || v.Rows["AES50A:9"] || v.Updated.IsZero() {
		t.Fatalf("saved: %+v", v)
	}
	b, err := os.ReadFile(filepath.Join(scenes, patchlistFile))
	if err != nil || !strings.Contains(string(b), "SM57 on a short boom") {
		t.Fatalf("file: %v %s", err, b)
	}

	// a restart reads it back
	api2 := New(quiet)
	api2.SetScenesDir(scenes)
	ts2 := httptest.NewServer(api2.Handler(http.NotFoundHandler()))
	defer ts2.Close()
	var again patchlistView
	getJSON(t, ts2.URL+"/api/patchlist", &again)
	if again.Title != "The Band" || again.Watermark || again.Notes["/ch/01"] != "SM57 on a short boom" {
		t.Fatalf("read back: %+v", again)
	}

	// what a request leaves out takes its first value
	if code, v = putPatchlist(t, ts.URL, `{"title": "Short"}`); code != http.StatusOK || v.Paper != "A4" || !v.Watermark || len(v.Notes) != 0 {
		t.Fatalf("partial: %d %+v", code, v)
	}

	for _, bad := range []string{
		`{"paper": "A3"}`,
		`{"title": "` + strings.Repeat("x", maxPatchText+1) + `"}`,
		`{"notes": {"/ch/01": "` + strings.Repeat("é", maxPatchNote+1) + `"}}`,
		`{"notes": {"has space": "x"}}`,
		`{"rows": {"": true}}`,
		`not json`,
	} {
		if code, _ := putPatchlist(t, ts.URL, bad); code != http.StatusBadRequest {
			t.Errorf("%.40s: %d, want 400", bad, code)
		}
	}
	getJSON(t, ts.URL+"/api/patchlist", &v)
	if v.Title != "Short" {
		t.Fatalf("a refused request changed it: %+v", v)
	}

	// another scenes folder has its own
	other := t.TempDir()
	api.SetScenesDir(other)
	getJSON(t, ts.URL+"/api/patchlist", &v)
	if v.Title != "" {
		t.Fatalf("other folder: %+v", v)
	}
}
