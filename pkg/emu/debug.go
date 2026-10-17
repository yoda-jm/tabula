package emu

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yoda-jm/tabula/pkg/scene"
)

// DebugHandler serves a read-only view of the emulator: the stored state,
// clients, traffic and unhandled requests. It is a debugging aid, not a
// console UI: nothing can be changed from it.
func (s *Server) DebugHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.debugPage)
	mux.HandleFunc("GET /scene.scn", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `inline; filename="tabula-emu.scn"`)
		scene.Write(w, s.store, scene.Header("tabula-emu", time.Now().Format(time.DateTime)))
	})
	mux.HandleFunc("GET /api/unhandled", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.Unhandled.List()) })
	mux.HandleFunc("GET /api/clients", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.Clients()) })
	mux.HandleFunc("GET /api/subscriptions", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.Subscriptions()) })
	mux.HandleFunc("GET /api/traffic", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		writeJSON(w, s.Traffic(cmp(n, 200)))
	})
	mux.HandleFunc("GET /api/node", func(w http.ResponseWriter, r *http.Request) {
		n := s.store.Schema().Resolve(r.URL.Query().Get("path"))
		if n == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(s.store.NodeLine(n) + "\n"))
	})
	return mux
}

func cmp(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

type debugNode struct {
	Path, Line string
	Params     []debugParam
}

type debugParam struct{ Name, Value, Type string }

func (s *Server) debugPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 300
	var nodes []debugNode
	total := 0
	for _, n := range s.store.Schema().Nodes {
		line := s.store.NodeLine(n)
		if q != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(q)) {
			continue
		}
		total++
		if len(nodes) >= limit {
			continue
		}
		dn := debugNode{Path: n.Path, Line: line}
		vals := s.store.Values(n)
		for i, f := range n.Fields {
			dn.Params = append(dn.Params, debugParam{f.Name, f.Type.Format(vals[i]), f.Type.Describe()})
		}
		nodes = append(nodes, dn)
	}
	refresh, _ := strconv.Atoi(r.URL.Query().Get("refresh"))
	data := map[string]any{
		"Cfg": s.cfg, "Addr": s.Addr(), "Uptime": time.Since(s.started).Round(time.Second),
		"Clients": s.Clients(), "Subs": s.Subscriptions(), "Unhandled": s.Unhandled.List(), "Traffic": s.Traffic(100),
		"Nodes": nodes, "Total": total, "Limit": limit, "Q": q, "Refresh": refresh, "Now": time.Now(),
		"NParams": len(s.store.Schema().Leaves()), "NNodes": len(s.store.Schema().Nodes),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := debugTmpl.Execute(w, data); err != nil {
		s.log.Warn("debug page", "err", err)
	}
}

var debugTmpl = template.Must(template.New("debug").Funcs(template.FuncMap{
	"ago": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return time.Since(t).Round(time.Second).String() + " ago"
	},
	"clock":  func(t time.Time) string { return t.Format("15:04:05.000") },
	"active": func(t time.Time) bool { return t.After(time.Now()) },
}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
{{if .Refresh}}<meta http-equiv="refresh" content="{{.Refresh}}">{{end}}
<title>tabula-emu</title>
<style>
:root { --bg:#fff; --fg:#1b1d21; --muted:#667085; --line:#e4e7ec; --accent:#2f5bd3; --warn:#b54708; --code:#f6f7f9; }
@media (prefers-color-scheme: dark) { :root { --bg:#14161a; --fg:#e6e8eb; --muted:#98a2b3; --line:#2b2f36; --accent:#7ea2ff; --warn:#f5a65b; --code:#1c1f24; } }
body { margin:0; padding:1rem; background:var(--bg); color:var(--fg); font:14px/1.45 system-ui, sans-serif; }
h1 { font-size:1.2rem; margin:0 0 .2rem } h2 { font-size:1rem; margin:1.6rem 0 .5rem }
.muted { color:var(--muted) } .warn { color:var(--warn) }
table { border-collapse:collapse; width:100%; } th, td { text-align:left; padding:.25rem .5rem; border-bottom:1px solid var(--line); vertical-align:top }
th { color:var(--muted); font-weight:600 } code, .mono { font-family:ui-monospace, monospace; font-size:12.5px }
.scroll { overflow-x:auto } pre { margin:0; white-space:pre; }
details summary { cursor:pointer; font-family:ui-monospace, monospace; font-size:12.5px; white-space:pre }
form { display:flex; gap:.5rem; flex-wrap:wrap } input[type=search] { flex:1; min-width:12rem; padding:.3rem .5rem }
a { color:var(--accent) } .pill { display:inline-block; padding:0 .4rem; border-radius:.6rem; background:var(--code) }
</style></head><body>
<h1>tabula-emu <span class="muted">— read-only debug view</span></h1>
<div class="muted">{{.Cfg.Model}} “{{.Cfg.Name}}” firmware {{.Cfg.Firmware}} · OSC on {{.Addr}} · up {{.Uptime}} ·
{{.NNodes}} nodes, {{.NParams}} parameters · <a href="/scene.scn">current state as .scn</a> ·
{{if .Refresh}}<a href="?q={{.Q}}">stop refresh</a>{{else}}<a href="?q={{.Q}}&refresh=2">auto-refresh</a>{{end}}</div>

<h2>Clients</h2>
<div class="scroll"><table><tr><th>Address</th><th>Requests</th><th>Last seen</th><th>/xremote</th></tr>
{{range .Clients}}<tr><td class="mono">{{.Addr}}</td><td>{{.Requests}}</td><td>{{ago .LastSeen}}</td>
<td>{{if active .XremoteUntil}}<span class="pill">subscribed</span>{{else}}<span class="muted">no</span>{{end}}</td></tr>
{{else}}<tr><td colspan="4" class="muted">No client yet.</td></tr>{{end}}</table></div>

<h2>Subscriptions <span class="muted">(meters, /subscribe… — not shown in the traffic log)</span></h2>
<div class="scroll"><table><tr><th>Client</th><th>Kind</th><th>Name</th><th>Every</th><th>Expires</th></tr>
{{range .Subs}}<tr><td class="mono">{{.Client}}</td><td>{{.Kind}}</td><td class="mono">{{.Name}}</td><td>{{.Interval}}</td><td>{{ago .Expires}}</td></tr>
{{else}}<tr><td colspan="5" class="muted">None.</td></tr>{{end}}</table></div>

<h2>Unhandled requests <span class="muted">(<a href="/api/unhandled">JSON</a>)</span></h2>
<div class="scroll"><table><tr><th>Count</th><th>Reason</th><th>Address</th><th>Tags</th><th>Last arguments</th><th>Detail</th><th>Last</th></tr>
{{range .Unhandled}}<tr><td>{{.Count}}</td><td class="warn">{{.Reason}}</td><td class="mono">{{.Address}}</td><td class="mono">{{.Tags}}</td>
<td class="mono">{{.LastArgs}}</td><td>{{.Detail}}</td><td>{{ago .Last}}</td></tr>
{{else}}<tr><td colspan="7" class="muted">Nothing so far.</td></tr>{{end}}</table></div>

<h2>State</h2>
<form method="get"><input type="search" name="q" value="{{.Q}}" placeholder="filter node lines, e.g. ch/01/ or IEM or -oo">
<button>Filter</button></form>
<p class="muted">{{.Total}} matching nodes{{if gt .Total .Limit}}, first {{.Limit}} shown{{end}}. Open a line to see its parameters.</p>
<div class="scroll">{{range .Nodes}}<details><summary>{{.Line}}</summary>
<table>{{range .Params}}<tr><td class="mono">{{.Name}}</td><td class="mono">{{.Value}}</td><td class="muted">{{.Type}}</td></tr>{{end}}</table>
</details>{{end}}</div>

<h2>Recent traffic <span class="muted">(<a href="/api/traffic">JSON</a>)</span></h2>
<div class="scroll"><table><tr><th>Time</th><th></th><th>Client</th><th>Message</th></tr>
{{range .Traffic}}<tr><td class="mono">{{clock .Time}}</td><td>{{if .In}}→{{else}}←{{end}}</td><td class="mono">{{.Client}}</td><td class="mono"><pre>{{.Text}}</pre></td></tr>
{{else}}<tr><td colspan="4" class="muted">No traffic yet.</td></tr>{{end}}</table></div>
</body></html>`))
