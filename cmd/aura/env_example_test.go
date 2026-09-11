package main

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// env_example_test.go keeps .env.example to what an operator must or may set. The file holds
// secrets and infrastructure; everything else is a code or compose default, and what an admin
// changes in the cockpit lives in aura.settings (management-key design, 2026-09-10). Two drifts
// are caught here: a name nothing reads any more, and a value that only repeats the default
// compose already supplies, which reads like a choice and is none.

var (
	envExampleLine    = regexp.MustCompile(`(?m)^([A-Z_][A-Z0-9_]*)=(.*?)\r?$`)
	composeEnvDefault = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*):-([^}]*)\}`)
	// composeRequiredEnv matches ${NAME:?message}: compose refuses to start without NAME.
	composeRequiredEnv = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*):\?`)
	upperIdentifier    = regexp.MustCompile(`[A-Z_][A-Z0-9_]{2,}`)
)

// envExampleEntries returns the active NAME=value lines of .env.example. Commented examples
// are documentation and are not checked.
func envExampleEntries(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := map[string]string{}
	for _, m := range envExampleLine.FindAllStringSubmatch(readProjectFile(t, root, ".env.example"), -1) {
		entries[m[1]] = m[2]
	}
	if len(entries) == 0 {
		t.Fatal(".env.example has no active NAME=value line")
	}
	return entries
}

// readerFile reports whether a tracked file counts as something that can read the
// environment. Documentation, planning notes, the generated cockpit bundle and .env.example
// itself do not.
func readerFile(path string) bool {
	switch {
	case path == ".env.example",
		strings.HasSuffix(path, ".md"),
		strings.HasPrefix(path, "docs/"),
		strings.HasPrefix(path, ".planning/"),
		strings.HasPrefix(path, "internal/webui/dist/"):
		return false
	}
	return true
}

func TestEnvExampleNamesHaveReaders(t *testing.T) {
	root := repoRootForTest(t)
	// Tracked files only: a checkout also holds local scratch (artifacts/, spikes/, the
	// operator's own .env) that would count as a reader here and in no deployed build.
	list := exec.Command("git", "ls-files", "-z")
	list.Dir = root
	out, err := list.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	read := map[string]struct{}{}
	for rel := range bytes.SplitSeq(out, []byte{0}) {
		path := string(rel)
		if path == "" || !readerFile(path) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			// A tracked file deleted in the working tree reads nothing.
			continue
		}
		for _, name := range upperIdentifier.FindAll(data, -1) {
			read[string(name)] = struct{}{}
		}
	}
	entries := envExampleEntries(t, root)
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		if _, ok := read[name]; !ok {
			t.Errorf(".env.example sets %s, which no tracked code, compose file or script reads", name)
		}
	}
}

func TestEnvExampleValuesDifferFromCompose(t *testing.T) {
	root := repoRootForTest(t)
	composeFiles, err := filepath.Glob(filepath.Join(root, "compose*.yaml"))
	if err != nil || len(composeFiles) == 0 {
		t.Fatalf("no compose*.yaml at the repository root (err %v)", err)
	}
	defaults := map[string]map[string]struct{}{}
	for _, path := range composeFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range composeEnvDefault.FindAllStringSubmatch(string(data), -1) {
			if defaults[m[1]] == nil {
				defaults[m[1]] = map[string]struct{}{}
			}
			defaults[m[1]][m[2]] = struct{}{}
		}
	}
	entries := envExampleEntries(t, root)
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		values := defaults[name]
		if len(values) != 1 {
			// No default, or compose files that disagree: there is no single value to repeat.
			continue
		}
		// ${NAME:-default} applies the default to an EMPTY value too, so an empty line says
		// the same thing as one that copies the default.
		if _, same := values[entries[name]]; same || entries[name] == "" {
			t.Errorf(".env.example sets %s=%q, which only repeats compose's default: comment it out", name, entries[name])
		}
	}
}
