---
phase: 37D-composer-skill-picker
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - prd.md
autonomous: true
requirements: [WEBSKILL-01, WEBSKILL-02, WEBSKILL-03]
must_haves:
  truths:
    - "prd.md documents the WEBSKILL-01..03 requirement group as a named subsection (transcribed from REQUIREMENTS.md) — Amendment #81 (D-11)"
    - "prd.md documents the new authenticated read route GET /api/composer/skills mounted behind plain RequireAuth (NOT governance.read), returning the active-skills rows {name, description, type}"
    - "prd.md documents the D-01 pinned-skill wire path: the client adds a `skill` field to the existing `aura` run-request envelope (symmetric with attachment_ids); the server applies it via Mechanism A — prepending the exact useAuthorityFrame+body string action=use emits to the MODEL user message through the existing TurnWithModelUserMessage context-prepend seam (ZERO runner change, no new agent tool, no new skills source of truth)"
    - "prd.md reconciles WEBSKILL-01/02 'identity-scoped / via the governance skills API' wording with the delivered scope: authentication-scoped GLOBAL active-skills snapshot (no per-identity skill scoping exists in Aura — D-04 verdict), served from the SAME loader.List() snapshot the governance board and the runtime skill tool read (so 'no new source of truth' holds), with per-identity skill scoping explicitly DEFERRED"
    - "prd.md records the quick-command actions add-files/new-chat/clear as pure client UI (no agent round-trip) and the pinned skill as a removable pill above the input (D-02/D-06)"
  artifacts:
    - path: "prd.md"
      provides: "37D PRD-amendment (#81) covering WEBSKILL-01..03 + the composer skill-picker surface + GET /api/composer/skills + the aura.skill envelope field + Mechanism A + the identity-scoped/global reconciliation"
      contains: "WEBSKILL-01"
  key_links: []
  prohibitions:
    - "MUST NOT write any Go source, web/ source, or test file in this plan — this is a docs-only PRD-first gate (D-11); git diff --name-only must show ONLY prd.md"
    - "MUST NOT document per-identity skill filtering as delivered — D-04 verdict is a GLOBAL snapshot behind RequireAuth; per-identity skill scoping is recorded as DEFERRED (out of scope for 37D)"
    - "MUST NOT document reuse of GET /api/governance/skills as the picker source — that route is governance.read-gated and 403s ordinary identities (the D-03 trap); the amendment records a NEW RequireAuth-only GET /api/composer/skills"
    - "MUST NOT document the 'forced first tool call' variant (Mechanism B) as delivered — the amendment records Mechanism A (server context-prepend, zero runner change); Mechanism B is the rejected alternative"
    - "MUST NOT hardcode a stale amendment number — grep prd.md for the true max `Amendment #` (research found #80) and use the next integer (#81); confirm before writing"
---

<objective>
Land the mandatory PRD-amendment commit that gates every 37D code plan (D-11, PRD-first absolute — CLAUDE.md "Senza PRD completo non si scrive una riga di codice"). The composer skill-picker surface is currently undocumented in prd.md: the WEBSKILL-01..03 requirement group, the new `GET /api/composer/skills` endpoint, the `aura.skill` run-request envelope extension, and the Mechanism-A server-side application of the pinned skill. This plan documents them BEFORE any implementation code is written, and reconciles the "identity-scoped / via the governance skills API" phrasing in ROADMAP SC#1 / WEBSKILL-01/02 with the GLOBAL active-skills snapshot behind plain `RequireAuth` that D-03/D-04 actually deliver.

Purpose: Satisfy the PRD-first principle and D-11; establish the architectural record every downstream 37D plan (02–05) builds against and declares `depends_on`.
Output: An amended prd.md committed as a standalone PRD-amendment (#81).
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/ROADMAP.md
@.planning/REQUIREMENTS.md
@.planning/phases/37D-composer-skill-picker/37D-CONTEXT.md
@.planning/phases/37D-composer-skill-picker/37D-RESEARCH.md
</context>

<artifacts_produced>
This plan produces (documentation only — no code symbols):
- **prd.md amendment #81** — a new "Composer Skill & Command Picker (Picker Skill/Comandi) — 37D" subsection adjacent to the 37B/37C web-parity amendments (near the Amendment #78/#79/#80 block, prd.md:~2938-2970) covering:
  1. the **WEBSKILL-01..03** requirement group with acceptance text (transcribed from REQUIREMENTS.md:87-89);
  2. the new authenticated read route **`GET /api/composer/skills`** mounted behind the whole-mux `RequireAuth` (NOT `governance.read`), returning the active-skills rows `{name, description, type}` projected from the loader snapshot — a lean sibling of the governance board's `activeSkillRows`, on a different ungated route;
  3. the **D-01 pinned-skill wire path**: the client folds a `skill` field into the existing `aura` run-request envelope (symmetric with `attachment_ids`); the server decodes `req.Aura.Skill` and applies it via **Mechanism A** — resolving the skill body and prepending the exact `useAuthorityFrame + body` string that `skill action=use` already emits to the MODEL user message through the existing `TurnWithModelUserMessage` context-prepend seam (the visible turn stays the raw user text; the model sees the authority-framed skill first) — **no new agent tool, no new skills source of truth (WEBSKILL-02), no runner change**;
  4. the **D-04 reconciliation**: WEBSKILL-01/02 phrase the list as "identity-scoped / via the governance skills API", but the delivered scope is authentication-scoped over a GLOBAL active-skills snapshot (no per-identity skill scoping exists — the loader is process-global, `NewSkillToolForIdentity` is dormant with zero prod callers, no skill-grant migration exists), served from the SAME `loader.List()` snapshot the governance board and the runtime `skill action=use` read (so "no new source of truth" holds); PERSISTED per-identity skill scoping is explicitly DEFERRED to a future phase;
  5. the **D-02/D-06 UI surface**: the `/`-triggered ARIA combobox menu (skills grouped by category + the quick-command actions add-files/new-chat/clear), the pinned skill shown as a removable pill above the input, and that add-files/new-chat/clear are pure client UI (no agent round-trip).
</artifacts_produced>

<tasks>

<task type="auto">
  <name>Task 1: Amend prd.md with the 37D Composer Skill & Command Picker (WEBSKILL-01..03 + GET /api/composer/skills + aura.skill envelope + Mechanism A + identity-scoped/global reconciliation)</name>
  <files>prd.md</files>
  <read_first>
    - prd.md around lines 2932-2970 — the existing Amendment #78 (37B WEBART) / #79-#80 (37C WEBVOICE) blocks: the "▶ Amendment #N (Phase 37X pre-execution gate …)" blockquote + requirement-group PRD-record shape to mirror and append the 37D amendment adjacently; match the existing heading/blockquote/format conventions AND grep `Amendment #` first to confirm the true max (research found #80 → this is #81; do not hardcode blindly).
    - .planning/REQUIREMENTS.md:87-89 — the locked WEBSKILL-01..03 acceptance text to transcribe faithfully (note WEBSKILL-01 "identity-scoped" + WEBSKILL-02 "via the governance API" phrasing — the reconciliation targets).
    - .planning/ROADMAP.md §"Phase 37D" (lines 506-522) — the Goal + Success Criteria wording ("identity-scoped", "via la governance skills API") to reconcile at the PRD level.
    - .planning/phases/37D-composer-skill-picker/37D-CONTEXT.md D-01..D-11 — the decision record the amendment must reflect (esp. D-01 Mechanism-A pinned invocation, D-03 RequireAuth-not-governance endpoint, D-04 global snapshot + per-identity DEFERRED, D-11 PRD-first) and the Deferred Ideas block (Cmd+K palette; per-identity skill grants).
    - .planning/phases/37D-composer-skill-picker/37D-RESEARCH.md § Summary + § D-04 Evidence Gate + § Pattern 3/4 (D-01 wire path) + § Open Questions Q1/Q3/Q4 — the corrections to carry: reuse the `aura` envelope (one field each side); Mechanism A reuses `useAuthorityFrame + body` + `TurnWithModelUserMessage` (zero runner change); the endpoint is authentication-only (global snapshot); amendment number = next after #80.
  </read_first>
  <action>
    Add a "Composer Skill & Command Picker (Picker Skill/Comandi) — 37D" subsection to prd.md as Amendment #81, adjacent to the existing 37B/37C web-parity amendments (near line 2970). Document, mirroring the #78/#79 structure: (1) the three requirements WEBSKILL-01, WEBSKILL-02, WEBSKILL-03 with their acceptance text transcribed from REQUIREMENTS.md; (2) the NEW authenticated read route `GET /api/composer/skills` — state it mounts behind the whole-mux `RequireAuth` wrap (like `imageProxyRoute`/`GET /api/voice/capabilities`), NOT behind the `governance.read` capability (that admin-scoped gate 403s ordinary identities — the exact trap D-03 exists to avoid), and returns `{skills: [{name, description, type}, ...]}` projected from the active-skills loader snapshot (a lean sibling of the governance board's `activeSkillRows`, reusing the SAME provider, on a different ungated route); (3) the D-01 pinned-skill wire path — the client adds a `skill` field to the existing `aura` run-request envelope alongside `attachment_ids` (`{aura: {attachment_ids?, skill?}}`), the server decodes `req.Aura.Skill`, and applies it via Mechanism A: resolve the skill body from the loader and prepend the exact `useAuthorityFrame` ("Follow these skill instructions for the current task:\n\n") + body string that `skill action=use` already emits to the MODEL user message via the existing `TurnWithModelUserMessage` context-prepend seam (the raw user text stays the persisted/visible turn; the model deterministically receives the framed skill first) — record explicitly that this reuses the existing runtime contract with NO new agent tool, NO new skills store (satisfying WEBSKILL-02's "no new source of truth"), and NO runner change, and that an unknown/empty pinned name is a no-op (the name is only ever a loader key, never a filesystem path); note that Mechanism B (a forced first `skill` tool call rendering a visible tool card) is the REJECTED alternative because it would require a new runner seam; (4) the D-04 reconciliation — WEBSKILL-01/02 phrase the list as "identity-scoped / via the governance skills API", but for 37D the DELIVERED scope is authentication-scoped (RequireAuth) over a GLOBAL active-skills snapshot: state the evidence (the loader is process-global with no identity field; `NewSkillToolForIdentity` is dormant with zero production callers; no skill-grant/scope migration exists), that the picker reads the SAME `loader.List()` snapshot the governance board and the runtime `skill action=use` resolve against (guaranteeing the listed set equals the invocable set — "no new source of truth"), and that a PERSISTED per-identity skill scoping/grant capability is explicitly DEFERRED to a future phase (introducing it is out of 37D scope); (5) the D-02/D-06 UI surface — the `/`-triggered menu opens only when `/` is the first character of an empty composer, renders above the input as an ARIA combobox/listbox (skills grouped by category + the quick-command actions), the selected skill shows as a removable pill above the input (mirroring the attachment chips), and add-files/new-chat/clear are pure client UI actions with NO agent round-trip (add-files reuses the existing Paperclip file-picker; new-chat reuses the existing startNewConversation; clear resets the composer input + pinned pill + pending attachments). Add `GET /api/composer/skills` to the PRD's route/endpoint catalog if one exists. Refactor-on-touch does not apply (docs).
  </action>
  <acceptance_criteria>
    - `grep -q "WEBSKILL-01" prd.md` AND `grep -q "WEBSKILL-02" prd.md` AND `grep -q "WEBSKILL-03" prd.md` succeed.
    - prd.md contains the literal strings `/api/composer/skills` and `aura` envelope `skill` field reference and `useAuthorityFrame` (or its literal "Follow these skill instructions") and `TurnWithModelUserMessage`.
    - prd.md's 37D subsection states the endpoint is `RequireAuth`-only and explicitly NOT `governance.read` (the anti-403 note).
    - prd.md's 37D subsection documents the GLOBAL active-skills snapshot AND records per-identity skill scoping as DEFERRED (the "identity-scoped" wording reconciled) — `grep -q "DEFERRED" prd.md` in-context succeeds and the subsection names the deferred per-identity scoping.
    - prd.md contains an "Amendment #81" (or the true next integer after the grep-confirmed max) header for the 37D block.
    - `git diff --name-only` shows ONLY `prd.md` changed (no Go, no web/, no test file).
  </acceptance_criteria>
  <verify>
    <automated>grep -q "WEBSKILL-01" prd.md && grep -q "WEBSKILL-03" prd.md && grep -q "/api/composer/skills" prd.md && grep -q "TurnWithModelUserMessage" prd.md && grep -q "RequireAuth" prd.md && grep -q "DEFERRED" prd.md && test "$(git diff --name-only | tr -d ' ')" = "prd.md" && echo PRD_AMENDMENT_OK</automated>
  </verify>
  <done>prd.md documents the WEBSKILL-01..03 group, the new RequireAuth-only GET /api/composer/skills route, the aura.skill envelope extension, the Mechanism-A server-side application (useAuthorityFrame + TurnWithModelUserMessage, zero runner change), the identity-scoped→global reconciliation (per-identity DEFERRED), and the D-02/D-06 UI surface, as Amendment #81; only prd.md changed.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| (none new) | Docs-only PRD-amendment; no runtime code, no new endpoint, no new input surface crosses a trust boundary in THIS plan. The threats it DOCUMENTS are mitigated in 37D-02/03/04. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37D-00 | Tampering (spec drift) | prd.md architectural record | mitigate | The amendment records the RequireAuth-not-governance mount + Mechanism A + global-snapshot verdict so downstream plans cannot silently reintroduce the 403 trap or a second skills source of truth |
| T-37D-SC | Tampering | npm/pip/cargo installs | accept | 37D installs NO external packages (RESEARCH § Package Legitimacy Audit: not applicable); no slopcheck surface this phase |
</threat_model>

<verification>
- `grep -q "WEBSKILL-01" prd.md` and `grep -q "/api/composer/skills" prd.md` and `grep -q "DEFERRED" prd.md` succeed.
- The PRD-amendment is a standalone commit landing BEFORE any 37D code commit (D-11). Every code plan (37D-02..05) declares `depends_on: ["37D-01"]`.
</verification>

<success_criteria>
- prd.md records the WEBSKILL-01..03 group, the new RequireAuth-only GET /api/composer/skills route, the aura.skill envelope extension, Mechanism A (reuses useAuthorityFrame + TurnWithModelUserMessage, no runner change / no new tool / no new source of truth), and the identity-scoped→global-snapshot reconciliation (per-identity scoping DEFERRED), as Amendment #81.
- Docs-only: no Go source, web source, package.json, or test file touched.
</success_criteria>

<output>
Create `.planning/phases/37D-composer-skill-picker/37D-01-SUMMARY.md` when done.
</output>
