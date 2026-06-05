---
phase: 11-skills
plan: 04
subsystem: skills
tags: [skills, slice-7c, migration-0010, append-only-audit, d29-coherence, writer, materialize, content-hash, scoring-gate]

requires:
  - phase: 11-02
    provides: "internal/skills package + Frontmatter/TypeInstruction|Snippet + loader (active-root scan) + builtin materialization idiom"
  - phase: 11-03
    provides: "ValidateForWrite (pure write-boundary primitive, allowBlocklisted override) + SanitizeName chokepoint + AURA_SKILL_INJECTION_BLOCKLIST config"
  - phase: 11-01
    provides: "Amended prd.md §Slice 7 truth-source (D-16 skill_ttl_sweep TaskKind, D-19 sidecar-not-ALTER, D-29 audit matrix, D-23 content_hash recovery, D-17 active-only materialize)"
  - phase: 10-09
    provides: "0009 scheduler_tasks.kind CHECK (the A2-landmine ALTER target) + the audit-forever role-separation precedent (agent_job_runs)"
  - phase: 08
    provides: "scoring.ComputeSkillTier/GateRecommended (shipped unwired; Phase 11 is the designated consumer)"
  - phase: 04
    provides: "canonical Store{pool,q} pattern (identity/askuser) + db.WithTx atomic-write seam + conversations ScanOrphans Lstat-no-follow idiom"
provides:
  - "Migration 0010_skill_audit: append-only aura.skill_audit (BEFORE UPDATE/DELETE row trigger + BEFORE TRUNCATE statement trigger + aura_app SELECT/INSERT-only grant) + D-29 coherence CHECK (5-row matrix as a 4-tuple disjunction) + scheduler_tasks.kind CHECK widened for skill_ttl_sweep (A2 landmine)"
  - "internal/skills/audit_store.go: canonical INSERT+SELECT-only AuditStore, SQLSTATE classification (errors.As+pgErr.Code) -> ErrAuditImmutable (42501) / ErrAuditIncoherent (23514); InsertAuditTx for the writer's WithTx closure"
  - "internal/skills/writer.go + writer_activate.go: scoring-gated WriteMutation (pending FS write + atomic D-29 pending audit), Activate/Archive/Delete with the approved/system audit tuples + content_hash on every row"
  - "internal/skills/materialize.go: active-only export-dir materialization (the /skills ro-mount source), Lstat-no-follow symlink strip, Dematerialize on archive/delete (D-17)"
  - "internal/skills/contenthash.go: canonical byte-sorted (relPath,bytes) sha256 (D-15/D-23), shared writer+installer helper"
  - "internal/db/migrate_steps.go: MigrateSteps reversibility seam for schema round-trip tests"
affects: [11-05, 11-06, 11-07, "skills-governance", "skill-install", "skill-snippets", "ask-user-resume-handler"]

tech-stack:
  added: []
  patterns:
    - "Append-only audit table = belt-and-suspenders: role grant withholds UPDATE/DELETE/TRUNCATE AND a row trigger (UPDATE/DELETE) AND a SEPARATE statement trigger (TRUNCATE — a row trigger never fires for TRUNCATE, Pitfall 1)"
    - "D-29 coherence CHECK as a disjunction of the four distinct allowed tuples (approve+reject share a shape); SQLSTATE 23514 -> ErrAuditIncoherent"
    - "FS-write-before-tx atomicity (mirrors conversations sidecar-spill): the pending skill lands on disk BEFORE the audit INSERT tx; a crash leaves a reconcilable orphan, the tx is the audit row (all-or-nothing)"
    - "WriteMutation NEVER activates (T-11-04-E1): Activate is a SEPARATE method only the resume handler / CLI call — structural self-approval block"
    - "Symlink-strip via manual os.ReadDir+Lstat recursion (NOT filepath.WalkDir) — keeps gosec G122/G703 clean and never descends a symlinked dir"

key-files:
  created:
    - internal/db/migrations/0010_skill_audit.up.sql
    - internal/db/migrations/0010_skill_audit.down.sql
    - internal/db/queries/skill_audit.sql
    - internal/db/migrate_steps.go
    - internal/skills/audit_store.go
    - internal/skills/audit_store_integration_test.go
    - internal/skills/writer.go
    - internal/skills/writer_activate.go
    - internal/skills/writer_test.go
    - internal/skills/materialize.go
    - internal/skills/materialize_test.go
    - internal/skills/contenthash.go
  modified:
    - internal/db/sqlc/models.go
    - internal/db/sqlc/querier.go
    - internal/db/sqlc/skill_audit.sql.go

key-decisions:
  - "The 0009 kind CHECK is auto-named scheduler_tasks_kind_check (verified live via pg_constraint) — the ALTER drops + re-adds it by that name; the down migration restores the original four-kind list"
  - "The D-29 matrix's approve+reject rows share the same column shape (ask_user + NOT NULL token + true + true), so the CHECK encodes FOUR distinct tuples, not five clauses"
  - "AuditStore is INSERT+SELECT-only by construction (no Update/Delete method) — the append-only contract is enforced at the DB and mirrored in the Go surface"
  - "content_hash is the canonical byte-sorted (relPath,bytes) sha256 (D-15) with a length-prefixed encoding that is collision-resistant across path/content boundaries; the writer hashes a single-file SKILL.md, the installer (11-06) will hash a cloned tree via the SAME HashSkillDir"
  - "db.MigrateSteps added (mirrors Reset) so the schema round-trip test can step down -1 and back up — a reusable reversibility seam, not a test-only hack"

patterns-established:
  - "Append-only DB ledger: SELECT+INSERT grant + UPDATE/DELETE row trigger + TRUNCATE statement trigger + SQLSTATE-classified sentinels (the audit-forever pattern beyond 0009's grant-only posture)"
  - "Gate-aware write primitive: scoring.ComputeSkillTier+GateRecommended decide pending vs activate; the writer never self-activates"

requirements-completed: [CAP-07]

duration: ~45min
completed: 2026-06-05
---

# Phase 11 Plan 04: Skills Writer / Audit / Materialize (Slice 7c core) Summary

**The write/audit/materialize backbone: migration 0010 ships the append-only `aura.skill_audit` (BEFORE UPDATE/DELETE row trigger + BEFORE TRUNCATE statement trigger + `aura_app` SELECT/INSERT-only grant — Pitfall #6 belt-and-suspenders) with the D-29 coherence CHECK and the A2-landmine `skill_ttl_sweep` kind-CHECK ALTER; the canonical INSERT-only audit store classifies via SQLSTATE; the scoring-gated Writer writes pending→active atomically with the D-29 audit INSERT in one `db.WithTx`, exposes Activate/Archive/Delete, and materializes only ACTIVE skills to the export dir with a Lstat-no-follow symlink strip.**

## Performance

- **Duration:** ~45 min
- **Completed:** 2026-06-05
- **Tasks:** 2 (both autonomous)
- **Files:** 15 (12 created, 3 sqlc-regenerated)

## Accomplishments

- **Migration 0010 (`0010_skill_audit.up/down.sql`)** — `aura.skill_audit` is APPEND-ONLY three ways (T-11-04-R1): `aura_app` holds SELECT+INSERT ONLY (no UPDATE/DELETE/TRUNCATE), a `BEFORE UPDATE OR DELETE ... FOR EACH ROW` trigger raises `insufficient_privilege`, and a SEPARATE `BEFORE TRUNCATE ... FOR EACH STATEMENT` trigger raises (Pitfall 1 — a row trigger never fires for TRUNCATE). The `skill_audit_d29_coherence` CHECK constrains `(approval_source, paused_state_token, gate_recommended, gate_taken)` to the four distinct allowed tuples (pending / ask_user / cli / system). `content_hash` is NOT NULL on every row (D-23). The 0009 `scheduler_tasks_kind_check` is dropped + re-added to admit `skill_ttl_sweep` (the A2 landmine, D-16). Plain non-CONCURRENT indexes on a fresh table (Pitfall 6).
- **Audit store (`audit_store.go`)** — canonical `AuditStore{pool,q}` (identity/askuser lineage), INSERT+SELECT only, SQLSTATE classification via `errors.As(&pgErr)+pgErr.Code` (never message-match): 42501 → `ErrAuditImmutable`, 23514 → `ErrAuditIncoherent`. `InsertAudit` (pool) + `InsertAuditTx` (the writer's WithTx closure) + `List` (CLI `--skill`/`--since` filter). Action + approval-source enums mirror the DB CHECKs.
- **Writer (`writer.go` + `writer_activate.go`)** — `WriteMutation` computes the tier via `scoring.ComputeSkillTier` and the gate via `scoring.GateRecommended` (create/update/install→Risky, delete→Destructive — not a hand-rolled tier), validates at the write boundary (`ValidateForWrite`, `allowBlocklisted=false` — model paths NEVER bypass the blocklist, T-11-03-E1), computes the `content_hash`, writes `pending/<name>/SKILL.md` atomically (temp dir + rename) BEFORE the tx, then records the D-29 pending tuple `(NULL,NULL,true,false)` inside `db.WithTx`. `Activate` (the resume/CLI half — SEPARATE method, T-11-04-E1) moves pending→active, materializes, and records the `ask_user`/`cli` approved tuple; `Archive`/`Delete` de-materialize + record the archive/delete rows.
- **Materialize (`materialize.go`)** — copies an ACTIVE skill's files into `AURA_SKILL_EXPORT_DIR` (the `/skills` ro-mount source, D-17), STRIPPING symlinks via Lstat-no-follow (Pitfall 4 / T-11-04-T1) so a malicious skill cannot project a host path into the sandbox mount; `Dematerialize` removes the subtree on archive/delete. ONLY active skills materialize — the mount tracks loader state in lockstep.
- **Content hash (`contenthash.go`)** — `HashSkillFiles`/`HashSkillDir`: the canonical byte-sorted `(relPath, bytes)` sha256 (D-15/D-23) with a length-prefixed encoding that is collision-resistant across path/content boundaries. The writer and the 11-06 installer share this ONE helper.
- **`db.MigrateSteps`** — a reversibility seam (mirrors `Reset`) so `TestMigration0010_SchemaRoundTrip` can step down −1 and back up.

## Task Commits

1. **Task 1: migration 0010 append-only audit + sqlc + audit store** — `7b0d5d42` (feat)
2. **Task 2: writer (scoring-gated pending→active + atomic audit) + materialize** — `c50bc631` (feat)

## Decisions Made

- **The 0009 kind CHECK name is `scheduler_tasks_kind_check`** — verified live against `pg_constraint` rather than guessed; the ALTER targets it by that exact auto-generated name and the down migration restores the original four-kind list.
- **D-29 is a four-tuple disjunction, not five** — the approve and reject events share the same `(ask_user, NOT NULL token, true, true)` column shape, so the CHECK encodes four distinct allowed tuples. The semantic difference (approved vs rejected) lives in the resume answer, not the audit tuple — correct per the matrix.
- **`MigrateSteps` is a real db-package helper, not a test hack** — it mirrors `Reset`/`Migrate` and is the legitimate reversibility primitive any schema round-trip test reuses.
- **Manual os.ReadDir+Lstat recursion over filepath.WalkDir** in materialize + HashSkillDir — keeps gosec G122 (WalkDir-callback TOCTOU) and G703 clean, never descends a symlinked dir, and matches the package's loader/orphan_scan idiom.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] gosec G122/G703 on the WalkDir-based materialize + hash**
- **Found during:** Task 2 (golangci-lint Gate-2 pass).
- **Issue:** `internal/skills` is NEW linted production code; gosec flagged G122 (filesystem op in a `filepath.WalkDir` callback = race-prone path / symlink-TOCTOU) and G703 (path traversal via taint) in the first `materialize.go`/`contenthash.go` drafts, which used `filepath.WalkDir` + `os.ReadFile`.
- **Fix:** rewrote both walks as a manual `os.ReadDir` + per-entry `os.Lstat` recursion (the loader/orphan_scan idiom) that skips symlinked entries up front and never descends them, plus justified `#nosec G304` (Lstat-confirmed regular file under a trusted root). This is strictly MORE secure than the WalkDir form (a symlinked directory is never even entered) and is the package's established pattern.
- **Files modified:** internal/skills/materialize.go, internal/skills/contenthash.go.
- **Verification:** `golangci-lint run ./internal/skills/... ./internal/db/...` → 0 issues; the symlink-strip test still PASSES on Linux.
- **Committed in:** `c50bc631` (Task 2).

**2. [Rule 3 - Blocking] missing `path/filepath` import in the integration test**
- **Found during:** Task 2 (first db_integration build after adding the writer tests).
- **Issue:** the new writer integration tests reference `filepath.Join`; the import was absent (the file previously only used `os`).
- **Fix:** added `path/filepath` (and `internal/scoring`) to the integration test imports.
- **Committed in:** `c50bc631` (Task 2).

**Total deviations:** 2 auto-fixed (both blocking, both Rule 3). No scope creep — the write/audit/materialize behavior is exactly as planned.

## Verification Evidence

- **db_integration (live stack, RAN — non-trivial runtime, NOT skipped):**
  - `go test -tags db_integration -run 'TestAuditImmutable|TestInstallAuditRow|TestAuditCoherence|TestMigration0010' ./internal/skills/` → **PASS** (0.72s). SC#1 INSERT round-trip; SC#2 UPDATE/DELETE/TRUNCATE all denied as `aura_app` + the row survives; D-29 CHECK rejects the incoherent `ask_user`+NULL-token tuple and accepts all four coherent shapes; 0010 down −1 + re-up clean.
  - `go test -tags db_integration -run 'TestWriterPendingAuditRow|TestWriterActivateAuditRow' ./internal/skills/` → **PASS** (0.63s). The pending audit row carries the `(NULL,NULL,true,false)` D-29 tuple + a non-empty content_hash; Activate materializes into the export dir and records the `ask_user` approved tuple (token NOT NULL).
  - Full tagged suite `go test -tags db_integration ./internal/skills/` → **PASS** (1.12s).
- **Unit (race, WSL):** `go test -race ./internal/skills/` → **PASS** (1.5s). `TestMaterializeStripsSymlink` PASSES on Linux (a symlink does NOT appear in the export dir) — the Windows run skips it (symlink creation needs privilege), the WSL/CI run is authoritative.
- **Migration grep-clean:** `BEFORE TRUNCATE` present (×2), `CONCURRENTLY` absent (0), `skill_ttl_sweep` added to the kind CHECK (×2).
- **`go vet ./...` → 0; `go build ./...` → exit 0.**
- **`golangci-lint run ./internal/skills/... ./internal/db/...` → 0 issues.**
- **All touched files ≤600 LOC** (largest production file: audit_store.go 250).
- **No file deletions** in either commit.

## Known Stubs

None goal-blocking for 7c. The `!gate` auto-activation branch in `WriteMutation` is structurally unreachable in v1 (all four gated actions gate) — it is kept for the future D-26 headless `auto` path and is documented as such. The 11-05 ask_user resume HANDLER and the `aura skills` CLI that CALL `Activate`/`Archive`/`Delete` are downstream (interface-first ordering, not stubs): this plan ships the methods + the audit/materialize seam they consume.

## Threat Flags

None — no new security surface beyond the plan's `<threat_model>`. T-11-04-R1 (audit tamper/wipe) is mitigated three ways and SC#2-tested; T-11-04-T1 (symlink escape via materialization) is mitigated by the Lstat-strip and tested (TestMaterializeStripsSymlink, Linux); T-11-04-E1 (self-activation) is structural (Activate is a separate method WriteMutation never calls); T-11-04-T2 (incoherent tuple) is the D-29 CHECK, tested; T-11-04-I1 (partial write) is FS-before-tx + boot-scan reconciliation.

## Next Phase Readiness

- The Writer (`WriteMutation`/`Activate`/`Archive`/`Delete`) + the `AuditStore` + `Materialize`/`Dematerialize` + the `HashSkillDir` content-pin are the contracts 11-05 (ask_user resume → `Activate`), 11-06 (installer → clone + `HashSkillDir` + `WriteMutation`/`Activate`), and 11-07 (snippets → materialized executable files under the `/skills` mount) build on.
- The `skill_ttl_sweep` TaskKind is now admitted by the scheduler kind CHECK — 11-07 wires the cron handler that USES it (D-16).
- The composition-root wiring (loader scan roots ↔ writer pending/active/archive dirs in agreement; the `/skills` compose mount) is downstream (11-05/11-07 + compose), not in this core write-primitive plan.

## Self-Check: PASSED

- FOUND: internal/db/migrations/0010_skill_audit.up.sql, 0010_skill_audit.down.sql, internal/db/queries/skill_audit.sql, internal/db/migrate_steps.go
- FOUND: internal/skills/audit_store.go, audit_store_integration_test.go, writer.go, writer_activate.go, writer_test.go, materialize.go, materialize_test.go, contenthash.go
- FOUND: commit `7b0d5d42` (Task 1)
- FOUND: commit `c50bc631` (Task 2)

---
*Phase: 11-skills*
*Completed: 2026-06-05*
