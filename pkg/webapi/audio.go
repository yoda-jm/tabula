package webapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/session"
)

// Audio tracks recorded with the sessions (pkg/audio): the configuration,
// the capture running with a recording, a test of the input, and the
// exports of a session for audio editors. These routes are the engineer's
// (access.go); the record status carries a summary of the capture.

type audioState struct {
	cfg     audio.Config
	file    string         // where changes are saved; "" keeps them in memory
	cap     *audio.Capture // running with the recording
	last    *audio.Status  // the capture of the last recording, once over
	saved   string         // capture state last written to meta.json
	savedAt time.Time
	testing bool
	source  func() (io.ReadCloser, error) // tests: replaces the recorder
}

func (s *Server) audioRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/audio", s.getAudio)
	mux.HandleFunc("POST /api/audio/config", s.setAudioConfig)
	mux.HandleFunc("GET /api/audio/devices", s.audioDevices)
	mux.HandleFunc("POST /api/audio/test", s.audioTest)
	mux.HandleFunc("GET /api/sessions/{name}/export", s.exportSession)
}

// SetAudioConfig sets the audio configuration; file is where changes made
// through the API are saved ("" keeps them in memory).
func (s *Server) SetAudioConfig(cfg audio.Config, file string) {
	cfg = cfg.Normalized()
	s.mu.Lock()
	s.audio.cfg, s.audio.file = cfg, file
	s.mu.Unlock()
	if args, err := cfg.CommandLine(); err == nil && cfg.Enabled {
		s.log.Info("audio recorded with every session", "command", audio.QuoteArgs(args), "tracks", cfg.Tracks)
	}
}

// startAudioLocked starts the audio capture of a new recording, if audio
// is enabled. A capture that cannot start is reported; the recording goes
// on without it.
func (s *Server) startAudioLocked(rec *session.Recorder) {
	a := &s.audio
	a.last, a.saved = nil, ""
	cfg := a.cfg.Normalized()
	if !cfg.Enabled {
		return
	}
	opt := audio.Options{Ref: rec.Started(), Log: s.log, Source: a.source}
	if s.mirror != nil {
		opt.Names = audio.TrackNames(s.mirror)
	}
	c, err := audio.Start(cfg, filepath.Join(rec.Dir(), "audio"), opt)
	if err != nil {
		s.log.Error("audio not recorded", "err", err)
		st := audio.Status{State: audio.Failed, Info: audio.Info{Rate: cfg.Rate, Bits: cfg.Bits, Channels: cfg.Channels,
			Device: cfg.Device, Error: "audio not recorded: " + err.Error()}, Free: -1, Remaining: -1}
		a.last = &st
		rec.SetAudio(&st.Info)
		return
	}
	a.cap, a.saved, a.savedAt = c, audio.Starting, time.Now()
	info := c.Info()
	rec.SetAudio(&info)
}

// tickAudio keeps the session's meta.json up to date with the capture:
// when its state changes, and every 30 seconds.
func (s *Server) tickAudio(rec *session.Recorder) {
	s.mu.Lock()
	c := s.audio.cap
	if c == nil || s.rec != rec {
		s.mu.Unlock()
		return
	}
	st := c.Status()
	due := st.State != s.audio.saved || time.Since(s.audio.savedAt) > 30*time.Second
	if due {
		s.audio.saved, s.audio.savedAt = st.State, time.Now()
	}
	s.mu.Unlock()
	if due {
		rec.SetAudio(&st.Info)
	}
}

// endRecording stops a recording detached from the server, and its audio
// capture first, so that meta.json ends with the final audio section. With
// audio, the Reaper project and the label track are written in the session.
func (s *Server) endRecording(rec *session.Recorder, c *audio.Capture) (*audio.Status, error) {
	var last *audio.Status
	if c != nil {
		st := c.Stop()
		last = &st
		rec.SetAudio(&st.Info)
	}
	err := rec.Stop()
	if last != nil && len(last.Tracks) > 0 {
		ses, oerr := session.Open(rec.Dir())
		if oerr == nil {
			oerr = ses.WriteExports()
		}
		if oerr != nil {
			s.log.Error("session exports", "err", oerr)
		}
	}
	return last, err
}

// endRecordingLocked ends the recording while s.mu is held (on disconnect).
func (s *Server) endRecordingLocked() {
	if s.rec == nil {
		return
	}
	last, _ := s.endRecording(s.rec, s.audio.cap)
	s.rec, s.audio.cap = nil, nil
	if last != nil {
		s.audio.last = last
	}
}

// audioStatusLocked is the status of the capture running, or of the last
// one; nil without audio.
func (s *Server) audioStatusLocked() *audio.Status {
	if s.audio.cap != nil {
		st := s.audio.cap.Status()
		return &st
	}
	return s.audio.last
}

// publicAudio is the capture summary of the record status, which viewers
// see too: without the device and the command line.
func publicAudio(st *audio.Status) *audio.Status {
	if st == nil {
		return nil
	}
	p := *st
	p.Device, p.Command = "", ""
	return &p
}

type audioView struct {
	Config audio.Config `json:"config"`
	// Command is the command line of the configuration; Error what is
	// wrong with it.
	Command   string            `json:"command,omitempty"`
	Error     string            `json:"error,omitempty"`
	Templates map[string]string `json:"templates"` // command line of each recorder
	ByteRate  int64             `json:"byte_rate"`
	Dir       string            `json:"dir"`       // where sessions are recorded
	Free      int64             `json:"free"`      // bytes free there, -1 if unknown
	Remaining float64           `json:"remaining"` // seconds of audio that fit there, -1 if unknown
	Status    *audio.Status     `json:"status,omitempty"`
	Testing   bool              `json:"testing"`
}

func (s *Server) audioView() audioView {
	s.mu.Lock()
	cfg, dir, st, testing := s.audio.cfg.Normalized(), s.sessionsDir, s.audioStatusLocked(), s.audio.testing
	s.mu.Unlock()
	v := audioView{Config: cfg, Templates: map[string]string{}, ByteRate: cfg.ByteRate(), Free: -1, Remaining: -1,
		Status: st, Testing: testing}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	v.Dir = dir
	for _, r := range audio.Recorders {
		if t := audio.Template(r, runtime.GOOS); t != "" {
			v.Templates[r] = t
		}
	}
	if err := cfg.Validate(); err != nil {
		v.Error = err.Error()
	} else if args, err := cfg.CommandLine(); err != nil {
		v.Error = err.Error()
	} else {
		v.Command = audio.QuoteArgs(args)
	}
	if free, err := freeSpace(dir); err == nil {
		v.Free = int64(free)
		if v.ByteRate > 0 {
			v.Remaining = max(0, float64(v.Free-audio.Reserve(v.ByteRate))/float64(v.ByteRate))
		}
	}
	return v
}

// freeSpace is the free space where dir is or will be (its first existing
// parent).
func freeSpace(dir string) (uint64, error) {
	for {
		if _, err := os.Stat(dir); err == nil {
			return audio.FreeSpace(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return 0, errors.New("no such directory")
		}
		dir = parent
	}
}

func (s *Server) getAudio(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.audioView())
}

// setAudioConfig replaces the configuration, used from the next recording.
func (s *Server) setAudioConfig(w http.ResponseWriter, r *http.Request) {
	var cfg audio.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	cfg = cfg.Normalized()
	if err := cfg.Validate(); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	s.audio.cfg = cfg
	file := s.audio.file
	s.mu.Unlock()
	if file != "" {
		if err := audio.SaveConfig(file, cfg); err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, s.audioView())
}

func (s *Server) audioDevices(w http.ResponseWriter, r *http.Request) {
	devs := audio.ListDevices(r.Context())
	if devs == nil {
		devs = []audio.Device{}
	}
	writeJSON(w, http.StatusOK, devs)
}

// audioTest records a few seconds without writing anything: the stream
// format, the levels of the tracks, or why it does not work. The body may
// carry a configuration to try (default: the current one).
func (s *Server) audioTest(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.audio.cfg
	busy := s.audio.cap != nil || s.audio.testing
	source, mirror := s.audio.source, s.mirror
	if !busy {
		s.audio.testing = true
	}
	s.mu.Unlock()
	if busy {
		httpError(w, http.StatusConflict, errors.New("the audio input is in use (recording or testing)"))
		return
	}
	defer func() {
		s.mu.Lock()
		s.audio.testing = false
		s.mu.Unlock()
	}()
	if body, _ := io.ReadAll(r.Body); len(body) > 0 {
		if err := json.Unmarshal(body, &cfg); err != nil {
			httpError(w, http.StatusBadRequest, err)
			return
		}
	}
	opt := audio.Options{Log: s.log, Source: source}
	if mirror != nil {
		opt.Names = audio.TrackNames(mirror)
	}
	c, err := audio.Start(cfg, "", opt)
	if err != nil {
		httpError(w, http.StatusBadRequest, fmt.Errorf("audio: %w", err))
		return
	}
	select {
	case <-c.Done():
	case <-time.After(testFor):
	case <-r.Context().Done():
	}
	writeJSON(w, http.StatusOK, c.Stop())
}

// testFor is how long a test of the input lasts.
var testFor = 3 * time.Second

// exportSession serves an export of a session:
// ?format=reaper (project), audacity (label track) or csv.
func (s *Server) exportSession(w http.ResponseWriter, r *http.Request) {
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
	format := r.URL.Query().Get("format")
	name, err := ses.ExportFile(format)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	ctype := "text/plain; charset=utf-8"
	switch format {
	case session.ExportAudacity:
		name = ses.Name() + " labels.txt"
	case session.ExportCSV:
		name, ctype = ses.Name()+".csv", "text/csv; charset=utf-8"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	ses.Export(w, format)
}
