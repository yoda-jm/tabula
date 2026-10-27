package session

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

func demo(t *testing.T) *state.Store {
	t.Helper()
	st := state.New(schema.Build("M32C"))
	f, err := os.Open("../../testdata/scenes/demo.scn")
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
	live := demo(t)
	dir := filepath.Join(t.TempDir(), "concert")
	rec, err := Start(dir, live, "M32C test", "set 1")
	if err != nil {
		t.Fatal(err)
	}
	before := demo(t)
	live.Set("/ch/12/mix/fader", osc.Float(0.6), "console")
	time.Sleep(20 * time.Millisecond)
	rec.Mark("Song 2")
	live.Set("/ch/13/mix/on", osc.Int(0), "console")
	live.ApplyText("/ch/14/config/name Lead Margot", "console")
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

// The audio section of meta.json is updated while recording.
func TestRecordAudioMeta(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "concert")
	rec, err := Start(dir, demo(t), "M32C test", "")
	if err != nil {
		t.Fatal(err)
	}
	a := &audio.Info{Rate: 48000, Bits: 24, Channels: 32, Offset: 0.42, Tracks: []audio.Track{{Channel: 1, Name: "Hang", File: "audio/01 Hang.wav"}}}
	if err := rec.SetAudio(a); err != nil {
		t.Fatal(err)
	}
	// Readable at once, as if the program died here.
	metas, _, err := List(filepath.Dir(dir))
	if err != nil || len(metas) != 1 || metas[0].Audio == nil || metas[0].Audio.Offset != 0.42 {
		t.Fatalf("meta while recording: %+v %v", metas, err)
	}
	a.Frames = 4800
	rec.SetAudio(a)
	rec.Stop()
	s, err := Open(dir)
	if err != nil || s.Meta.Audio == nil || s.Meta.Audio.Frames != 4800 || s.Meta.Audio.Tracks[0].Name != "Hang" {
		t.Errorf("meta: %+v %v", s.Meta, err)
	}
	if !rec.Started().Equal(s.Meta.Start) {
		t.Errorf("start %v, meta %v", rec.Started(), s.Meta.Start)
	}
}

// clock notes when each fader change of a replay arrives.
type clock struct {
	at   []time.Time
	text []string
}

func (c *clock) ApplyText(line string) error {
	if rest, ok := strings.CutPrefix(line, "/ch/01/mix/fader "); ok {
		c.at, c.text = append(c.at, time.Now()), append(c.text, rest)
	}
	return nil
}

// A replay keeps the recorded times, divided by the speed, without drifting.
func TestReplayTiming(t *testing.T) {
	s := &Session{Events: []Event{
		{T: 1.0, Addr: "/ch/01/mix/fader", Text: "-20.0"},
		{T: 1.3, Addr: "/ch/01/mix/fader", Text: "-15.0"},
		{T: 1.6, Marker: "Song 2"},
		{T: 1.6, Addr: "/ch/01/mix/fader", Text: "-10.0"},
		{T: 2.2, Addr: "/ch/01/mix/fader", Text: "-5.0"},
		{T: 3.1, Addr: "/ch/01/mix/fader", Text: "0.0"},
	}}
	c := &clock{}
	var progress []float64
	p := &Player{S: s, Target: c, Speed: 3, OnProgress: func(t float64) { progress = append(progress, t) }}
	if err := p.Play(context.Background(), "M32C", 1.0); err != nil {
		t.Fatal(err)
	}
	// the change at 1.0 is part of the state sent first, then 0.1 s, 0.2 s,
	// 0.4 s and 0.7 s after it at 3×
	if want := []string{"-15.0", "-10.0", "-5.0", "0.0"}; strings.Join(c.text, " ") != strings.Join(want, " ") {
		t.Fatalf("changes %q, want %q", c.text, want)
	}
	for i, want := range []float64{0.1, 0.3, 0.6} {
		got := c.at[i+1].Sub(c.at[0]).Seconds()
		t.Logf("change %d: %.4f s after the first, want %.1f", i+2, got, want)
		if math.Abs(got-want) > 0.05 {
			t.Errorf("change %d came %.3f s after the first, want %.3f", i+2, got, want)
		}
	}
	if fmt.Sprint(progress) != "[1.3 1.6 2.2 3.1]" {
		t.Errorf("progress %v", progress)
	}
}

// sentLines keeps every line a replay sends.
type sentLines struct{ sent []string }

func (l *sentLines) ApplyText(line string) error {
	l.sent = append(l.sent, line)
	return nil
}

// A virtual soundcheck keeps the console's inputs on the card: the routing
// switch is neither restored nor replayed, and the tracks start once the
// state is on the console, before the first timed change.
func TestReplaySkipAndReady(t *testing.T) {
	s := &Session{Events: []Event{
		{T: 0.5, Addr: "/config/routing/routswitch", Text: "REC"},
		{T: 0.6, Addr: "/ch/01/mix/fader", Text: "-5.0"},
	}}
	l := &sentLines{}
	ready := -1
	p := &Player{S: s, Target: l, Speed: 50,
		Skip:    func(path string) bool { return path == "/config/routing" || path == "/config/routing/routswitch" },
		OnReady: func() error { ready = len(l.sent); return nil }}
	if err := p.Play(context.Background(), "M32C", 0); err != nil {
		t.Fatal(err)
	}
	for _, line := range l.sent {
		if strings.HasPrefix(line, "/config/routing ") || strings.HasPrefix(line, "/config/routing/routswitch") {
			t.Errorf("sent %q", line)
		}
	}
	if ready < 1000 || ready != len(l.sent)-1 || l.sent[ready] != "/ch/01/mix/fader -5.0" {
		t.Errorf("ready after %d of %d lines", ready, len(l.sent))
	}
	p.OnReady = func() error { return errors.New("no player") }
	if err := p.Play(context.Background(), "M32C", 0); err == nil || err.Error() != "no player" {
		t.Errorf("failed start: %v", err)
	}
}

// TestOpenDamagedEvents reads a session whose events have a damaged line in
// the middle and a partial last line: only those are lost.
func TestOpenDamagedEvents(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(`{"start":"2026-10-26T20:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	events := `{"t":1,"marker":"Song 1"}
{"t":2,"addr":"/ch/01/mix/fader","text":"-10.0","value":0.5}
{"t":3,"ad
{"t":4,"marker":"Song 2"}
{"t":5,"addr":"/ch/01/mix/on","text":"OFF","value":0}
{"t":6,"mar`
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ts []float64
	for _, e := range s.Events {
		ts = append(ts, e.T)
	}
	if fmt.Sprint(ts) != "[1 2 4 5]" || fmt.Sprint(s.Damaged) != "[3 6]" {
		t.Fatalf("events at %v, damaged lines %v; want [1 2 4 5], [3 6]", ts, s.Damaged)
	}
	if m := s.Markers(); len(m) != 2 || m[1].Marker != "Song 2" {
		t.Fatalf("markers %+v", m)
	}
}

// TestRecordWriteError keeps the first error writing the changes, for the
// server to report: the changes after it are lost.
func TestRecordWriteError(t *testing.T) {
	live := demo(t)
	rec, err := Start(t.TempDir(), live, "M32C test", "")
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Stop()
	live.Set("/ch/12/mix/fader", osc.Float(0.6), "console")
	if err := rec.Err(); err != nil {
		t.Fatalf("error before the disk fails: %v", err)
	}
	rec.events.Close() // the disk fails
	live.Set("/ch/12/mix/fader", osc.Float(0.7), "console")
	if err := rec.Err(); err == nil {
		t.Fatal("no error once the changes cannot be written")
	}
}
