package procgroup

import (
	"os/exec"
	"testing"
)

// TestKillProcessGroupNilProcessIsNoop covers the shared (cross-OS) nil-guard: a
// cmd that was never started (or already reaped) has cmd.Process == nil, and
// KillProcessGroup must treat that as a no-op rather than panicking or erroring.
func TestKillProcessGroupNilProcessIsNoop(t *testing.T) {
	if err := KillProcessGroup(&exec.Cmd{}); err != nil {
		t.Fatalf("KillProcessGroup(nil Process) = %v, want nil", err)
	}
}

// TestSetProcessGroupSetsSysProcAttr is the cross-OS smoke check: the per-OS field
// assertions (Setpgid on Unix, CreationFlags on Windows) live in the build-tagged
// sibling test files since syscall.SysProcAttr's fields differ per OS.
func TestSetProcessGroupSetsSysProcAttr(t *testing.T) {
	cmd := &exec.Cmd{}
	SetProcessGroup(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SetProcessGroup left cmd.SysProcAttr nil")
	}
}
