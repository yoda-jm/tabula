package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
)

// The console's library: scenes, snippets and presets. Their listings are
// mirrored nodes (/-show/showfile/scene/NNN, …/snippet/NNN, /-libs/ch/NNN…),
// read with /api/state like any other; POST /api/library sends a command
// (save, load, copy, delete, rename) and, once the console has answered,
// reads again what it changed: the listing, or what a recall loaded.

func (s *Server) libraryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/library", s.library)
}

// libraryRequest is one library command; see docs/API.md, "Library".
type libraryRequest struct {
	Op     string `json:"op"`     // save, load, copy, delete, rename
	Kind   string `json:"kind"`   // scene, snippet, libchan, libfx, librout, libmon
	Index  int    `json:"index"`  // slot 0-99
	Name   string `json:"name"`   // save, rename
	Note   string `json:"note"`   // save scene
	To     *int   `json:"to"`     // copy: destination slot
	Target *int   `json:"target"` // libchan: strip 0-71, libfx: FX slot 0-7, saved from or loaded to
	Scope  *int   `json:"scope"`  // load libchan: bits of what to load, 0-63 (default all)
}

// libListings are the listing nodes of each element type, and the number
// their slot 0 has in the node path (presets count from 1).
var libListings = map[string]struct {
	node  string
	first int
}{
	"scene":   {"/-show/showfile/scene", 0},
	"snippet": {"/-show/showfile/snippet", 0},
	"libchan": {"/-libs/ch", 1},
	"libfx":   {"/-libs/fx", 1},
	"librout": {"/-libs/r", 1},
	"libmon":  {"/-libs/mon", 1},
}

func listingPath(kind string, slot int) string {
	l := libListings[kind]
	return fmt.Sprintf("%s/%03d", l.node, slot+l.first)
}

// stripPath maps a strip index (0-71: channels, aux ins, FX returns, buses,
// matrices, LR, M/C) to its path.
func stripPath(i int) string {
	switch {
	case i < 0:
	case i < 32:
		return fmt.Sprintf("/ch/%02d", i+1)
	case i < 40:
		return fmt.Sprintf("/auxin/%02d", i-31)
	case i < 48:
		return fmt.Sprintf("/fxrtn/%02d", i-39)
	case i < 64:
		return fmt.Sprintf("/bus/%02d", i-47)
	case i < 70:
		return fmt.Sprintf("/mtx/%02d", i-63)
	case i == 70:
		return "/main/st"
	case i == 71:
		return "/main/m"
	}
	return ""
}

func inRange(p *int, lo, hi int) bool { return p != nil && *p >= lo && *p <= hi }

// command checks a request and builds the console command, as in the
// protocol document: "/save ,siss scene 3 name note", "/load ,siii libchan
// 4 11 63"…
func (q libraryRequest) command() (string, []osc.Arg, error) {
	if _, ok := libListings[q.Kind]; !ok {
		return "", nil, fmt.Errorf("unknown kind %q (scene, snippet, libchan, libfx, librout, libmon)", q.Kind)
	}
	if q.Index < 0 || q.Index > 99 {
		return "", nil, errors.New("index must be in [0, 99]")
	}
	args := []osc.Arg{osc.String(q.Kind), osc.Int(int32(q.Index))}
	target := func(what string) error {
		switch {
		case q.Kind == "libchan" && !inRange(q.Target, 0, 71):
			return fmt.Errorf("%s a channel preset needs a target strip in [0, 71]", what)
		case q.Kind == "libfx" && !inRange(q.Target, 0, 7):
			return fmt.Errorf("%s an effect preset needs a target FX slot in [0, 7]", what)
		case q.Kind == "libchan" || q.Kind == "libfx":
			args = append(args, osc.Int(int32(*q.Target)))
		}
		return nil
	}
	name := strings.TrimSpace(q.Name)
	switch q.Op {
	case "save":
		if name == "" {
			return "", nil, errors.New("save needs a name")
		}
		args = append(args, osc.String(name))
		if q.Kind == "scene" {
			args = append(args, osc.String(strings.TrimSpace(q.Note)))
		}
		if err := target("saving"); err != nil {
			return "", nil, err
		}
	case "load":
		if err := target("loading"); err != nil {
			return "", nil, err
		}
		if q.Kind == "libchan" {
			scope := 63
			if q.Scope != nil {
				if !inRange(q.Scope, 0, 63) {
					return "", nil, errors.New("scope must be in [0, 63]")
				}
				scope = *q.Scope
			}
			args = append(args, osc.Int(int32(scope)))
		}
	case "copy":
		if q.Kind == "snippet" {
			return "", nil, errors.New("snippets cannot be copied: load and save them instead")
		}
		if !inRange(q.To, 0, 99) {
			return "", nil, errors.New("copy needs a destination in [0, 99]")
		}
		args = append(args, osc.Int(int32(*q.To)))
	case "delete":
	case "rename":
		if name == "" {
			return "", nil, errors.New("rename needs a name")
		}
		args = append(args, osc.String(name))
	default:
		return "", nil, fmt.Errorf("unknown op %q (save, load, copy, delete, rename)", q.Op)
	}
	return "/" + q.Op, args, nil
}

// changed selects the nodes a successful command changed on the console.
func (q libraryRequest) changed(c *console.Client) func(*schema.Node) bool {
	prefixes := []string{}
	exact := map[string]bool{listingPath(q.Kind, q.Index): true}
	if q.Op == "copy" {
		exact[listingPath(q.Kind, *q.To)] = true
	}
	if q.Op == "load" {
		switch q.Kind {
		case "scene", "snippet":
			exact = map[string]bool{"/-show/prepos": true, "/-insert": true}
			prefixes = []string{"/-ha/"}
			return func(n *schema.Node) bool { return n.InScene || exact[n.Path] || hasPrefix(n.Path, prefixes) }
		case "libchan":
			strip := stripPath(*q.Target)
			prefixes = []string{strip + "/", "/-ha/"}
			exact = map[string]bool{"/-insert": true}
			if strings.HasPrefix(strip, "/ch/") || strings.HasPrefix(strip, "/auxin/") {
				n, _ := strconv.Atoi(strip[strings.LastIndexByte(strip, '/')+1:])
				if strings.HasPrefix(strip, "/auxin/") {
					n += 32
				}
				if v, _, err := c.Mirror().Get(fmt.Sprintf("/-ha/%02d/index", n-1)); err == nil && v.Int >= 0 {
					exact[fmt.Sprintf("/headamp/%03d", v.Int)] = true
				}
			}
		case "libfx":
			prefixes = []string{fmt.Sprintf("/fx/%d", *q.Target+1)}
		case "librout":
			prefixes = []string{"/config/routing", "/config/userrout", "/-ha/"}
			exact = map[string]bool{}
		case "libmon":
			prefixes = []string{"/config/dp48"}
			exact = map[string]bool{}
		}
	}
	return func(n *schema.Node) bool { return exact[n.Path] || hasPrefix(n.Path, prefixes) }
}

func hasPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// library runs one library command. Listings are read again before the
// answer; what a recall changed is read again in the background (the
// changes come on the event stream).
func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	var req libraryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"op": ..., "kind": ..., "index": ...}`))
		return
	}
	cmd, args, err := req.command()
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c == nil {
		httpError(w, http.StatusConflict, errors.New("not connected"))
		return
	}
	err = c.Library(r.Context(), cmd, args...)
	switch {
	case errors.Is(err, console.ErrReadOnly):
		httpError(w, http.StatusForbidden, err)
		return
	case errors.Is(err, console.ErrRefused):
		httpError(w, http.StatusUnprocessableEntity, err)
		return
	case errors.Is(err, console.ErrTimeout):
		httpError(w, http.StatusGatewayTimeout, err)
		return
	case err != nil:
		httpError(w, http.StatusBadGateway, err)
		return
	}
	match := req.changed(c)
	refresh := func(ctx context.Context) {
		if err := c.Refresh(ctx, match); err != nil {
			s.log.Warn("library: reading back", "op", req.Op, "kind", req.Kind, "err", err)
		}
	}
	if req.Op == "load" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			refresh(ctx)
		}()
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		refresh(ctx)
		cancel()
	}
	w.WriteHeader(http.StatusNoContent)
}
