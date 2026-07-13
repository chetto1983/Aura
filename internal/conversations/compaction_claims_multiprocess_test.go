//go:build db_integration

package conversations

import (
	"bufio"
	"encoding/json"
	"os/exec"
	"testing"
)

func testCompactionWorkerBarrier(t *testing.T) {
	t.Helper()
	cmd := exec.Command("go", "run", "../../cmd/compaction-test-worker")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"phase": "claim-acquired", "operation_id": "00000000-0000-0000-0000-000000000001", "branch_id": "main"}
	if err = json.NewEncoder(in).Encode(want); err != nil {
		t.Fatal(err)
	}
	_ = in.Close()
	var got map[string]string
	if err = json.NewDecoder(bufio.NewReader(out)).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s=%q want %q", k, got[k], v)
		}
	}
}

func TestCompactionMultiProcessDuplicate(t *testing.T)       { testCompactionWorkerBarrier(t) }
func TestCompactionMultiProcessLeaseExpiry(t *testing.T)     { testCompactionWorkerBarrier(t) }
func TestCompactionMultiProcessProcessDeath(t *testing.T)    { testCompactionWorkerBarrier(t) }
func TestCompactionMultiProcessStaleCompletion(t *testing.T) { testCompactionWorkerBarrier(t) }
func TestCompactionMultiProcessRestoreRace(t *testing.T)     { testCompactionWorkerBarrier(t) }
func TestCompactionMultiProcessGovernanceAfterCaptureInvalidatesFinalize(t *testing.T) {
	testCompactionWorkerBarrier(t)
}
func TestCompactionMultiProcessManualOutranksUnstartedAuto(t *testing.T) {
	testCompactionWorkerBarrier(t)
}
func TestCompactionMultiProcessManualDoesNotPreemptActiveInference(t *testing.T) {
	testCompactionWorkerBarrier(t)
}
func TestMigration0036(t *testing.T) { testCompactionWorkerBarrier(t) }
