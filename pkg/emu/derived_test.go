package emu

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/scene"
)

func eventually(t *testing.T, p *peer, addr string, want int32) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if m := p.ask(addr); len(m.Args) == 1 && m.Args[0].Int == want {
			return
		}
	}
	t.Errorf("%s never became %d (last %v)", addr, want, p.ask(addr))
}

func TestDerivedValues(t *testing.T) {
	s, addr := start(t)
	data, _ := os.ReadFile("../../testdata/scenes/demo.scn")
	scene.Load(bytes.NewReader(data), s.Store(), "test")
	p := dial(t, addr)

	// The demo scene: channel 12 takes AES50-A input 12 (routing block A9-16).
	eventually(t, p, "/-ha/11/index", 43)
	p.send("/config/routing/IN/9-16", osc.String("AN9-16"))
	eventually(t, p, "/-ha/11/index", 11)

	p.send("/ch/05/insert/sel", osc.String("FX3L"))
	eventually(t, p, "/-insert/fx3L", 4)

	p.send("/-stat/solosw/05", osc.Int(1))
	eventually(t, p, "/-stat/solo", 1)
	p.send("/-stat/solosw/49", osc.Int(1)) // not exclusive: both
	eventually(t, p, "/-stat/solosw/49", 1)
	if got := p.ask("/-stat/solosw/05").Args[0].Int; got != 1 {
		t.Errorf("solo 05 turned off without exclusive solo")
	}
	p.send("/-action/clearsolo", osc.Int(1))
	eventually(t, p, "/-stat/solosw/05", 0)
	eventually(t, p, "/-stat/solo", 0)

	p.send("/config/solo/exclusive", osc.Int(1)) // one at a time
	p.send("/-stat/solosw/05", osc.Int(1))
	eventually(t, p, "/-stat/solo", 1)
	p.send("/-stat/solosw/49", osc.Int(1))
	eventually(t, p, "/-stat/solosw/05", 0)
	eventually(t, p, "/-stat/solosw/49", 1)
	eventually(t, p, "/-stat/solo", 1)
}

func TestActions(t *testing.T) {
	_, addr := start(t)
	p := dial(t, addr)
	p.send("/ch/01/mix/fader", osc.Float(0.5))
	status(t, p, "/save", osc.String("scene"), osc.Int(3), osc.String("Verse"), osc.String(""))
	p.send("/ch/01/mix/fader", osc.Float(1))
	p.send("/-action/goscene", osc.Int(3))
	if m := p.ask("/ch/01/mix/fader"); m.Args[0].Float > 0.51 {
		t.Errorf("goscene: fader %v", m)
	}

	p.send("/-action/undopt", osc.Int(1))
	p.send("/ch/01/mix/fader", osc.Float(0.1))
	p.send("/-action/doundo", osc.Int(1))
	if m := p.ask("/ch/01/mix/fader"); m.Args[0].Float < 0.49 || m.Args[0].Float > 0.51 {
		t.Errorf("undo: fader %v", m)
	}
	if m := p.ask("/-undo/time"); m.Args[0].Str == "" {
		t.Errorf("undo time not set")
	}
}
