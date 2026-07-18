# Phase 38: MCP Governance Hardening - Context

**Gathered:** 2026-07-18
**Status:** Ready for planning

<domain>
## Phase Boundary

One canonical transport classifier + explicit remote trust + bounded MCP lifecycle + audited CLI writes. Closes industrial-audit findings F-013/014/027/033/034/035/037/038/046 (+ QUAL-03 trust-norm unify), satisfying requirements **MCPH-01..09**.

The **WHAT is locked** by MCPH-01..09 in `REQUIREMENTS.md` (they read as a spec): reject mixed `url`+`command`, block remotes on empty trust, 400 on empty/blank/unknown trust body, bounded per-server mount timeout, stdio frame cap, process-tree kill on shutdown, CLI mutations audited-or-prod-disallowed, legacy env prod-disabled, live HTTP probe reports `OK=false`. This discussion only locks the **HOW** where a requirement or operator experience genuinely forked.

**Phase principle (inherited from v2.0.0 §Goal):** every hardening behavior is a **no-op under `dev`/`local_trusted`**; hardening activates under `server_production`. The operator's daily full-host experience is unchanged.

</domain>

<decisions>
## Implementation Decisions

### Canonical transport classifier (MCPH-01, QUAL-03)
- **D-01:** Introduce **one** canonical classifier in a new file `internal/mcp/classify.go` exporting `Classify(ManagedServer) → (ServerType, TrustClass, error)`. `validateManagedServers`, `NormalizedTrust`, `OpenServer`, and the manager mount path all call it. The existing `normalizedServerType` + `NormalizedTrust` collapse INTO it — a true single source of truth. Rationale: `managed_config.go` is already ~330 LOC (near the 600 cap); a new file keeps it under budget and makes the unify explicit. Industrial backing: LibreChat models transport selection as a single set of discriminated-union type guards (`isStdioOptions`/`isStreamableHTTPOptions`), not scattered checks.
- **D-02:** A **mixed `url`+`command` entry with no explicit `type`** is **rejected** by the classifier (error), never silently resolved. It must never reach stdio `Open`. (Today `normalizedServerType` silently resolves ambiguity to stdio — that is the F-027 bug being closed.) An explicit `type` disambiguates ONLY when the trust class matches the transport.

### Remote trust (MCPH-02, MCPH-03)
- **D-03:** Empty/blank trust on a remote (`streamable_http`/URL) entry means **BLOCKED, not runnable**. Explicit trust is required for every runnable remote transport.
- **D-04 (security):** The blocked-on-empty + mixed-transport rules evaluate on the **merged effective config** (what `MountForIdentity` actually mounts). A per-identity override may toggle `enable` and adjust *local* trust, but **a per-identity override may NOT elevate a REMOTE (`streamable_http`) server to a runnable trust class** — making a remote runnable requires the **admin shared catalog**. Fail-closed for the network-facing transport. (Note: `MountForIdentity` in `managed_config_identity.go:150` currently *does* let a per-identity pref set trust on class-(a) servers; this decision constrains that for remotes.)
- **D-05:** The governance trust endpoint returns **400 with no config/audit change** on empty body, `{}`, blank reason, or unknown class. A known class + non-empty reason is required.

### Bounded lifecycle (MCPH-04, MCPH-05, MCPH-06)
- **D-06 (mount timeout):** Each MCP mount runs under a **bounded per-server timeout, default ~10s, tunable via `AURA_MCP_MOUNT_TIMEOUT`**. On timeout the helper process is reaped and the server is **dropped (degrade-and-continue)** — registry construction returns within the deadline. `manager/runtime.go:34` currently mounts sequentially (`for name, server := range servers`) with no timeout — greenfield.
- **D-07 (hung-mount visibility):** A dropped/hung server is **surfaced**: WARN log + marked **unhealthy in `aura mcp status` and the governance health board**. Not a silent drop — the operator can see why a server is missing.
- **D-08 (stdio frame cap):** Stdio frames are capped at **1 MiB, tunable via `AURA_MCP_STDIO_MAX_FRAME`**. `client.go:352` currently reads frames via `bufio.Reader.ReadBytes('\n')` — unbounded growth until newline (the F-034 large-alloc bug). Replace with a bounded read; the `boundedbuffer` package is already imported in `client.go` (reusable).
- **D-09 (oversized-frame abort scope):** An over-cap frame **aborts the whole server transport deterministically** (tear down + mark unhealthy), NOT fail-one-call-and-resync. A desynced request/response stream is never trusted for the next call. Industrial backing: LibreChat's `guardMCPStreamableHTTPResponse` tears the transport down on a limit breach.
- **D-10 (process-tree kill):** Shutdown terminates the **stdio process tree** — Linux `Setpgid` process group, Windows Job Object — build-tagged per-OS. `client.go:462` `killProcess()` currently calls `cmd.Process.Kill()` (immediate PID only → leaked grandchildren, F-035).
- **D-11 (shutdown budget):** Registry shutdown closes all servers **concurrently under a single aggregate deadline (~5s)**; each stdio tree is killed and each HTTP close is bounded (~5s). Total shutdown stays bounded regardless of server count (not sequential N × timeout).

### Audited CLI writes (MCPH-07)
- **D-12:** All CLI MCP mutations (add/trust/enable/disable/remove, profiles) route through the **manager audited atomic writer** and append `mcp_audit` (table exists via migration `0022`), and are **allowed under `server_production`** with a full audit trail. Any write path not yet routed through the audited writer is **explicitly marked unaudited and disallowed under production** (the requirement's literal OR).
- **D-13 (audit actor):** A CLI write has no web principal, but `mcp_audit.actor_identity_id` is `NOT NULL`. Fill it with a **`cli`-namespaced principal derived from the OS user** (e.g. `cli:<os-username>`) — no extra flag, attributable to the machine operator, distinguishable from web writes. Reason still required on `trust`.

### Legacy env (MCPH-08)
- **D-14:** Under `server_production`, `AURA_MCP_SERVERS_JSON` is **prod-disabled unless `AURA_MCP_LEGACY_ENV_COMPAT=1`** is explicitly set; `dev`/`local_trusted` keep parsing it unchanged (`config_mcp.go` today). No-op under dev, hardens under prod.
- **D-15 (fail-closed):** Under `server_production` with the env **set** but the compat flag **unset**, serve/exec **hard-errors at boot** with a clear message naming the env var + the compat flag. A misconfigured prod deploy fails loudly instead of silently running without the operator's intended servers.

### Live HTTP probe (MCPH-09)
- **D-16:** Upgrade the MCP probe from **binary-presence** (`doctorProbeMCPBinary` in `cmd/aura/doctor.go` today) to a **live HTTP dial + `tools/list`** under a short deadline (**~5s, tunable via `AURA_MCP_PROBE_TIMEOUT`**). A dead/typoed endpoint reports **`OK=false`**, not healthy-by-config.
- **D-17:** **One** probe implementation is reused across **three surfaces**: `aura doctor`, `aura mcp status`, and the governance health board.
- **D-18 (probe scope):** `aura doctor` live-probes **only enabled + runnable HTTP servers** (skip disabled/blocked). Keeps doctor fast + meaningful in CI/health; does not dial servers the operator turned off.

### Claude's Discretion
Remaining choices are planner/executor-technical and were explicitly delegated: exact error strings, which file each helper lands in, test-fixture shapes, the precise per-OS `SysProcAttr` wiring, and the internal shape of the bounded stdio reader. Sane defaults already pinned above (1 MiB frame, 5s probe, 10s mount, 5s shutdown).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` §MCPH (lines ~110-118) — MCPH-01..09, the locked WHAT for this phase.
- `.planning/ROADMAP.md` §"Phase 38: MCP Governance Hardening" (line ~658) — goal + 4 success criteria.
- `prd.md` — truth-source (PRD-first principle); MCP governance + trust model + runtime profiles.

### Findings closed (2026-06-21 industrial audit)
- F-027 (classifier/trust-norm), F-013 (empty-remote-trust), F-038 (trust endpoint 400), F-033 (mount timeout/reap), F-034 (stdio frame cap), F-035 (shutdown process tree), F-037 (CLI audit), F-014 (legacy env), F-046 (dead-endpoint probe). Audit source: `docs/audit/` + `.planning/research/SUMMARY.md`.
- Quality audit (QUAL-03 trust-norm unify): `docs/audit/quality/` (README.md index).

### Code the classifier/lifecycle unify touches
- `internal/mcp/managed_config.go` — `normalizedServerType`, `NormalizedTrust`, `validateManagedServers`, trust-class + server-type enums (collapse into `classify.go`).
- `internal/mcp/managed_config_identity.go` §`MountForIdentity` (line 150) — per-identity effective-config merge (D-04 constraint point).
- `internal/mcp/transport.go` — `OpenServer` dispatch (must call the canonical classifier).
- `internal/mcp/client.go` — stdio read (`:352`) + `killProcess` (`:462`) + `boundedbuffer` import (D-08/D-09/D-10).
- `internal/mcp/manager/runtime.go` — sequential mount loop (`:34`) → per-server timeout + drop (D-06/D-07).
- `internal/mcp/manager/audit.go` + `configwrite.go` — audited atomic writer for CLI (D-12/D-13).
- `internal/mcp/probe.go` + `cmd/aura/doctor.go` (`doctorProbeMCPBinary`) + `cmd/aura/mcp_status.go` — live probe (D-16/D-17/D-18).
- `internal/config/config_mcp.go` — legacy `AURA_MCP_SERVERS_JSON` parsing (D-14/D-15).
- `internal/db/migrations/0022_mcp_audit.up.sql` — append-only `mcp_audit` schema (actor/action/server_name/reason).

### Industrial reference patterns (D:\tmp, read for approach only — not Aura code)
- `/d/tmp/LibreChat/packages/api/src/mcp/connection.ts` — discriminated-union transport type guards (`isStdioOptions`/`isStreamableHTTPOptions`), `getMCPStreamableHTTPResponseLimits` + `guardMCPStreamableHTTPResponse` (bounded-read + teardown-on-limit), `withTimeout` per-server init.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/boundedbuffer` — already imported in `client.go`; the bounded stdio-frame reader (D-08) should build on it, not `bufio.ReadBytes`.
- `internal/mcp/manager/audit.go` `MCPAuditStore` / `MCPAuditInsert{ActorIdentityID, Action, ServerName, Reason}` + migration `0022` append-only table — the audited-write substrate for CLI (D-12/D-13); reuse verbatim, add the `cli:` actor.
- `cmd/aura/doctor.go` `doctorCheck`/`doctorProbe` framework — extend with a live MCP HTTP probe rather than a new command (D-16/D-17).
- Existing trust enums (`TrustTrustedRecipe/TrustedLocal/SandboxedLocal/RemoteHTTP/Blocked`) + `IsKnownTrust` — the classifier reuses these; no new trust vocabulary.

### Established Patterns
- **No-op-under-dev / harden-under-prod** (v2.0.0 profile gating): `AURA_MCP_SSRF_ENFORCE` in `transport.go` is the template — prod-gated knob, default OFF, dev byte-identical. D-14/D-15 follow it.
- **Append-only audit inside `db.WithTx`** (0022 header): the mutation + audit row commit atomically. CLI writes must preserve this.
- **Per-identity overlay never mutates the shared catalog** (`MountForIdentity` value-copy): D-04 keeps this invariant and adds the remote-elevation guard.

### Integration Points
- Classifier → validation (`managed_config.go`), trust-norm (`NormalizedTrust`), open (`transport.go`/`OpenServer`), mount (`manager/runtime.go`).
- Live probe → `aura doctor`, `aura mcp status`, governance health board (one probe, three surfaces).
- CLI writers (`cmd/aura/mcp*.go`) → manager audited atomic writer → `mcp_audit`.

</code_context>

<specifics>
## Specific Ideas

- User explicitly wants the **MCP implementation itself modified**, not only governance wrappers — `client.go` stdio read/kill and `manager/runtime.go` mount loop are in scope.
- User asked to mine **industrial patterns** (D:\tmp reference repos + online). LibreChat's MCP subsystem (`packages/api/src/mcp/`) is the closest mature analog and is cited above; its discriminated-union classifier + bounded-read-then-teardown + per-server `withTimeout` directly informed D-01, D-06, D-08, D-09.
- Defaults pinned by the user: **1 MiB** stdio frame, **5s** probe deadline, **~10s** mount timeout, **~5s** aggregate shutdown — all env-knobbed.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope. (SSRF enforcement binding to the runtime profile is Phase 33 PROF-01/PROF-04, already noted in `transport.go`; docker-gateway runtime lifecycle is exercised only under `docker_integration` and remains daemon-gated.)

</deferred>

---

*Phase: 38-mcp-governance-hardening*
*Context gathered: 2026-07-18*
