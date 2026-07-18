---
phase: 38-mcp-governance-hardening
plan: 01
subsystem: mcp
tags: [mcp, classify, trust, transport, governance, validation, security]

# Dependency graph
requires: []
provides:
  - "internal/mcp/classify.go — Classify(ManagedServer) (serverType, trust string, err error): the single canonical transport+trust classifier (D-01, MCPH-01)"
  - "Mixed url+command entries rejected before any stdio Open (D-02, closes F-027) at validateManagedServers + OpenServer"
  - "Remote server with unset/blank/whitespace/unknown trust resolves to TrustBlocked, never auto-promoted to TrustRemoteHTTP (D-03, closes F-013)"
  - "Explicit type<->trust consistency matrix enforced (checkTypeTrustConsistency): stdio+remote_http and streamable_http+{trusted_local,sandboxed_local} are hard errors"
affects: [38-04, 38-05, 38-06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Single-source-of-truth classifier: scattered per-site transport/trust decisions collapse onto one Classify function; call sites become thin wrappers"
    - "Fail-closed classification: ambiguous/inconsistent server shapes are hard errors, not silent resolutions"

key-files:
  created:
    - internal/mcp/classify.go
    - internal/mcp/classify_test.go
  modified:
    - internal/mcp/managed_config.go
    - internal/mcp/managed_config_test.go
    - internal/mcp/transport.go
    - internal/mcp/transport_test.go

key-decisions:
  - "D-01: Promote Classify to the single source of truth; normalizedServerType/NormalizedTrust become thin wrappers, their standalone decision bodies retired."
  - "D-02: A mixed url+command entry (no explicit type) is rejected outright — it must never reach a stdio subprocess spawn (closes F-027)."
  - "D-03: Unset/blank/whitespace/unknown remote trust always resolves to TrustBlocked; the streamable_http->TrustRemoteHTTP auto-promote fallback is deleted (closes F-013)."
  - "A1 (locked): the explicit type<->trust matrix (stdio+remote_http, streamable_http+{trusted_local,sandboxed_local}) is locked as a hard error; revisit via PRD-amendment if a legit use case surfaces."

patterns-established:
  - "Classify-first dispatch: OpenServer and validateManagedServers call Classify before acting; a Classify error short-circuits before any side effect."
  - "Error-free legacy wrappers (normalizedServerType/NormalizedTrust) map a Classify error to the conservative default (ServerTypeStdio / TrustBlocked); callers needing to observe a rejection dispatch through Classify directly."

requirements-completed: [MCPH-01, MCPH-02]

coverage:
  - id: D1
    description: "Classify canonical transport+trust classifier exists in internal/mcp/classify.go as the single source of truth."
    requirement: "MCPH-01"
    verification:
      - kind: unit
        ref: "internal/mcp/classify_test.go#TestClassifyManagedServer"
        status: pass
    human_judgment: false
  - id: D2
    description: "A mixed url+command server (no explicit type) is rejected by Classify and never reaches stdio Open — proven at both the validation gate and the open site."
    requirement: "MCPH-01"
    verification:
      - kind: unit
        ref: "internal/mcp/transport_test.go#TestOpenServerRejectsMixedTransportBeforeAnyDispatch"
        status: pass
      - kind: unit
        ref: "internal/mcp/managed_config_test.go#TestValidateManagedServersRejectsMixedTransport"
        status: pass
    human_judgment: false
  - id: D3
    description: "A remote (streamable_http/URL) server with unset/blank/unknown trust resolves to TrustBlocked, not the runnable TrustRemoteHTTP (F-013 closed)."
    requirement: "MCPH-02"
    verification:
      - kind: unit
        ref: "internal/mcp/managed_config_test.go#TestNormalizedTrustRemoteHTTPInferred"
        status: pass
      - kind: unit
        ref: "internal/mcp/classify_test.go#TestClassifyManagedServer (remote-empty-trust rows)"
        status: pass
    human_judgment: false
  - id: D4
    description: "managed_config.go (normalizedServerType/NormalizedTrust/validateManagedServers) and transport.go (OpenServer) all dispatch through Classify; standalone decision bodies retired."
    requirement: "MCPH-01"
    verification:
      - kind: unit
        ref: "internal/mcp/managed_config_test.go#TestNormalizedServerType"
        status: pass
      - kind: unit
        ref: "go test ./internal/mcp/ -count=1 (whole package green)"
        status: pass
    human_judgment: false
  - id: D5
    description: "Explicit type<->trust matrix enforced: stdio+remote_http and streamable_http+{trusted_local,sandboxed_local} are hard Classify errors; the memory recipe (streamable_http+trusted_recipe) stays valid+runnable."
    requirement: "MCPH-01"
    verification:
      - kind: unit
        ref: "internal/mcp/classify_test.go#TestClassifyManagedServer (matrix rows)"
        status: pass
    human_judgment: true
    rationale: "Authored as a `backstop` truth (planner A1/Probe #1): the matrix is a locked assumption with no pre-existing exerciser; the phase verifier should independently confirm the matrix boundary from evidence rather than trust the authoring run alone."

# Metrics
duration: ~35min (spanned two sessions; Task 1 prior session, Task 2 completed there + closed out this session)
completed: 2026-07-18
status: complete
---

# Phase 38 · Plan 01 — Canonical MCP transport+trust classifier

**One `Classify(ManagedServer) (serverType, trust, err)` classifier now governs the `internal/mcp` core: mixed url+command is rejected before any stdio spawn (F-027) and an unset remote trust blocks instead of auto-promoting to runnable (F-013).**

## Canonical contract (for 38-04/38-05/38-06 to migrate against — do NOT re-derive)

```go
// internal/mcp/classify.go
func Classify(s ManagedServer) (serverType string, trust string, err error)
```

- **serverType** ∈ { `ServerTypeStdio` ("stdio"), `ServerTypeStreamableHTTP` ("streamable_http") }
- **trust** ∈ { `TrustTrustedRecipe`, `TrustTrustedLocal`, `TrustSandboxedLocal`, `TrustRemoteHTTP`, `TrustBlocked` } (existing `Trust*` constants — no new vocabulary introduced)
- **Transport resolution:** explicit `type==""` && url && command → **error** (mixed); `type==""` && url → streamable_http; `type==""` (cmd-only or bare) → stdio; explicit stdio/streamable_http → verbatim; any other non-empty type → **error** ("unknown type %q").
- **Trust resolution (`resolveTrust`):** known explicit class wins → else `recipe:`-prefixed `Source` → `TrustTrustedRecipe` → else `TrustBlocked`. **No streamable_http→TrustRemoteHTTP auto-promote.**
- **Consistency matrix (`checkTypeTrustConsistency`):** valid = stdio+{trusted_recipe,trusted_local,sandboxed_local,blocked}, streamable_http+{trusted_recipe,remote_http,blocked}. Invalid (hard error) = stdio+remote_http, streamable_http+{trusted_local,sandboxed_local}.
- **Legacy wrappers:** `normalizedServerType(cfg)` and `NormalizedTrust(name)` delegate to Classify; a Classify error maps to the conservative default (`ServerTypeStdio` / `TrustBlocked`). Callers that must OBSERVE a rejection call `Classify` directly (as `OpenServer` and `validateManagedServers` now do).

## Task commits

Each task committed atomically (TDD):

1. **Task 1 — RED**: add failing table test for Classify — `ae2dfb69` (test)
2. **Task 1 — GREEN**: implement Classify (classify.go: Classify + resolveTrust + checkTypeTrustConsistency) — `96124197` (feat)
3. **Task 2 — REFACTOR**: migrate managed_config.go + transport.go onto Classify; rewrite the two bug-encoding tests — `03642a60` (refactor)

_Note: Task 2's work was fully implemented (and green) in the working tree by the prior session; this session verified it (build/vet/test/file-size all clean) and closed it out per the safe-resume gate — commit + this SUMMARY._

## Files Created/Modified

- `internal/mcp/classify.go` (created, 100 LOC) — Classify + resolveTrust + checkTypeTrustConsistency
- `internal/mcp/classify_test.go` (created, 228 LOC) — TestClassifyManagedServer table + F-013/F-027 regression guards
- `internal/mcp/managed_config.go` (modified, 332 LOC) — normalizedServerType/NormalizedTrust are now thin Classify wrappers; validateManagedServers dispatches through Classify
- `internal/mcp/transport.go` (modified, 77 LOC) — OpenServer classifies via Classify first, returns its error on mixed/ambiguous instead of falling through to stdio Open
- `internal/mcp/managed_config_test.go` (modified) — TestNormalizedTrustRemoteHTTPInferred + TestNormalizedServerType rewritten to fixed behavior (justified inline per CLAUDE.md); TestValidateManagedServersRejectsMixedTransport added
- `internal/mcp/transport_test.go` (modified) — TestOpenServerRejectsMixedTransportBeforeAnyDispatch added

## Decisions Made

See `key-decisions` frontmatter (D-01/D-02/D-03 + locked assumption A1). No deviations from the plan's Task 1/Task 2 actions.

## Deviations from Plan

None material. `transport_test.go` was added to the touched set (not in the plan's declared `files_modified`) to house the SC1 "stdio Open unreached" regression guard the acceptance criteria explicitly call for — a strict addition, no scope creep.

## Issues Encountered

None. The one naming collision (a pre-existing `TestClassify` in `ssrf_test.go` for the lowercase SSRF `classify(ip)`) was already resolved in the RED commit by naming the new suite `TestClassifyManagedServer`.

## Next Phase Readiness

- **38-04** (manager/runtime.go, status.go, managed_config_identity.go), **38-05** (mount.go path), and **38-06** (mcp_status.go/doctor.go) migrate their remaining call sites onto the contract above.
- Whole `internal/mcp` package is green: `go build ./internal/mcp/...`, `go vet ./internal/mcp/`, and the Classify/NormalizedTrust/NormalizedServerType/ValidateManagedServers/OpenServer/EnabledServers suites all pass. `-race` re-run belongs to the phase-close full-matrix verification (WSL).

---
*Phase: 38-mcp-governance-hardening*
*Completed: 2026-07-18*
