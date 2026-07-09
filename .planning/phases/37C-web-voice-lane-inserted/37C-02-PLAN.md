---
phase: 37C-web-voice-lane-inserted
plan: 02
type: execute
wave: 2
depends_on: ["37C-01"]
files_modified:
  - internal/config/config.go
  - internal/config/config_knobs.go
  - internal/config/config_test.go
  - internal/assets/audio_processor.go
  - internal/assets/audio_processor_test.go
autonomous: true
requirements: [WEBVOICE-01, WEBVOICE-02]
must_haves:
  truths:
    - "config.Config carries TTSMaxChars loaded from AURA_TTS_MAX_CHARS with default 4096 (D-05 — the OpenAI /audio/speech 4096-char ceiling)"
    - "AURA_TTS_MAX_CHARS is a KindInt row in the knob registry (default 4096)"
    - "assets.AudioFormat is EXPORTED and strips the ;codecs= media-type parameter before mapping, so audio/webm;codecs=opus → webm and audio/mp4;codecs=mp4a.40.2 → m4a (not the ogg default)"
    - "the single existing caller (audio_processor.go ProcessAsset) uses the exported AudioFormat with unchanged behavior for bare MIME types"
  artifacts:
    - path: "internal/config/config.go"
      provides: "TTSMaxChars field + AURA_TTS_MAX_CHARS loader (envutil.IntDefault default 4096)"
      contains: "AURA_TTS_MAX_CHARS"
    - path: "internal/assets/audio_processor.go"
      provides: "exported codecs-strip AudioFormat map"
      contains: "func AudioFormat"
    - path: "internal/assets/audio_processor_test.go"
      provides: "TestAudioFormat table incl. ;codecs= cases"
      contains: "codecs"
  key_links:
    - from: "internal/assets/audio_processor.go"
      to: "assets.AudioFormat"
      via: "ProcessAsset transcribe format hint"
      pattern: "AudioFormat\\("
  prohibitions:
    - "MUST NOT add new container entries to AudioFormat — the ;codecs= strip makes bare audio/webm + audio/mp4 sufficient (RESEARCH Q4)"
    - "MUST NOT change the ogg default or the existing bare-MIME mappings — only add the codecs-strip + export + the AURA_TTS_MAX_CHARS knob"
    - "MUST NOT exceed 4096 as the AURA_TTS_MAX_CHARS default (the provider hard ceiling); a lower default is acceptable only if cost-motivated and documented"
---

<objective>
Lay the two daemon-free backend primitives the voice API depends on: the `AURA_TTS_MAX_CHARS` config knob (default 4096) and an EXPORTED, `;codecs=`-safe `assets.AudioFormat`. Both are pure logic (an int loader + a string map) and land in the `db_integration neo4j_integration` coverage gate, so both are exercised by daemon-free unit tests to hold the owned-surface ≥85% floor (CLAUDE.md).

Purpose: Provide the char-cap value (`/api/tts`) and the correct container→STT-format mapping (`/api/stt`) as reusable, unit-tested seams before the handlers are wired in 37C-03.
Output: `config.Config.TTSMaxChars` + its knob-registry row + `assets.AudioFormat` (exported, codecs-stripped) + two unit tests.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/ROADMAP.md
@.planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md
@internal/config/config.go
@internal/assets/audio_processor.go
</context>

<artifacts_produced>
This plan produces:
- **`config.Config.TTSMaxChars int`** field + its `envutil.IntDefault("AURA_TTS_MAX_CHARS", 4096)` loader line.
- **`AURA_TTS_MAX_CHARS`** KnobSpec row (KindInt, Default "4096") in `knobRegistry()`.
- **`assets.AudioFormat(mimeType string) string`** — the renamed/exported map with a `;codecs=` (and whitespace) strip on the media type before the switch.
</artifacts_produced>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Add AURA_TTS_MAX_CHARS config knob (field + loader + registry row + test)</name>
  <files>internal/config/config.go, internal/config/config_knobs.go, internal/config/config_test.go</files>
  <behavior>
    - Unset AURA_TTS_MAX_CHARS → config.Load()/loaded Config.TTSMaxChars == 4096.
    - AURA_TTS_MAX_CHARS=1200 → Config.TTSMaxChars == 1200.
    - knobRegistry() contains a row {Name:"AURA_TTS_MAX_CHARS", Kind:KindInt, Default:"4096"}.
  </behavior>
  <read_first>
    - internal/config/config.go:194-203 — the multimodal knob fields block (add `TTSMaxChars int` beside `MultimodalTimeoutSec`, matching the `// AURA_TTS_MAX_CHARS ...` doc-comment style).
    - internal/config/config.go:483-487 — the loader block where `TTSModel`/`MultimodalTimeoutSec` are read; add `TTSMaxChars: envutil.IntDefault("AURA_TTS_MAX_CHARS", 4096)` here.
    - internal/config/config_knobs.go:59-90 — the Tier B int-knob rows (mirror the `AURA_WEB_FETCH_MAX_BODY_BYTES`/`MULTIMODAL_TIMEOUT_SEC` shape for the new row).
    - internal/config/config_test.go — the existing Load/default assertions to extend (find how MultimodalTimeoutSec or another IntDefault knob is asserted and mirror it).
    - .planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md § Q5 — the 4096 rationale.
  </read_first>
  <action>
    Add a `TTSMaxChars int` field to `config.Config` in the multimodal block (config.go:~194-203) with a doc-comment noting `AURA_TTS_MAX_CHARS` and the 4096 provider ceiling. In the loader (config.go:~483-487) add `TTSMaxChars: envutil.IntDefault("AURA_TTS_MAX_CHARS", 4096)`. Add a `{Name: "AURA_TTS_MAX_CHARS", Kind: KindInt, Default: "4096"}` row to the Tier B section of `knobRegistry()` in config_knobs.go. Extend config_test.go with a table/subtest asserting the default (4096 when unset) and an override (a set value is parsed), mirroring the existing IntDefault-knob assertion; if the test suite has a knob-registry completeness assertion, include AURA_TTS_MAX_CHARS there too. Refactor-on-touch: no dead code / LOC regressions on the touched files.
  </action>
  <acceptance_criteria>
    - `grep -q "AURA_TTS_MAX_CHARS" internal/config/config.go` AND `grep -q "AURA_TTS_MAX_CHARS" internal/config/config_knobs.go`.
    - `grep -q "TTSMaxChars" internal/config/config.go` (field + loader present).
    - `go test ./internal/config/` passes, including a new assertion that default==4096 and an override is honored.
    - `go build ./...` and `go vet ./internal/config/` clean.
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/config/ -run 'TTSMaxChars|Load|Knob' && go vet ./internal/config/ && echo CONFIG_KNOB_OK</automated>
  </verify>
  <done>AURA_TTS_MAX_CHARS is a first-class int knob (field + loader default 4096 + registry row) with a passing default+override test.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Export a ;codecs=-safe assets.AudioFormat + TestAudioFormat (Wave-0 test scaffolding)</name>
  <files>internal/assets/audio_processor.go, internal/assets/audio_processor_test.go</files>
  <behavior>
    - AudioFormat("audio/webm;codecs=opus") == "webm" (codecs stripped, not the ogg default).
    - AudioFormat("audio/mp4;codecs=mp4a.40.2") == "m4a".
    - AudioFormat("audio/webm") == "webm"; AudioFormat("audio/mpeg") == "mp3"; AudioFormat("audio/wav") == "wav"; AudioFormat("") == "ogg"; AudioFormat("application/octet-stream") == "ogg".
    - AudioFormat("audio/webm ; codecs=opus") (whitespace) == "webm".
  </behavior>
  <read_first>
    - internal/assets/audio_processor.go:48,61-79 — the unexported `audioFormat` map + its single caller in ProcessAsset (:48). Rename to exported `AudioFormat`, fold in the codecs-strip, and update the caller in the SAME edit.
    - .planning/phases/37C-web-voice-lane-inserted/37C-RESEARCH.md § Q4 + Landmine #3 — MediaRecorder emits `audio/webm;codecs=opus` / `audio/mp4;codecs=...`; the `;codecs=` suffix must be split off (take the media type before the first `;`, trim spaces) before the exact-string switch; no new container entries needed once stripped.
  </read_first>
  <action>
    Rename `audioFormat` → exported `AudioFormat(mimeType string) string` in audio_processor.go. At the top of the function, strip any media-type parameters: take the substring before the first `;` and `strings.TrimSpace` it, then run the existing exact-string switch unchanged (keep the `ogg` default and all bare-MIME cases). Update the single caller in `ProcessAsset` (:48) to `AudioFormat(asset.MIMEType)`. Add `strings` to imports if needed. Create internal/assets/audio_processor_test.go with `TestAudioFormat` — a table covering the `<behavior>` cases (codecs-stripped webm/mp4, bare webm/mpeg/wav, empty, unknown, whitespace-around-`;`). Refactor-on-touch: keep the file ≤600 LOC, update the doc-comment to reflect the codecs-strip.
  </action>
  <acceptance_criteria>
    - `grep -q "func AudioFormat" internal/assets/audio_processor.go` (exported) AND no remaining lowercase `func audioFormat` definition.
    - `grep -q "AudioFormat(asset.MIMEType)" internal/assets/audio_processor.go` (caller updated).
    - `go test ./internal/assets/ -run TestAudioFormat` passes, asserting `audio/webm;codecs=opus`→`webm` and `audio/mp4;codecs=mp4a.40.2`→`m4a`.
    - `go build ./...` clean (no other package references the old lowercase name).
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/assets/ -run TestAudioFormat && go build ./... && echo AUDIOFORMAT_OK</automated>
  </verify>
  <done>assets.AudioFormat is exported, strips ;codecs= before mapping, its one caller is updated, and TestAudioFormat proves the codecs cases + unchanged bare-MIME behavior.</done>
</task>

</tasks>

<verification>
- `go test ./internal/config/ ./internal/assets/` passes with the two new tests.
- `go build ./...` + `go vet ./internal/config/ ./internal/assets/` clean.
- No daemon required — both primitives are pure logic exercised by unit tests (owned-surface gate contribution).
</verification>

<success_criteria>
- `AURA_TTS_MAX_CHARS` (default 4096) is a loaded, registry-catalogued int knob.
- `assets.AudioFormat` is exported and codecs-safe, with a passing `TestAudioFormat`.
</success_criteria>

<output>
Create `.planning/phases/37C-web-voice-lane-inserted/37C-02-SUMMARY.md` when done.
</output>
