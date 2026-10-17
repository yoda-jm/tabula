//go:build !windows

package console

func sockFD(fd uintptr) int { return int(fd) }
