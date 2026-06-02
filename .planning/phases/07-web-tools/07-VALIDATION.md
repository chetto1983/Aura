---
phase: 7
slug: web-tools
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-02
---

# Phase 7 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution. Derived from `07-RESEARCH.md` §Validation Architecture. The 4 ROADMAP success criteria are the requirement set (CAP-05). SSRF defense (D-24..D-30) is the dominant risk surface — security tests must be deterministic, non-leaky, and fail-closed.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (table-driven + goleak + race) |
| **Config file** | none — uses existing Aura Go toolchain |
| **Quick run command** | `go test ./internal/web/... ./internal/agent/tools/` |
| **Full suite command** | `go test -race -tags 'web_integration' ./internal/web/... ./internal/agent/tools/...` |
| **Estimated runtime** | ~{TBD by planner} seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/web/...`
- **After every plan wave:** Run the full `-race` suite
- **Before `/gsd-verify-work`:** Full suite must be green (incl. SSRF + DNS-rebinding tiers)
- **Max feedback latency:** {TBD by planner} seconds

---

## Per-Task Verification Map

> Derived during planning from RESEARCH §Validation Architecture. Every SSRF/contract decision (D-07..D-30, D-38..D-42) and each of the 4 ROADMAP success criteria maps to an observable signal at a test tier. Planner fills concrete Task IDs + commands.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 07-XX-XX | XX | X | CAP-05 | T-07-XX / — | {fail-closed block reason / contract assertion} | unit/integration/smoke | `{command}` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

> Planner enumerates from RESEARCH. Expected SSRF-critical scaffolds:

- [ ] IP-classification table tests (private/loopback/link-local/ULA/IPv4-mapped-IPv6 `::ffff:0:0/96`/unspecified/multicast/CGNAT)
- [ ] DNS-rebinding fixture (injectable Go resolver returning 1.2.3.4 then 127.0.0.1) — SC#4
- [ ] redirect-to-blocked-target httptest fixture — D-29
- [ ] SearXNG-unavailable fixture (structured `web_search_unavailable`) — D-06
- [ ] readability+markdown golden fixtures for `web_fetch` extraction — SC#2

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live `aura tool web_search` p95 ≤ 2s against running SearXNG | CAP-05 / SC#1 | Needs live SearXNG container + public internet | `scripts/` smoke against Compose stack |

*Planner confirms/expands. Most behaviors should have automated verification.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (esp. SSRF fixtures)
- [ ] No watch-mode flags
- [ ] Feedback latency < {N}s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
