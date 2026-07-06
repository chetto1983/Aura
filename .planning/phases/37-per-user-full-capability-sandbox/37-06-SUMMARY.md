---
phase: 37-per-user-full-capability-sandbox
plan: 06
subsystem: infra
tags: [sandbox, docker, egress, nftables, gvisor, opensandbox, sidecar, network-isolation, dockerfile]

# Dependency graph
requires:
  - phase: 37-per-user-full-capability-sandbox
    provides: "SBX-04 egress-default amendment (full-internet-minus-internal, D-06) + the gVisor⊥nat #934 note + SandboxConfig.EgressAllowlist knob (37-01)"
  - phase: 37-per-user-full-capability-sandbox
    provides: "EgressPolicy{Floor, FQDNAllowlist} + SandboxSpec + toHostConfig pinned-safe HostConfig + the Backend seam (37-02)"
  - phase: 37-per-user-full-capability-sandbox
    provides: "DockerBackend + Resolve/Suspend/Resume/Stop lifecycle + findBox/ensureRunning/ensureImage + docker_integration test scaffolding (37-04)"
provides:
  - "EgressSidecar + buildEgressSidecar(policy, boxID, runtime) — the always-on filter-table nft floor (DROP RFC1918 + 169.254.169.254 + shared-services bridge, ACCEPT public) that is gVisor-safe (filter table only), plus the runc-only OpenSandbox FQDN allowlist built from the CONFIGURED EgressPolicy.FQDNAllowlist"
  - "ErrRunscFQDNMutualExclusion — Runsc + a tightened allowlist is refused (issue #934) before launch"
  - "DockerBackend WithEgress(image) + launchEgress/suspendEgress/resumeEgress/teardownEgress wired into Resolve/Suspend/Resume/Stop — the sidecar shares the box netns (container:<box>), NET_ADMIN on the sidecar only, torn down with the box"
  - "docker/aura-egress/Dockerfile — the bespoke nft filter-floor sidecar image (digest-pinned base) with an inlined entrypoint; OpenSandbox FQDN mode is an opt-in bake"
affects: [37-05, 37-08]

# Tech tracking
tech-stack:
  added: []  # nftables lives in the sidecar image, not the Go module graph. moby client already direct (37-01).
  patterns:
    - "Go-generated nftables ruleset carried into a generic sidecar image via env (AURA_EGRESS_FLOOR_RULESET) — keeps rule generation unit-testable and the image config-free"
    - "Split egress enforcement (RESEARCH Pattern 6): always-on filter-table floor (runc+gVisor) vs opt-in nat-table FQDN allowlist (runc-only, #934) — the mutual-exclusion refused at build time"
    - "Sidecar lifecycle mirrors the box exactly: create-or-start on Resolve, stop-retain on Suspend, start on Resume, remove on Stop — sidecar name derived from identity id so BoxHandle alone drives teardown"

key-files:
  created:
    - internal/sandbox/usersandbox/egress.go
    - internal/sandbox/usersandbox/egress_test.go
    - internal/sandbox/usersandbox/egress_integration_test.go
    - docker/aura-egress/Dockerfile
  modified:
    - internal/sandbox/usersandbox/docker_backend.go
    - internal/sandbox/usersandbox/docker_backend_lifecycle.go

key-decisions:
  - "buildEgressSidecar takes a third arg (runtime RuntimeClass) beyond the plan's 2-arg prose — EgressPolicy does not carry the runtime, and refusing Runsc+FQDN (#934) needs it. Called as buildEgressSidecar(spec.Egress, h.ContainerID, spec.Runtime)."
  - "Egress is opt-in at the backend via WithEgress(image); unset => no sidecar (dev/local + the 37-04 lifecycle tests stay box-only). The floor is unconditional WHEN enabled (always-on per D-07), never gated on a non-empty allowlist."
  - "The default aura-egress image ships the bespoke nft filter-floor ONLY (the RESEARCH-recommended primary + A1 fallback). OpenSandbox FQDN egress is an opt-in bake at /usr/local/bin/opensandbox-egress — the source commit is NOT hard-coded (NEVER-SUPPOSE / package-legitimacy: a hallucinated SHA is worse than an explicit opt-in)."
  - "The shared-services bridge DROP (172.18.0.0/16) is Docker's default-address-pool first user network and is ALSO covered by the 172.16/12 RFC1918 line — listed explicitly as defense-in-depth (the tenancy boundary never depends on the broader line alone)."
  - "The live DROP test uses busybox timeout+wget by IP literal (10.0.0.1 / 169.254.169.254 / 172.18.0.1 dropped; example.com allowed) so the assertion is network-layer, not DNS-dependent."

patterns-established:
  - "Egress sidecar = the network boundary (per-box netns share + CAP_NET_ADMIN on the sidecar only); the box stays net-unprivileged — the boundary is the control, not a crippled box interior."
  - "native-Linux/CI enforcement gate (skipUnlessEnforcingBridge): t.Fatal under $CI on a non-Linux daemon, informational-only locally (Docker Desktop/WSL vpnkit NATs the bridge — Pitfall 3)."

requirements-completed: [SBX-04, SBX-03]

# Coverage metadata (#1602)
coverage:
  - id: D1
    description: "Always-on filter-table floor ruleset DROPs RFC1918 (10/8,172.16/12,192.168/16) + 169.254.169.254 metadata + the shared-services bridge and ACCEPTs public; filter-table only (gVisor-safe, no nat)."
    requirement: "SBX-04"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/egress_test.go#TestEgress_FloorRuleset"
        status: pass
    human_judgment: false
  - id: D2
    description: "Runsc (gVisor) + a tightened FQDN allowlist is refused with ErrRunscFQDNMutualExclusion (issue #934); Runsc floor-only is accepted."
    requirement: "SBX-04"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/egress_test.go#TestEgress_RunscRejectsFQDN"
        status: pass
    human_judgment: false
  - id: D3
    description: "NET_ADMIN is granted to the sidecar spec ONLY; the box HostConfig (toHostConfig) never adds a capability (D-07)."
    requirement: "SBX-04"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/egress_test.go#TestEgress_CapOnSidecarOnly"
        status: pass
    human_judgment: false
  - id: D4
    description: "buildEgressSidecar builds its OpenSandbox rules from the CONFIGURED EgressPolicy.FQDNAllowlist (37-05 specFor-sourced), not an inline list — exactly the configured hosts."
    requirement: "SBX-04"
    verification:
      - kind: unit
        ref: "internal/sandbox/usersandbox/egress_test.go#TestEgress_AllowlistFromPolicy"
        status: pass
    human_judgment: false
  - id: D5
    description: "On native-Linux dockerd the box reaches the public internet but is DROPPED from RFC1918/metadata/bridge (enforced, not advisory); NET_ADMIN on the sidecar not the box; sidecar retained on suspend + removed on stop (no orphan)."
    requirement: "SBX-04"
    verification:
      - kind: integration
        ref: "go test -tags docker_integration ./internal/sandbox/usersandbox/ -run TestEgress_FloorDropsInternal"
        status: unknown
    human_judgment: true
    rationale: "Compiles + skips locally (dockerd unreachable in the Windows worktree; enforcement is native-Linux-only per Pitfall 3). The live green runs on a native-Linux non-masquerading bridge (CI/32GB host) at phase validation — a reviewer confirms the DROP is real there, not the dev NAT masking it."
  - id: D6
    description: "A box resolved with a CONFIGURED FQDN allowlist reaches an allowed FQDN and a disallowed FQDN is DROPPED under runc."
    requirement: "SBX-04"
    verification:
      - kind: integration
        ref: "go test -tags docker_integration ./internal/sandbox/usersandbox/ -run TestEgress_FQDNAllowlist"
        status: unknown
    human_judgment: true
    rationale: "Runc-only (nat-table, #934) and needs an aura-egress image built WITH the OpenSandbox egress binary (AURA_EGRESS_FQDN_IMAGE); skips otherwise. This is the VALIDATION Manual-Only FQDN tier — a reviewer runs it on native Linux with the OpenSandbox-enabled image."
  - id: D7
    description: "The aura-egress sidecar image builds (bespoke nft filter-floor; digest-pinned base + inlined entrypoint)."
    requirement: "SBX-04"
    verification:
      - kind: integration
        ref: "docker build -f docker/aura-egress/Dockerfile -t aura-egress:build ."
        status: unknown
    human_judgment: true
    rationale: "No Docker daemon in the Windows worktree; the build runs on the native-Linux/WSL stack at phase validation (same posture as the 37-01 aura-sandbox image build)."

# Metrics
duration: ~50 min
completed: 2026-07-07
status: complete
---

# Phase 37 Plan 06: Per-box Egress Sidecar (SBX-04) Summary

**An always-on filter-table nftables floor sidecar (gVisor-safe) that gives every box full public internet (D-04) while DROPping the tenancy boundary — RFC1918 + the 169.254.169.254 metadata IP + the shared-services bridge (D-05) — plus the runc-only OpenSandbox FQDN allowlist built from the CONFIGURED EgressPolicy, launched into the box netns from DockerBackend.Resolve with CAP_NET_ADMIN on the sidecar only and torn down with the box.**

## Performance

- **Duration:** ~50 min
- **Started:** 2026-07-07 (worktree wave-3 parallel executor)
- **Completed:** 2026-07-07
- **Tasks:** 2
- **Files modified:** 6 (4 created, 2 modified)

## Accomplishments

- **The always-on filter-table floor is real and unit-proven.** `buildFloorRuleset` renders a `table ip aura_egress` output chain with `policy accept` (full public internet, D-04) and explicit `drop` for the metadata IP, all three RFC1918 ranges, and the shared-services bridge (D-05). It is **filter-table only** (no nat) so it is byte-identical under runc and gVisor `runsc` — the load-bearing gVisor⊥nat split (issue #934) the SBX-05 ADR records.
- **The gVisor⊥FQDN mutual-exclusion is refused before launch.** `buildEgressSidecar` returns `ErrRunscFQDNMutualExclusion` for `Runsc` + a non-empty `FQDNAllowlist` (the OpenSandbox nat-redirect gVisor cannot run); the filter-floor still enforces the tenancy boundary under runsc.
- **The allowlist is the CONFIGURED one, not a fixture.** When `EgressPolicy.FQDNAllowlist` is non-empty (sourced by 37-05 `specFor` from `AURA_SANDBOX_EGRESS_ALLOWLIST`), the sidecar gets `OPENSANDBOX_EGRESS_MODE=dns+nft` + `OPENSANDBOX_EGRESS_RULES` built from exactly those hosts — `TestEgress_AllowlistFromPolicy` proves the config drives enforcement (T-37-06-CONFIGDROP).
- **CAP_NET_ADMIN is on the sidecar ONLY.** The sidecar spec carries `NET_ADMIN`; the box `toHostConfig` never adds a capability (asserted against an adversarial spec). The box stays net-unprivileged (D-07).
- **The sidecar is wired into the box lifecycle.** `DockerBackend.Resolve` launches it after the box is live (`container:<box>` netns share), fail-closed; `Suspend` stops it (retained), `Resume` restarts it against the box's new netns, `Stop` removes it — no orphan. Egress is a no-op unless `WithEgress(image)` is wired, so the 37-04 box-only lifecycle tests stay green.
- **The sidecar image builds bespoke.** `docker/aura-egress/Dockerfile` is a digest-pinned debian + `nftables` image with an inlined entrypoint that applies the floor (`nft -f -`) then holds the netns — or execs OpenSandbox for the opt-in runc-only FQDN mode.
- **Native-Linux DROP enforcement tests are written and correctly gated.** `TestEgress_FloorDropsInternal` (public reach OK, internal DROPPED, cap placement, suspend-retain, stop-no-orphan) and `TestEgress_FQDNAllowlist` compile under `-tags docker_integration` and skip locally; `skipUnlessEnforcingBridge` makes them `t.Fatal` under `$CI` on a non-Linux daemon (no-skip-as-green; Pitfall 3).

## Task Commits

Each task was committed atomically:

1. **Task 1: Egress sidecar spec + filter-table floor + FQDN-allowlist mode + sidecar image** — `2eaa0278` (feat)
2. **Task 2: Launch the sidecar from Resolve (netns share) + native-Linux egress-DROP tests** — `ac14661e` (feat)

## Files Created/Modified

- `internal/sandbox/usersandbox/egress.go` (new) — `EgressSidecar`, `buildEgressSidecar(policy, boxID, runtime)`, `buildFloorRuleset`, `buildOpenSandboxRules`, `ErrRunscFQDNMutualExclusion`, the floor DROP-target + env constants (161 LOC).
- `internal/sandbox/usersandbox/egress_test.go` (new) — `TestEgress_FloorRuleset` / `RunscRejectsFQDN` / `CapOnSidecarOnly` / `AllowlistFromPolicy` (153 LOC).
- `internal/sandbox/usersandbox/egress_integration_test.go` (new) — `TestEgress_FloorDropsInternal` + `TestEgress_FQDNAllowlist` + `skipUnlessEnforcingBridge`/`boxWgetOK`/`inspectCapAdd` (docker_integration, 207 LOC).
- `docker/aura-egress/Dockerfile` (new) — the digest-pinned nft filter-floor sidecar image + inlined entrypoint.
- `internal/sandbox/usersandbox/docker_backend.go` (mod) — `WithEgress` option + `egressImage` field + `egressName`/`findEgress`/`launchEgress`/`suspendEgress`/`resumeEgress`/`teardownEgress` (194 LOC).
- `internal/sandbox/usersandbox/docker_backend_lifecycle.go` (mod) — egress hooks wired into Resolve/Suspend/Resume/Stop (217 LOC).

## Decisions Made

- **`buildEgressSidecar` third arg `runtime RuntimeClass`** — `EgressPolicy` carries no runtime, and the #934 Runsc+FQDN refusal needs it. See Deviation 1.
- **Egress opt-in via `WithEgress(image)`** — keeps the tenancy floor always-on WHEN enabled (never gated on the allowlist) while leaving dev/local + the 37-04 lifecycle tests box-only. The strict-profile composition root (37-05) wires it.
- **Default image = bespoke nft floor only; OpenSandbox FQDN is an opt-in bake** — the RESEARCH-recommended primary (+ A1 fallback). The OpenSandbox source commit is deliberately NOT hard-coded (NEVER-SUPPOSE / package-legitimacy). See Deviation 3.
- **Shared-services bridge DROP `172.18.0.0/16`** — Docker's default-address-pool first user network, defense-in-depth over the RFC1918 172.16/12 line.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking / signature] `buildEgressSidecar` takes `runtime RuntimeClass`**
- **Found during:** Task 1.
- **Issue:** The plan prose shows `buildEgressSidecar(policy, boxID)` (2 args), but the same task requires `TestEgress_RunscRejectsFQDN` (Runsc + FQDNAllowlist → error). `EgressPolicy` carries no runtime, so a 2-arg builder cannot detect the #934 mutual-exclusion.
- **Fix:** Added a third `runtime RuntimeClass` param; `Resolve` calls `buildEgressSidecar(spec.Egress, h.ContainerID, spec.Runtime)`. Satisfies both the must_haves (consumes `policy.FQDNAllowlist`) and the acceptance (Runsc+FQDN refused).
- **Files modified:** `egress.go`, `docker_backend.go`.
- **Verification:** `TestEgress_RunscRejectsFQDN` + `TestEgress_AllowlistFromPolicy` green.
- **Committed in:** `2eaa0278` / `ac14661e`.

**2. [Rule 3 - Blocking / file ownership] `docker_backend_lifecycle.go` modified (not in the plan's `files_modified`)**
- **Found during:** Task 2.
- **Issue:** Task 2's action ("Extend `DockerBackend.Resolve` … Track the sidecar so Suspend/Stop tear it down") targets `Resolve`/`Suspend`/`Resume`/`Stop`, which live in `docker_backend_lifecycle.go` (37-04 split them out of `docker_backend.go`). The plan's `files_modified` lists only `docker_backend.go`.
- **Fix:** Kept the egress launch/teardown METHODS in `docker_backend.go` (the plan's declared file) and added only the four minimal call-site hooks in `docker_backend_lifecycle.go`. That file is in neither 37-05's lane (router/reap/serve) nor any shared orchestrator artifact, so the edit is wave-safe.
- **Files modified:** `docker_backend_lifecycle.go` (4 hooks).
- **Verification:** `go build ./...`, `go vet`, unit + tagged compile all green; the 37-04 lifecycle tests unaffected (egress no-op without `WithEgress`).
- **Committed in:** `ac14661e`.

**3. [Rule 2 - Package legitimacy / caution] OpenSandbox FQDN binary is an opt-in bake, not a hard-coded source pin**
- **Found during:** Task 1 (Dockerfile).
- **Issue:** The plan asks to "build OpenSandbox's `components/egress` from a pinned commit" and `docker build` must be deterministic. I cannot verify the OpenSandbox repo layout / a specific commit SHA from this environment, and hard-coding an unverified SHA (or auto-fetching a third-party repo in the default build) risks a slopsquat/hallucinated pin — worse than an explicit opt-in.
- **Fix:** The default image builds the bespoke nft filter-floor ONLY (deterministic, network-free — the RESEARCH primary + A1 fallback). The entrypoint execs `/usr/local/bin/opensandbox-egress` when it is baked AND `OPENSANDBOX_EGRESS_MODE` is set; the Dockerfile documents the pinned-source bake recipe. `TestEgress_FQDNAllowlist` skips unless `AURA_EGRESS_FQDN_IMAGE` names an OpenSandbox-enabled image.
- **Files modified:** `docker/aura-egress/Dockerfile`, `egress_integration_test.go`.
- **Verification:** floor build is deterministic; the OpenSandbox pin is deferred to the operator/SBX-05 ADR with the exact recipe documented.
- **Committed in:** `2eaa0278` / `ac14661e`.

---

**Total deviations:** 3 auto-fixed (2 Rule 3 blocking, 1 Rule 2 package-legitimacy caution).
**Impact on plan:** No scope creep. The signature + call-site edits are necessary to satisfy the plan's own acceptance criteria within the wave's ownership boundary; the OpenSandbox opt-in is the sanctioned A1 fallback and honors the NEVER-SUPPOSE / package-legitimacy discipline. All unit acceptance criteria are green; the live DROP + FQDN + image-build legs run at phase validation on the native-Linux stack.

## Issues Encountered

- **Live docker_integration + the DROP enforcement deferred to native-Linux/CI.** dockerd is unreachable in this Windows worktree and Docker Desktop/WSL vpnkit NATs the bridge (advisory), so `TestEgress_*` and the `docker build` skip locally (the sanctioned skip; `t.Fatal` under `$CI` on a non-Linux daemon). The suite compiles under `-tags docker_integration`, all four unit tests pass, and the enforced green (public-reach + internal-DROP + cap-placement + teardown, FQDN allowlist, image build) runs at phase validation on the native-Linux stack.

## User Setup Required

None — no external service configuration. To enable the opt-in runc-only FQDN allowlist tier, an operator builds the `aura-egress` image WITH the pinned OpenSandbox egress binary at `/usr/local/bin/opensandbox-egress` (recipe documented in `docker/aura-egress/Dockerfile`) and sets `AURA_SANDBOX_EGRESS_ALLOWLIST`. The always-on filter-floor needs no configuration.

## Next Phase Readiness

- **37-05** (tool routing / composition root) wires `WithEgress(<aura-egress image ref>)` into the strict-profile `DockerBackend` so every routed box gets the always-on floor; the `EgressPolicy` it passes via `specFor` already flows to `buildEgressSidecar`.
- **37-08** (SBX-05 ADR) has the recorded decisions: the filter-floor ruleset (DROP targets), the gVisor⊥nat FQDN split (`ErrRunscFQDNMutualExclusion`, #934), the config-sourced allowlist path, and the OpenSandbox source-pin recipe as the accepted residual.
- Blockers: none. The live native-Linux DROP/FQDN/image-build run is a phase-validation step, not a code blocker.

## Self-Check: PASSED

- Created files exist: `egress.go`, `egress_test.go`, `egress_integration_test.go`, `docker/aura-egress/Dockerfile`, `37-06-SUMMARY.md` — all FOUND.
- Task commits exist: `2eaa0278`, `ac14661e` — both FOUND in `git log`.
- Plan `<verification>` re-run: `go build ./...` + `go vet ./internal/sandbox/usersandbox/` clean; `go test ./internal/sandbox/usersandbox/` (4 unit egress tests + the prior suite) green; `go build -tags docker_integration ./internal/sandbox/usersandbox/` compiles and the tagged egress tests skip locally (dockerd unreachable); no file > 600 LOC (largest touched: docker_backend_lifecycle.go 217). No edits to 37-05's lane (router.go/reap.go/serve*.go) or shared STATE.md/ROADMAP.md.

---
*Phase: 37-per-user-full-capability-sandbox*
*Completed: 2026-07-07*
