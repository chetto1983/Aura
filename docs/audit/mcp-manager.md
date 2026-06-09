# Audit: internal/mcp/manager

**Verdict:** needs-work — three dead-code issues and one security-relevant logic divergence between two trust-normalisation functions.

**Counts:** critical 0 / high 1 / medium 1 / low 2

---

## Findings

---

### [HIGH][BUG] `normalizedTrustForServer` accepts unknown/invalid trust class, bypassing blocked gate

**Location:** `internal/mcp/manager/runtime.go:153-164`

**Confidence:** high

**Detail:**

`normalizedTrustForServer` (runtime.go) returns `server.Trust.Class` verbatim whenever it is non-empty
(line 154-155), without validating it against the known trust-class enum.
`RuntimeLaunchConfig` then only blocks when `trust == mcp.TrustBlocked` (line 85).

Consequence: a server whose `trust.class` field contains a typo or an unrecognised value (e.g.,
`"trusted_local_typo"`) will pass the blocked gate and be launched — contrary to the fail-safe default
of `TrustBlocked` for unknown servers.

By contrast, the sister function `ManagedConfig.NormalizedTrust` in `internal/mcp/managed_config.go:220`
uses `isKnownTrust()` and falls through to source/type inference when the class is not recognised.
The two functions diverge on this input, making `RunnableManagedServers` / `RuntimeLaunchConfig` less
restrictive than `SnapshotStatus`.

Note: `SaveManagedConfig` calls `validateManagedServers` which rejects unknown trust classes on write,
so an in-memory-only or deserialized-but-not-re-saved config is the realistic attack surface (e.g., a
file edited directly on disk).

**Suggested fix:**

```go
func normalizedTrustForServer(server mcp.ManagedServer) string {
    if server.Trust.Class != "" && isKnownTrust(server.Trust.Class) {
        return server.Trust.Class
    }
    if strings.HasPrefix(strings.TrimSpace(server.Source), "recipe:") {
        return mcp.TrustTrustedRecipe
    }
    if server.Type == mcp.ServerTypeStreamableHTTP || strings.TrimSpace(server.URL) != "" {
        return mcp.TrustRemoteHTTP
    }
    return mcp.TrustBlocked
}
```

`isKnownTrust` is unexported in the `mcp` package; either export it or duplicate the switch locally
(it is 5 lines).

---

### [MEDIUM][DEAD-CODE] `StartupStarting`, `StartupReady`, `StartupFailed` constants are never referenced

**Location:** `internal/mcp/manager/status.go:13-15`

**Confidence:** high

**Detail:**

Three of the six `Startup*` constants are defined but never referenced anywhere outside their own
definition file. `StartupBlocked`, `StartupDisabled`, and `StartupUnknown` are used inside
`SnapshotStatus` and in tests; `StartupStarting`, `StartupReady`, and `StartupFailed` are not
referenced in any production or test code across the entire repo (verified with `grep -r` across
`D:/Aura`).

`SnapshotStatus` assigns exactly three states: `StartupDisabled`, `StartupBlocked`, and
`StartupUnknown` (the zero value). There is no runtime machinery that ever transitions a server to
`starting`, `ready`, or `failed` — those states belong to a lifecycle model that has not been
implemented. They are aspirational dead code.

**Suggested fix:**

Remove `StartupStarting`, `StartupReady`, and `StartupFailed` from `status.go` until the lifecycle
state machine is implemented. If they are intentionally reserved for future use, document them with a
`// TODO(slice-N):` comment; otherwise removing them keeps the surface honest.

---

### [LOW][DEAD-CODE] `ExportProfile`, `ImportProfile`, and `ImportOptions` are never called from production code

**Location:** `internal/mcp/manager/config.go:13-73`

**Confidence:** high

**Detail:**

`ExportProfile`, `ImportProfile`, and `ImportOptions` are exported and well-tested, but no non-test
caller exists anywhere in the repo. The `aura mcp profile` commands (`cmd/aura/mcp_profile.go`)
implement profile creation, switching, and membership management inline without calling these
functions. The `aura mcp install` command in `cmd/aura/mcp.go` also does not use them.

References verified: `grep` across `D:/Aura` finds only `config.go` (definition), `config_test.go`,
and `config_extra_test.go`.

These are not-wired public API — they exist for a profile-sharing workflow (`export` → share file →
`import`) that has not been wired to any CLI subcommand.

**Suggested fix:**

Either wire `ExportProfile` / `ImportProfile` to `aura mcp profile export <name>` and
`aura mcp profile import <file>` CLI subcommands (if the feature is intended), or mark them
unexported (`exportProfile`, `importProfile`) until they are wired, so they don't create a false
surface for callers.

---

### [LOW][BUG] `mergeEnvPreserveCredentials`: two consecutive `if` blocks are logically identical — second branch is unreachable

**Location:** `internal/mcp/manager/config.go:117-124`

**Confidence:** high

**Detail:**

```go
// line 117
if prior, ok := existingByKey[key]; ok && isSecretEnvKey(key) && isPlaceholderValue(key, value) {
    out = append(out, prior)
    continue
}
// line 121
if prior, ok := existingByKey[key]; ok && isSecretEnvKey(key) && !isPlaceholderValue(key, value) {
    out = append(out, prior)
    continue
}
```

The two guards differ only in `isPlaceholderValue` vs `!isPlaceholderValue` — their union covers all
cases where `existingByKey[key]` exists and `isSecretEnvKey(key)` is true. The second `if` block is
thus unreachable by any path that was not already caught by the first, making it dead code.

Both blocks perform the same action (`out = append(out, prior); continue`), so there is no observable
misbehavior today. However, the split into two visually distinct branches implies different intended
behavior (e.g., the second branch was perhaps meant to use `entry` rather than `prior` to accept an
incoming real credential on overwrite). The current code silently keeps the existing credential for
both placeholder and real incoming values, which is the tested and documented behavior, but the
redundant branch is a maintenance trap — a future maintainer might change one branch without
noticing the other is identical.

**Suggested fix:**

Collapse the two guards into one:

```go
if prior, ok := existingByKey[key]; ok && isSecretEnvKey(key) {
    // Always preserve the stored credential over any incoming value
    // (placeholder or real) when OverwriteCredentials is false.
    out = append(out, prior)
    continue
}
```

---

## What was checked

- All four non-test `.go` files: `config.go`, `runtime.go`, `status.go`, `catalog.go` (total ~400 LOC).
- All test files read to establish intended behaviour contracts.
- `internal/mcp/managed_config.go` read for type definitions and `NormalizedTrust` comparison.
- `cmd/aura/mcp.go`, `cmd/aura/mcp_profile.go`, `cmd/aura/mcp_status.go`, `cmd/aura/mcp_tools.go`,
  `internal/config/config.go`, `internal/agent/mcptools/mount.go` inspected for wiring.
- Grep across entire repo for every exported symbol and for every constant defined in the package.
- Go module version is 1.26 — loop-variable capture fix is in effect; no loop-capture issues exist.
- No goroutines, channels, mutexes, or shared state in this package — no race surface.
- No I/O, no DB, no HTTP clients, no `defer` usage — no resource-leak surface.
- `errMCPServerBlocked` is package-private sentinel used correctly with `errors.Is`.
