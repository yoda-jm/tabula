package state

import (
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
