# Phase 42: Industrial Conversation Compaction — Context

## Phase boundary

Rewrite Phase 42 as the complete industrial context lifecycle defined by the approved design and replacement SPEC. All formerly deferred production patterns are now in scope: proactive L2.4, recent-tail preservation, recursive compaction/rebase, typed content parts, durable memory separation, recovery/undo, operations UI, numerical evaluation, and staged rollout.

## Locked decisions

- Section 17 of `docs/superpowers/specs/2026-07-13-industrial-conversation-compaction-design.md` is normative and overrides older prose.
- `42-SPEC.md` IC-01..14 is the requirement set.
- `42-TRACEABILITY.md` must be complete before replacement plans are accepted.
- Legacy plans 42-01..07 are retired and cannot execute.
- L2.4 runs before L2.5 through one pure `BudgetDecision` seam.
- Trigger ratio is 0.80; target 0.55; minimum savings is max(4096,10% capacity); minimum reduction 20%.
- Invalid/consumed capacity fails closed without ratio math, persistence, or L2.5 loop.
- Recent tail is token-based, semantic-unit atomic, and disjoint from summarized manifests.
- Canonical transcript remains immutable and authoritative.
- Checkpoints are branch-aware structured JSON generations selected by CAS active pointer, never timestamp.
- Distributed durability uses claim → out-of-tx inference → serializable CAS finalize.
- Stable operation ID is reused across all retries.
- L0/governance is not summarized; unresolved user instructions use a typed authority ledger.
- Summaries render as internal non-authoritative context, never a fresh ordinary user instruction.
- Typed content parts are a prerequisite, not text pretending to be multimodal.
- Recursive generations cap at four before hierarchical canonical rebase.
- Durable memory is a separate typed product with automatic promotion disabled by default.
- Recovery is active → last-known-good → bounded rebuild → context_unavailable.
- Baseline telemetry/recovery ship before activation; enabled mode waits for numerical gates.
- Every delivery slice remains deployable with semantic activation disabled and rollback-compatible.

## Agent discretion

- Exact Go file splits, provided every source file stays ≤600 LOC and responsibility boundaries remain narrow.
- Exact SQL table names and enum representation, provided relational uniqueness/FK/CAS/lease constraints remain enforceable.
- Exact UI layout and glyphs, provided semantic compaction, L1 offload, and L2.5 loss are visually and accessibly distinct.
- Exact versioned evaluator/scorer implementation, provided corpus composition and numerical gates remain unchanged.
- Whether provider-native compaction is enabled for a provider, provided the capability contract and Aura checkpoint metadata are satisfied.

## Out of scope

- Deleting or rewriting canonical evidence.
- Provider-specific bypasses around Aura policy/audit/recovery.
- Injecting every memory into every prompt.
- Unbounded automatic retries.
- Invented prose descriptions for unavailable media.
- Automatic production enablement without shadow/canary approval.

## Required planning artifacts

- `42-TRACEABILITY.md`
- replacement `42-SPEC.md`
- refreshed `42-RESEARCH.md`
- refreshed `42-PATTERNS.md`
- replacement `42-VALIDATION.md`
- replacement executable `42-XX-PLAN.md` set matching the seven delivery slices
- updated ROADMAP/REQUIREMENTS references

## Success signal

The GSD plan checker reports `## VERIFICATION PASSED` with every IC requirement mapped, no legacy plan executable, dependency waves valid, explicit migration/runtime-state/rollback coverage, and runnable verification in every task.
