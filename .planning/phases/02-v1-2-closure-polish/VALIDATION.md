# v1.2 Closure And Polish Validation

Date: 2026-05-06

## Scope

This phase closes v1.2 source intake polish. The accepted formats are PDF, TXT, Markdown, JSON, CSV, XLSX, and DOCX.

## Implemented

- Source/API/Telegram/frontend upload claims now use the same supported-format truth.
- XLSX extraction routes through the sandbox manager and Pyodide, with input files mounted into the Pyodide filesystem before the trusted script runs.
- XLSX extraction rejects oversized/suspicious ZIP archives before invoking Pyodide and only parses a bounded number of rows per sheet.
- DOCX extraction routes through a fixed offline Pyodide script that reads WordprocessingML parts and writes normalized markdown plus JSON metadata.
- DOCX extraction is bounded by entry count, uncompressed size, XML part size, compression ratio, paragraph count, and text bytes.
- Non-PDF `extract_complete` sources flow through the dashboard/API ingest action into wiki pages.
- Wiki graph responses return empty edge lists as `[]`.
- Wiki graph UI tolerates legacy `null` edge/node responses.
- Conversation rows remain semantic table rows, while turn IDs provide real keyboard-accessible open buttons.
- Dashboard E2E token setup uses the local DB-backed seed path.

## Verification Commands

Run from `D:\Aura\.worktrees\v1-2-closure-polish`.

- `go test ./internal/source ./internal/ingest ./internal/api ./internal/telegram -count=1`
- `go test ./...`
- `go build ./...`
- `npm --prefix web run i18n:check`
- `npm --prefix web run lint`
- `npm --prefix web run build`
- `npm --prefix web run audit:frontend`
- `npm --prefix web run e2e`
- `go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean`
- `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\check-windows-gui-subsystem.ps1 <extracted snapshot aura.exe>`

## Results

- `go test ./internal/source ./internal/ingest ./internal/api ./internal/telegram -count=1`: PASS
- `go test ./...`: PASS
- `go build ./...`: PASS
- `npm --prefix web run i18n:check`: PASS
- `npm --prefix web run lint`: PASS
- `npm --prefix web run build`: PASS
- `npm --prefix web run audit:frontend`: PASS
- `npm --prefix web run e2e`: PASS with Aura running on `127.0.0.1:8081` and the DB-seeded E2E token loaded from `.env`.
- `go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean`: PASS
- `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\check-windows-gui-subsystem.ps1 <extracted snapshot aura.exe>`: PASS, printed `windows gui subsystem ok`.

## Future Work

- Evaluate Microsoft MarkItDown as a future generalized converter candidate.
- Add broader document and media ingestion only when each format has acceptance policy, extraction evidence, tests, and UI copy in the same slice.
