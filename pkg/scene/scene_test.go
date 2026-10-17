package scene

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yoda-jm/tabula/pkg/schema"
	"github.com/yoda-jm/tabula/pkg/state"
)

// Loading a real scene and writing it back must give the same file.
func TestFileRoundTrip(t *testing.T) {
	files, _ := filepath.Glob("../../testdata/scenes/*.scn")
	for _, file := range files {
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		st := state.New(schema.Build("M32C"))
		header, errs := Load(bytes.NewReader(want), st, "test")
		for _, err := range errs {
			t.Errorf("%s: %v", filepath.Base(file), err)
		}
		var got bytes.Buffer
		if err := Write(&got, st, header); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Bytes(), want) {
			g, w := strings.Split(got.String(), "\n"), strings.Split(string(want), "\n")
			for i := 0; i < len(g) && i < len(w); i++ {
				if g[i] != w[i] {
					t.Errorf("%s line %d:\n got %q\nwant %q", filepath.Base(file), i+1, g[i], w[i])
					break
				}
			}
			t.Errorf("%s: %d lines written, %d in file", filepath.Base(file), len(g), len(w))
		}
	}
}

func TestHeader(t *testing.T) {
	h := Header("GVO-2026", "")
	if len(h) != headerWidth || !strings.HasPrefix(h, `#4.0# "GVO-2026" "" %000000000 1 `) {
		t.Errorf("header %q (%d chars)", h, len(h))
	}
}
