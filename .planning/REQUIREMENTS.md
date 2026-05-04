# Requirements: Aura — Close Concern

**Defined:** 2026-05-04
**Core Value:** Durable, compounding personal memory that grows smarter with every conversation — without relying on external note-taking apps.

## Milestone v1.0 Requirements

Requirements for hardening v1.0. Each maps to roadmap phases.

### Fix & Secure

- [ ] **FIX-01**: User does not experience bot crash on invalid toolset profile names — `MustResolveProfiles` returns error instead of bare panic
- [ ] **FIX-02**: Database connections are centrally managed with WAL mode, busy timeout, and foreign key enforcement applied at startup
- [ ] **FIX-03**: Dashboard bearer tokens expire after a configurable TTL (default 30 days) and expired tokens are rejected with a distinct error
- [ ] **FIX-04**: API keys stored in SQLite settings (LLM, Embedding, Mistral, Ollama) are encrypted at rest with AES-256-GCM

### Test Coverage

- [ ] **TEST-01**: `internal/telegram` package has ≥55% unit test coverage covering conversation handler, streaming, document handling, and access control
- [ ] **TEST-02**: `internal/tray` package has basic unit test coverage for Windows and non-Windows paths plus a cross-platform `openBrowser` abstraction

### Refactor

- [ ] **REFACTOR-01**: Scheduler migrations are extracted from `store.go` into `scheduler/migrations.go` reducing the file from 754 to ~400 lines
- [ ] **REFACTOR-02**: Database schema changes use a versioned migration framework with `schema_versions` table, ordered up-migrations, and transactional application
- [ ] **REFACTOR-03**: `tools/files.go` is split into `files_xlsx.go`, `files_docx.go`, `files_pdf.go`, and `files_types.go` with no behavioral change

### Polish

- [ ] **POLISH-01**: Telebot v4 beta dependency risk is documented with a pinned commit hash and monitoring note for stable release

## Future Requirements

None — this milestone's scope is the complete CONCERNS.md audit. Deferred items:

<!-- Deferred from CONCERNS.md P3 tier -->
- **FUT-01**: Pyodide runtime bundle in Windows release artifact (sandbox.pyodide.6) — deferred: release packaging milestone separate from hardening

## Out of Scope

| Feature | Reason |
|---------|--------|
| New feature development | Hardening-only milestone |
| Replace chromem-go with sqlite-vector | Already evaluated and rejected in slice 11h |
| WebSocket real-time dashboard | Not needed for hardening |
| Mobile app | Web dashboard sufficient |
| Replace telebot v4 | Beta risk is documented, not resolved by swapping library |
| Distributed SQLite (Litestream/replication) | Local-first design — not needed |
| Replace SQLite with Postgres | Explicitly rejected in prd.md design principle #3 |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| FIX-01 | Phase 4 | Pending |
| FIX-02 | Phase 1 | Pending |
| FIX-03 | Phase 3 | Pending |
| FIX-04 | Phase 3 | Pending |
| TEST-01 | Phase 5 | Pending |
| TEST-02 | Phase 4 | Pending |
| REFACTOR-01 | Phase 2 | Pending |
| REFACTOR-02 | Phase 2 | Pending |
| REFACTOR-03 | Phase 4 | Pending |
| POLISH-01 | Phase 6 | Pending |

**Coverage:**
- v1 requirements: 10 total
- Mapped to phases: 10
- Unmapped: 0 ✓

---

*Requirements defined: 2026-05-04*
*Last updated: 2026-05-04 after roadmap creation*
