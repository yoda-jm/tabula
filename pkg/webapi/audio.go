package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
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
	// ending is closed once the recording ended on a disconnect has its
	// files finished (in the background): until then its capture may hold
	// the card, so a recording or a test starting waits for it
	ending chan struct{}
	// problems of the device of the running capture (audio.CheckDevice),
	// shown as warnings of its status
	problems []string
	devices  func(context.Context) []audio.Device // tests: replaces audio.ListDevices
	// probeMu is held while the cards are probed (audio.ProbeChannels),
	// taken before Server.mu: a capture or a test starting meanwhile could
	// find its card busy, so they wait for the probe to end.
	probeMu sync.Mutex
}

func (s *Server) audioRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/audio", s.getAudio)
	mux.HandleFunc("POST /api/audio/config", s.setAudioConfig)
	mux.HandleFunc("GET /api/audio/devices", s.audioDevices)
	mux.HandleFunc("POST /api/audio/test", s.audioTest)
	mux.HandleFunc("POST /api/audio/restart", s.restartAudio)
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
	a.last, a.saved, a.problems = nil, "", nil
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
		msg := "audio not recorded: " + err.Error()
		if h := audio.Hint(msg); h != "" {
			msg += ". " + h
		}
		st := audio.Status{State: audio.Failed, Info: audio.Info{Rate: cfg.Rate, Bits: cfg.Bits, Channels: cfg.Channels,
			Device: cfg.Device, Error: msg}, Free: -1, Remaining: -1}
		a.last = &st
		rec.SetAudio(&st.Info)
		return
	}
	a.cap, a.saved, a.savedAt = c, audio.Starting, time.Now()
	info := c.Info()
	rec.SetAudio(&info)
	go func() { // a device that is not the console's card records, but not the show
		problems := audio.CheckDevice(cfg, s.listDevices(context.Background(), false), runtime.GOOS).Problems
		for _, p := range problems {
			s.log.Warn("audio device", "problem", p)
		}
		s.mu.Lock()
		if a.cap == c {
			a.problems = problems
		}
		s.mu.Unlock()
	}()
}

// listDevices lists the capture devices (audio.ListDevices). probe asks
// their channels to the cards that do not tell them (audio.ProbeChannels),
// unless the audio input is in use: recording or testing.
func (s *Server) listDevices(ctx context.Context, probe bool) []audio.Device {
	if probe {
		s.audio.probeMu.Lock()
		defer s.audio.probeMu.Unlock()
	}
	s.mu.Lock()
	list := s.audio.devices
	busy := s.audio.testing || s.audio.cap != nil && s.audio.cap.Status().State != audio.Failed
	s.mu.Unlock()
	if list != nil {
		return list(ctx)
	}
	devs := audio.ListDevices(ctx)
	if probe && !busy {
		probeCards(ctx, devs)
	}
	return devs
}

// probeCards opens the cards; the tests leave this machine's alone.
var probeCards = audio.ProbeChannels

// audioStartWait is how long starting a recording waits for its audio to
// deliver samples or fail, so that the answer says whether it runs.
var audioStartWait = 3 * time.Second

// waitAudioStart waits until the audio of the recording runs or has
// failed (at most audioStartWait) and writes its state in meta.json.
func (s *Server) waitAudioStart() {
	s.mu.Lock()
	rec, c := s.rec, s.audio.cap
	s.mu.Unlock()
	if rec == nil || c == nil {
		return
	}
	for deadline := time.Now().Add(audioStartWait); c.Status().State == audio.Starting && time.Now().Before(deadline); {
		select {
		case <-c.Done():
		case <-time.After(20 * time.Millisecond):
		}
	}
	s.tickAudio(rec)
}

// restartAudio starts the audio of the recording again, with the current
// configuration, after it failed before recording anything (a wrong
// device, the card not plugged in): in the same session, the first sample
// timed from its start as always. Audio already recorded is never
// replaced: then a new recording is the way to record more.
func (s *Server) restartAudio(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	rec, old, st := s.rec, s.audio.cap, s.audioStatusLocked()
	enabled, testing := s.audio.cfg.Enabled, s.audio.testing
	s.mu.Unlock()
	switch {
	case rec == nil:
		httpError(w, http.StatusConflict, errors.New("not recording"))
		return
	case testing:
		httpError(w, http.StatusConflict, errors.New("the input is being tested"))
		return
	case !enabled:
		httpError(w, http.StatusBadRequest, errors.New("audio tracks are off: turn them on first"))
		return
	case st != nil && st.State != audio.Failed && st.State != audio.Stopped:
		httpError(w, http.StatusConflict, errors.New("the audio is being recorded"))
		return
	case st != nil && st.Frames > 0:
		httpError(w, http.StatusConflict, errors.New("this session has audio already: stop it and start a new recording to record more"))
		return
	}
	if old != nil {
		old.Stop() // over already: waits for its files to be finished
	}
	s.audio.probeMu.Lock()
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		s.audio.probeMu.Unlock()
		s.waitAudioStart()
		s.publishRecord()
		writeJSON(w, http.StatusOK, s.recordStatus())
	}()
	if s.rec != rec || s.audio.cap != old {
		return // stopped or restarted meanwhile: say how it is
	}
	s.audio.cap = nil
	if st != nil {
		for _, t := range st.Tracks { // headers without samples
			if t.File != "" {
				os.Remove(filepath.Join(rec.Dir(), "audio", path.Base(t.File)))
			}
		}
	}
	s.log.Info("audio restarted", "session", rec.Dir())
	s.startAudioLocked(rec)
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
		ses, oerr := s.openSession(rec.Dir())
		if oerr == nil {
			oerr = ses.WriteExports()
		}
		if oerr != nil {
			s.log.Error("session exports", "err", oerr)
		}
	}
	return last, err
}

// endRecordingLocked ends the recording while s.mu is held (on disconnect,
// or connecting to another console). Its files are finished in the
// background, as that takes seconds and every request needs s.mu; see
// audioState.ending.
func (s *Server) endRecordingLocked() {
	if s.rec == nil {
		return
	}
	rec, c, prev := s.rec, s.audio.cap, s.audio.ending
	s.rec, s.audio.cap = nil, nil
	s.stopUSBBackupLocked() // before the console link closes
	done := make(chan struct{})
	s.audio.ending = done
	go func() {
		defer close(done)
		if prev != nil {
			<-prev
		}
		s.log.Info("recording stopped", "session", rec.Dir())
		last, err := s.endRecording(rec, c)
		if err != nil {
			s.log.Error("recording", "err", err)
		}
		if last != nil {
			s.mu.Lock()
			s.audio.last = last
			s.mu.Unlock()
		}
		s.publishRecord()
	}()
}

// waitAudioFree waits for the card to be free (a recording ended on a
// disconnect being finished, an input test running), 5 s at most.
func (s *Server) waitAudioFree() error {
	deadline := time.Now().Add(audioFreeWait)
	for {
		s.mu.Lock()
		ending, testing := s.audio.ending, s.audio.testing
		s.mu.Unlock()
		if ending != nil {
			select {
			case <-ending:
			case <-time.After(time.Until(deadline)):
				return errors.New("the last recording is still being finished: try again in a few seconds")
			}
		}
		if !testing {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("the audio input is being tested: try again in a few seconds")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// audioFreeWait is how long a recording or a test waits for the card.
var audioFreeWait = 5 * time.Second

// audioStatusLocked is the status of the capture running, or of the last
// one; nil without audio.
func (s *Server) audioStatusLocked() *audio.Status {
	if s.audio.cap != nil {
		st := s.audio.cap.Status()
		if len(s.audio.problems) > 0 {
			st.Warnings = append(slices.Clone(s.audio.problems), st.Warnings...)
		}
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
	// Names is what the console's routing sends to each track of its card
	// ("" when nothing named), while connected: to choose the tracks.
	Names []string `json:"names,omitempty"`
	// Check says whether the device configured is there and is the
	// console's card, when audio is on.
	Check *audio.Check `json:"check,omitempty"`
}

func (s *Server) audioView() audioView {
	s.mu.Lock()
	cfg, dir, st, testing := s.audio.cfg.Normalized(), s.sessionsDir, s.audioStatusLocked(), s.audio.testing
	mirror := s.mirror
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
	if mirror != nil {
		v.Names = audio.TrackNames(mirror)
	}
	if cfg.Enabled {
		if c := audio.CheckDevice(cfg, s.listDevices(context.Background(), false), runtime.GOOS); len(c.Problems) > 0 {
			v.Check = &c
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

// checkCustom refuses a new custom recorder command unless the engineer is on
// the machine running tabula itself (signed in there when a password is
// set): an engineer link or session from another device must not be a way
// to run programs on that machine. Keeping the current command is fine.
func (s *Server) checkCustom(r *http.Request, cfg audio.Config) error {
	if cfg.Recorder != "custom" {
		return nil
	}
	if c := callerFrom(r); c.Local && c.Role == RoleEngineer {
		return nil
	}
	s.mu.Lock()
	cur := s.audio.cfg
	s.mu.Unlock()
	if cur.Recorder == "custom" && strings.TrimSpace(cur.Command) == strings.TrimSpace(cfg.Command) {
		return nil
	}
	return errors.New("a custom recorder command can only be chosen on the machine running tabula")
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
	if err := s.checkCustom(r, cfg); err != nil {
		httpError(w, http.StatusForbidden, err)
		return
	}
	// 32 channels from a card with 2 inputs: the mistake of a test
	if msg := audio.CheckChannels(cfg, s.listDevices(r.Context(), true), runtime.GOOS); msg != "" {
		httpError(w, http.StatusBadRequest, errors.New(msg))
		return
	}
	s.mu.Lock()
	cfg.USBBackup = s.audio.cfg.USBBackup // set with POST /api/record/usb
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
	devs := s.listDevices(r.Context(), true)
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
	ending := s.audio.ending
	s.mu.Unlock()
	if ending != nil {
		select { // a recording ended on a disconnect may still hold the card
		case <-ending:
		case <-time.After(audioFreeWait):
		}
	}
	s.audio.probeMu.Lock()
	s.mu.Lock()
	cfg := s.audio.cfg
	// a capture that failed holds no device: test the fix before restarting it
	busy := s.audio.cap != nil && s.audio.cap.Status().State != audio.Failed || s.audio.testing
	source, mirror := s.audio.source, s.mirror
	if !busy {
		s.audio.testing = true
	}
	s.mu.Unlock()
	s.audio.probeMu.Unlock()
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
		if err := s.checkCustom(r, cfg.Normalized()); err != nil {
			httpError(w, http.StatusForbidden, err)
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
// ?format=reaper (project), audacity (label track), csv or scene (the
// console at the start).
func (s *Server) exportSession(w http.ResponseWriter, r *http.Request) {
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
	case session.ExportScene:
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			httpError(w, http.StatusNotFound, errors.New("this session has no start scene"))
			return
		}
		name = ses.Name() + ".scn"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	ses.Export(w, format)
}
