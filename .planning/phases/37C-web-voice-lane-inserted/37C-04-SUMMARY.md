---
phase: 37C-web-voice-lane-inserted
plan: 04
subsystem: web voice-output lane (assistant-ui speech adapter + ephemeral voice mode + speaker control)
tags: [web-voice, WEBVOICE, tts, speech-adapter, assistant-ui, voice-mode, auto-speak, react, vitest, ephemeral-state]
type: execute
wave: 4
autonomous: true
dependency_graph:
  requires:
    - "37C-03 (POST /api/tts → audio/mpeg + X-Aura-TTS-Truncated header; GET /api/voice/capabilities → {tts,stt})"
  provides:
    - "web/src/chat/voice/useVoiceCapabilities — one-shot GET /api/voice/capabilities probe, default {false,false}"
    - "web/src/chat/voice/speechAdapter — custom SpeechSynthesisAdapter (fetch→blob→Audio, per-text cache, truncated flag from X-Aura-TTS-Truncated, dispose() revokes URLs)"
    - "web/src/chat/voice/shouldSpeak — pure OR predicate (Telegram ShouldSpeak parity)"
    - "web/src/chat/voice/voiceMocks — SHARED vitest doubles (Audio/objectURL/tts-fetch + MediaRecorder/getUserMedia for 37C-05)"
    - "web/src/chat/voice/{voiceModeContext,VoiceModeProvider} — ephemeral voice-mode context {caps,voiceMode,turnWasDictated,toggle,markTurnDictated,clearTurnDictated}"
    - "web/src/chat/voice/useAutoSpeak — shouldSpeak-gated auto-speak effect (mounted by 37C-05)"
    - "web/src/chat/voice/VoiceModeToggle + AppShell header wiring — caps.tts-gated ephemeral toggle"
    - "AssistantSpeakerControl in ExternalStoreChat_messages.tsx — caps.tts-gated Speak/StopSpeaking + D-05 too-long hint"
  affects:
    - "37C-05 (reuses voiceMocks DictationAdapter doubles + VoiceModeProvider.markTurnDictated; mounts useAutoSpeak + wires speechAdapter/dispose in ExternalStoreChat.tsx)"
    - "37C-06 (Playwright voice.spec.ts exercises the speaker + toggle live; coverage/Stryker gate over speechAdapter)"
tech_stack:
  added: []
  patterns:
    - "custom assistant-ui SpeechSynthesisAdapter over fetch→blob→new Audio(objectURL) with a per-text (thread-scoped) blob cache — a repeat Speak never re-bills /api/tts (D-03, Landmine #5)"
    - "truncated flag stamped BOTH on the Utterance and on every emitted status object, so the stock runtime surfaces it through s.message.speech.status by reference — the X-Aura-TTS-Truncated header is a visible D-05 hint, not dead code"
    - "context+hook in a NON-component .ts module (voiceModeContext, mirrors sourceExplorerControls.ts) with a DISABLED default so useVoiceMode outside a provider degrades to caps-false rather than throwing (keeps isolated chat tests green)"
    - "ephemeral session React state (D-06): voiceMode + turnWasDictated are useState only — no localStorage, no server pref"
    - "shared voiceMocks helper (Audio/objectURL/tts-fetch + MediaRecorder/getUserMedia), self-tested so it never drags the coverage floor; reused by 37C-05's dictation lane"
key_files:
  created:
    - "web/src/chat/voice/useVoiceCapabilities.ts + .test.ts (one-shot caps probe, a699fc38c)"
    - "web/src/chat/voice/speechAdapter.ts + .test.ts (custom SpeechSynthesisAdapter, a699fc38c)"
    - "web/src/chat/voice/shouldSpeak.ts + .test.ts (OR parity predicate, a699fc38c)"
    - "web/src/chat/voice/voiceMocks.ts + .test.ts (SHARED test doubles, a699fc38c)"
    - "web/src/chat/voice/voiceModeContext.ts (context + useVoiceMode hook + disabled default, 833d72a74)"
    - "web/src/chat/voice/VoiceModeProvider.tsx + .test.tsx (ephemeral provider, 833d72a74)"
    - "web/src/chat/voice/VoiceModeToggle.tsx (caps.tts-gated header switch, 833d72a74)"
    - "web/src/chat/voice/useAutoSpeak.ts + .test.tsx (shouldSpeak-gated auto-speak, 833d72a74)"
    - "web/src/AppShell.voice.test.tsx (toggle present/flip/absent, 833d72a74)"
    - "web/src/chat/ExternalStoreChat_messages.speaker.test.tsx (speaker control + tooLong + single-utterance, 0244648ea)"
  modified:
    - "web/src/AppShell.tsx (wrap chat-workspace in VoiceModeProvider + mount VoiceModeToggle; 590 LOC, 833d72a74)"
    - "web/src/chat/ExternalStoreChat_messages.tsx (AssistantSpeakerControl in the assistant ActionBar; 347 LOC, 0244648ea)"
    - "web/src/i18n/resources.ts (chat.voiceMode.on/off + chat.action.speak/stopSpeaking/tooLong, en+it; 833d72a74 + 0244648ea)"
decisions:
  - "the truncated flag is stamped on BOTH the Utterance (Utterance.truncated) AND every status object the adapter emits — the stock base-thread-runtime-core copies utterance.status into s.message.speech by reference, so the D-05 hint is genuinely reachable off the speech state in production without 37C-05 modifying the runtime (belt-and-suspenders against dead code)"
  - "useVoiceMode returns a DISABLED default (caps {false,false}) when no VoiceModeProvider is mounted — the context+hook live in a non-component voiceModeContext.ts (react-refresh), and the default keeps the existing ExternalStoreChat tests green (AssistantMessage renders with no provider → speaker absent, no throw)"
  - "VoiceModeToggle extracted to its own file (plan-sanctioned) so AppShell stays ≤600 (590 with the +provider wrap +toggle)"
  - "voiceMocks.ts is a shared, self-tested helper (voiceMocks.test.ts) — it is included in coverage and its MediaRecorder/getUserMedia doubles (for 37C-05) aren't exercised by 37C-04's other tests, so the self-test keeps it from dragging the aggregate under 85%"
  - "AssistantSpeakerControl kept INLINE (exported) in ExternalStoreChat_messages.tsx (grep tokens StopSpeaking/chat.action.speak/chat.action.tooLong land there) rather than extracted; the isolated speaker test imports it and mocks @assistant-ui/react minimally (no sibling does top-level assistant-ui destructuring, so module eval is safe)"
  - "the speaker test models single-active-utterance with a per-message React context feeding the mocked AuiIf/useAuiState/ActionBarPrimitive: a shared speakingId means at most one message can hold non-null speech, so a second Speak leaves exactly one StopSpeaking (RESEARCH Landmine #6)"
  - "single atomic feat commit per task (impl+test together) per the sequential-executor directive; commits run in the background because the whole-tree file-size pre-commit hook exceeds the 2-min foreground timeout"
metrics:
  tasks_completed: 3
  duration: "~55 min"
  completed: "2026-07-09"
  files_changed: 19
  commits: ["a699fc38c", "833d72a74", "0244648ea"]
requirements_touched: [WEBVOICE-01, WEBVOICE-03]
requirements_completed: []
coverage:
  - id: D1
    description: "useVoiceCapabilities: one-shot GET /api/voice/capabilities probe (Accept json + same-origin, AbortController-cancelled), default {false,false} on error/non-2xx, coerces non-true to false, fetches exactly once"
    requirement: WEBVOICE-03
    verification:
      - kind: unit
        ref: "web/src/chat/voice/useVoiceCapabilities.test.ts"
        status: pass
    human_judgment: false
  - id: D2
    description: "speechAdapter: fetch→blob→Audio, running→ended(finished/cancelled/error), per-text blob cache (no second fetch), truncated flag from X-Aura-TTS-Truncated, dispose() revokes cached URLs, cancel-before-blob path"
    requirement: WEBVOICE-01
    verification:
      - kind: unit
        ref: "web/src/chat/voice/speechAdapter.test.ts"
        status: pass
    human_judgment: false
  - id: D3
    description: "shouldSpeak(voiceMode, turnWasDictated) === voiceMode || turnWasDictated (Telegram ShouldSpeak parity, D-07)"
    requirement: WEBVOICE-01
    verification:
      - kind: unit
        ref: "web/src/chat/voice/shouldSpeak.test.ts"
        status: pass
    human_judgment: false
  - id: D4
    description: "VoiceModeProvider: ephemeral {caps,voiceMode,turnWasDictated,toggleVoiceMode,markTurnDictated,clearTurnDictated}; no localStorage/fetch-to-persist; resets on remount (D-06)"
    requirement: WEBVOICE-01
    verification:
      - kind: unit
        ref: "web/src/chat/voice/VoiceModeProvider.test.tsx"
        status: pass
    human_judgment: false
  - id: D5
    description: "useAutoSpeak: speaks a newly-completed assistant reply exactly when shouldSpeak(voiceMode, turnWasDictated); consumes turnWasDictated per turn; waits for isRunning to settle; never speaks rehydrated history on mount"
    requirement: WEBVOICE-01
    verification:
      - kind: unit
        ref: "web/src/chat/voice/useAutoSpeak.test.tsx"
        status: pass
    human_judgment: false
  - id: D6
    description: "AppShell header toggle: rendered only when caps.tts, flips voiceMode + aria-pressed on click, ABSENT when !caps.tts (WEBVOICE-03 degrade)"
    requirement: WEBVOICE-03
    verification:
      - kind: automated_ui
        ref: "web/src/AppShell.voice.test.tsx"
        status: pass
    human_judgment: false
  - id: D7
    description: "AssistantSpeakerControl: caps.tts-gated Speak/StopSpeaking toggle on s.message.speech; absent when !caps.tts; D-05 too-long hint present-when-truncated / absent-otherwise; single active utterance on concurrent Speak"
    requirement: WEBVOICE-01
    verification:
      - kind: unit
        ref: "web/src/chat/ExternalStoreChat_messages.speaker.test.tsx"
        status: pass
    human_judgment: false
  - id: D8
    description: "Live visual/UX of the voice-mode toggle + speaker control + too-long hint in the cockpit (BLUE palette, hover-reveal, glyphs, aria-live) against the real backend"
    verification: []
    human_judgment: true
    rationale: "Visual polish + live speaker playback are the 37C-06 Playwright terminal gate (voice.spec.ts against the live container) — unit tests prove behavior in jsdom but cannot render the aesthetic or play audio"
patterns-established:
  - "custom assistant-ui SpeechSynthesisAdapter with a thread-scoped per-text blob cache + dispose()"
  - "context+hook in a non-component module with a disabled default (provider-optional consumer)"
  - "shared, self-tested voiceMocks helper for the whole voice lane"
duration: 55min
completed: 2026-07-09
status: complete
---

# Phase 37C Plan 04: Web Voice-Output Lane Summary

**Built the voice-OUTPUT surface in a net-new `web/src/chat/voice/` module — a one-shot `useVoiceCapabilities` probe, a custom `SpeechSynthesisAdapter` (fetch→blob→`<audio>`, per-text blob cache, `truncated` flag from `X-Aura-TTS-Truncated`, `dispose()` revoke), the `shouldSpeak` OR-parity helper, a shared `voiceMocks`, an ephemeral `VoiceModeProvider` + AppShell header toggle, a `shouldSpeak`-gated `useAutoSpeak`, and a caps.tts-gated `Speak`/`StopSpeaking` control with the D-05 "too long" hint in the assistant ActionBar — all unit-tested (42 tests green), tsc + eslint + i18n-parity clean, and WITHOUT touching the near-cap `ExternalStoreChat.tsx` (37C-05's file).**

## Performance

- **Duration:** ~55 min
- **Completed:** 2026-07-09
- **Tasks:** 3 (all `type="auto" tdd="true"`)
- **Files created/modified:** 16 created + 3 modified (across 3 atomic commits)

## Accomplishments

- **Task 1 — voice/ core module** (`a699fc38c`, `VOICE_MODULE_OK`): `useVoiceCapabilities` (default `{false,false}`, same-origin, fetches once, AbortController-cancelled); `speechAdapter.createSpeechAdapter()` (running→ended(finished/cancelled/error), per-text blob cache so a repeat Speak issues NO second fetch, `truncated` from `X-Aura-TTS-Truncated`, `dispose()`→`revokeObjectURL`); `shouldSpeak` OR predicate; the shared `voiceMocks` (Audio/objectURL/tts-fetch + MediaRecorder/getUserMedia for 37C-05). 26 unit tests.
- **Task 2 — voice mode + auto-speak** (`833d72a74`, `VOICEMODE_OK`): the ephemeral `VoiceModeProvider` (`voiceMode`/`turnWasDictated`/`caps` + toggle/mark/clear, NO persistence), the caps.tts-gated `VoiceModeToggle` in the AppShell chat header, and the `shouldSpeak`-gated `useAutoSpeak` (speaks a new completed reply once, consumes `turnWasDictated`, waits for `isRunning`, never speaks rehydrated history). en+it `chat.voiceMode.*`. 11 unit tests.
- **Task 3 — speaker control + too-long hint** (`0244648ea`, `SPEAKER_CONTROL_OK`): `AssistantSpeakerControl` in the assistant `ActionBarPrimitive.Root` — an `AuiIf`-on-`s.message.speech` `Speak`/`StopSpeaking` toggle gated on `caps.tts`, plus a `role="note"` `t('chat.action.tooLong')` hint that renders ONLY while the active utterance is truncated (making the `X-Aura-TTS-Truncated` header a visible element, not dead code). en+it `chat.action.speak/stopSpeaking/tooLong`. 5 unit tests incl. single-active-utterance.
- **Degrade (WEBVOICE-03):** with `caps.tts=false` the toggle AND the speaker control are absent — proven in both `AppShell.voice.test.tsx` and the speaker test.

## Task Commits

1. **Task 1: voice/ module — useVoiceCapabilities + speechAdapter + shouldSpeak + voiceMocks** — `a699fc38c` (feat, 8 files / +759)
2. **Task 2: VoiceModeProvider + header toggle + useAutoSpeak + voiceMode i18n** — `833d72a74` (feat, 9 files / +546 / −5)
3. **Task 3: caps-gated Speak/StopSpeaking + too-long hint + speaker i18n** — `0244648ea` (feat, 3 files / +265)

**Plan metadata:** (this SUMMARY + STATE + ROADMAP) — final docs commit.

_TDD note: each `tdd="true"` task landed as ONE atomic `feat(...)` commit (impl + its co-located test) per the sequential-executor directive; test-first was honored locally (each symbol was compile-RED before it existed)._

## Files Created/Modified

**Created (voice/ module):**
- `useVoiceCapabilities.ts` / `.test.ts` — one-shot caps probe.
- `speechAdapter.ts` / `.test.ts` — custom `SpeechSynthesisAdapter` (cache + truncated + dispose).
- `shouldSpeak.ts` / `.test.ts` — OR parity predicate.
- `voiceMocks.ts` / `.test.ts` — shared vitest doubles (reused by 37C-05).
- `voiceModeContext.ts` — context + `useVoiceMode` hook + disabled default (non-component module).
- `VoiceModeProvider.tsx` / `.test.tsx` — ephemeral provider.
- `VoiceModeToggle.tsx` — caps.tts-gated header switch.
- `useAutoSpeak.ts` / `.test.tsx` — shouldSpeak-gated auto-speak effect.

**Created (tests at chat/app level):**
- `AppShell.voice.test.tsx`, `ExternalStoreChat_messages.speaker.test.tsx`.

**Modified:**
- `AppShell.tsx` — wrap chat-workspace in `VoiceModeProvider` + mount `VoiceModeToggle` (590 LOC).
- `ExternalStoreChat_messages.tsx` — `AssistantSpeakerControl` in the assistant ActionBar (347 LOC).
- `i18n/resources.ts` — `chat.voiceMode.on/off` + `chat.action.speak/stopSpeaking/tooLong` in BOTH en+it.

## Decisions Made

- **Truncated flag stamped on BOTH the Utterance and every status object.** The stock `base-thread-runtime-core.speak()` copies `utterance.status` into `s.message.speech` by reference, so stamping `truncated` on the status makes the D-05 hint reachable off the speech state in production WITHOUT 37C-05 modifying the runtime — the header is genuinely non-dead, belt-and-suspenders with the top-level `Utterance.truncated`.
- **`useVoiceMode` returns a DISABLED default when no provider is mounted** (context+hook in a non-component `voiceModeContext.ts`, mirroring `sourceExplorerControls.ts`). This satisfies `react-refresh/only-export-components` AND keeps the existing `ExternalStoreChat` tests green (AssistantMessage renders with no provider → speaker absent, no throw).
- **`AssistantSpeakerControl` kept inline (exported) in `ExternalStoreChat_messages.tsx`** so the acceptance grep tokens land there; the isolated speaker test imports it and mocks `@assistant-ui/react` minimally (verified no sibling does top-level assistant-ui destructuring, so module eval is safe).
- **`voiceMocks.ts` self-tested** so the shared helper (whose MediaRecorder/getUserMedia doubles are for 37C-05) doesn't drag the coverage aggregate below 85%.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Completed the local `docx-preview` + `xlsx` install so `tsc --noEmit` runs clean**
- **Found during:** Task 1 (first `tsc --noEmit`)
- **Issue:** `web/node_modules/` was missing `docx-preview` + `xlsx` (both DECLARED in `package.json` and pinned in `package-lock.json`, but not installed) → 5 pre-existing `tsc` errors in unrelated artifact-renderer files (`DocxPreview.tsx`, `XlsxPreview.tsx`, `renderers.test.tsx`), which also blocks the full `npm test`. None of my files erred.
- **Fix:** Ran `npm install` (synced from the EXISTING lockfile — hash `790bf00b…` unchanged before/after, i.e. no dependency drift), installing 14 missing declared packages. Full-tree `tsc --noEmit` is now clean.
- **Files modified:** none tracked (node_modules only; lockfile byte-unchanged).
- **Verification:** `md5sum package-lock.json` identical pre/post; `npx tsc --noEmit` → clean.
- **Committed in:** n/a (no tracked change).

**2. [Rule 3 - Blocking] Three plan-adjacent files added for lint/LOC/coverage compliance**
- **Found during:** Task 2 / Task 1
- **Issue:** (a) exporting `useVoiceMode` from a component `.tsx` trips `react-refresh/only-export-components` under `--max-warnings=0`; (b) AppShell was at 585 LOC with only ~15 of headroom; (c) the shared `voiceMocks.ts` is coverage-included but its dictation doubles aren't exercised by 37C-04's functional tests.
- **Fix:** (a) split the context+hook into a non-component `voiceModeContext.ts` (codebase precedent: `sourceExplorerControls.ts`); (b) extracted `VoiceModeToggle.tsx` — explicitly sanctioned by the plan's `<action>` ("extract a small VoiceModeToggle sibling if the addition risks the cap"); (c) added `voiceMocks.test.ts` to exercise the shared helper.
- **Files modified:** `voiceModeContext.ts`, `VoiceModeToggle.tsx`, `voiceMocks.test.ts` (all new).
- **Verification:** `eslint --max-warnings=0` clean; AppShell 590 ≤ 600; targeted vitest green.
- **Committed in:** `a699fc38c` (voiceMocks.test.ts) / `833d72a74` (voiceModeContext.ts, VoiceModeToggle.tsx).

---

**Total deviations:** 2 auto-fixed (both Rule 3 — blocking env/lint). **Impact:** No content deviation — every artifact, every `<behavior>` case, and all six prohibitions were delivered. The added files are structural (lint/LOC-forced splits the plan anticipated) and test-infra; the npm install completed a pre-existing environmental gap with zero lockfile drift. No architectural (Rule 4) decisions, no auth gates.

## Issues Encountered

- **Foreground `git commit` timed out on the whole-tree file-size pre-commit hook** (>2 min, known issue). Resolved by running each task commit in the background (no `--no-verify`; hooks ran fully). All three landed clean.
- **`rerender(sameElement)` bailed React out of reconciling** in the `useAutoSpeak` test (identical element reference), so the mocked thread state never re-read → the effect never fired. Fixed by rendering a FRESH element per rerender (`makeTree()`).

## Prohibitions Honored

- NO re-fetch of `/api/tts` on a repeat Speak of the same text (per-text blob cache — asserted).
- The `X-Aura-TTS-Truncated` header is NOT dead — surfaced on `Utterance.truncated` + status, RENDERED as `t('chat.action.tooLong')` (asserted present/absent).
- NO persistence of voice mode — `VoiceModeProvider` is ephemeral React state (no `localStorage`, no server pref — asserted).
- NO `adapters.voice` (RealtimeVoiceAdapter) wired — out of scope.
- Every new i18n key lands in BOTH en+it (parity suite green).
- `ExternalStoreChat.tsx` NOT edited (git-confirmed untouched at 595 LOC — 37C-05's sole owner).

## Known Stubs

None introduced. `useVoiceMode`'s disabled default (`caps {false,false}` outside a provider) and `useVoiceCapabilities`'s `{false,false}` default are the intentional WEBVOICE-03 degrade paths (D-06/D-11), not stubs. `turnWasDictated` is plumbed but only SET by 37C-05's dictation adapter (`markTurnDictated`) — that is the documented cross-plan seam (D-09), not a dangling stub: `useAutoSpeak` already consumes it, and the voice-mode toggle path exercises auto-speak independently.

## Verification

All web tests run on the Windows host via Git Bash (node v24.16.0, vitest 4.1.9, tsc 6.0.3) — NOT WSL.

| Check | Result |
|-------|--------|
| Task 1 targeted vitest (`speechAdapter`, `useVoiceCapabilities`, `shouldSpeak`, `voiceMocks`) | 26 pass → `VOICE_MODULE_OK` |
| Task 2 targeted vitest (`VoiceModeProvider`, `useAutoSpeak`, `AppShell.voice`) | 11 pass → `VOICEMODE_OK` |
| Task 3 targeted vitest (`ExternalStoreChat_messages.speaker`) | 5 pass → `SPEAKER_CONTROL_OK` |
| Full lane (`src/chat/voice` + speaker + `AppShell.voice`) | 8 files / 42 tests pass |
| `npx tsc --noEmit` | clean (full tree) |
| `npm run lint` scope (`eslint --max-warnings=0`) | clean on all new/edited files |
| i18n parity (`resources.parity` + `i18n`) | green (en↔it) |
| Regression (`ExternalStoreChat.test`, `AppShell.artifacts`) | unregressed |
| LOC ≤600 | AppShell 590 · _messages 347 · ExternalStoreChat 595 (untouched) |

_Full `npm test` coverage + Stryker mutation is the Wave-6 (37C-06) gate; this plan required targeted unit green + tsc clean, both achieved. The speechAdapter's truncated/dispose/cancel/error branches were written testably for the ≥70% Stryker run in 37C-06._

## Next Phase Readiness

- **37C-05 (web input lane)** is unblocked: it reuses `voiceMocks` (MediaRecorder/getUserMedia doubles), `VoiceModeProvider.markTurnDictated`, and mounts `useAutoSpeak` + wires `speechAdapter` (incl. `dispose()` on unmount) into `ExternalStoreChat.tsx` (which this plan deliberately left untouched at 595 LOC).
- **37C-06** exercises the speaker + toggle live (Playwright `voice.spec.ts`) and runs the coverage/Stryker gate over `speechAdapter`.
- **WEBVOICE-01/03 stay `[ ]`** — phase-spanning; the UI output half lands here, but the full requirement closes with the input lane (37C-05) + the e2e/coverage gate (37C-06). `requirements mark-complete` intentionally NOT run (37C-01/02/03 precedent).

## Self-Check: PASSED

- FOUND: `.planning/phases/37C-web-voice-lane-inserted/37C-04-SUMMARY.md`
- FOUND (created): all 14 `web/src/chat/voice/*` files + `web/src/AppShell.voice.test.tsx` + `web/src/chat/ExternalStoreChat_messages.speaker.test.tsx`
- FOUND (modified): `web/src/AppShell.tsx`, `web/src/chat/ExternalStoreChat_messages.tsx`, `web/src/i18n/resources.ts`
- FOUND: commit `a699fc38c` (Task 1 — voice module, 8 files / +759)
- FOUND: commit `833d72a74` (Task 2 — provider + toggle + auto-speak, 9 files / +546 / −5)
- FOUND: commit `0244648ea` (Task 3 — speaker control + too-long hint, 3 files / +265)
- Verify tokens printed: `VOICE_MODULE_OK`, `VOICEMODE_OK`, `SPEAKER_CONTROL_OK`
- Symbols confirmed: `/api/voice/capabilities` + `same-origin` in useVoiceCapabilities; `/api/tts` + `X-Aura-TTS-Truncated` + `truncated` + `revokeObjectURL` in speechAdapter; `voiceMode || turnWasDictated` in shouldSpeak; `turnWasDictated` + `markTurnDictated` in VoiceModeProvider; `shouldSpeak` in useAutoSpeak; `StopSpeaking` + `chat.action.speak` + `chat.action.tooLong` in ExternalStoreChat_messages; `voiceMode`/`stopSpeaking`/`tooLong` in BOTH en+it of resources.ts.
- Scope guard: `git status` confirms `ExternalStoreChat.tsx` NOT modified.
- `.planning/graphs/.last-build-status.json` left uncommitted per directive.

---
*Phase: 37C-web-voice-lane-inserted*
*Completed: 2026-07-09*
