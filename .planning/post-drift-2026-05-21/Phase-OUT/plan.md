# Phase-OUT — Output Discipline Stack

**Status:** 🟡 queued after Phase-CACHE
**Provenance:** Codex (#5), nanobot (#1, #2, #3, #11, #13), elysia (#5, #6)
**Estimated effort:** ~2 sessions
**LOC delta:** ~+520

---

## Why this phase

Multiple scouts independently surfaced different layers of output discipline. Cross-analysis (ANALYSIS-DEEP.md §1.2) shows they form a stack, not competing alternatives:

```
LLM sees: [structured preview]    (openhuman summarizer, lands in Phase-CTX)
            OR
          [head + tail + marker]  (Codex truncate-middle, this phase US-OUT-01)
            OR
          [path + small preview]  (nanobot spill-to-disk, this phase US-OUT-02)

Mid-turn: lookup throttle blocks duplicate web_search/web_fetch  (US-OUT-03)
          tasks_completed inline state block prevents tool-repeat (US-OUT-04)
          length-recovery + ignore-tool-calls-on-length (US-OUT-05)
          orphan tool_call backfill + skip-empty-assistant (US-OUT-06)
```

End-state: kills "I dropped data, retry the search" loop, kills "I haven't searched yet" relapse, kills silent-truncation hallucination, kills malformed-history provider rejection.

---

## Stories

### US-OUT-01 — Centralized truncate-middle + exec output wrapper

- **Scope:** New `internal/agent/tools/truncate.go` with `TruncateMiddle(s string, maxBytes int) string` + token variant. Walks `char_indices` for UTF-8-safe prefix_end + suffix_start. Inserts `…N tokens truncated…` marker the model recognizes. Swap `limitToolContent` (executor.go:99) to use it. Add structural wrapper for exec/file output (`Exit code:`, `Wall time:`, `Total output lines:`, `Output:`) so model sees the structural signal even when body is gutted.
- **Files:** NEW [internal/agent/tools/truncate.go](internal/agent/tools/truncate.go); MODIFY [internal/agent/executor.go](internal/agent/executor.go); MODIFY [internal/agent/exec_helpers.go](internal/agent/exec_helpers.go) or wherever exec output is formatted.
- **LOC delta:** +90 / -10 = +80.
- **Acceptance:**
  - `go test ./internal/agent/tools/...` green with new tests covering middle-truncate, marker presence, UTF-8 boundaries.
  - Probe: induce a 50KB grep output → result has head + tail + marker, total ≤ 8KB.
- **Provenance:** Codex `core/src/utils/string/src/truncate.rs:7-69` + `core/src/tools/mod.rs:64-89`.

### US-OUT-02 — Spill-to-disk + reference envelope

- **Scope:** When tool output > `max_tool_result_chars`, persist full payload to `runtime-workspace/tool-results/<session>/<call_id>.<ext>`. Replace inline content with reference envelope: header + path + original size + 1200-char preview. New `read_tool_result(path)` tool OR extend `workspace_read` if path inside workspace. LRU+age sweep (32 sessions, 7-day TTL) piggybacks on every persist.
- **Files:** NEW [internal/agent/tools/registry/spilloutput.go](internal/agent/tools/registry/spilloutput.go); NEW [internal/storage/sweep/lru_age.go](internal/storage/sweep/lru_age.go); MODIFY [internal/agent/tools/registry/boundoutput.go](internal/agent/tools/registry/boundoutput.go) (replace truncation marker with spill+envelope when applicable); NEW or EXTEND read tool.
- **LOC delta:** +180.
- **Acceptance:**
  - Probe: tool returning 100KB → on-disk file exists, LLM sees envelope with path + 1200-char preview.
  - Test: after 33 sessions, sweep evicts oldest.
- **Provenance:** nanobot `utils/helpers.py:322-368`, `:272-287`, `:297-309`.
- **Dependency:** US-OUT-01 (truncate-middle is still the floor when spill is unavailable; envelope content uses it).

### US-OUT-03 — Repeated external-lookup throttle

- **Scope:** Per-turn signature counter for `web_search` (by query, lowercased+trimmed) and `web_fetch` (by URL). After 2 hits of the same target, short-circuit with a hard error: "Error: repeated external lookup blocked. Use the results you already have to answer, or try a meaningfully different source." `_MAX_REPEAT_EXTERNAL_LOOKUPS = 2`. Extend signature to `search_memory` (by query) for free.
- **Files:** NEW [internal/agent/governance/repeated_lookup.go](internal/agent/governance/repeated_lookup.go); MODIFY [internal/agent/executor.go](internal/agent/executor.go) (wire before `Execute`).
- **LOC delta:** +80 + 40 tests.
- **Acceptance:**
  - Probe: agent calls `web_search("X")` twice → both run; third → blocked with error message.
  - Probe: `search_memory("X")` same shape.
  - Counter resets per turn (not per session).
- **Provenance:** nanobot `utils/runtime.py:68-102`.

### US-OUT-04 — `tasks_completed_string` inline state block

- **Scope:** Inject a `## Already done this turn` block (XML-tagged per-turn action ledger: `<task_N>` SUCCESSFUL/UNSUCCESSFUL with reasoning) BEFORE the per-iteration step hint. Per-turn only (chat history covers cross-turn). Cost: ~200-500 tokens/turn budget, acceptable vs wiki TOC.
- **Files:** MODIFY [internal/agent/turnstats.go](internal/agent/turnstats.go) (extend tracking with action+result+brief); NEW render function in [internal/conversation/system_prompt.go](internal/conversation/system_prompt.go); MODIFY [internal/agent/loop.go](internal/agent/loop.go) (injection point).
- **LOC delta:** +120 + 60 tests.
- **Acceptance:**
  - Probe: agent calls `search_memory("X")` → result empty → next iteration prompt contains `<task_1>...search_memory("X")...(SUCCESSFUL but no results)`.
  - Probe: the "I haven't searched yet, let me search" relapse no longer fires.
- **Provenance:** elysia `tree/objects.py:759-798`.

### US-OUT-05 — Length-recovery + ignore-tool-calls-on-length

- **Scope:** Two safety rails:
  - When LLM emits tool calls AND `finish_reason="length"`, ignore the tool calls (likely truncated JSON). Log warning.
  - When LLM hits output cap with partial text response, append user-side recovery prompt: "Output limit reached. Continue exactly where you left off — no recap, no apology." Cap at 3 such recoveries per turn.
- **Files:** MODIFY [internal/agent/loop.go](internal/agent/loop.go).
- **LOC delta:** +60 + 30 tests.
- **Acceptance:**
  - Test: mock LLM emits `finish_reason="length"` with tool_calls → tool calls NOT executed.
  - Test: mock LLM emits `finish_reason="length"` with non-blank text → next call appends recovery message.
- **Provenance:** nanobot `runner.py:398-403`, `:437-456`.

### US-OUT-06 — Orphan tool_call backfill + skip empty-assistant on persist

- **Scope:** Two history-cleanup operations:
  - Before every LLM call, strip `role:tool` messages whose `tool_call_id` doesn't match any prior assistant `tool_calls`. Insert synthetic `[Tool result unavailable — call was interrupted or lost]` for declared assistant tool_calls that never got a result.
  - When persisting to conversation archive, skip assistant messages with neither content nor tool_calls — they poison subsequent prompts.
- **Files:** NEW [internal/agent/governance/history_hygiene.go](internal/agent/governance/history_hygiene.go); MODIFY [internal/agent/loop.go](internal/agent/loop.go) (call before each LLM round); MODIFY [internal/conversation/archive.go](internal/conversation/archive.go) (skip empty-assistant).
- **LOC delta:** +130 + 60 tests.
- **Acceptance:**
  - Test: history with declared tool_call_id `abc` + no matching tool result → backfill inserts placeholder.
  - Test: empty-assistant turn → not persisted.
  - Probe: replay a known-broken-history scenario from `feedback_sqlite_wal_windows_corruption` recovery → no provider 400.
- **Provenance:** nanobot `runner.py:1070-1094`, `:1096-1135`, `loop.py:1444-1445`.

---

## Sequencing

US-OUT-01 first (truncate-middle is the floor). Then US-OUT-02 (spill uses truncate). Then US-OUT-03, US-OUT-04 in parallel (independent). US-OUT-05, US-OUT-06 last (history-shape changes; need stable upstream behavior).

**One story = one commit.**

---

## Risks

- **R1 (US-OUT-02)**: spill-to-disk introduces a new caching surface. LRU sweep must be tested under concurrent session activity. Mitigation: write the sweeper test FIRST.
- **R2 (US-OUT-03)**: per-turn counter may be too strict for legitimately iterative research. Mitigation: `_MAX_REPEAT_EXTERNAL_LOOKUPS = 2` is the production-tested number from nanobot; don't tune higher without bench evidence.
- **R3 (US-OUT-04)**: token cost of `tasks_completed_string` block is ~200-500/turn. With wiki TOC + this + overlays + history, runtime context budget gets squeezed. Mitigation: only inject when turn has >0 tool calls.
- **R4 (US-OUT-06)**: silent history mutation. Mitigation: log every backfill/skip event; expose via `/api/insights`.

---

## Verification

- `go test ./...` green.
- `golangci-lint run ./internal/agent/...` clean on touched files.
- Probe: re-run the QA-phase probe suite (`cmd/probe_chat/qa_phase_cases.go`) — must not regress; expect strict-pass +2-4 on the chatty-thrashing failures.
- Bench: measure prompt-token-per-turn before/after; expect +200-500 from tasks_completed but offset by -1000+ from spill (no re-execution).

---

*Updated 2026-05-21.*
