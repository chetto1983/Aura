package conversations

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/pkoukk/tiktoken-go"
)

// compactionCache is the durable half of L2.4 (migration 0096). A nil cache disables it
// and tryCompact behaves exactly as it did before: summarize the whole history, every
// turn, and throw the summary away.
//
// *Store implements it. The interface exists so the ladder stays a pure function of its
// inputs in unit tests, which is the same reason rotEmitter exists beside it.
type compactionCache interface {
	LoadCompaction(ctx context.Context, conversationID, branchID string) (Compaction, bool, error)
	SaveCompaction(ctx context.Context, conversationID, branchID string, c Compaction) error
}

// carriedSummaryPrompt introduces the previous summary inside the transcript handed to
// the summarizer. It is a distinct wording from compactionHeader on purpose: that one
// speaks to the ASSISTANT about its own context, this one speaks to the SUMMARIZER about
// its source material.
const carriedSummaryPrompt = "summary_of_still_earlier_turns: "

// compactionSummary returns the summary text for this history slice, reusing the durable
// one whenever it already speaks for every turn in the slice.
//
// Three outcomes, and only the last one costs an LLM call:
//
//   - the stored summary already covers the newest historical turn -> return it verbatim.
//     This is the common case on a long conversation: the watermark advances only when a
//     round ages out of the protected tail, so most turns reuse the previous turn's
//     summary and the prompt prefix stays byte-identical. That stability is the point;
//     a summary regenerated per turn moves the prefix and costs the provider cache,
//     measured at 90% of input tokens on a real 128-turn conversation.
//   - a stored summary covers PART of the slice -> summarize only what came after it,
//     with the old summary carried in as source material. Re-reading the whole history
//     every time is how a compaction loses what an earlier compaction had already
//     distilled (hermes-agent's context_compressor.py calls this the iterative update).
//   - nothing stored -> summarize the slice, as before.
func compactionSummary(
	ctx context.Context,
	sum Summarizer,
	cache compactionCache,
	conversationID, branchID string,
	history []Turn,
) (string, bool) {
	watermark := history[len(history)-1].Seq
	stored, hasStored := loadStoredCompaction(ctx, cache, conversationID, branchID)
	if hasStored && stored.CoversThroughSeq >= watermark && strings.TrimSpace(stored.Summary) != "" {
		return stored.Summary, true
	}

	fresh := history
	var carried []llm.Message
	if hasStored {
		fresh = turnsAfter(history, stored.CoversThroughSeq)
		if len(fresh) == 0 {
			// The watermark is ahead of every turn in this slice, which happens when the
			// caller re-reads a branch that was compacted further along. The stored text
			// still speaks for all of it.
			return stored.Summary, strings.TrimSpace(stored.Summary) != ""
		}
		carried = []llm.Message{{Role: llm.RoleUser, Content: carriedSummaryPrompt + stored.Summary}}
	}

	summary, err := sum.Summarize(ctx, append(carried, toMessages(fresh)...))
	if err != nil {
		slog.Warn("context compaction failed; falling back to deterministic drop", "err", err)
		// A stored summary that no longer covers everything still beats dropping the
		// oldest pair outright: it describes the turns it does cover, and L2.5 remains
		// free to drop what it must.
		if hasStored && strings.TrimSpace(stored.Summary) != "" {
			return stored.Summary, true
		}
		return "", false
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", false
	}
	saveCompaction(ctx, cache, conversationID, branchID, Compaction{
		Summary: summary, CoversThroughSeq: watermark, SourceTurns: len(fresh),
	})
	return summary, true
}

// loadStoredCompaction never fails the turn. A cache that cannot be read is a compaction
// that costs an LLM call, which is what every turn cost before this existed.
func loadStoredCompaction(
	ctx context.Context, cache compactionCache, conversationID, branchID string,
) (Compaction, bool) {
	if cache == nil || conversationID == "" {
		return Compaction{}, false
	}
	stored, ok, err := cache.LoadCompaction(ctx, conversationID, branchID)
	if err != nil {
		slog.Warn("durable compaction unreadable; summarizing from scratch",
			"conversation_id", conversationID, "err", err)
		return Compaction{}, false
	}
	return stored, ok
}

// saveCompaction is best-effort for the same reason: the summary is already in hand and
// the turn is already correct without the write. Losing it costs the NEXT turn one call.
func saveCompaction(
	ctx context.Context, cache compactionCache, conversationID, branchID string, c Compaction,
) {
	if cache == nil || conversationID == "" {
		return
	}
	if err := cache.SaveCompaction(ctx, conversationID, branchID, c); err != nil {
		slog.Warn("durable compaction not stored; the next turn will summarize again",
			"conversation_id", conversationID, "err", err)
	}
}

// turnsAfter returns the turns whose seq is past the watermark, preserving order.
func turnsAfter(turns []Turn, seq int) []Turn {
	for i, turn := range turns {
		if turn.Seq > seq {
			return turns[i:]
		}
	}
	return nil
}

// carriesPreviousSummary reports whether the first message is a summary carried in from an
// earlier compaction rather than a real turn. The summarizer needs to know: updating a
// summary and writing one from scratch are different instructions, and handing the update
// prompt a transcript with no previous summary asks the model to preserve something that
// is not there.
func carriesPreviousSummary(rounds []llm.Message) bool {
	return len(rounds) > 0 && strings.HasPrefix(rounds[0].Content, carriedSummaryPrompt)
}

// protectRecentTail moves whole rounds from the end of history into the protected region
// until the token budget is spent, and returns the two new slices.
//
// It stops on a ROUND boundary (a user turn), never mid-round: half a round in the summary
// and half verbatim would show the model a question whose answer it cannot see, or an
// answer to a question that is now only a sentence in a summary.
func protectRecentTail(enc *tiktoken.Tiktoken, history, active []Turn, cap int) ([]Turn, []Turn) {
	budget := int(float64(cap) * tailProtectionRatio)
	if budget <= 0 || len(history) == 0 {
		return history, active
	}
	spent := 0
	boundary := len(history)
	for i, turn := range slices.Backward(history) {
		spent += totalTokens(enc, history[i:i+1])
		if spent > budget {
			break
		}
		if turn.Role == llm.RoleUser {
			boundary = i
		}
	}
	if boundary == len(history) {
		return history, active
	}
	protected := make([]Turn, 0, len(history)-boundary+len(active))
	protected = append(protected, history[boundary:]...)
	protected = append(protected, active...)
	return history[:boundary], protected
}
