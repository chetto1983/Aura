# Audit: internal/mcp

**Verdict:** needs-work — one blocking-context bug, one not-wired exported method, two low-severity code-quality issues.

**Counts:** critical 0 / high 1 / medium 1 / low 2

## Findings

---

### [HIGH][BUG] Stdio client: context cancellation ignored inside roundtrip

**Location:** `internal/mcp/client.go:138-196` (`ListTools`, `CallTool`, `Ping`) and `internal/mcp/client.go:224-244` (`readResponse`)

**Confidence:** high

**Detail:**
All three public methods check `ctx.Err()` before acquiring `c.mu`. Once the mutex is taken, they call `c.roundtrip()`, which calls `readResponse()`. `readResponse` blocks indefinitely on `c.stdout.ReadBytes('\n')` — a plain `bufio.Reader` call with no context-aware deadline, no select, no channel. A caller that cancels (or times out) the context after the mutex is acquired will be stuck until the MCP server writes a matching response or the pipe is closed. The `exec.CommandContext` bind only kills the subprocess when the *Open* context is cancelled; per-call contexts have no process-level effect and are silently dropped after the pre-lock check.

In practice this manifests as goroutine hangs when an MCP tool takes longer than the agent's per-turn deadline.

**Suggested fix:**
Replace the `io.Pipe`-backed `cmd.StdoutPipe` with a `bufio.Reader` whose underlying `io.Reader` is wrapped in a goroutine that forwards to a channel, and use a `select` in `readResponse` over that channel and `ctx.Done()`. Alternatively, set a per-call deadline on the pipe via `os.File.SetDeadline` (available on the `*os.File` the pipe wraps after unwrapping with a type assertion). The HTTPClient already propagates context correctly via `http.NewRequestWithContext`.

---

### [MEDIUM][NOT-WIRED] `ManagedConfig.EnabledServers` is exported but never called in production

**Location:** `internal/mcp/managed_config.go:142-167`

**Confidence:** high

**Detail:**
`EnabledServers()` is an exported method on `ManagedConfig`. Grepping the entire repo (all non-test `.go` files) for `.EnabledServers(` returns only three hits, all in `internal/mcp/managed_config_test.go`. The production code path in `internal/config/config.go:306-310` uses `mcpmanager.RuntimeServers` and `mcpmanager.RunnableManagedServers` from the `manager` sub-package instead.

`EnabledServers` also has divergent semantics from the manager path: it does not apply the active profile, does not check trust class, and returns raw `ServerConfig` instead of the runtime-resolved launch configs (Docker, gateway, etc.). This means any caller that discovers and uses this method will silently bypass trust enforcement and profile selection.

**Suggested fix:**
Delete `EnabledServers` and migrate its three tests to use `manager.RunnableManagedServers` or `manager.RuntimeServers`. If a lightweight "all enabled stdio servers, ignore profile/trust" API is genuinely needed, document that contract explicitly in a follow-up; do not surface it on the public type without trust enforcement.

---

### [LOW][DEAD-CODE] `HTTPClient.roundtrip` (unexported) is a one-shot locking wrapper

**Location:** `internal/mcp/http_client.go:177-181`

**Confidence:** high

**Detail:**
`(*HTTPClient).roundtrip` acquires `c.mu` and delegates to `roundtripLocked`. It is called exactly once, from `initialize` (line 69), which is invoked by `OpenHTTP` before the client is exposed to any goroutine. All three public methods (`ListTools`, `CallTool`, `Ping`) acquire the lock themselves and call `roundtripLocked` directly, bypassing `roundtrip`. The method is not dead in the absolute sense (it is reachable), but it exists solely to add a lock around a pre-share operation that needs no locking — the lock adds noise and a false impression that concurrent callers race to call `roundtrip`.

**Suggested fix:**
In `initialize`, call `roundtripLocked` directly (the function already exists) and remove `roundtrip`. This eliminates the confusing layering without changing behavior.

---

### [LOW][BUG] `rpcResult` redundant unreachable branch when called from `decodeHTTPSSE`

**Location:** `internal/mcp/http_client.go:283-286` and `internal/mcp/http_client.go:294-296`

**Confidence:** high

**Detail:**
`decodeHTTPSSE` guards with `if resp.ID == nil || *resp.ID != wantID { continue }` at line 283 before calling `rpcResult(resp, wantID)` at line 286. Inside `rpcResult`, the identical check at line 295 (`if resp.ID == nil || *resp.ID != wantID`) is unreachable from the SSE call site — the invariant `resp.ID != nil && *resp.ID == wantID` is established by the caller. The branch is harmless (it returns an error string that would never be seen) but obscures the actual logic. The check is necessary only for the non-SSE call site in `decodeHTTPRPC` (line 268), where no pre-filtering is done.

**Suggested fix:**
Either (a) remove the ID check from `rpcResult` and add an explicit ID check in `decodeHTTPRPC` before calling `rpcResult`, or (b) add a doc comment to `rpcResult` explaining that the ID check is a guard for the non-SSE call path. Option (a) is cleaner.
