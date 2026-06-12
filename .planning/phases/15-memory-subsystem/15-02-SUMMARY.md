---
phase: 15-memory-subsystem
plan: 02
subsystem: infra
tags: [mcp, agent-memory, recipe, default-on, streamable-http, config, neo4j-labs]

# Dependency graph
requires:
  - phase: 15-01
    provides: "PRD amendment #62 re-scope (UX-09 on-demand reasoning/insight) + AURA_AGENT_MEMORY_MCP_* env catalog"
provides:
  - "memory streamable-HTTP trusted_recipe in BuiltInCatalog() (D-06/D-07/D-08)"
  - "default-on inject-unless-disabled seam in loadMCPServers (injectDefaultOnMemory helper)"
  - "memory mounts out of the box (no `aura mcp install`); `aura mcp disable memory` + explicit install both respected"
affects:
  - "15-04 memory_integration tier (asserts the live 16-tool Deferred + memory__* surface this plan wires)"
  - "15-03 aura memory operator CLI (reads cfg.MCPPolicies[\"memory\"] this plan populates)"

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "First HTTP (streamable_http) recipe in BuiltInCatalog() — env-driven URL from AURA_AGENT_MEMORY_MCP_PORT"
    - "Default-on managed mount via inject-unless-disabled (THE genuine new design node of the phase)"

key-files:
  created:
    - internal/config/config_mcp.go
    - internal/config/config_mcp_default_on_test.go
  modified:
    - internal/mcp/manager/catalog.go
    - internal/mcp/manager/catalog_test.go
    - internal/config/config.go
    - cmd/aura/mcp_test.go
    - cmd/aura/main_test.go

key-decisions:
  - "memory recipe is trusted_recipe (A1), NOT remote_http — so default-on is trusted (D-08)"
  - "Default-on uses Option (b) inject-unless-disabled in loadMCPServers (survives a deleted config), not seed-on-first-run"
  - "Inject lands AFTER the env-override delete loop so an AURA_MCP_SERVERS_JSON memory override still wins"

patterns-established:
  - "Pattern 1: env-driven recipe URL via a package-local helper (memoryRecipeURL) reading AURA_AGENT_MEMORY_MCP_PORT, default 8091"
  - "Pattern 2: default-on precedence ladder — explicit policy > env override > disable check > catalog inject"

requirements-completed: [UX-09]

# Metrics
duration: ~25min
completed: 2026-06-12
---

# Phase 15 Plan 02: Default-On Memory MCP Recipe Summary

**Registers neo4j-labs agent-memory as a trusted, default-on, fail-soft streamable-HTTP managed MCP recipe — the full 16-tool memory surface (Deferred + `memory__*`) mounts out of the box with no `aura mcp install`, via a new inject-unless-disabled seam in `loadMCPServers`.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-06-12T08:55:00Z (approx)
- **Completed:** 2026-06-12T09:20:00Z (approx)
- **Tasks:** 2 (both TDD)
- **Files modified:** 7 (2 created, 5 modified)

## Accomplishments

- `memory` `CatalogEntry` in `BuiltInCatalog()` — the first HTTP recipe (existing entries are stdio `Command`-based): `streamable_http` + `trusted_recipe`, URL `http://127.0.0.1:<port>/mcp/` composed from `AURA_AGENT_MEMORY_MCP_PORT` (default 8091, no new env var). Shows in `aura mcp recipes` as a `trusted_recipe` row + the URL in `--json`.
- `injectDefaultOnMemory` seam in `loadMCPServers`: on a fresh machine with an empty/absent `AURA_MCP_CONFIG`, `memory` lands in `cfg.MCPPolicies` with no prior install (D-08). Precedence ladder: explicit/operator policy wins → `AURA_MCP_SERVERS_JSON` override wins → `aura mcp disable memory` (Enabled=false) keeps it unmounted (D-09) → otherwise inject `LookupCatalog("memory").Server`.
- The 16-tool Deferred + `memory__*` mount flows through the EXISTING `MountManagedServer` (zero new mount code) — memory is now in `cfg.MCPPolicies`, so `buildRegistryWithMCP` mounts it fail-soft (a down sidecar is a lazy reconnect-on-use HTTP closer, never a boot-fatal error, D-09).

## Task Commits

Each task was committed atomically (TDD RED → GREEN):

1. **Task 1 (RED): failing memory recipe tests** - `7aba8d3d` (test)
2. **Task 1 (GREEN): register memory streamable-HTTP trusted recipe** - `93be535d` (feat)
3. **Task 2 (RED): failing default-on seam tests** - `980d1918` (test)
4. **Task 2 (GREEN): default-on memory mount seam** - `8113e645` (feat)

_Plan metadata (this SUMMARY) committed separately._

## Files Created/Modified

- `internal/mcp/manager/catalog.go` - new `memory` `CatalogEntry` + `memoryRecipeURL()` helper (port from `AURA_AGENT_MEMORY_MCP_PORT`). 157 LOC, ≤600.
- `internal/mcp/manager/catalog_test.go` - `TestCatalogIncludesMemoryStreamableHTTPRecipe`, `TestCatalogMemoryURLHonorsPortEnv`; updated the exact-name-list assertion to include `memory`.
- `internal/config/config.go` - `injectDefaultOnMemory(...)` call after the env delete loop, before the empty early-return. 501 LOC, ≤600.
- `internal/config/config_mcp.go` (NEW) - `injectDefaultOnMemory` helper + `memoryRecipeName` const. 38 LOC.
- `internal/config/config_mcp_default_on_test.go` (NEW) - `TestMemoryDefaultOn`, `_RespectsDisable`, `_RespectsExplicitInstall`, `_EnvServersOverrideWins`.
- `cmd/aura/mcp_test.go` - extended the recipes golden (`TestMCPRecipesListsBuiltins`) to assert the `memory` `trusted_recipe` row + the streamable-http URL in `--json`.
- `cmd/aura/main_test.go` - isolation fix in `TestBuildRegistryBlockedManagedServerNotLaunched` (see Deviations).

## Decisions Made

- **trusted_recipe, not remote_http** (A1 / D-08): default-on must be trusted, so the recipe carries `mcp.TrustTrustedRecipe` at both the `CatalogEntry.TrustClass` and `Server.Trust.Class` levels.
- **Option (b) inject-unless-disabled** over seed-on-first-run: survives a deleted `servers.json` ("core capability → on out of the box"), at the cost of touching the shared `loadMCPServers` load path. The disable check mirrors `RunnableManagedServers` (runtime.go:53).
- **Inject after the env delete loop**: so an `AURA_MCP_SERVERS_JSON` `memory` override still wins (verified by `TestMemoryDefaultOn_EnvServersOverrideWins`).
- **Helper in a new `config_mcp.go`** (not inline in config.go): keeps `loadMCPServers` focused and config.go well under the 600-LOC budget (501 LOC).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Stale assertion in `TestBuildRegistryBlockedManagedServerNotLaunched`**
- **Found during:** Task 2 (default-on seam — the broad regression sweep `go test ./internal/... ./cmd/...`)
- **Issue:** `TestBuildRegistryBlockedManagedServerNotLaunched` writes a managed config with only a `blocked` server, calls `config.LoadDB()` → `buildRegistryWithMCP`, and asserts `len(closers) == 0`. With default-on, `LoadDB()` now also injects `memory` into `cfg.MCPPolicies`, and `buildRegistryWithMCP` mounts it as a lazy reconnect-on-use HTTP closer (correct fail-soft behavior — the streamable-HTTP transport connects on first use, so the mount succeeds with a closer even when the sidecar is down). Result: `closers=1`, tripping the test. The test's `len(closers)==0` conflated "the blocked server didn't mount" (its real intent, still true) with "nothing else mounts" (no longer true).
- **Fix:** Added an explicitly-disabled `memory` entry (`Enabled=false`) to the test's managed config so it isolates the blocked-server assertion it actually targets, plus a new assertion that the disabled memory does not reach policies. Test intent unchanged; the memory default-on + disable-respect path is independently covered by `config.TestMemoryDefaultOn*`. Justification recorded in the commit body per CLAUDE.md ("NEVER MODIFY TESTS TO MAKE THEM PASS unless the test itself is broken").
- **Files modified:** `cmd/aura/main_test.go`
- **Verification:** `go test ./cmd/aura/` green; `go test ./internal/... ./cmd/...` shows zero failures.
- **Committed in:** `8113e645` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug — a stale test assertion directly caused by the in-scope default-on change).
**Impact on plan:** The deviation is the expected blast radius of a shared-load-path change (default-on widens what every `config.Load` caller sees). No scope creep — the fix isolates the pre-existing test's intent and the new behavior is covered by dedicated tests.

## Issues Encountered

- **Worktree cwd vs. plan's `cd /d/Aura`:** the plan's verify commands literally say `cd /d/Aura`, but that path is the MAIN repo, not this worktree (`D:/Aura/.claude/worktrees/agent-a648bb65ff82e3e59`). Running `cd /d/Aura && go test ...` ran the un-edited main-repo tree and falsely passed the RED step. Resolved by running all `go test`/`go vet`/`golangci-lint` from the worktree root (the Bash tool's cwd), never `cd /d/Aura`. RED was then correctly observed in the worktree.

## Verification

- `go test ./internal/mcp/manager/ ./internal/config/ ./cmd/aura/` — green (recipe golden + default-on + disable + explicit-install + env-override).
- `go test -race ./internal/config/ ./internal/mcp/manager/` and `go test -race ./cmd/aura/ -run 'TestBuildRegistry|Recipes'` — race-clean (native, Go 1.26.4 windows/amd64).
- `go test ./internal/... ./cmd/...` — zero failures across the full default-tag tree (broad regression sweep for the shared-load-path change).
- `go vet` + `golangci-lint run ./internal/config/ ./internal/mcp/manager/ ./cmd/aura/` — 0 issues.
- File-size hook (lefthook pre-commit) — all Go files ≤600 LOC (catalog.go 157, config.go 501, config_mcp.go 38).
- The live 16-tool Deferred + `memory__*` assertion and the fail-soft-with-sidecar-down assertion are owned by plan 15-04's `memory_integration` tier (this plan's mount path is REUSE-as-is).

## Known Stubs

None — both tasks wire real config/catalog surface with full unit coverage. No placeholder data, no hardcoded empties flowing to UI, no TODO/FIXME. The live sidecar round-trip is intentionally deferred to 15-04's `memory_integration` tier (per the plan's verification split), not a stub.

## Threat Flags

None — no new security surface beyond the plan's `<threat_model>`. The default-on widening (T-15-02-04, HIGH) is mitigated exactly as planned: `trusted_recipe` only, respects `aura mcp disable memory` (Task 2 disable check, tested), and the 16 tools mount Deferred + `memory__*` (reached only via `tool_search`, not force-loaded). No new endpoint/auth/file/schema surface was introduced in the Go wiring.

## Next Phase Readiness

- `cfg.MCPPolicies["memory"]` is populated on a fresh machine → 15-03 (`aura memory` operator CLI) can resolve the server via the existing `effectiveManagedMCPServer` path.
- 15-04 `memory_integration` tier can assert the live surface against the recipe URL (`AURA_AGENT_MEMORY_MCP_URL`/`...PORT`).
- No blockers. The reproducible compose `build:` (15-05) and the operator CLI (15-03) are independent follow-ons.

## Self-Check: PASSED

- `internal/config/config_mcp.go` exists.
- `internal/config/config_mcp_default_on_test.go` exists.
- `internal/mcp/manager/catalog.go` exists (modified).
- `.planning/phases/15-memory-subsystem/15-02-SUMMARY.md` exists.
- Commits `7aba8d3d` (test), `93be535d` (feat), `980d1918` (test), `8113e645` (feat) all present in `git log`.

---
*Phase: 15-memory-subsystem*
*Completed: 2026-06-12*
