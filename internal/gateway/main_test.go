package gateway

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the gateway unit tier under goleak so any leaked goroutine fails the
// package (no-leak discipline, mirrors internal/toolinvocations + internal/conversations).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
