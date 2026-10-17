package schema

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/yoda-jm/tabula/pkg/osc"
)

// Effect parameters: each slot has 64 parameters, /fx/N/par/01…64, whose
// meaning, OSC type, range and text format depend on the effect loaded in
// the slot. Slots 1-4 take the effects of fxType1, slots 5-8 those of fxType2,
// which share names and parameters with FX1-4 effects.
//
// The tables follow the ones built into M32-Edit 4.3 (Cfxdsp::fxparcfg_*:
// range, encoder steps, printf format, unit and factory default of each
// parameter, and the enum names), checked against Maillot's document (names,
// OSC types) and against the parameter lines of real scene files and of
// M32-Edit's factory presets (testdata/fx-params.txt). The tables are split
// by family: fx_reverb.go, fx_delay.go, fx_mod.go, fx_eq.go, fx_dyn.go and
// fx_tone.go.
//
// Floats are continuous. The console's encoders move them on a grid of
// "steps" values (M32-Edit's step counts, given in the tables), and all but
// one of the real values sit on it, but a factory preset holds a value off
// it (VREV Hi Cut 11k5): values are kept as given. Factory defaults are put
// on the grid, as the console shows them (a scene has the VRM Hi Multiply
// default, 0.7, as 0.69).

const fxParams = 64

// fxParam declares one effect parameter.
type fxParam struct {
	Label string
	Type  Type
}

// fxEffect is an effect's parameter list; positions after the last
// parameter are written as Unused.
type fxEffect struct {
	Params []fxParam
	Unused Unused
}

var fxEffects = fxMerge(fxReverbs, fxDelays, fxMods, fxEQs, fxDynamics, fxTones)

// fxMerge builds the effect table from the family tables: every effect
// writes "0" at the positions it does not use.
func fxMerge(families ...map[string][]fxParam) map[string]fxEffect {
	out := map[string]fxEffect{}
	for _, fam := range families {
		for name, ps := range fam {
			if _, dup := out[name]; dup || len(ps) > fxParams {
				panic("schema: bad effect table " + name)
			}
			out[name] = fxEffect{Params: ps, Unused: Unused{"0"}}
		}
	}
	return out
}

// Text formats of effect parameters, the console's printf formats: fN is
// "%.Nf", s0 "%+.0f" (balances: +0, -40) and kN "%.Nkf" (see kilo).
var (
	f0, f1, f2 = fixed(0, false, 0), fixed(1, false, 0), fixed(2, false, 0)
	s0         = fixed(0, true, 0)
	k0, k1     = kilo(0), kilo(1)
)

// kilo is the console's "%.<prec>kf" format for frequencies: the value is
// rounded at prec decimals; from 1000 up it is then written in kilo notation
// with one more decimal, truncated, "k" standing for the decimal point. With
// prec 0, 83 is "83", 7262 "7k2" and 20000 "20k0"; with prec 1, 344.61 is
// "344.6" and 5066 "5k06".
func kilo(prec int) func(float64) string {
	return func(v float64) string {
		x := v + 0.5*math.Pow(10, -float64(prec))
		if x < 1000 {
			return fixed(prec, false, 0)(v)
		}
		p := math.Pow(10, float64(prec+1))
		t := math.Floor(x/1000*p+1e-9) / p
		return strings.Replace(strconv.FormatFloat(t, 'f', prec+1, 64), ".", "k", 1)
	}
}

// num and numLog declare a linear and a logarithmic float parameter from its
// range, its number of encoder steps, format, unit and factory default (put
// on the step grid).
func num(label string, lo, hi float64, steps int, f func(float64) string, unit string, def float64) fxParam {
	return fxParam{label, onGrid(Num{Scale: Linf{lo, hi}, Fmt: f, Unit: unit}, steps, def)}
}

func numLog(label string, lo, hi float64, steps int, f func(float64) string, unit string, def float64) fxParam {
	return fxParam{label, onGrid(Num{Scale: Logf{lo, hi}, Fmt: f, Unit: unit}, steps, def)}
}

func onGrid(n Num, steps int, def float64) Num {
	last := float64(steps - 1)
	n.Def = n.Scale.Value(math.Round(n.Scale.Norm(def)*last) / last)
	return n
}

// choice and toggle declare enum parameters, toggle an OFF/ON one.
func choice(label string, names []string, def int32) fxParam {
	return fxParam{label, Enum{Names: names, Def: def}}
}

func toggle(label string, def int32) fxParam { return choice(label, OnOff.Names, def) }

// Parameters shared by many effects.

func preDelay(def float64) fxParam { return num("Pre Delay", 0, 200, 101, f0, "ms", def) }
func decay(lo, hi, def float64) fxParam {
	return numLog("Decay", lo, hi, 51, f2, "s", def)
}
func damping(def float64) fxParam { return numLog("Damping", 1000, 20000, 25, k1, "Hz", def) }
func level(def float64) fxParam   { return num("Level", -12, 12, 49, f1, "dB", def) }
func loCut(label string, def float64) fxParam {
	return numLog(label, 10, 500, 51, f0, "Hz", def)
}
func hiCut(label string, def float64) fxParam {
	return numLog(label, 200, 20000, 51, k0, "Hz", def)
}

// pct is a percentage (mix, depth, gain…) in steps of 2.
func pct(label string, def float64) fxParam { return num(label, 0, 100, 51, f0, "%", def) }
func balance(def float64) fxParam           { return num("Balance", -100, 100, 41, s0, "", def) }
func lfoSpeed(lo, hi, def float64) fxParam {
	return numLog("Speed", lo, hi, 101, f2, "Hz", def)
}

// dual builds the parameters of a dual effect: those of channel A, then
// those of channel B.
func dual(ch func(sfx string) []fxParam) []fxParam { return append(ch(" A"), ch(" B")...) }

// fxFields returns the 64 fields of an effect, or nil if it is not modelled.
func fxFields(effect string) []Field {
	e, ok := fxEffects[effect]
	if !ok {
		return nil
	}
	fs := make([]Field, fxParams)
	for i := range fs {
		fs[i] = Field{Name: two(i + 1), Type: e.Unused}
		if i < len(e.Params) {
			fs[i].Type, fs[i].Label = e.Params[i].Type, e.Params[i].Label
		}
	}
	return fs
}

// fxAny is the static type of effect parameters, used where the effect is
// not known; the store uses the effect's own types.
var fxAny = Num{Scale: Linf{0, 1}, Fmt: fixed(4, false, 0)}

func (b builder) fxSlot(i int) {
	p := fmt.Sprintf("/fx/%d", i)
	types := fxType1
	if i > 4 {
		types = fxType2
	}
	b.node(p, f("type", types))
	if i <= 4 {
		b.node(p+"/source", f("l", fxSource), f("r", fxSource))
	}
	b.s.add(&Node{Path: p + "/par", Fields: fields(fxParams, two, fxAny), InScene: true,
		Variant: &Variant{Selector: p + "/type", Fields: func(sel osc.Arg) []Field {
			return fxFields(types.Format(sel))
		}}})
}
