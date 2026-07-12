# Phase 42: LLM Conversation Compaction (`/compact` parity — Phase-4 deferred L3) — Specification

**Created:** 2026-07-08
**Ambiguity score:** 0.14 (gate: ≤ 0.20)
**Requirements:** 11 locked
**Phase number:** PROVISIONAL — slot with `/gsd-phase` (extends v2.0.0 tail or opens v2.1.0; not yet in ROADMAP.md).
**Status:** SPEC draft for `/gsd-discuss-phase` → `/gsd-plan-phase`. No code written (PRD-first gate).

## Goal

Give Aura's agent runtime **LLM-driven semantic conversation compaction** — the "L3" tier that Phase 4 explicitly deferred (`04-SPEC.md` Req#10 "L3 LLM-driven compaction is explicitly NOT implemented"; Out-of-scope "L3 full LLM-driven compaction (`chat_compact`) — deferred … PRD §1.8 OQ#3"). This closes the last context-management parity gap with Claude Code's `/compact`: instead of only the deterministic **lossy** L2.5 hard-drop (delete oldest user/assistant pairs) or the `ErrContextWindowExceeded` dead-end ("start a new chat"), a long conversation can be **summarized into a structured handoff and continued in place**, preserving intent, decisions, files, and pending work.

Two trigger surfaces, both landing this phase (operator-chosen 2026-07-08):
- **Manual `/compact`** — user-invoked on CLI (`aura chat compact <id>`), in the interactive REPL (`/compact`), and Telegram (`/compact`). Literal Claude Code parity.
- **Auto-fallback** — compaction fires automatically at the `ErrContextWindowExceeded` dead-end (the point where even L2.5 cannot reduce the history under the hard cap), replacing the "start a new chat" failure with a self-healing summarize-then-continue. The existing L1 (microcompact tool-eviction) and L2.5 (hard-drop pairs) ladder tiers are **unchanged** — compaction only replaces the terminal failure path (pure upside over today).

Persistence is **checkpoint-watermark, non-destructive**: the summary is persisted as one protected turn plus a `aura.conversation_compactions` audit/watermark row; the original pre-compaction turns are **retained** in `conversation_turns` (FTS search + audit still see them). History reconstruction after a checkpoint yields `[system L0, always-block, summary turn, turns after the checkpoint]`.

## Background

Aura's context management (`internal/conversations/context.go`) is a deterministic, **LLM-free** ladder — the file states outright *"No LLM call is made"* (`applyContextLadder`, context.go:227):

- **L0** system turn (`seq=1`) — protected, byte-stable (KV-cache invariant, Phase 6).
- **messages[1] always-block** (Agent.md + always-on skills) — rebuilt per turn by `runner.renderContextBlock`, injected by `injectAlwaysBlock`, protected like L0.
- **L1 microcompact** (`applyL1`) — rewrites old sidecar-backed `role='tool'` turns to a `read_tool_output(tool_call_id=…)` pointer. **Recoverable** (full bytes still on disk).
- **L2 budget gate** — `hard_cap = ContextWindow − max(MaxOutputTokens,20000) − 13000`; WARN at `0.75×hard_cap`.
- **L2.5 hard-drop** (`dropOldestPairs`) — deletes oldest user/assistant rounds until under cap, writes one `aura.context_rot_events` row. **Lossy, irreversible.**
- **overflow** — `ErrContextWindowExceeded` (context.go:62) → the REPL suggests `aura chat new`. **Dead end; nothing summarizes-then-continues.**

There is **no summarization anywhere in the codebase** (verified: `summar|compact|compress|distill|handoff` across `internal/` + `cmd/` — all unrelated hits). The closest existing pattern, and the template this phase mirrors, is the auto-title worker `internal/conversations/title.go`: `GenerateTitle(ctx, client llm.Client, model string, history []llm.Message) (string, error)` builds a 2-message `llm.Request` (`titlePrompt` + rendered history), `Temperature 0.3`, `MaxTokens 32`, drains `client.Stream`, sanitizes — with the Runner owning the `WithoutCancel`/`WithTimeout`/`WaitGroup` lifecycle (`runner.maybeAutoTitle`).

The reference material for the summarization prompt is `docs/compact_prompt.md` — a verbatim copy of Claude Code's own `/compact` prompt (7-section `<analysis>`+`<summary>` schema + a caller-injected "Compact Instructions" hook). It ships as documentation only; this phase adapts it into a byte-stable Go constant (Aura-neutralized framing, English-only rule per project convention, an explicit output-length cap).

Persistence primitives that exist and are reused: `conversations.Store.AppendTurn(ctx, AppendTurnParams)` (atomic INSERT-turn + aggregate UPDATE in one `db.WithTx`, row-locked monotonic `seq` when `Seq<=0`); the `context_rot_events` audit table + `ListContextRotEvents` projection (the pattern the new `conversation_compactions` table copies); migration 0017 branch columns (`branch_id`/`parent_seq`) — **not used** by the chosen checkpoint-watermark model, but noted as the rejected alternative. The composition root is `cmd/aura/chat_boot.go assembleChatEnv` → `runner.New(deps)`; the Runner's narrow store interface is `internal/runner/interfaces.go ConversationStore`.

## Requirements

1. **`chat_compact` core — LLM summarization call.** A single bounded LLM call condenses a conversation's history into a structured summary.
   - Current: No summarization exists; `title.go` is the only LLM-over-history call (32-token label).
   - Target: New `internal/conversations/compact.go` exposes `CompactConversation(ctx, client llm.Client, model string, history []llm.Message, opts CompactOptions) (CompactResult, error)` mirroring `generateTitle`: 2-message `llm.Request` (`compactSystemPrompt` system + rendered history), `MaxTokens = opts.MaxOutputTokens`, drains `client.Stream`, accumulates text + trailing `Usage`. `CompactResult{Summary string, TokensBefore int, TokensAfter int, Usage *llm.Usage}`. `opts.Focus` (optional per-invocation instruction) is appended to the system prompt as the "Compact Instructions" hook. Empty/whitespace summary → error (never persist an empty summary).
   - Acceptance: Unit test with a stub `llm.Client` returning a canned summary → `CompactResult.Summary` populated, `TokensBefore` = tiktoken count of input history, `TokensAfter` = count of the summary; a stub returning empty text → error, no persistence side effect; `opts.Focus="focus on the failing test"` appears in the outbound `Request.Messages[0].Content`.

2. **Adapted compaction prompt (byte-stable constant).** The Claude Code prompt is adapted, not copied verbatim.
   - Current: `docs/compact_prompt.md` is TypeScript-flavored, coding-agent-framed, English implicit, unbounded.
   - Target: `compactSystemPrompt` constant in `compact.go` keeps the 7-section `<analysis>`+`<summary>` schema (Primary Request/Intent, Key Technical Concepts, Files & Code Sections, Problem Solving, Pending Tasks, Current Work, Optional Next Step) but: (a) Aura-neutral framing (general agent, not "development work"); (b) an explicit "Reply in English only" clause (project rule, matching `titlePrompt`); (c) an output-length instruction bounded to the summary budget; (d) language-neutral illustrative examples. `docs/compact_prompt.md` is retained as the provenance reference and gains a header note pointing at the constant.
   - Acceptance: The constant is a package-level `const` (never templated per call — the only per-call variation is the appended `opts.Focus`); a test asserts it contains the 7 section headers and the English-only clause; `docs/compact_prompt.md` references the constant.

3. **Checkpoint-watermark persistence — migration + store.** The summary is persisted non-destructively with a watermark; originals are retained.
   - Current: `conversation_turns` is append-only-by-seq; no "replace history with a summary" primitive; `context_rot_events` audits L2.5 drops only.
   - Target: Migration `0036_conversation_compactions.up.sql` creates `aura.conversation_compactions (id uuid pk, conversation_id uuid FK→conversations ON DELETE CASCADE, checkpoint_seq int NOT NULL, summary_turn_seq int NOT NULL, trigger text CHECK IN (manual,auto), model text, tokens_before int, tokens_after int, created_at timestamptz default now())` with index `(conversation_id, created_at DESC)`; `.down.sql` drops it. sqlc queries `InsertConversationCompaction`, `GetLatestCompaction(conversation_id)`. New `internal/conversations/store_compact.go`: `RecordCompaction(ctx, params) error` and `LatestCompaction(ctx, conversationID) (*Compaction, error)`. The summary text is persisted as one `role='user'` turn via `AppendTurn` carrying the marker `__aura_compaction_summary__` in `ToolCallID` (a field a real user turn never sets — same trick as `alwaysBlockMarker`); `checkpoint_seq` = the seq of the newest turn folded into the summary, `summary_turn_seq` = the summary turn's allocated seq. The whole operation (append summary turn + insert compaction row) runs in one transaction so a crash never leaves a dangling watermark.
   - Acceptance: `db_integration` test: create a conversation with N turns, run compaction → exactly one `conversation_compactions` row (`trigger`, `checkpoint_seq`, `summary_turn_seq`, token counts) + one new summary turn with the marker; all N original turns still present in `conversation_turns` (FTS `SearchConversationTurns` still matches their content); the append + watermark are atomic (an injected failure rolls both back).

4. **History reconstruction honors the checkpoint.** After compaction, the loaded history starts from the summary, not `seq=1`.
   - Current: `LoadManagedHistory` → `managedFromTurns` loads the full seq-ordered turn list, then the ladder runs.
   - Target: Before `injectAlwaysBlock`, `managedFromTurns` consults `LatestCompaction(conversationID)`; when present it truncates the loaded body to `[the summary turn] + [turns with seq > checkpoint_seq]`, dropping the pre-checkpoint body turns from the *in-context* list only (they stay in the DB). The summary turn is protected exactly like the always-block: `isCompactionSummary(t)` (keyed on the marker) makes `applyL1` and `dropOldestPairs` never touch it, and `toMessages` strips the marker so it renders as a clean `role='user'` message. `messages[0]` (system L0) stays byte-identical → the KV-cache prefix survives; the cache invalidates only from the summary turn onward (a one-time, unavoidable cost at compaction). Reconstruction is deterministic: two consecutive `LoadManagedHistory` calls on a compacted conversation are byte-identical.
   - Acceptance: Unit test: given a turn list + a fake `LatestCompaction` at `checkpoint_seq=K`, the reconstructed messages = `[system, always-block, summary, turns>K]` and exclude every body turn `≤K`; the summary survives an L2.5 run that drops other pairs; two loads are byte-equal. `db_integration`: after a real compaction, `LoadManagedHistory` returns the summary + only post-checkpoint turns.

5. **Manual `/compact` — CLI subcommand.** `aura chat compact <id>` compacts a conversation from the terminal.
   - Current: `runChat` (cmd/aura/chat.go) hand-rolled switch: `list|new|resume|archive|unarchive|delete|rename|search`; no `compact`.
   - Target: `case "compact":` added to the switch → `chatCompact(args)` in new `cmd/aura/chat_compact.go`, booting via `bootChat(ctx)`, calling the compaction path on the store/runner for the given `<id>`, with optional `--focus "<instruction>"`. Prints `tokens_before → tokens_after (Δ%)` and the compaction id. A conversation with too little history to compact (e.g. under a minimum turn/token floor) prints a clear "nothing to compact" message and exits 0.
   - Acceptance: `aura chat compact <id>` on a multi-turn conversation prints the token delta and writes the watermark + summary turn; `--focus` is threaded into `CompactOptions.Focus`; a fresh 1-turn conversation prints "nothing to compact" and writes no row.

6. **Manual `/compact` — interactive REPL slash router.** The REPL gains its first real slash-command dispatcher.
   - Current: `cmd/aura/chat_repl.go chatLoop` only special-cases the literal `/exit`; every other line goes straight to `runner.Turn`. No slash router.
   - Target: A small `dispatchSlash(line) (handled bool, out string)` in the REPL (mirroring Telegram's `dispatchRich`), invoked in `chatLoop` immediately after the `/exit` check and before the turn dispatch. It handles `/compact [focus text]` (compacts `d.convID`, prints the token delta) and is structured so future slash commands slot in. Unknown `/`-prefixed input returns a "unknown command; type your message or /exit" hint (not sent to the LLM). Non-slash input is unchanged.
   - Acceptance: In-REPL `/compact` compacts the active conversation and prints the delta without invoking an LLM *turn* (only the compaction call); `/bogus` returns the hint and does not reach `runner.Turn`; a normal message still runs a turn unchanged.

7. **Manual `/compact` — Telegram.** Parity with the other channel commands.
   - Current: `internal/channels/telegram/commands.go dispatchRich` switch: `/start /help /cancel /cost /search /new /reset /clear /stop`; no `/compact`.
   - Target: `/compact` added to `dispatchRich`, intercepted before any LLM turn (like `/clear`), compacting the chat's deterministic conversation and replying with the token delta; `/help` text updated to advertise it.
   - Acceptance: `dispatch(ctx, chatID, "/compact")` is handled (never forwarded to the LLM), compacts the chat's conversation, and returns a user-facing confirmation with the token delta; `/help` lists `/compact`.

8. **Auto-fallback at the overflow dead-end.** Compaction replaces the `ErrContextWindowExceeded` failure.
   - Current: `runner.loadTurnHistory` → `LoadManagedHistory`; an `ErrContextWindowExceeded` propagates up and the REPL tells the user to start a new chat.
   - Target: When `AURA_COMPACT_AUTO_ENABLED` (default true) and `loadTurnHistory` gets `ErrContextWindowExceeded`, the Runner runs `CompactConversation` **once** (bounded by `WithTimeout`, `WithoutCancel` from the request ctx so a client disconnect can't corrupt a half-written watermark), persists the checkpoint (Req#3), then re-loads managed history and proceeds. If the reload is *still* over the cap (e.g. a single oversized latest turn that compaction of prior history cannot shrink), the original `ErrContextWindowExceeded` is surfaced — bounded, no retry loop. L1 and L2.5 are unchanged; auto-compaction sits strictly after the L2.5 dead-end. `trigger='auto'` on the row. A compaction LLM failure surfaces the original window error (never a worse state).
   - Acceptance: Integration/unit test with a stubbed over-cap history → the load returns `ErrContextWindowExceeded`, the Runner auto-compacts (one `trigger='auto'` row), re-loads, and the turn proceeds; a second forced over-cap after compaction surfaces the error exactly once (no infinite loop); `AURA_COMPACT_AUTO_ENABLED=false` → the old dead-end behavior is preserved.

9. **Config knobs + composition wiring.** Compaction is configured via `AURA_COMPACT_*` and threaded through the composition root.
   - Current: Context knobs (`AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS`, model window vars) exist; no compaction knobs; the knob registry `internal/config/config_knobs.go knobRegistry()` type-checks all `AURA_*` vars.
   - Target: Register in `knobRegistry()` and parse into `config.Config`: `AURA_COMPACT_AUTO_ENABLED` (bool, default true), `AURA_COMPACT_MAX_OUTPUT_TOKENS` (int, default 4096 — the summary budget), `AURA_COMPACT_TIMEOUT_SEC` (int, default 60 — bounds the auto call), `AURA_COMPACT_MODEL` (string, default "" → same model as the conversation; an optional cheaper-model override). Threaded into `runner.Deps` at `cmd/aura/chat_boot.go assembleChatEnv` (the way `EvictAfter` is), and exposed on `runner.ConversationStore`/Runner as needed. `aura config validate` type-checks the new knobs and `.env.example`/generated docs pick them up from the registry.
   - Acceptance: `aura config validate` passes with the new knobs set and rejects a non-bool `AURA_COMPACT_AUTO_ENABLED` / non-int `AURA_COMPACT_MAX_OUTPUT_TOKENS`; defaults apply when unset; a set `AURA_COMPACT_MODEL` is used for the compaction call while the conversation model is unchanged.

10. **Audit + cost attribution.** Compaction is auditable and its LLM cost is billed to the conversation.
    - Current: `context_rot_events` audits L2.5; `title.go` discards its call's `Usage`.
    - Target: Every compaction writes the `conversation_compactions` row (Req#3) with `trigger`, `model`, `tokens_before/after`. The compaction call's `Usage` (input/output/cached tokens + cost) is added to the conversation aggregates via the existing aggregate-update path (a compaction is a real billable call, unlike a discardable title). A `ListCompactions(conversationID)` projection (mirroring `ListContextRotEvents`) exposes the history for a future web cockpit gauge (UI itself out of scope, Req boundary).
    - Acceptance: `db_integration`: after a compaction, `conversations.total_input_tokens/total_output_tokens/total_cost_usd` increased by the compaction call's usage; `ListCompactions(id)` returns the row(s) in `created_at DESC` order with correct fields.

11. **Bounded, best-effort, never-corrupting.** Neither trigger can hang a turn or leave inconsistent state.
    - Current: The auto-title worker is the bounded-best-effort precedent (`WithoutCancel`/`WithTimeout`/`WaitGroup`, errors discarded).
    - Target: The auto path is `WithTimeout(AURA_COMPACT_TIMEOUT_SEC)` + `WithoutCancel` so it completes or fails cleanly regardless of client cancellation; the persist step (summary turn + watermark) is one transaction (Req#3) so a failure rolls back atomically; a compaction error never escalates a normal turn into a crash. The manual path surfaces errors to the user (CLI/REPL/Telegram) rather than silently swallowing them (a user asked for it explicitly).
    - Acceptance: Test: a compaction LLM timeout on the auto path → the turn surfaces the original `ErrContextWindowExceeded`, no partial row/turn written (`goleak` clean, no dangling goroutine); a DB failure mid-persist → neither the summary turn nor the watermark row exists (both rolled back); the manual path returns the error to the caller.

## Boundaries

**In scope:**
- `internal/conversations/compact.go` — `CompactConversation` + `compactSystemPrompt` (adapted from `docs/compact_prompt.md`) + history-render + sanitize helpers.
- Checkpoint-watermark persistence: migration `0036_conversation_compactions`, sqlc queries, `store_compact.go` (`RecordCompaction`/`LatestCompaction`/`ListCompactions`), summary-turn marker, atomic persist.
- Checkpoint-aware reconstruction in `context.go` (`managedFromTurns` truncation + `isCompactionSummary` protection + marker strip in `toMessages`).
- Manual `/compact`: CLI `aura chat compact <id> [--focus]`, REPL slash router + `/compact`, Telegram `/compact` + `/help` update.
- Auto-fallback in `runner.loadTurnHistory` at the `ErrContextWindowExceeded` seam (behind `AURA_COMPACT_AUTO_ENABLED`).
- Config knobs `AURA_COMPACT_*` (registry + Config + `chat_boot` wiring), cost attribution to conversation aggregates, `conversation_compactions` audit.
- Bounded/best-effort lifecycle (WithTimeout/WithoutCancel), atomic persist, `goleak`/`-race` clean.
- **[AMENDED 2026-07-12] Web cockpit manual `/compact` trigger** — `/compact` QuickCommand in the composer (`web/src/chat/composer/SkillPicker.tsx` + `skillPickerModel.ts`) wired to a new AG-UI POST route (`internal/agui/conversations_api.go`, sibling to the rot-events handler) that runs the shared server-side compaction path and returns the token delta; the web cockpit becomes the 4th manual trigger surface (parity with CLI/REPL/Telegram, Req#5–7). See §Amendment 2026-07-12.
- **[AMENDED 2026-07-12] Compaction markers on the context-budget gauge** — `GET /api/conversations/{id}/compactions` (thin `ListCompactions` wrapper) rendered as visually-distinct event markers on `web/src/chat/ContextBudgetGauge.tsx`, sibling to the existing `context_rot_events` (`pairs_dropped`) markers (D-11 parity). Read-path only. See §Amendment 2026-07-12.

**Out of scope:**
- **A new pre-L2.5 "L2.4" auto-compaction tier** (compact *before* the lossy hard-drop). Operator explicitly chose auto-fallback at the dead-end only; long conversations still lose oldest rounds to L2.5 first, and the manual `/compact` is the proactive tool to summarize before that loss. Documented as a known boundary, revisitable in a follow-on.
- **Branch-leaf persistence** (migration 0017 `ForkBranch`) — the rejected alternative to checkpoint-watermark; branch semantics model alternate futures, not a replaced past.
- **Web cockpit compaction UI beyond the two amended-in surfaces above** (dedicated compaction-history panel, per-compaction summary preview/diff, undo) — the trigger + gauge markers are in; richer compaction UX stays a later frontend phase.
- **Multimodal/image-turn summarization** — text turns only; image sidecars are summarized by reference, not re-described.
- **Neo4j long-term memory integration** (spilling the summarized rounds into the agent-memory subgraph) — Phase 15 memory subsystem territory; not this phase.
- **Automatic re-compaction of an already-compacted conversation on every overflow** beyond the single bounded auto-attempt per load (Req#8) — no compaction-of-a-compaction chaining in this phase.
- **Changing L1 (tool-eviction) or L2.5 (hard-drop) behavior** — they stay byte-for-byte as shipped.

## Constraints

- **PRD alignment:** This activates the L3 deferral documented in `04-SPEC.md` (Req#10 + Out-of-scope) and PRD §1.8 OQ#3 — **not a PRD deviation**. `/gsd-discuss-phase` must confirm no PRD-amendment is needed (the deferral note may want a one-line "activated in Phase 42" update, which is documentation, not an architectural amendment).
- **KV-cache invariant (Phase 6):** `messages[0]` (the `agent.SystemPrompt` constant) MUST stay byte-identical across compaction — the summary turn is inserted at `messages[2]` (after L0 + always-block), never into the cached system prefix. Verify the cache-stable-prefix test still holds on a compacted conversation.
- **Coverage floor ≥ 85%** across the full tag matrix (unit + `db_integration`), overriding the PRD 75/60 (CLAUDE.md). New DB-gated code (migration, sqlc, store) needs `db_integration` tests that actually run under `$CI` (no-skip-as-green); pure logic (prompt adaptation, reconstruction truncation, marker protection, token math) needs daemon-free unit tests. Mutation ≥ 70% killed on `compact.go` + the reconstruction change.
- **`-race` + `goleak` clean; `golangci-lint` 0; every file ≤ 600 LOC** — `context.go` is already large; the checkpoint logic may split into `context_compaction.go` (refactor-on-touch). `dupl` folding for the render/sanitize helpers shared with `title.go` (extract a common `renderHistory` if the two diverge only by cap).
- **English-only prompt** (project rule; matches `titlePrompt`).
- **Token estimation** uses the existing vendored `tiktoken-go` cl100k_base (`internal/conversations/tiktoken.go`) — the same ~5-10% approximation already used for budget gating (before/after counts are gating-grade, not billing-grade; billing comes from the call's real `Usage`).
- **Atomicity:** summary turn + watermark row in one `db.WithTx`; auto path `WithoutCancel` + `WithTimeout` so client cancellation cannot corrupt a half-written checkpoint.
- **Migration `0036`** occupies the next free slot (current head `0035_assets_source_kind_agent`); runs as `aura_migrate`, denied as `aura_app`; `.down.sql` reverses cleanly; re-run is a no-op.
- **Bounded auto-attempt:** at most one auto-compaction per `loadTurnHistory` call; a still-over-cap reload surfaces the original error — no retry loop.

## Acceptance Criteria

- [ ] `aura chat compact <id>` compacts a multi-turn conversation, prints `tokens_before → tokens_after`, writes one `conversation_compactions` row + one marked summary turn; original turns remain in `conversation_turns`.
- [ ] In-REPL `/compact [focus]` compacts the active conversation and prints the delta; `/bogus` returns a hint and is never sent to the LLM; normal messages still run a turn.
- [ ] Telegram `/compact` is bot-intercepted (never forwarded to the LLM), compacts the chat, and confirms with the token delta; `/help` advertises it.
- [ ] After compaction, `LoadManagedHistory` returns `[system, always-block, summary, turns>checkpoint]`, byte-identical across two calls; the summary turn survives an L2.5 drop of other pairs.
- [ ] `messages[0]` stays byte-identical before/after compaction (KV-cache prefix preserved) — cache-stable-prefix test green on a compacted conversation.
- [ ] Auto-fallback: a history that returns `ErrContextWindowExceeded` triggers exactly one `trigger='auto'` compaction, reloads, and the turn proceeds; a still-over-cap reload surfaces the error once (no loop); `AURA_COMPACT_AUTO_ENABLED=false` preserves the old dead-end.
- [ ] `CompactConversation` with an empty-summary stub errors and writes nothing; with `opts.Focus` set, the focus text rides the outbound system prompt.
- [ ] The compaction call's `Usage` is added to `conversations` token + USD aggregates; `ListCompactions(id)` returns the audit rows.
- [ ] Persist is atomic: an injected DB failure leaves neither the summary turn nor the watermark; an auto-path timeout leaves no partial state and no leaked goroutine (`goleak`).
- [ ] `aura config validate` type-checks `AURA_COMPACT_AUTO_ENABLED`/`AURA_COMPACT_MAX_OUTPUT_TOKENS`/`AURA_COMPACT_TIMEOUT_SEC`/`AURA_COMPACT_MODEL`; defaults apply when unset.
- [ ] `aura db migrate` applies `0036` cleanly; re-run is a no-op; `.down.sql` reverses; denied as `aura_app`, succeeds as `aura_migrate`.
- [ ] Coverage ≥ 85% across the touched packages on the full tag matrix; `-race` + `golangci-lint` clean; every touched file ≤ 600 LOC.

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                                 |
|--------------------|-------|------|--------|------------------------------------------------------------------------|
| Goal Clarity       | 0.90  | 0.75 | ✓      | Activates Phase-4 deferred L3; two triggers + persistence model locked |
| Boundary Clarity   | 0.88  | 0.70 | ✓      | In/out explicit; L2.4-tier + branch-leaf + web gauge explicitly out    |
| Constraint Clarity | 0.85  | 0.65 | ✓      | KV-cache invariant, atomicity, migration 0036, coverage floor locked   |
| Acceptance Criteria| 0.86  | 0.70 | ✓      | 11 pass/fail criteria, machine-checkable                               |
| **Ambiguity**      | 0.14  | ≤0.20| ✓      | Two open discuss-items: summary-turn role, min-compact floor (below)   |

Status: ✓ = met minimum. Open items for `/gsd-discuss-phase` (do not block the SPEC gate): (a) confirm summary-turn role = `user` (vs a dedicated synthetic role) and its exact placement relative to always-block; (b) set the minimum-history floor under which `/compact` is a no-op (turn count and/or token threshold); (c) decide whether `AURA_COMPACT_MODEL` defaults to a cheaper model out of the box or to the conversation model.

## Interview Log

| Round | Perspective     | Question summary                                   | Decision locked (operator, 2026-07-08)                                              |
|-------|-----------------|----------------------------------------------------|-------------------------------------------------------------------------------------|
| 0     | Researcher      | What exists today vs the parity target?            | LLM-free L1/L2/L2.5 ladder + dead-end error; no summarization; `title.go` is the analog; `docs/compact_prompt.md` is the reference prompt |
| 1     | Boundary Keeper | When does semantic compaction run?                 | Manual `/compact` (CLI+REPL+Telegram) **+** auto-fallback at the `ErrContextWindowExceeded` dead-end; L1/L2.5 unchanged |
| 1     | Boundary Keeper | How does the summary replace history?              | Checkpoint-watermark, non-destructive (summary turn + `conversation_compactions` row; originals retained for audit + FTS) |
| 1     | Boundary Keeper | Deliverable format under PRD-first discipline?     | This GSD phase SPEC first → `/gsd-discuss-phase` → `/gsd-plan-phase`                 |
| 1     | Researcher      | Is this a PRD deviation?                            | No — activates the L3 deferral documented in `04-SPEC.md` + PRD §1.8 OQ#3            |

## Amendment 2026-07-12 (discuss-phase, operator-directed)

`/gsd-discuss-phase 42` resolved the three open items (summary-turn role = `role='user'` + marker @ `messages[2]`; derived min-compact floor with no new knob; `AURA_COMPACT_MODEL` default `""` = same model as conversation) and made two additional decisions the operator directed:

1. **Prompt schema refinement (Req#2):** adopt the newer 9-section Claude Code compaction schema (adds "All user messages" + "Errors and fixes" sections, verbatim preservation of user-stated security/safety constraints, and a "reply in TEXT ONLY, call no tools" guard) instead of the older 7-section `docs/compact_prompt.md` — an *adaptation* under Req#2, mitigating the "governance decay" failure mode. The Req#2 acceptance test extends from 7 → 9 section headers + the no-tools guard.

2. **Frontend scope addition (SPEC boundary expansion):** operator directed frontend UI be included ("we must do also on frontend UI"). Two web cockpit surfaces move from Out-of-scope to In-scope (see Boundaries `[AMENDED 2026-07-12]`): the web manual `/compact` trigger and the compaction markers on `ContextBudgetGauge`. This is an operator-owned scope *addition*, documented here rather than via a full re-spec because it reuses shipped patterns (rot-events read-path + composer QuickCommand) and adds no new architecture. Richer compaction UI stays deferred.

Full decision rationale + canonical refs + code_context: `42-CONTEXT.md` (D-01…D-10). PRD alignment unchanged — still activates the Phase-4 L3 deferral, not a PRD deviation (D-07).

---

*Phase: 42-llm-conversation-compaction (PROVISIONAL number — place with `/gsd-phase`)*
*Spec created: 2026-07-08; amended 2026-07-12 (discuss-phase)*
*Next step: `/gsd-plan-phase 42` — read amended Boundaries + §Amendment 2026-07-12 + 42-CONTEXT.md before planning*
