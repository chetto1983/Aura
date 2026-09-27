//go:build !windows

package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/procgroup"
)

// Closing a host server's session ends the process group it leads: a child the server
// forked was still running after Close when only the server's own pid was reaped.
func TestLocalSessionCloseReapsTheServersChildren(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	server := ManagedServer{
		Command: "sh",
		Args:    []string{"-c", "sleep 300 & echo $! > " + pidFile + "; exec " + os.Args[0] + " -test.run=TestSDKHelperProcess"},
		Env:     []string{"AURA_MCP_SDK_HELPER=1"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := OpenSDKSession(ctx, "forks", server, EgressPolicy{}, SessionOptions{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	raw, err := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 || syscall.Kill(pid, 0) != nil {
		t.Fatalf("the server's child is not running before Close (pid %q, %v): the test proves nothing", raw, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	_ = session.Close()
	waitGone(t, pid, "the server's child")
}

// waitGone polls because a killed orphan lingers as a zombie until init reaps it, and a
// zombie still answers signal 0.
func waitGone(t *testing.T, pid int, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("%s (%d) outlived Close", what, pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A server that ignores its stdin's EOF is sent SIGTERM, and one that also ignores SIGTERM is
// killed; either way Close returns and leaves nothing running.
func TestCommandStdinCloseLadder(t *testing.T) {
	for name, script := range map[string]string{
		"exits on SIGTERM": "exec sleep 300",
		"ignores SIGTERM":  "trap '' TERM; sleep 300 & wait",
		"exits on its own": "read line; exit 0",
	} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command("sh", "-c", script)
			procgroup.SetProcessGroup(cmd)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			w := &commandStdin{WriteCloser: stdin, cmd: cmd, terminateWait: 200 * time.Millisecond}
			start := time.Now()
			_ = w.Close()
			if took := time.Since(start); took > 2*time.Second {
				t.Fatalf("Close took %s", took)
			}
			waitGone(t, -cmd.Process.Pid, "the server's process group")
		})
	}
}
