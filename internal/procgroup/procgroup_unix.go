//go:build !windows

// Package procgroup provides per-OS process-group/process-tree lifecycle helpers:
// SetProcessGroup marks a not-yet-started *exec.Cmd as the leader of a new OS-level
// process group before it is started, so a later KillProcessGroup can terminate the
// WHOLE spawned tree — not just the tracked PID — instead of leaking grandchild
// processes on shutdown (D-10/F-035). Shared by internal/agent/tools (shell_exec/
// shell_bg) and internal/mcp (the stdio MCP client) so both import one implementation.
package procgroup

import (
	"os/exec"
	"syscall"
)

// SetProcessGroup marks cmd as the leader of a new process group (Setpgid) before
// it is started, so KillProcessGroup can later terminate the whole spawned tree via
// a single group-targeted signal instead of only the tracked PID.
func SetProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// KillProcessGroup sends SIGKILL to cmd's whole process group (a negative pid
// targets the group whose id equals the group leader's pid, per kill(2)). A nil
// cmd.Process (never started, or already reaped) is a no-op.
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
