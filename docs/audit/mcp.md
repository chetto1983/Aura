# Audit: internal/mcp

**Verdict:** needs-work — two dead-code items and one logic bug in credential merge

**Counts:** critical 0 / high 1 / medium 2 / low 2

## Findings

---

### [HIGH][BUG] mergeEnvPreserveCredentials: existing placeholder wins over incoming real credential

**Location:** `internal/mcp/manager/config.go:117-124`

**Confidence:** high

**Detail:**

When `ImportProfile` is called without `OverwriteCredentials`, it delegates to `mergeEnvPreserveCredentials(existing.Env, next.Env)`. Inside that function there are two `if` branches for secret keys (lines 117 and 121). Together they collapse to: "for any secret key that exists in `existing`, always emit the `existing` entry." This means if the existing config has a placeholder such as `API_TOKEN=${API_TOKEN}` (as produced by `RedactEnv` during `ExportProfile`) and the incoming config carries a real value `API_TOKEN=real-secret`, the placeholder wins. The real credential is silently discarded.

The practical impact: a user who first exports a profile (which redacts all secrets to `${KEY}` placeholders) and later tries to import a filled-in version cannot load the real credentials without explicitly calling `ImportProfile` with `ImportOptions{OverwriteCredentials: true}`. This is counter-intuitive and undocumented — the docstring says "preserving existing credentials", but a placeholder is not a credential.

The two branches (lines 117-124) are also logically redundant: `isPlaceholderValue(key, value)` and `!isPlaceholderValue(key, value)` are complements, so both branches always resolve to `out = append(out, prior); continue`. The second branch (121-124) is reachable but produces the same outcome as removing it and simplifying to a single check.

**Suggested fix:**

Only preserve the existing entry when the existing value is itself a real (non-placeholder) credential:

```go
if prior, ok := existingByKey[key]; ok && isSecretEnvKey(key) && !isPlaceholderValue(key, prior) {
    out = append(out, prior)
    continue
}
// fall through: existing is placeholder or absent, use incoming
out = append(out, entry)
```

This way:
- existing=real, incoming=placeholder → existing wins (correct: don't overwrite a real credential with a placeholder)
- existing=real, incoming=real → existing wins (preserve existing credentials, consistent with `!OverwriteCredentials`)
- existing=placeholder, incoming=real → incoming wins (correct: populate an unfilled slot)

---

### [MEDIUM][NOT-WIRED] ManagedConfig.EnabledServers is not called in production code

**Location:** `internal/mcp/managed_config.go:142-167`

**Confidence:** high

**Detail:**

`EnabledServers()` is an exported method on `ManagedConfig`. A repo-wide Grep of all non-test `.go` files finds zero production call sites. All three references are in `internal/mcp/managed_config_test.go`. Production code exclusively routes through `manager.RuntimeServers` / `manager.RuntimeLaunchConfig` (called from `internal/config/config.go`, `cmd/aura/mcp.go`, `cmd/aura/mcp_tools.go`, and `internal/agent/mcptools/mount.go`).

`EnabledServers` predates the manager package. It silently includes `TrustBlocked` servers (no trust gate), returns only stdio servers, and ignores Docker/gateway runtime kinds — so it is also semantically incorrect relative to the current launch path. It is not a stepping stone to any current feature.

**Suggested fix:** Remove `EnabledServers`. Its tests (`TestEnabledServersExcludesHTTPAndEmptyIsNil`, `TestEnabledServersValidationError`, and the call in `TestLoadManagedConfig`) should be removed or redirected to `manager.RuntimeServers`.

---

### [MEDIUM][DEAD-CODE] StartupStarting, StartupReady, StartupFailed constants are never referenced

**Location:** `internal/mcp/manager/status.go:13-15`

**Confidence:** high

**Detail:**

The exported constants `StartupStarting = "starting"`, `StartupReady = "ready"`, and `StartupFailed = "failed"` are defined in `manager/status.go`. A repo-wide Grep across all `.go` files (including tests) finds zero references to any of them outside the file that defines them. `StartupBlocked`, `StartupDisabled`, and `StartupUnknown` are used — by `SnapshotStatus` and its tests — but the three orphaned constants are not wired into `SnapshotStatus` (which sets state only to `StartupUnknown`, `StartupDisabled`, or `StartupBlocked`). Lifecycle states "starting", "ready", and "failed" are not implemented; the field is always set to "unknown" when not explicitly blocked or disabled.

**Suggested fix:** Remove the three constants or implement the lifecycle tracking they imply (requires a live-state map outside `SnapshotStatus`). If the intent is future use, add a `// nolint:deadcode` comment with a TODO linking to the tracking issue.

---

### [LOW][BUG] notify in HTTPClient: StatusAccepted check is redundant

**Location:** `internal/mcp/http_client.go:206`

**Confidence:** high

**Detail:**

```go
if resp.StatusCode == http.StatusAccepted || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
```

`http.StatusAccepted` is 202, which falls inside `[200, 300)`. The first clause is always subsumed by the second. The condition is correct but the first clause is dead — remove it.

**Suggested fix:** `if resp.StatusCode >= 200 && resp.StatusCode < 300 {`

---

### [LOW][DEAD-CODE] rpcResult ID re-check is unreachable from the SSE call site

**Location:** `internal/mcp/http_client.go:294-296`

**Confidence:** medium

**Detail:**

`rpcResult` re-checks `resp.ID == nil || *resp.ID != wantID` at line 295. When called from `decodeHTTPSSE` (line 286), the check at line 283 already guarantees `resp.ID != nil && *resp.ID == wantID`, making the first clause of `rpcResult` unreachable from that call site. The JSON-decode path in `decodeHTTPRPC` does not pre-filter, so the check is reachable there.

The function's dual-call-site design means the guard is necessary for the JSON path but dead for the SSE path. Not a correctness issue, but it signals that `rpcResult` carries a precondition (`resp.ID` already validated) that is only sometimes true at the call site.

**Suggested fix:** Leave `rpcResult` as-is (removing the guard would break the JSON path). Optionally inline the error return directly in `decodeHTTPSSE` after line 283 to make the SSE path's contract explicit and remove the dead reachability.

---

## Notes on items checked and found clean

- **Mutex discipline (Client, HTTPClient):** `mu` is held for every public method that touches the stdin/stdout pipe or HTTP session state. `initialize` and `close` are called before or after the client is shared, so their lock-free / single-lock behavior is safe.
- **subprocess reaping (Client.Close):** The goroutine spawned to drain `cmd.Wait()` is bounded by a `time.After(5s)` then `Process.Kill()` + a blocking receive on `done`. No goroutine leak.
- **HTTP body close:** All `post` error paths defer `resp.Body.Close()` inline before returning the error. `decodeHTTPRPC` defers `body.Close()` before calling `decodeHTTPSSE`. No body leak found.
- **Context propagation:** `ListTools`, `CallTool`, `Ping` all check `ctx.Err()` early and forward `ctx` through to `http.NewRequestWithContext`. `Close` deliberately uses `context.Background()` — correct for cleanup paths.
- **Race safety:** `safeBuffer` guards all concurrent stderr reads with its own mutex. `nextID` is `atomic.Int64`. No shared maps written concurrently.
- **JSON handling:** `rpcResp.ID` is `*int64`; nil-check before dereference present at every call site.
- **`httpAuthFromEnv` / `MCP_HEADER_*` transform:** All-uppercase underscore-to-hyphen transform is consistent with operator-facing naming convention; the empty-string guard prevents inserting a zero-length key.
