package audio

import (
	"bufio"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// TrackReader reads a session's mono track, WAV as recorded or FLAC once
// compressed (compress.go), alike: replays, the virtual soundcheck, the
// local replay's meters and listening, the take-home copies.
type TrackReader interface {
	Rate() int
	Frames() int64
	// SeekFrame moves to sample n (clamped to the track).
	SeekFrame(n int64) error
	// Read gives the next samples, full scale at ±2³¹; io.EOF at the end.
	Read(dst []int32) (int, error)
	Close() error
}

// OpenTrack opens a mono WAV or FLAC track (by its extension).
func OpenTrack(path string) (TrackReader, error) {
	if IsFLAC(path) {
		return openFLAC(path)
	}
	return openWAVTrack(path)
}

// IsFLAC tells a FLAC track by its name.
func IsFLAC(path string) bool { return strings.EqualFold(filepath.Ext(path), ".flac") }

type wavTrack struct {
	f      *os.File
	br     *bufio.Reader
	format sampleFormat
	data   int64
	frames int64
	pos    int64
	rate   int
	buf    []byte
}

func openWAVTrack(path string) (*wavTrack, error) {
	info, err := ReadWAV(path)
	if err != nil {
		return nil, err
	}
	format, err := info.sampleFormat()
	switch {
	case err != nil:
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	case info.Channels != 1:
		return nil, fmt.Errorf("%s: %d channels, expected a mono track", filepath.Base(path), info.Channels)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(info.DataOffset, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return &wavTrack{f: f, br: bufio.NewReaderSize(f, 64<<10), format: format, data: info.DataOffset,
		frames: info.Frames, rate: info.Rate}, nil
}

func (w *wavTrack) Rate() int     { return w.rate }
func (w *wavTrack) Frames() int64 { return w.frames }
func (w *wavTrack) Close() error  { return w.f.Close() }

func (w *wavTrack) SeekFrame(n int64) error {
	n = min(max(n, 0), w.frames)
	if n == w.pos {
		return nil
	}
	if _, err := w.f.Seek(w.data+n*int64(w.format.size), io.SeekStart); err != nil {
		return err
	}
	w.br.Reset(w.f)
	w.pos = n
	return nil
}

func (w *wavTrack) Read(dst []int32) (int, error) {
	n := int(min(int64(len(dst)), w.frames-w.pos))
	if n <= 0 {
		return 0, io.EOF
	}
	size := w.format.size
	if cap(w.buf) < n*size {
		w.buf = make([]byte, n*size)
	}
	b := w.buf[:n*size]
	if _, err := io.ReadFull(w.br, b); err != nil {
		return 0, err
	}
	for i := range n {
		dst[i] = w.format.sample(b[i*size:])
	}
	w.pos += int64(n)
	return n, nil
}

// ErrNotCompressible is a track whose samples FLAC (16 or 24 bits) would
// not keep: float, or 32 bits using their low 8 bits. It stays WAV.
var ErrNotCompressible = errors.New("these samples are kept as WAV (float, or more than 24 bits)")

// CompressTrack writes the WAV track src as FLAC at dst, losslessly, and
// checks it: through dst+".part", decoded again from its start, its length
// and the MD5 of its samples compared with the WAV's; dst exists only once
// it matched. src is left alone (the caller removes it).
func CompressTrack(ctx context.Context, src, dst string) error {
	info, err := ReadWAV(src)
	if err != nil {
		return err
	}
	format, err := info.sampleFormat()
	if err != nil {
		return ErrNotCompressible
	}
	bps := 24
	switch {
	case info.Channels != 1:
		return fmt.Errorf("%s: %d channels, expected a mono track", filepath.Base(src), info.Channels)
	case format.float:
		return ErrNotCompressible
	case format.size == 2:
		bps = 16
	}
	in, err := openWAVTrack(src)
	if err != nil {
		return err
	}
	defer in.Close()
	part := dst + ".part"
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			out.Close()
			os.Remove(part)
		}
	}()
	fw, err := newFLACWriter(out, info.Rate, bps, info.Frames)
	if err != nil {
		return err
	}
	block := make([]int32, flacBlock)
	shift := uint(32 - bps)
	low := int32(1)<<shift - 1
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := in.Read(block)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		for i, v := range block[:n] {
			if v&low != 0 { // 32-bit samples using their low bits
				return ErrNotCompressible
			}
			block[i] = v >> shift
		}
		if err := fw.writeBlock(block[:n]); err != nil {
			return err
		}
	}
	if err := fw.close(); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := verifyFLAC(ctx, part, info.Frames, bps, fw.sum()); err != nil {
		return fmt.Errorf("%s: the FLAC copy does not match: %w", filepath.Base(src), err)
	}
	if err := os.Rename(part, dst); err != nil {
		return err
	}
	ok = true
	return nil
}

// verifyFLAC decodes a FLAC track from its start and checks its length and
// the MD5 of its samples.
func verifyFLAC(ctx context.Context, path string, frames int64, bps int, sum []byte) error {
	r, err := openFLAC(path)
	if err != nil {
		return err
	}
	defer r.Close()
	if r.info.total != frames || r.info.bps != bps || string(r.info.md5[:]) != string(sum) {
		return errors.New("header")
	}
	h := md5.New()
	block := make([]int32, flacBlock)
	b := make([]byte, 0, 3*flacBlock)
	shift := uint(32 - bps)
	got := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := r.Read(block)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		b = b[:0]
		for _, v := range block[:n] {
			v >>= shift
			if bps == 16 {
				b = append(b, byte(v), byte(v>>8))
			} else {
				b = append(b, byte(v), byte(v>>8), byte(v>>16))
			}
		}
		h.Write(b)
		got += int64(n)
	}
	if got != frames {
		return fmt.Errorf("%d samples decoded, %d recorded", got, frames)
	}
	if string(h.Sum(nil)) != string(sum) {
		return errors.New("the samples differ")
	}
	return nil
}
