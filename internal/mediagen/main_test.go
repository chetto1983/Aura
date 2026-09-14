package mediagen

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package when a catalog refresh or its endpoint reads leave a
// goroutine behind.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
