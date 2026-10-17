package schema

import "strings"

// Graphic and program equalizers.
var fxEQs = map[string][]fxParam{
	"GEQ":  geq(""),
	"TEQ":  geq(""),
	"GEQ2": dual(geq),
	"TEQ2": dual(geq),
	"P1A":  p1a(""),
	"P1A2": dual(p1a),
	"PQ5":  pq5("", 1, 5, 3, 1),
	"PQ5S": dual(func(ch string) []fxParam { return pq5(ch, 0, 0, 0, 0) }),
}

// geqBands are the 31 bands of the graphic equalizers.
var geqBands = []string{"20", "25", "31.5", "40", "50", "63", "80", "100", "125", "160", "200",
	"250", "315", "400", "500", "630", "800", "1k", "1k25", "1k6", "2k", "2k5", "3k15", "4k", "5k",
	"6k3", "8k", "10k", "12k5", "16k", "20k"}

// geq is a graphic equalizer: the gain of 31 bands, then a master level.
func geq(ch string) []fxParam {
	var ps []fxParam
	for _, b := range geqBands {
		label := b + " Hz"
		if k, frac, ok := strings.Cut(b, "k"); ok {
			label = k + strings.TrimSuffix("."+frac, ".") + " kHz"
		}
		ps = append(ps, num(label+ch, -15, 15, 61, f1, "dB", 0))
	}
	return append(ps, num("Master"+ch, -15, 15, 61, f1, "dB", 0))
}

// p1a is a program equalizer (Pultec EQP-1A).
func p1a(ch string) []fxParam {
	knob := func(label string, def float64) fxParam { return num(label+ch, 0, 10, 101, f1, "", def) }
	return []fxParam{
		toggle("Active"+ch, 1),
		num("Gain"+ch, -12, 12, 49, f1, "dB", 0),
		knob("Lo Boost", 0),
		choice("Lo Freq"+ch, []string{"20", "30", "60", "100"}, 0),
		knob("Lo Atten", 0),
		knob("Hi Width", 0.6),
		knob("Hi Boost", 0),
		choice("Hi Freq"+ch, []string{"3k", "4k", "5k", "8k", "10k", "12k", "16k"}, 5),
		knob("Hi Atten", 0),
		choice("Hi Atten Freq"+ch, []string{"5k", "10k", "20k"}, 2),
		toggle("Transformer"+ch, 1),
	}
}

// pq5 is a midrange equalizer (Pultec MEQ-5).
func pq5(ch string, on, midFreq, hiFreq, transformer int32) []fxParam {
	knob := func(label string) fxParam { return num(label+ch, 0, 10, 101, f1, "", 0) }
	return []fxParam{
		toggle("Active"+ch, on),
		num("Gain"+ch, -12, 12, 49, f1, "dB", 0),
		choice("Lo Freq"+ch, []string{"200", "300", "500", "700", "1000"}, 0),
		knob("Lo Boost"),
		choice("Mid Freq"+ch, []string{"200", "300", "500", "700", "1k", "1k5", "2k", "3k", "4k", "5k", "7k"}, midFreq),
		knob("Mid Cut"),
		choice("Hi Freq"+ch, []string{"1k5", "2k", "3k", "4k", "5k"}, hiFreq),
		knob("Hi Boost"),
		toggle("Transformer"+ch, transformer),
	}
}
