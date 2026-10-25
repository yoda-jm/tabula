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

// CheckDevice compares the device of cfg with the devices connected
// (ListDevices). It knows ALSA devices (arecord, and ffmpeg on Linux) and
// PulseAudio/PipeWire sources (parecord); other configurations, and a
// machine where nothing is listed, pass. With PlainNames (a device that is
// not the console's card, on purpose) only a missing device is a problem.
func CheckDevice(cfg Config, devs []Device, goos string) Check {
	cfg = cfg.Normalized()
	kind := cfg.Recorder
	if kind == "ffmpeg" && goos == "linux" {
		kind = "arecord" // ffmpeg -f alsa takes the ALSA names
	}
	if !cfg.Enabled || goos != "linux" || len(devs) == 0 || kind != "arecord" && kind != "parecord" {
		return Check{}
	}
	var console *Device
	for i := range devs {
		if devs[i].Console && devs[i].Recorder == kind {
			console = &devs[i]
			break
		}
	}
	dev := cfg.Device
	if dev == "" {
		dev = defaultDevice(cfg.Recorder, goos)
	}

	var found *Device
	known := false // whether dev names a card (or source) we can look for
	if kind == "arecord" {
		if card, num, ok := alsaCardOf(dev); ok {
			known = true
			for i := range devs {
				d := &devs[i]
				dc, dn, _ := alsaCardOf(d.ID)
				if d.Recorder == "arecord" && (dc == card || d.card == card) && dn == num {
					found = d
					break
				}
			}
		}
	} else if dev != "@DEFAULT_SOURCE@" {
		known = true
		for i := range devs {
			if devs[i].Recorder == "parecord" && devs[i].ID == dev {
				found = &devs[i]
				break
			}
		}
	}

	var c Check
	notConsole := func(what string) {
		if cfg.PlainNames {
			return
		}
		if console != nil {
			c.Problems = append(c.Problems, fmt.Sprintf("%s, not the console's card: %s (%s) is connected.", what, console.Name, console.ID))
			c.Console = console
		} else {
			c.Problems = append(c.Problems, what+", not the console's card, which is not connected: plug its USB cable in, then Find devices.")
		}
	}
	switch {
	case known && found == nil:
		msg := fmt.Sprintf("%s is not connected.", dev)
		if console != nil {
			msg += fmt.Sprintf(" The console's card is %s (%s).", console.Name, console.ID)
			c.Console = console
		} else if !cfg.PlainNames {
			msg += " Neither is the console's card: plug its USB cable in, then Find devices."
		}
		c.Problems = append(c.Problems, msg)
	case !known:
		// "default", "pipewire"…: wherever the system sends it
		notConsole(fmt.Sprintf("Records from %q, the system's default input", dev))
	case !found.Console:
		notConsole(fmt.Sprintf("%s is %s", dev, found.Name))
	case found.Channels > 0 && found.Channels != cfg.Channels:
		c.Problems = append(c.Problems, fmt.Sprintf("%s delivers %d channels, but Channels of the device says %d.", found.Name, found.Channels, cfg.Channels))
	}
	return c
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
		"The recorder program is not installed (arecord comes with alsa-utils)."},
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
