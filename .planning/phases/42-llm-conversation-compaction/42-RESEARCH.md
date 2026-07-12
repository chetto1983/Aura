# Phase 42: LLM Conversation Compaction - Research

**Researched:** 2026-07-12
**Domain:** Go agent-runtime context management (LLM summarization + non-destructive checkpoint persistence) + AG-UI/React cockpit surfaces
**Confidence:** HIGH (all code seams verified by direct source read at HEAD; no new external dependencies)

> **Scope note (per orchestrator):** The external domain/parity research (Claude Code + GPT-5.5/Codex compaction prompts, "Governance Decay" arxiv, open-webui, 5–20k-token trigger ranges) was already done during `/gsd-discuss-phase` and folded into D-04/D-05/D-06. This document does **not** re-derive the domain. Its three deliverables are: (1) the **Validation Architecture** (mandatory — feeds VALIDATION.md + the Nyquist Dimension-8 gate under the ≥85% floor / mutation ≥70% / goleak / -race regime); (2) **code-seam confirmation** (every `file:line` pointer from CONTEXT re-verified at HEAD) + the landmines the planner must design around; (3) concrete **"how to test this without a live daemon"** guidance so DB-gated code doesn't contribute zero coverage.

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (D-01 … D-10)

- **D-01 — Summary-turn marker:** Summary persisted as one `role='user'` turn carrying marker `__aura_compaction_summary__` in `ToolCallID` — identical trick to the proven `alwaysBlockMarker` (`internal/conversations/context.go:48`, `isAlwaysBlock` at `:303`). Rejected the dedicated-synthetic-role alternative.
- **D-02 — Placement `messages[2]`:** After `messages[0]` system L0 and `messages[1]` always-block. `messages[0]` stays byte-identical → KV-cache prefix survives (Phase-6 invariant); cache invalidates only from the summary turn onward. Reconstructed history = `[system, always-block, summary, turns with seq > checkpoint_seq]`.
- **D-03 — Protection:** Summary turn protected like the always-block: `isCompactionSummary(t)` (keyed on the marker) makes `applyL1` and `dropOldestPairs` never touch it; `toMessages` strips the marker so it renders as a clean `role='user'` message.
- **D-04 — Derived min-compact floor (no new knob):** `/compact` is a no-op ("nothing to compact", exit 0, no row) when body turns ≤ ~3 **OR** estimated input tokens < `2 × AURA_COMPACT_MAX_OUTPUT_TOKENS`. Derived from existing values → config surface stays at the 4 SPEC Req#9 knobs. (Claude's discretion: exact `~3` and `2×` constants tunable within this rationale; the *shape* — derived, no new knob, floor ≥ some multiple of the summary budget — is locked.)
- **D-05 — `AURA_COMPACT_MODEL` default `""`:** Same model as the conversation. Compaction is rare + load-bearing (a weak summary poisons every subsequent turn — governance-decay). `AURA_COMPACT_MODEL` is the cost-sensitive escape hatch. Rejected shipping a cheap default.
- **D-06 — 9-section prompt schema:** Adopt the **newer 9-section** Claude Code compaction schema (7 original sections + **"All user messages"** + **"Errors and fixes"**), plus **verbatim preservation of user-stated security/safety constraints** and a **"reply in TEXT ONLY, call no tools"** guard. Aura specifics on top: Aura-neutral framing, "Reply in English only" clause (matches `titlePrompt`), explicit output-length cap bounded to `AURA_COMPACT_MAX_OUTPUT_TOKENS`. **Planner note:** the Req#2 acceptance test extends from 7 → **9** section headers + English-only clause + no-tools guard.
- **D-07 — Not a PRD deviation:** Activates the L3 deferral in `04-SPEC.md` (Req#10 + Out-of-scope) and PRD §1.8 OQ#3. No PRD-amendment commit. Fold a one-line "activated in Phase 42" note into `04-SPEC.md`'s L3 entry and PRD §1.8 OQ#3 (documentation only, ~2-line touch) within this phase's scope.
- **D-08 — Web manual `/compact` trigger:** `/compact` as a `QuickCommand` in the composer palette (`web/src/chat/composer/skillPickerModel.ts` QuickCommand union + `SkillPicker.tsx` icon map) wired to a **new AG-UI POST route** (`internal/agui/conversations_api.go`, sibling to the rot-events handler) running the **shared** server-side compaction path and returning `{tokens_before, tokens_after, compaction_id}`. Below-floor (D-04) → "nothing to compact" non-error notice.
- **D-09 — Compaction markers on `ContextBudgetGauge`:** `GET /api/conversations/{id}/compactions` (thin `ListCompactions` wrapper — exact sibling of `handleConversationRotEvents`) + typed client hook, rendered as markers on `web/src/chat/ContextBudgetGauge.tsx`, **visually distinct** from the rot-events (`pairs_dropped`) markers. Read-path only, purely additive.
- **D-10 — Frontend quality bar:** New/edited React UI follows CLAUDE.md §Frontend_aesthetics (distinctive, not "AI slop"). Compaction marker must be visually distinguishable (different glyph/color, not just a tooltip). Frontend tests follow the existing `web/src` vitest + testing-library convention. The Go ≥85% coverage floor is a **Go-owned-surface** metric; the `web/src` suite is governed by its own frontend CI (`vitest`), **not** `scripts/coverage_gate.sh`.

### Claude's Discretion
- Exact numeric constants inside the D-04 derived floor (the "~3 body turns" + `2×` multiplier).
- Whether the checkpoint reconstruction logic lives in `context.go` or splits into `context_compaction.go` (SPEC flags `context.go` as large; refactor-on-touch / ≤600 LOC governs). **This research recommends the split — see Pitfall 7.**

### Deferred Ideas (OUT OF SCOPE)
- Pre-L2.5 "L2.4" proactive auto-compaction tier (compact *before* the lossy hard-drop). Manual `/compact` is the proactive tool for now.
- Richer compaction UI beyond D-08/D-09 (dedicated history panel, per-compaction preview/diff, undo).
- Neo4j long-term memory spill of summarized rounds (Phase 15 territory).
- Branch-leaf persistence (migration 0017 `ForkBranch`) — the rejected alternative to checkpoint-watermark.
- Multimodal/image-turn summarization; automatic re-compaction beyond the single bounded auto-attempt; changing L1/L2.5 behavior.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description (from SPEC) | Research Support (verified seam / enabling asset) |
|----|------------------------|---------------------------------------------------|
| COMPACT-01 | `CompactConversation` — single bounded LLM summarization call | `title.go` `generateTitle` (verified) is the exact structural template: 2-message `llm.Request`, drains `client.Stream`, explicit `model string`. Compaction differs by `MaxTokens=opts.MaxOutputTokens` + it keeps `Usage`. Stub-`llm.Client` test pattern already exists (`title_unit_test.go:50 TestGenerateTitle_StreamError`). |
| COMPACT-02 | Adapted 9-section prompt constant (D-06) | `titlePrompt` (`title.go:14`) is the English-only package-`const` precedent. Pure string constant → trivial unit assertion. |
| COMPACT-03 | Migration `0036` + sqlc + `store_compact.go` atomic persist | Migration head verified `0035_assets_source_kind_agent` → `0036` is next free slot. `AppendTurnTx(ctx, q, p)` (`store_append.go:116`) + `allocateTurnSeq` (`:195`) + `insertTurnAndAggregates` (`:251`) compose an in-package atomic tx. `context_rot_events.sql` is the query template. |
| COMPACT-04 | Checkpoint-aware reconstruction | `managedFromTurns` (`context.go:217`) is the truncation site; `injectAlwaysBlock`/`isAlwaysBlock`/`dropOldestPairs`/`toMessages` all verified — truncation reduces to a **pure** `truncateAtCheckpoint(turns, checkpointSeq)` + a marker predicate. |
| COMPACT-05 | Manual `/compact` — CLI `aura chat compact <id> [--focus]` | `runChat` switch verified at `chat.go:43` (`list\|new\|resume\|…`) — add `case "compact"`. |
| COMPACT-06 | Manual `/compact` — REPL slash router | `chatLoop` verified at `chat_repl.go:51`; `/exit` check at `:68`, turn dispatch at `:72` — `dispatchSlash` slots between. "testable REPL core" per the file header. |
| COMPACT-07 | Manual `/compact` — Telegram | `dispatchRich` switch verified at `commands.go:134` (`/start /help /cost /search /clear …`) — add `case "/compact"` + `helpText` update. |
| COMPACT-08 | Auto-fallback at `ErrContextWindowExceeded` | `loadTurnHistory` (`runner.go:485`) is the seam; `ErrContextWindowExceeded` (`context.go:62`) verified. `fakeConvStore.manErr` (`fakes_test.go:54`, "injectable LoadManagedHistory error") is the daemon-free injection point. |
| COMPACT-09 | `AURA_COMPACT_*` knobs + wiring | `knobRegistry()` (`config_knobs.go:58`) + `KindBool/KindInt/KindString` verified. `envutil.BoolDefault`/`IntDefault` verified. `Config.ContextToolEvictAfterTurns` (`config.go:62`/`:394`) + `runner.Deps.EvictAfter` (`chat_boot.go:337`) are the mirror pattern. |
| COMPACT-10 | Audit + cost attribution | Because the summary turn is appended with the compaction `Usage` in its `AppendTurnParams`, `appendTurnWrites` (`store_append.go:213`) folds `total_input/output_tokens/total_cost_usd` **automatically** — no separate aggregate call. `ListContextRotEvents` (`context.go:151`) is the `ListCompactions` projection template. |
| COMPACT-11 | Bounded, best-effort, never-corrupting | `maybeAutoTitle` (`runner_resume.go:19`) is the `WithoutCancel`+`WithTimeout` precedent; the persist is one `db.WithTx` (atomic rollback). NB: auto-compaction is **inline-bounded**, not a background goroutine — see Pattern 3. |
</phase_requirements>

## Summary

Phase 42 is an **integration/composition** phase, not a greenfield one: it introduces **zero new external dependencies**, reuses the vendored `tiktoken-go` encoder, the `llm.Client` streaming interface, `pgx`+`sqlc`, and mirrors two fully-proven code patterns — the auto-title worker (`title.go` + `runner.maybeAutoTitle`) for the LLM-over-history call and lifecycle, and the always-block marker/protection pattern (`context.go`) for the summary turn. Every `file:line` pointer in `42-CONTEXT.md` was re-verified at HEAD and **all hold** (see Code Seam Verification). The migration head is confirmed `0035` → `0036` is the next free slot.

The single most important architectural finding is that the phase decomposes cleanly into **pure logic** (daemon-free unit-testable) and a thin **DB-gated** shell. The summarization call, the 9-section prompt constant, the checkpoint **truncation** (`truncateAtCheckpoint` is a pure function once `checkpoint_seq` is known), the marker protection (`isCompactionSummary`), the marker strip in `toMessages`, the token math, the D-04 min-floor derivation, the auto-fallback control flow (injectable via `fakeConvStore.manErr`), the config knobs, the CLI/REPL/Telegram dispatch, and both new AG-UI handlers (tested with `scriptedRunner`/`fakeConvStore` doubles) are **all daemon-free**. Only the SQL execution itself — `RecordCompaction`'s atomic tx, `LatestCompaction`, `ListCompactions`, and the aggregate/FTS *integration* assertions — genuinely requires `db_integration`. This split is the backbone of the Validation Architecture and is what keeps the ≥85% owned-surface floor reachable.

**Primary recommendation:** Build one shared `Runner.Compact(ctx, convID, CompactRequest) (CompactOutcome, error)` method that all 5 callers invoke (auto-fallback + CLI + REPL + Telegram + web POST). Push every reconstruction/protection/floor decision into pure functions in a new `internal/conversations/context_compaction.go`, unit-test them exhaustively (table-driven + byte-equality + mutation ≥70%), and reserve `db_integration` for the store's SQL correctness, atomic-rollback, aggregate-attribution, and FTS-still-matches assertions. Persist the summary turn with the compaction `Usage` in its `AppendTurnParams` so cost attribution (COMPACT-10) falls out of the existing aggregate path for free.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| LLM summarization call | `internal/conversations` (`compact.go`) | `internal/runner` (owns `llm.Client`, passes it in) | Mirrors `title.go`: the package holds the prompt+shaping; the Runner owns the client + lifecycle. |
| Checkpoint persistence (atomic) | `internal/conversations` (`store_compact.go`) | Postgres (migration `0036` + sqlc) | In-package composition of `allocateTurnSeq`+`AppendTurnTx`+`InsertConversationCompaction` in one `db.WithTx`. |
| History reconstruction / truncation | `internal/conversations` (`context.go`/`context_compaction.go`) | — | Pure transform of the loaded turn list; the only DB touch is `LatestCompaction` lookup. |
| Compaction orchestration (shared) | `internal/runner` (`Runner.Compact`) | — | The Runner is the single seam all 5 trigger surfaces reach (it holds client + model + store). |
| Auto-fallback control flow | `internal/runner` (`loadTurnHistory`) | — | Inline at the `ErrContextWindowExceeded` dead-end, strictly after L2.5. |
| Config knobs | `internal/config` | `cmd/aura/chat_boot.go` (wiring) | Registry is the validation engine; `assembleChatEnv` threads into `runner.Deps`. |
| Manual triggers (CLI/REPL/Telegram) | `cmd/aura` + `internal/channels/telegram` | `internal/runner` (shared `Compact`) | Thin dispatch → shared path. cmd/aura excluded from Go coverage floor (behaviorally covered). |
| Web trigger (POST) + gauge markers (GET) | `internal/agui` (routes) | `web/src/chat` (React) | AG-UI holds both `Runner` + conv store; React consumes via typed hooks. Web has its own vitest gate. |

## Standard Stack

**No new packages.** This phase is built entirely from already-vendored, already-in-use dependencies. This is a deliberate de-risking property — there is no supply-chain surface to audit.

### Core (all pre-existing)
| Library | Version | Purpose | Why Standard (here) |
|---------|---------|---------|---------------------|
| `github.com/pkoukk/tiktoken-go` | vendored (cl100k_base blob embedded, `tiktoken.go`) | `TokensBefore`/`TokensAfter` gating-grade counts | Already the L2/L2.5 budget encoder; offline, CI-deterministic. `[VERIFIED: internal/conversations/tiktoken.go]` |
| `internal/llm` `Client` | in-repo | `Stream(ctx, Request) (<-chan Chunk, error)` | The provider-neutral streaming seam `title.go` already drains. `[VERIFIED: internal/conversations/title.go:52]` |
| `github.com/jackc/pgx/v5` + `internal/db/sqlc` | in-repo | migration `0036` + generated queries | `sql_package: pgx/v5`, `emit_interface: true`. `[VERIFIED: sqlc.yaml]` |
| `go.uber.org/goleak` | in-repo | leak detection (db_integration TestMain) | `[VERIFIED: internal/conversations/main_test.go]` |

### Frontend (all pre-existing)
| Library | Purpose | Why Standard (here) |
|---------|---------|---------------------|
| `vitest` + `@vitest/coverage-v8` | web test + coverage | `"test": "vitest run --coverage"`. `[VERIFIED: web/package.json:21]` |
| `@stryker-mutator/vitest-runner` | frontend mutation testing | Present in devDeps → web mutation spot-check is available. `[VERIFIED: web/package.json:69]` |
| `react-i18next` | marker labels | `ContextBudgetGauge` already uses `t('footer.compacted', …)`. `[VERIFIED: web/src/chat/ContextBudgetGauge.tsx]` |

**Installation:** none. `sqlc generate` (already in the toolchain) regenerates the query layer after the migration + query file land.

## Package Legitimacy Audit

**No external packages are installed by this phase.** Every dependency is already vendored and in production use (`tiktoken-go`, `pgx`, `sqlc`, `goleak`, `vitest`, `react-i18next`). slopcheck / registry verification is **N/A** — there is nothing new to verify.

| Package | Registry | Disposition |
|---------|----------|-------------|
| (none) | — | No new dependencies introduced by Phase 42. |

## Architecture Patterns

### System Architecture Diagram — the compaction data flow

```
                             ┌─────────────────── 5 TRIGGER SURFACES ───────────────────┐
  CLI `aura chat compact`    REPL `/compact`    Telegram `/compact`    Web POST /compact    Auto-fallback
      (chat_compact.go)      (dispatchSlash)     (dispatchRich)      (agui conversations_api)  (loadTurnHistory
            │                      │                    │                    │              on ErrCtxWindowExceeded)
            └──────────────────────┴─────────┬──────────┴────────────────────┴──────────────────┘
                                             ▼
                        ┌────────────── Runner.Compact(ctx, convID, req) ──────────────┐   ← ONE shared seam
                        │ 1. raw := Conv.LoadHistory(convID)         (byte-identical)   │
                        │ 2. floor check (D-04): body≤~3 OR tokens<2×maxOut → NoOp      │
                        │ 3. CompactConversation(ctx, client, model, raw, opts)         │   ← LLM call (drains Stream)
                        │      • WithoutCancel + WithTimeout on the AUTO path only      │
                        │      • empty summary → error, persist nothing                 │
                        │ 4. Conv.RecordCompaction(RecordParams{summary, checkpointSeq  │
                        │      = max(raw.seq), trigger, model, Usage})                  │
                        └───────────────────────────────┬──────────────────────────────┘
                                                         ▼
              ┌──────────── RecordCompaction  (ONE db.WithTx — atomic) ────────────┐
              │  seq := allocateTurnSeq(q, convID)      (row-locked, in-tx)         │
              │  AppendTurnTx(q, {Role:user, Content:summary,                       │
              │      ToolCallID:__aura_compaction_summary__, InputTokens/…=Usage})  │ → folds aggregates (COMPACT-10)
              │  InsertConversationCompaction(q, {checkpoint_seq, summary_turn_seq  │
              │      =seq, trigger, model, tokens_before/after})                    │
              └──────────────────────────────┬─────────────────────────────────────┘
                                             ▼  (originals RETAINED — FTS + audit unaffected)
                       aura.conversation_turns  +  aura.conversation_compactions (watermark)

  NEXT LOAD:  LoadManagedHistory → managedFromTurns
                 │  latest := LatestCompaction(convID)          ← only DB touch in reconstruction
                 │  turns  := truncateAtCheckpoint(turns, latest.checkpoint_seq)   ← PURE
                 └► applyContextLadder → injectAlwaysBlock → [system, always-block, summary, turns>checkpoint]
                       • isCompactionSummary protects the summary in applyL1 + dropOldestPairs
                       • toMessages strips the marker → clean role=user message
                       • messages[0] BYTE-IDENTICAL (KV-cache prefix preserved, D-02)

  READ PATH (web gauge):  GET /compactions → ListCompactions (sibling of ListContextRotEvents) → distinct markers
```

### Recommended file layout (new / touched)
```
internal/conversations/
├── compact.go             # NEW: CompactConversation + compactSystemPrompt (9-section) + render/sanitize
├── store_compact.go       # NEW: RecordCompaction (atomic tx) + LatestCompaction + ListCompactions + Compaction type
├── context.go             # EDIT: managedFromTurns (call LatestCompaction+truncate); dropOldestPairs (protect summary); toMessages (strip marker)
└── context_compaction.go  # NEW (recommended, D-B2 discretion): marker const + isCompactionSummary + truncateAtCheckpoint (pure, focused unit home)
internal/db/migrations/0036_conversation_compactions.{up,down}.sql   # NEW
internal/db/queries/conversation_compactions.sql                     # NEW → sqlc generate
internal/runner/
├── interfaces.go          # EDIT: widen ConversationStore with RecordCompaction (+ LoadHistory already present)
├── runner_compact.go      # NEW (recommended): Runner.Compact shared method + autoCompact inline path
└── runner.go              # EDIT: loadTurnHistory auto-fallback branch + Deps/struct compact fields
internal/config/{config.go,config_knobs.go}   # EDIT: 4 AURA_COMPACT_* knobs
internal/agui/{conversations_api.go,types.go,server.go}   # EDIT: POST /compact + GET /compactions + widen Runner/ConversationStore ifaces
cmd/aura/{chat.go,chat_compact.go(NEW),chat_repl.go,chat_boot.go}   # EDIT/NEW: CLI + REPL dispatch + Deps wiring
internal/channels/telegram/commands.go   # EDIT: /compact + /help
web/src/chat/composer/{skillPickerModel.ts,SkillPicker.tsx}   # EDIT: /compact QuickCommand
web/src/chat/{ContextBudgetGauge.tsx,Composer.tsx,ExternalStoreChat.tsx}   # EDIT
web/src/…/useConversations (hook module)   # EDIT: add useConversationCompactions sibling
```

### Pattern 1: Shared orchestration seam — one `Runner.Compact`, five callers
**What:** A single `Runner.Compact` method that loads raw history, applies the D-04 floor, calls `conversations.CompactConversation`, then `Conv.RecordCompaction`. All manual surfaces call it directly; the auto path calls it internally with `trigger="auto"`.
**When to use:** Always — D-08 explicitly mandates "Server-side compaction logic is shared … no duplicate compaction implementation." The `Runner` is the only object that holds the `llm.Client` + model + conv store together (verified: `runner.go` struct fields `client`, `cfg`, `Conv`). The AG-UI server already holds a `Runner` (verified `NewServer(run Runner, conv ConversationStore, …)`, `server.go:160`), Telegram/CLI/REPL all hold the Runner via `assembleChatEnv`.
**Interface widening required:**
- `runner.ConversationStore` (`interfaces.go:34`) gains `RecordCompaction(ctx, RecordCompactionParams) (Compaction, error)`. `LoadHistory` is **already present** (`:44`).
- `agui.Runner` (`server.go:53`) gains `Compact(ctx, convID, req) (outcome, error)`.
- `agui.ConversationStore` gains `ListCompactions(ctx, id) ([]Compaction, error)`.
- Test doubles `runner.fakeConvStore`, `agui.scriptedRunner`, `agui.fakeConvStore` gain the new methods.

### Pattern 2: Atomic checkpoint persist — in-package tx composition
**What:** `RecordCompaction` opens one `db.WithTx`, calls `allocateTurnSeq(q, convID)` → `AppendTurnTx(q, summaryParams)` → `q.InsertConversationCompaction(...)`, all bound to the same `*sqlc.Queries`.
**Why this works:** `AppendTurnTx` (`store_append.go:116`) is *exported for exactly this* — the resume committer (D-03) already spans a pause claim + answer turn in one tx the same way. `allocateTurnSeq` (`:195`) and `insertTurnAndAggregates` (`:251`) are in-package (lowercase) → `store_compact.go` in `package conversations` calls them directly. A crash between the append and the watermark rolls both back.
```go
// Source: composition mirrors internal/runner PoolResumeCommitter over store_append.go:116
func (s *Store) RecordCompaction(ctx context.Context, p RecordCompactionParams) (Compaction, error) {
    var out Compaction
    err := db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {
        seq, err := s.allocateTurnSeq(ctx, q, p.ConversationID)   // row-locked monotonic
        if err != nil { return err }
        // summary turn carries the compaction Usage → aggregates fold automatically (COMPACT-10)
        if err := s.AppendTurnTx(ctx, q, conversations.AppendTurnParams{
            ConversationID: p.ConversationID, Seq: seq, Role: llm.RoleUser,
            Content: p.Summary, ToolCallID: compactionSummaryMarker,
            InputTokens: p.Usage.InputTokens, OutputTokens: p.Usage.OutputTokens,
            CachedTokens: p.Usage.CachedTokens, CostUSD: p.Usage.CostUSD,
        }); err != nil { return err }
        row, err := q.InsertConversationCompaction(ctx, sqlc.InsertConversationCompactionParams{
            ConversationID: convUUID, CheckpointSeq: int32(p.CheckpointSeq),
            SummaryTurnSeq: int32(seq), Trigger: p.Trigger, Model: p.Model,
            TokensBefore: int32(p.TokensBefore), TokensAfter: int32(p.TokensAfter),
        })
        out = compactionFromRow(row)
        return err
    })
    return out, err
}
```
**Note — no spill risk:** `AppendTurnTx` rejects content that would spill past `turnCapBytes` (65536) with `ErrContentSpillUnsupported` (`store_append.go:123`). A `AURA_COMPACT_MAX_OUTPUT_TOKENS`=4096 summary is ≈16 KB — safe. Flag only if an operator sets the budget above ~16k tokens (≈64 KB); the planner should clamp or document.

### Pattern 3: Auto-fallback is **inline-bounded**, not fire-and-forget
**What:** Unlike `maybeAutoTitle` (a background `r.wg` goroutine), auto-compaction runs **inline** inside `loadTurnHistory` — the turn *waits* for the compacted history to proceed. It still uses `context.WithoutCancel` + `context.WithTimeout(AURA_COMPACT_TIMEOUT_SEC)` so a client disconnect can't abort a half-written checkpoint, but it spawns **no new goroutine** → no new goleak surface.
```go
// Source: seam at internal/runner/runner.go:485
func (r *Runner) loadTurnHistory(ctx context.Context, convID string, cfg conversations.ContextConfig, branchLeaf int) ([]llm.Message, error) {
    load := func() ([]llm.Message, error) {
        if branchLeaf > 0 { return r.Conv.LoadManagedHistoryForBranch(ctx, convID, branchLeaf, cfg) }
        return r.Conv.LoadManagedHistory(ctx, convID, cfg)
    }
    history, err := load()
    if err == nil || !errors.Is(err, conversations.ErrContextWindowExceeded) || !r.compactAutoEnabled {
        return history, err   // AURA_COMPACT_AUTO_ENABLED=false → old dead-end preserved
    }
    cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.compactTimeout)
    defer cancel()
    if _, cErr := r.Compact(cctx, convID, CompactRequest{Trigger: "auto"}); cErr != nil {
        return nil, err   // surface the ORIGINAL window error, never a worse state
    }
    if history, rErr := load(); rErr == nil {   // exactly ONE re-load — no retry loop
        return history, nil
    }
    return nil, err   // still over cap (e.g. one oversized latest turn) → original error, once
}
```

### Pattern 4: Reconstruction is a pure transform gated by one DB lookup
**What:** `managedFromTurns` (`context.go:217`) is a `*Store` method (has ctx+convID) → it can call `s.LatestCompaction`. Everything after is pure: `truncateAtCheckpoint(turns, checkpointSeq)` keeps `seq==1` (system) + all `seq > checkpoint_seq` (which *naturally includes the summary turn*, since `summary_turn_seq > checkpoint_seq`), dropping the pre-checkpoint body.
**Key elegance:** the summary turn needs no special selection — it survives the `seq > checkpoint_seq` filter for free, and `injectAlwaysBlock` then puts it at `messages[2]` exactly per D-02. Multiple compactions are handled by "latest checkpoint wins" (`ORDER BY created_at DESC LIMIT 1`) — an older summary at `seq ≤ new checkpoint` is correctly dropped.
```go
// PURE — belongs in context_compaction.go, unit-tested daemon-free
func truncateAtCheckpoint(turns []Turn, checkpointSeq int) []Turn {
    out := make([]Turn, 0, len(turns))
    for _, t := range turns {
        if t.Seq == 1 || t.Seq > checkpointSeq { // keep system L0 + everything after the checkpoint (incl. summary)
            out = append(out, t)
        }
    }
    return out
}
func isCompactionSummary(t Turn) bool { // mirrors isAlwaysBlock (context.go:303)
    return t.ToolCallID == compactionSummaryMarker && t.Role == llm.RoleUser
}
```
**`dropOldestPairs` edit:** extend the protected-head split (`context.go:387-392`) — after the always-block check, add `if len(turns) > start && isCompactionSummary(turns[start]) { start++ }` so L2.5 never drops the summary. **`toMessages` edit:** add an `isCompactionSummary(t)` branch (mirror the `isAlwaysBlock` branch at `context.go:452`) emitting a clean `role=user` message with the marker stripped.

### Anti-Patterns to Avoid
- **Recomputing `checkpoint_seq` at persist time instead of capturing it from the summarized snapshot.** Pass `checkpoint_seq = max(seq)` of the *history that was actually summarized* into `RecordCompaction`. If a turn races in between `LoadHistory` and the persist tx, it lands at `seq > checkpoint_seq` and is correctly *preserved* in context (not silently lost). Recomputing max(seq) inside the tx would fold an unsummarized turn under the checkpoint → data dropped from context. (Auto path holds the per-conversation lock so this can't happen; manual paths can — see Open Question 1.)
- **Duplicating the compaction call across the 5 surfaces.** Violates D-08. Route everything through `Runner.Compact`.
- **Templating `compactSystemPrompt` per call.** It is a package `const`; the only per-call variation is the appended `opts.Focus` (SPEC Req#2 acceptance asserts this).
- **Letting the summary turn reach the wire with its marker.** `toMessages` must strip it (a `role=user` message must not carry a `tool_call_id`).
- **Reusing the i18n key `footer.compacted` for the LLM-compaction marker.** That key already labels L2.5 hard-drops ("Compacted N older turns"). D-09/D-10 require a *distinct* label + glyph — see Pitfall 8.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Atomic summary-turn + watermark write | A bespoke two-statement sequence | `db.WithTx` + `allocateTurnSeq` + `AppendTurnTx` + `InsertConversationCompaction` | Row-locked monotonic seq + rollback already proven (resume committer). Hand-rolling races the seq. |
| Token counting | A char/4 heuristic | `countTokens(enc, …)` (`tiktoken.go:90`) | Same gating-grade encoder as L2/L2.5; consistent budgets. |
| LLM-over-history call | New stream plumbing | `CompactConversation` mirroring `generateTitle` (`title.go:39`) | Drains `client.Stream` correctly (interface contract: consumers MUST drain or the impl leaks). |
| Bounded best-effort lifecycle | New goroutine + timer | `context.WithoutCancel` + `context.WithTimeout` (Pattern 3) | The `maybeAutoTitle` precedent is leak-audited. Auto-compaction is inline → even simpler. |
| Summary protection in the ladder | Special-casing seq numbers | `isCompactionSummary` marker predicate (mirror `isAlwaysBlock`) | Marker-in-`ToolCallID` is collision-proof; a real user turn never sets it. |
| Compaction read route | New handler shape | Copy `handleConversationRotEvents` (`conversations_api.go:219`) verbatim (owner-gate + `writeJSON`) | Identical shape; only the store method + wire type change. |
| Config validation | Bespoke parse checks | Add 4 `KnobSpec` rows to `knobRegistry()` | "The registry IS the engine" — `aura config validate` + `.env.example` pick them up automatically. |

**Key insight:** every "hard" part of this phase already has a shipped, test-covered precedent in the same packages. The phase is pattern-application, not invention.

## Common Pitfalls / Landmines the planner must design around

### Pitfall 1: KV-cache byte-stability (`messages[0]`) — the Phase-6 invariant
**What goes wrong:** inserting the summary anywhere in the cached system prefix invalidates the whole KV-cache every turn (silent cost/latency regression).
**How to avoid:** summary is a *persisted turn* landing at `messages[2]` after `injectAlwaysBlock` (D-02). `messages[0]` = `agent.SystemPrompt` constant, untouched. **Verify:** extend the existing cache-stable-prefix assertion (see `context_alwaysblock_test.go`) to a *compacted* conversation — assert `messages[0]` byte-identical before/after compaction.
**Warning sign:** any change that makes the summary a system-role turn, or injects it before the always-block.

### Pitfall 2: Atomic persist / partial watermark
**What goes wrong:** a crash after the summary turn INSERT but before the watermark row → a dangling summary with no checkpoint (or vice-versa).
**How to avoid:** both in one `db.WithTx` (Pattern 2). **Verify (daemon-free):** the `store_fakedbtx_test.go` / `store_search_fakedbtx_test.go` fake-`DBTX` pattern injects a mid-tx error → assert neither row exists (covers the rollback branch without a live DB). **Verify (db_integration):** real injected failure rolls both back.

### Pitfall 3: `WithoutCancel`/`WithTimeout` lifecycle on the auto path
**What goes wrong:** a client disconnect mid-turn cancels the ctx and aborts the half-written checkpoint; or an unbounded compaction call hangs the turn.
**How to avoid:** Pattern 3 — `WithoutCancel(ctx)` then `WithTimeout(r.compactTimeout)`. Because it's inline (no goroutine), there is **no** new `r.wg` entry and **no** new goleak surface. **Verify:** a stub client that blocks past the timeout → the load surfaces the original `ErrContextWindowExceeded`; `goleak` clean (no dangling goroutine).

### Pitfall 4: marker-in-`ToolCallID` trick must be consistent in 3 places
`RecordCompaction` writes the marker; `isCompactionSummary` reads it (protection in `applyL1` + `dropOldestPairs`); `toMessages` strips it. Miss any one and either the summary leaks a `tool_call_id` to the provider, or L2.5 drops it on a long conversation. Keep the const + predicate colocated in `context_compaction.go`.

### Pitfall 5: migration `0036` slot + sqlc regen workflow
**Verified:** head is `0035_assets_source_kind_agent`; `0036` is free. sqlc reads the **schema from `internal/db/migrations`** (`sqlc.yaml:9`), queries from `internal/db/queries`, generates into `internal/db/sqlc`. Workflow: (1) write `0036_conversation_compactions.{up,down}.sql`, (2) write `internal/db/queries/conversation_compactions.sql` (`:exec`/`:one`/`:many` mirroring `context_rot_events.sql`), (3) `sqlc generate` → `Querier` gains `InsertConversationCompaction`/`GetLatestCompaction`/`ListCompactions`. `.down.sql` must reverse cleanly; re-run a no-op; applies as `aura_migrate`, denied as `aura_app` (CLAUDE.md / migration convention). `emit_interface: true` means the `fakeDBTX` unit tests can mock the new methods.

### Pitfall 6: `GetLatestCompaction` determinism
Two compactions could share a `created_at` (timestamptz precision). Order `ORDER BY created_at DESC, checkpoint_seq DESC LIMIT 1` (or `summary_turn_seq DESC`) so "latest wins" is deterministic — the reconstruction correctness depends on it.

### Pitfall 7: `context.go` LOC ceiling (≤600) + refactor-on-touch
**Verified:** `context.go` is **464 LOC**. The additions (const + `isCompactionSummary` + `truncateAtCheckpoint` + edits to `managedFromTurns`/`dropOldestPairs`/`toMessages`) are ~40–60 LOC → ~520 total, *under* 600 but close. **Recommendation (D-B2 discretion):** put the *new pure* symbols (marker const, `isCompactionSummary`, `truncateAtCheckpoint`) in a new `context_compaction.go`; keep only the small edits to the three existing funcs in `context.go`. This keeps `context.go` lean, isolates the new logic in a focused unit-test home, and satisfies refactor-on-touch (dead-code/dupl/comments in the same commit).

### Pitfall 8: frontend marker naming collision + **missing gauge test**
- The rot-events marker already renders `t('footer.compacted', {count})` → "Compacted N older turns" for **L2.5 hard-drops**. The new LLM-compaction marker (D-09) is semantically different (a summary checkpoint) and D-10 mandates a **visually distinct** glyph/color + label. Pick a new i18n key (e.g. `footer.summarized`) — do not overload `footer.compacted`.
- **Gotcha:** D-10 says "there is an existing `ContextBudgetGauge`/SkillPicker test suite to extend." **Verified only partially true:** `SkillPicker` has tests (`skillPickerModel.test.ts`, `__tests__/SkillPicker.test.tsx`), but **`ContextBudgetGauge` has NO test file** (no `ContextBudgetGauge.test.tsx`, no `footerMetrics.test.ts`). The planner must **create** these, not extend them — a real Wave-0 frontend gap.

### Pitfall 9: no-skip-as-green for the DB-gated store tests
`store_compact.go`'s SQL is exercised only under `//go:build db_integration`. The coverage gate runs `db_integration neo4j_integration` with a live DB, so these DO contribute coverage **iff they actually run**. Ensure the new integration tests use the `envOrSkip`→`t.Fatal`-under-`$CI` discipline (a sub-second "integration" runtime is a skip tell). See Validation Architecture for the pure-logic backstop that keeps the floor safe even if a live tier is misconfigured.

## Runtime State Inventory

> Phase 42 is **additive greenfield persistence** (a new table + new turns), not a rename/refactor/migration of existing runtime state. The five categories are answered for completeness:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | New `aura.conversation_compactions` rows + new marked summary turns in existing `aura.conversation_turns`. No existing data is rewritten (originals retained). | Forward-only migration `0036`; no data backfill. |
| Live service config | None — no external service holds compaction state. | None — verified (compaction is entirely Postgres-local). |
| OS-registered state | None. | None — verified (no scheduler/task/daemon registration touched). |
| Secrets/env vars | 4 new `AURA_COMPACT_*` knobs (config-read only; no secrets). | Register in `knobRegistry()` + `.env.example` (auto-generated). |
| Build artifacts | `sqlc generate` regenerates `internal/db/sqlc/*.go` (querier + models). | Run `sqlc generate` after the query file lands; commit the generated diff. |

**Canonical question — "after every file is updated, what runtime systems still have old state?":** none. There is no pre-existing compaction state to migrate; the phase only *adds*.

## Validation Architecture

**Nyquist validation is ENABLED** (`.planning/config.json` `workflow.nyquist_validation: true`). This section is mandatory and is the primary deliverable.

### Test Framework
| Property | Value |
|----------|-------|
| Go framework | stdlib `testing` + `go.uber.org/goleak` (db_integration TestMain) + `gotestsum` |
| Go config | build tags: **untagged** = daemon-free unit; `//go:build db_integration` = live-DB tier (verified `internal/conversations/main_test.go`) |
| Go quick run | `go test ./internal/conversations/ ./internal/runner/ ./internal/config/ ./internal/agui/` |
| Go full (gate) | `bash scripts/coverage_docker.sh` (containerized stack, disposable `aura_cov` DB) — the `db_integration neo4j_integration` matrix, ≥85% owned-surface floor |
| Go race | `go test -race ./internal/conversations/ ./internal/runner/ …` |
| Go mutation | `go-mutesting` (WSL) — ≥70% killed on `compact.go` + the reconstruction change (SPEC Constraints) |
| Web framework | `vitest` + `@vitest/coverage-v8`; mutation via `@stryker-mutator/vitest-runner` (verified `web/package.json`) |
| Web run | `cd web && npm test` (`vitest run --coverage`) — **separate gate**, not `scripts/coverage_gate.sh` (D-10) |

### The daemon-free vs db_integration split (the core analysis)

The ≥85% floor covers `internal/*` owned surface. **`cmd/aura` glue is excluded** (behaviorally covered per CLAUDE.md) and **`web/src` has its own vitest gate**. So the floor-critical Go surface is: `internal/conversations` (`compact.go`, `store_compact.go`, `context.go`/`context_compaction.go`), `internal/runner` (`Runner.Compact`, auto-fallback), `internal/config` (knobs), `internal/agui` (routes).

**Critical rule (from the objective):** DB-gated code that only *compiles+skips* contributes **zero** coverage. The mitigation is two-pronged: (a) push logic into **pure functions with untagged unit tests** (these run in *every* pass and always contribute), and (b) enforce no-skip-as-green on the `db_integration` tests so they actually execute in the gate. The table below marks, per requirement, which tier owns coverage and — crucially — **which requirements CANNOT be covered by `db_integration` alone**.

### Phase Requirements → Test Map

| Req | Behavior | Test Tier | Technique | Fixtures / Stubs | Floor-critical daemon-free? |
|-----|----------|-----------|-----------|------------------|------------------------------|
| **COMPACT-01** | `CompactConversation` shapes request, drains stream, counts tokens, errors on empty | **unit** | table-driven; assert `Summary` populated, `TokensBefore=tiktoken(input)`, `TokensAfter=tiktoken(summary)`, empty-stream→error+no-persist, `opts.Focus` rides `Request.Messages[0].Content` | stub `llm.Client` (reuse the `title_unit_test.go` stub returning a canned `Stream` channel) | **YES** — no DB; must be unit |
| **COMPACT-02** | 9-section prompt constant | **unit** | substring/golden assertions: all **9** headers + "Reply in English only" + "TEXT ONLY, call no tools" guard (D-06) | none (pure const) | **YES** |
| **COMPACT-03** | migration + sqlc + atomic persist | **db_integration** (SQL truth) **+ unit** (rollback branch) | db_int: N turns→`RecordCompaction`→exactly 1 watermark + 1 marked summary; N originals still present; FTS `SearchConversationTurns` still matches; atomic rollback on injected failure. unit: `fakeDBTX` injects mid-tx error → assert neither row | live DB (`aura_cov`) + `fakeDBTX` (from `store_fakedbtx_test.go`) | partial — the **rollback branch** is daemon-free via `fakeDBTX`; SQL correctness needs db_int |
| **COMPACT-04** | reconstruction honors checkpoint | **unit (primary)** **+ db_integration** | unit: `truncateAtCheckpoint` table-driven (`[system, always-block, summary, turns>K]`, excludes `≤K`); summary survives `dropOldestPairs`; **byte-equality** of two loads; `isCompactionSummary` predicate; `messages[0]` byte-identical (KV-cache). db_int: real post-compaction `LoadManagedHistory` | in-memory `[]Turn` + fake checkpoint seq; encoder from `encoder()` | **YES** — this is the highest-value daemon-free surface; db_int alone would leave the pure branches uncovered if a live tier is skipped |
| **COMPACT-05** | CLI `aura chat compact` | **unit** (dispatch+floor) + db_int (full path) | arg parse (`--focus`), D-04 no-op branch ("nothing to compact", exit 0, no row) | scripted runner / in-mem store | cmd/aura excluded from floor — behavioral only |
| **COMPACT-06** | REPL slash router | **unit** | table-driven `dispatchSlash`: `/compact`→handled+delta (no `runner.Turn`), `/bogus`→hint (never LLM), normal msg unchanged | drive `chatLoop` with scripted in/out (existing REPL test pattern) | cmd/aura excluded — behavioral |
| **COMPACT-07** | Telegram `/compact` | **unit** | `dispatch(ctx,chatID,"/compact")`→handled, not forwarded; `/help` lists it | scripted runner + telegram command test harness | telegram is owned surface → **YES**, keep unit |
| **COMPACT-08** | auto-fallback (once, no loop, toggle) | **unit** | inject `ErrContextWindowExceeded` via `fakeConvStore.manErr` (fires load #1, clears #2); assert exactly **one** `RecordCompaction`, re-load, turn proceeds; 2nd forced over-cap surfaces error once; `AUTO_ENABLED=false`→old dead-end | extend `fakeConvStore` (manErr-once + RecordCompaction spy); `-race` | **YES** — pure runner control flow, no DB |
| **COMPACT-09** | config knobs | **unit** | extend `config_knobs_test.go` registry iteration (golden count 4↑); bool/int reject bad values; defaults on unset; `AURA_COMPACT_MODEL` set → used | `t.Setenv` | **YES** |
| **COMPACT-10** | cost attribution + `ListCompactions` | **db_integration** (aggregate math) **+ unit** (projection mapping) | db_int: aggregates rise by the compaction `Usage`; `ListCompactions` returns rows `created_at DESC`. unit: pgtype→domain mapping of a row | live DB + row fixtures | partial — projection mapping daemon-free; aggregate integration needs db_int |
| **COMPACT-11** | bounded/atomic/never-corrupting | **unit** (timeout+rollback) **+ db_integration** | unit: stub client blocks past timeout → original error, `goleak` clean; `fakeDBTX` rollback → no partial state; manual path returns error. db_int: real mid-persist failure | stub client + `fakeDBTX` + `goleak` | **YES** for the timeout/rollback branches |
| **D-08** (web POST) | `/compact` trigger route | **agui unit** (httptest) | `scriptedRunner.Compact` spy called; returns `{tokens_before,tokens_after,compaction_id}`; owner-gate 404 for foreign id; below-floor→"nothing to compact" (200, non-error); body capped (`maxRunBodyBytes` DoS guard) | `NewServer(&scriptedRunner{}, &fakeConvStore{})` | **YES** — agui is owned surface, all-fake tests contribute |
| **D-09** (web GET) | `/compactions` read route | **agui unit** (httptest) | mirror `handleConversationRotEvents` test: owner-gate 404, `fakeConvStore.ListCompactions` projection, `writeJSON` shape | fakes | **YES** |
| **D-08/D-09** (React) | QuickCommand + gauge marker | **vitest** (own gate) | `skillPickerModel.test.ts`: `/compact` in the union; `SkillPicker.test.tsx`: icon; **new** `ContextBudgetGauge.test.tsx`: distinct marker vs rot-events; **new** hook test (mock GET); `Composer` dispatch (mock POST) + toast | testing-library + mocked fetch | web gate, not Go floor |

### Sampling Rate
- **Per task commit:** `go test ./internal/<touched>/` + `go vet` + `go build` (Gate 2 post-edit discipline); for web tasks `cd web && npm test`.
- **Per wave merge:** `go test -race ./internal/conversations/ ./internal/runner/ ./internal/config/ ./internal/agui/` + the untagged unit suite.
- **Phase gate:** `bash scripts/coverage_docker.sh` full `db_integration neo4j_integration` matrix ≥85% green **locally before push** (CLAUDE.md — never point `db_integration` at the live `aura` DB); `go-mutesting` ≥70% on `compact.go` + reconstruction; `cd web && npm test` green; `golangci-lint` 0; every touched file ≤600 LOC.

### Wave 0 Gaps (test infrastructure to create before implementation)
- [ ] `internal/conversations/compact_test.go` — untagged unit; covers COMPACT-01/02 (stub `llm.Client`, 9-header assertions). Reuse the `title_unit_test.go` stub-client shape.
- [ ] `internal/conversations/context_compaction_test.go` — untagged unit; `truncateAtCheckpoint`, `isCompactionSummary`, `toMessages` strip, `dropOldestPairs` summary-protection, byte-equality, `messages[0]` KV-cache stability (COMPACT-04).
- [ ] `internal/conversations/store_compact_test.go` (`//go:build db_integration`) — COMPACT-03/10: watermark+summary atomicity, originals-retained, FTS-still-matches, aggregate attribution, `ListCompactions` order.
- [ ] `internal/conversations/store_compact_fakedbtx_test.go` — untagged; the rollback branch via `fakeDBTX` (keeps COMPACT-03/11 rollback covered without a live DB).
- [ ] `internal/runner/fakes_test.go` — **extend** `fakeConvStore` with `RecordCompaction` (+ spy) and a manErr-once mode for COMPACT-08.
- [ ] `internal/runner/runner_compact_test.go` — untagged; COMPACT-08/11 auto-fallback control flow (`-race`, `goleak`).
- [ ] `internal/config/config_knobs_test.go` — **extend** golden registry count + the 4 knob parse cases (COMPACT-09).
- [ ] `internal/agui/*_test.go` — POST + GET route tests via `scriptedRunner`/`fakeConvStore` (D-08/D-09 backend).
- [ ] `web/src/chat/composer/skillPickerModel.test.ts` — **extend** for the `/compact` QuickCommand.
- [ ] `web/src/chat/__tests__/ContextBudgetGauge.test.tsx` — **CREATE** (does not exist); distinct-marker assertion + hook.
- [ ] (recommended) `web/src/chat/footerMetrics.test.ts` — **CREATE**; the compactions aggregation sibling of `totalPairsDropped`.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all Go work | ✓ (assumed — active repo) | go1.25+/1.26 | — |
| Postgres (`aura.*`) + `db_integration` env | COMPACT-03/10 db_int tests, coverage gate | ✓ via WSL/Docker stack (`scripts/coverage_docker.sh`) | 11 migrations shipped, head `0035` | pure-logic unit tests cover the rest of the floor |
| `sqlc` | regenerate query layer after `0036` | ✓ (in toolchain) | v2 config | — |
| `go-mutesting` (WSL) | mutation ≥70% on `compact.go` | ✓ (WSL `~/go/bin`) | go1.26 fork | — |
| Node + `vitest` | D-08/D-09 React tests | ✓ (`web/`) | vitest ^4.1.9 | — |
| Neo4j / `mcp-neo4j-cypher` | coverage gate `neo4j_integration` tag (unrelated to this phase but part of the matrix) | ✓ containerized | 0.6.0 | — |
| LLM provider (live) | end-to-end manual/auto verification (DoD >9.8 E2E) | ⚠ requires `OPENROUTER_API_KEY` | — | stub `llm.Client` covers all automated tiers; live only for final E2E |

**Missing dependencies with no fallback:** none — every automated tier runs offline (stub client + local DB). Only the final live E2E (CLAUDE.md DoD ">9.8 on real scenario") needs a real provider key.

## Security Domain

> `security_enforcement` absent in config → treated as **enabled**. This phase adds one new **mutating** HTTP route (POST `/compact`) and one read route, plus an LLM prompt that must preserve safety constraints.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control (verified pattern) |
|---------------|---------|-------------------------------------|
| V4 Access Control | **yes** | Both new routes MUST owner-gate via `GetForIdentity(ctx, id, scopedIdentityID(ctx))` before any read/mutate — the exact guard `handleConversationRotEvents` (`conversations_api.go:224`) and `handleRenameConversation` (`:261`) use. A foreign/absent id → 404 before the store touch. |
| V5 Input Validation | **yes** | POST body capped with `http.MaxBytesReader(w, r.Body, maxRunBodyBytes)` (mirror `handleRenameConversation:249`); `parseConvID` rejects malformed ids (404, never a 500 parse leak). `--focus`/`opts.Focus` is bounded (rides the summary budget) and is data, not code — never interpolated into SQL. |
| V7 Error Handling | **yes** | Store/runner errors through the route MUST go through `sanitizeErr`/`writeStoreErr` (a runner error can embed a DSN — see `approvals_api_unit_test.go` DSN-leak test). Assert no DSN on the wire. |
| V6 Cryptography | no | No crypto introduced. |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Cross-identity compaction trigger/read (IDOR) | Elevation of Privilege / Info Disclosure | Owner-gate both routes (V4); 404 before store touch. Unit-test the foreign-id 404 (fake store). |
| **Governance Decay** — compaction silently erasing user-stated safety constraints (arxiv, D-06) | Tampering (integrity of safety context) | D-06's prompt mandates **verbatim preservation of user-stated security/safety constraints** + the "TEXT ONLY, call no tools" guard. Unit-assert both are present in `compactSystemPrompt` (COMPACT-02). This is the phase's headline security control. |
| Summary turn leaking a `tool_call_id` to the provider (wire malformation) | Tampering | `toMessages` strips the marker (Pitfall 4); assert the emitted message has empty `ToolCallID`. |
| DoS via oversized `/compact` body or focus text | Denial of Service | `MaxBytesReader` cap on the POST body; summary bounded by `AURA_COMPACT_MAX_OUTPUT_TOKENS`; one bounded auto-attempt (no retry loop, COMPACT-08). |
| Error message leaking DSN/internal detail | Information Disclosure | `sanitizeErr` on every route error path (V7). |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `envutil` string-knob default is read via `os.Getenv`/`envDefault` (verified `BoolDefault`+`IntDefault`; no `StringDefault` seen) | Std Stack / COMPACT-09 | Low — `AURA_COMPACT_MODEL` uses `os.Getenv` (empty default is intended, D-05). Planner confirms the string-read helper. |
| A2 | The `agui` `useConversations` hook module (import `../conversations/useConversations` from `web/src/chat/`) exposes `useConversationRotEvents` as the sibling template for `useConversationCompactions` | Frontend seams | Low — the import is verified in `ContextBudgetGauge.tsx`; the exact file path wasn't opened (glob miss on the assumed dir). Planner locates the module by the import specifier. |
| A3 | Persisting the summary turn with the compaction `Usage` in its `AppendTurnParams` is the intended cost-attribution mechanism (vs a separate aggregate call) | Pattern 2 / COMPACT-10 | Low — `appendTurnWrites` folds `agg` from the same params (verified `store_append.go:213`); this is the natural path and avoids a double-count. Planner confirms no *other* code also folds the compaction usage. |
| A4 | Go toolchain version (1.25/1.26) and the live stack are available in the executor env | Env Availability | Low — active repo with shipped coverage campaigns; assumed present. |

**Note:** A1–A4 are low-risk *confirmations*, not open design questions. No `[ASSUMED]` claim in this research affects a locked decision, a compliance/retention/security *requirement*, or a package choice (there are no new packages).

## Open Questions (RESOLVED)

> All three questions are resolved by the Phase-42 plan set (42-01 / 42-03 / 42-04); each inline **RESOLVED:** note below points at the deliverable that closes it.

1. **Manual-path concurrency for `checkpoint_seq`.**
   - **RESOLVED (42-04 Task 1 + 42-01 prohibition):** `Runner.Compact` captures `checkpoint_seq` from the `LatestTurnSeq` snapshot at load and passes it to `RecordCompaction` — never recomputed in-tx (the 42-01 prohibition). A turn racing in between load and persist lands at `seq > checkpoint_seq` and is *preserved*; the auto path additionally holds the per-conversation lock, so the manual-path edge is benign either way.
   - What we know: the auto path runs under the per-conversation lock (`turnLocked`), so `checkpoint_seq` = summarized-snapshot max(seq) is race-free. Manual `/compact` (CLI/REPL/Telegram/web) does not obviously hold that lock.
   - What's unclear: whether a concurrent live turn could append between `LoadHistory` and `RecordCompaction` on the manual path.
   - Recommendation: pass `checkpoint_seq = max(seq of summarized history)` into `RecordCompaction` (do **not** recompute in-tx). A concurrent turn then lands at `seq > checkpoint_seq` and is *preserved* in context (benign). Optionally acquire the per-conversation runner lock inside `Runner.Compact` for full serialization — cheap, and eliminates the edge entirely. Planner's call.

2. **`AURA_COMPACT_MAX_OUTPUT_TOKENS` vs the 64 KB spill cap.**
   - **RESOLVED (42-03 Task 1):** `AURA_COMPACT_MAX_OUTPUT_TOKENS` is clamped at parse time (pure `clampCompactMaxOutput` → `compactMaxOutputCeiling` ≈8192 tokens ≈32KB) provably under the 65536-byte `turnCapBytes` spill cap, so a summary turn can never exceed the tx-append cap. Defense-in-depth: a bypassed over-cap summary still fails cleanly as `ErrContentSpillUnsupported` inside the same `db.WithTx` (COMPACT-11), never silent corruption.
   - What we know: `AppendTurnTx` rejects >`turnCapBytes` (65536) content with `ErrContentSpillUnsupported`. 4096 tokens ≈ 16 KB (safe); ~16k tokens ≈ 64 KB (borderline).
   - Recommendation: document the ceiling, or clamp the effective summary budget so a summary can never exceed the tx-append cap. Default (4096) is safe; only an extreme operator override risks it.

3. **Does `AURA_COMPACT_TIMEOUT_SEC` also bound the *manual* path?**
   - **RESOLVED (42-04 Task 1):** `Runner.Compact` applies `context.WithTimeout(ctx, r.compactTimeout)` on *every* trigger (manual + auto); `context.WithoutCancel` wraps *only* the auto path (the relocated `loadTurnHistory`), where a client disconnect must not corrupt the checkpoint — so a hung manual compaction is bounded too.

   SPEC Req#11 applies `WithTimeout` to the *auto* path explicitly; the manual path surfaces errors to the user. Recommendation: apply the same `WithTimeout` bound to `Runner.Compact` regardless of trigger (a hung manual compaction is still bad UX), with `WithoutCancel` reserved for the auto path (where a client disconnect must not corrupt the checkpoint). Planner to confirm the ctx-shaping per trigger.

## Sources

### Primary (HIGH confidence — direct source read at HEAD, 2026-07-12)
- `internal/conversations/context.go` — `alwaysBlockMarker:48`, `isAlwaysBlock:303`, `ErrContextWindowExceeded:62`, `managedFromTurns:217`, `applyContextLadder:228`, `injectAlwaysBlock:283`, `dropOldestPairs:381`, `toMessages:446` — **all CONTEXT pointers hold**.
- `internal/conversations/title.go` — `generateTitle:39` (2-msg request, drains `Stream:52`, explicit model) — the `CompactConversation` template.
- `internal/conversations/store_append.go` — `AppendTurn:56`, `AppendTurnTx:116` (exported tx-composable), `allocateTurnSeq:195`, `appendTurnWrites:213`, `insertTurnAndAggregates:251`.
- `internal/conversations/store.go` — `Store` struct, `LoadHistory:260` (raw byte-identical), `Turn`/`AppendTurnParams`.
- `internal/conversations/tiktoken.go` — `countTokens:90`, `encoder()`.
- `internal/runner/interfaces.go:34` (`ConversationStore`, `LoadHistory:44` present), `runner.go:485` (`loadTurnHistory`), `runner.go:66` (`Deps`), `runner_resume.go:19` (`maybeAutoTitle` WithoutCancel/WithTimeout/wg), `fakes_test.go:54` (`manErr`).
- `cmd/aura/chat.go:43` (switch), `chat_repl.go:51` (`chatLoop`, `/exit:68`, dispatch:72), `chat_boot.go:205/323/337` (`assembleChatEnv`, `Deps`, `EvictAfter`).
- `internal/channels/telegram/commands.go:134` (`dispatchRich`), `internal/config/config.go:62/394` + `config_knobs.go:58/84` (`knobRegistry`, `KnobKind`), `internal/envutil/envutil.go:22/37` (`IntDefault`/`BoolDefault`).
- `internal/agui/server.go:53/160` (`Runner` iface, `NewServer`), `conversations_api.go:219` (`handleConversationRotEvents` template), `sqlc.yaml`, `internal/db/queries/context_rot_events.sql`, `internal/db/migrations/*` (head `0035`).
- `web/src/chat/ContextBudgetGauge.tsx`, `web/src/chat/composer/{skillPickerModel.ts,SkillPicker.tsx,skillPickerModel.test.ts}`, `web/package.json` (vitest + stryker).
- `internal/conversations/main_test.go` (goleak + `//go:build db_integration`), `store_fakedbtx_test.go`/`store_search_fakedbtx_test.go` (fake-DBTX rollback pattern).

### Secondary (project docs — HIGH)
- `42-SPEC.md` (11 locked requirements + amendment 2026-07-12), `42-CONTEXT.md` (D-01…D-10), `CLAUDE.md` (coverage floor, no-skip-as-green, LOC ≤600, tag set), `.planning/config.json` (`nyquist_validation:true`).

### External (domain — already synthesized during discuss-phase, NOT re-derived here)
- Claude Code 9-section `compact.md`, GPT-5.5/Codex summarize-then-continue, arxiv "Governance Decay", open-webui context-compaction-threshold — folded into D-04/D-05/D-06 (see `42-CONTEXT.md` §canonical_refs).

## Metadata

**Confidence breakdown:**
- Code seams: **HIGH** — every `file:line` from CONTEXT re-verified by direct read; all hold; migration head confirmed.
- Standard stack: **HIGH** — zero new dependencies; all reused assets read in source.
- Validation architecture: **HIGH** — test tiers, build tags, goleak setup, fake patterns, and coverage-gate mechanics verified against actual test files.
- Pitfalls: **HIGH** — each landmine traced to a specific verified line + an existing precedent.
- Frontend: **MEDIUM-HIGH** — composer/gauge files verified; the exact `useConversations` hook module path inferred from an import specifier (A2); the missing gauge test is a confirmed gap.

**Research date:** 2026-07-12
**Valid until:** ~2026-08-11 (stable — no fast-moving external deps; invalidated only by refactors to `context.go`/`title.go`/`store_append.go`/`runner.go` or a new migration beyond `0036`).
