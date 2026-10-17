package audio

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Track is a channel of the device written to a file.
type Track struct {
	Channel int    `json:"channel"` // 1-based, in the device's stream
	Name    string `json:"name,omitempty"`
	// File is relative to the directory holding the audio directory (the
	// session): "audio/01 Hang.wav".
	File string `json:"file,omitempty"`
}

// Info describes the audio of a session; it is kept in the session's
// meta.json.
type Info struct {
	Rate     int     `json:"rate"`
	Bits     int     `json:"bits"`
	Channels int     `json:"channels"` // of the device stream
	Tracks   []Track `json:"tracks"`
	// Offset is the time of the first sample, in seconds after the start
	// of the session: a change at session time t is at t − Offset in the
	// files (see AudioTime).
	Offset float64 `json:"offset"`
	// Start is the wall clock time of the first sample.
	Start time.Time `json:"start,omitzero"`
	// DriftPPM is how many microseconds longer a second of audio lasts on
	// the system clock (which times the changes); 0 when not measured.
	DriftPPM float64 `json:"drift_ppm,omitempty"`
	Frames   int64   `json:"frames"` // samples per track
	Device   string  `json:"device,omitempty"`
	Command  string  `json:"command,omitempty"`
	Overruns int     `json:"overruns,omitempty"` // reported by the recorder: samples were lost
	Error    string  `json:"error,omitempty"`
}

// AudioTime converts a session time (seconds) to a position in the files.
func (i Info) AudioTime(t float64) float64 {
	return (t - i.Offset) / (1 + i.DriftPPM*1e-6)
}

// Capture states.
const (
	Starting  = "starting"  // the recorder runs, no sample yet
	Recording = "recording" // samples arrive
	Stopped   = "stopped"   // stopped on request
	Failed    = "failed"    // stopped by an error (Error says which)
)

// Status is the live state of a capture.
type Status struct {
	Info
	State    string   `json:"state"`
	Warnings []string `json:"warnings,omitempty"`
	Format   string   `json:"format,omitempty"` // of the stream, e.g. "s32le, 32 channels, 48000 Hz"
	Seconds  float64  `json:"seconds"`          // of audio recorded
	Bytes    int64    `json:"bytes"`            // in the files
	ByteRate int64    `json:"byte_rate"`        // bytes per second, all files
	Free     int64    `json:"free"`             // bytes free on the disk, -1 if unknown
	// Remaining is how many seconds of audio still fit on the disk (before
	// the capture stops itself), -1 if unknown.
	Remaining float64 `json:"remaining"`
	// Peaks are the peak levels of the tracks over the last second, linear
	// (1 = full scale).
	Peaks []float64 `json:"peaks,omitempty"`
}

// Options are the parts of a capture that are not configuration.
type Options struct {
	// Ref is when the session started: offsets are measured from it.
	Ref time.Time
	// Names of the device channels (index 0: channel 1), "" when unknown.
	Names []string
	Log   *slog.Logger
	// Source, if set, is read instead of running the recorder (an
	// in-process source); closing it stops the capture.
	Source func() (io.ReadCloser, error)

	freeSpace func(string) (uint64, error)
	create    func(string) (file, error)
	refresh   time.Duration // of the file headers
	rf64At    int64
	reserve   int64 // free space left when the capture stops itself
}

const (
	refreshEvery = 2 * time.Second
	syncEvery    = 15 // refreshes between two fsyncs (30 s)
	lowDisk      = 20 * time.Minute
	noDataAfter  = 3 * time.Second
)

// Reserve is the free space a capture leaves on the disk: it stops itself
// when only this much is left (256 MiB, or a minute of audio if more), so
// the files are finished cleanly and the session goes on.
func Reserve(byteRate int64) int64 {
	return max(256<<20, 60*byteRate)
}

// Capture records the tracks of a running recorder.
type Capture struct {
	cfg     Config
	dir     string
	opt     Options
	log     *slog.Logger
	tracks  []Track
	chans   []int // device channel of each track
	files   []*wavWriter
	command string
	src     io.ReadCloser
	stopFn  func() // asks the recorder to stop
	killFn  func()
	wait    func() error
	done    chan struct{}

	mu        sync.Mutex
	state     string
	err       error
	format    sampleFormat
	formatStr string
	frames    int64 // frames received and written (some still buffered)
	clock     *clock
	peaks     []int32
	lastPeaks []float64
	lastData  time.Time
	started   time.Time
	free      int64
	stopping  bool
	finished  bool
	overruns  int
	lostMS    float64
	stderr    []string // last lines
	warnDisk  string
}

// Start runs the recorder and writes the selected tracks into dir, one WAV
// file each (dir is created; no files if dir is empty: a test of the
// input, which measures the levels).
func Start(cfg Config, dir string, opt Options) (*Capture, error) {
	cfg = cfg.Normalized()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	chans, _ := ParseTracks(cfg.Tracks, cfg.Channels)
	if opt.Log == nil {
		opt.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opt.Ref.IsZero() {
		opt.Ref = time.Now()
	}
	if opt.freeSpace == nil {
		opt.freeSpace = FreeSpace
	}
	if opt.create == nil {
		opt.create = createFile
	}
	if opt.refresh == 0 {
		opt.refresh = refreshEvery
	}
	if opt.reserve == 0 {
		opt.reserve = Reserve(cfg.ByteRate())
	}
	c := &Capture{cfg: cfg, dir: dir, opt: opt, log: opt.Log, chans: chans, done: make(chan struct{}),
		state: Starting, clock: newClock(cfg.Rate, opt.Ref), peaks: make([]int32, len(chans)), free: -1}

	for _, ch := range chans {
		name := ""
		if ch <= len(opt.Names) && !cfg.PlainNames {
			name = opt.Names[ch-1]
		}
		c.tracks = append(c.tracks, Track{Channel: ch, Name: name})
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		if free, err := opt.freeSpace(dir); err == nil {
			c.free = int64(free)
			if c.free <= opt.reserve {
				return nil, fmt.Errorf("disk full: %s free in %s", FormatBytes(c.free), dir)
			}
		}
		for i, t := range c.tracks {
			base := FileName(t.Channel, t.Name)
			w, err := createWAV(opt.create, filepath.Join(dir, base), cfg.Rate, cfg.Bits, opt.rf64At)
			if err != nil {
				c.closeFiles()
				return nil, err
			}
			c.files = append(c.files, w)
			c.tracks[i].File = path.Join(filepath.Base(dir), base)
		}
	}

	var err error
	if opt.Source != nil {
		c.command = "(in-process source)"
		if c.src, err = opt.Source(); err != nil {
			c.closeFiles()
			return nil, err
		}
		c.stopFn = func() { c.src.Close() }
		c.killFn = c.stopFn
		c.wait = func() error { return nil }
	} else if err = c.startCommand(); err != nil {
		c.closeFiles()
		return nil, err
	}
	c.started = time.Now()
	go c.run()
	return c, nil
}

func (c *Capture) startCommand() error {
	args, err := c.cfg.CommandLine()
	if err != nil {
		return err
	}
	c.command = QuoteArgs(args)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = procAttr() // its own process group: Ctrl-C in a terminal is ours to handle
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	c.log.Info("audio recorder", "command", c.command, "pid", cmd.Process.Pid)
	c.src = stdout
	c.stopFn = func() { terminate(cmd.Process) }
	c.killFn = func() { kill(cmd.Process) }
	stderrDone := make(chan struct{})
	c.wait = func() error {
		select { // Wait closes the pipe: read what the recorder said first
		case <-stderrDone:
		case <-time.After(2 * time.Second):
		}
		return cmd.Wait()
	}
	go func() {
		c.readStderr(stderr)
		close(stderrDone)
	}()
	return nil
}

// FileName is the file of a track: "01 Hang.wav", or "Track 01.wav" when
// the name is unknown.
func FileName(channel int, name string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r < ' ', strings.ContainsRune(`/\:*?"<>|`, r):
			return '_'
		}
		return r
	}, name)
	clean = strings.Trim(clean, " .")
	if !hasLetterOrDigit(clean) {
		return fmt.Sprintf("Track %02d.wav", channel)
	}
	return fmt.Sprintf("%02d %s.wav", channel, clean)
}

func hasLetterOrDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127 {
			return true
		}
	}
	return false
}

var overrunRe = regexp.MustCompile(`(?i)(overrun|xrun)[^0-9]*([0-9.]+)?\s*ms`)

func (c *Capture) readStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		c.mu.Lock()
		c.stderr = append(c.stderr, line)
		if len(c.stderr) > 5 {
			c.stderr = c.stderr[1:]
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "overrun") || strings.Contains(lower, "xrun") {
			c.overruns++
			if m := overrunRe.FindStringSubmatch(line); m != nil && m[2] != "" {
				ms, _ := strconv.ParseFloat(m[2], 64)
				c.lostMS += ms
			}
		}
		c.mu.Unlock()
		c.log.Info("audio recorder", "says", line)
	}
}

// run reads the stream, writes the files, and finishes them at the end of
// the stream.
func (c *Capture) run() {
	defer close(c.done)
	// The reader never waits for the disk: a slow write is absorbed by the
	// queue (seconds of audio) rather than by the recorder's buffer.
	chunks := make(chan []byte, 2048)
	go c.read(chunks, 64<<10)
	c.write(chunks)
	exit := c.wait()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.finishFilesLocked()
	switch {
	case c.err != nil:
		c.state = Failed
	case c.stopping:
		c.state = Stopped
	default: // the recorder ended by itself
		msg := "the recorder stopped"
		if exit != nil {
			msg += " (" + exit.Error() + ")"
		}
		if len(c.stderr) > 0 {
			msg += ": " + strings.Join(c.stderr, " / ")
		}
		c.err, c.state = errors.New(msg), Failed
	}
	c.log.Info("audio stopped", "state", c.state, "frames", c.frames, "err", c.err)
}

// read detects the stream format, then passes the stream on in chunks,
// noting when they arrive.
func (c *Capture) read(chunks chan<- []byte, size int) {
	defer close(chunks)
	br := bufio.NewReaderSize(c.src, size)
	format, desc, err := c.detect(br)
	c.mu.Lock()
	c.format, c.formatStr = format, desc
	if err != nil && c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
	if err != nil {
		c.stopFn()
		io.Copy(io.Discard, br)
		return
	}
	if format.size == 0 {
		return // nothing at all: the recorder failed (see run)
	}
	frameBytes := int64(format.size * c.cfg.Channels)
	var total int64
	buf := make([]byte, size)
	for {
		n, err := br.Read(buf)
		if n > 0 {
			now := time.Now()
			total += int64(n)
			c.mu.Lock()
			c.clock.observe(now, total/frameBytes)
			c.lastData = now
			if c.state == Starting {
				c.state = Recording
			}
			c.mu.Unlock()
			chunks <- append([]byte(nil), buf[:n]...)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !c.isStopping() {
				c.log.Warn("audio stream", "err", err)
			}
			return
		}
	}
}

// detect reads the header of a WAV stream and checks it against the
// configuration; a raw stream is in the configured format.
func (c *Capture) detect(br *bufio.Reader) (sampleFormat, string, error) {
	head, err := br.Peek(12)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return sampleFormat{}, "", nil // (almost) nothing: the recorder failed (see run)
	}
	if err != nil {
		return sampleFormat{}, "", fmt.Errorf("audio stream: %w", err)
	}
	if string(head[8:12]) == "WAVE" && (string(head[:4]) == "RIFF" || string(head[:4]) == "RF64") {
		info, err := readWAVHeader(br)
		if err != nil {
			return sampleFormat{}, "", err
		}
		f, err := info.sampleFormat()
		if err != nil {
			return f, "", err
		}
		desc := fmt.Sprintf("WAV %s, %d channels, %d Hz", f.name, info.Channels, info.Rate)
		switch {
		case info.Channels != c.cfg.Channels:
			return f, desc, fmt.Errorf("the recorder sends %d channels, %d expected (%s)", info.Channels, c.cfg.Channels, desc)
		case info.Rate != c.cfg.Rate:
			return f, desc, fmt.Errorf("the recorder sends %d Hz, %d expected (%s)", info.Rate, c.cfg.Rate, desc)
		}
		return f, desc, nil
	}
	name := "s32le"
	if c.cfg.Recorder == "custom" && c.cfg.Input != "" {
		name = c.cfg.Input
	}
	f, err := parseFormat(name)
	return f, fmt.Sprintf("raw %s, %d channels, %d Hz", f.name, c.cfg.Channels, c.cfg.Rate), err
}

func (c *Capture) isStopping() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopping
}

func (c *Capture) isFinished() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.finished
}

// write takes the chunks off the queue until it is closed.
func (c *Capture) write(chunks <-chan []byte) {
	tick := time.NewTicker(min(time.Second, c.opt.refresh))
	defer tick.Stop()
	var rest []byte // a frame cut between two chunks
	lastRefresh, refreshes := time.Now(), 0
	for {
		select {
		case b, ok := <-chunks:
			if !ok {
				return
			}
			if len(rest) > 0 {
				b = append(rest, b...)
			}
			rest = c.consume(b)
		case now := <-tick.C:
			if c.isFinished() {
				continue
			}
			c.second(now)
			if now.Sub(lastRefresh) >= c.opt.refresh {
				lastRefresh = now
				refreshes++
				c.refresh(refreshes%syncEvery == 0)
			}
		}
	}
}

// consume writes the whole frames of b and returns the bytes left over.
func (c *Capture) consume(b []byte) []byte {
	c.mu.Lock()
	failed := c.err != nil
	f := c.format
	c.mu.Unlock()
	if failed || f.size == 0 {
		return nil // stopping after an error: the rest of the stream is dropped
	}
	frame := f.size * c.cfg.Channels
	n := len(b) / frame
	out := c.cfg.Bits / 8
	var peaks [128]int32
	for i, w := range c.files {
		off := (c.chans[i] - 1) * f.size
		start := len(w.buf)
		w.buf = append(w.buf, make([]byte, n*out)...)
		dst := w.buf[start:]
		peak := int32(0)
		for k := 0; k < n; k++ {
			s := f.sample(b[k*frame+off:])
			putSample(dst[k*out:], s, out)
			if s < 0 {
				s = ^s
			}
			peak = max(peak, s)
		}
		peaks[i] = peak
	}
	if len(c.files) == 0 { // a test of the input: levels only
		for i, ch := range c.chans {
			off := (ch - 1) * f.size
			for k := 0; k < n; k++ {
				s := f.sample(b[k*frame+off:])
				if s < 0 {
					s = ^s
				}
				peaks[i] = max(peaks[i], s)
			}
		}
	}
	var werr error
	for _, w := range c.files {
		if w.full() {
			if err := w.flush(); err != nil && werr == nil {
				werr = err
			}
		}
	}
	c.mu.Lock()
	c.frames += int64(n)
	for i := range c.peaks {
		c.peaks[i] = max(c.peaks[i], peaks[i])
	}
	c.mu.Unlock()
	if werr != nil {
		c.fail(werr)
	}
	return append([]byte(nil), b[n*frame:]...)
}

// second runs every second: levels, disk space, watchdog.
func (c *Capture) second(now time.Time) {
	c.mu.Lock()
	c.lastPeaks = make([]float64, len(c.peaks))
	for i, p := range c.peaks {
		c.lastPeaks[i] = float64(p) / (1 << 31)
		c.peaks[i] = 0
	}
	c.mu.Unlock()
	if c.dir == "" {
		return
	}
	free, err := c.opt.freeSpace(c.dir)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.free = int64(free)
	left := c.remainingLocked()
	c.warnDisk = ""
	if left >= 0 && left < lowDisk.Seconds() {
		c.warnDisk = fmt.Sprintf("disk almost full: %s of audio left", formatSeconds(left))
	}
	full := c.free <= c.opt.reserve
	c.mu.Unlock()
	if full {
		c.fail(fmt.Errorf("disk full (%s left): audio stopped, the session goes on", FormatBytes(int64(free))))
	}
}

func (c *Capture) remainingLocked() float64 {
	rate := c.cfg.ByteRate()
	if c.free < 0 || rate == 0 || c.dir == "" {
		return -1
	}
	return math.Max(0, float64(c.free-c.opt.reserve)/float64(rate))
}

// refresh writes the buffered samples and the header sizes of every file.
func (c *Capture) refresh(sync bool) {
	var err error
	for _, w := range c.files {
		if e := w.refresh(); e != nil && err == nil {
			err = e
		}
		if sync && err == nil {
			w.f.Sync()
		}
	}
	if err != nil {
		c.fail(err)
	}
}

// fail stops the capture after an error, keeping the files consistent.
func (c *Capture) fail(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	c.finishFilesLocked()
	c.mu.Unlock()
	c.log.Error("audio", "err", err)
	c.stopFn()
}

// finishFilesLocked ends every file at the same sample: the last one all of
// them hold.
func (c *Capture) finishFilesLocked() {
	if c.finished || len(c.files) == 0 {
		c.finished = true
		return
	}
	c.finished = true
	frames := int64(math.MaxInt64)
	for _, w := range c.files {
		w.flush()
		frames = min(frames, w.frames())
	}
	for _, w := range c.files {
		if err := w.finish(frames); err != nil && c.err == nil {
			c.err = err
		}
	}
	c.frames = frames
}

func (c *Capture) closeFiles() {
	for _, w := range c.files {
		w.f.Close()
		os.Remove(w.path)
	}
}

// Stop stops the recorder and finishes the files.
func (c *Capture) Stop() Status {
	c.mu.Lock()
	c.stopping = true
	c.mu.Unlock()
	c.stopFn()
	select {
	case <-c.done:
	case <-time.After(3 * time.Second):
		c.log.Warn("audio recorder does not stop: killing it")
		c.killFn()
		select {
		case <-c.done:
		case <-time.After(2 * time.Second): // a child of the recorder holds the pipe
			c.src.Close()
			<-c.done
		}
	}
	return c.Status()
}

// Done is closed when the capture is over (stopped or failed).
func (c *Capture) Done() <-chan struct{} { return c.done }

// Info describes what is recorded so far.
func (c *Capture) Info() Info {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.infoLocked()
}

func (c *Capture) infoLocked() Info {
	i := Info{Rate: c.cfg.Rate, Bits: c.cfg.Bits, Channels: c.cfg.Channels, Tracks: c.tracks,
		Frames: c.frames, Device: c.cfg.Device, Command: c.command, Overruns: c.overruns}
	if off, ok := c.clock.offset(); ok {
		i.Offset = math.Round(off*1e6) / 1e6
		i.Start = c.opt.Ref.Add(time.Duration(off * float64(time.Second)))
	}
	if d, ok := c.clock.drift(); ok && c.overruns == 0 {
		i.DriftPPM = math.Round(d*1e9) / 1e3
	}
	if c.err != nil {
		i.Error = c.err.Error()
	}
	return i
}

// Status is the live state of the capture.
func (c *Capture) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Status{Info: c.infoLocked(), State: c.state, Format: c.formatStr, ByteRate: c.cfg.ByteRate(),
		Free: c.free, Remaining: c.remainingLocked(), Peaks: c.lastPeaks}
	if c.err != nil && c.state != Stopped {
		s.State = Failed
	}
	s.Seconds = float64(c.frames) / float64(c.cfg.Rate)
	s.Bytes = c.frames * int64(c.cfg.Bits/8) * int64(len(c.files))
	if c.warnDisk != "" {
		s.Warnings = append(s.Warnings, c.warnDisk)
	}
	if c.overruns > 0 {
		w := fmt.Sprintf("the recorder lost samples: %d overrun(s)", c.overruns)
		if c.lostMS > 0 {
			w += fmt.Sprintf(", %.0f ms at least", c.lostMS)
		}
		s.Warnings = append(s.Warnings, w+"; from then on the tracks lag behind the session")
	}
	if !c.finished && c.err == nil {
		switch {
		case c.state == Starting && time.Since(c.started) > 10*time.Second:
			s.Warnings = append(s.Warnings, "no audio from the recorder yet")
		case c.state == Recording && time.Since(c.lastData) > noDataAfter:
			s.Warnings = append(s.Warnings, fmt.Sprintf("no audio for %s", time.Since(c.lastData).Round(time.Second)))
		}
	}
	return s
}

// FormatBytes writes a size for people: "12.5 GB".
func FormatBytes(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.0f MB", float64(n)/1e6)
	}
	return fmt.Sprintf("%d kB", n/1000)
}

func formatSeconds(s float64) string {
	d := time.Duration(s) * time.Second
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02d", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%d min", int(d.Minutes()))
}
