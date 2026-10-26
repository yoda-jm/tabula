package osc

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// Byte sequences taken from the examples of the unofficial X32/M32 OSC
// protocol document.
func TestEncodeMatchesDocument(t *testing.T) {
	cases := []struct {
		msg Message
		hex string
	}{
		{NewMessage("/ch/01/eq/1/q", Float(0.4648)), "2f63682f30312f65712f312f710000002c6600003eedfa44"},
		{NewMessage("/ch/01/gate/mode", String("GATE")), "2f63682f30312f676174652f6d6f6465000000002c7300004741544500000000"},
		{NewMessage("/ch/01/gate/mode", Int(3)), "2f63682f30312f676174652f6d6f6465000000002c69000000000003"},
		{NewMessage("/", String("/ch/01/mix/fader 3")), "2f0000002c7300002f63682f30312f6d69782f666164657220330000"},
	}
	for _, c := range cases {
		if got := hex.EncodeToString(c.msg.Bytes()); got != c.hex {
			t.Errorf("%s:\n got %s\nwant %s", c.msg, got, c.hex)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	in := NewMessage("/meters/1", Int(-7), Float(0.25), String("abc"), Blob([]byte{1, 2, 3, 4, 5}))
	out, err := Parse(in.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if out.Address != in.Address || len(out.Args) != len(in.Args) {
		t.Fatalf("got %v, want %v", out, in)
	}
	for i := range in.Args {
		if !in.Args[i].Equal(out.Args[i]) {
			t.Errorf("arg %d: got %v, want %v", i, out.Args[i], in.Args[i])
		}
	}
}

func TestLenientParsing(t *testing.T) {
	// "/info~~~" without a type tag string is accepted by the console.
	m, err := Parse([]byte("/info\x00\x00\x00"))
	if err != nil || m.Address != "/info" || len(m.Args) != 0 {
		t.Fatalf("got %v, %v", m, err)
	}
	// "/info~~~,~~~" with an empty tag string.
	m, err = Parse([]byte("/info\x00\x00\x00,\x00\x00\x00"))
	if err != nil || m.Address != "/info" || len(m.Args) != 0 {
		t.Fatalf("got %v, %v", m, err)
	}
	if _, err := Parse([]byte("#bundle\x00")); err != ErrBundle {
		t.Fatalf("bundle: got %v", err)
	}
	if _, err := Parse([]byte("/x\x00\x00,i\x00\x00\x00\x01")); err == nil {
		t.Fatal("truncated int accepted")
	}
}

// Arguments past the end of a packet are refused: blob sizes near 2^31 (the
// bounds check overflowed on 32-bit platforms, GOARCH=386 or arm, and the
// slice panicked) and "s" tags with no data left (each gave an empty string).
func TestArgumentsPastTheEnd(t *testing.T) {
	for _, size := range []uint32{0x7ffffffc, 0x7fffffff, 0x80000000, 0xfffffffc} {
		b := binary.BigEndian.AppendUint32([]byte("/a\x00\x00,b\x00\x00"), size)
		b = append(b, 1, 2, 3, 4)
		if _, err := Parse(b); !errors.Is(err, ErrMalformed) {
			t.Errorf("blob size %#x: got %v", size, err)
		}
	}
	for _, tags := range []string{",s", ",ss", ",s" + strings.Repeat("s", 4000)} {
		b := appendString([]byte("/a\x00\x00"), tags)
		if tags == ",ss" {
			b = appendString(b, "x") // the first string only
		}
		if m, err := Parse(b); !errors.Is(err, ErrMalformed) {
			t.Errorf("%d s tags: got %d arguments, %v", len(tags)-1, len(m.Args), err)
		}
	}
	// An empty string is four NUL bytes.
	m, err := Parse(appendString([]byte("/a\x00\x00,s\x00\x00"), ""))
	if err != nil || len(m.Args) != 1 || m.Args[0].Str != "" {
		t.Errorf("empty string: got %v, %v", m, err)
	}
}
