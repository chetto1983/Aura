# Phase 42: LLM Conversation Compaction - Context

**Gathered:** 2026-07-12
**Status:** Ready for planning

<domain>
## Phase Boundary

Give Aura's agent runtime **LLM-driven semantic conversation compaction** — the "L3" tier Phase 4 deferred. A long conversation is summarized into a structured handoff and **continued in place** (summarize-then-continue), instead of hitting the `ErrContextWindowExceeded` dead-end or only the lossy L2.5 hard-drop.

Two trigger surfaces, both in this phase:
- **Manual `/compact`** — CLI (`aura chat compact <id>`), interactive REPL (`/compact`), Telegram (`/compact`).
- **Auto-fallback** — fires once at the `ErrContextWindowExceeded` dead-end (after L2.5 can no longer reduce under the hard cap), replacing "start a new chat".

Persistence is **checkpoint-watermark, non-destructive**: summary persisted as one protected turn + one `aura.conversation_compactions` audit/watermark row; original pre-checkpoint turns are retained in `conversation_turns` (FTS + audit still see them).

</domain>

<spec_lock>
## Requirements (locked via SPEC.md)

**11 requirements are locked.** See `42-SPEC.md` for full requirements, boundaries, and acceptance criteria.

Downstream agents MUST read `42-SPEC.md` before planning or implementing. Requirements are not duplicated here.

**In scope (from SPEC.md):**
- `internal/conversations/compact.go` — `CompactConversation` + `compactSystemPrompt` (adapted from `docs/compact_prompt.md`) + history-render + sanitize helpers.
- Checkpoint-watermark persistence: migration `0036_conversation_compactions`, sqlc queries, `store_compact.go` (`RecordCompaction`/`LatestCompaction`/`ListCompactions`), summary-turn marker, atomic persist.
- Checkpoint-aware reconstruction in `context.go` (`managedFromTurns` truncation + `isCompactionSummary` protection + marker strip in `toMessages`).
- Manual `/compact`: CLI `aura chat compact <id> [--focus]`, REPL slash router + `/compact`, Telegram `/compact` + `/help` update.
- Auto-fallback in `runner.loadTurnHistory` at the `ErrContextWindowExceeded` seam (behind `AURA_COMPACT_AUTO_ENABLED`).
- Config knobs `AURA_COMPACT_*` (registry + Config + `chat_boot` wiring), cost attribution to conversation aggregates, `conversation_compactions` audit.
- Bounded/best-effort lifecycle (WithTimeout/WithoutCancel), atomic persist, `goleak`/`-race` clean.

**In scope — added 2026-07-12 via discuss-phase (SPEC boundary amendment, see D-08/D-09):**
- **Web cockpit manual `/compact` trigger** — `/compact` QuickCommand in the composer (`web/src/chat/composer/SkillPicker.tsx`) + a new AG-UI POST endpoint running the compaction path and returning the token delta. Makes the web cockpit the 4th manual trigger surface (parity with CLI/REPL/Telegram Req#5–7).
- **Compaction markers on the context-budget gauge** — `GET /api/conversations/{id}/compactions` (thin `ListCompactions` wrapper, the projection Req#10 already builds) rendered as event markers on `web/src/chat/ContextBudgetGauge.tsx`, sibling to the existing rot-events markers (D-11 parity).

**Out of scope (from SPEC.md):**
- A new pre-L2.5 "L2.4" auto-compaction tier (compact *before* the lossy hard-drop). NOTE: open-webui's per-model "Context Compaction Threshold" is exactly this proactive-threshold model — confirms L2.4 is a real future tier, still deferred.
- Branch-leaf persistence (migration 0017 `ForkBranch`) — the rejected alternative to checkpoint-watermark.
- Web cockpit frontend BEYOND the two surfaces amended in above (e.g. a dedicated compaction-history panel, per-compaction summary preview/diff) — the gauge markers + web trigger are now in; richer compaction UI stays a later frontend phase.
- Multimodal/image-turn summarization — text turns only.
- Neo4j long-term memory integration (spilling summarized rounds into the agent-memory subgraph) — Phase 15 territory.
- Automatic re-compaction of an already-compacted conversation beyond the single bounded auto-attempt per load.
- Changing L1 (tool-eviction) or L2.5 (hard-drop) behavior — they stay byte-for-byte.

</spec_lock>

<decisions>
## Implementation Decisions

The SPEC (ambiguity 0.14) flagged three open discuss-items; the discussion resolved all three plus a research-driven prompt-schema refinement and a PRD-doc housekeeping call. All five below are LOCKED for planning.

### Summary-turn role & placement
- **D-01:** The summary is persisted as one `role='user'` turn carrying the marker `__aura_compaction_summary__` in `ToolCallID` — the identical trick to the proven `alwaysBlockMarker` (`internal/conversations/context.go:48`, `isAlwaysBlock` at :303). Rejected the dedicated-synthetic-role alternative (provider role-ordering risk, no protection helper to reuse, more KV-cache/wire-format surface).
- **D-02:** Placement is `messages[2]` — after `messages[0]` system L0 and `messages[1]` always-block. `messages[0]` stays byte-identical → the KV-cache prefix survives (Phase-6 invariant); cache invalidates only from the summary turn onward (one-time, unavoidable at compaction). Reconstructed history = `[system, always-block, summary, turns with seq > checkpoint_seq]`.
- **D-03:** The summary turn is protected like the always-block: `isCompactionSummary(t)` (keyed on the marker) makes `applyL1` and `dropOldestPairs` never touch it; `toMessages` strips the marker so it renders as a clean `role='user'` message.

### Minimum-compact floor
- **D-04:** `/compact` is a no-op ("nothing to compact", exit 0, no row written) when the history is below a **derived** floor — no new env knob. No-op if body turns ≤ ~3 **OR** estimated input tokens < `2 × AURA_COMPACT_MAX_OUTPUT_TOKENS` (compaction cannot plausibly save tokens if the input isn't at least ~2× the summary budget). Derived from existing values → keeps the config surface at the 4 knobs SPEC Req#9 locks (rejected: a 5th `AURA_COMPACT_MIN_TOKENS` knob = config sprawl for a rarely-tuned value; rejected: hardcoded literals that drift when the summary budget changes). Grounding: industrial compaction triggers at 5–20k tokens; this derived floor lands sensibly under that for the manual case.

### Compaction model default
- **D-05:** `AURA_COMPACT_MODEL` defaults to `""` → the **same model as the conversation**. Rationale: compaction is infrequent (only the overflow dead-end or explicit `/compact`) and the summary is **load-bearing** — a weak summary poisons every turn after it (governance-decay failure mode). Aura already has a cheap routine tier (L1 observation-masking / tool-eviction); paying for the good model on this rare, one-shot, high-stakes call is the right trade. `AURA_COMPACT_MODEL` remains the escape hatch for cost-sensitive operators (rejected: shipping a cheap default).

### Prompt schema (SPEC Req#2 refinement)
- **D-06:** Adopt the **newer 9-section** Claude Code compaction schema, not the older 7-section `docs/compact_prompt.md`. Keep the 7 original sections (Primary Request/Intent, Key Technical Concepts, Files & Code Sections, Problem Solving, Pending Tasks, Current Work, Optional Next Step) and ADD: (a) an **"All user messages"** section + an **"Errors and fixes"** section; (b) **verbatim preservation of user-stated security/safety constraints** so they keep applying after compaction; (c) a **"reply in TEXT ONLY, call no tools"** guard in `compactSystemPrompt`. This is an *adaptation* of the prompt (still within Req#2's "adapt, not copy" mandate), NOT a new capability — it directly counters the "Governance Decay" failure mode (compaction silently erasing safety constraints in long-horizon agents). Aura specifics still apply on top: Aura-neutral framing, "Reply in English only" clause (matches `titlePrompt`), explicit output-length cap bounded to `AURA_COMPACT_MAX_OUTPUT_TOKENS`. **Planner note:** the Req#2 acceptance test ("asserts it contains the 7 section headers") must be extended to the 9 headers + the English-only clause + the no-tools guard.

### PRD/doc activation (housekeeping, in-scope)
- **D-07:** This phase is **confirmed NOT a PRD deviation** — it activates the L3 deferral documented in `04-SPEC.md` (Req#10 + Out-of-scope) and PRD §1.8 OQ#3. **No PRD-amendment commit needed.** Fold a one-line "activated in Phase 42" note into `04-SPEC.md`'s L3 deferral entry and PRD §1.8 OQ#3 within this phase's scope (documentation only, ~2-line touch) so the PRD truth-source doesn't keep asserting "L3 NOT implemented" after it ships.

### Web cockpit frontend (SPEC boundary amendment — operator-directed 2026-07-12)
Operator directed frontend UI be included ("we must do also on frontend UI"). These two surfaces were **out of scope in `42-SPEC.md`**; folding them in **expands a locked SPEC boundary** → requires a SPEC-amendment (Boundaries section) BEFORE planning (PRD-first). This is a scope *addition* the operator owns, NOT a re-interpretation of existing scope. **Planner MUST read the amended `42-SPEC.md` Boundaries + the new §Amendment note before planning.**
- **D-08 (web manual `/compact` trigger):** Add `/compact` as a `QuickCommand` in the composer command palette (`web/src/chat/composer/skillPickerModel.ts` QuickCommand union + `SkillPicker.tsx` icon map, alongside add-files/new-chat/clear). Wire it to a **new AG-UI POST route** (`internal/agui/conversations_api.go`, sibling to the existing GET rot-events handler) that runs the same server-side compaction path the CLI/REPL/Telegram triggers use and returns `{tokens_before, tokens_after, compaction_id}`; the composer renders the token delta as a system/toast line. Server-side compaction logic is shared (Req#5–7) — no duplicate compaction implementation. Below-floor (D-04) returns a "nothing to compact" response, surfaced as a non-error notice.
- **D-09 (compaction markers on ContextBudgetGauge):** Add `GET /api/conversations/{id}/compactions` (thin `ListCompactions` wrapper — `internal/agui/conversations_api.go`, exact sibling of `handleConversationRotEvents` at :215) + a typed client hook, and render compaction events as markers on the existing `web/src/chat/ContextBudgetGauge.tsx`, visually distinct from the rot-events (`pairs_dropped`) markers already there. Read-path only; purely additive.
- **D-10 (frontend quality bar):** New/edited React UI follows CLAUDE.md §Frontend_aesthetics (distinctive, not "AI slop"). The compaction marker must be visually distinguishable from rot-event markers (different glyph/color, not just a tooltip). Frontend tests follow the existing `web/src` vitest + testing-library convention (there is an existing `ContextBudgetGauge`/SkillPicker test suite to extend). NOTE: the CLAUDE.md ≥85% coverage floor is a Go-owned-surface metric; the web `web/src` suite is governed by its own frontend CI, not `scripts/coverage_gate.sh` — planner to confirm the web test-gate.

### Claude's Discretion
- Exact numeric constants inside the derived floor (the "~3 body turns" and the `2×` multiplier) — the planner/executor may tune within the D-04 rationale; the *shape* (derived, no new knob, floor ≥ some multiple of the summary budget) is locked.
- Whether the checkpoint reconstruction logic lives in `context.go` or splits into `context_compaction.go` — SPEC Constraints already flag `context.go` as large; refactor-on-touch / ≤600 LOC governs.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase spec + activated deferral
- `.planning/phases/42-llm-conversation-compaction/42-SPEC.md` — Locked 11 requirements, boundaries, constraints, acceptance criteria. MUST read before planning.
- `.planning/phases/04-*/04-SPEC.md` — the L3 deferral this phase activates (Req#10 "L3 LLM-driven compaction is explicitly NOT implemented"; Out-of-scope `chat_compact`). Gets the D-07 "activated in Phase 42" note.
- `prd.md` §1.8 OQ#3 — the PRD open-question this closes; gets the D-07 activation note.

### Prompt provenance
- `docs/compact_prompt.md` — verbatim OLDER 7-section Claude Code `/compact` prompt; retained as provenance reference, gains a header note pointing at the `compactSystemPrompt` constant. NOTE (D-06): the *newer 9-section* schema is the adaptation target, not this file's 7 sections.

### Code the phase mirrors / touches (SPEC Background)
- `internal/conversations/context.go` — the LLM-free ladder (L0/always-block/L1/L2/L2.5/overflow); `alwaysBlockMarker` (:48) + `isAlwaysBlock` (:303) are the marker/protection pattern D-01/D-03 copy; `managedFromTurns` is where checkpoint truncation lands (Req#4).
- `internal/conversations/title.go` — `generateTitle` is the template for `CompactConversation` (2-message `llm.Request`, drains `client.Stream`, takes an explicit `model string`); Runner owns the `WithoutCancel`/`WithTimeout`/`WaitGroup` lifecycle (`runner.maybeAutoTitle`).
- `internal/conversations/tiktoken.go` — vendored `tiktoken-go` cl100k_base for gating-grade before/after token counts.
- `internal/runner/interfaces.go` `ConversationStore` — the Runner's narrow store interface.
- `cmd/aura/chat_boot.go` `assembleChatEnv` → `runner.New(deps)` — composition root where `AURA_COMPACT_*` threads in (mirror `EvictAfter`).
- `cmd/aura/chat.go` `runChat` — the `list|new|resume|…` switch that gains `case "compact"`.
- `cmd/aura/chat_repl.go` `chatLoop` — gains the first real slash router `dispatchSlash`.
- `internal/channels/telegram/commands.go` `dispatchRich` — gains `/compact` (mirror `/clear` interception) + `/help` update.
- `internal/config/config_knobs.go` `knobRegistry()` — register the 4 `AURA_COMPACT_*` knobs.

### Web cockpit frontend (D-08/D-09 — the amended-in surfaces)
- `internal/agui/conversations_api.go` — AG-UI gateway; `handleConversationRotEvents` (:215, `GET /api/conversations/{id}/rot-events`) is the exact sibling template for the new `GET /compactions` read route AND the mux-registration pattern for the new POST compact-trigger route. `ListContextRotEvents` wrapper (:229) → `ListCompactions` wrapper.
- `internal/agui/types.go` — where the rot-event wire types live; add the compaction wire type alongside.
- `web/src/chat/ContextBudgetGauge.tsx` — the existing D-11 gauge consuming rot-events; gains compaction markers (D-09).
- `web/src/chat/composer/SkillPicker.tsx` + `web/src/chat/composer/skillPickerModel.ts` — the composer slash/command palette (`QuickCommand` union = add-files/new-chat/clear); `/compact` slots in here (D-08). `skillPickerModel.test.ts` is the test suite to extend.
- `web/src/chat/Composer.tsx` / `web/src/chat/ExternalStoreChat.tsx` — the chat send path where the `/compact` QuickCommand dispatches to the new AG-UI route and renders the token-delta notice.

### External research (context-rot / governance-decay grounding for D-05/D-06)
- Claude Code leaked `compact.md` skill (newer 9-section schema) — the D-06 adaptation target (local ref: `D:\tmp\system_prompts_leaks\Anthropic\Claude Code\bundled-skills\compact.md`).
- GPT-5.5/Codex system prompt (`D:\tmp\system_prompts_leaks\OpenAI\Codex\gpt-5.5-full.md` :88,:92) — validates automatic summarize-then-continue-in-place ("Do not restart from scratch; you continue naturally").
- arxiv "Governance Decay: How Context Compaction Silently Erases Safety Constraints in Long-Horizon LLM Agents" — the failure mode D-06's security-verbatim clause mitigates.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `alwaysBlockMarker` + `isAlwaysBlock` (`context.go:48`,`:303`) — copy verbatim as `__aura_compaction_summary__` + `isCompactionSummary` (D-01/D-03). Marker lives in `ToolCallID`, a field a real user turn never sets.
- `generateTitle` (`title.go`) — structural template for `CompactConversation`: 2-message request, `Temperature 0.3`, drains `client.Stream`, takes explicit `model string`. Compaction differs by `MaxTokens = opts.MaxOutputTokens` (not 32) and by persisting + billing its `Usage` (title discards it).
- `conversations.Store.AppendTurn` (atomic INSERT-turn + aggregate UPDATE in one `db.WithTx`, row-locked monotonic `seq`) — reused to persist the summary turn inside the same tx as the watermark row (Req#3 atomicity).
- `context_rot_events` table + `ListContextRotEvents` projection — the exact pattern `conversation_compactions` + `ListCompactions` copies.
- `tiktoken.go` cl100k_base — `TokensBefore`/`TokensAfter` counts (gating-grade; billing comes from the call's real `Usage`).

### Established Patterns
- Bounded best-effort worker lifecycle (`WithoutCancel` + `WithTimeout` + `WaitGroup`) from `runner.maybeAutoTitle` — the auto-compaction path (Req#11) follows it so a client disconnect can't corrupt a half-written checkpoint.
- Telegram `dispatchRich` command interception (before any LLM turn) — the template for both the Telegram `/compact` and the new REPL `dispatchSlash` router.
- Migration numbering: current head `0035_assets_source_kind_agent` → `0036_conversation_compactions` is the next free slot; `aura_migrate` applies, `aura_app` denied; `.down.sql` reverses; re-run no-op.
- **Frontend parity (D-08/D-09):** `handleConversationRotEvents` + `ContextBudgetGauge` + the rot-events markers are a complete, working template for the compaction read-path (route → wrapper → wire type → React hook → gauge marker). The composer `QuickCommand` union + `SkillPicker` is the working template for the web `/compact` trigger. Both new surfaces mirror shipped code — no new frontend architecture.

### Integration Points
- `runner.loadTurnHistory` → `LoadManagedHistory` is the `ErrContextWindowExceeded` seam where auto-fallback (Req#8) sits — strictly AFTER the L2.5 dead-end, at most one attempt per load.
- `managedFromTurns` (before `injectAlwaysBlock`) consults `LatestCompaction(conversationID)` to truncate the in-context body (Req#4) — DB body untouched.
- Compaction `Usage` flows into `conversations.total_input_tokens/total_output_tokens/total_cost_usd` via the existing aggregate-update path (Req#10) — a compaction is a real billable call.

</code_context>

<specifics>
## Specific Ideas

- The operator explicitly wanted external parity checked (`docs/compact_prompt.md`, Claude/GPT prompts under `D:\tmp`, online context-rot patterns). The research directly shaped D-05 (fidelity > cost, because summarization fidelity is the dominant failure mode) and D-06 (adopt the newer 9-section governance-hardened schema). GPT-5.5/Codex independently uses Aura's chosen summarize-then-continue-in-place model.
- Industrial trigger range (5–20k tokens) informed the D-04 derived-floor sizing.

</specifics>

<deferred>
## Deferred Ideas

- **Pre-L2.5 "L2.4" proactive auto-compaction tier** (summarize *before* the lossy hard-drop) — SPEC Out-of-scope; operator chose auto-fallback-at-dead-end only. Manual `/compact` is the proactive tool for now. open-webui's per-model "Context Compaction Threshold" (`AdvancedParams.svelte`) is exactly this tier — confirms it's a real, industry-standard future tier. Revisitable in a follow-on.
- **Richer compaction UI** beyond D-08/D-09 (dedicated compaction-history panel, per-compaction summary preview/diff, undo) — the trigger + gauge markers are now in scope; deeper compaction UX stays a later frontend phase.
- **Neo4j long-term memory spill** of summarized rounds into the agent-memory subgraph — Phase 15 territory.
- **"Observation masking halves cost vs LLM summarization" (research finding)** — Aura already has this as L1 tool-eviction; no action, noted as validation that the existing ladder is well-placed.

NOTE: The web `/compact` trigger + context-budget-gauge compaction markers were **moved from deferred INTO Phase 42 scope** on 2026-07-12 at operator direction (D-08/D-09) — this expanded the SPEC boundary and requires the SPEC-amendment note the planner must read.

</deferred>

---

*Phase: 42-llm-conversation-compaction*
*Context gathered: 2026-07-12*
