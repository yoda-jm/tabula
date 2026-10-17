// Package state holds the values of every console parameter and publishes
// changes. The emulator uses it as the console's memory; the client will use
// it as its mirror of a console.
package state

import (
	"fmt"
	"strings"
	"sync"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
)

// Change is one parameter that took a new value.
type Change struct {
	Leaf   *schema.Leaf
	Value  osc.Arg
	Origin string // who made the change, e.g. "udp 192.168.1.20:51000"
}

// Store is safe for concurrent use. Subscribers are called after the store
// is unlocked, in the order changes were made.
type Store struct {
	sch *schema.Schema

	mu   sync.RWMutex
	vals map[*schema.Node][]osc.Arg

	subMu  sync.Mutex
	subs   map[int]func([]Change)
	nextID int
	notify sync.Mutex // serializes deliveries
}

// New returns a store holding the default value of every parameter.
func New(s *schema.Schema) *Store {
	st := &Store{sch: s, vals: make(map[*schema.Node][]osc.Arg, len(s.Nodes)), subs: map[int]func([]Change){}}
	for _, n := range s.Nodes {
		v := make([]osc.Arg, len(n.Fields))
		for i, f := range n.Fields {
			v[i] = f.Type.Default()
		}
		st.vals[n] = v
	}
	return st
}

func (st *Store) Schema() *schema.Schema { return st.sch }

// Subscribe registers fn for every batch of changes; call the returned
// function to unsubscribe.
func (st *Store) Subscribe(fn func([]Change)) (cancel func()) {
	st.subMu.Lock()
	defer st.subMu.Unlock()
	id := st.nextID
	st.nextID++
	st.subs[id] = fn
	return func() {
		st.subMu.Lock()
		delete(st.subs, id)
		st.subMu.Unlock()
	}
}

func (st *Store) publish(ch []Change) {
	if len(ch) == 0 {
		return
	}
	st.notify.Lock()
	defer st.notify.Unlock()
	st.subMu.Lock()
	subs := make([]func([]Change), 0, len(st.subs))
	for _, fn := range st.subs {
		subs = append(subs, fn)
	}
	st.subMu.Unlock()
	for _, fn := range subs {
		fn(ch)
	}
}

// Get returns the value of the parameter at addr.
func (st *Store) Get(addr string) (osc.Arg, *schema.Leaf, error) {
	l := st.sch.Leaf(addr)
	if l == nil {
		return osc.Arg{}, nil, fmt.Errorf("unknown parameter %s", addr)
	}
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.vals[l.Node][l.Index], l, nil
}

// Set coerces a and stores it at addr; it reports whether the value changed.
func (st *Store) Set(addr string, a osc.Arg, origin string) (bool, error) {
	l := st.sch.Leaf(addr)
	if l == nil {
		return false, fmt.Errorf("unknown parameter %s", addr)
	}
	v, err := l.Type().Coerce(a)
	if err != nil {
		return false, fmt.Errorf("%s: %w", l.Addr, err)
	}
	ch := st.store(l.Node, []int{l.Index}, []osc.Arg{v}, origin)
	st.publish(ch)
	return len(ch) > 0, nil
}

// SetNode sets the leading fields of a node from OSC arguments (the
// multiple-type-tag form, e.g. /ch/01/eq/1 ,ifff).
func (st *Store) SetNode(n *schema.Node, args []osc.Arg, origin string) ([]Change, error) {
	if len(args) > len(n.Fields) {
		return nil, fmt.Errorf("%s: %d values for %d fields", n.Path, len(args), len(n.Fields))
	}
	idx := make([]int, len(args))
	vals := make([]osc.Arg, len(args))
	for i, a := range args {
		v, err := n.Fields[i].Type.Coerce(a)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", n.Path, n.Fields[i].Name, err)
		}
		idx[i], vals[i] = i, v
	}
	ch := st.store(n, idx, vals, origin)
	st.publish(ch)
	return ch, nil
}

// ApplyText applies node text such as "/ch/01/mix ON -3.0" (leading fields
// of a node, a prefix such as "ch/01" standing for its first node) or
// "ch/01/mix/fader -85.4" (one parameter), as the console's "/" command does.
func (st *Store) ApplyText(text, origin string) ([]Change, error) {
	ch, err := st.applyText(text, origin)
	st.publish(ch)
	return ch, err
}

func (st *Store) applyText(text, origin string) ([]Change, error) {
	path, rest := schema.SplitLine(strings.TrimLeft(text, " "))
	if l := st.sch.Leaf(path); l != nil {
		tok := strings.TrimSpace(rest)
		v, err := l.Type().Parse(tok)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", l.Addr, err)
		}
		return st.store(l.Node, []int{l.Index}, []osc.Arg{v}, origin), nil
	}
	n := st.sch.Resolve(path)
	if n == nil {
		return nil, fmt.Errorf("unknown node %s", path)
	}
	var toks []string
	if _, raw := n.Fields[0].Type.(schema.Raw); raw && len(n.Fields) == 1 {
		toks = []string{rest}
	} else {
		toks = schema.Tokenize(rest)
	}
	vals, err := n.ParseTokens(toks)
	if err != nil {
		return nil, err
	}
	idx := make([]int, len(vals))
	for i := range idx {
		idx[i] = i
	}
	return st.store(n, idx, vals, origin), nil
}

// ApplyLines applies many node lines as one batch of changes (scene loading).
// Lines that cannot be applied are returned as errors; the others still apply.
func (st *Store) ApplyLines(lines []string, origin string) []error {
	var all []Change
	var errs []error
	for _, line := range lines {
		ch, err := st.applyText(line, origin)
		if err != nil {
			errs = append(errs, err)
		}
		all = append(all, ch...)
	}
	st.publish(all)
	return errs
}

func (st *Store) store(n *schema.Node, idx []int, vals []osc.Arg, origin string) []Change {
	st.mu.Lock()
	defer st.mu.Unlock()
	cur := st.vals[n]
	var ch []Change
	for k, i := range idx {
		if cur[i].Equal(vals[k]) {
			continue
		}
		cur[i] = vals[k]
		ch = append(ch, Change{Leaf: st.leafOf(n, i), Value: vals[k], Origin: origin})
	}
	return ch
}

func (st *Store) leafOf(n *schema.Node, i int) *schema.Leaf {
	return st.sch.Leaf(n.Path + "/" + n.Fields[i].Name)
}

// Values returns a copy of a node's values.
func (st *Store) Values(n *schema.Node) []osc.Arg {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return append([]osc.Arg(nil), st.vals[n]...)
}

// NodeLine renders a node as text, e.g. "/ch/01/mix ON   0.0 ON +0 OFF   -oo".
func (st *Store) NodeLine(n *schema.Node) string { return n.FormatLine(st.Values(n)) }

// LeafLine renders one parameter as node text, e.g. "/ch/01/mix/fader   0.0".
func (st *Store) LeafLine(l *schema.Leaf) string {
	v, _, _ := st.Get(l.Addr)
	return l.Addr + " " + l.Type().Format(v)
}
