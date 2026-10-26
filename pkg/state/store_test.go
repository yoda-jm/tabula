package state

import (
	"strings"
	"testing"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
)

func TestSetGetAndFeed(t *testing.T) {
	st := New(schema.Build("M32C"))
	var got []Change
	cancel := st.Subscribe(func(ch []Change) { got = append(got, ch...) })
	defer cancel()

	changed, err := st.Set("/ch/01/mix/fader", osc.Float(0.75), "test")
	if err != nil || !changed {
		t.Fatalf("set: %v %v", changed, err)
	}
	if changed, _ := st.Set("/ch/01/mix/fader", osc.Float(0.75), "test"); changed {
		t.Error("same value reported as a change")
	}
	if _, err := st.Set("/ch/01/mix/fader", osc.String("x"), "test"); err == nil {
		t.Error("bad value accepted")
	}
	if _, err := st.Set("/ch/01/gate/mode", osc.String("DUCK"), "test"); err != nil {
		t.Error(err)
	}
	v, _, _ := st.Get("/ch/01/gate/mode")
	if v.Int != 4 {
		t.Errorf("gate mode %v, want 4", v)
	}
	if len(got) != 2 || got[0].Leaf.Addr != "/ch/01/mix/fader" || got[0].Origin != "test" {
		t.Errorf("feed: %+v", got)
	}
}

func TestApplyText(t *testing.T) {
	st := New(schema.Build("M32C"))
	for _, text := range []string{
		"ch/01/mix/fader -85.4", // leaf, no leading slash
		"/ch/02 newname 10 CY",  // prefix: first node, leading fields only
		`/ch/03/config "Room L" 61 MG 3`,
	} {
		if _, err := st.ApplyText(text, "test"); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
	for addr, want := range map[string]string{
		"/ch/01/mix/fader":   "/ch/01/mix/fader -85.3", // closest known value, as documented
		"/ch/02/config/name": `/ch/02/config/name "newname"`,
		"/ch/02/config/icon": "/ch/02/config/icon 10",
		"/ch/03/config/name": `/ch/03/config/name "Room L"`,
	} {
		if got := st.LeafLine(st.Schema().Leaf(addr)); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if got := st.NodeLine(st.Schema().Node("/ch/02/config")); got != `/ch/02/config "newname" 10 CY 2` {
		t.Errorf("node line %q", got)
	}
}

func TestEffectParameters(t *testing.T) {
	st := New(schema.Build("M32C"))
	errs := st.ApplyLines([]string{
		"/fx/2 HALL",
		"/fx/2/par 20 1.57 60 5k74 25 0.0 83 7k2 0.95 25 50 30 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
	}, "test")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	sch := st.Schema()
	if got := st.LeafLine(sch.Leaf("/fx/2/par/04")); got != "/fx/2/par/04 5k74" {
		t.Errorf("damping: %q", got)
	}
	if _, err := st.Set("/fx/2/par/06", osc.Float(0.75), "test"); err != nil {
		t.Fatal(err)
	}
	if got := st.LeafLine(sch.Leaf("/fx/2/par/06")); got != "/fx/2/par/06 6.0" {
		t.Errorf("level: %q", got)
	}

	// Loading another effect gives its defaults and its parameter types: the
	// stereo delay's mode is an enum.
	if _, err := st.Set("/fx/2/type", osc.String("DLY"), "test"); err != nil {
		t.Fatal(err)
	}
	if got := st.NodeLine(sch.Node("/fx/2/par")); !strings.HasPrefix(got, "/fx/2/par 100 223 X 1 1 13 10 20k0 97 30 30 20k0 0 0 ") {
		t.Errorf("DLY defaults: %q", got)
	}
	if _, err := st.Set("/fx/2/par/03", osc.Int(0), "test"); err != nil {
		t.Fatal(err)
	}
	if got := st.LeafLine(sch.Leaf("/fx/2/par/03")); got != "/fx/2/par/03 ST" {
		t.Errorf("mode: %q", got)
	}

	// Text that does not parse is kept verbatim, so scenes still round-trip.
	if errs := st.ApplyLines([]string{"/fx/2/par 100 223 Y"}, "test"); len(errs) != 1 {
		t.Errorf("bad mode accepted: %v", errs)
	}
	if got := st.NodeLine(sch.Node("/fx/2/par")); got != "/fx/2/par 100 223 Y" {
		t.Errorf("verbatim: %q", got)
	}

	// Back to HALL: the effect's defaults.
	st.Set("/fx/2/type", osc.String("HALL"), "test")
	if got := st.LeafLine(sch.Leaf("/fx/2/par/01")); got != "/fx/2/par/01 20" {
		t.Errorf("default pre delay: %q", got)
	}
}

// Text kept verbatim for an effect gives way to its values once one of them
// is set, even to the value it had. Text with control characters is never
// kept: it is written back as one line of a scene file.
func TestVerbatimTextGivesWay(t *testing.T) {
	st := New(schema.Build("M32C"))
	n := st.Schema().Node("/fx/1/par")
	for _, set := range []float32{0.9, 0.9} {
		if _, err := st.ApplyText("/fx/1/par 50 bogus", "test"); err == nil {
			t.Fatal("bad text accepted")
		}
		if got := st.NodeLine(n); got != "/fx/1/par 50 bogus" {
			t.Fatalf("verbatim: %q", got)
		}
		if _, err := st.Set("/fx/1/par/01", osc.Float(set), "test"); err != nil {
			t.Fatal(err)
		}
		if got := st.NodeLine(n); strings.Contains(got, "bogus") {
			t.Errorf("after a set: %q", got)
		}
	}
	want := st.NodeLine(n)
	for _, text := range []string{"/fx/1/par 50 bogus\n/ch/01/mix OFF", "/fx/1/par 50\tbogus", "/fx/1/par 50 bogus\x00"} {
		if _, err := st.ApplyText(text, "test"); err == nil {
			t.Errorf("%q accepted", text)
		}
		if got := st.NodeLine(n); got != want {
			t.Errorf("%q kept: %q", text, got)
		}
	}
}
