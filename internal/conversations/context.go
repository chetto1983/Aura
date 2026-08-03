package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/db"
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

const (
	defaultHistoryHardCapTurns = 50
	minHistoryHardCapTurns     = 4
	maxHistoryHardCapTurns     = 1000
)

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

// toolSearchName mirrors agent.searchTool. It is duplicated rather than imported because
// internal/agent depends on this package, not the other way round.
const toolSearchName = "tool_search"

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
	// HistoryHardCapTurns bounds rows fetched, sidecars rehydrated, and turns
	// tokenized before the L1/L2 ladder. The protected system head counts toward
	// this aggregate cap.
	HistoryHardCapTurns int
	// AlwaysBlock is the rendered messages[1] always-on skill block (D-07). When
	// non-empty the ladder injects it as a PROTECTED user-role turn immediately after
	// the system L0 turn — counted toward the budget but never evicted by L1/L2.5
	// (Pitfall 3). The Runner renders it per turn from current loader state; an empty
	// string means no always:true skill is active (the turn is omitted).
	AlwaysBlock string
	// TransientContext is a non-persisted reference item, budgeted as one unit and
	// inserted immediately before the current user turn. It is omitted, never
	// truncated, when the deterministic ladder cannot make room for it.
	TransientContext *TransientContext
	// ProviderErrorReserveTokens is the extra L2 hard-cap headroom the configured chat
	// backend needs for its token-estimation error (Wave 1.10). It is non-zero only on
	// the local llama.cpp path, which has no provider-side overflow net — an Aura
	// under-count there is a hard local window overflow, so the cap reserves this many
	// tokens below the SPEC Req#10 formula. The Runner sets it from
	// llm.ProviderErrorReserveTokens(cfg); zero (the default) leaves the formula
	// unchanged for OpenRouter and hand-built test configs.
	ProviderErrorReserveTokens int
}

// hardCap computes the L2 hard cap from the config (SPEC Req#10:
// hard_cap = ContextWindow - max(MaxOutputTokens, 20000) - 13000 -
// ProviderErrorReserveTokens). ProviderErrorReserveTokens (Wave 1.10) is 0 for
// OpenRouter and hand-built configs, so the historical formula is unchanged there;
// on the netless local llama.cpp path it reserves extra headroom for the
// tiktoken-vs-local-tokenizer gap. The formula is kept whenever it is already
// positive (normal/large windows). For a small-window model where the fixed
// reservation exceeds the window the formula is non-positive; instead of clamping to
// 0 (which disabled L2/L2.5 protection entirely — finding M-03) it floors to a
// positive smallWindowHardCapFloor so L2.5 truncation stays active. A degenerate
// ContextWindow <= 0 still yields 0.
func (c ContextConfig) hardCap() int {
	out := max(c.MaxOutputTokens, l2MinOutputReservation)
	cap := c.ContextWindow - out - l2HeadroomTokens - c.ProviderErrorReserveTokens
	if cap <= 0 {
		cap = smallWindowHardCapFloor(c.ContextWindow)
	}
	return cap
}

// HardCap exposes the exact history cap to the final model-request guard.
func (c ContextConfig) HardCap() int {
	return c.hardCap()
}

// smallWindowHardCapFloor returns the positive L2 cap used when the SPEC Req#10
// formula is non-positive (M-03). Adopting the nanobot precedent (a budget <= 0
// clamps to max(128, window//2) rather than disabling history management), Aura
// floors to ContextWindow/2 so L2.5 stays active on small windows. A window <= 0 is
// a misconfiguration with no usable budget, so it returns 0 (the only remaining
// hardCap==0 path; boot fail-fast is the seam to reject it).
func smallWindowHardCapFloor(window int) int {
	if window <= 0 {
		return 0
	}
	return window / 2
}

// rotEmitter is the narrow write surface ApplyContextLadder needs to record an L2.5
// drop. *Store satisfies it; unit tests pass a fake (no DB).
type rotEmitter interface {
	insertContextRotEvent(ctx context.Context, conversationID string, pairsDropped, before, after int) error
}

// insertContextRotEvent records one L2.5 hard-drop audit row (Store impl).
func (s *Store) insertContextRotEvent(ctx context.Context, conversationID string, pairsDropped, before, after int) error {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return fmt.Errorf("context rot event: %w", err)
	}
	return s.scoped(ctx, func(q *sqlc.Queries) error {
		if iErr := q.InsertContextRotEvent(ctx, sqlc.InsertContextRotEventParams{
			ConversationID: id,
			Action:         rotActionHardDropPairs,
			PairsDropped:   int32(pairsDropped),
			TokensBefore:   int32(before),
			TokensAfter:    int32(after),
		}); iErr != nil {
			return fmt.Errorf("insert context rot event %s: %w", conversationID, iErr)
		}
		return nil
	})
}

// RotEvent is the domain projection of one aura.context_rot_events row — the
// microcompact ladder audit trail (L2.5 hard-drop). PairsDropped/TokensBefore/
// TokensAfter quantify one drop; TS is the RFC3339 timestamp. The web cockpit's
// context-budget gauge marks these on the fill bar (D-11, Phase 25).
type RotEvent struct {
	TS           string // RFC3339
	Action       string
	PairsDropped int
	TokensBefore int
	TokensAfter  int
}

// ListContextRotEvents returns the microcompact ladder events for one conversation
// in ts-ASC order via the existing ListContextRotEvents sqlc query (no new query or
// migration — read-only projection at the pgtype boundary). A malformed id is a
// parse error the caller maps to a clean 404.
func (s *Store) ListContextRotEvents(ctx context.Context, conversationID string) ([]RotEvent, error) {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("list context rot events: %w", err)
	}
	var rows []sqlc.AuraContextRotEvents
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		listed, lErr := q.ListContextRotEvents(ctx, id)
		if lErr != nil {
			return fmt.Errorf("list context rot events %s: %w", conversationID, lErr)
		}
		rows = listed
		return nil
	}); err != nil {
		return nil, err
	}
	out := make([]RotEvent, 0, len(rows))
	for _, r := range rows {
		ts := ""
		if r.Ts.Valid {
			ts = r.Ts.Time.Format(time.RFC3339)
		}
		out = append(out, RotEvent{
			TS:           ts,
			Action:       r.Action,
			PairsDropped: int(r.PairsDropped),
			TokensBefore: int(r.TokensBefore),
			TokensAfter:  int(r.TokensAfter),
		})
	}
	return out, nil
}

// LoadManagedHistory loads the raw turns and applies the L1/L2/L2.5 ladder, the
// entry point the Runner calls (D-A2-06: the ladder is applied in/around
// LoadHistory). It uses the Store as the rot emitter. The database retains the
// protected system head and returns only the configured newest rows, bounding query,
// sidecar, and tokenizer work before the ladder runs.
func (s *Store) LoadManagedHistory(ctx context.Context, conversationID string, cfg ContextConfig) ([]llm.Message, error) {
	turns, err := s.loadRecentTurns(
		ctx,
		conversationID,
		normalizeHistoryTurnCap(cfg.HistoryHardCapTurns),
	)
	if err != nil {
		return nil, err
	}
	return s.managedFromTurns(ctx, conversationID, turns, cfg)
}

// LoadManagedHistoryForBranch is the path-aware variant (D-09 / CHAT-05): it walks the
// SELECTED branch path (leaf -> root via parent_seq, returned root -> leaf) with
// recursion bounded by HistoryHardCapTurns, then feeds that deterministic turn list
// into the SAME L1/L2/L2.5 ladder. A leafSeq <= 0 selects
// the conversation's canonical branch leaf, so the default selection reconstructs the
// linear history (the 0017 backfill chains the canonical branch parent_seq = seq-1).
// The protected head (system seq=1 + the always-block) is rebuilt by the ladder
// exactly as in the linear case — only the body turns differ per branch (Pitfall 3),
// so messages[0] stays byte-identical across branches (the CAP-04 cache invariant).
func (s *Store) LoadManagedHistoryForBranch(ctx context.Context, conversationID string, leafSeq int, cfg ContextConfig) ([]llm.Message, error) {
	if leafSeq <= 0 {
		leaf, err := s.CanonicalBranchLeaf(ctx, conversationID)
		if err != nil {
			return nil, err
		}
		leafSeq = leaf
	}
	turns, err := s.loadRecentBranchTurns(
		ctx,
		conversationID,
		leafSeq,
		normalizeHistoryTurnCap(cfg.HistoryHardCapTurns),
	)
	if err != nil {
		return nil, err
	}
	return s.managedFromTurns(ctx, conversationID, turns, cfg)
}

func normalizeHistoryTurnCap(value int) int {
	if value < minHistoryHardCapTurns || value > maxHistoryHardCapTurns {
		return defaultHistoryHardCapTurns
	}
	return value
}

// managedFromTurns applies the deterministic L1/L2/L2.5 ladder to an already-loaded
// turn list — the shared tail of both the linear (LoadManagedHistory) and path-aware
// (LoadManagedHistoryForBranch) entry points. It uses the Store as the rot emitter.
func (s *Store) managedFromTurns(ctx context.Context, conversationID string, turns []Turn, cfg ContextConfig) ([]llm.Message, error) {
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
	tail, tailTokens := usableTransientContext(cfg.TransientContext, enc, hardCap)
	ordinaryCap := hardCap
	if tail != nil {
		ordinaryCap -= tailTokens
	}
	tokensAfterL1 := totalTokens(enc, l1)
	totalAfterL1 := tokensAfterL1 + tailTokens

	// L2: budget gate. Over the warn cap (0.75×hard) → audit WARN; over the hard cap
	// → fall through to L2.5. If under the hard cap, we are done after L1 (SC-1:
	// zero rot rows). hardCap == 0 is now reached ONLY by a degenerate ContextWindow
	// <= 0 (M-03 floors every positive small window to ContextWindow/2 so it takes
	// the normal budget path); that misconfig has no usable budget, so the gate
	// returns the L1 history rather than erroring.
	if hardCap > 0 && totalAfterL1 > int(float64(hardCap)*l2WarnRatio) && totalAfterL1 <= hardCap {
		slog.Warn("conversation context over the L2 warn cap",
			"conversation_id", conversationID, "tokens", totalAfterL1, "hard_cap", hardCap)
	}
	if hardCap == 0 || tokensAfterL1 <= ordinaryCap {
		return injectTransientContext(repairManagedToolMessagePairs(toMessages(l1)), tail), nil
	}

	// L2.5: drop oldest user/assistant PAIRs (preserve system L0 + keep an even
	// non-system length) until under the hard cap, writing ONE rot-event row.
	reduced, pairsDropped := dropOldestPairs(enc, l1, ordinaryCap)
	tokensAfter := totalTokens(enc, reduced)
	if tail != nil && (pairsDropped == 0 || tokensAfter > ordinaryCap) {
		tail = nil
		tailTokens = 0
		ordinaryCap = hardCap
		if tokensAfterL1 <= ordinaryCap {
			return repairManagedToolMessagePairs(toMessages(l1)), nil
		}
		reduced, pairsDropped = dropOldestPairs(enc, l1, ordinaryCap)
		tokensAfter = totalTokens(enc, reduced)
	}
	if pairsDropped == 0 || tokensAfter > ordinaryCap {
		// L2.5 could not bring it under (only the system turn / a single oversized
		// turn remains) → the explicit window-exceeded error (suggest `aura chat new`).
		return nil, fmt.Errorf("%w (%d tokens, cap %d)", ErrContextWindowExceeded, tokensAfter+tailTokens, hardCap)
	}
	if err := emit.insertContextRotEvent(ctx, conversationID, pairsDropped, totalAfterL1, tokensAfter+tailTokens); err != nil {
		return nil, err
	}
	return injectTransientContext(repairManagedToolMessagePairs(toMessages(reduced)), tail), nil
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
	protected := toolSearchResultIDs(out)
	for i := range out {
		t := &out[i]
		if t.Seq == 1 || t.Role != llm.RoleTool {
			continue // never the system turn; only tool turns
		}
		if _, isSearch := protected[t.ToolCallID]; isSearch {
			// A tool_search result is not an ordinary tool output: it is where the schema
			// of a loaded deferred tool LIVES. Evicting it to a pointer takes away the
			// arguments the model needs to call that tool — and the pointer it gets back
			// says to page it via read_tool_output, which is itself a tool call. Cheaper
			// and honest to keep it: these results are small (one spec) and few.
			continue
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

// toolSearchResultIDs returns the tool_call ids answered by a tool_search result, by
// pairing each assistant turn's tool_calls with the RoleTool turn that carries the same id
// — the same anchoring agent.deriveActivated uses, and for the same reason: matching on
// result TEXT would let any tool mint an exemption by echoing a spec block.
func toolSearchResultIDs(turns []Turn) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, t := range turns {
		if len(t.ToolCalls) == 0 {
			continue
		}
		var calls []struct {
			ID       string `json:"id"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if err := json.Unmarshal(t.ToolCalls, &calls); err != nil {
			continue
		}
		for _, c := range calls {
			if c.Function.Name == toolSearchName {
				ids[c.ID] = struct{}{}
			}
		}
	}
	return ids
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
	for _, v := range slices.Backward(matches) {
		if len(v) != 2 {
			continue
		}
		id, err := strconv.Unquote(v[1])
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

// dropOldestPairs removes complete historical rounds after the protected head
// while preserving the final user-led active round byte-for-byte. The historical
// name/signature stays because rot-event accounting consumes its count.
func dropOldestPairs(enc *tiktoken.Tiktoken, turns []Turn, hardCap int) ([]Turn, int) {
	start := 0
	if len(turns) > 0 && turns[0].Seq == 1 && turns[0].Role == llm.RoleSystem {
		start = 1
	}
	if len(turns) > start && isAlwaysBlock(turns[start]) {
		start++
	}
	head := turns[:start]
	body := append([]Turn(nil), turns[start:]...)

	// Read-only scan, so ranging over the yielded copy is safe here — unlike a loop that
	// assigns into the element, where slices.Backward's copy would swallow the write.
	activeAt := 0
	for i, turn := range slices.Backward(body) {
		if turn.Role == llm.RoleUser {
			activeAt = i
			break
		}
	}
	history := body[:activeAt]
	active := body[activeAt:]

	pairsDropped := 0
	for len(history) > 0 {
		current := make([]Turn, 0, len(head)+len(history)+len(active))
		current = append(current, head...)
		current = append(current, history...)
		current = append(current, active...)
		if totalTokens(enc, current) <= hardCap {
			break
		}
		var dropped bool
		history, dropped = dropOldestRound(history)
		if !dropped {
			break
		}
		pairsDropped++
	}
	for len(history) > 0 && history[0].Role == llm.RoleTool {
		history = history[1:]
		if pairsDropped == 0 {
			pairsDropped = 1
		}
	}
	reduced := make([]Turn, 0, len(head)+len(history)+len(active))
	reduced = append(reduced, head...)
	reduced = append(reduced, history...)
	reduced = append(reduced, active...)
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
