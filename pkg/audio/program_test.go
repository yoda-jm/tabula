package audio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProgram(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tabula-test-rec.exe"), []byte("x"), 0o755)
	os.WriteFile(filepath.Join(dir, "myrec"), []byte("x"), 0o755)
	for _, c := range []struct{ name, goos, want string }{
		{"tabula-test-rec", "windows", filepath.Join(dir, "tabula-test-rec.exe")}, // ffmpeg.exe next to tabula.exe
		{"myrec", "linux", filepath.Join(dir, "myrec")},
		{"nothing-like-this", "linux", "nothing-like-this"}, // not found: the error says so
		{"/usr/bin/arecord", "linux", "/usr/bin/arecord"},
	} {
		if got := programIn(c.name, dir, c.goos); got != c.want {
			t.Errorf("%s on %s: %q, want %q", c.name, c.goos, got, c.want)
		}
	}
	if got := programIn("go", dir, "linux"); got != "go" { // on the PATH (the tests run with go)
		t.Errorf("on the PATH: %q", got)
	}
}
