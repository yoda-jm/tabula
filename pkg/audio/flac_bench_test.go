package audio

import (
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

type memAt struct{ b []byte }

func (m *memAt) WriteAt(p []byte, off int64) (int, error) {
	if need := int(off) + len(p); need > len(m.b) {
		m.b = append(m.b, make([]byte, need-len(m.b))...)
	}
	copy(m.b[off:], p)
	return len(p), nil
}

func benchSignal() []int32 {
	rng := rand.New(rand.NewSource(1))
	s := make([]int32, 30*48000)
	for i := range s {
		x := float64(i) / 48000
		s[i] = int32((0.3*math.Sin(2*math.Pi*220*x) + 0.05*(rng.Float64()-0.5)) * (1 << 23))
	}
	return s
}

func BenchmarkFLACEncode(b *testing.B) {
	s := benchSignal()
	b.SetBytes(int64(len(s) * 3))
	for range b.N {
		w, _ := newFLACWriter(&memAt{}, 48000, 24, int64(len(s)))
		for i := 0; i < len(s); i += flacBlock {
			w.writeBlock(s[i:min(i+flacBlock, len(s))])
		}
		w.close()
	}
}

func BenchmarkFLACDecode(b *testing.B) {
	s := benchSignal()
	m := &memAt{}
	w, _ := newFLACWriter(m, 48000, 24, int64(len(s)))
	for i := 0; i < len(s); i += flacBlock {
		w.writeBlock(s[i:min(i+flacBlock, len(s))])
	}
	w.close()
	path := filepath.Join(b.TempDir(), "s.flac")
	os.WriteFile(path, m.b, 0o644)
	buf := make([]int32, flacBlock)
	b.SetBytes(int64(len(s) * 3))
	for range b.N {
		r, _ := openFLAC(path)
		for {
			if _, err := r.Read(buf); err != nil {
				break
			}
		}
		r.Close()
	}
}
