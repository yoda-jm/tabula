package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() {
	passwordIterations = 1000 // fast hashes; the cost is stored with each one
	loginDelay = 0
}

// from sends a request from addr (host:port) with an access token, if any.
func from(h http.Handler, addr, method, url, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.RemoteAddr = addr
	req.Host = "192.0.2.1:8033"
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0")
	if token != "" {
		req.AddCookie(&http.Cookie{Name: accessCookie, Value: token})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

const (
	here  = "127.0.0.1:50000"
	phone = "192.0.2.10:50000"
	other = "192.0.2.11:50000"
)

func cookieOf(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == accessCookie {
			return c.Value
		}
	}
	return ""
}

func whoFor(t *testing.T, h http.Handler, addr, token string) whoView {
	t.Helper()
	var v whoView
	if err := json.Unmarshal(from(h, addr, "GET", "/api/whoami", "", token).Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPasswordHash(t *testing.T) {
	p, err := hashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !p.matches("correct horse") || p.matches("correct horsE") || p.matches("") {
		t.Errorf("matches: %+v", p)
	}
	var none *passwordHash
	if none.matches("") {
		t.Error("no password matches")
	}
	q, _ := hashPassword("correct horse")
	if q.Salt == p.Salt || q.Hash == p.Hash {
		t.Error("same salt twice")
	}
}

func TestLoginLimiter(t *testing.T) {
	var l loginLimiter
	t0 := time.Now()
	for i := 0; i < maxFails-1; i++ {
		l.failed("a", t0)
	}
	if d := l.wait("a", t0); d != 0 {
		t.Errorf("after %d failures: %v", maxFails-1, d)
	}
	l.failed("a", t0)
	if d := l.wait("a", t0); d != firstWait {
		t.Errorf("after %d failures: %v", maxFails, d)
	}
	if d := l.wait("b", t0); d != 0 {
		t.Errorf("another client waits %v", d)
	}
	t1 := t0.Add(firstWait)
	if d := l.wait("a", t1); d != 0 {
		t.Errorf("after the wait: %v", d)
	}
	l.failed("a", t1)
	if d := l.wait("a", t1); d != 2*firstWait {
		t.Errorf("doubled: %v", d)
	}
	l.succeeded("a")
	if d := l.wait("a", t1); d != 0 {
		t.Errorf("after success: %v", d)
	}

	// all clients together
	var g loginLimiter
	for i := 0; i < globalFails; i++ {
		g.failed(string(rune('A'+i)), t0.Add(time.Duration(i)*time.Second))
	}
	last := t0.Add(globalFails * time.Second)
	if d := g.wait("new", last); d != time.Minute-globalFails*time.Second {
		t.Errorf("global: %v", d)
	}
	if d := g.wait("new", t0.Add(time.Minute+time.Second)); d != 0 {
		t.Errorf("global, a minute later: %v", d)
	}
}

func TestDeviceName(t *testing.T) {
	for ua, want := range map[string]string{
		"Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0":                                                                  "Firefox on Linux (192.0.2.10)",
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36":                       "Chrome on Android (192.0.2.10)",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1": "Safari on iPhone (192.0.2.10)",
		"curl/8.0": "A browser (192.0.2.10)",
	} {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = phone
		req.Header.Set("User-Agent", ua)
		if got := deviceName(req); got != want {
			t.Errorf("%s: %q, want %q", ua, got, want)
		}
	}
}

func TestPassword(t *testing.T) {
	api := New(quiet)
	file := filepath.Join(t.TempDir(), "access.json")
	if err := api.SetAccessFile(file); err != nil {
		t.Fatal(err)
	}
	h := api.Handler(http.NotFoundHandler())
	theo, _ := api.acc.add("Theo", RoleMusician, 3)

	if w := from(h, phone, "POST", "/api/login", `{"password":"x"}`, ""); w.Code != http.StatusConflict {
		t.Errorf("login without a password: %d", w.Code)
	}
	if v := whoFor(t, h, here, ""); v.Role != RoleEngineer || v.Password {
		t.Errorf("before: %+v", v)
	}
	if w := from(h, here, "POST", "/api/access/password", `{"password":"short"}`, ""); w.Code != http.StatusBadRequest {
		t.Errorf("short password: %d", w.Code)
	}
	api.SetGuestRole(RoleEngineer)
	if w := from(h, here, "POST", "/api/access/password", `{"password":"correct horse"}`, ""); w.Code != http.StatusConflict {
		t.Errorf("password with guest engineer: %d", w.Code)
	}
	api.SetGuestRole(RoleNone)

	// The first password signs the engineer in on the way.
	w := from(h, here, "POST", "/api/access/password", `{"password":"correct horse"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body)
	}
	mine := cookieOf(w)
	if mine == "" {
		t.Fatal("no session cookie")
	}
	var st accessView
	json.Unmarshal(w.Body.Bytes(), &st)
	if !st.Password || len(st.Grants) != 2 || !st.Grants[1].Session || st.Grants[1].Token != "" || st.Current != st.Grants[1].ID {
		t.Errorf("status: %+v", st)
	}
	if st.Grants[0].Token == "" {
		t.Error("the link lost its token")
	}
	if v := whoFor(t, h, here, mine); v.Role != RoleEngineer || !v.Session || v.Link || !v.Password {
		t.Errorf("signed in: %+v", v)
	}

	// This machine needs the password now; links work as before.
	if v := whoFor(t, h, here, ""); v.Role != RoleNone || !v.Password || !v.Local {
		t.Errorf("this machine, not signed in: %+v", v)
	}
	if w := from(h, here, "GET", "/api/state", "", ""); w.Code != http.StatusForbidden {
		t.Errorf("this machine, state: %d", w.Code)
	}
	if v := whoFor(t, h, phone, theo.Token); v.Role != RoleMusician || !v.Link {
		t.Errorf("musician link: %+v", v)
	}

	// Sign in from a phone, then out.
	if w := from(h, phone, "POST", "/api/login", `{"password":"wrong horse"}`, ""); w.Code != http.StatusForbidden || cookieOf(w) != "" {
		t.Errorf("wrong password: %d", w.Code)
	}
	w = from(h, phone, "POST", "/api/login", `{"password":"correct horse"}`, "")
	tab := cookieOf(w)
	if w.Code != http.StatusOK || tab == "" {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	var v whoView
	json.Unmarshal(w.Body.Bytes(), &v)
	if v.Role != RoleEngineer || !v.Session {
		t.Errorf("login answer: %+v", v)
	}
	if g, _ := api.acc.byToken(tab); g.Name != "Firefox on Linux (192.0.2.10)" {
		t.Errorf("session name %q", g.Name)
	}
	if w := from(h, phone, "POST", "/api/logout", `{}`, tab); w.Code != http.StatusOK {
		t.Errorf("logout: %d", w.Code)
	}
	if _, ok := api.acc.byToken(tab); ok {
		t.Error("the session survived the logout")
	}

	// Changing the password ends the other sessions.
	tab = cookieOf(from(h, phone, "POST", "/api/login", `{"password":"correct horse"}`, ""))
	if w := from(h, here, "POST", "/api/access/password", `{"current":"nope","password":"battery staple"}`, mine); w.Code != http.StatusForbidden {
		t.Errorf("change, wrong current: %d", w.Code)
	}
	if w := from(h, here, "POST", "/api/access/password", `{"current":"correct horse","password":"battery staple"}`, mine); w.Code != http.StatusOK {
		t.Fatalf("change: %d %s", w.Code, w.Body)
	}
	if v := whoFor(t, h, phone, tab); v.Role != RoleNone {
		t.Errorf("other session after the change: %+v", v)
	}
	if v := whoFor(t, h, here, mine); v.Role != RoleEngineer {
		t.Errorf("own session after the change: %+v", v)
	}
	if v := whoFor(t, h, phone, theo.Token); v.Role != RoleMusician {
		t.Errorf("link after the change: %+v", v)
	}

	// Too many wrong passwords: that client waits, others do not.
	for i := 0; i < maxFails; i++ {
		from(h, phone, "POST", "/api/login", `{"password":"guess"}`, "")
	}
	w = from(h, phone, "POST", "/api/login", `{"password":"battery staple"}`, "")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "30" || !strings.Contains(w.Body.String(), "try again in 30 s") {
		t.Errorf("rate limit: %d %v %s", w.Code, w.Header(), w.Body)
	}
	if w := from(h, other, "POST", "/api/login", `{"password":"battery staple"}`, ""); w.Code != http.StatusOK {
		t.Errorf("another device: %d %s", w.Code, w.Body)
	}

	// The access file keeps the hash, never the password.
	data, _ := os.ReadFile(file)
	if strings.Contains(string(data), "battery") || !strings.Contains(string(data), "pbkdf2-sha256") {
		t.Errorf("access file: %s", data)
	}

	// Removing the password: this machine is the engineer again.
	if w := from(h, here, "POST", "/api/access/password", `{"current":"battery staple","password":""}`, mine); w.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", w.Code, w.Body)
	}
	if v := whoFor(t, h, here, ""); v.Role != RoleEngineer || v.Password {
		t.Errorf("after removal: %+v", v)
	}
}

func TestResetPassword(t *testing.T) {
	file := filepath.Join(t.TempDir(), "access.json")
	api := New(quiet)
	api.SetAccessFile(file)
	if had, err := api.ResetPassword(); had || err != nil {
		t.Errorf("reset without a password: %v %v", had, err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("reset made an access file")
	}
	h := api.Handler(http.NotFoundHandler())
	from(h, here, "POST", "/api/access/password", `{"password":"correct horse"}`, "")
	api.acc.add("Theo", RoleMusician, 3)

	again := New(quiet) // as tabula -reset-password does
	again.SetAccessFile(file)
	if !again.HasPassword() {
		t.Fatal("password not kept")
	}
	if had, err := again.ResetPassword(); !had || err != nil {
		t.Fatalf("reset: %v %v", had, err)
	}
	after := New(quiet)
	after.SetAccessFile(file)
	d := after.acc.snapshot()
	if after.HasPassword() || len(d.Grants) != 1 || d.Grants[0].Name != "Theo" {
		t.Errorf("after reset: %+v", d)
	}
}

func TestPasswordEndsUnsignedStreams(t *testing.T) {
	api := New(quiet)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/events") // from this machine, no password yet
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
	r, _ := http.Post(ts.URL+"/api/access/password", "application/json", strings.NewReader(`{"password":"correct horse"}`))
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("set: %d", r.StatusCode)
	}
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Error("the stream opened without signing in is still open")
	}
}
