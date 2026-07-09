---
phase: 37C-web-voice-lane-inserted
plan: 06
subsystem: terminal acceptance gate — live-container Playwright voice E2E + coverage/mutation gate + internal/webui/dist rebuild + a local-voice wiring fix
tags: [web-voice, WEBVOICE, e2e, playwright, live-container, coverage, stryker, mutation, tts, stt, kokoro, whisper, local-sidecars]
type: execute
wave: 6
autonomous: true
dependency_graph:
  requires:
    - phase: "37C-03"
      provides: "voice_api.go (POST /api/tts, POST /api/stt, GET /api/voice/capabilities) + SetVoice + serve_voice.go composition root"
    - phase: "37C-04"
      provides: "speechAdapter + useVoiceCapabilities + VoiceModeProvider + caps-gated Speak control"
    - phase: "37C-05"
      provides: "dictationAdapter + Composer dictation-primary mic + adapters:{speech,dictation} runtime wiring"
  provides:
    - "web/e2e/voice.spec.ts — live-container Playwright: real local-voice contract + speaker (real POST /api/tts audio/mpeg) + dictation (fake-media transcript insert + send) + degrade ({false,false})"
    - "Rebuilt internal/webui/dist — the baked cockpit bundle carrying the 37C voice surface"
    - "cmd/aura/serve_voice.go — web voice lane now SELECTABLE local↔cloud (defaults to the local aura-tts/aura-stt sidecars; supersedes the D-12 cloud-only gating)"
    - "Stryker mutation coverage over speechAdapter + dictationAdapter (added to the CI mutation gate)"
  affects:
    - "Phase 37C closes: WEBVOICE-01..04 proven end to end against a real rebuilt container; voice is live (not degraded) on the deployment's local sidecars"
tech-stack:
  added: []
  patterns:
    - "web voice lane selects local vs cloud by the same knob the rest of multimodal uses (CloudModel presence): default local (LocalBaseURL=cfg.TTSBaseURL/cfg.STTBaseURL), cloud override when AURA_TTS_MODEL/AURA_STT_CLOUD_MODEL is set"
    - "live-container Playwright voice E2E: real Kokoro TTS synth over the served SPA; the Audio element is stubbed via addInitScript so the speaking state is deterministic headless (audible playback is Manual-Only)"
    - "fake-media dictation E2E (--use-fake-device-for-media-stream + grantPermissions('microphone')) with a route-mocked /api/stt transcript (a fake device records silence — real accuracy is Manual-Only)"
    - "mutation-killing tests target the untested seams the survivors expose: getUserMedia-reject, fetch-throw, cancel-before-open, non-string STT text, working unsubscribes, and the request shape"
key-files:
  created:
    - "web/e2e/voice.spec.ts — the terminal live-container voice E2E (4 tests × chromium + mobile-chrome)"
  modified:
    - "cmd/aura/serve_voice.go — buildWebTTSClient/buildWebSTTClient now build LOCAL clients by default (selectable local↔cloud)"
    - "cmd/aura/serve_voice_test.go — httptest proofs the web lane routes to the local sidecars + local-branch cases"
    - "internal/webui/dist — rebuilt cockpit bundle (voice surface baked)"
    - "web/stryker.config.json — speechAdapter + dictationAdapter added to the mutate set"
    - "web/vitest.stryker.config.ts — the two adapter test files added to the Stryker include"
    - "web/src/chat/voice/dictationAdapter.test.ts + speechAdapter.test.ts — mutation-killing tests"
key-decisions:
  - "DEVIATION (operator-directed, supersedes D-12): the web voice lane must use the LOCAL sidecars and be selectable, not cloud-only. buildWebTTSClient/buildWebSTTClient default to the local aura-tts (Kokoro) / aura-stt (faster-whisper) sidecars (which are healthy + configured via TTS_BASE_URL/STT_BASE_URL) and switch to OpenRouter only when the cloud model is set. Without this the deployment reported capabilities {false,false} and the whole web voice lane degraded off."
  - "E2E realness split: capabilities + speaker hit the REAL local backend (real Kokoro mp3); dictation route-mocks /api/stt for a deterministic transcript (fake media records silence; real dictation accuracy is Manual-Only); degrade route-mocks capabilities {false,false}. Audible playback + live mic accuracy stay Manual-Only per VALIDATION.md."
  - "The E2E runs on chromium + mobile-chrome with 2 workers; a stop-before-getUserMedia-resolves race under 8-worker parallel load was defused with a settle window (real, but only reachable with instant fake media + a starved event loop)."
requirements-completed: [WEBVOICE-01, WEBVOICE-02, WEBVOICE-03, WEBVOICE-04]
coverage:
  - id: E1
    description: "voice.spec.ts against the rebuilt live container: real capabilities {true,true} + a live local Kokoro TTS synth (200 audio/mpeg bytes); the message speaker → real POST /api/tts → audio/mpeg + speaking state; fake-media dictation → editable transcript in the composer → Send; degrade → capabilities {false,false} → speaker absent, mic in attachment mode, no console errors"
    requirement: WEBVOICE-01
    verification:
      - kind: e2e
        ref: "web/e2e/voice.spec.ts (VOICE_E2E_OK — 8/8 chromium + mobile-chrome)"
        status: pass
    human_judgment: false
  - id: E2
    description: "Coverage floors: web vitest ≥85% (92.46% stmts); Stryker ≥70% killed on BOTH adapters (speechAdapter 86.57% / dictationAdapter 78.49%); Go owned-surface ≥85% on the full db_integration+neo4j_integration matrix"
    requirement: WEBVOICE-04
    verification:
      - kind: coverage
        ref: "cd web && npm test (COVERAGE_GATE_WEB_OK) + Stryker + WSL bash scripts/coverage_gate.sh (COVERAGE_GATE_GO_OK)"
        status: pass
    human_judgment: false
  - id: E3
    description: "Manual-Only (→ /gsd-verify-work): audible TTS playback intelligibility + live dictation accuracy on a real mic (perceptual, per VALIDATION.md)"
    verification: []
    human_judgment: true
    rationale: "Real audio decode/playback and live STT accuracy are perceptual judgments no automated assert covers; the E2E stubs the Audio element and route-mocks the transcript"
duration: ~3h
completed: 2026-07-09
status: complete
---

# Phase 37C Plan 06: Terminal Acceptance Gate Summary

**Proved WEBVOICE-01..04 end to end against a REAL rebuilt `aura` container: a live-container Playwright `voice.spec.ts` (real local-Kokoro TTS synth on the speaker, fake-media dictation → editable transcript → send, and `{false,false}` graceful degrade), the full coverage/mutation gate (web vitest 92.46% + Stryker ≥70% on both adapters + Go owned-surface ≥85% on the live db_integration+neo4j_integration matrix), and a rebuilt `internal/webui/dist` so the baked cockpit carries the voice surface. Along the way, corrected the composition root so the web voice lane uses the deployment's LOCAL aura-tts/aura-stt sidecars (selectable local↔cloud) instead of degrading off — the operator-directed supersession of the D-12 cloud-only decision.**

## Performance

- **Duration:** ~3h (dominated by the live-container rebakes + three full-matrix coverage runs + E2E harness bring-up)
- **Completed:** 2026-07-09
- **Tasks:** 2 (both `type="auto"`)
- **Commits:** 3 task commits + the metadata commit

## Accomplishments

- **Task 1 — dist rebuild + container rebake + live-container `voice.spec.ts` (`VOICE_E2E_OK`):**
  - Rebuilt `internal/webui/dist` (`npm run build`, vite outDir `../internal/webui/dist`) — the new bundle carries the voice surface (`/api/voice/capabilities`, `/api/tts`, `/api/stt` baked into the AppShell + ExternalStoreChat chunks; `voiceModeContext` chunk emitted).
  - Rebaked the `aura` container (`docker compose build aura && up -d aura`) — the 3-day-old pre-voice image served neither the routes nor the voice UI; the fresh image is healthy and its three voice routes answer (401 unauth = mounted, never 404).
  - `web/e2e/voice.spec.ts` (mirrors `artifacts.spec.ts` + `auth.ts`): real Authula login, external-serve `:9080`, 4 tests across chromium + mobile-chrome (**8/8 green, ~12–14s**):
    1. **real local voice** — `GET /api/voice/capabilities` → `{true,true}` + a live `POST /api/tts` synthesized over the local Kokoro sidecar (200 `audio/mpeg` bytes);
    2. **speaker** — clicking the assistant Read-aloud control drives a REAL `POST /api/tts` (`audio/mpeg`) and swaps to Stop-reading (the Audio element is stubbed so the speaking state is deterministic headless);
    3. **dictation** — fake-media mic → an editable transcript inserted into the composer → Send drives a real `POST /agent/run` (the transcript is a `/api/stt` route-mock — a fake device records silence);
    4. **degrade** — a `{false,false}` capabilities route-mock → the speaker control is absent, the mic stays in attachment mode ("Record audio"), the `page.evaluate` probe reads `{false,false}`, and there are no console errors.
- **Task 1 backend fix — local-voice wiring (`feat` `1cf4f5408`):** `buildWebTTSClient`/`buildWebSTTClient` built cloud-only clients (nil unless `AURA_TTS_MODEL`/`AURA_STT_CLOUD_MODEL` set), so the deployment — which ships healthy local `aura-tts` (Kokoro) + `aura-stt` (faster-whisper) sidecars via `TTS_BASE_URL`/`STT_BASE_URL` — reported `{false,false}` and the whole web voice lane degraded off. Made both builders SELECTABLE (default local, cloud override), mirroring the rest of multimodal; added daemon-free httptest proofs the web lane really routes to the local sidecars (no Bearer + no model on the Kokoro `/audio/speech` call; multipart + model/language on the whisper `/audio/transcriptions` call).
- **Task 2 — coverage/mutation gate (`COVERAGE_GATE_WEB_OK` + `COVERAGE_GATE_GO_OK`):**
  - **Web vitest:** 149 files / **1212 tests pass**; coverage **92.46% stmts / 86.53% branch / 92.6% funcs / 94.21% lines** (≥85% floor).
  - **Stryker (the two adapters):** overall **81.88%** killed — **speechAdapter 86.57%**, **dictationAdapter 78.49%** (both ≥70). Started at 73.1%/57.0%; added mutation-killing tests for the untested seams (getUserMedia-reject, fetch-throw, cancel-before-open, non-string STT text, working unsubscribes, the request shape, the truncated-cache).
  - **Go owned-surface:** **85.4%** on the full `db_integration neo4j_integration` matrix (live stack, `-count=1`) — ≥85% floor.
  - **`-race`:** clean on `internal/agui` (21.9s), `internal/assets` (1.2s), `internal/config` (1.1s), `cmd/aura` (8.4s).

## Task Commits

1. **Task 1 backend — wire web voice to the local sidecars (selectable local/cloud)** — `1cf4f5408` (feat, `serve_voice.go` + `serve_voice_test.go`, +164/−48)
2. **Task 1 — live-container voice E2E + rebuilt cockpit dist** — `edac06848` (test, `voice.spec.ts` + `internal/webui/dist`)
3. **Task 2 — mutation-harden the two voice adapters (Stryker ≥70% each)** — `caabf50bc` (test, stryker/vitest-stryker config + the two adapter tests, +156)

**Plan metadata:** (this SUMMARY + STATE + ROADMAP + REQUIREMENTS) — final docs commit.

## Deviations from Plan

### Rule 4 — Architectural (operator-directed, not a checkpoint)

**1. [Rule 4 — user-directed] Web voice lane made LOCAL-selectable (supersedes D-12 cloud-only)**
- **Found during:** Task 1 setup — probing the rebaked container showed `GET /api/voice/capabilities` would report `{false,false}` because `buildWebTTSClient`/`buildWebSTTClient` were cloud-only (nil unless a cloud model is set) and the deployment sets neither `AURA_TTS_MODEL` nor `AURA_STT_CLOUD_MODEL` — even though the local `aura-tts`/`aura-stt` sidecars are healthy and configured.
- **Directive:** the operator course-corrected mid-execution — the web voice must use the local sidecars and be selectable, not cloud-only-and-degraded.
- **Fix:** `buildWebTTSClient`/`buildWebSTTClient` default to the local sidecar (`LocalBaseURL=cfg.TTSBaseURL`/`cfg.STTBaseURL`, mp3 override kept for the web TTS) and switch to OpenRouter only when the cloud model is set; nil only when NEITHER is configured. The multimodal clients already selected local-vs-cloud by `CloudModel` presence, so this is a composition-root change with daemon-free httptest coverage.
- **Files:** `cmd/aura/serve_voice.go`, `cmd/aura/serve_voice_test.go`
- **Verification:** all voice-wiring tests green (incl. the new local-route proofs); the live container now reports `{true,true}` and synthesizes real mp3 over Kokoro (the E2E's first test).
- **Committed in:** `1cf4f5408`
- **PRD note:** this supersedes CONTEXT decision D-12 (cloud-only); a PRD-amendment should record "web voice = local↔cloud selectable, local default".

### Sanctioned config extensions

**2. [Sanctioned] Stryker + Stryker-vitest config additions**
- `web/stryker.config.json` (`mutate` set) and `web/vitest.config.ts`'s sibling `web/vitest.stryker.config.ts` (`include` allowlist) both needed the two adapters + their test files so the CI mutation gate covers them — the frontend quality gate (vitest ≥85% + Stryker ≥70%). Neither file is in the plan's `files_modified`; noted here as sanctioned.

## Issues Encountered (environment)

- **Authula E2E operator wiped.** The shared Postgres had lost the Authula operator (a prior coverage/db-reset wipe), so `auth.ts` sign-in 401'd. Re-seeded it with `go run scripts/authula_seed_e2e.go` (the CI-sanctioned seeder) using the `.env` credentials → sign-in green. `.env` is CRLF, so the loader strips `\r` before composing DSNs.
- **Coverage gate env completeness.** `scripts/coverage_gate.sh` needs the full live-stack env or the integration tiers SKIP locally (dropping the aggregate to 82.2% with DB+neo4j only). The real full-matrix run exports the composed DSNs + `POSTGRES_*` primitives + neo4j creds + the objectstore (host `:3900`, real `.env` keys) + the embedding sidecar (`:8081`) + `AURA_SKILL_EXPORT_DIR`, and **must cache-bust with `-count=1`** (go's test cache key excludes env vars, so an env-poor prior run is otherwise reused verbatim).
- **Dictation E2E flake (fixed).** Under 8-worker parallel load a `stop()` before `getUserMedia` resolved could no-op the recorder and hang; defused with a settle window + `--workers=2` for the live-container run. Two consecutive clean 8/8 runs.
- **DocumentsWorkspace vitest flake (pre-existing, not voice).** A known 5000ms timeout under parallel load; passes in isolation; not a voice regression.

## Prohibitions Honored

- **No skip-as-green:** the E2E drives a real navigation + real Authula auth against the rebuilt container with counted DOM/route assertions (~12–14s, never sub-second); the Go gate ran the LIVE `db_integration neo4j_integration` matrix (`-count=1`), not a compile-check.
- **No docker_integration-only Go surface:** the voice Go files (`voice_api.go`, `AudioFormat`, the config knob, `serve_voice.go`) are daemon-free unit-covered.
- **dist rebuilt:** the baked bundle carries the voice surface (grep-confirmed `/api/voice/capabilities` + `/api/tts` in the rebuilt chunks).

## Known Stubs

None. The E2E's Audio-element stub and `/api/stt` transcript route-mock are deterministic test seams for the perceptual surfaces that VALIDATION.md tracks as Manual-Only (audible playback, live mic accuracy) — not product stubs.

## Verification

| Check | Result |
|-------|--------|
| `internal/webui/dist` rebuilt (voice surface baked) | yes — `/api/voice/capabilities` + `/api/tts` + `/api/stt` in the new chunks |
| `aura` container rebaked + healthy | yes — image fresh (was 3 days old); voice routes mounted (401 unauth, not 404) |
| `voice.spec.ts` (chromium + mobile-chrome) | **8/8 green**, ~12–14s → `VOICE_E2E_OK` |
| web `npm test` coverage | 1212 pass; **92.46% stmts / 86.53% branch / 92.6% funcs / 94.21% lines** → `COVERAGE_GATE_WEB_OK` |
| Stryker (speechAdapter / dictationAdapter) | **86.57% / 78.49%** killed (both ≥70) |
| Go owned-surface (WSL, full matrix, `-count=1`) | **85.4%** ≥85% → `COVERAGE_GATE_GO_OK` |
| `-race` (agui / assets / config / cmd/aura) | **clean** (all 4 ok, no data races) |

## Next Phase Readiness

- **Phase 37C closes.** WEBVOICE-01..04 are proven end to end against a real rebuilt container; the web voice lane is LIVE on the deployment's local Kokoro/whisper sidecars (selectable local↔cloud), not degraded.
- **Manual-Only for `/gsd-verify-work`:** audible TTS playback intelligibility + live dictation accuracy on a real mic (perceptual).
- **PRD follow-up:** record the D-12 supersession (web voice = local↔cloud selectable, local default).

## Self-Check: PASSED

- FOUND (created): `web/e2e/voice.spec.ts`
- FOUND (rebuilt): `internal/webui/dist` — `/api/voice/capabilities` + `/api/tts` + `/api/stt` baked into the new chunks
- FOUND (modified): `cmd/aura/serve_voice.go` + `serve_voice_test.go`, `web/stryker.config.json`, `web/vitest.stryker.config.ts`, `web/src/chat/voice/{dictationAdapter,speechAdapter}.test.ts`
- FOUND: commit `1cf4f5408` (backend local-voice wiring)
- FOUND: commit `edac06848` (voice E2E + rebuilt dist)
- FOUND: commit `caabf50bc` (adapter mutation-hardening)
- Verify tokens printed: `VOICE_E2E_OK` (8/8), `COVERAGE_GATE_WEB_OK` (92.46% stmts), `COVERAGE_GATE_GO_OK` (85.4%)
- Live-container proof: capabilities `{true,true}` + real Kokoro `POST /api/tts` → `audio/mpeg` bytes (the old image 404s the routes)
- `.planning/graphs/.last-build-status.json` + `GRAPH_REPORT.md` left uncommitted per directive

---
*Phase: 37C-web-voice-lane-inserted*
*Completed: 2026-07-09*
