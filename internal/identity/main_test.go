//go:build db_integration

package identity

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the integration tier under goleak so any leaked pgx pool
// goroutine fails the package (no-leak discipline, mirrors internal/db).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
