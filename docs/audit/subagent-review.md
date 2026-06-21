# Subagent Review Update

Audit update date: 2026-06-21

Mode: read-only delegated module review. No production source code was modified.

## Delegation Summary

Six subagents were spawned successfully:

| Agent | Scope | Result |
|---|---|---|
| Planck | Agent loop, runner lifecycle, pause/resume, dispatch | 6 findings, including 2 P1 confirmations and 3 new lifecycle deltas |
| Dirac | Shell/filesystem/tools/action execution security | 10 findings, including host-boundary confirmations and background-shell ownership delta |
| Erdos | Persistence, memory, conversations, sidecars, DB state | 7 findings, including sidecar and resume confirmations plus storage lifecycle deltas |
| Ptolemy | MCP, plugins, managed server governance | 9 findings, including new P1 mixed-transport trust bypass |
| Godel | AG-UI/Web/API/auth/governance/frontend wiring | 5 findings, including new P1 identity-scoping issue |
| Socrates | Infrastructure, config, deployment, observability, operations | 6 findings, including static-secret and healthcheck confirmations plus scheduler/backup deltas |

A seventh testing/CI/reference explorer was attempted but could not be spawned because the current agent-thread limit was reached. That slice was reviewed locally with `rg`/PowerShell against tests, Makefile, workflows, and scripts.

## Net Audit Changes

The audit moved from 26 to 51 unique findings:

- P0: 0
- P1: 10
- P2: 28
- P3: 13

Production readiness score changed from 5.2/10 to 4.6/10 because the subagent pass found two additional P1 production blockers and several cross-cutting P2 lifecycle/security issues.

## New P1 Findings

1. Mixed `url` + `command` managed MCP entries can bypass local-command trust blocking.
2. Multi-user AG-UI/Web APIs are not consistently scoped to authenticated identity.

## Important New P2 Findings

- Single resume claim and answer append are not atomic.
- Pause flush failure is hidden after a consumer stops on pause.
- Mutating panic path loses mutating classification.
- Background shell IDs are predictable and not session-bound.
- MCP boot mount lacks per-server timeout.
- Stdio MCP response frames are uncapped.
- MCP shutdown can hang or leave child processes.
- Docker MCP network allowlist is advisory, not enforced.
- CLI MCP mutations bypass the audited atomic writer.
- MCP trust endpoint accepts empty body and defaults to `trusted_local`.
- Conversation delete can bypass in-memory session eviction.
- Crash-created sidecars inside live conversation directories are not reclaimed.
- Relative `AURA_RUN_DIR` can make sidecars cwd-dependent and unreadable.
- Scheduler jobs can cancel immediately on SIGTERM despite drain comments.
- Stop budgets are inconsistent with backup runtime.

## Testing/CI Local Review Update

The first audit under-emphasized the amount of quality infrastructure already present. The repository has extensive Go tests, race lanes, frontend Vitest/Playwright/Stryker scripts, CodeQL, coverage gates, and smoke/chaos scripts. The remaining quality concern is more precise:

- Some CI lanes still use raw `./...` rather than `scripts/go_packages.sh`.
- Agent safety, load, chaos, prompt-injection, and production-profile validations are not yet a single mandatory release gate.
- Workflow/tool references should be pinned more strictly for release supply-chain hardening.

## Source-Only Limitations

- Subagents did not run tests, exploit payloads, Docker stacks, or live services.
- Several lifecycle findings are inferred from source control flow and should get targeted regression tests.
- Existing worktree changes outside `docs/audit` were treated as user work and were not modified.

