# Phase 37D: Composer Skill & Command Picker - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-09
**Phase:** 37D-composer-skill-picker
**Areas discussed:** Selection semantics, List source & scoping, Menu scope, Trigger & UX model (+ user-requested research of Claude & industrial patterns)

---

## Pre-discussion research (user-requested)

User asked to research Claude's composer/skill-picker design + industrial `/`-menu patterns, and provided a **reference screenshot** of Claude's own picker (menu above input, "Digita per filtrare" filter field, icon+name+subtitle rows, "Productivity" category header grouping skills, `add-files` action listed alongside skills).

Findings folded into decisions:
- Claude picker: `/` menu above input, type-to-filter, category-grouped rows, mixes skills + quick actions.
- Notion/Slack/Linear: `/` at line-start opens a searchable, category-grouped, keyboard-first command list (de-facto standard).
- W3C APG: correct primitive = combobox + listbox (textarea keeps focus, `aria-expanded`/`aria-controls`/`aria-activedescendant`, ↑/↓/Enter/Esc/typeahead, scroll active option into view).

---

## Selection semantics

| Option | Description | Selected |
|--------|-------------|----------|
| Invoke as explicit tool | Selecting deterministically makes the agent's first action `skill action=use name=X`; the skill IS applied. | ✓ |
| Attach as turn context | Selecting injects a soft mention; the LLM decides whether to use the skill. | |
| Insert editable /name chip | Selecting inserts a removable `/skill-name` chip translated to invoke on send. | |

**User's choice:** Invoke as explicit tool.
**Notes:** Deterministic firing over the existing `skill action=use` runtime contract; planner must carry the pinned skill from composer → run request.

---

## Menu scope (quick actions)

| Option | Description | Selected |
|--------|-------------|----------|
| Skills + add-files only | Skills grouped by category + reuse existing add-files (Paperclip) action; defer new-chat/clear. | |
| Skills + add-files + new-chat/clear | Full Claude command set; new-chat/clear are net-new client actions. | ✓ |
| Skills only | List/filter/inject skills, no quick actions. | |

**User's choice:** Skills + add-files + new-chat/clear.
**Notes:** Matches the reference screenshot (add-files shown in menu). new-chat/clear do not exist in the web yet — net-new client-side UI actions.

---

## List source & scoping (source)

| Option | Description | Selected |
|--------|-------------|----------|
| New per-identity endpoint | Lean GET behind plain RequireAuth returning active skills to any authenticated identity. | ✓ |
| Reuse governance endpoint | Point picker at GET /api/governance/skills (governance.read-gated → 403 for non-admins). | |
| Relax gate on governance endpoint | Drop governance.read on the skills GET; widens admin-board read to all auth users. | |

**User's choice:** New per-identity endpoint.
**Notes:** The governance skills endpoint is `governance.read`-capability-gated, so ordinary identities get 403 — reusing it would break the picker for its target users.

---

## List source & scoping (global vs per-identity filtering)

| Option | Description | Selected |
|--------|-------------|----------|
| Global list, RequireAuth | Loader is process-global; return the global active-skills snapshot to any logged-in user. | |
| Per-identity filtering | Endpoint filters to what THIS identity may use (MUSR isolation). | |
| You decide (verify first) | Researcher confirms whether per-identity skill scoping exists, then pick. | ✓ |

**User's choice:** You decide (verify first) — evidence-gated (D-04).
**Notes:** Default to global snapshot behind RequireAuth if no per-identity scoping exists; filter if it does.

---

## Trigger & UX — trigger

| Option | Description | Selected |
|--------|-------------|----------|
| Only at start of empty input | `/` opens the menu only as the first char of an empty composer (matches reference). | ✓ |
| Anywhere after whitespace | `/` opens at line-start or after a space (Notion-style inline). | |

**User's choice:** Only at start of empty input.
**Notes:** Filter text = whatever follows the leading `/`.

---

## Trigger & UX — pinned skill affordance

| Option | Description | Selected |
|--------|-------------|----------|
| Removable pill above input | Adds a removable pill above the textarea (mirrors attachment-chip pattern); on send the turn carries skill=name. | ✓ |
| Fires immediately on select | Selecting sends the turn right away with that skill, no message. | |
| Inserts editable /name token | Inserts a `/name` token parsed to invoke on send (hybrid). | |

**User's choice:** Removable pill above input.
**Notes:** Keeps edit-before-send; reuses the existing AttachmentChip render pattern in Composer.tsx.

---

## Locked by reference + research (not separately voted)

- Menu above input; "Type to filter" field; rows = icon + name + one-line subtitle; category grouping with headers.
- W3C APG combobox+listbox a11y (textarea keeps focus, aria-activedescendant, ↑/↓/Enter/Esc/typeahead, scroll active into view).
- Degrade to no-op when list empty/unreachable; preserve Composer paste/drop/Enter-send.
- i18n en+it parity; web coverage ≥85%; unit React + Playwright e2e.
- PRD-first amendment gate (WEBSKILL-01..03 + surface + new endpoint) before any code.

## Claude's Discretion

- Endpoint path/name (`/api/composer/skills` vs `/api/skills`).
- Skill category grouping taxonomy (by Type / frontmatter / flat) — from available loader metadata.
- Whether new-chat/clear sit in the same list or a small "commands" group.
- D-04 global-vs-filtered scoping resolved by researcher evidence.

## Deferred Ideas

- Cmd+K global command palette — own phase.
- Building per-identity skill grants/scoping as a new capability — own phase (37D uses global snapshot if none exists).
- Conversation/artifact sharing — already Phase 37F.
