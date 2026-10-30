package webapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// The patch list (Patch list page): a PDF of the inputs and outputs the
// engineer hands to the venue's sound engineer, made by the page from the
// console's routing. tabula keeps what the console does not know: the
// title and the lines under it, the names of the stage boxes, a note per
// row (the mic, the stand) and the rows ticked or unticked by hand, in
// <scenes>/patchlist.json, next to the scene files they go with.

const (
	patchlistFile = "patchlist.json"
	maxPatchText  = 120 // the title, the details, the contact, a box name
	maxPatchNote  = 80
	maxPatchKey   = 40
	maxPatchRows  = 400 // notes, and rows ticked by hand
)

// patchlistView is the patch list's settings: GET and PUT /api/patchlist.
type patchlistView struct {
	Title     string            `json:"title"`
	Details   string            `json:"details"`
	Contact   string            `json:"contact"`
	BoxA      string            `json:"box_a"`
	BoxB      string            `json:"box_b"`
	Paper     string            `json:"paper"` // "A4" or "Letter"
	Watermark bool              `json:"watermark"`
	Notes     map[string]string `json:"notes"` // by row key: "/ch/01", "AES50A:3"
	Rows      map[string]bool   `json:"rows"`
	Updated   time.Time         `json:"updated,omitzero"`
}

// newPatchlist is the patch list before anything is typed; what a saved
// file or a request leaves out keeps these values.
func newPatchlist() patchlistView {
	return patchlistView{Paper: "A4", Watermark: true, Notes: map[string]string{}, Rows: map[string]bool{}}
}

type patchlistState struct {
	mu     sync.Mutex
	dir    string // the scenes folder data was read from
	data   patchlistView
	loaded bool
}

// patchlistLocked returns the patch list of the scenes folder, read on
// first use (or when the folder changed). Called with s.patchlist.mu held.
func (s *Server) patchlistLocked() patchlistView {
	dir := s.scenesFolder()
	l := &s.patchlist
	if l.loaded && l.dir == dir {
		return l.data
	}
	l.dir, l.loaded, l.data = dir, true, newPatchlist()
	path := filepath.Join(dir, patchlistFile)
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("patch list", "err", err)
		}
		return l.data
	}
	v := newPatchlist()
	if err := json.Unmarshal(b, &v); err != nil {
		s.log.Warn("patch list: not understood, starting empty", "file", path, "err", err)
		return l.data
	}
	if err := cleanPatchlist(&v); err != nil {
		s.log.Warn("patch list: not understood, starting empty", "file", path, "err", err)
		return l.data
	}
	l.data = v
	return v
}

// cleanPatchlist tidies the spaces of the texts and checks their lengths
// and the row keys; a note left empty is dropped.
func cleanPatchlist(v *patchlistView) error {
	for _, f := range []struct {
		name string
		s    *string
	}{{"the title", &v.Title}, {"the details", &v.Details}, {"the contact", &v.Contact}, {"the box on AES50 A", &v.BoxA}, {"the box on AES50 B", &v.BoxB}} {
		*f.s = strings.Join(strings.Fields(*f.s), " ")
		if utf8.RuneCountInString(*f.s) > maxPatchText {
			return fmt.Errorf("%s takes %d characters at most", f.name, maxPatchText)
		}
	}
	switch v.Paper {
	case "":
		v.Paper = "A4"
	case "A4", "Letter":
	default:
		return fmt.Errorf("paper %q: A4 or Letter", v.Paper)
	}
	notes := map[string]string{}
	for k, n := range v.Notes {
		if err := patchKey(k); err != nil {
			return err
		}
		n = strings.Join(strings.Fields(n), " ")
		if utf8.RuneCountInString(n) > maxPatchNote {
			return fmt.Errorf("%q: a note takes %d characters at most", n, maxPatchNote)
		}
		if n != "" {
			notes[k] = n
		}
	}
	if len(notes) > maxPatchRows {
		return fmt.Errorf("%d notes at most", maxPatchRows)
	}
	v.Notes = notes
	if v.Rows == nil {
		v.Rows = map[string]bool{}
	}
	for k := range v.Rows {
		if err := patchKey(k); err != nil {
			return err
		}
	}
	if len(v.Rows) > maxPatchRows {
		return fmt.Errorf("%d rows ticked by hand at most", maxPatchRows)
	}
	return nil
}

// patchKey checks a row key: short, printable, without spaces.
func patchKey(k string) error {
	if k == "" || len(k) > maxPatchKey || strings.ContainsFunc(k, func(r rune) bool { return !unicode.IsPrint(r) || unicode.IsSpace(r) }) {
		return fmt.Errorf("%q is not a row of the patch list", k)
	}
	return nil
}

func (s *Server) patchlistRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/patchlist", s.getPatchlist)
	mux.HandleFunc("PUT /api/patchlist", s.putPatchlist)
}

func (s *Server) getPatchlist(w http.ResponseWriter, r *http.Request) {
	s.patchlist.mu.Lock()
	v := s.patchlistLocked()
	s.patchlist.mu.Unlock()
	writeJSON(w, http.StatusOK, v)
}

// putPatchlist replaces the settings; what the request leaves out takes
// its first value (no title, A4, the watermark).
func (s *Server) putPatchlist(w http.ResponseWriter, r *http.Request) {
	v := newPatchlist()
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"title": "...", "notes": {...}, "rows": {...}, ...}`))
		return
	}
	if err := cleanPatchlist(&v); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	v.Updated = time.Now()
	s.patchlist.mu.Lock()
	defer s.patchlist.mu.Unlock()
	s.patchlistLocked() // the folder it belongs to
	if err := writeJSONFile(filepath.Join(s.patchlist.dir, patchlistFile), v); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	s.patchlist.data = v
	s.log.Info("patch list", "title", v.Title, "notes", len(v.Notes), "rows", len(v.Rows))
	writeJSON(w, http.StatusOK, v)
}
