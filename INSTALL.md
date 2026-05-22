# Installing Aura

Aura is a private Telegram second brain that runs as a self-hosted Docker stack.
It ingests sources, builds a local wiki, searches memory, runs a sandboxed
Python tool, and exposes a dashboard on localhost.

Current releases are Docker-image only. Use
`ghcr.io/chetto1983/aura:<version>` with Docker Compose. The old desktop binary
path is manual-only for legacy testing.

## What You Get

- A private Telegram bot.
- A local dashboard at `http://127.0.0.1:18080`.
- Local SearXNG web search.
- Direct Python execution inside the Aura container for `execute_code`,
  DOCX/XLSX extraction, charts, and generated artifacts.
- Local Garage backup/artifact storage.
- Qdrant vector search for wiki memory.
- Your own `data/aura.db`, Garage data, and Docker volumes for wiki, skills, Qdrant, and caches.

## Prerequisites

- Docker Desktop or Docker Engine with Docker Compose.
- A Telegram bot token from [@BotFather](https://t.me/BotFather).
- An OpenAI-compatible LLM endpoint.

## Step 1 - Create Your Telegram Bot

1. Open Telegram and search for **@BotFather**.
2. Send `/newbot`.
3. Pick a display name.
4. Pick a username ending in `bot`.
5. Copy the token BotFather returns.

## Step 2 - Start Aura

The `compose.image.yaml` overlay pulls four pre-built images from GHCR:
`aura` (chat + dashboard + tools), `aura-whisper` (audio IN sidecar),
`aura-pocket-tts` (audio OUT sidecar, default OFF), and `aura-markitdown`
(file conversion sidecar). Cold first start: **~5-10 min** (GHCR pulls
in parallel + 2 small local init containers).

**PowerShell (Windows):**

```powershell
git clone https://github.com/chetto1983/Aura
cd Aura
New-Item -ItemType Directory -Force data,runtime-workspace,garage | Out-Null
docker compose -f compose.yaml -f compose.image.yaml up -d
```

**Bash (macOS / Linux / WSL):**

```bash
git clone https://github.com/chetto1983/Aura
cd Aura
mkdir -p data runtime-workspace garage
docker compose -f compose.yaml -f compose.image.yaml up -d
```

Pin a release by setting any of the image env vars before `up -d`
(defaults to `:latest` for each):

```bash
AURA_IMAGE="ghcr.io/chetto1983/aura:v0.3.1" \
AURA_WHISPER_IMAGE="ghcr.io/chetto1983/aura-whisper:v0.3.1" \
AURA_POCKETTTS_IMAGE="ghcr.io/chetto1983/aura-pocket-tts:v0.3.1" \
AURA_MARKITDOWN_IMAGE="ghcr.io/chetto1983/aura-markitdown:v0.3.1" \
  docker compose -f compose.yaml -f compose.image.yaml up -d
```

If port `18080` is busy, pick another:

```powershell
$env:AURA_HOST_PORT = "8080"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

> **Note:** without the `compose.image.yaml` overlay (`docker compose up -d --build`
> from the working tree), the four sidecars compile from source (whisper.cpp,
> Kyutai TTS.cpp, Python+markitdown). Cold time: **~45-60 min**. Only use the
> raw `up -d --build` path for development against local source changes.

## Step 3 - Finish Setup

Open `http://127.0.0.1:18080` (or your custom `AURA_HOST_PORT`).

The first-run wizard asks for:

- `TELEGRAM_TOKEN`.
- LLM provider, base URL, model, and API key.

Optional settings such as embeddings, OCR, web search, sandbox runtime, and
Garage backups can be configured from the dashboard later.

The wizard writes all secrets and settings to `data/aura.db`.

## Step 4 - Claim Your Bot

1. Open Telegram and search for your bot username.
2. Tap **Start** or send `/start`.
3. Approve your own user if prompted.

Unknown users go into the dashboard approval queue.

## Data Locations

| Path | Purpose |
| --- | --- |
| `data/aura.db` | SQLite state: secrets, settings, auth, tasks, conversations, budget, embedding cache |
| Docker volume `aura-wiki` | Compiled memory pages and source evidence |
| Docker volume `aura-skills` | Installed skills |
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

Your `data/`, `garage/`, `aura-wiki`, `aura-skills`, and Qdrant derived volume are not
deleted by normal updates.

## Useful Operations

```powershell
docker compose -f compose.yaml -f compose.image.yaml ps
docker compose -f compose.yaml -f compose.image.yaml logs -f aura
docker compose -f compose.yaml -f compose.image.yaml restart aura
```

SearXNG is available on the host at `http://127.0.0.1:8088`.
`execute_code` runs through Python in the Aura container when `SANDBOX_ENABLED=true`.
Garage S3 is available on the host at `http://127.0.0.1:3900`.

Run a manual backup export from a development checkout:

```powershell
go run ./cmd/debug_backup
```

Run a quick container Python smoke:

```powershell
docker compose exec aura python3 - <<'PY'
print(sum(range(101)))
PY
```

## Troubleshooting

**Setup wizard does not appear**

Open the dashboard Settings page, clear the Telegram Token field, save, and restart Aura.

**Port 18080 is already allocated**

Set `$env:AURA_HOST_PORT = "8080"` (or any free port) and run Compose again.

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

Check `docker compose exec aura python3 --version`. Aura should log
`sandbox process runtime available` when the container runtime is healthy.

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
