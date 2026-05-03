# Aura Implementation Loop

This is a project-local Ralph-style loop package for implementing Aura slices.

It follows the Ralph Loop idea: each implementation iteration starts from fresh context, reads durable handoff files, runs deterministic verification, and repeats until the slice is genuinely complete.

The package is intentionally plain files:

- `RALPH.md` is the loop entrypoint.
- `scripts/status.ps1` reports the working tree without staging anything.
- `scripts/verify-go.ps1` runs Aura's Go verification stack.
- `scripts/verify-web.ps1` runs frontend verification when needed.

Use it with any runtime that can read a Ralph-style `RALPH.md`, or manually as the checklist for Codex/Claude/goose.

Typical goal:

```text
Implement search_memory unified evidence search across wiki, sources, and archive. Keep writes review-gated. Add natural prompt E2E.
```

The loop is not a replacement for human product judgment. It is a guardrail for long implementation runs: small slice, fresh context, deterministic checks, explicit commit.
