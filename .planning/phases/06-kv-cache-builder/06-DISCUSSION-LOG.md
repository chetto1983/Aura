# Phase 6: KV Cache Builder - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-02
**Phase:** 6-KV Cache Builder
**Areas discussed:** PromptBuilder shape, Cache-stats storage, Anthropic cache_control seam, CI gate fidelity, Stub LLM sourcing

---

## PromptBuilder shape

| Option | Description | Selected |
|--------|-------------|----------|
| Extract named PromptBuilder | Refactor inline construction into a real PromptBuilder type; clean seam for messages[1]/[2]; CI hook point. Invariant must survive the move. | ✓ |
| Keep inline, add layers on top | Don't refactor; invariant already holds; just add Tracker + cache-stats + CI gate additively. | |

**User's choice:** Extract named PromptBuilder
**Notes:** Surfaced that PRD's `internal/llm/prompt.go` target is likely unworkable (import cycle: `internal/llm` can't import `internal/agent/tools`) — builder belongs in `internal/agent`. Flagged as planner constraint D-01a.

---

## Cache-stats storage

| Option | Description | Selected |
|--------|-------------|----------|
| Persist per-turn metrics to Postgres | New aura.cache_metrics table; makes `cache-stats --since=1h` a real query; satisfies roadmap SC#4; overrides PRD OQ2. | ✓ |
| In-memory tracker, drop --since | Honor PRD OQ2 in-memory; requires amendment dropping --since from SC#4. | |
| In-memory + optional JSONL flush | Process-local + flush to $AURA_RUN_DIR jsonl for time window; no migration. | |

**User's choice:** Persist per-turn metrics to Postgres
**Notes:** Resolves the PRD-OQ2-vs-roadmap-SC#4 conflict in favor of the roadmap's literal `--since=1h`. Requires a PRD-amendment commit before implementation (PRD-first). Usage data already shipped (llm.Usage.CachedTokens/Cost).

---

## Anthropic cache_control seam

| Option | Description | Selected |
|--------|-------------|----------|
| Defer — YAGNI, document the seam | Don't add cache_control to Request/ToolDef for an unused provider; document where it would hook for Slice 13. | |
| Build the no-op seam now | Add provider-aware injection + Request.ToolsCacheControl per PRD OQ3, even as a no-op under OpenRouter. | ✓ |

**User's choice:** Build the no-op seam now
**Notes:** Aligns with PRD OQ3's "Proposto: SÌ" — no amendment needed. Breaks the "wire layer is caching-unaware" design comment in client.go → update on touch (D-03a).

---

## CI gate fidelity

| Option | Description | Selected |
|--------|-------------|----------|
| Runtime-faithful (stub-LLM replay) | Drive real 20-turn runner.Turn loop against deterministic stub LLM; hash messages[0] each turn via hidden `aura cache-audit`. Catches actual cross-slice poisoning. | ✓ |
| Synthetic Build() hash | Script calls Build() over fixtures and hashes [0]; constant-by-construction, catches nothing. | |
| Both — unit + runtime | Synthetic unit test + runtime-faithful CI gate; highest coverage, most code. | |

**User's choice:** Runtime-faithful (stub-LLM replay)
**Notes:** Claude flagged that messages[0] is constant by construction, so a synthetic hash is trivially green — the gate's value (amendment #16) is catching a future slice mutating the assembled prefix at runtime. Operator entrypoint = hidden `aura cache-audit` subcommand + thin bash wrapper.

---

## Stub LLM sourcing

| Option | Description | Selected |
|--------|-------------|----------|
| Reuse/extend existing test fakes | Extend agenttest.FakeClient (importable, captures Requests). | ✓ |
| New dedicated audit harness | Purpose-built deterministic client in a non-test package. | |
| You decide during planning | Defer sourcing to researcher/planner. | |

**User's choice:** Reuse/extend existing test fakes
**Notes:** Verified `internal/agent/agenttest/fakeclient.go` is importable (not _test.go), implements llm.Client, and captures every req in `Requests` — audit reads `Requests[n].Messages[0]` directly. `cmd/aura/cmdfakes_test.go` explicitly ruled out (package main, test-only, unreachable from a shipped subcommand).

## Claude's Discretion

- `aura.cache_metrics` column types / index strategy; aggregation client-side vs SQL.
- Exact PromptBuilder package boundary (subject to import-cycle constraint D-01a).
- Fixture turn content for the 20-turn replay (must be deterministic, include tool-call turns).

## Deferred Ideas

- messages[1] content (Agent.md) → Slice 10; messages[2] content (AgentInsight) → Slice 11e.
- Runtime provider selection / activating Anthropic ephemeral path → Slice 13 LLMRouter.
- Throwaway `chat-loop` REPL → superseded by shipped persisted `aura chat`.
