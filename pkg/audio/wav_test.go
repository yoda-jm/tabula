package audio

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func writeSamples(t *testing.T, w *wavWriter, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		b := make([]byte, w.bytes)
		putSample(b, int32(i)<<8, w.bytes)
		w.buf = append(w.buf, b...)
	}
}

func TestWAVWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.wav")
	w, err := createWAV(createFile, path, 48000, 24, 0)
	if err != nil {
		t.Fatal(err)
	}
	// An empty file is already valid.
	info, err := ReadWAV(path)
	if err != nil || info.Frames != 0 || info.DataOffset != headerSize || info.Rate != 48000 || info.Bits != 24 {
		t.Fatalf("empty: %+v %v", info, err)
	}
	writeSamples(t, w, 1001)
	if err := w.refresh(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	le := binary.LittleEndian
	if string(data[:4]) != "RIFF" || le.Uint32(data[4:]) != uint32(len(data)-8) || le.Uint32(data[76:]) != 3003 {
		t.Fatalf("refreshed header: % x", data[:80])
	}
	// The odd data size gets a pad byte, counted in the RIFF size only.
	if err := w.finish(1001); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if len(data) != headerSize+3003+1 || le.Uint32(data[4:]) != uint32(len(data)-8) || le.Uint32(data[76:]) != 3003 {
		t.Fatalf("finished: %d bytes, header % x", len(data), data[:80])
	}
	if got := int32(le.Uint32(append([]byte{0}, data[headerSize+3:headerSize+6]...))); got != 1<<8 {
		t.Errorf("sample 1: %#x", got)
	}
	info, err = ReadWAV(path)
	if err != nil || info.Frames != 1001 || info.DataSize != 3003 {
		t.Errorf("read back: %+v %v", info, err)
	}
}

// A file past the RIFF limit (lowered here) becomes RF64 with a ds64 chunk.
func TestRF64(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.wav")
	w, err := createWAV(createFile, path, 48000, 16, 1000)
	if err != nil {
		t.Fatal(err)
	}
	writeSamples(t, w, 400) // 800 bytes: still RIFF
	w.refresh()
	data, _ := os.ReadFile(path)
	if string(data[:4]) != "RIFF" || string(data[12:16]) != "JUNK" {
		t.Fatalf("small file: %q", data[:16])
	}
	writeSamples(t, w, 600) // 2000 bytes
	if err := w.finish(1000); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	le := binary.LittleEndian
	if string(data[:4]) != "RF64" || le.Uint32(data[4:]) != 0xFFFFFFFF || string(data[12:16]) != "ds64" ||
		le.Uint64(data[20:]) != uint64(len(data)-8) || le.Uint64(data[28:]) != 2000 || le.Uint64(data[36:]) != 1000 ||
		le.Uint32(data[76:]) != 0xFFFFFFFF {
		t.Fatalf("RF64 header: % x", data[:80])
	}
	info, err := ReadWAV(path)
	if err != nil || !info.RF64 || info.DataSize != 2000 || info.Frames != 1000 {
		t.Errorf("read back: %+v %v", info, err)
	}
}

// A recording cut short: the header is older than the data.
func TestReadWAVStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cut.wav")
	w, _ := createWAV(createFile, path, 48000, 24, 0)
	t.Cleanup(func() { w.f.Close() }) // before the temporary directory goes: Windows keeps an open file
	writeSamples(t, w, 100)
	w.refresh()
	writeSamples(t, w, 50)
	w.flush() // no header refresh: the program "died"
	info, err := ReadWAV(path)
	if err != nil || info.DataSize != 300 || info.Frames != 150 {
		t.Errorf("%+v %v", info, err)
	}
}

// wavStream builds the header ffmpeg writes on a pipe: extensible format,
// a LIST chunk, sizes unknown.
func wavStream(channels, rate, bits int) []byte {
	le := binary.LittleEndian
	var b bytes.Buffer
	b.WriteString("RIFF\xff\xff\xff\xffWAVE")
	fmtc := make([]byte, 40)
	le.PutUint16(fmtc[0:], 0xFFFE)
	le.PutUint16(fmtc[2:], uint16(channels))
	le.PutUint32(fmtc[4:], uint32(rate))
	le.PutUint32(fmtc[8:], uint32(rate*channels*bits/8))
	le.PutUint16(fmtc[12:], uint16(channels*bits/8))
	le.PutUint16(fmtc[14:], uint16(bits))
	le.PutUint16(fmtc[16:], 22)
	le.PutUint16(fmtc[18:], uint16(bits))
	le.PutUint16(fmtc[24:], 1) // PCM sub-format
	b.WriteString("fmt \x28\x00\x00\x00")
	b.Write(fmtc)
	b.WriteString("LIST\x05\x00\x00\x00abcde\x00") // odd size: padded
	b.WriteString("data\xff\xff\xff\xff")
	return b.Bytes()
}

func TestReadWAVHeader(t *testing.T) {
	info, err := readWAVHeader(bytes.NewReader(wavStream(32, 48000, 32)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := info.sampleFormat()
	if err != nil || f.name != "s32le" || info.Channels != 32 || info.Rate != 48000 || info.ValidBits != 32 {
		t.Errorf("%+v %v %v", info, f, err)
	}
	if _, err := readWAVHeader(bytes.NewReader([]byte("RIFF\x00\x00\x00\x00WAVEdata\x00\x00\x00\x00"))); err == nil {
		t.Error("data before fmt")
	}
}
