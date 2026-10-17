package webapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/state"
)

// Access: who may do what through the GUI and the API.
//
// Roles, from least to most: none, viewer (sees everything, changes
// nothing), musician (also changes their own in-ear mix: the sends to their
// bus from the channels, aux inputs and FX returns, and that bus's fader and
// mute) and engineer (everything). Requests from this machine are the
// engineer's. Other devices get the guest role (none by default) unless they
// opened an access link, /a/<token>, made by the engineer on the Access page:
// the link sets a cookie holding the token. Deleting an access ends it at
// once, open pages included.

type Role string

const (
	RoleNone     Role = "none"
	RoleViewer   Role = "viewer"
	RoleMusician Role = "musician"
	RoleEngineer Role = "engineer"
)

var roleRank = map[Role]int{RoleNone: 0, RoleViewer: 1, RoleMusician: 2, RoleEngineer: 3}

func (r Role) atLeast(o Role) bool { return roleRank[r] >= roleRank[o] }

// ParseRole reads a role name.
func ParseRole(s string) (Role, error) {
	r := Role(strings.ToLower(strings.TrimSpace(s)))
	if _, ok := roleRank[r]; !ok {
		return "", fmt.Errorf("unknown role %q (none, viewer, musician, engineer)", s)
	}
	return r, nil
}

// routeRoles is the least role each API route needs; any other /api/ route
// is the engineer's.
var routeRoles = map[string]Role{
	"GET /api/whoami":  RoleNone,
	"POST /api/logout": RoleNone,
	"GET /api/status":  RoleViewer,
	"GET /api/schema":  RoleViewer,
	"GET /api/state":   RoleViewer,
	"GET /api/events":  RoleViewer,
	"GET /api/record":  RoleViewer,
	"POST /api/set":    RoleMusician, // checked further for musicians
}

const accessCookie = "tabula_access"

// Grant is an access given to a person or a device.
type Grant struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Role    Role      `json:"role"`
	Bus     int       `json:"bus,omitempty"` // musician: their in-ear mix, bus 1-16
	Token   string    `json:"token"`
	Created time.Time `json:"created"`
}

type accessData struct {
	MusiciansCanWrite bool    `json:"musicians_can_write"`
	Grants            []Grant `json:"grants"`
}

type access struct {
	mu    sync.Mutex
	path  string // "" keeps the accesses in memory only
	data  accessData
	guest Role
}

func (a *access) init() {
	a.data.MusiciansCanWrite = true
	a.guest = RoleNone
}

// load reads the accesses from path, which may not exist yet.
func (a *access) load(path string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.path = path
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var d accessData
	if err := json.Unmarshal(data, &d); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	a.data = d
	return nil
}

// saveLocked writes the accesses, readable by their owner only: the file
// holds the tokens.
func (a *access) saveLocked() error {
	if a.path == "" {
		return nil
	}
	data, _ := json.MarshalIndent(a.data, "", "  ")
	tmp := a.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.path)
}

func (a *access) byToken(token string) (Grant, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, g := range a.data.Grants {
		if subtle.ConstantTimeCompare([]byte(g.Token), []byte(token)) == 1 {
			return g, true
		}
	}
	return Grant{}, false
}

func randomString(n int, enc func([]byte) string) string {
	b := make([]byte, n)
	rand.Read(b)
	return enc(b)
}

func (a *access) add(name string, role Role, bus int) (Grant, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return Grant{}, errors.New("a name is required")
	case role == RoleNone:
		return Grant{}, errors.New("an access needs a role")
	case role == RoleMusician && (bus < 1 || bus > 16):
		return Grant{}, errors.New("a musician's mix is a bus, 1 to 16")
	case role != RoleMusician:
		bus = 0
	}
	g := Grant{
		ID:      randomString(6, hex.EncodeToString),
		Name:    name,
		Role:    role,
		Bus:     bus,
		Token:   randomString(18, base64.RawURLEncoding.EncodeToString),
		Created: time.Now().UTC().Truncate(time.Second),
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data.Grants = append(a.data.Grants, g)
	return g, a.saveLocked()
}

func (a *access) remove(id string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, g := range a.data.Grants {
		if g.ID == id {
			a.data.Grants = append(a.data.Grants[:i], a.data.Grants[i+1:]...)
			return true, a.saveLocked()
		}
	}
	return false, nil
}

func (a *access) setMusiciansCanWrite(on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data.MusiciansCanWrite = on
	return a.saveLocked()
}

func (a *access) snapshot() accessData {
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.data
	d.Grants = append([]Grant{}, a.data.Grants...)
	return d
}

// SetAccessFile keeps the accesses in path (JSON), loading those it holds.
func (s *Server) SetAccessFile(path string) error { return s.acc.load(path) }

// SetGuestRole sets the role of other devices without an access link.
func (s *Server) SetGuestRole(r Role) {
	s.acc.mu.Lock()
	s.acc.guest = r
	s.acc.mu.Unlock()
}

// SetListenAddr tells the server where it listens, to make access links.
func (s *Server) SetListenAddr(addr string) {
	s.mu.Lock()
	s.listenAddr = addr
	s.mu.Unlock()
}

// caller is who makes a request.
type caller struct {
	Role  Role
	Grant *Grant // the access link used, if any
	Local bool   // from this machine
}

// origin is the name recorded with the changes the caller makes.
func (c caller) origin() string {
	if c.Grant != nil {
		return "web " + c.Grant.Name
	}
	if c.Local {
		return console.OriginLocal
	}
	return "web guest"
}

type callerKey struct{}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) callerOf(r *http.Request) caller {
	local := isLoopback(r.RemoteAddr)
	if ck, err := r.Cookie(accessCookie); err == nil && ck.Value != "" {
		if g, ok := s.acc.byToken(ck.Value); ok {
			return caller{Role: g.Role, Grant: &g, Local: local}
		}
	}
	if local {
		return caller{Role: RoleEngineer, Local: true}
	}
	s.acc.mu.Lock()
	defer s.acc.mu.Unlock()
	return caller{Role: s.acc.guest}
}

func callerFrom(r *http.Request) caller {
	if c, ok := r.Context().Value(callerKey{}).(caller); ok {
		return c
	}
	return caller{Role: RoleNone}
}

// guard checks every API request against the role it needs.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := s.callerOf(r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			need, ok := routeRoles[r.Method+" "+r.URL.Path]
			if !ok {
				need = RoleEngineer
			}
			if !c.Role.atLeast(need) {
				httpError(w, http.StatusForbidden, fmt.Errorf("this needs %s access", need))
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, c)))
	})
}

var (
	sendAddr = regexp.MustCompile(`^/(?:ch|auxin|fxrtn)/\d\d/mix/(\d\d)/(?:on|level|pan)$`)
	busAddr  = regexp.MustCompile(`^/bus/(\d\d)/mix/(?:fader|on)$`)
)

// mixBuses returns the buses of a musician's mix: their bus, and its partner
// when the pair is linked (a stereo in-ear mix).
func mixBuses(mirror *state.Store, bus int) []int {
	if bus < 1 || bus > 16 {
		return nil
	}
	odd := bus - (bus+1)%2
	if mirror != nil {
		if v, _, err := mirror.Get(fmt.Sprintf("/config/buslink/%d-%d", odd, odd+1)); err == nil && v.Int == 1 {
			return []int{odd, odd + 1}
		}
	}
	return []int{bus}
}

// musicianMay reports whether a musician whose mix is on buses may write
// addr: the sends to those buses from channels, aux inputs and FX returns,
// and the buses' fader and mute.
func musicianMay(addr string, buses []int) bool {
	m := sendAddr.FindStringSubmatch(addr)
	if m == nil {
		m = busAddr.FindStringSubmatch(addr)
	}
	if m == nil {
		return false
	}
	n, _ := strconv.Atoi(m[1])
	for _, b := range buses {
		if b == n {
			return true
		}
	}
	return false
}

// whoView is what /api/whoami returns.
type whoView struct {
	Role              Role   `json:"role"`
	Name              string `json:"name,omitempty"`
	Bus               int    `json:"bus,omitempty"`
	Buses             []int  `json:"buses,omitempty"` // the musician's mix: bus and linked partner
	Local             bool   `json:"local"`
	Link              bool   `json:"link"` // signed in with an access link
	MusiciansCanWrite bool   `json:"musicians_can_write"`
}

func (s *Server) whoami(r *http.Request) whoView {
	c := s.callerOf(r)
	v := whoView{Role: c.Role, Local: c.Local, Link: c.Grant != nil, MusiciansCanWrite: s.acc.snapshot().MusiciansCanWrite}
	if c.Grant != nil {
		v.Name, v.Bus = c.Grant.Name, c.Grant.Bus
		if c.Role == RoleMusician {
			s.mu.Lock()
			mirror := s.mirror
			s.mu.Unlock()
			v.Buses = mixBuses(mirror, c.Grant.Bus)
		}
	}
	return v
}

func (s *Server) getWhoami(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.whoami(r))
}

// login handles an access link: it keeps the token in a cookie and opens the
// page of the access's role.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	g, ok := s.acc.byToken(r.PathValue("token"))
	if !ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `<!doctype html><meta name="viewport" content="width=device-width"><title>tabula</title>`+
			`<p style="font:16px system-ui;margin:2em">This access link is not valid any more. Ask the engineer for a new one.</p>`)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: accessCookie, Value: g.Token, Path: "/", MaxAge: 365 * 24 * 3600,
		HttpOnly: true, SameSite: http.SameSiteLaxMode})
	target := "/#/mixer"
	if g.Role == RoleMusician {
		target = "/#/mix"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: accessCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	r.Header.Del("Cookie")
	writeJSON(w, http.StatusOK, s.whoami(r))
}

// accessView is what /api/access returns.
type accessView struct {
	Grants            []Grant  `json:"grants"`
	MusiciansCanWrite bool     `json:"musicians_can_write"`
	Guest             Role     `json:"guest"`
	URLs              []string `json:"urls"`      // where other devices reach this tabula
	Reachable         bool     `json:"reachable"` // false when listening on this machine only
}

func (s *Server) accessStatus() accessView {
	d := s.acc.snapshot()
	s.acc.mu.Lock()
	guest := s.acc.guest
	s.acc.mu.Unlock()
	s.mu.Lock()
	listen := s.listenAddr
	s.mu.Unlock()
	urls, reachable := baseURLs(listen)
	return accessView{Grants: d.Grants, MusiciansCanWrite: d.MusiciansCanWrite, Guest: guest, URLs: urls, Reachable: reachable}
}

// baseURLs lists the URLs other devices can use to reach a server listening
// on addr: one per IPv4 address of this machine when it listens on all of
// them.
func baseURLs(addr string) ([]string, bool) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []string{}, false
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() || host == "localhost" {
		return []string{"http://" + net.JoinHostPort(host, port)}, false
	}
	if host != "" && (ip == nil || !ip.IsUnspecified()) {
		return []string{"http://" + net.JoinHostPort(host, port)}, true
	}
	urls := []string{}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() && !n.IP.IsLinkLocalUnicast() {
				urls = append(urls, "http://"+net.JoinHostPort(n.IP.String(), port))
			}
		}
	}
	return urls, true
}

func (s *Server) getAccess(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.accessStatus())
}

func (s *Server) addAccess(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Role string `json:"role"`
		Bus  int    `json:"bus"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"name", "role", "bus"}`))
		return
	}
	role, err := ParseRole(req.Role)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	g, err := s.acc.add(req.Name, role, req.Bus)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	s.log.Info("access added", "name", g.Name, "role", g.Role, "bus", g.Bus)
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) deleteAccess(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := s.acc.remove(id)
	switch {
	case err != nil:
		httpError(w, http.StatusInternalServerError, err)
		return
	case !ok:
		httpError(w, http.StatusNotFound, errors.New("no such access"))
		return
	}
	s.log.Info("access removed", "id", id)
	s.hub.kick(id) // close the event streams opened with it
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) accessSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MusiciansCanWrite *bool `json:"musicians_can_write"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MusiciansCanWrite == nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"musicians_can_write": true|false}`))
		return
	}
	if err := s.acc.setMusiciansCanWrite(*req.MusiciansCanWrite); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	s.hub.publish("access", map[string]bool{"musicians_can_write": *req.MusiciansCanWrite})
	writeJSON(w, http.StatusOK, s.accessStatus())
}

// accessRoutes adds the access routes; guard checks the roles.
func (s *Server) accessRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /a/{token}", s.login)
	mux.HandleFunc("GET /api/whoami", s.getWhoami)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/access", s.getAccess)
	mux.HandleFunc("POST /api/access", s.addAccess)
	mux.HandleFunc("DELETE /api/access/{id}", s.deleteAccess)
	mux.HandleFunc("POST /api/access/settings", s.accessSettings)
}
