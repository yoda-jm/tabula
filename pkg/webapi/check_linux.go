package webapi

import (
	"context"
	"os/exec"
)

func probePower(ctx context.Context) powerInfo { return linuxPower("/sys/class/power_supply") }

// probeSleep asks logind (systemd-inhibit) what keeps the laptop awake,
// and reads what closing the lid does.
func probeSleep(ctx context.Context) sleepInfo {
	if _, err := exec.LookPath("systemd-inhibit"); err != nil {
		return sleepInfo{Why: "systemd-inhibit is not there (a system without systemd)"}
	}
	out, err := run(ctx, "systemd-inhibit", "--list", "--no-pager")
	if err != nil {
		return sleepInfo{Why: "systemd-inhibit --list: " + err.Error()}
	}
	s := sleepInfo{Known: true}
	var desktop bool
	s.Awake, desktop = inhibitors(out)
	if desktop {
		s.Lid = "desktop"
	} else {
		s.Lid = logindLid(logindFiles(), linuxPower("/sys/class/power_supply").Mains)
	}
	return s
}
