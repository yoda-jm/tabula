package audio

import (
	"os"
	"syscall"
)

// procAttr gives the recorder its own process group: Ctrl-C in the console
// reaches tabula only, which stops the recorder itself.
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// terminate stops the recorder: Windows has no signal to ask politely, and
// the stream needs no ending (the files are finished by tabula).
func terminate(p *os.Process) { p.Kill() }

func kill(p *os.Process) { p.Kill() }
