---
status: testing
phase: 37D-composer-skill-picker
source: [37D-VERIFICATION.md]
started: 2026-07-10T07:35:00Z
updated: 2026-07-10T07:35:00Z
---

## Current Test

number: 1
name: Visual parity of the "/" picker against the Claude reference
expected: |
  The rendered picker (menu-above-input, grouped Quick-commands/Skills sections,
  icon+name+subtitle rows, removable pinned pill) matches the intended look-and-feel
  (spacing, grouping, icon choice, pill styling) cited in 37D-DISCUSSION-LOG.md — a
  design-fidelity judgment, not a functional one.
awaiting: user response

## Tests

### 1. Visual parity of the "/" picker against the Claude reference
expected: The rendered picker matches the intended look-and-feel (spacing, grouping, icon choice, pill styling) against the Claude reference screenshot cited in 37D-DISCUSSION-LOG.md. Pixel/layout fidelity is subjective; unit + golden-replay e2e assert DOM roles/attributes/counts, not visual appearance (also flagged manual-only in 37D-VALIDATION.md).
result: [pending]

### 2. Live-LLM sanity check — Mechanism A actually shapes the reply
expected: Drive one real (non-mocked) turn against a live LLM backend — type "/", pick a real installed skill, send a message, and confirm the model's reply is visibly influenced by the pinned skill's instructions (the useAuthorityFrame + body prepend changes model behavior, not just the captured HTTP body). All automated proof uses golden-replay runners; low risk (Mechanism A reuses a shipped runtime contract) but real-time/external behavior outside static verification's reach.
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
