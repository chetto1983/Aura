---
phase: 37-per-user-full-capability-sandbox
plan: 05
subsystem: infra
tags: [sandbox, routing, gateway-pattern, identity, idle-reaper, scheduler, cron, docker, config, go]

# Dependency graph
requires:
  - phase: 37-per-user-full-capability-sandbox
    provides: "Backend E2B seam + BoxHandle/SandboxSpec/EgressPolicy/Resources + toHostConfig (37-02); DockerBackend Resolve/Suspend/Resume/Stop with transparent-resume-in-Resolve (37-04); KindSandboxReap handler + consumer-declared SandboxReaper seam + 0034 kind-widen migration (37-03); AURA_SANDBOX_* SandboxConfig surface (37-01)"
  - phase: 36-per-user-identity
    provides: "identityctx.IdentityID + the seeded `local` fallback UUID; seedIdentityPurgeSweep template (system-seeded scheduler TaskKind)"
provides:
  - "SandboxRouter + Route(ctx) (BoxHandle, routed bool, err error) — the single get-or-create routing seam: Strict() no-op under dev/local_trusted (routed=false), get-or-create keyed on identityctx.IdentityID under a strict profile, fail-CLOSED (routed=true, err) on box failure"
  - "specFor(id) — builds SandboxSpec sourcing Image/cgroup caps/egress FQDN allowlist from cfg.Sandbox (37-01); Runsc only under server_production (D-12)"
  - "SuspendIdle(ctx, now) (int, error) — the live handlers.SandboxReaper impl over the lastUsed map (no goroutine/ticker); suspends boxes idle past cfg.IdleTTLSec, clears tracking so the next Route auto-resumes inline (D-08)"
  - "Strict() accessor — the 37-07 skill tool selects box-side vs host-side snippet path"
  - "serve wiring: buildSandboxRouter (nil-safe under non-strict/Docker-unavailable), cron.KindSandboxReap registration, seedSandboxReapSweep (cadence from AURA_SANDBOX_IDLE_TTL_SEC), chatEnv.sandboxRouter handle for 37-07"
  - "cron.KindSandboxReap TaskKind ('sandbox_reap') — the cron-store literal mirroring KindIdentityPurge"
affects: [37-07, 37-09, sandbox-tool-routing]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "gateway.Decide short-circuit mirrored: `if r == nil || !r.profile.Strict()` returns the host-direct no-op (routed=false) — hardening never leaks into dev (SC-4)"
    - "fail-CLOSED containment: a routed-but-box-failed call returns (zero, true, err) so the tool denies, never falls back to host (D-09/GATE-01)"
    - "consumer-declared seam satisfied at the composition root: *SandboxRouter satisfies handlers.SandboxReaper structurally; the compile-time proof is the cmd/aura registration, so usersandbox never imports handlers (no reverse cycle)"
    - "idle reaper as scheduler-tick work: SuspendIdle snapshots the idle set under the mutex then Suspends outside the lock — no bespoke goroutine/ticker (goleak-clean)"
    - "typed-nil avoidance: a nil *SandboxRouter is converted to a genuinely-nil handlers.SandboxReaper interface so the handler's own nil-Reaper disabled no-op fires"

key-files:
  created:
    - internal/sandbox/usersandbox/router.go
    - internal/sandbox/usersandbox/reap.go
    - internal/sandbox/usersandbox/router_test.go
    - internal/sandbox/usersandbox/reap_integration_test.go
  modified:
    - internal/cron/store.go
    - cmd/aura/chat_boot.go
    - cmd/aura/serve_dispatch.go
    - cmd/aura/serve_provisioning.go
    - cmd/aura/serve.go
    - cmd/aura/serve_provisioning_test.go

key-decisions:
  - "Added cron.KindSandboxReap ('sandbox_reap') to internal/cron/store.go (NOT in the plan's files_modified list). The plan text literally registers `cron.KindSandboxReap`, and the established identity_purge pattern defines the kind in BOTH cron/store.go and handlers — the cron-store literal is what the dispatch map keys on and what CreateTask writes. A Rule 3 blocking fix to make the plan's own expression resolve, following the existing pattern verbatim."
  - "buildSandboxRouter constructs WITHOUT WithMaterializeSources. The Task 2 action specifies exactly `NewDockerBackend(cli, cfg.Sandbox.Image, limitsFrom(cfg.Sandbox))` and `NewSandboxRouter(...)` and says 'Do NOT wire the tools here (that is 37-07)'. Materialize sources resolve per-identity skills/Agent.md/pyscripts dirs — tool-adjacent wiring — so they land with the tool wiring in 37-07 (a nil resolver makes materialize a no-op; Resolve still succeeds, per 37-04)."
  - "Reap cadence derived from AURA_SANDBOX_IDLE_TTL_SEC (sandboxReapSweepMinutes = idleTTLSec/60, floored at 1m) — the D-08 knob, not a new hardcoded const. Worst-case suspend latency ≈ one TTL window; the box auto-resumes transparently on its next tool call."
  - "Route returns a value BoxHandle (matching Backend.Resolve), not a pointer; the plan's '(nil, ...)' prose is the zero-value BoxHandle{}. The `routed` bool — not a nil check — is the tool's contain-vs-host signal."

patterns-established:
  - "Per-identity routing seam: NewSandboxRouter(backend, profile, cfg) + Route(ctx) as the single get-or-create entry the strict-profile tools call; nil-router = safe host-direct no-op everywhere"

requirements-completed: []

# Coverage metadata
coverage:
  - id: D1
    description: "Route is a proven host-direct no-op under dev/local_trusted (backend never touched) and fail-CLOSED under a strict profile on box failure (routed=true, err!=nil) — the gateway.Decide short-circuit + the D-09/GATE-01 containment invariant."
    requirement: "SBX-01"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/router_test.go#TestRoute_DevNoOp,TestRoute_FailClosed"
        status: pass
    human_judgment: false
  - id: D2
    description: "The local/no-principal identity keys a `local`-id box (never host, never cross-identity); an authenticated principal keys strictly on its own identityctx id."
    requirement: "SBX-01"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/router_test.go#TestRoute_LocalFallback"
        status: pass
    human_judgment: false
  - id: D3
    description: "specFor sources cfg.Sandbox (image/CPU→NanoCPUs/memory/pids/egress allowlist) into the spec — a configured allowlist reaches EgressPolicy.FQDNAllowlist, not just a fixture; Runsc is selected only under server_production."
    requirement: "SBX-04"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/router_test.go#TestSpecFor_UsesConfiguredKnobs"
        status: pass
    human_judgment: false
  - id: D4
    description: "SuspendIdle suspends only boxes idle past cfg.IdleTTLSec, clears their tracking (second sweep is a no-op); the router bumps lastUsed on each Route."
    requirement: "SBX-03"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/router_test.go#TestRoute_IdleBump"
        status: pass
    human_judgment: false
  - id: D5
    description: "Live idle-suspend → transparent auto-resume: Route creates+starts the box, SuspendIdle stops it (suspend-retain), the next Route resumes the SAME container inline (D-08)."
    requirement: "SBX-03"
    verification:
      - kind: integration
        ref: "internal/sandbox/usersandbox/reap_integration_test.go#TestReap_IdleSuspendAutoResume (docker_integration; skips locally without dockerd, t.Fatal under $CI)"
        status: unknown
    human_judgment: true
    rationale: "Compiles + skips cleanly locally (dockerd unreachable in the Windows worktree via npipe). The live suspend→auto-resume pass runs on native-Linux dockerd at phase validation (WSL/CI)."
  - id: D6
    description: "The router + reaper are wired into serve: cron.KindSandboxReap registered with the router as SandboxReaper (nil-router = disabled no-op), seedSandboxReapSweep idempotent with cadence from AURA_SANDBOX_IDLE_TTL_SEC."
    requirement: "SBX-03"
    verification:
      - kind: unit
        ref: "cmd/aura/serve_provisioning_test.go#TestBuildDispatchRegistersSandboxReap,TestSandboxReapSweepMinutes"
        status: pass
    human_judgment: false

# Metrics
duration: ~40 min
completed: 2026-07-07
status: complete
---

# Phase 37 Plan 05: SandboxRouter routing seam + idle-suspend reaper wiring Summary

**`SandboxRouter.Route(ctx)` is the single get-or-create seam the 5 tools route through: it mirrors `gateway.Decide`'s `nil || !Strict()` short-circuit (host-direct no-op under dev/local_trusted, SC-4), get-or-creates the identity's box keyed on `identityctx.IdentityID` (seeded-`local` fallback) under a strict profile, and enforces fail-CLOSED containment (routed-but-box-failed ⇒ `(zero, true, err)`, the tool denies, never host — D-09/GATE-01). `specFor` sources image/cgroup caps/egress FQDN allowlist from `cfg.Sandbox` (37-01) so a configured allowlist reaches `EgressPolicy`; `SuspendIdle` (the live `handlers.SandboxReaper`) sweeps boxes idle past the TTL and Suspends them with transparent inline auto-resume on the next Route (D-08). The router + reaper are wired into `aura serve` — `cron.KindSandboxReap` registered, `seedSandboxReapSweep` seeded with a cadence derived from `AURA_SANDBOX_IDLE_TTL_SEC` — all nil-safe under a non-strict profile or a Docker-unavailable host.**

## Performance

- **Duration:** ~40 min
- **Completed:** 2026-07-07
- **Tasks:** 2
- **Files created:** 4 | **Files modified:** 6

## Accomplishments

- **The routing seam (SBX-01/GATE-01 crux).** `Route(ctx) (BoxHandle, routed bool, err error)` is the exact `gateway.Decide` shape: `if r == nil || !r.profile.Strict()` returns `(zero, false, nil)` — the host-direct no-op preserving the operator's full-host experience unchanged (SC-4/T-37-05-DEVLEAK). Under a strict profile it resolves the box for `identityID(ctx)` (authenticated id, else the seeded `local` UUID — the `tools.ownerFromContext` shape) and bumps `lastUsed`; a `backend.Resolve` failure returns `(zero, true, err)` — routed=true so the tool DENIES, never falls back to host (fail-CLOSED, D-09/GATE-01/T-37-05-FAILOPEN).
- **Config-sourced specFor (SBX-04's real knob).** `specFor(id)` maps `cfg.Sandbox` into the spec — `Image`, `WorkspaceVol=aura-box-<id>`, `Limits{NanoCPUs=CPULimit*1e9, MemoryBytes, PidsLimit}`, `Egress{Floor:true, FQDNAllowlist:cfg.EgressAllowlist}` — with `Runsc` selected only under `server_production` (D-12), every other strict profile `Runc`. A configured allowlist reaches `EgressPolicy.FQDNAllowlist`, not just a test fixture (T-37-05-CONFIGDROP).
- **Live idle reaper (SBX-03 lifecycle).** `SuspendIdle(ctx, now)` is the live `handlers.SandboxReaper`: it snapshots the idle-past-`cfg.IdleTTLSec` set under the router mutex, Suspends OUTSIDE the lock (a moby stop must not block a concurrent Route), and clears each suspended box's tracking so the next `Route` re-resolves and transparently auto-resumes it (inline, never scheduled — D-08). No goroutine/ticker; the sweep runs on the scheduler tick (goleak-clean, T-37-05-LEAK).
- **Serve wiring.** `buildSandboxRouter` constructs the `DockerBackend` + `SandboxRouter` from `cfg.Sandbox`, returning nil (a safe host-direct no-op everywhere) under a non-strict profile or when the Docker client can't be built — a Docker-unavailable host never fails boot. `cron.KindSandboxReap` is registered with the router as the reaper (a nil router becomes a genuinely-nil `SandboxReaper` interface → the handler's disabled no-op). `seedSandboxReapSweep` mirrors `seedIdentityPurgeSweep` (idempotent) with the cadence derived from `AURA_SANDBOX_IDLE_TTL_SEC`. The router handle is retained on `chatEnv.sandboxRouter` for 37-07.

## Task Commits

1. **Task 1: SandboxRouter.Route (Strict no-op + get-or-create + fail-CLOSED) + config-sourced specFor + reap impl** — `94a2db8b` (feat)
2. **Task 2: wire the router (config-sourced) + reaper into serve (register KindSandboxReap, seed the sweep)** — `28432138` (feat)

## Interface Handoff (for 37-07 / 37-09)

```
NewSandboxRouter(backend Backend, profile config.RuntimeProfile, cfg config.SandboxConfig) *SandboxRouter
(r *SandboxRouter) Route(ctx) (BoxHandle, routed bool, err error)   // the single get-or-create seam the tools call
(r *SandboxRouter) Strict() bool                                    // select box-side vs host-side snippet path (37-07)
(r *SandboxRouter) SuspendIdle(ctx, now) (int, error)               // handlers.SandboxReaper (already registered)
```

- **How the router reaches the tools:** the composition root retains the handle on `chatEnv.sandboxRouter` (built in `buildDispatch` via `buildSandboxRouter`). Plan 37-07 reads `chat.sandboxRouter`, calls `Route(ctx)` at the top of each box-capable tool's `Execute`, and branches on `(routed, err)`: `routed=false` ⇒ keep the host os/exec path; `err!=nil` ⇒ DENY (fail-CLOSED, never host); `routed=true, err==nil` ⇒ `backend.Exec` into the returned `BoxHandle`. `Strict()` picks the box-side snippet path.
- **Materialize sources:** `buildSandboxRouter` currently constructs the `DockerBackend` WITHOUT `WithMaterializeSources` (a nil resolver = materialize no-op, Resolve still succeeds). 37-07 adds `WithMaterializeSources` sourcing the identity's skills / Agent.md / pyscripts dirs when it wires the tools (the `identityDirRoots` helper in `serve_provisioning.go` is the path resolver to reuse).

## Decisions Made

- **`cron.KindSandboxReap` added to `internal/cron/store.go`** (not in the plan's `files_modified`). See Deviation 1 — a Rule 3 blocking fix mirroring the identity_purge pattern so the plan's own `cron.KindSandboxReap` expression resolves.
- **No `WithMaterializeSources` in `buildSandboxRouter`** — the Task 2 action specifies the bare 3-arg `NewDockerBackend` and says "Do NOT wire the tools here (that is 37-07)". Materialize sources are tool-adjacent and land with the tool wiring in 37-07.
- **Reap cadence derived from the TTL knob** (`sandboxReapSweepMinutes`, floored at 1m), not a new hardcoded const — per the plan's explicit "cadence derived from cfg.Sandbox.IdleTTLSec".
- **Typed-nil avoidance at the registration** — a nil `*SandboxRouter` is assigned to a `var sandboxReaper handlers.SandboxReaper` only when non-nil, so a nil router yields a genuinely-nil interface and the handler's own `Reaper == nil` disabled-no-op path fires (matches the identity_purge note; also the compile-time proof that `*SandboxRouter` satisfies the seam lives at the composition root, keeping usersandbox free of a handlers import).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Added `cron.KindSandboxReap` to `internal/cron/store.go`**
- **Found during:** Task 2 (dispatch-map registration).
- **Issue:** The plan registers `cron.KindSandboxReap` and seeds a `cron.KindSandboxReap` task, but only `handlers.KindSandboxReap` existed (37-03). The cron dispatch map is keyed on `cron.TaskKind`, and `CreateTaskParams.Kind`/`ListActiveTasks` compare against the cron-store literal — so the plan's own expression does not resolve without a `cron.KindSandboxReap` constant. `internal/cron/store.go` is not in the plan's `files_modified` list.
- **Fix:** Added `KindSandboxReap TaskKind = "sandbox_reap"` to the cron const block, mirroring `KindIdentityPurge` verbatim (its literal MUST equal `handlers.KindSandboxReap`, exactly as the store's own doc comment mandates for identity_purge). This follows the established pattern rather than inventing a `cron.TaskKind(handlers.KindSandboxReap)` conversion.
- **Files modified:** internal/cron/store.go
- **Verification:** `go build ./...`, `go vet ./...`, `go test ./internal/cron/` green; `TestBuildDispatchRegistersSandboxReap` asserts `entry.Meta().Kind == cron.KindSandboxReap`.
- **Committed in:** `28432138` (Task 2).

---

**Total deviations:** 1 auto-fixed (Rule 3 — blocking, follows the existing identity_purge pattern). **Impact:** No scope change; the plan's `files_modified` should additionally list `internal/cron/store.go` and `cmd/aura/serve_provisioning_test.go` (the mirrored registration test).

## Requirements Status

- **SBX-01** (full-capability box; tools executing INSIDE the box) — this plan delivers the ROUTING SEAM (the no-op/fail-CLOSED/identity-keyed `Route`) but NOT the tool interposition itself (37-07). SBX-01 stays open, consistent with 37-02/37-04.
- **SBX-03** (per-identity lifecycle incl. idle-suspend) — the idle-suspend leg is delivered live here (`SuspendIdle` on the scheduler + transparent auto-resume). SBX-03 is a multi-plan requirement; left for the orchestrator/verifier to reconcile at phase completion.
- **SBX-04** (configured egress allowlist) — `specFor` sources `cfg.Sandbox.EgressAllowlist` into `EgressPolicy.FQDNAllowlist` (the config→spec leg proven by `TestSpecFor_UsesConfiguredKnobs`); the nftables enforcement leg is 37-06. Left open.
- `requirements-completed: []` — REQUIREMENTS.md untouched (orchestrator/verifier reconciles the multi-plan SBX requirements after the wave).

## Known Stubs

None. `buildSandboxRouter` intentionally omits `WithMaterializeSources` (a documented forward seam for 37-07, not a stub — a nil resolver makes materialize a no-op and Resolve still succeeds per 37-04). The router is fully functional under a strict profile with a reachable Docker daemon.

## Threat Flags

None — no new trust-boundary surface beyond the plan's `<threat_model>`. The router interposes exactly the tool→router / identityctx→box-key / config→spec boundaries the plan enumerates, and each has its mitigation test (FailClosed / LocalFallback / SpecFor / DevNoOp).

## Issues Encountered

- **Live docker_integration run deferred to CI/WSL.** dockerd is unreachable in this Windows worktree (npipe is not stdlib-dialable), so `TestReap_IdleSuspendAutoResume` skips locally (the sanctioned local skip; `t.Fatal` under `$CI`). The suite compiles under `-tags docker_integration`; the live suspend→auto-resume green runs at phase validation on the native-Linux stack.

## Next Phase Readiness

- **37-07** (tool routing / send_file) has `chat.sandboxRouter` + `Route`/`Strict`/`Exec` to interpose the 5 box-capable tools and can add `WithMaterializeSources`.
- **37-06** (egress) is unaffected by this plan (separate files: egress.go / docker_backend.go). `specFor` already feeds `cfg.Sandbox.EgressAllowlist` into the spec its sidecar enforces.
- Blockers: none. The live docker_integration + `-race` + goleak run is a phase-validation (WSL/CI) step, not a code blocker.

## Self-Check: PASSED

- Created files exist: `router.go`, `reap.go`, `router_test.go`, `reap_integration_test.go` — all FOUND on disk.
- Task commits exist: `94a2db8b`, `28432138` — both FOUND in `git log`.
- Plan `<verification>` re-run: `go build ./...` green; `go vet ./...` clean; `go test ./internal/sandbox/usersandbox/` (5 unit tests) green; `go build/vet -tags docker_integration ./internal/sandbox/usersandbox/` green + reap integration test skips cleanly locally; `go build ./cmd/aura/` green; `go test ./cmd/aura/ -run "SandboxReap|Seed"` green; `go test ./internal/cron/` green. No file > 600 LOC (largest touched: serve.go 593).

---
*Phase: 37-per-user-full-capability-sandbox*
*Completed: 2026-07-07*
