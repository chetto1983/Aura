---
status: passed
phase: 04-hitl-identity-conversations
source: [04-VERIFICATION.md]
started: 2026-05-31T00:00:00Z
updated: 2026-05-31T06:00:00Z
executed_by: orchestrator (live model deepseek/deepseek-v4-flash:exacto + Postgres stack)
---

## Current Test

These four items are now AUTONOMOUS, self-asserting `live_e2e` Go tests — no human-in-the-loop.
They drive the REAL `runner.Runner` over the REAL `openai_compat` client (DeepSeek-V4 Flash via
OpenRouter) and the REAL `conversations`/`askuser`/`cachemetrics`/`identity` Stores on a live
Postgres, asserting DB ground truth (not reply prose) for every scenario.

- Test files: `internal/runner/live_e2e_test.go` (harness) + `internal/runner/live_e2e_scenarios_test.go` (the 4 scenarios)
- Build tag: `live_e2e` (PAID gate — skips locally when `OPENROUTER_API_KEY` is unset; the Postgres
  DSN env follows the no-skip-as-green helper: skips locally, `t.Fatal` under `$CI`)
- Tests: `TestLiveE2E_PauseResume`, `TestLiveE2E_ThreeSimultaneous`, `TestLiveE2E_AutoTitleAndCost`,
  `TestLiveE2E_SearchSimilarity`
- Reproduce:
  ```
  cp /d/Aura/.env ./.env
  set -a; . ./.env; set +a
  export AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
  export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
  go test -tags live_e2e -run TestLiveE2E -timeout 600s -v ./internal/runner/
  ```

Each test cleans up its own conversation (ON DELETE CASCADE) and joins the auto-title worker via
`Runner.Stop` before returning, so the package's `goleak.VerifyTestMain` stays clean and no garbage
is left in the shared `aura.*` tables.

## Tests

### 1. Live pause/resume in the REPL
expected: ask_user(kind=approval) pauses, [y/N] renders, answer resumes with the answer injected as RoleTool{ToolCallID:<original>}; the assistant continues (SC-4).
result: PASS — autonomous `TestLiveE2E_PauseResume`. The live model called ask_user(approval, irreversible-delete inducer); DB ground truth asserted programmatically: a `paused_states` row is created with a non-empty `tool_call_id` (e.g. `call_00_w686hCFs...`); after a programmatic `SubmitAnswer(accept,"yes")` the row's `resumed_answer` reads `{"action":"accept","content":"yes"}` and `resumed_at` is set; the rehydrated `LoadHistory` is wire-valid (`assertWireValid`) with the RoleTool answer keyed to the ORIGINAL `tool_call_id`; the post-resume `Turn(nil)` drives a GENUINE fresh LLM round (real `prompt_tokens`, e.g. 1173–1247; a new persisted assistant turn). The resume loop tolerates a model that re-asks (bounded resolve-and-redrive) — a re-pause is still a genuine fresh round. Observed reply ≈720–785 B.

### 2. Three simultaneous ask_user calls — FIFO order
expected: 3 ask_user calls in one turn → 3 paused_states rows, prompted in priority DESC order, all answered, wire-valid rehydration (CR-02).
result: PASS — autonomous `TestLiveE2E_ThreeSimultaneous`. The live model emitted 3 ask_user calls in ONE turn (Deploy prio=90 / region prio=50 / email prio=10). DB ground truth: `PendingFor` returns them in priority DESC `[90 50 10]`; `SubmitAnswers` resolves all 3 and each `paused_states.resumed_answer` matches its `{action,content}` (accept "yes-deploy" / accept "eu-west-1" / decline → "user declined to answer"); `LoadHistory` is wire-valid and `countAssistantPauseTurns` proves exactly ONE assistant turn carrying all 3 `tool_calls` immediately followed by 3 keyed tool answers — the CR-02 fix re-proven against a real model.

### 3. aura chat list shows auto-generated title + cumulative USD
expected: after a multi-turn conversation the title is a non-empty LLM-generated title; total_cost_usd > 0.
result: PASS — autonomous `TestLiveE2E_AutoTitleAndCost`. A 3-turn live conversation (France/Japan capitals) yields a non-empty LLM auto-title via the REAL `Runner` worker (observed e.g. "Capitals of France and Japan", "Capital of France and Japan", "Capital cities of France Japan" — model-dependent wording, asserted non-empty + `TitleSet`); `conversations.Get` reports cumulative `total_cost_usd` ≈ $0.0003 (> 0) and `total_input_tokens` ≈ 3100. Cross-checked: the conversation aggregate equals `SUM(cost_usd)` over the 3 append-only `aura.cache_metrics` rows. The test joins the worker via `Stop` and gives it a bounded number of re-fire attempts (the worker is best-effort; a title that never lands is surfaced as a real signal). The earlier REPL drain bug (commit 12506a8e) stays fixed.

### 4. aura chat search similarity-ordered excerpts
expected: search returns matching rows first, ordered by similarity DESC, in conv_id|seq|similarity|excerpt format.
result: PASS — autonomous `TestLiveE2E_SearchSimilarity`. A live multi-turn conversation persists term-dominant user turns; `conversations.SearchConversationTurns("photosynthesis", 50)` (the LOCKED pg_trgm `content % $1 ORDER BY similarity(content,$1) DESC` query) returns 3 hits for this conversation, ordered by similarity DESC, every hit containing the term, with the most term-dominant turn (`"photosynthesis photosynthesis"`, seq=5) ranking first at similarity 1.0000. (Confirms the prior note: a short term against sentence-length content scores below the 0.3 trigram threshold — correct pg_trgm behavior; the test uses term-dominant content so the property is observable. The search target is the persisted USER turn, so a transient provider hiccup on the assistant round is logged, not fatal.)

## Summary

total: 4
passed: 4
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

None blocking. The one-time live UAT found a real bug (auto-title worker never fired — iterator not
drained), fixed in commit 12506a8e with a regression test (cmd/aura/chat_render_drain_test.go).

Findings from converting the UAT to autonomous live tests:
- Cumulative `total_cost_usd` is a faithful pass-through of OpenRouter's wire-reported per-turn cost
  (`usage.Cost`), which the provider includes on most turns but not always. When the provider omits
  cost for every turn, the aggregate is legitimately $0 — NOT an Aura defect (the price-table fallback
  in `internal/llm/prices.go` is a display path the Runner's persisted aggregate does not use). SC#3
  therefore hard-gates the aggregation-correctness invariant (conversation aggregate == SUM of the
  per-turn `cache_metrics` rows) and cumulative TOKEN usage > 0 (always reliable), and asserts cost > 0
  only when the provider actually reported it.
- The live model occasionally re-asks (emits another `ask_user`) on resume instead of replying — a
  genuine fresh LLM round, the model's prerogative. SC#1 bounds a resolve-and-redrive loop so this is
  handled deterministically without weakening the "genuine post-resume reply" assertion.
- The auto-title worker is best-effort; SC#3 gives the REAL worker a bounded number of re-fire
  attempts rather than mocking the title.
