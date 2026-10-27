package webapi

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// The machine's name on the local network: an access link made with
// http://actarus.local:8033 keeps working when a venue hands the laptop
// another IP address. It is offered only once checked: a multicast DNS
// query for the name (as phones resolve .local names) must be answered
// with an address of this machine (avahi on Linux, Bonjour on macOS,
// Windows 10 and later answer it).

// mdnsAddr is where multicast DNS queries go.
var mdnsAddr = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// mdnsWait is how long a query waits for answers.
const mdnsWait = 600 * time.Millisecond

// mdnsName is the result of the last check, kept a while: a check costs
// mdnsWait and the Access page asks on every load.
type mdnsName struct {
	mu      sync.Mutex
	name    string // "actarus.local", "" when it does not answer
	checked time.Time
	lookup  func(ctx context.Context, name string) ([]net.IP, error) // tests
}

// mdnsKeep is how long a check is trusted: an answer, a silence.
const (
	mdnsKeepFound = time.Minute
	mdnsKeepNone  = 15 * time.Second
)

// localName returns this machine's .local name when the network answers
// for it with one of its addresses, "" otherwise.
func (s *Server) localName() string {
	m := &s.mdns
	m.mu.Lock()
	defer m.mu.Unlock()
	keep := mdnsKeepNone
	if m.name != "" {
		keep = mdnsKeepFound
	}
	if !m.checked.IsZero() && time.Since(m.checked) < keep {
		return m.name
	}
	host, err := os.Hostname()
	host, _, _ = strings.Cut(strings.ToLower(host), ".")
	m.name, m.checked = "", time.Now()
	if err != nil || host == "" || host == "localhost" {
		return ""
	}
	name := host + ".local"
	lookup := m.lookup
	if lookup == nil {
		lookup = queryMDNS
	}
	ctx, cancel := context.WithTimeout(context.Background(), mdnsWait)
	defer cancel()
	ips, err := lookup(ctx, name)
	if err != nil {
		s.log.Debug("mdns", "name", name, "err", err)
		return ""
	}
	mine := localIPv4s()
	for _, ip := range ips {
		if slices.ContainsFunc(mine, ip.Equal) {
			m.name = name
			break
		}
	}
	if m.name == "" && len(ips) > 0 {
		s.log.Info("mdns: the name answers with another machine's address", "name", name, "addresses", ips)
	}
	return m.name
}

// localIPv4s are this machine's IPv4 addresses, but loopback.
func localIPv4s() []net.IP {
	var out []net.IP
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
				out = append(out, n.IP)
			}
		}
	}
	return out
}

// queryMDNS asks the local network for the IPv4 addresses of a .local name
// (a "legacy unicast" query from a port of our own: the answers come back
// to it) and collects the answers until ctx is done.
func queryMDNS(ctx context.Context, name string) ([]net.IP, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	q, err := mdnsQuery(name)
	if err != nil {
		return nil, err
	}
	if _, err := conn.WriteToUDP(q, mdnsAddr); err != nil {
		return nil, err
	}
	if d, ok := ctx.Deadline(); ok {
		conn.SetReadDeadline(d)
	}
	var ips []net.IP
	buf := make([]byte, 9000)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			break // the deadline
		}
		for _, ip := range mdnsAnswers(buf[:n], name) {
			if !slices.ContainsFunc(ips, ip.Equal) {
				ips = append(ips, ip)
			}
		}
	}
	return ips, nil
}

// mdnsQuery builds a DNS query for the A records of name.
func mdnsQuery(name string) ([]byte, error) {
	b := make([]byte, 12, 12+len(name)+6) // id 0, flags 0, one question
	b[5] = 1
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if label == "" || len(label) > 63 {
			return nil, errors.New("not a host name: " + name)
		}
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	return append(b, 0, 0, 1, 0, 1), nil // root, type A, class IN
}

// mdnsAnswers reads the A records for name in a DNS message.
func mdnsAnswers(msg []byte, name string) []net.IP {
	if len(msg) < 12 || msg[2]&0x80 == 0 { // not a response
		return nil
	}
	qd, an := int(binary.BigEndian.Uint16(msg[4:])), int(binary.BigEndian.Uint16(msg[6:]))
	ar := int(binary.BigEndian.Uint16(msg[8:])) + int(binary.BigEndian.Uint16(msg[10:])) // authority, additional
	off := 12
	for range qd {
		var ok bool
		if _, off, ok = dnsName(msg, off); !ok || off+4 > len(msg) {
			return nil
		}
		off += 4
	}
	var ips []net.IP
	for range an + ar {
		var rr string
		var ok bool
		if rr, off, ok = dnsName(msg, off); !ok || off+10 > len(msg) {
			break
		}
		typ, class := binary.BigEndian.Uint16(msg[off:]), binary.BigEndian.Uint16(msg[off+2:])&0x7fff // the cache-flush bit
		size := int(binary.BigEndian.Uint16(msg[off+8:]))
		off += 10
		if off+size > len(msg) {
			break
		}
		if typ == 1 && class == 1 && size == 4 && strings.EqualFold(rr, strings.TrimSuffix(name, ".")) {
			ips = append(ips, net.IPv4(msg[off], msg[off+1], msg[off+2], msg[off+3]))
		}
		off += size
	}
	return ips
}

// dnsName reads a (possibly compressed) name at off; it returns the name
// and where the message goes on after it.
func dnsName(msg []byte, off int) (string, int, bool) {
	var labels []string
	next := -1
	for jumps := 0; off < len(msg); {
		n := int(msg[off])
		switch {
		case n == 0:
			if next < 0 {
				next = off + 1
			}
			return strings.Join(labels, "."), next, true
		case n&0xc0 == 0xc0: // a pointer to an earlier name
			if off+1 >= len(msg) || jumps > 10 {
				return "", 0, false
			}
			if next < 0 {
				next = off + 2
			}
			off = int(binary.BigEndian.Uint16(msg[off:]) & 0x3fff)
			jumps++
		case off+1+n > len(msg):
			return "", 0, false
		default:
			labels = append(labels, string(msg[off+1:off+1+n]))
			off += 1 + n
		}
	}
	return "", 0, false
}
