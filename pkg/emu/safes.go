package emu

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yoda-jm/tabula/pkg/schema"
)

// Scene safes and snippet filters: what a recall of the console's internal
// scenes and snippets leaves alone, or changes. A scene recall skips what
// the scene's safes (/-show/showfile/scene/NNN/safes), the parameter safes
// and channel safes of the show (/-show/showfile/show) and "safe main
// levels" (/-prefs/safe_masterlevels) protect. A snippet holds what its
// filters (/-show/showfile/snippet/NNN/eventtyp, channels, auxbuses,
// maingrps) select when it is saved, and a recall applies what they still
// select. The bits are those of Maillot's document; which nodes each group
// covers is read from its descriptions, partly guessed (docs/emulator.md).

// part is the kind of parameter a scene line, or one of its fields, sets.
type part int

const (
	partOther    part = iota
	partPreamp        // trim, polarity, low cut, delay; headamp gain and 48 V
	partConfig        // name, icon, colour, source
	partEQ            // equalizer
	partDyn           // gate and compressor
	partInsert        // insert
	partGroups        // DCA and mute group assigns, automix group
	partFader         // fader and pan
	partMute          // the mix's on switch
	partLRMC          // L/R and M/C assigns, M/C level
	partSend          // a send to a mix bus or a matrix
	partFX            // an effect slot: type, source, parameters
	partTalk          // talkback
	partConsole       // links, mute groups, oscillator, user controls, USB recorder, automix enable
	partSolo          // monitor and solo settings, DP48
	partRouting       // input routing blocks, Rec/Play
	partUserIn        // user input slots
	partUserOut       // user output slots
	partOutPatch      // outputs, P16, AES, USB recorder sources
)

// leafClass is what a scene parameter belongs to.
type leafClass struct {
	strip string // ch, auxin, fxrtn, bus, mtx, main, dca, fx, headamp; "" for the console's settings
	num   int    // from 1 (main: 1 for L/R, 2 for M/C); a headamp's index from 0
	part  part
	send  int    // partSend: the mix bus or matrix sent to, from 1
	field string // the field, "" for the whole node
}

func (c leafClass) input() bool { return c.strip == "ch" || c.strip == "auxin" || c.strip == "fxrtn" }

// classify says what the field of a scene node is ("" for the whole node).
func classify(path, field string) leafClass {
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	c := leafClass{field: field}
	if len(segs) < 2 {
		return c
	}
	switch segs[0] {
	case "config":
		switch {
		case segs[1] == "talk":
			c.part = partTalk
		case segs[1] == "solo" || strings.HasPrefix(segs[1], "dp48"):
			c.part = partSolo
		case segs[1] == "routing":
			c.part = partRouting
		case segs[1] == "userrout" && len(segs) > 2 && segs[2] == "in":
			c.part = partUserIn
		case segs[1] == "userrout":
			c.part = partUserOut
		default:
			c.part = partConsole
		}
		return c
	case "outputs":
		c.part = partOutPatch
		return c
	case "headamp":
		c.strip, c.num, c.part = "headamp", atoi(segs[1]), partPreamp
		return c
	case "fx":
		c.strip, c.num, c.part = "fx", atoi(segs[1]), partFX
		return c
	case "main":
		c.strip, c.num = "main", map[string]int{"st": 1, "m": 2}[segs[1]]
	case "ch", "auxin", "fxrtn", "bus", "mtx", "dca":
		c.strip, c.num = segs[0], atoi(segs[1])
	default:
		return c
	}
	sub := segs[2:]
	if len(sub) == 0 { // /dca/N: on, fader
		c.part = mixPart(field)
		return c
	}
	switch sub[0] {
	case "preamp", "delay":
		c.part = partPreamp
	case "config":
		c.part = partConfig
	case "eq":
		c.part = partEQ
	case "gate", "dyn":
		c.part = partDyn
	case "insert":
		c.part = partInsert
	case "grp", "automix":
		c.part = partGroups
	case "mix":
		if len(sub) > 1 {
			c.part, c.send = partSend, atoi(sub[1])
		} else {
			c.part = mixPart(field)
		}
	}
	return c
}

func mixPart(field string) part {
	switch field {
	case "on":
		return partMute
	case "fader", "pan":
		return partFader
	case "st", "mono", "mlevel":
		return partLRMC
	}
	return partOther
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// has tells whether a bit of a 32-bit map is set.
func has(v int32, bit int) bool { return bit >= 0 && bit < 32 && uint32(v)&(1<<bit) != 0 }

// paramBit is the bit of a strip's part in the parameter safes and the
// snippet filters (preamp 0 … mute 7), -1 for none.
func paramBit(p part) int {
	switch p {
	case partPreamp:
		return 0
	case partConfig:
		return 1
	case partEQ:
		return 2
	case partDyn:
		return 3
	case partInsert:
		return 4
	case partGroups:
		return 5
	case partFader, partLRMC:
		return 6
	case partMute:
		return 7
	}
	return -1
}

// inputStrip names input channel 1-40 (33-40 the aux inputs).
func inputStrip(ch int) (string, int) {
	if ch > 32 {
		return "auxin", ch - 32
	}
	return "ch", ch
}

// headampUsers maps each headamp to the input channels it feeds (1-40), as
// the console reports them in /-ha/NN/index.
func (s *Server) headampUsers() map[int][]int {
	users := map[int][]int{}
	for ch := 0; ch < 40; ch++ {
		if ha := s.intAt(fmt.Sprintf("/-ha/%02d/index", ch)); ha >= 0 {
			users[int(ha)] = append(users[int(ha)], ch+1)
		}
	}
	return users
}

// sceneSafes is what a scene recall leaves alone.
type sceneSafes struct {
	scene                                         int32 // the scene's safes, bits 1-8
	inputs, mxsends, mxbuses, console             int32 // parameter safes
	chan16, chan32, ret, buses, lrmtxdca, effects int32 // channel safes
	mainLevels                                    bool
	users                                         map[int][]int // headamp → input channels
}

func (s *Server) sceneSafes(idx int) sceneSafes {
	show := func(f string) int32 { return s.intAt("/-show/showfile/show/" + f) }
	k := sceneSafes{
		inputs: show("inputs"), mxsends: show("mxsends"), mxbuses: show("mxbuses"), console: show("console"),
		chan16: show("chan16"), chan32: show("chan32"), ret: show("return"), buses: show("buses"),
		lrmtxdca: show("lrmtxdca"), effects: show("effects"),
		mainLevels: s.intAt("/-prefs/safe_masterlevels") == 1, users: s.headampUsers(),
	}
	if idx > 0 { // scene 0 has no safes
		k.scene = s.intAt(fmt.Sprintf("/-show/showfile/scene/%03d/safes", idx))
	}
	return k
}

// keeps tells whether a recall leaves a parameter alone.
func (k sceneSafes) keeps(c leafClass) bool {
	sc := func(bit int) bool { return has(k.scene, bit) }
	switch {
	case sc(1) && c.part == partTalk,
		sc(2) && c.strip == "fx",
		sc(3) && c.strip == "bus",
		sc(4) && c.input() && c.part != partPreamp,
		sc(5) && (c.part == partConsole || c.part == partSolo),
		sc(6) && (c.input() && c.part == partPreamp || c.strip == "headamp"),
		sc(7) && (c.part == partOutPatch || c.part == partUserIn || c.part == partUserOut),
		sc(8) && c.part == partRouting:
		return true
	}
	switch {
	case c.input() || c.strip == "headamp":
		if has(k.inputs, paramBit(c.part)) || c.part == partSend && has(k.mxsends, c.send-1) {
			return true
		}
	case c.strip == "bus":
		if c.part == partSend && has(k.mxbuses, 0) || c.part != partPreamp && has(k.mxbuses, paramBit(c.part)) {
			return true
		}
	case c.strip == "":
		bits := map[part][]int{partConsole: {0}, partTalk: {0}, partSolo: {0, 1}, partRouting: {2}, partOutPatch: {3},
			partUserIn: {4}, partUserOut: {5}}[c.part]
		for _, b := range bits {
			if has(k.console, b) {
				return true
			}
		}
	}
	if k.stripSafe(c.strip, c.num) {
		return true
	}
	if c.strip == "headamp" {
		for _, ch := range k.users[c.num] {
			if k.stripSafe(inputStrip(ch)) {
				return true
			}
		}
	}
	return k.mainLevels && c.strip == "main" && c.field == "fader"
}

// stripSafe tells the strips the channel safes keep whole.
func (k sceneSafes) stripSafe(strip string, n int) bool {
	switch strip {
	case "ch":
		if n <= 16 {
			return has(k.chan16, n-1)
		}
		return has(k.chan32, n-17)
	case "auxin":
		return has(k.ret, n-1)
	case "fxrtn":
		return has(k.ret, n+7)
	case "bus":
		return has(k.buses, n-1)
	case "mtx":
		return has(k.lrmtxdca, n-1)
	case "main":
		return has(k.lrmtxdca, n+5)
	case "dca":
		return has(k.lrmtxdca, n+7)
	case "fx":
		return has(k.effects, n-1)
	}
	return false
}

// snippetFilters is what a snippet holds and changes.
type snippetFilters struct {
	events, channels, auxbuses, maingrps int32
	users                                map[int][]int // headamp → input channels
}

func (s *Server) snippetFilters(idx int) snippetFilters {
	p := fmt.Sprintf("/-show/showfile/snippet/%03d/", idx)
	return snippetFilters{events: s.intAt(p + "eventtyp"), channels: s.intAt(p + "channels"),
		auxbuses: s.intAt(p + "auxbuses"), maingrps: s.intAt(p + "maingrps"), users: s.headampUsers()}
}

// applies tells whether a snippet holds and changes a parameter.
func (f snippetFilters) applies(c leafClass) bool {
	switch c.strip {
	case "":
		bit, ok := map[part]int{partConsole: 21, partTalk: 21, partSolo: 22, partRouting: 23, partUserIn: 23, partUserOut: 23,
			partOutPatch: 24}[c.part]
		return ok && has(f.events, bit)
	case "fx":
		return has(f.events, 12+c.num)
	case "headamp":
		for _, ch := range f.users[c.num] {
			if f.stripOn(inputStrip(ch)) {
				return has(f.events, 0)
			}
		}
		return false
	}
	if !f.stripOn(c.strip, c.num) {
		return false
	}
	bit := paramBit(c.part)
	switch {
	case c.part == partLRMC:
		bit = 11
	case c.part == partSend && !c.input():
		bit = 12 // bus and main sends go to the matrices
	case c.part == partSend && c.send <= 8:
		bit = 8
	case c.part == partSend && c.send <= 12:
		bit = 9
	case c.part == partSend:
		bit = 10
	}
	return has(f.events, bit)
}

// stripOn tells the strips a snippet's filters select.
func (f snippetFilters) stripOn(strip string, n int) bool {
	switch strip {
	case "ch":
		return has(f.channels, n-1)
	case "auxin":
		return has(f.auxbuses, n-1)
	case "fxrtn":
		return has(f.auxbuses, n+7)
	case "bus":
		return has(f.auxbuses, n+15)
	case "mtx":
		return has(f.maingrps, n-1)
	case "main":
		return has(f.maingrps, n+5)
	case "dca":
		return has(f.maingrps, n+7)
	}
	return false
}

// filterLines keeps of saved lines what keep accepts: a line whole, cut
// down to the fields kept (one parameter per line, as snippet files hold
// them), or not at all. Variant nodes (effect parameters) go whole.
func (s *Server) filterLines(lines []relLine, keep func(leafClass) bool) []relLine {
	sch := s.store.Schema()
	var out []relLine
	for _, l := range lines {
		n := sch.Node(l.Suffix)
		if n == nil {
			if leaf := sch.Leaf(l.Suffix); leaf != nil && keep(classify(leaf.Node.Path, leaf.Node.Fields[leaf.Index].Name)) {
				out = append(out, l)
			}
			continue
		}
		if n.Variant != nil || len(n.Fields) == 0 {
			if keep(classify(n.Path, "")) {
				out = append(out, l)
			}
			continue
		}
		kept := make([]bool, len(n.Fields))
		all, none := true, true
		for i, f := range n.Fields {
			kept[i] = keep(classify(n.Path, f.Name))
			all, none = all && kept[i], none && !kept[i]
		}
		switch {
		case all:
			out = append(out, l)
		case !none:
			for i, tok := range schema.Tokenize(l.Rest) {
				if i < len(kept) && kept[i] {
					out = append(out, relLine{n.Path + "/" + n.Fields[i].Name, tok})
				}
			}
		}
	}
	return out
}
