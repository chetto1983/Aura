package runner

import (
	"context"
	"iter"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
)

type turnInput struct {
	visibleUserMsg *string
	modelUserMsg   *string
	branchLeaf     int
}

// TurnWithModelUserMessage persists visibleUserMsg as the human-facing user turn while
// sending modelUserMsg to the LLM for the active round.
func (r *Runner) TurnWithModelUserMessage(ctx context.Context, convID, visibleUserMsg, modelUserMsg string) iter.Seq2[*agent.Event, error] {
	return r.runTurn(ctx, convID, turnInput{visibleUserMsg: &visibleUserMsg, modelUserMsg: &modelUserMsg})
}

func currentRoundModelHistory(history []llm.Message, visibleUserMsg, modelUserMsg *string) []llm.Message {
	if visibleUserMsg == nil || modelUserMsg == nil || *visibleUserMsg == *modelUserMsg {
		return history
	}
	out := append([]llm.Message(nil), history...)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role == llm.RoleUser && out[i].Content == *visibleUserMsg {
			out[i].Content = *modelUserMsg
			return out
		}
	}
	return append(out, llm.Message{Role: llm.RoleUser, Content: *modelUserMsg})
}
