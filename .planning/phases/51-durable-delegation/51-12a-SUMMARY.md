---
phase: 51-durable-delegation
plan: 12a
subsystem: agui
tags: [swarm, sse, agui, display, privacy, go]

requires:
  - phase: 51-durable-delegation
    provides: "51-11 terminal transcript markers and queued swarm_spawn worker metadata"
provides:
  - "Identity-scoped push SSE for one worker transcript, with replay, tailing, reasoning redaction, and terminal/idle shutdown"
  - "A multiplexed aura.swarm.worker status stream carrying only child_id, status, last_event_at, events, and duration_sec"
  - "One swarm_spawn display normalizer for synchronous report arrays and durable queued worker objects"
affects: [51-12b, 51-08]

actuals:
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Server-side transcript tailing feeds the shipped streamSSE pump; the browser receives push events and owns no polling loop"
    - "The status stream projects bounded metadata from worker events and never serializes transcript content"
    - "Synchronous and queued swarm_spawn previews share one display normalizer and one ChildReport wire shape"

key-files:
  created:
    - internal/agui/server_swarm_events.go
    - internal/agui/server_swarm_events_status.go
    - internal/agui/server_swarm_events_test.go
  modified:
    - internal/agui/server.go
    - internal/agui/server_swarm_transcript.go
    - internal/agui/server_swarm_transcript_test.go
    - internal/agui/translator.go
    - cmd/aura/serve_agui.go
    - internal/agent/display/payload.go
    - internal/agent/display/preview.go
    - internal/agent/display/preview_test.go
    - internal/agent/display/swarm.go
    - internal/agent/display/swarm_test.go

key-decisions:
  - "Preflight the first child transcript read before committing SSE headers so a hostile child id remains indistinguishable from every other opaque 404 branch"
  - "Use the existing AURA_SWARM_CHILD_IDLE_SEC value for both transcript and status streams; a non-positive value disables idle expiry"
  - "Derive duration from observed transcript timestamps or the terminal marker rather than a wall-clock tick, so an unchanged transcript emits no duplicate status event"
  - "Map unknown terminal status values to failed instead of emitting a value outside the display vocabulary"

requirements-completed: [SWARM-12, SWARM-10]

coverage:
  - id: D1
    description: "An owned worker transcript replays and tails over SSE, skips malformed lines, redacts reasoning, and stops on a terminal marker or idle expiry."
    requirement: SWARM-12
    verification:
      - kind: unit
        ref: "internal/agui/server_swarm_events_test.go#TestSwarmWorkerEvents"
        status: pass
      - kind: race
        ref: "go test -race ./internal/agui/ -count=1"
        status: pass
    human_judgment: false
  - id: D2
    description: "The no-child route emits one bounded aura.swarm.worker event per worker state change and leaks no transcript text."
    requirement: SWARM-12
    verification:
      - kind: unit
        ref: "internal/agui/server_swarm_events_test.go#TestSwarmWorkerStatus"
        status: pass
      - kind: coverage
        ref: "internal/agui 4577/5342 statements = 85.6795%"
        status: pass
    human_judgment: false
  - id: D3
    description: "Synchronous and queued swarm_spawn previews normalize into the same ChildReport display payload with goal and attempts preserved."
    requirement: SWARM-10
    verification:
      - kind: unit
        ref: "internal/agent/display/preview_test.go#TestDecodeSwarmSpawnPreview"
        status: pass
      - kind: coverage
        ref: "internal/agent/display 117/118 statements = 99.1525%"
        status: pass
    human_judgment: false
  - id: D4
    description: "The worker-event endpoint is visually consumable by the cockpit pane and chip."
    requirement: SWARM-12
    verification: []
    human_judgment: true
    rationale: "Browser rendering and interaction are owned by plan 51-12b's live checkpoint."

duration: ~45min
completed: 2026-08-29
status: complete
---

# Phase 51 Plan 12a: Worker Event Stream Summary

**Aura now exposes one push-only, identity-scoped worker transcript stream and one metadata-only multiplexed worker status stream, while synchronous and durable swarm results share the same display normalizer.**

## Accomplishments

- Added `GET /api/conversations/{conv}/swarm/events?child={child}`. It reuses the transcript route's opaque ownership ladder, preflights the child read before writing headers, translates JSONL events through the shipped AG-UI translator with reasoning disabled, tails appended lines, and stops on the terminal marker or the configured idle threshold.
- Added the no-child mode on the same route. It emits `aura.swarm.worker` CUSTOM events with exactly five fields, one initial snapshot per known child, and later events only when that bounded state changes.
- Kept status derivation server-side and ordered: terminal marker, awaiting input, stalled, then running. The only emitted values are `ok`, `failed`, `needs_user_input`, `running`, `stalled`, and `dead_letter`.
- Extended the existing `swarmTranscriptReader` and daemon adapter with `ListChildTranscripts`; no second transcript interface or browser polling surface was introduced.
- Extended `display.ChildReport` with the already-shipped `goal` and `attempts` tags and taught `decodeToolPreview` to normalize both synchronous arrays and queued objects through the same cockpit payload.
- Passed build, vet, full package tests, WSL race tests, and the disposable-database coverage gate. Aggregate owned coverage was 29943/34438 (86.9%); `internal/agui` was 85.6795% and `internal/agent/display` was 99.1525%. The package policy did not change.

## Task Commits

| Task | Commit | Type | What |
|---|---|---|---|
| Task 1 RED | `c6a3a46a1` | test | Worker transcript SSE contract |
| Task 1 GREEN | `bddd4f3de` | feat | Identity-scoped replay, tail, terminal and idle handling |
| Task 2 RED | `aecd441cf` | test | Multiplexed status and queued-preview contracts |
| Task 2 GREEN | `c4ab0cdc9` | feat | Metadata-only worker status stream and shared display normalizer |

## Deviations From Plan

### Auto-fixed Issues

**1. Existing transcript fake no longer satisfied the deliberately widened interface**

- **Found during:** Task 1 compile.
- **Fix:** Added a no-op `ListChildTranscripts` implementation to the existing test double.
- **Impact:** Required compilation repair only; existing transcript-route behavior is unchanged.

**2. The RED idle assertion expected a map shape that AG-UI does not serialize**

- **Found during:** Task 1 GREEN run.
- **Fix:** Asserted the real JSON Patch wire shape (`path=/swarm_child_status`, `value=stalled`) instead of a nonexistent direct key/value object.
- **Impact:** The test is stricter against the actual protocol and production behavior was not weakened.

## Verification

- `go build ./...` - pass
- `go vet ./internal/agui/... ./internal/agent/display/... ./cmd/aura/...` - pass
- `go test ./internal/agent/display/ ./internal/agui/ -count=1` - pass
- `go test -race ./internal/agui/ ./internal/agent/display/ -count=1` - pass
- `bash scripts/coverage_docker.sh` - pass, aggregate 86.9%, package policy pass
- File-size gate - pass (`server.go` 583, `server_swarm_events.go` 161, `server_swarm_events_status.go` 193, `translator.go` 483)

## Self-Check: PASSED

All plan artifacts exist, all acceptance-critical tests and gates pass, the coverage policy is unchanged, and no file under `web/` was modified by this plan.

## Next Plan Readiness

Ready for `51-12b`: the cockpit can consume the child transcript stream and the canonical `aura.swarm.worker` status stream. Live evidence must remain textual and redacted; private browser screenshots are not retained.
