package main

import (
	"os"
	"path/filepath"
)

// logRotateAt is the size over which the log file starts again, the
// previous one kept as tabula.log.1: the log never grows without end.
const logRotateAt = 10 << 20

// logPath is the file the log goes to besides the terminal: -log, or
// tabula.log in the sessions directory by default; "" with -log none.
func logPath(flag, sessions string) string {
	switch flag {
	case "":
		return filepath.Join(sessions, "tabula.log")
	case "none":
		return ""
	}
	return flag
}

// openLog opens the log file to append to, creating its directory, and
// starts a new one when it is over logRotateAt.
func openLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > logRotateAt {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
}
