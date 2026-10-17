package emu

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/yoda-jm/tabula/pkg/state"
)

// Linked strips: /config/chlink, auxlink, fxlink, buslink and mtxlink pair an
// odd strip with the next one. The console keeps the pair in step: a change a
// client makes on one strip is applied to the other, and linking a pair copies
// the odd strip to the even one, as does turning a link option on for what it
// covers. M32-Edit only does this itself when offline.
//
// What follows the partner, from M32-Edit's offline logic (and matching the
// linked pairs of real M32C scenes):
//   - never: name, icon, input source, pan, send pans, gate and compressor key
//     sources, insert selection;
//   - with /config/linkcfg/hadly: delay and preamp (trim, polarity, low cut),
//     and for channels the headamp (gain, phantom) of their inputs;
//   - with linkcfg/eq: the EQ;
//   - with linkcfg/dyn: gate, compressor and insert on/position;
//   - with linkcfg/fdrmute: fader, mute, L/R and mono assignments, mono level;
//   - always: everything else (color, sends, DCA and mute groups, automix).

var linkGroups = map[string]struct {
	flag  string // link switches, as /config/chlink/1-2
	pairs int
}{
	"ch": {"/config/chlink", 16}, "auxin": {"/config/auxlink", 4}, "fxrtn": {"/config/fxlink", 4},
	"bus": {"/config/buslink", 8}, "mtx": {"/config/mtxlink", 3},
}

var stripAddr = regexp.MustCompile(`^/(ch|auxin|fxrtn|bus|mtx)/(\d\d)/(.+)$`)

// linkOption returns the /config/linkcfg switch a strip parameter (path
// relative to the strip) follows its partner under: "" for always, "-" for
// never.
func linkOption(rel string) string {
	switch rel {
	case "config/name", "config/icon", "config/source", "mix/pan",
		"gate/keysrc", "dyn/keysrc", "insert/sel":
		return "-"
	case "mix/on", "mix/fader", "mix/st", "mix/mono", "mix/mlevel":
		return "fdrmute"
	}
	head, _, _ := strings.Cut(rel, "/")
	switch head {
	case "delay", "preamp":
		return "hadly"
	case "eq":
		return "eq"
	case "gate", "dyn", "insert":
		return "dyn"
	case "mix":
		if strings.HasSuffix(rel, "/pan") {
			return "-"
		}
	}
	return ""
}

// partner returns the address of the same parameter on the linked strip, if
// the pair is linked and the parameter follows.
func (s *Server) partner(addr string) (string, bool) {
	m := stripAddr.FindStringSubmatch(addr)
	if m == nil {
		return "", false
	}
	n, _ := strconv.Atoi(m[2])
	g := linkGroups[m[1]]
	if n < 1 || n > 2*g.pairs || !s.linked(g.flag, n) {
		return "", false
	}
	switch opt := linkOption(m[3]); opt {
	case "-":
		return "", false
	case "":
	default:
		if s.intAt("/config/linkcfg/"+opt) != 1 {
			return "", false
		}
	}
	other := n + 1
	if n%2 == 0 {
		other = n - 1
	}
	return fmt.Sprintf("/%s/%02d/%s", m[1], other, m[3]), true
}

func (s *Server) linked(flag string, n int) bool {
	odd := n - (n+1)%2
	return s.intAt(fmt.Sprintf("%s/%d-%d", flag, odd, odd+1)) == 1
}

// link applies to linked partners the changes a client made.
func (s *Server) link(changes []state.Change) {
	for _, c := range changes {
		a := c.Leaf.Addr
		switch {
		case strings.HasPrefix(a, "/config/linkcfg/"):
			// Turning an option on brings every linked pair in step for it.
			if c.Value.Int == 1 {
				s.syncPairs(strings.TrimPrefix(a, "/config/linkcfg/"))
			}
		case strings.HasPrefix(a, "/config/") && strings.Contains(a, "link/"):
			if c.Value.Int == 1 {
				s.copyPair(a, "")
			}
		case strings.HasPrefix(a, "/headamp/"):
			s.linkHeadamp(a, c)
		default:
			if p, ok := s.partner(a); ok {
				s.store.Set(p, c.Value, originConsole)
			}
		}
	}
}

// syncPairs copies, in every linked pair, what follows under one link option
// from the odd strip to the even one.
func (s *Server) syncPairs(opt string) {
	for _, g := range linkGroups {
		for odd := 1; odd < 2*g.pairs; odd += 2 {
			if s.linked(g.flag, odd) {
				s.copyPair(fmt.Sprintf("%s/%d-%d", g.flag, odd, odd+1), opt)
			}
		}
	}
}

// copyPair copies the odd strip of a linked pair (/config/chlink/1-2…) to
// the even one: what follows under link option only, or all that follows
// when only is "".
func (s *Server) copyPair(flagAddr, only string) {
	slash := strings.LastIndexByte(flagAddr, '/')
	var kind string
	for k, g := range linkGroups {
		if g.flag == flagAddr[:slash] {
			kind = k
		}
	}
	var odd int
	if _, err := fmt.Sscanf(flagAddr[slash+1:], "%d-", &odd); kind == "" || err != nil {
		return
	}
	prefix := fmt.Sprintf("/%s/%02d/", kind, odd)
	for _, n := range s.store.Schema().Nodes {
		if !strings.HasPrefix(n.Path+"/", prefix) {
			continue
		}
		vals := s.store.Values(n)
		for i, f := range n.Fields {
			a := n.Path + "/" + f.Name
			if only != "" && linkOption(strings.TrimPrefix(a, prefix)) != only {
				continue
			}
			if p, ok := s.partner(a); ok {
				s.store.Set(p, vals[i], originConsole)
			}
		}
	}
	if kind == "ch" && (only == "" || only == "hadly") && s.intAt("/config/linkcfg/hadly") == 1 {
		from, to := s.intAt(fmt.Sprintf("/-ha/%02d/index", odd-1)), s.intAt(fmt.Sprintf("/-ha/%02d/index", odd))
		if from >= 0 && to >= 0 && from != to {
			for _, f := range []string{"gain", "phantom"} {
				v, _, _ := s.store.Get(fmt.Sprintf("/headamp/%03d/%s", from, f))
				s.store.Set(fmt.Sprintf("/headamp/%03d/%s", to, f), v, originConsole)
			}
		}
	}
}

// linkHeadamp applies a headamp change to the input of the linked channel.
func (s *Server) linkHeadamp(addr string, c state.Change) {
	var ha int32
	var field string
	if _, err := fmt.Sscanf(addr, "/headamp/%03d/%s", &ha, &field); err != nil || s.intAt("/config/linkcfg/hadly") != 1 {
		return
	}
	for ch := 1; ch <= 32; ch++ {
		if s.intAt(fmt.Sprintf("/-ha/%02d/index", ch-1)) != ha || !s.linked("/config/chlink", ch) {
			continue
		}
		other := ch + 1
		if ch%2 == 0 {
			other = ch - 1
		}
		if to := s.intAt(fmt.Sprintf("/-ha/%02d/index", other-1)); to >= 0 && to != ha {
			s.store.Set(fmt.Sprintf("/headamp/%03d/%s", to, field), c.Value, originConsole)
		}
		return
	}
}
