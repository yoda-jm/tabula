package webapi

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

// Web pages of other sites. Requests from this machine are the engineer's
// when no password is set, so a page of any site open in a browser here
// could otherwise drive tabula: the browser sends its requests from this
// machine. Cross-site requests that change something are refused, and so
// are requests naming tabula by a host name it does not know: a site can
// make its own name lead to this machine (DNS rebinding), and its pages
// would then be same-origin and read the answers.

// SetAllowedHosts adds host names tabula answers to, besides IP addresses,
// localhost and this machine's name: a DNS name, or the name a reverse proxy
// passes on. Call it before serving.
func (s *Server) SetAllowedHosts(hosts []string) {
	for _, h := range hosts {
		if h = normalHost(h); h != "" {
			s.allowedHosts = append(s.allowedHosts, h)
		}
	}
}

// normalHost lowercases a host name and drops its port and final dot.
func normalHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.Trim(strings.TrimSpace(host), "[]"), ".")
	return strings.ToLower(host)
}

// hostAllowed reports whether a request's Host names this tabula.
func (s *Server) hostAllowed(hostHeader string) bool {
	h := normalHost(hostHeader)
	if h == "" || net.ParseIP(h) != nil || h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if name, err := os.Hostname(); err == nil && name != "" {
		name = strings.ToLower(name)
		if h == name || h == name+".local" {
			return true
		}
	}
	for _, a := range s.allowedHosts {
		if h == a {
			return true
		}
	}
	return false
}

// protect refuses requests for unknown host names and cross-site requests
// that change something (net/http's CrossOriginProtection: Sec-Fetch-Site,
// else Origin against Host; requests without them, from curl or scripts,
// pass).
func (s *Server) protect(next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.log.Warn("cross-site request refused", "method", r.Method, "path", r.URL.Path, "origin", r.Header.Get("Origin"))
		httpError(w, http.StatusForbidden, errors.New("refused: this request comes from a page of another site"))
	}))
	inner := cop.Handler(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostAllowed(r.Host) {
			s.log.Warn("unknown host name refused", "host", r.Host, "from", r.RemoteAddr)
			msg := fmt.Sprintf("tabula does not answer to the name %q: open it by its IP address, or start it with -allowed-hosts %s",
				normalHost(r.Host), normalHost(r.Host))
			if strings.HasPrefix(r.URL.Path, "/api/") {
				httpError(w, http.StatusForbidden, errors.New(msg))
			} else {
				http.Error(w, msg, http.StatusForbidden)
			}
			return
		}
		inner.ServeHTTP(w, r)
	})
}
