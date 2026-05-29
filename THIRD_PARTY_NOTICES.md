# Third-Party Notices

This repository adapts a small number of open-source implementation patterns. Keep this file in sync when adapted source code is added, removed, or materially changed.

## google/adk-go

- Source: `https://github.com/google/adk-go`
- License: Apache License 2.0
- Use in Aura: Phase 2 adapts workflow-agent control-flow patterns for `SequentialAgent`, `LoopAgent`, and `ParallelAgent` while not importing `google.golang.org/adk`.
- Planned adapted files:
  - `internal/agent/workflow/sequential.go`
  - `internal/agent/workflow/loop.go`
  - `internal/agent/workflow/parallel.go`
- Required hygiene:
  - Preserve an in-source attribution comment in adapted workflow files.
  - Do not copy upstream NOTICE text selectively; if a relevant upstream NOTICE entry appears, carry the applicable notice here.
  - Keep Aura-specific divergences documented in code comments where they affect behavior: budget exhaustion, shared parent budget, two-phase dedup, captured cancel for escalation, and clean sibling drain.
