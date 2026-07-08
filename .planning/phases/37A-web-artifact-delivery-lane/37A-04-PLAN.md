---
phase: 37A-web-artifact-delivery-lane
plan: 04
type: execute
wave: 3
depends_on: ["37A-02", "37A-03"]
files_modified:
  - web/src/chat/displays/types.ts
  - web/src/chat/sseAdapter.ts
  - web/src/chat/sseAdapter_frames.ts
  - web/src/chat/displays/LocalArtifactDisplay.tsx
  - web/src/chat/__tests__/sseAdapter.test.ts
  - web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx
  - internal/webui/dist
autonomous: true
requirements: [WEBART-04]

must_haves:
  truths:
    - "sseAdapter routes an aura.artifact CUSTOM frame into a local_artifact DisplayPayload attached to the tool part by tool_call_id (was: dropped as a no-op)"
    - "LocalArtifactDisplay renders an authenticated download button (<a href=/api/assets/{asset_id}/download download={filename}>) when asset_id is present"
    - "When asset_id is absent (degraded: CLI / no-identity / nil Assets / empty thread / Put error) the card shows today's path chip — unchanged"
    - "When asset_id is present the raw host/container path is NOT rendered — the browser never receives a raw path"
    - "internal/webui/dist is rebuilt from the web/src changes and committed (web-dist-freshness passes)"
  artifacts:
    - path: "web/src/chat/displays/types.ts"
      provides: "DisplayArtifact.asset_id? + mime_type? optional fields"
      contains: "asset_id"
    - path: "web/src/chat/sseAdapter.ts"
      provides: "aura.artifact reducer branch + isArtifactDescriptor guard"
      contains: "aura.artifact"
    - path: "web/src/chat/displays/LocalArtifactDisplay.tsx"
      provides: "download-button branch keyed on asset_id"
      contains: "/api/assets/"
    - path: "internal/webui/dist"
      provides: "rebuilt embedded web bundle carrying the new reducer + card"
  key_links:
    - from: "sseAdapter.ts aura.artifact branch"
      to: "the tool part via tool_call_id"
      via: "ensureTool(state, d.tool_call_id) → writeTool({...part, display: local_artifact payload})"
      pattern: "aura.artifact"
    - from: "LocalArtifactDisplay download button"
      to: "GET /api/assets/{id}/download"
      via: "<a href={`/api/assets/${artifact.asset_id}/download`} download={filename}>"
      pattern: "/api/assets/\\$\\{artifact.asset_id\\}/download"
  prohibitions:
    - "When asset_id is present, the raw path chip MUST NOT render (D-13: replace the chip; no raw host/container path in the browser)"
    - "MUST NOT thread asset_id into the existing aura.display local_artifact payload (which only fires for shell/sandbox exec) — synthesize a fresh local_artifact payload in the aura.artifact branch (Landmine 7)"
    - "MUST NOT add a new display type — extend the existing local_artifact card (D-13); keep the backend/frontend type-string contract explicit (Elysia unknown-type-null footgun)"
    - "MUST NOT ship a stale internal/webui/dist — the bundle is rebuilt + committed with the src change"
---

<objective>
Close the web gap: stop dropping `aura.artifact` in `sseAdapter.ts`, synthesize a `local_artifact` `DisplayPayload` from the descriptor (correlated by the new `tool_call_id`), and extend `LocalArtifactDisplay` to render an authenticated download button targeting 37A-03's `GET /api/assets/{id}/download` when `asset_id` is present — falling back to today's path chip when degraded. Rebuild + commit the embedded `internal/webui/dist` bundle.

Purpose: this is WEBART-04 — the user-visible payoff. It consumes 37A-02's enriched descriptor contract (`asset_id`/`tool_call_id`/`filename`/`size_bytes`/`mime_type`) and 37A-03's download route. Frontend-only + the dist rebuild; no Go behavior change.

Output: `DisplayArtifact.asset_id?`/`mime_type?`; the `aura.artifact` reducer branch + `isArtifactDescriptor` guard; the `LocalArtifactDisplay` download button + i18n key; the rewritten no-op test + extended card test; a fresh `internal/webui/dist`.

## Research corrections honored (Open Q1 RESOLVED — do not regress)
- **Synthesize, don't thread (Landmine 7):** the `aura.display` `local_artifact` payload is produced ONLY by `normalizeCode` for code-producing tools (`display/code.go`) — `send_file` never flows through it. Build a fresh `local_artifact` `DisplayPayload` in the `sseAdapter` `aura.artifact` branch and attach by `tool_call_id`. Do NOT thread `asset_id` into `aura.display`.
- **Rewrite, don't delete (Landmine 5):** `sseAdapter.test.ts:383` ("an unrecognized CUSTOM name (aura.artifact) is a no-op") encodes the OLD drop contract — the legitimate CLAUDE.md exception (the test asserts the behavior we intentionally change). Rewrite it (with a commit-message justification) to assert `aura.artifact` now attaches the card; keep the `aura.display` non-payload no-op test intact.
- **Path replaced when asset_id present (D-13):** the download branch renders instead of the path chip — the browser never receives a raw host/container path in the authenticated case.
- **No structural frame change:** `CustomFrame {type,name,value}` already models any CUSTOM name; only the stale `sseAdapter_frames.ts` doc-comment ("aura.artifact … not modelled") is updated.
</objective>

<execution_context>
@.claude/get-shit-done/workflows/execute-plan.md
@.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37A-web-artifact-delivery-lane/37A-CONTEXT.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-RESEARCH.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-PATTERNS.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-02-SUMMARY.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-03-SUMMARY.md
</context>

<tasks>

<task type="auto">
  <name>Task 1: sseAdapter aura.artifact branch + isArtifactDescriptor guard + DisplayArtifact type + rewrite the no-op test</name>
  <files>web/src/chat/displays/types.ts, web/src/chat/sseAdapter.ts, web/src/chat/sseAdapter_frames.ts, web/src/chat/__tests__/sseAdapter.test.ts</files>
  <read_first>
    - web/src/chat/sseAdapter.ts (the CUSTOM branch :305-316 — the `aura.display` case is the verbatim template: `isDisplayPayload` guard → `ensureTool(state, id)` → `writeTool({...part, display})`; add the `aura.artifact` branch beside it)
    - web/src/chat/displays/types.ts (`DisplayArtifact` :51-55; the `DisplayPayload` union + `local_artifact` variant; `isDisplayPayload`)
    - web/src/chat/sseAdapter_frames.ts (`CustomFrame` :103-107 already models it; the stale doc-comment :109-116 to update)
    - web/src/chat/__tests__/sseAdapter.test.ts (the `frame('CUSTOM')` helper that emits an aura.artifact frame; the no-op test at :383 to REWRITE; the `frame('CUSTOM_DISPLAY')` aura.display test :373 as the attach-by-tool_call_id analog)
    - 37A-PATTERNS.md §"Open Q1 Resolution" (the exact reducer branch + guard + type change + degrade semantics) + §"Test Analogs"
    - .claude/skills/vercel-react-best-practices/SKILL.md + .claude/skills/streaming/SKILL.md (reducer discipline)
  </read_first>
  <action>
    In `web/src/chat/displays/types.ts`, add two optional readonly fields to `DisplayArtifact`: `asset_id?: string` (present → authenticated download button) and `mime_type?: string` (file-icon hint only, NOT trusted as a serve header). In `web/src/chat/sseAdapter.ts`, add an `aura.artifact` branch inside the existing CUSTOM case: guarded by a new `isArtifactDescriptor(v)` (a `v` with a string `tool_call_id` + string `filename`; NOT a `DisplayPayload` — it has no `type`), synthesize a `local_artifact` `DisplayPayload` `{ type: 'local_artifact', tool_call_id: d.tool_call_id, artifact: { filename, ...(size_bytes?), ...(asset_id?), ...(mime_type?), ...(asset_id === undefined && path !== undefined ? { path } : {}) } }` — i.e. include `path` ONLY in the degraded (no asset_id) branch — then `ensureTool(state, d.tool_call_id, ...)` + `writeTool(state, { ...part, display })`. Update the stale `sseAdapter_frames.ts` doc-comment to note aura.artifact is now consumed. REWRITE the `sseAdapter.test.ts:383` no-op test (with a commit-message justification per CLAUDE.md) to assert: an `aura.artifact` frame carrying `tool_call_id`+`filename`+`asset_id` attaches a `local_artifact` display to that tool part; a degraded frame (no `asset_id`, has `path`) attaches a card whose artifact has `path` and no `asset_id`. Keep the `aura.display` non-payload no-op test (:394) intact.
  </action>
  <acceptance_criteria>
    - `web/src/chat/displays/types.ts` `DisplayArtifact` contains `asset_id?` and `mime_type?`
    - `web/src/chat/sseAdapter.ts` contains an `aura.artifact` branch + an `isArtifactDescriptor` guard; the synthesized payload omits `path` when `asset_id` is set
    - the rewritten `sseAdapter.test.ts` asserts `aura.artifact` attaches a `local_artifact` card by `tool_call_id` (NOT a no-op); grep confirms the old `is a no-op` assertion for aura.artifact is gone
    - `cd web && npx vitest run src/chat/__tests__/sseAdapter.test.ts` exits 0
    - `cd web && npx tsc --noEmit` clean (typecheck)
  </acceptance_criteria>
  <verify>
    <automated>cd web &amp;&amp; npx tsc --noEmit &amp;&amp; npx vitest run src/chat/__tests__/sseAdapter.test.ts</automated>
  </verify>
  <done>aura.artifact is consumed (not dropped): the reducer synthesizes a local_artifact payload correlated by tool_call_id, omitting path when asset_id is set; the DisplayArtifact type carries the new optional fields; the no-op test is rewritten to the attach contract.</done>
</task>

<task type="auto">
  <name>Task 2: LocalArtifactDisplay download button (asset_id → authenticated <a download>, else path chip) + i18n + card test</name>
  <files>web/src/chat/displays/LocalArtifactDisplay.tsx, web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx, web/src/i18n locale files holding display.artifact.*</files>
  <read_first>
    - web/src/chat/displays/LocalArtifactDisplay.tsx (the whole file :1-70 — the path-chip branch :57-66 to make conditional on `asset_id`; `useTranslation` :28; `formatSize`; the header doc-comment :6-9 to update)
    - web/src/chat/displays/types.ts (`DisplayArtifact` with the new `asset_id?`/`mime_type?` from Task 1)
    - web/src/chat/displays/DisplayRouter.tsx (:62 — `local_artifact` already routes here; NO router change)
    - the i18n locale files holding `display.artifact.*` (grep for `display.artifact.pathLabel` / `display.artifact.noName` to find them; add a `display.artifact.download` label + aria text in every locale that has the siblings — the app ships Italian + English)
    - web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx (existing card tests to extend)
    - 37A-PATTERNS.md §"Open Q1 Resolution" (the download branch + degrade semantics) + CLAUDE.md Frontend_aesthetics (distinctive, accessible control — not generic)
    - .claude/skills/accessibility/SKILL.md + .claude/skills/frontend-design/SKILL.md
  </read_first>
  <action>
    In `LocalArtifactDisplay.tsx`, branch on `artifact?.asset_id`: when set, render an accessible download control `<a href={`/api/assets/${artifact.asset_id}/download`} download={filename}>` (a real anchor so the browser performs the authenticated same-origin GET; include an aria-label + the new i18n `display.artifact.download` label + the existing file icon; keep the filename + size) and DO NOT render the path chip. When `asset_id` is absent, render the existing path-chip branch unchanged (degraded card). Update the header doc-comment (:6-9) to note the download action now exists when asset_id is present. Add the `display.artifact.download` (+ aria) key to each locale file that has the `display.artifact.*` siblings. Style per CLAUDE.md Frontend_aesthetics — a distinctive, cohesive control, not a generic button; values render React-escaped (no `dangerouslySetInnerHTML`). Extend `LocalArtifactDisplay.test.tsx`: (a) `asset_id` present → an `<a>` with the correct `href` + `download` attribute renders and the raw path chip does NOT; (b) `asset_id` absent → the path chip renders and no `<a download>`; (c) never render a raw host path when `asset_id` is set.
  </action>
  <acceptance_criteria>
    - `LocalArtifactDisplay.tsx` renders `<a href={`/api/assets/${artifact.asset_id}/download`} download=...>` when `asset_id` is set and NOT the path chip in that branch
    - the degraded (no asset_id) branch renders the existing path chip unchanged
    - a `display.artifact.download` i18n key exists in each locale that has `display.artifact.pathLabel`
    - `cd web && npx vitest run src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx` exits 0; the card test asserts (a) href+download present & no path chip when asset_id set, (b) path chip when absent
    - `cd web && npx tsc --noEmit` clean; `cd web && npm run lint` clean (eslint --max-warnings=0)
  </acceptance_criteria>
  <verify>
    <automated>cd web &amp;&amp; npx tsc --noEmit &amp;&amp; npx vitest run src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx</automated>
  </verify>
  <done>LocalArtifactDisplay shows an accessible authenticated download button targeting /api/assets/{id}/download when asset_id is present (raw path replaced), and the unchanged path chip when degraded; i18n + card tests cover both branches.</done>
</task>

<task type="auto">
  <name>Task 3: Rebuild + commit internal/webui/dist + full web suite green (web-dist-freshness / coverage floor)</name>
  <files>internal/webui/dist</files>
  <read_first>
    - web/package.json (the `build` script :13 — `node tokens/generate-theme.mjs && tsc -b && vite build`; `test` :21 — `vitest run --coverage`; the vite config that outputs the embedded bundle to `internal/webui/dist`)
    - 37A-RESEARCH.md §Runtime State Inventory (the `internal/webui/dist` rebuild + commit requirement — QUAL-01 / web-dist-freshness CI job precedent)
    - the web-dist-freshness gate (grep the CI workflow / scripts for `web-dist-freshness` or `webui/dist` to see how staleness is detected)
  </read_first>
  <action>
    Run the web build from `web/` (`npm ci` if node_modules is absent, then `npm run build`) so `internal/webui/dist` is regenerated from the Task 1/2 `web/src` changes. Stage the rebuilt `internal/webui/dist` so it is committed IN THE SAME commit as the `web/src` changes (web-dist-freshness / QUAL-01 goes red on a stale bundle). Run the full web suite (`cd web && npm test` — vitest run --coverage) and confirm the `src/chat` coverage clears the ≥85% web floor. Confirm `internal/webui/dist` differs from HEAD (the bundle actually changed) and that a re-run of `npm run build` is idempotent (no further diff) so the committed bundle is fresh.
  </action>
  <acceptance_criteria>
    - `internal/webui/dist` is rebuilt: `git status` shows it modified (staged for the plan commit); a second `npm run build` produces no further diff (freshness)
    - `cd web && npm test` exits 0 and the `src/chat` (sseAdapter + LocalArtifactDisplay) coverage is ≥85%
    - `cd web && npx tsc --noEmit && npm run lint` clean
    - the web-dist-freshness check (as CI runs it) would pass on the committed tree (dist matches a fresh build of the committed src)
  </acceptance_criteria>
  <verify>
    <automated>cd web &amp;&amp; npm run build &amp;&amp; npm test</automated>
  </verify>
  <done>internal/webui/dist is rebuilt from the src changes and committed atomically with them (web-dist-freshness passes); the full web suite is green with src/chat coverage ≥85%.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| SSE descriptor → rendered DOM | the `aura.artifact` descriptor (server-provided, but includes agent/user-influenced filename + a possible raw host path) becomes a rendered card + a download href |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-XSS-web (raw-path / injection into DOM) | Information Disclosure / Tampering | LocalArtifactDisplay render | mitigate | When `asset_id` is present the raw path chip is NOT rendered (no host/container path in the browser, D-13); values render React-escaped (no `dangerouslySetInnerHTML`); the `<a>` targets the same-origin authenticated route, never a store URL. Proof: card test asserts no raw path when asset_id set (Task 2) |
| T-Type (unknown-type-null footgun) | Tampering | display type-string contract | mitigate | Reuse the existing `local_artifact` type (D-13) — no new display type that could silently render null (Elysia RenderDisplay.tsx:121); DisplayRouter already routes `local_artifact`. Backend/frontend type string stays explicit |
| T-Stale (stale embedded bundle) | Repudiation / Integrity | internal/webui/dist | mitigate | Rebuild + commit the dist atomically with the src (Task 3); web-dist-freshness CI gate blocks a stale bundle (QUAL-01) |
| T-37A-04-SC | Tampering | package installs | accept | Zero new deps (existing vitest/react/i18next). package.json/package-lock.json byte-unchanged. No `[ASSUMED]`/`[SUS]`/`[SLOP]` package — no legitimacy checkpoint |
</threat_model>

<verification>
- `cd web && npx tsc --noEmit && npm run lint` clean.
- `cd web && npm test` (vitest run --coverage) green; `src/chat` coverage ≥85% (web floor).
- The rewritten `sseAdapter.test.ts` asserts the attach contract (not the old no-op); `LocalArtifactDisplay.test.tsx` covers download-when-asset_id + chip-when-degraded + no-raw-path-when-asset_id.
- `internal/webui/dist` rebuilt + committed with the src change (web-dist-freshness passes); package.json/package-lock.json byte-unchanged.

## Manual-Only (from 37A-VALIDATION.md — runs at /gsd-verify-work, not an in-plan gate)
- Real-browser download UX: a live turn where the agent `send_file`s a DOCX → the download button appears → clicking streams the file with the correct name + `attachment` disposition, and the browser never received a raw host/container path. (Full-stack: serve + Garage + a real turn; jsdom/vitest cannot exercise the save-dialog + on-disk bytes.)
- Telegram artifact still delivered on the live Bot API after the descriptor enrichment (cross-channel non-regression).
</verification>

<success_criteria>
The web chat consumes `aura.artifact`, renders an authenticated download button targeting `/api/assets/{id}/download` when `asset_id` is present, and degrades to today's path chip otherwise; the browser never receives a raw path in the authenticated case; the embedded `internal/webui/dist` is rebuilt + committed; the web suite is green at ≥85%. WEBART-04 closed.
</success_criteria>

<output>
Create `.planning/phases/37A-web-artifact-delivery-lane/37A-04-SUMMARY.md` when done.
</output>
