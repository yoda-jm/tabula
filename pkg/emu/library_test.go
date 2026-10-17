package emu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
)

func status(t *testing.T, p *peer, addr string, args ...osc.Arg) int32 {
	t.Helper()
	m := p.ask(addr, args...)
	if m.Address != addr || len(m.Args) != 2 {
		t.Fatalf("%s: reply %v", addr, m)
	}
	return m.Args[1].Int
}

func TestLibrary(t *testing.T) {
	s, addr := start(t)
	if _, err := s.LoadLibrary(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	p := dial(t, addr)
	p.send("/ch/12/config/name", osc.String("Vx Remy"))
	p.send("/ch/12/mix/fader", osc.Float(0.5))

	// Scenes: save, listing, recall.
	if st := status(t, p, "/save", osc.String("scene"), osc.Int(1), osc.String("AAA"), osc.String("aaa")); st != 1 {
		t.Fatalf("save scene: %d", st)
	}
	if got := p.node("-show/showfile/scene/001"); got != `/-show/showfile/scene/001 "AAA" "aaa" %000000000 1` {
		t.Errorf("listing: %q", got)
	}
	p.send("/ch/12/mix/fader", osc.Float(0.9))
	watcher := dial(t, addr)
	watcher.send("/xremote")
	time.Sleep(20 * time.Millisecond)
	if st := status(t, p, "/load", osc.String("scene"), osc.Int(1)); st != 1 {
		t.Fatalf("load scene: %d", st)
	}
	if m := p.ask("/ch/12/mix/fader"); m.Args[0].Float < 0.49 || m.Args[0].Float > 0.51 {
		t.Errorf("fader after recall: %v", m)
	}
	if m, ok := watcher.recv(time.Second); !ok || !strings.HasPrefix(m.Address, "/") {
		t.Errorf("recall not pushed to /xremote clients: %v %v", m, ok)
	}
	if st := status(t, p, "/load", osc.String("scene"), osc.Int(50)); st != 0 {
		t.Errorf("load of an empty scene: %d", st)
	}

	// Channel presets: save ch12 (index 11), load its config only on ch03.
	if st := status(t, p, "/save", osc.String("libchan"), osc.Int(45), osc.String("Vox"), osc.Int(11)); st != 1 {
		t.Fatalf("save libchan: %d", st)
	}
	if got := p.node("-libs/ch/046"); !strings.Contains(got, `"Vox"`) || !strings.HasSuffix(got, " 1") {
		t.Errorf("preset listing: %q", got)
	}
	if st := status(t, p, "/load", osc.String("libchan"), osc.Int(45), osc.Int(2), osc.Int(2)); st != 1 {
		t.Fatalf("load libchan: %d", st)
	}
	if got := p.node("ch/03/config"); !strings.HasPrefix(got, `/ch/03/config "Vx Remy"`) {
		t.Errorf("preset config: %q", got)
	}
	if m := p.ask("/ch/03/mix/fader"); m.Args[0].Float != 0 {
		t.Errorf("preset scope ignored, fader %v", m)
	}

	if st := status(t, p, "/copy", osc.String("libchan"), osc.Int(45), osc.Int(48)); st != 1 {
		t.Errorf("copy: %d", st)
	}
	if st := status(t, p, "/rename", osc.String("libchan"), osc.Int(48), osc.String("Vox 2")); st != 1 {
		t.Errorf("rename: %d", st)
	}
	if st := status(t, p, "/delete", osc.String("libchan"), osc.Int(45)); st != 1 {
		t.Errorf("delete: %d", st)
	}
	if got := p.node("-libs/ch/049"); !strings.Contains(got, `"Vox 2"`) {
		t.Errorf("copied preset: %q", got)
	}

	// /showdump: the show line, then the scene in use.
	p.send("/showdump")
	var lines []string
	for {
		m, ok := p.recv(200 * time.Millisecond)
		if !ok {
			break
		}
		lines = append(lines, strings.TrimSpace(m.Args[0].Str))
	}
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "/-show/showfile/show ") || !strings.HasPrefix(lines[1], "/-show/showfile/scene/001 ") {
		t.Errorf("showdump: %q", lines)
	}

	// The library directory survives a restart.
	dir := s.lib.dir
	s2, _ := start(t)
	if n, err := s2.LoadLibrary(dir); err != nil || n != 2 {
		t.Fatalf("reload: %d items, %v", n, err)
	}
	if v, _, _ := s2.Store().Get("/-libs/ch/049/name"); v.Str != "Vox 2" {
		t.Errorf("reloaded preset name %v", v)
	}
}

func TestRoutingPreset(t *testing.T) {
	_, addr := start(t)
	p := dial(t, addr)
	p.send("/config/routing/IN/1-8", osc.String("A1-8"))
	status(t, p, "/save", osc.String("librout"), osc.Int(0), osc.String("DL32"))
	p.send("/config/routing/IN/1-8", osc.String("AN1-8"))
	if st := status(t, p, "/load", osc.String("librout"), osc.Int(0)); st != 1 {
		t.Fatalf("load librout: %d", st)
	}
	if got := p.node("config/routing/IN"); !strings.HasPrefix(got, "/config/routing/IN A1-8 ") {
		t.Errorf("routing after preset: %q", got)
	}
}

func TestImportM32EditPresets(t *testing.T) {
	s, addr := start(t)
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// M32-Edit's format: header with position, name, type, flags, hasdata.
	write("libchan.003", "#2.0# 3 \"My Vocal\" 0 %0011111100010100 1\n/config \"VOX\" 1 RD\n/preamp +0.0 OFF ON 24 101\n")
	write("libfx.002", "#2.0# 2 \"My Hall\" 1 0 1\ntype HALL\nsource MIX13 MIX13\npar 20 1.57 60 5k74 25 0.0 83 7k2 0.95 25 50 30"+strings.Repeat(" 0", 52)+"\n")
	if n, err := s.ImportM32EditPresets(dir); err != nil || n != 2 {
		t.Fatalf("imported %d, %v", n, err)
	}
	p := dial(t, addr)
	if got := p.node("-libs/ch/003"); !strings.Contains(got, `"My Vocal"`) {
		t.Errorf("listing: %q", got)
	}
	if st := status(t, p, "/load", osc.String("libchan"), osc.Int(2), osc.Int(4)); st != 1 {
		t.Fatalf("load imported channel preset: %d", st)
	}
	if got := p.node("ch/05/preamp"); got != "/ch/05/preamp +0.0 OFF ON 24 101" {
		t.Errorf("ch05 preamp: %q", got)
	}
	if st := status(t, p, "/load", osc.String("libfx"), osc.Int(1), osc.Int(2)); st != 1 {
		t.Fatalf("load imported effect preset: %d", st)
	}
	if got := p.node("fx/3"); got != "/fx/3 HALL" {
		t.Errorf("fx3: %q", got)
	}
	if got := p.node("fx/3/source"); got != "/fx/3/source MIX13 MIX13" {
		t.Errorf("fx3 source: %q", got)
	}
}
