package session

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
)

// wav24 is a mono 24-bit WAV file of frames samples.
func wav24(frames int) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	b.WriteString("RIFF")
	binary.Write(&b, le, uint32(36+frames*3))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(48000), uint32(48000 * 3), uint16(3), uint16(24)} {
		binary.Write(&b, le, v)
	}
	b.WriteString("data")
	binary.Write(&b, le, uint32(frames*3))
	for i := range frames {
		b.Write([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
	}
	return b.Bytes()
}

// homeSession writes a session of two tracks of 4800 samples, its exports
// written as at the end of a recording.
func homeSession(t *testing.T) *Session {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "2026-10-25_203512")
	os.MkdirAll(filepath.Join(dir, "audio"), 0o755)
	m := Meta{Format: 1, Start: time.Date(2026, 10, 25, 20, 35, 12, 0, time.UTC), Duration: 0.1,
		Audio: &audio.Info{Rate: 48000, Bits: 24, Channels: 32, Frames: 4800,
			Tracks: []audio.Track{{Channel: 1, Name: "Kick", File: "audio/01 Kick.wav"}, {Channel: 2, Name: "Snare", File: "audio/02 Snare.wav"}}}}
	data, _ := json.Marshal(m)
	for name, content := range map[string][]byte{
		"meta.json":          data,
		"events.jsonl":       []byte(`{"t":0.05,"marker":"Song 1"}` + "\n"),
		"start.scn":          []byte("#4.0# \"tabula\"\n/ch/01/config \"Kick\" 1 RD 1\n"),
		"audio/01 Kick.wav":  wav24(4800),
		"audio/02 Snare.wav": wav24(4800),
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteExports(); err != nil {
		t.Fatal(err)
	}
	return s
}

func names(files []HomeFile) string {
	var out []string
	for _, f := range files {
		out = append(out, f.Name)
	}
	return strings.Join(out, ", ")
}

func TestHomeFiles(t *testing.T) {
	s := homeSession(t)
	files, err := s.HomeFiles(false)
	if err != nil {
		t.Fatal(err)
	}
	// the console's part first: an interrupted download still has it
	want := "2026-10-25_203512.rpp, events.jsonl, labels.txt, meta.json, start.scn, audio/01 Kick.wav, audio/02 Snare.wav"
	if got := names(files); got != want {
		t.Errorf("files:\n%s\nwant\n%s", got, want)
	}
	if size, exact := HomeSize(files); !exact || size < 2*4800*3 {
		t.Errorf("size %d %v", size, exact)
	}
	var wav bytes.Buffer
	if err := files[5].WriteTo(context.Background(), &wav, nil); err != nil || !bytes.Equal(wav.Bytes(), wav24(4800)) {
		t.Errorf("WAV taken home as it is: %v, %d bytes", err, wav.Len())
	}

	files, err = s.HomeFiles(true)
	if err != nil {
		t.Fatal(err)
	}
	want = strings.ReplaceAll(want, ".wav", ".flac")
	if got := names(files); got != want {
		t.Errorf("FLAC files:\n%s\nwant\n%s", got, want)
	}
	if f := files[5]; !f.FLAC || f.Frames != 4800 || f.Size != int64(len(wav24(4800))) {
		t.Errorf("converted track: %+v", f)
	}
	if size, exact := HomeSize(files); exact || size >= 2*4800*3 {
		t.Errorf("FLAC size %d %v: about, smaller", size, exact)
	}
	// the Reaper project points at the FLAC files
	var rpp bytes.Buffer
	files[0].WriteTo(context.Background(), &rpp, nil)
	if p := rpp.String(); !strings.Contains(p, "<SOURCE FLAC\n        FILE \"audio/01 Kick.flac\"") || strings.Contains(p, ".wav") {
		t.Errorf("Reaper project:\n%s", p)
	}
	if !files[5].Audio() || files[0].Audio() {
		t.Error("Audio()")
	}
	if !audio.FLACAvailable() {
		return
	}
	var flac bytes.Buffer
	read := int64(0)
	if err := files[5].WriteTo(context.Background(), &flac, func(n int64) { read += n }); err != nil ||
		!bytes.HasPrefix(flac.Bytes(), []byte("fLaC")) || read != files[5].Size {
		t.Errorf("FLAC track: %v, %d bytes, %d read", err, flac.Len(), read)
	}
}
