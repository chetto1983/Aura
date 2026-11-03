package examplebank

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the package under goleak: the seed build is a goroutine, and every test waits for
// it to end.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
