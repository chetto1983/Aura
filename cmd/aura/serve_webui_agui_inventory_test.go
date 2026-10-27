package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
)

// TestServeWebuiMountsEveryAGUIRoute drives every pattern the AG-UI mux registers through
// the cockpit's parent mux. A route the AG-UI server serves but the parent never mounts is
// reachable from no client: it falls to the SPA fallback's /api/ exclusion and 404s.
//
// The class has shipped twice. On 2026-09-21 the embeddings picker catalogue 404'd on the
// live deployment (TestServeWebuiMountsEveryCatalogueRoute pins that list). On 2026-10-26 the
// owner-export download did: POST .../export-delete deleted the conversation and answered with
// a download_url that never reached AG-UI. Both were registered in internal/agui and missing
// from newServeHandler, so this test reads the registrations from the source instead of
// keeping a third list that can drift.
func TestServeWebuiMountsEveryAGUIRoute(t *testing.T) {
	patterns := aguiRegisteredPatterns(t)
	patterns = append(patterns, agui.RemoteAccessRoutes()...)
	patterns = append(patterns, agui.SettingsCatalogRoutes()...)
	if len(patterns) < 200 {
		t.Fatalf("read %d AG-UI patterns, want the full mux (>200): the source scan is broken", len(patterns))
	}

	const localID = "00000000-0000-0000-0000-000000000001"
	var hits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		_, _ = io.WriteString(w, `{"agui":true}`)
	})
	handler, err := newServeHandler(aguiHandler, authulaTestDeps(localID, everyCapability{id: localID}), &fakeAuthulaProvider{}, "")
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}

	for _, pattern := range patterns {
		method, path := splitPattern(pattern)
		target := concretePath(path)
		hits = nil
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, target, nil)
		addAuthulaSession(req)
		handler.ServeHTTP(rec, req)
		if len(hits) != 1 || hits[0] != target {
			t.Errorf("%q (as %s %s) never reached AG-UI through the cockpit mux: code=%d hits=%v", pattern, method, target, rec.Code, hits)
		}
	}
}

// everyCapability grants every capability, so a capability-gated mount is judged on whether
// it reaches AG-UI, not on whether the test principal holds the right grant.
type everyCapability struct{ id string }

func (e everyCapability) GetIdentityByID(_ context.Context, id string) (agui.Identity, error) {
	if id != e.id {
		return agui.Identity{}, errWiringNotFound
	}
	return agui.Identity{ID: id, Name: "local", Kind: "user"}, nil
}

func (e everyCapability) HasCapability(_ context.Context, id, _ string) (bool, error) {
	return id == e.id, nil
}

// aguiRegisteredPatterns parses internal/agui and returns the first argument of every
// mux.Handle/mux.HandleFunc call, folded to its constant value. The two registrations that
// range over a route list (remote access, settings catalogues) are skipped here; their lists
// are exported and appended by the caller. Any other argument that is not a constant string
// fails the test, so a new registration shape cannot silently drop out of the inventory.
func aguiRegisteredPatterns(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join("..", "..", "internal", "agui")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	consts := packageStringConsts(files)
	listVars := map[string]bool{"route": true, "pattern": true}
	var out []string
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
				return true
			}
			if recv, ok := sel.X.(*ast.Ident); !ok || recv.Name != "mux" {
				return true
			}
			if id, ok := call.Args[0].(*ast.Ident); ok && listVars[id.Name] {
				return true
			}
			value, ok := stringConst(call.Args[0], consts)
			if !ok {
				t.Errorf("%s: route pattern is not a constant string; extend aguiRegisteredPatterns", fset.Position(call.Pos()))
				return true
			}
			out = append(out, value)
			return true
		})
	}
	return out
}

func packageStringConsts(files []*ast.File) map[string]string {
	pending := map[string]ast.Expr{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						pending[name.Name] = vs.Values[i]
					}
				}
			}
		}
	}
	resolved := map[string]string{}
	for progress := true; progress; {
		progress = false
		for name, expr := range pending {
			if value, ok := stringConst(expr, resolved); ok {
				resolved[name] = value
				delete(pending, name)
				progress = true
			}
		}
	}
	return resolved
}

func stringConst(expr ast.Expr, consts map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(e.Value)
		return value, err == nil
	case *ast.Ident:
		value, ok := consts[e.Name]
		return value, ok
	case *ast.ParenExpr:
		return stringConst(e.X, consts)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, ok := stringConst(e.X, consts)
		if !ok {
			return "", false
		}
		right, ok := stringConst(e.Y, consts)
		return left + right, ok
	}
	return "", false
}

func splitPattern(pattern string) (method, path string) {
	if m, p, ok := strings.Cut(pattern, " "); ok {
		return m, p
	}
	return http.MethodGet, pattern
}

var wildcard = regexp.MustCompile(`\{[^}]*\}`)

// concretePath fills each wildcard with a literal segment: {$} matches the bare trailing
// slash, {name...} any remainder, {name} one segment.
func concretePath(path string) string {
	return wildcard.ReplaceAllStringFunc(path, func(w string) string {
		switch {
		case w == "{$}":
			return ""
		case strings.HasSuffix(w, "...}"):
			return "x/y"
		default:
			return "x"
		}
	})
}
