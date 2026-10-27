package console

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/emu"
	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// startEmu runs tabula-emu's server with the demo scene loaded.
func startEmu(t *testing.T) (*emu.Server, string) {
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
	s := emu.New(emu.Config{Model: "M32C", Name: "emu", Logger: quiet}, st)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.Serve(ctx, conn)
	return s, conn.LocalAddr().String()
}

func TestSyncFollowAndWrite(t *testing.T) {
	srv, addr := startEmu(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	mirror := state.New(schema.Build("M32C"))
	c, err := Dial(ctx, addr, mirror, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if st := c.Status(); st.Info.Model != "M32C" || st.Info.Name != "emu" {
		t.Errorf("info %+v", st.Info)
	}
	if err := c.Sync(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if st := c.Status(); st.Synced != st.Total || len(st.Missing) > 0 {
		t.Fatalf("sync %d/%d, missing %v", st.Synced, st.Total, st.Missing)
	}
	for _, n := range mirror.Schema().Nodes {
		if got, want := mirror.NodeLine(n), srv.Store().NodeLine(srv.Store().Schema().Node(n.Path)); got != want {
			t.Fatalf("mirror differs:\n got %q\nwant %q", got, want)
		}
	}

	// Changes made by someone else reach the mirror through /xremote.
	go c.Run(ctx)
	time.Sleep(50 * time.Millisecond)
	other, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	other.Write(osc.NewMessage("/ch/12/mix/fader", osc.Float(0.5)).Bytes())
	waitFor(t, func() bool {
		v, _, _ := mirror.Get("/ch/12/mix/fader")
		return v.Float > 0.49 && v.Float < 0.51
	})

	// Writes are refused until armed.
	if err := c.SetText("/ch/13/mix/on", "OFF"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("unarmed write: %v", err)
	}
	c.Arm(true)
	if err := c.SetText("/ch/13/mix/on", "OFF"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		v, _, _ := srv.Store().Get("/ch/13/mix/on")
		return v.Int == 0
	})
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("condition not met in time")
}

// A console that disappears is reported lost; when it comes back (here a
// new emulator on the same port, with a changed scene) the mirror is synced
// again.
func TestReconnect(t *testing.T) {
	st := state.New(schema.Build("M32C"))
	first := emu.New(emu.Config{Model: "M32C", Logger: quiet}, st)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().(*net.UDPAddr)
	ctx1, stop1 := context.WithCancel(context.Background())
	go first.Serve(ctx1, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	mirror := state.New(schema.Build("M32C"))
	c, err := Dial(ctx, addr.String(), mirror, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.tick, c.probeAfter = 50*time.Millisecond, 100*time.Millisecond
	if err := c.Sync(ctx, nil); err != nil {
		t.Fatal(err)
	}
	reconnected := make(chan struct{}, 1)
	c.OnReconnect(func() { reconnected <- struct{}{} })
	go c.Run(ctx)

	stop1()
	waitFor(t, func() bool { return !c.Status().Connected })

	st2 := state.New(schema.Build("M32C"))
	st2.ApplyText(`/ch/01/config "Rebooted" 1 GN 1`, "test")
	second := emu.New(emu.Config{Model: "M32C", Logger: quiet}, st2)
	conn2, err := net.ListenUDP("udp4", addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx2, stop2 := context.WithCancel(context.Background())
	defer stop2()
	go second.Serve(ctx2, conn2)

	select {
	case <-reconnected:
	case <-time.After(10 * time.Second):
		t.Fatal("no reconnection")
	}
	if v, _, _ := mirror.Get("/ch/01/config/name"); v.Str != "Rebooted" || !c.Status().Connected {
		t.Errorf("after reconnect: name %q, status %+v", v.Str, c.Status())
	}
}

// TestReadBack loses changes on the way (the console does not push them to
// this client) and finds them again by reading the nodes back.
func TestReadBack(t *testing.T) {
	srv, addr := startEmu(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	mirror := state.New(schema.Build("M32C"))
	c, err := Dial(ctx, addr, mirror, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Sync(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	origins := map[string]string{}
	mirror.Subscribe(func(ch []state.Change) {
		mu.Lock()
		defer mu.Unlock()
		for _, x := range ch {
			origins[x.Leaf.Addr] = x.Origin
		}
	})
	c.readbackEvery = time.Millisecond
	go c.Run(ctx)

	// the console pushes changes to every client but the one that made them
	lost := "udp " + c.conn.LocalAddr().String()
	srv.Store().Set("/ch/05/mix/fader", osc.Float(0.25), lost) // a mix: read back often
	srv.Store().Set("/ch/05/config/name", osc.String("Lost"), lost)
	set, _, _ := srv.Store().Get("/ch/05/mix/fader")
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(10 * time.Millisecond) { // a whole round
		fader := mirror.Schema().Leaf("/ch/05/mix/fader")
		got, want := mirror.LeafLine(fader), srv.Store().LeafLine(fader) // as node text: the console's resolution
		n, _, _ := mirror.Get("/ch/05/config/name")
		if got == want && n.Str == "Lost" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not read back: %q (want %q), %q", got, want, n.Str)
		}
	}
	mu.Lock()
	if origins["/ch/05/mix/fader"] != OriginReadback || origins["/ch/05/config/name"] != OriginReadback {
		t.Errorf("origins: %v", origins)
	}
	mu.Unlock()
	l := c.Status().Link
	if l == nil || l.Caught != 2 || l.Asked == 0 || l.Lost != 0 || l.LastCaught == "" || l.RTT < 0 { // 0 on Windows' coarse clock
		t.Errorf("link: %+v", l)
	}
	// nothing was written to the console: it keeps its values
	if v, _, _ := srv.Store().Get("/ch/05/mix/fader"); !v.Equal(set) {
		t.Errorf("console: %v", v)
	}
}

func TestReadBackOlderThanAWrite(t *testing.T) {
	c := &Client{readback: map[string]time.Time{}, wrote: map[string]time.Time{}}
	t0 := time.Now()
	if o, apply := c.readbackOrigin("/ch/01/mix"); o != OriginConsole || !apply {
		t.Errorf("not read back: %q %v", o, apply)
	}
	c.readback["/ch/01/mix"], c.wrote["/ch/01/mix"] = t0, t0.Add(-time.Second)
	if o, apply := c.readbackOrigin("/ch/01/mix"); o != OriginReadback || !apply {
		t.Errorf("written before: %q %v", o, apply)
	}
	// written after it was asked back: the answer is older than the write
	c.readback["/ch/01/mix"], c.wrote["/ch/01/mix"] = t0, t0.Add(time.Millisecond)
	if _, apply := c.readbackOrigin("/ch/01/mix"); apply || len(c.readback) != 0 {
		t.Errorf("written after: applied")
	}
}
