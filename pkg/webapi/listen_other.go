//go:build !linux

package webapi

import (
	"net"
	"time"
)

func setUserTimeout(*net.TCPConn, time.Duration) {}
