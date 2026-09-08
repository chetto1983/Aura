---
status: resolved
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
- next_action: transport correction complete; continue the separate coordinator-grounding and cross-owner validation work.
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
- fix: c5c5053c5 restricts non-navigation interception to the existing precache inventory. No new transport, retry wrapper or cache framework.
- verification: the new real-HTTP Playwright regression failed before the fix (fromServiceWorker true), then passed on desktop and mobile Chrome (2/2). Static asset responses still use the cache. SSE remains OPEN with zero errors for 16 seconds after stopWorker. MCP independently reproduced the passing result after verifying the new worker code was active; the initial probe during the old-to-new worker transition still exercised the old behavior. Probe EventSources closed in finally, CDP detached, no persistent bypass.
- live_control: 104W in conversation 01a07fc2-fa60-7290-a0a5-eaf2fc4823eb, child w1-5f8a2cbedea2eb3c99ff4da3b56661c1. Python PID50190 was sleeping75 before stopWorker. Subsequent UI cancellation returned202 (network request9752); the process disappeared and both worker and command became Annullato live, with no remaining stop control. Reload retained the selected worker and terminal state on the same URL. The first waitForResponse predicate mistakenly used /workers/ instead of the actual /swarm/ route; the native network log confirms the response, not that timed-out predicate.
- deployment: healthy image4500b18afce261da2289318fb29834297fb4776c402bdde3bd39dcf8118202e6, stamp9988af0b7-pwastream.
- limits: this isolates the PWA response-lifetime failure. The earlier intermittent conversation navigation was not reproduced by explicit goto/reload or the104W reload; no causal fix is claimed for it.
