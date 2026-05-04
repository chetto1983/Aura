# v1.0 Production Readiness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the approved v1.0 design into a production-readiness milestone that closes data integrity, migration safety, security, memory reliability, Telegram regression, and release gates.

**Architecture:** Keep explicit SQL and add a small internal DB/migration layer. Break implementation into focused subplans because v1.0 spans independent subsystems. Start with DB Foundation because every migration and security phase depends on one shared SQLite pool.

**Tech Stack:** Go, `database/sql`, `modernc.org/sqlite`, SQLite WAL, React/Vite dashboard, existing debug smoke commands, GoReleaser.

---

## Scope Check

The approved spec covers six independent subsystems. Do not implement all of v1.0 from one giant task list. Use this master plan as the milestone controller, then execute one focused subplan at a time.

Subplans:

1. DB Foundation
2. Migration Safety
3. Memory Reliability
4. Dashboard Security
5. Telegram Regression Harness
6. Release Gate

The current `.planning/phases/01-centralize-sqlite-db/PLAN.md` already covers much of DB Foundation. Before coding, reconcile it with this v1.0 boundary so it does not drag deferred v1.1 cleanup into v1.0.

## File Structure

Milestone planning files:

- Modify: `.planning/REQUIREMENTS.md` - replace the historical broad Close Concern requirement list with the approved v1.0 production blocker requirements and v1.1 deferrals.

- Modify: `.planning/PROJECT.md` — rename/clarify v1.0 as Production Readiness and list deferred v1.1 items.
- Modify: `.planning/MILESTONES.md` — set v1.0 scope to production blockers and record v1.1 Hardening Polish as the follow-up milestone.
- Modify: `.planning/ROADMAP.md` — replace the historical broad six-phase Close Concern roadmap with the approved six production-readiness phases.
- Modify: `.planning/STATE.md` — point current work to DB Foundation and this production-readiness boundary.
- Modify: `.planning/codebase/CONCERNS.md` — add the two missing production blockers found during brainstorming: archive reliability and settings secret redaction.
- Modify: `.planning/phases/01-centralize-sqlite-db/PLAN.md` — keep DB Foundation focused on the shared pool and production SQLite open paths.
- Modify: `.planning/phases/01-centralize-sqlite-db/01-VALIDATION.md` — align validation with DB Foundation only.

Implementation files by subplan:

- DB Foundation:
  - Create: `internal/db/db.go`
  - Create: `internal/db/db_test.go`
  - Modify: production store constructors in `internal/auth`, `internal/scheduler`, `internal/settings`, `internal/search`, `internal/swarm`
  - Modify: startup wiring in `cmd/aura/main.go` and `internal/telegram/setup.go`
- Migration Safety:
  - Create: `internal/db/migrations/`
  - Modify: store schema ownership so migrations run at startup, not lazily from unrelated stores
- Memory Reliability:
  - Modify: `internal/telegram/conversation.go`
  - Modify or add tests in `internal/telegram`
- Dashboard Security:
  - Modify: `internal/auth`
  - Modify: `internal/api/settings*.go`
  - Modify: `web/src` settings surfaces if they display secret values
- Telegram Regression Harness:
  - Add focused tests under `internal/telegram`
- Release Gate:
  - Modify CI/release docs or workflow only if the current release path does not run required gates

## Task 1: Reconcile Planning Artifacts

**Files:**
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `.planning/PROJECT.md`
- Modify: `.planning/MILESTONES.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/STATE.md`
- Modify: `.planning/codebase/CONCERNS.md`

- [ ] **Step 1: Update milestone name and boundary**

  In `.planning/PROJECT.md` and `.planning/MILESTONES.md`, change the active milestone from historical broad "Close Concern" wording to:

  ```markdown
  ## v1.0 — Production Readiness

  Goal: Make Aura safe to run as the daily production build by closing data-integrity, migration-safety, dashboard-security, memory-reliability, Telegram-regression, and release-gate blockers.

  In scope:
  - shared SQLite pool with WAL, busy_timeout, and foreign_keys
  - versioned migrations and upgrade safety
  - observable conversation archive failures
  - dashboard token expiry
  - settings secret redaction
  - focused Telegram critical-path tests
  - final production release gates

  Deferred to v1.1 Hardening Polish:
  - MustResolveProfiles panic fix unless future evidence proves production/user-controlled reachability before v1.0
  - file tool split
  - broad large-file refactors
  - tray coverage polish
  - telebot beta monitoring docs
  - full settings at-rest encryption unless redaction proves insufficient
  - arbitrary coverage targets outside Telegram critical paths
  ```

- [ ] **Step 2: Rewrite active requirements**

  In `.planning/REQUIREMENTS.md`, replace any historical broad Close Concern or all-tiers requirement list with only the approved v1.0 production blockers:

  ```markdown
  - DB Foundation/shared SQLite/PRAGMAs
  - Migration Safety/versioned migrations/upgrade idempotence
  - Memory Reliability/archive failure observability
  - Dashboard Security/token expiry
  - Dashboard Security/settings secret redaction
  - Telegram Regression Harness focused critical-path tests
  - Release Gate automated/manual checks
  ```

  Mark these as deferred to v1.1 Hardening Polish:

  ```markdown
  - MustResolveProfiles bare panic cleanup unless future evidence proves production/user-controlled reachability before v1.0
  - file tool split
  - broad large-file refactors
  - tray coverage polish
  - telebot beta monitoring docs
  - full settings at-rest encryption unless redaction proves insufficient
  - arbitrary coverage targets outside Telegram critical paths
  ```

- [ ] **Step 3: Add missing concerns**

  In `.planning/codebase/CONCERNS.md`, add two production blockers:

  ```markdown
  ## Missing Production Blockers Found During v1.0 Design

  **Conversation archive failures can be silent:**
  - Issue: archive append failures in the Telegram conversation path are not surfaced strongly enough for a memory-first product.
  - Impact: Aura can appear to answer correctly while losing durable conversation evidence.
  - Fix approach: make archive append failures observable through logging and focused tests.

  **Settings API can expose secrets:**
  - Issue: dashboard/settings responses can return raw API key values to any holder of a valid dashboard token.
  - Impact: a stolen dashboard token can exfiltrate LLM, OCR, or embedding credentials.
  - Fix approach: redact secret settings in API responses and UI state; keep write/test-connection paths working.
  ```

- [ ] **Step 4: Replace roadmap phases**

  In `.planning/ROADMAP.md`, use this phase list:

  ```markdown
  ## Phases

  ### Phase 1: DB Foundation
  One shared SQLite pool, production PRAGMAs, and no independent production DB opens.

  ### Phase 2: Migration Safety
  Versioned migrations, fresh/upgrade schema convergence, and idempotent startup.

  ### Phase 3: Memory Reliability
  Observable archive failures and critical memory-write tests.

  ### Phase 4: Dashboard Security
  Token expiry and settings secret redaction.

  ### Phase 5: Telegram Regression Harness
  Focused tests for conversation, streaming, document/OCR trigger, auth, and archive behavior.

  ### Phase 6: Release Gate
  Automated Go/web/sandbox/package checks plus manual Windows production smoke.
  ```

- [ ] **Step 5: Update current state**

  In `.planning/STATE.md`, set:

  ```markdown
  Phase: 1 of 6 (DB Foundation)
  Plan: Phase 1 — DB Foundation
  Status: v1.0 production-readiness design approved; planning artifacts reconciled; ready for DB Foundation execution
  Current focus: shared SQLite pool with WAL, busy_timeout, foreign_keys, and production store constructor injection
  ```

- [ ] **Step 6: Verify planning references**

  Run:

  ```powershell
  rg -n "Close Concern|selected all tiers|10/10 requirements|55%\\+|telebot beta|tray coverage|tools/files.go|MustResolveProfiles" .planning docs/superpowers/specs/2026-05-04-v1-production-readiness-design.md docs/superpowers/plans/2026-05-04-v1-production-readiness-plan.md
  ```

  Expected:

  - Active v1.0 scope lists only production-readiness blockers.
  - `Close Concern` appears only in historical notes, if at all.
  - Deferred items appear under v1.1/deferred scope, not v1.0 blockers.
  - `MustResolveProfiles` appears only in concern-audit entries or v1.1 deferrals with the production/user-controlled reachability caveat.

- [ ] **Step 7: Commit planning reconciliation**

  ```powershell
  git add .planning/REQUIREMENTS.md .planning/PROJECT.md .planning/MILESTONES.md .planning/ROADMAP.md .planning/STATE.md .planning/codebase/CONCERNS.md
  git commit -m "planning: align requirements with v1 scope"
  ```

## Task 2: Patch Phase 1 DB Foundation Plan

**Files:**
- Modify: `.planning/phases/01-centralize-sqlite-db/PLAN.md`
- Modify: `.planning/phases/01-centralize-sqlite-db/01-VALIDATION.md`

- [ ] **Step 1: Rename Phase 1 wording**

  Change "Centralize SQLite DB" to "DB Foundation" where the phase title is used. Keep `FIX-02` mapping.

- [ ] **Step 2: Clarify exact DB surfaces**

  Ensure the plan explicitly includes:

  ```markdown
  Production DB surfaces in scope:
  - `cmd/aura/main.go`
  - `internal/telegram/setup.go`
  - `internal/auth`
  - `internal/scheduler`
  - `internal/settings`
  - `internal/search` embed cache
  - `internal/search` SQLite FTS searcher
  - `internal/swarm`

  Debug commands and tests may create temp SQLite databases, but production startup must create the Aura database through `internal/db.Open`.
  ```

- [ ] **Step 3: Remove migration-framework work from Phase 1**

  Keep Phase 1 limited to:

  - shared DB pool;
  - PRAGMAs;
  - constructor injection;
  - shutdown ownership;
  - tests proving the open path.

  Move versioned migration runner work to Phase 2.

- [ ] **Step 4: Align validation**

  In `01-VALIDATION.md`, require:

  ```markdown
  - `go test ./internal/db ./internal/auth ./internal/scheduler ./internal/settings ./internal/search ./internal/swarm`
  - `go test ./internal/telegram ./cmd/aura`
  - `go build ./cmd/aura`
  - `rg 'sql\\.Open\\(\"sqlite\"' internal cmd`
  - fresh temp DB reports `journal_mode=wal`, `busy_timeout=5000`, and `foreign_keys=1`
  ```

- [ ] **Step 5: Commit Phase 1 plan patch**

  ```powershell
  git add .planning/phases/01-centralize-sqlite-db/PLAN.md .planning/phases/01-centralize-sqlite-db/01-VALIDATION.md
  git commit -m "planning: align phase 1 with db foundation"
  ```

## Task 3: Execute DB Foundation Subplan

**Files:**
- Create: `internal/db/db.go`
- Create: `internal/db/db_test.go`
- Modify: store constructors and startup wiring listed in Task 2

- [ ] **Step 1: Use the existing Phase 1 plan as the executable checklist**

  Open:

  ```powershell
  Get-Content .planning/phases/01-centralize-sqlite-db/PLAN.md
  ```

  Execute it task by task after Task 2 is committed.

- [ ] **Step 2: Use TDD for `internal/db.Open`**

  Start with tests in `internal/db/db_test.go` for:

  - empty path rejection;
  - WAL mode;
  - busy timeout;
  - foreign keys;
  - simple create/insert/select round trip.

- [ ] **Step 3: Keep commits small**

  Commit in this order:

  ```powershell
  git commit -m "db: add shared sqlite open path"
  git commit -m "db: inject shared pool into stores"
  git commit -m "db: wire shared pool through aura startup"
  git commit -m "db: verify production sqlite ownership"
  ```

- [ ] **Step 4: Run Phase 1 gate**

  Run:

  ```powershell
  go test ./internal/db ./internal/auth ./internal/scheduler ./internal/settings ./internal/search ./internal/swarm
  go test ./internal/telegram ./cmd/aura
  go build ./cmd/aura
  rg 'sql\.Open\("sqlite"' internal cmd
  ```

  Expected:

  - package tests pass;
  - `cmd/aura` builds;
  - only approved temp-test/debug open paths remain outside `internal/db`.

## Task 4: Write Migration Safety Subplan

**Files:**
- Create: `docs/superpowers/plans/2026-05-04-v1-migration-safety-plan.md`

- [ ] **Step 1: Create the subplan after DB Foundation passes**

  The plan must cover:

  - migration table name;
  - ordered migration registration;
  - transaction strategy;
  - fresh database schema creation;
  - v3.0.2 upgrade test;
  - idempotent rerun test;
  - store schema ownership cleanup.

- [ ] **Step 2: Commit the subplan**

  ```powershell
  git add docs/superpowers/plans/2026-05-04-v1-migration-safety-plan.md
  git commit -m "docs: plan v1 migration safety"
  ```

## Task 5: Write Remaining Subplans After Migration Design

**Files:**
- Create: `docs/superpowers/plans/2026-05-04-v1-memory-reliability-plan.md`
- Create: `docs/superpowers/plans/2026-05-04-v1-dashboard-security-plan.md`
- Create: `docs/superpowers/plans/2026-05-04-v1-telegram-regression-plan.md`
- Create: `docs/superpowers/plans/2026-05-04-v1-release-gate-plan.md`

- [ ] **Step 1: Write Memory Reliability plan**

  Include exact tests and code paths for archive append failure observability in `internal/telegram/conversation.go`.

- [ ] **Step 2: Write Dashboard Security plan**

  Include token expiry schema/API behavior and settings secret redaction behavior.

- [ ] **Step 3: Write Telegram Regression plan**

  Include focused tests for conversation, streaming, document/OCR trigger, auth, and archive behavior. Do not chase arbitrary package coverage.

- [ ] **Step 4: Write Release Gate plan**

  Include automated Go/web/sandbox checks, migration upgrade checks, archive/package extraction checks, and manual Windows smoke.

- [ ] **Step 5: Commit subplans together only if they are docs-only**

  ```powershell
  git add docs/superpowers/plans/2026-05-04-v1-memory-reliability-plan.md docs/superpowers/plans/2026-05-04-v1-dashboard-security-plan.md docs/superpowers/plans/2026-05-04-v1-telegram-regression-plan.md docs/superpowers/plans/2026-05-04-v1-release-gate-plan.md
  git commit -m "docs: plan remaining v1 production gates"
  ```

## Task 6: Final Milestone Completion Gate

**Files:**
- Modify: `.planning/MILESTONES.md`
- Modify: `.planning/PROJECT.md`
- Modify: `.planning/STATE.md`
- Modify: `docs/implementation-tracker.md`

- [ ] **Step 1: Run automated gate**

  ```powershell
  go fmt ./...
  go test ./...
  go vet ./...
  go build ./...
  npm --prefix web ci
  npm --prefix web run lint
  npm --prefix web run build
  go run ./cmd/debug_sandbox --smoke
  go run ./cmd/debug_sandbox --tool-smoke
  go run ./cmd/debug_sandbox --artifact-smoke
  ```

- [ ] **Step 2: Run release candidate gate**

  ```powershell
  go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
  ```

- [ ] **Step 3: Run manual Windows smoke**

  Verify:

  - clean unzip;
  - first-run setup;
  - Telegram `/start`;
  - dashboard token/login;
  - PDF ingest/OCR/wiki path;
  - reminder fires;
  - `execute_code` returns CSV/PNG artifacts;
  - tray opens dashboard.

- [ ] **Step 4: Mark milestone complete**

  Update `.planning/MILESTONES.md`, `.planning/PROJECT.md`, `.planning/STATE.md`, and `docs/implementation-tracker.md` with:

  ```markdown
  v1.0 Production Readiness: complete.
  Release gates passed:
  - automated Go/web/sandbox checks
  - migration fresh/upgrade/idempotence checks
  - release candidate package check
  - manual Windows smoke
  ```

- [ ] **Step 5: Commit milestone completion**

  ```powershell
  git add .planning/MILESTONES.md .planning/PROJECT.md .planning/STATE.md docs/implementation-tracker.md
  git commit -m "docs: complete v1 production readiness"
  ```

## Execution Recommendation

Use subagent-driven execution only after Task 2 is committed. DB Foundation has enough shared-state risk that each worker should own a disjoint slice:

- Worker 1: `internal/db`
- Worker 2: store constructor injection
- Worker 3: startup/shutdown wiring
- Worker 4: verification and residual `sql.Open` scan

Do not run multiple workers against the same file set.
