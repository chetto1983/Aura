# Aura — Architecture

Updated 2026-09-07. Module: `github.com/chetto1983/aura`.

Aura is a Go application with an embedded web frontend and a Compose service stack.
The composition root wires domain interfaces into the turn runtime. The same runtime
serves CLI, Telegram and authenticated web requests.

## Main flow

```text
CLI / Telegram / web
        |
        v
Authentication and channel context
        |
        v
Runner: history, context budget, pause/resume, persistence
        |
        v
LlmAgent: model rounds, tool dispatch, terminal response
        |
        +--> Gateway: policy, approval, durable reservation
        |         |
        |         +--> tools / sandbox / skills / MCP / documents / web
        |
        +--> bounded delegation and scheduled work
        |
        v
Events, persisted outcomes and delivery to the owning conversation
```

`internal/agent/agent.go` defines the open `Agent` interface. `Run` returns an
`iter.Seq2[*Event, error]`; events carry runtime outcomes and transport correlation.
`internal/runner` owns a turn's durable lifecycle. `cmd/aura/serve.go` and the chat
composition root assemble its dependencies.

## Runtime and context

A turn rehydrates conversation state and builds a fresh agent. Context management
reuses stored branch compaction, applies configured summarization when appropriate,
and enforces a hard budget. Tool-call/result pairing must remain valid after paging,
compaction and truncation. Provider-exposed reasoning is retained for its authorized
uses but is structurally excluded from ordinary LLM history.

The model-facing tool registry separates loaded definitions from a deferred roster.
`tool_search` promotes matching definitions into the callable set. Large results have
bounded previews and sidecar-backed continuation. Budgets and loop controls bound
work rather than claiming that an agent can run indefinitely.

Primary model routing is managed by `internal/llm`. The OpenAI-compatible wire client
uses the official OpenAI Go SDK. Provider-specific capability and reasoning settings
are translated by that layer. Supported profile changes can be hot-applied; model
credentials and limits are not frozen in this document.

Terminal responses, tool outcomes, approval pauses, cancellation and transport loss
have different states. Background outcomes remain attached to their originating
conversation. An explicit delivery action is required to send them elsewhere.

## Policy and identity

The gateway classifies tool calls, applies the active policy, and records execution
reservations. An indeterminate interrupted operation is not a successful retry and
must not be silently replayed as a fresh side effect.

Identity is resolved by the host. Postgres operations carry owner scope and RLS;
ArcadeDB uses one database and credential per identity. Garage bindings, conversations,
OAuth grants, skills ownership and grants are also identity-aware. Administrative
shared resources are distinct from an ordinary user's resources.

`AURA_PROFILE` selects `dev`, `local_trusted`, `single_user_hardened`, or
`server_production`. Strictness and sandbox routing depend on configuration and the
host. The enforcing Docker/gVisor path requires native Linux. Do not equate Docker
Desktop with that boundary or describe a default installation as universally hardened.

## Stores and authority

| Store | Authority and responsibility |
|---|---|
| Postgres | Conversations, identities, settings, scheduling, approvals, control metadata and audit records |
| ArcadeDB | Memory facts and graph relationships; derived conversation, reasoning and document retrieval records |
| Garage | Original objects and identity-bound file storage |
| Aura/workspace volumes | Runtime files, tool-result sidecars, materialized working files and integration state |

Postgres schema migrations are numbered from the migration directory at landing time.
The generated sqlc output must match its queries and migrations. ArcadeDB schemas are
applied through idempotent initialization; the memory and ingestion writers each own
their schema contracts.

## Memory

`cmd/arcadedb-mcp` exposes the authenticated memory API using the official Go MCP SDK.
The caller cannot supply another identity or an arbitrary database/query. The Go
client in `internal/arcadedb` uses native database operations for retrieval and writes.

Facts carry subject, predicate, object, statement, provenance and validity windows.
The active correction key and a historical database record identity serve different
purposes. Native `MENTIONS.fact_rid` links preserve historical support after closure.
Full mention sweeps scan retained history; incomplete inventories cannot reconcile.

Temporal `graph_path` checks admissibility during native traversal and returns the
supporting facts in the same query under REPEATABLE_READ. This provides repeatable
record reads, permits phantoms, and does not restore erased history. Without `as_of`,
paths describe stored topology. Graph diagnostics remain structural.

Facts and conversations are ranked separately and composed by quota. Conversation
hits hydrate bounded Postgres-authoritative windows. Automatic memory context is
bounded and reports its actual contribution. Reasoning requires explicit selection
and cannot become ordinary recall or fact-capture evidence.

See [Memory validation](memory-graph-validation.md) and the current
[MCP schemas](arcadedb-mcp-live-tools.json) for the concrete contract.

## Documents

`services/ingest` uses CocoIndex to reconcile identity-bound Garage sources into
ArcadeDB document cards and passages. Text extraction, supported format conversion,
token-bounded chunks and embeddings feed native indexing. The Go supervisor manages
workers from provisioned identities and their existing object-store bindings.

`internal/documents` reconciles retrieval with authorized source scope and returns
citations, source hashes, locators and explicit degraded status. `document_open`
provides the original bytes for computation. Conversation memory and document
retrieval are distinct contracts even when stored in the same tenant database.

## Extensions and transport

The managed MCP registry is in Postgres. Connections use HTTP or stdio, with native
SDK lifecycle and authorization handling. Stdio package preparation is separate
from starting a long-lived connection. Mounted MCP results are trusted by the
current product policy and retain size limits and tool authorization controls.
Web/document content and delegated output retain their separate untrusted-content
boundaries. Resource views can be rendered through the cockpit.

Skills are loaded from their ownership-aware roots. The shared library, user-owned
skills, explicit grants, and builtin skills have different lifecycle rules. The
retired `Agent.md` profile and file-backed `servers.json` registry are not runtime
sources.

`internal/agui` handles HTTP/SSE, run resumption and cockpit APIs. `internal/webui`
embeds the frontend build. Telegram is a channel adapter using shared attachment,
turn, cancellation and delivery behavior. The runtime remains independent of AG-UI.

## Operations and verification

Logs, OpenTelemetry, metrics and readiness expose different aspects of runtime state.
Backup mechanisms are owned by Aura for Postgres and by ArcadeDB for its databases.
[Backup and restore](BACKUP-RESTORE.md) documents the four-plane drill and its limits.

Quality checks include unit/race, live integration, browser tests, mutation and
separate coverage authorities. A tagged release requires the complete
[release evidence bundle](release-readiness.md). The product contract is [prd.md](../prd.md);
source versions and default settings live in manifests and [.env.example](../.env.example).
