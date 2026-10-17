package emu

import (
	"fmt"
	"math"
	"time"

	"github.com/yoda-jm/tabula/pkg/schema"
)

// Synthetic meters: every input channel carries a slowly breathing signal;
// levels then follow the mix as set on the emulator (gate, compressor, fader,
// mute, sends, buses, mains, matrices), so meters react to what a client does.

// signal is the input level (linear, 1.0 = 0 dBFS) of channel i at t seconds.
func signal(i int, t float64) float64 {
	fi := float64(i)
	slow := 0.5 + 0.5*math.Sin(2*math.Pi*t/(4+0.7*float64(i%7))+fi)
	fast := 0.5 + 0.5*math.Sin(2*math.Pi*t*(2.3+0.37*float64(i%5))+1.7*fi)
	return dbToLin(-42 + 22*slow + 6*fast)
}

func dbToLin(db float64) float64 {
	if math.IsInf(db, -1) {
		return 0
	}
	return math.Pow(10, db/20)
}

func linToDB(v float64) float64 {
	if v <= 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(v)
}

// mixLevels is one snapshot of every metered point.
type mixLevels struct {
	in, gateGR, dynGR, post [48]float64 // channels 0-31, aux 32-39, fx returns 40-47
	bus, busGR, busIn       [16]float64 // busIn: the mix before dynamics and fader
	mtx, mtxGR, mtxIn       [6]float64
	main, mainGR, mainIn    float64 // stereo L/R carry the same level
	mono, monoGR, monoIn    float64
	dca                     [8]float64
}

type reader struct{ s *Server }

func (r reader) num(addr string) float64 {
	v, l, err := r.s.store.Get(addr)
	if err != nil {
		return 0
	}
	if n, ok := r.s.store.TypeOf(l).(schema.Num); ok {
		return n.Display(v)
	}
	return float64(v.Int)
}

func (r reader) int(addr string) int {
	v, _, _ := r.s.store.Get(addr)
	return int(v.Int)
}

func (r reader) on(addr string) bool { return r.int(addr) == 1 }

func (r reader) gain(addr string) float64 { return dbToLin(r.num(addr)) }

// dynamics returns the gate and compressor gains applied to level x.
func (r reader) dynamics(p string, x float64) (gate, comp float64) {
	gate, comp = 1, 1
	db := linToDB(x)
	if r.s.store.Schema().Leaf(p+"/gate/on") != nil && r.on(p+"/gate/on") && db < r.num(p+"/gate/thr") {
		gate = dbToLin(-r.num(p + "/gate/range"))
	}
	if r.on(p + "/dyn/on") {
		thr := r.num(p + "/dyn/thr")
		var ratio float64
		v, l, _ := r.s.store.Get(p + "/dyn/ratio")
		fmt.Sscan(l.Type().Format(v), &ratio)
		if ratio > 1 && db > thr {
			comp = dbToLin((thr + (db-thr)/ratio) - db)
		}
	}
	return gate, comp
}

func (s *Server) levels(now time.Time) *mixLevels {
	r := reader{s}
	t := now.Sub(s.started).Seconds()
	lv := &mixLevels{}
	var busPow [16]float64
	var mainPow, monoPow float64
	strips := make([]string, 0, 48)
	for i := 1; i <= 32; i++ {
		strips = append(strips, fmt.Sprintf("/ch/%02d", i))
	}
	for i := 1; i <= 8; i++ {
		strips = append(strips, fmt.Sprintf("/auxin/%02d", i))
	}
	for i := 1; i <= 8; i++ {
		strips = append(strips, fmt.Sprintf("/fxrtn/%02d", i))
	}

	// Channels feed buses and mains; FX returns follow the FX send buses.
	pass := func(lo, hi int) {
		for i := lo; i < hi; i++ {
			p := strips[i]
			switch {
			case i < 32 && r.int(p+"/config/source") > 0:
				lv.in[i] = signal(i, t)
			case i >= 40:
				lv.in[i] = s.fxReturn(r, lv, (i-40)/2)
			}
			lv.gateGR[i], lv.dynGR[i] = 1, 1
			if i < 32 {
				lv.gateGR[i], lv.dynGR[i] = r.dynamics(p, lv.in[i])
			}
			pre := lv.in[i] * lv.gateGR[i] * lv.dynGR[i]
			if r.on(p + "/mix/on") {
				lv.post[i] = pre * r.gain(p+"/mix/fader")
			}
			if r.on(p + "/mix/st") {
				mainPow += lv.post[i] * lv.post[i]
			}
			if r.on(p + "/mix/mono") {
				x := lv.post[i] * r.gain(p+"/mix/mlevel")
				monoPow += x * x
			}
			for b := 0; b < 16; b++ {
				sp := fmt.Sprintf("%s/mix/%02d", p, b+1)
				if !r.on(sp + "/on") {
					continue
				}
				src := pre
				if r.int(fmt.Sprintf("%s/mix/%02d/type", p, b/2*2+1)) == 4 { // POST
					src = lv.post[i]
				}
				x := src * r.gain(sp+"/level")
				busPow[b] += x * x
			}
			grp := r.int(p + "/grp/dca")
			for d := 0; d < 8; d++ {
				if grp&(1<<d) != 0 {
					lv.dca[d] = math.Max(lv.dca[d], lv.post[i])
				}
			}
		}
	}
	finishBuses := func() {
		for b := 0; b < 16; b++ {
			p := fmt.Sprintf("/bus/%02d", b+1)
			x := math.Sqrt(busPow[b])
			lv.busIn[b] = x
			gate, comp := r.dynamics(p, x)
			lv.busGR[b] = gate * comp
			if r.on(p + "/mix/on") {
				lv.bus[b] = x * lv.busGR[b] * r.gain(p+"/mix/fader")
			}
			if r.on(p + "/mix/st") {
				mainPow += lv.bus[b] * lv.bus[b]
			}
		}
	}
	pass(0, 40)
	finishBuses()
	pass(40, 48)

	x := math.Sqrt(mainPow)
	lv.mainIn = x
	_, lv.mainGR = r.dynamics("/main/st", x)
	if r.on("/main/st/mix/on") {
		lv.main = x * lv.mainGR * r.gain("/main/st/mix/fader")
	}
	x = math.Sqrt(monoPow)
	lv.monoIn = x
	_, lv.monoGR = r.dynamics("/main/m", x)
	if r.on("/main/m/mix/on") {
		lv.mono = x * lv.monoGR * r.gain("/main/m/mix/fader")
	}
	for m := 0; m < 6; m++ {
		var pow float64
		for _, src := range []struct {
			p   string
			lvl float64
		}{{"/main/st", lv.main}, {"/main/m", lv.mono}} {
			sp := fmt.Sprintf("%s/mix/%02d", src.p, m+1)
			if r.on(sp + "/on") {
				y := src.lvl * r.gain(sp+"/level")
				pow += y * y
			}
		}
		for b := 0; b < 16; b++ {
			sp := fmt.Sprintf("/bus/%02d/mix/%02d", b+1, m+1)
			if r.on(sp + "/on") {
				y := lv.bus[b] * r.gain(sp+"/level")
				pow += y * y
			}
		}
		p := fmt.Sprintf("/mtx/%02d", m+1)
		y := math.Sqrt(pow)
		lv.mtxIn[m] = y
		_, lv.mtxGR[m] = r.dynamics(p, y)
		if r.on(p + "/mix/on") {
			lv.mtx[m] = y * lv.mtxGR[m] * r.gain(p+"/mix/fader")
		}
	}
	return lv
}

// fxReturn is the return level of FX slot k (0-3): the level of its source bus.
func (s *Server) fxReturn(r reader, lv *mixLevels, k int) float64 {
	src := r.int(fmt.Sprintf("/fx/%d/source/l", k+1))
	if src < 1 || src > 16 {
		return 0
	}
	return lv.bus[src-1] * 0.5
}

// outputLevel is the level of an output source index (/outputs/…/src).
func outputLevel(lv *mixLevels, src int) float64 {
	switch {
	case src == 1 || src == 2:
		return lv.main
	case src == 3:
		return lv.mono
	case src >= 4 && src <= 19:
		return lv.bus[src-4]
	case src >= 20 && src <= 25:
		return lv.mtx[src-20]
	case src >= 26 && src <= 57:
		return lv.post[src-26]
	case src >= 58 && src <= 65:
		return lv.post[32+src-58]
	case src >= 66 && src <= 73:
		return lv.post[40+src-66]
	}
	return 0
}

func f32(v float64) uint32 { return math.Float32bits(float32(v)) }

// meterValues returns the values of /meters/<id> (as float32 bits, or packed
// shorts for meters 15 and 16).
func (s *Server) meterValues(id int, params []int32, now time.Time) []uint32 {
	lv := s.levels(now)
	r := reader{s}
	var out []uint32
	add := func(v ...float64) {
		for _, x := range v {
			out = append(out, f32(x))
		}
	}
	addSlice := func(v []float64) { add(v...) }
	outputs := func(prefix string, n int) {
		for i := 1; i <= n; i++ {
			add(outputLevel(lv, r.int(fmt.Sprintf("%s/%02d/src", prefix, i))))
		}
	}
	switch id {
	case 0:
		addSlice(lv.in[:48])
		addSlice(lv.bus[:])
		addSlice(lv.mtx[:])
	case 1:
		addSlice(lv.in[:32])
		addSlice(lv.gateGR[:32])
		addSlice(lv.dynGR[:32])
	case 2:
		addSlice(lv.bus[:])
		addSlice(lv.mtx[:])
		add(lv.main, lv.main, lv.mono)
		addSlice(lv.busGR[:])
		addSlice(lv.mtxGR[:])
		add(lv.mainGR, lv.monoGR)
	case 3:
		outputs("/outputs/aux", 6)
		addSlice(lv.in[32:48])
	case 4:
		addSlice(lv.in[:40])
		outputs("/outputs/main", 16)
		outputs("/outputs/p16", 16)
		outputs("/outputs/aux", 6)
		outputs("/outputs/aes", 2)
		add(lv.main, lv.main)
	case 5:
		group := 0
		if len(params) > 0 {
			group = int(params[0])
		}
		switch group {
		case 1:
			addSlice(lv.post[16:32])
		case 2:
			addSlice(lv.post[32:48])
		case 3:
			addSlice(lv.bus[:])
		default:
			addSlice(lv.post[:16])
		}
		grp := 0
		if len(params) > 1 {
			grp = int(params[1])
		}
		switch grp {
		case 1:
			addSlice(lv.bus[:8])
		case 2:
			addSlice(lv.bus[8:])
		case 3:
			addSlice(lv.mtx[:])
			add(0, 0)
		default:
			addSlice(lv.dca[:])
		}
		add(lv.main, lv.main, lv.mono)
	case 6:
		ch := 0
		if len(params) > 0 {
			ch = int(params[0])
		}
		switch {
		case ch >= 0 && ch < 48:
			add(lv.in[ch], lv.gateGR[ch], lv.dynGR[ch], lv.post[ch])
		case ch >= 48 && ch < 64:
			add(lv.bus[ch-48], 1, lv.busGR[ch-48], lv.bus[ch-48])
		case ch >= 64 && ch < 70:
			add(lv.mtx[ch-64], 1, lv.mtxGR[ch-64], lv.mtx[ch-64])
		case ch == 70:
			add(lv.main, 1, lv.mainGR, lv.main)
		default:
			add(lv.mono, 1, lv.monoGR, lv.mono)
		}
	case 7:
		addSlice(lv.bus[:])
	case 8:
		addSlice(lv.mtx[:])
	case 9:
		for k := 0; k < 8; k++ {
			ret := 0.0
			if k < 4 {
				ret = lv.in[40+2*k]
			}
			add(ret*2, ret*2, ret, ret)
		}
	case 10:
		for range 32 {
			add(1)
		}
	case 11:
		add(lv.main, lv.main, 0, 1, 0)
	case 12:
		add(lv.main, lv.main, 0, 0)
	case 13:
		addSlice(lv.in[:48])
	case 14:
		for range 80 {
			add(0)
		}
	case 15:
		out = s.rta(lv, now)
	case 16:
		var shorts []float64
		shorts = append(shorts, lv.gateGR[:32]...)
		shorts = append(shorts, lv.dynGR[:32]...)
		shorts = append(shorts, lv.busGR[:]...)
		shorts = append(shorts, lv.mtxGR[:]...)
		shorts = append(shorts, lv.mainGR, lv.monoGR)
		for i := 0; i < len(shorts); i += 2 {
			out = append(out, uint32(uint16(shorts[i]*32767))|uint32(uint16(shorts[i+1]*32767))<<16)
		}
		for range 4 {
			out = append(out, 0)
		}
	}
	return out
}
