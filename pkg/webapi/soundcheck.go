package webapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/session"
)

// A virtual soundcheck (docs/recording.md): a replay of the console moves
// with the session's tracks played back to the console's card, in step, so
// the show can be mixed again without the band. The console's inputs must
// hear the card: tabula can switch its routing to PLAY for the time of it
// (/config/routing/routswitch) and put it back as it was afterwards; the
// replay leaves that switch alone.

// routswitch chooses where the console's inputs come from: REC (0, the
// stage, /config/routing/IN) or PLAY (1, /config/routing/PLAY, the card
// by default).
const routswitch = "/config/routing/routswitch"

// routingSwitch is what a soundcheck's replay neither restores nor replays.
func routingSwitch(path string) bool { return path == "/config/routing" || path == routswitch }

// soundcheckState is the soundcheck running with a replay (under Server.mu).
type soundcheckState struct {
	on   bool                           // the tracks play with the replay
	sink func() (io.WriteCloser, error) // tests: replaces the player program
}

// soundcheck plays a session's tracks with its replay.
type soundcheck struct {
	s      *Server
	c      *console.Client
	cfg    audio.Config
	ses    *session.Session
	from   float64 // session time
	cancel context.CancelFunc
	pb     *audio.Playback
	// routed is the routing switch before the soundcheck, -1 when tabula
	// did not switch it
	routed int32
}

// newSoundcheck checks a session's tracks can play from from: an error and
// its HTTP status otherwise.
func (s *Server) newSoundcheck(c *console.Client, ses *session.Session, speed float64) (*soundcheck, int, error) {
	info := ses.Meta.Audio
	hasFiles := false
	if info != nil {
		for _, t := range info.Tracks {
			hasFiles = hasFiles || t.File != ""
		}
	}
	switch {
	case !hasFiles:
		return nil, http.StatusConflict, errors.New("this session has no audio tracks to play")
	case speed != 0 && speed != 1:
		return nil, http.StatusBadRequest, errors.New("a virtual soundcheck plays at 1×: the tracks are not sped up")
	}
	if err := s.waitAudioFree(); err != nil {
		return nil, http.StatusConflict, err
	}
	s.mu.Lock()
	cfg, recording := s.audio.cfg.Normalized(), s.rec != nil
	s.mu.Unlock()
	if recording {
		return nil, http.StatusConflict, errors.New("a recording is running: stop it before a virtual soundcheck")
	}
	if _, err := cfg.PlayCommandLine(info.Channels, info.Rate); err != nil {
		return nil, http.StatusConflict, fmt.Errorf("the tracks cannot be played: %w (Audio settings)", err)
	}
	return &soundcheck{s: s, c: c, cfg: cfg, ses: ses, routed: -1}, 0, nil
}

// toCard switches the console's inputs to the playback routing (PLAY),
// unless they are on it already.
func (sc *soundcheck) toCard() error {
	v, _, err := sc.c.Mirror().Get(routswitch)
	if err != nil {
		return err
	}
	if v.Int == 1 {
		return nil
	}
	if err := sc.c.Set(routswitch, osc.Int(1)); err != nil {
		return fmt.Errorf("switching the inputs to the card: %w", err)
	}
	sc.routed = v.Int
	sc.s.log.Info("virtual soundcheck: the console's inputs hear the card (routing PLAY)")
	return nil
}

// start plays the tracks: called as the replay's timed changes start.
func (sc *soundcheck) start() error {
	info := *sc.ses.Meta.Audio
	sc.s.mu.Lock()
	sink := sc.s.sc.sink
	sc.s.mu.Unlock()
	pb, err := audio.StartPlayback(sc.cfg, sc.ses.Dir, info, audio.PlayOptions{From: info.AudioTime(sc.from), Log: sc.s.log, Sink: sink})
	if err != nil {
		msg := "virtual soundcheck: " + err.Error()
		if h := audio.Hint(msg); h != "" {
			msg += ". " + h
		}
		return errors.New(msg)
	}
	sc.pb = pb
	started := time.Now()
	sc.s.mu.Lock()
	sc.s.sc.on = true
	sc.s.mu.Unlock()
	sc.s.log.Info("virtual soundcheck: playing", "session", sc.ses.Name(), "from", sc.from, "player", pb.Command())
	go func() { // the progress goes on between the changes; a player that fails ends the replay
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-pb.Done():
				if pb.Err() != nil {
					sc.cancel()
				}
				return
			case <-t.C:
				sc.s.mu.Lock()
				sc.s.replayAt = max(sc.s.replayAt, sc.from+time.Since(started).Seconds())
				sc.s.mu.Unlock()
				sc.s.publishRecord()
			}
		}
	}()
	return nil
}

// finish ends the soundcheck once the replay is over: the tracks play to
// their end (unless the replay was stopped), then the routing goes back as
// it was. It returns what to report.
func (sc *soundcheck) finish(ctx context.Context, err error) error {
	if sc.pb != nil {
		if err == nil {
			select {
			case <-sc.pb.Done():
			case <-ctx.Done():
			}
		}
		sc.pb.Stop()
		if perr := sc.pb.Err(); perr != nil {
			err = fmt.Errorf("virtual soundcheck: %w", perr)
		}
	}
	sc.s.mu.Lock()
	sc.s.sc.on = false
	sc.s.mu.Unlock()
	if sc.routed >= 0 { // put back even if writes were disarmed meanwhile: the stage must be heard again
		if rerr := sc.c.SetAs(routswitch, osc.Int(sc.routed), console.OriginLocal, true); rerr != nil {
			sc.s.log.Error("virtual soundcheck: the inputs stay on the card", "err", rerr)
			if errors.Is(err, context.Canceled) { // stopped: this is what to report
				err = nil
			}
			err = errors.Join(err, fmt.Errorf("the console's inputs are still on the card (routing PLAY): switch them back to REC on the Routing page (%v)", rerr))
		} else {
			sc.s.log.Info("virtual soundcheck: the console's inputs are back on the stage (routing REC)")
		}
	}
	return err
}
