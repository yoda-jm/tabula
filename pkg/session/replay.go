package session

import (
	"context"
	"time"

	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

// Target receives a replay: a console client or anything that takes node
// text ("/ch/01/mix/fader -3.0", whole node lines).
type Target interface {
	ApplyText(line string) error
}

// StoreTarget replays into a store.
type StoreTarget struct{ Store *state.Store }

func (t StoreTarget) ApplyText(line string) error {
	_, err := t.Store.ApplyText(line, "replay")
	return err
}

// Player replays a session onto a target.
type Player struct {
	S      *Session
	Target Target
	Speed  float64 // 1 = real time
	// OnProgress, if set, is called with the replay position after each event.
	OnProgress func(t float64)
	// Skip, if set, leaves out the nodes and parameters it names (a node
	// path, or a parameter's address): a virtual soundcheck keeps the
	// console's inputs on the card.
	Skip func(path string) bool
	// OnReady, if set, is called once the state at from is on the target,
	// as the timed changes start (the tracks of a virtual soundcheck start
	// playing then); an error ends the replay.
	OnReady func() error
}

// Play sends the state at from (snapshot plus changes so far) as node lines,
// then each later change at its time, until the end or ctx is done.
func (p *Player) Play(ctx context.Context, model schema.Model, from float64) error {
	scratch := state.New(schema.Build(model))
	p.S.StateAt(scratch, from)
	skip := p.Skip
	if skip == nil {
		skip = func(string) bool { return false }
	}
	for _, n := range scratch.Schema().Nodes {
		if !n.InScene && !isPlayable(n.Path) || skip(n.Path) {
			continue
		}
		if err := p.Target.ApplyText(scratch.NodeLine(n)); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	speed := p.Speed
	if speed <= 0 {
		speed = 1
	}
	if p.OnReady != nil {
		if err := p.OnReady(); err != nil {
			return err
		}
	}
	wall := time.Now()
	for _, e := range p.S.Events {
		if e.T <= from || e.Addr == "" || skip(e.Addr) {
			continue
		}
		due := wall.Add(time.Duration((e.T - from) / speed * float64(time.Second)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Until(due)):
		}
		if err := p.Target.ApplyText(e.Addr + " " + e.Text); err != nil {
			return err
		}
		if p.OnProgress != nil {
			p.OnProgress(e.T)
		}
	}
	return nil
}

// isPlayable says which non-scene nodes a replay restores: the ones that
// change what is heard or seen (solo, mute groups are in the scene), not
// preferences, network settings or library listings.
func isPlayable(path string) bool {
	for _, p := range []string{"/-stat/solosw", "/-stat/selidx", "/-stat/chfaderbank", "/-stat/grpfaderbank"} {
		if path == p || len(path) > len(p) && path[:len(p)+1] == p+"/" {
			return true
		}
	}
	return false
}
