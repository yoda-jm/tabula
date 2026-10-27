package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	// ReplayAudio: the replay plays the session's tracks to the card too (a
	// virtual soundcheck, soundcheck.go)
	ReplayAudio bool          `json:"replay_audio,omitempty"`
	Error       string        `json:"error,omitempty"`
	// Waiting: console changes waiting for the disk for more than
	// slowDisk (a disk that hangs: they are written once it takes them)
	Waiting int           `json:"waiting,omitempty"`
	Audio   *audio.Status `json:"audio,omitempty"` // the audio capture of the recording, or of the last one
	USB         usbBackupView `json:"usb"`             // the backup on the console's USB stick (usbbackup.go)
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
	v := recordView{Replaying: s.replayCancel != nil, ReplayOf: s.replayOf, ReplayAt: s.replayAt, ReplayAudio: s.sc.on,
		Error: s.recordErr, Audio: publicAudio(s.audioStatusLocked()), USB: s.backup.view}
	v.USB.On = s.audio.cfg.USBBackup
	if s.rec != nil {
		m := s.rec.Meta()
		v.Recording, v.Name, v.Meta = true, filepath.Base(s.rec.Dir()), &m
		if n, since := s.rec.Backlog(); n > 0 && time.Since(since) > slowDisk {
			v.Waiting = n
		}
	}
	return v
}

// slowDisk is how long changes wait for the disk before the Record page says so.
const slowDisk = 2 * time.Second

func (s *Server) publishRecord() { s.hub.publish("record", s.recordStatus()) }

// StartRecording records the connected console into a new session. It
// waits for the card to be free first: a recording started during an
// input test would find it busy, and be recorded without its audio. The
// files are created and the recorder started without s.mu: every other
// request goes on meanwhile; the recording shows once it runs.
func (s *Server) StartRecording(note string) error {
	s.mu.Lock()
	busy := s.rec != nil || s.recStarting != nil
	s.mu.Unlock()
	if busy { // at once, rather than after the start being made
		return errors.New("already recording")
	}
	if err := s.waitAudioFree(); err != nil {
		return err
	}
	s.audio.probeMu.Lock() // see audioState.probeMu
	defer s.audio.probeMu.Unlock()
	s.mu.Lock()
	switch {
	case s.mirror == nil || s.connecting:
		s.mu.Unlock()
		return errors.New("not connected (or still syncing)")
	case s.rec != nil || s.recStarting != nil:
		s.mu.Unlock()
		return errors.New("already recording")
	case s.audio.testing: // started again since waitAudioFree
		s.mu.Unlock()
		return errors.New("the audio input is being tested: try again in a few seconds")
	}
	st := s.client.Status()
	mirror, dir, consoleName := s.mirror, s.sessionsDir, strings.TrimSpace(st.Info.Model+" "+st.Info.Name)
	cfg, source := s.audio.cfg.Normalized(), s.audio.source
	starting := make(chan struct{})
	s.recStarting = starting
	s.mu.Unlock()
	defer close(starting)

	name := time.Now().Format("2006-01-02_150405")
	// stopped and started again within the second: 2026-10-26_203512-2
	for i, base := 2, name; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name, "meta.json")); err != nil {
			break
		}
		name = base + "-" + strconv.Itoa(i)
	}
	rec, err := session.Start(filepath.Join(dir, name), mirror, consoleName, note)
	var c *audio.Capture
	var failed *audio.Status
	if err == nil && cfg.Enabled {
		c, failed = s.startCapture(cfg, rec, mirror, source)
	}

	s.mu.Lock()
	s.recStarting = nil
	switch {
	case err != nil:
		s.mu.Unlock()
		return err
	case s.mirror != mirror: // disconnected meanwhile
		s.mu.Unlock()
		s.endRecording(rec, c)
		return errors.New("disconnected while the recording started")
	}
	s.rec, s.recordErr = rec, ""
	s.log.Info("recording", "session", rec.Dir())
	s.useCaptureLocked(c, failed)
	s.startUSBBackupLocked(rec)
	s.mu.Unlock()
	s.noteCapture(rec, cfg, c, failed)
	go s.tickRecord(rec)
	return nil
}

// tickRecord publishes the recording status every second.
func (s *Server) tickRecord(rec *session.Recorder) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var failed error
	for range t.C {
		s.mu.Lock()
		current := s.rec == rec
		if err := rec.Err(); current && err != nil && failed == nil {
			failed = err
			s.recordErr = "the console's changes cannot be written (" + err.Error() + "): those after it are lost"
			s.log.Error("recording: the changes cannot be written", "session", rec.Dir(), "err", err)
		}
		s.mu.Unlock()
		if !current {
			return
		}
		s.tickAudio(rec)
		s.publishRecord()
	}
}

// StopRecording ends the current recording, if any, and its audio; it
// also waits for a recording ended on a disconnect to be finished (tabula
// stopping).
func (s *Server) StopRecording() error {
	s.mu.Lock()
	if starting := s.recStarting; starting != nil { // stop it once it runs
		s.mu.Unlock()
		<-starting
		s.mu.Lock()
	}
	rec, c, ending := s.rec, s.audio.cap, s.audio.ending
	s.rec, s.audio.cap = nil, nil
	if rec != nil {
		s.stopUSBBackupLocked()
	}
	s.mu.Unlock()
	if ending != nil {
		<-ending
	}
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

// recordMark adds a marker: {"label": "Song 2"}, or the next song of the
// setlist ({"next": true}) or one of its songs ({"song": 2}, from 0); with
// none of them, a timed marker (setlist.go). It answers the record status
// and the marker added.
func (s *Server) recordMark(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label string `json:"label"`
		Next  bool   `json:"next"`
		Song  *int   `json:"song"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	s.mu.Lock()
	rec := s.rec
	s.mu.Unlock()
	if rec == nil {
		httpError(w, http.StatusConflict, errors.New("not recording"))
		return
	}
	m, err := s.addMarker(rec, strings.TrimSpace(req.Label), req.Next, req.Song)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	s.publishRecord()
	writeJSON(w, http.StatusOK, struct {
		recordView
		Marked markView `json:"marked"`
	}{s.recordStatus(), m})
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
	// Damaged lists the lines of the change log that could not be read:
	// their changes are missing from the session.
	Damaged []int `json:"damaged,omitempty"`
	// Size is the bytes of the session's folder; FLACSize about what it
	// takes with its tracks in FLAC (takehome.go), when it has WAV tracks.
	Size     int64 `json:"size,omitempty"`
	FLACSize int64 `json:"flac_size,omitempty"`
	// Setlist is the songs the session was recorded with (setlist.go)
	Setlist []string `json:"setlist,omitempty"`
}

// openSession opens a session, logging the lines of its change log that
// could not be read (their changes are skipped).
func (s *Server) openSession(dir string) (*session.Session, error) {
	ses, err := session.Open(dir)
	if err == nil && len(ses.Damaged) > 0 {
		s.log.Error("session: lines of events.jsonl could not be read, their changes are skipped",
			"session", filepath.Base(dir), "lines", audio.FormatTracks(ses.Damaged))
	}
	return ses, err
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
	ses, err := s.openSession(dir)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	_, err = os.Stat(filepath.Join(dir, "start.scn"))
	v := sessionView{Name: filepath.Base(dir), Meta: ses.Meta, Markers: ses.Markers(),
		Snapshot: len(ses.Start), Scene: err == nil, Damaged: ses.Damaged, Setlist: sessionSetlist(dir)}
	if files, err := ses.HomeFiles(false); err == nil {
		v.Size, _ = session.HomeSize(files)
	}
	if files, err := ses.HomeFiles(true); err == nil {
		if size, exact := session.HomeSize(files); !exact {
			v.FLACSize = size
		}
	}
	writeJSON(w, http.StatusOK, v)
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
// armed): {"session": "2026-10-17_203000", "from": 0, "speed": 1}. With
// "audio", its tracks play to the card too, a virtual soundcheck
// (soundcheck.go); with "routing" as well, the console's inputs are
// switched to the card for the time of it.
func (s *Server) replayStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Session string  `json:"session"`
		From    float64 `json:"from"`
		Speed   float64 `json:"speed"`
		Audio   bool    `json:"audio"`
		Routing bool    `json:"routing"`
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
	ses, err := s.openSession(dir)
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
	var sc *soundcheck
	if req.Audio {
		var code int
		if sc, code, err = s.newSoundcheck(c, ses, req.Speed); err != nil {
			httpError(w, code, err)
			return
		}
	}
	s.backupBefore("replaying " + req.Session)
	if sc != nil && req.Routing {
		if err := sc.toCard(); err != nil {
			httpError(w, http.StatusBadGateway, err)
			return
		}
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
	if sc != nil {
		sc.from, sc.cancel = req.From, cancel
		p.Skip, p.OnReady = routingSwitch, sc.start
	}
	go func() {
		err := p.Play(ctx, s.model, req.From)
		if sc != nil {
			err = sc.finish(ctx, err)
		}
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
