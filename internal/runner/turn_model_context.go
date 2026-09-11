package runner

import (
	"context"
	"iter"
	"slices"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
)

type turnInput struct {
	visibleUserMsg *string
	modelUserMsg   *string
	branchLeaf     int
}

// titleSource is what the conversation is named after: the text the person typed this
// turn. Never the model-only rewrite, and never the loaded history, whose first user-role
// turn can be the injected skills and profile block. Empty on a turn without a new
// message (resume, branch re-run), which leaves the title to the turn that had one.
func (in turnInput) titleSource() string {
	if in.visibleUserMsg == nil {
		return ""
	}
	return *in.visibleUserMsg
}

// TurnWithModelUserMessage persists visibleUserMsg as the human-facing user turn while
// sending modelUserMsg to the LLM for the active round.
func (r *Runner) TurnWithModelUserMessage(ctx context.Context, convID, visibleUserMsg, modelUserMsg string) iter.Seq2[*agent.Event, error] {
	return r.runTurn(ctx, convID, turnInput{visibleUserMsg: &visibleUserMsg, modelUserMsg: &modelUserMsg})
}

func currentRoundModelHistory(history []llm.Message, visibleUserMsg, modelUserMsg *string) []llm.Message {
	if modelUserMsg == nil || (visibleUserMsg != nil && *visibleUserMsg == *modelUserMsg) {
		return history
	}
	out := append([]llm.Message(nil), history...)
	// Index form, deliberately: slices.Backward yields a COPY of each element, so
	// assigning through the value variable writes to the copy and the slice keeps the
	// visible text. Must address out[i] to mutate.
	for i := range slices.Backward(out) {
		if visibleUserMsg != nil && out[i].Role == llm.RoleUser && out[i].Content == *visibleUserMsg {
			out[i].Content = *modelUserMsg
			return out
		}
	}
	return append(out, llm.Message{Role: llm.RoleUser, Content: *modelUserMsg})
}
