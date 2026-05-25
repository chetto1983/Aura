package tools_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/sandbox"
)

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

// TestExecuteShellTool_HeredocBacktickInjectionRegression replays the exact
// command from production conversation turn 342 (chat 1148481707 on 2026-05-25)
// where an unquoted `<<EOF` heredoc containing markdown backticks caused the
// shell to perform command substitution on `file` and `execute_shell`, writing
// the empty substitution result into AGENT.md and silently corrupting the
// overlay file. The fix auto-quotes the heredoc delimiter (`<<EOF` -> `<<'EOF'`)
// so the body is treated literally.
func TestExecuteShellTool_HeredocBacktickInjectionRegression(t *testing.T) {
	executor := &fakeCommandExecutor{result: &sandbox.Result{OK: true, ExitCode: 0, ElapsedMs: 5}}
	tool := tools.NewExecuteShellTool(executor)
	badCommand := "cat <<EOF >> AGENT.md\n\n## Operational Constraints\n- **CRITICAL:** Never use the `file` tool. All file operations (read, write, search, patch, list, etc.) must be performed via `execute_shell`.\nEOF\n"
	if _, err := tool.Execute(context.Background(), map[string]any{"command": badCommand}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(executor.command, "<<'EOF'") {
		t.Fatalf("heredoc delimiter not auto-quoted; backticks would expand:\n%s", executor.command)
	}
	if strings.Contains(executor.command, "<<EOF\n") || strings.Contains(executor.command, "<<EOF ") || strings.HasSuffix(executor.command, "<<EOF") {
		t.Fatalf("unquoted <<EOF still present:\n%s", executor.command)
	}
}

func TestExecuteShellTool_LeavesAlreadyQuotedHeredoc(t *testing.T) {
	executor := &fakeCommandExecutor{result: &sandbox.Result{OK: true, ExitCode: 0, ElapsedMs: 1}}
	tool := tools.NewExecuteShellTool(executor)
	already := "cat <<'EOF' >> x\nfoo\nEOF\n"
	if _, err := tool.Execute(context.Background(), map[string]any{"command": already}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if executor.command != already {
		t.Fatalf("already-quoted heredoc was rewritten:\n got=%q\nwant=%q", executor.command, already)
	}
}

func TestExecuteShellTool_QuotesDashedHeredoc(t *testing.T) {
	executor := &fakeCommandExecutor{result: &sandbox.Result{OK: true, ExitCode: 0, ElapsedMs: 1}}
	tool := tools.NewExecuteShellTool(executor)
	cmd := "cat <<-END\n\tfoo\nEND\n"
	if _, err := tool.Execute(context.Background(), map[string]any{"command": cmd}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(executor.command, "<<-'END'") {
		t.Fatalf("dashed heredoc not quoted as <<-'END':\n%s", executor.command)
	}
}

// TestExecuteShellTool_DemotesExit0WhenStderrShowsShellError replays the second
// half of the turn 342 incident: the shell exits 0 because `cat` succeeded,
// but stderr contains the `/bin/sh: 1: execute_shell: not found` line that
// proves the embedded command substitution failed. The fix demotes this to
// a tool-level error so the LLM does not believe the operation succeeded.
func TestExecuteShellTool_DemotesExit0WhenStderrShowsShellError(t *testing.T) {
	executor := &fakeCommandExecutor{result: &sandbox.Result{
		OK:       true,
		ExitCode: 0,
		Stdout:   "",
		Stderr:   "Usage: file [-bcCdEhikLlNnprsSvzZ0]\n/bin/sh: 1: execute_shell: not found\n",
	}}
	tool := tools.NewExecuteShellTool(executor)
	_, err := tool.Execute(context.Background(), map[string]any{"command": "echo hi"})
	if err == nil {
		t.Fatal("expected shell-failure detection to demote exit=0 to error")
	}
	if !strings.Contains(err.Error(), "execute_shell: not found") {
		t.Fatalf("error did not surface the shell failure marker: %v", err)
	}
}

func TestExecuteShellTool_AllowsExit0WithBenignStderr(t *testing.T) {
	executor := &fakeCommandExecutor{result: &sandbox.Result{
		OK:       true,
		ExitCode: 0,
		Stdout:   "hello\n",
		Stderr:   "warning: deprecated option used\n",
	}}
	tool := tools.NewExecuteShellTool(executor)
	out, err := tool.Execute(context.Background(), map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatalf("benign stderr must not demote to error: %v", err)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "deprecated") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestExecuteShellTool_DetectsBashLineFailureMarker(t *testing.T) {
	executor := &fakeCommandExecutor{result: &sandbox.Result{
		OK:       true,
		ExitCode: 0,
		Stderr:   "bash: line 3: missing_cmd: command not found\n",
	}}
	tool := tools.NewExecuteShellTool(executor)
	if _, err := tool.Execute(context.Background(), map[string]any{"command": "do_thing"}); err == nil {
		t.Fatal("expected bash 'line N:' marker to demote exit=0 to error")
	}
}

func TestExecuteShellToolNotRegisteredForNonProcessManager(t *testing.T) {
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

// TestExecuteShellTool_DefinitionIsDeferred verifies AC: execute_shell has
// VisibilityTier=VisibilityDeferred so it lands in the deferred section of
// RenderSplitManifest and is excluded from the always-on schema pool.
func TestExecuteShellTool_DefinitionIsDeferred(t *testing.T) {
	executor := &fakeCommandExecutor{result: &sandbox.Result{OK: true}}
	tool := tools.NewExecuteShellTool(executor)
	if tool == nil {
		t.Fatal("expected non-nil execute_shell tool")
	}
	def := tool.Definition()
	if !def.IsDeferred() {
		t.Fatalf("execute_shell must be Deferred; got VisibilityTier=%q", def.VisibilityTier)
	}
}
