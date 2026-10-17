package schema

import "fmt"

// De-essers, wave designer, limiter, combinator and compressors.
var fxDynamics = map[string][]fxParam{
	"DES": {
		desBand("Lo Band L"),
		desBand("Hi Band L"),
		desBand("Lo Band R"),
		desBand("Hi Band R"),
		choice("Voice", desVoices, 0),
		choice("Mode", []string{"ST", "MS"}, 0),
	},
	"DES2": {
		desBand("Lo Band A"),
		desBand("Hi Band A"),
		desBand("Lo Band B"),
		desBand("Hi Band B"),
		choice("Voice A", desVoices, 0),
		choice("Voice B", desVoices, 0),
	},
	"WAVD": dual(func(ch string) []fxParam {
		return []fxParam{
			num("Attack"+ch, -100, 100, 101, f0, "%", 0),
			num("Sustain"+ch, -100, 100, 101, f0, "%", 0),
			num("Gain"+ch, -24, 24, 97, f1, "dB", 0),
		}
	}),
	"LIM": {
		num("Input Gain", 0, 18, 37, f1, "dB", 0),
		num("Out Gain", -18, 0, 37, f1, "dB", -0.5),
		pct("Squeeze", 0),
		num("Knee", 0, 10, 11, f0, "dB", 3),
		numLog("Attack", 0.05, 1, 51, f2, "ms", 0.05),
		numLog("Release", 20, 2000, 51, f0, "ms", 662),
		toggle("Stereo Link", 1),
		toggle("Auto Gain", 0),
	},
	"CMB":   combinator(""),
	"CMB2":  dual(combinator),
	"FAC":   append([]fxParam{toggle("Active", 1)}, fairChannel("")...),
	"FAC1M": append([]fxParam{toggle("Active", 1)}, append(fairChannel(" M"), fairChannel(" S")...)...),
	"FAC2": dual(func(ch string) []fxParam {
		return append([]fxParam{toggle("Active"+ch, 1)}, fairChannel(ch)...)
	}),
	"LEC":  leisure(""),
	"LEC2": dual(leisure),
	"ULC":  ultimo(""),
	"ULC2": dual(ultimo),
}

var desVoices = []string{"FEM", "MALE"}

func desBand(label string) fxParam { return num(label, 0, 50, 51, f1, "", 0) }

// combinator is a five-band compressor.
func combinator(ch string) []fxParam {
	gain := func(label string) fxParam { return num(label+ch, -10, 10, 41, f1, "dB", 0) }
	ps := []fxParam{
		toggle("Active"+ch, 1),
		choice("Band Solo"+ch, []string{"OFF", "Bd1", "Bd2", "Bd3", "Bd4", "Bd5"}, 0),
		pct("Mix"+ch, 100),
		num("Attack"+ch, 0, 19, 20, f0, "", 5),
		numLog("Release"+ch, 20, 3000, 51, f0, "ms", 500),
		toggle("Auto Release"+ch, 1),
		num("SBC Speed"+ch, 1, 10, 10, f0, "", 5),
		toggle("SBC On"+ch, 1),
		num("Xover"+ch, -50, 50, 51, f0, "", 0),
		choice("Xover Slope"+ch, []string{"12", "48"}, 1),
		choice("Ratio"+ch, []string{"1.1", "1.2", "1.3", "1.5", "1.7", "2", "2.5", "3", "3.5", "4", "5", "7", "10", "LIM"}, 7),
		num("Threshold"+ch, -40, 0, 81, f1, "dB", 0),
		gain("Gain"),
	}
	for b := 1; b <= 5; b++ {
		band := fmt.Sprintf("Band %d ", b)
		ps = append(ps, gain(band+"Threshold"), gain(band+"Gain"), choice(band+"Lock"+ch, []string{"0", "1"}, 0))
	}
	return append(ps, choice("Meter Mode"+ch, []string{"GR", "PEAK", "SBC"}, 0))
}

// fairChannel is a channel of the fair compressors (Fairchild 670), its
// on switch aside.
func fairChannel(ch string) []fxParam {
	return []fxParam{
		num("Input Gain"+ch, -20, 0, 41, f1, "dB", -8),
		num("Threshold"+ch, 0, 10, 101, f1, "", 5),
		num("Time"+ch, 1, 6, 6, f0, "", 2),
		pct("Bias"+ch, 50),
		num("Gain"+ch, -18, 6, 49, f1, "dB", -8.5),
		num("Balance"+ch, -100, 100, 41, f0, "%", 55),
	}
}

// leisure is the leisure compressor (LA-2A).
func leisure(ch string) []fxParam {
	return []fxParam{
		toggle("Active"+ch, 1),
		num("Gain"+ch, 0, 100, 51, f0, "", 44),
		num("Peak"+ch, 0, 100, 51, f0, "", 48),
		choice("Mode"+ch, []string{"COMP", "LIM"}, 0),
		num("Out Gain"+ch, -18, 6, 49, f1, "dB", 1),
	}
}

// ultimo is the ultimo compressor (1176).
func ultimo(ch string) []fxParam {
	return []fxParam{
		toggle("Active"+ch, 1),
		num("Input Gain"+ch, -48, 0, 49, f0, "dB", -24),
		num("Out Gain"+ch, -48, 0, 49, f0, "dB", -23),
		num("Attack"+ch, 1, 7, 61, f1, "", 3.7),
		num("Release"+ch, 1, 7, 61, f1, "", 1.9),
		choice("Ratio"+ch, []string{"4", "8", "12", "20", "ALL"}, 0),
	}
}
