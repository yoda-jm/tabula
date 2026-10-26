package audio

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Device is a capture device a recorder can record from.
type Device struct {
	Recorder string `json:"recorder"` // arecord, parecord or ffmpeg
	ID       string `json:"id"`       // what to give as the device
	Name     string `json:"name"`
	Channels int    `json:"channels,omitempty"` // capture channels, when known (the most)
	// MinChannels is the fewest channels the device records, when it takes
	// a range (an HDA card: 2 to 4); 0 when it is Channels.
	MinChannels int    `json:"min_channels,omitempty"`
	Format      string `json:"format,omitempty"` // e.g. "S32_LE, 24 bits"
	Rates       string `json:"rates,omitempty"`  // e.g. "44100, 48000"
	// Console is true for what looks like the console's USB card (X-USB,
	// DN32-USB).
	Console bool `json:"console,omitempty"`

	card string // ALSA card number of an arecord device ("1")
}

func (d Device) minChannels() int {
	if d.MinChannels > 0 {
		return d.MinChannels
	}
	return d.Channels
}

// channelRange is how many channels the device records: "32", "2 to 4".
func (d Device) channelRange() string {
	if lo := d.minChannels(); lo < d.Channels {
		return fmt.Sprintf("%d to %d", lo, d.Channels)
	}
	return strconv.Itoa(d.Channels)
}

var consoleCard = regexp.MustCompile(`(?i)x-?usb|dn32|m32|x32|midas|behringer`)

// runner runs a listing command and returns its output (both streams).
type runner func(ctx context.Context, name string, args ...string) (string, error)

func run(ctx context.Context, name string, args ...string) (string, error) {
	name = program(name)
	if _, err := exec.LookPath(name); err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// ListDevices lists the capture devices this machine's recorders know:
// ALSA cards (arecord -l, with what /proc/asound says of USB cards) and
// PulseAudio/PipeWire sources on Linux, ffmpeg's devices elsewhere. It only
// lists: no device is opened.
func ListDevices(ctx context.Context) []Device {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return listDevices(ctx, runtime.GOOS, run, os.ReadFile)
}

func listDevices(ctx context.Context, goos string, run runner, read func(string) ([]byte, error)) []Device {
	var out []Device
	switch goos {
	case "linux":
		if text, err := run(ctx, "arecord", "-l"); err == nil {
			out = append(out, parseArecordList(text, read)...)
		}
		if text, err := run(ctx, "pactl", "list", "short", "sources"); err == nil {
			out = append(out, parsePactlSources(text)...)
		}
	case "darwin":
		text, _ := run(ctx, "ffmpeg", "-hide_banner", "-f", "avfoundation", "-list_devices", "true", "-i", "")
		out = parseAVFoundation(text)
	case "windows":
		text, _ := run(ctx, "ffmpeg", "-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy")
		out = parseDShow(text)
	}
	for i := range out {
		out[i].Console = consoleCard.MatchString(out[i].Name + " " + out[i].ID)
	}
	return out
}

// ProbeChannels fills in the channels of the ALSA cards ListDevices could
// not tell (an HDA card has no /proc stream file): arecord opens each for
// an instant to say what it takes, and records nothing (256 channels are
// asked, which no card takes). A card in use stays unknown. Not while a
// capture starts: its card could be busy for an instant.
func ProbeChannels(ctx context.Context, devs []Device) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	probeChannels(ctx, devs, run)
}

func probeChannels(ctx context.Context, devs []Device, run runner) {
	for i := range devs {
		d := &devs[i]
		if d.Recorder != "arecord" || d.Channels > 0 || !strings.HasPrefix(d.ID, "hw:") {
			continue
		}
		out, _ := run(ctx, "arecord", "-D", d.ID, "--dump-hw-params", "-c", "256", "-d", "1", "-t", "raw", os.DevNull)
		if lo, hi, ok := parseHWChannels(out); ok {
			d.Channels = hi
			if lo < hi {
				d.MinChannels = lo
			}
		}
	}
}

// "CHANNELS: 2", "CHANNELS: [2 4]" in arecord --dump-hw-params
var hwChannels = regexp.MustCompile(`(?m)^CHANNELS: (?:(\d+)|([\[(])(\d+) (\d+)([\])]))\s*$`)

// parseHWChannels reads the channels a card takes, from the first dump.
func parseHWChannels(text string) (lo, hi int, ok bool) {
	m := hwChannels.FindStringSubmatch(text)
	switch {
	case m == nil:
		return 0, 0, false
	case m[1] != "":
		n, _ := strconv.Atoi(m[1])
		return n, n, n > 0
	}
	lo, _ = strconv.Atoi(m[3])
	hi, _ = strconv.Atoi(m[4])
	if m[2] == "(" {
		lo++
	}
	if m[5] == ")" {
		hi--
	}
	return lo, hi, lo > 0 && lo <= hi
}

// "card 1: XUSB [X-USB], device 0: USB Audio [USB Audio]"
var arecordCard = regexp.MustCompile(`^card (\d+): (\S+) \[(.*)\], device (\d+): (.*?) \[(.*)\]`)

// parseArecordList reads `arecord -l`: one hw: device per card and device.
func parseArecordList(text string, read func(string) ([]byte, error)) []Device {
	var out []Device
	for _, line := range strings.Split(text, "\n") {
		m := arecordCard.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		d := Device{Recorder: "arecord", ID: fmt.Sprintf("hw:CARD=%s,DEV=%s", m[2], m[4]), Name: m[3], card: m[1]}
		if m[6] != "" && m[6] != m[3] {
			d.Name += ", " + m[6]
		}
		if data, err := read(fmt.Sprintf("/proc/asound/card%s/stream%s", m[1], m[4])); err == nil {
			d.Channels, d.Format, d.Rates = parseUSBStream(string(data))
		}
		out = append(out, d)
	}
	return out
}

// parseUSBStream reads the capture side of /proc/asound/cardN/streamM, which
// USB cards have: the first capture format, channels and rates.
func parseUSBStream(text string) (channels int, format, rates string) {
	capture := false
	bits := ""
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case !strings.HasPrefix(line, " ") && strings.HasSuffix(t, ":"):
			capture = t == "Capture:"
			continue
		case !capture:
			continue
		}
		key, val, ok := strings.Cut(t, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "Format":
			if format == "" {
				format = val
			}
		case "Channels":
			if channels == 0 {
				channels, _ = strconv.Atoi(val)
			}
		case "Rates":
			if rates == "" {
				rates = val
			}
		case "Bits":
			if bits == "" {
				bits = val
			}
		}
	}
	if format != "" && bits != "" {
		format += ", " + bits + " bits"
	}
	return
}

// parsePactlSources reads `pactl list short sources`:
// "56	alsa_input.usb-…	PipeWire	s32le 32ch 48000Hz	SUSPENDED".
func parsePactlSources(text string) []Device {
	var out []Device
	for _, line := range strings.Split(text, "\n") {
		f := strings.Split(strings.TrimSpace(line), "\t")
		if len(f) < 4 || strings.HasSuffix(f[1], ".monitor") {
			continue
		}
		d := Device{Recorder: "parecord", ID: f[1], Name: f[1]}
		for _, w := range strings.Fields(f[3]) {
			switch {
			case strings.HasSuffix(w, "ch"):
				d.Channels, _ = strconv.Atoi(strings.TrimSuffix(w, "ch"))
			case strings.HasSuffix(w, "Hz"):
				d.Rates = strings.TrimSuffix(w, "Hz")
			default:
				d.Format = w
			}
		}
		out = append(out, d)
	}
	return out
}

// "[AVFoundation indev @ 0x7f…] [1] DN32-USB"
var avfDevice = regexp.MustCompile(`\] \[(\d+)\] (.+)$`)

// parseAVFoundation reads the audio devices ffmpeg lists on macOS.
func parseAVFoundation(text string) []Device {
	var out []Device
	audio := false
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "audio devices:") {
			audio = true
			continue
		}
		if strings.Contains(line, "video devices:") {
			audio = false
			continue
		}
		if m := avfDevice.FindStringSubmatch(strings.TrimSpace(line)); audio && m != nil {
			out = append(out, Device{Recorder: "ffmpeg", ID: m[1], Name: strings.TrimSpace(m[2])})
		}
	}
	return out
}

// `[dshow @ 0…] "Microphone (DN32-USB)" (audio)`, or, from older ffmpeg, a
// name line under "DirectShow audio devices".
var dshowDevice = regexp.MustCompile(`\]\s+"([^"]+)"\s*(\((audio|video|none)[^)]*\))?`)

// parseDShow reads the audio devices ffmpeg lists on Windows.
func parseDShow(text string) []Device {
	var out []Device
	section := ""
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, "DirectShow audio devices"):
			section = "audio"
			continue
		case strings.Contains(line, "DirectShow video devices"):
			section = "video"
			continue
		case strings.Contains(line, "Alternative name"):
			continue
		}
		m := dshowDevice.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		kind := m[3]
		if kind == "" {
			kind = section
		}
		if kind == "audio" {
			out = append(out, Device{Recorder: "ffmpeg", ID: m[1], Name: m[1]})
		}
	}
	return out
}
