package runner

import (
	"context"

	"github.com/chetto1983/aura/internal/conversations"
)

// CompactConversation runs the operator-requested compaction over the branch of a thread
// that ends at leafSeq, the one on the operator's screen. A leafSeq <= 0 is the turn written
// last, the branch a client that names none is continuing.
//
// It builds the SAME ContextConfig a turn would use — same summarizer, same model, same
// eviction and history caps — because the point of the command is to decide what the NEXT
// turn replays. A config assembled separately here would be a second opinion about the
// conversation, and the operator would be told about a compaction the ladder then declined
// to use.
//
// The memory block is nil: TransientContext is a per-message reference item chosen for the
// message being sent, and this call is not sending one.
func (r *Runner) CompactConversation(ctx context.Context, conversationID string, leafSeq int) (conversations.CompactionResult, error) {
	return r.Conv.Compact(ctx, conversationID, leafSeq, r.contextConfig(ctx, nil))
}
