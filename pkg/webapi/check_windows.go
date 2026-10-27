package webapi

import (
	"context"
	"unsafe"
)

var getSystemPowerStatus = kernel32.NewProc("GetSystemPowerStatus") // kernel32: disks_windows.go

// systemPowerStatus is Windows' SYSTEM_POWER_STATUS.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

func probePower(ctx context.Context) powerInfo {
	var st systemPowerStatus
	if r, _, err := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&st))); r == 0 {
		return powerInfo{Why: "GetSystemPowerStatus: " + err.Error()}
	}
	p := powerInfo{Known: st.ACLineStatus != 255, Mains: st.ACLineStatus == 1, Battery: -1, Charging: st.BatteryFlag&8 != 0}
	if st.BatteryFlag&128 != 0 { // no battery
		p.Mains = true
	} else if st.BatteryLifePercent <= 100 {
		p.Battery = int(st.BatteryLifePercent)
	}
	return p
}

func probeSleep(ctx context.Context) sleepInfo {
	return sleepInfo{Why: "not read on Windows"}
}
