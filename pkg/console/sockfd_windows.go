//go:build windows

package console

import "syscall"

func sockFD(fd uintptr) syscall.Handle { return syscall.Handle(fd) }
