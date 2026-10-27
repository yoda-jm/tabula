package webapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/yoda-jm/tabula/pkg/console"
)

// Copying a strip's sections (its EQ, dynamics, sends…) to other strips of
// the same kind in one step, as the console's channel copy does: each node
// of the sections is sent with the "/" command as node text, as a scene
// file load does. The step can be undone (undo.go).

// copySections are the sections that can be copied, by the node paths
// they hold below the strip ("/eq", "/eq/1"…).
var copySections = map[string]func(rel string) bool{
	"preamp": under("/preamp"),
	"delay":  under("/delay"),
	"gate":   under("/gate"),
	"dyn":    under("/dyn"),
	"eq":     under("/eq"),
	"insert": under("/insert"),
	"sends":  func(rel string) bool { return strings.HasPrefix(rel, "/mix/") },
	"main":   func(rel string) bool { return rel == "/mix" },
}

func under(p string) func(string) bool {
	return func(rel string) bool { return rel == p || strings.HasPrefix(rel, p+"/") }
}

// copyStrip matches the strips a copy goes from and to, and gives their
// kind (L/R and M/C do not have the same nodes: they are not copied).
var copyStrip = regexp.MustCompile(`^/(ch|auxin|fxrtn|bus|mtx)/\d\d$`)

func stripKind(path string) string {
	if m := copyStrip.FindStringSubmatch(path); m != nil {
		return m[1]
	}
	return ""
}

// copyStrips copies sections of a strip to others:
// {"from": "/ch/01", "to": ["/ch/02"], "sections": ["eq", "dyn"]}.
func (s *Server) copyStrips(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From     string   `json:"from"`
		To       []string `json:"to"`
		Sections []string `json:"sections"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"from", "to": [...], "sections": [...]}`))
		return
	}
	kind := stripKind(req.From)
	switch {
	case kind == "":
		httpError(w, http.StatusBadRequest, fmt.Errorf("%q is not a strip that can be copied", req.From))
		return
	case len(req.To) == 0 || len(req.Sections) == 0:
		httpError(w, http.StatusBadRequest, errors.New("choose the strips to copy to and the sections to copy"))
		return
	}
	for _, t := range req.To {
		if stripKind(t) != kind || t == req.From {
			httpError(w, http.StatusBadRequest, fmt.Errorf("%s: a strip copies to other strips of its kind", t))
			return
		}
	}
	for _, sec := range req.Sections {
		if copySections[sec] == nil {
			httpError(w, http.StatusBadRequest, fmt.Errorf("unknown section %q (preamp, delay, gate, dyn, eq, insert, sends, main)", sec))
			return
		}
	}
	s.mu.Lock()
	c, mirror, connecting := s.client, s.mirror, s.connecting
	s.mu.Unlock()
	switch {
	case c == nil || mirror == nil || connecting:
		httpError(w, http.StatusConflict, errors.New("not connected (or still syncing)"))
		return
	case !c.Status().Armed:
		httpError(w, http.StatusForbidden, console.ErrReadOnly)
		return
	}

	// what to send, and the lines putting back what it overwrites
	sch := mirror.Schema()
	var send, before []string
	for _, n := range sch.Nodes {
		rel, ok := strings.CutPrefix(n.Path, req.From)
		if !ok || !slices.ContainsFunc(req.Sections, func(sec string) bool { return copySections[sec](rel) }) {
			continue
		}
		line := mirror.NodeLine(n)
		for _, t := range req.To {
			if tn := sch.Node(t + rel); tn != nil {
				send = append(send, t+rel+strings.TrimPrefix(line, n.Path))
				before = append(before, mirror.NodeLine(tn))
			}
		}
	}
	if len(send) == 0 {
		httpError(w, http.StatusBadRequest, errors.New("these strips have none of these sections"))
		return
	}
	for i, line := range send {
		if err := c.ApplyText(line); err != nil {
			if i > 0 {
				s.undo.push(mirror, &undoStep{what: copyWhat(req.From, req.To, req.Sections) + " (stopped)", at: time.Now(), lines: before[:i]})
				s.publishUndo()
			}
			httpError(w, http.StatusBadGateway, fmt.Errorf("stopped at %s: %w", line, err))
			return
		}
	}
	s.undo.push(mirror, &undoStep{what: copyWhat(req.From, req.To, req.Sections), at: time.Now(), lines: before})
	s.publishUndo()
	s.log.Info("strip copied", "from", req.From, "to", strings.Join(req.To, ","), "sections", strings.Join(req.Sections, ","), "nodes", len(send))
	writeJSON(w, http.StatusOK, map[string]int{"nodes": len(send)})
}

func copyWhat(from string, to, sections []string) string {
	return fmt.Sprintf("copy of %s (%s) to %s", from, strings.Join(sections, ", "), strings.Join(to, ", "))
}
