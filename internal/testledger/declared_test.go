package testledger

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBuildConstraint(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"and-not", "//go:build a && !b\n\npackage x\n", "a && !b"},
		{"or", "// Copyright\n\n//go:build a || b\n\npackage x\n", "a || b"},
		{"none", "package x\n", ""},
		{"after the package clause it is a comment", "package x\n\n//go:build a\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := BuildConstraint([]byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if expr != nil {
				got = expr.String()
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	if _, err := BuildConstraint([]byte("//go:build a &&\n\npackage x\n")); err == nil {
		t.Fatal("a malformed constraint must be an error")
	}
}

func TestGatingTags(t *testing.T) {
	cases := map[string][]string{
		"a && !b":          {"a"},
		"a || b":           {"a", "b"},
		"!(a && !c)":       {"c"},
		"!web_integration": nil,
		"b && a && b":      {"a", "b"},
	}
	for src, want := range cases {
		expr, err := BuildConstraint([]byte("//go:build " + src + "\n\npackage x\n"))
		if err != nil {
			t.Fatal(err)
		}
		if got := GatingTags(expr); !slices.Equal(got, want) {
			t.Errorf("%s: got %v, want %v", src, got, want)
		}
	}
	if got := GatingTags(nil); got != nil {
		t.Errorf("no constraint gates nothing, got %v", got)
	}
}

func TestTestFuncs(t *testing.T) {
	src := `package x

import (
	"testing"
	tt "testing"
)

func TestPlain(t *testing.T)        {}
func Test(t *testing.T)             {}
func TestAliased(t *tt.T)           {}
func TestMain(m *testing.M)         {}
func Testdata(t *testing.T)         {}
func BenchmarkX(b *testing.B)       {}
func FuzzX(f *testing.F)            {}
func TestHelper(tb testing.TB)      {}
func TestReturns(t *testing.T) bool { return true }
func TestTwo(t *testing.T, n int)   {}
func TestÜmlaut(t *testing.T)       {}

type suite struct{}

func (suite) TestMethod(t *testing.T) {}
`
	file, err := parser.ParseFile(token.NewFileSet(), "x_test.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TestPlain", "Test", "TestAliased", "TestÜmlaut"}
	if got := TestFuncs(file); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestIgnoredByGoTool(t *testing.T) {
	for rel, want := range map[string]bool{
		"internal/x/x_test.go":                 false,
		"scripts/testdata/go_test_ledger/x.go": true,
		"internal/.cache/x_test.go":            true,
		"internal/_old/x_test.go":              true,
		"internal/under_score/x_test.go":       false,
		"testdata/x_test.go":                   true,
	} {
		if got := IgnoredByGoTool(rel); got != want {
			t.Errorf("%s: got %v, want %v", rel, got, want)
		}
	}
}

func TestDeclaredGo(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
	write("a/live_test.go", "//go:build live && !race\n\npackage a\n\nimport \"testing\"\n\nfunc TestLive(t *testing.T) {}\n")
	write("a/testdata/fixture_test.go", "package broken\n\nfunc TestNever(t *testing.T) { this does not parse }\n")

	files, err := DeclaredGo(root, []string{"a/a_test.go", "a/live_test.go", "a/testdata/fixture_test.go", "a/deleted_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("want the two built files, got %+v", files)
	}
	if f := files[0]; f.Rel != "a/a_test.go" || f.Dir != "a" || f.Expr != nil || !slices.Equal(f.Tests, []string{"TestA"}) {
		t.Errorf("untagged file: %+v", f)
	}
	if f := files[1]; f.Expr == nil || f.Expr.String() != "live && !race" || !slices.Equal(f.Tests, []string{"TestLive"}) {
		t.Errorf("tagged file: %+v", f)
	}

	write("b/bad_test.go", "//go:build ||\n\npackage b\n")
	if _, err := DeclaredGo(root, []string{"b/bad_test.go"}); err == nil {
		t.Fatal("a malformed constraint must fail the read, naming the file")
	}
	outside := filepath.Join(t.TempDir(), "outside_test.go")
	if err := os.WriteFile(outside, []byte("package o\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "a", "link_test.go")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{filepath.ToSlash(outside), "a/link_test.go"} {
		if _, err := DeclaredGo(root, []string{rel}); err == nil {
			t.Errorf("%s leaves the root and must be refused, not read", rel)
		}
	}
	if files, err := DeclaredGo(root, []string{"../outside_test.go"}); err != nil || len(files) != 0 {
		t.Errorf("a .. path is one the go tool ignores: got %v, %v", files, err)
	}
	if _, err := DeclaredGo(filepath.Join(root, "missing"), nil); err == nil {
		t.Fatal("a root that does not exist must be an error")
	}
	write("c/c_test.go", "package c\n\nfunc TestC( {\n")
	if _, err := DeclaredGo(root, []string{"c/c_test.go"}); err == nil {
		t.Fatal("a file that does not parse must fail the read")
	}
}
