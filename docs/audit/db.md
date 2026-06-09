# Audit: internal/db

**Verdict:** needs-work — two real defects (misplaced doc comment, error-chain break on redaction), one type-safety smell in generated code, all low-to-medium severity; no critical or high findings.

**Counts:** critical 0 / high 0 / medium 2 / low 1

---

## Findings

### [MEDIUM][BUG] Misplaced doc comment: `redactErrorPassword` comment documents `databaseNameFromDSN`

**Location:** `internal/db/migrate.go:158-162`

**Confidence:** high

**Detail:**
Lines 158-161 are a Go doc comment that begins `// redactErrorPassword scrubs known passwords…`. In Go, a doc comment attaches to the _immediately following_ declaration. The immediately following declaration is `func databaseNameFromDSN(dsn string) (string, error)` (line 162). As a result:
- `go doc` and IDEs display `redactErrorPassword scrubs…` as the documentation for `databaseNameFromDSN`.
- The actual `redactErrorPassword` function (line 182) has no doc comment at all.

This is a cut-paste error: the comment block was written for `redactErrorPassword` but was placed one function too early, likely when the helper functions were reordered. The mislabelling is confirmed by reading the comment text: it accurately describes `redactErrorPassword`'s scrub-and-replace logic, not `databaseNameFromDSN`'s path-extraction logic.

**Suggested fix:**
Move the comment block to immediately precede `func redactErrorPassword(…)` at line 182, and add a separate concise comment for `databaseNameFromDSN`:

```go
// databaseNameFromDSN extracts the database name component from a Postgres DSN.
func databaseNameFromDSN(dsn string) (string, error) { … }

// redactErrorPassword scrubs known passwords from an error message before it
// is wrapped. Defense-in-depth on top of parametrized queries — if a Postgres
// error message ever echoes a literal substring of the password (e.g. via
// an unexpected upstream code path), this strips it.
func redactErrorPassword(err error, passwords ...string) error { … }
```

---

### [MEDIUM][BUG] `redactErrorPassword` silently breaks the error chain when redaction fires

**Location:** `internal/db/migrate.go:182-197`, call sites `migrate.go:118,126,134,137,144,153`

**Confidence:** high

**Detail:**
`redactErrorPassword` has two return paths:

1. No password found in error text → returns the **original** `err` unchanged (line 194). Wrapping with `%w` preserves the full `pgconn.PgError` chain.
2. Password found and replaced → returns `errors.New(msg)` (line 196). This is an opaque error with no `Unwrap()` method; it is **not** the original Postgres error. The `%w` in every call site (`fmt.Errorf("…: %w", redactErrorPassword(err, password))`) then wraps this opaque value, not the original, permanently destroying the error chain.

Consequence: any downstream consumer that calls `errors.As(err, &pgconn.PgError{})` on an EnsureRoles error will silently get `false` when the password happened to appear in the error message (e.g., a malformed ALTER ROLE that echoes the literal). The error remains propagated as a string — no silent swallow — but the structured SQLSTATE is irrecoverable.

No current caller of `EnsureRoles` uses `errors.Is`/`errors.As`, so this is latent rather than actively triggered. However, the `%w` verb's stated purpose (preserving structure) is defeated whenever redaction fires, making the wrapping misleading.

**Suggested fix:**
Return a sentinel error type that wraps the original while substituting the message string, so the `pgconn.PgError` remains unwrappable:

```go
type redactedError struct {
    msg  string
    orig error
}

func (e *redactedError) Error() string  { return e.msg }
func (e *redactedError) Unwrap() error  { return e.orig }

func redactErrorPassword(err error, passwords ...string) error {
    if err == nil {
        return nil
    }
    msg := err.Error()
    for _, p := range passwords {
        if p == "" {
            continue
        }
        msg = strings.ReplaceAll(msg, p, "***")
    }
    if msg == err.Error() {
        return err
    }
    return &redactedError{msg: msg, orig: err}
}
```

---

### [LOW][BUG] `AggregateCacheMetricsSinceRow` uses `interface{}` for aggregate columns, defeating compile-time type safety

**Location:** `internal/db/sqlc/cache_metrics.sql.go:23-28`

**Confidence:** high

**Detail:**
The generated struct for the `AggregateCacheMetricsSince` query has three `interface{}` fields:

```go
type AggregateCacheMetricsSinceRow struct {
    Turns             int64       `json:"turns"`
    TotalPromptTokens interface{} `json:"total_prompt_tokens"`
    TotalCachedTokens interface{} `json:"total_cached_tokens"`
    TotalCostUsd      interface{} `json:"total_cost_usd"`
}
```

This is a known sqlc limitation with `COALESCE(SUM(...), 0)` aggregates that mix numeric types in a way the generator cannot statically resolve. The `cachemetrics` package compensates via the `anyInt64`/`anyNumericFloat` type-switch helpers (`internal/cachemetrics/store_helpers.go`), which handle multiple pgx runtime shapes.

The risk is: any future direct consumer of `AggregateCacheMetricsSinceRow` that accesses these fields without going through `anyInt64` will compile fine but panic at runtime on unexpected type assertions. The file carries a `DO NOT EDIT` header, so the fix belongs in the sqlc query configuration.

**Suggested fix:**
In the sqlc query configuration (`internal/db/queries/cache_metrics.sql` or equivalent), cast the aggregate expressions to typed aliases so sqlc can infer a concrete type:

```sql
SELECT count(*)                                        AS turns,
       coalesce(sum(prompt_tokens), 0)::bigint         AS total_prompt_tokens,
       coalesce(sum(cached_tokens), 0)::bigint         AS total_cached_tokens,
       coalesce(sum(cost_usd),      0)::numeric(10,4)  AS total_cost_usd
FROM aura.cache_metrics
WHERE ts >= $1::timestamptz
```

After regeneration, the struct fields would be `int64` / `pgtype.Numeric`, eliminating the `interface{}` escape hatch and the `anyInt64`/`anyNumericFloat` helpers.

---

## What was checked and found clean

- **Resource leaks**: All `rows.Close()` calls are deferred immediately after `Query`. All migration `m.Close()` calls are in deferred closures. `pgxpool.Pool.Close()` is called in all error-exit and deferred paths. No resource leak found.
- **Context propagation**: Every public function accepts and threads `ctx` through all DB calls. No `context.Background()` injection inside library code.
- **Nil-pointer risks**: `Open`, `Ping`, `Status`, `WithTx` all guard against `nil` pool/config arguments with explicit fast-fail errors.
- **SQL injection**: All parameterized queries use `$N` placeholders. The only dynamic SQL construction is in `EnsureRoles` (CREATE/ALTER ROLE with inlined password) — this is explicitly documented and defended by `strings.ReplaceAll(password, "'", "''")` SQL-literal escaping, plus the `redactErrorPassword` defense-in-depth. Database and role names in `grantCreateDatabaseSQL` use `quoteIdent` (double-quote escaping). The role names are hardcoded constants, not user input.
- **Race conditions**: No shared mutable state in the package. The `pgxpool.Pool` is goroutine-safe per pgx documentation. No maps, slices, or structs with concurrent write access without synchronization.
- **Dead code**: All unexported functions (`redactDSN`, `redactDSNUsername`, `redactErrorPassword`, `databaseNameFromDSN`, `grantCreateDatabaseSQL`, `quoteIdent`, `isUndefinedTable`, `migrateAllCountingSteps`) are referenced by package-internal callers or tests. `migrationStepper` interface is used by `migrateAllCountingSteps`. `MigrateSteps` is referenced by `internal/skills/audit_store_integration_test.go`. No dead code found.
- **Not-wired code**: All exported functions (`Open`, `Migrate`, `MigrateSteps`, `Reset`, `Status`, `Ping`, `EnsureRoles`, `WithTx`) are called from at least one non-test production caller. All sqlc-generated query methods are referenced from their respective domain stores.
- **Error wrapping discipline**: All errors use `%w` except when intentionally creating opaque sentinels (`errors.New(errMissingMigrateURL)`). The one exception is the chain-break case documented in db-2.
- **`go vet ./internal/db/...`**: clean (verified).
