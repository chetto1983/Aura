package mcptools

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the mcptools unit tests under goleak. The mount helpers return a
// closer that shuts the underlying transport down; every test that opens one (the
// httptest-backed streamable-HTTP path) must call it, so a leaked HTTP persistConn
// or a transport goroutine surviving Close trips here. The fake Server tests own no
// goroutines, so the only leak surface is the real HTTPClient transport.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
