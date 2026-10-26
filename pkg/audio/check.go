package audio

import (
	"fmt"
	"regexp"
	"strings"
)

// Check is what this machine says of the device of a configuration, before
// recording: whether it is there and is the console's card.
type Check struct {
	// Problems are why a recording would probably not get the console's
	// tracks, for people.
	Problems []string `json:"problems,omitempty"`
	// Console is the console's card when it is connected and is not the
	// device configured: what to record from instead.
	Console *Device `json:"console,omitempty"`
}

var (
	alsaNamed = regexp.MustCompile(`CARD=([^,]+)(?:,DEV=(\d+))?`)      // hw:CARD=DN32USB,DEV=0, sysdefault:CARD=PCH
	alsaHW    = regexp.MustCompile(`^(?:plug)?hw:([^,=]+)(?:,(\d+))?`) // hw:1,0, plughw:XUSB
)

// alsaCardOf is the card (name or number) and device of an ALSA device name.
func alsaCardOf(dev string) (card, num string, ok bool) {
	m := alsaNamed.FindStringSubmatch(dev)
	if m == nil {
		m = alsaHW.FindStringSubmatch(dev)
	}
	if m == nil {
		return "", "", false
	}
	if m[2] == "" {
		m[2] = "0"
	}
	return m[1], m[2], true
}

// lookup finds the device of cfg among the devices connected
// (ListDevices). It knows ALSA devices (arecord, and ffmpeg on Linux) and
// PulseAudio/PipeWire sources (parecord): kind is "arecord" or "parecord",
// or "" for the other configurations, which are not looked for. known tells
// whether dev names a card or a source to look for ("default" does not);
// console is the console's card, when connected.
type lookup struct {
	kind, dev      string
	known          bool
	found, console *Device
}

func findDevice(cfg Config, devs []Device, goos string) lookup {
	cfg = cfg.Normalized()
	var l lookup
	l.kind = cfg.Recorder
	if l.kind == "ffmpeg" && goos == "linux" {
		l.kind = "arecord" // ffmpeg -f alsa takes the ALSA names
	}
	if goos != "linux" || l.kind != "arecord" && l.kind != "parecord" {
		l.kind = ""
		return l
	}
	for i := range devs {
		if devs[i].Console && devs[i].Recorder == l.kind {
			l.console = &devs[i]
			break
		}
	}
	l.dev = cfg.Device
	if l.dev == "" {
		l.dev = defaultDevice(cfg.Recorder, goos)
	}
	if l.kind == "arecord" {
		if card, num, ok := alsaCardOf(l.dev); ok {
			l.known = true
			for i := range devs {
				d := &devs[i]
				dc, dn, _ := alsaCardOf(d.ID)
				if d.Recorder == "arecord" && (dc == card || d.card == card) && dn == num {
					l.found = d
					break
				}
			}
		}
	} else if l.dev != "@DEFAULT_SOURCE@" {
		l.known = true
		for i := range devs {
			if devs[i].Recorder == "parecord" && devs[i].ID == l.dev {
				l.found = &devs[i]
				break
			}
		}
	}
	return l
}

// CheckDevice compares the device of cfg with the devices connected
// (ListDevices), for ALSA devices and PulseAudio/PipeWire sources; other
// configurations, and a machine where nothing is listed, pass. With
// PlainNames (a device that is not the console's card, on purpose) only a
// missing device is a problem.
func CheckDevice(cfg Config, devs []Device, goos string) Check {
	cfg = cfg.Normalized()
	l := findDevice(cfg, devs, goos)
	if !cfg.Enabled || len(devs) == 0 || l.kind == "" {
		return Check{}
	}
	var c Check
	notConsole := func(what string) {
		if cfg.PlainNames {
			return
		}
		if l.console != nil {
			c.Problems = append(c.Problems, fmt.Sprintf("%s, not the console's card: %s (%s) is connected.", what, l.console.Name, l.console.ID))
			c.Console = l.console
		} else {
			c.Problems = append(c.Problems, what+", not the console's card, which is not connected: plug its USB cable in, then Find devices.")
		}
	}
	switch found := l.found; {
	case l.known && found == nil:
		msg := fmt.Sprintf("%s is not connected.", l.dev)
		if l.console != nil {
			msg += fmt.Sprintf(" The console's card is %s (%s).", l.console.Name, l.console.ID)
			c.Console = l.console
		} else if !cfg.PlainNames {
			msg += " Neither is the console's card: plug its USB cable in, then Find devices."
		}
		c.Problems = append(c.Problems, msg)
	case !l.known:
		// "default", "pipewire"…: wherever the system sends it
		notConsole(fmt.Sprintf("Records from %q, the system's default input", l.dev))
	case !found.Console:
		notConsole(fmt.Sprintf("%s is %s", l.dev, found.Name))
	case found.Channels > 0 && (cfg.Channels > found.Channels || cfg.Channels < found.minChannels()):
		c.Problems = append(c.Problems, fmt.Sprintf("%s delivers %s channels, but Channels of the device says %d.", found.Name, found.channelRange(), cfg.Channels))
	}
	return c
}

// CheckChannels says why cfg asks more channels than its device has, or
// "": a device connected whose number of channels is known (ListDevices,
// then ProbeChannels).
func CheckChannels(cfg Config, devs []Device, goos string) string {
	cfg = cfg.Normalized()
	l := findDevice(cfg, devs, goos)
	d := l.found
	if d == nil || d.Channels == 0 || cfg.Channels <= d.Channels {
		return ""
	}
	msg := fmt.Sprintf("%s (%s) has %s input channels, not %d: set Channels of the device to %d", d.Name, d.ID, d.channelRange(), cfg.Channels, d.Channels)
	if c := l.console; c != nil && c != d {
		msg += fmt.Sprintf(", or record from the console's card, %s (%s)", c.Name, c.ID)
	}
	return msg + "."
}

// hints explain the recorder errors people meet most.
var hints = []struct {
	re   *regexp.Regexp
	hint string
}{
	{regexp.MustCompile(`(?i)channels count (non|not) available`),
		"The device has no mode with this many channels: is it the console's card (Find devices), and does Channels of the device match the card's mode?"},
	{regexp.MustCompile(`(?i)device or resource busy`),
		"Another program holds the device (PipeWire, an audio editor…): see Device or resource busy in the recording guide."},
	{regexp.MustCompile(`(?i)cannot find card|no such device|no such file or directory|unknown pcm|no such entity`),
		"The device is not there: is the console's USB cable plugged in? Find devices lists what is connected."},
	{regexp.MustCompile(`(?i)sample format (non|not) available`),
		"The device does not deliver this sample format: record from plughw:… instead of hw:…, or through parecord."},
	{regexp.MustCompile(`(?i)rate (non|not) available|rate doesn't match`),
		"The device does not run at this rate: set Rate to the console's sample rate."},
	{regexp.MustCompile(`(?i)executable file not found`),
		"The recorder program is not installed: arecord comes with alsa-utils; ffmpeg (ffmpeg.exe) can be put next to tabula, or installed with brew install ffmpeg (macOS), winget install Gyan.FFmpeg (Windows) or the system's packages."},
}

// Hint says in plain words what to do about a recorder error, or "".
func Hint(msg string) string {
	for _, h := range hints {
		if h.re.MatchString(msg) {
			return h.hint
		}
	}
	return ""
}

// withHint adds the hint of an error message to it.
func withHint(msg string) string {
	if h := Hint(msg); h != "" && !strings.Contains(msg, h) {
		return msg + ". " + h
	}
	return msg
}
