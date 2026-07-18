---
phase: 38
slug: mcp-governance-hardening
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on (high)
threats_open: 0
asvs_level: 1
created: 2026-07-18
---

# Phase 38 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> MCP governance hardening — trust classification, bounded lifetimes, audited mutations.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| operator servers.json / recipe → mcp runtime | A hand-edited or recipe-installed server entry crosses into the trust/transport decision; an untrusted shape must not become runnable. | server config (command, url, trust class) |
| config → stdio subprocess spawn | A mis-typed/ambiguous entry must not be dispatched to a stdio `Open` (arbitrary local command execution). | executable command + args |
| stdio MCP server → host process memory | A hostile/misbehaving server writes an unterminated/oversized line; the reader must not grow memory without bound. | JSON-RPC stdout frames |
| MCP subprocess tree → host process table | A spawned helper may fork grandchildren that outlive the tracked PID; shutdown must reap the whole tree. | OS processes |
| dev-leftover env → production boot | A `dev`-style `AURA_MCP_SERVERS_JSON` carried into prod would silently run un-governed servers; boot must refuse it unless explicitly opted-in. | env var |
| per-identity overlay → admin shared catalog | A per-identity preference must not cross the admin-governance boundary to make a network-facing server runnable. | identity trust overlay |
| hung/unresponsive helper → boot/shutdown liveness | A single hung stdio server must not block boot or shutdown indefinitely. | handshake / close timing |
| configured HTTP endpoint → operator health view | A dead/typoed endpoint must not be reported healthy purely because it is configured (false confidence). | probe verdict |
| local operator CLI → MCP config + audit ledger | A CLI mutation must be attributable (`cli:<user>`) and append-only audited, or disallowed under production. | config mutation + audit row |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-38-01 | Elevation of Privilege | `NormalizedTrust` remote auto-promote (F-013) | high | mitigate | `internal/mcp/classify.go`: streamable_http→TrustRemoteHTTP fallback deleted; unknown/empty remote trust → `TrustBlocked`. Test: `TestClassify_RemoteEmptyTrust` (classify_test.go:206). | closed |
| T-38-02 | Tampering / Validation (V5) | `normalizedServerType` mixed-entry silent stdio (F-027) | high | mitigate | `Classify` rejects mixed url+command before dispatch; `OpenServer` surfaces the error instead of opening stdio. Test: classify_test.go:23 mixed-entry case. | closed |
| T-38-05 | Denial of Service (memory) | `readResponseBlocking` unbounded read (F-034) | medium | mitigate | `internal/mcp/client.go`: `bufio.Scanner` + 1 MiB cap (`defaultStdioMaxFrame`, `AURA_MCP_STDIO_MAX_FRAME`) → deterministic `ErrStdioFrameTooLarge` + transport abort. | closed |
| T-38-06 | Denial of Service (resource) | `killProcess` single-PID kill (F-035) | medium | mitigate | `internal/procgroup` (unix Setpgid+Kill(-pid) / windows taskkill /F /T) reaps the whole subprocess tree. Regression: procgroup_unix_test / procgroup_windows_test. | closed |
| T-38-09 | Tampering / Elevation of Privilege | legacy `AURA_MCP_SERVERS_JSON` under prod (F-014) | high | mitigate | Prod fail-closed gate `gateMCPLegacyEnv` (`cmd/aura`) unless `AURA_MCP_LEGACY_ENV_COMPAT=1`. Test: `TestGateMCPLegacyEnv`. | closed |
| T-38-04 | Elevation of Privilege (V4) | per-identity overlay elevates a remote (D-04) | high | mitigate | `MountForIdentity` + `mutateIdentityPref` classify the server and refuse remote trust elevation (skip in merge, sentinel error on write). | closed |
| T-38-01b | Elevation of Privilege | duplicated F-013 auto-promote in `manager/runtime.go` | high | mitigate | `normalizedTrustForServer` migrated onto canonical `Classify`; duplicate auto-promote branch deleted (runtime.go:60/88/166). | closed |
| T-38-07 | Denial of Service | hung mount blocks boot (F-033) | medium | mitigate | Bounded per-server handshake ctx (`AURA_MCP_MOUNT_TIMEOUT`) distinct from process ctx; reap+drop; WARN-and-continue. **LIVE-PROVEN** (container: hung server dropped at exact 3s, subprocess reaped, healthy servers survive). | closed |
| T-38-08 | Denial of Service | sequential unbounded shutdown (F-035 shutdown) | medium | mitigate | Concurrent errgroup fan-out under one `AURA_MCP_SHUTDOWN_TIMEOUT` aggregate deadline. **LIVE-PROVEN** (container: straggler abandoned at 1s WARN, rc=0). | closed |
| T-38-07b | Denial of Service (self-inflicted) | Pitfall #2 healthy-server-kill / openReplacement | medium | mitigate | Two-context split (process vs handshake) in `Open` + mount chain; `bridge_reconnect` openReplacement de-risked. | closed |
| T-38-10 | Information Disclosure (false confidence) | `writeRuntimeCheck` skip-probe for HTTP (F-046) | medium | mitigate | Early-return deleted; `mcp.ProbeServer` for HTTP bounded by `AURA_MCP_PROBE_TIMEOUT`; dead endpoint → OK=false (mcp_status.go). | closed |
| T-38-10b | Denial of Service (probe) | unbounded / goroutine-leaking probe | medium | mitigate | Per-server `context.WithTimeout` + isolation; goleak-clean. **LIVE-PROVEN** (container: hung managed server → deterministic fail, no leaked dial). | closed |
| T-38-11 | Repudiation (V7) | unaudited CLI mutation (F-037) | high | mitigate | Every mutation routed through `WriteConfigWithAudit` with a `cli:<os-username>` actor (`cmd/aura/mcp_audit_actor.go`); append-only `mcp_audit`; unroutable writes prod-disallowed. Test: db_integration audit-row. | closed |
| T-38-12 | Validation (V13/V5) | TrustApprove dead-fallback back door (F-038, Pitfall #5) | high | mitigate | Empty-class default removed; validation centralized in `TrustApprove`; web 400 gate kept. Test: `TestGovernanceWriteTrustRejectsUnderspecified` + CLI no-reason rejection. | closed |
| T-38-13 | Repudiation (weak provenance) | CLI trust NULL-reason (Pitfall #12) | medium | mitigate | Required `--reason` flag on `aura mcp trust`; the audit row always carries a non-null reason. Test: db_integration reason assertion. | closed |

*Status: open · closed · open-below-threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above `high` count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| R-38-SC | T-38-SC (all 7 plans) | Supply chain — no new external packages introduced by Phase 38. All work reuses stdlib (`os/exec`, `syscall`, `os/user`, `context`, `net/http/httptest`) + already-vendored deps (`golang.org/x/sync/errgroup`, `github.com/jackc/pgx/v5` + sqlc). RESEARCH Package Legitimacy Audit found no npm/pip/cargo install and no `[ASSUMED]`/`[SUS]`/`[SLOP]` packages, so no legitimacy checkpoint is required. | Phase 38 secure-phase | 2026-07-18 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-07-18 | 15 (+1 accepted supply-chain) | 15 | 0 | secure-phase (State B, ASVS L1, register authored at plan time; all mitigations L1-verified present in implementation, three timeout mitigations LIVE-PROVEN in the rebuilt aura container) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-07-18
