package messagedrafts

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain installs the package-wide goroutine-leak gate: the scheduled review and the lapsed
// pass start lock-holding work against this store.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
