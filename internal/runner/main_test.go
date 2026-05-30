package runner

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"go.uber.org/goleak"
)

// TestMain runs the whole runner package (unit tier — no DB, no network) under
// goleak so the auto-title WaitGroup join is asserted: a worker that is not joined
// by Runner.Stop leaks a goroutine and fails the package (Pitfall 3 / D-A5-01).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
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
