package emu

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
)

// rtaAt decodes /meters/15 at now into dB per band.
func rtaAt(t *testing.T, s *Server, now time.Time) [rtaBands]float64 {
	t.Helper()
	v := s.meterValues(15, nil, s.levels(now), now)
	if len(v) != rtaBands/2 {
		t.Fatalf("%d RTA values, want %d", len(v), rtaBands/2)
	}
	var out [rtaBands]float64
	for i, x := range v {
		out[2*i] = float64(int16(x)) / 256
		out[2*i+1] = float64(int16(x>>16)) / 256
	}
	return out
}

func apply(t *testing.T, s *Server, lines ...string) {
	t.Helper()
	for _, l := range lines {
		if _, err := s.Store().ApplyText(l, "test"); err != nil {
			t.Fatalf("%s: %v", l, err)
		}
	}
}

func TestRTAFollowsSource(t *testing.T) {
	s, addr := start(t)
	p := dial(t, addr)
	const band1k = 57 // 1015 Hz
	now := time.Now()

	// Channel 5 pulled down: the mains are silent, its input is not.
	p.send("/-action/setrtasrc", osc.Int(4))
	eventually(t, p, "/-stat/rtasource", 4)
	pre := rtaAt(t, s, now)
	if pre[band1k] < -80 {
		t.Fatalf("channel 5 before the EQ: %.1f dB at 1 kHz", pre[band1k])
	}

	// After the EQ, a +15 dB bell at 1 kHz shows.
	apply(t, s, "/ch/05/eq ON", "/ch/05/eq/2 PEQ 1k00 +15.00 2.0")
	s.setInt("/-stat/rtasource", 4+rtaPostEQ)
	post := rtaAt(t, s, now)
	if d := post[band1k] - pre[band1k]; d < 13 || d > 15.5 {
		t.Errorf("EQ bell: %+.1f dB at 1 kHz, want about +15", d)
	}
	if d := post[10] - pre[10]; math.Abs(d) > 0.5 {
		t.Errorf("EQ bell: %+.1f dB at 39 Hz, want 0", d)
	}

	// The low cut applies before the EQ too.
	apply(t, s, "/ch/05/preamp +0.0 OFF ON 24 200")
	s.setInt("/-stat/rtasource", 4)
	if d := rtaAt(t, s, now)[10] - pre[10]; d > -40 {
		t.Errorf("low cut at 200 Hz: %+.1f dB at 39 Hz", d)
	}

	// Dual GEQ A of FX slot 5, inserted on channel 5: band 18 (1 kHz) at +12 dB.
	apply(t, s, "/fx/5 GEQ2", "/ch/05/insert ON PRE FX5L")
	s.store.Set("/fx/5/par/18", osc.Float(0.9), "test")
	gq5a := 74 + 2*(5-1)
	s.setInt("/-stat/rtasource", int32(gq5a))
	pre = rtaAt(t, s, now)
	s.setInt("/-stat/rtasource", int32(gq5a+rtaPostEQ))
	post = rtaAt(t, s, now)
	if pre[band1k] < -80 {
		t.Fatalf("GEQ input: %.1f dB at 1 kHz", pre[band1k])
	}
	if d := post[band1k] - pre[band1k]; d < 10 || d > 12.5 {
		t.Errorf("GEQ band: %+.1f dB at 1 kHz, want about +11", d)
	}

	// Nothing is inserted on FX slot 6.
	s.setInt("/-stat/rtasource", int32(74+2*(6-1)))
	if v := rtaAt(t, s, now); v[band1k] != -128 {
		t.Errorf("GEQ not inserted: %.1f dB at 1 kHz, want -128", v[band1k])
	}
}

func TestRTABlob(t *testing.T) {
	s, addr := start(t)
	p := dial(t, addr)
	p.send("/-action/setrtasrc", osc.Int(0))
	// M32-Edit's request: an alias, the meter, two ints and a time factor;
	// it ignores blobs whose count is not 50.
	m := p.ask("/batchsubscribe", osc.String("meters/15"), osc.String("/meters/15"), osc.Int(0), osc.Int(0), osc.Int(1))
	if m.Address != "meters/15" || len(m.Args) != 1 || len(m.Args[0].Blob) != 204 {
		t.Fatalf("RTA reply %v", m)
	}
	if n := binary.LittleEndian.Uint32(m.Args[0].Blob); n != 50 {
		t.Errorf("RTA count %d", n)
	}
	p.send("/unsubscribe")
	drain(p)
	_ = s
}
