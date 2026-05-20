# STT alternatives — research consolidated (2026-05-20)

Mirror of the Pocket-TTS spike pattern applied to STT. Aura's current baseline: `whisper.cpp v1.8.4` with `ggml-small.bin` (487 MB multilingue) as `aura-whisper` sidecar — measured 7s elapsed on a 3s OGG voice memo (cold start, 4 CPU threads).

**Research method**: parallel sub-agents (D:/tmp local + 2026 online) launched 2026-05-20 evening, BOTH died with 529 overload (sustained Anthropic API outage event). Consolidated inline from: (1) direct WebFetch of antirez/qwen-asr (user lead), (2) Aura's own Wave 2 measurements, (3) memory references, (4) model knowledge.

## Hard constraints

- CPU-only mini-PC, **4 thread budget per sidecar** (per [[feedback_minipc_cpu_budget]])
- Self-hosted only — no cloud APIs
- **Italian primary** — operator voice memos in IT
- Docker sidecar pattern (long-lived HTTP service)
- License: MIT/Apache/BSD; AGPL/GPL blocks distribution
- Buffered OK; streaming nice-to-have

## Baseline measurement — what we're replacing

`whisper.cpp v1.8.4` + `ggml-small.bin` (487 MB, multilingue Whisper-small from OpenAI):

- 3s OGG Telegram voice memo → ~7s elapsed transcription
- **RTF ≈ 0.43x** (worse than real-time)
- Cold start ~3-7s extra for model load
- Italian quality: passable on clean speech, struggles on Cuneo accent / dialect (anecdotal, no WER measured)
- Static link 135 MB runtime image
- License MIT (whisper.cpp wrapper) + MIT (Whisper model weights)

This is the bar. Replacement must be **either faster OR more accurate on Italian**, ideally both.

## Top candidate — antirez/qwen-asr

**URL**: https://github.com/antirez/qwen-asr  
**Stars**: 546 (very fresh, 6 commits visible 2026-05)  
**License**: MIT  
**Stack**: pure C + BLAS (Accelerate on macOS, OpenBLAS on Linux), zero external deps beyond stdlib  
**Model**: Qwen3-ASR 0.6B or 1.7B (Alibaba Qwen3 series, modern transformer ASR)

### Why it's interesting

| Dimension | antirez/qwen-asr | whisper.cpp baseline |
|---|---|---|
| Language | C + BLAS | C + ggml |
| Deps | Zero ext | Zero ext |
| CPU-only | Yes | Yes |
| **RTF offline** | **7.99x (0.6B) / 4.29x (1.7B)** | ~0.43x |
| RTF streaming | 4.69x (0.6B) / 2.54x (1.7B) | N/A (no streaming) |
| KV cache | Prefill reuse + rolling window | Static |
| Italian | `--language Italian` explicit | Multilingue (small) |
| Deployment | Binary, C library, stdin/stdout pipe | Binary, HTTP server |
| Maturity | Semi-experimental (author asks for forks not PRs) | v1.8.4 production |
| **WER on IT** | **NOT REPORTED** | Documented (~10-15% Common Voice IT for small) |

### Architecture fit — strong

This is the **STT equivalent of how Pocket-TTS fit for TTS**: same architectural philosophy as Aura's current whisper.cpp sidecar (C + BLAS + MIT + zero-deps), but ~10-18x faster claimed, with streaming + built-in KV cache pattern that matches the design insights already banked in [[reference_phase_kv_cache_design_2026-05-20]] (the ds4 cache pattern — both repos are by antirez).

### The gap

**No published WER on Italian.** Author benchmarks are RTF-only. Aura needs to measure WER on a small Italian fixture before committing — the same blind-listening approach used to pick Pocket-TTS over Piper Paola.

### The risk

"Semi-experimental" status, 6 commits, author explicitly declines PR collaboration. This means:

- Bug fixes from upstream may be slow/absent
- Aura would maintain its own patches if needed
- No long-term roadmap visibility

Compared to whisper.cpp's mature ecosystem (ggerganov/whisper.cpp has years of patches, GGUF model rotation, community fixes), qwen-asr is a younger bet.

## Other 2026 candidates worth considering

### Whisper variants — incremental upgrade path

1. **whisper-large-v3-turbo** (OpenAI Whisper, released late 2024) — 8x faster than v3, comparable quality, multilingue. GGUF available for whisper.cpp. **Drop-in model swap** for current sidecar. Pros: zero architectural change, mature stack, public WER on IT. Cons: still Whisper, no streaming, RTF likely 0.8-1.5x on 4-thread CPU (not 4-8x like qwen-asr).
2. **faster-whisper** (CTranslate2 backend) — Python sidecar, INT8 quantized, ~4x RTF on CPU per upstream README. License MIT. Cons: PyTorch ecosystem + ~500MB+ image, no native Go integration.
3. **distil-whisper** — Hugging Face distillation, smaller models. EN-focused; **distil-whisper-large-v3 has limited IT coverage**. Probably SKIP for Aura.
4. **insanely-fast-whisper** — GPU-focused, less relevant for CPU mini-PC.
5. **whisperX** — adds word-level alignment + speaker diarization, but heavier. Aura doesn't need diarization for voice memos.

### Modern non-Whisper ASR

6. **NVIDIA Parakeet** (TDT-CTC family) — open weights, very fast on CPU. **IT support unclear** — primarily EN-trained. Probably SKIP unless IT fine-tune exists.
7. **NVIDIA Canary-1B** — multilingue (EN+ES+DE+FR), **NO Italian** in 1B variant per release notes. SKIP.
8. **Meta MMS** (Massively Multilingual Speech, wav2vec2-based) — supports 1000+ languages including IT, but heavier per-language fine-tuning needed for quality. SKIP for v1.
9. **Meta SeamlessM4T v2** — multimodal (speech+text translation), overkill + heavy. SKIP.
10. **Moonshine** (Useful Sensors) — tiny, EN-only, designed for edge devices. SKIP (no IT).
11. **Kyutai STT** — Kyutai (same lab as Pocket-TTS) shipped Moshi which has integrated speech-to-text. Worth a check but Moshi is speech-to-speech-conversational, not pure transcription. Probably MISMATCH for Aura's "voice memo → text → agent loop" shape.

### Italian fine-tuned models (memory)

Per [[reference_phase_mm_audio_models_2026-05-18]]:

- **litus-ai/whisper-small-ita** — PyTorch only, needs conversion to GGML for whisper.cpp use. Path: `whisper.cpp/models/convert-safetensors-to-ggml.py` + Q5_0 quantize → ~150 MB. **Deferred** as a Whisper-stack fine-tune upgrade.
- **bofenghuang/whisper-large-v3** family — French + Italian fine-tunes. Heavier (~3 GB). Probably not worth on 4-thread CPU.

## Comparison table — top 4 viable for Aura

| Engine | RTF on 4-thread CPU (claimed/measured) | IT WER public | Image size | License | Maturity | Streaming | Verdict |
|---|---|---|---|---|---|---|---|
| **whisper.cpp + ggml-small.bin** (current) | 0.43x (measured Aura) | ~12% (Common Voice IT) | 135 MB | MIT | Production | No | Baseline |
| **whisper.cpp + whisper-large-v3-turbo GGUF** | ~1-2x (claim) | ~7% (improved IT) | ~600 MB | MIT | Production | No | Safe upgrade |
| **whisper.cpp + litus-ai-small-ita GGML** | 0.43x (small-class) | ~7-9% (IT-finetuned, est.) | 285 MB | MIT (model: TBD) | Production | No | IT specialist |
| **antirez/qwen-asr 0.6B** | **7.99x claimed offline** | **NOT MEASURED** | ~1 GB (binary + 0.6B weights est.) | MIT | Semi-experimental | Yes | **High-upside bet** |

## Recommendation — 3-engine spike, not commit

Following the **Pocket-TTS decision pattern** ([[project_2026-05-20_wave3_tts_decision_pocket_tts]]): run a blind comparison spike on 3 Italian voice memo samples (same approach as the TTS A/B), measure WER + RTF + cold-start on the actual Aura mini-PC, then commit.

**Spike candidates** (in priority order):

1. **antirez/qwen-asr 0.6B** — highest potential upside, biggest unknown (no public IT WER)
2. **whisper-large-v3-turbo GGUF** in current whisper.cpp sidecar — safe upgrade path, drop-in model swap, ~7% IT WER expected
3. **litus-ai/whisper-small-ita** post-GGML-conversion in current whisper.cpp sidecar — IT specialist, mid-sized

Skip candidates: faster-whisper (Python sidecar tax), distil-whisper (poor IT), Parakeet/Canary/MMS/SeamlessM4T (no IT or heavy), Moonshine (no IT), Kyutai STT (wrong shape).

### Spike measurement plan

For each candidate, on the mini-PC:

1. **Compile/build** the sidecar locally
2. **3 Italian samples** of varying length: ~3s (short query), ~15s (medium voice memo), ~60s (long memo with dialect/accent)
3. **Capture**:
   - WER vs ground-truth transcript (hand-corrected; same 3 samples across candidates)
   - RTF (audio_duration / elapsed_ms)
   - Cold-start time (first invocation after sidecar boot)
   - Memory footprint at steady state
   - Image size
4. **Subjective**: read all 3 transcripts; is the meaning preserved? Any hallucinations? (Whisper is famous for hallucinating on silence — qwen-asr untested)

Decision rule: **if antirez/qwen-asr 0.6B clears WER ≤15% on the 3 samples AND RTF ≥3x measured on mini-PC, adopt as Wave 3.5 swap**. Else, fall back to whisper-large-v3-turbo as the safe upgrade.

## Bank-worthy pattern

**antirez is shipping inference engines that match Aura's stack philosophy**:

- ds4 (DeepSeek V4 Flash LLM, pure C, MIT) ↔ Aura's chat LLM ambitions
- qwen-asr (Qwen3-ASR pure C MIT) ↔ Aura's STT
- Both with **KV cache as first-class design concern** (matches Phase-KV insights)

This is a pattern worth banking: **check `github.com/antirez/*` when Aura needs a new CPU-only inference sidecar**. 2-for-2 fit so far.

## Decision pending

NOT committing to a swap yet. Two paths forward:

- **Conservative**: pin Wave 3.5 = whisper-large-v3-turbo model swap (drop-in, ~600 MB, immediate +5% IT WER, 0 architectural risk). 1 story Ralph.
- **Ambitious**: 3-engine spike → if qwen-asr wins, Wave 3.5 swaps sidecar entirely (architectural shift). 1 spike day + 1 story Ralph.

Recommended: **ambitious path**. Aura already did the analog for TTS (chose Pocket-TTS over Piper despite higher complexity, won on UX). Pattern is proven.

## Open questions

1. **Where does Wave 3.5 sit in the roadmap?** After Phase-ONB (next), after Phase-KV, or interleaved with one of them? STT swap is independent of all current staged phases — could land any time.
2. **Should the spike be a separate Ralph phase or an out-of-band session?** Spike work doesn't fit cleanly into "user story" shape — it's measurement, not implementation.
3. **Italian voice sample pool** — Aura's archive already has Davide's real voice memos. Use those as the spike fixture (with sanitization for sensitive content), or synthesize via Pocket-TTS for reproducibility?

## Sources

- [github.com/antirez/qwen-asr](https://github.com/antirez/qwen-asr) — primary candidate, deep-dived via WebFetch 2026-05-20
- [github.com/ggerganov/whisper.cpp](https://github.com/ggerganov/whisper.cpp) — current baseline
- [huggingface.co/openai/whisper-large-v3-turbo](https://huggingface.co/openai/whisper-large-v3-turbo) — safe-upgrade target
- [huggingface.co/litus-ai/whisper-small-ita](https://huggingface.co/litus-ai/whisper-small-ita) — IT specialist (deferred per memory)
- `D:/tmp/audio-spike/whisper.log` — Aura's own Wave 2 spike measurements (UTF-16 file, partial inspection)
- `D:/tmp/openhuman/app/src/features/human/voice/sttClient.ts` — openhuman uses CLOUD STT (not relevant for Aura's local-only stack but documents the cloud-proxy pattern)
- [[reference_phase_mm_audio_models_2026-05-18]] — litus-ai conversion path
- [[project_2026-05-20_wave3_tts_decision_pocket_tts]] — TTS decision pattern to mirror
- [[feedback_minipc_cpu_budget]] — 4-thread sidecar budget
- [[feedback_gpu_not_for_embedding_workload]] — CPU > GPU rationale for Aura

## Related memory

- [[reference_phase_mm_audio_models_2026-05-18]]
- [[project_2026-05-19_phase_mm_wave2_closed]] — current Wave 2 audio IN baseline
- [[reference_pocket_tts_v2_candidate]] — original TTS bet pattern
- [[feedback_check_tmp_sources_then_brainstorm_best]] — research discipline this doc follows
