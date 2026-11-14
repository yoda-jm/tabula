package audio

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// writeWAVSamples writes a mono WAV of samples (full scale ±2³¹) at bits.
func writeWAVSamples(t *testing.T, path string, bits int, s []int32) {
	t.Helper()
	w, err := createWAV(createFile, path, 48000, bits, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range s {
		b := make([]byte, w.bytes)
		putSample(b, v, w.bytes)
		w.buf = append(w.buf, b...)
	}
	if err := w.finish(int64(len(s))); err != nil {
		t.Fatal(err)
	}
}

func readAll(t *testing.T, path string) []int32 {
	t.Helper()
	r, err := OpenTrack(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out := make([]int32, r.Frames())
	n := 0
	for n < len(out) {
		k, err := r.Read(out[n:])
		if err != nil {
			t.Fatalf("read at %d: %v", n, err)
		}
		n += k
	}
	return out
}

// signals are hard and easy cases: noise, music-like, silence, constant
// stretches, full-scale square, ramps, odd lengths.
func signals() map[string][]int32 {
	rng := rand.New(rand.NewSource(1))
	out := map[string][]int32{}
	sig := func(name string, n int, f func(i int) float64) {
		s := make([]int32, n)
		for i := range s {
			// 24-bit values in 32, as the console's card sends them
			s[i] = int32(math.Max(-1, math.Min(1-1.0/(1<<23), f(i)))*(1<<23)) << 8
		}
		out[name] = s
	}
	sig("noise", 3*flacBlock+77, func(int) float64 { return rng.Float64()*2 - 1 })
	sig("sines", 5*48000+1, func(i int) float64 {
		x := float64(i) / 48000
		return 0.3*math.Sin(2*math.Pi*220*x) + 0.2*math.Sin(2*math.Pi*3300*x)*math.Sin(x) + 0.01*(rng.Float64()-0.5)
	})
	sig("silence", 2*flacBlock, func(int) float64 { return 0 })
	sig("square", flacBlock+1, func(i int) float64 {
		if i/50%2 == 0 {
			return 1
		}
		return -1
	})
	sig("ramp", flacBlock, func(i int) float64 { return float64(i)/flacBlock*2 - 1 })
	sig("tiny", 5, func(i int) float64 { return float64(i) / 10 })
	sig("one", 1, func(int) float64 { return -0.5 })
	sig("empty", 0, func(int) float64 { return 0 })
	return out
}

func TestFLACRoundTrip(t *testing.T) {
	dir := t.TempDir()
	external, _ := exec.LookPath("flac")
	for name, s := range signals() {
		for _, bits := range []int{16, 24, 32} {
			wav := filepath.Join(dir, name+".wav")
			fl := filepath.Join(dir, name+".flac")
			writeWAVSamples(t, wav, bits, s)
			if err := CompressTrack(context.Background(), wav, fl); err != nil {
				t.Fatalf("%s %d bits: %v", name, bits, err)
			}
			want, got := readAll(t, wav), readAll(t, fl)
			if len(want) != len(got) {
				t.Fatalf("%s %d bits: %d samples, want %d", name, bits, len(got), len(want))
			}
			for i := range want {
				if want[i] != got[i] {
					t.Fatalf("%s %d bits: sample %d is %d, want %d", name, bits, i, got[i], want[i])
				}
			}
			if _, err := os.Stat(fl + ".part"); err == nil {
				t.Errorf("%s: the .part is left", name)
			}
			if external != "" { // the reference decoder agrees: a standard FLAC file, MD5 checked
				if out, err := exec.Command(external, "-t", "-s", fl).CombinedOutput(); err != nil {
					t.Errorf("%s %d bits: flac -t: %v %s", name, bits, err, out)
				}
			}
		}
	}
}

// Seeking anywhere reads the samples there, backwards and forwards.
func TestFLACSeek(t *testing.T) {
	dir := t.TempDir()
	s := signals()["sines"]
	wav, fl := filepath.Join(dir, "s.wav"), filepath.Join(dir, "s.flac")
	writeWAVSamples(t, wav, 24, s)
	if err := CompressTrack(context.Background(), wav, fl); err != nil {
		t.Fatal(err)
	}
	r, err := OpenTrack(fl)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	want := readAll(t, wav)
	buf := make([]int32, 700)
	for _, at := range []int64{4 * 48000, 10, 2*48000 + 4095, 4096, 0, int64(len(s)) - 3, 3 * 48000} {
		if err := r.SeekFrame(at); err != nil {
			t.Fatal(err)
		}
		n, err := r.Read(buf)
		if err != nil {
			t.Fatalf("read at %d: %v", at, err)
		}
		for i := range n {
			if buf[i] != want[at+int64(i)] {
				t.Fatalf("at %d+%d: %d, want %d", at, i, buf[i], want[at+int64(i)])
			}
		}
	}
	if fr := r.(*flacReader); len(fr.seeks) != 3 { // 5 s: points at 0, 2, 4 s
		t.Errorf("seek points: %+v", fr.seeks)
	}
}

// 32-bit samples using their low bits, and floats, stay WAV.
func TestFLACNotCompressible(t *testing.T) {
	dir := t.TempDir()
	wav := filepath.Join(dir, "x.wav")
	writeWAVSamples(t, wav, 32, []int32{1, 2, 3})
	if err := CompressTrack(context.Background(), wav, filepath.Join(dir, "x.flac")); !errors.Is(err, ErrNotCompressible) {
		t.Errorf("32 bits with low bits: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.flac.part")); err == nil {
		t.Error("a .part is left")
	}
}

// The FLAC files are about the size the reference encoder makes at its
// fastest level (flac -1), when it is installed.
func TestFLACSize(t *testing.T) {
	ref, err := exec.LookPath("flac")
	if err != nil {
		t.Skip("no flac to compare with")
	}
	dir := t.TempDir()
	for _, name := range []string{"sines", "noise", "square"} {
		wav, fl, refFile := filepath.Join(dir, name+".wav"), filepath.Join(dir, name+".flac"), filepath.Join(dir, name+".ref.flac")
		writeWAVSamples(t, wav, 24, signals()[name])
		if err := CompressTrack(context.Background(), wav, fl); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(ref, "-1", "-s", "-f", "-o", refFile, wav).CombinedOutput(); err != nil {
			t.Fatalf("flac -1: %v %s", err, out)
		}
		a, _ := os.Stat(fl)
		b, _ := os.Stat(refFile)
		if ratio := float64(a.Size()) / float64(b.Size()); ratio > 1.05 {
			t.Errorf("%s: %d bytes, flac -1 makes %d (%.0f%%)", name, a.Size(), b.Size(), 100*ratio)
		}
	}
}

func BenchmarkCompressTrack(b *testing.B) {
	dir := b.TempDir()
	rng := rand.New(rand.NewSource(1))
	s := make([]int32, 30*48000)
	for i := range s {
		x := float64(i) / 48000
		s[i] = int32((0.3*math.Sin(2*math.Pi*220*x)+0.05*(rng.Float64()-0.5))*(1<<23)) << 8
	}
	t := &testing.T{}
	wav := filepath.Join(dir, "s.wav")
	writeWAVSamples(t, wav, 24, s)
	b.SetBytes(int64(len(s) * 3))
	for i := 0; i < b.N; i++ {
		fl := filepath.Join(dir, "s.flac")
		os.Remove(fl)
		if err := CompressTrack(context.Background(), wav, fl); err != nil {
			b.Fatal(err)
		}
	}
}

// flacSession is playSession with its tracks compressed.
func flacSession(t *testing.T) (string, Info) {
	t.Helper()
	dir, info := playSession(t)
	for i, tr := range info.Tracks {
		if tr.File == "" {
			continue
		}
		wav := filepath.Join(dir, tr.File)
		fl := wav[:len(wav)-len(".wav")] + ".flac"
		if err := CompressTrack(context.Background(), wav, fl); err != nil {
			t.Fatal(err)
		}
		os.Remove(wav)
		info.Tracks[i].File = tr.File[:len(tr.File)-len(".wav")] + ".flac"
	}
	return dir, info
}

// A compressed session plays and meters exactly as the WAV one.
func TestFLACSessionReads(t *testing.T) {
	wavDir, wavInfo := playSession(t)
	flacDir, flacInfo := flacSession(t)
	for _, from := range []float64{0, 100.0 / 48000, -0.001} {
		a, b := play(t, wavInfo, wavDir, from), play(t, flacInfo, flacDir, from)
		if len(a) != len(b) {
			t.Fatalf("from %v: %d samples, want %d", from, len(b), len(a))
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("from %v: sample %d is %d, want %d", from, i, b[i], a[i])
			}
		}
	}
	lw, err := OpenLevels(wavDir, wavInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer lw.Close()
	lf, err := OpenLevels(flacDir, flacInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	for _, at := range []float64{0.001, 0.02, 0.01, 0.0205} { // back and forth
		a, b := lw.At(at, 0.002), lf.At(at, 0.002)
		if a[2] != b[2] || a[4] != b[4] {
			t.Errorf("levels at %v: %v, want %v", at, b, a)
		}
	}
}

// A FLAC track decodes back into the WAV file it was, byte for byte.
func TestFLACAsWAV(t *testing.T) {
	dir := t.TempDir()
	for _, bits := range []int{16, 24} {
		s := signals()["sines"]
		wav, fl := filepath.Join(dir, "s.wav"), filepath.Join(dir, "s.flac")
		writeWAVSamples(t, wav, bits, s)
		if err := CompressTrack(context.Background(), wav, fl); err != nil {
			t.Fatal(err)
		}
		var out bytesBuffer
		if err := WriteFLACAsWAV(context.Background(), &out, fl, nil); err != nil {
			t.Fatal(err)
		}
		want, _ := os.ReadFile(wav)
		size, _ := FLACAsWAVSize(fl)
		if string(out.b) != string(want) || size != int64(len(want)) {
			t.Errorf("%d bits: %d bytes (size said %d), want the %d of the WAV", bits, len(out.b), size, len(want))
		}
		if n, _ := TrackFrames(fl); n != int64(len(s)) {
			t.Errorf("frames %d", n)
		}
	}
}

type bytesBuffer struct{ b []byte }

func (w *bytesBuffer) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }

// A WAV track compressed since its name was read opens as FLAC.
func TestOpenTrackCompressedMeanwhile(t *testing.T) {
	dir := t.TempDir()
	wav, fl := filepath.Join(dir, "s.wav"), filepath.Join(dir, "s.flac")
	writeWAVSamples(t, wav, 24, signals()["ramp"])
	if err := CompressTrack(context.Background(), wav, fl); err != nil {
		t.Fatal(err)
	}
	want := readAll(t, wav)
	os.Remove(wav)
	got := readAll(t, wav)
	if len(got) != len(want) || got[100] != want[100] {
		t.Errorf("read %d samples, want %d", len(got), len(want))
	}
	if _, err := OpenTrack(filepath.Join(dir, "nope.wav")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a track nowhere: %v", err)
	}
}
