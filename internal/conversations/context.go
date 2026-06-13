package conversations

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

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

// alwaysBlockSeq is the synthetic seq the messages[1] always-block turn carries when
// the ladder injects it (D-07). seq=1 is the system L0 turn; the always-block sits at
// seq=2 (messages[1]) and is protected by L1/L2.5 exactly like the system L0 turn
// (Pitfall 3). It is NEVER a persisted seq — the always-block is rebuilt per turn from
// current loader state, so a skill add/remove changes messages[1] while messages[0]
// stays byte-identical (CAP-04).
const alwaysBlockSeq = 2

// alwaysBlockMarker tags the synthetic always-block turn in its ToolCallID field — a
// field a real persisted user turn NEVER populates. isAlwaysBlock keys off this
// marker (not seq+role alone, which a real persisted user turn at seq=2 would
// collide with), so the ladder protects ONLY the injected block.
const alwaysBlockMarker = "__aura_always_block__"

var readToolOutputCallIDRe = regexp.MustCompile(`read_tool_output\(tool_call_id=("(?:\\.|[^"\\])*")`)

// spillFooterMarker is the prefix tools.NewResult writes when it spills a large
// output to a sidecar. Its presence (or a ContentSidecarPath) means the full bytes
// are retrievable via read_tool_output — the only case where L1 may rewrite a tool
// turn to a pointer (M-01).
const spillFooterMarker = "[output truncated:"

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
	// AlwaysBlock is the rendered messages[1] always-on skill block (D-07). When
	// non-empty the ladder injects it as a PROTECTED user-role turn immediately after
	// the system L0 turn — counted toward the budget but never evicted by L1/L2.5
	// (Pitfall 3). The Runner renders it per turn from current loader state; an empty
	// string means no always:true skill is active (the turn is omitted).
	AlwaysBlock string
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
	// Inject the messages[1] always-block (D-07) as a PROTECTED turn right after the
	// system L0 turn, BEFORE the ladder runs, so it is counted toward the budget and
	// protected by L1/L2.5 exactly like the system turn (Pitfall 3).
	turns = injectAlwaysBlock(turns, cfg.AlwaysBlock)

	// L1: microcompact — rewrite old role='tool' turns to a read_tool_output pointer.
	l1 := applyL1(turns, cfg.ToolEvictAfterTurns)

	hardCap := cfg.hardCap()
	tokensAfterL1 := totalTokens(enc, l1)

	// L2: budget gate. Over the warn cap (0.75×hard) → audit WARN; over the hard cap
	// → fall through to L2.5. If under the hard cap, we are done after L1 (SC-1:
	// zero rot rows).
	if hardCap > 0 && tokensAfterL1 > int(float64(hardCap)*l2WarnRatio) && tokensAfterL1 <= hardCap {
		slog.Warn("conversation context over the L2 warn cap",
			"conversation_id", conversationID, "tokens", tokensAfterL1, "hard_cap", hardCap)
	}
	if hardCap == 0 || tokensAfterL1 <= hardCap {
		return repairManagedToolMessagePairs(toMessages(l1)), nil
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
	return repairManagedToolMessagePairs(toMessages(reduced)), nil
}

// injectAlwaysBlock inserts the messages[1] always-block as a protected user-role
// turn (seq=alwaysBlockSeq) immediately after a leading system L0 turn (or at the
// head when no system turn is present — the agent prepends its own system message,
// so the ladder's history may or may not carry a persisted seq=1). An empty block is
// a no-op. The injected turn is a COPY-friendly synthetic Turn (no sidecar, no
// tool-call payload); it is identified later by isAlwaysBlock so L1/L2.5 never touch
// it.
func injectAlwaysBlock(turns []Turn, block string) []Turn {
	if block == "" {
		return turns
	}
	always := Turn{Seq: alwaysBlockSeq, Role: llm.RoleUser, Content: block, ToolCallID: alwaysBlockMarker}
	start := 0
	if len(turns) > 0 && turns[0].Seq == 1 && turns[0].Role == llm.RoleSystem {
		start = 1
	}
	out := make([]Turn, 0, len(turns)+1)
	out = append(out, turns[:start]...)
	out = append(out, always)
	out = append(out, turns[start:]...)
	return out
}

// isAlwaysBlock reports whether t is the injected messages[1] always-block turn (the
// synthetic seq=2 user-role marker). The ladder protects it like the system L0 turn
// (Pitfall 3 / D-07 flagged constraint): neither L1 rewrite nor L2.5 pair-drop may
// touch it.
func isAlwaysBlock(t Turn) bool {
	return t.ToolCallID == alwaysBlockMarker && t.Role == llm.RoleUser
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
		if t.Seq < threshold && isSidecarBacked(*t) {
			// Preserve the spill-id pointer minted by tools.NewResult; old rows that
			// predate opaque sidecar ids fall back to the provider ToolCallID.
			t.Content = readToolOutputPointer(readToolOutputSpillID(t.Content, t.ToolCallID))
			t.ContentSidecarPath = "" // already inlined as a pointer
		}
	}
	return out
}

// isSidecarBacked reports whether a RoleTool turn's full content is retrievable via
// read_tool_output — either an explicit ContentSidecarPath or an inline spill footer
// (M-01). Only such turns may be evicted to a pointer; a non-spilled turn (an
// ask_user answer, a small result) has nowhere to page back from, so rewriting it
// would create a dead pointer that silently destroys the content.
func isSidecarBacked(t Turn) bool {
	if t.ContentSidecarPath != "" {
		return true
	}
	return strings.Contains(html.UnescapeString(t.Content), spillFooterMarker)
}

func readToolOutputSpillID(content, fallback string) string {
	unescaped := html.UnescapeString(content)
	footerAt := strings.LastIndex(unescaped, spillFooterMarker)
	if footerAt < 0 {
		return fallback
	}
	matches := readToolOutputCallIDRe.FindAllStringSubmatch(unescaped[footerAt:], -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if len(matches[i]) != 2 {
			continue
		}
		id, err := strconv.Unquote(matches[i][1])
		if err == nil && id != "" {
			return id
		}
	}
	return fallback
}

// readToolOutputPointer is the L1 eviction target: a compact instruction telling
// the model to page the full output back via read_tool_output (the sidecar is
// still on disk; only the in-history content is replaced).
// The public parameter stays named tool_call_id, but for new sidecars the value
// is the opaque spill id from the original footer.
func readToolOutputPointer(spillID string) string {
	if spillID == "" {
		return "[tool output evicted from context; not retrievable]"
	}
	return fmt.Sprintf("[tool output evicted to save context; page it back via read_tool_output(tool_call_id=%q)]", spillID)
}

// dropOldestPairs removes oldest conversational rounds after the protected head
// until the history fits hardCap or no droppable round remains. The historical
// name/signature stays because rot-event accounting consumes its count.
func dropOldestPairs(enc *tiktoken.Tiktoken, turns []Turn, hardCap int) ([]Turn, int) {
	// Split off the PROTECTED head: a leading system turn (seq=1) AND the messages[1]
	// always-block (seq=2, D-07 / Pitfall 3) if present. Neither is ever dropped — the
	// always-block is protected exactly like the system L0 turn so a long conversation
	// never silently loses an always-on skill.
	start := 0
	if len(turns) > 0 && turns[0].Seq == 1 && turns[0].Role == llm.RoleSystem {
		start = 1
	}
	if len(turns) > start && isAlwaysBlock(turns[start]) {
		start++
	}
	system := turns[:start]
	body := append([]Turn(nil), turns[start:]...)

	pairsDropped := 0
	for len(body) >= 2 {
		current := append(append([]Turn(nil), system...), body...)
		if totalTokens(enc, current) <= hardCap {
			break
		}
		var dropped bool
		body, dropped = dropOldestRound(body)
		if !dropped {
			break
		}
		pairsDropped++
	}
	for len(body) > 0 && body[0].Role == llm.RoleTool {
		body = body[1:]
		if pairsDropped == 0 {
			pairsDropped = 1
		}
	}
	reduced := append(append([]Turn(nil), system...), body...)
	return reduced, pairsDropped
}

func dropOldestRound(body []Turn) ([]Turn, bool) {
	if len(body) == 0 {
		return body, false
	}
	for i := 1; i < len(body); i++ {
		if body[i].Role == llm.RoleUser {
			return body[i:], true
		}
	}
	return body[:0], true
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
		// The synthetic always-block carries an internal marker in ToolCallID for
		// protection bookkeeping (isAlwaysBlock); strip it so the wire message is a
		// clean user-role message (a user message must not carry a tool_call_id).
		if isAlwaysBlock(t) {
			out = append(out, llm.Message{Role: llm.RoleUser, Content: t.Content})
			continue
		}
		msg, err := turnToMessage(t)
		if err != nil {
			msg = llm.Message{Role: t.Role, Content: t.Content, ToolCallID: t.ToolCallID}
		}
		out = append(out, msg)
	}
	return out
}
