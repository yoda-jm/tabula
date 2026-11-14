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
	recording := s.rec != nil || s.recStarting != nil
	inUse := s.replayCancel != nil && s.replayOf == name || s.local != nil && s.local.name == name
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

// compressPollEvery is how often a waiting compression looks again.
var compressPollEvery = 2 * time.Second

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
			c.mu.Unlock()
			time.Sleep(compressPollEvery)
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
	var todo []int
	for i, t := range info.Tracks {
		if t.File != "" && strings.EqualFold(filepath.Ext(t.File), ".wav") {
			todo = append(todo, i)
		}
	}
	c.mu.Lock()
	c.session, c.paused, c.done, c.total = name, "", 0, len(todo)
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
		before, statErr := os.Stat(wav)
		err := audio.CompressTrack(ctx, wav, dst)
		cancel()
		c.mu.Lock()
		c.cancel = nil
		c.mu.Unlock()
		switch {
		case errors.Is(err, context.Canceled):
			setPaused("recording")
			return errCompressWait
		case errors.Is(err, audio.ErrNotCompressible):
			s.log.Info("compress: kept as WAV", "session", name, "track", t.File, "why", err)
			continue
		case statErr != nil && err == nil:
			err = statErr
		}
		if err != nil {
			return err
		}
		// the FLAC file is the track from now on, then the WAV goes
		info.Tracks[i].File = rel
		if err := session.WriteMeta(dir, ses.Meta); err != nil {
			info.Tracks[i].File = t.File
			os.Remove(dst)
			return err
		}
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
	if compressed > 0 {
		if err := ses.WriteExports(); err != nil { // the Reaper project names the FLAC files
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
