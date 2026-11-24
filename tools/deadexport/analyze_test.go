package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func fixtureFindings(t *testing.T, tags string) []string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	findings, err := run(dir, tags, []string{"./..."})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got []string
	for _, f := range findings {
		got = append(got, f.Symbol+" "+f.Pos)
	}
	return got
}

// The fixture holds one case per rule. Reported: an exported function nobody calls, one
// only a test calls, a method only a test calls, a method of a type that reaches an
// interface (deadcode's blind spot) and a method of a generic type no instance calls.
// Spared: a direct call, a method value, a method expression, a method an interface
// declares (fmt.Stringer), a method reached only by promotion through an embedding type
// that implements an interface, a generic method an instance calls, a generated file, an
// unexported helper and a main package's own export.
func TestAnalyzeReportsWhatNoProductionCodeReaches(t *testing.T) {
	want := []string{
		"example.test/fixture/lib.Box.Drop lib/lib.go:45",
		"example.test/fixture/lib.Store.Dead lib/lib.go:17",
		"example.test/fixture/lib.TagOnly lib/lib.go:48",
		"example.test/fixture/lib.TestOnly lib/lib.go:9",
		"example.test/fixture/lib.Thing.Spare lib/lib.go:31",
		"example.test/fixture/lib.Unused lib/lib.go:7",
	}
	if got := fixtureFindings(t, ""); !reflect.DeepEqual(got, want) {
		t.Fatalf("findings:\n got  %q\n want %q", got, want)
	}
}

// A caller behind a build tag counts only when the tag is on.
func TestAnalyzeHonoursBuildTags(t *testing.T) {
	for _, f := range fixtureFindings(t, "extra") {
		if f == "example.test/fixture/lib.TagOnly lib/lib.go:48" {
			t.Fatal("TagOnly reported although the extra-tagged file calls it")
		}
	}
}

// A package that does not type-check must fail the run: the functions its broken callers
// would have referenced would otherwise read as dead.
func TestRunRefusesAPartialProgram(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("testdata", "broken"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(dir, "", []string{"./..."}); err == nil {
		t.Fatal("run on a package with a type error returned no error")
	}
}

func TestRunRefusesAnEmptyMatch(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(dir, "", []string{"./nothing/..."}); err == nil {
		t.Fatal("run with a pattern matching no package returned no error")
	}
}
