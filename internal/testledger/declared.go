// Package testledger judges a CI run by what it executed: every declared test ran, or a named
// reason says why it did not (docs/superpowers/specs/2026-11-25-test-ledger-design.md).
package testledger

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// GoFile is one tracked _test.go file the go tool builds.
type GoFile struct {
	Rel   string          // repository-relative, slash-separated
	Dir   string          // Rel's directory
	Expr  constraint.Expr // its //go:build constraint, nil without one
	Tests []string        // the TestXxx functions `go test -run` selects from
}

// DeclaredGo reads the test files in rels, repository-relative paths as `git ls-files` lists them.
// Files the go tool never builds are skipped, and so is a listed file deleted from the work tree;
// a path that leaves root is an error, not a read.
func DeclaredGo(root string, rels []string) ([]GoFile, error) {
	tree, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tree.Close() }()
	var files []GoFile
	fset := token.NewFileSet()
	for _, rel := range rels {
		if IgnoredByGoTool(rel) {
			continue
		}
		data, err := tree.ReadFile(filepath.FromSlash(rel))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		expr, err := BuildConstraint(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		parsed, err := parser.ParseFile(fset, rel, data, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, GoFile{Rel: rel, Dir: path.Dir(rel), Expr: expr, Tests: TestFuncs(parsed)})
	}
	return files, nil
}

// IgnoredByGoTool follows `go help packages`: directories named testdata and names starting with
// "." or "_" are never built.
func IgnoredByGoTool(rel string) bool {
	return slices.ContainsFunc(strings.Split(rel, "/"), func(part string) bool {
		return part == "testdata" || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_")
	})
}

// TestFuncs lists the functions `go test` runs as tests: TestXxx(t *testing.T) with no result,
// where Xxx does not start with a lower-case letter. TestMain(m *testing.M) is the entry point, not
// a test; TestMain(t *testing.T) is a test (cmd/go/internal/load/test.go, go1.27.4).
func TestFuncs(file *ast.File) []string {
	var tests []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !takesTestingT(fn.Type) {
			continue
		}
		rest, ok := strings.CutPrefix(fn.Name.Name, "Test")
		if r, _ := utf8.DecodeRuneInString(rest); ok && (rest == "" || !unicode.IsLower(r)) {
			tests = append(tests, fn.Name.Name)
		}
	}
	return tests
}

// takesTestingT matches func(*T) with no result as cmd/go's isTestFunc does: T qualified by any
// import name (testing.T, an alias) or bare (a dot-import).
func takesTestingT(fn *ast.FuncType) bool {
	if (fn.Results != nil && len(fn.Results.List) > 0) || fn.Params == nil ||
		len(fn.Params.List) != 1 || len(fn.Params.List[0].Names) > 1 {
		return false
	}
	star, ok := fn.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	switch t := star.X.(type) {
	case *ast.Ident:
		return t.Name == "T"
	case *ast.SelectorExpr:
		return t.Sel.Name == "T"
	}
	return false
}

// BuildConstraint returns the //go:build line comment before the package clause, or nil. It reads
// the parsed comments, so text inside a /* */ block that looks like a directive is not one.
func BuildConstraint(src []byte) (constraint.Expr, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		return nil, err
	}
	for _, group := range file.Comments {
		if group.Pos() >= file.Package {
			break
		}
		for _, c := range group.List {
			if constraint.IsGoBuild(c.Text) {
				return constraint.Parse(c.Text)
			}
		}
	}
	return nil, nil
}
