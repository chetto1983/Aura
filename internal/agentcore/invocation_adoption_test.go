package agentcore

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAgentCoreBuilderAdoptedByWebAndTelegram(t *testing.T) {
	repoRoot := testRepoRoot(t)
	files := []string{
		filepath.Join("cmd", "aura", "web_chat.go"),
		filepath.Join("internal", "channels", "telegram", "invocation_builder.go"),
	}

	for _, rel := range files {
		t.Run(rel, func(t *testing.T) {
			path := filepath.Join(repoRoot, rel)
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", rel, err)
			}
			text := string(src)
			if !strings.Contains(text, "agentcore.Builder{}).Build") {
				t.Fatalf("%s does not call agentcore.Builder.Build", rel)
			}
			if !strings.Contains(text, "agentcore.InvocationInput{") {
				t.Fatalf("%s does not pass agentcore.InvocationInput", rel)
			}

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, src, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", rel, err)
			}
			var directLiterals []string
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok || len(lit.Elts) == 0 {
					return true
				}
				if !isAgentInvocationType(lit.Type) {
					return true
				}
				directLiterals = append(directLiterals, fset.Position(lit.Pos()).String())
				return true
			})
			if len(directLiterals) > 0 {
				t.Fatalf("%s still owns non-empty agent.Invocation literals: %s", rel, strings.Join(directLiterals, ", "))
			}
		})
	}
}

func TestAgentCoreBuilderDoesNotAddRuntimeFlagShim(t *testing.T) {
	repoRoot := testRepoRoot(t)
	files := []string{
		filepath.Join("internal", "config", "config.go"),
		filepath.Join("cmd", "aura", "web_chat.go"),
		filepath.Join("internal", "channels", "telegram", "invocation_builder.go"),
	}

	for _, rel := range files {
		src, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if strings.Contains(string(src), "AURA_AGENTCORE_BUILDER") {
			t.Fatalf("%s contains AURA_AGENTCORE_BUILDER; do not add a no-op runtime flag after legacy invocation assembly was removed", rel)
		}
	}
}

func isAgentInvocationType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Invocation" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "agent"
}

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
