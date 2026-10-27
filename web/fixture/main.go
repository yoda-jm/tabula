// Command fixture writes what the GUI loads from tabula when tabula is
// connected to an emulated M32C holding the demo show (the schema, every
// value, the status), as JSON files in a directory. The page tests of the
// GUI (web/src/pages/pages.test.tsx) render every page from them; run from
// the repository's root:
//
//	go run ./web/fixture DIR
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/yoda-jm/tabula/pkg/emu"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
	"github.com/yoda-jm/tabula/pkg/webapi"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./web/fixture DIR")
		os.Exit(2)
	}
	if err := write(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
}

func write(dir string) error {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := state.New(schema.Build("M32C"))
	f, err := os.Open("testdata/scenes/demo.scn")
	if err != nil {
		return err
	}
	scene.Load(f, st, "demo")
	f.Close()
	console := emu.New(emu.Config{Model: "M32C", Name: "tabula-emu", Firmware: "4.06", Logger: quiet}, st)
	if _, err := console.MountUSB("testdata/usb"); err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go console.Serve(ctx, conn)

	api := webapi.New(quiet)
	tmp, err := os.MkdirTemp("", "tabula-fixture-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	api.SetSessionsDir(filepath.Join(tmp, "sessions"))
	api.SetScenesDir(filepath.Join(tmp, "scenes"))
	srv := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer srv.Close()
	api.Connect(conn.LocalAddr().String())
	defer api.Disconnect()

	get := func(path string) ([]byte, error) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: %s", path, resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		b, err := get("/api/status")
		if err != nil {
			return err
		}
		var s struct{ Connected bool }
		if json.Unmarshal(b, &s) == nil && s.Connected {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the emulator did not sync: %s", b)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, path := range map[string]string{"schema": "/api/schema", "state": "/api/state", "status": "/api/status", "record": "/api/record", "check": "/api/check"} {
		b, err := get(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}
