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
// last 10 s unless renewed; updates are sent every 50 ms × time factor. A
// client holds at most maxSubs of them at once.
const (
	subLease = 10 * time.Second
	subTick  = 50 * time.Millisecond
	maxSubs  = 64
)

type subscription struct {
	client   *Client
	name     string // what /renew and /unsubscribe match: alias or command
	kind     string
	interval time.Duration
	expires  time.Time
	next     time.Time
	produce  func(t *tick) (osc.Message, bool)
}

// tick is one round of updates: the subscriptions served at the same time
// share the meter levels, computed once, when the first of them needs them.
type tick struct {
	s   *Server
	now time.Time
	lv  *mixLevels
}

func (t *tick) levels() *mixLevels {
	if t.lv == nil {
		t.lv = t.s.levels(t.now)
	}
	return t.lv
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

// add starts s, refused when its client already holds maxSubs live
// subscriptions.
func (ss *subscriptions) add(s *subscription) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	live := 0
	for i, o := range ss.list {
		if o.client != s.client {
			continue
		}
		// A new request for the same name replaces the previous one.
		if o.name == s.name {
			ss.list[i] = s
			return nil
		}
		if o.expires.After(s.next) { // s.next is now: a subscription starts at once
			live++
		}
	}
	if live >= maxSubs {
		return fmt.Errorf("%d subscriptions already, the most a client can hold", maxSubs)
	}
	ss.list = append(ss.list, s)
	return nil
}

// matches reports whether a /renew or /unsubscribe naming name concerns s.
// The leading slash is optional (the document's example has none), and a
// meter subscription also answers to its address alone: "/meters/6" for the
// one named "/meters/6 16".
func (s *subscription) matches(name string) bool {
	if name == "" {
		return true
	}
	own, name := strings.TrimPrefix(s.name, "/"), strings.TrimPrefix(name, "/")
	if addr, _, _ := strings.Cut(own, " "); s.kind == "meters" && addr == name {
		return true
	}
	return own == name
}

// renew extends the client's subscriptions named name, or all of them.
func (ss *subscriptions) renew(c *Client, name string, now time.Time) int {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	n := 0
	for _, s := range ss.list {
		if s.client == c && s.matches(name) && s.expires.After(now) {
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
		if s.client == c && s.matches(name) {
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

// clients returns the clients holding a live subscription.
func (ss *subscriptions) clients(now time.Time) map[*Client]bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	out := map[*Client]bool{}
	for _, s := range ss.list {
		if s.expires.After(now) {
			out[s.client] = true
		}
	}
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
			tk := &tick{s: s, now: now}
			for _, sub := range s.subs.due(now) {
				if m, ok := sub.produce(tk); ok {
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
	return s.subs.add(&subscription{
		client: c, name: name, kind: "meters", interval: interval(tf), expires: now.Add(subLease), next: now,
		produce: func(t *tick) (osc.Message, bool) {
			return osc.NewMessage(addr, osc.Blob(meterBlob(s.meterValues(id, params, t.levels(), t.now)))), true
		},
	})
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
	return s.subs.add(&subscription{
		client: c, name: addr, kind: "subscribe", interval: interval(tf), expires: now.Add(subLease), next: now,
		produce: func(*tick) (osc.Message, bool) {
			v, l, err := s.store.Get(addr)
			if err != nil {
				return osc.Message{}, false
			}
			return osc.NewMessage(l.Addr, v), true
		},
	})
}

// The widest range a /formatsubscribe pattern can take (the console's widest,
// the USB stick listing /-usb/dir/001…999), and the most parameters it can
// return: their 32-byte strings still fit in one packet.
const (
	maxFormatRange  = 1000
	maxFormatParams = 1024
)

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
	add := func(a string) error {
		if s.store.Schema().Leaf(a) == nil {
			return fmt.Errorf("unknown parameter %s", a)
		}
		if len(addrs) == maxFormatParams {
			return fmt.Errorf("more than %d parameters", maxFormatParams)
		}
		addrs = append(addrs, a)
		return nil
	}
	for i, a := range m.Args[:len(m.Args)-3] {
		if a.Tag != 's' {
			return fmt.Errorf("argument %d must be a string", i+1)
		}
		if i == 0 {
			alias = a.Str
			continue
		}
		if !strings.Contains(a.Str, "*") {
			if err := add(a.Str); err != nil {
				return err
			}
			continue
		}
		// The range is counted in 64 bits: an int32 loop up to 2147483647
		// would never end.
		lo, hi := int64(ints[0]), int64(ints[1])
		if hi < lo || hi-lo >= maxFormatRange {
			return fmt.Errorf("range %d to %d: expects at most %d numbers, in order", lo, hi, maxFormatRange)
		}
		for n := lo; n <= hi; n++ {
			if err := add(expandStars(a.Str, int32(n))); err != nil {
				return err
			}
		}
	}
	now := time.Now()
	return s.subs.add(&subscription{
		client: c, name: alias, kind: "formatsubscribe", interval: interval(ints[2]), expires: now.Add(subLease), next: now,
		produce: func(*tick) (osc.Message, bool) {
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
	return s.subs.add(&subscription{
		client: c, name: alias, kind: "batchsubscribe", interval: interval(ints[2]), expires: now.Add(subLease), next: now,
		produce: func(t *tick) (osc.Message, bool) {
			return osc.NewMessage(alias, osc.Blob(meterBlob(s.meterValues(id, params, t.levels(), t.now)))), true
		},
	})
}
