// Package state holds the values of every console parameter and publishes
// changes. The emulator uses it as the console's memory; the client will use
// it as its mirror of a console.
package state

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
)

// ErrNotModelled is returned for parameters whose type depends on a value
// with no model (an effect type the schema does not know).
var ErrNotModelled = errors.New("not modelled yet")

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
	// raw keeps the verbatim text of variant nodes that are not modelled for
	// their current selector, or whose text did not parse.
	raw map[*schema.Node]string

	subMu  sync.Mutex
	subs   map[int]func([]Change)
	nextID int
	notify sync.Mutex // serializes deliveries
}

// New returns a store holding the default value of every parameter.
func New(s *schema.Schema) *Store {
	st := &Store{sch: s, vals: make(map[*schema.Node][]osc.Arg, len(s.Nodes)),
		raw: map[*schema.Node]string{}, subs: map[int]func([]Change){}}
	for _, n := range s.Nodes {
		v := make([]osc.Arg, len(n.Fields))
		for i, f := range n.Fields {
			v[i] = f.Type.Default()
		}
		st.vals[n] = v
	}
	for _, n := range s.Nodes {
		if n.Variant != nil {
			st.resetLocked(n, "default")
		}
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

// fieldsLocked returns the current fields of n; ok is false for a variant
// node whose selector value is not modelled.
func (st *Store) fieldsLocked(n *schema.Node) ([]schema.Field, bool) {
	if n.Variant == nil {
		return n.Fields, true
	}
	sel := st.sch.Leaf(n.Variant.Selector)
	fs := n.Variant.Fields(st.vals[sel.Node][sel.Index])
	return fs, fs != nil
}

// Fields returns the current fields of a node (they vary for variant nodes);
// ok is false when the node is not modelled for its current selector.
func (st *Store) Fields(n *schema.Node) ([]schema.Field, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.fieldsLocked(n)
}

// TypeOf returns the current type of a parameter, nil if not modelled.
func (st *Store) TypeOf(l *schema.Leaf) schema.Type {
	fs, ok := st.Fields(l.Node)
	if !ok {
		return nil
	}
	return fs[l.Index].Type
}

func (st *Store) mutate(fn func() ([]Change, error)) ([]Change, error) {
	st.mu.Lock()
	ch, err := fn()
	st.mu.Unlock()
	st.publish(ch)
	return ch, err
}

// Get returns the value of the parameter at addr.
func (st *Store) Get(addr string) (osc.Arg, *schema.Leaf, error) {
	l := st.sch.Leaf(addr)
	if l == nil {
		return osc.Arg{}, nil, fmt.Errorf("unknown parameter %s", addr)
	}
	st.mu.RLock()
	defer st.mu.RUnlock()
	if _, ok := st.fieldsLocked(l.Node); !ok {
		return osc.Arg{}, l, fmt.Errorf("%s: %w", l.Addr, ErrNotModelled)
	}
	return st.vals[l.Node][l.Index], l, nil
}

// Set coerces a and stores it at addr; it reports whether the value changed.
func (st *Store) Set(addr string, a osc.Arg, origin string) (bool, error) {
	l := st.sch.Leaf(addr)
	if l == nil {
		return false, fmt.Errorf("unknown parameter %s", addr)
	}
	ch, err := st.mutate(func() ([]Change, error) {
		fs, ok := st.fieldsLocked(l.Node)
		if !ok {
			return nil, fmt.Errorf("%s: %w", l.Addr, ErrNotModelled)
		}
		v, err := fs[l.Index].Type.Coerce(a)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", l.Addr, err)
		}
		return st.storeLocked(l.Node, []int{l.Index}, []osc.Arg{v}, origin), nil
	})
	return len(ch) > 0, err
}

// SetNode sets the leading fields of a node from OSC arguments (the
// multiple-type-tag form, e.g. /ch/01/eq/1 ,ifff).
func (st *Store) SetNode(n *schema.Node, args []osc.Arg, origin string) ([]Change, error) {
	return st.mutate(func() ([]Change, error) {
		fs, ok := st.fieldsLocked(n)
		if !ok {
			return nil, fmt.Errorf("%s: %w", n.Path, ErrNotModelled)
		}
		if len(args) > len(fs) {
			return nil, fmt.Errorf("%s: %d values for %d fields", n.Path, len(args), len(fs))
		}
		idx := make([]int, len(args))
		vals := make([]osc.Arg, len(args))
		for i, a := range args {
			v, err := fs[i].Type.Coerce(a)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", n.Path, fs[i].Name, err)
			}
			idx[i], vals[i] = i, v
		}
		return st.storeLocked(n, idx, vals, origin), nil
	})
}

// ApplyText applies node text such as "/ch/01/mix ON -3.0" (leading fields
// of a node, a prefix such as "ch/01" standing for its first node) or
// "ch/01/mix/fader -85.4" (one parameter), as the console's "/" command does.
func (st *Store) ApplyText(text, origin string) ([]Change, error) {
	return st.mutate(func() ([]Change, error) { return st.applyTextLocked(text, origin) })
}

// ApplyLines applies many node lines as one batch of changes (scene loading).
// Lines that cannot be applied are returned as errors; the others still apply.
func (st *Store) ApplyLines(lines []string, origin string) []error {
	var errs []error
	st.mutate(func() ([]Change, error) {
		var all []Change
		for _, line := range lines {
			ch, err := st.applyTextLocked(line, origin)
			if err != nil {
				errs = append(errs, err)
			}
			all = append(all, ch...)
		}
		return all, nil
	})
	return errs
}

func (st *Store) applyTextLocked(text, origin string) ([]Change, error) {
	// Node text may keep the line feed of /node replies: M32-Edit sends it
	// back like that when it writes a whole scene to the console.
	text = strings.TrimRight(text, "\r\n")
	path, rest := schema.SplitLine(strings.TrimLeft(text, " "))
	if l := st.sch.Leaf(path); l != nil {
		fs, ok := st.fieldsLocked(l.Node)
		if !ok {
			return nil, fmt.Errorf("%s: %w", l.Addr, ErrNotModelled)
		}
		v, err := fs[l.Index].Type.Parse(strings.TrimSpace(rest))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", l.Addr, err)
		}
		return st.storeLocked(l.Node, []int{l.Index}, []osc.Arg{v}, origin), nil
	}
	n := st.sch.Resolve(path)
	if n == nil {
		return nil, fmt.Errorf("unknown node %s", path)
	}
	fs, ok := st.fieldsLocked(n)
	if !ok {
		st.raw[n] = rest
		return nil, nil
	}
	var toks []string
	if _, raw := fs[0].Type.(schema.Raw); raw && len(fs) == 1 {
		toks = []string{rest}
	} else {
		toks = schema.Tokenize(rest)
	}
	vals, err := schema.ParseFields(n.Path, fs, toks)
	if err != nil {
		if n.Variant != nil {
			st.raw[n] = rest // keep the text so scenes still round-trip
			return nil, fmt.Errorf("%w (kept as text)", err)
		}
		return nil, err
	}
	delete(st.raw, n)
	idx := make([]int, len(vals))
	for i := range idx {
		idx[i] = i
	}
	return st.storeLocked(n, idx, vals, origin), nil
}

func (st *Store) storeLocked(n *schema.Node, idx []int, vals []osc.Arg, origin string) []Change {
	cur := st.vals[n]
	var ch []Change
	for k, i := range idx {
		if cur[i].Equal(vals[k]) {
			continue
		}
		cur[i] = vals[k]
		l := st.leafOf(n, i)
		ch = append(ch, Change{Leaf: l, Value: vals[k], Origin: origin})
		for _, dep := range st.sch.Dependents(l.Addr) {
			ch = append(ch, st.resetLocked(dep, origin)...)
		}
	}
	return ch
}

// resetLocked gives a variant node the defaults of its current fields, as
// the console does when another effect is loaded in a slot.
func (st *Store) resetLocked(n *schema.Node, origin string) []Change {
	fs, ok := st.fieldsLocked(n)
	if !ok {
		st.raw[n] = strings.TrimSpace(strings.Repeat("0 ", len(n.Fields)))
		return nil
	}
	delete(st.raw, n)
	var ch []Change
	cur := st.vals[n]
	for i, f := range fs {
		v := f.Type.Default()
		if cur[i].Equal(v) {
			continue
		}
		cur[i] = v
		ch = append(ch, Change{Leaf: st.leafOf(n, i), Value: v, Origin: origin})
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
func (st *Store) NodeLine(n *schema.Node) string {
	st.mu.RLock()
	defer st.mu.RUnlock()
	if raw, ok := st.raw[n]; ok {
		return n.Path + " " + raw
	}
	fs, _ := st.fieldsLocked(n)
	return schema.FormatFields(n.Path, fs, st.vals[n])
}

// LeafLine renders one parameter as node text, e.g. "/ch/01/mix/fader   0.0".
func (st *Store) LeafLine(l *schema.Leaf) string {
	v, _, err := st.Get(l.Addr)
	if err != nil {
		return l.Addr
	}
	return l.Addr + " " + st.TypeOf(l).Format(v)
}
