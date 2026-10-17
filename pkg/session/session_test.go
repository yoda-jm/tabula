package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

func gvo(t *testing.T) *state.Store {
	t.Helper()
	st := state.New(schema.Build("M32C"))
	f, err := os.Open("../../testdata/scenes/GVO-2026.scn")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, errs := scene.Load(f, st, "test"); len(errs) > 0 {
		t.Fatal(errs)
	}
	return st
}

func lines(st *state.Store) map[string]string {
	out := map[string]string{}
	for _, n := range st.Schema().Nodes {
		if n.InScene {
			out[n.Path] = st.NodeLine(n)
		}
	}
	return out
}

func same(t *testing.T, what string, got, want *state.Store) {
	t.Helper()
	g, w := lines(got), lines(want)
	for path, line := range w {
		if g[path] != line {
			t.Fatalf("%s: %s\n got %q\nwant %q", what, path, g[path], line)
		}
	}
}

func TestRecordAndReplay(t *testing.T) {
	live := gvo(t)
	dir := filepath.Join(t.TempDir(), "concert")
	rec, err := Start(dir, live, "M32C test", "set 1")
	if err != nil {
		t.Fatal(err)
	}
	before := gvo(t)
	live.Set("/ch/12/mix/fader", osc.Float(0.6), "console")
	time.Sleep(20 * time.Millisecond)
	rec.Mark("Song 2")
	live.Set("/ch/13/mix/on", osc.Int(0), "console")
	live.ApplyText("/ch/14/config/name Vx Isa", "console")
	if err := rec.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(dir, live, "", ""); err == nil {
		t.Error("a second recording overwrote the session")
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Meta.Events != 3 || s.Meta.Markers != 1 || len(s.Markers()) != 1 || s.Markers()[0].Marker != "Song 2" {
		t.Fatalf("meta %+v, markers %+v", s.Meta, s.Markers())
	}
	// The snapshot opens as a regular scene file.
	f, _ := os.Open(filepath.Join(dir, "start.scn"))
	snap := state.New(schema.Build("M32C"))
	if _, errs := scene.Load(f, snap, "test"); len(errs) > 0 {
		t.Fatal(errs)
	}
	f.Close()
	same(t, "snapshot", snap, before)

	atStart := state.New(schema.Build("M32C"))
	s.StateAt(atStart, 0)
	same(t, "state at 0", atStart, before)
	atEnd := state.New(schema.Build("M32C"))
	s.StateAt(atEnd, s.Meta.Duration)
	same(t, "state at end", atEnd, live)

	// Replaying into a fresh store at high speed ends in the live state.
	target := state.New(schema.Build("M32C"))
	p := &Player{S: s, Target: StoreTarget{target}, Speed: 100}
	if err := p.Play(context.Background(), "M32C", 0); err != nil {
		t.Fatal(err)
	}
	same(t, "replay", target, live)

	metas, dirs, err := List(filepath.Dir(dir))
	if err != nil || len(metas) != 1 || dirs[0] != dir {
		t.Errorf("list: %v %v %v", metas, dirs, err)
	}
}
