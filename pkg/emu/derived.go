package emu

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/state"
)

// Values the console computes itself (/-stat/solo, /-insert, /-ha/NN/index)
// are refreshed in the background after the changes they depend on; store
// subscribers cannot change the store themselves.

const originConsole = "console"

type derived struct {
	mu                 sync.Mutex
	solo, inserts, has bool
	kick               chan struct{}
}

func (s *Server) watchDerived(changes []state.Change) {
	d := &s.derived
	d.mu.Lock()
	for _, c := range changes {
		a := c.Leaf.Addr
		switch {
		case c.Origin == originConsole:
		case strings.HasPrefix(a, "/-stat/solosw/"):
			d.solo = true
		case strings.HasSuffix(a, "/insert/sel"):
			d.inserts = true
		case strings.HasSuffix(a, "/config/source") || strings.HasPrefix(a, "/config/routing"):
			d.has = true
		}
	}
	d.mu.Unlock()
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

func (s *Server) runDerived(ctx context.Context) {
	for {
		s.derived.mu.Lock()
		solo, ins, has := s.derived.solo, s.derived.inserts, s.derived.has
		s.derived.solo, s.derived.inserts, s.derived.has = false, false, false
		s.derived.mu.Unlock()
		if solo {
			s.deriveSolo()
		}
		if ins {
			s.deriveInserts()
		}
		if has {
			s.deriveHeadamps()
		}
		select {
		case <-ctx.Done():
			return
		case <-s.derived.kick:
		}
	}
}

func (s *Server) intAt(addr string) int32 {
	v, _, _ := s.store.Get(addr)
	return v.Int
}

func (s *Server) setInt(addr string, v int32) { s.store.Set(addr, osc.Int(v), originConsole) }

// deriveSolo: /-stat/solo is ON while any solo switch is.
func (s *Server) deriveSolo() {
	on := int32(0)
	for i := 1; i <= 80; i++ {
		if s.intAt(fmt.Sprintf("/-stat/solosw/%02d", i)) == 1 {
			on = 1
			break
		}
	}
	s.setInt("/-stat/solo", on)
}

// deriveInserts: /-insert lists, for each FX side and aux insert point, the
// strip index it is inserted into, -1 for none.
func (s *Server) deriveInserts() {
	where := map[int32]int32{} // insert/sel index -> strip index
	for i := 71; i >= 0; i-- {
		p, _ := stripPath(i)
		if s.store.Schema().Leaf(p+"/insert/sel") == nil {
			continue
		}
		if sel := s.intAt(p + "/insert/sel"); sel > 0 {
			where[sel] = int32(i)
		}
	}
	names := s.store.Schema().Node("/-insert").Fields
	for k, f := range names {
		v, ok := where[int32(k+1)] // sel 1 is FX1L … 22 is AUX6, in /-insert order
		if !ok {
			v = -1
		}
		s.setInt("/-insert/"+f.Name, v)
	}
}

// deriveHeadamps: /-ha/NN/index is the headamp feeding channel NN through
// its source and the input routing (local, AES50 A or B), -1 for none.
func (s *Server) deriveHeadamps() {
	block := "/config/routing/IN/"
	if v, _, _ := s.store.Get("/config/routing/routswitch"); v.Int == 1 {
		block = "/config/routing/PLAY/"
	}
	blocks := []string{"1-8", "9-16", "17-24", "25-32"}
	for ch := 0; ch < 40; ch++ {
		ha := int32(-1)
		if ch < 32 {
			if src := s.intAt(fmt.Sprintf("/ch/%02d/config/source", ch+1)); src >= 1 && src <= 32 {
				in := int(src - 1)
				l := s.store.Schema().Leaf(block + blocks[in/8])
				v, _, _ := s.store.Get(l.Addr)
				ha = headampOf(l.Type().Format(v), in%8)
			}
		}
		s.setInt(fmt.Sprintf("/-ha/%02d/index", ch), ha)
	}
}

// headampOf maps a routing block ("A17-24") and an offset in it to a
// headamp index: local 0-31, AES50 A 32-79, AES50 B 80-127.
func headampOf(block string, offset int) int32 {
	var base int
	switch {
	case strings.HasPrefix(block, "AN"):
		base, block = 0, block[2:]
	case strings.HasPrefix(block, "A"):
		base, block = 32, block[1:]
	case strings.HasPrefix(block, "B"):
		base, block = 80, block[1:]
	default:
		return -1 // card, user inputs
	}
	first, _, _ := strings.Cut(block, "-")
	n, err := strconv.Atoi(first)
	if err != nil {
		return -1
	}
	return int32(base + n - 1 + offset)
}

// action runs the side effect of a /-action/… set.
func (s *Server) action(addr string, v osc.Arg, origin string) {
	n := int(v.Int)
	run := func(kind string, idx int) {
		s.libraryOp(osc.NewMessage("/load", osc.String(kind), osc.Int(int32(idx))), kind, origin)
	}
	switch strings.TrimPrefix(addr, "/-action/") {
	case "goscene":
		run("scene", n)
	case "gosnippet":
		run("snippet", n)
	case "gocue":
		p := fmt.Sprintf("/-show/showfile/cue/%03d", n)
		if sc := s.intAt(p + "/scene"); sc >= 0 {
			run("scene", int(sc))
		}
		if sn := s.intAt(p + "/bit"); sn >= 0 {
			run("snippet", int(sn))
		}
		s.store.Set("/-show/prepos/current", osc.Int(int32(n)), origin)
	case "clearsolo":
		if n == 1 {
			for i := 1; i <= 80; i++ {
				s.store.Set(fmt.Sprintf("/-stat/solosw/%02d", i), osc.Int(0), origin)
			}
			s.setInt("/-action/clearsolo", 0)
		}
	case "undopt":
		if n == 1 {
			var b strings.Builder
			scene.Write(&b, s.store, "")
			s.undoMu.Lock()
			s.undo = strings.Split(strings.TrimSpace(b.String()), "\n")
			s.undoMu.Unlock()
			s.store.Set("/-undo/time", osc.String(time.Now().Format("15:04:05")), originConsole)
			s.setInt("/-action/undopt", 0)
		}
	case "doundo":
		if n == 1 {
			s.undoMu.Lock()
			lines := s.undo
			s.undoMu.Unlock()
			if len(lines) > 0 {
				s.store.ApplyLines(lines, origin)
			}
			s.setInt("/-action/doundo", 0)
		}
	case "initall":
		if n == 1 {
			s.resetScene(origin)
			s.setInt("/-action/initall", 0)
		}
	}
}

// resetScene gives every scene parameter its default value.
func (s *Server) resetScene(origin string) {
	var lines []string
	for _, n := range s.store.Schema().Nodes {
		if !n.InScene || n.Variant != nil {
			continue
		}
		vals := make([]osc.Arg, len(n.Fields))
		for i, f := range n.Fields {
			vals[i] = f.Type.Default()
		}
		lines = append(lines, n.FormatLine(vals))
	}
	s.store.ApplyLines(lines, origin)
}
