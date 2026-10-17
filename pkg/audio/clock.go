package audio

import (
	"math"
	"time"
)

// clock estimates when the first sample of a stream was captured, and how
// fast the audio clock runs against the system clock, from the arrival
// times of the samples.
//
// A read at time w bringing the count of frames received to n says that
// the first frame was captured at w − n/rate, plus the latency of that read
// (the recorder's period, the pipe, scheduling). The smallest of these over
// the first seconds is the best estimate of the start: it is off by the
// latency no read can avoid (USB transfer, driver), a few milliseconds.
//
// The audio clock is the console's: against the system clock, which times
// the session's changes, it drifts by some parts per million. The smallest
// value of each window of audio, fitted by a line over the whole recording,
// gives that drift.
type clock struct {
	rate   float64
	ref    time.Time // offsets are measured from it (the session start)
	window float64   // seconds of audio per window

	seen   bool
	start  float64 // best estimate so far, from the first window
	winEnd float64
	winMin float64
	winX   float64
	xs, ys []float64 // per window: audio time and smallest offset
}

const clockWindow = 10 // seconds

func newClock(rate int, ref time.Time) *clock {
	return &clock{rate: float64(rate), ref: ref, window: clockWindow}
}

// observe records that frames frames had arrived at time at.
func (c *clock) observe(at time.Time, frames int64) {
	x := float64(frames) / c.rate
	y := at.Sub(c.ref).Seconds() - x
	if !c.seen {
		c.seen, c.start, c.winEnd, c.winMin, c.winX = true, y, c.window, y, x
		return
	}
	if x <= c.window && y < c.start {
		c.start = y
	}
	if x > c.winEnd {
		c.xs, c.ys = append(c.xs, c.winX), append(c.ys, c.winMin)
		c.winEnd += c.window * math.Ceil((x-c.winEnd)/c.window)
		c.winMin, c.winX = y, x
	} else if y < c.winMin {
		c.winMin, c.winX = y, x
	}
}

// offset is the time of the first sample, in seconds after ref; ok is false
// before any sample.
func (c *clock) offset() (float64, bool) { return c.start, c.seen }

// drift is how much longer a second of audio lasts on the system clock
// (2e-5: 20 ppm, an hour of audio spans 72 ms more); ok is false until a
// minute of audio, or if the estimate is not plausible.
func (c *clock) drift() (float64, bool) {
	n := float64(len(c.xs))
	if n < 6 {
		return 0, false
	}
	var sx, sy, sxx, sxy float64
	for i := range c.xs {
		sx += c.xs[i]
		sy += c.ys[i]
		sxx += c.xs[i] * c.xs[i]
		sxy += c.xs[i] * c.ys[i]
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0, false
	}
	b := (n*sxy - sx*sy) / den
	if math.Abs(b) > 1e-3 { // a sound card's clock is good to 100 ppm: lost samples, not drift
		return 0, false
	}
	return b, true
}
