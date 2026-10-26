package webapi

import (
	"net"
	"syscall"
	"time"
)

const tcpUserTimeout = 0x12 // TCP_USER_TIMEOUT, not in package syscall

func setUserTimeout(c *net.TCPConn, d time.Duration) {
	if rc, err := c.SyscallConn(); err == nil {
		rc.Control(func(fd uintptr) {
			syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, tcpUserTimeout, int(d.Milliseconds()))
		})
	}
}
