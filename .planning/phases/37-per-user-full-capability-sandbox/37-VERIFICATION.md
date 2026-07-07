---
phase: 37-per-user-full-capability-sandbox
verified: 2026-07-07T00:00:00Z
status: gaps_found
score: 4/5 must-haves verified (1 BLOCKER)
overrides_applied: 0
gaps:
  - truth: "A configured egress allowlist cannot reach a disallowed host; the default egress posture is full public internet minus the tenancy boundary (DROP RFC1918 + 169.254.169.254 cloud-metadata + the shared-services Docker bridge), not --network none (SBX-04 amended per D-06, ROADMAP Success Criterion #4)."
    status: failed
    reason: "The egress-sidecar mechanism (internal/sandbox/usersandbox/egress.go, buildEgressSidecar, the filter-table floor, the FQDN allowlist) is fully built and unit-tested, and DockerBackend.Resolve/Suspend/Resume/Stop correctly call launchEgress/suspendEgress/resumeEgress/teardownEgress — but launchEgress is gated on `b.egressImage != \"\"`, and the ONLY production constructor of DockerBackend (cmd/aura/serve_dispatch.go:buildSandboxRouter) never calls WithEgress(image). config.SandboxConfig has no EgressImage field, and a repo-wide grep confirms zero Go code reads AURA_SANDBOX_EGRESS_IMAGE (only compose.yaml sets it as a container env var that is never consumed). Consequently every per-identity box created by the shipped binary — under single_user_hardened or server_production, for every identity — gets NO egress sidecar: no filter-table floor, no RFC1918/metadata/bridge DROP, no enforced allowlist. The box runs on plain default Docker networking. This is not a 'deferred live-test' gap (the sanctioned WSL/CI category per this phase's precedent) — it is a structural composition-root gap: the always-on floor cannot become 'on' via any currently-exposed configuration path."
    artifacts:
      - path: "cmd/aura/serve_dispatch.go"
        issue: "buildSandboxRouter (lines 147-159) constructs usersandbox.NewDockerBackend with only WithMaterializeSources; WithEgress is never called, so DockerBackend.egressImage is always empty in the shipped binary and launchEgress/suspendEgress/resumeEgress/teardownEgress are permanent no-ops."
      - path: "internal/config/config_sandbox.go"
        issue: "SandboxConfig has no field sourcing an egress sidecar image ref (no EgressImage / AURA_SANDBOX_EGRESS_IMAGE parsing), so there is no config-level path to reach WithEgress even if buildSandboxRouter were fixed to read one."
      - path: "compose.yaml"
        issue: "Sets AURA_SANDBOX_EGRESS_IMAGE as a container env var and documents it as 'the digest-pin point for the per-box egress sidecar,' implying it is live; it is read by zero Go code (repo-wide grep confirms), so the compose comment overstates the current wiring."
      - path: "docs/adr/0037-per-identity-docker-sandbox.md"
        issue: "Records 'the always-on egress floor (SBX-04)' as a delivered compensating control (Consequences section, Residual B) without noting it is currently unwired/inert in the composition root — the ADR overstates the shipped state."
    missing:
      - "Add an AURA_SANDBOX_EGRESS_IMAGE (or similarly named) field to config.SandboxConfig + a KnobSpec catalog row, following the existing PROF env-catalog pattern from 37-01."
      - "Call usersandbox.WithEgress(cfg.Sandbox.EgressImage) in cmd/aura/serve_dispatch.go:buildSandboxRouter (guarded: no-op when empty, exactly as 37-08 anticipated 37-05 would do)."
      - "Re-run TestEgress_FloorDropsInternal / TestEgress_FQDNAllowlist against native-Linux dockerd with a real box created via buildSandboxRouter (not just via WithEgress in a test file) to prove the floor is reachable end-to-end from the composition root, not only from a hand-built DockerBackend in a test."
      - "Correct the compose.yaml comment and the ADR to either state the pending wiring accurately or be updated once the fix lands."
deferred: []
human_verification:
  - test: "Run `go test -tags docker_integration -race ./internal/sandbox/usersandbox/...` and `./internal/agent/tools/...` on native-Linux dockerd (WSL/CI, per CLAUDE.md sanctioned deferral)."
    expected: "TestDockerBackend_RoundTrip, TestLifecycle_SuspendResumeDelete, TestVolume_CrossIdentityDeny, TestResolve_MaterializesInputs, TestReap_IdleSuspendAutoResume, TestRoute_StrictExecInBox, TestSnippetExec_RoutedEndToEnd, TestShellBg_RunsInBox all PASS with goleak clean and -race clean."
    why_human: "dockerd is unreachable on this Windows verification host (no CGO/gcc, no daemon); these tiers compile and skip cleanly locally per the sanctioned Windows-dev-host limitation (identical posture to Phase 36) — they must be executed live on the native-Linux stack before Gate-3 close."
  - test: "Run `TestEgress_FloorDropsInternal` / `TestEgress_FQDNAllowlist` on native-Linux dockerd AFTER the egress-wiring gap above is fixed, using a box actually created via buildSandboxRouter (not a hand-constructed DockerBackend in the test file)."
    expected: "A routed box under server_production reaches the public internet but is DROPPED from an RFC1918 target / 169.254.169.254 / the shared bridge; a disallowed FQDN is DROPPED when a tightened allowlist is configured."
    why_human: "This is the closing proof for the BLOCKER gap above; it requires both the code fix and a native-Linux dockerd (Docker-Desktop/WSL vpnkit NATs the bridge and cannot validate DROP enforcement, per Pitfall 3)."
  - test: "Run the D-14 concurrency-soak (`AURA_SANDBOX_SOAK_REALHOST=1 go test -tags docker_integration ./internal/sandbox/usersandbox/ -run TestSoak`) on the real 32GB appliance host."
    expected: "10-20 concurrent per-identity boxes fit within the 32GB envelope with headroom; Resolve p95 <~2s; Resume p95 <~1s; the cgroup-cap starvation probe shows no co-tenant starvation. Fill the 37-VALIDATION.md Manual-Only results table."
    why_human: "Dev WSL is capped at 15.47 GiB (documented, deliberately gated behind AURA_SANDBOX_SOAK_REALHOST so it never false-passes/fails on the dev cap) — the pass/fail verdict is only meaningful on the real 32GB host."
  - test: "Bring up a box with `runtime: runsc` under server_production (compose.gvisor.yaml) and confirm exec works and the filter-table floor still drops internal traffic."
    expected: "gVisor runsc smoke passes; the filter-table floor (once the egress-wiring gap above is fixed) still enforces the tenancy boundary under runsc; the FQDN allowlist is correctly refused (ErrRunscFQDNMutualExclusion) if configured together with runsc."
    why_human: "Requires the runsc runtime installed and a native-Linux host; not exercisable on this Windows verification host."
---

# Phase 37: Per-User Full-Capability Sandbox Verification Report

**Phase Goal:** Resolve F-001 — host shell/fs run inside a per-identity full-capability Docker sandbox under hardened/production; the host is never exposed.
**Verified:** 2026-07-07
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Under `server_production`, shell/fs target the per-identity sandbox and the real host filesystem is unreachable | ✓ VERIFIED (mechanism); live proof deferred | `SandboxRouter.Route` short-circuits `(zero,false,nil)` under `!Strict()` (router.go:79-93, `TestRoute_DevNoOp` pass) and routes `shell_exec`/`fs_read`/`fs_write`/`send_file`/`skill action=use` through `Router.Exec`/`WriteFile`/`CopyArtifactOut` under a strict profile (grep confirms `Router:`/`SandboxRouter:` wired on all 5 tools in cmd/aura/main.go, web_* deliberately excluded). Live in-box execution proof (`TestRoute_StrictExecInBox`, `TestSnippetExec_RoutedEndToEnd`) is `docker_integration`-tagged, compiles, and skips cleanly on this Windows host (no dockerd) — sanctioned WSL/CI deferral per CLAUDE.md/Phase-36 precedent. |
| 2 | Docker-socket/`--privileged`/`--network host`/bind-mounts are unrepresentable (test-asserted) | ✓ VERIFIED | `SandboxSpec` (spec.go) has no Privileged/NetworkMode/Binds/Devices/CapAdd/socket field; `toHostConfig` (translate.go) pins `Privileged:false, NetworkMode:"", Binds:nil, AutoRemove:false` as the sole crossing. `TestSpec_NoHostExposureFields` and `TestTranslate_PinsSafe` (table + 1000 `pgregory.net/rapid` adversarial specs) both PASS locally (re-ran, see below). |
| 3 | Cross-identity leakage is impossible and the idle-TTL lifecycle works | ✓ VERIFIED (mechanism); live proof deferred | Per-identity named volumes (`aura-box-<id>`) enforce storage isolation by construction (spike-078 pattern, no app-prefix scoping); `SuspendIdle` (reap.go) is registered as `cron.KindSandboxReap` (confirmed in `internal/cron/store.go` + `cmd/aura/serve_dispatch.go`) with migration 0034 widening `scheduler_tasks.kind`; `seedSandboxReapSweep` wired in `serve_provisioning.go`. `TestRoute_IdleBump`/`TestSpecFor_UsesConfiguredKnobs` PASS locally. Live cross-identity-deny + suspend/resume/delete + auto-resume proofs (`TestVolume_CrossIdentityDeny`, `TestLifecycle_SuspendResumeDelete`, `TestReap_IdleSuspendAutoResume`) are `docker_integration`-tagged and skip cleanly here (sanctioned deferral). |
| 4 | A configured egress allowlist cannot reach a disallowed host; default egress posture is full public internet minus the tenancy boundary, not `--network none` | ✗ **FAILED (BLOCKER)** | The egress mechanism (egress.go: `buildEgressSidecar`, the filter-table floor DROPping RFC1918/metadata/bridge, the runc-only OpenSandbox FQDN allowlist, `ErrRunscFQDNMutualExclusion`) is fully implemented and unit-proven (`TestEgress_FloorRuleset`/`RunscRejectsFQDN`/`CapOnSidecarOnly`/`AllowlistFromPolicy` all PASS). `DockerBackend.Resolve/Suspend/Resume/Stop` correctly call `launchEgress`/`suspendEgress`/`resumeEgress`/`teardownEgress`. **But** `launchEgress` is a no-op whenever `b.egressImage == ""`, and the ONLY production construction site — `buildSandboxRouter` in `cmd/aura/serve_dispatch.go` — builds `DockerBackend` with `WithMaterializeSources` only; `WithEgress` is never called anywhere in `cmd/aura`. `config.SandboxConfig` has no field to source an egress image from, and a repo-wide grep for `AURA_SANDBOX_EGRESS_IMAGE` across `*.go` returns zero matches — the env var `compose.yaml` sets is read by nothing. **Every box the shipped binary creates, in every profile, gets no egress sidecar at all.** This is a confirmed composition-root gap, not merely an unexecuted live test. |
| 5 | An ADR records container-per-identity (K8s/gVisor-default → DGX) + a pre-merge concurrency benchmark on 32GB | ✓ VERIFIED (mechanism); live benchmark run deferred; ADR has an accuracy gap tied to #4 | `docs/adr/0037-per-identity-docker-sandbox.md` substantively records D-15 (container-per-identity over Docker, K8s/agent-sandbox/gVisor-default reserved for DGX-Spark), the E2B/agent-sandbox forward-compat shape, and three Residuals (daemon Docker-socket surface + docker-socket-proxy narrowing, gVisor⊥nat #934, D-06 egress-default amendment). `internal/sandbox/usersandbox/bench_soak_test.go` (`TestSoak_ConcurrentIdentities`) compiles, vets clean, and correctly SKIPs without `AURA_SANDBOX_SOAK_REALHOST=1` (verified: skips locally). However, the ADR's Consequences/Residual-B text describes "the always-on egress floor (SBX-04)" as a delivered compensating control without disclosing the #4 wiring gap — the ADR overstates the current shipped state on this one point. |

**Score:** 4/5 truths mechanism-verified; 1 truth (SC#4, SBX-04) FAILED at the composition root — a genuine BLOCKER, not a deferred-live-test item.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/sandbox/usersandbox/spec.go` | SandboxSpec/RuntimeClass/EgressPolicy/Resources, no escape fields | ✓ VERIFIED | Confirmed via read + `TestSpec_NoHostExposureFields` pass. |
| `internal/sandbox/usersandbox/translate.go` | private `toHostConfig`, pins dangerous fields | ✓ VERIFIED | Confirmed via read; pins present, `TestTranslate_PinsSafe` (1000 rapid cases) pass. |
| `internal/sandbox/usersandbox/backend.go` | `Backend` E2B interface (5 verbs, unchanged) | ✓ VERIFIED | `TestBackend_StreamVerbNotOnInterface` (37-09) confirms the streaming verb stays off the interface (D-02 preserved). |
| `internal/sandbox/usersandbox/docker_backend*.go` | DockerBackend implementing Backend, lifecycle + exec + materialize | ✓ VERIFIED (unit); docker_integration deferred | Compiles, builds, unit tests pass; tagged suite skips cleanly (no dockerd here). |
| `internal/sandbox/usersandbox/router.go` | SandboxRouter.Route (Strict no-op + fail-CLOSED + config-sourced specFor) | ✓ VERIFIED | All 5 unit tests (`TestRoute_DevNoOp/FailClosed/LocalFallback/IdleBump`, `TestSpecFor_UsesConfiguredKnobs`) pass. |
| `internal/sandbox/usersandbox/egress.go` | egress sidecar spec + filter-table floor + FQDN mode | ⚠️ VERIFIED-BUT-ORPHANED | The artifact itself is substantive and unit-tested, but it is never invoked from the only production call site (`buildSandboxRouter` never sets `WithEgress`) — see Truth #4 above. Classified as the SC#4 BLOCKER, not merely an ORPHANED artifact note, because it is a ROADMAP success criterion. |
| `docker/aura-egress/Dockerfile` | CAP_NET_ADMIN sidecar image (nft floor) | ✓ VERIFIED (file present); `docker build` deferred (no Docker daemon here) | File exists per 37-06-SUMMARY; live build is a WSL/CI item, consistent with 37-01's aura-sandbox image build. |
| `internal/cron/handlers/sandbox_reap.go` | KindSandboxReap + SandboxReapHandler + seam | ✓ VERIFIED | 4 unit tests pass; no `usersandbox` import (grep confirmed empty); no goroutine/ticker. |
| `internal/db/migrations/0034_*.sql` | widen scheduler_tasks.kind for sandbox_reap | ✓ VERIFIED (structurally); db_integration reversibility deferred | up/down read correctly (delete-before-narrow pattern mirrors 0033); live db_integration run needs the Postgres stack (sanctioned deferral). |
| `internal/config/config_sandbox.go` | AURA_SANDBOX_* operator surface | ✓ VERIFIED | 6 knobs present + KnobSpec rows; no EgressImage field (part of the SC#4 gap). |
| `docs/adr/0037-per-identity-docker-sandbox.md` | SBX-05 ADR | ✓ VERIFIED (substantive) with a factual-accuracy caveat | See Truth #5 above. |
| `internal/sandbox/usersandbox/bench_soak_test.go` | D-14 concurrency-soak harness | ✓ VERIFIED (compiles, vets, skips correctly) | Live real-32GB-host run is Manual-Only (Gate-3), correctly gated. |
| `cmd/aura/serve_dispatch.go` (`buildSandboxRouter`) | composition root wiring | ⚠️ INCOMPLETE | Wires backend/router/materialize/reaper correctly; does NOT wire egress (the BLOCKER). |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `router.go Route` | `config.RuntimeProfile.Strict()` | `!r.profile.Strict()` short-circuit | ✓ WIRED | Confirmed by read + `TestRoute_DevNoOp`. |
| `router.go specFor` | `cfg.Sandbox` (config) | field-by-field mapping | ✓ WIRED | `TestSpecFor_UsesConfiguredKnobs` proves configured Image/CPU/Memory/Pids/EgressAllowlist reach the spec. |
| `cmd/aura/serve_dispatch.go` | `internal/cron/handlers.KindSandboxReap` | dispatch map registration | ✓ WIRED | `TestBuildDispatchRegistersSandboxReap` (cmd/aura) pass. |
| `internal/agent/tools/*.go` | `internal/sandbox/usersandbox.SandboxRouter` | `Route(ctx)` at top of each Execute | ✓ WIRED | Confirmed via grep + unit tests (`TestShellExec_FailClosedNoHostFallback`, etc.), all pass. |
| `internal/sandbox/usersandbox/docker_backend.go` | `internal/sandbox/usersandbox/egress.go` | `Resolve` → `launchEgress` → `buildEgressSidecar` | ⚠️ WIRED-BUT-DEAD | The call chain itself is correct code, but the entry condition (`egressImage != ""`) is never satisfied by the only production caller (`buildSandboxRouter`) — see Truth #4. This is the SC#4 BLOCKER. |
| `EgressPolicy.FQDNAllowlist` (config-sourced) | `buildEgressSidecar` | `policy.FQDNAllowlist` param | ✓ WIRED (in isolation); unreachable in production | `TestEgress_AllowlistFromPolicy` proves the mapping in a unit test that constructs `DockerBackend` directly with `WithEgress`; the production path never reaches this because `WithEgress` is never called. |

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
|---|---|---|---|---|
| SBX-01 | 37-01, 37-02, 37-04, 37-05, 37-07, 37-09 | Host shell/fs tools execute inside per-identity box under strict profiles | ✓ SATISFIED (mechanism); live docker_integration proof pending (human_verification) | All 5 tools routed, fail-CLOSED, dev-unchanged, unit tests pass; live in-box execution deferred to WSL/CI. |
| SBX-02 | 37-02 | Docker-socket/privileged/host-net/bind-mounts unrepresentable | ✓ SATISFIED | Structural type layer + adversarial property tests pass; REQUIREMENTS.md already checks this `[x]`. |
| SBX-03 | 37-01, 37-03, 37-04, 37-05, 37-06 | Per-identity lifecycle + cross-identity isolation | ✓ SATISFIED (mechanism); live docker_integration proof pending (human_verification) | Named-volume isolation, suspend/resume/delete, idle-TTL reaper on the scheduler (no goroutine) all unit-proven; live cross-deny + lifecycle deferred to WSL/CI. |
| SBX-04 | 37-01, 37-05, 37-06 | Enforced egress default + configured allowlist | ✗ **BLOCKED** | The egress mechanism is built and unit-tested but never wired to run at the composition root — see the BLOCKER above. |
| SBX-05 | 37-08 | ADR + D-14 concurrency benchmark | ✓ SATISFIED (mechanism); live 32GB-host run pending (human_verification); ADR has an accuracy caveat re: SC#4 | ADR substantive; benchmark harness correctly gated; live run is Manual-Only Gate-3 evidence. |

No orphaned requirements: REQUIREMENTS.md's SBX-01..05 entries are each claimed by at least one plan's frontmatter `requirements` list; all five IDs are accounted for above. REQUIREMENTS.md currently shows SBX-01/03/04/05 unchecked and SBX-02 checked `[x]` — this matches the evidence above (SBX-02 alone fully closes; the others have either a live-test deferral or, for SBX-04, a genuine unresolved gap) and should NOT be changed to all-checked until the SBX-04 gap is closed.

### Anti-Patterns Found

No `TBD`/`FIXME`/`XXX`/`TODO`/`HACK`/`PLACEHOLDER` markers found in any of the 21 phase-touched source files scanned (router.go, reap.go, egress.go, docker_backend*.go, materialize.go, spec.go, translate.go, backend.go, router_tools.go, shell_exec.go, shell_bg.go, fs_read.go, fs_write.go, send_file.go, skill.go, skill_read.go, config_sandbox.go, serve_dispatch.go, serve_provisioning.go, sandbox_reap.go). No empty-implementation or hardcoded-empty-data stubs found in the reviewed files beyond the documented, intentional forward seams (e.g., `sandboxMaterializeSources` sourcing only the skills export dir, noted as a documented forward seam in 37-07's SUMMARY, not a stub).

One documentation-accuracy issue (not a code anti-pattern): `docs/adr/0037-per-identity-docker-sandbox.md` and the `compose.yaml` `AURA_SANDBOX_EGRESS_IMAGE` comment both describe the egress floor / image-pin point as live/delivered without disclosing that it is currently unreachable from the composition root (see Truth #4 / the BLOCKER gap).

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|---|---|---|---|
| Whole-module build | `go build ./...` | clean, no output | ✓ PASS |
| Whole-module vet | `go vet ./...` | clean, no output | ✓ PASS |
| Tagged build | `go build -tags docker_integration ./...` | clean, no output | ✓ PASS |
| Whole-module unit tests | `go test ./...` | all 67 packages ok/no-test-files, 0 FAIL | ✓ PASS |
| SBX-02/SBX-01 unit tests | `go test ./internal/sandbox/usersandbox/ -run "TestSpec_NoHostExposureFields\|TestTranslate_PinsSafe\|TestSpec_RunscOnlyServerProduction\|TestRoute_DevNoOp\|TestRoute_FailClosed\|TestRoute_LocalFallback\|TestRoute_IdleBump\|TestSpecFor_UsesConfiguredKnobs\|TestEgress_*"` | all PASS (rapid: 1000/1000) | ✓ PASS |
| Tagged suite (skip posture) | `go test -tags docker_integration ./internal/sandbox/usersandbox/... ./internal/agent/tools/... ./internal/cron/... ./internal/db/... ./cmd/aura/...` | all packages `ok`; docker_integration-tagged tests SKIP (not FAIL) locally | ✓ PASS (sanctioned skip, matches Phase-36 precedent) |
| Egress composition-root reachability | `grep -rn "AURA_SANDBOX_EGRESS_IMAGE" --include="*.go" .` | 0 matches | ✗ FAIL — confirms the BLOCKER (the env var compose.yaml sets is read by no Go code) |
| Egress composition-root reachability | `grep -n "WithEgress" cmd/aura/*.go` | 0 matches | ✗ FAIL — confirms the BLOCKER (buildSandboxRouter never calls WithEgress) |
| File-size discipline | `wc -l` on all touched usersandbox/tools/cmd files | max 593 LOC (serve.go) | ✓ PASS (≤600 cap honored everywhere) |

### Probe Execution

No `scripts/*/tests/probe-*.sh` files found for this phase; no probes declared in any 37-0X-PLAN.md/SUMMARY.md. Step 7c: SKIPPED (no probes declared).

### Human Verification Required

See the `human_verification` YAML block in the frontmatter above for the four sanctioned-deferral items (docker_integration live suite on WSL/CI, egress-DROP re-verification post-fix, D-14 32GB soak, gVisor runsc smoke). These are consistent with the Phase-36 precedent for Windows-dev-host executed phases and do NOT by themselves block the phase — they are the documented WSL/CI Gate-3 fill.

### Gaps Summary

Four of five ROADMAP success criteria are mechanism-complete with only the sanctioned live-docker/32GB-host tiers deferred to WSL/CI (an accepted, precedented limitation per CLAUDE.md and the Phase-36 pattern this repo already follows). The fifth — **Success Criterion #4 (SBX-04, egress enforcement)** — has a genuine, code-confirmed gap: the entire egress-sidecar mechanism (filter-table floor + FQDN allowlist + gVisor mutual-exclusion) is well-built and unit-tested in isolation, but it is **never invoked from the only production composition path** (`cmd/aura/serve_dispatch.go:buildSandboxRouter`). No config field, env var, or code path currently lets `WithEgress` fire in the shipped binary. This means that today, under `server_production`, every per-identity box gets **no network containment at all** — not the intended "full internet minus the tenancy boundary" floor, and not even the superseded `--network none`. This was self-flagged by the 37-07 and 37-08 executors ("egress-image wiring is deliberately NOT added here — a separate follow-up, out of lane" / "AURA_SANDBOX_EGRESS_IMAGE is a forward reference... it is NOT read by the current binary") but was not surfaced as blocking SC#4, and no later phase in ROADMAP.md addresses it — it is not a deferred item, it is an open gap within this phase's own success criteria. The fix is small in scope (add an `EgressImage` config knob + one `WithEgress(...)` call in `buildSandboxRouter`), but it must land before Phase 37's goal ("the host is never exposed" via a fully-enforced containment boundary) can be considered achieved.

**This looks intentional** (the executors documented it as a deliberate scope decision). To accept this deviation instead of treating it as a blocking gap, add to this file's frontmatter:

```yaml
overrides:
  - must_have: "A configured egress allowlist cannot reach a disallowed host; the default egress posture is full public internet minus the tenancy boundary"
    reason: "<explain why shipping Phase 37 without a live egress floor is acceptable, and when the follow-up wiring lands>"
    accepted_by: "<name>"
    accepted_at: "<ISO timestamp>"
```

Absent such an override, this is a BLOCKER and Phase 37 should not be considered closed.

---

*Verified: 2026-07-07*
*Verifier: Claude (gsd-verifier)*
