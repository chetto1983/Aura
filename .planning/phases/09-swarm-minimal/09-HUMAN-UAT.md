---
status: partial
phase: 09-swarm-minimal
source: [09-VERIFICATION.md]
started: 2026-06-04T12:00:00Z
updated: 2026-06-04T12:00:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. SC#5 — Live dual-gate cot_eval swarm E2E (mail + WhatsApp MCP read-back + judge ≥90%)

expected: |
  Operator brings up the whatsmeow bridge (fork chetto1983/whatsapp-mcp @ 6de1dcd),
  health-checks REST :8080 (405 on GET /api/send is the alive signal), sources .env, then runs:

      go test -tags cot_eval -run TestSwarmE2E -timeout 600s -v ./internal/eval/

  Hard floor (all must hold): ≥2 workers spawned via tool_use on a NATURAL prompt
  (no "swarm" word), expected facts present in the aggregated answer, self-mail +
  self-WhatsApp messages exist on read-back via the same mounted MCP servers
  (WhatsApp JID duality: bridge-sent rows under <phone>@s.whatsapp.net), wall-clock
  < 1.5× the single-worker baseline. Judge rubric ≥90% equal-weight average across
  4 dimensions + control prompt does NOT over-spawn.

  Then fill the TBD row in docs/aura-quality-snapshot.md with the recorded numbers.
result: [pending]

## Summary

total: 1
passed: 0
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps
