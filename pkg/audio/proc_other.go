//go:build !unix && !windows

package audio

import (
	"os"
	"syscall"
)

func procAttr() *syscall.SysProcAttr { return nil }

func terminate(p *os.Process) { p.Kill() }

func kill(p *os.Process) { p.Kill() }
