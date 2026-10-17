package schema

// Enhancers, exciters, imagers, guitar amps and tube stages.
var fxTones = map[string][]fxParam{
	"ENH":  enhancer("", []fxParam{pct("Spread", 22)}, 16, 11, 16, 25, 12, 40),
	"ENH2": dual(func(ch string) []fxParam { return enhancer(ch, nil, 14, 20, 12, 38, 16, 50) }),
	"EXC":  exciter("", 3310, 28, 24, 14, 30),
	"EXC2": dual(func(ch string) []fxParam { return exciter(ch, 7000, 0, 0, 0, 0) }),
	"IMG": {
		num("Balance", -100, 100, 101, f0, "%", 0),
		num("Mono Pan", -100, 100, 101, f0, "%", 0),
		num("Stereo Pan", -100, 100, 101, f0, "%", 0),
		num("Shv Gain", 0, 12, 25, f1, "dB", 0),
		numLog("Shv Freq", 100, 1000, 25, f0, "Hz", 250),
		numLog("Shv Q", 1, 10, 25, f2, "", 2),
		num("Out Gain", -12, 12, 49, f1, "dB", 0),
	},
	"EDI": {
		toggle("Active", 1),
		choice("Stereo Input", ediModes, 0),
		choice("Stereo Output", ediModes, 0),
		num("ST Spread", -50, 50, 51, f0, "", 0),
		num("LMF Spread", -50, 50, 51, f0, "", 0),
		num("Balance", -50, 50, 51, f0, "", 0),
		num("Center Distance", -50, 50, 51, f0, "", 0),
		num("Out Gain", -12, 12, 49, f1, "dB", 0),
	},
	"SON": dual(func(ch string) []fxParam {
		return []fxParam{
			toggle("Active"+ch, 1),
			num("Lo Contour"+ch, 0, 10, 51, f1, "", 0.6),
			num("Process"+ch, 0, 10, 51, f1, "", 0.8),
			num("Out Gain"+ch, -12, 12, 49, f1, "dB", 0),
		}
	}),
	"AMP":  guitarAmp(""),
	"AMP2": dual(guitarAmp),
	"DRV":  tubeStage("", 25, 20, 28, -6.5, 55, 18700, 154, 4100),
	"DRV2": dual(func(ch string) []fxParam { return tubeStage(ch, 50, 20, 20, 0, 100, 7000, 200, 3000) }),
}

var ediModes = []string{"ST", "M/S"}

// enhancer is a channel of the enhancers; the stereo one has a spread after
// the out gain.
func enhancer(ch string, spread []fxParam, bassGain, bassFreq, midGain, midQ, hiGain, hiFreq float64) []fxParam {
	return cat([]fxParam{num("Out Gain"+ch, -12, 12, 49, f1, "dB", 0)}, spread, []fxParam{
		pct("Bass Gain"+ch, bassGain),
		num("Bass Freq"+ch, 1, 50, 50, f0, "", bassFreq),
		pct("Mid Gain"+ch, midGain),
		num("Mid Q"+ch, 1, 50, 50, f0, "", midQ),
		pct("Hi Gain"+ch, hiGain),
		num("Hi Freq"+ch, 1, 50, 50, f0, "", hiFreq),
		toggle("Solo"+ch, 0),
	})
}

func exciter(ch string, tune, peak, zeroFill, timbre, harmonics float64) []fxParam {
	return []fxParam{
		numLog("Tune"+ch, 1000, 10000, 51, k1, "Hz", tune),
		pct("Peak"+ch, peak),
		pct("Zero Fill"+ch, zeroFill),
		num("Timbre"+ch, -50, 50, 51, f0, "", timbre),
		pct("Harmonics"+ch, harmonics),
		pct("Mix"+ch, 30),
		toggle("Solo"+ch, 0),
	}
}

func guitarAmp(ch string) []fxParam {
	var ps []fxParam
	for _, k := range []string{"Preamp", "Buzz", "Punch", "Crunch", "Drive", "Level", "Low", "High"} {
		ps = append(ps, num(k+ch, 0, 10, 41, f1, "", 5))
	}
	return append(ps, toggle("Cabinet"+ch, 1))
}

func tubeStage(ch string, drive, even, odd, gain, loCut, hiCut, loFreq, hiFreq float64) []fxParam {
	return []fxParam{
		num("Drive"+ch, 0, 100, 21, f0, "%", drive),
		num("Even Har"+ch, 0, 50, 26, f0, "%", even),
		num("Odd Har"+ch, 0, 50, 26, f0, "%", odd),
		num("Gain"+ch, -12, 12, 49, f1, "dB", gain),
		numLog("Lo Cut"+ch, 20, 200, 51, f0, "Hz", loCut),
		numLog("Hi Cut"+ch, 4000, 20000, 51, k0, "Hz", hiCut),
		num("Lo Gain"+ch, -12, 12, 49, f1, "dB", 0),
		numLog("Lo Freq"+ch, 50, 400, 51, f0, "Hz", loFreq),
		num("Hi Gain"+ch, -12, 12, 49, f1, "dB", 0),
		numLog("Hi Freq"+ch, 1000, 10000, 51, k0, "Hz", hiFreq),
	}
}
