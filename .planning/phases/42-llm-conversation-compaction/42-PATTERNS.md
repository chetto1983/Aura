# Phase 42: LLM Conversation Compaction - Pattern Map

**Mapped:** 2026-07-12
**Files analyzed:** 28 (10 new · 18 edited) across Go backend + React cockpit
**Analogs found:** 28 / 28 (every file has a shipped, verified analog — this is a pattern-application phase, not greenfield)

> Consumes: `42-SPEC.md` (In-scope), `42-CONTEXT.md` (D-01…D-10), `42-RESEARCH.md` (seams re-verified at HEAD).
> Every `file:line` below was opened and excerpted directly; the migration head is confirmed `0035_assets_source_kind_agent` → `0036` is the next free slot.

## Load-bearing correction the planner MUST absorb

**There is NO separate AG-UI wire struct for the rot-events read path, and there must not be one for compactions either.** `handleConversationRotEvents` serializes the DOMAIN struct `conversations.RotEvent` directly (`writeJSON(w, events)`), and the frontend consumes the Go PascalCase field names verbatim (`web/src/conversations/useConversations.ts`: *"Wire shapes are the Go store projections serialised with their Go field names (the structs carry no json tags)"*). So:
- The "compaction wire type" is the **domain** `conversations.Compaction` struct declared in `store_compact.go` (serialized as-is).
- The `internal/agui/types.go` edit is **widening the `ConversationStore` interface** (which lives in types.go) with `ListCompactions`, NOT adding a JSON DTO.
- The React `Compaction` interface in `useConversations.ts` must use **PascalCase** fields matching the Go struct (like `RotEvent { TS; Action; PairsDropped; … }`).

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|-------------------|------|-----------|----------------|-------|
| **NEW** `internal/conversations/compact.go` | service (LLM-over-history) | request-response (stream drain) | `internal/conversations/title.go` (`generateTitle`) | exact |
| **NEW** `internal/conversations/context_compaction.go` | utility (pure transforms) | transform | `internal/conversations/context.go` (`alwaysBlockMarker`/`isAlwaysBlock`) | exact |
| **NEW** `internal/conversations/store_compact.go` | store/model | CRUD (atomic tx + projection) | `store_append.go` (`AppendTurnTx`) + `context.go` (`ListContextRotEvents`) | exact |
| **NEW** `internal/db/migrations/0036_conversation_compactions.{up,down}.sql` | migration | DDL | `0005_conversations.up.sql` (`context_rot_events` table + grants) | role-match |
| **NEW** `internal/db/queries/conversation_compactions.sql` | query (sqlc) | CRUD | `internal/db/queries/context_rot_events.sql` | exact |
| **NEW** `internal/runner/runner_compact.go` | service (shared orchestration) | request-response | `internal/runner/runner_resume.go` (`maybeAutoTitle` lifecycle) + relocated `loadTurnHistory` (moved from `runner.go`, 42-04 FIX-1) | role-match¹ |
| **NEW** `cmd/aura/chat_compact.go` | controller (CLI) | request-response | `cmd/aura/chat.go` (`chatRename`/`chatSetStatus`) | exact |
| **NEW** `web/src/chat/ContextBudgetGauge.test.tsx` | test (component render) | — | `web/src/chat/__tests__/RuntimeFooter.test.tsx` | role-match² |
| **EDIT** `internal/conversations/context.go` | utility (ladder) | transform | self — mirror `isAlwaysBlock`/`dropOldestPairs`/`toMessages` | exact |
| **EDIT** `internal/runner/runner.go` | service (seam) | request-response | self — `Deps.EvictAfter:86` (compact fields + `runner.New` constructor defaulting only; `loadTurnHistory` RELOCATES → `runner_compact.go` per 42-04 FIX-1, keeps runner.go < 600 LOC) | exact |
| **EDIT** `internal/runner/interfaces.go` | interface | — | self — `ConversationStore` widening precedent | exact |
| **EDIT** `cmd/aura/chat.go` | controller (dispatch) | request-response | self — `runChat` switch `:43` | exact |
| **EDIT** `cmd/aura/chat_repl.go` | controller (REPL) | event-driven (line loop) | `telegram/commands.go` (`dispatchRich`) | role-match |
| **EDIT** `internal/channels/telegram/commands.go` | controller (dispatch) | event-driven | self — `/clear` interception `:153`/`:216` | exact |
| **EDIT** `internal/config/config_knobs.go` | config | — | self — `knobRegistry()` KnobSpec rows | exact |
| **EDIT** `internal/config/config.go` | config | — | self — `ContextToolEvictAfterTurns:62/394` (+ `clampCompactMaxOutput` parse-time helper, 42-03 FIX-3; if edit would push ≥600 LOC → split compact fields to NEW `config_compact.go`, 42-03 FIX-2) | exact |
| **EDIT** `cmd/aura/chat_boot.go` | config (composition root) | — | self — `Deps{… EvictAfter:337 …}` | exact |
| **EDIT** `internal/agui/conversations_api.go` | controller (route) | request-response | self — `handleConversationRotEvents:219` + `handleRenameConversation:244` | exact |
| **EDIT** `internal/agui/server.go` | interface | — | self — `Runner` iface `:53` | exact |
| **EDIT** `internal/agui/types.go` | interface | — | self — `ConversationStore` iface (`ListContextRotEvents:38`) | exact |
| **EDIT** `web/src/conversations/useConversations.ts` | hook (data layer) | request-response | self — `useConversationRotEvents:160` + `RotEvent:42` | exact |
| **EDIT** `web/src/chat/ContextBudgetGauge.tsx` | component | request-response | self — the `dropped > 0` marker block `:78` | exact |
| **EDIT** `web/src/chat/footerMetrics.ts` | utility (projection) | transform | self — `totalPairsDropped:147` | exact |
| **EDIT** `web/src/chat/composer/skillPickerModel.ts` | model (pure) | transform | self — `QuickCommand` union `:12` + `QUICK_COMMANDS:52` | exact |
| **EDIT** `web/src/chat/composer/SkillPicker.tsx` | component | — | self — `COMMAND_ICON:24` | exact |
| **EDIT** `web/src/chat/composer/skillPickerModel.test.ts` | test (pure) | — | self — extend existing suite | exact |
| **EDIT** `web/src/chat/Composer.tsx` / `ExternalStoreChat.tsx` | component (dispatch) | request-response | self — `handleSelect` command switch `:137` | exact |

¹ `runner_compact.go` reuses the `WithoutCancel`+`WithTimeout` ctx-shaping of `maybeAutoTitle` but is **inline-bounded, NOT a background goroutine** (RESEARCH Pattern 3 — no new `r.wg` entry, no goleak surface).
² `ContextBudgetGauge` has **NO existing test** (verified: `__tests__/*Gauge*` glob empty) — this is a genuine Wave-0 gap. `RuntimeFooter.test.tsx` is the closest render+hook-mock analog.

---

## Pattern Assignments

### `internal/conversations/compact.go` (service, request-response) — NEW

**Analog:** `internal/conversations/title.go` — `generateTitle` (`:39`)

**Core LLM-over-history pattern to mirror** (`title.go:39-65`):
```go
func generateTitle(ctx context.Context, client llm.Client, model string, history []llm.Message) (string, error) {
	if client == nil { return "", fmt.Errorf("generate title: nil client") }
	req := llm.Request{
		Model: model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: titlePrompt},
			{Role: llm.RoleUser, Content: renderHistoryForTitle(history)},
		},
		Temperature: 0.3,
		MaxTokens:   32,
	}
	ch, err := client.Stream(ctx, req)
	if err != nil { return "", fmt.Errorf("generate title: stream: %w", err) }
	var b strings.Builder
	for chunk := range ch { // drain fully (interface contract)
		b.WriteString(chunk.Text)
	}
	title := sanitizeTitle(b.String())
	if title == "" { return "", fmt.Errorf("generate title: empty result") }
	return title, nil
}
```

**English-only prompt const precedent** (`title.go:14`):
```go
const titlePrompt = "You generate a concise 4-6 word title summarizing a conversation. " +
	"Reply with the title ONLY: no quotes, no trailing punctuation, no preamble."
```

**What to replicate:** the 2-message `llm.Request` (system const + rendered history), the full `for chunk := range ch` drain (the `llm.Client.Stream` contract — a partial drain leaks), the explicit `model string` param, empty-result → error (never persist an empty summary, SPEC Req#1).
**What differs:**
- `MaxTokens = opts.MaxOutputTokens` (the summary budget, not `32`).
- **Keep the trailing `Usage`** — accumulate the final chunk's `*llm.Usage` (title *discards* it; a compaction is a real billable call, SPEC Req#10). Return `CompactResult{Summary, TokensBefore, TokensAfter, Usage}`.
- `compactSystemPrompt` is the **9-section** governance-hardened const (D-06): 7 original headers + "All user messages" + "Errors and fixes" + verbatim safety-constraint preservation + a "reply in TEXT ONLY, call no tools" guard + the "Reply in English only" clause. The only per-call variation is `opts.Focus` appended as the "Compact Instructions" hook (SPEC Req#2 acceptance asserts the focus text rides `Request.Messages[0].Content`).
- `TokensBefore`/`TokensAfter` via `countTokens(enc, …)` (`tiktoken.go:90`, the L2/L2.5 encoder) — gating-grade, NOT the billing figure.
- **`dupl`-fold `renderHistoryForTitle`** if the render diverges from `title.go` only by the per-turn cap (CLAUDE.md dupl gate; RESEARCH Constraints).

---

### `internal/conversations/context_compaction.go` (utility, transform) — NEW (recommended split, D-B2 discretion)

**Analog:** `internal/conversations/context.go` — `alwaysBlockMarker` (`:48`) + `isAlwaysBlock` (`:303`)

**The marker + protection predicate to copy verbatim** (`context.go:48`, `:303`):
```go
const alwaysBlockMarker = "__aura_always_block__"

func isAlwaysBlock(t Turn) bool {
	return t.ToolCallID == alwaysBlockMarker && t.Role == llm.RoleUser
}
```

**What to replicate:** the identical marker-in-`ToolCallID` trick (a field a real persisted user turn NEVER sets → collision-proof, D-01/D-03):
```go
const compactionSummaryMarker = "__aura_compaction_summary__"

func isCompactionSummary(t Turn) bool {
	return t.ToolCallID == compactionSummaryMarker && t.Role == llm.RoleUser
}

// PURE — table-driven + mutation-≥70% target (RESEARCH). Keeps seq==1 (system L0)
// + every turn after the checkpoint (which naturally INCLUDES the summary turn,
// since summary_turn_seq > checkpoint_seq), dropping the pre-checkpoint body.
func truncateAtCheckpoint(turns []Turn, checkpointSeq int) []Turn {
	out := make([]Turn, 0, len(turns))
	for _, t := range turns {
		if t.Seq == 1 || t.Seq > checkpointSeq {
			out = append(out, t)
		}
	}
	return out
}
```
**Why this file exists:** `context.go` is **464 LOC** (RESEARCH Pitfall 7); the new pure symbols land here to keep `context.go` under the 600-LOC ceiling and give the pure logic a focused unit-test home. **Colocate all three marker touch-points' const + predicate here** (Pitfall 4: writer, protector, stripper must agree).
**What differs:** nothing structural — this is the always-block pattern re-keyed to the compaction marker.

---

### `internal/conversations/context.go` (utility, transform) — EDIT (three surgical touches)

**Analog:** self. Mirror the three existing always-block touch-points exactly.

**1. `injectAlwaysBlock` synthetic-turn shape** (`context.go:287`) — the summary turn is persisted (not injected), but carries the same marker field:
```go
always := Turn{Seq: alwaysBlockSeq, Role: llm.RoleUser, Content: block, ToolCallID: alwaysBlockMarker}
```

**2. `managedFromTurns` is where truncation lands** (`context.go:217`) — it is a `*Store` method (has `ctx`+`conversationID`), so it can do the one DB lookup then the pure transform:
```go
func (s *Store) managedFromTurns(ctx context.Context, conversationID string, turns []Turn, cfg ContextConfig) ([]llm.Message, error) {
	enc, err := encoder()
	if err != nil { return nil, fmt.Errorf("load managed history %s: tiktoken encoder: %w", conversationID, err) }
	return applyContextLadder(ctx, conversationID, turns, cfg, enc, s)
}
```
→ Insert BEFORE `applyContextLadder`: `latest, _ := s.LatestCompaction(ctx, conversationID); if latest != nil { turns = truncateAtCheckpoint(turns, latest.CheckpointSeq) }`. The summary then survives the `seq > checkpoint_seq` filter for free and `injectAlwaysBlock` puts it at `messages[2]` per D-02.

**3. `dropOldestPairs` protected-head split** (`context.go:386-392`) — add the summary right after the always-block guard:
```go
start := 0
if len(turns) > 0 && turns[0].Seq == 1 && turns[0].Role == llm.RoleSystem { start = 1 }
if len(turns) > start && isAlwaysBlock(turns[start]) { start++ }
// ADD: if len(turns) > start && isCompactionSummary(turns[start]) { start++ }
```

**4. `toMessages` marker strip** (`context.go:446-463`) — add an `isCompactionSummary` branch mirroring the `isAlwaysBlock` one:
```go
if isAlwaysBlock(t) {
	out = append(out, llm.Message{Role: llm.RoleUser, Content: t.Content})
	continue
}
// ADD the same for isCompactionSummary(t): emit a clean role=user message,
// marker stripped (a user message must not carry a tool_call_id on the wire — Pitfall 4).
```
**What to replicate:** all four are one- to three-line mirrors of the shipped always-block handling. **What differs:** only the DB lookup in touch #2 (`LatestCompaction`); everything else is pure.
**KV-cache invariant (D-02 / Pitfall 1):** `messages[0]` stays the `agent.SystemPrompt` const, byte-identical — extend the existing cache-stable-prefix assertion (`context_alwaysblock_test.go`) to a *compacted* conversation.

---

### `internal/conversations/store_compact.go` (store/model, CRUD atomic + projection) — NEW

**Analog A — atomic persist:** `internal/conversations/store_append.go` (`AppendTurnTx:116`, `allocateTurnSeq:195`, `insertTurnAndAggregates:251`) + the `db.WithTx` wrapper (`AppendTurn:64`).

**The tx-composable primitives to compose** (`store_append.go`):
```go
// :116 — exported for exactly this cross-write composition; requires Seq>0; rejects spill.
func (s *Store) AppendTurnTx(ctx context.Context, q *sqlc.Queries, p AppendTurnParams) error {
	if p.Seq <= 0 { return fmt.Errorf("append turn tx %s: %w", p.ConversationID, ErrSeqRequired) }
	if len(postgresTextSafe(p.Content)) > s.turnCapBytes {
		return fmt.Errorf("append turn tx %s seq %d: %w", p.ConversationID, p.Seq, ErrContentSpillUnsupported)
	}
	turn, agg, err := s.appendTurnWrites(p)
	if err != nil { return err }
	return insertTurnAndAggregates(ctx, q, turn, agg)
}

// :195 — row-locked monotonic seq inside the caller's tx.
func (s *Store) allocateTurnSeq(ctx context.Context, q *sqlc.Queries, conversationID string) (int, error) { … }
```

**The `db.WithTx` wrapper shape** (`store_append.go:64`):
```go
return db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
	return insertTurnAndAggregates(ctx, q, turn, agg)
})
```

**`AppendTurnParams` carries the Usage that folds aggregates automatically** (`store_append.go:34`) — this is the free cost-attribution path (COMPACT-10):
```go
type AppendTurnParams struct {
	ConversationID string
	Seq            int
	Role           string
	Content        string
	ToolCallID     string   // ← put compactionSummaryMarker HERE
	ToolCalls      []byte
	InputTokens    int
	OutputTokens   int      // appendTurnWrites folds these into total_*_tokens / total_cost_usd
	CachedTokens   int
	CostUSD        float64
}
```

**What to replicate:** `RecordCompaction` opens ONE `db.WithTx`, then inside it: `allocateTurnSeq` → `AppendTurnTx` (Role=`user`, `ToolCallID=compactionSummaryMarker`, `Input/OutputTokens/CachedTokens/CostUSD` = the compaction call's `Usage`) → `q.InsertConversationCompaction(...)`. A crash between the append and the watermark rolls BOTH back (SPEC Req#3 atomicity). Both `allocateTurnSeq` and `insertTurnAndAggregates` are in-package lowercase → `store_compact.go` in `package conversations` calls them directly.
**What differs:** you also insert the watermark row in the same tx; the summary `Content` (≈16 KB at the 4096-token default) stays well under `turnCapBytes` (65536) so `AppendTurnTx`'s spill-reject never fires (RESEARCH OQ2 — clamp/document only if an operator sets the budget above ~16k tokens).
**Anti-pattern (RESEARCH):** pass `checkpoint_seq = max(seq of the summarized snapshot)` INTO `RecordCompaction`; do NOT recompute `max(seq)` inside the tx (a turn racing in between `LoadHistory` and the tx would get folded under the checkpoint and silently dropped from context).

**Analog B — the `ListCompactions` / `LatestCompaction` projection:** `internal/conversations/context.go` — `ListContextRotEvents` (`:151`) and the `RotEvent` domain struct (`:139`).

**The projection + domain-struct template** (`context.go:139`, `:151`):
```go
type RotEvent struct {
	TS           string // RFC3339
	Action       string
	PairsDropped int
	TokensBefore int
	TokensAfter  int
}

func (s *Store) ListContextRotEvents(ctx context.Context, conversationID string) ([]RotEvent, error) {
	id, err := parseUUID("conversation_id", conversationID)
	if err != nil { return nil, fmt.Errorf("list context rot events: %w", err) }
	rows, err := s.q.ListContextRotEvents(ctx, id)
	if err != nil { return nil, fmt.Errorf("list context rot events %s: %w", conversationID, err) }
	out := make([]RotEvent, 0, len(rows))
	for _, r := range rows {
		ts := ""
		if r.Ts.Valid { ts = r.Ts.Time.Format(time.RFC3339) }
		out = append(out, RotEvent{TS: ts, Action: r.Action, PairsDropped: int(r.PairsDropped), …})
	}
	return out, nil
}
```
**What to replicate:** `parseUUID` guard → one sqlc call → map at the `pgtype` boundary (`.Valid` → `Format(time.RFC3339)`, `int32`→`int`). Declare a domain `Compaction` struct with **plain Go types + NO json tags** (the frontend reads the PascalCase names): `Compaction{ID, TS, Trigger, Model, CheckpointSeq, SummaryTurnSeq, TokensBefore, TokensAfter}`. `LatestCompaction` returns `*Compaction` (nil when none) for the reconstruction lookup.
**What differs:** `ListCompactions` orders `created_at DESC` (SPEC Req#10 — the newest first, vs rot-events' `ts ASC`). `GetLatestCompaction` must be deterministic: `ORDER BY created_at DESC, summary_turn_seq DESC LIMIT 1` (Pitfall 6 — two compactions can share a `created_at`).

---

### `internal/db/queries/conversation_compactions.sql` (query, sqlc) — NEW

**Analog:** `internal/db/queries/context_rot_events.sql`
```sql
-- name: InsertContextRotEvent :exec
INSERT INTO aura.context_rot_events (
    conversation_id, action, pairs_dropped, tokens_before, tokens_after
) VALUES ($1, $2, $3, $4, $5);

-- name: ListContextRotEvents :many
SELECT ts, conversation_id, action, pairs_dropped, tokens_before, tokens_after
FROM aura.context_rot_events
WHERE conversation_id = $1
ORDER BY ts ASC;
```
**What to replicate:** the `-- name: X :kind` sqlc annotation style, positional `$N` binds, explicit column list.
**What differs:** three queries — `InsertConversationCompaction :one` (RETURNING the row so `RecordCompaction` gets `id`/`created_at`), `GetLatestCompaction :one` (`ORDER BY created_at DESC, summary_turn_seq DESC LIMIT 1`), `ListCompactions :many` (`ORDER BY created_at DESC`). Workflow (Pitfall 5): write the migration + this file, then `sqlc generate` → `Querier` gains the three methods (`emit_interface:true` → the `fakeDBTX` unit tests can mock them).

---

### `internal/db/migrations/0036_conversation_compactions.{up,down}.sql` (migration, DDL) — NEW

**Analog:** `internal/db/migrations/0005_conversations.up.sql` — the `context_rot_events` table + grants (`:38-47`, `:64-65`).
```sql
CREATE TABLE aura.context_rot_events (
    ts              timestamptz NOT NULL DEFAULT now(),
    conversation_id uuid        NOT NULL,
    action          text        NOT NULL,
    pairs_dropped   integer     NOT NULL,
    tokens_before   integer     NOT NULL,
    tokens_after    integer     NOT NULL
);
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.context_rot_events  TO aura_app;
GRANT ALL                            ON aura.context_rot_events  TO aura_migrate;
```
**What to replicate:** the `aura.` schema prefix, `timestamptz DEFAULT now()`, the **two-line GRANT pair** (`aura_app` gets DML, `aura_migrate` gets ALL — CLAUDE.md: applies as `aura_migrate`, denied as `aura_app`), a `COMMENT ON TABLE`.
**What differs (SPEC Req#3 columns):** `id uuid pk`, `conversation_id uuid` with an explicit **`REFERENCES aura.conversations(id) ON DELETE CASCADE`** FK (rot-events lack the FK; compactions need it), `checkpoint_seq int NOT NULL`, `summary_turn_seq int NOT NULL`, `trigger text CHECK (trigger IN ('manual','auto'))`, `model text`, `tokens_before/after int`, `created_at timestamptz DEFAULT now()`, plus `CREATE INDEX … (conversation_id, created_at DESC)`. `.down.sql` = `DROP TABLE IF EXISTS aura.conversation_compactions;` (re-run no-op; reverses cleanly). Slot `0036` is the confirmed next free head after `0035_assets_source_kind_agent`.

---

### `internal/runner/runner_compact.go` (service, shared orchestration) — NEW (recommended)

**Analog:** `internal/runner/runner_resume.go` — `maybeAutoTitle` (`:19`), for the `WithoutCancel`+`WithTimeout` ctx-shaping ONLY.
```go
r.wg.Add(1)
go func() {
	defer r.wg.Done()
	ctx := context.WithoutCancel(turnCtx) // load-bearing: turnCtx cancels on Turn return
	ctx, cancel := context.WithTimeout(ctx, r.titleTimeout)
	defer cancel()
	title, gerr := conversations.GenerateTitle(ctx, r.client, r.cfg.Model, hist)
	if gerr != nil || title == "" { return }
	_ = r.Conv.SetTitleIfNull(ctx, convID, title)
}()
```
**What to replicate:** `context.WithoutCancel(ctx)` then `context.WithTimeout(ctx, r.compactTimeout)` so a client disconnect can't corrupt a half-written checkpoint (SPEC Req#11). Build ONE `Runner.Compact(ctx, convID, CompactRequest) (CompactOutcome, error)` that all 5 callers reach (D-08 mandate: shared, no duplicate impl): load raw history (`r.Conv.LoadHistory`) → D-04 floor check → `conversations.CompactConversation` → `r.Conv.RecordCompaction`.
**What CRITICALLY differs (RESEARCH Pattern 3 — the headline distinction):** auto-compaction is **INLINE-BOUNDED, NOT a `go func()` on `r.wg`.** The turn *waits* for the compacted history. No new goroutine → **no new goleak surface, no `r.wg` entry.** Use the ctx-shaping, drop the goroutine.

---

### `internal/runner/runner.go` (service, seam) — EDIT

**Analog:** self — `Deps`/`Runner` gain compact fields mirroring `EvictAfter`. **NOTE (42-04 FIX-1):** the `loadTurnHistory` rewrite shown below RELOCATES into `runner_compact.go` (cohesive auto-compaction seam; keeps `runner.go` < 600 LOC — it is 579 today). `runner.go`'s own edit is the field additions + `runner.New` constructor defaulting only; `loadTurnHistory` (`:485`) is deleted from here in the same task that recreates it in `runner_compact.go`.
```go
func (r *Runner) loadTurnHistory(ctx context.Context, convID string, cfg conversations.ContextConfig, branchLeaf int) ([]llm.Message, error) {
	if branchLeaf > 0 { return r.Conv.LoadManagedHistoryForBranch(ctx, convID, branchLeaf, cfg) }
	return r.Conv.LoadManagedHistory(ctx, convID, cfg)
}
```
**Deps + Runner field precedent** (`runner.go:86`, `:161`):
```go
EvictAfter   int    // AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS — L1 eviction age   (Deps :86)
evictAfter int      // (unexported Runner field :161)
```
**What to replicate:** thread `compactAutoEnabled bool` + `compactTimeout time.Duration` + `compactMaxOutput int` + `compactModel string` through `Deps` → `Runner` exactly like `EvictAfter`→`evictAfter`. Rewrite `loadTurnHistory` (relocated into `runner_compact.go`, 42-04 FIX-1) per RESEARCH Pattern 3: on `errors.Is(err, conversations.ErrContextWindowExceeded)` AND `r.compactAutoEnabled`, run `r.Compact(cctx, convID, CompactRequest{Trigger:"auto"})` once, re-load ONCE, else surface the ORIGINAL window error (no retry loop). `AURA_COMPACT_AUTO_ENABLED=false` → early-return the old dead-end unchanged (SPEC Req#8).

---

### `internal/runner/interfaces.go` (interface) — EDIT

**Analog:** self — `ConversationStore` (`:34`). `LoadHistory` is ALREADY present (`:44`).
```go
type ConversationStore interface {
	…
	AppendTurn(ctx context.Context, p conversations.AppendTurnParams) error
	LoadHistory(ctx context.Context, conversationID string) ([]llm.Message, error)
	LoadManagedHistory(ctx context.Context, conversationID string, cfg conversations.ContextConfig) ([]llm.Message, error)
	…
}
```
**What to replicate:** add `RecordCompaction(ctx, RecordCompactionParams) (conversations.Compaction, error)` + `LatestCompaction(ctx, conversationID) (*conversations.Compaction, error)` as new lines (narrow-interface convention: *"only the methods it calls"*). Extend `runner/fakes_test.go fakeConvStore` with a `RecordCompaction` spy + a manErr-once mode (RESEARCH — the daemon-free COMPACT-08 injection point is `fakeConvStore.manErr`).
**What differs:** nothing — this is the interface-widening precedent this file already embodies.

---

### `cmd/aura/chat.go` + `cmd/aura/chat_compact.go` (controller, CLI) — EDIT + NEW

**Analog:** self — `runChat` switch (`:43`) + the sibling handlers (`chatSetStatus:113`, `chatRename:129`) that boot via `bootChat(ctx)`.
```go
switch args[0] {
case "list":    chatList(args[1:])
case "resume":  chatResume(args[1:])
case "rename":  chatRename(args[1:])
case "search":  chatSearch(args[1:])
default:
	fmt.Fprintln(os.Stderr, chatUsage)
	os.Exit(1)
}
```
**Sibling handler shape** (`chat.go:129`):
```go
func chatRename(args []string) {
	if len(args) < 2 { fmt.Fprintln(os.Stderr, "usage: aura chat rename <id> <title>"); os.Exit(1) }
	ctx := context.Background()
	env := bootChat(ctx)
	defer env.close()
	if err := env.conv.Rename(ctx, args[0], args[1]); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
	fmt.Printf("ok: renamed %s\n", args[0])
}
```
**What to replicate:** add `case "compact": chatCompact(args[1:])` to the switch (+ update `chatUsage`). `chatCompact` boots via `bootChat(ctx)`, defers `env.close()`, resolves `--focus` via the existing `flagValue(args, "--focus")` helper (used by `chatSearch` for `--limit`), calls `env.run.Compact(ctx, id, …)`, prints `tokens_before → tokens_after (Δ%)` + the compaction id, `os.Exit(1)` on error.
**What differs:** the D-04 below-floor case prints "nothing to compact" and **exits 0** (not an error).
**Coverage note:** `cmd/aura` is excluded from the ≥85% floor (behaviorally covered) — the floor-critical logic (floor check, `--focus` parse) lives in the shared `Runner.Compact`, not here.

---

### `cmd/aura/chat_repl.go` (controller, REPL slash router) — EDIT

**Analog for the loop seam:** self — `chatLoop` (`:51`), `/exit` check (`:68`), turn dispatch (`:72`):
```go
if line == exitCommand {
	_, _ = fmt.Fprintln(d.out, "bye")
	return nil
}
if line != "" {
	if err := runUserTurn(ctx, d, line, reader); err != nil { … }
}
```
**Analog for the router itself:** `telegram/commands.go dispatchRich` (see below) — the REPL gains its FIRST slash router mirroring it.
**What to replicate:** a small `dispatchSlash(line) (handled bool, out string)` invoked immediately after the `/exit` check and before `runUserTurn`. `/compact [focus]` → `d.run.Compact(...)`, print the delta, **no `runner.Turn`**. `/bogus` → "unknown command; type your message or /exit" hint (never sent to the LLM). Non-slash input unchanged (SPEC Req#6).
**What differs:** unlike Telegram (Italian strings, `commandReply` struct), the REPL router returns a plain string and writes to `d.out`. Structure it so future slash commands slot in (table or switch).

---

### `internal/channels/telegram/commands.go` (controller, dispatch) — EDIT

**Analog:** self — `dispatchRich` (`:128`) + the `/clear` interception (`:153`, `:216`) + `helpText` (`:169`).
```go
func (c *commands) dispatchRich(ctx context.Context, chatID int64, text string) (handled bool, reply commandReply) {
	…
	switch name {
	case "/clear":
		return true, textReply(c.clear(ctx, chatID))
	…
	default:
		return true, textReply("Istruzione non riconosciuta. Usa /help per la lista dei comandi.")
	}
}

func (c *commands) clear(ctx context.Context, chatID int64) string {
	c.fireCancel(chatID)                                   // stop any in-flight turn FIRST
	if c.deps.Clear == nil { return "…non disponibile." }
	if err := c.deps.Clear.Delete(ctx, convID(chatID)); err != nil { return "…riprova." }
	return "Conversazione cancellata. Ricominciamo da capo."
}
```
**What to replicate:** add `case "/compact":` intercepted BEFORE any LLM turn (a handled command NEVER reaches `handleTurn` — the T-13-06 invariant); the handler compacts the chat's deterministic conversation (`convID(chatID)`) and replies with the token delta. Add a `compactBackend` seam to `commandDeps` (mirroring `clearBackend` `:50` — a consumer-side interface declared here so the dispatcher stays unit-testable with a double). Update `helpText` (`:169`) to advertise `/compact` (SPEC Req#7).
**What differs:** `/clear` calls `fireCancel` before deleting; `/compact` does not delete, so it need not cancel first — but note a concurrent live turn (RESEARCH Open-Q 1: pass `checkpoint_seq = max(summarized seq)`, don't recompute in-tx).

---

### `internal/config/config_knobs.go` + `config.go` + `cmd/aura/chat_boot.go` (config wiring) — EDIT

**Analog A — the registry rows:** `config_knobs.go knobRegistry()` (`:58`), Tier-B int/bool precedent (`:82-84`):
```go
{Name: "AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS", Kind: KindInt, Default: "10"},
{Name: "AURA_AGUI_CORS_PERMISSIVE",           Kind: KindBool, Default: "false"},
{Name: "AURA_SANDBOX_IMAGE",                   Kind: KindString, Default: "aura-sandbox:latest"},
```
**Analog B — the Config field + parse:** `config.go:62` / `:394`:
```go
ContextToolEvictAfterTurns int // AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS — L1 microcompact eviction age
…
ContextToolEvictAfterTurns: envutil.IntDefault("AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS", 10),
```
**Analog C — the composition-root thread:** `chat_boot.go assembleChatEnv` `Deps{… EvictAfter: cfg.ContextToolEvictAfterTurns …}` (`:337`).
**What to replicate:** register 4 `KnobSpec` rows — `AURA_COMPACT_AUTO_ENABLED` (`KindBool`, "true"), `AURA_COMPACT_MAX_OUTPUT_TOKENS` (`KindInt`, "4096"), `AURA_COMPACT_TIMEOUT_SEC` (`KindInt`, "60"), `AURA_COMPACT_MODEL` (`KindString`, ""). Add matching `Config` fields; parse the bool/int via `envutil.BoolDefault`/`IntDefault`. Thread all four into `runner.Deps` in `assembleChatEnv` exactly like `EvictAfter`. `aura config validate` picks up the reparse checks for free ("the registry IS the engine").
**What differs (A1):** `envutil` has **NO `StringDefault`** (verified) — read `AURA_COMPACT_MODEL` via `os.Getenv` directly (empty default is intended, D-05 = same model as conversation). `KindString` rows carry no reparse check, matching this.

---

### `internal/agui/conversations_api.go` (controller, routes) — EDIT — GET read + POST trigger

**Analog for the GET read route:** `handleConversationRotEvents` (`:219`) + its mux registration (`:52`):
```go
mux.HandleFunc("GET /api/conversations/{id}/rot-events", s.handleConversationRotEvents)
…
func (s *Server) handleConversationRotEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseConvID(w, r)
	if !ok { return }
	// Owner gate (MUSR-01 / D-06): a foreign/absent id 404s before the unscoped read.
	if _, err := s.conv.GetForIdentity(r.Context(), id, scopedIdentityID(r.Context())); err != nil {
		writeStoreErr(w, err)
		return
	}
	events, err := s.conv.ListContextRotEvents(r.Context(), id)
	if err != nil { writeStoreErr(w, err); return }
	writeJSON(w, events)   // ← serializes the DOMAIN struct directly; NO agui DTO
}
```
**Analog for the POST trigger route (body + DoS cap):** `handleRenameConversation` (`:244`):
```go
r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)   // T-12-12 DoS guard
var body renameBody
if err := json.NewDecoder(r.Body).Decode(&body); err != nil { http.Error(w, "invalid request body", http.StatusBadRequest); return }
```
**What to replicate:**
- `GET /api/conversations/{id}/compactions` = a verbatim copy of `handleConversationRotEvents` with `s.conv.ListCompactions` swapped in (owner-gate → `writeStoreErr` → `writeJSON`). Register on the mux next to the rot-events line (`:52`).
- `POST /api/conversations/{id}/compact` = the owner-gate + `MaxBytesReader` cap from rename, then call `s.run.Compact(...)` (the AG-UI server holds a `Runner`), return `{tokens_before, tokens_after, compaction_id}` via `writeJSONStatus`. Below-floor (D-04) → a 200 "nothing to compact" non-error notice, not an error.
**What differs:** the POST is a *mutating* route so it MUST route through the Runner (not a raw store call) and MUST owner-gate (V4 IDOR — SECURITY domain). Errors go through `sanitizeErr`/`writeStoreErr` (V7 — a runner error can embed a DSN).

---

### `internal/agui/server.go` + `types.go` (interfaces) — EDIT

**Analog A — `Runner` iface (server.go:53):**
```go
type Runner interface {
	Turn(ctx context.Context, convID string, userMsg *string) iter.Seq2[*agent.Event, error]
	SubmitAnswers(ctx context.Context, answers map[string]runner.ResponseInput) (int, error)
	NewConversation(ctx context.Context) (string, error)
	DeleteConversationLifecycle(ctx context.Context, identityID, convID string) (int64, error)
}
```
→ add `Compact(ctx context.Context, convID string, req runner.CompactRequest) (runner.CompactOutcome, error)`; extend the `scriptedRunner` test double with a `Compact` spy.

**Analog B — `ConversationStore` iface (types.go, `ListContextRotEvents:38`):**
```go
type ConversationStore interface {
	Get(ctx context.Context, conversationID string) (conversations.Conversation, error)
	LoadHistory(ctx context.Context, conversationID string) ([]llm.Message, error)
	…
	ListContextRotEvents(ctx context.Context, conversationID string) ([]conversations.RotEvent, error)
	GetForIdentity(ctx context.Context, conversationID, identityID string) (conversations.Conversation, error)
}
```
→ add `ListCompactions(ctx, conversationID) ([]conversations.Compaction, error)` right beside `ListContextRotEvents`; extend the `agui.fakeConvStore` double.
**What differs:** `NewServer(run, conv, cfg)` (`server.go:160`) signature is UNCHANGED — the new methods ride the existing narrow interfaces. **No new JSON DTO in `types.go`** (see the top-of-file correction).

---

### `web/src/conversations/useConversations.ts` (hook, data layer) — EDIT

**Analog:** self — `RotEvent` interface (`:42`) + `useConversationRotEvents` (`:160`) + the read-key const (`:106`):
```ts
/** GET /api/conversations/{id}/rot-events — microcompact ladder audit row (D-11). */
export interface RotEvent {
	readonly TS: string;
	readonly Action: string;
	readonly PairsDropped: number;
	readonly TokensBefore: number;
	readonly TokensAfter: number;
}
export const CONVERSATION_ROT_EVENTS_KEY = 'conversation-rot-events';

export function useConversationRotEvents(conversationId: string) {
	return useQuery({
		queryKey: [CONVERSATION_ROT_EVENTS_KEY, conversationId],
		queryFn: () => getJSON<RotEvent[]>(`/api/conversations/${encodeURIComponent(conversationId)}/rot-events`),
		enabled: conversationId.length > 0,
		retry: false,
	});
}
```
**Post-mutation invalidate precedent** (`useRenameConversation:176`): `onSuccess → queryClient.invalidateQueries({ queryKey: [CONVERSATIONS_KEY] })`.
**What to replicate:** add a `Compaction` interface with **PascalCase** fields matching the Go domain struct (`TS`, `Trigger`, `Model`, `CheckpointSeq`, `SummaryTurnSeq`, `TokensBefore`, `TokensAfter`), a `CONVERSATION_COMPACTIONS_KEY`, a `useConversationCompactions(id)` query (verbatim shape, `/compactions` URL), and a `useCompactConversation()` mutation (POST `/api/conversations/{id}/compact`) whose `onSuccess` invalidates the compactions key so the gauge marker refreshes. `credentials: 'same-origin'` + `retry: false` are mandatory (the file header contract).
**What differs:** the POST mutation returns `{tokens_before, tokens_after, compaction_id}` (the composer renders the delta as a toast) — note this response uses **snake_case** (it's a hand-written handler struct, not a store projection), unlike the PascalCase read projections.

---

### `web/src/chat/ContextBudgetGauge.tsx` + `footerMetrics.ts` (component + projection) — EDIT

**Analog A — the marker block (ContextBudgetGauge.tsx:38, :78):**
```tsx
const { data: rotEvents } = useConversationRotEvents(conversationId);
const dropped = totalPairsDropped(rotEvents ?? []);
…
{dropped > 0 ? (
	<p className="font-mono text-[0.75rem] text-text-muted">
		{t('footer.compacted', { count: dropped })}
	</p>
) : null}
```
**Analog B — the aggregation (footerMetrics.ts:147):**
```ts
/** Total pairs dropped by the microcompact ladder (sum of pairs_dropped events). */
export function totalPairsDropped(events: readonly { readonly PairsDropped: number }[]): number {
	return events.reduce((sum, e) => sum + finiteNumber(e.PairsDropped, 0), 0);
}
```
**What to replicate:** consume `useConversationCompactions(conversationId)`, add a sibling `totalSummarized(compactions)` (or `compactionCount`) aggregation in `footerMetrics.ts`, render a SECOND marker line when it's > 0.
**What CRITICALLY differs (Pitfall 8 / D-10):** the i18n key **`footer.compacted` is ALREADY taken** by the L2.5 rot-events marker — you MUST use a NEW distinct key (e.g. `footer.summarized`) AND a **visually distinct glyph/color** (D-10: "different glyph/color, not just a tooltip"), so an operator distinguishes an LLM summary-checkpoint from a hard-drop. Follow CLAUDE.md §Frontend_aesthetics (distinctive, not "AI slop").

---

### `web/src/chat/composer/skillPickerModel.ts` + `SkillPicker.tsx` + `skillPickerModel.test.ts` (composer palette) — EDIT

**Analog A — the QuickCommand union + catalog (skillPickerModel.ts:12, :52):**
```ts
export type QuickCommand = 'add-files' | 'new-chat' | 'clear';

const QUICK_COMMANDS: readonly QuickCommandSpec[] = [
	{ command: 'add-files', labelKey: `${KEY_PREFIX}.cmdAddFiles`, subtitleKey: `${KEY_PREFIX}.cmdAddFilesSubtitle` },
	{ command: 'new-chat',  labelKey: `${KEY_PREFIX}.cmdNewChat`,  subtitleKey: `${KEY_PREFIX}.cmdNewChatSubtitle` },
	{ command: 'clear',     labelKey: `${KEY_PREFIX}.cmdClear`,    subtitleKey: `${KEY_PREFIX}.cmdClearSubtitle` },
];
```
**Analog B — the icon map (SkillPicker.tsx:24):**
```tsx
const COMMAND_ICON: Record<QuickCommand, LucideIcon> = {
	'add-files': Paperclip,
	'new-chat': MessageSquarePlus,
	clear: Eraser,
};
```
**What to replicate:** add `'compact'` to the `QuickCommand` union, a `{ command: 'compact', labelKey, subtitleKey }` catalog row (its identifier `'compact'` doubles as the filter corpus), and a distinct Lucide glyph in `COMMAND_ICON` (e.g. `Combine`/`FoldVertical` — NOT the `Eraser` used for `clear`). Extend `skillPickerModel.test.ts` (the pure model is the coverage/mutation target): the `filterPickerItems` / `commandIds` assertions must include `'compact'`.
**What differs:** nothing structural — the union + catalog + icon map are designed for exactly this addition.

---

### `web/src/chat/Composer.tsx` / `ExternalStoreChat.tsx` (component dispatch) — EDIT

**Analog:** the `handleSelect` command switch (`Composer.tsx:137`):
```tsx
if (item.kind === 'skill') {
	onPinSkill?.({ name: item.name, description: item.description, type: item.type });
} else if (item.command === 'add-files') {
	fileInputRef.current?.click();
} else if (item.command === 'new-chat') {
	void onNewChat?.();
} else { // clear
	onPinSkill?.(null);
	clearPendingAttachments();
}
```
**What to replicate:** add an `else if (item.command === 'compact')` branch that fires the `useCompactConversation()` mutation for the active conversation and renders the returned token delta as a system/toast line (the below-floor response surfaces as a non-error notice). The dispatch wiring lives where the other QuickCommands are handled (Composer owns the palette; `ExternalStoreChat` owns `onNewChat` and the conversation id).
**What differs:** `add-files`/`new-chat`/`clear` are pure client actions; `/compact` is the first QuickCommand that hits a **server mutation** (POST) — model it on the `useCreateConversation`/mutation pattern, not the pure-client resets.

---

### `web/src/chat/ContextBudgetGauge.test.tsx` (test, component render) — NEW

**Analog:** `web/src/chat/__tests__/RuntimeFooter.test.tsx` (the gauge has **NO** existing test — Wave-0 gap, Pitfall 8).
```tsx
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';

function stubFetch(opts?: { rotEvents?: { PairsDropped: number }[] }) {
	return vi.fn((input: RequestInfo | URL) => {
		const url = urlOf(input);
		if (url.includes('/rot-events')) {
			return Promise.resolve(new Response(JSON.stringify(
				(opts?.rotEvents ?? []).map((e) => ({ TS: '…', Action: 'hard_drop_pairs', PairsDropped: e.PairsDropped, … })),
			), { status: 200 }));
		}
		return Promise.resolve(new Response(JSON.stringify(opts?.agg ?? AGG), { status: 200 }));
	});
}
// render wrapped in <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
```
**What to replicate:** the `QueryClientProvider` wrapper + `import '../../i18n/i18n'` + a `stubFetch` that branches on the URL and returns a JSON `Response` with the **Go PascalCase** field names. Add a `/compactions` branch returning `Compaction[]` fixtures. Assert the compaction marker renders with the NEW i18n key + distinct glyph, and is DISTINCT from the `pairs_dropped` marker.
**What differs:** the analog stubs `/rot-events`; the new test stubs BOTH `/rot-events` and `/compactions` to prove the two markers coexist and are visually distinguishable (D-10).

---

## Shared Patterns

### Marker-in-`ToolCallID` protection (highest leverage — D-01/D-03, Pitfall 4)
**Source:** `internal/conversations/context.go:48` (`alwaysBlockMarker`) + `:303` (`isAlwaysBlock`) + `:390` (dropOldestPairs split) + `:452` (toMessages strip).
**Apply to:** `context_compaction.go` (declare `compactionSummaryMarker` + `isCompactionSummary`), `store_compact.go` (write it in `AppendTurnParams.ToolCallID`), `context.go` (protect in `dropOldestPairs`, strip in `toMessages`). The const + predicate MUST be colocated and consistent across all three touch-points, or the summary leaks a `tool_call_id` to the provider OR L2.5 drops it on a long conversation.

### Atomic checkpoint persist (SPEC Req#3, Pitfall 2)
**Source:** `store_append.go:64` (`db.WithTx` wrapper) + `:116` (`AppendTurnTx`) + `:195` (`allocateTurnSeq`) + `:251` (`insertTurnAndAggregates`).
**Apply to:** `store_compact.go RecordCompaction` — summary turn append + watermark insert in ONE `db.WithTx`; a failure rolls both back. Daemon-free rollback coverage via the `store_fakedbtx_test.go` fake-`DBTX` pattern; SQL correctness via `db_integration`.

### Cost attribution rides `AppendTurnParams.Usage` (SPEC Req#10, A3)
**Source:** `store_append.go:213` (`appendTurnWrites`) folds `Input/OutputTokens/CachedTokens/CostUSD` into `total_*` aggregates automatically.
**Apply to:** put the compaction call's `Usage` in the summary turn's `AppendTurnParams` → `total_input_tokens/total_output_tokens/total_cost_usd` rise with NO separate aggregate call. Confirm no OTHER code path also folds the same usage (double-count guard).

### Owner-gate every AG-UI route (V4 IDOR — SECURITY domain)
**Source:** `conversations_api.go:224/261` — `s.conv.GetForIdentity(ctx, id, scopedIdentityID(ctx))` before any read/mutate; `parseConvID` → 404 before the store touch; `MaxBytesReader(w, r.Body, maxRunBodyBytes)` on POST; `sanitizeErr`/`writeStoreErr` on every error path (a runner error can embed a DSN).
**Apply to:** BOTH new routes (`GET /compactions`, `POST /compact`).

### Command-interception-before-LLM-turn (SPEC Req#6/#7)
**Source:** `telegram/commands.go:123` — `dispatch` returns `handled=true` for every `/command` so it NEVER reaches `handleTurn`; the consumer-side backend seam (`clearBackend:50`) keeps it unit-testable.
**Apply to:** the REPL `dispatchSlash` and the Telegram `/compact` case — a handled `/compact` runs the compaction call only, never a `runner.Turn`.

### Frontend read chain: read-route → domain-struct-as-wire → typed hook → gauge marker (D-09)
**Source:** `conversations_api.go handleConversationRotEvents:219` → `conversations.RotEvent` (serialized directly) → `useConversations.ts useConversationRotEvents:160` + `RotEvent:42` → `footerMetrics.ts totalPairsDropped:147` → `ContextBudgetGauge.tsx:78`.
**Apply to:** the entire compactions read path — copy each link, swapping the store method + PascalCase struct + i18n key + glyph.

---

## No Analog Found

None. Every file maps to a shipped, HEAD-verified analog. Two nuances the planner should treat as "adapt, not copy":
- **9-section compaction prompt (D-06):** `title.go titlePrompt` is the *structural* const precedent (English-only, package `const`), but the 9-section governance-hardened content is an **adaptation target** (Claude Code `compact.md` + the "Governance Decay" arxiv), not a codebase copy. Its acceptance test (9 headers + English-only clause + no-tools guard) has no existing assertion to extend — it is net-new.
- **`runner_compact.go` inline-bounded lifecycle:** `maybeAutoTitle` is the ctx-shaping analog but is a *goroutine*; the compaction auto path deliberately **drops the goroutine** (inline) — do not copy the `r.wg.Add(1)/go func()` scaffold.

---

## Metadata

**Analog search scope:** `internal/conversations`, `internal/runner`, `internal/config`, `internal/agui`, `internal/channels/telegram`, `internal/db/{migrations,queries}`, `cmd/aura`, `web/src/{chat,chat/composer,conversations}`.
**Files scanned (opened + excerpted):** 22 source analogs + migration DDL grep + 2 frontend test analogs.
**Migration head verified:** `0035_assets_source_kind_agent` (→ `0036` free).
**Key confirmation vs CONTEXT/RESEARCH:** `envutil` has no `StringDefault` (A1 holds — `AURA_COMPACT_MODEL` reads via `os.Getenv`); AG-UI rot-events serialize the DOMAIN struct with NO json tags (so the "compaction wire type" is the domain `conversations.Compaction`, and the `types.go` edit is an interface widening, not a DTO).
**Pattern extraction date:** 2026-07-12
