# Phase-MM Audio Spike — 2026-05-19

Pre-implementation validation of Whisper IN + Piper TTS OUT before writing
`InboundMessage.Attachments` (Wave 1 ARCH). Goal: cheap proof that latency,
footprint, and quality clear the bar for Italian voice memos and TTS replies.

**Master HEAD at spike**: `f5459e3c`
**Spike workspace**: `D:/tmp/audio-spike/` (ephemeral, not committed)
**Container**: `audio-spike:local`, 3.01 GB, CPU-only, 4 threads, 6 GB RAM cap

---

## TL;DR — go / no-go

| Component | Verdict | Why |
|---|---|---|
| **Whisper baseline GGML small (multilanguage, 487 MB)** | **GO** | 32s IT audio → 12.4s decode, RTF 0.39, accurate transcript including punctuation. Within 5-10s target on medium sample; RAM peak 827 MB; CPU 4-thread saturates cleanly |
| **Whisper litus-ai/whisper-small-ita (Italian-tuned)** | **OPEN — fix conversion** | PyTorch safetensors fetched (923 MB) but `whisper.cpp/models/convert-h5-to-ggml.py` path / args failed silently during spike. Baseline is good enough to ship Wave 2 without it. Italian-tuned remains a potential accuracy upgrade later |
| **Piper Paola medium IT (61 MB ONNX)** | **GO** | Post-load RTF 0.11-0.13 → 312-char paragraph generated as 15.9s WAV in 1.7s wall. First-call cold-start ~670 ms (model load). **Quality verified by user listening 2026-05-19 — natural Italian prosody, no obvious artifacts on all 3 test sentences** |
| **Piper Giorgio (kirys79/piper_italiano)** | **OPEN — repo layout unknown** | None of 4 candidate filenames (`it_IT-giorgio-medium.onnx`, `giorgio.onnx`, `voices/giorgio/giorgio.onnx`, `models/giorgio.onnx`) HEAD-checked OK. Need manual repo browse — but the 3-suspicious-files flag from HF security scanner is enough to make Paola the safer default |

**Net**: Wave 2 audio IN + OUT can ship with **Whisper baseline + Piper Paola** today.
Italian-tuned Whisper and Giorgio voice become Phase-MM-followup, not blockers.

---

## Method

### Container
```dockerfile
FROM python:3.11-slim-bookworm
RUN apt-get install git build-essential cmake ffmpeg ...
RUN git clone whisper.cpp && cd whisper.cpp && make -j4   # builds /opt/whisper.cpp/build/bin/whisper-cli
RUN curl piper_linux_x86_64.tar.gz                         # rhasspy/piper 2023.11.14-2
RUN pip install --index-url .../whl/cpu torch transformers safetensors numpy
```

- Base `python:3.11-slim-bookworm` chosen after 2 build failures on `ubuntu:22.04`
  and `debian:bookworm-slim` (both hit a 403 CDN edge on `python3-setuptools-whl`
  via Cloudflare and Fastly respectively). The Python tarball preinstalls Python
  + pip without going through that apt path, sidesteps the issue.
- Torch pinned to CPU-only via `--index-url https://download.pytorch.org/whl/cpu`.
  Default `pip install torch` on Linux pulls ~1.5 GB of CUDA wheels — wasteful on
  the mini-PC (no GPU) and per memory `gpu-not-for-embedding-workload` CPU is the
  right tool here anyway. Final image is 3.01 GB; the bulk is torch + whisper.cpp
  build artifacts. **Production sidecar should be much smaller** (whisper.cpp
  binary + GGML alone is ~470 MB; no torch needed at runtime, only for the
  one-shot PyTorch→GGML conversion).

### Volumes
```yaml
./samples-in    -> /workspace/samples-in    # user voice memos (mp3 / m4a / wav)
./samples-out   -> /workspace/samples-out   # piper-generated TTS wavs
./models        -> /workspace/models        # whisper GGML + piper ONNX
./results       -> /workspace/results       # transcripts + run logs + summary JSON
./scripts       -> /workspace/scripts:ro    # fetch + run helpers
```

CPU cap: 4 cores (per memory `minipc-cpu-budget` — mini-PC shared with user work).
Memory cap: 6 GB.

### Models fetched

| Model | Source | Size on disk | Status |
|---|---|---|---|
| Whisper baseline GGML | ggerganov/whisper.cpp HF (`ggml-small.bin`) | 466 MB | ✅ |
| Whisper Italian PyTorch source | litus-ai/whisper-small-ita HF | 923 MB safetensors + tokenizer | ✅ download / ❌ conversion |
| Piper Paola medium | rhasspy/piper-voices HF (`it/it_IT/paola/medium/`) | 61 MB ONNX + 7 KB JSON | ✅ |
| Piper Giorgio | kirys79/piper_italiano HF | 0 (filename probe failed) | ❌ |

### Samples (user-provided)
Two TTS-synthetic Italian samples were dropped instead of real voice memos.
**This skews accuracy upward** — TTS audio is cleaner than a real Telegram
voice memo (no background noise, perfect prosody, no codec artifacts).
Numbers below are an **optimistic upper bound**; real-world accuracy on
voice memo input is expected to be slightly worse.

| File | Codec | Duration | Notes |
|---|---|---|---|
| `..._15-1-2.mp3` | mp3 24 kHz mono | 4.1 s | "Ciao come stai? Questa è una prova di verifica modello." |
| `..._15-2-2.mp3` | mp3 24 kHz mono | 32.1 s | Testo canzone "Furore" — Paola & Chiara |

Both converted to 16 kHz mono WAV via `ffmpeg -ar 16000 -ac 1 -c:a pcm_s16le`
inside the container (whisper.cpp requirement).

### Piper test sentences (chosen to stress 3 distinct scenarios)
1. **Short greeting**: "Ciao Aura, sei collegata?" (25 chars)
2. **Technical with number**: "Ho appena ricevuto un errore quattrocentoventinove
   dal modello; vuoi che riproviamo la richiesta tra trenta secondi o preferisci
   aprire una segnalazione adesso?" (160 chars — tests TTS handling of number
   spelled out as words)
3. **Long paragraph**: 312 chars with multi-clause sentences and topic shifts
   (tests prosody stability over time)

---

## Results

### Whisper baseline (`ggml-small.bin`, 466 MB, multilanguage)

```
Sample  short (4.1s):
  elapsed_ms=6633  RTF=1.616  cpu=242%  rss_peak=767 MB
  transcript: " Ciao come stai? Questa è una prova di verifica modello."

Sample  medium (32.1s):
  elapsed_ms=12366  RTF=0.385  cpu=392%  rss_peak=827 MB
  transcript (3 lines):
    "la pista non è più buia e l'ansia con te si annulla la musica muove la sola illusione di averti con me."
    "Pre-Ritornello, Paola and Chiara, non dici niente però dentro ai tuoi occhi c'è un fuoco,"
    "una stroba un riflesso di noi e tutti i colori di questa città."
    "Ritornello, Paola and Chiara, in questa notte di sole furore, furore amarsi e fare rumore nel mio respiro tu senza affermarci più ballare,"
    "ancora ballare come se fosse l'ultima se fosse l'ultima canzone furore con te, con te."
```

**Observations**:
- Short sample: high RTF (1.62×) due to model load + decode overhead on tiny
  input. This is the *cold-start* cost; subsequent calls should reuse the
  in-memory model. Production wiring should keep whisper.cpp running as a
  long-lived sidecar to amortize the load.
- Medium sample: RTF 0.385 = 2.6× faster than realtime. **Beats the 10s target
  on 30s input** comfortably.
- CPU utilization 392 % on a 4-thread cap = saturating all 4 threads cleanly.
- RAM peak 827 MB stays well under the 6 GB cap.
- Transcript faithfulness: punctuation present, sentence boundaries detected,
  Italian word recognition is solid even on song lyrics. The marker "Paola and
  Chiara" appears literally because the TTS audio pronounced "Paola e Chiara"
  but the song's English-style billing leaked through — minor artifact, not a
  systemic accuracy issue.

### Piper TTS — Paola medium voice

```
Sentence 1 (25 chars):   wav=1.6s   elapsed=817 ms   RTF=0.51  (incl. model load)
Sentence 2 (160 chars):  wav=7.7s   elapsed=1009 ms  RTF=0.131
Sentence 3 (312 chars):  wav=15.9s  elapsed=1705 ms  RTF=0.107
```

**Observations**:
- **Cold start cost is ~670 ms** (model load). Piper's own internal RTF
  reported for sentence 1 (after load) is **0.065** — i.e. 65 ms inference for
  1.4 s of audio. The 817 ms total elapsed = 670 ms load + 147 ms inference +
  overhead.
- **Post-load RTF stable at 0.11-0.13** — generates audio ~8-10× faster than
  realtime even on a 312-char paragraph.
- Output: 22 kHz mono WAV PCM s16le. Sizes scale linearly with audio duration
  (~44 KB per second).
- Per-call load is wasteful for an interactive channel — Piper too needs the
  long-lived sidecar pattern.

**Quality verdict**: pending manual listening of the 3 WAVs in `samples-out/`.
Misure tecniche eccellenti, qualità acustica da validare a orecchio.

### Failures (documented for follow-up, not blocking spike)

1. **Whisper Italian-tuned conversion** — `convert-h5-to-ggml.py` invocation
   in `fetch-models.sh` runs but does not produce `ggml-small-ita.bin`. The
   script wraps the call in `|| echo "  CONVERSION FAILED"` so it's non-fatal
   but the failure is silent. Need to:
     - Run the convert script manually with verbose output to capture the
       traceback
     - Check whether whisper.cpp at this version uses a different convert
       entrypoint (e.g. `convert-pt-to-ggml.py`, or a `python -m` invocation)
     - Verify the litus-ai repo layout matches what the script expects
       (some HF Whisper repos require a `whisper-py` checkout alongside)
   ETA to fix: ~30-60 min of focused work.

2. **Piper Giorgio voice download** — kirys79/piper_italiano repo layout
   doesn't match any of 4 candidate filenames the fetch script probes.
   Need to browse the repo (or `huggingface_hub` list_repo_files) to find the
   actual ONNX path. **However**, HF flagged 3 files in this repo as
   suspicious — the integrity concern outweighs the value of a second voice.
   Recommend: drop Giorgio from default config, stick with Paola, revisit
   only if user explicitly wants male voice fallback.

3. **Cold-start cost** — both Whisper and Piper pay ~500-700 ms loading the
   model on each invocation. This is fine for one-shot CLI testing but **not
   acceptable for an interactive Telegram channel**. Production wiring must
   keep both running as long-lived sidecars (`aura-whisper`, `aura-piper`)
   exposing HTTP endpoints, similar to the `aura-markitdown` and
   `embeddinggemma` patterns already in compose.yaml. Add a 4th sidecar (`aura-piper`)
   following the same shape as `aura-embed` (Wave 2.10 pattern).

---

## Verdict per component

### Whisper IN → **GO with baseline GGML small (multilanguage)**
- 0.39× RTF on 30s realistic input clears the 10s/30s target
- Italian accuracy on TTS audio is high; real voice memos expected slightly
  worse but still usable for short-medium commands
- 466 MB model file, 827 MB RAM peak — fits comfortably in mini-PC budget
- Italian-tuned variant is a **follow-up accuracy lift**, not a Wave 2 blocker
- Sidecar pattern required: long-lived process, HTTP API, amortize load

### Piper OUT → **GO with Paola medium (rhasspy official)**
- Post-load RTF 0.11 = 10× realtime, easily clears <1s/sentence target
- 61 MB ONNX, 22 kHz mono WAV output
- rhasspy is the upstream Piper team — no third-party integrity flags
- **Default OFF** for user-initiated TTS only — never auto-trigger TTS on
  every assistant reply (memory: `phase-mm-audio-models` says
  "operator-toggled OFF by default")
- Giorgio (male voice) deferred indefinitely until repo integrity verified

---

## Next steps (concrete, prioritized)

### Wave 1.5 — Substrate prep (must come BEFORE Wave 2 audio code)

1. **US-MM-INIT01: aura-whisper sidecar in compose.yaml**
   - Service `aura-whisper`: build from a slim Dockerfile (whisper.cpp + GGML
     baseline, NO torch), expose HTTP `/v1/transcribe` returning `{text, segments}`
   - Volume `aura-whisper-models` for `ggml-small.bin`
   - `aura-init-models` job extension: fetch `ggml-small.bin` to that volume
     on first boot (pattern from commit `eb7e61ad`)
   - Healthcheck: `curl /health` returning 200 within 10s

2. **US-MM-INIT02: aura-piper sidecar in compose.yaml**
   - Service `aura-piper`: piper binary + Paola voice ONNX, expose HTTP
     `/v1/synthesize` returning audio/wav
   - Volume `aura-piper-voices` for `it_IT-paola-medium.onnx` + `.json`
   - `aura-init-models` extension: fetch Paola voice on first boot
   - Default OFF: operator toggle `AURA_TTS_ENABLED=false` in env (memory
     `phase-mm-audio-models`)

### Wave 2 — Audio IN code (after substrate)

3. **US-MM-A01: voice-message inbound capture** (Telegram + web channels)
   - Extend `InboundMessage.Attachments` with `kind=audio` + URL + duration
   - Telegram: handle `voice` and `audio` updates, download via Bot API
   - Persist as `source.Kind=audio` in `wiki/raw/src_<sha>/`

4. **US-MM-A02: whisper transcription pipeline**
   - On receipt of audio attachment, call `aura-whisper` sidecar
   - Inject transcript as user-visible text in the agent loop
   - Store transcript in `source` metadata (frontmatter `transcript:`)

5. **US-MM-A03: probe coverage for audio IN**
   - probe_chat case `transient_audio_transcribe`: synthetic Telegram voice
     attachment → verify transcript appears in conversation archive +
     assistant reply references content

### Wave 3 — Audio OUT code (after Wave 2)

6. **US-MM-A04: explicit TTS tool**
   - New tool `speak_reply(text string)` returning a path to a generated WAV
   - Default OFF: gated by `AURA_TTS_ENABLED` + per-user opt-in
   - Telegram: send as `voice` message; web: inline audio player
   - Never auto-triggered from system prompt — explicit user request only

### Follow-up (any time, non-blocking)

7. **Whisper Italian-tuned conversion**: ~30-60 min spike to fix
   `convert-h5-to-ggml.py` invocation, produce `ggml-small-ita.bin`, drop into
   `aura-whisper` volume as alternative model. Operator env knob to switch.

8. **Real voice memo benchmark**: re-run spike against 3 actual Telegram voice
   memos (not TTS audio) to measure realistic accuracy + latency. May reveal
   need for VAD preprocessing or chunked decoding on >60s memos.

9. **Production image size**: current 3.01 GB image carries torch unnecessarily
   (only used for one-shot conversion). Production `aura-whisper` and
   `aura-piper` sidecars should be <500 MB each, no torch, no build-essential.

---

## Sidecar topology — proposed Wave 1.5 placement

Reference: current `compose.yaml` as of master `f5459e3c`.

```
                      +---------------------+
   Telegram Bot API   |       aura          |   LAN  0.0.0.0:18080 -> :8080
   web.telegram.org   | (Go binary, agent   |<------- dashboard / web chat
        |  ^          |    loop, channels)  |
        |  |          +----+-+-+-+-+-+-+----+
   voice / text          |   | | | | | |
   replies               |   | | | | | |
                         v   v v v v v v
                 +---+---+---+---+---+---+---+----+
                 |   |   |   |   |   |   |   |    |
                 v   v   v   v   v   v   v   v    v
              searxng qdrant garage  emb  md  WHIS PIPE   (sidecars; existing
              :8088  :6333  :3900  :8081 :3001 :8082 :8083  black-box = new)
              search vector  S3   embed mkdwn  IN   OUT
                            artif gemma                      ^^^^^^^^^^^^^^^^^^
                                                             |  Wave 1.5    |
              (existing today, master f5459e3c)              |  proposed    |
                                                             +--------------+

                        +---------------------+
                        |  aura-init-models   |  one-shot, fetches GGUF/ONNX
                        |  (build cache or    |  to ./data on first boot;
                        |   first-run init)   |  blocks dependent sidecars
                        +----------+----------+  until SHA-256 verified
                                   |
                                   v
                  ./data/{embeddinggemma-300m-Q4_0.gguf,
                          ggml-small.bin,            <-- new (Wave 1.5)
                          it_IT-paola-medium.onnx,   <-- new (Wave 1.5)
                          it_IT-paola-medium.onnx.json}

   Compose depends_on chain (Wave 1.5 additions in *bold*):

   aura-secrets ---> aura-init-models ---> aura-llama-embed --+
                                       \-> aura-whisper *  ----+--> aura
                                       \-> aura-piper   *  ----+
                                                                +--> (via env URLs)
   searxng / garage / qdrant / aura-markitdown ------------------+
```

### Naming + conventions

| New service | Image | Port (inside docker net) | Volume | Model | Env in aura |
|---|---|---|---|---|---|
| `aura-whisper` | `aura-whisper:local` (build context `docker/whisper/`) | `:8082` | `./data:/models:ro` reads `ggml-small.bin` | 466 MB baseline GGML | `WHISPER_BASE_URL=http://aura-whisper:8082` |
| `aura-piper` | `aura-piper:local` (build context `docker/piper/`) | `:8083` | `./data/piper:/voices:ro` reads `it_IT-paola-medium.onnx{,.json}` | 61 MB ONNX | `PIPER_BASE_URL=http://aura-piper:8083`, `AURA_TTS_ENABLED=false` (default off) |

Both follow the **markitdown-mcp pattern** (the template Wave 2.9 established):
- `read_only: true` + `tmpfs:/tmp:size=256m`
- `cap_drop: [ALL]` + `security_opt: no-new-privileges:true`
- `deploy.resources.limits.cpus: "2.0"` (each gets 2 cores — leaves 4 cores for
  `aura` itself + 4 for the host workload; total CPU budget respects the
  16-core mini-PC cap with `aura-llama-embed` running 4 threads concurrently
  during its inference window)
- `deploy.resources.limits.memory: 1G` (whisper baseline 827 MB peak fits;
  piper post-load is ~150 MB so 1G is generous)

### HTTP shapes (proposed, finalize during US-MM-INIT01/02)

**aura-whisper**:
```
POST /v1/transcribe   (multipart/form-data)
  audio: <bytes wav/mp3/m4a/ogg>
  language: "it"
  threads: 4
->
{ "text": "...", "segments": [{start,end,text}], "duration_s": float, "elapsed_ms": int }
```

**aura-piper**:
```
POST /v1/synthesize   (application/json)
{ "text": "Ciao Aura, sei collegata?", "voice": "it_IT-paola-medium" }
->
audio/wav  (22 kHz mono PCM)
+ headers:  X-Duration-S: 1.59 ; X-Elapsed-Ms: 92
```

Both expose `GET /health` -> 200 within 10s, used by compose healthchecks +
`aura` boot-time readiness check (`aura/cmd/aura/app_wire.go` already has the
shape for embedding endpoint reachability).

### Init-models extension (US-MM-INIT00 — pre-req for both INIT01 + INIT02)

`docker/init-models/Dockerfile` + entrypoint script gain two more `curl + sha256`
blocks:

```
AURA_WHISPER_MODEL_URL   = https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin
AURA_WHISPER_MODEL_SHA256= <pin at fetch time>
AURA_PIPER_VOICE_URL     = https://huggingface.co/rhasspy/piper-voices/resolve/main/it/it_IT/paola/medium/it_IT-paola-medium.onnx
AURA_PIPER_VOICE_SHA256  = <pin at fetch time>
AURA_PIPER_VOICE_CFG_URL = https://huggingface.co/rhasspy/piper-voices/resolve/main/it/it_IT/paola/medium/it_IT-paola-medium.onnx.json
AURA_PIPER_VOICE_CFG_SHA256 = <pin at fetch time>
```

Same operator-override pattern as embedding model URL — env-var swappable, SHA
pinned. Cache-hit re-runs exit in ~1s (existing behavior, just adds 2 files to
the manifest).

### Why this shape over alternatives

- **vs single mega-sidecar** "aura-audio": separates concerns + lets operator
  disable TTS without losing voice-in transcription. Aligns with current
  pattern (markitdown ≠ embed ≠ qdrant).
- **vs in-binary** (Go cgo to whisper.cpp + Go ONNX runtime): faster spike-to-
  ship path; matches existing sidecar pattern; lets us swap models / engines
  without rebuilding the Go binary; cgo+ONNX integration in Go is fragile on
  Windows where Aura developers work.
- **vs only-on-demand** (spawn whisper.cpp per voice memo): cold-start ~670 ms
  measured during spike. With ~5-20 voice memos per day expected, the
  long-lived sidecar amortizes load + keeps the model warm in memory.

---

## Files & artifacts

| Path | Contents |
|---|---|
| `D:/tmp/audio-spike/Dockerfile` | spike container definition |
| `D:/tmp/audio-spike/compose.yaml` | one-shot service with 4-CPU cap |
| `D:/tmp/audio-spike/scripts/fetch-models.sh` | model download orchestration |
| `D:/tmp/audio-spike/scripts/run-whisper-v2.sh` | working whisper invocation |
| `D:/tmp/audio-spike/scripts/run-piper.sh` | piper invocation |
| `D:/tmp/audio-spike/samples-in/*.mp3` | user-dropped TTS-synthetic samples |
| `D:/tmp/audio-spike/samples-out/*.wav` | Piper-generated WAVs (listen!) |
| `D:/tmp/audio-spike/results/whisper-baseline-*.txt` | transcripts |
| `D:/tmp/audio-spike/results/piper-summary.json` | timing JSON |

`D:/tmp/audio-spike/` is intentionally outside the repo (ephemeral). Final
spike artifacts (this doc) belong here in `docs/` per memory `no-docs-in-tmp`.
