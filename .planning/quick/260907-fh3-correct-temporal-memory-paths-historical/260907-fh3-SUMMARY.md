---
id: 260907-fh3
status: verified-locally
date: 2026-09-07
---

# Temporal memory correction and agent-answer checks

Implemented native valid-time paths with same-query supporting evidence under
REPEATABLE_READ; retained historical mentions by database record identity; made
partial sweeps refuse reconciliation; and prioritized direct evidence in bounded
support-aware expansion. Existing topology-only requests remain supported.

The real MCP write path exposed NULL active keys on closed FACT records. Native
LINK identity fixes historical support without changing correction keys. The
installed interim key index also caused NULL collisions, so schema setup replaces
it with the measured RID index. Regression tests cover both actual supersession
and the index upgrade, in addition to isolated native graph fixtures.

Both Aura and MCP containers were rebuilt and accepted through this agent's
mounted OAuth MCP. Image IDs and the workspace-build provenance are recorded in
[memory-graph-validation.md](../../../docs/memory-graph-validation.md).
Ordinary `codex mcp login aura-memory` completed automatically after restart.

| Evidence | Result |
|---|---|
| [Mounted MCP acceptance](mounted-e2e-results.json) | 11/11 |
| [Six-query context comparison](mounted-budget-results.json) | Direct fact at limit=1: 2/6 to 6/6 |
| Same queries, full JSON within 512 estimated tokens | 2/6 to 6/6 |
| Same queries, full JSON within 1,024/2,048 estimated tokens | 3/6 to 6/6 |
| [Final agent answers](ANSWER-RESULTS.md), with [prior criteria](ANSWER-CRITERIA.md) | 18/18 guided, self-reviewed cases |
| Full ArcadeDB integration with race | PASS, 86.7% coverage, zero skips |
| Full MCP SDK live suite with race | PASS |
| Temporal-validator mutation check | 26/27 killed, 96.3% |

The repeated read protocol also passed with a concurrent validity update.
Historical mention reconstruction, distinct supports on identical endpoints,
orphan exclusion and incomplete inventories are covered by live regressions.
All synthetic records were confined to disposable test-owned databases.

## Reproduction and limits

`eval_mcp_budget.go` consumes before/after mounted evidence captures using the
same fixed instant and Aura's vendored cl100k vocabulary. Example from repo root:

```sh
go run .planning/quick/260907-fh3-correct-temporal-memory-paths-historical/eval_mcp_budget.go -before .planning/tmp/memory-mcp-before.json -after .planning/tmp/memory-mcp-after.json
```

Raw operator memory and local test logs were stored in ignored `.planning/tmp/`;
a concurrent workspace cleanup removed that directory before publication.
Re-running the evaluator requires paired captures; the original private inputs
are unavailable. Committed artifacts retain project answers and aggregate results. Token counts
are estimates, and no conversation budget was borrowed for facts.

The final-answer check exercises the agent's actual use of mounted memory; it is
guided and self-reviewed in an informed session, not independent or blind. It
does not close the broader Phase 49 agent benchmark or establish quality for
every question. No PPR advantage was demonstrated. Repeatable records do not
exclude phantoms, and deleted entities/facts cannot be reconstructed.

Existing tests that rejected all path timestamps or expected current-only mention
sweeps were updated because those contracts changed. Depth-two mock expectations
now enforce the native supported traversal and transaction protocol. No success
criteria were weakened to conceal production failures.

PRD amendments preceded implementation (`5edb272ec`, `810acc6ca`, `c415bcfe2`).
Implementation: `da5d16eec`; commit hooks passed formatting, size, vet and lint.
Final commit hooks and remote CI are checked when publishing this task; this
summary records local acceptance and does not assert a future CI result.
