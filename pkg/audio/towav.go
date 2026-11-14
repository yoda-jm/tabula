package audio

import (
	"context"
	"io"
)

// A compressed session goes home "as recorded" with its FLAC tracks
// decoded back into WAV files on the way (session/takehome.go): nothing is
// written on this machine's disk.

// TrackFrames is the length of a track, WAV or FLAC.
func TrackFrames(path string) (int64, error) {
	r, err := OpenTrack(path)
	if err != nil {
		return 0, err
	}
	defer r.Close()
	return r.Frames(), nil
}

// flacWAV is the WAV file a FLAC track decodes to.
func flacWAV(path string) (*wavWriter, *flacReader, error) {
	r, err := openFLAC(path)
	if err != nil {
		return nil, nil, err
	}
	bits := 24
	if r.info.bps <= 16 {
		bits = 16
	}
	w := &wavWriter{rate: r.info.rate, bytes: bits / 8, rf64At: 1<<32 - 1}
	w.data = r.info.total * int64(w.bytes)
	w.pad = w.data%2 == 1
	return w, r, nil
}

// FLACAsWAVSize is the size of the WAV file a FLAC track decodes to.
func FLACAsWAVSize(path string) (int64, error) {
	w, r, err := flacWAV(path)
	if err != nil {
		return 0, err
	}
	r.Close()
	size := headerSize + w.data
	if w.pad {
		size++
	}
	return size, nil
}

// WriteFLACAsWAV decodes a FLAC track into a WAV file written to out; read
// is told the FLAC bytes read as they are (roughly: by frame).
func WriteFLACAsWAV(ctx context.Context, out io.Writer, path string, read func(n int64)) error {
	w, r, err := flacWAV(path)
	if err != nil {
		return err
	}
	defer r.Close()
	if _, err := out.Write(w.header()); err != nil {
		return err
	}
	block := make([]int32, flacBlock)
	buf := make([]byte, 0, flacBlock*w.bytes)
	var lastPos int64
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
		buf = buf[:n*w.bytes]
		for i, v := range block[:n] {
			putSample(buf[i*w.bytes:], v, w.bytes)
		}
		if _, err := out.Write(buf); err != nil {
			return err
		}
		if read != nil {
			if pos, err := r.f.Seek(0, io.SeekCurrent); err == nil && pos > lastPos {
				read(pos - lastPos)
				lastPos = pos
			}
		}
	}
	if w.pad {
		_, err = out.Write([]byte{0})
	}
	return err
}
