package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func putSetlist(t *testing.T, url, body string) (int, setlistView) {
	t.Helper()
	req, _ := http.NewRequest("PUT", url+"/api/setlist", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v setlistView
	json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

type markAnswer struct {
	recordView
	Marked markView `json:"marked"`
}

func mark(t *testing.T, url, body string) (int, markAnswer) {
	t.Helper()
	resp, err := http.Post(url+"/api/record/mark", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v markAnswer
	json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

func TestSetlist(t *testing.T) {
	_, emuAddr := startEmu(t)
	sessions := t.TempDir()
	api := New(quiet)
	api.SetSessionsDir(sessions)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()

	var v setlistView
	getJSON(t, ts.URL+"/api/setlist", &v)
	if v.Songs == nil || len(v.Songs) != 0 || v.Next != 0 {
		t.Fatalf("empty: %+v", v)
	}

	// typed with blank lines and spaces; kept next to the sessions
	code, v := putSetlist(t, ts.URL, `{"songs":["  Opening ", "", "Second   song", "Third", "Fourth", "Encore"]}`)
	if code != http.StatusOK || strings.Join(v.Songs, "|") != "Opening|Second song|Third|Fourth|Encore" || v.Next != 0 {
		t.Fatalf("put: %d %+v", code, v)
	}
	tooMany, _ := json.Marshal(map[string][]string{"songs": slices.Repeat([]string{"x"}, 201)})
	for body, want := range map[string]int{
		`{"songs":["` + strings.Repeat("a", 81) + `"]}`: http.StatusBadRequest,
		string(tooMany): http.StatusBadRequest,
		`nope`:          http.StatusBadRequest,
	} {
		if code, _ := putSetlist(t, ts.URL, body); code != want {
			t.Errorf("%.40s: %d, want %d", body, code, want)
		}
	}

	// marks need a recording
	if code, _ := mark(t, ts.URL, `{"next":true}`); code != http.StatusConflict {
		t.Errorf("mark, not recording: %d", code)
	}
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)
	if code := post(t, ts.URL+"/api/record/start", ``); code != http.StatusOK {
		t.Fatalf("record: %d", code)
	}
	if code, m := mark(t, ts.URL, `{"next":true}`); code != http.StatusOK || m.Marked.Label != "1. Opening" || !m.Recording {
		t.Errorf("first song: %d %+v", code, m.Marked)
	}

	// two devices at once never mark the same song
	var mu sync.Mutex
	var labels []string
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, m := mark(t, ts.URL, `{"next":true}`)
			mu.Lock()
			labels = append(labels, m.Marked.Label)
			mu.Unlock()
		}()
	}
	wg.Wait()
	slices.Sort(labels)
	if got := strings.Join(labels[:4], "|"); got != "2. Second song|3. Third|4. Fourth|5. Encore" || !strings.HasPrefix(labels[4], "Marker ") {
		t.Errorf("concurrent marks: %q", labels)
	}

	// another song, a label, a song that is not there
	if _, m := mark(t, ts.URL, `{"song":2}`); m.Marked.Label != "3. Third" {
		t.Errorf("song 3: %+v", m.Marked)
	}
	getJSON(t, ts.URL+"/api/setlist", &v)
	if v.Next != 3 {
		t.Errorf("after song 3, next %d", v.Next)
	}
	if _, m := mark(t, ts.URL, `{"label":"Break","next":true}`); m.Marked.Label != "Break" {
		t.Errorf("a label: %+v", m.Marked)
	}
	if code, _ := mark(t, ts.URL, `{"song":9}`); code != http.StatusBadRequest {
		t.Errorf("song 10: %d", code)
	}
	getJSON(t, ts.URL+"/api/setlist", &v)
	if v.Next != 3 {
		t.Errorf("a label moved the next song: %d", v.Next)
	}

	// back one song; moved past the end, it stops there
	post(t, ts.URL+"/api/setlist/next", `{"next":2}`)
	getJSON(t, ts.URL+"/api/setlist", &v)
	if v.Next != 2 {
		t.Errorf("back: %d", v.Next)
	}
	post(t, ts.URL+"/api/setlist/next", `{"next":99}`)
	getJSON(t, ts.URL+"/api/setlist", &v)
	if v.Next != 5 {
		t.Errorf("past the end: %d", v.Next)
	}
	// reordered: the next song stays the same song
	post(t, ts.URL+"/api/setlist/next", `{"next":1}`)
	if _, v = putSetlist(t, ts.URL, `{"songs":["Second song","Opening","Third"]}`); v.Next != 0 {
		t.Errorf("reordered: next %d", v.Next)
	}

	// the session keeps the songs it was recorded with, and its markers
	var rv recordView
	getJSON(t, ts.URL+"/api/record", &rv)
	post(t, ts.URL+"/api/record/stop", ``)
	var one sessionView
	getJSON(t, ts.URL+"/api/sessions/"+rv.Name, &one)
	if strings.Join(one.Setlist, "|") != "Second song|Opening|Third" || len(one.Markers) != 8 || one.Markers[0].Marker != "1. Opening" {
		t.Errorf("session: %q, %d markers", one.Setlist, len(one.Markers))
	}

	// tabula started again: the setlist is still there
	again := New(quiet)
	again.SetSessionsDir(sessions)
	ts2 := httptest.NewServer(again.Handler(http.NotFoundHandler()))
	defer ts2.Close()
	getJSON(t, ts2.URL+"/api/setlist", &v)
	if strings.Join(v.Songs, "|") != "Second song|Opening|Third" || v.Next != 0 {
		t.Errorf("after a restart: %+v", v)
	}
	if _, err := os.Stat(filepath.Join(sessions, setlistFile+".tmp")); err == nil {
		t.Errorf("a temporary file stayed")
	}
	// a damaged file: an empty setlist, not a failure
	os.WriteFile(filepath.Join(sessions, setlistFile), []byte("{"), 0o644)
	third := New(quiet)
	third.SetSessionsDir(sessions)
	third.setlist.mu.Lock()
	if v := third.setlistLocked(); len(v.Songs) != 0 {
		t.Errorf("damaged: %+v", v)
	}
	third.setlist.mu.Unlock()
}
