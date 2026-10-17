package console

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/emu"
	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// startEmu runs tabula-emu's server with GVO-2026 loaded.
func startEmu(t *testing.T) (*emu.Server, string) {
	t.Helper()
	st := state.New(schema.Build("M32C"))
	f, err := os.Open("../../testdata/scenes/GVO-2026.scn")
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
