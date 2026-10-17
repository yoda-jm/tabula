package audio

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

// TrackNames names the 32 tracks of the console's card (index 0: track 1)
// from the mirrored console state, "" when nothing names a track.
//
// The card's outputs are patched by blocks of 8 (/config/routing/CARD):
// from inputs (local, AES50 A/B, card, user inputs), from outputs (main,
// P16, aux) or user outputs. An input is named after the first channel (or
// aux input) whose source takes it, through the input patch
// (/config/routing/IN, /config/routing/IN/AUX); an output after what feeds
// it (a bus, a matrix, the main, a direct out).
func TrackNames(st *state.Store) []string {
	r := router{st}
	out := make([]string, 32)
	for t := range out {
		out[t] = r.cardTrack(t + 1)
	}
	return out
}

type router struct{ st *state.Store }

// text is the node text of a parameter ("A1-8", `"Hang"`), "" if unknown.
func (r router) text(addr string) string {
	v, l, err := r.st.Get(addr)
	if err != nil {
		return ""
	}
	t := r.st.TypeOf(l)
	if _, isStr := t.(schema.Str); isStr {
		return v.Str
	}
	return strings.TrimSpace(t.Format(v))
}

func (r router) int(addr string) int {
	v, _, err := r.st.Get(addr)
	if err != nil {
		return -1
	}
	return int(v.Int)
}

// A patch block is written as a port and a range: "A9-16", "CARD1-8",
// "P169-16", "OUT1-8", "UIN25-32", "AUX1-6/Mon", "AuxIN1-6/TB".
var blockRe = regexp.MustCompile(`^(AN|A|B|CARD|UIN|UOUT|OUT|P16|AUX|AuxIN)(\d+)-\d+`)

// The blocks of the input and card patches.
var blocks8 = []string{"1-8", "9-16", "17-24", "25-32"}

// "A1-4": aux inputs 1-4 from AES50-A 1-4.
var auxRe = regexp.MustCompile(`^(AN|A|B|CARD|UIN)1-(\d)$`)

// physical input numbering, as in the user patch (/config/userrout):
// local 1-32, AES50-A 33-80, AES50-B 81-128, card 129-160, aux in 161-166.
func inputID(port string, n int) int {
	switch port {
	case "AN":
		return n
	case "A":
		return 32 + n
	case "B":
		return 80 + n
	case "CARD":
		return 128 + n
	case "AuxIN":
		return 160 + n
	}
	return 0
}

// source is where a signal comes from: a physical input, or a console
// output (main 1-16, p16 1-16, aux 1-6).
type source struct {
	input  int    // physical input id, 0 if an output
	output string // "main", "p16", "aux"
	n      int
}

// block resolves position pos (0-7) of a patch block.
func (r router) block(name string, pos int) source {
	m := blockRe.FindStringSubmatch(name)
	if m == nil {
		return source{}
	}
	first, _ := strconv.Atoi(m[2])
	n := first + pos
	switch port := m[1]; port {
	case "AN", "A", "B", "CARD":
		return source{input: inputID(port, n)}
	case "AuxIN":
		if pos < 6 {
			return source{input: inputID(port, n)}
		}
	case "UIN":
		return source{input: r.userIn(n)}
	case "UOUT":
		return r.userOut(n)
	case "OUT":
		return source{output: "main", n: n}
	case "P16":
		return source{output: "p16", n: n}
	case "AUX":
		if pos < 6 {
			return source{output: "aux", n: n}
		}
	}
	return source{}
}

// userIn is the physical input of user input n (0 if off or a talkback).
func (r router) userIn(n int) int {
	v := r.int(fmt.Sprintf("/config/userrout/in/%02d", n))
	if v < 1 || v > 166 {
		return 0
	}
	return v
}

// userOut is the source of user output n.
func (r router) userOut(n int) source {
	v := r.int(fmt.Sprintf("/config/userrout/out/%02d", n))
	switch {
	case v >= 1 && v <= 166:
		return source{input: v}
	case v >= 169 && v <= 184:
		return source{output: "main", n: v - 168}
	case v >= 185 && v <= 200:
		return source{output: "p16", n: v - 184}
	case v >= 201 && v <= 206:
		return source{output: "aux", n: v - 200}
	}
	return source{}
}

func (r router) cardTrack(t int) string {
	src := r.block(r.text(fmt.Sprintf("/config/routing/CARD/%s", blocks8[(t-1)/8])), (t-1)%8)
	switch {
	case src.input > 0:
		return r.inputName(src.input)
	case src.output != "":
		return r.outputName(src.output, src.n)
	}
	return ""
}

// channelInput is the physical input feeding a channel source (1-32: input
// 1-32, 33-38: aux 1-6), 0 if none.
func (r router) channelInput(src int) int {
	switch {
	case src >= 1 && src <= 32:
		b := r.text(fmt.Sprintf("/config/routing/IN/%s", blocks8[(src-1)/8]))
		return r.block(b, (src-1)%8).input
	case src >= 33 && src <= 38:
		return r.auxInput(src - 32)
	}
	return 0
}

// auxInput is the physical input of aux input n (1-6): the aux inputs, or
// for the first ones what /config/routing/IN/AUX patches there ("A1-4": aux
// 1-4 from AES50-A 1-4).
func (r router) auxInput(n int) int {
	m := auxRe.FindStringSubmatch(r.text("/config/routing/IN/AUX"))
	if m != nil {
		if count, _ := strconv.Atoi(m[2]); n <= count {
			if m[1] == "UIN" {
				return r.userIn(n)
			}
			return inputID(m[1], n)
		}
	}
	return inputID("AuxIN", n)
}

// inputName names a physical input after the first channel or aux input
// taking it.
func (r router) inputName(input int) string {
	for _, strip := range []struct {
		prefix string
		n      int
	}{{"/ch", 32}, {"/auxin", 8}} {
		for i := 1; i <= strip.n; i++ {
			p := fmt.Sprintf("%s/%02d/config", strip.prefix, i)
			if r.channelInput(r.int(p+"/source")) == input {
				return stripName(r.text(p + "/name"))
			}
		}
	}
	return ""
}

// outputName names a console output after its source (/outputs/…/src: OFF,
// Main L, Main R, M/C, buses 1-16, matrices 1-6, direct outs of the 32
// channels, 8 aux inputs and 8 FX returns, monitor L/R, talkback).
func (r router) outputName(kind string, n int) string {
	src := r.int(fmt.Sprintf("/outputs/%s/%02d/src", kind, n))
	named := func(path, fallback string) string {
		if name := stripName(r.text(path + "/config/name")); name != "" {
			return name
		}
		return fallback
	}
	switch {
	case src == 1 || src == 2:
		side := map[int]string{1: "L", 2: "R"}[src]
		if name := stripName(r.text("/main/st/config/name")); name != "" {
			return name + " " + side
		}
		return "Main " + side
	case src == 3:
		return named("/main/m", "Main M/C")
	case src >= 4 && src <= 19:
		return named(fmt.Sprintf("/bus/%02d", src-3), "")
	case src >= 20 && src <= 25:
		return named(fmt.Sprintf("/mtx/%02d", src-19), "")
	case src >= 26 && src <= 57:
		return named(fmt.Sprintf("/ch/%02d", src-25), "")
	case src >= 58 && src <= 65:
		return named(fmt.Sprintf("/auxin/%02d", src-57), "")
	case src >= 66 && src <= 73:
		return named(fmt.Sprintf("/fxrtn/%02d", src-65), "")
	case src == 74 || src == 75:
		return "Monitor " + map[int]string{74: "L", 75: "R"}[src]
	}
	return ""
}

// stripName keeps a strip name that names something ("-" or blanks do not).
func stripName(s string) string {
	s = strings.TrimSpace(s)
	if !hasLetterOrDigit(s) {
		return ""
	}
	return s
}
