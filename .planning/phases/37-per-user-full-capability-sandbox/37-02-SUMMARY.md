---
phase: 37-per-user-full-capability-sandbox
plan: 02
subsystem: infra
tags: [sandbox, docker, moby, security, unrepresentability, e2b, rapid, gvisor]

# Dependency graph
requires:
  - phase: 37-per-user-full-capability-sandbox
    provides: "RuntimeProfile enum + Strict() (internal/config/config_runtimeprofile.go) — the named-const discipline RuntimeClass mirrors and the server_production gate for Runsc"
provides:
  - "internal/sandbox/usersandbox package (greenfield)"
  - "SandboxSpec — the ONLY constructible box-config type; host-exposure escapes are unrepresentable (SBX-02, fully closed)"
  - "toHostConfig — the single private SandboxSpec→moby HostConfig translator pinning the dangerous fields safe as literals present nowhere else"
  - "RuntimeClass typed enum {Runc, Runsc} with the server_production-only Runsc gate (NewSandboxSpec)"
  - "Backend interface (E2B verbs Resolve/Exec/Suspend/Resume/Stop) + BoxHandle/ExecRequest/ExecResult — the seam 37-04 (DockerBackend) and the DGX agent-sandbox E2B gateway implement"
  - "docker_integration build-tag skip-helper (skipUnlessDockerd) — CI-fatal, local-skip dockerd gate shared by all downstream integration tiers"
affects: [37-04, 37-05, 37-06, 37-07, 37-09]

# Tech tracking
tech-stack:
  added: []  # no new module deps — moby/moby/api (v1.54.2, already indirect) promoted to a direct types consumer; go.mod deliberately untouched
  patterns:
    - "Unrepresentability over validation (SBX-02): omit the escape field entirely rather than reject it at runtime"
    - "Pin-in-one-place: the dangerous moby HostConfig literals live ONLY in translate.go"
    - "Typed non-string enum (int-backed) so a runtime string like \"host\" is structurally impossible"
    - "Consumer-declared seam interface (Backend) so the Docker/E2B backends drop in behind it"
    - "No-skip-as-green build-tag skip-helper (t.Fatal under $CI, t.Skip locally)"

key-files:
  created:
    - "internal/sandbox/usersandbox/spec.go — SandboxSpec/RuntimeClass/EgressPolicy/Resources + NewSandboxSpec Runsc gate"
    - "internal/sandbox/usersandbox/translate.go — private toHostConfig with the pinned-safe HostConfig"
    - "internal/sandbox/usersandbox/backend.go — Backend E2B-verb interface + BoxHandle/ExecRequest/ExecResult"
    - "internal/sandbox/usersandbox/spec_test.go — TestSpec_NoHostExposureFields + TestSpec_RunscOnlyServerProduction"
    - "internal/sandbox/usersandbox/translate_test.go — TestTranslate_PinsSafe (table + rapid, ≥1000 adversarial specs)"
    - "internal/sandbox/usersandbox/dockertest_support.go — docker_integration skipUnlessDockerd gate"
  modified: []

key-decisions:
  - "RuntimeClass is an INT-backed enum (not string) — strengthens the prohibition 'must not carry host' to structural impossibility; dockerRuntime() is a total map to \"\"/\"runsc\"."
  - "NewSandboxSpec(spec, profile) validates-and-returns rather than a 7-arg builder — no field duplication (NO DUP), same Runsc gate."
  - "The docker_integration skip-helper probes dockerd via stdlib (net.Dial), NOT the moby client — the client binding is 37-04's per the orchestrator's wave-scope directive."
  - "Only SBX-02 marked complete; SBX-01 stays open (its acceptance — tools executing inside the box — is the tool-routing plans 37-05/37-07)."

patterns-established:
  - "SBX-02 unrepresentability: SandboxSpec has no privileged/host-net/bind/device/cap/socket field; a golden-field-list + forbidden-token + no-moby-type reflection test fails the moment one is added."
  - "SBX-02 pinned translator: toHostConfig(SandboxSpec) sets Privileged=false, NetworkMode \"\", Binds=nil, AutoRemove=false, CapDrop empty, and volume/tmpfs-only Mounts — the docker socket (a host path) has no bind vector, so it is unrepresentable."

requirements-completed: [SBX-02]

coverage:
  - id: D1
    description: "SandboxSpec makes host-exposure escapes structurally unrepresentable (no privileged/host-net/bind/device/cap/socket field)"
    requirement: "SBX-02"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/spec_test.go#TestSpec_NoHostExposureFields"
        status: pass
    human_judgment: false
  - id: D2
    description: "toHostConfig pins every dangerous moby HostConfig field safe for any adversarial spec (Privileged=false, non-host NetworkMode, Binds=nil, AutoRemove=false, safe Runtime, no bind/socket mount)"
    requirement: "SBX-02"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/translate_test.go#TestTranslate_PinsSafe (table + rapid, 1000 adversarial specs)"
        status: pass
    human_judgment: false
  - id: D3
    description: "RuntimeClass is a typed non-string enum; Runsc is accepted only under server_production and runc never becomes runsc"
    requirement: "SBX-02"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/spec_test.go#TestSpec_RunscOnlyServerProduction"
        status: pass
    human_judgment: false
  - id: D4
    description: "Backend interface exposes exactly the five 082-corrected E2B verbs (Resolve/Exec/Suspend/Resume/Stop) + BoxHandle/ExecRequest/ExecResult, ready for the 37-04 DockerBackend and DGX E2B drop-in"
    verification:
      - kind: other
        ref: "go build ./internal/sandbox/usersandbox/ && go doc ./internal/sandbox/usersandbox Backend (5 verbs, exported types)"
        status: pass
    human_judgment: true
    rationale: "A pure seam/contract with no logic — compilation proves it exists but no behavioral test asserts the exact verb set; a reviewer confirms it matches the 082-corrected E2B contract (D-02)."
  - id: D5
    description: "docker_integration skip-helper (skipUnlessDockerd) gates the tier on a reachable dockerd — t.Skip locally, t.Fatal under $CI (no-skip-as-green)"
    verification:
      - kind: other
        ref: "go build -tags docker_integration ./internal/sandbox/usersandbox/ (compiles under the tag)"
        status: pass
    human_judgment: true
    rationale: "The $CI-fatal branch is code-visible but not exercised by a wave-1 unit test (no live dockerd in this tier); a reviewer confirms the no-skip-as-green branch before the integration tiers rely on it."

# Metrics
duration: ~40 min
completed: 2026-07-06
status: complete
---

# Phase 37 Plan 02: Per-User Sandbox Type Layer (SBX-02) Summary

**SBX-02 closed at the type layer: a `SandboxSpec` with no host-exposure field, a single private `toHostConfig` that pins moby's dangerous `HostConfig` fields safe as literals present nowhere else (proven over 1000 rapid-generated adversarial specs), plus the E2B-verb `Backend` seam and a no-skip-as-green `docker_integration` gate — all stdlib/existing-deps, no moby client.**

## Performance

- **Duration:** ~40 min
- **Started:** 2026-07-06T21:56:00Z (approx)
- **Completed:** 2026-07-06T22:35:59Z
- **Tasks:** 2
- **Files created:** 6

## Accomplishments
- **SBX-02 fully closed (structural + behavioral).** `SandboxSpec` carries only `{IdentityID, Image, WorkspaceVol, Runtime, Egress, Limits}` — there is no `Privileged`, `NetworkMode`, `Binds`, `Devices`, `CapAdd`, or docker-socket field, so host re-exposure cannot be *set*, not merely rejected. A golden-field-list + forbidden-token + no-moby-type reflection test fails the instant an escape field is added.
- **The single pinned translator.** `toHostConfig` (translate.go, private) is the only place a moby `HostConfig` is built; it pins `Privileged=false`, `NetworkMode ""` (never host), `Binds=nil`, `AutoRemove=false`, `CapDrop` empty (D-12), and mounts ONLY the per-identity workspace volume + tmpfs scratch + shared uv-cache volume. The docker socket is a host path whose sole mount vector is a bind — which never appears — so it is unrepresentable. `TestTranslate_PinsSafe` proves all four STRIDE mitigations (T-37-02-SOCKET/PRIV/BIND/RUNSC) over a 5-case table + 1000 rapid adversarial specs.
- **RuntimeClass enum + server_production gate.** `RuntimeClass` is an int-backed enum `{Runc, Runsc}` (never a free string that could carry `"host"`); `NewSandboxSpec` rejects `Runsc` outside `server_production` with `ErrRunscRequiresServerProduction`.
- **Backend E2B seam.** The `Backend` interface exposes exactly the five spike-082-corrected verbs (`Resolve`/`Exec`/`Suspend`/`Resume`/`Stop`) plus `BoxHandle`/`ExecRequest`/`ExecResult` — the contract 37-04's `DockerBackend` and the DGX agent-sandbox E2B gateway both implement. No implementation here (that is 37-04).
- **Shared `docker_integration` gate.** `skipUnlessDockerd` skips locally but `t.Fatal`s under `$CI` when dockerd is unreachable — the no-skip-as-green scaffolding every downstream integration tier consumes.

## Interface Handoff (for 37-04 / 37-05)

**SandboxSpec (spec.go):**
```
SandboxSpec{ IdentityID, Image, WorkspaceVol string; Runtime RuntimeClass; Egress EgressPolicy; Limits Resources }
RuntimeClass  int enum { Runc, Runsc }   // dockerRuntime(): Runc→"", Runsc→"runsc"
EgressPolicy{ Floor bool; FQDNAllowlist []string }
Resources{ NanoCPUs, MemoryBytes, PidsLimit int64 }
NewSandboxSpec(spec SandboxSpec, profile config.RuntimeProfile) (SandboxSpec, error)  // Runsc gated to server_production
```

**Backend (backend.go):**
```
Resolve(ctx, spec SandboxSpec) (BoxHandle, error)     // idempotent DIRECT create (not Claim) + transparent Resume (D-02/D-08)
Exec(ctx, h BoxHandle, req ExecRequest) (ExecResult, error)
Suspend(ctx, h BoxHandle) error                        // OperatingMode:Suspended — retain box+volume
Resume(ctx, h BoxHandle) error
Stop(ctx, h BoxHandle) error                           // ShutdownPolicy:Delete — destroy box+volume
BoxHandle{ ContainerID, IdentityID string }
ExecRequest{ Command, Dir string; Env []string }
ExecResult{ Stdout, Stderr []byte; ExitCode int }
```

## Task Commits

1. **Task 1 (TDD RED): failing SBX-02 tests** — `619a8998` (test)
2. **Task 1 (TDD GREEN): SandboxSpec + translator** — `29900545` (feat)
3. **Task 2: Backend seam + docker_integration skip-helper** — `e094172c` (feat)

_TDD gate: `test(37-02)` (619a8998) precedes `feat(37-02)` (29900545). No refactor commit needed._

## Files Created/Modified
- `internal/sandbox/usersandbox/spec.go` — SandboxSpec + RuntimeClass/EgressPolicy/Resources + NewSandboxSpec Runsc gate
- `internal/sandbox/usersandbox/translate.go` — private toHostConfig, the single pinned-safe HostConfig
- `internal/sandbox/usersandbox/backend.go` — Backend E2B-verb interface + BoxHandle/ExecRequest/ExecResult
- `internal/sandbox/usersandbox/spec_test.go` — structural + Runsc-gate tests
- `internal/sandbox/usersandbox/translate_test.go` — table + rapid pin-safety property
- `internal/sandbox/usersandbox/dockertest_support.go` — docker_integration CI-fatal dockerd gate
- `.planning/REQUIREMENTS.md` — SBX-02 checked complete

## Decisions Made
- **RuntimeClass int-backed enum (not string):** the prohibition "must not carry host" is satisfied structurally — no string value exists — rather than by a check. `dockerRuntime()` is a total map (`Runc`/unknown → `""`, `Runsc` → `"runsc"`).
- **NewSandboxSpec(spec, profile) validate-and-return:** avoids a 7-arg builder and field duplication while enforcing the identical Runsc gate.
- **Only SBX-02 marked complete:** SBX-01's acceptance (host tools actually executing inside the box, routed by `SandboxRouter`) is delivered by the later tool-routing plans; this plan lays only its Backend/spec seam.

## Deviations from Plan

### Auto-fixed / scope-aligned Issues

**1. [Rule 3 - Scope alignment] docker_integration skip-helper uses a stdlib dockerd probe, not the moby client `Ping`**
- **Found during:** Task 2 (Backend seam + skip-helper)
- **Issue:** The plan's Task 2 prose says the helper "pings dockerd (moby client Ping)", but the orchestrator's wave-scope directive is explicit: the moby *client* import belongs to 37-04 (Wave 2), and this plan must stay client-free (production code uses only the moby `api/types` for translate.go).
- **Fix:** `skipUnlessDockerd` resolves the dockerd endpoint from `DOCKER_HOST` (unix/tcp, default socket) and probes it with `net.DialTimeout` — satisfying the acceptance contract (compiles under the tag, `t.Skip` locally, `t.Fatal` under `$CI`) without binding the moby client. The real moby-client `Ping` is left to 37-04's docker_integration smoke.
- **Files modified:** internal/sandbox/usersandbox/dockertest_support.go
- **Verification:** `go build -tags docker_integration ./internal/sandbox/usersandbox/` passes; the `$CI` t.Fatal branch is code-visible.
- **Committed in:** e094172c

**2. [Rule 3 - Blocking] TDD RED committed with compiling stubs (not a build-failing RED)**
- **Found during:** Task 1 (RED)
- **Issue:** The repo's pre-commit `go vet` hook forbids a compile-breaking commit, so a pure "undefined-symbol" RED cannot be committed. (The build failure WAS demonstrated by running the tests before any implementation.)
- **Fix:** The RED commit adds the tests plus type/signature scaffolding whose logic is stubbed (`NewSandboxSpec` returns without the gate; `toHostConfig` returns an empty HostConfig), so it compiles and the two behavioral tests fail. GREEN fills in the logic. This matches the plan's own wording: "the tests must compile and FAIL." 
- **Files modified:** internal/sandbox/usersandbox/spec.go, translate.go (stub→real between RED and GREEN)
- **Verification:** RED: `go vet` clean + behavioral tests fail; GREEN: all three tests pass.
- **Committed in:** 619a8998 (RED) → 29900545 (GREEN)

---

**Total deviations:** 2 (both Rule 3 scope/blocking alignment). **Impact:** No scope creep and no loss of coverage — the skip-helper's CI-fatal contract is fully met without the client, and the RED→GREEN sequence is preserved. RuntimeClass-as-int and the 2-arg constructor are design choices within the plan's "named-const discipline"/"constructor or validation" latitude (recorded under Decisions, not deviations).

## Issues Encountered
- **`go test -race` unavailable in this sandbox (no gcc / CGO).** Per CLAUDE.md, native `-race` runs in WSL/CI Linux. This package is pure data transformation (no goroutines, channels, or shared mutable state), so the detector has nothing to find by construction; untagged race is clean and the enforced `-race` gate runs in CI.

## Known Stubs
None. The RED stubs were replaced in GREEN (spec.go/translate.go are fully implemented). `backend.go` is an interface-only seam by design (its `DockerBackend` implementation is 37-04), not a placeholder feeding fake data.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- **SBX-02 is fully closed** at the type layer; the four STRIDE mitigations in the plan's threat register (SOCKET/PRIV/BIND/RUNSC) are implemented and test-asserted.
- **37-04 (DockerBackend)** implements `Backend` using `github.com/moby/moby/client` (the first client-binding site, per wave scope) + `toHostConfig`, and reuses `skipUnlessDockerd` for its live round-trip smoke.
- **37-05/37-07** wire the five tools through a `SandboxRouter` over this `Backend` — that routing is what closes **SBX-01** (left open here).
- go.mod is deliberately untouched (no dependency on 37-01's changes); `go build ./...` and `go vet ./...` are green across the module.

## Self-Check: PASSED
- Files created exist: spec.go, translate.go, backend.go, spec_test.go, translate_test.go, dockertest_support.go — all FOUND.
- Task commits exist: 619a8998, 29900545, e094172c — all FOUND.
- Plan verification green: `go test ./internal/sandbox/usersandbox/` ok; `TestSpec_NoHostExposureFields` ok; `go build -tags docker_integration ./internal/sandbox/usersandbox/` ok; pins (`Privileged:`/`NetworkMode:`/`AutoRemove:`) appear only in translate.go.

---
*Phase: 37-per-user-full-capability-sandbox*
*Completed: 2026-07-06*
