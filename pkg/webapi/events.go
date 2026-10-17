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
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func (h *hub) init() { h.subs = map[chan []byte]struct{}{} }

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

func (h *hub) add() chan []byte {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) remove(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

// events streams Server-Sent Events: "status" (connection state), "change"
// (an array of changed parameters) and "resync" (reload the state).
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := s.hub.add()
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
