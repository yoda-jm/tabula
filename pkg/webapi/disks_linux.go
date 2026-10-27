package webapi

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// plugged lists the disks mounted under /media, /run/media or /mnt, where
// desktops mount USB disks (/media/<user>/<label>), but the one holding
// the sessions directory.
func plugged(sessions string) []diskView {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []diskView
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text()) // device mount-point type options 0 0
		if len(fields) < 4 {
			continue
		}
		dir := unescapeMount(fields[1])
		if !strings.HasPrefix(dir, "/media/") && !strings.HasPrefix(dir, "/run/media/") && !strings.HasPrefix(dir, "/mnt/") {
			continue
		}
		if within(sessions, dir) || fields[2] == "autofs" {
			continue
		}
		ro := strings.HasPrefix(fields[3], "ro,") || fields[3] == "ro"
		out = append(out, diskView{Path: dir, Name: filepath.Base(dir), Writable: !ro && writable(dir)})
	}
	return out
}
