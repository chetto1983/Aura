package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/build/constraint"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tier_runners_test.go: every build tag that keeps a tracked test file out of the default
// run has a CI runner — a workflow step that passes it to `go test`, resolved by
// tier_runners_resolve_test.go — or a line in manualTiersPath saying why it runs by hand.
// The mcp_live_integration tier was broken from 2026-09-24 to 2026-11-25 and nobody saw it,
// because nothing ran -tags mcp_live_integration; this makes the next such tier fail here.

const manualTiersPath = ".github/manual_test_tiers.txt"

func TestEveryTestTierHasACIRunnerOrAReason(t *testing.T) {
	root := repoRootForTest(t)
	list := exec.Command("git", "ls-files", "-z", "--", "*_test.go")
	list.Dir = root
	out, err := list.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var files []string
	for rel := range bytes.SplitSeq(out, []byte{0}) {
		if len(rel) > 0 {
			files = append(files, string(rel))
		}
	}
	used, runners, manual := tierInventory(t, root, files)
	for _, violation := range tierRunnerViolations(used, runners, manual) {
		t.Error(violation)
	}
	for _, tag := range slices.Sorted(maps.Keys(used)) {
		if via := runners[tag]; len(via) > 0 {
			t.Logf("%s\trunner\t%s", tag, strings.Join(via, " | "))
		} else {
			t.Logf("%s\tmanual\t%s", tag, manual[tag])
		}
	}
}

func tierInventory(t *testing.T, root string, files []string) (used, runners map[string][]string, manual map[string]string) {
	t.Helper()
	used, err := gatingTestTags(root, files, toolchainTagsForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	runners, err = workflowTierRunners(root)
	if err != nil {
		t.Fatal(err)
	}
	manual, err = readManualTiers(filepath.Join(root, manualTiersPath))
	if err != nil {
		t.Fatal(err)
	}
	return used, runners, manual
}

func tierRunnerViolations(used, runners map[string][]string, manual map[string]string) []string {
	var violations []string
	for _, tag := range slices.Sorted(maps.Keys(used)) {
		if _, listed := manual[tag]; !listed && len(runners[tag]) == 0 {
			violations = append(violations, fmt.Sprintf(
				"build tag %s gates %d tracked test file(s) (%s) and no workflow step passes it to go test: wire a runner or list it in %s with a reason",
				tag, len(used[tag]), used[tag][0], manualTiersPath))
		}
	}
	for _, tag := range slices.Sorted(maps.Keys(manual)) {
		if manual[tag] == "" {
			violations = append(violations, fmt.Sprintf("%s: %s has no reason", manualTiersPath, tag))
		}
		if len(used[tag]) == 0 {
			violations = append(violations, fmt.Sprintf("%s: %s gates no tracked test file any more: remove it", manualTiersPath, tag))
		}
		if len(runners[tag]) > 0 {
			violations = append(violations, fmt.Sprintf("%s: %s now runs in CI (%s): remove it", manualTiersPath, tag, runners[tag][0]))
		}
	}
	return violations
}

// gatingTestTags maps each feature tag that selects a test file to those files. Paths the
// go tool never builds are skipped: `go help packages` ignores directories named testdata
// and names starting with "." or "_".
func gatingTestTags(root string, files []string, toolchain goToolchainTags) (map[string][]string, error) {
	used := map[string][]string{}
	for _, rel := range files {
		if slices.ContainsFunc(strings.Split(rel, "/"), func(part string) bool {
			return part == "testdata" || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_")
		}) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		expr, err := goBuildConstraint(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		tags := map[string]bool{}
		gatingTags(expr, false, tags)
		for tag := range tags {
			if !toolchain.decides(tag) {
				used[tag] = append(used[tag], rel)
			}
		}
	}
	return used, nil
}

func goBuildConstraint(src []byte) (constraint.Expr, error) {
	for line := range strings.Lines(string(src)) {
		line = strings.TrimSpace(line)
		if constraint.IsGoBuild(line) {
			return constraint.Parse(line)
		}
		if strings.HasPrefix(line, "package ") {
			break
		}
	}
	return nil, nil
}

// gatingTags collects the tags expr needs SET to select a file: those under an even number
// of negations. A tag that only appears negated (//go:build !web_integration) keeps its
// file in the default run, so it gates nothing out and needs no runner.
func gatingTags(expr constraint.Expr, negated bool, out map[string]bool) {
	switch e := expr.(type) {
	case *constraint.TagExpr:
		if !negated {
			out[e.Tag] = true
		}
	case *constraint.NotExpr:
		gatingTags(e.X, !negated, out)
	case *constraint.AndExpr:
		gatingTags(e.X, negated, out)
		gatingTags(e.Y, negated, out)
	case *constraint.OrExpr:
		gatingTags(e.X, negated, out)
		gatingTags(e.Y, negated, out)
	}
}

type goToolchainTags struct {
	goos, goarch map[string]bool
}

// toolchainTagsForTest reads GOOS and GOARCH from `go tool dist list`, the toolchain's own
// port table: the standard library keeps its known-OS and known-arch lists in
// internal/syslist, which cannot be imported, and a hand-typed copy would go stale.
func toolchainTagsForTest(t *testing.T) goToolchainTags {
	t.Helper()
	out, err := exec.Command("go", "tool", "dist", "list", "-json").Output()
	if err != nil {
		t.Fatalf("go tool dist list: %v", err)
	}
	var ports []struct{ GOOS, GOARCH string }
	if err := json.Unmarshal(out, &ports); err != nil {
		t.Fatalf("go tool dist list: %v", err)
	}
	tags := goToolchainTags{goos: map[string]bool{}, goarch: map[string]bool{}}
	for _, port := range ports {
		tags.goos[port.GOOS] = true
		tags.goarch[port.GOARCH] = true
	}
	return tags
}

// decides reports whether the go command sets tag by itself, so no -tags flag is its
// runner. Following `go help buildconstraint`: GOOS, GOARCH, GOARCH.feature, "unix", the
// compiler (gc, gccgo), "cgo" and go1.N; plus race, msan and asan, which cmd/go adds for
// the flags of the same name, goexperiment.*, which GOEXPERIMENT sets, and "ignore", the
// word that help text names as the conventional never-satisfied one.
func (g goToolchainTags) decides(tag string) bool {
	if g.goos[tag] || g.goarch[tag] {
		return true
	}
	if arch, _, ok := strings.Cut(tag, "."); ok && g.goarch[arch] {
		return true
	}
	switch tag {
	case "unix", "gc", "gccgo", "cgo", "race", "msan", "asan", "ignore":
		return true
	}
	return strings.HasPrefix(tag, "goexperiment.") || constraint.GoVersion(&constraint.TagExpr{Tag: tag}) != ""
}

// readManualTiers reads "<tag> <reason>" lines; blank lines and # comments are skipped.
func readManualTiers(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	manual := map[string]string{}
	for n, line := range slices.Collect(strings.Lines(string(data))) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tag, reason := line, ""
		if i := strings.IndexAny(line, " \t"); i >= 0 {
			tag, reason = line[:i], strings.TrimSpace(line[i:])
		}
		if _, dup := manual[tag]; dup {
			return nil, fmt.Errorf("%s:%d: %s is listed twice", path, n+1, tag)
		}
		manual[tag] = reason
	}
	return manual, nil
}
