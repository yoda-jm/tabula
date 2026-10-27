package webapi

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/state"
)

// Backups: before something overwrites the console's settings (a scene
// file load, a replay, a recall of a scene, snippet or preset, a scene from
// the USB stick, initialize, undo), the settings are saved as a scene file
// of the scenes folder, "Backup 2026-10-27 21.14.05.scn", so one wrong
// click can be put back with Load. The newest keepBackups are kept.

// keepBackups is how many backups the scenes folder keeps (about 70 KB each).
const keepBackups = 30

// backupFile matches the backups' file names, and only them: the oldest are deleted.
var backupFile = regexp.MustCompile(`^Backup \d{4}-\d\d-\d\d \d\d\.\d\d\.\d\d(-\d+)?\.scn$`)

// backupBefore saves the console's settings before what ("recalling scene
// 03") overwrites them. It returns the file written, "" when none was:
// not connected or still syncing, or the same settings as the newest
// backup (stepping through snippets back and forth). A backup that fails
// is logged and does not stop what comes next: on stage, a full disk
// should not keep a scene from loading.
func (s *Server) backupBefore(what string) string {
	file, err := s.saveBackup(what, time.Now())
	if err != nil {
		s.log.Warn("backup before "+what, "err", err)
		s.mu.Lock()
		s.backupErr = fmt.Sprintf("the backup before %s failed: %v", what, err)
		s.mu.Unlock()
		return ""
	}
	if file != "" {
		s.mu.Lock()
		s.backupErr = ""
		s.mu.Unlock()
		s.log.Info("backup", "before", what, "file", file)
	}
	return file
}

func (s *Server) saveBackup(what string, now time.Time) (string, error) {
	s.mu.Lock()
	mirror, connecting, dir := s.mirror, s.connecting, s.scenesDir
	s.mu.Unlock()
	if mirror == nil || connecting {
		return "", nil
	}
	var body bytes.Buffer
	if err := scene.Write(&body, mirror, ""); err != nil {
		return "", err
	}
	backups, err := listBackups(dir)
	if err != nil {
		return "", err
	}
	if n := len(backups); n > 0 {
		if last, err := os.ReadFile(filepath.Join(dir, backups[n-1])); err == nil {
			if _, rest, ok := bytes.Cut(last, []byte("\n")); ok && bytes.Equal(rest, body.Bytes()[1:]) {
				return "", nil
			}
		}
	}
	header := scene.Header("Backup before "+what, "saved by tabula "+now.Format("2006-01-02 15:04:05"))
	data := append([]byte(header), body.Bytes()...)
	base := "Backup " + now.Format("2006-01-02 15.04.05")
	name := base + ".scn"
	for i := 2; ; i++ {
		err = s.writeSceneFile(name, data, false)
		if !errors.Is(err, os.ErrExist) || i > 99 {
			break
		}
		name = fmt.Sprintf("%s-%d.scn", base, i)
	}
	if err != nil {
		return "", err
	}
	backups = append(backups, name)
	for _, old := range backups[:max(0, len(backups)-keepBackups)] {
		if err := os.Remove(filepath.Join(dir, old)); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("backup: removing an old one", "file", old, "err", err)
		}
	}
	return name, nil
}

// listBackups returns the backups of the folder, oldest first.
func listBackups(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && backupFile.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	// "…05.scn" before "…05-2.scn": compare without the extension
	slices.SortFunc(out, func(a, b string) int {
		return strings.Compare(strings.TrimSuffix(a, ".scn"), strings.TrimSuffix(b, ".scn"))
	})
	return out, nil
}

// libNames name what a library load changes, for its backup.
var libNames = map[string]string{
	"scene": "recalling scene", "snippet": "recalling snippet", "libchan": "loading channel preset",
	"libfx": "loading effect preset", "librout": "loading routing preset", "libmon": "loading monitor preset",
}

// slotName is "scene 03 Rappel": a slot of the library as the console numbers it.
func slotName(mirror *state.Store, kind string, index int) string {
	l := libListings[kind]
	num := fmt.Sprintf("%02d", index)
	if l.first == 1 {
		num = fmt.Sprintf("%03d", index+1)
	}
	if v, _, err := mirror.Get(listingPath(kind, index) + "/name"); err == nil && strings.TrimSpace(v.Str) != "" {
		num += " " + strings.TrimSpace(v.Str)
	}
	return num
}

// backupWhat names a library command for the backup before it, "" for
// one that does not change the console's settings (save, copy…).
func (q libraryRequest) backupWhat(mirror *state.Store) string {
	if q.Op != "load" || libNames[q.Kind] == "" {
		return ""
	}
	return libNames[q.Kind] + " " + slotName(mirror, q.Kind, q.Index)
}

// actionBackup names a write to /-action that overwrites the console's
// settings (a recall, initialize, undo), "" for any other write.
func actionBackup(mirror *state.Store, addr string, v osc.Arg) string {
	n := int(v.Int)
	if v.Tag != 'i' {
		return ""
	}
	switch addr {
	case "/-action/goscene":
		return "recalling scene " + slotName(mirror, "scene", n)
	case "/-action/gosnippet":
		return "recalling snippet " + slotName(mirror, "snippet", n)
	case "/-action/gocue":
		return fmt.Sprintf("recalling cue %d", n)
	case "/-action/initall":
		if n == 1 {
			return "initializing the console"
		}
	case "/-action/doundo":
		if n == 1 {
			return "undoing on the console"
		}
	}
	return ""
}
