package webapi

import (
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// mdnsReply answers a query for name with addr, as avahi does: the question
// again, the A record (its name compressed, the cache-flush bit set), and
// an AAAA record to skip.
func mdnsReply(t *testing.T, query []byte, name string, addr net.IP) []byte {
	t.Helper()
	q, err := mdnsQuery(name)
	if err != nil || string(query) != string(q) {
		t.Errorf("query %x, want %x (%v)", query, q, err)
	}
	b := append([]byte{0, 0, 0x84, 0, 0, 1, 0, 1, 0, 0, 0, 1}, q[12:]...)
	b = append(b, 0xc0, 12, 0, 1, 0x80, 1, 0, 0, 0, 120, 0, 4)
	b = append(b, addr.To4()...)
	b = append(b, 0xc0, 12, 0, 28, 0x80, 1, 0, 0, 0, 120, 0, 16)
	return append(b, make([]byte, 16)...)
}

func TestMDNSQuery(t *testing.T) {
	// a responder of this machine stands for avahi
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer func(a *net.UDPAddr) { mdnsAddr = a }(mdnsAddr)
	mdnsAddr = conn.LocalAddr().(*net.UDPAddr)
	go func() {
		buf := make([]byte, 512)
		n, from, err := conn.ReadFromUDP(buf)
		if err == nil {
			conn.WriteToUDP([]byte("not dns"), from)
			conn.WriteToUDP(mdnsReply(t, buf[:n], "stage.local", net.IPv4(192, 0, 2, 7)), from)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	ips, err := queryMDNS(ctx, "stage.local")
	if err != nil || fmt.Sprint(ips) != "[192.0.2.7]" {
		t.Errorf("answer: %v %v", ips, err)
	}

	for _, bad := range [][]byte{nil, {0, 0, 0x84, 0, 0, 1}, append([]byte{0, 0, 0x84, 0, 0, 1, 0, 1, 0, 0, 0, 0}, 0xc0, 0xff)} {
		if ips := mdnsAnswers(bad, "stage.local"); ips != nil {
			t.Errorf("%x: %v", bad, ips)
		}
	}
	// a pointer loop ends
	loop := append([]byte{0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0}, 0xc0, 12)
	if ips := mdnsAnswers(loop, "stage.local"); ips != nil {
		t.Errorf("loop: %v", ips)
	}
	if _, err := mdnsQuery("a..local"); err == nil {
		t.Errorf("empty label accepted")
	}
}

func TestAccessNameURL(t *testing.T) {
	mine := localIPv4s()
	if len(mine) == 0 {
		t.Skip("no network address on this machine")
	}
	host, _ := os.Hostname()
	host, _, _ = strings.Cut(strings.ToLower(host), ".")
	api := New(quiet)
	api.SetListenAddr("[::]:8033")
	asked := 0
	answer := []net.IP{net.IPv4(192, 0, 2, 7)} // another machine's
	api.mdns.lookup = func(_ context.Context, name string) ([]net.IP, error) {
		asked++
		if name != host+".local" {
			t.Errorf("asked %q", name)
		}
		return answer, nil
	}
	if v := api.accessStatus(httptest.NewRequest("GET", "/api/access", nil)); v.NameURL != "" {
		t.Errorf("another machine's address: %q", v.NameURL)
	}
	api.mdns.checked = time.Time{} // forget the check
	answer = []net.IP{mine[0]}
	if v := api.accessStatus(httptest.NewRequest("GET", "/api/access", nil)); v.NameURL != "http://"+host+".local:8033" {
		t.Errorf("name: %q", v.NameURL)
	}
	api.accessStatus(httptest.NewRequest("GET", "/api/access", nil))
	if asked != 2 {
		t.Errorf("asked %d times: a check is kept a while", asked)
	}
	// listening on one address only: the name could lead to another one
	api.SetListenAddr(mine[0].String() + ":8033")
	if v := api.accessStatus(httptest.NewRequest("GET", "/api/access", nil)); v.NameURL != "" {
		t.Errorf("one address: %q", v.NameURL)
	}
}
