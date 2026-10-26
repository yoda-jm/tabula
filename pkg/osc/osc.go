// Package osc encodes and decodes Open Sound Control 1.0 messages, with the
// leniencies the X32/M32 family relies on: a missing type tag string is
// accepted, and replies such as "node" do not start with a slash.
package osc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Arg is one OSC argument. Tag selects which field holds the value.
type Arg struct {
	Tag   byte // 'i', 'f', 's' or 'b'
	Int   int32
	Float float32
	Str   string
	Blob  []byte
}

func Int(v int32) Arg     { return Arg{Tag: 'i', Int: v} }
func Float(v float32) Arg { return Arg{Tag: 'f', Float: v} }
func String(v string) Arg { return Arg{Tag: 's', Str: v} }
func Blob(v []byte) Arg   { return Arg{Tag: 'b', Blob: v} }

// Equal reports whether two arguments carry the same tag and value.
func (a Arg) Equal(b Arg) bool {
	if a.Tag != b.Tag {
		return false
	}
	switch a.Tag {
	case 'i':
		return a.Int == b.Int
	case 'f':
		return a.Float == b.Float
	case 's':
		return a.Str == b.Str
	case 'b':
		return string(a.Blob) == string(b.Blob)
	}
	return true
}

func (a Arg) String() string {
	switch a.Tag {
	case 'i':
		return strconv.Itoa(int(a.Int))
	case 'f':
		return strconv.FormatFloat(float64(a.Float), 'f', 4, 32)
	case 's':
		return strconv.Quote(a.Str)
	case 'b':
		return fmt.Sprintf("<blob %dB>", len(a.Blob))
	}
	return "<" + string(a.Tag) + ">"
}

// Message is a single OSC message (bundles are not used by the console).
type Message struct {
	Address string
	Args    []Arg
}

func NewMessage(addr string, args ...Arg) Message { return Message{Address: addr, Args: args} }

// Tags returns the type tag string, e.g. ",ifff".
func (m Message) Tags() string {
	b := make([]byte, 0, len(m.Args)+1)
	b = append(b, ',')
	for _, a := range m.Args {
		b = append(b, a.Tag)
	}
	return string(b)
}

// String renders the message for logs: /ch/01/mix/fader ,f 0.7500
func (m Message) String() string {
	if len(m.Args) == 0 {
		return m.Address
	}
	parts := make([]string, 0, len(m.Args)+2)
	parts = append(parts, m.Address, m.Tags())
	for _, a := range m.Args {
		parts = append(parts, a.String())
	}
	return strings.Join(parts, " ")
}

// Bytes encodes the message.
func (m Message) Bytes() []byte {
	b := appendString(nil, m.Address)
	b = appendString(b, m.Tags())
	for _, a := range m.Args {
		switch a.Tag {
		case 'i':
			b = binary.BigEndian.AppendUint32(b, uint32(a.Int))
		case 'f':
			b = binary.BigEndian.AppendUint32(b, math.Float32bits(a.Float))
		case 's':
			b = appendString(b, a.Str)
		case 'b':
			b = binary.BigEndian.AppendUint32(b, uint32(len(a.Blob)))
			b = append(b, a.Blob...)
			b = pad(b)
		}
	}
	return b
}

var (
	ErrBundle    = errors.New("osc: bundles are not supported")
	ErrMalformed = errors.New("osc: malformed message")
)

// Parse decodes one message.
func Parse(b []byte) (Message, error) {
	addr, n := readString(b, 0)
	if addr == "" {
		return Message{}, ErrMalformed
	}
	if addr == "#bundle" {
		return Message{}, ErrBundle
	}
	m := Message{Address: addr}
	if n >= len(b) || b[n] != ',' {
		return m, nil // no type tag string: older OSC form, accepted by the console
	}
	tags, n := readString(b, n)
	for _, t := range []byte(tags[1:]) {
		switch t {
		case 'i', 'f':
			if n+4 > len(b) {
				return m, fmt.Errorf("%w: truncated %c argument", ErrMalformed, t)
			}
			v := binary.BigEndian.Uint32(b[n:])
			n += 4
			if t == 'i' {
				m.Args = append(m.Args, Int(int32(v)))
			} else {
				m.Args = append(m.Args, Float(math.Float32frombits(v)))
			}
		case 's':
			// readString would give an empty string past the end: a run of
			// "s" tags with no data is not a run of empty strings.
			if n >= len(b) {
				return m, fmt.Errorf("%w: missing s argument", ErrMalformed)
			}
			var s string
			s, n = readString(b, n)
			m.Args = append(m.Args, String(s))
		case 'b':
			if n+4 > len(b) {
				return m, fmt.Errorf("%w: truncated blob size", ErrMalformed)
			}
			size := int(binary.BigEndian.Uint32(b[n:]))
			n += 4
			// size > len(b)-n rather than n+size > len(b): the sum overflows
			// on 32-bit platforms for sizes near 2^31.
			if size < 0 || size > len(b)-n {
				return m, fmt.Errorf("%w: truncated blob", ErrMalformed)
			}
			m.Args = append(m.Args, Blob(append([]byte(nil), b[n:n+size]...)))
			n = align(n + size)
		default:
			return m, fmt.Errorf("%w: unsupported type tag %q", ErrMalformed, t)
		}
	}
	return m, nil
}

func appendString(b []byte, s string) []byte {
	b = append(b, s...)
	b = append(b, 0)
	return pad(b)
}

func pad(b []byte) []byte {
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func align(n int) int { return (n + 3) &^ 3 }

// readString reads a NUL-terminated string at off and returns it with the
// aligned offset that follows. A missing terminator ends the string at the
// end of the buffer.
func readString(b []byte, off int) (string, int) {
	if off >= len(b) {
		return "", len(b)
	}
	end := off
	for end < len(b) && b[end] != 0 {
		end++
	}
	return string(b[off:end]), min(align(end+1), len(b))
}
