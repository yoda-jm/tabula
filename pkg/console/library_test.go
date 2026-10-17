package console

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

func TestLibrary(t *testing.T) {
	srv, addr := startEmu(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	go c.Run(ctx)
	time.Sleep(50 * time.Millisecond) // /xremote registered
	listing := func(n *schema.Node) bool { return strings.HasPrefix(n.Path, "/-show/showfile/scene/") }

	other, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	other.Write(osc.NewMessage("/ch/12/mix/fader", osc.Float(0.5)).Bytes())
	waitFor(t, func() bool { v, _, _ := mirror.Get("/ch/12/mix/fader"); return v.Float > 0.49 && v.Float < 0.51 })

	save := []osc.Arg{osc.String("scene"), osc.Int(3), osc.String("Concert"), osc.String("set 1")}
	if err := c.Library(ctx, "/save", save...); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("unarmed save: %v", err)
	}
	c.Arm(true)
	if err := c.Library(ctx, "/save", save...); err != nil {
		t.Fatal(err)
	}
	if err := c.Refresh(ctx, listing); err != nil {
		t.Fatal(err)
	}
	if got := mirror.NodeLine(mirror.Schema().Node("/-show/showfile/scene/003")); got != `/-show/showfile/scene/003 "Concert" "set 1" %000000000 1` {
		t.Errorf("listing after save: %q", got)
	}

	// Recall: the emulator applies the scene; Refresh brings the mirror along.
	other.Write(osc.NewMessage("/ch/12/mix/fader", osc.Float(0.1)).Bytes())
	waitFor(t, func() bool { v, _, _ := mirror.Get("/ch/12/mix/fader"); return v.Float < 0.11 })
	want, _, _ := srv.Store().Get("/ch/01/mix/fader")
	if err := c.Library(ctx, "/load", osc.String("scene"), osc.Int(3)); err != nil {
		t.Fatal(err)
	}
	if err := c.Refresh(ctx, func(n *schema.Node) bool { return n.InScene }); err != nil {
		t.Fatal(err)
	}
	got, _, _ := mirror.Get("/ch/12/mix/fader")
	if emuV, _, _ := srv.Store().Get("/ch/12/mix/fader"); got.Float != emuV.Float || got.Float < 0.49 || got.Float > 0.51 {
		t.Errorf("fader after recall: mirror %v, emulator %v", got, emuV)
	}
	if v, _, _ := mirror.Get("/ch/01/mix/fader"); v.Float != want.Float {
		t.Errorf("ch01 fader %v, want %v", v, want)
	}

	// Refused commands and their status.
	if err := c.Library(ctx, "/load", osc.String("scene"), osc.Int(50)); !errors.Is(err, ErrRefused) {
		t.Errorf("load of an empty scene: %v", err)
	}
	if err := c.Library(ctx, "/copy", osc.String("snippet"), osc.Int(0), osc.Int(1)); !errors.Is(err, ErrRefused) {
		t.Errorf("copy of a snippet: %v", err)
	}
	if err := c.Library(ctx, "/node", osc.String("scene"), osc.Int(0)); err == nil {
		t.Error("/node accepted as a library command")
	}

	// Rename, copy, delete.
	for _, step := range []struct {
		cmd  string
		args []osc.Arg
	}{
		{"/rename", []osc.Arg{osc.String("scene"), osc.Int(3), osc.String("Encore")}},
		{"/copy", []osc.Arg{osc.String("scene"), osc.Int(3), osc.Int(4)}},
		{"/delete", []osc.Arg{osc.String("scene"), osc.Int(3)}},
	} {
		if err := c.Library(ctx, step.cmd, step.args...); err != nil {
			t.Fatalf("%s: %v", step.cmd, err)
		}
	}
	if err := c.Refresh(ctx, listing); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := mirror.Get("/-show/showfile/scene/003/hasdata"); v.Int != 0 {
		t.Errorf("scene 3 still there after delete")
	}
	if v, _, _ := mirror.Get("/-show/showfile/scene/004/name"); v.Str != "Encore" {
		t.Errorf("copied scene name %q", v.Str)
	}
}

// A console that does not answer: Library gives up after LibraryWait,
// without sending the command again.
func TestLibraryTimeout(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	got := make(chan string, 8)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			m, err := osc.Parse(buf[:n])
			if err != nil {
				continue
			}
			got <- m.Address
			switch m.Address { // enough to connect
			case "/info":
				conn.WriteToUDP(osc.NewMessage("/info", osc.String("V2.07"), osc.String("osc-server"), osc.String("M32C"), osc.String("4.06")).Bytes(), from)
			case "/xinfo":
				conn.WriteToUDP(osc.NewMessage("/xinfo", osc.String("127.0.0.1"), osc.String("mute"), osc.String("M32C"), osc.String("4.06")).Bytes(), from)
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Dial(ctx, conn.LocalAddr().String(), state.New(schema.Build("M32C")), quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Arm(true)
	old := LibraryWait
	LibraryWait = 200 * time.Millisecond
	defer func() { LibraryWait = old }()
	if err := c.Library(ctx, "/delete", osc.String("scene"), osc.Int(1)); !errors.Is(err, ErrTimeout) {
		t.Errorf("no answer: %v", err)
	}
	deletes := 0
	for len(got) > 0 {
		if <-got == "/delete" {
			deletes++
		}
	}
	if deletes != 1 {
		t.Errorf("/delete sent %d times", deletes)
	}
}
