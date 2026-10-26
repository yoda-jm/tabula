package webapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// hub fans events out to the browsers connected to /api/events.
type hub struct {
	mu        sync.Mutex
	subs      map[chan []byte]*sub
	meterKick chan struct{}
}

// sub is one browser's event stream.
type sub struct {
	want  map[meterKey]bool // meter groups it asked for
	grant string            // the access it was opened with, if any
	quit  chan struct{}     // closed when that access is deleted
	lost  bool              // events were dropped: it is sent "resync" first
}

// pingEvery is how often a stream gets a "ping" event, so a browser can tell
// a stream that went silent (a half-open connection) and open it again.
const pingEvery = 5 * time.Second

var resyncMsg = []byte("event: resync\ndata: {}\n\n")

func (h *hub) init() {
	h.subs = map[chan []byte]*sub{}
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
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, data))
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, sb := range h.subs {
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

func (h *hub) add(meters []meterKey, grant string) (chan []byte, <-chan struct{}) {
	ch := make(chan []byte, 256)
	sb := &sub{want: map[meterKey]bool{}, grant: grant, quit: make(chan struct{})}
	for _, k := range meters {
		sb.want[k] = true
	}
	h.mu.Lock()
	h.subs[ch] = sb
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
}

func (h *hub) remove(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

// events streams Server-Sent Events: "status" (connection state), "change"
// (an array of changed parameters), "resync" (reload the state), "record",
// "access" (the musicians' write permission changed), "meters" for the
// groups named in ?meters=, and "ping" every 5 s.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	grant := ""
	if g := callerFrom(r).Grant; g != nil {
		grant = g.ID
	}
	ch, quit := s.hub.add(parseMeterKeys(r.URL.Query().Get("meters")), grant)
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
