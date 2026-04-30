# Aura Implementation Tracker

Track work against `pdr.md` v4.0-next (Standalone Second Brain + PDF Ingestion).

## Slice Order (from PDR §12)

1. **Config**: Mistral OCR keys, model, base URL, limits, feature flag.
2. **Source store** (`internal/source`): source ID, raw file storage, `source.json` read/write, listing.
3. **OCR client** (`internal/ocr`): Mistral `/v1/ocr` client + fake-server tests.
4. **Telegram PDF handler** (`internal/telegram/documents.go`): MIME/size validation, download, store, OCR trigger.
5. **Source tools**: `store_source`, `ocr_source`, `read_source`, `list_sources`, `lint_sources`.
6. **Ingestion** (`internal/ingest`): `ingest_source` pipeline, source summary page, affected-page reindex.
7. **Wiki maintenance**: `append_log`, `rebuild_index`, `list_wiki`, `lint_wiki`.
8. **Reminder/scheduler tools**: SQLite `scheduled_tasks`, `schedule_task`, `list_tasks`, `cancel_task`.
9. **Natural prompt tests**: extend `cmd/debug_tools` or add `cmd/debug_ingest`.
10. **UI**: source inbox, PDF status, wiki graph and health dashboard.

Slices 1–7 must land before any UI work. Slice 8 (reminders) is independent and can land in parallel after slice 1.

## Current State (2026-04-29)

Working tree before this session:

- Embedding config moved to Mistral defaults (`EMBEDDING_BASE_URL=https://api.mistral.ai/v1`, `EMBEDDING_MODEL=mistral-embed`) — `internal/config/config.go`, `internal/config/config_test.go`, `.env.example` modified, not yet committed.
- `cmd/debug_tools/main.go` added (untracked) — natural prompt smoke harness for `write_wiki` / `read_wiki` / `search_wiki` and optional live web tools via `--live-web`.
- New product docs: `docs/picobot-tools-audit.md`, `docs/second-brain-consolidation-strategy.md`, `pdr.md`.
- Branch: `ralph/US-010-observability`.

Existing packages: `budget`, `config`, `conversation`, `health`, `llm`, `logging`, `orchestrator`, `search`, `skill`, `telegram`, `tools`, `tracing`, `wiki`. No `source`, `ocr`, `ingest` yet.

## Slice Status

| # | Slice | Status | Notes |
| - | ----- | ------ | ----- |
| 1 | Config (Mistral OCR) | done | Mistral OCR fields + defaults + tests. |
| 2 | Source store | done | `internal/source` with sha256 dedup, atomic source.json, per-id mutex, kind/status filter. |
| 3 | OCR client | done | `internal/ocr` Mistral client with wire-verified table_format/extract_header/extract_footer; render to PDR §4 ocr.md. |
| 4 | Telegram PDF handler | done | `internal/telegram/documents.go` non-blocking single-message progress, bounded concurrency=2, AfterOCR hook for slice 6. |
| 5 | Source tools | done | `internal/tools/source.go` — store_source, read_source, list_sources, lint_sources, ocr_source. Wired in bot.go. 13 unit tests. |
| 6 | Ingestion | done | `internal/ingest` pipeline + `ingest_source` tool. Auto-ingest wired via `docHandler.AfterOCR`; emits source summary page with [[wiki-link]] note in final Telegram progress message. 10 test funcs (15 cases) + `live_ingest` catch-up test. Live-tested end-to-end via Telegram + catch-up on three sources. |
| 7 | Wiki maintenance | done | `list_wiki`, `lint_wiki`, `rebuild_index`, `append_log` LLM tools wrapping the existing `wiki.Store` primitives. Exported `RebuildIndex`/`AppendLog`. 15 unit tests. |
| 8 | Reminder/scheduler | done | SQLite-backed scheduler with at/daily kinds, reminder + wiki_maintenance task kinds, bootstrapped 03:00 nightly job. Tools: schedule_task, list_tasks, cancel_task. Autonomous goroutine + 4 autonomy tests. |
| 9 | Natural prompt tests for OCR/ingest | done | `cmd/debug_ingest` — 10 LLM-driven scenarios covering source/ingest/wiki-maintenance/scheduler tools. Hermetic temp wiki + temp SQLite. All passing live. |
| 10 | UI | done | All sub-slices shipped (10a + 10b + 10c + 10d + 10e). |
| 10a | UI: read-only HTTP API | done | `internal/api` package. JSON GET endpoints for health rollup, wiki pages/page/graph, source list/detail/ocr/raw, tasks list/detail. Mounted at `/api/` on the existing health server via `healthServer.Mount` + `http.StripPrefix`. 14 unit tests; race clean. |
| 10b | UI: frontend scaffold + wiki/graph views | done | React 19 + Vite SPA in `web/`, copied from sacchi reference and pruned. 5 routes via react-router-dom v7 (HealthDashboard, WikiPanel, WikiPageView, WikiGraphView lazy, SourceInbox, TasksPanel). Built into `internal/api/dist/` and embedded via `//go:embed all:dist`. Listener defaults to `127.0.0.1:8080`. Tray gains "Open Dashboard". QR landing deleted. |
| 10c.1 | UI: browser PDF upload (mini-slice from 10c) | done | `POST /api/sources/upload` runs the same pipeline as Telegram (store → OCR → auto-ingest), gated by new `requireLoopback` middleware. Drop-zone + click-to-pick on `/sources` with sonner per-file toasts. `.env` flipped to `HTTP_PORT=127.0.0.1:8081` so the LAN listener path is also closed. Live-tested with `6MBU00242200.pdf` (224 KB, 1 page) — full pipeline ~1.4 s end-to-end. |
| 10c | UI: write actions (ingest/reocr/cancel/upsert/rebuild/log) | done | 6 loopback-gated POST endpoints + matching dashboard actions. Backend: `internal/api/sources_write.go`, `wiki_write.go`, `tasks_write.go`. Frontend: ingest + re-OCR per-row buttons on `/sources` (Re-OCR shown for stored/failed, Ingest shown for ocr_complete/failed); Cancel button + "+ New task" dialog (one-time `at` or daily HH:MM, reminder/wiki_maintenance, recipient_id field shown only for reminder kind) on `/tasks`; "Rebuild index" button on `/wiki`. 21 new Go tests covering happy paths + every input-validation branch + the loopback gate negative case. SPA rebuilt into `internal/api/dist/`. |
| 10d | UI: bearer auth + Telegram-issued tokens | done | New `internal/auth` package (api_tokens table on the scheduler SQLite file; SHA-256 hashed storage; `Issue`/`Lookup`/`Revoke` + `RequireBearer` middleware). Every `/api/*` route now requires `Authorization: Bearer <token>` — there is no public login endpoint. Tokens are minted via the new `request_dashboard_token` LLM tool, which delivers them out-of-band via Telegram so plaintext never lands in conversation logs. The 10c `requireLoopback` gate retired since auth supersedes it. New endpoints: `GET /api/auth/whoami`, `POST /api/auth/logout`. Frontend: `/login` route (paste-token form), localStorage `aura_token`, Authorization header on every request, 401 → redirect with `?expired=1` hint, Sign-out button in sidebar. 7 router auth tests + 12 store/middleware tests + 5 tool tests. Telegram allowlist remains canonical (re-checked on every authed request). |
| 11a | MCP client + boot wiring | done | New `internal/mcp` package (Picobot-port: stdio + Streamable-HTTP transports, JSON-RPC 2.0, `initialize` → `tools/list` → `tools/call`). New `internal/tools/MCPTool` adapter so MCP tools register as `mcp_<server>_<tool>` in the same registry the LLM sees. Config: `MCP_SERVERS_PATH=./mcp.json` (gitignored runtime, `mcp.example.json` tracked). Bot boot loads servers, warns on connection failures, never fatal; `Bot.Stop()` closes all clients. 5 client tests (HTTP/SSE/error) + 6 config-loader tests + 5 tool-wrapper tests (15 total, race-clean). |
| 11b | Skills + MCP dashboard panels | done | Backend: `GET /api/skills` (list), `GET /api/skills/{name}` (full SKILL.md content with 16k truncation guard), `GET /api/mcp/servers` (per-server transport, tool count, full schema). New `Deps.Skills *skills.Loader` and `Deps.MCP []*mcp.Client` plumbed from the bot. `mcp.Client.Transport()` getter added (returns "stdio" or "http"). Frontend: `/skills` and `/mcp` routes, both bearer-authed; expandable cards with live SKILL.md previews and per-tool input-schema toggles; sidebar gains Sparkles + Plug nav; `g k` / `g m` keyboard chord shortcuts. 12 new Go tests (skills happy/empty/nil-loader/404/bad-name/truncation + mcp empty/populated/nil-client). |
| 11c | skills.sh install + delete (admin gated) | done | New `SKILLS_ADMIN=false` config flag (opt-in). Backend: `GET /api/skills/catalog?q=&limit=` (passthrough to skills.sh via the existing `CatalogClient`), `POST /api/skills/install` (admin-gated; runs `npx skills add <source> [--skill <id>]` from `SKILLS_PATH` with a sanitized env that drops `TELEGRAM_TOKEN`/`MISTRAL_API_KEY` and a 90s timeout), `POST /api/skills/{name}/delete` (admin-gated; refuses traversal + symlinks via `filepath.Rel` containment). New `internal/skills/admin.go`: `NPXInstaller` + `FSDeleter` plus a `IsSkillNotFound` helper so the api package can map filesystem-not-found to a 404. Frontend: `SkillsPanel` rewritten with Local/Catalog tabs; debounced search box; per-row Install button (sonner progress toast → success/failure with truncated `npx` output); per-row Delete with confirm; auto-detected admin-gated banner appears the first time a 403 is seen. 19 new tests (12 install/delete API + 4 catalog + 4 FSDeleter unit including symlink refusal + sanitized env). |
| 11d | Invoke MCP tools from dashboard | done | `POST /api/mcp/{server}/tools/{tool}` — bearer-authed (no extra admin gate; the operator already trusts everything in `mcp.json` because the LLM can call those servers). 60 s context timeout, 64 KiB body cap, 64 KiB output cap. Validates `server` against the loaded MCP-client list and `tool` against the server's advertised tools (404 on unknown). Body: arbitrary JSON object → forwarded as `arguments`; empty body / `null` → `{}`. Tool errors (`isError:true`) come back as `200 {ok:false, is_error:true, error}` so the UI can render them inline; transport / timeout failures arrive as `200 {ok:false, is_error:false, error}`. Frontend: each tool row in `MCPPanel` gains a Run button revealing a JSON textarea (seeded from `input_schema.properties` when available), Invoke action with sonner progress toast, color-coded result panel (success/tool-error/transport). 8 new Go tests (happy path with arg-passthrough verification, empty body, 5 bad-body variants, unknown server, unknown tool, bad tool name, server tool error, transport error, large output truncation). |
| 10e | UI: polish + theme redesign | done | Two waves: **(A) polish** — dark mode default, shadcn `Skeleton` placeholders replace "Loading…" across HealthDashboard / WikiPanel / SourceInbox / TasksPanel; stronger empty-state CTAs (BookText / Calendar icons + helpful copy); ErrorBoundary fires a `sonner.error` toast on top of the inline card; `Shell` component splits desktop sidebar from a mobile slide-over (radix Sheet, < md); global keyboard shortcuts via `useKeyboardShortcuts` (`?` opens help dialog, `g h/w/g/s/t` chord navigation). Backend `/api/health` extended with `process` block (version, git_revision, started_at, uptime_seconds) — git revision read once via `runtime/debug.ReadBuildInfo`. **(B) theme redesign from logo** — palette derived from the new orb logo (deep navy disc, electric cyan-blue arrow A); rewrote light + dark + contrast shadcn token blocks in oklch; ambient aurora radial-gradient on dark/contrast bodies; new inline-SVG `BrandMark` (sidebar) + larger glowing `LoginBrandMark` (login page); active-nav items get a brand glow (`bg-primary/10 ring-primary/20 shadow-[0_0_20px_-8px_var(--primary)]`); cards gain a hover top-stripe gradient + `hover:border-primary/30`. Bundle: 521 KB JS / 161 KB gz, 105 KB CSS / 18 KB gz. |

## Session Log

### 2026-04-30 — Slice 11d (Invoke MCP tools from dashboard)

- One atomic commit. Phase 11 complete: skills + MCP fully reachable from the dashboard, end-to-end.
- **Auth model decision**: bearer auth only, no `MCP_ADMIN` flag. Reasoning: MCP servers are opt-in via `mcp.json` — if the operator wired one, the LLM can already invoke its tools through the agent loop, so a separate dashboard gate would be theatre. Bearer auth + Telegram allowlist re-check (existing `RequireBearer` middleware) is the same gate every other write endpoint uses.
- **Backend** (`internal/api`):
  - `mcp_write.go` (new) — `handleMCPInvoke` resolves the client by name (404 on miss), checks the tool is advertised by that server (404 on unadvertised), validates the URL-path tool name against `^[A-Za-z0-9_.\-]{1,128}$`, parses the body (caps at 64 KiB; empty/`null` → `{}`; non-object → 400), and calls `client.CallTool` with a 60 s `context.WithTimeout`. Distinguishes server-reported `isError:true` (the client returns these as `tool error: …`) from transport/timeout failures and surfaces both as `200 {ok:false}` with the right `is_error` flag so the UI can render them inline. Output is clipped at 64 KiB.
  - `types.go` — `MCPInvokeResponse{ok, is_error?, output?, error?}`.
  - `router.go` — `POST /mcp/{server}/tools/{tool}` registered after the existing read endpoints.
- **Frontend** (`web/src/components/MCPPanel.tsx`):
  - `ToolRow` gains a Run button that toggles a JSON textarea + Invoke action. The textarea is seeded by `seedArgsFromSchema(input_schema)` — for each `properties` entry, emits `0` for integer/number, `false` for boolean, `[]` for array, `{}` for object, `""` for the rest. Operators can clear it back to `{}` if the seed is wrong.
  - Submit parses the body locally (rejects non-object JSON before the network call), invokes via `api.invokeMCPTool`, and renders a `ToolResult` panel: green for `ok`, amber for `is_error`, red for transport. Output (or error message) is shown in a scrollable monospace block capped at `max-h-64` so a chatty tool can't blow the layout.
  - `web/src/api.ts` — `api.invokeMCPTool(server, tool, args)`. `web/src/types/api.ts` — `MCPInvokeResponse`.
- **Tests**: 8 new in `mcp_write_test.go`:
  - `TestMCPInvoke_HappyPath` — sends `{q:"hello", n:42}`, asserts the fake server received the args nested under `"arguments"`.
  - `TestMCPInvoke_EmptyBodyMeansNoArgs` — empty POST body → `arguments:{}`.
  - `TestMCPInvoke_RejectsNonObjectBody` — table-driven for `"string"`, `42`, `[]`, `{`, `not json`. All return 400.
  - `TestMCPInvoke_UnknownServer` / `_UnknownTool` / `_BadToolName` — 404 / 404 / 400.
  - `TestMCPInvoke_ServerToolError` — fake server returns `isError:true`; response is `200 {ok:false, is_error:true}`.
  - `TestMCPInvoke_TransportError` — fake server returns 500 to `tools/call`; response is `200 {ok:false, is_error:false}`.
  - `TestMCPInvoke_ClipsLargeOutput` — output past `mcpInvokeMaxOutput` ends with `[truncated]`.
- **Verification**: `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./internal/{api,mcp}/...` all pass; `npm run lint`, `npx tsc --noEmit`, `vite build` clean.
- Bundle: 544 KB JS / 166 KB gz; 110 KB CSS / 19 KB gz (~4 KB JS / ~1 KB CSS net growth from 11c).
- Files touched: `internal/api/mcp_write.go` (new), `internal/api/mcp_write_test.go` (new), `internal/api/router.go`, `internal/api/types.go`, `web/src/types/api.ts`, `web/src/api.ts`, `web/src/components/MCPPanel.tsx`, `internal/api/dist/*` (rebuilt), `docs/implementation-tracker.md`.
- Manual verification still owed by user: with at least one MCP server in `mcp.json` (e.g. `npx mcp-server-fetch`), open `/mcp` → expand the server → click Run on a tool → seeded JSON appears in the textarea → click Invoke → green panel with the tool's text content. For a failing tool (e.g. fetch with a bad URL), expect the amber `is_error` panel.
- Phase 11 wrap-up: 11a (MCP client + boot) → 11b (read-only dashboard panels) → 11c (skills.sh install + delete) → 11d (MCP tool invocation). All four shipped today, all behind the existing bearer-auth (with `SKILLS_ADMIN` adding an extra gate over arbitrary code execution).

### 2026-04-30 — Slice 11c (skills.sh install + delete, admin gated)

- One atomic commit. `/skills` page now has working catalog browse + install + delete behind a config-flag gate.
- **Threat model**: `npx skills add <src>` runs arbitrary code from the catalog. Treat the install endpoint as a privileged operation. Hardening:
  - Off by default. New `SKILLS_ADMIN` env var (default `false`); the API returns 403 unless explicitly enabled. Frontend renders an inline banner explaining the toggle the first time a 403 is observed.
  - `source` is constrained by `^[A-Za-z0-9@:._/\-]{1,200}$` and rejects any segment containing `..`. We never invoke a shell — `os/exec` argv-only.
  - Subprocess env is sanitized to PATH + node lookup + npm config vars (drops `TELEGRAM_TOKEN`, `MISTRAL_API_KEY`, etc.) so install logs / errors can't leak Aura secrets to npm/skills.sh.
  - Install runs with a 90-second `context.WithTimeout` ceiling and `cwd = SKILLS_PATH`.
  - Delete runs `filepath.Rel` containment check after `filepath.Join` (catches `..`, absolute paths, Windows separators) and refuses symlinks via `os.Lstat`.
  - The deleter never recurses outside the configured skills directory.
- **Backend** (`internal/api`):
  - `types.go` adds `SkillCatalogItem`, `SkillInstallResponse`, `SkillDeleteResponse`.
  - `skills_catalog.go` (new) — proxies `skills.CatalogClient.Search` with `q` + `limit` query params. Returns `[]` for nil-client (so the frontend always sees an array). 502 on upstream failure.
  - `skills_write.go` (new) — `handleSkillInstall` validates the body, applies the admin gate, calls `Deps.SkillsInstaller.Install` with a 90s context. Truncates output at 2 KiB before returning. `handleSkillDelete` applies the gate and maps `ErrSkillNotFound` to 404, generic errors to 500.
  - `router.go` — `Deps` gains `SkillsCatalog`, `SkillsInstaller` (interface), `SkillsDeleter` (interface), `SkillsAdmin bool`. Three new routes (`GET /skills/catalog`, `POST /skills/install`, `POST /skills/{name}/delete`).
- **Skills runtime** (`internal/skills/admin.go`, new):
  - `NPXInstaller`: shells `npx skills add <src> [--skill <id>]` via `os/exec.CommandContext`. Picks `npx.cmd` on Windows. Sanitized env keeps PATH/PATHEXT/HOME/USERPROFILE/APPDATA/LOCALAPPDATA/TEMP/TMP/NODE_PATH/NPM_CONFIG_*; drops everything else.
  - `FSDeleter`: rejects empty names, traversal, symlinks, non-directories. Returns a package-internal sentinel for not-found that `IsSkillNotFound` reports on. Bot bridges this to `api.ErrSkillNotFound` via a small adapter to avoid an import cycle.
- **Config**: `internal/config` adds `SkillsAdmin bool` from `SKILLS_ADMIN` (default false). `.env.example` and `.env` both updated.
- **Bot wiring** (`internal/telegram/bot.go`): hoists `skillsCatalog` to a variable shared between the LLM tool and the API; constructs `NPXInstaller` + `FSDeleter` unconditionally so flipping the gate at runtime needs only a restart, not a rebuild; passes everything through `api.NewRouter`. New `skillsDeleterAdapter` translates the deleter's not-found sentinel.
- **Frontend** (`web/src/components/SkillsPanel.tsx`):
  - Tabs: Local (existing accordion + per-row Delete) and Catalog (search + install).
  - `useDebounce(value, 350ms)` proper-effect implementation throttles skills.sh queries to one per 350 ms of typing.
  - Install / Delete buttons surface `sonner.loading → success/error` toasts. The 403 path triggers a one-line `setAdminGated(true)` so the user sees the gate banner without having to read the network tab.
  - Empty Local state now points to the Catalog tab as the first install option.
  - SPA bundle: 540 KB JS / 165 KB gz; 109 KB CSS / 19 KB gz (~7 KB JS / ~2 KB CSS net).
- **Tests**: 19 total — 9 install (admin-off / nil-installer / empty source / 5 bad-source variants / bad skill_id / happy / failure-surfaces-output / output truncation), 5 delete (admin-off / bad-name / not-found / happy / generic error), 4 catalog passthrough (happy / query filter / nil client / upstream 500), and 4 in `internal/skills` (FSDeleter remove, not-found, traversal cases, symlink refusal — symlink test self-skips on platforms without unprivileged symlink support, e.g. Windows). One sanitized-env test in the same suite verifies secret env vars don't reach the subprocess. Race-clean.
- **Verification**: `go build ./...` clean, `go vet ./...` clean, `go test ./...` PASS, `go test -race ./internal/{api,skills}/...` clean. Frontend: `npm run lint` clean, `npx tsc --noEmit` clean, `vite build` ok.
- Files touched: `internal/config/config.go`, `internal/api/types.go`, `internal/api/router.go`, `internal/api/skills_catalog.go` (new), `internal/api/skills_write.go` (new), `internal/api/skills_catalog_test.go` (new), `internal/api/skills_write_test.go` (new), `internal/skills/admin.go` (new), `internal/skills/admin_test.go` (new), `internal/telegram/bot.go`, `web/src/types/api.ts`, `web/src/api.ts`, `web/src/components/SkillsPanel.tsx` (rewrite), `internal/api/dist/*` (rebuilt), `.env.example`, `.env`, `docs/implementation-tracker.md`.
- Manual verification still owed by user: set `SKILLS_ADMIN=true` in `.env`, restart Aura, log in to dashboard at `http://127.0.0.1:8081/skills`. Expect:
  1. Two tabs visible: Local + Catalog.
  2. Catalog tab populates from skills.sh; typing filters within ~350 ms.
  3. Click Install on a small skill — toast shows the npx command, then success or a clipped failure log.
  4. After install, switch to Local — the new skill appears.
  5. Click Delete on a local skill — confirm prompt, then toast on success.
  6. Set `SKILLS_ADMIN=false`, restart, retry: install/delete buttons return 403 and the amber banner appears in the panel.
- Next slice: **11d — invoke MCP tools from the dashboard.** `POST /api/mcp/{server}/tools/{tool}` (admin-gated reuse) with input-schema-driven form on `/mcp`.

### 2026-04-30 — Slice 11b (Skills + MCP dashboard panels)

- One atomic commit. Phase 11 read-only surface complete; mutation/invocation lands in 11c + 11d.
- **Backend** (`internal/api`):
  - `internal/api/types.go` — new DTOs: `SkillSummary` (name + description), `SkillDetail` (adds content + truncated flag), `MCPToolInfo` (name + description + input_schema), `MCPServerSummary` (name + transport + tool_count + tools[]).
  - `internal/api/skills.go` (new) — `handleSkillsList` returns `[]SkillSummary` (or `[]` when `Deps.Skills` is nil so the frontend always sees a valid array). `handleSkillGet` validates the path with `^[A-Za-z0-9_-]{1,64}$`, calls `Loader.LoadByName`, and truncates content at `maxSkillBodyChars=16000` with `truncated:true` so the dashboard can warn.
  - `internal/api/mcp.go` (new) — `handleMCPServers` enumerates `Deps.MCP []*mcp.Client`, skips nil entries, returns servers + tools sorted by name for deterministic rendering.
  - `internal/api/router.go` — `Deps` gains `Skills *skills.Loader` and `MCP []*mcp.Client`. Three new routes registered (`GET /skills`, `GET /skills/{name}`, `GET /mcp/servers`) inside the auth-wrapped mux.
  - `internal/mcp/client.go` — added `Transport()` getter and `TransportStdio` / `TransportHTTP` constants. Constructors set `transportKind` on the client struct.
  - `internal/telegram/bot.go` — passes `Skills: skillLoader, MCP: mcpClients` into `api.NewRouter`.
- **Frontend**:
  - `web/src/types/api.ts` — TS mirrors of the four new DTOs.
  - `web/src/api.ts` — `api.skills()`, `api.skill(name)`, `api.mcpServers()` (each goes through the same bearer-authed `get<T>` helper as the rest).
  - `web/src/components/SkillsPanel.tsx` (new) — accordion of local skills. Each row click lazy-fetches `/skills/{name}` and renders the full SKILL.md as a monospaced block. Truncation banner appears when content was clipped. Empty state shows `Sparkles` icon + a one-line "Drop a folder under skills/<name>/SKILL.md" CTA.
  - `web/src/components/MCPPanel.tsx` (new) — server cards with transport icon (`Server` for stdio, `Globe` for http) + tool count. Expanding a server reveals its tools as `mcp_<server>_<tool>` rows; each tool has a "show schema" toggle that pretty-prints the upstream `input_schema` JSON. Empty state guides the user to `mcp.example.json`.
  - `web/src/App.tsx` — `/skills` and `/mcp` routes added inside the auth'd `<Shell>`.
  - `web/src/components/Sidebar.tsx` — `Sparkles` (Skills) + `Plug` (MCP) nav items appended after Tasks.
  - `web/src/components/Shell.tsx` — keyboard chord shortcuts extended: `g k` → Skills, `g m` → MCP. Help dialog rows added.
  - SPA rebuilt into `internal/api/dist/`. Bundle: 533 KB JS / 163 KB gz; 107 KB CSS / 19 KB gz (~12 KB JS / ~2 KB CSS net growth).
- **Tests**: 7 new tests in `internal/api/skills_test.go` (empty / nil-loader / list returns / detail found / 404 / bad-name / nil-loader on detail / truncation) + 3 in `internal/api/mcp_test.go` (empty / populated with full tool metadata / nil-client). Both files use a stand-alone Deps with a real `skills.Loader` rooted at `t.TempDir()` (skills) or an in-memory `httptest` MCP fake (mcp).
- **Verification**: `go build ./...` clean, `go vet ./...` clean, `go test ./...` PASS, `go test -race ./internal/{api,mcp,tools}/...` clean. Frontend: `npm run lint` clean, `npx tsc --noEmit` clean, `npm run build` ok.
- Files touched: `internal/api/types.go`, `internal/api/router.go`, `internal/api/skills.go` (new), `internal/api/mcp.go` (new), `internal/api/skills_test.go` (new), `internal/api/mcp_test.go` (new), `internal/mcp/client.go`, `internal/telegram/bot.go`, `web/src/types/api.ts`, `web/src/api.ts`, `web/src/App.tsx`, `web/src/components/Sidebar.tsx`, `web/src/components/Shell.tsx`, `web/src/components/SkillsPanel.tsx` (new), `web/src/components/MCPPanel.tsx` (new), `internal/api/dist/*` (rebuilt), `docs/implementation-tracker.md`.
- Manual verification still owed by user: open `/skills` and confirm `aura-implementation` (and any other local skills) show up with descriptions; expand one and verify the SKILL.md body renders. Open `/mcp` — empty state should appear if no `mcp.json` exists; copy `mcp.example.json` → `mcp.json`, restart, verify both example servers appear (the example commands will likely fail to connect — that's expected; populate with real servers to see live tools).
- Next slice: **11c — skill install/delete (admin-gated).** `install_skill` (shells `npx skills add ...`), `delete_skill`, `create_skill` sandboxed via `os.Root`. New admin gate (`SKILLS_ADMIN=true` or per-user flag). Frontend: install button on catalog rows + delete on local-skill rows.

### 2026-04-30 — Slice 11a (MCP client + boot wiring)

- Phase 11 begins: skills + MCP, Picobot-style. 11a is pure plumbing — backend only, no user-visible UI yet (that lands in 11b).
- New `internal/mcp` package, ported from Picobot's `internal/mcp/client.go`:
  - `Client` with `NewStdioClient(name, command, args)` and `NewHTTPClient(name, url, headers)` constructors.
  - JSON-RPC 2.0 envelope: `initialize` (clientInfo `aura/3.0`, protocolVersion `2025-03-26`) → `notifications/initialized` (fire-and-forget) → `tools/list` to populate `Client.Tools()`. `CallTool` posts `tools/call` and concatenates the `content[].text` items, surfacing `isError:true` as a Go error.
  - `stdioTransport`: `exec.Command(command, args...)` with stdin pipe write + scanner read; per-request mutex; line-delimited JSON-RPC; `Close()` kills the process. Server notifications without `id` are skipped.
  - `httpTransport`: per-request `POST` to the configured URL; honors `Mcp-Session-Id` header round-trip; accepts `application/json` or `text/event-stream` responses (parses the first `data: {…id…}` frame from SSE). HTTP 202 → empty `{}` (notification-style notify); non-200 → error with body.
  - `Tool` struct exposes `Name`, `Description`, `InputSchema map[string]any`.
- New `internal/mcp/config.go` — `LoadServers(path)` loader for `mcp.json`:
  - File schema is `{"mcpServers": {"<name>": {"command":..., "args":..., "url":..., "headers":...}}}`. Empty path or missing file returns empty map (opt-in, no warning); malformed JSON is fatal so misconfig surfaces fast.
  - `DisallowUnknownFields` so typos don't silently degrade.
  - Per-entry validation: name regex `^[A-Za-z0-9_-]{1,32}$` (so the registered tool name `mcp_<server>_<tool>` stays sane); exactly one of `command` / `url` must be set.
- New `internal/tools/mcp.go` — `MCPTool` adapter implementing the existing `tools.Tool` interface:
  - `Name()` → `mcp_<server>_<tool>` (collision-proof across servers + native tools).
  - `Description()` → `[MCP: <server>] <upstream desc>` so the LLM can tell at a glance the tool came from MCP.
  - `Parameters()` returns the upstream `inputSchema` unchanged when present; otherwise an empty `{type:object, properties:{}}` so providers requiring a schema don't reject the registration.
  - `Execute(ctx, args)` proxies to `client.CallTool`; nil-client guard for safety.
- Config: new `MCP_SERVERS_PATH=./mcp.json` env (default tracks repo root). `mcp.json` itself is gitignored; `mcp.example.json` is committed as the template (one stdio entry, one HTTP entry).
- Bot boot wiring (`internal/telegram/bot.go`):
  - After all native tools are registered, `mcp.LoadServers(cfg.MCPServersPath)` is called. On error: warn + continue (no MCP). On success: each server is connected (stdio or HTTP per config), discovered tools wrapped via `NewMCPTool` and registered in the same `tools.Registry` the LLM sees. Connection failures are warned per-server, never fatal — a flaky third-party MCP server doesn't kill the bot.
  - `Bot.mcpClients []*mcp.Client` retained on the struct. `Bot.Stop()` calls `Close()` on every client (stdio servers get their child process killed; HTTP is a no-op).
  - Logs only `server` + `tools` count on registration. No URLs, no headers, no tool args printed.
- Tests:
  - `internal/mcp/client_test.go` (10 tests): HTTP `initialize`+`tools/list` happy path; `tools/call` round-trip; `tools/call` with `isError:true`; SSE response parsing; HTTP 500 on initialize; `LoadServers` empty path / missing file / valid mixed config / both-transports-set rejection / neither-transport-set rejection / bad-name rejection / unknown-top-level-field rejection.
  - `internal/tools/mcp_test.go` (5 tests): name format; description prefix; schema pass-through; schema fallback when nil; `Execute` round-trip via in-memory MCP server; nil-client rejection.
- Verification: `go build ./...` clean; `go vet ./...` clean; `go test ./...` PASS (full suite); `go test -race ./internal/mcp/... ./internal/tools/...` clean.
- Side fix: `go mod tidy` promoted `github.com/skip2/go-qrcode` from indirect to direct (already used by slice 10i but wasn't in the direct require block, which the IDE flagged).
- Files touched: `internal/mcp/client.go` (new), `internal/mcp/config.go` (new), `internal/mcp/client_test.go` (new), `internal/tools/mcp.go` (new), `internal/tools/mcp_test.go` (new), `internal/config/config.go`, `internal/telegram/bot.go`, `.env.example`, `.env`, `.gitignore`, `mcp.example.json` (new), `go.mod`, `docs/implementation-tracker.md`.
- Next slice: **11b — skills + MCP dashboard panels.** Read-only `/api/skills` and `/api/mcp/servers` endpoints + new `/skills` and `/mcp` SPA routes (cards listing local skills and connected MCP servers with their discovered tool counts/schemas). Bearer auth like the rest.

### 2026-04-30 - Skills discovery and local skill loading

- Added read-only Aura skills support using Picobot's `skills/<name>/SKILL.md` pattern: `internal/skills` loads validated local skills from `SKILLS_PATH`, skips broken drafts, and renders a bounded prompt block on every Telegram turn.
- Added `search_skill_catalog`, a read-only skills.sh catalog search tool. `list_skills` / `read_skill` inspect only locally installed skills; installation/mutation remains deferred behind a future admin/review flow.
- Config now includes `SKILLS_PATH=./skills` and `SKILLS_CATALOG_URL=https://skills.sh/`. Added `skills/README.md` with the local skill format.
- Verification: live `skills.sh` parser check found catalog entries; `go test ./internal/skills ./internal/tools ./internal/config ./internal/telegram ./internal/conversation`, `go test ./...`, `go build ./...`, and `go vet ./...` passed.

### 2026-04-30 - Polish and harden Telegram login

- Hardened the Telegram login surface by removing the external QR image dependency. Aura now serves `GET /telegram/qr.png` locally as a generated PNG for `https://t.me/<bot>?start=login`.
- `GET /telegram` now includes `qr_url`, sets no-store/nosniff headers, and only accepts valid Telegram-style usernames. Invalid or missing bot usernames return 503 instead of emitting malformed links.
- Login UI now uses the local QR endpoint and has clearer loading/unavailable copy when the bot metadata is not ready.
- Verification: `npm run lint`, `npx tsc --noEmit`, `npm run build`, `go test ./...`, `go build ./...`, and `go vet ./...` passed.

### 2026-04-30 - Fix mobile sheet trigger crash

- Fixed a dashboard crash where Radix threw ``DialogTrigger` must be used within `Dialog``. Root cause: `Shell.tsx` rendered `SheetTrigger` outside its `<Sheet>` provider. Since the sheet is already controlled by React state, the mobile hamburger now opens it directly with `setMobileOpen(true)`.
- Rebuilt embedded dashboard assets.
- Verification: `npm run lint`, `npx tsc --noEmit`, `npm run build`, `go test ./...`, `go build ./...`, and `go vet ./...` passed.

### 2026-04-30 - Telegram QR/link on login

- Restored the missing Telegram entry point on the dashboard login screen: it now shows the running bot handle, a clickable `t.me` link, and a QR code for `https://t.me/<bot>?start=login`.
- Added public `GET /telegram` on the health server. It exposes only bot link metadata (`username`, `url`, `start_url`) and does not mint or validate dashboard tokens.
- Reserved `/telegram` in the embedded SPA fallback so the React app does not shadow the JSON endpoint.
- Verification: `go test ./...`, `go build ./...`, `go vet ./...`, `npm run lint`, `npx tsc --noEmit`, and `npm run build` all passed.

### 2026-04-30 - Bootstrap login fix

- Fixed the first-run auth trap introduced by slice 10d: Aura can now start with `TELEGRAM_ALLOWLIST` blank.
- Blank allowlist mode is one-user bootstrap mode. The first Telegram user who sends `/start` is persisted in the existing SQLite auth DB (`allowed_users`) and receives a dashboard token immediately. Later `/start`, `/login`, or `/token` requests from that same user mint fresh tokens without going through the LLM.
- If `TELEGRAM_ALLOWLIST` is configured, bootstrap mode is disabled and the env allowlist remains the source of truth.
- Login page copy now tells users to use `/start` for first setup or `/login` for a fresh token.
- Verification: `go test ./...`, `go build ./...`, `go vet ./...`, `npm run lint`, `npx tsc --noEmit`, and `npm run build` all passed.
- Files touched: `internal/config/config.go`, `internal/config/config_test.go`, `internal/auth/store.go`, `internal/auth/store_test.go`, `internal/telegram/bot.go`, `internal/telegram/bot_test.go`, `web/src/components/Login.tsx`, `.env.example`, `docs/implementation-tracker.md`, plus rebuilt `internal/api/dist/*`.

### 2026-04-30 — Slice 10e complete (polish + theme redesign)

- Single atomic commit. Phase 10 (UI) is now fully landed.
- **Backend touch (`/api/health` metadata)**:
  - `internal/api/types.go` — `HealthRollup` gains a `Process` block: `version`, `git_revision`, `started_at`, `uptime_seconds`. The frontend dashboard footer renders these.
  - `internal/api/router.go` — `Deps` gains `Version` + `StartedAt` fields.
  - `internal/api/health.go` — populates `Process`. `git_revision` is read once via `runtime/debug.ReadBuildInfo()` (vcs.revision setting), short-truncated to 7 chars, cached in a `sync.Once`. Avoids ldflags plumbing entirely; works whenever the binary was built inside a git tree.
  - `internal/telegram/bot.go` — passes `Version: "3.0"` (matching `cmd/aura/main.go`'s `auraVersion` const) and `StartedAt: time.Now().UTC()` into `api.NewRouter`. Hardcoded with a comment because `cmd/aura` isn't importable; if version churn becomes a thing, lift it into `internal/config`.
- **Frontend polish**:
  - New `web/src/components/ui/skeleton.tsx` (standard shadcn `<Skeleton>`).
  - All four data panels swap their text-only "Loading…" for layout-faithful skeletons: `DashboardSkeleton` (3-card grid), `WikiSkeleton` (5 row stubs), `SourceInboxSkeleton` (drop-zone + 2 status sections), `TasksSkeleton` (header + 3 task rows). Reduces layout shift on first paint.
  - Empty states get visual weight: WikiPanel shows a `BookText` icon + "Drop a PDF on /sources or chat with the bot" CTA when `data.length === 0`; TasksPanel shows a `Calendar` icon + "+ New task" hint inside a dashed-border block when no tasks exist.
  - `ErrorBoundary` fires `sonner.error(message, { description: 'Check the console…', duration: 6000 })` from `componentDidCatch` so failures pop above the fold even on long pages.
  - `useAppTheme.readInitialTheme` flipped to dark-by-default — only honors an explicit `prefers-color-scheme: light` system setting; otherwise dark.
  - New `web/src/components/Shell.tsx` consolidates the auth'd shell layout: desktop sidebar always-visible at `md+`, mobile collapses into a `Sheet`-backed slide-over triggered by a hamburger in a top bar that only renders below `md`. App.tsx swapped the inline flex layout for `<Shell>`. The `Sidebar` component now takes an optional `onNavigate` callback so mobile nav clicks close the drawer.
  - Global keyboard shortcuts: `useKeyboardShortcuts` installs a single `keydown` listener with a tiny chord state machine. `?` opens a help dialog (rolled by hand instead of pulling Radix Dialog into the shell) listing all shortcuts. `g` followed by `h/w/g/s/t` navigates to home/wiki/graph/sources/tasks within a 1.2s window. The handler skips when the focused element is an input/textarea/select/contenteditable so chords never hijack form typing.
- **Theme redesign from the logo**:
  - Studied `Logo/loho new.png` — deep-navy disc with an electric cyan-blue arrow-A glyph and a subtle teal halo. Translated to oklch tokens.
  - `web/src/index.css` — rewrote three palette blocks. Light mode: white-paper canvas + `oklch(0.62 0.16 245)` electric blue as `--primary` (single saturated accent; everything else stays neutral). Dark mode (`[data-theme="dark"]` AND `.dark` — both apply because useAppTheme sets both selectors): deep navy background `oklch(0.16 0.03 250)`, lifted card `oklch(0.21 0.035 250)`, slightly darker sidebar `oklch(0.18 0.035 250)`, brighter cyan `oklch(0.7 0.18 240)` for primary, even brighter `oklch(0.75 0.2 235)` for the focus ring. Matched the `--bg`/`--surface` Sacchi-legacy variables (still used by chat/wiki panels) so the chat surface looks consistent. Both `[data-theme="dark"]` and `.dark` blocks updated and noted to keep in sync.
  - Ambient aurora — two soft radial spotlights (top-right cyan + bottom-left indigo at 6-8% alpha) baked into `body` background under `.dark` and `[data-theme="contrast"]`. Adds subtle depth without affecting readability.
  - Inline-SVG brand mark: `BrandMark` in Sidebar (36×36, soft halo) + `LoginBrandMark` on the unauth login page (64×64 with a stronger halo + an extra radial gradient behind it). Both render the arrow-A glyph from the logo using `var(--primary)` so they retint with the theme.
  - Sidebar header upgraded to brand mark + tracked-letter "SECOND BRAIN" eyebrow under the wordmark.
  - Active nav items: `bg-primary/10 text-primary font-medium ring-1 ring-primary/20 shadow-[0_0_20px_-8px_var(--primary)]` — gives the active row a soft cyan glow that's clearly the brand color without being neon-loud.
  - HealthDashboard `Card` gets a hover stripe (top-edge gradient that fades in) and `hover:border-primary/30`. The `StatusBar` swaps zinc/blue/emerald/rose for slate/sky/primary/rose so the "ingested" bucket renders in the brand color (visual reinforcement that ingestion is the success path).
  - Dashboard heading gets a subtitle ("Live health rollup · refreshes every 5s") so the page header scans more like a 2026 dashboard than a placeholder.
  - All `.sacchi-*` legacy CSS untouched — those classes power the chat/product views which weren't part of the dashboard surface and don't need 10e treatment.
- Verification: `go build ./...` clean, `go vet ./...` clean, `go test ./...` PASS, `npm run lint` clean, `npx tsc --noEmit` clean, `vite build` ok (521 KB JS / 161 KB gz; 105 KB CSS / 18 KB gz; CSS grew ~7 KB from new tokens).
- Files touched: `internal/api/types.go`, `internal/api/router.go`, `internal/api/health.go`, `internal/telegram/bot.go`, `web/src/index.css`, `web/src/hooks/useAppTheme.ts`, `web/src/types/api.ts`, `web/src/components/ui/skeleton.tsx` (new), `web/src/components/Shell.tsx` (new), `web/src/components/Sidebar.tsx`, `web/src/components/Login.tsx`, `web/src/components/HealthDashboard.tsx`, `web/src/components/WikiPanel.tsx`, `web/src/components/SourceInbox.tsx`, `web/src/components/TasksPanel.tsx`, `web/src/components/ErrorBoundary.tsx`, `web/src/App.tsx`, `internal/api/dist/*` (rebuilt), `docs/implementation-tracker.md`.
- Manual verification still owed by user: dark theme renders by default; mobile drawer slides on a narrow window; `?` opens the shortcut help; `g w` navigates to /wiki; sidebar BrandMark glows; login page shows the larger glowing orb.
- Phase 10 complete. **Next phase TBD** — possible follow-ups: `last_error` per-subsystem plumbing (deferred from 10e per the design doc), Prometheus `/metrics`, Lighthouse CI, Playwright auth-flow smoke.

### 2026-04-30 — Slice 10d complete (bearer auth + Telegram-issued tokens)

- Two atomic commits. **A** (`a4d3fdf`): backend (auth package, middleware, /auth/{whoami,logout} endpoints, request_dashboard_token tool, dropping requireLoopback). **B** (this commit): frontend wiring (Login.tsx, Authorization header, route guard, Sign-out button) + tracker + SPA rebuild.
- **Threat model addressed**: every `/api/*` request requires a valid bearer. Tokens are minted only through Telegram — there is no public `/api/auth/login`. The Telegram allowlist remains canonical: `RequireBearer` re-checks the user against `cfg.IsAllowlisted` on every request, so removing a user from `TELEGRAM_ALLOWLIST` immediately revokes dashboard access without separate plumbing.
- Backend (commit A):
  - `internal/auth/store.go` — `api_tokens` table on the existing scheduler SQLite file (single backup artifact). Tokens are 32 random bytes encoded as base64url (~43 chars); only the SHA-256 hash is persisted. `Lookup` uses `crypto/subtle.ConstantTimeCompare` defensively even though SQLite already keys on the hash. `last_used` updated inline (MVP — design notes a 30s batch if it shows up as a hot row). Sentinel `ErrInvalid` covers unknown / malformed / revoked uniformly so middleware can't accidentally enumerate token state.
  - `internal/auth/middleware.go` — `RequireBearer(store, allowlist, logger, next)` extracts `Bearer <token>`, calls `store.Lookup`, rechecks the allowlist, stashes user ID via a private context key. 401 JSON body on every failure path. Token text never logged (a leak there would defeat hashing).
  - `internal/api/auth.go` — `GET /auth/whoami` (echoes the user ID resolved from the bearer; cheap), `POST /auth/logout` (revokes the request's bearer; idempotent — second logout still returns 200).
  - `internal/api/router.go` — `Deps` gains `Auth *auth.Store` + `Allowlist auth.AllowlistFunc`. When `Auth` is non-nil the entire mux is wrapped in `RequireBearer` — every route, including `/auth/whoami` and `/auth/logout`. Tests that don't need auth pass `Auth: nil` and the router stays unwrapped.
  - `internal/tools/auth.go` — `RequestDashboardTokenTool` issues a fresh token, allowlist-checks defensively, ships the plaintext via `TokenSender` (an interface the bot satisfies). Critical: the LLM tool result confirms delivery but never contains the token. On Telegram send failure, the freshly-issued token is revoked so the partial state can't leave a usable bearer floating in the DB. Constructor returns nil if any dep is nil so the bot can skip registration cleanly when auth isn't configured.
  - `internal/telegram/bot.go` — opens `auth.OpenStore` on the same SQLite file as scheduler. New `Bot.SendToUser(userID, message)` method satisfies `tools.TokenSender` (parses the user ID as a chat ID and calls `bot.Send(tele.ChatID(...))`). `request_dashboard_token` registered after `b` is constructed so the bot can be its own sender. `api.NewRouter` call now passes `Auth` + `Allowlist`.
  - Tests: 12 store/middleware tests (round-trip, empty user, unknown / empty / revoked tokens, double-revoke, token uniqueness over 50 issuances, multi-user isolation, header parsing edge cases, case-insensitive scheme, revoked + de-allowlisted rejection paths). 7 router-level integration tests (401 unauthed, 200 authed, revoked → 401, de-allowlisted → 401, write endpoints gated, /auth/whoami, /auth/logout revoke flow). 5 tool tests (happy path with leak-check on the result string, no-context, non-allowlisted, send failure → revoke, nil-arg constructor). Race-clean.
  - `requireLoopback` retired — auth supersedes it. `TestWrite_RejectsNonLoopback` removed from `writes_test.go`. The `doLocal` helper there is now a vestige (its `RemoteAddr=127.0.0.1` line is no-op without the gate) but kept to minimize churn in this slice; harmless.
- Frontend (this commit):
  - `web/src/lib/auth.ts` — `getToken`/`setToken`/`clearToken` localStorage helpers under key `aura_token`. Catches localStorage exceptions (private browsing) so they degrade to silent failure rather than crash.
  - `web/src/api.ts` — `authHeaders()` attaches `Authorization: Bearer <token>` on every fetch. `handle401()` clears the stored token and bounces to `/login?expired=1` (with a redirect-loop guard for when we're already on /login). `readError()` extracted as a shared helper since the 401 path now needs the same JSON-error parsing as the success path. Two new methods: `whoami()` and `logout()`. `WhoamiResponse` added to `types/api.ts`.
  - `web/src/components/Login.tsx` — single-input paste-token form. On mount, if a token already exists, it fires a silent `whoami()` and either navigates home (still valid) or clears the token (rejected). `?expired=1` query param shows an explicit "session expired or was revoked" hint above the form so returning users know why they're back at the login screen. Token uses `<input type="password">` to keep it off the screen during paste; `autoComplete="off"` so browsers don't autofill.
  - `web/src/App.tsx` — top-level route refactor. `/login` is unauth'd; everything else goes through a `RequireAuth` wrapper that reads `getToken()` synchronously and bounces to `/login` if missing. Avoids the initial flash of "Loading…" / "Error: unauthorized" that the api.ts redirect alone would produce. The real validity check still happens on the first API call.
  - `web/src/components/Sidebar.tsx` — Sign-out button in the footer next to the theme toggle. Calls `api.logout()` (best-effort — server-side revoke is hardening, not a correctness gate), then `clearToken()` + navigate to `/login`. Sonner toast confirms.
  - SPA rebuilt into `internal/api/dist/`. Bundle sizes essentially unchanged from 10c.
- Verification: `go build ./...` clean, `go vet ./...` clean, `go test ./...` PASS, `go test -race ./internal/{api,auth,tools}/...` clean, `npm run lint` clean, `npx tsc --noEmit` clean.
- Bootstrap recipe (manual verification still owed by user):
  1. Start the bot: `go run ./cmd/aura`
  2. In Telegram, send "give me a dashboard token" (or similar).
  3. The bot replies with a token. Copy it.
  4. Open `http://127.0.0.1:8081/` → redirected to `/login`.
  5. Paste token, click Sign in. Dashboard loads.
  6. Click Sign out. Token revoked server-side; back at `/login`. Re-pasting the old token shows the rejection message.
- Files touched (this commit): `web/src/api.ts`, `web/src/types/api.ts`, `web/src/lib/auth.ts` (new), `web/src/components/Login.tsx` (new), `web/src/components/Sidebar.tsx`, `web/src/App.tsx`, `internal/api/dist/*` (rebuilt), `docs/implementation-tracker.md`.
- Next slice: **10e — polish** (mobile drawer, dark-mode default, empty states, loading skeletons, keyboard shortcuts, observability surfaced on `/api/health`).

### 2026-04-30 — Slice 10c complete (write actions)

- Two atomic commits. **A**: `slice 10c: write endpoints (sources/wiki/tasks)`. **B**: this commit — frontend wiring + tracker update + SPA rebuild.
- Backend (commit A, `5611e7d`):
  - `internal/api/router.go` — `Deps` gains `Location *time.Location` for daily HH:MM resolution; `SchedulerStore` interface gains `Upsert` + `Cancel`. Six new routes registered behind `requireLoopback` (POST `/sources/{id}/ingest`, `/sources/{id}/reocr`, `/wiki/index/rebuild`, `/wiki/log`, `/tasks`, `/tasks/{name}/cancel`).
  - `internal/api/sources_write.go` (new) — `handleSourceIngest` re-runs `Pipeline.AfterOCR` (idempotent because `Compile` rewrites the same slug); status precondition is `ocr_complete` or `ingested`, returns 409 otherwise. `handleSourceReocr` reads `original.pdf` via `Path`, reruns `OCR.Process`, rewrites `ocr.md`/`ocr.json`, flips status, then chains `AfterOCR` when `Ingest` is wired. Both return 503 when the relevant client is nil so the dashboard can show a clear "set MISTRAL_API_KEY" message instead of a generic 500. `decodeJSONBody` helper caps body at 64 KiB and disallows unknown fields.
  - `internal/api/wiki_write.go` (new) — `handleWikiRebuild` calls `wiki.Store.RebuildIndex`. `handleWikiAppendLog` validates action against a `[A-Za-z0-9_.-]{1,32}` regex and asserts `wiki.Slug(slug) == slug` so log.md can't be smuggled into. Both go through a private `wikiWriter` interface so the public `WikiStore` type stays read-only at the contract level.
  - `internal/api/tasks_write.go` (new) — `handleTaskUpsert` mirrors the `schedule_task` LLM tool semantics: name regex, kind in {reminder, wiki_maintenance}, exactly one of `at` (RFC3339 UTC) or `daily` (HH:MM in local TZ), reminder requires `recipient_id` (no user-context shortcut from HTTP). `handleTaskCancel` flips active → cancelled and disambiguates 404 vs 409 via a follow-up `GetByName` so the UI shows "already cancelled" vs "no such task" cleanly.
  - `internal/api/writes_test.go` (new, 21 test funcs) — uses `doLocal` (RemoteAddr=127.0.0.1) and `doRemote` (default 192.0.2.1) helpers to cover both happy paths and the loopback gate. Negative cases: bad IDs, disabled OCR/Ingest (503), missing/malformed JSON, every input-validation branch on tasks (missing fields, both at+daily, reminder without recipient, past at, bad daily format).
  - `internal/telegram/bot.go` — passes `time.Local` into `Deps.Location` so daily schedules in the API resolve in the same TZ as the LLM tool.
  - Verification at commit A time: `go test ./internal/api/...` 35 tests PASS (14 existing + 21 new); `go test ./...` clean; `go build ./...` clean; `go vet ./...` clean; `go test -race ./internal/api/...` clean.
- Frontend (this commit):
  - `web/src/types/api.ts` — adds `IngestResponse`, `ReocrResponse`, `UpsertTaskRequest` interfaces mirroring the Go DTOs.
  - `web/src/api.ts` — new `post<T>(path, body?)` helper (no 8s GET timeout because OCR can run for minutes); six new methods: `ingestSource`, `reocrSource`, `rebuildWikiIndex`, `appendWikiLog`, `upsertTask`, `cancelTask`.
  - `web/src/components/SourceInbox.tsx` — new "Actions" column. `SourceActions` subcomponent renders Re-OCR for `stored`/`failed`, Ingest for `ocr_complete`/`failed`, nothing for `ingested`. Per-row in-flight tracking via `busyIds: Set<string>` so the same button can't be double-clicked. Sonner toasts (`loading` → `success`/`error`) and `refetch()` on success so the table updates immediately rather than waiting for the 5 s poll.
  - `web/src/components/TasksPanel.tsx` — header gains a "+ New task" button; per-row Cancel button on `active` rows. `NewTaskDialog` wraps a `NewTaskForm` keyed on `open` so each open mounts fresh state (sidesteps the `react-hooks/set-state-in-effect` lint rule that blocked the naive useEffect-reset approach). Form supports both `wiki_maintenance` and `reminder` kinds, with `recipient_id` rendered conditionally. `<input type="datetime-local">` for `at` mode is converted to UTC RFC3339 via `new Date(at).toISOString()`.
  - `web/src/components/WikiPanel.tsx` — header gains a "Rebuild index" button. Toast → refetch on success.
  - SPA rebuilt into `internal/api/dist/`. New CSS bundle 98 kB → 17 kB gzipped; main JS bundle 504 kB → 156 kB gzipped (the 500 kB warning is the existing graph view + Tiptap reference; not 10c-specific).
- Verification (this commit): `go build ./...` clean; `go vet ./...` clean; `go test ./...` full suite PASS; `npm run lint` in `web/` clean; `npx tsc --noEmit` clean.
- All write endpoints stay loopback-only (`requireLoopback` middleware). LAN exposure remains gated until **slice 10d** ships bearer auth.
- Files touched (this commit): `internal/api/dist/*` (rebuilt), `web/src/api.ts`, `web/src/types/api.ts`, `web/src/components/SourceInbox.tsx`, `web/src/components/TasksPanel.tsx`, `web/src/components/WikiPanel.tsx`, `docs/implementation-tracker.md`.
- Manual verification still owed by user: open the dashboard at `http://127.0.0.1:8081/`, confirm (a) re-OCR + ingest buttons appear on the right rows, (b) "+ New task" dialog round-trips a daily-recurring task, (c) Cancel flips an active task to cancelled, (d) "Rebuild index" on `/wiki` succeeds.
- Next slice: **10d — bearer-token auth** so the listener can be exposed beyond loopback. Then **10e** for polish (empty-state copy, error retry UX, accessibility pass).

### 2026-04-30 — Browser PDF upload (10c mini-slice)

- One-shot mini-slice carved out of 10c so the user could drop PDFs onto the dashboard immediately. The remaining 10c endpoints (ingest, reocr, cancel, rebuild) stay deferred.
- Backend (`380d7f2`):
  - `internal/api/upload.go` — `POST /sources/upload` handler. Multipart parse (`OCR_MAX_FILE_MB` cap, default 100), filename + ext check, `source.Store.Put` → `ocr.Client.Process` → atomic `ocr.md` + `ocr.json` write → status flip to `ocr_complete` → `ingest.Pipeline.AfterOCR` for auto-ingest. Mirrors `internal/telegram/documents.go` step-for-step minus the Telegram progress UX. `UploadResponse` DTO carries `id`, `status`, `duplicate`, `filename`, `page_count`, `wiki_pages`, `ingest_note`, `ocr_error`, and a human-friendly `note` summary.
  - `requireLoopback` middleware in the same file: `net.SplitHostPort(r.RemoteAddr)` + `IsLoopback()`, returns 403 otherwise. Does NOT honor `X-Forwarded-For` since there's no reverse proxy. This is the gate that protects the write surface until 10d ships bearer auth.
  - `internal/api/router.go` — `SourceStore` interface gains `Put` + `Update` (writes were previously read-only). `Deps` gains `OCR`, `Ingest`, `MaxUploadMB`. Route registered through `requireLoopback`.
  - `internal/telegram/bot.go` — passes `ocrClient`, `ingestPipeline`, and `cfg.OCRMaxFileMB` to `api.NewRouter`.
- Frontend (`380d7f2`):
  - `web/src/types/api.ts` — `UploadResponse` interface mirrors the Go DTO.
  - `web/src/api.ts` — `api.uploadSource(File)` wraps a multipart POST. Bypasses the 8 s GET timeout intentionally — OCR can take minutes for large PDFs.
  - `web/src/components/SourceInbox.tsx` — drop zone + hidden `<input type="file" multiple accept=".pdf">`. Drag-and-drop on the outer container with the standard `dragOver`/`dragLeave`/`drop` handlers. Sequential per-file uploads with `sonner` `toast.loading` → `toast.success`/`toast.error`. After each upload, `refetch()` from `useApi` triggers an immediate poll so the table reflects the new `ingested` row without waiting for the 5 s tick.
- `.env` updated to `HTTP_PORT=127.0.0.1:8081` (was `:8081`, LAN-wide). `.env.example` already had `127.0.0.1:8080` from slice 10b.
- Live verification on `6MBU00242200.pdf`:
  - `src_67467125f865d781` directory created with `original.pdf` (229 952 bytes), `ocr.md`, `ocr.json`, `source.json` (status=`ingested`, OCR model `mistral-ocr-latest`, 1 page).
  - Wiki page `wiki/source-6mbu00242200.md` (1 911 bytes) generated with proper frontmatter (`category: sources`, `sources: [source:src_67467125f865d781]`, schema v2, prompt `ingest_v1`).
  - `wiki/index.md` and `wiki/log.md` rebuilt by the wiki maintenance hook.
  - Total elapsed ~1.4 s (PDF stored 10:23:13.65 UTC → wiki page written 10:23:15 UTC).
- Verification commands run: `go build ./...` clean; `go vet ./...` clean; `go test ./...` full suite PASS; `npm run lint` + `npx tsc --noEmit` in `web/` clean.
- Files touched: `internal/api/router.go`, `internal/api/upload.go` (new), `internal/telegram/bot.go`, `web/src/api.ts`, `web/src/types/api.ts`, `web/src/components/SourceInbox.tsx`, `internal/api/dist/*` (rebuilt), `.env` (port binding).
- Next: rest of slice **10c** — `POST /api/sources/{id}/ingest`, `POST /api/sources/{id}/reocr`, `POST /api/tasks/{name}/cancel`, `POST /api/tasks` (upsert), `POST /api/wiki/index/rebuild`, `POST /api/wiki/log`. All gated by the same `requireLoopback` middleware until 10d. UI: ingest button on stored/failed source rows, cancel button on active tasks, "+ New task" dialog on `/tasks`, "Rebuild index" overflow on `/wiki`.

### 2026-04-30 — Slice 10b complete (frontend scaffold + wiki/graph views)

- Slice 10b shipped via 6 intermediate commits (`53ad7ab` → `9f0c01f` → `49c0b6b` → `70b2ce6` plus Phase 4 + final). Approach 1 from the design doc: copy from `D:\sacchi_Agent\frontend\src-app` and prune sacchi-specific files, rewire to Aura's `/api/*` endpoints from slice 10a.
- New `web/` directory: React 19 + Vite + TypeScript + Tailwind v4 + shadcn/ui. Pruned deps (~6 npm packages dropped — copilot, ag-ui, cmdk, vaul). Added `react-router-dom@7`. `vite.config.ts` writes build output directly to `internal/api/dist/` so `//go:embed` reads it without a copy step.
- 5 client-side routes: `/` HealthDashboard, `/wiki` WikiPanel, `/wiki/:slug` WikiPageView, `/graph` WikiGraphView (lazy-loaded, force-graph-2d), `/sources` SourceInbox, `/tasks` TasksPanel. SPA fallback in `internal/api/static.go` handles deep-link refresh.
- New components written from scratch against Aura's API: `HealthDashboard`, `SourceInbox`, `TasksPanel`, `WikiPageView` (read-only via react-markdown), `ErrorBoundary`. Sacchi components rewritten: `App`, `Sidebar`, `WikiPanel`, `WikiGraphView`, `EventStrip` (stub), `WikiEditor` (stub).
- `useApi` hook: shared fetch + 5s polling with `document.visibilityState` pause, stale-with-pill on subsequent failures, 8s `AbortController` timeout. No SWR / TanStack Query.
- Hand-written DTOs in `web/src/types/api.ts` mirroring `internal/api/types.go`. ~80 LOC.
- Theme handling: kept sacchi's three-theme `useAppTheme` (`light` | `dark` | `contrast`) intact; Sidebar uses `cycleTheme` and per-theme icons. Adapted via approach A from the design's gray-area question.
- Backend changes: `internal/config/config.go` HTTPPort default `:8080` → `127.0.0.1:8080`; `.env.example` updated with comment about LAN exposure deferring to slice 10d. `internal/health/server.go` deletes `handleLanding` + `landingPage` HTML constant; `go-qrcode` dep removed via `go mod tidy`. `internal/api/static.go` provides multi-frame `//go:embed all:dist` + SPA fallback handler with `ErrNoStaticAssets` for the pre-build state. `cmd/aura/main.go` mounts the static handler after the API on the same health server mux.
- Tray gains "Open Dashboard" menu item that shells out to `rundll32 url.dll,FileProtocolHandler` with the URL derived via new `dashboardHost` helper (`:8080` → `localhost:8080`, `0.0.0.0:port` → `localhost:port`, anything else passthrough).
- `Makefile` gains `web` (vite dev), `web-build` (npm install + npm run build), `ui-dev` (parallel bot + vite).
- Verification: `go vet` + `go test` clean across `internal/api`, `internal/health`, `internal/config`, `internal/tray`. `go test -race ./internal/api/...` clean. `tsc --noEmit` clean. `npm run lint` clean (after fixing one `react-hooks/purity` violation in the Countdown component — pinned `now` to state instead of calling `Date.now()` during render). Sacchi files retain `/** @ts-nocheck */` headers we kept; not blocking.
- Deferred to user: full-tree `go build ./...` was scoped to in-slice packages because `cmd/build_icon/main.go` had a parallel in-flight edit. The user landed `6584a16` mid-execution which fixed it; final tree should now build clean.
- Files touched (commit-by-commit summary):
  - `53ad7ab` 10b prep: localhost binding + static handler scaffold (config/.env.example/health/api/static.go + tests)
  - `9f0c01f` 10b WIP: copy sacchi → web/ and prune (whole `web/` tree, sacchi-specific files deleted, package.json + vite.config.ts + index.html rewritten)
  - `49c0b6b` 10b WIP: types + api client + useApi hook
  - (Phase 4 commit, name varies by squash) new components
  - `70b2ce6` 10b WIP: adapt copied components to /api/* and react-router
  - Final commit (this commit): build SPA, wire static handler in main, tray Open Dashboard, Makefile, tracker update.
- Manual verification still owed by user: `go run ./cmd/aura`, then http://localhost:8080/ should render the dashboard; the 13-item checklist in `docs/plans/2026-04-30-slice-10b-plan.md` Task 37 is the canonical list. The tray's Open Dashboard launches the browser.
- Next slice: **10c — UI write actions** (POST endpoints + ingest/cancel/rebuild buttons). Or 10d (auth) if LAN exposure is needed sooner.

### 2026-04-30 — Slice 10a complete (read-only HTTP API)

- Slice 10a (read-only HTTP API) done. Lays the JSON contract the dashboard frontend (slice 10b) will consume. Every read-side data the UI needs is reachable via `curl http://localhost:8080/api/...`; no write endpoints in this slice (those land in 10c).
- New package `internal/api` (7 files):
  - `types.go` — DTOs intentionally separate from internal models (`wiki.Page`, `source.Source`, `scheduler.Task`) so a future internal field rename doesn't break the frontend wire format. Times normalized to RFC3339 UTC at the boundary; `omitempty` on optional fields. `Task.ScheduleAt` and `Task.LastRunAt` are `*time.Time` so unset values omit cleanly instead of rendering as `0001-01-01`.
  - `router.go` — `NewRouter(Deps) http.Handler` builds a Go 1.22 `ServeMux` with method-prefixed patterns (`GET /health`, `GET /sources/{id}`, etc). Routes are mount-agnostic — they don't include `/api`; callers wrap with `http.StripPrefix("/api", ...)`. `Deps` accepts interfaces (`WikiStore`, `SourceStore`, `SchedulerStore`) rather than concrete types so tests could swap fakes if pure-real-store fixtures ever get expensive. Two regex validators (`sourceIDRe`, `taskNameRe`) gate untrusted path segments before they reach filesystem joins.
  - `wiki.go` — `GET /wiki/pages` lists `[{slug, title, category, tags, updated_at}]` sorted by category then slug; `GET /wiki/page?slug=X` returns the full page with a `Frontmatter` map (rendered from the structured `wiki.Page` fields, not raw YAML) and a 1 MiB body cap (413 if exceeded); `GET /wiki/graph` builds nodes from every wiki page and edges from `wiki.ExtractWikiLinks(body)` + frontmatter `Related`, deduping per source-page (so a page that links to the same target via both wikilink and related yields one edge — wikilink wins) and dropping self-loops + dangling edges to non-existent slugs. `latestWikiMTime` walks the wiki dir for the newest `.md` mtime — exposed via a new `wiki.Store.Dir()` accessor — so `/health` doesn't have to read+parse every page on every poll.
  - `sources.go` — `GET /sources` (with `?kind=` and `?status=` filters validated at the boundary, 400 on bogus values) returns lightweight `SourceSummary` rows; `GET /sources/{id}` returns the full `SourceDetail` including SHA256 / size / mime / OCR model / last-error. `GET /sources/{id}/ocr` reads `ocr.md` via `source.Store.Path` (containment-checked) and returns 404 if missing. `GET /sources/{id}/raw` is PDF-only — non-PDF kinds return 404 — streams `original.pdf` via `http.ServeContent` so the browser gets proper conditional-GET / range support and an `inline; filename="..."` disposition for save-as.
  - `tasks.go` — `GET /tasks` (optional `?status=` filter) and `GET /tasks/{name}`. `taskDTO` shapes the response and pointerizes the optional times.
  - `health.go` — `GET /health` rollup: wiki page count + last update mtime, sources by_status counts, tasks by_status counts, soonest active-task `next_run_at` (or null). Single fetch, single round-trip — the dashboard home page can render from this alone.
  - `router_test.go` — 14 test funcs / 21+ subtests using `httptest`. Each test gets its own `t.TempDir` with a real `wiki.Store`, real `source.Store`, and real SQLite-backed `scheduler.Store`; no fakes. Coverage: empty rollup, populated rollup with done-task exclusion from next_run, sort-order on `/wiki/pages`, body markdown round-trip, the 5 bad-input cases on `/wiki/page` (missing/empty/invalid-chars/path-traversal/unknown-slug), graph edge dedup + self-loop filter + dangling-target filter, source list filter validation + DTO trim, source 404 vs 400 vs OK, ocr.md present-vs-missing, raw PDF stream + Content-Type + non-PDF rejection, task list filter + status-filter rejection, task get happy/missing/malformed-name, unknown-path 404, method-not-allowed.
- `internal/wiki/store.go` — added `Dir() string` accessor (3 lines). The API uses it for the mtime walk in `/health`; the LLM-facing wiki tools don't need it.
- `internal/health/server.go` — added a `mux *http.ServeMux` field to the `Server` struct (the mux already existed but was scoped to `NewServer`) plus a `Mount(prefix, handler)` method so the API can be attached without touching the Server's existing `/`, `/status`, `/health` handlers. No behavior change for the existing endpoints.
- `internal/telegram/bot.go` — `Bot` gained an `api http.Handler` field, built once in `New` from `wikiStore`, `sourceStore`, `schedStore`, and exposed via `APIHandler() http.Handler` so `cmd/aura/main.go` can hand it to the health server. No new dependencies on the bot's hot path — the API doesn't touch `tools.Registry`, `llm.Client`, or anything else that mutates state.
- `cmd/aura/main.go` — moved `healthServer.Start()` to *after* `Bot.New` + `Mount` so the API routes are wired before the listener accepts requests (previously a request hitting `/api/...` during the millisecond between Start and bot construction would have 404'd; now there's no race). Adds `net/http` import for `http.StripPrefix`.
- Verification: `go test ./internal/api/...` PASS (14 tests / 21 subtests, no skips); `go test ./...` full suite PASS; `go build ./...` clean; `go vet ./...` clean; `go test -race ./internal/api/...` clean.
- Files touched: `internal/api/types.go` (new), `internal/api/router.go` (new), `internal/api/wiki.go` (new), `internal/api/sources.go` (new), `internal/api/tasks.go` (new), `internal/api/health.go` (new), `internal/api/router_test.go` (new), `internal/wiki/store.go` (`Dir()`), `internal/health/server.go` (`mux` field + `Mount`), `internal/telegram/bot.go` (api field + APIHandler), `cmd/aura/main.go` (mount + reordered Start), `docs/implementation-tracker.md`.
- Manual verification recipe (still owed by user, no LLM access to a browser): run `go run ./cmd/aura`, then `curl http://localhost:8080/api/health` should return the rollup; `curl http://localhost:8080/api/wiki/pages` should list seeded pages; `curl http://localhost:8080/api/sources` should list the three live-tested PDFs; `curl http://localhost:8080/api/tasks?status=active` should show the bootstrapped `nightly-wiki-maintenance` row.
- Next slice: **10b — Frontend scaffold + wiki/graph views** (copy `D:\sacchi_Agent\frontend\src-app` → `web/`, strip sacchi-specific pieces per the slice plan, wire `/api/*` calls in `src/api.ts`, build into `web/dist`, embed via `//go:embed`). Or push 10c (write actions) first if the read-only API needs more endpoints once the UI is built.

### 2026-04-30 — Side work: Windows system tray icon

- Out-of-band addition (not in the original PDR §12 slice order): a system tray icon when the bot starts, so the user sees Aura is running and can stop it from the OS shell.
- New package `internal/tray` (3 files):
  - `tray.go` — public API: `Options{Title, Tooltip, Version}`, `Run(opts) error` (blocks; MUST be called from main goroutine because `fyne.io/systray` requires the main thread on Windows), `Stop()` (safe from any goroutine).
  - `tray_windows.go` — real impl. `//go:embed icon.ico` for the asset, `systray.Run(onReady, onExit)` blocks until Quit. `onReady` sets icon/title/tooltip, adds a disabled `"Aura <version>"` header, separator, then `"Quit Aura"` menu item. A goroutine waits on `mQuit.ClickedCh` and calls `systray.Quit()` to unblock Run. `Stop()` also calls `systray.Quit()`.
  - `tray_other.go` — non-Windows stub. `Run` blocks on a package-level channel; `Stop` closes it via `sync.Once`. Mirrors the Windows lifecycle so `cmd/aura/main.go` is platform-agnostic.
- Icon: `internal/tray/icon.ico` generated once from `Logo/logo.png` via PowerShell + .NET (`System.Drawing.Image` → 256x256 aspect-preserved bitmap → `Bitmap.GetHicon()` → `Icon.FromHandle().Save()`). 41 KB single-frame ICO. Regenerate by re-running the conversion if the logo changes.
- `cmd/aura/main.go` restructured:
  - Added `auraVersion = "3.0"` const (replaces three string literals).
  - Removed `defer healthServer.Shutdown` (the deferred Shutdown ran during normal exit but the bot.Stop() was never deferred — explicit shutdown sequence is clearer now and properly orders bot stop before health server shutdown).
  - Bot creation failure now shuts the health server down before `os.Exit(1)`.
  - `go bot.Start()` runs as before.
  - Signal goroutine: `<-sigCh` → `tray.Stop()`. Bridges SIGINT/SIGTERM to the tray's quit path so the same shutdown sequence runs whether the user closes from the tray menu or sends a signal.
  - `tray.Run(...)` runs on the main goroutine and blocks. After it returns, the explicit shutdown sequence runs: log → `bot.Stop()` → `healthServer.Shutdown()`.
- Dependency: `fyne.io/systray v1.12.0` (and transitive `github.com/godbus/dbus/v5 v5.1.0` upgrade) added via `go get fyne.io/systray@latest && go mod tidy`.
- Verification: `go build ./...` clean, `go vet ./...` clean, `go test ./...` full suite PASS (existing tests untouched; tray package is a thin wrapper with no tests — manual verification on first run only).
- Files touched: `internal/tray/tray.go` (new), `internal/tray/tray_windows.go` (new), `internal/tray/tray_other.go` (new), `internal/tray/icon.ico` (new, generated), `Logo/logo.png` (canonical source asset, previously untracked), `cmd/aura/main.go` (restructured), `go.mod` + `go.sum` (deps), `docs/implementation-tracker.md`.
- Manual verification still pending: run `go run ./cmd/aura` and confirm the tray icon appears, hover-tooltip reads `Aura — running on :PORT`, and "Quit Aura" cleanly stops the bot. The tray and SIGINT paths both feed into `tray.Stop()` so they should behave identically.

### 2026-04-30 — Slice 9 complete (cmd/debug_ingest)

- `cmd/debug_ingest/main.go` — natural-prompt smoke harness mirroring `cmd/debug_tools` but for the source / ingest / wiki-maintenance / scheduler tools shipped in slices 5–8. Hermetic: temp wiki dir + temp SQLite scheduler DB. Reads LLM_API_KEY + EMBEDDING_API_KEY from `.env`.
- Pre-seeds two sources before the LLM run: a stored text source (`smoke-note.txt`, status=stored) and an ocr_complete PDF source with a hand-written `ocr.md` (so `ingest_source` has something real to compile without needing a live Mistral OCR call).
- 10 scenarios — one tool per scenario, each asserting the LLM picked the right tool and the final text contains expected markers:
  - `list_sources` (sees both seeded IDs)
  - `read_source` (filename round-trip)
  - `lint_sources` (correctly buckets the ocr_complete source as awaiting-ingest)
  - `ingest_source` (compiles the fixture into `source-aura-debug-ingest-fixture`)
  - `list_wiki` post-ingest (finds the new page)
  - `lint_wiki` (clean wiki passes)
  - `append_log` (writes a `smoke-test` entry to `log.md`)
  - `schedule_task` with `in: 90s` (relative duration, exercises the slice-8 follow-up path)
  - `list_tasks` (surfaces the scheduled task)
  - `cancel_task` (flips it to cancelled)
- Uses `RenderSystemPrompt(now, time.Local)` so the LLM sees the runtime time block (slice-8 follow-up). Threads a synthetic user ID via `tools.WithUserID` so the reminder branch of `schedule_task` works uniformly even though we only test wiki_maintenance kind here (which doesn't need a recipient).
- Live run on `glm-5.1:cloud`: **all 10 scenarios PASS first try**. The LLM picked the relative `in` field for the scheduler scenario (no UTC math) — the slice-8 follow-up is now battle-tested through a different model (Telegram run was on the user's primary model).
- Verification: `go build ./...` clean, `go vet ./...` clean, `go run ./cmd/debug_ingest` PASS 10/10.
- Files touched: `cmd/debug_ingest/main.go` (new), `docs/implementation-tracker.md`.
- Next slice: **10 — UI** (last remaining item; everything 1–9 is now done and exercised).

### 2026-04-30 — Slice 8 follow-up (current-time prompt + in/at_local)

- **Live-tested slice 8** with the bot running. First attempt: LLM picked `at=2026-04-30T06:48:00Z` which was already in the past (current UTC was 07:18) — validation rejected. LLM retried with `at=2026-05-01T06:43:00Z` (tomorrow morning), which was technically future but nowhere near the user's "fra 60 secondi" (in 60 seconds) intent. Fast-forwarded the row by hand to `now+30s` to prove the dispatcher fires (it did, ≤13s after the next tick).
- **Root cause**: the LLM has no ground-truth current time and can't reliably do timezone math. Two fixes shipped:
  1. **Runtime context in the system prompt**. `RenderSystemPrompt(now, loc)` appends a `## Runtime Context` block with current local time + UTC time + timezone + a brief recipe for the four schedule fields. `bot.go` calls it on every turn so the snapshot stays fresh.
  2. **Robust schedule fields on `schedule_task`**. Added `in` (relative duration: `60s`, `5m`, `2h`, `1d`) and `at_local` (wall-clock without offset, parsed in the configured timezone). Both bypass the LLM's UTC math entirely. Existing `at` (UTC ISO) and `daily` (HH:MM) still work; the four are mutually exclusive.
- `internal/conversation/system_prompt.go` — added `RenderSystemPrompt(now time.Time, loc *time.Location) string`. The original `DefaultSystemPrompt()` is preserved for callers that don't need wall-clock awareness.
- `internal/telegram/bot.go` — system prompt now refreshes on every user message via `convCtx.SetSystemMessage(conversation.RenderSystemPrompt(time.Now(), time.Local))`, replacing the once-per-conversation set.
- `internal/tools/scheduler.go` — `schedule_task` now accepts `in`, `at_local`, `at`, `daily`. Mutually exclusive: passing more than one is rejected up front. Past timestamps in `at_local` and `at` produce errors that include the current clock so the LLM has a hint on the next retry. New helper `parseLocalWallClock(s, loc)` accepts four shapes (`T`/space separator, with/without seconds), and rejects strings carrying timezone info (those belong in `at`).
- `internal/tools/scheduler_test.go` — added 4 happy-path tests (`TestScheduleTaskTool_RelativeIn`, `TestScheduleTaskTool_AtLocal` pinned to `Europe/Rome`, `TestScheduleTaskTool_AtLocalRejectsPast`, `TestParseLocalWallClock_AcceptsCommonShapes`/`_RejectsTimezoneSuffixes`) plus 4 new bad-input cases covering `in`/`at_local` validation. `TestParseLocalWallClock_AcceptsCommonShapes` skips when `Europe/Rome` tzdata is unavailable so the suite stays green on minimal images.
- Verification: `go test ./...` PASS (full suite); `go build ./...` clean; `go vet ./...` clean.
- Files touched: `internal/conversation/system_prompt.go` (added `RenderSystemPrompt` + `time` import), `internal/telegram/bot.go` (per-turn refresh), `internal/tools/scheduler.go` (new params + helper), `internal/tools/scheduler_test.go` (5 new tests + 4 new validation cases), `docs/implementation-tracker.md`.

### 2026-04-30 — Slice 8 complete (autonomous SQLite scheduler)

- Slice 8 (reminder/scheduler) done — reframed around the user's autonomy requirement: not just one-shot user reminders but a real cron with bootstrapped system jobs that survive process restarts.
- `internal/scheduler/types.go` — `Task` struct with two kinds (`reminder`, `wiki_maintenance`) and two schedule kinds (`at` ISO8601-UTC, `daily` HH:MM-local). `RecipientID` field captured from the LLM-call context so reminders go back to the right chat.
- `internal/scheduler/store.go` — SQLite `scheduled_tasks` table (idempotent migration), Upsert (UNIQUE-name conflict → updates schedule + payload), GetByName, List (sorted by next_run_at), DueTasks (active + next_run_at ≤ now), MarkFired, Cancel, Delete. Helper `NextDailyRun(daily, loc, after)` is the cron arithmetic — handles both initial scheduling and the post-fire roll-forward, including the at-fire-time edge case (advance to tomorrow). `ParseDailyTime` is strict (HH:MM, zero-padded, 0–23 / 0–59).
- `internal/scheduler/scheduler.go` — tick loop runs in a goroutine, immediate tick on startup so missed-while-offline tasks fire on boot. Pure `advance()` for state transitions (one-shot success → done, one-shot failure → failed, daily → reschedule + StatusActive even on dispatch failure so transient errors don't kill recurring jobs).
- `internal/scheduler/scheduler_test.go` — 14 test funcs / 21 cases. Three are explicit autonomy proofs: `TestScheduler_Autonomous` (schedule a task 500ms in the future, do nothing, verify the dispatcher fires it within 3s), `TestScheduler_AutonomousDailyReschedules` (recurring task fires + advances to tomorrow), `TestScheduler_PicksUpStaleTaskAfterRestart` (task with next_run_at in the past gets picked up on first tick — the restart-recovery contract).
- `internal/tools/scheduler.go` — three LLM tools:
  - `schedule_task` — `{name, kind, payload?, at?, daily?}`. Reminder kind requires user-id from context (rejected up front otherwise, so we never persist a task with no recipient). Mutually exclusive at/daily; rejects past `at`.
  - `list_tasks` — optional status filter, groups by status.
  - `cancel_task` — flips active → cancelled.
- `internal/tools/context.go` — `WithUserID(ctx, id)` / `UserIDFromContext(ctx)` so the bot can thread the calling user's Telegram ID into tool execution without polluting tool args. WithUserID with empty id is a no-op so existing IDs aren't clobbered.
- `internal/tools/scheduler_test.go` — 11 tests covering one-shot reminder happy path (asserts RecipientID captured from ctx), reminder-without-user rejection, daily wiki_maintenance happy path (asserts no recipient captured for autonomous tasks), 6 input-validation cases, list grouping + status filter, cancel + re-cancel, missing-name guard, context helper round-trip.
- `internal/telegram/bot.go` wiring:
  - Built scheduler store from `cfg.DBPath` (shares the SQLite file with FTS5 search; separate connection pool — fine for single-process).
  - Registered `schedule_task`, `list_tasks`, `cancel_task`.
  - `dispatchTask` method: `reminder` parses RecipientID and sends `⏰ <payload>` via `b.bot.Send(tele.ChatID(id), …)`; `wiki_maintenance` runs `RebuildIndex` + `Lint` (warns per issue) + `AppendLog("nightly-maintenance", "")` — pure deterministic, no LLM round-trip.
  - Bootstrap upsert of `nightly-wiki-maintenance` (kind=wiki_maintenance, daily=03:00) on boot. Idempotent via name uniqueness; restart doesn't duplicate, and a user `schedule_task` with the same name overrides.
  - `Start()` now also starts the scheduler goroutine; `Stop()` stops it and closes the DB.
  - Tool execution call site (line 505) wraps ctx with `tools.WithUserID(ctx, userID)` so reminders capture the right recipient.
- Verification: `go test ./...` PASS (scheduler 14 funcs, scheduler tools 11 funcs, full suite green); `go build ./...` clean; `go vet ./...` clean. One unrelated flaky network-port test in `internal/ocr` (httptest reuse on Windows) — passes on retry.
- Files touched: `internal/scheduler/types.go` (new), `internal/scheduler/store.go` (new, ~310 lines), `internal/scheduler/scheduler.go` (new, ~165 lines), `internal/scheduler/scheduler_test.go` (new, ~480 lines), `internal/tools/scheduler.go` (new, ~245 lines), `internal/tools/scheduler_test.go` (new, ~250 lines), `internal/tools/context.go` (new, ~30 lines), `internal/telegram/bot.go` (modified — import, scheduler creation, bootstrap, dispatcher, Start/Stop, ctx wiring), `docs/implementation-tracker.md`.
- Next slice: **9 — Natural prompt tests for OCR/ingest** (extend `cmd/debug_tools` or add `cmd/debug_ingest`). After that: slice 10 (UI), the only remaining item before standalone Aura is feature-complete per the PDR.

### 2026-04-30 — Slice 7 follow-up (live test, log.md empty-slug fix)

- **Live-tested all four slice 7 tools in one Telegram turn** with the prompt: "Do a full wiki maintenance pass: list every page so I can see what's there, run a lint check for broken links and missing categories, rebuild the index just to be safe, and append a log entry with action 'maintenance-pass' so we have a record."
- LLM decomposed it into the expected sequence: `list_wiki` (1ms, 196 bytes) → `lint_wiki` (1ms, 71 bytes) → `rebuild_index` (5ms) → `append_log` (8ms). All four returned cleanly; total elapsed ~330ms.
- **Cosmetic bug found**: `append_log` with no slug rendered the page cell as `[[]]` (literal empty wiki-link) — visible in `log.md` and rendered as a broken link in graph view. Fix: only wrap the slug in `[[...]]` when non-empty; emit a blank cell otherwise.
- Hand-fixed the stale `[[]]` row in `wiki/log.md` (one-time artifact from the live test before the fix).
- Test added: `TestAppendLogTool_EmptySlug` now also reads `log.md` and asserts no literal `[[]]` and that the row has a blank page cell.
- Verification: `go test ./...` PASS, `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/wiki/store.go` (3-line render fix in `appendLog`), `internal/tools/wiki_maintenance_test.go` (extended assertion).

### 2026-04-30 — Slice 7 complete

- Slice 7 (wiki maintenance tools) done. Most of the heavy lifting already lived in `internal/wiki/store.go` (`ListPages`, `Lint`, private `updateIndex` / `appendLog`), so the slice is mostly thin LLM tool wrappers plus exporting the two private helpers.
- `internal/wiki/store.go`: added public `RebuildIndex(ctx)` and `AppendLog(ctx, action, slug)` that delegate to the existing private methods. Kept the private versions so internal call sites in `WritePage` / `DeletePage` / `MigrateYAMLToMD` stay unchanged.
- `internal/tools/wiki_maintenance.go` (new):
  - `list_wiki` — `{category?, limit?}` (default 50, max 200). Returns pages grouped by category, sorted by category then slug, with `[[slug]]` wiki-links inline. Case-insensitive category filter. Output capped via `truncateForToolContext` at 8000 chars.
  - `lint_wiki` — no args. Wraps `wiki.Store.Lint`, groups issues by slug under `## [[slug]]` headers, emits "Wiki is clean: no issues found." when empty.
  - `rebuild_index` — no args. Calls `wiki.Store.RebuildIndex`, returns the page count from a follow-up `ListPages`.
  - `append_log` — `{action (required, ≤50 chars, trimmed), slug?}`. Surfaces `wiki.Store.AppendLog` so the LLM can record query/summary events that don't go through `WritePage`. Truncates over-long actions to keep `log.md` table rows readable. Empty/whitespace action rejected.
- `internal/telegram/bot.go`: registered all four tools always (no conditional gating — all four work as long as `wikiStore` exists, which is always true).
- `internal/tools/wiki_maintenance_test.go` (new): 15 unit tests covering empty wiki, multi-category grouping (incl. category sort order), case-insensitive filter, empty-filter result, limit truncation, nil-store guards on every tool, clean-lint, lint with mixed issues (broken link / broken related / missing category), rebuild over a corrupted `index.md`, append_log with/without slug, action-length truncation, empty-action rejection. Test helper `putPage` derives slug from title via `wiki.Slug` to mirror production.
- Verification: `go test ./...` PASS; `go build ./...` clean; `go vet ./...` clean.
- Files touched: `internal/wiki/store.go` (+13 lines), `internal/tools/wiki_maintenance.go` (new, ~280 lines), `internal/tools/wiki_maintenance_test.go` (new, ~310 lines), `internal/telegram/bot.go` (+5 lines wiring), `docs/implementation-tracker.md`.
- Next slice: **8 — Reminder/scheduler (SQLite `scheduled_tasks`, `schedule_task`, `list_tasks`, `cancel_task`)**. Independent of slices 1–7. Picobot has a battle-tested cron pattern in `picobot/internal/cron` and SQLite migration helpers — start there.

### 2026-04-30 — Slice 6 follow-up #2 (readable slugs, migration)

- **Problem reported**: source page slugs were opaque hex (`source-src-24abf740febd9eac`). Unreadable for the LLM and useless in the wiki graph view — every source clusters as `source-src-…` with no semantic differentiation. Violates the LLM-wiki principle from `docs/llm-wiki.md`: "the cross-references are already there… the wiki keeps getting richer".
- **Fix**: title now derives from the display filename (sans extension). `Source: uta.pdf` → title `Source: uta` → slug `source-uta`. `Source: MARCHETTO DAVIDE_DDT N. 90.pdf` → `source-marchetto-davide-ddt-n-90`. Empty filename falls back to `Source: <id>` so slugs are always valid.
- **Collision handling**: `Pipeline.resolveTitle` reads the candidate slug; if the wiki page there belongs to a different source, the title gets a short id suffix (first 6 hex of `src_…`) so `wiki.Slug(title)` produces a unique slug. Title (not slug) is the disambiguation point because `wiki.Store.WritePage` derives the on-disk filename from `page.Title`; making them disagree silently overwrites the older page (caught by the FilenameCollision test).
- **Migration**: `Compile` now compares `src.WikiPages` against the freshly-computed slug. If they differ (e.g. slug rule changed, or filename was renamed), the new page is written, the old slug(s) are best-effort deleted via `wiki.Store.DeletePage`, and `source.json` is updated to point only at the new slug. Wiki no longer accumulates dead pages on slug rule changes.
- **Idempotence is now slug-aware**: a re-Compile only short-circuits when status=ingested *and* `WikiPages == [newSlug]`. A stale-slug ingested source is treated as "needs migration" rather than "already done".
- **Live migration run** on the three pre-existing sources:
  - `src_24abf740febd9eac` (`uta.pdf`) → `source-uta`
  - `src_684b8214169e35bf` (`MARCHETTO DAVIDE_DDT N. 90.pdf`) → `source-marchetto-davide-ddt-n-90`
  - `src_437ecedcb716dbbf` (`4_5942613039617418204.pdf`) → `source-4-5942613039617418204`
  - All three old `source-src-<hex>.md` pages deleted; `source.json` `wiki_pages` updated; new pages have correct frontmatter and `Status: ingested`.
- **Tests added** (5 new + helper): `TestCompile_FilenameCollision` (two PDFs same filename get distinct slugs, neither overwrites the other), `TestCompile_MigratesStaleSlug` (planted stale page is rewritten and old slug deleted), `TestCompile_EmptyFilenameFallback` (empty filename → id-based fallback slug), `TestBuildTitle` (6 cases incl. extension stripping, whitespace, fallback), `TestShortID` (5 cases), `TestStaleSlugsToDelete` (4 cases). `TestCompile_HappyPath` updated to assert `source-paper` slug and `Source: paper` title. New helper `putOCRCompleteAs` lets tests pin filename and content for collision scenarios.
- **Style**: replaced manual `for` loop with `slices.Contains` for `pageBelongsTo` per gopls hint.
- Verification: `go test ./...` PASS (all tests + 5 new); `go test -tags=live_ingest -run TestLiveIngest` PASS on all three migrated sources; `go build ./...` clean; `go vet ./...` clean.
- Files touched: `internal/ingest/pipeline.go` (slug-resolution + migration logic, ~50 LOC), `internal/ingest/pipeline_test.go` (new tests + helper), `docs/implementation-tracker.md`.

### 2026-04-30 — Slice 6 follow-up (live test, Status fix, catch-up)

- **Live-tested slice 6 auto-ingest via real Telegram bot**: uploaded `uta.pdf` (1 page, 59 KB UTA fuel-card delivery letter) — OCR 1.35s, auto-hook fired ~210ms after OCR, final progress message read `✅ Done · src_24abf740febd9eac · 1 page · 1.6s · compiled as [[source-src-24abf740febd9eac]]`. `source.json` flipped to `status=ingested` with `wiki_pages` set. Wiki page on disk had full PDR §4 layout: frontmatter (`title`, `category=sources`, `tags=[source,pdf]`, `sources=[source:src_…]`), Metadata block, Raw OCR pointer, Preview block with the inlined Italian fuel-card form.
- **Cosmetic bug found and fixed**: rendered page body said `Status: ocr_complete` because `buildSummaryBody` was called before `sources.Update` flipped status. The page would never refresh on idempotent recompile (status=ingested → "already compiled" early-return), so the body was permanently wrong. Fix: render `source.StatusIngested` literally in `buildSummaryBody` since Compile only reaches the render step on success and the flip is the very next operation. Test updated to assert `Status: ingested` in the body.
- **Catch-up live test added**: `internal/ingest/live_test.go` (build tag `live_ingest`) takes `INGEST_SOURCE_IDS` from env and runs `Pipeline.Compile` on each. Asserts the wiki page is on disk with `Status: ingested` and `source.json` flipped. Same env-loading pattern as `internal/ocr/live_test.go`. Skips cleanly when env not set.
- **Catch-up run** on the two pre-hook sources from yesterday's live test: `INGEST_SOURCE_IDS="src_684b8214169e35bf,src_437ecedcb716dbbf" LIVE_WIKI_PATH=D:/Aura/wiki go test -tags=live_ingest -run TestLiveIngest -v ./internal/ingest/...` — both compiled cleanly. After this run, all three on-disk sources (`src_24abf740febd9eac`, `src_684b8214169e35bf`, `src_437ecedcb716dbbf`) report `status=ingested` with their corresponding wiki pages on disk. Stale `Status: ocr_complete` line in the live-tested `uta.pdf` page was hand-fixed in the wiki file (one-time artifact of the pre-fix run; future writes use the corrected renderer).
- **WIKI_PATH gotcha**: the live test reads `WIKI_PATH` from `.env`, which is `./wiki` (relative to the bot's run dir). Tests run from `internal/ingest/` so the relative resolves to a non-existent path. Override with `LIVE_WIKI_PATH=D:/Aura/wiki` (absolute) when running locally.
- Verification: `go test ./...` PASS (default tags), `go test -tags=live_ingest ...` PASS (catch-up), `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/ingest/pipeline.go` (1-line render fix + comment), `internal/ingest/pipeline_test.go` (new assertion), `internal/ingest/live_test.go` (new, build-tagged), `docs/implementation-tracker.md`.
- Wiki content files (`wiki/source-src-*.md`, `wiki/index.md`, `wiki/log.md`) are user data and intentionally not staged for commit. They live on disk only.

### 2026-04-30 — Slice 6 complete

- Slice 6 (ingestion pipeline) done:
  - `internal/ingest/pipeline.go`: `Pipeline.Compile(ctx, sourceID)` turns a `status=ocr_complete` source into a `Source <id>` wiki summary page, flips status to `ingested`, and (best-effort) reindexes the slug via `search.Engine.ReindexWikiPage`. Idempotent: a second call on an `ingested` source returns the existing slug with `Created=false` and a "already compiled" note. Emits a deterministic body — Metadata block, Raw OCR pointer (`wiki/raw/<id>/ocr.md`), and a 1000-char preview of the OCR body (header lines from `internal/ocr/render.go` are stripped so the preview starts at real content). UTF-8-safe truncation.
  - `Pipeline.AfterOCR(ctx, src) (note, err)`: adapter matching the new `telegram.AfterOCRHook` signature so the pipeline plugs straight into `docHandlerConfig.AfterOCR`.
  - `internal/tools/ingest.go`: `ingest_source` LLM tool (`source_id` → "Compiled / Already compiled source <id> as [[slug]]"). Lets the LLM re-run ingest on stored sources and is the recovery path when the auto-hook fails.
  - `internal/telegram/documents.go`: `AfterOCRHook` signature changed from `func(ctx, src) error` to `func(ctx, src) (note, err)`. The optional note replaces the static "ready for ingest" tail in the final progress edit, so a successful auto-ingest now ends as `✅ Done · src_… · N pages · Xs · compiled as [[source-src-…]]`. Hook failure logs and falls back to "ready for ingest" so the user can retry via `ingest_source`. Also fixed a `defer hookCancel()` inside the conditional that would have leaked the cancel until `process` returned — now an explicit `hookCancel()` after the call.
  - `internal/telegram/bot.go`: builds `ingest.Pipeline` unconditionally (only deps are sourceStore + wikiStore, both already present), registers `ingest_source` always, and wires `ingestPipeline.AfterOCR` into the Telegram doc handler.
  - `internal/ingest/pipeline_test.go`: 10 test funcs covering happy path (verifies wiki page contents, source flip to ingested, no preview leakage of OCR header lines), idempotence, missing-ocr.md error pointing at `ocr_source`, wrong-status error, unknown id, path-traversal id, the `AfterOCR` adapter shape, `buildPreview` (5 cases incl. zero/empty/truncate/no-header), UTF-8 boundary safety, and that `wiki.Store.WritePage` produces `index.md` + `log.md` side files.
- Design notes:
  - Title = `"Source " + sourceID` (not display filename). Two PDFs with the same display filename can't collide; the human-readable filename lives in the body.
  - `Source: source:<id>` frontmatter so the wiki schema picks up the source linkage.
  - Search reindex is best-effort (warn on failure) — the page is durable on disk regardless. Matches the slice 4 "OCR is durable even if downstream fails" pattern.
  - Hook signature change is a breaking change to the unexported `AfterOCRHook` type only; no external callers.
- Verification: `go test ./...` PASS (incl. `internal/ingest` 10 funcs / 15 cases, `internal/telegram` still passing the 12 slice-4 tests after signature change); `go build ./...` clean; `go vet ./...` clean.
- Files touched: `internal/ingest/pipeline.go` (new), `internal/ingest/pipeline_test.go` (new), `internal/tools/ingest.go` (new), `internal/telegram/bot.go` (modified — import, ingest pipeline build, registry register, AfterOCR wiring), `internal/telegram/documents.go` (modified — AfterOCRHook signature, tail composition, defer fix), `docs/implementation-tracker.md`.
- Pre-existing diagnostics in `bot.go` from slices 4–5 still out of scope.
- Next slice: **7 — Wiki maintenance tools (`append_log`, `rebuild_index`, `list_wiki`, `lint_wiki`)**. Surfaces the wiki/index/log machinery that already lives in `internal/wiki` to the LLM, and lets it audit/refresh wiki structure between ingest runs.

### 2026-04-30 — Slice 5 complete

- Slice 5 (LLM source tools) done:
  - `internal/tools/source.go`: 5 tools — `store_source` (text/url; PDFs are Telegram-only because the LLM can't stream binary), `ocr_source` (Mistral OCR pipeline mirror of `internal/telegram/documents.go` for re-OCR or post-hoc OCR), `read_source` (modes: metadata / ocr / excerpt; falls back to `original.txt`/`original.url` for non-PDF kinds when no `ocr.md`), `list_sources` (kind/status filter, default-20-max-100 limit, truncated indicator), `lint_sources` (buckets: stored awaiting OCR / OCR awaiting ingest / failed). Output capped via existing `truncateForToolContext`.
  - `internal/tools/source_test.go`: 13 unit tests — text+dedup, url+validation, nil-store, read modes (metadata/excerpt/ocr) incl. invalid id and bad mode, list filter+limit, list empty, lint buckets, lint clean, ocr_source no-client, ocr_source non-PDF reject, ocr_source happy path with httptest fake Mistral, ocr_source failure → status=failed + Error recorded.
  - `internal/telegram/bot.go`: registry wiring — source tools always registered when sourceStore exists; `ocr_source` only when `ocrClient != nil` so the LLM never sees a tool it can't actually run. Reordered the source/OCR setup above the registry block so the registry can see them.
- Design notes:
  - PDR §6 spec for `store_source` listed `path|url|content` inputs. Slice 5 deliberately omits `path` because the LLM has no filesystem; admin/console paths can come later. PDF entry stays Telegram-only.
  - `ocr_source` re-uses `ocr.RenderMarkdown` and `source.Store.Path` (containment-checked) so writes are bounded to `wiki/raw/<id>/`. On failure it flips status to `failed` and records the error message — same shape as the Telegram pipeline.
  - `read_source` modes are sized to fit the existing 8000-char tool budget (`maxSourceToolChars`); `excerpt` is 4000 chars to leave room for follow-up tool calls.
- Verification: `go test ./...` PASS (13 new tests); `go build ./...` clean; `go vet ./...` clean. Pre-existing `bot.go` lints (unused `userID`, `WriteString(fmt.Sprintf(...))`) were noted in slice 4 and remain out of scope.
- Files touched: `internal/tools/source.go` (new), `internal/tools/source_test.go` (new), `internal/telegram/bot.go` (modified — moved source/ocr setup above registry; added 4 always-on + 1 conditional source-tool registrations), `docs/implementation-tracker.md`.
- Next slice: **6 — Ingestion (`internal/ingest`)**. Pipeline turns `ocr.md` into compiled wiki pages with source backlinks, source summary page, and `wiki/log.md` entry. Wires into `docHandler.AfterOCR` so an uploaded PDF auto-ingests.

### 2026-04-30 — Multipage debug for `src_437ecedcb716dbbf`

- Symptom: 2-page Italian PMS PDF produced an `ocr.md` where `## Page 2` body is just `.`.
- Investigation:
  - `pdftotext -f 2 -l 2 wiki/raw/src_437ecedcb716dbbf/original.pdf` → empty output.
  - `pypdf` page 2: `extract_text() == ''`, no `/XObject`, no `/Resources`. Fully blank page in the source PDF.
  - `ocr.json` page 2: `markdown: "."`, empty `images`, `tables`, `hyperlinks`, header/footer null. Mistral correctly reported a near-empty page.
- Cause: not a flag interaction, not a Mistral bug — the source PDF really has a blank page 2. The flag re-test (`EXTRACT_HEADER=false EXTRACT_FOOTER=false INCLUDE_IMAGES=false`) would have shown the same `.` because those flags only affect header/footer/image extraction, never page-body text.
- No code change in this session; finding is for the renderer backlog.
- **Renderer follow-up (deferred, not slice 5):** detect "near-empty" pages (`strings.TrimSpace(body)` matches `.` or is empty) and render `## Page N (blank)` with no body, instead of literal `.`. This is a `internal/ocr/render.go` change only; leaves `ocr.json` untouched.
- **Re-render recipe (cheap, no new OCR calls):** since `ocr.json` is the raw Mistral response and `ocr.md` is a pure derivation, any renderer fix can be replayed against existing sources without API cost:

  ```go
  // pseudocode for a future cmd/rerender_ocr or similar
  for each dir in wiki/raw/*/:
      raw := read("ocr.json") // unmarshal into ocr.OCRResponse
      meta := ocr.RenderMeta{SourceID: id, Filename: source.Filename, Model: source.OCRModel}
      md   := ocr.RenderMarkdown(meta, raw)
      atomicWrite(dir/"ocr.md", md)
  ```

  Constraints: must reuse `internal/source.Store.Path` for containment, must atomic-rename, must skip dirs missing `ocr.json` (status=stored or failed). Add a `--dry-run` diff mode.

### 2026-04-29 — Slice 4 complete

- Slice 4 (Telegram PDF handler) done:
  - `internal/telegram/documents.go`: `docHandler` with bounded semaphore (`docConcurrencyLimit=2` simultaneous OCR jobs), single-message progress UX (initial reply → edits in place at each pipeline step → final ✅/❌), `AfterOCRHook` extension point for slice 6, validate-then-async pattern (handler returns within ~100ms; goroutine does the heavy lifting). `progressEditor` falls back to a fresh send if Edit fails (e.g. message deleted). Picobot/wiki conventions reused: per-key mutex (sync), atomic file writes via existing `source.Store.Path` containment.
  - `internal/telegram/bot.go`: `Bot` gained `sources`, `ocr`, `docs` fields. `New()` always builds a `source.Store` from `WIKI_PATH`; OCR client only when `OCR_ENABLED && MISTRAL_API_KEY != ""`. `registerHandlers` adds `tele.OnDocument` → `docs.onDocument` (gated on docs != nil so failures in source/OCR setup never break text handling).
  - `internal/telegram/documents_test.go`: 12 unit tests on pure functions — `validatePDF` (PDF/non-PDF/oversize/no-cap/nil/charset-suffixed mime), `safeName` (trim, empty, control chars, path chars, truncation), `formatSize` (B/KB/MB/GB rounding), `formatDuration` (ms / fractional s / s / m s), `pluralS`. Live Telegram round-trip is out of scope (needs actual Telegram session); the goroutine pipeline is exercised end-to-end already by the slice 3 follow-up `TestLiveE2E`.
- UX choices (single-message progress, bounded concurrency=2, dup-aware reply, error-as-final-edit) match the slice 4 design discussed before implementation.
- Verification: `go test ./...` PASS (incl. `internal/telegram` 12 new tests), `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/telegram/documents.go` (new), `internal/telegram/documents_test.go` (new), `internal/telegram/bot.go` (modified — imports, struct, New, registerHandlers), `docs/implementation-tracker.md`.
- Pre-existing diagnostics in `bot.go` (unused `userID` param, `WriteString(fmt.Sprintf(...))` style hints in `onStatus`) are out of slice 4 scope; left for a future cleanup commit.
- **Live-tested end-to-end via the actual Telegram bot** (`go run ./cmd/aura`, real PDFs uploaded by chat):
  - 1-page Italian receipt (RICEVUTA, 19 KB) — OCR 1.4s, 4-file layout written.
  - 1-page Italian DDT delivery note (55 KB) — OCR 2.3s, 4-file layout written.
  - 2-page Italian PMS test scenario (3 KB) — OCR 0.8s, ocr.md correctly emits `## Page 1` and `## Page 2` headings.
  - Each upload produced `original.pdf`, `source.json` (status=ocr_complete, ocr_model=mistral-ocr-latest, page_count, sha256), `ocr.md` (PDR §4 layout), `ocr.json` (raw Mistral response) under `wiki/raw/<source_id>/`. Filename sanitization preserved spaces in display while sha256 dedup keyed off content. Single-message progress UX confirmed.
- Next slice: **5 — LLM-facing source tools (`store_source`, `ocr_source`, `read_source`, `list_sources`, `lint_sources`)**. Lets the LLM drive the same pipeline (re-OCR a stored source, list inbox, surface unprocessed sources) and read source content into context for slice 6 ingest.

### 2026-04-29 — Slice 3 complete

- Slice 3 (OCR client) done:
  - `internal/ocr/types.go`: `OCRRequest` (wire body — verified against [Mistral basic_ocr docs](https://docs.mistral.ai/capabilities/document_ai/basic_ocr/) — includes `table_format`, `extract_header`, `extract_footer`, `include_image_base64`), `Document`, `OCRResponse`, `Page` (with header/footer), `Usage`.
  - `internal/ocr/client.go`: `Client` + `Config`. Bearer auth, JSON post, base64 PDF in `data:application/pdf;base64,...` URL, capped 256-char error snippets, 256 MiB response cap. HTTP shape mirrors `internal/tools/ollama_web.go`.
  - `internal/ocr/render.go`: `RenderMarkdown` produces PDR §4 ocr.md layout (`# Source OCR: <filename>`, `Source ID:`, `Model:`, then `## Page N`). Index+1 → 1-based display; defensive fallback when all pages report index=0.
  - Tests: 13 across `client_test.go` (success path verifies model/base64/auth header; include_images flag; extraction flags sent on wire; flags omitted when zero-valued; HTTP 401 doesn't leak API key; HTTP 500 snippet capped; bad JSON; empty bytes; missing base URL; trailing slash; default model) and `render_test.go` (PDR layout, model override, empty pages kept, all-zero-index fallback, missing filename placeholder).
- Wire-format correction: discovered late that `table_format`, `extract_header`, `extract_footer` are wire-level Mistral params (not Aura render hints as I initially assumed). Added them to `OCRRequest` and `Config`, plumbed from constructor to body, with tests asserting both presence-when-set and omission-when-zero (so `omitempty` correctly hides them from the JSON when defaulted).
- Verification: `go test ./...` PASS, `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/ocr/types.go`, `internal/ocr/client.go`, `internal/ocr/render.go`, `internal/ocr/client_test.go`, `internal/ocr/render_test.go`, `docs/implementation-tracker.md`.
- Next slice: **4 — Telegram PDF handler (`internal/telegram/documents.go`)**. Allowlist-gated PDF upload from Telegram, MIME/size validation against `OCR_MAX_FILE_MB`, download to `wiki/raw/<source_id>/`, `source.Store.Put`, then call `ocr.Client.Process` if `OCR_ENABLED`, write `ocr.md` + `ocr.json` via `source.Store.Path`, flip status to `ocr_complete`. No raw PDF text or base64 in logs (PDR §9).

### 2026-04-29 — Slice 2 complete

- Slice 2 (source store) done:
  - `internal/source/source.go`: `Kind` (pdf/text/url), `Status` (stored/ocr_complete/ingested/failed), `Source` struct matching PDR §4 schema.
  - `internal/source/store.go`: `Store` rooted at `<wiki>/raw/`. `Put` (sha256 dedup + atomic write), `Get`, `List` (kind/status filter, sorted desc), `Update` (mutator pattern), `Path` (containment-checked join), `RawDir`. Per-id mutex via `sync.Map`. Atomic temp+rename copied from `internal/wiki/store.go`. Regex ID validation pattern adapted from picobot's `isValidMemoryFile`.
  - `internal/source/store_test.go`: 10 test funcs — create, dedup, not-exist, invalid IDs (incl. traversal), list filters + bogus entries skipped, update persistence, mutator-error propagation, validation, path traversal rejection, all 3 kinds.
- Source ID format: `src_<first 16 hex of sha256>` — stable, dedupable, filesystem-safe. External IDs validated against `^src_[a-f0-9]{16}$` before any path join.
- Verification: `go test ./...` PASS (incl. `internal/source` 10 tests), `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/source/source.go` (new), `internal/source/store.go` (new), `internal/source/store_test.go` (new), `docs/implementation-tracker.md`.
- Next slice: **3 — OCR client (`internal/ocr`)**. Mistral `/v1/ocr` request/response, base64 PDF path, fake-server tests. Integrates with `source.Store.Update` to flip status to `ocr_complete` and write `ocr.md` / `ocr.json` via `source.Store.Path`.

### 2026-04-29 — Slice 1 complete

- Created this tracker per `aura-implementation` skill First Actions.
- Slice 1 (config) done:
  - `internal/config/config.go`: added `MistralAPIKey`, `MistralOCRModel`, `MistralOCRBaseURL`, `MistralOCRTableFormat`, `MistralOCRIncludeImages`, `MistralOCRExtractHeader`, `MistralOCRExtractFooter`, `OCREnabled`, `OCRMaxPages`, `OCRMaxFileMB` with PDR §3 defaults. Keys deliberately separate from `LLM_API_KEY` and `EMBEDDING_API_KEY`.
  - `internal/config/config_test.go`: extended `TestLoadSuccess` to assert OCR defaults and unset OCR env vars.
  - `.env.example`: documented OCR section.
- Verification: `go test ./...` (all packages PASS), `go build ./...` (clean), `go vet ./...` (clean).
- Files touched: `internal/config/config.go`, `internal/config/config_test.go`, `.env.example`, `docs/implementation-tracker.md`.
- Next slice: **2 — Source store (`internal/source`)**. Needs source ID generation (sha256 + ULID), `wiki/raw/<source_id>/` layout, atomic `source.json` write, listing, and tests for dedupe by sha256.
- Pre-existing diagnostic noted (not introduced this slice): `internal/config/config.go:52` — `IsAllowlisted` loop could use `slices.Contains`. Out of scope.
