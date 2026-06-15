---
status: complete
phase: 09-swarm-minimal
source: [09-VERIFICATION.md]
started: 2026-06-04T12:00:00Z
updated: 2026-06-04T13:10:00Z
---

## Current Test

[complete]

## Tests

### 1. SC#5 — Live dual-gate cot_eval swarm E2E (mail + WhatsApp MCP read-back + judge ≥90%)

expected: |
  Operator brings up the whatsmeow bridge (fork chetto1983/whatsapp-mcp @ 6de1dcd),
  health-checks REST :8080, sources .env, then runs:

      go test -tags cot_eval -run TestSwarmE2E -timeout 600s -v ./internal/eval/

  Hard floor (all must hold): ≥2 workers spawned via tool_use on a NATURAL prompt
  (no "swarm" word), expected facts present, self-mail + self-WhatsApp read-back via
  the same mounted MCP servers, fan-out wall-clock < 1.5× single-worker baseline.
  Judge rubric ≥90% + control prompt does NOT over-spawn.
result: |
  PASS (2026-06-04, live run 8 of 8). workers=2 (w1 11.6s ‖ w2 15.9s overlapped),
  facts present, mail read-back found, WhatsApp read-back found, fan-out 15 877ms vs
  baseline 12 200ms = 1.30× (< 1.5), e2e turn 27 833ms advisory, judge mean 1.00
  (5/5 × 4 dimensions), control: 0 workers + no_over_spawn 5/5. Scored report:
  docs/aura-swarm-eval-2026-06-04.md; numbers recorded in docs/aura-quality-snapshot.md.
  7 earlier runs surfaced and fixed: unbounded mcp.Client.Close wait (5s kill-timeout),
  judge MaxTokens reasoning starvation (256→2048), disconnected whatsmeow session
  (bridge restart + log), swarm_spawn stub without trigger/call-shape, empty-goals
  error that steered away instead of teaching the arg shape, per-worker MCP
  arg-discovery round-trip (bridged stubs now carry "Required args: …").

## Summary

total: 1
passed: 1
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
