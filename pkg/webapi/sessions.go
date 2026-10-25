package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/session"
)

// Recording and replay of shows. Sessions live in subdirectories of the
// sessions directory, named by their start time.

type recordView struct {
	Recording bool          `json:"recording"`
	Name      string        `json:"name,omitempty"`
	Meta      *session.Meta `json:"meta,omitempty"`
	Replaying bool          `json:"replaying"`
	ReplayOf  string        `json:"replay_of,omitempty"`
	ReplayAt  float64       `json:"replay_at,omitempty"`
	Error     string        `json:"error,omitempty"`
	Audio     *audio.Status `json:"audio,omitempty"` // the audio capture of the recording, or of the last one
}

// SetSessionsDir sets where sessions are kept.
func (s *Server) SetSessionsDir(dir string) {
	s.mu.Lock()
	s.sessionsDir = dir
	s.mu.Unlock()
}

func (s *Server) recordStatus() recordView {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := recordView{Replaying: s.replayCancel != nil, ReplayOf: s.replayOf, ReplayAt: s.replayAt, Error: s.recordErr,
		Audio: publicAudio(s.audioStatusLocked())}
	if s.rec != nil {
		m := s.rec.Meta()
		v.Recording, v.Name, v.Meta = true, filepath.Base(s.rec.Dir()), &m
	}
	return v
}

func (s *Server) publishRecord() { s.hub.publish("record", s.recordStatus()) }

// StartRecording records the connected console into a new session.
func (s *Server) StartRecording(note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mirror == nil || s.connecting {
		return errors.New("not connected (or still syncing)")
	}
	if s.rec != nil {
		return errors.New("already recording")
	}
	name := time.Now().Format("2006-01-02_150405")
	st := s.client.Status()
	rec, err := session.Start(filepath.Join(s.sessionsDir, name), s.mirror, strings.TrimSpace(st.Info.Model+" "+st.Info.Name), note)
	if err != nil {
		return err
	}
	s.rec, s.recordErr = rec, ""
	s.log.Info("recording", "session", rec.Dir())
	s.startAudioLocked(rec)
	go s.tickRecord(rec)
	return nil
}

// tickRecord publishes the recording status every second.
func (s *Server) tickRecord(rec *session.Recorder) {
	for range time.Tick(time.Second) {
		s.mu.Lock()
		current := s.rec == rec
		s.mu.Unlock()
		if !current {
			return
		}
		s.tickAudio(rec)
		s.publishRecord()
	}
}

// StopRecording ends the current recording, if any, and its audio.
func (s *Server) StopRecording() error {
	s.mu.Lock()
	rec, c := s.rec, s.audio.cap
	s.rec, s.audio.cap = nil, nil
	s.mu.Unlock()
	if rec == nil {
		return nil
	}
	s.log.Info("recording stopped", "session", rec.Dir())
	last, err := s.endRecording(rec, c)
	if last != nil {
		s.mu.Lock()
		s.audio.last = last
		s.mu.Unlock()
	}
	return err
}

func (s *Server) recordStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Note string `json:"note"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if err := s.StartRecording(req.Note); err != nil {
		httpError(w, http.StatusConflict, err)
		return
	}
	s.waitAudioStart() // the answer says whether the audio runs
	s.publishRecord()
	writeJSON(w, http.StatusOK, s.recordStatus())
}

func (s *Server) recordStop(w http.ResponseWriter, r *http.Request) {
	if err := s.StopRecording(); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	s.publishRecord()
	writeJSON(w, http.StatusOK, s.recordStatus())
}

func (s *Server) recordMark(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label string `json:"label"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	s.mu.Lock()
	rec := s.rec
	s.mu.Unlock()
	if rec == nil {
		httpError(w, http.StatusConflict, errors.New("not recording"))
		return
	}
	if req.Label == "" {
		req.Label = "Marker " + rec.Elapsed().Round(time.Second).String()
	}
	rec.Mark(req.Label)
	s.publishRecord()
	writeJSON(w, http.StatusOK, s.recordStatus())
}

func (s *Server) getRecord(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.recordStatus())
}

type sessionView struct {
	Name    string          `json:"name"`
	Meta    session.Meta    `json:"meta"`
	Markers []session.Event `json:"markers,omitempty"`
	// Snapshot is the number of node lines of the console at the start
	// (start.scn and start.nodes); Scene whether start.scn is there.
	Snapshot int  `json:"snapshot,omitempty"`
	Scene    bool `json:"scene,omitempty"`
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	root := s.sessionsDir
	s.mu.Unlock()
	metas, dirs, err := session.List(root)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	out := []sessionView{}
	for i, m := range metas {
		out = append(out, sessionView{Name: filepath.Base(dirs[i]), Meta: m})
	}
	writeJSON(w, http.StatusOK, out)
}

// sessionDir resolves a session name inside the sessions directory.
func (s *Server) sessionDir(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return "", errors.New("bad session name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.sessionsDir, name)
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		return "", errors.New("no such session")
	}
	return dir, nil
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	dir, err := s.sessionDir(r.PathValue("name"))
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	ses, err := session.Open(dir)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	_, err = os.Stat(filepath.Join(dir, "start.scn"))
	writeJSON(w, http.StatusOK, sessionView{Name: filepath.Base(dir), Meta: ses.Meta, Markers: ses.Markers(),
		Snapshot: len(ses.Start), Scene: err == nil})
}

// deleteSession removes a session and its audio files, unless it is being
// recorded or replayed.
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dir, err := s.sessionDir(name)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	s.mu.Lock()
	busy := s.rec != nil && filepath.Base(s.rec.Dir()) == name || s.replayCancel != nil && s.replayOf == name
	s.mu.Unlock()
	if busy {
		httpError(w, http.StatusConflict, errors.New("this session is being recorded or replayed"))
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info("session deleted", "session", dir)
	w.WriteHeader(http.StatusNoContent)
}

// replayStart replays a session onto the connected console (writes must be
// armed): {"session": "2026-10-17_203000", "from": 0, "speed": 1}.
func (s *Server) replayStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Session string  `json:"session"`
		From    float64 `json:"from"`
		Speed   float64 `json:"speed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	dir, err := s.sessionDir(req.Session)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	ses, err := session.Open(dir)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	s.mu.Lock()
	c := s.client
	busy := s.replayCancel != nil
	s.mu.Unlock()
	switch {
	case c == nil:
		httpError(w, http.StatusConflict, errors.New("not connected"))
		return
	case !c.Status().Armed:
		httpError(w, http.StatusForbidden, errors.New("writes are not armed: a replay writes to the console"))
		return
	case busy:
		httpError(w, http.StatusConflict, errors.New("already replaying"))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.replayCancel, s.replayOf, s.replayAt, s.recordErr = cancel, req.Session, req.From, ""
	s.mu.Unlock()
	last := time.Time{}
	p := &session.Player{S: ses, Target: c, Speed: req.Speed, OnProgress: func(t float64) {
		s.mu.Lock()
		s.replayAt = t
		s.mu.Unlock()
		if time.Since(last) > 250*time.Millisecond {
			last = time.Now()
			s.publishRecord()
		}
	}}
	go func() {
		err := p.Play(ctx, s.model, req.From)
		s.mu.Lock()
		s.replayCancel, s.replayOf = nil, ""
		if err != nil && !errors.Is(err, context.Canceled) {
			s.recordErr = err.Error()
		}
		s.mu.Unlock()
		s.publishRecord()
	}()
	s.publishRecord()
	writeJSON(w, http.StatusAccepted, s.recordStatus())
}

func (s *Server) replayStop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.replayCancel != nil {
		s.replayCancel()
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.recordStatus())
}
