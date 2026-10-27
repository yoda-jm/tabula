package webapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUndoAndStripCopy(t *testing.T) {
	emuSrv, emuAddr := startEmu(t)
	api := New(quiet)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)
	console := emuSrv.Store()
	line := func(path string) string { return console.NodeLine(console.Schema().Node(path)) }
	var u undoView
	undo := func(want int) {
		t.Helper()
		if code := post(t, ts.URL+"/api/undo", ``); code != want {
			t.Fatalf("undo: %d, want %d", code, want)
		}
		getJSON(t, ts.URL+"/api/undo", &u)
	}

	if code := post(t, ts.URL+"/api/undo", ``); code != http.StatusForbidden {
		t.Errorf("unarmed undo: %d", code)
	}
	post(t, ts.URL+"/api/arm", `{"armed":true}`)
	undo(http.StatusConflict) // nothing yet

	// a fader dragged is one step; another parameter is another
	fader, name := line("/ch/01/mix"), line("/ch/02/config")
	for _, db := range []string{"-20", "-12", "-3"} {
		post(t, ts.URL+"/api/set", `{"addr":"/ch/01/mix/fader","text":"`+db+`"}`)
	}
	post(t, ts.URL+"/api/set", `{"addr":"/ch/02/config/name","text":"Changed"}`)
	getJSON(t, ts.URL+"/api/undo", &u)
	if u.Count != 2 || !strings.HasPrefix(u.What, "/ch/02/config/name (was ") {
		t.Fatalf("steps: %+v", u)
	}
	undo(http.StatusOK)
	if got := line("/ch/02/config"); got != name || u.Count != 1 || !strings.HasPrefix(u.What, "/ch/01/mix/fader (was ") {
		t.Errorf("after one undo: %q (want %q), %+v", got, name, u)
	}
	undo(http.StatusOK)
	if got := line("/ch/01/mix"); got != fader || u.Count != 0 {
		t.Errorf("after two: %q (want %q), %+v", got, fader, u)
	}

	// a channel's EQ to two others, then undone
	eqs := []string{"/eq", "/eq/1", "/eq/2", "/eq/3", "/eq/4"}
	before := map[string]string{}
	for _, ch := range []string{"/ch/02", "/ch/03"} {
		for _, e := range eqs {
			before[ch+e] = line(ch + e)
		}
	}
	post(t, ts.URL+"/api/set", `{"addr":"/ch/01/eq/2/g","text":"4.5"}`) // so that the copy changes something
	resp, err := http.Post(ts.URL+"/api/strips/copy", "application/json",
		strings.NewReader(`{"from":"/ch/01","to":["/ch/02","/ch/03"],"sections":["eq"]}`))
	if err != nil || resp.StatusCode != http.StatusOK {
		var e map[string]string
		decode(t, resp, &e)
		t.Fatalf("copy: %v %v", err, e)
	}
	resp.Body.Close()
	for _, ch := range []string{"/ch/02", "/ch/03"} {
		for _, e := range eqs {
			if got, want := line(ch+e), ch+strings.TrimPrefix(line("/ch/01"+e), "/ch/01"); got != want {
				t.Errorf("copied %s: %q, want %q", ch+e, got, want)
			}
		}
	}
	getJSON(t, ts.URL+"/api/undo", &u)
	if u.Count != 2 || u.What != "copy of /ch/01 (eq) to /ch/02, /ch/03" {
		t.Errorf("copy step: %+v", u)
	}
	undo(http.StatusOK)
	for path, want := range before {
		if got := line(path); got != want {
			t.Errorf("undone %s: %q, want %q", path, got, want)
		}
	}

	for body, want := range map[string]int{
		`{"from":"/main/st","to":["/main/m"],"sections":["eq"]}`:      http.StatusBadRequest,
		`{"from":"/ch/01","to":["/bus/01"],"sections":["eq"]}`:        http.StatusBadRequest,
		`{"from":"/ch/01","to":["/ch/01"],"sections":["eq"]}`:         http.StatusBadRequest,
		`{"from":"/ch/01","to":["/ch/02"],"sections":["name"]}`:       http.StatusBadRequest,
		`{"from":"/ch/01","to":[],"sections":["eq"]}`:                 http.StatusBadRequest,
		`{"from":"/fxrtn/01","to":["/fxrtn/02"],"sections":["gate"]}`: http.StatusBadRequest, // no gate on an FX return
	} {
		if code := post(t, ts.URL+"/api/strips/copy", body); code != want {
			t.Errorf("%s: %d, want %d", body, code, want)
		}
	}
	post(t, ts.URL+"/api/arm", `{"armed":false}`)
	if code := post(t, ts.URL+"/api/strips/copy", `{"from":"/ch/01","to":["/ch/02"],"sections":["eq"]}`); code != http.StatusForbidden {
		t.Errorf("unarmed copy: %d", code)
	}
}
