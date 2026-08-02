# ADR 0044 — Deployment profiles

- **Status:** Accepted
- **Date:** 2026-07-31
- **Requirement:** OPS-06 / F-026
- **Relates to:** ADR 0037 and `prd.md` Amendments #96–#106.3

## Context

Aura serves a trusted local workstation and hardened multi-user appliances. Implicit environment
differences previously left security knobs configured but unreachable in Compose.

## Decision

`dev` and `local_trusted` preserve the operator's direct-host workflow. `single_user_hardened` and
`server_production` activate strict validation, authenticated ownership, ToolGateway enforcement,
per-identity sandbox routing, egress containment, managed MCP governance, durable mutation ledger,
readiness dependencies, and production secret checks. Compose forwards every effective profile
knob explicitly; validation reports the effective behavior, not merely environment strings.

The current appliance is single-node Docker Compose. ADR 0037 owns the Docker sandbox boundary and
reserves Kubernetes/agent-sandbox/gVisor-default for the DGX/multi-node tier. DR covers Postgres,
the sidecars and the object store; the memory plane has **no** rehearsed restore — its per-identity
databases are not dumped, so the volume must be snapshotted out of band until that gap closes
(`scripts/restore_drill.sh`). Release artifacts use immutable source/action/tool references and an SBOM.

## Consequences

- A strict profile that cannot construct its containment boundary fails closed.
- Production deployment requires the release-readiness gate, rollback plan, and drilled evidence.
- Profile-specific bypasses must be explicit, tested, and documented as accepted risk.
- A new deployment class or softened strict invariant requires a new ADR.
