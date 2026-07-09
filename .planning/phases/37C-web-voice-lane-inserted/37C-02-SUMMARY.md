---
phase: 37C-web-voice-lane-inserted
plan: 02
subsystem: config + assets (daemon-free backend primitives)
tags: [config-knob, audio-format, web-voice, WEBVOICE, backend-foundation, daemon-free-tests]
type: execute
wave: 2
autonomous: true
dependency_graph:
  requires:
    - "37C-01 (prd.md Amendment #79 — the PRD-first D-14 gate)"
  provides:
    - "config.Config.TTSMaxChars int (default 4096) — the POST /api/tts soft char-cap value"
    - "assets.AudioFormat (EXPORTED, ;codecs=-safe) — the POST /api/stt container→STT-format map"
  affects:
    - "37C-03 (POST /api/tts + POST /api/stt handlers depend_on both primitives)"
tech_stack:
  added: []
  patterns:
    - "knob-registry KindInt row (D-08 'the registry IS the engine' — reparse pass validates for free)"
    - "media-type parameter strip (bare type before the first ';', trimmed) before an exact-string switch"
key_files:
  created:
    - ".planning/phases/37C-web-voice-lane-inserted/37C-02-SUMMARY.md"
  modified:
    - "internal/config/config.go (TTSMaxChars field + loadBase loader, commit 646b99539)"
    - "internal/config/config_knobs.go (AURA_TTS_MAX_CHARS KindInt registry row, 646b99539)"
    - "internal/config/config_test.go (TTSMaxChars default+override test + clearPostgresEnv key, 646b99539)"
    - "internal/assets/audio_processor.go (audioFormat → exported ;codecs=-safe AudioFormat + caller, c7f45a359)"
    - "internal/assets/audio_processor_test.go (TestAudioFormat table incl. codecs cases, c7f45a359)"
    - ".planning/STATE.md (tracking)"
    - ".planning/ROADMAP.md (37C-02 checkbox)"
decisions:
  - "AURA_TTS_MAX_CHARS default = 4096 (the OpenAI-compatible /audio/speech provider ceiling OpenRouter proxies; a longer input hard-400s) — the ceiling is the lock, never exceeded"
  - "The knob is a KindInt registry row so the kind-driven reparse pass flags a malformed value Fatal under a strict tier for free; TestKnobRegistry structural invariants + the rapid property tests absorb it with no new assertions"
  - "AudioFormat strips the ;codecs= media-type parameter before the unchanged exact-string switch — no new container entries needed (RESEARCH Q4); bare-MIME behavior + the ogg default are byte-unchanged"
  - "Single atomic commit per task (implementation + its test together) per the sequential-executor directive (implement→verify→commit); test-first discipline honored locally (each new symbol was compile-RED before it existed), RED+GREEN landed atomically — see TDD Gate Compliance"
metrics:
  tasks_completed: 2
  duration: "~35 min"
  completed: "2026-07-09"
  files_changed: 5
  commits: ["646b99539", "c7f45a359"]
requirements_touched: [WEBVOICE-01, WEBVOICE-02]
requirements_completed: []
---

# Phase 37C Plan 02: Web Voice Backend Foundation Summary

**One-liner:** Landed the two daemon-free backend primitives 37C-03's voice API depends on — the `AURA_TTS_MAX_CHARS` int knob (default 4096, the OpenAI `/audio/speech` provider ceiling) as a first-class config field + registry row, and an EXPORTED, `;codecs=`-safe `assets.AudioFormat` that maps `audio/webm;codecs=opus`→`webm` and `audio/mp4;codecs=mp4a.40.2`→`m4a` instead of the `ogg` default — both fully exercised by new unit tests with no daemon.

## What Was Built

Two `type="auto" tdd="true"` tasks, each committed atomically after WSL verification.

### Task 1 — `AURA_TTS_MAX_CHARS` config knob (commit `646b99539`, verify token `CONFIG_KNOB_OK`)

- **`config.Config.TTSMaxChars int`** field added in the multimodal block beside `MultimodalTimeoutSec`, with a doc-comment recording `AURA_TTS_MAX_CHARS` + the 4096 provider ceiling.
- **Loader line** `TTSMaxChars: envutil.IntDefault("AURA_TTS_MAX_CHARS", 4096)` in `loadBase()` — so the value is populated by **both** `Load()` and `LoadDB()` (the loader is the shared builder).
- **Registry row** `{Name: "AURA_TTS_MAX_CHARS", Kind: KindInt, Default: "4096"}` appended to the Tier B block of `knobRegistry()` (config_knobs.go). Because it is `KindInt`, the generic kind-driven reparse pass now flags a malformed value `Fatal` under a strict tier with zero per-knob code (D-08).
- **Test** `TestTTSMaxChars_DefaultAndOverride` (unset→4096, `AURA_TTS_MAX_CHARS=1200`→1200), plus the knob added to `clearPostgresEnv` so the default assertion is hermetic and the existing rapid property tests (`TestRapidEnvStrictness` / `…NoFalsePositive` / `…AggregationMonotonic`) pick it up automatically.

### Task 2 — exported `;codecs=`-safe `assets.AudioFormat` (commit `c7f45a359`, verify token `AUDIOFORMAT_OK`)

- **Renamed** unexported `audioFormat` → exported **`AudioFormat(mimeType string) string`**; its single caller in `ProcessAsset` updated in the same edit; `strings` added to imports.
- **Codecs strip:** the function now takes the media type before the first `;` and `strings.TrimSpace`s it before the existing exact-string switch. A browser `MediaRecorder` blob reports `audio/webm;codecs=opus` (Chrome/Firefox) or `audio/mp4;codecs=mp4a.40.2` (Safari); previously both fell through to the `ogg` default and mis-tagged the container the cloud STT JSON route needs. **No new container entries** — the bare `audio/webm`/`audio/mp4` cases suffice once stripped (RESEARCH Q4). The `ogg` default and every bare-MIME mapping are byte-unchanged.
- **Test** `TestAudioFormat` — a 14-row table: `audio/webm;codecs=opus`→`webm`, `audio/mp4;codecs=mp4a.40.2`→`m4a`, whitespace-around-`;` (`audio/webm ; codecs=opus`→`webm`), every bare alias (mpeg/mp3/wav/x-wav/webm/flac/mp4/m4a/x-m4a), empty→`ogg`, and `application/octet-stream`→`ogg`.

## Verification

All commands run in WSL against `/mnt/d/Aura` (Windows AV blocks native `.test.exe`); go1.26.5.

| Check | Task 1 (config) | Task 2 (assets) |
|-------|-----------------|-----------------|
| `gofmt -l` (touched files) | clean | clean |
| `go vet` | clean | clean |
| targeted `go test` | `CONFIG_KNOB_OK` (`-run 'TTSMaxChars\|Load\|Knob'`) | `AUDIOFORMAT_OK` (14/14 subtests, `-run TestAudioFormat`) |
| `go build ./...` | `BUILD_OK` | `BUILD_OK` |
| `go test -race` (CGO_ENABLED=1) | `RACE_CONFIG_OK` | `RACE_ASSETS_OK` |
| pre-commit hooks | gofmt+vet+lint(0 issues)+file-size(≤600) green | same, green |

**Coverage of the new symbols (daemon-free, owned-surface floor contribution):**
- `AudioFormat` = **100.0%** of statements (function-level, via `TestAudioFormat` alone — every switch arm + the strip path).
- `loadBase`/`Load`/`LoadDB` (which populate `TTSMaxChars`) = **100.0%**.
- Combined untagged: `internal/config` **93.1%**; `internal/assets` 64.7% untagged — the sub-85% assets number is entirely the daemon-gated store/processor paths (`db_integration`/`garage_integration` object-store + live-sidecar processors) that do not run without their tags; the two symbols this plan adds are pure logic at 100%, so they RAISE the tagged owned-surface aggregate, never lower it. No `docker_integration`-only surface introduced.

Combined `go vet ./internal/config/ ./internal/assets/` + `go test -cover` both green (`COMBINED_OK`).

## Deviations from Plan

**None to the plan's content.** Both artifacts and every `<behavior>` case were delivered exactly as specified; all three prohibitions were respected:
- **No new container entries** added to `AudioFormat` (the `;codecs=` strip makes bare `audio/webm`+`audio/mp4` sufficient).
- **The `ogg` default and existing bare-MIME mappings unchanged** — only the strip + export + the knob were added.
- **The 4096 default was not exceeded** (it is the exact provider ceiling).

No auto-fix rules fired (no bugs, missing functionality, or blockers encountered); no architectural decisions needed; no authentication gates.

## TDD Gate Compliance

The two tasks carry `tdd="true"`, but this run's sequential-executor directive mandates **implement → verify (WSL) → commit** as ONE atomic commit per task. Consequently each task landed as a single `feat(37C): …` commit containing implementation **and** its test, rather than a separate `test(…)` RED commit followed by a `feat(…)` GREEN commit. Test-first discipline was honored locally — each new symbol (`TTSMaxChars`, the exported `AudioFormat`) did not exist when its test was written, so the test was compile-RED before implementation — but RED and GREEN were committed together. No separate `test(…)` gate commit exists in `git log` by design; this is the disclosure the gate requires.

## Requirements

`WEBVOICE-01` / `WEBVOICE-02` are **phase-spanning** — this plan delivers only their backend primitives (the char-cap value and the container→format map), not the full features (handlers 37C-03, adapters/UI 37C-04..05). They remain `[ ]` and `requirements mark-complete` was intentionally NOT run, matching the 37C-01 / 37B precedent.

## Known Stubs

None introduced. Both symbols are complete, wired (loader + single caller), and unit-tested. (`config.Config.TTSMaxChars` has no consumer YET — that is 37C-03's `SetVoice`/`handleTTS` — but it is a fully-loaded config value, not a placeholder.)

## Next

Wave 3 — **37C-03** (voice API: `POST /api/tts` + `POST /api/stt` + `GET /api/voice/capabilities` handlers + `SetVoice` + the mp3 web `TTSClient` wiring), which `depends_on` both primitives this plan provides.

## Self-Check: PASSED

- FOUND: `.planning/phases/37C-web-voice-lane-inserted/37C-02-SUMMARY.md`
- FOUND (all 5 modified sources): `internal/config/config.go`, `internal/config/config_knobs.go`, `internal/config/config_test.go`, `internal/assets/audio_processor.go`, `internal/assets/audio_processor_test.go`
- FOUND: commit `646b99539` (Task 1 — config knob, 3 files / +21 / -1)
- FOUND: commit `c7f45a359` (Task 2 — AudioFormat export, 2 files / +48 / -5)
- Symbols confirmed: `AURA_TTS_MAX_CHARS` ×2 in config.go (field + loader) + ×1 in config_knobs.go (registry row); `func AudioFormat` ×1 (exported); `AudioFormat(asset.MIMEType)` ×1 (caller updated); no `func audioFormat` lowercase definition remains in any Go source.
- Verify tokens printed: `CONFIG_KNOB_OK`, `AUDIOFORMAT_OK`; `-race` PASS on both packages (`RACE_CONFIG_OK` / `RACE_ASSETS_OK`); `go build ./...` `BUILD_OK`.
- No unintended deletions; no out-of-scope file touched (`.planning/graphs/.last-build-status.json` left uncommitted per directive).
