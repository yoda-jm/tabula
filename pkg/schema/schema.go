// Package schema describes every parameter of an X32/M32 console: its OSC
// address, type and range, and how it is grouped into nodes, the lines used
// by /node replies and scene files.
package schema

import (
	"fmt"
	"strings"

	"github.com/yoda-jm/tabula/pkg/osc"
)

// Field is one parameter of a node; its OSC address is Node.Path + "/" + Name.
type Field struct {
	Name  string
	Type  Type
	Label string // human name when Name is only a number (effect parameters)
}

// Node is a group of parameters written as one text line, e.g.
// "/ch/01/mix ON  -3.0 ON +0 OFF   -oo".
type Node struct {
	Path   string
	Fields []Field
	// InScene is true for nodes written in scene files.
	InScene bool
	// Variant, when set, makes the field types depend on another parameter.
	Variant *Variant
	// Lazy nodes are not read when connecting: clients read them when they
	// need them (the entries of the USB stick's directory).
	Lazy bool
}

// Variant makes a node's field types depend on the value of another
// parameter, its selector: effect parameters depend on the effect type.
type Variant struct {
	Selector string // address of the selecting parameter, e.g. "/fx/1/type"
	// Fields returns the node's fields for a selector value (same count and
	// names as Node.Fields), or nil when that value is not modelled yet: the
	// node text is then kept verbatim.
	Fields func(sel osc.Arg) []Field
}

// Leaf is one addressable parameter.
type Leaf struct {
	Addr  string
	Node  *Node
	Index int
}

func (l *Leaf) Type() Type { return l.Node.Fields[l.Index].Type }

// Schema is the full parameter tree of a console model.
type Schema struct {
	Model  Model
	Nodes  []*Node // scene nodes in scene file order, then the others
	nodes  map[string]*Node
	leaves map[string]*Leaf
	order  []*Leaf
	deps   map[string][]*Node // selector address -> variant nodes
}

func newSchema(m Model) *Schema {
	return &Schema{Model: m, nodes: map[string]*Node{}, leaves: map[string]*Leaf{}, deps: map[string][]*Node{}}
}

func (s *Schema) add(n *Node) {
	if _, dup := s.nodes[n.Path]; dup {
		panic("schema: duplicate node " + n.Path)
	}
	s.Nodes = append(s.Nodes, n)
	s.nodes[n.Path] = n
	for i, f := range n.Fields {
		l := &Leaf{Addr: n.Path + "/" + f.Name, Node: n, Index: i}
		if _, dup := s.leaves[l.Addr]; dup {
			panic("schema: duplicate leaf " + l.Addr)
		}
		s.leaves[l.Addr] = l
		s.order = append(s.order, l)
	}
	if n.Variant != nil {
		s.deps[n.Variant.Selector] = append(s.deps[n.Variant.Selector], n)
	}
}

// Dependents returns the variant nodes selected by the parameter at addr.
func (s *Schema) Dependents(addr string) []*Node { return s.deps[addr] }

func canon(path string) string {
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

// Node returns the node at path ("/ch/01/mix" or "ch/01/mix").
func (s *Schema) Node(path string) *Node { return s.nodes[canon(path)] }

// Leaf returns the parameter at addr.
func (s *Schema) Leaf(addr string) *Leaf { return s.leaves[canon(addr)] }

// Leaves returns all parameters in scene order.
func (s *Schema) Leaves() []*Leaf { return s.order }

// Resolve maps a /node or "/" request path to a node: an exact node, or the
// first node below a prefix ("ch/01" is "/ch/01/config"), as the console does.
func (s *Schema) Resolve(path string) *Node {
	p := canon(strings.TrimRight(path, "/"))
	if n := s.nodes[p]; n != nil {
		return n
	}
	for _, n := range s.Nodes {
		if strings.HasPrefix(n.Path, p+"/") {
			return n
		}
	}
	return nil
}

// FormatLine renders a node line from its values.
func (n *Node) FormatLine(vals []osc.Arg) string { return FormatFields(n.Path, n.Fields, vals) }

// ParseTokens parses node-text tokens. Fewer tokens than fields is allowed:
// only the leading fields are returned, as the console does for "/" commands.
func (n *Node) ParseTokens(toks []string) ([]osc.Arg, error) {
	return ParseFields(n.Path, n.Fields, toks)
}

// FormatFields renders a node line from fields and values.
func FormatFields(path string, fields []Field, vals []osc.Arg) string {
	var b strings.Builder
	b.WriteString(path)
	for i, f := range fields {
		b.WriteByte(' ')
		b.WriteString(f.Type.Format(vals[i]))
	}
	return b.String()
}

// ParseFields parses node-text tokens for the given fields.
func ParseFields(path string, fields []Field, toks []string) ([]osc.Arg, error) {
	if len(fields) == 1 {
		if _, raw := fields[0].Type.(Raw); raw {
			v, _ := Raw{}.Parse(strings.Join(toks, " "))
			return []osc.Arg{v}, nil
		}
	}
	if len(toks) > len(fields) {
		return nil, fmt.Errorf("%s: %d values for %d fields", path, len(toks), len(fields))
	}
	vals := make([]osc.Arg, len(toks))
	for i, tok := range toks {
		v, err := fields[i].Type.Parse(tok)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", path, fields[i].Name, err)
		}
		vals[i] = v
	}
	return vals, nil
}

// SplitLine splits "path rest..." into the path and the text after the first
// space, kept verbatim.
func SplitLine(line string) (path, rest string) {
	path, rest, _ = strings.Cut(line, " ")
	return path, rest
}

// Tokenize splits node text on spaces (and other blanks such as a trailing
// line feed), keeping "quoted strings" whole.
func Tokenize(s string) []string {
	blank := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
	var toks []string
	for i := 0; i < len(s); {
		if blank(s[i]) {
			i++
			continue
		}
		start := i
		if s[i] == '"' {
			end := strings.IndexByte(s[i+1:], '"')
			if end < 0 {
				toks = append(toks, s[i:])
				break
			}
			i += end + 2
		} else {
			for i < len(s) && !blank(s[i]) {
				i++
			}
		}
		toks = append(toks, s[start:i])
	}
	return toks
}
