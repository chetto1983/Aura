---
phase: 46-mcp-trust-and-facade
plan: 06
subsystem: mcp-bridge
tags: [multiplexed, per-action-risk, classifier, image-pin, tracer-slice, d-21, d-32, d-34, d-35, mcp-04, mcp-02]

# Dependency graph
requires:
  - phase: 46-mcp-trust-and-facade (plan 04)
    provides: "The frozen-at-mount deferral arithmetic this plan's Multiplexed flip sits beside — the curated calendar tool is 1 model-facing tool, so it now clears TOOL-14's <=3 ceiling where the raw 14 never could"
  - phase: 46-mcp-trust-and-facade (plan 05)
    provides: "The curated fork itself: one `calendar` tool with a 14-value `action` enum, and the immutable ghcr.io/chetto1983/aura-pim-mcp:38c94fd9d22d85c4b89f3d5b1f8202970faed117 tag this plan pins"
provides:
  - "internal/agent/mcptools/bridge_multiplex.go — CalendarMultiplexedToolName, MultiplexedMCPTools(), MCPActionClassFor(), the isKnownMultiplexedMCPTool gate, and mount-time reconcileCuratedActions"
  - "internal/gateway classifyCalendarAction + the multiplexedClassifiers entry, so one tool classifies at four different tiers by action"
  - "trustedRecipeActions[calendarRecipeSource] read as ACTION names, with the mixed-key spaces documented on the table itself (D-35)"
  - "The calendar sidecar pinned to the curated image at all six sites, in the same commit as the table re-key (D-32)"
affects: [46-07, 46-08, 46-09]

actuals:
  tokens: 0
  tasks: 3
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Allowlist-gated capability flag: Multiplexed is set only for a namespaced name Aura already has a classifier for (isKnownMultiplexedMCPTool), never inferred from schema shape — a stranger's server carrying an `action` argument boots cleanly at the generic fail-closed tier instead of panicking ValidateClassifiable."
    - "Bidirectional mount-time reconciliation: an action in the fork's enum with no risk-table entry, and a table entry the fork no longer advertises, each WARN by name and neither panics boot — drift is reported, never silently absorbed."
    - "One risk source, two key spaces, documented in place: the same trustedRecipeActions table is action-keyed for calendar and raw-tool-name-keyed for memory/whatsapp, with the asymmetry written on the table rather than left for a reader to infer."

key-files:
  created:
    - internal/agent/mcptools/bridge_multiplex.go
    - internal/agent/mcptools/bridge_multiplex_test.go
    - internal/gateway/classify_multiplexed_comms_test.go
  modified:
    - internal/agent/mcptools/bridge.go
    - internal/agent/mcptools/bridge_risk.go
    - internal/agent/mcptools/bridge_risk_test.go
    - internal/gateway/classify.go
    - internal/gateway/guard_test.go
    - internal/mcp/calendar_integration_test.go
    - compose.yaml
    - .github/workflows/ci.yml
    - docker/aura/PROVENANCE.md
    - .planning/codebase/INTEGRATIONS.md
  deleted: []
---

# Plan 46-06 — The tracer slice: calendar curated end to end

## Performance

| Metric | Value |
|---|---|
| Tasks | 3 |
| Commits | 2 (one RED, one atomic GREEN+pin per D-32) |
| Files changed | 12 |
| Largest touched file | `internal/agent/mcptools/bridge.go` at 518 LOC — inside the 600 rule |

## Accomplishments

The silent failure D-21 named is closed. Before this plan a bridged tool never set `Multiplexed`,
so `ValidateClassifiable` skipped its per-action assertion and `classify` handed ONE flat tier to
the whole merged tool — `calendar(action=list_calendars)` and `calendar(action=send_email)` scored
identically, so either every read demanded approval or `send_email` ran un-gated, with no panic to
warn anyone. Now:

- `bridge_multiplex.go` carries the curated-tool name constant, the exported per-action class
  lookup (`MCPActionClassFor`), the allowlist (`MultiplexedMCPTools`), the `isKnownMultiplexedMCPTool`
  gate and `reconcileCuratedActions`.
- `bridge.go:211` sets `spec.Multiplexed = isKnownMultiplexedMCPTool(name)` — D-34's rule, so the
  flag is earned by having a classifier, never inferred from a schema carrying an `action` property.
- `internal/gateway/classify.go` registers `classifyCalendarAction` in `multiplexedClassifiers`,
  keyed by the namespaced tool name. `gateway` imports `mcptools`; `mcptools` never imports
  `gateway`, so no cycle.
- `trustedRecipeActions[calendarRecipeSource]`'s 14 keys are read as action names. No relabeling
  was needed — the fork's curated enum values already equal the keys 46-05 shipped.
- The image pin moved from the pre-curation `:10383276…` to the curated
  `:38c94fd9d22d85c4b89f3d5b1f8202970faed117` at all six occurrences across four files, in the
  same commit as the table re-key.

## Task Commits

| Task | Commit | Subject |
|---|---|---|
| 1–2 (RED) | `09e87b8d5` | test(46-06): add failing tests for calendar per-action risk classification |
| 3 (GREEN + pin, atomic per D-32) | `2edbc3910` | feat(46-06): curate calendar into one multiplexed tool, classify per action, pin the sidecar |

## Files Created/Modified

- `internal/agent/mcptools/bridge_multiplex.go` *(created, 138 LOC)* — name constants, allowlist,
  `MultiplexedMCPTools()`, `MCPActionClassFor()`, `isKnownMultiplexedMCPTool()`,
  `curatedActionEnum()`, `reconcileCuratedActions()`
- `internal/agent/mcptools/bridge_multiplex_test.go` *(created, 197 LOC)*
- `internal/gateway/classify_multiplexed_comms_test.go` *(created, 178 LOC)*
- `internal/agent/mcptools/bridge.go` — the D-34 `Multiplexed` gate and the mount-time reconcile call
- `internal/agent/mcptools/bridge_risk.go` — the D-35 mixed-key comment on the table itself
- `internal/gateway/classify.go` — `classifyCalendarAction` + the `multiplexedClassifiers` entry
- `internal/mcp/calendar_integration_test.go` — extended to the curated one-tool surface
- `compose.yaml`, `.github/workflows/ci.yml` (×2), `docker/aura/PROVENANCE.md`,
  `.planning/codebase/INTEGRATIONS.md` (×2) — the pin, all in the atomic commit

## Acceptance Criteria — measured

| must_have | Verdict | Evidence |
|---|---|---|
| Curated tool carries `Multiplexed: true`, Aura boots without panicking | ✅ | `bridge.go:211`; `go build ./...` and `go vet ./...` exit 0 |
| A server with an `action` property but no classifier does NOT get Multiplexed inferred | ✅ | `TestMultiplexedNotInferredFromSchemaShape` PASS |
| Read action → `Safe`, `send_email` → `Destructive`, same tool | ✅ | `TestClassifyCalendarActionTiers` PASS (read / mutate / destructive_send / destructive_respond) |
| Unknown, empty, absent, unparseable action → `Risky`, never Safe/Normal | ✅ | `TestClassifyCalendarActionFailsSafe` PASS (5 subtests incl. nil args) |
| Table action-keyed for calendar, raw-name-keyed for memory, documented on the table | ✅ | `bridge_risk.go:36-47` comment; calendar's 14 keys are action names |
| Exactly one risk source — no second table, no annotation lowering a tier | ✅ | One `trustedRecipeActions`, one lookup site; escalate-only preserved |
| Every `MultiplexedMCPTools()` name has exactly one `multiplexedClassifiers` entry | ✅ | `TestEveryMultiplexedMCPToolHasAClassifier` PASS — fails if either side is added to alone |
| Mount WARNs both directions on action/table drift, never panics | ✅ | `TestCuratedActionReconciliationWarnsBothDirections` PASS |
| compose default is the immutable 40-hex tag, not `:sidecar` | ✅ | `compose.yaml:1052` |
| EVERY pin site carries the SAME new tag in the SAME commit | ✅ | `grep` for the old sha across all four files returns zero; six new-sha sites all in `2edbc3910` |
| Pin + re-key + Multiplexed flip + classifier land in ONE commit | ✅ | `2edbc3910` |
| classify idempotent, race-free under concurrency | ✅ | `go test -race` green on both packages (WSL, CGO_ENABLED=1) |
| Two servers can never register the same model-facing name | ✅ | namespacing + `Registry.Register` panic-on-duplicate, unchanged |

**Gates run:** `go build ./...` 0 · `go vet ./...` 0 · `go test ./internal/agent/mcptools/ ./internal/gateway/` ok ·
`go test -race` ok in WSL (`CGO_ENABLED=1`; Windows `-race` needs cgo and is unavailable here per CLAUDE.md).

**Live proof (beyond the plan's ask):** the `aura-pim-mcp` service was recreated onto the new pin
and probed directly — `tools/list` returns exactly ONE tool, `calendar`. The `calendar_integration`
tier was then executed live against it (`AURA_PIM_MCP_URL=http://127.0.0.1:8093/`,
`-tags=calendar_integration`): `TestCalendarServerLive` PASS, negotiated protocol `2025-11-25`.

## Deviations from Plan

1. **Executed inline by the orchestrator, not by the dispatched executor.** Three gsd-executor
   dispatches were killed by the harness's 600s stream-idle watchdog (the same failure that killed
   three executors in Phase 45.1). Both plan commits are the executor's work, made across resumes;
   the orchestrator ran the verification gates, recreated the sidecar, ran the live tier, and wrote
   this SUMMARY after the operator directed it to finish inline. No plan content was weakened to
   finish.
2. **`docker/aura/PROVENANCE.md:11` prose corrected alongside the pin.** It described the fork's
   surface as "tool surface trimmed 29→14", which 46-05 made false. Corrected in the same edit —
   leaving it would have left a stale claim in the provenance record.

## Issues Encountered

- **The MCP-05 round-trip is only half-proven.** `TestCalendarServerLive` reports it plainly:
  `get_calendar_events` returned no `eventId`/`calendarId` to chain from, because this host has
  **zero connected accounts**. The schema half of MCP-05 is exercised; the opaque-reference
  round-trip is not. This is a fixture limitation, not a code gap — and it is exactly what plan
  46-07's live scenario has to close.
- **Inherited from 46-05, still open:** the fork deleted 14 MSTest files (2,062 LOC) with no
  replacement, and its `ci.yml` never triggers on `aura/pim-sidecar`. This
  `calendar_integration` tier is now the only automated proof the curated surface behaves.
- **Aggregate coverage gate not run.** Per phase precedent (46-04, 45.1), `scripts/coverage_docker.sh`
  is deferred to phase close (46-09 Task 4), which owns it.

## Next Phase Readiness

46-07 can run: the sidecar is live on the curated pin, advertises one tool, and Aura classifies its
actions at four tiers. 46-07 must connect at least one real account, or the MCP-05 round-trip stays
unproven for the whole phase. 46-08 repeats this exact pattern for WhatsApp — with the difference
that its two view-bound tools stay raw and advertised (46-02's views-exempt decision), so WhatsApp
ends at 3 model-facing tools, not 1.

## Self-Check: PASSED
