---
phase: 18
slug: slice-7e-executable-snippet-reuse-steady-state-artifact-runs
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-06
---

# Phase 18 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + goleak + build-tag tiers (`db_integration`, `sandbox_integration`, `live_e2e`, `cot_eval`) |
| **Config file** | Makefile (quality/quality-full gates) |
| **Quick run command** | `go vet ./... && go build ./... && go test -race ./internal/skills/... ./internal/agent/tools/... ./internal/toolinvocations/...` |
| **Full suite command** | WSL: `make quality-full` (stack up — vet+build+lint+race+coverage≥85%+integration+mutation) |
| **Estimated runtime** | ~60 seconds quick / ~12 min full |

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test -race ./internal/<touched-package>/`
- **After every plan wave:** full tag-matrix for touched packages (`-tags 'db_integration sandbox_integration'`, composed DSNs from POSTGRES_PASSWORD)
- **Before `/gsd-verify-work`:** `make quality-full` green + the chat-surface E2E gate run live (steady-state artifact run timing measured from the tool_invocations ledger, not the reply)
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled by planner) | | | | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] (filled by planner from PLAN.md task map)

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
