---
phase: 1
slug: infra-db-knowledge
plan: 02
type: execute
slice: 0.5
title: "Slice 0.5 — Postgres + sqlc + pgx + golang-migrate"
wave: 1
depends_on: []
files_modified:
  - cmd/aura/main.go
  - cmd/aura/db.go
  - internal/config/config.go
  - internal/db/config.go
  - internal/db/db.go
  - internal/db/migrate.go
  - internal/db/ping.go
  - internal/db/status.go
  - internal/db/reset.go
  - internal/db/migrations/0001_init.up.sql
  - internal/db/migrations/0001_init.down.sql
  - internal/db/migrations/0002_knowledge_migrations.up.sql
  - internal/db/migrations/0002_knowledge_migrations.down.sql
  - internal/db/queries/knowledge_migrations.sql
  - internal/db/sqlc/db.go
  - internal/db/sqlc/models.go
  - internal/db/sqlc/querier.go
  - internal/db/sqlc/knowledge_migrations.sql.go
  - internal/db/db_test.go
  - compose.yaml
  - Makefile
  - sqlc.yaml
  - .env.example
  - scripts/restore_drill.sh
  - prd.md
autonomous: true
requirements:
  - INFRA-01
tags:
  - postgres
  - sqlc
  - pgx
  - golang-migrate
  - infra
must_haves:
  truths:
    - "Operator runs `aura db migrate` and observes idempotent migration; re-run prints \"no pending migrations\" and exits 0 (per D-01 + ROADMAP SC#1)"
    - "Operator runs `aura db migrate` connected as `aura_app` role and observes `permission denied`; migration only succeeds as `aura_migrate` (per D-07 + ROADMAP SC#2)"
    - "Operator runs `bash scripts/restore_drill.sh` against a `pg_dump` artifact and observes restore completing in under 90 seconds (per ROADMAP SC#3)"
    - "Operator runs `aura db ping` and observes `ok` + measured latency printed to stdout; exit code 0"
    - "Operator runs `aura db status` and observes the `schema_migrations` table rows tabulated (golang-migrate's own table); exit code 0"
    - "Operator runs `aura db migrate` with `AURA_DB_MIGRATE_URL` empty and observes fail-fast with exact stderr containing `AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17` (per D-07)"
    - "Operator runs `make sqlc && git diff --exit-code internal/db/sqlc/` and observes exit code 0 (sqlc-generated code in sync — CI golden test)"
    - "DSN credentials never appear in error messages or slog output (Pitfall #2 redaction enforced)"
  artifacts:
    - path: "compose.yaml"
      provides: "Postgres compose service at repo root (per D-02)"
      contains: "postgres:17.10-alpine3.23"
    - path: "Makefile"
      provides: "make targets sqlc, db-up, db-migrate, db-status, db-reset, restore-drill"
      contains: "db-migrate:"
    - path: "sqlc.yaml"
      provides: "sqlc v2 config; engine postgresql; output internal/db/sqlc/"
      contains: "version: \"2\""
    - path: ".env.example"
      provides: "Postgres env template — POSTGRES_PASSWORD, AURA_DB_URL, AURA_DB_MIGRATE_URL"
      contains: "AURA_DB_MIGRATE_URL"
    - path: "internal/config/config.go"
      provides: "Root composite Config{DB, RunDir, ToolPreviewCap}"
      contains: "func Load()"
    - path: "internal/db/db.go"
      provides: "pgxpool.Open(ctx, *Config) + redactDSN helper"
      contains: "pgxpool.NewWithConfig"
    - path: "internal/db/migrate.go"
      provides: "golang-migrate runner; embed.FS; idempotent Up()"
      contains: "AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17"
    - path: "internal/db/migrations/0001_init.up.sql"
      provides: "CREATE SCHEMA aura + role grants + default privileges"
      contains: "CREATE SCHEMA IF NOT EXISTS aura"
    - path: "internal/db/migrations/0002_knowledge_migrations.up.sql"
      provides: "Audit table aura.knowledge_migrations (consumed by Slice 0.7)"
      contains: "CREATE TABLE aura.knowledge_migrations"
    - path: "internal/db/queries/knowledge_migrations.sql"
      provides: "sqlc named queries RecordKnowledgeMigration + ListAppliedKnowledgeMigrations"
      contains: "-- name: RecordKnowledgeMigration :exec"
    - path: "internal/db/sqlc/knowledge_migrations.sql.go"
      provides: "Generated sqlc bindings consumed by Slice 0.7 internal/knowledge/migrate.go"
      contains: "func (q *Queries) RecordKnowledgeMigration"
    - path: "scripts/restore_drill.sh"
      provides: "pg_dump → pg_restore harness with <90s assertion"
      contains: "ELAPSED_MS > 90000"
    - path: "internal/db/db_test.go"
      provides: "//go:build db_integration test suite with goleak"
      contains: "goleak.VerifyTestMain"
    - path: "cmd/aura/db.go"
      provides: "runDB(args) inner switch over migrate|ping|status|reset"
      contains: "case \"migrate\":"
  key_links:
    - from: "cmd/aura/main.go"
      to: "cmd/aura/db.go"
      via: "case \"db\": runDB(os.Args[2:])"
      pattern: "case \"db\":"
    - from: "cmd/aura/db.go"
      to: "internal/config.Load"
      via: "godotenv.Load + os.Getenv composite"
      pattern: "config\\.Load"
    - from: "internal/db/migrate.go"
      to: "internal/db/migrations/*.sql"
      via: "//go:embed migrations/*.sql"
      pattern: "//go:embed migrations/\\*\\.sql"
    - from: "sqlc.yaml"
      to: "internal/db/sqlc/"
      via: "make sqlc → sqlc generate"
      pattern: "out: internal/db/sqlc"
    - from: "internal/db/migrations/0002_knowledge_migrations.up.sql"
      to: "internal/db/queries/knowledge_migrations.sql"
      via: "schema → sqlc codegen → generated bindings"
      pattern: "aura\\.knowledge_migrations"
---

<objective>
Stand up Postgres 17-alpine + `pgx/v5` pool + `sqlc` v2 codegen + `golang-migrate/v4` with embedded migrations, all routed through the new `aura db {migrate|ping|status|reset}` CLI surface. Land the `aura.*` schema, the `aura_app`/`aura_migrate` role separation, and the `aura.knowledge_migrations` audit table that Slice 0.7 will consume. Ship the restore-drill harness so ROADMAP Phase 1 SC#3 is measurable.

Purpose: This slice is the persistence cornerstone for every later phase. Every domain table from Phase 4 (`paused_states`, `conversation_turns`), Phase 10 (`agent_job`, `agent_job_runs`), Phase 11 (`skill_audit`), and beyond inherits the pattern materialized here. Role separation is defense-in-depth (Amendment #17, Pitfall #6 in PRD). The literal D-07 error string and the redactDSN discipline (Pitfall #2) are tested invariants — paraphrasing breaks future phases' contracts.

Output: ~280 src LOC + ~120 test LOC across `internal/config/`, `internal/db/`, `cmd/aura/db.go`; 4 SQL migrations; 1 sqlc query file; sqlc-generated bindings (committed); `compose.yaml` (postgres-only at this stage), `Makefile`, `sqlc.yaml`, `.env.example` at repo root; `scripts/restore_drill.sh`; one-line PRD amendment correcting `sandbox/compose.yaml` → `compose.yaml` for both Slice 0.5 and Slice 0.7 file-target rows (per D-02).

This plan is closed by a single atomic commit per D-01: `slice 0.5: postgres + sqlc + pgx + golang-migrate infrastructure`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@d:\Aura\.planning\PROJECT.md
@d:\Aura\.planning\ROADMAP.md
@d:\Aura\.planning\STATE.md
@d:\Aura\.planning\REQUIREMENTS.md

# Phase context (mandatory)
@d:\Aura\.planning\phases\01-infra-db-knowledge\01-CONTEXT.md
@d:\Aura\.planning\phases\01-infra-db-knowledge\01-RESEARCH.md
@d:\Aura\.planning\phases\01-infra-db-knowledge\01-PATTERNS.md
@d:\Aura\.planning\phases\01-infra-db-knowledge\01-VALIDATION.md

# Source of truth
@d:\Aura\prd.md
@d:\Aura\CLAUDE.md

# Skeleton integration point (sole modified file outside greenfield)
@d:\Aura\cmd\aura\main.go
@d:\Aura\go.mod
</context>

<threat_model>

## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| operator-shell → Aura process | `aura db {...}` CLI args + env from `.env` cross from the user shell into the Aura binary; treated as semi-trusted (single-user local product per Out-of-Scope row) |
| Aura process → Postgres | DSNs carry credentials; pool errors may leak passwords if not redacted |
| `aura_app` role → schema `aura` | DB engine enforces grant set; defense-in-depth boundary between runtime and DDL |
| `compose.yaml` env interpolation → container env | `${POSTGRES_PASSWORD:?}` is the secrets gateway; must fail-fast if `.env` missing the key |
| `pg_dump` artifact → restore drill | dump files contain full DB contents including audit rows; treated as sensitive for Phase 10+ backup discipline (out of scope this slice but flagged) |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-1.05-01 | Information disclosure | `internal/db/db.go` pgxpool error path | mitigate | Implement `redactDSN(s string) string` private helper in `db.go`; wrap every pgx error as `fmt.Errorf("op to %s: %w", redactDSN(cfg.URL), err)`. Pitfall #2. Test `TestRedactDSN_StripsPassword` asserts the helper masks password. |
| T-1.05-02 | Elevation of privilege | `aura_app` role attempting DDL | mitigate | `0001_init.up.sql` grants `aura_app` only `USAGE` on schema + DEFAULT PRIVILEGES `SELECT, INSERT, UPDATE, DELETE` on TABLES; never `TRUNCATE`, `DROP`, `CREATE`. `TestRoleSeparation_AppDenied` asserts `permission denied for table` on attempted TRUNCATE/DROP as `aura_app`. |
| T-1.05-03 | Tampering | Concurrent `aura db migrate` invocations | mitigate | golang-migrate v4 acquires Postgres advisory lock during `m.Up()` (verified in upstream postgres driver source). `TestMigrate_AdvisoryLockHonored` spawns two parallel migrates and asserts one returns `ErrLockNotAcquired` or both serialize cleanly. |
| T-1.05-04 | Information disclosure | `.env.example` committed to repo | mitigate | `.env.example` ships only placeholder `changeme` values + explicit `# required, must change` comments; `.env` itself remains in `.gitignore` (verified during execution); `compose.yaml` uses `${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required in .env}` fail-fast interpolation. Grep gate: `grep -v '^#' .env.example | grep -cE '(password|secret).*=.*[^c][^h][^a][^n][^g][^e][^m][^e]'` returns 0. |
| T-1.05-05 | Information disclosure | `docker compose logs postgres` after first boot | accept | Postgres logs do not echo `POSTGRES_PASSWORD` env at startup (verified upstream); accept with monitoring note for Phase 10 backup slice. |
| T-1.05-06 | Tampering | `0001_init.up.sql` role-creation password handling | mitigate | golang-migrate iofs does NOT support psql variable substitution; Go-side bootstrap step (`internal/db/migrate.go::ensureRoles`) reads env passwords and executes parametrized role creation via pgxpool BEFORE running `m.Up()`. `TestEnsureRoles_NoPlaintextInError` asserts no plaintext password in any error wrap. |
| T-1.05-07 | Denial of service | `pg_dump` artifact path injection via `scripts/restore_drill.sh` arg | accept | Restore drill is operator-driven; arg `$1` is shell-quoted and only accepts a local file path. Documented as operator-trusted; future Phase 10 backup handler will add input validation. |

**Block-on threshold:** `high`. T-1.05-01 / T-1.05-02 / T-1.05-04 / T-1.05-06 are mitigations that MUST be verified by automated test before commit. T-1.05-05 / T-1.05-07 are accepted with rationale.

</threat_model>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Configuration scaffolding + compose.yaml + Makefile + sqlc.yaml + .env.example + PRD amendment</name>
  <files>
    compose.yaml,
    Makefile,
    sqlc.yaml,
    .env.example,
    internal/config/config.go,
    internal/db/config.go,
    prd.md
  </files>
  <read_first>
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-RESEARCH.md (§Code Examples lines 866-891 `sqlc.yaml`, lines 893-916 `compose.yaml` postgres-only, lines 980-1014 `Makefile` base targets, lines 1044-1051 `.env.example` Postgres block; §Architectural Responsibility Map "Composition" row; §Pattern 1 lines 332-376 for DBConfig fields),
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-PATTERNS.md (lines 168-208 `internal/config/config.go` shape; lines 212-233 `internal/db/config.go` shape; lines 428-444 `compose.yaml` critical config; lines 447-457 `Makefile` Slice 0.5 targets; lines 460-468 `sqlc.yaml` locked options; lines 471-481 `.env.example` Slice 0.5 keys),
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-CONTEXT.md (§Decisions D-02 compose at root, D-07 URL vs MigrateURL carriage),
    @d:\Aura\prd.md (§Slice 0.5 lines 21-127 — Postgres goal, stack, file targets; §Amendment #17 role separation),
    @d:\Aura\CLAUDE.md (§Persistence, §Env vars, §Behavioral rules god-class ban)
  </read_first>
  <behavior>
    - sqlc.yaml is `version: "2"` with engine `postgresql`, schema dir `internal/db/migrations`, queries dir `internal/db/queries`, output dir `internal/db/sqlc`, sql_package `pgx/v5`, emit_interface true, emit_exact_table_names true, emit_json_tags true, json_tags_case_style snake.
    - compose.yaml at REPO ROOT (not sandbox/) per D-02; ships ONLY the `postgres` service this slice (Slice 0.7 will append `neo4j` and `aura-llama-embed`); image `postgres:17.10-alpine3.23`; named volume `aura-postgres`; port mapped to `127.0.0.1:5432:5432` (loopback only); healthcheck `pg_isready -U aura -d aura` with interval 5s timeout 5s retries 10 start_period 10s; fail-fast env interpolation `${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required in .env}`.
    - Makefile targets `help`, `sqlc`, `db-up`, `db-migrate` (depends on db-up), `db-status`, `db-reset` (guarded by `[ "$$AURA_RESET_YES" = "1" ]`), `restore-drill` (depends on db-up). `db-up` waits for `docker compose ps --format json postgres` to report `"Health":"healthy"` via `until` loop. Header comment documents Windows operators should use PowerShell (Pitfall #7).
    - .env.example ships Postgres keys only this slice: `POSTGRES_PASSWORD=changeme`, `AURA_DB_URL=postgres://aura:changeme@127.0.0.1:5432/aura?sslmode=disable`, `AURA_DB_MIGRATE_URL=postgres://aura_migrate:changeme@127.0.0.1:5432/aura?sslmode=disable`. Each line has inline comment explaining role and "must change" requirement.
    - internal/config/config.go: package `config`; struct `Config{ DB db.Config; RunDir string; ToolPreviewCap int }` (Neo4j and LLM fields land in later slices); `Load() (*Config, error)` calls `_ = godotenv.Load()` then populates from env with private helpers `envDefault(key, fallback string) string` and `envIntDefault(key string, fallback int) int`; `RunDir` defaults via private `defaultRunDir()` returning OS-appropriate per-user dir under `$AURA_RUN_DIR` env override; `ToolPreviewCap` reads `AURA_CONTEXT_PREVIEW_CAP_BYTES` default 2048.
    - internal/db/config.go: package `db`; struct `Config{ URL string; MigrateURL string; MaxConns int32; MinConns int32; MaxConnIdleTime time.Duration }` per D-07; defaults applied in `db.Open` (zero values = pool defaults `MaxConns=10, MinConns=1, MaxConnIdleTime=30*time.Second`).
    - PRD amendment (one-line edit in `prd.md`): change Slice 0.5 file-target row `sandbox/compose.yaml (diff)` → `compose.yaml (diff)` AND Slice 0.7 file-target row `sandbox/compose.yaml (diff)` → `compose.yaml (diff)` per D-02. NO other PRD edits in this task.
    - Tests: `internal/config/config_test.go` (unit, no build tag) with `TestLoad_DefaultsApplied`, `TestLoad_EnvOverrides`, `TestEnvIntDefault_ParsesValid_FallsBackOnGarbage`. Coverage ≥75% on config package.
    - File LOC budget: compose.yaml ~25 lines (postgres-only), Makefile ~40 lines (base), sqlc.yaml ~25 lines, .env.example ~12 lines, internal/config/config.go ~110 LOC, internal/db/config.go ~40 LOC. All ≤600.
  </behavior>
  <action>
    Materialize the configuration + compose scaffolding for Slice 0.5 — every artifact greenfield except `prd.md`:

    1. Create `sqlc.yaml` at repo root using the locked options block from RESEARCH §Code Examples lines 866-891 (verbatim): `version: "2"`, single `sql` entry with `engine: postgresql`, schema `internal/db/migrations`, queries `internal/db/queries`, gen.go config (package=sqlc, out=internal/db/sqlc, sql_package=pgx/v5, sql_driver=github.com/jackc/pgx/v5, emit_interface=true, emit_exact_table_names=true, emit_json_tags=true, emit_empty_slices=true, emit_prepared_queries=false, json_tags_case_style=snake, output_db_file_name=db.go, output_models_file_name=models.go, output_querier_file_name=querier.go, omit_unused_structs=false).

    2. Create `compose.yaml` at repo root per D-02 (NOT `sandbox/compose.yaml`). Ships ONLY the postgres service per slice atomicity (Slice 0.7 will append `neo4j` + `aura-llama-embed`). Use the postgres block from RESEARCH §Code Examples lines 899-917 verbatim plus the `name: aura` top-level project name and the `volumes: { aura-postgres: }` declaration. Fail-fast interpolation `${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required in .env}`. Healthcheck `pg_isready -U aura -d aura`. Loopback-only port `127.0.0.1:5432:5432`.

    3. Create `Makefile` at repo root with Slice 0.5 targets only (Slice 0.7 will append `neo4j-*` + `smoke`): `help`, `sqlc`, `db-up`, `db-migrate` (depends on db-up), `db-status`, `db-reset` (guarded by `AURA_RESET_YES=1` env check), `restore-drill` (depends on db-up). Copy from RESEARCH §Code Examples lines 980-1014 base block. Phony declarations. Header comment notes Windows operators must use PowerShell or `MSYS_NO_PATHCONV=1` for `docker compose run` calls (Pitfall #7).

    4. Create `.env.example` at repo root with Slice 0.5 Postgres keys (per PATTERNS.md line 477): `POSTGRES_PASSWORD=changeme  # required, must change`, `AURA_DB_URL=postgres://aura:changeme@127.0.0.1:5432/aura?sslmode=disable  # role aura_app, runtime`, `AURA_DB_MIGRATE_URL=postgres://aura_migrate:changeme@127.0.0.1:5432/aura?sslmode=disable  # role aura_migrate, DDL only`. Slice 0.7 will append Neo4j + embed keys; do not include them here.

    5. Create `internal/config/config.go` per PATTERNS.md lines 174-208 (Slice 0.5 form). Public `Config` struct with fields `DB db.Config`, `RunDir string`, `ToolPreviewCap int`. Public `Load() (*Config, error)` that calls `_ = godotenv.Load()` then reads env. Private helpers `envDefault(key, fallback string) string`, `envIntDefault(key string, fallback int) int`, `defaultRunDir() string` (returns `os.UserCacheDir()/aura` on success, falls back to `os.TempDir()/aura`). Module path is `github.com/chetto1983/aura` per skeleton. Do NOT add `Neo4j` or `LLM` fields — those land in later slices.

    6. Create `internal/db/config.go` per PATTERNS.md lines 219-231 + D-07. Public `Config` struct with `URL string`, `MigrateURL string`, `MaxConns int32`, `MinConns int32`, `MaxConnIdleTime time.Duration`. No methods — pure data; defaults applied by `db.Open`.

    7. Create `internal/config/config_test.go` with three unit tests covering env override / defaults / int parsing fallback. Use `t.Setenv`. No build tag — runs in every PR. Use stdlib `testing` only.

    8. Edit `prd.md`: locate Slice 0.5 file-target row containing `sandbox/compose.yaml` and replace with `compose.yaml`; locate Slice 0.7 file-target row containing `sandbox/compose.yaml` and replace with `compose.yaml`. Both per D-02. NO other PRD edits in this task — the Slice 0.7 acceptance row 182 amendment and the ROADMAP `aura knowledge ping` correction belong to Slice 0.7 plan (03-PLAN.md).

    9. Post-edit Gate 2: `go vet ./...`, `go build ./...`, `go test ./internal/config/...`, `go test -race ./internal/config/...`. All four MUST exit 0 before declaring this task done.
  </action>
  <verify>
    <automated>cd d:/Aura && go vet ./internal/config/... && go build ./... && go test ./internal/config/... -race -count=1</automated>
    <commands>
      - `test -f d:/Aura/compose.yaml` (exit 0)
      - `test -f d:/Aura/Makefile` (exit 0)
      - `test -f d:/Aura/sqlc.yaml` (exit 0)
      - `test -f d:/Aura/.env.example` (exit 0)
      - `test -f d:/Aura/internal/config/config.go` (exit 0)
      - `test -f d:/Aura/internal/db/config.go` (exit 0)
      - `grep -q 'postgres:17.10-alpine3.23' d:/Aura/compose.yaml` (exit 0)
      - `grep -q '127.0.0.1:5432:5432' d:/Aura/compose.yaml` (exit 0)
      - `grep -q 'POSTGRES_PASSWORD required in .env' d:/Aura/compose.yaml` (exit 0)
      - `grep -q '^db-migrate:' d:/Aura/Makefile` (exit 0)
      - `grep -q 'version: "2"' d:/Aura/sqlc.yaml` (exit 0)
      - `grep -q 'out: internal/db/sqlc' d:/Aura/sqlc.yaml` (exit 0)
      - `grep -q 'AURA_DB_MIGRATE_URL=' d:/Aura/.env.example` (exit 0)
      - `grep -qv '^#' d:/Aura/.env.example | grep -q 'changeme' d:/Aura/.env.example` (placeholder safety)
      - `grep -c 'sandbox/compose.yaml' d:/Aura/prd.md` returns 0 (D-02 amendment applied to both Slice 0.5 and Slice 0.7 rows)
      - `grep -c '^compose.yaml' d:/Aura/prd.md` returns ≥ 2 (one Slice 0.5 row, one Slice 0.7 row)
      - `wc -l d:/Aura/compose.yaml d:/Aura/Makefile d:/Aura/sqlc.yaml d:/Aura/.env.example d:/Aura/internal/config/config.go d:/Aura/internal/db/config.go` — every value ≤ 600
    </commands>
  </verify>
  <done>
    All seven artifacts exist with locked content. PRD amendment (D-02) committed in this slice's task — both Slice 0.5 and Slice 0.7 file-target rows now reference `compose.yaml` (root). `go vet` + `go build` + `go test ./internal/config/... -race` all exit 0. `internal/config/config_test.go` coverage ≥75% per package. No file exceeds 600 LOC.
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Postgres pool + migrate runner + migrations + sqlc codegen + restore drill + integration tests</name>
  <files>
    internal/db/db.go,
    internal/db/migrate.go,
    internal/db/ping.go,
    internal/db/status.go,
    internal/db/reset.go,
    internal/db/migrations/0001_init.up.sql,
    internal/db/migrations/0001_init.down.sql,
    internal/db/migrations/0002_knowledge_migrations.up.sql,
    internal/db/migrations/0002_knowledge_migrations.down.sql,
    internal/db/queries/knowledge_migrations.sql,
    internal/db/sqlc/db.go,
    internal/db/sqlc/models.go,
    internal/db/sqlc/querier.go,
    internal/db/sqlc/knowledge_migrations.sql.go,
    internal/db/db_test.go,
    cmd/aura/main.go,
    cmd/aura/db.go,
    scripts/restore_drill.sh
  </files>
  <read_first>
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-RESEARCH.md (§Pattern 1 lines 332-376 pgxpool.Open shape; §Pattern 2 lines 386-425 golang-migrate iofs runner; §Pattern 4 lines 537-565 audit table + sqlc queries; §Code Examples lines 752-806 `0001_init.up.sql`/`.down.sql`, lines 810-833 `0002_knowledge_migrations.*`, lines 1074-1113 `restore_drill.sh`; §Common Pitfalls #1 default privileges, #2 DSN redaction; §Validation Architecture INFRA-01 rows; §Assumptions Log A9 iofs path),
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-PATTERNS.md (lines 84-165 `cmd/aura/main.go` surgical extension; lines 237-271 `internal/db/db.go` shape; lines 275-307 `internal/db/migrate.go` shape; lines 310-323 migrations 0001 critical decisions; lines 326-347 migration 0002 schema; lines 351-368 sqlc source; lines 381-391 ping/status/reset; lines 394-424 db_test.go mandatory structure; lines 486-491 restore_drill.sh),
    @d:\Aura\.planning\phases\01-infra-db-knowledge\01-CONTEXT.md (§Decisions D-01 atomic commit, D-07 literal error string, D-08 minimal schema scope reminder for Slice 0.7),
    @d:\Aura\cmd\aura\main.go (lines 22-46 existing switch dispatcher + usage line — surgical extension only),
    @d:\Aura\go.mod (verify pgx/v5 + golang-migrate/v4 + godotenv + goleak + uuid already // indirect; no go get needed)
  </read_first>
  <behavior>
    - `internal/db/db.go` exports `Open(ctx context.Context, cfg *Config) (*pgxpool.Pool, error)` per RESEARCH Pattern 1. Applies pool defaults when cfg fields are zero. Wraps every error with `redactDSN(s string) string` private helper (Pitfall #2 mitigation) that uses `net/url.Parse` to mask password. Exports `Close(pool *pgxpool.Pool) error` if non-trivial; otherwise rely on `pool.Close()` directly.
    - `internal/db/migrate.go` exports `Migrate(ctx context.Context, migrateURL string) (int, error)` per RESEARCH Pattern 2. `//go:embed migrations/*.sql` declares `migrationsFS`. Returns count of newly applied (`post - pre` from `m.Version()` calls). Empty `migrateURL` returns `errors.New("AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17")` verbatim per D-07. Wraps `migrate.ErrNoChange` as success (count = 0). Also exports `EnsureRoles(ctx context.Context, migrateURL string, appPassword, migratePassword string) error` — Go-side bootstrap that runs role-creation DDL via pgxpool BEFORE `m.Up()` (T-1.05-06 mitigation — golang-migrate iofs does not support psql vars). EnsureRoles uses parametrized queries via `pool.Exec(ctx, "DO $$ ... END $$", appPassword, migratePassword)` form OR temp connection + literal interpolation through `pgx.Identifier`-safe formatting — choose parametrized to avoid plaintext-in-error.
    - `internal/db/ping.go` exports `Ping(ctx context.Context, pool *pgxpool.Pool) (time.Duration, error)` — `SELECT 1`, measures latency, returns it. CLI handler prints `ok: ping in Xms`.
    - `internal/db/status.go` exports `Status(ctx context.Context, pool *pgxpool.Pool) ([]MigrationRow, error)` — queries golang-migrate's own `schema_migrations` table (`SELECT version, dirty FROM schema_migrations`); returns rows. CLI handler tabulates.
    - `internal/db/reset.go` exports `Reset(ctx context.Context, migrateURL string) error` — guarded by `--yes` flag at CLI level; calls `migrate.Down()` then `migrate.Up()` to re-apply. DROP SCHEMA CASCADE only if migrate-down fails halfway (defensive cleanup).
    - `internal/db/schema.sql` is NOT created in this slice — sqlc.yaml is configured to consume the `internal/db/migrations` directory directly (per RESEARCH §Code Examples lines 866-891). If a future executor discovers that `sqlc generate` requires a single schema file vs the migrations directory, they may add `internal/db/schema.sql` then as a legitimate deviation with PRD-amendment trail; do not pre-create it.
    - `internal/db/migrations/0001_init.up.sql` per RESEARCH §Code Examples lines 752-786. CREATE SCHEMA IF NOT EXISTS aura. CREATE ROLE aura_app + aura_migrate using `DO $$` blocks with `IF NOT EXISTS` checks (passwords come from Go-side EnsureRoles bootstrap, NOT from psql vars — see note line 788). GRANT USAGE ON SCHEMA aura TO aura_app; GRANT CREATE, USAGE ON SCHEMA aura TO aura_migrate. ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO aura_app (Pitfall #1). NO grant of TRUNCATE/DROP/CREATE to aura_app. COMMENT ON SCHEMA aura is the documentation hook for search_path.
    - `internal/db/migrations/0001_init.down.sql` per RESEARCH lines 790-806: REVOKE all + DROP SCHEMA aura CASCADE + DROP ROLE aura_app + DROP ROLE aura_migrate (all IF EXISTS).
    - `internal/db/migrations/0002_knowledge_migrations.up.sql` per RESEARCH lines 812-827 + PATTERNS.md lines 333-345: `CREATE TABLE aura.knowledge_migrations (version integer PRIMARY KEY, name text NOT NULL, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`; `GRANT SELECT, INSERT ON aura.knowledge_migrations TO aura_app` (Slice 0.7 needs INSERT for audit row writes); `GRANT ALL ON aura.knowledge_migrations TO aura_migrate`; `CREATE INDEX knowledge_migrations_applied_at_idx ON aura.knowledge_migrations (applied_at DESC)`; `COMMENT ON TABLE`.
    - `internal/db/migrations/0002_knowledge_migrations.down.sql` per RESEARCH line 833: `DROP TABLE IF EXISTS aura.knowledge_migrations`.
    - `internal/db/queries/knowledge_migrations.sql` per RESEARCH Pattern 4 lines 557-564 + PATTERNS.md lines 356-364: two named queries — `RecordKnowledgeMigration :exec` INSERT, `ListAppliedKnowledgeMigrations :many` SELECT ORDER BY version ASC.
    - `internal/db/sqlc/*.go` generated by `sqlc generate`; committed; CI golden test (`make sqlc && git diff --exit-code internal/db/sqlc/`) enforces sync.
    - `internal/db/db_test.go` with `//go:build db_integration` tag, `TestMain` calls `goleak.VerifyTestMain(m)`. Tests: `TestMigrate_Idempotent` (re-run returns 0 newly applied), `TestMigrate_MissingURLFailsFast` (unit-safe, no container needed; asserts `err.Error()` contains literal D-07 string), `TestRoleSeparation_AppDenied` (connect as aura_app, attempt TRUNCATE, assert permission denied), `TestPing` (SELECT 1 + latency), `TestConfig_PoolParams` (unit-safe — verifies cfg fields propagate to pgxpool.Config without opening), `TestRedactDSN_StripsPassword` (unit-safe — asserts helper masks password), `TestEnsureRoles_NoPlaintextInError` (unit-safe — induce role error, assert no plaintext password in error string), `TestRecordAndListKnowledgeMigrations` (integration — INSERT a row via aura_app, SELECT via ListAppliedKnowledgeMigrations).
    - `cmd/aura/main.go` surgical edit per PATTERNS.md lines 120-127: insert ONE new case `case "db": runDB(os.Args[2:])` between existing `"chat"` and `"shell"/"serve"` branches. Update usage line to `usage: aura {serve|shell|chat <msg>|tools|db <sub>}`. Do NOT touch existing tools/chat/shell/serve branches or buildRegistry/stubClient helpers.
    - `cmd/aura/db.go` NEW file per PATTERNS.md lines 140-161: `runDB(args []string)` with inner switch over `migrate|ping|status|reset`. Calls `config.Load()`. For `migrate`: uses `cfg.DB.MigrateURL` (D-07: empty → fail-fast via `db.Migrate` returning the literal error). For `ping`: opens pool with `cfg.DB.URL` (aura_app role). For `status`: opens pool with `cfg.DB.URL`. For `reset`: requires `--yes` flag in args, uses `cfg.DB.MigrateURL`. On any error: print to stderr + os.Exit(1).
    - `scripts/restore_drill.sh` per RESEARCH §Code Examples lines 1074-1113 verbatim. `#!/usr/bin/env bash`, `set -euo pipefail`, takes optional `$1` dump file path, runs `pg_dump` if missing, drops + recreates `aura_restore_drill` DB, times `pg_restore`, asserts `ELAPSED_MS < 90000`, cleanup. Documents `jq` not required; uses `date +%s%N`.
    - Run `make sqlc` (Task 1's Makefile target) to regenerate `internal/db/sqlc/`; commit the generated files.
    - File LOC budgets: db.go ~90, migrate.go ~80, ping.go ~30, status.go ~50, reset.go ~40, 0001_init.up.sql ~50, 0001_init.down.sql ~10, 0002.up.sql ~20, 0002.down.sql ~3, queries/knowledge_migrations.sql ~10, db_test.go ~150 (split into db_role_test.go + db_migrate_test.go if exceeds 400 — none expected to exceed 600), cmd/aura/db.go ~80, scripts/restore_drill.sh ~40. All ≤600.
  </behavior>
  <action>
    Build the Postgres runtime + migrate runner + sqlc bindings + CLI dispatcher + restore drill — every file greenfield except `cmd/aura/main.go` (surgical extension only).

    1. Create `internal/db/db.go` per PATTERNS.md lines 237-271 + RESEARCH §Pattern 1. Public `Open(ctx context.Context, cfg *Config) (*pgxpool.Pool, error)` calls `pgxpool.ParseConfig(cfg.URL)`, applies defaults when fields are zero (`MaxConns=10, MinConns=1, MaxConnIdleTime=30*time.Second`), calls `pgxpool.NewWithConfig`, then `pool.Ping(ctx)`. Wrap ALL errors with `fmt.Errorf("open pool to %s: %w", redactDSN(cfg.URL), err)`. Private `redactDSN(s string) string` uses `net/url.Parse`; on success masks `u.User` via `url.UserPassword(u.User.Username(), "***")`; on parse error returns `"<unparseable-dsn>"`. T-1.05-01 mitigation.

    2. Create `internal/db/migrate.go` per PATTERNS.md lines 275-307 + RESEARCH §Pattern 2. `//go:embed migrations/*.sql` declares `migrationsFS embed.FS`. Public `Migrate(ctx context.Context, migrateURL string) (int, error)` — empty `migrateURL` returns `errors.New("AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17")` LITERAL (D-07). Calls `iofs.New(migrationsFS, "migrations")`, `migrate.NewWithSourceInstance("iofs", src, migrateURL)`, `defer m.Close()`, reads `pre, _, _ := m.Version()`, calls `m.Up()` mapping `migrate.ErrNoChange` to success, reads `post, _, _ := m.Version()`, returns `int(post) - int(pre)`. Public `EnsureRoles(ctx context.Context, migrateURL string, appPassword, migratePassword string) error` opens a one-shot pgxpool against migrateURL, runs role-creation DDL via parametrized queries (NOT psql vars), closes. Document at top of file: "Go-side EnsureRoles bootstrap runs BEFORE migrate.Up() because golang-migrate iofs does not support psql variable substitution — see RESEARCH §Code Examples note line 788."

    3. Create `internal/db/ping.go`, `internal/db/status.go`, `internal/db/reset.go` per behavior spec above. `Ping` runs `SELECT 1` and returns measured `time.Duration`. `Status` queries `schema_migrations` table (golang-migrate's own table at default name); each row has `version bigint` + `dirty bool`. `Reset` is dev-only: refuses without `--yes` arg + `AURA_RESET_YES=1` env; on confirm, runs `m.Down()` then `m.Up()`.

    4. Create the SQL migrations:
       - `internal/db/migrations/0001_init.up.sql` per RESEARCH §Code Examples lines 752-786. CREATE SCHEMA + CREATE ROLE via `DO $$` blocks (roles created with literal passwords substituted by Go-side EnsureRoles via parametrized queries, NOT psql vars — the .up.sql contains the GRANT/DEFAULT PRIVILEGES lines but the role creation happens in Go before migrate.Up runs). Document the split in a comment at the top of the file. GRANT USAGE ON SCHEMA aura TO aura_app; GRANT CREATE, USAGE ON SCHEMA aura TO aura_migrate. ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO aura_app + USAGE, SELECT ON SEQUENCES TO aura_app (Pitfall #1). NO TRUNCATE/DROP/CREATE grant to aura_app.
       - `internal/db/migrations/0001_init.down.sql` per RESEARCH lines 790-806: REVOKE + DROP SCHEMA CASCADE + DROP ROLE (all IF EXISTS).
       - `internal/db/migrations/0002_knowledge_migrations.up.sql` per RESEARCH lines 810-828. CREATE TABLE + GRANT SELECT, INSERT to aura_app + GRANT ALL to aura_migrate + CREATE INDEX + COMMENT.
       - `internal/db/migrations/0002_knowledge_migrations.down.sql` per RESEARCH line 833: `DROP TABLE IF EXISTS aura.knowledge_migrations`.

    5. Create `internal/db/queries/knowledge_migrations.sql` per RESEARCH §Pattern 4 lines 557-564: `-- name: RecordKnowledgeMigration :exec` INSERT with 3 placeholders + `-- name: ListAppliedKnowledgeMigrations :many` SELECT ORDER BY version ASC.

    6. Do NOT create `internal/db/schema.sql` — sqlc.yaml uses the `internal/db/migrations` directory directly. (If a future executor finds `sqlc generate` actually requires the single-file form, they may add it then with a PRD-amendment trail — out of scope for this commit.)

    7. Run `make sqlc` to generate `internal/db/sqlc/{db,models,querier,knowledge_migrations.sql}.go`. Commit the generated files. CI golden test: `make sqlc && git diff --exit-code internal/db/sqlc/` exits 0.

    8. Create `internal/db/db_test.go` with `//go:build db_integration` build tag, `goleak.VerifyTestMain` in `TestMain`. Tests: TestMigrate_Idempotent (integration), TestMigrate_MissingURLFailsFast (unit — asserts literal D-07 string via `assert.Contains` or `strings.Contains`), TestRoleSeparation_AppDenied (integration — connect as aura_app, attempt `TRUNCATE aura.knowledge_migrations` + `DROP TABLE aura.knowledge_migrations`, both must return permission denied), TestPing (integration), TestConfig_PoolParams (unit — verify pool config defaults applied for zero-value Config), TestRedactDSN_StripsPassword (unit — assert helper outputs masked DSN for parseable input + `<unparseable-dsn>` for garbage), TestEnsureRoles_NoPlaintextInError (unit + integration variant — induce role creation error against a closed connection, assert error string contains zero plaintext password chars), TestRecordAndListKnowledgeMigrations (integration — INSERT via aura_app + SELECT all, verify ordering by version).

    9. Edit `cmd/aura/main.go` surgically per PATTERNS.md lines 120-127:
       - Insert ONE new case in the existing `switch os.Args[1]` block between `"chat"` and `"shell", "serve":` branches:
         ```
         case "db":
             runDB(os.Args[2:])
         ```
       - Update `usage()` line to: `usage: aura {serve|shell|chat <msg>|tools|db <sub>}`.
       - Add `import "github.com/chetto1983/aura/cmd/aura"` if `runDB` lives in same `main` package (no import needed — same package); otherwise leave imports untouched.
       - DO NOT touch existing tools/chat/shell/serve branches or `buildRegistry()`/`printTools()`/`chatOnce()`/`stubClient` helpers.

    10. Create `cmd/aura/db.go` NEW file in `package main` per PATTERNS.md lines 140-161. Public `runDB(args []string)` with `len(args) < 1` → usage error + exit 1. Calls `config.Load()`; on err fail. Inner switch over `args[0]`: `"migrate"` → `db.Migrate(ctx, cfg.DB.MigrateURL)` + print result; `"ping"` → `db.Open(ctx, &cfg.DB)` + `db.Ping(ctx, pool)` + print; `"status"` → `db.Open` + `db.Status` + tabulate; `"reset"` → check `--yes` in args[1:] + check `AURA_RESET_YES=1` env, then `db.Reset(ctx, cfg.DB.MigrateURL)`. On any error: `fmt.Fprintln(os.Stderr, err); os.Exit(1)`.

    11. Create `scripts/restore_drill.sh` per RESEARCH §Code Examples lines 1074-1113 verbatim. `chmod +x` after creation. Self-tests in shell: bash `-n` syntax check + `shellcheck` clean (if available).

    12. Post-edit Gate 2 (mandatory): `go vet ./...`, `go build ./...`, `go test ./internal/db/...` (unit only), `go test -race ./internal/db/...` (unit only). Document container-running integration suite in commit body: `make db-up && go test -race -tags db_integration ./internal/db/...`.

    13. Verify coverage: `go test ./internal/db/... -tags db_integration -race -coverprofile=cover.out && go tool cover -func=cover.out | grep total:` returns ≥ 0.60 (60% integration); pure unit coverage ≥ 0.75.
  </action>
  <verify>
    <automated>cd d:/Aura && go vet ./... && go build ./... && go test ./internal/db/... -race -count=1 && go test -tags db_integration -run TestMigrate_MissingURLFailsFast ./internal/db -count=1</automated>
    <commands>
      - `test -f d:/Aura/internal/db/db.go` (exit 0)
      - `test -f d:/Aura/internal/db/migrate.go` (exit 0)
      - `test -f d:/Aura/internal/db/migrations/0001_init.up.sql` (exit 0)
      - `test -f d:/Aura/internal/db/migrations/0002_knowledge_migrations.up.sql` (exit 0)
      - `test -f d:/Aura/internal/db/queries/knowledge_migrations.sql` (exit 0)
      - `test -f d:/Aura/internal/db/sqlc/knowledge_migrations.sql.go` (exit 0)
      - `test -f d:/Aura/cmd/aura/db.go` (exit 0)
      - `test -f d:/Aura/scripts/restore_drill.sh && test -x d:/Aura/scripts/restore_drill.sh` (exit 0)
      - `grep -q 'AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17' d:/Aura/internal/db/migrate.go` (exit 0 — D-07 literal)
      - `grep -q '//go:embed migrations/\*\.sql' d:/Aura/internal/db/migrate.go` (exit 0)
      - `grep -q 'case "db":' d:/Aura/cmd/aura/main.go` (exit 0 — dispatcher extended)
      - `grep -q 'usage: aura {serve|shell|chat <msg>|tools|db <sub>}' d:/Aura/cmd/aura/main.go` (exit 0 — usage line updated)
      - `grep -q 'CREATE SCHEMA IF NOT EXISTS aura' d:/Aura/internal/db/migrations/0001_init.up.sql` (exit 0)
      - `grep -q 'CREATE TABLE aura.knowledge_migrations' d:/Aura/internal/db/migrations/0002_knowledge_migrations.up.sql` (exit 0)
      - `grep -q 'GRANT SELECT, INSERT ON aura.knowledge_migrations TO aura_app' d:/Aura/internal/db/migrations/0002_knowledge_migrations.up.sql` (exit 0 — Slice 0.7 needs this grant)
      - `grep -Eq 'ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate' d:/Aura/internal/db/migrations/0001_init.up.sql` (exit 0 — Pitfall #1 mitigation)
      - `grep -cE 'GRANT (TRUNCATE|DROP|CREATE).*aura_app' d:/Aura/internal/db/migrations/0001_init.up.sql` returns 0 (T-1.05-02 — aura_app must NOT have these grants)
      - `grep -q '//go:build db_integration' d:/Aura/internal/db/db_test.go` (exit 0)
      - `grep -q 'goleak.VerifyTestMain' d:/Aura/internal/db/db_test.go` (exit 0)
      - `grep -q 'TestRoleSeparation_AppDenied' d:/Aura/internal/db/db_test.go` (exit 0)
      - `grep -q 'TestMigrate_MissingURLFailsFast' d:/Aura/internal/db/db_test.go` (exit 0)
      - `grep -q 'ELAPSED_MS > 90000' d:/Aura/scripts/restore_drill.sh` (exit 0 — ROADMAP SC#3)
      - `bash -n d:/Aura/scripts/restore_drill.sh` (exit 0 — shell syntax)
      - `cd d:/Aura && make sqlc && git diff --exit-code internal/db/sqlc/` (exit 0 — sqlc golden test; INFRA-01 regression dimension)
      - Container-gated (run after `make db-up`): `cd d:/Aura && go test -tags db_integration -race ./internal/db/... -count=1` exits 0
      - Container-gated: `cd d:/Aura && AURA_DB_MIGRATE_URL=$(grep ^AURA_DB_MIGRATE_URL .env | cut -d= -f2) go run ./cmd/aura db migrate` exits 0 with stdout matching `ok:` prefix; second invocation exits 0 with stdout matching `no pending` or `0 newly applied` (ROADMAP SC#1 idempotency)
      - Container-gated: `cd d:/Aura && PGPASSWORD=$(grep ^POSTGRES_PASSWORD .env | cut -d= -f2) bash scripts/restore_drill.sh` exits 0 with elapsed_ms < 90000 (ROADMAP SC#3)
      - `wc -l d:/Aura/internal/db/*.go d:/Aura/cmd/aura/db.go d:/Aura/scripts/restore_drill.sh` — every value ≤ 600
    </commands>
  </verify>
  <done>
    Postgres + sqlc + pgx + golang-migrate stack operational. `aura db migrate` runs idempotently against fresh container (ROADMAP SC#1 ✓). `aura_app` role denied DDL operations (ROADMAP SC#2 ✓ via TestRoleSeparation_AppDenied). `scripts/restore_drill.sh` completes < 90s against pg_dump artifact (ROADMAP SC#3 ✓). `make sqlc` golden test green. Literal D-07 error string asserted by TestMigrate_MissingURLFailsFast. DSN redaction asserted by TestRedactDSN_StripsPassword. INFRA-01 coverage thresholds met (≥75% unit, ≥60% integration). No file exceeds 600 LOC. `cmd/aura/main.go` surgical edit preserves all existing branches.

    **Atomic commit per D-01** with template (copy verbatim from PRD §Slice 0.5 lines 109-125):
    ```
    slice 0.5: postgres + sqlc + pgx + golang-migrate infrastructure

    [body: enumerate every artifact created; note D-02 PRD amendment (sandbox/compose.yaml → compose.yaml for both Slice 0.5 and Slice 0.7 rows) landed in this commit per CONTEXT.md decision; note T-1.05-01 redactDSN mitigation; note T-1.05-02 role-deny test passing; note Go 1.26.0 bump fortuitously satisfies Phase 0 Amendment #1; reference RESEARCH.md Pattern 1/2/4 anchors; reference D-01 atomic commit + D-07 literal error string discipline]

    Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
    ```

    After this commit, Gate 2 verification narrows to `./internal/config/... ./internal/db/...`. Slice 0.7 (03-PLAN.md) starts in Wave 2.
  </done>
</task>

</tasks>

<verification>

## Slice 0.5 Phase Verification (after both tasks complete)

```bash
# Quick gate (per-task, no containers)
cd d:/Aura
go vet ./...
go build ./...
go test ./internal/config/... ./internal/db/... -race -count=1   # unit-only, build-tag-excluded integration skips

# Integration gate (containers warm)
make db-up
go test -tags db_integration -race -count=1 ./internal/db/...
# Expected: TestMigrate_Idempotent, TestMigrate_MissingURLFailsFast, TestRoleSeparation_AppDenied, TestPing, TestConfig_PoolParams, TestRedactDSN_StripsPassword, TestEnsureRoles_NoPlaintextInError, TestRecordAndListKnowledgeMigrations — all PASS

# CLI smoke
go run ./cmd/aura db migrate   # exits 0, prints "ok: 2 migrations applied (0001_init, 0002_knowledge_migrations)"
go run ./cmd/aura db migrate   # exits 0, prints "ok: no pending migrations" (idempotency — ROADMAP SC#1)
go run ./cmd/aura db ping      # exits 0, prints "ok: ping in Xms"
go run ./cmd/aura db status    # exits 0, tabulates schema_migrations rows

# Role separation (ROADMAP SC#2)
AURA_DB_MIGRATE_URL=postgres://aura_app:CHANGEME@127.0.0.1:5432/aura?sslmode=disable go run ./cmd/aura db migrate
# Expected: non-zero exit + stderr contains "permission denied"

# Fail-fast on missing MigrateURL (D-07)
unset AURA_DB_MIGRATE_URL && go run ./cmd/aura db migrate
# Expected: non-zero exit + stderr contains literal "AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17"

# Restore drill (ROADMAP SC#3)
PGPASSWORD=$(grep ^POSTGRES_PASSWORD .env | cut -d= -f2) bash scripts/restore_drill.sh
# Expected: exit 0, "ok: restore drill PASSED (<90000 ms)"

# sqlc golden test
make sqlc && git diff --exit-code internal/db/sqlc/    # exit 0

# Structural: every file ≤600 LOC
wc -l internal/db/*.go internal/config/*.go cmd/aura/db.go scripts/restore_drill.sh compose.yaml Makefile sqlc.yaml .env.example
# Expected: every value ≤ 600

# PRD amendment (D-02) verification
grep -c 'sandbox/compose.yaml' prd.md   # returns 0
grep -cE '^[^|]*compose\.yaml' prd.md   # returns ≥ 2 (Slice 0.5 + Slice 0.7 file-target rows)
```

</verification>

<success_criteria>

Slice 0.5 closes when:

- [ ] All artifacts in `files_modified` exist at the specified paths
- [ ] `go vet ./... && go build ./... && go test ./internal/... -race` exits 0 (unit gate)
- [ ] `go test -tags db_integration -race ./internal/db/...` exits 0 with all listed tests passing (integration gate)
- [ ] Coverage ≥ 75% unit on `internal/config` + `internal/db`; ≥ 60% integration on `internal/db` (PRD Gate 3 thresholds)
- [ ] `make sqlc && git diff --exit-code internal/db/sqlc/` exits 0 (CI golden test)
- [ ] `aura db migrate` idempotent (ROADMAP SC#1)
- [ ] `aura db migrate` as `aura_app` returns permission denied (ROADMAP SC#2 / T-1.05-02 mitigation)
- [ ] `scripts/restore_drill.sh` completes < 90s (ROADMAP SC#3)
- [ ] Literal D-07 error string present in `migrate.go` and asserted by test
- [ ] `redactDSN` helper masks password (T-1.05-01 mitigation, asserted)
- [ ] No file in this slice exceeds 600 LOC (god-class ban)
- [ ] PRD amendment (D-02) landed: `sandbox/compose.yaml` → `compose.yaml` for both Slice 0.5 and Slice 0.7 rows
- [ ] Single atomic commit per D-01 with PRD-verbatim template + Co-Authored-By trailer
- [ ] `01-VALIDATION.md` per-task table updated: tasks 1 + 2 rows marked ✅ green; Wave 0 gaps 1-7 (compose/Makefile/.env/sqlc/0001/0002/db_test/restore_drill/role-separation harness) closed

</success_criteria>

<output>
On task completion, write `.planning/phases/01-infra-db-knowledge/01-02-SUMMARY.md` documenting:
- Artifacts created and LOC actually used per file
- Test results (which tests run, coverage %, any flakes)
- D-02 PRD amendment scope (line-level diff)
- Commit SHA + body
- Any deviations from this plan (RESEARCH skeleton drift, etc.)
- Handoff notes for 03-PLAN.md (Slice 0.7) — specifically: `aura.knowledge_migrations` table is now present with `INSERT` grant to `aura_app`; `internal/db/sqlc/knowledge_migrations.sql.go` exports `RecordKnowledgeMigration` + `ListAppliedKnowledgeMigrations` ready for Slice 0.7 consumption
</output>
