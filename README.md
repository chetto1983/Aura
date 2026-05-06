# Aura

Aura is a Go-based Telegram assistant with LLM integrations, local wiki storage, search, budget tracking, health endpoints, logging, and optional tracing.

## Install

Aura is now container-first. End users should run the Docker Compose stack,
which starts Aura headless beside SearXNG search and Garage S3 backup storage.
Desktop binaries remain useful for local development and legacy installs, but
Docker is the supported server path.

## Build from source (developers)

- Go 1.26.2 or newer matching `go.mod`
- Node 20+ for the web dashboard
- A Telegram bot token
- At least one allowlisted Telegram user ID
- Optional OpenAI-compatible LLM and embedding API credentials

## Setup

Create a local environment file from the template:

```powershell
Copy-Item .env.example .env
```

Then fill in the required values:

- `TELEGRAM_TOKEN`
- `TELEGRAM_ALLOWLIST`

Optional LLM settings can point to any OpenAI-compatible API, including OpenAI, OpenRouter, Mistral, Groq, DeepSeek, Together, or local Ollama.

## Common Commands

```powershell
docker compose --profile test run --rm test
go build ./...
go run ./cmd/aura
go run ./cmd/debug_llm
```

The same commands are available through `make`:

```powershell
make test
make build
make run
make debug-llm
```

## Container Stack

For server-style installs, Aura ships a Docker Compose stack with Aura running
headless beside SearXNG and Garage S3:

```powershell
New-Item -ItemType Directory -Force data,wiki,skills,garage | Out-Null
Copy-Item .env.example data/.env
docker compose up -d --build
```

Open <http://127.0.0.1:8080>. SearXNG is exposed on
<http://127.0.0.1:8088>. See [docs/container.md](docs/container.md).
If port 8080 is already occupied, set `AURA_HOST_PORT` before starting Compose
and open that port instead.

## Runtime Data

The repo ignores generated runtime files such as `.env`, `aura.db`, and built binaries. Wiki raw data is also ignored by default, while schema and documentation files stay tracked.

## Health

Aura starts an HTTP health and observability server on `HTTP_PORT`, defaulting to `:8080`.
