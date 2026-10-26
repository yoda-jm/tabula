package emu

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
)

// The console's internal library: 100 scenes, 100 snippets and 100 presets
// of each kind (channel, effect, routing, AES50/DP48), managed with /save,
// /load, /copy, /delete, /rename and listed by /-show/showfile/… and
// /-libs/… nodes. Snippet parameter filters and scene safes are not applied
// yet: snippets hold and recall a whole scene.

const libSlots = 100

// libKinds maps the command names to the /-libs listing they update and the
// file extension used in a library directory.
var libKinds = map[string]struct{ listing, ext string }{
	"scene":   {"/-show/showfile/scene", "scn"},
	"snippet": {"/-show/showfile/snippet", "snp"},
	"libchan": {"/-libs/ch", "chn"},
	"libfx":   {"/-libs/fx", "efx"},
	"librout": {"/-libs/r", "rou"},
	"libmon":  {"/-libs/mon", "mon"},
}

// entry is a saved item: node lines relative to the saved base path (the
// suffix after it, "" for the base node itself) and their text.
type entry struct {
	Name, Note string
	Lines      []relLine
}

type relLine struct{ Suffix, Rest string }

type library struct {
	mu    sync.Mutex
	items map[string]*[libSlots]*entry
	dir   string // persistence directory, "" for none
}

func newLibrary() *library {
	l := &library{items: map[string]*[libSlots]*entry{}}
	for k := range libKinds {
		l.items[k] = &[libSlots]*entry{}
	}
	return l
}

// stripPath maps a strip index [0, 71] to its path.
func stripPath(i int) (string, bool) {
	switch {
	case i < 0:
	case i < 32:
		return fmt.Sprintf("/ch/%02d", i+1), true
	case i < 40:
		return fmt.Sprintf("/auxin/%02d", i-31), true
	case i < 48:
		return fmt.Sprintf("/fxrtn/%02d", i-39), true
	case i < 64:
		return fmt.Sprintf("/bus/%02d", i-47), true
	case i < 70:
		return fmt.Sprintf("/mtx/%02d", i-63), true
	case i == 70:
		return "/main/st", true
	case i == 71:
		return "/main/m", true
	}
	return "", false
}

// capture returns the scene lines of the nodes under the given prefixes
// (base itself if none), with paths relative to base.
func (s *Server) capture(base string, prefixes ...string) []relLine {
	if len(prefixes) == 0 {
		prefixes = []string{base}
	}
	var out []relLine
	for _, n := range s.store.Schema().Nodes {
		if !n.InScene {
			continue
		}
		for _, p := range prefixes {
			if p != "" && n.Path != p && !strings.HasPrefix(n.Path, p+"/") {
				continue
			}
			_, rest := schema.SplitLine(s.store.NodeLine(n))
			out = append(out, relLine{strings.TrimPrefix(n.Path, base), rest})
			break
		}
	}
	return out
}

// channelScope says which preset lines a /load libchan scope keeps: bit 0
// head amp, 1 configuration, 2 gate, 3 compressor, 4 equalizer, 5 sends.
func channelScope(suffix string, scope int) bool {
	part := strings.SplitN(strings.TrimPrefix(suffix, "/"), "/", 2)[0]
	bit := map[string]int{"preamp": 0, "delay": 0, "config": 1, "insert": 1, "grp": 1, "automix": 1,
		"gate": 2, "dyn": 3, "eq": 4, "mix": 5}[part]
	return scope&(1<<bit) != 0
}

// library dispatches /save, /load, /copy, /delete, /rename and /add, and
// replies with the command name, the element type and a status (1 = done).
func (s *Server) library(m osc.Message, c *Client) {
	kind, _ := strArgN(m, 0)
	ok, detail := s.libraryOp(m, kind, "udp "+c.Addr)
	if !ok && detail != "" {
		s.Unhandled.Record(m, c.Addr, ReasonRejected, detail)
	}
	status := int32(0)
	if ok {
		status = 1
	}
	s.send(c, osc.NewMessage(m.Address, osc.String(kind), osc.Int(status)))
}

func strArgN(m osc.Message, i int) (string, bool) {
	if i < len(m.Args) && m.Args[i].Tag == 's' {
		return m.Args[i].Str, true
	}
	return "", false
}

func intArgN(m osc.Message, i int) (int, bool) {
	if i < len(m.Args) && m.Args[i].Tag == 'i' {
		return int(m.Args[i].Int), true
	}
	return 0, false
}

func (s *Server) libraryOp(m osc.Message, kind, origin string) (bool, string) {
	if m.Address == "/add" {
		return s.addCue(m, origin)
	}
	if _, known := libKinds[kind]; !known {
		return false, fmt.Sprintf("unknown element type %q", kind)
	}
	idx, ok := intArgN(m, 1)
	if !ok || idx < 0 || idx >= libSlots {
		return false, "expects an index in [0, 99]"
	}
	lib := s.lib
	lib.mu.Lock()
	defer lib.mu.Unlock()
	slots := lib.items[kind]

	switch m.Address {
	case "/save":
		name, _ := strArgN(m, 2)
		e := &entry{Name: name}
		switch kind {
		case "scene":
			e.Note, _ = strArgN(m, 3)
			e.Lines = s.capture("")
		case "snippet":
			e.Lines = s.capture("")
		case "libchan":
			ch, _ := intArgN(m, 3)
			base, ok := stripPath(ch)
			if !ok {
				return false, "channel index out of [0, 71]"
			}
			e.Lines = s.capture(base)
		case "libfx":
			slot, _ := intArgN(m, 3)
			if slot < 0 || slot > 7 {
				return false, "effect slot out of [0, 7]"
			}
			e.Lines = s.capture(fmt.Sprintf("/fx/%d", slot+1))
		case "librout":
			e.Lines = s.capture("", "/config/routing", "/config/userrout")
		case "libmon":
			e.Lines = s.capture("", "/config/dp48")
		}
		slots[idx] = e
		s.listEntry(kind, idx, e, origin)
		lib.persist(kind, idx, e)
		return true, ""

	case "/load":
		e := slots[idx]
		if e == nil {
			return false, fmt.Sprintf("%s %d is empty", kind, idx)
		}
		base := ""
		keep := func(relLine) bool { return true }
		switch kind {
		case "libchan":
			ch, _ := intArgN(m, 2)
			var ok bool
			if base, ok = stripPath(ch); !ok {
				return false, "channel index out of [0, 71]"
			}
			scope := 63
			if v, given := intArgN(m, 3); given {
				scope = v
			}
			keep = func(l relLine) bool { return channelScope(l.Suffix, scope) }
		case "libfx":
			slot, _ := intArgN(m, 2)
			if slot < 0 || slot > 7 {
				return false, "effect slot out of [0, 7]"
			}
			base = fmt.Sprintf("/fx/%d", slot+1)
		}
		var lines []string
		for _, l := range e.Lines {
			if keep(l) && s.store.Schema().Node(base+l.Suffix) != nil {
				lines = append(lines, base+l.Suffix+" "+l.Rest)
			}
		}
		lib.mu.Unlock()
		errs := s.store.ApplyLines(lines, origin)
		lib.mu.Lock()
		if kind == "scene" || kind == "snippet" {
			s.store.Set("/-show/prepos/current", osc.Int(int32(idx)), origin)
		}
		if len(errs) > 0 {
			return false, errs[0].Error()
		}
		return true, ""

	case "/copy":
		if kind == "snippet" {
			return false, "snippets cannot be copied (use /load and /save)"
		}
		dst, ok := intArgN(m, 2)
		if !ok || dst < 0 || dst >= libSlots || slots[idx] == nil {
			return false, "expects a saved source and a destination in [0, 99]"
		}
		cp := *slots[idx]
		slots[dst] = &cp
		s.listEntry(kind, dst, &cp, origin)
		lib.persist(kind, dst, &cp)
		return true, ""

	case "/delete":
		slots[idx] = nil
		s.listEntry(kind, idx, nil, origin)
		lib.persist(kind, idx, nil)
		return true, ""

	case "/rename":
		name, ok := strArgN(m, 2)
		if !ok || slots[idx] == nil {
			return false, "expects a saved element and a name"
		}
		slots[idx].Name = name
		s.listEntry(kind, idx, slots[idx], origin)
		lib.persist(kind, idx, slots[idx])
		return true, ""
	}
	return false, "unknown operation"
}

// listEntry updates the /-show or /-libs listing of a slot.
func (s *Server) listEntry(kind string, idx int, e *entry, origin string) {
	name, note, has := "", "", 0
	if e != nil {
		name, note, has = e.Name, e.Note, 1
	}
	switch kind {
	case "scene":
		s.store.ApplyText(fmt.Sprintf("/-show/showfile/scene/%03d %s %s %%000000000 %d", idx, schema.Quote(name), schema.Quote(note), has), origin)
	case "snippet":
		p := fmt.Sprintf("/-show/showfile/snippet/%03d", idx)
		s.store.Set(p+"/name", osc.String(name), origin)
		s.store.Set(p+"/hasdata", osc.Int(int32(has)), origin)
	default:
		p := fmt.Sprintf("%s/%03d", libKinds[kind].listing, idx+1)
		s.store.Set(p+"/name", osc.String(name), origin)
		s.store.Set(p+"/hasdata", osc.Int(int32(has)), origin)
	}
}

// addCue handles "/add ,sis cue <number> <name>": the cue goes to the first
// free cue slot, with number 1252 meaning cue 12.5.2.
func (s *Server) addCue(m osc.Message, origin string) (bool, string) {
	if kind, _ := strArgN(m, 0); kind != "cue" {
		return false, "only cues can be added"
	}
	numb, ok := intArgN(m, 1)
	name, _ := strArgN(m, 2)
	if !ok {
		return false, "expects a cue number"
	}
	for i := 0; i < libSlots; i++ {
		p := fmt.Sprintf("/-show/showfile/cue/%03d", i)
		v, _, err := s.store.Get(p + "/name")
		n, _, _ := s.store.Get(p + "/numb")
		if err == nil && v.Str == "" && n.Int == 0 {
			s.store.Set(p+"/numb", osc.Int(int32(numb)), origin)
			s.store.Set(p+"/name", osc.String(name), origin)
			return true, ""
		}
	}
	return false, "no free cue slot"
}

// showdump sends the show line, then every cue, scene and snippet in use.
func (s *Server) showdump(c *Client) {
	sch := s.store.Schema()
	send := func(path string) {
		if n := sch.Node(path); n != nil {
			s.send(c, osc.NewMessage("node", osc.String(s.store.NodeLine(n)+"\n")))
		}
	}
	send("/-show/showfile/show")
	for i := 0; i < libSlots; i++ {
		if v, _, err := s.store.Get(fmt.Sprintf("/-show/showfile/cue/%03d/name", i)); err == nil && v.Str != "" {
			send(fmt.Sprintf("/-show/showfile/cue/%03d", i))
		}
	}
	for _, kind := range []string{"scene", "snippet"} {
		for i := 0; i < libSlots; i++ {
			if v, _, err := s.store.Get(fmt.Sprintf("%s/%03d/hasdata", libKinds[kind].listing, i)); err == nil && v.Int == 1 {
				send(fmt.Sprintf("%s/%03d", libKinds[kind].listing, i))
			}
		}
	}
}

// Library directories hold one file per saved item, e.g. scene-07.scn: a
// header line with the name and note, then the node lines.

func (l *library) persist(kind string, idx int, e *entry) {
	if l.dir == "" {
		return
	}
	file := filepath.Join(l.dir, fmt.Sprintf("%s-%02d.%s", kind, idx, libKinds[kind].ext))
	if e == nil {
		os.Remove(file)
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "#4.0# %s %s\n", schema.Quote(e.Name), schema.Quote(e.Note))
	for _, line := range e.Lines {
		fmt.Fprintf(&b, "%s %s\n", line.Suffix, line.Rest)
	}
	os.WriteFile(file, []byte(b.String()), 0o644)
}

// LoadLibrary reads a library directory and keeps it updated from now on.
func (s *Server) LoadLibrary(dir string) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	s.lib.mu.Lock()
	s.lib.dir = dir
	s.lib.mu.Unlock()
	files, _ := filepath.Glob(filepath.Join(dir, "*-[0-9][0-9].*"))
	n := 0
	for _, file := range files {
		base := filepath.Base(file)
		kind, rest, _ := strings.Cut(base, "-")
		if _, ok := libKinds[kind]; !ok || len(rest) < 2 {
			continue
		}
		idx, err := strconv.Atoi(rest[:2])
		if err != nil {
			continue
		}
		e, err := readEntry(file)
		if err != nil {
			s.log.Warn("library file", "file", file, "err", err)
			continue
		}
		s.lib.mu.Lock()
		s.lib.items[kind][idx] = e
		s.lib.mu.Unlock()
		s.listEntry(kind, idx, e, "library")
		n++
	}
	return n, nil
}

func readEntry(file string) (*entry, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	e := &entry{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if strings.HasPrefix(line, "#") {
				toks := schema.Tokenize(line)
				switch {
				case len(toks) >= 3 && strings.HasPrefix(toks[1], `"`): // ours: #4.0# "name" "note"
					e.Name, e.Note = schema.Unquote(toks[1]), schema.Unquote(toks[2])
				case len(toks) >= 3: // M32-Edit: #2.0# <pos> "name" <type> %flags <hasdata>
					e.Name = schema.Unquote(toks[2])
				}
				continue
			}
		}
		suffix, rest := schema.SplitLine(line)
		e.Lines = append(e.Lines, relLine{suffix, rest})
	}
	return e, sc.Err()
}

// ImportM32EditPresets loads the preset files of M32-Edit's library
// (~/.config/.M32-Edit/Xlib: libchan.001, libfx.012, librout.003…) into the
// empty preset slots, as a console ships with factory presets.
func (s *Server) ImportM32EditPresets(dir string) (int, error) {
	files, err := filepath.Glob(filepath.Join(dir, "lib*.[0-9][0-9][0-9]"))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, file := range files {
		kind, num, _ := strings.Cut(filepath.Base(file), ".")
		idx, err := strconv.Atoi(num)
		if _, ok := libKinds[kind]; !ok || err != nil || idx < 1 || idx > libSlots {
			continue
		}
		e, err := readEntry(file)
		if err != nil {
			s.log.Warn("preset file", "file", file, "err", err)
			continue
		}
		if kind == "libfx" { // "type AMBI", "source …", "par …" are relative to /fx/N
			for i, l := range e.Lines {
				switch l.Suffix {
				case "type":
					e.Lines[i].Suffix = ""
				case "source", "par":
					e.Lines[i].Suffix = "/" + l.Suffix
				}
			}
		}
		s.lib.mu.Lock()
		free := s.lib.items[kind][idx-1] == nil
		if free {
			s.lib.items[kind][idx-1] = e
		}
		s.lib.mu.Unlock()
		if free {
			s.listEntry(kind, idx-1, e, "library")
			n++
		}
	}
	return n, nil
}
