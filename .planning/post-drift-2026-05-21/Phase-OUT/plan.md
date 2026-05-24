# Phase-OUT — Output Discipline + Budget Enforcement

**Status:** ✅ closed 2026-05-23 (historical plan; do not execute as-is)
**Provenance:** Codex (#5), nanobot (#1, #2, #3, #11, #13), elysia (#5, #6), plus 2 new scouts on tool-budget enforcement (`tool-budget-enforcement-patterns.md` + `tool-budget-2026-online.md`).
**Estimated effort:** ~3 sessions, 9 atomic stories (6 output-discipline + 3 budget-enforcement)
**LOC delta:** ~+720 (~+520 original + ~+200 budget stories)

---

## Why this phase

Two convergent problem classes — both code-level, both backed by 2026 production evidence:

### Problem A — Output discipline (the original stack)

Multiple scouts (codex/nanobot/elysia) independently surfaced different layers. Cross-analysis (`ANALYSIS-DEEP.md` §1.2) shows they form a stack, not competing alternatives:

```text
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

### Problem B — Budget enforcement (NEW — added 2026-05-22)

Live evidence 2026-05-22: agent invocation made 30+ `web_search`/`web_fetch` calls in 2 minutes with 4 ignored 404s, eventually `elapsed_ms=179668 llm_calls=28 tool_calls=33 delivered=false error="context canceled"`. A prompt rule ("prefer wiki first") didn't help — the LLM ignored it.

Two convergent scouts proved this needs CODE-level enforcement (not prompt-level), with specific production patterns:

| Pattern | Source | What it kills |
|---|---|---|
| Per-tool-class budget (web=3, wiki=20, exec=2) | arXiv 2511.17006 + BATS benchmark | Cheap-tool over-spending on vertical agents |
| OR-of-four guard (iter + tokens + wall-clock + $) | Sattyam Jain $47K postmortem + Hermes ChatCompletionHelper | Single-signal blindness; nanobot 200-iter cap with no wall-clock = 180s timeouts |
| Force-finalize on cap (strip tools + synthesis turn) | Hermes `chat_completion_helpers.py:925` | "context canceled" with empty reply (user gets nothing) |
| Hash-based same-call dedup (2-strike on same url/query) | nanobot `_MAX_REPEAT_EXTERNAL_LOOKUPS=2` | The exact 30-web_search thrash pattern |

**Anti-pattern explicit:** elysia's `tree_count_string` injected prompt-pressure warnings ("you're approaching cap"). Hermes REMOVED equivalent prompt-pressure in PR #7915 with the rationale "models give up prematurely on complex tasks." Aura's "prefer wiki first" prompt rule failed for the same reason — **prompt-pressure is noise; code-level enforcement is signal.**

US-OUT-03 (repeated-lookup throttle, original story) already implements the hash-based same-call dedup at the FIRST layer. US-OUT-07/08/09 (NEW) add the per-class budget + OR-of-four guard + force-finalize at the runtime-supervisor layer.

End-state: kills "I dropped data, retry the search" loop (US-OUT-01/02), kills "I haven't searched yet" relapse (US-OUT-04), kills silent-truncation hallucination (US-OUT-05), kills malformed-history provider rejection (US-OUT-06), kills 30-call thrash (US-OUT-03/07), kills "180s timeout with empty reply" (US-OUT-08/09).

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

### US-OUT-07 — Per-tool-class budget caps (web=3, wiki=20, exec=2)

- **Scope:** Add a per-tool-class budget enforced inside `internal/agent/executor.go` (or equivalent dispatch hub). Class → cap mapping configurable via env vars + runtime settings, with sensible defaults: `web=3, wiki=20, exec=2, source=8, scheduler=5, ask_user=3, default=10`. Counter is per-turn (resets between agent.Run invocations). When a tool's class hits cap → the tool call is rejected at executor level with a structured error fed back as the tool result (so the LLM sees "web budget exhausted; use wiki or finalize" not a silent failure).
- **Source:** arXiv 2511.17006 + BATS benchmark — per-class beats flat for vertical agents. Aura's web/wiki/exec are very different cost/utility classes.
- **Files:** NEW [internal/agent/governance/budget.go](internal/agent/governance/budget.go); MODIFY [internal/agent/executor.go](internal/agent/executor.go) (call budget check before Execute); MODIFY [internal/config/applier.go](internal/config/applier.go) (new keys: `AURA_TOOL_BUDGET_WEB`, `AURA_TOOL_BUDGET_WIKI`, etc., int 1..100).
- **LOC delta:** +120 + 60 tests.
- **Acceptance:**
  - Probe: a turn that calls `web_search` 4 times in a row → 4th call returns budget-exhausted error; LLM sees the error and pivots.
  - Counter resets per turn (verified by 2 sequential turns with 3 web calls each — both succeed).
  - Setting `AURA_TOOL_BUDGET_WEB=10` via DB updates → reflects in agent runtime after restart.
  - `tool_class()` helper maps tool name → class (regex-based: `web_*` → web, `read_source/store_source/ingest_source/ocr_source` → source, etc.) and is unit-tested.
- **Single atomic commit:** `feat(agent): per-tool-class budget caps with runtime override (US-OUT-07)`

### US-OUT-08 — OR-of-four guard: iter + tokens + wall-clock + dollars

- **Scope:** Replace the current single-signal MaxIterations cap with an OR-of-four guard at the top of every iteration:
  - `iterations >= MaxIterations` (current — keep)
  - `tokens_total >= MaxTokens` (new — defaults to 500k per existing `MAX_CONTEXT_TOKENS` setting)
  - `wall_clock >= MaxElapsed` (existing — Aura has `MaxElapsed: 120s` per `AURABOT_TIMEOUT_SEC`; lower to 60s default)
  - `cost_usd >= MaxCost` (new — defaults to `HARD_BUDGET` setting, currently 20)
  - When any one trips, set `stop_reason` to that specific signal and trigger US-OUT-09 force-finalize. Logged as structured event with the trigger.
- **Source:** Sattyam Jain $47K LangChain postmortem + Hermes `ChatCompletionHelper` (rejects single-signal blindness as the production failure mode in 2026).
- **Files:** MODIFY [internal/agent/loop.go](internal/agent/loop.go) (at iteration top, check 4 signals); NEW [internal/agent/governance/guard.go](internal/agent/governance/guard.go) with structured `StopReason` enum.
- **LOC delta:** +80 + 40 tests.
- **Acceptance:**
  - Test: turn that exceeds tokens but stays under iter cap → trips `stop_reason=token_budget_exceeded`.
  - Test: turn that's slow (>60s) but few tool calls → trips `stop_reason=wall_clock_exceeded`.
  - Test: turn that costs >$20 → trips `stop_reason=cost_budget_exceeded`.
  - All four signals visible in `/api/runs/<id>` JSON.
- **Single atomic commit:** `feat(agent): OR-of-four budget guard (iter + tokens + wall-clock + cost) (US-OUT-08)`

### US-OUT-09 — Force-finalize on budget exhaustion (Hermes pattern)

- **Scope:** When US-OUT-07 OR US-OUT-08 trips, fire ONE additional LLM call with `tool_choice="none"` (no tools available) and a user-side prompt: "Budget exhausted (signal: <stop_reason>). Synthesize the best answer you have from the context so far. Be honest about what's missing." The single synthesis turn produces a useful reply instead of the current `error="context canceled"` empty.
- **Source:** Hermes `agent/chat_completion_helpers.py:925` `_force_synthesis_turn` — production-validated for user-facing agents (Telegram is user-facing → silent failure is unacceptable).
- **Files:** MODIFY [internal/agent/loop.go](internal/agent/loop.go) — extend existing `gracefulFinalize` to be called on the new stop_reasons (currently only fires on empty_llm_response and max_iterations); NEW helper in [internal/agent/governance/](internal/agent/governance/) that builds the synthesis prompt with `stop_reason` injection.
- **LOC delta:** +60.
- **Acceptance:**
  - Probe: induce wall-clock budget exhaustion (mock LLM with 30s response time × 3 turns) → user gets a synthesis reply like "I was working on X but ran out of time; here's what I found: ..." NOT an empty/error reply.
  - The synthesis turn is logged with `stop_reason` so the dashboard can show "agent budget hit X at turn N — synthesized from partial work."
  - Three stop_reasons supported: `token_budget_exceeded`, `wall_clock_exceeded`, `cost_budget_exceeded` (plus existing `max_iterations_hit`, `empty_llm_response`).
- **Single atomic commit:** `feat(agent): force-finalize on budget exhaustion (Hermes pattern) (US-OUT-09)`

---

## Sequencing

US-OUT-01 first (truncate-middle is the floor). Then US-OUT-02 (spill uses truncate). Then US-OUT-03, US-OUT-04 in parallel (independent). US-OUT-05, US-OUT-06 last in original group (history-shape changes; need stable upstream behavior).

THEN the budget stories (require existing throttle US-OUT-03 to coexist cleanly):
- US-OUT-07 (per-class budget) — independent of 01-06
- US-OUT-08 (OR-of-four guard) — depends on US-OUT-07's `BudgetExhausted` shape for stop_reason classification
- US-OUT-09 (force-finalize) — depends on US-OUT-08 for the stop_reason signals to trigger on

**One story = one commit.**

---

## Risks

- **R1 (US-OUT-02)**: spill-to-disk introduces a new caching surface. LRU sweep must be tested under concurrent session activity. Mitigation: write the sweeper test FIRST.
- **R2 (US-OUT-03)**: per-turn counter may be too strict for legitimately iterative research. Mitigation: `_MAX_REPEAT_EXTERNAL_LOOKUPS = 2` is the production-tested number from nanobot; don't tune higher without bench evidence.
- **R3 (US-OUT-04)**: token cost of `tasks_completed_string` block is ~200-500/turn. With wiki TOC + this + overlays + history, runtime context budget gets squeezed. Mitigation: only inject when turn has >0 tool calls.
- **R4 (US-OUT-06)**: silent history mutation. Mitigation: log every backfill/skip event; expose via `/api/insights`.
- **R5 (US-OUT-07)**: per-class budget caps may be too low for legitimate complex queries (e.g. multi-source research). Mitigation: defaults are tunable via runtime settings; expose via dashboard; first 2 weeks log every cap-hit so we tune empirically.
- **R6 (US-OUT-08)**: cost-budget enforcement requires accurate $/turn estimate; today Aura has `EstimateCost` but its accuracy depends on provider returning usage. Mitigation: when usage missing, fall back to other 3 signals; never block on cost alone.
- **R7 (US-OUT-09)**: force-finalize synthesis turn itself costs $ + adds latency. Mitigation: cap at ONE synthesis call (no recursive finalize); if synthesis itself errors, return the partial work directly without further LLM. Anti-pattern: prompt-pressure warnings in the synthesis prompt — keep it terse and factual per Hermes #7915 lesson.

---

## Verification

- `go test ./...` green.
- `golangci-lint run ./internal/agent/...` clean on touched files.
- Probe: re-run the QA-phase probe suite (`cmd/probe_chat/qa_phase_cases.go`) — must not regress; expect strict-pass +2-4 on the chatty-thrashing failures.
- Bench: measure prompt-token-per-turn before/after; expect +200-500 from tasks_completed but offset by -1000+ from spill (no re-execution).
- **Live thrash regression test (new, US-OUT-07/08/09):** induce the 2026-05-22 scenario (query that triggers web_search → 404 → retry loop). Pre-fix: 28 LLM calls / 33 tool calls / 180s / empty reply. Post-fix targets: ≤8 LLM calls, ≤6 tool calls, ≤30s, NON-empty synthesis reply with explicit `stop_reason` mention. Document in `docs/quality-bench/runs/post-out-2026-XX-XX.md`.

---

*Updated 2026-05-22 — added US-OUT-07/08/09 budget enforcement stories backed by 2 convergent scout deliverables (`tool-budget-enforcement-patterns.md` + `tool-budget-2026-online.md`).*
