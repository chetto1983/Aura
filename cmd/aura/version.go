package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Build metadata. Stamped at release time by goreleaser via
// `-ldflags -X main.version=… -X main.commit=… -X main.date=…`
// (see .goreleaser.yaml). For `go build`/`go install` these stay at their
// defaults and are backfilled from the embedded module build info.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// runVersion prints human-readable build metadata to stdout.
func runVersion() {
	fmt.Print(versionString())
}

// versionString renders the version block. Split from runVersion so it is unit
// testable without capturing stdout.
func versionString() string {
	v, c, d := buildInfo()
	var b strings.Builder
	fmt.Fprintf(&b, "aura %s\n", v)
	fmt.Fprintf(&b, "commit: %s\n", orUnknown(c))
	fmt.Fprintf(&b, "built:  %s\n", orUnknown(d))
	fmt.Fprintf(&b, "go:     %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return b.String()
}

// buildInfo resolves version/commit/date, preferring the ldflags stamps and
// falling back to runtime/debug build info — which carries the module version
// for `go install module@vX` and VCS revision/time for a git `go build`.
func buildInfo() (v, c, d string) {
	v, c, d = version, commit, date
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return v, c, d
	}
	if v == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		v = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if c == "" {
				c = s.Value
			}
		case "vcs.time":
			if d == "" {
				d = s.Value
			}
		}
	}
	return v, c, d
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
