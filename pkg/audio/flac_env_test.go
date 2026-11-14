package audio

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestCompressFile times CompressTrack on $FLAC_WAV (a manual check).
func TestCompressFile(t *testing.T) {
	src := os.Getenv("FLAC_WAV")
	if src == "" {
		t.Skip("FLAC_WAV not set")
	}
	dst := t.TempDir() + "/x.flac"
	start := time.Now()
	if err := CompressTrack(context.Background(), src, dst); err != nil {
		t.Fatal(err)
	}
	a, _ := os.Stat(src)
	b, _ := os.Stat(dst)
	el := time.Since(start)
	t.Logf("%d -> %d bytes (%.0f%%) in %v: %.0f MB/s of WAV, encode + verify", a.Size(), b.Size(), 100*float64(b.Size())/float64(a.Size()), el, float64(a.Size())/el.Seconds()/1e6)
}
