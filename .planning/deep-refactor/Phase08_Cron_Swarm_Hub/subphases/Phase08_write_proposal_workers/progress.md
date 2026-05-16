# Phase-S Progress

## Status: CLOSED 2026-05-16

| Story | Title                                                         | SHA        |
|-------|---------------------------------------------------------------|------------|
| US-S01 | propose_patch action-dispatch tool                           | 64b6623d   |
| US-S02 | RiskTier=write_proposal + tool allowlist policy              | 52ff817c   |
| US-S03 | subagent_dispatch risk_tier=write_proposal support           | ad5e1c74   |
| US-S04 | Integration test + TTL sweep + Phase-S closure               | (this commit) |

## What shipped

- **propose_patch** tool: 3 action variants (wiki / user_memory / operational),
  sha256[:16] idempotency, provenance from identity context, tests 6 cases.
- **RiskTier='write_proposal'** in NodeSpec.Validate + DirectWriteToolNames
  single source of truth in tool_policy.go.
- **subagent_dispatch** tool extended with optional risk_tier per node; server-side
  allowlist enforcement; default allowlist for write_proposal includes propose_patch
  + common read tools.
- **TestParentSpawnsTwoWriteProposalSubagentsAndCollectsProposals**: E2E test
  asserting 2 pending proposed_updates rows, correct kinds, aggregated markdown,
  and wiki_page-in-allowlist blocked by NodeSpec.Validate.
- **SweepStaleProposals** (internal/learning/proposal_ttl.go): deletes pending
  proposals older than maxAge; approved rows never swept. Daily cron task
  'proposal_ttl_sweep' at 03:00.

## Architecture boundary respected

`write_proposal` subagents can call propose_patch (review-gated) but CANNOT
call wiki_page, source, task, file, or agent_note directly. NodeSpec.Validate
enforces this at dispatch time.
