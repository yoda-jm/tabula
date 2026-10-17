package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

func TestMusicianScope(t *testing.T) {
	st := state.New(schema.Build("M32C"))
	if got := mixBuses(st, 3); !slices.Equal(got, []int{3}) {
		t.Errorf("mono mix: %v", got)
	}
	st.Set("/config/buslink/3-4", osc.Int(1), "test")
	if got := mixBuses(st, 4); !slices.Equal(got, []int{3, 4}) {
		t.Errorf("stereo mix: %v", got)
	}
	for addr, want := range map[string]bool{
		"/ch/01/mix/03/level":    true,
		"/ch/32/mix/04/on":       true,
		"/auxin/07/mix/03/pan":   true,
		"/fxrtn/02/mix/03/level": true,
		"/bus/03/mix/fader":      true,
		"/bus/04/mix/on":         true,
		"/ch/01/mix/05/level":    false, // another musician's mix
		"/ch/01/mix/03/type":     false, // the tap is the engineer's
		"/ch/01/mix/fader":       false,
		"/bus/03/mix/pan":        false,
		"/bus/03/eq/1/g":         false,
		"/main/st/mix/fader":     false,
		"/config/buslink/3-4":    false,
	} {
		if got := musicianMay(addr, []int{3, 4}); got != want {
			t.Errorf("%s: %v, want %v", addr, got, want)
		}
	}
}

func TestBaseURLs(t *testing.T) {
	if urls, ok := baseURLs("127.0.0.1:8033"); ok || len(urls) != 1 {
		t.Errorf("loopback: %v %v", urls, ok)
	}
	if urls, ok := baseURLs("192.168.1.20:8033"); !ok || urls[0] != "http://192.168.1.20:8033" {
		t.Errorf("one address: %v %v", urls, ok)
	}
	if _, ok := baseURLs("[::]:8033"); !ok {
		t.Errorf("all addresses: not reachable")
	}
}

// remote sends a request as another device of the network would.
func remote(h http.Handler, method, url, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.RemoteAddr = "192.0.2.10:50000"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: accessCookie, Value: token})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func local(h http.Handler, method, url, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:50000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestAccess(t *testing.T) {
	emuSrv, emuAddr := startEmu(t)
	api := New(quiet)
	if err := api.SetAccessFile(filepath.Join(t.TempDir(), "access.json")); err != nil {
		t.Fatal(err)
	}
	h := api.Handler(http.NotFoundHandler())
	api.Connect(emuAddr)
	for deadline := time.Now().Add(15 * time.Second); !api.status().Connected; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("not connected")
		}
	}

	// Without a link, another device sees nothing; this machine is the engineer.
	if w := remote(h, "GET", "/api/state", "", ""); w.Code != http.StatusForbidden {
		t.Errorf("guest state: %d", w.Code)
	}
	var who whoView
	json.Unmarshal(local(h, "GET", "/api/whoami", "").Body.Bytes(), &who)
	if who.Role != RoleEngineer || !who.Local {
		t.Errorf("local: %+v", who)
	}
	api.SetGuestRole(RoleViewer)
	if w := remote(h, "GET", "/api/state", "", ""); w.Code != http.StatusOK {
		t.Errorf("viewer state: %d", w.Code)
	}
	if w := remote(h, "POST", "/api/set", `{"addr":"/ch/01/mix/03/level","value":0.5}`, ""); w.Code != http.StatusForbidden {
		t.Errorf("viewer set: %d", w.Code)
	}
	if w := remote(h, "GET", "/api/access", "", ""); w.Code != http.StatusForbidden {
		t.Errorf("viewer access list: %d", w.Code)
	}
	api.SetGuestRole(RoleNone)

	// The engineer gives Remy the mix of bus 3; the link opens the mix page.
	w := local(h, "POST", "/api/access", `{"name":"Remy","role":"musician","bus":3}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("add access: %d %s", w.Code, w.Body)
	}
	var g Grant
	json.Unmarshal(w.Body.Bytes(), &g)
	w = remote(h, "GET", "/a/"+g.Token, "", "")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/#/mix" || !strings.Contains(w.Header().Get("Set-Cookie"), g.Token) {
		t.Errorf("login: %d %v", w.Code, w.Header())
	}
	if w := remote(h, "GET", "/a/nope", "", ""); w.Code != http.StatusForbidden {
		t.Errorf("bad link: %d", w.Code)
	}

	// Remy changes their own mix, writes not armed; nothing else.
	if w := remote(h, "POST", "/api/set", `{"addr":"/ch/02/mix/03/level","text":"-6"}`, g.Token); w.Code != http.StatusNoContent {
		t.Errorf("own send: %d %s", w.Code, w.Body)
	}
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		v, _, _ := emuSrv.Store().Get("/ch/02/mix/03/level")
		if v.Float > 0.59 && v.Float < 0.61 { // -6 dB
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("emulator send level %v", v)
		}
	}
	for _, addr := range []string{"/ch/02/mix/04/level", "/ch/02/mix/fader", "/main/st/mix/fader"} {
		if w := remote(h, "POST", "/api/set", `{"addr":"`+addr+`","value":0.5}`, g.Token); w.Code != http.StatusForbidden {
			t.Errorf("%s: %d", addr, w.Code)
		}
	}
	if w := remote(h, "POST", "/api/connect", `{"addr":"127.0.0.1"}`, g.Token); w.Code != http.StatusForbidden {
		t.Errorf("musician connect: %d", w.Code)
	}
	local(h, "POST", "/api/access/settings", `{"musicians_can_write":false}`)
	if w := remote(h, "POST", "/api/set", `{"addr":"/bus/03/mix/fader","value":0.5}`, g.Token); w.Code != http.StatusForbidden {
		t.Errorf("writes not allowed: %d", w.Code)
	}
	local(h, "POST", "/api/access/settings", `{"musicians_can_write":true}`)
	if w := remote(h, "POST", "/api/set", `{"addr":"/bus/03/mix/fader","value":0.5}`, g.Token); w.Code != http.StatusNoContent {
		t.Errorf("bus fader: %d", w.Code)
	}

	// The accesses survive a restart; deleting one ends it.
	api2 := New(quiet)
	api2.SetAccessFile(api.acc.path)
	if _, ok := api2.acc.byToken(g.Token); !ok {
		t.Errorf("access not kept")
	}
	if w := local(h, "DELETE", "/api/access/"+g.ID, ""); w.Code != http.StatusNoContent {
		t.Errorf("delete: %d", w.Code)
	}
	if w := remote(h, "GET", "/api/state", "", g.Token); w.Code != http.StatusForbidden {
		t.Errorf("deleted access still works: %d", w.Code)
	}
}

func TestDeletedAccessEndsStream(t *testing.T) {
	api := New(quiet)
	h := api.Handler(http.NotFoundHandler())
	g, _ := api.acc.add("Viewer", RoleViewer, 0)
	ts := httptest.NewServer(h)
	defer ts.Close()
	req, _ := http.NewRequest("GET", ts.URL+"/api/events", nil)
	req.AddCookie(&http.Cookie{Name: accessCookie, Value: g.Token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	ended := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := resp.Body.Read(buf); err != nil {
				close(ended)
				return
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)
	api.acc.remove(g.ID)
	api.hub.kick(g.ID)
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Error("the stream is still open")
	}
}
