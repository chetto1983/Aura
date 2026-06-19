---
status: testing
phase: 26-typed-display-protocol-router
source: [26-01-SUMMARY.md, 26-02-SUMMARY.md, 26-03-SUMMARY.md, 26-04-SUMMARY.md, 26-05-SUMMARY.md, 26-06-SUMMARY.md]
started: 2026-06-19T05:12:11Z
updated: 2026-06-19T05:12:11Z
---

## Current Test
<!-- OVERWRITE each test - shows where we are -->

number: 1
name: Cold Start Smoke Test
expected: |
  Kill any running `aura serve`. Rebuild the binary so the embedded `internal/webui/dist`
  is fresh (the dist was rebuilt in 26-06). Start `aura serve` against the live
  Postgres+Neo4j stack and open the cockpit in a browser; log in.
  Server boots with no errors, the cockpit SPA loads, and sending a question that triggers
  a web_search streams an answer where the web tool turn upgrades from a plain raw
  activity card to a rich typed display (no crash, no blank/lost card).
awaiting: user response

## Tests

### 1. Cold Start Smoke Test
expected: |
  Kill any running `aura serve`; rebuild the binary (fresh embedded dist); start `aura serve`
  on the live stack; open + log into the cockpit. Server boots clean, SPA loads, and a
  web_search question streams an answer whose web tool turn renders as a rich typed display
  (not a raw card, no crash, no blank card).
result: [pending]

### 2. Typed web_result card (live web_search)
expected: |
  Ask something that triggers web_search (e.g. a current-events / "search the web for…" query).
  Instead of a raw "web_search" activity card you see a rich web_result card: one row per
  result with title, domain chip, snippet, a relevance meter, and published date; thumbnails
  load through the image-proxy (no broken external images, no raw external host in the img src);
  and if there are >3 results, in-card pagination shows "X–Y of N" with working prev/next and a
  per-page control (default 3/page).
result: [pending]

### 3. Citation bubbles → hovercard → click-through (human sign-off #1)
expected: |
  In a completed web_search answer containing [n] markers: each [n] is an inline chip placed
  at the claim position (not bunched at the end of the paragraph). Hovering OR keyboard-focusing
  a chip opens a hovercard showing a type-icon + title + snippet preview. Clicking the chip body
  opens the Source Explorer focused to that source's Metadata. Cited sources render in the accent
  color; consulted-only sources are neutral. An [n] with no matching source stays as literal text
  (no live chip).
result: [pending]

### 4. "Sources (N)" button → read-only Source Explorer (human sign-off #2)
expected: |
  After a web answer, a "Sources (N)" affordance appears (hidden when N=0). Clicking it opens a
  fullscreen Source Explorer with three views: Table (sortable headers, search box, a Cited/Consulted
  tag per row, 9-rows/page pagination), Metadata (ref/type/url/confidence/snippet shown as text),
  and Configuration (cited/consulted counts as text). ALL views are READ-ONLY — there is NO
  Re-Analyze / Clear / Save / Edit / Apply control and no form input. Esc closes the sheet, focus is
  trapped while open and restored on close, and an incomplete-source warning banner appears if any
  source is incomplete.
result: [pending]

### 5. document card (live web_fetch)
expected: |
  Ask something that triggers web_fetch on a specific URL. The fetched page renders as a sanitized
  markdown document card: headings, lists and links render; any inline [n] citations resolve; images
  display (routed safely); and a Copy-text control copies the raw markdown. A page containing a
  <script> or raw HTML does NOT execute — it is sanitized/escaped.
result: [pending]

### 6. D-06 replay parity (human sign-off #4)
expected: |
  After a web_search / web_fetch turn, reopen the thread (reload the cockpit or re-select the
  conversation from history). The typed web_result / document displays re-render IDENTICALLY to the
  live render — same cards, same sources, same citations — not raw fallback cards, nothing lost.
  (Optional automated proof: from web/, run `AURA_E2E_ORIGIN=<serve-origin> npx playwright test replay`
  against a live `aura serve` serving the freshly-built dist — all 8 replay tests pass, including the
  replayText === liveText parity assertion.)
result: [pending]

### 7. Non-web card types — Phase 26 live-scope acceptance (covers swarm sign-off #3 + code/table/chart/system_event/local_artifact)
expected: |
  IMPORTANT SCOPE NOTE confirmed by code read (llm_agent_events.go:152 → deriveDisplay →
  NormalizeToolPreview): in Phase 26 the ONLY tools wired to the live typed-display emit are
  web_search (→ web_result) and web_fetch (→ document). The other six card types — system_event,
  swarm_report, code, table, chart, local_artifact — are fully built and proven by the 502-test
  Vitest suite (including the no-mailbox negative assertion for swarm and the no-SSRF-internals
  assertion for system_event), but they have NO live producer this phase. So a live swarm_spawn /
  shell / web-error today shows the RAW fallback card by design, not the typed card.
  EXPECTED: operator confirms acceptance that these six card types are automated-verified-only this
  milestone (live emit deferred to a later phase). NOTE: this corrects 26-VERIFICATION.md human item
  #3, which asked for a "live swarm_spawn → SwarmReportTable" check that is not achievable as written
  because swarm is not live-wired in Phase 26.
result: [pending]

## Summary

total: 7
passed: 0
issues: 0
pending: 7
skipped: 0
blocked: 0

## Gaps

[none yet]
