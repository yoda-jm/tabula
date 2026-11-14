package webapi

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/audio"
	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/session"
	"github.com/yoda-jm/tabula/pkg/state"
)

// writeMonoWAV writes a 16-bit mono track at 48 kHz holding level (0-1)
// from frame from on, silence before.
func writeMonoWAV(t *testing.T, path string, frames, from int, level float64) {
	t.Helper()
	data := make([]byte, 2*frames)
	for i := from; i < frames; i++ {
		v := int16(level * 32767)
		if i%2 == 1 {
			v = -v
		}
		binary.LittleEndian.PutUint16(data[2*i:], uint16(v))
	}
	h := make([]byte, 44)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(data)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], 48000)
	binary.LittleEndian.PutUint32(h[28:], 96000)
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(data)))
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, append(h, data...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// localSession writes a 2 s session of the demo band: the scene at the
// start, track 1 (Hang) silent for 1 s then at half level, and two
// changes to the channel it feeds.
func localSession(t *testing.T, dir string, ch string) {
	t.Helper()
	scn, err := os.ReadFile("../../testdata/scenes/demo.scn")
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "start.scn"), scn, 0o644)
	writeMonoWAV(t, filepath.Join(dir, "audio", "01 Hang.wav"), 96000, 48000, 0.5)
	meta := session.Meta{Format: 1, Start: time.Now(), Console: "M32C test", Events: 2, Duration: 2,
		Audio: &audio.Info{Rate: 48000, Bits: 16, Channels: 32, Frames: 96000,
			Tracks: []audio.Track{{Channel: 1, Name: "Hang", File: "audio/01 Hang.wav"}, {Channel: 2, Name: "Cajon"}}}}
	b, _ := json.Marshal(meta)
	os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644)
	events := `{"t":0.5,"addr":"` + ch + `/mix/fader","text":"-20.0"}` + "\n" +
		`{"t":1.0,"addr":"` + ch + `/mix/on","text":"OFF"}` + "\n"
	os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events), 0o644)
}

func TestLocalReplay(t *testing.T) {
	_, emuAddr := startEmu(t)
	api := New(quiet)
	sessions := t.TempDir()
	api.SetSessionsDir(sessions)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)

	// the channel hearing Hang, recorded on track 1
	demo := state.New(schema.Build("M32C"))
	f, _ := os.Open("../../testdata/scenes/demo.scn")
	scene.Load(f, demo, "test")
	f.Close()
	hang := -1
	for i, track := range audio.InputTracks(demo) {
		if track == 1 && i < 32 {
			hang = i
			break
		}
	}
	if hang < 0 {
		t.Fatal("no channel records on track 1 in the demo scene")
	}
	ch := fmt.Sprintf("/ch/%02d", hang+1)
	localSession(t, filepath.Join(sessions, "show"), ch)

	status := func() statusView {
		var st statusView
		getJSON(t, ts.URL+"/api/status", &st)
		return st
	}
	transport := func(body string) int { return post(t, ts.URL+"/api/local/transport", body) }
	mirrored := func(addr string) string {
		api.mu.Lock()
		m := api.mirror
		api.mu.Unlock()
		if m == nil {
			return ""
		}
		v, l, err := m.Get(addr)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(m.TypeOf(l).Format(v))
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: %+v", what, status().Local)
			}
		}
	}

	if code := transport(`{"play":true}`); code != http.StatusConflict {
		t.Errorf("transport, no local replay: %d", code)
	}
	if code := post(t, ts.URL+"/api/local/open", `{"session":"nope"}`); code != http.StatusNotFound {
		t.Errorf("no such session: %d", code)
	}
	if code := post(t, ts.URL+"/api/local/open", `{"session":"show"}`); code != http.StatusOK {
		t.Fatalf("open: %d", code)
	}
	connected(t, ts.URL)
	st := status()
	if st.Local == nil || st.Local.Back != emuAddr || st.Console == nil || st.Console.Addr == emuAddr || !st.Console.Armed {
		t.Fatalf("open: %+v %+v", st.Local, st.Console)
	}
	if !st.Local.Audio || st.Local.Listen != "off" || st.Local.Duration != 2 {
		t.Errorf("open: %+v", st.Local)
	}
	if code := post(t, ts.URL+"/api/local/open", `{"session":"show"}`); code != http.StatusConflict {
		t.Errorf("opened twice: %d", code)
	}
	if code := post(t, ts.URL+"/api/record/start", ``); code == http.StatusOK {
		t.Error("recording while a local replay is open")
	}

	// a seek while paused sets the state of that time, which the GUI's mirror follows
	if code := transport(`{"at":1.5}`); code != http.StatusOK {
		t.Fatalf("seek: %d", code)
	}
	waitFor("seek", func() bool { return mirrored(ch+"/mix/fader") == "-20.0" && mirrored(ch+"/mix/on") == "OFF" })
	if l := status().Local; l.Playing || l.At != 1.5 {
		t.Errorf("after the seek: %+v", l)
	}
	// the meters: Hang is silent before 1 s, at half level after
	l := api.localNow()
	if in := l.inputs(); in[hang] < 0.49 || in[hang] > 0.51 {
		t.Errorf("Hang at 1.5 s: %v", in[hang])
	}
	transport(`{"at":0.2}`)
	waitFor("back to 0.2", func() bool { return mirrored(ch+"/mix/on") == "ON" })
	if in := l.inputs(); in[hang] != 0 {
		t.Errorf("Hang at 0.2 s: %v", in[hang])
	}

	// played at 4×, to the end
	transport(`{"play":true,"speed":4}`)
	waitFor("the end", func() bool { l := status().Local; return !l.Playing && l.At == 2 })
	if mirrored(ch+"/mix/on") != "OFF" {
		t.Error("the changes were not replayed")
	}

	// listening: the stereo out was not recorded; track 1 plays, in step at 1×
	if code := put(t, ts.URL+"/api/local/listen", `{"preset":"stereo"}`); code != http.StatusConflict {
		t.Errorf("stereo, not recorded: %d", code)
	}
	if code := put(t, ts.URL+"/api/local/listen", `{"tracks":[{"track":2,"gain":1}]}`); code != http.StatusBadRequest {
		t.Errorf("a track without a file: %d", code)
	}
	sink := &streamSink{}
	api.mu.Lock()
	api.sc.sink = func() (io.WriteCloser, error) { return sink, nil }
	api.mu.Unlock()
	if code := put(t, ts.URL+"/api/local/listen", `{"tracks":[{"track":1,"gain":1,"pan":0}]}`); code != http.StatusOK {
		t.Fatalf("listen: %d", code)
	}
	transport(`{"at":1,"play":true,"speed":1}`)
	waitFor("heard", func() bool { return status().Local.Heard })
	time.Sleep(100 * time.Millisecond)
	transport(`{"play":false}`)
	sink.mu.Lock()
	n := sink.b.Len()
	sink.mu.Unlock()
	if n == 0 || n%8 != 0 {
		t.Errorf("listening: %d bytes, want stereo frames", n)
	}

	// SOLO cues the raw track in the headphones: Hang's, in the middle
	soloAddr := fmt.Sprintf("/-stat/solosw/%02d", hang+1)
	l.srv.Store().Set(soloAddr, osc.Int(1), "test")
	waitFor("cue", func() bool { s := status().Local.Cue; return len(s) == 1 && s[0].Channel == 1 && s[0].Pan == 0 })
	// a seek replays the session, not its solos: the cue stays
	transport(`{"at":0.5}`)
	time.Sleep(200 * time.Millisecond)
	if s := status().Local.Cue; len(s) != 1 {
		t.Errorf("the cue after a seek: %+v", s)
	}
	l.srv.Store().Set(soloAddr, osc.Int(0), "test")
	waitFor("cue released", func() bool { return len(status().Local.Cue) == 0 })

	// marks: at the position, or at a time, sorted, kept in the session
	transport(`{"at":1.25}`)
	if code := post(t, ts.URL+"/api/local/mark", `{}`); code != http.StatusOK {
		t.Fatalf("mark: %d", code)
	}
	post(t, ts.URL+"/api/local/mark", `{"label":"  Chorus   in ","at":0.5}`)
	if code := post(t, ts.URL+"/api/local/mark", `{"label":"`+strings.Repeat("x", 81)+`"}`); code != http.StatusBadRequest {
		t.Errorf("a long label: %d", code)
	}
	if m := status().Local.Markers; len(m) != 2 || m[0].Label != "Chorus in" || m[0].T != 0.5 || m[1].Label != "Mark 0:01" || m[1].T != 1.25 {
		t.Errorf("markers: %+v", m)
	}
	ses, err := session.Open(filepath.Join(sessions, "show"))
	if err != nil {
		t.Fatal(err)
	}
	if m := ses.Markers(); len(m) != 2 || ses.Meta.Markers != 2 || m[0].Origin != "review" || ses.Meta.Events != 2 {
		t.Errorf("in the session: %+v, meta %d markers %d events", m, ses.Meta.Markers, ses.Meta.Events)
	}

	// the mark editor: renamed, moved, one removed, one added; the band follows
	edit := func(body string) int { return put(t, ts.URL+"/api/sessions/show/markers", body) }
	if code := edit(`{"markers":[{"label":"Chorus","t":0.75},{"label":"Outro","t":1.9,"origin":"review"}]}`); code != http.StatusOK {
		t.Fatalf("edit: %d", code)
	}
	if m := status().Local.Markers; len(m) != 2 || m[0].Label != "Chorus" || m[0].T != 0.75 || m[1].Label != "Outro" {
		t.Errorf("after the edit: %+v", m)
	}
	for body, want := range map[string]int{
		`{"markers":[{"label":"  ","t":1}]}`:     http.StatusBadRequest,
		`{"markers":[{"label":"Late","t":99}]}`:  http.StatusBadRequest,
		`{"markers":[{"label":"Early","t":-1}]}`: http.StatusBadRequest,
		`{"nope":1}`:                             http.StatusBadRequest,
	} {
		if code := edit(body); code != want {
			t.Errorf("%s: %d, want %d", body, code, want)
		}
	}
	if code := put(t, ts.URL+"/api/sessions/nope/markers", `{"markers":[]}`); code != http.StatusNotFound {
		t.Errorf("no such session: %d", code)
	}

	// back to the console: the same one, writes disarmed
	if code := post(t, ts.URL+"/api/local/close", ``); code != http.StatusOK {
		t.Fatalf("close: %d", code)
	}
	connected(t, ts.URL)
	st = status()
	if st.Local != nil || st.Console == nil || st.Console.Addr != emuAddr || st.Console.Armed {
		t.Errorf("back: %+v %+v", st.Local, st.Console)
	}
	if code := post(t, ts.URL+"/api/local/close", ``); code != http.StatusConflict {
		t.Errorf("closed twice: %d", code)
	}
}

func put(t *testing.T, url, body string) int {
	t.Helper()
	req, _ := http.NewRequest("PUT", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// Opened many times at once: one local replay, the others refused, nothing
// left running once it is closed.
func TestLocalOpenOnce(t *testing.T) {
	api := New(quiet)
	sessions := t.TempDir()
	api.SetSessionsDir(sessions)
	localSession(t, filepath.Join(sessions, "show"), "/ch/01")
	codes := make(chan int, 8)
	for range 8 {
		go func() {
			code, _ := api.OpenLocal("show", 0)
			codes <- code
		}()
	}
	ok := 0
	for range 8 {
		if <-codes == http.StatusOK {
			ok++
		}
	}
	if ok != 1 {
		t.Errorf("%d local replays opened", ok)
	}
	api.CloseLocal(false)
	api.mu.Lock()
	opening, local := api.localOpening, api.local
	api.mu.Unlock()
	if opening || local != nil {
		t.Errorf("after closing: opening %v, local %v", opening, local)
	}
}

// The session's own solos are not carried into the session console: SOLO
// is the listener's.
func TestLocalSessionSolosLeftOut(t *testing.T) {
	api := New(quiet)
	sessions := t.TempDir()
	api.SetSessionsDir(sessions)
	dir := filepath.Join(sessions, "show")
	localSession(t, dir, "/ch/01")
	f, _ := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"t":0.3,"addr":"/-stat/solosw/01","text":"ON"}` + "\n")
	f.Close()
	if code, err := api.OpenLocal("show", 1); err != nil {
		t.Fatalf("%d %v", code, err)
	}
	defer api.CloseLocal(false)
	l := api.localNow()
	if v, _, _ := l.srv.Store().Get("/-stat/solosw/01"); v.Int != 0 {
		t.Error("the session's solo is on in the session console")
	}
	if cue := l.view().Cue; len(cue) != 0 {
		t.Errorf("cued: %+v", cue)
	}
}
