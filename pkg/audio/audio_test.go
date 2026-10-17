package audio

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseTracks(t *testing.T) {
	for _, c := range []struct {
		spec string
		want []int
	}{
		{"", []int{1, 2, 3, 4}},
		{"all", []int{1, 2, 3, 4}},
		{"1-2", []int{1, 2}},
		{"4, 1-2,2", []int{1, 2, 4}},
		{"3;1", []int{1, 3}},
	} {
		got, err := ParseTracks(c.spec, 4)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseTracks(%q) = %v, %v; want %v", c.spec, got, err, c.want)
		}
	}
	for _, bad := range []string{"0", "5", "3-1", "a", "1-", ",", "1-99"} {
		if got, err := ParseTracks(bad, 4); err == nil {
			t.Errorf("ParseTracks(%q) = %v, want an error", bad, got)
		}
	}
	if s := FormatTracks([]int{1, 2, 3, 5, 7, 8}); s != "1-3,5,7-8" {
		t.Errorf("FormatTracks: %q", s)
	}
}

func TestConfig(t *testing.T) {
	c := Config{Tracks: "1-16"}.Normalized()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	// 16 tracks of 24 bits at 48 kHz: 2.3 MB/s, 12.4 GB for two sets of 45 min.
	if r := c.ByteRate(); r != 16*48000*3 {
		t.Errorf("byte rate %d", r)
	}
	for _, bad := range []Config{
		{Recorder: "sox"}, {Recorder: "custom"}, {Bits: 20}, {Channels: 200}, {Rate: 1000},
		{Tracks: "33"}, {Recorder: "custom", Command: "x", Input: "s20le"},
	} {
		if err := bad.Normalized().Validate(); err == nil {
			t.Errorf("%+v: no error", bad)
		}
	}
}

func TestCommandLine(t *testing.T) {
	c := Config{Recorder: "arecord", Device: "hw:CARD=X32,DEV=0", Channels: 32, Rate: 48000, Bits: 24}
	args, err := c.commandLine("linux")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(args, " "); got != "arecord -D hw:CARD=X32,DEV=0 -c 32 -r 48000 -f S32_LE -t raw --buffer-time=500000 --period-time=10000" {
		t.Errorf("arecord: %s", got)
	}
	c.Device = ""
	args, _ = c.commandLine("linux")
	if args[2] != "default" {
		t.Errorf("default device: %v", args)
	}
	// A device with spaces stays one argument.
	c.Recorder, c.Device = "ffmpeg", "Microphone (DN32-USB)"
	args, _ = c.commandLine("windows")
	if !contains(args, "audio=Microphone (DN32-USB)") || args[len(args)-1] != "-" {
		t.Errorf("dshow: %q", args)
	}
	if q := quoteArgs(args); !strings.Contains(q, "'audio=Microphone (DN32-USB)'") {
		t.Errorf("quoted: %s", q)
	}
	c.Device = ""
	if _, err := c.commandLine("windows"); err == nil {
		t.Error("dshow without a device")
	}
	c.Recorder, c.Command = "custom", `"C:\Program Files\rec.exe" -d '{device}' -n {channels}`
	c.Device = "my card"
	args, _ = c.commandLine("windows")
	if !reflect.DeepEqual(args, []string{`C:\Program Files\rec.exe`, "-d", "my card", "-n", "32"}) {
		t.Errorf("custom: %q", args)
	}
	if _, err := splitArgs(`a "b`); err == nil {
		t.Error("unterminated quote")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestSamples(t *testing.T) {
	get := func(name string, b ...byte) int32 {
		f, err := parseFormat(name)
		if err != nil {
			t.Fatal(err)
		}
		return f.sample(b)
	}
	for _, c := range []struct {
		got, want int32
	}{
		{get("s16le", 0x34, 0x12), 0x12340000},
		{get("s16le", 0x00, 0x80), -1 << 31},
		{get("s24le", 0x56, 0x34, 0x12), 0x12345600},
		{get("s24le", 0xff, 0xff, 0xff), -256},
		{get("s24-32le", 0x56, 0x34, 0x12, 0x00), 0x12345600},
		{get("s24-32le", 0x00, 0x00, 0x80, 0xff), -1 << 31},
		{get("s32le", 0x78, 0x56, 0x34, 0x12), 0x12345678},
		{get("f32le", 0, 0, 0, 0x3f), 1 << 30},      // 0.5
		{get("f32le", 0, 0, 0x80, 0x3f), 1<<31 - 1}, // 1.0 clips
		{get("f32le", 0, 0, 0x80, 0xbf), -1 << 31},  // -1.0
	} {
		if c.got != c.want {
			t.Errorf("got %#x, want %#x", c.got, c.want)
		}
	}
	b := make([]byte, 4)
	putSample(b, 0x12345678, 3)
	if b[0] != 0x56 || b[1] != 0x34 || b[2] != 0x12 {
		t.Errorf("24 bits: % x", b[:3])
	}
	putSample(b, -1<<31, 2)
	if b[0] != 0 || b[1] != 0x80 {
		t.Errorf("16 bits: % x", b[:2])
	}
}

func TestFileName(t *testing.T) {
	for name, want := range map[string]string{
		"Hang":        "01 Hang.wav",
		"":            "Track 01.wav",
		"-":           "Track 01.wav",
		" Vx Rémy ":   "01 Vx Rémy.wav",
		"AC/DC: live": "01 AC_DC_ live.wav",
		"..":          "Track 01.wav",
	} {
		if got := FileName(1, name); got != want {
			t.Errorf("FileName(%q) = %q, want %q", name, got, want)
		}
	}
}
