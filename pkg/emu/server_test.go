package emu

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"math"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

func start(t *testing.T) (*Server, *net.UDPAddr) {
	t.Helper()
	st := state.New(schema.Build("M32C"))
	s := New(Config{Model: "M32C", Name: "test-desk", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, st)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.Serve(ctx, conn)
	return s, conn.LocalAddr().(*net.UDPAddr)
}

type peer struct {
	t    *testing.T
	conn *net.UDPConn
}

func dial(t *testing.T, addr *net.UDPAddr) *peer {
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &peer{t, conn}
}

func (p *peer) send(addr string, args ...osc.Arg) {
	p.t.Helper()
	if _, err := p.conn.Write(osc.NewMessage(addr, args...).Bytes()); err != nil {
		p.t.Fatal(err)
	}
}

// recv returns the next message, or ok=false after the timeout.
func (p *peer) recv(timeout time.Duration) (osc.Message, bool) {
	p.t.Helper()
	buf := make([]byte, 65536)
	p.conn.SetReadDeadline(time.Now().Add(timeout))
	n, err := p.conn.Read(buf)
	if err != nil {
		return osc.Message{}, false
	}
	m, err := osc.Parse(buf[:n])
	if err != nil {
		p.t.Fatal(err)
	}
	return m, true
}

func (p *peer) ask(addr string, args ...osc.Arg) osc.Message {
	p.t.Helper()
	p.send(addr, args...)
	m, ok := p.recv(time.Second)
	if !ok {
		p.t.Fatalf("no reply to %s", addr)
	}
	return m
}

func (p *peer) node(path string) string {
	p.t.Helper()
	m := p.ask("/node", osc.String(path))
	if m.Address != "node" || len(m.Args) != 1 || !strings.HasSuffix(m.Args[0].Str, "\n") {
		p.t.Fatalf("bad /node reply %v", m)
	}
	return strings.TrimSuffix(m.Args[0].Str, "\n")
}

func TestInfo(t *testing.T) {
	_, addr := start(t)
	p := dial(t, addr)
	if m := p.ask("/info"); m.String() != `/info ,ssss "V2.07" "osc-server" "M32C" "4.06"` {
		t.Errorf("/info: %v", m)
	}
	if m := p.ask("/xinfo"); len(m.Args) != 4 || m.Args[0].Str != "127.0.0.1" || m.Args[1].Str != "test-desk" {
		t.Errorf("/xinfo: %v", m)
	}
	if m := p.ask("/status"); len(m.Args) != 3 || m.Args[0].Str != "active" {
		t.Errorf("/status: %v", m)
	}
}

func TestGetSetNode(t *testing.T) {
	_, addr := start(t)
	p := dial(t, addr)

	p.send("/ch/01/mix/fader", osc.Float(0.75))
	p.send("/ch/01/config/name", osc.String("Hang"))
	p.send("/ch/01/gate/mode", osc.String("DUCK"))
	// 0.75 is not one of the 1024 fader values: the console keeps 767/1023
	// (0x3f3feffc in the protocol document's fader table).
	if m := p.ask("/ch/01/mix/fader"); len(m.Args) != 1 || math.Float32bits(m.Args[0].Float) != 0x3f3feffc {
		t.Errorf("fader: %v", m)
	}
	if m := p.ask("/ch/01/gate/mode"); len(m.Args) != 1 || m.Args[0].Int != 4 {
		t.Errorf("gate mode: %v", m)
	}
	if got := p.node("ch/01/config"); got != `/ch/01/config "Hang" 1 OFF 1` {
		t.Errorf("node: %q", got)
	}
	if got := p.node("ch/01"); !strings.HasPrefix(got, "/ch/01/config ") {
		t.Errorf("node prefix: %q", got)
	}
	if got := p.node("headamp/124/phantom"); got != "/headamp/124/phantom OFF" {
		t.Errorf("leaf node: %q", got)
	}

	// Multiple type tags on a node address.
	p.send("/ch/01/eq/1", osc.Int(2), osc.Float(0.2650), osc.Float(0.5), osc.Float(0.4648))
	if got := p.node("ch/01/eq/1"); got != "/ch/01/eq/1 PEQ 124.7 +0.00 2.0" {
		t.Errorf("eq node: %q", got)
	}

	// "/" sets from text and is echoed back.
	if m := p.ask("/", osc.String("ch/02/mix ON -10")); m.Address != "/" || m.Args[0].Str != "ch/02/mix ON -10" {
		t.Errorf("echo: %v", m)
	}
	if got := p.node("ch/02/mix"); got != "/ch/02/mix ON -10.0 OFF +0 OFF   -oo" {
		t.Errorf("after /: %q", got)
	}
}

func TestXremoteFanOut(t *testing.T) {
	_, addr := start(t)
	a, b := dial(t, addr), dial(t, addr)
	a.send("/xremote")
	b.send("/xremote")
	time.Sleep(20 * time.Millisecond)

	a.send("/ch/05/mix/on", osc.Int(1))
	if m, ok := b.recv(time.Second); !ok || m.String() != "/ch/05/mix/on ,i 1" {
		t.Fatalf("b got %v %v", m, ok)
	}
	if m, ok := a.recv(100 * time.Millisecond); ok {
		t.Errorf("origin got its own change back: %v", m)
	}
	// Same value again: no change, nothing pushed.
	a.send("/ch/05/mix/on", osc.Int(1))
	if m, ok := b.recv(100 * time.Millisecond); ok {
		t.Errorf("unchanged value pushed: %v", m)
	}
}

// X32-Edit and M32-Edit query console nodes when they connect.
func TestConsoleNodes(t *testing.T) {
	_, addr := start(t)
	p := dial(t, addr)
	if got := p.node("-prefs/card"); got != "/-prefs/card USB 32/32 32/32 IN WC 64 1-32 1-32 BOTH 32Ch USB AUTO SD1" {
		t.Errorf("node: %q", got)
	}
	if m := p.ask("/-stat/selidx"); m.String() != "/-stat/selidx ,i 0" {
		t.Errorf("selidx: %v", m)
	}
}

func TestUnhandledIsRecorded(t *testing.T) {
	s, addr := start(t)
	p := dial(t, addr)
	p.send("/ch/01/nosuch")
	p.send("/ch/01/nosuch")
	p.send("/meters", osc.String("/meters/1"))
	p.send("/ch/01/mix/fader", osc.String("loud"))
	p.ask("/info") // wait for the previous packets to be handled

	got := map[string]Unhandled{}
	for _, u := range s.Unhandled.List() {
		got[u.Address] = u
	}
	if u := got["/ch/01/nosuch"]; u.Count != 2 || u.Reason != ReasonUnknown {
		t.Errorf("/ch/01/nosuch: %+v", u)
	}
	if u := got["/meters"]; u.Reason != ReasonUnimplemented || u.LastArgs != `"/meters/1"` {
		t.Errorf("/meters: %+v", u)
	}
	if u := got["/ch/01/mix/fader"]; u.Reason != ReasonRejected || u.Detail == "" {
		t.Errorf("rejected fader: %+v", u)
	}
}

// A scene loaded into the emulator and dumped back with /node over the
// network, as Maillot's X32GetScene does, must give the original file.
func TestSceneDumpOverNetwork(t *testing.T) {
	s, addr := start(t)
	want, err := os.ReadFile("../../testdata/scenes/GVO-2026.scn")
	if err != nil {
		t.Fatal(err)
	}
	header, errs := scene.Load(bytes.NewReader(want), s.Store(), "test")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	p := dial(t, addr)
	var got strings.Builder
	got.WriteString(header + "\n")
	for _, line := range strings.Split(string(want), "\n") {
		if !strings.HasPrefix(line, "/") {
			continue
		}
		path, _ := schema.SplitLine(line)
		got.WriteString(p.node(strings.TrimPrefix(path, "/")) + "\n")
	}
	if got.String() != string(want) {
		g, w := strings.Split(got.String(), "\n"), strings.Split(string(want), "\n")
		for i := range min(len(g), len(w)) {
			if g[i] != w[i] {
				t.Fatalf("line %d:\n got %q\nwant %q", i+1, g[i], w[i])
			}
		}
		t.Fatalf("%d lines, want %d", len(g), len(w))
	}
}
