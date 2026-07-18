//go:build !windows

package procgroup

import (
	"os/exec"
	"testing"
)

// TestSetProcessGroupUnixSetsSetpgid asserts the structural SysProcAttr wiring
// (Setpgid=true) SetProcessGroup must set for KillProcessGroup's Kill(-pid,...) to
// later reach the whole group, not just the tracked PID.
func TestSetProcessGroupUnixSetsSetpgid(t *testing.T) {
	cmd := &exec.Cmd{}
	SetProcessGroup(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatalf("SysProcAttr = %+v, want Setpgid=true", cmd.SysProcAttr)
	}
}
