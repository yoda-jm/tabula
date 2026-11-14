package audio

import (
	"math"
	"testing"
)

func TestLevels(t *testing.T) {
	dir, info := playSession(t)
	info.Offset = 1 // the first sample at session time 1 s
	l, err := OpenLevels(dir, info)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if !l.Has(2) || l.Has(3) || !l.Has(4) {
		t.Errorf("tracks: 2 %v 3 %v 4 %v", l.Has(2), l.Has(3), l.Has(4))
	}
	frame := func(n int) float64 { return 1 + float64(n)/48000 }
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-6 }
	// frames 400-499: the peaks are the last samples, (i+1)·step
	got := l.At(frame(500), 100.0/48000)
	if !near(got[2], 500*256.0/(1<<31)) || !near(got[4], 500*65536.0/(1<<31)) {
		t.Errorf("at frame 500: %v", got)
	}
	// past the end of the shorter track, before the first sample
	if got = l.At(frame(1000), 100.0/48000); got[4] != 0 || got[2] == 0 {
		t.Errorf("at frame 1000: %v", got)
	}
	if got = l.At(0.5, 0.05); got[2] != 0 || got[4] != 0 {
		t.Errorf("before the audio: %v", got)
	}
	if _, err := OpenLevels(t.TempDir(), info); err == nil {
		t.Error("no files: no error")
	}
}
