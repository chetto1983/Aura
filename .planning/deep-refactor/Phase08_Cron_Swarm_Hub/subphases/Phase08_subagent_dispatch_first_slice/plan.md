# Phase08 Subagent Dispatch First Slice — Plan

Status: **closed 2026-05-16** — US-R01..R04 shipped.

## Goal

Close capability gap #5 "RunGraph subagent dispatch dinamico" from the 2026-05-16
"Aura come Claude Code" brainstorm. Scope: **read-only fanout only** (no write-capable
workers, no team_collaboration, no plan-execute, no critic-review, no hierarchical DAG —
all explicit deferrals for full Phase 8).

## Scope

| Story | Title | Key artefact |
| --- | --- | --- |
| US-R01 | NodeSpec + HubBridge adapter | `internal/swarm/nodespec.go` + `hub_bridge.go` Dispatch method |
| US-R02 | Wire ChannelSwarm + Router.WaitForRun | `internal/chat/hub_swarm.go` + `hub.go` swarm handling |
| US-R03 | subagent_dispatch LLM tool | `internal/agent/tools/registry/subagent.go` |
| US-R04 | Integration test + Phase-R closure | `internal/swarm/parent_child_integration_test.go` + docs |

## Key prd.md references

- §Phase 8 line 1589: "treats read-only fanout as a safe first slice, not the permanent architecture ceiling"
- §Phase 8 lines 1582–1583: NodeSpec with goal, instruction, tool/capability grant, budgets, risk tier
- memory `feedback_minipc_cpu_budget`: BudgetSecs ≤ 300, max parallel children ≤ 3, poll interval ≥ 200ms

## Non-Goals (explicit deferrals — full Phase 8 scope)

- `write_proposal` risk tier workers
- `team_collaboration` (named teammates + durable mailbox)
- plan-execute topology
- critic-review topology
- hierarchical and hybrid DAG execution
- durable RunGraph/EdgeSpec storage substrate (Phase 8B)
- ScheduleFire + durable cron migration (Phase 8D-E)
- Team trace events, plan approval gates, budget events (Phase 8F-G)
