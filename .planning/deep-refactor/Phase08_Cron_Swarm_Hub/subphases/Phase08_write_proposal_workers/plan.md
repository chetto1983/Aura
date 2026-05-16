# Phase 8 — Write-Proposal Workers (Phase-S)

## Source references

- `prd.md §Phase 8` line 1471: "allows future write-capable workers only through proposal or workflow gates"
- Master plan Wave 3 Pack C (deferred): "propose_patch + sanitization + TTL"

## Goal

Extend the Phase-R read-only subagent fanout with a `write_proposal` RiskTier.
Write-capable subagents CANNOT mutate wiki/sources/tasks directly; they can only
call `propose_patch`, which lands rows in `proposed_updates` for operator review.

## Stories

| ID    | Title                                                              |
|-------|--------------------------------------------------------------------|
| US-S01 | Add `propose_patch` LLM tool (ActionDispatchOneOf schema)         |
| US-S02 | Extend `NodeSpec.RiskTier='write_proposal'` + tool allowlist policy |
| US-S03 | Extend `subagent_dispatch` tool: accept `risk_tier` per node      |
| US-S04 | Integration test + TTL sweep + Phase-S closure docs               |

## Architecture decisions

1. **No direct writes** — proposals only. propose_patch is the sole mutation path.
2. **Single source of truth for forbidden tools** — `swarm.DirectWriteToolNames`
   in `tool_policy.go`; `subagent.go` mirrors it locally to avoid the import cycle.
3. **TTL sweep** — stale pending proposals (>30 days) are purged by a daily
   cron task `proposal_ttl_sweep` at 03:00 local. Approved/rejected rows are
   never swept.
4. **Fail-open auth** — hub.authorizeSwarmDispatch returns nil when no Authorizer
   is in context (same as all other channels).
