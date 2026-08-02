# Decisions

## ADR 0037 — Container-per-identity over Docker for the per-user full-capability sandbox
- source: D:\Aura\docs\adr\0037-per-identity-docker-sandbox.md
- status: locked
- decision: On the mini-PC appliance, Aura runs a container-per-identity sandbox directly over the Docker Engine API (the `moby/moby/client` SDK) — not Kubernetes, and not an `agent-sandbox` warm-pool `SandboxClaim`. K8s + `agent-sandbox` + gVisor-as-default are reserved for the DGX-Spark multi-node tier.
- scope: per-identity sandbox, Docker Engine API, DockerBackend, gVisor, DGX-Spark

## ADR 0038 — Graph-store license posture: Neo4j Community (GPLv3) now, ArcadeDB (Apache-2.0) as the appliance-distribution fallback
- source: D:\Aura\docs\adr\0038-graph-store-license-neo4j-gplv3-vs-arcadedb-apache.md
- status: locked
- decision: Stay on Neo4j Community (GPLv3) for the current single-node posture. Treat GPLv3 conveyance as a solvable compliance task, not a blocker, if and when Aura ships a distributed appliance. ArcadeDB (Apache-2.0) remains the pre-vetted fallback, with two explicit switch triggers.
- scope: Neo4j Community, GPLv3 conveyance, ArcadeDB, GDS Community, distributed appliances

## ADR 0039 — Conversation sharing vs. identity isolation: the public tier as a bounded, mitigated hole in MUSR
- source: D:\Aura\docs\adr\0039-conversation-sharing-vs-identity-isolation.md
- status: locked
- decision: Ship the public share tier, bounded by seven fail-closed mitigations — capability gate, org kill-switch, explicit opt-in, mandatory expiry, mandatory revoke, redacted snapshot, and hashed opaque token — plus audit across every tier.
- scope: public conversation sharing, identity isolation, share.public capability, AURA_SHARE_PUBLIC_ENABLED, opaque share tokens, shared_links, share_audit, Garage

## ADR 0040 — Agent loop semantics
- source: D:\Aura\docs\adr\0040-agent-loop-semantics.md
- status: locked
- decision: The runner owns the turn lifecycle and the durable conversation is authoritative. The model may request tools, but it cannot commit persistence or bypass the gateway. Every loop is bounded by model-call, tool-call, token, wall-clock, and output limits. Cancellation stops new work.
- scope: runner, agent turn lifecycle, durable conversation, tool calls, pauses, retries, cancellation

## ADR 0041 — Tool consequence policy
- source: D:\Aura\docs\adr\0041-tool-consequence-policy.md
- status: locked
- decision: Aura classifies the effective operation from the source tool and normalized arguments. Destructive, irreversible, broad-scope, credential/security-boundary, or externally costly actions require explicit confirmation/resume or are denied when no responder exists. Unknown consequence or missing ownership fails closed.
- scope: tool consequence policy, MCP annotations, mutation authorization, idempotency, sandbox

## ADR 0042 — Memory provenance and erasure
- source: D:\Aura\docs\adr\0042-memory-provenance-and-erasure.md
- status: locked
- decision: Agent Memory is a tenant-scoped MCP capability. Tenant scoping is enforced by the store: each identity has its own database and credential, and owner erasure is a database drop. ADR 0038 owns the choice of store; the current one is ArcadeDB.
- scope: Agent Memory, memory provenance, automatic recall, owner erasure, ArcadeDB

## ADR 0043 — MCP trust and lifecycle
- source: D:\Aura\docs\adr\0043-mcp-trust-and-lifecycle.md
- status: locked
- decision: Managed configuration is authoritative. A server has exactly one transport, explicit enable state, trust class, and bounded mount/call/close budgets. Remote HTTP defaults blocked. Initialization, reconnect, frames, bodies, schemas, results, and shutdown are bounded.
- scope: MCP, managed configuration, remote HTTP, local commands, reconnect schemas, shutdown

## ADR 0044 — Deployment profiles
- source: D:\Aura\docs\adr\0044-deployment-profiles.md
- status: locked
- decision: `dev` and `local_trusted` preserve the operator's direct-host workflow. `single_user_hardened` and `server_production` activate strict validation, authenticated ownership, ToolGateway enforcement, per-identity sandbox routing, egress containment, managed MCP governance, durable mutation ledger, readiness dependencies, and production secret checks. The current appliance is single-node Docker Compose.
- scope: deployment profiles, Docker Compose, ToolGateway, per-identity sandbox routing, managed MCP governance, disaster recovery, release artifacts

## Aura — Dedicated persistent workspace + per-user Garage object store (design + ADR)
- source: D:\Aura\docs\superpowers\specs\2026-07-21-aura-dedicated-workspace-garage-design.md
- status: proposed
- decision: Give Aura a dedicated persistent workspace where it stores scripts, artifacts, and a node/python toolchain, plus a durable per-user object store. The local working root is `/workspace`; durable per-user storage uses the Garage bucket `aura-<id>`; the S3 MCP is deferred to MUSR.
- scope: persistent workspace, Garage object store, document toolchain, S3 MCP, artifact delivery
