# Audit: internal/knowledge

**Verdict:** needs-work — two latent races in the initialize timeout path; one nil-deref on use-after-close; all other surfaces clean.

**Counts:** critical 0 / high 0 / medium 2 / low 1

---

## Findings

### [MEDIUM][RACE] Data race: `c.stdin` accessed in goroutine without lock while `Close()` can set it to nil

**Location:** `internal/knowledge/client.go:102–113` (goroutine) and `internal/knowledge/client.go:229–231` (Close)

**Confidence:** medium

**Detail:**
`initializeWithContext` launches a goroutine that runs `c.initialize()`. When `ctx.Done()` fires, the function kills the process and waits up to 1 second in the inner `select`. If `time.After(1s)` fires first (goroutine still alive — blocked on `c.stdout.ReadBytes`), `initializeWithContext` returns. `Open` (caller) then calls `c.Close()`, which under `c.mu` calls `c.stdin.Close()` and sets `c.stdin = nil`. If the goroutine then unblocks and reaches `fmt.Fprintln(c.stdin, ...)` (line 134) or `fmt.Fprintln(c.stdin, ...)` (line 148), it reads `c.stdin` concurrently with `Close()` writing it — a Go memory model data race. `initialize()` holds no lock when reading `c.stdin`; `Close()` holds `c.mu` when writing it.

In practice the window is narrow: after `Process.Kill()`, the OS closes the pipe immediately, so the goroutine finishes quickly. But this is an OS timing assumption — under load or a slow reaper, the 1 s drain budget can expire while the goroutine is mid-write, making the race reachable.

**Suggested fix:**
Add a boolean `closing atomic.Bool` or re-check `c.stdin` under `c.mu` inside `initialize`, or extend `c.mu` to cover `initialize()` since the comment "needs no lock (the client is not yet shared)" no longer holds once `initializeWithContext` introduces concurrent call paths. The cleanest fix is to ensure `c.stdin.Close()` inside `Close()` is called first (it will cause `fmt.Fprintln` to return an error), and then set `c.stdin = nil` only after the goroutine has drained — i.e., always wait on `done` in the inner select before calling `Close`:
```go
case <-time.After(time.Second):
    // goroutine may still hold c.stdin; do not race — the caller's c.Close()
    // will close stdin, unblocking the goroutine within microseconds.
    // nothing more to do here — fall through to error return
```
The current code is already structured this way but the race exists between the goroutine's pending `c.stdin` read and `Close`'s `c.stdin = nil` write. Removing the `c.stdin = nil` line from `Close` (leaving it for GC) or protecting `c.stdin` reads in `initialize()` with `c.mu` closes the race.

---

### [MEDIUM][BUG] Goroutine leaked for up to 1 second when `initializeWithContext` inner drain timer fires

**Location:** `internal/knowledge/client.go:105–111`

**Confidence:** high

**Detail:**
When the context deadline fires during initialization and the inner `time.After(time.Second)` case is selected (goroutine still alive), `initializeWithContext` returns with the goroutine still running. The goroutine accesses `c` (stdin, stdout, stderr, password) after `initializeWithContext` has returned and `c.Close()` has been called. The goroutine will eventually exit (broken pipe from the killed subprocess), but there is an indeterminate window during which it is leaked.

`goleak.VerifyTestMain` in `client_unit_test.go` would catch this leak, but no unit or integration test exercises the exact path: `ConnectTimeoutSec=1`, subprocess slow to respond to Kill (i.e., the inner 1 s budget expires). `TestOpenKeepsProcessAliveAfterConnectTimeout` uses a well-behaved fake MCP that always completes initialization, so it never hits the abort branch.

**Suggested fix:**
In the timeout branch, always drain `done` before returning, with a generous but bounded deadline (e.g., 2 s after the kill). Remove the fall-through path that leaves the goroutine alive:
```go
case <-ctx.Done():
    if c.cmd != nil && c.cmd.Process != nil {
        _ = c.cmd.Process.Kill()
    }
    // Block until goroutine exits (pipe closed by Kill → quick).
    select {
    case <-done:
    case <-time.After(2 * time.Second):
        // Truly stuck; accept the leak rather than blocking forever.
    }
    return fmt.Errorf("initialize timeout: %w", ctx.Err())
```
This ensures goleak passes and removes the concurrent access window described in knowledge-1.

---

### [LOW][BUG] Nil-deref panic if `Cypher()` is called after `Close()`

**Location:** `internal/knowledge/client.go:208` (`Cypher`), `internal/knowledge/client.go:231` (`Close`)

**Confidence:** high

**Detail:**
`Close()` sets `c.stdin = nil` under `c.mu`. `Cypher()` also acquires `c.mu`, so the two cannot run concurrently — but if `Cypher` is called sequentially after `Close`, it reaches `fmt.Fprintln(c.stdin, string(enc))` with `c.stdin == nil` (a nil `io.WriteCloser` interface), causing a nil pointer dereference panic at the `w.Write(p)` call inside `fmt.Fprintln`.

No production code currently reaches this path: the CLI defers `mcp.Close()` after all Cypher calls. However, there is no programmatic guard, so any future caller that reuses the client object after closing it will panic rather than receive a clean error.

**Suggested fix:**
Add a sentinel check in `Cypher` after acquiring the lock:
```go
c.mu.Lock()
defer c.mu.Unlock()
if c.stdin == nil {
    return nil, fmt.Errorf("cypher call on closed client")
}
```

---

## What was checked and found clean

- **No dead exported symbols.** Every exported identifier (`Client`, `Config`, `Open`, `Close`, `Cypher`, `Migrate`, `Reset`, `Status`, `Ping`, `PingResult`, `MigrationRow`, `SchemaExecutor`, `OpenSchema`, `DefaultEmbedDimensions`) is referenced in production code (`cmd/aura/neo4j.go`, `internal/config/config.go`). Verified with Grep across the full repo.
- **No unexported dead code.** All unexported helpers (`pingMCP`, `pingEmbed`, `kernelVersion`, `connectContext`, `stderrTail`, `redactSecrets`, `decodeRows`, `loadMigrations`, `parseMigrationName`, `splitCypherStatements`, `safeBuffer`, `rpcReq`, `rpcResp`) are called from within the package.
- **`splitCypherStatements` semicolon logic.** Trailing `;` produces an empty final segment; the `len(keep) > 0` guard correctly discards it. All three migration statements use `IF NOT EXISTS`, so a partial-migration re-run (schema DDL applied but audit row absent) is idempotent.
- **`safeBuffer` concurrency.** Both `Write` and `String` hold `sb.mu` — race-free under concurrent subprocess stderr writes and goroutine error-path reads.
- **`Close()` idempotency.** Double-close is safe: second call sees `c.stdin == nil` and `c.cmd == nil`, returns nil without action.
- **`pingEmbed` defer order.** `resp.Body.Close()` is deferred later (executes first), then `client.CloseIdleConnections()` fires — correct ordering; the connection is idle before `CloseIdleConnections` is called.
- **No unchecked HTTP status path.** `pingEmbed` checks `resp.StatusCode != http.StatusOK` and returns an error before decoding.
- **`parseMigrationName` int32 overflow guard.** The explicit `[1, 999999]` bound before `int32(n)` makes the conversion provably safe.
- **`int64` ID counter.** `c.nextID` is `atomic.Int64`, safe under concurrent use.
- **Context propagation.** All Cypher calls pass `ctx` through; the short-circuit `ctx.Err()` check at `Cypher` entry is correct. `pingEmbed` uses `http.NewRequestWithContext`.
- **go.mod version.** `go 1.26.4` — `strings.SplitSeq` (1.24+) and `for i := range N` (1.22+) are valid.
- **`go vet ./internal/knowledge/` output:** clean (no issues).
