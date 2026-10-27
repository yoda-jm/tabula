package webapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yoda-jm/tabula/pkg/console"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/state"
)

// Scene files: the .scn files kept in a folder of this machine (-scenes),
// as the console, M32-Edit or X32-Edit write them. One is sent to the
// console line by line, the way the editors' PC → Mixer does: every
// parameter it holds changes at once, without the console's scene safes.
// The console's settings are saved as one too. See docs/API.md, "Scene
// files".

// maxSceneFile bounds an upload: a whole scene is about 70 KB.
const maxSceneFile = 2 << 20

// sceneLoadState is the scene file being sent to the console, one at a time.
type sceneLoadState struct {
	mu       sync.Mutex
	progress *sceneProgress
}

// sceneProgress is published as "scenefile" events while a file loads,
// then once more when it is over (Done).
type sceneProgress struct {
	File    string   `json:"file"`
	Sent    int      `json:"sent"`
	Total   int      `json:"total"`
	Done    bool     `json:"done"`
	Skipped []string `json:"skipped,omitempty"` // lines this console does not know
	Error   string   `json:"error,omitempty"`
	// writes were disarmed during the load: they disarm once it is over
	DisarmAfter bool `json:"disarm_after,omitempty"`
	// the console's settings before the load, saved as this file (scenebackup.go)
	Backup string `json:"backup,omitempty"`
}

func (s *Server) sceneFileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/scenefiles", s.listSceneFiles)
	mux.HandleFunc("POST /api/scenefiles", s.saveSceneFile)
	mux.HandleFunc("GET /api/scenefiles/{file}", s.getSceneFile)
	mux.HandleFunc("PUT /api/scenefiles/{file}", s.putSceneFile)
	mux.HandleFunc("DELETE /api/scenefiles/{file}", s.deleteSceneFile)
	mux.HandleFunc("POST /api/scenefiles/{file}/load", s.loadSceneFile)
}

// SetScenesDir sets the folder of the scene files.
func (s *Server) SetScenesDir(dir string) {
	s.mu.Lock()
	s.scenesDir = dir
	s.mu.Unlock()
}

func (s *Server) scenesFolder() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scenesDir
}

type sceneFile struct {
	File     string    `json:"file"` // "GVO-2026.scn"
	Name     string    `json:"name"` // from the header
	Note     string    `json:"note,omitempty"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// sceneHeader reads `#4.0# "name" "note" %000000000 1`.
var sceneHeader = regexp.MustCompile(`^#[^#]*#\s*"((?:[^"\\]|\\.)*)"(?:\s+"((?:[^"\\]|\\.)*)")?`)

func parseHeader(line string) (name, note string) {
	if m := sceneHeader.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
		return unquote(m[1]), unquote(m[2])
	}
	return "", ""
}

func unquote(s string) string {
	return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(s)
}

// validFileName accepts a plain .scn file name of the folder.
func validFileName(name string) error {
	switch {
	case name == "" || len(name) > 120:
		return errors.New("a file name of 1 to 120 characters is needed")
	case strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, "."):
		return fmt.Errorf("%q is not a file name of the scenes folder", name)
	case !strings.EqualFold(filepath.Ext(name), ".scn"):
		return fmt.Errorf("%q is not a scene file (.scn)", name)
	case strings.IndexFunc(name, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0:
		return fmt.Errorf("%q holds control characters", name)
	}
	return nil
}

// fileNameFor makes a file name of a scene name: "GVO 2026" → "GVO 2026.scn".
func fileNameFor(name string) string {
	f := strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	f = strings.Trim(f, ". ")
	if f == "" {
		f = "scene"
	}
	if len(f) > 100 {
		f = f[:100]
	}
	return f + ".scn"
}

func (s *Server) listSceneFiles(w http.ResponseWriter, r *http.Request) {
	dir := s.scenesFolder()
	out := struct {
		Dir         string         `json:"dir"`
		Files       []sceneFile    `json:"files"`
		Loading     *sceneProgress `json:"loading,omitempty"`
		BackupError string         `json:"backup_error,omitempty"` // the last backup failed (scenebackup.go)
	}{Dir: dir, Files: []sceneFile{}}
	s.mu.Lock()
	out.BackupError = s.backupErr
	s.mu.Unlock()
	if abs, err := filepath.Abs(dir); err == nil {
		out.Dir = abs
	}
	s.scenes.mu.Lock()
	if p := s.scenes.progress; p != nil && !p.Done {
		c := *p
		out.Loading = &c
	}
	s.scenes.mu.Unlock()
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	for _, e := range entries {
		if e.IsDir() || validFileName(e.Name()) != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		f := sceneFile{File: e.Name(), Size: info.Size(), Modified: info.ModTime()}
		if fh, err := os.Open(filepath.Join(dir, e.Name())); err == nil {
			line, _ := bufio.NewReader(io.LimitReader(fh, 4096)).ReadString('\n')
			fh.Close()
			f.Name, f.Note = parseHeader(line)
		}
		out.Files = append(out.Files, f)
	}
	slices.SortFunc(out.Files, func(a, b sceneFile) int { return strings.Compare(strings.ToLower(a.File), strings.ToLower(b.File)) })
	writeJSON(w, http.StatusOK, out)
}

// sceneFilePath checks the {file} of a request and returns its path.
func (s *Server) sceneFilePath(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := r.PathValue("file")
	if err := validFileName(name); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return "", false
	}
	return filepath.Join(s.scenesFolder(), name), true
}

func (s *Server) getSceneFile(w http.ResponseWriter, r *http.Request) {
	p, ok := s.sceneFilePath(w, r)
	if !ok {
		return
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		httpError(w, http.StatusNotFound, fmt.Errorf("no scene file %s", filepath.Base(p)))
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(p)))
	w.Write(data)
}

// sceneLines reads a scene file: its header and node lines. A file
// without node lines is not a scene.
func sceneLines(data []byte) (header string, lines []string, err error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case strings.HasPrefix(line, "#") && header == "" && len(lines) == 0:
			header = line
		case strings.HasPrefix(line, "/"):
			lines = append(lines, line)
		}
	}
	if err := sc.Err(); err != nil {
		return "", nil, err
	}
	if len(lines) == 0 {
		return "", nil, errors.New("no parameter lines (/ch/01/config …): not a scene file")
	}
	return header, lines, nil
}

// writeSceneFile writes a file of the folder, unless it exists and
// replace is false.
func (s *Server) writeSceneFile(name string, data []byte, replace bool) error {
	dir := s.scenesFolder()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dir, name)
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !replace {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(p, flags, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrExist) {
		httpError(w, http.StatusConflict, errors.New("a scene file of this name exists: replace it, or choose another name"))
		return
	}
	httpError(w, http.StatusInternalServerError, err)
}

// putSceneFile keeps a scene file sent from the browser (?replace=1 to
// replace one of the same name).
func (s *Server) putSceneFile(w http.ResponseWriter, r *http.Request) {
	p, ok := s.sceneFilePath(w, r)
	if !ok {
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxSceneFile+1))
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	if len(data) > maxSceneFile {
		httpError(w, http.StatusRequestEntityTooLarge, errors.New("larger than 2 MB: not a scene file"))
		return
	}
	if _, _, err := sceneLines(data); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.writeSceneFile(filepath.Base(p), data, r.URL.Query().Get("replace") == "1"); err != nil {
		writeError(w, err)
		return
	}
	s.log.Info("scene file", "kept", p)
	w.WriteHeader(http.StatusNoContent)
}

// saveSceneFile writes the console's current settings as a scene file.
func (s *Server) saveSceneFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Note    string `json:"note"`
		File    string `json:"file"` // default: from the name
		Replace bool   `json:"replace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, errors.New(`expects {"name": ..., "note": ...}`))
		return
	}
	name := strings.TrimSpace(req.Name)
	if req.File == "" {
		req.File = fileNameFor(name)
	}
	if err := validFileName(req.File); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	mirror, connecting := s.mirror, s.connecting
	s.mu.Unlock()
	if mirror == nil || connecting {
		httpError(w, http.StatusConflict, errors.New("not connected (or still syncing)"))
		return
	}
	var b bytes.Buffer
	if err := scene.Write(&b, mirror, scene.Header(name, strings.TrimSpace(req.Note))); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.writeSceneFile(req.File, b.Bytes(), req.Replace); err != nil {
		writeError(w, err)
		return
	}
	s.log.Info("scene file", "saved", req.File)
	writeJSON(w, http.StatusOK, map[string]string{"file": req.File})
}

func (s *Server) deleteSceneFile(w http.ResponseWriter, r *http.Request) {
	p, ok := s.sceneFilePath(w, r)
	if !ok {
		return
	}
	if err := os.Remove(p); errors.Is(err, os.ErrNotExist) {
		httpError(w, http.StatusNotFound, fmt.Errorf("no scene file %s", filepath.Base(p)))
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info("scene file", "deleted", p)
	w.WriteHeader(http.StatusNoContent)
}

// sceneProgressEvery is how many lines go between two progress events.
const sceneProgressEvery = 50

// loadSceneFile sends a scene file to the console, line by line, each
// echoed before the next goes (console.Client.ApplyText). It goes on if
// the browser leaves: half a scene would be worse. Lines this console
// does not know (another model, another firmware) are skipped and told.
func (s *Server) loadSceneFile(w http.ResponseWriter, r *http.Request) {
	p, ok := s.sceneFilePath(w, r)
	if !ok {
		return
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		httpError(w, http.StatusNotFound, fmt.Errorf("no scene file %s", filepath.Base(p)))
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	_, lines, err := sceneLines(data)
	if err != nil {
		httpError(w, http.StatusUnprocessableEntity, err)
		return
	}
	s.mu.Lock()
	c, mirror, connecting, replaying := s.client, s.mirror, s.connecting, s.replayCancel != nil
	s.mu.Unlock()
	switch {
	case c == nil || mirror == nil || connecting:
		httpError(w, http.StatusConflict, errors.New("not connected (or still syncing)"))
		return
	case !c.Status().Armed:
		httpError(w, http.StatusForbidden, console.ErrReadOnly)
		return
	case replaying:
		httpError(w, http.StatusConflict, errors.New("a session is being replayed onto the console"))
		return
	}

	// what this console knows, in the file's order (effect parameters
	// follow the effect types set before them)
	scratch := state.New(mirror.Schema())
	var send, skipped []string
	for _, line := range lines {
		if _, err := scratch.ApplyText(line, "check"); err != nil {
			skipped = append(skipped, line)
			continue
		}
		send = append(send, line)
	}

	s.scenes.mu.Lock()
	if cur := s.scenes.progress; cur != nil && !cur.Done {
		s.scenes.mu.Unlock()
		httpError(w, http.StatusConflict, fmt.Errorf("%s is being loaded", cur.File))
		return
	}
	prog := &sceneProgress{File: filepath.Base(p), Total: len(send), Skipped: skipped}
	s.scenes.progress = prog
	s.scenes.mu.Unlock()
	publish := func() {
		s.scenes.mu.Lock()
		v := *prog
		s.scenes.mu.Unlock()
		s.hub.publish("scenefile", v)
	}
	backup := s.backupBefore("loading " + prog.File)
	s.scenes.mu.Lock()
	prog.Backup = backup
	s.scenes.mu.Unlock()
	publish()
	s.log.Info("scene file: loading", "file", prog.File, "lines", len(send), "skipped", len(skipped))
	start := time.Now()
	var failed error
	for i, line := range send {
		if err := c.ApplyText(line); err != nil {
			failed = fmt.Errorf("stopped at line %d of %d (%s): %w", i+1, len(send), line, err)
			break
		}
		s.scenes.mu.Lock()
		prog.Sent = i + 1
		s.scenes.mu.Unlock()
		if (i+1)%sceneProgressEvery == 0 {
			publish()
		}
	}
	s.scenes.mu.Lock()
	prog.Done = true
	if failed != nil {
		prog.Error = failed.Error()
	}
	v := *prog
	s.scenes.mu.Unlock()
	if v.DisarmAfter {
		c.Arm(false)
		s.publishStatus()
	}
	s.hub.publish("scenefile", v)
	if failed != nil {
		s.log.Warn("scene file", "file", prog.File, "err", failed)
	} else {
		s.log.Info("scene file: loaded", "file", prog.File, "lines", prog.Sent, "took", time.Since(start).Round(time.Millisecond))
	}
	writeJSON(w, http.StatusOK, v)
}

// disarmAfterLoad makes a disarm wait for the end of the scene file being
// loaded (on), or keeps writes armed after it (off); false when no file is
// loading. Stopping in the middle would leave the console with half a
// scene, worse than either the old one or the new one.
func (s *Server) disarmAfterLoad(on bool) bool {
	s.scenes.mu.Lock()
	p := s.scenes.progress
	loading := p != nil && !p.Done
	var v sceneProgress
	if loading {
		p.DisarmAfter = on
		v = *p
	}
	s.scenes.mu.Unlock()
	if loading {
		s.hub.publish("scenefile", v)
		if on {
			s.log.Info("writes disarm once the scene file is loaded", "file", v.File)
		}
	}
	return loading
}
