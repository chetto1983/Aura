package toolapprovals

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain installs the package-wide goroutine-leak gate: the one-shot test races two
// deciding transactions against the same record.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
