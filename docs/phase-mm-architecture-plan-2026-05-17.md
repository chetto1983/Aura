# Phase-MM Architectural Substrate Plan
**Date:** 2026-05-17  
**Scope:** Identify architectural changes needed in Aura's substrate to support multimodal input/output (audio + image) before feature-specific implementation.  
**Status:** Pre-implementation research. No code changes. This document defines the boundary between substrate (core changes) and plugin concerns (model selection).

---

## Executive Summary

Aura's substrate is **text-centric by accident, not design**. The core abstractions (InboundMessage, llm.Message, conversation.Context, source store) are shaped around text because no multimodal work has landed yet. Phase-MM requires **minimum viable substrate changes** in three areas:

1. **InboundMessage.Attachments** — formalize how Telegram voice/photo/document uploads enter the system (already partially present via ChannelData).
2. **llm.Message.Content shape** — OpenAI/Anthropic vision APIs require multipart content (text + image_url + audio_url), not a single Content string.
3. **Source store extensions** — audio/image bytes need the same immutable storage + extraction pipeline (transcript for audio, OCR/vision for images) that PDFs already have.

**Gateway principle:** Once these three substrate pieces land, the next four features (Whisper transcription, vision analysis, TTS, generation) are tool-level work. The system will transparently handle multimodal messages without feature-level breaking changes.

Everything else (which Whisper variant, which vision model, whether to run locally or call an API, system prompt awareness of capabilities) remains a **plugin concern** — configurable via TOOLS.md, MCP, or dashboard settings.

**Backward compatibility:** All changes are additive. Existing text-only messages, tool results, and prompts continue to work unchanged.

---

## 1. Text-Only-Assumption Inventory

### 1.1 InboundMessage (chat/types.go)

**Current state:**
- `Text string` — the only user-supplied content.
- `Attachments []AttachmentRef` field exists but is **not populated by Telegram adapter**.
- `ChannelData map[string]any` carries Telegram upload metadata out-of-band; adapters treat this as opaque to the core.

**Assumption:**
- Agent loop never sees attachment bytes, only source IDs (via store_source tool).
- Text is always present and sufficient to route the message.

**Implication for MM:**
- Attachments must be **normalized by InboundAdapter.Normalize()** (not left as channel-specific metadata).
- Telegram voice/audio messages will be converted to AttachmentRef at the adapter boundary, not passed to the agent as ChannelData.

### 1.2 llm.Message.Content (llm/client.go)

**Current state:**
```go
type Message struct {
    Role      string
    Content   string  // <-- single string, no multipart support
    ToolCalls []ToolCall
    ToolCallID string
}
```

**Assumption:**
- Content is always plain text (or serialized tool-call metadata).
- Vision/audio APIs are not integrated; no image_url, audio_url, or image content types.

**Implication for MM:**
- OpenAI and Anthropic vision APIs expect `[{"type": "text", "text": "..."}, {"type": "image_url", "image_url": {"url": "..."}}]` in the wire JSON.
- Current openai.go serializes Content to a single `{"role": "user", "content": "text"}` JSON object.
- **Backward-compat challenge:** Changing Content from string to []ContentPart breaks every callsite.

### 1.3 conversation.Context (conversation/context.go)

**Current state:**
```go
type Context struct {
    messages []llm.Message
    ...
}

func (c *Context) AddAssistantMessage(content string)
func (c *Context) AddToolResultMessage(toolCallID, content string)
```

**Assumption:**
- All message content flows through string parameters.
- Context builds the full prompt as-is; no per-message content transformation.

**Implication for MM:**
- Once llm.Message.Content becomes multipart, these adders must handle both text and non-text.
- ConversationContext is a **consumer, not a producer** of this complexity — the serialization happens in the adapter (openai.go).

### 1.4 Source Store (storage/sources/store/source.go, store.go)

**Current state:**
- Kind enum: pdf, text, markdown, json, csv, url, xlsx, docx, pptx, epub, html, zip, image, pdf_generated, sandbox_artifact.
- **Status: extracting → ocr_complete → ingested** (pipeline for PDFs only).
- `Extract *ExtractionMeta` (extractor name, version, text bytes, page count, warnings).

**Assumption:**
- Only PDFs trigger OCR pipeline (internal/storage/sources/ocr, Mistral Document AI).
- Other formats (docx/xlsx/pptx/epub/html/zip) go through markitdown (libmagic-based text extraction).
- **Audio and image uploads are rejected at the boundary today.**

**Implication for MM:**
- Audio: needs `Kind = "audio"` and an extraction path (Whisper → transcript.md).
- Image: needs `Kind = "image"` (already in enum but unused) and OCR/vision path.
- Both should follow the pattern: store bytes → extract/transcribe → produce markdown → ingest → wiki summary.

---

## 2. Substrate vs. Plugin Classification

| Piece | Layer | Reason | Landing Path |
|-------|-------|--------|--------------|
| InboundMessage.Attachments population | **SUBSTRATE** | Telegram/web adapters must normalize uploads before agent sees them. All channels need this. | chat/types.go + channels/telegram/inbound.go |
| llm.Message multipart Content | **SUBSTRATE** | OpenAI/Anthropic vision APIs require it. Cannot be plugin logic — it's wire-format forced by external APIs. | llm/client.go + llm/openai.go |
| Transcript extraction from audio bytes | **PLUGIN** | Which transcription service (Whisper local vs API vs Google Speech API) is configurable. Core stores the bytes; tool decides the transcriber. | agent/tools/registry (new `transcribe_audio` tool) |
| Vision description of image bytes | **PLUGIN** | Which vision model (Claude, GPT-4V, LLaVA local, etc.) is configurable. Core stores the bytes; tool decides the model. | agent/tools/registry (new `analyze_image` tool) |
| Audio playback / TTS generation | **PLUGIN** | Which TTS engine and voice profile are configurable. | agent/tools/registry (new `generate_audio` tool) |
| Image generation | **PLUGIN** | Which generative model is configurable. | agent/tools/registry (new `generate_image` tool) |
| Source store audio/image handling | **SUBSTRATE** | Immutable storage, deduplication, and extraction pipeline are core concerns. Must work identically to PDF storage. | storage/sources/store + new extraction sidecar wiring |
| System prompt awareness of multimodal | **PLUGIN** | TOOLS.md can say "you can see images" without changing the agent core. | Prompt overlays + capability gates |
| Capability gates (media.vision.read, media.audio.read) | **PLUGIN** | Gates are policy, not substrate. But infrastructure for capability checks is SUBSTRATE (identity/grants). | identity/grants + agent/tools registry |

---

## 3. llm.Message.Content Shape Change

### 3.1 Current Wire Shape (openai.go)

```json
{
  "role": "user",
  "content": "Tell me about this image"
}
```

### 3.2 Future Wire Shape (with vision support)

```json
{
  "role": "user",
  "content": [
    { "type": "text", "text": "Tell me about this image" },
    { "type": "image_url", "image_url": { "url": "https://..." } }
  ]
}
```

### 3.3 Backward-Compat Strategy (Recommended): Additive Field, Dual Serialization

Add `MultipartContent []ContentPart` field. Serialization logic: if MultipartContent non-empty, serialize as array; else use Content string (legacy).

Callsites unchanged; new path uses new field. Existing tests pass.

---

## 4. InboundMessage.Attachments Design

The `AttachmentRef` struct is ready as-is. Status enum already supports the pipeline (queued → uploading → stored → extracting → extract_complete → ingested).

**Change needed in Telegram adapter (channels/telegram/inbound.go):**
Populate `InboundMessage.Attachments` by extracting Telegram file metadata (Document, Voice, Photo) into AttachmentRef slice with Status="queued".

**Hub flow:**
1. Receive InboundMessage with Attachments.
2. Async: for each queued Attachment, download Telegram file → store.Put → update SourceID, Status → emit EventAttachmentStatus.
3. Agent sees InboundMessage.Attachments with resolved SourceIDs.

---

## 5. Source-Ingestion Lifecycle for Non-Text

### Audio Lifecycle (Proposed)

```
Telegram voice message in InboundMessage.Attachments
  → Hub: store.Put(Kind=KindAudio, Bytes) [sync]
  → Agent turn: transcribe_audio tool calls store.Read(sourceID)
    → Whisper → transcript text in context THIS TURN
  → [Optional] async: store.Update(Status=extracting) 
    → Whisper → transcript.md
    → ingest pipeline → wiki summary
    → Status: stored → [extracting] → ingested
```

### Image Lifecycle (Proposed)

```
Telegram photo in InboundMessage.Attachments
  → Hub: store.Put(Kind=KindImage, Bytes) [sync]
  → Agent turn: analyze_image tool calls store.Read(sourceID)
    → Vision API → description in context THIS TURN
  → [Optional] async: store.Update(Status=extracting)
    → Vision describe → vision_description.md
    → ingest pipeline → wiki summary
    → Status: stored → [extracting] → ingested
```

**Key:** Both inline consumption (immediate turn) AND optional background ingestion (long-term memory).

---

## 6. Cross-Cutting Concerns

### Capability Gates

Add media.audio.read, media.audio.generate, media.image.read, media.image.generate. No substrate change needed — capability infrastructure already exists.

### Privacy: Tool Argument Logging

Existing rule: log argument KEYS only, never values. For multimodal: SourceID values are opaque (safe to log); transcription/vision results redacted like other tool results.

### Cost Tracking

Vision/audio calls have separate costs from token budget. Existing `governance.Tracker` handles this; just wire tool execution to it.

---

## 7. Architectural Phase-MM Stories

### US-MM-ARCH01: Extend InboundMessage.Attachments + Telegram adapter

Normalize Telegram file uploads (voice/photo/document) into AttachmentRef. Hub downloads files → store.Put → updates SourceIDs → emits EventAttachmentStatus.

### US-MM-ARCH02: Extend llm.Message with MultipartContent + openai.go serializer

Add `MultipartContent []ContentPart`. Serialization: if present, emit array; else use Content string (legacy). Backward compatible.

### US-MM-ARCH03: Source store Kind enum extensions for audio/image

Verify Kind enum includes KindAudio (new), KindImage (exists but unused). Extend formats.go to handle audio/image file extensions.

---

## 8. Summary: Changes Per File

| File | Type | Change | Backward Compat |
|------|------|--------|-----------------|
| internal/chat/types.go | OutboundEvent | Optional Attachments field (additive) | ✓ |
| internal/llm/client.go | Message | Add MultipartContent []ContentPart field | ✓ |
| internal/llm/openai.go | Send/Stream | Detect MultipartContent; serialize as array if present | ✓ |
| internal/channels/telegram/inbound.go | Normalize | Call normalizeAttachments(); populate InboundMessage.Attachments | ✓ |
| internal/storage/sources/store/source.go | Kind | Add KindAudio; extend ExtractionMeta | ✓ |
| internal/storage/sources/store/formats.go | OriginalFilenameForKind | Extend for audio/image extensions | ✓ |

---

## 9. Open Questions

1. Inline vs. background ingest for audio/image? (Recommend: both.)
2. Which transcription/vision service? (Plugin concern; Phase-U or TOOLS.md.)
3. Audio input Telegram-only or web too? (Recommend: Telegram first.)
4. Attachment privacy in run_events? (Recommend: log SourceID + filename; suppress transcript/description.)
5. Who enforces media capability gates — tool or source reader? (Recommend: tool level.)

---

## Conclusion

Phase-MM substrate work = **3 focused pieces**:

1. Attachments normalization (InboundMessage + Telegram adapter).
2. Multipart content serialization (llm.Message + openai.go).
3. Source store multimedia support (Kind enum + extraction metadata).

Everything else is either already present or a plugin concern. Next 2-3 stories unblock feature work. Then Phase-U (plugin layout) and Phase 8 (multi-agent).
