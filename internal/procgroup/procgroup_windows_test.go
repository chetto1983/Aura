//go:build windows

package procgroup

import (
	"os/exec"
	"syscall"
	"testing"
)

// TestSetProcessGroupWindowsSetsCreateNewProcessGroup asserts the structural
// SysProcAttr wiring (CREATE_NEW_PROCESS_GROUP) SetProcessGroup must set for
// KillProcessGroup's taskkill /T to later walk and terminate the whole tree.
func TestSetProcessGroupWindowsSetsCreateNewProcessGroup(t *testing.T) {
	cmd := &exec.Cmd{}
	SetProcessGroup(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("SysProcAttr = %+v, want CreationFlags with CREATE_NEW_PROCESS_GROUP set", cmd.SysProcAttr)
	}
}

// TestTaskkillProcessMissingTolerates covers the three "process already gone"
// phrasings KillProcessGroup treats as success, plus a genuine-failure case that
// must NOT be tolerated (it should still surface as an error to the caller).
func TestTaskkillProcessMissingTolerates(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{"not found", `ERROR: The process "1234" not found.`, true},
		{"not running", "ERROR: The process with PID 1234 is not running.", true},
		{"no tasks are running", "INFO: No tasks are running which match the specified criteria.", true},
		{"genuine failure", "ERROR: Access is denied.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskkillProcessMissing([]byte(tc.out)); got != tc.want {
				t.Errorf("taskkillProcessMissing(%q) = %v, want %v", tc.out, got, tc.want)
			}
		})
	}
}
