# Phase 38: MCP Governance Hardening - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-18
**Phase:** 38-mcp-governance-hardening
**Areas discussed:** Legacy env disposition, CLI mutation posture, Hung-mount behavior, Dead-endpoint probe surface, Classifier home/shape, Per-identity vs shared trust, Compat-off + env-set, Frame/probe defaults, CLI-write audit actor, Oversized-frame abort scope, Doctor probe scope, Registry shutdown budget

The WHAT is locked by MCPH-01..09 (spec-like). Discussion covered only the 12 HOW/behavioral forks; the user requested industrial-pattern research (D:\tmp reference repos + online) and confirmed the MCP implementation itself is in scope to modify.

---

## Legacy env disposition (MCPH-08 / F-014)

| Option | Description | Selected |
|--------|-------------|----------|
| Prod-disable + dev compat flag | Prod ignores env unless AURA_MCP_LEGACY_ENV_COMPAT=1; dev unchanged | ✓ |
| Auto-translate to managed config | Parse legacy JSON → managed entries w/ Trust=blocked + audit at boot | |
| Hard-remove entirely | Delete the legacy path; managed config only | |

**User's choice:** Prod-disable + dev compat flag
**Notes:** Matches the phase no-op-under-dev principle with least churn.

## CLI mutation posture (MCPH-07 / F-037)

| Option | Description | Selected |
|--------|-------------|----------|
| Audited + allowed in prod | All mutations through audited writer (mcp_audit); usable in prod; un-routed paths marked unaudited + prod-blocked | ✓ |
| Web-governance-only in prod | CLI mutations error under prod; web is sole write path | |
| Audited-only, no prod gate | Audited writer, no production distinction | |

**User's choice:** Audited + allowed in prod

## Hung-mount behavior (MCPH-04 / F-033)

| Option | Description | Selected |
|--------|-------------|----------|
| Surface + configurable, ~10s default | Drop + WARN + unhealthy in status/board; AURA_MCP_MOUNT_TIMEOUT default 10s | ✓ |
| Surface, fixed 10s | Same visibility, hard-coded 10s | |
| Silent drop | Degrade-and-continue, debug log only | |

**User's choice:** Surface + configurable, ~10s default

## Dead-endpoint probe surface (MCPH-09 / F-046)

| Option | Description | Selected |
|--------|-------------|----------|
| Extend doctor + mcp status + board | One live HTTP dial+tools/list probe reused across three surfaces | ✓ |
| New `aura mcp doctor` command | Dedicated CLI command | |
| Governance board only | Web-only OK=false | |

**User's choice:** Extend doctor + mcp status + board

## Classifier home + shape (MCPH-01 / QUAL-03)

| Option | Description | Selected |
|--------|-------------|----------|
| New classify.go, one Classify() | New file; validation+trust-norm+open+mount all call it; normalizedServerType+NormalizedTrust collapse in | ✓ |
| Fold into managed_config.go | Unify in-place, no new file | |
| Classifier + separate trust resolver | Two funcs, shared validation core | |

**User's choice:** New classify.go, one Classify()
**Notes:** managed_config.go already ~330 LOC; new file keeps it under the 600 cap and makes the unify explicit. Backed by LibreChat's single type-guard module.

## Per-identity vs shared remote trust (MCPH-02, security)

| Option | Description | Selected |
|--------|-------------|----------|
| Effective-config check; identity can NOT elevate remotes | Rules run on merged effective config; per-identity can enable/adjust local trust but a remote runnable class requires the admin shared catalog | ✓ |
| Effective check; identity MAY elevate remotes | Keep today's behavior (per-identity trust can make a remote runnable) | |
| Shared-layer only | Runnable decided pre-overlay; per-identity trust ignored for runnable | |

**User's choice:** Effective-config check; identity can NOT elevate remotes
**Notes:** Fail-closed for the network-facing transport. Constrains the current MountForIdentity path.

## Compat-off + env-set behavior (MCPH-08 fail-closed)

| Option | Description | Selected |
|--------|-------------|----------|
| Hard-error at boot | serve/exec refuses to start, message names env + compat flag | ✓ |
| Ignore + WARN log | Boot proceeds, env ignored, one WARN | |
| Ignore, no log | Silent drop | |

**User's choice:** Hard-error at boot
**Notes:** Fail-loud so a misconfigured prod deploy can't silently drop servers.

## Frame/probe defaults

| Option | Description | Selected |
|--------|-------------|----------|
| 1 MiB frame + 5s probe, both env-knobbed | AURA_MCP_STDIO_MAX_FRAME 1 MiB; AURA_MCP_PROBE_TIMEOUT 5s | ✓ |
| 4 MiB frame + 3s probe, env-knobbed | More payload headroom, tighter probe | |
| 1 MiB / 5s, fixed (no knobs) | Same values hard-coded | |

**User's choice:** 1 MiB frame + 5s probe, both env-knobbed

## CLI-write audit actor (MCPH-07)

| Option | Description | Selected |
|--------|-------------|----------|
| OS user + 'cli' source marker | actor_identity_id = cli:<os-username>; no flag, attributable, distinguishable from web | ✓ |
| Fixed 'system/cli' principal | One constant actor for all CLI writes | |
| Require explicit --identity/env or refuse | CLI write must name the acting identity | |

**User's choice:** OS user + 'cli' source marker
**Notes:** mcp_audit.actor_identity_id is NOT NULL; CLI has no web session, so a cli-namespaced OS-derived principal fills it.

## Oversized-frame abort scope (MCPH-05)

| Option | Description | Selected |
|--------|-------------|----------|
| Tear down the server transport | Over-cap frame aborts connection deterministically; server dropped/unhealthy | ✓ |
| Fail the call, resync stream | Discard frame, fail one call, keep server | |

**User's choice:** Tear down the server transport
**Notes:** A desynced stream is never trusted. Backed by LibreChat's guard-and-teardown.

## Doctor probe scope (MCPH-09)

| Option | Description | Selected |
|--------|-------------|----------|
| Enabled + runnable only | Probe only enabled+runnable HTTP servers; skip disabled/blocked | ✓ |
| Runnable by default, --all opt-in | Default runnable-only; --all probes everything | |
| All configured HTTP servers | Always probe everything incl. disabled/blocked | |

**User's choice:** Enabled + runnable only
**Notes:** Keeps doctor fast + meaningful in CI/health.

## Registry shutdown budget (MCPH-06)

| Option | Description | Selected |
|--------|-------------|----------|
| Parallel close, one aggregate deadline | Concurrent close under single ~5s global budget; each stdio tree killed, HTTP close bounded | ✓ |
| Sequential per-server timeout | One at a time, worst-case N × timeout | |

**User's choice:** Parallel close, one aggregate deadline

---

## Claude's Discretion

Remaining choices delegated to planner/executor: exact error strings, which file each helper lands in, test-fixture shapes, per-OS `SysProcAttr` wiring (Setpgid/Job Object), internal shape of the bounded stdio reader. Sane defaults pinned above.

## Deferred Ideas

None — discussion stayed within phase scope. SSRF profile-binding is Phase 33 (PROF-01/PROF-04, noted in transport.go); docker-gateway runtime lifecycle stays daemon-gated under docker_integration.
