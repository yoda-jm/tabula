package webapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// browse sends a request from this machine to host, with headers.
func browse(h http.Handler, method, host, url, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:50000"
	req.Host = host
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestCrossSiteRequestsRefused(t *testing.T) {
	api := New(quiet)
	h := api.Handler(http.NotFoundHandler())
	const host = "localhost:8033"
	pw := `{"password":"long enough"}`

	// a page of another site, in a browser of this machine
	for _, hdr := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site"},
		{"Sec-Fetch-Site": "same-site"},
		{"Origin": "http://evil.example"},
	} {
		if w := browse(h, "POST", host, "/api/access/password", pw, hdr); w.Code != http.StatusForbidden {
			t.Errorf("%v: %d, want 403", hdr, w.Code)
		}
	}
	if api.HasPassword() {
		t.Fatal("a cross-site request set the password")
	}
	// reading is not refused (the browser keeps the answer from the other site)
	if w := browse(h, "GET", host, "/api/whoami", "", map[string]string{"Sec-Fetch-Site": "cross-site"}); w.Code != http.StatusOK {
		t.Errorf("cross-site GET: %d", w.Code)
	}
	// tabula's own pages, and scripts without browser headers
	if w := browse(h, "POST", host, "/api/access/settings", `{"musicians_can_write":false}`, nil); w.Code != http.StatusOK {
		t.Errorf("script: %d %s", w.Code, w.Body)
	}
	if w := browse(h, "POST", host, "/api/access/settings", `{"musicians_can_write":true}`, map[string]string{"Origin": "http://localhost:8033"}); w.Code != http.StatusOK {
		t.Errorf("same origin: %d %s", w.Code, w.Body)
	}
	if w := browse(h, "POST", host, "/api/access/password", pw, map[string]string{"Sec-Fetch-Site": "same-origin"}); w.Code != http.StatusOK {
		t.Errorf("same-origin: %d %s", w.Code, w.Body)
	}
	if !api.HasPassword() {
		t.Error("tabula's own page could not set the password")
	}
}

func TestUnknownHostRefused(t *testing.T) {
	api := New(quiet)
	api.SetAllowedHosts([]string{"Tabula.LAN", " "})
	h := api.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("gui")) }))
	name, _ := os.Hostname()
	for host, want := range map[string]int{
		"127.0.0.1:8033":       http.StatusOK,
		"192.168.1.20:8033":    http.StatusOK,
		"[::1]:8033":           http.StatusOK,
		"localhost:8033":       http.StatusOK,
		"localhost":            http.StatusOK,
		"tabula.lan:8033":      http.StatusOK,
		"tabula.lan.":          http.StatusOK,
		name + ":8033":         http.StatusOK,
		name + ".local":        http.StatusOK,
		"evil.example:8033":    http.StatusForbidden, // a site whose name leads here (DNS rebinding)
		"tabula.lan.evil.test": http.StatusForbidden,
	} {
		for _, path := range []string{"/api/whoami", "/"} {
			if w := browse(h, "GET", host, path, "", nil); w.Code != want {
				t.Errorf("%s%s: %d, want %d", host, path, w.Code, want)
			}
		}
	}
	w := browse(h, "GET", "evil.example:8033", "/", "", nil)
	if !strings.Contains(w.Body.String(), "-allowed-hosts evil.example") {
		t.Errorf("refusal: %q", w.Body)
	}
}
