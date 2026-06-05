package skills

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain installs the package-wide goroutine-leak gate. The loader is leak-free
// by construction: the TTL cache is a lazy mutex-guarded snapshot with NO
// background goroutine, so a leak here would mean a test (or the loader) spawned
// an unjoined goroutine.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
