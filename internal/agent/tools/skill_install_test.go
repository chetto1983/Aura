package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeCatalog is a scripted skillCatalog. results are returned ranked already (the
// handler does not re-rank — the live CatalogClient ranks); disabled toggles D-12.
type fakeCatalog struct {
	results  []CatalogResult
	err      error
	disabled bool
	calls    int
	gotQuery string
}

func (f *fakeCatalog) Search(_ context.Context, query string) ([]CatalogResult, error) {
	f.calls++
	f.gotQuery = query
	return f.results, f.err
}

func (f *fakeCatalog) Disabled() bool { return f.disabled }

// fakeInstaller is a scripted skillInstaller.
type fakeInstaller struct {
	summary InstallSummary
	err     error
	calls   int
	gotRepo string
	gotName string
}

func (f *fakeInstaller) Install(_ context.Context, repoURL, skillName string) (InstallSummary, error) {
	f.calls++
	f.gotRepo = repoURL
	f.gotName = skillName
	if f.err != nil {
		return InstallSummary{}, f.err
	}
	return f.summary, nil
}

func mustResult(t *testing.T, r ToolResult, err error) ToolResult {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return r
}

// catalogOK runs actionCatalog and asserts no error, returning the result.
func catalogOK(t *testing.T, tool *SkillTool, ctx context.Context, raw json.RawMessage) ToolResult {
	t.Helper()
	r, err := tool.actionCatalog(ctx, raw)
	return mustResult(t, r, err)
}

// withToolCallCtx returns a ctx carrying the tool-call context NewResult requires
// (the catalog handler formats via NewResult). A large cap keeps the preview path.
func withToolCallCtx(t *testing.T) context.Context {
	t.Helper()
	return WithToolCallContext(context.Background(), "s", "tc", t.TempDir(), 1<<20)
}

// TestActionCatalogRanksAndEnabledByDefault asserts action=catalog returns the
// installs-ranked results and is enabled by default (D-12): a non-disabled catalog
// dials and the result lists the hits.
func TestActionCatalogRanksAndEnabledByDefault(t *testing.T) {
	cat := &fakeCatalog{results: []CatalogResult{
		{Name: "xlsx", Source: "anthropics/skills", Installs: 900},
		{Name: "csv", Source: "owner/csv", Installs: 100},
	}}
	tool := &SkillTool{Catalog: cat}

	ctx := withToolCallCtx(t)
	raw, _ := json.Marshal(map[string]any{"action": "catalog", "query": "spreadsheet"})
	res := catalogOK(t, tool, ctx, raw)

	if cat.calls != 1 || cat.gotQuery != "spreadsheet" {
		t.Errorf("catalog call = (%d, %q), want (1, spreadsheet)", cat.calls, cat.gotQuery)
	}
	if !strings.Contains(res.Preview, "xlsx") || !strings.Contains(res.Preview, "csv") {
		t.Errorf("catalog result missing hits: %q", res.Preview)
	}
	// installs-ranked order: xlsx (900) appears before csv (100).
	if strings.Index(res.Preview, "xlsx") > strings.Index(res.Preview, "csv") {
		t.Errorf("catalog not installs-ranked (xlsx should precede csv): %q", res.Preview)
	}
}

// TestActionCatalogDisabledReturnsGuidance asserts a disabled catalog (D-12 escape
// hatch) returns enable-guidance WITHOUT dialing.
func TestActionCatalogDisabledReturnsGuidance(t *testing.T) {
	cat := &fakeCatalog{disabled: true}
	tool := &SkillTool{Catalog: cat}

	ctx := withToolCallCtx(t)
	raw, _ := json.Marshal(map[string]any{"action": "catalog", "query": "x"})
	res := catalogOK(t, tool, ctx, raw)

	if cat.calls != 0 {
		t.Errorf("disabled catalog must not dial, calls=%d", cat.calls)
	}
	if !strings.Contains(res.Preview, "disabled") {
		t.Errorf("want disabled-guidance, got %q", res.Preview)
	}
}

// TestActionCatalogEmptyQuery asserts an empty query is guarded before dialing.
func TestActionCatalogEmptyQuery(t *testing.T) {
	cat := &fakeCatalog{}
	tool := &SkillTool{Catalog: cat}
	raw, _ := json.Marshal(map[string]any{"action": "catalog", "query": "  "})
	_, err := tool.actionCatalog(withToolCallCtx(t), raw)
	if err == nil {
		t.Fatal("empty query: want error")
	}
	if cat.calls != 0 {
		t.Errorf("empty query must not dial, calls=%d", cat.calls)
	}
}

// TestActionInstallPausesAndSurfacesRedFlags asserts action=install lands pending via
// the installer seam and PAUSES the turn (the *ErrAwaitingUserInput sentinel) with the
// red flags surfaced in the question — the model never activates (D-03).
func TestActionInstallPausesAndSurfacesRedFlags(t *testing.T) {
	inst := &fakeInstaller{summary: InstallSummary{
		Name:           "xlsx",
		ContentHash:    "sha256:abc",
		AlwaysStripped: true,
		RedFlags:       []string{"frontmatter metadata declares an install[] block", "2 bundled executable file(s)"},
		Tier:           "risky",
	}}
	tool := &SkillTool{Installer: inst}

	raw, _ := json.Marshal(map[string]any{"action": "install", "repo": "https://github.com/anthropics/skills"})
	_, err := tool.actionInstall(context.Background(), raw)

	var pause *ErrAwaitingUserInput
	if !errors.As(err, &pause) {
		t.Fatalf("install: want *ErrAwaitingUserInput pause, got %v", err)
	}
	if pause.Kind != KindApproval {
		t.Errorf("pause kind = %q, want %q", pause.Kind, KindApproval)
	}
	if inst.calls != 1 || inst.gotRepo != "https://github.com/anthropics/skills" {
		t.Errorf("installer call = (%d, %q)", inst.calls, inst.gotRepo)
	}
	// The red flags + the always-strip note must be surfaced in the gate question (D-13).
	if !strings.Contains(pause.Question, "install[] block") {
		t.Errorf("red flag not surfaced in gate: %q", pause.Question)
	}
	if !strings.Contains(pause.Question, "bundled executable") {
		t.Errorf("bundled-exec red flag not surfaced: %q", pause.Question)
	}
	if !strings.Contains(pause.Question, "always:true") && !strings.Contains(pause.Question, "always-on") {
		t.Errorf("always-strip note not surfaced: %q", pause.Question)
	}
}

// TestActionInstallNoRedFlags asserts a clean install still pauses (gated) and notes
// that no red flags were found.
func TestActionInstallNoRedFlags(t *testing.T) {
	inst := &fakeInstaller{summary: InstallSummary{Name: "clean", Tier: "risky"}}
	tool := &SkillTool{Installer: inst}
	raw, _ := json.Marshal(map[string]any{"action": "install", "repo": "https://example.com/clean.git"})
	_, err := tool.actionInstall(context.Background(), raw)
	var pause *ErrAwaitingUserInput
	if !errors.As(err, &pause) {
		t.Fatalf("want pause, got %v", err)
	}
	if !strings.Contains(pause.Question, "No supply-chain red flags") {
		t.Errorf("clean install should note no red flags: %q", pause.Question)
	}
}

// TestActionInstallErrorIsToolError asserts an installer error (blocklist hit, missing
// SKILL.md, unreachable repo) surfaces as a tool ERROR (self-correct), NOT a pause.
func TestActionInstallErrorIsToolError(t *testing.T) {
	inst := &fakeInstaller{err: errors.New("no SKILL.md found in cloned repo")}
	tool := &SkillTool{Installer: inst}
	raw, _ := json.Marshal(map[string]any{"action": "install", "repo": "https://example.com/empty.git"})
	_, err := tool.actionInstall(context.Background(), raw)
	if err == nil {
		t.Fatal("install error: want a tool error")
	}
	var pause *ErrAwaitingUserInput
	if errors.As(err, &pause) {
		t.Fatal("install error must be a tool error, NOT a pause")
	}
}

// TestActionInstallRequiresRepo asserts a missing repo is a tool error before any
// installer call.
func TestActionInstallRequiresRepo(t *testing.T) {
	inst := &fakeInstaller{}
	tool := &SkillTool{Installer: inst}
	raw, _ := json.Marshal(map[string]any{"action": "install"})
	_, err := tool.actionInstall(context.Background(), raw)
	if err == nil {
		t.Fatal("missing repo: want error")
	}
	if inst.calls != 0 {
		t.Errorf("installer must not be called on missing repo, calls=%d", inst.calls)
	}
}

// TestInstallCatalogRoutedThroughExecute asserts the router dispatches install +
// catalog (the 11-02 notYetWired placeholders are replaced) — action=install reaches
// the installer and pauses; action=catalog reaches the catalog.
func TestInstallCatalogRoutedThroughExecute(t *testing.T) {
	inst := &fakeInstaller{summary: InstallSummary{Name: "x", Tier: "risky"}}
	cat := &fakeCatalog{results: []CatalogResult{{Name: "x", Installs: 1}}}
	tool := &SkillTool{Installer: inst, Catalog: cat}

	// install via Execute → pause
	rawI, _ := json.Marshal(map[string]any{"action": "install", "repo": "https://x/y.git"})
	_, err := tool.Execute(context.Background(), rawI)
	var pause *ErrAwaitingUserInput
	if !errors.As(err, &pause) {
		t.Fatalf("Execute(install): want pause, got %v", err)
	}
	if inst.calls != 1 {
		t.Errorf("installer not reached via Execute, calls=%d", inst.calls)
	}

	// catalog via Execute → result (needs the tool-call ctx for NewResult)
	rawC, _ := json.Marshal(map[string]any{"action": "catalog", "query": "x"})
	if _, err := tool.Execute(withToolCallCtx(t), rawC); err != nil {
		t.Fatalf("Execute(catalog): %v", err)
	}
	if cat.calls != 1 {
		t.Errorf("catalog not reached via Execute, calls=%d", cat.calls)
	}
}
