package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestVersion(t *testing.T) {
	rev := debug.BuildSetting{Key: "vcs.revision", Value: "9f91019c0ffee"}
	clean := debug.BuildSetting{Key: "vcs.modified", Value: "false"}
	dirty := debug.BuildSetting{Key: "vcs.modified", Value: "true"}
	for _, c := range []struct {
		v    string
		s    []debug.BuildSetting
		want string
	}{
		{"v0.2.0", []debug.BuildSetting{rev, dirty}, "v0.2.0"}, // set at release: kept
		{"latest-9f91019", nil, "latest-9f91019"},
		{"dev", []debug.BuildSetting{rev, clean}, "dev 9f91019"},
		{"dev", []debug.BuildSetting{rev, dirty}, "dev 9f91019+"},
		{"dev", nil, "dev"}, // built outside git, or with -buildvcs=false
	} {
		if got := version(c.v, c.s); got != c.want {
			t.Errorf("version(%q, %v) = %q, want %q", c.v, c.s, got, c.want)
		}
	}
}
