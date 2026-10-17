package audio

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

// Command templates of the recorders: {device}, {channels} and {rate} are
// replaced once the line is split into arguments, so a device name with
// spaces stays one argument.
//
// arecord writes raw samples: to its standard output, a WAV stream would end
// after 2 GiB (six minutes of 32 channels). Its buffer (0.5 s) absorbs a
// busy moment of the laptop; short periods (10 ms) time the samples finely.
// parecord records from PulseAudio or PipeWire. ffmpeg writes a WAV stream,
// whose header is checked against the configuration.
func Template(recorder, goos string) string {
	switch recorder {
	case "arecord":
		return "arecord -D {device} -c {channels} -r {rate} -f S32_LE -t raw --buffer-time=500000 --period-time=10000"
	case "parecord":
		return "parecord --raw --format=s32le --rate={rate} --channels={channels} --no-remix --no-remap --latency-msec=20 -d {device}"
	case "ffmpeg":
		in := "-f alsa -channels {channels} -sample_rate {rate} -i {device}"
		switch goos {
		case "darwin":
			in = "-f avfoundation -i :{device}"
		case "windows":
			in = "-f dshow -channels {channels} -sample_rate {rate} -audio_buffer_size 20 -i audio={device}"
		}
		return "ffmpeg -hide_banner -nostdin -nostats -loglevel warning " + in + " -c:a pcm_s32le -f wav -"
	}
	return ""
}

// defaultDevice is what an empty device means for each recorder.
func defaultDevice(recorder, goos string) string {
	switch recorder {
	case "arecord":
		return "default"
	case "parecord":
		return "@DEFAULT_SOURCE@"
	case "ffmpeg":
		switch goos {
		case "darwin":
			return "0"
		case "linux":
			return "default"
		}
	}
	return ""
}

// CommandLine returns the recorder's arguments for a normalized configuration.
func (c Config) CommandLine() ([]string, error) {
	return c.commandLine(runtime.GOOS)
}

func (c Config) commandLine(goos string) ([]string, error) {
	tmpl := c.Command
	if c.Recorder != "custom" {
		tmpl = Template(c.Recorder, goos)
	}
	if tmpl == "" {
		return nil, fmt.Errorf("no command for recorder %q", c.Recorder)
	}
	dev := c.Device
	if dev == "" {
		dev = defaultDevice(c.Recorder, goos)
	}
	if dev == "" && strings.Contains(tmpl, "{device}") {
		return nil, fmt.Errorf("choose a device for %s", c.Recorder)
	}
	args, err := splitArgs(tmpl)
	if err != nil {
		return nil, err
	}
	r := strings.NewReplacer("{device}", dev, "{channels}", strconv.Itoa(c.Channels), "{rate}", strconv.Itoa(c.Rate))
	for i, a := range args {
		args[i] = r.Replace(a)
	}
	return args, nil
}

// splitArgs splits a command line as a shell would, without expanding
// anything: arguments are separated by spaces, '…' and "…" quote.
// Backslashes are kept as they are (Windows paths).
func splitArgs(line string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg, quote := false, rune(0)
	for _, r := range line {
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			if r == '"' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inArg = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote in %q", quote, line)
	}
	if inArg {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return args, nil
}

// quoteArgs writes arguments back as a command line (for display).
func quoteArgs(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		switch {
		case a == "" || strings.ContainsAny(a, " \t\n\""):
			a = "'" + a + "'"
		case strings.Contains(a, "'"):
			a = `"` + a + `"`
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}
