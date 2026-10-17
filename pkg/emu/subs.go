package emu

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
)

// Subscriptions (/meters, /subscribe, /formatsubscribe, /batchsubscribe)
// last 10 s unless renewed; updates are sent every 50 ms × time factor.
const (
	subLease = 10 * time.Second
	subTick  = 50 * time.Millisecond
)

type subscription struct {
	client   *Client
	name     string // what /renew and /unsubscribe match: alias or command
	kind     string
	interval time.Duration
	expires  time.Time
	next     time.Time
	produce  func(now time.Time) (osc.Message, bool)
}

// Subscription is the debug view of an active subscription.
type Subscription struct {
	Client   string        `json:"client"`
	Kind     string        `json:"kind"`
	Name     string        `json:"name"`
	Interval time.Duration `json:"interval"`
	Expires  time.Time     `json:"expires"`
}

type subscriptions struct {
	mu   sync.Mutex
	list []*subscription
}

func (ss *subscriptions) add(s *subscription) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	// A new request for the same name replaces the previous one.
	for i, o := range ss.list {
		if o.client == s.client && o.name == s.name {
			ss.list[i] = s
			return
		}
	}
	ss.list = append(ss.list, s)
}

func sameName(a, b string) bool { return strings.TrimPrefix(a, "/") == strings.TrimPrefix(b, "/") }

// renew extends the client's subscriptions named name, or all of them.
func (ss *subscriptions) renew(c *Client, name string, now time.Time) int {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	n := 0
	for _, s := range ss.list {
		if s.client == c && (name == "" || sameName(s.name, name)) && s.expires.After(now) {
			s.expires = now.Add(subLease)
			n++
		}
	}
	return n
}

func (ss *subscriptions) remove(c *Client, name string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	kept := ss.list[:0]
	for _, s := range ss.list {
		if s.client == c && (name == "" || sameName(s.name, name)) {
			continue
		}
		kept = append(kept, s)
	}
	ss.list = kept
}

// due returns the subscriptions to serve now and forgets expired ones.
func (ss *subscriptions) due(now time.Time) []*subscription {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	var out []*subscription
	kept := ss.list[:0]
	for _, s := range ss.list {
		if !s.expires.After(now) {
			continue
		}
		kept = append(kept, s)
		if !s.next.After(now) {
			out = append(out, s)
			s.next = s.next.Add(s.interval)
			if s.next.Before(now) {
				s.next = now.Add(s.interval)
			}
		}
	}
	ss.list = kept
	return out
}

func (ss *subscriptions) view() []Subscription {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	out := make([]Subscription, 0, len(ss.list))
	for _, s := range ss.list {
		out = append(out, Subscription{s.client.Addr, s.kind, s.name, s.interval, s.expires})
	}
	return out
}

func (s *Server) runSubscriptions(ctx context.Context) {
	t := time.NewTicker(subTick / 5)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for _, sub := range s.subs.due(now) {
				if m, ok := sub.produce(now); ok {
					s.sendQuiet(sub.client, m)
				}
			}
		}
	}
}

// interval converts a time factor: 50 ms × tf, tf outside [1, 99] counting as 1.
func interval(tf int32) time.Duration {
	if tf < 1 || tf > 99 {
		tf = 1
	}
	return time.Duration(tf) * subTick
}

func intArgs(args []osc.Arg) ([]int32, bool) {
	out := make([]int32, len(args))
	for i, a := range args {
		if a.Tag != 'i' {
			return nil, false
		}
		out[i] = a.Int
	}
	return out, true
}

// meterArgCount is the number of int parameters each /meters/N request takes.
var meterArgCount = map[int]int{5: 2, 6: 1}

// meters handles "/meters ,s[i…] /meters/N [args] [time_factor]".
func (s *Server) meters(m osc.Message, c *Client) error {
	if len(m.Args) == 0 || m.Args[0].Tag != 's' {
		return fmt.Errorf("expects a meter address")
	}
	path := m.Args[0].Str
	id, err := meterID(path)
	if err != nil {
		return err
	}
	ints, ok := intArgs(m.Args[1:])
	if !ok {
		return fmt.Errorf("meter parameters must be ints")
	}
	params := ints[:min(len(ints), meterArgCount[id])]
	var tf int32
	if len(ints) > len(params) {
		tf = ints[len(params)]
	}
	addr := "/meters/" + fmt.Sprint(id)
	name := addr
	for _, p := range params {
		name += fmt.Sprintf(" %d", p)
	}
	now := time.Now()
	s.subs.add(&subscription{
		client: c, name: name, kind: "meters", interval: interval(tf), expires: now.Add(subLease), next: now,
		produce: func(now time.Time) (osc.Message, bool) {
			return osc.NewMessage(addr, osc.Blob(meterBlob(s.meterValues(id, params, now)))), true
		},
	})
	return nil
}

func meterID(path string) (int, error) {
	var id int
	if _, err := fmt.Sscanf(strings.TrimPrefix(path, "/"), "meters/%d", &id); err != nil || id < 0 || id > 16 {
		return 0, fmt.Errorf("unknown meter %q", path)
	}
	return id, nil
}

// meterBlob is the blob payload: a little-endian count, then little-endian
// floats (or packed values already in uint32 form for meters 15 and 16).
func meterBlob(v []uint32) []byte {
	b := make([]byte, 4+4*len(v))
	binary.LittleEndian.PutUint32(b, uint32(len(v)))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4+4*i:], x)
	}
	return b
}

// subscribe handles "/subscribe ,s[i] <address> [tf]".
func (s *Server) subscribe(m osc.Message, c *Client) error {
	if len(m.Args) == 0 || m.Args[0].Tag != 's' {
		return fmt.Errorf("expects an address")
	}
	addr := m.Args[0].Str
	if s.store.Schema().Leaf(addr) == nil {
		return fmt.Errorf("unknown parameter %s", addr)
	}
	var tf int32
	if len(m.Args) > 1 && m.Args[1].Tag == 'i' {
		tf = m.Args[1].Int
	}
	now := time.Now()
	s.subs.add(&subscription{
		client: c, name: addr, kind: "subscribe", interval: interval(tf), expires: now.Add(subLease), next: now,
		produce: func(time.Time) (osc.Message, bool) {
			v, l, err := s.store.Get(addr)
			if err != nil {
				return osc.Message{}, false
			}
			return osc.NewMessage(l.Addr, v), true
		},
	})
	return nil
}

// formatSubscribe handles "/formatsubscribe ,ss[s…]iii <alias> <cmd>… <i0> <i1> <tf>";
// runs of '*' in a command take each number of [i0, i1], zero-padded.
func (s *Server) formatSubscribe(m osc.Message, c *Client) error {
	if len(m.Args) < 5 {
		return fmt.Errorf("expects an alias, commands and three ints")
	}
	ints, ok := intArgs(m.Args[len(m.Args)-3:])
	if !ok {
		return fmt.Errorf("the three last arguments must be ints")
	}
	alias := ""
	var addrs []string
	for i, a := range m.Args[:len(m.Args)-3] {
		if a.Tag != 's' {
			return fmt.Errorf("argument %d must be a string", i+1)
		}
		if i == 0 {
			alias = a.Str
			continue
		}
		if !strings.Contains(a.Str, "*") {
			addrs = append(addrs, a.Str)
			continue
		}
		for n := ints[0]; n <= ints[1]; n++ {
			addrs = append(addrs, expandStars(a.Str, n))
		}
	}
	for _, a := range addrs {
		if s.store.Schema().Leaf(a) == nil {
			return fmt.Errorf("unknown parameter %s", a)
		}
	}
	now := time.Now()
	s.subs.add(&subscription{
		client: c, name: alias, kind: "formatsubscribe", interval: interval(ints[2]), expires: now.Add(subLease), next: now,
		produce: func(time.Time) (osc.Message, bool) {
			body := make([]byte, 4, 4+4*len(addrs))
			for _, a := range addrs {
				v, _, _ := s.store.Get(a)
				switch v.Tag {
				case 'f':
					body = binary.LittleEndian.AppendUint32(body, math.Float32bits(v.Float))
				case 's':
					str := make([]byte, 32)
					copy(str, v.Str)
					body = append(body, str...)
				default:
					body = binary.LittleEndian.AppendUint32(body, uint32(v.Int))
				}
			}
			binary.LittleEndian.PutUint32(body, uint32(len(body)))
			return osc.NewMessage(alias, osc.Blob(body)), true
		},
	})
	return nil
}

func expandStars(pattern string, n int32) string {
	start := strings.IndexByte(pattern, '*')
	end := start
	for end < len(pattern) && pattern[end] == '*' {
		end++
	}
	return pattern[:start] + fmt.Sprintf("%0*d", end-start, n) + pattern[end:]
}

// batchSubscribe handles "/batchsubscribe ,ssiii <alias> /meters/N <a> <b> <tf>".
func (s *Server) batchSubscribe(m osc.Message, c *Client) error {
	if len(m.Args) != 5 || m.Args[0].Tag != 's' || m.Args[1].Tag != 's' {
		return fmt.Errorf("expects ,ssiii")
	}
	ints, ok := intArgs(m.Args[2:])
	if !ok {
		return fmt.Errorf("expects ,ssiii")
	}
	alias := m.Args[0].Str
	id, err := meterID(m.Args[1].Str)
	if err != nil {
		return err
	}
	params := ints[:meterArgCount[id]]
	now := time.Now()
	s.subs.add(&subscription{
		client: c, name: alias, kind: "batchsubscribe", interval: interval(ints[2]), expires: now.Add(subLease), next: now,
		produce: func(now time.Time) (osc.Message, bool) {
			return osc.NewMessage(alias, osc.Blob(meterBlob(s.meterValues(id, params, now)))), true
		},
	})
	return nil
}
