---
phase: 46-mcp-trust-and-facade
plan: 01
subsystem: docs
tags: [prd, mcp, trust, tool-tiering, env-catalogue]

# Dependency graph
requires:
  - phase: 45.1-native-mcp-client
    provides: SDK-based MCP client, _meta identity, escalate-only risk fallback, decline-and-surface elicitation — all ratified in prose by Amendment #124
provides:
  - "prd.md Amendment #122 — ratifies commit 34b892512 (MCP-01/02/03 trust posture), MCP-04's fork-side curation mechanism (two always-loaded slots, not one Aura-side facade), and MCP-05's fix (fork accepts its own opaque handle instead of host-injected accountId)"
  - "prd.md Amendment #123 — TOOL-14's tiering axis change from size to frequency + hard count budget (<=3 model-facing tools/server, global cap 2 slots, overflow fails closed), superseding A4 by name and extending #44 by name"
  - "prd.md Amendment #124 — ratifies Phase 45.1's shipped-but-un-amended changes in prose; repairs the AURA_MCP_* env catalogue (13 new rows, 1 dead row deleted)"
affects: [46-02-fork-curation-design, 46-03-roadmap-worked-example, 46-04, 46-05, 46-06, 46-08]

actuals:
  tokens: 5100
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "PRD amendment house style (heading + What was measured / What changes / What this measurement does NOT prove), reused verbatim from Amendment #121"
    - "Amendment numbers resolved at landing time from `grep -o \"Amendment #[0-9]\\+\" prd.md | sort -n | tail -1`, never copied from a planning document"

key-files:
  created: []
  modified:
    - prd.md

key-decisions:
  - "Amendment numbers 122/123/124 assigned (next free integers after #121, contiguous, no reuse)"
  - "MCP-04's curated surface is stated as 'two slots', not 'two tools' — per-fork tool count is explicitly left to plan 46-02, not pre-empted here"
  - "D-27's count rule (<=3 model-facing tools/server, global cap 2) recorded inside TOOL-14's amendment (#123) rather than as its own amendment, per D-29"
  - "The dead AURA_MCP_PING_INTERVAL_SEC catalogue row and its retired source file are described in Amendment #124 without ever reproducing either literal string, satisfying the plan's own machine verification that neither string survives in prd.md"

patterns-established:
  - "A PRD amendment that ratifies already-shipped code names every surviving guardrail explicitly (never leaves a removed defense implicit) and always carries a scope boundary paragraph"

requirements-completed: [MCP-01, MCP-02, MCP-03, MCP-04, MCP-05, TOOL-14]

coverage:
  - id: D1
    description: "prd.md ratifies 34b892512 (MCP-01/02/03), records MCP-04's fork-side curation mechanism and MCP-05's accountId fix, in Amendment #121's house style, with a stated scope boundary (Amendment #122)"
    requirement: "MCP-01"
    verification:
      - kind: other
        ref: "bash -c 'grep -q \"34b892512\" prd.md && grep -qE \"^## §.*\\(Amendment #[0-9]+, 2026-08-17\\)\" prd.md && grep -q \"What this measurement does NOT prove\" prd.md && echo OK' -> OK"
        status: pass
    human_judgment: false
  - id: D2
    description: "TOOL-14's amendment supersedes A4 by name, extends #44 by name, changes prd.md:154's tiering axis from size to frequency + hard count budget, and states the <=3/cap-2 numbers as code constants (Amendment #123)"
    requirement: "TOOL-14"
    verification:
      - kind: other
        ref: "bash -c 'grep -q sandbox_exec prd.md && grep -q read_tool_output prd.md && git diff --unified=0 prd.md | grep -q \"Tool grandi\" && echo OK' -> OK"
        status: pass
    human_judgment: false
  - id: D3
    description: "prd.md no longer documents the deleted MCP ping-poller and catalogues every live AURA_MCP_* env var (13 new rows); 45.1's shipped changes are ratified in prose (Amendment #124)"
    requirement: "MCP-02"
    verification:
      - kind: other
        ref: "bash -c '! grep -q \"AURA_MCP_PING_INTERVAL_SEC\" prd.md && grep -q \"AURA_MCP_ELICITATION_TIMEOUT_SEC\" prd.md && ! grep -q \"bridge_ping.go\" prd.md && echo OK' -> OK"
        status: pass
    human_judgment: false

duration: 35min
completed: 2026-08-22
status: complete
---

# Phase 46 Plan 01: PRD amendment batch (MCP trust, TOOL-14 tiering, 45.1 ratification) Summary

**Three dated prd.md amendments (#122/#123/#124) that ratify the already-shipped MCP trust posture, record MCP-04/05's fork-side mechanism, change the tool-tiering axis from size to frequency + a hard count budget, and repair the AURA_MCP_* env catalogue — unblocking every code plan in Phase 46.**

## Performance

- **Duration:** ~35 min
- **Started:** ~2026-08-22T13:10:00Z (estimated — extensive required-reading phase preceded the first commit)
- **Completed:** 2026-08-22T13:46:17Z
- **Tasks:** 3
- **Files modified:** 1 (`prd.md`)

## Accomplishments

- **Amendment #122** (`## §MCP trust and curated surface`) ratifies commit `34b892512`: MCP tool descriptions/summaries reach the model as plain text (MCP-01/03), per-call result fencing for MCP is removed with the surviving guardrails named explicitly (`mcpToolRisk`'s fail-closed default, `unsafeToRepeatBeyondAura`'s escalate-only escalation, the model-blind approval gate, `Registry.Register`'s panic-on-duplicate, `capSchemaDescriptions`' byte caps), and Amendment #110's "non-persisted, untrusted reference item" memory-block wording is amended (not restored) while `prd.md`'s document-passage `TrustUntrusted` line stays untouched. Records MCP-04's mechanism as **two curated always-loaded slots** (not tools) living in the forks, and MCP-05's fix as the fork accepting its own opaque handle instead of a host-injected `accountId` (spec basis: MCP has no `accountId` concept; identity is the OAuth token, RFC 8707 audience-bound).
- **Amendment #123** (`## §Tool tiering axis`) changes `prd.md:154`'s axis from size to **frequency plus a hard count budget**, superseding amendment A4 by name (`read_tool_output`'s size-based non-deferred tier) and extending amendment #44 by name (`sandbox_exec`'s non-deferral is now an instance of the frequency axis, not a size exception). States D-27's count rule numerically: a server exposing `<=3` model-facing tools earns an always-loaded slot, global cap 2 slots, overflow fails closed to `Deferred: true` — both numbers are code constants, never env vars.
- **Amendment #124** (`## §Phase 45.1 ratification and the AURA_MCP_* catalogue`) deletes the dead `AURA_MCP_PING_INTERVAL_SEC` catalogue row (describing a poller and source file Phase 45.1 deleted) and adds 13 catalogue rows for live `AURA_MCP_*` env vars the tree already reads but `prd.md` never catalogued (mount/shutdown/call/elicitation timeouts, mount-retry attempts, config path, sandbox origin, legacy servers-json escape hatch, network allowlist, whatsapp bridge URL override, SSRF enforce, probe timeout). Ratifies 45.1's shipped `_meta.aura.user_identifier` identity cutover, the escalate-only fallback risk branch, and the decline-and-surface elicitation handler.
- Updated the adjacent "Nota MCP server registration" paragraph, whose claim that "the only MCP env vars are the test-tier `AURA_MCP_*_SERVER_JSON` overrides" was contradicted the moment the 13 new rows landed; it now names the 7 genuinely test-tier vars explicitly (`AURA_MCP_CALCULATOR_SERVER_JSON`, `AURA_MCP_WHATSAPP_SERVER_JSON`, `AURA_MCP_HELPER`, `AURA_MCP_HELPER_MODE`, `AURA_MCP_HELPER_TOOLS`, `AURA_MCP_SDK_HELPER`, `AURA_MCP_SDK_HELPER_TOOLS`).

## Task Commits

Each task was committed atomically:

1. **Task 1: The decision-bearing amendment** — `245e9d9d4` (docs)
2. **Task 2: The TOOL-14 amendment** — `73bc1ab44` (docs)
3. **Task 3: Ratify Phase 45.1 and repair the AURA_MCP_* env catalogue** — `bf7024124` (docs)

_No separate plan-metadata commit — this SUMMARY, STATE.md and ROADMAP.md land in the final phase-tracking commit (see below)._

## Files Created/Modified

- `prd.md` — three new dated amendments appended (§MCP trust and curated surface #122, §Tool tiering axis #123, §Phase 45.1 ratification and the AURA_MCP_* catalogue #124); `prd.md:154`'s tiering-axis line amended in place with a struck-through dated marker; the `AURA_MCP_*` env catalogue table gained 13 rows and lost 1 dead row; the adjacent MCP-server-registration note corrected

## Decisions Made

- Amendment numbers resolved at landing time via `grep -o "Amendment #[0-9]\+" prd.md | sort -n | tail -1` → next free integers 122/123/124, confirmed contiguous post-landing with no collision.
- MCP-04's curated surface is stated in the plural as **slots**, not tools — the plan's own `assumption_delta_decision` (`promote`) required this; the exact per-fork tool count is explicitly deferred to plan 46-02's design doc, never pre-empted here.
- D-27's hard count budget (`<=3` model-facing tools/server, global cap 2) was recorded inside TOOL-14's amendment (#123) rather than as a fourth standalone amendment, per D-29 — one statement of the tiering axis, Phase 48 inherits both halves from one place.
- The dead `AURA_MCP_PING_INTERVAL_SEC` env var and its deleted source file are described in Amendment #124 using paraphrase only ("the background per-server MCP liveness-poll-interval env var", "the bounded poll loop... deleted outright") — the plan's own `<verify>` block demands `grep -q "AURA_MCP_PING_INTERVAL_SEC" prd.md` and `grep -q "bridge_ping.go" prd.md` both fail, so neither literal string appears anywhere in `prd.md` after this plan, even while explaining what was removed and why.
- Every code citation (file:line) in all three amendments was independently re-verified against the live tree at commit `807cbfb10` before being written — `bridge_call.go:64-80`, `bridge.go:319-353`/`27-31,215-235`/`406-441`, `bridge_risk.go:108-155`/`136-138`/`205-207`, `internal/agent/tools/spec.go:183-189`, `internal/agent/tools/testdata/deferred_manifest.json:69,151-153,170,186`, `internal/agent/mcptools/bridge_memory.go:27-29`, and the pinned SDK's `Tool`/`ToolAnnotations` structs at `go-sdk@v1.7.0 mcp/protocol.go:1898-1948,1967-1992` (confirmed no priority/always-load field exists) — none were copied from CONTEXT.md's line numbers without re-checking, since some had drifted slightly from the 2026-08-17 measurement pass.

## Deviations from Plan

None — plan executed exactly as written. The only adjustment was cosmetic: the two literal strings the plan's own `<verify>` block forbids (`AURA_MCP_PING_INTERVAL_SEC`, `bridge_ping.go`) were initially drafted into Amendment #124's prose (a natural way to explain a deletion) and then rewritten as paraphrase before commit, once the verification script's exact requirement was re-read. This is not a deviation from the plan's *intent* — the plan's acceptance criteria and its `<verify>` block already specified this outcome; it was caught by running the verification locally before committing, exactly as the plan's own `<verify>` step requires.

## Issues Encountered

None. `go build ./...` and `go vet ./...` stayed green throughout (no code touched); every task's automated `<verify>` script and every acceptance-criteria bullet was checked with `grep`/`git diff` before its commit.

## User Setup Required

None — no external service configuration required. Pure documentation change.

## Next Phase Readiness

- All three amendment numbers (#122, #123, #124) are landed, contiguous, and citable by plan 46-02 (fork curation design), 46-03 (ROADMAP worked example), and every later code plan in this phase.
- `git diff --stat 807cbfb10 HEAD` confirms `prd.md` is the only file touched across all three commits — no code, test, or other planning-doc drift.
- Plan 46-02's blocking one-way checkpoint (the WhatsApp two-view exemption decision) is unaffected by this plan; Amendment #122(d) deliberately states the curated-surface rule as "slots" rather than "tools" so it does not pre-empt that decision.

## Self-Check: PASSED

- FOUND: `.planning/phases/46-mcp-trust-and-facade/46-01-SUMMARY.md`
- FOUND: `245e9d9d4` (Task 1 commit)
- FOUND: `73bc1ab44` (Task 2 commit)
- FOUND: `bf7024124` (Task 3 commit)
