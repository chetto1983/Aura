# Audit: internal/db

**Verdict:** needs-work — two real bugs (version-count arithmetic, hardcoded DB name in EnsureRoles) and no races or dead code.

**Counts:** critical 0 / high 1 / medium 1 / low 1

---

## Findings

### [HIGH][BUG] Migrate() counts migrations by version-number difference, not by actual migration count

**Location:** `internal/db/migrate.go:52-57`

**Confidence:** high

**Detail:**

```go
pre, _, _ := m.Version()      // uint: last applied version number (0 if ErrNilVersion on fresh DB)
...m.Up()...
post, _, _ := m.Version()     // uint: last applied version number after Up
return int(post) - int(pre), nil
```

`m.Version()` returns the file-version number of the last applied migration (e.g., `12` after applying `0012_telegram.up.sql`), not a sequential count of applied files. The subtraction `int(post) - int(pre)` gives the difference between version numbers, which equals the number of migrations applied only when numbering is perfectly sequential and gapless.

Current state (0001–0012) happens to be sequential so the test at `db_test.go:164` passes. But the contract is wrong: if the next migration is `0014` (skipping `0013`), then after applying it, `post=14, pre=12, diff=2`, but only one migration was applied. The caller at `cmd/aura/db.go` prints `"applied N migrations"` — it would print `2` when only `1` was applied.

The error-case is also silently swallowed: when the DB is fresh, `m.Version()` returns `(0, false, migrate.ErrNilVersion)`. The `_` discards this error and `pre=0` happens to be the correct baseline, but only by the accident of zero-value matching the desired "no migrations" baseline.

**Suggested fix:**

Count applied migrations before and after by counting rows in `schema_migrations`, or use a dedicated counter driven by the source. The simplest correct approach:

```go
// Count *.up.sql entries in the source before and after is fragile.
// Instead, query the tracker directly after Up:
var priorCount, postCount int
// pool.QueryRow(ctx, "SELECT count(*) FROM public.schema_migrations").Scan(&priorCount)
// m.Up()
// pool.QueryRow(ctx, "SELECT count(*) FROM public.schema_migrations").Scan(&postCount)
// return postCount - priorCount, nil
```

Alternatively, if the sequential-version invariant is intended to be maintained forever, document it explicitly in a code comment AND add a CI check that verifies no gaps exist in the migration file sequence.

---

### [MEDIUM][BUG] EnsureRoles hardcodes database name "aura" in GRANT statement

**Location:** `internal/db/migrate.go:114`

**Confidence:** high

**Detail:**

```go
if _, err := pool.Exec(ctx, "GRANT CREATE ON DATABASE aura TO aura_migrate"); err != nil {
```

The pool is opened against `bootstrapURL`, which the caller controls. The database name in the GRANT is hardcoded as `"aura"` regardless of what database `bootstrapURL` points to. If `bootstrapURL` connects to a cluster where the application database is named differently (e.g., `aura_staging`), the GRANT lands on the wrong database or fails with "database does not exist", while the schema and public grants further below use the connected database (executing `CREATE SCHEMA` in the connected DB).

This inconsistency is self-concealing in the standard local-dev path because `bootstrapURL` always connects to `aura`. The test `TestMigrate_Phase4_AppliesAndSeeds` avoids this path entirely — it issues the grant manually (`admin.Exec(ctx, "GRANT CREATE ON DATABASE "+freshDB+" TO aura_migrate")`) instead of calling `EnsureRoles` for the fresh database.

**Suggested fix:**

Extract the database name from `bootstrapURL` at the top of `EnsureRoles` using `url.Parse`, validate it is non-empty, and substitute it into the GRANT statement:

```go
u, _ := url.Parse(bootstrapURL)
dbName := strings.TrimPrefix(u.Path, "/")
pool.Exec(ctx, fmt.Sprintf("GRANT CREATE ON DATABASE %s TO aura_migrate", pgIdentQuote(dbName)))
```

Where `pgIdentQuote` double-quotes and escapes the identifier to prevent SQL injection via a crafted DB name.

---

### [LOW][BUG] m.Version() error silently discarded — dirty-migration baseline is invisible

**Location:** `internal/db/migrate.go:52` and `internal/db/migrate.go:56`, same pattern in `reset.go:N/A` (Reset does not call Version)

**Confidence:** medium

**Detail:**

```go
pre, _, _ := m.Version()
```

`m.Version()` returns `(version uint, dirty bool, err error)`. Three values are discarded: the dirty flag and the error. On a fresh database `err = migrate.ErrNilVersion` and `version = 0`, which is the correct pre-Up baseline by coincidence. But `dirty = true` (a prior interrupted migration) is silently dropped — if `pre` version is dirty, `m.Up()` will fail with an "error: Dirty database version N. Fix and force version." error, which is correctly propagated. The issue is that the returned count is still `int(post) - int(pre)` where `post = 0` (Up failed), so `Migrate` returns `(0, error)`. The count is never used in the error path, so this is benign in practice.

However the dirty flag discard means callers have no way to distinguish "no migrations applied because database is already at target" from "no migrations applied because `m.Version()` returned a non-ErrNilVersion error (connection problem) and pre defaulted to 0". Both return a count of 0 on success. This could mask a misconfigured URL that gets its `Version()` call to fail (returning `pre=0`) just before `m.Up()` succeeds against a different backend.

**Suggested fix:**

Log (via `slog.Warn`) when `err != nil && !errors.Is(err, migrate.ErrNilVersion)` on the `pre` version call; treat the pre-version error as a baseline ambiguity rather than a silent 0. At minimum, change the comment to document that `ErrNilVersion` is expected and other errors are ignored.

---

## What was checked and found clean

- **Nil pointer derefs**: `Open`, `Ping`, `Status` all guard against nil `cfg`/`pool` inputs at the function entry point. `WithTx` has no nil pool guard but pool.Begin will return a descriptive error immediately; no panic path.
- **Resource leaks**: All `pool.Query` call sites (`Status`, all sqlc-generated `:many` queries) have `defer rows.Close()`. `pgxpool.Pool.Close()` is the caller's responsibility (open/close at the cmd layer), not the package's. `pgxpool.New` in `EnsureRoles` has `defer pool.Close()`. The migrate/reset migrators use `defer func() { _, _ = m.Close() }()` consistently.
- **Unchecked errors**: All `pool.QueryRow.Scan`, `pool.Query`, `pool.Exec` results are checked. The `m.Close()` error is intentionally discarded (golang-migrate convention; the resource is being released, not committed). `tx.Rollback` in `WithTx` is intentionally fire-and-forget (best-effort cleanup; the original error is already set).
- **WithTx panic recovery**: The named-return + defer pattern is correctly structured. A panic inside `fn` triggers Rollback and re-panics; a nil fn-return triggers Commit and its error replaces nil; a non-nil fn-return triggers Rollback and returns fn's error.
- **Races**: No shared mutable state in this package. `redactDSN`, `redactDSNUsername`, `redactErrorPassword` are stateless pure functions. No goroutines are spawned. `pgxpool` is concurrency-safe by design. No maps or slices with concurrent access.
- **Dead code**: All exported functions (`Open`, `Migrate`, `MigrateSteps`, `Reset`, `EnsureRoles`, `Ping`, `Status`, `WithTx`, `MigrationRow`, `Config`) are referenced by at least one non-test caller in the repo. Unexported functions (`redactDSN`, `redactDSNUsername`, `redactErrorPassword`, `isUndefinedTable`) are all called within the package. The sqlc-generated `(*Queries).WithTx` method is generated code and not flagged.
- **Not-wired code**: `MigrateSteps` is called only from `internal/skills/audit_store_integration_test.go` (a test) and not from any production path. This is intentional — it is the reversibility seam for integration tests.
- **SQL correctness**: `SearchConversationTurns` correctly filters NULL-content rows via `WHERE content % $1` (trigram NULL comparison returns NULL/false). `LockConversationForTurnAppend` + `NextConversationTurnSeq` are always called together inside a `db.WithTx`, so seq allocation is serialized by the `SELECT FOR UPDATE` lock. `ON CONFLICT DO NOTHING` on `InsertCacheMetric` and `InsertToolInvocation` is intentional idempotency (documented).
- **Password redaction**: `redactDSN` correctly handles empty string, unparseable URL, no userinfo, no password, and password-present cases. `redactErrorPassword` correctly skips empty password entries and returns the original error unchanged when no scrubbing is needed (preserving the original error identity for `errors.Is`/`errors.As` chains when no substitution occurs).
