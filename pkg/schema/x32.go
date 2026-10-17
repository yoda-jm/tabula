package schema

import (
	"fmt"
	"math"
)

// Model is a console model as reported by /info: X32, X32RACK, X32C, X32P,
// X32CORE, M32, M32R or M32C. All share the same parameter tree.
type Model string

// Ranges, steps and enum lists follow the "Unofficial X32/M32 OSC Remote
// Protocol" (P-G. Maillot, 2021); text formats follow real scene files.

var (
	colors   = Enum{Names: []string{"OFF", "RD", "GN", "YE", "BL", "MG", "CY", "WH", "OFFi", "RDi", "GNi", "YEi", "BLi", "MGi", "CYi", "WHi"}}
	gateMode = Enum{Names: []string{"EXP2", "EXP3", "EXP4", "GATE", "DUCK"}, Def: 3}
	dynMode  = Enum{Names: []string{"COMP", "EXP"}}
	dynDet   = Enum{Names: []string{"PEAK", "RMS"}, Def: 1}
	dynEnv   = Enum{Names: []string{"LIN", "LOG"}, Def: 1}
	ratio    = Enum{Names: []string{"1.1", "1.3", "1.5", "2.0", "2.5", "3.0", "4.0", "5.0", "7.0", "10", "20", "100"}, Def: 5}
	prePost  = Enum{Names: []string{"PRE", "POST"}, Def: 1}
	keyFilt  = Enum{Names: []string{"LC6", "LC12", "HC6", "HC12", "1.0", "2.0", "3.0", "5.0", "10.0"}, Def: 6}
	hpSlope  = Enum{Names: []string{"12", "18", "24"}, Def: 2}
	insSel   = Enum{Names: []string{"OFF", "FX1L", "FX1R", "FX2L", "FX2R", "FX3L", "FX3R", "FX4L", "FX4R",
		"FX5L", "FX5R", "FX6L", "FX6R", "FX7L", "FX7R", "FX8L", "FX8R",
		"AUX1", "AUX2", "AUX3", "AUX4", "AUX5", "AUX6"}}
	eqType6  = Enum{Names: []string{"LCut", "LShv", "PEQ", "VEQ", "HShv", "HCut"}, Def: 2}
	eqType14 = Enum{Names: append(append([]string(nil), eqType6.Names...),
		"BU6", "BU12", "BS12", "LR12", "BU18", "BU24", "BS24", "LR24"), Def: 2}
	sendType  = Enum{Names: []string{"IN/LC", "<-EQ", "EQ->", "PRE", "POST", "GRP"}, Def: 4}
	outPos    = Enum{Names: []string{"IN/LC", "IN/LC+M", "<-EQ", "<-EQ+M", "EQ->", "EQ->+M", "PRE", "PRE+M", "POST"}, Def: 8}
	pflAfl    = Enum{Names: []string{"PFL", "AFL"}}
	automixGr = Enum{Names: []string{"OFF", "X", "Y"}}
	fxSource  = Enum{Names: []string{"INS", "MIX1", "MIX2", "MIX3", "MIX4", "MIX5", "MIX6", "MIX7", "MIX8",
		"MIX9", "MIX10", "MIX11", "MIX12", "MIX13", "MIX14", "MIX15", "MIX16", "M/C"}}
	fxType1 = Enum{Names: []string{"HALL", "AMBI", "RPLT", "ROOM", "CHAM", "PLAT", "VREV", "VRM",
		"GATE", "RVRS", "DLY", "3TAP", "4TAP", "CRS", "FLNG", "PHAS", "DIMC", "FILT", "ROTA", "PAN",
		"SUB", "D/RV", "CR/R", "FL/R", "D/CR", "D/FL", "MODD", "GEQ2", "GEQ", "TEQ2", "TEQ", "DES2",
		"DES", "P1A", "P1A2", "PQ5", "PQ5S", "WAVD", "LIM", "CMB", "CMB2", "FAC", "FAC1M", "FAC2",
		"LEC", "LEC2", "ULC", "ULC2", "ENH2", "ENH", "EXC2", "EXC", "IMG", "EDI", "SON", "AMP2",
		"AMP", "DRV2", "DRV", "PIT2", "PIT"}}
	fxType2 = Enum{Names: []string{"GEQ2", "GEQ", "TEQ2", "TEQ", "DES2", "DES", "P1A", "P1A2", "PQ5",
		"PQ5S", "WAVD", "LIM", "FAC", "FAC1M", "FAC2", "LEC", "LEC2", "ULC", "ULC2", "ENH2", "ENH",
		"EXC2", "EXC", "IMG", "EDI", "SON", "AMP2", "AMP", "DRV2", "DRV", "PHAS", "FILT", "PAN", "SUB"}}
	iqGroup   = Enum{Names: []string{"OFF", "A", "B"}}
	iqSpeaker = Enum{Names: []string{"none", "iQ8", "iQ10", "iQ12", "iQ15", "iQ15B", "iQ18B"}}
	iqEQ      = Enum{Names: []string{"Linear", "Live", "Speech", "Playback", "User"}}

	routIn = Enum{Names: []string{"AN1-8", "AN9-16", "AN17-24", "AN25-32", "A1-8", "A9-16", "A17-24", "A25-32",
		"A33-40", "A41-48", "B1-8", "B9-16", "B17-24", "B25-32", "B33-40", "B41-48",
		"CARD1-8", "CARD9-16", "CARD17-24", "CARD25-32", "UIN1-8", "UIN9-16", "UIN17-24", "UIN25-32"}}
	routAux = Enum{Names: []string{"AUX1-4", "AN1-2", "AN1-4", "AN1-6", "A1-2", "A1-4", "A1-6",
		"B1-2", "B1-4", "B1-6", "CARD1-2", "CARD1-4", "CARD1-6", "UIN1-2", "UIN1-4", "UIN1-6"}}
	routBlock = Enum{Names: append(append([]string(nil), routIn.Names[:20]...),
		"OUT1-8", "OUT9-16", "P161-8", "P169-16", "AUX1-6/Mon", "AuxIN1-6/TB",
		"UOUT1-8", "UOUT9-16", "UOUT17-24", "UOUT25-32", "UOUT33-40", "UOUT41-48",
		"UIN1-8", "UIN9-16", "UIN17-24", "UIN25-32")}
	routOut1 = Enum{Names: []string{"AN1-4", "AN9-12", "AN17-20", "AN25-28", "A1-4", "A9-12", "A17-20", "A25-28",
		"A33-36", "A41-44", "B1-4", "B9-12", "B17-20", "B25-28", "B33-36", "B41-44",
		"CARD1-4", "CARD9-12", "CARD17-20", "CARD25-28", "OUT1-4", "OUT9-12", "P161-4", "P169-12",
		"AUX/CR", "AUX/TB", "UOUT1-4", "UOUT9-12", "UOUT17-20", "UOUT25-28", "UOUT33-36", "UOUT41-44",
		"UIN1-4", "UIN9-12", "UIN17-20", "UIN25-28"}}
	routOut2 = Enum{Names: []string{"AN5-8", "AN13-16", "AN21-24", "AN29-32", "A5-8", "A13-16", "A21-24", "A29-32",
		"A37-40", "A45-48", "B5-8", "B13-16", "B21-24", "B29-32", "B37-40", "B45-48",
		"CARD5-8", "CARD13-16", "CARD21-24", "CARD29-32", "OUT5-8", "OUT13-16", "P165-8", "P1613-16",
		"AUX/CR", "AUX/TB", "UOUT5-8", "UOUT13-16", "UOUT21-24", "UOUT29-32", "UOUT37-40", "UOUT45-48",
		"UIN5-8", "UIN13-16", "UIN21-24", "UIN29-32"}}
)

var negInf = math.Inf(-1)

func fader(def float64) Num {
	return Num{Scale: Level{}, Steps: 1024, Fmt: levelFmt, Unit: "dB", Def: def}
}
func level161(def float64) Num {
	return Num{Scale: Level{}, Steps: 161, Fmt: levelFmt, Unit: "dB", Def: def}
}

// lin builds a linear float from its range and step size.
func lin(lo, hi, step float64, f func(float64) string, unit string, def float64) Num {
	return Num{Scale: Linf{lo, hi}, Steps: int(math.Round((hi-lo)/step)) + 1, Fmt: f, Unit: unit, Def: def}
}

func logf(lo, hi float64, steps int, f func(float64) string, unit string, def float64) Num {
	return Num{Scale: Logf{lo, hi}, Steps: steps, Fmt: f, Unit: unit, Def: def}
}

var (
	pan       = lin(-100, 100, 2, fixed(0, true, 0), "", 0)
	trim      = lin(-18, 18, 0.25, fixed(1, true, 0), "dB", 0)
	delayTime = lin(0.3, 500, 0.1, fixed(1, false, 5), "ms", 0.3)
	hpf       = logf(20, 400, 101, fixed(0, false, 3), "Hz", 20)
	keyFreq   = logf(20, 20000, 201, freqFmt, "Hz", 990.9)
	eqGain    = lin(-15, 15, 0.25, fixed(2, true, 0), "dB", 0)
	eqQ       = logf(10, 0.3, 72, fixed(1, false, 0), "", 2)
	attack    = lin(0, 120, 1, fixed(0, false, 0), "ms", 10)
	headGain  = lin(-12, 60, 0.5, fixed(1, true, 0), "dB", 0)
)

func hold(def float64) Num    { return logf(0.02, 2000, 101, holdFmt, "ms", def) }
func release(def float64) Num { return logf(5, 4000, 101, releaseFmt, "ms", def) }
func freq(steps int, def float64) Num {
	return logf(20, 20000, steps, freqFmt, "Hz", def)
}

type builder struct{ s *Schema }

func (b builder) node(path string, fields ...Field) {
	b.s.add(&Node{Path: path, Fields: fields, InScene: true})
}

func f(name string, t Type) Field { return Field{Name: name, Type: t} }

// fields builds n fields named by name(i) for i in [1, n].
func fields(n int, name func(int) string, t Type) []Field {
	out := make([]Field, n)
	for i := range out {
		out[i] = f(name(i+1), t)
	}
	return out
}

func two(i int) string   { return fmt.Sprintf("%02d", i) }
func plain(i int) string { return fmt.Sprint(i) }
func pair(i int) string  { return fmt.Sprintf("%d-%d", 2*i-1, 2*i) }

// Build returns the parameter tree of a console model: the scene nodes in
// scene file order, then the nodes outside scenes (console.go).
func Build(m Model) *Schema {
	b := builder{newSchema(m)}
	b.config()
	for i := 1; i <= 32; i++ {
		b.channel(fmt.Sprintf("/ch/%02d", i), i)
	}
	for i := 1; i <= 8; i++ {
		b.auxin(fmt.Sprintf("/auxin/%02d", i), int32(32+i))
	}
	for i := 1; i <= 8; i++ {
		b.fxrtn(fmt.Sprintf("/fxrtn/%02d", i))
	}
	for i := 1; i <= 16; i++ {
		b.bus(fmt.Sprintf("/bus/%02d", i), fmt.Sprintf("MixBus %d", i))
	}
	for i := 1; i <= 6; i++ {
		b.matrix(fmt.Sprintf("/mtx/%02d", i), fmt.Sprintf("Matrix %d", i))
	}
	b.main("/main/st", true)
	b.main("/main/m", false)
	for i := 1; i <= 8; i++ {
		p := fmt.Sprintf("/dca/%d", i)
		b.node(p, f("on", OnOff), f("fader", fader(0)))
		b.node(p+"/config", f("name", Str{Max: 12, Def: fmt.Sprintf("DCA %d", i)}), f("icon", Int{1, 74, 1}), f("color", colors))
	}
	for i := 1; i <= 8; i++ {
		p := fmt.Sprintf("/fx/%d", i)
		if i <= 4 {
			b.node(p, f("type", fxType1))
			b.node(p+"/source", f("l", fxSource), f("r", fxSource))
		} else {
			b.node(p, f("type", fxType2))
		}
		// Parameter formats depend on the effect type; kept as text for now.
		b.node(p+"/par", f("raw", Raw{}))
	}
	b.outputs()
	for i := 0; i < 128; i++ {
		b.node(fmt.Sprintf("/headamp/%03d", i), f("gain", headGain), f("phantom", OnOff))
	}
	b.console()
	return b.s
}

func (b builder) config() {
	b.node("/config/chlink", fields(16, pair, OnOff)...)
	b.node("/config/auxlink", fields(4, pair, OnOff)...)
	b.node("/config/fxlink", fields(4, pair, OnOff)...)
	b.node("/config/buslink", fields(8, pair, OnOff)...)
	b.node("/config/mtxlink", fields(3, pair, OnOff)...)
	b.node("/config/mute", fields(6, plain, OnOff)...)
	b.node("/config/linkcfg", f("hadly", OnOff), f("eq", OnOff), f("dyn", OnOff), f("fdrmute", OnOff))
	b.node("/config/mono", f("mode", Enum{Names: []string{"LR+M", "LCR"}}), f("link", OnOff))
	b.node("/config/solo",
		f("level", level161(0)),
		f("source", Enum{Names: []string{"OFF", "LR", "LR+C", "LRPFL", "LRAFL", "AUX56", "AUX78"}}),
		f("sourcetrim", lin(-18, 18, 0.5, fixed(1, false, 0), "dB", 0)),
		f("chmode", pflAfl), f("busmode", pflAfl), f("dcamode", pflAfl),
		f("exclusive", OnOff), f("followsel", OnOff), f("followsolo", OnOff),
		f("dimatt", lin(-40, 0, 1, fixed(0, false, 0), "dB", -20)),
		f("dim", OnOff), f("mono", OnOff), f("delay", OnOff),
		f("delaytime", delayTime),
		f("masterctrl", OnOff), f("mute", OnOff), f("dimpfl", OnOff))
	b.node("/config/talk", f("enable", OnOff), f("source", Enum{Names: []string{"INT", "EXT"}}))
	for _, ab := range []string{"A", "B"} {
		b.node("/config/talk/"+ab, f("level", level161(-12)), f("dim", OnOff), f("latch", OnOff), f("destmap", Bitmap{18}))
	}
	b.node("/config/osc",
		f("level", level161(-40)), f("f1", freq(121, 100)), f("f2", freq(121, 1000)),
		f("fsel", Enum{Names: []string{"F1", "F2"}}),
		f("type", Enum{Names: []string{"SINE", "PINK", "WHITE"}}),
		f("dest", Int{0, 25, 0}))
	b.node("/config/userrout/out", fields(48, two, Int{0, 208, 0})...)
	b.node("/config/userrout/in", fields(32, two, Int{0, 168, 0})...)
	b.node("/config/routing", f("routswitch", Enum{Names: []string{"REC", "PLAY"}}))
	in := []Field{f("1-8", routIn), f("9-16", routIn), f("17-24", routIn), f("25-32", routIn), f("AUX", routAux)}
	b.node("/config/routing/IN", in...)
	blocks := []string{"1-8", "9-16", "17-24", "25-32", "33-40", "41-48"}
	for _, port := range []string{"AES50A", "AES50B"} {
		b.node("/config/routing/"+port, fields(6, func(i int) string { return blocks[i-1] }, routBlock)...)
	}
	b.node("/config/routing/CARD", fields(4, func(i int) string { return blocks[i-1] }, routBlock)...)
	b.node("/config/routing/OUT", f("1-4", routOut1), f("5-8", routOut2), f("9-12", routOut1), f("13-16", routOut2))
	b.node("/config/routing/PLAY", in...)
	for _, set := range []string{"A", "B", "C"} {
		p := "/config/userctrl/" + set
		b.node(p, f("color", colors))
		b.node(p+"/enc", fields(4, plain, Str{Max: 7, Def: "-"})...)
		b.node(p+"/btn", fields(8, func(i int) string { return fmt.Sprint(i + 4) }, Str{Max: 7, Def: "-"})...)
	}
	b.node("/config/tape", f("gainL", lin(-6, 24, 0.5, fixed(1, false, 0), "dB", 0)),
		f("gainR", lin(-6, 24, 0.5, fixed(1, false, 0), "dB", 0)), f("autoplay", OnOff))
	b.node("/config/amixenable", f("X", OnOff), f("Y", OnOff))
	// Leaf name "asap" as written in Maillot's document (DP48 broadcast target port).
	b.node("/config/dp48", f("scope", Bitmap{4}), f("broadcast", Int{0, 1, 0}), f("asap", Enum{Names: []string{"AESA", "AESB"}}))
	b.node("/config/dp48/assign", fields(48, two, Int{0, 12, 0})...)
	b.node("/config/dp48/link", fields(24, two, Int{0, 1, 0})...)
	b.node("/config/dp48/grpname", fields(12, two, Str{Max: 8})...)
}

func strip(name string, icon int32, withSource bool, source int32) []Field {
	fs := []Field{f("name", Str{Max: 12, Def: name}), f("icon", Int{1, 74, icon}), f("color", colors)}
	if withSource {
		fs = append(fs, f("source", Int{0, 64, source}))
	}
	return fs
}

func (b builder) filter(p string) {
	b.node(p, f("on", OnOff), f("type", keyFilt), f("f", keyFreq))
}

func (b builder) dyn(p string, keysrc bool) {
	fs := []Field{
		f("on", OnOff), f("mode", dynMode), f("det", dynDet), f("env", dynEnv),
		f("thr", lin(-60, 0, 0.5, fixed(1, false, 0), "dB", 0)), f("ratio", ratio),
		f("knee", lin(0, 5, 1, fixed(0, false, 0), "", 1)),
		f("mgain", lin(0, 24, 0.5, fixed(2, false, 0), "dB", 0)),
		f("attack", attack), f("hold", hold(10)), f("release", release(151)), f("pos", prePost),
	}
	if keysrc {
		fs = append(fs, f("keysrc", Int{0, 64, 0}))
	}
	fs = append(fs, f("mix", lin(0, 100, 5, fixed(0, false, 0), "%", 100)), f("auto", OnOff))
	b.node(p+"/dyn", fs...)
	b.filter(p + "/dyn/filter")
}

func (b builder) eq(p string, bands int, types Enum) {
	b.node(p+"/eq", f("on", OnOff))
	defaults := []float64{124.7, 496.6, 1970, 10020, 10020, 10020}
	for i := 1; i <= bands; i++ {
		b.node(fmt.Sprintf("%s/eq/%d", p, i), f("type", types), f("f", freq(201, defaults[i-1])), f("g", eqGain), f("q", eqQ))
	}
}

// sends adds mix/01..n: odd sends carry pan, tap and pan-follow, even ones
// on and level only.
func (b builder) sends(p string, n int) {
	for i := 1; i <= n; i++ {
		sp := fmt.Sprintf("%s/mix/%02d", p, i)
		if i%2 == 1 {
			b.node(sp, f("on", OnOff), f("level", level161(negInf)), f("pan", pan), f("type", sendType), f("panFollow", Int{0, 1, 0}))
		} else {
			b.node(sp, f("on", OnOff), f("level", level161(negInf)))
		}
	}
}

func (b builder) grp(p string) {
	b.node(p+"/grp", f("dca", Bitmap{8}), f("mute", Bitmap{6}))
}

func (b builder) channelMix(p string) {
	b.node(p+"/mix", f("on", OnOff), f("fader", fader(negInf)), f("st", OnOff), f("pan", pan), f("mono", OnOff), f("mlevel", level161(negInf)))
}

func (b builder) channel(p string, n int) {
	b.node(p+"/config", strip("", 1, true, int32(n))...)
	b.node(p+"/delay", f("on", OnOff), f("time", delayTime))
	b.node(p+"/preamp", f("trim", trim), f("invert", OnOff), f("hpon", OnOff), f("hpslope", hpSlope), f("hpf", hpf))
	b.node(p+"/gate", f("on", OnOff), f("mode", gateMode),
		f("thr", lin(-80, 0, 0.5, fixed(1, false, 0), "dB", -80)),
		f("range", lin(3, 60, 1, fixed(1, false, 0), "dB", 60)),
		f("attack", attack), f("hold", hold(50.2)), f("release", release(258)), f("keysrc", Int{0, 64, 0}))
	b.filter(p + "/gate/filter")
	b.dyn(p, true)
	b.node(p+"/insert", f("on", OnOff), f("pos", prePost), f("sel", insSel))
	b.eq(p, 4, eqType6)
	b.channelMix(p)
	b.sends(p, 16)
	b.grp(p)
	b.node(p+"/automix", f("group", automixGr), f("weight", lin(-12, 12, 0.5, fixed(1, true, 5), "dB", 0)))
}

func (b builder) auxin(p string, source int32) {
	b.node(p+"/config", strip("", 1, true, source)...)
	b.node(p+"/preamp", f("trim", trim), f("invert", OnOff))
	b.eq(p, 4, eqType6)
	b.channelMix(p)
	b.sends(p, 16)
	b.grp(p)
}

func (b builder) fxrtn(p string) {
	b.node(p+"/config", strip("", 1, false, 0)...)
	b.eq(p, 4, eqType6)
	b.channelMix(p)
	b.sends(p, 16)
	b.grp(p)
}

func (b builder) bus(p, name string) {
	b.node(p+"/config", strip(name, 1, false, 0)...)
	b.dyn(p, true)
	b.node(p+"/insert", f("on", OnOff), f("pos", Enum{Names: prePost.Names}), f("sel", insSel))
	b.eq(p, 6, eqType6)
	b.node(p+"/mix", f("on", OnOff), f("fader", fader(negInf)), f("st", OnOff), f("pan", pan), f("mono", OnOff), f("mlevel", level161(negInf)))
	b.sends(p, 6)
	b.grp(p)
}

func (b builder) matrix(p, name string) {
	b.node(p+"/config", strip(name, 1, false, 0)...)
	b.node(p+"/preamp", f("invert", OnOff))
	b.dyn(p, false)
	b.node(p+"/insert", f("on", OnOff), f("pos", Enum{Names: prePost.Names}), f("sel", insSel))
	b.eq(p, 6, eqType14)
	b.node(p+"/mix", f("on", OnOff), f("fader", fader(negInf)))
	b.grp(p)
}

func (b builder) main(p string, stereo bool) {
	b.node(p+"/config", strip("", 1, false, 0)...)
	b.dyn(p, false)
	b.node(p+"/insert", f("on", OnOff), f("pos", Enum{Names: prePost.Names}), f("sel", insSel))
	b.eq(p, 6, eqType14)
	if stereo {
		b.node(p+"/mix", f("on", OnOff), f("fader", fader(negInf)), f("pan", pan))
	} else {
		b.node(p+"/mix", f("on", OnOff), f("fader", fader(negInf)))
	}
	b.sends(p, 6)
	b.grp(p)
}

func (b builder) outputs() {
	src := Int{0, 76, 0}
	for i := 1; i <= 16; i++ {
		p := fmt.Sprintf("/outputs/main/%02d", i)
		b.node(p, f("src", src), f("pos", outPos), f("invert", OnOff))
		b.node(p+"/delay", f("on", OnOff), f("time", delayTime))
	}
	for i := 1; i <= 6; i++ {
		b.node(fmt.Sprintf("/outputs/aux/%02d", i), f("src", src), f("pos", outPos), f("invert", OnOff))
	}
	for i := 1; i <= 16; i++ {
		p := fmt.Sprintf("/outputs/p16/%02d", i)
		b.node(p, f("src", src), f("pos", outPos), f("invert", OnOff))
		b.node(p+"/iQ", f("group", iqGroup), f("speaker", iqSpeaker), f("eq", iqEQ), f("model", Int{0, 7, 0}))
	}
	for i := 1; i <= 2; i++ {
		b.node(fmt.Sprintf("/outputs/aes/%02d", i), f("src", src), f("pos", outPos), f("invert", OnOff))
	}
	for i := 1; i <= 2; i++ {
		b.node(fmt.Sprintf("/outputs/rec/%02d", i), f("src", src), f("pos", outPos))
	}
}
