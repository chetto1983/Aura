package telegramadapter

import (
	"fmt"
	"path/filepath"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"

	tele "gopkg.in/telebot.v4"
)

func pinnedOperationalWriteInCalls(calls []llm.ToolCall) bool {
	return agent.PinnedOperationalWriteInCalls(calls)
}

func (ib *InvocationBuilder) checkBudgetQuota(c tele.Context, userID string, estimatedTokens int) error {
	bgt := ib.b.BudgetRuntime()
	if bgt == nil {
		return nil
	}
	if bgt.IsHardBudgetExceeded() {
		ib.b.Logger().Warn("hard budget exceeded, halting LLM call", "user_id", userID)
		if _, err := c.Bot().Send(c.Recipient(), "Budget limit reached. LLM calls are temporarily halted."); err != nil {
			ib.b.Logger().Warn("budget notice send failed", "error", err)
		}
		return fmt.Errorf("hard budget exceeded")
	}
	if !bgt.CanAfford(estimatedTokens, 500) {
		ib.b.Logger().Warn("predicted cost exceeds hard budget, halting LLM call", "user_id", userID)
		if _, err := c.Bot().Send(c.Recipient(), "Predicted cost would exceed budget. Please adjust your budget or wait."); err != nil {
			ib.b.Logger().Warn("budget notice send failed", "error", err)
		}
		return fmt.Errorf("budget unaffordable")
	}
	return nil
}

// overlayWriteInCalls reports whether any of the given tool calls wrote to an
// overlay file (file tool, action write or patch, overlay-file basename). When
// true the executor reloads the system prompt before the next LLM iteration.
func overlayWriteInCalls(calls []llm.ToolCall) bool {
	for _, call := range calls {
		if call.Name != "file" {
			continue
		}
		action, _ := call.Arguments["action"].(string)
		if action != "write" && action != "patch" {
			continue
		}
		pathVal, _ := call.Arguments["path"].(string)
		if conversation.IsOverlayFileName(filepath.Base(pathVal)) {
			return true
		}
	}
	return false
}
