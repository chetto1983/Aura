# Roadmap: Aura v1.0 — Production Readiness

**Created:** 2026-05-04
**Milestone:** v1.0 Production Readiness
**Total phases:** 6

## Milestone Goal

Make Aura safe to run as the daily production build by closing data-integrity, migration-safety, dashboard-security, memory-reliability, Telegram-regression, and release-gate blockers.

## Boundary

In scope:
- Shared SQLite pool with WAL, busy_timeout, and foreign_keys
- Versioned migrations and upgrade safety
- Observable conversation archive failures
- Dashboard token expiry
- Settings secret redaction
- Focused Telegram critical-path tests
- Final production release gates

Deferred to v1.1 Hardening Polish:
- MustResolveProfiles panic fix unless production reachability promotes it back to a blocker
- File tool split, including `tools/files.go`
- Broad large-file refactors
- tray coverage polish
- telebot beta monitoring docs
- Full settings at-rest encryption unless redaction proves insufficient
- Arbitrary coverage targets outside Telegram critical paths, including 55%+ package-wide goals

## Dependency Graph

```
Phase 1 (DB Foundation)
  └→ Phase 2 (Migration Safety)
       ├→ Phase 4 (Dashboard Security)
       └→ Phase 6 (Release Gate)

Phase 3 (Memory Reliability)
  └→ Phase 5 (Telegram Regression Harness)
       └→ Phase 6 (Release Gate)
```

Critical path: Phase 1 → Phase 2 → Phase 4 → Phase 6, with Phase 3 and Phase 5 feeding the same release gate.

## Phases

### Phase 1: DB Foundation

One shared SQLite pool, production PRAGMAs, and no independent production DB opens.

**Addresses:** FIX-02, SQLite configuration gaps
**Depends on:** —
**Success criteria:**
- Production startup creates the Aura database through a shared DB open path.
- WAL, busy_timeout, and foreign_keys are applied at open time.
- Store constructors accept the shared pool and do not own production DB lifecycle.

### Phase 2: Migration Safety

Versioned migrations, fresh/upgrade schema convergence, and idempotent startup.

**Addresses:** REFACTOR-02, ad-hoc per-store migrations
**Depends on:** Phase 1
**Success criteria:**
- A fresh database and an upgraded database converge to the same schema.
- Startup migrations are idempotent and run before production stores initialize.
- Failed migrations do not leave partially applied schema state.

### Phase 3: Memory Reliability

Observable archive failures and critical memory-write tests.

**Addresses:** MEM-01, conversation archive observability
**Depends on:** —
**Success criteria:**
- Archive append failures in the Telegram conversation path are logged strongly enough to diagnose.
- Focused tests cover successful and failed conversation archive writes.

### Phase 4: Dashboard Security

Token expiry and settings secret redaction.

**Addresses:** FIX-03, SEC-01
**Depends on:** Phase 2
**Success criteria:**
- Dashboard bearer tokens expire according to configured TTL behavior.
- Settings API responses and dashboard state redact secret values while write and test-connection paths keep working.

### Phase 5: Telegram Regression Harness

Focused tests for conversation, streaming, document/OCR trigger, auth, and archive behavior.

**Addresses:** TEST-01 narrowed to Telegram critical paths
**Depends on:** Phase 3
**Success criteria:**
- Critical Telegram paths are covered with hermetic tests.
- Tests avoid real network calls and do not require production Telegram credentials.

### Phase 6: Release Gate

Automated Go/web/sandbox/package checks plus manual Windows production smoke.

**Addresses:** REL-01
**Depends on:** Phases 2, 4, and 5
**Success criteria:**
- Automated Go, web, sandbox, migration, and package checks pass.
- Manual Windows smoke validates first-run setup, Telegram, dashboard login, ingest/OCR/wiki, reminders, sandbox artifacts, and tray dashboard launch.

## Requirement Traceability

| Requirement | Phase | v1.0 Rationale |
|-------------|-------|----------------|
| FIX-02: Multiple SQLite connection pools | Phase 1 | Data-integrity foundation |
| SQLite PRAGMAs: WAL, busy_timeout, foreign_keys | Phase 1 | Production DB reliability |
| REFACTOR-02: Versioned migration framework | Phase 2 | Upgrade safety |
| MEM-01: Conversation archive failures observable | Phase 3 | Memory reliability |
| FIX-03: Dashboard token expiration | Phase 4 | Dashboard security |
| SEC-01: Settings API secret redaction | Phase 4 | Credential exposure prevention |
| TEST-01: Telegram critical-path tests | Phase 5 | Regression confidence |
| REL-01: Production release gate | Phase 6 | Release readiness |

## Deferred Requirement Traceability

| Deferred item | Follow-up milestone | Reason |
|---------------|---------------------|--------|
| MustResolveProfiles panic fix | v1.1 Hardening Polish | High-severity concern, but not part of the approved production-readiness gate unless production reachability is proven |
| File tool split | v1.1 Hardening Polish | Large-file cleanup, not a production blocker |
| Broad large-file refactors | v1.1 Hardening Polish | Useful maintainability work outside v1.0 gate |
| tray coverage polish | v1.1 Hardening Polish | Lower risk than DB, security, memory, and Telegram paths |
| telebot beta monitoring docs | v1.1 Hardening Polish | Dependency monitoring can follow the production gate |
| Full settings at-rest encryption | v1.1 Hardening Polish | Revisit if API redaction is insufficient |
| Arbitrary coverage targets outside Telegram critical paths | v1.1 Hardening Polish | v1.0 only gates critical Telegram behavior |

---

*Roadmap defined: 2026-05-04*
*Last updated: 2026-05-04*
