package audio

import (
	"strings"
	"testing"
)

func TestCheckDevice(t *testing.T) {
	pch := Device{Recorder: "arecord", ID: "hw:CARD=PCH,DEV=0", Name: "HDA Intel PCH, ALC294 Analog", card: "0"}
	dn32 := Device{Recorder: "arecord", ID: "hw:CARD=DN32USB,DEV=0", Name: "DN32-USB, USB Audio", Channels: 32, Console: true, card: "1"}
	src := Device{Recorder: "parecord", ID: "alsa_input.usb-KLARK_TEKNIK_DN32-USB_74E97946-00.multichannel-input", Name: "DN32-USB", Channels: 32, Console: true}
	withCard := []Device{pch, dn32, src}
	without := []Device{pch}
	cfg := func(recorder, device string, channels int) Config {
		c := DefaultConfig()
		c.Enabled, c.Recorder, c.Device, c.Channels = true, recorder, device, channels
		return c
	}
	for _, tc := range []struct {
		name    string
		cfg     Config
		devs    []Device
		want    string // in the problem; "" for none
		console bool   // the console's card is offered
	}{
		// the concert of 2026-10-25: the laptop's card kept from a test
		{"laptop card", cfg("arecord", "hw:CARD=PCH,DEV=0", 32), withCard, "hw:CARD=PCH,DEV=0 is HDA Intel PCH, ALC294 Analog, not the console's card: DN32-USB, USB Audio (hw:CARD=DN32USB,DEV=0) is connected", true},
		{"laptop card, no console", cfg("arecord", "hw:CARD=PCH,DEV=0", 2), without, "not the console's card, which is not connected", false},
		{"console card", cfg("arecord", "hw:CARD=DN32USB,DEV=0", 32), withCard, "", false},
		{"console card by number", cfg("arecord", "plughw:1,0", 32), withCard, "", false},
		{"wrong channels", cfg("arecord", "hw:CARD=DN32USB,DEV=0", 16), withCard, "delivers 32 channels, but Channels of the device says 16", false},
		{"unplugged", cfg("arecord", "hw:CARD=DN32USB,DEV=0", 32), without, "hw:CARD=DN32USB,DEV=0 is not connected. Neither is the console's card", false},
		{"other card name", cfg("arecord", "hw:CARD=XUSB,DEV=0", 32), withCard, "hw:CARD=XUSB,DEV=0 is not connected. The console's card is DN32-USB", true},
		{"default", cfg("arecord", "", 32), withCard, `Records from "default", the system's default input, not the console's card`, true},
		{"ffmpeg default", cfg("ffmpeg", "default", 32), withCard, "not the console's card", true},
		{"parecord source", cfg("parecord", src.ID, 32), withCard, "", false},
		{"parecord default", cfg("parecord", "", 32), withCard, `Records from "@DEFAULT_SOURCE@"`, true},
		{"parecord gone", cfg("parecord", "alsa_input.usb-Behringer_X-USB-00.pro-input-0", 32), without, "is not connected", false},
		{"custom", cfg("custom", "", 32), withCard, "", false},
		{"nothing listed", cfg("arecord", "hw:CARD=PCH,DEV=0", 32), nil, "", false},
	} {
		c := CheckDevice(tc.cfg, tc.devs, "linux")
		got := strings.Join(c.Problems, " | ")
		if tc.want == "" && got != "" || !strings.Contains(got, tc.want) || len(c.Problems) > 1 {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
		if (c.Console != nil) != tc.console || c.Console != nil && !c.Console.Console {
			t.Errorf("%s: console %+v", tc.name, c.Console)
		}
	}

	// Audio off, another system, or another device on purpose.
	off := cfg("arecord", "hw:CARD=PCH,DEV=0", 32)
	off.Enabled = false
	if c := CheckDevice(off, withCard, "linux"); len(c.Problems) > 0 {
		t.Errorf("off: %+v", c)
	}
	if c := CheckDevice(cfg("arecord", "hw:CARD=PCH,DEV=0", 32), withCard, "darwin"); len(c.Problems) > 0 {
		t.Errorf("macOS: %+v", c)
	}
	plain := cfg("arecord", "hw:CARD=PCH,DEV=0", 2)
	plain.PlainNames = true
	if c := CheckDevice(plain, withCard, "linux"); len(c.Problems) > 0 {
		t.Errorf("plain names: %+v", c)
	}
	plain.Device = "hw:CARD=M16DX,DEV=0"
	if c := CheckDevice(plain, withCard, "linux"); len(c.Problems) != 1 || !strings.Contains(c.Problems[0], "is not connected") {
		t.Errorf("plain names, unplugged: %+v", c)
	}
}

func TestHint(t *testing.T) {
	for msg, want := range map[string]string{
		"the recorder stopped (exit status 1): arecord: set_params:1398: Channels count non available": "no mode with this many channels",
		"arecord: main:850: audio open error: Device or resource busy":                                 "Another program holds the device",
		"ALSA lib confmisc.c:165:(snd_config_get_card) cannot find card 'DN32USB'":                     "is the console's USB cable plugged in?",
		"arecord: set_params:1387: Sample format non available":                                        "plughw",
		`arecord: exec: "arecord": executable file not found in $PATH`:                                 "alsa-utils",
		"all fine": "",
	} {
		if got := Hint(msg); want == "" && got != "" || !strings.Contains(got, want) {
			t.Errorf("%q: %q", msg, got)
		}
	}
	if got := withHint("Channels count non available"); !strings.HasPrefix(got, "Channels count non available. The device has no mode") {
		t.Errorf("withHint: %q", got)
	}
	if got := withHint(withHint("Channels count non available")); strings.Count(got, "The device has") != 1 {
		t.Errorf("hinted twice: %q", got)
	}
}
