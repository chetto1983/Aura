package tools

import (
	"context"
	"encoding/json"
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
	bg := NewBackgroundShells()
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
	bg := NewBackgroundShells()
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
	bg := NewBackgroundShells()
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
	deadline := time.Now().Add(2 * time.Second)
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

func TestBackgroundShell_Errors(t *testing.T) {
	ctx := ctxWith(t, "sess-bge", "call-bge")

	if _, err := (&ShellExec{}).Execute(ctx, bgArgs(t, "echo x")); err == nil {
		t.Fatal("expected an error when the Background registry is nil")
	}
	bg := NewBackgroundShells()
	if _, err := (&ShellPoll{Shells: bg}).Execute(ctx, mustJSON(t, map[string]string{"shell_id": "nope"})); err == nil {
		t.Fatal("expected an unknown shell_id error from shell_poll")
	}
	if _, err := (&ShellKill{Shells: bg}).Execute(ctx, mustJSON(t, map[string]string{"shell_id": "nope"})); err == nil {
		t.Fatal("expected an unknown shell_id error from shell_kill")
	}
}
