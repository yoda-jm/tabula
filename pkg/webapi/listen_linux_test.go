package webapi

import (
	"net"
	"syscall"
	"testing"
)

func TestListenDeadPeer(t *testing.T) {
	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if c, err := net.Dial("tcp", ln.Addr().String()); err == nil {
			defer c.Close()
			c.Read(make([]byte, 1))
		}
	}()
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	rc, _ := c.(*net.TCPConn).SyscallConn()
	var ms int
	rc.Control(func(fd uintptr) { ms, err = syscall.GetsockoptInt(int(fd), syscall.IPPROTO_TCP, tcpUserTimeout) })
	if err != nil || ms != int(deadPeer.Milliseconds()) {
		t.Errorf("TCP_USER_TIMEOUT %d ms, %v", ms, err)
	}
}
