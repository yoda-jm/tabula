package webapi

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Reading this machine's power and sleep settings for the pre-show check:
// the parsing of what Linux and macOS tell, kept here for every system so
// the tests run everywhere; check_<os>.go asks.

// run runs a command, its output within ctx's time.
func run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

// linuxPower reads the power supplies of /sys/class/power_supply (root).
func linuxPower(root string) powerInfo {
	entries, err := os.ReadDir(root)
	if err != nil {
		return powerInfo{Why: err.Error()}
	}
	read := func(dir, name string) string {
		b, _ := os.ReadFile(filepath.Join(root, dir, name))
		return strings.TrimSpace(string(b))
	}
	p := powerInfo{Known: true, Battery: -1}
	mains, mainsOnline, battery := false, false, false
	for _, e := range entries {
		switch read(e.Name(), "type") {
		case "Mains", "USB":
			mains = true
			mainsOnline = mainsOnline || read(e.Name(), "online") == "1"
		case "Battery":
			if read(e.Name(), "scope") == "Device" { // a mouse, a headset
				continue
			}
			battery = true
			if c, err := strconv.Atoi(read(e.Name(), "capacity")); err == nil && p.Battery < 0 {
				p.Battery = c
			}
			status := read(e.Name(), "status")
			p.Charging = p.Charging || status == "Charging" || status == "Full"
			if !mains && (status == "Charging" || status == "Full" || status == "Not charging") {
				mainsOnline = true // no mains entry: the battery tells
			}
		}
	}
	p.Mains = mainsOnline || !battery
	return p
}

// inhibitors reads `systemd-inhibit --list`: what blocks sleep (or idle),
// and whether something handles the lid switch itself (a desktop's power
// manager, whose own settings then decide).
func inhibitors(out string) (awake string, lid bool) {
	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		return "", false
	}
	head := lines[0]
	what, why, mode := strings.Index(head, "WHAT"), strings.Index(head, "WHY"), strings.Index(head, "MODE")
	comm := strings.Index(head, "COMM")
	if what < 0 || why < what || mode < why || comm < 0 || comm > what {
		return "", false
	}
	cut := func(l string, a, b int) string {
		if a >= len(l) {
			return ""
		}
		return strings.TrimSpace(l[a:min(b, len(l))])
	}
	var by []string
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" || strings.HasSuffix(strings.TrimSpace(l), "listed.") {
			continue
		}
		w, m := cut(l, what, why), cut(l, mode, len(l))
		if m != "block" {
			continue
		}
		kinds := strings.Split(w, ":")
		if hasAny(kinds, "sleep", "idle") {
			who := cut(l, comm, what)
			if reason := cut(l, why, mode); reason != "" {
				who += " (" + reason + ")"
			}
			by = append(by, who)
		}
		if hasAny(kinds, "handle-lid-switch") {
			lid = true
		}
	}
	return strings.Join(by, ", "), lid
}

func hasAny(list []string, want ...string) bool {
	for _, x := range list {
		for _, w := range want {
			if x == w {
				return true
			}
		}
	}
	return false
}

// logindFiles are logind's configuration files, in the order they apply
// (the main file, then the drop-ins sorted by name).
func logindFiles() []string {
	files := []string{"/etc/systemd/logind.conf", "/etc/elogind/logind.conf"}
	var drop []string
	for _, d := range []string{"/usr/lib/systemd/logind.conf.d", "/usr/local/lib/systemd/logind.conf.d", "/etc/systemd/logind.conf.d", "/run/systemd/logind.conf.d"} {
		m, _ := filepath.Glob(filepath.Join(d, "*.conf"))
		drop = append(drop, m...)
	}
	sort.SliceStable(drop, func(i, j int) bool { return filepath.Base(drop[i]) < filepath.Base(drop[j]) })
	return append(files, drop...)
}

var logindSetting = regexp.MustCompile(`^\s*(HandleLidSwitch|HandleLidSwitchExternalPower)\s*=\s*(\S+)`)

// logindLid reads what closing the lid does from logind's files: suspend
// (logind's default), ignore or lock; on mains power the setting for
// external power applies when set.
func logindLid(files []string, mains bool) string {
	set := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			if m := logindSetting.FindStringSubmatch(l); m != nil {
				set[m[1]] = m[2]
			}
		}
	}
	action := set["HandleLidSwitch"]
	if ext := set["HandleLidSwitchExternalPower"]; mains && ext != "" {
		action = ext
	}
	switch action {
	case "ignore", "lock":
		return action
	}
	return "suspend" // the default, and every way of sleeping or stopping
}

var (
	pmsetSource  = regexp.MustCompile(`Now drawing from '([^']+)'`)
	pmsetBattery = regexp.MustCompile(`(\d+)%;\s*([a-zA-Z ]+);`)
)

// pmsetBatt reads `pmset -g batt` (macOS).
func pmsetBatt(out string) powerInfo {
	m := pmsetSource.FindStringSubmatch(out)
	if m == nil {
		return powerInfo{Why: "pmset said nothing of the power"}
	}
	p := powerInfo{Known: true, Mains: m[1] == "AC Power", Battery: -1}
	if b := pmsetBattery.FindStringSubmatch(out); b != nil {
		p.Battery, _ = strconv.Atoi(b[1])
		p.Charging = strings.TrimSpace(b[2]) != "discharging"
	}
	return p
}

var (
	pmsetSleep     = regexp.MustCompile(`(?m)^\s*sleep\s+(\d+)`)
	pmsetAssertion = regexp.MustCompile(`(?m)^\s*pid \d+\(([^)]+)\):.*\b(PreventUserIdleSystemSleep|PreventSystemSleep)\b`)
)

// pmsetAwake reads `pmset -g` and `pmset -g assertions` (macOS): what
// keeps the Mac awake, "" nothing.
func pmsetAwake(settings, assertions string) string {
	if m := pmsetSleep.FindStringSubmatch(settings); m != nil && m[1] == "0" {
		return "the power settings (sleep never)"
	}
	var by []string
	for _, m := range pmsetAssertion.FindAllStringSubmatch(assertions, -1) {
		if !hasAny(by, m[1]) {
			by = append(by, m[1])
		}
	}
	return strings.Join(by, ", ")
}
