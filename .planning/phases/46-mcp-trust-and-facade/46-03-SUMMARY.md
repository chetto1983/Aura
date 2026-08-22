---
phase: 46-mcp-trust-and-facade
plan: 03
subsystem: docs
tags: [requirements, roadmap, mcp, footnotes, clean-rewrite]

# Dependency graph
requires:
  - phase: 46-mcp-trust-and-facade (plan 01)
    provides: prd.md Amendments #122/#123/#124 — the amendment numbers this plan cites when rewriting MCP-02/04/05 and Phase 46's Rationale
  - phase: 46-mcp-trust-and-facade (plan 02)
    provides: "The operator's views-exempt / name-keep-d18 decisions (docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md) — the source for the WhatsApp '3 model-facing tools' worked example this plan writes into ROADMAP.md for the first time"
provides:
  - "REQUIREMENTS.md's MCP-02/04/05 rows in the MCPC-01..05 clean shape (current text first, amended clause citing prd.md Amendment #122, no inline strikethrough), with a Footnotes subsection preserving every relocated word"
  - "ROADMAP.md's Phase 46 section in the already-clean Phase 47 shape (Goal / Depends on / Requirements / Rationale / Success Criteria / Plans), with TOOL-14's calendar-1/WhatsApp-3 worked example stated for the first time, and a Footnotes subsection preserving every relocated word"
affects: [46-04, 46-05, 46-06, 46-07, 46-08, 46-09]

actuals:
  tokens: 3400
  tasks: 2
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Clean-row rewrite with mandatory dated footnotes (D-31): current-state prose first, an inline **Amended YYYY-MM-DD (...)** clause, and every removed word relocated verbatim into a `<a name=\"fn-...\"></a>` anchored footnote linked from the paragraph it came from — never deleted, never left struck inline (except an explicitly-excluded criterion)."

key-files:
  created: []
  modified:
    - .planning/REQUIREMENTS.md
    - .planning/ROADMAP.md

key-decisions:
  - "MCP-03's row also carried an inline `~~struck~~` reference to MCP-02's former fencing clause, even though the plan's own read_first text asserted MCP-01 and MCP-03 'already read clean.' That assertion was stale — the plan's own automated `<verify>` script (grep -c '~~' over the MCP-01..MCP-05 block must equal 0) would have failed with MCP-03 left untouched. Treated as a Rule 1 plan-authoring bug: MCP-03 was cleaned the same way as MCP-02/04/05, with its struck span relocated to a fifth footnote (fn-mcp-03-1) not explicitly named in the plan's action text but required by its acceptance criteria."
  - "ROADMAP.md's Phase 46 section dropped its 'What changed, measured:' bulleted subsection and its 'Blocking before any code' paragraph outright, without footnoting them. Neither was struck-through or SUPERSEDED-tagged — they were true, current process narrative (measurement bullets already fully preserved in prd.md Amendment #122 and REQUIREMENTS.md's rewritten rows; the blocking gate they described is already satisfied by plan 46-01). Phase 47's target shape has no analogous subsections, so their substance was folded into Goal/Rationale/TOOL-14 rather than relocated to a footnote, since D-31's footnote mandate applies to superseded (struck/SUPERSEDED-tagged) wording, not to all historical narrative being condensed for concision."
  - "The 'Revised 2026-08-16' header blockquote (which itself contained the word 'SUPERSEDED', describing the now-obsolete inline-strikethrough convention) was replaced with a one-line plain-text pointer to 46-CONTEXT.md, matching Phase 45.1's non-blockquote citation style — once every inline SUPERSEDED paragraph is relocated, the header's claim ('paragraphs below marked SUPERSEDED are kept...') would otherwise be false."
  - "Per the orchestrator's explicit upstream_facts instruction, MCP-02/04/05 are NOT marked complete in REQUIREMENTS.md (checkboxes stay `[ ]`) and `requirements.mark-complete` was not run for this plan, even though this plan's own frontmatter lists them under `requirements:` — their documentation was rewritten clean, but the underlying behavior (fork curation, D-27 count rule, action re-keying) has not shipped yet and remains open for plans 46-04 through 46-08."

requirements-completed: []

coverage:
  - id: D1
    description: "REQUIREMENTS.md's MCP-02, MCP-04 and MCP-05 rows rewritten clean (MCPC-01..05 shape), citing prd.md Amendment #122, with a Footnotes subsection holding all 4 relocated passages (plus MCP-03's, cleaned for consistency with the same acceptance gate)"
    requirement: "MCP-04"
    verification:
      - kind: other
        ref: "bash -c 'awk \"NR>=113 && NR<=126\" .planning/REQUIREMENTS.md | grep -c \"~~\" | grep -qx 0 && grep -q \"Footnotes\" .planning/REQUIREMENTS.md && echo OK' -> OK (plan's own automated <verify>)"
        status: pass
      - kind: other
        ref: "grep -qF sweep for all 4 verbatim extracted strings ('Per-call result fencing and', \"MCP-02's per-call result fencing and\", '**one** curated `comms` surface — a single loaded slot', '`accountId` is resolved host-side from the operator's configuration, like `user_identifier`') — all found"
        status: pass
    human_judgment: false
  - id: D2
    description: "ROADMAP.md's Phase 46 section rewritten in Phase 47's clean shape (Goal/Depends on/Requirements/Rationale/Success Criteria/Plans), citing prd.md Amendments #122/#123, stating TOOL-14's calendar-1/WhatsApp-3 worked example for the first time from 46-02-SUMMARY.md, with SC#5 left inline-struck (D-07) and 4 other superseded passages relocated to footnotes"
    requirement: "MCP-04"
    verification:
      - kind: other
        ref: "bash -c 'sed -n \"/^### Phase 46:/,/^### Phase 47:/p\" .planning/ROADMAP.md > /tmp/p46.md; grep -q \"DELETED 2026-08-16 (D-07)\" /tmp/p46.md && grep -q \"Footnotes\" /tmp/p46.md && grep -q \"46-09-PLAN.md\" /tmp/p46.md && echo OK' -> OK (plan's own automated <verify>)"
        status: pass
      - kind: other
        ref: "grep -n '~~' over the Phase 46 section returns exactly one hit (SC#5); grep -n 'SUPERSEDED' over the same section returns zero hits; grep -c 'Multiplexed' returns >=1 outside the footnotes (D-21 paragraph intact)"
        status: pass
    human_judgment: false

duration: 32min
completed: 2026-08-22
status: complete
---

# Phase 46 Plan 03: REQUIREMENTS/ROADMAP clean rewrite Summary

**Rewrote REQUIREMENTS.md's MCP-02/04/05 rows and ROADMAP.md's entire Phase 46 section from struck-through-and-appended prose into clean current-state text, relocating 8 superseded passages into two new dated Footnotes subsections (one per file) so the falsification trail survives one hop away instead of inline.**

## Performance

- **Duration:** ~32 min
- **Started:** ~2026-08-22T13:50:00Z (estimated — an extensive required-reading phase over the plan, both wave-1 SUMMARYs, CONTEXT.md's D-06/D-07/D-17/D-18/D-20/D-21/D-27, PATTERNS.md's clean-row shape, and prd.md's Amendment #122 preceded the first edit)
- **Completed:** 2026-08-22T14:18:08Z
- **Tasks:** 2
- **Files modified:** 2 (`.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md`)

## Accomplishments

- **REQUIREMENTS.md**: MCP-02, MCP-04 and MCP-05 now read as current-state prose in the same shape MCPC-01..05 already use — plain text first, a bolded `**Amended 2026-08-16 (...; ratified in prd.md Amendment #122)**` clause folding in what changed, and an inline `([superseded wording](#fn-mcp-0X-1))` link to a new `#### Footnotes — superseded MCP requirement wording` subsection. MCP-03's row carried the same struck pattern (a leftover reference to MCP-02's former fencing clause) and was cleaned identically, since the plan's own acceptance grep counts strikethrough across the whole MCP-01..MCP-05 block, not just the three named rows. 4 verbatim passages extracted, 4 footnotes added — every one independently re-found by exact substring search.
- MCP-04's row now states the WhatsApp exemption explicitly and by name: "one multiplexed tool per sidecar, plus whatever a fork deliberately exempts from its own merge (WhatsApp keeps its two view-bound reads, `list_chats` and `list_messages`, so its slot holds 3 model-facing tools)" — replacing the stale "two curated multiplexed tools" framing that was no longer true of the WhatsApp side after 46-02's `views-exempt` decision.
- **ROADMAP.md**: The Phase 46 section's `▶ Revised 2026-08-16` header (itself carrying the word SUPERSEDED) and six struck/SUPERSEDED-tagged paragraphs were rewritten into the same Goal / Depends on / Requirements / Rationale / Success Criteria / Plans shape Phase 47 already uses, citing prd.md Amendments #122 and #123. A new `#### Footnotes — superseded Phase 46 wording` subsection preserves the 4 relocated passages verbatim, each linked from the paragraph it came from.
- **TOOL-14's worked example is stated for the first time, sourced from 46-02-SUMMARY.md as instructed**: under the operator's `views-exempt` selection, calendar exposes 1 curated tool and WhatsApp exposes 3 (1 curated + 2 exempted view-bound reads), both qualifying under Amendment #123's `<=3` count rule with WhatsApp at zero headroom; the rejected `views-drop` alternative would have been 1 each. This is the first document in the phase permitted to state this arithmetic concretely (46-01 runs in parallel with 46-02's decision; this plan runs after it).
- **Success Criterion 5 was left untouched**, exactly as instructed: still struck, still carrying `DELETED 2026-08-16 (D-07)` inline — not relocated to a footnote, not reworded, not reinterpreted as live.
- The D-21 load-bearing-consequence paragraph, the TOOL-14 paragraph, and the MCP-trust-question paragraph's live conclusion (Amendment #110 amended not restored, `prd.md:4579` untouched) all stay inline, as required — none of these are superseded.
- Ran `roadmap.update-plan-progress 46` after the Task 2 rewrite (per the plan's own ordering constraint) — it reported `plan_count: 9, summary_count: 2` with no diff against the already-correct "2/9 plans executed" line, confirming the generated progress state matches what this rewrite already states.

## Task Commits

Each task was committed atomically:

1. **Task 1: Rewrite REQUIREMENTS.md's MCP-02/04/05 rows clean with dated footnotes** — `aa33e710f` (docs)
2. **Task 2: Rewrite ROADMAP.md's Phase 46 section clean with dated footnotes** — `47793052c` (docs)

_No separate plan-metadata commit yet — this SUMMARY, STATE.md and ROADMAP.md's plan-progress line land in the closing metadata commit described below._

## Files Created/Modified

- `.planning/REQUIREMENTS.md` — MCP-02/04/05 (and MCP-03, for consistency) rewritten clean; new `#### Footnotes — superseded MCP requirement wording` subsection added inside `### MCP trust and facade`
- `.planning/ROADMAP.md` — the entire Phase 46 section rewritten in Phase 47's clean shape; new `#### Footnotes — superseded Phase 46 wording` subsection added at the end of the section, before `### Phase 47:`

## Decisions Made

See frontmatter `key-decisions` for the full list. In short: MCP-03's stray strikethrough was cleaned as a Rule 1 fix to satisfy the plan's own acceptance grep; ROADMAP's now-redundant "What changed, measured" / "Blocking before any code" narrative was folded into Goal/Rationale/TOOL-14 rather than footnoted, since it was true current-at-the-time narrative, not superseded wording; the header blockquote was replaced with a plain pointer to CONTEXT.md; and MCP-02/04/05 were NOT marked complete in REQUIREMENTS.md per the orchestrator's explicit instruction, even though this plan's own frontmatter lists them.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] MCP-03's row also required strikethrough cleanup, contrary to the plan's own read_first assertion**
- **Found during:** Task 1
- **Issue:** The plan's `<read_first>` and `<action>` text stated "Leave MCP-01 and MCP-03 as they are — they already read clean and are already `[x]`." This was stale: MCP-03's live text (`.planning/REQUIREMENTS.md:121`, pre-edit) carried `~~MCP-02's per-call result fencing and~~`, a leftover reference to MCP-02's own former fencing clause. Left untouched, the plan's own automated `<verify>` (`grep -c '~~' ... | grep -qx 0`, scoped over lines 113-126, i.e. the whole MCP-01..MCP-05 block) would have failed.
- **Fix:** Cleaned MCP-03's row the same way as MCP-02/04/05 — removed the inline strikethrough, kept the sentence's meaning ("the residual risk is carried by fail-closed risk classification and by the operator's control over what gets mounted at all"), and relocated the removed span to a fifth footnote (`fn-mcp-03-1`), keeping the extracted-count/footnote-count invariant intact (4/4 for this file, counting the plan-named MCP-02/04/05 spans plus this one).
- **Files modified:** `.planning/REQUIREMENTS.md`
- **Verification:** `awk "NR>=113 && NR<=126" .planning/REQUIREMENTS.md | grep -c '~~'` returns 0; the extracted string `"MCP-02's per-call result fencing and"` is present verbatim in the new Footnotes subsection (`grep -qF` confirmed)
- **Committed in:** `aa33e710f` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 bug — a stale plan assertion that would have failed the plan's own acceptance gate)
**Impact on plan:** Necessary for the plan's own machine-checkable acceptance criteria to pass; no scope creep — MCP-03's meaning was preserved exactly, only its inline strikethrough syntax was relocated to a footnote in the same style as its siblings.

## Issues Encountered

None beyond the deviation above. `git diff --stat` after both tasks confirmed only `.planning/REQUIREMENTS.md` and `.planning/ROADMAP.md` were touched by this plan's commits — a concurrently-dirty working tree (unrelated, pre-existing modifications to `internal/agent/prompt/builder.go`, `internal/llm/*`, and `prd.md`, plus two untracked files `docs/pms-mcp-tool-surface.md` and `.planning/milestone.lock`) was present throughout execution and was deliberately left untouched and unstaged in both commits, per the explicit instruction to leave unrelated dirty files alone.

## User Setup Required

None — no external service configuration required. Pure documentation change; no code touched.

## Next Phase Readiness

- Both planning documents now describe Phase 46 as it currently is; every superseded word from both files is recoverable, dated, in the same file, one click away via the new footnote anchors.
- `.planning/REQUIREMENTS.md`'s MCP-02/04/05 checkboxes remain unchecked, `[ ]` — the fork curation (46-04..46-08) is still open work, not yet complete. TOOL-14 stays `[x]`, already closed by plan 46-01.
- Plan 46-04 (D-27's deferral count rule) and plan 46-05 (calendar fork curation) can now cite `.planning/ROADMAP.md`'s Phase 46 section and `.planning/REQUIREMENTS.md`'s MCP-04/05 rows directly, without needing to first mentally subtract struck-through history to find current state.
- `git diff --stat aa33e710f~1 47793052c` confirms only the two planning documents were touched across both commits — no code, test, or other planning-doc drift from this plan's own work.

## Self-Check: PASSED

- FOUND: `.planning/phases/46-mcp-trust-and-facade/46-03-SUMMARY.md`
- FOUND: `aa33e710f` (Task 1 commit — `git log --oneline --all` matches)
- FOUND: `47793052c` (Task 2 commit — `git log --oneline --all` matches)
- FOUND: REQUIREMENTS.md acceptance script (`awk NR>=113 && NR<=126 | grep -c '~~' == 0 && grep -q Footnotes`) -> OK
- FOUND: ROADMAP.md acceptance script (`DELETED 2026-08-16 (D-07)` + `Footnotes` + `46-09-PLAN.md` all present) -> OK
- FOUND: `git diff --stat` across both task commits touches only `.planning/REQUIREMENTS.md` and `.planning/ROADMAP.md`

---
*Phase: 46-mcp-trust-and-facade*
*Completed: 2026-08-22*
