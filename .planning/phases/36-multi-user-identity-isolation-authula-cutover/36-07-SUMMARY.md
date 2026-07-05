---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 07
subsystem: infra
tags: [mcp, skills, multi-user, per-identity, identityctx, path-traversal, filesystem-isolation, pyscripts]

# Dependency graph
requires:
  - phase: 36 (36-01)
    provides: local admin-cap seed + identity model the local/no-principal fallback maps onto (D-25)
  - phase: 11 (skills subsystem)
    provides: skills Loader + MaterializeBuiltins + $AURA_SKILLS_DIR the per-identity rooting layers on
  - phase: profile store
    provides: profileDir traversal-safe containment guard (the exact analog extracted here)
provides:
  - profile.RootIdentityDir — shared traversal-safe per-identity rooting primitive (mcp+skills reuse)
  - mcp.MountForIdentity / SetEnabledForIdentity / SetTrustForIdentity — per-identity MCP config overlay
  - mcp.IsSharedAdminGoverned + SourceRecipeMemory — class-(b) shared agent-memory marker (single source of truth)
  - skills.NewSkillToolForIdentity — per-identity skills + pyscripts storage rooting (built-ins shared read-only)
affects: [36-08 provisioning saga (creates these roots per identity), 36-10 mcp/skill audit UI, 37 snippet execution isolation, mcp manager mount wiring]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Shared traversal guard: one profile.RootIdentityDir chokepoint for every per-identity filesystem root (no duplication)"
    - "Overlay-over-read-only-catalog: per-identity enable/trust deltas layered on the shared MCP catalog by value-copy (shared never mutated)"
    - "Class marker by intrinsic Source (recipe:memory), name-independent, single source of truth with the manager catalog"
    - "Additive *ForIdentity with local/no-principal fallback (D-21/D-25); depth-1 loader scan keeps per-identity subdirs from leaking through the shared base root"

key-files:
  created:
    - internal/mcp/managed_config_identity.go
    - internal/mcp/managed_config_identity_test.go
    - internal/skills/identity_root.go
    - internal/skills/identity_root_test.go
  modified:
    - internal/profile/store.go
    - internal/mcp/manager/catalog.go

key-decisions:
  - "Extract the profileDir guard as exported profile.RootIdentityDir rather than duplicate it into mcp+skills (CLAUDE.md no-duplication)"
  - "Identify class-(b) by Source==SourceRecipeMemory (name-independent) and make the manager catalog reference the same constant (single source of truth for the isolation marker)"
  - "Per-identity MCP overlay is a light IdentityMCPConfig (enable/trust only), NOT a full ManagedConfig, so it dodges validateManagedServers' command/url requirement"
  - "MCP uses a uniform per-identity overlay (empty overlay => shared catalog, backward compatible); skills local/no-principal resolves the BASE dir (built-ins live there, single-user backward compat)"
  - "NewSkillToolForIdentity returns the resolved storage roots (exported form of the plan's newSkillToolForIdentity) — execution/tool-assembly wiring is a later plan; this plan is storage rooting only"

patterns-established:
  - "Pattern: per-identity filesystem rooting always goes through profile.RootIdentityDir (charset + ../slash reject + filepath.Rel escape check)"
  - "Pattern: shared class-(b) infra is admin-governed only — the per-identity path refuses it (ErrSharedAdminGoverned), overlay ignored on mount"

requirements-completed: []  # MUSR-01 is phase-spanning (filesystem plane advanced here; RLS=04, docs=05, Garage=06, saga=08, flip=12) — NOT closed by this plan.

coverage:
  - id: D1
    description: "profile.RootIdentityDir — shared traversal-safe per-identity rooting guard (charset + ../slash reject + filepath.Rel containment), reused by mcp+skills"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/mcp/managed_config_identity_test.go#TestIdentityConfigPathRejectsTraversal, internal/skills/identity_root_test.go#TestNewSkillToolForIdentityRejectsTraversal, internal/profile (existing edge tests)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Per-identity MCP config: MountForIdentity layers class-(a) enable/trust overlay over the read-only shared catalog without mutating it"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/mcp/managed_config_identity_test.go#TestSetEnabledForIdentityOverlaysClassA, #TestSetTrustForIdentityOverlaysClassA, #TestIdentityConfigPathContained"
        status: pass
    human_judgment: false
  - id: D3
    description: "Class-(b) shared agent-memory server (recipe:memory, :8091) is admin-governed only — per-identity toggle refused, overlay ignored on mount"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/mcp/managed_config_identity_test.go#TestClassBSharedServerNotUserToggleable, #TestSetEnabledForIdentityUnknownServer, #TestIsSharedAdminGoverned"
        status: pass
    human_judgment: false
  - id: D4
    description: "Per-identity skills + pyscripts rooting: NewSkillToolForIdentity roots $AURA_SKILLS_DIR/{id} + ~/.aura/pyscripts/{id}; built-ins shared read-only; cross-identity isolation; local→base backward compat"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/skills/identity_root_test.go#TestNewSkillToolForIdentityRootsUserSkills, #TestNewSkillToolForIdentitySharedBuiltinsIsolateUsers, #TestNewSkillToolForIdentityLocalUsesBase"
        status: pass
    human_judgment: false
  - id: D5
    description: "Race + tagged-matrix tier for the new mcp/skills/profile code (CGO-required -race)"
    verification:
      - kind: unit
        ref: "go test -race ./internal/mcp/ ./internal/skills/ ./internal/profile/ (WSL/CI — CGO disabled on this Windows host)"
        status: unknown
    human_judgment: false
    rationale: "-race needs CGO/gcc (unavailable on this host); must run green in WSL/CI before phase close (no-skip-as-green)."

# Metrics
duration: ~35min
completed: 2026-07-05
status: complete
---

# Phase 36 Plan 07: Per-identity MCP config + skills/pyscripts filesystem rooting Summary

**Traversal-safe per-identity filesystem isolation (D-20/D-21): a shared `profile.RootIdentityDir` guard roots `~/.aura/mcp/{id}/servers.json` (class-(a) enable/trust overlay over the read-only shared catalog; class-(b) agent-memory admin-governed) and `$AURA_SKILLS_DIR/{id}` + `~/.aura/pyscripts/{id}` (built-ins shared read-only, local backward-compatible).**

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-07-05T19:55:26Z
- **Tasks:** 2 (both TDD: RED → GREEN)
- **Files modified:** 6 (4 created, 2 modified)

## Accomplishments
- Extracted `profile.RootIdentityDir(root, identity)` — the single traversal-safe per-identity rooting primitive (identityPattern charset + `..`/slash reject + `filepath.Rel` "escapes root" assertion), reused by both mcp and skills; `profileDir` now delegates to it and the `ErrInvalidIdentity` sentinel is preserved (profile edge tests stay green).
- `internal/mcp/managed_config_identity.go`: `MountForIdentity` layers a per-identity `~/.aura/mcp/{id}/servers.json` overlay (enable/trust for class-(a)) over the read-only shared catalog, by value-copy so the shared map is never mutated; `SetEnabledForIdentity`/`SetTrustForIdentity` write the overlay.
- Class-(b) isolation (D-19): the shared agent-memory server is identified name-independently by `Source == SourceRecipeMemory`; mount ignores any overlay pref for it and the mutators return `ErrSharedAdminGoverned` (plus `ErrUnknownServer` for a name absent from the shared catalog).
- `internal/skills/identity_root.go`: `NewSkillToolForIdentity(ctx, base)` roots user skills under `$AURA_SKILLS_DIR/{id}` and snippets under `~/.aura/pyscripts/{id}` via the same guard, keeps built-ins shared read-only (`LoaderRoots = [sharedBase, perIdentityUser]`, never copied per identity), and resolves the base dirs unchanged for the local/no-principal path — proven cross-identity-isolated by a two-identity loader test (the depth-1 loader scan makes a `{id}` subdir under base carry no top-level SKILL.md, so user skills never leak through the shared base root).

## Task Commits

Each task followed the TDD RED → GREEN cycle, committed atomically:

1. **Task 1: Traversal-safe root helper + per-identity MCP config**
   - `e74c0b6f` (test) — failing per-identity MCP config tests
   - `124840a1` (feat) — profile.RootIdentityDir + mcp MountForIdentity/*ForIdentity + catalog single-source marker
2. **Task 2: Per-identity skills + pyscripts storage rooting**
   - `4cd5a13e` (test) — failing per-identity skills rooting tests
   - `3ec72d52` (feat) — NewSkillToolForIdentity storage rooting

**Plan metadata:** docs commit (this SUMMARY + STATE.md + ROADMAP.md)

## Files Created/Modified
- `internal/mcp/managed_config_identity.go` (created) — per-identity MCP overlay, MountForIdentity, Set{Enabled,Trust}ForIdentity, IsSharedAdminGoverned, SourceRecipeMemory
- `internal/mcp/managed_config_identity_test.go` (created) — 7 tests (traversal, containment, class-(a) overlay, class-(b) refusal, unknown-server, marker)
- `internal/skills/identity_root.go` (created) — NewSkillToolForIdentity, SkillRootBase, IdentitySkillRoots, local fallback
- `internal/skills/identity_root_test.go` (created) — 4 tests (rooting, traversal, shared-builtins isolation, local backward-compat)
- `internal/profile/store.go` (modified) — extracted exported RootIdentityDir; profileDir delegates
- `internal/mcp/manager/catalog.go` (modified) — memory recipe Source references mcp.SourceRecipeMemory (deviation, below)

## Decisions Made
- **Shared guard over duplication:** exported `profile.RootIdentityDir` (CLAUDE.md no-duplication) rather than copy the containment check into mcp+skills. `internal/mcp` and `internal/skills` importing `internal/profile` is cycle-free (profile imports only stdlib).
- **Class-(b) by intrinsic Source, single source of truth:** `IsSharedAdminGoverned` keys on `Source == SourceRecipeMemory` (name-independent); the manager catalog now references the same constant so the isolation guard and the recipe stamp cannot drift.
- **Light overlay type:** the per-identity overlay is `IdentityMCPConfig` (enable/trust only), not a full `ManagedConfig`, so it isn't subject to `validateManagedServers`' command/url requirement.
- **Local fallback asymmetry (intentional):** MCP is uniform (empty overlay ⇒ shared catalog, backward compatible); skills resolves the BASE dir for local/no-principal because built-ins live at the base and the single-user CLI must keep reading `$AURA_SKILLS_DIR` directly (D-21 Test 3).
- **Storage rooting only:** `NewSkillToolForIdentity` returns the resolved roots (exported form of the plan's `newSkillToolForIdentity`); wiring into the live manager mount + `cmd/aura` skill-tool constructor, and snippet execution isolation, are later plans (08 creates the roots per identity; 37 owns execution).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Single source of truth for the class-(b) isolation marker**
- **Found during:** Task 1 (per-identity MCP config)
- **Issue:** `IsSharedAdminGoverned` must match the exact `Source` the manager catalog stamps onto the memory recipe. Two independent `"recipe:memory"` literals (guard vs. catalog) that must stay in sync for a security boundary is exactly the drift fragility CLAUDE.md's no-duplication rule forbids — a future rename of one silently breaks the class-(b) refusal.
- **Fix:** Defined `mcp.SourceRecipeMemory` and pointed the manager catalog's memory recipe `Source` (both the CatalogEntry and the ManagedServer) at it. Value-identical (`"recipe:memory"`), so `catalog_test`'s `memory.Source == "recipe:memory"` assertion stays green.
- **Files modified:** internal/mcp/manager/catalog.go (1 file beyond the plan's `files_modified`)
- **Verification:** `go test ./internal/mcp/manager/` green; `go vet ./...` clean; `TestIsSharedAdminGoverned` passes.
- **Committed in:** 124840a1 (Task 1 GREEN)

---

**Total deviations:** 1 auto-fixed (Rule 2 - missing critical / correctness). One file beyond `files_modified` (catalog.go), mirroring how 36-03 added serve.go for the reaper wiring.
**Impact on plan:** No scope creep — the change makes the class-(b) isolation guard correct and drift-proof. All other files exactly as scoped.

## Issues Encountered
None — the design (shared guard + overlay-over-read-only-catalog + depth-1 loader scan) fell out of the established `profileDir` and skills-loader patterns cleanly.

## Verification (no-skip-as-green)
- `go build ./...` — clean.
- `go vet ./...` — clean (repo-wide regression scan).
- `go test ./internal/mcp/ ./internal/skills/ ./internal/profile/ ./internal/mcp/manager/` — all green (11 new tests + existing suites) on this Windows host.
- **`-race` NOT run here** — CGO is disabled on this host (no gcc); per CLAUDE.md "Where to run what", the `-race` tier + the full tagged matrix must run green in WSL/CI before phase close. Honestly reported as `unknown` (coverage D5), not fabricated.

## User Setup Required
None — no external service configuration, no new env vars, no new packages, no migrations.

## Next Phase Readiness
- Rooting primitives are established and unit-proven. **Plan 08 (provisioning saga)** should CREATE these roots per identity (`~/.aura/mcp/{id}`, `$AURA_SKILLS_DIR/{id}`, `~/.aura/pyscripts/{id}`) as idempotent saga legs.
- **Wiring** `MountForIdentity` into the live `internal/mcp/manager` mount and `NewSkillToolForIdentity` into `cmd/aura`'s skill-tool constructor (with the export-activation loader root) is a later plan — this plan deliberately stops at storage rooting.
- MUSR-01 stays open (phase-spanning): RLS carrier=04, documents fail-closed=05, Garage keys=06, saga=08, admin audit UI=10, rollout flip=12. The two-identity live E2E (success-criterion 1) closes at 36-12.
- Before phase close: run the `-race`/tagged matrix in WSL/CI (D5).

## Self-Check: PASSED
- Created files exist: internal/mcp/managed_config_identity.go, internal/mcp/managed_config_identity_test.go, internal/skills/identity_root.go, internal/skills/identity_root_test.go — all present.
- Task commits exist: e74c0b6f, 124840a1, 4cd5a13e, 3ec72d52 — all in `git log`.

---
*Phase: 36-multi-user-identity-isolation-authula-cutover*
*Completed: 2026-07-05*
