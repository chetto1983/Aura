# Design - Industrial multimodal asset pipeline

- **Date:** 2026-06-18
- **Status:** Draft (awaiting user review -> writing-plans)
- **Author:** Codex (brainstorming session with Davide)
- **Relates to:** Web cockpit chat, Telegram channel, native document ingestion, Phase 26 typed display protocol

## 1. Context and problem

Aura already has the hard part of document understanding: a native ingestion service that
extracts files, writes durable job state, indexes searchable document/chunk records in Neo4j,
and queues embeddings. The existing service is local-path based:
[`documents.Service.IngestPath`](../../../internal/documents/service.go) accepts PDF, XLSX,
XLSM, and DOCX; the job table lives in
[`0015_document_ingest_jobs`](../../../internal/db/migrations/0015_document_ingest_jobs.up.sql);
the Neo4j document schema lives in
[`0002_documents.cypher`](../../../internal/knowledge/migrations/0002_documents.cypher);
and the agent already has a `document_search` tool for indexed user documents.

Telegram is also already multimodal, but channel-specific. Its dispatch handles voice, photo,
and document updates in
[`bot_dispatch.go`](../../../internal/channels/telegram/bot_dispatch.go). Voice is downloaded
and sent to STT, photos are downloaded and sent to vision/OCR, and documents are either routed
to native document ingestion or converted through the MarkItDown sidecar. Today those paths read
file bytes into memory or temporary files and do not preserve the original file in a shared,
auditable asset store.

The web cockpit has the opposite shape: it has the polished assistant-ui chat runtime, but the
composer is text-only. [`Composer.tsx`](../../../web/src/chat/Composer.tsx) renders only input,
send, and stop. [`sseAdapter.ts`](../../../web/src/chat/sseAdapter.ts) posts only string user
content to `/agent/run`, and [`agui/server.go`](../../../internal/agui/server.go) explicitly
rejects structured multimodal user message content.

The missing industrial layer is therefore not "build document ingestion from scratch." It is a
shared asset pipeline that stores original files once, exposes a consistent lifecycle to web and
Telegram, reuses the existing processors, and gives the agent a protected text/search context
while retaining original object references for future true multimodal model calls.

## 2. Decisions locked in the brainstorm

| Topic | Decision |
|---|---|
| First product surface | Web cockpit uploads first, with Telegram treated as a first-class ingress in the same architecture |
| Implementation approach | **Asset Pipeline First**: shared asset store and processors before native multimodal model calls |
| Original storage | Garage-backed S3-compatible object storage, behind an Aura object-store abstraction |
| Browser upload path | Direct browser-to-Garage upload via short-lived Aura-issued presigned PUT |
| Modalities in first slice | Documents, images, and audio |
| Processing default | Hybrid: small image/audio process before the turn; large documents upload first and the turn starts once searchable |
| Searchability scope | Attachments belong to the current thread by default; user may opt in to save/promote to global library |
| Global save default | Opt in per attachment |
| Agent context | Backend-built protected attachment summary block before the user text |
| First-slice limits | Documents 100 MiB, images 25 MiB, audio 100 MiB, all env-configurable |
| UX | Gemini/Codex/Claude-style composer: paperclip/plus, drag/drop, paste, mic, chips before send, lifecycle cards after send |
| `/agent/run` posture | Keep Runner/user message content text-only in the first slice; attachment refs are resolved server-side into protected text |

## 3. Source notes

The design aligns with current official references:

- assistant-ui supports file attachments in React chat, including drag/drop, paste, adapters,
  progress updates, external-store runtime support, and server-upload adapters for large files:
  <https://www.assistant-ui.com/docs/guides/attachments>. The Gemini example shows the target
  style direction for a single-row pill composer with an add-attachment affordance:
  <https://www.assistant-ui.com/examples/gemini>.
- Garage documents S3 Signature v4, path/vhost-style URLs, and presigned URLs as implemented:
  <https://garagehq.deuxfleurs.fr/documentation/reference-manual/s3-compatibility/>.
- Telegram Bot API, current as of 2026-06-18, documents cloud `getFile` downloads up to 20 MB,
  `sendDocument` uploads up to 50 MB, and local Bot API server mode with unlimited downloads
  and uploads up to 2000 MB:
  <https://core.telegram.org/bots/api#getfile>,
  <https://core.telegram.org/bots/api#senddocument>,
  <https://core.telegram.org/bots/api#using-a-local-bot-api-server>.

## 4. Goals and non-goals

### Goals

1. Add a durable `asset` control plane that tracks original files, ownership, scope, object
   location, processor state, extracted summaries, document ids, and errors.
2. Add a Garage/S3-compatible object-store abstraction and default compose wiring for Garage.
3. Add web upload APIs for presign, finalize, status, retry, delete, and promote-to-library.
4. Add a web attachment UX using assistant-ui attachment primitives/adapters and Aura's existing
   chat design language.
5. Refactor Telegram media ingestion to stream into the same asset pipeline instead of owning a
   parallel byte/temp-file path.
6. Reuse existing document, image/OCR, and audio/STT processors behind common processor seams.
7. Build a protected attachment context block server-side and pass text-only content to the
   Runner and LLM loop.
8. Preserve original object references so later phases can call multimodal models with real
   file/image/audio handles rather than only extracted text.

### Non-goals for the first implementation slice

- No raw structured multimodal content inside AG-UI `Message.Content`.
- No direct LLM access to Garage credentials or arbitrary object URLs.
- No global library by default; promotion remains an explicit user action.
- No antivirus/DLP engine in the first slice. The design leaves a scan state hook, but the first
  pass relies on type/size validation, auth, processor sandboxing, and untrusted-content prompts.
- No full "Google Drive" document manager. The library surface is limited to save/promote,
  list/search later, and delete.
- No replacement of the existing document ingestion service. The first slice adapts it.

## 5. Architecture

The target architecture is one asset pipeline with multiple ingress adapters:

```
Web composer      Telegram media        future CLI/import
     |                 |                      |
     v                 v                      v
Aura asset API    Telegram adapter       import adapter
     |                 |                      |
     +-------> Postgres asset rows <----------+
                    |
                    v
          Garage/S3 original object
                    |
                    v
      modality processors and indexers
        |           |              |
        v           v              v
   documents     image OCR       audio STT
        |           |              |
        +------ normalized attachment context
                    |
                    v
       protected block + user text -> Runner.Turn
```

The central boundary is `internal/assets` (new). It owns asset metadata, lifecycle transitions,
object-store writes/reads, processor dispatch, and prompt-context projection. Web and Telegram
become ingress adapters. They may know how to receive files from their channel, but they do not
own long-term storage, state machines, or prompt injection rules.

The object store boundary is `internal/objectstore` (new). It exposes only the operations Aura
needs: presign PUT/GET when safe, server-side PUT from a stream, HEAD/stat, GET stream, delete,
and key construction. Garage is the default implementation, but the interface should stay
S3-compatible enough to swap MinIO/AWS later.

The processor boundary is `internal/assets/processors` or equivalent. It fans out by modality:
documents use `internal/documents`, images reuse/genericize the Telegram `photoClient` logic,
audio reuse/genericize the Telegram `voiceClient` logic, and all processors write normalized
asset results back to the asset store.

## 6. Data model

Add a Postgres migration for an asset control plane. Names are indicative; the implementation
plan can adjust exact SQL naming to match repo conventions.

### `aura.assets`

Core fields:

- `id uuid primary key`
- `identity_id uuid not null`
- `source_kind text not null` values such as `web`, `telegram`, `cli`
- `source_ref text` for Telegram file id/message id or external import id
- `thread_id text` nullable; present for thread-scoped uploads
- `scope text not null` values `thread`, `library`
- `modality text not null` values `document`, `image`, `audio`, `unknown`
- `status text not null`
- `file_name text not null`
- `mime_type text not null`
- `declared_size_bytes bigint`
- `size_bytes bigint`
- `content_hash text`
- `object_bucket text not null`
- `object_key text not null`
- `object_etag text`
- `document_id text` nullable; populated when document ingestion creates/searches a doc
- `summary text` nullable; OCR/transcript/document abstract
- `metadata jsonb not null default '{}'`
- `error_code text`, `error_message text`
- timestamps: `created_at`, `uploaded_at`, `accepted_at`, `processed_at`, `searchable_at`,
  `completed_at`, `deleted_at`

Indexes:

- `(identity_id, created_at desc)`
- `(thread_id, created_at asc)`
- `(identity_id, scope, created_at desc)`
- `(content_hash, identity_id)` for dedupe/reuse hints
- `(status, created_at)` for worker polling
- unique `(identity_id, object_key)`

### `aura.asset_events`

Optional but recommended for operator-grade debugging:

- `asset_id`
- `seq`
- `from_status`
- `to_status`
- `reason`
- `detail jsonb`
- `created_at`

This gives the UI lifecycle cards and logs a single source of truth instead of inferring from
processor logs.

### Status lifecycle

Statuses:

`created -> presigned -> uploaded -> accepted -> processing -> searchable -> embedding -> complete`

Terminal/side statuses:

`failed`, `refused`, `deleted`, `canceled`

Rules:

- `uploaded` means the object exists, not that it is safe/useful.
- `accepted` means server-side validation passed actual size/type checks.
- `searchable` means a document has enough extracted/indexed text for the agent to answer with
  citations. For image/audio, the analogous state is "summary/transcript ready"; they may jump
  to `complete`.
- `complete` means all configured background work finished.
- Upload acceptance and processing success are separate. A file can be accepted and later fail
  extraction/transcription.

## 7. Object storage and Garage

Garage runs as a compose sibling and is configured as Aura's default S3-compatible object store.
Aura holds the Garage keys server-side. Browser clients only receive short-lived presigned URLs
for a single object key and operation.

Object key shape:

```
identity/{identity_id}/asset/{asset_id}/original
```

The original filename stays in Postgres metadata, not the object key. This avoids path traversal,
encoding surprises, secret-bearing filenames in logs, and future rename churn.

Presigned PUT contract:

1. Browser asks Aura for a presign with filename, MIME type, declared size, modality hint, and
   thread id.
2. Aura authenticates the user, validates declared size against env-configured limits, creates an
   asset row, and returns `{asset_id, upload_url, method, required_headers, expires_at}`.
3. Browser uploads directly to Garage.
4. Browser calls finalize.
5. Aura HEADs the object and, when needed, streams it back from Garage to compute hash/sniff type.
6. Aura moves the asset to `accepted` or `refused`, then starts processing.

Presign TTL should be short, for example 5-15 minutes. The final server-side validation is
authoritative because client-declared size and type are not trusted.

Garage-specific constraints:

- Do not rely on S3 bucket ACLs/policies for app authorization. Aura owns authz in Postgres and
  only issues presigned URLs for assets the current identity can access.
- Prefer path-style URLs in local/dev compose unless vhost-style DNS is explicitly configured.
- Treat missing S3 features as a portability concern. The first slice needs PUT, GET, HEAD,
  DELETE, SigV4, and presigned URLs only.

## 8. Backend APIs

All web APIs live behind the existing web auth/capability boundary.

### Upload/control endpoints

- `POST /api/assets/presign`
  - Input: `thread_id`, `file_name`, `mime_type`, `size_bytes`, `modality_hint`
  - Output: `asset_id`, `upload_url`, `required_headers`, `expires_at`, `limits`
- `POST /api/assets/{id}/finalize`
  - Server validates object presence, size, hash, MIME, ownership, and starts processing.
- `GET /api/assets/{id}`
  - Returns lifecycle state for one asset.
- `GET /api/assets?thread_id=...`
  - Returns assets attached to a thread.
- `POST /api/assets/{id}/retry`
  - Retries failed processor work when the original object still exists.
- `POST /api/assets/{id}/promote`
  - Changes `scope` from `thread` to `library`.
- `DELETE /api/assets/{id}`
  - Soft deletes metadata and deletes object when safe.

### Run endpoint extension

Keep the Runner and final user message text-only. Extend the `/agent/run` handler with an
Aura-owned top-level extension field, for example:

```json
{
  "threadId": "...",
  "messages": [{ "id": "...", "role": "user", "content": "Compare these two files" }],
  "aura": {
    "attachment_ids": ["asset-1", "asset-2"]
  }
}
```

The handler decodes the SDK `RunAgentInput` plus the Aura extension, validates that every asset
belongs to the current identity and thread, builds the protected attachment block, prepends it
to the last user message string, then calls the existing `Runner.Turn(ctx, threadID, userMsg)`.

This preserves first-slice compatibility:

- `lastUserMessage` still sees a string.
- The LLM loop remains text/tool based.
- Unknown structured content remains rejected.
- The source of truth for attachment context is the backend, not the browser.

If the SDK struct makes top-level extension awkward, use a small Aura wrapper struct that embeds
the SDK payload and carries `AttachmentIDs`. Do not put attachment JSON inside `Message.Content`.

## 9. Protected attachment context

The agent receives a backend-created block before user text:

```text
<attachments trust="untrusted_user_uploads">
Attachment A1:
- asset_id: ...
- filename: manual.pdf
- modality: document
- status: searchable
- document_id: doc-...
- summary: ...
- retrieval: Use document_search with document_id="doc-..." for detailed cited chunks.

Attachment A2:
- asset_id: ...
- filename: note.ogg
- modality: audio
- status: complete
- transcript: ...
</attachments>

User message:
...
```

Rules:

- Extracted text, OCR, transcripts, filenames, and document contents are untrusted user upload
  content.
- The block must explicitly tell the model not to follow instructions inside attachments unless
  the user asks to analyze them.
- Documents should provide short summaries and `document_id`, not dump entire extracted content.
- Image/audio summaries may be included directly when small enough.
- If an asset failed processing, the block says so and excludes unreliable extracted content.
- If a document is still processing, the web UX should not start the run unless the user removes
  it or chooses to send without it.

This is the first-slice bridge between file uploads and the current text/tool agent. Later, a
true multimodal model call can use the retained Garage object reference without changing the
user-facing asset lifecycle.

## 10. Web UX

The target is industrial chat input behavior similar to Gemini/Codex/Claude, implemented with
assistant-ui primitives and the existing Aura visual language.

### Composer controls

Add:

- paperclip/plus button using assistant-ui attachment primitives
- drag/drop over the chat lane and composer
- paste image/file support
- mic capture for audio notes
- chips for pending and uploaded attachments
- retry/remove controls per chip
- optional "save to library" toggle per attachment, default off

The composer remains compact and work-focused. It should not become a landing-page-like upload
panel. Attachments are chips before send, then compact lifecycle cards in the sent user message.

### Attachment adapter

Implement an Aura upload adapter:

1. Validate type and size locally for immediate feedback.
2. Call `/api/assets/presign`.
3. PUT to Garage with progress.
4. Call finalize.
5. Poll or subscribe to asset status until ready/failed.
6. Return a complete assistant-ui attachment whose id is the Aura `asset_id`.

Large files must not be base64'd into browser memory. assistant-ui's server-upload adapter pattern
fits this: upload to storage and pass a reference.

### Send behavior

- No attachments: unchanged.
- Images/audio: small uploads process before the turn; send is blocked while required processing
  is running.
- Documents: upload finalizes first, then processing starts; send starts only when documents are
  `searchable`, unless the user removes the file or explicitly sends without failed assets.
- Failed assets remain visible with retry/remove. They are not silently omitted.

### Replay and display integration

Thread history should include asset references in user turns. On replay, user message attachments
render as read-only lifecycle cards. Phase 26's typed display router is adjacent: it can later
render richer document/image/audio cards, but upload correctness must not depend on typed display
completion.

## 11. Telegram ingress

Telegram should stop being a separate multimodal island. It becomes another asset ingress:

1. Receive `OnVoice`, `OnPhoto`, or `OnDocument`.
2. Resolve Telegram file metadata and file id.
3. Create an Aura asset row with `source_kind="telegram"` and `source_ref` containing file id,
   message id, chat id, and Telegram user id.
4. Stream the Telegram file reader into Garage through the server-side object-store PUT path.
5. Finalize the same asset.
6. Let the shared processor pipeline produce transcript/OCR/document index.
7. Start the turn using the same protected attachment context.

This keeps Telegram UX features but removes duplicate storage and processing decisions.

### Standard Bot API and local Bot API

With the standard cloud Bot API, Aura is limited by Telegram `getFile` download behavior. As of
2026-06-18, `getFile` supports bot downloads up to 20 MB and `sendDocument` supports sending
files up to 50 MB. Aura's 100 MiB document/audio limits therefore apply to web and to Telegram
only when the bytes are reachable.

For industrial Telegram deployments, add optional local Bot API server config:

- `TELEGRAM_API_BASE_URL`
- `TELEGRAM_FILE_BASE_URL`
- `AURA_TELEGRAM_LOCAL_BOT_API=true`

When enabled, Telegram file paths may be local paths or local-server URLs. The adapter should
stream them into Garage and can honor Aura's 100 MiB first-slice limits, or higher future limits
up to the local Bot API server envelope.

### Telegram UX

- Acknowledge accepted assets immediately, for example "Documento ricevuto, lo sto elaborando".
- Edit/send lifecycle status when processing changes.
- On searchable/complete, start the agent turn or tell the user the asset is ready for questions.
- Offer "save to library" through an inline button for thread-scoped uploads.
- Do not inject full markdown conversion as a raw Telegram message. The agent sees the same
  protected block and can call `document_search`.

## 12. Processors

### Documents

Reuse `internal/documents` instead of replacing it. The industrial adaptation is either:

- add `IngestObject(ctx, req, objectRef)` that streams the Garage object to a bounded temp file
  and calls `IngestPath`, or
- add a more general `IngestReader` path if the extractor/client can stream multipart without
  needing a filesystem path.

First implementation should favor the smallest safe adapter around `IngestPath`, then refactor
to reader-native only if temp files become a real bottleneck.

The asset row stores `document_id` once available. The agent context tells the model to use
`document_search` with that id for detailed recall and citations.

### Images

Extract the reusable logic from Telegram photo handling into a package that can process any
asset object stream. It should:

- downscale oversized images as the current Telegram path does,
- send image bytes to the local OCR/vision sidecar or configured cloud vision route,
- store a short description/OCR text in `assets.summary`,
- optionally store dimensions/thumbnail metadata later.

### Audio

Extract reusable STT logic from Telegram voice handling. It should:

- stream the original object to the STT sidecar as multipart,
- preserve language/model knobs from existing config,
- store transcript text in `assets.summary` or `metadata.transcript`,
- support browser mic audio and Telegram voice notes through the same processor.

### Worker model

First slice can process immediately after finalize in a goroutine if it mirrors existing daemon
style. The design should still expose a pollable state machine so a later worker table/queue can
replace goroutines without changing web/Telegram clients.

## 13. Config and compose

New config fields:

- `AURA_OBJECTSTORE_BACKEND=garage|s3|filesystem-dev`
- `AURA_OBJECTSTORE_ENDPOINT`
- `AURA_OBJECTSTORE_REGION`
- `AURA_OBJECTSTORE_BUCKET`
- `AURA_OBJECTSTORE_ACCESS_KEY`
- `AURA_OBJECTSTORE_SECRET_KEY`
- `AURA_OBJECTSTORE_PATH_STYLE=true`
- `AURA_OBJECTSTORE_PUBLIC_ENDPOINT` for browser presigned URLs when Garage is behind Caddy
- `AURA_ASSET_MAX_DOCUMENT_BYTES=104857600`
- `AURA_ASSET_MAX_IMAGE_BYTES=26214400`
- `AURA_ASSET_MAX_AUDIO_BYTES=104857600`
- `AURA_ASSET_PRESIGN_TTL_SEC=600`
- `AURA_ASSET_PROCESSING_CONCURRENCY`
- optional Telegram local Bot API fields listed above

Compose should add Garage as a sibling service with a persistent volume and local-only defaults.
Caddy or the existing web reverse-proxy layer should expose the presigned public endpoint only
when needed by the browser. Garage credentials stay in env and are never sent to clients.

## 14. Security and trust boundaries

- Authz is identity-based. Every asset read/write checks current identity and thread/scope.
- Object keys are server-generated. Client filenames never become paths.
- Presigned URLs are short-lived and single-object. Aura validates the object after upload.
- Actual size/type validation happens server-side after upload. Declared browser metadata is only
  a hint.
- All extracted content is untrusted prompt content. The protected block must label it as such.
- The agent should not receive raw Garage credentials or broad presigned URLs.
- Delete must revoke app access immediately by marking the asset deleted, then best-effort delete
  object storage.
- CORS for Garage/browser uploads should allow only the cockpit origin and required headers.
- Logs should include asset ids and status transitions, not raw filenames when avoidable and not
  presigned URLs.
- Rate limits and quotas should exist per identity: concurrent uploads, total active bytes, and
  processor concurrency.
- Processor sidecars remain isolated boundaries for parsing/OCR/STT. Their errors become
  sanitized asset errors, never raw stack traces in the UI.

## 15. Error handling

| Failure | Behavior |
|---|---|
| Presign validation rejects type/size | Composer chip shows refused state; no object row beyond refused metadata if created |
| Browser PUT fails | Chip shows upload failed with retry; no processing starts |
| Finalize cannot find object | Asset becomes failed/refused; user can retry upload |
| Actual object exceeds limit | Asset refused; object deleted best-effort |
| Unsupported document type | Asset refused with supported-type copy |
| Processor timeout/failure | Asset failed; retry allowed if original object exists |
| Document extraction succeeds but embedding fails | Asset/document can be `searchable`; embedding status remains failed/pending |
| Telegram download unavailable | Telegram sends fail copy; no turn starts |
| Local Bot API not configured for large Telegram file | Telegram explains size/path limit and asks for web upload or smaller file |
| User sends while asset processing | Web blocks with visible processing state; Telegram sends accepted/processing status |

The UX rule is: never make the user wonder whether the file was accepted, processing, searchable,
failed, or omitted.

## 16. Testing and verification

### Backend unit tests

- asset store lifecycle transitions, ownership checks, status history, soft delete
- object-store fake for presign/finalize/head/get/delete
- size/type validation and server-side mismatch handling
- protected attachment block builder, including prompt-injection labeling
- `/agent/run` extension validation with string-only final content
- document adapter maps asset object to `documents.IngestRequest` and stores `document_id`
- image/audio processors with fake sidecar servers
- Telegram adapter streams to object-store fake without `io.ReadAll`

### Backend integration tests

- Postgres migration and sqlc queries for assets/events
- Garage/local S3 integration tier: presign -> PUT -> finalize -> GET/HEAD
- document ingestion E2E from Garage object to `document_search`
- Telegram standard small-file path with fake telebot filer
- optional local Bot API path for larger file metadata/path behavior

### Web tests

- attachment adapter validates type/size and calls presign/finalize in order
- upload progress and retry/remove states
- drag/drop, paste, and mic capture state transitions
- send blocked while required processing is pending
- sent user message renders read-only attachment lifecycle cards
- replay rehydrates assets from thread snapshot/API
- a11y: native controls, focus, labels, 44px targets, no text overlap

### E2E/manual gates

- Web: upload a PDF, wait for searchable card, ask a question, see `document_search` used.
- Web: paste an image, get OCR/description, ask about it.
- Web: record mic audio, get transcript, ask follow-up.
- Telegram: send voice/photo/document and confirm each lands in the shared asset table and
  object store.
- Garage: restart Aura with objects persisted and thread replay still works.

## 17. Implementation boundaries

Suggested implementation order for the later writing-plans phase:

1. Asset schema, store, object-store interface, Garage config/compose.
2. Web presign/finalize/status APIs with fake object-store tests and Garage integration tier.
3. Document asset processor using existing `documents.Service`.
4. Protected attachment block builder and `/agent/run` extension.
5. Web composer attachment adapter and lifecycle chips/cards.
6. Generic image/audio processors extracted from Telegram clients.
7. Telegram adapter refactor to stream into the shared asset pipeline.
8. Promote-to-library and delete/retry polish.

This order makes the shared substrate real before the UI and Telegram refactor lean on it.

## 18. Interaction with current Phase 26 display work

The current working tree contains typed display routing/pagination work under
`web/src/chat/displays`. That is complementary but separate. The asset pipeline should not wait
for rich display cards. First-slice attachments can render with ordinary React components and
later switch to typed display payloads when Phase 26 per-type cases mature.

The backend may eventually emit `aura.display` payloads for asset lifecycle or document cards,
but upload, storage, processing, and protected prompt context must remain backend-owned even if
the display router is not ready.

## 19. Open risks and chosen defaults

- **Scope size:** This is bigger than a single small patch. The implementation plan should split
  it into substrate, web, processors, and Telegram waves.
- **Presigned PUT size enforcement:** Client-declared size is not enough. Server-side finalize is
  the hard gate.
- **Telegram >20 MB inbound files:** Standard Bot API cannot satisfy Aura's 100 MiB target. The
  design supports larger Telegram files only through optional local Bot API server mode.
- **Processor temp files:** Reusing `IngestPath` through bounded temp files is acceptable first.
  Refactor to reader-native ingestion only when justified.
- **Prompt injection from documents:** Attachment content is always untrusted and fenced in a
  protected block; document detail should be retrieved through `document_search`, not pasted
  wholesale into the prompt.
- **Global library creep:** Save/promote is explicit. Thread attachment behavior ships first.

## 20. Acceptance criteria

The design is implemented when:

1. Web can attach document/image/audio files without base64-loading large files into browser
   memory.
2. Original files are stored in Garage and tracked in Postgres asset rows.
3. Document assets become searchable through the existing document pipeline and
   `document_search`.
4. Image/audio assets produce OCR/description/transcript summaries through shared processors.
5. `/agent/run` remains text-only at the Runner boundary while receiving backend-built attachment
   context.
6. Telegram media and web uploads land in the same asset/object pipeline.
7. Thread replay shows sent attachments and their lifecycle states.
8. Failed uploads/processors are visible and retryable; they are never silently omitted.
9. Assets are identity-scoped, short-lived presigns are used, and Garage credentials never reach
   the browser.
