package webapi

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/session"
)

// Taking a show home (docs/recording.md, "Taking a show home"): a session
// downloaded as one zip, made while it is sent, or copied to a disk plugged
// into this machine (a USB disk), its tracks as they were recorded (WAV) or
// converted to FLAC on the way. Neither writes anything on this machine's
// disk: an export does not fill the stage laptop's disk. Engineer routes.

type homeState struct {
	mu     sync.Mutex
	copy   *copyView // the copy running, or the last one
	cancel context.CancelFunc
	// tests: the disks plugged in, their space, a pause before each file
	disks      func() []diskView
	space      func(path string) (free, total uint64, err error)
	beforeFile func(name string)
}

// diskView is a disk a session can be copied to.
type diskView struct {
	Path     string `json:"path"` // where it is mounted: /media/yoda/BAND
	Name     string `json:"name"`
	Free     int64  `json:"free"` // bytes, -1 if unknown
	Size     int64  `json:"size"` // bytes, -1 if unknown
	Writable bool   `json:"writable"`
}

// copyView is a copy of a session to a disk.
type copyView struct {
	Session string `json:"session"`
	Disk    string `json:"disk"`   // its path
	Folder  string `json:"folder"` // where the session goes, once complete: <disk>/tabula/<session>
	FLAC    bool   `json:"flac"`
	Done    int64  `json:"done"`  // bytes read from this machine's disk
	Total   int64  `json:"total"` // to read
	Files   int    `json:"files"` // copied
	Of      int    `json:"of"`
	File    string `json:"file,omitempty"` // being copied
	// State is copying, done, failed or cancelled; a copy that does not
	// end done leaves nothing on the disk.
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	Recording bool      `json:"recording,omitempty"` // started while a show was being recorded
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended,omitzero"`
}

// Copy states.
const (
	copyRunning   = "copying"
	copyDone      = "done"
	copyFailed    = "failed"
	copyCancelled = "cancelled"
)

// copyMargin is the room a copy leaves on the disk, on top of the session.
const copyMargin = 16 << 20

// copyingSuffix names the folder of a copy until it is complete.
const copyingSuffix = " (copying)"

func (s *Server) homeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{name}/zip", s.zipSession)
	mux.HandleFunc("POST /api/sessions/{name}/copy", s.copySession)
	mux.HandleFunc("GET /api/disks", s.getDisks)
	mux.HandleFunc("GET /api/copy", s.getCopy)
	mux.HandleFunc("POST /api/copy/stop", s.stopCopy)
}

// recordingSession reports whether the session is being recorded: its
// files are not complete.
func (s *Server) recordingSession(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec != nil && filepath.Base(s.rec.Dir()) == name
}

var (
	errRecordingThis = errors.New("this session is being recorded: its files are complete once the recording stops")
	errNoFLAC        = errors.New("FLAC needs ffmpeg: install it, or put it next to tabula (see the recording guide)")
)

// homeFiles checks a session can be taken home and lists its files.
func (s *Server) homeFiles(w http.ResponseWriter, name string, flac bool) (*session.Session, []session.HomeFile, bool) {
	dir, err := s.sessionDir(name)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return nil, nil, false
	}
	switch {
	case s.recordingSession(name):
		httpError(w, http.StatusConflict, errRecordingThis)
		return nil, nil, false
	case flac && !audio.FLACAvailable():
		httpError(w, http.StatusConflict, errNoFLAC)
		return nil, nil, false
	}
	ses, err := s.openSession(dir)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return nil, nil, false
	}
	files, err := ses.HomeFiles(flac)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return nil, nil, false
	}
	return ses, files, true
}

// zipSession sends a session as one zip, made as it goes (?flac=1: the
// tracks converted to FLAC): its files in a folder named after it.
func (s *Server) zipSession(w http.ResponseWriter, r *http.Request) {
	flac := r.URL.Query().Get("flac") == "1"
	ses, files, ok := s.homeFiles(w, r.PathValue("name"), flac)
	if !ok {
		return
	}
	folder := ses.Name()
	if flac {
		folder += " FLAC"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", folder+".zip"))
	start := time.Now()
	s.log.Info("session: zip", "session", ses.Name(), "flac", flac, "files", len(files))
	zw := zip.NewWriter(w)
	for _, f := range files {
		h := &zip.FileHeader{Name: folder + "/" + f.Name, Method: zip.Deflate, Modified: f.Mod}
		if f.Audio() {
			h.Method = zip.Store
		}
		fw, err := zw.CreateHeader(h)
		if err == nil {
			err = f.WriteTo(r.Context(), fw, nil)
		}
		if err != nil { // the browser sees the download fail
			s.log.Warn("session: zip not complete", "session", ses.Name(), "file", f.Name, "err", err)
			return
		}
	}
	if err := zw.Close(); err != nil {
		s.log.Warn("session: zip not complete", "session", ses.Name(), "err", err)
		return
	}
	s.log.Info("session: zip sent", "session", ses.Name(), "took", time.Since(start).Round(time.Second))
}

// listDisks lists the disks plugged in, but the one holding the sessions,
// with their space.
func (s *Server) listDisks() []diskView {
	s.home.mu.Lock()
	list, space := s.home.disks, s.home.space
	s.home.mu.Unlock()
	if space == nil {
		space = diskSpace
	}
	var disks []diskView
	if list != nil {
		disks = list()
	} else {
		s.mu.Lock()
		dir := s.sessionsDir
		s.mu.Unlock()
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		disks = plugged(dir)
	}
	for i := range disks {
		disks[i].Free, disks[i].Size = -1, -1
		if free, total, err := space(disks[i].Path); err == nil {
			disks[i].Free, disks[i].Size = int64(free), int64(total)
		}
	}
	return disks
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Server) getDisks(w http.ResponseWriter, r *http.Request) {
	disks := s.listDisks()
	if disks == nil {
		disks = []diskView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"disks": disks, "flac": audio.FLACAvailable()})
}

func (s *Server) getCopy(w http.ResponseWriter, r *http.Request) {
	s.home.mu.Lock()
	var v *copyView
	if s.home.copy != nil {
		c := *s.home.copy
		v = &c
	}
	s.home.mu.Unlock()
	writeJSON(w, http.StatusOK, v)
}

// stopCopy cancels the copy running: what it copied is removed.
func (s *Server) stopCopy(w http.ResponseWriter, r *http.Request) {
	s.home.mu.Lock()
	if s.home.cancel != nil {
		s.home.cancel()
	}
	s.home.mu.Unlock()
	s.getCopy(w, r)
}

// copySession copies a session to a disk plugged in, {"disk": path,
// "flac": false}, into <disk>/tabula/<session> (" FLAC" added for FLAC
// tracks): first into a folder named "<session> (copying)", renamed once
// every file is there, so an interrupted copy is not taken for a whole
// one. It is refused when the disk has no room for it.
func (s *Server) copySession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disk string `json:"disk"`
		FLAC bool   `json:"flac"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Disk == "" {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"disk": ..., "flac": false}`))
		return
	}
	ses, files, ok := s.homeFiles(w, r.PathValue("name"), req.FLAC)
	if !ok {
		return
	}
	var disk *diskView
	for _, d := range s.listDisks() {
		if d.Path == req.Disk {
			disk = &d
		}
	}
	switch {
	case disk == nil:
		httpError(w, http.StatusBadRequest, fmt.Errorf("no disk %s plugged in", req.Disk))
		return
	case !disk.Writable:
		httpError(w, http.StatusConflict, fmt.Errorf("%s is read-only", disk.Name))
		return
	}
	folder := ses.Name()
	if req.FLAC {
		folder += " FLAC"
	}
	root := filepath.Join(disk.Path, "tabula")
	dest := filepath.Join(root, folder)
	if _, err := os.Stat(dest); err == nil {
		httpError(w, http.StatusConflict, fmt.Errorf("already on %s, in %s", disk.Name, dest))
		return
	}
	need, exact := session.HomeSize(files)
	if disk.Free >= 0 && disk.Free < need+copyMargin {
		about := ""
		if !exact {
			about = "about "
		}
		httpError(w, http.StatusInsufficientStorage, fmt.Errorf("not enough room on %s: the session takes %s%s, the disk has %s free",
			disk.Name, about, audio.FormatBytes(need), audio.FormatBytes(disk.Free)))
		return
	}
	var total int64
	for _, f := range files {
		total += f.Size
	}
	s.mu.Lock()
	recording := s.rec != nil
	s.mu.Unlock()
	v := &copyView{Session: ses.Name(), Disk: disk.Path, Folder: dest, FLAC: req.FLAC, Total: total, Of: len(files),
		State: copyRunning, Recording: recording, Started: time.Now()}
	ctx, cancel := context.WithCancel(context.Background())
	s.home.mu.Lock()
	if c := s.home.copy; c != nil && c.State == copyRunning {
		s.home.mu.Unlock()
		cancel()
		httpError(w, http.StatusConflict, fmt.Errorf("%s is being copied: one copy at a time", c.Session))
		return
	}
	s.home.copy, s.home.cancel = v, cancel
	view := *v
	s.home.mu.Unlock()
	s.log.Info("session: copy", "session", ses.Name(), "to", dest, "flac", req.FLAC, "bytes", total)
	go s.runCopy(ctx, cancel, v, files, filepath.Join(root, folder+copyingSuffix), dest)
	writeJSON(w, http.StatusAccepted, view)
}

// runCopy copies the files into tmp, then renames it dest.
func (s *Server) runCopy(ctx context.Context, cancel context.CancelFunc, v *copyView, files []session.HomeFile, tmp, dest string) {
	defer cancel()
	s.home.mu.Lock()
	pause := s.home.beforeFile
	s.home.mu.Unlock()
	os.RemoveAll(tmp) // an interrupted copy of ours
	err := os.MkdirAll(tmp, 0o755)
	for _, f := range files {
		if err != nil {
			break
		}
		if pause != nil {
			pause(f.Name)
		}
		if err = ctx.Err(); err != nil {
			break
		}
		s.home.mu.Lock()
		v.File = f.Name
		s.home.mu.Unlock()
		err = copyHomeFile(ctx, f, filepath.Join(tmp, filepath.FromSlash(f.Name)), func(n int64) {
			s.home.mu.Lock()
			v.Done += n
			s.home.mu.Unlock()
		})
		if err == nil {
			s.home.mu.Lock()
			v.Files++
			s.home.mu.Unlock()
		}
	}
	if err == nil {
		if _, serr := os.Stat(dest); serr == nil {
			err = fmt.Errorf("%s appeared meanwhile", dest)
		} else {
			err = os.Rename(tmp, dest)
		}
	}
	if err != nil {
		os.RemoveAll(tmp)
	}
	s.home.mu.Lock()
	v.File, v.Ended = "", time.Now()
	switch {
	case err == nil:
		v.State = copyDone
	case ctx.Err() != nil:
		v.State = copyCancelled
	default:
		v.State, v.Error = copyFailed, copyError(err)
	}
	st := *v
	s.home.mu.Unlock()
	if st.State == copyFailed {
		s.log.Warn("session: copy failed", "session", st.Session, "to", st.Folder, "err", err)
	} else {
		s.log.Info("session: copy "+st.State, "session", st.Session, "to", st.Folder, "took", st.Ended.Sub(st.Started).Round(time.Second))
	}
}

// copyHomeFile writes one file to the disk, flushed to it before the next.
func copyHomeFile(ctx context.Context, f session.HomeFile, to string, read func(int64)) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(out, 1<<20)
	err = f.WriteTo(ctx, bw, read)
	if err == nil {
		err = bw.Flush()
	}
	if err == nil {
		err = out.Sync() // a disk unplugged right after has the file
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	os.Chtimes(to, f.Mod, f.Mod)
	return nil
}

// unescapeMount reads the octal escapes of /proc/self/mounts: "\040" is a
// space.
func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// copyError says what to do about the usual errors of a copy.
func copyError(err error) string {
	var pe *fs.PathError
	switch {
	case errors.Is(err, syscall.ENOSPC):
		return "the disk is full (" + err.Error() + "): nothing was left on it"
	case errors.Is(err, syscall.EFBIG):
		return "a file is larger than the disk takes (" + err.Error() + "): a FAT32 disk takes files up to 4 GB, format it as exFAT"
	case errors.As(err, &pe) && errors.Is(err, fs.ErrPermission):
		return "not allowed to write on the disk (" + err.Error() + ")"
	}
	return err.Error()
}
