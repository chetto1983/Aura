---
phase: 1
slug: centralize-sqlite-db
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-04
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` package |
| **Config file** | none — standard `go test` |
| **Quick run command** | `go test ./internal/db/... -count=1` |
| **Full suite command** | `go test ./... -count=1` |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** `go test ./internal/db/... -count=1`
- **After every plan wave:** `go test ./internal/{db,auth,scheduler,settings,swarm,search}/... -count=1`
- **Before `/gsd-verify-work`:** Full `go test ./... -count=1` must be green
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 1-01-01 | 01 | 1 | FIX-02-WAL | — | WAL journal mode active after db.Open | unit | `go test ./internal/db/ -run TestOpen_WALMode -count=1` | ❌ W0 | ⬜ pending |
| 1-01-02 | 01 | 1 | FIX-02-PRAGMA | T-1-01 | All PRAGMAs applied; path traversal rejected | unit | `go test ./internal/db/ -run TestOpen -count=1` | ❌ W0 | ⬜ pending |
| 1-02-01 | 02 | 2 | FIX-02-INJECT | T-1-02 | auth.NewStoreWithDB accepts shared pool | unit | `go test ./internal/auth/ -count=1` | ✅ | ⬜ pending |
| 1-02-02 | 02 | 2 | FIX-02-INJECT | T-1-02 | scheduler.NewStoreWithDB accepts shared pool; no Close() | unit | `go test ./internal/scheduler/ -count=1` | ✅ | ⬜ pending |
| 1-02-03 | 02 | 2 | FIX-02-INJECT | T-1-02 | settings.NewStoreWithDB accepts shared pool | unit | `go test ./internal/settings/ -count=1` | ✅ | ⬜ pending |
| 1-02-04 | 02 | 2 | FIX-02-INJECT | T-1-02 | swarm.NewStoreWithDB no longer calls SetMaxOpenConns(1) | unit | `go test ./internal/swarm/ -count=1` | ✅ | ⬜ pending |
| 1-02-05 | 02 | 2 | FIX-02-INJECT | — | EmbedCache constructable via NewEmbedCacheWithDB | unit | `go test ./internal/search/ -run TestEmbedCache -count=1` | ✅ | ⬜ pending |
| 1-02-06 | 02 | 2 | FIX-02-INJECT | — | sqliteSearcher constructable via newSqliteSearcherWithDB | unit | `go test ./internal/search/ -count=1` | ✅ | ⬜ pending |
| 1-03-01 | 03 | 3 | FIX-02-INJECT | — | NewEngineWithFallback accepts *sql.DB, not dbPath | unit | `go test ./internal/search/ -count=1` | ✅ | ⬜ pending |
| 1-04-01 | 04 | 4 | FIX-02-SINGLE | T-1-02 | main.go opens pool once; defer close | build | `go build ./cmd/aura/` | ✅ | ⬜ pending |
| 1-04-02 | 04 | 4 | FIX-02-CLOSE | T-1-02 | telegram.New accepts *sql.DB; no internal sql.Open | build | `go build ./internal/telegram/` | ✅ | ⬜ pending |
| 1-04-03 | 04 | 4 | FIX-02-CLOSE | T-1-02 | Bot.Stop() no longer calls schedDB.Close() or authDB.Close() | build | `go build ./internal/telegram/` | ✅ | ⬜ pending |
| 1-05-xx | 05 | 5 | FIX-02-INJECT | — | All test files migrated; go test passes per package | unit | `go test ./internal/search/ ./internal/scheduler/ ./internal/auth/ ./internal/settings/ ./internal/swarm/ ./internal/api/ ./internal/telegram/ ./internal/tools/ ./internal/conversation/ ./cmd/ -count=1` | ✅ | ⬜ pending |
| 1-06-01 | 06 | 6 | FIX-02-SINGLE | — | Only internal/db has sql.Open("sqlite") | static | `rg 'sql\.Open\("sqlite"' --glob '!internal/db/*'` | N/A | ⬜ pending |
| 1-06-02 | 06 | 6 | FIX-02-DRIVER | — | Only internal/db imports modernc.org/sqlite | static | `rg '_ "modernc.org/sqlite"' --glob '!internal/db/*'` | N/A | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/db/db_test.go` — TestOpen_WALMode, TestOpen_Pragmas, TestOpen_NonexistentPath, TestOpen_EmptyPath, TestOpen_ExistingDB
- [ ] Framework install: none needed — standard `go test`

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| WAL mode persists across restarts | FIX-02-WAL | Requires process restart | Open DB, verify WAL, close, reopen, verify WAL still active |
| No double-close panic on shutdown | FIX-02-CLOSE | Requires full process lifecycle | Run aura, send SIGINT, verify clean shutdown in logs |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
