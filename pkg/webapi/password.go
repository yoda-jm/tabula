package webapi

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The engineer password. Once set, nobody is the engineer by being on this
// machine any more (behind a reverse proxy every request comes from it):
// every browser signs in with the password, which makes a session, an
// engineer access kept like the links but listed apart. Musician, viewer and
// engineer links work as before.

// passwordIterations is the PBKDF2 cost of a new password hash (tests lower it).
var passwordIterations = 600_000

// loginDelay slows down every wrong password (tests lower it).
var loginDelay = 500 * time.Millisecond

const minPasswordLen = 8

// passwordHash is a password as the access file keeps it.
type passwordHash struct {
	Algo       string `json:"algo"` // pbkdf2-sha256
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"` // base64
	Hash       string `json:"hash"` // base64
}

func hashPassword(password string) (*passwordHash, error) {
	salt := make([]byte, 16)
	rand.Read(salt)
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if err != nil {
		return nil, err
	}
	return &passwordHash{
		Algo:       "pbkdf2-sha256",
		Iterations: passwordIterations,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Hash:       base64.StdEncoding.EncodeToString(key),
	}, nil
}

func (p *passwordHash) matches(password string) bool {
	if p == nil || p.Algo != "pbkdf2-sha256" || p.Iterations < 1 {
		return false
	}
	salt, err1 := base64.StdEncoding.DecodeString(p.Salt)
	want, err2 := base64.StdEncoding.DecodeString(p.Hash)
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, p.Iterations, len(want))
	return err == nil && subtle.ConstantTimeCompare(key, want) == 1
}

func (a *access) hasPassword() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.data.Password != nil
}

func (a *access) checkPassword(password string) bool {
	a.mu.Lock()
	p := a.data.Password
	a.mu.Unlock()
	return p.matches(password)
}

// setPassword replaces the password (nil removes it) and ends every session
// but keep; it returns the sessions ended.
func (a *access) setPassword(p *passwordHash, keep string) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data.Password = p
	var ended []string
	grants := a.data.Grants[:0:0]
	for _, g := range a.data.Grants {
		if g.Session && g.ID != keep {
			ended = append(ended, g.ID)
			continue
		}
		grants = append(grants, g)
	}
	a.data.Grants = grants
	return ended, a.saveLocked()
}

// addSession signs a device in with the password.
func (a *access) addSession(name string) (Grant, error) {
	g := Grant{
		ID:      randomString(6, hex.EncodeToString),
		Name:    name,
		Role:    RoleEngineer,
		Session: true,
		Token:   randomString(24, base64.RawURLEncoding.EncodeToString),
		Created: time.Now().UTC().Truncate(time.Second),
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data.Grants = append(a.data.Grants, g)
	return g, a.saveLocked()
}

// HasPassword reports whether an engineer password is set.
func (s *Server) HasPassword() bool { return s.acc.hasPassword() }

// ResetPassword removes the engineer password and its sessions from the
// access file: the way back in when the password is lost.
func (s *Server) ResetPassword() (bool, error) {
	if !s.acc.hasPassword() {
		return false, nil
	}
	_, err := s.acc.setPassword(nil, "")
	return true, err
}

// loginLimiter slows down password guessing: after maxFails wrong passwords
// in a row a client waits, twice as long after each further one; and all
// clients together get at most globalFails wrong passwords a minute.
type loginLimiter struct {
	mu      sync.Mutex
	clients map[string]*loginClient
	recent  []time.Time // wrong passwords of the last minute, all clients
}

type loginClient struct {
	fails int
	until time.Time
	last  time.Time
}

const (
	maxFails    = 5
	firstWait   = 30 * time.Second
	longestWait = time.Hour
	globalFails = 30
)

// wait returns how long ip must wait before trying again (0: now).
func (l *loginLimiter) wait(ip string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	var d time.Duration
	if c := l.clients[ip]; c != nil && c.until.After(now) {
		d = c.until.Sub(now)
	}
	l.recent = trimBefore(l.recent, now.Add(-time.Minute))
	if len(l.recent) >= globalFails {
		d = max(d, l.recent[0].Add(time.Minute).Sub(now))
	}
	return d
}

func trimBefore(ts []time.Time, t time.Time) []time.Time {
	i := 0
	for i < len(ts) && !ts[i].After(t) {
		i++
	}
	return ts[i:]
}

func (l *loginLimiter) failed(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.clients == nil {
		l.clients = map[string]*loginClient{}
	}
	if len(l.clients) > 10000 { // forget the clients that may try again
		for k, c := range l.clients {
			if now.Sub(c.last) > longestWait {
				delete(l.clients, k)
			}
		}
	}
	c := l.clients[ip]
	if c == nil {
		c = &loginClient{}
		l.clients[ip] = c
	}
	c.fails++
	c.last = now
	if c.fails >= maxFails {
		wait := time.Duration(float64(firstWait) * math.Pow(2, float64(c.fails-maxFails)))
		c.until = now.Add(min(wait, longestWait))
	}
	l.recent = append(l.recent, now)
}

func (l *loginLimiter) succeeded(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.clients, ip)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// tryPassword checks a password against the limiter, one at a time: a wrong
// one counts and is slowed down. It returns the wait when the client must
// not try yet.
func (s *Server) tryPassword(r *http.Request, password string) (ok bool, wait time.Duration) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	ip := clientIP(r)
	if d := s.limiter.wait(ip, time.Now()); d > 0 {
		return false, d
	}
	if s.acc.checkPassword(password) {
		s.limiter.succeeded(ip)
		return true, 0
	}
	s.limiter.failed(ip, time.Now())
	time.Sleep(loginDelay)
	return false, 0
}

func tooManyTries(w http.ResponseWriter, wait time.Duration) {
	secs := int(math.Ceil(wait.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	httpError(w, http.StatusTooManyRequests, fmt.Errorf("too many wrong passwords: try again in %s", waitText(secs)))
}

func waitText(secs int) string {
	if secs < 90 {
		return fmt.Sprintf("%d s", secs)
	}
	return fmt.Sprintf("%d min", (secs+59)/60)
}

// deviceName names a session after the browser, its system and its address.
func deviceName(r *http.Request) string {
	ua := r.UserAgent()
	browser := "A browser"
	for _, b := range []struct{ key, name string }{
		{"Edg/", "Edge"}, {"OPR/", "Opera"}, {"Firefox/", "Firefox"}, {"FxiOS/", "Firefox"},
		{"CriOS/", "Chrome"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"},
	} {
		if strings.Contains(ua, b.key) {
			browser = b.name
			break
		}
	}
	system := ""
	for _, o := range []struct{ key, name string }{
		{"Android", "Android"}, {"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Windows", "Windows"},
		{"CrOS", "ChromeOS"}, {"Mac OS X", "macOS"}, {"Linux", "Linux"},
	} {
		if strings.Contains(ua, o.key) {
			system = " on " + o.name
			break
		}
	}
	where := clientIP(r)
	if isLoopback(r.RemoteAddr) {
		where = "this machine"
	}
	return fmt.Sprintf("%s%s (%s)", browser, system, where)
}

func setAccessCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: accessCookie, Value: token, Path: "/", MaxAge: 365 * 24 * 3600,
		HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// signIn makes a session for the request's device and hands it its cookie;
// r then reads as signed in.
func (s *Server) signIn(w http.ResponseWriter, r *http.Request) error {
	g, err := s.acc.addSession(deviceName(r))
	if err != nil {
		return err
	}
	setAccessCookie(w, g.Token)
	r.Header.Del("Cookie")
	r.AddCookie(&http.Cookie{Name: accessCookie, Value: g.Token})
	s.log.Info("signed in", "device", g.Name)
	return nil
}

// postLogin signs a device in with the engineer password.
func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"password"}`))
		return
	}
	if !s.acc.hasPassword() {
		httpError(w, http.StatusConflict, errors.New("no engineer password is set"))
		return
	}
	ok, wait := s.tryPassword(r, req.Password)
	switch {
	case wait > 0:
		tooManyTries(w, wait)
		return
	case !ok:
		s.log.Warn("wrong engineer password", "from", clientIP(r))
		httpError(w, http.StatusForbidden, errors.New("wrong password"))
		return
	}
	if err := s.signIn(w, r); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.whoami(r))
}

// setPassword sets, changes or removes (empty password) the engineer
// password. Every other session ends; the engineer setting the first one on
// this machine is signed in on the way.
func (s *Server) setPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"current", "password"}`))
		return
	}
	had := s.acc.hasPassword()
	if req.Password == "" && !had {
		writeJSON(w, http.StatusOK, s.accessStatus(r))
		return
	}
	if req.Password != "" {
		if len([]rune(req.Password)) < minPasswordLen {
			httpError(w, http.StatusBadRequest, fmt.Errorf("the password needs at least %d characters", minPasswordLen))
			return
		}
		s.acc.mu.Lock()
		guest := s.acc.guest
		s.acc.mu.Unlock()
		if guest == RoleEngineer {
			httpError(w, http.StatusConflict, errors.New("every device is the engineer (-guest engineer): a password would protect nothing"))
			return
		}
	}
	if had {
		ok, wait := s.tryPassword(r, req.Current)
		switch {
		case wait > 0:
			tooManyTries(w, wait)
			return
		case !ok:
			httpError(w, http.StatusForbidden, errors.New("the current password is wrong"))
			return
		}
	}
	var p *passwordHash
	if req.Password != "" {
		var err error
		if p, err = hashPassword(req.Password); err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
	}
	c := callerFrom(r)
	keep := ""
	if c.Grant != nil && c.Grant.Session {
		keep = c.Grant.ID
	}
	ended, err := s.acc.setPassword(p, keep)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	for _, id := range ended {
		s.hub.kick(id)
	}
	if !had {
		s.hub.kick("") // pages open without signing in stop seeing the console
		if c.Grant == nil {
			if err := s.signIn(w, r); err != nil {
				httpError(w, http.StatusInternalServerError, err)
				return
			}
		}
	}
	s.log.Info("engineer password", "set", p != nil, "sessions ended", len(ended))
	s.hub.publish("access", map[string]bool{"password": p != nil})
	writeJSON(w, http.StatusOK, s.accessStatus(r))
}
