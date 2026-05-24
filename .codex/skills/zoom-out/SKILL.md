---
name: zoom-out
description: Use when Aura work is losing the current slice, module map, caller map, source-of-truth route, or broader architecture context.
disable-model-invocation: true
---

# Zoom Out

Inside `D:/Aura`, rebuild only the context needed to keep the slice safe:

- active user goal and bounded slice;
- source-of-truth route: `AGENTS.md`, `CLAUDE.md`, `PRD.md`, ADR JSON,
  post-drift index, recent `scripts/ralph/progress.txt`, and selected phase
  files;
- affected modules, callers, entrypoints, tests, and canonical stores;
- constraints from PRD section 9 and the selected phase `benchmark.md`;
- unrelated dirty files that must be preserved;
- next exact command or file to inspect.

Do not edit files. Do not create new planning state. Do not read the whole repo
to feel safer; use `rg` and direct reads until the map is good enough to resume
`$aura-implementation-loop`.
