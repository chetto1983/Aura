package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	goversion "go/version"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// The test-file side of tier_runners_test.go: which tracked test files sit behind a
// feature tag, and which `go test` invocation, if any, actually executes them.

type tierTestFile struct {
	rel, dir string
	expr     constraint.Expr
	gating   []string
	tests    []string
}

// tierTestFiles reads every tracked test file the go tool would build. A file is behind a
// feature tag when its constraint needs a tag set that the go command does not set by
// itself. Test names are read only in directories that hold such a file: a helper-only
// tagged file runs when a test compiled beside it does, untagged ones included.
func tierTestFiles(root string, rels []string, toolchain goToolchainTags) ([]tierTestFile, error) {
	var files []tierTestFile
	taggedDirs := map[string]bool{}
	for _, rel := range rels {
		if ignoredByGoTool(rel) {
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
		f := tierTestFile{rel: rel, dir: path.Dir(rel), expr: expr}
		for _, tag := range slices.Sorted(maps.Keys(tags)) {
			if !toolchain.decides(tag) {
				f.gating = append(f.gating, tag)
			}
		}
		if len(f.gating) > 0 {
			taggedDirs[f.dir] = true
		}
		files = append(files, f)
	}
	fset := token.NewFileSet()
	for i := range files {
		if !taggedDirs[files[i].dir] {
			continue
		}
		parsed, err := parser.ParseFile(fset, filepath.Join(root, files[i].rel), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files[i].tests = testFuncs(parsed)
	}
	return files, nil
}

// ignoredByGoTool follows `go help packages`: directories named testdata and names
// starting with "." or "_" are never built.
func ignoredByGoTool(rel string) bool {
	return slices.ContainsFunc(strings.Split(rel, "/"), func(part string) bool {
		return part == "testdata" || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_")
	})
}

// testFuncs lists the TestXxx functions `go test -run` selects from; TestMain always runs
// and is not one of them.
func testFuncs(file *ast.File) []string {
	var tests []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name == "TestMain" {
			continue
		}
		rest, ok := strings.CutPrefix(fn.Name.Name, "Test")
		if r, _ := utf8.DecodeRuneInString(rest); ok && (rest == "" || !unicode.IsLower(r)) {
			tests = append(tests, fn.Name.Name)
		}
	}
	return tests
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
// file in the default run, so it gates nothing out.
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
