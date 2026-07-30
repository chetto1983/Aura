# ADR 0042 — Memory provenance and erasure

- **Status:** Accepted
- **Date:** 2026-07-31
- **Requirement:** OPS-06 / F-025
- **Relates to:** `prd.md` Amendments #61, #62, #103, #106

## Context

Long-term Memory can silently cross tenants, inject more context than configured, lose source
identity, or survive owner deletion unless one component owns provenance and erasure.

## Decision

Agent Memory is a tenant-scoped MCP capability backed by Neo4j, mounted once and shared by
model-facing calls, automatic recall, and semantic readiness. Every stored or recalled item carries
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
