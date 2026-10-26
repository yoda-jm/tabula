// Package buildinfo tells which build of tabula runs: the version set when
// a release is built, or the commit a build from a git checkout was made
// from (a stage laptop running `go build` in its clone).
package buildinfo

import "runtime/debug"

// Version returns v, the version set at build time; for "dev", the version
// of a plain go build, it adds the commit when Go recorded it: "dev
// 9f91019", or "dev 9f91019+" with changes not committed.
func Version(v string) string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		return version(v, bi.Settings)
	}
	return v
}

func version(v string, settings []debug.BuildSetting) string {
	if v != "dev" {
		return v
	}
	var rev string
	modified := false
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if rev == "" {
		return v
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if modified {
		rev += "+"
	}
	return v + " " + rev
}
