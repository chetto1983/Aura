package tools

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestShellExecSpecIsNotDeferred(t *testing.T) {
	s := (&ShellExec{}).Spec()
	if s.Name != "shell_exec" {
		t.Fatalf("name = %q, want shell_exec", s.Name)
	}
	// Keystone tool: the model must always see it + its arg schema (like sandbox_exec).
	if s.Deferred {
		t.Fatal("shell_exec must NOT be deferred — the model needs the arg schema")
	}
}

func TestShellExecRunsCommand(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"echo hello-aura"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "hello-aura") {
		t.Fatalf("preview missing stdout: %q", res.Preview)
	}
}

func TestShellExecReportsExitCode(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")

	// `exit 7` runs identically under `/bin/sh -c` and `cmd /c`.
	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"exit 7"}`))
	if err != nil {
		t.Fatalf("a failing command is a normal result, not a Go error: %v", err)
	}
	if !strings.Contains(res.Preview, "[exit code 7]") {
		t.Fatalf("preview missing exit-code marker: %q", res.Preview)
	}
}

func TestShellExecPassesEnv(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")

	cmd := `echo "$AURA_SHELL_TEST"`
	if runtime.GOOS == "windows" {
		cmd = "echo %AURA_SHELL_TEST%"
	}
	raw, _ := json.Marshal(shellExecArgs{Command: cmd, Env: map[string]string{"AURA_SHELL_TEST": "marker-42"}})

	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "marker-42") {
		t.Fatalf("env not passed through to command: %q", res.Preview)
	}
}

func TestShellExecHonorsCwd(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")
	dir := t.TempDir()

	cmd := "pwd"
	if runtime.GOOS == "windows" {
		cmd = "cd"
	}
	raw, _ := json.Marshal(shellExecArgs{Command: cmd, Cwd: dir})

	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, filepath.Base(dir)) {
		t.Fatalf("cwd %q not honored, output: %q", dir, res.Preview)
	}
}

func TestShellExecTimesOut(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")

	cmd := "sleep 3"
	if runtime.GOOS == "windows" {
		cmd = "ping -n 4 127.0.0.1"
	}
	raw, _ := json.Marshal(shellExecArgs{Command: cmd, TimeoutMs: 200})

	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("a timeout is a normal result, not a Go error: %v", err)
	}
	if !strings.Contains(res.Preview, "[command timed out]") {
		t.Fatalf("preview missing timeout marker: %q", res.Preview)
	}
}

func TestShellExecRejectsBadArgs(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")

	tests := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{name: "malformed JSON", raw: json.RawMessage(`{`), want: "shell_exec args"},
		{name: "empty command", raw: json.RawMessage(`{"command":"   "}`), want: "command is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tool.Execute(ctx, tt.raw); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want substring %q", err, tt.want)
			}
		})
	}
}
