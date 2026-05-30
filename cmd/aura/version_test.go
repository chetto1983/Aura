package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestVersionString_ContainsExpectedFields(t *testing.T) {
	out := versionString()
	for _, want := range []string{"aura ", "commit:", "built:", "go:", runtime.Version()} {
		if !strings.Contains(out, want) {
			t.Errorf("versionString() missing %q\n--- got ---\n%s", want, out)
		}
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("versionString() must end with newline, got %q", out)
	}
}

func TestBuildInfo_DefaultsAndBackfill(t *testing.T) {
	v, c, d := buildInfo()
	// In `go test` the ldflags are unset, so version is "dev" unless the build
	// info carries a real module version (it doesn't for a test binary).
	if v == "" {
		t.Error("version must never be empty")
	}
	// commit/date are either empty (no VCS stamp under `go test`) or the VCS
	// values — buildInfo must not panic and must return strings either way.
	_ = c
	_ = d
}

func TestOrUnknown(t *testing.T) {
	if got := orUnknown(""); got != "unknown" {
		t.Errorf("orUnknown(\"\") = %q, want \"unknown\"", got)
	}
	if got := orUnknown("abc123"); got != "abc123" {
		t.Errorf("orUnknown(\"abc123\") = %q, want \"abc123\"", got)
	}
}

func TestRunVersion_WritesToStdout(t *testing.T) {
	out := captureStdout(t, runVersion)
	if !strings.Contains(out, "aura ") {
		t.Errorf("runVersion stdout missing 'aura ': %q", out)
	}
}
