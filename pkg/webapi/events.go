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
	subs      map[chan []byte]map[meterKey]bool // meter groups each browser wants
	meterKick chan struct{}
}

func (h *hub) init() {
	h.subs = map[chan []byte]map[meterKey]bool{}
	h.meterKick = make(chan struct{}, 1)
}

// publishMeters sends meter values to the browsers that asked for key.
func (h *hub) publishMeters(key meterKey, data []byte) {
	msg := []byte(fmt.Sprintf("event: meters\ndata: %s\n\n", data))
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, want := range h.subs {
		if want[key] {
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
	for _, want := range h.subs {
		for k := range want {
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
	for ch := range h.subs {
		select {
		case ch <- msg:
		default: // a slow browser drops events; it resyncs on "resync"
		}
	}
}

func (h *hub) add(meters []meterKey) chan []byte {
	ch := make(chan []byte, 256)
	want := map[meterKey]bool{}
	for _, k := range meters {
		want[k] = true
	}
	h.mu.Lock()
	h.subs[ch] = want
	h.mu.Unlock()
	if len(meters) > 0 {
		select {
		case h.meterKick <- struct{}{}:
		default:
		}
	}
	return ch
}

func (h *hub) remove(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

// events streams Server-Sent Events: "status" (connection state), "change"
// (an array of changed parameters), "resync" (reload the state), "record",
// and "meters" for the groups named in ?meters=.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := s.hub.add(parseMeterKeys(r.URL.Query().Get("meters")))
	defer s.hub.remove(ch)

	first, _ := json.Marshal(s.status())
	fmt.Fprintf(w, "event: status\ndata: %s\n\n", first)
	flusher.Flush()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			w.Write(msg)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
