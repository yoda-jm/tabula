//go:build !linux && !darwin && !windows

package webapi

import "context"

func probePower(ctx context.Context) powerInfo { return powerInfo{Why: "not read on this system"} }

func probeSleep(ctx context.Context) sleepInfo { return sleepInfo{Why: "not read on this system"} }
