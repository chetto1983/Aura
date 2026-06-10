# Audit: internal/knowledge

**Verdict:** needs-work — two real defects (deadlock + context-ignore) plus a code-duplication violation of the project's own rules.
**Counts:** critical 0 / high 1 / medium 1 / low 2

## Findings

---

### [HIGH][RACE] `Close()` deadlocks when called concurrently with an in-flight `Cypher()`

**Location:** `internal/knowledge/client.go:227` (`Close`) and `client.go:201-211` (`Cypher`)
**Confidence:** high

**Detail:**
`Cypher()` acquires `c.mu` at line 201 and then blocks on `c.stdout.ReadBytes('\n')` at line 211, waiting for the MCP subprocess response. `Close()` also acquires `c.mu` at line 227 before it can close stdin. If a second goroutine calls `Close()` while `Cypher()` is blocked on `ReadBytes`, `Close()` waits forever for the mutex — it can never reach the `c.stdin.Close()` that would unblock `Cypher()`'s pipe read. Deadlock.

The current CLI callers (`cmd/aura/neo4j.go` lines 97, 171) use sequential `defer mcp.Close()` and are not affected today. Any future agent or HTTP handler that cancels a context and triggers cleanup via `Close()` while a Cypher call is in-flight will deadlock.

The method comment on `Cypher` says "Serialized via mu" — this correctly serialises concurrent Cypher calls against each other, but `Close()` sharing the same mutex makes teardown racy with any call.

**Suggested fix:**
Use a separate `closeMu sync.Mutex` (or `sync.RWMutex`) for teardown, or implement close by signalling (close a done-channel) and letting `Cypher` notice after the current I/O round-trip completes. The simplest safe fix: in `Close()`, kill the process *before* waiting for the mutex, so `ReadBytes` unblocks and `Cypher` can return — then acquire the lock for cleanup.

```go
func (c *Client) Close() error {
    // Kill first so any blocked ReadBytes unblocks, releasing the mutex.
    if c.cmd != nil && c.cmd.Process != nil {
        _ = c.cmd.Process.Kill()
    }
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.stdin != nil {
        _ = c.stdin.Close()
        c.stdin = nil
    }
    cmd := c.cmd
    c.cmd = nil
    if cmd == nil {
        return nil
    }
    return cmd.Wait()
}
```

---

### [MEDIUM][BUG] `Cypher()` ignores context cancellation once inside the mutex

**Location:** `internal/knowledge/client.go:197-223`
**Confidence:** high

**Detail:**
`Cypher` guards against a pre-cancelled context at lines 198-200, but after `c.mu.Lock()` is acquired (line 201), neither `fmt.Fprintln` (line 208) nor `c.stdout.ReadBytes('\n')` (line 211) is context-aware. If the caller's context is cancelled while the method is blocked waiting for the MCP subprocess to respond, the goroutine is stuck until the subprocess either replies or dies. No context deadline or cancellation interrupts the pipe read.

This matters for any caller passing a request-scoped context (e.g., from a Telegram message handler or an AG-UI request). The goroutine effectively ignores the caller's cancellation budget after the first check.

`TestCypher_ContextCancelled` only tests a pre-cancelled context (cancel before call), not a mid-call cancellation — so the gap is not covered by tests.

D-06 policy (no graceful degrade on subprocess crash) is intentional, but context cancellation is a separate concern from subprocess liveness.

**Suggested fix:**
Wrap the I/O in a goroutine similar to `initializeWithContext`; select on `ctx.Done()` to kill the subprocess and unblock the read. Alternatively, document that `Cypher()` must only be called with a context that outlives the subprocess's expected response time, and callers must not cancel the context mid-flight.

---

### [LOW][BUG] `initializeWithContext`: successful `initialize()` after kill is silently discarded

**Location:** `internal/knowledge/client.go:105-112`
**Confidence:** medium

**Detail:**
When context times out and the subprocess is killed, the inner select waits up to 1 second for `initialize()` to drain. If `initialize()` returns `nil` (success) within that window — theoretically possible if the handshake completed just before the kill — the code at line 107 does nothing (the `if err != nil` branch is not taken), falls through to line 112, and returns a timeout error anyway. The successful completion is ignored.

This is correct from a caller perspective (context was cancelled = error should propagate), but the outcome is internally inconsistent: the MCP subprocess has finished its handshake but the client is being torn down as if initialization failed. The `c.Close()` at line 86 will then wait for a process that is already completing normally (not killed), potentially causing `cmd.Wait()` to return a non-error exit.

Severity is low because: (a) in practice, a successful handshake in < 1 second after a kill is improbable; (b) the caller discards the client either way.

**Suggested fix:**
Inside the inner select, on `err == nil` return a sentinel or restructure so the kill path always returns an error that clearly describes the outcome.

---

### [LOW][DEAD-CODE] `safeBuffer` duplicated from `internal/mcp/client.go`

**Location:** `internal/knowledge/client.go:272-287`
**Confidence:** high

**Detail:**
`safeBuffer` (struct + `Write` + `String` methods, 16 lines) is identical to the type defined in `internal/mcp/client.go:319-334`. This violates the project's CLAUDE.md "REUSABLE CODE — Never duplicate; extract a helper" rule. The two packages don't share code so the duplication is invisible to the compiler, but a future fix to one copy will not propagate to the other.

Grep evidence:
- `internal/knowledge/client.go:272` — definition
- `internal/mcp/client.go:319` — identical definition

**Suggested fix:**
Extract `safeBuffer` into an internal shared helper, e.g., `internal/iobuf/safebuffer.go`, and import it from both packages.
