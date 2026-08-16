package runner

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"go.uber.org/goleak"
)

// TestMain runs the whole runner package under goleak so the auto-title WaitGroup join is
// asserted: a worker that is not joined by Runner.Stop leaks a goroutine and fails the
// package (Pitfall 3 / D-A5-01).
//
// Idle HTTP keep-alives are reaped first, and they are NOT what this gate is about. Go's
// shared transport parks two goroutines per pooled connection (persistConn.readLoop /
// writeLoop) and keeps them until the idle timeout — so a package that made ANY outbound
// request during its run can end with two goroutines parked in `select` and `IO wait` and
// nothing wrong with it. That is exactly how this tier failed in CI on 2026-08-16: every
// test PASSed, and goleak reported two transport loops with no frame of ours anywhere in
// the stacks. Closing the pool makes the assertion mean what it says — a goroutine still
// standing after this is ours, not the connection pool's.
func TestMain(m *testing.M) {
	code := m.Run()
	// Before the check, not after: goleak.Cleanup runs once the verdict is already in.
	closeIdleHTTPConnections()
	if err := goleak.Find(); err != nil {
		fmt.Fprintln(os.Stderr, "goleak:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// closeIdleHTTPConnections drains the shared transport's connection pool. It handles the
// default transport only: a test that builds its own http.Client owns closing it, and
// pretending otherwise here would hide that omission rather than fix it.
func closeIdleHTTPConnections() {
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
	http.DefaultClient.CloseIdleConnections()
}

// decodeToolCallsForFake mirrors conversations' tool_calls jsonb decode for the
// in-memory fake's LoadHistory reconstruction.
func decodeToolCallsForFake(raw []byte) []llm.ToolCall {
	var calls []llm.ToolCall
	_ = json.Unmarshal(raw, &calls)
	return calls
}

// containsFold is a case-insensitive substring check for the fake search store.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
