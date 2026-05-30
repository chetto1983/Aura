package openai_compat

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain installs goleak for the whole package so every test inherits
// goroutine-leak detection. The cancel-mid-stream test (Req#3) depends on this:
// a leaked read goroutine or lingering persistConn would trip it here.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
