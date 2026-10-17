package emu

import (
	"encoding/json"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
)

// Reasons a request is recorded as not handled.
const (
	ReasonUnknown       = "unknown address"
	ReasonUnimplemented = "not implemented yet"
	ReasonRejected      = "rejected"
	ReasonMalformed     = "malformed packet"
)

// Unhandled aggregates one kind of request the emulator could not serve.
type Unhandled struct {
	Address    string    `json:"address"`
	Tags       string    `json:"tags"`
	Reason     string    `json:"reason"`
	Detail     string    `json:"detail,omitempty"`
	Count      int       `json:"count"`
	First      time.Time `json:"first"`
	Last       time.Time `json:"last"`
	LastClient string    `json:"last_client"`
	LastArgs   string    `json:"last_args,omitempty"`
}

// Unhandleds records every request the emulator did not serve: the first
// occurrence of each kind is logged as a warning, every occurrence goes to
// the optional JSON lines file, and all are kept for the debug page.
type Unhandleds struct {
	log *slog.Logger

	mu    sync.Mutex
	kinds map[string]*Unhandled
	jsonl io.Writer
}

func newUnhandleds(log *slog.Logger, jsonl io.Writer) *Unhandleds {
	return &Unhandleds{log: log, kinds: map[string]*Unhandled{}, jsonl: jsonl}
}

func (u *Unhandleds) Record(m osc.Message, client, reason, detail string) {
	now := time.Now()
	args := ""
	if len(m.Args) > 0 {
		parts := make([]string, len(m.Args))
		for i, a := range m.Args {
			parts[i] = a.String()
		}
		args = strings.Join(parts, " ")
	}
	key := reason + "\x00" + m.Address + "\x00" + m.Tags()
	if reason == ReasonRejected {
		key += "\x00" + detail
	}

	u.mu.Lock()
	k := u.kinds[key]
	first := k == nil
	if first {
		k = &Unhandled{Address: m.Address, Tags: m.Tags(), Reason: reason, Detail: detail, First: now}
		u.kinds[key] = k
	}
	k.Count++
	k.Last, k.LastClient, k.LastArgs = now, client, args
	if u.jsonl != nil {
		line, _ := json.Marshal(struct {
			Time    time.Time `json:"time"`
			Client  string    `json:"client"`
			Address string    `json:"address"`
			Tags    string    `json:"tags"`
			Args    string    `json:"args,omitempty"`
			Reason  string    `json:"reason"`
			Detail  string    `json:"detail,omitempty"`
		}{now, client, m.Address, m.Tags(), args, reason, detail})
		u.jsonl.Write(append(line, '\n'))
	}
	u.mu.Unlock()

	if first {
		u.log.Warn("unhandled request", "reason", reason, "address", m.Address, "tags", m.Tags(),
			"args", args, "detail", detail, "client", client)
	} else {
		u.log.Debug("unhandled request", "reason", reason, "address", m.Address, "client", client)
	}
}

// List returns all kinds, most frequent first.
func (u *Unhandleds) List() []Unhandled {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]Unhandled, 0, len(u.kinds))
	for _, k := range u.kinds {
		out = append(out, *k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Address < out[j].Address
	})
	return out
}

// Packet is one message seen on the wire, kept for the debug page.
type Packet struct {
	Time   time.Time `json:"time"`
	In     bool      `json:"in"`
	Client string    `json:"client"`
	Text   string    `json:"text"`
}

// traffic is a ring of the latest packets.
type traffic struct {
	mu   sync.Mutex
	ring []Packet
	next int
	full bool
}

func newTraffic(size int) *traffic { return &traffic{ring: make([]Packet, max(size, 1))} }

func (t *traffic) add(p Packet) {
	t.mu.Lock()
	t.ring[t.next] = p
	t.next = (t.next + 1) % len(t.ring)
	t.full = t.full || t.next == 0
	t.mu.Unlock()
}

// latest returns up to n packets, newest first.
func (t *traffic) latest(n int) []Packet {
	t.mu.Lock()
	defer t.mu.Unlock()
	size := t.next
	if t.full {
		size = len(t.ring)
	}
	n = min(n, size)
	out := make([]Packet, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, t.ring[(t.next-1-i+len(t.ring))%len(t.ring)])
	}
	return out
}
