package mcp

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the mcp package unit tests under goleak so a Client whose
// subprocess survives Close, or an HTTPClient leaving a persistConn goroutine
// behind, trips the leak detector. httptest.Server.Close shuts the server side;
// the stdio Close drains cmd.Wait, so a clean run leaks nothing.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
