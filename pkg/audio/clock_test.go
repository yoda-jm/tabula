package audio

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

// Samples arrive in 10 ms periods, each read late by 1 to 30 ms; the audio
// clock runs 25 ppm slow. The estimate finds the start within a
// millisecond and the drift within a ppm.
func TestClock(t *testing.T) {
	ref := time.Now()
	const start, drift, rate = 0.350, 25e-6, 48000
	c := newClock(rate, ref)
	rnd := rand.New(rand.NewSource(1))
	if _, ok := c.offset(); ok {
		t.Fatal("offset before any sample")
	}
	for frames := int64(480); frames <= 600*rate; frames += 480 {
		captured := start + float64(frames)/rate*(1+drift)
		late := 0.001 + 0.029*rnd.Float64()*rnd.Float64()
		c.observe(ref.Add(time.Duration((captured+late)*float64(time.Second))), frames)
	}
	off, ok := c.offset()
	if !ok || math.Abs(off-start-0.001) > 0.001 {
		t.Errorf("offset %.4f, want %.4f (plus the 1 ms floor)", off, start)
	}
	d, ok := c.drift()
	if !ok || math.Abs(d-drift) > 1e-6 {
		t.Errorf("drift %.2f ppm, want %.2f", d*1e6, drift*1e6)
	}
	info := Info{Offset: off, DriftPPM: d * 1e6}
	// A change made when the 300th second of audio was captured.
	at := start + 300*(1+drift)
	if got := info.AudioTime(at); math.Abs(got-300) > 0.002 {
		t.Errorf("audio time %.4f, want 300", got)
	}
}

func TestClockShort(t *testing.T) {
	c := newClock(48000, time.Now())
	c.observe(time.Now(), 480)
	if _, ok := c.drift(); ok {
		t.Error("drift from one read")
	}
}
