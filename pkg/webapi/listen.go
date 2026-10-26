package webapi

import (
	"net"
	"time"
)

// deadPeer is how long a connection's data may go unanswered before the
// system closes it, where it can be told (Linux). A phone gone off the Wi-Fi
// sends nothing more: its event stream, pinged every 5 s, then ends within
// half a minute, rather than the quarter of an hour TCP waits by itself, and
// the Access page no longer shows it online.
const deadPeer = 20 * time.Second

// Listen listens for the GUI on addr.
func Listen(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return deadPeerListener{ln}, nil
}

type deadPeerListener struct{ net.Listener }

func (l deadPeerListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if tc, ok := c.(*net.TCPConn); ok {
		setUserTimeout(tc, deadPeer)
	}
	return c, err
}
