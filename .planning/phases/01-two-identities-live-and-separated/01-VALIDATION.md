---
phase: "1"
slug: "two-identities-live-and-separated"
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: "2026-09-07"
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from `01-RESEARCH.md` § Validation Architecture. The Per-Task Verification Map
> is filled by the planner; every value below it was read from source, not assumed.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` + build tags; no external test framework |
| **Config file** | none — the `//go:build` tag lines on each file are the config |
| **Quick run command** | `go vet -tags 'db_integration garage_integration authula_integration musr_e2e arcadedb_integration' ./cmd/aura/` (compile-only floor, mirrors `ci.yml:465`) |
| **Full suite command** | `make musr-e2e` (new target this phase — disposable Postgres + Garage + ArcadeDB bring-up, seed, tagged run, teardown) |
| **Estimated runtime** | `TestTwoIdentityCrossDeny` measured at 2.98s on 2026-09-07; full `make musr-e2e` including bring-up not yet measured |

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test ./internal/<touched>/ && go test -race ./internal/<touched>/` (CLAUDE.md § Post-edit validation)
- **After every plan wave:** the tagged tier for that wave — the `go vet -tags` compile floor at minimum; `make musr-e2e` once the ArcadeDB/Garage bring-up leg exists
- **Before `/gsd-verify-work`:** `make musr-e2e` green in CI
- **Max feedback latency:** ~60s for the per-commit loop; the tagged tier is bring-up bound and not yet measured

---

## Build Tags and Owning Gates

| Tag | What it gates | Owning gate |
|-----|---------------|-------------|
| `db_integration` | Postgres-backed assertions (conversations, approvals, documents/RLS) | `musr-e2e` CI job (existing) |
| `garage_integration` | Garage object-store cross-deny | `musr-e2e` CI job (existing) |
| `authula_integration` | Real Authula-configured stack precondition | `musr-e2e` CI job (existing) |
| `musr_e2e` | The two-identity acceptance file itself | `musr-e2e` CI job (existing) |
| `arcadedb_integration` | ArcadeDB long-term-memory cross-deny — the plane the existing gate does not cover | `musr-e2e` CI job, extended (new this phase) |
| unit (untagged) | Config gate logic, CLI flag parsing | `make quality` (existing) |
| `-race` + `goleak` | The concurrent-runner separation test | `musr-e2e` CI job; `-race` already present at `ci.yml:543` |

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| _(filled by planner)_ | | | | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers ISO-01's config-gate assertion (`internal/config/config_validate_test.go:233-247`)
and ISO-02's five already-proven planes (`TestTwoIdentityCrossDeny`). Everything below is new:

- [ ] Sandbox-image boot preflight test — ISO-01's second half; production code does not exist yet
- [ ] `arcadedb_integration`-tagged memory cross-deny subtests inside the existing `TestTwoIdentityCrossDeny` tree — ISO-02
- [ ] `make musr-e2e` target + its bring-up script — ISO-02a, the unattended/clean-checkout/CI leg
- [ ] Concurrent-runner white-box test (`-race` + `goleak`, goroutines inside one `go test` process) — ISO-05
- [ ] Live closing harness driving two concurrent authenticated `/agent/run` conversations — E2E-01
- [ ] `aura identity create` CLI verb, exercised inside the seed step — E2E-02

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Conversation quality of the two concurrent live identities, scored ≥9.8 | E2E-01 | The score is a judgement against a written rubric; the machine-checkable half of the same run (both conversations complete, tools fire, no cross-read) is what blocks | Run the closing harness against a live `aura serve` with two provisioned identities; score both transcripts against the phase rubric and record the evidence |
| Mutation spot-check ≥70% killed on the phase's critical file(s) | CLAUDE.md gate | `go-mutesting` runs only under WSL and is not wired into CI | `GOFLAGS=-tags=db_integration go-mutesting ./internal/<critical>/` under WSL with the composed DSNs exported |

---

## Sampling Argument — what these checks would miss

- The concurrent-runner test samples **one deliberate collision shape** (same tool, same arguments,
  overlapping in time). A leak that manifests only under a different shape — different tools racing,
  or a three-way race — is not covered. Randomized interleaving is explicitly deferred, not hidden.
- The `musr-e2e` CI job runs `-p 1` (`ci.yml:543`) to serialize shared Postgres writes. The
  concurrency this phase must prove therefore has to be **goroutines within one `go test` process**;
  separate `go test` invocations would be serialized away and the test would pass vacuously.
- The shared-surface list is a **floor, not a ceiling**. Proving the enumerated surfaces disjoint does
  not prove no other process-wide singleton leaks.

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency measured and recorded
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
