package webapi

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/state"
)

// The pre-show check (GET /api/check, the Check page): what can spoil a
// recording or a show, found before it starts rather than after: the
// console and the link to it, the card's mode and sample rate against the
// audio settings, the device, the disk, the power, sleep, the console's
// USB stick and the phones. Each line says what is found and what to do.

// Check levels: ok (green), warn (amber), bad (red), unknown (grey: this
// machine cannot tell), info (nothing to check).
const (
	checkOK      = "ok"
	checkWarn    = "warn"
	checkBad     = "bad"
	checkUnknown = "unknown"
	checkInfo    = "info"
)

// checkLine is one line of the check.
type checkLine struct {
	ID    string `json:"id"`    // console, link, card, rate, device, disk, power, sleep, usb, phones, busy
	Title string `json:"title"` // "Console"
	Level string `json:"level"`
	Text  string `json:"text"`          // what is found
	Fix   string `json:"fix,omitempty"` // what to do about it
}

type checkView struct {
	Lines []checkLine `json:"lines"`
	Hours float64     `json:"hours"` // the show length the disk is checked for
	At    time.Time   `json:"at"`
}

// powerInfo is what this machine says of its power supply (check_*.go).
type powerInfo struct {
	Known    bool
	Mains    bool // on mains power, or no battery at all
	Battery  int  // percent, -1 without a battery
	Charging bool
	Why      string // unknown: why
}

// sleepInfo is what this machine says of going to sleep (check_*.go).
type sleepInfo struct {
	Known bool
	Awake string // what keeps it awake now ("" nothing)
	Lid   string // what closing the lid does: suspend, ignore, lock, desktop (its own settings), "" unknown
	Why   string // unknown: why
}

// checkState caches what is slow to find (a command run, the devices) a
// few seconds: the page asks every few seconds.
type checkState struct {
	mu      sync.Mutex
	at      time.Time
	power   powerInfo
	sleep   sleepInfo
	devices []audio.Device
	// tests: replace this machine's probes
	probePower func(context.Context) powerInfo
	probeSleep func(context.Context) sleepInfo
}

// checkCache is how long the machine's probes are kept.
const checkCache = 5 * time.Second

// checkProbe is how long a probe (a command run) may take.
const checkProbe = time.Second

// host returns this machine's power, sleep and audio devices, probed
// again when older than checkCache.
func (s *Server) host() (powerInfo, sleepInfo, []audio.Device) {
	s.check.mu.Lock()
	defer s.check.mu.Unlock()
	if !s.check.at.IsZero() && time.Since(s.check.at) < checkCache {
		return s.check.power, s.check.sleep, s.check.devices
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkProbe)
	defer cancel()
	power, sleep := s.check.probePower, s.check.probeSleep
	if power == nil {
		power = probePower
	}
	if sleep == nil {
		sleep = probeSleep
	}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); s.check.power = power(ctx) }()
	go func() { defer wg.Done(); s.check.sleep = sleep(ctx) }()
	go func() { defer wg.Done(); s.check.devices = s.listDevices(ctx, false) }()
	wg.Wait()
	s.check.at = time.Now()
	return s.check.power, s.check.sleep, s.check.devices
}

// checkHours bounds the show length asked: 3 h unless said, 15 min to 24 h.
func checkHours(q string) float64 {
	h, err := strconv.ParseFloat(q, 64)
	if err != nil || math.IsNaN(h) {
		return 3
	}
	return min(24, max(0.25, h))
}

func (s *Server) getCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.runCheck(r, checkHours(r.URL.Query().Get("hours"))))
}

// runCheck makes every line of the check.
func (s *Server) runCheck(r *http.Request, hours float64) checkView {
	power, sleep, devices := s.host()
	st := s.status()
	s.mu.Lock()
	mirror, cfg, dir := s.mirror, s.audio.cfg.Normalized(), s.sessionsDir
	s.mu.Unlock()
	if !st.Connected {
		mirror = nil
	}
	rec := s.recordStatus()
	v := checkView{Hours: hours, At: time.Now()}
	add := func(l checkLine) { v.Lines = append(v.Lines, l) }
	add(checkConsole(st))
	if st.Connected {
		add(checkLink(st.Console.Link))
	}
	add(checkCard(cfg, mirror))
	add(checkRate(cfg, mirror))
	add(checkDevice(cfg, devices, runtime.GOOS))
	add(checkDisk(cfg, dir, hours))
	add(checkPower(power))
	add(checkSleep(sleep))
	add(checkUSB(cfg, mirror))
	add(s.checkPhones(r))
	add(checkBusy(rec))
	return v
}

func checkConsole(st statusView) checkLine {
	l := checkLine{ID: "console", Title: "Console"}
	c := st.Console
	switch {
	case st.Connecting && c != nil && c.Total > 0:
		l.Level, l.Text = checkWarn, fmt.Sprintf("Syncing: %d of %d values read.", c.Synced, c.Total)
		l.Fix = "Wait for the end of the sync."
	case st.Connecting:
		l.Level, l.Text = checkBad, "Connecting, the console does not answer yet."
		if st.Error != "" {
			l.Text = "The console does not answer: " + st.Error + "."
		}
		l.Fix = "Check the console is on and on the same network as this laptop (its address on the Connect page)."
	case c == nil:
		l.Level, l.Text = checkBad, "Not connected to a console."
		l.Fix = "Connect on the Connect page, or start tabula with -console <address>."
	case !st.Connected:
		l.Level, l.Text = checkBad, fmt.Sprintf("The console (%s) stopped answering.", c.Addr)
		l.Fix = "Check the network cable or Wi-Fi to the console: tabula connects again by itself."
	default:
		l.Level = checkOK
		l.Text = strings.Join(nonEmpty(c.Info.Name, c.Info.Model, firmware(c.Info.Firmware)), " · ") + ": connected and synced."
		if n := len(c.Missing); n > 0 {
			l.Level = checkWarn
			l.Text = fmt.Sprintf("%s: connected, but %d values never answered (%s…).", strings.Join(nonEmpty(c.Info.Name, c.Info.Model), " · "), n, c.Missing[0])
			l.Fix = "Another firmware than tabula knows: the pages showing them may be wrong. Disconnect and connect again to retry."
		}
	}
	return l
}

func firmware(fw string) string {
	if fw == "" {
		return ""
	}
	return "fw " + fw
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, x := range s {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

// checkLink rates the link as the top bar does (web/src/lib/link.ts).
func checkLink(l *console.Link) checkLine {
	line := checkLine{ID: "link", Title: "Link to the console"}
	if l == nil || l.Asked == 0 {
		line.Level, line.Text = checkUnknown, "Being measured: tabula reads the console back for a few seconds."
		return line
	}
	lost := 100 * float64(l.Lost) / float64(l.Asked)
	line.Text = fmt.Sprintf("%s round trip, %d of the last %d requests unanswered.", ms(l.RTT), l.Lost, l.Asked)
	if l.Caught > 0 {
		line.Text += fmt.Sprintf(" %d changes lost on the network were found again.", l.Caught)
	}
	switch {
	case lost >= 15 || l.RTT >= 300:
		line.Level = checkBad
		line.Fix = "The network to the console loses or delays too much: use a cable, or move the Wi-Fi access point closer to the console, away from the stage's radio mics."
	case lost >= 3 || l.RTT >= 80:
		line.Level = checkWarn
		line.Fix = "The network to the console is weak: a cable, or a Wi-Fi access point closer to the console, makes it steadier."
	default:
		line.Level = checkOK
	}
	return line
}

func ms(v float64) string {
	if v < 10 {
		return strconv.FormatFloat(v, 'f', 1, 64) + " ms"
	}
	return strconv.Itoa(int(math.Round(v))) + " ms"
}

// textOf is a parameter's node text, "" when unknown.
func textOf(mirror *state.Store, addr string) string {
	if mirror == nil {
		return ""
	}
	v, l, err := mirror.Get(addr)
	if err != nil {
		return ""
	}
	if t := mirror.TypeOf(l); t != nil {
		return strings.TrimSpace(t.Format(v))
	}
	return ""
}

// cardToComputer is how many channels the card's mode sends to the computer
// (X-USB and DN32-USB, /-prefs/card/USBmode "32/8": the first number, as
// Maillot's document writes "32in/8out"), 0 when unknown.
func cardToComputer(mode string) int {
	in, _, ok := strings.Cut(mode, "/")
	n, err := strconv.Atoi(in)
	if !ok || err != nil {
		return 0
	}
	return n
}

// audioOff is the line of a check that matters only when audio is recorded.
func audioOff(id, title string) checkLine {
	return checkLine{ID: id, Title: title, Level: checkInfo, Text: "The audio is not recorded (Record page, Audio settings): nothing to check."}
}

// checkCard compares the card's mode with the channels tabula records.
func checkCard(cfg audio.Config, mirror *state.Store) checkLine {
	if !cfg.Enabled {
		return audioOff("card", "Card mode")
	}
	l := checkLine{ID: "card", Title: "Card mode"}
	mode := textOf(mirror, "/-prefs/card/USBmode")
	if mirror == nil || mode == "" {
		l.Level, l.Text = checkUnknown, "Read from the console once it is connected."
		return l
	}
	if ifc := textOf(mirror, "/-prefs/card/UFifc"); ifc != "" && ifc != "USB" {
		l.Level = checkWarn
		l.Text = fmt.Sprintf("The card's interface is %s, not USB.", ifc)
		l.Fix = "On the console: Setup › Card, choose USB for a DN32-USB or X-USB card."
		return l
	}
	to := cardToComputer(mode)
	tracks, _ := audio.ParseTracks(cfg.Tracks, cfg.Channels)
	last := 0
	if len(tracks) > 0 {
		last = tracks[len(tracks)-1]
	}
	switch {
	case to == 0:
		l.Level, l.Text = checkUnknown, fmt.Sprintf("Card mode %s: unknown to tabula.", mode)
	case cfg.Channels != to:
		l.Level = checkBad
		l.Text = fmt.Sprintf("The card is in mode %s: it sends %d channels to the computer, the audio settings expect %d.", mode, to, cfg.Channels)
		l.Fix = fmt.Sprintf("On the console: Setup › Card, mode %d/…, or set Channels of the device to %d (Record page, Audio settings).", cfg.Channels, to)
		if last > to {
			l.Fix = fmt.Sprintf("On the console: Setup › Card, a mode sending at least %d channels (32/32), then Channels of the device %d.", last, to)
		}
	default:
		l.Level = checkOK
		l.Text = fmt.Sprintf("Mode %s: the card sends %d channels to the computer; tabula records %s.", mode, to, trackCount(tracks, to))
	}
	return l
}

// trackCount says which of the card's channels are recorded: "all of
// them", "16 of them (1-16)".
func trackCount(tracks []int, of int) string {
	if len(tracks) == of {
		return "all of them"
	}
	return fmt.Sprintf("%d of them (%s)", len(tracks), audio.FormatTracks(tracks))
}

// checkRate compares the console's sample rate with the audio settings'.
func checkRate(cfg audio.Config, mirror *state.Store) checkLine {
	if !cfg.Enabled {
		return audioOff("rate", "Sample rate")
	}
	l := checkLine{ID: "rate", Title: "Sample rate"}
	rate := map[string]int{"48K": 48000, "44K1": 44100}[textOf(mirror, "/-prefs/clockrate")]
	switch {
	case rate == 0:
		l.Level, l.Text = checkUnknown, "Read from the console once it is connected."
	case rate != cfg.Rate:
		l.Level = checkBad
		l.Text = fmt.Sprintf("The console runs at %s, the audio settings record at %s.", khz(rate), khz(cfg.Rate))
		l.Fix = fmt.Sprintf("Set Sample rate to %d in the audio settings (Record page): the console's rate is the one the card delivers.", rate)
	default:
		l.Level, l.Text = checkOK, fmt.Sprintf("%s on the console and in the audio settings.", khz(rate))
	}
	return l
}

func khz(rate int) string {
	return strconv.FormatFloat(float64(rate)/1000, 'f', -1, 64) + " kHz"
}

// checkDevice says whether the device recorded from is there and is the
// console's card (audio.CheckDevice, as the Record page does).
func checkDevice(cfg audio.Config, devices []audio.Device, goos string) checkLine {
	if !cfg.Enabled {
		return audioOff("device", "Audio device")
	}
	l := checkLine{ID: "device", Title: "Audio device"}
	dev := cfg.Device
	if dev == "" {
		dev = "the recorder's default"
	}
	if cfg.Recorder == "custom" {
		l.Level, l.Text = checkUnknown, "A custom recorder: tabula cannot check its device."
		l.Fix = "Record page: Test the input shows whether the tracks come."
		return l
	}
	c := audio.CheckDevice(cfg, devices, goos)
	switch {
	case len(c.Problems) > 0:
		l.Level, l.Text = checkBad, strings.Join(c.Problems, " ")
		l.Fix = "Record page, Audio settings: Find devices and pick the console's card, then Test the input."
		if c.Console != nil {
			l.Fix = fmt.Sprintf("Record page, Audio settings: record from %s (the console's card), then Test the input.", c.Console.ID)
		}
	case goos != "linux" || cfg.Recorder != "arecord" && cfg.Recorder != "parecord" && cfg.Recorder != "ffmpeg":
		l.Level, l.Text = checkUnknown, fmt.Sprintf("%s: not checked on this system.", dev)
		l.Fix = "Record page: Test the input shows whether the tracks come."
	default:
		l.Level, l.Text = checkOK, fmt.Sprintf("%s is connected.", dev)
	}
	return l
}

// consoleMovesPerHour is about what a session's console moves take an hour.
const consoleMovesPerHour = 20 << 20

// checkDisk says whether the show fits on the sessions disk, and that the
// sessions folder can be written.
func checkDisk(cfg audio.Config, dir string, hours float64) checkLine {
	l := checkLine{ID: "disk", Title: "Disk"}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		l.Level, l.Text = checkBad, fmt.Sprintf("The sessions folder %s cannot be made: %v.", dir, err)
		l.Fix = "Start tabula with -sessions on a disk this user can write to."
		return l
	}
	f, err := os.CreateTemp(dir, ".tabula-check-*")
	if err != nil {
		l.Level, l.Text = checkBad, fmt.Sprintf("Nothing can be written in the sessions folder %s: %v.", dir, err)
		l.Fix = "Free some space, or start tabula with -sessions on a disk this user can write to."
		return l
	}
	f.Close()
	os.Remove(f.Name())
	free, err := freeSpace(dir)
	if err != nil {
		l.Level, l.Text = checkUnknown, fmt.Sprintf("The free space of the sessions disk is unknown: %v.", err)
		return l
	}
	perHour := int64(consoleMovesPerHour)
	reserve := int64(0)
	what := "the console moves"
	if cfg.Enabled {
		br := cfg.ByteRate()
		perHour += br * 3600
		reserve = audio.Reserve(br)
		what = "the audio"
	}
	need := int64(hours*float64(perHour)) + reserve
	fits := float64(int64(free)-reserve) / float64(perHour)
	l.Text = fmt.Sprintf("%s for %s of %s, %s free: %s fit.", audio.FormatBytes(need), duration(hours), what, audio.FormatBytes(int64(free)), duration(fits))
	switch {
	case int64(free) < need:
		l.Level = checkBad
		l.Fix = "Free some space (Record page: take older sessions home, then delete them), record fewer tracks, or start tabula with -sessions on a larger disk."
	case float64(free) < 1.25*float64(need):
		l.Level = checkWarn
		l.Fix = "Little room to spare: free some space before the show."
	default:
		l.Level = checkOK
	}
	return l
}

// duration writes hours as "3 h", "1 h 30", "45 min".
func duration(h float64) string {
	if h <= 0 {
		return "nothing"
	}
	min := int(math.Round(h * 60))
	switch {
	case min < 60:
		return fmt.Sprintf("%d min", min)
	case min%60 == 0:
		return fmt.Sprintf("%d h", min/60)
	case min >= 600:
		return fmt.Sprintf("%d h", int(math.Round(h)))
	}
	return fmt.Sprintf("%d h %02d", min/60, min%60)
}

func checkPower(p powerInfo) checkLine {
	l := checkLine{ID: "power", Title: "Power"}
	switch {
	case !p.Known:
		l.Level, l.Text = checkUnknown, "Unknown on this system"
		if p.Why != "" {
			l.Text += ": " + p.Why
		}
		l.Text += "."
		l.Fix = "Plug the laptop in for the show."
	case p.Mains && p.Battery < 0:
		l.Level, l.Text = checkOK, "On mains power (no battery)."
	case p.Mains:
		l.Level, l.Text = checkOK, fmt.Sprintf("On mains power, battery %d%%.", p.Battery)
	case p.Battery >= 0 && p.Battery < 30:
		l.Level, l.Text = checkBad, fmt.Sprintf("On battery, %d%% left.", p.Battery)
		l.Fix = "Plug the laptop in: a show is longer than what is left."
	default:
		l.Level, l.Text = checkWarn, "On battery"
		if p.Battery >= 0 {
			l.Text += fmt.Sprintf(", %d%% left", p.Battery)
		}
		l.Text += "."
		l.Fix = "Plug the laptop in for the show."
	}
	return l
}

func checkSleep(sl sleepInfo) checkLine {
	l := checkLine{ID: "sleep", Title: "Sleep"}
	const self = " tabula does not keep the laptop awake by itself yet."
	switch {
	case !sl.Known:
		l.Level, l.Text = checkUnknown, "Unknown on this system"
		if sl.Why != "" {
			l.Text += ": " + sl.Why
		}
		l.Text += "." + self
		l.Fix = "In the system's power settings, set the laptop never to sleep, and to do nothing when the lid closes, for the show."
		return l
	case sl.Awake != "":
		l.Level, l.Text = checkOK, "Kept awake by "+sl.Awake+"."
	default:
		l.Level, l.Text = checkWarn, "Nothing keeps the laptop awake: it may go to sleep when idle, and stop the recording."+self
		l.Fix = "Set the laptop never to sleep for the show (power settings), or run tabula under systemd-inhibit (Linux) or caffeinate (macOS)."
	}
	switch sl.Lid {
	case "suspend":
		l.Text += " Closing the lid suspends it."
		l.Level = maxLevel(l.Level, checkWarn)
		if l.Fix == "" {
			l.Fix = "Keep the lid open, or set closing the lid to do nothing (Linux: HandleLidSwitch=ignore in logind.conf)."
		} else {
			l.Fix += " Keep the lid open, or set closing the lid to do nothing."
		}
	case "ignore", "lock":
		l.Text += " Closing the lid does not suspend it."
	case "desktop":
		l.Text += " What closing the lid does is the desktop's setting."
	}
	return l
}

// maxLevel is the worse of two levels.
func maxLevel(a, b string) string {
	rank := map[string]int{checkInfo: 0, checkOK: 1, checkUnknown: 2, checkWarn: 3, checkBad: 4}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// checkUSB checks the console's USB stick, when tabula starts the console's
// own recorder with every recording (the USB stick backup).
func checkUSB(cfg audio.Config, mirror *state.Store) checkLine {
	l := checkLine{ID: "usb", Title: "Console's USB stick"}
	if !cfg.USBBackup {
		l.Level, l.Text = checkInfo, "The backup on the console's USB stick is off (Record page): nothing to check."
		return l
	}
	switch textOf(mirror, "/-stat/usbmounted") {
	case "ON":
		l.Level, l.Text = checkOK, "A USB stick is in the console, for the backup recording."
	case "OFF":
		l.Level, l.Text = checkBad, "No USB stick in the console: the backup recording cannot start."
		l.Fix = "Put a USB stick in the console (FAT32, with room for the show), or turn the backup off on the Record page."
	default:
		l.Level, l.Text = checkUnknown, "Read from the console once it is connected."
	}
	return l
}

// checkPhones says whether the musicians' phones can reach tabula.
func (s *Server) checkPhones(r *http.Request) checkLine {
	l := checkLine{ID: "phones", Title: "Phones"}
	a := s.accessStatus(r)
	if !a.Reachable {
		l.Level, l.Text = checkWarn, "tabula listens on this laptop only: phones cannot reach it."
		l.Fix = "Start tabula with -http :8033 for the in-ear mixes on the musicians' phones."
		return l
	}
	where := strings.Join(a.URLs, ", ")
	if a.NameURL != "" {
		where = a.NameURL + " (" + where + ")"
	}
	musicians, online := 0, 0
	for _, g := range a.Grants {
		if g.Role != RoleMusician || g.Session {
			continue
		}
		musicians++
		if a.Online.Grants[g.ID].Streams > 0 {
			online++
		}
	}
	l.Level = checkOK
	l.Text = "Phones reach tabula at " + where + "."
	switch {
	case musicians == 0:
		l.Text += " No musician has an access link yet (Access page)."
	default:
		l.Text += fmt.Sprintf(" %d of %d musicians' phones have their mix open.", online, musicians)
	}
	return l
}

func checkBusy(rec recordView) checkLine {
	l := checkLine{ID: "busy", Title: "Recording", Level: checkInfo}
	switch {
	case rec.Recording:
		l.Text = "Recording now: " + rec.Name + "."
	case rec.Replaying:
		l.Text = "A session is being replayed onto the console: " + rec.ReplayOf + "."
	default:
		l.Level, l.Text = checkOK, "Nothing is recorded or replayed now: ready to start."
	}
	return l
}
