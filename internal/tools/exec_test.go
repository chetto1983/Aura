package tools_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/aura/aura/internal/sandbox"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/tools"
)

func TestExecuteCodeTool_NilManager(t *testing.T) {
	tool := tools.NewExecuteCodeTool(nil)
	if tool != nil {
		t.Fatal("expected nil tool when manager is nil")
	}
}

func TestExecuteCodeTool_DescriptionDefersSimpleDocumentsToTypedTools(t *testing.T) {
	manager, err := sandbox.NewManager(sandbox.Config{
		Runtime: fakeExecRuntime{result: &sandbox.Result{OK: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := tools.NewExecuteCodeTool(manager)
	desc := tool.Description()
	for _, want := range []string{"Use create_xlsx/create_docx/create_pdf", "for simple documents", "/tmp/aura_out", "computed artifacts"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q:\n%s", want, desc)
		}
	}
}

type fakeSandboxExecutor struct {
	result *sandbox.Result
	code   string
}

func (f *fakeSandboxExecutor) Execute(_ context.Context, code string, _ bool) (*sandbox.Result, error) {
	f.code = code
	return f.result, nil
}

func TestExecuteCodeToolAcceptsExecutorInterface(t *testing.T) {
	executor := &fakeSandboxExecutor{result: &sandbox.Result{OK: true, Stdout: "ok", ExitCode: 0, ElapsedMs: 1}}
	tool := tools.NewExecuteCodeTool(executor)
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

type fakeCommandExecutor struct {
	result  *sandbox.Result
	command string
}

func (f *fakeCommandExecutor) ExecuteCommand(_ context.Context, command string, _ bool) (*sandbox.Result, error) {
	f.command = command
	return f.result, nil
}

func TestExecuteShellToolAcceptsCommandExecutorInterface(t *testing.T) {
	executor := &fakeCommandExecutor{result: &sandbox.Result{OK: true, Stdout: "shell ok\n", ExitCode: 0, ElapsedMs: 2}}
	tool := tools.NewExecuteShellTool(executor)
	if tool == nil {
		t.Fatal("expected execute_shell tool")
	}
	out, err := tool.Execute(context.Background(), map[string]any{"command": "echo shell ok"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if executor.command != "echo shell ok" || !strings.Contains(out, "shell ok") {
		t.Fatalf("command=%q out=%q", executor.command, out)
	}
}

func TestExecuteShellToolNotRegisteredForPyodideManager(t *testing.T) {
	manager, err := sandbox.NewManager(sandbox.Config{
		Runtime: fakeExecRuntime{result: &sandbox.Result{OK: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tool := tools.NewExecuteShellTool(manager); tool != nil {
		t.Fatal("expected execute_shell to stay disabled for non-process runtimes")
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
	tool := tools.NewExecuteCodeToolWithSender(manager, sender)
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
	tool := tools.NewExecuteCodeToolWithStore(manager, sender, store)
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
	tool := tools.NewExecuteCodeToolWithStore(manager, nil, store)
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

type fakeExecRuntime struct {
	result *sandbox.Result
	err    error
}

func (r fakeExecRuntime) Kind() sandbox.RuntimeKind { return sandbox.RuntimeKindPyodide }

func (r fakeExecRuntime) Execute(context.Context, string, bool) (*sandbox.Result, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.result, nil
}

func (fakeExecRuntime) CheckAvailability() sandbox.Availability {
	return sandbox.Availability{Available: true, Kind: sandbox.RuntimeKindPyodide, Detail: "ok"}
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
