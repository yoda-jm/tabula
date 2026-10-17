package schema

import (
	"fmt"
	"strings"

	"github.com/yoda-jm/tabula/pkg/osc"
)

// Effect parameters: each slot has 64 parameters, /fx/N/par/01…64, whose
// meaning, range and text format depend on the effect loaded in the slot.
// Ranges follow Maillot's document; text formats follow real scene files and
// M32-Edit's factory presets. Effects missing from fxEffects are kept as text.

const fxParams = 64

// fxParam declares one effect parameter.
type fxParam struct {
	Label string
	Type  Type
}

// fxEffect is an effect's parameter list; positions after the last
// parameter are written as Unused (most effects write "0").
type fxEffect struct {
	Params []fxParam
	Unused Unused
}

var fxEffects = map[string]fxEffect{
	"HALL": {Params: []fxParam{
		{"Pre Delay", fxLin(0, 200, fixed(0, false, 0), "ms", 20)},
		{"Decay", fxLog(0.2, 5, fixed(2, false, 0), "s", 1.57)},
		{"Size", fxLin(2, 100, fixed(0, false, 0), "", 60)},
		{"Damping", fxLog(1000, 20000, kilo(2), "Hz", 5740)},
		{"Diffuse", fxLin(1, 30, fixed(0, false, 0), "", 25)},
		{"Level", fxLin(-12, 12, fixed(1, false, 0), "dB", 0)},
		{"Lo Cut", fxLog(10, 500, fixed(0, false, 0), "Hz", 83)},
		{"Hi Cut", fxLog(200, 20000, kilo(1), "Hz", 7200)},
		{"Bass Multi", fxLog(0.5, 2, fixed(2, false, 0), "", 0.95)},
		{"Spread", fxLin(0, 50, fixed(0, false, 0), "", 25)},
		{"Shape", fxLin(0, 250, fixed(0, false, 0), "", 50)},
		{"Mod Speed", fxLin(0, 100, fixed(0, false, 0), "", 30)},
	}, Unused: Unused{"0"}},
}

// fxLin and fxLog are continuous effect parameters: the console's step
// counts for effect parameters are not documented.
func fxLin(lo, hi float64, f func(float64) string, unit string, def float64) Num {
	return Num{Scale: Linf{lo, hi}, Fmt: f, Unit: unit, Def: def}
}

func fxLog(lo, hi float64, f func(float64) string, unit string, def float64) Num {
	return Num{Scale: Logf{lo, hi}, Fmt: f, Unit: unit, Def: def}
}

// kilo prints values of 1000 and more in kilo notation with dec decimals
// (5k74, 7k2), and smaller ones as whole numbers.
func kilo(dec int) func(float64) string {
	return func(v float64) string {
		if round(v, 0) < 1000 {
			return fixed(0, false, 0)(v)
		}
		return strings.Replace(fixed(dec, false, 0)(v/1000), ".", "k", 1)
	}
}

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
