# Aura Container Stack

Aura can run as a server-style Docker Compose stack. In this mode Aura runs
headless, so the Windows tray is disabled and shutdown is handled by Docker
signals.

## Services

- `aura`: the Telegram bot and embedded dashboard.
- `searxng`: local metasearch for the upcoming SearXNG web-search provider.

The dashboard is bound to `127.0.0.1:8080` on the host. SearXNG is bound to
`127.0.0.1:8088` on the host and is reachable from Aura as
`http://searxng:8080`.

## First Run

Copy the environment template and fill in your Telegram token, or leave it
blank to use the first-run setup wizard:

```powershell
Copy-Item .env.example .env
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

Docker volumes hold user data:

- `aura-data`: SQLite DB, logs, MCP config, prompt overlays.
- `aura-wiki`: compiled wiki and source evidence.
- `aura-skills`: installed skills.
- `searxng-cache`: SearXNG cache.

Back up these volumes before moving hosts or upgrading major versions.

## Notes

- `compose.yaml` sets `AURA_HEADLESS=true`; desktop builds still keep the tray.
- `docker/searxng/settings.yml` enables JSON output. Without `json` in
  `search.formats`, SearXNG returns `403` for API requests with `format=json`.
- The container stack disables `SANDBOX_ENABLED` by default because the Pyodide
  runtime bundle is not included in the initial image.
