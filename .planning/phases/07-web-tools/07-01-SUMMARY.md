---
phase: 07-web-tools
plan: 01
subsystem: infra
tags: [searxng, web-search, web-fetch, readability, html-to-markdown, ssrf, compose, config, goleak]

# Dependency graph
requires:
  - phase: 05-sandbox-2a-stateless
    provides: fail-closed security posture + remote-service HTTP-client + goleak TestMain idioms reused as analogs
  - phase: 06-kv-cache-builder
    provides: ToolResult spillover + read_tool_output paging path that web_fetch large markdown will reuse
provides:
  - SearXNG Compose service (no host port, :ro settings, JSON format enabled, live-verified)
  - codeberg.org/readeck/go-readability/v2 + github.com/JohannesKaufmann/html-to-markdown/v2 deps (D-20)
  - AURA_WEB_* + SEARXNG_URL root config fields, fail-closed at call time (not boot-fatal)
  - internal/web package skeleton (doc.go) + goleak main_test.go harness
affects: [07-02 web engine wave (searxng/ssrf/dnspin/transport/fetcher/html), 07-03 tool adapters, 07-04 CLI + integration]

# Tech tracking
tech-stack:
  added:
    - codeberg.org/readeck/go-readability/v2 v2.1.1
    - github.com/JohannesKaufmann/html-to-markdown/v2 v2.5.1
    - golang.org/x/net (promoted to direct — shared *html.Node bridge type)
    - searxng/searxng:2026.5.31-7159b8aed (Compose image)
  patterns:
    - "Sandbox-precedent root config fields (envDefault/envIntDefault, never fatal) extended with envBoolDefault"
    - "Fail-CLOSED-at-call-time for optional subsystem URL (empty SEARXNG_URL loads fine, errors at search time)"
    - "Internal-only Compose service: absence of ports: key IS the access control (shared egress network, no host bind)"
    - "use_default_settings override-only settings.yml (small, survives image upgrades)"

key-files:
  created:
    - internal/web/doc.go
    - internal/web/main_test.go
    - searxng/settings.yml
  modified:
    - go.mod
    - go.sum
    - compose.yaml
    - internal/config/config.go
    - internal/config/config_test.go
    - .env.example

key-decisions:
  - "Pinned searxng/searxng:2026.5.31-7159b8aed after verifying its Granian entrypoint binds :8080 (OQ4/A3 resolved — D-02's 8080 is correct, the file's 8888 is overridden by GRANIAN_PORT)"
  - "settings.yml uses use_default_settings:true + overrides only formats/limiter/secret — small + upgrade-safe rather than copying the 67KB default"
  - "golang.org/x/net promoted to a direct dep via a compile-time anchor in doc.go so the two D-20 libs survive go mod tidy before the html.go wave consumes them"
  - "Empty SEARXNG_URL kept out of any fail-fast path (D-05/D-06): regression-locked by TestWebDefaults_AppliedAndNotFatal so aura db migrate never breaks"

patterns-established:
  - "envBoolDefault: parse-fail -> fallback, never boot-fatal (mirrors envIntDefault) for opt-in toggles"
  - "Web package goleak harness behind //go:build !web_integration, mirroring internal/sandbox/docker_test.go's tier split"

requirements-completed: [CAP-05]

# Metrics
duration: ~35min
completed: 2026-06-02
---

# Phase 7 Plan 01: Web Tools Infra + Config Substrate Summary

**Stood up the Phase 7 substrate — a host-port-less SearXNG Compose service with a live-verified JSON-enabled read-only settings.yml, the two D-20 readability/markdown Go deps, the AURA_WEB_* + SEARXNG_URL root config (fail-closed at call time), and a goleak-armed internal/web skeleton — so the later engine/tool/CLI waves have somewhere to run and read from.**

## Performance

- **Duration:** ~35 min (continuation after an approved supply-chain checkpoint)
- **Tasks:** 3/3 (Task 1 was the human-verify checkpoint, approved before this session)
- **Files modified:** 9 (3 created, 6 modified)
- **Commits:** 3 task commits + this docs commit

## Accomplishments

### Task 1 — readability/markdown deps (commit 2119fd71)
- `go get codeberg.org/readeck/go-readability/v2@v2.1.1` + `github.com/JohannesKaufmann/html-to-markdown/v2@v2.5.1`, re-confirmed both as the latest published versions before install (the package-legitimacy checkpoint was approved: the [SLOP] verdict on html-to-markdown/v2 is a documented false positive).
- `golang.org/x/net` promoted to a direct dep (the shared `*html.Node` bridge type between the two libs).
- `internal/web/doc.go` documents the shared-engine boundary and carries a compile-time anchor (`readability.FromReader` / `html.Node` / `htmltomarkdown.ConvertNode`) so the deps survive `go mod tidy` ahead of the html.go wave.
- No `AURA_WEB_FETCH_ALLOW_*` escape hatch added anywhere (D-30).

### Task 2 — SearXNG Compose service + settings.yml (commit 06414dfd)
- `searxng` service pinned to `searxng/searxng:2026.5.31-7159b8aed`, `container_name: aura-searxng`, `restart: unless-stopped`, `:ro` settings mount, `/healthz` healthcheck via the image's `wget`, attached to a new `aura-web` bridge network.
- **No `ports:` key** — the deliberate divergence from every other service (D-03, T-07-01): SearXNG egresses to the public internet but is unreachable from the host.
- `searxng/settings.yml` uses `use_default_settings:true` and enables `search.formats: [html, json]` (D-04, RESEARCH Pitfall 5), with `limiter:false` / `public_instance:false` for an internal single-consumer instance.
- Live-verified through `docker compose up searxng`: the mounted 1.8KB file is authoritative, `/healthz`=OK, and an in-network `POST format=json` returns real `{"query","results":[...]}` JSON.

### Task 3 — root config + goleak harness (commit f0a0b06e)
- Added `SearxngURL` (env `SEARXNG_URL`, empty default), `WebDNSPinTTLSec=60`, `WebResponseCapBytes=24000`, `WebCachePersistent=false`, `WebSearchTimeoutSec=20`, `WebFetchTimeoutSec=30`, `WebUserAgent="Aura/0.x web_fetch"` to the root `config.Config` (Sandbox precedent).
- New `envBoolDefault` helper (parse-fail → fallback, never fatal).
- Empty `SEARXNG_URL` is **not** boot-fatal — `LoadDB()`/`Load()` succeed, regression-locked by `TestWebDefaults_AppliedAndNotFatal` (D-05/D-06).
- `internal/web/main_test.go` adds `goleak.VerifyTestMain` behind `//go:build !web_integration`.
- `.env.example` documents the full catalog with per-line comments; `TestWebEnvOverrides` + `TestEnvBoolDefault` added.

## Verification

- `go build ./...` + `go vet ./...` green.
- `go test ./internal/web/...` (goleak TestMain, zero leaks) + `go test ./internal/config/...` green; `-race` clean on both (CGO_ENABLED=1).
- `golangci-lint run` on both touched packages: 0 issues.
- `docker compose config` validates; rendered `searxng` service has **no** `ports:` key; settings mount ends `:ro`; image is a concrete pin.
- Live SearXNG `format=json` round-trip returns results (Pitfall 5 disproved under the real compose mount).
- Repo-wide scan: no `AURA_WEB_FETCH_ALLOW_*` string in any `.go`/`.yaml`/`.example`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] go mod tidy pruned the two new deps (no importer yet)**
- **Found during:** Task 1
- **Issue:** `go mod tidy` removes modules nothing imports; the plan's Task 1 acceptance requires both module paths present in go.mod and `go build` to succeed with them, but no source imported them yet (the engine is a later wave).
- **Fix:** Pulled the `internal/web/doc.go` skeleton creation forward into Task 1 (it was originally listed under Task 3's actions) and gave it a minimal compile-time anchor referencing both libs + `golang.org/x/net/html`. This is the package's natural home and is the only correct way to anchor the deps. Task 3 then only added the goleak `main_test.go` to the package.
- **Files modified:** internal/web/doc.go (Task 1 instead of Task 3)
- **Commit:** 2119fd71

**2. [Rule 1 - Tooling] MSYS path mangling broke the manual docker-run validation (not the artifact)**
- **Found during:** Task 2 validation
- **Issue:** A manual `docker run -v "$(pwd)/searxng/...:..."` from Git Bash mangled the mount target to `\Program Files\Git\etc\searxng\settings.yml`, so the default 67KB settings stayed active and `format=json` returned 403 — a false alarm, not a config bug.
- **Fix:** Validated through `docker compose up searxng` instead (relative compose path is not subject to MSYS mangling). The mounted file then loaded correctly and JSON returned real results. The shipped compose service is correct; only my throwaway manual test command was wrong.
- **Files modified:** none (validation method only)

## Authentication Gates

None. (The Task 1 package-legitimacy checkpoint was the only gate; it was approved before this session.)

## Threat Model Compliance

- T-07-01 (info disclosure): searxng has no `ports:` key — mitigated, verified by `docker compose config` grep.
- T-07-02 (tampering): settings.yml mounted `:ro` — mitigated.
- T-07-03 (DoS on missing config): empty SEARXNG_URL fail-closed at call time, not boot — mitigated, regression-locked.
- T-07-SC (supply chain): module paths verified before install at the approved checkpoint — mitigated.

## Notes for Downstream Waves

- The `internal/web/doc.go` anchor (`extractionDeps` blanks) is a temporary scaffold — delete it once `html.go` imports `readability`/`htmltomarkdown` directly (noted inline in doc.go).
- SEARXNG_URL is intentionally empty by default; the engine wave (07-02/03) must emit `web_search_unavailable{searxng_not_configured}` when it is empty — do NOT reintroduce a localhost autodetect or public fallback (D-05).
- Compose default in-network URL for the running stack: `SEARXNG_URL=http://searxng:8080/search` (the Aura process must join the `aura-web` network to reach it).

## Self-Check: PASSED

- internal/web/doc.go — FOUND
- internal/web/main_test.go — FOUND
- searxng/settings.yml — FOUND
- compose.yaml searxng service — FOUND (grep `searxng:`)
- commit 2119fd71 — FOUND
- commit 06414dfd — FOUND
- commit f0a0b06e — FOUND
