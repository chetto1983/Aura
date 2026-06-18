---
phase: 26-typed-display-protocol-router
plan: 03
subsystem: api
tags: [go, kv-cache, citations, ssrf, image-proxy, display-protocol, replay, trust-boundary]

# Dependency graph
requires:
  - phase: 26-typed-display-protocol-router (plan 01)
    provides: "display.Normalize / NormalizeWithRegistry + the URL-keyed source Registry + RenderSourceList + the Actions.Display slot + the aura.display CUSTOM translator branch"
  - phase: 06-kv-cache-builder
    provides: "prompt.Budget tail-inject append-to-copy + the cache_invariant_audit.sh messages[0] gate (aura cache-audit)"
  - phase: 07-web-tools
    provides: "web.Client SSRF guard (validateAndPin) + hardenedTransport + web.Result/Page (the FetchImage + decode inputs)"
  - phase: 24-web-foundation-serve-auth-health
    provides: "the RequireAuth whole-origin gate every /api/ route inherits"
provides:
  - "prompt.Budget.Sources: the D-05 volatile numbered source list rides the tail-inject copy (messages[0] byte-identical)"
  - "display.NormalizeToolPreview: the SINGLE decode+normalize seam shared by the live agent loop and the replay snapshot projection (one normalizer for live + replay)"
  - "web.Client.FetchImage: SSRF-safe image fetch reusing validateAndPin + hardenedTransport"
  - "GET /api/image-proxy behind RequireAuth (agui Server.Mux + parent-mux delegation + SetImageProxy boot wiring)"
  - "projectDisplaySnapshot: the display-aware MESSAGES_SNAPSHOT (re-derived DisplayPayload per tool turn, D-06)"
affects: [26-04 per-type display cards (web_result <img> via /api/image-proxy), 26-05 source explorer + citations]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Tail-inject the volatile source list via prompt.Budget.Sources (append-to-copy), static citation convention in messages[0] — KV-cache invariant preserved (Pitfall 2)"
    - "One normalizer for live + replay: display.NormalizeToolPreview decodes the SAME persisted ResultPreview both paths consume, so replay == live by construction (Pitfall 4)"
    - "SSRF-safe egress reuse: FetchImage rides the existing hardenedTransport + validateAndPin (never a fresh http.Get) with an image content-type allowlist (svg excluded) + size cap + no-redirect-follow"
    - "Display-aware snapshot mirror structs: the SDK types.ToolCall has no display field, so projectDisplaySnapshot uses local structs that marshal byte-compatibly + an additive `display` key"

key-files:
  created:
    - internal/agent/llm_agent_construct.go
    - internal/agent/llm_agent_display.go
    - internal/agent/llm_agent_sources_test.go
    - internal/agent/prompt/budget_sources_test.go
    - internal/agent/prompt/cache_audit_sources_test.go
    - internal/agent/display/preview.go
    - internal/agent/display/preview_test.go
    - internal/web/fetcher_image.go
    - internal/web/fetcher_image_test.go
    - internal/agui/image_proxy.go
    - internal/agui/image_proxy_test.go
    - internal/agui/server_display.go
    - internal/agui/server_display_test.go
  modified:
    - internal/agent/prompt/builder.go
    - internal/agent/prompt.go
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_events.go
    - internal/agui/server.go
    - cmd/aura/serve.go
    - cmd/aura/serve_webui.go
    - cmd/aura/cache_audit.go
    - scripts/cache_invariant_audit.sh
    - scripts/fixtures/cache_invariant/turn-08.json

key-decisions:
  - "display.NormalizeToolPreview is the shared decode+normalize seam (NEW symbol): the live agent (deriveDisplay) and the replay projection (rederiveDisplays) both call it over the persisted ResultPreview, so live/replay parity is structural not asserted-after-the-fact"
  - "The live web tool-result Event now also sets Actions.Display (the same decode that feeds the source registry) so the live aura.display CUSTOM event the D-06 replay must MATCH actually exists"
  - "projectDisplaySnapshot uses local display-aware mirror structs (not the SDK events.Message/types.ToolCall) because neither SDK type has a `display` field; the JSON marshals byte-compatibly + the additive per-tool-call `display` key 26-02's toolCallsFromSnapshot already reads"
  - "FetchImage rejects a 3xx outright (no re-validate-and-follow) — stricter than web_fetch — because an image redirect could rebind to a private host between hops and the proxy has no reason to chase it"
  - "EXPECTED_REQUESTS 22->23: turn-08 converted to a web_search->cite turn driven by a deterministic network-free auditSearchEngine, so the cache gate proves the source list never poisons messages[0]"

patterns-established:
  - "Pattern: volatile per-turn citation data rides prompt.Budget.Sources tail-inject; the static convention sentence is invariant in messages[0]"
  - "Pattern: a source-bearing web tool-result decodes its persisted preview once via display.NormalizeToolPreview, feeding both the per-run registry (source list) and Actions.Display (live) / the snapshot (replay)"

requirements-completed: [DISP-01, DISP-03, DISP-05]

# Metrics
duration: ~34min
completed: 2026-06-18
---

# Phase 26 Plan 03: Backend Protocol Completion (source tail-inject, image-proxy, replay re-derive) Summary

**The three architecturally non-trivial backend wirings: the D-05 numbered source list now rides `prompt.Budget.Sources` (the KV-cache-safe append-to-copy tail-inject — `messages[0]` byte-identical, cache gate green) while a static citation convention lives in the system prompt; a D-09 SSRF-safe `/api/image-proxy` reuses the existing `validateAndPin` guard behind `RequireAuth`; and D-06 reopened-thread tool turns re-derive their `DisplayPayload` through the SAME `display.NormalizeToolPreview` seam the live loop uses, so replay matches live by construction.**

## Performance
- **Duration:** ~34 min
- **Started:** 2026-06-18T17:26:50Z
- **Completed:** 2026-06-18T18:00:27Z
- **Tasks:** 3
- **Files:** 23 (13 created + 10 modified)

## Accomplishments
- **Task 1 (DISP-01/05 source tail-inject):** `prompt.Budget` gains a `Sources` field + a `<sources>` `block()` line (mirroring `<current_time>`); `llm_agent` keeps a per-run `*display.Registry`, decodes each `web_search`/`web_fetch` result into it (also setting `Actions.Display` on the live tool-result Event), and threads `RenderSourceList()` into `Budget.Sources` so the volatile list rides the tail-inject copy — `messages[0]` stays byte-identical. A static "Cite your sources … emit `[n]`" sentence lives in `messages[0]`. The cache-invariant audit gains a `web_search`→cite fixture (turn-08, **23** requests) with a deterministic network-free engine; the gate is green.
- **Task 2 (DISP-05/D-09 image-proxy):** `web.Client.FetchImage` reuses the hardened transport + `validateAndPin` (NOT a fresh `http.Get`) with an image content-type allowlist (png/jpeg/webp/gif; **svg excluded**), an `io.LimitReader` size cap, and no redirect follow. `GET /api/image-proxy` mounts on the agui `Server.Mux` behind the `RequireAuth` whole-origin gate (parent serve mux delegates the route; `SetImageProxy(web.NewClient)` wired at daemon boot) with a strict `Content-Type` + `Cache-Control` + `nosniff`. Never an open relay.
- **Task 3 (D-06 re-derive):** `handleMessages` emits a display-aware `MESSAGES_SNAPSHOT`; `projectDisplaySnapshot` re-runs the SAME normalizer over each persisted tool-result turn (one shared per-thread registry → stable cross-turn RefIDs) and attaches the re-derived `DisplayPayload` to the matching assistant tool-call entry. An unrecognized tool produces no display (replay D-FALLBACK == live).

## Task Commits
1. **Task 1: tail-inject D-05 numbered source list via Budget.Sources** — `01f0fd4e` (feat)
2. **Task 2: SSRF-safe image-proxy (FetchImage + /api/image-proxy)** — `191bd859` (feat)
3. **Task 3: re-derive displays at projectMessages for live/replay parity (D-06)** — `be7f2fb5` (feat)

## Per-type preview-sufficiency audit (OQ2 / A4)
The in-scope source-bearing display types re-derive **losslessly** from the persisted `ResultPreview`:

| Display type | Source tool | Preview = full result? | Re-derives from preview? | Store-fallback needed? |
|---|---|---|---|---|
| `web_result` | `web_search` | YES — the adapter marshals `{"results":[…]}` as the whole result (`web_search.go:101`), persisted verbatim as `ResultPreview` | YES | No |
| `document` | `web_fetch` | YES — the adapter marshals the full `web.Page` (title/url/content_md/links) | YES | No |

The other display kinds (`code`, `local_artifact`, `table`, `chart`, `system_event`, `swarm_report`) are not wired to the live `Actions.Display` emit in this plan (the source-list + citation core is the scope of 26-03), so there is nothing to re-derive for them yet — they fall through to D-FALLBACK on both live and replay, identically. **No store-fallback is required for any in-scope type.**

## Image-proxy route + query-param contract (for 26-04 WebResultDisplay `<img>`)
- **Route:** `GET /api/image-proxy?url=<percent-encoded external image URL>` (behind `RequireAuth`).
- **Success:** `200` with the raw image bytes, `Content-Type` = the matched media type (one of `image/png|jpeg|webp|gif`), `Cache-Control: private, max-age=3600`, `X-Content-Type-Options: nosniff`.
- **Errors:** `400` missing `url`; `502` blocked/unfetchable (sanitized body, no IP/host leak); `503` proxy unwired.
- **Frontend (26-04):** `<img src="/api/image-proxy?url={encodeURIComponent(thumbnail)}" referrerpolicy="no-referrer" loading="lazy">`; add a CSP `img-src 'self'`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Split llm_agent.go to satisfy the 600-LOC cap (refactor-on-touch)**
- **Found during:** Task 1 (the source-registry field + Budget.Sources wiring pushed `llm_agent.go` to 614 LOC; the file-size pre-commit gate blocked the commit).
- **Fix:** Extracted `NewLlmAgent` + `resolveBreaker` + the lifecycle accessors (`Name`/`Description`/`OwnsBudget`/`SubAgents`/`FindAgent`) into a new `internal/agent/llm_agent_construct.go` (`llm_agent.go` 614→547 LOC). Per CLAUDE.md DEEP REFACTOR ON TOUCH.
- **Files modified:** internal/agent/llm_agent.go, internal/agent/llm_agent_construct.go
- **Commit:** 01f0fd4e

**2. [Rule 2 - Missing critical functionality] Live `Actions.Display` emit + parent-mux route + boot wiring**
- **Found during:** Tasks 1–3.
- **Issue:** (a) For D-06 replay to "match live", the live path must actually emit `Actions.Display` — 26-01 created the slot but nothing set it on the live tool-result Event. (b) Registering `/api/image-proxy` only on the agui `Server.Mux` makes it unreachable in production unless the parent serve mux delegates it and the daemon calls `SetImageProxy`.
- **Fix:** (a) `toolResultEvent` now sets `Actions.Display` from the decoded web result (the same decode that feeds the source registry). (b) Added `imageProxyRoute` to the parent serve mux (delegating to aguiHandler) + `SetImageProxy(web.NewClient(chat.cfg))` at daemon boot.
- **Files modified:** internal/agent/llm_agent_events.go, cmd/aura/serve.go, cmd/aura/serve_webui.go
- **Commit:** 01f0fd4e (live Display) / 191bd859 (route + boot wiring)

**3. [Rule 1 - Dedup] Promote the preview decode into the shared display.NormalizeToolPreview**
- **Found during:** Task 3.
- **Issue:** Task 1 placed a `decodeWebToolResult` helper in the agent package; Task 3's replay path needed the identical decode. Duplicating it would risk live/replay drift (the exact Pitfall 4 the plan warns about).
- **Fix:** Promoted the decode+normalize into `display.NormalizeToolPreview` (a NEW shared symbol in the `display` package, which already imports `web`); both the live `deriveDisplay` and the replay `rederiveDisplays` call it. Live/replay parity is now structural, not coincidental.
- **Files modified:** internal/agent/display/preview.go (new), internal/agent/llm_agent_display.go (refactored to delegate)
- **Commit:** be7f2fb5

---
**Total deviations:** 3 auto-fixed (1 blocking LOC split, 1 missing live-wiring/route, 1 dedup). No architectural changes; no scope creep — the three plan deliverables landed exactly as specified.

## Known Stubs
None. The source list, image-proxy, and replay re-derive are fully wired to real inputs. The per-type cards that consume the image-proxy + displays are 26-04/05 scope (not stubs of this plan).

## Issues Encountered
- The first Task-1 commit was blocked by the file-size hook (deviation 1) and a gofmt re-stage (the `gofmt` pre-commit step reformats then the commit must re-stage). Resolved by the split + re-stage; subsequent commits were clean.
- `.planning/STATE.md` carried a pre-existing working-tree modification from execution start (out of scope for the task commits); it is handled in the tracking step.

## Threat surface scan
No new security-relevant surface beyond the plan's `<threat_model>`. The image-proxy is the one new network egress and is fully inside the existing SSRF guard (T-26-09) behind `RequireAuth` (T-26-10); the source list rides the cache-safe tail-inject (T-26-08); the replay re-derive runs the same trusted normalizer (T-26-11). No new dependency (T-26-SC).

## Self-Check: PASSED
All 13 created files exist on disk; all 3 task commits (`01f0fd4e`, `191bd859`, `be7f2fb5`) are present in git history. `go build ./...` + `go vet` clean; `bash scripts/cache_invariant_audit.sh` green (23 byte-identical `messages[0]` hashes); `go test -race` green across `internal/agent/prompt`, `internal/agent`, `internal/agent/display`, `internal/web`, `internal/agui`, `cmd/aura`.

---
*Phase: 26-typed-display-protocol-router*
*Completed: 2026-06-18*
