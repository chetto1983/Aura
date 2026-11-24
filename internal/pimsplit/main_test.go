package pimsplit

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package on a leaked goroutine: the integration tier opens a pool and a conn.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
