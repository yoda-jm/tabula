package emu

import (
	"fmt"
	"math"
	"math/cmplx"
	"time"
)

// The RTA (/meters/15) analyses the source chosen with /-action/setrtasrc and
// kept in /-stat/rtasource: 0-31 channels, 32-39 aux inputs, 40-47 FX returns,
// 48-63 buses, 64-69 matrices, 70 L/R, 71 M/C, 72 monitor, 73 talkback, 74-89
// dual GEQ A/B and 90-97 stereo GEQ of FX slots 1-8, before the EQ; 98 more
// for the same taps after the EQ. M32-Edit selects the channel shown on its EQ
// page, or the graphic EQ shown on its FX page.
//
// Each source gets a synthetic spectrum around its level. The low cut of a
// channel shapes it, and taps after the EQ add the response of the strip's
// equalizer, or of the graphic EQ, so editing them shows on the RTA.

const (
	rtaBands   = 100
	rtaPostEQ  = 98 // added to a source for the tap after the EQ
	sampleRate = 48000
)

// rtaFreq is the centre of RTA band i, ten bands per octave from 20 Hz to
// 18.66 kHz (the document's table).
func rtaFreq(i int) float64 { return 1250 * math.Pow(2, float64(i-60)/10) }

// preFader is the level of strip i (as numbered by stripPath) before its
// dynamics and fader.
func preFader(lv *mixLevels, i int) float64 {
	switch {
	case i < 48:
		return lv.in[i] * lv.gateGR[i]
	case i < 64:
		return lv.busIn[i-48]
	case i < 70:
		return lv.mtxIn[i-64]
	case i == 70:
		return lv.mainIn
	}
	return lv.monoIn
}

// rtaTap is the point the RTA listens to.
type rtaTap struct {
	level  float64 // linear
	strip  string  // strip whose low cut, and EQ if eq is set, come before the tap
	eq     bool
	geq    int // FX slot of the graphic EQ before the tap, 0 for none
	geqPar int // its first band parameter: 1, or 33 for side B of a dual GEQ
}

func (s *Server) rtaTap(r reader, lv *mixLevels, src int) rtaTap {
	post := src >= rtaPostEQ
	if post {
		src -= rtaPostEQ
	}
	switch {
	case src < 72:
		p, _ := stripPath(src)
		return rtaTap{level: preFader(lv, src), strip: p, eq: post}
	case src == 72:
		return rtaTap{level: lv.main}
	case src < 74 || src > 97:
		return rtaTap{}
	}
	// Graphic EQs listen to the strip they are inserted on.
	slot, sides := (src-74)/2+1, []int{(src - 74) % 2}
	if src >= 90 {
		slot, sides = src-89, []int{0, 1}
	}
	tap := rtaTap{}
	for _, side := range sides {
		if t, ok := s.insertTap(r, lv, slot, side); ok {
			tap = t
			break
		}
	}
	if post && s.isGEQ(r, slot) {
		tap.geq, tap.geqPar = slot, 1
		if src < 90 {
			tap.geqPar += 32 * ((src - 74) % 2)
		}
	}
	return tap
}

// insertTap finds the strip whose insert is side (0 L, 1 R) of FX slot.
func (s *Server) insertTap(r reader, lv *mixLevels, slot, side int) (rtaTap, bool) {
	sel := 1 + 2*(slot-1) + side // FX1L is 1 in /…/insert/sel
	for i := range 72 {
		p, _ := stripPath(i)
		if s.store.Schema().Leaf(p+"/insert/on") != nil && r.on(p+"/insert/on") && r.int(p+"/insert/sel") == sel {
			return rtaTap{level: preFader(lv, i), strip: p, eq: r.int(p+"/insert/pos") == 1}, true
		}
	}
	return rtaTap{}, false
}

func (s *Server) isGEQ(r reader, slot int) bool {
	v, l, err := s.store.Get(fmt.Sprintf("/fx/%d/type", slot))
	if err != nil {
		return false
	}
	switch s.store.TypeOf(l).Format(v) {
	case "GEQ", "GEQ2", "TEQ", "TEQ2":
		return true
	}
	return false
}

// rta returns the 100 RTA bands, packed by two as int16 dB × 256.
func (s *Server) rta(lv *mixLevels, now time.Time) []uint32 {
	r := reader{s}
	src := r.int("/-stat/rtasource")
	tap := s.rtaTap(r, lv, src)
	filters := tap.filters(r)
	t := now.Sub(s.started).Seconds()
	base := linToDB(tap.level)
	out := make([]uint32, rtaBands/2)
	for i := range rtaBands {
		f := rtaFreq(i)
		db := base + rtaShape(src%rtaPostEQ, i, t)
		for _, fl := range filters {
			db += fl(f)
		}
		db = math.Max(-128, math.Min(0, db))
		out[i/2] |= uint32(uint16(int16(math.Round(db*256)))) << (16 * (i % 2))
	}
	return out
}

// rtaShape is the spectrum of a source relative to its level: a broad hump
// whose centre (90 Hz to 1 kHz) depends on the source, falling off towards
// both ends and moving a little over time.
func rtaShape(seed, band int, t float64) float64 {
	centre := 22 + float64(seed*11%36)
	db := -0.35 * math.Abs(float64(band)-centre)
	if band > 80 {
		db -= 0.6 * float64(band-80)
	}
	return db + 2.5*math.Sin(t*2.7+float64(band)/6+float64(seed))
}

// filters returns the gain (dB) at f of each filter between the source and
// the tap.
func (tap rtaTap) filters(r reader) []func(f float64) float64 {
	var out []func(float64) float64
	sch := r.s.store.Schema()
	p := tap.strip
	if p != "" && sch.Leaf(p+"/preamp/hpon") != nil && r.on(p+"/preamp/hpon") {
		order := 2 + r.int(p+"/preamp/hpslope") // 12, 18 or 24 dB/octave
		fc := r.num(p + "/preamp/hpf")
		out = append(out, func(f float64) float64 { return butterworth(order, fc/f) })
	}
	if p != "" && tap.eq && r.on(p+"/eq/on") {
		for b := 1; ; b++ {
			bp := fmt.Sprintf("%s/eq/%d", p, b)
			v, l, err := r.s.store.Get(bp + "/type")
			if err != nil {
				break
			}
			typ, f0, g, q := r.s.store.TypeOf(l).Format(v), r.num(bp+"/f"), r.num(bp+"/g"), r.num(bp+"/q")
			lowCut := b <= 3 // crossover types cut the lows on bands 1-3, the highs above
			out = append(out, func(f float64) float64 { return eqBand(typ, f0, g, q, lowCut, f) })
		}
	}
	if tap.geq > 0 {
		var gains [32]float64 // 31 bands, then the master level
		for i := range gains {
			gains[i] = r.num(fmt.Sprintf("/fx/%d/par/%02d", tap.geq, tap.geqPar+i))
		}
		out = append(out, func(f float64) float64 { return geqGain(gains, f) })
	}
	return out
}

// butterworth is the gain (dB) of a Butterworth filter of the given order at
// x, the ratio of the cut-off to the frequency for a low cut (or the
// frequency to the cut-off for a high cut).
func butterworth(order int, x float64) float64 {
	return -10 * math.Log10(1+math.Pow(x, float64(2*order)))
}

// eqBand is the gain (dB) at f of one parametric EQ band: the biquads of
// Robert Bristow-Johnson's cookbook, and Butterworth or Linkwitz-Riley cuts
// for the crossover types of bus and main EQs (Bessel taken as Butterworth).
func eqBand(typ string, f0, gain, q float64, lowCut bool, f float64) float64 {
	x := f / f0
	if lowCut {
		x = f0 / f
	}
	switch typ {
	case "BU6":
		return butterworth(1, x)
	case "BU12", "BS12":
		return butterworth(2, x)
	case "BU18":
		return butterworth(3, x)
	case "BU24", "BS24":
		return butterworth(4, x)
	case "LR12":
		return 2 * butterworth(1, x)
	case "LR24":
		return 2 * butterworth(2, x)
	}
	w0 := 2 * math.Pi * f0 / sampleRate
	a := math.Pow(10, gain/40)
	alpha := math.Sin(w0) / (2 * q)
	cw := math.Cos(w0)
	sa := 2 * math.Sqrt(a) * alpha
	var b0, b1, b2, a0, a1, a2 float64
	switch typ {
	case "PEQ", "VEQ":
		b0, b1, b2 = 1+alpha*a, -2*cw, 1-alpha*a
		a0, a1, a2 = 1+alpha/a, -2*cw, 1-alpha/a
	case "LShv":
		b0, b1, b2 = a*((a+1)-(a-1)*cw+sa), 2*a*((a-1)-(a+1)*cw), a*((a+1)-(a-1)*cw-sa)
		a0, a1, a2 = (a+1)+(a-1)*cw+sa, -2*((a-1)+(a+1)*cw), (a+1)+(a-1)*cw-sa
	case "HShv":
		b0, b1, b2 = a*((a+1)+(a-1)*cw+sa), -2*a*((a-1)+(a+1)*cw), a*((a+1)+(a-1)*cw-sa)
		a0, a1, a2 = (a+1)-(a-1)*cw+sa, 2*((a-1)-(a+1)*cw), (a+1)-(a-1)*cw-sa
	case "LCut":
		b0, b1, b2 = (1+cw)/2, -(1 + cw), (1+cw)/2
		a0, a1, a2 = 1+alpha, -2*cw, 1-alpha
	case "HCut":
		b0, b1, b2 = (1-cw)/2, 1-cw, (1-cw)/2
		a0, a1, a2 = 1+alpha, -2*cw, 1-alpha
	default:
		return 0
	}
	z := cmplx.Exp(complex(0, -2*math.Pi*f/sampleRate)) // z⁻¹
	h := (complex(b0, 0) + complex(b1, 0)*z + complex(b2, 0)*z*z) /
		(complex(a0, 0) + complex(a1, 0)*z + complex(a2, 0)*z*z)
	return 20 * math.Log10(cmplx.Abs(h))
}

// geqGain is the gain (dB) at f of a graphic EQ, interpolating between its
// 31 third-octave bands (1 kHz is band 17), plus its master level.
func geqGain(gains [32]float64, f float64) float64 {
	x := math.Max(0, math.Min(30, 17+3*math.Log2(f/1000)))
	i := min(int(x), 29)
	frac := x - float64(i)
	return gains[i]*(1-frac) + gains[i+1]*frac + gains[31]
}
