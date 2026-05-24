---
name: to-issues
description: Use when the user wants to break an Aura PRD, phase plan, or implementation plan into independently verifiable vertical slices, Ralph-sized stories, or issue-shaped work items.
---

# To Issues

Break a plan into independently-grabbable work using vertical slices (tracer
bullets).

For Aura, this skill is a planning transformer, not an automatic issue-tracker
publisher. It writes or proposes Aura phase slices unless the current user turn
explicitly asks for GitHub issues.

## Aura Overlay

- Use `PRD.md`, `.planning/aura-deep-refactor-decisions.json`, the post-drift
  index, active phase files, and recent `scripts/ralph/progress.txt` as source
  inputs.
- Preserve or require the plan's `D:/tmp` example sweep and current 2026
  best-practice sweep. Do not emit executable slices from a plan that lacks
  those sources unless the slice explicitly records the source gap as blocked.
- Output slices that can be copied into `plan.md`, `benchmark.md`,
  `progress.md`, or a staged Ralph JSON when the user explicitly selected Ralph.
- Each slice must name affected files, canonical store, verification command,
  ground-truth benchmark, dependencies, non-goals, residue expectations, and
  the dedicated slice-QA packet plus atomic commit boundary/message.
- Do not publish to an external tracker, create branches, push, or mutate
  `scripts/ralph/prd.json` unless the current turn explicitly asks.
- Every executable Aura slice must end in one atomic local commit before the
  next slice starts. The issue/slice text should make that boundary obvious.
- Every executable Aura slice must get dedicated micro-QA. The slice text must
  name the bounded QA scope and must not prescribe a full QA sweep by default.
- If the user asks for real GitHub issues, import `github:github` after the
  Aura slice list is approved.

## Process

### 1. Gather context

Work from durable Aura source files, not only conversation context. If the user
passes an issue reference, URL, or path as an argument, fetch/read it as
evidence and translate accepted decisions into Aura planning files.

### 2. Explore the codebase (optional)

If you have not already explored the codebase, use targeted `rg`/direct reads to
understand the current state of the affected packages. Slice titles and
descriptions should use Aura's PRD/ADR vocabulary and respect decisions in the
area you're touching.

### 3. Draft vertical slices

Break the plan into **tracer bullet** slices. Each slice is a thin vertical path
through every owned integration layer end-to-end, not a horizontal layer-only
task.

Slices may be 'HITL' or 'AFK'. HITL slices require human interaction, such as an architectural decision or a design review. AFK slices can be implemented and merged without human interaction. Prefer AFK over HITL where possible.

<vertical-slice-rules>
- Each slice delivers a narrow but COMPLETE path through every layer (schema, API, UI, tests)
- A completed slice is demoable or verifiable on its own
- A completed slice has a dedicated QA PASS/HOLD packet scoped to its diff
- A completed slice is one atomic local commit
- Prefer many thin slices over few thick ones
</vertical-slice-rules>

### 4. Quiz the user

Present the proposed breakdown as a numbered list. For each slice, show:

- **Title**: short descriptive name
- **Type**: HITL / AFK
- **Blocked by**: which other slices (if any) must complete first
- **PRD/benchmark gates covered**: which gates this slice proves

Ask the user:

- Does the granularity feel right? (too coarse / too fine)
- Are the dependency relationships correct?
- Should any slices be merged or split further?
- Are the correct slices marked as HITL and AFK?

Iterate until the user approves the breakdown.

### 5. Publish or Record

For Aura default mode, record the approved slice list in the active phase
planning artifact or return it as a handoff if the user asked for read-only
planning. For explicit GitHub issue mode, publish approved issues in dependency
order after confirming the repository and labels.

Publish or record work in dependency order (blockers first) so later slices can
reference earlier slice identifiers.

<aura-slice-template>
## Slice

Short vertical-slice title.

## What must be true

End-to-end behavior or planning outcome, stated from the PRD gate backward.

## Scope

- Affected files/packages
- Canonical store or source of truth
- `D:/tmp` examples inspected
- 2026 best-practice sources inspected
- Non-goals

## Verification

- Baseline command
- Post-edit command
- Ground-truth benchmark row or probe
- Dedicated slice-QA scope and reviewer mode
- Residue expectation
- Atomic commit message

## Dependencies

Blocking slices or "None - can start immediately".
</aura-slice-template>

Use the issue template below only when the current user turn explicitly asks for
GitHub issues.

<issue-template>
## Parent

A reference to the parent GitHub issue in explicit issue mode. If the source was
not an existing issue, omit this section.

## What to build

A concise description of this vertical slice. Describe the end-to-end behavior, not layer-by-layer implementation.

Avoid specific file paths or code snippets — they go stale fast. Exception: if a prototype produced a snippet that encodes a decision more precisely than prose can (state machine, reducer, schema, type shape), inline it here and note briefly that it came from a prototype. Trim to the decision-rich parts — not a working demo, just the important bits.

## Acceptance criteria

- [ ] Criterion 1
- [ ] Criterion 2
- [ ] Criterion 3

## Blocked by

- A reference to the blocking ticket (if any)

Or "None - can start immediately" if no blockers.

</issue-template>

Do NOT close or modify any parent issue. In Aura default mode, do not touch an
external tracker at all.
