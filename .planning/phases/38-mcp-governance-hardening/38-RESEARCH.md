# Phase 38: MCP Governance Hardening - Research

**Researched:** 2026-07-18
**Domain:** Go backend hardening — MCP (Model Context Protocol) transport classification, process lifecycle, audited configuration writes, live health probing
**Confidence:** HIGH

## Summary

This phase closes nine audit findings (F-013/014/027/033/034/035/037/038/046) by consolidating scattered MCP transport/trust logic into one classifier, fixing an unbounded stdio read, adding bounded mount/shutdown lifecycles, routing CLI writes through the existing audit substrate, and making the HTTP MCP probe live everywhere it is surfaced. Every piece of substrate this phase needs already exists in the codebase in a *proven, working form elsewhere* — this is a consolidation and wiring phase, not a green-field-design phase.

Three findings materially change scope versus what CONTEXT.md's summary implies, verified by direct code reading:

1. **F-013 is a live, confirmed bug, not a hypothetical.** Both `mcp.NormalizedTrust` (`internal/mcp/managed_config.go:220-235`) and `manager.normalizedTrustForServer` (`internal/mcp/manager/runtime.go:166-177`) contain an *identical* fallback: a `streamable_http`/URL-shaped server with an empty or unknown `Trust.Class` and no `recipe:` source prefix is auto-promoted to `TrustRemoteHTTP` — a **runnable** trust class. `RunnableManagedServers` only excludes `TrustBlocked`, so this server mounts with zero operator approval. This is the exact mechanism D-03/MCPH-02 must close: drop the auto-promote-to-`TrustRemoteHTTP` branch; an untrusted remote must resolve to `TrustBlocked`.
2. **MCPH-03/F-038 already appears fully shipped for the web endpoint.** `internal/agui/governance_write_api.go:112-133` (`handleMCPTrust`) already rejects empty body / `{}` / blank class / blank reason / unknown class with 400, and `internal/agui/governance_write_api_test.go:314-344` (`TestGovernanceWriteTrustRejectsUnderspecified`) already table-tests exactly those seven cases, asserting zero mutation and zero audit row. Treat this as **verify-and-guard**, not build. The real remaining SC3 work is entirely MCPH-07 (CLI audit).
3. **The literal file:line citations in CONTEXT.md for the "sequential mount loop" and the MCP-probe upgrade point at the wrong functions.** The actual per-server mount loop (F-033's own evidence file) is `cmd/aura/main.go`'s `buildRegistryWithMCP`, not `internal/mcp/manager/runtime.go:34` (that loop only builds launch configs, it never calls `mcp.Open`). The actual F-046 bug is in `cmd/aura/mcp_status.go`'s `writeRuntimeCheck`, not `cmd/aura/doctor.go`'s `doctorProbeMCPBinary` (which checks an unrelated Neo4j-Cypher sidecar binary). See Common Pitfalls #6 and #11 for the full trace.

**Primary recommendation:** Build `internal/mcp/classify.go` as the single source of truth for transport+trust resolution (fixing the F-013 promotion bug on the way), thread a *second, narrower* context into the mount path so the new mount-timeout never kills an already-healthy server, reuse the already-proven `internal/agent/tools` per-OS process-group-kill pair for D-10 instead of introducing Windows Job Objects, replace `bufio.Reader.ReadBytes('\n')` with `bufio.Scanner`+`.Buffer()`+`bufio.ErrTooLong` for D-08, and route every CLI MCP mutation through the exact same `manager.WriteConfigWithAudit` the web governance-write path already uses. Nine independent call sites re-implement "is this server remote/HTTP" today (see Architecture Patterns); the classifier collapse must account for all nine, not the four CONTEXT.md names.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Canonical transport classifier (MCPH-01, QUAL-03)**
- **D-01:** Introduce **one** canonical classifier in a new file `internal/mcp/classify.go` exporting `Classify(ManagedServer) → (ServerType, TrustClass, error)`. `validateManagedServers`, `NormalizedTrust`, `OpenServer`, and the manager mount path all call it. The existing `normalizedServerType` + `NormalizedTrust` collapse INTO it — a true single source of truth. Rationale: `managed_config.go` is already ~330 LOC (near the 600 cap); a new file keeps it under budget and makes the unify explicit. Industrial backing: LibreChat models transport selection as a single set of discriminated-union type guards (`isStdioOptions`/`isStreamableHTTPOptions`), not scattered checks.
- **D-02:** A **mixed `url`+`command` entry with no explicit `type`** is **rejected** by the classifier (error), never silently resolved. It must never reach stdio `Open`. (Today `normalizedServerType` silently resolves ambiguity to stdio — that is the F-027 bug being closed.) An explicit `type` disambiguates ONLY when the trust class matches the transport.

**Remote trust (MCPH-02, MCPH-03)**
- **D-03:** Empty/blank trust on a remote (`streamable_http`/URL) entry means **BLOCKED, not runnable**. Explicit trust is required for every runnable remote transport.
- **D-04 (security):** The blocked-on-empty + mixed-transport rules evaluate on the **merged effective config** (what `MountForIdentity` actually mounts). A per-identity override may toggle `enable` and adjust *local* trust, but **a per-identity override may NOT elevate a REMOTE (`streamable_http`) server to a runnable trust class** — making a remote runnable requires the **admin shared catalog**. Fail-closed for the network-facing transport. (Note: `MountForIdentity` in `managed_config_identity.go:150` currently *does* let a per-identity pref set trust on class-(a) servers; this decision constrains that for remotes.)
- **D-05:** The governance trust endpoint returns **400 with no config/audit change** on empty body, `{}`, blank reason, or unknown class. A known class + non-empty reason is required.

**Bounded lifecycle (MCPH-04, MCPH-05, MCPH-06)**
- **D-06 (mount timeout):** Each MCP mount runs under a **bounded per-server timeout, default ~10s, tunable via `AURA_MCP_MOUNT_TIMEOUT`**. On timeout the helper process is reaped and the server is **dropped (degrade-and-continue)** — registry construction returns within the deadline. `manager/runtime.go:34` currently mounts sequentially (`for name, server := range servers`) with no timeout — greenfield.
- **D-07 (hung-mount visibility):** A dropped/hung server is **surfaced**: WARN log + marked **unhealthy in `aura mcp status` and the governance health board**. Not a silent drop — the operator can see why a server is missing.
- **D-08 (stdio frame cap):** Stdio frames are capped at **1 MiB, tunable via `AURA_MCP_STDIO_MAX_FRAME`**. `client.go:352` currently reads frames via `bufio.Reader.ReadBytes('\n')` — unbounded growth until newline (the F-034 large-alloc bug). Replace with a bounded read; the `boundedbuffer` package is already imported in `client.go` (reusable).
- **D-09 (oversized-frame abort scope):** An over-cap frame **aborts the whole server transport deterministically** (tear down + mark unhealthy), NOT fail-one-call-and-resync. A desynced request/response stream is never trusted for the next call. Industrial backing: LibreChat's `guardMCPStreamableHTTPResponse` tears the transport down on a limit breach.
- **D-10 (process-tree kill):** Shutdown terminates the **stdio process tree** — Linux `Setpgid` process group, Windows Job Object — build-tagged per-OS. `client.go:462` `killProcess()` currently calls `cmd.Process.Kill()` (immediate PID only → leaked grandchildren, F-035).
- **D-11 (shutdown budget):** Registry shutdown closes all servers **concurrently under a single aggregate deadline (~5s)**; each stdio tree is killed and each HTTP close is bounded (~5s). Total shutdown stays bounded regardless of server count (not sequential N × timeout).

**Audited CLI writes (MCPH-07)**
- **D-12:** All CLI MCP mutations (add/trust/enable/disable/remove, profiles) route through the **manager audited atomic writer** and append `mcp_audit` (table exists via migration `0022`), and are **allowed under `server_production`** with a full audit trail. Any write path not yet routed through the audited writer is **explicitly marked unaudited and disallowed under production** (the requirement's literal OR).
- **D-13 (audit actor):** A CLI write has no web principal, but `mcp_audit.actor_identity_id` is `NOT NULL`. Fill it with a **`cli`-namespaced principal derived from the OS user** (e.g. `cli:<os-username>`) — no extra flag, attributable to the machine operator, distinguishable from web writes. Reason still required on `trust`.

**Legacy env (MCPH-08)**
- **D-14:** Under `server_production`, `AURA_MCP_SERVERS_JSON` is **prod-disabled unless `AURA_MCP_LEGACY_ENV_COMPAT=1`** is explicitly set; `dev`/`local_trusted` keep parsing it unchanged (`config_mcp.go` today). No-op under dev, hardens under prod.
- **D-15 (fail-closed):** Under `server_production` with the env **set** but the compat flag **unset**, serve/exec **hard-errors at boot** with a clear message naming the env var + the compat flag. A misconfigured prod deploy fails loudly instead of silently running without the operator's intended servers.

**Live HTTP probe (MCPH-09)**
- **D-16:** Upgrade the MCP probe from **binary-presence** (`doctorProbeMCPBinary` in `cmd/aura/doctor.go` today) to a **live HTTP dial + `tools/list`** under a short deadline (**~5s, tunable via `AURA_MCP_PROBE_TIMEOUT`**). A dead/typoed endpoint reports **`OK=false`**, not healthy-by-config.
- **D-17:** **One** probe implementation is reused across **three surfaces**: `aura doctor`, `aura mcp status`, and the governance health board.
- **D-18 (probe scope):** `aura doctor` live-probes **only enabled + runnable HTTP servers** (skip disabled/blocked). Keeps doctor fast + meaningful in CI/health; does not dial servers the operator turned off.

### Claude's Discretion
Remaining choices are planner/executor-technical and were explicitly delegated: exact error strings, which file each helper lands in, test-fixture shapes, the precise per-OS `SysProcAttr` wiring, and the internal shape of the bounded stdio reader. Sane defaults already pinned above (1 MiB frame, 5s probe, 10s mount, 5s shutdown).

### Deferred Ideas (OUT OF SCOPE)
None — discussion stayed within phase scope. (SSRF enforcement binding to the runtime profile is Phase 33 PROF-01/PROF-04, already noted in `transport.go`; docker-gateway runtime lifecycle is exercised only under `docker_integration` and remains daemon-gated.)
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| MCPH-01 | Single canonical transport classifier governs validation/trust-norm/eligibility/mounting/opening; mixed `url`+`command` rejected unless explicit type + matching trust disambiguates. | Nine existing call sites inventoried (Architecture Patterns §Call-Site Inventory); `Classify` signature + rejection rules sketched (Code Examples #1); the F-027 bug traced to `normalizedServerType`'s silent stdio default. |
| MCPH-02 | Empty/blank trust on a remote entry = BLOCKED, not runnable; explicit trust required for every runnable remote. | F-013's *exact* live mechanism found and quoted in two files (Summary + Pitfall #1); fix is "drop the auto-promote-to-`TrustRemoteHTTP` fallback," not a net-new rule. |
| MCPH-03 | Trust endpoint requires known class + non-empty reason; empty body/`{}`/blank reason/unknown class → 400, no mutation. | **Already shipped and tested** for the web endpoint (`handleMCPTrust` + `TestGovernanceWriteTrustRejectsUnderspecified`, both quoted verbatim). Remaining work is a dead-fallback cleanup in `TrustApprove` if the CLI reuses it (Pitfall #5). |
| MCPH-04 | Bounded per-server mount timeout; hung helper reaped; registry construction returns within deadline. | Real mount loop located (`cmd/aura/main.go:buildRegistryWithMCP`, not `runtime.go:34`); the existing `mcptools.MountWithRetry`/`mcpMountRetryPolicy` retry budget is ALREADY ctx-aware end-to-end (Pitfall #6); the two-context design fork (process lifetime vs. handshake deadline) is the load-bearing gotcha (Pitfall #2). |
| MCPH-05 | Stdio frames capped; oversized frame aborts deterministically, no large alloc. | `bufio.Scanner`+`.Buffer()`+`bufio.ErrTooLong` identified as the stdlib-native replacement for `ReadBytes` (Don't Hand-Roll #1); `boundedbuffer.Buffer`'s actual (wrong-fit) semantics documented (Pitfall #3); Scanner's EOF-swallowing divergence documented (Pitfall #4). |
| MCPH-06 | Shutdown bounds HTTP close + kills stdio process tree; no hang, no leaked children. | HTTP close is *already* bounded (`httpCloseTimeout = 5*time.Second`, verified); the codebase's own proven per-OS process-group-kill pair found (`internal/agent/tools/shell_exec_{unix,windows}.go`, Code Example #2); `closeMCPServers`'s sequential-no-deadline shape confirmed as the sole remaining gap (Pitfall #7). |
| MCPH-07 | CLI MCP mutations route through the audited atomic writer or are explicitly unaudited+disallowed under production. | Confirmed **every** CLI mutation (`cmd/aura/mcp.go`, `mcp_profile.go`) calls `mcp.SaveManagedConfig` directly today — zero audit, zero DB pool threaded into the CLI at all (Architecture Patterns §Governance Write Path). `os/user`-based `cli:<os-username>` derivation is genuinely new (verified: zero existing `os/user` usage in the repo). |
| MCPH-08 | Legacy `AURA_MCP_SERVERS_JSON` production-disabled unless compat flag set. | Exact existing bespoke-gate template found and quoted (`gateDestructiveShell`/`gateMUSRIsolation` in `config_validate.go`, Code Example #4); boot-enforcement call chain traced end-to-end (`cfg.Validate()` → `ValidateProfile` → aggregated gates, called from `chat_boot.go:227` before any DB work). |
| MCPH-09 | Live HTTP dial + `tools/list` probe; dead/typoed endpoint reports `OK=false`. | `mcp.ProbeServer` **already** dials HTTP correctly and is **already** tested (`probe_test.go`: `TestMCPProbe_HTTPEndpointDialsAndCountsTools`/`...DialFailure`). The real gap is two callers that bypass it (`cmd/aura/mcp_status.go:writeRuntimeCheck` hardcodes "http endpoint configured" without dialing; `mcpStatus` uses the non-live `SnapshotStatus`). D-17's "one probe, three surfaces" already holds for the governance board; doctor + `mcp status` need wiring, not a new probe. |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

These directives apply to every task this phase produces and were treated as authoritative during research (same tier as CONTEXT.md decisions):

- **PRD-first:** no code without a complete PRD; this phase's WHAT is already locked by REQUIREMENTS.md §MCPH, so no PRD-amendment is expected unless a plan deviates from D-01..D-18.
- **File size:** no tracked file >600 LOC. `internal/mcp/managed_config.go` is already ~330 LOC and MUST NOT absorb the classifier (D-01 already routes it to a new `classify.go` for this exact reason). `internal/mcp/client.go` is 477 LOC — the bounded-frame-reader change should stay lean; if the process-tree-kill build-tag split adds meaningfully, consider a sibling file rather than growing `client.go` further.
- **Deep refactor on touch:** every file edited in this phase gets dead-code removal + LOC ≤600 + updated comments in the same commit. This directly applies to the nine-call-site classifier collapse (Architecture Patterns) — each old scattered check should be deleted, not left as an unreachable duplicate.
- **Coverage floor 85%** across the full tag matrix (`db_integration neo4j_integration` only — no `docker_integration` job). Every daemon/container-gated code path in this phase (none are Docker-gated; the process-tree-kill code is plain OS-process, not Docker) still needs daemon-free unit tests. The ONE genuinely `db_integration`-gated behavior in this phase is the CLI audit-row write (MCPH-07); everything else must be provable with pure `go test`.
- **No-skip-as-green:** any new tagged test must `t.Fatal` under `$CI` if its env is missing, never silently skip.
- **Env var convention:** every new knob is `AURA_MCP_<UNIT>` (already matches D-06/D-08/D-14/D-16's literal names in CONTEXT.md).
- **Post-edit validation:** `go vet ./...`, `go build ./...`, `go test ./internal/<pkg>/`, `go test -race ./internal/<pkg>/` after every Go file edit — apply per-package (`internal/mcp`, `internal/mcp/manager`, `internal/agent/mcptools`, `internal/config`, `internal/agui`, `cmd/aura`) as each is touched.
- **Never modify tests to make them pass** unless the test itself encodes the OLD (buggy) behavior being deliberately fixed — this applies directly to `TestNormalizedServerType`/`TestNormalizedTrustRemoteHTTPInferred` in `managed_config_test.go`, which currently assert the F-013/F-027 buggy behavior and MUST be deliberately updated as part of this phase (not "broken tests to route around" — their assertions describe the bug being fixed).
- **Follow existing patterns; never invent new approaches when codebase patterns exist:** directly governs D-10 (reuse `internal/agent/tools`'s taskkill-based Windows kill instead of introducing Job Objects) and the CLI-audit wiring (reuse `manager.WriteConfigWithAudit` verbatim).
- **Reusable code, no duplication:** directly governs the process-group-kill extraction (Don't Hand-Roll #2) and the nine-call-site classifier collapse.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Transport/trust classification (`Classify`) | API/Backend (`internal/mcp`) | — | Pure in-process decision function; no I/O, no external boundary |
| Remote-trust elevation guard (D-04) | API/Backend (`internal/mcp`, per-identity overlay) | — | Enforced at the config-merge layer (`MountForIdentity`) and the write layer (`SetTrustForIdentity`), both in-process |
| Trust-endpoint 400 gate (D-05) | API/Backend (`internal/agui` HTTP handler) | — | Wire-boundary validation before any provider call; already implemented |
| Bounded mount + reap (D-06/D-07) | API/Backend (`cmd/aura` boot path, `internal/mcp` process spawn) | — | Boot-time in-process orchestration; no external service |
| Stdio frame cap + abort (D-08/D-09) | API/Backend (`internal/mcp/client.go`) | — | Raw transport/protocol framing over an OS pipe |
| Process-tree kill (D-10) | API/Backend (`internal/mcp`, OS process management) | — | Direct `os/exec`/`syscall` interaction, no external service |
| Concurrent bounded shutdown (D-11) | API/Backend (`cmd/aura` composition root) | — | Orchestrates in-process closers; no external service |
| Audited CLI writes (D-12/D-13) | API/Backend (`cmd/aura` CLI) | Database/Storage (`aura.mcp_audit`, Postgres) | The write is a CLI-process concern; the durability/append-only guarantee is enforced at the DB tier (triggers, role grants) |
| Legacy env prod-gating (D-14/D-15) | API/Backend (`internal/config` boot validation) | — | Boot-time config-validation concern, no external service |
| Live HTTP probe (D-16/D-17/D-18) | API/Backend (`internal/mcp/probe.go`, consumed by `cmd/aura` + `internal/agui`) | — | Outbound HTTP dial from the daemon process to a configured MCP endpoint; no browser/CDN tier involved anywhere in this phase |

This entire phase lives in the API/Backend tier — there is no browser, SSR, or CDN capability anywhere in scope. The only secondary tier is Database/Storage, and only for the append-only audit ledger.

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `bufio` (stdlib) | go1.26.5 | Bounded line-oriented stdio frame reading with a hard size cap and a distinguishable "too large" error | `bufio.Scanner` + `.Buffer(buf, max)` already implements exactly "read up to N bytes looking for a delimiter, return a named sentinel (`bufio.ErrTooLong`) if exceeded" — no hand-rolled truncation logic needed |
| `context` (stdlib) | go1.26.5 | Per-operation deadlines: mount handshake, live probe, aggregate shutdown | Already the pervasive concurrency primitive across this exact codebase (`readResponseContext`, `MountWithRetry`, `handleMCPProbe`) |
| `os/exec` (stdlib) | go1.26.5 | Subprocess spawn, `SysProcAttr`, `cmd.Cancel` hook | Already the substrate of `mcp.Client`; `cmd.Cancel` (Go 1.20+) lets ctx-cancellation trigger a custom kill function instead of the default single-PID `Process.Kill()` |
| `syscall` (stdlib) + `golang.org/x/sys` | v0.46.0 (promote `// indirect` → direct) | Unix process-group `Setpgid` + `Kill(-pid, SIGKILL)` | Already used identically in `internal/agent/tools/shell_exec_unix.go`; `golang.org/x/sys` is already resolved in `go.sum` transitively, so promoting it to direct is a `go.mod` edit, not a new dependency |
| `golang.org/x/sync/errgroup` | v0.21.0 (already a direct dependency) | Fan-out concurrent shutdown of N MCP transports under one aggregate deadline | Idiomatic Go concurrency-group primitive; already vendored and used elsewhere in this codebase's dependency tree |
| `os/user` (stdlib) | go1.26.5 | Derive the `cli:<os-username>` audit actor (D-13) | Cross-platform current-user lookup; **zero existing usage in this repo** — verified via repo-wide grep, this is genuinely new code, not a reuse |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/jackc/pgx/v5` + generated `sqlc` | already vendored | `MCPAuditInsert`/`InsertMCPAuditTx` inside `db.WithTx` | Reuse verbatim for CLI writes — do not build a parallel audit mechanism |
| `go.uber.org/goleak` | already vendored, wired via `internal/mcp/main_test.go` `TestMain` | Goroutine-leak detection for the bounded-mount / bounded-shutdown tests | Any new test in `internal/mcp` automatically runs under this `TestMain`; tests in `cmd/aura`/`internal/agent/mcptools` should check for an equivalent `TestMain` before assuming leak-detection is active there |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `bufio.Scanner` + `.Buffer()` for the bounded frame reader | A hand-rolled incremental reader atop the existing `*bufio.Reader` | Only justified if Scanner's EOF-swallowing semantics (Pitfall #4) prove unworkable after a spike — unlikely; stdlib-first per "Don't Hand-Roll" |
| `syscall.Kill(-pid, SIGKILL)` (Unix) + `taskkill /F /T` (Windows, shelled out) | `golang.org/x/sys/windows` Job Objects (`CreateJobObject`/`AssignProcessToJobObject`/`TerminateJobObject`) | Job Objects are more "textbook correct" (auto-kill on handle close, survives re-parenting) but this codebase already has a *working, CI-proven* taskkill-based pattern in production (`internal/agent/tools`); introducing Job Objects would be new, untested-in-this-repo machinery for marginal benefit over an already-shipped mechanism — reject per "follow existing patterns" |
| `errgroup` fan-out + one wrapping `context.WithTimeout` for shutdown | Manual `sync.WaitGroup` + buffered channel + `select` | `errgroup` is more idiomatic and already a direct dependency; the manual version is more code for the identical outcome |
| Extracting `setProcessGroup`/`killProcessGroup` to a shared package | Duplicating the ~30 LOC pair into `internal/mcp` | Duplication violates "REUSABLE CODE. Never duplicate; extract a helper" (CLAUDE.md); extraction is the correct call, exact new package name is Claude's Discretion |

**Installation:**
```bash
# No new external packages. Promote an existing indirect dependency to direct:
go get golang.org/x/sys@v0.46.0
go mod tidy
```

**Version verification:** Both `golang.org/x/sys v0.46.0` and `golang.org/x/sync v0.21.0` are already present and resolved in this repo's `go.mod`/`go.sum` (confirmed via direct grep, 2026-07-18). No registry lookup was needed since nothing new is being introduced — this phase adds zero new third-party packages.

## Package Legitimacy Audit

**No new external packages are introduced by this phase.** Every capability needed (bounded scanning, process-group kill, concurrent shutdown, audit-actor derivation) is covered by Go stdlib (`bufio`, `syscall`, `os/exec`, `os/user`) plus two dependencies **already present and in active use elsewhere in this exact codebase**:

| Package | Registry | Age/Status in this repo | Source Repo | Disposition |
|---------|----------|--------------------------|-------------|-------------|
| `golang.org/x/sys` | Go module proxy | Already resolved at v0.46.0, currently `// indirect` in `go.mod`; being promoted to a direct import (no version change) | `go.googlesource.com/sys` (golang.org/x org, canonical Go extended stdlib) | Approved — pre-existing, not newly introduced |
| `golang.org/x/sync` | Go module proxy | Already a direct dependency at v0.21.0, `errgroup` sub-package already available | `go.googlesource.com/sync` (golang.org/x org, canonical Go extended stdlib) | Approved — pre-existing, not newly introduced |

**Packages removed due to slopcheck `[SLOP]` verdict:** none (nothing new to check).
**Packages flagged as suspicious `[SUS]`:** none.

The slopcheck/registry-verification protocol was not run because there is nothing new to verify — both packages are golang.org/x canonical extended-stdlib modules already vendored in this exact repository's `go.sum`, verified by direct grep rather than external lookup.

## Architecture Patterns

### System Architecture Diagram — Lifecycle (mount → call → shutdown)

```
Boot (cmd/aura/main.go: buildRegistryWithMCP)
  │
  ├─ for each configured server name (sequential, sorted) ──────────────────┐
  │                                                                          │
  │   mountOnce(ctx) := MountManagedServer | MountServer                    │
  │        │                                                                │
  │        ▼                                                                │
  │   [NEW] bounded handshake ctx = context.WithTimeout(daemonCtx, ─────┐    │
  │          AURA_MCP_MOUNT_TIMEOUT)          ◄── narrower than          │    │
  │        │                                     daemonCtx — see         │    │
  │        ▼                                     Pitfall #2              │    │
  │   mcp.Open(daemonCtx, handshakeCtx, name, cfg)                      │    │
  │        │  spawns subprocess, cmd := exec.CommandContext(daemonCtx,…) │    │
  │        │  (process lifetime = daemon lifetime, NOT mount timeout)   │    │
  │        │  setProcessGroup(cmd)  [NEW, D-10]                         │    │
  │        │  cmd.Start()                                               │    │
  │        ▼                                                            │    │
  │   initializeContext(handshakeCtx)  ── bounded by AURA_MCP_MOUNT_TIMEOUT
  │        │  on timeout: kill via cmd.Cancel = killProcessGroup(cmd)   │    │
  │        │  (whole tree dies, not just the tracked PID)               │    │
  │        ▼                                                            │    │
  │   mcptools.MountWithRetry (ALREADY ctx-aware end-to-end,            │    │
  │        existing 6-attempt capped-exponential backoff; the NEW       │    │
  │        bounded handshakeCtx caps the WHOLE retry budget, not        │    │
  │        each attempt individually)                                   │    │
  │        │  success ──► srv := newReconnectingServer(name,cfg,cli)    │    │
  │        │                    (EXISTING — see below)                  │    │
  │        │  failure ──► [NEW D-07] WARN log + mark unhealthy in       │    │
  │        │              aura mcp status / governance health board;   │    │
  │        │              degrade-and-continue (boot never aborts)     │    │
  │        └──────────────────────────────────────────────────────────┘    │
  └───────────────────────────────────────────────────────────────────────┘
  │
  ▼
tools.Registry (frozen, immutable once agent runs)


Runtime call (agent turn → tool dispatch → reconnectingServer → mcp.Client)
  │
  ▼
reconnectingServer.CallTool/ListTools (EXISTING, internal/agent/mcptools/bridge_reconnect.go)
  │  client.CallTool(ctx,…) ──► mcp.Client.roundtripContext ──► readResponseContext
  │       │
  │       ▼
  │  [NEW D-08] bufio.Scanner-based bounded read (cap = AURA_MCP_STDIO_MAX_FRAME)
  │       │  normal line  ──► JSON-RPC response, matched by id, returned
  │       │  oversized    ──► bufio.ErrTooLong ──► [NEW D-09] abortTransport()
  │       │                    (kill+close, deterministic — never resync)
  │       │                    returns error satisfying mcp.IsTransportError
  │       ▼
  │  IsTransportError == true ──► reconnectingServer ALREADY treats this as
  │       fatal-to-this-connection: closes the failed client, spawns a FRESH
  │       mcp.Open + handshake (own backoff + 3-strike circuit breaker),
  │       NEVER reuses the poisoned pipe. D-09's "abort deterministically,
  │       never resync" is already structurally guaranteed at THIS layer —
  │       see Pitfall #9.
  └───────────────────────────────────────────────────────────────────────


Shutdown (cmd/aura/main.go: closeMCPServers)
  │
  │  TODAY: sequential reverse-order loop, each closer() blocking in turn
  │         (stdio Close ~5s+kill; HTTP Close ~5s — EACH already bounded,
  │         but N servers cost up to 5×N seconds serially)
  │
  ▼
  [NEW D-11] fan out ALL closers concurrently (errgroup or goroutines+chan)
       under ONE aggregate context.WithTimeout(ctx, AURA_MCP_SHUTDOWN_TIMEOUT)
       │  each stdio closer ──► Client.Close() ──► killProcessGroup (D-10,
       │                         whole tree, not just the tracked PID)
       │  each HTTP closer  ──► HTTPClient.Close() (already bounded 5s)
       │  aggregate deadline fires ──► log + return; abandoned stragglers
       │                         finish shortly after on their OWN internal
       │                         bound (harmless, see Pitfall #8 re: goleak)
       └───────────────────────────────────────────────────────────────────
```

### System Architecture Diagram — Governance Write + Probe (CLI/Web → audit; probe reuse)

```
Web:  POST /api/governance/mcp/{name}/trust
        │
        ▼
      handleMCPTrust (internal/agui/governance_write_api.go)
        │  400 gate: class=="" || reason=="" || !IsKnownTrust(class)
        │  (ALREADY SHIPPED — MCPH-03/D-05/F-038)
        ▼
      mcpWriteAdapter.TrustApprove (cmd/aura/serve_governance_write.go)
        │  ⚠ has a dead "class==''→trusted_local" fallback, unreachable
        │    via HTTP today but LIVE if the CLI calls this directly
        │    without pre-validating (Pitfall #5)
        ▼
      manager.WriteConfigWithAudit (temp file → db.WithTx(InsertMCPAuditTx)
        │                            → os.Rename on commit)
        ▼
      aura.mcp_audit (append-only, migration 0022, triggers reject UPDATE/
                       DELETE/TRUNCATE)

CLI:  aura mcp trust <name>   [TODAY: bypasses ALL of the above]
        │
        ▼
      mcpTrust (cmd/aura/mcp_profile.go)
        │  hardcodes Class=TrustTrustedLocal, Reason="" (no --reason flag)
        │  mcp.SaveManagedConfig(path, doc)  ── DIRECT file write, NO pool,
        │                                        NO audit row (F-037 gap)
        ▼
      [NEW D-12/D-13] must route through the SAME WriteConfigWithAudit,
        with ActorIdentityID = "cli:" + osUsername (os/user.Current(),
        graceful fallback chain — see Pitfall #10), threading a *pgxpool.Pool
        into the CLI subcommand dispatch for the first time (genuinely new
        plumbing — see Common Pitfalls #10)


Probe reuse (D-16/D-17/D-18):

  mcp.ProbeServer(ctx, name, server)   ── ALREADY correct + ALREADY tested
        │                                  for BOTH stdio and streamable_http
        │                                  (probe_test.go: HTTPEndpointDials
        │                                  AndCountsTools / …DialFailure)
        │
        ├──► governance health board (internal/agui/governance_api.go
        │       handleMCPProbe) ── ALREADY wired, bounded 3s (defaultProbeTimeout)
        │
        ├──► aura mcp status ── [GAP] mcpStatus() uses SnapshotStatus
        │       (config-derived StartupState, never dials) — needs wiring
        │
        └──► aura mcp doctor --all ── [GAP] writeRuntimeCheck special-cases
                streamable_http: prints "http endpoint configured" and
                SKIPS calling ProbeServer entirely (the literal F-046 bug);
                stdio path already calls ProbeServer correctly

  aura doctor (top level, cmd/aura/doctor.go doctorChecks())
        │  today's 5 checks (postgres/neo4j/embed/mcp-neo4j-cypher/llm_key)
        │  do NOT touch the managed MCP server registry AT ALL —
        │  doctorProbeMCPBinary checks an UNRELATED Neo4j-Cypher sidecar
        │  binary via LookPath, not any managed MCP server (Pitfall #11)
        │
        └──► [NEW] add a 6th check that live-probes enabled+runnable HTTP
               servers via mcp.ProbeServer, bounded by AURA_MCP_PROBE_TIMEOUT
               (D-18: skip disabled/blocked)
```

### Call-Site Inventory — why the classifier collapse is bigger than 4 functions

CONTEXT.md names four call sites for D-01 (`validateManagedServers`, `NormalizedTrust`, `OpenServer`, the manager mount path). Direct code reading found **nine** independent re-implementations of "is this server remote/HTTP" and/or "what is its effective trust", several of which are security-gating, not display-only:

| # | File:Function | Shape | Security-gating? |
|---|---------------|-------|-------------------|
| 1 | `internal/mcp/managed_config.go` `normalizedServerType` | `Type==""` → infer from `URL!=""&&Command==""`, else stdio; **silently resolves ambiguity to stdio (F-027)** | **Yes** — feeds #2, #3, #4 |
| 2 | `internal/mcp/managed_config.go` `NormalizedTrust` | Calls #1; **auto-promotes empty/unknown trust on an HTTP-shaped server to `TrustRemoteHTTP` (F-013)** | **Yes** — feeds `RunnableManagedServers`-equivalent gating |
| 3 | `internal/mcp/managed_config.go` `EnabledServers` | Calls #1 directly to exclude HTTP servers from the stdio launch map | No (display/filter only) |
| 4 | `internal/mcp/transport.go` `OpenServer` | Calls #1 to dispatch `Open` (stdio) vs `OpenHTTP` — **the exact SC1 "never call stdio open" gate** | **Yes — highest priority** |
| 5 | `internal/mcp/manager/runtime.go` `isStreamableHTTPServer` | Independent reimplementation: `Type==X \|\| URL!=""` | Yes — feeds `RuntimeServers`/`RunnableManagedServers` |
| 6 | `internal/mcp/manager/runtime.go` `normalizedTrustForServer` | Independent reimplementation of #2's shape; **same F-013 auto-promote bug, duplicated** | **Yes** |
| 7 | `internal/mcp/manager/status.go` `runtimeName` | Inline `URL!=""\|\|Type==X` for a display label | No (display only) |
| 8 | `internal/agent/mcptools/mount.go` `isStreamableHTTPManagedServer` | Independent reimplementation, chooses `OpenServer` vs `managedStdioConfig`+`Open` at the **actual mount call site** | **Yes — second gate over stdio Open** |
| 9 | `cmd/aura/mcp_status.go` `writeRuntimeCheck` | Inline `Type==X\|\|URL!=""` to skip the live probe for HTTP servers (the F-046 spot) | No (probe-routing, not trust-gating, but still worth collapsing) |

**Recommendation:** migrate all nine onto `Classify`. Sites #1/#2/#4/#5/#6/#8 are security-relevant and must be prioritized first; #3/#7/#9 are consistency cleanups that can follow once the gating sites are converted, but leaving them on the old logic re-creates exactly the "scattered checks" problem this phase exists to close.

### Recommended Project Structure

```
internal/mcp/
├── classify.go              # NEW — Classify(ManagedServer) (ServerType, TrustClass, error)
├── classify_test.go         # NEW — table tests: mixed url+command, empty remote trust, etc.
├── managed_config.go        # normalizedServerType/NormalizedTrust bodies replaced with thin
│                             #   Classify(...) calls; validateManagedServers calls Classify
├── client.go                # readResponseBlocking: bufio.Scanner replaces bufio.Reader.ReadBytes;
│                             #   killProcess delegates to the extracted process-group helper
├── client_unix.go (or reuse a shared internal/procgroup package)   # NEW build-tagged
├── client_windows.go (or the shared package's windows file)        # NEW build-tagged
├── probe.go                 # unchanged (already correct + tested)
internal/mcp/manager/
├── runtime.go                # isStreamableHTTPServer/normalizedTrustForServer replaced with
│                              #   Classify(...) calls
├── status.go                 # runtimeName replaced with Classify(...) call
internal/agent/mcptools/
├── mount.go                  # isStreamableHTTPManagedServer replaced with Classify(...) call
├── mount_timeout.go          # NEW (or extend mount_retry.go) — per-server bounded handshake ctx
cmd/aura/
├── main.go                   # buildRegistryWithMCP: bounded handshake ctx; closeMCPServers:
│                              #   concurrent fan-out + aggregate deadline
├── mcp.go, mcp_profile.go    # every mutation routes through a new shared write-with-audit
│                              #   helper (pool + cli:<osuser> actor)
├── mcp_audit_actor.go         # NEW — os/user-based `cli:<os-username>` derivation, graceful fallback
├── mcp_status.go              # writeRuntimeCheck/mcpStatus wired to mcp.ProbeServer for HTTP too
├── doctor.go                  # NEW 6th check: live-probe enabled+runnable HTTP MCP servers
internal/config/
├── config_validate.go         # NEW gateMCPLegacyEnv(p RuntimeProfile) []Violation
├── config_knobs.go             # NEW KnobSpec row for AURA_MCP_LEGACY_ENV_COMPAT (KindBool,
│                                #   read inside internal/config — in scope per existing precedent)
```

### Pattern 1: Discriminated classification via a single validating function (not a tagged union type)

**What:** Go has no algebraic sum types. The existing codebase's idiom for "one of several shapes" is a plain value struct (`ManagedServer`) inspected by a switch/if-chain inside one function that returns the resolved discriminant plus an error — not a generics-heavy polymorphic type hierarchy.
**When to use:** `Classify(ManagedServer) (ServerType, TrustClass, error)` — mirrors the exact shape LibreChat uses conceptually (`isStdioOptions`/`isStreamableHTTPOptions` as boolean type-guards feeding a switch), translated to Go's idiom of "one function, multiple return values, explicit error."
**Example (recommended shape, not final code — exact error strings are Claude's Discretion):**
```go
// Source: pattern distilled from LibreChat packages/api/src/mcp/connection.ts
// (isStdioOptions/isStreamableHTTPOptions, read for approach only — TypeScript, not ported).
func Classify(s ManagedServer) (serverType string, trust string, err error) {
    hasURL := strings.TrimSpace(s.URL) != ""
    hasCmd := strings.TrimSpace(s.Command) != ""
    explicitType := strings.TrimSpace(s.Type)

    switch {
    case explicitType == "" && hasURL && hasCmd:
        // D-02: mixed, ambiguous, no explicit type — reject, never silently resolve to stdio.
        return "", "", fmt.Errorf("mcp classify: server has both url and command with no explicit type")
    case explicitType == "" && hasURL:
        serverType = ServerTypeStreamableHTTP
    case explicitType == "" && hasCmd:
        serverType = ServerTypeStdio
    case explicitType == ServerTypeStdio, explicitType == ServerTypeStreamableHTTP:
        serverType = explicitType
        // TODO(planner): trust/transport compatibility matrix — see Open Questions #1.
    default:
        return "", "", fmt.Errorf("mcp classify: unknown type %q", s.Type)
    }

    trust = resolveTrust(s, serverType) // absorbs NormalizedTrust's logic MINUS the F-013
                                         // auto-promote-to-remote_http fallback (Pitfall #1)
    return serverType, trust, nil
}
```

### Pattern 2: Two-context mount (process lifetime vs. handshake deadline)

**What:** `exec.CommandContext(ctx, ...)` ties the *entire subprocess lifetime* to `ctx` — the stdlib kills the process the moment `ctx` is done, not just the in-flight call. A per-mount timeout context must therefore bound ONLY the initialize handshake, never the context passed to `exec.CommandContext`, or every successfully-mounted server would be killed exactly `AURA_MCP_MOUNT_TIMEOUT` seconds after boot.
**When to use:** Any time a bounded-attempt operation (mount handshake) sits inside a long-lived resource's constructor (the subprocess itself, meant to run for the daemon's lifetime).
**Example:**
```go
// processCtx: the long-lived daemon ctx already threaded through chat_boot.go → this
// is what exec.CommandContext must use, so the subprocess's lifetime = daemon lifetime.
cmd := exec.CommandContext(processCtx, command, args...)
setProcessGroup(cmd) // D-10, before Start()
cmd.Cancel = func() error { return killProcessGroup(cmd) } // whole tree, not just the PID
if err := cmd.Start(); err != nil { ... }

// handshakeCtx: a SEPARATE, narrower deadline for ONLY the initialize round-trip (D-06).
handshakeCtx, cancel := context.WithTimeout(processCtx, mountTimeout)
defer cancel()
if err := c.initializeContext(handshakeCtx); err != nil {
    // handshake timed out or failed — kill the (already spawned) subprocess and
    // return, WITHOUT tearing down processCtx (no other server is affected).
    _ = c.Close()
    return nil, err
}
// success: the Client now lives under processCtx only; handshakeCtx is already
// cancelled (deferred) and has no further effect on the running subprocess.
```

### Anti-Patterns to Avoid

- **Passing one bounded `context.WithTimeout(daemonCtx, mountTimeout)` straight into both `exec.CommandContext` and the handshake read.** This is the single highest-risk mistake in this phase: it silently kills every healthy MCP server `AURA_MCP_MOUNT_TIMEOUT` seconds after a successful mount. See Pitfall #2.
- **Reaching for `boundedbuffer.Buffer` for the D-08 frame cap.** That type is a silent keep-newest-N-bytes sink built for stderr-tail capture; it never errors, so using it here would produce silent truncation (still desyncing the stream) instead of the required deterministic abort. See Pitfall #3.
- **Introducing `golang.org/x/sys/windows` Job Objects for D-10.** The codebase already has a proven, shipped, CI-exercised process-group-kill pair (`internal/agent/tools/shell_exec_{unix,windows}.go`) that satisfies the D-10 intent with less new surface area. Reuse it (via extraction), don't build a second, different mechanism.
- **Leaving the `manager/runtime.go`/`mount.go`/`status.go` duplicate classification functions in place "because they still work."** They encode the exact F-013 bug independently of `managed_config.go`'s copy — fixing one and not the others leaves a live security hole in the un-migrated call sites.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Bounded line read with a deterministic "too large" error | A custom incremental reader atop `bufio.Reader` that manually tracks byte counts and truncates | `bufio.Scanner` + `.Buffer(initial, max)`, checking `errors.Is(scanner.Err(), bufio.ErrTooLong)` after `Scan()` returns false | Stdlib already implements exactly this contract; hand-rolling risks off-by-one/allocation bugs in security-sensitive framing code |
| Cross-platform process-tree kill | `golang.org/x/sys/windows` Job Object plumbing (`CreateJobObject`/`SetInformationJobObject`/`AssignProcessToJobObject`) | The already-shipped `internal/agent/tools/shell_exec_{unix,windows}.go` pair (`Setpgid`+`syscall.Kill(-pid,...)` / `CREATE_NEW_PROCESS_GROUP`+`taskkill /F /T`), extracted to a shared location | Duplicating ~30 LOC violates "never duplicate, extract a helper"; introducing a second, different Windows-kill mechanism when a working one already exists in this repo violates "follow existing patterns" |
| Concurrent-with-timeout fan-in for shutdown | Manual `sync.WaitGroup` + hand-rolled channel-close-race handling | `golang.org/x/sync/errgroup` (already a direct dependency) fanning out goroutines, wrapped in one `context.WithTimeout` | Idiomatic, already vendored, less code for the same guarantee |
| Env-var-driven timeouts/caps (seconds/bytes) | A new bespoke env parser | `internal/envutil.IntDefault(key, fallbackSeconds)` (already exists, silent-fallback semantics matches the rest of this domain's env knobs) × `time.Second`/`* 1` for bytes | Already extracted in Phase 32 (QUAL-03) specifically to stop this kind of copy-paste; reuse it rather than re-deriving int-parsing-with-fallback again |
| Audited atomic config write for the CLI | A parallel "CLI-flavored" atomic-write+audit mechanism | `manager.WriteConfigWithAudit` (already exists, already correct: temp file → `db.WithTx(InsertMCPAuditTx)` → `os.Rename` on commit) | Building a second mechanism for the same invariant (config mutation + audit row commit together) is exactly the kind of duplication CLAUDE.md forbids, and doubles the surface that can drift out of sync |
| Fake external stdio MCP servers for tests | A new subprocess-testing harness | The already-established `TestHelperProcess` re-exec idiom (`internal/mcp/client_open_test.go`, reused in 7+ other test files across `cmd/aura`) — extend its mode dispatch (today: `""`/`"crash"`/`"hang"`) with new modes as needed | This exact pattern is already proven portable across this project's Windows+WSL/Linux CI matrix; a new approach would need to re-earn that portability guarantee |

**Key insight:** every "don't hand-roll" item in this phase has a working, already-proven-in-this-exact-codebase reference implementation. This is unusual and worth naming explicitly: the risk in this phase is not "we lack a pattern," it's "an implementer reaches for a plausible-looking but wrong-fit existing type (`boundedbuffer.Buffer`) or invents new machinery (Job Objects) when a correct answer already ships elsewhere in the repo."

## Common Pitfalls

### Pitfall 1 (CRITICAL — this IS the F-013 bug): The current trust-normalization fallback auto-promotes an untrusted remote to a runnable class
**What goes wrong:** `mcp.NormalizedTrust` (`managed_config.go:220-235`) and `manager.normalizedTrustForServer` (`runtime.go:166-177`) both contain this exact fallback chain: if the trust class is unknown/empty AND the source isn't `recipe:`-prefixed AND the server is HTTP/URL-shaped, **return `TrustRemoteHTTP`** — a class `RunnableManagedServers` treats as runnable (it only excludes `TrustBlocked`).
**Why it happens:** The original author intended "infer a sensible default trust for a recognizable shape," but the effect is that *any* custom `url`-only entry with no `trust` block at all becomes immediately runnable with zero operator approval — silently defeating the entire trust-approval workflow for remotes.
**How to avoid:** In the new `Classify`, remove the "streamable_http → `TrustRemoteHTTP`" fallback branch entirely. An HTTP-shaped server with no known trust class and no recipe-prefix source must resolve to `TrustBlocked`. Preserve the recipe-prefix → `TrustTrustedRecipe` branch (that's Aura's own catalog vetting, a genuinely different case from "nothing was ever set").
**Warning signs:** `TestNormalizedTrustRemoteHTTPInferred` (`managed_config_test.go:290`) currently asserts the OLD (buggy) behavior by name — it must be deliberately rewritten as part of this fix, not treated as a regression to preserve.

### Pitfall 2 (CRITICAL, load-bearing): A single bounded context for both process-lifetime and handshake would kill healthy servers
**What goes wrong:** `mcp.Open(ctx, name, cfg)` today uses ONE `ctx` for both `exec.CommandContext(ctx, ...)` (the subprocess's entire tracked lifetime) and `c.initializeContext(ctx)` (the handshake read). If the D-06 fix wraps the *existing* call with `context.WithTimeout(daemonCtx, mountTimeout)` and passes that single bounded context straight into `Open`, the subprocess will be killed by the stdlib's ctx-cancellation watcher the moment `mountTimeout` elapses — **even if the mount succeeded in the first 200ms and the server has been serving fine since.**
**Why it happens:** `exec.CommandContext`'s documented behavior is "the provided context is used to kill the process... if the context becomes done before the command completes on its own" — this is unconditional, not scoped to "only during the handshake."
**How to avoid:** Thread two contexts: the long-lived daemon/process context (unchanged, used for `exec.CommandContext`) and a new, narrower `context.WithTimeout(daemonCtx, mountTimeout)` used ONLY for the `initializeContext` call. See Pattern 2 and Code Example above for the exact shape. This requires a signature change somewhere in the `Open` → `initializeContext` call chain (or an overload/variant), which is genuinely new API surface, not a one-line fix.
**Warning signs:** Any test where a successfully-mounted server's process disappears roughly `AURA_MCP_MOUNT_TIMEOUT` seconds after boot is this bug.

### Pitfall 3: `boundedbuffer.Buffer` is the wrong tool for the D-08 frame cap
**What goes wrong:** `internal/boundedbuffer/buffer.go`'s `Buffer` type is a `Write`-only, mutex-guarded ring-like sink that **silently keeps only the newest N bytes** on overflow — it has no `Read` method, no "exceeded" signal, and never returns an error. It is used today in `client.go` ONLY to capture a bounded tail of stderr for error messages (`stderr = boundedbuffer.New(0)`). CONTEXT.md's code-context note ("the boundedbuffer package is already imported... reusable") is about *pattern proximity*, not a literal fit.
**Why it happens:** The name "bounded buffer" is a plausible-sounding match for "bounded stdio frame," but the semantics (silent-truncate-keep-newest vs. deterministic-error-on-exceed) are opposite of what D-08/D-09 need.
**How to avoid:** Use `bufio.Scanner` + `.Buffer(initialBuf, maxFrameSize)` instead (Don't Hand-Roll #1). Leave `boundedbuffer.Buffer` untouched for its existing stderr-capture role.
**Warning signs:** If a fix "reuses `boundedbuffer`" and the resulting behavior on an oversized frame is silent truncation followed by a (now-corrupted) JSON parse attempt rather than an immediate, named error — that's this pitfall.

### Pitfall 4: `bufio.Scanner`'s default split function swallows EOF differently than today's `ReadBytes`
**What goes wrong:** Today's `bufio.Reader.ReadBytes('\n')` always returns a non-nil error when it can't find the delimiter before EOF — the caller (`readResponseBlocking`) unconditionally treats that as a transport failure. `bufio.Scanner`'s default `ScanLines` split function, however, returns the final non-newline-terminated chunk as a valid token at EOF, and only on the *next* `Scan()` call does it return `false` with `Err() == nil` (a "clean" end, not an error). If the replacement code doesn't explicitly handle "`Scan()` returned false, `Err()` is nil," a dead/crashed server would look like "no more responses" (silently returning zero data) instead of erroring.
**Why it happens:** `ScanLines`/`Scanner` was designed for well-behaved line-oriented text files, where a clean EOF is a normal outcome — not for a JSON-RPC-over-stdio session, where EOF always means "the peer is gone" and must surface as an error.
**How to avoid:** After `!scanner.Scan()`, always check `scanner.Err()`: if it wraps `bufio.ErrTooLong`, that's the D-08/D-09 abort path; if it's `nil`, synthesize a transport error (e.g., wrap `io.ErrUnexpectedEOF`) rather than returning `(nil, nil)`.
**Warning signs:** A test that closes the fake server's stdout mid-session and expects an error, but the client silently hangs waiting for a response that will never come (because the read loop treated `false, nil` as "keep looping" instead of "the peer is gone").

### Pitfall 5: `TrustApprove`'s dead empty-class fallback becomes live if the CLI reuses it without pre-validating
**What goes wrong:** `mcpWriteAdapter.TrustApprove` (`cmd/aura/serve_governance_write.go:96-127`) still contains `if class == "" { class = mcp.TrustTrustedLocal }`. This branch is unreachable via the HTTP path today ONLY because `handleMCPTrust` rejects an empty class with 400 *before* calling `TrustApprove`. If the CLI's `mcp trust` command is unified onto `TrustApprove` (recommended for D-12) without first validating class/reason the same way the HTTP handler does, this dead fallback becomes live again for the CLI, silently defaulting an unspecified class to `trusted_local` — reintroducing a variant of F-038 through the back door.
**Why it happens:** The validation currently lives at the wire boundary (HTTP handler), not in the shared provider method — a classic "guard lives in only one of two callers" trap.
**How to avoid:** Either move the class/reason validation into `TrustApprove` itself (single source of truth for both callers), or have the CLI path call the identical validation helper the HTTP handler uses before invoking `TrustApprove`.
**Warning signs:** A CLI `mcp trust <name>` call that succeeds with no `--class`/`--reason` flags and lands `trusted_local` with a NULL reason in the audit row.

### Pitfall 6: The cited "sequential mount loop... `runtime.go:34`" is the wrong function
**What goes wrong:** CONTEXT.md's D-06 cites `internal/mcp/manager/runtime.go:34` as the sequential mount loop with no timeout. That line is inside `RuntimeServers`, which only builds `ServerConfig` launch descriptors (command/args/env) — it never calls `mcp.Open`/`OpenServer` and spawns nothing. The actual per-server spawn loop (confirmed as F-033's own evidence file in `docs/audit/bug-report.md`) is `cmd/aura/main.go`'s `buildRegistryWithMCP`, which calls the ALREADY-EXISTING `mcptools.MountWithRetry` (with its own 6-attempt capped-exponential-backoff retry policy, `~17s` worst-case today) for each server, sequentially.
**Why it happens:** `runtime.go` and `mount.go`/`main.go` both contain "for name, server := range ..." loops with similar shapes; the CONTEXT.md author likely pattern-matched on the shape rather than tracing the actual spawn call.
**How to avoid:** Plan the D-06/D-07 bounded-mount work against `cmd/aura/main.go:buildRegistryWithMCP` and `internal/agent/mcptools/mount_retry.go:MountWithRetry`, not `runtime.go`.
**Warning signs:** A plan task that edits `manager/runtime.go` expecting to add a mount timeout there will find no spawn call to bound.

### Pitfall 7: Per-transport close is already bounded; the shutdown gap is purely "sequential, no aggregate deadline"
**What goes wrong:** It's tempting to re-derive per-transport timeouts for D-11. Both already exist and are correct: `closeWaitTimeout = 5 * time.Second` (stdio, `client.go:404`) and `httpCloseTimeout = 5 * time.Second` (HTTP, `http_client.go:26`). The actual gap is that `closeMCPServers` (`cmd/aura/main.go:301-309`) runs every closer **sequentially, in reverse order, with no concurrency and no outer deadline** — so N servers cost up to `5×N` seconds serially even though each one individually is well-behaved.
**Why it happens:** The function was written as a simple defer-unwind loop before multi-server shutdown-time scaling was a concern.
**How to avoid:** Fan out all closers concurrently (errgroup or goroutines+channel) under ONE `context.WithTimeout(ctx, AURA_MCP_SHUTDOWN_TIMEOUT)`; do not touch the individual `closeWaitTimeout`/`httpCloseTimeout` constants, they are already correct.
**Warning signs:** A plan that proposes shortening the per-transport timeouts to "make room" for more servers under the aggregate budget — that's solving the wrong layer.

### Pitfall 8 (testing nuance): goleak + "abandon stragglers after the aggregate deadline" can flake
**What goes wrong:** If the D-11 aggregate-timeout implementation "gives up" on any closer goroutine still running past the aggregate deadline (correct behavior — it must not block shutdown further), a test that asserts `goleak.VerifyNone(t)` (or relies on `goleak.VerifyTestMain`) *immediately* after the aggregate deadline fires can flake: the abandoned goroutine is still finishing its own internal bound (e.g., the stdio Close's kill-then-drain-Wait sequence) and hasn't exited yet at the exact instant the assertion runs.
**Why it happens:** "The function returned promptly" and "every spawned goroutine has fully unwound" are different guarantees; conflating them in a single immediate post-call assertion is a common concurrency-test mistake.
**How to avoid:** Either give the test a short settle window before asserting no leaks, or have the test explicitly wait on its own tracked completion signal (a `sync.WaitGroup` the test owns) separately from asserting the shutdown function's own wall-clock bound.
**Warning signs:** An intermittent, hard-to-reproduce `goleak` failure specifically in a shutdown/timeout test, but not in the equivalent happy-path test.

### Pitfall 9: The existing `reconnectingServer` wrapper already provides most of D-09's "never resync" guarantee — don't build a parallel mechanism
**What goes wrong:** `internal/agent/mcptools/bridge_reconnect.go`'s `reconnectingServer` already wraps every mounted **stdio** server (not HTTP — only stdio, via `mount.go`'s `newReconnectingServer(name, cfg, cli)` path) and treats ANY `mcp.IsTransportError` as fatal-to-that-connection: it closes the failed client and transparently spawns a brand-new process + fresh handshake (with exponential backoff and a 3-consecutive-failures circuit breaker opening for 30s), **never attempting to keep using the same desynced pipe**. If the D-08/D-09 implementation additionally builds its own separate "mark unhealthy"/reconnect-tracking state at the raw `mcp.Client` layer, it will duplicate and potentially conflict with this existing mechanism.
**Why it happens:** D-09's phrasing ("abort the whole transport... mark unhealthy") reads like it needs new machinery, but the "abort" half is nearly free once the oversized-frame error correctly satisfies `IsTransportError` — the wrapper already reacts to that.
**How to avoid:** Ensure the new oversized-frame error wraps/satisfies `mcp.ErrTransport` (so `IsTransportError` is true) and is raised as SOON as the cap is exceeded (not after further reads). For the "mark unhealthy" observability requirement, prefer surfacing `reconnectingServer`'s existing `reconnectFailures`/`breakerOpenUntil` state through a new status-query method, rather than inventing a second, parallel health flag at the `Client` level.
**Warning signs:** Two independent places in the code deciding whether a given MCP server is "healthy," which can disagree.

### Pitfall 10: `os/user.Current()` can fail on minimal/containerized hosts — the audit actor must never be empty
**What goes wrong:** `mcp_audit.actor_identity_id` is `NOT NULL`. `os/user.Current()` (needed fresh for D-13, zero existing usage in this repo) can return an error on some minimal container images (missing `/etc/passwd` entry for the running UID when cgo is disabled) or in unusual sandboxed environments.
**Why it happens:** Go's pure-Go fallback for `os/user` on Unix parses `/etc/passwd`/NSS sources, which can be absent or incomplete in stripped container images; this is a well-known, if uncommon, Go stdlib rough edge.
**How to avoid:** Build a small fallback chain: `os/user.Current()` → on error, fall back to `os.Getenv("USER")`/`os.Getenv("USERNAME")` → on total failure, a final literal fallback (e.g., `"cli:unknown"`) so the CLI mutation is NEVER blocked from writing its audit row purely because username resolution failed.
**Warning signs:** A CLI MCP mutation that errors out with an `os/user` failure instead of completing with a degraded-but-non-empty actor string.

### Pitfall 11: The F-046 probe fix targets the wrong function if you follow D-16's literal citation
**What goes wrong:** CONTEXT.md's D-16 says to upgrade `doctorProbeMCPBinary` (`cmd/aura/doctor.go`). That function checks `AURA_MCP_NEO4J_CYPHER_BIN`'s presence via `LookPath` — it is the Neo4j-Cypher MCP sidecar binary check, part of `aura doctor`'s fixed 5-check list (postgres/neo4j/embed/mcp-neo4j-cypher/llm_key), and has **nothing to do with the managed MCP server registry** (`~/.aura/mcp/servers.json`). The actual bug matching F-046's evidence file and MCPH-09's literal text ("a dead HTTP MCP endpoint reports OK=false") is in `cmd/aura/mcp_status.go`'s `writeRuntimeCheck`: for any `streamable_http`/URL-shaped server it unconditionally prints `"%s: http endpoint configured\n"` and explicitly **skips calling `mcp.ProbeServer`** — the stdio branch a few lines below DOES call `ProbeServer` correctly. `mcp.ProbeServer` itself already dials HTTP correctly and is already tested.
**Why it happens:** Two different functions in two different files both have "MCP" and "probe/doctor" in their names/vicinity; a literal reading of D-16 points at the wrong one.
**How to avoid:** Fix `writeRuntimeCheck` (called from `mcpDoctorAll`, itself `aura mcp doctor --all`) to call `mcp.ProbeServer` for HTTP servers too, bounded by `context.WithTimeout(ctx, AURA_MCP_PROBE_TIMEOUT)` (today NEITHER branch of `writeRuntimeCheck` imposes a timeout on the `ctx` it receives — trace confirms `runMCP` → `runMCPCommand(context.Background(), ...)`, i.e., no deadline anywhere in this CLI path today). Separately, `aura mcp status` (`mcpStatus` in the same file) uses `manager.SnapshotStatus`, a config-derived (non-live) view — D-17 requires this surface to ALSO reflect the live probe. If a genuinely new "6th doctor check" is desired for D-18's "`aura doctor` probes only enabled+runnable HTTP servers," that is additive to `cmd/aura/doctor.go`'s `doctorChecks()`, separate from fixing `writeRuntimeCheck`.
**Warning signs:** A plan task titled "upgrade doctorProbeMCPBinary" — that function is out of scope for MCPH-09 as literally described.

### Pitfall 12: The CLI's `mcp trust` command has no `--reason`/`--class` flags today
**What goes wrong:** `cmd/aura/mcp_profile.go`'s `mcpTrust(args []string, out io.Writer) error` takes only `<name>`, hardcodes `Class: mcp.TrustTrustedLocal`, and never populates `Reason`. If this command is routed through the audited writer (D-12/D-13) as-is, every CLI trust action will forever write a `NULL` reason to `mcp_audit.reason` — weaker provenance than the web path, which requires a non-empty reason.
**Why it happens:** The CLI command predates the audit requirement entirely; it was a simple, no-frills trust-approve shortcut.
**How to avoid:** Add `--reason <text>` (required, mirroring the web endpoint's non-empty-reason rule) and optionally `--class <class>` (default `trusted_local` for backward-compatible single-arg usage) to `aura mcp trust`. This is a CLI UX decision within Claude's Discretion, but the *absence* of a reason field is a concrete gap worth flagging rather than silently accepting a NULL-reason CLI audit trail.
**Warning signs:** A shipped `aura mcp trust <name>` that writes an audit row with `reason: NULL` while the web path's equivalent action always has a reason.

## Code Examples

### 1. Classify — see Architecture Patterns, Pattern 1 (full sketch above)

### 2. Reusing the existing per-OS process-group-kill pair (D-10)

```go
// Source: internal/agent/tools/shell_exec_unix.go (VERBATIM existing pattern, already
// shipped and CI-proven in this repo — read for reuse, not for approach only).
//go:build !windows
func setProcessGroup(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
func killProcessGroup(cmd *exec.Cmd) error {
    if cmd.Process == nil { return nil }
    return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
```
```go
// Source: internal/agent/tools/shell_exec_windows.go (VERBATIM existing pattern).
//go:build windows
func setProcessGroup(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
func killProcessGroup(cmd *exec.Cmd) error {
    if cmd.Process == nil { return nil }
    pid := strconv.Itoa(cmd.Process.Pid)
    out, err := exec.Command("taskkill", "/F", "/T", "/PID", pid).CombinedOutput()
    if err == nil || taskkillProcessMissing(out) { return nil }
    return fmt.Errorf("taskkill process group %s: %w", pid, err)
}
```
Recommendation: extract this pair to a small shared package (name is Claude's Discretion, e.g. `internal/procgroup`) so both `internal/agent/tools` and `internal/mcp` import the same implementation rather than duplicating it. In `mcp.Client`, call `setProcessGroup(cmd)` before `cmd.Start()` in `Open`, and replace `killProcess()`'s `cmd.Process.Kill()` body with `killProcessGroup(cmd)`. Do NOT adopt `shell_exec.go`'s `cmd.Cancel = func() error { return killProcessGroup(cmd) }` idiom verbatim for the *mount-timeout* ctx (see Pitfall #2) — that idiom is correct only for the ctx that legitimately owns the process's whole lifetime.

### 3. Bounded stdio frame read (D-08/D-09)

```go
// Illustrative shape — exact error type/naming is Claude's Discretion.
const defaultStdioMaxFrame = 1 << 20 // 1 MiB, tunable via AURA_MCP_STDIO_MAX_FRAME

var ErrStdioFrameTooLarge = fmt.Errorf("%w: stdio frame exceeds cap", ErrTransport)

func (c *Client) readResponseBlocking(want int64) (json.RawMessage, error) {
    for {
        if !c.scanner.Scan() {
            if err := c.scanner.Err(); err != nil {
                if errors.Is(err, bufio.ErrTooLong) {
                    c.abortTransport() // D-09: deterministic teardown, never resync
                    return nil, fmt.Errorf("%w: %v%s", ErrStdioFrameTooLarge, err, c.stderrTail())
                }
                return nil, fmt.Errorf("%w: recv: %w%s", ErrTransport, err, c.stderrTail())
            }
            // Pitfall #4: Scan()==false with Err()==nil is a clean EOF from Scanner's
            // perspective, but for this protocol it always means "peer is gone".
            return nil, fmt.Errorf("%w: recv: %w%s", ErrTransport, io.ErrUnexpectedEOF, c.stderrTail())
        }
        line := c.scanner.Bytes()
        if len(bytes.TrimSpace(line)) == 0 { continue }
        var resp rpcResp
        if err := json.Unmarshal(line, &resp); err != nil {
            return nil, fmt.Errorf("decode response: %w", err)
        }
        if resp.ID == nil || *resp.ID != want { continue }
        if resp.Error != nil {
            return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
        }
        return resp.Result, nil
    }
}
// Construction (in Open): c.scanner = bufio.NewScanner(stdoutPipe)
//   c.scanner.Buffer(make([]byte, 0, 4096), maxFrame) — replaces bufio.NewReader(stdoutPipe).
```

### 4. Legacy-env production gate — exact existing template to mirror (D-14/D-15)

```go
// Source: internal/config/config_validate.go:210-218 (gateDestructiveShell, EXISTING,
// verbatim shape to mirror) — this is the precise, already-proven "prod-only, raw env
// check, Fatal Violation" pattern this codebase already uses for an analogous knob.
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

// NEW — the D-14/D-15 gate, same shape, registered into ValidateProfile's aggregation
// (config_validate.go:85-87, alongside gateRequiredSecrets/gateObjectStoreCreds/etc.):
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
Boot already enforces this for free: `cfg.Validate()` (`config_validate.go:61-76`) aggregates every gate via `ValidateProfile` and returns an error on any `Fatal` violation; `chat_boot.go:227` calls `cfg.Validate()` before any DB work and propagates the error — no new boot-wiring is needed beyond registering the new gate function.

### 5. Reusing the existing TestHelperProcess fake-server idiom for new lifecycle tests

```go
// Source: internal/mcp/client_open_test.go (EXISTING, already reused in 7+ test files
// across this repo — extend its mode dispatch, don't invent a new subprocess-test harness).
func TestHelperProcess(t *testing.T) {
    if os.Getenv("AURA_MCP_HELPER") != "1" { return }
    mode := os.Getenv("AURA_MCP_HELPER_MODE")
    // existing: "", "crash", "hang" — extend with e.g. "oversize" (writes one line
    // larger than AURA_MCP_STDIO_MAX_FRAME with no newline) and "grandchild" (spawns
    // a further child process and writes both PIDs to a file named by an env var
    // BEFORE hanging, so a test can verify the WHOLE tree died after kill).
    ...
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| `bufio.Reader.ReadBytes('\n')` (unbounded growth until newline or EOF) | `bufio.Scanner` + `.Buffer(buf, max)` with `bufio.ErrTooLong` | This phase (D-08) | Deterministic memory ceiling per frame; a misbehaving/hostile MCP server can no longer OOM the host process |
| `cmd.Process.Kill()` (single tracked PID only) | `syscall.Kill(-pid, SIGKILL)` (Unix) / `taskkill /F /T` (Windows) — whole process group/tree | This phase (D-10), already proven in `internal/agent/tools` since an earlier phase | No leaked grandchild processes on MCP shutdown or mount-timeout |
| Sequential, unbounded-total registry mount loop | Per-server bounded handshake (D-06) inside the ALREADY-EXISTING ctx-aware retry wrapper | This phase (D-06), building on `MountWithRetry` shipped earlier | Boot always returns within a predictable window regardless of how many configured servers are unreachable |
| Sequential, unbounded-total registry shutdown loop | Concurrent fan-out under one aggregate deadline (D-11) | This phase | Shutdown wall-clock time stops scaling linearly with server count |
| CLI MCP mutations: direct `mcp.SaveManagedConfig` (no audit) | Routed through `manager.WriteConfigWithAudit` with a `cli:<os-username>` actor (D-12/D-13) | This phase | Full audit parity between CLI and web governance-write surfaces |
| `AURA_MCP_SERVERS_JSON` parsed unconditionally in every profile | Prod-disabled unless `AURA_MCP_LEGACY_ENV_COMPAT=1` (D-14/D-15) | This phase | A `server_production` deploy can no longer silently run an un-governed, unaudited legacy server set |
| MCP HTTP health surfaced as "endpoint configured" (config-only) | Live dial + `tools/list` under a bounded deadline, `OK=false` on failure (D-16..D-18) | This phase | Operators see a dead/typoed HTTP MCP endpoint immediately instead of a false-healthy status |

**Deprecated/outdated:**
- The `normalizedServerType`/`NormalizedTrust` free functions in `managed_config.go` become thin wrappers over (or fully replaced by) `Classify` — their standalone bodies are retired as part of this phase, not merely supplemented.
- `doctorProbeMCPBinary`'s scope is unaffected by this phase (it remains the Neo4j-Cypher sidecar check) — do not repurpose it; it was miscited in CONTEXT.md's D-16 (see Pitfall #11).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | An HTTP-shaped server with an explicit `type` mismatched against a "local-only" trust class (e.g., `type: streamable_http` with `trust.class: trusted_local`) should be rejected by `Classify` as an inconsistent explicit-type declaration. CONTEXT.md's D-02 says "an explicit type disambiguates ONLY when the trust class matches the transport" but does not spell out the exact compatibility matrix. | Architecture Patterns Pattern 1, Open Questions #1 | If the matrix is wrong, the classifier could either be too permissive (re-opening a variant of F-013/F-027) or too strict (breaking a legitimately-configured server, e.g. the `memory` recipe which is `streamable_http` + `trusted_recipe`) |
| A2 | `AURA_MCP_SHUTDOWN_TIMEOUT` is a reasonable, consistent env-var name for D-11's aggregate shutdown deadline. CONTEXT.md pins the ~5s default but does not name an env var for it (unlike D-06/D-08/D-16, which do). | Code Examples, Architecture Patterns | Low risk — purely a naming choice within Claude's Discretion; any consistent `AURA_MCP_*` name works |
| A3 | Registering `AURA_MCP_LEGACY_ENV_COMPAT` in `config_knobs.go`'s `knobRegistry()` is in-scope (it's a `KindBool` read inside `internal/config`, matching the registry's own stated Tier A/B criteria), while `AURA_MCP_MOUNT_TIMEOUT`/`AURA_MCP_STDIO_MAX_FRAME`/`AURA_MCP_PROBE_TIMEOUT` (read inside `internal/mcp`/`internal/mcp/manager`, outside `internal/config`) are Tier C and deliberately excluded per the registry file's own documented scope decision (D-16, Phase 33). | Standard Stack, Recommended Project Structure | Low risk — the registry file's own comments explicitly document this scope boundary; worst case is a planner adds all four knobs to the registry, which is harmless over-inclusion, not a functional bug |
| A4 | Extracting `setProcessGroup`/`killProcessGroup` to a new shared package (rather than duplicating them into `internal/mcp`) is worth the extra indirection, per CLAUDE.md's "never duplicate, extract a helper" rule. | Don't Hand-Roll, Code Example #2 | Low risk — if the planner instead duplicates the ~30 LOC pair, it's a CLAUDE.md-rule violation but not a functional bug; easy to correct in review |

**If this table is empty:** N/A — see entries above. All four assumptions are low-to-medium risk implementation-shape choices, not compliance/security/retention decisions requiring user confirmation before locking.

## Open Questions

1. **What is the exact type↔trust-class compatibility matrix for an explicitly-typed server (D-02's "the trust class matches the transport")?**
   - What we know: `stdio` + {`trusted_recipe`, `trusted_local`, `sandboxed_local`, `blocked`} is clearly valid (today's normal case); `streamable_http` + {`trusted_recipe`, `remote_http`, `blocked`} is clearly valid (today's `memory`/custom-HTTP cases); an empty/unknown trust on `streamable_http` must resolve to `blocked` (Pitfall #1).
   - What's unclear: whether `stdio` + `remote_http` or `streamable_http` + `trusted_local`/`sandboxed_local` should be a hard classifier error (inconsistent explicit declaration) or silently normalized/ignored. No existing test or code path exercises this combination today.
   - Recommendation: treat as a hard `Classify` error (fail loud on an inconsistent explicit declaration, consistent with D-02's fail-closed spirit for ambiguity) unless discuss-phase/planning surfaces a legitimate use case for allowing it.

2. **Should `aura mcp trust`'s CLI-audit unification add `--reason`/`--class` flags, or accept a permanently-NULL/hardcoded audit row for CLI trust actions?**
   - What we know: the web endpoint requires a non-empty reason (D-05, already shipped); the CLI command today has neither flag and hardcodes `trusted_local` (Pitfall #12).
   - What's unclear: whether operator UX parity with the web endpoint is a phase goal, or whether "CLI writes are audited (even minimally)" alone satisfies D-12's literal text.
   - Recommendation: add `--reason` (required) for parity and audit-quality; `--class` optional with the existing default. This is within Claude's Discretion per CONTEXT.md, flagged here only because it's a concrete, easily-overlooked gap.

3. **Does the CLI's DB-pool-wiring for MCPH-07 need to reuse an existing "open a pool for a one-shot CLI command" pattern, or is this genuinely the first CLI subcommand (besides `identity recover`/`identity recover-operator`) to need one?**
   - What we know: `cmd/aura/recovery.go`/`recover_operator.go` already open a `*pgxpool.Pool` for CLI use (`identityRecover(ctx, store, pool, args)`); the MCP CLI commands (`mcp.go`, `mcp_profile.go`) currently take no `ctx`/pool at all.
   - What's unclear: the exact call-chain change needed in `main.go`'s subcommand dispatch (`case "mcp": runMCP(os.Args[2:])`) to thread a pool through only when a mutation subcommand runs (vs. read-only subcommands like `recipes`/`status`/`list`, which have no need for a DB connection).
   - Recommendation: mirror `identityRecover`'s pool-open-close lifecycle, scoped to only the mutating `mcp` subcommands (add/install/trust/enable/disable/remove/profile create/use/add/remove), leaving read-only subcommands pool-free.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| PostgreSQL | MCPH-07's CLI-audit-row `db_integration` test only | Confirmed present in the project's existing dev/CI setup (already required by every other `db_integration`-tagged suite) | per existing project config | None needed — this is the one test genuinely requiring it, matching the project's existing `db_integration` tag convention |
| Docker | Not required anywhere in this phase | N/A | — | N/A — no `docker_integration` tag applies to any MCPH capability; process-tree-kill tests spawn plain OS processes, not containers |
| Neo4j | Not required anywhere in this phase | N/A | — | N/A |

**Missing dependencies with no fallback:** none — this phase's only external dependency (Postgres, for one test) is already a standing requirement of this project's existing `db_integration` CI tier.

**Missing dependencies with fallback:** none applicable.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `go.uber.org/goleak` (already vendored; wired via `TestMain` in `internal/mcp/main_test.go` — verify/add an equivalent `TestMain` in `internal/agent/mcptools` and `cmd/aura` if new goroutine-spawning tests land there) |
| Config file | none — pure Go tests, no external test-framework config |
| Quick run command | `go test ./internal/mcp/... ./internal/mcp/manager/... ./internal/agent/mcptools/... ./internal/config/... -race` (seconds; no DB/Docker needed for anything except the one MCPH-07 integration test, which is excluded by default since it requires the `db_integration` build tag) |
| Full suite command | `bash scripts/coverage_docker.sh` (per CLAUDE.md — provisions the disposable `aura_cov` DB) or `go test $(bash scripts/go_packages.sh) -tags="db_integration" -race` for the one gated suite |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| MCPH-01 | `Classify` rejects mixed `url`+`command` with no explicit type; never reaches stdio `Open` | unit | `go test ./internal/mcp/ -run TestClassify -race` | ❌ Wave 0 — new `classify_test.go` |
| MCPH-01 | All 9 inventoried call sites route through `Classify` (no residual duplicate logic) | unit (regression) | `go test ./internal/mcp/... ./internal/mcp/manager/... ./internal/agent/mcptools/... -race` | ⚠ Partially exists — `managed_config_test.go`/`runtime_test.go` cover the OLD behavior and need deliberate updates, not new files |
| MCPH-02 | Empty/blank trust on a remote resolves to `TrustBlocked`, never `TrustRemoteHTTP` | unit | `go test ./internal/mcp/ -run TestClassify_RemoteEmptyTrust -race` | ❌ Wave 0 — extends the F-013 fix; `TestNormalizedTrustRemoteHTTPInferred` must be rewritten, not just supplemented |
| MCPH-02 | Per-identity override cannot elevate a remote server's trust (D-04) | unit | `go test ./internal/mcp/ -run TestSetTrustForIdentity_RemoteBlocked -race` | ❌ Wave 0 — `managed_config_identity_test.go` exists but has no remote-server fixture yet |
| MCPH-03 | Trust endpoint 400s on empty body/`{}`/blank reason/unknown class, no mutation | unit | `go test ./internal/agui/ -run TestGovernanceWriteTrustRejectsUnderspecified -race` | ✅ **Already exists and passes** — verify only, do not re-implement |
| MCPH-04 | Hung mount drops within `AURA_MCP_MOUNT_TIMEOUT`; helper process reaped; healthy servers unaffected | unit + race | `go test ./internal/agent/mcptools/... ./cmd/aura/... -run TestMount.*Timeout -race` | ❌ Wave 0 — reuses the existing `TestHelperProcess` "hang" mode; new test must also assert a HEALTHY server mounted moments earlier is NOT killed by the same timeout (Pitfall #2 regression guard) |
| MCPH-04 | Registry construction returns within deadline regardless of one hung server | unit | same as above | ❌ Wave 0 |
| MCPH-05 | Oversized stdio frame aborts read without large allocation | unit | `go test ./internal/mcp/ -run TestStdioOversizedFrame -race` | ❌ Wave 0 — extends `client_test.go`'s `fakeServer`/`newTestPair` (pure `io.Pipe`, no process spawn) |
| MCPH-05 | Oversized frame surfaces as `IsTransportError` (feeds MCPH-06's abort path) | unit | same as above | ❌ Wave 0 |
| MCPH-06 | Shutdown terminates the whole stdio process tree (grandchild included) | unit (real subprocess, no Docker) | `go test ./internal/mcp/ -run TestClose.*ProcessTree -race` | ❌ Wave 0 — new `TestHelperProcess` mode that spawns + PID-reports a grandchild |
| MCPH-06 | Aggregate shutdown deadline bounds total wall-clock time regardless of server count | unit + race (goleak-aware, see Pitfall #8) | `go test ./cmd/aura/ -run TestCloseMCPServers_Concurrent -race` | ❌ Wave 0 |
| MCPH-07 | CLI mutation appends exactly one `mcp_audit` row with `ActorIdentityID = "cli:<osuser>"` | **integration** (`db_integration`) | `go test ./cmd/aura/... -tags=db_integration -run TestMCPCLIAudit -race` | ❌ Wave 0 — the ONE db-gated test in this phase |
| MCPH-07 | An unrouted CLI write path is disallowed under `server_production` | unit | `go test ./cmd/aura/... -run TestMCPCLIProdDisallow -race` | ❌ Wave 0 (pure profile-string check, no DB) |
| MCPH-08 | `server_production` + `AURA_MCP_SERVERS_JSON` set + compat flag unset → boot hard-error | unit | `go test ./internal/config/ -run TestGateMCPLegacyEnv -race` | ⚠ `config_validate_test.go` exists with the sibling-gate pattern already established — extend, don't create new infra |
| MCPH-09 | Dead/typoed HTTP MCP endpoint reports `OK=false` via `aura doctor`, `aura mcp status`, governance board | unit (httptest, no real network) | `go test ./internal/mcp/ ./cmd/aura/... ./internal/agui/... -run TestProbe.*Dead -race` | ✅ `probe.go`'s underlying behavior already covered by `probe_test.go`; ❌ new tests needed for the `writeRuntimeCheck`/`mcpStatus`/`doctorChecks` wiring specifically |

### Sampling Rate
- **Per task commit:** the quick run command scoped to the package(s) touched (`go vet`/`go build`/`go test [-race]` per CLAUDE.md's post-edit-validation rule)
- **Per wave merge:** `go test $(bash scripts/go_packages.sh) -race` (untagged) plus the `db_integration`-tagged run for any wave touching MCPH-07
- **Phase gate:** full suite green (`bash scripts/coverage_docker.sh`) before `/gsd-verify-work`, since MCPH-07's audit-row test is the one behavior that only proves itself against a real Postgres instance

### Wave 0 Gaps
- [ ] `internal/mcp/classify_test.go` — covers MCPH-01/MCPH-02 (new file)
- [ ] `internal/mcp/managed_config_identity_test.go` — extend with a class-(a) REMOTE server fixture (today's fixture only has a class-(a) local stdio server + the class-(b) admin-governed remote memory server; D-04's guard needs a class-(a) remote to prove against)
- [ ] `internal/mcp/client_test.go` (or a new `client_frame_test.go`) — oversized-frame abort, covers MCPH-05/MCPH-06 partially
- [ ] `internal/mcp/client_open_test.go` — extend `TestHelperProcess`'s mode dispatch with an "oversize" and a "grandchild" mode
- [ ] `cmd/aura/main_test.go` (or a new file) — bounded-mount-doesn't-kill-healthy-servers regression test (Pitfall #2), concurrent-shutdown-with-aggregate-deadline test
- [ ] `cmd/aura/mcp_audit_integration_test.go` (new, `db_integration` tag) — the ONE MCPH-07 db-gated test
- [ ] `internal/config/config_validate_test.go` — extend with `TestGateMCPLegacyEnv` (sibling-gate pattern already established, low effort)
- [ ] `cmd/aura/mcp_status_test.go` (new or extend `mcp_test.go`) — dead-HTTP-endpoint `OK=false` for `writeRuntimeCheck`/`mcpStatus`
- [ ] Framework install: none — `goleak`/`testing` already present; no new test-framework dependency

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | This phase adds no new authentication surface; the CLI actor (D-13) is attribution, not authentication, and reuses the OS's own login boundary |
| V3 Session Management | No | No session concept introduced |
| V4 Access Control | **Yes** | D-04's remote-trust-elevation guard (a per-identity override must not make a network-facing server runnable) is exactly an access-control boundary between the per-identity overlay and the admin-governed shared catalog |
| V5 Validation, Sanitization & Encoding | **Yes** | The classifier's mixed-transport rejection (D-02) and the trust-endpoint 400 gate (D-05, already shipped) are both input-validation boundaries; the `Classify` function is the canonical place this validation must live |
| V6 Cryptography | No | No new cryptographic material; secrets already flow through the existing `secret.IsSecretEnvKey`/`RedactSecrets` redaction path, untouched by this phase |
| V7 Error Handling & Logging | **Yes** | The append-only `mcp_audit` ledger (D-12/D-13) is precisely a V7 non-repudiation control; the WARN-on-hung-mount/oversized-frame requirements (D-07/D-09) are V7 error-visibility controls |
| V13 API and Web Service | **Yes** | The governance trust HTTP endpoint's strict-decode + 400 behavior (D-05, already shipped) is a V13 control; this phase's job here is verification, not new implementation |
| V14 Configuration | **Yes** | The legacy-env prod-gating (D-14/D-15) is a textbook V14 "secure configuration by default, explicit opt-out required" control, following this project's already-established `RuntimeProfile.Strict()` pattern |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| An operator-supplied (or hand-edited) `servers.json` entry with a bare `url` and no `trust` block auto-becomes runnable (F-013, confirmed live in this codebase — Pitfall #1) | Elevation of Privilege | Fix the `Classify`/trust-normalization fallback to resolve unknown/empty remote trust to `Blocked`, never auto-infer `RemoteHTTP` |
| A per-identity overlay preference silently makes a shared-catalog remote server runnable for that identity without admin approval (D-04) | Elevation of Privilege | The remote-elevation guard in `SetTrustForIdentity`/`MountForIdentity`, keyed on the classifier's `ServerType` |
| A misbehaving or hostile stdio MCP server sends an unterminated/oversized line, growing the reading process's memory without bound (F-034) | Denial of Service | Bounded `bufio.Scanner` read with a hard cap, deterministic abort (D-08/D-09) |
| A hung/unresponsive stdio MCP server blocks boot indefinitely, or blocks shutdown indefinitely (F-033/F-035) | Denial of Service | Bounded per-server mount handshake (D-06) and bounded, concurrent shutdown (D-11) |
| An MCP subprocess spawns grandchildren that survive the tracked PID's death, accumulating orphaned processes over repeated restarts (F-035) | Denial of Service (resource exhaustion) | Process-group/tree kill (D-10) instead of single-PID kill |
| CLI-originated MCP configuration changes leave no audit trail, so a compromised or careless local operator action cannot be attributed or reconstructed after the fact (F-037) | Repudiation | Route every CLI mutation through the same append-only, trigger-enforced `mcp_audit` ledger the web path already uses (D-12/D-13) |
| A `server_production` deployment silently keeps trusting an un-governed, unaudited `AURA_MCP_SERVERS_JSON` server set left over from a `dev` config (F-014) | Tampering / Elevation of Privilege | Fail-closed boot validation unless an explicit compatibility flag is set (D-14/D-15) |
| A dead or typoed HTTP MCP endpoint is reported healthy purely because it is *configured*, masking a real outage from the operator (F-046) | Information Disclosure (false confidence) / availability-adjacent | Live dial + `tools/list` under a bounded deadline, `OK=false` on any failure (D-16..D-18) |

## Sources

### Primary (HIGH confidence — direct code reading in this repository, 2026-07-18)
- `internal/mcp/managed_config.go`, `managed_config_identity.go`, `transport.go`, `client.go`, `probe.go`, `redact.go` — classifier/trust/transport/stdio-client/probe substrate
- `internal/mcp/manager/runtime.go`, `status.go`, `audit.go`, `configwrite.go` — runtime eligibility, status snapshot, audit store, atomic-write-with-audit wrapper
- `internal/agent/mcptools/mount.go`, `mount_retry.go`, `bridge_reconnect.go` — the actual mount call sites, the existing ctx-aware retry policy, the existing reconnect-on-transport-error wrapper
- `cmd/aura/main.go` (`buildRegistryWithMCP`, `closeMCPServers`, `mcpMountRetryPolicy`) — the real boot mount loop and shutdown loop
- `cmd/aura/mcp.go`, `mcp_profile.go`, `mcp_status.go`, `doctor.go` — every CLI MCP subcommand, the F-046 probe-skip bug, the unrelated `doctorProbeMCPBinary` check
- `internal/agui/governance_write_api.go`, `governance_write_seam.go`, `governance_api.go` + `cmd/aura/serve_governance_write.go`, `serve_governance.go` — the already-shipped web governance write/read providers, including the already-passing F-038 fix and test
- `internal/config/config_validate.go`, `config_runtimeprofile.go`, `config_knobs.go`, `config.go` (`Validate`) — the exact boot-time profile-gating call chain and the sibling gate template to mirror
- `internal/boundedbuffer/buffer.go` — confirmed the actual (wrong-fit) semantics of this type
- `internal/agent/tools/shell_exec.go`, `shell_exec_unix.go`, `shell_exec_windows.go` — the already-proven, already-shipped process-group-kill pattern
- `internal/mcp/client_open_test.go`, `client_test.go`, `client_timeout_test.go`, `main_test.go`, `probe_test.go`, `managed_config_test.go`, `managed_config_identity_test.go` — existing test-fixture and test-harness conventions (TestHelperProcess re-exec idiom, io.Pipe fake server, goleak wiring)
- `internal/agui/governance_write_api_test.go` — confirmed `TestGovernanceWriteTrustRejectsUnderspecified` already exists and covers all seven F-038 cases
- `docs/audit/bug-report.md` (F-013, F-014, F-027, F-033, F-034, F-035, F-037, F-038, F-046 entries) — original finding evidence-file citations, cross-checked against current code
- `go.mod`/`go.sum` — confirmed `golang.org/x/sys v0.46.0` (indirect) and `golang.org/x/sync v0.21.0` (direct) are already resolved
- `/d/tmp/LibreChat/packages/api/src/mcp/connection.ts` — confirmed `isStdioOptions`/`isStreamableHTTPOptions` (lines 76, 106), `getMCPStreamableHTTPResponseLimits`/`guardMCPStreamableHTTPResponse` (lines 276, 292), and `withTimeout` usage (line 27 import, line 1893 call site) exist at the cited locations — read for pattern/approach only, not ported

### Secondary (MEDIUM confidence)
- None — every claim in this document was verified directly against this repository's code or its already-vendored dependency manifest; no external web search was performed since the entire phase scope is internal-codebase consolidation plus Go stdlib usage already well-established in this Claude's training knowledge (bufio.Scanner semantics, os/exec.Cmd.Cancel behavior, os/user fallback behavior) and cross-checked against this repo's own Go version (1.26.5) for API availability.

### Tertiary (LOW confidence)
- None.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new dependencies; both reused modules verified present in `go.sum`
- Architecture: HIGH — every call site, function, and behavior cited was read directly from this repository's current code, not inferred from CONTEXT.md's summary alone; three material corrections to CONTEXT.md's own citations were found and verified (F-013's live mechanism, the real mount-loop location, the real F-046 location)
- Pitfalls: HIGH — every pitfall traces to a specific, quoted, verified code behavior or a specific stdlib documented behavior (e.g., `exec.CommandContext`'s kill-on-cancel semantics, `bufio.Scanner`'s EOF handling), not speculation

**Research date:** 2026-07-18
**Valid until:** 2026-08-17 (30 days — this is internal-codebase research with no external library version drift risk; re-verify only if `master` moves significantly on `internal/mcp`/`internal/agui`/`cmd/aura` before planning executes)
