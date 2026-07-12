# Phase 42: LLM Conversation Compaction - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-12
**Phase:** 42-llm-conversation-compaction
**Areas discussed:** Summary-turn role/placement, Minimum-compact floor, Compaction model default, Prompt schema (Req#2 refinement), PRD/doc activation note, Web cockpit frontend (mid-discussion scope addition)

Requirements locked by `42-SPEC.md` (11 reqs, ambiguity 0.14). Discussion covered implementation decisions only. Operator additionally directed external parity research: `docs/compact_prompt.md`, Claude/GPT system prompts under `D:\tmp`, and online context-rot patterns — findings folded into the decisions below.

---

## Summary-turn role & placement

| Option | Description | Selected |
|--------|-------------|----------|
| role=user + marker @ messages[2] | role='user' with `__aura_compaction_summary__` in ToolCallID (identical to proven `alwaysBlockMarker`), inserted after L0 + always-block; keeps messages[0] byte-identical, reuses AppendTurn + protection pattern | ✓ |
| Dedicated synthetic role | New/synthetic role for the summary | |

**User's choice:** role=user + marker @ messages[2].
**Notes:** Rejected synthetic role — provider role-ordering/alternation risk, no existing protection helper to reuse, more KV-cache/wire-format surface, no upside over the marker trick. Confirmed in code: `context.go:48` `alwaysBlockMarker`, `:303` `isAlwaysBlock`.

---

## Minimum-compact floor

| Option | Description | Selected |
|--------|-------------|----------|
| Derived floor, no new knob | No-op if body turns ≤ ~3 OR input tokens < 2× AURA_COMPACT_MAX_OUTPUT_TOKENS; derived from existing values | ✓ |
| Hardcoded constants | Fixed literals (e.g. < 4 turns / < 4000 tokens) | |
| New env knob (AURA_COMPACT_MIN_TOKENS) | Operator-configurable floor | |

**User's choice:** Derived floor, no new knob.
**Notes:** Keeps config surface at the 4 knobs SPEC Req#9 locks (no sprawl). Industrial trigger range 5–20k tokens grounded the sizing. Exact constants left to planner/executor within the locked shape.

---

## Compaction model default

| Option | Description | Selected |
|--------|-------------|----------|
| '' → same model as conversation | Best fidelity; compaction is rare + load-bearing; AURA_COMPACT_MODEL is the cost escape hatch | ✓ |
| Ship a cheaper model default | Lower per-compaction cost | |

**User's choice:** '' → same model as conversation.
**Notes:** Research flags summarization *fidelity* as the dominant failure mode; a weak summary poisons every subsequent turn. Aura's cheap routine tier is L1 masking, not the compaction call. Operators wanting cheap can still set the knob.

---

## Prompt schema (SPEC Req#2 refinement)

| Option | Description | Selected |
|--------|-------------|----------|
| Adopt newer 9-section hardening | Keep 7 sections + add "All user messages" + "Errors and fixes" + verbatim security-constraint preservation + "text only, no tools" guard | ✓ |
| Follow SPEC as written (7-section) | Adapt exactly the older 7-section docs/compact_prompt.md | |

**User's choice:** Adopt newer 9-section hardening.
**Notes:** Parity finding — `docs/compact_prompt.md` is the OLDER Claude Code prompt; the newer leaked `compact.md` skill (`D:\tmp\...\bundled-skills\compact.md`) is 9-section with governance-decay hardening. Still an *adaptation* under Req#2 (not a new capability). Directly counters the arxiv "Governance Decay" failure mode. Planner must extend the Req#2 header-assertion test from 7 to 9 headers + English-only clause + no-tools guard.

---

## PRD/doc activation note

| Option | Description | Selected |
|--------|-------------|----------|
| Fold the doc note into this phase | One-line "activated in Phase 42" on 04-SPEC L3 deferral + PRD §1.8 OQ#3; documentation only, no PRD-amendment | ✓ |
| Separate housekeeping, out of scope | Leave notes as-is, track elsewhere | |

**User's choice:** Fold the doc note into this phase.
**Notes:** Confirmed NOT a PRD deviation — activates a documented deferral. No PRD-amendment commit. Prevents truth-source drift (PRD/04-SPEC still asserting "L3 NOT implemented" after ship).

## Web cockpit frontend (mid-discussion scope addition)

Operator directed mid-discussion: "we must do also on frontend UI" + "search openwebui ... clone on d:/tmp". Both selected surfaces were SPEC-out-of-scope → folded in via SPEC boundary amendment (see 42-SPEC.md §Amendment 2026-07-12).

| Option | Description | Selected |
|--------|-------------|----------|
| Web manual /compact trigger | /compact QuickCommand in composer SkillPicker + new AG-UI POST route → token delta; web = 4th trigger surface | ✓ |
| Compaction markers on ContextBudgetGauge | GET /compactions (ListCompactions wrapper) + markers on existing gauge, sibling to rot-events markers | ✓ |

**User's choice:** Both.
**Notes:** Grounded in shipped patterns — `handleConversationRotEvents`/`ContextBudgetGauge` (read-path template) + composer `QuickCommand`/`SkillPicker` (trigger template). open-webui 0.10.2 reference: its per-model "Context Compaction Threshold" (`AdvancedParams.svelte`) is the proactive-threshold model = Aura's deferred L2.4 tier (not Phase 42); its command-palette-in-composer pattern is already matched by Aura's SkillPicker. New UI must follow CLAUDE.md §Frontend_aesthetics and keep the compaction marker visually distinct from rot markers (D-10). Scope expansion is operator-owned; SPEC amended rather than re-spec'd because it adds no new architecture.

## Claude's Discretion

- Exact numeric constants inside the derived compact floor (the "~3 turns" and "2×" multiplier) — tune within the D-04 rationale; the shape is locked.
- Whether checkpoint reconstruction stays in `context.go` or splits into `context_compaction.go` — refactor-on-touch / ≤600 LOC governs.

## Deferred Ideas

- Pre-L2.5 "L2.4" proactive auto-compaction tier — SPEC out-of-scope; revisitable follow-on.
- Web cockpit context-budget gauge marking compaction events — separate frontend phase (`ListCompactions` ships the data).
- Neo4j long-term memory spill of summarized rounds — Phase 15 territory.
- "Observation masking halves cost vs summarization" research finding — Aura already has it as L1; noted as validation, no action.
