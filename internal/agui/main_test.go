package agui

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the agui package under goleak. The translator is a pure function
// (no goroutines), but the per-package goleak convention is mandatory (D-A5-01)
// and guards the server pump goroutine the later plans add to this package.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
