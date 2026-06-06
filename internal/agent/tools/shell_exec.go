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
	// WorkspaceRoot is the default working directory when a call omits cwd and no
	// tracked cwd exists yet. Empty → the Aura process's current working directory.
	WorkspaceRoot string
	// DefaultTimeout caps a call that omits timeout_ms. Zero → defaultShellTimeout.
	DefaultTimeout time.Duration

	// mu guards cwd: the per-session PERSISTENT working directory (Claude-Code
	// Bash-tool parity — a `cd` in one call carries into the next). Keyed by the
	// tool-call session id from WithToolCallContext ("" for bare-ctx callers). The
	// tracked dir is the shell's final $PWD, captured via the cwd marker on POSIX
	// shells; the cmd.exe fallback does not track (degraded mode, documented).
	mu  sync.Mutex
	cwd map[string]string
}

type shellExecArgs struct {
	Command   string            `json:"command"`
	Cwd       string            `json:"cwd"`
	TimeoutMs int64             `json:"timeout_ms"`
	Env       map[string]string `json:"env"`
}

type shellExecFooter struct {
	ExitCode   *int   `json:"exit_code,omitempty"`
	Cwd        string `json:"cwd"`
	DurationMS int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out"`
}

const defaultShellTimeout = 120 * time.Second

func (s *ShellExec) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "The shell command line to run, e.g. \"ls -la\", \"python3 script.py\", \"git status\". Runs through the host system shell, so pipes, redirects, and && chains all work. For long scripts, create the file with fs_write first, then run it here."},
    "cwd": {"type": "string", "description": "Optional working directory override. Your working directory PERSISTS between calls (a cd carries over) and starts at your workspace."},
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
			"Your working directory persists between calls (a cd carries over) and starts at your workspace. " +
			"Returns combined stdout and stderr plus a final [aura_shell {...}] JSON footer with exit_code, cwd, duration_ms, and timed_out; rely on that footer instead of spending separate pwd or exit-code calls. " +
			"For running untrusted or model-generated code in isolation, use sandbox_exec instead.",
		Parameters: params,
		// NOT deferred: this is the keystone tool — the model must always see it and
		// its argument schema, exactly like sandbox_exec.
		Deferred: false,
		// Conservatively Mutating (D-43): a command line can write files or mutate
		// state and the agent cannot tell `ls` from `python build.py` statically.
		Mutating: true,
	}
}

func (s *ShellExec) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a shellExecArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		// Truncated args are almost always the output-token budget cutting a giant
		// command mid-JSON (observed live: a one-shot python script carrying all its
		// data). The hint steers the model to the incremental pattern instead of
		// retrying the same oversized call (D-15 self-correction).
		return ToolResult{}, fmt.Errorf("shell_exec args: %w — your arguments were likely truncated by the output budget; "+
			"put large or multi-line content in files with fs_write/fs_edit, then run the file here", err)
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
	started := time.Now()

	// Models occasionally emit CRLF line endings inside command; under the POSIX
	// shell a stray \r corrupts heredoc terminators and the cwd-tracking wrap
	// (live run 9, amendment #53 / D-42). Normalizing is always safe: no POSIX or
	// cmd.exe construct needs a literal CR inside a command line.
	command := strings.ReplaceAll(a.Command, "\r\n", "\n")
	posix := !shellIsCmdFallback()
	if posix {
		command = wrapForCwdTracking(command)
	}
	name, args := shellInvocation(command)
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = s.workdir(ctx, a.Cwd)
	cmd.Env = mergeEnv(a.Env)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	out := stdout.String()
	finalCwd := cmd.Dir
	if posix {
		var capturedCwd string
		out, capturedCwd = extractCwdMarker(out)
		if capturedCwd != "" {
			finalCwd = capturedCwd
		}
		s.storeCwd(ctx, capturedCwd)
	}
	ecPtr := exitCodePtr(runErr, runCtx.Err())
	rendered := renderShellOutput(out, stderr.String(), runErr, runCtx.Err())
	rendered = appendShellFooter(rendered, shellExecFooter{
		ExitCode:   ecPtr,
		Cwd:        finalCwd,
		DurationMS: time.Since(started).Milliseconds(),
		TimedOut:   errors.Is(runCtx.Err(), context.DeadlineExceeded),
	})
	res, err := NewResult(ctx, rendered)
	if err != nil {
		return ToolResult{}, err
	}
	meta := ToolResultMeta{
		"cwd":       finalCwd,
		"timed_out": errors.Is(runCtx.Err(), context.DeadlineExceeded),
	}
	if ecPtr != nil {
		meta["exit_code"] = *ecPtr
	}
	res.Meta = &meta
	return res, nil
}

// workdir resolves the call's starting directory: explicit cwd arg > the session's
// tracked cwd (Bash-tool parity) > WorkspaceRoot. A tracked dir that no longer
// exists (or a POSIX-only form a degraded shell stored) is skipped, never fatal.
func (s *ShellExec) workdir(ctx context.Context, cwd string) string {
	if strings.TrimSpace(cwd) != "" {
		return cwd
	}
	s.mu.Lock()
	tracked := s.cwd[shellSessionKey(ctx)]
	s.mu.Unlock()
	if tracked != "" {
		if _, err := os.Stat(tracked); err == nil {
			return tracked
		}
	}
	// Empty is valid — exec falls back to the Aura process's current directory.
	return s.WorkspaceRoot
}

// storeCwd records the shell's final $PWD as the session's tracked cwd. Empty
// (marker missing — e.g. the command exited the group early, or a timeout kill)
// keeps the previous tracking.
func (s *ShellExec) storeCwd(ctx context.Context, dir string) {
	if dir == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cwd == nil {
		s.cwd = map[string]string{}
	}
	s.cwd[shellSessionKey(ctx)] = dir
}

// shellSessionKey scopes cwd tracking per conversation/session: the session id
// from WithToolCallContext, "" for bare-ctx callers (unit tests, one-shot CLIs).
func shellSessionKey(ctx context.Context) string {
	if tc, ok := toolCallCtx(ctx); ok {
		return tc.sessionID
	}
	return ""
}

// cwdMarker fences the shell's final $PWD on the last stdout line so Execute can
// track cwd across calls. extractCwdMarker strips it before the model sees output.
const cwdMarker = "__AURA_CWD__"

// wrapForCwdTracking groups the user command, preserves its exit code, and prints
// the final working directory behind the marker. `pwd -W` (Git Bash) yields a
// Windows-valid path so the next call's cmd.Dir works; plain pwd is the POSIX
// fallback (a non-Windows-valid form is skipped by the workdir Stat guard).
func wrapForCwdTracking(command string) string {
	return "{\n" + command + "\n}\n__aura_ec=$?\nprintf '\\n%s %s\\n' '" + cwdMarker + "' \"$(pwd -W 2>/dev/null || pwd)\"\nexit $__aura_ec"
}

// extractCwdMarker splits the marker line back out of stdout: returns the cleaned
// output (the marker's injected leading newline removed) and the captured dir
// ("" when the marker never printed).
func extractCwdMarker(stdout string) (clean, dir string) {
	idx := strings.LastIndex(stdout, cwdMarker+" ")
	if idx < 0 {
		return stdout, ""
	}
	tail := stdout[idx+len(cwdMarker)+1:]
	if nl := strings.IndexByte(tail, '\n'); nl >= 0 {
		tail = tail[:nl]
	}
	clean = stdout[:idx]
	clean = strings.TrimSuffix(clean, "\n") // the printf-injected separator, not user output
	return clean, strings.TrimSpace(tail)
}

// shellIsCmdFallback reports whether the resolved shell is the degraded cmd.exe
// fallback (no POSIX bash found): no cwd tracking there — cmd has no brace
// grouping and its cd semantics differ.
func shellIsCmdFallback() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	name, _ := windowsShell()
	base := strings.ToLower(name)
	return base == "cmd" || strings.HasSuffix(base, `\cmd.exe`)
}

// windowsShell resolves the best available shell on Windows ONCE: a POSIX bash
// gives Claude-Code Bash-tool parity — the quoting, heredocs, pipes, and `~`
// expansion the model writes by training prior. Git Bash's KNOWN locations are
// preferred over a PATH lookup: build toolchains (w64devkit/MSYS busybox) often
// shadow PATH with a stripped ash that lacks `pwd -W` (cwd tracking) and
// coreutils. cmd.exe is the degraded fallback only: it mangles POSIX quoting
// ("unterminated string literal" on python -c) and caps the command line at ~8K —
// both observed breaking the live xlsx North-Star run (amendment #52 / D-41).
var windowsShell = sync.OnceValues(func() (string, string) {
	for _, p := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p, "-c"
		}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p, "-c"
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

func exitCodePtr(runErr, ctxErr error) *int {
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		return nil
	}
	if ec, ok := exitCode(runErr); ok {
		return &ec
	}
	if runErr == nil {
		ec := 0
		return &ec
	}
	return nil
}

func appendShellFooter(output string, footer shellExecFooter) string {
	raw, err := json.Marshal(footer)
	if err != nil {
		return output
	}
	var b strings.Builder
	b.WriteString(output)
	ensureTrailingNewline(&b)
	b.WriteString("[aura_shell ")
	b.Write(raw)
	b.WriteByte(']')
	return b.String()
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
