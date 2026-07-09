# Phase 37D: Composer Skill & Command Picker - Pattern Map

**Mapped:** 2026-07-09
**Files analyzed:** 15 (6 new, 8 modified, 1 referenced-only)
**Analogs found:** 14 / 15 exact-or-role-match; 1 partial (combobox core is greenfield — W3C APG)

> RESEARCH.md already cites every seam to file:line. This file STRUCTURES those into a per-file
> analog table + short representative excerpts the planner drops straight into plan actions. Every
> path/line below was re-opened and verified this session (excerpts are current at HEAD).

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| NEW `internal/agui/composer_api.go` | controller (HTTP handler) | request-response (read GET) | `internal/agui/governance_api.go` (`handleSkillsList`/`activeSkillRows`) | exact |
| NEW `internal/agui/composer_api_test.go` | test | request-response | `governance_api_test.go` (`TestGovernanceSkillsStages`) + `auth_test.go` (403/401 matrix) | exact |
| MOD `internal/agui/server_run_request.go` | model (request DTO decode) | request-response | self — existing `Aura.AttachmentIDs` decode (L9-33) | exact (extend in place) |
| MOD `internal/agui/server.go` (`handleRun`) | controller | streaming (SSE run) | self — attachment prepend + `TurnWithModelUserMessage` split (L321-360) | exact (same file/seam) |
| NEW `internal/agui/server_skill_run_test.go` | test | streaming | `internal/agui/server_assets_run_test.go` + `scriptedRunner` harness | exact |
| NEW `cmd/aura/serve_webui_composer.go` | config / route mount | request-response | `cmd/aura/serve_webui_voice.go` (`registerVoiceRoutes` / `voiceCapabilitiesRoute`) | exact |
| MOD `cmd/aura/serve_webui.go` (1-line register call) | config | — | self — `registerVoiceRoutes(...)` / `registerMUSRRoutes(...)` calls (L498-503) | exact |
| NEW `web/src/chat/composer/api.ts` (skills client) | service (fetch client) | request-response | `web/src/governance/governanceApi.ts` (`getJSON`/`fetchSkills`/`SkillRow`) | exact |
| NEW `web/src/chat/composer/SkillPicker.tsx` | component (combobox menu) | event-driven (keyboard) | pill: `AttachmentChip.tsx` (exact); menu shell: `ConversationSidebar.tsx` (partial); combobox/listbox core: **greenfield (W3C APG)** | partial |
| NEW `web/src/chat/composer/__tests__/SkillPicker.test.tsx` | test (unit/a11y) | event-driven | `web/src/chat/__tests__/*Composer*.test.tsx` (Vitest + RTL) | role-match |
| MOD `web/src/chat/Composer.tsx` | component | event-driven | self — `uploads.items.map(...AttachmentChip...)` + `fileInputRef.click()` + `ComposerPrimitive.Input` | exact (same file) |
| MOD `web/src/chat/ExternalStoreChat.tsx` | store / provider | streaming | self — `uploads` seam (`useAttachmentUploads` → `<Composer>` → `onNew`/`streamRun`) | exact (same file) |
| MOD `web/src/chat/sseAdapter.ts` | service (transform) | streaming | self — `StreamRunOptions.attachmentIds` → `aura:{attachment_ids}` envelope | exact (same file) |
| NEW `web/e2e/composer-skills.spec.ts` | test (e2e) | request-response | `web/e2e/artifacts.spec.ts` / `voice.spec.ts` (golden-replay + counted DOM asserts) | role-match |
| MOD `web/src/i18n/resources.ts` | config (i18n) | — | self — existing `chat.composer` / `chat.attachments` en+it groups + parity test | exact (same file) |
| (ref) `web/src/AppShell.tsx` `startNewConversation` | provider (quick action target) | request-response | self — `startNewConversation` (L251-263); D-02 `new-chat` reuses it as-is | exact (reuse, no new logic) |

---

## Pattern Assignments

### NEW `internal/agui/composer_api.go` (controller, request-response)

**Analog:** `internal/agui/governance_api.go` — reuse the row projection VERBATIM; mount ungated (D-03).

**Row struct to reuse** (`governance_api.go:79-85`) — the picker needs `{name, description, type}`; `language` rides free for grouping (Claude's Discretion, D-07):
```go
type skillRow struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Language    string `json:"language,omitempty"`
	ContentHash string `json:"contentHash,omitempty"`
}
```

**Projection to reuse VERBATIM** (`governance_api.go:297-308`):
```go
func activeSkillRows(loaded []skills.Skill) []skillRow {
	rows := make([]skillRow, 0, len(loaded))
	for _, sk := range loaded {
		rows = append(rows, skillRow{Name: sk.Name, Description: sk.Description, Type: sk.Type, Language: sk.Language})
	}
	return rows
}
```

**Handler shape** — mirror the `stageActive` branch of `handleSkillsList` (`governance_api.go:266-288`), minus the stage switch. The provider seam is already wired: `s.governance.Skills.ActiveSkills()` (`SkillsBoardProvider`, `governance_seam.go:37-41`) returns `loader.List()` — the SAME global snapshot the runtime `skill action=use` resolves against (D-04 evidence chain). Nil-provider → 503 (client degrades to empty per D-09):
```go
func (s *Server) handleComposerSkills(w http.ResponseWriter, _ *http.Request) {
	if s.governance.Skills == nil {
		http.Error(w, "skills unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{"skills": activeSkillRows(s.governance.Skills.ActiveSkills())})
}
```
> `writeJSON` / `writeJSONStatus` helpers already exist in this package (used across `handleSkillsList`). Register the route pattern on `Server.Mux` next to the governance routes so `aguiHandler` serves it.

---

### NEW `internal/agui/composer_api_test.go` (test, request-response)

**Analog A — row assertion:** `governance_api_test.go:299-337` (`TestGovernanceSkillsStages`). Reuse the `scriptedSkillsBoard{active: []skills.Skill{...}}` fake + `govServer(GovernanceProviders{Skills: board})` + `doGov(t, s, http.MethodGet, path)` harness; assert the body contains the active skill names and carries `name`/`description`/`type`:
```go
board := &scriptedSkillsBoard{active: []skills.Skill{{Name: "active-one", Description: "a", Type: "instruction"}}}
s := govServer(GovernanceProviders{Skills: board})
rec := doGov(t, s, http.MethodGet, "/api/composer/skills")
// assert 200 + body contains "active-one" + {"name"/"description"/"type"}
```

**Analog B — RequireAuth-not-capability (the D-03 anti-403 assertion):** `auth_test.go:335` (`TestRequireAuth`) + `auth_test.go:506-566` (`TestRequireCapability`). The key differential to prove: an identity WITHOUT `governance.read` still gets its list (200, non-empty), where the governance route would 403 (`RequireCapability` → `http.StatusForbidden`, `auth_test.go:536-537,549-550`). Assert the composer route inherits plain `RequireAuth` only.

---

### MOD `internal/agui/server_run_request.go` (model/DTO, request-response)

**Analog:** self — the existing `attachment_ids` decode. Add a `Skill` field to BOTH structs (the typed struct AND the ext-decode struct). Current shape (L9-33):
```go
type runAgentRequest struct {
	RunAgentInput types.RunAgentInput
	Aura          struct {
		AttachmentIDs []string `json:"attachment_ids"`
		// Skill string `json:"skill"`   ← ADD (mirror both here and in the ext decode below)
	}
}
// ...in decodeRunAgentRequest, the inner ext struct at L25-29 gets the same `Skill string json:"skill"`.
```
> One field each side; the double-decode (typed `RunAgentInput` + raw `ext.Aura`) is the existing idiom — do not restructure it.

---

### MOD `internal/agui/server.go` `handleRun` (controller, streaming)

**Analog:** self — the attachment context-prepend + visible/model split ALREADY in `handleRun` (L321-360). This is the exact Mechanism A seam (D-01, RESEARCH Pattern 4): resolve the pinned skill body, prepend the authority frame to the MODEL message, persist the raw user text as the VISIBLE turn.

**Existing split seam to extend** (`server.go:321-360`):
```go
// buildTurnUserMessage already prepends attachment/doc-catalog context to the MODEL message:
modelUserMsg, code, emsg := s.buildTurnUserMessage(ctx, r, in.ThreadID, req.Aura.AttachmentIDs, userMsg)
// ...
turn := s.run.Turn(ctx, in.ThreadID, modelUserMsg)
if userMsg != nil && modelUserMsg != nil && *userMsg != *modelUserMsg {
	if split, ok := s.run.(modelUserMessageRunner); ok {
		turn = split.TurnWithModelUserMessage(ctx, in.ThreadID, *userMsg, *modelUserMsg)
	}
}
```

**Authority-frame literal to reuse VERBATIM** (`internal/agent/tools/skill_read.go:15,115-119` — the EXACT string `action=use` emits; do not re-invent it):
```go
const useAuthorityFrame = "Follow these skill instructions for the current task:\n\n"
// actionUse tail: body, ok := t.Loader.Body(name); ... return NewResult(ctx, useAuthorityFrame+body)
```

**For 37D:** when `req.Aura.Skill != ""`, resolve `body, ok := loader.Body(req.Aura.Skill)` (or `Get`), and set the model message to `useAuthorityFrame + body + "\n\n" + <existing modelUserMsg>` BEFORE the `TurnWithModelUserMessage` call, so the framed skill leads. Unknown name → treat as no-op/400, never a passthrough (V5 input validation; `loader.Get`/`Body` returns `ok=false`). The runner contract (`internal/runner/turn_model_context.go:17-21`): `TurnWithModelUserMessage` "persists visibleUserMsg as the human-facing user turn while sending modelUserMsg to the LLM" — visible stays raw, model sees the skill first. **No new tool, no runner change.**
> Loader access: the run path already builds a loader (`newSkillTool`, `serve_adapters.go:277-288`); expose `Body(name)`/`Get(name)` to the server via the same provider seam already used for `ActiveSkills()` rather than a second loader instance (Pitfall 2 — one source of truth).

---

### NEW `internal/agui/server_skill_run_test.go` (test, streaming)

**Analog:** `internal/agui/server_assets_run_test.go:13-47` (`TestServerRunPrependsAttachmentBlock`) — copy its structure exactly. The harness is shared: `scriptedRunner{events: textTurn("ok")}` records `gotVisibleUserMsg` / `gotModelUserMsg` via its `TurnWithModelUserMessage` impl (`server_test.go:152-167`); `serveRunWithPrincipal(t, s, body)` drives the request.

**Assertion template** (mirror `server_assets_run_test.go:35-46`):
```go
body := `{"threadId":"...","messages":[{"id":"m1","role":"user","content":"do the thing"}],"aura":{"skill":"skill-creator"}}`
rec := serveRunWithPrincipal(t, s, body)
// assert 200; run.gotVisibleUserMsg == "do the thing" (raw, persisted);
// run.gotModelUserMsg contains useAuthorityFrame + <resolved body> + "do the thing".
```
Add the WEBSKILL-02 divergence guard (Pitfall 2): a test asserting the composer list ⊆ the runtime loader set (both read `loader.List()`).

---

### NEW `cmd/aura/serve_webui_composer.go` + MOD `cmd/aura/serve_webui.go` (config, route mount)

**Analog:** `cmd/aura/serve_webui_voice.go` — the EXACT 600-LOC-split + RequireAuth-only precedent (Pitfall 5). Copy the file skeleton; the `voiceCapabilitiesRoute` bare-`aguiHandler` mount is the RequireAuth-only model (D-03):
```go
const (
	voiceCapabilitiesRoute = "GET /api/voice/capabilities"
)
func registerVoiceRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps) {
	mux.Handle(ttsRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(sttRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(voiceCapabilitiesRoute, aguiHandler) // ← RequireAuth-only, NO capability
}
```

**37D mount — bare `aguiHandler` (inherits `RequireAuth` from the whole-mux wrap; DO NOT wrap in `RequireCapability`):**
```go
const composerSkillsRoute = "GET /api/composer/skills"
func registerComposerRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps) {
	mux.Handle(composerSkillsRoute, aguiHandler) // RequireAuth-only (like imageProxyRoute/voiceCapabilitiesRoute)
}
```

**Contrast — the 403 trap to AVOID** (`serve_webui.go:421`): governance skills IS capability-gated —
`mux.Handle(governanceSkillsRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))`.
The whole-mux wrap that supplies `RequireAuth` is `serve_webui.go:531`: `return agui.RequireAuth(mux, auth), nil`.
Register with a 1-line `registerComposerRoutes(mux, aguiHandler, auth)` call in `serve_webui.go` next to `registerVoiceRoutes(...)` / `registerMUSRRoutes(...)` (L498-503). Method+path-specific pattern wins Go 1.22 longest-pattern precedence over `/api/` and the `/` embed catch-all (Pitfall 4).

---

### NEW `web/src/chat/composer/api.ts` (service, request-response)

**Analog:** `web/src/governance/governanceApi.ts:15,57-63,209-214` — same-origin `getJSON`, a `{skills: [...]}` envelope, and the exact `SkillRow` shape:
```ts
import { getJSON } from '../api/json'; // ALWAYS credentials:'same-origin'; non-200 incl 401 THROWS Error("HTTP <n>")
export const COMPOSER_SKILLS_PATH = '/api/composer/skills';
export interface ComposerSkillRow { readonly name: string; readonly description: string; readonly type: string }
export async function fetchComposerSkills(): Promise<readonly ComposerSkillRow[]> {
  const body = await getJSON<{ skills?: readonly ComposerSkillRow[] }>(COMPOSER_SKILLS_PATH);
  return body.skills ?? []; // degrade-to-no-op (D-09): a throw/empty ⇒ menu never opens
}
```
> The existing `fetchSkills(stage)` (`governanceApi.ts:209-214`) is the line-for-line template — same `body.skills ?? []` fallback.

---

### NEW `web/src/chat/composer/SkillPicker.tsx` (component, event-driven) — PARTIAL analog

The picker decomposes into three concerns; two have exact/partial in-repo analogs, one is greenfield:

1. **Removable pinned-skill pill — EXACT analog** `web/src/chat/attachments/AttachmentChip.tsx:12-35`. Copy the chip: bordered `inline-flex` span + label + a ghost icon `Button` with `aria-label={t(...remove...)}` firing `onRemove`:
```tsx
<span className="inline-flex min-h-10 max-w-full items-center gap-2 rounded-md border border-border bg-surface-2 px-2 py-1 text-xs text-text">
  <span className="min-w-0 truncate">{item.file.name}</span>
  <Button type="button" variant="ghost" size="icon" aria-label={t('chat.attachments.remove', { name: item.file.name })}
          onClick={() => { onRemove(item.localId); }} className="h-8 min-h-8 w-8 rounded-full ...">
    <X data-icon aria-hidden="true" className="size-3.5" />
  </Button>
</span>
```

2. **Menu open/close + Escape + outside-click + above-input portal positioning — PARTIAL analog** `web/src/conversations/ConversationSidebar.tsx:226-288,363-385`: `const [menuOpen, setMenuOpen] = useState(false)`, `menuRef`, an Escape/outside-click `keydown`+`pointerdown` effect, `positionMenuForButton(...)`, and a `role="menu"` popover gated on `menuOpen`. Reuse the OPEN/CLOSE + dismiss mechanics; the trigger differs (text `/` not a button).

3. **ARIA combobox+listbox core — GREENFIELD (no in-repo analog).** No existing file implements `role="combobox"`/`role="listbox"` with `aria-activedescendant` roving + ↑/↓/Enter/Esc/typeahead + JS-scroll-active-into-view. Build to the **W3C WAI-ARIA APG Combobox pattern** (D-08, CONTEXT canonical ref) — the textarea keeps DOM focus with `aria-expanded`/`aria-controls`/`aria-activedescendant`; focus never moves into the list. RESEARCH confirms: *"the only genuinely new logic is the menu component itself."* Planner sources this from CONTEXT D-07/D-08 + RESEARCH Pattern 5, NOT an in-repo copy.

---

### NEW `web/src/chat/composer/__tests__/SkillPicker.test.tsx` (test, event-driven)

**Analog:** the existing Composer unit tests under `web/src/chat/__tests__/` (Vitest + `@testing-library/react`; coverage gate 85, `web/vitest.config.ts:28-32`). Cover: `/`-at-empty-start opens; typing filters; ↑/↓/Enter/Esc; ARIA attrs present (`aria-expanded`/`aria-controls`/`aria-activedescendant`); degrade-to-no-op on empty/unreachable (menu never opens).

---

### MOD `web/src/chat/Composer.tsx` (component, event-driven)

**Analog:** self — three existing seams to extend in place:

**Pill row to mirror** for the pinned skill (`Composer.tsx:199-205`) — render a `SkillPill` next to the attachment chips:
```tsx
{uploads !== undefined && uploads.items.length > 0 ? (
  <div className="flex flex-wrap gap-2">
    {uploads.items.map((item) => (<AttachmentChip key={item.localId} item={item} onRemove={uploads.remove} />))}
  </div>
) : null}
```

**`add-files` quick action** = the existing Paperclip handler (`Composer.tsx:235`) — the menu entry just calls it, no new mechanism (D-02):
```tsx
onClick={() => fileInputRef.current?.click()}
```

**Trigger + key handling** on `ComposerPrimitive.Input` (`Composer.tsx:257-263`). Read composer text reactively via the existing selector idiom (`useAuiState((s) => s.composer.dictation)` at L47; `aui.composer().getState().text` at L80 — so `s.composer.text` is the analog selector). Derive `menuOpen = text.startsWith('/') && skills.length > 0` (D-05/D-09), pass `onChange`/`onKeyDown` (library COMPOSES them via `composeEventHandlers` — RESEARCH Pattern 5); `preventDefault` keys ONLY while open. DO NOT touch `ComposerPrimitive.Root`'s `onPaste`/`onDrop`/`onDragOver` (L193-196) — Enter-send/paste/drop must stay intact (D-09).

**Prop wiring:** extend `ComposerProps` (L31-34) with the pinned-skill state + skills (mirror the optional `uploads?: AttachmentUploads` prop).

---

### MOD `web/src/chat/ExternalStoreChat.tsx` (store/provider, streaming)

**Analog:** self — the `uploads` seam is the exact template for lifting `pinnedSkill` (RESEARCH Pattern 3). Mirror all four touch points:
- **Create state** like `const uploads = useAttachmentUploads(threadId)` (L194) → add `const [pinnedSkill, setPinnedSkill] = useState<...>()` (or a small `usePinnedSkill` hook).
- **Pass to Composer** like `<Composer uploads={uploads} draftPrompt={draftPrompt} />` (L594) → add `pinnedSkill`/`setPinnedSkill`.
- **Read in `onNew`** like `const readyAttachmentIds = uploads.readyAssetIds` (L212) and carry it into `streamRun` (L248-251):
```tsx
await streamRun({
  threadId: runThreadId,
  userText: text,
  attachmentIds: readyAttachmentIds,
  // skill: pinnedSkill?.name,   ← ADD (mirror attachmentIds)
  signal: controller.signal,
  ...(onArtifact !== undefined ? { onArtifact } : {}),
  onUpdate: (assistant, usage) => { /* ...existing... */ },
});
// after send, clear the pill like: if (readyAttachmentIds.length > 0) uploads.clearReady();  (L267)
```
- **`new-chat` quick action** = the existing `startNewConversation` (`AppShell.tsx:251-263`) — thread it down as a callback (D-02). **`clear`** = pure client reset of composer input + pinned pill + pending attachments (RESEARCH Open-Q2 recommendation A3 — NOT conversation deletion).

---

### MOD `web/src/chat/sseAdapter.ts` (service/transform, streaming)

**Analog:** self — add one optional field to `StreamRunOptions` (L465-469) and one property to the `aura` envelope (L580-586):
```ts
export interface StreamRunOptions {
  readonly threadId: string;
  readonly userText: string;
  readonly signal: AbortSignal;
  readonly attachmentIds?: readonly string[];
  // readonly skill?: string;   ← ADD
  // ...
}
// body (L580-586): fold skill into the SAME aura object as attachment_ids:
body: JSON.stringify({
  threadId: opts.threadId,
  messages: [{ id, role: 'user', content: opts.userText }],
  ...(opts.attachmentIds?.length ? { aura: { attachment_ids: opts.attachmentIds } } : {}),
  // → merge into one aura object carrying { attachment_ids?, skill? } when either is set
}),
```
> Keep it ONE `aura` object when both are present (do not emit two `aura` keys). The server decodes `req.Aura.Skill` alongside `req.Aura.AttachmentIDs`.

---

### NEW `web/e2e/composer-skills.spec.ts` (test, e2e)

**Analog:** `web/e2e/artifacts.spec.ts:1-55` (and `voice.spec.ts`). Copy the golden-replay harness: `gotoAuthenticated` from `./auth`, mock `/api/composer/skills` + the `/agent/run` SSE at the page-network layer (`sseFromFrames` idiom), and assert COUNTED DOM/route facts guarded `> 0` (no-skip-as-green, CLAUDE.md). Flow: open `/` menu → filter → select skill → pill appears → send fires the run carrying `aura.skill`; plus `new-chat`/`clear` behavior.

---

### MOD `web/src/i18n/resources.ts` (config, i18n)

**Analog:** self — the existing `chat.composer` / `chat.attachments` key groups (en at L63/L70, it at L350/L357). Add `composer.skillPicker.*` (menu labels, "Type to filter", group headers, pill aria, quick-command labels) to BOTH `en` and `it` under the `chat` namespace. Parity is CI-enforced by `web/src/i18n/__tests__/resources.parity.test.ts` — miss one language and it fails (Pitfall 7).

---

## Shared Patterns

### Auth mounting — plain RequireAuth (NO capability)
**Source:** `cmd/aura/serve_webui_voice.go:38` (`voiceCapabilitiesRoute` bare `aguiHandler`) + `cmd/aura/serve_webui.go:407` (`imageProxyRoute`), whole-mux wrap at `serve_webui.go:531`.
**Apply to:** the new `GET /api/composer/skills` route. This is the single most important cross-cutting decision (D-03) — the governance-skills sibling at `serve_webui.go:421` wraps `RequireCapability(governanceReadCapability)` and is the 403 trap to avoid.

### Active-skills row projection (one source of truth)
**Source:** `internal/agui/governance_api.go:297-308` (`activeSkillRows`) + `skillRow` (L79-85) + `SkillsBoardProvider.ActiveSkills()` (`governance_seam.go:37-41`).
**Apply to:** `composer_api.go`. Reuse VERBATIM so the picker, governance board, and runtime `skill action=use` all read `loader.List()` (D-04; guards Pitfall 2).

### `aura` run-envelope one-field extension
**Source:** `web/src/chat/sseAdapter.ts:580-586` (client) ↔ `internal/agui/server_run_request.go:9-33` (server decode).
**Apply to:** `sseAdapter.ts` + `server_run_request.go`. `attachment_ids` is the exact precedent — add `skill` symmetrically, one field each side.

### Server-side authority-framed prepend (Mechanism A — zero runner change)
**Source:** `internal/agent/tools/skill_read.go:15,115-119` (`useAuthorityFrame` + `actionUse` output) + `internal/agui/server.go:355-360` + `internal/runner/turn_model_context.go:17-21` (`TurnWithModelUserMessage`).
**Apply to:** `server.go` `handleRun`. Reuse the EXACT string `action=use` emits; prepend it to the model message; persist raw visible turn. No new tool, no runner seam (RESEARCH Open-Q1 recommendation; A2).

### Same-origin JSON fetch client
**Source:** `web/src/governance/governanceApi.ts:15,209-214` (`getJSON`, `body.skills ?? []`).
**Apply to:** `web/src/chat/composer/api.ts`. Non-200 (incl 401) throws → the picker degrades to empty (D-09).

### i18n en+it parity
**Source:** `web/src/i18n/resources.ts` groups + `web/src/i18n/__tests__/resources.parity.test.ts`.
**Apply to:** every new `composer.skillPicker.*` string (D-10).

---

## No / Partial Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `web/src/chat/composer/SkillPicker.tsx` (combobox/listbox CORE) | component | event-driven | No file in `web/src` implements a `role="combobox"`+`role="listbox"` with `aria-activedescendant` roving + ↑/↓/Enter/Esc/typeahead + JS-scroll-active-into-view. The pill (`AttachmentChip.tsx`) and the open/close+dismiss+portal mechanics (`ConversationSidebar.tsx`) ARE analogs, but the keyboard-combobox shell is greenfield. Planner builds it to the **W3C WAI-ARIA APG Combobox pattern** per CONTEXT D-07/D-08 + RESEARCH Pattern 5 — RESEARCH confirms this is "the only genuinely new logic" in the phase. |

Everything else maps to an exact or role-match in-repo analog (most are same-file MODs extending an existing seam).

---

## Metadata

**Analog search scope:** `internal/agui/`, `internal/agent/tools/`, `internal/runner/`, `internal/skills/`, `cmd/aura/`, `web/src/chat/`, `web/src/governance/`, `web/src/chat/attachments/`, `web/src/conversations/`, `web/src/i18n/`, `web/e2e/`.
**Files scanned (opened this session):** governance_api.go, governance_api_test.go, auth_test.go, server_run_request.go, server.go, server_context.go, server_test.go, server_assets_run_test.go, governance_seam.go, skill_read.go, turn_model_context.go, loader.go, serve_webui.go, serve_webui_voice.go, AttachmentChip.tsx, governanceApi.ts, sseAdapter.ts, Composer.tsx, ExternalStoreChat.tsx, AppShell.tsx, ConversationSidebar.tsx, artifacts.spec.ts, resources.ts (+ e2e/i18n directory listings).
**Key cross-cutting insight:** 37D is wiring, not invention — 8 of the 14 mapped files are same-file MODs extending an already-proven seam (`aura` envelope, `TurnWithModelUserMessage` prepend, `uploads` lift, `activeSkillRows` projection, voice-route split). The lone greenfield surface is the ARIA combobox core of `SkillPicker.tsx`.
**Pattern extraction date:** 2026-07-09
