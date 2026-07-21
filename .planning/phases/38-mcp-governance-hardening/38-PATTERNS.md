# Phase 38: MCP Governance Hardening - Pattern Map

**Mapped:** 2026-07-18
**Files analyzed:** 29 (11 new, 18 modified, incl. tests)
**Analogs found:** 27 / 29 (2 have no true analog — flagged in "No Analog Found")

This phase is a **consolidation/wiring phase**: nearly every new behavior has a working, proven analog already shipped elsewhere in this exact repository. The dominant pattern is "the fix already exists at site A; site B still has the pre-fix shape" — so most Pattern Assignments below cite an **in-repo sibling**, not an external reference. Per RESEARCH.md's corrections, `cmd/aura/main.go` (not `manager/runtime.go`) is the real mount/shutdown loop, and `cmd/aura/mcp_status.go` (not `cmd/aura/doctor.go`'s `doctorProbeMCPBinary`) is the real F-046 probe-skip bug — both verified directly below with line numbers.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/mcp/classify.go` (NEW) | utility (pure decision fn) | transform | `internal/mcp/managed_config.go` `normalizedServerType`/`NormalizedTrust` (217-235, 301-315) + `manager/runtime.go` `normalizedTrustForServer`/`isStreamableHTTPServer` (166-181) | exact — these ARE the bodies being unified |
| `internal/mcp/classify_test.go` (NEW) | test | transform | `internal/mcp/managed_config_test.go` `TestValidateManagedServers` table style (299-351) | exact |
| `internal/mcp/managed_config.go` (MODIFIED) | model/utility | CRUD (config read/write) | self — thin-wrapper refactor onto `Classify` | exact (self) |
| `internal/mcp/managed_config_identity.go` (MODIFIED) | utility (overlay merge) | CRUD | self — `MountForIdentity` (150-183), `SetTrustForIdentity` (194-201) | exact (self) |
| `internal/mcp/transport.go` (MODIFIED) | utility (dispatcher) | request-response | self — `OpenServer` (21-32) + `AURA_MCP_SSRF_ENFORCE` gate template (38-45) | exact (self) |
| `internal/mcp/client.go` (MODIFIED: `readResponseBlocking`, `killProcess`, `Open`) | service (stdio transport) | streaming (frame read) + process lifecycle | D-10: `internal/agent/tools/shell_exec_{unix,windows}.go` (verbatim). D-08: stdlib `bufio.Scanner`. D-06: self, two-context split | exact (D-10 verbatim reuse target) |
| `internal/mcp/client_unix.go` (NEW, or shared `internal/procgroup`) | utility (OS process mgmt) | event-driven (signal) | `internal/agent/tools/shell_exec_unix.go` (full 19-LOC file) | exact — verbatim reuse |
| `internal/mcp/client_windows.go` (NEW, or shared `internal/procgroup`) | utility (OS process mgmt) | event-driven | `internal/agent/tools/shell_exec_windows.go` (full 35-LOC file) | exact — verbatim reuse |
| `internal/mcp/manager/runtime.go` (MODIFIED: `isStreamableHTTPServer`, `normalizedTrustForServer`) | service (eligibility resolver) | CRUD (config → runnable set) | `internal/mcp/classify.go` (once it exists) | exact (post-refactor) |
| `internal/mcp/manager/status.go` (MODIFIED: `runtimeName`) | service (status projection) | transform | `internal/mcp/classify.go` | exact |
| `internal/agent/mcptools/mount.go` (MODIFIED: `isStreamableHTTPManagedServer`) | service (mount dispatcher) | request-response | `internal/mcp/classify.go` | exact |
| `internal/agent/mcptools/mount_retry.go` or new sibling (MODIFIED/NEW — bounded handshake ctx) | service (retry/timeout wrapper) | event-driven (backoff) | self `MountWithRetry` (47-75) — **caution**: `bridge_reconnect.go`'s `openReplacement` (229-246) is a superficially-similar two-context shape but may itself carry the Pitfall #2 bug class (see Pattern Assignments, flagged, do not copy verbatim without verifying) | role-match, with caution flag |
| `cmd/aura/main.go` (MODIFIED: `buildRegistryWithMCP`, `closeMCPServers`) | controller (composition root / CLI boot) | batch (sequential loop) → concurrent fan-out | self (`buildRegistryWithMCP` 224-275, `closeMCPServers` 301-309) | exact (self) |
| `cmd/aura/mcp.go` (MODIFIED: `mcpAdd`/`mcpInstall`/`mcpSetEnabled`/`mcpRemove`) | controller (CLI subcommand) | CRUD | `cmd/aura/serve_governance_write.go` `mcpWriteAdapter.InstallServer`/`SetEnabled`/`RemoveServer` (39-169) | exact — same ops, adapter already audits correctly |
| `cmd/aura/mcp_profile.go` (MODIFIED: `mcpTrust` + profile add/remove) | controller (CLI subcommand) | CRUD | `cmd/aura/serve_governance_write.go` `TrustApprove` (96-127) — **with the dead-fallback caution**, Pitfall #5 | exact, with caution flag |
| `cmd/aura/mcp_audit_actor.go` (NEW) | utility | transform | none — RESEARCH confirms zero existing `os/user` usage repo-wide; closest shape is `recover_operator.go`'s graceful-degrade-chain style | no true analog — new pattern, shape only |
| `main.go`'s `case "mcp":` dispatch + `runMCP`/`runMCPCommand` (MODIFIED — pool threading) | controller (composition wiring) | request-response | `cmd/aura/identity.go` `runIdentity` → `identityRecover(ctx, store, pool, args)` and `recover_operator.go`'s `identityRecoverOperator(ctx, pool, cfg, args)` | exact |
| `internal/config/config_validate.go` (MODIFIED: new `gateMCPLegacyEnv`) | config (validation gate) | transform | self `gateDestructiveShell` (210-219) | exact — verbatim template |
| `internal/config/config_knobs.go` (MODIFIED: new `KnobSpec` row) | config (registry) | transform | self `knobRegistry` `KindBool` rows (e.g. line 77 `AURA_AGUI_CORS_PERMISSIVE`) | exact |
| `cmd/aura/mcp_status.go` (MODIFIED: `writeRuntimeCheck`, `mcpStatus`) | controller (CLI + status render) | request-response | `internal/mcp/probe.go` `ProbeServer` (already correct) + `internal/agui/governance_api.go` `handleMCPProbe` (228-249, ctx.WithTimeout wiring) | exact |
| `cmd/aura/doctor.go` (MODIFIED: new 6th check) | controller (health check registry) | request-response | self `doctorChecks()` (89-97), any `doctorProbe` shape e.g. `defaultDoctorProbeNeo4j` (113-127) | exact |
| `cmd/aura/serve_governance_write.go` (MODIFIED: `TrustApprove` validation) | service (write adapter) | CRUD | `internal/agui/governance_write_api.go` `handleMCPTrust` (112-133) — the validation to hoist in/share | exact |
| `internal/mcp/managed_config_test.go` (MODIFIED — deliberately rewrite 2 tests) | test | transform | self | exact |
| `internal/mcp/managed_config_identity_test.go` (MODIFIED — new remote fixture) | test | CRUD | self (`TestSetTrustForIdentityOverlaysClassA`, ~line 116) | exact |
| `internal/mcp/client_test.go` or new `client_frame_test.go` (NEW/MODIFIED) | test | streaming | self `fakeServer`/`newTestPair` (19-96) | exact |
| `internal/mcp/client_open_test.go` (MODIFIED — new `TestHelperProcess` modes) | test | event-driven (subprocess) | self `TestHelperProcess` (18-77) | exact |
| `cmd/aura/main_test.go` or new file (NEW) | test | batch/concurrent | `internal/mcp/client_timeout_test.go` (ctx-timeout pattern, 3 tests) | role-match |
| `cmd/aura/mcp_audit_integration_test.go` (NEW, `db_integration`) | test | CRUD (DB) | `internal/mcp/manager/audit.go` header cites `internal/skills/audit_store.go` / `internal/identity/audit_store.go` as the origin store-test pattern (not directly re-read here; MCPAuditStore itself is fully read below) | role-match (cited, not re-verified) |
| `internal/config/config_validate_test.go` (MODIFIED — new `TestGateMCPLegacyEnv`) | test | transform | self `TestGateDestructiveShell` (157-183) | exact |

## Pattern Assignments

### Cluster 1 — Canonical transport classifier (D-01, D-02; MCPH-01)

#### `internal/mcp/classify.go` (NEW)

**Analogs:** `internal/mcp/managed_config.go` (the two functions being unified) + `internal/mcp/manager/runtime.go` (the duplicate, independently-reimplemented copy — must also collapse).

**The exact bug being fixed** — `normalizedServerType` silently resolves ambiguity to stdio (F-027), `internal/mcp/managed_config.go:301-315`:
```go
func normalizedServerType(cfg ManagedServer) string {
	switch strings.TrimSpace(cfg.Type) {
	case "":
		if strings.TrimSpace(cfg.URL) != "" && strings.TrimSpace(cfg.Command) == "" {
			return ServerTypeStreamableHTTP
		}
		return ServerTypeStdio          // <-- mixed url+command falls through to HERE, silently
	case ServerTypeStdio:
		return ServerTypeStdio
	case ServerTypeStreamableHTTP:
		return ServerTypeStreamableHTTP
	default:
		return strings.TrimSpace(cfg.Type)
	}
}
```

**The exact F-013 auto-promote bug** — `internal/mcp/managed_config.go:217-235` (`NormalizedTrust`):
```go
// NormalizedTrust resolves a server's effective trust class, inferring it from the
// recipe source or HTTP type when unset and defaulting to TrustBlocked for unknown
// or missing servers.
func (c ManagedConfig) NormalizedTrust(name string) string {
	server, ok := c.MCPServers[name]
	if !ok {
		return TrustBlocked
	}
	if isKnownTrust(server.Trust.Class) {
		return server.Trust.Class
	}
	if strings.HasPrefix(strings.TrimSpace(server.Source), "recipe:") {
		return TrustTrustedRecipe
	}
	if normalizedServerType(server) == ServerTypeStreamableHTTP {
		return TrustRemoteHTTP        // <-- D-03/F-013: MUST become TrustBlocked instead
	}
	return TrustBlocked
}
```

**The independently-duplicated copy of the SAME bug** — `internal/mcp/manager/runtime.go:166-181` (must ALSO be migrated onto `Classify`, not just `managed_config.go`):
```go
func normalizedTrustForServer(server mcp.ManagedServer) string {
	if mcp.IsKnownTrust(server.Trust.Class) {
		return server.Trust.Class
	}
	if strings.HasPrefix(strings.TrimSpace(server.Source), "recipe:") {
		return mcp.TrustTrustedRecipe
	}
	if server.Type == mcp.ServerTypeStreamableHTTP || strings.TrimSpace(server.URL) != "" {
		return mcp.TrustRemoteHTTP    // <-- same bug, duplicated
	}
	return mcp.TrustBlocked
}

func isStreamableHTTPServer(server mcp.ManagedServer) bool {
	return strings.TrimSpace(server.Type) == mcp.ServerTypeStreamableHTTP || strings.TrimSpace(server.URL) != ""
}
```

**Enum vocabulary to reuse (no new trust vocabulary)** — `internal/mcp/managed_config.go:14-30`:
```go
const (
	ServerTypeStdio          = "stdio"
	ServerTypeStreamableHTTP = "streamable_http"

	TrustTrustedRecipe  = "trusted_recipe"
	TrustTrustedLocal   = "trusted_local"
	TrustSandboxedLocal = "sandboxed_local"
	TrustRemoteHTTP     = "remote_http"
	TrustBlocked        = "blocked"
	...
)
```

**Exported gate other packages already use** — `internal/mcp/managed_config.go:326-331`:
```go
// IsKnownTrust reports whether class is a recognized trust class. It lets callers
// outside this package (e.g. the manager runtime) gate an explicit Trust.Class the
// same way NormalizedTrust does, instead of trusting an arbitrary string.
func IsKnownTrust(class string) bool {
	return isKnownTrust(class)
}
```
`Classify` should live beside these and reuse `isKnownTrust`/the trust constants verbatim — no new vocabulary, per CONTEXT.md D-01.

**Package doc convention to follow** (imports/comment style) — `internal/mcp/managed_config.go:1-10`:
```go
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)
```

#### Call sites to migrate onto `Classify` (all 6 security-gating ones, per RESEARCH's 9-site inventory)

1. `internal/mcp/transport.go:22` `OpenServer` — dispatch gate (**highest priority**, this is the "never call stdio Open for an HTTP server" gate):
```go
func OpenServer(ctx context.Context, name string, server ManagedServer) (Transport, error) {
	if normalizedServerType(server) == ServerTypeStreamableHTTP {
		headers, bearer := httpAuthFromEnv(server.Env)
		return OpenHTTP(ctx, name, HTTPConfig{
			URL: server.URL, Headers: headers, BearerToken: bearer, Enforce: ssrfEnforceFromEnv(),
		})
	}
	return Open(ctx, name, ServerConfig{Command: server.Command, Args: server.Args, Env: server.Env})
}
```
2. `internal/mcp/managed_config.go:249` `validateManagedServers` — the `switch normalizedServerType(cfg)` dispatch.
3. `internal/mcp/manager/runtime.go:60,63,88` `RunnableManagedServers`/`RuntimeLaunchConfig` — trust-blocked skip + HTTP-vs-stdio branch.
4. `internal/agent/mcptools/mount.go:36,66-68` `MountManagedServer`/`isStreamableHTTPManagedServer` — the actual mount-time gate:
```go
func isStreamableHTTPManagedServer(server mcp.ManagedServer) bool {
	return strings.TrimSpace(server.Type) == mcp.ServerTypeStreamableHTTP || strings.TrimSpace(server.URL) != ""
}
```
5. `internal/mcp/manager/status.go:90-98` `runtimeName` — display-only, lower priority, but should still migrate (leaving it on old logic re-creates the scattered-checks problem).
6. `cmd/aura/mcp_status.go:98` `writeRuntimeCheck` — inline `server.Type == mcp.ServerTypeStreamableHTTP || server.URL != ""` (also the F-046 probe-skip site, see Cluster 6).

---

### Cluster 2 — Remote-trust elevation guard (D-04; MCPH-02)

#### `internal/mcp/managed_config_identity.go` (MODIFIED)

**Analog:** self. `MountForIdentity` currently lets a per-identity overlay set trust on ANY non-shared-admin-governed server, with no transport check — `internal/mcp/managed_config_identity.go:150-183`:
```go
func MountForIdentity(identity string) (ManagedConfig, error) {
	sharedPath, err := ManagedConfigPath()
	...
	for name, srv := range shared.MCPServers {
		if !IsSharedAdminGoverned(srv) {
			if pref, ok := overlay.Preferences[name]; ok {
				if pref.Enabled != nil {
					srv.Enabled = pref.Enabled
				}
				if strings.TrimSpace(pref.Trust.Class) != "" {
					srv.Trust = pref.Trust   // <-- D-04: must NOT allow this for a remote (streamable_http) server
				}
			}
		}
		eff.MCPServers[name] = srv
	}
	return eff, nil
}
```
The guard needs a `Classify`/`normalizedServerType`-based check here: if `srv` classifies as `ServerTypeStreamableHTTP`, skip applying `pref.Trust` (silently ignore, mirroring how `IsSharedAdminGoverned` is silently ignored today) or return an error — Claude's Discretion on exact error-vs-ignore shape, but the STRUCTURE (an `if !IsSharedAdminGoverned(srv) { ... }` guard clause) is the exact place to add the second condition.

**Write-side counterpart** — `internal/mcp/managed_config_identity.go:194-201` `SetTrustForIdentity` (the mutation path a CLI/web per-identity trust-set call would hit; needs the SAME remote check before persisting):
```go
func SetTrustForIdentity(identity, name, class string) error {
	if !isKnownTrust(class) {
		return fmt.Errorf("mcp: unknown trust class %q", class)
	}
	return mutateIdentityPref(identity, name, func(p *IdentityServerPref) {
		p.Trust = ManagedTrust{Class: strings.TrimSpace(class)}
	})
}
```
And the shared guard function it delegates to, `mutateIdentityPref` (`managed_config_identity.go:206-236`), already has the exact shape to extend (it looks up `srv` from the shared catalog before mutating — the remote check slots in right after `IsSharedAdminGoverned`):
```go
func mutateIdentityPref(identity, name string, mutate func(*IdentityServerPref)) error {
	...
	srv, ok := shared.MCPServers[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownServer, name)
	}
	if IsSharedAdminGoverned(srv) {
		return fmt.Errorf("%w: %q", ErrSharedAdminGoverned, name)
	}
	// D-04 guard slots in HERE — reject/ignore a remote (streamable_http) trust elevation.
	...
}
```

**Sentinel-error convention to mirror** — `internal/mcp/managed_config_identity.go:27-34`:
```go
var (
	ErrSharedAdminGoverned = errors.New("mcp: server is shared admin-governed (class-b), not per-identity toggleable")
	ErrUnknownServer = errors.New("mcp: server not found in shared catalog")
)
```
A new `ErrRemoteElevationForbidden`-style sentinel (naming Claude's Discretion) following this exact `errors.New("mcp: ...")` convention is the natural extension point.

---

### Cluster 3 — Bounded mount lifecycle (D-06, D-07, D-11; MCPH-04, MCPH-06)

**CRITICAL — RESEARCH correction verified live:** the sequential mount loop CONTEXT.md cites at `manager/runtime.go:34` is `RuntimeServers`, which only builds launch descriptors — confirmed, `internal/mcp/manager/runtime.go:28-48`, no `mcp.Open`/`OpenServer` call anywhere in it. The REAL spawn loop is below.

#### `cmd/aura/main.go` `buildRegistryWithMCP` (MODIFIED) — the real per-server mount loop, `cmd/aura/main.go:224-275`:
```go
func buildRegistryWithMCP(ctx context.Context, cfg *config.Config, ts *cronTaskStore, sandboxRouter *usersandbox.SandboxRouter) (*tools.Registry, runtimeToolHandles, []func() error, error) {
	...
	closers := make([]func() error, 0, len(serverNames))
	for _, name := range serverNames {
		mountOnce := func(c context.Context) (func() error, []string, error) {
			if _, managed := cfg.MCPPolicies[name]; managed {
				return mcptools.MountManagedServer(c, reg, name, cfg.MCPPolicies[name])
			}
			return mcptools.MountServer(c, reg, name, cfg.MCPServers[name])
		}
		closer, mounted, err := mcptools.MountWithRetry(ctx, name, mcpMountRetryPolicy(), mountOnce)
		if err != nil {
			slog.Warn("mcp mount failed", "server", name, "err", err)
			continue          // <-- D-07: this WARN-and-continue already exists; extend with "unhealthy" marking
		}
		slog.Info("mcp mounted", "server", name, "tools", len(mounted))
		closers = append(closers, closer)
	}
	return reg, handles, closers, nil
}
```
**Where the D-06 timeout wraps in:** `mountOnce` is what `MountWithRetry` calls per attempt; today `ctx` (the SAME `ctx` passed to `buildRegistryWithMCP`, i.e. the daemon's long-lived boot ctx) flows straight through to `mcp.Open`'s `exec.CommandContext(ctx, ...)`. Per RESEARCH Pattern 2 / Pitfall #2, the fix must derive a SEPARATE, narrower `context.WithTimeout` **only for the handshake**, not for the process-lifetime ctx — this requires either (a) a new parameter threaded through `MountServer`/`MountManagedServer` → `mcp.Open`, or (b) `mcp.Open`'s signature gaining a second ctx/option. See Cluster 5 for the two-context split itself.

**Existing retry-budget constants to extend, not duplicate** — `cmd/aura/main.go:277-299`:
```go
const (
	defaultMCPMountAttempts  = 6
	defaultMCPMountBaseDelay = time.Second
	defaultMCPMountMaxDelay  = 5 * time.Second
)

func mcpMountRetryPolicy() mcptools.MountRetryPolicy {
	policy := mcptools.MountRetryPolicy{
		Attempts: defaultMCPMountAttempts, BaseDelay: defaultMCPMountBaseDelay, MaxDelay: defaultMCPMountMaxDelay,
	}
	if v := strings.TrimSpace(os.Getenv("AURA_MCP_MOUNT_RETRY_ATTEMPTS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			policy.Attempts = n
		}
	}
	return policy
}
```
This is the EXACT env-knob-override style to mirror for `AURA_MCP_MOUNT_TIMEOUT` (D-06) — a package-level `const default... = 10 * time.Second` + a small resolver function reading the env override via `strconv`/`envutil.IntDefault`.

#### `cmd/aura/main.go` `closeMCPServers` (MODIFIED) — the real shutdown loop, `cmd/aura/main.go:301-309`:
```go
func closeMCPServers(closers []func() error) error {
	var first error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i](); err != nil && first == nil {
			first = err
		}
	}
	return first
}
```
Per Pitfall #7, each individual closer is ALREADY bounded (`closeWaitTimeout = 5s` stdio, `httpCloseTimeout = 5s` HTTP — do not touch these constants). The gap is purely "sequential, no aggregate deadline" — D-11 fans this out concurrently under ONE `context.WithTimeout`. `golang.org/x/sync/errgroup` is already a direct dependency (used elsewhere in this codebase's tree per RESEARCH) — no new import needed, just this file gains an `errgroup.Group` fan-out + `context.WithTimeout(ctx, AURA_MCP_SHUTDOWN_TIMEOUT)`.

**⚠ Flagged caution — do not copy `bridge_reconnect.go`'s `openReplacement` verbatim as "the proven two-context pattern":** `internal/agent/mcptools/bridge_reconnect.go:229-246`:
```go
func (s *reconnectingServer) openReplacement(parent context.Context, timeout time.Duration) ([]mcp.ToolDef, reconnectingClient, error) {
	if timeout <= 0 {
		timeout = defaultMCPReconnectTimeout
	}
	reconnectCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), timeout)
	defer cancel()

	next, err := openMCPClient(reconnectCtx, s.name, s.cfg)   // openMCPClient == mcp.Open
	...
	defs, err := next.ListTools(reconnectCtx)
	...
	return defs, next, nil
}
```
This passes `reconnectCtx` (the SAME bounded ctx) into `openMCPClient`/`mcp.Open`, which internally does `exec.CommandContext(ctx, ...)` (`client.go:109`). Per stdlib semantics, `exec.CommandContext`'s cancellation watcher stays armed for the process's WHOLE lifetime between `Start()` and `Wait()` — and `Wait()` is only called inside `Client.Close()`, not here. The `defer cancel()` fires the instant `openReplacement` returns (success path included), which would cancel `reconnectCtx` — the exact ctx tied to the subprocess's `exec.CommandContext` — **while the just-reconnected process is still running and has not yet been `Wait()`-ed**. This is structurally the SAME risk class as Pitfall #2 (a bounded ctx doubling as both handshake-deadline and process-lifetime ctx). Whether this manifests as an actual observed bug in the existing reconnect path was NOT verified by execution during this pattern-mapping pass (no test was run) — flagging it here so the planner either (a) verifies it empirically before treating this function as a "proven good" reference, or (b) fixes both call sites with the same corrected two-context shape in one pass. Do not present this function as the safe template without that check.

---

### Cluster 4 — Stdio frame cap + process-tree kill (D-08, D-09, D-10; MCPH-05, MCPH-06)

#### `internal/mcp/client.go` `readResponseBlocking` (MODIFIED) — the exact unbounded-read bug, `internal/mcp/client.go:350-371`:
```go
func (c *Client) readResponseBlocking(want int64) (json.RawMessage, error) {
	for {
		line, err := c.stdout.ReadBytes('\n')     // <-- F-034: unbounded growth until '\n' or EOF
		if err != nil {
			return nil, fmt.Errorf("%w: recv: %w%s", ErrTransport, err, c.stderrTail())
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var resp rpcResp
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if resp.ID == nil || *resp.ID != want {
			continue // a notification or an out-of-band/earlier id — skip
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}
```
Replace `c.stdout` (currently `*bufio.Reader`, constructed at `client.go:128` `bufio.NewReader(stdoutPipe)`) with a `*bufio.Scanner` + `.Buffer(make([]byte,0,4096), maxFrame)`, checking `errors.Is(scanner.Err(), bufio.ErrTooLong)` for the D-09 abort path, per RESEARCH's Code Example #3 (already fully sketched there — reuse it directly, it was written against this exact function's shape).

**Field to change** — `internal/mcp/client.go:75-88` (`Client` struct — `stdout` field type changes from `*bufio.Reader` to `*bufio.Scanner`, or a new field is added alongside):
```go
type Client struct {
	name            string
	cmd             *exec.Cmd
	stdin           io.WriteCloser
	stdout          *bufio.Reader     // <-- becomes *bufio.Scanner (or a sibling field)
	stdoutCloser    io.Closer
	stderr          *boundedbuffer.Buffer
	...
}
```
**⚠ `boundedbuffer.Buffer` is the WRONG fit for this** (confirmed by direct read, `internal/boundedbuffer/buffer.go:23-40`):
```go
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)
	if b.limit <= 0 {
		return written, nil
	}
	if len(p) >= b.limit {
		b.buf = append(b.buf[:0], p[len(p)-b.limit:]...)   // <-- silently keeps NEWEST bytes, never errors
		return written, nil
	}
	...
}
```
It has no `Read` method and never errors on overflow — it is Write-only and used TODAY only for `c.stderr` tail-capture (`client.go:119` `stderr := boundedbuffer.New(0)`). Leave that usage untouched; do not route the D-08 frame cap through this type — it would silently truncate instead of deterministically aborting (opposite of D-09's requirement).

#### `internal/mcp/client.go` `killProcess` (MODIFIED) — the exact single-PID-only bug, `internal/mcp/client.go:460-464`:
```go
func (c *Client) killProcess() {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()     // <-- F-035: kills only the tracked PID, leaks grandchildren
	}
}
```

#### Reuse source (VERBATIM, already shipped + CI-proven) — `internal/agent/tools/shell_exec_unix.go` (full file, 19 LOC):
```go
//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
```

`internal/agent/tools/shell_exec_windows.go` (full file, 35 LOC):
```go
//go:build windows

package tools

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	out, err := exec.Command("taskkill", "/F", "/T", "/PID", pid).CombinedOutput()
	if err == nil || taskkillProcessMissing(out) {
		return nil
	}
	return fmt.Errorf("taskkill process group %s: %w", pid, err)
}

func taskkillProcessMissing(out []byte) bool {
	msg := strings.ToLower(string(bytes.TrimSpace(out)))
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "not running") ||
		strings.Contains(msg, "no tasks are running")
}
```
**Confirmed: no `internal/procgroup` package exists yet** (verified via directory listing) — extracting these two files to a new shared package (name is Claude's Discretion) so both `internal/agent/tools` and `internal/mcp` import the identical implementation is the correct move per "never duplicate, extract a helper." Wiring point in `client.go`: call `setProcessGroup(cmd)` right before `cmd.Start()` in `Open` (`client.go:109-123`), and replace `killProcess`'s body with `killProcessGroup(c.cmd)`.

---

### Cluster 5 — Audited CLI writes (D-12, D-13; MCPH-07)

**Confirmed by direct read: every CLI MCP mutation bypasses the audited writer entirely today.**

#### `cmd/aura/mcp.go` `mcpAdd` (MODIFIED) — direct unaudited write, `cmd/aura/mcp.go:198-210`:
```go
doc.MCPServers[name] = mcp.ManagedServer{
	Command: command, Args: commandArgs, Env: env,
	Enabled: mcpBoolPtr(enabled), Source: "manual",
	Trust: mcp.ManagedTrust{Class: trustClass},
}
ensureProfileMembership(&doc, doc.ActiveProfileName(), name)
if err := mcp.SaveManagedConfig(path, doc); err != nil {   // <-- F-037: direct file write, no pool, no audit row
	return err
}
return writef(out, "ok: added %s in %s\n", name, path)
```

#### `cmd/aura/mcp_profile.go` `mcpTrust` (MODIFIED) — the exact Pitfall #12 gap: no `--reason`/`--class` flags, hardcoded class, `cmd/aura/mcp_profile.go:153-175`:
```go
func mcpTrust(args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: aura mcp trust <name>")
	}
	name := strings.TrimSpace(args[0])
	doc, path, err := loadManagedMCPConfig()
	...
	server.Trust = mcp.ManagedTrust{Class: mcp.TrustTrustedLocal}   // <-- hardcoded, no --class flag
	doc.MCPServers[name] = server
	if err := mcp.SaveManagedConfig(path, doc); err != nil {         // <-- unaudited, no --reason flag either
		return err
	}
	return writef(out, "ok: trusted %s as %s\n", name, mcp.TrustTrustedLocal)
}
```

#### The EXACT target shape to route through — `cmd/aura/serve_governance_write.go` `mcpWriteAdapter` (already correct, web-side), `serve_governance_write.go:96-127` (`TrustApprove`):
```go
func (a mcpWriteAdapter) TrustApprove(ctx context.Context, actor, name, class, reason string) (agui.MCPWriteResult, error) {
	doc, err := a.load()
	...
	server, ok := doc.MCPServers[name]
	...
	class = strings.TrimSpace(class)
	if class == "" {
		class = mcp.TrustTrustedLocal   // <-- ⚠ Pitfall #5: dead fallback, unreachable via HTTP (handleMCPTrust
	}                                    //     pre-validates before calling this) but LIVE if the CLI calls
	if !mcp.IsKnownTrust(class) {        //     TrustApprove directly without pre-validating class/reason first
		return agui.MCPWriteResult{}, fmt.Errorf("mcp trust: unknown trust class %q", class)
	}
	server.Trust = mcp.ManagedTrust{
		Class: class, ApprovedBy: actor, ApprovedAt: time.Now().UTC().Format(time.RFC3339), Reason: reason,
	}
	doc.MCPServers[name] = server

	if err := mcpmanager.WriteConfigWithAudit(ctx, a.pool, a.path, doc, mcpmanager.MCPAuditInsert{
		ActorIdentityID: actor, Action: "trust", ServerName: name, Reason: reason,
	}); err != nil {
		return agui.MCPWriteResult{}, err
	}
	return agui.MCPWriteResult{Name: name, Server: server, Probe: a.probe(ctx, name, server)}, nil
}
```
And the SAME adapter's `InstallServer`/`SetEnabled`/`RemoveServer` (`serve_governance_write.go:39-73, 129-169`) are the exact analogs for `mcpAdd`/`mcpInstall`/`mcpSetEnabled`/`mcpRemove` — all follow the identical shape: load → mutate → `mcpmanager.WriteConfigWithAudit(ctx, pool, path, doc, MCPAuditInsert{...})`.

**⚠ Flagged for the planner (Pitfall #5):** the dead `class == ""` fallback above must NOT become live for the CLI. Either hoist the validation from `handleMCPTrust` (below) into `TrustApprove` itself (single source of truth for both callers), or have `mcpTrust` call the identical check before invoking `TrustApprove`.

**Wire-boundary validation already shipped and tested (verify-and-guard only, MCPH-03/D-05)** — `internal/agui/governance_write_api.go:112-133` `handleMCPTrust`:
```go
func (s *Server) handleMCPTrust(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.beginMCPWrite(w, r)
	if !ok {
		return
	}
	var body mcpTrustBody
	if !decodeMCPBody(w, r, &body) {
		return
	}
	class := strings.TrimSpace(body.Class)
	reason := strings.TrimSpace(body.Reason)
	if class == "" || reason == "" || !mcp.IsKnownTrust(class) {
		http.Error(w, "trust requires a known class and a non-empty reason", http.StatusBadRequest)
		return
	}
	res, err := s.governanceWrite.MCP.TrustApprove(r.Context(), actor, r.PathValue("name"), class, reason)
	...
}
```
This is the EXACT validation shape (`class == "" || reason == "" || !mcp.IsKnownTrust(class)`) the CLI path (or `TrustApprove` itself) must apply before ever reaching the dead fallback.

#### `internal/mcp/manager/audit.go` (REUSE VERBATIM — no modification needed) — the audited-write substrate, full relevant excerpt `internal/mcp/manager/audit.go:51-85`:
```go
type MCPAuditInsert struct {
	ActorIdentityID string
	Action          string
	ServerName      string
	Reason          string // "" -> NULL
}

func InsertMCPAuditTx(ctx context.Context, q *sqlc.Queries, in MCPAuditInsert) error {
	if _, err := q.InsertMcpAudit(ctx, in.toParams()); err != nil {
		return fmt.Errorf("insert mcp audit %q (tx): %w", in.ServerName, classifyMCPAuditErr(err))
	}
	return nil
}
```

#### `internal/mcp/manager/configwrite.go` (REUSE VERBATIM) — the atomic temp→tx→rename wrapper, `configwrite.go:42-58`:
```go
func WriteConfigWithAudit(ctx context.Context, pool *pgxpool.Pool, path string, next mcp.ManagedConfig, in MCPAuditInsert) error {
	tmp, err := writeConfigTemp(path, next)
	if err != nil {
		return err
	}
	if err := db.WithTx(ctx, pool, func(q *sqlc.Queries) error {
		return InsertMCPAuditTx(ctx, q, in)
	}); err != nil {
		_ = os.Remove(tmp) // no applied-but-unaudited: discard the staged config
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit MCP config %s: %w", path, err)
	}
	return nil
}
```
This is THE mechanism to route every CLI mutation through — no parallel mechanism should be built (CLAUDE.md "reusable code, no duplication").

#### Pool-threading into the CLI — the pattern to copy for main.go's `case "mcp":` dispatch. `cmd/aura/recovery.go:37-56` `identityRecover(ctx, store, pool, args)`:
```go
func identityRecover(ctx context.Context, store *identity.Store, pool *pgxpool.Pool, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, identityUsage)
		os.Exit(1)
	}
	name := args[0]
	id, err := store.GetIdentityByName(ctx, name)
	...
	token, err := mintBreakGlassToken(ctx, pool, id.ID)
	...
}
```
And `cmd/aura/recover_operator.go:47` `identityRecoverOperator(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, args []string)` — same shape, `pool` threaded as an explicit parameter into the subcommand function, opened/closed by the caller (`runIdentity`, defined at `cmd/aura/identity.go:33`, not fully read here but its dispatch signature is confirmed by the grep). Mirror this EXACTLY for `runMCP`/`runMCPCommand` (`cmd/aura/mcp.go:24-68`): today `runMCPCommand(ctx, args, out)` takes no pool at all — the mutating subcommands (`add`/`install`/`trust`/`enable`/`disable`/`remove`/`profile create|use|add|remove`) need a `*pgxpool.Pool` threaded in, while read-only subcommands (`recipes`/`status`/`list`/`logs`/`doctor`/`tools`/`console`) stay pool-free (RESEARCH Open Question 3's recommendation).

#### `cmd/aura/mcp_audit_actor.go` (NEW) — no existing analog; `os/user` has zero prior usage in this repo (verified by RESEARCH's repo-wide grep). Nearest STYLE analog for a graceful-degrade fallback chain is `recover_operator.go`'s multi-source secret resolution (`breakglass.Sourcer` — env → `--generate` → hidden prompt, `recover_operator.go:55-68`) — same "try A, fall back to B, fall back to C, never fail the caller" shape, different domain. Build the `cli:<os-username>` derivation as: `os/user.Current()` → on error, `os.Getenv("USER")`/`os.Getenv("USERNAME")` → on total failure, a literal fallback (e.g. `"cli:unknown"`) — per RESEARCH Pitfall #10, the audit actor must NEVER be empty since `mcp_audit.actor_identity_id` is `NOT NULL`.

---

### Cluster 6 — Legacy env prod-gating (D-14, D-15; MCPH-08)

#### `internal/config/config_validate.go` (MODIFIED — new `gateMCPLegacyEnv`)

**Analog:** self, `gateDestructiveShell`, the EXACT template to mirror — `internal/config/config_validate.go:210-219`:
```go
func (c *Config) gateDestructiveShell(p RuntimeProfile) []Violation {
	if p != ProfileServerProduction {
		return nil
	}
	raw := strings.TrimSpace(os.Getenv("AURA_SHELL_DESTRUCTIVE_PATTERNS"))
	if strings.EqualFold(raw, "off") {
		return []Violation{{Knob: "AURA_SHELL_DESTRUCTIVE_PATTERNS", Sev: Fatal,
			Msg: "destructive-shell gate must not be disabled (off) under server_production"}}
	}
	return nil
}
```
New gate, same shape (already sketched in RESEARCH Code Example #4 — reuse directly):
```go
func (c *Config) gateMCPLegacyEnv(p RuntimeProfile) []Violation {
	if p != ProfileServerProduction {
		return nil
	}
	if strings.TrimSpace(os.Getenv("AURA_MCP_SERVERS_JSON")) == "" {
		return nil
	}
	if !envutil.BoolDefault("AURA_MCP_LEGACY_ENV_COMPAT", false) {
		return []Violation{{Knob: "AURA_MCP_SERVERS_JSON", Sev: Fatal,
			Msg: "legacy MCP env config is disabled under server_production unless " +
				"AURA_MCP_LEGACY_ENV_COMPAT=1 is explicitly set"}}
	}
	return nil
}
```

**Registration point** — `internal/config/config_validate.go:85-99` `ValidateProfile` (append the new gate call to this list, same pattern as every sibling gate):
```go
func (c *Config) ValidateProfile(p RuntimeProfile) []Violation {
	var vs []Violation
	vs = append(vs, c.gateRequiredSecrets()...)
	vs = append(vs, c.gateRunDir()...)
	vs = append(vs, c.gateWebBind()...)
	vs = append(vs, reparsePass(p)...)
	vs = append(vs, c.gateObjectStoreCreds(p)...)
	vs = append(vs, c.gateGarageRPCSecret(p)...)
	vs = append(vs, c.gateReplication(p)...)
	vs = append(vs, c.gateCORS(p)...)
	vs = append(vs, c.gateDestructiveShell(p)...)
	vs = append(vs, c.gateWebAuth(p)...)
	vs = append(vs, c.gateMUSRIsolation(p)...)
	vs = append(vs, c.gateObjectStoreEndpoint(p)...)
	// vs = append(vs, c.gateMCPLegacyEnv(p)...)   <-- new call slots in here
	return vs
}
```
Boot already enforces this for free via `cfg.Validate()` (`config_validate.go:61-76`), called from `cmd/aura/chat_boot.go:176` and `:227` (confirmed by direct grep) before any DB work — no new boot-wiring needed beyond registering the gate.

#### `internal/config/config_knobs.go` (MODIFIED — new `KnobSpec` row)

**Analog:** self, existing `KindBool` rows, `config_knobs.go:77-79`:
```go
{Name: "AURA_AGUI_CORS_PERMISSIVE", Kind: KindBool, Default: "false"},
{Name: "AURA_SHELL_DESTRUCTIVE_PATTERNS", Kind: KindString, Default: ""},
{Name: "AURA_WEB_TRUST_PROXY", Kind: KindBool, Default: "false"},
```
New row follows the identical shape: `{Name: "AURA_MCP_LEGACY_ENV_COMPAT", Kind: KindBool, Default: "false"}`. Per RESEARCH Assumption A3, this ONE knob is Tier A-appropriate (registered here); the other three phase knobs (`AURA_MCP_MOUNT_TIMEOUT`/`AURA_MCP_STDIO_MAX_FRAME`/`AURA_MCP_PROBE_TIMEOUT`) are read inside `internal/mcp`/`internal/mcp/manager`, outside `internal/config` — Tier C, deliberately excluded from this registry per its own documented scope boundary.

#### `internal/envutil/envutil.go` (REUSE VERBATIM — no modification) — the int/bool env-knob reader every new timeout/cap should use, full relevant excerpt:
```go
func IntDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func BoolDefault(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
```
Use `envutil.IntDefault("AURA_MCP_MOUNT_TIMEOUT_SEC", 10)` / `envutil.IntDefault("AURA_MCP_STDIO_MAX_FRAME", 1<<20)` / `envutil.IntDefault("AURA_MCP_PROBE_TIMEOUT_SEC", 5)` style rather than hand-rolling `strconv.Atoi(os.Getenv(...))` inline (already extracted in an earlier phase specifically to stop this copy-paste).

---

### Cluster 7 — Live HTTP probe (D-16, D-17, D-18; MCPH-09)

**CRITICAL — RESEARCH correction verified live:** `cmd/aura/doctor.go`'s `doctorProbeMCPBinary` (`doctor.go:146-156`) checks `cfg.Neo4j.MCPBinary` via `LookPath` — the Neo4j-Cypher sidecar binary, UNRELATED to the managed MCP server registry:
```go
func defaultDoctorProbeMCPBinary(_ context.Context, cfg *config.Config) (string, error) {
	bin := strings.TrimSpace(cfg.Neo4j.MCPBinary)
	if bin == "" {
		return "", fmt.Errorf("AURA_MCP_NEO4J_CYPHER_BIN is empty")
	}
	path, err := doctorLookPath(bin)
	...
}
```
Do NOT touch this function for MCPH-09 — it is out of scope (confirmed, do not repurpose).

**The REAL F-046 bug** — `cmd/aura/mcp_status.go` `writeRuntimeCheck`, `mcp_status.go:90-109`:
```go
func writeRuntimeCheck(ctx context.Context, out io.Writer, name string, server mcp.ManagedServer) error {
	if server.Type == mcp.ServerTypeStreamableHTTP || server.URL != "" {
		return writef(out, "%s: http endpoint configured\n", name)   // <-- F-046: skips ProbeServer entirely for HTTP
	}
	if strings.TrimSpace(server.Command) == "" {
		return writef(out, "%s: runtime missing command\n", name)
	}
	res := mcp.ProbeServer(ctx, name, server)   // <-- stdio branch ALREADY calls ProbeServer correctly
	if !res.OK {
		return writef(out, "%s: runtime missing %s\n", name, server.Command)
	}
	return writef(out, "%s: runtime ok (%s)\n", name, server.Command)
}
```
Fix: call `mcp.ProbeServer(ctx, name, server)` for the HTTP branch too (delete the early-return special case), bounded by `context.WithTimeout(ctx, AURA_MCP_PROBE_TIMEOUT)` — today NEITHER branch imposes any deadline on the `ctx` it receives (`mcpDoctorAll` → `runMCPCommand(context.Background(), ...)`, confirmed no deadline anywhere in this CLI path).

`cmd/aura/mcp_status.go` `mcpStatus` (`mcp_status.go:14-48`) uses `mcpmanager.SnapshotStatus(doc)` — a config-derived, non-live view (`manager/status.go:40-68`, `StartupState` computed from `Enabled`/`NormalizedTrust` only, never dials anything). D-17 requires this surface to ALSO reflect the live probe — needs additive wiring, not a replacement of `SnapshotStatus` (which still serves the config-derived columns: trust/runtime/profiles).

#### `internal/mcp/probe.go` (REUSE VERBATIM — already correct + already tested, ZERO modification needed) — full relevant excerpt, `probe.go:47-76`:
```go
func ProbeServer(ctx context.Context, name string, server ManagedServer) ProbeResult {
	res := ProbeResult{Name: name}
	if normalizedServerType(server) != ServerTypeStreamableHTTP && strings.TrimSpace(server.Command) == "" {
		res.Detail = "runtime missing command"
		res.Err = res.Detail
		return res
	}
	client, err := OpenServer(ctx, name, server)
	if err != nil {
		res.Detail = "dial failed"
		res.Err = RedactSecrets(err.Error())
		return res
	}
	defer func() { _ = client.Close() }()
	tools, err := client.ListTools(ctx)
	if err != nil {
		res.Detail = "tools/list failed"
		res.Err = RedactSecrets(err.Error())
		return res
	}
	res.OK = true
	res.ToolCount = len(tools)
	res.Detail = RedactSecrets(fmt.Sprintf("ok (%d tools)", len(tools)))
	return res
}
```
This ALREADY dials both stdio and HTTP correctly (via `OpenServer`, which dispatches on `normalizedServerType` → migrates to `Classify` per Cluster 1). D-17's "one probe, three surfaces" is a wiring task, not a new-implementation task.

#### The already-correct third surface (governance health board) — `internal/agui/governance_api.go` `handleMCPProbe`, `governance_api.go:228-249` (the pattern the OTHER two surfaces should converge toward):
```go
func (s *Server) handleMCPProbe(w http.ResponseWriter, r *http.Request) {
	if s.governance.MCP == nil {
		http.Error(w, "mcp board not configured", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	doc := s.governance.MCP.Servers()
	server, ok := doc.MCPServers[name]
	if !ok {
		http.Error(w, "mcp server not found", http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.probeDeadline())
	defer cancel()
	res := s.governance.MCP.Probe(ctx, name, server)
	res.Detail = SanitizeString(res.Detail)
	res.Err = SanitizeString(res.Err)
	writeJSON(w, res)
}

func (s *Server) probeDeadline() time.Duration {
	if s.probeTimeout > 0 {
		return s.probeTimeout
	}
	return defaultProbeTimeout   // 3 * time.Second, governance_api.go:39
}
```
This `context.WithTimeout(r.Context(), deadline)` + per-row isolation shape (a hung/dead server fails ONLY its own row/result, never blocks siblings) is exactly what `writeRuntimeCheck` and the new `doctor.go` 6th check need to replicate, just with `AURA_MCP_PROBE_TIMEOUT` in place of the hardcoded 3s (or reconciled to the same knob — Claude's Discretion).

#### `cmd/aura/doctor.go` (MODIFIED — new 6th check) — the exact registry + probe-function shape to extend, `doctor.go:89-97` + `doctor.go:113-127`:
```go
func doctorChecks() []doctorCheck {
	return []doctorCheck{
		{name: "postgres", probe: doctorProbePostgres, failureCode: exitUnreachable},
		{name: "neo4j", probe: doctorProbeNeo4j, failureCode: exitUnreachable},
		{name: "embed", probe: doctorProbeEmbed, failureCode: exitUnreachable},
		{name: "mcp-neo4j-cypher", probe: doctorProbeMCPBinary, failureCode: exitInfra},
		{name: "llm_key", probe: doctorProbeLLMKey, failureCode: 0},
		// {name: "mcp", probe: doctorProbeMCPServers, failureCode: exitUnreachable},  <-- NEW 6th check
	}
}

func defaultDoctorProbeNeo4j(ctx context.Context, cfg *config.Config) (string, error) {
	mcp, err := doctorOpenNeo4j(ctx, cfg)
	if err != nil {
		return "", err
	}
	defer func() { _ = mcp.Close() }()
	rows, err := mcp.Read(ctx, "RETURN 1 AS ok", nil)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("RETURN 1 returned no rows")
	}
	return "RETURN 1 round-trip OK", nil
}
```
The new check's `doctorProbe` function (signature `func(context.Context, *config.Config) (string, error)`, `doctor.go:18`) should load the managed config, iterate `RunnableManagedServers`, filter to HTTP-only (D-18: skip disabled/blocked, and per D-18 "only... runnable HTTP servers" — skip stdio too, since stdio already gets a real spawn via other paths and D-18 is scoped to the dead-endpoint HTTP problem), call `mcp.ProbeServer` per server bounded by `AURA_MCP_PROBE_TIMEOUT`, and aggregate to a single pass/fail + detail string in the same style as `defaultDoctorProbeNeo4j` above (one string detail, one error).

---

## Shared Patterns

### No-op-under-dev / harden-under-prod gate template
**Source:** `internal/config/config_validate.go:210-219` (`gateDestructiveShell`) + `internal/mcp/transport.go:38-45` (`ssrfEnforceFromEnv`)
**Apply to:** D-14/D-15 (`gateMCPLegacyEnv`), and any other profile-gated knob this phase introduces.
```go
func ssrfEnforceFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AURA_MCP_SSRF_ENFORCE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
```
Both templates share the shape: default OFF/lenient, an explicit opt-in flips behavior, dev/local_trusted stays byte-identical.

### Append-only audit inside `db.WithTx`
**Source:** `internal/mcp/manager/configwrite.go:42-58` (`WriteConfigWithAudit`)
**Apply to:** Every CLI mutation in `cmd/aura/mcp.go`/`mcp_profile.go` (D-12/D-13).
```go
if err := db.WithTx(ctx, pool, func(q *sqlc.Queries) error {
	return InsertMCPAuditTx(ctx, q, in)
}); err != nil {
	_ = os.Remove(tmp) // no applied-but-unaudited: discard the staged config
	return err
}
if err := os.Rename(tmp, path); err != nil { ... }
```
The mutation and its audit row commit atomically, or neither commits.

### Env-knob reading (silent-fallback leaf)
**Source:** `internal/envutil/envutil.go:22-47` (`IntDefault`/`BoolDefault`)
**Apply to:** Every new `AURA_MCP_*` timeout/cap knob (D-06/D-08/D-11/D-16).
```go
func IntDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" { return fallback }
	n, err := strconv.Atoi(v)
	if err != nil { return fallback }
	return n
}
```

### `TestHelperProcess` re-exec idiom (subprocess test fake)
**Source:** `internal/mcp/client_open_test.go:18-88` (already reused in 7+ test files across the repo per RESEARCH)
**Apply to:** New process-tree-kill test (a "grandchild" mode) and a new oversized-frame test (an "oversize" mode).
```go
func TestHelperProcess(t *testing.T) {
	if os.Getenv("AURA_MCP_HELPER") != "1" { return }
	mode := os.Getenv("AURA_MCP_HELPER_MODE")
	if mode == "crash" { ... }
	// existing modes: "", "crash", "hang" — extend with "oversize"/"grandchild"
	...
}
func helperServerConfig(mode string) ServerConfig {
	args := []string{"-test.run=TestHelperProcess"}
	env := []string{"AURA_MCP_HELPER=1"}
	if mode != "" { env = append(env, "AURA_MCP_HELPER_MODE="+mode) }
	return ServerConfig{Command: os.Args[0], Args: args, Env: env}
}
```

### `io.Pipe` fake-server harness for pure unit tests (no subprocess)
**Source:** `internal/mcp/client_test.go:19-96` (`fakeServer`, `newClientForTest`, `newTestPair`)
**Apply to:** The oversized-frame abort unit test (MCPH-05) — no process spawn needed, just feed bytes through the pipe.
```go
func newClientForTest(name string, stdin io.WriteCloser, stdout io.Reader) *Client {
	return &Client{name: name, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: boundedbuffer.New(0)}
}
func newTestPair(t *testing.T) (*Client, func()) {
	t.Helper()
	cliStdinR, cliStdinW := io.Pipe()
	srvStdoutR, srvStdoutW := io.Pipe()
	fs := &fakeServer{in: cliStdinR, out: srvStdoutW}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); fs.run() }()
	c := newClientForTest("fake", cliStdinW, srvStdoutR)
	cleanup := func() { _ = cliStdinW.Close(); _ = srvStdoutW.Close(); _ = srvStdoutR.Close(); wg.Wait() }
	return c, cleanup
}
```
Note: `newClientForTest` constructs a `*Client` directly with `stdout: bufio.NewReader(stdout)` — if `readResponseBlocking` moves to `*bufio.Scanner`, this test constructor needs updating too (it is a direct struct literal, not going through `Open`).

### goleak `TestMain` wiring
**Source:** `internal/mcp/main_test.go:13-15` (already wired for the whole `internal/mcp` package)
```go
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```
**Apply to:** Any new test in `internal/mcp`/`internal/mcp/manager` automatically runs under this. New goroutine-spawning tests in `cmd/aura` (the concurrent-shutdown test, MCPH-06) or `internal/agent/mcptools` should check for an equivalent `TestMain` before assuming leak-detection is active there — verify, do not assume.

### Gate-function table-test style
**Source:** `internal/config/config_validate_test.go:157-183` (`TestGateDestructiveShell`)
**Apply to:** New `TestGateMCPLegacyEnv`.
```go
func TestGateDestructiveShell(t *testing.T) {
	tests := []struct {
		name      string
		profile   RuntimeProfile
		env       string
		wantFatal bool
	}{
		{name: "prod off lowercase", profile: ProfileServerProduction, env: "off", wantFatal: true},
		{name: "prod empty keeps gate", profile: ProfileServerProduction, env: "", wantFatal: false},
		{name: "hardened allows off", profile: ProfileSingleUserHardened, env: "off", wantFatal: false},
		{name: "dev allows off", profile: ProfileDev, env: "off", wantFatal: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", tc.env)
			vs := (&Config{}).gateDestructiveShell(tc.profile)
			got := hasViolation(vs, "AURA_SHELL_DESTRUCTIVE_PATTERNS", Fatal)
			if got != tc.wantFatal {
				t.Errorf("gateDestructiveShell(%s) env=%q: Fatal=%v, want %v (%+v)", tc.profile, tc.env, got, tc.wantFatal, vs)
			}
		})
	}
}
```
The `hasViolation(vs []Violation, knob string, sev Severity) bool` helper (`config_validate_test.go:50`) and `allProfiles` fixture (`config_validate_test.go:61`) are already shared infra — reuse both, do not redefine.

---

## No Analog Found

Files/behaviors with no close pre-existing match in the codebase (planner should treat these as genuinely new, following RESEARCH.md's sketch rather than an in-repo copy):

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `cmd/aura/mcp_audit_actor.go` | utility | transform | `os/user` package has ZERO existing usage anywhere in this repo (confirmed by RESEARCH's repo-wide grep); the `cli:<os-username>` derivation + graceful-degrade-to-`"cli:unknown"` fallback chain is genuinely new code, not a reuse. Nearest STYLE (not substance) analog: `cmd/aura/recover_operator.go`'s multi-source secret-resolution fallback chain. |
| Two-context mount split (`mcp.Open`'s signature / `internal/agent/mcptools` timeout wrapper) | service | event-driven | No existing call site in this codebase correctly separates "ctx that owns the subprocess's whole lifetime" from "ctx that only bounds the handshake" — `bridge_reconnect.go`'s `openReplacement` is the closest STRUCTURAL shape but is flagged above as potentially sharing the same risk class (Pitfall #2), not a verified-safe reference. This is genuinely new API surface per RESEARCH ("requires a signature change... not a one-line fix"). |

## Metadata

**Analog search scope:** `internal/mcp/`, `internal/mcp/manager/`, `internal/agent/mcptools/`, `internal/agent/tools/` (process-kill only), `internal/config/`, `internal/agui/` (governance write/read only), `internal/boundedbuffer/`, `internal/envutil/`, `cmd/aura/` (mcp*, doctor, main, recovery, recover_operator, serve_governance_write, identity dispatch only).
**Files scanned (Read in full or targeted):** 29 source files + 7 test files = 36 files read directly; line counts verified via `wc -l` before reading to confirm all fit single-pass (largest: `cmd/aura/mcp.go` at 503 LOC, `internal/mcp/client.go` at 477 LOC — both well under the 2,000-line re-read threshold, read once each).
**Pattern extraction date:** 2026-07-18
**Corrections applied per orchestrator instruction:** RESEARCH.md's three corrections to CONTEXT.md's citations were verified by direct code reading during this pass and are reflected above: (1) F-013's live mechanism confirmed in BOTH `managed_config.go` and `manager/runtime.go` (not just one), (2) the real mount/shutdown loop is `cmd/aura/main.go` (`buildRegistryWithMCP`/`closeMCPServers`), not `manager/runtime.go:34`, (3) the real F-046 bug is `cmd/aura/mcp_status.go`'s `writeRuntimeCheck`, not `cmd/aura/doctor.go`'s `doctorProbeMCPBinary` (confirmed unrelated — Neo4j-Cypher sidecar binary check).
