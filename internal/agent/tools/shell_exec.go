package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ShellExec is Aura's keystone host tool: a full terminal. It runs a command line
// through the host system shell, in-process, with the operator's own privileges —
// the same power Claude Code's Bash tool has. There is no sandbox hop and no path
// fence: for a single trusted operator on their own machine the host shell IS the
// capability (amendment #50 / D-15c). Untrusted, model-generated code still has the
// deliberate sandbox_exec escalation.
type ShellExec struct {
	// WorkspaceRoot is the default working directory when a call omits cwd. Empty
	// → the Aura process's current working directory.
	WorkspaceRoot string
	// DefaultTimeout caps a call that omits timeout_ms. Zero → defaultShellTimeout.
	DefaultTimeout time.Duration
}

type shellExecArgs struct {
	Command   string            `json:"command"`
	Cwd       string            `json:"cwd"`
	TimeoutMs int64             `json:"timeout_ms"`
	Env       map[string]string `json:"env"`
}

const defaultShellTimeout = 120 * time.Second

func (s *ShellExec) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "The shell command line to run, e.g. \"ls -la\", \"python3 script.py\", \"git status\". Runs through the host system shell, so pipes, redirects, and && chains all work."},
    "cwd": {"type": "string", "description": "Optional working directory. Defaults to the workspace root."},
    "timeout_ms": {"type": "integer", "minimum": 0, "description": "Optional timeout in milliseconds. Omit for the default (120s)."},
    "env": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Optional extra environment variables for this command only."}
  },
  "required": ["command"]
}`)
	return Spec{
		Name:    "shell_exec",
		Summary: "Run a shell command on the host — a full terminal.",
		Description: "Run a command line through the host system shell, in-process, with full access to the machine — this is your primary way to get things done, like a real terminal. " +
			"Pipes, redirects, && chains, any installed interpreter (python, node, go), git, and filesystem work all just work. " +
			"Returns combined stdout and stderr, plus an exit-code marker when the command fails. " +
			"For running untrusted or model-generated code in isolation, use sandbox_exec instead.",
		Parameters: params,
		// NOT deferred: this is the keystone tool — the model must always see it and
		// its argument schema, exactly like sandbox_exec.
		Deferred: false,
	}
}

func (s *ShellExec) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a shellExecArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("shell_exec args: %w", err)
	}
	if strings.TrimSpace(a.Command) == "" {
		return ToolResult{}, fmt.Errorf("shell_exec: command is required")
	}

	timeout := s.DefaultTimeout
	if timeout <= 0 {
		timeout = defaultShellTimeout
	}
	if a.TimeoutMs > 0 {
		timeout = time.Duration(a.TimeoutMs) * time.Millisecond
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name, args := shellInvocation(a.Command)
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = s.workdir(a.Cwd)
	cmd.Env = mergeEnv(a.Env)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	return NewResult(ctx, renderShellOutput(stdout.String(), stderr.String(), runErr, runCtx.Err()))
}

func (s *ShellExec) workdir(cwd string) string {
	if strings.TrimSpace(cwd) != "" {
		return cwd
	}
	// Empty is valid — exec falls back to the Aura process's current directory.
	return s.WorkspaceRoot
}

// windowsShell resolves the best available shell on Windows ONCE: a POSIX bash
// (Git Bash / MSYS / w64devkit) gives Claude-Code Bash-tool parity — the quoting,
// heredocs, pipes, and `~` expansion the model writes by training prior. cmd.exe
// is the degraded fallback only: it mangles POSIX quoting ("unterminated string
// literal" on python -c) and caps the command line at ~8K — both observed breaking
// the live xlsx North-Star run (amendment #52 / D-41).
var windowsShell = sync.OnceValues(func() (string, string) {
	if p, err := exec.LookPath("bash"); err == nil {
		return p, "-c"
	}
	for _, p := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p, "-c"
		}
	}
	return "cmd", "/c"
})

// shellInvocation wraps the command line in the host system shell so pipes,
// redirects, and chains work. `-c` (not `-lc`) keeps it fast and predictable; the
// deployment sets PATH for the Aura process and the child inherits it.
func shellInvocation(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		name, flag := windowsShell()
		return name, []string{flag, command}
	}
	return "/bin/sh", []string{"-c", command}
}

// mergeEnv returns nil when there are no extras (nil → child inherits the Aura
// process environment unchanged); otherwise the process env plus the overrides.
func mergeEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return nil
	}
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func renderShellOutput(stdout, stderr string, runErr, ctxErr error) string {
	var b strings.Builder
	b.WriteString(stdout)
	if stderr != "" {
		ensureTrailingNewline(&b)
		b.WriteString(stderr)
	}
	switch {
	case errors.Is(ctxErr, context.DeadlineExceeded):
		appendStatus(&b, "[command timed out]")
	case runErr != nil:
		if ec, ok := exitCode(runErr); ok {
			appendStatus(&b, fmt.Sprintf("[exit code %d]", ec))
		} else {
			appendStatus(&b, fmt.Sprintf("[command failed: %v]", runErr))
		}
	}
	if b.Len() == 0 {
		return "[no output]"
	}
	return b.String()
}

func exitCode(err error) (int, bool) {
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return ee.ExitCode(), true
	}
	return 0, false
}

func appendStatus(b *strings.Builder, status string) {
	ensureTrailingNewline(b)
	b.WriteString(status)
}

func ensureTrailingNewline(b *strings.Builder) {
	if s := b.String(); s != "" && !strings.HasSuffix(s, "\n") {
		b.WriteByte('\n')
	}
}
