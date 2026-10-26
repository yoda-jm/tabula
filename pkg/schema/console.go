package schema

import (
	"fmt"
	"math"
)

// Console nodes are not written in scene files: preferences, status, show
// and preset listings, USB and X-Live! state, actions. Field order and text
// formats follow a real M32C (M32-Edit's cache) where known, then the
// protocol document, then Maillot's emulator (X32PrefStat.h, X32.c).

var (
	barSpec = Enum{Names: []string{"BAR", "SPEC"}}
	sdState = Enum{Names: []string{"NONE", "READY", "PROTECT", "ERROR"}}
	madi    = []string{"1-32", "9-40", "17-48", "25-56", "33-64"}
	msTime  = Int{0, 86399999, 0} // X-Live! times, up to 24 h in ms
	flag    = Int{0, 1, 0}

	// Ch01…Ch32, Aux1…Aux8, Fx1L…Fx4R, Bus1…Bs16, Mtx1…Mtx6, LR, M/C. The
	// document elides the bus names; "Bs10" is from Maillot's emulator.
	selIdx = Enum{Names: func() []string {
		var n []string
		for i := 1; i <= 32; i++ {
			n = append(n, fmt.Sprintf("Ch%02d", i))
		}
		for i := 1; i <= 8; i++ {
			n = append(n, fmt.Sprintf("Aux%d", i))
		}
		for i := 1; i <= 4; i++ {
			n = append(n, fmt.Sprintf("Fx%dL", i), fmt.Sprintf("Fx%dR", i))
		}
		for i := 1; i <= 16; i++ {
			if i < 10 {
				n = append(n, fmt.Sprintf("Bus%d", i))
			} else {
				n = append(n, fmt.Sprintf("Bs%d", i))
			}
		}
		for i := 1; i <= 6; i++ {
			n = append(n, fmt.Sprintf("Mtx%d", i))
		}
		return append(n, "LR", "M/C")
	}()}
)

// status adds a node that is not part of scene files.
func (b builder) status(path string, fields ...Field) {
	b.s.add(&Node{Path: path, Fields: fields})
}

// with returns e with another default.
func with(e Enum, def int32) Enum {
	e.Def = def
	return e
}

// anyInt takes every int32, for values whose range is not documented.
func anyInt(def int32) Int { return Int{math.MinInt32, math.MaxInt32, def} }

// from0 makes a field name function count from 0.
func from0(name func(int) string) func(int) string {
	return func(i int) string { return name(i - 1) }
}

func (b builder) console() {
	b.headampIndex()
	b.inserts()
	b.show()
	b.libs()
	b.prefs()
	b.usb()
	b.stat()
	b.actions()
	b.urec()
}

// headampIndex adds /-ha/00…39/index, the headamp feeding channel 1-32 or
// aux in 1-8, -1 for none; computed by the console from the input routing.
func (b builder) headampIndex() {
	for i := int32(0); i < 40; i++ {
		def := i
		if i >= 32 {
			def = -1 // aux ins have no headamp
		}
		b.status(fmt.Sprintf("/-ha/%02d", i), f("index", Int{-1, 127, def}))
	}
}

// inserts adds /-insert, the channel each FX or aux insert is used by, in
// the order fx1L, fx1R … fx8R, aux1 … aux6; computed by the console.
// -1 for an unused insert is a guess.
func (b builder) inserts() {
	ins := Int{-1, 71, -1}
	var fs []Field
	for i := 1; i <= 8; i++ {
		fs = append(fs, f(fmt.Sprintf("fx%dL", i), ins), f(fmt.Sprintf("fx%dR", i), ins))
	}
	fs = append(fs, fields(6, func(i int) string { return fmt.Sprint("aux", i) }, ins)...)
	b.status("/-insert", fs...)
}

func (b builder) show() {
	// The ten ints are documented as scene safe bitmaps, but they change
	// as cues are added in the document's /showdump example: range left open.
	sh := []Field{f("name", Str{})}
	for _, n := range []string{"inputs", "mxsends", "mxbuses", "console", "chan16", "chan32", "return", "buses", "lrmtxdca", "effects"} {
		sh = append(sh, f(n, anyInt(0)))
	}
	// Not documented: the firmware version that wrote the show ("2.08" in
	// the document's examples).
	sh = append(sh, f("version", Str{Def: "4.06"}))
	b.status("/-show/showfile/show", sh...)

	// 100 cues as in the document's text and appendix; its table header and
	// Maillot's emulator go up to cue/499.
	for i := 0; i < 100; i++ {
		b.status(fmt.Sprintf("/-show/showfile/cue/%03d", i),
			f("numb", Int{0, 50000, 0}), // 10327 is cue 103.2.7
			f("name", Str{}), f("skip", flag),
			f("scene", Int{-1, 99, -1}), f("bit", Int{-1, 99, -1}), // bit is the snippet
			f("miditype", Int{0, 3, 0}), // none, program change, control change, note
			f("midichan", Int{0, 16, 1}), f("midipara1", Int{0, 127, 0}), f("midipara2", Int{0, 127, 0}))
	}
	for i := 0; i < 100; i++ {
		b.status(fmt.Sprintf("/-show/showfile/scene/%03d", i),
			f("name", Str{}), f("notes", Str{}), f("safes", Bitmap{9}), f("hasdata", flag))
	}
	// Snippet filters are bitmaps, written as decimal ints.
	for i := 0; i < 100; i++ {
		b.status(fmt.Sprintf("/-show/showfile/snippet/%03d", i),
			f("name", Str{}), f("eventtyp", Int{0, 1<<25 - 1, 0}),
			f("channels", anyInt(0)), f("auxbuses", anyInt(0)), // 32 bits
			f("maingrps", Int{0, 1<<16 - 1, 0}), f("hasdata", flag))
	}
	// Line highlighted on the scenes page (cues, scenes or snippets as set
	// by /-prefs/show_control), -1 for none.
	b.status("/-show/prepos", f("current", Int{-1, 99, -1}))
}

// libs adds the preset listings: channel, effect, routing and (FW 4.0+)
// AES50/DP48 presets. Slots are empty: the emulator has no preset contents.
// Type 3 for the AES50/DP48 presets is a guess.
func (b builder) libs() {
	for t, lib := range []string{"ch", "fx", "r", "mon"} {
		for i := 1; i <= 100; i++ {
			b.status(fmt.Sprintf("/-libs/%s/%03d", lib, i),
				f("pos", Int{0, 100, int32(i)}), f("name", Str{}), f("type", Int{0, 3, int32(t)}),
				f("flags", Bitmap{16}), f("hasdata", flag))
		}
	}
}

func (b builder) prefs() {
	whole := fixed(0, false, 0)
	b.status("/-prefs",
		f("style", Str{}), // "ablesque" after a factory reset
		f("bright", lin(10, 100, 5, whole, "", 10)),
		f("lcdcont", lin(0, 100, 2, whole, "", 0)),
		f("ledbright", lin(10, 100, 5, whole, "", 10)),
		f("lamp", lin(10, 100, 10, whole, "", 10)),
		f("lampon", OnOff),
		f("clockrate", Enum{Names: []string{"48K", "44K1"}}),
		// The document calls the last source "Exp. Card"; its token is a guess.
		f("clocksource", Enum{Names: []string{"INT", "AES50A", "AES50B", "CARD"}}),
		f("confirm_general", with(OnOff, 1)), f("confirm_overwrite", with(OnOff, 1)),
		f("confirm_sceneload", with(OnOff, 1)),
		f("viewrtn", OnOff), f("selfollowsbank", OnOff), f("scene_advance", OnOff),
		f("safe_masterlevels", OnOff), f("haflags", Bitmap{4}), f("autosel", OnOff),
		f("show_control", Enum{Names: []string{"CUES", "SCENES", "SNIPPETS"}}),
		f("clockmode", Enum{Names: []string{"24h", "12h"}}),
		f("hardmute", OnOff), f("dcamute", OnOff),
		f("invertmutes", Enum{Names: []string{"NORM", "INV"}}),
		f("name", Str{Def: "tabula-emu"}), // network name, as in /xinfo
		f("rec_control", Enum{Names: []string{"USB", "XLIVE"}}))
	// /-prefs/fastFaders (documented for the M32R) is not in the M32C's node.

	b.status("/-prefs/ip", f("dhcp", OnOff))
	for _, p := range []string{"addr", "mask", "gateway"} {
		b.status("/-prefs/ip/"+p, fields(4, from0(plain), Int{0, 255, 0})...)
	}
	b.status("/-prefs/remote", f("enable", OnOff),
		f("protocol", Enum{Names: []string{"MC", "HUI", "CC"}}),
		f("port", Enum{Names: []string{"MIDI", "CARD", "RTP"}}),
		f("ioenable", Bitmap{14}))
	// The document lists URECsdsel first; the console writes it last.
	b.status("/-prefs/card",
		f("UFifc", Enum{Names: []string{"FW", "USB"}, Def: 1}),
		f("UFmode", Enum{Names: []string{"32/32", "16/16", "32/8", "8/32"}}),
		f("USBmode", Enum{Names: []string{"32/32", "16/16", "32/8", "8/32", "8/8", "2/2"}}),
		f("ADATwc", Enum{Names: []string{"IN", "OUT"}}),
		f("ADATsync", Enum{Names: []string{"WC", "ADAT1", "ADAT2", "ADAT3", "ADAT4"}}),
		f("MADImode", Enum{Names: []string{"56", "64"}, Def: 1}),
		f("MADIin", Enum{Names: madi}),
		f("MADIout", Enum{Names: append([]string{"OFF"}, madi...), Def: 1}),
		f("MADIsrc", Enum{Names: []string{"OFF", "OPT", "COAX", "BOTH"}, Def: 3}),
		f("URECtracks", Enum{Names: []string{"32Ch", "16Ch", "8Ch"}}),
		f("URECplayb", Enum{Names: []string{"SD", "USB"}, Def: 1}),
		f("URECrout", Enum{Names: []string{"REC", "PLAY", "AUTO"}, Def: 2}),
		f("URECsdsel", Enum{Names: []string{"SD1", "SD2"}}))
	b.status("/-prefs/rta",
		f("visibility", Enum{Names: []string{"OFF", "25%", "30%", "35%", "40%", "45%", "50%",
			"55%", "60%", "65%", "70%", "75%", "80%"}, Def: 10}),
		f("gain", lin(0, 60, 6, whole, "dB", 18)),
		f("autogain", with(OnOff, 1)),
		f("source", Int{0, 73, 1}), // 0 none, 1 monitor, 2-33 ch… (see /-stat/rtasource)
		f("pos", prePost),
		f("mode", barSpec),
		f("options", Bitmap{6}),
		f("det", Enum{Names: []string{"RMS", "PEAK"}, Def: 1}),
		f("decay", logf(0.25, 16, 19, fixed(2, false, 0), "s", 1)),
		f("peakhold", Enum{Names: []string{"OFF", "1", "2", "3", "4", "5", "6", "7", "8"}}),
		// Four more values written by the M32C, not in the document; names
		// and ranges from M32-Edit. It shows the RTA behind the channel EQ
		// when rtaoneq is set, and draws a 60 dB range with gain30 instead of
		// a 30 dB range with gain when range is ON.
		f("rtaoneq", Int{0, 1, 1}), f("rtaongeq", Int{0, 7, 1}), f("range", OnOff),
		f("gain30", lin(0, 30, 3, whole, "dB", 30)))
	// Maillot's emulator spells IQeqset and IQsound; the document's names are kept.
	for i := 1; i <= 16; i++ {
		b.status(fmt.Sprintf("/-prefs/iQ/%02d", i), f("iQmodel", iqSpeaker), f("iQeqset", iqEQ), f("iQsound", Int{0, 7, 0}))
	}
	// Keyboard layout, then the keyboard history (FW 4.0+).
	b.status("/-prefs/key", append([]Field{f("layout", Enum{Names: []string{"QWERTY", "QWERTZ", "AZERTY", "ABCDEF"}})},
		fields(100, from0(two), Str{Max: 12})...)...)
}

// usb adds the USB stick browser, maintained by the console: the current
// directory and its entries 001 to maxpos (up to 999), "[..]" and "[name]"
// for directories. The entries are lazy: read when the stick is browsed.
func (b builder) usb() {
	b.status("/-usb", f("path", Str{}), f("title", Str{}))
	b.status("/-usb/dir", f("dirpos", Int{0, 999, 0}), f("maxpos", Int{0, 999, 0}))
	for i := 1; i <= 999; i++ {
		b.s.add(&Node{Path: fmt.Sprintf("/-usb/dir/%03d", i), Fields: []Field{f("type", Str{}), f("name", Str{})}, Lazy: true})
	}
}

func (b builder) stat() {
	b.status("/-stat",
		f("selidx", selIdx),
		f("chfaderbank", Int{0, 3, 0}), f("grpfaderbank", Int{0, 5, 0}),
		f("sendsonfader", OnOff), f("bussendbank", Int{0, 3, 0}), f("eqband", Int{0, 5, 0}),
		f("solo", OnOff), // computed by the console: some solo is on
		f("keysolo", OnOff), f("userbank", Int{0, 2, 0}), f("autosave", OnOff),
		// Unlocked, locked, shutdown; Maillot's emulator writes it as OFF/ON.
		f("lock", Int{0, 2, 0}),
		f("usbmounted", OnOff), f("remote", OnOff),
		f("rtamodeeq", barSpec), f("rtamodegeq", barSpec), f("rtaeqpre", OnOff), f("rtageqpost", OnOff),
		// RTA source, set with /-action/setrtasrc: 0-31 ch, 32-39 aux, 40-47
		// FX returns, 48-63 buses, 64-69 matrices, 70 L/R, 71 M/C, 72
		// monitor, 73 talkback, 74-89 dual GEQ A/B and 90-97 stereo GEQ of FX
		// slots 1-8 (M32-Edit's list), pre EQ; 98 more for post EQ.
		f("rtasource", Int{0, 195, 0}),
		f("xcardtype", Int{0, 11, 6}), // computed by the console; DN32-USB is a guess matching /-prefs/card
		f("xcardsync", OnOff), f("geqonfdr", OnOff),
		f("geqpos", Int{0, 0x8ff, 0}), // FX slot << 8 | first band shown
		f("dcaspill", Int{0, 8, 0}))

	// Screen names as in the document; Maillot's emulator uses the page node
	// names (CHAN, ROUTE, LIB, FX, MON, SCENE).
	b.status("/-stat/screen",
		f("screen", Enum{Names: []string{"HOME", "METERS", "ROUTING", "SETUP", "LIBRARY", "EFFECTS",
			"MONITOR", "USB", "SCENES", "ASSIGN", "LOCK"}}),
		f("mutegrp", OnOff), f("utils", OnOff))
	for _, s := range []struct {
		screen string
		pages  []string
	}{
		{"CHAN", []string{"HOME", "CONFIG", "GATE", "DYN", "EQ", "MIX", "MAIN"}},
		{"METER", []string{"CHANNEL", "MIXBUS", "AUX/FX", "IN/OUT", "RTA", "AMIX"}},
		// The document repeats AES50A for page 2; AES50B as in Maillot's emulator.
		{"ROUTE", []string{"HOME", "AES50A", "AES50B", "CARDOUT", "XLROUT", "ANAOUT", "AUXOUT", "P16OUT", "USER"}},
		{"SETUP", []string{"GLOB", "CONF", "REMOTE", "NETW", "NAMES", "PREAMPS", "CARD"}},
		{"LIB", []string{"CHAN", "EFFECT", "ROUTE", "MONITOR"}},
		{"FX", []string{"HOME", "FX1", "FX2", "FX3", "FX4", "FX5", "FX6", "FX7", "FX8"}},
		{"MON", []string{"MONITOR", "TALKA", "TALKB", "OSC"}},
		{"USB", []string{"HOME", "CONFIG"}},
		{"SCENE", []string{"HOME", "SCENES", "BITS", "PARSAFE", "CHNSAFE", "MIDI"}},
		{"ASSIGN", []string{"HOME", "SETA", "SETB", "SETC"}},
	} {
		b.status("/-stat/screen/"+s.screen, f("page", Enum{Names: s.pages}))
	}

	// 01-32 ch, 33-40 aux in, 41-48 FX returns, 49-64 buses, 65-70
	// matrices, 71 LR, 72 M/C, 73-80 DCAs.
	b.status("/-stat/solosw", fields(80, two, OnOff)...)
	// Detected AES50 devices; field order from Maillot's emulator.
	b.status("/-stat/aes50", f("A", Str{}), f("B", Str{}), f("state", Bitmap{5}))
	// State names from Maillot's emulator (the document gives no tokens).
	b.status("/-stat/tape",
		f("state", Enum{Names: []string{"STOP", "PPAUSE", "PLAY", "RPAUSE", "RECORD", "FF", "REW"}}),
		f("file", Str{}), f("etime", Int{0, math.MaxInt32, 0}),
		f("rtime", anyInt(0))) // -(dropouts + 1) while recording
	b.status("/-stat/osc", f("on", OnOff))
	b.status("/-stat/talk", f("A", OnOff), f("B", OnOff))
	// User controls set to MIDI: 01-24 buttons (0 or 127), 25-36 encoders.
	for i := 1; i <= 36; i++ {
		b.status(fmt.Sprintf("/-stat/userpar/%02d", i), f("value", Int{0, 127, 0}))
	}
	b.status("/-stat/urec",
		f("state", Enum{Names: []string{"STOP", "PPAUSE", "PLAY", "REC"}}),
		f("etime", msTime), f("rtime", msTime))
}

// actions adds /-action, commands to the console, and /-undo. The emulator
// only stores them for now.
func (b builder) actions() {
	idx := Int{0, 99, 0}
	slot := Int{0, 100, 0} // X-Live! sessions and markers 1-100, the console answers 0
	b.status("/-action",
		f("setip", flag), f("setclock", Str{}),
		f("initall", flag), f("initlib", flag), f("initshow", flag), f("savestate", flag),
		f("undopt", flag), f("doundo", flag),
		f("playtrack", Int{-1, 1, 0}), // platrack in Maillot's emulator
		f("newscreen", anyInt(0)), f("clearsolo", flag), f("setprebus", anyInt(0)),
		f("setsrate", flag), f("setrtasrc", Int{0, 195, 0}), f("recselect", anyInt(0)),
		f("gocue", idx), f("goscene", idx), f("gosnippet", idx),
		f("selsession", slot), f("delsession", slot), f("selmarker", slot),
		f("delmarker", slot), f("savemarker", slot), f("addmarker", flag),
		f("setposition", msTime), // selposition in the document's appendix and Maillot's emulator
		f("clearalert", flag), f("formatcard", flag))
	// Time of the undo checkpoint, e.g. "18:36:54"; empty when there is none.
	b.status("/-undo", f("time", Str{}))
}

// urec adds the X-Live! card state, reported by the card (no card here).
func (b builder) urec() {
	b.status("/-urec",
		f("sessionmax", Int{0, 100, 0}), f("markermax", Int{0, 100, 0}),
		f("sessionlen", msTime), f("sessionpos", Int{0, 100, 0}), f("markerpos", Int{0, 100, 0}),
		f("batterystate", Enum{Names: []string{"NONE", "GOOD", "LOW"}}),
		f("srate", flag), // 44.1 kHz, 48 kHz
		f("tracks", Int{0, 32, 0}),
		f("sessionspan", Int{0, 3, 0}), f("sessionoffs", msTime),
		f("sd1state", sdState), f("sd2state", sdState),
		f("sd1info", Str{Max: 32}), f("sd2info", Str{Max: 32}),
		f("errormessage", Str{}), f("errorcode", Int{0, 23, 0}))
	for i := 1; i <= 100; i++ {
		b.status(fmt.Sprintf("/-urec/session/%03d", i), f("name", Str{Max: 19}))
	}
	for i := 1; i <= 100; i++ {
		b.status(fmt.Sprintf("/-urec/marker/%03d", i), f("time", msTime))
	}
}
