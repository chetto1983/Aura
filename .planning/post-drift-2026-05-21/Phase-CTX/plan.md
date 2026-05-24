# Phase-CTX — Context Engineering Substrate

**Status:** active closure slice; CTX substrate shipped, CTX-CLOSE source/benchmark repaired 2026-05-24
**Provenance:** hermes scout (§2.1 + §8 ContextEngine/ContextCompressor), openhuman scout (§5 payload_summarizer), Codex scout (#7 inline auto-compaction), online 2026 scout (§3 Anthropic effective context engineering + Chroma context-rot)
**Estimated effort:** ~3 sessions
**LOC delta:** ~+900

---

## Why this phase

ANALYSIS-DEEP.md §2.4 surfaced that 4 scouts independently proposed different layers of compaction/summarization. They form a STACK, not alternatives:

1. **ContextEngine ABC** (hermes #2.1) is the substrate — pluggable compression interface, no logic, ~80 LOC.
2. **Default ContextCompressor impl** (hermes #8) with SUMMARY_PREFIX + scaled budget + tool-result pre-pruning + JSON-arg sanitizer + streaming scrubber. ~500 LOC.
3. **payload_summarizer hook** (openhuman #5) plugs into the ContextEngine for per-tool-result compression with circuit breaker. ~300 LOC.
4. **Inline auto-compaction trigger** (Codex #7) is a new ContextEngine implementation that activates at 70-80% token budget. ~400 LOC, mostly reuses #1+#2.

Plus: token-budget compaction REPLACES the current 50-msg sliding window (online consensus + Chroma context-rot study: length alone degrades 18 SOTA models).

End-state: clean seam for compaction, mid-turn summarization of huge tool results, conversation-level compaction at 70%, no silent context-rot.

---

## Stories

### US-CTX-01 — `ContextEngine` interface + default no-op impl

- **Scope:** Define `internal/conversation.ContextEngine` interface:
  ```go
  type ContextEngine interface {
      Name() string
      ShouldCompress(promptTokens int) bool
      Compress(messages []llm.Message, currentTokens int, focusTopic string) []llm.Message
      UpdateFromResponse(usage llm.Usage)
  }
  ```
  Default impl wraps current behavior (50-msg cap → returns unchanged once below cap). Agent loop reads counters from engine instead of inline.
- **Files:** NEW [internal/conversation/engine.go](internal/conversation/engine.go); MODIFY [internal/agent/loop.go](internal/agent/loop.go), [internal/chat/agentloop.go](internal/chat/agentloop.go).
- **LOC delta:** +80.
- **Acceptance:**
  - `go test ./internal/conversation/... ./internal/agent/...` green.
  - Default impl preserves existing 50-msg cap behavior.
- **Provenance:** hermes `agent/context_engine.py`.

### US-CTX-02 — `ContextCompressor` with SUMMARY_PREFIX + scaled budget

- **Scope:** New `internal/conversation/compressor.go` implementing `ContextEngine` with hermes patterns:
  - `SUMMARY_PREFIX` filter-safe summarizer preamble (the production-tested handoff prompt).
  - Scaled summary budget: min 2000 tokens, 20% ratio, max 12000 tokens.
  - Tool-result pre-pruning (deterministic 1-liner replacement: `[search_files] content search for 'X' in agent/ -> 12 matches`).
  - JSON-arg sanitization preserving validity (recursively shrink string leaves).
  - Streaming context-tag scrubber.
  - Image-aware token budgeting (`_IMAGE_TOKEN_ESTIMATE = 1600` per image, matches Claude Code).
- **Files:** NEW [internal/conversation/compressor.go](internal/conversation/compressor.go); NEW [internal/conversation/compressor_test.go](internal/conversation/compressor_test.go).
- **LOC delta:** +500 + 200 tests.
- **Acceptance:**
  - `go test ./internal/conversation/...` green with golden test on SUMMARY_PREFIX byte-stability.
  - Test: JSON-arg truncation produces VALID JSON (round-trip via `json.Unmarshal`).
  - Test: streaming scrubber removes `<memory-context>...</memory-context>` even when split across chunks.
- **Provenance:** hermes `agent/context_compressor.py` (~1000 lines), `agent/memory_manager.py:62-150`.
- **Dependency:** US-CTX-01.

### US-CTX-03 — `payload_summarizer` with circuit breaker

- **Scope:** Trait-based interception of oversized tool results BEFORE they enter agent history:
  - `PayloadSummarizer` interface with `MaybeSummarize(toolName, parentTaskHint, raw)` returning `Option<SummarizedPayload>`.
  - Pass-through guards: below threshold (skip), above max cap (skip — let truncation handle), circuit breaker tripped (skip).
  - Circuit breaker: 3 consecutive failures → disabled for session.
  - **summary >= raw rejection** — if summarizer makes things worse, don't replace.
  - Parent-only scope: only the orchestrator session gets the summarizer; sub-agents see raw.
  - Token estimate `chars/4` (model-agnostic).
- **Files:** NEW [internal/agent/governance/payload_summarizer.go](internal/agent/governance/payload_summarizer.go); MODIFY [internal/agent/executor.go](internal/agent/executor.go) (hook in tool loop after each tool execution).
- **LOC delta:** +300 + 100 tests.
- **Acceptance:**
  - Test: tool result ≥ threshold → summarizer called; result replaces inline.
  - Test: 3 consecutive summarizer failures → breaker trips; subsequent results pass raw.
  - Test: `summary.len() >= raw.len()` → reject, pass raw.
  - **Critical**: summarizer agent's own `payload_summarizer` must be `None` (openhuman lesson — recursive dispatch observed in production, disabled by default).
- **Provenance:** openhuman `harness/payload_summarizer.rs` (487 lines).
- **Dependency:** US-CTX-01, US-CTX-02 (for the summarization LLM call shape).

### US-CTX-04 — Token-budget compaction replacing 50-msg cap

- **Scope:** New `ContextEngine` implementation: `AutoCompactEngine`. Triggers compaction at 70% of model context window (instead of waiting for 100% / 50-msg cap). Two-scope: `Total` vs `BodyAfterPrefix` so overlays + initial-context stay "free".
- **Files:** NEW [internal/conversation/auto_compact.go](internal/conversation/auto_compact.go); MODIFY [internal/agent/loop.go](internal/agent/loop.go) (call engine BEFORE each LLM call); MODIFY [internal/config/config.go](internal/config/config.go) (token-budget config — replace `MaxConversationMessages` with `MaxConversationTokens`).
- **LOC delta:** +400.
- **Acceptance:**
  - Test: 70% threshold triggers compaction; below → passes through unchanged.
  - Probe: 200-turn debug conversation no longer loses early premise (sliding window dropped it).
- **Provenance:** Codex `core/src/session/turn.rs:301-358`, `:655-705`. Online §3.2.
- **Dependency:** US-CTX-01, US-CTX-02.

### US-CTX-05 — Wire `payload_summarizer` + auto-compact onto agent loop

- **Scope:** Replace default `ContextEngine` with `AutoCompactEngine` (US-CTX-04) and register `payload_summarizer` (US-CTX-03). Add config envs: `AURA_CTX_ENGINE` (default `auto_compact`), `AURA_PAYLOAD_SUMMARIZER` (default `true`, but `None` for summarizer agent itself), `AURA_PAYLOAD_THRESHOLD_TOKENS` (default 4096), `AURA_PAYLOAD_MAX_TOKENS` (default 32000).
- **Files:** MODIFY [cmd/aura/app.go](cmd/aura/app.go), [internal/agent/loop.go](internal/agent/loop.go), [internal/config/config.go](internal/config/config.go).
- **LOC delta:** +60.
- **Acceptance:**
  - Smoke: long conversation with big tool results → both compaction AND payload_summarizer fire.
  - `/api/metrics` exposes counters: `ctx_compactions_total`, `payload_summarizations_total`, `payload_summarizer_breaker_trips_total`.

---

## Sequencing

US-CTX-01 (interface) → US-CTX-02 (default impl) → US-CTX-03 (payload summarizer trait) → US-CTX-04 (auto-compact engine) → US-CTX-05 (wire). Each is one commit per `feedback_one_module_per_slice`.

---

## Risks

- **R1 (US-CTX-03)**: openhuman has payload_summarizer DISABLED in production (`threshold_tokens=0`) after observing recursive dispatch. Aura must wire `payload_summarizer = None` for the summarizer agent itself AND ensure thresholds exceed expected summary output size. ANALYSIS-DEEP.md flags this as the highest-risk pattern in the phase.
- **R2 (US-CTX-02)**: SUMMARY_PREFIX is production-tested hermes prompt. Don't tune it without bench evidence — every word is load-bearing.
- **R3 (US-CTX-04)**: switching from msg-count to token-budget changes cap semantics. Existing sessions mid-deploy may compact unexpectedly. Mitigation: default token-budget high enough that current 50-msg conversations don't trigger.
- **R4**: payload_summarizer uses an LLM call → adds latency per oversized tool result. Mitigation: only trigger above threshold; circuit breaker prevents pathological behavior.

---

## Verification

- `go test ./internal/conversation/... ./internal/agent/...` green.
- Token budget metric tracked per turn; expect p50 unchanged for short conversations, p99 dramatically lower for long conversations (compaction kicks in).
- Long-conversation probe: 100+ turns, debug agent retains early premise (test against ground-truth wiki page).
- Bench: re-grade strict-pass; expect +2-4 cases recovered on long-conversation failures.

---

## CTX-CLOSE slices

These slices close Phase-CTX after the original substrate stories. They are
translated from `scripts/ralph/prd.json` into this phase folder so future work
does not depend on the Ralph queue or chat memory.

### US-CTX-06 - AutoCompactEngine robustness

- **Status:** shipped in commits `b792b343`, `9c5e0759`, `d56c4595`, and
  locked by `0742d3ac`.
- **Owned behavior:** prefix protection, hysteresis, focus topic capping and
  loop wiring.
- **Verification anchor:** `internal/conversation/auto_compact_test.go`,
  `internal/agent/focus_topic_test.go`, and the slice QA packet in
  `progress.md`.
- **Non-goal:** no benchmark threshold tuning; that belongs to US-CTX-07.

### US-CTX-07 - Compaction benchmark and quality snapshot

- **Status:** shipped 2026-05-24; live benchmark gate passed with a recorded
  Gemma quality caveat.
- **Goal:** prove the compaction substrate earns production value with
  repeatable fixture data, per-model savings/latency/quality metrics, and a
  `docs/aura-quality-snapshot.md` row.
- **Files expected:** `cmd/bench_ctx/`,
  `internal/conversation/testdata/bench/`,
  `.planning/post-drift-2026-05-21/Phase-CTX/bench-results-<date>.json`, and
  `docs/aura-quality-snapshot.md`.
- **Model set:** `deepseek/deepseek-v4-flash` (163840 ctx),
  `google/gemma-4-26b-a4b-it` (131072 ctx), and
  `anthropic/claude-sonnet-4` (200000 ctx).
- **Gate:** passed. DeepSeek and Claude `long_session` live rows showed
  `savings_pct=99` and `quality_keyword_retained=true`; Gemma showed
  `savings_pct=99` but `quality_keyword_retained=false`, so Gemma remains a
  tuning caveat rather than a default summarizer recommendation.
- **Non-goals:** no threshold code change in this story unless the benchmark
  harness cannot run without it; threshold tuning is recorded as a follow-up
  candidate.

### US-CTX-08 - Compaction event log

- **Status:** shipped 2026-05-24; storage/API event log and redaction checks
  are in place.
- **Goal:** persist per-compaction debug facts and expose them through
  `/api/conversations/:id/compactions`.
- **Files:** `internal/conversation/compaction_events.go`,
  `internal/conversation/auto_compact.go`,
  `internal/db/migrations/m27_add_conversation_compactions.go`,
  `internal/api/conversations.go`, `internal/api/router.go`,
  `internal/channels/telegram/invocation_builder.go`, and
  `internal/storage/memoryindex/rebuild.go`.
- **Gate:** SQLite/API ground truth must prove per-event fields, not only
  aggregate metrics.
- **Result:** `conversation_compactions` stores metadata-only rows keyed by
  `chat_id + turn_index`, with `run_id`, message/token counts, elapsed time,
  threshold/cumulative token fields, timestamp, and a redacted bounded focus
  preview. The API returns the matching conversation id from the archive row.
- **Non-goal:** raw prompts, full messages, tool outputs, and unredacted focus
  text are not persisted.

---

## Implementation Gates

| Slice | Source Gate | Benchmark Gate | Slice QA | Commit Boundary |
| --- | --- | --- | --- | --- |
| US-CTX-06 | Closed by existing code/tests | Unit and delta lint passed in `progress.md` | `self-audited-slice-qa`, passed | `0742d3ac test(ctx): lock compaction robustness checks` |
| US-CTX-07A | `source.md` rows for D:/tmp and 2026 eval practice | offline fixture parser/renderer and deterministic result artifact | diff + fixture bytes + negative malformed fixture | `feat(bench): add CTX compaction fixture runner` |
| US-CTX-07B | live OpenRouter credentials available from env, no secret logging | container/test-profile bench run writes JSON and snapshot row | diff + artifact JSON + quality gate | `feat(bench): record Phase-CTX live compaction snapshot` |
| US-CTX-08 | API/storage event schema mapped | SQLite row + API response + redaction check | diff + negative unauthorized/missing conversation check | `feat(observability): record compaction events` |

## PRD Coverage Matrix

| PRD / Queue Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| Context is runtime state, not durable memory | Why this phase | `benchmark.md` rows B-CTX-07-3/4 | `source.md` ADR/local rows | covered |
| Compaction must preserve instructions and current user task | US-CTX-06 | `benchmark.md` row B-CTX-06 | `source.md` Codex/Hermes rows | shipped |
| Phase must be validated with metrics, not "it ran" | US-CTX-07 | `benchmark.md` rows B-CTX-07-1..7 | `source.md` OpenAI/NIST/Chroma rows | self-audited; next slice bounded |
| Bench must update product quality snapshot | US-CTX-07 | `benchmark.md` row B-CTX-07-6 | `docs/aura-quality-snapshot.md` pattern | planned |
| Per-event debug visibility for compaction | US-CTX-08 | `benchmark.md` row B-CTX-08-1 | `source.md` ADR observability row | shipped 2026-05-24 |

---

*Updated 2026-05-21.*
