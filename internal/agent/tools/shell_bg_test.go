package tools

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
)

func bgArgs(t *testing.T, command string) json.RawMessage {
	t.Helper()
	return mustJSON(t, map[string]any{"command": command, "background": true})
}

func pollOnce(ctx context.Context, t *testing.T, poll *ShellPoll, id, filter string) (body, status string) {
	t.Helper()
	args := map[string]string{"shell_id": id}
	if filter != "" {
		args["filter"] = filter
	}
	res, err := poll.Execute(ctx, mustJSON(t, args))
	if err != nil {
		t.Fatalf("shell_poll: %v", err)
	}
	if res.Meta == nil {
		t.Fatal("shell_poll Meta is nil")
	}
	st, _ := (*res.Meta)["status"].(string)
	return res.Preview, st
}

// registerJob puts a job in the registry exactly the way startBox does — newShell + register —
// but with no box behind it. usersandbox.ExecStreamHandle wraps a live moby client and cannot be
// constructed without a daemon, and the registry semantics under test (incremental paging, the
// concurrency cap, pruning, shutdown, poll/kill authority) do not depend on what the handle is.
// The live runs-in-a-box proof is shell_bg_sandbox_docker_test.go.
func registerJob(t *testing.T, bg *BackgroundShells, ctx context.Context, id string) *bgShell {
	t.Helper()
	sh := bg.newShell(ctx, id)
	sh.cancel = func() {
		sh.mu.Lock()
		sh.done = true
		sh.mu.Unlock()
	}
	if err := bg.register(id, sh); err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
	return sh
}

// A poll returns only the output produced SINCE the last poll, then reports the final status —
// the incremental read-off contract shell_poll's whole design rests on.
func TestBackgroundShell_PollIsIncremental(t *testing.T) {
	bg := NewBackgroundShells(nil)
	ctx := ctxWith(t, "sess-bg", "call-bg")
	sh := registerJob(t, bg, ctx, "job-inc")
	if _, err := sh.Write([]byte("hello-bg\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	poll := &ShellPoll{Shells: bg}
	body, status := pollOnce(ctx, t, poll, "job-inc", "")
	if !strings.Contains(body, "hello-bg") {
		t.Fatalf("polled output missing job stdout: %q", body)
	}
	if status != "running" {
		t.Fatalf("status = %q, want running", status)
	}

	// Nothing new since the last poll.
	body, _ = pollOnce(ctx, t, poll, "job-inc", "")
	if !strings.Contains(body, "[no new output]") {
		t.Fatalf("second poll should report no new output, got %q", body)
	}

	sh.finish(&bgBoxExit{code: 0})
	if _, status = pollOnce(ctx, t, poll, "job-inc", ""); status != "exited:0" {
		t.Fatalf("status = %q, want exited:0", status)
	}
}

func TestBackgroundShell_Filter(t *testing.T) {
	bg := NewBackgroundShells(nil)
	ctx := ctxWith(t, "sess-bgf", "call-bgf")
	sh := registerJob(t, bg, ctx, "job-filter")
	if _, err := sh.Write([]byte("keep-me\ndrop-me\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	body, _ := pollOnce(ctx, t, &ShellPoll{Shells: bg}, "job-filter", "keep")
	if !strings.Contains(body, "keep-me") {
		t.Fatalf("filtered output missing keep-me: %q", body)
	}
	if strings.Contains(body, "drop-me") {
		t.Fatalf("filter leaked a non-matching line: %q", body)
	}
}

func TestBackgroundShell_Kill(t *testing.T) {
	bg := NewBackgroundShells(nil)
	ctx := ctxWith(t, "sess-bgk", "call-bgk")
	registerJob(t, bg, ctx, "job-kill")

	poll := &ShellPoll{Shells: bg}
	if _, status := pollOnce(ctx, t, poll, "job-kill", ""); status != "running" {
		t.Fatalf("status = %q, want running", status)
	}
	if _, err := (&ShellKill{Shells: bg}).Execute(ctx, mustJSON(t, map[string]string{"shell_id": "job-kill"})); err != nil {
		t.Fatalf("shell_kill: %v", err)
	}
	if _, status := pollOnce(ctx, t, poll, "job-kill", ""); status != "killed" {
		t.Fatalf("status after kill = %q, want killed", status)
	}
}

func TestBackgroundShellsCapRunningJobs(t *testing.T) {
	t.Setenv("AURA_SHELL_BG_MAX", "1")
	bg := NewBackgroundShells(nil)
	ctx := context.Background()
	registerJob(t, bg, ctx, "job-1")

	err := bg.register("job-2", bg.newShell(ctx, "job-2"))
	if err == nil {
		t.Fatal("want cap error for second running background shell")
	}
	if !strings.Contains(err.Error(), "cap") || !strings.Contains(err.Error(), "1") {
		t.Fatalf("cap error = %v, want mention cap 1", err)
	}
}

func TestBackgroundShellsPrunesFinishedBeforeCap(t *testing.T) {
	t.Setenv("AURA_SHELL_BG_MAX", "1")
	bg := NewBackgroundShells(nil)
	ctx := context.Background()
	sh := registerJob(t, bg, ctx, "job-first")
	sh.finish(&bgBoxExit{code: 0})

	if err := bg.register("job-second", bg.newShell(ctx, "job-second")); err != nil {
		t.Fatalf("finished shell should be pruned before cap check: %v", err)
	}
}

func TestBackgroundShellsShutdownKillsRunning(t *testing.T) {
	bg := NewBackgroundShells(nil)
	const id = "job-shutdown"
	registerJob(t, bg, context.Background(), id)
	ctx, cancel := context.WithTimeout(context.Background(), backgroundKillWaitTimeout())
	defer cancel()
	if err := bg.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	sh, ok := bg.get(id)
	if !ok {
		t.Fatalf("shell %s missing", id)
	}
	sh.mu.Lock()
	killed := sh.killed
	done := sh.done
	sh.mu.Unlock()
	if !killed {
		t.Fatal("shutdown must mark running shell killed")
	}
	if !done {
		t.Fatal("shutdown must wait for shell completion")
	}
	if err := bg.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown should be harmless: %v", err)
	}
}

func TestBackgroundShellReaperPanicRecoverMarksFailedDone(t *testing.T) {
	sh := &bgShell{bufCap: 1024}
	cancelled := false
	runBackgroundShellReaper(sh, func() error {
		panic("reaper boom")
	}, func() {
		cancelled = true
	})

	if !cancelled {
		t.Fatal("reaper must always cancel after wait exits or panics")
	}
	chunk, status := sh.snapshot(nil)
	if status != "exited:1" {
		t.Fatalf("status = %q, want exited:1", status)
	}
	if !strings.Contains(chunk, "panic: reaper boom") {
		t.Fatalf("snapshot missing panic detail: %q", chunk)
	}
}

func backgroundKillWaitTimeout() time.Duration {
	if runtime.GOOS == "windows" {
		return 8 * time.Second
	}
	return 2 * time.Second
}

func TestBackgroundShellBufferCapsAndDropsConsumed(t *testing.T) {
	sh := &bgShell{bufCap: 8}
	if n, err := sh.Write([]byte("123456")); err != nil || n != 6 {
		t.Fatalf("write = (%d, %v), want (6,nil)", n, err)
	}
	chunk, status := sh.snapshot(nil)
	if chunk != "123456" || status != "running" {
		t.Fatalf("snapshot = (%q,%q), want (123456,running)", chunk, status)
	}
	if len(sh.buf) != 0 || sh.readOff != 0 {
		t.Fatalf("snapshot must drop consumed bytes, buf=%q readOff=%d", sh.buf, sh.readOff)
	}

	if n, err := sh.Write([]byte("abcdefghijk")); err != nil || n != 11 {
		t.Fatalf("write long = (%d, %v), want (11,nil)", n, err)
	}
	chunk, _ = sh.snapshot(nil)
	if !strings.Contains(chunk, "background output truncated") || !strings.Contains(chunk, "defghijk") || strings.Contains(chunk, "abc") {
		t.Fatalf("capped background output should report the retained tail only, got %q", chunk)
	}
	if len(sh.buf) != 0 || sh.readOff != 0 {
		t.Fatalf("post-truncation snapshot must drop consumed bytes, buf=%q readOff=%d", sh.buf, sh.readOff)
	}
}

func TestShellPollRedactsModelPreview(t *testing.T) {
	// The job is owned by (local, "sess-bg-redact") so the same-session poll below is
	// authorized (MUSR-03); the id key is an arbitrary registry key, not a minted one.
	bg := &BackgroundShells{shells: map[string]*bgShell{"job-redact": {bufCap: 1024, ownerID: localOwnerID, sessionID: "sess-bg-redact"}}}
	if _, err := bg.shells["job-redact"].Write([]byte("token=supersecretvalue123\n")); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	body, _ := pollOnce(ctxWith(t, "sess-bg-redact", "call-bg-redact"), t, &ShellPoll{Shells: bg}, "job-redact", "")
	if strings.Contains(body, "supersecretvalue123") {
		t.Fatalf("shell_poll leaked secret output: %q", body)
	}
	if !strings.Contains(body, shellRedacted) {
		t.Fatalf("shell_poll output missing redaction placeholder: %q", body)
	}
}

func TestBackgroundShell_Errors(t *testing.T) {
	ctx := ctxWith(t, "sess-bge", "call-bge")

	noRegistry := &ShellExec{Router: newStrictBoxRouter(&fakeBoxBackend{})}
	if _, err := noRegistry.Execute(ctx, bgArgs(t, "echo x")); err == nil {
		t.Fatal("expected an error when the Background registry is nil")
	}
	bg := NewBackgroundShells(nil)
	if _, err := (&ShellPoll{Shells: bg}).Execute(ctx, mustJSON(t, map[string]string{"shell_id": "nope"})); err == nil {
		t.Fatal("expected an unknown shell_id error from shell_poll")
	}
	if _, err := (&ShellKill{Shells: bg}).Execute(ctx, mustJSON(t, map[string]string{"shell_id": "nope"})); err == nil {
		t.Fatal("expected an unknown shell_id error from shell_kill")
	}
}
