package schema

var (
	delayFactors  = []string{"1/4", "3/8", "1/2", "2/3", "1", "4/3", "3/2", "2", "3"}
	delayPatterns = []string{"1/4", "1/3", "3/8", "1/2", "2/3", "3/4", "1", "1/4X", "1/3X", "3/8X", "1/2X", "2/3X", "3/4X", "1X"}
)

// Delays, and delays or modulations combined with a reverb.
var fxDelays = map[string][]fxParam{
	"DLY": {
		pct("Mix", 100),
		fxTime(223),
		choice("Mode", []string{"ST", "X", "M"}, 1), // M32-Edit's default; the test scenes hold ST
		choice("Factor L", delayFactors, 4),
		choice("Factor R", delayFactors, 4),
		num("Offset L/R", -100, 100, 201, f0, "ms", 13),
		loCut("Lo Cut", 10),
		hiCut("Hi Cut", 20000),
		loCut("Feed Lo Cut", 97),
		pct("Feed Left", 30),
		pct("Feed Right", 30),
		hiCut("Feed Hi Cut", 20000),
	},
	"3TAP": {
		fxTime(200),
		pct("Gain Base", 100),
		tapPan("Pan Base", 0),
		feedback(30),
		loCut("Lo Cut", 10),
		hiCut("Hi Cut", 20000),
		choice("Factor A", delayFactors, 5),
		pct("Gain A", 50),
		tapPan("Pan A", -100),
		choice("Factor B", delayFactors, 6),
		pct("Gain B", 60),
		tapPan("Pan B", 100),
		toggle("Cross Feed", 0),
		toggle("Mono", 0),
		toggle("Dry", 0),
	},
	"4TAP": {
		fxTime(200),
		pct("Gain Base", 100),
		feedback(30),
		loCut("Lo Cut", 10),
		hiCut("Hi Cut", 20000),
		num("Spread", 0, 6, 7, f0, "", 5),
		choice("Factor A", delayFactors, 5),
		pct("Gain A", 50),
		choice("Factor B", delayFactors, 4),
		pct("Gain B", 50),
		choice("Factor C", delayFactors, 6),
		pct("Gain C", 50),
		toggle("Cross Feed", 0),
		toggle("Mono", 0),
		toggle("Dry", 0),
	},
	"D/RV": cat(
		delayHead(240, 5, 7600, 40, 40, 0),
		chamber(24, 1.43, 52, f0, 5600, 89),
		[]fxParam{pct("Mix", 100)}),
	"D/CR": cat(
		delayHead(201, 6, 9700, 30, 40, 60),
		chorus(1.07, 20, 15.1, 100, 100),
		[]fxParam{pct("Mix", 100)}),
	"D/FL": cat(
		delayHead(185, 6, 9700, 38, 30, 50),
		flanger(0.56, 80, 4.2, 180, 80),
		[]fxParam{pct("Mix", 100)}),
	"CR/R": cat(
		chorus(0.47, 20, 15.1, 180, 100),
		[]fxParam{balance(10)},
		chamber(20, 1.32, 40, f0, 6400, 83),
		[]fxParam{pct("Mix", 100)}),
	"FL/R": cat(
		flanger(0.41, 80, 2.2, 25, 70),
		[]fxParam{balance(-15)},
		chamber(14, 0.97, 24, f1, 7200, 113),
		[]fxParam{pct("Mix", 100)}),
	"MODD": {
		fxTime(300),
		choice("Delay", []string{"1", "1/2", "2/3", "3/2"}, 0),
		feedback(30),
		loCut("Lo Cut", 97),
		hiCut("Hi Cut", 9700),
		num("Depth", 0, 100, 21, f0, "%", 20),
		numLog("Rate", 0.05, 10, 101, f2, "Hz", 1.07),
		choice("Setup", []string{"PAR", "SER"}, 1),
		choice("Type", []string{"AMB", "CLUB", "HALL"}, 1),
		num("Decay", 1, 10, 19, f1, "", 5),
		numLog("Damping", 1000, 20000, 51, k0, "Hz", 5600),
		balance(0),
		pct("Mix", 100),
	},
}

func fxTime(def float64) fxParam   { return num("Time", 1, 3000, 3000, f0, "ms", def) }
func feedback(def float64) fxParam { return num("Feedback", 0, 100, 51, f1, "%", def) }

func tapPan(label string, def float64) fxParam { return num(label, -100, 100, 41, f0, "%", def) }

// delayHead is the delay of the delay / reverb or modulation combinations.
func delayHead(time float64, pattern int32, feedHiCut, fb, crossFeed, bal float64) []fxParam {
	return []fxParam{
		fxTime(time),
		choice("Pattern", delayPatterns, pattern),
		numLog("Feed Hi Cut", 1000, 20000, 51, k0, "Hz", feedHiCut),
		feedback(fb),
		pct("Cross Feed", crossFeed),
		balance(bal),
	}
}

// chamber is the reverb of the combinations; flanger / chamber writes its
// size with one decimal.
func chamber(pre, dec, size float64, sizeFmt func(float64) string, damp, lo float64) []fxParam {
	return []fxParam{
		preDelay(pre),
		decay(0.1, 5, dec),
		num("Size", 2, 100, 50, sizeFmt, "", size),
		numLog("Damping", 1000, 20000, 51, k0, "Hz", damp),
		loCut("Lo Cut", lo),
	}
}

// chorus and flanger are the modulations of the combinations.
func chorus(speed, depth, delay, phase, wave float64) []fxParam {
	return []fxParam{
		lfoSpeed(0.05, 4, speed),
		num("Depth", 0, 100, 21, f0, "%", depth),
		numLog("Delay", 0.5, 50, 51, f1, "ms", delay),
		num("Phase", 0, 180, 37, f0, "", phase),
		num("Wave", 0, 100, 21, f0, "%", wave),
	}
}

func flanger(speed, depth, delay, phase, feed float64) []fxParam {
	return []fxParam{
		lfoSpeed(0.05, 4, speed),
		num("Depth", 0, 100, 21, f0, "%", depth),
		numLog("Delay", 0.5, 20, 51, f1, "ms", delay),
		num("Phase", 0, 180, 37, f0, "", phase),
		num("Feed", -90, 90, 37, f0, "%", feed),
	}
}

// cat concatenates parameter lists.
func cat(lists ...[]fxParam) []fxParam {
	var out []fxParam
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}
