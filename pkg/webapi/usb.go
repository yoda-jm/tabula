package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
)

// The USB stick in the console, browsed and played as on its USB recorder
// screen, with the commands of Maillot's X32USB: /-usb/dir lists the
// current directory (001 to maxpos, "[..]" and "[name]" for directories),
// /-action/recselect n enters a directory, plays a WAV file or loads a
// scene, a snippet, a preset or a show; /-stat/tape/state pauses, plays,
// stops and winds the player, and /-action/playtrack goes to the previous
// or next file. The console does
// not push its listing: it is read again after each change. The player's
// state (/-stat/tape) comes with the other changes. See docs/API.md, "USB
// stick".

type usbEntry struct {
	Index int    `json:"index"` // as recselect numbers it, from 1
	Name  string `json:"name"`  // without the brackets of directories
	// Kind: parent, dir, wav, scene, snippet, show, effect, routing,
	// channel, monitor (AES50/DP48), prefs, volume (System Volume
	// Information) or file.
	Kind string `json:"kind"`
}

type usbView struct {
	Mounted bool       `json:"mounted"`
	Path    string     `json:"path"`  // the current directory's name, "" at the root
	Title   string     `json:"title"` // the file selected
	Pos     int        `json:"pos"`   // its entry
	Entries []usbEntry `json:"entries"`
}

// usbKind is what an entry is, from its name (as X32USB tells; .mon, the
// AES50/DP48 presets, as M32-Edit names them).
func usbKind(name string) (string, string) {
	if strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") {
		inner := name[1 : len(name)-1]
		switch inner {
		case "..", "…":
			return "parent", inner
		case "System Volume Information":
			return "volume", inner
		}
		return "dir", inner
	}
	kinds := map[string]string{".wav": "wav", ".scn": "scene", ".snp": "snippet", ".shw": "show",
		".efx": "effect", ".rou": "routing", ".chn": "channel", ".mon": "monitor", ".prf": "prefs"}
	if k, ok := kinds[strings.ToLower(path.Ext(name))]; ok {
		return k, name
	}
	return "file", name
}

// usbLoads are the files whose opening loads settings into the console:
// they are backed up first (scenebackup.go).
var usbLoads = map[string]bool{"scene": true, "snippet": true, "show": true, "effect": true, "routing": true, "channel": true, "monitor": true, "prefs": true}

// usbState keeps one USB command at a time: the listing read after one
// belongs to it.
type usbState struct {
	mu sync.Mutex
}

func (s *Server) usbRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/usb", s.getUSB)
	mux.HandleFunc("POST /api/usb", s.usbCommand)
}

func (s *Server) usbClient(w http.ResponseWriter) *console.Client {
	s.mu.Lock()
	c, connecting := s.client, s.connecting
	s.mu.Unlock()
	if c == nil || connecting {
		httpError(w, http.StatusConflict, errors.New("not connected (or still syncing)"))
		return nil
	}
	return c
}

// usbWindow is how many names are asked at a time.
const usbWindow = 16

// readUSB asks the console for its USB stick and the current directory.
func readUSB(ctx context.Context, c *console.Client) (usbView, error) {
	v := usbView{Entries: []usbEntry{}}
	m, err := c.Get(ctx, "/-stat/usbmounted")
	if err != nil {
		return v, err
	}
	if v.Mounted = m.Int == 1; !v.Mounted {
		return v, nil
	}
	str := func(addr string) (string, error) {
		a, err := c.Get(ctx, addr)
		return a.Str, err
	}
	if v.Path, err = str("/-usb/path"); err != nil {
		return v, err
	}
	if v.Title, err = str("/-usb/title"); err != nil {
		return v, err
	}
	pos, err := c.Get(ctx, "/-usb/dir/dirpos")
	if err != nil {
		return v, err
	}
	v.Pos = int(pos.Int)
	maxpos, err := c.Get(ctx, "/-usb/dir/maxpos")
	if err != nil {
		return v, err
	}
	n := min(max(int(maxpos.Int), 0), 999)
	names := make([]string, n)
	errs := make([]error, n)
	sem := make(chan struct{}, usbWindow)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			names[i], errs[i] = str(fmt.Sprintf("/-usb/dir/%03d/name", i+1))
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return v, err
	}
	for i, name := range names {
		if name == "" {
			continue
		}
		kind, shown := usbKind(name)
		v.Entries = append(v.Entries, usbEntry{Index: i + 1, Name: shown, Kind: kind})
	}
	return v, nil
}

func (s *Server) getUSB(w http.ResponseWriter, r *http.Request) {
	c := s.usbClient(w)
	if c == nil {
		return
	}
	s.usb.mu.Lock()
	defer s.usb.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	v, err := readUSB(ctx, c)
	if err != nil {
		httpError(w, http.StatusGatewayTimeout, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// usbSettle is how long the console takes to read a directory of the
// stick, at most: the listing is read again once its path changed, or
// after this.
var usbSettle = 1500 * time.Millisecond

// usbCommand runs one command: open (an entry: enters a directory, plays
// a WAV file, loads a scene…), play, pause, stop, ff, rew (fast forward and
// rewind, until play, pause or stop), prev, next. It changes the console,
// so writes must be armed. open answers the listing after it.
func (s *Server) usbCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Op    string `json:"op"`
		Index int    `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"op": ..., "index": ...}`))
		return
	}
	c := s.usbClient(w)
	if c == nil {
		return
	}
	s.usb.mu.Lock()
	defer s.usb.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var addr string
	var val int32
	switch req.Op {
	case "open":
		addr, val = "/-action/recselect", int32(req.Index)
	case "play", "pause", "stop", "ff", "rew":
		addr, val = "/-stat/tape/state", map[string]int32{"stop": 0, "pause": 1, "play": 2, "ff": 5, "rew": 6}[req.Op]
	case "prev", "next":
		addr, val = "/-action/playtrack", map[string]int32{"prev": -1, "next": 1}[req.Op]
	default:
		httpError(w, http.StatusBadRequest, fmt.Errorf("unknown op %q (open, play, pause, stop, ff, rew, prev, next)", req.Op))
		return
	}
	var before usbView
	if req.Op == "open" {
		var err error
		if before, err = readUSB(ctx, c); err != nil {
			httpError(w, http.StatusGatewayTimeout, err)
			return
		}
		if !before.Mounted {
			httpError(w, http.StatusConflict, errors.New("no USB stick in the console"))
			return
		}
	}
	var entry *usbEntry
	for i := range before.Entries {
		if before.Entries[i].Index == req.Index {
			entry = &before.Entries[i]
		}
	}
	if req.Op == "open" && entry == nil {
		httpError(w, http.StatusBadRequest, fmt.Errorf("no entry %d in this directory", req.Index))
		return
	}
	if entry != nil && usbLoads[entry.Kind] && c.Status().Armed {
		s.backupBefore(fmt.Sprintf("loading %s from the USB stick", entry.Name))
	}
	err := c.Set(addr, osc.Int(val))
	switch {
	case errors.Is(err, console.ErrReadOnly):
		httpError(w, http.StatusForbidden, err)
		return
	case err != nil:
		httpError(w, http.StatusBadGateway, err)
		return
	}
	s.log.Info("usb", "op", req.Op, "entry", req.Index)
	if req.Op != "open" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch entry.Kind {
	case "parent", "dir":
		// the console reads the directory: wait for its path to change
		deadline := time.Now().Add(usbSettle)
		for time.Now().Before(deadline) {
			p, err := c.Get(ctx, "/-usb/path")
			if err != nil || p.Str != before.Path {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	case "scene", "snippet", "channel", "effect", "routing", "monitor", "show":
		// what the load changed: read back like a recall (library.go); a
		// show changes the show's listings only
		match := func(n *schema.Node) bool { return n.InScene || strings.HasPrefix(n.Path, "/-ha/") }
		if entry.Kind == "show" {
			match = func(n *schema.Node) bool { return strings.HasPrefix(n.Path, "/-show/") }
		}
		ctx, cancel := s.lib.start(time.Minute)
		go func() {
			defer cancel()
			if err := c.Refresh(ctx, match); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Warn("usb: reading back", "err", err)
			}
		}()
	}
	v, err := readUSB(ctx, c)
	if err != nil {
		httpError(w, http.StatusGatewayTimeout, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
