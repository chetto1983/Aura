---
status: partial
phase: 04-hitl-identity-conversations
source: [04-VERIFICATION.md]
started: 2026-05-31T00:00:00Z
updated: 2026-05-31T00:00:00Z
---

## Current Test

[awaiting human testing — requires a live OPENROUTER_API_KEY + the Postgres stack up]

## Tests

### 1. Live pause/resume in the REPL
expected: Run `aura chat new`; prompt a model turn that calls `ask_user(kind=approval, question="Proceed?")`; the REPL renders a `[y/N]` prompt; answer `y`; the loop resumes with the answer injected as RoleTool{ToolCallID:<original>} and the assistant continues (SC-4, no duplicate ask_user, no silent re-run).
result: [pending]

### 2. Three simultaneous ask_user calls — FIFO order
expected: A model turn that calls `ask_user` three times with distinct priorities produces 3 rows in `aura.paused_states`; the REPL prompts them in `priority DESC, created_at ASC` order; answering all three resumes the loop with all three answers; the rehydrated history is wire-valid (one assistant turn with all 3 tool_calls followed by the 3 tool answers — CR-02 regression).
result: [pending]

### 3. Auto-title + cumulative USD in `aura chat list`
expected: After a 4-turn conversation, the title column shows a non-empty LLM-generated title and `total_cost_usd > 0`.
result: [pending]

### 4. `aura chat search` similarity-ordered excerpts
expected: Insert a turn with known content; run `aura chat search "<phrase>"`; the matching row appears first in `conv_id|seq|similarity|excerpt` format, ordered by similarity DESC.
result: [pending]

## Summary

total: 4
passed: 0
issues: 0
pending: 4
skipped: 0
blocked: 0

## Gaps
