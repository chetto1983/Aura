package agui

import (
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestMain runs the agui package under goleak. The translator is a pure function
// (no goroutines), but the per-package goleak convention is mandatory (D-A5-01)
// and guards the server pump goroutine the later plans add to this package.
func TestMain(m *testing.M) {
	// Windows initializes time.Local lazily through the registry. The fanout slow-subscriber
	// tests intentionally hit slog warning paths under tight timing gates, so warm local time
	// before goleak-sensitive tests measure producer liveness.
	_ = time.Now().Format(time.RFC3339Nano)
	goleak.VerifyTestMain(m)
}
