---
phase: 07-web-tools
plan: 04
subsystem: web-tools
tags: [web_search, web_fetch, deferred-tool, sanitized-error, spillover, aura-web-cli, ssrf-smoke, web_integration, goleak, mutation, gate-3]

# Dependency graph
requires:
  - phase: 07-web-tools (plan 01)
    provides: SearXNG compose service (no host port) + settings.yml (formats html,json) + AURA_WEB_* root config + internal/web goleak main_test.go
  - phase: 07-web-tools (plan 02)
    provides: hardenedTransport + guard.validateAndPin + dnsPin + WebError/internalError/sanitize + D-38 enum + ssrf.go classifier
  - phase: 07-web-tools (plan 03)
    provides: web.NewClient(cfg) + web.Search(ctx, SearchParams) + web.Fetch(ctx, convID, url) + Result/Page/SearchParams + ExtractMarkdown
  - phase: 04-conversation (tools)
    provides: tools.NewResult spillover + WithToolCallContext + ReadToolOutput + Spec.Deferred + manifest alpha-sort
provides:
  - tools.WebSearch / tools.WebFetch — Deferred:true adapters over injectable searchEngine/fetchEngine seams (*web.Client satisfies both)
  - sanitized inline error mapping (web.AsWebError → {error,reason,message} via NewResult, never a Go error / never an IP/host/header)
  - web_fetch large content_md spillover via NewResult (D-21, zero new code)
  - cmd/aura web (doctor + tool web_search/web_fetch) hand-parsed CLI, sysexits 70/71/64, no public fallback
  - scripts/ssrf_smoke.sh (SC#3, 4/4 blocked_url grep-clean) + scripts/web_search_smoke.sh (D-43)
  - internal/web web_integration tier (TestSearch_Live/TestFetch_Live/TestDNSRebind) + envOrSkip t.Fatal-under-$CI + own goleak TestMain
  - CI web-integration-test job (SSRF smoke + live tier in a Go container on the aura-web network)
affects: [phase-close CAP-05 sign-off (pending the Task 4 human-verify), agent loop tool dispatch (web_search/web_fetch now registered)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Tool adapters depend on a small injectable engine interface (searchEngine/fetchEngine), not the concrete *web.Client — mirrors Execute.Runner sandbox.Runner; makes the adapter unit-testable with a double and keeps the security boundary in internal/web"
    - "A *web.WebError is the MODEL channel (inline {error,reason,message} via NewResult so the model self-corrects); only a non-web error propagates as a Go error to the loop"
    - "web_fetch reads the conversation scope from the tool-call context (tc.sessionID), never from the model, so the engine's per-conversation DNS pin is keyed correctly"
    - "web_integration tier carries its own //go:build web_integration goleak TestMain; the unit main_test.go is //go:build !web_integration so exactly one TestMain compiles per build; ALL unit test files in internal/web are now !web_integration-tagged so the integration build is self-contained"
    - "SearXNG client (searxGet) needs DisableKeepAlives:true like the hardened transport — a pooled persistConn leaks under the integration goleak TestMain otherwise"

key-files:
  created:
    - internal/agent/tools/web_search.go
    - internal/agent/tools/web_fetch.go
    - internal/agent/tools/web_search_test.go
    - internal/agent/tools/web_fetch_test.go
    - cmd/aura/web.go
    - internal/web/searxng_integration_test.go
    - internal/web/dnspin_integration_test.go
    - scripts/ssrf_smoke.sh
    - scripts/web_search_smoke.sh
  modified:
    - cmd/aura/main.go (import web, case "web", usage, buildRegistry registers WebSearch+WebFetch via one web.NewClient)
    - internal/web/searxng.go (Gate-3 goleak fix: DisableKeepAlives on the search client)
    - internal/web/ssrf_test.go (Gate-3 mutation hardening: 6 more mutants killed)
    - internal/web/{cache,fetcher,html,searxng}_test.go (added //go:build !web_integration)
    - .planning/phases/07-web-tools/07-VALIDATION.md (wave_0_complete true + rows green)
    - docs/aura-quality-snapshot.md (web row + Phase 7 detail table with Gate-3 numbers)
    - .github/workflows/ci.yml (web-integration-test job)

decisions:
  - "Tool adapters use injectable engine interfaces (searchEngine/fetchEngine) rather than the concrete *web.Client, so error-mapping + spillover are unit-testable without a live backend or the unexported web test seams"
  - "cmd/aura/web.go was landed in Task 1's commit (not Task 2's) because cmd/aura/main.go references runWeb — atomic build-green required them together; the smoke scripts + integration tier stayed in Task 2"
  - "Gate-3 surfaced two fixes committed as a fix(07-04) deviation: the searxng goleak (Rule 1 bug) and ssrf.go mutation hardening (61.1% -> 94.4%)"

metrics:
  duration_minutes: ~110 (+ gap-closure session)
  completed: 2026-06-02
  tasks_completed: 3 of 4 (Task 4 is the blocking-human checkpoint; the gap-closure bug+quality fixes are in, Gate-3 re-run green, CAP-05 still pending the human sign-off)
  commits: 5 + 5 gap-closure (f5764ecc PRD-amend, 1e424487 Layer-1 rename, 997b8693 + 00d76c59 Layer-2 cleanup, 8f25cc13 coverage tests)
---

# Phase 07 Plan 04: Web Tools LLM-Facing Layer + Operator/CI Surface Summary

The thin LLM-facing layer for Phase 7: two `Deferred:true` tool adapters (`web_search`, `web_fetch`) over the shared SSRF-hardened `web.Client`, the `aura web` operator CLI (doctor + tool verbs), the live `web_integration` test tier, and the SSRF/search smoke scripts — wired into the agent registry and CI. Tasks 1–3 are complete and committed; Task 4 (live SC#1/SC#2 acceptance + Gate-3 numbers) is the blocking-human checkpoint and is returned to the orchestrator with all gathered evidence — CAP-05 is NOT marked complete.

## What was built

- **`web_search.go` / `web_fetch.go`** — `Deferred:true` adapters holding an injectable `searchEngine` / `fetchEngine` (mirrors `Execute.Runner sandbox.Runner`; `*web.Client` satisfies both). They marshal D-09 controls only (no `engines`/`safesearch`/`pageno`/`format` — D-10), delegate to `web.Client`, map a `*web.WebError` to inline `{error,reason,message}` via `NewResult` (D-41), and on success route through `NewResult` so a large `content_md` spills to the sidecar (D-21).
- **Registry wiring** — `buildRegistry` registers both via one `web.NewClient(config.LoadDB())`; manifest auto-sorts (`web_fetch` < `web_search`); `case "web"` + usage + doc-block updated.
- **`cmd/aura/web.go`** — hand-parsed `aura web doctor` and `aura web tool web_search|web_fetch '<json>'`; `config.LoadDB()` (no `OPENROUTER_API_KEY`); sysexits 70/71/64; never a public-instance fallback; `tool` (singular) does not collide with `tools` (plural).
- **`scripts/ssrf_smoke.sh`** (SC#3, verified 4/4 blocked grep-clean) + **`scripts/web_search_smoke.sh`** (D-43, fail-loud under `$CI`).
- **`internal/web/searxng_integration_test.go` + `dnspin_integration_test.go`** — `//go:build web_integration`, own goleak TestMain, `envOrSkip` that `t.Fatal`s under `$CI` (no-skip-as-green).
- **CI `web-integration-test` job** — SSRF smoke (CI armed) + live tier from a Go container joined to the `aura_aura-web` network (SearXNG keeps NO host port).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Tagged the untagged unit test files `//go:build !web_integration`**
- **Found during:** Task 2 (`go vet -tags web_integration ./internal/web/`)
- **Issue:** `cache_test.go`, `fetcher_test.go`, `html_test.go`, `searxng_test.go` lacked a build tag while their shared helpers (`stubResolver`, `testClient`) lived in `ssrf_test.go` (`!web_integration`) — so the `web_integration` build failed to compile (`undefined: stubResolver`).
- **Fix:** Added `//go:build !web_integration` to the four files so the integration build is self-contained.
- **Files:** internal/web/{cache,fetcher,html,searxng}_test.go
- **Commit:** 7aa4d020

**2. [Rule 1 - Bug] SearXNG search client leaked a persistConn (goleak)**
- **Found during:** Task 4 live `TestSearch_Live`
- **Issue:** `searxGet` used a bare `&http.Client{}`; after a live search the pooled `persistConn` readLoop/writeLoop outlived the request and tripped the new `web_integration` goleak TestMain.
- **Fix:** `&http.Client{Transport: &http.Transport{DisableKeepAlives: true}}` (docker.go / transport.go idiom). `TestSearch_Live` now passes goleak-clean.
- **Files:** internal/web/searxng.go
- **Commit:** f86cbf13

**3. [Rule 1 - Bug / Gate-3] ssrf.go mutation score below the 70% floor (61.1%)**
- **Found during:** Task 4 `go-mutesting ./internal/web/ssrf.go`
- **Issue:** 7 mutants survived — `this_network` 0/8, the IPv4-mapped-IPv6 distinct predicates, `validateAndPin`'s pin-WRITE + pin-REUSE return, and the resolver-error/empty-record fail-closed path were not asserted by `ssrf_test.go`.
- **Fix:** Added table rows + `TestValidateAndPin_PinReuse` (counting resolver proves no second resolve) + `TestValidateAndPin_ResolveFailsClosed`. Score 61.1% → **94.4%** (17/18). The lone survivor is the unreachable `metadataV6Pfx` branch (`fd00:ec2::/32` is caught by `IsPrivate` first — a dead branch flagged for a behavior-neutral Wave-2 cleanup, not touched here per scope).
- **Files:** internal/web/ssrf_test.go
- **Commit:** f86cbf13

## Gap closure (2026-06-02 — the Task 4 checkpoint bug + quality gap, fixed)

The live Gate-3 found a real BLOCKING bug + a quality gap; both are now fixed (4 commits, see below). CAP-05 is still NOT self-marked complete — that is the human's call.

**Bug (fixed): SC#2 raw-HTML-cap.** `web.Fetch` applied a 24000-byte ceiling (the mis-named `WebResponseCapBytes`/`AURA_WEB_RESPONSE_CAP_BYTES`) to the RAW HTML body in `gateAndRead`, so any real content page (164KB Wikipedia) returned `response_too_large` BEFORE readability extraction. Confirmed the field is read in EXACTLY ONE place (the raw-body LimitReader) — it never governed the LLM-facing payload (that is `tools.NewResult`'s preview cap, `AURA_CONTEXT_PREVIEW_CAP_BYTES`). **Layer 1:** renamed the field → `WebFetchMaxBodyBytes` / `AURA_WEB_FETCH_MAX_BODY_BYTES`, default 24000 → 5_000_000 (raw-body DoS ceiling), PRD env-catalog amended first.

**Quality gap (fixed): noisy Wikipedia markdown.** The extracted markdown carried inline `[\[n\]](#cite_note)` citation anchors, the full references/notes tail, and a "From Wikipedia" boilerplate line. **Layer 2:** `html.go` post-processes the markdown — strips citation anchors, truncates at the References/Notes/Citations/Bibliography heading OR the first zero-padded reflist marker (`01.`/`02.`, the headingless Wikipedia case the live run exposed), strips the boilerplate + the `<!--THE END-->` converter artifact, filters fragment-only links, and re-evaluates `low_content` post-cleanup. All patterns are STRUCTURAL markdown forms (not natural-language prose).

## Gate-3 evidence (final, re-run live against the up stack)

- **SC#2 live web_fetch (en.wikipedia.org/wiki/Knowledge_graph): PASS** — content_md **36070 B → 16429 B**; no `#cite_note`/`#cite_ref`, no "From Wikipedia", no references tail, no fragment-only links.
- **SC#1 TestSearch_Live: PASS ~1.01s** (advisory, under the 2s budget; p95 is env-tunable per project policy, not a hard gate).
- **SC#3 ssrf_smoke: 4/4 blocked_url, grep-clean** ✅
- **SC#4 TestDNSRebind: PASS** (DNS pin reuse proven; the live fetch path now executes — TestFetch_Live also PASS).
- **ssrf.go mutation: 94.4%** (17/18; ssrf.go untouched; lone survivor = unreachable metadataV6Pfx dead branch) — ≥70% ✅
- **Owned-surface coverage gate (`internal/*`, db+neo4j tags): 87.4% — PASS** (the official Gate-3 floor, ≥85%).
- internal/web standalone: **91.0% unit / 91.5% combined (unit + web_integration) — PASS ≥85%** (was 80.5% / 82.0%). Focused test pass (commit 82557f69) added disk-cache round-trip (getDiskLocked/setDiskLocked/newCache persistent path), both `Error()` methods, fetch cache-HIT (1 origin hit per 2 fetches, D-31), and search validHostname/domainAllowed/buildQuery validation tables. Deliberately left uncovered (no asilo nido): convertNode A2 fallback (needs a node ConvertNode fails on — contrived), control deep dial-guard defensive arms (covered by TestTransport_ControlRecheck; only unreachable arms remain), newCache read-only-home MkdirAll-fail fallback, fetchFromCache cache-corruption unmarshal branch.
- **golangci-lint: 0 issues** ✅
- **aura web doctor (live stack): reachable: yes, JSON round-trip OK, status OK** ✅

## Threat Flags

None — no new trust boundary beyond the plan's `<threat_model>`. The adapters consume the existing sanitized `WebError`; the CLI adds no public-fallback path (D-05/D-44 held).

## Self-Check: PASSED

All created files exist on disk; the original 4 task/deviation commits (2c4062bd, 7aa4d020, e1e7bc9d, f86cbf13) plus the 5 gap-closure commits (f5764ecc, 1e424487, 997b8693, 00d76c59, 8f25cc13) are in the git history. The bug + quality gap are fixed, the full Gate-3 was re-run live (SC#1-4 green, official coverage gate 87.4%, lint 0, ssrf mutation 94.4%, doctor OK). Task 4 remains the blocking-human checkpoint and is intentionally NOT self-approved — CAP-05 is not marked complete.
