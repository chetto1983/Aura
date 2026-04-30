package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/skills"
)

func TestSearchSkillCatalogTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{\"source\":\"vercel-labs/agent-skills\",\"skillId\":\"vercel-react-best-practices\",\"name\":\"vercel-react-best-practices\",\"installs\":361000}`)
		fmt.Fprint(w, `{\"source\":\"anthropics/skills\",\"skillId\":\"frontend-design\",\"name\":\"frontend-design\",\"installs\":353600}`)
	}))
	defer srv.Close()

	tool := NewSearchSkillCatalogTool(skills.NewCatalogClient(srv.URL))
	result, err := tool.Execute(context.Background(), map[string]any{"query": "frontend", "limit": 5})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result, "frontend-design") || !strings.Contains(result, "npx skills add anthropics/skills --skill frontend-design") {
		t.Fatalf("Execute() result missing catalog item:\n%s", result)
	}
}

func TestListSkillsTool(t *testing.T) {
	dir := t.TempDir()
	writeToolSkill(t, dir, "alpha", "Alpha skill", "Alpha body")
	writeToolSkill(t, dir, "beta", "Beta skill", "Beta body")

	tool := NewListSkillsTool(skills.NewLoader(dir))
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result, `"name": "alpha"`) || !strings.Contains(result, `"description": "Beta skill"`) {
		t.Fatalf("Execute() result missing skills:\n%s", result)
	}
}

func TestListSkillsToolEmpty(t *testing.T) {
	tool := NewListSkillsTool(skills.NewLoader(filepath.Join(t.TempDir(), "missing")))
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result != "No local skills found." {
		t.Fatalf("Execute() = %q", result)
	}
}

func TestReadSkillTool(t *testing.T) {
	dir := t.TempDir()
	writeToolSkill(t, dir, "alpha", "Alpha skill", "Alpha body")

	tool := NewReadSkillTool(skills.NewLoader(dir))
	result, err := tool.Execute(context.Background(), map[string]any{"name": "alpha"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result, "# alpha") || !strings.Contains(result, "Alpha body") {
		t.Fatalf("Execute() result missing skill:\n%s", result)
	}
}

func TestReadSkillToolRejectsBadName(t *testing.T) {
	tool := NewReadSkillTool(skills.NewLoader(t.TempDir()))
	if _, err := tool.Execute(context.Background(), map[string]any{"name": "../alpha"}); err == nil {
		t.Fatal("Execute() expected bad name error")
	}
}

func writeToolSkill(t *testing.T, root, name, description, body string) {
	t.Helper()
	path := filepath.Join(root, name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + body
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
