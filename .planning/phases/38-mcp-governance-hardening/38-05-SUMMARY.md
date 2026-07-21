---
phase: 38-mcp-governance-hardening
plan: 05
subsystem: infra
tags: [go, context, concurrency, errgroup, mcp, boot, shutdown, dos-mitigation]

# Dependency graph
requires:
  - phase: 38-mcp-governance-hardening (38-01)
    provides: "mcp.Classify canonical transport+trust classifier"
  - phase: 38-mcp-governance-hardening (38-02)
    provides: "Bounded stdio frame cap + shared internal/procgroup process-tree kill (mcp.Open's setProcessGroup wiring), which OpenWithHandshakeContext builds on"
provides:
  - "internal/mcp.OpenWithHandshakeContext: two-context Open variant (process-lifetime ctx for exec.CommandContext, separate bounded handshake ctx for initialize)"
  - "internal/agent/mcptools.MountServer/MountManagedServer two-context signature (processCtx, handshakeCtx) threading the split through the whole mount chain"
  - "internal/agent/mcptools.MountWithDefs/bridgeFromDefs: mount-time tool discovery via the RAW transport, bypassing reconnectingServer's independent reconnect budget"
  - "reconnectingServer.setProcessContext/processContext: the reconnect path's own process-lifetime ctx, decoupled from the reconnect-handshake ctx (Pitfall #2 fix)"
  - "cmd/aura buildRegistryWithMCP: per-server bounded handshake ctx (AURA_MCP_MOUNT_TIMEOUT, default 10s) distinct from the daemon ctx"
  - "cmd/aura closeMCPServers: errgroup-based concurrent fan-out under one aggregate AURA_MCP_SHUTDOWN_TIMEOUT deadline (default 5s), replacing the sequential reverse-order loop"
affects: ["38-06 (live probe / governance board visibility for a dropped/unreachable server)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Two-context Open/mount API: a long-lived process-lifetime ctx threaded separately from a short, per-attempt bounded handshake ctx, so a handshake timeout can never double as (and accidentally cancel) the subprocess's own lifetime context"
    - "Mount-time tool discovery via the raw transport, not the reconnecting wrapper: reconnectingServer.ListTools/CallTool intentionally reconnect-on-transport-error using their OWN independent budget, which is correct for post-mount runtime resilience but wrong for a bounded initial mount (it would silently absorb the caller's deadline into its own, much longer one)"
    - "errgroup fan-out + one aggregate context.WithTimeout for bounding a batch of independent, already-individually-bounded operations without imposing sequential wall-clock cost"

key-files:
  created:
    - internal/agent/mcptools/bridge_reconnect_realsubprocess_test.go
  modified:
    - internal/mcp/client.go
    - internal/agent/mcptools/mount.go
    - internal/agent/mcptools/mount_retry.go
    - internal/agent/mcptools/bridge.go
    - internal/agent/mcptools/bridge_reconnect.go
    - internal/agent/mcptools/bridge_reconnect_branches_test.go
    - internal/agent/mcptools/bridge_test.go
    - internal/agent/mcptools/bridge_trust_test.go
    - internal/agent/mcptools/managed_mount_test.go
    - internal/agent/mcptools/memory_integration_test.go
    - internal/agent/mcptools/mount_retry_test.go
    - internal/agent/mcptools/mount_test.go
    - internal/agent/memory_recall_integration_test.go
    - internal/eval/harness_swarm_e2e_test.go
    - cmd/aura/main.go
    - cmd/aura/main_test.go
    - cmd/aura/registry_test.go
    - cmd/aura/chat_boot_test.go

key-decisions:
  - "Mount-time discovery (the first tools/list right after Open) is listed via the RAW mcp.Client, not the reconnectingServer wrapper — discovered mid-plan via the RED test itself (an 11s instead of ~1s mount for a hung server): reconnectingServer.ListTools treats a bounded-ctx timeout as an ordinary transport error and transparently reconnects using its OWN ~10s reconnectTimeout budget (context.WithoutCancel-severed from the caller's ctx), silently absorbing far more than AURA_MCP_MOUNT_TIMEOUT. bridgeFromDefs/MountWithDefs let the bridging/registration machinery accept pre-listed defs so the reconnecting wrapper is only ever exercised for POST-mount calls."
  - "reconnectingServer gained a processCtx field (set via setProcessContext right after construction) instead of MountServer/MountManagedServer wiring a fresh ctx per reconnect — this makes the reconnect path reuse the EXACT SAME long-lived daemon ctx the initial Open used for exec.CommandContext, so a reconnected replacement subprocess's lifetime is never accidentally tied to the short, deferred-cancel reconnect-handshake ctx."
  - "isStreamableHTTPManagedServer (call-site #8) now delegates to mcp.Classify but resolves a Classify ERROR to `true` (not `false`): this routes an ambiguous/inconsistent config to the OpenServer branch, which re-classifies and surfaces that SAME error — guaranteeing a rejected config can never silently fall through to a stdio subprocess spawn (preserves/strengthens the F-027 protection Classify was built to close, rather than reintroducing it at this call site)."
  - "closeMCPServers keeps its existing `[]func() error` signature (no added ctx parameter): it builds its own bounded context.Background()-rooted deadline internally, matching the sibling pattern already established in the same file (chatEnv.close's BackgroundShells.Shutdown) — shutdown must proceed on its own budget regardless of any caller-supplied ctx's state."
  - "AURA_MCP_MOUNT_TIMEOUT/AURA_MCP_SHUTDOWN_TIMEOUT are Tier C knobs (envutil.IntDefault, not registered in config_knobs.go), per the phase RESEARCH's established convention for this cluster of env vars."

patterns-established:
  - "Any future two-context API in this codebase should mirror mcp.OpenWithHandshakeContext's naming/ordering convention: (processCtx, handshakeCtx, ...args) with processCtx always first, long-lived, never deferred-canceled by the immediate caller."

requirements-completed: [MCPH-04, MCPH-06]

coverage:
  - id: D1
    description: "A hung MCP server's mount handshake is dropped within ~AURA_MCP_MOUNT_TIMEOUT (bounded per-server handshake ctx, distinct from the daemon-lifetime process ctx); buildRegistryWithMCP returns within a bounded window regardless of how many servers are unreachable, and a server mounted moments earlier survives past the deadline unaffected (Pitfall #2 regression guard)."
    requirement: MCPH-04
    verification:
      - kind: unit
        ref: "cmd/aura/main_test.go#TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives"
        status: pass
    human_judgment: false
  - id: D2
    description: "closeMCPServers fans out ALL closers concurrently under ONE aggregate AURA_MCP_SHUTDOWN_TIMEOUT deadline (errgroup); total shutdown wall-clock is bounded regardless of server count, not sequential N×5s. Zero closers returns immediately; an abandoned straggler settles on its own already-bounded per-transport 5s without leaking a goroutine past teardown."
    requirement: MCPH-06
    verification:
      - kind: unit
        ref: "cmd/aura/main_test.go#TestCloseMCPServers_ConcurrentBoundedShutdown"
        status: pass
      - kind: unit
        ref: "cmd/aura/main_test.go#TestCloseMCPServers_ZeroClosersReturnsImmediately"
        status: pass
      - kind: unit
        ref: "cmd/aura/main_test.go#TestCloseMCPServers_AggregateDeadlineAbandonsStragglers"
        status: pass
    human_judgment: false
  - id: D3
    description: "bridge_reconnect.go's openReplacement is empirically proven NOT to share the Pitfall #2 class: a reconnected replacement subprocess (opened through the REAL, unstubbed openMCPClient) survives past its own bounded, deferred-cancel reconnect-handshake ctx firing, because the subprocess's process-lifetime ctx is the reconnectingServer's separately-tracked processCtx, not the short handshake ctx."
    requirement: MCPH-06
    verification:
      - kind: unit
        ref: "internal/agent/mcptools/bridge_reconnect_realsubprocess_test.go#TestOpenReplacement_ReconnectedSubprocessSurvivesHandshakeCtxCancel"
        status: pass
    human_judgment: false
  - id: D4
    description: "mount.go's isStreamableHTTPManagedServer (call-site #8) is migrated onto mcp.Classify."
    requirement: MCPH-01
    verification:
      - kind: unit
        ref: "internal/agent/mcptools/managed_mount_test.go (existing TestMountManagedServer_* suite, unchanged assertions, now exercising the Classify-backed dispatch)"
        status: pass
    human_judgment: false

duration: ~2h40min
completed: 2026-07-18
status: complete
---

# Phase 38 Plan 05: Bounded MCP Mount + Concurrent Shutdown Summary

**Two-context mount (process ctx vs. bounded handshake ctx) through `mcp.OpenWithHandshakeContext` → `mountStdio` → `MountWithRetry`, plus `errgroup`-based concurrent aggregate-deadline shutdown in `closeMCPServers`, closing MCPH-04 and MCPH-06's shutdown half — and, discovered mid-execution, a second Pitfall #2 instance in `reconnectingServer`'s mount-time discovery path.**

## Performance

- **Duration:** ~2h40min (including the mid-execution discovery, isolation, and fix of a bug the plan's own RED test surfaced)
- **Completed:** 2026-07-18T13:47:00Z
- **Tasks:** 2 (Task 1 tdd="true": RED then GREEN; Task 2: single commit alongside Task 1's GREEN, since Task 2 has no tdd gate)
- **Files modified:** 18 (1 created, 17 modified)

## Accomplishments
- `internal/mcp/client.go`'s `Open` now delegates to a new `OpenWithHandshakeContext(processCtx, handshakeCtx, name, cfg)`: `exec.CommandContext` is bound to `processCtx` (the daemon-lifetime context), `initializeContext` is bound to `handshakeCtx` (the per-server mount deadline) — a handshake timeout reaps the just-spawned subprocess without touching `processCtx` or any other server sharing it.
- `internal/agent/mcptools/mount.go`'s `MountServer`/`MountManagedServer` thread both contexts through a shared `mountStdio` helper, which lists tools via the **raw** `*mcp.Client` (`cli.ListTools(handshakeCtx)`) **before** wrapping it in `reconnectingServer` — closing a second Pitfall #2-class bug the RED test itself exposed (see Decisions Made).
- `cmd/aura/main.go`'s `buildRegistryWithMCP` derives a per-server `handshakeCtx := context.WithTimeout(ctx, mountTimeout)` (mountTimeout from the new `AURA_MCP_MOUNT_TIMEOUT`, default 10s) and passes it as the bound that caps the WHOLE `MountWithRetry` budget (every attempt + backoff), while `ctx` itself (the daemon ctx) stays the process-lifetime context for every mounted server.
- `closeMCPServers` is rewritten from a sequential reverse-order loop to an `errgroup.Group` fan-out under one `context.WithTimeout(context.Background(), shutdownTimeout)` (from the new `AURA_MCP_SHUTDOWN_TIMEOUT`, default 5s): total shutdown wall-clock is bounded regardless of server count; an abandoned straggler at the deadline settles on its own already-bounded per-transport 5s without a lasting goroutine leak.
- `mount.go`'s `isStreamableHTTPManagedServer` (call-site #8) is migrated onto the canonical `mcp.Classify`, closing the last duplicated transport-dispatch decision body (MCPH-01) while **strengthening** (not weakening) the F-027 mixed-config protection: a `Classify` error routes to the `OpenServer` branch, which re-classifies and surfaces that same error, so an ambiguous/rejected config can never silently reach a stdio spawn.
- `bridge_reconnect.go`'s `openReplacement` is empirically proven to share the Pitfall #2 class (a real subprocess test showed "the pipe is being closed" once the reconnect-handshake ctx's deferred cancel fired) and is fixed with the same two-context shape: `reconnectingServer` now tracks its own `processCtx` (set by `MountServer`/`MountManagedServer` right after construction), and `openReplacement` passes that — not the bounded reconnect-handshake ctx — as the replacement subprocess's process-lifetime context.

## Task Commits

Task 1 is TDD (RED then GREEN); Task 2 has no tdd gate and its production code (the `closeMCPServers` rewrite) landed alongside Task 1's RED commit (its own tests already pass there), with Task 2's `bridge_reconnect.go` fix + empirical test following the same RED→GREEN arc as Task 1 and landing in the GREEN commit:

1. **Task 1: Two-context bounded mount (process ctx vs handshake ctx)**
   - RED - `83dbec81` (test): `TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives` + `TestOpenReplacement_ReconnectedSubprocessSurvivesHandshakeCtxCancel` both fail against the pre-fix production code; `TestCloseMCPServers_*` (Task 2) already pass since that rewrite is orthogonal and non-TDD.
   - GREEN - `baf3094a` (feat): `mount.go`'s `mountStdio` routes mount-time discovery through the raw client + `MountWithDefs`; `bridge_reconnect.go` gains `processCtx`/`setProcessContext`/`processContext` and `openReplacement` uses it; `chat_boot_test.go`'s pre-existing `TestBootReleaseResourcesDrainsClosersAndClosesPool` is fixed for a data race the now-concurrent `closeMCPServers` exposed (Rule 1).
2. **Task 2: Concurrent bounded shutdown + de-risk bridge_reconnect.go openReplacement**
   - Production (`closeMCPServers` errgroup rewrite, `AURA_MCP_MOUNT_TIMEOUT`/`AURA_MCP_SHUTDOWN_TIMEOUT` resolvers) and its tests: `83dbec81` (alongside Task 1's RED commit — see Deviations).
   - `openReplacement`'s de-risk fix + its empirical regression test: `83dbec81` (RED) / `baf3094a` (GREEN), interleaved with Task 1's commits since both bugs live in the same files.

_No refactor commits were needed — both GREEN implementations landed clean, verified against a genuine RED failure first._

## Files Created/Modified
- `internal/mcp/client.go` - `Open` delegates to new `OpenWithHandshakeContext(processCtx, handshakeCtx, ...)`; `exec.CommandContext` bound to `processCtx`, `initializeContext` bound to `handshakeCtx`
- `internal/agent/mcptools/mount.go` - `MountServer`/`MountManagedServer` two-context signatures; new shared `mountStdio` lists via the raw client + `MountWithDefs`; `isStreamableHTTPManagedServer` migrated onto `mcp.Classify`
- `internal/agent/mcptools/mount_retry.go` - doc-only: clarifies that the caller's handshake ctx caps the WHOLE `MountWithRetry` budget (every attempt + backoff), not one attempt
- `internal/agent/mcptools/bridge.go` - new `bridgeFromDefs`/`MountWithDefs`/`finishMount` decomposition: `Bridge`/`Mount` unchanged externally, but now share their registration/refresh-hook body with the new pre-listed-defs path
- `internal/agent/mcptools/bridge_reconnect.go` - `reconnectingServer` gains `processCtx`/`setProcessContext`/`processContext`; `openReplacement` uses `processContext()` (not the bounded handshake ctx) as the replacement's process-lifetime ctx; `openMCPClient` is now two-context
- `internal/agent/mcptools/bridge_reconnect_realsubprocess_test.go` - **new**: a real (unstubbed) subprocess empirical test for the openReplacement Pitfall #2 fix
- `internal/agent/mcptools/{bridge_reconnect_branches_test.go,bridge_test.go,bridge_trust_test.go,managed_mount_test.go,memory_integration_test.go,mount_retry_test.go,mount_test.go}` - mechanical signature updates for the two-context `openMCPClient`/`MountServer`/`MountManagedServer` (compile-only, no behavior change)
- `internal/agent/memory_recall_integration_test.go`, `internal/eval/harness_swarm_e2e_test.go` - same mechanical signature update, under the `memory_integration`/`cot_eval` build tags respectively
- `cmd/aura/main.go` - `buildRegistryWithMCP`'s per-server bounded `handshakeCtx`; new `mcpMountTimeout`/`mcpShutdownTimeout` resolvers; `closeMCPServers` errgroup rewrite
- `cmd/aura/main_test.go` - `TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives` + three `TestCloseMCPServers_*` tests
- `cmd/aura/registry_test.go` - `runRegistryTestMCPServer` gains a `AURA_MCP_HELPER_MODE=hang` branch on `tools/list` (reused by the new mount test instead of duplicating a second self-exec helper)
- `cmd/aura/chat_boot_test.go` - `orderRecorder` (mutex-guarded) replaces a bare `*[]string` shared across now-concurrently-invoked closer functions

## Decisions Made
- **Mount-time discovery bypasses `reconnectingServer` entirely.** Found via the RED test itself: with the two-context split alone in place, a hung server's mount took ~11s instead of the intended ~1s, because `Mount(handshakeCtx, reg, name, srv)` (where `srv` is the reconnecting wrapper) calls `srv.ListTools(handshakeCtx)`, and `reconnectingServer.ListTools` treats ANY transport error — including the caller's own `handshakeCtx` deadline expiring — as a cue to transparently reconnect using its own independent `reconnectTimeout` (10s default, `context.WithoutCancel`-severed from the caller's ctx). That silently absorbed ~10x the intended mount budget. Fix: `mountStdio` calls `cli.ListTools(handshakeCtx)` on the **raw** `*mcp.Client` first (bounded purely by `handshakeCtx`, no reconnect layer), THEN wraps the client in `reconnectingServer` and mounts the pre-fetched defs via new `MountWithDefs`. Bridged tools still reference the reconnecting wrapper for every CALL after a successful mount, so runtime resilience is unaffected — only the INITIAL discovery round-trip is re-routed.
- **`reconnectingServer` tracks its own `processCtx`.** `openReplacement`'s empirical test (`TestOpenReplacement_ReconnectedSubprocessSurvivesHandshakeCtxCancel`, driving a REAL subprocess through the unstubbed `openMCPClient`) proved the pre-existing code shared the Pitfall #2 class: `openReplacement` passed the same bounded, `defer`-cancelled `reconnectCtx` as both the handshake bound and (via `openMCPClient` → `mcp.Open`) the subprocess's `exec.CommandContext` — so `defer cancel()` firing at `openReplacement`'s return killed the just-reconnected subprocess ("the pipe is being closed") almost immediately. Verified by temporarily reintroducing the exact pre-fix wiring (`openMCPClient(handshakeCtx, handshakeCtx, ...)`) and confirming the test fails, then restoring the fix and confirming it passes — this was NOT presented as "proven good" without the check, per the plan's explicit requirement.
- **`isStreamableHTTPManagedServer`'s `Classify`-error branch resolves to `true`, not `false`.** A naive migration (error → not-HTTP → fall through to stdio) would have silently bypassed `Classify`'s own F-027 protection at exactly this call site. Resolving an error to `true` instead routes to the `OpenServer` branch, which independently re-classifies and surfaces the SAME rejection — the ambiguous/inconsistent config can never reach a stdio subprocess spawn either way.
- **`closeMCPServers` keeps its existing signature** (`[]func() error`, no `ctx` parameter) and builds its own bounded `context.Background()`-rooted deadline internally, mirroring the same file's pre-existing `chatEnv.close`/`BackgroundShells.Shutdown` pattern — shutdown proceeds on its own budget regardless of any external ctx's state.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Mount-time discovery through `reconnectingServer` silently defeated the mount timeout by ~10x**
- **Found during:** Task 1, verifying the RED test actually fails for the right reason (and then re-verifying GREEN)
- **Issue:** The plan's Task 1 action described only the two-context `Open`/`MountWithRetry` threading; it did not anticipate that `Mount()`'s tools/list call — routed through the already-constructed `reconnectingServer` — would itself trigger a full reconnect attempt on the caller's own handshake-ctx timeout, using an independent ~10s budget unrelated to `AURA_MCP_MOUNT_TIMEOUT`.
- **Fix:** Added `bridgeFromDefs`/`MountWithDefs` to `bridge.go` (a decomposition of the existing `Bridge`/`Mount`, no external signature change to either) and rewrote `mountStdio` to list tools via the raw client before constructing the reconnecting wrapper.
- **Files modified:** internal/agent/mcptools/bridge.go, internal/agent/mcptools/mount.go
- **Verification:** `TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives` fails (~11s) without the fix, passes (~3-4s) with it; confirmed by temporarily reverting and re-applying.
- **Committed in:** 83dbec81 (RED, test only) / baf3094a (GREEN)

**2. [Rule 1 - Bug] `bridge_reconnect.go:openReplacement` shared the Pitfall #2 class**
- **Found during:** Task 2's mandated empirical check
- **Issue:** `openReplacement` passed the same bounded, deferred-cancel ctx as both the reconnect-handshake bound and the replacement subprocess's process-lifetime ctx (via `openMCPClient` → `mcp.Open`'s single-ctx `exec.CommandContext`). The deferred `cancel()` at `openReplacement`'s return killed the just-reconnected subprocess.
- **Fix:** Added `processCtx`/`setProcessContext`/`processContext` to `reconnectingServer`, wired by `MountServer`/`MountManagedServer` right after construction; `openReplacement` now passes `processContext()` as the process-lifetime argument to the two-context `openMCPClient`.
- **Files modified:** internal/agent/mcptools/bridge_reconnect.go, internal/agent/mcptools/mount.go
- **Verification:** New `TestOpenReplacement_ReconnectedSubprocessSurvivesHandshakeCtxCancel` (real, unstubbed subprocess) fails with "the pipe is being closed" against the pre-fix wiring, passes with the fix; confirmed both directions.
- **Committed in:** 83dbec81 (RED, test only) / baf3094a (GREEN)

**3. [Rule 1 - Bug, pre-existing test] `TestBootReleaseResourcesDrainsClosersAndClosesPool` raced on a shared `*[]string`**
- **Found during:** GREEN verification (a multi-package `go test` run intermittently failed this unrelated, pre-existing test; a standalone run passed)
- **Issue:** The test recorded closer-invocation order into a bare `*[]string` mutated by two closure literals passed to `closeMCPServers`. Under the OLD sequential `closeMCPServers`, this was safe (one goroutine, in order). Task 2's concurrent `errgroup`-based rewrite now invokes both closures on separate goroutines — an unsynchronized `append` from two goroutines is a genuine data race (occasionally producing a short/corrupted slice, which is exactly the observed intermittent failure), not a flaky assertion.
- **Fix:** Added a mutex-guarded `orderRecorder` type; `poolCloseSpy` and the test now use it. The test's real invariant (both closers drain; pool closes only after they all finish) is unchanged and still asserted.
- **Files modified:** cmd/aura/chat_boot_test.go
- **Verification:** `go test -race` (WSL, real cgo) green; `go test -count=10` of the single test green; 3 consecutive fresh full-package runs green (previously intermittently failed ~1-in-3).
- **Committed in:** baf3094a (GREEN)

---

**Total deviations:** 3 auto-fixed (all Rule 1 - bugs; two are genuinely new Pitfall #2-class discoveries the plan's own RED tests surfaced, one is a pre-existing test's concurrency-safety gap exposed by Task 2's intentional behavior change)
**Impact on plan:** All three were necessary for correctness — the first two are the actual DoS-mitigation bugs this plan exists to close (a bounded mount that isn't actually bounded, and a reconnect that kills its own replacement); the third is a test-only fix required because Task 2's concurrent shutdown (explicitly requested by the plan) invalidated an unstated sequential-execution assumption in an unrelated pre-existing test. No scope creep — no unrequested production features were added.

## Issues Encountered
- **The plan's literal "ship two atomic commits, RED then GREEN" instruction required temporarily re-reverting already-working GREEN code to author an honest RED commit.** Since Task 1's mount fix and Task 2's `bridge_reconnect.go` fix live in the same handful of files (`mount.go`, `bridge_reconnect.go`), and Task 2 itself has no `tdd="true"` gate, the two tasks' RED/GREEN cycles were interleaved rather than fully separable into 4 distinct commits. Resolved pragmatically: one RED commit (both new regression tests failing, `closeMCPServers`'s already-correct Task 2 code and tests passing since that task isn't TDD-gated) followed by one GREEN commit (both fixes + the third, GREEN-verification-discovered test fix). Documented here in full per the sanctioned "minimal compiling stub, then GREEN replaces the body" pattern's spirit.
- **`go test ./pkgA ./pkgB ./pkgC` (multiple packages in one invocation) intermittently failed a test that passed standalone.** Root-caused to the genuine data race in `chat_boot_test.go` (deviation #3), not to any cross-package interference — Go still runs each package's tests in its own process either way, but the OS's differing goroutine-scheduling behavior under the additional process load made the race visible more often in the combined invocation. Confirmed via `go test -race` in WSL after the fix: clean.
- **`go test -race` requires cgo; this Windows session has no gcc/w64devkit toolchain on PATH.** Resolved identically to 38-02: ran the full `-race` matrix (including the two new regression tests, the three shutdown tests, and the fixed `TestBootReleaseResourcesDrainsClosersAndClosesPool`) under WSL Ubuntu (`CGO_ENABLED=1`, native gcc 15) via the `/mnt/d/...` mount, in addition to native Windows (non-race) runs.

## User Setup Required

None - `AURA_MCP_MOUNT_TIMEOUT` (default 10s) and `AURA_MCP_SHUTDOWN_TIMEOUT` (default 5s) are optional, silently-defaulted Tier C env knobs (`envutil.IntDefault`) per this phase's convention (RESEARCH Assumption A3) — no action needed unless an operator wants to change them.

## Next Phase Readiness
- MCPH-04 (bounded mount + reap + bounded registry construction) and MCPH-06's shutdown half (concurrent aggregate deadline) are both closed and empirically verified, including the two Pitfall #2-class bugs the plan's own regression tests surfaced beyond its literal scope.
- 38-06 (the live probe / governance board visibility half of D-07) can build directly on this plan's WARN-and-drop behavior: a dropped/unreachable server has no new health-flag state introduced here (per the plan's `planner_assumptions`), so 38-06's live probe is the sole source of "unhealthy" visibility.
- No blockers. `AURA_MCP_MOUNT_TIMEOUT`/`AURA_MCP_SHUTDOWN_TIMEOUT` remain Tier C (read inside their respective packages, not registered in `config_knobs.go`), consistent with `AURA_MCP_STDIO_MAX_FRAME` from 38-02.

---
*Phase: 38-mcp-governance-hardening*
*Completed: 2026-07-18*

## Self-Check: PASSED

**Files verified to exist:**
- FOUND: internal/mcp/client.go
- FOUND: internal/agent/mcptools/mount.go
- FOUND: internal/agent/mcptools/mount_retry.go
- FOUND: internal/agent/mcptools/bridge.go
- FOUND: internal/agent/mcptools/bridge_reconnect.go
- FOUND: internal/agent/mcptools/bridge_reconnect_realsubprocess_test.go
- FOUND: internal/agent/mcptools/bridge_reconnect_branches_test.go
- FOUND: internal/agent/mcptools/bridge_test.go
- FOUND: internal/agent/mcptools/bridge_trust_test.go
- FOUND: internal/agent/mcptools/managed_mount_test.go
- FOUND: internal/agent/mcptools/memory_integration_test.go
- FOUND: internal/agent/mcptools/mount_retry_test.go
- FOUND: internal/agent/mcptools/mount_test.go
- FOUND: internal/agent/memory_recall_integration_test.go
- FOUND: internal/eval/harness_swarm_e2e_test.go
- FOUND: cmd/aura/main.go
- FOUND: cmd/aura/main_test.go
- FOUND: cmd/aura/registry_test.go
- FOUND: cmd/aura/chat_boot_test.go

**Commits verified to exist (`git log --oneline --all`):**
- FOUND: 83dbec81 (test: RED, bounded two-context mount + reconnect Pitfall #2)
- FOUND: baf3094a (feat: GREEN, two-context split + reconnect fix)

**Plan-level verification re-confirmed:**
- `go build ./...` clean (Windows native + WSL/CGO_ENABLED=1).
- `go vet ./...` clean, default + `memory_integration` + `cot_eval` tags (both platforms).
- `golangci-lint run ./internal/agent/mcptools/... ./internal/mcp/... ./cmd/aura/...`: 0 issues.
- `bash scripts/check-file-size.sh`: all 2010 tracked source files within the 600-LOC cap.
- `go test ./internal/mcp/... ./internal/agent/mcptools/... ./cmd/aura/...` (Windows native): 3 consecutive fresh (`-count=1`) runs, all green, no flakiness.
- `go test -race ./internal/mcp/... ./internal/agent/mcptools/... ./cmd/aura/...` (WSL, real cgo): all green — includes both new regression tests, the three concurrent-shutdown tests, and the fixed `TestBootReleaseResourcesDrainsClosersAndClosesPool`; `TestMain`'s `goleak.VerifyTestMain` passed (no leaked goroutines from the abandoned-straggler shutdown path).
- The plan's exact `<verify>`/`<verification>` grep/test invocations for both tasks were re-run verbatim and pass.
- grep checks: `exec.CommandContext` bound to `processCtx` (not `handshakeCtx`) in client.go; `errgroup` present in `cmd/aura/main.go`; `closeWaitTimeout`/`httpCloseTimeout` still `5 * time.Second`, untouched.
