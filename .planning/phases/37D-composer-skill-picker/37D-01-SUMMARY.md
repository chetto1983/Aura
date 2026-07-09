---
phase: 37D-composer-skill-picker
plan: 01
subsystem: docs/prd
tags: [prd-amendment, pre-code-gate, composer-skill-picker, WEBSKILL, docs-only]
type: execute
wave: 1
autonomous: true
dependency_graph:
  requires: []
  provides:
    - "prd.md Amendment #81 — the architectural record every 37D code plan (02..05) depends_on"
  affects:
    - "37D-02..05 (all gated behind this PRD-first commit, D-11)"
tech_stack:
  added: []
  patterns:
    - "PRD-amendment blockquote (mirrors WEBART #78 / WEBVOICE #79-#80) — git-log ordering is the gate"
key_files:
  created:
    - ".planning/phases/37D-composer-skill-picker/37D-01-SUMMARY.md"
  modified:
    - "prd.md (Amendment #81 — Composer Skill & Command Picker, +11 lines, commit 2a409051)"
    - ".planning/STATE.md (tracking)"
    - ".planning/ROADMAP.md (37D-01 checkbox)"
decisions:
  - "Amendment #81 records Mechanism A (server context-prepend of the exact useAuthorityFrame + body via the existing TurnWithModelUserMessage seam) as delivered — zero runner change, no new agent tool, no new skills store; Mechanism B (forced first tool call / visible card) rejected because it needs a new runner seam"
  - "D-04 reconciliation: WEBSKILL-01/02 'identity-scoped / via the governance skills API' delivered as an authentication-scoped GLOBAL active-skills snapshot behind plain RequireAuth (NOT governance.read — the D-03 403 trap); PERSISTED per-identity skill scoping DEFERRED (NewSkillToolForIdentity dormant, zero prod callers; no skill-grant migration 0001-0035)"
  - "New RequireAuth-only GET /api/composer/skills returns {name,description,type} from the SAME loader.List() snapshot the governance board + runtime skill action=use read — one source of truth (WEBSKILL-02); 200 empty list / 503 unavailable, never 403"
  - "Pinned skill rides the EXISTING aura run-request envelope (aura.skill, symmetric with attachment_ids — one field each side); unknown/empty name is a no-op (loader key only, never a filesystem path)"
  - "Amendment number grep-confirmed: prd.md max was #80 (37C) → this is #81"
metrics:
  tasks_completed: 1
  duration: "~11 min (docs-only)"
  completed: "2026-07-09"
  files_changed: 1
  commit: "2a409051"
requirements_touched: [WEBSKILL-01, WEBSKILL-02, WEBSKILL-03]
requirements_completed: []
---

# Phase 37D Plan 01: Composer Skill & Command Picker PRD-Amendment Pre-Code Gate Summary

**One-liner:** Landed prd.md Amendment #81 documenting the composer skill-picker surface (WEBSKILL-01..03, the new `RequireAuth`-only `GET /api/composer/skills`, the `aura.skill` run-request envelope field, Mechanism-A server-side pinned-skill application, and the identity-scoped→GLOBAL-snapshot reconciliation) as the mandatory PRD-first (D-11) commit that gates every 37D code plan.

## What Was Built

A single docs-only task: a new **"Composer Skill & Command Picker (Picker Skill/Comandi) — 37D"** amendment (numbered **#81**) appended to `prd.md` immediately after the WEBVOICE Amendment #80 block (before the Slice-9 `**Goal.**` line, near line 2971), mirroring the #78/#79/#80 blockquote/heading/format conventions. The amendment records, at the PRD level:

1. **WEBSKILL-01..03 requirement group** — transcribed faithfully from `REQUIREMENTS.md:87-89`, each carrying an explicit scope-reconciliation note pointing at parts (3)/(4).
2. **The new authenticated read route `GET /api/composer/skills`** — mounts behind the whole-mux `RequireAuth` wrap (like `imageProxyRoute` / `graphSchemaRoute` / `GET /api/me` / `GET /api/voice/capabilities`), **explicitly NOT** behind `governance.read` (that admin-scoped gate 403s ordinary identities — the exact D-03 trap). Returns `{skills:[{name,description,type},...]}` projected from the active-skills loader snapshot (a lean sibling of the governance board's `activeSkillRows`, consuming the SAME `SkillsBoardProvider.ActiveSkills()` / `loader.List()` seam on a different ungated route); 200 empty list / 503 unavailable, never 403; a specific `GET` method+path sibling (never a bare `/api/` subtree); mount lands in a new `serve_webui_composer.go` to respect the 600-LOC cap.
3. **The D-01 pinned-skill wire path (`aura.skill` + Mechanism A, zero runner change)** — the client folds a `skill` field into the EXISTING `aura` run envelope (`{aura:{attachment_ids?,skill?}}`, symmetric with `attachment_ids`); the server decodes `req.Aura.Skill` and applies it via **Mechanism A**: resolve the skill body from the loader and prepend the exact `useAuthorityFrame` string ("Follow these skill instructions for the current task:") + body that `skill action=use` already emits, onto the MODEL-visible message through the existing `TurnWithModelUserMessage` context-prepend seam (raw user text stays the persisted/visible turn). **No new agent tool, no new skills store (WEBSKILL-02), no runner change**; unknown/empty name is a no-op (loader key only, never a path). **Mechanism B** (forced first tool call / visible card) recorded as the REJECTED alternative.
4. **The D-04 reconciliation** — WEBSKILL-01/02's "identity-scoped / via the governance skills API" is delivered as an authentication-scoped **GLOBAL** active-skills snapshot behind plain `RequireAuth`. Evidence chain recorded (loader is process-global with no identity field; `ActiveSkills()` == `loader.List()` over fixed global roots; the live `newSkillTool` uses the SAME roots so list == invocable set; `NewSkillToolForIdentity` DORMANT with zero prod callers; no skill-grant table in migrations 0001-0035). A **PERSISTED per-identity skill scoping/grant capability is explicitly DEFERRED** to a future phase.
5. **The D-02/D-06 UI surface** — `/`-triggered (only as the first char of an empty composer) ARIA combobox+listbox rendered above the input (aria-expanded/controls/activedescendant, ↑/↓/Enter/Esc + typeahead, JS-scroll of the active option), skills grouped by category + quick-command actions; selected skill = a removable pill above the input; `add-files`/`new-chat`/`clear` are pure client UI (Paperclip `fileInputRef.click()` / `startNewConversation` / reset composer input+pill+pending attachments — no conversation delete) with NO agent round-trip; degrade-to-no-op on empty/unreachable list.

## Verification

The plan's `<automated>` verify intent passed (all content tokens present; no code/web/test leakage):
- All required grep tokens present in `prd.md`: `WEBSKILL-01`, `WEBSKILL-02`, `WEBSKILL-03`, `/api/composer/skills`, `aura.skill`, `useAuthorityFrame`, `Follow these skill instructions`, `TurnWithModelUserMessage`, `RequireAuth`, `governance.read`, `DEFERRED`, `Amendment #81`.
- The 37D subsection states the endpoint is `RequireAuth`-only and explicitly NOT `governance.read` (anti-403 note), and records the GLOBAL snapshot + per-identity scoping DEFERRED.
- Code/web/test leakage check: `git diff --name-only` filtered for `\.(go|ts|tsx|js|jsx|py)$|^web/` → empty (`NO_CODE_LEAK_OK`). The only dirty files were `prd.md` (this change) and `.planning/STATE.md` (the orchestrator's pre-execution "37D executing" prep — tracking, not code).
- Pre-commit hooks green (vet 5.0s + whole-tree file-size 71.3s), no `--no-verify`; task commit `2a409051` = 1 file / +11 / 0 deletions.

## Deviations from Plan

**None to the plan's task** — the amendment was written exactly as specified, honoring all five prohibitions: no Go/web/test file touched (docs-only, prd.md only); per-identity filtering recorded as DEFERRED (not delivered); `GET /api/governance/skills` reuse NOT recorded (new `RequireAuth`-only `GET /api/composer/skills` instead); Mechanism B recorded as the rejected alternative (Mechanism A delivered); amendment number grep-confirmed (#80 max → #81, not hardcoded blindly).

### Working-tree hygiene (execution-mechanics, not a plan deviation)

`.planning/STATE.md` was dirty at start carrying the orchestrator's "37D executing" pre-execution update (current_phase 37C→37D, total_plans 77→82, Current Position block). Matching the 37C-01 precedent, it was NOT reverted: the prd.md task commit used an explicit `git add prd.md` pathspec so only prd.md landed in `2a409051`, and STATE.md was then further updated (plan advance) and committed in the separate tracking commit (this executor owns the shared-file updates in sequential/non-worktree mode).

## Requirements

`WEBSKILL-01..03` are **phase-spanning** — this plan only DOCUMENTS them; they are delivered by the code plans 37D-02..05. They remain `[ ]` and `requirements mark-complete` was intentionally NOT run (matching the 37B/37C gate-plan precedent).

## Known Stubs

None introduced. (The amendment *records* that the per-identity rooting primitive `NewSkillToolForIdentity` remains a pre-existing DORMANT primitive with zero production callers and that a persisted per-identity skill-grant store is DEFERRED — documented intent, not a new stub created by this plan.)

## Next

Wave 2 — **37D-02** (backend: `GET /api/composer/skills` RequireAuth-only global snapshot + the `aura.skill` decode + Mechanism-A prepend + divergence guard) and **37D-03** (frontend picker foundation), both of which `depends_on` this commit.

## Self-Check: PASSED

- FOUND: `.planning/phases/37D-composer-skill-picker/37D-01-SUMMARY.md`
- FOUND: `prd.md` with `Amendment #81` present in commit `2a409051`
- FOUND: commit `2a409051` (prd.md amendment, 1 file / +11 / 0 deletions)
- All required grep tokens present; no Go/web/test file touched (`NO_CODE_LEAK_OK`); `git diff --name-only` code-filtered == empty at task-commit time.
- No unintended deletions.
