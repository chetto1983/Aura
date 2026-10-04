# Aura Asset Pipeline

The asset pipeline lets operators attach documents, images, audio and video to Aura
from the web cockpit or Telegram. Aura stores the original file in an
S3-compatible object store, tracks lifecycle state in Postgres, and hands each
modality to its processor: the ingest sidecar indexes documents, the vision route
summarizes images, and speech-to-text transcribes audio.

Use this guide when you configure local development, bring up Garage, or debug an
asset that is stuck in the UI.

For [Cloudflare Remote Access](cloudflare-remote-access.md), browser presigns use the request's
public HTTPS origin. Cloudflared reaches Caddy's private HTTP listener, which forwards the
HTTPS browser scheme to Aura and shares the direct frontdoor's per-identity Garage routes.
Uploads, finalization, downloads and Studio saves must work on that same public hostname after
Access OTP and Authula. No Garage service/admin port is published through WARP. An HTTP URL or
private Docker hostname in a browser presign is a failure, not a reason to disable TLS checks.

## Components

- `internal/assets` owns asset metadata, status transitions, retry, promote, and
  delete.
- `internal/objectstore` hides the storage backend. The default production-like
  backend is Garage/S3. `filesystem-dev` is available for local backend tests.
- Postgres stores `aura.assets` and `aura.asset_events`.
- Garage stores the original object bytes.
- Documents are indexed by the ingest sidecar (CocoIndex with Apache Tika), which
  reconciles the identity's bucket into ArcadeDB. The asset's own document step only
  names the object the way the index names it (`internal/assets/document_processor.go`).
- The image processor calls the configured vision endpoint.
- The audio processor calls the configured STT endpoint.

When a user sends attachments, Aura validates the asset ids and builds a protected
context block on the backend, prepended to the user message. Images and video also
reach the model as native content parts (`internal/assets/turn_media_loader.go`).

## Required Environment

Object storage:

| Variable | Purpose | Default |
| --- | --- | --- |
| `AURA_OBJECTSTORE_BACKEND` | Storage backend: `garage`, `s3`, `filesystem-dev`, or `fake` | `garage` |
| `AURA_OBJECTSTORE_ENDPOINT` | Internal S3-compatible endpoint used by Aura | `http://127.0.0.1:3900` locally, `http://garage:3900` in Compose |
| `AURA_OBJECTSTORE_PUBLIC_ENDPOINT` | Host rewrite for browser-visible presigned URLs; behind a proxy the request's own origin wins | `http://127.0.0.1:3900` in Compose |
| `AURA_OBJECTSTORE_REGION` | S3 signing region | `garage` |
| `AURA_OBJECTSTORE_BUCKET` | Bucket for original asset objects | `aura-assets` |
| `AURA_OBJECTSTORE_ACCESS_KEY` | S3 access key | empty in `.env.example`; `install.sh` generates it |
| `AURA_OBJECTSTORE_SECRET_KEY` | S3 secret key | empty in `.env.example`; `install.sh` generates it |
| `AURA_OBJECTSTORE_PATH_STYLE` | Use path-style S3 URLs, required by Garage defaults | `true` |
| `GARAGE_RPC_SECRET` | 32-byte hex RPC secret used by the Garage node | empty in `.env.example`; `install.sh` generates it |

The compiled fallbacks for the access and secret key are development values only; a strict
deployment profile refuses them (`internal/config/config_validate.go`).

Asset limits:

| Variable | Purpose | Default |
| --- | --- | --- |
| `AURA_ASSET_MAX_DOCUMENT_BYTES` | Maximum document size (pdf, docx, pptx, xlsx, xlsm, html, htm, csv, md, markdown, txt, json, xml, epub) | `104857600` |
| `AURA_ASSET_MAX_IMAGE_BYTES` | Maximum image size | `26214400` |
| `AURA_ASSET_MAX_AUDIO_BYTES` | Maximum audio size | `104857600` |
| `AURA_ASSET_MAX_VIDEO_BYTES` | Maximum video size (mp4, webm) | read at boot from the cockpit's Settings |
| `AURA_ASSET_PRESIGN_TTL_SEC` | Presigned upload URL lifetime | `600` |
| `AURA_ASSET_PROCESSING_CONCURRENCY` | Asset worker width knob | `2` |

Processor endpoints:

| Variable | Purpose |
| --- | --- |
| `MULTIMODAL_BASE_URL` and `MULTIMODAL_MODEL` | Local OCR/vision endpoint and model |
| `STT_BASE_URL`, `STT_MODEL`, `STT_LANGUAGE` | Speech-to-text endpoint, model, and language hint |

Telegram:

| Variable | Purpose |
| --- | --- |
| `TELEGRAM_BOT_TOKEN` | Bot token |
| `TELEGRAM_API_BASE_URL` | Optional local Bot API base URL |
| `TELEGRAM_FILE_BASE_URL` | Optional local Bot API file base URL |
| `AURA_TELEGRAM_LOCAL_BOT_API` | Set `true` when using a local Bot API server |

## Garage Startup Notes

Start the local stack pieces that the asset pipeline needs:

```powershell
docker compose up -d postgres garage aura-ocr-vl aura-stt
bash scripts/garage_bootstrap.sh
go run ./cmd/aura db migrate
```

The Compose service starts Garage with `docker/garage/garage.toml`.
`scripts/garage_bootstrap.sh` assigns the single local node, creates the bucket
from `AURA_OBJECTSTORE_BUCKET`, imports the credentials from
`AURA_OBJECTSTORE_ACCESS_KEY` / `AURA_OBJECTSTORE_SECRET_KEY`, and grants the key
read/write/owner access to the bucket.

For a quick local backend check before Garage is bootstrapped, use:

```powershell
$env:AURA_OBJECTSTORE_BACKEND='filesystem-dev'
$env:AURA_OBJECTSTORE_ENDPOINT='file:///C:/tmp/aura-assets'
```

Do not use `filesystem-dev` for browser upload smoke tests. Its presigned URL is
a local `file://` URL, while the web upload flow expects an HTTP(S) endpoint.

When Aura runs inside Docker and the browser runs on the host, presigned URLs must not
name `http://garage:3900`, which resolves only inside Compose. Compose already sets
`AURA_OBJECTSTORE_PUBLIC_ENDPOINT=http://127.0.0.1:3900`; behind a proxy the browser
request's own origin is used instead (`internal/agui/assets_api.go`).

## Web Upload Flow

1. The cockpit calls `POST /api/assets/presign` with the file name, MIME type,
   size, thread id, and modality hint.
2. Aura creates an asset row and returns a presigned upload URL plus
   `required_headers`.
3. The browser uploads the file with those exact headers. The signature covers
   `Content-Type` and the `x-amz-meta-*` headers, so a client that drops one fails with
   a signature mismatch.
4. The cockpit calls `POST /api/assets/{id}/finalize`.
5. Aura verifies the object, applies size/type limits, marks the asset
   `accepted`, and starts processing.
6. The cockpit polls `GET /api/assets/{id}` until the asset is ready or terminal.
7. `/agent/run` sends `aura.attachment_ids`; Aura builds the protected attachment
   context server-side.

Thread replay uses `GET /api/assets?thread_id=...` and renders asset cards next
to the matching user turns. Operators can retry failed assets, promote ready
assets to the library, or delete an asset from the thread.

## Telegram Flow

Telegram media now streams through the same asset service as web uploads.
Documents, photos, and voice notes create `source_kind="telegram"` asset rows,
write the original object to the object store, and reuse the same processors.

The standard Telegram Bot API
[`getFile`](https://core.telegram.org/bots/api#getfile) endpoint currently
documents a 20 MB download limit. For larger received files, run a
[local Bot API server](https://tdlib.github.io/telegram-bot-api/) and set
`TELEGRAM_API_BASE_URL`, `TELEGRAM_FILE_BASE_URL`, and
`AURA_TELEGRAM_LOCAL_BOT_API=true`. Telegram's local Bot API server documents
larger local file handling, including uploads up to 2000 MB.

Keep Aura's own asset limits in mind. A local Bot API server can make a large
file reachable, but Aura will still refuse it if it exceeds
`AURA_ASSET_MAX_DOCUMENT_BYTES`, `AURA_ASSET_MAX_IMAGE_BYTES`, or
`AURA_ASSET_MAX_AUDIO_BYTES`.

## Smoke Test

Use the smoke script against a live Aura HTTP server and an HTTP(S) object store:

```bash
bash scripts/asset_smoke.sh path/to/sample.pdf
```

Useful overrides:

```bash
export AURA_BASE_URL=http://127.0.0.1:9080
export AURA_ASSET_SMOKE_THREAD_ID=asset-smoke
export AURA_ASSET_SMOKE_MIME=application/pdf
```

Aura web auth is Authula-only. Pass an authenticated Authula cookie jar with
`AURA_ASSET_SMOKE_COOKIE_JAR=/path/to/cookies.txt`, or pass a complete Cookie
header with `AURA_ASSET_SMOKE_COOKIE`.

## Troubleshooting States

`refused`:

- The file exceeded an Aura size limit.
- The document extension is not supported.
- The object was uploaded with a size that does not match the asset policy.

`failed`:

- The object was missing when Aura finalized the asset.
- The vision route or STT returned an error.
- The object store credentials, bucket, or endpoint are wrong.

`complete`:

- Processing finished for images, audio or video.
- For documents it means the object was named; it does not mean the document is
  searchable yet. The ingest sidecar indexes it on its own schedule, and whether it is
  searchable is answered by ArcadeDB, not by the asset status.

If browser uploads fail before finalize, inspect the presigned response and the
actual PUT request. The PUT must use the returned `upload_url`, method, and every
entry in `required_headers`.
