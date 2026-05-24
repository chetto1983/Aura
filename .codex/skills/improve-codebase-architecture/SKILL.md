---
name: improve-codebase-architecture
description: Use when the user wants to improve Aura architecture, plan a critical refactor, find module ownership problems, compare architecture alternatives, or make a codebase slice more testable and AI-navigable.
---

# Improve Codebase Architecture

## Aura Overlay

When this skill is used inside `D:/Aura`, Aura's repo rules override the generic
process below:

- Read the same minimum route as `$aura-implementation-loop`: `AGENTS.md`,
  `CLAUDE.md`, `PRD.md`, `.planning/aura-deep-refactor-decisions.json`, the
  post-drift index, recent `scripts/ralph/progress.txt`, and only the selected
  phase/source files needed for the slice.
- Treat `PRD.md` section 9 dependency rules as Aura's architecture map. Domain
  policy must not import volatile delivery, storage, provider, UI, Telegram,
  cron, Qdrant, SQLite row, or filesystem concerns directly.
- Use `CONTEXT.md` or `docs/adr/` only if they actually exist in this repo.
  Otherwise use `PRD.md`, the ADR JSON, phase files, package names, and local
  tests as the vocabulary and decision source.
- Before proposing alternatives, inspect concrete examples under `D:/tmp` and
  current 2026 best-practice sources. Record exact example paths, source URLs or
  doc names, adopted patterns, and rejected patterns. If none are available,
  mark the gap explicitly instead of omitting the source step.
- Produce 3-4 alternatives for critical phases, then a short RFC: chosen module
  shape, rejected alternatives, source files, verification, remaining risk, and
  the phase file that will own the decision.
- Record adopted and rejected ideas in the active Aura `source.md`, `plan.md`,
  `benchmark.md`, `progress.md`, or ADR JSON before implementation relies on
  them.
- Do not create branches, commits, pushes, PRs, issue tracker entries, or new
  canonical planning routes from this skill.
- If subagent tools are unavailable, do targeted `rg`/direct reads and label the
  result `self-audited` rather than pretending independent review happened.

Surface architectural friction and propose **deepening opportunities** — refactors that turn shallow modules into deep ones. The aim is testability and AI-navigability.

## Glossary

Use these terms exactly in every suggestion. Consistent language is the point — don't drift into "component," "service," "API," or "boundary." Full definitions in [LANGUAGE.md](LANGUAGE.md).

- **Module** — anything with an interface and an implementation (function, class, package, slice).
- **Interface** — everything a caller must know to use the module: types, invariants, error modes, ordering, config. Not just the type signature.
- **Implementation** — the code inside.
- **Depth** — leverage at the interface: a lot of behaviour behind a small interface. **Deep** = high leverage. **Shallow** = interface nearly as complex as the implementation.
- **Seam** — where an interface lives; a place behaviour can be altered without editing in place. (Use this, not "boundary.")
- **Adapter** — a concrete thing satisfying an interface at a seam.
- **Leverage** — what callers get from depth.
- **Locality** — what maintainers get from depth: change, bugs, knowledge concentrated in one place.

Key principles (see [LANGUAGE.md](LANGUAGE.md) for the full list):

- **Deletion test**: imagine deleting the module. If complexity vanishes, it was a pass-through. If complexity reappears across N callers, it was earning its keep.
- **The interface is the test surface.**
- **One adapter = hypothetical seam. Two adapters = real seam.**

This skill is _informed_ by the project's domain model. The domain language gives names to good seams; ADRs record decisions the skill should not re-litigate.

## Process

### 1. Explore

Read the project's domain glossary and any ADRs in the area you're touching first.

Then use available exploration helpers or direct `rg`/file reads to walk the
codebase. Don't follow rigid heuristics - explore organically and note where
you experience friction:

- Where does understanding one concept require bouncing between many small modules?
- Where are modules **shallow** — interface nearly as complex as the implementation?
- Where have pure functions been extracted just for testability, but the real bugs hide in how they're called (no **locality**)?
- Where do tightly-coupled modules leak across their seams?
- Which parts of the codebase are untested, or hard to test through their current interface?

Apply the **deletion test** to anything you suspect is shallow: would deleting it concentrate complexity, or just move it? A "yes, concentrates" is the signal you want.

### 2. Present candidates

Present a numbered list of deepening opportunities. For each candidate:

- **Files** — which files/modules are involved
- **Problem** — why the current architecture is causing friction
- **Solution** — plain English description of what would change
- **Benefits** — explained in terms of locality and leverage, and also in how tests would improve

For Aura, use `PRD.md`, ADR JSON, phase files, package names, and tests for
domain vocabulary. For non-Aura projects that provide `CONTEXT.md`, use that
domain vocabulary plus [LANGUAGE.md](LANGUAGE.md) vocabulary for architecture.
If `CONTEXT.md` defines "Order," talk about "the Order intake module" - not
"the FooBarHandler," and not "the Order service."

**ADR conflicts**: if a candidate contradicts an existing ADR, only surface it when the friction is real enough to warrant revisiting the ADR. Mark it clearly (e.g. _"contradicts ADR-0007 — but worth reopening because…"_). Don't list every theoretical refactor an ADR forbids.

For Aura critical phases, do propose the candidate module shapes as alternatives
plus a recommended choice, because `$aura-plan-builder` needs a lockable RFC.
For exploratory non-Aura use, ask the user: "Which of these would you like to
explore?"

### 3. Grilling loop

Once the user picks a candidate, drop into a grilling conversation. Walk the design tree with them — constraints, dependencies, the shape of the deepened module, what sits behind the seam, what tests survive.

Side effects happen inline as decisions crystallize. In Aura, side effects must
go through Aura planning files or the ADR JSON; do not create or update
`CONTEXT.md` unless the user explicitly selects a non-Aura workflow.

- **Naming a deepened module after a concept not recorded anywhere?** In Aura,
  propose the target phase file or ADR JSON entry that should own the term. In
  non-Aura projects with `CONTEXT.md`, add the term there.
- **Sharpening a fuzzy term during the conversation?** In Aura, record it in the
  active phase files or ADR JSON only after the decision is accepted. In non-Aura
  projects with `CONTEXT.md`, update `CONTEXT.md`.
- **User rejects the candidate with a load-bearing reason?** Offer an ADR, framed as: _"Want me to record this as an ADR so future architecture reviews don't re-suggest it?"_ Only offer when the reason would actually be needed by a future explorer to avoid re-suggesting the same thing — skip ephemeral reasons ("not worth it right now") and self-evident ones. See [ADR-FORMAT.md](../grill-with-docs/ADR-FORMAT.md).
- **Want to explore alternative interfaces for the deepened module?** See [INTERFACE-DESIGN.md](INTERFACE-DESIGN.md).
