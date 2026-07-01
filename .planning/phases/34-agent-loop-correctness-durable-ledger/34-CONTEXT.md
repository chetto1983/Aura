# Phase 34: Agent-Loop Correctness + Durable Ledger - Context

**Gathered:** 2026-07-01
**Status:** Ready for planning

<domain>
## Phase Boundary

Close **11 audit findings** (F-003/004/005/009/010/029/030/031/040/045/048 → requirements **LOOP-01..11**) plus the **QUAL-04** correctness fixes deferred here from Phase 33, making the agent loop, HITL pause/resume, and conversation-sidecar persistence *industrially correct*.

Concretely: (1) terminal `text_response` exclusivity, (2) atomic HITL single+batch resume + atomic pause exposure, (3) fenced sidecar reads + crash-orphan reconciliation, (4) `send_file` outside-workspace determinism, (5) documented+asserted spilled-content search boundary, plus the mechanical fixes (atomic `fs_write`, mutating-panic classification, `Stop()` goroutine leak, int32 guard, double-`Validate`/pool-close).

Requirements LOOP-01..11 + QUAL-04 are **tightly locked** in `.planning/REQUIREMENTS.md`; this discussion captured only **HOW**, not WHAT. It was **research-backed** (4 parallel researchers: 2026 web patterns + the curated `D:/tmp` repos + the live Aura code) and every gray area converged on the **minimal industrial form** (no atomic bombs).

**⚠ Roadmap goal reconciliation (see D-07):** the ROADMAP Phase-34 goal prose says *"durable ledger state machine (migration 0025)."* Research showed the ledger is over-engineering for these findings, and **this phase now requires NO new DB migration.** All four ROADMAP success criteria remain satisfied. The roadmap goal text should be updated at plan-phase time (drop the ledger/migration clause). Also note: `0025_document_control_plane` already shipped, so "0025" in the goal was already stale — next free slot would have been 0026.

**It does NOT deliver** (scope fence, carried from `33-CONTEXT.md`): per-profile *runtime* enforcement of tool capabilities, the central **ToolGateway** + policy engine, the **mutating-tool durable ledger reservation** (F-011/GATE-03/04), or completing the tool `Mutating` classification — all **Phase 35+**. See Deferred Ideas.

</domain>

<decisions>
## Implementation Decisions

### Terminal `text_response` exclusivity (LOOP-01 / F-003)
- **D-01:** **Hard-reject the whole mixed model step → force replan.** If a terminal `text_response` appears with **any** sibling tool call, reject the step atomically before any sibling executes (return synthetic tool results + trip the recover/finalize path). NOT "allow read-only siblings" (option B).
- **D-01a:** Rationale — matches native semantics (Anthropic/OpenAI/LangGraph: any tool call present ⇒ *not* final) and needs **zero reliance on the `Mutating` flag**, which is currently untrustworthy (`skill`/`task`/`swarm_spawn` are unflagged and action-multiplexed, so "allow read-only siblings" would let `skill action=create` run beside a final answer — re-introducing F-003). Option B would first require a classification-hardening project (deferred to Phase 35).
- **D-01b:** Implement in `internal/agent/llm_agent_dispatch.go` by **reusing the existing dedup-trip path** (`appendSyntheticToolResults` + `maybeRecover`/`finalize`, already present for the dedup guard). Short-circuit when `terminalIdx >= 0 && len(runnable) > 0`. This also **fixes a latent bug**: a 2nd `text_response` in a step is silently dropped today. Bounded replan count reuses the existing `maybeRecover` counter.

### HITL resume/pause durability (LOOP-02/03/04 / F-004/029/030)
- **D-02:** **One cross-store `db.WithTx` transaction — NO ledger, NO migration.** Both `askuser.Store` and `conversations.Store` wrap the **same `*pgxpool.Pool`** and their tables (`aura.paused_states`, `aura.conversation_turns`) live in the **same generated sqlc package**, so a single `sqlc.New(tx)` already exposes every needed query. A single tx is the natural extension of the existing `WithTx` seam — the same shape as ADK-Go's `applyEvent` (single-tx event-append + state-update, no ledger).
- **D-03 (mechanical refactor for D-02):**
  1. sqlc: `MarkPausedStateResumed` `:exec → :execrows` (so the `RowsAffected==0 → ErrPauseNotFound` gate works inside the shared tx — today that's the *only* reason `askuser.MarkResumed` bypasses sqlc with a raw `pool.Exec`).
  2. `askuser`: add `MarkResumedTx(q,…)`, `MarkResumedBatchTx(q,…)`, `InsertTx(q,…)`; existing methods become thin `WithTx` wrappers (DRY).
  3. `conversations`: extract `appendTurnTx(q,…)` from the body already inside `AppendTurn`'s `db.WithTx` closure (sidecar spill stays **outside** the tx, as today); `AppendTurn` becomes a wrapper.
  4. New narrow **`ResumeCommitter`** seam (owns the pool, injected at the composition root) with `CommitResume` / `CommitResumeBatch` = one `db.WithTx` doing claim(s)+append(s). The runner calls it instead of split `MarkResumed → AppendTurn`.
- **D-04 (batch, LOOP-02):** `SubmitAnswers` is **inject-first today** (appends all answers before `MarkResumedBatch`). Fix: **claim ALL pauses, then inject ALL answers, in one tx.** A concurrent/duplicate batch serializes on the conditional update and the whole tx rolls back → exactly one answer per pause, no orphan `RoleTool` turns.
- **D-05 (pause exposure, F-030):** Move `pause.Insert` **out of `persistPause` (per-Event, immediate)** into **`flushPause` (round-end)**, and write the assistant `ask_user` tool_call turn **+ all N `paused_states` rows in one `db.WithTx`** (the tracker already accumulates `tr.pauses`). A pause becomes consumable (`ListPendingAll`/`PendingFor`) **only after** its wire-valid assistant tool-call history is durable.
- **D-06 (idempotency):** The **existing `WHERE resumed_at IS NULL` conditional update IS the idempotency key** (rows-affected → `ErrPauseNotFound`). No idempotency-key column needed. The documented "claimed-without-answer" orphan the code warns about becomes **structurally impossible** (failed tx ⇒ nothing committed ⇒ pause stays pending ⇒ user retries = the "repairable state" LOOP-03 requires).
- **D-07 (roadmap reconciliation):** Drop the ROADMAP goal's *"durable ledger state machine (migration 0025)."* Justification: the ledger family (transactional outbox / idempotency ledger) solves the **dual-write** problem (DB + external broker); F-004/029/030 are all **single-pool** writes spannable by one tx, so a ledger would add a second durability subsystem + repair worker to reconcile a state D-06 makes impossible. **No migration in this phase.** All 4 success criteria still met. Planner updates the roadmap goal text.

### Sidecar path fencing (LOOP-05 / F-005)
- **D-08:** **Reconstruct the path from `(runDir, convID, seq)` and read through Go `os.Root`; treat the DB `content_sidecar_path` column as a "did-spill" flag only.** The column is redundant with the `(conversation_id, seq)` primary key, and `turnSidecarPath` already builds the exact path deterministically. Assert an **absolute `runDir`** first, then `os.OpenRoot(runDir)` + `Root.ReadFile("conversations/<id>/<seq>.content")`.
  - Fix **BOTH** vulnerable reads: `internal/conversations/store.go` `loadTurns` **and** `store_branch.go` `loadBranchTurns`.
  - Mirrors the in-repo `read_tool_output.go` "reconstruct-don't-trust + absolute-runDir assert" fence — converges both sidecar readers on one model. NOT fence-in-place (option B still starts from the untrusted string and must hand-roll TOCTOU/symlink handling).
  - `os.Root` neutralizes a symlink swapped at the `.content` leaf. `Root.ReadFile`/`WriteFile` landed Go 1.25 (`os.OpenInRoot` since 1.24); Aura is Go **1.26.4** — API available. Pass the **trusted operator-configured `runDir`** to `OpenRoot` (it follows symlinks in the root arg itself). **Zero migration, zero backfill** (existing rows already store the reconstructable path).

### Crash-orphan sidecar reconciliation (LOOP-09 / F-040)
- **D-09:** **Age-grace reconcile of live conversation dirs vs committed rows, in the existing `orphan_scan.go`/`sweeper.go`.** For each live `conversations/<id>/` dir, keep every `<seq>.content` whose row has a non-null spill marker; remove the rest once older than an age-grace cutoff (mirror the existing `tmpTTL` constant). NOT temp+rename (option B trades a benign leak for a **malignant unreadable-history** failure mode and still needs a GC backstop).
  - **Two cautions from the code:** (1) **scope the sweep strictly to the `.content` suffix** — the dir also holds `<spillID>.result` tool-output sidecars (sessionID==conversationID) that MUST survive; (2) reuse the scan's existing `Lstat`/symlink guard. `cleanupSidecarOnTxError` already covers graceful rollback; this closes only the process-crash-before-commit window. The boot scan **and** the interval `Sweeper` both run it for free.
  - Optional orthogonal add (NOT required for F-040): fsync file + fsync dir in `writeTurnSidecar` if *power-loss* (not just process-crash) durability is ever in scope — a separate durability knob.

### Spilled-content search (LOOP-10 / F-048)
- **D-10:** **Document + assert the exclusion** (LOOP-10 explicitly blesses this). Add a comment on `maybeSpill` + the search SQL + `models.go` noting spilled `content=NULL` is excluded from trigram search, and a test asserting a `>cap` turn spills and does **not** appear in `SearchConversationTurns`. NOT a preview column / index table.
  - **Decisive rationale:** Aura's search is whole-string, length-normalized trigram `similarity()` (`content % $1`, GIN `gin_trgm_ops`). A >64 KiB body's trigram count dominates the denominator → score ~0 → never clears the 0.3 threshold, so **even repopulating `content` would not surface a large turn.** Spill is rare (single message >64 KiB ≈ 10k–16k words; verbose tool-result turns already have their own sidecar + L1-compaction mitigation). Building search infra buys ≈ nothing.
  - **Upgrade path preserved:** if telemetry ever shows frequent large *searchable* turns, a short-preview column (length-compatible with `%`) drops in cleanly at a future migration — a considered tsvector move, not a reflexive patch now.

### send_file outside-workspace (LOOP-06 / F-009)
- **D-11:** **Return a deterministic unsupported/blocked error; remove the advertised approval route.** Rewrite `outsideWorkspaceResult` (`internal/agent/tools/send_file.go`) to a terminal `errorResult` that **drops** the `ask_user`/`resume_context={"type":"send_file_outside_workspace",...}` instruction and tells the model to copy the file into the workspace / use an approved path (~5–10 LOC, 1 file). Delete the dead route. NOT the ~225-LOC approval-ledger + resume-hook (option A) — no product requirement asks for cross-workspace egress; the workspace fence (AG-019) is deliberate and the in-workspace path is a working self-correction. Matches Codex `assess_patch_safety`'s deterministic `Reject{reason}` branch (the only legitimate states are "wire a real approval" or "reject deterministically" — Aura currently advertises the first while implementing neither).

### Mechanical correctness fixes (bundled — no fork, established patterns)
- **D-12 (F-010 / LOOP-07):** `fs_write` uses `atomicWriteFile` (temp+rename) like `fs_edit`; preserve mode/permissions on overwrite; a mid-write crash never leaves a truncated target.
- **D-13 (F-031 / LOOP-08):** Pre-resolve the tool descriptor's `Mutating` flag **before** execution and copy it into the `runToolRecovering` panic-recovery result (`internal/agent/llm_agent_parallel.go`), so a mutating tool that panics **after** a side effect still arms `sideEffected` / the completion gate.
- **D-14 (F-045 / LOOP-11):** `waitWorkers`/`Stop` (`internal/runner/runner_resume.go`) use a **single lifecycle-owned done channel** so repeated `Stop` calls on a hung worker don't accumulate blocked waiter goroutines.
- **D-15 (QUAL-04):** (a) `askuser/store.go:231` (`ListRecent` `int32(limit)`) — add an int32 overflow guard on `limit`. (b) `bootChatEnvWithConfig` (`cmd/aura/chat.go:178` + `:197`) — single `Validate()` + deferred pool-close; verify no overlay-path pool leak. (Deferred to Phase 34 from `33-CONTEXT.md` D-130.)

### Claude's Discretion
- Exact `ResumeCommitter` seam name/shape and where the composition root injects the pool; whether `os.Root` is opened once per load or per read; the grace-window constant value (mirror `tmpTTL`); the precise wording of the documented search-exclusion.
- Whether to leave a code comment flagging the `skill`/`task`/`swarm_spawn` mutating-classification gap as a Phase-35 note — but **do NOT fix the classification here** (scope fence; D-01 is classification-independent by design).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (authoritative WHAT)
- `.planning/REQUIREMENTS.md` — **LOOP-01..11** + **QUAL-04** (exact requirement text + linked findings).
- `.planning/ROADMAP.md` §"Phase 34: Agent-Loop Correctness + Durable Ledger" — goal + 4 success criteria. **Update the goal text per D-07** (drop the ledger/migration clause).

### Audit findings (the contract — MUST read; `docs/audit/bug-report.md`)
- F-003 (~L39) terminal `text_response` after mutating siblings · F-004 (~L58) batch resume claim ordering · F-005 (~L77) sidecar trusts DB path · F-009 (~L159) send_file approval unwired · F-010 (~L180) fs_write non-atomic · F-029 (~L543) single resume claim+append not atomic · F-030 (~L560) pause flush failure hidden · F-031 (~L579) mutating-panic loses classification · F-040 (~L754) crash sidecars in live dirs · F-045 (~L840) worker-wait goroutine leak · F-048 (~L893) spilled content not searchable.

### Prior phase context (scope boundary + deferral)
- `.planning/phases/33-runtime-profiles-config-validation/33-CONTEXT.md` — D-130 (QUAL-04 correctness fixes deferred to Phase 34); `<deferred>` (Tool Gateway boundary — mutating-tool ledger + runtime enforcement = Phase 35).

### Code to extend / mirror (read before writing)
- **HITL:** `internal/runner/runner_resume.go` (`SubmitAnswer` claim-first + documented residual; `SubmitAnswers` inject-first bug; `applyResumeHook`; `waitWorkers`/`Stop`), `internal/runner/runner.go` (deferred `flushOnce`), `internal/runner/runner_persist.go` (`persistPause` per-Event / `flushPause` round-end — F-030), `internal/runner/interfaces.go` (`PauseStore`/`ConversationStore`), `internal/askuser/store.go` (`MarkResumed` raw `pool.Exec` → sqlc; `MarkResumedBatch`; `:231` int32), `internal/conversations/store_append.go` (`AppendTurn` `WithTx` body to extract; `cleanupSidecarOnTxError`), `internal/db/tx.go` (`WithTx(*sqlc.Queries)` seam — spans both stores), `internal/db/queries/paused_states.sql` (`MarkPausedStateResumed :exec → :execrows`).
- **Terminal exclusivity:** `internal/agent/llm_agent_dispatch.go` (`splitTerminalCall`, `runRunnableBatch`, dedup-trip `appendSyntheticToolResults`+`maybeRecover`/`finalize`, `sideEffected`), `internal/agent/llm_agent.go` (`runTerminal`, `terminalTool`), `internal/agent/llm_agent_completion.go` (gate can veto after side effects), `internal/agent/llm_agent_parallel.go` (`runToolRecovering` — F-031), `internal/agent/tools/spec.go` (`Mutating` hint).
- **Sidecar:** `internal/conversations/store.go` (`loadTurns` vuln read; `sidecarDir`), `store_branch.go` (`loadBranchTurns` vuln read), `store_helpers.go` (`maybeSpill`, `turnSidecarPath`, `writeTurnSidecar`, `validateID`), `orphan_scan.go` (`scanConversationOrphans`, `tmpTTL`, symlink guard), `sweeper.go`, `internal/agent/tools/read_tool_output.go` (reconstruct-don't-trust fence to mirror), `internal/agent/tools/result.go` (`sidecarPath`, strict `validateID`), `internal/db/migrations/0005_conversations.up.sql` (`content_sidecar_path`, PK `(conversation_id, seq)`).
- **Spill search:** `internal/conversations/store_helpers.go` (`maybeSpill`, 64 KiB cap), `internal/db/queries/conversation_turns.sql` (locked trigram query — cross-slice, Telegram `/search` reuses byte-for-byte), `internal/conversations/store.go` (`SearchConversationTurns`), `internal/db/migrations/0006_conversation_turns_fts.up.sql`, `internal/config/config_knobs.go` (`AURA_CONVERSATION_TURN_CAP_BYTES`).
- **send_file / fs / QUAL:** `internal/agent/tools/send_file.go` (`outsideWorkspaceResult`), `cmd/aura/serve_adapters.go` (`chainResumeHooks`, `newShellResumeHook`/`newSkillResumeHook` pattern), `internal/agent/tools/fs_write.go` + `fs.go` (`atomicWriteFile`) + `fs_edit.go`, `cmd/aura/chat.go` (`bootChatEnvWithConfig` double-`Validate` :178/:197).

### External precedents (D:/tmp — inspected)
- `D:/tmp/adk-go-study/session/database/service.go` — `applyEvent`: single-tx event-append + state-update, **no ledger** (precedent for D-02). `server/adka2a/v2/input_required.go` — resume validates a response for every pending call-ID + dedups (mirrors D-05/F-030).
- `D:/tmp/codex/codex-rs/core/src/safety.rs` — `assess_patch_safety`: out-of-project write → `AskUser` | deterministic `Reject`, never an unwireable route (precedent for D-11). **Note:** `D:/tmp/codex` + `D:/tmp/nanobot` top-level are bare `.git` shells; `codex-rs` subtree + `adk-go-study` are the usable working trees.

### Web sources (2026 corroboration)
- Go `os.Root`: https://go.dev/blog/osroot · https://pkg.go.dev/os (Root.ReadFile go1.25, os.OpenInRoot go1.24) · golang/go#71806 · golang/go#73126
- HITL/durable resume: https://docs.langchain.com/oss/python/langgraph/interrupts · transactional-outbox (AWS Prescriptive Guidance; event-driven.io) · https://brandur.org/postgres-atomicity · https://pkg.go.dev/github.com/jackc/pgx/v5
- Terminal semantics: https://docs.anthropic.com/en/docs/build-with-claude/tool-use · https://reference.langchain.com/python/langgraph.prebuilt/tool_node/tools_condition · https://developers.openai.com/codex/agent-approvals-security
- pg_trgm length-normalization: https://www.postgresql.org/docs/current/pgtrgm.html

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`internal/db/tx.go` `WithTx(ctx, pool, fn func(*sqlc.Queries) error)`** — the seam that makes D-02 a small refactor, not a re-architecture; already used by both `conversations` and `askuser` stores.
- **`internal/agent/tools/read_tool_output.go`** — the reconstruct-from-ctx-ids + absolute-runDir fence to mirror for D-08 (conversation sidecars).
- **`internal/agent/tools/fs.go` `atomicWriteFile`** — reuse verbatim for D-12 (`fs_edit` already uses it).
- **`llm_agent_dispatch.go` dedup-trip path** (`appendSyntheticToolResults` + `maybeRecover`/`finalize`) — reuse for D-01 (terminal rejection), incl. the bounded replan counter.
- **`orphan_scan.go` + `sweeper.go`** (boot scan + interval sweep, `tmpTTL`, `Lstat` guard) — the natural home for D-09 age-grace GC.
- **`cleanupSidecarOnTxError`** — already handles graceful rollback; D-09 only adds the crash-before-commit case.

### Established Patterns
- **`db.WithTx` closure + sqlc `Queries`** = the one durability pattern (no second mechanism — reject ledger).
- **Reconstruct-don't-trust** for sidecar paths (proven in tools; extend to conversations).
- **`WHERE resumed_at IS NULL` conditional-update = idempotency key** (claim-then-act).
- **Tx-accepting `…Tx(q,…)` variant + thin `WithTx` wrapper** = the DRY shape for exposing store ops to a cross-store tx.
- CLAUDE.md gates apply: `go vet/build/test -race` per touched package, coverage floor ≥85% owned-surface, mutation ≥70% on touched files, table tests + `goleak` + realistic fixtures, no-skip-as-green.

### Integration Points
- New **`ResumeCommitter`** injected at the composition root (serve + cron + telegram — wherever the runner is built); the runner calls it instead of split `MarkResumed → AppendTurn`.
- `flushPause` becomes the atomic pause-persistence site (D-05); pause-visibility readers (`ListPendingAll`, `PendingFor`) now only ever see wire-valid pauses.
- sqlc regeneration needed for `MarkPausedStateResumed :execrows` (D-03).
- `os.Root` is the **first concrete use** in Aura's Go code (Go 1.26.4) — squarely on documented intent.

</code_context>

<specifics>
## Specific Ideas

- User explicitly asked to **research 2026 industrial patterns online + inspect the curated `D:/tmp` repos** before deciding — so every decision is research-backed and corroborated against the live code, not assumed.
- All four gray areas **converged on the minimal industrial form** (memory `feedback_no_atomic_bombs_minimal_industrial_shape`): the standout is D-02/D-07 — **replacing** the roadmap's "durable ledger" with a single transaction because the ledger solves a dual-write problem Aura doesn't have here.
- `D:/tmp/adk-go-study` is the load-bearing Go precedent (its `Agent` interface is the one Aura's shape was modeled on); `codex-rs/core/src/safety.rs` is the reference for deterministic reject-vs-approve (D-11).

</specifics>

<deferred>
## Deferred Ideas

These surfaced from the research/audit's fuller vision but belong to LATER phases — do NOT pull into Phase 34:

- **Outbox / idempotency ledger for the EXTERNAL resume-relay hook** (`applyResumeHook` → swarm relay of the resume to a child thread, potentially over MCP) — the one genuine dual-write. Deferred; only if that relay is later shown to need exactly-once external delivery does a **targeted `resume_relay_outbox`** (row written inside D-02's tx, drained by one bounded worker) earn its place — NOT a general resume-state ledger.
- **Complete the tool `Mutating` classification** (`skill`/`task`/`swarm_spawn` are unflagged + action-multiplexed) + an **action-aware** mutating model → **Phase 35 ToolGateway**. Required *before* "allow read-only siblings beside a terminal" (D-01 option B) could ever be safe.
- **`send_file` cross-workspace egress approval subsystem** (~225-LOC ledger + resume hook across all composition roots) → only if a real egress requirement lands; option A is the known, proven pattern to add *then*.
- **Short-preview / tsvector spilled-content search** → future phase, only if spill telemetry shows frequent large *searchable* turns; considered tsvector migration, not a reflexive patch.
- **fsync file + dir for power-loss (not just process-crash) durability** in `writeTurnSidecar` → separate durability knob, orthogonal to F-040.
- **Mutating-tool durable ledger reservation** (F-011 / GATE-03/04) + **per-profile runtime enforcement** of tool caps / path fences / sandbox routing / egress → **Phase 35** (already deferred in `33-CONTEXT.md`).

### Reviewed Todos (not folded)
None — `todo.match-phase 34` returned 0 matches.

[No scope-creep ideas surfaced — discussion stayed within phase scope.]

</deferred>

---

*Phase: 34-agent-loop-correctness-durable-ledger*
*Context gathered: 2026-07-01*
