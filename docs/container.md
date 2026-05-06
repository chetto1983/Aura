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
- `test`: optional developer test container with Go 1.26.2, Node 22, and the
  Linux sandbox capability needed for no-network skill tests.

The dashboard is bound to `127.0.0.1:8080` on the host. SearXNG is bound to
`127.0.0.1:8088` on the host and is reachable from Aura as
`http://searxng:8080`. Garage's S3 API is bound to `127.0.0.1:3900` on the
host and is reachable from Aura as `http://garage:3900`.

## First Run

Create the host data folders, then copy the environment template into the
mounted data folder. You may leave `TELEGRAM_TOKEN` blank to use the first-run
setup wizard:

```powershell
New-Item -ItemType Directory -Force data,wiki,skills,garage | Out-Null
Copy-Item .env.example data/.env
docker compose up -d --build
```

Open the dashboard:

```text
http://127.0.0.1:8080
```

If another local service already owns port 8080, pick a host port without
changing Aura's in-container port:

```powershell
$env:AURA_HOST_PORT = "18080"
docker compose up -d --build
```

Then open `http://127.0.0.1:18080`.

Probe SearXNG from the host:

```powershell
go run ./cmd/debug_searxng -base-url http://127.0.0.1:8088 -q "aura search test" -json
```

Run a manual backup export to Garage:

```powershell
go run ./cmd/debug_backup
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

## Data

Visible host folders hold user data:

- `data/`: `.env`, SQLite DB, logs, MCP config, prompt overlays.
- `wiki/`: compiled wiki and source evidence.
- `skills/`: installed skills.
- `garage/`: Garage S3 metadata and object data once the Garage service is enabled.

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
- Backup export is manual first: it archives `.env`, `aura.db`, `wiki/`, and
  `skills/` into `backups/YYYY-MM-DD-HHMMSS/aura-backup.tar.gz` in the
  configured Garage bucket. SQLite and wiki storage remain local files.
- `docker/searxng/settings.yml` enables JSON output. Without `json` in
  `search.formats`, SearXNG returns `403` for API requests with `format=json`.
- The app container disables `SANDBOX_ENABLED` by default because the Pyodide
  runtime bundle is not included in the production image. The separate `test`
  service mounts the working tree and includes Node so bundled runtime tests can
  execute when `runtime/pyodide/` exists locally.
