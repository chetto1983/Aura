package mediagen

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package when a catalog refresh, its endpoint reads or a video
// job supervisor leave a goroutine behind.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
