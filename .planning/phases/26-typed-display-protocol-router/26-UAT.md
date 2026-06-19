---
status: complete
phase: 26-typed-display-protocol-router
source: [26-01-SUMMARY.md, 26-02-SUMMARY.md, 26-03-SUMMARY.md, 26-04-SUMMARY.md, 26-05-SUMMARY.md, 26-06-SUMMARY.md]
started: 2026-06-19T05:12:11Z
updated: 2026-06-19T06:25:00Z
method: automated E2E (Playwright) on desktop + mobile, against a freshly-built `aura serve`
---

## Current Test

[testing complete]

## Validation method

Validated by a real-browser Playwright E2E matrix (not conversational UAT) per the
autonomous QA goal: 51 tests across **3 device profiles** — desktop Chrome, Pixel 5
(mobile Chrome), iPhone 13 (mobile WebKit) — run against a freshly-built `aura serve`
embedding the current dist. 3 consecutive green runs (51/51 each). Each typed-display
card renders through the real DisplayRouter via deterministic snapshot injection (the
proven D-06 replay path); heavy/SSE routes are mocked at the page-network layer so only
the served SPA + passphrase auth come from the live serve.

A critical production bug was found and fixed during validation (see Gaps).

## Tests

### 1. Cold Start Smoke Test
expected: fresh binary + dist, `aura serve` boots clean, cockpit loads + auth, web tool turn renders a rich typed display
result: pass
evidence: serve rebuilt from fresh dist, /healthz 200, full e2e suite green against it

### 2. Typed web_result card (live web_search) + image-proxy thumbnails + pagination
expected: rich card (title/domain chip/snippet/relevance), thumbnails via /api/image-proxy (no raw external img), "X–Y of N" pagination
result: pass
evidence: displays.spec "web_result renders a rich card with image-proxy thumbnails + in-card pagination" — green on chromium + mobile-chrome + mobile-safari; asserts img[src^="/api/image-proxy"], 0 raw external img, "1–3 of 5"→"4–5 of 5"

### 3. Citation chip → read-only Source Explorer click-through
expected: inline cited chip; click opens the Source Explorer focused on the source; cited=accent
result: pass
evidence: displays.spec "citation chip opens the read-only Source Explorer focused on the source" — green on all 3 profiles

### 4. "Sources (N)" button → read-only Source Explorer (Table/Metadata/Configuration)
expected: "Sources (N)" affordance; opens fullscreen sheet; Table/Metadata/Configuration tabs; NO Re-Analyze/Clear/Save/Edit/Apply; Esc closes
result: pass
evidence: displays.spec ""Sources (N)" button opens the Source Explorer with read-only Table/Metadata/Configuration" — green on all 3; asserts read-only notice + absence of all write verbs

### 5. document card (web_fetch) — sanitized markdown, script stripped
expected: sanitized markdown (heading/list/links), injected <script> does not execute
result: pass
evidence: displays.spec "document card renders sanitized markdown and never executes injected script" — green on all 3; window.__xss_doc undefined

### 6. D-06 replay parity (reopen → re-derive → identical render)
expected: typed display renders live, then re-renders IDENTICALLY on reopen (replayText === liveText)
result: pass
evidence: replay.spec "a typed display renders LIVE, then re-renders IDENTICALLY on reopen" — green on all 3; D-06 parity assertion held

### 7. Data/status cards (system_event / swarm_report / table / chart / code / local_artifact)
expected: each card renders its typed payload correctly; security postures hold (no SSRF internals; escaped code; no swarm mailbox)
result: pass
evidence: displays.spec system_event (no 169.254.169.254 in DOM), swarm_report (worker table + row-expand, no textbox/composer in card), table (sort/filter/copy/CSV/paginate), chart (accessible <table> fallback), code (escaped <script>, window.__xss_code undefined) — all green on all 3 profiles.
note: in Phase 26 only web_search/web_fetch are wired to the LIVE typed-display emit; the other six cards are validated at the real-browser render/interaction level via snapshot injection (their live producers are a later phase). This is the deliberate, documented 26-03 scope.

## Summary

total: 7
passed: 7
issues: 0
pending: 0
skipped: 0
blocked: 0
e2e_tests: 51 (17 per profile × 3 profiles), 3 consecutive green runs

## Gaps

# A critical production bug was found AND fixed during validation (closed, not open):
- truth: "The cockpit renders on iOS Safari / iPadOS Safari"
  status: fixed
  severity: blocker
  found_by: mobile-safari (iPhone 13 / WebKit) E2E project
  root_cause: "vite auraCssCompatLintPlugin renamed the built CSS to a post-strip hash but its reference-rewrite missed lazy route chunks under rolldown — AppShell-*.js kept pointing at index-Dr39YWH1.css while the emitted file was index-e8c67f63.css. The 404 (text/plain) tripped WebKit's strict stylesheet-MIME check and crashed the SPA to its error boundary. Chromium silently tolerated the failed preload, masking it."
  fix: "strip the legacy declaration in place, keep the rolldown-emitted filename (commit 26ce045a); dist regenerated, all CSS references resolve, CSS serves as text/css"
  verified: "mobile-safari E2E green (51/51 × 3 runs); web_result card renders on iPhone 13 (was a render crash)"

# No open gaps. All automated + mobile gates green.
