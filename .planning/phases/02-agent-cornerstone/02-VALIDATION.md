---
phase: 2
slug: agent-cornerstone
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-29
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `02-RESEARCH.md` § Validation Architecture (27 named tests across 4 SC + supporting acceptance + property-based + fuzz).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (Go 1.26.3) + `pgregory.net/rapid` v1.3.0 (property) + `go.uber.org/goleak` v1.3.0 (leak) |
| **Config file** | none — `golangci-lint` config installed in Wave 0 (CONVENTIONS.md L19 gap, RESEARCH finding #3) |
| **Quick run command** | `go test ./internal/agent/... ./internal/canonicaljson/...` |
| **Full suite command** | `go vet ./... && go build ./... && go test -race ./internal/agent/... ./internal/canonicaljson/... && bash scripts/loop_budget_smoke.sh` |
| **Estimated runtime** | ~20–40 seconds (unit + race + smoke; unit-only phase, no DB/Neo4j tiers) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/<package touched>/` + `go vet ./...`
- **After every plan wave:** Run `go test -race ./internal/agent/... ./internal/canonicaljson/...`
- **Before `/gsd:verify-work`:** Full suite must be green (incl. `-race` + `scripts/loop_budget_smoke.sh`)
- **Max feedback latency:** 40 seconds

---

## Per-Task Verification Map

> Populated by the planner / Nyquist auditor from PLAN.md tasks. Anchor tests from RESEARCH.md § Validation Architecture:

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 2-XX-XX | XX | 1 | INFRA-03 | — | budget hard total bound never exceeded (SC#3) | unit/property | `go test -race ./internal/agent/` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `github.com/google/uuid` v1.6.0 added as direct dep (A6) — `go get`
- [ ] `pgregory.net/rapid` v1.3.0 added (property-based, D-21)
- [ ] `.golangci.yml` installed + configured (SPEC L123 / CLAUDE.md Gate 2; RESEARCH finding #3)
- [ ] `goleak.VerifyTestMain(m)` wired in `internal/agent/workflow/workflow_test.go` (SC#1, D-23)

*goleak + errgroup already present in go.mod — no install needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `aura agent dry-run --request-id auto` UUIDv7 on every Event (SC#4) | INFRA-03 | operator CLI smoke | run command, confirm every JSON Event line carries an OTel-shaped `request_id` |

*All other phase behaviors have automated verification (smoke script + go test).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 40s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
