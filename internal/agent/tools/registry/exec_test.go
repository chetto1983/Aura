package tools_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/sandbox"
	"github.com/aura/aura/internal/storage/sources/store"
)

func TestExecuteCodeTool_NilManager(t *testing.T) {
	tool := tools.NewExecuteCodeToolWithStoreAndRegistry(nil, nil, nil, nil)
	if tool != nil {
		t.Fatal("expected nil tool when manager is nil")
	}
}

func TestExecuteCodeTool_DescriptionHasRequiredPhrases(t *testing.T) {
	manager, err := sandbox.NewManager(sandbox.Config{
		Runtime: fakeExecRuntime{result: &sandbox.Result{OK: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := tools.NewExecuteCodeToolWithStoreAndRegistry(manager, nil, nil, nil)
	desc := tool.Description()
	// These phrases are mandated by TestDescriptionAuditSpecificPhrases; keep in sync.
	for _, want := range []string{"Read-only stdout, capped, results are INTERNAL — synthesize before replying", "Ephemeral"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q:\n%s", want, desc)
		}
	}
}

type fakeSandboxExecutor struct {
	result *sandbox.Result
	code   string
}

type allowInternalToolAuthorizer struct{}

func (allowInternalToolAuthorizer) Authorize(_ context.Context, params identity.AuthorizeParams) (identity.AuthorizationDecision, error) {
	return identity.AuthorizationDecision{
		ActorID:    params.ActorID,
		Capability: params.Capability,
		Resource:   params.Resource,
		Decision:   identity.DecisionAllow,
		Reason:     "test_grant",
	}, nil
}

func authorizedInternalToolContext(ctx context.Context) context.Context {
	ctx = identity.WithAuthorizer(ctx, allowInternalToolAuthorizer{})
	return identity.WithActorID(ctx, "actor:test-internal-tool")
}

func (f *fakeSandboxExecutor) Execute(_ context.Context, code string, _ bool) (*sandbox.Result, error) {
	f.code = code
	return f.result, nil
}

func TestExecuteCodeToolAcceptsExecutorInterface(t *testing.T) {
	executor := &fakeSandboxExecutor{result: &sandbox.Result{OK: true, Stdout: "ok", ExitCode: 0, ElapsedMs: 1}}
	tool := tools.NewExecuteCodeToolWithStoreAndRegistry(executor, nil, nil, nil)
	if tool == nil {
		t.Fatal("expected execute_code tool")
	}
	out, err := tool.Execute(context.Background(), map[string]any{"code": "print('ok')"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if executor.code != "print('ok')" || !strings.Contains(out, "ok") {
		t.Fatalf("code=%q out=%q", executor.code, out)
	}
}


func TestExecuteCodeTool_DeliversArtifacts(t *testing.T) {
	manager, err := sandbox.NewManager(sandbox.Config{
		Runtime: fakeExecRuntime{result: &sandbox.Result{
			OK:        true,
			Stdout:    "created plot\n",
			ExitCode:  0,
			ElapsedMs: 11,
			Artifacts: []sandbox.Artifact{{
				Name:      "plot.png",
				MimeType:  "image/png",
				Bytes:     []byte("png-bytes"),
				SizeBytes: 9,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sender := &execArtifactSender{}
	tool := tools.NewExecuteCodeToolWithStoreAndRegistry(manager, sender, nil, nil)
	if tool == nil {
		t.Fatal("tool = nil")
	}

	out, err := tool.Execute(tools.WithUserID(context.Background(), "12345"), map[string]any{
		"code": "make plot",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(sender.sent))
	}
	got := sender.sent[0]
	if got.userID != "12345" || got.filename != "plot.png" || string(got.body) != "png-bytes" {
		t.Fatalf("sent = %+v", got)
	}
	if got.caption == "" {
		t.Fatal("caption is empty")
	}
	if !containsAll(out, "artifacts:", "plot.png", "delivered=true") {
		t.Fatalf("output = %q", out)
	}
}

func TestExecuteCodeTool_PersistsArtifactsAsSources(t *testing.T) {
	manager, err := sandbox.NewManager(sandbox.Config{
		Runtime: fakeExecRuntime{result: &sandbox.Result{
			OK:        true,
			Stdout:    "created csv\n",
			ExitCode:  0,
			ElapsedMs: 12,
			Artifacts: []sandbox.Artifact{{
				Name:      "metrics.csv",
				MimeType:  "text/csv",
				Bytes:     []byte("name,value\naura,1\n"),
				SizeBytes: int64(len("name,value\naura,1\n")),
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := source.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sender := &execArtifactSender{}
	tool := tools.NewExecuteCodeToolWithStoreAndRegistry(manager, sender, store, nil)
	if tool == nil {
		t.Fatal("tool = nil")
	}

	out, err := tool.Execute(tools.WithUserID(context.Background(), "12345"), map[string]any{
		"code": "make csv",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !containsAll(out, "artifacts:", "metrics.csv", "delivered=true", "persisted=true", "source_id=src_") {
		t.Fatalf("output = %q", out)
	}

	rows, err := store.List(source.ListFilter{Kind: source.KindSandboxArtifact})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("stored rows = %d, want 1", len(rows))
	}
	rec := rows[0]
	if rec.Filename != "metrics.csv" || rec.MimeType != "text/csv" || rec.Status != source.StatusIngested {
		t.Fatalf("stored source = %+v", rec)
	}
	if _, err := os.Stat(store.Path(rec.ID, "original.csv")); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteCodeTool_PersistedScriptArtifactIsReadableSource(t *testing.T) {
	script := "from pathlib import Path\nprint('AURA_SKILL_E2E script remembered')\n"
	manager, err := sandbox.NewManager(sandbox.Config{
		Runtime: fakeExecRuntime{result: &sandbox.Result{
			OK:        true,
			Stdout:    "created script and result\n",
			ExitCode:  0,
			ElapsedMs: 12,
			Artifacts: []sandbox.Artifact{{
				Name:      "aura_skill_e2e.py",
				MimeType:  "text/x-python",
				Bytes:     []byte(script),
				SizeBytes: int64(len(script)),
			}, {
				Name:      "aura_skill_e2e_result.md",
				MimeType:  "text/markdown",
				Bytes:     []byte("# Aura Skill E2E\n\n- Skill read: aura-python-sandbox\n- Network: disabled\n"),
				SizeBytes: int64(len("# Aura Skill E2E\n\n- Skill read: aura-python-sandbox\n- Network: disabled\n")),
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := source.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := tools.NewExecuteCodeToolWithStoreAndRegistry(manager, nil, store, nil)
	if tool == nil {
		t.Fatal("tool = nil")
	}

	out, err := tool.Execute(context.Background(), map[string]any{"code": "create e2e artifacts"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	ids := regexp.MustCompile(`source_id=(src_[a-f0-9]{16})`).FindAllStringSubmatch(out, -1)
	if len(ids) != 2 {
		t.Fatalf("output source ids = %v, want 2\n%s", ids, out)
	}

	read := tools.NewReadSourceTool(store)
	got, err := read.Execute(context.Background(), map[string]any{
		"source_id": ids[0][1],
		"mode":      "excerpt",
	})
	if err != nil {
		t.Fatalf("read persisted script source: %v", err)
	}
	if !strings.Contains(got, "AURA_SKILL_E2E script remembered") {
		t.Fatalf("read_source output = %q", got)
	}
}

func TestExecuteCodeTool_ExecutesInternalToolManifestWhenAllowed(t *testing.T) {
	executor := &fakeSandboxExecutor{result: &sandbox.Result{
		OK:        true,
		Stdout:    "script done\n",
		ExitCode:  0,
		ElapsedMs: 3,
		Artifacts: []sandbox.Artifact{{
			Name:      "aura_tool_calls.json",
			MimeType:  "application/json",
			Bytes:     []byte(`{"calls":[{"tool":"fake_internal","args":{"value":"x"}}]}`),
			SizeBytes: int64(len(`{"calls":[{"tool":"fake_internal","args":{"value":"x"}}]}`)),
		}},
	}}
	registry := tools.NewRegistry(nil)
	registry.Register(fakeInternalTool{})
	tool := tools.NewExecuteCodeToolWithStoreAndRegistry(executor, nil, nil, registry)
	if tool == nil {
		t.Fatal("tool = nil")
	}

	ctx := tools.WithAllowedToolNames(authorizedInternalToolContext(context.Background()), []string{"execute_code", "fake_internal"})
	out, err := tool.Execute(ctx, map[string]any{
		"code":          "emit manifest",
		"tools_allowed": []any{"fake_internal"},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !containsAll(out, "script done", "internal_tool_results:", `"tool": "fake_internal"`, `"ok": true`, "internal ok: x") {
		t.Fatalf("output = %q", out)
	}
	if strings.Contains(out, "artifacts:") || strings.Contains(out, "aura_tool_calls.json") {
		t.Fatalf("manifest leaked as user artifact:\n%s", out)
	}
}

func TestExecuteCodeTool_RejectsInternalToolNotAvailableInActiveTurn(t *testing.T) {
	executor := &fakeSandboxExecutor{result: &sandbox.Result{
		OK:        true,
		ExitCode:  0,
		ElapsedMs: 3,
		Artifacts: []sandbox.Artifact{{
			Name:  "aura_tool_calls.json",
			Bytes: []byte(`{"calls":[{"tool":"fake_internal","args":{"value":"x"}}]}`),
		}},
	}}
	registry := tools.NewRegistry(nil)
	registry.Register(fakeInternalTool{})
	tool := tools.NewExecuteCodeToolWithStoreAndRegistry(executor, nil, nil, registry)

	ctx := tools.WithAllowedToolNames(context.Background(), []string{"execute_code"})
	_, err := tool.Execute(ctx, map[string]any{
		"code":          "emit manifest",
		"tools_allowed": []any{"fake_internal"},
	})
	if err == nil || !strings.Contains(err.Error(), "not available in the active turn") {
		t.Fatalf("error = %v, want active-turn rejection", err)
	}
}

func TestExecuteCodeTool_RejectsInternalToolNotInToolsAllowed(t *testing.T) {
	executor := &fakeSandboxExecutor{result: &sandbox.Result{
		OK:        true,
		ExitCode:  0,
		ElapsedMs: 3,
		Artifacts: []sandbox.Artifact{{
			Name:  "aura_tool_calls.json",
			Bytes: []byte(`{"calls":[{"tool":"fake_internal","args":{"value":"x"}}]}`),
		}},
	}}
	registry := tools.NewRegistry(nil)
	registry.Register(fakeInternalTool{})
	tool := tools.NewExecuteCodeToolWithStoreAndRegistry(executor, nil, nil, registry)

	ctx := tools.WithAllowedToolNames(context.Background(), []string{"execute_code", "fake_internal", "other_tool"})
	_, err := tool.Execute(ctx, map[string]any{
		"code":          "emit manifest",
		"tools_allowed": []any{"other_tool"},
	})
	if err == nil || !strings.Contains(err.Error(), "was not listed in tools_allowed") {
		t.Fatalf("error = %v, want tools_allowed rejection", err)
	}
}

func TestExecuteCodeTool_ManifestArtifactIsNotPersistedOrDelivered(t *testing.T) {
	executor := &fakeSandboxExecutor{result: &sandbox.Result{
		OK:        true,
		Stdout:    "script done\n",
		ExitCode:  0,
		ElapsedMs: 4,
		Artifacts: []sandbox.Artifact{{
			Name:      "aura_tool_calls.json",
			MimeType:  "application/json",
			Bytes:     []byte(`{"calls":[{"tool":"fake_internal","args":{"value":"x"}}]}`),
			SizeBytes: int64(len(`{"calls":[{"tool":"fake_internal","args":{"value":"x"}}]}`)),
		}, {
			Name:      "plot.png",
			MimeType:  "image/png",
			Bytes:     []byte("png-bytes"),
			SizeBytes: 9,
		}},
	}}
	store, err := source.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sender := &execArtifactSender{}
	registry := tools.NewRegistry(nil)
	registry.Register(fakeInternalTool{})
	tool := tools.NewExecuteCodeToolWithStoreAndRegistry(executor, sender, store, registry)

	ctx := tools.WithAllowedToolNames(authorizedInternalToolContext(tools.WithUserID(context.Background(), "12345")), []string{"execute_code", "fake_internal"})
	out, err := tool.Execute(ctx, map[string]any{
		"code":          "emit manifest and plot",
		"tools_allowed": []any{"fake_internal"},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0].filename != "plot.png" {
		t.Fatalf("sent = %+v, want only plot.png", sender.sent)
	}
	if strings.Contains(out, "aura_tool_calls.json") || !strings.Contains(out, "plot.png") {
		t.Fatalf("output = %q", out)
	}
	rows, err := store.List(source.ListFilter{Kind: source.KindSandboxArtifact})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Filename != "plot.png" {
		t.Fatalf("stored rows = %+v, want only plot.png", rows)
	}
}

type fakeExecRuntime struct {
	result *sandbox.Result
	err    error
}

func (r fakeExecRuntime) Kind() sandbox.RuntimeKind { return sandbox.RuntimeKindUnavailable }

func (r fakeExecRuntime) Execute(context.Context, string, bool) (*sandbox.Result, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.result, nil
}

func (fakeExecRuntime) CheckAvailability() sandbox.Availability {
	return sandbox.Availability{Available: true, Kind: sandbox.RuntimeKindUnavailable, Detail: "ok"}
}

func (fakeExecRuntime) ValidateCode(string) error { return nil }

type execArtifactSender struct {
	sent []execArtifactSend
}

type execArtifactSend struct {
	userID   string
	filename string
	body     []byte
	caption  string
}

func (s *execArtifactSender) SendDocumentToUser(userID, filename string, body []byte, caption string) error {
	s.sent = append(s.sent, execArtifactSend{userID: userID, filename: filename, body: body, caption: caption})
	return nil
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}

// TestExecuteCodeTool_DefinitionIsDeferred verifies AC: execute_code has
// VisibilityTier=VisibilityDeferred so it lands in the deferred section of
// RenderSplitManifest and is excluded from the always-on schema pool.
func TestExecuteCodeTool_DefinitionIsDeferred(t *testing.T) {
	executor := &fakeSandboxExecutor{result: &sandbox.Result{OK: true}}
	tool := tools.NewExecuteCodeToolWithStoreAndRegistry(executor, nil, nil, nil)
	if tool == nil {
		t.Fatal("expected non-nil execute_code tool")
	}
	def := tool.Definition()
	if !def.IsDeferred() {
		t.Fatalf("execute_code must be Deferred; got VisibilityTier=%q", def.VisibilityTier)
	}
}

// TestRenderSplitManifest_ExecuteToolsInDeferredSection is the manifest probe
// AC: with real execute_code and execute_shell definitions, both tools appear
// in the deferred section and NOT in the always-on catalog.
func TestRenderSplitManifest_ExecuteToolsInDeferredSection(t *testing.T) {
	codeExec := &fakeSandboxExecutor{result: &sandbox.Result{OK: true}}
	codeTool := tools.NewExecuteCodeToolWithStoreAndRegistry(codeExec, nil, nil, nil)
	shellExec := &fakeCommandExecutor{result: &sandbox.Result{OK: true}}
	shellTool := tools.NewExecuteShellTool(shellExec)

	defs := []tools.ToolDefinition{codeTool.Definition(), shellTool.Definition()}
	got := tools.RenderSplitManifest(defs)

	const deferredHeader = "## Deferred tools (call tool_search to load schema)"
	if !strings.Contains(got, deferredHeader) {
		t.Fatalf("deferred section header missing in manifest:\n%s", got)
	}
	deferredStart := strings.Index(got, deferredHeader)
	deferredSection := got[deferredStart:]
	for _, name := range []string{"execute_code", "execute_shell"} {
		if !strings.Contains(deferredSection, "- "+name) {
			t.Errorf("tool %q missing from deferred section:\n%s", name, deferredSection)
		}
		if deferredStart > 0 && strings.Contains(got[:deferredStart], "- "+name) {
			t.Errorf("tool %q leaked into always-on section:\n%s", name, got[:deferredStart])
		}
	}
}

type fakeInternalTool struct{}

func (fakeInternalTool) Name() string { return "fake_internal" }

func (fakeInternalTool) Description() string { return "fake internal tool" }

func (fakeInternalTool) Parameters() map[string]any { return map[string]any{"type": "object"} }

func (fakeInternalTool) Execute(_ context.Context, args map[string]any) (string, error) {
	value, _ := args["value"].(string)
	return "internal ok: " + value, nil
}
