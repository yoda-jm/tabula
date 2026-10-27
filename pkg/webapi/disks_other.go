//go:build !linux && !darwin && !windows

package webapi

// plugged knows no disks on this system: copies are for Linux, macOS and
// Windows.
func plugged(sessions string) []diskView { return nil }
