# Audit: internal/agent/mcptools

**Verdict:** needs-work — one routing bug silently discards Docker/gateway runtime transformation for servers that carry both a URL and a Command field.

**Counts:** critical 0 / high 1 / medium 1 / low 1

## Findings

---

### [HIGH][BUG] Docker/gateway runtime transform silently discarded for URL+Command servers

**Location:** `internal/agent/mcptools/mount.go:47-56`
**Confidence:** high

**Detail:**

`openManagedServer` routes any server whose `URL` field is non-empty to `mcp.OpenServer`,
regardless of whether `Command` is also set:

```go
if strings.TrimSpace(server.Type) == mcp.ServerTypeStreamableHTTP ||
    strings.TrimSpace(server.URL) != "" {
    return mcp.OpenServer(ctx, name, server)   // HTTP path
}
cfg, err := mcpmanager.RuntimeLaunchConfig(name, server) // stdio path
```

`mcp.OpenServer` internally calls `normalizedServerType`, which classifies
`{Type:"", URL:"...", Command:"cmd"}` as **stdio** (URL is only the HTTP signal when
`Command` is absent). So `mcp.OpenServer` falls through to `mcp.Open(Command)` — the
URL is ignored at runtime.

More importantly, the `mcpmanager.RuntimeLaunchConfig` call is skipped for such a
server. `RuntimeLaunchConfig` is the only place that transforms Docker (`Runtime.Kind =
"docker"`) and Docker-gateway (`Runtime.Kind = "docker_gateway"`) servers into their
correct `docker run …` / `docker mcp gateway run …` command shapes. A server configured
as `{URL:"unused", Runtime:{Kind:"docker", Image:"ghcr.io/..."}, Command:"leftover"}`
silently launches with the raw `Command` instead of the composed Docker invocation,
defeating container isolation entirely.

The condition in `openManagedServer` should mirror `mcp.managed_config.go`'s
`normalizedServerType` (URL is HTTP only when `Command` is also absent), OR delegate
type classification to a shared helper.

Contrast: `internal/mcp/manager/runtime.go`'s `isStreamableHTTPServer` uses the same
`URL != ""` shorthand as `openManagedServer`, so the manager layer and the mcptools
layer agree — but both diverge from the persistence-layer validator
(`managed_config.go:normalizedServerType`) that a user could inspect.

**Suggested fix:**

```go
func openManagedServer(ctx context.Context, name string, server mcp.ManagedServer) (mcp.Transport, error) {
    // Use the same classification as the persistence layer so a URL+Command
    // server is correctly identified as stdio and goes through RuntimeLaunchConfig.
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

(Requires exporting `normalizedServerType` from `internal/mcp` or moving it to a
shared helper, which also fixes the identical divergence in `runtime.go`.)

---

### [MEDIUM][BUG] `emptyObjectSchema` shared-slice aliasing — latent, safe today but fragile

**Location:** `internal/agent/mcptools/bridge.go:32,85`
**Confidence:** medium

**Detail:**

```go
var emptyObjectSchema = json.RawMessage(`{"type":"object"}`)
// ...
params := emptyObjectSchema  // copies the slice header, shares backing array
```

Every `bridgedTool` whose server advertised no schema gets a `Parameters` field that
aliases `emptyObjectSchema`'s backing array. Currently no consumer mutates `Parameters`
in place: `manifest.go` copies via `[]byte(s.Parameters)` and `append([]byte(nil), ...)`,
and `json.Unmarshal` reads without writing. So no corruption occurs today.

However, any future consumer that unmarshals *into* `spec.Parameters` (instead of *from*
it) would silently corrupt the shared sentinel and break all no-schema tools in the same
registry instance. The fix is a zero-cost one-liner.

**Suggested fix:**

```go
// In bridgeTools, force an independent copy so no bridgedTool aliases the sentinel:
params := append(json.RawMessage(nil), emptyObjectSchema...)
```

Or declare `emptyObjectSchema` as a function that returns a fresh slice each call.

---

### [LOW][BUG] Unchecked type assertion in `registerBridged` — will panic on non-`*bridgedTool` input

**Location:** `internal/agent/mcptools/bridge.go:128`
**Confidence:** medium

**Detail:**

```go
for _, t := range bridged {
    bt := t.(*bridgedTool)  // panics if t is not *bridgedTool
```

`registerBridged` is unexported and currently only called by `Mount` (which calls
`Bridge`/`bridgeTools`, which only appends `*bridgedTool`). The call graph is safe.
But `registerBridged`'s signature accepts `[]tools.Tool` — any future caller passing
externally-constructed tools (e.g. in a test or a composed mount) would get a
non-descriptive panic instead of a typed error.

**Suggested fix:**

```go
bt, ok := t.(*bridgedTool)
if !ok {
    return nil, fmt.Errorf("mcp bridge: unexpected tool type %T (internal error)", t)
}
```

---

## What was checked and found clean

- **Nil-pointer paths:** `Bridge` → `bridgeTools` → `registerBridged` chain: no nil
  dereferences. `bridgedTool.Execute` guards `len(raw) > 0` before unmarshal. The
  `emptyObjectSchema` fallback is always non-nil.
- **Error propagation:** All errors from `ListTools`, `CallTool`, `mcp.Open`,
  `mcp.OpenServer`, `RuntimeLaunchConfig`, and `reg.Get` are checked and either
  returned or converted to tool-level error content per the stated contract.
- **All-or-nothing registration:** `registerBridged` validates the full batch (both
  raw-duplicate and namespaced-collision checks) before the second loop that calls
  `reg.Register`. No partial registration is possible.
- **Collision disambiguation:** `hashSuffix` key is `bt.spec.Name + "\x00" + bt.name`
  (raw wire name), so three tools with distinct raw names that sanitize to the same
  namespaced form each get a distinct suffix. The re-truncation before appending the
  suffix keeps the final name within `maxToolNameLen`.
- **Name length invariant:** `namespacedName` is proven correct for all edge cases
  (long namespace, long tool, long namespace + long tool, prefix > budget). The
  `nsChars = max(budget-len(nsDelimiter), 0)` guard ensures `__` always survives, and
  the slice index `sanitizeName(namespace)[:nsChars]` is always in-bounds when
  `len(prefix) > budget` (proven: nsChars < len(sanitizeName(namespace))).
- **Dead code:** All unexported functions (`bridgeTools`, `registerBridged`, `firstLine`,
  `requiredArgsHint`, `sanitizeName`, `hashSuffix`, `namespacedName`, `openManagedServer`)
  are reachable from exported entry points. All exported symbols are used in production
  code (`cmd/aura/main.go`).
- **Goroutines / races:** This package spawns no goroutines. All state is local to each
  call. No concurrent access is possible within this package.
- **Context propagation:** `ctx` is threaded from every public function down to `ListTools`,
  `CallTool`, `mcp.Open`, and `mcp.OpenServer`. No context is dropped or replaced with
  `context.Background()`.
- **Resource leaks:** `MountServer` and `MountManagedServer` both call `cli.Close()` /
  `srv.Close()` on the error path before returning. The happy path returns the closer
  for the caller to call at shutdown. No leaks in the bridge or naming code.
