package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/emu"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

// syncBuffer is a buffer the test reads while tabula writes its log in it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// dirs are the flags keeping a test's files in its own directory.
func dirs(t *testing.T) []string {
	d := t.TempDir()
	return []string{"-sessions", filepath.Join(d, "sessions"), "-scenes", filepath.Join(d, "scenes"), "-access", filepath.Join(d, "access.json"), "-http", "127.0.0.1:0"}
}

// runOnce runs tabula with a command line expected to end by itself.
func runOnce(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out, errOut syncBuffer
	code = run(ctx, args, &out, &errOut, func(net.Addr) { t.Errorf("%v: serves the GUI, expected to end", args) })
	return code, out.String(), errOut.String()
}

// serve runs tabula until the test stops it: stop returns its exit status.
func serve(t *testing.T, args ...string) (url string, log *syncBuffer, stop func() int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	log = &syncBuffer{}
	addr := make(chan net.Addr, 1)
	done := make(chan int, 1)
	go func() { done <- run(ctx, args, io.Discard, log, func(a net.Addr) { addr <- a }) }()
	select {
	case a := <-addr:
		url = "http://" + a.String()
	case code := <-done:
		cancel()
		t.Fatalf("%v: ended (%d) instead of serving:\n%s", args, code, log)
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatalf("%v: not serving:\n%s", args, log)
	}
	stopped := false
	stop = func() int {
		if stopped {
			return 0
		}
		stopped = true
		cancel()
		select {
		case code := <-done:
			return code
		case <-time.After(10 * time.Second):
			t.Fatalf("tabula did not stop:\n%s", log)
			return -1
		}
	}
	t.Cleanup(func() { stop() })
	return url, log, stop
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("%s: %v", url, err)
	}
}

func TestFlags(t *testing.T) {
	for _, c := range []struct {
		args []string
		code int
		want string // in what tabula printed
	}{
		{[]string{"-version"}, 0, "tabula dev"},
		{[]string{"-h"}, 0, "-audio-device"},
		{[]string{"-nope"}, 2, "flag provided but not defined: -nope"},
		{[]string{"connect"}, 2, `unexpected argument "connect"`},
		{[]string{"-guest", "musician"}, 2, "a musician needs an access link"},
		{[]string{"-guest", "nobody"}, 2, "-guest"},
		{[]string{"-audio-bits", "13"}, 2, "13 bits: expected 16, 24 or 32"},
		{[]string{"-http", "127.0.0.1:99999"}, 1, "listen"},
	} {
		code, stdout, stderr := runOnce(t, append(dirs(t), c.args...)...)
		if code != c.code || !strings.Contains(stdout+stderr, c.want) {
			t.Errorf("%v: exit %d, want %d with %q:\n%s%s", c.args, code, c.code, c.want, stdout, stderr)
		}
	}
}

func TestLogFile(t *testing.T) {
	// by default, the log goes to tabula.log in the sessions directory too
	args := dirs(t)
	runOnce(t, append(args, "-guest", "nobody")...)
	b, err := os.ReadFile(filepath.Join(args[1], "tabula.log"))
	if err != nil || !strings.Contains(string(b), "msg=tabula version=dev") || !strings.Contains(string(b), "-guest nobody") {
		t.Errorf("default log: %v\n%s", err, b)
	}

	// a log over 10 MB starts again, the old one kept as .1
	file := filepath.Join(t.TempDir(), "show.log")
	if err := os.WriteFile(file, bytes.Repeat([]byte("x"), logRotateAt+1), 0o644); err != nil {
		t.Fatal(err)
	}
	runOnce(t, append(dirs(t), "-log", file, "-guest", "nobody")...)
	if fi, err := os.Stat(file + ".1"); err != nil || fi.Size() != logRotateAt+1 {
		t.Errorf("rotated log: %v", err)
	}
	if b, _ := os.ReadFile(file); len(b) > 4096 || !strings.Contains(string(b), "msg=tabula") {
		t.Errorf("new log: %d bytes", len(b))
	}

	// a log that cannot be written: the terminal only, and tabula still starts
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, nil, 0o644)
	if code, _, stderr := runOnce(t, append(dirs(t), "-log", filepath.Join(blocker, "tabula.log"), "-guest", "nobody")...); code != 2 ||
		!strings.Contains(stderr, "the log goes to the terminal only") {
		t.Errorf("unwritable log: %d\n%s", code, stderr)
	}
	if code, _, _ := runOnce(t, append(dirs(t), "-log", "none", "-guest", "nobody")...); code != 2 {
		t.Errorf("-log none: %d", code)
	}
	if logPath("none", "s") != "" || logPath("", "s") != filepath.Join("s", "tabula.log") || logPath("x.log", "s") != "x.log" {
		t.Errorf("logPath")
	}
}

func TestPassword(t *testing.T) {
	args := dirs(t)
	url, _, stop := serve(t, args...)
	resp, err := http.Post(url+"/api/access/password", "application/json", strings.NewReader(`{"password":"backstage-2026"}`))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("set password: %v %v", err, resp)
	}
	resp.Body.Close()
	stop()

	// every device the engineer would bypass the password
	if code, _, stderr := runOnce(t, append(args, "-guest", "engineer")...); code != 2 || !strings.Contains(stderr, "an engineer password is set") {
		t.Errorf("-guest engineer with a password: %d\n%s", code, stderr)
	}
	if code, stdout, _ := runOnce(t, append(args, "-reset-password")...); code != 0 || !strings.Contains(stdout, "engineer password removed from") {
		t.Errorf("-reset-password: %d %s", code, stdout)
	}
	if code, stdout, _ := runOnce(t, append(args, "-reset-password")...); code != 0 || !strings.Contains(stdout, "no engineer password in") {
		t.Errorf("-reset-password again: %d %s", code, stdout)
	}
}

// startEmu runs an emulated M32C holding the demo show; it returns its address.
func startEmu(t *testing.T) string {
	t.Helper()
	st := state.New(schema.Build("M32C"))
	f, err := os.Open("../../testdata/scenes/demo.scn")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scene.Load(f, st, "test")
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := emu.New(emu.Config{Model: "M32C", Logger: quiet}, st)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.Serve(ctx, conn)
	return conn.LocalAddr().String()
}

// TestServe starts tabula as on a stage laptop: -console and -record, and
// stops it as Ctrl-C does: the recording ends cleanly.
func TestServe(t *testing.T) {
	args := dirs(t)
	logFile := filepath.Join(t.TempDir(), "show.log")
	url, log, stop := serve(t, append(args, "-console", startEmu(t), "-record", "-log", logFile)...)

	var st struct {
		Connected bool   `json:"connected"`
		Version   string `json:"version"`
	}
	var rec struct {
		Recording bool   `json:"recording"`
		Name      string `json:"name"`
	}
	for deadline := time.Now().Add(15 * time.Second); !rec.Recording; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("not recording: %+v %+v\n%s", st, rec, log)
		}
		getJSON(t, url+"/api/status", &st)
		getJSON(t, url+"/api/record", &rec)
	}
	if !st.Connected || !strings.HasPrefix(st.Version, "dev") {
		t.Errorf("status: %+v", st)
	}
	var about struct{ Log, Sessions string }
	getJSON(t, url+"/api/about", &about)
	if about.Log != logFile || !filepath.IsAbs(about.Sessions) {
		t.Errorf("about: %+v", about)
	}

	if code := stop(); code != 0 {
		t.Errorf("exit %d", code)
	}
	// the recording ended with tabula: its session is complete
	b, err := os.ReadFile(filepath.Join(args[1], rec.Name, "meta.json"))
	var meta struct {
		End time.Time `json:"end"`
	}
	if err != nil || json.Unmarshal(b, &meta) != nil || meta.End.IsZero() {
		t.Errorf("session %s after the stop: %v\n%s", rec.Name, err, b)
	}
	if b, _ := os.ReadFile(logFile); !strings.Contains(string(b), "msg=connected") || !strings.Contains(string(b), "msg=\"tabula stopped\"") {
		t.Errorf("log file:\n%s", b)
	}
}
