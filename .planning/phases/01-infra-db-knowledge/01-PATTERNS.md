# Phase 1: Infra DB + Knowledge - Pattern Map

**Mapped:** 2026-05-29
**Files analyzed:** 31 new/modified (Slice 0.5 = 14, Slice 0.7 = 17)
**Analogs found:** 1 / 31 (the rest are greenfield → RESEARCH.md skeleton anchors)

> **Greenfield reality:** the post-rewrite skeleton (`af4ca65c`, 633 LOC) has **zero** pre-existing patterns for DB, Neo4j, MCP subprocess, config, migrations, sqlc, compose, Makefile, or `.env`. The ONE integration point is the `cmd/aura/main.go` subcommand dispatcher. Every other file pulls its canonical pattern from RESEARCH.md §Code Examples / §Architecture Patterns. The planner copies from RESEARCH skeletons, not from existing code.

---

## Starting State (already-applied side effects)

The researcher session left the working tree with:

- `go.mod` at `go 1.26.0` (verified via `Read d:\Aura\go.mod`) — **NOTE drift:** RESEARCH.md §Summary claims `go 1.25.0`, actual file shows `go 1.26.0`. Both satisfy Phase 0 Amendment #1 (≥ Go 1.25). Planner: **do not re-edit `go 1.26.0`**; treat as starting state.
- `go.mod` `require` block (all `// indirect` because no Go code imports them yet):
  - `github.com/golang-migrate/migrate/v4 v4.19.1`
  - `github.com/google/uuid v1.6.0`
  - `github.com/jackc/pgpassfile v1.0.0`
  - `github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761`
  - `github.com/jackc/pgx/v5 v5.9.2`
  - `github.com/joho/godotenv v1.5.1`
  - `go.uber.org/goleak v1.3.0`
  - `golang.org/x/text v0.31.0`
- `go.sum` exists with checksums for the above.

**Planner instruction:** Slice 0.5 plan must NOT include `go get pgx/v5 / golang-migrate / godotenv / goleak / uuid` tasks. They are already staged. The deps flip from `// indirect` → direct as soon as the first `import` lands in Slice 0.5.

---

## File Classification

### Slice 0.5 — Postgres (lands first, gates Slice 0.7)

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `cmd/aura/main.go` (modified) | cli-handler (dispatcher edit) | CLI entry → switch case | `cmd/aura/main.go:22-42` (self) | exact (self-extend) |
| `internal/config/config.go` | config | godotenv load → struct composite | NONE — greenfield | NONE → RESEARCH §Architectural Responsibility Map "Composition" row |
| `internal/db/config.go` | config | env → `DBConfig{URL, MigrateURL, pool params}` | NONE — greenfield | NONE → RESEARCH §Pattern 1 lines 332-376 |
| `internal/db/db.go` | sqlc-source / pool-open | env → `pgxpool.ParseConfig` → `pgxpool.NewWithConfig` → `Pool.Ping` | NONE — greenfield | NONE → RESEARCH §Pattern 1 `Open()` func skeleton |
| `internal/db/migrate.go` | schema-migration runner | embed.FS → `iofs.New` → `migrate.NewWithSourceInstance` → `m.Up()` | NONE — greenfield | NONE → RESEARCH §Pattern 2 lines 386-425 |
| `internal/db/ping.go` | cli-handler subcommand impl | pool → `SELECT 1` → latency print | NONE — greenfield | NONE → RESEARCH §Recommended Project Structure row "ping.go ~30 LOC" |
| `internal/db/status.go` | cli-handler subcommand impl | pool → query `schema_migrations` table → tabulate | NONE — greenfield | NONE → RESEARCH §Recommended Project Structure row "status.go ~50 LOC" |
| `internal/db/reset.go` | cli-handler subcommand impl | MigrateURL → `DROP SCHEMA CASCADE` → re-migrate (dev guard) | NONE — greenfield | NONE → RESEARCH §Recommended Project Structure row "reset.go ~40 LOC" |
| `internal/db/schema.sql` | sqlc-source | concat migrations for sqlc engine | NONE — greenfield | NONE → RESEARCH §`sqlc.yaml` (schema dir = `internal/db/migrations`) |
| `internal/db/migrations/0001_init.up.sql` | schema-migration | DDL: CREATE SCHEMA + roles + default privileges | NONE — greenfield | NONE → RESEARCH §Code Examples `0001_init.up.sql` lines 752-786 |
| `internal/db/migrations/0001_init.down.sql` | schema-migration (reversal) | DROP SCHEMA CASCADE + DROP ROLE | NONE — greenfield | NONE → RESEARCH §Code Examples `0001_init.down.sql` lines 790-806 |
| `internal/db/migrations/0002_knowledge_migrations.up.sql` | schema-migration | DDL: `aura.knowledge_migrations` + grants + index | NONE — greenfield | NONE → RESEARCH §Code Examples `0002_knowledge_migrations.up.sql` lines 810-828 |
| `internal/db/migrations/0002_knowledge_migrations.down.sql` | schema-migration (reversal) | DROP TABLE | NONE — greenfield | NONE → RESEARCH §Code Examples line 833 |
| `internal/db/queries/knowledge_migrations.sql` | sqlc-source | sqlc named queries: RecordKnowledgeMigration (`:exec`) + ListAppliedKnowledgeMigrations (`:many`) | NONE — greenfield | NONE → RESEARCH §Pattern 4 lines 556-565 |
| `internal/db/sqlc/*.go` (generated) | sqlc-source (codegen output) | sqlc generate from queries + schema | NONE — greenfield | NONE → `make sqlc` produces; committed; CI golden test |
| `internal/db/db_test.go` (`//go:build db_integration`) | test-harness | goleak + container pool + idempotent migrate + role-deny | NONE — greenfield | NONE → RESEARCH §Validation Architecture "INFRA-01" rows |
| `compose.yaml` (root, NEW) | docker-infra | Compose → postgres service | NONE — greenfield | NONE → RESEARCH §Code Examples `compose.yaml` lines 893-916 |
| `Makefile` (root, NEW) | makefile | targets: sqlc / db-up / db-migrate / db-status / db-reset / restore-drill | NONE — greenfield | NONE → RESEARCH §Code Examples `Makefile` lines 980-1014 |
| `sqlc.yaml` (root, NEW) | config (build-tool) | sqlc v2 config → engine postgresql → `internal/db/sqlc/` | NONE — greenfield | NONE → RESEARCH §Code Examples `sqlc.yaml` lines 866-891 |
| `.env.example` (root, NEW) | config (env template) | committed template; operator copies to `.env` | NONE — greenfield | NONE → RESEARCH §Code Examples `.env.example` lines 1044-1051 (Postgres block) |
| `scripts/restore_drill.sh` | docs-fixture / test-harness | pg_dump → pg_restore → assert < 90s | NONE — greenfield | NONE → RESEARCH §Code Examples `restore_drill.sh` lines 1074-1113 |

### Slice 0.7 — Neo4j (after Slice 0.5 Gate 2 green)

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `cmd/aura/main.go` (modified again) | cli-handler (dispatcher edit) | adds `case "neo4j"` | `cmd/aura/main.go:22-42` + Slice 0.5 edit | exact (self-extend) |
| `internal/config/config.go` (modified) | config | adds `Neo4j Neo4jConfig` field | self (Slice 0.5 form) | exact (self-extend) |
| `internal/knowledge/config.go` | config | env → `Neo4jConfig{BoltURL, User, Password, Database, MCPBinary, ConnectTimeoutSec, EmbedURL, EmbedDimensions}` | NONE — greenfield | NONE → RESEARCH §Recommended Project Structure `config.go ~40 LOC` |
| `internal/knowledge/client.go` | cli-handler (MCP subprocess wrapper) | `exec.CommandContext` → stdio JSON-RPC → Cypher(ctx, query, params) | NONE — greenfield | NONE → RESEARCH §Pattern 3 lines 434-534 |
| `internal/knowledge/migrate.go` | schema-migration runner (Cypher) | embed.FS *.cypher → SHA-256 → check audit → execute via MCP → write audit row | NONE — greenfield | NONE → RESEARCH §Pattern 4 lines 537-565 + §Recommended Project Structure `migrate.go ~120 LOC` |
| `internal/knowledge/migrations/0001_init.cypher` | schema-migration (Cypher) | `:Chunk(id)` UNIQUE constraint + chunk_embedding HNSW 768d cosine M=32 ef_construction=200 + chunk_text fulltext | NONE — greenfield | NONE → RESEARCH §Code Examples `0001_init.cypher` lines 836-860 |
| `internal/knowledge/ping.go` | cli-handler subcommand impl | MCP ping (RETURN 1, server version) + embed dim self-test via POST /v1/embeddings | NONE — greenfield | NONE → RESEARCH §Pattern 5 lines 568-622 (`PingEmbed` func) |
| `internal/knowledge/status.go` | cli-handler subcommand impl | query Postgres `aura.knowledge_migrations` via sqlc → tabulate | NONE — greenfield | NONE → RESEARCH §Recommended Project Structure `status.go ~40 LOC` |
| `internal/knowledge/reset.go` | cli-handler subcommand impl | DROP indexes + constraints + `MATCH (n) DETACH DELETE` via MCP + re-migrate | NONE — greenfield | NONE → RESEARCH §Recommended Project Structure `reset.go ~50 LOC` |
| `internal/knowledge/client_test.go` (`//go:build neo4j_integration`) | test-harness | goleak + MCP spawn + migrate + cypher MATCH + dim assert | NONE — greenfield | NONE → RESEARCH §Validation Architecture "INFRA-02" rows |
| `compose.yaml` (root, MODIFIED) | docker-infra | append neo4j + aura-llama-embed services | self (Slice 0.5 form) | exact (self-extend) |
| `Makefile` (root, MODIFIED) | makefile | append neo4j-up / neo4j-migrate / neo4j-status / neo4j-reset / smoke | self (Slice 0.5 form) | exact (self-extend) |
| `.env.example` (root, MODIFIED) | config (env template) | append `NEO4J_*`, `AURA_NEO4J_*`, `AURA_MCP_NEO4J_*`, `AURA_EMBED_*` | self (Slice 0.5 form) | exact (self-extend) |
| `scripts/neo4j_smoke.sh` | docs-fixture / test-harness | fixtures *.md → embed → upsert :Chunk → 5 queries → assert recall@5=5/5 + p95≤30ms | NONE — greenfield | NONE → RESEARCH §Code Examples `neo4j_smoke.sh` lines 1118-1182 |
| `scripts/fixtures/neo4j-smoke/0[1-5]_*.md` | docs-fixture | 5 short IT docs (amatriciana / duomo / panda / nome della rosa / espresso) | NONE — greenfield | NONE → CONTEXT.md §Specific Ideas + RESEARCH §Code Examples `queries.txt` lines 1186-1192 |
| `scripts/fixtures/neo4j-smoke/queries.txt` | docs-fixture | 5 `query|expected_id` lines | NONE — greenfield | NONE → RESEARCH §Code Examples `queries.txt` lines 1186-1192 |

---

## Pattern Assignments

### `cmd/aura/main.go` (cli-handler, CLI entry → subcommand dispatch) — THE ONE INTEGRATION POINT

**Analog:** self (`cmd/aura/main.go`, lines 22-42, the existing `switch os.Args[1]` block)

**Existing dispatcher** (`d:\Aura\cmd\aura\main.go:22-42`, verbatim):
```go
func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "tools":
		printTools()
	case "chat":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: aura chat <message>")
			os.Exit(1)
		}
		chatOnce(os.Args[2])
	case "shell", "serve":
		fmt.Println("TODO: implemented by the agent-loop and CLI slices")
	default:
		usage()
		os.Exit(1)
	}
}
```

**Existing usage line** (`d:\Aura\cmd\aura\main.go:44-46`):
```go
func usage() {
	fmt.Fprintln(os.Stderr, "usage: aura {serve|shell|chat <msg>|tools}")
}
```

**Planner instructions:**

1. **Slice 0.5 edit — preserve all existing cases.** Add ONE new case to the switch, in alphabetical order between `"chat"` and `"shell"/"serve"`:
   ```go
   case "db":
       runDB(os.Args[2:])      // Slice 0.5 — dispatch in cmd/aura/db.go
   ```
   Update `usage()` line to read: `usage: aura {serve|shell|chat <msg>|tools|db <sub>}`.

2. **Slice 0.7 edit — same surgical add.** Insert ONE more case between `"db"` and `"shell"/"serve"`:
   ```go
   case "neo4j":
       runNeo4j(os.Args[2:])   // Slice 0.7 — dispatch in cmd/aura/neo4j.go
   ```
   Update `usage()` line to read: `usage: aura {serve|shell|chat <msg>|tools|db <sub>|neo4j <sub>}`.

3. **DO NOT TOUCH** the existing `case "tools"`, `case "chat"`, `case "shell", "serve":` branches or the `buildRegistry()` / `printTools()` / `chatOnce()` / `stubClient` helpers. They belong to Phase 2/3 and are out of scope.

4. **Sub-dispatchers (`cmd/aura/db.go` and `cmd/aura/neo4j.go`) are NEW files.** Each owns its own inner `switch` over `{migrate, ping, status, reset}`. Pattern for each:
   ```go
   // cmd/aura/db.go (Slice 0.5) — NEW file
   func runDB(args []string) {
       if len(args) < 1 {
           fmt.Fprintln(os.Stderr, "usage: aura db {migrate|ping|status|reset}")
           os.Exit(1)
       }
       cfg, err := config.Load()
       if err != nil { fail(err) }
       switch args[0] {
       case "migrate":
           // uses cfg.DB.MigrateURL — empty → fail-fast per D-07
       case "ping":
           // uses cfg.DB.URL
       case "status":
           // uses cfg.DB.URL
       case "reset":
           // uses cfg.DB.MigrateURL + --yes guard
       default:
           fmt.Fprintln(os.Stderr, "unknown subcommand")
           os.Exit(1)
       }
   }
   ```

5. **godotenv load order:** Slice 0.5's `config.Load()` must `_ = godotenv.Load()` BEFORE reading env. Best-effort: missing `.env` is not an error in non-dev modes (Phase 0 Amendment #1 / PRD Slice 1 Open Question 1).

---

### `internal/config/config.go` (config, godotenv → root composite)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Architectural Responsibility Map row "Config loading" + CONTEXT.md decision row 66 ("`internal/config` stays a thin root composite `Config{LLM, DB, Neo4j, RunDir, ToolPreviewCap}`; per-subsystem config lives in the owning package").

**Concrete shape (Slice 0.5 form; Slice 0.7 appends the `Neo4j` field):**
```go
// Slice 0.5 form
package config

import (
    "github.com/chetto1983/aura/internal/db"
    "github.com/joho/godotenv"
)

type Config struct {
    DB             db.Config
    RunDir         string
    ToolPreviewCap int
    // Neo4j knowledge.Config       ← added in Slice 0.7
    // LLM    llm.Config              ← added in Phase 2 / Slice 1
}

func Load() (*Config, error) {
    _ = godotenv.Load() // best-effort; missing .env not fatal
    return &Config{
        DB: db.Config{
            URL:        os.Getenv("AURA_DB_URL"),
            MigrateURL: os.Getenv("AURA_DB_MIGRATE_URL"),
            // pool defaults populated by db package if zero
        },
        RunDir:         envDefault("AURA_RUN_DIR", defaultRunDir()),
        ToolPreviewCap: envIntDefault("AURA_CONTEXT_PREVIEW_CAP_BYTES", 2048),
    }, nil
}
```

**LOC budget:** ~110 (per RESEARCH §Recommended Project Structure). Fits one file; do NOT split.

**God-class guard:** Per CONTEXT.md row 66, **no subsystem fields directly under root** beyond the locked five (`LLM, DB, Neo4j, RunDir, ToolPreviewCap`). Future slices (`sandbox`, `web`, `cron`, `skills`) own their own config struct in their own package and are pulled in at subcommand-handler level, not here.

---

### `internal/db/config.go` (config, env → DBConfig)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Pattern 1 lines 344-350 (`Config` struct definition).

**Concrete shape:**
```go
package db

import "time"

type Config struct {
    URL             string        // role aura_app — runtime
    MigrateURL      string        // role aura_migrate — DDL only (D-07)
    MaxConns        int32         // default 10
    MinConns        int32         // default 1
    MaxConnIdleTime time.Duration // default 30s
}
```

**LOC budget:** ~40. One file.

---

### `internal/db/db.go` (pool-open + ping helper)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Pattern 1 `Open()` function lines 352-376 (verbatim-copyable).

**Concrete shape — Open + Ping + Close:**
```go
func Open(ctx context.Context, cfg *Config) (*pgxpool.Pool, error) {
    pc, err := pgxpool.ParseConfig(cfg.URL)
    if err != nil {
        return nil, fmt.Errorf("parse db url: %w", err)
    }
    if cfg.MaxConns > 0 { pc.MaxConns = cfg.MaxConns }
    if cfg.MinConns > 0 { pc.MinConns = cfg.MinConns }
    if cfg.MaxConnIdleTime > 0 { pc.MaxConnIdleTime = cfg.MaxConnIdleTime }
    pool, err := pgxpool.NewWithConfig(ctx, pc)
    if err != nil {
        return nil, fmt.Errorf("open pool: %w", err)
    }
    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        return nil, fmt.Errorf("ping: %w", err)
    }
    return pool, nil
}
```

**Pitfall #2 mitigation (RESEARCH §Common Pitfalls #2):** Wrap pgx errors with a `redactDSN()` helper so `POSTGRES_PASSWORD` doesn't leak into slog logs. Example:
```go
return nil, fmt.Errorf("open pool to %s: %w", redactDSN(cfg.URL), err)
```
Implement `redactDSN(s string) string` as a small private helper in this file (replaces `userinfo` password portion with `***`).

**LOC budget:** ~90. One file.

---

### `internal/db/migrate.go` (golang-migrate wrapper with embed.FS)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Pattern 2 lines 386-425 (verbatim-copyable).

**Concrete shape:**
```go
//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(ctx context.Context, migrateURL string) (int, error) {
    if migrateURL == "" {
        return 0, errors.New("AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17")
    }
    src, err := iofs.New(migrationsFS, "migrations")
    if err != nil { return 0, fmt.Errorf("source: %w", err) }
    m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL)
    if err != nil { return 0, fmt.Errorf("new migrator: %w", err) }
    defer m.Close()
    pre, _, _ := m.Version()
    if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
        return 0, fmt.Errorf("up: %w", err)
    }
    post, _, _ := m.Version()
    return int(post) - int(pre), nil
}
```

**D-07 enforcement:** the literal error message `AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17` is **load-bearing** — it is asserted verbatim by `TestMigrate_MissingURLFailsFast` (RESEARCH §Validation Architecture row INFRA-01 unit test). Do NOT paraphrase.

**LOC budget:** ~80. One file.

---

### `internal/db/migrations/0001_init.up.sql` + `.down.sql` (schema + roles)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Code Examples `0001_init.up.sql` lines 752-786 and `.down.sql` lines 790-806.

**Critical decisions enforced:**
- **DEFAULT PRIVILEGES (Pitfall #1):** Use `ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura GRANT ...` so future migrations inherit grants on tables created by `aura_migrate`.
- **No `:'aura_app_password'` psql variable** (RESEARCH §Code Examples note line 788) — golang-migrate's `iofs` driver does NOT support psql vars. Recommended path (per RESEARCH line 788): bake role creation into a Go-side bootstrap step OR use a templated DDL approach where the migrate runner reads `POSTGRES_PASSWORD` and substitutes via `os.Expand`. **Planner choice:** simplest is the Go-side bootstrap variant — `aura db migrate` reads env, executes role-creation DDL via pgxpool parametrized queries BEFORE running `m.Up()`. Document this in the migrate.go file header comment as "why not all DDL in the .sql file".
- **TRUNCATE / DROP not granted** to `aura_app` (CONTEXT.md row 63). Defense-in-depth via engine.

**LOC budget:** ~50 up / ~10 down.

---

### `internal/db/migrations/0002_knowledge_migrations.up.sql` + `.down.sql` (audit table)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Code Examples lines 810-828 and 833.

**Concrete shape:**
```sql
CREATE TABLE aura.knowledge_migrations (
    version    integer        PRIMARY KEY,
    name       text           NOT NULL,
    checksum   text           NOT NULL,                       -- SHA-256
    applied_at timestamptz    NOT NULL DEFAULT now()
);
GRANT SELECT, INSERT ON aura.knowledge_migrations TO aura_app;
GRANT ALL            ON aura.knowledge_migrations TO aura_migrate;
CREATE INDEX knowledge_migrations_applied_at_idx
    ON aura.knowledge_migrations (applied_at DESC);
```

**Critical:** the table is written by Slice 0.7's `aura neo4j migrate` (via Postgres `aura_app` role). The grant must include `INSERT` (CONTEXT.md row 65). Slice 0.7 cannot land before this migration is applied.

---

### `internal/db/queries/knowledge_migrations.sql` (sqlc source)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Pattern 4 lines 556-565.

**Concrete shape:**
```sql
-- name: RecordKnowledgeMigration :exec
INSERT INTO aura.knowledge_migrations (version, name, checksum)
VALUES ($1, $2, $3);

-- name: ListAppliedKnowledgeMigrations :many
SELECT version, name, checksum, applied_at
FROM aura.knowledge_migrations
ORDER BY version ASC;
```

**Slice boundary:** the FILE lands in Slice 0.5 commit (so sqlc generates the bindings); the CONSUMER (`internal/knowledge/migrate.go`) lands in Slice 0.7. This is the only cross-slice file dependency.

---

### `internal/db/sqlc/*.go` (generated)

**Analog:** NONE — greenfield (sqlc generates)

**Pattern source:** `make sqlc` invocation with `sqlc.yaml` from RESEARCH §Code Examples lines 866-891.

**Critical:** committed; CI golden test asserts `make sqlc && git diff --exit-code internal/db/sqlc/` exits 0 (RESEARCH §Validation Architecture INFRA-01 regression row).

---

### `internal/db/{ping,status,reset}.go` (subcommand impls)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Recommended Project Structure rows (`ping.go ~30 LOC`, `status.go ~50 LOC`, `reset.go ~40 LOC`).

**Acceptance contract (CONTEXT.md §Specifics row 178):**
- `aura db migrate` / `ping` / `status`: exit 0 on success, print human-readable + machine-parseable status line (e.g. `ok: 2 migrations applied (0001_init, 0002_knowledge_migrations)`). Exit non-zero on failure.
- `aura db status`: prints golang-migrate's own `schema_migrations` table.
- `aura db reset`: requires `--yes` flag (Makefile gates with `AURA_RESET_YES=1` env). DROP SCHEMA CASCADE → re-migrate. Dev only.

---

### `internal/db/db_test.go` (`//go:build db_integration`)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Validation Architecture "INFRA-01" rows + §Project Constraints "NO TEST ASILO NIDO" + RESEARCH §Sources `go.uber.org/goleak v1.3.0`.

**Mandatory structure:**
```go
//go:build db_integration

package db

import (
    "testing"
    "go.uber.org/goleak"
)

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}

func TestMigrate_Idempotent(t *testing.T)         { /* ... */ }
func TestMigrate_MissingURLFailsFast(t *testing.T) { /* asserts literal D-07 string */ }
func TestRoleSeparation_AppDenied(t *testing.T)    { /* aura_app TRUNCATE → permission denied */ }
func TestPing(t *testing.T)                        { /* SELECT 1 + latency */ }
func TestConfig_PoolParams(t *testing.T)           { /* MaxConns/MinConns/idle honored */ }
```

**Mock vs live container:** `TestMigrate_MissingURLFailsFast` and `TestConfig_PoolParams` are unit (no container needed); the rest need a live `docker compose up -d postgres`. CI flow: spin up postgres → `go test -tags db_integration -race ./internal/db/...`.

**LOC budget:** ~120. Single file is fine; split into `db_role_test.go` / `db_migrate_test.go` if it grows past 600.

---

### `compose.yaml` (root, NEW Slice 0.5; modified Slice 0.7)

**Analog:** NONE — greenfield (NB: legacy `sandbox/compose.yaml` ref in PRD is vestigial — D-02 amendment supersedes).

**Pattern source:** RESEARCH.md §Code Examples `compose.yaml` lines 893-976.

**Critical config points:**
- **Named volumes** for `aura-postgres`, `aura-neo4j`, `aura-neo4j-plugins`, `aura-llama-embed` (CONTEXT.md row 73 — no bind-mounts on Windows; memory `feedback_sqlite_wal_windows_corruption`).
- **Loopback-only port maps:** `127.0.0.1:5432:5432`, `127.0.0.1:7474:7474`, `127.0.0.1:7687:7687`, `127.0.0.1:8081:8081`.
- **Fail-fast env interpolation:** `${POSTGRES_PASSWORD:?POSTGRES_PASSWORD required in .env}`, same for `NEO4J_PASSWORD`.
- **Healthchecks** (CONTEXT.md row 68):
  - postgres → `pg_isready -U aura -d aura`
  - neo4j → `cypher-shell -u neo4j -p $$NEO4J_PASSWORD --database neo4j 'RETURN 1'` (Pitfall #3: NOT `nc -z 7687`; verified by RESEARCH A3)
  - aura-llama-embed → `curl -sf http://localhost:8081/health` (note: dim check NOT here — see Pattern 5 / Pitfall #5; container-level probe only verifies sidecar process is alive)
- **`-t 4`** on aura-llama-embed (mini-PC CPU budget memory `feedback_minipc_cpu_budget`).
- **`start_period: 40s`** on neo4j (APOC + GDS download); `start_period: 60s` on aura-llama-embed (first-boot HF model pull).

---

### `Makefile` (root, NEW Slice 0.5; modified Slice 0.7)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Code Examples `Makefile` lines 980-1040.

**Slice 0.5 targets:** `help`, `sqlc`, `db-up`, `db-migrate`, `db-status`, `db-reset` (guarded), `restore-drill`.

**Slice 0.7 appends:** `neo4j-up`, `neo4j-migrate` (depends on `db-migrate` because audit table needs to exist), `neo4j-status`, `neo4j-reset` (guarded), `smoke`.

**Windows note (CONTEXT.md row 117 / Pitfall #7):** Document at top of Makefile that targets must be run from PowerShell on Windows, OR each target with `docker compose run` prefixes `MSYS_NO_PATHCONV=1`. Phase 1 Makefile targets do NOT use `docker compose run` (they use `docker compose up -d` + `go run`), so this is mostly informational; the constraint binds only on `restore_drill.sh` if it shells via docker.

---

### `sqlc.yaml` (root, NEW)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Code Examples `sqlc.yaml` lines 866-891 (verbatim).

**Locked options (CONTEXT.md row 60):** engine postgresql, json_tags=snake, emit_interface, emit_exact_table_names, output `internal/db/sqlc/`.

---

### `.env.example` (root, NEW Slice 0.5; modified Slice 0.7)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Code Examples `.env.example` lines 1044-1070.

**Slice 0.5 keys:** `POSTGRES_PASSWORD`, `AURA_DB_URL`, `AURA_DB_MIGRATE_URL`.

**Slice 0.7 appended keys:** `NEO4J_USER`, `NEO4J_PASSWORD`, `AURA_NEO4J_BOLT_URL`, `AURA_NEO4J_DATABASE` (defaults to `neo4j` per Pitfall #8), `AURA_MCP_NEO4J_CYPHER_BIN`, `AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC`, `AURA_EMBED_BASE_URL`, `AURA_EMBED_DIMENSIONS`.

**Placeholder convention:** all secrets default to `changeme` with inline `# required, must change` comments. `.env` itself is gitignored per existing `.gitignore`.

---

### `scripts/restore_drill.sh` (Slice 0.5)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Code Examples `restore_drill.sh` lines 1074-1113 (verbatim-copyable).

**Acceptance:** ROADMAP Phase 1 SC#3 — restore < 90s. Script asserts via `ELAPSED_MS` comparison; exits non-zero on overrun.

---

### `internal/knowledge/config.go` (Slice 0.7)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Recommended Project Structure row + §Pattern 3 `Open()` signature.

**Concrete shape:**
```go
package knowledge

type Config struct {
    BoltURL            string        // bolt://127.0.0.1:7687
    User               string        // "neo4j"
    Password           string        // ${NEO4J_PASSWORD}
    Database           string        // "neo4j" (Community single-DB; Pitfall #8)
    MCPBinary          string        // "mcp-neo4j-cypher" on PATH
    ConnectTimeoutSec  int           // default 10
    EmbedURL           string        // http://127.0.0.1:8081
    EmbedDimensions    int           // 768 (Amendment #18 contract)
}
```

**LOC budget:** ~40. One file.

---

### `internal/knowledge/client.go` (MCP subprocess wrapper)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Pattern 3 lines 434-534 (verbatim-copyable shape).

**Key load-bearing details:**
- **`exec.CommandContext`** + stdio pipes (`StdinPipe` / `StdoutPipe`).
- **Mutex-serialized JSON-RPC 2.0 envelope** — MCP stdio is single-pipe; concurrent calls would interleave.
- **`atomic.Int64` request ID counter.**
- **Fail-fast on spawn failure** with install hint: `"spawn %s: %w (PATH check: pip install mcp-neo4j-cypher==0.6.0)"`.
- **D-06 policy** in send/recv error path: error message references "MCP may have crashed — D-06 policy: fail Aura process". No restart, no graceful degrade.
- **`Close()`** sends EOF on stdin + waits for child exit.

**Assumption A10 risk:** the JSON-RPC envelope shape (`{"jsonrpc":"2.0","id":N,"method":"tools/call","params":{...}}`) is not exercised by hands-on probe in this research session. **Planner instruction:** Slice 0.7 Wave 0 must include a one-shot manual probe task — spawn `mcp-neo4j-cypher --transport stdio` against a running Neo4j container, send a literal `tools/call` request, capture the response shape, and align the Go decoder accordingly before merging.

**LOC budget:** ~120. One file. If it grows past 600 (won't in Phase 1), split into `client.go` (lifecycle) + `client_rpc.go` (envelope) per CLAUDE.md god-class ban.

---

### `internal/knowledge/migrate.go` (Cypher migration runner)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Pattern 4 lines 537-565 (audit table + sqlc queries pattern).

**Required structure:**
```go
//go:embed migrations/*.cypher
var cypherFS embed.FS

func Migrate(ctx context.Context, mcp *Client, pg *pgxpool.Pool) (int, error) {
    // 1. Read embed.FS, sort numerically by filename prefix.
    // 2. For each file: compute SHA-256 of body.
    // 3. Query Postgres aura.knowledge_migrations (via sqlc generated
    //    ListAppliedKnowledgeMigrations) — skip if version already applied.
    // 4. Verify checksum mismatch is treated as ERROR (history corruption).
    // 5. Execute via mcp.Cypher(ctx, body, nil, write=true).
    // 6. On success, call generated RecordKnowledgeMigration via aura_app role
    //    (this is INSERT-only, granted to aura_app in 0002).
    // 7. Return count of newly-applied.
}
```

**Slice boundary contract:** consumes `internal/db/sqlc/knowledge_migrations.sql.go` (generated in Slice 0.5). Cross-slice import is acceptable — knowledge depends on db's generated client.

**LOC budget:** ~120. One file.

---

### `internal/knowledge/migrations/0001_init.cypher` (HNSW + fulltext)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Code Examples `0001_init.cypher` lines 836-860 (verbatim-copyable).

**D-08 scope lock:** ONLY `:Chunk(id)` UNIQUE constraint + `chunk_embedding` HNSW 768d cosine M=32 ef_construction=200 + `chunk_text` fulltext. NO `:Document`, `:Entity`, etc. — those belong to Slice 11a.

**A7 risk:** `vector.dimensions: 768` is hard-coded in the Cypher. RESEARCH note line 862 documents this is acceptable for D-08 minimal-schema scope. Future amendment #18 swap runbook (Slice 11b+) may template via `os.Expand`; out of scope here.

**Pitfall #4 mitigation:** Neo4j 5.26 supports `IF NOT EXISTS` on all three statements; the migration is idempotent at the Cypher level (belt + suspenders on top of the audit row check).

---

### `internal/knowledge/ping.go` (server-version + dim self-test)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Pattern 5 `PingEmbed` function lines 587-621 (verbatim-copyable).

**Two sub-checks:**
1. **MCP ping:** `mcp.Cypher(ctx, "RETURN gds.version() AS version", nil, write=false)` — returns Neo4j server version. If it returns 0 rows, MCP subprocess isn't really talking to Neo4j.
2. **Embed dim self-test (Pattern 5 / Pitfall #5):** POST `{"input":"ping","model":"embedding"}` to `cfg.EmbedURL + "/v1/embeddings"`, decode response, assert `len(data[0].embedding) == cfg.EmbedDimensions`.

**Error message contract (Pitfall #5, RESEARCH §Validation Architecture INFRA-02 error-handling row):**
```go
return fmt.Errorf(
    "embedding sidecar returned dim=%d but AURA_EMBED_DIMENSIONS=%d — refuse to start (Pitfall #7 silent corruption); see prd.md amendment #18 swap runbook",
    actual, expectedDim)
```
This message is asserted verbatim by `TestPingEmbed_DimMismatch`. Do NOT paraphrase.

**PRD amendment side-effect (RESEARCH §Summary risk 1):** PRD acceptance row 182 claims sidecar `/health` returns `{"dim":768}`. Ground truth: it returns `{"status":"ok"}`. Slice 0.7 commit must ship a one-line PRD amendment correcting acceptance row 182 to reference the Go-side `/v1/embeddings` probe.

**LOC budget:** ~80. One file.

---

### `internal/knowledge/{status,reset}.go`

**Analog:** NONE — greenfield

**status.go:** queries Postgres `aura.knowledge_migrations` via sqlc (`ListAppliedKnowledgeMigrations` from Slice 0.5 generated code), tabulates. ~40 LOC.

**reset.go:** drops all indexes + constraints via MCP, then `MATCH (n) DETACH DELETE`, then re-runs `Migrate()`. Guarded by `--yes` flag + `AURA_RESET_YES=1` env (consistent with `db reset`). Dev only. ~50 LOC.

---

### `internal/knowledge/client_test.go` (`//go:build neo4j_integration`)

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Validation Architecture "INFRA-02" rows.

**Mandatory structure:**
```go
//go:build neo4j_integration

package knowledge

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}

func TestPing_ReturnsServerVersion(t *testing.T)        { /* MCP RETURN 1 + server version 5.26.x */ }
func TestPingEmbed_DimMismatch(t *testing.T)            { /* mocked /v1/embeddings → 384d → exact error msg */ }
func TestPingEmbed_Live(t *testing.T)                   { /* live aura-llama-embed → dim 768 */ }
func TestCypherMigrate_Idempotent(t *testing.T)         { /* re-run = 0 newly-applied */ }
func TestCypherMigrate_WritesAuditRow(t *testing.T)     { /* checks aura.knowledge_migrations row */ }
func TestMCPCrash_FailsAura(t *testing.T)               { /* cmd.Process.Kill → next call returns wrapped err */ }
func TestInitCypher_AllArtifactsPresent(t *testing.T)   { /* SHOW INDEXES + SHOW CONSTRAINTS */ }
```

**Build tag:** `//go:build neo4j_integration` (some tests also need `db_integration` for the audit-row assertion; use the combined tag `//go:build neo4j_integration && db_integration` in files that need both).

**LOC budget:** ~120 in one file. Split into `client_test.go` (MCP lifecycle) + `migrate_test.go` (Cypher migration + audit) + `ping_test.go` (dim assertion) if it grows.

---

### `scripts/neo4j_smoke.sh` + `scripts/fixtures/neo4j-smoke/*.md` + `queries.txt`

**Analog:** NONE — greenfield

**Pattern source:** RESEARCH.md §Code Examples `neo4j_smoke.sh` lines 1118-1182 + `queries.txt` lines 1186-1192 (both verbatim-copyable).

**Fixture content (CONTEXT.md §Specifics row 177):** five short Italian docs:
- `01_amatriciana.md` — breve nota sulla pasta amatriciana
- `02_duomo_milano.md` — descrizione del Duomo di Milano
- `03_fiat_panda.md` — scheda Fiat Panda 30 history
- `04_nome_della_rosa.md` — sinossi *Il nome della rosa* di Eco
- `05_espresso_napoletano.md` — note sul caffè espresso napoletano

**queries.txt format:** `query|expected_id` per line (RESEARCH §Code Examples lines 1186-1192).

**Acceptance contract (ROADMAP Phase 1 SC#5):** recall@5 = 5/5 + p95 ≤ 30ms.

**Dependency:** `jq` (RESEARCH §Environment Availability — fallback documented as `choco install jq` / `pacman -S jq`).

---

## Shared Patterns

### Cross-cutting: error wrapping with redacted secrets

**Source:** RESEARCH.md §Pitfall #2 + §Security Domain V7

**Apply to:** every file in `internal/db/*.go` and `internal/knowledge/*.go` that wraps a pgx or MCP error.

**Concrete:**
```go
// in internal/db/db.go (private helper, reused by ping/status/reset)
func redactDSN(s string) string {
    if u, err := url.Parse(s); err == nil && u.User != nil {
        u.User = url.UserPassword(u.User.Username(), "***")
        return u.String()
    }
    return "<unparseable-dsn>"
}

// usage in any error wrap:
return nil, fmt.Errorf("open pool to %s: %w", redactDSN(cfg.URL), err)
```

The same redaction approach applies to `AURA_NEO4J_BOLT_URL` if it ever embeds credentials (currently it doesn't, but be defensive).

### Cross-cutting: literal error messages are LOAD-BEARING

**Source:** RESEARCH.md §Validation Architecture INFRA-01/02 error-handling rows + CONTEXT.md D-07.

**Apply to:** `internal/db/migrate.go` (empty MigrateURL) and `internal/knowledge/ping.go` (dim mismatch).

These are NOT prose:
- `AURA_DB_MIGRATE_URL required for DDL operations — see prd.md amendment #17`
- `embedding sidecar returned dim=%d but AURA_EMBED_DIMENSIONS=%d — refuse to start (Pitfall #7 silent corruption); see prd.md amendment #18 swap runbook`
- `spawn %s: %w (PATH check: pip install mcp-neo4j-cypher==0.6.0)`

Tests will `assert.Contains(err.Error(), "AURA_DB_MIGRATE_URL required")` — paraphrasing breaks the test. Implement them verbatim.

### Cross-cutting: build-tag integration tests with goleak

**Source:** RESEARCH.md §Pattern + §Validation Architecture + CONTEXT.md row 75.

**Apply to:** `internal/db/db_test.go` and `internal/knowledge/client_test.go`.

```go
//go:build db_integration       // or neo4j_integration
package <pkg>

import (
    "testing"
    "go.uber.org/goleak"
)

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

CI invocation: `go test ./... -tags 'db_integration neo4j_integration' -race -count=1`. Unit tests (no tag) run in every PR; integration tests run when sidecars are available.

### Cross-cutting: deferred-tool pattern is irrelevant for Phase 1

**Source:** `internal/agent/tools/spec.go:18-25` (Spec struct), `internal/agent/tools/spec.go:1-11` (package doc), CONTEXT.md row 151.

**Existing pattern (verbatim from `d:\Aura\internal\agent\tools\spec.go:18-25`):**
```go
type Spec struct {
    Name        string
    Summary     string          // one line, always shown in the manifest
    Description string          // full description; only shown when not Deferred OR after a tool_search hit
    Parameters  json.RawMessage // JSON-schema for the tool arguments
    Deferred    bool            // true → full spec hidden until tool_search loads it
}
```

**Phase 1 contract:** Phase 1 introduces ZERO new `tools.Tool` implementations. This pattern is informational only — listed so the planner knows NOT to wire `db`/`neo4j` subcommands as deferred LLM tools. They are CLI subcommands routed through `os.Args` in `cmd/aura/main.go`, NOT LLM-facing tools. Future phases (Phase 4+) will introduce `knowledge`-facing tools (Cypher exec, schema introspect); those will use the deferred-tool pattern.

### Cross-cutting: env var naming `AURA_<DOMAIN>_<UNIT>`

**Source:** CLAUDE.md §Env vars + RESEARCH.md §Project Constraints "ENV VAR CONVENTION" + CONTEXT.md row 107.

**Apply to:** every new env var in `.env.example` and every `os.Getenv` call.

- Aura-controlled: `AURA_DB_URL`, `AURA_DB_MIGRATE_URL`, `AURA_NEO4J_BOLT_URL`, `AURA_NEO4J_DATABASE`, `AURA_MCP_NEO4J_CYPHER_BIN`, `AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC`, `AURA_EMBED_BASE_URL`, `AURA_EMBED_DIMENSIONS`, `AURA_RUN_DIR`, `AURA_CONTEXT_PREVIEW_CAP_BYTES`.
- Upstream-canonical (NO `AURA_` prefix): `POSTGRES_PASSWORD`, `NEO4J_USER`, `NEO4J_PASSWORD`.
- Unit suffix mandatory on caps: `_BYTES`, `_SEC`, `_MS`. Example: `AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC=10`.

### Cross-cutting: post-edit Gate 2 validation

**Source:** CLAUDE.md §Post-edit validation + RESEARCH.md §Project Constraints.

**Apply to:** every Go file edit in this phase.

```bash
go vet ./...
go build ./...
go test ./internal/<package>/
go test -race ./internal/<package>/
```

Slice 0.5 Gate 2 narrows to `./internal/db/... ./internal/config/...`. Slice 0.7 Gate 2 expands to `./internal/knowledge/...`. Phase gate (after both slices) runs the integration-tagged suite.

### Cross-cutting: god-class ban (≤ 600 LOC)

**Source:** CLAUDE.md §Behavioral rules + CONTEXT.md row 77.

**Apply to:** every file in this phase. Biggest single file projected is `internal/knowledge/client.go` at ~120 LOC (well under 600). If any file grows past 400 LOC during implementation, the executor refactors **in the same commit** per `feedback_one_module_per_slice`.

---

## No Analog Found

**All Phase 1 files except `cmd/aura/main.go` extension.** This is by design — Phase 1 is a greenfield substrate phase. Pattern sources live in RESEARCH.md §Code Examples and §Architecture Patterns sections, not in the existing codebase.

| Category | Files | Pattern source |
|----------|-------|----------------|
| Postgres pool / migrate | `internal/db/*.go`, `internal/db/migrations/*.sql` | RESEARCH §Pattern 1, §Pattern 2, §Code Examples 0001/0002 |
| sqlc | `sqlc.yaml`, `internal/db/queries/*.sql`, `internal/db/sqlc/*.go` (generated) | RESEARCH §Code Examples `sqlc.yaml`, §Pattern 4 |
| Neo4j MCP subprocess | `internal/knowledge/*.go` | RESEARCH §Pattern 3, §Pattern 4, §Pattern 5 |
| Cypher migration | `internal/knowledge/migrations/0001_init.cypher` | RESEARCH §Code Examples 0001_init.cypher |
| Compose / Makefile / .env | `compose.yaml`, `Makefile`, `.env.example` | RESEARCH §Code Examples respective sections |
| Smoke + restore scripts | `scripts/restore_drill.sh`, `scripts/neo4j_smoke.sh`, `scripts/fixtures/neo4j-smoke/*` | RESEARCH §Code Examples respective sections + CONTEXT.md §Specific Ideas |
| Config composite | `internal/config/config.go` | RESEARCH §Architectural Responsibility Map "Composition" + CONTEXT.md row 66 |

**Planner consumes RESEARCH.md directly** for each of the above. The skeletons are compilable shapes (not pseudo-code) per RESEARCH §Metadata confidence breakdown.

---

## Metadata

**Analog search scope:**
- `d:\Aura\cmd\aura\main.go` (90 LOC — read fully)
- `d:\Aura\internal\agent\loop.go` (131 LOC — read fully)
- `d:\Aura\internal\llm\client.go` (78 LOC — read fully)
- `d:\Aura\internal\agent\tools\spec.go` (61 LOC — read fully)
- `d:\Aura\go.mod` (read fully — confirms `go 1.26.0` and pre-staged Phase 1 deps as `// indirect`)
- (Not read: `internal/agent/tools/{manifest,search,text_response}.go`, `internal/sandbox/sandbox.go`, `internal/swarm/swarm.go` — none touch DB, Neo4j, MCP, config, migrations, sqlc, or compose; per CONTEXT.md row 138-140 these are untouched in Phase 1)

**Files scanned (Glob/Grep):** 0 wider scan — the codebase tree was already enumerated in CONTEXT.md `<code_context>` block and STRUCTURE.md §Current State. Existing 633-LOC skeleton fully accounted for.

**Cross-references verified:**
- `cmd/aura/main.go:22-42` switch dispatcher — verified by direct read.
- `internal/agent/tools/spec.go:18-25` Spec struct — verified by direct read.
- `go.mod` deps and Go version — verified by direct read (note: actual `go 1.26.0` vs RESEARCH-claimed `1.25.0`; both ≥ Phase 0 Amendment #1 floor).

**Pattern extraction date:** 2026-05-29
