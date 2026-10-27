package webapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yoda-jm/tabula/pkg/session"
)

// The setlist: the show's songs, typed before it (Record page), with the
// next one to mark. It is kept in <sessions>/setlist.json, so a restart of
// tabula and every engineer device share it. One tap (top bar, or the M
// key) marks the next song with its name, "3. Song name", and moves on:
// chosen and moved on under one lock, two devices never mark the same song
// twice. A session keeps the setlist it was recorded with (setlist.json in
// its folder), and its details list the songs with their marker times.

const (
	setlistFile  = "setlist.json"
	maxSongs     = 200
	maxSongChars = 80
)

// setlistView is the setlist: GET /api/setlist, "setlist" events.
type setlistView struct {
	Songs   []string  `json:"songs"`
	Next    int       `json:"next"` // the next song to mark (from 0); len(songs) once all are marked
	Updated time.Time `json:"updated,omitzero"`
}

type setlistState struct {
	mu     sync.Mutex
	dir    string // the sessions folder data was read from
	data   setlistView
	loaded bool
}

// setlistLocked returns the setlist of the sessions folder, read on first
// use (or when the folder changed). Called with s.setlist.mu held.
func (s *Server) setlistLocked() setlistView {
	s.mu.Lock()
	dir := s.sessionsDir
	s.mu.Unlock()
	l := &s.setlist
	if l.loaded && l.dir == dir {
		return l.data
	}
	l.dir, l.loaded, l.data = dir, true, setlistView{Songs: []string{}}
	b, err := os.ReadFile(filepath.Join(dir, setlistFile))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("setlist", "err", err)
		}
		return l.data
	}
	var v setlistView
	if err := json.Unmarshal(b, &v); err != nil {
		s.log.Warn("setlist: not understood, starting empty", "file", filepath.Join(dir, setlistFile), "err", err)
		return l.data
	}
	if v.Songs, err = cleanSongs(v.Songs); err != nil {
		s.log.Warn("setlist", "err", err)
	}
	v.Next = min(max(v.Next, 0), len(v.Songs))
	l.data = v
	return v
}

// saveSetlistLocked keeps v as the setlist, in the sessions folder and in
// the session being recorded. Called with s.setlist.mu held.
func (s *Server) saveSetlistLocked(v setlistView) error {
	v.Updated = time.Now()
	s.setlistLocked() // the folder it belongs to
	if err := writeJSONFile(filepath.Join(s.setlist.dir, setlistFile), v); err != nil {
		return err
	}
	s.setlist.data = v
	s.sessionSetlistLocked()
	s.hub.publishEngineers("setlist", v)
	return nil
}

// sessionSetlistLocked writes the songs into the session being recorded,
// which keeps the setlist it was recorded with.
func (s *Server) sessionSetlistLocked() {
	s.mu.Lock()
	rec := s.rec
	s.mu.Unlock()
	if rec == nil || len(s.setlist.data.Songs) == 0 {
		return
	}
	if err := writeJSONFile(filepath.Join(rec.Dir(), setlistFile), setlistView{Songs: s.setlist.data.Songs}); err != nil {
		s.log.Warn("setlist: not kept with the session", "session", rec.Dir(), "err", err)
	}
}

// writeJSONFile writes v to path through a temporary file: a crash never
// leaves half a file.
func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// cleanSongs trims the songs and drops the empty ones.
func cleanSongs(in []string) ([]string, error) {
	out := []string{}
	for _, song := range in {
		song = strings.Join(strings.Fields(song), " ")
		switch {
		case song == "":
			continue
		case utf8.RuneCountInString(song) > maxSongChars:
			return out, fmt.Errorf("%q: a song name takes %d characters at most", song, maxSongChars)
		case len(out) == maxSongs:
			return out, fmt.Errorf("a setlist holds %d songs at most", maxSongs)
		}
		out = append(out, song)
	}
	return out, nil
}

// songLabel is the marker of a song: "3. Song name".
func songLabel(i int, song string) string { return fmt.Sprintf("%d. %s", i+1, song) }

func (s *Server) setlistRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setlist", s.getSetlist)
	mux.HandleFunc("PUT /api/setlist", s.putSetlist)
	mux.HandleFunc("POST /api/setlist/next", s.setlistNext)
}

func (s *Server) getSetlist(w http.ResponseWriter, r *http.Request) {
	s.setlist.mu.Lock()
	v := s.setlistLocked()
	s.setlist.mu.Unlock()
	writeJSON(w, http.StatusOK, v)
}

// putSetlist replaces the songs: {"songs": [...], "next"?: n}; without
// next, the next song stays the same one when it is still in the list.
func (s *Server) putSetlist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Songs []string `json:"songs"`
		Next  *int     `json:"next"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"songs": [...], "next": n}`))
		return
	}
	songs, err := cleanSongs(req.Songs)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	s.setlist.mu.Lock()
	defer s.setlist.mu.Unlock()
	old := s.setlistLocked()
	next := old.Next
	switch {
	case req.Next != nil:
		next = *req.Next
	case len(old.Songs) == 0:
		next = 0
	case old.Next < len(old.Songs):
		// the same song is next, wherever it went
		if i := slices.Index(songs, old.Songs[old.Next]); i >= 0 {
			next = i
		}
	default:
		next = len(songs) // all marked: stays so
	}
	v := setlistView{Songs: songs, Next: min(max(next, 0), len(songs))}
	if err := s.saveSetlistLocked(v); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info("setlist", "songs", len(songs), "next", v.Next)
	writeJSON(w, http.StatusOK, s.setlist.data)
}

// setlistNext moves the next song: {"next": n} (back one song, skip one).
func (s *Server) setlistNext(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Next *int `json:"next"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Next == nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"next": n}`))
		return
	}
	s.setlist.mu.Lock()
	defer s.setlist.mu.Unlock()
	v := s.setlistLocked()
	v.Next = min(max(*req.Next, 0), len(v.Songs))
	if err := s.saveSetlistLocked(v); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.setlist.data)
}

// markView is the marker just added.
type markView struct {
	Label string  `json:"label"`
	T     float64 `json:"t"` // seconds after the session's start
}

// addMarker adds a marker to rec: label, or the song of the setlist the
// request names ("next": the next song, "song": that one, from 0), then
// the next song is the one after it. A song asked for when there is none
// left gives a plain timed marker.
func (s *Server) addMarker(rec *session.Recorder, label string, next bool, song *int) (markView, error) {
	s.setlist.mu.Lock()
	defer s.setlist.mu.Unlock()
	v := s.setlistLocked()
	i := -1
	switch {
	case song != nil:
		if *song < 0 || *song >= len(v.Songs) {
			return markView{}, fmt.Errorf("no song %d in the setlist", *song+1)
		}
		i = *song
	case next && label == "" && v.Next < len(v.Songs):
		i = v.Next
	}
	if i >= 0 {
		label = songLabel(i, v.Songs[i])
	}
	t := rec.Elapsed().Seconds()
	if label == "" {
		label = "Marker " + rec.Elapsed().Round(time.Second).String()
	}
	rec.Mark(label)
	if i >= 0 {
		v.Next = i + 1
		if err := s.saveSetlistLocked(v); err != nil {
			s.log.Warn("setlist: the next song is not kept", "err", err)
		}
	} else {
		s.sessionSetlistLocked()
	}
	return markView{Label: label, T: t}, nil
}

// sessionSetlist reads the songs a session was recorded with.
func sessionSetlist(dir string) []string {
	b, err := os.ReadFile(filepath.Join(dir, setlistFile))
	if err != nil {
		return nil
	}
	var v setlistView
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	songs, _ := cleanSongs(v.Songs)
	return songs
}
