package console

import (
	"context"
	"math"
	"regexp"
	"slices"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

// The read-back: a change the console sends over UDP can be lost (a weak
// Wi-Fi), and the mirror then shows an old value until the next sync. While
// connected, every node is asked again in turn, slowly; a value that
// differs is taken from the console (it is right: nothing is written to
// it), applied with OriginReadback so the log and the session tell it. The
// same requests measure the link: delay and unanswered requests.

// OriginReadback marks the changes the read-back found: the console had
// another value than the mirror, a change lost on the network.
const OriginReadback = "console, read back"

// readbackEvery is the pace of the read-back: 25 nodes a second, every
// other one from the mixes (faders, mutes, sends: about once a minute),
// the rest of the console in about 3 minutes.
const readbackEvery = 40 * time.Millisecond

// hotNodes are the nodes read back more often: what a mix is made of.
var hotNodes = regexp.MustCompile(`^/(ch|auxin|fxrtn|bus|mtx)/\d\d/mix(/\d\d)?$|^/main/(st|m)/mix|^/dca/\d$|^/config/mute`)

// linkWindow is how many read-backs the link quality is measured on.
const linkWindow = 100

// Link is the quality of the link to the console, measured by the read-back.
type Link struct {
	RTT        float64   `json:"rtt_ms"` // smoothed round trip of a request, in ms
	Asked      int       `json:"asked"`  // read-backs measured (the last ones, up to 100)
	Lost       int       `json:"lost"`   // of them, unanswered within a second
	Caught     int       `json:"caught"` // values found changed since the connection (changes lost)
	LastCaught string    `json:"last_caught,omitempty"`
	CaughtAt   time.Time `json:"caught_at,omitzero"`
}

type linkStats struct {
	rtt        float64
	lost       [linkWindow]bool
	n, next    int
	caught     int
	lastCaught string
	caughtAt   time.Time
}

func (l *linkStats) add(rtt time.Duration, answered bool) {
	l.lost[l.next] = !answered
	l.next = (l.next + 1) % linkWindow
	l.n = min(l.n+1, linkWindow)
	if answered {
		ms := float64(rtt) / float64(time.Millisecond)
		if l.rtt == 0 {
			l.rtt = ms
		} else {
			l.rtt += (ms - l.rtt) / 10
		}
	}
}

func (l *linkStats) view() *Link {
	if l.n == 0 {
		return nil
	}
	v := &Link{RTT: math.Round(l.rtt*10) / 10, Asked: l.n, Caught: l.caught, LastCaught: l.lastCaught, CaughtAt: l.caughtAt}
	for _, lost := range l.lost[:l.n] {
		if lost {
			v.Lost++
		}
	}
	return v
}

// readbackOrder splits the nodes to read back: the mixes, and the rest.
func readbackOrder(s *schema.Schema) (hot, rest []*schema.Node) {
	for _, n := range s.Nodes {
		switch {
		case n.Lazy:
		case hotNodes.MatchString(n.Path):
			hot = append(hot, n)
		default:
			rest = append(rest, n)
		}
	}
	return hot, rest
}

// readBack asks the nodes again in turn while connected, until ctx is done.
func (c *Client) readBack(ctx context.Context) {
	hot, rest := readbackOrder(c.mirror.Schema())
	t := time.NewTicker(c.readbackEvery)
	defer t.Stop()
	h, r := 0, 0
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		var n *schema.Node
		if i%2 == 0 && len(hot) > 0 || len(rest) == 0 {
			n, h = hot[h%len(hot)], h+1
		} else {
			n, r = rest[r%len(rest)], r+1
		}
		c.mu.Lock()
		connected := c.status.Connected
		// a node the console did not answer in the sync would count as lost each time
		never := slices.Contains(c.status.Missing, n.Path)
		c.mu.Unlock()
		if connected && !never { // not lost, nor syncing again
			go c.readBackNode(ctx, n)
		}
	}
}

func (c *Client) readBackNode(ctx context.Context, n *schema.Node) {
	start := time.Now()
	c.mu.Lock()
	c.readback[n.Path] = start
	c.mu.Unlock()
	_, err := c.ask(ctx, osc.NewMessage("/node", osc.String(n.Path[1:])), "node "+n.Path, 1)
	if ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	delete(c.readback, n.Path)
	c.link.add(time.Since(start), err == nil)
	c.mu.Unlock()
}

// readbackOrigin tells how to apply a node line received for path: as a
// read-back (OriginReadback), as any change (OriginConsole), or not at all
// when this client wrote the node after asking it back (the answer is
// older than the write). Called with c.mu held.
func (c *Client) readbackOrigin(path string) (origin string, apply bool) {
	asked, ok := c.readback[path]
	if !ok {
		return OriginConsole, true
	}
	delete(c.readback, path)
	if c.wrote[path].After(asked) {
		return "", false
	}
	return OriginReadback, true
}

// caught counts the changes a read-back found.
func (c *Client) caught(ch []state.Change) {
	if len(ch) == 0 {
		return
	}
	line := ch[0].Leaf.Addr
	if t := c.mirror.TypeOf(ch[0].Leaf); t != nil {
		line += " " + t.Format(ch[0].Value)
	}
	c.mu.Lock()
	c.link.caught += len(ch)
	c.link.lastCaught, c.link.caughtAt = line, time.Now()
	c.mu.Unlock()
}

// touch notes that this client wrote to the node holding path (a leaf
// address, a node path or a node line's path): a read-back asked before
// must not undo it.
func (c *Client) touch(path string) {
	sch := c.mirror.Schema()
	if l := sch.Leaf(path); l != nil {
		path = l.Node.Path
	} else if n := sch.Resolve(path); n != nil {
		path = n.Path
	}
	c.mu.Lock()
	c.wrote[path] = time.Now()
	c.mu.Unlock()
}
