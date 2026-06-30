package agui

import (
	"context"
	"iter"

	"github.com/chetto1983/aura/internal/agent"
)

type modelUserMessageRunner interface {
	TurnWithModelUserMessage(ctx context.Context, convID, visibleUserMsg, modelUserMsg string) iter.Seq2[*agent.Event, error]
}
