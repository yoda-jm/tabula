package webapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

const kdeInhibitors = `WHO           UID  USER PID COMM            WHAT                                                                       WHY                                                        MODE
Realtime Kit  0    root 704 rtkit-daemon    sleep                                                                      Demote realtime scheduling and stop canary.                delay
PowerDevil    1000 yoda 722 org_kde_powerde handle-power-key:handle-suspend-key:handle-hibernate-key:handle-lid-switch KDE handles power events                                   block
Screen Locker 1000 yoda 690 ksmserver       sleep                                                                      Ensuring that the screen gets locked before going to sleep delay

3 inhibitors listed.
`

const awakeInhibitors = `WHO  UID  USER PID  COMM            WHAT       WHY        MODE
yoda 1000 yoda 4242 systemd-inhibit sleep:idle the show    block
UPower 0  root 410  upowerd         sleep      Pause      delay

2 inhibitors listed.
`

func TestInhibitors(t *testing.T) {
	if awake, lid := inhibitors(kdeInhibitors); awake != "" || !lid {
		t.Errorf("KDE: %q %v", awake, lid)
	}
	if awake, lid := inhibitors(awakeInhibitors); awake != "systemd-inhibit (the show)" || lid {
		t.Errorf("systemd-inhibit: %q %v", awake, lid)
	}
	if awake, lid := inhibitors("No inhibitors.\n"); awake != "" || lid {
		t.Errorf("none: %q %v", awake, lid)
	}
}

func TestLogindLid(t *testing.T) {
	dir := t.TempDir()
	main, drop := filepath.Join(dir, "logind.conf"), filepath.Join(dir, "50-show.conf")
	os.WriteFile(main, []byte("[Login]\n#HandleLidSwitch=suspend\nHandleLidSwitch=lock\n"), 0o644)
	if got := logindLid([]string{main}, false); got != "lock" {
		t.Errorf("main file: %q", got)
	}
	os.WriteFile(drop, []byte("[Login]\nHandleLidSwitchExternalPower=ignore\n"), 0o644)
	if got := logindLid([]string{main, drop}, true); got != "ignore" {
		t.Errorf("on mains: %q", got)
	}
	if got := logindLid([]string{main, drop}, false); got != "lock" {
		t.Errorf("on battery: %q", got)
	}
	if got := logindLid([]string{filepath.Join(dir, "none")}, true); got != "suspend" {
		t.Errorf("default: %q", got)
	}
}

func TestLinuxPower(t *testing.T) {
	supply := func(files map[string]string) string {
		root := t.TempDir()
		for path, v := range files {
			os.MkdirAll(filepath.Join(root, filepath.Dir(path)), 0o755)
			os.WriteFile(filepath.Join(root, path), []byte(v+"\n"), 0o644)
		}
		return root
	}
	laptop := map[string]string{"AC/type": "Mains", "AC/online": "0", "BAT0/type": "Battery", "BAT0/capacity": "64", "BAT0/status": "Discharging",
		"hidpp_battery_0/type": "Battery", "hidpp_battery_0/scope": "Device", "hidpp_battery_0/capacity": "5"}
	if p := linuxPower(supply(laptop)); !p.Known || p.Mains || p.Battery != 64 || p.Charging {
		t.Errorf("on battery: %+v", p)
	}
	laptop["AC/online"], laptop["BAT0/status"] = "1", "Charging"
	if p := linuxPower(supply(laptop)); !p.Mains || p.Battery != 64 || !p.Charging {
		t.Errorf("plugged in: %+v", p)
	}
	if p := linuxPower(supply(map[string]string{"x/type": "Unknown"})); !p.Known || !p.Mains || p.Battery != -1 {
		t.Errorf("a desktop: %+v", p)
	}
	if p := linuxPower(filepath.Join(t.TempDir(), "none")); p.Known {
		t.Errorf("no sysfs: %+v", p)
	}
}

func TestPmset(t *testing.T) {
	if p := pmsetBatt("Now drawing from 'Battery Power'\n -InternalBattery-0 (id=4653155)\t87%; discharging; 4:13 remaining present: true\n"); !p.Known || p.Mains || p.Battery != 87 || p.Charging {
		t.Errorf("battery: %+v", p)
	}
	if p := pmsetBatt("Now drawing from 'AC Power'\n"); !p.Known || !p.Mains || p.Battery != -1 {
		t.Errorf("a desktop Mac: %+v", p)
	}
	if got := pmsetAwake("System-wide power settings:\n sleep                0 (sleep prevented by coreaudiod)\n", ""); got != "the power settings (sleep never)" {
		t.Errorf("sleep 0: %q", got)
	}
	assertions := "Listed by owning process:\n   pid 123(caffeinate): [0x0001] 00:01:02 PreventUserIdleSystemSleep named: \"caffeinate command-line tool\"\n" +
		"   pid 99(coreaudiod): [0x0002] 00:00:10 PreventUserIdleSleep named: \"audio\"\n"
	if got := pmsetAwake(" sleep 10\n", assertions); got != "caffeinate" {
		t.Errorf("assertions: %q", got)
	}
}

func TestCheckLines(t *testing.T) {
	mirror := state.New(schema.Build("M32C"))
	set := func(addr, text string) {
		t.Helper()
		if _, err := mirror.ApplyText(addr+" "+text, "test"); err != nil {
			t.Fatal(err)
		}
	}
	cfg := audio.DefaultConfig()
	cfg.Enabled, cfg.Channels, cfg.Tracks, cfg.Rate = true, 32, "1-16", 48000
	set("/-prefs/card/USBmode", "32/32")
	set("/-prefs/clockrate", "48K")
	if l := checkCard(cfg, mirror); l.Level != checkOK || !strings.Contains(l.Text, "records 16 of them (1-16)") {
		t.Errorf("card: %+v", l)
	}
	set("/-prefs/card/USBmode", "16/16")
	if l := checkCard(cfg, mirror); l.Level != checkBad || !strings.Contains(l.Text, "sends 16 channels") {
		t.Errorf("card 16/16: %+v", l)
	}
	cfg.Rate = 44100
	if l := checkRate(cfg, mirror); l.Level != checkBad || !strings.Contains(l.Fix, "48000") {
		t.Errorf("rate: %+v", l)
	}
	if l := checkRate(cfg, nil); l.Level != checkUnknown {
		t.Errorf("rate, not connected: %+v", l)
	}
	off := cfg
	off.Enabled = false
	if l := checkCard(off, mirror); l.Level != checkInfo {
		t.Errorf("audio off: %+v", l)
	}

	// the disk: what fits, a folder that cannot be written
	dir := t.TempDir()
	if l := checkDisk(cfg, dir, 3); l.Level == checkUnknown || !strings.Contains(l.Text, "for 3 h of the audio") || !strings.Contains(l.Text, "fit") {
		t.Errorf("disk: %+v", l)
	}
	if l := checkDisk(cfg, dir, 1e6); l.Level != checkBad {
		t.Errorf("a show too long: %+v", l)
	}
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, nil, 0o644)
	if l := checkDisk(cfg, filepath.Join(blocker, "sessions"), 3); l.Level != checkBad {
		t.Errorf("unwritable: %+v", l)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the check left %v", entries)
	}

	for _, c := range []struct {
		p    powerInfo
		want string
	}{
		{powerInfo{Known: true, Mains: true, Battery: 80}, checkOK},
		{powerInfo{Known: true, Mains: true, Battery: -1}, checkOK},
		{powerInfo{Known: true, Battery: 64}, checkWarn},
		{powerInfo{Known: true, Battery: 12}, checkBad},
		{powerInfo{}, checkUnknown},
	} {
		if l := checkPower(c.p); l.Level != c.want {
			t.Errorf("power %+v: %+v", c.p, l)
		}
	}
	for _, c := range []struct {
		s    sleepInfo
		want string
	}{
		{sleepInfo{Known: true, Awake: "caffeinate", Lid: "ignore"}, checkOK},
		{sleepInfo{Known: true, Awake: "caffeinate", Lid: "suspend"}, checkWarn},
		{sleepInfo{Known: true}, checkWarn},
		{sleepInfo{Why: "not read on Windows"}, checkUnknown},
	} {
		if l := checkSleep(c.s); l.Level != c.want || !strings.Contains(l.Text+l.Fix, "") {
			t.Errorf("sleep %+v: %+v", c.s, l)
		}
	}
	for _, c := range []struct {
		l    *console.Link
		want string
	}{
		{&console.Link{RTT: 3.4, Asked: 100}, checkOK},
		{&console.Link{RTT: 12, Asked: 100, Lost: 5}, checkWarn},
		{&console.Link{RTT: 400, Asked: 100}, checkBad},
		{nil, checkUnknown},
	} {
		if l := checkLink(c.l); l.Level != c.want {
			t.Errorf("link %+v: %+v", c.l, l)
		}
	}
	cfg.USBBackup = true
	set("/-stat/usbmounted", "OFF")
	if l := checkUSB(cfg, mirror); l.Level != checkBad {
		t.Errorf("no stick: %+v", l)
	}
	if checkHours("") != 3 || checkHours("100") != 24 || checkHours("0") != 0.25 || checkHours("1.5") != 1.5 || checkHours("NaN") != 3 {
		t.Errorf("hours")
	}
	if duration(1.5) != "1 h 30" || duration(0.75) != "45 min" || duration(3) != "3 h" || duration(-1) != "nothing" || duration(41.3) != "41 h" {
		t.Errorf("duration: %s %s %s", duration(1.5), duration(0.75), duration(41.3))
	}
}

// TestCheck asks /api/check of a tabula connected to the emulator, with
// this machine's probes replaced.
func TestCheck(t *testing.T) {
	emuSrv, emuAddr := startEmu(t)
	api := New(quiet)
	api.SetSessionsDir(t.TempDir())
	api.check.probePower = func(context.Context) powerInfo { return powerInfo{Known: true, Battery: 55} }
	api.check.probeSleep = func(context.Context) sleepInfo { return sleepInfo{Known: true, Lid: "suspend"} }
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()

	level := func(v checkView, id string) string {
		for _, l := range v.Lines {
			if l.ID == id {
				return l.Level
			}
		}
		return "missing"
	}
	var v checkView
	getJSON(t, ts.URL+"/api/check", &v)
	if v.Hours != 3 || level(v, "console") != checkBad || level(v, "link") != "missing" || level(v, "power") != checkWarn || level(v, "sleep") != checkWarn {
		t.Errorf("not connected: %+v", v)
	}
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)
	cfg := audio.DefaultConfig()
	cfg.Enabled, cfg.Device, cfg.Tracks = true, "hw:CARD=X32,DEV=0", "1-16"
	api.SetAudioConfig(cfg, "")
	emuSrv.Store().Set("/-prefs/card/USBmode", osc.Int(1), "test") // 16/16
	waitFor := func(id, want string) {
		t.Helper()
		for range 100 {
			getJSON(t, ts.URL+"/api/check?hours=2", &v)
			if level(v, id) == want {
				return
			}
		}
		t.Fatalf("%s never %s: %+v", id, want, v)
	}
	waitFor("card", checkBad)
	if v.Hours != 2 || level(v, "console") != checkOK || level(v, "rate") != checkOK || level(v, "busy") != checkOK || level(v, "phones") != checkWarn {
		t.Errorf("connected: %+v", v)
	}
}
