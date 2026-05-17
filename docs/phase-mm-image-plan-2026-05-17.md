# Phase-MM Image Multimodal Implementation Plan

**Date:** 2026-05-17  
**Status:** Concrete design for Phase-MM image IN + image OUT  
**Scope:** 2–3 atomic user stories for vision + generation pipeline

---

## Executive Summary

Aura's vision substrate is **70% wired already**:
- LLM client (\internal/llm/openai.go\) supports OpenAI-compatible chat completions via OpenRouter — multipart content (text + \image_url\) is **not yet implemented** but the transport is ready.
- Source store (\internal/storage/sources/store\) has a \KindImage\ constant reserved but extraction is gated to Wave 2.9.5.
- Telegram document handler (\internal/telegram/documents.go\) processes PDFs through OCR; images are rejected at the upload boundary.
- Tools ecosystem (\internal/agent/tools/registry\) exposes \ocr_source\, \ead_source\, \store_source\ following a bounded-byte pattern that ports cleanly to images.

### Recommendation

**Image IN (default):** Inline vision via OpenRouter Claude Sonnet 4.6 or Opus 4.7 (no local requirement).
- Cost: ~\.01–\.03 per image.
- Quality: Best.
- Latency: 2–5s for typical 500KB images.
- Local alternative: Opt-in \VISION_BACKEND=local\ → Qwen2.5-VL via llama.cpp (slower, privacy).

**Image OUT (future):** DALL-E 3 via OpenAI API (\.04/image) as default; local Flux.1-schnell as operator opt-in.

---

## 1. Telegram Photo Flow

### Current State

Photos are **not wired** today:
- \internal/telegram/handlers.go\ registers \	ele.OnDocument\ but not \	ele.OnPhoto\.
- \internal/telegram/documents.go\ validates via \alidateDocument()\ and rejects non-PDF MIME types at \DetectUploadFormat()\ (see \internal/storage/sources/store/formats.go\).
- Photos → source store requires:
  1. \handlers.go\: add \.bot.Handle(tele.OnPhoto, b.photos.onPhoto)\ (mirrors \onDocument\).
  2. Create \internal/telegram/photos.go\ mirror of \documents.go\ with concurrency limit (2-at-a-time, like docs).
  3. Skip OCR; route directly to \source.Put()\ with \Kind: KindImage\.
  4. On save: trigger optional vision analysis (inline or stored).

### Two Routing Paths

#### Path A: INLINE (recommended for most use cases)
- User sends photo in Telegram.
- \photos.onPhoto\ → \source.Put()\ → status \StatusStored\ (fast, no API wait).
- Telegram message → \hub.Receive()\ → agent loop.
- Chat message includes \[image]\ hint + image bytes or URL.
- LLM client converts to multipart: \{"type": "image_url", "image_url": {"url": "..."}}\ alongside text.
- LLM sees image, reasons inline, replies.
- **Pro:** Single turn, fast feedback, no intermediate storage/retrieval.
- **Con:** Each vision call costs tokens; re-analysis requires reprompt.

#### Path B: INGESTED (for searchability / re-use)
- User sends photo.
- \photos.onPhoto\ → \source.Put()\ + vision call (Mistral Document AI for images, or Claude via \nalyze_image\ tool).
- Write \ision.md\ (analogue of \ocr.md\) with LLM-generated description.
- Ingest into wiki (via existing \internal/storage/sources/ingest\ pipeline).
- Chat message cites source ID; LLM fetches via \ead_source\.
- **Pro:** Indexable, reusable, wiki-searchable.
- **Con:** Async, more latency, network I/O.

**Recommendation:** Support both. Default to INLINE for Telegram photos (fast + synchronous). Path B fires if user explicitly calls \ingest_source\ on a \KindImage\ source.

### File Layout (PDR §4 extension)

\\\
wiki/
  raw/
    src_<sha16>/
      original.png          (immutable, from Telegram)
      source.json           (metadata: Kind=image, MimeType=image/png, etc.)
      vision.md             (optional, written by vision analysis)
      vision.json           (optional, raw LLM response for replay)
\\\

---

## 2. Vision Model Decision Matrix

| Model | Provider | Cost (per image) | Latency | Quality | Local Alt? | Notes |
|-------|----------|-----------------|---------|---------|------------|-------|
| Claude Sonnet 4.6 | OpenRouter | \.003 | 2–3s | Excellent | No | **Recommended default.** Sonnet 4.6 has vision; Opus 4.7 also available. |
| Claude Opus 4.7 | OpenRouter | \.015 | 3–5s | Best | No | Higher cost, slightly better on dense/complex scenes. |
| GPT-4o | OpenRouter | \.010 | 3–4s | Very good | No | Good alternative if Anthropic quotas tight. |
| Gemini 2.0 Flash | OpenRouter | \.001 | 2–3s | Good | No | Cheapest; slightly lower quality on OCR-like tasks. |
| Qwen2.5-VL (7B) | llama.cpp (local) | \ | 10–30s | Good | Yes | CPU-only, 5–15s on 4-thread mini-PC. No API cost. Privacy-first. |
| LLaVA-1.6 (13B) | llama.cpp (local) | \ | 15–40s | Decent | Yes | Slower than Qwen2.5-VL. Training data older (cutoff ~2021). |

**Recommendation:** 
- **Default:** \VISION_MODEL=anthropic/claude-sonnet-4-6\ via \LLM_BASE_URL=https://openrouter.io\ (already wired; no code change needed, just config).
- **Operator opt-in:** \VISION_BACKEND=local\ + \VISION_MODEL_PATH=/path/to/qwen2.5-vl-7b.gguf\ → spawn llama.cpp subprocess.

**Cost per 1000 images (assuming avg 500KB each):**
- Sonnet 4.6: \ (viable for personal agent).
- Opus 4.7: \.
- Qwen2.5-VL local: \ (hardware amortized).

---

## 3. Generation Model Decision Matrix

| Model | Provider | Cost | Latency | Quality | Local? | Notes |
|-------|----------|------|---------|---------|--------|-------|
| DALL-E 3 | OpenAI | \.04 | 3–10s | Excellent | No | **Recommended default.** No need to new API key (reuse existing if available). |
| Flux Pro | Replicate/FAL | \.013 | 8–15s | Excellent | Yes* | Faster than DALL-E, but image quality debates. FAL is simpler integration. |
| Flux.1-schnell | Replicate | \.003 | 4–6s | Very good | Yes* | Local via diffusers (requires GPU). 4 steps → fast. |
| Stable Diffusion 3 | Replicate | \.010 | 5–8s | Good | Yes* | Slightly slower than Flux. Better control tokens. |
| Stable Diffusion XL (local) | diffusers/comfyui | \ | 30–60s | Good | Yes | CPU-only = very slow. GPU ~8–12s. Aura mini-PC likely no GPU → not viable. |

*Local models require operator to run inference locally (diffusers, comfyui, or Replicate API wrapper).

**Recommendation:**
- **Default:** \generate_image\ tool calls Replicate + Flux.1-schnell API (cheapest + good quality + under 10s).
- **Operator opt-in:** \IMAGE_GEN_BACKEND=local\ + GPU requirement documented (not suitable for headless/mini-PC).
- **Future:** DALL-E 3 if operator provides \OPENAI_API_KEY\ separately.

**Cost per 1000 images:**
- Flux.1-schnell (Replicate): \.
- Flux Pro (FAL): \.
- DALL-E 3: \.

---

## 4. LLM Client Multipart-Content Gap Analysis

### Current State (internal/llm/openai.go)

The \convertMessage()\ function (line 356–383) converts \llm.Message\ to \chatMessage\:

\\\go
func convertMessage(m Message) chatMessage {
    msg := chatMessage{
        Role:       m.Role,
        ToolCallID: m.ToolCallID,
    }
    if m.Content != "" || (m.Role != "assistant" && len(m.ToolCalls) == 0) {
        content := m.Content
        msg.Content = &content  // ← single text string, not multipart
    }
    // ...
    return msg
}
\\\

**Gap:** \msg.Content\ is a \*string\, not an array of content blocks. OpenAI spec requires:
\\\json
{
  "role": "user",
  "content": [
    {"type": "text", "text": "What's in this image?"},
    {"type": "image_url", "image_url": {"url": "data:image/png;base64,..."}}
  ]
}
\\\

### Required Changes

**In \internal/llm/client.go\:**
1. Extend \Message\ struct to support optional \ImageURLs []string\ field (or generic \ContentParts []ContentPart\).
2. Extend \LLM.Request\ struct with optional vision model selector (e.g., \ForceVisionModel string\).

**In \internal/llm/openai.go\:**
1. Update \chatMessage\ struct:
   \\\go
   type chatMessage struct {
       Role       string         \json:"role"\
       Content    interface{}    \json:"content"\  // can be string or []contentBlock
       ...
   }
   \\\
2. Update \convertMessage()\ to dispatch on image URLs:
   \\\go
   if len(m.ImageURLs) > 0 {
       blocks := []map[string]any{
           {"type": "text", "text": m.Content},
       }
       for _, url := range m.ImageURLs {
           blocks = append(blocks, map[string]any{
               "type": "image_url",
               "image_url": map[string]any{"url": url},
           })
       }
       msg.Content = blocks
   } else {
       content := m.Content
       msg.Content = &content
   }
   \\\
3. **No change to \Stream()\ or \Send()\ signatures** — wire conversion happens in \convertMessage()\, which already handles all message shapes.

### Estimate

3–4 hours: implement, test with fixture image, verify \nthropic/claude-sonnet-4-6\ accepts multipart messages.

---

## 5. Image Source-Span Schema

For bounding-box citations (e.g., "the cat in this region"), extend \ocr.md\ sidecar:

**\ision.md\ layout** (mirrors \ocr.md\):
\\\markdown
# Source Vision Analysis: photo.jpg

Source ID: src_abc123
Model: anthropic/claude-sonnet-4-6

## Description
A whiteboard with the text "HELLO WORLD" in blue marker, person's hand pointing to "WORLD".

## Objects Detected
- Object 1: Text "HELLO WORLD" at x=50, y=100, w=200, h=40 [confidence: 0.98]
- Object 2: Hand (person's) at x=150, y=80, w=80, h=120 [confidence: 0.87]

## Tags
whiteboard, text, hand, indoor
\\\

**source.json extension** (optional, for fast queries):
\\\json
{
  "id": "src_abc123",
  "kind": "image",
  "vision_model": "anthropic/claude-sonnet-4-6",
  "vision_tags": ["whiteboard", "text", "hand"],
  "width_px": 1920,
  "height_px": 1440
}
\\\

**No schema change required for Phase-MM-IMAGE stories.** Bounding boxes can land in the markdown as free text; structured queries come in Phase-U (plugin system).

---

## 6. Tool Surface Recommendation

### Image IN: Inline (no new tool, automatic at channel adapter level)

When Telegram user sends photo + text:
\\\
User: "describe this [photo attached]"
\\\

Adapter flow:
1. \photos.onPhoto\ → \source.Put()\ with \KindImage\.
2. \inbound.Normalize()\ → \InboundMessage.Attachments = [AttachmentRef{ID: "src_abc123", Kind: "image"}]\.
3. \invocation_builder.Build()\ → extracts image bytes via \source.FileResolver.Path()\ → base64 or data URL.
4. Passes to LLM client as \Message.ImageURLs = [...]\.
5. LLM reasons on the image inline.

**Status:** Wiring lives in \internal/channels/telegram/invocation_builder.go\ (Build method).

### Image IN: Vision Tool (optional, for stored sources)

\\\go
// analyze_image tool
// Input: source_id (of a KindImage source)
// Output: LLM-generated description + tags
\\\

**Not required for Phase-MM-IMAGE.** Nice-to-have for phase after (Phase-U plugin system).

### Image OUT: Generation Tool (required)

\\\go
// generate_image tool
// Input: prompt (string), style? (string), size? (enum: 256x256, 512x512, 1024x1024)
// Output: image bytes (base64) + metadata (model, seed, cost)
\\\

**Owned by:** \internal/agent/tools/registry/generate.go\ (new file).

**Integration points:**
- Wire into \egistry.go\ tool registration.
- Call Replicate API (prefer \al\ client lib for simplicity).
- Cost tracking: log to budget system (\internal/budget\).
- Return constraint: keep output <4KB base64 (image token budget).

---

## 7. Phase-MM-IMAGE User Stories

### Story 1: Wire Telegram photos to source store + inline vision

**Title:** \MM-IMAGE-001: Photo → source store → LLM vision inline\

**Acceptance Criteria:**
1. User sends photo in Telegram → stored as \KindImage\ source in \wiki/raw/src_<sha16>/\.
2. Metadata saved to \source.json\ with \Kind: image\, \MimeType: image/png\, \Width/Height\ (extracted from EXIF or image header).
3. Chat message → agent loop → LLM client converts to multipart (text + image_url).
4. LLM (Claude Sonnet 4.6 via OpenRouter) receives image inline.
5. Agent replies with description (test: golden fixture image with "HELLO WORLD" text → assert reply contains "hello" or "world").

**E2E Test Fixture:**
- **Input image:** 512×512 PNG, white background, blue text "HELLO WORLD" (center), saved as \	estdata/hello_world.png\.
- **Test flow:**
  \\\
  1. Telegram user sends fixture image + text "what does this say?"
  2. Handler stores → src_0123456789abcdef
  3. Chat loop → LLM gets image + "what does this say?"
  4. LLM responds (capture full text).
  5. Assert response contains "hello" (case-insensitive) or "world".
  \\\

**Scope:**
- \internal/telegram/photos.go\ (new file, ~100 LOC).
- \internal/telegram/handlers.go\ (add 1 line: \Handle(tele.OnPhoto, ...)\).
- \internal/llm/openai.go\ + \client.go\ (multipart support, ~40 LOC).
- \internal/channels/telegram/invocation_builder.go\ (extract image bytes, ~30 LOC).
- Test: \internal/telegram/photos_test.go\ + E2E fixture.

**Estimate:** 4–5 hours.

---

### Story 2: Implement \generate_image\ tool (Replicate Flux.1-schnell)

**Title:** \MM-IMAGE-002: generate_image tool → Replicate Flux.1-schnell\

**Acceptance Criteria:**
1. Agent can call \generate_image(prompt="a red cat on a blue chair")\.
2. Tool queries Replicate Flux.1-schnell API (4-step).
3. Returns image bytes (base64) + metadata (model, width, height, seed).
4. Cost logged to budget system (~\.003 per call).
5. Error handling: network timeout, quota exhaustion → graceful message (not crash).
6. E2E test: prompt "simple red circle on white" → assert image size ≈512px, valid PNG.

**Parameters:**
\\\json
{
  "prompt": {
    "type": "string",
    "description": "Image generation prompt. Be specific."
  },
  "style": {
    "type": "string",
    "description": "Optional style hint: 'photorealistic', 'illustration', 'abstract', etc."
  },
  "size": {
    "type": "string",
    "enum": ["256x256", "512x512", "1024x1024"],
    "description": "Output size (default: 512x512)"
  }
}
\\\

**Scope:**
- \internal/agent/tools/registry/generate.go\ (new file, ~150 LOC).
- \internal/config\ (new env: \REPLICATE_API_KEY\, \IMAGE_GEN_BACKEND=replicate\).
- Wire Replicate API client (use \github.com/replicate/replicate-go\ or HTTP).
- Budget tracking: call \udget.LogToolCost()\ (if interface exists; else add).
- Test: \internal/agent/tools/registry/generate_test.go\ with mock API.

**Estimate:** 3–4 hours.

---

### Story 3: E2E integration test — photo + generation roundtrip

**Title:** \MM-IMAGE-003: E2E test — photo upload → vision analysis → generate image based on description\

**Acceptance Criteria:**
1. User sends fixture image (whiteboard with "HELLO WORLD").
2. Agent analyzes → "I see text that says HELLO WORLD".
3. User prompts: "generate a logo inspired by what you see" → agent calls \generate_image\.
4. Agent receives image bytes → replies with generated image + description.
5. Full turn executes without errors; token budget respected.
6. Test asserts:
   - Source stored with \KindImage\.
   - Vision analysis executed (LLM received image).
   - Generation tool called (Replicate API mocked).
   - Final reply includes image reference.

**Scope:**
- \internal/channels/telegram/fixture/scenarios_test.go\ (extend existing fixture with image scenario).
- Fixture images: \hello_world.png\ (input), \mock_generated_logo.png\ (output).
- Mock Replicate API (via \httpmock\ or similar).
- Test runs full agent loop (streaming disabled for determinism).

**Estimate:** 2–3 hours.

---

## 8. Implementation Roadmap (Ralph Order)

\\\
Wave 1 (parallel):
  - MM-IMAGE-001: Telegram photos → source + inline vision
  - MM-IMAGE-002: generate_image tool

Wave 2 (dependent on Wave 1):
  - MM-IMAGE-003: E2E roundtrip test
\\\

**Total estimate:** 9–12 hours across 2–3 sessions.

---

## 9. Open Questions for the Operator

1. **Vision model choice:** Sonnet 4.6 (cheap, fast) vs. Opus 4.7 (slower, best quality)? Or configurable via \VISION_MODEL\ env?

2. **Local vision fallback:** Should \VISION_BACKEND=local\ + Qwen2.5-VL be wired in Phase-MM-IMAGE, or deferred to Phase-MM (later)?

3. **Image generation:** Replicate (simple, no new API key) vs. DALL-E 3 (better quality, requires OpenAI key)?

4. **Photo acceptance:** Accept all image formats (PNG, JPEG, WebP) or restrict to JPEG for Telegram compatibility?

5. **Vision analysis on ingest:** Should \ingest_source\ on a \KindImage\ automatically trigger vision analysis → \ision.md\, or only on explicit tool call?

6. **Bounding box citations:** Phase-MM-IMAGE (free text) or Phase-MM-CITATIONS (structured)?

7. **Cost budget:** Allocate separate monthly budget for vision/generation (e.g., \/month), or roll into agent budget?

---

## References & Context

- **PRD Phase-MM scope:** \prd.md\ §7.4 (multimodal core, audio IN highest ROI, image IN next).
- **Current source store:** \internal/storage/sources/store/source.go\ (KindImage reserved, Wave 2.9.5).
- **OCR pipeline analogue:** \internal/telegram/documents.go\ (validated pattern for async extraction).
- **LLM client:** \internal/llm/openai.go\ (OpenRouter wired; multipart content TBD).
- **Tool registry:** \internal/agent/tools/registry/source*.go\ (bounded-byte pattern, byte caps, error handling).
- **E2E test fixture:** \internal/channels/telegram/fixture/scenarios_test.go\ (existing chat simulation framework).

---

**Generated:** 2026-05-17 · Ready for Ralph queue