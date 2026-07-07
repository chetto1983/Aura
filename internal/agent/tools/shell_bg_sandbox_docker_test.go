//go:build docker_integration

// shell_bg_sandbox_docker_test.go is the docker_integration tier for the routed BACKGROUND shell
// (SBX-01, plan 37-09): against a LIVE Docker daemon it proves a strict-profile background
// shell_exec job runs INSIDE the per-identity box as a streamed box exec (not a host process),
// shell_poll returns the box's streamed output, and shell_kill stops the box exec. Shutdown joins
// every reaper/pump goroutine (goleak via the package TestMain) so no box exec or waiter leaks.
// Compiled ONLY under `-tags docker_integration`; skips locally when dockerd is unreachable and
// t.Fatals under $CI (no-skip-as-green).

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestShellBg_RunsInBox(t *testing.T) {
	skipUnlessDockerdTools(t)
	router := newDockerRouter(t, nil)
	bg := NewBackgroundShells(router)
	t.Cleanup(func() { _ = bg.Shutdown(context.Background()) })

	shell := &ShellExec{Background: bg, Router: router}
	poll := &ShellPoll{Shells: bg}
	kill := &ShellKill{Shells: bg}
	ctx := ctxWith(t, "sess-bg-box", "call-bg-box")

	// Start a long-running loop in the box; it must return a shell_id immediately (not block).
	start, err := shell.Execute(ctx, json.RawMessage(`{"command":"i=0; while true; do echo tick-$i; i=$((i+1)); sleep 0.2; done","background":true}`))
	if err != nil {
		t.Fatalf("background start: %v", err)
	}
	if start.Meta == nil || (*start.Meta)["background"] != true {
		t.Fatalf("start did not report a background job: %+v", start.Meta)
	}
	shellID, _ := (*start.Meta)["shell_id"].(string)
	if shellID == "" {
		t.Fatalf("no shell_id returned: %q", start.Preview)
	}

	pollArgs, _ := json.Marshal(map[string]string{"shell_id": shellID})

	// Poll until the box produces output — proving the job runs (and streams) INSIDE the box.
	var sawTick bool
	for i := 0; i < 50; i++ {
		res, perr := poll.Execute(ctx, json.RawMessage(pollArgs))
		if perr != nil {
			t.Fatalf("poll: %v", perr)
		}
		if strings.Contains(res.Preview, "tick-") {
			sawTick = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !sawTick {
		t.Fatal("background box job produced no streamed output (poll never saw a tick)")
	}

	// shell_kill stops the box exec.
	killArgs, _ := json.Marshal(map[string]string{"shell_id": shellID})
	if _, kerr := kill.Execute(ctx, json.RawMessage(killArgs)); kerr != nil {
		t.Fatalf("kill: %v", kerr)
	}

	// After kill, poll reports the job as killed (not running).
	var killed bool
	for i := 0; i < 50; i++ {
		res, perr := poll.Execute(ctx, json.RawMessage(pollArgs))
		if perr != nil {
			t.Fatalf("poll after kill: %v", perr)
		}
		if res.Meta != nil && (*res.Meta)["status"] == "killed" {
			killed = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !killed {
		t.Fatal("background box job not reported killed after shell_kill")
	}
}
