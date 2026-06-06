//go:build db_integration

package toolinvocations

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the integration tier under goleak so any leaked pgx pool goroutine
// fails the package (no-leak discipline, mirrors internal/conversations + internal/identity).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
