package emu

import (
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
)

// get asks the value of addr, skipping /xremote pushes of other parameters.
func get(t *testing.T, p *peer, addr string) []osc.Arg {
	t.Helper()
	p.send(addr)
	for {
		m, ok := p.recv(time.Second)
		if !ok {
			t.Fatalf("no reply to %s", addr)
		}
		if m.Address == addr {
			return m.Args
		}
	}
}

// same checks that two parameters hold the same value, or differ.
func same(t *testing.T, p *peer, a, b string, want bool) {
	t.Helper()
	va, vb := get(t, p, a), get(t, p, b)
	eq := len(va) == 1 && len(vb) == 1 && va[0].Tag == vb[0].Tag &&
		va[0].Int == vb[0].Int && va[0].Float == vb[0].Float && va[0].Str == vb[0].Str
	if eq != want {
		t.Errorf("%s = %v, %s = %v: same %v, want %v", a, va, b, vb, eq, want)
	}
}

func TestLinkedChannels(t *testing.T) {
	_, addr := start(t)
	a, b := dial(t, addr), dial(t, addr)
	for _, opt := range []string{"hadly", "eq", "dyn", "fdrmute"} {
		a.send("/config/linkcfg/"+opt, osc.Int(1))
	}
	a.send("/ch/01/config/name", osc.String("Left"))
	a.send("/ch/01/config/color", osc.Int(3))
	a.send("/ch/01/mix/fader", osc.Float(0.5))
	a.send("/ch/01/mix/pan", osc.Float(0))
	a.send("/ch/01/eq/on", osc.Int(1))

	// Linking copies the odd channel to the even one, except name and pan.
	a.send("/config/chlink/1-2", osc.Int(1))
	same(t, a, "/ch/01/mix/fader", "/ch/02/mix/fader", true)
	same(t, a, "/ch/01/config/color", "/ch/02/config/color", true)
	same(t, a, "/ch/01/eq/on", "/ch/02/eq/on", true)
	same(t, a, "/ch/01/config/name", "/ch/02/config/name", false)
	same(t, a, "/ch/01/mix/pan", "/ch/02/mix/pan", false)

	// A change on either channel moves the other, and every /xremote client
	// hears about the partner, the one that made the change included.
	a.send("/xremote")
	b.send("/xremote")
	drain(a)
	drain(b)
	a.send("/ch/02/mix/fader", osc.Float(0.25))
	if m, ok := a.recv(time.Second); !ok || m.Address != "/ch/01/mix/fader" {
		t.Errorf("origin heard %v, want /ch/01/mix/fader", m)
	}
	got := map[string]bool{}
	for range 2 {
		if m, ok := b.recv(time.Second); ok {
			got[m.Address] = true
		}
	}
	if !got["/ch/01/mix/fader"] || !got["/ch/02/mix/fader"] {
		t.Errorf("other client heard %v", got)
	}
	same(t, a, "/ch/01/mix/fader", "/ch/02/mix/fader", true)

	// Sends follow; insert selection and key sources do not.
	a.send("/ch/01/mix/03/level", osc.Float(0.6))
	same(t, a, "/ch/01/mix/03/level", "/ch/02/mix/03/level", true)
	a.send("/ch/02/insert/sel", osc.Int(3))
	same(t, a, "/ch/01/insert/sel", "/ch/02/insert/sel", false)

	// Link options: with linkcfg/eq off, EQ changes stay on their channel.
	a.send("/config/linkcfg/eq", osc.Int(0))
	a.send("/ch/01/eq/1/g", osc.Float(0.8))
	same(t, a, "/ch/01/eq/1/g", "/ch/02/eq/1/g", false)

	// Turning the option back on brings the pair in step, odd to even.
	a.send("/config/linkcfg/eq", osc.Int(1))
	same(t, a, "/ch/01/eq/1/g", "/ch/02/eq/1/g", true)

	// Unlinked pairs are independent.
	a.send("/config/chlink/1-2", osc.Int(0))
	a.send("/ch/01/mix/fader", osc.Float(0.9))
	same(t, a, "/ch/01/mix/fader", "/ch/02/mix/fader", false)
}

func TestLinkedBusesAndHeadamps(t *testing.T) {
	_, addr := start(t)
	p := dial(t, addr)
	p.send("/config/linkcfg/fdrmute", osc.Int(1))
	p.send("/config/linkcfg/hadly", osc.Int(1))

	p.send("/config/buslink/3-4", osc.Int(1))
	p.send("/bus/04/mix/fader", osc.Float(0.3))
	same(t, p, "/bus/03/mix/fader", "/bus/04/mix/fader", true)

	// Channels 1 and 2 take local inputs 1 and 2 (headamps 0 and 1).
	eventually(t, p, "/-ha/01/index", 1)
	p.send("/config/chlink/1-2", osc.Int(1))
	p.send("/headamp/000/gain", osc.Float(0.75))
	same(t, p, "/headamp/000/gain", "/headamp/001/gain", true)
	p.send("/headamp/001/phantom", osc.Int(1))
	same(t, p, "/headamp/000/phantom", "/headamp/001/phantom", true)
}
