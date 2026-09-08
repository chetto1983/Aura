---
status: fixing
trigger: "anche la UI ha bisogno di una modernizzazione non posso vedere gli agenti lavorare"
created: 2026-09-08
updated: 2026-09-08
---

## Symptoms

- Expected: worker status and transcript streams deliver terminal outcomes while the pane is open.
- Actual: in MCP104T both EventSource objects were CLOSED, the pane showed a stream error but retained Running and the old controls.
- Error: "Impossibile caricare l'attività dell'agente. Apri il file del report o riprova."
- Timeline: observed intermittently after local container updates. Retained replay works. Instrumented104U delivered RUN_FINISHED, closed only the child stream, and showed cancellation correctly.
- Reproduction: open a live worker during a45-second shell command and cancel it; initial104T failed, instrumented104U passed. A later reload unexpectedly returned to104T from104U; an explicit goto104U followed by reload did not reproduce that navigation.

## Current Focus

- hypothesis: the custom service worker proxies every same-origin GET, including long-lived SSE; stopping or replacing that worker may close proxied streams.
- next_action: restrict non-navigation interception to explicit precache assets and add a real service-worker-enabled browser regression.
- test: browser-only controlled lifetime experiment, no agent execution, no mocked HTTP responses, no application edits.
- result: both connections started OPEN; stopping the active service worker changed only the mediated connection to CLOSED with an error. The direct connection stayed OPEN.

## Evidence

-104T conversation:01a07f5d-aa8f-7d09-bab4-b1e9283d961c; child w1-2c21fd958624f82363a57497b6ab03a6.
- Backend job canceled at attempt1; raw tool record has canceled status and cancelled metadata; authenticated fresh SSE fetch returns full terminal data and RUN_FINISHED.
- CDP Runtime.queryObjects inspected both actual EventSource objects: readyState2 (CLOSED).
-104U conversation:01a07f71-f1f0-7d20-9783-72153c9df612; read-only open/error/close instrumentation observed normal child completion and an open global stream.
- User explicitly confirmed only this Codex session uses the browser; concurrent sandbox filesystem work is unrelated ownership and must be preserved.
- server_sse.go sends15-second heartbeats and flushes; ReadTranscript opens the file anew and advances only over complete lines. Fresh responses are not compressed.
- vite.config.ts's custom PWA fetch handler calls respondWith(caches.match(request).then(cached=>cached||fetch(request))) for EVERY same-origin GET that is not navigation. No API/SSE bypass exists.
- Playwright CI sets serviceWorkers:block, so its green route/visual suites do not exercise this service-worker path.

## Eliminated

- hypothesis: another agent or the user navigates this tab concurrently.
  evidence: explicit user answer, "Solo questa sessione usa il browser".
- hypothesis: the cancellation record or generated terminal SSE is missing.
  evidence: actual persisted transcript, job row, and fresh authenticated stream contain the terminal cancellation.
- hypothesis: response compression buffers the stream.
  evidence: live response has no Content-Encoding.

## Resolution

- root_cause: the catch-all same-origin GET handler binds API/SSE response lifetime to the PWA worker. CDP stopWorker reproduces the closure while a simultaneous bypassed connection survives.
- fix: pending; use the existing precache inventory to leave non-asset requests untouched by respondWith.
- verification: causal MCP A/B experiment passed; after-fix regression pending. Probe EventSources closed in finally, bypass restored false, CDP detached. No data or agent execution was mutated.
