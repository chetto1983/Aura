# ADR 0042 — Memory provenance and erasure

- **Status:** Accepted
- **Date:** 2026-07-31 · **Amended:** 2026-08-02 (backing store named below; see ADR 0038)
- **Requirement:** OPS-06 / F-025
- **Relates to:** `prd.md` Amendments #61, #62, #103, #106
- **Supersedes:** ADR 0038's active Neo4j store choice; ADR 0038 remains a historical licensing record

## Context

Long-term Memory can silently cross tenants, inject more context than configured, lose source
identity, or survive owner deletion unless one component owns provenance and erasure.

## Decision

Agent Memory is a tenant-scoped MCP capability, mounted once and shared by
model-facing calls, automatic recall, and semantic readiness. Tenant scoping is enforced by the
**store**, not by a query filter: the backing store gives each identity its own database and its
own credential, so a cross-tenant read is refused by the server rather than being one forgotten
`WHERE` clause away. Owner erasure is therefore a database drop, not a sweep. (ADR 0038 owns the
choice of store; the current one is ArcadeDB.) Every stored or recalled item carries
its immutable kind and ID. Automatic recall enforces one aggregate item cap and reports the exact
admitted order plus observed reranker outcome; degraded ranking is not adaptive-eligible.

Aura does not launch unowned post-write observation, preference, consolidation, or retention tasks.
The identity-deletion saga is the sole owner-erasure coordinator and verifies all registered planes.
Conversation sidecars remain bounded working history, not a second hidden long-term memory.

## Consequences

- Owner identity is required at the host/MCP boundary and never accepted from model arguments.
- Readiness performs a bounded tenant-scoped functional search through the process-owned client.
- Provenance, aggregate bounds, concurrent writers, readiness failure, and deletion verification
  are release scenarios.
- New automatic inference or consolidation requires an explicit lifecycle and erasure amendment.
