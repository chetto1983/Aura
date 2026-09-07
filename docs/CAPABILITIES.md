# Aura — Capabilities

Reviewed against the current source on 2026-09-07. **Implemented** means a production
path exists, not that every configuration has been independently accepted for launch.
Release evidence is governed by [Release readiness](release-readiness.md).

## Agent and operator surfaces

| Capability | Current behavior | Boundary |
|---|---|---|
| Conversations | Web, CLI and Telegram share the turn runtime and durable history | Channel-specific delivery and authentication still apply |
| Streaming and steering | Stream results, interrupt/cancel, and submit steering to a live turn | Disconnecting a browser is distinct from stopping the run |
| Context management | Reuse persisted compaction, compact when configured, and enforce a hard context budget | Compaction may call a model; fallback truncation is reported |
| Tools | Files, terminal, web, documents, scheduling, skills and MCP tools | Tool policy, granted capabilities and sandbox routing determine execution |
| Deferred discovery | Load large tool schemas through `tool_search` | A deferred tool remains discoverable; it is not a hidden/removed operation |
| Delegation | Bounded foreground/background workers, status and durable outcomes | Results belong to their originating conversation; external delivery is explicit |
| Scheduler | Reminders, agent jobs and maintenance; operator inspection and control | Job approval and notification behavior depend on the job and owning route |
| Cockpit | Chat, documents, memory graph, scheduler, settings, approvals, integrations and skills | Administrative actions require the corresponding capabilities |

## Knowledge and memory

| Capability | Current behavior | Evidence or implementation |
|---|---|---|
| Durable facts | Typed entities, sources, validity windows, replay, precise supersession and forgetting | `internal/arcadedb/memory*.go` |
| Atomic memory batches | Ordered identity-scoped changes with replay receipts and final-state validation | `internal/arcadedb/memory_batch*.go` |
| Retrieval | Semantic/lexical/graph operations; independent fact/conversation rankings and quotas | `internal/arcadedb/memory_recall*.go` |
| Historical paths | Bounded native traversal with `as_of`, supporting facts and explicit consistency semantics | [Graph validation](memory-graph-validation.md) |
| Graph diagnostics | Stored connectivity, components, degree and coreness | Structural statistics; diagnostics reject temporal projection |
| Historical mention support | Native links preserve record identity after an active correction key closes | Complete sweeps retain history; incomplete inventories do not reconcile |
| Conversation recall | Derived search records hydrate bounded authoritative Postgres turns | Edits/deletions propagate; active-context sources are excluded where applicable |
| Explicit reasoning recall | Authorized provider-exposed traces are accessed through an explicit selector | Not ordinary recall, automatic context, or a source of memory facts |
| Memory MCP | 13 operations in the current server schema, with OAuth-derived identity | [Live tool schema](arcadedb-mcp-live-tools.json) |
| Document retrieval | Indexed passages, citation tokens, source hashes, locators and degradation status | [Document ingestion](document-ingestion.md) |
| Whole-file work | `document_open` materializes the original for computation and conversion | Use for aggregates, conversions and insufficient passage evidence |

## Identity and extension

| Capability | Current behavior |
|---|---|
| Authentication | Authula-backed web identity and sessions; identity-bound MCP OAuth |
| Memory isolation | A database and credential per identity; server-enforced access |
| Object isolation | Per-identity Garage bindings and protected credential resolution |
| Skills | Owned/shared skills, grants, install, use, archive and restore; builtins have a protected lifecycle |
| MCP | Managed registry in Postgres, HTTP and stdio connections, preparation and per-identity authorization |
| MCP views | Mounted servers can expose UI resources rendered through the cockpit |
| Recovery access | Audited operator recovery paths and controlled deprovisioning |
| Sandbox | Per-identity execution boxes and deployment-dependent egress/isolation controls |

## Operations

| Capability | Current behavior | Limit |
|---|---|---|
| Model configuration | Supported primary-route settings apply through the runtime/settings path | A model's advertised capabilities determine usable features |
| Local services | Embeddings, ingestion and optional local model/media services | Hardware and Compose profiles matter; cloud routes remain external |
| Observability | Structured logs, metrics, traces, health/readiness and operator boards | A healthy process alone does not prove every dependency or exporter works |
| Postgres backup | Nightly dump, atomic completed-file promotion, 14-day retention | Include the resulting files in an off-host policy |
| ArcadeDB backup | Native per-database automatic ZIP backups every 60 minutes | A separate volume is still on the same host by default |
| Restore drill | Postgres, conversation sidecars, Garage and ArcadeDB | [Measured scope](BACKUP-RESTORE.md), not a complete host-loss rehearsal |
| Release checks | Exact-commit evidence, coverage authorities, mutation, security, recovery and rollback | Missing required evidence does not become a pass because a score is high |

The 18 guided Codex answer cases in the memory report are a bounded self-evaluation.
They do not replace the independent running-Aura answer suite or a public comparative
benchmark. See [Memory validation](memory-graph-validation.md).
