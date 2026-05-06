<p align="center">
  <img src="Logo/logo.png" alt="Aura logo" width="112">
</p>

<h1 align="center">Aura</h1>

<p align="center">
  A private Telegram second brain that runs as a self-hosted Docker stack.
</p>

<p align="center">
  <a href="https://github.com/chetto1983/Aura/pkgs/container/aura"><img alt="Docker image" src="https://img.shields.io/badge/image-ghcr.io%2Fchetto1983%2Faura-2496ED"></a>
  <a href="docs/container.md"><img alt="Install path" src="https://img.shields.io/badge/install-Docker%20Compose-0B6B50"></a>
  <img alt="Go version" src="https://img.shields.io/badge/Go-1.26.2-00ADD8">
</p>

Aura is a personal assistant you own. It chats through your Telegram bot,
ingests files into a local source inbox, builds a Markdown wiki, searches memory
with embeddings, and gives you a dashboard for sources, tasks, settings,
backups, skills, and health.

The supported install path is now Docker Compose. Release tags publish only the
container image at `ghcr.io/chetto1983/aura:<version>`.

## What Runs

The default stack starts four local services:

- `aura`: the Telegram bot, memory engine, tools, and embedded dashboard.
- `searxng`: local web search for the stable `web_search` tool.
- `garage`: S3-compatible artifact and backup storage.
- `garage-webui`: optional Garage admin UI behind a Compose profile.

All user data stays in visible folders beside the Compose file:

- `data/`: `.env`, SQLite database, logs, MCP config, prompt overlays.
- `wiki/`: compiled memory pages and source evidence.
- `skills/`: installed agent skills.
- `garage/`: Garage object storage data.

## Quick Start

Prerequisites:

- Docker Desktop or Docker Engine with Docker Compose.
- A Telegram bot token from [@BotFather](https://t.me/BotFather).
- An OpenAI-compatible LLM endpoint, or a local Ollama-compatible endpoint.

Start Aura:

```powershell
git clone https://github.com/chetto1983/Aura
cd Aura
New-Item -ItemType Directory -Force data,wiki,skills,garage | Out-Null
Copy-Item .env.example data/.env
$env:AURA_IMAGE = "ghcr.io/chetto1983/aura:latest"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

Open the setup wizard:

```text
http://127.0.0.1:8080
```

If port `8080` is busy:

```powershell
$env:AURA_HOST_PORT = "18080"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

Then open `http://127.0.0.1:18080`.

## Setup Flow

1. Paste your `TELEGRAM_TOKEN`.
2. Pick an OpenAI-compatible LLM preset or choose **Custom**.
3. Test the model connection.
4. Optional: configure embeddings, OCR, search, sandbox, and Garage backup keys.
5. Click **Save and start Aura**.
6. Open Telegram, start your bot, and approve your own user.

Aura writes bootstrap config to `data/.env` and runtime settings to
`data/aura.db`.

## Update

Pull the newest image and recreate the Aura container:

```powershell
docker compose -f compose.yaml -f compose.image.yaml pull aura
docker compose -f compose.yaml -f compose.image.yaml up -d
```

Pin a release by setting `AURA_IMAGE`:

```powershell
$env:AURA_IMAGE = "ghcr.io/chetto1983/aura:v1.2.3"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

## Develop

Build the local working tree instead of pulling GHCR:

```powershell
docker compose up -d --build
```

Run the canonical test container:

```powershell
docker compose --profile test run --rm test
```

Useful local commands:

```powershell
go test ./...
go build ./...
go run ./cmd/aura
go run ./cmd/debug_llm
go run ./cmd/debug_searxng -base-url http://127.0.0.1:8088 -q "aura search test" -json
```

## Release

A normal release is a Docker image release.

```powershell
git tag v1.2.3
git push origin v1.2.3
```

The `Docker image` workflow publishes:

- `ghcr.io/chetto1983/aura:v1.2.3`
- `ghcr.io/chetto1983/aura:latest`
- a `sha-*` traceability tag

The legacy desktop binary workflow is manual-only. Use it only for secondary
desktop builds.

## Docs

- [Install guide](INSTALL.md)
- [Container stack](docs/container.md)
- [Implementation tracker](docs/implementation-tracker.md)
- [Product requirements](prd.md)
