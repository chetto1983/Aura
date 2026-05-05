# v1.2 Daily Memory Quality Design

Date: 2026-05-05
Status: approved for planning
Source of truth: `.planning/PROJECT.md`, `.planning/codebase/CONCERNS.md`, `docs/llm-wiki.md`

## Purpose

v1.2 makes Aura's memory feel sharper in daily use. After v1.0 production readiness and v1.1 hardening, the next bottleneck is not whether Aura runs safely; it is whether Aura remembers the right things, retrieves the right evidence, and proposes useful wiki updates without creating noise.

The milestone is evaluation-led. Aura should gain a repeatable local memory scorecard first, then use that scorecard to guide retrieval and wiki proposal improvements. The goal is measurable memory quality, not a broad feature expansion.

## Milestone Boundary

v1.2 is in scope when the work improves how Aura answers from memory or proposes durable wiki updates.

In scope:

- create a local memory quality scorecard with realistic daily-use prompts;
- score answer correctness, evidence grounding, stale-fact handling, and proposal quality;
- improve retrieval context packing across wiki, archive, source, and proposal evidence;
- improve wiki proposal generation so proposals are deduplicated, source-backed, and reviewable;
- add focused tests and fixtures for scorecard behavior;
- close the milestone only after the scorecard reaches the agreed pass threshold.

Out of scope:

- UI redesign or dashboard layout work;
- replacing `chromem-go`, SQLite, or the embedding stack;
- broad package splitting unrelated to memory quality;
- settings encryption or security hardening unrelated to memory behavior;
- changing the core wiki philosophy from reviewed durable updates to automatic mutation.

## Requirements

### EVAL-01: Memory Quality Scorecard

Aura must have a repeatable local scorecard for daily-memory behavior.

Acceptance:

- scorecard cases live in versioned fixtures, not ad hoc manual notes;
- cases cover project-status recall, decision recall, next-action recall, stale-fact resistance, and evidence use;
- each case has an expected behavior and a small rubric that can be evaluated without live Telegram;
- the scorecard command produces a compact pass/fail summary suitable for release gates.

### RET-01: Evidence Retrieval Reliability

Aura must retrieve and pack the most relevant memory evidence before answering memory-heavy prompts.

Acceptance:

- retrieval includes wiki pages, conversation archive evidence, source summaries, and pending proposal context where available;
- context packing prefers current, source-backed facts over older duplicate facts;
- retrieval output preserves enough source metadata for the answer or proposal to cite evidence;
- tests cover at least one stale-vs-current fact case and one cross-source synthesis case.

### PROP-01: Wiki Proposal Deduplication

Wiki proposals must avoid creating duplicate or near-duplicate review items.

Acceptance:

- new proposals are compared with existing pending proposals and relevant wiki pages;
- duplicate proposals are skipped or merged into an existing pending proposal with an explicit note;
- tests cover duplicate proposal suppression and non-duplicate proposal preservation.

### PROP-02: Source-Backed Proposal Text

Wiki proposals must be reviewable from their own text.

Acceptance:

- proposal text states what should change, why, and which evidence supports it;
- proposals avoid vague summaries such as "update the wiki" without concrete target content;
- stale or conflicting facts are flagged instead of silently overwritten;
- tests cover proposal text for an accepted new fact and a conflict case.

### REL-03: v1.2 Memory Release Gate

v1.2 closes only when the scorecard and core tests pass.

Acceptance:

- focused memory scorecard reaches the agreed pass threshold;
- focused package tests for retrieval/proposal code pass;
- broad Go verification passes;
- `.planning/` and `docs/implementation-tracker.md` record the scorecard result and remaining known gaps.

## Architecture

The milestone should add a small evaluation layer before changing core behavior.

### Scorecard Package

Add or extend a test/debug surface that can run deterministic memory cases against local fixtures. The scorecard should avoid live LLM dependence at first when possible by testing prompt construction, retrieval selection, proposal shaping, and expected evidence selection. If a live model path is needed later, it should be optional and clearly marked.

The scorecard should output:

- case name;
- requirement area;
- pass/fail;
- short failure reason;
- aggregate threshold result.

### Retrieval Improvements

Retrieval changes should stay near existing memory/search boundaries:

- `internal/tools/memory_search.go` for tool-facing memory search behavior;
- `internal/search` for ranking and evidence search helpers;
- `internal/conversation` for context packing and prompt envelopes;
- `internal/wiki` and proposal stores only where evidence metadata is needed.

The design should prefer explicit ranking signals over hidden prompt tricks. Useful signals include source type, recency, wiki link proximity, exact slug/title match, and whether the fact already exists in a reviewed wiki page.

### Proposal Improvements

Proposal logic should remain review-gated. v1.2 can make proposals smarter, but it should not silently mutate durable wiki content.

Proposal generation should produce structured internal data before final text where practical:

- target slug or suggested slug;
- proposed operation: create, update, merge, or conflict;
- evidence snippets or references;
- duplicate/conflict status;
- final reviewer-facing text.

This keeps dedupe and conflict handling testable without relying only on natural-language matching.

## Data Flow

1. A memory-heavy prompt or maintenance job asks for recall, synthesis, or a wiki update.
2. Retrieval gathers candidates from wiki, archive, sources, and pending proposals.
3. Ranking chooses current and source-backed evidence first.
4. The answer path receives compact evidence with references.
5. The proposal path checks existing wiki/pending proposals before creating new review work.
6. The scorecard runs the same retrieval/proposal paths against fixtures and records whether the expected evidence and behavior appear.

## Error Handling

Memory quality failures should be diagnosable but not user-hostile.

- Missing evidence should produce an "insufficient evidence" outcome rather than confident fabrication.
- Retrieval backend errors should be logged and surfaced in scorecard output.
- Proposal dedupe errors should fail closed by avoiding automatic durable mutation; creating a clearly marked review item is safer than silent wiki writes.
- Scorecard failures should name the case and missing expectation.

## Testing Strategy

Testing should start narrow and become the release gate.

Focused tests:

- scorecard fixture loading and threshold calculation;
- retrieval ranking for stale/current facts;
- retrieval packing for multi-source evidence;
- duplicate proposal suppression;
- conflict proposal text.

Release verification:

- run the memory scorecard;
- run focused memory/retrieval/proposal package tests;
- run the broad Go verifier.

Manual verification:

- optional live Telegram smoke with three daily-memory prompts after automated checks pass;
- review a sample generated proposal for clarity and evidence grounding.

## Success Criteria

v1.2 is complete when:

- Aura has a repeatable memory quality scorecard;
- the scorecard covers at least five realistic daily-use memory cases;
- retrieval improvements pass stale/current and cross-source evidence tests;
- wiki proposals are deduplicated and source-backed in tests;
- the scorecard reaches the agreed pass threshold;
- core Go verification remains green.

## Deferred

- Dashboard redesign for proposal review;
- automatic wiki mutation without human review;
- vector-store replacement;
- broad `internal/tools` package split;
- arbitrary package-wide coverage targets.

## Scorecard Threshold

The first v1.2 release threshold is fixed at planning start: all fixture-loading and deterministic retrieval/proposal tests must pass, and at least 4 of 5 daily-memory cases must pass the scorecard before closing v1.2.
