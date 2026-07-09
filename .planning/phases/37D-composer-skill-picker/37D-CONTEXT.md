# Phase 37D: Composer Skill & Command Picker - Context

**Gathered:** 2026-07-09
**Status:** Ready for planning

<domain>
## Phase Boundary

A keyboard-driven `/` menu in the web Composer (`web/src/chat/Composer.tsx`) that lists the skills available to the authenticated identity **plus** quick command actions, filterable incrementally, so a user can invoke/attach a skill inline in a turn — parity with Claude's own composer picker (see reference screenshot in DISCUSSION-LOG) — instead of skills being managed only in the admin Governance board.

Third and last of the three cockpit-web parity areas from the voice/artifact/skill audit (37B artifacts, 37C voice, 37D skills). We are clarifying HOW to implement the scoped WEBSKILL surface — new capabilities (Cmd+K palette, per-identity skill grants, sharing) belong in other phases.

</domain>

<decisions>
## Implementation Decisions

### Selection semantics (fork b — RESOLVED)
- **D-01:** Selecting a skill **invokes it as an explicit tool**, not a soft context hint. The picked skill is pinned to the turn; on send, the run request carries `skill=<name>` and the agent runs `skill action=use name=<name>` as its **first action** (deterministic — the skill IS applied, not merely suggested). This reuses the existing runtime contract (`internal/agent/tools/skill.go` `action=use`; taught in `internal/agent/prompt.go` `<skills>`). Planner must define how the pinned skill name travels composer → run request (new field on the send payload / external-store submit path).

### Menu scope (fork c — RESOLVED, full command set)
- **D-02:** The menu lists **skills + quick command actions**, matching the Claude reference:
  - **skills** — grouped by category, incremental filter;
  - **`add-files`** — reuse the existing Paperclip file-picker handler already in `Composer.tsx` (no new mechanism, just a menu entry that triggers `fileInputRef.click()`);
  - **`new-chat`** and **`clear`** — **net-new client-side UI actions** (they do NOT exist in the web today; grep confirmed no `newChat`/`clearConversation` in `web/src`). Planner scopes these as pure client actions (no agent turn). Wire `new-chat`/`clear` to the existing conversation/thread controls in `AppShell`/`ExternalStoreChat`.
- Quick actions are pure client UI (no agent round-trip); skills produce a pinned invocation (D-01).

### List source & scoping (fork a — RESOLVED with an evidence gate)
- **D-03:** **New per-identity endpoint behind plain `RequireAuth`** — do NOT reuse `GET /api/governance/skills`. That board endpoint is gated by the **`governance.read` capability** (`cmd/aura/serve_webui.go:105`), so an ordinary identity without the admin grant gets **403** and the picker would be empty/broken for exactly its target users. Add a lean handler (working name `GET /api/composer/skills`, planner may rename) returning the active skills as `{name, description, type}` — reuse the loader snapshot (`ActiveSkills()`), project onto a minimal row (mirror `activeSkillRows` in `governance_api.go` for shape, not the mount). Mount behind `RequireAuth` (whole-mux wrap), NOT behind `governanceReadCapability`.
- **D-04 (evidence-gated):** Whether the endpoint returns the **global** active-skills snapshot (RequireAuth only) or must **filter per identity** depends on whether per-identity skill scoping exists in Aura. Today the loader is process-global (`ActiveSkills()` is a global snapshot). **Researcher MUST verify** whether Phase 36 identity isolation introduces any per-identity skill grant/scope before planning locks this. Default if no per-identity scoping exists: global snapshot behind `RequireAuth`. If it does exist: filter to what the bound identity may use (MUSR isolation).

### Trigger & interaction model (fork — RESOLVED)
- **D-05:** `/` opens the picker **only when it is the first character of an empty composer** (matches the Claude reference). `/` typed mid-text is literal. The filter query is the text typed after the leading `/`.
- **D-06:** The pinned skill is shown as a **removable pill above the input**, alongside attachment chips (mirror the existing `AttachmentChip` pattern + `uploads.items` render row in `Composer.tsx`). The user can still type a message and remove the pill before sending. On send, the turn carries the pinned skill (D-01). Do NOT insert editable `/name` text (we chose invoke-as-tool precisely to avoid on-send text parsing).

### Locked by reference screenshot + research (not re-litigated)
- **D-07:** Menu renders **above** the input; a "Type to filter" field; rows carry **icon + name + optional one-line description/subtitle**; entries **grouped by category** with section headers (the Claude reference shows a "Productivity" group; `add-files` sits above the skill groups).
- **D-08:** Accessibility follows the **W3C APG combobox + listbox** pattern: the textarea/input keeps DOM focus with `aria-expanded` / `aria-controls` / `aria-activedescendant` pointing at the highlighted option (focus does NOT move into the list); keyboard `↑`/`↓`/`Enter`/`Esc` + typeahead; JS scrolls the active option into view (aria-activedescendant is not auto-scrolled by browsers).
- **D-09:** **Degrade to no-op** when the skills list is empty or the endpoint is unreachable — `/` simply does not open a menu (no error surface), preserving normal `/` typing. Preserve the Composer's existing paste/drop/Enter-to-send behavior (do not intercept Enter when the menu is closed).
- **D-10:** i18n en+it parity for all new strings (menu labels, group headers, pill, quick-command labels); web coverage **≥85%**; unit React tests + Playwright e2e (open picker → filter → select skill → pill appears → send fires the invocation; quick-action new-chat/clear behavior).

### PRD-first gate (mandatory, blocks all code)
- **D-11:** 37D requires a **PRD-amendment BEFORE any code** (mirrors 37B-01 / 37C-01 pattern, D-14/D-19): add the WEBSKILL-01..03 requirement group + document the composer skill-picker surface + the new `GET /api/composer/skills` endpoint (currently undocumented in the PRD). Wave 1 = PRD-amendment gate; no implementation plan may land before it.

### Claude's Discretion
- Endpoint path/name (`/api/composer/skills` vs `/api/skills`) — planner picks, staying consistent with existing `/api/...` naming.
- Exact category grouping taxonomy for skills (by `Type`, by frontmatter, or a flat "Skills" group) — planner/researcher decide from the loader's available metadata; the reference shows grouping but the source field is open.
- Whether `new-chat`/`clear` live in the same picker list or a small "commands" group — keep them discoverable via `/` per D-02.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope
- `.planning/ROADMAP.md` — Phase 37D section (Goal, WEBSKILL-01..03, Success Criteria, design forks a/b/c).
- `prd.md` — target of the mandatory PRD-amendment (WEBSKILL-01..03 + composer skill-picker surface + new endpoint). See §Q&A revision protocol.

### Backend — skills API + auth mounting
- `internal/agui/governance_api.go` — existing `GET /api/governance/skills` (`handleSkillsList` ~L261, `activeSkillRows`): **reference for row shape only** (`{Name, Description, Type, Language}`), NOT reused as the picker source.
- `cmd/aura/serve_webui.go` — auth/capability mounting; `governanceReadCapability = "governance.read"` (L105) is the gate to AVOID; the new endpoint mounts behind the whole-mux `RequireAuth` wrap.
- `internal/agent/tools/skill.go` — the `skill` tool `action=use` runtime contract the pinned invocation targets.
- `internal/agent/prompt.go` — `<skills>` section (the runtime already knows `skill action=list/use`).
- `internal/skills/loader.go` — `List()`/`ActiveSkills()` global snapshot; the source for D-04's global-vs-per-identity verification.

### Frontend — Composer integration
- `web/src/chat/Composer.tsx` — the integration point; **attachment-chip render pattern** (`uploads.items.map(... AttachmentChip ...)`) is the model to mirror for the removable skill pill (D-06); the Paperclip `fileInputRef.click()` handler is the `add-files` action (D-02).
- `web/src/chat/attachments/AttachmentChip.tsx` — pill/chip visual pattern.
- `web/src/governance/governanceApi.ts` — `SkillRow` type + `GOV_SKILLS_PATH` (frontend skills type reference).
- `web/src/AppShell.tsx` / `web/src/chat/ExternalStoreChat.tsx` — where `new-chat`/`clear` conversation controls and the send/submit path live (D-02 quick actions + D-01 pinned-skill payload).

### External / prior-phase conventions
- W3C WAI-ARIA APG **Combobox pattern** (https://www.w3.org/WAI/ARIA/apg/patterns/combobox/) — the a11y contract for D-08.
- `.planning/phases/37C-web-voice-lane-inserted/37C-CONTEXT.md` and `.planning/phases/37B-web-artifact-sidebar/37B-CONTEXT.md` — AppShell integration, i18n en+it parity, ≥85% coverage + Playwright conventions to reuse.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **Attachment chip row** (`Composer.tsx` `uploads.items.map(...AttachmentChip...)`): the exact pattern for rendering the removable pinned-skill pill (D-06).
- **Paperclip handler** (`fileInputRef.current?.click()` in `Composer.tsx`): IS the `add-files` quick action — no new mechanism (D-02).
- **`ActiveSkills()` loader snapshot** + `activeSkillRows` projection (`governance_api.go`): reuse the shape/logic for the new per-identity endpoint (D-03).
- **`skill action=use` runtime** (`skill.go` + `prompt.go`): the pinned invocation reuses the existing contract — no new agent tool (D-01).
- **`@assistant-ui/react` `ComposerPrimitive`**: the Composer is built on it; the picker must not break `ComposerPrimitive.Input` Enter-send / paste / drop (D-09).

### Established Patterns
- New authenticated read routes mount behind the whole-mux `RequireAuth` wrap in `serve_webui.go`; capability-gated routes (`governance.read`/`governance.write`) are a stronger, admin-scoped tier — the picker endpoint stays in the plain `RequireAuth` tier (D-03).
- i18n keys added in `en`+`it` with parity checked in CI (37B/37C precedent).

### Integration Points
- Composer → run request: a new field carries the pinned skill name so the server/agent fires `skill action=use` first (D-01) — planner defines the wire path through the external-store submit.
- `new-chat`/`clear` → existing conversation/thread controls in `AppShell`/`ExternalStoreChat` (D-02).

</code_context>

<specifics>
## Specific Ideas

- **Reference screenshot (user-provided):** Claude's own composer `/` menu — menu above the input, "Digita per filtrare" (Type to filter) field, rows with icon + name + subtitle (`add-files / Apri selettore file`), a "Productivity" category header grouping skills (`skill-creator`, `memory-management`, `start`, `task-management`, `update`). This is the visual/interaction target for parity.
- User explicitly asked to research Claude + industrial slash-menu patterns (Notion/Slack/Linear `/`, W3C combobox) before deciding — done; findings folded into D-07/D-08.

</specifics>

<deferred>
## Deferred Ideas

- **Cmd+K global command palette** — a broader command surface; not requested, its own phase.
- **Per-identity skill grants/scoping as a new capability** — if the researcher finds no per-identity skill scoping exists (D-04), *building* it is out of scope for 37D; 37D returns the global snapshot behind `RequireAuth`. Introducing per-identity skill governance would be its own phase.
- **Conversation/artifact sharing** — already Phase 37F.

None of the above expand 37D scope; captured so they are not lost.

</deferred>

---

*Phase: 37D-composer-skill-picker*
*Context gathered: 2026-07-09*
