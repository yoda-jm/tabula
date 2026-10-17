package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// A track file is a mono PCM WAV file with an 80-byte header:
//
//	 0 "RIFF" size  "WAVE"
//	12 "JUNK" 28    (room for the ds64 chunk)
//	48 "fmt " 16    PCM, 1 channel, rate, bits
//	72 "data" size  samples…
//
// The sizes are refreshed while recording, so the file is valid up to the
// last refresh if the program dies. When the file outgrows 4 GiB the header
// turns into RF64 (EBU Tech 3306): "RF64", and the JUNK chunk becomes the
// ds64 chunk holding the 64-bit sizes; the 32-bit sizes are then 0xFFFFFFFF.
const headerSize = 80

// file is what a track is written to (an *os.File; a failing one in tests).
type file interface {
	io.Writer
	io.WriterAt
	Truncate(size int64) error
	Sync() error
	Close() error
}

func createFile(path string) (file, error) { return os.Create(path) }

// flushSize is how much of a track is buffered before being written.
const flushSize = 64 << 10

type wavWriter struct {
	f      file
	path   string
	rate   int
	bytes  int   // per sample
	rf64At int64 // RIFF size beyond which the file becomes RF64
	buf    []byte
	data   int64 // bytes of samples in the file
	rf64   bool
	pad    bool // a pad byte follows odd-sized data (when finished)
	err    error
}

func createWAV(create func(string) (file, error), path string, rate, bits int, rf64At int64) (*wavWriter, error) {
	if rf64At <= 0 {
		rf64At = math.MaxUint32
	}
	f, err := create(path)
	if err != nil {
		return nil, err
	}
	w := &wavWriter{f: f, path: path, rate: rate, bytes: bits / 8, rf64At: rf64At, buf: make([]byte, 0, flushSize+4096)}
	if _, err := f.Write(w.header()); err != nil {
		f.Close()
		return nil, err
	}
	return w, nil
}

func (w *wavWriter) riffSize() int64 {
	n := headerSize - 8 + w.data
	if w.pad {
		n++
	}
	return n
}

func (w *wavWriter) header() []byte {
	h := make([]byte, headerSize)
	le := binary.LittleEndian
	riff := w.riffSize()
	if riff > w.rf64At {
		w.rf64 = true
	}
	copy(h[0:], "RIFF")
	le.PutUint32(h[4:], uint32(riff))
	copy(h[8:], "WAVE")
	copy(h[12:], "JUNK")
	le.PutUint32(h[16:], 28)
	copy(h[48:], "fmt ")
	le.PutUint32(h[52:], 16)
	le.PutUint16(h[56:], 1) // PCM
	le.PutUint16(h[58:], 1) // mono
	le.PutUint32(h[60:], uint32(w.rate))
	le.PutUint32(h[64:], uint32(w.rate*w.bytes))
	le.PutUint16(h[68:], uint16(w.bytes))
	le.PutUint16(h[70:], uint16(8*w.bytes))
	copy(h[72:], "data")
	le.PutUint32(h[76:], uint32(w.data))
	if w.rf64 {
		copy(h[0:], "RF64")
		le.PutUint32(h[4:], math.MaxUint32)
		copy(h[12:], "ds64")
		le.PutUint64(h[20:], uint64(riff))
		le.PutUint64(h[28:], uint64(w.data))
		le.PutUint64(h[36:], uint64(w.data/int64(w.bytes))) // sample count
		le.PutUint32(h[44:], 0)                             // no table
		le.PutUint32(h[76:], math.MaxUint32)
	}
	return h
}

// full says the buffer should be written out.
func (w *wavWriter) full() bool { return len(w.buf) >= flushSize }

// flush writes the buffered samples.
func (w *wavWriter) flush() error {
	if w.err != nil || len(w.buf) == 0 {
		w.buf = w.buf[:0]
		return w.err
	}
	n, err := w.f.Write(w.buf)
	w.data += int64(n)
	w.buf = w.buf[:0]
	if err != nil {
		w.err = fmt.Errorf("writing %s: %w", w.path, err)
	}
	return w.err
}

// refresh writes the buffered samples and the current sizes in the header.
func (w *wavWriter) refresh() error {
	if err := w.flush(); err != nil {
		return err
	}
	if _, err := w.f.WriteAt(w.header(), 0); err != nil {
		w.err = fmt.Errorf("writing %s: %w", w.path, err)
	}
	return w.err
}

// frames is the number of whole samples in the file.
func (w *wavWriter) frames() int64 { return w.data / int64(w.bytes) }

// finish keeps the first frames samples (every track ends at the same
// sample, whole), writes the header and closes the file. It works on a full
// disk: it only shrinks the file and rewrites its header.
func (w *wavWriter) finish(frames int64) error {
	w.flush() // an error leaves what could be written
	var err error
	if keep := frames * int64(w.bytes); keep < w.data {
		w.data = keep
		err = w.f.Truncate(headerSize + w.data)
	}
	if w.data%2 == 1 {
		if _, perr := w.f.WriteAt([]byte{0}, headerSize+w.data); perr == nil {
			w.pad = true
		}
	}
	if _, herr := w.f.WriteAt(w.header(), 0); herr != nil {
		err = errors.Join(err, herr)
	}
	return errors.Join(err, w.f.Sync(), w.f.Close())
}

// WAVInfo describes a WAV or RF64 file or stream.
type WAVInfo struct {
	Format     int `json:"format"`   // 1 PCM, 3 IEEE float
	Channels   int `json:"channels"` // interleaved
	Rate       int `json:"rate"`
	Bits       int `json:"bits"`       // container bits per sample
	ValidBits  int `json:"valid_bits"` // significant bits (extensible format)
	RF64       bool
	DataOffset int64 // where the samples start
	DataSize   int64 // bytes of samples from the header (-1: unknown, a stream)
	Frames     int64 // samples per channel: in the file, whatever the header says
}

// sampleFormat is the stream format of the samples.
func (i WAVInfo) sampleFormat() (sampleFormat, error) {
	switch {
	case i.Format == 3 && i.Bits == 32:
		return parseFormat("f32le")
	case i.Format == 1 && i.Bits == 16:
		return parseFormat("s16le")
	case i.Format == 1 && i.Bits == 24:
		return parseFormat("s24le")
	case i.Format == 1 && i.Bits == 32: // 24 valid bits are the high ones
		return parseFormat("s32le")
	}
	return sampleFormat{}, fmt.Errorf("unsupported WAV samples: format %d, %d bits", i.Format, i.Bits)
}

// readWAVHeader reads the chunks of a WAV or RF64 header up to the start of
// the samples. A streamed header carries made-up sizes: only the format is
// trusted.
func readWAVHeader(r io.Reader) (WAVInfo, error) {
	le := binary.LittleEndian
	var info WAVInfo
	head := make([]byte, 12)
	if _, err := io.ReadFull(r, head); err != nil {
		return info, fmt.Errorf("WAV header: %w", err)
	}
	if string(head[8:12]) != "WAVE" || (string(head[:4]) != "RIFF" && string(head[:4]) != "RF64") {
		return info, errors.New("not a WAV stream")
	}
	info.RF64 = string(head[:4]) == "RF64"
	info.DataOffset = 12
	var ds64Data int64 = -1
	gotFmt := false
	for {
		ch := make([]byte, 8)
		if _, err := io.ReadFull(r, ch); err != nil {
			return info, fmt.Errorf("WAV header: %w", err)
		}
		id, size := string(ch[:4]), int64(le.Uint32(ch[4:]))
		info.DataOffset += 8
		if id == "data" {
			info.DataSize = size
			if info.RF64 && size == math.MaxUint32 {
				info.DataSize = ds64Data
			}
			if !gotFmt {
				return info, errors.New("WAV header: data before fmt")
			}
			return info, nil
		}
		if size > 1<<20 {
			return info, fmt.Errorf("WAV header: %q chunk of %d bytes", id, size)
		}
		body := make([]byte, size+size%2)
		if _, err := io.ReadFull(r, body); err != nil {
			return info, fmt.Errorf("WAV header: %w", err)
		}
		info.DataOffset += int64(len(body))
		switch {
		case id == "ds64" && size >= 24:
			ds64Data = int64(le.Uint64(body[8:]))
		case id == "fmt " && size >= 16:
			gotFmt = true
			info.Format = int(le.Uint16(body[0:]))
			info.Channels = int(le.Uint16(body[2:]))
			info.Rate = int(le.Uint32(body[4:]))
			align := int(le.Uint16(body[12:]))
			info.ValidBits = int(le.Uint16(body[14:]))
			if info.Channels > 0 {
				info.Bits = 8 * align / info.Channels
			}
			if info.Format == 0xFFFE && size >= 40 { // extensible: the format is in the sub-format GUID
				info.ValidBits = int(le.Uint16(body[18:]))
				info.Format = int(le.Uint16(body[24:]))
			}
		}
	}
}

// ReadWAV describes a WAV or RF64 file. Frames counts the samples the file
// holds, even if the header was not refreshed (a recording cut short).
func ReadWAV(path string) (WAVInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return WAVInfo{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return WAVInfo{}, err
	}
	head := make([]byte, 4096)
	n, _ := io.ReadFull(f, head)
	info, err := readWAVHeader(bytes.NewReader(head[:n]))
	if err != nil {
		return info, fmt.Errorf("%s: %w", path, err)
	}
	inFile := st.Size() - info.DataOffset
	if info.DataSize < 0 || info.DataSize > inFile || info.DataSize == math.MaxUint32 {
		info.DataSize = inFile
	}
	if align := int64(info.Channels * info.Bits / 8); align > 0 {
		info.Frames = inFile / align
	}
	return info, nil
}
