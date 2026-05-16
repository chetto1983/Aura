# Phase06 Lesson Promotion — Plan

Status: **closed 2026-05-16** — US-N01..N04 shipped (commits ff936cbb, a53cdde6, e6fc412b, US-N04 SHA post-commit).

## Goal

Close the Phase 6 deferral: wire the full `experience_store → operational_memory → skills`
layer movement (prd.md §5.7 line 707, §5.8 lines 776-779).

The Phase 6 backbone (`tool_attempts` table, ToolObservation contract, retry budgets,
WarningsReader) shipped in US-J01..J06. This sub-phase adds the missing aggregation +
promotion + retrieval pieces.

## Scope

| Story | Title | Key artefact |
| --- | --- | --- |
| US-N01 | LessonCandidate + AggregateForPromotion | `internal/agent/tools/attempts/aggregator.go` |
| US-N02 | PromoteLessons + cron task | `internal/learning/promoter.go` + cron wiring |
| US-N03 | WriteApprovedLesson → KindOperational | `internal/learning/writer.go` + applier hook |
| US-N04 | recall_operational LLM tool + closure docs | `internal/agent/tools/registry/recall_operational.go` |

## Key prd.md references

- §5.8 lines 751-802 — internal/learning contract (minimum loop: tool failure → structured feedback → retry → persist → retrieve → promote after validation)
- §5.7 lines 691-693 — layer movement: experience_store → operational/skills via validation
- Phase 6 line 1394 — "promote repeated lessons into memory, skills, or tool policy only after validation"

## Non-Goals

- Automatic promotion without operator approval (US-N03 gate is explicit approval)
- Self-coding (§12 non-goal)
- Phase 9 write-policy hardening (separate milestone)
