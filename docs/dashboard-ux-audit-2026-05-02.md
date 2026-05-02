# Aura Dashboard UX Audit - 2026-05-02

## Scope

Audited the authenticated dashboard at `http://localhost:8081` using the live Aura server and a fresh DB-issued dashboard token. The crawl covered:

- `/` Home
- `/wiki`
- `/graph`
- `/sources`
- `/tasks`
- `/skills`
- `/mcp`
- `/pending`
- `/conversations`
- `/summaries`
- `/maintenance`
- `/settings`

Each route was checked at desktop `1440x1000` and mobile `390x844` widths. The audit looked for UX anti-patterns, color contrast failures, small touch targets, horizontal overflow, page crashes, console issues, request failures, and API `4xx`/`5xx` responses.

## Summary

The dashboard is stable under the audited routes: no page crashes, console errors, failed requests, or API error responses were observed. The main UX debt is concentrated in four repeat patterns:

- Mobile tables and the graph canvas overflow the viewport.
- Many controls are below the 44px touch-target floor.
- Several secondary/metadata colors miss WCAG AA contrast.
- Session expiry clears auth and redirects without preserving route/draft state.

## Fix Pass - 2026-05-02

Status: implemented and build-verified.

- Added mobile card layouts for wiki pages, sources, tasks, and conversations while keeping desktop tables on larger screens.
- Changed graph sizing to measure from its container instead of starting at a fixed 800px width, with a mobile node list fallback.
- Raised primary touch targets across the shell, filters, settings controls, task/source/skill actions, maintenance, MCP, pending users, and summaries.
- Replaced native destructive browser dialogs with the shared dashboard confirm/prompt dialog flow.
- Improved contrast for settings metadata/badges, the health status legend, and maintenance helper text.
- Preserved `returnTo` across hard 401 redirects via query string plus `sessionStorage`.
- Added inline accessible reasons for disabled settings actions and conversation export.

Verification:

- `npm run build` passes from `web/`.
- `go test ./...` passes from the repository root.
- Static grep confirms no feature code still calls `window.confirm` or `window.prompt`.
- Browser MCP/Chrome tools were requested but were not exposed by the available MCP tool discovery in this session, so the final browser screenshot pass still needs a manual or MCP rerun.

## Findings

### High - Mobile Data Views Overflow

**Anti-pattern:** Scroll & Viewport, Mobile & Viewport-Specific  
**User harm:** Mobile users see content extending off-screen and must pan horizontally to read tables. This is especially costly on dense operational pages where comparison across columns matters.

Evidence:

- `/wiki` mobile: table width extends to about `500px` on a `390px` viewport.
- `/sources` mobile: table width extends to about `600px`.
- `/tasks` mobile: table width extends to about `700px`.
- `/conversations` mobile: table width extends to about `644px`.

Source references:

- `web/src/components/WikiPanel.tsx:107` uses `min-w-[500px]`.
- `web/src/components/SourceInbox.tsx:162` uses `min-w-[600px]`.
- `web/src/components/TasksPanel.tsx:139` uses `min-w-[700px]`.
- `web/src/components/ConversationsPanel.tsx:127` renders a full table without a mobile card alternative.

Concrete fix:

Use responsive table patterns:

- Keep tables on desktop.
- On mobile, render row cards with the most important fields stacked.
- If a table must remain, wrap it in an explicitly labeled horizontal scroll region with visible scroll affordance and sticky first column/header.

### High - Graph Canvas Is Fixed Too Large On Mobile

**Anti-pattern:** Mobile & Viewport-Specific, Visual Rendering  
**User harm:** `/graph` mobile renders almost no useful page text and the canvas is measured at `800px` wide, overflowing a `390px` viewport.

Source references:

- `web/src/components/WikiGraphView.tsx:34` initializes graph size to `{ width: 800, height: 600 }`.
- `web/src/components/WikiGraphView.tsx:36` relies on `ResizeObserver`, but the initial fixed size still leaks into mobile rendering.
- `web/src/components/WikiGraphView.tsx:57` passes the measured width directly into `ForceGraph2D`.

Concrete fix:

Initialize size from the container only after it has a non-zero measurement. Clamp width to the viewport/container and add a mobile fallback state if the graph is not inspectable at small widths. Consider a searchable node list below the canvas on mobile.

### Medium - Touch Targets Are Too Small Across Core Controls

**Anti-pattern:** Accessibility as UX  
**User harm:** Buttons, filters, inputs, and icon controls are harder to tap, especially on mobile and touch laptops.

Evidence:

- Mobile hamburger is `32x32`.
- Many form controls are about `31-32px` tall.
- Settings reveal buttons are `32x32`.
- Tasks `Cancel`/`Delete` buttons are about `23px` tall.
- Skills `Delete` buttons are about `26px` tall.
- Conversations checkbox is about `13x13`.

Source references:

- `web/src/components/Shell.tsx:35` mobile nav button uses `p-2`, producing `32x32`.
- `web/src/components/WikiPanel.tsx:71`, `:85`, `:90`, `:99` use compact 31-32px controls.
- `web/src/components/TasksPanel.tsx:177`, `:188`, `:294`, `:306`, `:324`, `:339`, `:369`, `:377`, `:386`, `:393` use compact controls.
- `web/src/components/SkillsPanel.tsx:215`, `:297`, `:334` use compact controls.
- `web/src/components/ConversationsPanel.tsx:72`, `:83`, `:93`, `:102`, `:110` use compact controls.
- `web/src/components/SettingsPanel.tsx:144`, `:152`, `:324`, `:381` use 32-36px controls.
- `web/src/components/MaintenancePanel.tsx:69`, `:78`, `:171`, `:179` use compact controls.

Concrete fix:

Create shared dashboard control sizing tokens:

- Default desktop: `min-h-9`.
- Touch/mobile: `min-h-11`, `min-w-11` for icon buttons.
- For tiny visual controls like checkboxes, keep the visual checkbox small but wrap it in a 44px label hit area.

### Medium - Contrast Misses In Status And Metadata Text

**Anti-pattern:** Accessibility as UX, Contrast Failures  
**User harm:** Low-vision users and users on dim displays lose status/context text. These strings often explain what data means, so fading them too far has real task cost.

Observed failures:

- Home source status bar label `ingested: 4` measured about `1.55:1`.
- Settings switch accessible text and key metadata measured about `1.55:1` to `3.67:1`.
- Maintenance empty-state helper text measured about `3.83:1`.
- Light/contrast theme settings source badges previously measured very low for `from .env` because light-blue text is used on a light surface.

Source references:

- `web/src/components/HealthDashboard.tsx:173` maps `ingested` to `bg-primary`; label/legend treatment around `:199` needs a contrast-safe pairing.
- `web/src/components/SettingsPanel.tsx:211` source-badge classes use saturated/tinted text combinations.
- `web/src/components/SettingsPanel.tsx:227` uses `text-muted-foreground/70` for setting keys.
- `web/src/components/MaintenancePanel.tsx:198` uses `text-muted-foreground/70`.

Concrete fix:

Avoid opacity-based text for important metadata. Prefer explicit semantic tokens, for example `--text-muted-aa`, `--status-info-fg`, and `--status-info-bg`, with AA-tested values per theme. Treat status-badge text as normal text and target at least `4.5:1`.

### Medium - Session Expiry Loses Route And Draft State

**Anti-pattern:** Navigation, Routing & State Persistence; Timing & Race Conditions  
**User harm:** If a token expires while a user is editing settings, creating a task, filtering conversations, or writing JSON for an MCP tool, the next API call clears auth and hard-redirects. Unsaved work can be lost.

Source references:

- `web/src/api.ts:58` clears the token.
- `web/src/api.ts:62` hard-redirects to `/login?expired=1`.
- `web/src/components/Login.tsx:27` restores only `location.state.from`, which is not present for hard redirects.

Concrete fix:

Redirect with a `returnTo` query parameter and persist draft state for active forms before redirect. At minimum, preserve `window.location.pathname + window.location.search` in `sessionStorage` before `handle401()` redirects, then restore it after successful login.

### Medium - Disabled Controls Lack Inline Reasons

**Anti-pattern:** Error Handling & Recovery  
**User harm:** Users can see disabled controls but do not always know what must change to enable them.

Source references:

- `web/src/components/SettingsPanel.tsx:143` disables `Test connection` when `LLM_BASE_URL` is empty.
- `web/src/components/SettingsPanel.tsx:151` disables `Save` when there are no dirty settings.
- `web/src/components/ConversationsPanel.tsx:71` disables `Export JSON` when no rows match.

Concrete fix:

Attach `aria-describedby` hints or keep the controls enabled and explain the constraint on click. Good examples:

- "Add an LLM base URL to test the provider."
- "Change at least one setting to save."
- "No conversation rows match the current filters."

### Low - Destructive Actions Use Native Confirm

**Anti-pattern:** Notifications, Interruptions & Dialogs  
**User harm:** Native `window.confirm` is abrupt, inconsistently styled, and cannot show richer context or undo options. It works as a safety net, but it is not ideal for a dashboard with an otherwise polished interaction model.

Source references:

- `web/src/components/TasksPanel.tsx:58` uses `window.confirm` for permanent task deletion.
- `web/src/components/SkillsPanel.tsx:127` uses `window.confirm` for skill deletion.

Concrete fix:

Replace native confirms with the existing dialog primitives. Include the object name, consequence, cancel/default action, and a clearly styled destructive action. For tasks, consider favoring "Cancel" over "Delete" and keeping delete behind an extra confirmation.

## Route Notes

| Route | Desktop | Mobile |
| --- | --- | --- |
| `/` | Stable. One status/legend contrast miss. | Stable. Hamburger target is small. |
| `/wiki` | Stable. Compact filters/buttons. | Table overflows; filters/buttons are small. |
| `/graph` | Stable canvas. | Canvas overflows and page gives little non-canvas fallback. |
| `/sources` | Stable. | Table overflows. |
| `/tasks` | Stable. Cancel/delete buttons are small. | Table overflows; action buttons are small. |
| `/skills` | Stable. Delete buttons are small. | Delete buttons and tab controls are small. |
| `/mcp` | Stable empty state. Server count button is small. | Server count button and hamburger are small. |
| `/pending` | Stable empty state. | Stable; hamburger is small. |
| `/conversations` | Stable. Inputs/checkbox/export are small. | Table overflows; filters and checkbox are small. |
| `/summaries` | Stable empty state. | Stable; hamburger is small. |
| `/maintenance` | Stable empty state. Selects are small; helper text contrast miss. | Same as desktop plus small hamburger. |
| `/settings` | Stable. Many controls below 44px; metadata contrast misses. | Same as desktop; no horizontal overflow observed. |

## Positive Findings

- All audited routes loaded without console errors, page errors, failed requests, or API `4xx`/`5xx` responses.
- Authenticated routing worked with a DB-issued token.
- Most pages have clear headings and useful empty states.
- The shell correctly hides keyboard shortcuts while focus is inside inputs, textareas, selects, or contenteditable nodes.
- Settings now avoid horizontal overflow on mobile despite long secret/config values.

## Suggested Fix Order

1. Build a shared responsive `DataTable`/`MobileRows` pattern and apply it to wiki, sources, tasks, and conversations.
2. Fix graph sizing/fallback on mobile.
3. Raise shared control hit areas to 44px on touch surfaces.
4. Replace opacity-based metadata colors with AA-safe tokens.
5. Preserve route and draft state across auth expiry.
6. Replace native destructive confirms with styled dialogs.

## Notes

This audit used live data, so row counts and specific table overflow widths reflect the database state on 2026-05-02. It did not exhaustively submit every form or invoke every write action; the findings are based on route rendering, visible controls, static source review, and non-destructive browser inspection.
