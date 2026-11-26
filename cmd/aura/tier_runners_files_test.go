package main

import (
	"encoding/json"
	"go/build/constraint"
	goversion "go/version"
	"maps"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/testledger"
)

// The test-file side of tier_runners_test.go: which tracked test files sit behind a
// feature tag, and which `go test` invocation, if any, actually executes them.

type tierTestFile struct {
	rel, dir string
	expr     constraint.Expr
	gating   []string
	tests    []string
}

// tierTestFiles reads every tracked test file the go tool would build
// (testledger.DeclaredGo). A file is behind a feature tag when its constraint needs a tag set
// that the go command does not set by itself.
func tierTestFiles(root string, rels []string, toolchain goToolchainTags) ([]tierTestFile, error) {
	declared, err := testledger.DeclaredGo(root, rels)
	if err != nil {
		return nil, err
	}
	files := make([]tierTestFile, 0, len(declared))
	for _, d := range declared {
		f := tierTestFile{rel: d.Rel, dir: d.Dir, expr: d.Expr, tests: d.Tests}
		for _, tag := range gatingTags(d.Expr) {
			if !toolchain.decides(tag) {
				f.gating = append(f.gating, tag)
			}
		}
		files = append(files, f)
	}
	return files, nil
}

// fileCoverage says which invocations execute f. An invocation executes a test of f when
// its tags satisfy f's constraint, its packages include f's directory and its -run selects
// the test; a helper-only file is executed when such an invocation selects a test compiled
// beside it. missing lists f's tests that no invocation selects. The run returned is one
// that executes f, an unconditional one when there is any.
func fileCoverage(f tierTestFile, sameDir []tierTestFile, runs []goTestRun) (run goTestRun, reached bool, missing []string) {
	executes := func(r goTestRun, tests []string) bool {
		if !r.covers(f.dir) || !r.satisfies(f.expr) || !slices.ContainsFunc(tests, r.runs) {
			return false
		}
		if !reached || (run.condition != "" && r.condition == "") {
			run = r
		}
		reached = true
		return true
	}
	if len(f.tests) == 0 {
		for _, r := range runs {
			var compiled []string
			for _, g := range sameDir {
				if r.satisfies(g.expr) {
					compiled = append(compiled, g.tests...)
				}
			}
			executes(r, compiled)
		}
		return run, reached, nil
	}
	for _, test := range f.tests {
		selected := false
		for _, r := range runs {
			selected = executes(r, []string{test}) || selected
		}
		if !selected {
			missing = append(missing, test)
		}
	}
	return run, reached, missing
}

// whyUnreached names the first condition no invocation meets, for the failure message.
func whyUnreached(f tierTestFile, runs []goTestRun) string {
	tagged := slices.DeleteFunc(slices.Clone(runs), func(r goTestRun) bool { return !r.satisfies(f.expr) })
	switch {
	case len(tagged) == 0:
		return "no go test invocation sets tags satisfying " + f.expr.String()
	case !slices.ContainsFunc(tagged, func(r goTestRun) bool { return r.covers(f.dir) }):
		return "invocations with its tags never name ./" + f.dir
	default:
		return "invocations with its tags and package select none of its tests with -run"
	}
}

// satisfies evaluates a constraint as CI's runners build it: ubuntu-latest, linux/amd64,
// gc with cgo, this toolchain's release tags, race only under -race.
func (r goTestRun) satisfies(expr constraint.Expr) bool {
	if expr == nil {
		return true
	}
	return expr.Eval(func(tag string) bool {
		switch tag {
		case "linux", "amd64", "amd64.v1", "unix", "gc", "cgo":
			return true
		case "race":
			return r.race
		}
		if v := constraint.GoVersion(&constraint.TagExpr{Tag: tag}); v != "" {
			return goversion.Compare(v, runtime.Version()) <= 0
		}
		return slices.Contains(r.tags, tag)
	})
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

// decides reports whether the go command sets tag by itself, so it is no feature tier.
// Following `go help buildconstraint`: GOOS, GOARCH, GOARCH.feature, "unix", the compiler
// (gc, gccgo), "cgo" and go1.N; plus race, msan and asan, which cmd/go adds for the flags
// of the same name, goexperiment.*, which GOEXPERIMENT sets, and "ignore", the word that
// help text names as the conventional never-satisfied one.
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

// gatingTags lists, sorted, the tags expr needs SET to select a file: those under an even number of
// negations. A tag that only appears negated (//go:build !web_integration) keeps its file in the
// default run, so it gates nothing out.
func gatingTags(expr constraint.Expr) []string {
	tags := map[string]bool{}
	gating(expr, false, tags)
	if len(tags) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(tags))
}

func gating(expr constraint.Expr, negated bool, out map[string]bool) {
	switch e := expr.(type) {
	case *constraint.TagExpr:
		if !negated {
			out[e.Tag] = true
		}
	case *constraint.NotExpr:
		gating(e.X, !negated, out)
	case *constraint.AndExpr:
		gating(e.X, negated, out)
		gating(e.Y, negated, out)
	case *constraint.OrExpr:
		gating(e.X, negated, out)
		gating(e.Y, negated, out)
	}
}
