# Phase-MM Audio Implementation Plan
**Date:** 2026-05-17
**Scope:** Audio IN (Whisper transcription) + Audio OUT (TTS)
**Target:** Mini-PC (16 cores shared, 4-thread sidecar budget), single Telegram user

---

## 📌 Status update — 2026-05-19

**Wave 1.5 (sidecar substrate) and Wave 2 (audio IN) SHIPPED + E2E verified.**

- Master HEAD with audio IN landed: see `git log --oneline` for `feat(audio):`
  commits + the post-ship fixes (`fix(audio): whisper static link + piper-tts
  1.4 synthesize_wav API`).
- Substrate live:
  - `aura-whisper` sidecar (whisper.cpp v1.8.4 CMake static build, 135 MB,
    port 8082) loads `ggml-small.bin` from `./data/ggml-small.bin` (487 MB,
    SHA-pinned in `internal/install/whisper.go`).
  - `aura-piper` sidecar (Python http.server + `piper-tts` 1.4, 416 MB,
    port 8083) loads `it_IT-paola-medium.onnx` from `./data/piper/` (61 MB
    + 7 KB JSON, both SHA-pinned in `internal/install/piper.go`).
  - `aura-init-models` extended to fetch both artifacts on first boot;
    cache-hit re-runs exit in ~1 s.
- Audio IN code:
  - `internal/storage/sources/store/source.go`: `KindAudio`,
    `StatusTranscribeComplete`, `TranscriptMeta`, `OriginalDeletedAt` added.
  - `internal/telegram/voice_handler.go` (~280 LOC): `tele.OnVoice` handler
    mirrors `documents.go` shape — download → SHA-dedupe store → whisper
    transcribe → write `transcript.txt` → **delete `original.ogg`** (privacy:
    voice memos are PII-equivalent; transcript supersedes the raw audio) →
    dispatch transcript as `chat.InboundMessage` via `AfterTranscribeHook`.
  - `internal/llm/whisper/client.go`: HTTP client to `aura-whisper` with an
    injectable `Transcode` field (default ffmpeg pipeline OGG/Opus → 16 kHz
    mono PCM WAV; tests inject a pass-through to stay codec-free).
- UX: when the AfterTranscribeHook succeeds the voice handler **deletes the
  progress chrome** so the chat shows only `[voice memo] → [assistant reply]`,
  no "Transcribed..." ghost. On hook failure the progress stays with an error
  tail so the user sees what broke.

### Reality vs original plan

- **Pipeline shape changed**: this doc originally proposed a CLI-shelling
  whisper invocation. Wave 1.5 chose the sidecar HTTP shape instead (matches
  `aura-markitdown` + `aura-llama-embed` precedent). The Go client speaks
  `POST /inference` multipart to whisper-server upstream.
- **whisper-server is WAV-only**: the upstream HTTP handler decodes only
  16 kHz mono PCM WAV. Telegram voice is OGG/Opus. Aura transcodes via
  ffmpeg (installed in the aura container) inside `whisper.Client.Transcode`.
- **Model**: shipped `ggml-small.bin` baseline (multilanguage, validated 7 s
  on a 3 s Italian memo). `litus-ai/whisper-small-ita` Italian-finetuned
  upgrade is an env-var swap (`AURA_WHISPER_MODEL_URL` + `AURA_WHISPER_MODEL_SHA256`)
  — the spike-doc conversion fix is still a follow-up.
- **Italian voice**: shipped Paola from upstream `rhasspy/piper-voices`
  (verified quality 2026-05-19 by user listening). Giorgio from
  `kirys79/piper_italiano` was dropped — HF flagged 3 suspicious files in
  that repo; Paola from the rhasspy team has no integrity flags.

### Wave 3 (audio OUT) is the queued next step

Substrate is live (`aura-piper` healthy on `127.0.0.1:8083`, synthesis
verified: "Ciao Aura, sei collegata?" → 1.39 s mono 22 kHz WAV in 116 ms).
**No Go code wires it yet.** The Wave 3 design adopts the hermes-agent
per-chat `voice_mode` pattern (`off` | `voice_only` | `all`) with TTS sent
**in addition to text**, never replacing it. See memory entry
`reference-hermes-voice-mode-pattern` for the design and the proposed
US-MM-A04 / A05 / A06 story breakdown.

---

## 1. Executive Summary

**Recommended Setup:**

| Path | Choice | Rationale |
|------|--------|-----------|
| **Audio IN** | whisper.cpp local, [`litus-ai/whisper-small-ita`](https://huggingface.co/litus-ai/whisper-small-ita) Italian-finetuned, CPU threads=4 | Italian operator (Davide); finetuned model handles "ho fatto il caffè" / "ricordami di chiamare il commercialista" better than multi-lingual base. Whisper.cpp loads GGUF-converted weights. ~5-10s per 30s on 4 CPU threads. No API cost. |
| **Audio OUT** | Piper local, [`kirys79/piper_italiano`](https://huggingface.co/kirys79/piper_italiano), native .ogg output, operator-toggled | Italian voice for an Italian operator (no point in en_US-amy reading "ti ho ricordato la riunione" with American accent). Piper supports ONNX models; piper_italiano ships ready-to-use. Fast synthesis (2-3s for 30s), no cloud cost. |
| **Transcription trigger** | INLINE only (default) | Transcribe immediately upon Telegram voice message; result feeds directly into agent turn as text. No separate ingestion pipeline needed initially. |
| **TTS trigger** | Optional per-reply toggle | Operator sets audio_enable_tts=true in dashboard; LLM never auto-triggers to avoid every turn producing voice. Explicit opt-in. |

**Scope fits within Phase-MM budget (~4 sessions)** because:
- Reuses existing source store pattern (no new storage layer)
- Telegram document handler already has concurrent upload + progress editing model
- Tools are additive (don't refactor existing agents)
- No database schema change needed for Phase-MM audio

---

## 2. Telegram Voice Message Flow (Current + Extension)

### Current PDF Path (Slice 4 to Slice 6)

**File:** internal/telegram/documents.go, **Handler:** onDocument()

1. User sends PDF via Telegram
2. Telegram adapter routes to tele.OnDocument handler
3. onDocument() validates format, spawns goroutine, sends "📄 Got it..." progress
4. process() acquires semaphore (max 2 concurrent), downloads via h.bot.File()
5. sources.Put() stores immutable src_<sha16>/original.pdf (SHA-deduped)
6. Markitdown OR Mistral OCR extracts text
7. writeNextToSource() saves ocr.md, ocr.json alongside original
8. sources.Update() sets status=ocr_complete, attaches metadata
9. afterOCRHook called (optional ingest trigger)
10. Progress edits to "✅ Done · src_abc123 · 25 pages · 12.4s · ready for ingest"

### Extension: Voice Message Handler (Slice 4.5, NEW)

**New File:** internal/telegram/voice_handler.go

Pattern mirrors documents.go for audio. Handler registered with b.bot.Handle(tele.OnVoice, b.voice.onVoiceMessage). Downloads audio from Telegram (native OGG format), stores in src_<sha16>/original.ogg, calls whisper.cpp sidecar to transcribe, writes transcript.txt alongside. Updates source status to StatusTranscribeComplete. Fires optional afterTranscribe hook for INLINE pipeline integration (feeds transcript directly into agent turn).

---

## 3. Whisper Local vs API: Decision Matrix

| Criterion | whisper.cpp (Local) | Groq Whisper API | OpenAI Whisper API |
|-----------|---------------------|------------------|-------------------|
| **Latency (30s audio)** | 5-10s (whisper-small-ita on 4 CPU threads) | 2-3s + network | 3-5s + network |
| **Memory footprint** | ~480MB (whisper-small-ita quantized) | Negligible | Negligible |
| **Mini-PC budget fit** | ✅ 4 threads max, acceptable | ⚠️ Network OK, breaks local pattern | ⚠️ Same |
| **Italian quality** | ⭐ finetuned on Italian corpus | ⚠️ multi-lingual (Whisper-large generic) | ⚠️ multi-lingual |
| **Cost** | 0 | 0.001 per 30s | 0.0015 per 30s |
| **Aura alignment** | ⭐ Self-hosted | Cloud lock-in | Cloud lock-in |

**Recommendation: whisper.cpp local with [`litus-ai/whisper-small-ita`](https://huggingface.co/litus-ai/whisper-small-ita)** — Italian-finetuned, self-hosted, mini-PC latency fits, mirrors llama.cpp sidecar pattern. Wire WHISPER_API_KEY_GROQ env var as optional fallback; default path always local Italian model.

**Implementation:** internal/llm/whisper/client.go

Client struct with localCmd (whisper.cpp sidecar) and optional apiKey for Groq fallback. Transcribe() tries local first, falls back to API. Local path invokes whisper.cpp CLI, parses JSON output. Timeout: 30s (configurable).

**Config wiring** (internal/config/config.go):
- WhisperBackend string (default "local")
- WhisperAPIKey string (env: WHISPER_API_KEY_GROQ)
- WhisperModel string (default "whisper-small-ita")
- WhisperLanguage string (default "it")
- WhisperCPUThreads int (default 4)

---

## 4. TTS Local vs API: Decision Matrix

| Criterion | Piper (Local) | ElevenLabs API | OpenAI TTS |
|-----------|---------------|----------------|-----------|
| **Latency (30s text)** | 2-3s (CPU) | 1-2s + network | 1-2s + network |
| **Voice quality** | Good (natural) | Excellent (100+ voices) | Good (6 voices) |
| **Cost** | 0 | 0.0001 per 30s | 0.00001 per 30s |
| **Output format** | WAV (needs ffmpeg → .ogg/opus) | MP3 (needs conversion) | MP3, PCM, Opus |
| **Mini-PC budget fit** | ✅ 2-3 threads, 200MB | ⚠️ API-only | ⚠️ API-only |
| **Aura alignment** | ⭐ Self-hosted | Cloud lock-in | Cloud lock-in |

**Recommendation: Piper local (default voice) with optional operator toggle.** — Fast enough, zero cost, self-hosted. Operator sets audio_enable_tts=true in dashboard; LLM never auto-triggers.

**Implementation:** internal/llm/tts/piper.go

PiperClient struct with cmd (piper sidecar), voice config. Synthesize() pipes text to piper stdin, captures WAV output, invokes ffmpeg to convert to OGG/opus (Telegram format), returns OGG bytes. Timeout: 15s.

**Config wiring** (internal/config/config.go):
- AudioEnableTTS bool (default false)
- TTSBackend string (default "piper")
- TTSAPIKey string (env: TTS_API_KEY_ELEVENLABS or TTS_API_KEY_OPENAI)

---

## 5. Audio Source-Span Schema (Time Ranges)

**Deferred to Phase-MM-EXTENDED.** Current phase only requires:

- compact_memory_documents already supports byte_start, byte_end (PDF page ranges)
- For audio transcripts, don't need time ranges initially — entire transcript is single searchable document
- Later phase (if user needs "find what I said at 2:30") would add time_start_seconds, time_end_seconds columns

**For Phase-MM:** Store as source.KindAudio with Source.TranscriptMeta containing DurationSecs. Write full transcript to transcript.txt. Search returns whole-transcript matches.

---

## 6. Tool Surface Recommendation

**Three tools:**

### 6a. transcribe_audio Tool (NEW, Callable by LLM)

File: internal/agent/tools/registry/audio_transcribe.go

Tool loads stored audio source, reads bytes, calls whisper client, writes transcript. Parameters: source_id (required), language (optional, "it" default for the Italian-finetuned whisper-small-ita model). Returns: "Transcribed src_abc · 120s · 850 words". Use case: operator uploads podcast clip, LLM calls this to fill transcript on demand.

### 6b. synthesize_speech Tool (NEW, Callable by LLM — but GATED)

File: internal/agent/tools/registry/audio_synthesize.go

Tool generates voice message from text (up to 1000 chars). **Capability-gated** (like existing tools with TOOL_ALLOWLIST). Operator must explicitly enable before LLM can call. Default disabled (no surprise voice messages). Returns file handle for Telegram delivery.

### 6c. No new store_audio tool needed

Existing store_source(kind="text") covers "save transcript as text source". LLM can't stream binary audio (same reason as PDFs).

---

## 7. Phase-MM-AUDIO User Stories (Atomic, E2E-Testable)

### Story 1: MM-01 — Voice Message Ingestion & Storage

**Goal:** Telegram users can send voice messages; Aura stores + transcribes them.

**Acceptance Criteria:**
1. Handler registered: b.bot.Handle(tele.OnVoice, b.voice.onVoiceMessage)
2. Voice message triggers progress: "🎙️ Got it — transcribing..."
3. Source stored: src_<sha16>/original.ogg (Telegram native format)
4. Whisper.cpp invoked: 30s memo transcribes in <10s (4 threads, whisper-small-ita)
5. Transcript written: src_<sha16>/transcript.txt contains expected keywords
6. Status updated: source.StatusTranscribeComplete (new constant)
7. Progress edits to: "✅ Done · src_abc · 30s · ready for ingest"
8. Golden test: real 30s fixture voice file ("hello world test") → verify transcript contains "hello" + "world"

**Implementation:**
- File: internal/telegram/voice_handler.go (~250 LOC)
- Extend: internal/storage/sources/store/source.go (add KindAudio, StatusTranscribeComplete, TranscriptMeta)
- Extend: internal/storage/sources/store/formats.go (register .ogg upload format)
- Extend: internal/telegram/handlers.go (register voice handler)
- Test fixture: internal/telegram/fixture/voice_hello_world.ogg (30s, pre-encoded)

**Commit:** 1 atomic commit, handlers + source schema + tests.

### Story 2: MM-02 — Whisper.cpp Sidecar Integration

**Goal:** Whisper.cpp subprocess runs alongside Aura, transcribes via local CPU.

**Acceptance Criteria:**
1. Config: WHISPER_MODEL=whisper-small-ita, WHISPER_LANGUAGE=it, WHISPER_CPU_THREADS=4, WHISPER_BACKEND=local
2. Sidecar spawned on startup or per-request (CLI invocation simpler)
3. Latency <10s for 30s audio on mini-PC (4 shared cores)
4. Fallback: if WHISPER_API_KEY_GROQ set and local fails, transparently use Groq API
5. Error handling: transcribe timeout after 30s → "Transcription timed out (audio too long?)"
6. Test: Mock whisper.cpp CLI, verify correct args + JSON parse

**Implementation:**
- File: internal/llm/whisper/client.go (~200 LOC)
- File: internal/llm/whisper/local.go (CLI invocation)
- File: internal/llm/whisper/api.go (Groq fallback, optional)
- Extend: internal/config/config.go (WhisperBackend, WhisperModel, WhisperAPIKey, WhisperCPUThreads)
- Extend: cmd/aura/app.go (wire whisper.Client into voice handler on startup)
- Test: mock CLI, verify args + parsing

**Commit:** 1 atomic commit, config + sidecar client + tests.

### Story 3: MM-03 — TTS Output & Dashboard Toggle

**Goal:** Aura can reply with voice; operator toggles via dashboard.

**Acceptance Criteria:**
1. Config: AUDIO_ENABLE_TTS=false (default)
2. Dashboard setting: checkbox "Enable voice replies"
3. When enabled + Telegram + short reply (<1000 chars): optional synthesis
4. Piper sidecar OR API config (TTS_BACKEND=openai)
5. Output: .ogg/opus (Telegram voice message format)
6. ffmpeg conversion: WAV → OGG/opus
7. Telegram delivery: bot sends synthesized voice as reply attachment
8. Test: "Test TTS message" → 2-3s synthesis → verify .ogg bytes

**Implementation:**
- File: internal/llm/tts/piper.go (~180 LOC)
- File: internal/llm/tts/api.go (OpenAI/ElevenLabs fallback)
- Extend: internal/telegram/outbound.go (conditional synthesis on reply)
- Extend: internal/api/settings.go (expose audio_enable_tts toggle)
- Extend: internal/config/config.go (AudioEnableTTS, TTSBackend, TTSAPIKey)
- ffmpeg wrapper for WAV → OGG
- Test: mock Piper, verify ffmpeg invocation, assert .ogg file created

**Commit:** 1 atomic commit, TTS client + outbound integration + dashboard setting.

---

### Test Fixture Spec (Shared Across 3 Stories)

**Golden audio file:** cmd/test_fixtures/voice_hello_world.ogg
- Duration: 30 seconds
- Content: "Hello world, this is a test message." (spoken clearly)
- Format: Telegram native OGG/opus (44.1kHz, mono)
- Use case: MM-01 golden test + MM-02 latency benchmark

**Verification in tests:**

    func TestVoiceTranscription(t *testing.T) {
        voiceBytes := mustReadFixture("voice_hello_world.ogg")
        src, _ := handler.processVoice(ctx, userID, voiceBytes)
        transcriptBytes, _ := os.ReadFile(handler.store.Path(src.ID, "transcript.txt"))
        transcript := string(transcriptBytes)
        
        if !strings.Contains(strings.ToLower(transcript), "hello") {
            t.Fatalf("Expected 'hello' in transcript, got: %s", transcript)
        }
        if !strings.Contains(strings.ToLower(transcript), "world") {
            t.Fatalf("Expected 'world' in transcript, got: %s", transcript)
        }
    }

---

## 8. Open Questions for Davide (Operator)

1. **Whisper model size:** `litus-ai/whisper-small-ita` (~480MB, 5-10s per 30s) is the recommended default for Italian. If latency is too high, fallback options: `whisper-medium-ita` (when available, faster but ~1.5GB) or Groq API. Measure on real mini-PC first.

2. **Voice message frequency & storage:** Expect daily or occasional? 1 min of OGG ≈ 30KB; 10 voice memos = 300KB/week. Keep on local SSD, no Garage backup needed.

3. **TTS voice preference:** Default is [`kirys79/piper_italiano`](https://huggingface.co/kirys79/piper_italiano) (Italian male, conversational). If Piper Italian repo offers multiple voices, pick one explicitly via PIPER_VOICE env var. Other Italian Piper variants on HuggingFace are alternatives.

4. **Transcription language:** Default `WHISPER_LANGUAGE=it` for the Italian-finetuned model. If multilingual content arrives (English video transcripts, etc.), the model still works but quality drops. Auto-detect can be added later if needed.

5. **transcribe_audio tool:** Should LLM be able to call on demand? Recommend: enable by default (read-only, safe).

6. **TTS security:** Should synthesize_speech be restricted (TOOLS_ALLOWLIST) since it creates assets? Recommend: default disabled (AUDIO_ENABLE_TTS=false); operator explicitly enables.

---

## References

- **Telegram Bot API:** https://core.telegram.org/bots/api#voice
- **whisper.cpp:** https://github.com/ggerganov/whisper.cpp (CLI, server mode)
- **Piper TTS:** https://github.com/rhasspy/piper (local, ~8 voices)
- **Groq Whisper API:** https://console.groq.com/docs/speech-text
- **Existing Aura patterns:**
  - PDF handler: internal/telegram/documents.go (concurrency, progress edits, afterOCR hook)
  - Source store: internal/storage/sources/store/source.go (Kind, Status, metadata)
  - Tool design: internal/agent/tools/registry/*.go (Name, Description, Parameters, Execute)
  - Config: internal/config/config.go (envconfig struct)
