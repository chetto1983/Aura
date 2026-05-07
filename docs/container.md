# Aura Container Stack

Aura can run as a server-style Docker Compose stack. In this mode Aura runs
headless, so the Windows tray is disabled and shutdown is handled by Docker
signals.

## Services

- `aura`: the Telegram bot and embedded dashboard.
- `searxng`: local metasearch for Aura's SearXNG web-search provider.
- `garage`: local S3-compatible object storage for manual backup exports.
- `garage-webui`: optional Garage admin UI behind the `garage-ui` Compose
  profile.
- `qdrant`: local vector database sidecar for rebuildable wiki/memory
  embeddings.
- `test`: optional developer test container with Go 1.26.2, Node 22, and the
  Linux sandbox capability needed for no-network skill tests.

The dashboard is bound to `127.0.0.1:8080` on the host. SearXNG is bound to
`127.0.0.1:8088` on the host and is reachable from Aura as
`http://searxng:8080`. Garage's S3 API is bound to `127.0.0.1:3900` on the
host and is reachable from Aura as `http://garage:3900`.
Qdrant's REST API is bound to `127.0.0.1:6333` on the host and is reachable
from Aura as `http://qdrant:6333`.

## First Run

Create the host data folders, then copy the environment template into the
mounted data folder. Released installs pull the published GHCR image; you may
leave `TELEGRAM_TOKEN` blank to use the first-run setup wizard:

```powershell
New-Item -ItemType Directory -Force data,wiki,skills,garage | Out-Null
Copy-Item .env.example data/.env
$env:AURA_IMAGE = "ghcr.io/chetto1983/aura:latest"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

Pin a specific release by setting `AURA_IMAGE` to a tag such as
`ghcr.io/chetto1983/aura:v1.2.3`.

Open the dashboard:

```text
http://127.0.0.1:8080
```

If another local service already owns port 8080, pick a host port without
changing Aura's in-container port:

```powershell
$env:AURA_HOST_PORT = "18080"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

Then open `http://127.0.0.1:18080`.

For local development, build from the working tree instead:

```powershell
docker compose up -d --build
```

Probe SearXNG from the host:

```powershell
go run ./cmd/debug_searxng -base-url http://127.0.0.1:8088 -q "aura search test" -json
```

Run a manual backup export to Garage:

```powershell
go run ./cmd/debug_backup
```

Probe Qdrant health:

```powershell
go run ./cmd/debug_qdrant -url http://127.0.0.1:6333
```

Rebuild the optional Qdrant wiki index after memory cleanup:

```powershell
go run ./cmd/debug_qdrant -url http://127.0.0.1:6333 -rebuild -timeout 5m
```

This command uses `EMBEDDING_API_KEY`, `EMBEDDING_BASE_URL`, and
`EMBEDDING_MODEL`; Aura's SQLite FTS mirror remains the fallback runtime index
until Qdrant search is promoted in a later slice.

By default this writes a complete artifact set:

- `backups/<timestamp>/aura-backup.tar.gz`: full restore point with `.env`,
  `aura.db`, `wiki/`, and `skills/`.
- `artifacts/<timestamp>/source-originals.tar.gz`: source metadata plus
  immutable originals such as PDFs, DOCX, TXT, HTML-derived files, uploads, and
  sandbox artifacts stored under `wiki/raw/src_*/original.*`.
- `artifacts/<timestamp>/extractions.tar.gz`: generated OCR/extraction files
  such as `ocr.md`, `ocr.json`, `extract.md`, `extract.json`, cleaned markdown,
  and extracted assets.
- `artifacts/<timestamp>/memory-snapshot.tar.gz`: compiled wiki/memory pages,
  excluding raw sources so OCR bloat does not get duplicated into memory.
- `artifacts/<timestamp>/embedding-index.tar.gz`: `aura.db` plus SQLite
  WAL/SHM sidecars and `wiki/index.md`, enough to preserve embedding cache and
  search/index state without making S3 the live database.
- `artifacts/<timestamp>/audit-bundle.tar.gz`: logs and `reports/` when
  present, for failed ingest cases, E2E artifacts, and debug bundles.
- `artifacts/<timestamp>/manifest.json`: uploaded object manifest.

To upload only the legacy full restore object, run:

```powershell
go run ./cmd/debug_backup -mode full
```

Open the optional Garage Web UI:

```powershell
docker compose --profile garage-ui up -d garage-webui
```

Then browse to `http://127.0.0.1:3909`.

## Test In Docker

Docker is the canonical development and release gate. Run Go tests through the
Compose test profile so the environment matches Aura's Linux container path:

```powershell
docker compose --profile test run --rm test
```

The test image includes Node for the Pyodide runner tests. The service also
adds `SYS_ADMIN` with an unconfined seccomp profile because Linux no-network
skill tests create a temporary network namespace; a plain `docker run
golang:... go test ./...` container cannot do that reliably.
The command excludes incidental Go packages under `web/node_modules`.

Live XLSX/DOCX Pyodide extraction is opt-in because it can need more memory
than a default Docker Desktop engine exposes. Run it only after assigning Docker
enough memory:

```powershell
docker compose --profile test run --rm -e AURA_SOURCE_PYODIDE_LIVE=1 test go test ./internal/source -run TestPyodide -count=1 -v
```

Inside the Compose network, Aura should use:

```text
http://searxng:8080
```

## Publishing Releases

Aura release tags publish only the Docker image. Push a version tag:

```powershell
git tag v1.2.3
git push origin v1.2.3
```

GitHub Actions builds `ghcr.io/chetto1983/aura:v1.2.3`, also tags it as
`latest`, and publishes linux/amd64 plus linux/arm64 variants. The legacy
GoReleaser binary workflow is manual-only and should not run for normal
releases.

## Data

Visible host folders hold user data:

- `data/`: `.env`, SQLite DB, logs, MCP config, prompt overlays.
- `wiki/`: compiled wiki and source evidence.
- `skills/`: installed skills.
- `garage/`: Garage S3 metadata and object data once the Garage service is enabled.
- `qdrant-storage`: Docker-managed named volume for Qdrant's derived vector
  index. Qdrant warns that Docker Desktop FUSE bind mounts can corrupt vector
  storage, so Aura keeps this one derived index in a named volume and rebuilds
  it from `wiki/` when needed.

Back up these folders before moving hosts or upgrading major versions.

## Notes

- `compose.yaml` sets `AURA_HEADLESS=true`; desktop builds still keep the tray.
- `compose.yaml` sets `AURA_ENV_PATH=/data/.env`; the setup wizard writes the
  Telegram token there rather than to an ephemeral container filesystem.
- `compose.yaml` sets `WEB_SEARCH_PROVIDER=searxng`, so Aura registers the
  stable `web_search` tool against the bundled SearXNG service instead of
  requiring Ollama web credentials. The paired `web_fetch` tool uses Aura's
  bounded direct HTTP fetcher in this mode.
- `compose.yaml` starts Garage with `--single-node --default-bucket`, matching
  Garage's quick-start path. The local demo keys are intentionally low-trust;
  rotate them before exposing Garage beyond localhost.
- Backup export is manual first. It writes a full restore point under
  `backups/YYYY-MM-DD-HHMMSS/` plus categorized artifact archives under
  `artifacts/YYYY-MM-DD-HHMMSS/`. SQLite and wiki storage remain local files.
- `docker/searxng/settings.yml` enables JSON output. Without `json` in
  `search.formats`, SearXNG returns `403` for API requests with `format=json`.
- `compose.yaml` starts Qdrant from `qdrant/qdrant:latest` with storage in the
  Docker-managed `qdrant-storage` volume. Aura exposes `QDRANT_URL`,
  `QDRANT_COLLECTION`, optional `QDRANT_API_KEY`, and `SEARCH_BACKEND` in the
  dashboard settings. `SEARCH_BACKEND=chromem` keeps local chromem/SQLite search
  as the default; `SEARCH_BACKEND=qdrant` queries the Qdrant sidecar first and
  falls back locally.
- The app container enables `SANDBOX_ENABLED=true` and ships the bundled
  Pyodide runtime at `/app/runtime/pyodide`. Node.js is installed in the image
  for the runner script. The separate `test` service still mounts the working
  tree so live runtime tests can exercise the local bundle directly.
