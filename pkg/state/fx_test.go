package state

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/yoda-jm/tabula/pkg/schema"
)

type fxSample struct {
	line           int
	effect, params string
	want           string // text written back, if not params
}

func readFxSamples(t *testing.T) []fxSample {
	fh, err := os.Open("../../testdata/fx-params.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	var out []fxSample
	sc := bufio.NewScanner(fh)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "= "):
			if len(out) == 0 {
				t.Fatalf("line %d: rewrite without a sample", n)
			}
			out[len(out)-1].want = line[2:]
		default:
			effect, params, _ := strings.Cut(line, " ")
			out = append(out, fxSample{line: n, effect: effect, params: params})
		}
	}
	if len(out) == 0 {
		t.Fatal("no samples")
	}
	return out
}

// Every real effect parameter line loads without error into a slot holding
// its effect, and is written back byte for byte. Effects of FX5-8 are also
// loaded in slot 5.
func TestEffectSamples(t *testing.T) {
	sch := schema.Build("M32C")
	seen := map[string]bool{}
	for _, s := range readFxSamples(t) {
		seen[s.effect] = true
		want := s.params
		if s.want != "" {
			want = s.want
		}
		for _, slot := range []int{1, 5} {
			st := New(sch)
			p := fmt.Sprintf("/fx/%d", slot)
			if _, err := st.ApplyText(p+" "+s.effect, "test"); err != nil {
				if slot == 1 {
					t.Errorf("line %d: %v", s.line, err)
				}
				continue // not an FX5-8 effect
			}
			if errs := st.ApplyLines([]string{p + "/par " + s.params}, "test"); len(errs) > 0 {
				t.Errorf("line %d: %v", s.line, errs)
				continue
			}
			n := sch.Node(p + "/par")
			if _, ok := st.Fields(n); !ok {
				t.Errorf("line %d: %s not modelled", s.line, s.effect)
				continue
			}
			if got := st.NodeLine(n); got != n.Path+" "+want {
				t.Errorf("line %d (%s):\n got %q\nwant %q", s.line, s.effect, got, n.Path+" "+want)
			}
		}
	}
	t.Logf("%d effects with real samples", len(seen))
}
