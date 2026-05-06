# Aura Container Stack

Aura can run as a server-style Docker Compose stack. In this mode Aura runs
headless, so the Windows tray is disabled and shutdown is handled by Docker
signals.

## Services

- `aura`: the Telegram bot and embedded dashboard.
- `searxng`: local metasearch for Aura's SearXNG web-search provider.

The dashboard is bound to `127.0.0.1:8080` on the host. SearXNG is bound to
`127.0.0.1:8088` on the host and is reachable from Aura as
`http://searxng:8080`.

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

Probe SearXNG from the host:

```powershell
go run ./cmd/debug_searxng -base-url http://127.0.0.1:8088 -q "aura search test" -json
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
- `docker/searxng/settings.yml` enables JSON output. Without `json` in
  `search.formats`, SearXNG returns `403` for API requests with `format=json`.
- The container stack disables `SANDBOX_ENABLED` by default because the Pyodide
  runtime bundle is not included in the initial image.
