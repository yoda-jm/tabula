package main

import (
	"flag"
	"path/filepath"
	"testing"

	"github.com/yoda-jm/tabula/pkg/audio"
)

func parseAudio(t *testing.T, args ...string) *audioFlags {
	t.Helper()
	fs := flag.NewFlagSet("tabula", flag.ContinueOnError)
	f := addAudioFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAudioFlags(t *testing.T) {
	saved := audio.Config{Enabled: true, Recorder: "parecord", Device: "saved", Channels: 32, Rate: 48000, Tracks: "1-8", Bits: 16}

	// No flag: the saved configuration.
	if c := parseAudio(t).config(saved); c != saved.Normalized() {
		t.Errorf("no flags: %+v", c)
	}
	c := parseAudio(t, "-audio-device", "hw:CARD=X32,DEV=0", "-audio-tracks", "1-16", "-audio-bits", "24").config(audio.DefaultConfig())
	if !c.Enabled || c.Device != "hw:CARD=X32,DEV=0" || c.Tracks != "1-16" || c.Bits != 24 || c.Recorder != audio.DefaultRecorder() {
		t.Errorf("device flags: %+v", c)
	}
	if c := parseAudio(t, "-audio=false", "-audio-device", "x").config(saved); c.Enabled {
		t.Errorf("-audio=false: %+v", c)
	}
	off := audio.DefaultConfig()
	if c := parseAudio(t, "-audio-tracks", "1-16").config(off); !c.Enabled {
		t.Errorf("-audio-tracks: not enabled")
	}
	if c := parseAudio(t, "-audio").config(off); !c.Enabled || c.Device != "" {
		t.Errorf("-audio: %+v", c)
	}
	c = parseAudio(t, "-audio-cmd", "rec -c {channels}", "-audio-input", "f32le").config(saved)
	if c.Recorder != "custom" || c.Command != "rec -c {channels}" || c.Input != "f32le" || c.Device != "saved" {
		t.Errorf("custom: %+v", c)
	}

	dir := t.TempDir()
	if _, _, err := parseAudio(t, "-audio-device", "x", "-audio-tracks", "40").load(dir); err == nil {
		t.Error("bad tracks accepted")
	}
	audio.SaveConfig(filepath.Join(dir, "audio.json"), saved)
	c, file, err := parseAudio(t, "-audio-bits", "32").load(dir)
	if err != nil || file != filepath.Join(dir, "audio.json") || c.Device != "saved" || c.Bits != 32 {
		t.Errorf("load: %+v %s %v", c, file, err)
	}
}
