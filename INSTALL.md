# Installing Aura

Aura is a private Telegram second brain that runs as a self-hosted Docker stack.
It ingests sources, builds a local wiki, searches memory, runs a sandboxed
Python tool, and exposes a dashboard on localhost.

Current releases are Docker-image only. Use
`ghcr.io/chetto1983/aura:<version>` with Docker Compose. The old desktop binary
path is manual-only for legacy testing.

## What You Get

- A private Telegram bot.
- A local dashboard at `http://127.0.0.1:8080`.
- Local SearXNG web search.
- A Pyodide sandbox sidecar for `execute_code`, DOCX/XLSX extraction, charts,
  and generated artifacts.
- Local Garage backup/artifact storage.
- Optional Qdrant vector search with local fallback.
- Your own `data/aura.db`, `wiki/`, `skills/`, and `garage/` folders.

## Prerequisites

- Docker Desktop or Docker Engine with Docker Compose.
- A Telegram bot token from [@BotFather](https://t.me/BotFather).
- An OpenAI-compatible LLM endpoint, or a local Ollama-compatible endpoint.

## Step 1 - Create Your Telegram Bot

1. Open Telegram and search for **@BotFather**.
2. Send `/newbot`.
3. Pick a display name.
4. Pick a username ending in `bot`.
5. Copy the token BotFather returns.

## Step 2 - Start Aura

```powershell
git clone https://github.com/chetto1983/Aura
cd Aura
New-Item -ItemType Directory -Force data,wiki,skills,garage | Out-Null
Copy-Item .env.example data/.env
$env:AURA_IMAGE = "ghcr.io/chetto1983/aura:latest"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

If port `8080` is busy:

```powershell
$env:AURA_HOST_PORT = "18080"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

## Step 3 - Finish Setup

Open `http://127.0.0.1:8080`, or `http://127.0.0.1:18080` if you changed
`AURA_HOST_PORT`.

The first-run wizard asks for:

- `TELEGRAM_TOKEN`.
- LLM provider, base URL, model, and API key.

Optional settings such as embeddings, OCR, web search, sandbox runtime, and
Garage backups can be configured from the dashboard later.

The wizard writes bootstrap values to `data/.env`; runtime settings are stored
in `data/aura.db`.

## Step 4 - Claim Your Bot

1. Open Telegram and search for your bot username.
2. Tap **Start** or send `/start`.
3. Approve your own user if prompted.

Unknown users go into the dashboard approval queue.

## Data Locations

| Path | Purpose |
| --- | --- |
| `data/.env` | Bootstrap config such as Telegram token and paths |
| `data/aura.db` | SQLite state: settings, auth, tasks, conversations, budget, embedding cache |
| `wiki/` | Compiled memory pages and source evidence |
| `skills/` | Installed skills |
| `garage/` | Garage S3 metadata/object data |
| Docker volume `qdrant-storage` | Docker-managed derived vector index |

SQLite remains Aura's canonical database. MongoDB is not part of the default
stack; it was evaluated and deferred until measured repository pressure justifies
an optional adapter.

## Updating

```powershell
docker compose -f compose.yaml -f compose.image.yaml pull aura
docker compose -f compose.yaml -f compose.image.yaml up -d
```

Pin a release by setting `AURA_IMAGE`:

```powershell
$env:AURA_IMAGE = "ghcr.io/chetto1983/aura:v1.2.3"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

Your `data/`, `wiki/`, `skills/`, `garage/`, and Qdrant derived volume are not
deleted by normal updates.

## Useful Operations

```powershell
docker compose -f compose.yaml -f compose.image.yaml ps
docker compose -f compose.yaml -f compose.image.yaml logs -f aura
docker compose -f compose.yaml -f compose.image.yaml restart aura
```

SearXNG is available on the host at `http://127.0.0.1:8088`.
The Pyodide sandbox is internal-only at `http://pyodide:8787`.
Garage S3 is available on the host at `http://127.0.0.1:3900`.

Run a manual backup export from a development checkout:

```powershell
go run ./cmd/debug_backup
```

Run sandbox sidecar smokes:

```powershell
docker compose --profile test run --rm --no-deps test go run ./cmd/debug_sandbox -tool-smoke -runtime-url http://pyodide:8787 -timeout 3m
docker compose --profile test run --rm --no-deps test go run ./cmd/debug_sandbox -artifact-smoke -runtime-url http://pyodide:8787 -timeout 5m
```

## Troubleshooting

**Setup wizard does not appear**

Clear `TELEGRAM_TOKEN=` in `data/.env` and restart Aura.

**Port 8080 is already allocated**

Set `$env:AURA_HOST_PORT = "18080"` and run Compose again.

**Bot does not reply after `/start`**

Check `docker compose logs -f aura`, verify the Telegram token, and confirm your
user is allowed in the dashboard approval queue.

**Unauthorized from the LLM**

Open `/settings`, click **Test connection**, fix the provider URL/key/model, and
save.

**Budget exceeded**

Open `/settings` and adjust `SOFT_BUDGET`, `HARD_BUDGET`,
`COST_INPUT_PER_M_TOKENS`, or `COST_OUTPUT_PER_M_TOKENS`.

**Sandbox unavailable**

Check `docker compose ps pyodide`. Aura should log
`sandbox container runtime available` when the sidecar is healthy.

## Development

Build from the working tree:

```powershell
docker compose up -d --build
```

Run the canonical test container:

```powershell
docker compose --profile test run --rm test
```

Local commands:

```powershell
go test ./...
npm --prefix web run i18n:check
npm --prefix web run build
go run ./cmd/debug_llm
go run ./cmd/debug_searxng -base-url http://127.0.0.1:8088 -q "aura search test" -json
```

See [docs/container.md](docs/container.md) for the full stack and release gate.
