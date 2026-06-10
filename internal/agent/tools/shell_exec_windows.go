//go:build windows

package tools

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	out, err := exec.Command("taskkill", "/F", "/T", "/PID", pid).CombinedOutput()
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
