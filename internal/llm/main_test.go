package llm_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain installs the goleak gate now even though config/prices spawn no
// goroutines — the package gains the SSE client (Plan 02) whose cancel-path is
// goleak-sensitive, so the harness is established here once.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
