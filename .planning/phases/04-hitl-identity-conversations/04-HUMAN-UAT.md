---
status: passed
phase: 04-hitl-identity-conversations
source: [04-VERIFICATION.md]
started: 2026-05-31T00:00:00Z
updated: 2026-05-31T06:00:00Z
executed_by: orchestrator (live model deepseek/deepseek-v4-flash:exacto + Postgres stack)
---

## Current Test

[complete — all 4 items executed live against deepseek/deepseek-v4-flash:exacto with the Postgres stack up; ground truth verified in aura.paused_states / aura.conversation_turns / aura.conversations]

## Tests

### 1. Live pause/resume in the REPL
expected: ask_user(kind=approval) pauses, [y/N] renders, answer resumes with the answer injected as RoleTool{ToolCallID:<original>}; the assistant continues (SC-4).
result: PASS. The live model called ask_user(approval, "Proceed?"). DB ground truth: paused_states.resumed_answer = {"action":"accept","content":"yes"}; conversation_turns showed assistant{tool_call call_00_qrd...} → tool{content:"yes", tool_call_id:call_00_qrd...} (answer keyed to the ORIGINAL tool_call_id — SC-4 wire-valid). A real-task variant ("rename folder, ask approval first") produced a genuine post-resume reply (`mv vecchio-progetto nuovo-progetto`, 1534 tok) — resume drives a fresh LLM round.

### 2. Three simultaneous ask_user calls — FIFO order
expected: 3 ask_user calls in one turn → 3 paused_states rows, prompted in priority DESC order, all answered, wire-valid rehydration (CR-02).
result: PASS. The model emitted 3 ask_user calls in one turn; the REPL prompted them priority DESC (Deploy=90 [y/N] → region=50 free-text → email=10 [y/N]). DB: 3 paused_states rows all resolved with correct {action,content}; conversation_turns showed ONE assistant turn carrying all 3 tool_calls (call_00/01/02) immediately followed by 3 tool answers each keyed to its tool_call_id — the CR-02 fix proven against a real model (no wire-invalid interleaving).

### 3. aura chat list shows auto-generated title + cumulative USD
expected: after a multi-turn conversation the title is a non-empty LLM-generated title; total_cost_usd > 0.
result: PASS (after fixing a real bug found here — commit 12506a8e). A 3-turn conversation now shows title "Capital of France and Japan asked" and COST_USD $0.0003 in `aura chat list`. (Bug found: the REPL consumer returned early on the final Event, so the auto-title worker never fired; fixed by draining the iterator. Cumulative token+USD aggregation was already correct.)

### 4. aura chat search similarity-ordered excerpts
expected: search returns matching rows first, ordered by similarity DESC, in conv_id|seq|similarity|excerpt format.
result: PASS. `aura chat search "capital"` returned matching turns ordered by similarity DESC (0.348 → 0.320 → 0.308) in CONVERSATION|SEQ|SIMILARITY|EXCERPT format over the locked pg_trgm FTS query. (A short 5-char term scoring below the 0.3 trigram threshold against longer content is correct pg_trgm behavior, not a defect.)

## Summary

total: 4
passed: 4
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

None. One real bug (auto-title worker never fired — iterator not drained) was found by this live UAT and fixed in commit 12506a8e with a regression test (cmd/aura/chat_render_drain_test.go).
