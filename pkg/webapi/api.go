// Package webapi is the HTTP API of the tabula client: connection
// management, the parameter schema, the mirrored console state, writes, and
// a Server-Sent Events stream of changes.
package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/session"
	"github.com/yoda-jm/tabula/pkg/state"
)

// Server holds at most one console connection, shared by every browser.
type Server struct {
	log    *slog.Logger
	model  schema.Model
	schema *schema.Schema

	mu         sync.Mutex
	client     *console.Client
	mirror     *state.Store
	cancel     context.CancelFunc
	connecting bool
	lastErr    string
	cancelSub  func()

	hub hub

	schemaOnce sync.Once
	schemaJSON []byte

	sessionsDir  string
	rec          *session.Recorder
	recordErr    string
	replayCancel context.CancelFunc
	replayOf     string
	replayAt     float64
	recordOnSync bool

	acc        access
	listenAddr string
}

func New(log *slog.Logger) *Server {
	s := &Server{log: log, model: "M32C", schema: schema.Build("M32C"), sessionsDir: "sessions"}
	s.hub.init()
	s.acc.init()
	return s
}

// Handler returns the API routes (mounted under /api/) and serves ui for
// everything else. Every API request is checked against the caller's role
// (see access.go).
func (s *Server) Handler(ui http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.getStatus)
	mux.HandleFunc("GET /api/discover", s.discover)
	mux.HandleFunc("POST /api/connect", s.connect)
	mux.HandleFunc("POST /api/disconnect", s.disconnect)
	mux.HandleFunc("POST /api/arm", s.arm)
	mux.HandleFunc("GET /api/schema", s.getSchema)
	mux.HandleFunc("GET /api/state", s.getState)
	mux.HandleFunc("POST /api/set", s.set)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/record", s.getRecord)
	mux.HandleFunc("POST /api/record/start", s.recordStart)
	mux.HandleFunc("POST /api/record/stop", s.recordStop)
	mux.HandleFunc("POST /api/record/mark", s.recordMark)
	mux.HandleFunc("GET /api/sessions", s.listSessions)
	mux.HandleFunc("GET /api/sessions/{name}", s.getSession)
	mux.HandleFunc("POST /api/replay/start", s.replayStart)
	mux.HandleFunc("POST /api/replay/stop", s.replayStop)
	s.accessRoutes(mux)
	s.libraryRoutes(mux)
	mux.Handle("/", ui)
	return gzipJSON(s.guard(mux))
}

// Connect connects to a console and starts syncing it (also used by -console).
func (s *Server) Connect(addr string) error {
	s.mu.Lock()
	if s.connecting {
		s.mu.Unlock()
		return errors.New("already connecting")
	}
	s.closeLocked()
	s.connecting, s.lastErr = true, ""
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.mu.Unlock()
	s.publishStatus()

	go func() {
		mirror := state.New(schema.Build(s.model))
		dctx, dcancel := context.WithTimeout(ctx, 5*time.Second)
		c, err := console.Dial(dctx, addr, mirror, s.log)
		dcancel()
		if err != nil {
			s.mu.Lock()
			s.connecting, s.lastErr = false, err.Error()
			s.mu.Unlock()
			s.publishStatus()
			return
		}
		s.mu.Lock()
		s.client, s.mirror = c, mirror
		s.mu.Unlock()
		s.publishStatus()

		last := time.Time{}
		c.Sync(ctx, func(done, total int) {
			if time.Since(last) > 200*time.Millisecond || done == total {
				last = time.Now()
				s.publishStatus()
			}
		})
		unsub := mirror.Subscribe(s.publishChanges)
		s.mu.Lock()
		s.connecting, s.cancelSub = false, unsub
		s.mu.Unlock()
		s.publishStatus()
		s.hub.publish("resync", json.RawMessage(`{}`))
		s.mu.Lock()
		auto := s.recordOnSync
		s.mu.Unlock()
		if auto {
			if err := s.StartRecording("started with -record"); err != nil {
				s.log.Error("recording", "err", err)
			}
			s.publishRecord()
		}
		go s.runMeters(ctx, c)
		c.OnStatus(s.publishStatus)
		c.OnReconnect(func() {
			s.publishStatus()
			s.hub.publish("resync", json.RawMessage(`{}`))
			select { // subscribe the wanted meters again
			case s.hub.meterKick <- struct{}{}:
			default:
			}
		})
		c.Run(ctx)
	}()
	return nil
}

// RecordOnSync starts a recording as soon as a console is connected and synced.
func (s *Server) RecordOnSync(on bool) {
	s.mu.Lock()
	s.recordOnSync = on
	s.mu.Unlock()
}

func (s *Server) closeLocked() {
	if s.rec != nil {
		s.rec.Stop()
		s.rec = nil
	}
	if s.replayCancel != nil {
		s.replayCancel()
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.cancelSub != nil {
		s.cancelSub()
		s.cancelSub = nil
	}
	if s.client != nil {
		s.client.Close()
		s.client, s.mirror = nil, nil
	}
	s.connecting = false
}

// statusView is what /api/status returns.
type statusView struct {
	Connected  bool            `json:"connected"`
	Connecting bool            `json:"connecting"`
	Error      string          `json:"error,omitempty"`
	Console    *console.Status `json:"console,omitempty"`
}

func (s *Server) status() statusView {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := statusView{Connecting: s.connecting, Error: s.lastErr}
	if s.client != nil {
		st := s.client.Status()
		v.Console = &st
		v.Connected = st.Connected && !s.connecting
	}
	return v
}

func (s *Server) publishStatus() { s.hub.publish("status", s.status()) }

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.status())
}

func (s *Server) discover(w http.ResponseWriter, r *http.Request) {
	found, err := console.Discover(r.Context(), 1500*time.Millisecond)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	if found == nil {
		found = []console.Info{}
	}
	writeJSON(w, http.StatusOK, found)
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Addr string `json:"addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Addr) == "" {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"addr": "host[:port]"}`))
		return
	}
	if err := s.Connect(strings.TrimSpace(req.Addr)); err != nil {
		httpError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusAccepted, s.status())
}

func (s *Server) disconnect(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.closeLocked()
	s.mu.Unlock()
	s.publishStatus()
	writeJSON(w, http.StatusOK, s.status())
}

func (s *Server) arm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Armed bool `json:"armed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"armed": true|false}`))
		return
	}
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c == nil {
		httpError(w, http.StatusConflict, errors.New("not connected"))
		return
	}
	c.Arm(req.Armed)
	s.publishStatus()
	writeJSON(w, http.StatusOK, s.status())
}

// typeView describes a parameter type for the GUI.
type typeView struct {
	Kind     string   `json:"kind"` // enum, int, bitmap, string, lin, log, level, raw, unused
	Describe string   `json:"describe"`
	Names    []string `json:"names,omitempty"` // enum
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Steps    int      `json:"steps,omitempty"` // floats: number of known values (0 = continuous)
	Bits     int      `json:"bits,omitempty"`
	Unit     string   `json:"unit,omitempty"`
}

func ptr(v float64) *float64 { return &v }

func describeType(t schema.Type) typeView {
	v := typeView{Describe: t.Describe()}
	switch t := t.(type) {
	case schema.Enum:
		v.Kind, v.Names = "enum", t.Names
	case schema.Int:
		v.Kind, v.Min, v.Max = "int", ptr(float64(t.Min)), ptr(float64(t.Max))
	case schema.Bitmap:
		v.Kind, v.Bits = "bitmap", t.Bits
	case schema.Str:
		v.Kind = "string"
		if t.Max > 0 {
			v.Max = ptr(float64(t.Max))
		}
	case schema.Num:
		v.Steps, v.Unit = t.Steps, t.Unit
		switch sc := t.Scale.(type) {
		case schema.Linf:
			v.Kind, v.Min, v.Max = "lin", ptr(sc.Min), ptr(sc.Max)
		case schema.Logf:
			v.Kind, v.Min, v.Max = "log", ptr(sc.Min), ptr(sc.Max)
		case schema.Level:
			v.Kind = "level"
		}
	case schema.Unused:
		v.Kind = "unused"
	default:
		v.Kind = "raw"
	}
	return v
}

// fieldView is a field of a node; Type indexes the schema's type table
// (/api/schema), or is inlined as TypeInfo for variant nodes (/api/state).
type fieldView struct {
	Name     string    `json:"name"`
	Label    string    `json:"label,omitempty"`
	Type     int       `json:"type"`
	TypeInfo *typeView `json:"type_info,omitempty"`
}

type nodeView struct {
	Path    string      `json:"path"`
	Scene   bool        `json:"scene"`
	Variant string      `json:"variant,omitempty"` // selector address
	Fields  []fieldView `json:"fields"`
}

type schemaView struct {
	Model schema.Model `json:"model"`
	Types []typeView   `json:"types"`
	Nodes []nodeView   `json:"nodes"`
}

func buildSchemaView(sch *schema.Schema) schemaView {
	sv := schemaView{Model: sch.Model}
	index := map[string]int{}
	for _, n := range sch.Nodes {
		nv := nodeView{Path: n.Path, Scene: n.InScene}
		if n.Variant != nil {
			nv.Variant = n.Variant.Selector
		}
		for _, f := range n.Fields {
			tv := describeType(f.Type)
			key, _ := json.Marshal(tv)
			i, ok := index[string(key)]
			if !ok {
				i = len(sv.Types)
				index[string(key)] = i
				sv.Types = append(sv.Types, tv)
			}
			nv.Fields = append(nv.Fields, fieldView{Name: f.Name, Label: f.Label, Type: i})
		}
		sv.Nodes = append(sv.Nodes, nv)
	}
	return sv
}

// getSchema returns every node with its static fields. Variant nodes
// (effect parameters) get their current fields from /api/state.
func (s *Server) getSchema(w http.ResponseWriter, r *http.Request) {
	s.schemaOnce.Do(func() { s.schemaJSON, _ = json.Marshal(buildSchemaView(s.schema)) })
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=3600")
	w.Write(s.schemaJSON)
}

// valueView is one parameter value: its node text and its raw value
// (normalized float, int index or string).
type valueView struct {
	Addr  string `json:"addr"`
	Text  string `json:"text"`
	Value any    `json:"value"`
}

func argValue(a osc.Arg) any {
	switch a.Tag {
	case 'i':
		return a.Int
	case 'f':
		f := float64(a.Float)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0
		}
		return f
	case 's':
		return a.Str
	}
	return nil
}

type stateNode struct {
	Path   string      `json:"path"`
	Line   string      `json:"line"`
	Fields []fieldView `json:"fields,omitempty"` // only for variant nodes
	Values []valueView `json:"values"`
	Text   bool        `json:"text,omitempty"` // kept as text: not modelled
}

// getState returns the mirrored values of the nodes under ?prefix= (all
// nodes without a prefix).
func (s *Server) getState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	mirror := s.mirror
	s.mu.Unlock()
	if mirror == nil {
		httpError(w, http.StatusConflict, errors.New("not connected"))
		return
	}
	prefix := r.URL.Query().Get("prefix")
	out := []stateNode{}
	for _, n := range mirror.Schema().Nodes {
		if prefix != "" && !strings.HasPrefix(n.Path, prefix) {
			continue
		}
		sn := stateNode{Path: n.Path, Line: mirror.NodeLine(n)}
		fs, ok := mirror.Fields(n)
		if !ok {
			sn.Text = true
			out = append(out, sn)
			continue
		}
		if n.Variant != nil {
			for _, f := range fs {
				tv := describeType(f.Type)
				sn.Fields = append(sn.Fields, fieldView{Name: f.Name, Label: f.Label, Type: -1, TypeInfo: &tv})
			}
		}
		vals := mirror.Values(n)
		for i, f := range fs {
			sn.Values = append(sn.Values, valueView{n.Path + "/" + f.Name, f.Type.Format(vals[i]), argValue(vals[i])})
		}
		out = append(out, sn)
	}
	writeJSON(w, http.StatusOK, out)
}

// set writes one parameter: {"addr": "...", "text": "-3.0"} or
// {"addr": "...", "value": 0.75 | 3 | "name"} (floats normalized to [0, 1]).
// A musician only writes their own mix, armed or not, when the engineer
// lets musicians change their mix.
func (s *Server) set(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Addr  string          `json:"addr"`
		Text  *string         `json:"text"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Addr == "" {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"addr": ..., "text"|"value": ...}`))
		return
	}
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c == nil {
		httpError(w, http.StatusConflict, errors.New("not connected"))
		return
	}
	who := callerFrom(r)
	unarmed := false
	if who.Role != RoleEngineer {
		switch {
		case who.Grant == nil || who.Role != RoleMusician:
			httpError(w, http.StatusForbidden, errors.New("this access cannot change anything"))
			return
		case !s.acc.snapshot().MusiciansCanWrite:
			httpError(w, http.StatusForbidden, errors.New("the engineer has not let musicians change their mix"))
			return
		case !musicianMay(req.Addr, mixBuses(c.Mirror(), who.Grant.Bus)):
			httpError(w, http.StatusForbidden, errors.New("your access only changes your own mix"))
			return
		}
		unarmed = true
	}
	var arg osc.Arg
	var err error
	switch {
	case req.Text != nil:
		arg, err = c.ParseText(req.Addr, *req.Text)
	case len(req.Value) > 0:
		arg, err = decodeValue(c.Mirror(), req.Addr, req.Value)
	default:
		err = errors.New(`"text" or "value" is required`)
	}
	if err == nil {
		err = c.SetAs(req.Addr, arg, who.origin(), unarmed)
	}
	switch {
	case errors.Is(err, console.ErrReadOnly):
		httpError(w, http.StatusForbidden, err)
	case err != nil:
		httpError(w, http.StatusBadRequest, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// decodeValue turns a JSON value into the OSC argument the parameter takes.
func decodeValue(mirror *state.Store, addr string, raw json.RawMessage) (osc.Arg, error) {
	l := mirror.Schema().Leaf(addr)
	if l == nil {
		return osc.Arg{}, fmt.Errorf("unknown parameter %s", addr)
	}
	t := mirror.TypeOf(l)
	if t == nil {
		return osc.Arg{}, fmt.Errorf("%s: %w", addr, state.ErrNotModelled)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return osc.Arg{}, err
	}
	switch x := v.(type) {
	case string:
		return osc.String(x), nil
	case float64:
		if t.Tag() == 'f' {
			return osc.Float(float32(x)), nil
		}
		return osc.Int(int32(math.Round(x))), nil
	case bool:
		if x {
			return osc.Int(1), nil
		}
		return osc.Int(0), nil
	}
	return osc.Arg{}, fmt.Errorf("unsupported value %s", raw)
}

func (s *Server) publishChanges(ch []state.Change) {
	s.mu.Lock()
	mirror := s.mirror
	s.mu.Unlock()
	if mirror == nil {
		return
	}
	type changeView struct {
		valueView
		Origin string `json:"origin"`
	}
	out := make([]changeView, 0, len(ch))
	for _, c := range ch {
		t := mirror.TypeOf(c.Leaf)
		text := ""
		if t != nil {
			text = t.Format(c.Value)
		}
		out = append(out, changeView{valueView{c.Leaf.Addr, text, argValue(c.Value)}, c.Origin})
	}
	s.hub.publish("change", out)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
