package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/secret"
)

// ShellExec is Aura's keystone host tool: a full terminal. It runs a command line
// through the host system shell, in-process, with the operator's own privileges —
// the same power Claude Code's Bash tool has. There is no sandbox hop and no path
// fence: for a single trusted operator on their own machine the host shell IS the
// capability (amendment #50 / D-15c). For a long job, "background": true returns a
// shell_id read via shell_poll and stopped via shell_kill.
type ShellExec struct {
	// WorkspaceRoot is the default working directory when a call omits cwd and no
	// tracked cwd exists yet. Empty → the Aura process's current working directory.
	WorkspaceRoot string
	// DefaultTimeout caps a call that omits timeout_ms. Zero → defaultShellTimeout.
	DefaultTimeout time.Duration

	// Background, when set, is the shared registry that holds jobs started with
	// "background": true so shell_poll/shell_kill (wired to the same registry) can
	// read and stop them across turns. Nil → background mode is unavailable.
	Background *BackgroundShells

	// Approvals is the one-shot ledger for commands matching
	// AURA_SHELL_DESTRUCTIVE_PATTERNS. Nil fails closed for configured matches.
	Approvals *ShellApprovals

	// Router is the per-identity box routing seam (SBX-01, plan 37-07). Nil (dev/local_trusted,
	// the CLI/manifest paths) keeps the host os/exec path byte-for-byte; under a strict profile
	// Route returns a live box handle and Execute runs the command INSIDE the box via
	// Router.Exec (never host), failing CLOSED on a box error (D-09/GATE-01).
	Router *usersandbox.SandboxRouter

	// mu guards cwd: the per-session PERSISTENT working directory (Claude-Code
	// Bash-tool parity — a `cd` in one call carries into the next). Keyed by the
	// tool-call session id from WithToolCallContext ("" for bare-ctx callers). The
	// tracked dir is the shell's final $PWD, captured via the cwd marker on POSIX
	// shells; the cmd.exe fallback does not track (degraded mode, documented).
	mu  sync.Mutex
	cwd map[string]string
}

type shellExecArgs struct {
	Command    string            `json:"command"`
	Cwd        string            `json:"cwd"`
	TimeoutMs  int64             `json:"timeout_ms"`
	Env        map[string]string `json:"env"`
	Background bool              `json:"background"`
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
    "env": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Optional extra environment variables for this command only."},
    "background": {"type": "boolean", "description": "Run the command in the background and return a shell_id immediately instead of blocking. Read its output later with shell_poll and stop it with shell_kill. Use for long jobs (builds, downloads, dev servers)."}
  },
  "required": ["command"]
}`)
	return Spec{
		Name:    "shell_exec",
		Summary: "Run a shell command on the host — a full terminal.",
		Description: "Run a command line through the host system shell, in-process, with full access to the machine — use it for local commands, builds, scripts, and glue work that dedicated tools do not cover. " +
			"Do NOT reach for it when a dedicated tool fits: to read, search, or write files use fs_read / fs_grep / fs_glob and fs_write / fs_edit (they return structured results and page large files instead of flooding context); to get current web facts like a price, the weather, or today's news use the dedicated web search/fetch tools (load them with tool_search if they are not in your list). Reaching for the shell because the specific tool is not visible is the most common mistake. " +
			"Pipes, redirects, && chains, any installed interpreter (python, node, go), git, and filesystem work all just work. " +
			"Your working directory persists between calls (a cd carries over) and starts at your workspace. " +
			"Returns combined stdout and stderr plus a final [aura_shell {...}] JSON footer with exit_code, cwd, duration_ms, and timed_out; rely on that footer instead of spending separate pwd or exit-code calls. " +
			"For a long-running job set \"background\": true — it returns immediately with a shell_id you read with shell_poll and stop with shell_kill.",
		Parameters: params,
		// Deferred: the full terminal stays available through tool_search, but its
		// large, permissive schema should not dominate the hot manifest for simple
		// chat/web tasks.
		Deferred: true,
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

	// Models occasionally emit CRLF line endings inside command; under the POSIX
	// shell a stray \r corrupts heredoc terminators and the cwd-tracking wrap
	// (live run 9, amendment #53 / D-42). Normalize once and use the same command
	// for destructive matching, approval digesting, and execution.
	commandForGate := strings.ReplaceAll(a.Command, "\r\n", "\n")
	workdir := s.workdir(ctx, a.Cwd)

	// Route decision (SBX-01, plan 37-07). routed=false ⇒ the host os/exec path below runs
	// unchanged (dev/local_trusted, SC-4); routed=true+err ⇒ fail-CLOSED deny (never host,
	// D-09/GATE-01); routed=true ⇒ the command runs INSIDE the box (executeInBox).
	boxHandle, routed, routeErr := s.Router.Route(ctx)
	if routed && routeErr != nil {
		return sandboxUnavailableResult("shell_exec", routeErr), nil
	}

	// AG-018: validate an explicitly model-supplied cwd up front so a bad directory
	// is a clean, self-correctable error rather than an opaque shell exec failure. HOST ONLY —
	// on the routed branch a `/workspace/...` cwd is a BOX path, not a host path, so host-stat'ing
	// it would falsely fail; the box exec surfaces its own bad-dir error instead.
	if !routed {
		if explicit := strings.TrimSpace(a.Cwd); explicit != "" {
			if info, statErr := os.Stat(explicit); statErr != nil {
				return ToolResult{}, fmt.Errorf("shell_exec: cwd %q is not accessible: %w", explicit, statErr)
			} else if !info.IsDir() {
				return ToolResult{}, fmt.Errorf("shell_exec: cwd %q is not a directory", explicit)
			}
		}
	}
	approvalRequired, err := s.requireShellApproval(ctx, commandForGate, workdir)
	if err != nil {
		return ToolResult{}, err
	}
	if approvalRequired != nil {
		return *approvalRequired, nil
	}

	if a.Background {
		if s.Background == nil {
			return ToolResult{}, fmt.Errorf("shell_exec: background mode is not available in this context")
		}
		// Routed (strict): the background job runs INSIDE the per-identity box via a streamed box
		// exec (37-09), mirroring executeInBox's box dir/env; a box start failure denies fail-CLOSED
		// (D-09/GATE-01), never a host process. routed=false keeps the host *exec.Cmd path unchanged.
		var (
			id  string
			err error
		)
		if routed {
			id, err = s.Background.startBox(ctx, boxHandle, commandForGate, s.boxWorkdir(ctx, a.Cwd), boxEnv(a.Env))
			if err != nil {
				return sandboxUnavailableResult("shell_exec", err), nil
			}
		} else {
			id, err = s.Background.start(ctx, commandForGate, workdir, mergeEnv(a.Env))
			if err != nil {
				return ToolResult{}, err
			}
		}
		rendered := fmt.Sprintf("Started in the background as %s. Read its output with shell_poll (shell_id=%q); stop it with shell_kill.\n[aura_shell_bg {\"shell_id\":%q,\"status\":\"running\"}]", id, id, id)
		res, err := NewResult(ctx, rendered)
		if err != nil {
			return ToolResult{}, err
		}
		res.Meta = &ToolResultMeta{"shell_id": id, "background": true}
		return res, nil
	}

	// Routed: run the command INSIDE the per-identity box (never host). Background box jobs are
	// out of scope here (37-09) — a non-background routed call goes to executeInBox.
	if routed {
		return s.executeInBox(ctx, boxHandle, commandForGate, a.Cwd, a.Env, a.TimeoutMs)
	}

	timeout := effectiveShellTimeout(s.DefaultTimeout, a.TimeoutMs)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()

	command := commandForGate
	posix := !shellIsCmdFallback()
	if posix {
		command = wrapForCwdTracking(command)
	}
	name, args := shellInvocation(command)
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = workdir
	cmd.Env = mergeEnv(a.Env)
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	cmd.WaitDelay = 5 * time.Second

	captured := newShellOutputCapture(shellOutputBufCap())
	cmd.Stdout = captured.stdoutWriter()
	cmd.Stderr = captured.stderrWriter()
	runErr := cmd.Run()

	out := captured.stdoutString()
	combined := captured.combinedString()
	stderr := captured.stderrString()
	finalCwd := cmd.Dir
	if posix {
		var capturedCwd string
		_, capturedCwd = extractCwdMarker(out)
		combined, _ = removeCwdMarkerLine(combined)
		if capturedCwd != "" {
			finalCwd = capturedCwd
		}
		s.storeCwd(ctx, capturedCwd)
	}
	combined = redactModelPreview(combined)
	stderr = redactModelPreview(stderr)
	ecPtr := exitCodePtr(runErr, runCtx.Err())
	body := renderShellBody(combined, runErr, runCtx.Err())
	footer := renderShellFooter(ctx, body, stderr, runErr, runCtx.Err(), shellExecFooter{
		ExitCode:   ecPtr,
		Cwd:        finalCwd,
		DurationMS: time.Since(started).Milliseconds(),
		TimedOut:   shellTimedOut(runErr, runCtx.Err()),
	})
	res, err := NewResultReservingTail(ctx, body, footer)
	if err != nil {
		return ToolResult{}, err
	}
	meta := ToolResultMeta{
		"cwd":       finalCwd,
		"timed_out": shellTimedOut(runErr, runCtx.Err()),
	}
	if ecPtr != nil {
		meta["exit_code"] = *ecPtr
	}
	res.Meta = &meta
	return res, nil
}

type shellOutputCapture struct {
	mu       sync.Mutex
	combined boundedOutputBuffer
	stdout   boundedOutputBuffer
	stderr   boundedOutputBuffer
}

func newShellOutputCapture(capBytes int) *shellOutputCapture {
	if capBytes <= 0 {
		capBytes = defaultShellOutputCap
	}
	return &shellOutputCapture{
		combined: boundedOutputBuffer{capBytes: capBytes},
		stdout:   boundedOutputBuffer{capBytes: capBytes},
		stderr:   boundedOutputBuffer{capBytes: capBytes},
	}
}

type boundedOutputBuffer struct {
	buf      []byte
	capBytes int
	dropped  int64
}

func (b *boundedOutputBuffer) Write(p []byte) {
	if len(p) == 0 {
		return
	}
	if b.capBytes <= 0 {
		b.capBytes = defaultShellOutputCap
	}
	if len(p) >= b.capBytes {
		b.dropped += int64(len(b.buf) + len(p) - b.capBytes)
		b.buf = append(b.buf[:0], p[len(p)-b.capBytes:]...)
		return
	}
	overflow := len(b.buf) + len(p) - b.capBytes
	if overflow > 0 {
		b.dropped += int64(overflow)
		copy(b.buf, b.buf[overflow:])
		b.buf = b.buf[:len(b.buf)-overflow]
	}
	b.buf = append(b.buf, p...)
}

func (b *boundedOutputBuffer) String() string {
	if b.dropped <= 0 {
		return string(b.buf)
	}
	return fmt.Sprintf("[output truncated: dropped %d byte(s); showing last %d byte(s)]\n%s",
		b.dropped, len(b.buf), string(b.buf))
}

type shellOutputStream int

const (
	shellStreamStdout shellOutputStream = iota
	shellStreamStderr
)

type shellStreamWriter struct {
	capture *shellOutputCapture
	stream  shellOutputStream
}

func (c *shellOutputCapture) stdoutWriter() *shellStreamWriter {
	return &shellStreamWriter{capture: c, stream: shellStreamStdout}
}

func (c *shellOutputCapture) stderrWriter() *shellStreamWriter {
	return &shellStreamWriter{capture: c, stream: shellStreamStderr}
}

func (w *shellStreamWriter) Write(p []byte) (int, error) {
	w.capture.mu.Lock()
	defer w.capture.mu.Unlock()
	w.capture.combined.Write(p)
	if w.stream == shellStreamStderr {
		w.capture.stderr.Write(p)
	} else {
		w.capture.stdout.Write(p)
	}
	return len(p), nil
}

func (c *shellOutputCapture) combinedString() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.combined.String()
}

func (c *shellOutputCapture) stdoutString() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stdout.String()
}

func (c *shellOutputCapture) stderrString() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stderr.String()
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

// mergeEnvCap returns a non-negative slice-capacity hint for mergeEnv, saturating
// to maxInt rather than wrapping if the parent+extra+2 sum ever overflowed (it
// cannot in practice — both are live-process len() values — but an unbounded add
// feeding make() is an arithmetic-safety footgun CodeQL flags).
func mergeEnvCap(parentLen, extraLen int) int {
	const defaults = 2
	if parentLen < 0 || extraLen < 0 {
		return 0
	}
	if parentLen > math.MaxInt-extraLen-defaults {
		return math.MaxInt
	}
	return parentLen + extraLen + defaults
}

// mergeEnv returns the process env plus Aura's runtime-safe defaults and the
// caller's overrides. Python defaults to UTF-8 so model-written scripts with
// symbols in progress output do not fail under Windows cp1252 consoles.
func mergeEnv(extra map[string]string) []string {
	parent := os.Environ()
	env := make([]string, 0, mergeEnvCap(len(parent), len(extra)))
	for _, kv := range parent {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || secret.IsSecretEnvVar(k, strings.TrimPrefix(kv, k+"=")) {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"PYTHONIOENCODING=utf-8",
		"PYTHONUTF8=1",
	)
	for k, v := range extra {
		if secret.IsSecretEnvVar(k, v) {
			continue
		}
		env = append(env, k+"="+v)
	}
	return env
}

func renderShellOutput(output string, runErr, ctxErr error) string {
	body := renderShellBody(output, runErr, ctxErr)
	status := shellStatusLine(runErr, ctxErr)
	if status == "" {
		return body
	}
	var b strings.Builder
	b.WriteString(body)
	appendStatus(&b, status)
	return b.String()
}

func renderShellBody(output string, runErr, ctxErr error) string {
	if output == "" && shellStatusLine(runErr, ctxErr) == "" {
		return "[no output]"
	}
	return output
}

func shellStatusLine(runErr, ctxErr error) string {
	switch {
	case shellTimedOut(runErr, ctxErr):
		return "[command timed out]"
	case errors.Is(ctxErr, context.Canceled):
		return "[command cancelled]"
	case runErr != nil:
		if ec, ok := exitCode(runErr); ok {
			return fmt.Sprintf("[exit code %d]", ec)
		}
		return fmt.Sprintf("[command failed: %v]", runErr)
	}
	return ""
}

func shellTimedOut(runErr, ctxErr error) bool {
	return errors.Is(ctxErr, context.DeadlineExceeded) || errors.Is(runErr, exec.ErrWaitDelay)
}

const shellStderrTailCap = 800

func renderShellFooter(ctx context.Context, body, stderr string, runErr, ctxErr error, footer shellExecFooter) string {
	status := shellStatusLine(runErr, ctxErr)
	reserved := appendShellFooter(status, footer)
	if shouldReserveStderrTail(ctx, body, reserved, stderr) {
		reserved = appendShellFooter(joinFooterSections(status, stderrTailBlock(stderr)), footer)
	}
	return reserved
}

func shouldReserveStderrTail(ctx context.Context, body, reserved, stderr string) bool {
	if strings.TrimSpace(stderr) == "" {
		return false
	}
	tc, ok := toolCallCtx(ctx)
	if !ok {
		return false
	}
	return len(body)+len(reserved) > tc.cap
}

func stderrTailBlock(stderr string) string {
	tail := strings.TrimRight(truncateTailBytes(stderr, shellStderrTailCap), "\n")
	if tail == "" {
		return ""
	}
	return "[stderr tail]\n" + tail
}

func joinFooterSections(parts ...string) string {
	var b strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(part)
	}
	return b.String()
}

func truncateTailBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func exitCode(err error) (int, bool) {
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return ee.ExitCode(), true
	}
	return 0, false
}

func exitCodePtr(runErr, ctxErr error) *int {
	if shellTimedOut(runErr, ctxErr) || errors.Is(ctxErr, context.Canceled) {
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
