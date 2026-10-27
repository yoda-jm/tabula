package emu

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yoda-jm/tabula/pkg/osc"
)

func TestClassify(t *testing.T) {
	for _, c := range []struct {
		path, field string
		want        leafClass
	}{
		{"/ch/07/mix", "fader", leafClass{strip: "ch", num: 7, part: partFader, field: "fader"}},
		{"/ch/07/mix", "on", leafClass{strip: "ch", num: 7, part: partMute, field: "on"}},
		{"/ch/07/mix", "mlevel", leafClass{strip: "ch", num: 7, part: partLRMC, field: "mlevel"}},
		{"/ch/07/mix/12", "level", leafClass{strip: "ch", num: 7, part: partSend, send: 12, field: "level"}},
		{"/ch/07/gate/filter", "on", leafClass{strip: "ch", num: 7, part: partDyn, field: "on"}},
		{"/auxin/03/preamp", "trim", leafClass{strip: "auxin", num: 3, part: partPreamp, field: "trim"}},
		{"/main/m/mix", "fader", leafClass{strip: "main", num: 2, part: partFader, field: "fader"}},
		{"/dca/4", "on", leafClass{strip: "dca", num: 4, part: partMute, field: "on"}},
		{"/headamp/033", "gain", leafClass{strip: "headamp", num: 33, part: partPreamp, field: "gain"}},
		{"/fx/2/par", "", leafClass{strip: "fx", num: 2, part: partFX}},
		{"/config/userrout/in", "01", leafClass{part: partUserIn, field: "01"}},
		{"/config/routing/IN", "1-8", leafClass{part: partRouting, field: "1-8"}},
		{"/config/dp48/assign", "01", leafClass{part: partSolo, field: "01"}},
		{"/config/mute", "1", leafClass{part: partConsole, field: "1"}},
		{"/outputs/p16/05/iQ", "group", leafClass{part: partOutPatch, field: "group"}},
	} {
		if got := classify(c.path, c.field); got != c.want {
			t.Errorf("%s %s: %+v, want %+v", c.path, c.field, got, c.want)
		}
	}
}

// near compares faders, quantized to their steps.
func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }

// float reads a float parameter of the emulator.
func float(t *testing.T, s *Server, addr string) float32 {
	t.Helper()
	v, _, err := s.Store().Get(addr)
	if err != nil {
		t.Fatal(err)
	}
	return v.Float
}

func TestSceneSafes(t *testing.T) {
	s, addr := start(t)
	p := dial(t, addr)
	st := s.Store()
	set := func(addr string, v osc.Arg) {
		t.Helper()
		if _, err := st.Set(addr, v, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if v, _, _ := st.Get("/-ha/00/index"); v.Int != 0 {
		t.Fatalf("channel 1 is fed by headamp %d, expected 0", v.Int)
	}
	// what the scene holds, then what the console has when it is recalled
	values := func(f float32, on int32) {
		for _, a := range []string{"/ch/01/mix/fader", "/ch/02/mix/fader", "/bus/01/mix/fader", "/main/st/mix/fader", "/headamp/000/gain"} {
			set(a, osc.Float(f))
		}
		set("/ch/01/mix/on", osc.Int(on))
	}
	values(0.5, 1)
	if st := status(t, p, "/save", osc.String("scene"), osc.Int(1), osc.String("Show"), osc.String("")); st != 1 {
		t.Fatalf("save: %d", st)
	}
	const scene, live = 0.5, 0.9

	for _, c := range []struct {
		name  string
		safe  map[string]int32 // safes set for the recall
		after map[string]float32
		on    int32 // /ch/01/mix/on after the recall
	}{
		{"no safes", nil, map[string]float32{"/ch/01/mix/fader": scene, "/headamp/000/gain": scene}, 1},
		{"scene safe: channels", map[string]int32{"/-show/showfile/scene/001/safes": 1 << 4},
			map[string]float32{"/ch/01/mix/fader": live, "/ch/02/mix/fader": live, "/headamp/000/gain": scene, "/bus/01/mix/fader": scene}, 0},
		{"scene safe: preamps", map[string]int32{"/-show/showfile/scene/001/safes": 1 << 6},
			map[string]float32{"/ch/01/mix/fader": scene, "/headamp/000/gain": live}, 1},
		{"scene safe: mix buses", map[string]int32{"/-show/showfile/scene/001/safes": 1 << 3},
			map[string]float32{"/bus/01/mix/fader": live, "/ch/01/mix/fader": scene}, 1},
		{"parameter safe: mute of the inputs", map[string]int32{"/-show/showfile/show/inputs": 1 << 7},
			map[string]float32{"/ch/01/mix/fader": scene}, 0},
		{"parameter safe: faders of the inputs", map[string]int32{"/-show/showfile/show/inputs": 1 << 6},
			map[string]float32{"/ch/01/mix/fader": live, "/ch/02/mix/fader": live, "/bus/01/mix/fader": scene}, 1},
		{"parameter safe: faders of the buses", map[string]int32{"/-show/showfile/show/mxbuses": 1 << 6},
			map[string]float32{"/bus/01/mix/fader": live, "/ch/01/mix/fader": scene}, 1},
		{"channel safe: ch 1 and its headamp", map[string]int32{"/-show/showfile/show/chan16": 1},
			map[string]float32{"/ch/01/mix/fader": live, "/headamp/000/gain": live, "/ch/02/mix/fader": scene}, 0},
		{"channel safe: L/R", map[string]int32{"/-show/showfile/show/lrmtxdca": 1 << 6},
			map[string]float32{"/main/st/mix/fader": live, "/ch/01/mix/fader": scene}, 1},
		{"safe main levels", map[string]int32{"/-prefs/safe_masterlevels": 1},
			map[string]float32{"/main/st/mix/fader": live, "/bus/01/mix/fader": scene}, 1},
	} {
		values(live, 0)
		for a, v := range c.safe {
			set(a, osc.Int(v))
		}
		if got := status(t, p, "/load", osc.String("scene"), osc.Int(1)); got != 1 {
			t.Fatalf("%s: load %d", c.name, got)
		}
		for a, want := range c.after {
			if got := float(t, s, a); !near(got, want) {
				t.Errorf("%s: %s = %.2f, want %.2f", c.name, a, got, want)
			}
		}
		if v, _, _ := st.Get("/ch/01/mix/on"); v.Int != c.on {
			t.Errorf("%s: /ch/01/mix/on = %d, want %d", c.name, v.Int, c.on)
		}
		for a := range c.safe {
			set(a, osc.Int(0))
		}
	}

	// scene 0 has no safes of its own
	if got := status(t, p, "/save", osc.String("scene"), osc.Int(0), osc.String("Base"), osc.String("")); got != 1 {
		t.Fatal(got)
	}
	values(live, 0)
	set("/-show/showfile/scene/000/safes", osc.Int(1<<4))
	status(t, p, "/load", osc.String("scene"), osc.Int(0))
	if got := float(t, s, "/ch/01/mix/fader"); !near(got, scene) {
		t.Errorf("scene 0 kept channels: fader %.2f", got)
	}
}

func TestSnippetFilters(t *testing.T) {
	s, addr := start(t)
	dir := t.TempDir()
	if _, err := s.LoadLibrary(dir); err != nil {
		t.Fatal(err)
	}
	p := dial(t, addr)
	st := s.Store()
	set := func(addr string, v osc.Arg) {
		t.Helper()
		if _, err := st.Set(addr, v, "test"); err != nil {
			t.Fatal(err)
		}
	}
	values := func(f float32, on int32) {
		set("/ch/01/mix/fader", osc.Float(f))
		set("/ch/02/mix/fader", osc.Float(f))
		set("/ch/01/eq/1/g", osc.Float(f))
		set("/ch/01/mix/on", osc.Int(on))
	}
	// faders and pans of ch 1, set by a client before saving (kept in the file)
	p.send("/-show/showfile/snippet/000/eventtyp", osc.Int(1<<6))
	p.send("/-show/showfile/snippet/000/channels", osc.Int(1))
	values(0.5, 1)
	if got := status(t, p, "/save", osc.String("snippet"), osc.Int(0), osc.String("Solo")); got != 1 {
		t.Fatalf("save: %d", got)
	}
	var held []string
	for _, l := range s.lib.items["snippet"][0].Lines {
		held = append(held, l.Suffix)
	}
	if strings.Join(held, " ") != "/ch/01/mix/fader /ch/01/mix/pan" {
		t.Errorf("the snippet holds %q", held)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "snippet-00.snp"))
	if first, _, _ := strings.Cut(string(b), "\n"); first != `#4.0# "Solo" 64 1 0 0 1` {
		t.Errorf("snippet file starts with %q", first)
	}

	values(0.9, 0)
	if got := status(t, p, "/load", osc.String("snippet"), osc.Int(0)); got != 1 {
		t.Fatalf("load: %d", got)
	}
	f1, f2, eq := float(t, s, "/ch/01/mix/fader"), float(t, s, "/ch/02/mix/fader"), float(t, s, "/ch/01/eq/1/g")
	if !near(f1, 0.5) || !near(f2, 0.9) || !near(eq, 0.9) {
		t.Errorf("after the recall: ch 1 fader %.2f (0.5), ch 2 fader %.2f (0.9), ch 1 EQ %.2f (0.9)", f1, f2, eq)
	}
	if v, _, _ := st.Get("/ch/01/mix/on"); v.Int != 0 {
		t.Errorf("the recall changed the mute")
	}

	// a filter taken off after the save: the recall leaves ch 1 alone too
	p.send("/-show/showfile/snippet/000/channels", osc.Int(0))
	values(0.9, 0)
	status(t, p, "/load", osc.String("snippet"), osc.Int(0))
	if f := float(t, s, "/ch/01/mix/fader"); !near(f, 0.9) {
		t.Errorf("ch 1 fader %.2f after a recall without ch 1", f)
	}

	// safes and filters set by a client are kept in the library directory
	status(t, p, "/save", osc.String("scene"), osc.Int(3), osc.String("Show"), osc.String("x"))
	p.send("/-show/showfile/scene/003/safes", osc.Int(1<<8|1<<2))
	p.ask("/info") // the sets above are handled
	s2, _ := start(t)
	if _, err := s2.LoadLibrary(dir); err != nil {
		t.Fatal(err)
	}
	line := func(path string) string { return s2.Store().NodeLine(s2.Store().Schema().Node(path)) }
	if got := line("/-show/showfile/scene/003"); got != `/-show/showfile/scene/003 "Show" "x" %100000100 1` {
		t.Errorf("reloaded scene listing %q", got)
	}
	if got := line("/-show/showfile/snippet/000"); got != `/-show/showfile/snippet/000 "Solo" 64 0 0 0 1` {
		t.Errorf("reloaded snippet listing %q", got)
	}

	// a scene saved again keeps the safes of its slot; deleted, it loses them
	status(t, p, "/save", osc.String("scene"), osc.Int(3), osc.String("Show 2"), osc.String(""))
	if v, _, _ := st.Get("/-show/showfile/scene/003/safes"); v.Int != 1<<8|1<<2 {
		t.Errorf("safes after a new save: %b", v.Int)
	}
	status(t, p, "/delete", osc.String("scene"), osc.Int(3))
	if v, _, _ := st.Get("/-show/showfile/scene/003/safes"); v.Int != 0 {
		t.Errorf("safes after delete: %b", v.Int)
	}
}
