---
phase: 37-per-user-full-capability-sandbox
plan: 10
subsystem: infra
tags: [sandbox, egress, docker, moby, nftables, config, composition-root, SBX-04, fail-closed]

# Dependency graph
requires:
  - phase: 37 (37-01)
    provides: SBX-04 egress amendment (D-06) + AURA_SANDBOX_* config surface + KnobSpec registry
  - phase: 37 (37-04)
    provides: DockerBackend + WithEgress option + launchEgress lifecycle (the sidecar the floor rides on)
  - phase: 37 (37-05)
    provides: SandboxRouter (Strict no-op + fail-CLOSED Route) + buildSandboxRouter composition root
  - phase: 37 (37-06)
    provides: egress.go filter-table floor + OpenSandbox FQDN allowlist (the mechanism now wired live)
provides:
  - config.SandboxConfig.EgressImage sourced from AURA_SANDBOX_EGRESS_IMAGE (non-empty default = floor-on, SC#4)
  - AURA_SANDBOX_EGRESS_IMAGE cataloged in the KnobSpec registry (KindString, non-secret)
  - buildSandboxRouter wires usersandbox.WithEgress(cfg.Sandbox.EgressImage) via newSandboxBackend (SBX-04 closed at the composition root)
  - DockerBackend.EgressImage() read-only accessor (docker-free wiring regression seam)
  - docker-free cmd/aura wiring guard + composition-root live-DROP re-test (docker_integration, WSL/CI)
  - compose.yaml + ADR 0037 truthed-up to the now-live, fail-CLOSED floor wiring
affects: [phase-37 verification, phase-37-Gate-3, secure-phase-37, 37A-web-artifact-delivery]

# Tech tracking
tech-stack:
  added: []  # zero new dependencies — go.mod/go.sum byte-unchanged
  patterns:
    - "Composition-root option wiring is regression-tested docker-free via a read-only accessor (EgressImage()) — the applied functional-option is observable without a daemon (NewDockerBackend never dials at construction)."
    - "A security floor that must be ON by default is sourced from a NON-EMPTY config default so the strict-profile posture is fail-CLOSED (absent image => refuse), never fail-open (silent no-op)."

key-files:
  created:
    - cmd/aura/serve_dispatch_egress_test.go
    - cmd/aura/serve_dispatch_egress_integration_test.go
  modified:
    - internal/config/config_sandbox.go
    - internal/config/config_knobs.go
    - internal/config/config_sandbox_test.go
    - cmd/aura/serve_dispatch.go
    - internal/sandbox/usersandbox/docker_backend.go
    - internal/sandbox/usersandbox/egress_integration_test.go
    - compose.yaml
    - docs/adr/0037-per-identity-docker-sandbox.md

key-decisions:
  - "The AURA_SANDBOX_EGRESS_IMAGE default is NON-EMPTY (aura-egress:latest) — floor-ON by default under strict profiles (SC#4/D-06). An empty default would wire WithEgress yet leave the floor OFF, which does not close SC#4; PROHIBITED by the plan."
  - "Box creation is fail-CLOSED when the egress image is set but unavailable (ensureImage pull-fail -> launchEgress error -> Resolve error -> Route routed=true,err -> the tool DENIES). A strict box never comes up un-floored."
  - "This plan changed only WHERE the sidecar is constructed (the composition root); the D-07 sidecar contract (NET_ADMIN on the sidecar only, box netns share, filter-table floor) is unchanged. The 37-04 hand-built lifecycle tests stay box-only."

patterns-established:
  - "Docker-free composition-root wiring guard: assert a functional-option was applied by exposing a minimal read-only accessor and passing a nil client (construction never dials)."
  - "no-skip-as-green integration test: //go:build docker_integration + a native-Linux enforcing-bridge gate that t.Fatal under $CI on a non-linux daemon; local skip only."

requirements-completed: [SBX-04]

coverage:
  - id: D1
    description: "config.SandboxConfig.EgressImage sourced from AURA_SANDBOX_EGRESS_IMAGE with a NON-EMPTY default (aura-egress:latest) + cataloged in the KnobSpec registry — the 'zero Go code reads AURA_SANDBOX_EGRESS_IMAGE' half of the BLOCKER closed."
    requirement: "SBX-04"
    verification:
      - kind: unit
        ref: "internal/config/config_sandbox_test.go#TestLoad_SandboxConfig (defaults on unset + env overrides parse into typed fields)"
        status: pass
    human_judgment: false
  - id: D2
    description: "buildSandboxRouter wires usersandbox.WithEgress(cfg.Sandbox.EgressImage) via newSandboxBackend; DockerBackend.EgressImage() accessor proves it docker-free; non-strict profile stays a nil host-direct no-op."
    requirement: "SBX-04"
    verification:
      - kind: unit
        ref: "cmd/aura/serve_dispatch_egress_test.go#TestBuildSandboxRouterWiresEgress"
        status: pass
    human_judgment: false
  - id: D3
    description: "Composition-root live egress DROP: a box created via buildSandboxRouter -> Route carries its aura-egress sidecar (NET_ADMIN + shared box netns), reaches the public internet but is DROPPED from 169.254.169.254 / RFC1918; Stop leaves no orphan."
    requirement: "SBX-04"
    verification:
      - kind: integration
        ref: "cmd/aura/serve_dispatch_egress_integration_test.go#TestBuildSandboxRouter_LaunchesEgressFloor (go test -tags docker_integration -race ./cmd/aura/ -run TestBuildSandboxRouter_LaunchesEgressFloor, AURA_EGRESS_ENFORCE=1)"
        status: unknown
    human_judgment: true
    rationale: "Requires a native-Linux non-masquerading dockerd + the aura-egress image built (docker build docker/aura-egress). This Windows host has no dockerd and CGO_ENABLED=0; Docker-Desktop/WSL vpnkit NATs the bridge and cannot validate DROP (37-RESEARCH Pitfall 3). Carried forward to WSL/CI (37-VALIDATION.md Manual-Only, Dimension 8 SBX-04) — the closing live proof for the BLOCKER."
  - id: D4
    description: "compose.yaml + ADR 0037 (Negative bullet + Residual B/C) truthed-up to describe the now-live, config-sourced, fail-CLOSED egress floor wiring (no longer an unwired forward reference)."
    verification:
      - kind: other
        ref: "grep -c 'buildSandboxRouter' docs/adr/0037-per-identity-docker-sandbox.md == 4 (>=1); compose.yaml AURA_SANDBOX_EGRESS_IMAGE comment states config.SandboxConfig reads it (LIVE)"
        status: pass
    human_judgment: false

# Metrics
duration: ~35min
completed: 2026-07-07
status: complete
---

# Phase 37 Plan 10: Wire the Always-On Egress Floor into buildSandboxRouter (SBX-04) Summary

**Closed the Phase-37 SBX-04 BLOCKER: the always-on egress sidecar was built + unit-tested + launched by DockerBackend but INERT in the shipped binary — buildSandboxRouter now sources `AURA_SANDBOX_EGRESS_IMAGE` (non-empty default = floor-on) into `usersandbox.WithEgress`, so every strict-profile box gets the DROP-RFC1918/metadata/bridge floor, fail-CLOSED when the image is absent.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-07-07T06:44:00Z (approx)
- **Completed:** 2026-07-07T07:18:00Z
- **Tasks:** 3
- **Files modified:** 10 (8 modified + 2 created)

## Accomplishments
- **SBX-04 closed at the composition root:** `buildSandboxRouter` constructs the production `DockerBackend` via the new `newSandboxBackend(cli, cfg)` helper, which adds exactly one `usersandbox.WithEgress(cfg.Sandbox.EgressImage)` alongside `WithMaterializeSources` — `launchEgress` is no longer a permanent no-op in the shipped binary.
- **Floor-ON by default:** `config.SandboxConfig.EgressImage` is sourced from `AURA_SANDBOX_EGRESS_IMAGE` with a NON-EMPTY default (`aura-egress:latest`, mirroring the `AURA_SANDBOX_IMAGE` triplet verbatim), cataloged in the KnobSpec registry (`KindString`, non-secret). The repo-wide `AURA_SANDBOX_EGRESS_IMAGE`-in-`*.go` grep inverted **0 → 10 matches** — the BLOCKER's "zero matches" symptom is closed.
- **Fail-CLOSED posture:** a strict host without the egress image built/available refuses box creation (ensureImage pull-fail → Resolve error → Route `routed=true,err` → the tool denies) rather than running un-floored.
- **Regression-guarded two ways:** a docker-free `cmd/aura` wiring guard (`TestBuildSandboxRouterWiresEgress`, always-on CI — proves the wiring + non-empty default + non-strict→nil without a daemon) and a `//go:build docker_integration` composition-root live-DROP re-test (`TestBuildSandboxRouter_LaunchesEgressFloor`, CI `t.Fatal` on a non-linux daemon — no-skip-as-green).
- **Docs truthed-up:** `compose.yaml` + ADR 0037 (Negative bullet + Residual B/C) now describe the now-live, config-sourced, fail-CLOSED floor wiring instead of a delivered-but-inert control.

## Task Commits

Per the plan's `<commit>` directive (gap closure = ONE atomic commit; project TDD_MODE OFF), all three tasks landed in a single atomic fix commit:

1. **Task 1: Source the egress-sidecar image into config (the env-catalog triplet)** — `bdebc5c9`
2. **Task 2: Wire WithEgress at the composition root + regression-guard it (docker-free) + composition-root live DROP** — `bdebc5c9`
3. **Task 3: Truth-up the compose.yaml + ADR docs** — `bdebc5c9`

**Fix commit:** `bdebc5c9` (fix, 10 files, +394/-16 — gofmt/vet/file-size hooks green, no `--no-verify`)
**Plan metadata:** this `docs(37-10)` commit (SUMMARY + STATE + ROADMAP + REQUIREMENTS + VALIDATION)

## Files Created/Modified
- `internal/config/config_sandbox.go` — `EgressImage` field on `SandboxConfig` + `defaultSandboxEgressImage` const + the `envDefault("AURA_SANDBOX_EGRESS_IMAGE", …)` loader line.
- `internal/config/config_knobs.go` — `AURA_SANDBOX_EGRESS_IMAGE` KnobSpec row (KindString, non-secret) + updated accuracy comment.
- `internal/config/config_sandbox_test.go` — `TestLoad_SandboxConfig` extended (non-empty default assertion + digest-pinned override).
- `cmd/aura/serve_dispatch.go` — extracted `newSandboxBackend(cli, cfg)` adding `WithEgress(cfg.Sandbox.EgressImage)`; `buildSandboxRouter` calls it; doc comment states floor-on + fail-CLOSED.
- `internal/sandbox/usersandbox/docker_backend.go` — `func (b *DockerBackend) EgressImage() string` read-only accessor.
- `cmd/aura/serve_dispatch_egress_test.go` (NEW) — `TestBuildSandboxRouterWiresEgress` docker-free wiring guard.
- `cmd/aura/serve_dispatch_egress_integration_test.go` (NEW) — `TestBuildSandboxRouter_LaunchesEgressFloor` composition-root live-DROP re-test (`//go:build docker_integration`).
- `internal/sandbox/usersandbox/egress_integration_test.go` — header cross-reference to the cmd/aura composition-root proof (no behavioral change).
- `compose.yaml` — `AURA_SANDBOX_EGRESS_IMAGE` comment reworded to LIVE/fail-closed, parallel to `AURA_SANDBOX_IMAGE`.
- `docs/adr/0037-per-identity-docker-sandbox.md` — Negative bullet + Residual B/C disclose the composition-root wiring + fail-CLOSED posture.

## Decisions Made
- **Non-empty egress default (locked by the plan):** `aura-egress:latest`, not `""`. SC#4 requires the floor ON by default under strict profiles; an empty default would wire `WithEgress` yet leave the floor OFF. A non-empty default also makes box creation fail-CLOSED when the image is absent (correct posture, consistent with the phase's D-09 router).
- **Composition-root only:** changed only where the sidecar is constructed; the D-07 sidecar contract and the 37-04 hand-built box-only lifecycle tests are untouched.
- **Doc-free wiring proof:** exposed a minimal `EgressImage()` accessor rather than reaching into unexported state, so the wiring is provable on every CI run without a daemon (a nil client is safe — `NewDockerBackend` never dials at construction).

## Deviations from Plan

None — plan executed exactly as written.

One micro-realization worth recording (not a scope change): the plan's acceptance gate requires `grep -c 'WithEgress(cfg.Sandbox.EgressImage)' cmd/aura/serve_dispatch.go == 1` and notes "comment prose does not match this exact call expression". The initial doc comments repeated the exact parenthesized expression (grep would return 3); they were reworded ("WithEgress from cfg.Sandbox.EgressImage", "via WithEgress sourced from cfg.Sandbox.EgressImage") so only the real call matches — exactly the plan's stated intent.

**Total deviations:** 0 auto-fixed. **Impact on plan:** none — the fix is the surgical composition-root wiring the plan specified.

## Issues Encountered
- `config.Load()` fail-fasts on an empty LLM key (verified via `cmd/aura/chat_test.go`), so the docker-free test's "default-loaded" assertion sets a placeholder `OPENROUTER_API_KEY` and clears `AURA_SANDBOX_EGRESS_IMAGE` before `config.Load()` to force the in-code default deterministically — mirroring the `internal/config` test discipline (`clearPostgresEnv` sets the same placeholder).

## Known Stubs
None. No hardcoded empty values or placeholders introduced; the `EgressImage()` accessor returns real wired state, and the config default is a live, non-empty value.

## Verification (local gates — all green)
- `go vet ./...` clean; `go build ./...` AND `go build -tags docker_integration ./...` clean (the new tagged test compiles).
- `go vet -tags docker_integration ./cmd/aura/ ./internal/sandbox/usersandbox/` clean.
- `go test ./internal/config/ ./cmd/aura/ ./internal/sandbox/usersandbox/` green (config triplet + docker-free wiring guard; docker_integration tests compile + skip locally).
- Grep gates: `WithEgress(cfg.Sandbox.EgressImage)` in serve_dispatch.go == 1; `EgressImage string` in config_sandbox.go == 1; `func (b *DockerBackend) EgressImage()` == 1; repo-wide `AURA_SANDBOX_EGRESS_IMAGE` in `*.go` == 10 (was 0); `buildSandboxRouter` in ADR 0037 == 4.
- `go.mod` / `go.sum` byte-unchanged (git diff empty).

## Carried Forward (WSL/CI human_verification — honestly deferred, NOT passed)
- **`-race` on the touched packages** (`./cmd/aura/ ./internal/config/ ./internal/sandbox/usersandbox/`) — not runnable here (`CGO_ENABLED=0`, no gcc). Must run green in WSL/CI.
- **Composition-root live egress DROP** — `docker build docker/aura-egress` then `AURA_EGRESS_ENFORCE=1 go test -tags docker_integration -race ./cmd/aura/ -run TestBuildSandboxRouter_LaunchesEgressFloor` on a native-Linux non-masquerading dockerd (or under `$CI`). This is the closing live proof for the SBX-04 BLOCKER; recorded in 37-VALIDATION.md's Manual-Only table (Dimension 8, SBX-04). Must run green before Phase 37 Gate-3 close.

## Next Phase Readiness
- SBX-04 is wired + docker-free-regression-proven; `/gsd-verify-work 37` can re-verify the BLOCKER is closed and drive the WSL/CI live tiers.
- Still open for Phase 37 Gate-3: the composition-root live DROP + the backend-level `TestEgress_*` on native Linux, the D-14 32GB concurrency soak, and `/gsd-secure-phase 37`.
- No blockers introduced; zero new dependencies; no schema/migration in scope.

## Self-Check: PASSED

- FOUND: `cmd/aura/serve_dispatch_egress_test.go`
- FOUND: `cmd/aura/serve_dispatch_egress_integration_test.go`
- FOUND: fix commit `bdebc5c9`
- FOUND: `WithEgress(cfg.Sandbox.EgressImage)` wiring in `cmd/aura/serve_dispatch.go`
- FOUND: `func (b *DockerBackend) EgressImage()` accessor in `internal/sandbox/usersandbox/docker_backend.go`

---
*Phase: 37-per-user-full-capability-sandbox*
*Completed: 2026-07-07*
