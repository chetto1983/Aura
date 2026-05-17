# Phase-MM Audio Implementation Plan
**Date:** 2026-05-17  
**Scope:** Audio IN (Whisper transcription) + Audio OUT (TTS)  
**Target:** Mini-PC (16 cores shared, 4-thread sidecar budget), single Telegram user

---

## 1. Executive Summary

**Recommended Setup:**

| Path | Choice | Rationale |
|------|--------|-----------|
| **Audio IN** | whisper.cpp local, base.en model, CPU threads=4 | Matches Aura's "all-local where possible" pattern; latency-bound (30s memo ~5-8s on base model). No API cost. Mirrors llama.cpp sidecar pattern. |
| **Audio OUT** | Piper local, native .ogg output, operator-toggled | Fast synthesis (2-3s for 30s transcript), acceptable quality for casual use, no cloud cost. Falls back gracefully if disabled. |
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
| **Latency (30s audio)** | 5-8s (base.en on 4 CPU threads) | 2-3s + network | 3-5s + network |
| **Memory footprint** | 300MB (base.en quantized) | Negligible | Negligible |
| **Mini-PC budget fit** | ✅ 4 threads max, acceptable | ⚠️ Network OK, breaks local pattern | ⚠️ Same |
| **Cost** | 0 | 0.001 per 30s | 0.0015 per 30s |
| **Aura alignment** | ⭐ Self-hosted | Cloud lock-in | Cloud lock-in |

**Recommendation: whisper.cpp local (base.en model)** — Self-hosted, mini-PC latency fits, mirrors llama.cpp sidecar pattern. Wire WHISPER_API_KEY_GROQ env var as optional fallback; default path always local.

**Implementation:** internal/llm/whisper/client.go

Client struct with localCmd (whisper.cpp sidecar) and optional apiKey for Groq fallback. Transcribe() tries local first, falls back to API. Local path invokes whisper.cpp CLI, parses JSON output. Timeout: 30s (configurable).

**Config wiring** (internal/config/config.go):
- WhisperBackend string (default "local")
- WhisperAPIKey string (env: WHISPER_API_KEY_GROQ)
- WhisperModel string (default "base.en")
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

Tool loads stored audio source, reads bytes, calls whisper client, writes transcript. Parameters: source_id (required), language (optional, "en" default). Returns: "Transcribed src_abc · 120s · 850 words". Use case: operator uploads podcast clip, LLM calls this to fill transcript on demand.

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
4. Whisper.cpp invoked: 30s memo transcribes in <10s (4 threads, base.en)
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
1. Config: WHISPER_MODEL=base.en, WHISPER_CPU_THREADS=4, WHISPER_BACKEND=local
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

1. **Whisper model size:** base.en (30MB, 5-8s per 30s) or medium.en (1.4GB, 2-3s)? Recommend starting with base.en, measure latency.

2. **Voice message frequency & storage:** Expect daily or occasional? 1 min of OGG ≈ 30KB; 10 voice memos = 300KB/week. Keep on local SSD, no Garage backup needed.

3. **TTS voice preference:** Piper default is "en_US-amy-low" (female, friendly). Acceptable or prefer different voice from Piper's 8 voices?

4. **Transcription language:** Assume English (en) for now? Or need auto-detect for Italian/German?

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
