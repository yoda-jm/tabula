package emu

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
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
	// Renaming the console (a preference) changes what /xinfo reports.
	p.send("/-prefs/name", osc.String("FOH"))
	if m := p.ask("/xinfo"); m.Args[1].Str != "FOH" {
		t.Errorf("/xinfo after rename: %v", m)
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
	p.send("/ch/01/mix/fader", osc.String("loud"))
	p.ask("/info") // wait for the previous packets to be handled

	got := map[string]Unhandled{}
	for _, u := range s.Unhandled.List() {
		got[u.Address] = u
	}
	if u := got["/ch/01/nosuch"]; u.Count != 2 || u.Reason != ReasonUnknown {
		t.Errorf("/ch/01/nosuch: %+v", u)
	}
	if u := got["/ch/01/mix/fader"]; u.Reason != ReasonRejected || u.Detail == "" {
		t.Errorf("rejected fader: %+v", u)
	}
}

// A scene loaded into the emulator and dumped back with /node over the
// network, as Maillot's X32GetScene does, must give the original file.
func TestSceneDumpOverNetwork(t *testing.T) {
	s, addr := start(t)
	want, err := os.ReadFile("../../testdata/scenes/demo.scn")
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

func TestMetersAndSubscriptions(t *testing.T) {
	_, addr := start(t)
	p := dial(t, addr)
	p.send("/ch/17/mix/on", osc.Int(1))

	// /meters ,si /meters/6 16: four strip meters of channel 17.
	m := p.ask("/meters", osc.String("/meters/6"), osc.Int(16))
	if m.Address != "/meters/6" || len(m.Args) != 1 || m.Args[0].Tag != 'b' {
		t.Fatalf("meters: %v", m)
	}
	blob := m.Args[0].Blob
	if len(blob) != 20 || binary.LittleEndian.Uint32(blob) != 4 {
		t.Fatalf("meters blob % x", blob)
	}
	if in := math.Float32frombits(binary.LittleEndian.Uint32(blob[4:])); in <= 0 || in > 1 {
		t.Errorf("channel 17 input level %v", in)
	}
	p.send("/unsubscribe")
	drain(p)

	// The document's /formatsubscribe example: mutes of channels 06 to 09.
	for ch := 6; ch <= 9; ch++ {
		p.send(fmt.Sprintf("/ch/%02d/mix/on", ch), osc.Int(1))
	}
	m = p.ask("/formatsubscribe", osc.String("/testme"), osc.String("/ch/**/mix/on"), osc.Int(6), osc.Int(9), osc.Int(80))
	if got := hex.EncodeToString(m.Bytes()); got != "2f746573746d65002c620000000000141400000001000000010000000100000001000000" {
		t.Errorf("formatsubscribe: %s", got)
	}
	p.send("/unsubscribe", osc.String("/testme"))
	drain(p)

	m = p.ask("/batchsubscribe", osc.String("/yy"), osc.String("/meters/6"), osc.Int(0), osc.Int(0), osc.Int(40))
	if m.Address != "/yy" || len(m.Args[0].Blob) != 20 {
		t.Errorf("batchsubscribe: %v", m)
	}
	p.send("/unsubscribe")
	drain(p)

	// /subscribe with time factor 1: an update every 50 ms.
	p.send("/subscribe", osc.String("/ch/01/mix/on"), osc.Int(1))
	n := 0
	for deadline := time.Now().Add(300 * time.Millisecond); time.Now().Before(deadline); {
		if m, ok := p.recv(100 * time.Millisecond); ok && m.Address == "/ch/01/mix/on" {
			n++
		}
	}
	if n < 3 {
		t.Errorf("%d updates in 300 ms", n)
	}
}

func drain(p *peer) {
	for {
		if _, ok := p.recv(80 * time.Millisecond); !ok {
			return
		}
	}
}

// M32-Edit writes a scene to the console ("PC -> Mixer") as one "/" command
// per node line, each keeping the line feed of /node replies.
func TestSceneWrittenLikeM32Edit(t *testing.T) {
	s, addr := start(t)
	want, err := os.ReadFile("../../testdata/scenes/demo.scn")
	if err != nil {
		t.Fatal(err)
	}
	p := dial(t, addr)
	for _, line := range strings.Split(string(want), "\n") {
		if !strings.HasPrefix(line, "/") {
			continue
		}
		if m := p.ask("/", osc.String(strings.TrimPrefix(line, "/")+"\n")); m.Address != "/" {
			t.Fatalf("no echo for %q: %v", line, m)
		}
	}
	if n := len(s.Unhandled.List()); n > 0 {
		t.Fatalf("%d kinds of rejected lines, first %+v", n, s.Unhandled.List()[0])
	}
	var got strings.Builder
	got.WriteString(strings.SplitN(string(want), "\n", 2)[0] + "\n")
	scene.Write(&got, s.Store(), "")
	g := strings.SplitN(got.String(), "\n", 3)
	w := strings.SplitN(string(want), "\n", 2)
	if g[0]+"\n"+g[2] != w[0]+"\n"+w[1] {
		t.Error("scene written through \"/\" commands differs from the file")
	}
}

// offline returns an emulator that does not serve: tests call its handlers.
func offline() *Server {
	return New(Config{Model: "M32C", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, state.New(schema.Build("M32C")))
}

// /formatsubscribe ranges are bounded and checked number by number: a range
// ending at 2147483647 never ended, and a wide one took hundreds of MB.
func TestFormatSubscribeRange(t *testing.T) {
	s, c := offline(), &Client{Addr: "test"}
	req := func(i0, i1 int32, cmds ...string) error {
		args := []osc.Arg{osc.String("/fs")}
		for _, cmd := range cmds {
			args = append(args, osc.String(cmd))
		}
		args = append(args, osc.Int(i0), osc.Int(i1), osc.Int(80))
		return s.formatSubscribe(osc.NewMessage("/formatsubscribe", args...), c)
	}
	if err := req(6, 9, "/ch/**/mix/on"); err != nil {
		t.Fatal(err)
	}
	for _, r := range [][2]int32{{9, 6}, {1, 5000000}, {math.MaxInt32 - 1, math.MaxInt32}, {math.MinInt32, math.MaxInt32}, {30, 40}} {
		if err := req(r[0], r[1], "/ch/**/mix/on"); err == nil {
			t.Errorf("range %d to %d accepted", r[0], r[1])
		}
	}
	// Ten times the 128 headamps: more parameters than a reply can carry.
	cmds := make([]string, 10)
	for i := range cmds {
		cmds[i] = "/headamp/***/gain"
	}
	if err := req(0, 127, cmds...); err == nil {
		t.Error("1280 parameters accepted")
	}
}

// /renew and /unsubscribe name a meter subscription by its address, with or
// without the slash, as in the document's "/renew ,s meters/5".
func TestRenewMetersByAddress(t *testing.T) {
	s, c := offline(), &Client{Addr: "test"}
	s.meters(osc.NewMessage("/meters", osc.String("/meters/5"), osc.Int(3), osc.Int(1)), c)
	s.meters(osc.NewMessage("/meters", osc.String("/meters/6"), osc.Int(16)), c)
	if n := s.subs.renew(c, "meters/5", time.Now()); n != 1 {
		t.Errorf("/renew meters/5 renewed %d subscriptions", n)
	}
	s.subs.remove(c, "/meters/6")
	if v := s.Subscriptions(); len(v) != 1 || v[0].Name != "/meters/5 3 1" {
		t.Errorf("after /unsubscribe /meters/6: %+v", v)
	}
}

// A client holds a bounded number of subscriptions; a request renewing one
// it holds is still accepted.
func TestSubscriptionLimit(t *testing.T) {
	s, c := offline(), &Client{Addr: "test"}
	sub := func(c *Client, addr string) error {
		return s.subscribe(osc.NewMessage("/subscribe", osc.String(addr)), c)
	}
	for i := range maxSubs {
		if err := sub(c, fmt.Sprintf("/headamp/%03d/gain", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := sub(c, "/headamp/000/gain"); err != nil {
		t.Errorf("renewing a subscription: %v", err)
	}
	if err := sub(c, "/ch/01/mix/on"); err == nil {
		t.Error("subscription past the limit accepted")
	}
	if err := sub(&Client{Addr: "other"}, "/ch/01/mix/on"); err != nil {
		t.Errorf("another client: %v", err)
	}
}

// The meter subscriptions served at one tick share one computation of the
// levels, the costly part: serving many costs about as much as one.
func TestMetersShareLevels(t *testing.T) {
	s, c := offline(), &Client{Addr: "test"}
	for id := 0; id <= 16; id++ {
		if err := s.meters(osc.NewMessage("/meters", osc.String(fmt.Sprintf("/meters/%d", id))), c); err != nil {
			t.Fatal(err)
		}
	}
	one := testing.AllocsPerRun(5, func() { s.levels(time.Now()) })
	all := testing.AllocsPerRun(5, func() {
		tk := &tick{s: s, now: time.Now()}
		for _, sub := range s.subs.list {
			sub.produce(tk)
		}
	})
	t.Logf("%.0f allocations for the levels, %.0f for %d meters", one, all, len(s.subs.list))
	if all > 2*one {
		t.Errorf("%.0f allocations for %d meters, %.0f for the levels alone", all, len(s.subs.list), one)
	}
}

// The sends of FX returns reach the buses: a bus fed by an FX return only
// shows a level.
func TestFxReturnFeedsBus(t *testing.T) {
	s := offline()
	apply(t, s,
		"/ch/01/mix ON 0.0 OFF +0 OFF -oo",
		"/ch/01/mix/01 ON 0.0 +0 PRE 0",
		"/bus/01/mix ON 0.0 OFF +0 OFF -oo",
		"/fx/1/source MIX1 MIX1",
		"/fxrtn/01/mix ON 0.0 OFF +0 OFF -oo",
		"/fxrtn/01/mix/03 ON 0.0 +0 POST 0",
		"/bus/03/mix ON 0.0 OFF +0 OFF -oo",
	)
	lv := s.levels(time.Now())
	if lv.bus[0] <= 0 || lv.in[40] <= 0 {
		t.Fatalf("bus 1 %v, FX return 1 %v: no signal", lv.bus[0], lv.in[40])
	}
	if lv.bus[2] <= 0 {
		t.Errorf("bus 3, fed by FX return 1: level %v", lv.bus[2])
	}
}

// Node text is one line: a line feed inside "/" text is refused, and text
// kept verbatim for an effect gives way once one of its parameters is set.
func TestNodeTextIsOneLine(t *testing.T) {
	s, addr := start(t)
	p := dial(t, addr)
	if m := p.ask("/", osc.String("fx/1/par 50 bogus\n/ch/01/mix OFF")); m.Address != "/" {
		t.Fatalf("no echo: %v", m)
	}
	if !unhandled(s, "/", ReasonRejected) {
		t.Error("text with a line feed not rejected")
	}
	if got := p.node("fx/1/par"); strings.Contains(got, "bogus") {
		t.Errorf("text with a line feed kept: %q", got)
	}
	p.ask("/", osc.String("fx/1/par 50 bogus"))
	if got := p.node("fx/1/par"); got != "/fx/1/par 50 bogus" {
		t.Fatalf("text kept verbatim: %q", got)
	}
	p.send("/fx/1/par/01", osc.Float(0.9))
	if got := p.node("fx/1/par"); strings.Contains(got, "bogus") {
		t.Errorf("after /fx/1/par/01: %q", got)
	}
}

// What the emulator keeps of the requests it sees is bounded: the text of a
// packet, and the kinds of unhandled requests.
func TestJournalIsBounded(t *testing.T) {
	s := offline()
	args := make([]osc.Arg, 4000)
	for i := range args {
		args[i] = osc.Int(int32(i))
	}
	m := osc.NewMessage("/ch/01/nosuch", args...)
	s.traffic.add(Packet{Text: m.String()})
	s.Unhandled.Record(m, "test", ReasonUnknown, "")
	if n := len(s.Traffic(1)[0].Text); n > maxText {
		t.Errorf("traffic entry of %d bytes", n)
	}
	u := s.Unhandled.List()[0]
	if len(u.LastArgs) > maxText || len(u.Tags) > maxText {
		t.Errorf("unhandled entry with %d bytes of arguments, %d of tags", len(u.LastArgs), len(u.Tags))
	}
	for i := range maxKinds + 10 {
		s.Unhandled.Record(osc.NewMessage(fmt.Sprintf("/nosuch/%d", i)), "test", ReasonUnknown, "")
	}
	list := s.Unhandled.List()
	if len(list) != maxKinds+1 {
		t.Fatalf("%d kinds, want %d", len(list), maxKinds+1)
	}
	if list[0].Address != "<other>" || list[0].Count != 11 {
		t.Errorf("most frequent: %+v", list[0])
	}
}

// Clients are forgotten after a minute without requests, unless they still
// hold a subscription or an /xremote lease.
func TestIdleClientsForgotten(t *testing.T) {
	s := offline()
	addr := func(port int) *net.UDPAddr { return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port} }
	idle, sub, remote := s.client(addr(1)), s.client(addr(2)), s.client(addr(3))
	s.subscribe(osc.NewMessage("/subscribe", osc.String("/ch/01/mix/on")), sub)
	s.mu.Lock()
	for _, c := range []*Client{idle, sub, remote} {
		c.LastSeen = c.LastSeen.Add(-2 * clientIdle)
	}
	remote.XremoteUntil = time.Now().Add(xremoteLease)
	s.swept = time.Time{}
	s.mu.Unlock()
	s.client(addr(4))
	var got []string
	for _, c := range s.Clients() {
		got = append(got, c.Addr)
	}
	if want := "127.0.0.1:4 127.0.0.1:3 127.0.0.1:2"; strings.Join(got, " ") != want {
		t.Errorf("clients %v, want %s", got, want)
	}
}
