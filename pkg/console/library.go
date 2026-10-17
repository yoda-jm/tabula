package console

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/schema"
)

// The console's library of scenes, snippets and presets is managed with
// /save, /load, /copy, /delete and /rename (protocol document, "Manipulation
// of datasets"). The console answers each with the command, the element
// type and a status: "/load ,si scene 1", 0 when it failed. Listings live in
// /-show/showfile/… and /-libs/…; the console does not always push their
// changes, nor every value a recall changes, to the client that asked:
// Refresh reads them again.

// ErrRefused is the console answering a library command with status 0.
var ErrRefused = errors.New("the console refused")

// LibraryWait is how long Library waits for the console's answer.
var LibraryWait = 3 * time.Second

var libraryCommands = map[string]bool{"/save": true, "/load": true, "/copy": true, "/delete": true, "/rename": true}

// Library sends a library command, e.g. Library(ctx, "/load",
// osc.String("scene"), osc.Int(3)), and waits for the console's status. It
// changes the console, so writes must be armed. The command is sent once: a
// lost answer is reported as ErrTimeout, though the console may have done it.
func (c *Client) Library(ctx context.Context, cmd string, args ...osc.Arg) error {
	if !libraryCommands[cmd] {
		return fmt.Errorf("%s is not a library command", cmd)
	}
	if len(args) < 2 || args[0].Tag != 's' || args[1].Tag != 'i' {
		return fmt.Errorf("%s expects an element type and an index", cmd)
	}
	c.mu.Lock()
	armed := c.status.Armed
	c.mu.Unlock()
	if !armed {
		return ErrReadOnly
	}
	// One command at a time: answers only tell the command and the type.
	c.libMu.Lock()
	defer c.libMu.Unlock()

	ch := make(chan osc.Message, 1)
	c.mu.Lock()
	c.waiters[cmd] = append(c.waiters[cmd], ch)
	c.mu.Unlock()
	if err := c.send(osc.NewMessage(cmd, args...)); err != nil {
		c.dropWaiter(cmd, ch)
		return err
	}
	var reply osc.Message
	select {
	case reply = <-ch:
	case <-ctx.Done():
		c.dropWaiter(cmd, ch)
		return ctx.Err()
	case <-time.After(LibraryWait):
		c.dropWaiter(cmd, ch)
		return ErrTimeout
	}
	what := fmt.Sprintf("%s %s %d", cmd, args[0].Str, args[1].Int)
	if len(reply.Args) < 2 || reply.Args[1].Tag != 'i' {
		return fmt.Errorf("%s: unexpected answer %s", what, reply)
	}
	if reply.Args[1].Int != 1 {
		return fmt.Errorf("%w %s", ErrRefused, what)
	}
	c.log.Info("library", "command", what)
	return nil
}

// Refresh requests again the nodes match selects (regular nodes first,
// then effect parameters, whose fields depend on the effect types) and
// applies the answers to the mirror; changed values are reported like any
// other change from the console.
func (c *Client) Refresh(ctx context.Context, match func(*schema.Node) bool) error {
	var regular, variants []*schema.Node
	for _, n := range c.mirror.Schema().Nodes {
		switch {
		case !match(n):
		case n.Variant != nil:
			variants = append(variants, n)
		default:
			regular = append(regular, n)
		}
	}
	var mu sync.Mutex
	var failed []string
	each := func(path string, err error) {
		if err != nil {
			mu.Lock()
			failed = append(failed, path)
			mu.Unlock()
		}
	}
	if err := c.fetchNodes(ctx, regular, each); err != nil {
		return err
	}
	if err := c.fetchNodes(ctx, variants, each); err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("%w: %d nodes (%s…)", ErrTimeout, len(failed), failed[0])
	}
	return ctx.Err()
}
