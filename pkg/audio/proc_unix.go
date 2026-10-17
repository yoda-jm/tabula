//go:build unix

package audio

import (
	"os"
	"syscall"
)

// procAttr puts the recorder in its own process group: Ctrl-C in the
// terminal reaches tabula only, which stops the recorder after finishing
// what it has received.
func procAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

// terminate asks the recorder (and whatever it started) to stop.
func terminate(p *os.Process) { syscall.Kill(-p.Pid, syscall.SIGTERM) }

func kill(p *os.Process) { syscall.Kill(-p.Pid, syscall.SIGKILL) }
