---
phase: 37C-web-voice-lane-inserted
plan: 05
subsystem: web voice-input lane (assistant-ui dictation adapter + Composer mic rewrite + runtime adapter wiring)
tags: [web-voice, WEBVOICE, stt, dictation-adapter, assistant-ui, mediarecorder, auto-speak, react, vitest, a11y]
type: execute
wave: 5
autonomous: true
dependency_graph:
  requires:
    - phase: "37C-03"
      provides: "POST /api/stt (transcribe-and-discard) + GET /api/voice/capabilities {tts,stt}"
    - phase: "37C-04"
      provides: "voiceMocks (MediaRecorder/getUserMedia/Audio doubles), VoiceModeProvider.markTurnDictated, useAutoSpeak, speechAdapter + dispose(), useVoiceMode caps"
  provides:
    - "web/src/chat/voice/dictationAdapter — custom DictationAdapter (MediaRecorder → POST /api/stt → onSpeech(isFinal:true) native insert; empty→no-insert clean; 4xx→error; cancel→no-POST)"
    - "Composer dictation-primary mic (runtime startDictation/stopDictation) with the KEPT MediaRecorder→uploads.addFiles attachment fallback + aria-live dictation-state region + markTurnDictated on insert"
    - "web/src/chat/voice/useVoiceRuntime — caps-gated adapters:{speech,dictation} + speechAdapter.dispose()-on-unmount, extracted so ExternalStoreChat.tsx stays ≤600 LOC"
    - "web/src/chat/voice/AutoSpeak — component that mounts useAutoSpeak inside the runtime provider"
    - "ExternalStoreChat.tsx runtime wiring: adapters attach directly on useExternalStoreRuntime (no RuntimeAdapterProvider) + <AutoSpeak/> mounted + dispose-on-unmount"
    - "i18n chat.dictation.{start,stop,listening,transcribing,error} (en+it)"
  affects:
    - "37C-06 (Playwright voice.spec.ts exercises dictation + speaker + degrade live; coverage/Stryker ≥85%/≥70% gate over both adapters; internal/webui/dist rebuild)"
tech-stack:
  added: []
  patterns:
    - "custom assistant-ui DictationAdapter: single-shot MediaRecorder→POST whose stop() AWAITS the onstop→POST→callbacks so the composer's session.stop().finally(cleanup) can't unsubscribe onSpeech before the insert lands (RESEARCH Landmine #1)"
    - "insert via onSpeech({transcript,isFinal:true}) — the ONLY path base-composer-runtime-core writes into the composer _text; onSpeechEnd is cleanup-only (payload discarded)"
    - "Composer dictation state machine driven from s.composer.dictation + an imperative getState().text read: markTurnDictated fires only when a transcript actually inserted (text grew), empty/error announces chat.dictation.error and keeps the mic usable"
    - "voice runtime wiring extracted into a useVoiceRuntime() hook + AutoSpeak component so the near-cap ExternalStoreChat.tsx stays ≤600 LOC while attaching adapters directly on the external-store runtime (RESEARCH Q1: capabilities derive from adapter presence, no RuntimeAdapterProvider)"
    - "cross-closure mutable flag held in an object ({cancelled}) to survive TS flow-narrowing under @typescript-eslint/no-unnecessary-condition"
key-files:
  created:
    - "web/src/chat/voice/dictationAdapter.ts + .test.ts (custom DictationAdapter, 761a30166)"
    - "web/src/chat/voice/useVoiceRuntime.ts (caps-gated adapters + dispose-on-unmount, 44ca13c0b)"
    - "web/src/chat/voice/AutoSpeak.tsx (auto-speak mount component, 44ca13c0b)"
    - "web/src/chat/ExternalStoreChat.voice.test.tsx (adapter gating + dispose + AutoSpeak, 44ca13c0b)"
  modified:
    - "web/src/chat/Composer.tsx (dictation-primary mic + aria-live + kept attachment fallback; 283 LOC, cb809f7ce/d6e7d9d7c)"
    - "web/src/chat/__tests__/Composer.test.tsx (dictation branch + no-regression + error cases, cb809f7ce/d6e7d9d7c)"
    - "web/src/chat/ExternalStoreChat.tsx (adapters:{speech,dictation} + AutoSpeak + dispose; 599 LOC, 44ca13c0b)"
    - "web/src/chat/ExternalStoreChat_messages.tsx (readability: text-[0.7rem]→[0.75rem] on the D-05 hint, d6e7d9d7c)"
    - "web/src/i18n/resources.ts (chat.dictation.* en+it, cb809f7ce)"
key-decisions:
  - "stop() awaits the recorder's onstop→POST→callbacks (via a settle promise) so onSpeech inserts BEFORE the composer's stop().finally cleanup unsubscribes it — without this the single-shot transcript is silently dropped (the deeper form of Landmine #1)"
  - "markTurnDictated fires from the Composer only when the composer text GREW during the session (a real insert), detected via an imperative aui.composer().getState() read (no per-keystroke subscription); empty transcript and /api/stt errors are indistinguishable at the Composer, so both announce chat.dictation.error and leave the mic usable"
  - "dictation error handling is split: a synchronous startDictation() throw (no adapter) degrades to the attachment record path (D-10); an async session error announces chat.dictation.error via the aria-live region (the plan's sanctioned softer alternative)"
  - "voice wiring extracted to useVoiceRuntime()+AutoSpeak so ExternalStoreChat.tsx lands at 599 LOC (≤600); a tightened 25-07 comment reclaimed the LOC the adapters field cost"
  - "requirements mark-complete intentionally NOT run — WEBVOICE-02/04 close with 37C-06's live Playwright + coverage/Stryker gate (37C-01..04 precedent)"
patterns-established:
  - "single-shot DictationAdapter whose stop() awaits the transcribe round-trip so native onSpeech insertion survives the composer's post-stop teardown"
  - "Composer voice branch = keep the attachment path as the degraded fallback, never replace it (WEBVOICE-04 no-regression)"
requirements-completed: []
coverage:
  - id: D1
    description: "dictationAdapter: MediaRecorder→POST /api/stt; inserts the transcript via onSpeech(isFinal:true) BEFORE onSpeechEnd cleanup; empty transcript (200 {text:''}) inserts nothing and ends reason:stopped; a non-ok /api/stt ends reason:error with NO onSpeech; cancel() stops tracks + ends cancelled with no POST"
    requirement: WEBVOICE-02
    verification:
      - kind: unit
        ref: "web/src/chat/voice/dictationAdapter.test.ts"
        status: pass
    human_judgment: false
  - id: D2
    description: "Composer Mic dictation-primary on caps.stt (runtime startDictation/stopDictation) with markTurnDictated on a real insert + aria-live listening/transcribing/error; reverts to the KEPT MediaRecorder→uploads.addFiles attachment path when !caps.stt OR startDictation throws (no regression)"
    requirement: WEBVOICE-04
    verification:
      - kind: unit
        ref: "web/src/chat/__tests__/Composer.test.tsx"
        status: pass
    human_judgment: false
  - id: D3
    description: "Runtime wiring: adapters:{speech,dictation} attach directly on useExternalStoreRuntime gated on caps (undefined ⇒ native degrade); AutoSpeak mounted inside the provider; speechAdapter.dispose() invoked on unmount (revokes cached object URLs); ExternalStoreChat.tsx ≤600 LOC"
    requirement: WEBVOICE-02
    verification:
      - kind: unit
        ref: "web/src/chat/ExternalStoreChat.voice.test.tsx"
        status: pass
      - kind: unit
        ref: "web/src/chat/__tests__/ExternalStoreChat.test.tsx (real-runtime mount unregressed)"
        status: pass
    human_judgment: false
  - id: D4
    description: "i18n dictation-state keys chat.dictation.{start,stop,listening,transcribing,error} present in BOTH en + it (parity suite green)"
    verification:
      - kind: unit
        ref: "web/src/i18n (resources.parity)"
        status: pass
    human_judgment: false
  - id: D5
    description: "Live dictation → editable transcript → send + speaker playback + degrade in the real cockpit against the backend; Stryker ≥70% killed over speechAdapter + dictationAdapter; full-matrix coverage ≥85%"
    verification: []
    human_judgment: true
    rationale: "Live mic capture + real /api/stt round-trip + audio playback are the 37C-06 Playwright terminal gate (voice.spec.ts against the live container); jsdom unit tests prove behavior but cannot capture audio or run the mutation gate"
duration: 55min
completed: 2026-07-09
status: complete
---

# Phase 37C Plan 05: Web Voice-Input Lane + Runtime Wiring Summary

**Built the voice-INPUT lane and fused both voice lanes into the runtime: a custom `DictationAdapter` (MediaRecorder → `POST /api/stt` → editable transcript inserted natively via `onSpeech(isFinal:true)`, with a `stop()` that awaits the transcribe round-trip so the insert survives the composer's post-stop teardown), a dictation-primary Composer Mic that keeps today's `uploads.addFiles` attachment path as the degraded fallback and announces listening/transcribing/error via an `aria-live` region, and the final `adapters:{speech,dictation}` + `AutoSpeak` + `dispose()`-on-unmount wiring on `useExternalStoreRuntime` — all extracted into a `useVoiceRuntime()` hook so `ExternalStoreChat.tsx` stays at 599 LOC (≤600).**

## Performance

- **Duration:** ~55 min
- **Completed:** 2026-07-09
- **Tasks:** 3 (all `type="auto" tdd="true"`) + 1 fix commit
- **Files created/modified:** 5 created + 5 modified (across 4 commits)

## Accomplishments

- **Task 1 — dictationAdapter** (`761a30166`, `DICTATION_ADAPTER_OK`): `createDictationAdapter()` opens `getUserMedia`, records with `MediaRecorder`, and on stop POSTs the blob to `/api/stt`. On 200 it fires `onSpeech({transcript,isFinal:true})` (the native insert) FIRST, then `onSpeechEnd` (cleanup); an empty transcript inserts nothing and ends `reason:stopped`; a non-ok response ends `reason:error` with NO `onSpeech` (Composer degrades); `cancel()` stops the tracks and ends `cancelled` without POSTing. Critically, `stop()` awaits the onstop→POST→callbacks so the composer's `session.stop().finally(cleanup)` cannot unsubscribe `onSpeech` before the insert lands. 5 unit tests over the shared `voiceMocks`.
- **Task 2 — Composer dictation-primary** (`cb809f7ce`, `COMPOSER_DICTATION_OK`): when `caps.stt` the Mic toggles runtime dictation and, on a real insert (composer text grew), calls `markTurnDictated()` for auto-speak parity (D-07); an `aria-live role="status"` region announces listening → transcribing → error; the mic `<button>` carries an accurate `aria-label` + `aria-pressed`. When `!caps.stt` OR `startDictation()` throws, it reverts to the unchanged `MediaRecorder → uploads.addFiles` attachment path (WEBVOICE-04, no regression); the Paperclip path is untouched. en+it `chat.dictation.*`. 11 unit tests.
- **Task 3 — runtime wiring** (`44ca13c0b`, `RUNTIME_WIRING_OK`): `useVoiceRuntime()` builds the two adapters once, gates each on caps (`undefined ⇒ native degrade`), and revokes the speech adapter's cached object URLs via `dispose()` on unmount (Landmine #5); `ExternalStoreChat.tsx` attaches `adapters: voiceAdapters` on `useExternalStoreRuntime` and mounts `<AutoSpeak/>` inside `AssistantRuntimeProvider` — landing at **599 LOC** (≤600). 5 unit tests + the existing real-runtime `ExternalStoreChat.test` unregressed.
- **Degrade + a11y:** `caps={false,false}` ⇒ both adapters `undefined` ⇒ runtime reports speech/dictation false (native degrade); the dictation state is announced to screen readers; the mic is a labelled toggle in both modes.

## Task Commits

1. **Task 1: dictationAdapter (MediaRecorder → /api/stt → onSpeech(isFinal))** — `761a30166` (feat, 2 files / +260)
2. **Task 2: Composer Mic dictation-primary + kept attachment fallback + i18n** — `cb809f7ce` (feat, 3 files / +235 / −13)
3. **Task 3: wire both adapters + AutoSpeak + dispose-on-unmount** — `44ca13c0b` (feat, 4 files / +133 / −5)
4. **Fix: readability-token offenders + dictation error-branch coverage** — `d6e7d9d7c` (fix, 3 files / +21 / −3)

**Plan metadata:** (this SUMMARY + STATE + ROADMAP) — final docs commit.

_TDD note: each `tdd="true"` task landed as ONE atomic `feat(...)` commit (impl + its co-located test) per the sequential-executor directive; test-first was honored locally (each symbol was compile-RED before it existed)._

## Files Created/Modified

**Created:**
- `voice/dictationAdapter.ts` / `.test.ts` — custom `DictationAdapter` (MediaRecorder → POST /api/stt → onSpeech insert).
- `voice/useVoiceRuntime.ts` — caps-gated `adapters:{speech,dictation}` + `dispose()`-on-unmount effect.
- `voice/AutoSpeak.tsx` — mounts `useAutoSpeak` inside the runtime provider (renders null).
- `ExternalStoreChat.voice.test.tsx` — adapter gating + dispose-on-unmount + AutoSpeak mount.

**Modified:**
- `Composer.tsx` — dictation-primary mic + aria-live state + kept attachment fallback (283 LOC).
- `__tests__/Composer.test.tsx` — dictation branch + no-regression + error-announcement cases (11 tests).
- `ExternalStoreChat.tsx` — `adapters: voiceAdapters` + `<AutoSpeak/>` + tightened 25-07 comment (599 LOC).
- `ExternalStoreChat_messages.tsx` — readability fix on the D-05 hint (text-[0.7rem]→[0.75rem]).
- `i18n/resources.ts` — `chat.dictation.{start,stop,listening,transcribing,error}` in BOTH en + it.

## Decisions Made

- **`stop()` awaits the transcribe round-trip.** The composer runs `session.stop().finally(() => _cleanupDictation())`, and cleanup unsubscribes `onSpeech`. A naive `stop: () => rec.stop()` resolves immediately, so cleanup would fire before the single-shot POST's `onSpeech` — silently dropping the transcript. The adapter's `stop()` awaits a `settle` promise resolved in the recorder's `onstop` finally, so the insert always lands first. This is the deeper, non-obvious form of RESEARCH Landmine #1.
- **`markTurnDictated` fires on a real insert, not on stop.** The Composer captures the composer text length at dictation start and, when the session tears down, marks the turn only if the text grew (imperative `getState()` read, no per-keystroke subscription). Empty transcripts and `/api/stt` errors are indistinguishable from the Composer's vantage (both end with no text change), so both announce `chat.dictation.error` and leave the mic usable — the adapter-level distinction is proven in `dictationAdapter.test.ts`.
- **Two error arms.** A synchronous `startDictation()` throw (no adapter configured) degrades to the attachment record path (D-10, testable); an async session error surfaces `chat.dictation.error` via the live region (the plan's sanctioned softer alternative).
- **Extraction over inlining.** The adapters field + AutoSpeak mount + dispose effect would push `ExternalStoreChat.tsx` over 600 LOC if inlined, so the memo/caps/dispose live in `useVoiceRuntime()` and the auto-speak mount in `AutoSpeak.tsx`; the file lands at 599 with a tightened (not gutted) 25-07 comment.
- **`requirements mark-complete` deferred.** WEBVOICE-02/04 both carry live-e2e acceptance that closes with 37C-06; `requirements-completed: []` matches the 37C-01..04 precedent.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Readability-token offenders blocking the full vitest suite**
- **Found during:** Task 3 (first full `npm test` run)
- **Issue:** `readabilityTokens.test.ts` (a full-suite-only gate the targeted per-task runs skip) flagged two arbitrary-token offenders: (a) `ExternalStoreChat_messages.tsx` used `text-[0.7rem]` (10.85px < the 11px operator-density floor) on the 37C-04 D-05 too-long hint — a latent offender that shipped because 37C-04 ran only targeted tests; (b) my own Task-2 `Composer.tsx` tinted the dictating mic with `text-accent`, which the gate reserves for the primary Send CTA.
- **Fix:** `text-[0.7rem]`→`text-[0.75rem]` (11.6px, matches the running-status row); dropped the `text-accent` tint (the dictation state is already conveyed by the Square glyph + aria-pressed + the aria-live region).
- **Files modified:** `ExternalStoreChat_messages.tsx`, `Composer.tsx`
- **Verification:** `readabilityTokens.test.ts` green; full suite 1203/1203.
- **Committed in:** `d6e7d9d7c`

**2. [Rule 1 - Coverage] Cover the dictation error/empty branch**
- **Found during:** Task 3 (coverage read: `Composer.tsx` 84.9% < 85%)
- **Issue:** the dictation error/empty announcement path (session ends with no insert → no `markTurnDictated`, `chat.dictation.error` announced) was uncovered, dipping the file below the 85% floor.
- **Fix:** added a Composer test for the no-insert branch; `Composer.tsx` → 85.7% stmts. The aggregate was already 92.3% (global gate), but the per-file dip warranted a real test of new logic.
- **Files modified:** `__tests__/Composer.test.tsx`
- **Verification:** `Composer.tsx` 85.7% stmts; aggregate 92.3% ≥ 85%.
- **Committed in:** `d6e7d9d7c`

---

**Total deviations:** 2 auto-fixed (both Rule 1 — a shipped readability regression + a coverage gap on new logic). **Impact:** No content deviation from the plan; every `<behavior>` case and all six prohibitions were delivered. One offender was inherited from 37C-04 and fixed on discovery per the CLAUDE.md "CI baseline green — fix all red" mandate. No architectural (Rule 4) decisions, no auth gates.

## Issues Encountered

- **Composer test filter path.** The plan's verify command `npx vitest run src/chat/Composer` matches nothing — the pre-existing `Composer.test.tsx` lives under `src/chat/__tests__/` (not co-located). Ran it as `src/chat/__tests__/Composer.test.tsx`; behavior/token unchanged (`COMPOSER_DICTATION_OK`).
- **Full-suite flake (unrelated).** The final full `npm test` surfaced a single 5000ms **timeout** in `src/documents/__tests__/DocumentsWorkspace.test.tsx` — an unrelated file; it passes in isolation (4/4) and passed in the clean full run (1203/1203) right after the readability fixes. A known parallel-load flake on the shared mini-PC, not a regression from this plan.
- **Lint false positives.** `@typescript-eslint/no-unnecessary-condition` flagged the cross-closure `cancelled` flag (fixed with an object holder), and `no-unnecessary-type-assertion` flagged `as` widening in the test doubles (fixed with typed variables inside `vi.hoisted`).

## Prohibitions Honored

- Transcript inserted via `onSpeech({transcript,isFinal:true})`, NEVER `onSpeechEnd` (asserted: onSpeech fires before onSpeechEnd; empty transcript skips onSpeech).
- The `MediaRecorder → uploads.addFiles` attachment path is KEPT as the degraded fallback (asserted: `!caps.stt` records an attachment; `startDictation` throw degrades to it); the Paperclip path is untouched.
- `speechAdapter.dispose()` is invoked on chat/runtime unmount (asserted via the mocked adapter's dispose spy).
- `adapters.voice` (RealtimeVoiceAdapter) NOT wired; `RuntimeAdapterProvider` NOT used (grep-confirmed absent).
- `ExternalStoreChat.tsx` at 599 LOC ≤ 600 (kept via `useVoiceRuntime` extraction).
- Every new i18n key lands in BOTH en+it (parity suite green).

## Known Stubs

None introduced. The `chat.dictation.error` announcement covering both empty-transcript and `/api/stt` error is an intentional Composer-level simplification (the two are indistinguishable from composer state; the adapter distinguishes them and is unit-tested), not a stub.

## Verification

All web tests run on the Windows host via Git Bash (node v24.16.0, vitest 4.1.9, tsc 6.0.3) — NOT WSL.

| Check | Result |
|-------|--------|
| Task 1 (`src/chat/voice/dictationAdapter`) | 5 pass → `DICTATION_ADAPTER_OK` |
| Task 2 (`src/chat/__tests__/Composer.test.tsx`) | 11 pass → `COMPOSER_DICTATION_OK` |
| Task 3 (`src/chat/ExternalStoreChat.voice`) | 5 pass → `RUNTIME_WIRING_OK` |
| `npx tsc --noEmit` | clean (full tree) |
| `npm run lint` scope (`eslint --max-warnings=0`) | clean on all new/edited files |
| i18n parity (`resources.parity`) | green (en↔it) |
| Full suite (`npm test`, coverage) | 149 files / **1203 pass** (one unrelated DocumentsWorkspace timeout flake, passes isolated) |
| Aggregate coverage | 92.3% stmts / 86.4% branch / 92.4% funcs / 94.1% lines (≥85% floor) |
| LOC ≤600 | ExternalStoreChat.tsx **599** · Composer.tsx 283 |
| Regression (`ExternalStoreChat.test`, `.attachments`, speaker, `AppShell.voice`) | unregressed |

_Full `npm test` coverage + Stryker ≥70% on both adapters is the Wave-6 (37C-06) gate; this plan required targeted unit green + tsc/lint clean + full-suite green, all achieved. The dictationAdapter's insert/empty/error/cancel branches were written testably for the ≥70% Stryker run in 37C-06._

## Next Phase Readiness

- **37C-06 (terminal gate)** is unblocked: the whole voice surface is live end to end — speaker + auto-speak (37C-04) and dictation + runtime wiring (37C-05). It exercises dictation → editable transcript → send + speaker playback + degrade via Playwright `voice.spec.ts` against the live container, runs the coverage/Stryker gate over both adapters, and rebuilds `internal/webui/dist`.
- **WEBVOICE-01/02/03/04 stay `[ ]`** — phase-spanning; the input lane + full runtime wiring land here, but the requirements close with 37C-06's live e2e + coverage/mutation gate. `requirements mark-complete` intentionally NOT run (37C-01..04 precedent).

## Self-Check: PASSED

- FOUND (created): `voice/dictationAdapter.ts` + `.test.ts`, `voice/useVoiceRuntime.ts`, `voice/AutoSpeak.tsx`, `ExternalStoreChat.voice.test.tsx`
- FOUND (modified): `Composer.tsx`, `__tests__/Composer.test.tsx`, `ExternalStoreChat.tsx`, `ExternalStoreChat_messages.tsx`, `i18n/resources.ts`
- FOUND: commit `761a30166` (Task 1 — dictationAdapter, +260)
- FOUND: commit `cb809f7ce` (Task 2 — Composer dictation-primary, +235/−13)
- FOUND: commit `44ca13c0b` (Task 3 — runtime wiring, +133/−5)
- FOUND: commit `d6e7d9d7c` (fix — readability + coverage, +21/−3)
- Verify tokens printed: `DICTATION_ADAPTER_OK`, `COMPOSER_DICTATION_OK`, `RUNTIME_WIRING_OK`
- Symbols confirmed: `/api/stt` + `isFinal: true` in dictationAdapter; `caps` + `markTurnDictated` + `aria-live` + `addFiles` in Composer.tsx; `adapters:` + `dictation` + `AutoSpeak` + `dispose` in ExternalStoreChat.tsx (NO `RuntimeAdapterProvider`); `chat.dictation.listening/transcribing/error` in BOTH en+it
- LOC: `ExternalStoreChat.tsx` 599 ≤ 600 · `Composer.tsx` 283 ≤ 600
- `.planning/graphs/.last-build-status.json` + `GRAPH_REPORT.md` left uncommitted per directive

---
*Phase: 37C-web-voice-lane-inserted*
*Completed: 2026-07-09*
