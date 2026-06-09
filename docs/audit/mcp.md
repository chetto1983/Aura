# Audit: internal/mcp

**Verdict:** needs-work — one data race on `HTTPClient.sessionID` in `Close()`, one scanner buffer overflow that silently truncates large SSE payloads, three dead-code constants, and one exported method reachable only from tests.

**Counts:** critical 0 / high 1 / medium 2 / low 2

## Findings

---

### [HIGH][RACE] `HTTPClient.Close()` reads/writes `sessionID` without holding `mu`

**Location:** `internal/mcp/http_client.go:150–172`  
**Confidence:** high

`sessionID` is read at line 150, written at lines 164 and 170, and also passed to the request header in `decorate()` at line 248 — all without acquiring `c.mu`. Every other writer and reader of `sessionID` (`roundtripLocked` lines 189, 229; `decorate` called from `post` via `roundtripLocked`) runs while holding `c.mu`. The `Close()` method acquires no lock at all.

In production the transport's closer is returned from `MountManagedServer`/`MountServer` (`internal/agent/mcptools/mount.go`) and called at agent shutdown while in-flight tool calls (which hold `mu`) may still be executing. The race detector will fire if `Close()` and a concurrent `CallTool`/`ListTools`/`Ping` overlap.

**Suggested fix:** acquire `c.mu` at the top of `Close()`:

```go
func (c *HTTPClient) Close() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.sessionID == "" {
        return nil
    }
    // ... rest unchanged
}
```

---

### [MEDIUM][BUG] `decodeHTTPSSE` uses default `bufio.Scanner` (64 KiB limit); large SSE payloads silently fail

**Location:** `internal/mcp/http_client.go:268–288`  
**Confidence:** high

`bufio.NewScanner(r)` defaults to `bufio.MaxScanTokenSize` = 65 536 bytes. A Streamable-HTTP MCP server that returns a large tool result (web-fetch body, big SQL result set, bulk document) as a single `data:` line exceeding 64 KiB causes `scanner.Scan()` to return `false` and `scanner.Err()` to return `bufio.ErrTooLong`. The caller in `decodeHTTPSSE` then returns that error — but the wrapping in `roundtripLocked → CallTool` swallows the root cause label, so the user sees an opaque error with no mention of buffer overflow.

**Suggested fix:** set a larger buffer before the scan loop:

```go
scanner := bufio.NewScanner(r)
scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1 MiB — matches web-fetch body cap
```

---

### [MEDIUM][BUG] `mergeEnvPreserveCredentials` contains two mutually exclusive branches that do identical work; logic is correct but one branch is dead

**Location:** `internal/mcp/manager/config.go:117–124`  
**Confidence:** high

Lines 117–119 handle `isPlaceholderValue == true`; lines 121–123 handle `isPlaceholderValue == false`. Both execute `out = append(out, prior); continue`. Together they cover the full boolean space, so when `ok && isSecretEnvKey(key)`, the existing value is always used regardless of whether the incoming value is a placeholder. The second branch (lines 121–123) is semantically dead — the `continue` in the first branch always fires when both conditions are true.

This is not incorrect (the combined intent is to preserve existing credentials when `OverwriteCredentials=false`), but the duplicated `if prior, ok := existingByKey[key]; ok ...` map lookup and the misleading split across two conditions make the logic appear to differentiate two cases when it does not.

**Suggested fix:** collapse to one branch:

```go
if prior, ok := existingByKey[key]; ok && isSecretEnvKey(key) {
    out = append(out, prior)
    continue
}
```

---

### [LOW][DEAD-CODE] `StartupStarting`, `StartupReady`, `StartupFailed` constants are exported but never referenced outside their own file

**Location:** `internal/mcp/manager/status.go:13–15`  
**Confidence:** high

A full-repo grep (`internal/**/*.go` and `cmd/**/*.go`) finds zero non-definition references to `StartupStarting`, `StartupReady`, or `StartupFailed`. They are not referenced in tests either. `StartupBlocked`, `StartupDisabled`, and `StartupUnknown` are all assigned in `SnapshotStatus`; these three are not. They look like forward scaffolding for a live-monitoring layer that was never wired.

**Suggested fix:** remove or, if they represent a planned public API, add a doc comment and suppress with `//nolint:deadcode` until they are used.

---

### [LOW][NOT-WIRED] `ManagedConfig.EnabledServers()` is called only from tests; no production caller exists

**Location:** `internal/mcp/managed_config.go:142–167`  
**Confidence:** high

Production code routes through `manager.RuntimeServers` and `manager.RunnableManagedServers` (called from `internal/config/config.go:311–315`). `EnabledServers()` is called only in `internal/mcp/managed_config_test.go` (lines 56, 411, 424). The method was the original stdio-only dispatch path before the manager sub-package was introduced; it is now superseded and tested but unreachable in production.

Exporting a method with no production caller creates a false API surface and a validation path that can diverge from the real one.

**Suggested fix:** either delete `EnabledServers()` and migrate its test coverage to the manager-level functions, or demote it to unexported `enabledServers()` and document its test-helper status.
