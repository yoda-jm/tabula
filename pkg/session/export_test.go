package session

import (
	"bytes"
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
)

// recordAudio writes a second of silence on two tracks into dir/audio.
func recordAudio(t *testing.T, dir string) audio.Info {
	t.Helper()
	src := func() (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		go pw.Write(make([]byte, 48000*2*4))
		return pr, nil
	}
	c, err := audio.Start(audio.Config{Recorder: "custom", Command: "x", Channels: 2}, filepath.Join(dir, "audio"),
		audio.Options{Names: []string{"Hang", ""}, Source: src})
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); c.Status().Frames < 48000; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("no audio: %+v", c.Status())
		}
	}
	c.Stop()
	return c.Info()
}

func TestExports(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "2026-10-17_203000")
	info := recordAudio(t, dir)
	info.Offset, info.DriftPPM = 0.5, 100
	start := time.Date(2026, 10, 17, 20, 30, 0, 0, time.UTC)
	s := &Session{Dir: dir, Meta: Meta{Start: start, Audio: &info}, Events: []Event{
		{T: 0.25, Marker: "Before the audio"},
		{T: 0.75, Addr: "/ch/01/mix/fader", Text: " -6.0", Value: 0.6, Origin: "console"},
		{T: 100.5001, Marker: `Song "2"`},
	}}

	var b bytes.Buffer
	if err := s.Export(&b, ExportAudacity); err != nil {
		t.Fatal(err)
	}
	// (100.5001 - 0.5) / 1.0001 = 99.9902
	if want := "0.000000\t0.000000\tBefore the audio\n99.990101\t99.990101\tSong \"2\"\n"; b.String() != want {
		t.Errorf("labels:\n%q\nwant\n%q", b.String(), want)
	}

	b.Reset()
	if err := s.Export(&b, ExportReaper); err != nil {
		t.Fatal(err)
	}
	rpp := b.String()
	for _, want := range []string{
		"<REAPER_PROJECT 0.1", "SAMPLERATE 48000 0 0\n",
		"MARKER 1 0.250025 \"Before the audio\" 0\n", "MARKER 2 100.490101 'Song \"2\"' 0\n",
		"NAME \"01 Hang\"\n", "POSITION 0.500000\n", "LENGTH 1.000000\n", "FILE \"audio/01 Hang.wav\"\n",
		"FILE \"audio/Track 02.wav\"\n",
	} {
		if !strings.Contains(rpp, want) {
			t.Errorf("Reaper project lacks %q:\n%s", want, rpp)
		}
	}
	if strings.Count(rpp, "<") != strings.Count(rpp, ">") {
		t.Errorf("unbalanced project:\n%s", rpp)
	}

	b.Reset()
	if err := s.Export(&b, ExportCSV); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&b).ReadAll()
	if err != nil || len(rows) != 4 {
		t.Fatalf("csv: %v %v", rows, err)
	}
	if got := strings.Join(rows[2], "|"); got != "0.750|0.250|2026-10-17T20:30:00.750Z|change|/ch/01/mix/fader|-6.0|0.6|console|" {
		t.Errorf("csv change: %s", got)
	}
	if rows[3][3] != "marker" || rows[3][8] != `Song "2"` {
		t.Errorf("csv marker: %v", rows[3])
	}

	if err := s.Export(&b, "pdf"); err == nil {
		t.Error("unknown export")
	}
	if err := s.WriteExports(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"2026-10-17_203000.rpp", "labels.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Error(err)
		}
	}
}

// Without audio, the exports use the session's times.
func TestExportsWithoutAudio(t *testing.T) {
	s := &Session{Dir: t.TempDir(), Events: []Event{{T: 12.5, Marker: "Song 2"}}}
	var b bytes.Buffer
	s.Export(&b, ExportAudacity)
	if b.String() != "12.500000\t12.500000\tSong 2\n" {
		t.Errorf("labels: %q", b.String())
	}
	b.Reset()
	s.Export(&b, ExportReaper)
	if !strings.Contains(b.String(), "MARKER 1 12.500000 \"Song 2\" 0") || strings.Contains(b.String(), "<TRACK") {
		t.Errorf("project: %s", b.String())
	}
}
