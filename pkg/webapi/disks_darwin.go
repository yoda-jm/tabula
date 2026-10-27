package webapi

import (
	"os"
	"path/filepath"
	"syscall"
)

// plugged lists the volumes in /Volumes but the system's own (a link to /)
// and the one holding the sessions directory.
func plugged(sessions string) []diskView {
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return nil
	}
	var root syscall.Stat_t
	if err := syscall.Stat("/", &root); err != nil {
		return nil
	}
	var out []diskView
	for _, e := range entries {
		dir := filepath.Join("/Volumes", e.Name())
		var st syscall.Stat_t
		if syscall.Stat(dir, &st) != nil || st.Dev == root.Dev || within(sessions, dir) {
			continue
		}
		out = append(out, diskView{Path: dir, Name: e.Name(), Writable: writable(dir)})
	}
	return out
}
