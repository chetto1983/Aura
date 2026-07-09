# Phase 37D: Composer Skill & Command Picker - Research

**Researched:** 2026-07-09
**Domain:** Web cockpit (React 19 + @assistant-ui/react 0.14.22) composer UX + Go AG-UI run wire-path; skills registry reuse
**Confidence:** HIGH (all claims cited to file:line in the live codebase; the one generic-UX area — slash-menu/ARIA combobox — was pre-researched into CONTEXT D-07/D-08 and is not re-litigated here)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Selecting a skill **invokes it as an explicit tool**, not a soft context hint. The picked skill is pinned to the turn; on send, the run request carries `skill=<name>` and the agent runs `skill action=use name=<name>` as its **first action** (deterministic). Reuses the existing runtime contract (`internal/agent/tools/skill.go` `action=use`; taught in `internal/agent/prompt.go` `<skills>`). Planner must define how the pinned skill name travels composer → run request.
- **D-02:** Menu lists **skills + quick command actions**: skills grouped by category with incremental filter; `add-files` reuses the existing Paperclip `fileInputRef.click()` handler; `new-chat` and `clear` are **net-new client-side UI actions** wired to existing conversation/thread controls in `AppShell`/`ExternalStoreChat`. Quick actions are pure client UI (no agent round-trip); skills produce a pinned invocation.
- **D-03:** **New per-identity endpoint behind plain `RequireAuth`** — do NOT reuse `GET /api/governance/skills` (governance.read-gated → 403 for ordinary identities). Add a lean handler (working name `GET /api/composer/skills`, planner may rename) returning active skills as `{name, description, type}` — reuse the loader snapshot (`ActiveSkills()`), mirror `activeSkillRows` for shape. Mount behind `RequireAuth` (whole-mux wrap), NOT behind `governanceReadCapability`.
- **D-04 (evidence-gated):** Whether the endpoint returns the **global** active-skills snapshot or must **filter per identity** depends on whether per-identity skill scoping exists. Default if none exists: global snapshot behind `RequireAuth`. **→ RESOLVED in this research: verdict (a), no per-identity scoping — see the D-04 section below.**
- **D-05:** `/` opens the picker **only when it is the first character of an empty composer**. `/` typed mid-text is literal. Filter query = text after the leading `/`.
- **D-06:** Pinned skill shown as a **removable pill above the input**, mirroring the existing `AttachmentChip` pattern + `uploads.items` render row. User can still type + remove the pill before sending. No editable `/name` text.
- **D-07:** Menu renders **above** the input; a "Type to filter" field; rows carry **icon + name + optional one-line subtitle**; entries **grouped by category** with section headers.
- **D-08:** Accessibility follows the **W3C APG combobox + listbox** pattern: textarea keeps DOM focus with `aria-expanded`/`aria-controls`/`aria-activedescendant`; keyboard `↑`/`↓`/`Enter`/`Esc` + typeahead; JS scrolls active option into view.
- **D-09:** **Degrade to no-op** when the skills list is empty or the endpoint is unreachable — `/` simply does not open a menu. Preserve the Composer's existing paste/drop/Enter-to-send behavior (do not intercept Enter when the menu is closed).
- **D-10:** i18n en+it parity for all new strings; web coverage **≥85%**; unit React tests + Playwright e2e.
- **D-11:** 37D requires a **PRD-amendment BEFORE any code** (mirrors 37B-01 / 37C-01). Wave 1 = PRD-amendment gate; no implementation plan may land before it.

### Claude's Discretion
- Endpoint path/name (`/api/composer/skills` vs `/api/skills`) — planner picks, consistent with `/api/...` naming.
- Skill category grouping taxonomy (by `Type`, by frontmatter, or a flat "Skills" group) — decide from loader metadata; source field is open.
- Whether `new-chat`/`clear` live in the same list or a small "commands" group — keep them discoverable via `/`.

### Deferred Ideas (OUT OF SCOPE)
- **Cmd+K global command palette** — its own phase.
- **Per-identity skill grants/scoping as a new capability** — if no per-identity scoping exists (D-04), *building* it is out of scope for 37D; return the global snapshot behind `RequireAuth`.
- **Conversation/artifact sharing** — already Phase 37F.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| **WEBSKILL-01** | Typing "/" at the start of a Composer line opens a keyboard-filterable menu listing skills available to the authenticated identity (identity-scoped) with ↑/↓/Enter/Esc + per-row description. | D-04 verdict: the "identity-scoped" list == the **global** active-skills snapshot (no per-identity scoping exists; the runtime skill tool itself is global — §D-04). New `GET /api/composer/skills` behind `RequireAuth` (§Backend Endpoint). `/`-trigger + keyboard nav on `ComposerPrimitive.Input` via `composeEventHandlers` (§Frontend Composer). |
| **WEBSKILL-02** | Selecting an entry injects the skill into the turn per the existing runtime contract; no new source of truth (reuses the skills registry). | Pinned skill rides the existing `aura` run-request envelope (`server_run_request.go`); server applies it via the existing `action=use` authority-frame output through the `TurnWithModelUserMessage` context-prepend seam — **no new agent tool, no new skills store** (§D-01 Wire Path). |
| **WEBSKILL-03** | Accessible (ARIA combobox/listbox), preserves Composer paste/drop/Enter-to-send, degrades to a no-op when the skills API is empty/unreachable; unit + e2e; coverage ≥85%. | `ComposerPrimitive.Input` composes caller `onKeyDown`/`onChange`/`onPaste` with its internal handlers → intercept keys only when menu open, pass-through when closed (§D-05/D-09). Degrade-to-no-op = empty list ⇒ menu never opens. Validation: §Validation Architecture. |
</phase_requirements>

## Summary

37D is a **scoped parity feature built almost entirely from reuse** — one lean new backend GET, one new frontend menu/pill component, and a one-field extension to an *already-existing* run-request envelope. There is no new library, no new database table, no new agent tool, and no new skills source of truth. Every integration point already exists in the codebase and is cited below.

**The D-04 evidence gate is resolved decisively: verdict (a) — NO per-identity skill scoping exists in Aura.** The skills `Loader` is a process-global, mutex-guarded snapshot with no identity field (`internal/skills/loader.go:59-70`); the governance board's `ActiveSkills()` is literally `loader.List()` over global roots (`cmd/aura/serve_governance.go:110`); and — the clinching evidence — the per-identity rooting primitive `NewSkillToolForIdentity` (`internal/skills/identity_root.go:61`) has **zero production callers** (it is defined, unit-tested, and referenced only in Phase-36 planning docs; the live agent-run skill tool `newSkillTool` uses the same global `skillLoaderRoots(cfg)` — `cmd/aura/serve_adapters.go:277-288`). So the picker's global list will exactly match what a pinned `skill action=use` can resolve at run time. No grants table exists in any migration (0001-0035 — the only skill table is `skill_audit`). **The endpoint returns the global active-skills snapshot behind plain `RequireAuth`.**

**Primary recommendation:** Add `GET /api/composer/skills` mounted with plain `mux.Handle(route, aguiHandler)` (inherits `RequireAuth` from the whole-mux wrap, NO `RequireCapability`) returning `activeSkillRows(loader.List())`. Lift a `pinnedSkill` state into `ExternalStoreChat` (mirroring the existing `uploads` seam), render the pill via the `AttachmentChip` pattern, and carry the name on the existing `aura` run envelope (`{ aura: { skill } }`). Apply it server-side via the existing `TurnWithModelUserMessage` context-prepend seam using the exact `useAuthorityFrame + body` string `action=use` already produces — no runner change, deterministic. Gate the whole phase behind a PRD amendment (#81, after 37C's #80).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| List available skills for the picker | API / Backend (`internal/agui`) | — | Skills registry lives server-side (`internal/skills` loader); the browser must not enumerate the filesystem. New `GET /api/composer/skills`. |
| Auth-scope the list to a logged-in identity | Frontend Server / API mount (`cmd/aura/serve_webui*.go`) | — | `RequireAuth` whole-mux wrap already binds the principal; the picker needs authentication only (D-04: no per-identity filtering). |
| `/`-trigger, filter, keyboard nav, a11y | Browser / Client (`web/src/chat`) | — | Pure composer UX on `@assistant-ui/react` `ComposerPrimitive`. |
| Pinned-skill pill + removal | Browser / Client | — | Mirrors the client-side attachment-chip render. |
| Carry pinned skill on send | Browser → API (`sseAdapter.ts` → `server_run_request.go`) | — | Extends the existing `aura` run envelope; one field each side. |
| Deterministically apply the skill first | API / Backend (`internal/agui` + `internal/runner`) | — | Server prepends the authority-framed skill body to the model message via the existing `TurnWithModelUserMessage` seam. |
| `add-files` / `new-chat` / `clear` quick actions | Browser / Client (`AppShell`/`ExternalStoreChat`/`Composer`) | — | D-02: pure client actions, no agent round-trip. |

## D-04 Evidence Gate — VERDICT (a): NO per-identity skill scoping (global snapshot behind RequireAuth)

**This was the blocking question. It is resolved with decisive evidence. The endpoint returns the GLOBAL active-skills snapshot behind plain `RequireAuth`; do NOT build per-identity filtering.**

### Evidence chain

1. **The loader is process-global — no identity field.** `internal/skills/loader.go:59-70`: the `Loader` struct carries `roots`, `ttl`, `bodyCap`, `blocklist`, and a mutex-guarded `snapshot map[string]Skill`. `List()` (`:91`) and `Get()` (`:103`) return the global snapshot; there is no identity/MUSR/principal parameter anywhere in the file. `[VERIFIED: codebase internal/skills/loader.go:59-109]`

2. **The governance board's `ActiveSkills()` is literally `loader.List()`.** `cmd/aura/serve_governance.go:110`: `func (a skillsBoardAdapter) ActiveSkills() []skills.Skill { return a.loader.List() }`, where the loader is built with global roots `skills.NewLoader(skills.Config{Roots: skillLoaderRoots(cfg), ...})` (`:140`). `[VERIFIED: codebase cmd/aura/serve_governance.go:110,140]`

3. **`skillLoaderRoots(cfg)` is global — no identity component.** `cmd/aura/serve_adapters.go:425-427`: `return []string{filepath.Join(cfg.SkillExportDir, ".agents", "skills"), cfg.SkillsDir}`. Two fixed roots, same for every caller. `[VERIFIED: codebase cmd/aura/serve_adapters.go:425-427]`

4. **The LIVE agent-run skill tool uses the same global loader.** `cmd/aura/serve_adapters.go:277-288` (`newSkillTool`): the skill tool the model actually invokes is built with `skills.NewLoader(skills.Config{Roots: skillLoaderRoots(cfg), ...})` — the identical global roots. So a pinned `skill action=use name=X` resolves against the **same global set** the picker would list. The picker's list and the runtime's resolvable set are guaranteed identical. `[VERIFIED: codebase cmd/aura/serve_adapters.go:277-288]`

5. **The per-identity rooting primitive is DORMANT — zero production callers.** `internal/skills/identity_root.go:61` defines `NewSkillToolForIdentity(ctx, base)` which *would* root a non-local identity at `LoaderRoots: []string{base.SkillsDir, userDir}` (`:91`). But a repo-wide search for production callers returns **nothing**: `grep -rn "NewSkillToolForIdentity" --include=*.go | grep -v _test.go | grep -v identity_root.go` → empty. Its only references are its own unit tests (`identity_root_test.go`) and Phase-36 planning docs. The Phase-36 summary confirms this explicitly: *"NewSkillToolForIdentity returns the resolved storage roots… execution/tool-assembly wiring is a later plan; this plan is storage rooting only"* and *"Wiring… NewSkillToolForIdentity into cmd/aura's skill-tool constructor… is a later plan"* (`.planning/phases/36-…/36-07-SUMMARY.md:56,96,102`). `[VERIFIED: codebase grep + internal/skills/identity_root.go:61-93 + 36-07-SUMMARY.md]`

6. **No skill-grant/scope/binding table in any migration.** Migrations `0001`-`0035` contain exactly one skill table: `skill_audit` (`0010`, `0018`). A search for `skill.*(grant|scope|binding)` across `internal/db/migrations/` returns only Postgres `GRANT SELECT…` role-privilege lines on `aura.skill_audit` — no per-identity skill authorization table. `[VERIFIED: codebase internal/db/migrations/ + grep]`

### Consequence for the plan
- The endpoint is **authentication-only**, not authorization-scoped: mount behind the whole-mux `RequireAuth`, NOT behind `governanceReadCapability` (that is exactly the 403 trap D-03 exists to avoid — governance routes are wrapped in `agui.RequireCapability(…, governanceReadCapability)` at `serve_webui.go:419-424`).
- Reuse the existing global snapshot. The cleanest option: **reuse the exact `activeSkillRows(loader.List())` projection** (`internal/agui/governance_api.go:297-308`) but mount it on a *different, ungated* route. This guarantees the picker, the governance board, and the runtime skill tool all read one source of truth (`skillLoaderRoots(cfg)` → `loader.List()`).
- Do NOT introduce `NewSkillToolForIdentity` wiring — that is deferred per CONTEXT and would be scope creep.

## Standard Stack

**No new external packages.** 37D is built entirely from libraries already in the tree. Verified against `web/package.json` and the Go module.

### Core (existing — reuse)
| Library / Module | Version | Purpose | Evidence |
|---|---|---|---|
| `@assistant-ui/react` | 0.14.22 | The Composer is `ComposerPrimitive.Root/Input/Send/Cancel`; the runtime is `useExternalStoreRuntime`. | `[VERIFIED: web/package.json]` + `web/src/chat/Composer.tsx:1`, `ExternalStoreChat.tsx:4-11` |
| `react-i18next` | (installed) | en+it strings via `useTranslation()`; parity CI test. | `[VERIFIED: web/src/chat/Composer.tsx:11]` |
| `lucide-react` | (installed) | Icons (`Paperclip`, `X`, etc.) for rows + pill remove. | `[VERIFIED: web/src/chat/Composer.tsx:2]` |
| `internal/skills` (Go) | in-repo | The `Loader` global snapshot the endpoint reads. | `[VERIFIED: internal/skills/loader.go]` |
| `internal/agui` (Go) | in-repo | Route registration + `activeSkillRows` projection + run handler. | `[VERIFIED: internal/agui/governance_api.go, server.go]` |

### Supporting (existing — reuse)
| Item | Purpose | Evidence |
|---|---|---|
| `getJSON` (`web/src/api/json.ts`) | Same-origin fetch helper (throws on non-200 incl. 401) for the new skills client. | `[VERIFIED: web/src/governance/governanceApi.ts:15]` |
| `AttachmentChip` render pattern | Template for the removable pinned-skill pill. | `[VERIFIED: web/src/chat/attachments/AttachmentChip.tsx:12-35]` |
| `useAttachmentUploads` seam | Template for a lifted `usePinnedSkill`/`useState` in `ExternalStoreChat`. | `[VERIFIED: web/src/chat/ExternalStoreChat.tsx:194]` |
| `draftPrompt` prop plumbing | Precedent for injecting/reading composer text via `aui.composer().setText()`. | `[VERIFIED: web/src/chat/Composer.tsx:62-67]` |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|---|---|---|
| Server-side context-prepend of skill body (Mechanism A) | Forced first `skill` tool-call injected into the run (Mechanism B) | B matches D-01's literal wording + renders a visible tool card, but needs a NEW runner seam to force a first tool call — none exists today. A reuses `TurnWithModelUserMessage` with zero runner change. See §D-01 Wire Path. |
| Reuse `activeSkillRows` projection on a new route | Brand-new projection struct | No benefit; reuse guarantees one source of truth (D-04). |

**Installation:** none. (`npm install` unchanged; no `go get`.)

## Package Legitimacy Audit

**Not applicable — 37D installs no external packages.** All dependencies (`@assistant-ui/react`, `react-i18next`, `lucide-react`, in-repo Go modules) are already present and vendored. No npm/PyPI/crates addition is proposed, so there is no slopcheck/registry surface for this phase. `[VERIFIED: web/package.json unchanged by this phase's design]`

## Architecture Patterns

### System Architecture Diagram (37D data flow)

```
   User types "/" as first char of empty composer
                     │
                     ▼
        ComposerPrimitive.Input (onChange/onKeyDown composed via composeEventHandlers)
                     │  text.startsWith('/')  → derive menuOpen; filter = text.slice(1)
                     ▼
   SkillPicker overlay (ARIA combobox+listbox, renders ABOVE input, D-07/D-08)
      │                                   ▲
      │ GET /api/composer/skills          │ rows: {name, description, type}
      ▼ (once, cached)                    │
   RequireAuth (whole-mux wrap) ──► handleComposerSkills ──► activeSkillRows(loader.List())
                                             (global snapshot — D-04)
      │
      │ user picks a SKILL  ────────────────► pinnedSkill state (lifted in ExternalStoreChat)
      │                                          │  renders removable pill (AttachmentChip pattern, D-06)
      │ user picks add-files ─► fileInputRef.click()      │
      │ user picks new-chat ─► startNewConversation()     │  (pure client, D-02)
      │ user picks clear     ─► clear input/pill (see OQ) │
      │                                                   ▼
      └────────────────────────────────►  onNew(message)  [Composer Send]
                                             │  streamRun({ threadId, userText, attachmentIds,
                                             │             skill: pinnedSkill?.name })
                                             ▼
                              POST /agent/run  body: { threadId, messages,
                                                       aura: { attachment_ids, skill } }
                                             │
                                             ▼
                      decodeRunAgentRequest → runAgentRequest.Aura.Skill
                                             │
                                             ▼
        handleRun: resolve skill body (loader) → prepend useAuthorityFrame+body to MODEL message
                   → run.TurnWithModelUserMessage(convID, visible=userText, model=framed+userText)
                                             │  (visible turn persisted raw; model sees framed skill first)
                                             ▼
                                   AG-UI SSE stream ──► sseAdapter reducer ──► chat
```

### Recommended file layout (new + touched)
```
internal/agui/
├── composer_api.go            # NEW: handleComposerSkills (mirrors governance_api activeSkillRows)
├── server_run_request.go      # TOUCH: add Skill field to Aura envelope (both structs)
├── server.go                  # TOUCH: resolve+apply pinned skill in handleRun (~L323/355)
cmd/aura/
├── serve_webui.go             # TOUCH: const composerSkillsRoute + plain mux.Handle (RequireAuth only)
│                              #        (or a new serve_webui_composer.go to respect 600-LOC cap)
web/src/chat/
├── Composer.tsx               # TOUCH: /-trigger, menu, pill row, onKeyDown/onChange compose
├── ExternalStoreChat.tsx      # TOUCH: lift pinnedSkill state; pass skill to streamRun in onNew
├── sseAdapter.ts              # TOUCH: StreamRunOptions.skill → aura.skill on the body
├── composer/                  # NEW dir: SkillPicker.tsx, SkillPill.tsx, useSkillPicker.ts, api.ts
web/src/i18n/resources.ts      # TOUCH: en+it composer.skillPicker.* keys
web/e2e/
├── composer-skills.spec.ts    # NEW: Playwright e2e (mirror artifacts.spec.ts / voice.spec.ts)
```

### Pattern 1: Mount a new authenticated read route (D-03)
**What:** Add a route behind the whole-mux `RequireAuth` WITHOUT a capability gate.
**When to use:** the composer skills GET.
**Precedent:** `imageProxyRoute`/`graphSchemaRoute` mount plainly, while governance routes wrap in `RequireCapability`. And the self-scoped `GET /api/me` + `GET /api/voice/capabilities` are the exact "RequireAuth-only" precedent (moved into `serve_webui_musr.go` / `serve_webui_voice.go` to respect the 600-LOC cap).
```go
// Source: cmd/aura/serve_webui.go:407,413-414 (plain) vs :419-424 (capability-gated)
mux.Handle(imageProxyRoute, aguiHandler)                 // RequireAuth only (inherited)
mux.Handle(graphSchemaRoute, aguiHandler)                // RequireAuth only
mux.Handle(governanceSkillsRoute,                        // ← the 403 trap to AVOID
    agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
// 37D: mount like the FIRST group, NOT the third:
mux.Handle(composerSkillsRoute, aguiHandler)             // RequireAuth only → any identity gets its list
// whole-mux wrap that supplies RequireAuth:
return agui.RequireAuth(mux, auth), nil                  // serve_webui.go:531
```
**Anti-pattern:** wrapping in `RequireCapability(governanceReadCapability)` — an ordinary identity without the admin grant gets 403 and an empty/broken picker (`[VERIFIED: serve_webui.go:104-105,419-424]`).

### Pattern 2: Reuse the active-skills row projection (D-03)
```go
// Source: internal/agui/governance_api.go:297-308
func activeSkillRows(loaded []skills.Skill) []skillRow {
    rows := make([]skillRow, 0, len(loaded))
    for _, sk := range loaded {
        rows = append(rows, skillRow{Name: sk.Name, Description: sk.Description, Type: sk.Type, Language: sk.Language})
    }
    return rows
}
// skillRow{Name,Description,Type,Language,ContentHash} — the picker needs {name,description,type}; language is a free bonus for grouping (Claude's Discretion).
```
The provider seam is `agui.SkillsBoardProvider.ActiveSkills() []skills.Skill` (`internal/agui/governance_seam.go:38`); the composer endpoint can consume the SAME provider (already wired in `buildGovernanceProviders`) or a thin `loader.List()` adapter. `[VERIFIED: internal/agui/governance_seam.go:38, governance_api.go:297-308]`

### Pattern 3: D-01 pinned-skill wire path — mirror the `uploads` seam
**Client → server envelope already exists.** `streamRun` POSTs `/agent/run` with an `aura` extension object that today carries `attachment_ids`:
```ts
// Source: web/src/chat/sseAdapter.ts:580-586
body: JSON.stringify({
  threadId: opts.threadId,
  messages: [{ id, role: 'user', content: opts.userText }],
  ...(opts.attachmentIds?.length ? { aura: { attachment_ids: opts.attachmentIds } } : {}),
}),
// 37D: add skill to the SAME aura object:  aura: { attachment_ids, skill: opts.skill }
```
**Server decode already exists.** Add a `Skill` field to BOTH structs in `server_run_request.go`:
```go
// Source: internal/agui/server_run_request.go:9-33
type runAgentRequest struct {
    RunAgentInput types.RunAgentInput
    Aura          struct {
        AttachmentIDs []string `json:"attachment_ids"`
        // Skill string `json:"skill"`   ← ADD
    }
}
// ...and the ext decode struct at :25-29 gets the same field.
```
**Lift the pinned-skill state exactly like `uploads`.** `uploads` is created in `ExternalStoreChat` (`useAttachmentUploads(threadId)`, :194), passed to `<Composer uploads={uploads} …>` (:594), and read in `onNew` (`uploads.readyAssetIds`, :212). Do the same: a `pinnedSkill` `useState`/hook in `ExternalStoreChat`, passed to `Composer` for the pill+menu, and read in `onNew` to pass `skill: pinnedSkill?.name` into `streamRun`. `[VERIFIED: ExternalStoreChat.tsx:194,205-266,594]`

### Pattern 4: D-01 server-side application — Mechanism A (RECOMMENDED, zero runner change)
`action=use` is a **pure read that executes nothing**: it returns `useAuthorityFrame + body` (`"Follow these skill instructions for the current task:\n\n" + body`) as text (`internal/agent/tools/skill_read.go:96-120,15`). The server can produce that exact string itself and prepend it to the **model-visible** message using the existing context-injection seam:
```go
// Source: internal/agui/server.go:321-360  +  internal/runner/turn_model_context.go:17-21
// buildTurnUserMessage already prepends attachment/doc-catalog context to the MODEL message,
// then handleRun splits visible-vs-model via TurnWithModelUserMessage:
turn := s.run.Turn(ctx, in.ThreadID, modelUserMsg)
if userMsg != nil && modelUserMsg != nil && *userMsg != *modelUserMsg {
    if split, ok := s.run.(modelUserMessageRunner); ok {
        turn = split.TurnWithModelUserMessage(ctx, in.ThreadID, *userMsg, *modelUserMsg)
    }
}
// runner: "persists visibleUserMsg as the human-facing user turn while sending modelUserMsg to the LLM"
```
For 37D: when `req.Aura.Skill != ""`, resolve the skill body via the loader and set `modelUserMsg = useAuthorityFrame + body + "\n\n" + userText` (the same shape the runner test uses for a knowledge-base block: `model := "<…pinned context…>\n\nUser message:\n" + visible`, `runner_test.go:208-210`). The user's raw text stays the persisted/visible turn; the model deterministically receives the skill instructions first. **No new tool, no runner change, reuses the exact `action=use` output.** `[VERIFIED: server.go:321-360, server_context.go:19, turn_model_context.go:17-21, skill_read.go:96-120, runner_test.go:208-210]`

> **Tradeoff vs D-01's literal wording:** Mechanism A does not render a visible `skill` tool card in the transcript (the effect is byte-identical to `action=use`, but framed server-side). If a visible tool card / literal "first tool action" is required, Mechanism B (forced first tool call) is needed — but no runner seam for that exists today, so it is a larger change. **This is the one genuine design decision for the planner (see Open Questions Q1).**

### Pattern 5: D-05/D-09 — `/`-trigger + key handling without breaking Enter-send
`ComposerPrimitive.Input` extends `react-textarea-autosize`'s `TextareaAutosizeProps` and **composes** caller handlers with its own via `composeEventHandlers` (verified in the installed dist: `onChange`, `onKeyDown`, `onPaste` are each composed; `cancelOnEscape` defaults true; internal `handleKeyPress` handles Enter). So:
- Pass `onChange` (or read `useAuiState((s) => s.composer.text)` reactively — `s.composer.text` exists; cf. `useAuiState((s) => s.composer.dictation)` at `Composer.tsx:47` and `aui.composer().getState().text` at `:80`).
- Derive `menuOpen = text.startsWith('/')` (satisfies D-05: menu opens only when `/` is the first char of the whole composer content; `/` mid-text never starts the string).
- Pass `onKeyDown`: **when `menuOpen`**, handle ↑/↓/Enter/Esc and call `event.preventDefault()` so the library's Enter-send/Escape-cancel do not also fire; **when closed**, do nothing → the library's Enter-send/paste/drop run untouched (D-09).
```tsx
// Source: node_modules/@assistant-ui/react/dist/primitives/composer/ComposerInput.d.ts
//         + ComposerInput.js (composeEventHandlers over onChange/onKeyDown/onPaste)
<ComposerPrimitive.Input
  onChange={handleComposerChange}         // composed with library's internal onChange
  onKeyDown={menuOpen ? handleMenuKeys : undefined}  // preventDefault only while open
  … existing props …
/>
```
Paste/drop are ALSO already on `ComposerPrimitive.Root` (`onPaste`/`onDrop`/`onDragOver`, `Composer.tsx:194-196`) — do not touch them. `[VERIFIED: node_modules/@assistant-ui/react/dist/primitives/composer/ComposerInput.{d.ts,js}, Composer.tsx:47,80,194-196]`

### Anti-Patterns to Avoid
- **Gating the picker endpoint on `governance.read`** — 403 for the exact users it targets (D-03).
- **Building per-identity skill filtering** — no scoping exists; it is deferred (D-04).
- **Inserting an editable `/name` token into the composer** — D-06 chose the pill precisely to avoid on-send text parsing.
- **Intercepting Enter when the menu is closed** — breaks Enter-to-send (D-09).
- **A second skills source of truth** — reuse `loader.List()`; WEBSKILL-02 forbids a new store.
- **Putting the new route on a bare `/api/`** — must be a specific method+path sibling under the `/api/` carve-out or it shadows `/api/integrations/` (`serve_webui.go:170-183` pattern).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---|---|---|---|
| Client→server per-turn extension field | A new POST endpoint / new request type | The existing `aura` envelope in `streamRun` + `server_run_request.go` | Already carries `attachment_ids`; one field each side. `[VERIFIED: sseAdapter.ts:580-586, server_run_request.go]` |
| Deterministic skill application | A new agent tool or runner branch | The existing `useAuthorityFrame+body` output + `TurnWithModelUserMessage` prepend seam | `action=use` already produces the exact string; the prepend seam already exists for attachments. `[VERIFIED: skill_read.go:15,119, server.go:355-359]` |
| Removable pinned-skill pill | A bespoke chip component | The `AttachmentChip` render pattern + `uploads.items.map` row | Same visual language, same a11y remove-button pattern. `[VERIFIED: AttachmentChip.tsx, Composer.tsx:199-205]` |
| add-files action | A new file-picker | `fileInputRef.current?.click()` | Already the Paperclip handler. `[VERIFIED: Composer.tsx:222-235]` |
| new-chat action | A new conversation-create flow | `startNewConversation()` | Already exists in AppShell. `[VERIFIED: AppShell.tsx:251-263]` |
| Skills list source | A new query/store | `loader.List()` / `activeSkillRows` | D-04: one global source of truth. `[VERIFIED: serve_governance.go:110, governance_api.go:297]` |
| Textarea Enter/Escape/paste handling | Custom key plumbing | `ComposerPrimitive.Input` composed handlers | Library composes your handlers; don't reimplement send. `[VERIFIED: ComposerInput dist]` |

**Key insight:** every "hard" part of this phase is already solved somewhere in the tree; 37D is wiring, not invention. The only genuinely new logic is the menu component itself (a11y combobox — pattern locked in D-08) and the ~6 lines of skill-body-prepend in `handleRun`.

## Common Pitfalls

### Pitfall 1: Capability-gated skills route (the D-03 trap)
**What goes wrong:** Reusing `GET /api/governance/skills` or copying its `RequireCapability(governanceReadCapability)` mount → ordinary identities get 403, picker is empty.
**Why:** governance routes are admin-scoped (`serve_webui.go:419-424`); the picker's audience is *non-admin* identities.
**How to avoid:** mount `composerSkillsRoute` with plain `mux.Handle(route, aguiHandler)` like `imageProxyRoute`.
**Warning signs:** e2e as a non-admin identity returns 403 / empty menu.

### Pitfall 2: Picker list diverges from what the runtime can invoke
**What goes wrong:** the picker lists a skill the runtime `action=use` cannot resolve (or vice-versa) → pinned invocation fails.
**Why it can't happen today (but guard it):** both use `skillLoaderRoots(cfg)` → `loader.List()` (D-04 evidence #3/#4). Keep it that way — do NOT introduce `NewSkillToolForIdentity` on only one side.
**How to avoid:** the endpoint reads the SAME provider/loader the run path uses; add an assertion test that the composer list ⊆ the run-time loader set.
**Warning signs:** "skill use: unknown skill" errors after a valid pick.

### Pitfall 3: `/` menu breaks Enter-to-send or literal-slash typing
**What goes wrong:** intercepting Enter/`/` unconditionally.
**How to avoid:** only `preventDefault` keys when `menuOpen`; only open the menu when `text.startsWith('/')` (whole-content), never mid-text (D-05).
**Warning signs:** cannot type a message that contains `/`; Enter opens the menu instead of sending.

### Pitfall 4: New route accidentally shadows `/api/integrations/`
**What goes wrong:** registering a bare `/api/` or a too-broad subtree.
**How to avoid:** register a specific `GET /api/composer/skills` sibling (Go 1.22 method+path precedence), exactly like the governance/graph/image-proxy routes; the `/api/` fallback-exclusion already returns it as a backend route.
**Warning signs:** the integrations proxy or the SPA fallback misroutes. `[VERIFIED: serve_webui.go:74-94,170-183]`

### Pitfall 5: 600-LOC ceiling on `serve_webui.go`
**What goes wrong:** adding the mount + const to `serve_webui.go` pushes it over the CLAUDE.md 600-LOC cap (it was split once already — QUAL-01).
**How to avoid:** follow the established split — put the composer mount in a new `serve_webui_composer.go` (mirror `serve_webui_voice.go` / `serve_webui_musr.go`, registered via a `registerComposerRoutes(mux, aguiHandler, auth)` call from `newServeHandler`). `[VERIFIED: serve_webui.go:494-503]`

### Pitfall 6: aria-activedescendant not auto-scrolled
**What goes wrong:** highlighted option scrolls out of view (browsers don't auto-scroll aria-activedescendant).
**How to avoid:** JS-scroll the active option into view on ↑/↓ (D-08 explicitly calls this out).

### Pitfall 7: i18n parity CI failure
**What goes wrong:** adding en keys without it (or vice-versa) fails `resources.parity.test.ts`.
**How to avoid:** add every `composer.skillPicker.*` key to both `en` and `it` in `web/src/i18n/resources.ts` (chat keys live there). `[VERIFIED: web/src/i18n/resources.ts, __tests__/resources.parity.test.ts]`

## Code Examples

### Reading the composer text reactively (D-05 trigger)
```ts
// Source pattern: web/src/chat/Composer.tsx:47 (useAuiState selector), :80 (getState().text)
const text = useAuiState((s) => s.composer.text);
const menuOpen = uiOpen && text.startsWith('/') && skills.length > 0; // D-09: empty ⇒ never opens
const filter = text.startsWith('/') ? text.slice(1) : '';
```

### New skills API client (mirror governanceApi.ts)
```ts
// Source pattern: web/src/governance/governanceApi.ts:15-18,55-63
import { getJSON } from '../api/json';
export const COMPOSER_SKILLS_PATH = '/api/composer/skills';
export interface ComposerSkillRow { readonly name: string; readonly description: string; readonly type: string }
export function fetchComposerSkills(signal?: AbortSignal): Promise<{ skills: ComposerSkillRow[] }> {
  return getJSON(COMPOSER_SKILLS_PATH, signal); // same-origin, throws on non-200 incl 401
}
```

### Backend handler (mirror handleSkillsList active branch)
```go
// Source pattern: internal/agui/governance_api.go:266-288,297-308
func (s *Server) handleComposerSkills(w http.ResponseWriter, _ *http.Request) {
    if s.governance.Skills == nil {
        http.Error(w, "skills unavailable", http.StatusServiceUnavailable) // D-09 degrade upstream: client treats as empty
        return
    }
    writeJSON(w, map[string]any{"skills": activeSkillRows(s.governance.Skills.ActiveSkills())})
}
```

## Runtime State Inventory

**Not applicable — 37D is a greenfield feature, not a rename/refactor/migration.** No stored data keys, live-service config, OS-registered state, secrets, or build artifacts embed a string being renamed. The one new persisted-adjacent surface is the run-request `aura.skill` field, which is transient per-turn (not stored). Verified: no migration, no datastore key, no env var is introduced or renamed by this phase's design.

## Validation Architecture

*(nyquist_validation is enabled in config.json — this section is required and is lifted into VALIDATION.md.)*

### Test Framework
| Property | Value |
|----------|-------|
| Frontend unit framework | Vitest + @testing-library/react (`web/vitest.config.ts`) `[VERIFIED]` |
| Frontend coverage gate | v8 thresholds statements/branches/functions/lines = **85** (`web/vitest.config.ts:28-32`) `[VERIFIED]` |
| Frontend e2e | Playwright (`web/playwright.config.ts`; specs in `web/e2e/`) `[VERIFIED]` |
| Backend framework | Go `testing` + table-driven; `httptest` for handlers; `-race`; owned-surface ≥85% via `scripts/coverage_gate.sh` `[VERIFIED: CLAUDE.md]` |
| Quick run (web unit) | `cd web && npm test` (= `vitest run --coverage`) `[VERIFIED: web/package.json]` |
| Quick run (backend) | `go test ./internal/agui/ ./internal/skills/` |
| Full e2e | `cd web && npm run test:e2e` (= `playwright test`) `[VERIFIED]` |
| i18n parity | `web/src/i18n/__tests__/resources.parity.test.ts` `[VERIFIED]` |

### Success Criterion → Test Map
| SC / Req | Behavior | Test Type | Automated Command | File (new/existing) |
|---|---|---|---|---|
| SC1 / WEBSKILL-01 | `/` at empty-composer start opens menu; type filters; ↑/↓/Enter/Esc navigate | unit (React) | `cd web && npm test -- SkillPicker` | ❌ Wave 0: `web/src/chat/composer/__tests__/SkillPicker.test.tsx` |
| SC1 / WEBSKILL-01 | endpoint returns global active-skills rows behind RequireAuth | backend unit | `go test ./internal/agui/ -run ComposerSkills` | ❌ Wave 0: `internal/agui/composer_api_test.go` |
| SC1 / WEBSKILL-01 | non-admin identity gets a NON-empty list (not 403) | backend/integration | `go test ./internal/agui/ -run ComposerSkills_RequireAuthNotCapability` | ❌ Wave 0 (auth-matrix assertion; mirror `auth_test.go` 403 cases) |
| SC2 / WEBSKILL-02 | picking a skill pins it; send carries `aura.skill`; server applies authority-framed body first | unit (client) | `cd web && npm test -- sseAdapter` (assert body shape) | ❌ Wave 0: extend `sseAdapter` tests |
| SC2 / WEBSKILL-02 | server prepends framed skill body to model msg, persists raw visible turn | backend unit | `go test ./internal/agui/ -run Run_PinnedSkill` | ❌ Wave 0: `internal/agui/server_skill_run_test.go` (mirror `server_assets_run_test.go`) |
| SC2 / WEBSKILL-02 | picker list ⊆ runtime loader set (no divergence, Pitfall 2) | backend unit | `go test ./internal/agui/ -run ComposerSkillsMatchesRuntime` | ❌ Wave 0 |
| SC3 / WEBSKILL-03 | a11y: aria-expanded/controls/activedescendant; focus stays on input | unit (a11y) | `cd web && npm test -- SkillPicker.a11y` | ❌ Wave 0 (assert ARIA attrs; cf. `graph-a11y.spec.ts` for e2e a11y) |
| SC3 / WEBSKILL-03 | paste/drop/Enter-send preserved when menu closed | unit (React) | `cd web && npm test -- Composer` | ✅ extend existing `web/src/chat/__tests__/Composer*.test.tsx` |
| SC3 / WEBSKILL-03 | degrade-to-no-op on empty/unreachable list | unit (React) | `cd web && npm test -- SkillPicker.degrade` | ❌ Wave 0 |
| SC3 / WEBSKILL-03 | e2e: open→filter→select→pill→send fires invocation; new-chat/clear behavior | e2e | `cd web && npm run test:e2e -- composer-skills` | ❌ Wave 0: `web/e2e/composer-skills.spec.ts` (mirror `artifacts.spec.ts`, `voice.spec.ts`) |
| SC3 / WEBSKILL-03 | coverage ≥85% web + owned-surface Go | gate | `cd web && npm test` ; `bash scripts/coverage_gate.sh` | existing gates |
| D-10 | en+it parity for new keys | unit | `cd web && npm test -- resources.parity` | ✅ existing `resources.parity.test.ts` |

### Sampling Rate
- **Per task commit:** `cd web && npm test -- <touched file>` and/or `go test ./internal/agui/` (+ `-race` on backend packages touched).
- **Per wave merge:** full `cd web && npm test` (coverage) + `go test ./internal/agui/ ./internal/skills/`.
- **Phase gate:** full web unit + `npm run test:e2e -- composer-skills` green + owned-surface Go coverage ≥85% before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/agui/composer_api_test.go` — endpoint rows + RequireAuth-not-capability (WEBSKILL-01).
- [ ] `internal/agui/server_skill_run_test.go` — pinned-skill applied first; visible turn persisted raw (WEBSKILL-02) — model on `server_assets_run_test.go`.
- [ ] `web/src/chat/composer/__tests__/SkillPicker.test.tsx` — trigger, filter, keyboard, a11y, degrade (WEBSKILL-01/03).
- [ ] `web/e2e/composer-skills.spec.ts` — full flow + quick actions (WEBSKILL-03) — model on `artifacts.spec.ts`.
- [ ] extend `sseAdapter` tests for the `aura.skill` body field.
- [ ] en+it `composer.skillPicker.*` keys in `resources.ts` (parity test already exists).

## Security Domain

*(security_enforcement absent ⇒ enabled. This phase adds one authenticated read route and one per-turn field.)*

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---|---|---|
| V2 Authentication | yes | New route inherits the whole-mux `RequireAuth` (`serve_webui.go:531`); no new unauth surface. `[VERIFIED]` |
| V4 Access Control | yes | D-04: authentication-only (no per-identity data crosses — the list is global, non-sensitive skill metadata; body is never sent). Do NOT add an unauthenticated variant. |
| V5 Input Validation | yes | `aura.skill` is a bounded name (`[a-z0-9-]`, 1-64 — `SanitizeName`, `skillParamsSchemaHonest`); the server must resolve it via `loader.Get(name)` and treat an unknown name as a no-op/400, never a passthrough. `[VERIFIED: skill.go:123, loader.go:241]` |
| V6 Cryptography | no | none. |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation |
|---|---|---|
| Non-admin blocked from own picker (the D-03 mistake) | Denial of Service (self) | Plain `RequireAuth` mount, not `governance.read`. |
| Pinned-skill name used to read arbitrary files | Tampering / Info-disclosure | Resolve only via the loader's validated name set (`SanitizeName` grammar + `loader.Get`); the loader already symlink-strips + body-caps + blocklist-scans (`loader.go:144-220`). No path is accepted from the client — only a skill *name*. `[VERIFIED]` |
| Skill body as prompt-injection vector | Tampering | Bodies already pass the load-time NFKC+literal injection blocklist scan in the loader (`loader.go:207-220`); the authority-frame is the SAME one `action=use` uses. No new trust surface. `[VERIFIED]` |
| Oversized `aura.skill` payload | DoS | `handleRun` already `MaxBytesReader`-caps the body (`server.go:286`); the field is a short name. `[VERIFIED]` |

## Environment Availability

**No new external dependencies.** 37D is code/config only against already-present tooling. The e2e tier needs a running authenticated stack (same as 37B/37C), which the project already provisions for Playwright.
| Dependency | Required By | Available | Version | Fallback |
|---|---|---|---|---|
| `@assistant-ui/react` | Composer integration | ✓ | 0.14.22 | — `[VERIFIED: web/package.json]` |
| Vitest + v8 coverage | web unit + ≥85% gate | ✓ | (installed) | — `[VERIFIED: web/vitest.config.ts]` |
| Playwright | e2e | ✓ | (installed) | run e2e in CI against the rebuilt container (37B/37C precedent) `[VERIFIED: web/playwright.config.ts, web/e2e/]` |
| Go toolchain + live stack | backend unit/integration + owned-surface coverage | ✓ | (WSL/CI) | — `[VERIFIED: CLAUDE.md]` |

**Missing dependencies with no fallback:** none.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|---|---|---|
| A1 | `useAuiState((s) => s.composer.text)` exposes the live composer text reactively (inferred from `s.composer.dictation` selector at Composer.tsx:47 + `aui.composer().getState().text` at :80). | Pattern 5 / Code Examples | LOW — if the selector shape differs, fall back to an `onChange` handler on `ComposerPrimitive.Input` (which is confirmed composed). Either way the trigger works. |
| A2 | Mechanism A (server context-prepend) is acceptable as the D-01 "runs skill action=use as its first action" implementation (effect-equivalent, no visible tool card). | Pattern 4 / Open Q1 | MEDIUM — if the planner/user requires a visible `skill` tool card, Mechanism B (forced first tool call) is needed, which requires a new runner seam. Flagged as Q1. |
| A3 | "clear" quick action means "clear the composer input + pinned pill + pending attachments" (pure client), not "delete the current conversation". | Open Q2 | MEDIUM — CONTEXT says "pure client, no agent round-trip" (favors input-clear) but also "wire to existing conversation/thread controls" (could mean `useDeleteConversation`). Needs a one-line planner/user decision. |
| A4 | The 37D PRD amendment is #81 (next after 37C's #79/#80). | D-11 / Open Q3 | LOW — planner must confirm the highest existing amendment number in prd.md before writing (grep `Amendment #`). |
| A5 | Category grouping uses skill `Type` and/or the free `Language` field already on the row (Claude's Discretion). The loader exposes `Name/Description/Always/Type/Language` — no richer "category" field exists. | D-07 / Discretion | LOW — if a richer taxonomy is wanted, it needs new frontmatter (out of scope); a flat "Skills" group + a "Commands" group is the safe default. `[VERIFIED: loader.go:26-34]` |

## Open Questions

1. **D-01 application mechanism (the one real design decision).**
   - What we know: `action=use` is a pure read returning `useAuthorityFrame+body`; the `TurnWithModelUserMessage` prepend seam exists and needs no runner change (Mechanism A). A forced first tool call (Mechanism B) matches D-01's literal wording + shows a tool card but needs a new runner seam.
   - Recommendation: **Mechanism A** (reuse, deterministic, zero runner change). If a visible tool card is a hard requirement, scope Mechanism B as a runner change in the plan. Surface this to the user in discuss/plan.

2. **"clear" quick-action semantics (D-02).**
   - What we know: `startNewConversation` (AppShell.tsx:251) is the clear "new-chat" control (exists). There is NO "clear current thread" control; `useDeleteConversation` (useConversations.ts:209, DELETE /api/conversations/{id}) exists but is a server mutation behind a confirm dialog. CONTEXT says quick actions are "pure client, no agent round-trip."
   - Recommendation: implement `clear` as **clear the composer input + drop the pinned pill + pending attachments** (pure client, matches "no round-trip"); do NOT map it to conversation deletion (that is a server mutation + confirm UX, closer to Telegram `/clear`). Confirm with user.

3. **PRD amendment number + exact insertion point (D-11).**
   - What we know: the 37B amendment is #78, 37C is #79/#80 (`prd.md:2938,2950,2970`). The pattern: a blocking `> **▶ Amendment #N (Phase 37D pre-execution gate …)**` block transcribing WEBSKILL-01..03 + the composer skill-picker surface + the new `GET /api/composer/skills` endpoint.
   - Recommendation: add **Amendment #81** mirroring #78/#79 structure; planner greps `Amendment #` for the true max first.

4. **Endpoint name (Claude's Discretion).** `/api/composer/skills` vs `/api/skills`. Recommendation: `/api/composer/skills` (namespaced, unambiguous vs the governance skills route). Minor.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|---|---|---|---|
| Skills managed only in the admin Governance board (governance.read-gated) | Inline `/` composer picker for any authenticated identity | Phase 37D (this) | Non-admins can invoke skills inline. |
| Per-identity skill storage rooting planned (`NewSkillToolForIdentity`) | Still DORMANT (zero prod callers); runtime skill tool is global | Phase 36 (rooting primitive shipped, wiring deferred) | 37D correctly targets the global snapshot. |

**Deprecated/outdated:** none relevant. (Note: `.claude/skills/assistant-ui/` referenced in config.json was empty at research time — the installed dist under `web/node_modules/@assistant-ui/react` is the authoritative source and was read directly.)

## Sources

### Primary (HIGH confidence — read directly this session)
- `internal/skills/loader.go` — global snapshot; `List()/Get()`; no identity field (D-04).
- `internal/skills/identity_root.go` + repo-wide grep — `NewSkillToolForIdentity` dormant, zero prod callers (D-04).
- `cmd/aura/serve_governance.go`, `serve_adapters.go` — `ActiveSkills()`=`loader.List()`; global `skillLoaderRoots`; live `newSkillTool` uses same global roots (D-04).
- `internal/agui/governance_api.go` — `activeSkillRows`, `skillRow{Name,Description,Type,Language}`, `handleSkillsList` (D-03 shape).
- `cmd/aura/serve_webui.go` — RequireAuth whole-mux wrap (:531), plain vs capability-gated mounts (:407/:419-424), governanceReadCapability (:105), 600-LOC split precedent (:494-503) (D-03).
- `internal/agui/server_run_request.go`, `server.go`, `server_context.go`, `internal/runner/turn_model_context.go`, `internal/agent/tools/skill_read.go`, `internal/agent/prompt.go`, `internal/runner/runner_test.go` — D-01 wire path + Mechanism A seams.
- `web/src/chat/Composer.tsx`, `ExternalStoreChat.tsx`, `sseAdapter.ts`, `attachments/AttachmentChip.tsx`, `AppShell.tsx`, `governance/governanceApi.ts` — D-01/D-02/D-05/D-06/D-09 frontend seams.
- `node_modules/@assistant-ui/react/dist/primitives/composer/ComposerInput.{d.ts,js}` — `composeEventHandlers` over onChange/onKeyDown/onPaste (D-05/D-09).
- `web/vitest.config.ts` (≥85 thresholds), `web/playwright.config.ts`, `web/e2e/`, `web/src/i18n/` + `resources.parity.test.ts`, `web/package.json` (assistant-ui 0.14.22, scripts) — D-10.
- `internal/db/migrations/` (0001-0035) — no skill-grant table (D-04).
- `prd.md:2938,2950,2970` (Amendments #78/#79/#80) — D-11 amendment pattern.
- `REQUIREMENTS.md` (WEBSKILL-01..03), `.planning/phases/36-…/36-07-SUMMARY.md` (NewSkillToolForIdentity deferral).

### Secondary (MEDIUM)
- W3C WAI-ARIA APG Combobox pattern (`https://www.w3.org/WAI/ARIA/apg/patterns/combobox/`) — D-08 a11y contract (pre-researched into CONTEXT; not re-verified this session). `[CITED]`

### Tertiary (LOW)
- none.

## Metadata

**Confidence breakdown:**
- D-04 verdict: HIGH — five independent evidence lines converge (loader struct, ActiveSkills, global roots, dormant primitive, no migration).
- D-01 wire path: HIGH for the seams (envelope, structs, TurnWithModelUserMessage) — MEDIUM on Mechanism A-vs-B being the *chosen* one (design decision, Q1/A2).
- D-03 mounting + row shape: HIGH — exact precedents cited.
- D-05/D-09 key handling: HIGH — verified `composeEventHandlers` in the installed dist.
- D-02 quick actions: HIGH for add-files/new-chat (exist) — MEDIUM on "clear" semantics (Q2/A3).
- D-10 validation: HIGH — configs read directly.

**Research date:** 2026-07-09
**Valid until:** ~2026-08-09 for the codebase claims (stable internal APIs); re-verify assistant-ui behavior if `@assistant-ui/react` is upgraded past 0.14.22.
