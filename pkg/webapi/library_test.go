package webapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yoda-jm/tabula/pkg/osc"
)

func TestLibraryCommands(t *testing.T) {
	for _, c := range []struct {
		req  libraryRequest
		want string // command and arguments, or "error"
	}{
		{libraryRequest{Op: "save", Kind: "scene", Index: 3, Name: " Concert ", Note: "set 1"}, `/save ,siss "scene" 3 "Concert" "set 1"`},
		{libraryRequest{Op: "save", Kind: "snippet", Index: 0, Name: "Intro"}, `/save ,sis "snippet" 0 "Intro"`},
		{libraryRequest{Op: "save", Kind: "libchan", Index: 45, Name: "Vox", Target: ptrInt(11)}, `/save ,sisi "libchan" 45 "Vox" 11`},
		{libraryRequest{Op: "save", Kind: "libfx", Index: 2, Name: "Hall", Target: ptrInt(1)}, `/save ,sisi "libfx" 2 "Hall" 1`},
		{libraryRequest{Op: "save", Kind: "librout", Index: 0, Name: "DL32"}, `/save ,sis "librout" 0 "DL32"`},
		{libraryRequest{Op: "load", Kind: "scene", Index: 99}, `/load ,si "scene" 99`},
		{libraryRequest{Op: "load", Kind: "libchan", Index: 45, Target: ptrInt(2)}, `/load ,siii "libchan" 45 2 63`},
		{libraryRequest{Op: "load", Kind: "libchan", Index: 45, Target: ptrInt(2), Scope: ptrInt(2)}, `/load ,siii "libchan" 45 2 2`},
		{libraryRequest{Op: "load", Kind: "libfx", Index: 1, Target: ptrInt(7)}, `/load ,sii "libfx" 1 7`},
		{libraryRequest{Op: "copy", Kind: "libchan", Index: 45, To: ptrInt(48)}, `/copy ,sii "libchan" 45 48`},
		{libraryRequest{Op: "delete", Kind: "scene", Index: 99}, `/delete ,si "scene" 99`},
		{libraryRequest{Op: "rename", Kind: "scene", Index: 99, Name: "myScene"}, `/rename ,sis "scene" 99 "myScene"`},
		{libraryRequest{Op: "save", Kind: "scene", Index: 3}, "error"},                    // no name
		{libraryRequest{Op: "load", Kind: "scene", Index: 100}, "error"},                  // index
		{libraryRequest{Op: "load", Kind: "show", Index: 1}, "error"},                     // kind
		{libraryRequest{Op: "copy", Kind: "snippet", Index: 1, To: ptrInt(2)}, "error"},   // snippets
		{libraryRequest{Op: "copy", Kind: "scene", Index: 1}, "error"},                    // destination
		{libraryRequest{Op: "load", Kind: "libchan", Index: 1}, "error"},                  // target
		{libraryRequest{Op: "load", Kind: "libfx", Index: 1, Target: ptrInt(8)}, "error"}, // FX slot
		{libraryRequest{Op: "load", Kind: "libchan", Index: 1, Target: ptrInt(0), Scope: ptrInt(64)}, "error"},
		{libraryRequest{Op: "showdump", Kind: "scene", Index: 1}, "error"},
	} {
		cmd, args, err := c.req.command()
		got := "error"
		if err == nil {
			got = osc.NewMessage(cmd, args...).String()
		}
		if got != c.want {
			t.Errorf("%+v: got %s, want %s", c.req, got, c.want)
		}
	}
	if listingPath("scene", 3) != "/-show/showfile/scene/003" || listingPath("libchan", 45) != "/-libs/ch/046" {
		t.Error("listing paths")
	}
}

func ptrInt(v int) *int { return &v }

func TestLibraryAPI(t *testing.T) {
	_, emuAddr := startEmu(t)
	api := New(quiet)
	ts := httptest.NewServer(api.Handler(http.NotFoundHandler()))
	defer ts.Close()
	post(t, ts.URL+"/api/connect", `{"addr":"`+emuAddr+`"}`)
	connected(t, ts.URL)

	save := `{"op":"save","kind":"scene","index":2,"name":"Concert","note":"set 1"}`
	if code := post(t, ts.URL+"/api/library", save); code != http.StatusForbidden {
		t.Errorf("unarmed save: %d", code)
	}
	post(t, ts.URL+"/api/arm", `{"armed":true}`)
	if code := post(t, ts.URL+"/api/library", save); code != http.StatusNoContent {
		t.Fatalf("save: %d", code)
	}
	var nodes []stateNode
	getJSON(t, ts.URL+"/api/state?prefix=/-show/showfile/scene/002", &nodes)
	if len(nodes) != 1 || nodes[0].Line != `/-show/showfile/scene/002 "Concert" "set 1" %000000000 1` {
		t.Errorf("listing after save: %+v", nodes)
	}
	if code := post(t, ts.URL+"/api/library", `{"op":"load","kind":"scene","index":50}`); code != http.StatusUnprocessableEntity {
		t.Errorf("load of an empty scene: %d", code)
	}
	if code := post(t, ts.URL+"/api/library", `{"op":"load","kind":"scene"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("load scene 0 (empty, refused by the console): %d", code)
	}
	if code := post(t, ts.URL+"/api/library", `{"op":"rename","kind":"scene","index":2}`); code != http.StatusBadRequest {
		t.Errorf("rename without a name: %d", code)
	}

	// A recall brings the mirror back to the scene (read again in the background).
	other, _ := net.Dial("udp", emuAddr)
	defer other.Close()
	var before []stateNode
	getJSON(t, ts.URL+"/api/state?prefix=/ch/12/mix", &before)
	other.Write(osc.NewMessage("/ch/12/mix/on", osc.Int(1-int32(before[0].Values[0].Value.(float64)))).Bytes())
	time.Sleep(100 * time.Millisecond)
	if code := post(t, ts.URL+"/api/library", `{"op":"load","kind":"scene","index":2}`); code != http.StatusNoContent {
		t.Fatalf("load: %d", code)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		var after []stateNode
		getJSON(t, ts.URL+"/api/state?prefix=/ch/12/mix", &after)
		if after[0].Line == before[0].Line {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after recall %q, want %q", after[0].Line, before[0].Line)
		}
	}

	if code := post(t, ts.URL+"/api/library", `{"op":"delete","kind":"scene","index":2}`); code != http.StatusNoContent {
		t.Errorf("delete: %d", code)
	}
	getJSON(t, ts.URL+"/api/state?prefix=/-show/showfile/scene/002", &nodes)
	if nodes[0].Values[3].Value != float64(0) {
		t.Errorf("listing after delete: %+v", nodes[0].Line)
	}
}
