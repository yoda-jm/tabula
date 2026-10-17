// Package console is the client side of the console protocol: it connects to
// an X32/M32 (or tabula-emu), mirrors its whole state in a state.Store and
// keeps that mirror up to date. Writes are refused unless armed.
package console

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

// Origins of mirror changes.
const (
	OriginConsole = "console" // reported by the console
	OriginLocal   = "local"   // written by this client
)

var (
	ErrReadOnly = errors.New("writes are not armed")
	ErrTimeout  = errors.New("console did not answer")
)

// Info is what the console reports about itself.
type Info struct {
	ServerVersion string `json:"server_version"`
	ServerName    string `json:"server_name"`
	Model         string `json:"model"`
	Firmware      string `json:"firmware"`
	IP            string `json:"ip"`
	Name          string `json:"name"`
}

// Status is the connection state.
type Status struct {
	Addr      string    `json:"addr"`
	Info      Info      `json:"info"`
	Connected bool      `json:"connected"`
	Synced    int       `json:"synced"` // nodes received during the initial sync
	Total     int       `json:"total"`
	Missing   []string  `json:"missing,omitempty"` // nodes the console never answered
	Armed     bool      `json:"armed"`
	LastSeen  time.Time `json:"last_seen"`
}

// Client is one connection to a console.
type Client struct {
	addr   *net.UDPAddr
	conn   *net.UDPConn
	mirror *state.Store
	log    *slog.Logger

	mu       sync.Mutex
	status   Status
	waiters  map[string][]chan osc.Message // by reply address, "node /path" for /node
	listener []func(osc.Message)           // meters and other unsolicited messages
}

// Dial connects to the console at addr ("host" or "host:port"), checks it
// answers /info, and starts receiving. Call Sync to fill the mirror.
func Dial(ctx context.Context, addr string, mirror *state.Store, log *slog.Logger) (*Client, error) {
	if log == nil {
		log = slog.Default()
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "10023")
	}
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, ua)
	if err != nil {
		return nil, err
	}
	c := &Client{addr: ua, conn: conn, mirror: mirror, log: log, waiters: map[string][]chan osc.Message{}}
	c.status.Addr = ua.String()
	go c.readLoop()

	info, err := c.ask(ctx, osc.NewMessage("/info"), "/info", 3)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("no console at %s: %w", ua, err)
	}
	c.mu.Lock()
	c.status.Connected = true
	c.status.Info.ServerVersion, c.status.Info.ServerName = str(info, 0), str(info, 1)
	c.status.Info.Model, c.status.Info.Firmware = str(info, 2), str(info, 3)
	c.mu.Unlock()
	if x, err := c.ask(ctx, osc.NewMessage("/xinfo"), "/xinfo", 2); err == nil {
		c.mu.Lock()
		c.status.Info.IP, c.status.Info.Name = str(x, 0), str(x, 1)
		c.mu.Unlock()
	}
	log.Info("connected", "console", ua.String(), "model", c.status.Info.Model, "name", c.status.Info.Name)
	return c, nil
}

func str(m osc.Message, i int) string {
	if i < len(m.Args) && m.Args[i].Tag == 's' {
		return m.Args[i].Str
	}
	return ""
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) Mirror() *state.Store { return c.mirror }

func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.status
	s.Missing = append([]string(nil), s.Missing...)
	return s
}

// Arm enables or disables writes to the console.
func (c *Client) Arm(on bool) {
	c.mu.Lock()
	c.status.Armed = on
	c.mu.Unlock()
	c.log.Info("writes", "armed", on)
}

// OnMessage registers fn for messages that are neither replies nor parameter
// updates (meters, subscription blobs).
func (c *Client) OnMessage(fn func(osc.Message)) {
	c.mu.Lock()
	c.listener = append(c.listener, fn)
	c.mu.Unlock()
}

func (c *Client) send(m osc.Message) error {
	_, err := c.conn.Write(m.Bytes())
	return err
}

// Send sends a raw message (subscriptions, meters…), regardless of arming.
func (c *Client) Send(m osc.Message) error { return c.send(m) }

// ask sends m and waits for a reply addressed key, retrying on timeout.
func (c *Client) ask(ctx context.Context, m osc.Message, key string, tries int) (osc.Message, error) {
	for try := 0; try < tries; try++ {
		ch := make(chan osc.Message, 1)
		c.mu.Lock()
		c.waiters[key] = append(c.waiters[key], ch)
		c.mu.Unlock()
		if err := c.send(m); err != nil {
			return osc.Message{}, err
		}
		select {
		case r := <-ch:
			return r, nil
		case <-ctx.Done():
			return osc.Message{}, ctx.Err()
		case <-time.After(time.Second):
			c.dropWaiter(key, ch)
		}
	}
	return osc.Message{}, ErrTimeout
}

func (c *Client) dropWaiter(key string, ch chan osc.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	list := c.waiters[key]
	for i, w := range list {
		if w == ch {
			c.waiters[key] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(c.waiters[key]) == 0 {
		delete(c.waiters, key)
	}
}

func (c *Client) readLoop() {
	buf := make([]byte, 65536)
	for {
		n, err := c.conn.Read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				c.mu.Lock()
				c.status.Connected = false
				c.mu.Unlock()
				return
			}
			continue // e.g. ICMP port unreachable while the console boots
		}
		m, err := osc.Parse(buf[:n])
		if err != nil {
			c.log.Debug("bad packet from console", "err", err)
			continue
		}
		c.handle(m)
	}
}

func (c *Client) handle(m osc.Message) {
	key := m.Address
	var line string
	if m.Address == "node" && len(m.Args) == 1 {
		line = strings.TrimSuffix(m.Args[0].Str, "\n")
		path, _ := schema.SplitLine(line)
		key = "node " + path
	}
	c.mu.Lock()
	c.status.LastSeen = time.Now()
	var waiter chan osc.Message
	if list := c.waiters[key]; len(list) > 0 {
		waiter = list[0]
		if len(list) == 1 {
			delete(c.waiters, key)
		} else {
			c.waiters[key] = list[1:]
		}
	}
	listeners := c.listener
	c.mu.Unlock()

	switch {
	case line != "":
		if _, err := c.mirror.ApplyText(line, OriginConsole); err != nil {
			c.log.Warn("node text not understood", "line", line, "err", err)
		}
	case len(m.Args) == 1 && c.mirror.Schema().Leaf(m.Address) != nil:
		if _, err := c.mirror.Set(m.Address, m.Args[0], OriginConsole); err != nil {
			c.log.Warn("update not understood", "msg", m.String(), "err", err)
		}
	case waiter == nil:
		for _, fn := range listeners {
			fn(m)
		}
	}
	if waiter != nil {
		waiter <- m
	}
}

// Sync requests every node of the schema with /node and waits for the
// replies, a window of requests at a time so the network is not flooded.
// Variant nodes (effect parameters) are requested after the rest, once the
// values selecting their fields (effect types) are known. progress, if set,
// is called after each reply, never concurrently.
func (c *Client) Sync(ctx context.Context, progress func(done, total int)) error {
	var regular, variants []*schema.Node
	for _, n := range c.mirror.Schema().Nodes {
		if n.Variant != nil {
			variants = append(variants, n)
		} else {
			regular = append(regular, n)
		}
	}
	total := len(regular) + len(variants)
	c.mu.Lock()
	c.status.Synced, c.status.Total, c.status.Missing = 0, total, nil
	c.mu.Unlock()

	var mu sync.Mutex
	done := 0
	var missing []string
	fetch := func(nodes []*schema.Node) error {
		const window = 32
		sem := make(chan struct{}, window)
		var wg sync.WaitGroup
		defer wg.Wait()
		for _, n := range nodes {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}
			wg.Add(1)
			go func(n *schema.Node) {
				defer wg.Done()
				defer func() { <-sem }()
				_, err := c.ask(ctx, osc.NewMessage("/node", osc.String(strings.TrimPrefix(n.Path, "/"))), "node "+n.Path, 3)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					missing = append(missing, n.Path)
				} else {
					done++
				}
				c.mu.Lock()
				c.status.Synced = done
				c.mu.Unlock()
				if progress != nil {
					progress(done, total) // one call at a time
				}
			}(n)
		}
		return nil
	}
	if err := fetch(regular); err != nil {
		return err
	}
	if err := fetch(variants); err != nil {
		return err
	}
	c.mu.Lock()
	c.status.Missing = missing
	c.mu.Unlock()
	if len(missing) > 0 {
		c.log.Warn("nodes the console did not answer", "count", len(missing), "first", missing[0])
	}
	return ctx.Err()
}

// Run keeps the connection alive (/xremote renewal) until ctx is done.
func (c *Client) Run(ctx context.Context) {
	c.send(osc.NewMessage("/xremote"))
	t := time.NewTicker(8 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.send(osc.NewMessage("/xremote"))
			c.mu.Lock()
			stale := time.Since(c.status.LastSeen) > 20*time.Second
			c.mu.Unlock()
			if stale {
				if _, err := c.ask(ctx, osc.NewMessage("/xinfo"), "/xinfo", 1); err != nil {
					c.mu.Lock()
					c.status.Connected = false
					c.mu.Unlock()
				}
			}
		}
	}
}

// Set writes one parameter to the console and to the mirror (the console
// does not echo changes back to the client that made them).
func (c *Client) Set(addr string, v osc.Arg) error {
	c.mu.Lock()
	armed := c.status.Armed
	c.mu.Unlock()
	if !armed {
		return ErrReadOnly
	}
	l := c.mirror.Schema().Leaf(addr)
	if l == nil {
		return fmt.Errorf("unknown parameter %s", addr)
	}
	t := c.mirror.TypeOf(l)
	if t == nil {
		return fmt.Errorf("%s: %w", addr, state.ErrNotModelled)
	}
	cv, err := t.Coerce(v)
	if err != nil {
		return fmt.Errorf("%s: %w", addr, err)
	}
	if err := c.send(osc.NewMessage(l.Addr, cv)); err != nil {
		return err
	}
	_, err = c.mirror.Set(l.Addr, cv, OriginLocal)
	return err
}

// SetText writes one parameter given as node text, e.g. "-3.5" or "GATE".
func (c *Client) SetText(addr, text string) error {
	l := c.mirror.Schema().Leaf(addr)
	if l == nil {
		return fmt.Errorf("unknown parameter %s", addr)
	}
	t := c.mirror.TypeOf(l)
	if t == nil {
		return fmt.Errorf("%s: %w", addr, state.ErrNotModelled)
	}
	v, err := t.Parse(text)
	if err != nil {
		return fmt.Errorf("%s: %w", addr, err)
	}
	return c.Set(addr, v)
}

// Discover broadcasts /xinfo on the local network and returns the consoles
// that answered within wait.
func Discover(ctx context.Context, wait time.Duration) ([]Info, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := setBroadcast(conn); err != nil {
		return nil, err
	}
	msg := osc.NewMessage("/xinfo").Bytes()
	targets := []*net.UDPAddr{{IP: net.IPv4bcast, Port: 10023}}
	if ifaces, err := net.InterfaceAddrs(); err == nil {
		for _, a := range ifaces {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
				ip := ipn.IP.To4()
				bc := make(net.IP, 4)
				for i := range bc {
					bc[i] = ip[i] | ^ipn.Mask[len(ipn.Mask)-4+i]
				}
				targets = append(targets, &net.UDPAddr{IP: bc, Port: 10023})
			}
		}
	}
	for _, t := range targets {
		conn.WriteToUDP(msg, t)
	}
	deadline := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetReadDeadline(deadline)
	seen := map[string]bool{}
	var out []Info
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		m, err := osc.Parse(buf[:n])
		if err != nil || m.Address != "/xinfo" || seen[from.String()] {
			continue
		}
		seen[from.String()] = true
		out = append(out, Info{IP: from.IP.String(), Name: str(m, 1), Model: str(m, 2), Firmware: str(m, 3)})
	}
	return out, nil
}
