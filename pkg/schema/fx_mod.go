package schema

// Modulations, filters and pitch shifters.
var fxMods = map[string][]fxParam{
	"CRS": {
		lfoSpeed(0.05, 5, 0.48),
		num("Depth L", 0, 100, 21, f0, "%", 20),
		num("Depth R", 0, 100, 21, f0, "%", 20),
		numLog("Delay L", 0.5, 50, 51, f1, "ms", 15.1),
		numLog("Delay R", 0.5, 50, 51, f1, "ms", 16.6),
		pct("Mix", 100),
		loCut("Lo Cut", 83),
		hiCut("Hi Cut", 10400),
		num("Phase", 0, 180, 37, f0, "", 120),
		num("Wave", 0, 100, 21, f0, "%", 100),
		num("Spread", 0, 100, 21, f0, "%", 100),
	},
	"FLNG": {
		lfoSpeed(0.05, 5, 0.32),
		num("Depth L", 0, 100, 21, f0, "%", 80),
		num("Depth R", 0, 100, 21, f0, "%", 80),
		numLog("Delay L", 0.5, 20, 51, f1, "ms", 4.2),
		numLog("Delay R", 0.5, 20, 51, f1, "ms", 4.2),
		pct("Mix", 100),
		loCut("Lo Cut", 83),
		hiCut("Hi Cut", 15100),
		num("Phase", 0, 180, 37, f0, "", 180),
		loCut("Feed Lo Cut", 122),
		hiCut("Feed Hi Cut", 11500),
		num("Feed", -90, 90, 37, f0, "%", 70),
	},
	"PHAS": {
		lfoSpeed(0.05, 5, 1.44),
		pct("Depth", 54),
		num("Resonance", 0, 80, 41, f0, "%", 42),
		num("Base", 0, 50, 26, f0, "", 28),
		num("Stages", 2, 12, 11, f0, "", 4),
		pct("Mix", 70),
		num("Wave", -50, 50, 21, f0, "", -25),
		num("Phase", 0, 180, 37, f0, "", 0),
		num("Env Modulation", -100, 100, 41, f0, "%", 0),
		envAttack(1000, 21),
		envHold(52),
		envRelease(1000, 209),
	},
	"DIMC": {
		toggle("Active", 1),
		choice("Mode", []string{"M", "ST"}, 1),
		toggle("Dry", 1),
		toggle("Mode 1", 1),
		toggle("Mode 2", 0),
		toggle("Mode 3", 1),
		toggle("Mode 4", 0),
	},
	"FILT": {
		numLog("Speed", 0.05, 20, 151, f2, "Hz", 0.2),
		pct("Depth", 0),
		num("Resonance", 0, 100, 21, f0, "%", 70),
		numLog("Base", 20, 15000, 101, k1, "Hz", 104.7),
		choice("Mode", []string{"LP", "HP", "BP", "NO"}, 0),
		pct("Mix", 100),
		choice("Wave", []string{"TRI", "SIN", "SAW", "SAW-", "RMP", "SQU", "RND"}, 0),
		num("Phase", 0, 180, 37, f0, "", 90),
		num("Env Modulation", -100, 100, 101, f0, "%", 64),
		envAttack(250, 12),
		envRelease(500, 143),
		pct("Drive", 60),
		// Factory presets from older firmware write OFF/ON.
		fxParam{"4 Pole", Enum{Names: []string{"2POL", "4POL"}, Def: 1, Aliases: map[string]int32{"OFF": 0, "ON": 1}}},
		toggle("Side Chain", 0),
	},
	"ROTA": {
		numLog("Lo Speed", 0.1, 4, 101, f2, "Hz", 0.66),
		numLog("Hi Speed", 2, 10, 101, f2, "Hz", 5.01),
		pct("Accelerate", 38),
		pct("Distance", 0),
		num("Balance", -100, 100, 41, f0, "%", -15),
		pct("Mix", 100),
		choice("Stop", []string{"RUN", "STOP"}, 0),
		choice("Slow", []string{"SLOW", "FAST"}, 0),
	},
	"PAN": {
		lfoSpeed(0.05, 5, 1.44),
		num("Phase", 0, 180, 37, f0, "", 180),
		num("Wave", -50, 50, 21, f0, "", 0),
		num("Depth", 0, 100, 21, f0, "%", 100),
		num("Env Speed", 0, 100, 21, f0, "%", 0),
		num("Env Depth", 0, 100, 21, f0, "%", 0),
		envAttack(1000, 21),
		envHold(52),
		envRelease(1000, 209),
	},
	"SUB": dual(func(ch string) []fxParam {
		return []fxParam{
			toggle("Active"+ch, 1),
			choice("Range"+ch, []string{"LO", "MID", "HI"}, 0),
			pct("Dry"+ch, 100),
			pct("Octave -1"+ch, 30),
			pct("Octave -2"+ch, 0),
		}
	}),
	"PIT": {
		semitone("Semitone", 0),
		cent("Cent", 0),
		pitchDelay("Delay", 5),
		loCut("Lo Cut", 50),
		pitchHiCut(16000),
		pct("Mix", 100),
	},
	"PIT2": {
		semitone("Semitone 1", 0),
		cent("Cent 1", -10),
		pitchDelay("Delay 1", 5),
		pct("Gain 1", 100),
		tapPan("Pan 1", -50),
		pct("Mix", 100),
		semitone("Semitone 2", 0),
		cent("Cent 2", 10),
		pitchDelay("Delay 2", 7),
		pct("Gain 2", 100),
		tapPan("Pan 2", 50),
		pitchHiCut(16000),
	},
}

// Envelope times of the phaser, the tremolo / panner and the mood filter.
func envAttack(hi, def float64) fxParam  { return numLog("Attack", 10, hi, 51, f0, "ms", def) }
func envHold(def float64) fxParam        { return numLog("Hold", 1, 2000, 51, f0, "ms", def) }
func envRelease(hi, def float64) fxParam { return numLog("Release", 10, hi, 51, f0, "ms", def) }

func semitone(label string, def float64) fxParam { return num(label, -12, 12, 25, f0, "", def) }
func cent(label string, def float64) fxParam     { return num(label, -50, 50, 101, f0, "", def) }
func pitchDelay(label string, def float64) fxParam {
	return numLog(label, 1, 500, 101, f1, "ms", def)
}
func pitchHiCut(def float64) fxParam { return numLog("Hi Cut", 2000, 20000, 51, k0, "Hz", def) }
