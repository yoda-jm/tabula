package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/yoda-jm/tabula/pkg/audio"
)

// audioFlags record the console's USB audio with every session. The
// settings made in the GUI are kept in <sessions>/audio.json; the flags
// given on the command line override them, and turn audio on (unless
// -audio=false).
type audioFlags struct {
	fs       *flag.FlagSet
	on       *bool
	device   *string
	channels *int
	tracks   *string
	bits     *int
	rate     *int
	recorder *string
	cmd      *string
	input    *string
	plain    *bool
	list     *bool
}

func addAudioFlags(fs *flag.FlagSet) *audioFlags {
	return &audioFlags{
		fs:       fs,
		on:       fs.Bool("audio", false, "record the audio tracks with every session; any -audio-… flag implies it (-audio=false: not this time)"),
		device:   fs.String("audio-device", "", "audio device: ALSA device for arecord (hw:CARD=X32,DEV=0), source for parecord, device for ffmpeg"),
		channels: fs.Int("audio-channels", 32, "channels the audio device delivers"),
		tracks:   fs.String("audio-tracks", "", `channels to record, e.g. "1-16" or "1-16,23-24" (default all)`),
		bits:     fs.Int("audio-bits", 24, "bits per sample of the WAV files: 16, 24 or 32"),
		rate:     fs.Int("audio-rate", 48000, "sample rate of the audio device"),
		recorder: fs.String("audio-recorder", audio.DefaultRecorder(), "program recording the audio: arecord, parecord, ffmpeg or custom (-audio-cmd)"),
		cmd:      fs.String("audio-cmd", "", "recorder command ({device}, {channels}, {rate} replaced) writing raw samples (-audio-input) or WAV on its output; implies -audio-recorder custom"),
		input:    fs.String("audio-input", "s32le", "sample format of a raw stream from -audio-cmd: s16le, s24le, s24-32le, s32le or f32le"),
		plain:    fs.Bool("audio-plain-names", false, `name the files "Track 01.wav"…, not after the console's routing`),
		list:     fs.Bool("audio-list", false, "list the audio capture devices and exit"),
	}
}

// config applies the flags given on the command line to base.
func (f *audioFlags) config(base audio.Config) audio.Config {
	c := base
	f.fs.Visit(func(fl *flag.Flag) {
		if strings.HasPrefix(fl.Name, "audio-") && fl.Name != "audio-list" {
			c.Enabled = true
		}
		switch fl.Name {
		case "audio-device":
			c.Device = *f.device
		case "audio-channels":
			c.Channels = *f.channels
		case "audio-tracks":
			c.Tracks = *f.tracks
		case "audio-bits":
			c.Bits = *f.bits
		case "audio-rate":
			c.Rate = *f.rate
		case "audio-cmd":
			c.Command, c.Recorder = *f.cmd, "custom"
		case "audio-input":
			c.Input = *f.input
		case "audio-plain-names":
			c.PlainNames = *f.plain
		}
	})
	// explicit choices last: -audio=false wins over -audio-device
	f.fs.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "audio":
			c.Enabled = *f.on
		case "audio-recorder":
			c.Recorder = *f.recorder
		}
	})
	return c.Normalized()
}

// load reads the configuration saved in the sessions directory and applies
// the flags; it returns it with the file to save changes to.
func (f *audioFlags) load(sessions string) (audio.Config, string, error) {
	file := filepath.Join(sessions, "audio.json")
	base, err := audio.LoadConfig(file)
	if err != nil {
		return base, file, err
	}
	c := f.config(base)
	if c.Enabled {
		if err := c.Validate(); err != nil {
			return c, file, err
		}
	}
	return c, file, nil
}

func listAudioDevices(w io.Writer) {
	devs := audio.ListDevices(context.Background())
	if len(devs) == 0 {
		fmt.Fprintln(w, "No capture device found (arecord, pactl or ffmpeg list them). Give -audio-device by hand.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "RECORDER\tDEVICE\tCHANNELS\tFORMAT\tNAME")
	for _, d := range devs {
		ch, mark := "", ""
		if d.Channels > 0 {
			ch = fmt.Sprint(d.Channels)
		}
		if d.Console {
			mark = "  <- console card?"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s%s\n", d.Recorder, d.ID, ch, d.Format, d.Name, mark)
	}
	tw.Flush()
}
