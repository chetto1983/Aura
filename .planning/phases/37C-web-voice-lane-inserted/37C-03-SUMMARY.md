---
phase: 37C-web-voice-lane-inserted
plan: 03
subsystem: agui voice API + cmd/aura composition-root wiring (daemon-free)
tags: [web-voice, WEBVOICE, tts, stt, capabilities, narrow-seams, mp3-web, cloud-only, daemon-free-tests, backend-spine]
type: execute
wave: 3
autonomous: true
dependency_graph:
  requires:
    - "37C-02 (config.Config.TTSMaxChars default 4096 + exported ;codecs=-safe assets.AudioFormat)"
  provides:
    - "POST /api/tts — text→audio/mpeg, soft rune char-cap + X-Aura-TTS-Truncated header; 503 when unconfigured; never persists"
    - "POST /api/stt — multipart audio→transcript, transcribe-and-DISCARD (no asset/Garage/DB row); ;codecs=→format map; clean 200 {\"text\":\"\"} on empty transcript"
    - "GET /api/voice/capabilities — SELF-scoped {tts,stt} presence probe, 200 even unconfigured (never 503)"
    - "agui.Server.SetVoice(ttsSynthesizer, sttTranscriber, maxChars) — the D-13 injection seam"
    - "cmd/aura buildWebTTSClient (Format=mp3) / buildWebSTTClient (cloud-only) / wireVoiceProviders + registerVoiceRoutes parent-mux mounts"
  affects:
    - "37C-04 (web output lane: useVoiceCapabilities + speechAdapter consume /api/voice/capabilities + /api/tts)"
    - "37C-05 (web input lane: dictationAdapter consumes /api/stt)"
    - "37C-06 (terminal e2e gate exercises the three routes live)"
tech_stack:
  added: []
  patterns:
    - "consumer-side narrow interface seams (ttsSynthesizer/sttTranscriber) mirroring settingsStore/auditReader — handlers unit-test with fakes, no network"
    - "dedicated mp3 web TTSClient instance (TTSConfig.Format=mp3) instead of a per-call format arg — Telegram opus client untouched (RESEARCH Landmine #2, smallest blast radius)"
    - "typed-nil-safe interface injection: an absent concrete client passed to SetVoice as an untyped-nil literal (never a typed-nil *pointer) so presence reports false, not a nil-wrapped non-nil interface"
    - "transcribe/synthesize-and-discard (D-08): the audio never touches the asset service — proven by a recording fakeAssetService asserted untouched"
    - "refactor-on-touch extraction (serve_webui_voice.go beside serve_webui_musr.go; seedSkillTTLSweep moved to serve_provisioning.go) to hold the 600-LOC cap"
key_files:
  created:
    - "internal/agui/voice_api.go (3 handlers + ttsSynthesizer/sttTranscriber seams + SetVoice + registerVoiceRoutes, commit 0d418a1a3)"
    - "internal/agui/voice_api_test.go (daemon-free suite, 0d418a1a3 + branch coverage 3dfbcf3e0)"
    - "cmd/aura/serve_voice.go (wireVoiceProviders + buildWebTTSClient + buildWebSTTClient, commit f74bfce63)"
    - "cmd/aura/serve_voice_test.go (mp3/opus-untouched/cloud-only/degraded/all-switch-arms, f74bfce63)"
    - "cmd/aura/serve_webui_voice.go (route consts + registerVoiceRoutes parent-mux mounts, f74bfce63)"
    - ".planning/phases/37C-web-voice-lane-inserted/37C-03-SUMMARY.md"
  modified:
    - "internal/agui/server.go (tts/stt/ttsMaxChars fields + s.registerVoiceRoutes(mux) in Mux(), 0d418a1a3)"
    - "cmd/aura/serve.go (wireVoiceProviders(aguiServer, chat.cfg) after NewServer; seedSkillTTLSweep moved out, f74bfce63)"
    - "cmd/aura/serve_webui.go (registerVoiceRoutes(mux, aguiHandler, auth) beside registerMUSRRoutes, f74bfce63)"
    - "cmd/aura/serve_provisioning.go (seedSkillTTLSweep co-located with its sibling seed helpers, f74bfce63)"
    - ".planning/STATE.md (tracking)"
    - ".planning/ROADMAP.md (37C-03 checkbox)"
decisions:
  - "mp3-for-web is a DEDICATED web TTSClient instance with TTSConfig.Format=mp3 (NOT a per-call format arg — Synthesize has none, tts.go:60); the Telegram opus client (multimodalConfig) is untouched — RESEARCH Landmine #2, smallest blast radius"
  - "SetVoice depends on the narrow ttsSynthesizer/sttTranscriber seams (not the concrete *multimodal clients) so the handlers unit-test with fakes and no network — the settingsStore/auditReader precedent"
  - "cloud-only (D-12): buildWebTTSClient/buildWebSTTClient return nil unless the cloud model is set; an absent client is injected to SetVoice as an untyped-nil literal so the capability reports false and the POST 503s — never a typed-nil-wrapped non-nil interface (which would report true and panic on first call)"
  - "GET /api/voice/capabilities is 200 {tts,stt} even when unconfigured ({false,false}) — never 503 (D-11); the two POSTs 503 on a nil client. All three require a principal (asset-handler shape); the production 401 comes from the whole-mux RequireAuth"
  - "empty STT transcript is a clean 200 {\"text\":\"\"}, never a 5xx (RESEARCH Nyquist edge); the audio is discarded either way (no asset written)"
  - "refactor-on-touch (CLAUDE.md ≤600): serve.go was at the cap, so the +7-line wiring forced seedSkillTTLSweep to move to serve_provisioning.go beside its sibling seed helpers (serve.go 607→572); the voice route mounts live in a new serve_webui_voice.go (serve_webui.go stays 551)"
  - "single atomic feat commit per task (impl+test together) per the sequential-executor directive, + one follow-up test(...) commit hardening the owned-surface coverage floor"
metrics:
  tasks_completed: 2
  duration: "~55 min"
  completed: "2026-07-09"
  files_changed: 9
  commits: ["0d418a1a3", "f74bfce63", "3dfbcf3e0"]
requirements_touched: [WEBVOICE-01, WEBVOICE-02, WEBVOICE-03, WEBVOICE-04]
requirements_completed: []
---

# Phase 37C Plan 03: Web Voice API Backend Spine Summary

**One-liner:** Built the three identity-scoped voice handlers the web lanes (37C-04/05) and the e2e (37C-06) consume — `POST /api/tts` (text→`audio/mpeg` with a rune-safe `AURA_TTS_MAX_CHARS` cap + `X-Aura-TTS-Truncated` header), `POST /api/stt` (transcribe-and-**discard**: no asset/Garage/DB row, `;codecs=`-stripped container map, a clean `200 {"text":""}` on an empty transcript), and `GET /api/voice/capabilities` (SELF-scoped `{tts,stt}`, 200 even unconfigured) — over narrow `ttsSynthesizer`/`sttTranscriber` seams injected by a new `SetVoice`, plus the composition root that builds a **dedicated mp3 web `TTSClient`** (Telegram opus untouched) + a **cloud-only `STTClient`** and mounts the routes — all proven by daemon-free unit + `-race` suites.

## What Was Built

Two `type="auto" tdd="true"` tasks, each committed atomically after WSL verification, plus one coverage-hardening `test(...)` commit.

### Task 1 — `internal/agui/voice_api.go`: 3 handlers + seams + `SetVoice` (commit `0d418a1a3`, verify token `VOICE_API_OK`)

- **Narrow seams (consumer-side, D-A2-02):** `ttsSynthesizer` (`Synthesize(ctx,text) ([]byte,error)` + `AudioFormat() string`) and `sttTranscriber` (`Transcribe(ctx,audio,fileName,format) (string,error)`) — `*multimodal.TTSClient`/`*multimodal.STTClient` satisfy them structurally, so the handlers unit-test with fakes and no network. `SetVoice(tts, stt, maxChars)` stores them on the `Server` (new `tts`/`stt`/`ttsMaxChars` fields in `server.go`); `s.registerVoiceRoutes(mux)` wired into `Mux()` beside `s.registerAssetRoutes(mux)`.
- **`handleTTS`:** nil `s.tts`→503; no principal→401; empty text→400; else the text is rune-capped at `ttsMaxChars` (rune-safe prefix via `capText`, an `X-Aura-TTS-Truncated: true` header when it overflowed, D-05) and synthesized; the bytes are written `Content-Type: audio/mpeg` + `X-Content-Type-Options: nosniff`. A `Synthesize` error is a 502. **Never persists.**
- **`handleSTT`:** nil `s.stt`→503; no principal→401; reads the `audio` multipart part (body capped 25 MiB), maps its part `Content-Type` via `assets.AudioFormat` (strips `;codecs=` → `audio/webm;codecs=opus`→`webm`), transcribes, returns `{"text":...}`. A `Transcribe` error is a 502; a missing part is a 400. **NEVER calls the asset service** — no `assets.Asset`, no Garage object, no DB row, no async poll (D-08). An **empty transcript is a clean `200 {"text":""}`**, not a 5xx.
- **`handleVoiceCapabilities`:** no principal→401; else `200 {tts: s.tts != nil, stt: s.stt != nil}` — **NEVER 503** (D-11), so an unconfigured stack reports `{false,false}` cleanly.
- **`voice_api_test.go`** (daemon-free): owner-200 + `audio/mpeg` + body + `nosniff`; unauth-401 (RequireAuth); degrade-503; char-cap table (under/at/over→prefix+truncated header/empty→400); STT owner (`;codecs=opus`→`webm`) + **no-persist proof** (recording `fakeAssetService` asserted untouched); STT empty-transcript clean 200; capabilities table ({true,true}/{true,false}/{false,true}/**{false,false}**) + 401. Plus (commit `3dfbcf3e0`) the error/401/cap branches: synth-error 502, invalid-JSON 400, transcribe-error 502 (asset still untouched), missing-part 400, handler-own no-principal 401s, and a direct `TestCapText` incl. `maxChars<=0` + a rune-safe multibyte prefix.

### Task 2 — `cmd/aura` composition-root wiring (commit `f74bfce63`, verify token `VOICE_WIRING_OK`)

- **`serve_voice.go`:** `buildWebTTSClient(cfg)` builds the **dedicated mp3 web `TTSClient`** (`TTSConfig.Format="mp3"`, `CloudModel=cfg.TTSModel`, over the shared `LLM.BaseURL/APIKey`) — cloud-only, `nil` when `TTSModel==""`; `buildWebSTTClient(cfg)` builds the cloud-only `STTClient` (`CloudModel=cfg.STTCloudModel`), `nil` when unset (D-12). `wireVoiceProviders` injects them via `SetVoice`, passing an **untyped-nil literal** for an absent client (never a typed-nil `*multimodal` pointer) so presence reports correctly.
- **`serve_webui_voice.go`** (mirrors `serve_webui_musr.go`): `ttsRoute`/`sttRoute`/`voiceCapabilitiesRoute` consts + `registerVoiceRoutes(mux, aguiHandler, auth)` — the two cost-bearing POSTs behind `RequireCapability(agentRunCapability)`, capabilities bare `aguiHandler` (RequireAuth-only, like `meRoute`).
- **Wiring:** `wireVoiceProviders(aguiServer, chat.cfg)` after `NewServer` in `serve.go`; `registerVoiceRoutes(mux, aguiHandler, auth)` beside `registerMUSRRoutes` in `serve_webui.go`.
- **`serve_voice_test.go`:** web TTS `AudioFormat()=="mp3"`; opus-untouched (`multimodalConfig(cfg).TTSFormat=="opus"` coexists); cloud-only STT gating (`nil`/`Cloud()`); degraded `{false,false}`; and **all `wireVoiceProviders` switch arms proven end-to-end** through the real `GET /api/voice/capabilities` (a tts-only wire reports `stt:false` — the typed-nil guard, not a nil-wrapped `true`). The round-trip stamps a principal via `identityctx` (the fallback `principalFrom` reads), so no live session is needed.

## Verification

All commands run in WSL against `/mnt/d/Aura` (Windows AV blocks native `.test.exe`); go1.26.5. These handler/wiring tests are daemon-free (`net/http/httptest`) — no Docker/DSN.

| Check | Task 1 (`internal/agui`) | Task 2 (`cmd/aura`) |
|-------|--------------------------|---------------------|
| `go fmt` (touched) | clean | clean |
| `go vet` | clean | clean |
| targeted `go test` | `VOICE_API_OK` (`-run 'TestTTS\|TestSTT\|TestVoiceCapabilities'`, all subtests) | `VOICE_WIRING_OK` (`-run 'WireVoice\|Voice'`, all subtests) |
| full package unit | `ok` 6.79s | `ok` 1.34s |
| `go build ./...` | — | clean |
| `go test -race` (CGO_ENABLED=1) | PASS (voice run) | PASS (voice run) |
| pre-commit hooks | gofmt+vet+lint(0)+file-size(≤600) green | same, green |

**Coverage (daemon-free func-level on `voice_api.go`, owned-surface floor contribution):** `SetVoice` 100% · `registerVoiceRoutes` 100% · `capText` 100% · `handleVoiceCapabilities` 100% · **`handleTTS` 96.3%** · **`handleSTT` 91.3%** — the only uncovered lines are the rare read-error guards (`readCappedBody`/`io.ReadAll` failure), single-line defensive returns needing a faulty reader. `cmd/aura/serve_voice.go`: `wireVoiceProviders`/`buildWebTTSClient`/`buildWebSTTClient` all **100%**. `voice_api.go` sits comfortably above the ≥85% owned-surface bar; the full-matrix (`db_integration neo4j_integration`, live stack) aggregate gate is the wave-boundary/CI must-run and was not run on this Windows host (honestly carried forward — no daemon-gated surface was introduced, so this file only raises the aggregate).

**LOC (≤600 cap):** `voice_api.go` 190 · `server.go` 520 · `serve_voice.go` 79 · `serve_webui_voice.go` 39 · `serve.go` 572 (was 607 pre-refactor) · `serve_webui.go` 551 · `serve_provisioning.go` 487.

## Deviations from Plan

**One refactor-on-touch (Rule 3 / CLAUDE.md ≤600), no content deviation.** The +7-line `wireVoiceProviders` wiring pushed `cmd/aura/serve.go` from 600 to **607 LOC** (over the hard cap, which the file-size hook enforces by raw `wc -l`). Per CLAUDE.md "deep refactor on touch," `seedSkillTTLSweep` (~35 lines) was moved out of `serve.go` into `cmd/aura/serve_provisioning.go`, **co-located with its two sibling seed helpers** (`seedIdentityPurgeSweep`/`seedSandboxReapSweep`) — a clean concern grouping, zero behavior change (same package, all references intact, full `cmd/aura` unit + `-race` green after the move). Net: `serve.go` 607→**572**, `serve_provisioning.go` 450→**487**. The route mounts already landed in the new `serve_webui_voice.go` per the plan (so `serve_webui.go` stays 551).

All artifacts and every `<behavior>` case were delivered; all six prohibitions respected: STT never persists (no `s.assets` reference in the handler — grep-confirmed); mp3 is a dedicated web client, Telegram opus untouched; capabilities never 503s; empty transcript is a clean 200; the handler struct depends on the narrow seams, not concrete `*multimodal` clients; `voice_api.go`/`serve_webui.go` both ≤600.

No auto-fix bug/missing-functionality rules fired beyond the coverage hardening (Rule 2 — the owned-surface floor); no architectural decisions; no authentication gates.

## TDD Gate Compliance

Both tasks carry `tdd="true"`, but this run's sequential-executor directive mandates **implement → verify (WSL) → commit** as ONE atomic commit per task. Each task landed as a single `feat(37C): …` commit (impl **and** its test), rather than a split `test(…)` RED → `feat(…)` GREEN pair. Test-first discipline was honored locally — each new symbol (`SetVoice`, `handleTTS`, `wireVoiceProviders`, …) did not exist when its test was written, so the test was compile-RED before implementation — but RED and GREEN were committed together. A third `test(37C): …` commit (`3dfbcf3e0`) subsequently hardened the owned-surface coverage floor (error/401/cap branches). This is the disclosure the gate requires.

## Requirements

`WEBVOICE-01..04` are **phase-spanning**. This plan delivers the **backend spine** (the three routes, the mp3-vs-opus split, cloud-only degrade, `RequireAuth`, ≥85% daemon-free coverage) that the requirements build on, but the full features land across the remaining lanes: the speaker control (`WEBVOICE-01`) in 37C-04, the Composer dictation (`WEBVOICE-02`) in 37C-05, the UI degrade half of `WEBVOICE-03` in 37C-04, and the e2e + coverage/Stryker gate (`WEBVOICE-04`) in 37C-06. They remain `[ ]` and `requirements mark-complete` was intentionally **NOT** run, matching the 37C-01/37C-02/37B precedent.

## Known Stubs

None introduced. The three handlers, `SetVoice`, and the composition-root wiring are complete and fully wired end-to-end (`wireVoiceProviders(aguiServer, chat.cfg)` → `SetVoice` → the live routes on `Server.Mux`), exercised by daemon-free tests. No hardcoded empty values flow to a consumer; the capabilities `{false,false}` degrade path is intentional (D-11), not a stub.

## Next

Wave 4 — **37C-04** (web output lane: `useVoiceCapabilities` hook + `speechAdapter` + `VoiceModeProvider`/auto-speak + the caps-gated Speak/StopSpeaking control), which consumes `GET /api/voice/capabilities` + `POST /api/tts` from this plan's backend spine.

## Self-Check: PASSED

- FOUND: `.planning/phases/37C-web-voice-lane-inserted/37C-03-SUMMARY.md`
- FOUND (created sources): `internal/agui/voice_api.go`, `internal/agui/voice_api_test.go`, `cmd/aura/serve_voice.go`, `cmd/aura/serve_voice_test.go`, `cmd/aura/serve_webui_voice.go`
- FOUND (modified sources): `internal/agui/server.go`, `cmd/aura/serve.go`, `cmd/aura/serve_webui.go`, `cmd/aura/serve_provisioning.go`
- FOUND: commit `0d418a1a3` (Task 1 — voice API handlers + seams + SetVoice, 3 files / +583)
- FOUND: commit `f74bfce63` (Task 2 — mp3 web providers + parent-mux mounts, 6 files / +292 / -35)
- FOUND: commit `3dfbcf3e0` (coverage — voice branch tests, 1 file / +146)
- Verify tokens printed: `VOICE_API_OK`, `VOICE_WIRING_OK`; `-race` PASS on both packages; `go build ./...` clean.
- Symbols confirmed: `registerVoiceRoutes` + `func (s *Server) SetVoice` in voice_api.go; `registerVoiceRoutes` in server.go (Mux) + serve_webui.go (mount); `audio/mpeg` + `X-Aura-TTS-Truncated` in voice_api.go; `Format: "mp3"` + `SetVoice` in serve_voice.go; no `s.assets` reference in handleSTT (no-persist).
- No unintended deletions (seedSkillTTLSweep moved, not dropped — present in serve_provisioning.go); `.planning/graphs/.last-build-status.json` + `GRAPH_REPORT.md` left uncommitted per directive.
