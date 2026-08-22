---
phase: 46-mcp-trust-and-facade
plan: 02
subsystem: docs
tags: [mcp, design-doc, curated-surface, whatsapp, calendar, mcp-apps-views]

# Dependency graph
requires:
  - phase: 46-mcp-trust-and-facade (plan 01)
    provides: prd.md Amendments #122/#123/#124 — MCP-04/05's fork-side curation mechanism, TOOL-14's frequency+count-budget tiering axis, unblocking every code plan in this phase
provides:
  - "docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md — the in-repo contract both fork commits (46-05..46-08) implement: calendar's 14-action curated tool, WhatsApp's 12-action curated tool + 2-tool exemption, the flat-union schema shape, MCP-05's accountId fix, the three-key-space trustedRecipeActions description, and the mount-time reconciliation predicate"
  - "The operator's resolution of the WhatsApp views-vs-merge conflict the 2026-08-22 halt surfaced: views-exempt (WhatsApp ends at 3 model-facing tools, not 1) and name-keep-d18 (calendar__calendar, whatsapp__messages)"
affects: [46-03-roadmap-worked-example, 46-05, 46-06, 46-07, 46-08, 46-09]

actuals:
  tokens: 8740
  tasks: 2
  commits: 1

tech-stack:
  added: []
  patterns:
    - "Design-doc-before-fork-commit sequencing (D-25): the contract lands in Aura's tree first, then one plan per fork implements it, joined by the immutable image pin (D-32)"
    - "Ten-section design-doc shape (2026-06-16 precedent) extended to cover two forks' curated surfaces in one document, since both are governed by the same D-27 count rule at the same time"

key-files:
  created:
    - docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md
  modified: []

key-decisions:
  - "Task 1(A) SELECTED: views-exempt — list_chats and list_messages are exempted from WhatsApp's merge (kept raw, advertised, non-Mutating); WhatsApp ends the phase at 3 model-facing tools (1 curated + 2 raw), not 1. Rejected: views-drop (curate all 14, let the two live MCP Apps views go inert — a 403 on the callback path and a not-found once the raw names are deleted)."
  - "Task 1(B) SELECTED: name-keep-d18 — model-facing calendar__calendar and whatsapp__messages, derived from catalog.go's mount namespaces (calendar, whatsapp) through name.go's namespacedName. Rejected: name-pim (calendar__pim — reads better but diverges from D-18's literal wording)."
  - "The per-result view-binding candidate (one curated tool pointing at two documents) is recorded as INVESTIGATED-AND-ABSENT, not offered as an option: bridgedTool carries exactly one mcp.ViewRef, and SEP-1865 (status Final) lists per-result UI binding under its own Future Considerations — not part of the finalized spec."
  - "trustedRecipeActions ends the phase with THREE key spaces, not two: calendar=action-keyed (14), whatsapp=MIXED within one source (12 action-keyed + 2 raw-tool-name-keyed, disjoint), memory=raw-tool-name-keyed. D-35's original two-way framing is amended in this document."
  - "The mount-time reconciliation predicate is: a trustedRecipeActions entry is accounted-for when it is EITHER a member of the curated tool's action enum OR the name of a tool the server currently advertises — otherwise list_chats/list_messages WARN spuriously at every mount."
  - "MCP-05's fix: get_calendar_event_details drops accountId entirely and instead takes the same opaque eventId get_calendar_events already returns per event, plus calendarId and timeZone. Aura never re-cases, normalizes, or re-encodes the reference."
  - "The 2026-08-17 '12 actions vs 14 / aura/cockpit-connect vs main' question is recorded as SETTLED by live measurement and commit 73764ea11, not silently dropped — aura/cockpit-connect is retired (fused into main 2026-07-01, 143 commits behind), and every WhatsApp curation commit lands on main directly."

patterns-established:
  - "A curated multiplexed MCP tool may coexist with fork-exempted raw tools from the same mount, provided the exemption's rationale (an existing grant the merge would break) is recorded in the design doc and the risk table's key spaces stay disjoint and documented"

requirements-completed: [MCP-04, MCP-05]

coverage:
  - id: D1
    description: "docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md exists with the ten-section shape, specifying both curated surfaces (calendar 14 actions / WhatsApp 12 actions + 2-tool exemption), all risk classes matching bridge_risk.go exactly, the flat-union schema shape, D-19's ID discipline, MCP-05's accountId fix, the three-key-space table, and the reconciliation predicate"
    requirement: "MCP-04"
    verification:
      - kind: other
        ref: "bash -c 'f=docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md; test -f \"$f\"; [ \"$(grep -c \"^## \" \"$f\")\" -ge 10 ]; for s in COMPAT-03 eventId 4096 list_chats sha- aura/pim-sidecar; do grep -qF -- \"$s\" \"$f\"; done' -> OK (plan's own automated <verify> script, re-run post-edit)"
        status: pass
      - kind: other
        ref: "grep sweep for required: [\"action\"], chatId, messageId, emailId, oneOf, no hide-list, no curation config, COMPAT-01, COMPAT-02, 997 + 399, 1,396, aura-publish-image, publish-image.yml, 40-hex, calendar__calendar, whatsapp__messages"
        status: pass
      - kind: other
        ref: "manual diff of §5a/§5b action-risk tables against internal/agent/mcptools/bridge_risk.go's live trustedRecipeActions table (read in full at commit 807cbfb10-HEAD): calendar 10 read/2 mutate/2 destructive and whatsapp 7 read/1 mutate/4 destructive (curated 12) + 2 read (exempted) all match byte-for-byte"
        status: pass
    human_judgment: false
  - id: D2
    description: "The two measured always-loaded description byte counts (list_messages=997B, list_chats=399B) are re-confirmed live against the exact pin the doc names (whatsapp-mcp:sha-e0b8345)"
    verification:
      - kind: integration
        ref: "live tools/list JSON-RPC probe against 127.0.0.1:8092/mcp (running container ghcr.io/chetto1983/whatsapp-mcp:sha-e0b8345) during this plan's execution: 14 tools returned; list_messages description_bytes=997, schema_bytes=1018; list_chats description_bytes=399, schema_bytes=456 — exact match to the doc's stated numbers"
        status: pass
    human_judgment: false

duration: 55min
completed: 2026-08-22
status: complete
---

# Phase 46 Plan 02: Curated MCP surface design doc Summary

**One design doc (`docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md`) specifying calendar's 14-action curated tool and WhatsApp's 12-action curated tool plus a 2-tool view-exemption (`list_chats`/`list_messages` stay raw and advertised), with the operator's `views-exempt` + `calendar__calendar`/`whatsapp__messages` naming decisions recorded verbatim.**

## Performance

- **Duration:** ~55 min (estimated — an extensive required-reading phase over CONTEXT.md, HALT,
  RESEARCH, the 2026-06-16 precedent, and eight source files preceded the single commit)
- **Started:** ~2026-08-22T13:07:00Z (estimated)
- **Completed:** 2026-08-22T14:02:31Z
- **Tasks:** 2 (Task 1: checkpoint:decision, already resolved by the operator before this execution
  — recorded, not re-asked; Task 2: write the design doc)
- **Files modified:** 1 created (`docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md`)

## Accomplishments

- Wrote the ten-section curated-surface design doc mirroring the 2026-06-16 `aura-pim-mcp`
  fork-design precedent's shape, covering **both** forks in one document (D-24): calendar's
  14-action curated tool with no exemptions (calendar declares no `resources` capability and has
  zero MCP Apps views, confirmed live 2026-08-22), and WhatsApp's 12-action curated tool plus a
  separately-headed exemption table naming `list_chats` and `list_messages` with their `ui://`
  bindings.
- Recorded the operator's Task 1 decision **verbatim**, together with both rejected alternatives and
  their stated costs, quoted from the plan's own option text (not paraphrased): `views-exempt`
  selected over `views-drop`; `name-keep-d18` selected over `name-pim`.
- Recorded the per-result-view candidate as **INVESTIGATED-AND-ABSENT**, citing `bridgedTool`'s
  single `mcp.ViewRef` field and SEP-1865's "Future Considerations" listing of per-result UI binding
  as not part of the finalized spec.
- Stated the merged-description byte budget as numbers, not an inference: ~1.5-2KB target per
  curated tool, `maxMCPDescriptionBytes = 4096` as the hard cap, and the exempted pair's measured
  always-loaded cost — `list_messages` 997B description (1,018B schema) + `list_chats` 399B
  description (456B schema) = **1,396 bytes** — **re-confirmed live during this plan's execution**
  against the exact pinned container (`ghcr.io/chetto1983/whatsapp-mcp:sha-e0b8345`, running,
  healthy) via a direct `tools/list` JSON-RPC call to `127.0.0.1:8092/mcp`: 14 tools returned, byte
  counts matched the doc's stated numbers exactly.
- Diffed every action's risk class in the doc's §5a/§5b tables against `bridge_risk.go`'s live
  `trustedRecipeActions` table and confirmed an exact match: calendar 10 read / 2 mutate /
  2 destructive; WhatsApp curated-12 7 read / 1 mutate / 4 destructive; both exempted tools read.
  No action was re-tiered by the merge.
- Recorded the three-key-space shape `trustedRecipeActions` ends the phase with (calendar
  action-keyed; WhatsApp MIXED — 12 action-keyed + 2 raw-tool-name-keyed, disjoint; memory
  raw-tool-name-keyed) and the mount-time reconciliation predicate (a table entry is accounted-for
  when it is EITHER a member of the curated tool's `action` enum OR the name of a tool the server
  currently advertises).
- Stated MCP-05's fix concretely: `get_calendar_event_details` drops `accountId` and instead takes
  the opaque `eventId` its own listing action (`get_calendar_events`) already returns, plus
  `calendarId` and `timeZone` — no `accountId` anywhere in the curated schema.
- Named both fork repos, edit sites, branches (`aura/pim-sidecar` for PIM, `main` for WhatsApp), and
  tag formats (40-hex `github.sha` vs. `sha-<7hex>`), and stated explicitly that
  `aura/cockpit-connect` is retired — no instruction anywhere in the doc pushes curation commits
  there.

## Task Commits

1. **Task 1: Decide the WhatsApp views question, and confirm the two model-facing tool names** —
   no separate commit (per the plan's own `<files>` block: "Decisions log — written in Task 2"). The
   operator's decision was already resolved before this execution began and is recorded verbatim
   inside the Task 2 commit's file.
2. **Task 2: Write the curated-surface design doc** — `2012ce81e` (docs)

**Plan metadata:** this SUMMARY + STATE.md + ROADMAP.md (below).

## Files Created/Modified

- `docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md` — the ten-section curated-surface
  design doc: Background & motivation; Goals/Non-goals; Architecture; Thin-fork changes; the two
  curated surfaces (action tables, exemption table, flat-union schema, ID discipline, description
  budget); Aura-side integration (naming, three-key-space table, reconciliation predicate); Security
  & policy; Validation plan; Risks & open items; Decisions log.

## Decisions Made

See frontmatter `key-decisions` for the full list. In short: `views-exempt` (WhatsApp keeps
`list_chats`/`list_messages` raw and advertised, landing at 3 model-facing tools) and
`name-keep-d18` (`calendar__calendar`, `whatsapp__messages`) were selected by the operator before
this execution and are now recorded verbatim, with both rejected alternatives and their costs, in
the design doc's Decisions log (§10).

## Deviations from Plan

None — plan executed exactly as written. Task 1's checkpoint was pre-resolved by the operator (per
the orchestrator's instruction) and was recorded rather than re-asked; Task 2 was written exactly to
the plan's `<action>` block, and every literal string its `<verify>` script and acceptance criteria
require (`COMPAT-03`, `eventId`, `4096`, `list_chats`, `sha-`, `aura/pim-sidecar`, `required:
["action"]`, `chatId`, `messageId`, `emailId`, `oneOf`, `no hide-list`, `no curation config`,
`COMPAT-01`, `COMPAT-02`, the measured byte counts, both tag formats, and both model-facing names)
was confirmed present by a full grep sweep before this SUMMARY was written.

## Issues Encountered

None. The first draft omitted the bare digit string `4096` (having written only `4,096` with a
comma) and used capitalized "No hide-list."/"No curation config." instead of the lowercase forms the
plan's acceptance criteria named — both were caught by running the plan's own verification sweep
before committing, and fixed in the same pre-commit editing pass (not a separate deviation, since the
plan's own acceptance criteria already specified this exact outcome).

## User Setup Required

None — no external service configuration required. Pure documentation change; no code touched.

## Next Phase Readiness

- The design doc is landed, and plans 46-05 through 46-08 have a citable, byte-verified contract:
  calendar's 14-action table, WhatsApp's 12-action table + 2-tool exemption, both model-facing
  names (`calendar__calendar`, `whatsapp__messages`), the three-key-space `trustedRecipeActions`
  shape, and the mount-time reconciliation predicate.
- `git diff --stat 2012ce81e~1 2012ce81e` confirms the design doc is the only file this plan
  touched — no code, test, or other planning-doc drift.
- 46-03 (REQUIREMENTS/ROADMAP clean rewrite) must say "WhatsApp: 1 curated + 2 exempted = 3
  model-facing tools," never "1," when it restates D-18's worked example — this document is the
  citable source for that correction.
- 46-08 (WhatsApp fork curation) now has its Task 1 framing corrected: it should point at this
  document's `views-exempt` decision, not at the now-moot 2026-08-17 "12 vs 14 actions" question.

## Self-Check: PASSED

- FOUND: `docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md`
- FOUND: `2012ce81e` (Task 2 commit — `git log --oneline --all | grep 2012ce81e` matches)
- FOUND: live re-measurement against `ghcr.io/chetto1983/whatsapp-mcp:sha-e0b8345` matches the doc's
  stated byte counts exactly (997/1018 for `list_messages`, 399/456 for `list_chats`, 14 tools total)
- FOUND: `bridge_risk.go`'s live `trustedRecipeActions` table matches the doc's §5a/§5b risk classes
  exactly, action-for-action

---
*Phase: 46-mcp-trust-and-facade*
*Completed: 2026-08-22*
