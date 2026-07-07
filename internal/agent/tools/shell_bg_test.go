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

// drain polls until the shell leaves the running state, returning the joined NEW
// output chunks (footers stripped) and the final status. Fails on timeout.
func drain(ctx context.Context, t *testing.T, poll *ShellPoll, id, filter string) (string, string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var out strings.Builder
	for {
		body, status := pollOnce(ctx, t, poll, id, filter)
		for ln := range strings.SplitSeq(body, "\n") {
			if strings.HasPrefix(ln, "[aura_shell_bg ") || ln == "[no new output]" {
				continue
			}
			out.WriteString(ln)
			out.WriteByte('\n')
		}
		if status != "running" {
			return out.String(), status
		}
		if time.Now().After(deadline) {
			t.Fatalf("background shell %s still running after deadline; status=%s", id, status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBackgroundShell_StartPollComplete(t *testing.T) {
	bg := NewBackgroundShells(nil)
	sh := &ShellExec{Background: bg}
	ctx := ctxWith(t, "sess-bg", "call-bg")

	res, err := sh.Execute(ctx, bgArgs(t, "echo hello-bg"))
	if err != nil {
		t.Fatalf("background start: %v", err)
	}
	id, _ := (*res.Meta)["shell_id"].(string)
	if id == "" {
		t.Fatalf("no shell_id in meta: %#v", res.Meta)
	}
	if !strings.Contains(res.Preview, "background") {
		t.Fatalf("preview missing background notice: %q", res.Preview)
	}

	poll := &ShellPoll{Shells: bg}
	out, status := drain(ctx, t, poll, id, "")
	if !strings.Contains(out, "hello-bg") {
		t.Fatalf("polled output missing stdout: %q", out)
	}
	if status != "exited:0" {
		t.Fatalf("status = %q, want exited:0", status)
	}
	// A second poll after completion returns no NEW output (incremental read-off).
	body, _ := pollOnce(ctx, t, poll, id, "")
	if !strings.Contains(body, "[no new output]") {
		t.Fatalf("second poll should report no new output, got %q", body)
	}
}

func TestBackgroundShell_Filter(t *testing.T) {
	if shellIsCmdFallback() {
		t.Skip("cmd.exe fallback does not honor ';' command separation")
	}
	bg := NewBackgroundShells(nil)
	sh := &ShellExec{Background: bg}
	ctx := ctxWith(t, "sess-bgf", "call-bgf")

	res, err := sh.Execute(ctx, bgArgs(t, "echo keep-me; echo drop-me"))
	if err != nil {
		t.Fatalf("background start: %v", err)
	}
	id, _ := (*res.Meta)["shell_id"].(string)
	poll := &ShellPoll{Shells: bg}
	out, _ := drain(ctx, t, poll, id, "keep")
	if !strings.Contains(out, "keep-me") {
		t.Fatalf("filtered output missing keep-me: %q", out)
	}
	if strings.Contains(out, "drop-me") {
		t.Fatalf("filter leaked a non-matching line: %q", out)
	}
}

func TestBackgroundShell_Kill(t *testing.T) {
	if shellIsCmdFallback() {
		t.Skip("cmd.exe fallback has no sleep; kill needs a POSIX shell")
	}
	bg := NewBackgroundShells(nil)
	sh := &ShellExec{Background: bg}
	ctx := ctxWith(t, "sess-bgk", "call-bgk")

	res, err := sh.Execute(ctx, bgArgs(t, "sleep 5"))
	if err != nil {
		t.Fatalf("background start: %v", err)
	}
	id, _ := (*res.Meta)["shell_id"].(string)
	poll := &ShellPoll{Shells: bg}
	if _, status := pollOnce(ctx, t, poll, id, ""); status != "running" {
		t.Fatalf("status = %q, want running", status)
	}

	kill := &ShellKill{Shells: bg}
	if _, err := kill.Execute(ctx, mustJSON(t, map[string]string{"shell_id": id})); err != nil {
		t.Fatalf("shell_kill: %v", err)
	}
	deadline := time.Now().Add(backgroundKillWaitTimeout())
	for {
		if _, status := pollOnce(ctx, t, poll, id, ""); status != "running" {
			return // killed well before the 5s sleep would have ended
		}
		if time.Now().After(deadline) {
			t.Fatal("killed shell still reports running after 2s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBackgroundShellsCapRunningJobs(t *testing.T) {
	if shellIsCmdFallback() {
		t.Skip("cmd.exe fallback has no sleep; cap fixture needs a long-running command")
	}
	t.Setenv("AURA_SHELL_BG_MAX", "1")
	bg := NewBackgroundShells(nil)
	id, err := bg.start(context.Background(), "sleep 2", "", mergeEnv(nil))
	if err != nil {
		t.Fatalf("start first: %v", err)
	}
	defer func() { _ = bg.kill(id) }()
	if _, err := bg.start(context.Background(), "sleep 2", "", mergeEnv(nil)); err == nil {
		t.Fatal("want cap error for second running background shell")
	} else if !strings.Contains(err.Error(), "cap") || !strings.Contains(err.Error(), "1") {
		t.Fatalf("cap error = %v, want mention cap 1", err)
	}
}

func TestBackgroundShellsPrunesFinishedBeforeCap(t *testing.T) {
	t.Setenv("AURA_SHELL_BG_MAX", "1")
	bg := NewBackgroundShells(nil)
	id, err := bg.start(context.Background(), "echo done", "", mergeEnv(nil))
	if err != nil {
		t.Fatalf("start first: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sh, ok := bg.get(id); ok {
			sh.mu.Lock()
			done := sh.done
			sh.mu.Unlock()
			if done {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := bg.start(context.Background(), "echo second", "", mergeEnv(nil)); err != nil {
		t.Fatalf("finished shell should be pruned before cap check: %v", err)
	}
}

func TestBackgroundShellsShutdownKillsRunning(t *testing.T) {
	if shellIsCmdFallback() {
		t.Skip("cmd.exe fallback has no sleep; shutdown fixture needs a long-running command")
	}
	bg := NewBackgroundShells(nil)
	id, err := bg.start(context.Background(), "sleep 30", "", mergeEnv(nil))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
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

	if _, err := (&ShellExec{}).Execute(ctx, bgArgs(t, "echo x")); err == nil {
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
