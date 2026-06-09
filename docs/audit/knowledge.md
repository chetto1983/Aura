# Audit: internal/knowledge

**Verdict:** needs-work — one not-wired config knob, one Close/Cypher unsynchronised access, one version-prefix over-match.  
**Counts:** critical 0 / high 1 / medium 1 / low 1

## Scope

Files audited (non-test only):

- `internal/knowledge/client.go`
- `internal/knowledge/config.go`
- `internal/knowledge/migrate.go`
- `internal/knowledge/ping.go`
- `internal/knowledge/reset.go`
- `internal/knowledge/schema.go`
- `internal/knowledge/status.go`
- `internal/knowledge/migrations/0001_init.cypher`

Test files read for intent:
`client_test.go`, `client_unit_test.go`, `client_paths_test.go`, `smoke_test.go`, `integration_env_test.go`, `test_helpers_test.go`.

Callers grepped across the full repo (`D:/Aura/**/*.go`).

---

## Findings

### [HIGH][NOT-WIRED] `Config.ConnectTimeoutSec` is parsed and documented but never consumed

**Location:** `internal/knowledge/config.go:23`, `internal/knowledge/client.go:49-87`, `internal/knowledge/schema.go:30-39`  
**Confidence:** high

`Config.ConnectTimeoutSec` is declared with the doc comment "first-call connect/retry budget" and is populated from `AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC` by `internal/config/config.go:215`. Neither `Open()` nor `OpenSchema()` reads this field:

- `Open()` calls `exec.CommandContext(ctx, cfg.MCPBinary, args...)` — the context deadline (if any) is all it gets; `ConnectTimeoutSec` is not applied.
- `OpenSchema()` calls `driver.VerifyConnectivity(ctx)` with the caller's context; `ConnectTimeoutSec` is not applied.
- `pingEmbed()` creates `http.Client{Timeout: 10 * time.Second}` — hardcoded, not derived from `ConnectTimeoutSec`.

The operator sets `AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC=25` expecting a longer connect budget; nothing changes. The config key, the struct field, the test assertion in `internal/config/config_test.go:416`, and the `aura config` display in `cmd/aura/config.go:71` all give a false impression that the knob works.

**Suggested fix:** In `Open()`, wrap the caller's context with `context.WithTimeout(ctx, time.Duration(cfg.ConnectTimeoutSec)*time.Second)` to apply the budget to subprocess spawn + MCP handshake. Apply the same wrapping in `OpenSchema()`'s `VerifyConnectivity` call. Replace the hardcoded `10 * time.Second` in `pingEmbed` with `time.Duration(expectedDim /* wrong — pass cfg */)` — or pass `cfg.ConnectTimeoutSec` into `pingEmbed`. Alternatively, if the intent is that callers always supply a deadline-scoped context, remove the field from `Config` and delete the associated env-var parsing in `internal/config/config.go` to avoid the false knob.

---

### [MEDIUM][RACE] `Close()` accesses `c.stdin` without holding `c.mu`

**Location:** `internal/knowledge/client.go:199-207`  
**Confidence:** medium

`Cypher()` holds `c.mu` for the entire request-response cycle and calls `fmt.Fprintln(c.stdin, ...)` on the write side of the stdio pipe. `Close()` does not acquire `c.mu` before calling `c.stdin.Close()`. If a caller concurrently calls `Cypher()` and `Close()` (e.g., context-cancellation racing a shutdown path), there is an unsynchronised read/write of `c.stdin` between the two goroutines.

In practice the current callers in `cmd/aura/neo4j.go` are sequential (defer close after the last cypher call), so this does not fire today. But it is a latent race for any future caller (or an AGUI / Telegram goroutine) that issues a `Cypher()` while another goroutine calls `Close()` on shutdown.

**Suggested fix:** Acquire `c.mu` at the top of `Close()` before accessing `c.stdin`:

```go
func (c *Client) Close() error {
    c.mu.Lock()
    stdin := c.stdin
    c.mu.Unlock()
    if stdin != nil {
        _ = stdin.Close()
    }
    if c.cmd == nil {
        return nil
    }
    return c.cmd.Wait()
}
```

Note: `cmd.Wait()` must be called without holding `mu` since it blocks; the snapshot approach above is sufficient to serialize the `stdin.Close()` call.

---

### [LOW][BUG] `kernelVersion` / `pingMCP` version prefix check over-matches future Neo4j minor lines

**Location:** `internal/knowledge/ping.go:52`  
**Confidence:** low

```go
if !strings.HasPrefix(version, "5.26") {
    return "", fmt.Errorf("unexpected Neo4j version %q (want 5.26.x ...)", version)
}
```

`strings.HasPrefix` matches any string whose first four characters are `5.26`, including hypothetical future versions `5.260`, `5.261`, `5.269`. If Neo4j ever ships a `5.260.x` (unlikely but not impossible given their versioning history), this check would silently accept it. More concretely, it would also accept `5.26` with no patch suffix (bare version string from a dev build).

**Suggested fix:** Tighten the check to match the full minor segment:

```go
if !strings.HasPrefix(version, "5.26.") {
    return "", fmt.Errorf(...)
}
```

This is a one-character change (add the trailing `.`) that eliminates the over-match without requiring semver parsing.

---

## What was checked and found clean

- **Nil-pointer derefs:** `Client` fields (`cmd`, `stdin`, `stderr`) are all guarded before use in `Close()` and `Open()`. `decodeRows` checks `len(result) == 0` before unmarshal.
- **Unchecked errors:** All `fmt.Fprintln` write errors are checked. `resp.Body.Close()` is deliberately discarded (idiomatic). `schema.Close()` in `OpenSchema` error path is deliberately discarded with `_`.
- **Resource leaks:** `StdinPipe` and `StdoutPipe` are handled correctly — `stdin.Close()` signals EOF; `cmd.Wait()` cleans up the stdout pipe per `exec.Cmd` contract. The `safeBuffer` does not spawn goroutines. `pingEmbed`'s `resp.Body.Close()` is deferred correctly; `CloseIdleConnections()` is also deferred to prevent goroutine leaks in goleak.
- **Context propagation:** `Cypher()` checks `ctx.Err()` before acquiring the lock. `OpenSchema()` passes `ctx` to `VerifyConnectivity` and `driver.Close`. `pingEmbed` uses `http.NewRequestWithContext`.
- **Dead code:** All exported symbols (`Open`, `Close`, `Cypher`, `OpenSchema`, `SchemaExecutor.Exec`, `SchemaExecutor.Close`, `Migrate`, `Reset`, `Status`, `Ping`, `PingResult`, `MigrationRow`, `Config`, `DefaultEmbedDimensions`) are referenced from `cmd/aura/neo4j.go` and/or `internal/config/config.go`. All unexported helpers (`decodeRows`, `kernelVersion`, `pingEmbed`, `pingMCP`, `splitCypherStatements`, `loadMigrations`, `parseMigrationName`, `stderrTail`, `redactSecrets`, `buildRequest`, `safeBuffer`) are called within the package.
- **SQL/JSON mishandling:** `decodeRows` correctly handles the nested `{"content":[{"type":"text","text":"<json>"}]}` envelope, including `isError`, empty content, and invalid inner JSON.
- **Migration idempotency:** The `IF NOT EXISTS` guards in `0001_init.cypher` make partial re-runs safe even though Cypher DDL cannot be transactional.
- **Integer conversion:** `parseMigrationName` bounds the version to `[1, 999999]` before converting `int` to `int32` — provably safe.
- **`safeBuffer` concurrency:** Both `Write` and `String` acquire the embedded mutex; concurrent subprocess writes and error-path reads are safe.
