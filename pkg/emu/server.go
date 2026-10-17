// Package emu emulates an X32/M32 console on the network: it answers the
// console's OSC protocol from a state store and pushes changes to the
// clients registered with /xremote.
package emu

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

// xremoteLease is how long a client keeps receiving changes after /xremote.
const xremoteLease = 10 * time.Second

// Config describes the emulated console.
type Config struct {
	Model         schema.Model // reported by /info and /xinfo, e.g. "M32C"
	Name          string       // network name, reported by /xinfo
	Firmware      string       // console version, e.g. "4.06"
	ServerVersion string       // OSC server version, e.g. "V2.07"
	Traffic       int          // packets kept for the debug page
	UnhandledLog  io.Writer    // optional JSON lines log of unhandled requests
	Logger        *slog.Logger
}

// Client is a remote peer seen by the emulator.
type Client struct {
	Addr         string    `json:"addr"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	Requests     int       `json:"requests"`
	XremoteUntil time.Time `json:"xremote_until"`
	Info         string    `json:"info,omitempty"` // how the client identified itself, if it did

	udp     *net.UDPAddr
	localIP string
}

// Server is the emulated console.
type Server struct {
	cfg     Config
	store   *state.Store
	log     *slog.Logger
	started time.Time

	conn atomic.Pointer[net.UDPConn]

	mu      sync.Mutex
	clients map[string]*Client

	Unhandled *Unhandleds
	traffic   *traffic
}

func New(cfg Config, st *state.Store) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Name == "" {
		cfg.Name = "tabula-emu"
	}
	if cfg.Firmware == "" {
		cfg.Firmware = "4.06"
	}
	if cfg.ServerVersion == "" {
		cfg.ServerVersion = "V2.07"
	}
	if cfg.Traffic == 0 {
		cfg.Traffic = 1000
	}
	s := &Server{
		cfg: cfg, store: st, log: cfg.Logger, started: time.Now(),
		clients:   map[string]*Client{},
		Unhandled: newUnhandleds(cfg.Logger, cfg.UnhandledLog),
		traffic:   newTraffic(cfg.Traffic),
	}
	st.Subscribe(s.broadcast)
	return s
}

func (s *Server) Config() Config         { return s.cfg }
func (s *Server) Store() *state.Store    { return s.store }
func (s *Server) Started() time.Time     { return s.started }
func (s *Server) Traffic(n int) []Packet { return s.traffic.latest(n) }

// Clients returns the peers seen so far, most recent first.
func (s *Server) Clients() []Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Client, 0, len(s.clients))
	for _, c := range s.clients {
		out = append(out, *c)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].LastSeen.After(out[j-1].LastSeen); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// ListenAndServe serves on addr (e.g. ":10023") until ctx is done.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", ua)
	if err != nil {
		return err
	}
	return s.Serve(ctx, conn)
}

// Addr returns the local address once serving.
func (s *Server) Addr() net.Addr {
	if c := s.conn.Load(); c != nil {
		return c.LocalAddr()
	}
	return nil
}

func (s *Server) Serve(ctx context.Context, conn *net.UDPConn) error {
	s.conn.Store(conn)
	s.log.Info("console emulator listening", "addr", conn.LocalAddr().String(), "model", s.cfg.Model, "name", s.cfg.Name)
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	buf := make([]byte, 65536)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.handle(buf[:n], from)
	}
}

func (s *Server) client(from *net.UDPAddr) *Client {
	key := from.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.clients[key]
	if c == nil {
		c = &Client{Addr: key, FirstSeen: time.Now(), udp: from}
		s.clients[key] = c
		s.log.Info("new client", "client", key)
	}
	c.LastSeen = time.Now()
	c.Requests++
	return c
}

func (s *Server) handle(pkt []byte, from *net.UDPAddr) {
	c := s.client(from)
	m, err := osc.Parse(pkt)
	if err != nil {
		s.traffic.add(Packet{Time: time.Now(), In: true, Client: c.Addr, Text: "<malformed> " + err.Error()})
		s.Unhandled.Record(osc.Message{Address: "<malformed>"}, c.Addr, ReasonMalformed, err.Error())
		return
	}
	s.traffic.add(Packet{Time: time.Now(), In: true, Client: c.Addr, Text: m.String()})
	s.dispatch(m, c)
}

func (s *Server) send(c *Client, m osc.Message) {
	if _, err := s.conn.Load().WriteToUDP(m.Bytes(), c.udp); err != nil {
		s.log.Warn("send failed", "client", c.Addr, "err", err)
		return
	}
	s.traffic.add(Packet{Time: time.Now(), Client: c.Addr, Text: m.String()})
}

func strArg(m osc.Message) (string, bool) {
	if len(m.Args) == 1 && m.Args[0].Tag == 's' {
		return m.Args[0].Str, true
	}
	return "", false
}

func (s *Server) dispatch(m osc.Message, c *Client) {
	origin := "udp " + c.Addr
	switch m.Address {
	case "/info":
		s.send(c, osc.NewMessage("/info", osc.String(s.cfg.ServerVersion), osc.String("osc-server"),
			osc.String(string(s.cfg.Model)), osc.String(s.cfg.Firmware)))
		return
	case "/xinfo":
		s.send(c, osc.NewMessage("/xinfo", osc.String(s.localIP(c)), osc.String(s.cfg.Name),
			osc.String(string(s.cfg.Model)), osc.String(s.cfg.Firmware)))
		return
	case "/status":
		s.send(c, osc.NewMessage("/status", osc.String("active"), osc.String(s.localIP(c)), osc.String(s.cfg.Name)))
		return
	case "/xremote":
		s.mu.Lock()
		c.XremoteUntil = time.Now().Add(xremoteLease)
		s.mu.Unlock()
		return
	case "/node":
		path, ok := strArg(m)
		if !ok {
			s.Unhandled.Record(m, c.Addr, ReasonRejected, "expects one string")
			return
		}
		s.node(m, c, path)
		return
	case "/":
		text, ok := strArg(m)
		if !ok {
			s.Unhandled.Record(m, c.Addr, ReasonRejected, "expects one string")
			return
		}
		if _, err := s.store.ApplyText(text, origin); err != nil {
			s.Unhandled.Record(m, c.Addr, ReasonRejected, err.Error())
		}
		s.send(c, m) // the console echoes "/" commands
		return
	case "/unsubscribe", "/renew", "/subscribe", "/formatsubscribe", "/batchsubscribe",
		"/meters", "/showdump", "/load", "/save", "/copy", "/delete", "/rename", "/add":
		s.Unhandled.Record(m, c.Addr, ReasonUnimplemented, "")
		return
	}

	if l := s.store.Schema().Leaf(m.Address); l != nil {
		if _, raw := l.Type().(schema.Raw); raw {
			s.Unhandled.Record(m, c.Addr, ReasonUnimplemented, "effect parameters are not modelled yet")
			return
		}
		switch len(m.Args) {
		case 0:
			v, _, _ := s.store.Get(l.Addr)
			s.send(c, osc.NewMessage(l.Addr, v))
		case 1:
			if _, err := s.store.Set(l.Addr, m.Args[0], origin); err != nil {
				s.Unhandled.Record(m, c.Addr, ReasonRejected, err.Error())
			}
		default:
			s.Unhandled.Record(m, c.Addr, ReasonRejected, "one value expected")
		}
		return
	}
	if n := s.store.Schema().Node(m.Address); n != nil && len(m.Args) > 0 {
		// Multiple type tags on a node address set its leading fields; the
		// console only accepts ints and floats here.
		for _, a := range m.Args {
			if a.Tag != 'i' && a.Tag != 'f' {
				s.Unhandled.Record(m, c.Addr, ReasonRejected, "node sets take ints and floats only")
				return
			}
		}
		if _, err := s.store.SetNode(n, m.Args, origin); err != nil {
			s.Unhandled.Record(m, c.Addr, ReasonRejected, err.Error())
		}
		return
	}
	s.Unhandled.Record(m, c.Addr, ReasonUnknown, "")
}

// node answers /node: a node line, or one parameter as text, "\n"-terminated,
// sent under the address "node" (without a slash, as the console does).
func (s *Server) node(m osc.Message, c *Client, path string) {
	sch := s.store.Schema()
	var line string
	if l := sch.Leaf(path); l != nil {
		line = s.store.LeafLine(l)
	} else if n := sch.Resolve(path); n != nil {
		line = s.store.NodeLine(n)
	} else {
		s.Unhandled.Record(m, c.Addr, ReasonUnknown, "/node "+path)
		return
	}
	s.send(c, osc.NewMessage("node", osc.String(line+"\n")))
}

// broadcast pushes changes to the /xremote clients other than their origin.
func (s *Server) broadcast(changes []state.Change) {
	if s.conn.Load() == nil {
		return
	}
	now := time.Now()
	s.mu.Lock()
	var targets []*Client
	for _, c := range s.clients {
		if c.XremoteUntil.After(now) {
			targets = append(targets, c)
		}
	}
	s.mu.Unlock()
	for _, c := range targets {
		origin := "udp " + c.Addr
		for _, ch := range changes {
			if ch.Origin == origin {
				continue
			}
			if _, raw := ch.Leaf.Type().(schema.Raw); raw {
				continue
			}
			s.send(c, osc.NewMessage(ch.Leaf.Addr, ch.Value))
		}
	}
}

// localIP is the address of this host as seen from the client.
func (s *Server) localIP(c *Client) string {
	s.mu.Lock()
	ip := c.localIP
	s.mu.Unlock()
	if ip != "" {
		return ip
	}
	ip = "127.0.0.1"
	if conn, err := net.DialUDP("udp", nil, c.udp); err == nil {
		ip = conn.LocalAddr().(*net.UDPAddr).IP.String()
		conn.Close()
	}
	if la, ok := s.conn.Load().LocalAddr().(*net.UDPAddr); ok && !la.IP.IsUnspecified() && la.IP != nil {
		ip = la.IP.String()
	}
	ip = strings.TrimPrefix(ip, "::ffff:")
	s.mu.Lock()
	c.localIP = ip
	s.mu.Unlock()
	return ip
}
