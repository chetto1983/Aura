---
phase: 1
slug: centralize-sqlite-db
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-04
---

# Phase 1 - Validation Strategy

> Per-phase validation contract for DB Foundation execution.

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` package |
| **Config file** | none - standard `go test` |
| **Quick run command** | `go test ./internal/db -count=1` |
| **Full phase command** | `go test ./internal/db ./internal/auth ./internal/scheduler ./internal/settings ./internal/search ./internal/swarm ./internal/telegram ./cmd/aura -count=1` |
| **Estimated runtime** | ~30 seconds |

## Required Verification

- `go test ./internal/db ./internal/auth ./internal/scheduler ./internal/settings ./internal/search ./internal/swarm`
- `go test ./internal/telegram ./cmd/aura`
- `go build ./cmd/aura`
- `rg 'sql\.Open\("sqlite"' internal cmd`
- fresh temp DB reports `journal_mode=wal`, `busy_timeout=5000`, and `foreign_keys=1`

## Sampling Rate

- **After DB factory changes:** `go test ./internal/db -count=1`
- **After constructor injection changes:** `go test ./internal/auth ./internal/scheduler ./internal/settings ./internal/search ./internal/swarm -count=1`
- **After production wiring changes:** `go test ./internal/telegram ./cmd/aura -count=1`
- **Before phase sign-off:** run every command in Required Verification
- **Max feedback latency:** 30 seconds for targeted package checks

## Per-Task Verification Map

| Task ID | Plan Area | Requirement | Test Type | Automated Command | Status |
|---------|-----------|-------------|-----------|-------------------|--------|
| 1-01 | Shared DB pool | `internal/db.Open` applies WAL, busy timeout, and foreign keys | unit | `go test ./internal/db -count=1` | pending |
| 1-02 | Constructor injection | In-scope stores/search components accept shared `*sql.DB` | unit/build | `go test ./internal/auth ./internal/scheduler ./internal/settings ./internal/search ./internal/swarm -count=1` | pending |
| 1-03 | Entrypoint wiring | Production startup opens the Aura DB through `internal/db.Open` | unit/build | `go test ./internal/telegram ./cmd/aura -count=1` | pending |
| 1-04 | Shutdown ownership | Shared pool is closed by `cmd/aura/main.go`, not by stores or bot shutdown | unit/build | `go test ./internal/telegram ./cmd/aura -count=1` | pending |
| 1-05 | Static guard | Production `sql.Open("sqlite"` calls are removed from `internal` and `cmd` except the DB foundation path | static | `rg 'sql\.Open\("sqlite"' internal cmd` | pending |

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Clean shutdown owns the pool once | FIX-02-CLOSE | Requires process lifecycle | Run Aura, stop it, and confirm shutdown logs do not show double-close or store-close failures |

## Phase Boundary

Phase 1 validates the DB Foundation only. Schema versioning and upgrade orchestration validation belongs to Phase 2.

## Validation Sign-Off

- [ ] Required verification commands pass
- [ ] Fresh temp DB PRAGMA assertions pass
- [ ] Static search reviewed for production SQLite opens
- [ ] Phase 2 schema versioning and upgrade orchestration work remains out of Phase 1
- [ ] `nyquist_compliant: true` set in frontmatter after validation is complete

**Approval:** pending
