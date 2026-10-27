package webapi

import (
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"
)

// hub fans events out to the browsers connected to /api/events.
type hub struct {
	mu        sync.Mutex
	subs      map[chan []byte]*sub
	seen      map[string]presence // by access, since tabula started
	meterKick chan struct{}
}

// sub is one browser's event stream.
type sub struct {
	want     map[meterKey]bool // meter groups it asked for
	grant    string            // the access it was opened with, if any
	addr     string            // the browser's IP address
	local    bool              // from this machine
	engineer bool              // it gets the "online" events
	quit     chan struct{}     // closed when that access is deleted
	lost     bool              // events were dropped: it is sent "resync" first
}

// presence tells whether an access is in use: a page open with it keeps an
// event stream open.
type presence struct {
	Streams int       `json:"streams"`        // event streams open now; 0 is offline
	Addr    string    `json:"addr,omitempty"` // the IP address of its last stream
	Seen    time.Time `json:"seen"`           // when it last opened or closed one
}

// onlineView tells which devices have a page open, for the Access page.
type onlineView struct {
	Grants map[string]presence `json:"grants"` // by access; one never opened is left out
	Guests []string            `json:"guests"` // addresses of devices open without an access
}

// pingEvery is how often a stream gets a "ping" event, so a browser can tell
// a stream that went silent (a half-open connection) and open it again.
const pingEvery = 5 * time.Second

var resyncMsg = []byte("event: resync\ndata: {}\n\n")

func (h *hub) init() {
	h.subs = map[chan []byte]*sub{}
	h.seen = map[string]presence{}
	h.meterKick = make(chan struct{}, 1)
}

// publishMeters sends meter values to the browsers that asked for key.
func (h *hub) publishMeters(key meterKey, data []byte) {
	msg := []byte(fmt.Sprintf("event: meters\ndata: %s\n\n", data))
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, sb := range h.subs {
		if sb.want[key] {
			select {
			case ch <- msg:
			default:
			}
		}
	}
}

// wantedMeters returns the meter groups at least one browser wants.
func (h *hub) wantedMeters() []meterKey {
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := map[meterKey]bool{}
	var out []meterKey
	for _, sb := range h.subs {
		for k := range sb.want {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	return out
}

func (h *hub) publish(event string, v any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.publishLocked(event, v, nil)
}

// publishEngineers sends an event to the engineers' streams only.
func (h *hub) publishEngineers(event string, v any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.publishLocked(event, v, func(sb *sub) bool { return sb.engineer })
}

// publishLocked sends an event to the streams to (all of them when nil)
// allows.
func (h *hub) publishLocked(event string, v any, to func(*sub) bool) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, data))
	for ch, sb := range h.subs {
		if to != nil && !to(sb) {
			continue
		}
		// a slow browser drops events: once there is room again, it is told
		// to load the state again before it gets the next ones
		if sb.lost {
			select {
			case ch <- resyncMsg:
				sb.lost = false
			default:
				continue
			}
		}
		select {
		case ch <- msg:
		default:
			sb.lost = true
		}
	}
}

// add opens an event stream for a browser of addr (its IP address).
func (h *hub) add(meters []meterKey, c caller, addr string) (chan []byte, <-chan struct{}) {
	ch := make(chan []byte, 256)
	sb := &sub{want: map[meterKey]bool{}, addr: addr, local: c.Local, engineer: c.Role == RoleEngineer, quit: make(chan struct{})}
	if c.Grant != nil {
		sb.grant = c.Grant.ID
	}
	for _, k := range meters {
		sb.want[k] = true
	}
	h.mu.Lock()
	h.subs[ch] = sb
	if sb.grant != "" {
		p := h.seen[sb.grant]
		p.Streams++
		p.Addr, p.Seen = addr, time.Now()
		h.seen[sb.grant] = p
	}
	h.publishOnlineLocked()
	h.mu.Unlock()
	if len(meters) > 0 {
		select {
		case h.meterKick <- struct{}{}:
		default:
		}
	}
	return ch, sb.quit
}

// kick ends the event streams opened with the access grant.
func (h *hub) kick(grant string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, sb := range h.subs {
		if sb.grant == grant {
			close(sb.quit)
			delete(h.subs, ch)
		}
	}
	delete(h.seen, grant)
	h.publishOnlineLocked()
}

func (h *hub) remove(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sb, ok := h.subs[ch]
	if !ok {
		return // kicked
	}
	delete(h.subs, ch)
	if p, ok := h.seen[sb.grant]; ok {
		p.Streams--
		p.Seen = time.Now()
		h.seen[sb.grant] = p
	}
	h.publishOnlineLocked()
}

// online tells which devices have a page open.
func (h *hub) online() onlineView {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.onlineLocked()
}

func (h *hub) onlineLocked() onlineView {
	v := onlineView{Grants: maps.Clone(h.seen), Guests: []string{}}
	for _, sb := range h.subs {
		if sb.grant == "" && !sb.local && !slices.Contains(v.Guests, sb.addr) {
			v.Guests = append(v.Guests, sb.addr)
		}
	}
	slices.Sort(v.Guests)
	return v
}

// publishOnlineLocked tells the engineers' pages which devices are online,
// each time a page opens or closes.
func (h *hub) publishOnlineLocked() {
	h.publishLocked("online", h.onlineLocked(), func(sb *sub) bool { return sb.engineer })
}

// events streams Server-Sent Events: "status" (connection state), "change"
// (an array of changed parameters), "resync" (reload the state), "record",
// "access" (the musicians' write permission changed), "online" (which
// devices have a page open, for engineers only), "undo" (what Undo would
// undo, for engineers only), "meters" for the groups
// named in ?meters=, and "ping" every 5 s.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	addr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		addr = r.RemoteAddr
	}
	ch, quit := s.hub.add(parseMeterKeys(r.URL.Query().Get("meters")), callerFrom(r), addr)
	defer s.hub.remove(ch)

	first, _ := json.Marshal(s.status())
	fmt.Fprintf(w, "event: status\ndata: %s\n\n", first)
	flusher.Flush()
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-quit:
			return
		case msg := <-ch:
			w.Write(msg)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, "event: ping\ndata: {}\n\n")
			flusher.Flush()
		}
	}
}
