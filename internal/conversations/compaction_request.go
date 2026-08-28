// compaction_request.go is the OPERATOR-REQUESTED half of L2.4 — what `/compact` runs.
//
// The ladder's own compaction is a reaction to a budget; this one is a decision, taken
// while there is still room. Everything under the trigger is shared: the same summarizer,
// the same durable row, the same watermark the next turn reads through
// applyStoredCompaction. Nothing here is a second compaction implementation, and that is
// deliberate — an explicit path that summarized differently from the automatic one would
// mean the operator could not tell, from the result, what the model is now going to see.
package conversations

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrCompactionUnavailable means the deployment has no summarizer wired
	// (AURA_CONTEXT_COMPACTION disabled, or no chat client), so there is no way to
	// condense anything. The caller surfaces it as "unavailable", never as a failure of
	// the conversation.
	ErrCompactionUnavailable = errors.New("compaction is not configured for this deployment")

	// ErrNothingToCompact means there is no history behind the active round — nothing a
	// summary could stand for. It is a fact about the conversation, not an error in it.
	ErrNothingToCompact = errors.New("nothing to compact: this conversation has no earlier turns")

	// ErrCompactionNotWorthwhile means the summary came back LARGER than the turns it would
	// replace, so keeping it would make every later turn more expensive rather than less.
	//
	// Measured live 2026-08-17 on a 4-turn thread of one-sentence answers: 2005 tokens of
	// history, 2533 tokens of summary. The framing and the summarizer's own section headings
	// have a floor, and a short conversation is already below it. The guard exists because
	// applyStoredCompaction keeps a stored summary IN FORCE from then on — an inflating
	// compaction is not a bad turn, it is a permanent tax.
	ErrCompactionNotWorthwhile = errors.New("nothing to gain: a summary of this conversation would be longer than the conversation")

	// ErrCompactionFailed means the summarizer was asked and did not answer — a provider
	// error, a timeout, an empty completion. It is the only one of the four that is a
	// malfunction, and it says "nothing was changed" because that is the fact the operator
	// needs: a failed compaction removes NOTHING, and the alternative reading (that the
	// command ate the conversation) is the one a bare error invites.
	//
	// It exists because this used to be an untyped error, which the gateway turned into a
	// 500 — which is what the operator saw on 2026-08-17 when the summary was hitting an
	// output cap. hermes-agent reports the same state in the same terms: "Summary
	// generation failed; no messages were removed."
	ErrCompactionFailed = errors.New("the summary could not be generated; nothing in this conversation was changed")
)

// CompactionResult is what one requested compaction did, in the terms the operator asked
// the question in: how far the summary now reaches, how many turns went into it, and the
// size of the replayed history before and after. The token counts are the SAME tiktoken
// estimate the ladder budgets with, so the two numbers can be compared to the context
// gauge without translation.
type CompactionResult struct {
	Summary          string
	CoversThroughSeq int
	SourceTurns      int
	TokensBefore     int
	TokensAfter      int
}

// Compact condenses everything before the active round into this branch's durable summary
// and returns what that changed. The summary is stored, so the next turn's ladder finds it
// through applyStoredCompaction and replays the summary instead of the turns — which is the
// whole effect of the command; nothing is deleted, and aura.conversation_turns still holds
// every turn the summary speaks for.
//
// It compacts to the ACTIVE ROUND rather than to the protected tail the automatic path
// keeps: an operator asking for this is asking for the room, and leaving a quarter of the
// budget verbatim because the ladder would have is not what they asked for.
//
// cfg.branchID is unexported and therefore zero here — the canonical branch, the same one
// LoadManagedHistory files under. A conversation being read on a fork keeps its own row and
// is left alone (applyStoredCompaction only replaces turns the watermark covers).
func (s *Store) Compact(ctx context.Context, conversationID string, cfg ContextConfig) (CompactionResult, error) {
	if cfg.Summarizer == nil {
		return CompactionResult{}, ErrCompactionUnavailable
	}
	enc, err := encoder()
	if err != nil {
		return CompactionResult{}, fmt.Errorf("compact %s: tiktoken encoder: %w", conversationID, err)
	}
	loaded, err := s.loadManagedLinearTurns(ctx, conversationID, cfg.branchID,
		normalizeHistoryPageSize(cfg.HistoryHardCapTurns), cfg.ToolEvictAfterTurns)
	if err != nil {
		return CompactionResult{}, err
	}
	prepared := prepareTurns(loaded.turns, cfg)
	head, history, active := splitHeadHistoryActive(prepared)
	if len(history) == 0 {
		return CompactionResult{}, ErrNothingToCompact
	}
	cache := loaded.cache
	if snapshot, ok := cache.(*compactionSnapshot); ok && !snapshot.writable {
		manual := *snapshot
		manual.writable = true
		cache = &manual
	}
	summary, ok := compactionSummary(ctx, cfg.Summarizer, cache, conversationID, cfg.branchID, history)
	if !ok {
		return CompactionResult{}, fmt.Errorf("compact %s: %w", conversationID, ErrCompactionFailed)
	}
	coversThroughSeq := history[len(history)-1].Seq
	compacted := make([]Turn, 0, len(head)+1+len(active))
	compacted = append(compacted, head...)
	compacted = append(compacted, compactionTurn(summary.Summary, coversThroughSeq))
	compacted = append(compacted, active...)
	result := CompactionResult{
		Summary:          summary.Summary,
		CoversThroughSeq: coversThroughSeq,
		SourceTurns:      summary.SourceTurns,
		TokensBefore:     totalTokens(enc, prepared),
		TokensAfter:      totalTokens(enc, compacted),
	}
	// Keep it only if it is worth keeping. The ladder never has to ask — it compacts because
	// the history no longer fits, and anything beats the hard drop below it — but here
	// nothing is over budget, and a stored summary stays IN FORCE on every later turn. So a
	// summary that costs more than the turns it replaces is refused rather than saved, and
	// the model call already spent is the price of finding that out.
	//
	// A REUSED summary is past that decision: it is already the branch's state, and the
	// operator asking again does not un-make it.
	if summary.Fresh {
		if result.TokensAfter >= result.TokensBefore {
			return CompactionResult{}, ErrCompactionNotWorthwhile
		}
		storeCompaction(ctx, cache, conversationID, cfg.branchID, summary)
	}
	return result, nil
}
