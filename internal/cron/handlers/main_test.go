package handlers

import (
	"os"
	"testing"

	"go.uber.org/goleak"
)

// TestMain installs the package-wide goroutine-leak gate. The handlers are leak-free
// by construction: the agent_job drains the FakeClient's pre-closed channels (no
// background goroutine), and the auto-reject test joins its goroutine before
// returning. A leak here would mean a handler spawned an unjoined goroutine.
//
// It also intercepts the fake-docker self-exec BEFORE m.Run(): the backup Run tests
// inject os.Args[0] as the `docker` binary, so this process is re-spawned with a docker
// argv. Handling it here (rather than letting the suite run inside the child) makes the
// re-exec recursion-safe — the child performs only the fake-docker work and exits,
// never running another test that could itself re-spawn.
func TestMain(m *testing.M) {
	if os.Getenv("AURA_BACKUP_HELPER") == "1" {
		runFakeDocker()
		return
	}
	goleak.VerifyTestMain(m)
}
