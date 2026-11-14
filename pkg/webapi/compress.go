package webapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/session"
)

// Compressing sessions: their WAV tracks become FLAC files, losslessly, at
// about half their size, in the background, one track at a time. Each
// track is encoded to a .part, decoded again and compared with the WAV
// (length, MD5 of the samples) before it replaces the WAV: meta.json then
// names the FLAC file, and the WAV goes. A crash leaves every track whole,
// WAV or FLAC; a .part left over is removed at the next run. It waits
// while a recording runs (the disk is the recording's: a track being
// compressed is given up and done again later) and while the session is
// replayed, soundchecked, replayed locally or copied. Every reader reads
// FLAC tracks as WAV ones (audio.OpenTrack).

type compressState struct {
	mu      sync.Mutex
	queue   []string
	running bool
	session string
	track   string
	done    int
	total   int
	saved   int64
	paused  string
	err     string
	cancel  context.CancelFunc // the track being compressed
	poll    time.Duration      // tests: how often a waiting compression looks again
}

// compressView is the compression: GET /api/compress, "compress" events.
type compressView struct {
	Queue   []string `json:"queue"`             // the sessions waiting, the one compressed first
	Session string   `json:"session,omitempty"` // being compressed
	Track   string   `json:"track,omitempty"`
	Done    int      `json:"done"`  // tracks of the session done
	Total   int      `json:"total"` // tracks of the session to compress
	Saved   int64    `json:"saved"` // bytes saved since tabula started
	Paused  string   `json:"paused,omitempty"`
	Error   string   `json:"error,omitempty"` // the last session that failed, and why
}

var errCompressWait = errors.New("waiting")

func (s *Server) compressView() compressView {
	c := &s.cmp
	c.mu.Lock()
	defer c.mu.Unlock()
	return compressView{Queue: append([]string{}, c.queue...), Session: c.session, Track: c.track, Done: c.done,
		Total: c.total, Saved: c.saved, Paused: c.paused, Error: c.err}
}

func (s *Server) publishCompress() { s.hub.publishEngineers("compress", s.compressView()) }

// CompressSession queues a session to compress.
func (s *Server) CompressSession(name string) {
	c := &s.cmp
	c.mu.Lock()
	for _, q := range c.queue {
		if q == name {
			c.mu.Unlock()
			return
		}
	}
	c.queue = append(c.queue, name)
	start := !c.running
	c.running = true
	c.mu.Unlock()
	if start {
		go s.compressRun()
	}
	s.publishCompress()
}

// compressInterrupt gives up the track being compressed: a recording
// starts, the disk is its.
func (s *Server) compressInterrupt() {
	c := &s.cmp
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()
}

// compressBusy says why a session cannot be compressed now, "" if it can.
func (s *Server) compressBusy(name string) string {
	s.mu.Lock()
	recording := s.rec != nil || s.recStarting != nil || s.finishing > 0
	inUse := s.replayCancel != nil && s.replayOf == name || s.local != nil && s.local.name == name || s.localOpening
	s.mu.Unlock()
	switch {
	case recording:
		return "recording"
	case inUse:
		return "in use"
	}
	s.home.mu.Lock()
	defer s.home.mu.Unlock()
	if cp := s.home.copy; cp != nil && cp.State == copyRunning && cp.Session == name {
		return "in use"
	}
	return ""
}

// compressPollEvery is how often a waiting compression looks again
// (compressState.poll replaces it in tests).
const compressPollEvery = 2 * time.Second

func (s *Server) compressPoll() time.Duration {
	if s.cmp.poll > 0 {
		return s.cmp.poll
	}
	return compressPollEvery
}

func (s *Server) compressRun() {
	c := &s.cmp
	for {
		c.mu.Lock()
		if len(c.queue) == 0 {
			c.running, c.session, c.track, c.done, c.total, c.paused = false, "", "", 0, 0, ""
			c.mu.Unlock()
			s.publishCompress()
			return
		}
		name := c.queue[0]
		c.mu.Unlock()
		err := s.compressSession(name)
		c.mu.Lock()
		switch {
		case errors.Is(err, errCompressWait):
			if c.paused == "in use" && len(c.queue) > 1 { // the others meanwhile
				c.queue = append(c.queue[1:], name)
			}
			c.mu.Unlock()
			time.Sleep(s.compressPoll())
			continue
		case err != nil:
			c.err = fmt.Sprintf("%s: %v", name, err)
			s.log.Error("compress", "session", name, "err", err)
		}
		c.queue = c.queue[1:]
		c.session, c.track, c.done, c.total, c.paused = "", "", 0, 0, ""
		c.mu.Unlock()
		s.publishCompress()
	}
}

// compressSession compresses the WAV tracks of a session; errCompressWait
// when it has to wait (what is done stays done).
func (s *Server) compressSession(name string) error {
	c := &s.cmp
	setPaused := func(why string) {
		c.mu.Lock()
		changed := c.paused != why
		c.session, c.paused = name, why
		c.mu.Unlock()
		if changed {
			s.publishCompress()
		}
	}
	if why := s.compressBusy(name); why != "" {
		setPaused(why)
		return errCompressWait
	}
	dir, err := s.sessionDir(name)
	if err != nil {
		return nil // deleted meanwhile: nothing to do
	}
	ses, err := s.openSession(dir)
	if err != nil {
		return err
	}
	parts, _ := filepath.Glob(filepath.Join(dir, "audio", "*.flac.part"))
	for _, p := range parts {
		os.Remove(p)
	}
	info := ses.Meta.Audio
	if info == nil {
		return nil
	}
	for _, t := range info.Tracks { // a WAV left over (Windows: it was open) once its FLAC was named
		if audio.IsFLAC(t.File) {
			wav := filepath.Join(dir, filepath.FromSlash(strings.TrimSuffix(t.File, filepath.Ext(t.File))+".wav"))
			if _, err := os.Stat(wav); err == nil && os.Remove(wav) == nil {
				s.log.Info("compress: a WAV left over removed", "file", wav)
			}
		}
	}
	var todo []int
	for i, t := range info.Tracks {
		if t.File != "" && strings.EqualFold(filepath.Ext(t.File), ".wav") {
			todo = append(todo, i)
		}
	}
	c.mu.Lock()
	c.session, c.paused, c.done, c.total = name, "", 0, len(todo)
	if strings.HasPrefix(c.err, name+":") { // an earlier run's: this one says again
		c.err = ""
	}
	c.mu.Unlock()
	s.publishCompress()
	compressed := 0
	for k, i := range todo {
		if why := s.compressBusy(name); why != "" {
			setPaused(why)
			return errCompressWait
		}
		t := info.Tracks[i]
		wav := filepath.Join(dir, filepath.FromSlash(t.File))
		rel := strings.TrimSuffix(t.File, filepath.Ext(t.File)) + ".flac"
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		ctx, cancel := context.WithCancel(context.Background())
		c.mu.Lock()
		c.track, c.done, c.paused, c.cancel = filepath.Base(t.File), k, "", cancel
		c.mu.Unlock()
		s.publishCompress()
		// a recording, a replay or a copy starting meanwhile stops the track
		var why string
		watched := make(chan struct{})
		go func() {
			tick := time.NewTicker(s.compressPoll() / 4)
			defer tick.Stop()
			for {
				select {
				case <-watched:
					return
				case <-tick.C:
					if w := s.compressBusy(name); w != "" {
						c.mu.Lock()
						why = w
						c.mu.Unlock()
						cancel()
						return
					}
				}
			}
		}()
		before, statErr := os.Stat(wav)
		err := statErr
		if err == nil {
			err = audio.CompressTrack(ctx, wav, dst)
		}
		close(watched)
		cancel()
		c.mu.Lock()
		c.cancel = nil
		if why == "" && errors.Is(err, context.Canceled) {
			why = "recording" // compressInterrupt
		}
		stopped := why
		c.mu.Unlock()
		switch {
		case errors.Is(err, context.Canceled):
			setPaused(stopped)
			return errCompressWait
		case errors.Is(err, audio.ErrNotCompressible):
			s.log.Info("compress: kept as WAV", "session", name, "track", t.File, "why", err)
			continue
		case err != nil: // this track stays WAV; the others go on
			s.log.Error("compress: kept as WAV", "session", name, "track", t.File, "err", err)
			c.mu.Lock()
			c.err = fmt.Sprintf("%s: %s: %v", name, t.File, err)
			c.mu.Unlock()
			continue
		}
		// the FLAC file is the track from now on, then the WAV goes; meta.json
		// is read again (a review may have added a marker meanwhile)
		named := false
		err = session.UpdateMeta(dir, func(m *session.Meta) {
			if m.Audio == nil {
				return
			}
			for j := range m.Audio.Tracks {
				if m.Audio.Tracks[j].Channel == t.Channel && m.Audio.Tracks[j].File == t.File {
					m.Audio.Tracks[j].File, named = rel, true
				}
			}
		})
		if err == nil && !named {
			err = fmt.Errorf("%s is no longer in meta.json", t.File)
		}
		if err != nil {
			os.Remove(dst)
			return err
		}
		info.Tracks[i].File = rel
		if err := os.Remove(wav); err != nil {
			s.log.Warn("compress: the WAV is still there", "file", wav, "err", err)
		}
		compressed++
		if after, err := os.Stat(dst); err == nil {
			c.mu.Lock()
			c.saved += before.Size() - after.Size()
			c.mu.Unlock()
		}
	}
	if compressed > 0 { // the Reaper project names the FLAC files (read again: markers added meanwhile)
		fresh, err := s.openSession(dir)
		if err == nil {
			err = fresh.WriteExports()
		}
		if err != nil {
			s.log.Warn("compress: exports", "session", name, "err", err)
		}
	}
	s.log.Info("compress: done", "session", name, "tracks", compressed)
	return nil
}

func (s *Server) compressRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/compress", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.compressView())
	})
	mux.HandleFunc("POST /api/sessions/{name}/compress", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if _, err := s.sessionDir(name); err != nil {
			httpError(w, http.StatusNotFound, err)
			return
		}
		s.mu.Lock()
		recordingThis := s.rec != nil && filepath.Base(s.rec.Dir()) == name
		s.mu.Unlock()
		if recordingThis {
			httpError(w, http.StatusConflict, errors.New("this session is being recorded"))
			return
		}
		s.CompressSession(name)
		writeJSON(w, http.StatusAccepted, s.compressView())
	})
}

// compressing tells whether a session's tracks are being compressed now
// (a copy or a download would see files come and go).
func (s *Server) compressing(name string) bool {
	c := &s.cmp
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running && c.session == name && c.paused == ""
}
