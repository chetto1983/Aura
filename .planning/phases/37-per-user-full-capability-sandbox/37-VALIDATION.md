---
phase: 37
slug: per-user-full-capability-sandbox
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-06
---

# Phase 37 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `37-RESEARCH.md` → `## Validation Architecture`. Per-task rows
> are seeded at requirement level; task IDs are backfilled once PLAN.md files exist.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` + `testify` + `pgregory.net/rapid` (property-based) + `go.uber.org/goleak` (leak gate) |
| **Config file** | none (Go convention); build tags gate tiers |
| **Quick run command** | `go test ./internal/sandbox/usersandbox/` (unit — spec/translator/router, no Docker) |
| **Full suite command** | `go test -tags="docker_integration" -race ./internal/sandbox/...` (needs a live dockerd) + `make quality-full` |
| **Estimated runtime** | unit ~sub-second; integration ~30–90s on live dockerd |

**New build tag:** `docker_integration` (mirrors the existing `db_integration` / `neo4j_integration` convention). The skip-helper MUST `t.Fatal` under `$CI` when `dockerd` is unreachable (no-skip-as-green, CLAUDE.md).

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test ./internal/sandbox/usersandbox/` (unit — sub-second, no Docker)
- **After every plan wave:** `go test -tags=docker_integration -race ./internal/sandbox/...` on a live dockerd (CI Linux)
- **Before `/gsd-verify-work`:** `make quality-full` green (owned-surface coverage ≥85%, mutation ≥70% on `translate.go`/`router.go`/`egress.go` per REL-02) **+ the D-14 soak on the real 32GB host**
- **Max feedback latency:** ~90 seconds (integration tier)

---

## Per-Task Verification Map

> Requirement-level seed. `Task ID` / `Plan` / `Wave` columns are backfilled by the planner
> (must_haves) and the nyquist-auditor once PLAN.md task IDs are assigned.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD | TBD | TBD | SBX-01 | T-37-fail-open | Strict → shell/fs route into box; host FS unreachable | integration | `go test -tags=docker_integration ./internal/sandbox/usersandbox/ -run TestRoute_StrictExecInBox` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | SBX-01 | — | dev/local_trusted → routing no-op (host-direct) | unit | `go test ./internal/sandbox/usersandbox/ -run TestRoute_DevNoOp` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | SBX-02 | T-37-socket/priv/hostnet | Privileged/host-net/bind/socket unrepresentable — structural | unit | `go test ./internal/sandbox/usersandbox/ -run TestSpec_NoHostExposureFields` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | SBX-02 | T-37-adversarial-spec | Translator always emits safe HostConfig for adversarial specs | unit (table + rapid) | `go test ./internal/sandbox/usersandbox/ -run TestTranslate_PinsSafe` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | SBX-03 | T-37-cross-vol | Cross-identity `/workspace` leak impossible (A writes, B can't read) | integration | `go test -tags=docker_integration ./internal/sandbox/usersandbox/ -run TestVolume_CrossIdentityDeny` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | SBX-03 | — | create→suspend→resume→delete; volume retained on suspend, gone on delete | integration | `go test -tags=docker_integration ./internal/sandbox/usersandbox/ -run TestLifecycle_SuspendResumeDelete` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | SBX-03 | — | Idle-TTL reaper suspends via the scheduler; next call auto-resumes | integration | `go test -tags=docker_integration ./internal/sandbox/usersandbox/ -run TestReap_IdleSuspendAutoResume` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | SBX-04 | T-37-lateral/metadata | Default floor: box reaches public internet, CANNOT reach RFC1918/metadata/bridge | integration (native-Linux) | `go test -tags=docker_integration ./internal/sandbox/usersandbox/ -run TestEgress_FloorDropsInternal` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | SBX-04 | T-37-egress-bypass | Tightened allowlist: allowed host resolves, disallowed host DROPPED | integration (native-Linux, runc) | `go test -tags=docker_integration ./internal/sandbox/usersandbox/ -run TestEgress_FQDNAllowlist` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | SBX-04 | — | `runtime: runsc` selectable under server_production (spec accepts) | unit | `go test ./internal/sandbox/usersandbox/ -run TestSpec_RunscOnlyServerProduction` | ❌ W0 | ⬜ pending |
| 1 | 37-08 | 4 | SBX-05 | — | ADR file exists with the required decisions (D-15 + 3 residuals) | manual/doc | `test -f docs/adr/0037-per-identity-docker-sandbox.md && grep -qi container-per-identity docs/adr/0037-per-identity-docker-sandbox.md` | ✅ `docs/adr/0037-per-identity-docker-sandbox.md` | ✅ green |
| 2 | 37-08 | 4 | SBX-05 | T-37-08-STARVE/FALSEBENCH | D-14 soak: 10–20 boxes fit 32GB + headroom, Resolve/Resume p95, cgroup caps prevent starvation | integration (real-32GB-host) | `AURA_SANDBOX_SOAK_REALHOST=1 go test -tags docker_integration ./internal/sandbox/usersandbox/ -run TestSoak` | ✅ `internal/sandbox/usersandbox/bench_soak_test.go` | ⬜ pending (Manual-Only) |
| TBD | TBD | TBD | GATE-01/D-09 | T-37-fail-open | Strict + box-create failure → fail-CLOSED ToolResult, never host | unit | `go test ./internal/agent/tools/ -run TestShellExec_FailClosedNoHostFallback` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | — | — | No goroutine leak on box lifecycle / reaper | integration | `goleak` in package `TestMain` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/sandbox/usersandbox/spec_test.go` — SBX-02 structural + translator tests (no Docker; do first)
- [ ] `internal/sandbox/usersandbox/router_test.go` — `Strict()`-no-op + fail-CLOSED (D-09)
- [ ] `internal/sandbox/usersandbox/docker_backend_integration_test.go` — full moby SDK round-trip smoke (Pitfall 7 — earliest real-dockerd proof of the v0.4.1 options-struct API)
- [ ] `internal/sandbox/usersandbox/egress_integration_test.go` — floor DROP + FQDN (native-Linux, `t.Fatal` under `$CI`)
- [ ] `internal/cron/handlers/sandbox_reap_test.go` — reaper handler (mirror `identity_purge_test.go`)
- [ ] Build-tag skip-helper for `docker_integration` that `t.Fatal`s under `$CI` when dockerd is unreachable
- [ ] New migration widening `scheduler_tasks.kind` CHECK for `sandbox_reap` (mirror 0033)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| D-14 concurrency soak — 10–20 concurrent per-identity boxes fit the 32GB envelope with headroom; Resolve p95 <~2s; Resume-from-suspend p95 <~1s; cgroup caps (2 CPU / 2GB / 512 pids start) prevent starvation | SBX-05 success-criterion 5 | Dev WSL Docker is capped at 15.47 GiB — insufficient; needs the real 32GB host | See **D-14 concurrency-soak run instructions** below; record the results table as the Gate-3 evidence |
| Egress enforcement DROP (floor + FQDN) | SBX-04 | Docker's dev NAT/userland-proxy can mask enforcement; must run on native Linux | Run `TestEgress_*` on a native-Linux runner (CI Linux), never WSL/Docker-Desktop NAT |
| **Composition-root live egress DROP** (`buildSandboxRouter` → `Route`, 37-10) | SBX-04 | Docker-Desktop/WSL vpnkit NATs the bridge and cannot validate DROP (Pitfall 3); needs a native-Linux non-masquerading dockerd + the `aura-egress` image built | `docker build docker/aura-egress` then `AURA_EGRESS_ENFORCE=1 go test -tags docker_integration -race ./cmd/aura/ -run TestBuildSandboxRouter_LaunchesEgressFloor` on native Linux (or under `$CI`). Expected: a box created via the production composition root carries its `aura-egress-<id>` sidecar (NET_ADMIN + shared box netns), reaches the public internet but is DROPPED from `169.254.169.254` / RFC1918; Stop leaves no orphan. This is the closing live proof for the SBX-04 BLOCKER — must run green before Phase 37 Gate-3 close. |
| gVisor `runsc` tier smoke | SBX-04/D-12 | Requires `runsc` runtime installed + note that FQDN-allowlist (nat table) is runc-only (research #934) | Bring up a box with `runtime: runsc` under server_production; confirm exec works and the filter-table floor still drops internal |

### D-14 concurrency-soak run instructions (Gate-3 evidence)

**Harness:** `internal/sandbox/usersandbox/bench_soak_test.go` → `TestSoak_ConcurrentIdentities`
(`//go:build docker_integration`). It resolves N per-identity boxes concurrently, then
suspends+resumes them, samples aggregate RAM against the 32 GB envelope, and runs a starvation
probe (a CPU-hog box must not starve a co-tenant under the cgroup caps).

**Gating (T-37-08-FALSEBENCH):** the pass/fail assertions run ONLY with
`AURA_SANDBOX_SOAK_REALHOST=1`. Without it (dev WSL / ordinary CI) the test SKIPS with a
real-host-only message — the 15.47 GiB WSL cap cannot validate the 10–20-box envelope, so a run
there would be a false pass/fail. This is a **sanctioned skip** (the benchmark is deliberately
excluded from the CI functional tier).

**Run on the real 32 GB host (native Linux, dockerd reachable):**

```bash
# Build/pull the fat box image first (default AURA_SANDBOX_TEST_IMAGE is busybox:stable — set
# it to the real aura-sandbox image for a faithful RAM footprint).
export AURA_SANDBOX_SOAK_REALHOST=1
export AURA_SANDBOX_TEST_IMAGE=aura-sandbox:latest   # or the digest-pinned registry ref
export AURA_SANDBOX_SOAK_N=15                         # 10–20 envelope; default 10
#   optional overrides (defaults mirror config.SandboxConfig D-14 starts):
#   AURA_SANDBOX_CPU_LIMIT=2  AURA_SANDBOX_MEMORY_LIMIT=2147483648  AURA_SANDBOX_PIDS_LIMIT=512
#   AURA_SANDBOX_SOAK_HEADROOM_BYTES=2147483648  (min free RAM required after N boxes are live)
go test -tags docker_integration ./internal/sandbox/usersandbox/ -run TestSoak -count=1 -v
```

The harness prints a `D-14 concurrency-soak (Gate-3 evidence)` block; transcribe it into the
results table below. **Pass = all four rows green** (fits 32 GB with headroom + no starvation —
not a scale SLA, D-14).

| Run date | Host (MemTotal) | N | Per-box caps | Aggregate footprint | Free RAM after N | Resolve p95 (<~2s) | Resume p95 (<~1s) | Starvation-free (co-tenant p95 <2s) | Verdict |
|----------|-----------------|---|--------------|---------------------|------------------|--------------------|-------------------|-------------------------------------|---------|
| _pending 32GB-host run_ | ~32 GiB | 10–20 | 2 CPU / 2 GiB / 512 pids | _fill_ | _fill (≥ headroom)_ | _fill_ | _fill_ | _fill_ | ⬜ pending |
| 2026-07-07 (WSL, **informational** — 9 GiB < 32 GiB, T-37-08-FALSEBENCH) | 9.71 GiB | 8 | 2 CPU / 2 GiB / 512 pids | ~0 GiB (busybox) | 4.98 GiB (≥ 1 GiB) | **865 ms** | **361 ms** | **89 ms → starvation-free=true** | ✅ mechanism (32 GB envelope verdict still appliance-only) |

---

## Live UAT Results — 2026-07-07 (WSL Ubuntu, Docker 29.6.1, go 1.26.4 + gcc 15.2, `-race`)

Run autonomously after the 37-10 gap closure with the stack up. Environment: WSL2 **Docker Desktop**
engine (`docker info` → Name=docker-desktop, vpnkit), 9.71 GiB RAM, no gVisor `runsc`.

| Tier | Requirement | Result | Evidence |
|------|-------------|--------|----------|
| `usersandbox` docker_integration `-race` suite | SBX-01/03 | ✅ **LIVE PASS** | RoundTrip, Lifecycle (Suspend/Resume/Delete), VolumeCrossIdentityDeny, MaterializesInputs, Reap (IdleSuspendAutoResume), all Route*/Spec*/Translate — real containers, race-clean (10.6 s) |
| npm `docx` + `xlsx` skills in box | SBX-01 (skill exec) | ✅ **LIVE PASS** | aura-sandbox box (Node 24.18 / npm 11.16 / py 3.11); `npm install docx xlsx` on-demand; generated a valid 8776-B `.docx` (`word/document.xml`) + 16215-B `.xlsx` (`xl/workbook.xml`, live `SUM()` formula, round-tripped) |
| D-14 concurrency soak (9 GB, N=8) | SBX-05 | ✅ **mechanism** (informational) | Resolve p95 865 ms (<2 s), Resume p95 361 ms (<1 s), starvation-free=true; 32 GB / N=10–20 envelope verdict still appliance-only (T-37-08-FALSEBENCH) |
| Composition-root egress DROP | SBX-04 | ⚠️ **Pitfall-3 gated** | Sidecar launched via `buildSandboxRouter` with NET_ADMIN + shared netns (confirmed live); RFC1918 DROP confirmed indirectly — the box's RFC1918 DNS (`192.168.65.7`) was dropped by the floor. Full "reach-public-AND-drop-internal" needs a native-Linux non-masquerading dockerd (Docker Desktop's DNS is *itself* RFC1918). Latent cap-assertion bug (`CAP_` prefix) FIXED (commit `abc578b5`). |
| Tool-routing tests (`agent/tools`) | SBX-01 | ⚠️ **gVisor-gated** | server_production → `runsc`; runsc not installed. Tool→box execution proven live via **runc** (usersandbox Route tests + the docx/xlsx skill). Observation: these tests hard-fail without runsc instead of gating on availability (pre-existing robustness gap, not 37-10). |
| gVisor `runsc` smoke | SBX-04/D-12 | ❌ **not runnable** | runsc not installed on the Docker Desktop WSL2 engine |

**Net (2026-07-07 WSL run):** SBX-02 (unit) + SBX-03 (lifecycle / cross-identity / reap) LIVE-verified; SBX-01
core routing + real skill execution live-verified (runc); SBX-04 composition-root wiring + sidecar launch +
RFC1918-drop live-evidenced. Full egress DROP, gVisor `runsc`, and the 32 GB soak remained infra-gated on WSL.
**Superseded for the egress DROP + lifecycle tiers by the 2026-07-08 native-dockerd run below.**

## Live UAT Results — 2026-07-08 (casaserver 192.168.1.21, NATIVE-Linux dockerd)

Environment: **casaserver** — Ubuntu 24.04, kernel 6.8, **native Docker Engine** (`docker context` = `default`,
NOT Docker-Desktop → **non-masquerading bridge**, satisfies 37-RESEARCH Pitfall 3), 8 GB / 4-core (shared home
server also running Home Assistant + Immich + Ollama). go 1.26.4, **no `-race`** (no gcc on the box). Box image
`busybox:stable`, sidecar `aura-egress:latest`.

| Tier | Requirement | Result | Evidence |
|------|-------------|--------|----------|
| Backend egress DROP floor | SBX-04 | ✅ **LIVE PASS** | `TestEgress_FloorDropsInternal` (`AURA_EGRESS_ENFORCE=1`) --- PASS 41.17s: box REACHED example.com, DROPPED from `10.0.0.1` (RFC1918) + `169.254.169.254` (metadata); sidecar (NET_ADMIN on sidecar only, shared box netns) torn down clean on Stop (no orphan). **The Pitfall-3 gate is CLEARED — full reach-public-AND-drop-internal proven.** |
| Composition-root egress DROP | SBX-04 | ✅ **LIVE PASS** | `TestBuildSandboxRouter_LaunchesEgressFloor` (`AURA_SANDBOX_IMAGE=busybox:stable`) --- PASS 16.85s: the PRODUCTION `buildSandboxRouter → Route` path launched the sidecar + enforced the same boundary. busybox box avoids the fat aura-sandbox image; wiring is identical. |
| `usersandbox` docker_integration suite | SBX-01/03 | ✅ **LIVE PASS** | full package `ok 79.29s` (RoundTrip, Lifecycle Suspend/Resume/Delete, VolumeCrossIdentityDeny, MaterializesInputs, Reap, all Route*/Spec*/Translate) on native dockerd. |
| gVisor `runsc` smoke | SBX-04/D-12 | ⬜ **not run** | needs runsc install + a Docker daemon restart that would bounce the host's HA/Immich/Ollama — not run on the shared server. |
| D-14 32 GB soak | SBX-05 | ⬜ **impossible here** | 8 GB box; 32 GB envelope is appliance-only. |
| FQDN allowlist | SBX-04 | ⬜ **not run** | needs an `aura-egress` image built with the pinned OpenSandbox binary. |

**Net (updated):** SBX-01, SBX-02, SBX-03, and **SBX-04 egress DROP (backend floor AND composition-root)** are
now LIVE-verified on a native-Linux non-masquerading dockerd. **Genuinely remaining (REL-03 must-runs, NOT code
gaps):** native `-race` (no gcc; `-race` green on WSL 2026-07-07), FQDN-allowlist image, gVisor `runsc`, and the
32 GB soak envelope. Actionable follow-up unchanged: **WR-01** — a native-Linux `docker_integration` CI job
(there is none today) would run all of these under `$CI` fail-closed.

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
