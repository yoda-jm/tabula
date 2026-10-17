// Package audio records the tracks of the console's USB card along with a
// session. An external recorder (arecord, parecord or ffmpeg) streams the
// card's channels as interleaved PCM on its standard output, and each
// selected channel is written to its own WAV file, named after what feeds it
// on the console ("01 Hang.wav").
//
// The files stay readable if the program dies: the sizes in their headers
// are refreshed every few seconds. A file growing past 4 GiB becomes RF64
// (EBU Tech 3306), which Audacity, Reaper and Ardour read.
//
// The time of the first sample is estimated from the arrival times of the
// samples (see clock.go), so the tracks line up with the changes and markers
// of the session.
package audio

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Config chooses what to record and how.
type Config struct {
	// Enabled records audio with every session.
	Enabled bool `json:"enabled"`
	// Recorder is the program that captures: arecord, parecord, ffmpeg, or
	// custom (Command).
	Recorder string `json:"recorder"`
	// Device is what the recorder records from: an ALSA device for arecord
	// (hw:CARD=X32,DEV=0), a source for parecord, an avfoundation or dshow
	// device for ffmpeg. Empty: the recorder's default.
	Device string `json:"device"`
	// Channels the device delivers, recorded or not (32 for the card).
	Channels int `json:"channels"`
	Rate     int `json:"rate"`
	// Tracks are the channels written to files, e.g. "1-16" or "1-8,12";
	// empty for all.
	Tracks string `json:"tracks,omitempty"`
	// Bits per sample of the files: 16, 24 or 32.
	Bits int `json:"bits"`
	// Command is the command line of the custom recorder, where {device},
	// {channels} and {rate} are replaced. It writes the stream on its
	// standard output: raw samples in the Input format, or a WAV stream.
	Command string `json:"command,omitempty"`
	// Input is the sample format of a raw stream from a custom command:
	// s16le, s24le (3 bytes), s24-32le (24 bits in 4 bytes, ALSA's S24_LE),
	// s32le (default) or f32le.
	Input string `json:"input,omitempty"`
}

// Recorders known by name; "custom" runs Config.Command.
var Recorders = []string{"arecord", "parecord", "ffmpeg", "custom"}

// DefaultRecorder is arecord on Linux, ffmpeg elsewhere.
func DefaultRecorder() string {
	if runtime.GOOS == "linux" {
		return "arecord"
	}
	return "ffmpeg"
}

// DefaultConfig records every channel of a 32-channel card at 48 kHz into
// 24-bit files (not enabled).
func DefaultConfig() Config {
	return Config{Recorder: DefaultRecorder(), Channels: 32, Rate: 48000, Bits: 24}
}

// Normalized fills the zero fields with their defaults.
func (c Config) Normalized() Config {
	d := DefaultConfig()
	if c.Recorder == "" {
		c.Recorder = d.Recorder
	}
	if c.Channels == 0 {
		c.Channels = d.Channels
	}
	if c.Rate == 0 {
		c.Rate = d.Rate
	}
	if c.Bits == 0 {
		c.Bits = d.Bits
	}
	c.Device = strings.TrimSpace(c.Device)
	c.Tracks = strings.TrimSpace(c.Tracks)
	c.Command = strings.TrimSpace(c.Command)
	c.Input = strings.TrimSpace(c.Input)
	return c
}

// Validate checks a normalized configuration.
func (c Config) Validate() error {
	known := false
	for _, r := range Recorders {
		known = known || r == c.Recorder
	}
	switch {
	case !known:
		return fmt.Errorf("unknown recorder %q (one of %s)", c.Recorder, strings.Join(Recorders, ", "))
	case c.Recorder == "custom" && c.Command == "":
		return errors.New("the custom recorder needs a command")
	case c.Channels < 1 || c.Channels > 128:
		return fmt.Errorf("%d channels: expected 1 to 128", c.Channels)
	case c.Rate < 8000 || c.Rate > 192000:
		return fmt.Errorf("rate %d Hz: expected 8000 to 192000", c.Rate)
	case c.Bits != 16 && c.Bits != 24 && c.Bits != 32:
		return fmt.Errorf("%d bits: expected 16, 24 or 32", c.Bits)
	}
	if c.Input != "" {
		if _, err := parseFormat(c.Input); err != nil {
			return err
		}
	}
	_, err := ParseTracks(c.Tracks, c.Channels)
	return err
}

// ByteRate is the number of bytes per second written to the files.
func (c Config) ByteRate() int64 {
	tracks, err := ParseTracks(c.Tracks, c.Channels)
	if err != nil {
		return 0
	}
	return int64(len(tracks)) * int64(c.Rate) * int64(c.Bits/8)
}

// ParseTracks reads a list of channels such as "1-16" or "1-8, 12, 17-20"
// (empty or "all": every channel), sorted and without duplicates.
func ParseTracks(spec string, channels int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.EqualFold(spec, "all") {
		out := make([]int, channels)
		for i := range out {
			out[i] = i + 1
		}
		return out, nil
	}
	seen := map[int]bool{}
	for _, part := range strings.FieldsFunc(spec, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(lo)
		if err != nil {
			return nil, fmt.Errorf("tracks %q: %q is not a channel number", spec, part)
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(hi); err != nil {
				return nil, fmt.Errorf("tracks %q: %q is not a range", spec, part)
			}
		}
		if a < 1 || b > channels || a > b {
			return nil, fmt.Errorf("tracks %q: %s is outside 1-%d", spec, part, channels)
		}
		for ch := a; ch <= b; ch++ {
			seen[ch] = true
		}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("tracks %q: no channel", spec)
	}
	out := make([]int, 0, len(seen))
	for ch := range seen {
		out = append(out, ch)
	}
	sort.Ints(out)
	return out, nil
}

// FormatTracks writes channels back as ranges: [1 2 3 5] → "1-3,5".
func FormatTracks(tracks []int) string {
	var b strings.Builder
	for i := 0; i < len(tracks); {
		j := i
		for j+1 < len(tracks) && tracks[j+1] == tracks[j]+1 {
			j++
		}
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(tracks[i]))
		if j > i {
			b.WriteString("-" + strconv.Itoa(tracks[j]))
		}
		i = j + 1
	}
	return b.String()
}

// sampleFormat is how one sample is stored in the stream from the recorder.
type sampleFormat struct {
	name  string
	size  int // bytes
	float bool
	pad24 bool // 24 bits in the low three bytes of four (ALSA S24_LE)
}

var formats = []sampleFormat{
	{name: "s16le", size: 2},
	{name: "s24le", size: 3},
	{name: "s24-32le", size: 4, pad24: true},
	{name: "s32le", size: 4},
	{name: "f32le", size: 4, float: true},
}

func parseFormat(name string) (sampleFormat, error) {
	for _, f := range formats {
		if strings.EqualFold(f.name, name) {
			return f, nil
		}
	}
	names := make([]string, len(formats))
	for i, f := range formats {
		names[i] = f.name
	}
	return sampleFormat{}, fmt.Errorf("unknown sample format %q (one of %s)", name, strings.Join(names, ", "))
}

// sample reads one sample as a 32-bit value, full scale at ±2³¹.
func (f sampleFormat) sample(b []byte) int32 {
	switch {
	case f.size == 2:
		return int32(int16(uint16(b[0])|uint16(b[1])<<8)) << 16
	case f.size == 3:
		return int32(uint32(b[0])<<8 | uint32(b[1])<<16 | uint32(b[2])<<24)
	}
	u := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	switch {
	case f.pad24:
		return int32(u << 8)
	case f.float:
		v := float64(math.Float32frombits(u))
		switch {
		case v >= 1:
			return 1<<31 - 1
		case v <= -1:
			return -1 << 31
		case v != v: // NaN
			return 0
		}
		return int32(v * (1 << 31))
	}
	return int32(u)
}

// putSample writes the top bits of a sample (full scale ±2³¹) in little
// endian, truncated to bytes ∈ {2, 3, 4}.
func putSample(b []byte, s int32, bytes int) {
	u := uint32(s)
	switch bytes {
	case 2:
		b[0], b[1] = byte(u>>16), byte(u>>24)
	case 3:
		b[0], b[1], b[2] = byte(u>>8), byte(u>>16), byte(u>>24)
	default:
		b[0], b[1], b[2], b[3] = byte(u), byte(u>>8), byte(u>>16), byte(u>>24)
	}
}
