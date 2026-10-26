package audio

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// program finds a recorder program: on the PATH, else next to tabula's own
// executable (ffmpeg.exe unzipped next to tabula.exe on Windows). Without
// either, it gives name back: running it then says it is not found.
func program(name string) string {
	dir := ""
	if exe, err := os.Executable(); err == nil {
		dir = filepath.Dir(exe)
	}
	return programIn(name, dir, runtime.GOOS)
}

func programIn(name, dir, goos string) string {
	if strings.ContainsAny(name, `/\`) || dir == "" {
		return name
	}
	if _, err := exec.LookPath(name); err == nil {
		return name
	}
	p := filepath.Join(dir, name)
	if goos == "windows" && filepath.Ext(p) == "" {
		p += ".exe"
	}
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return p
	}
	return name
}
