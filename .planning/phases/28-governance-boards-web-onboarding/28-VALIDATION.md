---
phase: 28
slug: governance-boards-web-onboarding
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-20
---

# Phase 28 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source: `28-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (backend) + vitest (web) + Playwright (e2e) + Stryker (web mutation) |
| **Config file** | `Makefile` (Go gates) · `web/vitest.config.ts` · `web/stryker.conf.json` · `web/playwright.config.ts` |
| **Quick run command** | `go test ./internal/agui/... ./internal/onboarding/... ./internal/identity/... ./internal/webauth/...` + `cd web && npm run test` |
| **Full suite command** | `make quality-full` + `cd web && npm run test:coverage && npm run test:mutation && npm run test:e2e && node scripts/contrast-check.mjs` |
| **Estimated runtime** | ~TBD seconds (planner/Wave 0 to measure) |

---

## Sampling Rate

- **After every task commit:** Run `{quick run command}`
- **After every plan wave:** Run `{full suite command}`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** TBD seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 28-01-01 | 01 | 0 | TBD | T-28-XX / — | {planner fills from RESEARCH § Validation Architecture} | unit | `{command}` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*
*Planner populates this map from `28-RESEARCH.md` § Validation Architecture (REST handlers · no-escalation validator · cross-store saga + per-leg compensation failure-injection · live MCP probe mock · immutable audit row · onboarding one-prompt-per-step · web vitest/Stryker/Playwright/contrast).*

---

## Wave 0 Requirements

- [ ] Backend gap closures flagged by RESEARCH (e.g. `ListRunsForTask` query, skills pending/archived partition, `Store.ListCapabilities`, `0021_identity_audit` migration) land with their test stubs
- [ ] `web/src/governance/` + `web/src/onboarding/` vitest + Stryker config covers the new dirs (≥85% / ≥70%)
- [ ] Playwright specs + `scripts/contrast-check.mjs` targets registered for the new board + wizard screens

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Scannable Telegram QR links a live channel end-to-end | ONBD-01b | Requires a real Telegram client scanning the rendered QR against the live bot | Operator scans the wizard QR with a phone, sends `/start`, confirms the channel links once and a replay/expired token is rejected |

*Planner refines; automate every behavior that has a testable seam.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < TBDs
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
