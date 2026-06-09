# Audit: internal/agent/mcptools

**Verdict:** needs-work — two latent correctness issues; no crashes or races in production paths.

**Counts:** critical 0 / high 0 / medium 1 / low 2

## Findings

---

### [MEDIUM][BUG] `openManagedServer` routing condition diverges from `normalizedServerType`

**Location:** `internal/agent/mcptools/mount.go:48`

**Confidence:** high

**Detail:**

`openManagedServer` routes to `mcp.OpenServer` when either `server.Type == "streamable_http"` OR `server.URL != ""`:

```go
if strings.TrimSpace(server.Type) == mcp.ServerTypeStreamableHTTP || strings.TrimSpace(server.URL) != "" {
    return mcp.OpenServer(ctx, name, server)
}
```

Every other classification site in the codebase uses `normalizedServerType`, which classifies a server as HTTP only when `URL != "" AND Command == ""`. The gap matters for a `ManagedServer` where both `URL` and `Command` are non-empty and `Type` is unset:

- `openManagedServer`: URL is set → routes to `OpenServer`.
- `OpenServer` internally uses `normalizedServerType`: Command is non-empty → classifies as stdio → launches the subprocess with `Command`, silently ignoring `URL`.

The operator effect: an HTTP URL is silently dropped, the stdio binary is launched instead. No crash — but the mount succeeds with the wrong transport.

This does not fire through the standard config loading path (`loadMCPServers`): a URL+Command server without explicit Type is classified as stdio by `normalizedServerType`, so it lands in `MCPServers` (via `RuntimeServers`), not in `MCPPolicies`, and is mounted via `MountServer` (which does not call `openManagedServer`). However, code that constructs a `ManagedServer` inline and passes it directly to `MountManagedServer` (e.g. tests, spike scripts, or future call sites) will hit this silently.

**Suggested fix:**

Replace the bespoke condition with `normalizedServerType`:

```go
func openManagedServer(ctx context.Context, name string, server mcp.ManagedServer) (mcp.Transport, error) {
    if mcp.NormalizedServerType(server) == mcp.ServerTypeStreamableHTTP {
        return mcp.OpenServer(ctx, name, server)
    }
    cfg, err := mcpmanager.RuntimeLaunchConfig(name, server)
    if err != nil {
        return nil, err
    }
    return mcp.Open(ctx, name, cfg)
}
```

(`normalizedServerType` is currently unexported; export it as `NormalizedServerType` or expose it via a helper in `managed_config.go`.) Alternatively, call `mcp.OpenServer` unconditionally and let it dispatch internally — but that bypasses the trust gate (`RuntimeLaunchConfig`) for stdio servers.

---

### [LOW][NOT-WIRED] `Bridge` is exported but has no production caller outside the package

**Location:** `internal/agent/mcptools/bridge.go:74`

**Confidence:** high

**Detail:**

`Bridge` is an exported function. A grep across the entire repo (excluding tests and spike scripts under `.planning/`) shows zero calls to `mcptools.Bridge` in production code:

```
# production callers of mcptools.*:
cmd/aura/main.go:180: mcptools.MountManagedServer(...)
cmd/aura/main.go:182: mcptools.MountServer(...)
```

`Bridge` is called only from within the package (by `Mount` at bridge.go:117) and from `bridge_test.go`. The `.planning/` spike scripts call `mcptools.Mount`, not `mcptools.Bridge`.

`Bridge` is a legitimate public seam — callers that want a `[]tools.Tool` slice without immediately registering it (e.g. to inspect or filter) have a valid use case. But it is currently untested in that external role and is invisible to API users who only import `mcptools`. If it is intentional public API, add a doc comment clarifying the external contract. If it is not intended to be public, unexport it.

**Suggested fix:**

If the external use case is future-only and not yet planned, unexport: rename to `bridgeTools` (already exists as the inner implementation; merge the two) and make `Mount` the sole public entry point. If public API is intended, add a doc comment confirming the contract.

---

### [LOW][BUG] Unchecked type assertion `t.(*bridgedTool)` in `registerBridged`

**Location:** `internal/agent/mcptools/bridge.go:128`

**Confidence:** high

**Detail:**

```go
bt := t.(*bridgedTool)
```

`registerBridged` accepts `[]tools.Tool` (interface slice) but unconditionally asserts each element to `*bridgedTool`. A nil element or any non-`*bridgedTool` value panics with no recovery. The function is unexported and its only call site (`Mount`, bridge.go:121) passes the output of `Bridge`, which is always `[]*bridgedTool` — so no panic occurs today.

The risk is maintenance: if a future refactor passes a different concrete type through the same function (e.g. wrapping for observability), the panic surface is non-obvious and has no defensive check.

**Suggested fix:**

Either change the slice type to `[]*bridgedTool` so the type system enforces the invariant:

```go
func registerBridged(reg *tools.Registry, bridged []*bridgedTool) ([]string, error) {
```

Or add a checked assertion with a clear error:

```go
bt, ok := t.(*bridgedTool)
if !ok {
    return nil, fmt.Errorf("mcp bridge: internal error: expected *bridgedTool, got %T", t)
}
```

---

## Clean checks (no findings)

The following were checked exhaustively and found to be clean:

- **Collision disambiguation** (`registerBridged`, bridge.go:124-160): the two-pass design (validation pass with `seenRaw`/`chosen` mutation, then registration pass) is correct for N-way collisions. Hash keys include the raw tool name (`bt.name`), so distinct raw names that sanitize identically always get distinct suffixes. The post-disambiguation `chosen[name]` guard correctly catches SHA-256 suffix collisions (astronomically rare) as an all-or-nothing error.
- **Name length cap** (`namespacedName`, name.go:49-65): the overflow branch correctly reserves space for both the `__` delimiter and the 13-byte hash suffix before truncating; the `max(budget-len(nsDelimiter), 0)` guard prevents negative slice indices for extremely long namespaces. Verified by the adversarial sweep in `name_test.go`.
- **Nil/null InputSchema fallback** (bridge.go:85-88): `strings.TrimSpace(string(nil)) == ""` in Go, so `nil`, `json.RawMessage("null")`, and empty schemas all fall through to `emptyObjectSchema`. Verified.
- **Context propagation**: context is threaded through every call chain (`Open`, `ListTools`, `CallTool`, `NewResult`). No context is dropped or ignored.
- **Goroutine safety**: no goroutines are spawned in this package. The Registry and all types are single-owner (built at mount time, immutable thereafter). No races.
- **Error wrapping**: all errors use `%w` or are passed through unchanged. `Bridge` errors propagate from `srv.ListTools`; `Mount` wraps spawn/registration failures with `fmt.Errorf("mount %q: %w", name, err)`.
- **Execute error contract**: CallTool errors are converted to inline `"error: ..."` content (not Go errors), matching the web_search contract. JSON unmarshal failures on args ARE Go errors (correct — the model cannot self-correct from malformed JSON structure).
- **Resource cleanup**: on any error in `MountServer`/`MountManagedServer`, the underlying transport is explicitly closed before returning (`_ = cli.Close()` / `_ = srv.Close()`). The success path returns the closer for the caller to call at shutdown.
