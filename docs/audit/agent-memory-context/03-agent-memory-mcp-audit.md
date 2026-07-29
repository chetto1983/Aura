# Agent Memory and MCP Audit

## Executive domain conclusion

The memory service is deeply integrated and its code is vendored, testable, and
operationally reproducible. Its central security contract is nevertheless
unsafe: tenant scope is an optional caller argument on an unauthenticated
service. Aura's canonical bridge compensates for this on one path, but direct
resources, direct clients, aliased mounts, and sidecar short-term behavior
bypass that compensation.

The memory write contract is also ambiguous. A logical mutation spans multiple
Neo4j transactions, while many integration exceptions are returned as ordinary
JSON text. Aura then treats that text as success and may durably replay it.

## Deployment and server lifecycle

- `docker/agent-memory/Dockerfile:9-15,51-79` builds a pinned vendored fork and
  records the upstream baseline/provenance.
- `compose.yaml:537-636` runs `neo4j-agent-memory mcp serve` with HTTP transport,
  extended profile, persistent sessions, `aura-local` default user, loopback
  host publication, and Compose-network reachability.
- `docker/agent-memory/src/neo4j_agent_memory/mcp/server.py:103-119` constructs
  `FastMCP`, registers tools/resources/prompts, and provides no auth provider.
- The health check at `compose.yaml:631-636` establishes only that a TCP socket
  accepts connections.

The image deliberately omits some extraction stages because Aura is intended
to drive long-term memory explicitly. The extended MCP server still registers
short-term resources/tools and observer paths, creating a larger direct surface
than Aura exposes to its model.

## Contract inventory

### Aura model-visible memory tools

`internal/agent/mcptools/bridge.go:236-277` exposes six long-term tools on a
canonically named memory mount:

- `memory_search`
- `memory_add_fact`
- `memory_add_preference`
- `memory_get_entity`
- `memory_update`
- `memory_forget`

Short-term/session, relationship, and lower-level entity/fact tools are hidden
from the model but remain available to direct host/direct MCP consumers. Memory
tools are non-deferred only when the namespace is literally `memory`.

### Host consumers

- `cmd/aura/memory.go`: explicit operator search/read/write/update/forget and
  short-term session/message operations.
- `cmd/aura/memory_onboarding.go`: sequential profile writes and a completion
  sentinel.
- `cmd/aura/serve_recall.go`: owner-scoped, long-term-only automatic recall.

Automatic recall opens a fresh MCP transport/session for each eligible turn
through `callMemoryToolText` rather than reusing the mounted reconnecting
server. This duplicates lifecycle, health, and retry semantics
([ARC-003](08-findings-register.md#arc-003-automatic-recall-opens-a-fresh-raw-mcp-session-per-turn)).

## MCP startup, registration, and routing

Aura sorts managed server names, opens them within a bounded aggregate budget,
lists tools, validates tool names/schemas/collisions, and publishes a bridge
only after the set is acceptable (`cmd/aura/main.go:352-405`;
`internal/agent/mcptools/bridge.go:467-545`). A failed server is logged and
dropped without blocking boot.

This is deterministic at initial mount, but four contract gaps remain:

- recipe source is discarded before bridge enforcement; a supported alias
  bypasses identity stamping/hiding ([MCP-001](08-findings-register.md#mcp-001-memory-recipe-alias-bypasses-scoping-and-surface-policy));
- lock wait is not context-cancellable ([MCP-002](08-findings-register.md#mcp-002-mcp-session-lock-wait-ignores-cancellation));
- reconnect refresh handles changed definitions only, not added/removed tools
  ([MCP-005](08-findings-register.md#mcp-005-reconnect-does-not-publish-added-or-removed-tools));
- initialize results are not validated against a common protocol/capability
  contract ([MCP-006](08-findings-register.md#mcp-006-initialize-negotiation-is-not-contract-validated)).

Stdio and HTTP intentionally use different protocol versions, but no common
compatibility table explains or tests that difference.

## Identity, authorization, and resources

The canonical bridge overwrites a tool's `user_identifier` with Aura's
authenticated identity and tags returned content untrusted. That is a good
client control, not server authorization.

At the service boundary:

- `MemoryConfig.multi_tenant` defaults false
  (`config/settings.py:288-296`) and Compose does not enable it.
- tools accept optional `user_identifier` (`mcp/_tools.py:123-506`);
- missing scope selects global graph/vector queries;
- the server has no transport principal from which to derive a tenant;
- `memory://entities` and `memory://preferences` are global resources, and the
  session context resource has no owner parameter
  (`mcp/_resources.py:41-108`);
- enabling the existing multi-tenant switch does not guard every read/write.

The full failure is [SEC-001](08-findings-register.md#sec-001-unauthenticated-memory-mcp-exposes-global-and-forgeable-tenant-scope).
The independent short-term ownership takeover is
[SEC-002](08-findings-register.md#sec-002-session-claiming-can-cross-user-short-term-memory).

## Memory lifecycle trace

### Capture, classification, and normalization

Memory enters through explicit model tools, CLI commands, onboarding, or direct
short-term message storage. Automatic preference extraction is disabled in
Compose. Pydantic constrains basic shapes and entity types are normalized, but
there is no PII/secret classification or executable-instruction classification
before persistence.

### Deduplication and persistence

- Entities use exact/semantic lookup and have a composite uniqueness control.
- Facts/preferences perform check-then-create under owner/deduplication scope.
- Each `execute_write` is its own transaction and atomically increments the
  corpus epoch with that query.
- Node creation, temporal properties, owner edges, and subject/entity links are
  separate writes.

This permits orphans and duplicate retry results
([MEM-001](08-findings-register.md#mem-001-logical-memory-mutations-are-not-atomic))
and concurrent duplicates/lost updates
([CON-001](08-findings-register.md#con-001-concurrent-memory-writers-can-duplicate-branch-and-lose-updates)).

### Retrieval, ranking, and injection

Scoped searches retrieve a five-times candidate pool and optionally rerank.
Automatic context retrieves preferences and entities independently, then
formats them as plain text. The model-visible text drops IDs, confidence,
source, time, validity, and score while an out-of-band adaptive ledger keeps
ordered IDs and revisions.

The declared reranker revision is hard-coded `none-v1` even when Compose enables
the reranker. See
[MEM-004](08-findings-register.md#mem-004-recall-strips-item-provenance-and-misreports-reranker-revision).
The configured “per turn” item cap is actually applied once per kind, allowing
2x the advertised total
([MEM-005](08-findings-register.md#mem-005-recall-can-inject-twice-the-configured-item-cap)).

### Update, supersession, expiration, and delete

Owner-scoped per-node update/forget primitives exist. Supersession writes
`valid_until` and a `SUPERSEDED_BY` edge, but ordinary preference search does
not filter either; obsolete preferences remain eligible for recall
([MEM-002](08-findings-register.md#mem-002-superseded-preferences-remain-eligible-for-recall)).

Consolidation and TTL archival are dormant/manual. Archive marks records rather
than deleting them, and a dormant supersession job can choose a candidate from
another owner. There is no bulk user-memory delete adapter
([MEM-003](08-findings-register.md#mem-003-retention-and-consolidation-are-dormant-and-not-fully-owner-safe);
[PRIV-001](08-findings-register.md#priv-001-production-deprovisioning-retains-agent-memory-and-conversations)).

## Error mapping, idempotency, and retry safety

The common reconnect wrapper correctly refuses to replay a mutating call after
an ambiguous send failure. The higher semantic layer defeats that guarantee:
Agent Memory catches exceptions and returns `{"error": ...}` content, the
bridge sees nil transport error, and strict gateway mode records the operation
completed for up to 30 days. Onboarding also checks only the transport error and
can stamp its completion sentinel after semantic write failures.

See [MCP-004](08-findings-register.md#mcp-004-domain-errors-become-successful-idempotent-mcp-results).
CLI `memory update` is omitted from the command mutation registry and the memory
CLI replaces its invocation context with `context.Background`
([MCP-007](08-findings-register.md#mcp-007-cli-memory-update-and-operation-context-bypass-idempotency-controls)).

## Legacy and bypassed implementations

`internal/knowledge/client.go` remains an active independent MCP-like subprocess
client used by chat, serving, and provisioning. It passes a Neo4j password in
argv and lacks common client frame/process/cancellation hardening
([ARC-001](08-findings-register.md#arc-001-active-knowledge-client-bypasses-common-mcp-hardening)).

This duplication is not dead code. It creates two process/session security and
failure contracts that operators must understand and test.

## Positive controls

- Deterministic sorted initial mount and collision rejection.
- Default-unknown MCP mutations are treated as mutating, not read-only.
- Canonical memory calls overwrite supplied identity.
- MCP results carry untrusted provenance.
- Common clients cap frames/bodies and protect read-only replay.
- One write query and its corpus epoch increment share a transaction.
- Entity composite uniqueness prevents one important duplicate class.
- CI rebuilds/tests the vendored image rather than a stale external image.

These should be preserved through remediation.
