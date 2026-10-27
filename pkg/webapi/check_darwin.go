package webapi

import "context"

func probePower(ctx context.Context) powerInfo {
	out, err := run(ctx, "pmset", "-g", "batt")
	if err != nil {
		return powerInfo{Why: "pmset -g batt: " + err.Error()}
	}
	return pmsetBatt(out)
}

// probeSleep reads the sleep setting and the assertions keeping the Mac
// awake; a MacBook sleeps when its lid closes (unless in clamshell mode,
// with an external display, keyboard and power).
func probeSleep(ctx context.Context) sleepInfo {
	settings, err := run(ctx, "pmset", "-g")
	if err != nil {
		return sleepInfo{Why: "pmset -g: " + err.Error()}
	}
	assertions, _ := run(ctx, "pmset", "-g", "assertions")
	s := sleepInfo{Known: true, Awake: pmsetAwake(settings, assertions)}
	if probePower(ctx).Battery >= 0 {
		s.Lid = "suspend"
	}
	return s
}
