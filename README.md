# Aura

Aura is a Go-based Telegram assistant with LLM integrations, local wiki storage, search, budget tracking, health endpoints, logging, and optional tracing.

## Requirements

- Go 1.25.5 or newer matching `go.mod`
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

Optional LLM settings can point to OpenAI-compatible APIs or local Ollama, depending on the provider path you want to use.

## Common Commands

```powershell
go test ./...
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

## Runtime Data

The repo ignores generated runtime files such as `.env`, `aura.db`, and built binaries. Wiki raw data is also ignored by default, while schema and documentation files stay tracked.

## Health

Aura starts an HTTP health and observability server on `HTTP_PORT`, defaulting to `:8080`.
