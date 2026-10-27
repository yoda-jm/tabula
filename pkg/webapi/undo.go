package webapi

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/state"
)

// Undo: the engineer's last changes made from tabula (a fader, a field, a
// strip copied to others) are undone newest first, each step putting back
// the values it overwrote. The console's own changes, the musicians' and
// what a recall or a scene file load changed are not steps: the backups
// before a load put those back (scenebackup.go).

// undoSteps is how many steps are kept.
const undoSteps = 50

// undoMerge: writes to one parameter closer than this are one step (a fader drag).
const undoMerge = 3 * time.Second

type undoStep struct {
	what  string
	at    time.Time
	lines []string // node text putting back what the step overwrote
	addr  string   // a step of one parameter: its address, to merge the next writes
	last  time.Time
}

type undoState struct {
	mu     sync.Mutex
	mirror *state.Store // the steps belong to one connection
	steps  []*undoStep
}

// undoView is what GET /api/undo and the "undo" events tell.
type undoView struct {
	Count int       `json:"count"`
	What  string    `json:"what,omitempty"` // the step Undo undoes
	At    time.Time `json:"at,omitzero"`
}

func (u *undoState) viewLocked() undoView {
	v := undoView{Count: len(u.steps)}
	if n := len(u.steps); n > 0 {
		v.What, v.At = u.steps[n-1].what, u.steps[n-1].at
	}
	return v
}

// push adds a step, or merges a write into the last step when it writes
// the same parameter soon after; it reports whether what Undo tells changed.
func (u *undoState) push(mirror *state.Store, st *undoStep) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.mirror != mirror {
		u.mirror, u.steps = mirror, nil
	}
	if n := len(u.steps); n > 0 && st.addr != "" {
		if top := u.steps[n-1]; top.addr == st.addr && st.at.Sub(top.last) < undoMerge {
			top.last = st.at
			return false
		}
	}
	st.last = st.at
	u.steps = append(u.steps, st)
	if len(u.steps) > undoSteps {
		u.steps = u.steps[len(u.steps)-undoSteps:]
	}
	return true
}

// noteWrite records a write of the engineer's: prev is the parameter's
// line before it ("/ch/12/mix/fader -10.0").
func (s *Server) noteWrite(mirror *state.Store, addr, prev string) {
	l := mirror.Schema().Leaf(addr)
	if l == nil || mirror.LeafLine(l) == prev {
		return
	}
	_, was := splitLine(prev)
	if s.undo.push(mirror, &undoStep{what: fmt.Sprintf("%s (was %s)", addr, was), at: time.Now(), lines: []string{prev}, addr: addr}) {
		s.publishUndo()
	}
}

// splitLine cuts node text into its address and its values.
func splitLine(line string) (addr, rest string) {
	for i := 0; i < len(line); i++ {
		if line[i] == ' ' {
			j := i
			for j < len(line) && line[j] == ' ' {
				j++
			}
			return line[:i], line[j:]
		}
	}
	return line, ""
}

func (s *Server) publishUndo() {
	s.undo.mu.Lock()
	v := s.undo.viewLocked()
	s.undo.mu.Unlock()
	s.hub.publishEngineers("undo", v)
}

func (s *Server) getUndo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	mirror := s.mirror
	s.mu.Unlock()
	s.undo.mu.Lock()
	if s.undo.mirror != mirror {
		s.undo.mirror, s.undo.steps = mirror, nil
	}
	v := s.undo.viewLocked()
	s.undo.mu.Unlock()
	writeJSON(w, http.StatusOK, v)
}

// postUndo undoes the newest step: its lines are sent to the console.
func (s *Server) postUndo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	c, mirror := s.client, s.mirror
	s.mu.Unlock()
	if c == nil || mirror == nil {
		httpError(w, http.StatusConflict, errors.New("not connected"))
		return
	}
	if !c.Status().Armed {
		httpError(w, http.StatusForbidden, console.ErrReadOnly)
		return
	}
	s.undo.mu.Lock()
	n := len(s.undo.steps)
	if s.undo.mirror != mirror || n == 0 {
		s.undo.mu.Unlock()
		httpError(w, http.StatusConflict, errors.New("nothing to undo"))
		return
	}
	st := s.undo.steps[n-1]
	s.undo.steps = s.undo.steps[:n-1]
	s.undo.mu.Unlock()
	for i, line := range st.lines {
		if err := c.ApplyText(line); err != nil {
			if i == 0 { // nothing changed: the step stays
				s.undo.mu.Lock()
				s.undo.steps = append(s.undo.steps, st)
				s.undo.mu.Unlock()
			}
			httpError(w, http.StatusBadGateway, fmt.Errorf("undo %s: %w", st.what, err))
			s.publishUndo()
			return
		}
	}
	s.log.Info("undo", "step", st.what, "lines", len(st.lines))
	s.publishUndo()
	s.getUndo(w, r)
}
