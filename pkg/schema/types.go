package schema

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/yoda-jm/tabula/pkg/osc"
)

// Type describes one console parameter: the OSC type it travels as, its range,
// and how it is written in node (scene) text.
type Type interface {
	Tag() byte
	Default() osc.Arg
	// Format renders a value as a node-text token, padding included.
	Format(osc.Arg) string
	// Parse reads a node-text token.
	Parse(string) (osc.Arg, error)
	// Coerce converts an incoming OSC argument to the canonical, quantized value.
	Coerce(osc.Arg) (osc.Arg, error)
	// Describe is a short human description, e.g. "linf [-18, 18, 0.25] dB".
	Describe() string
}

// Enum is an int carried as an index into Names; clients may also send the name.
type Enum struct {
	Names []string
	Def   int32
}

func (Enum) Tag() byte            { return 'i' }
func (e Enum) Default() osc.Arg   { return osc.Int(e.Def) }
func (e Enum) Describe() string   { return "enum {" + strings.Join(e.Names, ", ") + "}" }
func (e Enum) valid(i int32) bool { return i >= 0 && int(i) < len(e.Names) }
func (e Enum) Format(v osc.Arg) string {
	if e.valid(v.Int) {
		return e.Names[v.Int]
	}
	return strconv.Itoa(int(v.Int))
}

func (e Enum) Parse(s string) (osc.Arg, error) {
	for i, n := range e.Names {
		if n == s {
			return osc.Int(int32(i)), nil
		}
	}
	if i, err := strconv.Atoi(s); err == nil && e.valid(int32(i)) {
		return osc.Int(int32(i)), nil
	}
	return osc.Arg{}, fmt.Errorf("%q is not one of %s", s, e.Describe())
}

func (e Enum) Coerce(a osc.Arg) (osc.Arg, error) {
	switch a.Tag {
	case 's':
		return e.Parse(a.Str)
	case 'i', 'f':
		i := argInt(a)
		if !e.valid(i) {
			return osc.Arg{}, fmt.Errorf("%d out of range for %s", i, e.Describe())
		}
		return osc.Int(i), nil
	}
	return osc.Arg{}, errTag(a, e)
}

// OnOff is the console's boolean.
var OnOff = Enum{Names: []string{"OFF", "ON"}}

// Int is a plain integer in [Min, Max].
type Int struct{ Min, Max, Def int32 }

func (Int) Tag() byte                 { return 'i' }
func (t Int) Default() osc.Arg        { return osc.Int(t.Def) }
func (t Int) Describe() string        { return fmt.Sprintf("int [%d, %d]", t.Min, t.Max) }
func (t Int) Format(v osc.Arg) string { return strconv.Itoa(int(v.Int)) }

func (t Int) Parse(s string) (osc.Arg, error) {
	i, err := strconv.Atoi(s)
	if err != nil {
		return osc.Arg{}, fmt.Errorf("%q is not an int", s)
	}
	return t.Coerce(osc.Int(int32(i)))
}

func (t Int) Coerce(a osc.Arg) (osc.Arg, error) {
	switch a.Tag {
	case 's':
		return t.Parse(a.Str)
	case 'i', 'f':
		i := argInt(a)
		if i < t.Min || i > t.Max {
			return osc.Arg{}, fmt.Errorf("%d out of range for %s", i, t.Describe())
		}
		return osc.Int(i), nil
	}
	return osc.Arg{}, errTag(a, t)
}

// Bitmap is an int written as %0101 with Bits digits, most significant first.
type Bitmap struct{ Bits int }

func (Bitmap) Tag() byte          { return 'i' }
func (Bitmap) Default() osc.Arg   { return osc.Int(0) }
func (b Bitmap) Describe() string { return fmt.Sprintf("%%int (%d bits)", b.Bits) }
func (b Bitmap) Format(v osc.Arg) string {
	s := strconv.FormatInt(int64(uint32(v.Int)), 2)
	if len(s) < b.Bits {
		s = strings.Repeat("0", b.Bits-len(s)) + s
	}
	return "%" + s
}

func (b Bitmap) Parse(s string) (osc.Arg, error) {
	var v int64
	var err error
	if rest, ok := strings.CutPrefix(s, "%"); ok {
		v, err = strconv.ParseInt(rest, 2, 64)
	} else {
		v, err = strconv.ParseInt(s, 10, 64)
	}
	if err != nil {
		return osc.Arg{}, fmt.Errorf("%q is not a bitmap", s)
	}
	return b.Coerce(osc.Int(int32(v)))
}

func (b Bitmap) Coerce(a osc.Arg) (osc.Arg, error) {
	switch a.Tag {
	case 's':
		return b.Parse(a.Str)
	case 'i', 'f':
		i := argInt(a)
		if i < 0 || int64(i) >= int64(1)<<b.Bits {
			return osc.Arg{}, fmt.Errorf("%d out of range for %s", i, b.Describe())
		}
		return osc.Int(i), nil
	}
	return osc.Arg{}, errTag(a, b)
}

// Str is a string written between double quotes in node text. Max 0 is
// no known limit.
type Str struct {
	Max int
	Def string
}

func (Str) Tag() byte                 { return 's' }
func (s Str) Default() osc.Arg        { return osc.String(s.Def) }
func (s Str) Format(v osc.Arg) string { return `"` + v.Str + `"` }

func (s Str) Describe() string {
	if s.Max == 0 {
		return "string"
	}
	return fmt.Sprintf("string (max %d)", s.Max)
}

func (s Str) Parse(tok string) (osc.Arg, error) {
	if len(tok) >= 2 && tok[0] == '"' && tok[len(tok)-1] == '"' {
		tok = tok[1 : len(tok)-1]
	}
	return s.Coerce(osc.String(tok))
}

func (s Str) Coerce(a osc.Arg) (osc.Arg, error) {
	if a.Tag != 's' {
		return osc.Arg{}, errTag(a, s)
	}
	v := a.Str
	if s.Max > 0 && len(v) > s.Max {
		v = v[:s.Max]
	}
	return osc.String(v), nil
}

// Raw keeps node text verbatim. It stands in for parameters whose format
// depends on other state (effect parameters) until they are modelled.
type Raw struct{}

func (Raw) Tag() byte                       { return 's' }
func (Raw) Default() osc.Arg                { return osc.String("") }
func (Raw) Describe() string                { return "raw text (not modelled yet)" }
func (Raw) Format(v osc.Arg) string         { return v.Str }
func (Raw) Parse(s string) (osc.Arg, error) { return osc.String(s), nil }
func (r Raw) Coerce(a osc.Arg) (osc.Arg, error) {
	if a.Tag != 's' {
		return osc.Arg{}, errTag(a, r)
	}
	return a, nil
}

// Scale maps the normalized OSC float [0, 1] to the displayed value.
type Scale interface {
	Value(norm float64) float64
	Norm(value float64) float64
	Describe(steps int) string
}

// Linf is a linear scale.
type Linf struct{ Min, Max float64 }

func (l Linf) Value(n float64) float64 { return l.Min + n*(l.Max-l.Min) }
func (l Linf) Norm(v float64) float64  { return (v - l.Min) / (l.Max - l.Min) }
func (l Linf) Describe(steps int) string {
	return fmt.Sprintf("linf [%g, %g, %g]", l.Min, l.Max, (l.Max-l.Min)/float64(steps-1))
}

// Logf is a logarithmic scale (Min may be larger than Max, as for EQ Q).
type Logf struct{ Min, Max float64 }

func (l Logf) Value(n float64) float64 { return l.Min * math.Exp(n*math.Log(l.Max/l.Min)) }
func (l Logf) Norm(v float64) float64  { return math.Log(v/l.Min) / math.Log(l.Max/l.Min) }
func (l Logf) Describe(steps int) string {
	return fmt.Sprintf("logf [%g, %g, %d]", l.Min, l.Max, steps)
}

// Level is the console's fader law: four linear segments meeting at -60,
// -30 and -10 dB, 0.75 being 0 dB and 1.0 being +10 dB. 0.0 is -oo.
type Level struct{}

func (Level) Value(n float64) float64 {
	switch {
	case n <= 0:
		return math.Inf(-1)
	case n < 0.0625:
		return 480*n - 90
	case n < 0.25:
		return 160*n - 70
	case n < 0.5:
		return 80*n - 50
	}
	return 40*n - 30
}

func (Level) Norm(db float64) float64 {
	switch {
	case math.IsInf(db, -1) || db < -90:
		return 0
	case db < -60:
		return (db + 90) / 480
	case db < -30:
		return (db + 70) / 160
	case db < -10:
		return (db + 50) / 80
	}
	return (db + 30) / 40
}

func (Level) Describe(steps int) string { return fmt.Sprintf("level [-oo, +10, %d]", steps) }

// Num is a float parameter: normalized on the wire, quantized to Steps known
// values, displayed through Scale and Fmt.
type Num struct {
	Scale Scale
	Steps int
	Fmt   func(float64) string
	Unit  string
	Def   float64 // default, in displayed units
}

func (Num) Tag() byte { return 'f' }

func (n Num) Describe() string {
	d := n.Scale.Describe(n.Steps)
	if n.Unit != "" {
		d += " " + n.Unit
	}
	return d
}

func (n Num) quantize(norm float64) float64 {
	if math.IsNaN(norm) || norm <= 0 {
		return 0
	}
	if norm >= 1 {
		return 1
	}
	last := float64(n.Steps - 1)
	return math.Round(norm*last) / last
}

func (n Num) Default() osc.Arg { return osc.Float(float32(n.quantize(n.Scale.Norm(n.Def)))) }

// Display returns the displayed value of a normalized float.
func (n Num) Display(v osc.Arg) float64 {
	return n.Scale.Value(n.quantize(float64(v.Float)))
}

func (n Num) Format(v osc.Arg) string { return n.Fmt(n.Display(v)) }

func (n Num) Parse(s string) (osc.Arg, error) {
	v, err := parseNumber(s)
	if err != nil {
		return osc.Arg{}, err
	}
	return osc.Float(float32(n.quantize(n.Scale.Norm(v)))), nil
}

func (n Num) Coerce(a osc.Arg) (osc.Arg, error) {
	switch a.Tag {
	case 'f':
		return osc.Float(float32(n.quantize(float64(a.Float)))), nil
	case 'i':
		return osc.Float(float32(n.quantize(float64(a.Int)))), nil
	case 's':
		return n.Parse(a.Str)
	}
	return osc.Arg{}, errTag(a, n)
}

// parseNumber reads node-text numbers: plain decimals, "-oo", and the
// kilo notation "1k49" (1490), "10k4" (10400).
func parseNumber(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "-oo" {
		return math.Inf(-1), nil
	}
	if k := strings.IndexByte(s, 'k'); k > 0 {
		v, err := strconv.ParseFloat(s[:k]+"."+s[k+1:], 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a number", s)
		}
		return v * 1000, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	return v, nil
}

func argInt(a osc.Arg) int32 {
	if a.Tag == 'f' {
		return int32(math.Round(float64(a.Float)))
	}
	return a.Int
}

func errTag(a osc.Arg, t Type) error {
	return fmt.Errorf("type tag %q not accepted for %s", a.Tag, t.Describe())
}

// Formatters for node text. Like the console, they round ties away from zero
// (-8.25 prints -8.3) and never print a negative zero.

// round rounds v to prec decimals, ties away from zero.
func round(v float64, prec int) float64 {
	p := math.Pow(10, float64(prec))
	r := math.Round(v*p) / p
	if r == 0 {
		return 0 // no negative zero
	}
	return r
}

func fixed(prec int, sign bool, width int) func(float64) string {
	return func(v float64) string {
		v = round(v, prec)
		s := strconv.FormatFloat(v, 'f', prec, 64)
		if sign && v >= 0 {
			s = "+" + s
		}
		return padLeft(s, width)
	}
}

func padLeft(s string, width int) string {
	if len(s) < width {
		return strings.Repeat(" ", width-len(s)) + s
	}
	return s
}

// levelFmt prints dB levels right-aligned on 5 characters: "  -oo", "-12.0",
// "  0.0", " +0.3" (positive levels carry a sign, zero does not).
func levelFmt(v float64) string {
	if math.IsInf(v, -1) {
		return "  -oo"
	}
	return fixed(1, round(v, 1) > 0, 5)(v)
}

// freqFmt prints frequencies as 295.8 below 1 kHz and 1k49 (truncated) above.
func freqFmt(v float64) string {
	if v < 1000 {
		return fixed(1, false, 0)(v)
	}
	k := int(v / 1000)
	return fmt.Sprintf("%dk%02d", k, int((v-float64(k)*1000)/10))
}

// holdFmt prints three significant digits on 4 characters: 0.02, 50.2, " 502".
func holdFmt(v float64) string {
	switch {
	case v < 9.995:
		return fixed(2, false, 4)(v)
	case v < 99.95:
		return fixed(1, false, 4)(v)
	}
	return fixed(0, false, 4)(v)
}

// releaseFmt prints whole milliseconds after one space: " 49", " 151".
// Unverified against the console for values below 10 or above 999.
func releaseFmt(v float64) string { return " " + fixed(0, false, 0)(v) }
