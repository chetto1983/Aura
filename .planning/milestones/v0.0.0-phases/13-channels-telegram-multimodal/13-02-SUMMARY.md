---
phase: 13-channels-telegram-multimodal
plan: 02
subsystem: api
tags: [send_file, artifact, ag-ui, deferred-tool, channels, translator, custom-event]

# Dependency graph
requires:
  - phase: 12-agui-gateway
    provides: "internal/agui/translator.go (Event→AG-UI event state machine, STATE_DELTA additive-branch template) + Actions.ArtifactDelta forward-compat field (event.go:71)"
  - phase: 03-agent-core
    provides: "tools.ToolResult.Meta outbound seam (shell_exec), Deferred-tool convention (web_fetch), toolResultEvent Meta projection (llm_agent_events.go)"
provides:
  - "send_file Deferred:true / Mutating:false tool: emits a channel-agnostic {path,filename,caption} artifact descriptor on ToolResult.Meta (D-05)"
  - "toolResultEvent Meta→Actions.ArtifactDelta lift (metaArtifact helper) — the named emit seam that first populates the forward-compat ArtifactDelta field"
  - "translator.go additive ArtifactDelta→CUSTOM(aura.artifact) AG-UI event branch (D-06, channel-agnostic)"
affects: [13-06 (telegram artifact.go sendDocument consumer), channels, multimodal]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Outbound artifact descriptor via ToolResult.Meta (mirrors shell_exec res.Meta), NOT the ask_user sentinel (name-gated to ask_user only)"
    - "Single named emit seam: toolResultEvent is the ONLY route that populates Actions.ArtifactDelta"
    - "Additive translator branch keyed on len(Actions.ArtifactDelta)>0, mirroring the STATE_DELTA close-first idiom"
    - "Namespaced AG-UI CUSTOM event (aura.artifact) as the channel-agnostic artifact carrier"

key-files:
  created:
    - internal/agent/tools/send_file.go
    - internal/agent/tools/send_file_test.go
    - internal/agent/llm_agent_events_artifact_test.go
    - internal/agui/translator_artifact_test.go
  modified:
    - internal/agent/llm_agent_events.go
    - internal/agui/translator.go
    - internal/agui/translator_test.go
    - internal/agui/testdata/golden-events.json

key-decisions:
  - "send_file uses the shell_exec outbound res.Meta seam (artifact descriptor), NOT the ask_user sentinel — the sentinel path is name-gated to ask_user (amendment #51/D-40) and would be silently dropped"
  - "ASCII-caption sanitizer is local to send_file.go: accented Latin folded to base letters, all other non-ASCII dropped (Pitfall 4 / T-13-02-CaptionInject), no channel coupling"
  - ">50MB returns a file_too_large error ToolResult (never a silent truncation, OQ3/T-13-02-Artifact); unreadable/dir/empty path returns file_unreadable; neither carries artifact Meta"
  - "Artifact AG-UI event is a namespaced CUSTOM event (aura.artifact), NOT an overloaded TOOL_CALL_RESULT (D-06 channel-agnostic)"

patterns-established:
  - "metaArtifact helper next to toolResultMetaMap/exitCodeFromMeta: pulls a typed map[string]any off ToolResult.Meta, ok=false on nil/absent/wrong-type"
  - "Artifact translator branch slotted before STATE_DELTA, close-first via closeRuns(), continue (no TEXT/TOOL/STATE for that Event)"

requirements-completed: [UX-02]

# Metrics
duration: ~28min
completed: 2026-06-08
---

# Phase 13 Plan 02: Channel-Agnostic Artifact Delivery Substrate Summary

**`send_file` Deferred tool emitting a `{path,filename,caption}` artifact descriptor on `ToolResult.Meta`, lifted onto `Actions.ArtifactDelta` by `toolResultEvent`, and mapped to a namespaced AG-UI `CUSTOM(aura.artifact)` event by an additive translator branch — Telegram-unaware end to end (D-05/D-06).**

## Performance

- **Duration:** ~28 min
- **Started:** 2026-06-08
- **Completed:** 2026-06-08
- **Tasks:** 2 (both TDD)
- **Files modified:** 8 (4 created, 4 modified)

## Accomplishments

- **`send_file` tool (Task 1):** `Deferred:true` / `Mutating:false` with a `{path, caption?}` schema + inline example. `Execute` stat-gates the path — a ≤50MB readable file returns a `ToolResult` whose `Meta` carries an `artifact` descriptor `{path, filename, caption}`; a >50MB file returns a `file_too_large` error result; an unreadable/directory/empty path returns `file_unreadable`. The caption is ASCII-sanitized. No channel is named anywhere.
- **Emit seam (Task 1):** `toolResultEvent` (`llm_agent_events.go`) now lifts the `Meta["artifact"]` descriptor onto `ev.Actions.ArtifactDelta` via a new `metaArtifact` helper. This is the single named route that populates the previously-unmapped forward-compat field; it is purely additive (a run without the key leaves `ArtifactDelta` nil, so every existing event is byte-identical).
- **Translator branch (Task 2):** an Event with non-empty `Actions.ArtifactDelta` closes any open text/reasoning run, then yields exactly one `CUSTOM` AG-UI event (`NewCustomEvent("aura.artifact", WithValue(descriptor))`) and continues — emitting no `TEXT`/`TOOL`/`STATE` for that Event. The branch mirrors the `STATE_DELTA` close-first idiom; all prior golden fixtures stay byte-identical.

## Task Commits

Each task was committed atomically (TDD: failing test → implementation in the same commit):

1. **Task 1: send_file deferred tool + Meta→ArtifactDelta emit seam** - `99397e43` (feat)
2. **Task 2: translator ArtifactDelta → custom AG-UI event branch** - `604a0bfa` (feat)

**Plan metadata:** committed with SUMMARY/STATE/ROADMAP (docs).

## Files Created/Modified

- `internal/agent/tools/send_file.go` (created, 178 LOC) — the `SendFile` Deferred tool: stat-gate, artifact descriptor on `Meta`, `file_too_large`/`file_unreadable` error results, `asciiCaption`/`foldToASCII` sanitizer.
- `internal/agent/tools/send_file_test.go` (created) — deferred-spec, three Execute cases (meta-set / >50MB / unreadable), directory/empty-path, ASCII-caption, channel-agnostic grep.
- `internal/agent/llm_agent_events.go` (modified) — `metaArtifact` helper + the `ev.Actions.ArtifactDelta` lift in `toolResultEvent`.
- `internal/agent/llm_agent_events_artifact_test.go` (created) — lift-on-present / nil-when-absent / nil-on-other-Meta-key / `metaArtifact` type guard.
- `internal/agui/translator.go` (modified, 321 LOC) — `artifactEventName` constant + the additive `ArtifactDelta`→`CUSTOM` branch.
- `internal/agui/translator_artifact_test.go` (created) — one-CUSTOM-zero-others, close-open-run, payload-verbatim, golden-shape, empty-delta-ignored.
- `internal/agui/translator_test.go` (modified) — property test now interleaves random `artifact` events under the lifecycle-balance invariants.
- `internal/agui/testdata/golden-events.json` (modified) — added the `CUSTOM` golden fixture (all prior fixtures unchanged).

## Decisions Made

See `key-decisions` frontmatter. Core choices: (1) use the `shell_exec` outbound `res.Meta` seam rather than the name-gated `ask_user` sentinel; (2) local ASCII caption sanitizer (no channel coupling); (3) error result on >50MB (never silent truncation); (4) a namespaced `CUSTOM` event, not an overloaded `TOOL_CALL_RESULT`. All decisions were pre-resolved in 13-RESEARCH (OQ1/OQ3, D-05/D-06).

## Deviations from Plan

None - plan executed exactly as written. Both tasks landed on the named seams (`shell_exec` `res.Meta` outbound, `toolResultEvent` lines 101-102 neighborhood lift, `translator.go` STATE_DELTA-branch mirror) with no architectural changes, no missing-critical additions, and no blocking fixes required.

## Issues Encountered

None. The CUSTOM event JSON shape was probed live before seeding the golden fixture (`{"type":"CUSTOM","name":"aura.artifact","value":{...}}`) to avoid a golden mismatch.

## Known Stubs

None. `send_file` is fully wired to `Meta` → `ArtifactDelta` → `CUSTOM`. The channel-side *consumer* (Telegram `artifact.go` → `sendDocument`) is explicitly out of this plan's scope — it is plan 13-06, as stated in the objective. This is a deliberate substrate/consumer split (D-06), not an unfinished stub.

## Validation

- `go test ./internal/agent/tools/ -run SendFile` — green (deferred-spec + 3 Execute cases + ASCII + dir/empty + channel-agnostic grep).
- `go test ./internal/agent/ -run 'ToolResultEvent|Artifact'` — green (Meta→ArtifactDelta lift, nil-when-absent, type guard).
- `go test ./internal/agui/ -run 'Artifact|Translate'` — green (CUSTOM branch + golden + property with artifact kind).
- `go vet ./...` + `go build ./...` — exit 0 module-wide.
- `go test -race ./internal/agent/... ./internal/agui/` — race-clean.
- `golangci-lint run ./internal/agent/tools/ ./internal/agent/ ./internal/agui/` — 0 issues.
- No "telegram" reference in `send_file.go` or `llm_agent_events.go` (D-06 channel-agnostic, grep-clean).
- File sizes: `send_file.go` 178 LOC, `llm_agent_events.go` 222 LOC, `translator.go` 321 LOC — all ≤600.

## Next Phase Readiness

The artifact substrate is complete and channel-agnostic. The `CUSTOM(aura.artifact)` event with a `{path,filename,caption}` value is ready for:
- **13-06** Telegram `artifact.go` — the consumer that keys on the `aura.artifact` name and renders `tele.Document{File: FromDisk(path), FileName: filename}` → `sendDocument`.
- **CLI / AG-UI HTTP** — render-or-pass-through consumers, no substrate change needed.

No blockers. `send_file` is NOT yet registered into `buildBaseRegistry` — registration into the live channel registry happens when the Telegram channel is wired (Wave 2, 13-05/13-06); the tool + emit + translator substrate built here is the dependency those plans consume.

## Self-Check: PASSED

All created files exist on disk; both task commits (`99397e43`, `604a0bfa`) are in the git log.

---
*Phase: 13-channels-telegram-multimodal*
*Completed: 2026-06-08*
