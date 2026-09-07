# Aura — Technical overview

Updated 2026-09-07. This document describes the current implementation and product
scope. A capability being implemented is distinct from approving a release; use
[Release readiness](release-readiness.md) for the exact-candidate evidence contract.

## Product

Aura is an agent for ongoing work on infrastructure the operator controls. It combines
conversations, temporal memory, document retrieval, tool execution, scheduled work,
and an embedded web cockpit. CLI and Telegram use the same underlying turn runtime.
The intended appliance audience includes small organizations that want to operate
their own assistant and choose their model provider.

The runtime is a Go binary, accompanied by Compose services. Postgres, ArcadeDB,
Garage, embeddings, ingestion, optional model/media services and integrations are
separate processes. "One binary" describes the application, not the whole stack.

## What makes the implementation useful

| Concern | Implemented approach | Practical meaning |
|---|---|---|
| Ongoing context | Durable conversations, compaction, memory recall and projection | Earlier work can be retrieved after it leaves the current context |
| Changing knowledge | Facts with sources and validity windows; explicit correction and erasure | Historical and current evidence can be distinguished |
| Document work | Indexed passages with citations, plus original-file access | Lookup and whole-file computation use the appropriate evidence |
| Account boundaries | Owner-scoped Postgres operations, per-identity ArcadeDB credentials/databases, Garage bindings and skills ownership | Identity is resolved by the host rather than accepted from a tool argument |
| Consequential tools | Policy decisions, approvals and durable execution records | Operators can inspect what was requested and what actually ran |
| Extensibility | Instruction skills, executable snippets and managed MCP connections | Capabilities can be added without changing the core agent loop |
| Operations | Health/readiness, logs/traces, backup schedules and restore drills | Deployment state has executable checks and inspectable evidence |

These are product choices, not claims of exclusive algorithms or superiority over
other agents. The native memory implementation and its measured limits are described
in [Memory validation](memory-graph-validation.md).

## Deployment and model boundary

The interactive `create-aura-appliance` installer can target the workstation or a Linux
host over SSH. It probes the target and selects CPU or CUDA embeddings. Its requirements
and defaults are maintained in the [installer guide](../packages/create-aura/README.md).

Model routing is configurable. OpenRouter is the default route; local
OpenAI-compatible endpoints are supported. A cloud route receives the context supplied
to it, so self-hosted storage does not imply fully offline processing. Optional
integrations similarly contact their configured services. Credentials and supported
model settings can be managed through the cockpit.

Runtime profiles and sandbox configuration determine execution boundaries. Do not
infer a hardened or mutually-untrusted multi-user deployment from a default install.
The supported strict sandbox path requires native Linux; Docker Desktop is not the
same isolation environment. See [Architecture](ARCHITECTURE.md).

## Memory and data

Postgres is authoritative for conversations, identities, settings, jobs and control
records. ArcadeDB holds memory facts and derived retrieval records, including eligible
conversation projections and document indexing. Garage stores original objects.

Ordinary memory retrieval keeps facts and conversation evidence distinct. Provider-
exposed reasoning is a separate, explicitly selected evidence class; it is excluded
from automatic context and fact capture. A temporal graph path describes admissible
relationships still stored, with supporting records. It does not reconstruct erased
history or establish causation.

## Recovery

Postgres has a scheduled dump and rolling retention. ArcadeDB independently backs up
all databases every 60 minutes to a separate volume. Both the four-plane restore drill
and a restore of a real scheduled memory archive passed on 2026-09-07.
[Backup and restore](BACKUP-RESTORE.md) records the procedures and measurements.

Recovery also needs objects, workspaces, configuration, encryption keys and relevant
integration state. Local backup volumes alone do not cover host loss. Small fixture
restore timings are not service-level recovery commitments.

## Distribution and evidence

As checked on 2026-09-07, `v1.0.2-rc1` is the latest tagged prerelease; `edge` tracks
master. Check [Releases](https://github.com/chetto1983/Aura/releases) for current tags.
Tagged publication requires the release evidence bundle. CI, image publication,
local deployment and release approval are distinct results.

The test surface includes race/leak checks, live database and MCP integration,
coverage policies, mutation checks, browser tests and restore drills. Consult
[Capabilities](CAPABILITIES.md), [the CI workflow](../.github/workflows/ci.yml), and
[the dated quality ledger](aura-quality-snapshot.md). Passing selected memory cases
does not establish answer accuracy on every question or domain.
