# Scope, Methodology, and Evidence

## Audit boundary

The authoritative scope was `D:\Aura`. `D:\tmp` was used only for an isolated
Go build cache during one safe focused test; no code from it was treated as
Aura evidence. The audit covered:

- Agent Memory sidecar source, image, configuration, MCP tools/resources, graph
  schema/queries, memory integrations, observer, consolidation, and tests.
- Aura MCP configuration, catalog/manager, transports, bridge, reconnect,
  discovery, idempotency, readiness, CLI, onboarding, and host recall.
- Conversation persistence, context ladder, dynamic recall, prompt building,
  hooks, tool results, final synthesis, LLM transport, and swarm handoff.
- Provisioning/deprovisioning, retention, security/trust controls,
  observability, CI, quality snapshots, runbooks, and design/PRD documents.

The implementation was not modified. No production service, migration,
formatter, generator, package installer, branch, or commit operation was run.

## Baseline and concurrent working-tree state

- Branch: `master`
- Revision: `5c151cf9541ff9946057361f97e757677d82cac8`
- Initial `git status --short`: 246 entries, all treated as user-owned.
- Output directory at baseline: absent.
- Later pre-output checkpoint: 248 entries.
- Final check excluding this output directory: 253 entries.

Several user-owned files changed during the inspection, including active MCP
configuration/identity files. The lead and specialists did not write them.
Consequently, this is a time-bounded audit of evidence observed in the dirty
working tree, not an atomic snapshot. Each finding uses narrow symbols and
paths so it can be revalidated against the subsequent state.

## Workstreams actually executed

The lead read the repository rulebook, captured the baseline, mapped the system,
independently checked core evidence, ran the single focused safe test, reconciled
findings, and wrote the reports.

Three specialist agents ran in parallel:

| Agent | Assigned workstreams | Principal delivered domains |
|---|---|---|
| `architecture_mcp` (Ampere) | Agent A, Repository Mapper; Agent B, MCP Protocol | Components, wiring, lifecycle, contracts, alias enforcement, transports, readiness, legacy bypasses |
| `memory_context` (Hilbert) | Agent C, Memory Lifecycle; Agent D, Context/Token Budget | Full memory lifecycle, exact LLM path, truncation, provenance, retention, token invariants |
| `reliability_security_qa` (Galileo) | Agent E, Reliability/Performance; Agent F, Security/Privacy; Agent G, QA/Observability | Concurrency, partial failure, cross-user attacks, deprovisioning, telemetry, CI/test gaps |

Each specialist was required to read `D:\Aura\CLAUDE.md`, stay read-only, avoid
services/tests/writes, separate assumptions from findings, and challenge a
cross-domain assumption. The lead independently checked the core evidence for
the P0 paths, model-context regression, token boundary, deprovision wiring, and
semantic-error chain before acceptance.

## Method

1. **Inventory:** repository-wide symbol/path searches and configuration/test
   discovery established boundaries before conclusions.
2. **Static call tracing:** interfaces were followed into concrete callers,
   transports, side effects, storage queries, and downstream consumers.
3. **Adversarial review:** specialists independently assessed lifecycle,
   protocol, isolation, concurrency, token, failure, and observability
   assumptions.
4. **Reconciliation:** overlaps were merged by root cause. An issue received P0
   only where the code exposes a direct cross-user disclosure/corruption path.
5. **Safe validation:** one focused existing Go test was run with an isolated
   cache after static evidence identified the exact regression.
6. **Design:** remediation, migration, validation, acceptance, rollback, and
   residual risk were specified without implementation.

## Evidence standards

- `CONFIRMED`: directly established by code/configuration/test evidence.
- `HIGH-CONFIDENCE`: multiple paths support the conclusion but a runtime fact is
  still required.
- `SUSPECTED`: plausible but not proved.
- `NOT ASSESSABLE`: the required runtime artifact is absent.

The register contains 29 CONFIRMED findings and one NOT ASSESSABLE performance
item. No unsupported hypothesis was promoted to a defect. Repository searches
for absent callers or tests are described where absence is material.

## Representative inspected files

### Aura orchestration and context

- `internal/runner/runner.go`, `turn_model_context.go`,
  `dynamic_recall.go`, `runner_delete.go`
- `internal/conversations/context.go`, `context_dynamic.go`, `store.go`
- `internal/agent/llm_agent*.go`, `hooks*.go`, `dynamic_tail.go`
- `internal/agent/prompt/builder.go`
- `internal/llm/config.go`, `openai_compat/client.go`
- `internal/agent/tools/result.go`, `swarm_spawn.go`

### MCP and memory integration

- `internal/mcp/client.go`, `http_client.go`, `transport*.go`,
  `managed_config*.go`, `manager/*`
- `internal/agent/mcptools/bridge.go`, `bridge_reconnect.go`, `mount.go`
- `cmd/aura/main.go`, `mcp.go`, `memory.go`, `memory_onboarding.go`,
  `serve_recall.go`, `serve_provisioning.go`
- `internal/gateway/reserve.go`, `decide.go`
- `internal/knowledge/client.go`

### Vendored Agent Memory

- `docker/agent-memory/Dockerfile`
- `docker/agent-memory/src/neo4j_agent_memory/mcp/{server,_tools,_resources,_observer}.py`
- `.../memory/{long_term,short_term,consolidation}.py`
- `.../integration.py`, `integration_context.py`
- `.../graph/{client,queries,schema}.py`
- `.../config/settings.py`, `.../core/memory.py`
- corresponding `docker/agent-memory/tests` and CI configuration

### Lifecycle, operations, and intent

- `internal/agui/deprovision.go`
- `internal/readiness/state.go`, `internal/retention/policy.go`
- `internal/obs/catalog.go`
- `observability/prometheus/rules/*.yml`, `observability/runbooks/*`
- `.github/workflows/ci.yml`, `compose.yaml`, `.env.example`
- `prd.md`, `docs/superpowers/specs/2026-07-28-memory-surface-design.md`,
  `docs/aura-quality-snapshot.md`

The machine-readable index contains a broader representative list.

## Commands executed

Read-only commands included `git status --short`, `git rev-parse HEAD`,
`git branch --show-current`, `git diff -- <path>`, `rg --files`, targeted `rg
-n -C` symbol/caller searches, and narrow `Get-Content` reads. No web source was
needed because repository code/configuration was authoritative.

The only test command was:

```powershell
$env:GOCACHE='D:\tmp\aura-agent-memory-audit-gocache'
go test ./internal/runner -run '^TestTurnWithModelUserMessagePersistsVisibleTextAndSendsModelContext$' -count=1
```

It failed in 43.7 seconds with `LLM request did not include model-context user
message`, confirming CTX-001. The isolated cache is outside the repository.

Broader suites were not run because the worktree was actively changing and
many relevant suites depend on live services or persistent integration state.
Static inspection of their definitions and CI wiring was still performed.

## Limitations

- No live Neo4j graph was queried, so existing orphan/duplicate/cross-owner data
  volume is unknown.
- No service was started; deployed network policy, FastMCP framework body
  limits, and actual strict runtime profile remain unverified.
- Current-model 100K recall/latency evidence is explicitly invalidated in the
  quality snapshot; production scale is NOT ASSESSABLE.
- No packet/trace capture or production telemetry was available.
- Concurrent user edits mean exact line numbers and some uncommitted behavior
  may change after the cutoff.
