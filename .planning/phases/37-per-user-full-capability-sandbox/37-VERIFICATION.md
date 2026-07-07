---
phase: 37-per-user-full-capability-sandbox
verified: 2026-07-07T10:30:00Z
status: human_needed
score: 5/5 must-haves mechanism-verified (0 FAILED); 4 WSL/CI live-tier items pending human_verification
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: "4/5 must-haves verified (1 BLOCKER)"
  gaps_closed:
    - "SC#4/SBX-04 composition-root wiring gap: buildSandboxRouter now calls usersandbox.WithEgress(cfg.Sandbox.EgressImage) via the new newSandboxBackend helper; config.SandboxConfig.EgressImage is sourced from AURA_SANDBOX_EGRESS_IMAGE with a NON-EMPTY default (aura-egress:latest); the KnobSpec registry catalogs it; repo-wide grep for AURA_SANDBOX_EGRESS_IMAGE across *.go inverted 0 -> 10 matches (independently re-run); the fail-closed chain (ensureImage pull-fail -> launchEgress error -> Resolve error -> Route(_,true,err) -> tool denies) was independently traced end-to-end by reading router.go/docker_backend.go/docker_backend_lifecycle.go directly, not by trusting the SUMMARY."
  gaps_remaining: []
  regressions: []
human_verification:
  - test: "Run `go test -tags docker_integration -race ./internal/sandbox/usersandbox/... ./internal/agent/tools/... ./cmd/aura/... ./internal/config/...` on native-Linux dockerd (WSL/CI, per CLAUDE.md sanctioned Windows-dev-host deferral)."
    expected: "TestDockerBackend_RoundTrip, TestLifecycle_SuspendResumeDelete, TestVolume_CrossIdentityDeny, TestResolve_MaterializesInputs, TestReap_IdleSuspendAutoResume, TestRoute_StrictExecInBox, TestSnippetExec_RoutedEndToEnd, TestShellBg_RunsInBox, and TestBuildSandboxRouterWiresEgress all PASS with goleak clean and -race clean (this Windows host has CGO_ENABLED=0 — `-race` cannot even compile here, confirmed: `go test -race` errors with 'requires cgo')."
    why_human: "dockerd is unreachable and CGO_ENABLED=0 on this Windows verification host (confirmed by direct command execution); these tiers compile and skip/error cleanly locally per the sanctioned Windows-dev-host limitation (identical posture to Phase 36) — they must be executed live on the native-Linux stack before Gate-3 close."
  - test: "Run `go test -tags docker_integration -race ./cmd/aura/ -run TestBuildSandboxRouter_LaunchesEgressFloor` (with `AURA_EGRESS_ENFORCE=1` or under `$CI`, after `docker build docker/aura-egress`) AND `go test -tags docker_integration ./internal/sandbox/usersandbox/ -run 'TestEgress_FloorDropsInternal|TestEgress_FQDNAllowlist'` on a native-Linux non-masquerading dockerd."
    expected: "A box created via the PRODUCTION buildSandboxRouter -> Route path carries its aura-egress-<id> sidecar (NET_ADMIN on the sidecar only, shared box netns via container:<box>), reaches the public internet (example.com) but is DROPPED from 169.254.169.254 and an RFC1918 target; Stop leaves no orphan sidecar. The backend-level floor + a tightened FQDN allowlist enforce identically. This is the closing live proof that the now-wired mechanism (SC#4/SBX-04) actually enforces the tenancy boundary end-to-end."
    why_human: "This is the closing proof for the now-closed composition-root gap; it requires a native-Linux non-masquerading dockerd — Docker-Desktop/WSL vpnkit NATs the bridge and cannot validate DROP enforcement (37-RESEARCH Pitfall 3). Confirmed here: `go test -tags docker_integration ./cmd/aura/ -run TestBuildSandboxRouter_LaunchesEgressFloor` and the backend-level `TestEgress_FloorDropsInternal`/`TestEgress_FQDNAllowlist` all SKIP cleanly on this host (no dockerd) — never FAIL, so no regression, but no live proof either."
  - test: "Run the D-14 concurrency-soak (`AURA_SANDBOX_SOAK_REALHOST=1 go test -tags docker_integration ./internal/sandbox/usersandbox/ -run TestSoak`) on the real 32GB appliance host."
    expected: "10-20 concurrent per-identity boxes fit within the 32GB envelope with headroom; Resolve p95 <~2s; Resume p95 <~1s; the cgroup-cap starvation probe shows no co-tenant starvation. Fill the 37-VALIDATION.md Manual-Only results table (Gate-3 evidence)."
    why_human: "Dev WSL is capped at 15.47 GiB (documented, deliberately gated behind AURA_SANDBOX_SOAK_REALHOST so it never false-passes/fails on the dev cap) — the pass/fail verdict is only meaningful on the real 32GB host. Confirmed here: without the tag the test 'no tests to run'; with `-tags docker_integration` it correctly SKIPs (not FAIL) with the real-host-only message."
  - test: "Bring up a box with `runtime: runsc` under server_production (compose.gvisor.yaml) and confirm exec works and the filter-table floor still drops internal traffic."
    expected: "gVisor runsc smoke passes; the filter-table floor (now reachable from the composition root) still enforces the tenancy boundary under runsc; a configured FQDN allowlist together with runsc is correctly refused (ErrRunscFQDNMutualExclusion)."
    why_human: "Requires the runsc runtime installed and a native-Linux host; not exercisable on this Windows verification host."
---

# Phase 37: Per-User Full-Capability Sandbox Verification Report

**Phase Goal:** Resolve F-001 — host shell/fs run inside a per-identity full-capability Docker sandbox under hardened/production; the host is never exposed.
**Verified:** 2026-07-07
**Status:** human_needed
**Re-verification:** Yes — after gap-closure plan 37-10 (SBX-04 composition-root wiring fix, commit `bdebc5c9`)

## Re-Verification Summary

The previous verification (`37-VERIFICATION.md`, `gaps_found`, 4/5) found ONE BLOCKER: the entire
egress-sidecar mechanism (filter-table floor DROPping RFC1918 + `169.254.169.254` metadata + the
shared-services bridge) was built and unit-tested, but **inert in the shipped binary** — the only
production constructor (`cmd/aura/serve_dispatch.go:buildSandboxRouter`) never called `WithEgress`,
and no config field or env var reached it. This was explicitly classified as a **structural
composition-root gap**, not a deferred-live-test item.

Gap-closure plan 37-10 (commit `bdebc5c9`) landed a surgical fix. I independently re-verified the
fix against the actual codebase — reading every touched file, grepping for the exact wiring
expressions, and running the build/vet/test commands myself (not trusting SUMMARY.md's claims):

**The composition-root gap is genuinely CLOSED.** Evidence (all independently reproduced, not
copied from the SUMMARY):

- `config.SandboxConfig.EgressImage string` field exists (`internal/config/config_sandbox.go:81`),
  sourced by `envDefault("AURA_SANDBOX_EGRESS_IMAGE", defaultSandboxEgressImage)` (line 96), with
  `defaultSandboxEgressImage = "aura-egress:latest"` (line 35) — a **non-empty** default, confirmed
  by reading the source and by running `TestLoad_SandboxConfig` myself (PASS, asserts the non-empty
  default).
- `AURA_SANDBOX_EGRESS_IMAGE` KnobSpec row cataloged (`internal/config/config_knobs.go:136`,
  `KindString`, `Default: "aura-egress:latest"`) — confirmed by reading the source and re-running
  `TestKnobRegistry` (PASS).
- `buildSandboxRouter` → `newSandboxBackend(cli, cfg)` → `usersandbox.NewDockerBackend(cli,
  cfg.Sandbox.Image, limitsFrom(cfg.Sandbox), usersandbox.WithMaterializeSources(...),
  usersandbox.WithEgress(cfg.Sandbox.EgressImage))` (`cmd/aura/serve_dispatch.go:170-174`) — I ran
  `grep -c 'WithEgress(cfg.Sandbox.EgressImage)' cmd/aura/serve_dispatch.go` myself: **1** match, exactly
  as the plan's acceptance gate required.
- Repo-wide `grep -rn "AURA_SANDBOX_EGRESS_IMAGE" --include="*.go" .`: I re-ran this myself and got
  **10 matches** across 5 files (was 0 at the previous verification) — the exact BLOCKER symptom is
  inverted.
- `DockerBackend.EgressImage()` read-only accessor exists (`internal/sandbox/usersandbox/docker_backend.go:95`).
- **Fail-closed chain independently traced by reading the source, not the SUMMARY:**
  `ensureImage` pull-failure (`docker_backend_lifecycle.go:213-214`) → `launchEgress` returns a
  wrapped error (`docker_backend.go:144-145`) → `Resolve` returns a wrapped error
  (`docker_backend_lifecycle.go:69-71`) → `Route` returns `(BoxHandle{}, true, err)`
  (`router.go:84-86`, read directly — confirms `routed=true` on error, the fail-CLOSED contract) →
  `TestShellExec_FailClosedNoHostFallback` re-run by me: **PASS**.
- Docker-free regression guard `TestBuildSandboxRouterWiresEgress`
  (`cmd/aura/serve_dispatch_egress_test.go`) — I ran it myself: **PASS** (asserts a distinct
  digest-pinned ref reaches `WithEgress`, the default-loaded config is non-empty, and a non-strict
  profile stays nil).
- Composition-root live-DROP re-test `TestBuildSandboxRouter_LaunchesEgressFloor`
  (`cmd/aura/serve_dispatch_egress_integration_test.go`, `//go:build docker_integration`) — I ran it
  myself with the tag: **SKIP** (not FAIL — no dockerd on this host), with a `$CI`-gated `t.Fatal`
  path on a non-Linux daemon confirmed by reading the gate functions (no-skip-as-green honored).
- `go vet ./...`, `go build ./...`, `go build -tags docker_integration ./...`, and
  `go vet -tags docker_integration ./cmd/aura/ ./internal/sandbox/usersandbox/` — all run by me,
  **all clean**.
- `go test ./...` — run by me: **67 packages, 62 `ok`, 5 `[no test files]`, 0 FAIL.**
- `go.mod`/`go.sum` byte-unchanged in commit `bdebc5c9` — confirmed via
  `git show bdebc5c9 --stat -- go.mod go.sum` (empty diff).
- `compose.yaml` and `docs/adr/0037-per-identity-docker-sandbox.md` — read directly: both now
  describe the egress image as **LIVE**, name `buildSandboxRouter`/`WithEgress` explicitly, and
  disclose the fail-closed consequence. No overstatement remains.

**Per the task's evaluation framework, SC#4/SBX-04 now sits in the SAME category the original
verifier applied to SC#1 and SC#3: mechanism VERIFIED at the composition root; the LIVE DROP proof
on native-Linux dockerd remains a sanctioned WSL/CI human_verification deferral.** This is not a
downgrade of rigor — the actual code gap (the thing that made it a BLOCKER, not a mere deferred
test) is closed, confirmed by direct reading and independent command execution, not by trusting
`37-10-SUMMARY.md`.

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Under `server_production`, shell/fs target the per-identity sandbox and the real host filesystem is unreachable | ✓ VERIFIED (mechanism); live proof deferred | **Unchanged by 37-10 (regression-confirmed).** `router.go` `Route` short-circuits `(zero,false,nil)` under `!Strict()` (re-read lines 79-93; `TestRoute_DevNoOp` re-run, PASS). All 5 tools wired through the router (unchanged files; `TestShellExec_FailClosedNoHostFallback` re-run, PASS). Live in-box execution (`TestRoute_StrictExecInBox`, `TestSnippetExec_RoutedEndToEnd`) remains `docker_integration`-tagged — sanctioned WSL/CI deferral. |
| 2 | Docker-socket/`--privileged`/`--network host`/bind-mounts are unrepresentable (test-asserted) | ✓ VERIFIED | **Unchanged by 37-10 (regression-confirmed).** `TestSpec_NoHostExposureFields` and `TestTranslate_PinsSafe` (1000 `pgregory.net/rapid` cases) re-run by me: PASS. No files in this truth's chain were touched by 37-10. No live-test dependency — fully closed. |
| 3 | Cross-identity leakage is impossible and the idle-TTL lifecycle works | ✓ VERIFIED (mechanism); live proof deferred | **Unchanged by 37-10 (regression-confirmed).** `TestRoute_IdleBump`/`TestSpecFor_UsesConfiguredKnobs` re-run: PASS. `internal/cron/handlers` reaper tests (`TestSandboxReapMeta/RunSuspends/Disabled/RunError`) re-run: PASS; re-confirmed `grep -n "usersandbox" internal/cron/handlers/sandbox_reap.go` returns zero (no forbidden import). Live cross-identity-deny + lifecycle + auto-resume remain `docker_integration`-tagged — sanctioned deferral. |
| 4 | A configured egress allowlist cannot reach a disallowed host; default egress posture is full public internet minus the tenancy boundary, not `--network none` | ✓ **VERIFIED (mechanism) — composition-root gap CLOSED by 37-10**; live proof deferred | **THE FIX, independently verified:** `config.SandboxConfig.EgressImage` (non-empty default `aura-egress:latest`) → `buildSandboxRouter`'s `newSandboxBackend` → `usersandbox.WithEgress(cfg.Sandbox.EgressImage)` — confirmed by reading `config_sandbox.go`, `config_knobs.go`, `serve_dispatch.go`, `docker_backend.go` directly, and by grep (`WithEgress(cfg.Sandbox.EgressImage)` count==1 in serve_dispatch.go; repo-wide `AURA_SANDBOX_EGRESS_IMAGE` in `*.go`: 10 matches, was 0). Fail-CLOSED chain read end-to-end (ensureImage → launchEgress → Resolve → Route(_,true,err)); `TestShellExec_FailClosedNoHostFallback` + `TestBuildSandboxRouterWiresEgress` re-run/run: PASS. `go build`/`go vet`/tagged build all clean (re-run by me). `go.mod`/`go.sum` byte-unchanged (confirmed). The composition-root live DROP test (`TestBuildSandboxRouter_LaunchesEgressFloor`) exists, compiles under `-tags docker_integration`, and correctly **SKIPs** locally (ran myself — SKIP, not FAIL, no dockerd here) — the SAME sanctioned-deferral category as SC#1/#3, no longer a code gap. |
| 5 | An ADR records container-per-identity (K8s/gVisor-default → DGX) + a pre-merge concurrency benchmark on 32GB | ✓ VERIFIED (mechanism); live benchmark run deferred; **ADR accuracy gap RESOLVED** | `docs/adr/0037-per-identity-docker-sandbox.md` — re-read directly: the Negative-costs bullet and Residual B now explicitly disclose the composition-root wiring (`buildSandboxRouter` launches the sidecar via `usersandbox.WithEgress(cfg.Sandbox.EgressImage)`, "landed in 37-10") and the fail-CLOSED posture. The accuracy gap the previous verification flagged (ADR overstating a "delivered" control without disclosing the wiring path) is closed — no overstatement remains on direct re-read. `bench_soak_test.go` (`TestSoak_ConcurrentIdentities`) re-run with `-tags docker_integration`: compiles, vets clean, correctly SKIPs without `AURA_SANDBOX_SOAK_REALHOST=1`. Live 32GB-host run remains Manual-Only (unchanged). |

**Score:** 5/5 truths mechanism-verified (up from 4/5 at the previous verification); **0 FAILED**.
Truths #1, #3, #4, #5 each carry an outstanding WSL/CI live-tier proof (human_verification); #2 has
no live dependency and is fully closed.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/config/config_sandbox.go` | `EgressImage` field, non-empty default, `envDefault` loader line | ✓ VERIFIED | Read directly: field at line 81, const at line 35, loader at line 96. `grep -c 'EgressImage string'` == 1. |
| `internal/config/config_knobs.go` | `AURA_SANDBOX_EGRESS_IMAGE` KnobSpec row | ✓ VERIFIED | Read directly: line 136, `KindString`, `Default: "aura-egress:latest"`. `TestKnobRegistry` re-run: PASS. |
| `cmd/aura/serve_dispatch.go` (`buildSandboxRouter`/`newSandboxBackend`) | Composition-root wires `WithEgress(cfg.Sandbox.EgressImage)` | ✓ VERIFIED | Read directly: lines 150-174. `grep -c 'WithEgress(cfg.Sandbox.EgressImage)'` == 1 (independently re-run). Was `⚠️ INCOMPLETE` at the previous verification — now closed. |
| `internal/sandbox/usersandbox/docker_backend.go` (`EgressImage()`) | Read-only accessor for the docker-free wiring test | ✓ VERIFIED | Read directly: line 95, `func (b *DockerBackend) EgressImage() string { return b.egressImage }`. |
| `cmd/aura/serve_dispatch_egress_test.go` (NEW) | `TestBuildSandboxRouterWiresEgress` docker-free regression guard | ✓ VERIFIED | Read directly (56 LOC); ran myself: PASS. Asserts distinct pinned ref reaches `WithEgress`, default-loaded config is non-empty, non-strict stays nil. |
| `cmd/aura/serve_dispatch_egress_integration_test.go` (NEW) | `TestBuildSandboxRouter_LaunchesEgressFloor` composition-root live-DROP re-test | ✓ VERIFIED (compiles, gates correctly) | Read directly (247 LOC); `//go:build docker_integration` confirmed; CI-gate functions (`egressITDockerdOrGate`, `egressITEnforcingBridgeOrGate`) `t.Fatal` under `$CI` on unreachable/non-Linux daemon — no-skip-as-green honored. Ran with the tag: SKIPs locally (no dockerd), as expected. Live run is human_verification. |
| `internal/sandbox/usersandbox/egress.go` | egress sidecar spec + filter-table floor + FQDN mode | ✓ VERIFIED (now reachable from production) | Unchanged file, re-read for continuity; `TestEgress_FloorRuleset/RunscRejectsFQDN/CapOnSidecarOnly/AllowlistFromPolicy` re-run: PASS. No longer `VERIFIED-BUT-ORPHANED` — the only production call site now reaches it by default. |
| `compose.yaml` | `AURA_SANDBOX_EGRESS_IMAGE` comment states LIVE/consumed | ✓ VERIFIED | Read directly (lines 195-204): explicitly states `config.SandboxConfig` reads it and `buildSandboxRouter` passes it to `WithEgress`; documents the fail-CLOSED consequence. No longer overstates/understates. |
| `docs/adr/0037-per-identity-docker-sandbox.md` | SBX-05 ADR, now truthed-up on SBX-04 wiring | ✓ VERIFIED (substantive, accuracy gap resolved) | Read directly: Negative bullet + Residual B/C now name `buildSandboxRouter`/`WithEgress` explicitly. `grep -c 'buildSandboxRouter'` == 4. |
| `internal/config/config_sandbox_test.go` | `TestLoad_SandboxConfig` extended for the egress-image triplet | ✓ VERIFIED | Read directly: non-empty default assertion (lines 55-57) + digest-pinned override assertion (lines 94-96). Ran myself: PASS. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `router.go Route` | `config.RuntimeProfile.Strict()` | `!r.profile.Strict()` short-circuit | ✓ WIRED | Re-confirmed by direct read + `TestRoute_DevNoOp` re-run. |
| `router.go specFor` | `cfg.Sandbox` (config) | field-by-field mapping | ✓ WIRED | `TestSpecFor_UsesConfiguredKnobs` re-run: PASS. |
| `internal/agent/tools/*.go` | `internal/sandbox/usersandbox.SandboxRouter` | `Route(ctx)` at top of each Execute | ✓ WIRED | `TestShellExec_FailClosedNoHostFallback` re-run: PASS. Unchanged files. |
| `cmd/aura/serve_dispatch.go (buildSandboxRouter → newSandboxBackend)` | `internal/sandbox/usersandbox.WithEgress` | `WithEgress(cfg.Sandbox.EgressImage)` in the `NewDockerBackend` opts | ✓ **WIRED (was WIRED-BUT-DEAD)** | Independently confirmed by direct read (`serve_dispatch.go:170-174`) and grep (count==1). This is the link the BLOCKER was about — now reachable from the shipped binary. |
| `internal/config/config_sandbox.go (SandboxConfig.EgressImage)` | `AURA_SANDBOX_EGRESS_IMAGE` | `envDefault("AURA_SANDBOX_EGRESS_IMAGE", defaultSandboxEgressImage)` | ✓ **WIRED (was NOT_WIRED)** | Independently confirmed by direct read + `TestLoad_SandboxConfig` re-run (PASS, non-empty default asserted). |
| `internal/sandbox/usersandbox/docker_backend.go (launchEgress)` | the always-on floor (`egress.go buildEgressSidecar`) | `egressImage != ""` guard, now satisfied by the non-empty production default | ✓ **WIRED (was WIRED-BUT-DEAD)** | The guard condition is now reachable in production because the config default is non-empty; confirmed by reading `docker_backend.go` line 129 (`if b.egressImage == "" { return nil }`) alongside the confirmed non-empty default. |
| `EgressPolicy.FQDNAllowlist` (config-sourced) | `buildEgressSidecar` | `policy.FQDNAllowlist` param | ✓ WIRED | Unchanged; `TestEgress_AllowlistFromPolicy` re-run: PASS. Now reachable in production via the same composition-root fix. |
| `Resolve` (`docker_backend_lifecycle.go`) | `Route` (`router.go`) | error propagation on `launchEgress` failure | ✓ WIRED (fail-CLOSED, independently traced) | Read directly: `Resolve` line 69-71 wraps and returns the `launchEgress` error; `Route` line 84-86 returns `(BoxHandle{}, true, err)` on that error. `TestShellExec_FailClosedNoHostFallback` re-run: PASS. |

### Data-Flow Trace (Level 4)

Not applicable in the UI-rendering sense (this phase has no dynamic-data-rendering component); the
equivalent trace for this phase is the fail-closed error-propagation chain, covered above under Key
Link Verification (`Resolve` → `Route`) and independently traced by direct source reading rather
than trusting the SUMMARY's narrative.

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
|---|---|---|---|---|
| SBX-01 | 37-01, 37-02, 37-04, 37-05, 37-07, 37-09 | Host shell/fs tools execute inside per-identity box under strict profiles | ✓ SATISFIED (mechanism); live docker_integration proof pending (human_verification) | Unchanged by 37-10 (no files in this chain touched). Regression-confirmed: all unit tests re-run, PASS. |
| SBX-02 | 37-02 | Docker-socket/privileged/host-net/bind-mounts unrepresentable | ✓ SATISFIED | Unchanged. No live-test dependency. REQUIREMENTS.md checks this `[x]` — correct. |
| SBX-03 | 37-01, 37-03, 37-04, 37-05, 37-06 | Per-identity lifecycle + cross-identity isolation | ✓ SATISFIED (mechanism); live docker_integration proof pending (human_verification) | Unchanged by 37-10. Regression-confirmed: reaper + router unit tests re-run, PASS. |
| SBX-04 | 37-01, 37-05, 37-06, **37-10** | Enforced egress default + configured allowlist | ✓ **SATISFIED (mechanism) — composition-root BLOCKER CLOSED**; live docker_integration DROP proof pending (human_verification) | The composition-root wiring gap that BLOCKED this requirement is closed and independently confirmed (see Truth #4 above). The requirement's full behavioral guarantee (a disallowed host is actually DROPPED when the wired mechanism runs against a real bridge) still needs the native-Linux live proof — same category SBX-01/03 have always carried. |
| SBX-05 | 37-08 | ADR + D-14 concurrency benchmark | ✓ SATISFIED (mechanism); live 32GB-host run pending (human_verification); **ADR accuracy caveat RESOLVED** | ADR now accurately discloses the SBX-04 composition-root wiring (no more overstatement). Benchmark harness correctly gated; live run is Manual-Only Gate-3 evidence. |

**No orphaned requirements:** REQUIREMENTS.md's SBX-01..05 entries are each claimed by at least one
plan's frontmatter `requirements` list (37-10 declares `requirements: [SBX-04]`, confirmed by
reading its frontmatter); all five IDs are accounted for above.

### Requirements Checkbox Consistency — WARNING (human decision requested)

**REQUIREMENTS.md currently shows:** `SBX-01 [ ]`, `SBX-02 [x]`, `SBX-03 [ ]`, `SBX-04 [x]`, `SBX-05 [ ]`
(confirmed by direct grep of `.planning/REQUIREMENTS.md`).

**This is internally inconsistent.** All five requirements are now in one of exactly two states:

1. **No outstanding live-test dependency** — only SBX-02 (structural type-system claim, fully
   provable with local unit + property-based tests, zero Docker dependency). Correctly `[x]`.
2. **Mechanism fully verified in code; a WSL/CI live-daemon or real-host proof is still
   outstanding** — SBX-01, SBX-03, SBX-04, SBX-05 are ALL in this state. Before 37-10, SBX-04 was
   additionally blocked by a genuine code gap (the composition-root wiring), which is why the
   previous verification distinguished it from SBX-01/03/05. **That distinguishing code gap is now
   closed.** SBX-04 is therefore in the exact same state SBX-01/03/05 have always been in:
   mechanism-verified, live proof pending.

`STATE.md`'s own 37-10 entry explicitly documents the reasoning for flipping SBX-04 to `[x]`:
*"SBX-04 marked `[x]` (mechanism wired + docker-free regression-proven; the live DROP is the
documented WSL/CI Gate-3 must-run, matching the phase's WSL/CI-deferral precedent)."* This
reasoning is sound on its own terms, but it was **not applied symmetrically** — SBX-01/03/05 are
equally "mechanism wired/verified + docker-free-or-unit-proven, live proof deferred to WSL/CI
Gate-3," yet they remain unchecked.

**Two ways to resolve this, either is defensible — this is a decision for the developer, not
something I will silently correct:**

- **Option A (conservative — recommended):** Revert `SBX-04` to `[ ]` until the composition-root
  live DROP test (`TestBuildSandboxRouter_LaunchesEgressFloor`) and the backend-level
  `TestEgress_FloorDropsInternal`/`TestEgress_FQDNAllowlist` actually pass on native-Linux dockerd.
  This matches the phase's own established convention (checked = fully E2E-verified including live
  proof, per CLAUDE.md's "DEFINITION OF DONE... fully validate E2E") and keeps SBX-01/03/04/05
  symmetric (all `[ ]` pending WSL/CI; only SBX-02 `[x]`).
- **Option B (symmetric-relaxed):** If the project instead wants "checked" to mean "mechanism +
  composition-root wiring code-complete, no known BLOCKER — live proof tracked separately as Gate-3
  evidence in `37-VALIDATION.md`," then SBX-01, SBX-03, and SBX-05 should ALSO be flipped to `[x]`
  now, for consistency with SBX-04's new state — all four are equally mechanism-complete with only
  live-tier evidence outstanding.

**Not proposed:** leaving the current mixed state (`SBX-04 [x]` while `SBX-01/03/05 [ ]`) as-is —
it is not self-consistent under either reading of what the checkbox means.

### Anti-Patterns Found

Re-scanned all 10 files touched by 37-10
(`internal/config/config_sandbox.go`, `internal/config/config_knobs.go`,
`internal/config/config_sandbox_test.go`, `cmd/aura/serve_dispatch.go`,
`internal/sandbox/usersandbox/docker_backend.go`, `cmd/aura/serve_dispatch_egress_test.go`,
`cmd/aura/serve_dispatch_egress_integration_test.go`,
`internal/sandbox/usersandbox/egress_integration_test.go`, `compose.yaml`,
`docs/adr/0037-per-identity-docker-sandbox.md`) directly via grep:

- No `TBD`/`FIXME`/`XXX` markers found.
- No `TODO`/`HACK`/`PLACEHOLDER` markers found.
- One incidental case-insensitive "placeholder" match: `serve_dispatch_egress_test.go:33` —
  `// placeholder; clear AURA_SANDBOX_EGRESS_IMAGE to force the in-code default...` — this refers to
  a placeholder **API-key test value** (`sk-test-egress-wiring`), not a code stub. Not an
  anti-pattern.
- No empty-implementation or hardcoded-empty-data stubs found. `EgressImage()` returns real wired
  state; the config default is a live, non-empty value (confirmed by reading and by the passing
  `TestLoad_SandboxConfig`/`TestBuildSandboxRouterWiresEgress` assertions on the actual value).

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|---|---|---|---|
| Whole-module build | `go build ./...` | clean, no output | ✓ PASS (re-run) |
| Whole-module vet | `go vet ./...` | clean, no output | ✓ PASS (re-run) |
| Tagged build | `go build -tags docker_integration ./...` | clean, no output | ✓ PASS (re-run) |
| Tagged vet on touched packages | `go vet -tags docker_integration ./cmd/aura/ ./internal/sandbox/usersandbox/` | clean, no output | ✓ PASS (re-run) |
| Whole-module unit tests | `go test ./...` | 67 packages: 62 `ok`, 5 `[no test files]`, 0 FAIL | ✓ PASS (re-run) |
| Config sandbox knob tests | `go test ./internal/config/ -run 'TestLoad_SandboxConfig\|TestKnobRegistry\|TestReparsePass'` | all PASS | ✓ PASS (re-run) |
| Docker-free egress wiring guard | `go test ./cmd/aura/ -run 'TestBuildSandboxRouterWiresEgress\|TestBuildDispatchRegistersSandboxReap'` | all PASS | ✓ PASS (re-run) |
| SC#1/#2/#3 regression (unit) | `go test ./internal/sandbox/usersandbox/ -run "TestSpec_NoHostExposureFields\|TestTranslate_PinsSafe\|TestSpec_RunscOnlyServerProduction\|TestRoute_DevNoOp\|TestRoute_FailClosed\|TestRoute_LocalFallback\|TestRoute_IdleBump\|TestSpecFor_UsesConfiguredKnobs"` | all PASS | ✓ PASS (re-run, no regression) |
| Fail-closed tool deny (regression) | `go test ./internal/agent/tools/ -run TestShellExec_FailClosedNoHostFallback` | PASS | ✓ PASS (re-run) |
| Reaper regression | `go test ./internal/cron/handlers/ -run TestSandboxReap` | all 4 subtests PASS | ✓ PASS (re-run, no regression) |
| Egress composition-root reachability | `grep -rn "AURA_SANDBOX_EGRESS_IMAGE" --include="*.go" .` | 10 matches (was 0) | ✓ PASS — BLOCKER symptom inverted (re-run) |
| Egress composition-root reachability | `grep -c 'WithEgress(cfg.Sandbox.EgressImage)' cmd/aura/serve_dispatch.go` | 1 | ✓ PASS — the exact production wiring exists exactly once (re-run) |
| Egress composition-root reachability | `grep -n "WithEgress" cmd/aura/*.go` | 8 matches across 3 files (doc comments + the 1 real call + 2 test-file usages) | ✓ PASS — no longer 0 |
| docker_integration composition-root live test | `go test -tags docker_integration ./cmd/aura/ -run TestBuildSandboxRouter_LaunchesEgressFloor -v` | SKIP (no dockerd), not FAIL | ✓ PASS (sanctioned skip posture, re-run) |
| docker_integration backend-level egress tests | `go test -tags docker_integration ./internal/sandbox/usersandbox/ -run TestEgress -v` | 2 SKIP (no dockerd) + 4 unit PASS | ✓ PASS (sanctioned skip posture, re-run) |
| D-14 soak harness | `go test -tags docker_integration ./internal/sandbox/usersandbox/ -run TestSoak -v` | SKIP (real-32GB-host only) | ✓ PASS (sanctioned skip posture, re-run) |
| `-race` availability | `go test -race ./cmd/aura/ -run TestBuildSandboxRouterWiresEgress` | `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1` | ✗ CANNOT RUN — confirms `CGO_ENABLED=0` on this Windows host (CLAUDE.md-documented limitation); routed to human_verification |
| `go.mod`/`go.sum` diff | `git show bdebc5c9 --stat -- go.mod go.sum` | empty (no file lines) | ✓ PASS — byte-unchanged, confirmed |
| File-size discipline | `wc -l` on the 6 non-doc files 37-10 touched/created | max 247 LOC (`serve_dispatch_egress_integration_test.go`) | ✓ PASS (≤600 cap honored) |

### Probe Execution

No `scripts/*/tests/probe-*.sh` files found for this phase; no probes declared in any
37-0X-PLAN.md/SUMMARY.md. Step 7c: SKIPPED (no probes declared).

### Human Verification Required

See the `human_verification` YAML block in the frontmatter above for the four sanctioned-deferral
items:

1. **Full `docker_integration` + `-race` suite on native-Linux dockerd (WSL/CI)** — covers SC#1
   (in-box execution) and SC#3 (cross-identity-deny, lifecycle, reaper auto-resume), plus `-race`
   clean on the 37-10-touched packages (`cmd/aura`, `internal/config`,
   `internal/sandbox/usersandbox`). Cannot run here: no dockerd, `CGO_ENABLED=0` (confirmed).
2. **Composition-root + backend-level live egress DROP proof** on native-Linux non-masquerading
   dockerd with the `aura-egress` image built — the closing SC#4/SBX-04 evidence. Cannot run here:
   no dockerd (confirmed — both tests SKIP cleanly).
3. **D-14 32GB concurrency soak** on the real host — SC#5/SBX-05 evidence. Cannot run here: WSL
   capped at 15.47 GiB (confirmed — deliberately gated, SKIPs correctly).
4. **gVisor `runsc` smoke test** — supplementary SC#4/SBX-04 (D-12) evidence. Cannot run here: no
   `runsc` runtime, no native-Linux host.

None of these are code gaps — all four are the same sanctioned WSL/CI deferral category this phase
has used from its first verification onward (Phase-36 precedent, CLAUDE.md-documented Windows-dev-host
limitation). They gate the final Gate-3 close but do NOT block this re-verification's status from
reflecting that the composition-root BLOCKER is closed.

Additionally, see **Requirements Checkbox Consistency** above — a documentation/tracking decision
(not a live-test item) that needs an explicit developer choice before Gate-3 close.

### Gaps Summary

**The BLOCKER is closed.** Plan 37-10 (commit `bdebc5c9`) surgically wired
`usersandbox.WithEgress(cfg.Sandbox.EgressImage)` into `buildSandboxRouter`'s only production
construction path, sourced from a new `config.SandboxConfig.EgressImage` field defaulting
non-empty (`aura-egress:latest`) and cataloged in the KnobSpec registry. I independently confirmed
every load-bearing claim by reading the actual source files and re-running the build/vet/test
commands myself — not by trusting `37-10-SUMMARY.md`'s narrative. The repo-wide
`AURA_SANDBOX_EGRESS_IMAGE`-in-`*.go` grep that returned 0 matches at the previous verification
(the smoking-gun symptom of the BLOCKER) now returns 10, confirmed by my own re-run. The fail-closed
posture (a strict box refuses to start rather than run un-floored) was traced end-to-end through
three files I read directly (`docker_backend.go` → `docker_backend_lifecycle.go` → `router.go`),
not asserted from the summary.

All five ROADMAP success criteria are now mechanism-complete in the codebase. Four of them (SC#1,
#3, #4, #5) still have a live-tier proof outstanding — each is a sanctioned WSL/CI deferral this
Windows verification host cannot execute (no dockerd, `CGO_ENABLED=0`, no 32GB host, no `runsc`
runtime), consistent with the Phase-36 precedent this repo already follows. SC#2 has no such
dependency and is fully closed.

**Status is `human_needed`, not `passed`** — while no truth is FAILED, four outstanding
human-verification items exist (per Step 9 of the verification process, any non-empty human
verification list forces `human_needed` even when every truth is otherwise VERIFIED). None of the
four live tiers were run or claimed to pass by this verification; each is explicitly marked
"pending"/"deferred"/"SKIP" above.

**One documentation-consistency issue requires a developer decision** (see the dedicated section
above): REQUIREMENTS.md currently checks `SBX-04 [x]` while leaving `SBX-01/03/05 [ ]`, even though
all four are now in the identical "mechanism-verified, live-proof-pending" state. This is an
internal inconsistency introduced when the 37-10 gap-closure docs commit flipped only SBX-04 (per
its own `requirements-completed: [SBX-04]` frontmatter) without reconciling against the
already-established convention for SBX-01/03/05. Recommend Option A (revert SBX-04 to `[ ]` until
its live DROP proof passes) for consistency with the phase's stricter, already-precedented
discipline — but this is presented as a choice for the developer, not silently corrected.

---

*Verified: 2026-07-07*
*Verifier: Claude (gsd-verifier) — re-verification after gap-closure plan 37-10*
