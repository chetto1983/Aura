---
phase: 26-typed-display-protocol-router
plan: 01
subsystem: api
tags: [go, ag-ui, display-protocol, normalizer, citations, sse, trust-boundary]

# Dependency graph
requires:
  - phase: 12-agui-gateway
    provides: "aura.artifact CUSTOM-event twin pattern (translator.go) + Actions struct omitempty-pointer idiom"
  - phase: 05-web-tools
    provides: "web.Result/ResultMetadata, web.Page, web.WebError sanitized enum (the normalizer inputs)"
  - phase: 09-swarm
    provides: "swarm.ChildReport + Status enum (mirrored as display.ChildReport)"
provides:
  - "internal/agent/display: the typed-display trust boundary (Normalize dispatch + per-tool normalizers + URL-keyed source registry)"
  - "display.Payload tagged-union wire contract (the shape 26-02 mirrors in web/src/chat/displays/types.ts)"
  - "Actions.Display additive omitempty slot on the agent event"
  - "DisplayEventName + the aura.display CUSTOM branch in the AG-UI translator"
affects: [26-02 frontend router, 26-03 source tail-inject + replay, 26-05 source explorer, 26-06 image-proxy]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Trust boundary: only internal/agent/display turns an untrusted tool result into a rich-renderable Payload (HARDEN-08); unrecognized tools return (Payload{}, false) for the raw-card fallback (D-FALLBACK)"
    - "Additive CUSTOM-event twin: aura.display mirrors aura.artifact (DisplayEventName + Actions.Display slot + one Translate branch), no text/tool/reasoning lifecycle change"
    - "Redeclare-to-break-a-cycle: display.ChildReport/Status* mirror swarm with byte-identical JSON tags (swarm imports agent, agent imports display)"
    - "URL-keyed per-turn source registry: stable RefID/Index, cited-vs-consulted, numbered preview list (D-05)"

key-files:
  created:
    - internal/agent/display/doc.go
    - internal/agent/display/payload.go
    - internal/agent/display/normalize.go
    - internal/agent/display/web.go
    - internal/agent/display/code.go
    - internal/agent/display/swarm.go
    - internal/agent/display/systemevent.go
    - internal/agent/display/sources.go
    - internal/agui/translator_display_test.go
  modified:
    - internal/agent/event.go
    - internal/agui/translator.go
    - internal/agui/testdata/golden-events.json

key-decisions:
  - "display.ChildReport is a wire-identical local mirror of swarm.ChildReport, NOT an import — importing swarm would close an agent->display->swarm->agent cycle (Rule 3 deviation)"
  - "The WebError->system_event check runs BEFORE the per-tool result type-switch in NormalizeWithRegistry: a tool error arrives under any tool name, so a web_fetch error must still render its sanitized system_event (Rule 1 deviation)"
  - "Normalize builds a fresh per-call Registry; NormalizeWithRegistry takes a caller-owned per-turn Registry for cross-call source accumulation (26-03 will thread this)"
  - "severityFor defaults to 'error' for an unmapped web code (fail-safe — unknown class treated as a hard block)"

patterns-established:
  - "Pattern 1: per-tool normalizer = func(toolCallID, typed-result, *Registry) (Payload, bool); dispatch type-switches on toolName"
  - "Pattern 2: system_event consumes ONLY the sanitize()-classified reason or the swarm Status enum — never free-form error text (no SSRF-internals leak)"

requirements-completed: [DISP-01, DISP-04, DISP-05, SWARM-01]

# Metrics
duration: 31min
completed: 2026-06-18
---

# Phase 26 Plan 01: Typed-Display Protocol Backend Trust Boundary Summary

**A pure-data `internal/agent/display` package that normalizes each in-scope tool result into a typed `DisplayPayload` union, an additive `Actions.Display` slot, the `aura.display` CUSTOM translator branch (twin of `aura.artifact`), and the URL-keyed source registry (cited vs consulted) that powers downstream citations.**

## Performance

- **Duration:** ~31 min
- **Started:** 2026-06-18T11:33:00Z
- **Completed:** 2026-06-18T12:05:00Z
- **Tasks:** 3
- **Files modified:** 21 (9 source + golden fixture + 11 tests; 2 existing files touched)

## Accomplishments
- The `display.Payload` flat tagged union + 8 `Kind` wire constants + per-type structs (`WebItem`/`Document`/`Code`/`Artifact`/`Table`/`Chart`/`System`/`Source`), with decode(encode)==identity and a frontend-literal pin test.
- The `Normalize` dispatch + per-tool normalizers: `web_search`→`web_result` (domain + metadata tier + URL-keyed RefIDs), `web_fetch`→`document`, `swarm_spawn`→`swarm_report`, `shell/sandbox`→`code`/`local_artifact`, any tool's error→`system_event`. Unrecognized tool / wrong result type → `(Payload{}, false)` (D-FALLBACK).
- The URL-keyed source `Registry` (stable cross-turn RefIDs, fragment/trailing-slash collapse, cited-vs-consulted, numbered `[n] Title — url` renderer).
- `system_event` proven to leak no SSRF internals (test asserts the rendered reason carries no IP/host/redirect token across every web-safety code).
- The additive `Actions.Display` omitempty slot + `DisplayEventName` + the `aura.display` CUSTOM `Translate` branch, with a golden fixture and a nil-Display regression guard. Package coverage 98.9%, race-clean.

## Task Commits

Each task was committed atomically (TDD test + impl folded per task — the tests pin pure-data contracts):

1. **Task 1: DisplayPayload union + Actions.Display slot** - `1c9ab360` (feat)
2. **Task 2: Per-tool normalizers + URL-keyed source registry** - `d0279215` (feat)
3. **Task 3: DisplayEventName + aura.display CUSTOM branch + golden fixture** - `5c44b2f9` (feat)

## Files Created/Modified
- `internal/agent/display/doc.go` - package + trust-boundary contract (HARDEN-08)
- `internal/agent/display/payload.go` - `Payload` union, `Kind` constants, per-type structs, `ChildReport` mirror, `Source`
- `internal/agent/display/normalize.go` - `Normalize` / `NormalizeWithRegistry` dispatch
- `internal/agent/display/web.go` - `normalizeWebSearch` / `normalizeWebFetch`
- `internal/agent/display/code.go` - `normalizeCode` (text→code, file→local_artifact) + `CodeInput`
- `internal/agent/display/swarm.go` - `Status*` constants + `normalizeSwarm`
- `internal/agent/display/systemevent.go` - `normalizeWebError` / `normalizeSwarmStatus` + severity map
- `internal/agent/display/sources.go` - the `Registry` + `RenderSourceList`
- `internal/agent/event.go` - `Actions.Display *display.Payload` omitempty slot + import
- `internal/agui/translator.go` - `DisplayEventName` + the aura.display CUSTOM branch
- `internal/agui/testdata/golden-events.json` - `CUSTOM_DISPLAY` fixture (additive)
- 11 `*_test.go` - table/golden/security tests (display pkg + agent event + agui translator)

## DisplayPayload wire shape (for 26-02 to mirror in `web/src/chat/displays/types.ts`)

```jsonc
// Payload
{
  "type": "web_result|document|code|local_artifact|table|chart|system_event|swarm_report",
  "tool_call_id": "string",            // sseAdapter correlation key
  "title": "string?",
  "web_results": [WebItem]?,           // type=web_result
  "document": Document?,               // type=document
  "code": Code?,                       // type=code
  "artifact": Artifact?,               // type=local_artifact
  "table": Table?,                     // type=table
  "chart": Chart?,                     // type=chart
  "system": System?,                   // type=system_event
  "swarm": [ChildReport]?,             // type=swarm_report
  "sources": [Source]?                 // the per-turn registry (any web-consulting payload)
}

// WebItem
{ "title": string, "url": string, "snippet": string?, "domain": string?,
  "score": number?, "published_at": string?, "thumbnail": string?, "ref_id": string? }

// Document  { "title": string?, "url": string?, "content_md": string }
// Code      { "body": string, "lang": string? }
// Artifact  { "filename": string, "size_bytes": number?, "path": string? }
// Table     { "columns": [string], "rows": [[string]] }
// Chart     { "x_labels": [string], "y_values": [number], "x_axis_label": string? }
// System    { "class": string, "reason": string?, "message": string?, "severity": "error|warning" }

// ChildReport (wire-identical to swarm.ChildReport)
{ "goal_index": number, "child_id": string, "status": "ok|failed|needs_user_input",
  "summary": string?, "error": string?, "question": string?, "options": [string]?, "tool_call_id": string? }

// Source (registry entry, D-05)
{ "ref_id": string, "index": number, "type": Kind?, "title": string?, "url": string?,
  "snippet": string?, "confidence": number?, "cited": bool }
```

The CUSTOM event is `{ "type":"CUSTOM", "name":"aura.display", "value": <Payload> }` — the sseAdapter attaches `value` to the tool part by `value.tool_call_id`.

## Decisions Made
- See `key-decisions` frontmatter. Headline: `display.ChildReport` is a local mirror (cycle break), and the error-check precedes the per-tool type-switch in the dispatch.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Mirror swarm.ChildReport instead of importing it (import cycle)**
- **Found during:** Task 1 (Payload union)
- **Issue:** The plan's must_have `Swarm []swarm.ChildReport` requires `display` to import `swarm`. But `swarm` imports `internal/agent`, and `event.go` (package `agent`) imports `display` — `agent → display → swarm → agent` is a compile-breaking cycle.
- **Fix:** Declared `display.ChildReport` + `Status*` constants as a wire-identical local mirror (byte-for-byte JSON tags), the same redeclare-to-break-a-cycle idiom `agent.PauseOption` already uses for `tools.Option`. The wire shape and every ChildReport field are preserved, so SWARM-01/D-08 are fully satisfied; only the Go type identity differs.
- **Files modified:** internal/agent/display/payload.go, internal/agent/display/swarm.go
- **Verification:** `go build ./...` clean (no cycle); `TestNormalizeSwarm` asserts every field survives.
- **Committed in:** 1c9ab360 / d0279215

**2. [Rule 1 - Bug] Hoist the WebError check above the per-tool type-switch**
- **Found during:** Task 2 (dispatch)
- **Issue:** A `web_fetch` error (`*web.WebError`) hit `case "web_fetch"`, failed the `web.Page` type-assert, and returned `(Payload{}, false)` — the error never reached the system_event normalizer. A tool error can arrive under any tool name.
- **Fix:** Moved the `*web.WebError` check to the top of `NormalizeWithRegistry`, before the per-tool switch. An error result now normalizes to `system_event` regardless of which tool produced it.
- **Files modified:** internal/agent/display/normalize.go
- **Verification:** `TestNormalizeDispatch/web_error` passes (was failing); full package green.
- **Committed in:** d0279215

---

**Total deviations:** 2 auto-fixed (1 blocking cycle, 1 dispatch bug)
**Impact on plan:** Both essential for a building, correct package. No scope creep — the wire contract and all four requirements are delivered exactly as specified.

## Issues Encountered
- Pre-existing uncommitted working-tree changes (`internal/webui/dist/*`, `web/src/chat/*`, `.planning/STATE.md`) from a prior/parallel session are OUT OF SCOPE (frontend = 26-02's lane). They were left untouched; only this plan's 21 backend/test files were staged per-commit.

## Known Stubs
None. Every normalizer is fully wired to its real input type; no placeholder/empty-value flows to a rendered surface. (The frontend router that consumes these payloads is 26-02's scope, not a stub of this plan.)

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- The `display.Payload` wire contract is frozen and documented above — 26-02 can mirror it in `web/src/chat/displays/types.ts` and add the `CUSTOM`/`aura.display` reducer frame.
- 26-03 can thread a per-turn `*display.Registry` through `NormalizeWithRegistry` for the numbered source-list tail-inject (D-05) and re-run the same normalizer at snapshot projection for replay (D-06).
- No blockers.

## Self-Check: PASSED

All 12 created/modified files exist on disk; all 3 task commits (`1c9ab360`, `d0279215`, `5c44b2f9`) are present in git history.

---
*Phase: 26-typed-display-protocol-router*
*Completed: 2026-06-18*
