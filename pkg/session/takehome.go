package session

import (
	"bytes"
	"cmp"
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
)

// Taking a show home: every file of a session's folder, for a zip download
// or a copy to a USB disk, the WAV tracks optionally converted to FLAC on
// the way. A conversion writes nothing on this machine's disk.

// FLACRatio is about the size of a show's tracks once in FLAC, to check a
// disk has room before converting: live tracks shrink to 50 to 65 %,
// silent ones to almost nothing.
const FLACRatio = 0.7

// HomeFile is one file of a session as it is taken home.
type HomeFile struct {
	Name string // slash path in the session's folder: "audio/01 Kick.wav", "audio/01 Kick.flac" once converted
	Path string // the file on this machine; "" for a file made on the way
	Size int64  // bytes read on this machine (the WAV file of a converted track)
	Mod  time.Time
	// FLAC is a WAV track converted on the way, Frames its length.
	FLAC   bool
	Frames int64
	// WAV is a FLAC track (a compressed session) decoded back into the WAV
	// it was, Out the size of that WAV.
	WAV  bool
	Out  int64
	data []byte // a file made on the way: the Reaper project of FLAC tracks
}

// Audio reports whether the file is a track (WAV or FLAC): zips store
// them as they are, they would not compress.
func (f HomeFile) Audio() bool {
	ext := strings.ToLower(path.Ext(f.Name))
	return ext == ".wav" || ext == ".flac"
}

// HomeFiles lists the files of the session's folder, the small ones first
// (an interrupted download still has the console's part), then the tracks.
// With flac, the WAV tracks become FLAC files and the Reaper project points
// at them.
func (s *Session) HomeFiles(flac bool) ([]HomeFile, error) {
	var out []HomeFile
	converted := map[string]string{} // track → its name taken home
	err := filepath.WalkDir(s.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.Dir, p)
		if err != nil {
			return err
		}
		if strings.HasSuffix(rel, ".part") || strings.HasSuffix(rel, ".tmp") { // being written (compressing, meta.json)
			return nil
		}
		f := HomeFile{Name: filepath.ToSlash(rel), Path: p, Size: info.Size(), Mod: info.ModTime()}
		if flac && strings.HasPrefix(f.Name, "audio/") && strings.EqualFold(path.Ext(f.Name), ".wav") {
			if w, err := audio.ReadWAV(p); err == nil && w.Format == 1 && w.Channels > 0 && w.DataSize > 0 &&
				(w.Bits == 16 || w.Bits == 24 || w.Bits == 32) {
				converted[f.Name] = flacName(f.Name)
				f.FLAC, f.Frames, f.Name = true, w.DataSize/int64(w.Channels*w.Bits/8), flacName(f.Name)
			}
		}
		if !flac && strings.HasPrefix(f.Name, "audio/") && audio.IsFLAC(f.Name) { // compressed: as recorded
			if size, err := audio.FLACAsWAVSize(p); err == nil {
				wav := strings.TrimSuffix(f.Name, path.Ext(f.Name)) + ".wav"
				converted[f.Name] = wav
				f.WAV, f.Out, f.Name = true, size, wav
			}
		}
		out = append(out, f)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(converted) > 0 && s.Meta.Audio != nil {
		var b bytes.Buffer
		if err := s.writeReaper(&b, converted); err != nil {
			return nil, err
		}
		rpp := s.Name() + ".rpp"
		out = slices.DeleteFunc(out, func(f HomeFile) bool { return f.Name == rpp })
		out = append(out, HomeFile{Name: rpp, Size: int64(b.Len()), Mod: time.Now(), data: b.Bytes()})
	}
	// one file per name: a WAV left over beside its FLAC (compressed) gives way
	seen := map[string]bool{}
	slices.SortStableFunc(out, func(a, b HomeFile) int { // the track meta.json names first
		return cmp.Compare(b2i(!s.names(a.Path)), b2i(!s.names(b.Path)))
	})
	out = slices.DeleteFunc(out, func(f HomeFile) bool {
		if seen[f.Name] {
			return true
		}
		seen[f.Name] = true
		return false
	})
	slices.SortFunc(out, func(a, b HomeFile) int {
		if a.Audio() != b.Audio() {
			if a.Audio() {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

// flacName is a WAV track's name once converted: "audio/01 Kick.flac".
func flacName(wav string) string { return strings.TrimSuffix(wav, path.Ext(wav)) + ".flac" }

// WriteTo writes the file's content to w, converted to FLAC for a converted
// track; read is told the bytes read on this machine as they are.
func (f HomeFile) WriteTo(ctx context.Context, w io.Writer, read func(n int64)) error {
	if f.Path == "" {
		_, err := w.Write(f.data)
		if read != nil {
			read(int64(len(f.data)))
		}
		return err
	}
	in, err := os.Open(f.Path)
	if err != nil {
		return err
	}
	defer in.Close()
	if f.WAV {
		in.Close()
		return audio.WriteFLACAsWAV(ctx, w, f.Path, read)
	}
	r := &progressReader{ctx: ctx, r: in, read: read}
	if f.FLAC {
		return audio.EncodeFLAC(ctx, w, r, f.Frames)
	}
	_, err = io.Copy(w, r)
	return err
}

// progressReader tells what it reads, and stops when ctx is done.
type progressReader struct {
	ctx  context.Context
	r    io.Reader
	read func(n int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.r.Read(b)
	if n > 0 && p.read != nil {
		p.read(int64(n))
	}
	return n, err
}

// HomeSize is the bytes of the files taken home: exact, or about for FLAC
// tracks (FLACRatio).
func HomeSize(files []HomeFile) (size int64, exact bool) {
	exact = true
	for _, f := range files {
		switch {
		case f.FLAC:
			size += int64(float64(f.Size) * FLACRatio)
			exact = false
		case f.WAV:
			size += f.Out
		default:
			size += f.Size
		}
	}
	return size, exact
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// names tells whether meta.json names the file at path as a track.
func (s *Session) names(path string) bool {
	if s.Meta.Audio == nil || path == "" {
		return false
	}
	for _, t := range s.Meta.Audio.Tracks {
		if t.File != "" && filepath.Join(s.Dir, filepath.FromSlash(t.File)) == path {
			return true
		}
	}
	return false
}
