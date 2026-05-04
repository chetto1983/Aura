# Agent Notes

## Project

Aura is a Go Telegram assistant with an embedded React dashboard. The main binary is `./cmd/aura`; `./cmd/debug_llm`, `./cmd/debug_tools`, and `./cmd/debug_ingest` are smoke-test utilities. `./cmd/build_icon` generates the tray icon.

The product direction is a standalone second brain: Picobot-style agent tools merged with the LLM Wiki pattern from `docs/llm-wiki.md`. Aura maintains its own source inbox, wiki, search, graph, review queue, and audit log instead of relying on Obsidian.

Current shipped surfaces (see `prd.md` v4.0 + `docs/implementation-tracker.md` for detail):

- `internal/source` + `internal/ocr` + `internal/ingest` — Mistral OCR PDF pipeline with sha256 dedup and auto-ingest.
- `internal/scheduler` — SQLite-backed reminders + nightly maintenance jobs.
- `internal/skills` — Anthropic skill format with progressive-disclosure manifest, multi-root loader (`SKILLS_PATH` + `.claude/skills`), and skills.sh catalog install/delete behind `SKILLS_ADMIN`.
- `internal/mcp` — stdio + Streamable-HTTP MCP client; tools auto-register as `mcp_<server>_<tool>`.
- `internal/api` + `web/` — React 19 dashboard embedded via `//go:embed all:dist`. Bearer auth via tokens minted with the `request_dashboard_token` LLM tool and shipped over Telegram.
- `internal/auth` — `api_tokens`, `pending_users`, `allowed_users` SQLite tables (auth + /start approval queue).
- `internal/tray` — Windows tray icon with "Open Dashboard"; no-op on other platforms.
- `internal/telegram/markdown.go` — LLM Markdown → Telegram HTML subset renderer (slice 11u).

Active phase work now lives under `.planning/` (for example `.planning/STATE.md`, `.planning/ROADMAP.md`, and `.planning/phases/*`). Historical phase plans previously under `docs/plans/` were removed from the active docs tree; use git history for those artifacts and `docs/implementation-tracker.md` for shipped slice history.

## Commands

- Format: `go fmt ./...`
- Test: `go test ./...`
- Build: `go build ./...`
- Run app: `go run ./cmd/aura`
- LLM smoke test: `go run ./cmd/debug_llm`

`make all` runs tests and then builds the project.

## Local Files

Do not commit `.env`, database files, binaries, or generated wiki raw data. `.env.example` is tracked as the safe configuration template.

## Working Rules

- Preserve user edits in the working tree.
- Prefer small, focused changes that follow the existing Go package layout.
- Use `Body` for wiki page content; the project has migrated from YAML page files to markdown with frontmatter.
- Keep wiki links in `[[slug]]` form.
- Use `LLM_API_KEY` for the chat model and Ollama web tools when configured. Use dedicated Mistral embedding settings (`EMBEDDING_API_KEY`, `EMBEDDING_BASE_URL=https://api.mistral.ai/v1`, `EMBEDDING_MODEL=mistral-embed`) for wiki search; do not fall back from embeddings to `LLM_API_KEY`.
