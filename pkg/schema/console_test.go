package schema

import (
	"os"
	"strings"
	"testing"

	"github.com/yoda-jm/tabula/pkg/osc"
)

// cacheLines returns node lines of a real M32C, as cached by M32-Edit
// (M32Edit.init, written without the leading slash).
func cacheLines(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/m32edit-cache.txt")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		lines = append(lines, "/"+l)
	}
	return lines
}

func defaults(n *Node) []osc.Arg {
	vals := make([]osc.Arg, len(n.Fields))
	for i, f := range n.Fields {
		vals[i] = f.Type.Default()
	}
	return vals
}

func TestConsoleCacheRoundTrip(t *testing.T) {
	s := Build("M32C")
	for _, line := range cacheLines(t) {
		path, rest := SplitLine(line)
		n := s.Node(path)
		if n == nil {
			t.Errorf("unknown node %s", path)
			continue
		}
		if n.InScene {
			t.Errorf("%s is marked as a scene node", path)
		}
		vals, err := n.ParseTokens(Tokenize(rest))
		if err != nil {
			t.Error(err)
			continue
		}
		if len(vals) != len(n.Fields) {
			t.Errorf("%s has %d values, want %d", path, len(vals), len(n.Fields))
			continue
		}
		if got := n.FormatLine(vals); got != line {
			t.Errorf("\n got %q\nwant %q", got, line)
		}
	}
}

// Defaults are the M32C's values, its network name and presets excepted.
func TestConsoleDefaults(t *testing.T) {
	s := Build("M32C")
	for _, line := range cacheLines(t) {
		path, _ := SplitLine(line)
		if strings.HasPrefix(path, "/-libs/") && !strings.HasSuffix(line, " 0") {
			continue // a factory preset
		}
		want := line
		if path == "/-prefs" {
			want = strings.Replace(line, `"M32C-0A-EB-1A"`, `"tabula-emu"`, 1)
		}
		n := s.Node(path)
		if got := n.FormatLine(defaults(n)); got != want {
			t.Errorf("default\n got %q\nwant %q", got, want)
		}
	}
	for path, want := range map[string]string{
		"/-libs/ch/100":             `/-libs/ch/100 100 "" 0 %0000000000000000 0`,
		"/-prefs/iQ/01":             "/-prefs/iQ/01 none Linear 0", // as in M32-Edit's defaults
		"/-ha/31":                   "/-ha/31 31",
		"/-ha/32":                   "/-ha/32 -1",
		"/-show/showfile/cue/000":   `/-show/showfile/cue/000 0 "" 0 -1 -1 0 1 0 0`,
		"/-show/showfile/scene/099": `/-show/showfile/scene/099 "" "" %000000000 0`,
		"/-stat/screen":             "/-stat/screen HOME OFF OFF",
	} {
		n := s.Node(path)
		if n == nil {
			t.Errorf("missing node %s", path)
			continue
		}
		if got := n.FormatLine(defaults(n)); got != want {
			t.Errorf("default\n got %q\nwant %q", got, want)
		}
	}
}

func TestConsoleLeaves(t *testing.T) {
	s := Build("M32C")
	for addr, want := range map[string]string{
		"/-prefs/style":                   "string",
		"/-prefs/bright":                  "linf [10, 100, 5]",
		"/-prefs/lamp":                    "linf [10, 100, 10]",
		"/-prefs/clockrate":               "enum {48K, 44K1}",
		"/-prefs/haflags":                 "%int (4 bits)",
		"/-prefs/ip/gateway/3":            "int [0, 255]",
		"/-prefs/remote/ioenable":         "%int (14 bits)",
		"/-prefs/card/MADIout":            "enum {OFF, 1-32, 9-40, 17-48, 25-56, 33-64}",
		"/-prefs/rta/gain":                "linf [0, 60, 6] dB",
		"/-prefs/rta/decay":               "logf [0.25, 16, 19] s",
		"/-prefs/iQ/16/iQmodel":           "enum {none, iQ8, iQ10, iQ12, iQ15, iQ15B, iQ18B}",
		"/-prefs/key/layout":              "enum {QWERTY, QWERTZ, AZERTY, ABCDEF}",
		"/-prefs/key/99":                  "string (max 12)",
		"/-stat/lock":                     "int [0, 2]",
		"/-stat/screen/ROUTE/page":        "enum {HOME, AES50A, AES50B, CARDOUT, XLROUT, ANAOUT, AUXOUT, P16OUT, USER}",
		"/-stat/solosw/80":                "enum {OFF, ON}",
		"/-stat/userpar/36/value":         "int [0, 127]",
		"/-stat/urec/rtime":               "int [0, 86399999]",
		"/-show/prepos/current":           "int [-1, 99]",
		"/-show/showfile/show/version":    "string",
		"/-show/showfile/cue/099/numb":    "int [0, 50000]",
		"/-show/showfile/scene/099/safes": "%int (9 bits)",
		"/-libs/mon/100/flags":            "%int (16 bits)",
		"/-usb/dir/maxpos":                "int [0, 999]",
		"/-usb/dir/099/name":              "string",
		"/-urec/session/100/name":         "string (max 19)",
		"/-urec/marker/100/time":          "int [0, 86399999]",
		"/-action/goscene":                "int [0, 99]",
		"/-undo/time":                     "string",
		"/-insert/fx8R":                   "int [-1, 71]",
		"/-insert/aux6":                   "int [-1, 71]",
		"/-ha/39/index":                   "int [-1, 127]",
	} {
		l := s.Leaf(addr)
		if l == nil {
			t.Errorf("missing leaf %s", addr)
			continue
		}
		if got := l.Type().Describe(); got != want {
			t.Errorf("%s: %s, want %s", addr, got, want)
		}
	}
	if n := s.Node("/-insert"); len(n.Fields) != 22 {
		t.Errorf("/-insert has %d fields, want 22", len(n.Fields))
	}
	if n := s.Node("/-stat"); len(n.Fields) != 23 {
		t.Errorf("/-stat has %d fields, want 23", len(n.Fields))
	}

	// Values from the document's tables.
	cases := []struct {
		addr string
		in   osc.Arg
		text string
	}{
		{"/-prefs/rta/decay", osc.Float(0.5556), "2.52"},
		{"/-prefs/rta/decay", osc.Float(0.8889), "10.08"},
		{"/-prefs/rta/decay", osc.Float(1), "16.00"},
		{"/-prefs/rta/visibility", osc.Int(12), "80%"},
		{"/-prefs/rta/gain", osc.Float(1), "60"},
		{"/-stat/selidx", osc.Int(48), "Bus1"},
		{"/-stat/selidx", osc.Int(71), "M/C"},
		{"/-stat/selidx", osc.String("Bs16"), "Bs16"},
		{"/-show/showfile/scene/001/safes", osc.Int(0x106), "%100000110"},
	}
	for _, c := range cases {
		l := s.Leaf(c.addr)
		v, err := l.Type().Coerce(c.in)
		if err != nil {
			t.Errorf("%s %v: %v", c.addr, c.in, err)
			continue
		}
		if got := l.Type().Format(v); got != c.text {
			t.Errorf("%s %v: got %q, want %q", c.addr, c.in, got, c.text)
		}
	}
}

// Console nodes are never in scene files; scene nodes always are.
func TestConsoleNotInScene(t *testing.T) {
	s := Build("M32C")
	for _, n := range s.Nodes {
		if n.InScene == strings.HasPrefix(n.Path, "/-") {
			t.Errorf("%s: InScene %v", n.Path, n.InScene)
		}
	}
	for path, want := range map[string]string{
		"-prefs":             "/-prefs",
		"-prefs/iQ":          "/-prefs/iQ/01",
		"-show":              "/-show/showfile/show",
		"-show/showfile/cue": "/-show/showfile/cue/000",
		"-libs":              "/-libs/ch/001",
		"-ha":                "/-ha/00",
		"-stat/screen":       "/-stat/screen",
		"-urec":              "/-urec",
	} {
		if n := s.Resolve(path); n == nil || n.Path != want {
			t.Errorf("Resolve(%s) = %v, want %s", path, n, want)
		}
	}
}
