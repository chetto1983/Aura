---
phase: 11
slug: skills
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-05
---

# Phase 11 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + build-tag tiers (`db_integration`, `sandbox_integration`) |
| **Config file** | Makefile (quality/quality-full gates) |
| **Quick run command** | `go vet ./... && go build ./... && go test -race ./internal/skills/...` |
| **Full suite command** | `make quality-full` (WSL, stack up — includes coverage gate ≥85%) |
| **Estimated runtime** | ~60 seconds quick / ~10 min full |

---

## Sampling Rate

- **After every task commit:** Run `go vet ./... && go build ./... && go test -race ./internal/<touched-package>/`
- **After every plan wave:** Run full tag-matrix tests for touched packages (`-tags 'db_integration sandbox_integration'` with composed DSNs)
- **Before `/gsd-verify-work`:** Full suite must be green (`make quality-full`)
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled by planner from PLAN.md tasks) | | | CAP-07 / CAP-08 | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] (filled by planner — fuzz harness `FuzzSkillValidator` stub, integration-tier skip-helpers honoring no-skip-as-green under `$CI`)

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| (filled by planner) | | | |

*If none: "All phase behaviors have automated verification."*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
