# Phase 25: Chat + Approval Center - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-17
**Phase:** 25-chat-approval-center
**Areas discussed:** Reasoning & tool activity, Approval center, Conversation management, Chat interaction scope + footer, Context-budget visibility (operator-added)

---

## Reasoning & tool activity (CHAT-03)

### Reasoning (CoT) web policy

| Option | Description | Selected |
|--------|-------------|----------|
| Opt-in toggle, default OFF | Keep conservative redacted default; per-session toggle surfaces CoT in a drawer | |
| Drawer ON by default | CoT streams into a collapsible drawer every turn; richest transparency, flips the hardened default | ✓ |
| OFF entirely this phase | No CoT surfacing; defer the drawer | |

**User's choice:** Drawer ON by default.
**Notes:** Justified by the whole-origin-private single-operator cockpit (Phase 24 D-03) — the only viewer is the authenticated operator. Distinct from the Phase-22 trace-persistence redaction (HARDEN-05), which stays.

### Tool-activity stream richness (before Phase 26 typed displays)

| Option | Description | Selected |
|--------|-------------|----------|
| Name + status chips | Compact name + state timeline; rich payloads wait for Phase 26 | |
| Name + collapsed raw result | Tool name + expandable raw text/JSON blob now (no typed rendering) | ✓ |
| You decide | Planner picks from assistant-ui affordances | |

**User's choice:** Name + collapsed raw result.
**Notes:** Builds a lightweight raw view that Phase 26's display router upgrades to typed displays.

---

## Approval Center (APRV-01..03)

> The operator's framing reference: **"perfectly like Claude Code."** The first AskUserQuestion (surface + verbs) was rejected in favor of conversational clarification; the operator clarified the surface should work "like Claude Code" (inline permission/clarify pattern). The design below was reflected back in prose and confirmed.

### Surface

| Option | Description | Selected |
|--------|-------------|----------|
| Header badge + slide-over | Pending-count badge + global slide-over across threads + jump-to-thread | (basis for D-04) |
| Dedicated 'Approvals' page | Full nav route | |
| Both: badge + page | Badge + dedicated page | |

**User's choice:** "perfectly like Claude Code" → **inline in-thread resolution** (Claude Code interrupt model) as the primary surface, PLUS a header badge + lightweight cross-thread list for discovery of background/scheduled-thread interrupts (Aura's multi-thread wrinkle). No heavyweight dashboard.

### Verb semantics (accept/decline/cancel → Resume[]/cancel-run)

**User's choice (terse "3" on the confirm, read as "your call" under "perfectly like Claude Code"):** Claude-Code-faithful mapping — **Answer**=resume with answer; **Deny**=resume with explicit "declined" (agent continues, told no); **Cancel/esc**=abort run + auto-resolve. Stale/auto-terminated render explicit terminal state (APRV-03).

---

## Conversation management (CHAT-02)

### Delete semantics

| Option | Description | Selected |
|--------|-------------|----------|
| Archive-first, delete behind confirm | Primary action archives (reversible); explicit 'Delete permanently' + confirm calls Store.Delete | ✓ |
| Hard delete + confirm only | Single destructive action; no archived state | |
| Archive only this phase | Defer hard delete | |

**User's choice:** Archive-first, hard-delete behind confirm.

### FTS search presentation

| Option | Description | Selected |
|--------|-------------|----------|
| Result list w/ snippets → open at match | Panel of matching turns w/ highlighted snippet; click opens thread at match | ✓ |
| Filter the sidebar list only | Search narrows the sidebar; no turn-level snippets | |
| You decide | Planner picks | |

**User's choice:** Result list with snippets → open at match.
**Notes:** Builder defaults locked without asking — inline rename, auto-title from first turn, recent-first sidebar.

---

## Chat interaction scope + footer (CHAT-01, CHAT-04)

### Chat interaction richness

| Option | Description | Selected |
|--------|-------------|----------|
| Stream + stop-a-turn | Streaming + interrupt button; no edit/branch | |
| Add edit + regenerate | Also edit last prompt + regenerate; some backend change | |
| Full assistant-ui (branch trees) | Message branching/versioning; needs conversation-tree backend | ✓ |

**User's choice:** Full assistant-ui branch trees.

### Branching sequencing (thinking-partner follow-up — scope flag)

| Option | Description | Selected |
|--------|-------------|----------|
| Build it in Phase 25 (bigger phase) | Branching lands WITH the chat lane as an explicit sub-slice; ROADMAP amendment | ✓ |
| Stream+stop+edit/regen now, branching next phase | Linear Claude-Code chat now; branching dedicated follow-up | |
| Stream+stop only now, full branching later | Pure linear streaming; both edit/regen + branching deferred | |

**User's choice:** Build it in Phase 25 (bigger phase).
**Notes:** Deliberate scope addition beyond CHAT-01 — flagged for a ROADMAP/REQUIREMENTS amendment at plan time (PRD-first). Needs tree migration + path-aware history + re-run semantics + KV-cache-invariant care.

### Cost/cache footer content

| Option | Description | Selected |
|--------|-------------|----------|
| Latest turn + session total | This turn's tokens + cache-hit % + running session cumulative | (folded) |
| Per-turn only | Latest turn only | |
| Estimated $ cost too | Also render estimated currency cost (provider pricing) | ✓ |

**User's choice:** Estimated $ cost too.
**Notes:** Feasible without a bespoke pricing table — OpenRouter returns a native `cost` field. Footer = per-turn + session tokens, cache-hit %, and est. $.

---

## Context-budget visibility (operator-added area)

> Added by the operator via the multiSelect free-text ("miss also contest windows"); disambiguated to interpretation #1 — context-budget visibility in the chat UI (not multi-window panes, not model context-size config).

### Indicator content

| Option | Description | Selected |
|--------|-------------|----------|
| Fill gauge + compaction events | Tokens-in-context vs window gauge + microcompact-fired markers | ✓ |
| Numeric tokens only | Current tokens (+ max); no compaction surfacing | |
| You decide | Planner picks detail | |

**User's choice:** Fill gauge + compaction events.

### Placement

| Option | Description | Selected |
|--------|-------------|----------|
| In the footer w/ cost+cache | One runtime instrument cluster | ✓ |
| Conversation header | Per-thread, top of chat | |
| Expandable drawer | Collapsed chip → detail panel | |

**User's choice:** In the footer alongside cost + cache (one runtime instrument cluster).

---

## Claude's Discretion

- assistant-ui runtime adapter choice (`@assistant-ui/react-ag-ui` vs external-store runtime over the SSE).
- The cross-thread pending-query shape on `askuser.Store` + its thin HTTP adapter.
- The conversation HTTP adapter surface (`/api/conversations…`) under the Phase-24 `/api/` carve-out + `RequireAuth`.
- Footer data source (turn-complete SSE signal vs REST read of persisted `cache_metrics`).
- Empty/error/loading states, mobile/responsive behavior, conversation-list nav chrome.

## Deferred Ideas

- Typed-display protocol + router → Phase 26.
- Neo4j Graph Explorer → Phase 27.
- Read-only governance boards + web onboarding → Phase 28.
- Governance WRITE surfaces (MCP config, skills install/approval queue UI) → Phase 29 (reuses this phase's `Interrupt`/`Resume[]` protocol).
- `ui_control` operator-OS shell (dock/rail/command palette) + scheduler write surfaces → follow-up milestone.
- Non-OpenRouter $ pricing table → only if a different provider becomes primary.
- **NOT deferred:** full conversation branch trees folded into Phase 25 by operator decision (D-09).
