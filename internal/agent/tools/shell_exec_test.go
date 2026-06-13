package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestShellExecSpecIsDeferred(t *testing.T) {
	s := (&ShellExec{}).Spec()
	if s.Name != "shell_exec" {
		t.Fatalf("name = %q, want shell_exec", s.Name)
	}
	// Full host shell remains discoverable via tool_search, but should not dominate
	// the hot manifest for simple chat/web tasks.
	if !s.Deferred {
		t.Fatal("shell_exec must be deferred; load the full schema through tool_search")
	}
}

func TestShellExecSpecMentionsStructuredFooter(t *testing.T) {
	s := (&ShellExec{}).Spec()
	for _, needle := range []string{"[aura_shell", "exit_code", "cwd", "duration_ms"} {
		if !strings.Contains(s.Description, needle) {
			t.Fatalf("shell_exec description missing structured footer hint %q: %q", needle, s.Description)
		}
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
	if res.Meta == nil {
		t.Fatal("Meta is nil, want exit_code")
	}
	if got, ok := (*res.Meta)["exit_code"].(int); !ok || got != 0 {
		t.Fatalf("Meta[exit_code] = %#v, want int(0)", (*res.Meta)["exit_code"])
	}
}

func TestShellExecPreviewIncludesStructuredFooter(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh-footer", "call-sh")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"echo hello-aura"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	footer := lastNonEmptyLine(res.Preview)
	if !strings.HasPrefix(footer, "[aura_shell ") || !strings.HasSuffix(footer, "]") {
		t.Fatalf("preview missing structured aura_shell footer: %q", res.Preview)
	}
	var payload struct {
		ExitCode   *int   `json:"exit_code"`
		Cwd        string `json:"cwd"`
		DurationMS int64  `json:"duration_ms"`
		TimedOut   bool   `json:"timed_out"`
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(footer, "[aura_shell "), "]")
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("footer is not JSON: %v; footer=%q", err, footer)
	}
	if payload.ExitCode == nil || *payload.ExitCode != 0 {
		t.Fatalf("footer exit_code = %#v, want 0", payload.ExitCode)
	}
	if strings.TrimSpace(payload.Cwd) == "" {
		t.Fatalf("footer cwd is empty: %q", footer)
	}
	if payload.DurationMS < 0 {
		t.Fatalf("footer duration_ms = %d, want >= 0", payload.DurationMS)
	}
	if payload.TimedOut {
		t.Fatalf("footer timed_out = true for successful echo")
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
	if res.Meta == nil {
		t.Fatal("Meta is nil, want exit_code")
	}
	if got, ok := (*res.Meta)["exit_code"].(int); !ok || got != 7 {
		t.Fatalf("Meta[exit_code] = %#v, want int(7)", (*res.Meta)["exit_code"])
	}
}

func TestShellExecLargeFailureKeepsFooter(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh-large-fail", "call-sh-large-fail")
	raw, _ := json.Marshal(shellExecArgs{Command: largeFailingCommand()})

	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("a failing command is a normal result, not a Go error: %v", err)
	}
	if !res.Truncated {
		t.Fatal("large output should be truncated")
	}
	for _, needle := range []string{"[exit code 7]", "[aura_shell ", `"exit_code":7`, "ERRTAIL"} {
		if !strings.Contains(res.Preview, needle) {
			t.Fatalf("preview missing %q after truncation: %q", needle, res.Preview)
		}
	}
}

// shellIsCmd reports whether the resolved Windows shell is the degraded cmd.exe
// fallback (no POSIX bash found) — the syntax the tests feed depends on it.
func shellIsCmd() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	name, _ := shellInvocation("true")
	base := strings.ToLower(filepath.Base(name))
	return base == "cmd" || base == "cmd.exe"
}

func TestShellExecPassesEnv(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")

	cmd := `echo "$AURA_SHELL_TEST"`
	if shellIsCmd() {
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

func TestMergeEnvFiltersParentSecretsButKeepsExplicitEnv(t *testing.T) {
	t.Setenv("AURA_PARENT_TOKEN", "parent-secret")
	env := mergeEnv(map[string]string{
		"AURA_CHILD_TOKEN": "child-secret",
		"AURA_VISIBLE":     "visible",
	})
	joined := strings.Join(env, "\x00")
	if strings.Contains(joined, "parent-secret") {
		t.Fatalf("secret-shaped parent env leaked into child env: %q", joined)
	}
	if !strings.Contains(joined, "AURA_CHILD_TOKEN=child-secret") {
		t.Fatalf("explicit per-call env must be preserved even for secret-shaped keys: %q", joined)
	}
	if !strings.Contains(joined, "AURA_VISIBLE=visible") {
		t.Fatalf("explicit visible env missing: %q", joined)
	}
}

func TestMergeEnvStripsPrivateKeyFromChild(t *testing.T) {
	// B-09 acceptance: a bare *_KEY parent secret must not be inherited by the
	// shell child — the same outcome the MCP path already had.
	t.Setenv("PRIVATE_KEY", "leaked-private-key-material")
	joined := strings.Join(mergeEnv(nil), "\x00")
	if strings.Contains(joined, "leaked-private-key-material") || strings.Contains(joined, "PRIVATE_KEY=") {
		t.Fatalf("PRIVATE_KEY leaked into shell child env: %q", joined)
	}
}

func TestRedactModelPreviewRedactsCredentialShapes(t *testing.T) {
	got := redactModelPreview("Authorization: Bearer sk-or-v1deadbeefcafebabe\n" +
		"token=abc123def456\n" +
		`{"api_key":"ZZZZsecretZZZZ"}`)
	for _, bad := range []string{"sk-or-v1deadbeefcafebabe", "abc123def456", "ZZZZsecretZZZZ"} {
		if strings.Contains(got, bad) {
			t.Fatalf("secret %q survived redaction in %q", bad, got)
		}
	}
	if strings.Count(got, shellRedacted) < 2 {
		t.Fatalf("redacted preview missing placeholders: %q", got)
	}
}

func TestShellExecRedactsModelPreview(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh-redact", "call-sh")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"echo token=supersecretvalue123"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(res.Preview, "supersecretvalue123") {
		t.Fatalf("preview leaked command output secret: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, shellRedacted) {
		t.Fatalf("preview missing redaction placeholder: %q", res.Preview)
	}
}

func TestShellExecEffectiveTimeoutClampsRequestedTimeout(t *testing.T) {
	t.Setenv(envShellMaxTimeoutMs, "50")
	if got := effectiveShellTimeout(2*time.Second, 1000); got != 50*time.Millisecond {
		t.Fatalf("requested timeout was not clamped: got %v want 50ms", got)
	}
	if got := effectiveShellTimeout(10*time.Millisecond, 0); got != 10*time.Millisecond {
		t.Fatalf("default timeout below cap should pass through: got %v", got)
	}
}

func TestShellOutputCaptureCapsBuffers(t *testing.T) {
	capture := newShellOutputCapture(8)
	if n, err := capture.stdoutWriter().Write([]byte("1234567890")); err != nil || n != 10 {
		t.Fatalf("write stdout = (%d, %v), want (10,nil)", n, err)
	}
	got := capture.stdoutString()
	if !strings.Contains(got, "output truncated") || !strings.Contains(got, "34567890") {
		t.Fatalf("capped stdout did not report tail truncation: %q", got)
	}
	if strings.Contains(got, "1234567890") {
		t.Fatalf("capped stdout kept the full payload: %q", got)
	}
}

func TestShellExecDefaultsPythonUTF8(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh-utf8", "call-sh")

	cmd := `printf '%s:%s\n' "$PYTHONIOENCODING" "$PYTHONUTF8"`
	if shellIsCmd() {
		cmd = "echo %PYTHONIOENCODING%:%PYTHONUTF8%"
	}
	raw, _ := json.Marshal(shellExecArgs{Command: cmd})

	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "utf-8:1") {
		t.Fatalf("Python UTF-8 defaults not visible to shell command: %q", res.Preview)
	}
}

func TestShellExecHonorsCwd(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")
	dir := t.TempDir()

	cmd := "pwd"
	if shellIsCmd() {
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
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")

	cmd := orphanGrandchildCommand(t, pidFile)
	timeoutMS := int64(300)
	if runtime.GOOS == "windows" {
		timeoutMS = 1500
	}
	raw, _ := json.Marshal(shellExecArgs{Command: cmd, TimeoutMs: timeoutMS})

	started := time.Now()
	res, err := tool.Execute(ctx, raw)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("a timeout is a normal result, not a Go error: %v", err)
	}
	if !strings.Contains(res.Preview, "[command timed out]") {
		t.Fatalf("preview missing timeout marker: %q", res.Preview)
	}
	if elapsed > time.Duration(timeoutMS)*time.Millisecond+6*time.Second {
		t.Fatalf("Execute returned after %s, want within timeout+WaitDelay margin", elapsed)
	}
	pid := readPIDFile(t, pidFile)
	if waitForPIDDead(pid, 2*time.Second) {
		t.Fatalf("grandchild pid %d is still alive after shell timeout", pid)
	}
}

func TestShellExecReportsCancellation(t *testing.T) {
	tool := &ShellExec{}
	parent, cancel := context.WithCancel(ctxWith(t, "sess-sh-cancel", "call-sh"))

	cmd := "sleep 30"
	if shellIsCmd() {
		cmd = "ping -n 30 127.0.0.1"
	}
	raw, _ := json.Marshal(shellExecArgs{Command: cmd, TimeoutMs: 30_000})

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	res, err := tool.Execute(parent, raw)
	if err != nil {
		t.Fatalf("a cancellation is a normal result, not a Go error: %v", err)
	}
	if !strings.Contains(res.Preview, "[command cancelled]") {
		t.Fatalf("preview missing cancelled marker: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "[command timed out]") {
		t.Fatalf("cancelled run should not be reported as timeout: %q", res.Preview)
	}
	if res.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if got, ok := (*res.Meta)["timed_out"].(bool); !ok || got {
		t.Fatalf("Meta[timed_out] = %#v, want false", (*res.Meta)["timed_out"])
	}
}

func TestRenderShellOutputClassifiesWaitDelay(t *testing.T) {
	got := renderShellOutput("partial output", osexec.ErrWaitDelay, nil)
	if !strings.Contains(got, "[command timed out]") {
		t.Fatalf("ErrWaitDelay should render as timeout, got: %q", got)
	}
	if strings.Contains(got, "WaitDelay") || strings.Contains(got, "[command failed:") {
		t.Fatalf("ErrWaitDelay leaked internal failure to model: %q", got)
	}
	if ec := exitCodePtr(osexec.ErrWaitDelay, nil); ec != nil {
		t.Fatalf("ErrWaitDelay exitCodePtr = %#v, want nil", *ec)
	}
}

func TestShellExecPreservesStdoutStderrInterleave(t *testing.T) {
	if shellIsCmd() {
		t.Skip("cmd.exe fallback: interleave fixture is POSIX-only")
	}
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh-interleave", "call-sh")

	cmd := "printf 'out1\\n'; sleep 0.05; printf 'err1\\n' >&2; sleep 0.05; printf 'out2\\n'; sleep 0.05; printf 'err2\\n' >&2"
	raw, _ := json.Marshal(shellExecArgs{Command: cmd})

	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertInOrder(t, res.Preview, []string{"out1", "err1", "out2", "err2"})
}

// TestShellExecCwdPersistsAcrossCalls: Bash-tool parity — a `cd` in one call
// carries into the next call of the SAME session; a different session still starts
// at the workspace root; the tracking marker never leaks into the model-visible
// output. Skipped under the degraded cmd.exe fallback (no tracking there).
func TestShellExecCwdPersistsAcrossCalls(t *testing.T) {
	if shellIsCmd() {
		t.Skip("cmd.exe fallback: cwd tracking is POSIX-only (degraded mode)")
	}
	root := t.TempDir()
	tool := &ShellExec{WorkspaceRoot: root}

	res, err := tool.Execute(ctxWith(t, "sess-cwd", "call-1"),
		json.RawMessage(`{"command":"mkdir -p subdir && cd subdir && echo moved"}`))
	if err != nil {
		t.Fatalf("Execute(cd): %v", err)
	}
	if strings.Contains(res.Preview, cwdMarker) {
		t.Fatalf("tracking marker leaked into output: %q", res.Preview)
	}

	res, err = tool.Execute(ctxWith(t, "sess-cwd", "call-2"),
		json.RawMessage(`{"command":"pwd -W 2>/dev/null || pwd"}`))
	if err != nil {
		t.Fatalf("Execute(pwd): %v", err)
	}
	if !strings.Contains(res.Preview, "subdir") {
		t.Fatalf("cwd did not persist across calls: pwd = %q (want .../subdir)", res.Preview)
	}

	// A DIFFERENT session must not inherit the first session's cd.
	res, err = tool.Execute(ctxWith(t, "sess-other", "call-3"),
		json.RawMessage(`{"command":"pwd -W 2>/dev/null || pwd"}`))
	if err != nil {
		t.Fatalf("Execute(pwd other session): %v", err)
	}
	if strings.Contains(res.Preview, "subdir") {
		t.Fatalf("cwd LEAKED across sessions: pwd = %q", res.Preview)
	}
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

func orphanGrandchildCommand(t *testing.T, pidFile string) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		return fmt.Sprintf("sleep 30 & echo $! > %s; sleep 30", shellQuote(pidFile))
	}
	scriptPath := filepath.Join(filepath.Dir(pidFile), "orphan-grandchild.ps1")
	script := fmt.Sprintf("$p = Start-Process -FilePath powershell -ArgumentList '-NoProfile','-Command','Start-Sleep -Seconds 30' -PassThru\nSet-Content -LiteralPath %s -Value $p.Id\nStart-Sleep -Seconds 30\n", psQuote(pidFile))
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write PowerShell fixture: %v", err)
	}
	if shellIsCmd() {
		return fmt.Sprintf(`powershell -NoProfile -ExecutionPolicy Bypass -File "%s"`, strings.ReplaceAll(scriptPath, `"`, `\"`))
	}
	return fmt.Sprintf("powershell -NoProfile -ExecutionPolicy Bypass -File %s", shellQuote(scriptPath))
}

func largeFailingCommand() string {
	if runtime.GOOS == "windows" {
		if shellIsCmd() {
			return `powershell -NoProfile -Command "$s='x'*10000; [Console]::Out.WriteLine($s); [Console]::Error.WriteLine('ERRTAIL'); exit 7"`
		}
		return `powershell -NoProfile -Command '$s="x"*10000; [Console]::Out.WriteLine($s); [Console]::Error.WriteLine("ERRTAIL"); exit 7'`
	}
	return "i=0; while [ $i -lt 10000 ]; do printf x; i=$((i+1)); done; printf '\\nERRTAIL\\n' >&2; exit 7"
}

func readPIDFile(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read grandchild pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse grandchild pid %q: %v", string(raw), err)
	}
	if pid <= 0 {
		t.Fatalf("grandchild pid = %d, want > 0", pid)
	}
	return pid
}

func waitForPIDDead(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		alive, err := processAlive(pid)
		if err == nil && !alive {
			return false
		}
		if time.Now().After(deadline) {
			return alive
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func processAlive(pid int) (bool, error) {
	if runtime.GOOS == "windows" {
		out, err := osexec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").CombinedOutput()
		if err != nil {
			return false, err
		}
		return strings.Contains(string(out), fmt.Sprintf(`"%d"`, pid)), nil
	}
	err := osexec.Command("kill", "-0", strconv.Itoa(pid)).Run()
	return err == nil, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func assertInOrder(t *testing.T, haystack string, needles []string) {
	t.Helper()
	pos := 0
	for _, needle := range needles {
		idx := strings.Index(haystack[pos:], needle)
		if idx < 0 {
			t.Fatalf("missing %q after byte %d in output: %q", needle, pos, haystack)
		}
		pos += idx + len(needle)
	}
}

// TestShellExecNormalizesCRLF: a model that emits CRLF line endings inside
// command must not corrupt heredoc terminators or the POSIX cwd-tracking wrap
// (live run 9, amendment #53 / D-42).
func TestShellExecNormalizesCRLF(t *testing.T) {
	if shellIsCmd() {
		t.Skip("cmd.exe fallback: heredocs are POSIX-only")
	}
	tool := &ShellExec{WorkspaceRoot: t.TempDir()}

	cmd := "cat > out.txt <<'EOF'\r\ncrlf-payload\r\nEOF\r\ncat out.txt"
	raw, _ := json.Marshal(shellExecArgs{Command: cmd})

	res, err := tool.Execute(ctxWith(t, "sess-crlf", "call-1"), raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "crlf-payload") || strings.Contains(res.Preview, "syntax error") {
		t.Fatalf("CRLF command corrupted the shell: %q", res.Preview)
	}
}

func TestExtractCwdMarker(t *testing.T) {
	clean, dir := extractCwdMarker("hello\n" + cwdMarker + " D:/work\n")
	if clean != "hello" || dir != "D:/work" {
		t.Fatalf("extract = (%q, %q), want (hello, D:/work)", clean, dir)
	}
	clean, dir = extractCwdMarker("no marker here")
	if clean != "no marker here" || dir != "" {
		t.Fatalf("no-marker extract = (%q, %q)", clean, dir)
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

func TestShellExecBadJSONSteersLargeContentToFileTools(t *testing.T) {
	tool := &ShellExec{}
	ctx := ctxWith(t, "sess-sh", "call-sh")

	_, err := tool.Execute(ctx, json.RawMessage(`{`))
	if err == nil {
		t.Fatal("Execute returned nil error for malformed JSON")
	}
	msg := err.Error()
	if !strings.Contains(msg, "fs_write") || !strings.Contains(msg, "fs_edit") {
		t.Fatalf("malformed-args hint should steer large content to file tools, got: %q", msg)
	}
	if strings.Contains(msg, "cat >>") || strings.Contains(msg, "heredoc") {
		t.Fatalf("malformed-args hint should not steer the model to shell heredocs/appends, got: %q", msg)
	}
}
