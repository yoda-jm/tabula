package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// FLAC is how a show's tracks are taken home at about half their size:
// ffmpeg reads a WAV track on its standard input and writes FLAC on its
// output, so nothing is written on this machine's disk on the way.

// FLACAvailable reports whether ffmpeg is there to convert to FLAC (on the
// PATH or next to tabula, as for recording).
func FLACAvailable() bool {
	_, err := exec.LookPath(program("ffmpeg"))
	return err == nil
}

// EncodeFLAC converts a WAV track read from src into FLAC written to dst;
// frames is its length (FLAC files say it in their header, which ffmpeg
// cannot fill in when it writes to a pipe). Tracks of 32 bits are written
// as 24-bit FLAC: the console's card sends 24-bit audio.
func EncodeFLAC(ctx context.Context, dst io.Writer, src io.Reader, frames int64) error {
	cmd := exec.CommandContext(ctx, program("ffmpeg"), "-hide_banner", "-nostdin", "-loglevel", "error",
		"-f", "wav", "-i", "pipe:0", "-map", "0:a", "-c:a", "flac", "-f", "flac", "pipe:1")
	var stderr bytes.Buffer
	out := &flacHeader{w: dst, frames: frames}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = src, out, &limited{&stderr, 4096}
	err := cmd.Run()
	if ferr := out.flush(); err == nil {
		err = ferr
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("ffmpeg: %v: %s", err, msg)
		}
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return nil
}

// flacStart is the length of the FLAC signature and its STREAMINFO block.
const flacStart = 4 + 4 + 34

// flacHeader passes ffmpeg's FLAC stream through, writing the number of
// samples into its STREAMINFO block on the way (players then know how
// long the track is).
type flacHeader struct {
	w      io.Writer
	frames int64
	head   []byte
	done   bool
}

func (f *flacHeader) Write(p []byte) (int, error) {
	n := len(p)
	if !f.done {
		take := min(flacStart-len(f.head), len(p))
		f.head = append(f.head, p[:take]...)
		p = p[take:]
		if len(f.head) < flacStart {
			return n, nil
		}
		if err := f.flush(); err != nil {
			return 0, err
		}
	}
	if len(p) > 0 {
		if _, err := f.w.Write(p); err != nil {
			return 0, err
		}
	}
	return n, nil
}

// flush writes the start of the stream, once.
func (f *flacHeader) flush() error {
	if f.done {
		return nil
	}
	f.done = true
	h := f.head
	// "fLaC", then the STREAMINFO block (type 0, 34 bytes): the 36 bits of
	// the total samples end its bytes 13 to 17
	if len(h) == flacStart && string(h[:4]) == "fLaC" && h[4]&0x7f == 0 && h[7] == 34 && f.frames > 0 && f.frames < 1<<36 {
		h[21] = h[21]&0xf0 | byte(f.frames>>32)&0x0f
		binary.BigEndian.PutUint32(h[22:26], uint32(f.frames))
	}
	_, err := f.w.Write(h)
	return err
}

// limited keeps the first n bytes written to it.
type limited struct {
	b *bytes.Buffer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.n - l.b.Len(); room > 0 {
		l.b.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}
