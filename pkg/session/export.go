package session

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
)

// Exports of a session for audio editors: a Reaper project (the audio
// tracks at their place, the markers), an Audacity label track (the
// markers, at their time in the audio files) and a CSV of every change and
// marker; and the console at the start, as a scene file for M32-Edit.
const (
	ExportReaper   = "reaper"
	ExportAudacity = "audacity"
	ExportCSV      = "csv"
	ExportScene    = "scene"
)

// Name is the session's name: its directory.
func (s *Session) Name() string { return filepath.Base(s.Dir) }

// ExportFile is the file name of an export, as written in the session
// directory.
func (s *Session) ExportFile(format string) (string, error) {
	switch format {
	case ExportReaper:
		return s.Name() + ".rpp", nil
	case ExportAudacity:
		return "labels.txt", nil
	case ExportCSV:
		return "events.csv", nil
	case ExportScene:
		return "start.scn", nil
	}
	return "", fmt.Errorf("unknown export %q (reaper, audacity, csv, scene)", format)
}

// Export writes an export of the session.
func (s *Session) Export(w io.Writer, format string) error {
	switch format {
	case ExportReaper:
		return s.writeReaper(w, nil)
	case ExportAudacity:
		return s.writeLabels(w)
	case ExportCSV:
		return s.writeCSV(w)
	case ExportScene:
		f, err := os.Open(filepath.Join(s.Dir, "start.scn"))
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	}
	_, err := s.ExportFile(format)
	return err
}

// WriteExports writes the Reaper project and the label track into the
// session directory, next to the audio directory the project refers to.
func (s *Session) WriteExports() error {
	for _, f := range []string{ExportReaper, ExportAudacity} {
		name, _ := s.ExportFile(f)
		out, err := os.Create(filepath.Join(s.Dir, name))
		if err != nil {
			return err
		}
		err = s.Export(out, f)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// AudioTime is where session time t falls in the audio files (t itself
// without audio).
func (s *Session) AudioTime(t float64) float64 {
	if s.Meta.Audio == nil {
		return t
	}
	return s.Meta.Audio.AudioTime(t)
}

// writeReaper writes a Reaper project whose timeline is the session's: each
// audio file is an item on its own track, starting when its first sample
// was captured, and the markers are project markers at the place of the
// audio they were set on. Paths are relative to the session directory:
// the project opens from there. The tracks in flac are FLAC files instead
// of WAV files (a session taken home converted, takehome.go).
func (s *Session) writeReaper(w io.Writer, flac map[string]bool) error {
	a := s.Meta.Audio
	rate := 48000
	offset := 0.0
	if a != nil {
		rate, offset = a.Rate, a.Offset
	}
	shift := math.Max(0, -offset) // Reaper's timeline starts at 0
	b := &strings.Builder{}
	fmt.Fprintf(b, "<REAPER_PROJECT 0.1 \"6.0/tabula\" %d\n", s.Meta.Start.Unix())
	fmt.Fprintf(b, "  SAMPLERATE %d 0 0\n", rate)
	for i, m := range s.Markers() {
		pos := shift + offset + s.AudioTime(m.T)
		fmt.Fprintf(b, "  MARKER %d %s %s 0\n", i+1, seconds(math.Max(0, pos)), rppString(m.Marker))
	}
	if a != nil {
		for _, t := range a.Tracks {
			frames := a.Frames
			if info, err := audio.ReadWAV(filepath.Join(s.Dir, filepath.FromSlash(t.File))); err == nil {
				frames = info.Frames
			}
			name := strings.TrimSuffix(filepath.Base(t.File), filepath.Ext(t.File))
			fmt.Fprintf(b, "  <TRACK\n    NAME %s\n", rppString(name))
			file, source := t.File, "WAVE"
			if flac[t.File] {
				file, source = flacName(t.File), "FLAC"
			}
			fmt.Fprintf(b, "    <ITEM\n      POSITION %s\n      LENGTH %s\n      NAME %s\n",
				seconds(shift+offset), seconds(float64(frames)/float64(a.Rate)), rppString(path.Base(file)))
			fmt.Fprintf(b, "      <SOURCE %s\n        FILE %s\n      >\n    >\n  >\n", source, rppString(file))
		}
	}
	b.WriteString(">\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// rppString quotes a string as Reaper does: with ", ' or ` (whichever it
// does not hold).
func rppString(s string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
	for _, q := range []string{`"`, `'`, "`"} {
		if !strings.Contains(s, q) {
			return q + s + q
		}
	}
	return `"` + strings.ReplaceAll(s, `"`, `'`) + `"`
}

func seconds(t float64) string { return strconv.FormatFloat(t, 'f', 6, 64) }

// writeLabels writes an Audacity label track: one point label per marker,
// "start<TAB>end<TAB>label", at its time in the audio files (Audacity puts
// imported files at 0).
func (s *Session) writeLabels(w io.Writer) error {
	b := &strings.Builder{}
	for _, m := range s.Markers() {
		t := seconds(math.Max(0, s.AudioTime(m.T)))
		label := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(m.Marker)
		fmt.Fprintf(b, "%s\t%s\t%s\n", t, t, label)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeCSV writes every change and marker: session time, time in the audio
// files, wall clock time, and what happened.
func (s *Session) writeCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	cw.Write([]string{"time", "audio_time", "clock", "type", "address", "text", "value", "origin", "label"})
	for _, e := range s.Events {
		at := ""
		if s.Meta.Audio != nil {
			at = strconv.FormatFloat(s.AudioTime(e.T), 'f', 3, 64)
		}
		clock := s.Meta.Start.Add(time.Duration(e.T * float64(time.Second))).Format("2006-01-02T15:04:05.000Z07:00")
		kind, value := "change", ""
		if e.Marker != "" {
			kind = "marker"
		}
		if e.Value != nil {
			value = fmt.Sprint(e.Value)
		}
		cw.Write([]string{strconv.FormatFloat(e.T, 'f', 3, 64), at, clock, kind, e.Addr, strings.TrimSpace(e.Text), value, e.Origin, e.Marker})
	}
	cw.Flush()
	return cw.Error()
}
