package webapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/emu"
	"github.com/yoda-jm/tabula/pkg/osc"
	"github.com/yoda-jm/tabula/pkg/scene"
	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func startEmu(t *testing.T) (*emu.Server, string) {
	t.Helper()
	st := state.New(schema.Build("M32C"))
	f, err := os.Open("../../testdata/scenes/GVO-2026.scn")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scene.Load(f, st, "test")
	s := emu.New(emu.Config{Model: "M32C", Logger: quiet}, st)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.Serve(ctx, conn)
	return s, conn.LocalAddr().String()
}

func post(t *testing.T, url, body string) int {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func TestAPI(t *testing.T) {
	emuSrv, emuAddr := startEmu(t)
	api := New(quiet)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()

	if code := post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`); code != http.StatusAccepted {
		t.Fatalf("connect: %d", code)
	}
	var st statusView
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		getJSON(t, ts.URL+"/api/status", &st)
		if st.Connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not connected: %+v", st)
		}
	}
	if st.Console.Synced != st.Console.Total {
		t.Errorf("synced %d/%d", st.Console.Synced, st.Console.Total)
	}

	var sv schemaView
	getJSON(t, ts.URL+"/api/schema", &sv)
	if len(sv.Nodes) != len(api.schema.Nodes) || len(sv.Types) > 400 {
		t.Errorf("schema: %d nodes, %d types", len(sv.Nodes), len(sv.Types))
	}

	var nodes []stateNode
	getJSON(t, ts.URL+"/api/state?prefix=/ch/12/config", &nodes)
	if len(nodes) != 1 || nodes[0].Values[0].Value != "Vx Remy" {
		t.Errorf("state: %+v", nodes)
	}
	getJSON(t, ts.URL+"/api/state?prefix=/fx/2/par", &nodes)
	if len(nodes) != 1 || nodes[0].Fields[0].TypeInfo == nil || nodes[0].Fields[0].Label != "Pre Delay" {
		t.Errorf("effect parameters: %+v", nodes)
	}

	// Event stream: the change made below must come through.
	resp, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	events := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				events <- sc.Text()
			}
		}
	}()

	if code := post(t, ts.URL+"/api/set", `{"addr":"/ch/12/mix/fader","text":"-6"}`); code != http.StatusForbidden {
		t.Errorf("unarmed set: %d", code)
	}
	post(t, ts.URL+"/api/arm", `{"armed":true}`)
	if code := post(t, ts.URL+"/api/set", `{"addr":"/ch/12/mix/fader","text":"-6"}`); code != http.StatusNoContent {
		t.Errorf("set: %d", code)
	}
	if code := post(t, ts.URL+"/api/set", `{"addr":"/ch/12/mix/on","value":0}`); code != http.StatusNoContent {
		t.Errorf("set value: %d", code)
	}
	timeout := time.After(2 * time.Second)
	for found := false; !found; {
		select {
		case e := <-events:
			found = strings.Contains(e, `"addr":"/ch/12/mix/fader","text":" -6.0"`)
		case <-timeout:
			t.Fatal("no change event")
		}
	}
	// The write reaches the emulator over UDP: give it a moment.
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		v, _, _ := emuSrv.Store().Get("/ch/12/mix/on")
		if v.Int == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("emulator mute not set: %v", v)
		}
	}
}

func connected(t *testing.T, url string) {
	t.Helper()
	var st statusView
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		getJSON(t, url+"/api/status", &st)
		if st.Connected {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("not connected: %+v", st)
		}
	}
}

func TestRecordAndReplay(t *testing.T) {
	emuSrv, emuAddr := startEmu(t)
	api := New(quiet)
	api.SetSessionsDir(t.TempDir())
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)

	if code := post(t, ts.URL+"/api/record/start", `{"note":"set 1"}`); code != http.StatusOK {
		t.Fatalf("record start: %d", code)
	}
	// A change made on the "console" (another client of the emulator).
	other, _ := net.Dial("udp", emuAddr)
	defer other.Close()
	other.Write(osc.NewMessage("/ch/12/mix/fader", osc.Float(0.25)).Bytes())
	time.Sleep(100 * time.Millisecond)
	post(t, ts.URL+"/api/record/mark", `{"label":"Song 2"}`)
	post(t, ts.URL+"/api/record/stop", ``)

	var list []sessionView
	getJSON(t, ts.URL+"/api/sessions", &list)
	if len(list) != 1 || list[0].Meta.Events < 1 || list[0].Meta.Markers != 1 {
		t.Fatalf("sessions: %+v", list)
	}
	var one sessionView
	getJSON(t, ts.URL+"/api/sessions/"+list[0].Name, &one)
	if len(one.Markers) != 1 || one.Markers[0].Marker != "Song 2" {
		t.Errorf("session: %+v", one)
	}

	// Replay needs armed writes, then brings the console back to the recording.
	body := `{"session":"` + list[0].Name + `","from":0,"speed":20}`
	if code := post(t, ts.URL+"/api/replay/start", body); code != http.StatusForbidden {
		t.Errorf("unarmed replay: %d", code)
	}
	post(t, ts.URL+"/api/arm", `{"armed":true}`)
	other.Write(osc.NewMessage("/ch/12/mix/fader", osc.Float(1)).Bytes())
	time.Sleep(50 * time.Millisecond)
	if code := post(t, ts.URL+"/api/replay/start", body); code != http.StatusAccepted {
		t.Fatalf("replay: %d", code)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		v, _, _ := emuSrv.Store().Get("/ch/12/mix/fader")
		var rv recordView
		getJSON(t, ts.URL+"/api/record", &rv)
		if !rv.Replaying && v.Float > 0.24 && v.Float < 0.26 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("replay did not restore the fader: %v, %+v", v, rv)
		}
	}
}

func TestMeters(t *testing.T) {
	_, emuAddr := startEmu(t)
	api := New(quiet)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)

	resp, err := http.Get(ts.URL + "/api/events?meters=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got := make(chan []float64, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<16), 1<<20)
		meters := false
		for sc.Scan() {
			line := sc.Text()
			if line == "event: meters" {
				meters = true
				continue
			}
			if meters && strings.HasPrefix(line, "data: ") {
				var m struct {
					ID     string    `json:"id"`
					Values []float64 `json:"values"`
				}
				json.Unmarshal([]byte(line[6:]), &m)
				if m.ID == "1" {
					got <- m.Values
					return
				}
			}
			meters = false
		}
	}()
	select {
	case v := <-got:
		if len(v) != 96 {
			t.Errorf("%d values for /meters/1", len(v))
		}
		if v[11] <= 0 { // channel 12 has a source and a signal
			t.Errorf("channel 12 input level %v", v[11])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no meters event")
	}
}
