package webapi

import (
	"net/http"
	"path/filepath"
	"time"
)

// SetBuild tells which tabula this is, for the GUI: its version and the
// file its log goes to ("" for the terminal only).
func (s *Server) SetBuild(version, logFile string) {
	s.mu.Lock()
	s.version, s.logFile = version, logFile
	s.mu.Unlock()
}

// Disconnect lets the console go (at exit).
func (s *Server) Disconnect() {
	s.mu.Lock()
	s.closeLocked()
	s.mu.Unlock()
}

// aboutView is what GET /api/about returns: which tabula runs, since when,
// and where it keeps its files (absolute paths), for the engineer.
type aboutView struct {
	Version  string    `json:"version"`
	Started  time.Time `json:"started"`
	Log      string    `json:"log"`
	Sessions string    `json:"sessions"`
	Scenes   string    `json:"scenes"`
	Access   string    `json:"access"`
	Listen   string    `json:"listen"`
}

func (s *Server) getAbout(w http.ResponseWriter, r *http.Request) {
	abs := func(p string) string {
		if p == "" {
			return ""
		}
		if a, err := filepath.Abs(p); err == nil {
			return a
		}
		return p
	}
	s.acc.mu.Lock()
	access := s.acc.path
	s.acc.mu.Unlock()
	s.mu.Lock()
	v := aboutView{
		Version:  s.version,
		Started:  s.started,
		Log:      s.logFile,
		Sessions: abs(s.sessionsDir),
		Scenes:   abs(s.scenesDir),
		Access:   abs(access),
		Listen:   s.listenAddr,
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, v)
}
