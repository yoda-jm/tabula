package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// testWAV is a mono PCM WAV file of frames samples (a sine) with the
// 44-byte header most programs write.
func testWAV(bits, frames int) []byte {
	n := bits / 8
	var b bytes.Buffer
	le := binary.LittleEndian
	b.WriteString("RIFF")
	binary.Write(&b, le, uint32(36+frames*n))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(48000), uint32(48000 * n), uint16(n), uint16(bits)} {
		binary.Write(&b, le, v)
	}
	b.WriteString("data")
	binary.Write(&b, le, uint32(frames*n))
	for i := range frames {
		v := int64(math.Sin(float64(i)/10) * float64(int64(1)<<(bits-2)))
		for k := range n {
			b.WriteByte(byte(v >> (8 * k)))
		}
	}
	return b.Bytes()
}

// streamInfo is the start of a FLAC stream as ffmpeg writes it to a pipe:
// no total samples.
func streamInfo() []byte {
	h := make([]byte, flacStart)
	copy(h, "fLaC")
	h[7] = 34
	h[18], h[19], h[20], h[21] = 0x0b, 0xb8, 0x01, 0x70 // 48 kHz, mono, 24 bits
	return h
}

func TestFLACHeader(t *testing.T) {
	var out bytes.Buffer
	f := &flacHeader{w: &out, frames: 0x1_2345_6789}
	stream := append(streamInfo(), "frames…"...)
	for _, b := range stream { // written a byte at a time
		if n, err := f.Write([]byte{b}); n != 1 || err != nil {
			t.Fatal(n, err)
		}
	}
	got := out.Bytes()
	if len(got) != len(stream) || got[21] != 0x71 || !bytes.Equal(got[22:26], []byte{0x23, 0x45, 0x67, 0x89}) ||
		got[20] != 0x01 || string(got[flacStart:]) != "frames…" {
		t.Errorf("% x", got[:26])
	}

	// not a FLAC stream, or a short one: as it came
	for _, in := range [][]byte{[]byte("not flac at all, a longer error message from somewhere"), []byte("fLaC")} {
		out.Reset()
		f := &flacHeader{w: &out, frames: 10}
		f.Write(in)
		f.flush()
		if !bytes.Equal(out.Bytes(), in) {
			t.Errorf("%q → %q", in, out.Bytes())
		}
	}
}

func TestEncodeFLAC(t *testing.T) {
	if !FLACAvailable() {
		t.Skip("no ffmpeg")
	}
	for _, bits := range []int{16, 24} {
		wav := testWAV(bits, 48000)
		var flac bytes.Buffer
		if err := EncodeFLAC(context.Background(), &flac, bytes.NewReader(wav), 48000); err != nil {
			t.Fatal(err)
		}
		b := flac.Bytes()
		if len(b) < flacStart || string(b[:4]) != "fLaC" || binary.BigEndian.Uint32(b[22:26]) != 48000 || len(b) > len(wav)/2 {
			t.Fatalf("%d bits: %d bytes of FLAC for %d of WAV: % x", bits, len(b), len(wav), b[:min(len(b), 26)])
		}
		// lossless: decoded, the same samples
		dir := t.TempDir()
		in := filepath.Join(dir, "t.flac")
		if err := os.WriteFile(in, b, 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-i", in, "-f", map[int]string{16: "s16le", 24: "s24le"}[bits], "-").Output()
		if err != nil || !bytes.Equal(out, wav[44:]) {
			t.Errorf("%d bits: decoded %d bytes (%v), want the %d of the WAV", bits, len(out), err, len(wav)-44)
		}
	}

	// not a WAV stream: ffmpeg's error
	if err := EncodeFLAC(context.Background(), &bytes.Buffer{}, bytes.NewReader([]byte("nope")), 10); err == nil {
		t.Error("no error for a stream that is not WAV")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := EncodeFLAC(ctx, &bytes.Buffer{}, bytes.NewReader(testWAV(24, 4800)), 4800); err != context.Canceled {
		t.Errorf("cancelled: %v", err)
	}
}
