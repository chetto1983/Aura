---
phase: 23-frontend-infrastructure-industrial-foundation
plan: 03
subsystem: infra
tags: [go-embed, aura-serve, http-servemux, ci, github-actions, playwright, docker, multistage, node24, frontend]

# Dependency graph
requires:
  - phase: 23-01 (FND-01/02/03)
    provides: internal/webui //go:embed all:dist leaf package + Handler(); web/ npm package + lockfile + zero-warning gate configs; playwright.config.ts webServer stub
  - phase: 23-02 (FND-02/04/05)
    provides: real committed Vite bundle at internal/webui/dist (branded shell, theme-before-paint); vite build.outDir = ../internal/webui/dist
provides:
  - aura serve mounts internal/webui.Handler() additively at / on the loopback http.Server; AG-UI routes keep priority (newServeHandler parent mux)
  - cmd/aura/serve_webui_test.go (stdlib httptest, fake AG-UI handler) proving / -> 200 shell, /healthz + /threads/ AG-UI priority, bogus path -> 404
  - four path-filtered Node-24 blocking web CI jobs (web-lint, web-test, web-dist-freshness, web-e2e) in the unified ci.yml
  - scripts/web_dist_freshness.sh (git diff --exit-code -- internal/webui/dist) + Makefile web-freshness target
  - docker/aura/Dockerfile node:24-bookworm-slim webbuild stage feeding COPY --from=webbuild /internal/webui/dist into the go build stage
  - playwright.config.ts webServer.env forwarding DB/serve vars from process.env (A-SERVE-NOSTACK resolved)
affects: [24-web-foundation, 25-chat-approval]

# Tech tracking
tech-stack:
  added: []
  patterns: [additive-parent-servemux-mount, go122-longest-pattern-precedence, fake-handler-precedence-assertion, dorny-paths-filter-web-jobs, playwright-webserver-env-forwarding, node24-webbuild-multistage, git-diff-exit-code-freshness-gate]

key-files:
  created:
    - cmd/aura/serve_webui.go
    - cmd/aura/serve_webui_test.go
    - scripts/web_dist_freshness.sh
  modified:
    - cmd/aura/serve.go
    - .github/workflows/ci.yml
    - Makefile
    - web/playwright.config.ts
    - docker/aura/Dockerfile

key-decisions:
  - "Additive parent http.ServeMux (newServeHandler) over a Server.Mux() refactor — Phase 24 owns the real SPA-fallback/route-exclusion (WEB-01); Phase 23 only renders the static placeholder at /"
  - "Go 1.22 longest-pattern precedence makes the AG-UI prefixes authoritative over the / catch-all; a fake AG-UI handler in the test makes the precedence assertion genuine (not just 'both handlers exist')"
  - "Freshness gate + Dockerfile COPY target adapted from the plan's web/dist to internal/webui/dist (Go //go:embed is package-relative; vite outDir is ../internal/webui/dist — 23-01 Dev#1 / 23-02 carryover)"
  - "web-e2e provisions docker-compose Postgres + migrate + dummy OPENROUTER_API_KEY (A-SERVE-NOSTACK): aura serve hard-requires a PG pool (db.Open unconditional in bootServe), so a stack-free boot is impossible — mirror the AG-UI live smoke env"
  - "playwright.config.ts forwards an allow-list of DB/serve vars from process.env into webServer.env (Playwright does not inherit runner env, #19780)"

requirements-completed: [FND-02, FND-03, FND-06]

# Metrics
duration: 8min
completed: 2026-06-16
---

# Phase 23 Plan 03: Serve Mount, Web CI Jobs & Node-24 Docker Stage Summary

**The embedded operator SPA is wired into the running daemon and the quality pipeline: `aura serve` mounts `internal/webui.Handler()` additively at `/` (AG-UI routes keep priority, no SPA-fallback), four path-filtered Node-24 blocking web CI jobs (lint/test/freshness/e2e) land in the unified `ci.yml`, a `git diff --exit-code -- internal/webui/dist` freshness gate + Makefile target tamper-proof the committed bundle, and a `node:24-bookworm-slim` webbuild Docker stage rebuilds the dist reproducibly with no Node-for-SPA in the runtime image.**

## Performance

- **Duration:** ~8 min
- **Started:** 2026-06-16T10:57:06Z
- **Completed:** 2026-06-16T11:04:45Z
- **Tasks:** 3 (all auto)
- **Files modified:** 8 (3 created, 5 modified)

## Accomplishments

- **FND-02 serve mount:** `cmd/aura/serve_webui.go` adds `newServeHandler(aguiHandler)` — a parent `http.ServeMux` that registers the AG-UI route prefixes (`/healthz`, `/readyz`, `/debug/vars`, `/metrics`, `/agent/run`, `/threads/`) to the AG-UI handler and the `/` catch-all to `webui.Handler()`. Go 1.22 longest-pattern precedence keeps the AG-UI routes authoritative; the embed adds no new listener or bind (T-23-06). `bootServe` now sets `httpSrv.Handler` to this parent handler and **fails the daemon boot** (closing the chatEnv) on a webui embed error rather than mounting a half-wired host. `internal/webui` stays leaf-level — the AG-UI boundary gate stays green. **No SPA-fallback / index.html-fallback / API-404 exclusion** was added (deliberately Phase 24 / WEB-01); a missing asset under `/` 404s from `http.FileServerFS`.
- **FND-02 deterministic proof:** `cmd/aura/serve_webui_test.go` (stdlib `testing` + `httptest`, **no testify**) drives a **fake AG-UI handler** that records the paths it receives, so the precedence assertion is genuine: `GET /` → 200 `text/html` with the `data-theme` + `aura` brand markers; `GET /healthz` and `GET /threads/abc/messages` route to the AG-UI handler (hit-count asserted); `GET /no-such-asset.js` → 404 with **zero** leakage to the AG-UI handler. `go vet`/`build`/`test`/`-race` + `agui_boundary_check.sh` + `check-file-size.sh` all green.
- **FND-03/FND-06 web CI:** four Node-24 jobs in the unified `ci.yml` (D-17), each guarded by `dorny/paths-filter@v3` on `web/**` + `.github/workflows/ci.yml` so Go-only PRs no-op them green, all **blocking on every PR** (D-16): `web-lint` (`npm run lint` `--max-warnings=0` + `format:check` + `typecheck` — the golangci-lint parity bar, D-03), `web-test` (Vitest + coverage), `web-dist-freshness` (`fetch-depth: 0` + the freshness script), and `web-e2e` (provisions docker-compose Postgres + `aura db migrate` + builds the fresh dist + `go build -o aura` + Playwright Chromium + `npm run test:e2e`, teardown `if: always()`).
- **FND-06 freshness gate:** `scripts/web_dist_freshness.sh` (mirrors `agui_boundary_check.sh`'s header — `set -euo pipefail`, human-readable failure line, explicit exit codes) rebuilds `web/` and asserts `git diff --exit-code -- internal/webui/dist/` is empty (T-23-01 tamper-evidence). `Makefile` gains a `web-freshness` target shelling to it.
- **FND-06 Docker:** `docker/aura/Dockerfile` gains a `node:24-bookworm-slim AS webbuild` stage before the go build stage; the go stage `COPY --from=webbuild /internal/webui/dist ./internal/webui/dist` overwrites the committed embed source with a fresh reproducible build (ties D-05↔D-06). The webbuild Node never reaches runtime (multi-stage discards it). A comment documents the runtime stage's existing Node-24 as **orthogonal** — it serves recipe MCP servers (mail-mcp), never the SPA, which is baked into the Go binary before runtime (so a reviewer does not read runtime-Node as a D-06 violation).
- **A-SERVE-NOSTACK resolved:** `aura serve` hard-requires a Postgres pool (`bootServe` → `bootServeChatEnv` → `db.Open` is unconditional), so a stack-free boot is impossible. `web-e2e` provisions the stack exactly like the AG-UI live smoke (the `integration-test` job env), and `web/playwright.config.ts`'s `webServer.env` forwards the DB/serve vars (`AURA_DB_URL`/`AURA_DB_MIGRATE_URL`/`OPENROUTER_API_KEY`/…) from `process.env` (Playwright does not inherit the runner env — #19780).

## internal/webui/dist Path Adaptation (vs the plan's web/dist text)

The plan text (frontmatter, Task 2, Task 3, threat model) references `web/dist` and `cd web && git diff -- dist/`. **`web/dist` does not exist** — 23-02 set vite `build.outDir = ../internal/webui/dist` because Go `//go:embed all:dist` is package-relative (the embed source can only live at `internal/webui/dist`). Two deliverables were adapted from the repo root:

1. **`scripts/web_dist_freshness.sh`** — builds via `( cd web && npm ci && npm run build )` (writing to `../internal/webui/dist`) and diffs `git diff --exit-code -- internal/webui/dist/` from the repo root, not `web/dist`.
2. **`docker/aura/Dockerfile`** — at `WORKDIR /web`, `npm run build` (outDir `../internal/webui/dist`) writes to `/internal/webui/dist`; the go stage `COPY --from=webbuild /internal/webui/dist ./internal/webui/dist` (the plan's `/web/dist` is never created).

These are not bugs — they are the only paths the `//go:embed` directive can read, established in 23-01 Deviation #1 and 23-02.

## Verify Evidence

| Gate | Command | Result |
|------|---------|--------|
| serve mount vet/build | `go vet ./cmd/aura/ && go build ./cmd/aura/` | GREEN |
| serve mount httptest | `go test ./cmd/aura/ -run TestServeWebui -count=1` | GREEN (/ 200 shell, /healthz + /threads/ AG-UI priority, bogus 404) |
| serve mount race | `go test ./cmd/aura/ -run TestServeWebui -race -count=1` | GREEN |
| broad cmd/aura | `go test ./cmd/aura/ -count=1` | GREEN (no pre-existing failure surfaced this run) |
| agui boundary | `bash scripts/agui_boundary_check.sh` | GREEN (webui stays leaf) |
| file-size | `bash scripts/check-file-size.sh` | GREEN (all ≤600 LOC) |
| freshness script syntax | `bash -n scripts/web_dist_freshness.sh` | GREEN (+ greps internal/webui/dist + git diff --exit-code) |
| ci.yml jobs present | `grep web-e2e / web-dist-freshness / node-version / AURA_DB_URL` | GREEN (4 web jobs) |
| ci.yml parses | `python -c "yaml.safe_load(...)"` | GREEN (16 jobs incl. web-lint/web-test/web-dist-freshness/web-e2e) |
| Makefile target | `grep web-freshness Makefile` | GREEN |
| Dockerfile webbuild | `grep 'FROM node:24-bookworm-slim AS webbuild' + 'COPY --from=webbuild /internal/webui/dist'` | GREEN |
| web typecheck (config edit) | `cd web && npm run typecheck` | GREEN (exit 0) |
| web lint (config edit) | `cd web && npm run lint` | GREEN (exit 0, --max-warnings=0) |
| web format:check (config edit) | `cd web && npm run format:check` | GREEN (exit 0) |

## Task Commits

Each task was committed atomically (imperative subject + why body + Co-Authored-By):

1. **Task 1: Additively mount webui.Handler() into aura serve (no SPA-fallback) + httptest** — `4dcba3fd` (feat)
2. **Task 2: Four path-filtered web CI jobs + web-dist-freshness gate script + Makefile target** — `4dd172c8` (feat)
3. **Task 3: Node-24 webbuild Docker stage (no Node-for-SPA in runtime)** — `776c6163` (feat)

_Plan metadata commit (this SUMMARY + STATE + ROADMAP) follows._

## Decisions Made

- **Additive parent mux, not a `Server.Mux()` refactor** — RESEARCH Open Question 2's minimal-additive recommendation. `newServeHandler(http.Handler)` takes the AG-UI handler so the test can inject a fake; Phase 24 owns the real route-exclusion (WEB-01).
- **Fake AG-UI handler for a genuine precedence assertion** — the test records every path the AG-UI handler receives. If `/` won precedence over a registered AG-UI prefix, the AG-UI handler would never fire and the hit-count assertion would fail — proving Go 1.22 longest-pattern-wins, not merely that both handlers are wired.
- **`web-e2e` mirrors the AG-UI-live-smoke stack env** — a dummy non-empty `OPENROUTER_API_KEY` lets `config.LoadServe` boot (it fail-fasts on an empty key); the agent loop is never exercised, only the static `/` render is.
- **playwright.config.ts env allow-list** — an explicit `SERVE_ENV_KEYS` list forwards only the DB/serve vars from `process.env`, dropping any unset key (not passing `"undefined"`).

## Deviations from Plan

### Adapted (path reconciliation, not auto-fixes)

**1. [Plan path correction] web/dist → internal/webui/dist in the freshness gate + Dockerfile**
- **Found during:** Tasks 2 + 3 (carried over from 23-01 Dev#1 / 23-02)
- **Issue:** The plan text says `git diff -- web/dist/` and `COPY --from=webbuild /web/dist ./web/dist`. `web/dist` does not exist — vite `build.outDir` is `../internal/webui/dist` (Go embed is package-relative).
- **Fix:** Freshness script diffs `internal/webui/dist/` from the repo root; Dockerfile copies `/internal/webui/dist`. This was anticipated by the orchestrator brief; it is the only path the `//go:embed` directive can read.
- **Files:** scripts/web_dist_freshness.sh, docker/aura/Dockerfile
- **Verification:** `bash -n` clean + grep internal/webui/dist; Dockerfile grep matches
- **Committed in:** 4dd172c8 (script), 776c6163 (Dockerfile)

No Rule-1/2/3 auto-fixes were needed — the existing seams (`webui.Handler()`, `aguiServer.Mux()`, `chat.close()`) were sufficient and the implementation matched the planned shape exactly aside from the dist-path reconciliation.

## Known Stubs

None new. The `internal/webui` handler still serves only the static placeholder shell (the Phase-23 boundary); the real SPA-fallback/route-exclusion is the Phase-24 (WEB-01) deliverable, as designed. The empty AppShell panels (23-02 Known Stubs) are unchanged intentional placeholders.

## CI Reconciliation — flagged (true proof is the first Linux CI run)

Two gates get their **true** proof only on the first Linux CI run, not on this Windows box:

- **`web-dist-freshness`** will likely **RED on its first real run**: the committed `internal/webui/dist` was built on **Windows Node 22** (23-02), and Linux Node 24 bundler output is unlikely to be byte-identical (23-02's A-DIST-REPRO flag). This is the EXPECTED byte-canonical reconciliation — the operator recommits the CI-built (Linux Node 24) dist. The gate was created correctly (`bash -n` clean, correct `internal/webui/dist` diff target); **no Windows rebuild was run to "fix" a diff** (that would not match Linux output anyway).
- **`web-e2e`** (Playwright booting `aura serve` over docker-compose Postgres) can only truly pass in the CI job with the stack up. Locally, the deterministic equivalent is the Go `serve_webui_test.go` httptest (GREEN), which proves the same `/` → embedded-shell mount without a browser or DB.

## Threat Surface

No new threat surface beyond the plan's `<threat_model>`. The serve mount is additive on the existing hardcoded-loopback bind (T-23-06 accept, existing posture); the freshness gate + Dockerfile `COPY --from` implement the T-23-01 tamper-evidence mitigation; `npm ci` (not `npm install`) in CI + Docker is the T-23-SC supply-chain control. The HIGH-severity web-exposure controls (auth, non-loopback guard, SPA route-exclusion) remain deferred to Phase 24 as flagged (T-23-DEFER-AUTH).

## User Setup Required

None — no external service configuration. The CI jobs use a CI-placeholder Postgres + a dummy non-network `OPENROUTER_API_KEY`.

## Next Phase Readiness

- **24-web-foundation** inherits the proven embed→serve→render path: `newServeHandler` is the seam it extends for the real SPA-fallback (index.html catch-all for client routes) + API/health route-exclusion + web-auth + non-loopback boot guard (WEB-01/02/03). The four web CI jobs + freshness gate + webbuild Docker stage are the durable pipeline it builds on.
- **Operator action (Phase 23 close):** on the first Linux CI run, recommit the Linux-Node-24-built `internal/webui/dist` if `web-dist-freshness` reds (the expected byte-canonical reconciliation).

## Self-Check: PASSED

All 3 created files present on disk (`cmd/aura/serve_webui.go`, `cmd/aura/serve_webui_test.go`, `scripts/web_dist_freshness.sh`); all 5 modified files carry the changes; all three task commits (`4dcba3fd`, `4dd172c8`, `776c6163`) present in git history.

---
*Phase: 23-frontend-infrastructure-industrial-foundation*
*Completed: 2026-06-16*
