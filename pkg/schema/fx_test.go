package schema

import (
	"strings"
	"testing"

	"github.com/yoda-jm/tabula/pkg/osc"
)

// Every effect of both slot types has a table, with distinct labels.
func TestEffectTables(t *testing.T) {
	known := map[string]bool{}
	for _, types := range []Enum{fxType1, fxType2} {
		for _, name := range types.Names {
			known[name] = true
			if _, ok := fxEffects[name]; !ok {
				t.Errorf("%s: no parameter table", name)
			}
		}
	}
	for name, e := range fxEffects {
		if !known[name] {
			t.Errorf("%s: not an effect type", name)
		}
		labels := map[string]bool{}
		for i, p := range e.Params {
			if p.Label == "" || labels[p.Label] {
				t.Errorf("%s/%02d: label %q", name, i+1, p.Label)
			}
			labels[p.Label] = true
		}
	}
	t.Logf("%d effects", len(fxEffects))
}

// OSC type tags of the effects' parameters, from Maillot's document. Where
// the document is wrong (its tag strings or parameter lists disagree with
// its own ranges, M32-Edit's tables or real files), the comment says so.
var fxTags = map[string]string{
	"HALL": "ffffffffffff", "AMBI": "ffffffffff", "RPLT": "ffffffffffffffff",
	"ROOM": "ffffffffffffffff", "CHAM": "ffffffffffffffff", "PLAT": "ffffffffffff",
	"VREV": "fffiifffff", "VRM": "ffffffffffffi", "GATE": "ffffffffff", "RVRS": "fffffffff",
	"DLY": "ffiiifffffff", "3TAP": "ffffffiffiffiii", "4TAP": "ffffffifififiii",
	"CRS": "fffffffffff", "FLNG": "ffffffffffff", "PHAS": "ffffffffffff", "DIMC": "iiiiiii",
	"FILT": "ffffififffffii", "ROTA": "ffffffii", "PAN": "fffffffff", "SUB": "iifffiifff",
	"D/RV": "fiffffffffff", "CR/R": "ffffffffffff", "FL/R": "ffffffffffff",
	"D/CR": "fiffffffffff", "D/FL": "fiffffffffff", "MODD": "fifffffiiffff",
	"GEQ2": strings.Repeat("f", 64), "GEQ": strings.Repeat("f", 32),
	"TEQ2": strings.Repeat("f", 64), "TEQ": strings.Repeat("f", 32),
	"DES2": "ffffii", "DES": "ffffii",
	"P1A":  "iffifffifii",             // the document has ifffifffifii: Lo Freq is an enum
	"P1A2": "iffifffifii iffifffifii", // same
	"PQ5":  "ififififi", "PQ5S": "ififififi ififififi",
	"WAVD": "ffffff", "LIM": "ffffffii",
	"CMB":   "iifffififiiffffiffiffiffiffii",
	"CMB2":  "iifffififiiffffiffiffiffiffii iifffififiiffffiffiffiffiffii",
	"FAC":   "iffffff",
	"FAC1M": "iffffffffffff", // the document lists it with FAC2; M32-Edit has one switch
	"FAC2":  "iffffffiffffff",
	"LEC":   "iffif", "LEC2": "iffififfif", "ULC": "iffffi", "ULC2": "iffffiiffffi",
	"ENH":  "ffffffffi", // the document's string misses Spread
	"ENH2": "fffffffifffffffi",
	"EXC":  "ffffffi", "EXC2": "ffffffiffffffi", "IMG": "fffffff", "EDI": "iiifffff",
	"SON": "ifffifff", "AMP": "ffffffffi", "AMP2": "ffffffffiffffffffi",
	"DRV": "ffffffffff", "DRV2": "ffffffffffffffffffff",
	"PIT":  "ffffff",       // Lo Cut and Hi Cut, not the Gain and Pan of the document
	"PIT2": "ffffffffffff", // one Mix, after voice 1: the document has 13 parameters
}

func TestEffectTypeTags(t *testing.T) {
	for name, e := range fxEffects {
		var b strings.Builder
		for _, p := range e.Params {
			b.WriteByte(p.Type.Tag())
		}
		if want := strings.ReplaceAll(fxTags[name], " ", ""); b.String() != want {
			t.Errorf("%s: tags %s, want %s", name, b.String(), want)
		}
	}
}

// The defaults of every effect write a line that parses back to the same
// text, and each default is a valid value.
func TestEffectDefaults(t *testing.T) {
	for name := range fxEffects {
		fs := fxFields(name)
		vals := make([]osc.Arg, len(fs))
		for i, f := range fs {
			vals[i] = f.Type.Default()
			if c, err := f.Type.Coerce(vals[i]); err != nil || !c.Equal(vals[i]) {
				t.Errorf("%s/%s: default %v coerces to %v, %v", name, f.Name, vals[i], c, err)
			}
		}
		line := FormatFields("/fx/1/par", fs, vals)
		_, rest := SplitLine(line)
		got, err := ParseFields("/fx/1/par", fs, Tokenize(rest))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if again := FormatFields("/fx/1/par", fs, got); again != line {
			t.Errorf("%s:\n got %q\nwant %q", name, again, line)
		}
	}
}

// Kilo notation as the console writes it: rounded at the format's
// precision, then truncated with one more decimal from 1 kHz up.
func TestKilo(t *testing.T) {
	for _, c := range []struct {
		prec int
		v    float64
		want string
	}{
		{0, 82.69, "83"},
		{0, 999.4, "999"},
		{0, 999.6, "1k0"},
		{0, 7262.2, "7k2"}, // the HALL default Hi Cut, 7200 on the encoder grid
		{0, 4169.4, "4k1"},
		{0, 17583, "17k5"},
		{0, 19999.9999, "20k0"},
		{1, 344.61, "344.6"},
		{1, 5066.4, "5k06"},
		{1, 12139, "12k13"},
		{1, 20000, "20k00"},
	} {
		if got := kilo(c.prec)(c.v); got != c.want {
			t.Errorf("kilo(%d)(%v) = %q, want %q", c.prec, c.v, got, c.want)
		}
	}
}

// Older firmware spellings are read, and written the current way.
func TestEffectAliases(t *testing.T) {
	poles := fxEffects["FILT"].Params[12].Type
	v, err := poles.Parse("ON")
	if err != nil || poles.Format(v) != "4POL" {
		t.Errorf("4 Pole ON: %v %v", v, err)
	}
	freeze := fxEffects["VRM"].Params[12].Type
	if v, err := freeze.Parse("0"); err != nil || freeze.Format(v) != "OFF" {
		t.Errorf("Freeze 0: %v %v", v, err)
	}
}
