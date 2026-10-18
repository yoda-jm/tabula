package audio

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

func demo(t *testing.T) *state.Store {
	t.Helper()
	st := state.New(schema.Build("M32C"))
	f, err := os.Open("../../testdata/scenes/demo.scn")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, errs := scene.Load(f, st, "test"); len(errs) > 0 {
		t.Fatal(errs)
	}
	return st
}

// The band's scene: the card takes AES50-A 1-16 and 25-32 (the stage box,
// through the input patch to the channels) and outputs 9-16 (FX buses and
// matrices).
func TestTrackNamesDemo(t *testing.T) {
	got := TrackNames(demo(t))
	want := []string{
		"Hang", "Cajon", "Snare", "Djembe", "Bongos Perc", "Auto harpe", "Basse", "Guitare Elec",
		"Guitare Acc", "Ukulele", "", "Vx Theo", "Vx Paul", "Vx Margot", "Vx Simon", "Vx Bastien",
		"Fx Room", "Fx Hall", "Fx Delay", "Fx Chorus", "RappelL", "RappelR", "FacadeL", "FacadeR",
		"", "", "", "", "", "", "", "BT",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestTrackNamesRouting(t *testing.T) {
	st := state.New(schema.Build("X32"))
	apply := func(lines ...string) {
		t.Helper()
		if errs := st.ApplyLines(lines, "test"); len(errs) > 0 {
			t.Fatal(errs)
		}
	}
	// Factory patch: local inputs 1-32 to the channels and the card.
	apply(`/ch/03/config "Kick" 1 RD 3`, `/ch/04/config "Kick 2" 1 RD 3`, `/ch/05/config "" 1 RD 5`)
	names := TrackNames(st)
	if names[2] != "Kick" || names[3] != "" || names[4] != "" {
		t.Errorf("factory: %q", names[:6])
	}
	// The card's 9-16 from AES50-B 1-8, which the input patch puts on inputs 17-24.
	apply("/config/routing/CARD AN1-8 B1-8 AN17-24 UIN1-8", "/config/routing/IN AN1-8 AN9-16 B1-8 AN25-32 AUX1-4",
		`/ch/20/config "Sax" 1 RD 20`)
	if names = TrackNames(st); names[11] != "Sax" {
		t.Errorf("AES50-B: %q", names[8:16])
	}
	// User inputs: card 25 is user input 1, taken from local input 3.
	apply("/config/userrout/in 3 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0")
	if names = TrackNames(st); names[24] != "Kick" || names[25] != "" {
		t.Errorf("user input: %q", names[24:26])
	}
	// Outputs: main L/R and a bus; aux inputs through the aux patch.
	apply("/config/routing/CARD OUT1-8 AuxIN1-6/TB P161-8 UOUT1-8",
		"/outputs/main/01 1 POST OFF", "/outputs/main/02 2 POST OFF", "/outputs/main/03 5 POST OFF",
		`/bus/02/config "Wedge" 1 RD`, `/auxin/03/config "Laptop" 1 RD 35`, `/main/st/config "PA" 1 WH`,
		"/config/userrout/out 188 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
		"/outputs/p16/04 28 POST OFF") // direct out of channel 3
	names = TrackNames(st)
	if names[0] != "PA L" || names[1] != "PA R" || names[2] != "Wedge" || names[10] != "Laptop" || names[19] != "Kick" || names[24] != "Kick" {
		t.Errorf("outputs: %q", names)
	}
}

func TestListDevices(t *testing.T) {
	files := map[string]string{"/proc/asound/card2/stream0": `Behringer X-USB at usb-0000:00:14.0-2, high speed : USB Audio

Playback:
  Status: Stop
  Interface 1
    Altset 1
    Format: S32_LE
    Channels: 32
    Rates: 44100, 48000
    Bits: 24

Capture:
  Status: Stop
  Interface 2
    Altset 1
    Format: S32_LE
    Channels: 32
    Endpoint: 0x86 (6 IN) (ASYNC)
    Rates: 44100, 48000
    Data packet interval: 125 us
    Bits: 24
`}
	outputs := map[string]string{
		"arecord -l": `**** List of CAPTURE Hardware Devices ****
card 0: PCH [HDA Intel PCH], device 0: ALC892 Analog [ALC892 Analog]
  Subdevices: 1/1
  Subdevice #0: subdevice #0
card 2: XUSB [X-USB], device 0: USB Audio [USB Audio]
  Subdevices: 1/1
  Subdevice #0: subdevice #0
`,
		"pactl list short sources": "55\talsa_output.pci.monitor\tPipeWire\ts32le 2ch 48000Hz\tSUSPENDED\n" +
			"56\talsa_input.usb-Behringer_X-USB-00.pro-input-0\tPipeWire\ts32le 32ch 48000Hz\tSUSPENDED\n",
		"ffmpeg -hide_banner -f avfoundation -list_devices true -i ": `[AVFoundation indev @ 0x7f8] AVFoundation video devices:
[AVFoundation indev @ 0x7f8] [0] FaceTime HD Camera
[AVFoundation indev @ 0x7f8] AVFoundation audio devices:
[AVFoundation indev @ 0x7f8] [0] MacBook Pro Microphone
[AVFoundation indev @ 0x7f8] [1] DN32-USB
`,
		"ffmpeg -hide_banner -list_devices true -f dshow -i dummy": `[dshow @ 000001] "Integrated Camera" (video)
[dshow @ 000001]   Alternative name "@device_pnp_\\?\usb"
[dshow @ 000001] "Microphone (DN32-USB)" (audio)
[dshow @ 000001]   Alternative name "@device_cm_{33D9A762}\wave_{1}"
`,
	}
	run := func(_ context.Context, name string, args ...string) (string, error) {
		if out, ok := outputs[strings.Join(append([]string{name}, args...), " ")]; ok {
			return out, nil
		}
		return "", errors.New("not found")
	}
	read := func(path string) ([]byte, error) {
		if s, ok := files[path]; ok {
			return []byte(s), nil
		}
		return nil, os.ErrNotExist
	}
	got := listDevices(context.Background(), "linux", run, read)
	want := []Device{
		{Recorder: "arecord", ID: "hw:CARD=PCH,DEV=0", Name: "HDA Intel PCH, ALC892 Analog"},
		{Recorder: "arecord", ID: "hw:CARD=XUSB,DEV=0", Name: "X-USB, USB Audio", Channels: 32, Format: "S32_LE, 24 bits", Rates: "44100, 48000", Console: true},
		{Recorder: "parecord", ID: "alsa_input.usb-Behringer_X-USB-00.pro-input-0", Name: "alsa_input.usb-Behringer_X-USB-00.pro-input-0", Channels: 32, Format: "s32le", Rates: "48000", Console: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("linux:\n got %+v\nwant %+v", got, want)
	}
	got = listDevices(context.Background(), "darwin", run, read)
	if len(got) != 2 || got[1] != (Device{Recorder: "ffmpeg", ID: "1", Name: "DN32-USB", Console: true}) {
		t.Errorf("macOS: %+v", got)
	}
	got = listDevices(context.Background(), "windows", run, read)
	if len(got) != 1 || got[0].ID != "Microphone (DN32-USB)" || !got[0].Console {
		t.Errorf("windows: %+v", got)
	}
	if got := parseDShow("[dshow @ 0] DirectShow audio devices\n[dshow @ 0]  \"Line (DN32-USB)\"\n[dshow @ 0]     Alternative name \"@x\"\n"); len(got) != 1 || got[0].ID != "Line (DN32-USB)" {
		t.Errorf("older ffmpeg: %+v", got)
	}
}
