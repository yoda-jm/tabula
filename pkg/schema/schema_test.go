package schema

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yoda-jm/tabula/pkg/osc"
)

// Every node line of the real scene files must parse, and format back to
// exactly the same text.
func TestSceneLinesRoundTrip(t *testing.T) {
	s := Build("M32C")
	files, _ := filepath.Glob("../../testdata/scenes/*.scn")
	if len(files) == 0 {
		t.Fatal("no test scenes")
	}
	for _, file := range files {
		fh, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<16), 1<<20)
		lines, bad := 0, 0
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "/") {
				continue
			}
			lines++
			path, rest := SplitLine(line)
			n := s.Node(path)
			if n == nil {
				bad++
				t.Errorf("%s: unknown node %s", filepath.Base(file), path)
				continue
			}
			if n.Variant != nil {
				continue // effect parameters: checked through the store (pkg/scene)
			}
			vals, err := n.ParseTokens(tokensFor(n, rest))
			if err != nil {
				bad++
				t.Errorf("%s: %v", filepath.Base(file), err)
				continue
			}
			if len(vals) != len(n.Fields) {
				bad++
				t.Errorf("%s: %s has %d values, want %d", filepath.Base(file), path, len(vals), len(n.Fields))
				continue
			}
			if got := n.FormatLine(vals); got != line {
				bad++
				t.Errorf("%s:\n got %q\nwant %q", filepath.Base(file), got, line)
			}
		}
		fh.Close()
		t.Logf("%s: %d lines, %d mismatches", filepath.Base(file), lines, bad)
	}
}

func tokensFor(n *Node, rest string) []string {
	if _, raw := n.Fields[0].Type.(Raw); raw && len(n.Fields) == 1 {
		return []string{rest}
	}
	return Tokenize(rest)
}

func TestSchemaShape(t *testing.T) {
	s := Build("M32C")
	for addr, want := range map[string]string{
		"/ch/01/mix/fader":       "level [-oo, +10, 1024] dB",
		"/ch/32/mix/16/level":    "level [-oo, +10, 161] dB",
		"/ch/01/eq/4/q":          "logf [10, 0.3, 72]",
		"/headamp/127/gain":      "linf [-12, 60, 0.5] dB",
		"/config/routing/IN/AUX": "",
		"/fx/8/type":             "",
	} {
		l := s.Leaf(addr)
		if l == nil {
			t.Errorf("missing leaf %s", addr)
			continue
		}
		if want != "" && l.Type().Describe() != want {
			t.Errorf("%s: %s, want %s", addr, l.Type().Describe(), want)
		}
	}
	if n := s.Resolve("ch/01"); n == nil || n.Path != "/ch/01/config" {
		t.Errorf("Resolve(ch/01) = %v", n)
	}
	t.Logf("%d nodes, %d parameters", len(s.Nodes), len(s.Leaves()))
}

// Frequencies are truncated in kilo notation, but not below the value they
// stand for: the log scale lands a hair under 2 kHz and 20 kHz.
func TestFrequencyText(t *testing.T) {
	s := Build("M32C")
	for _, c := range []struct {
		addr string
		norm float32
		want string
	}{
		{"/ch/01/eq/1/f", 1, "20k00"},
		{"/ch/01/eq/1/f", 0, "20.0"},
		{"/config/osc/f1", 80.0 / 120, "2k00"},
		{"/config/osc/f1", 79.0 / 120, "1k88"}, // 1888.1
		{"/ch/01/gate/filter/f", 113.0 / 200, "990.9"},
	} {
		l := s.Leaf(c.addr)
		if got := l.Type().Format(osc.Float(c.norm)); got != c.want {
			t.Errorf("%s %v: got %q, want %q", c.addr, c.norm, got, c.want)
		}
	}
}

// Known values from the protocol document.
func TestDocumentedValues(t *testing.T) {
	s := Build("M32C")
	cases := []struct {
		addr string
		in   osc.Arg
		text string
	}{
		{"/ch/01/mix/fader", osc.Float(0.75), "  0.0"},
		{"/ch/01/mix/fader", osc.Float(0.8250), " +3.0"},
		{"/ch/01/mix/fader", osc.Float(0), "  -oo"},
		{"/ch/01/mix/fader", osc.Float(1.0 / 1023), "-89.5"},
		{"/ch/02/mix/pan", osc.Float(0.75), "+50"},
		{"/ch/01/eq/1/q", osc.Float(0.4648), "2.0"},
		{"/ch/01/eq/1/f", osc.Float(0.5), "632.5"},
		{"/ch/01/gate/mode", osc.String("GATE"), "GATE"},
		{"/ch/01/gate/mode", osc.Int(3), "GATE"},
		{"/ch/01/dyn/release", osc.Float(0.51), " 151"},
		{"/ch/01/gate/hold", osc.Float(0.88), " 502"},
		{"/config/osc/f2", osc.Float(0.7), "2k51"},
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
