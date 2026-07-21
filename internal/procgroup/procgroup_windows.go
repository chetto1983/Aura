//go:build windows

// Package procgroup provides per-OS process-group/process-tree lifecycle helpers:
// SetProcessGroup marks a not-yet-started *exec.Cmd as the leader of a new OS-level
// process group before it is started, so a later KillProcessGroup can terminate the
// WHOLE spawned tree — not just the tracked PID — instead of leaking grandchild
// processes on shutdown (D-10/F-035). Shared by internal/agent/tools (shell_exec/
// shell_bg) and internal/mcp (the stdio MCP client) so both import one implementation.
package procgroup

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// SetProcessGroup marks cmd as the root of a new Windows process group before it is
// started, so KillProcessGroup's taskkill /T can walk and terminate the whole
// spawned tree instead of only the tracked PID.
func SetProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// KillProcessGroup force-kills cmd's whole process tree via taskkill /F /T. A nil
// cmd.Process (never started, or already reaped) is a no-op; a taskkill failure that
// means the process was already gone is tolerated, not an error.
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// G204: pid is strconv.Itoa of the OS-assigned PID of a process this package
	// itself spawned (cmd.Process.Pid), never external/untrusted input.
	pid := strconv.Itoa(cmd.Process.Pid)
	out, err := exec.Command("taskkill", "/F", "/T", "/PID", pid).CombinedOutput() //nolint:gosec
	if err == nil || taskkillProcessMissing(out) {
		return nil
	}
	return fmt.Errorf("taskkill process group %s: %w", pid, err)
}

func taskkillProcessMissing(out []byte) bool {
	msg := strings.ToLower(string(bytes.TrimSpace(out)))
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "not running") ||
		strings.Contains(msg, "no tasks are running")
}
