package webapi

import (
	"context"
	"time"

	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/state"
)

// The link to the console: the read-back (pkg/console/readback.go) finds
// the changes lost on the network and measures the link; the status tells
// its quality to the GUI, the log tells each change found, and a session
// being recorded gets "console lost" and "console back" markers.

// linkReportEvery is how often the link's quality may be published.
var linkReportEvery = 5 * time.Second

// reportLink publishes the status when the link's quality changed, until ctx is done.
func (s *Server) reportLink(ctx context.Context, c *console.Client) {
	t := time.NewTicker(linkReportEvery)
	defer t.Stop()
	var last console.Link
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if l := c.Status().Link; l != nil && *l != last {
			last = *l
			s.publishStatus()
		}
	}
}

// markLink writes the console's loss and return into the session being recorded.
func (s *Server) markLink(c *console.Client) {
	s.mu.Lock()
	rec := s.rec
	s.mu.Unlock()
	if rec == nil {
		return
	}
	if c.Status().Connected {
		rec.Mark("console back")
	} else {
		rec.Mark("console lost")
	}
}

// logReadback logs the values the read-back found changed: the console is
// right, the mirror (and the GUI, and the session) take its value.
func (s *Server) logReadback(ch []state.Change, text func(state.Change) string) {
	for _, c := range ch {
		if c.Origin == console.OriginReadback {
			s.log.Info("read back: the console has another value, a change lost on the network", "addr", c.Leaf.Addr, "value", text(c))
		}
	}
}
