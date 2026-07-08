---
phase: 37B
slug: web-artifact-sidebar
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-08
---

# Phase 37B — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> **Source of truth for the detailed strategy:** `37B-RESEARCH.md` → `## Validation Architecture`.
> The Per-Task Verification Map below is materialized after planning (Wave 0 / `/gsd-add-tests`)
> once plan task IDs exist.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | vitest + React Testing Library (web unit); Playwright (web e2e) — confirm exact versions from `web/package.json` in Wave 0 |
| **Config file** | `web/vitest.config.*` + `web/playwright.config.*` (confirm paths in Wave 0) |
| **Quick run command** | `{quick web unit command — confirm in Wave 0}` |
| **Full suite command** | `{full web unit + e2e command — confirm in Wave 0}` |
| **Estimated runtime** | ~{N} seconds |

---

## Sampling Rate

- **After every task commit:** Run the quick web unit command
- **After every plan wave:** Run the full suite command
- **Before `/gsd-verify-work`:** Full suite must be green + web coverage ≥85% (WEBART-08)
- **Max feedback latency:** {N} seconds

---

## Per-Task Verification Map

*(Materialized post-planning — one row per plan task, mapping to WEBART-05..08 and the RESEARCH.md
`## Validation Architecture` requirement→test map + STRIDE/ASVS threat refs.)*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 37B-01-01 | 01 | 1 | WEBART-05 | T-37B-01 / — | {expected secure behavior} | unit | `{command}` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Confirm `web/package.json` test stack (vitest/RTL + Playwright) + exact quick/full commands
- [ ] Confirm web coverage-gate config + how the ≥85% floor is measured/enforced
- [ ] Test stubs for WEBART-05..08 per RESEARCH.md `## Validation Architecture`
- [ ] Mock `docx-preview` / `xlsx` so lazy renderer chunks do not drag the coverage aggregate (per research)

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Visual parity vs Claude.ai reference screenshot (panel layout, row labels, toggle) | WEBART-05 | Pixel/visual judgment vs reference | Open a thread with delivered artifacts; compare panel against `D:\Immagini\Screenshots\Screenshot 2026-07-08 104850.png` |
| Mobile drawer parity (portal/backdrop/focus-trap/Esc/swipe) below `lg` | WEBART-05 | Cross-viewport interaction feel | Narrow viewport < lg; open Artefatti drawer; verify parity with the nav drawer |

*If none: "All phase behaviors have automated verification."*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < {N}s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
