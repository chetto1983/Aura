package conversations

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/pkoukk/tiktoken-go"
)

// l2HeadroomTokens is the fixed budget reservation subtracted from the L2 hard cap
// (SPEC Req#10: hard_cap = ContextWindow - max(MaxOutputTokens,20000) - 13000). The
// 13000 covers the system prompt + tool manifest + per-request overhead that is not
// in the persisted history.
const l2HeadroomTokens = 13000

// l2MinOutputReservation is the floor for the output reservation (SPEC Req#10:
// max(MaxOutputTokens, 20000)).
const l2MinOutputReservation = 20000

// l2WarnRatio is the fraction of the hard cap at which L2 logs a WARN.
const l2WarnRatio = 0.75

// rotActionHardDropPairs is the context_rot_events.action value written when L2.5
// drops oldest user/assistant pairs (amendment #22).
const rotActionHardDropPairs = "hard_drop_pairs"

// ErrContextWindowExceeded is returned by ApplyContextLadder when the history is
// still over the L2 hard cap after L1 (and L2.5 cannot reduce it — e.g. only the
// system turn remains). It is a normal-flow error the REPL surfaces (suggesting
// `aura chat new`), NEVER the iter.Seq2 error slot.
var ErrContextWindowExceeded = errors.New("conversation context exceeds the model window; start a new chat with `aura chat new`")

// ContextConfig carries the L1/L2 inputs. ContextWindow + MaxOutputTokens come
// from llm.Config (04-01); ToolEvictAfterTurns from config.Config
// (AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS).
type ContextConfig struct {
	ContextWindow       int
	MaxOutputTokens     int
	ToolEvictAfterTurns int
}

// hardCap computes the L2 hard cap from the config (SPEC Req#10).
func (c ContextConfig) hardCap() int {
	out := c.MaxOutputTokens
	if out < l2MinOutputReservation {
		out = l2MinOutputReservation
	}
	cap := c.ContextWindow - out - l2HeadroomTokens
	if cap < 0 {
		cap = 0
	}
	return cap
}

// rotEmitter is the narrow write surface ApplyContextLadder needs to record an L2.5
// drop. *Store satisfies it; unit tests pass a fake (no DB).
type rotEmitter interface {
	insertContextRotEvent(ctx context.Context, conversationID string, pairsDropped, before, after int) error
}

// insertContextRotEvent records one L2.5 hard-drop audit row (Store impl).
func (s *Store) insertContextRotEvent(ctx context.Context, conversationID string, pairsDropped, before, after int) error {
	id, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return fmt.Errorf("context rot event: %w", err)
	}
	if err := s.q.InsertContextRotEvent(ctx, sqlc.InsertContextRotEventParams{
		ConversationID: id,
		Action:         rotActionHardDropPairs,
		PairsDropped:   int32(pairsDropped),
		TokensBefore:   int32(before),
		TokensAfter:    int32(after),
	}); err != nil {
		return fmt.Errorf("insert context rot event %s: %w", conversationID, err)
	}
	return nil
}

// LoadManagedHistory loads the raw turns and applies the L1/L2/L2.5 ladder, the
// entry point the Runner calls (D-A2-06: the ladder is applied in/around
// LoadHistory). It uses the Store as the rot emitter.
func (s *Store) LoadManagedHistory(ctx context.Context, conversationID string, cfg ContextConfig) ([]llm.Message, error) {
	turns, err := s.loadTurns(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	enc, err := encoder()
	if err != nil {
		return nil, fmt.Errorf("load managed history %s: tiktoken encoder: %w", conversationID, err)
	}
	return applyContextLadder(ctx, conversationID, turns, cfg, enc, s)
}

// applyContextLadder runs the deterministic ladder in SC-1 order: L1 tool-clearing
// → L2 budget gate → L2.5 oldest-pair drop. It is a pure function of (turns, cfg,
// encoder) except for the L2.5 rot-event write through emit. No LLM call is made.
func applyContextLadder(
	ctx context.Context,
	conversationID string,
	turns []Turn,
	cfg ContextConfig,
	enc *tiktoken.Tiktoken,
	emit rotEmitter,
) ([]llm.Message, error) {
	// L1: microcompact — rewrite old role='tool' turns to a read_tool_output pointer.
	l1 := applyL1(turns, cfg.ToolEvictAfterTurns)

	hardCap := cfg.hardCap()
	tokensAfterL1 := totalTokens(enc, l1)

	// L2: budget gate. Over warn → WARN (logged by the caller via the returned
	// signal is overkill; we keep the gate pure and let L2.5 reduce). If under the
	// hard cap, we are done after L1 (SC-1: zero rot rows).
	if hardCap == 0 || tokensAfterL1 <= hardCap {
		return toMessages(l1), nil
	}

	// L2.5: drop oldest user/assistant PAIRs (preserve system L0 + keep an even
	// non-system length) until under the hard cap, writing ONE rot-event row.
	reduced, pairsDropped := dropOldestPairs(enc, l1, hardCap)
	tokensAfter := totalTokens(enc, reduced)
	if pairsDropped == 0 || tokensAfter > hardCap {
		// L2.5 could not bring it under (only the system turn / a single oversized
		// turn remains) → the explicit window-exceeded error (suggest `aura chat new`).
		return nil, fmt.Errorf("%w (%d tokens, cap %d)", ErrContextWindowExceeded, tokensAfter, hardCap)
	}
	if err := emit.insertContextRotEvent(ctx, conversationID, pairsDropped, tokensAfterL1, tokensAfter); err != nil {
		return nil, err
	}
	return toMessages(reduced), nil
}

// applyL1 rewrites the content of role='tool' turns older than evictAfter turns
// (by turn distance from the newest) to a read_tool_output(<tool_call_id>) pointer.
// seq=1 (the system turn) is NEVER touched (Pitfall 1, KV-cache poisoning). A
// non-positive evictAfter disables L1. The returned slice is a copy — the input is
// not mutated (byte-identity of LoadHistory must hold).
func applyL1(turns []Turn, evictAfter int) []Turn {
	out := make([]Turn, len(turns))
	copy(out, turns)
	if evictAfter <= 0 || len(out) == 0 {
		return out
	}
	maxSeq := out[len(out)-1].Seq
	threshold := maxSeq - evictAfter
	for i := range out {
		t := &out[i]
		if t.Seq == 1 || t.Role != llm.RoleTool {
			continue // never the system turn; only tool turns
		}
		if t.Seq < threshold {
			t.Content = readToolOutputPointer(t.ToolCallID)
			t.ContentSidecarPath = "" // already inlined as a pointer
		}
	}
	return out
}

// readToolOutputPointer is the L1 eviction target: a compact instruction telling
// the model to page the full output back via read_tool_output (the sidecar is
// still on disk; only the in-history content is replaced).
func readToolOutputPointer(toolCallID string) string {
	if toolCallID == "" {
		return "[tool output evicted from context; not retrievable]"
	}
	return fmt.Sprintf("[tool output evicted to save context; page it back via read_tool_output(tool_call_id=%q)]", toolCallID)
}

// dropOldestPairs removes oldest user/assistant PAIRs after the leading system
// turn until the history fits hardCap or no droppable pair remains. It preserves
// the system L0 turn and keeps the non-system remainder even (len(history)%2==0
// for the non-system part). Returns the reduced turns + the count of pairs dropped.
func dropOldestPairs(enc *tiktoken.Tiktoken, turns []Turn, hardCap int) ([]Turn, int) {
	// Split off a leading system turn (seq=1) if present — it is never dropped.
	start := 0
	if len(turns) > 0 && turns[0].Seq == 1 && turns[0].Role == llm.RoleSystem {
		start = 1
	}
	system := turns[:start]
	body := append([]Turn(nil), turns[start:]...)

	pairsDropped := 0
	for len(body) >= 2 {
		current := append(append([]Turn(nil), system...), body...)
		if totalTokens(enc, current) <= hardCap {
			break
		}
		body = body[2:] // drop the oldest pair
		pairsDropped++
	}
	reduced := append(append([]Turn(nil), system...), body...)
	return reduced, pairsDropped
}

// totalTokens sums the cl100k_base token estimate over the turns' content +
// tool-call payloads.
func totalTokens(enc *tiktoken.Tiktoken, turns []Turn) int {
	total := 0
	for _, t := range turns {
		total += countTokens(enc, t.Content)
		total += countTokens(enc, string(t.ToolCalls))
		total += countTokens(enc, t.ToolCallID)
	}
	return total
}

// toMessages projects the (post-ladder) turns onto loop messages. A malformed
// tool_calls jsonb would have failed earlier in LoadHistory; here it is best-effort
// (a decode error drops the tool_calls rather than failing the whole load).
func toMessages(turns []Turn) []llm.Message {
	out := make([]llm.Message, 0, len(turns))
	for _, t := range turns {
		msg, err := turnToMessage(t)
		if err != nil {
			msg = llm.Message{Role: t.Role, Content: t.Content, ToolCallID: t.ToolCallID}
		}
		out = append(out, msg)
	}
	return out
}
