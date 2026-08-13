package tools

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// shell_exec's user-visible contract, over the fakeBoxBackend harness. Every one of these used to
// run a real host shell; the tool has one place to run now, and a canned box result is what a real
// call sees — plus it pins the exact command, dir and env composed, which a live container can only
// observe end-to-end. The live in-a-box proof (real timeout kill, real interleave, real exit codes)
// is shell_exec_sandbox_docker_test.go.

// boxShell wires a ShellExec over a backend answering with the given result.
func boxShell(res usersandbox.ExecResult) (*ShellExec, *fakeBoxBackend) {
	be := &fakeBoxBackend{execResult: res}
	return &ShellExec{Router: newStrictBoxRouter(be)}, be
}

// echoingBoxRouter answers every exec by echoing the command back as stdout, so a test that cares
// whether a command RAN (rather than what it printed) can see it did.
func echoingBoxRouter() *usersandbox.SandboxRouter {
	return routerWith(&fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: []byte(cmd)}
	}})
}

// blockingBoxBackend blocks in Exec until the call's context is done, then reports the context
// error the way a real backend does — the shape executeInBox classifies as timeout vs cancel.
type blockingBoxBackend struct{ fakeBoxBackend }

func (b *blockingBoxBackend) Exec(ctx context.Context, _ usersandbox.BoxHandle, req usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	b.execCalls = append(b.execCalls, req)
	<-ctx.Done()
	return usersandbox.ExecResult{}, ctx.Err()
}

// Whether shell_exec is deferred is asserted ONCE, in
// TestOnlyTheWorkingSetIsAlwaysActive, which is where the whole manifest budget is
// weighed. It used to be asserted here too, and that duplication is exactly how the
// set drifted to seventeen tools with each file defending its own seat.
//
// What belongs here is the operational doctrine that moved OUT of the system prompt
// and into this description: the model now reads it with the schema, at the moment
// it is about to run a command, instead of thousands of tokens earlier in a prefix
// that was describing a tool it could not even call.
func TestShellExecSpecCarriesItsOperationalRules(t *testing.T) {
	s := (&ShellExec{}).Spec()
	if s.Name != "shell_exec" {
		t.Fatalf("name = %q, want shell_exec", s.Name)
	}
	for _, rule := range []string{
		"One call is one shell transaction",
		"QUERY data, never dump it",
		"Never author file content here",
		"python3 -m pip",
		"untrusted",
	} {
		if !strings.Contains(s.Description, rule) {
			t.Errorf("shell_exec description dropped the %q rule; it has no other home", rule)
		}
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

// The description is what the model reads before it writes a path. It must not promise the host:
// a tool description that says "on the host" is an instruction to attempt host paths, which the
// box refuses — the same class of failure amendment #96 already paid for.
func TestShellExecSpecDoesNotPromiseHostAccess(t *testing.T) {
	s := (&ShellExec{}).Spec()
	text := strings.ToLower(s.Summary + " " + s.Description + " " + string(s.Parameters))
	for _, banned := range []string{"on the host", "host system shell", "host filesystem"} {
		if strings.Contains(text, banned) {
			t.Fatalf("shell_exec spec still promises host access (%q): %s", banned, text)
		}
	}
	if !strings.Contains(text, "container") {
		t.Fatalf("shell_exec spec must say where the command actually runs: %s", text)
	}
}

func TestShellExecRunsCommandInTheBox(t *testing.T) {
	tool, be := boxShell(usersandbox.ExecResult{Stdout: []byte("hello-aura\n")})
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
	if len(be.execCalls) != 1 || !strings.Contains(be.execCalls[0].Command, "echo hello-aura") {
		t.Fatalf("the command must reach the box verbatim: %#v", be.execCalls)
	}
}

func TestShellExecPreviewIncludesStructuredFooter(t *testing.T) {
	tool, _ := boxShell(usersandbox.ExecResult{
		Stdout: []byte("hello-aura\n\n" + cwdMarker + " /workspace\n"),
	})
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
	if payload.Cwd != "/workspace" {
		t.Fatalf("footer cwd = %q, want the box $PWD from the marker", payload.Cwd)
	}
	if payload.DurationMS < 0 {
		t.Fatalf("footer duration_ms = %d, want >= 0", payload.DurationMS)
	}
	if payload.TimedOut {
		t.Fatalf("footer timed_out = true for a successful command")
	}
}

func TestShellExecReportsExitCode(t *testing.T) {
	tool, _ := boxShell(usersandbox.ExecResult{ExitCode: 7})
	ctx := ctxWith(t, "sess-sh", "call-sh")

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

// The footer is what the Spec tells the model to read instead of spending a separate pwd or
// exit-code call, so it must survive the truncation that pages a large output into the sidecar —
// together with a tail of stderr, which is where the failure's cause lives.
func TestShellExecLargeFailureKeepsFooterAndStderrTail(t *testing.T) {
	tool, _ := boxShell(usersandbox.ExecResult{
		Stdout:   []byte(strings.Repeat("x", 10000)),
		Stderr:   []byte("ERRTAIL\n"),
		ExitCode: 7,
	})
	ctx := ctxWith(t, "sess-sh-large-fail", "call-sh-large-fail")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"build"}`))
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

// The caller's env reaches the box, and the host environment does NOT (boxEnv builds from the
// call's overrides alone — see shell_exec_boxenv_test.go).
func TestShellExecPassesEnvAndPythonUTF8Defaults(t *testing.T) {
	tool, be := boxShell(usersandbox.ExecResult{})
	ctx := ctxWith(t, "sess-sh", "call-sh")

	raw, _ := json.Marshal(shellExecArgs{Command: "env", Env: map[string]string{"AURA_SHELL_TEST": "marker-42"}})
	if _, err := tool.Execute(ctx, raw); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(be.execCalls) != 1 {
		t.Fatalf("want one box exec, got %d", len(be.execCalls))
	}
	env := be.execCalls[0].Env
	if got, ok := envValue(env, "AURA_SHELL_TEST"); !ok || got != "marker-42" {
		t.Fatalf("caller env not passed to the box: %v", env)
	}
	if got, ok := envValue(env, "PYTHONUTF8"); !ok || got != "1" {
		t.Fatalf("box env missing the Python UTF-8 default: %v", env)
	}
}

func TestShellExecRedactsModelPreview(t *testing.T) {
	tool, _ := boxShell(usersandbox.ExecResult{Stdout: []byte("token=supersecretvalue123\n")})
	ctx := ctxWith(t, "sess-sh-redact", "call-sh")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"echo token=..."}`))
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

func TestShellExecEffectiveTimeoutClampsRequestedTimeout(t *testing.T) {
	t.Setenv(envShellMaxTimeoutMs, "50")
	if got := effectiveShellTimeout(2*time.Second, 1000); got != 50*time.Millisecond {
		t.Fatalf("requested timeout was not clamped: got %v want 50ms", got)
	}
	if got := effectiveShellTimeout(10*time.Millisecond, 0); got != 10*time.Millisecond {
		t.Fatalf("default timeout below cap should pass through: got %v", got)
	}
}

// The box backend hands back the WHOLE stream in memory, so the cap is what stands between a
// `find /` and an unbounded blob in the model's context. It keeps the tail, which is both what a
// reader wants and where the cwd marker rides.
func TestCapShellOutputKeepsTheTailAndReportsTheDrop(t *testing.T) {
	t.Setenv(envShellOutputBufCap, "8")
	got := capShellOutput([]byte("1234567890"))
	if !strings.Contains(got, "output truncated") || !strings.Contains(got, "34567890") {
		t.Fatalf("capped output did not report tail truncation: %q", got)
	}
	if strings.Contains(got, "1234567890") {
		t.Fatalf("capped output kept the full payload: %q", got)
	}
}

func TestShellExecCapsBoxOutput(t *testing.T) {
	t.Setenv(envShellOutputBufCap, "64")
	tool, _ := boxShell(usersandbox.ExecResult{Stdout: []byte(strings.Repeat("y", 4096))})
	res, err := tool.Execute(ctxWith(t, "sess-sh-cap", "call-sh-cap"), json.RawMessage(`{"command":"yes"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "output truncated") {
		t.Fatalf("an unbounded box stdout must be capped before it reaches the model: %q", res.Preview)
	}
}

func TestShellExecHonorsCwd(t *testing.T) {
	tool, be := boxShell(usersandbox.ExecResult{})
	ctx := ctxWith(t, "sess-sh", "call-sh")

	raw, _ := json.Marshal(shellExecArgs{Command: "pwd", Cwd: "/workspace/project"})
	if _, err := tool.Execute(ctx, raw); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(be.execCalls) != 1 || be.execCalls[0].Dir != "/workspace/project" {
		t.Fatalf("cwd not honored on the box exec: %#v", be.execCalls)
	}
}

func TestShellExecTimesOut(t *testing.T) {
	be := &blockingBoxBackend{}
	tool := &ShellExec{Router: newStrictBoxRouter(be)}
	ctx := ctxWith(t, "sess-sh-timeout", "call-sh")

	raw, _ := json.Marshal(shellExecArgs{Command: "sleep 30", TimeoutMs: 50})
	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("a timeout is a normal result, not a Go error: %v", err)
	}
	if !strings.Contains(res.Preview, "[command timed out]") {
		t.Fatalf("preview missing timeout marker: %q", res.Preview)
	}
	if res.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if got, ok := (*res.Meta)["timed_out"].(bool); !ok || !got {
		t.Fatalf("Meta[timed_out] = %#v, want true", (*res.Meta)["timed_out"])
	}
}

func TestShellExecReportsCancellation(t *testing.T) {
	be := &blockingBoxBackend{}
	tool := &ShellExec{Router: newStrictBoxRouter(be)}
	parent, cancel := context.WithCancel(ctxWith(t, "sess-sh-cancel", "call-sh"))

	raw, _ := json.Marshal(shellExecArgs{Command: "sleep 30", TimeoutMs: 30_000})
	go func() {
		time.Sleep(50 * time.Millisecond)
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

// The box demuxes stdout and stderr into separate buffers, so the rendered body is stdout then
// stderr — not the byte-level interleave the deleted host arm preserved. Pin what it actually is,
// so nobody reads the absence of a test as a promise of interleave.
func TestShellExecAppendsStderrAfterStdout(t *testing.T) {
	tool, _ := boxShell(usersandbox.ExecResult{Stdout: []byte("out1\nout2\n"), Stderr: []byte("err1\nerr2\n")})
	res, err := tool.Execute(ctxWith(t, "sess-sh-streams", "call-sh"), json.RawMessage(`{"command":"build"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertInOrder(t, res.Preview, []string{"out1", "out2", "err1", "err2"})
}

// Bash-tool parity: a `cd` in one call carries into the next call of the SAME session, a different
// session starts fresh, and the tracking marker never leaks into the model-visible output.
func TestShellExecCwdPersistsAcrossCalls(t *testing.T) {
	tool, be := boxShell(usersandbox.ExecResult{Stdout: []byte("moved\n\n" + cwdMarker + " /workspace/subdir\n")})

	res, err := tool.Execute(ctxWith(t, "sess-cwd", "call-1"),
		json.RawMessage(`{"command":"mkdir -p subdir && cd subdir && echo moved"}`))
	if err != nil {
		t.Fatalf("Execute(cd): %v", err)
	}
	if strings.Contains(res.Preview, cwdMarker) {
		t.Fatalf("tracking marker leaked into output: %q", res.Preview)
	}

	if _, err := tool.Execute(ctxWith(t, "sess-cwd", "call-2"), json.RawMessage(`{"command":"pwd"}`)); err != nil {
		t.Fatalf("Execute(pwd): %v", err)
	}
	if got := be.execCalls[1].Dir; got != "/workspace/subdir" {
		t.Fatalf("cwd did not persist across calls: dir = %q", got)
	}

	// A DIFFERENT session must not inherit the first session's cd; an empty Dir lets the box use
	// its own workspace WORKDIR.
	if _, err := tool.Execute(ctxWith(t, "sess-other", "call-3"), json.RawMessage(`{"command":"pwd"}`)); err != nil {
		t.Fatalf("Execute(pwd other session): %v", err)
	}
	if got := be.execCalls[2].Dir; got != "" {
		t.Fatalf("cwd LEAKED across sessions: dir = %q", got)
	}
}

// A model that emits CRLF line endings inside command must not corrupt heredoc terminators or the
// cwd-tracking wrap (live run 9, amendment #53 / D-42): the normalization happens before the
// command is composed, so no \r reaches the box.
func TestShellExecNormalizesCRLF(t *testing.T) {
	tool, be := boxShell(usersandbox.ExecResult{Stdout: []byte("crlf-payload\n")})

	raw, _ := json.Marshal(shellExecArgs{Command: "cat > out.txt <<'EOF'\r\ncrlf-payload\r\nEOF\r\ncat out.txt"})
	if _, err := tool.Execute(ctxWith(t, "sess-crlf", "call-1"), raw); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(be.execCalls[0].Command, "\r") {
		t.Fatalf("a CRLF line ending reached the box shell: %q", be.execCalls[0].Command)
	}
}

func TestExtractCwdMarker(t *testing.T) {
	clean, dir := extractCwdMarker("hello\n" + cwdMarker + " /workspace/work\n")
	if clean != "hello" || dir != "/workspace/work" {
		t.Fatalf("extract = (%q, %q), want (hello, /workspace/work)", clean, dir)
	}
	clean, dir = extractCwdMarker("no marker here")
	if clean != "no marker here" || dir != "" {
		t.Fatalf("no-marker extract = (%q, %q)", clean, dir)
	}
}

// The argument guards run BEFORE the route, so a malformed call errors identically whether or not
// a box is available — and never reads as a sandbox outage.
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

func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for _, v := range slices.Backward(lines) {
		if line := strings.TrimSpace(v); line != "" {
			return line
		}
	}
	return ""
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
