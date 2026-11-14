package audio

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// Levels reads the level of a session's tracks at a moment of the
// session, for meters that follow a local replay: the peak of the samples
// in a short window before that moment, read from the WAV files as asked
// (about 2400 samples a track for a 50 ms window: nothing is cached).
type Levels struct {
	info   Info
	mu     sync.Mutex
	tracks map[int]*levelTrack // by track number (Track.Channel)
	buf    []byte
}

type levelTrack struct {
	f      *os.File
	format sampleFormat
	data   int64 // where the samples start
	frames int64
}

// OpenLevels opens the tracks of a session (dir, its audio info) whose
// file is there; a session without any gives an error.
func OpenLevels(dir string, info Info) (*Levels, error) {
	l := &Levels{info: info, tracks: map[int]*levelTrack{}}
	var errs []error
	for _, t := range info.Tracks {
		if t.File == "" {
			continue
		}
		path := filepath.Join(dir, t.File)
		w, err := ReadWAV(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		format, err := w.sampleFormat()
		if err == nil && w.Channels != 1 {
			err = fmt.Errorf("%d channels, expected a mono track", w.Channels)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", filepath.Base(path), err))
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		l.tracks[t.Channel] = &levelTrack{f: f, format: format, data: w.DataOffset, frames: w.Frames}
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
		e := min(end, tr.frames)
		if e <= start {
			out[ch] = 0
			continue
		}
		size := int64(tr.format.size)
		need := int((e - start) * size)
		if cap(l.buf) < need {
			l.buf = make([]byte, need)
		}
		b := l.buf[:need]
		got, err := tr.f.ReadAt(b, tr.data+start*size)
		if err != nil && !errors.Is(err, io.EOF) {
			out[ch] = 0
			continue
		}
		peak := 0.0
		for i := 0; i+int(size) <= got; i += int(size) {
			v := math.Abs(float64(tr.format.sample(b[i:])))
			peak = max(peak, v)
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
		errs = append(errs, tr.f.Close())
	}
	l.tracks = map[int]*levelTrack{}
	return errors.Join(errs...)
}
