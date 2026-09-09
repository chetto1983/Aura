package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// slowBoxBackend finishes after `delay` instead of blocking forever, so a promoted job
// can actually be observed COMPLETING — the half a killed command never reaches.
type slowBoxBackend struct {
	fakeBoxBackend
	delay time.Duration
}

func (b *slowBoxBackend) Exec(ctx context.Context, _ usersandbox.BoxHandle, req usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	b.execCalls = append(b.execCalls, req)
	select {
	case <-time.After(b.delay):
		return usersandbox.ExecResult{Stdout: []byte("finished late\n"), ExitCode: 0}, nil
	case <-ctx.Done():
		return usersandbox.ExecResult{}, ctx.Err()
	}
}

// TestShellExecPromotesOnTimeout: a command that outlives its cap is HANDED BACK as a
// background job, not killed.
//
// Measured 2026-09-09 on this session's own runtime, which does exactly this: a 10s
// command under a 3s cap reported "moved to the background (ID: …)" and still reached
// exit code 0. Aura instead killed it and told the model only "[command timed out]",
// which is why the same command was retried identically — 28 recorded tool-call turns,
// zero uses of background, because the choice is demanded BEFORE anyone can know how
// long the command will take.
func TestShellExecPromotesOnTimeout(t *testing.T) {
	be := &slowBoxBackend{delay: 300 * time.Millisecond}
	reg := NewBackgroundShells(newStrictBoxRouter(be))
	tool := &ShellExec{Router: newStrictBoxRouter(be), Background: reg}
	ctx := ctxWith(t, "sess-promote", "call-sh")

	raw, _ := json.Marshal(shellExecArgs{Command: "long-build", TimeoutMs: 50})
	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("a promotion is a normal result, not a Go error: %v", err)
	}
	if strings.Contains(res.Preview, "[command timed out]") {
		t.Fatalf("a promoted command must not be reported as killed: %q", res.Preview)
	}
	id := promotedShellID(t, res.Preview)

	// The work continues and completes: the point of promoting instead of killing.
	deadline := time.Now().Add(5 * time.Second)
	var status, chunk string
	for time.Now().Before(deadline) {
		sh, ok := reg.get(id)
		if !ok {
			t.Fatalf("promoted job %q is not in the registry", id)
		}
		chunk, status = sh.snapshot(nil)
		if status != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status != "exited:0" {
		t.Fatalf("promoted job status = %q, want exited:0", status)
	}
	if !strings.Contains(chunk, "finished late") {
		t.Fatalf("the output of the promoted job was lost: %q", chunk)
	}
}

// TestShellExecPromotionIsKillable: promotion must not create a job nobody can stop.
func TestShellExecPromotionIsKillable(t *testing.T) {
	be := &slowBoxBackend{delay: 30 * time.Second}
	reg := NewBackgroundShells(newStrictBoxRouter(be))
	tool := &ShellExec{Router: newStrictBoxRouter(be), Background: reg}
	ctx := ctxWith(t, "sess-promote-kill", "call-sh")

	raw, _ := json.Marshal(shellExecArgs{Command: "endless", TimeoutMs: 50})
	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	id := promotedShellID(t, res.Preview)
	sh, ok := reg.get(id)
	if !ok {
		t.Fatalf("promoted job %q missing from the registry", id)
	}
	sh.cancel()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, status := sh.snapshot(nil); status != "running" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a promoted job survived its own cancel — shell_kill would be a no-op on it")
}

// TestShellExecFastCommandIsUnchanged: the promotion path must cost the common case
// nothing — a command that finishes inside its cap still returns its output inline.
func TestShellExecFastCommandIsUnchanged(t *testing.T) {
	be := &slowBoxBackend{delay: 10 * time.Millisecond}
	reg := NewBackgroundShells(newStrictBoxRouter(be))
	tool := &ShellExec{Router: newStrictBoxRouter(be), Background: reg}
	ctx := ctxWith(t, "sess-fast", "call-sh")

	raw, _ := json.Marshal(shellExecArgs{Command: "quick", TimeoutMs: 5_000})
	res, err := tool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "finished late") {
		t.Fatalf("a fast command must return its output inline: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "shell_poll") {
		t.Fatalf("a fast command must not be promoted: %q", res.Preview)
	}
}

// promotedShellID pulls the job id out of the promotion notice, failing the test when the
// notice does not carry one — an id the model cannot read is a job it cannot collect.
func promotedShellID(t *testing.T, preview string) string {
	t.Helper()
	const marker = "shell_id: "
	_, rest, ok := strings.Cut(preview, marker)
	if !ok {
		t.Fatalf("promotion notice carries no shell_id: %q", preview)
	}
	end := strings.IndexAny(rest, " \n]")
	if end < 0 {
		t.Fatalf("unterminated shell_id in %q", preview)
	}
	return rest[:end]
}
