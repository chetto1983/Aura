# Requirements: Aura v1.0 Production Readiness

**Defined:** 2026-05-04
**Core Value:** Durable, compounding personal memory that grows smarter with every conversation without relying on external note-taking apps.

## Milestone v1.0 Requirements

v1.0 is limited to production blockers: data integrity, upgrade safety, memory reliability, dashboard security, Telegram critical-path regression confidence, and final release readiness.

### Production Blockers

- [x] **DB-01 DB Foundation:** Production startup uses one shared SQLite pool with WAL mode, `busy_timeout`, and foreign key enforcement applied through the approved DB open path.
- [x] **MIG-01 Migration Safety:** Schema changes run through deterministic versioned migrations with transactional application, fresh-install support, upgrade support, and idempotent reruns.
- [x] **MEM-01 Memory Reliability:** Conversation archive failures are observable through logging or returned errors so Aura does not silently lose durable memory.
- [x] **SEC-01 Dashboard Token Expiry:** Dashboard bearer tokens carry expiry metadata, default to a configurable 30-day TTL, and expired tokens are rejected distinctly from invalid tokens.
- [x] **SEC-02 Settings Secret Redaction:** Settings API responses and dashboard state redact LLM, embedding, Mistral, and Ollama secrets while preserving write and test-connection flows.
- [x] **TEST-01 Telegram Regression Harness:** Focused hermetic tests cover critical Telegram paths: conversation handling, streaming edits, document/OCR triggers, access control, and archive behavior.
- [x] **REL-01 Release Gate:** Automated and manual release checks prove Go, web, sandbox, migration, packaging, and Windows smoke readiness before tagging v1.0.

## Milestone v1.1 Requirements

v1.1 is limited to trustworthy daily-use hardening: no avoidable production panics, observable runtime cleanup failures, packaged Windows tray UX, dependency watchpoints, and a focused release gate.

- [ ] **PANIC-01 Toolset Profile Panic Removal:** Invalid or stale toolset profile names must not cause an unstructured process panic in production paths.
- [ ] **OBS-01 Shutdown Close Observability:** Shutdown close failures for long-lived services are logged with enough context to diagnose DB/client close problems.
- [ ] **OBS-02 Tray Browser-Open Observability:** Tray dashboard-open failures are visible to the operator, and invalid dashboard URLs are rejected before shell handoff.
- [ ] **OBS-03 Telegram Cleanup Observability:** Cosmetic Telegram cleanup failures, such as placeholder deletion during streaming, are observable at low severity.
- [ ] **AUDIT-01 Token Audit Update Observability:** Auth token `last_used` write failures are observable without denying an otherwise valid dashboard request.
- [ ] **DEP-01 Telebot Beta Monitoring:** Aura tracks the `gopkg.in/telebot.v4` beta dependency with a pinned-version review checklist and smoke expectations.
- [ ] **UX-01 Packaged Windows Console Suppression:** GoReleaser-produced Windows artifacts build `cmd/aura` as a GUI/tray-first binary without a console window while development and debug commands keep console output.
- [ ] **REL-02 Focused v1.1 Release Gate:** Focused package tests, broad Go verification, Windows GUI-subsystem package inspection, and any required manual smoke pass before tagging v1.1.

## Deferred to v1.1 Hardening Polish

- **FUT-01 MustResolveProfiles panic cleanup:** Deferred unless future evidence proves production/user-controlled reachability before v1.0.
- **FUT-02 File tool split:** Split file-generation tools, including `tools/files.go`, outside the production-readiness gate.
- **FUT-03 Broad large-file refactors:** Defer maintainability-only file splitting and package cleanup.
- **FUT-04 Tray coverage polish:** Defer tray/browser-open coverage beyond any minimal safety fix required by release smoke.
- **FUT-05 Telebot beta monitoring docs:** Defer dependency monitoring notes until after the v1.0 gate.
- **FUT-06 Full settings at-rest encryption:** Defer unless secret redaction proves insufficient for v1.0 security.
- **FUT-07 Arbitrary coverage targets:** Defer package-wide targets outside Telegram critical paths, including 55%+ goals.

## Future Requirements

Deferred items from the concern audit stay in v1.1 Hardening Polish or later unless they become proven production blockers.

<!-- Deferred from CONCERNS.md P3 tier -->
- **FUT-08 Pyodide runtime bundle in Windows release artifact:** Deferred to release packaging work outside this production-readiness requirements list unless the v1.0 release gate exposes a packaging blocker.

## Out of Scope

| Feature | Reason |
|---------|--------|
| New feature development | Hardening-only milestone |
| Replace chromem-go with sqlite-vector | Already evaluated and rejected in slice 11h |
| WebSocket real-time dashboard | Not needed for hardening |
| Mobile app | Web dashboard sufficient |
| Replace telebot v4 | Beta risk is monitored later, not resolved by swapping library |
| Distributed SQLite (Litestream/replication) | Local-first design; not needed |
| Replace SQLite with Postgres | Explicitly rejected in prd.md design principle #3 |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| DB-01 | Phase 1: DB Foundation | Done — merged in PR #1 on 2026-05-05 |
| MIG-01 | Phase 2: Migration Safety | Done — merged in PR #1 on 2026-05-05 |
| MEM-01 | Phase 3: Memory Reliability | Done — direct and buffered archive append failures logged and covered on 2026-05-05 |
| SEC-01 | Phase 4: Dashboard Security | Done — token expiry schema/store/config/middleware landed on 2026-05-05 |
| SEC-02 | Phase 4: Dashboard Security | Done — settings API/UI redacts secret values on reads while preserving writes/tests on 2026-05-05 |
| TEST-01 | Phase 5: Telegram Regression Harness | Done - focused Telegram conversation/archive, streaming edit, document/OCR trigger, and access-control tests landed on 2026-05-05 |
| REL-01 | Phase 6: Release Gate | Done - automated Go/web/sandbox/package checks and manual Windows smoke passed on 2026-05-05 |

**Coverage:**
- v1.0 production-readiness requirements: 7 total
- Complete: 7
- Remaining: 0
- Mapped to phases: 7
- Unmapped: 0

---

*Requirements defined: 2026-05-04*
*Last updated: 2026-05-05 after v1.0 Production Readiness completion*
