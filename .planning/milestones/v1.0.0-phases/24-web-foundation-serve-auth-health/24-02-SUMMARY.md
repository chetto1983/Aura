---
phase: 24-web-foundation-serve-auth-health
plan: 02
subsystem: web-foundation
tags: [web, spa-fallback, embed, serve, WEB-01]
requires:
  - "internal/webui.Handler() + Sub() (Phase-23 static embed host)"
  - "cmd/aura/serve_webui.go newServeHandler + aguiRoutePrefixes (Phase-23 mux wiring)"
  - "cmd/aura/integrations_proxy.go integrationsRoutePrefix = /api/integrations/ (already mounted)"
provides:
  - "internal/webui.Handler(apiPrefixes []string) — SPA-fallback handler (client route -> index.html; excluded prefix -> 404)"
  - "internal/webui.spaFallback — the fallback http.HandlerFunc (leaf-level, stdlib-only)"
  - "cmd/aura.fallbackExcludedPrefixes() — single-source exclusion set incl. the /api/ carve-out"
affects:
  - "cmd/aura/serve_webui.go (Plan 24-03 also edits this — changes kept self-contained)"
  - "cmd/aura/serve.go bootServe (unchanged here; Plan 24-01 guard preserved)"
tech-stack:
  added: []
  patterns:
    - "SPA-fallback over embed.FS: excluded-prefix -> http.NotFound; '/'+unknown route -> http.ServeFileFS(index.html); real asset -> http.FileServerFS"
    - "single-source exclusion list passed caller->package (no second hard-coded list to drift)"
    - "http.ServeFileFS for the shell to avoid the http.FileServerFS index.html->'./' canonical-redirect loop"
key-files:
  created: []
  modified:
    - internal/webui/embed.go
    - internal/webui/embed_test.go
    - internal/webui/doc.go
    - cmd/aura/serve_webui.go
    - cmd/aura/serve_webui_test.go
decisions:
  - "Exclusion set is single-sourced in cmd/aura/fallbackExcludedPrefixes() and passed into webui.Handler — internal/webui never hard-codes a second list (T-24-08 anti-drift)."
  - "/api/ is an EXCLUSION prefix ONLY, never a mux.Handle registration — a second /api/ mount would shadow the already-registered /api/integrations/ subtree (T-24-07)."
  - "The exclusion uses the broadened /agent/ namespace (not the exact /agent/run mux route) so a typo like /agent/typo 404s as backend rather than leaking the SPA shell (Task-1 behavior contract)."
  - "The shell is served via http.ServeFileFS (not by mutating r.URL.Path to /index.html through http.FileServerFS) to avoid the stdlib index.html->'./' redirect loop discovered during GREEN."
metrics:
  duration: ~30min
  completed: 2026-06-16
  tasks: 2
  files: 5
---

# Phase 24 Plan 02: Web Foundation — WEB-01 SPA-Fallback Host Summary

Turned the Phase-23 static placeholder into the real single-binary SPA host: `internal/webui.Handler` now takes a caller-supplied exclusion prefix set and serves `index.html` for unknown client routes (React Router deep links resolve) while returning a real 404 for any excluded API/agent/health prefix — so the SPA shell never leaks to an API client (WEB-01 / SC1).

## What Was Built

- **`internal/webui.Handler(apiPrefixes []string) (http.Handler, error)` + `spaFallback`** (`internal/webui/embed.go`) — `Handler` now accepts the exclusion set from the caller and returns an `http.HandlerFunc` (`spaFallback`) built over `fs.Sub(distFS, "dist")`. Resolution order: trim the leading `/`; if the path matches any excluded (normalized, no-leading-slash) prefix → `http.NotFound` (real 404, never the shell); else if the path is empty or has no embedded regular file → serve `index.html` via `http.ServeFileFS` (deep client-route → SPA shell); else delegate to `http.FileServerFS(sub)` so MIME + path-cleaning stay stdlib-driven. `normalizePrefixes` trims caller prefixes so `/agent/run` and `agent/run` compare identically; `statMissing` treats a missing file OR a directory as a client route. All resolution is against the embedded `fs.FS` — never the host filesystem — so `../` traversal cannot escape the embed (T-24-06). Imports stay `embed`/`io/fs`/`net/http`/`strings` only (leaf invariant).
- **`cmd/aura.fallbackExcludedPrefixes()` + wired `newServeHandler`** (`cmd/aura/serve_webui.go`) — a single-source exclusion set combining the AG-UI namespaces (`/healthz`, `/readyz`, `/debug/vars`, `/metrics`, the broadened `/agent/`, `/threads/`), the `integrationsRoutePrefix` (`/api/integrations/`), and the forward-compat `/api/` carve-out — passed into `webui.Handler(...)`. The mux registrations are unchanged (the AG-UI prefixes loop + `integrationsRoutePrefix` + `mux.Handle("/", static)`); critically `/api/` is **NOT** registered on the mux (it lives only in the fallback exclusion), so it cannot collide with the already-mounted `/api/integrations/` subtree (T-24-07).
- **Flipped + extended tests** — `internal/webui/embed_test.go` adds `TestSPAFallback` (unknown client route → index.html 200; excluded prefixes `/api/nope`, `/agent/typo`, `/threads/...`, `/readyz`, `/metrics`, `/debug/vars`, `/api/integrations/x` → 404 with no `<div id="root"` marker; real asset → 200 JS; missing asset under a client path → shell). `TestHandler` was updated for the new signature and keeps the theme-before-paint + brand assertions. `cmd/aura/serve_webui_test.go` flips the Phase-23 "GET bogus asset → 404 (no SPA-fallback)" subtest to assert an unknown client route → index.html (200, AG-UI handler NOT hit), and adds `/api/nope`+`/agent/typo` → real 404 and `/api/integrations/<unknown>` → integrations-proxy 404 (its own `unknown integration` body) precedence cases.
- **Deep refactor on touch** — rewrote the stale "Phase-23 static placeholder only … DO NOT add fallback logic here" comments in `embed.go` and `doc.go`, and the "Phase-23 scope … no SPA catch-all" header in `serve_webui.go`. They now describe the shipped SPA-fallback, the single-source exclusion contract, and the `/api/` carve-out rationale (WEB-01 is the sanctioned supersession).

## Verification

- `go vet ./...` — clean. `go build ./...` — succeeds.
- `go test ./internal/webui/ ./cmd/aura/` — green.
- `go test -race ./internal/webui/` + `go test -race ./cmd/aura/ -run TestServeWebui` — clean.
- `bash scripts/agui_boundary_check.sh` — exits 0 (internal/webui stays leaf; agent closure free of agui).
- `go test ./internal/webui/ -cover` — **95.2%** (above the 85% owned-surface floor); `spaFallback` fully unit-covered.
- Behavior cross-check (encoded in tests, equivalent to the optional curl sanity): `/some/route` → 200 text/html with the SPA shell; `/api/nope` → 404 with a non-HTML body containing no `<div id="root"` marker.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] index.html canonical-redirect loop in the fallback**
- **Found during:** Task 1 GREEN phase.
- **Issue:** The RESEARCH §Pattern 1 reference body sets `p = "index.html"` then `r.URL.Path = "/index.html"` and delegates to `http.FileServerFS`. `http.FileServerFS` canonicalizes a request for `/index.html` with a 301 redirect to `./`, which the catch-all then re-resolves — producing an HTTP redirect loop (`stopped after 10 redirects`) for every fallback path including `/`.
- **Fix:** Serve the shell directly with `http.ServeFileFS(w, r, sub, "index.html")` (no canonical redirect) and only delegate real assets to `http.FileServerFS`. Added `statMissing` to centralize the asset-vs-route decision.
- **Files modified:** `internal/webui/embed.go` (commit 74a8d0bf).
- **Commit:** 74a8d0bf.

**2. [Rule 1 - Bug] exclusion set must broaden /agent/run → /agent/ to 404 a typo**
- **Found during:** Task 1/2.
- **Issue:** `aguiRoutePrefixes` carries the exact `/agent/run`, but the Task-1 behavior contract requires `/agent/typo` → 404. Using the exact route as the exclusion prefix let `/agent/typo` fall through to the SPA shell (200) — an SC1 leak.
- **Fix:** `fallbackExcludedPrefixes()` excludes the broadened `/agent/` namespace (documented why it differs from the exact `/agent/run` mux registration). The mux registration itself is unchanged.
- **Files modified:** `cmd/aura/serve_webui.go`, test fixture in `internal/webui/embed_test.go` (commits 74a8d0bf, f0c8fba0).
- **Commit:** f0c8fba0.

> Note (process, not a deviation): the repo-wide `lefthook` pre-commit `go vet` gate cannot pass while a TDD signature change (Task 1) leaves the caller (Task 2) un-wired. Rather than `--no-verify` (forbidden), Task 2's caller fix was completed in the working tree before committing, so both per-task commits land with a fully-compiling tree and a green hook. The two commits remain atomic per task (Task-1 = the three `internal/webui` files; Task-2 = the two `cmd/aura` files).

## TDD Gate Compliance

Both tasks were `tdd="true"`.
- **Task 1:** RED — `TestSPAFallback`/`TestHandler` failed to compile against the old `Handler()` signature (`too many arguments`); GREEN — the new `Handler([]string)` + `spaFallback` made them pass (after the redirect-loop fix above); committed at 74a8d0bf.
- **Task 2:** RED — `TestServeWebui` failed to compile (`not enough arguments in call to webui.Handler`); GREEN — `fallbackExcludedPrefixes()` + the wired `newServeHandler` made the flipped + added cases pass; committed at f0c8fba0.

Gate sequence in git log: `feat(...)` for each task with its test changes in the same commit (the tests and the implementation that satisfies them are one feature per task). No test was massaged to pass — the `serve_webui_test.go` flip is a sanctioned rewrite because the WEB-01 contract genuinely inverted (justified in the commit body).

## Known Stubs

None. The SPA-fallback is fully implemented end-to-end and unit-covered at 95.2%. The web auth boundary (WEB-02 boot guard landed in Plan 24-01; WEB-03 login/`RequireAuth`) and the production React dist rebuild are intentionally separate plans (24-03 / 24-04) per the phase plan — not stubs in this code.

## Threat Surface Coverage

All of this plan's `<threat_model>` mitigations are in place and test-asserted:
- **T-24-05** (shell leaked for a real API 404): `spaFallback` returns `http.NotFound` for every excluded prefix; asserted by `/api/nope`+`/agent/typo` → 404 with no `<div id="root"` marker (both unit + cmd tests).
- **T-24-06** (path traversal): resolution is `fs.Stat` / `http.FileServerFS` / `http.ServeFileFS` over the embedded `fs.FS`, never `os.Open` on the host FS.
- **T-24-07** (`/api/` re-registration shadowing `/api/integrations/`): `/api/` is exclusion-only, never `mux.Handle("/api/", ...)`; the integrations-precedence test confirms `/api/integrations/<unknown>` still reaches the proxy.
- **T-24-08** (exclusion drift): single-sourced via `fallbackExcludedPrefixes()` passed into `webui.Handler`; no second hard-coded list in `internal/webui`.
- **T-24-SC** (package installs): zero new packages — stdlib `embed`/`io/fs`/`net/http`/`strings` only.

No new security surface beyond the plan's register.

## Self-Check: PASSED

- FOUND: internal/webui/embed.go (spaFallback + Handler([]string) present; no internal/* import — boundary check green)
- FOUND: internal/webui/embed_test.go (TestSPAFallback present)
- FOUND: internal/webui/doc.go (rewritten leaf-fallback doc)
- FOUND: cmd/aura/serve_webui.go (fallbackExcludedPrefixes + webui.Handler(...) wired; no mux.Handle("/api/", ...))
- FOUND: cmd/aura/serve_webui_test.go (flipped client-route case + /api/nope + /agent/typo + integrations-precedence subtests)
- FOUND commit: 74a8d0bf (Task 1 — SPA-fallback handler in internal/webui)
- FOUND commit: f0c8fba0 (Task 2 — single-source exclusion + /api/ carve-out)
