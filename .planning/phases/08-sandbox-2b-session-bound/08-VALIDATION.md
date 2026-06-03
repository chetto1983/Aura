---
phase: 8
slug: sandbox-2b-session-bound
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-03
---

# Phase 8 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (Go 1.26) + Python stdlib unittest (sidecar) |
| **Config file** | none — build-tag `sandbox_integration` tier; `synctest` for TTL reaper |
| **Quick run command** | `go test ./internal/sandbox/ ./internal/scoring/` |
| **Full suite command** | `go test -race -tags 'sandbox_integration' ./internal/sandbox/ ./internal/scoring/ ./internal/conversations/` |
| **Estimated runtime** | ~60 seconds (unit); integration adds container spin-up |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/<package>/`
- **After every plan wave:** Run the full suite command
- **Before `/gsd-verify-work`:** Full suite (incl. `sandbox_integration` tier) must be green
- **Max feedback latency:** 60 seconds (unit); integration on wave boundary

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD | — | — | CAP-02 | — | populated by planner | — | — | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Confirm `migrations/0008_sandbox_sessions.{up,down}.sql` (NOT 0010 — repo is at 0007) with `conversation_id uuid REFERENCES conversations(id)` (NOT text — landmine #1/#2 from RESEARCH.md)
- [ ] `internal/web` SSRF export-or-extract decision (landmine #5) before egress-proxy plan can reuse `classify`/`guard`/`dnsPin`
- [ ] Live egress-bridge reachability spike (landmine #3, MEDIUM confidence) — session-container seccomp/network posture vs host proxy

*Full Wave 0 gap list lives in 08-RESEARCH.md `## Validation Architecture`.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| TBD | CAP-02 | populated by planner | — |

*The 4 ROADMAP success criteria (session persistence, symlink-escape refusal, TTL reap, network allowlist) map to named test commands in 08-RESEARCH.md `## Validation Architecture`.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
