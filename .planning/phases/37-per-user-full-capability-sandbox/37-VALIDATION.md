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
| TBD | TBD | TBD | SBX-05 | — | ADR file exists with the required decisions | manual/doc | reviewer checks `docs/adr/` | ❌ W0 | ⬜ pending |
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
| D-14 concurrency soak — 10–20 concurrent per-identity boxes fit the 32GB envelope with headroom; Resolve p95 <~2s; Resume-from-suspend p95 <~1s; cgroup caps (2 CPU / 2GB / 512 pids start) prevent starvation | SBX-05 success-criterion 5 | Dev WSL Docker is capped at 15.47 GiB — insufficient; needs the real 32GB host | Run the soak harness (`bench_soak_test.go` or `cmd/` tool) on the 32GB host; record RAM envelope + p95 latencies + no-starvation into this table as the Gate-3 evidence |
| Egress enforcement DROP (floor + FQDN) | SBX-04 | Docker's dev NAT/userland-proxy can mask enforcement; must run on native Linux | Run `TestEgress_*` on a native-Linux runner (CI Linux), never WSL/Docker-Desktop NAT |
| gVisor `runsc` tier smoke | SBX-04/D-12 | Requires `runsc` runtime installed + note that FQDN-allowlist (nat table) is runc-only (research #934) | Bring up a box with `runtime: runsc` under server_production; confirm exec works and the filter-table floor still drops internal |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
