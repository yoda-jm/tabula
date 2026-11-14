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
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Playing a session's tracks back to the card, for a virtual soundcheck:
// each track is read from its WAV file and put back on the card channel it
// was recorded from (silence on the others), interleaved into one stream of
// raw 32-bit samples piped to a player program (aplay, pacat, ffmpeg) that
// plays it on the card. Nothing is written to the disk; the player's
// buffer paces the reading.

// PlayTemplate is the player matching each recorder ({device}, {channels}
// and {rate} replaced as for the recorders): aplay for arecord, pacat for
// parecord, ffmpeg for ffmpeg (ALSA on Linux; AudioToolbox on macOS, a
// device index, untried). "" when there is none (ffmpeg on Windows: use a
// custom player command).
func PlayTemplate(recorder, goos string) string {
	switch recorder {
	case "arecord":
		return "aplay -D {device} -c {channels} -r {rate} -f S32_LE -t raw --buffer-time=200000 --period-time=10000 -"
	case "parecord":
		return "pacat --playback --raw --format=s32le --rate={rate} --channels={channels} --no-remix --no-remap --latency-msec=50 -d {device}"
	case "ffmpeg":
		in := "ffmpeg -hide_banner -nostdin -nostats -loglevel warning -f s32le -ar {rate} -ac {channels} -i - "
		switch goos {
		case "linux":
			return in + "-f alsa {device}"
		case "darwin":
			return in + "-f audiotoolbox -audio_device_index {device} -"
		}
	}
	return ""
}

// PlayDevice is where the tracks are played: PlayDevice when set, else the
// device recorded from (an ALSA device plays and records); for PulseAudio
// the output of the card whose input is recorded.
func (c Config) PlayDevice() string {
	return c.playDevice(runtime.GOOS)
}

func (c Config) playDevice(goos string) string {
	if c.PlayTo != "" {
		return c.PlayTo
	}
	dev := c.Device
	if dev == "" {
		dev = defaultDevice(c.Recorder, goos)
	}
	if c.Recorder == "parecord" {
		switch {
		case dev == "@DEFAULT_SOURCE@":
			return "@DEFAULT_SINK@"
		case strings.HasPrefix(dev, "alsa_input."):
			dev = "alsa_output." + strings.TrimPrefix(dev, "alsa_input.")
			return strings.Replace(dev, "-input", "-output", 1)
		}
	}
	return dev
}

// PlayCommandLine returns the player's arguments, for channels at rate (the
// session's stream): PlayCommand when set, else the player of the recorder.
func (c Config) PlayCommandLine(channels, rate int) ([]string, error) {
	return c.playCommandLine(runtime.GOOS, channels, rate)
}

func (c Config) playCommandLine(goos string, channels, rate int) ([]string, error) {
	tmpl := c.PlayCommand
	if tmpl == "" {
		tmpl = PlayTemplate(c.Recorder, goos)
	}
	if tmpl == "" {
		return nil, fmt.Errorf("no player for %s here: set a command to play the tracks", c.Recorder)
	}
	dev := c.playDevice(goos)
	if dev == "" && strings.Contains(tmpl, "{device}") {
		return nil, errors.New("choose the device to play the tracks to")
	}
	args, err := splitArgs(tmpl)
	if err != nil {
		return nil, err
	}
	r := strings.NewReplacer("{device}", dev, "{channels}", strconv.Itoa(channels), "{rate}", strconv.Itoa(rate))
	for i, a := range args {
		args[i] = r.Replace(a)
	}
	return args, nil
}

// PlayOptions are what a playback needs besides the configuration.
type PlayOptions struct {
	// From is where to start in the files, in seconds; before 0 (a session
	// time before the audio started), silence plays first.
	From float64
	Log  *slog.Logger
	// Sink, if set, receives the stream instead of the player program.
	Sink func() (io.WriteCloser, error)
	// Mix, if set, plays these tracks mixed into two channels (left,
	// right) instead of each track on its own channel: listening to a
	// session on the laptop. The player is asked for 2 channels. Every
	// recorded track is read, so that SetMix changes what is heard in the
	// stream, without a gap; changes fade over a block (about 20 ms), the
	// start fades in, and Stop fades out: no click.
	Mix []MixTrack
}

// MixTrack is a track in a stereo mix: Channel its track number (1-based),
// Gain linear, Pan from -1 (left) to 1 (right).
type MixTrack struct {
	Channel int     `json:"track"`
	Gain    float64 `json:"gain"`
	Pan     float64 `json:"pan"`
}

// gains are the left and right gains of a track in a mix: centre plays at
// full level in both, a hard pan in one only.
func (m MixTrack) gains() (l, r float64) {
	pan := min(max(m.Pan, -1), 1)
	return m.Gain * min(1, 1-pan), m.Gain * min(1, 1+pan)
}

// Playback plays tracks to the card.
type Playback struct {
	info    Info
	opt     PlayOptions
	command string
	tracks  []playTrack
	sink    io.WriteCloser
	stopFn  func()
	wait    func() error
	done    chan struct{}

	mu       sync.Mutex
	frames   int64 // frames sent to the player
	err      error
	stopping bool
	fading   bool // a mix fades out, then its stream ends
	stderr   []string
}

type playTrack struct {
	channel int     // 0-based in the stream
	gl, gr  float64 // its gains in a mix, as heard now
	tl, tr  float64 // the gains it fades to (SetMix), under Playback.mu
	f       *os.File
	r       *bufio.Reader
	format  sampleFormat
	left    int64 // frames still to read
}

// playFrames is how many frames go to the player at once (about 20 ms).
const playFrames = 1024

// StartPlayback plays the tracks described by info, whose files are in dir
// (the session's directory), from opt.From. The stream has info's channels
// and rate, as recorded.
func StartPlayback(cfg Config, dir string, info Info, opt PlayOptions) (*Playback, error) {
	cfg = cfg.Normalized()
	if info.Rate <= 0 || info.Channels <= 0 || len(info.Tracks) == 0 {
		return nil, errors.New("this session has no audio tracks")
	}
	if opt.Log == nil {
		opt.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	p := &Playback{info: info, opt: opt, done: make(chan struct{})}
	skip := int64(0)
	if opt.From > 0 {
		skip = int64(opt.From * float64(info.Rate))
	}
	mixed := map[int]MixTrack{}
	for _, m := range opt.Mix {
		mixed[m.Channel] = m
	}
	for _, t := range info.Tracks {
		if t.File == "" || t.Channel < 1 || t.Channel > info.Channels {
			continue
		}
		tr, err := openTrack(filepath.Join(dir, filepath.FromSlash(t.File)), info.Rate, skip)
		if err != nil {
			p.closeTracks()
			return nil, err
		}
		tr.channel = t.Channel - 1
		tr.tl, tr.tr = mixed[t.Channel].gains() // from silence: the start fades in
		delete(mixed, t.Channel)
		p.tracks = append(p.tracks, tr)
	}
	if opt.Mix != nil && len(mixed) > 0 {
		p.closeTracks()
		for ch := range mixed {
			return nil, fmt.Errorf("track %d was not recorded", ch)
		}
	}
	if len(p.tracks) == 0 {
		return nil, errors.New("this session has no audio tracks")
	}
	if opt.Sink != nil {
		sink, err := opt.Sink()
		if err != nil {
			p.closeTracks()
			return nil, err
		}
		p.command, p.sink = "(in-process sink)", sink
		p.stopFn, p.wait = func() { sink.Close() }, func() error { return nil }
	} else if err := p.startPlayer(cfg); err != nil {
		p.closeTracks()
		return nil, err
	}
	go p.run()
	return p, nil
}

// openTrack opens a mono track and moves to frame skip.
func openTrack(path string, rate int, skip int64) (playTrack, error) {
	info, err := ReadWAV(path)
	if err != nil {
		return playTrack{}, err
	}
	format, err := info.sampleFormat()
	switch {
	case err != nil:
		return playTrack{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	case info.Channels != 1:
		return playTrack{}, fmt.Errorf("%s: %d channels, expected a mono track", filepath.Base(path), info.Channels)
	case info.Rate != rate:
		return playTrack{}, fmt.Errorf("%s: %d Hz, the session is at %d Hz", filepath.Base(path), info.Rate, rate)
	}
	f, err := os.Open(path)
	if err != nil {
		return playTrack{}, err
	}
	skip = min(skip, info.Frames)
	if _, err := f.Seek(info.DataOffset+skip*int64(format.size), io.SeekStart); err != nil {
		f.Close()
		return playTrack{}, err
	}
	return playTrack{f: f, r: bufio.NewReaderSize(f, 64<<10), format: format, left: info.Frames - skip}, nil
}

func (p *Playback) startPlayer(cfg Config) error {
	args, err := cfg.PlayCommandLine(p.channels(), p.info.Rate)
	if err != nil {
		return err
	}
	p.command = QuoteArgs(args)
	cmd := exec.Command(program(args[0]), args[1:]...)
	cmd.SysProcAttr = procAttr()
	stdin, err := cmd.StdinPipe()
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
	p.opt.Log.Info("audio player", "command", p.command, "pid", cmd.Process.Pid)
	p.sink = stdin
	p.stopFn = func() { terminate(cmd.Process) }
	stderrDone := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				p.mu.Lock()
				p.stderr = append(p.stderr, line)
				if len(p.stderr) > 5 {
					p.stderr = p.stderr[1:]
				}
				p.mu.Unlock()
				p.opt.Log.Info("audio player", "says", line)
			}
		}
		close(stderrDone)
	}()
	p.wait = func() error {
		select {
		case <-stderrDone:
		case <-time.After(2 * time.Second):
		}
		return cmd.Wait()
	}
	return nil
}

// run writes the stream until the tracks end or the playback is stopped.
func (p *Playback) run() {
	defer close(p.done)
	err := p.stream()
	p.sink.Close() // the player plays what it holds, then ends
	exit := p.wait()
	p.closeTracks()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopping {
		return
	}
	switch {
	case exit != nil || errors.Is(err, errPlayerGone): // what the player said tells why
		msg := "the player stopped"
		if exit != nil {
			msg += " (" + exit.Error() + ")"
		}
		if len(p.stderr) > 0 {
			msg += ": " + strings.Join(p.stderr, " / ")
		}
		p.err = errors.New(withHint(msg))
	case err != nil:
		p.err = err
	}
	if p.err != nil {
		p.opt.Log.Warn("audio playback", "err", p.err)
	}
}

// channels is the stream's channel count: the session's, 2 for a mix.
func (p *Playback) channels() int {
	if p.opt.Mix != nil {
		return 2
	}
	return p.info.Channels
}

// stream interleaves the tracks into the card's channels, or mixes them
// into two.
func (p *Playback) stream() error {
	width := 4 * p.channels()
	buf := make([]byte, playFrames*width)
	if p.opt.From < 0 { // the audio started after this point of the session
		for silence := int64(-p.opt.From * float64(p.info.Rate)); silence > 0; {
			n := min(silence, playFrames)
			clear(buf[:n*int64(width)])
			if err := p.write(buf[:n*int64(width)], n); err != nil {
				return err
			}
			silence -= n
		}
	}
	block := make([]byte, playFrames*4)
	var accL, accR []float64
	if p.opt.Mix != nil {
		accL, accR = make([]float64, playFrames), make([]float64, playFrames)
	}
	for {
		n := int64(0)
		for _, t := range p.tracks {
			n = max(n, min(t.left, playFrames))
		}
		if n == 0 {
			return nil // every track is over
		}
		clear(buf[:n*int64(width)])
		fading := false // this block fades out: the stream ends after it
		if accL != nil {
			p.mu.Lock()
			fading = p.fading
			p.mu.Unlock()
		}
		for i := range p.tracks {
			t := &p.tracks[i]
			m, size := int(min(t.left, n)), t.format.size
			if _, err := io.ReadFull(t.r, block[:m*size]); err != nil {
				return fmt.Errorf("%s: %w", filepath.Base(t.f.Name()), err)
			}
			if accL != nil {
				p.mu.Lock()
				tl, tr := t.tl, t.tr
				p.mu.Unlock()
				if fading {
					tl, tr = 0, 0
				}
				// a linear fade over the block to the gains asked for
				dl, dr := (tl-t.gl)/float64(n), (tr-t.gr)/float64(n)
				if t.gl != 0 || t.gr != 0 || tl != 0 || tr != 0 {
					for k := range m {
						v := float64(t.format.sample(block[k*size:]))
						accL[k] += v * (t.gl + dl*float64(k+1))
						accR[k] += v * (t.gr + dr*float64(k+1))
					}
				}
				t.gl, t.gr = tl, tr
			} else {
				for k := range m {
					off := k*width + 4*t.channel
					putSample(buf[off:off+4], t.format.sample(block[k*size:]), 4)
				}
			}
			t.left -= int64(m)
		}
		if accL != nil {
			for k := range int(n) {
				putSample(buf[k*8:k*8+4], clip32(accL[k]), 4)
				putSample(buf[k*8+4:k*8+8], clip32(accR[k]), 4)
				accL[k], accR[k] = 0, 0
			}
		}
		if err := p.write(buf[:n*int64(width)], n); err != nil {
			return err
		}
		if fading { // the block just written faded out
			return nil
		}
	}
}

// SetMix changes the tracks heard in a mix, fading to their gains over a
// block; a track not in mix fades out. A track not recorded is ignored.
func (p *Playback) SetMix(mix []MixTrack) {
	gains := map[int]MixTrack{}
	for _, m := range mix {
		gains[m.Channel] = m
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.tracks {
		t := &p.tracks[i]
		t.tl, t.tr = gains[t.channel+1].gains()
	}
}

// clip32 rounds a mixed sample to 32 bits, clipped at full scale.
func clip32(v float64) int32 {
	return int32(min(max(math.Round(v), math.MinInt32), math.MaxInt32))
}

// errPlayerGone is a write to a player that no longer reads.
var errPlayerGone = errors.New("the player stopped")

func (p *Playback) write(b []byte, frames int64) error {
	if _, err := p.sink.Write(b); err != nil {
		if p.isStopping() {
			return nil
		}
		return fmt.Errorf("%w: %v", errPlayerGone, err)
	}
	p.mu.Lock()
	p.frames += frames
	p.mu.Unlock()
	return nil
}

func (p *Playback) isStopping() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopping
}

func (p *Playback) closeTracks() {
	for _, t := range p.tracks {
		t.f.Close()
	}
}

// Stop stops the playback at once and waits for the player to end; a mix
// fades out first (a block, then what the player holds).
func (p *Playback) Stop() {
	select {
	case <-p.done: // over: its player is gone (its process id may be someone else's now)
		return
	default:
	}
	if p.opt.Mix != nil {
		p.mu.Lock()
		p.fading = true
		p.mu.Unlock()
		select {
		case <-p.done:
			return
		case <-time.After(400 * time.Millisecond):
		}
	}
	p.mu.Lock()
	p.stopping = true
	p.mu.Unlock()
	p.stopFn()
	<-p.done
}

// Done is closed once the playback is over: tracks played, stopped or failed.
func (p *Playback) Done() <-chan struct{} { return p.done }

// Err is why the playback failed, nil while it plays, once stopped, or
// played to its end.
func (p *Playback) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// Command is the player's command line.
func (p *Playback) Command() string { return p.command }

// Sent is how many seconds of audio went to the player (its buffer, a
// fraction of a second, is still to be heard).
func (p *Playback) Sent() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return float64(p.frames) / float64(p.info.Rate)
}
