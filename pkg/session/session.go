// Package session records a show: a snapshot of the console when recording
// starts, then every change with its time, plus markers. A session is a
// directory:
//
//	meta.json     what was recorded, from which console, when
//	start.scn     the scene part of the snapshot (opens in M32-Edit)
//	start.nodes   the other nodes of the snapshot (preferences, status…)
//	events.jsonl  one JSON object per change or marker, in time order
//	audio/        the audio tracks, when recorded with the session (pkg/audio)
//
// Sessions replay into a state.Store (seek, inspection) or, through a
// Target, onto a console or the emulator.
package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/state"
)

// Meta describes a session.
type Meta struct {
	Format   int       `json:"format"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end,omitempty"`
	Console  string    `json:"console,omitempty"` // model and name, e.g. "M32C M32C-0A-EB-1A"
	Note     string    `json:"note,omitempty"`
	Events   int       `json:"events"`
	Markers  int       `json:"markers"`
	Duration float64   `json:"duration"` // seconds
	// Audio describes the audio tracks recorded with the session.
	Audio *audio.Info `json:"audio,omitempty"`
	// USBBackup is the console's own recording on its USB stick, started
	// with the session.
	USBBackup *USBBackup `json:"usb_backup,omitempty"`
}

// USBBackup is where the console recorded the session on its USB stick, or
// why it did not.
type USBBackup struct {
	File    string `json:"file,omitempty"`    // on the stick, e.g. /R_20261026-203512.wav
	Already bool   `json:"already,omitempty"` // the console was recording already: it was left on
	Error   string `json:"error,omitempty"`
}

// Event is a change or a marker, at T seconds after the start.
type Event struct {
	T      float64 `json:"t"`
	Addr   string  `json:"addr,omitempty"`
	Text   string  `json:"text,omitempty"`  // node text of the value
	Value  any     `json:"value,omitempty"` // normalized float, int or string
	Origin string  `json:"origin,omitempty"`
	Marker string  `json:"marker,omitempty"`
}

// Recorder writes a session while a store changes. The changes and
// markers go through a queue to a goroutine writing events.jsonl: a disk
// that hangs never holds up the store's other subscribers (the live view).
type Recorder struct {
	dir   string
	store *state.Store
	start time.Time

	// owned by the writing goroutine
	events  io.WriteCloser
	w       *bufio.Writer
	queue   chan []Event
	written chan struct{} // closed once the queue is written out

	mu      sync.Mutex
	meta    Meta
	cancel  func()
	closed  bool
	werr    error     // the first error writing events.jsonl
	waiting int       // events queued, not written yet
	since   time.Time // when the oldest of them was queued
	dropped int       // events lost: the queue was full (the disk hung)

	metaMu sync.Mutex // one meta.json write at a time, outside mu
}

// createEvents creates events.jsonl (tests: a disk that hangs).
var createEvents = func(path string) (io.WriteCloser, error) { return os.Create(path) }

// queueBatches is how many batches of changes wait for the disk before
// the next ones are dropped: minutes of a busy show.
var queueBatches = 8192

// Start snapshots the store into dir (created, must not hold a session)
// and records its changes until Stop.
func Start(dir string, st *state.Store, consoleName, note string) (*Recorder, error) {
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err == nil {
		return nil, fmt.Errorf("%s already holds a session", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &Recorder{dir: dir, store: st, queue: make(chan []Event, queueBatches), written: make(chan struct{})}
	f, err := createEvents(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		return nil, err
	}
	r.events, r.w = f, bufio.NewWriter(f)
	go r.write()

	// Subscribe before the snapshot: a change made meanwhile is in both, and
	// replaying it after the snapshot gives the right state either way.
	r.start = time.Now()
	r.meta = Meta{Format: 1, Start: r.start, Console: consoleName, Note: note}
	r.cancel = st.Subscribe(r.record)
	if err := r.snapshot(); err != nil {
		r.Stop()
		return nil, err
	}
	r.mu.Lock()
	m := r.meta // the changes recorded meanwhile count in it
	r.mu.Unlock()
	if err := writeMeta(r.dir, m); err != nil {
		r.Stop()
		return nil, err
	}
	return r, nil
}

func (r *Recorder) snapshot() error {
	sc, err := os.Create(filepath.Join(r.dir, "start.scn"))
	if err != nil {
		return err
	}
	header := scene.Header("tabula "+r.start.Format("2006-01-02 15:04"), r.meta.Note)
	if err := scene.Write(sc, r.store, header); err != nil {
		sc.Close()
		return err
	}
	if err := sc.Close(); err != nil {
		return err
	}
	var b strings.Builder
	for _, n := range r.store.Schema().Nodes {
		if !n.InScene && !n.Lazy {
			b.WriteString(r.store.NodeLine(n))
			b.WriteByte('\n')
		}
	}
	return os.WriteFile(filepath.Join(r.dir, "start.nodes"), []byte(b.String()), 0o644)
}

func (r *Recorder) record(changes []state.Change) {
	t := time.Since(r.start).Seconds()
	batch := make([]Event, 0, len(changes))
	for _, c := range changes {
		e := Event{T: t, Addr: c.Leaf.Addr, Origin: c.Origin}
		if typ := r.store.TypeOf(c.Leaf); typ != nil {
			e.Text = typ.Format(c.Value)
		}
		switch c.Value.Tag {
		case 'i':
			e.Value = c.Value.Int
		case 'f':
			e.Value = c.Value.Float
		case 's':
			e.Value = c.Value.Str
		}
		batch = append(batch, e)
	}
	r.enqueue(batch)
}

// enqueue hands events to the writing goroutine, without ever waiting:
// when the queue is full (the disk has not taken anything for minutes),
// they are counted as lost.
func (r *Recorder) enqueue(batch []Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || len(batch) == 0 {
		return
	}
	select {
	case r.queue <- batch:
		if r.waiting == 0 {
			r.since = time.Now()
		}
		r.waiting += len(batch)
	default:
		r.dropped += len(batch)
	}
}

// write writes the queued events to events.jsonl, flushing whenever it
// has caught up: the file stays usable if the program dies mid-show.
func (r *Recorder) write() {
	defer close(r.written)
	var lost error
	for batch := range r.queue {
		var events, markers int
		for _, e := range batch {
			line, _ := json.Marshal(e)
			r.w.Write(line)
			r.w.WriteByte('\n')
			if e.Marker != "" {
				markers++
			} else {
				events++
			}
		}
		if len(r.queue) == 0 && lost == nil {
			lost = r.w.Flush()
		}
		r.mu.Lock()
		r.meta.Events += events
		r.meta.Markers += markers
		r.waiting -= len(batch)
		if r.waiting > 0 {
			r.since = time.Now() // the oldest left is younger than this
		}
		if lost != nil && r.werr == nil {
			r.werr = lost
		}
		r.mu.Unlock()
	}
	if err := r.w.Flush(); err != nil {
		r.mu.Lock()
		if r.werr == nil {
			r.werr = err
		}
		r.mu.Unlock()
	}
}

// Err returns the first error writing the changes (a full disk…): the
// changes after it are lost. Changes dropped because the disk did not
// take them in time count too.
func (r *Recorder) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.werr == nil && r.dropped > 0 {
		return fmt.Errorf("the disk did not take them in time: %d not written", r.dropped)
	}
	return r.werr
}

// Backlog is how many changes wait to be written, and since when: a disk
// that hangs (rather than fails) shows here first.
func (r *Recorder) Backlog() (waiting int, since time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.waiting, r.since
}

// Mark adds a marker (song start, incident…) at the current time.
func (r *Recorder) Mark(label string) {
	r.enqueue([]Event{{T: time.Since(r.start).Seconds(), Marker: label}})
}

// Started is when the recording started: session times count from it.
func (r *Recorder) Started() time.Time { return r.start }

// SetAudio records what the audio capture running with the session wrote
// so far (nil: no audio) in meta.json.
func (r *Recorder) SetAudio(a *audio.Info) error {
	r.mu.Lock()
	r.meta.Audio = a
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	m := r.meta
	m.Duration = time.Since(r.start).Seconds()
	r.mu.Unlock()
	return r.writeMeta(m)
}

// writeMeta writes meta.json without holding mu: a slow disk does not
// hold up who reads the recording's state.
func (r *Recorder) writeMeta(m Meta) error {
	r.metaMu.Lock()
	defer r.metaMu.Unlock()
	return writeMeta(r.dir, m)
}

// SetUSBBackup records the console's own recording of the session in
// meta.json.
func (r *Recorder) SetUSBBackup(b *USBBackup) error {
	r.mu.Lock()
	r.meta.USBBackup = b
	m := r.meta
	if !r.closed {
		m.Duration = time.Since(r.start).Seconds()
	}
	r.mu.Unlock()
	return r.writeMeta(m)
}

// Elapsed is the time since the recording started.
func (r *Recorder) Elapsed() time.Duration { return time.Since(r.start) }

// Meta returns the session description so far.
func (r *Recorder) Meta() Meta {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.meta
	m.Duration = time.Since(r.start).Seconds()
	return m
}

func (r *Recorder) Dir() string { return r.dir }

// Stop ends the recording, once the changes queued are written.
func (r *Recorder) Stop() error {
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.meta.End = time.Now()
	r.meta.Duration = r.meta.End.Sub(r.start).Seconds()
	close(r.queue)
	r.mu.Unlock()
	<-r.written
	r.mu.Lock()
	err := errors.Join(r.werr, r.events.Close())
	m := r.meta
	r.mu.Unlock()
	return errors.Join(err, r.writeMeta(m))
}

// writeMeta replaces meta.json at once: it is rewritten while recording.
func writeMeta(dir string, m Meta) error {
	data, _ := json.MarshalIndent(m, "", "  ")
	tmp := filepath.Join(dir, "meta.json.tmp")
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "meta.json"))
}

// Session is a recorded session, loaded in memory.
type Session struct {
	Dir    string
	Meta   Meta
	Start  []string // snapshot node lines (scene, then other nodes)
	Events []Event  // in time order
	// Damaged lists the lines of events.jsonl that could not be read, so
	// their events are missing (the last line of a recording cut short).
	Damaged []int
}

// Open loads a session directory.
func Open(dir string) (*Session, error) {
	s := &Session{Dir: dir}
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.Meta); err != nil {
		return nil, fmt.Errorf("meta.json: %w", err)
	}
	for _, name := range []string{"start.scn", "start.nodes"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "/") {
				s.Start = append(s.Start, line)
			}
		}
	}
	f, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for n := 1; sc.Scan(); n++ {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			if n == 1 {
				return nil, fmt.Errorf("events.jsonl line %d: %w", n, err)
			}
			// A recording cut short may end with a partial line; a damaged
			// line elsewhere loses that event only, not the ones after it.
			s.Damaged = append(s.Damaged, n)
			continue
		}
		s.Events = append(s.Events, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("events.jsonl: %w", err)
	}
	sort.SliceStable(s.Events, func(i, j int) bool { return s.Events[i].T < s.Events[j].T })
	if s.Meta.Duration == 0 && len(s.Events) > 0 {
		s.Meta.Duration = s.Events[len(s.Events)-1].T
	}
	return s, nil
}

// Markers returns the session's markers.
func (s *Session) Markers() []Event {
	var out []Event
	for _, e := range s.Events {
		if e.Marker != "" {
			out = append(out, e)
		}
	}
	return out
}

// StateAt fills st with the console state t seconds into the session: the
// snapshot, then every change up to t.
func (s *Session) StateAt(st *state.Store, t float64) []error {
	lines := append([]string(nil), s.Start...)
	for _, e := range s.Events {
		if e.T > t {
			break
		}
		if e.Addr != "" {
			lines = append(lines, e.Addr+" "+e.Text)
		}
	}
	return st.ApplyLines(lines, "replay")
}

// List returns the sessions found in the subdirectories of root, newest first.
func List(root string) ([]Meta, []string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	type item struct {
		m   Meta
		dir string
	}
	var items []item
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
		if err != nil {
			continue
		}
		var m Meta
		if json.Unmarshal(data, &m) == nil {
			items = append(items, item{m, dir})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].m.Start.After(items[j].m.Start) })
	metas, dirs := make([]Meta, len(items)), make([]string, len(items))
	for i, it := range items {
		metas[i], dirs[i] = it.m, it.dir
	}
	return metas, dirs, nil
}
