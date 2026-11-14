package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/emu"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/session"
	"github.com/yoda-jm/tabula/pkg/state"
)

// Local replay: a session played back on this machine, without the
// console. tabula starts a session console (an emulator in tabula, on
// 127.0.0.1, holding the session's state), leaves the console it was
// connected to, and connects to the session console: every page then
// follows the session as it follows a console. The session's changes are
// replayed into it, its input meters take the levels of the recorded
// tracks, and the tracks chosen to listen to play on this machine, in step.
// Back to the console closes it and connects to the console left, writes
// disarmed. Nothing reaches the real console meanwhile: tabula holds no
// link to it.

// localLevelWindow is the stretch of audio a meter frame reads.
const localLevelWindow = 0.05

// errSeeked ends a replay once the state at its start is set: a seek
// while paused.
var errSeeked = errors.New("seeked")

type localState struct {
	name   string
	ses    *session.Session
	srv    *emu.Server
	addr   string // the session console's
	back   string // the console to go back to, "" for none
	stop   context.CancelFunc
	levels *audio.Levels // nil without audio

	mu      sync.Mutex
	playing bool
	speed   float64
	at      float64   // the position when paused, or when play began
	since   time.Time // when play began (once the state was set)
	cancel  context.CancelFunc
	gen     int // the play running, so an old one ending changes nothing
	listen  []audio.MixTrack
	lmode   string           // "off", "stereo", "tracks"
	cue     []audio.MixTrack // the raw tracks cued (the strips soloed): heard in place of listen
	marks   []markView       // the session's markers, in time order, with those added here
	pb      *audio.Playback
	lerr    string
	err     string
}

// localView is the local replay in the status.
type localView struct {
	Session  string           `json:"session"`
	Duration float64          `json:"duration"`
	At       float64          `json:"at"`    // seconds of the session
	AtTime   int64            `json:"at_ms"` // when At was read (Unix ms): pages move on from it while playing
	Playing  bool             `json:"playing"`
	Speed    float64          `json:"speed"`
	Audio    bool             `json:"audio"`            // the session has tracks to read
	Main     [2]int           `json:"main"`             // the tracks recording main L and R, 0 if none
	Listen   string           `json:"listen"`           // off, stereo, tracks
	Tracks   []audio.MixTrack `json:"tracks,omitempty"` // what plays
	Cue      []audio.MixTrack `json:"cue,omitempty"`    // the raw tracks cued with SOLO, heard instead
	Markers  []markView       `json:"markers"`          // the session's markers, in time order
	Heard    bool             `json:"heard"`            // the listening plays now
	Error    string           `json:"error,omitempty"`  // the replay or the listening stopped on it
	Back     string           `json:"back,omitempty"`   // the console to go back to
}

// position is where the replay is now. Called with l.mu held.
func (l *localState) position() float64 {
	if !l.playing || l.since.IsZero() {
		return l.at
	}
	return min(l.at+time.Since(l.since).Seconds()*l.speed, l.ses.Meta.Duration)
}

func (l *localState) view() *localView {
	l.mu.Lock()
	defer l.mu.Unlock()
	v := &localView{Session: l.name, Duration: l.ses.Meta.Duration, At: l.position(), AtTime: time.Now().UnixMilli(),
		Playing: l.playing, Speed: l.speed, Audio: l.levels != nil, Listen: l.lmode, Tracks: l.listen,
		Heard: l.pb != nil, Error: l.err, Back: l.back, Cue: l.cue, Markers: slices.Clone(l.marks)}
	if v.Error == "" {
		v.Error = l.lerr
	}
	v.Main[0], v.Main[1] = audio.MainTracks(l.srv.Store())
	return v
}

// inputs gives the session console the levels of the tracks recording its
// inputs, at the replay's position.
func (l *localState) inputs() [40]float64 {
	var out [40]float64
	l.mu.Lock()
	t := l.position()
	l.mu.Unlock()
	lv := l.levels.At(t, localLevelWindow)
	for i, track := range audio.InputTracks(l.srv.Store()) {
		if track > 0 {
			out[i] = lv[track]
		}
	}
	return out
}

// OpenLocal starts a local replay of a session, at from (paused).
func (s *Server) OpenLocal(name string, from float64) (int, error) {
	dir, err := s.sessionDir(name)
	if err != nil {
		return http.StatusNotFound, err
	}
	ses, err := s.openSession(dir)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	s.mu.Lock()
	switch {
	case s.local != nil:
		err = errors.New("a local replay is open: close it first")
	case s.rec != nil || s.recStarting != nil:
		err = errors.New("a recording is running: stop it before a local replay")
	case s.replayCancel != nil:
		err = errors.New("a replay to the console is running: stop it first")
	case s.audio.testing:
		err = errors.New("the audio input is being tested")
	}
	back, model := s.addr, s.model
	s.mu.Unlock()
	if err != nil {
		return http.StatusConflict, err
	}

	st := state.New(schema.Build(model))
	from = min(max(from, 0), ses.Meta.Duration)
	ses.StateAt(st, from)
	l := &localState{name: name, ses: ses, back: back, speed: 1, at: from, lmode: "off", marks: []markView{}}
	for _, m := range ses.Markers() {
		l.marks = append(l.marks, markView{Label: m.Marker, T: m.T})
	}
	cfg := emu.Config{Model: model, Name: "tabula-session", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if info := ses.Meta.Audio; info != nil {
		if l.levels, err = audio.OpenLevels(dir, *info); err != nil {
			s.log.Warn("local replay: the meters follow no track", "session", name, "err", err)
			l.levels = nil
		}
	}
	if l.levels != nil {
		cfg.Inputs = l.inputs
	}
	l.srv = emu.New(cfg, st)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		if l.levels != nil {
			l.levels.Close()
		}
		return http.StatusInternalServerError, err
	}
	ctx, stop := context.WithCancel(context.Background())
	l.stop, l.addr = stop, conn.LocalAddr().String()
	go l.srv.Serve(ctx, conn)
	unwatch := st.Subscribe(func(ch []state.Change) {
		for _, c := range ch {
			if strings.HasPrefix(c.Leaf.Addr, "/-stat/solosw/") {
				s.localCue(l)
				return
			}
		}
	})
	go func() { <-ctx.Done(); unwatch() }()
	if left, right := audio.MainTracks(st); left > 0 && right > 0 {
		l.listen, l.lmode = []audio.MixTrack{{Channel: left, Gain: 1, Pan: -1}, {Channel: right, Gain: 1, Pan: 1}}, "stereo"
	}

	s.mu.Lock()
	s.local = l
	s.mu.Unlock()
	s.log.Info("local replay", "session", name, "from", from, "console", l.addr, "back", back)
	s.connectTo(l.addr)
	return http.StatusOK, nil
}

// CloseLocal ends the local replay and connects to the console it left
// (when reconnect, and there was one); writes come back disarmed.
func (s *Server) CloseLocal(reconnect bool) {
	s.mu.Lock()
	l := s.local
	s.local = nil
	s.mu.Unlock()
	if l == nil {
		return
	}
	l.mu.Lock()
	l.gen++
	if l.cancel != nil {
		l.cancel()
	}
	pb := l.pb
	l.pb, l.playing = nil, false
	l.mu.Unlock()
	if pb != nil {
		pb.Stop()
	}
	if l.levels != nil {
		l.levels.Close()
	}
	s.log.Info("local replay closed", "session", l.name, "back", l.back)
	switch {
	case !reconnect:
	case l.back != "":
		s.Connect(l.back)
	default:
		s.mu.Lock()
		s.closeLocked()
		s.mu.Unlock()
	}
	l.stop()
	s.publishStatus()
}

// localNow returns the local replay, nil if none.
func (s *Server) localNow() *localState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.local
}

// play replays from the current position at the current speed, and starts
// the listening at 1×. Called with l.mu held.
func (s *Server) localPlayLocked(l *localState, seekOnly bool) {
	l.gen++
	gen := l.gen
	if l.cancel != nil {
		l.cancel()
	}
	s.stopListenLocked(l)
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel, l.err = cancel, ""
	l.playing, l.since = !seekOnly, time.Time{}
	from, speed := l.at, l.speed
	p := &session.Player{S: l.ses, Target: session.StoreTarget{Store: l.srv.Store()}, Speed: speed, Skip: isSolo,
		OnReady: func() error {
			if seekOnly {
				return errSeeked
			}
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.gen != gen {
				return context.Canceled
			}
			l.since = time.Now()
			s.startListenLocked(l)
			return nil
		}}
	go func() {
		err := p.Play(ctx, s.model, from)
		if err == nil && !seekOnly { // the last change is made: on to the end of the session
			l.mu.Lock()
			left := time.Duration((l.ses.Meta.Duration - l.position()) / speed * float64(time.Second))
			l.mu.Unlock()
			select {
			case <-ctx.Done():
				err = ctx.Err()
			case <-time.After(left):
			}
		}
		l.mu.Lock()
		if l.gen == gen {
			if !seekOnly && err == nil { // played to its end
				l.at, l.playing, l.since = l.ses.Meta.Duration, false, time.Time{}
				s.stopListenLocked(l)
			}
			if err != nil && !errors.Is(err, errSeeked) && !errors.Is(err, context.Canceled) {
				l.err, l.playing = err.Error(), false
				s.log.Warn("local replay", "err", err)
			}
		}
		l.mu.Unlock()
		s.publishStatus()
	}()
}

// pauseLocked stops where it is. Called with l.mu held.
func (s *Server) localPauseLocked(l *localState) {
	l.at = l.position()
	l.gen++
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	l.playing, l.since = false, time.Time{}
	s.stopListenLocked(l)
}

// startListenLocked plays the tracks listened to from the position, at
// 1× only. Called with l.mu held.
func (s *Server) startListenLocked(l *localState) {
	l.lerr = ""
	info := l.ses.Meta.Audio
	mix := l.heardLocked()
	if len(mix) == 0 || l.speed != 1 || info == nil {
		return
	}
	s.mu.Lock()
	cfg, sink := s.audio.cfg.Normalized(), s.sc.sink
	s.mu.Unlock()
	cfg.PlayTo, cfg.PlayCommand = listenDevice(cfg.Recorder), ""
	pb, err := audio.StartPlayback(cfg, l.ses.Dir, *info, audio.PlayOptions{
		From: info.AudioTime(l.position()), Log: s.log, Sink: sink, Mix: mix})
	if err != nil {
		l.lerr = "listening: " + err.Error()
		s.log.Warn("local replay: listening", "err", err)
		return
	}
	l.pb = pb
	go func() {
		<-pb.Done()
		if err := pb.Err(); err != nil {
			l.mu.Lock()
			if l.pb == pb {
				l.pb, l.lerr = nil, "listening: "+err.Error()
			}
			l.mu.Unlock()
			s.publishStatus()
		}
	}()
}

// stopListenLocked stops the listening. Called with l.mu held; the player
// stops in the background (it may take a moment to drain).
func (s *Server) stopListenLocked(l *localState) {
	if l.pb != nil {
		go l.pb.Stop()
		l.pb = nil
	}
}

// heardLocked is what the listener hears: the tracks cued, else the
// Listen choice. Called with l.mu held.
func (l *localState) heardLocked() []audio.MixTrack {
	if len(l.cue) > 0 {
		return l.cue
	}
	return l.listen
}

// remixLocked changes what is heard while playing: in the stream when the
// player runs (a crossfade, no gap), else by starting it. Called with
// l.mu held.
func (s *Server) remixLocked(l *localState) {
	switch {
	case !l.playing || l.since.IsZero(): // heard from the next play
	case l.pb != nil:
		l.pb.SetMix(l.heardLocked())
	default:
		s.startListenLocked(l)
	}
}

// isSolo tells the solo switches apart: the session's own solos are not
// replayed, the headphones are the listener's (localCue).
func isSolo(path string) bool { return strings.HasPrefix(path, "/-stat/solo") }

// localCue cues raw tracks with the session console's SOLO buttons, as an
// engineer checks a source in the headphones: the track recording a
// soloed input strip is heard in the middle, as recorded (after the
// headamp, before EQ, dynamics and fader: not the console's solo), and
// soloing L/R cues the stereo out when recorded; in place of the Listen
// choice, which is back once no solo is left.
func (s *Server) localCue(l *localState) {
	st := l.srv.Store()
	tracks := audio.InputTracks(st)
	recorded := map[int]bool{}
	if info := l.ses.Meta.Audio; info != nil {
		for _, t := range info.Tracks {
			recorded[t.Channel] = t.File != ""
		}
	}
	var cue []audio.MixTrack
	seen := map[int]bool{}
	add := func(track int, pan float64) {
		if track > 0 && recorded[track] && !seen[track] {
			seen[track] = true
			cue = append(cue, audio.MixTrack{Channel: track, Gain: 1, Pan: pan})
		}
	}
	on := func(i int) bool { // strip i of the 80, from 0
		v, _, err := st.Get(fmt.Sprintf("/-stat/solosw/%02d", i+1))
		return err == nil && v.Int == 1
	}
	for i, track := range tracks {
		if on(i) {
			add(track, 0)
		}
	}
	if on(70) { // main L/R
		left, right := audio.MainTracks(st)
		add(left, -1)
		add(right, 1)
	}
	l.mu.Lock()
	same := len(cue) == len(l.cue)
	for i := range cue {
		same = same && cue[i] == l.cue[i]
	}
	if !same {
		l.cue = cue
		s.remixLocked(l)
	}
	l.mu.Unlock()
	if !same {
		s.publishStatus()
	}
}

// listenDevice is this machine's own output, for the player of a recorder.
func listenDevice(recorder string) string {
	switch recorder {
	case "parecord":
		return "@DEFAULT_SINK@"
	case "ffmpeg":
		return "default" // ALSA; macOS takes a device index (not tried)
	}
	return "default"
}

func (s *Server) localRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/open", s.localOpen)
	mux.HandleFunc("POST /api/local/transport", s.localTransport)
	mux.HandleFunc("PUT /api/local/listen", s.localListen)
	mux.HandleFunc("POST /api/local/close", s.localClose)
	mux.HandleFunc("POST /api/local/mark", s.localMark)
	mux.HandleFunc("PUT /api/sessions/{name}/markers", s.putMarkers)
}

func (s *Server) localOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Session string  `json:"session"`
		From    float64 `json:"from"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"session": name, "from": seconds}`))
		return
	}
	if code, err := s.OpenLocal(req.Session, req.From); err != nil {
		httpError(w, code, err)
		return
	}
	writeJSON(w, http.StatusOK, s.status())
}

// localTransport plays, pauses, seeks and sets the speed:
// {"play"?: bool, "at"?: s, "speed"?: x}.
func (s *Server) localTransport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Play  *bool    `json:"play"`
		At    *float64 `json:"at"`
		Speed *float64 `json:"speed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"play": bool, "at": seconds, "speed": x}`))
		return
	}
	if req.Speed != nil && (*req.Speed < 0.25 || *req.Speed > 16) {
		httpError(w, http.StatusBadRequest, errors.New("a speed goes from 0.25 to 16"))
		return
	}
	l := s.localNow()
	if l == nil {
		httpError(w, http.StatusConflict, errors.New("no local replay is open"))
		return
	}
	l.mu.Lock()
	playing := l.playing
	if req.Play != nil {
		playing = *req.Play
	}
	if req.At != nil || req.Speed != nil || req.Play != nil {
		s.localPauseLocked(l)
	}
	if req.At != nil {
		l.at = min(max(*req.At, 0), l.ses.Meta.Duration)
	}
	if req.Speed != nil {
		l.speed = *req.Speed
	}
	switch {
	case playing && l.at < l.ses.Meta.Duration:
		s.localPlayLocked(l, false)
	case req.At != nil:
		s.localPlayLocked(l, true) // the state at the new position, paused
	}
	l.mu.Unlock()
	s.publishStatus()
	writeJSON(w, http.StatusOK, s.status())
}

// localListen chooses what plays on this machine: {"preset": "stereo" |
// "off"} or {"tracks": [{"track", "gain", "pan"}]}.
func (s *Server) localListen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Preset string           `json:"preset"`
		Tracks []audio.MixTrack `json:"tracks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"preset": "stereo" | "off"} or {"tracks": [...]}`))
		return
	}
	l := s.localNow()
	if l == nil {
		httpError(w, http.StatusConflict, errors.New("no local replay is open"))
		return
	}
	info := l.ses.Meta.Audio
	has := map[int]bool{}
	if info != nil {
		for _, t := range info.Tracks {
			has[t.Channel] = t.File != ""
		}
	}
	var mix []audio.MixTrack
	mode := "tracks"
	switch req.Preset {
	case "off":
		mode = "off"
	case "stereo":
		left, right := audio.MainTracks(l.srv.Store())
		if left == 0 || right == 0 || !has[left] || !has[right] {
			httpError(w, http.StatusConflict, errors.New("the card did not record the main L/R"))
			return
		}
		mix, mode = []audio.MixTrack{{Channel: left, Gain: 1, Pan: -1}, {Channel: right, Gain: 1, Pan: 1}}, "stereo"
	case "":
		for _, t := range req.Tracks {
			if !has[t.Channel] {
				httpError(w, http.StatusBadRequest, fmt.Errorf("track %d was not recorded", t.Channel))
				return
			}
			if t.Gain < 0 || t.Gain > 4 || t.Pan < -1 || t.Pan > 1 {
				httpError(w, http.StatusBadRequest, fmt.Errorf("track %d: a gain goes from 0 to 4, a pan from -1 to 1", t.Channel))
				return
			}
			mix = append(mix, t)
		}
		if len(mix) == 0 {
			mode = "off"
		}
	default:
		httpError(w, http.StatusBadRequest, fmt.Errorf("unknown preset %q", req.Preset))
		return
	}
	l.mu.Lock()
	l.listen, l.lmode = mix, mode
	s.remixLocked(l)
	l.mu.Unlock()
	s.publishStatus()
	writeJSON(w, http.StatusOK, s.status())
}

func (s *Server) localClose(w http.ResponseWriter, r *http.Request) {
	if s.localNow() == nil {
		httpError(w, http.StatusConflict, errors.New("no local replay is open"))
		return
	}
	s.CloseLocal(true)
	writeJSON(w, http.StatusOK, s.status())
}

// localMark adds a marker to the session replayed, at the replay's
// position ("at": another time): {"label"?, "at"?}. Without a label: "Mark
// 12:34". It goes into the session (events.jsonl, meta.json) and its
// exports, as a marker set while recording does.
func (s *Server) localMark(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label string   `json:"label"`
		At    *float64 `json:"at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"label": text, "at": seconds}`))
		return
	}
	label := strings.Join(strings.Fields(req.Label), " ")
	if utf8.RuneCountInString(label) > maxSongChars {
		httpError(w, http.StatusBadRequest, fmt.Errorf("a marker takes %d characters at most", maxSongChars))
		return
	}
	l := s.localNow()
	if l == nil {
		httpError(w, http.StatusConflict, errors.New("no local replay is open"))
		return
	}
	l.mu.Lock()
	t := l.position()
	if req.At != nil {
		t = min(max(*req.At, 0), l.ses.Meta.Duration)
	}
	l.mu.Unlock()
	if label == "" {
		label = "Mark " + clock(t)
	}
	e, err := session.AddMarker(l.ses.Dir, t, label)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	m := markView{Label: e.Marker, T: e.T}
	l.mu.Lock()
	i := sort.Search(len(l.marks), func(i int) bool { return l.marks[i].T > m.T })
	l.marks = slices.Insert(l.marks, i, m)
	l.mu.Unlock()
	if l.ses.Meta.Audio != nil { // the Reaper project and the labels hold it too
		if ses, err := s.openSession(l.ses.Dir); err == nil {
			if err := ses.WriteExports(); err != nil {
				s.log.Warn("local replay: exports", "err", err)
			}
		}
	}
	s.log.Info("local replay: marked", "session", l.name, "label", m.Label, "t", m.T)
	s.publishStatus()
	writeJSON(w, http.StatusOK, map[string]any{"marked": m, "status": s.status()})
}

// clock writes seconds as m:ss or h:mm:ss.
func clock(t float64) string {
	sec := int(t)
	if sec >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec/60%60, sec%60)
	}
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

// maxMarkers is the most markers a session takes from the mark editor.
const maxMarkers = 1000

// putMarkers replaces a session's markers (the mark editor):
// {"markers": [{"label", "t", "origin"?}]}, labels tidied (1 to 80
// characters), times within the session. Not while it is recorded.
func (s *Server) putMarkers(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dir, err := s.sessionDir(name)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	var req struct {
		Markers []struct {
			Label  string  `json:"label"`
			T      float64 `json:"t"`
			Origin string  `json:"origin"`
		} `json:"markers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Markers == nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"markers": [{"label", "t"}]}`))
		return
	}
	s.mu.Lock()
	recording := s.rec != nil && filepath.Base(s.rec.Dir()) == name
	s.mu.Unlock()
	if recording {
		httpError(w, http.StatusConflict, errors.New("this session is being recorded: its markers are edited once it stops"))
		return
	}
	ses, err := s.openSession(dir)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	if len(req.Markers) > maxMarkers {
		httpError(w, http.StatusBadRequest, fmt.Errorf("a session takes %d markers at most", maxMarkers))
		return
	}
	marks := make([]session.Event, 0, len(req.Markers))
	for _, m := range req.Markers {
		label := strings.Join(strings.Fields(m.Label), " ")
		switch {
		case label == "":
			httpError(w, http.StatusBadRequest, errors.New("a marker needs a label"))
			return
		case utf8.RuneCountInString(label) > maxSongChars:
			httpError(w, http.StatusBadRequest, fmt.Errorf("%q: a marker takes %d characters at most", label, maxSongChars))
			return
		case !(m.T >= 0 && m.T <= ses.Meta.Duration):
			httpError(w, http.StatusBadRequest, fmt.Errorf("%q: %v s is not in the session (0 to %v s)", label, m.T, ses.Meta.Duration))
			return
		}
		origin := m.Origin
		if origin != "review" {
			origin = "" // a marker set while recording, or edited
		}
		marks = append(marks, session.Event{T: m.T, Marker: label, Origin: origin})
	}
	if err := session.SetMarkers(dir, marks); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	ses, err = s.openSession(dir)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	if ses.Meta.Audio != nil {
		if err := ses.WriteExports(); err != nil {
			s.log.Warn("markers: exports", "session", name, "err", err)
		}
	}
	if l := s.localNow(); l != nil && l.name == name { // the band shows them
		l.mu.Lock()
		l.marks = l.marks[:0:0]
		for _, m := range ses.Markers() {
			l.marks = append(l.marks, markView{Label: m.Marker, T: m.T})
		}
		l.mu.Unlock()
		s.publishStatus()
	}
	s.log.Info("markers edited", "session", name, "markers", len(marks))
	writeJSON(w, http.StatusOK, map[string]any{"markers": ses.Markers()})
}
