---
phase: 04-hitl-identity-conversations
verified: 2026-05-31T00:00:00Z
status: passed
score: 13/13 must-haves verified; 4/4 live UAT items passed
overrides_applied: 0
uat_executed: 2026-05-31 (live deepseek/deepseek-v4-flash:exacto + Postgres stack); all 4 items PASS — see 04-HUMAN-UAT.md. One bug found+fixed during UAT: auto-title worker never fired (iterator not drained), commit 12506a8e.
gaps:
  - truth: "ask_user.Execute returns the ErrAwaitingUserInput sentinel, never a ToolResult — AND the tool is registered in the production registry so the LLM can actually trigger a pause"
    status: resolved
    reason: "RESOLVED in commit 6a808839 — added reg.Register(tools.AskUser{}) to buildRegistry() in cmd/aura/main.go. Proven at the artifact level: `aura tools` now renders `[active] ask_user` in the live manifest. Regression tests added (cmd/aura/registry_test.go: TestBuildRegistry_RegistersAskUser asserts ask_user is in the production registry; TestBuildRegistry_CoreToolsPresent locks the rest of the always-on manifest) so the omission cannot silently recur. The ROADMAP SC-1 path is now reachable from production (pending the live-LLM human UAT below)."
    artifacts:
      - path: "cmd/aura/main.go"
        issue: "RESOLVED — buildRegistry() now registers tools.AskUser{}"
human_verification:
  - test: "Trigger a real pause/resume in the live REPL"
    expected: "Run aura chat new; prompt a model turn that calls ask_user(kind=approval,question='Proceed?'); observe REPL renders [y/N] prompt; answer 'y'; observe the loop resumes with the answer injected and the assistant continues"
    why_human: "Requires a live REPL connected to a live LLM (OPENROUTER_API_KEY + stack up). Cannot be verified by grep or unit test."
  - test: "3 simultaneous ask_user calls FIFO order"
    expected: "A model turn that calls ask_user three times with distinct priorities produces 3 rows in aura.paused_states; the REPL prompts them in priority DESC order; answering all three resumes the loop with all three answers"
    why_human: "Multi-pause FIFO requires a live model that actually batches 3 ask_user calls in one turn — no unit test can drive a real LLM to do this."
  - test: "aura chat list shows auto-generated titles and cumulative USD after a multi-turn conversation"
    expected: "After a 4-turn conversation the title column shows a non-empty LLM-generated title; total_cost_usd > 0"
    why_human: "Requires a live LLM call for the auto-title worker; simulated in unit tests but the real title-generation path needs a live API key."
  - test: "aura chat search 'specific phrase' returns matching excerpts ordered by similarity"
    expected: "Insert a turn with known content; run aura chat search 'phrase'; observe the matching row first in conv_id|seq|similarity|excerpt format"
    why_human: "Requires a live Postgres stack with the pg_trgm GIN index in place; the db_integration tier verifies the query but not the CLI output format."
---

# Phase 4: HITL + Identity + Conversations — Verification Report

**Phase Goal:** Tight cluster of agent-loop primitives. `ask_user` tool with sentinel `ErrAwaitingUserInput` + FIFO multi-pause persisted in `aura.paused_states`. Identity minimal + `capability_grants` scaffolding (single-user `local` default with wildcard `*`). Conversation persistence multi-thread Claude.ai-style with microcompact L1 + budget L2 + auto-title + per-conversation token+USD aggregation. FTS via `pg_trgm` GIN index on `conversation_turns.content` + `aura chat search` CLI.
**Verified:** 2026-05-31T00:00:00Z
**Status:** passed (13/13 automated must-haves + 4/4 live UAT items)
**Re-verification:** Truth #2 re-verified after fix 6a808839; 4 live-LLM UAT items executed against deepseek/deepseek-v4-flash:exacto — all PASS (see 04-HUMAN-UAT.md). Auto-title bug found+fixed (12506a8e).

---

## Goal Achievement

### Observable Truths

| #  | Truth | Status | Evidence |
|----|-------|--------|----------|
| 1  | ask_user.Execute returns ErrAwaitingUserInput sentinel, never a ToolResult; args validation rejects empty question, exactly-1 option, non-distinct labels, priority outside 0-100 | VERIFIED | internal/agent/tools/ask_user.go:114-143; validateOptions():148-168; test coverage in ask_user_test.go |
| 2  | ask_user is registered in the production registry so the LLM can trigger a pause in a live aura chat session | VERIFIED (fixed 6a808839) | buildRegistry() in cmd/aura/main.go:69 now registers tools.AskUser{}; `aura tools` renders `[active] ask_user`; regression tests cmd/aura/registry_test.go assert it. |
| 3  | The pause-detection seam catches the sentinel BEFORE the generic error fallback, suppresses RoleTool, rewrites assistant msg to ask_user-only tool_calls, emits Actions.AwaitingInput Event (never the iter error slot) | VERIFIED | internal/agent/llm_agent_pause.go:34-131; pauseCalls/detectPause/emitPauses/pauseEvent all present and wired; errors.As sentinel check at line 78 |
| 4  | Intra-turn exclusivity: ask_user batched with siblings rewrites the assistant msg to ask_user-only tool_calls; siblings dropped | VERIFIED | llm_agent_pause.go pauseToolCalls():59-65; pauseCalls():34-49 only extracts ask_user calls; runner_persist.go flushPause():120-141 persists the single combined assistant turn (CR-02 fix) |
| 5  | askuser.Store ListPending returns rows in priority DESC, created_at ASC, token ASC order and survives a fresh Store instance (crash recovery) | VERIFIED | internal/askuser/store.go:144-158; comment at line 141-143 documents the mandatory token tiebreaker; db/queries/paused_states.sql ORDER BY matches |
| 6  | HasCapability('local','any_tool') returns true on a fresh boot via the '*' wildcard; grant/revoke of '*' is rejected; grant/revoke of ordinary caps is idempotent | VERIFIED | internal/identity/store.go:111-121 HasCapability; 128-140 GrantCapability (idempotent via isUniqueViolation); 160-165 validateGrantInput rejects '*' before DB; 0004 migration seeds the local/'*' row |
| 7  | AppendTurn writes INSERT turn + UPDATE conversation aggregates in one db.WithTx tx; a mid-tx failure leaves no partial turn (SC-2) | VERIFIED | internal/conversations/store.go:254-300; db.WithTx wraps both INSERT and UPDATE at line 288; SC-2 integration test exists in store_test.go |
| 8  | LoadHistory reconstructs []llm.Message from conversation_turns ORDER BY seq, byte-identical across two calls | VERIFIED | internal/conversations/store.go:307-321 LoadHistory; loadTurns():325-348 rehydrates sidecar content deterministically; no nondeterministic source |
| 9  | content > AURA_CONVERSATION_TURN_CAP_BYTES spills to a sidecar file with content=NULL + content_sidecar_path set | VERIFIED | internal/conversations/store.go:259 maybeSpill; Config.TurnCapBytes defaults to 65536 at New():73-75; sidecarDir() path-traversal guard at 414-419 |
| 10 | L1 microcompact rewrites only role='tool' turns older than evict window to a read_tool_output pointer, NEVER seq=1; SC-1: L1-alone writes zero context_rot_events rows | VERIFIED | internal/conversations/context.go applyL1():146-165; seq==1 guard at line 156; SC-1 path returns at line 122-124 before L2.5 (no rot event); TestLoadManagedHistory_SC1_NoRotEventOnL1Alone exists |
| 11 | L2.5 hard buffer drops the oldest user/assistant pair preserving len(history)%2==0 and writes a context_rot_events row | VERIFIED | context.go dropOldestPairs():181-201; insertContextRotEvent():66-81; rotActionHardDropPairs constant; integration test TestLoadManagedHistory_L2_5_WritesRotEvent |
| 12 | auto-title fires after seq>=3 via WithoutCancel+WithTimeout, WaitGroup-tracked, goleak-clean; LLM failure leaves title NULL without crashing | VERIFIED | internal/runner/runner_resume.go:18-45 maybeAutoTitle; context.WithoutCancel at line 36; r.wg.Add(1)/defer r.wg.Done(); Stop():203-216 joins wg via waitWorkers(); WR-03 fix copies history snapshot |
| 13 | Resume = fresh agent.Run over rehydrated history (SC-4): the answer is injected as RoleTool{ToolCallID:<original>}; no duplicate ask_user tool_call; no silent LLM re-run | VERIFIED | runner_resume.go SubmitAnswer():68-84 injectAnswer path; cancel path fixed (CR-01: cancelConversation injects RoleTool answers before auto-resolve); CR-02 fix: flushPause writes single combined assistant turn; regression tests in runner_resume_fixes_test.go |

**Score:** 13/13 truths verified (truth #2 resolved in fix 6a808839 — ask_user now registered in the production manifest)

---

## Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/agent/tools/ask_user.go` | Non-deferred ask_user + ErrAwaitingUserInput sentinel (pure types, no DB) | VERIFIED | 169 LOC; Deferred:false; imports no DB/sqlc |
| `internal/agent/llm_agent_pause.go` | Pause-detection seam: catch sentinel, suppress RoleTool, rewrite assistant msg, emit Actions.AwaitingInput | VERIFIED | 131 LOC; errors.As(err, &pause) at line 78 |
| `internal/askuser/store.go` | Store over paused_states: Insert/GetByToken/ListPending/MarkResumed/MarkResumedBatch/Cleanup/AutoResolve | VERIFIED | 341 LOC; all methods present; no tools import |
| `internal/identity/store.go` | identity.Store; HasCapability wildcard-or-exact; grant/revoke with SQLSTATE classification | VERIFIED | 186 LOC; capNameRe compiled once; SQLSTATE via errors.As |
| `internal/conversations/store.go` | conversations.Store: Create/LoadHistory/AppendTurn(WithTx)/List/Search/Delete/SetTitleIfNull/status | VERIFIED | 419 LOC; all methods present |
| `internal/conversations/context.go` | L1/L2/L2.5 deterministic context ladder + cached tiktoken-go encoder | VERIFIED | 229 LOC; applyContextLadder; encoder() singleton via sync.Once |
| `internal/conversations/orphan_scan.go` | ScanOrphans with symlink guard + tmp TTL + audit-only size WARN | VERIFIED | 198 LOC; Lstat guard at line 78-88; warnIfOversized never purges |
| `internal/conversations/title.go` | generateTitle best-effort body (WaitGroup owned by Runner) | VERIFIED | 108 LOC; GenerateTitle exported wrapper; no wg/ctx here |
| `internal/conversations/tiktoken.go` | Offline cl100k_base via embedded vocab file; cached encoder | VERIFIED | 95 LOC; //go:embed cl100k/cl100k_base.tiktoken; sync.Once encoder |
| `internal/runner/runner.go` | Runner orchestrator: Turn/SubmitAnswer(s)/Stop; sole paused_states writer; auto-title WaitGroup owner | VERIFIED | Turn at line 148; wg sync.WaitGroup at line 73 |
| `internal/runner/interfaces.go` | Consumer-side narrow interfaces ConversationStore/PauseStore/IdentityStore (D-A2-02) | VERIFIED | All three interfaces declared; concrete Stores satisfy implicitly |
| `internal/runner/runner_persist.go` | Event-sourced persistence seam; flushPause for CR-02 combined assistant turn | VERIFIED | persistEvent:43-54; flushPause:120-141; CR-02 fix present |
| `internal/runner/runner_resume.go` | SubmitAnswer/SubmitAnswers/Stop/maybeAutoTitle; CR-01 cancel fix injects RoleTool | VERIFIED | cancelConversation:154-168 injects RoleTool per pending before auto-resolve |
| `internal/db/tx.go` | Shared WithTx(ctx, pool, fn) atomic-write helper | VERIFIED | 39 LOC; panic-safe defer; re-panics on recover |
| `internal/db/migrations/0003_paused_states.up.sql` | paused_states with text conversation_id (no FK), proxied_* columns | VERIFIED | conversation_id text; proxied_from_child_id uuid; partial index |
| `internal/db/migrations/0004_identity.up.sql` | identities + capability_grants; seed local/'*' ON CONFLICT DO NOTHING | VERIFIED | Both tables; seed at lines 27-32; explicit DML grants |
| `internal/db/migrations/0005_conversations.up.sql` | conversations + conversation_turns + context_rot_events + paused_states FK alter + resumed_answer | VERIFIED | All tables present; ALTER at lines 52-58; pg_trgm extension at line 73; NO conversation_spillover table |
| `internal/db/migrations/0006_conversation_turns_fts.up.sql` | Single-statement CREATE INDEX CONCURRENTLY (CONCURRENTLY isolation) | VERIFIED | Exactly one statement; IF NOT EXISTS for idempotency |
| `internal/db/sqlc/querier.go` | Regenerated Querier interface with full Phase-4 query surface | VERIFIED | SearchConversationTurns at line 44; all paused_states/identity/conversations methods present |
| `internal/llm/config.go` | ContextWindow + MaxOutputTokens fields for L2 budget | VERIFIED | ContextWindow at line 73; MaxOutputTokens at line 74; env overrides AURA_MODEL_CONTEXT_WINDOW/AURA_MODEL_MAX_OUTPUT_TOKENS at lines 50-51 |
| `internal/config/config.go` | Four Phase-4 AURA_* conversation env vars with defaults | VERIFIED | Lines 112-115: ConversationTurnCapBytes=65536, ContextToolEvictAfterTurns=10, HistoryHardCapTurns=50, RunDirWarnThresholdBytes=1073741824 |
| `cmd/aura/chat.go` | aura chat {list|new|resume|archive|unarchive|delete|rename|search} switch + composition root bootChat + inline pause rendering | VERIFIED | switch at lines 69-89; bootChat:96-148 (ScanOrphans, InitEncoder, Runner construction); Runner.Turn driven in chatLoop |
| `cmd/aura/paused_states.go` | aura paused-states {list|purge --before <ISO> --confirm} CLI (switch) | VERIFIED | runPausedStates():27-56; --confirm guard at line 91 |
| `cmd/aura/main.go` | case "identity" + case "paused-states" + case "chat" wired | VERIFIED | Lines 38-46: identity, paused-states, chat all present |
| `scripts/microcompact_smoke.sh` | L1 eviction + SC-1 zero-rot + L2.5 hard_drop_pairs CI contract; no-skip-as-green | VERIFIED | Lines 43-90; asserts RUN + PASS for both test names; grep contract on context.go |

---

## Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `internal/agent/llm_agent_pause.go` | `tools.ErrAwaitingUserInput` | `errors.As` before generic err!=nil fallback | VERIFIED | detectPause():71-82; errors.As(err, &pause) at line 78 |
| `internal/agent/event.go` | `Actions.AwaitingInput` | New pointer field round-tripped byte-identically | VERIFIED | Actions struct line 67-72; AwaitingInput *AwaitingInput json:"awaiting_input,omitempty" |
| `internal/conversations/store.go` | `internal/db.WithTx` | AppendTurn wraps WithTx for atomic INSERT+UPDATE | VERIFIED | store.go line 288: db.WithTx(ctx, s.pool, func(q *sqlc.Queries) error {...}) |
| `internal/conversations/context.go` | `internal/llm.Config ContextWindow/MaxOutputTokens` | L2 hard_cap = ContextWindow - max(MaxOutputTokens,20000) - 13000 | VERIFIED | context.go hardCap():47-57; uses ContextConfig.ContextWindow and MaxOutputTokens |
| `internal/runner/runner.go` | `askuser.Store via PauseStore interface` | Observes Actions.AwaitingInput Event → InsertPausedState (sole writer) | VERIFIED | runner_persist.go persistPause():87-112; r.pause.Insert call; turnTracker sole-writer pattern |
| `cmd/aura/chat.go` | `runner.Runner` | REPL drives Turn; composition root constructs Stores + Runner | VERIFIED | bootChat():96-148; runner.New() at line 136; chatLoop drives Runner.Turn |
| `cmd/aura/main.go` | `runPausedStates` | top-level switch case paused-states | VERIFIED | main.go line 44: case "paused-states": runPausedStates(os.Args[2:]) |
| `internal/db/migrations/0005_conversations.up.sql` | `aura.paused_states` | ALTER conversation_id to uuid + FK conversations(id) ON DELETE CASCADE | VERIFIED | 0005 lines 52-58: ALTER TYPE uuid + ADD CONSTRAINT FK ON DELETE CASCADE |
| `internal/db/queries/conversation_turns.sql` (via sqlc) | `pg_trgm similarity` | content % $1 ORDER BY similarity(content,$1) DESC | VERIFIED | querier.go SearchConversationTurns at line 42-44; LOCKED comment preserved |

---

## Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|--------------|--------|--------------------|--------|
| `internal/conversations/store.go SearchConversationTurns` | rows from pg_trgm GIN index | sqlc.SearchConversationTurns bound query `content % $1 ORDER BY similarity DESC` | Yes — parameterized query against real DB | FLOWING |
| `internal/conversations/store.go LoadHistory` | []llm.Message reconstructed from DB | ListTurnsBySeq ORDER BY seq + sidecar rehydration | Yes — DB query + disk read | FLOWING |
| `internal/conversations/store.go AppendTurn aggregates` | total_cost_usd | numericFromFloat(p.CostUSD) folded in SQL UPDATE | Yes — accumulates real per-turn cost | FLOWING |
| `internal/runner/runner_resume.go maybeAutoTitle` | title | GenerateTitle via live llm.Client.Stream | Yes when live client; best-effort (NULL on failure) | FLOWING |
| `internal/conversations/context.go applyContextLadder` | []llm.Message (ladder output) | loadTurns + tiktoken encoder (offline) | Yes — deterministic from DB rows | FLOWING |
| `cmd/aura/chat.go chatList` | []Conversation | conv.List() → sqlc.ListConversations | Yes — DB query | FLOWING |

---

## Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| ask_user tool syntax validity | `bash -c "grep -q 'Deferred.*false' D:/Aura/internal/agent/tools/ask_user.go"` | exit 0 | PASS |
| ErrAwaitingUserInput type exists | `bash -c "grep -q 'type ErrAwaitingUserInput struct' D:/Aura/internal/agent/tools/ask_user.go"` | exit 0 | PASS |
| 0006 migration is single-statement | file contains exactly `CREATE INDEX CONCURRENTLY IF NOT EXISTS` as sole DDL | verified by read | PASS |
| No timed_out in codebase | `grep -r timed_out internal/ cmd/` | no matches | PASS |
| No conversation_spillover table | `grep -r conversation_spillover internal/db/migrations/` | no match (only a comment in 0005 confirming absence) | PASS |
| No cobra dependency | `grep cobra go.mod` | no match | PASS |
| ask_user missing from production registry | `grep -n AskUser cmd/aura/main.go` | no match | FAIL — BLOCKER |

---

## Probe Execution

| Probe | Command | Result | Status |
|-------|---------|--------|--------|
| `scripts/microcompact_smoke.sh` | `bash -n scripts/microcompact_smoke.sh` (syntax check) | The script contains both `context_rot_events` and `read_tool_output` grep assertions; no-skip-as-green guards present | PASS (syntax + contract assertions verified by read; live run deferred to human gate — requires WSL stack up) |

Note: The live execution of `microcompact_smoke.sh` requires the Postgres stack (WSL). The orchestrator's validation evidence confirms `TestLoadManagedHistory_SC1_NoRotEventOnL1Alone` and `TestLoadManagedHistory_L2_5_WritesRotEvent` passed in the db_integration tier.

---

## Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| CORE-02 | 04-01, 04-03, 04-05 | ask_user pause/resume FIFO multi-pause + paused_states persistence + sentinel | PARTIAL | All code artifacts present and tested. Gap: ask_user not in production registry — the LLM cannot trigger a pause in a live session. |
| CORE-03 | 04-01, 04-02 | Identity minimal + capability_grants scaffolding + HasCapability wildcard | SATISFIED | identity.Store complete; CLI wired; seeded migration verified; HasCapability wildcard semantics present |
| CORE-04 | 04-01, 04-04, 04-05 | Conversation persistence + L1/L2/L2.5 context management + auto-title + token/USD agg | SATISFIED | All artifacts present; SC-2 atomicity; byte-identical LoadHistory; tiktoken offline; orphan scan; sidecar spill |
| CORE-05 | 04-01, 04-04, 04-05 | pg_trgm FTS + aura chat search CLI | SATISFIED | 0006 migration correct; SearchConversationTurns locked query; chat search CLI wired |

**Orphaned requirements:** None — all 4 IDs declared in plan frontmatter, all present in REQUIREMENTS.md as Phase 4.

---

## Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `cmd/aura/main.go` | 63-70 | `buildRegistry()` missing `reg.Register(tools.AskUser{})` | BLOCKER | Production LLM never sees ask_user in the tool manifest; REPL cannot trigger pauses with a real LLM; ROADMAP SC-1 and SC-2 cannot be demonstrated live |
| `internal/conversations/title.go` | 80, 104 | Byte-slices UTF-8 string on truncation (WR-02, from code review — non-blocking per REVIEW.md) | WARNING | Multibyte rune straddling perTurnCap/titleMaxChars produces invalid UTF-8 in stored title |
| `internal/runner/runner_persist.go` | 34-45 | budget-trip Escalate Event persists no assistant turn (WR-04 — non-blocking per REVIEW.md) | WARNING | User turn left unanswered in transcript on budget exhaustion; cost not aggregated |
| `internal/conversations/store_helpers.go` | 151-161 | `numericFromFloat` int64 mantissa overflow for absurd/infinite costs (WR-05 — non-blocking per REVIEW.md) | WARNING | Garbage cost delta folds silently into total_cost_usd aggregate |

**Debt markers:** No `TBD`, `FIXME`, or `XXX` found in modified source files.

---

## Human Verification Required

The following items need a live environment (Postgres stack up + OPENROUTER_API_KEY) to verify:

### 1. Live pause/resume REPL flow (ROADMAP SC-1 — currently BLOCKED by registry gap)

**Test:** Fix the registry gap (register `ask_user` in `buildRegistry()`), then run `aura chat new`; prompt a model turn that calls `ask_user(kind=approval, question='Proceed?')`; type 'y' at the `[y/N]` prompt.
**Expected:** The loop pauses, one row appears in `aura.paused_states`, answering 'y' resumes the loop with the answer injected as `RoleTool`, the assistant continues.
**Why human:** Requires a live LLM + stack; the pause path is unit-tested with FakeClient but the actual trigger from a real model needs interactive confirmation.

### 2. 3 simultaneous ask_user calls FIFO order (ROADMAP SC-2)

**Test:** After registry fix, craft a prompt that causes the model to call `ask_user` three times in one assistant turn with distinct priorities; observe REPL renders them in priority DESC order.
**Expected:** 3 `aura.paused_states` rows; prompts appear in FIFO order; answering all 3 resumes the loop with all 3 answers.
**Why human:** Requires a live model that actually batches 3 ask_user calls — unit tests use FakeClient but cannot demonstrate real model behaviour.

### 3. aura chat list auto-title + cumulative USD

**Test:** After a 4+ turn conversation with a live LLM, run `aura chat list`.
**Expected:** The conversation row shows a non-empty title (generated by the auto-title worker); total_cost_usd > 0.
**Why human:** Auto-title calls the live LLM; displayed cost correctness requires a real provider response with usage figures.

### 4. aura chat search returns similarity-ordered excerpts

**Test:** After inserting turns with known content, run `aura chat search "phrase"`.
**Expected:** Output shows `conv_id|seq|similarity|excerpt` ordered similarity DESC; the excerpt window is approximately ±60 chars around the match.
**Why human:** Requires the pg_trgm GIN index to be populated with real data (stack up + prior turns written).

---

## Gaps Summary

**One blocker** preventing full goal achievement:

**`ask_user` not registered in production registry (`cmd/aura/main.go:buildRegistry`).** The entire HITL pause primitive — `ErrAwaitingUserInput`, `llm_agent_pause.go`, `askuser.Store`, the Runner's sole-writer pattern, the resume-as-fresh-Run SC-4 path — is built correctly and fully tested. The test harness registers `tools.AskUser{}` explicitly. But `buildRegistry()`, which is called by `bootChat()` and used by the production REPL, does not register the tool. A live LLM running in `aura chat` never sees `ask_user` in the manifest, so it cannot call it, so no pause Event is ever emitted, so no `paused_states` row is ever written. ROADMAP success criteria 1 and 2 ("operator triggers ask_user") cannot be satisfied in production.

**Fix required:** One line added to `buildRegistry()` in `cmd/aura/main.go`:
```go
reg.Register(tools.AskUser{})
```

All other 12 must-haves are VERIFIED. The three non-blocking warnings (WR-02/04/05) are known items from the code review, accepted as non-blocking by `04-REVIEW.md status: resolved`.

---

_Verified: 2026-05-31T00:00:00Z_
_Verifier: Claude (gsd-verifier)_
