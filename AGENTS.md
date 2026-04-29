# Agent Notes

## Project

Aura is a Go Telegram assistant. The main binary is `./cmd/aura`; `./cmd/debug_llm` is a small LLM smoke-test utility.

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
