package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/identity"
	auraskills "github.com/aura/aura/internal/skills"
)

func TestSkillTool_ListCatalogAndInfo(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "aura-runtime-safety", "Runtime guardrails", "Keep runtime safe.")
	writeTestSkill(t, root, "frontend-design", "Design UI", strings.Repeat("body ", 10))

	catalog := &fakeSkillCatalog{items: []auraskills.CatalogItem{
		{Source: "anthropics/skills", SkillID: "frontend-design", Name: "Frontend Design", Installs: 42},
		{Source: "openai/skills", SkillID: "spreadsheets", Name: "Spreadsheets", Installs: 7},
	}}
	tool := NewSkillTool(auraskills.NewLoader(root), catalog, nil, nil, false)
	if tool == nil {
		t.Fatal("NewSkillTool returned nil")
	}

	list := executeSkillTool(t, tool, context.Background(), map[string]any{"action": "list", "limit": 1})
	if list["schema"] != "skill_list" {
		t.Fatalf("schema = %v, want skill_list", list["schema"])
	}
	if list["count"] != float64(1) || list["total"] != float64(2) || list["truncated"] != true {
		t.Fatalf("list counts = count %v total %v truncated %v", list["count"], list["total"], list["truncated"])
	}

	cat := executeSkillTool(t, tool, context.Background(), map[string]any{"action": "catalog", "query": "front", "limit": 5})
	if cat["schema"] != "skill_catalog" || cat["count"] != float64(2) {
		t.Fatalf("catalog response = %#v", cat)
	}
	if catalog.query != "front" || catalog.limit != 5 {
		t.Fatalf("catalog query/limit = %q/%d, want front/5", catalog.query, catalog.limit)
	}

	info := executeSkillTool(t, tool, context.Background(), map[string]any{"action": "info", "name": "aura-runtime-safety"})
	if info["schema"] != "skill_info" || info["name"] != "aura-runtime-safety" {
		t.Fatalf("info response = %#v", info)
	}
	if !strings.Contains(info["content"].(string), "Keep runtime safe") {
		t.Fatalf("info content = %q", info["content"])
	}
}

func TestSkillTool_InstallDeniedWithoutAdminFlag(t *testing.T) {
	installer := &fakeSkillInstaller{}
	tool := NewSkillTool(&fakeSkillLoader{}, nil, installer, nil, false)
	ctx := skillToolContext(identity.CapabilitySkillsInstall)

	resp := executeSkillTool(t, tool, ctx, map[string]any{
		"action":   "install",
		"source":   "anthropics/skills",
		"skill_id": "frontend-design",
	})
	assertSkillDenial(t, resp, identity.CapabilitySkillsInstall)
	if installer.called {
		t.Fatal("installer called on denied install")
	}
}

func TestSkillTool_InstallDeniedWithoutCapability(t *testing.T) {
	installer := &fakeSkillInstaller{}
	tool := NewSkillTool(&fakeSkillLoader{}, nil, installer, nil, true)
	ctx := skillToolContext()

	resp := executeSkillTool(t, tool, ctx, map[string]any{
		"action":   "install",
		"source":   "anthropics/skills",
		"skill_id": "frontend-design",
	})
	assertSkillDenial(t, resp, identity.CapabilitySkillsInstall)
	if installer.called {
		t.Fatal("installer called on denied install")
	}
}

func TestSkillTool_InstallAndRemoveWithAdminCapability(t *testing.T) {
	loader := &fakeSkillLoader{}
	installer := &fakeSkillInstaller{output: "installed ok"}
	deleter := &fakeSkillDeleter{}
	tool := NewSkillTool(loader, nil, installer, deleter, true)
	ctx := skillToolContext(identity.CapabilitySkillsInstall, identity.CapabilitySkillsDelete)

	installed := executeSkillTool(t, tool, ctx, map[string]any{
		"action":   "install",
		"source":   "anthropics/skills",
		"skill_id": "frontend-design",
	})
	if installed["schema"] != "skill_install" || installed["ok"] != true {
		t.Fatalf("install response = %#v", installed)
	}
	if installer.source != "anthropics/skills" || installer.skillID != "frontend-design" {
		t.Fatalf("installer args = %q/%q", installer.source, installer.skillID)
	}
	if loader.invalidations != 1 {
		t.Fatalf("loader invalidations after install = %d, want 1", loader.invalidations)
	}

	removed := executeSkillTool(t, tool, ctx, map[string]any{"action": "remove", "name": "frontend-design"})
	if removed["schema"] != "skill_remove" || removed["ok"] != true || removed["name"] != "frontend-design" {
		t.Fatalf("remove response = %#v", removed)
	}
	if deleter.name != "frontend-design" {
		t.Fatalf("deleter name = %q", deleter.name)
	}
	if loader.invalidations != 2 {
		t.Fatalf("loader invalidations after remove = %d, want 2", loader.invalidations)
	}
}

func TestSkillTool_RemoveDeniedWithoutCapability(t *testing.T) {
	deleter := &fakeSkillDeleter{}
	tool := NewSkillTool(&fakeSkillLoader{}, nil, nil, deleter, true)
	resp := executeSkillTool(t, tool, skillToolContext(), map[string]any{"action": "remove", "name": "frontend-design"})
	assertSkillDenial(t, resp, identity.CapabilitySkillsDelete)
	if deleter.called {
		t.Fatal("deleter called on denied remove")
	}
}

func TestSkillTool_InvalidInputsAndNilDeps(t *testing.T) {
	if NewSkillTool(nil, nil, nil, nil, false) != nil {
		t.Fatal("NewSkillTool with nil deps should return nil")
	}
	tool := NewSkillTool(nil, nil, &fakeSkillInstaller{}, &fakeSkillDeleter{}, true)
	ctx := skillToolContext(identity.CapabilitySkillsInstall, identity.CapabilitySkillsDelete)

	if _, err := tool.Execute(ctx, map[string]any{"action": "install", "source": "../bad"}); err == nil {
		t.Fatal("invalid install source error = nil")
	}
	if _, err := tool.Execute(ctx, map[string]any{"action": "remove", "name": "../bad"}); err == nil {
		t.Fatal("invalid remove name error = nil")
	}
	if _, err := tool.Execute(ctx, map[string]any{"action": "unknown"}); err == nil {
		t.Fatal("unknown action error = nil")
	}
}

func TestSkillTool_DescriptionStaysShort(t *testing.T) {
	tool := NewSkillTool(&fakeSkillLoader{}, nil, nil, nil, false)
	if n := len(tool.Description()); n > 200 {
		t.Fatalf("description length = %d, want <= 200", n)
	}
}

func writeTestSkill(t *testing.T, root, name, desc, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s\n", name, desc, body)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func executeSkillTool(t *testing.T, tool *SkillTool, ctx context.Context, args map[string]any) map[string]any {
	t.Helper()
	out, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute(%v): %v", args, err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", out, err)
	}
	return resp
}

func assertSkillDenial(t *testing.T, resp map[string]any, capability identity.Capability) {
	t.Helper()
	if resp["schema"] != "denial" || resp["error"] != "capability_denied" || resp["capability"] != string(capability) {
		t.Fatalf("denial = %#v, want capability %s", resp, capability)
	}
}

func skillToolContext(allowed ...identity.Capability) context.Context {
	authz := fakeSkillAuthorizer{allowed: map[identity.Capability]bool{}}
	for _, cap := range allowed {
		authz.allowed[cap] = true
	}
	ctx := identity.WithAuthorizer(context.Background(), authz)
	return identity.WithActorID(ctx, "actor:skill-test")
}

type fakeSkillAuthorizer struct {
	allowed map[identity.Capability]bool
}

func (f fakeSkillAuthorizer) Authorize(_ context.Context, params identity.AuthorizeParams) (identity.AuthorizationDecision, error) {
	decision := identity.DecisionDeny
	reason := "missing_grant"
	if f.allowed[params.Capability] {
		decision = identity.DecisionAllow
		reason = "active_grant"
	}
	return identity.AuthorizationDecision{
		ActorID:    params.ActorID,
		Capability: params.Capability,
		Resource:   params.Resource,
		Decision:   decision,
		Reason:     reason,
	}, nil
}

type fakeSkillLoader struct {
	invalidations int
}

func (f *fakeSkillLoader) LoadAll() ([]auraskills.Skill, error) {
	return nil, nil
}

func (f *fakeSkillLoader) LoadByName(string) (auraskills.Skill, error) {
	return auraskills.Skill{}, errors.New("not implemented")
}

func (f *fakeSkillLoader) Invalidate() {
	f.invalidations++
}

type fakeSkillCatalog struct {
	items []auraskills.CatalogItem
	query string
	limit int
}

func (f *fakeSkillCatalog) Search(_ context.Context, query string, limit int) ([]auraskills.CatalogItem, error) {
	f.query = query
	f.limit = limit
	return append([]auraskills.CatalogItem(nil), f.items...), nil
}

type fakeSkillInstaller struct {
	called  bool
	source  string
	skillID string
	output  string
}

func (f *fakeSkillInstaller) Install(_ context.Context, source, skillID string) (string, error) {
	f.called = true
	f.source = source
	f.skillID = skillID
	return f.output, nil
}

type fakeSkillDeleter struct {
	called bool
	name   string
}

func (f *fakeSkillDeleter) Delete(name string) error {
	f.called = true
	f.name = name
	return nil
}
