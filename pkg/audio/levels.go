package audio

import (
	"errors"
	"math"
	"path/filepath"
	"sync"
)

// Levels reads the level of a session's tracks at a moment of the
// session, for meters that follow a local replay: the peak of the samples
// in a short window before that moment, read from the tracks (WAV or
// FLAC) as asked: about 2400 samples a track for a 50 ms window, a FLAC
// frame decoded and kept for the next read.
type Levels struct {
	info   Info
	mu     sync.Mutex
	tracks map[int]TrackReader // by track number (Track.Channel)
	buf    []int32
}

// OpenLevels opens the tracks of a session (dir, its audio info) whose
// file is there; a session without any gives an error.
func OpenLevels(dir string, info Info) (*Levels, error) {
	l := &Levels{info: info, tracks: map[int]TrackReader{}}
	var errs []error
	for _, t := range info.Tracks {
		if t.File == "" {
			continue
		}
		r, err := OpenTrack(filepath.Join(dir, filepath.FromSlash(t.File)))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		l.tracks[t.Channel] = r
	}
	if len(l.tracks) == 0 {
		return nil, errors.Join(append(errs, errors.New("no audio track to read"))...)
	}
	return l, nil
}

// Has tells whether track n (1-32) can be read.
func (l *Levels) Has(n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.tracks[n]
	return ok
}

// At returns the peak level (1.0 = 0 dBFS) of each track, by track
// number, over the window seconds before session time t; 0 before the
// first sample and after the last.
func (l *Levels) At(t, window float64) map[int]float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[int]float64, len(l.tracks))
	rate := float64(l.info.Rate)
	end := int64(math.Round(l.info.AudioTime(t) * rate))
	n := int64(window * rate)
	start := max(end-n, 0)
	for ch, tr := range l.tracks {
		e := min(end, tr.Frames())
		out[ch] = 0
		if e <= start || tr.SeekFrame(start) != nil {
			continue
		}
		need := int(e - start)
		if cap(l.buf) < need {
			l.buf = make([]int32, need)
		}
		b := l.buf[:need]
		got := 0
		for got < need {
			k, err := tr.Read(b[got:])
			if err != nil {
				break
			}
			got += k
		}
		peak := 0.0
		for _, v := range b[:got] {
			peak = max(peak, math.Abs(float64(v)))
		}
		out[ch] = min(peak/(1<<31), 1)
	}
	return out
}

// Close closes the tracks.
func (l *Levels) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var errs []error
	for _, tr := range l.tracks {
		errs = append(errs, tr.Close())
	}
	l.tracks = map[int]TrackReader{}
	return errors.Join(errs...)
}
