# Phase-MM Multimodal Costs — May 2026 market check

Companion to `docs/phase-mm-synthesis-2026-05-17.md` (the original 9-story plan) and `docs/phase-mm-audio-plan-2026-05-17.md` (audio side). Researched 2026-05-18 to validate Phase-MM cost assumptions against current market.

## TL;DR

- **Phase-MM locked stack remains the right call**: Sonnet 4.6 image IN ($0.003/img) + Flux 1 Schnell image OUT ($0.003/img) are still the bargain in May 2026
- **Video OUT skip decision RECONFIRMED**: even cheapest tier (Veo 3.1 Lite $0.05/sec) is 50x more expensive than image OUT; rare-use case → not worth implementing
- **Video IN newly affordable** ($0.005/min via Gemini) — propose as Phase-MM Wave 4 opt-in when user demand surfaces
- **Total multimodal budget ~$0.45/month** for typical Aura usage (vs ~$30/mo Sonnet text baseline → multimodal adds <2%)

## Image IN (vision/understanding)

| Provider | Cost/img | Latency | Quality (Italian) | Notes |
|---|---|---|---|---|
| **Claude Sonnet 4.6** (LOCKED) | ~$0.003/img | 2-3s | Best | Via OpenRouter, stack consistency |
| GPT-4.1 / GPT-5 vision | ~$0.005-0.010/img | 1-2s | Top tier | More expensive |
| Gemini 2.5 Flash | ~$0.0006/img (560 tokens) | <1s | Good | 5x cheaper than Sonnet |
| Gemini 3 Pro Image | $0.0011/img | 1-2s | Top tier multimodal | Newer tier |

**Decision: keep Sonnet 4.6.** At $0.003/img × 100 foto/mese = $0.30/mese the cost is negligible. The 5x savings on Gemini Flash isn't worth adding a second provider to the stack (more env vars, more cred management, more failure modes). Only reconsider if Aura usage scales to 1000+ foto/mese.

## Image OUT (generation)

May 2026 market is tiered:
- **Premium proprietary** (DALL-E 4, Midjourney API, Imagen 4): $0.03-0.20/img
- **Open-weight self-hosted** (SD 3.5, Flux 1.2 Pro, Ideogram 3, Recraft V3): $0.02-0.10/img
- **Aggregators** (Replicate, fal.ai, Together, Fireworks running open-weight): $0.002-0.04/img

| Provider | Cost/img | Quality | Notes |
|---|---|---|---|
| **Flux 1 Schnell via Replicate** (LOCKED) | $0.002-0.003/img | Good | 4-6s, already planned |
| Flux 2 Schnell | $0.015/img | Better | 5x cost, marginal upgrade |
| Flux 2 Pro | $0.05/img | Excellent | Pro use only |
| Ideogram 3.0 | $0.03-0.09/img | **Best text-in-image** | Niche: logos, posters |
| Recraft V3 | $0.04 raster / $0.08 vector | Vector king | Niche |
| DALL-E 3/4 | $0.04-0.08/img | Premium | Legacy now |
| Imagen 4 | $0.03-0.12/img | Top | GCP-anchored |

**Decision: keep Flux 1 Schnell.** Absolute bargain at $0.002-0.003/img. 50 img/mese = $0.10. Only upgrade to Flux 2 Pro if quality proves insufficient in production.

## Video IN (understanding) — not in original roadmap

| Provider | Cost | Notes |
|---|---|---|
| **Gemini API video** | $0.000077/sec (Flash) or $0.0003/sec (Pro) | 258 tokens/sec at 1fps sampling + audio tokens if present |
| OpenAI vision-on-video | Not native | Requires manual frame sampling + multiple vision calls |

**Aura cost estimate**: 30-sec voice+video memo via Gemini Flash = $0.0025/memo. 20 memo/mese = $0.05.

**Decision: defer to Phase-MM Wave 4 opt-in.** Affordable but requires adding Gemini to the provider stack (today Aura uses Sonnet only via OpenRouter). Don't pre-build infrastructure without user demand. If the user starts sending video memos to Aura via Telegram, then implement Wave 4 (likely 2-3 stories: provider adapter, video upload handler, frame-sampling helper).

## Video OUT (generation) — skip decision RECONFIRMED

| Provider | Cost/sec | Max duration | Notes |
|---|---|---|---|
| **Veo 3.1 Lite** | $0.05/sec | Standard | Cheapest 2026 tier (new) |
| Veo 3.1 Fast (no audio) | $0.10/sec | 60s | Vertex AI / fal.ai / Replicate |
| Veo 3.1 Standard | ~$0.05-0.08/sec ($0.40/clip) | 60s | Full quality + audio |
| Kling 3.0 | $0.07/sec | Up to 2 min | Production quality leader |
| Runway Gen-4.5 | ~$0.10/sec | Max 16 sec | Image-to-video king |
| Pika | Subscription/credits | Variable | Social creator focus (lip-sync, swaps) |
| **Sora 2** | DISCONTINUED 2026-09-24 | 25s | OpenAI shutting API |
| Sora's internal cost | $1.30/clip 10-sec | — | Explains why API was so expensive |

**Internal economics**: OpenAI reportedly spent $1.30 per 10-sec Sora 2 clip to produce. Video generation is GPU-heavy + compute-expensive. The cheapest tier ($0.05/sec via Veo 3.1 Lite) reflects the current market floor — unlikely to drop significantly until specialized hardware (TPU video accelerators?) reaches more providers.

**Decision: skip CONFIRMED.** Cost math:
- 5-sec clip × $0.05/sec = $0.25/clip (cheapest)
- 100 clips/mese = $25/mese = **~50x image OUT cost**
- Use case is rare for a personal assistant (Aura is text/voice/image-first)
- Video OUT is Phase 8 substrate territory (would re-open under a concrete recurring workload — e.g., marketing-research that needs short social clips). Per memory `project_2026-05-17_roadmap_after_phase_t`, Phase 8 is de-scoped until that workload appears.

## Sora discontinuation context

OpenAI announced March 2026:
- Sora web/app experiences discontinued April 26, 2026 (already happened)
- API discontinued September 24, 2026 (5 months away)

This is significant: the only OpenAI video product is being shut down because the unit economics didn't work. Reinforces "video OUT is expensive infrastructure" thesis. Google (Veo via Vertex/Gemini), Runway, and Kling are now the dominant providers.

## Monthly budget Aura multimodal — typical usage

| Component | Volume/month | Cost |
|---|---|---|
| Image IN (Sonnet 4.6) | 100 photos | $0.30 |
| Image OUT (Flux schnell) | 50 images | $0.10 |
| Audio IN (Whisper local) | 30 voice memos | $0.00 |
| Audio OUT (Piper local) | 100 TTS | $0.00 |
| Video IN (Gemini, Wave 4 opt-in) | 20 × 30sec | $0.05 |
| Video OUT (skipped) | 0 | $0.00 |
| **Multimodal total** | | **~$0.45/month** |
| Text chat baseline (Sonnet 4.6) | daily use | ~$30/month |
| **Grand total** | | **~$30.45/month** |

**Multimodal feature adds <2% to the monthly cost** while providing dramatic UX gains (voice-in-Italian, photo-understanding, image-generation, optional video-understanding).

## Open questions

1. **Italian voice-clone TTS**: Piper is good but generic. If user wants a personalized voice, ElevenLabs charges $5-22/month subscription. Out of scope for Phase-MM Wave 1-3.
2. **Long-form video summary**: if user sends a 30-min meeting recording, Gemini video can process it but cost scales: 30 min × $0.005/min = $0.15/recording. Phase-MM Wave 4 opt-in territory.
3. **Real-time video stream**: GoogleMeet integration like openhuman's mascot is a different architecture (live ingestion). Phase-U plugin territory if at all.

## Sources (May 2026)

- [LLM API Pricing 2026 — TLDL](https://www.tldl.io/resources/llm-api-pricing-2026)
- [Cross-Provider LLM API Pricing Comparison April 2026 — PE Collective](https://pecollective.com/blog/llm-pricing-comparison-2026/)
- [AI Image Generation API Pricing 2026 (12 Providers) — Digital Applied](https://www.digitalapplied.com/blog/ai-image-generation-api-pricing-comparison-2026)
- [Text-to-Image Generators 2026 Compare — CodeSOTA](https://www.codesota.com/tasks/text-to-image)
- [AI Image Generation APIs in 2026: DALL-E, Imagen, Flux, Midjourney Compared — NovaKit](https://www.novakit.ai/blog/ai-image-generation-apis-2026-compared)
- [Veo 3.1 Pricing per Second 2026 — AI Free API](https://www.aifreeapi.com/en/posts/veo-3-1-pricing-per-second-gemini-api)
- [AI Video Market After Sora — Digital Applied](https://www.digitalapplied.com/blog/ai-video-market-after-sora-runway-kling-veo-2026)
- [Best AI Video Generators 2026 — Hedra](https://www.hedra.com/blog/best-ai-video-generators)
- [Gemini Developer API pricing](https://ai.google.dev/gemini-api/docs/pricing)
- [Gemini Video Understanding](https://ai.google.dev/gemini-api/docs/video-understanding)
- [AI Image Model Pricing — Replicate vs fal.ai 2026](https://pricepertoken.com/image)
