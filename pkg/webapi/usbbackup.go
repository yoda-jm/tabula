package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/session"
)

// The backup on the console's USB stick: with the setting on
// (audio.Config.USBBackup), Start recording also starts the console's own
// recorder (/-stat/tape/state RECORD). The console writes a stereo WAV file
// of what feeds its recorder (the main mix unless /outputs/rec says
// otherwise) to its stick by itself, so the backup does not depend on the
// laptop. Stop recording stops it. A recording the console was making
// already is left on, and the player is not interrupted: walk-in music
// played from the stick would stop. The console starts recording without
// writes being armed: the setting asked for it. See docs/recording.md.

// Tape states, as /-stat/tape/state numbers them.
const (
	tapeStop     = 0
	tapePause    = 1
	tapePlay     = 2
	tapeRecPause = 3
	tapeRecord   = 4
)

// usbBackupWait is how long the console may take to start recording.
var usbBackupWait = 3 * time.Second

// usbBackupView is the setting and the backup of the recording (or of the
// last one), in /api/record.
type usbBackupView struct {
	On       bool   `json:"on"`                 // started with every recording
	Starting bool   `json:"starting,omitempty"` // being started
	File     string `json:"file,omitempty"`     // the file the console records, on the stick
	Already  bool   `json:"already,omitempty"`  // the console was recording already: Stop leaves it on
	Error    string `json:"error,omitempty"`    // why the recording has no backup
}

type usbBackupState struct {
	view usbBackupView     // all but On
	rec  *session.Recorder // the recording it backs up; nil once stopped
	sent bool              // the console was asked to record: Stop stops it
}

// startUSBBackupLocked starts the backup of rec, if the setting is on.
func (s *Server) startUSBBackupLocked(rec *session.Recorder) {
	if !s.audio.cfg.USBBackup || s.client == nil {
		return
	}
	s.backup = usbBackupState{rec: rec, view: usbBackupView{Starting: true}}
	go s.startUSBBackup(rec, s.client)
}

func (s *Server) startUSBBackup(rec *session.Recorder, c *console.Client) {
	b, sent, err := runUSBBackup(c)
	if err != nil {
		b.Error = err.Error()
	}
	s.mu.Lock()
	if s.backup.rec != rec { // the recording ended meanwhile
		s.mu.Unlock()
		if sent {
			stopTape(c)
		}
		return
	}
	s.backup.view = usbBackupView{File: b.File, Already: b.Already, Error: b.Error}
	s.backup.sent = sent
	s.mu.Unlock()
	switch {
	case err != nil:
		s.log.Warn("usb backup: not recorded on the console's stick", "err", err)
	case b.Already:
		s.log.Info("usb backup: the console was recording on its stick already", "file", b.File)
	default:
		s.log.Info("usb backup: the console records on its stick", "file", b.File)
	}
	if werr := rec.SetUSBBackup(&b); werr != nil {
		s.log.Error("usb backup", "err", werr)
	}
	s.publishRecord()
}

// runUSBBackup has the console record on its stick. sent tells whether it
// was asked to (it may have started after the wait).
func runUSBBackup(c *console.Client) (b session.USBBackup, sent bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), usbBackupWait+5*time.Second)
	defer cancel()
	m, err := c.Get(ctx, "/-stat/usbmounted")
	if err != nil {
		return b, false, fmt.Errorf("the console did not answer: %w", err)
	}
	if m.Int != 1 {
		return b, false, errors.New("no USB stick in the console")
	}
	st, err := c.Get(ctx, "/-stat/tape/state")
	if err != nil {
		return b, false, fmt.Errorf("the console did not answer: %w", err)
	}
	switch st.Int {
	case tapeRecord, tapeRecPause:
		f, err := c.Get(ctx, "/-stat/tape/file")
		return session.USBBackup{File: f.Str, Already: true}, false, err
	case tapePlay, tapePause:
		return b, false, errors.New("the console's USB player is in use (walk-in music?): the backup was not started, so as not to stop it; " +
			"start it on the console's recorder screen once the music is over")
	}
	if err := c.SetAs("/-stat/tape/state", osc.Int(tapeRecord), console.OriginLocal, true); err != nil {
		return b, false, err
	}
	for deadline := time.Now().Add(usbBackupWait); ; time.Sleep(200 * time.Millisecond) {
		if st, err = c.Get(ctx, "/-stat/tape/state"); err == nil && st.Int == tapeRecord {
			break
		}
		if time.Now().After(deadline) {
			return b, true, fmt.Errorf("the console did not start recording on its stick within %v (a full or locked stick?)", usbBackupWait)
		}
	}
	f, err := c.Get(ctx, "/-stat/tape/file")
	return session.USBBackup{File: f.Str}, true, err
}

// stopUSBBackupLocked stops the backup when the recording ends, unless the
// console's player was used since (Stop would interrupt it).
func (s *Server) stopUSBBackupLocked() {
	sent, file := s.backup.sent, s.backup.view.File
	s.backup.rec, s.backup.sent, s.backup.view.Starting = nil, false, false
	if !sent || s.client == nil || s.mirror == nil {
		return
	}
	if v, _, err := s.mirror.Get("/-stat/tape/state"); err == nil && (v.Int == tapePlay || v.Int == tapePause) {
		return
	}
	c := s.client
	if err := stopTape(c); err != nil {
		s.log.Warn("usb backup: could not stop the console's recorder", "err", err)
		return
	}
	s.log.Info("usb backup stopped", "file", file)
	go func() { // the console may not tell the client that stopped it
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		time.Sleep(200 * time.Millisecond)
		c.Get(ctx, "/-stat/tape/state")
	}()
}

// stopTape stops the console's recorder. The message is sent as it is: the
// mirror learns the new state from the console, as setting it here would
// publish the change, which needs s.mu.
func stopTape(c *console.Client) error {
	return c.Send(osc.NewMessage("/-stat/tape/state", osc.Int(tapeStop)))
}

// setUSBBackup turns the backup on or off for the next recordings. Turned
// on during a recording, it starts now; turned off, the console's recording
// goes on until Stop recording.
func (s *Server) setUSBBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"on": true|false}`))
		return
	}
	s.mu.Lock()
	s.audio.cfg.USBBackup = req.On
	cfg, file := s.audio.cfg, s.audio.file
	// started now, or again after it failed (a stick put in since)
	if b := s.backup; req.On && s.rec != nil && (b.rec != s.rec || b.view.Error != "" && !b.sent) {
		s.startUSBBackupLocked(s.rec)
	}
	s.mu.Unlock()
	if file != "" {
		if err := audio.SaveConfig(file, cfg); err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.log.Info("usb backup", "on", req.On)
	s.publishRecord()
	writeJSON(w, http.StatusOK, s.recordStatus())
}
