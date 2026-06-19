# Aura Asset Pipeline

The asset pipeline lets operators attach documents, images, and audio to Aura
from the web cockpit or Telegram. Aura stores the original file in an
S3-compatible object store, tracks lifecycle state in Postgres, and sends the
processed result to the existing document, OCR, or speech-to-text processors.

Use this guide when you configure local development, bring up Garage, or debug an
asset that is stuck in the UI.

## Components

- `internal/assets` owns asset metadata, status transitions, retry, promote, and
  delete.
- `internal/objectstore` hides the storage backend. The default production-like
  backend is Garage/S3. `filesystem-dev` is available for local backend tests.
- Postgres stores `aura.assets` and `aura.asset_events`.
- Garage stores the original object bytes.
- MarkItDown indexes document assets through the existing document ingestion
  path.
- The image processor calls the configured OCR/vision endpoint.
- The audio processor calls the configured STT endpoint.

The agent runner stays text-only. When a user sends attachments, Aura validates
the asset ids, builds a protected context block on the backend, and prepends that
block to the user message.

## Required Environment

Object storage:

| Variable | Purpose | Default |
| --- | --- | --- |
| `AURA_OBJECTSTORE_BACKEND` | Storage backend: `garage`, `s3`, `filesystem-dev`, or `fake` | `garage` |
| `AURA_OBJECTSTORE_ENDPOINT` | Internal S3-compatible endpoint used by Aura | `http://127.0.0.1:3900` locally, `http://garage:3900` in Compose |
| `AURA_OBJECTSTORE_PUBLIC_ENDPOINT` | Optional host rewrite for browser-visible presigned URLs | empty |
| `AURA_OBJECTSTORE_REGION` | S3 signing region | `garage` |
| `AURA_OBJECTSTORE_BUCKET` | Bucket for original asset objects | `aura-assets` |
| `AURA_OBJECTSTORE_ACCESS_KEY` | S3 access key | `GK000000000000000000000000` in `.env.example` |
| `AURA_OBJECTSTORE_SECRET_KEY` | S3 secret key | 32-byte hex dev value in `.env.example` |
| `AURA_OBJECTSTORE_PATH_STYLE` | Use path-style S3 URLs, required by Garage defaults | `true` |
| `GARAGE_RPC_SECRET` | 32-byte hex RPC secret used by the Garage node | deterministic dev value in `.env.example` |

Asset limits:

| Variable | Purpose | Default |
| --- | --- | --- |
| `AURA_ASSET_MAX_DOCUMENT_BYTES` | Maximum PDF/XLSX/XLSM/DOCX size | `104857600` |
| `AURA_ASSET_MAX_IMAGE_BYTES` | Maximum image size | `26214400` |
| `AURA_ASSET_MAX_AUDIO_BYTES` | Maximum audio size | `104857600` |
| `AURA_ASSET_PRESIGN_TTL_SEC` | Presigned upload URL lifetime | `600` |
| `AURA_ASSET_PROCESSING_CONCURRENCY` | Asset worker width knob | `2` |

Processor endpoints:

| Variable | Purpose |
| --- | --- |
| `DOCUMENTS_BASE_URL` | MarkItDown `/convert` endpoint for document assets |
| `MULTIMODAL_BASE_URL` and `MULTIMODAL_MODEL` | Local OCR/vision endpoint and model |
| `AURA_VISION_CLOUD` | Set `true` to route image processing through cloud vision |
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
docker compose up -d postgres neo4j garage markitdown aura-ocr-vl aura-stt
bash scripts/garage_bootstrap.sh
go run ./cmd/aura db migrate
go run ./cmd/aura neo4j migrate
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

When Aura runs inside Docker and the browser runs on the host, set:

```powershell
$env:AURA_OBJECTSTORE_PUBLIC_ENDPOINT='http://127.0.0.1:3900'
```

Without that rewrite, Aura may sign URLs for `http://garage:3900`, which is
valid inside Compose but not resolvable by a host browser.

## Web Upload Flow

1. The cockpit calls `POST /api/assets/presign` with the file name, MIME type,
   size, thread id, and modality hint.
2. Aura creates an asset row and returns a presigned upload URL plus
   `required_headers`.
3. The browser uploads the file with those exact headers. Garage signatures
   include `Content-Type` and `Content-Length`, so clients must send both.
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

If passphrase auth is enabled, export `AURA_WEB_AUTH_SECRET`; the script will
mint a local passphrase-session cookie for the seeded `local` identity. If you
use Authula, pass an authenticated cookie jar with
`AURA_ASSET_SMOKE_COOKIE_JAR=/path/to/cookies.txt`.

## Troubleshooting States

`refused`:

- The file exceeded an Aura size limit.
- The document extension is not supported.
- The object was uploaded with a size that does not match the asset policy.

`failed`:

- The object was missing when Aura finalized the asset.
- MarkItDown, OCR, or STT returned an error.
- The object store credentials, bucket, or endpoint are wrong.

`searchable`:

- Document text is indexed and ready for `document_search`.
- Dense embeddings may still be running in the background.

`complete`:

- Processing finished for images or audio.
- For documents, this also means the enhancement phase finished.

If browser uploads fail before finalize, inspect the presigned response and the
actual PUT request. The PUT must use the returned `upload_url`, method, and every
entry in `required_headers`.
