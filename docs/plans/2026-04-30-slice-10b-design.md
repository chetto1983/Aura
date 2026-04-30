# Slice 10b — Frontend Scaffold + Wiki/Graph Views

**Status:** approved 2026-04-30
**Reference:** `references/phase-10-ui.md` (slice 10b section); upstream PDR §12 item 10
**Predecessor:** slice 10a (read-only HTTP API at `/api/*`) — committed `371a068`
**Successors:** 10c (write actions), 10d (auth), 10e (polish)

## Decision summary

| # | Decision | Choice |
| - | -------- | ------ |
| 1 | Distribution | Single self-contained Go binary; `web/dist` committed to git |
| 2 | Routing | `react-router-dom` v7 with browser router, 5 routes |
| 3 | Network exposure | Bind listener to `127.0.0.1:8080` by default until 10d auth ships |
| 4 | Home view | `HealthDashboard` at `/` |
| 5 | Frontend approach | Approach 1: copy `D:\sacchi_Agent\frontend\src-app` → `web/`, prune sacchi-specific files, rewire to Aura's `/api/*` |
| 6 | Existing QR landing page | Deleted entirely; dashboard owns `/` |
| 7 | TS types | Hand-written in `src/types/api.ts`, mirroring `internal/api/types.go` |
| 8 | Data layer | Plain `useApi` hook (no SWR / TanStack Query) |
| 9 | Stale data on poll failure | Keep showing previous data with a `⚠ stale` pill |
| 10 | Fetch timeout | 8 seconds via `AbortController` |
| 11 | Frontend unit tests | None in 10b; `tsc --noEmit` + eslint + manual checklist + Go-side embed test |

## Architecture

Single Go binary embeds the built SPA. Browser talks to:

```
[browser] ──HTTP──► localhost:8080
                     ├─ /api/*  → internal/api router (slice 10a)
                     ├─ /health, /status (existing)
                     └─ /*      → static handler serving web/dist via //go:embed
                                  with SPA fallback (any unmatched GET → index.html)
```

In dev, Vite runs at `:5173` and proxies `/api/*` to `:8080` so the React HMR loop stays fast while the Go bot keeps running unchanged.

## Repo layout

```
D:\Aura\
  web/                              ← copied from D:\sacchi_Agent\frontend\src-app then pruned
    package.json                    ← drop @copilotkit/*, @ag-ui/*, cmdk, vaul; add react-router-dom@7
    vite.config.ts                  ← dev proxy /api → :8080; build.outDir = "../internal/api/dist"
    tsconfig*.json, eslint.config.js, components.json
    index.html
    public/                         ← favicon set generated from Logo/logo.png
    src/
      main.tsx, App.tsx             ← App = <BrowserRouter> with 6 <Route>s
      api.ts                        ← typed fetch wrappers around /api/*
      types/api.ts                  ← hand-written DTOs mirroring internal/api/types.go
      hooks/
        useAppTheme.ts              ← copied (dark-mode persist via localStorage)
        useApi.ts                   ← new shared fetch+poll hook
        useAura.ts                  ← renamed from sacchi useSessions, repurposed for /health polling
      components/
        Sidebar.tsx                 ← copied + rewired (drop skill/agent links)
        WikiPanel.tsx               ← copied + rewired to /api/wiki/pages and /api/wiki/page
        WikiGraphView.tsx           ← copied; force-graph physics retained as-is
        WikiEditor.tsx              ← copied + tiptap forced to read-only
        WikiPageView.tsx            ← NEW: /wiki/:slug, wraps read-only WikiEditor
        SourceInbox.tsx             ← NEW: /sources, table grouped by status
        TasksPanel.tsx              ← NEW: /tasks, table grouped by status
        HealthDashboard.tsx         ← NEW: /, three cards
        Markdown.tsx                ← copied as-is
        EventStrip.tsx              ← copied + simplified (drop agent event stream)
        StderrLogSheet.tsx          ← copied; populated by 10e
        ErrorBoundary.tsx           ← NEW: catches render errors per-route
        ui/                         ← copied (shadcn primitives)
      lib/, types/                  ← copied; data/ deleted (sacchi fixtures)
    dist/                           ← committed; rebuilt by `npm run build`
  internal/api/
    static.go                       ← NEW: //go:embed all:dist + SPA fallback handler
    dist/                           ← Vite output dir (committed)
  internal/config/config.go         ← change HTTPPort default to "127.0.0.1:8080"
  internal/health/server.go         ← drop landing page handler entirely
  internal/tray/tray_windows.go     ← add "Open Dashboard" menu item
  cmd/aura/main.go                  ← mount static handler at "/"
  Makefile                          ← add `web`, `web-build`, `ui-dev` targets
```

## Routes & components

| Route | Component | Data | Polls |
| ----- | --------- | ---- | ----- |
| `/` | `HealthDashboard` | `GET /api/health` | 5s |
| `/wiki` | `WikiPanel` | `GET /api/wiki/pages` | no |
| `/wiki/:slug` | `WikiPageView` | `GET /api/wiki/page?slug=X` | no |
| `/graph` | `WikiGraphView` | `GET /api/wiki/graph` | no |
| `/sources` | `SourceInbox` | `GET /api/sources` | 5s |
| `/tasks` | `TasksPanel` | `GET /api/tasks` | 5s |

### Component sketches

- **Sidebar** — fixed 240 px panel. Nav: Home, Wiki, Graph, Sources, Tasks. Bottom: dark-mode toggle + build-version label.
- **HealthDashboard** — three cards stacked: Wiki (page count + relative-time last updated), Sources (horizontal stacked bar by status), Tasks (active count + countdown to next run).
- **WikiPanel** — search input + category filter chips + grouped table (Title · Tags · Updated). Click row → `/wiki/:slug`.
- **WikiPageView** — header (title, breadcrumbs, back link, category, tags) + read-only `WikiEditor` populated from `body_md`.
- **WikiGraphView** — react-force-graph-2d canvas; nodes colored by category; click → `/wiki/:slug`; hover tooltip with title.
- **SourceInbox** — grouped table by status (Failed → Stored → OCR complete → Ingested), columns Filename · Created · Pages · Wiki pages. Failed rows show `error` in a collapsible cell.
- **TasksPanel** — grouped table by status (Active → Done → Cancelled → Failed). Active rows tick a live countdown to `next_run_at` every second.
- **EventStrip** — thin top bar: bot status (online from last successful `/api/health`), build version, current time.
- **Markdown / StderrLogSheet** — copied verbatim; `StderrLogSheet` stays empty until 10e.

### Files removed from the sacchi copy

- `copilot-spike.tsx`, `useAgent.ts`, `ChatPanel.tsx`, `ArtifactRouter.tsx`, `GenericArtifact.tsx`
- `ProductCard.tsx`, `ProductDetailPanel.tsx`, `ProductArtifact.tsx`
- `SkillsCommand.tsx`, `SkillsDialog.tsx`, `SkillCard.tsx`, `SkillDetailSheet.tsx`, `SkillBadges.tsx`, `useSkillInstall.ts`, `useSkillsRegistry.ts`, `InstallButton.tsx`, `UninstallConfirmDialog.tsx`, `PermissionConsent.tsx`
- `data/` (sacchi-specific fixtures)

### npm dependency delta

Removed: `@copilotkit/react-core`, `@copilotkit/react-ui`, `@ag-ui/client`, `@ag-ui/core`, `cmdk`, `vaul`
Added: `react-router-dom@^7`
Kept: react 19, react-dom, vite, tailwindcss v4, shadcn primitives, lucide-react, react-force-graph-2d, react-markdown, remark-gfm, @tiptap/* (read-only renderer for WikiEditor), zod (still useful for API runtime sanity if drift surfaces), sonner, class-variance-authority, clsx, tailwind-merge, @fontsource-variable/geist

## Data flow

### `src/api.ts`

Single typed module wrapping `fetch`. `ApiError` carries `status` and `message`. Network failures → `status: 0`. 8-second `AbortController` per request.

```ts
export const api = {
  health:     () => get<HealthRollup>("/health"),
  wikiPages:  () => get<WikiPageSummary[]>("/wiki/pages"),
  wikiPage:   (slug: string) => get<WikiPage>(`/wiki/page?slug=${encodeURIComponent(slug)}`),
  wikiGraph:  () => get<Graph>("/wiki/graph"),
  sources:    (q?) => get<SourceSummary[]>("/sources" + qs(q)),
  source:     (id) => get<SourceDetail>(`/sources/${id}`),
  sourceOCR:  (id) => get<{ markdown: string }>(`/sources/${id}/ocr`),
  tasks:      (q?) => get<Task[]>("/tasks" + qs(q)),
  task:       (name) => get<Task>(`/tasks/${name}`),
};
```

### TypeScript DTOs

Hand-written in `src/types/api.ts`, mirroring `internal/api/types.go`. ~80 LOC. No codegen tool. Drift detection: a missing/renamed field surfaces at runtime when the response fails to parse — visible quickly because polling exercises every endpoint.

### `useApi` hook

```ts
function useApi<T>(fetcher: () => Promise<T>, intervalMs?: number) {
  const [state, set] = useState<{ data?: T; error?: Error; loading: boolean }>({ loading: true });
  useEffect(() => {
    let alive = true;
    const tick = () =>
      fetcher()
        .then(d => alive && set({ data: d, loading: false }))
        .catch(e => alive && set(s => ({ ...s, error: e, loading: false })));
    tick();
    if (!intervalMs) return () => { alive = false };
    const id = setInterval(tick, intervalMs);
    return () => { alive = false; clearInterval(id); };
  }, [fetcher, intervalMs]);
  return state;
}
```

- First fetch failure → `error` set, `data` undefined → component renders error state with Retry.
- Subsequent fetch failure → keep stale `data`, set `error`, render `⚠ stale` pill on the card. Stale data is more useful than a wiped panel.
- Tab visibility: pause interval when `document.visibilityState === "hidden"`.
- No SWR / TanStack Query. Adds 12+ KB and a model not paid back at this scale. Revisit at 10c when mutations land.

### Cross-route caching

Skipped. Each route owns its fetch.

### Navigation

`<Link to="/wiki/some-slug">` from `WikiPanel` rows; `useNavigate()` from `WikiGraphView` node clicks. Browser back/forward via react-router.

## Error handling

Polled read-only viewer. Every API failure is recoverable on the next tick.

| State | UI |
| ----- | -- |
| First fetch loading | shadcn `<Skeleton>` rows |
| First fetch error | error card with message + Retry button |
| Empty result | explicit empty message ("No tasks scheduled") |
| Subsequent fetch error | stale data + `⚠ stale` corner pill |
| 404 on `/wiki/:slug` | "Page not found" card + back link; URL preserved |
| Render error | `<ErrorBoundary>` per route → "Something went wrong — refresh" card |

`<Toaster />` mounted at app root for slice 10c. Silent in 10b.

Explicitly out of scope for 10b: 401 auth handling (10d), optimistic update rollback (10c), SSE/WebSocket reconnection (deferred), service worker / offline-first (out of project scope).

## Static asset embedding

`internal/api/static.go`:

```go
//go:embed all:dist
var distFS embed.FS

func StaticHandler() http.Handler { ... }
```

`StaticHandler` returns a handler that:
1. Serves `dist/<path>` for any GET that resolves to a real asset
2. Falls back to `dist/index.html` for any unmatched GET that doesn't start with `/api/`, `/health`, `/status` — this is what makes deep-link refresh work for `/wiki/source-paper`, `/graph`, etc.

`cmd/aura/main.go` mounts the handler at `"/"` on the existing health server *after* `bot.APIHandler()` is mounted at `/api/`. ServeMux routes longer paths first so `/api/*` always wins.

## Build pipeline

Adds to `Makefile`:

```make
.PHONY: web web-build ui-dev

web:
	cd web && npm run dev

web-build:
	cd web && npm install && npm run build

ui-dev:
	$(MAKE) -j2 web run

build: web-build
	go build ./...
```

`web/dist` is committed. Frontend changes flow: edit `src/*` → `make web-build` → `git add internal/api/dist web/<changed>` → commit. A pre-commit hook to verify `dist` is up-to-date is deferred to 10e.

## Side wiring

- `internal/config/config.go` — `HTTPPort` default flips from `:8080` to `127.0.0.1:8080`. `.env.example` updated; `.env` left alone (user may have set their own).
- `internal/health/server.go` — `handleLanding` and the `landingPage` HTML constant deleted. The `/` mux entry replaced by the static handler in `main.go`. `SetBotUsername` becomes a no-op (or the method is removed; deferred decision when we touch the file).
- `internal/tray/tray_windows.go` — add `Open Dashboard` menu item above `Quit Aura`. Click handler shells out to the OS open command (`exec.Command("rundll32", "url.dll,FileProtocolHandler", "http://localhost:8080")`). Non-Windows stub leaves it absent.
- `cmd/aura/main.go` — mount order: `healthServer.Mount("/api/", http.StripPrefix("/api", bot.APIHandler()))`, then `healthServer.Mount("/", api.StaticHandler())`.

## Testing

| Check | How |
| ----- | --- |
| TS compiles | `tsc --noEmit` via `vite build` |
| Lint | `npm run lint` (eslint) |
| Embed works | `internal/api/static_test.go` — three cases: `/` returns `index.html`, `/wiki/anything` SPA-falls-back, `/api/health` still routes to API |
| Existing Go suite | `go test ./...` — must remain green |
| Race | `go test -race ./internal/api/...` — must remain clean |

No frontend unit tests in 10b. Vitest deferred to 10e if a regression surfaces.

### Manual checklist (`docs/phase-10-ui-test-plan.md`)

- [ ] `cd web && npm install && npm run build` succeeds; `internal/api/dist/index.html` exists
- [ ] `go run ./cmd/aura` starts; tray icon appears; `http://localhost:8080/` loads HealthDashboard
- [ ] `http://10.0.0.X:8080/` from another LAN device is refused (binding fix)
- [ ] All 5 sidebar nav items navigate; URL updates; back/forward works
- [ ] `/wiki` shows seeded pages; click into one renders body markdown
- [ ] `/wiki/source-paper` opened in a fresh tab loads directly (SPA fallback)
- [ ] `/graph` renders force-directed graph; node click navigates to `/wiki/:slug`
- [ ] `/sources` shows the 3 live-tested PDFs grouped under Ingested
- [ ] `/tasks` shows `nightly-wiki-maintenance` under Active with live countdown
- [ ] HealthDashboard shows correct counts; 5s poll updates after a Telegram upload
- [ ] Dark-mode toggle in sidebar persists across reload
- [ ] Disconnect bot (`Ctrl+C`) → dashboard shows ⚠ stale pills, doesn't blank
- [ ] Reconnect bot → pills clear within 5s
- [ ] Tray "Open Dashboard" menu item launches a browser at `http://localhost:8080`

## Acceptance bar

All 5 views render against the live API; every checklist item passes; full Go suite green; vet + race clean; one atomic commit titled `slice 10b: frontend scaffold + wiki/graph views`.

Visual polish (mobile drawer < 768 px, hand-tuned skeletons, keyboard shortcuts, Lighthouse pass) is explicitly slice 10e and not blocking 10b.

## Risk register

| Risk | Likelihood | Mitigation |
| ---- | ---------- | ---------- |
| Sacchi components import skill/copilot files we deleted | High | Build will fail visibly; fix imports per file; budget 1–2 hours |
| `react-force-graph-2d` performance with >100 nodes | Low for now | Aura wiki has ~10 pages; revisit at 1k+ |
| `web/dist` bloats git history | Medium | Accept for solo project; if it becomes an issue, switch to release-tag-only commits or move to CI builds |
| Tiptap read-only mode still ships full editor JS | Low | Bundle size impact ~30 KB gz; acceptable. Swap to react-markdown alone if it becomes painful |
| LAN binding lockdown surprises a user who expected `:8080` to work from phone | Low | Documented in CHANGELOG / commit body; one-line override (`HTTP_PORT=0.0.0.0:8080`) |

## Out of scope

Reaffirming the plan's deferred items so they don't creep:

- Multi-user support (single-tenant per Telegram allowlist)
- Real-time push (SSE / WebSocket) — polling is fine
- Browser PDF upload (Telegram remains the entry point)
- Wiki edit-from-browser (LLM owns wiki writes per `docs/llm-wiki.md`)
- Auth (slice 10d)
- Mobile drawer / keyboard shortcuts / Lighthouse polish (slice 10e)

## Hand-off

Next: invoke the `writing-plans` skill to expand this design into a sequenced implementation plan (file-level deltas, ordered steps, commit-able subtasks).
