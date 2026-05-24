package agent

import (
	"strings"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/memoryindex"
)

// PinnedOperationalWriteInCalls reports whether any call is a high/critical-
// priority operational propose_patch — callers use this to invalidate the
// pinned-memory overlay cache after such a write.
func PinnedOperationalWriteInCalls(calls []llm.ToolCall) bool {
	for _, call := range calls {
		if call.Name != "propose_patch" {
			continue
		}
		action, _ := call.Arguments["action"].(string)
		if strings.TrimSpace(action) != "operational" {
			continue
		}
		priority, _ := call.Arguments["priority"].(string)
		switch memoryindex.NormalizePriority(priority) {
		case memoryindex.PriorityCritical, memoryindex.PriorityHigh:
			return true
		}
	}
	return false
}
