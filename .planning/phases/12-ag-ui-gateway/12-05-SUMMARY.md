---
phase: 12-ag-ui-gateway
plan: 05
subsystem: api
tags: [reasoning, sse, openai-compat, chain-of-thought, streaming, deepseek, vllm, agent-events]

# Dependency graph
requires:
  - phase: 01-llm-client
    provides: "openai_compat SSE parser (wireChunk/handleChunk), llm.Chunk, llm.Client streaming"
  - phase: 09-agent-runtime
    provides: "agent.Event/LLMResponse model, LlmAgent consume loop, chunkEvent mirror pattern"
provides:
  - "wireChunk.Delta accept-both reasoning + reasoning_content with immediate Chunk{Reasoning} emission (token-per-token, no accumulation)"
  - "llm.Chunk.Reasoning field (fourth mutually-exclusive variant)"
  - "agent.LLMResponse.Reasoning field (additive, omitempty, round-trip-symmetric)"
  - "agent.(*LlmAgent).reasoningChunkEvent mirror of chunkEvent"
  - "consume loop case c.Reasoning != \"\" (stream-only, never folded into accumulated content)"
  - "two dual-field golden SSE fixtures proving identical emitted events (#33)"
affects: [12-06-ag-ui-translator, cli-renderer, slice-13-vllm-local]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Accept-both wire decoding: a helper resolves reasoning vs reasoning_content so a provider field rename cannot silently drop CoT"
    - "Stream-only data plane: reasoning is emitted immediately but never written to the accumulated content the persistence layer reads"
    - "Additive Event field via eventWire json tags: omitempty string stays byte-symmetric for free, verified by round-trip test (no MarshalJSON hand-edit)"

key-files:
  created:
    - internal/llm/openai_compat/testdata/reasoning-field.txt
    - internal/llm/openai_compat/testdata/reasoning-content-field.txt
  modified:
    - internal/llm/openai_compat/sse.go
    - internal/llm/openai_compat/sse_test.go
    - internal/llm/client.go
    - internal/agent/event.go
    - internal/agent/event_test.go
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_events.go
    - internal/agent/llm_agent_test.go

key-decisions:
  - "reasoningDelta helper prefers `reasoning` over `reasoning_content` when both present (no real provider sends both); accept-both per #33"
  - "Reasoning bypasses the tool-call accumulator entirely and is never written to the consume loop's `b` builder — stream-only, never persisted (amendment #57)"
  - "Reasoning Event uses LLMResponse.Reasoning (not Content) so a downstream renderer/translator can distinguish CoT from assistant prose"

patterns-established:
  - "Accept-both wire-field decoding via a tiny resolver to harden against provider field renames"
  - "Stream-only Event field: emitted live, omitempty on the wire, excluded from accumulated/persisted content"

requirements-completed: [UX-01]

# Metrics
duration: 22 min
completed: 2026-06-06
---

# Phase 12 Plan 05: Reasoning Data-Plane Summary

**Additive reasoning data-plane threading chain-of-thought from the SSE wire (accept-both `reasoning`/`reasoning_content`) through `llm.Chunk.Reasoning` and `agent.LLMResponse.Reasoning` to a `reasoningChunkEvent`, emitted token-per-token and never persisted (amendment #57 / acceptance #33).**

## Performance

- **Duration:** 22 min
- **Started:** 2026-06-06T (plan start)
- **Completed:** 2026-06-06
- **Tasks:** 2
- **Files modified:** 8 (+ 2 created)

## Accomplishments
- `wireChunk.Delta` now decodes BOTH `reasoning` (vLLM/DGX local) and `reasoning_content` (DeepSeek-V4 remote); `handleChunk` emits each reasoning delta the instant it decodes as `llm.Chunk{Reasoning}`, mirroring the Content branch — token-per-token immediacy, no buffering.
- `llm.Chunk` gains a `Reasoning` field (fourth mutually-exclusive populated variant); reasoning bypasses the tool-call accumulator entirely (stream-only).
- `agent.LLMResponse` gains a `Reasoning` field (omitempty, round-trip-symmetric via `eventWire` json tags — no Marshal/Unmarshal hand-edit needed).
- `reasoningChunkEvent` mirrors `chunkEvent` (same trace-identity stamping, sets `Reasoning` not `Content`); the consume loop adds `case c.Reasoning != ""` that yields it WITHOUT writing to the accumulated `b` builder, so the persisted answer stays reasoning-free.
- Two dual-field golden SSE fixtures carry the same logical stream differing only in the delta field name and prove identical emitted `[]Chunk{Reasoning}` sequences (acceptance #33), plus immediacy ordering, no-leak, and empty-delta skip.
- The `internal/agent` ⇸ `internal/agui` boundary stays intact (zero agui imports — D-17).

## Task Commits

Each task was committed atomically (TDD: test + impl folded per task; field additions land with their emission/consumer):

1. **Task 1: wireChunk accept-both reasoning + immediate Chunk{Reasoning} emission + dual-field golden fixtures** - `9ff25938` (feat)
2. **Task 2: agent.LLMResponse.Reasoning + reasoningChunkEvent + consume case** - `9f981dc1` (feat)

_Note: `llm.Chunk.Reasoning` was added in Task 1 (commit 9ff25938) because the wire layer emits it; Task 2 added the agent-side `LLMResponse.Reasoning`, the mirror event, and the consume case._

## Files Created/Modified
- `internal/llm/openai_compat/sse.go` - `wireChunk.Delta` accept-both fields; `handleChunk` emits `Chunk{Reasoning}` immediately via the `reasoningDelta` resolver; doc comments updated.
- `internal/llm/openai_compat/sse_test.go` - `TestStream_ReasoningDualField`: identical emitted events for both fixtures (#33), immediacy ordering, no-leak, empty-delta skip.
- `internal/llm/openai_compat/testdata/reasoning-field.txt` - golden SSE using `delta.reasoning` (vLLM/DGX style).
- `internal/llm/openai_compat/testdata/reasoning-content-field.txt` - golden SSE using `delta.reasoning_content` (DeepSeek-V4 style); same payload as the other fixture.
- `internal/llm/client.go` - `llm.Chunk.Reasoning string` field + struct doc naming Reasoning as a populated variant.
- `internal/agent/event.go` - `LLMResponse.Reasoning string` (omitempty, stream-only CoT, D-17 additive).
- `internal/agent/event_test.go` - round-trip tests: reasoning set (wire carries `reasoning`, omits `content`) and empty (wire omits `reasoning`).
- `internal/agent/llm_agent_events.go` - `reasoningChunkEvent` mirror of `chunkEvent`.
- `internal/agent/llm_agent.go` - consume `case c.Reasoning != ""` that yields the reasoning Event without writing to `b`; doc comment updated.
- `internal/agent/llm_agent_test.go` - `TestLlmAgent_ReasoningChunk_StreamOnly`: reasoning Event emitted (Reasoning set, Content empty) and reasoning absent from the final accumulated answer.

## Decisions Made
- `reasoningDelta(reasoning, reasoningContent)` prefers `reasoning` when both are non-empty (no real provider sends both). Accept-both per #33 so a provider field rename cannot silently drop CoT.
- Reasoning never enters the tool-call accumulator and never the consume loop's `b` builder — the returned `text` (what persistence reads) is content-only.
- `LLMResponse.Reasoning` round-trips via its json tag through `eventWire.LLMResponse`; an `omitempty` string is byte-symmetric for free — verified by round-trip tests, MarshalJSON/UnmarshalJSON untouched.

## Deviations from Plan

Plan executed exactly as written — no functional deviations. One acceptance-criterion measurement note (not a code change):

### Acceptance-criterion measurement note

**1. [Note] `grep -c 'Reasoning string'` literal does not match gofmt-aligned struct fields**
- **Found during:** Task 2 acceptance verification
- **Issue:** The plan's criteria `grep -c 'Reasoning string' internal/llm/client.go ≥ 1` and `... internal/agent/event.go ≥ 1` use a single-space literal. gofmt aligns struct field columns, so the source reads `Reasoning    string` (multiple spaces) and the single-space literal grep returns 0.
- **Resolution:** No code change — gofmt alignment is mandated by the pre-commit hook and the field is functionally correct. Presence verified with the alignment-tolerant pattern `grep -nE 'Reasoning +string'` (1 match in each file) and proven by the passing round-trip + consume tests. This is a brittle literal-match criterion, not a defect; modifying the source to satisfy a single-space grep would fight gofmt.
- **Files modified:** none
- **Verification:** `grep -nE 'Reasoning +string' internal/llm/client.go` → `77: Reasoning    string`; `... internal/agent/event.go` → `54: Reasoning    string ...`; tests `TestEvent_LLMResponseReasoning_RoundTripsByteIdentical`, `TestEvent_EmptyReasoning_OmitsKey`, `TestLlmAgent_ReasoningChunk_StreamOnly` all PASS.

---

**Total deviations:** 0 functional (1 measurement note on a brittle grep criterion).
**Impact on plan:** None. All behavior, threat mitigations (T-12-17, T-12-18), and success criteria satisfied as written.

## Verification Results

- `go vet ./internal/llm/... ./internal/agent/...` — clean
- `go build ./...` — clean
- `go test -race ./internal/llm/openai_compat/ ./internal/agent/` — PASS (race-clean, run with the Windows toolchain fix; CI runs native Linux race)
- `golangci-lint run ./internal/llm/... ./internal/agent/...` — 0 issues
- `bash scripts/check-file-size.sh` — all Go files ≤ 600 LOC
- `git diff --stat internal/llm/openai_compat/accumulate.go` — empty (reasoning bypasses the accumulator)
- `internal/agent` imports of `internal/agui` — 0 (D-17 boundary intact)
- dual-field fixtures emit identical `[]Chunk{Reasoning}` (#33), reasoning arrives before content (immediacy), empty delta skipped, no leak into accumulated content

## Known Stubs
None — this plan is a pure additive data-plane; no placeholders, empty data sources, or unwired surfaces introduced.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Reasoning now reaches `agent.LLMResponse.Reasoning` as a live, omitempty Event field — ready for Plan 12-06 (AG-UI translator) to fan it out to a REASONING_* AG-UI event stream and for the CLI renderer to display the live CoT.
- Stream-only invariant holds: reasoning is never persisted to `conversation_turns`, so no migration or persistence change is needed downstream.
- No blockers.

## Self-Check: PASSED

- `internal/llm/openai_compat/testdata/reasoning-field.txt` — FOUND
- `internal/llm/openai_compat/testdata/reasoning-content-field.txt` — FOUND
- commit `9ff25938` — FOUND
- commit `9f981dc1` — FOUND

---
*Phase: 12-ag-ui-gateway*
*Completed: 2026-06-06*
