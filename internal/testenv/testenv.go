// Package testenv is how a Go test that needs something from its environment (a variable, a
// daemon, a binary, a symlink) learns it is missing. It skips where a developer runs it and fails
// under $CI, where a skipped tier would be a job that passes while exercising nothing (CLAUDE.md,
// no skip-as-green). Every gate here ends in Unavailable, so the rule is written once.
package testenv

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// CI reports whether the test runs under CI: $CI is set to anything but whitespace.
func CI() bool { return strings.TrimSpace(os.Getenv("CI")) != "" }

// Unavailable ends a test that cannot run here: it fails under CI and skips elsewhere.
func Unavailable(t testing.TB, format string, args ...any) {
	t.Helper()
	why := fmt.Sprintf(format, args...)
	if CI() {
		t.Fatal(why + "; $CI is set, so this fails instead of skipping")
	}
	t.Skip(why)
}

// Require returns the environment variable key, or ends the test through Unavailable when it is
// unset or blank. The value is returned as set, untrimmed.
func Require(t testing.TB, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if strings.TrimSpace(value) == "" {
		Unavailable(t, "%s is unset", key)
	}
	return value
}

// Executable returns the path of the named program, or ends the test when it is not on PATH.
func Executable(t testing.TB, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		Unavailable(t, "%s is not installed: %v", name, err)
	}
	return path
}

// Symlink makes link point at target, or ends the test where the host cannot make symlinks
// (Windows without the privilege).
func Symlink(t testing.TB, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		Unavailable(t, "this host cannot make symlinks: %v", err)
	}
}
