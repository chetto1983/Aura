---
status: passed
phase: 37D-composer-skill-picker
source: [37D-VERIFICATION.md]
started: 2026-07-10T07:35:00Z
updated: 2026-07-10T06:05:00Z
---

## Current Test

number: -
name: All tests complete
expected: |
  Both manual-only checks confirmed PASS by the operator on the live :9080
  deployment (rebaked aura:local image embedding the 37D dist).
awaiting: none

## Tests

### 1. Visual parity of the "/" picker against the Claude reference
expected: The rendered picker matches the intended look-and-feel (spacing, grouping, icon choice, pill styling) against the Claude reference. Pixel/layout fidelity is subjective; unit + golden-replay e2e assert DOM roles/attributes/counts, not visual appearance.
result: passed — operator confirmed on live :9080 (2026-07-10); the "/" menu opens with grouped Quick-commands/Skills, icon+name+subtitle rows, filter, and the removable pill render as intended.

### 2. Live-LLM sanity check — Mechanism A actually shapes the reply
expected: A real (non-mocked) turn against the live LLM — type "/", pick a real installed skill, send — the model's reply is visibly influenced by the pinned skill's instructions (useAuthorityFrame + body prepend changes behavior, not just the wire body).
result: passed — operator confirmed on live :9080 (2026-07-10); pinning a skill and sending produced a reply reflecting the skill's instructions. Enter-to-send / paste / drop unaffected.

## Summary

total: 2
passed: 2
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

None — both manual-only items passed; all 28 automated must-haves already VERIFIED (37D-VERIFICATION.md).
