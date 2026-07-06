//go:build docker_integration

// main_test.go installs the goroutine-leak gate for the docker_integration tier: after the
// lifecycle / cross-deny / materialize tests run, no goroutine may survive. The moby client's
// HTTP transport goroutines are reaped by the per-test cli.Close() (t.Cleanup), so a leak here
// means a backend path spawned work it never joined. Defined ONLY under docker_integration so
// the untagged unit tests keep their default (no-TestMain) execution.

package usersandbox

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
