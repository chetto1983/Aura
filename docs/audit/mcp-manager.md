# Audit: internal/mcp/manager

**Verdict:** needs-work — one not-wired env injection and five dead exported symbols.

**Counts:** critical 0 / high 1 / medium 1 / low 3

---

## Findings

### [HIGH][NOT-WIRED] `AURA_MCP_NETWORK_ALLOW` never reaches the Docker container

**Location:** `internal/mcp/manager/runtime.go:123-135`
**Confidence:** high

**Detail:**
`dockerRuntimeConfig` builds the docker command-line in `args` and separately builds a `env` slice. The `-e KEY` loop (lines 124-129) forwards only `server.Env` entries to the container. Then, at line 131, `AURA_MCP_NETWORK_ALLOW=...` is appended to `env` — *after* the `-e` loop has finished. This `env` slice becomes `ServerConfig.Env`, which `mcp.Open()` passes to `cmd.Env` (the docker *client* process's own environment), not to the container. Docker containers only receive env vars that are explicitly forwarded via `-e` flags in `args`. No other code reads `AURA_MCP_NETWORK_ALLOW` from the docker client environment. The container process never sees the allowlist.

The test at `runtime_test.go:55` asserts `cfg.Env` contains the var (correct — it is in `cfg.Env`) but never checks that a `-e AURA_MCP_NETWORK_ALLOW` flag appears in `cfg.Args`. The assertion is vacuously true and hides the gap.

**Suggested fix:**
Move the `AURA_MCP_NETWORK_ALLOW` append *before* the `-e` loop, or add an explicit `-e AURA_MCP_NETWORK_ALLOW` to `args` after appending it to `env`:

```go
if len(server.Runtime.Network) > 0 {
    env = append(env, "AURA_MCP_NETWORK_ALLOW="+strings.Join(server.Runtime.Network, ","))
    args = append(args, "-e", "AURA_MCP_NETWORK_ALLOW")
}
```

Add a test asserting `cfg.Args` contains `-e AURA_MCP_NETWORK_ALLOW` when `Network` is non-empty.

---

### [MEDIUM][DEAD-CODE] `StartupStarting`, `StartupReady`, `StartupFailed` defined but never used

**Location:** `internal/mcp/manager/status.go:13-15`
**Confidence:** high

**Detail:**
Three exported startup-state constants are declared in the `const` block:

```go
StartupStarting = "starting"
StartupReady    = "ready"
StartupFailed   = "failed"
```

`SnapshotStatus` only ever assigns `StartupDisabled`, `StartupBlocked`, or `StartupUnknown`. No production code in the repo (cmd/ or internal/) references these three constants by name. Only `StartupBlocked` and `StartupDisabled` are referenced in tests, and only within the package itself.

These constants were defined in anticipation of a live-process lifecycle tracker that does not yet exist. They clutter the exported API with states the runtime never emits.

**Suggested fix:**
Either remove the three constants and document the gap in a TODO comment in `SnapshotStatus`, or prefix them with `_` and a comment marking them as reserved until the live-process tracker is implemented. If they are part of a planned AG-UI/status-stream API, add a code comment referencing the relevant slice so the intent is clear.

---

### [LOW][NOT-WIRED] `ExportProfile`, `ImportProfile`, `RedactEnv`, `ImportOptions` have no production callers

**Location:** `internal/mcp/manager/config.go:19,46,77,13`
**Confidence:** high

**Detail:**
All four exported symbols are defined and thoroughly tested within the package, but no production code in `cmd/` or `internal/` (other than this package's own tests) imports or calls them. Verified by searching the entire repo for all import paths and call sites of `mcpmanager.ExportProfile`, `mcpmanager.ImportProfile`, `mcpmanager.RedactEnv`, `mcpmanager.ImportOptions`.

The `aura mcp profile` commands (`mcp_profile.go`) manipulate profiles directly via `mcp.SaveManagedConfig` without going through `ExportProfile`/`ImportProfile`. There is no `aura mcp export` or `aura mcp import` command.

This is not-wired rather than dead-code because the functions are designed for a future `mcp export/import` CLI verb. The tests confirm their correctness, but the wiring to a CLI entry-point is absent.

**Suggested fix:**
Either wire `ExportProfile`/`ImportProfile` to `aura mcp export`/`aura mcp import` subcommands in `mcp.go`, or mark the functions with a `// Future: ...` doc comment explaining they await the export/import CLI verbs. `RedactEnv` is a helper for `ExportProfile` and follows the same fate.

---

### [LOW][DEAD-CODE] `RuntimeLocal`, `RuntimeDocker`, `RuntimeDockerGateway` exported constants unused outside package

**Location:** `internal/mcp/manager/runtime.go:14-16`
**Confidence:** medium

**Detail:**
The three runtime-kind constants re-export the `mcp.RuntimeKind*` values under shorter names:

```go
RuntimeLocal         = mcp.RuntimeKindLocal
RuntimeDocker        = mcp.RuntimeKindDocker
RuntimeDockerGateway = mcp.RuntimeKindDockerGateway
```

No production code outside this package references `manager.RuntimeLocal`, `manager.RuntimeDocker`, or `manager.RuntimeDockerGateway` by the exported `manager.*` name — callers that need runtime kinds use `mcp.RuntimeKindLocal` etc. directly. Within the package they are used correctly; the exports are unreferenced aliases.

Confidence is medium (not high) because they are legitimately useful as part of the public API for future callers and cause no harm.

**Suggested fix:**
If the package's public contract intends callers to use `manager.RuntimeLocal` instead of `mcp.RuntimeKindLocal`, document the intent. Otherwise, unexport them (`runtimeLocal` etc.) and have the package-internal code use them privately, keeping `mcp.RuntimeKind*` as the public source of truth.

---

### [LOW][DEAD-CODE] `AuthBearerToken`, `AuthNotLoggedIn`, `AuthOAuth`, `AuthUnsupported` consumed only as string values, not as constants

**Location:** `internal/mcp/manager/status.go:20-23`
**Confidence:** low

**Detail:**
`cmd/aura/mcp_status.go` consumes `StatusSnapshot.AuthStatus` (the field) by printing it, not by switching on the exported constant names. No production code uses `manager.AuthBearerToken` etc. by identifier — only within the package's own tests. The constants are exported but their callers only care about the string values they represent (`"bearerToken"`, `"oAuth"`, etc.).

Confidence is low because this is a boundary style issue rather than a hard defect: the constants ARE the canonical source for those string values and should be exported for any future consumer who wants to switch on `AuthStatus`.

**Suggested fix:**
No code change required. If a future consumer in `cmd/` needs to switch on `AuthStatus` values, they should import the constants. Consider adding a `// AuthBearerToken, AuthOAuth, AuthNotLoggedIn, AuthUnsupported are the AuthStatus values emitted by SnapshotStatus.` doc comment on `StatusSnapshot.AuthStatus` to make the connection explicit.

---

## Checked and found clean

- **Nil-pointer derefs**: `doc.MCPServers[name]` reads in `RunnableManagedServers` are guarded by the profile's server list, which is pre-filtered by `ProfileServerNames`; a missing key yields a zero-value `ManagedServer` (harmless).
- **Goroutine leaks / concurrency**: package is stateless; no goroutines, no shared mutable state, no maps written concurrently.
- **Context propagation**: no I/O in this package; context is not relevant.
- **Resource leaks**: no files, connections, or rows opened.
- **Error wrapping (`%w`)**: all error paths use `fmt.Errorf("...: %w", err)` correctly; `errMCPServerBlocked` is wrapped with `%w` and checked with `errors.Is`.
- **Logic / inverted conditions**: `isStreamableHTTPServer` and `normalizedTrustForServer` were cross-checked against their test table; no inverted predicates found.
- **Slice aliasing**: `mergeEnvPreserveCredentials` uses `append([]string(nil), ...)` for the final copy; no aliasing.
- **`ImportProfile` always returns nil**: vestigial `error` return type; no current failure path exists. Not a correctness bug.
- **`LookupCatalog` O(N) per call**: with 4 entries the cost is negligible; not flagged.
