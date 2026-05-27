# Technology Stack

**Analysis Date:** 2026-05-27

## Languages

**Primary:**
- Go 1.26.2 — single-binary server, agent loop, tool runtime, Telegram bot, embedded HTTP API. Module path `github.com/aura/aura` (`go.mod:3`).
- TypeScript ~6.0 (with React 19 TSX) — embedded dashboard SPA at `web/`.

**Secondary:**
- Python 3.12/3.13 — baked into the runtime image for utility scripts and converter sidecars (`Dockerfile:83-94`, `docker/markitdown/Dockerfile`, `docker/pocket-tts/Dockerfile`).
- PowerShell + Bash — operational scripts under `scripts/` (`scripts/check-file-size.sh`, `scripts/test-heuristic-removal.ps1`, `scripts/launch_chrome_cdp.ps1`).
- C/C++ — vendored upstream (whisper.cpp built from source in `docker/whisper/Dockerfile`); Aura ships no first-party C code.

## Runtime

**Environment:**
- Go runtime 1.26.2 (Dockerfile build stage `FROM golang:1.26.2-bookworm`; `Dockerfile:1`).
- Linux/amd64 + linux/arm64 are the published images (`.github/workflows/docker-image.yml:23-44`); `aura-whisper` is amd64-only because whisper.cpp v1.8.4 arm64 cross-compile is broken under QEMU.
- Final image base: `debian:bookworm-slim` (`Dockerfile:24`).
- Container user: non-root `aura` (uid 10001), with `cap_net_raw` + `cap_net_admin` granted via `setcap` for `nmap` and `tcpdump` (`Dockerfile:60-70`).
- Node.js 22 — only needed at frontend build time and for the `@executeautomation/database-server` MCP shipped inside the image (`Dockerfile:16-22`, `.github/workflows/ci.yml:87`).

**Package Manager:**
- Go modules — `go.mod` + `go.sum` (lockfile present, ~140 transitive deps).
- npm — `web/package.json` + `web/package-lock.json` (Linux CI uses `npm install` instead of `npm ci` due to cross-platform lock drift; see `.github/workflows/ci.yml:93-99`).
- pip — Python deps installed system-wide into the Aura image (`Dockerfile:83-94`); markitdown + pocket-tts sidecars use isolated venvs.
- uv — Astral's resolver for the pocket-tts venv build (`docker/pocket-tts/Dockerfile:24-44`).

## Frameworks

**Core (Go):**
- `gopkg.in/telebot.v4` v4.0.0-beta.7 — Telegram Bot API client (`go.mod:5`; entry in `internal/telegram/bot.go`).
- `modernc.org/sqlite` v1.50.0 — pure-Go SQLite driver, no CGO (`go.mod:29`; CGO disabled, see `Dockerfile:11`).
- `go.uber.org/zap` v1.28.0 — structured logging with secret sanitization (`internal/logging/sanitize.go`).
- `github.com/go-git/go-git/v5` v5.19.0 — wiki version control without shelling out to git.
- `github.com/xuri/excelize/v2` v2.10.1 — xlsx creation/reading inside Go (`internal/agent/tools/registry/files_xlsx.go`).
- `github.com/go-pdf/fpdf` v0.9.0 — PDF creation (`internal/agent/tools/registry/files_pdf.go`).
- `github.com/ledongthuc/pdf` — PDF text extraction (`internal/storage/sources/store/extract_pdf.go`).
- `github.com/chromedp/chromedp` v0.15.1 — headless Chrome control (`cmd/probe_telegram_ui/`, web E2E telemetry).
- `github.com/aws/aws-sdk-go-v2/service/s3` v1.100.1 — S3 client (talks to local Garage, NOT AWS) (`internal/backup/s3.go`).
- `gopkg.in/yaml.v3` v3.0.1 — wiki frontmatter, skill manifests (`internal/skills/loader.go`).
- `github.com/fsnotify/fsnotify` v1.10.1 — file-watching for `mcp.json` and skills reconciler.
- `github.com/google/uuid` v1.6.0 — IDs.
- `github.com/rivo/uniseg` v0.4.7 — grapheme clustering for the TokenJuice compactor (`internal/tokenjuice/`).
- `fyne.io/systray` v1.12.0 — Windows tray icon (`internal/tray/tray_windows.go`), guarded by `AURA_HEADLESS`.
- `github.com/JohannesKaufmann/html-to-markdown/v2` — web fetch normalization (`internal/agent/tools/registry/web_common.go`).
- `github.com/go-shiori/go-readability` — article extraction from `web` action fetches.
- `github.com/eekstunt/telegramify-markdown-go` — Telegram MarkdownV2 escaping (`internal/channels/telegram/outbound.go`).
- `github.com/skip2/go-qrcode` — QR codes for first-run setup.
- `github.com/disintegration/imaging` — image manipulation.
- `golang.org/x/net`, `golang.org/x/text`, `golang.org/x/crypto`, `golang.org/x/image` — stdlib extensions.

**Frontend:**
- React 19.2.5 + React-DOM 19.2.5 (`web/package.json:40-41`).
- Vite 8.0.10 — dev server (port 5173 proxy to `:8080`), build to `internal/api/dist/` (`web/vite.config.ts`).
- TypeScript ~6.0.2 + typescript-eslint 8.58.2.
- Tailwind CSS v4.2.4 via `@tailwindcss/vite` (`web/vite.config.ts:11`).
- `react-router-dom` v7.0.0 — SPA routing.
- shadcn/ui + Radix UI + `class-variance-authority` + `tailwind-merge` + `tw-animate-css` — UI primitives.
- `@assistant-ui/react` 0.14.7 + `@assistant-ui/react-data-stream` — chat surface widget.
- `@tiptap/*` 3.22.4 — wiki page editor (`@tiptap/starter-kit` + markdown + link + typography + pm + react).
- `react-markdown` 8.0.7 + `remark-gfm` 3.0.1 + `rehype-highlight` 7.0.2 — read-only Markdown rendering.
- `react-force-graph-2d` 1.29.1 — wiki/graph visualization.
- `i18next` 26.0.8 + `react-i18next` + `i18next-browser-languagedetector` — IT/EN translations (`web/scripts/check-i18n.mjs`).
- `react-timezone-select` 3.3.3 — settings page.
- `lucide-react` 1.11.0 + `@fontsource-variable/geist` 5.2.8 — icons + font.
- `zod` 4.3.6 — runtime schema validation.
- `sonner` 2.0.7 — toast notifications.

**Testing:**
- Go: `testing` stdlib + `go test -race` (`.github/workflows/ci.yml:44`).
- Frontend unit: Vitest 4.1.6 (`web/package.json:13`; config: `web/vite.config.ts`).
- Frontend E2E: Playwright 1.59.1 (`web/playwright.config.ts`; specs under `web/e2e/`).
- Probe harnesses (Go binaries): `cmd/probe_chat`, `cmd/probe_doc`, `cmd/probe_ingest_e2e`, `cmd/probe_reasoning`, `cmd/probe_telegram_ui`, `cmd/probe_webfetch`, `cmd/quality_bench`.
- Debug harnesses: `cmd/debug_llm`, `cmd/debug_searxng`, `cmd/debug_qdrant`, `cmd/debug_ingest`, `cmd/debug_tools`, `cmd/debug_xlsx`, `cmd/debug_pdf`, `cmd/debug_docx`, `cmd/debug_backup`, `cmd/debug_telegram`, `cmd/debug_reconcile`, `cmd/debug_convdump`, `cmd/debug_common`, `cmd/debug_sandbox` (referenced in CLAUDE.md but not present as a directory at scan time).

**Build/Dev:**
- Docker Compose v2 — `compose.yaml` (dev) + `compose.prod.yaml` (production hardening) + `compose.image.yaml`.
- Multi-stage Dockerfiles: `Dockerfile` (main aura), `Dockerfile.test` (CI go test container), `docker/whisper/Dockerfile`, `docker/pocket-tts/Dockerfile`, `docker/markitdown/Dockerfile`, `docker/init-models/Dockerfile`, `docker/garage-init/Dockerfile`, `docker/secrets/Dockerfile`.
- Make — `Makefile` aliases (`make build`, `make test`, `make compose-up`, `make download-models`).
- Lefthook — opt-in pre-commit hooks (`lefthook.yml`): `golangci-lint --new-from-rev=HEAD`, `dupl -t 60`, file-size 600-LOC cap.
- Custom lint scripts: `scripts/check-file-size.sh` (enforces the 600-LOC-per-file invariant from `CLAUDE.md`), `scripts/registry-diff.sh`, `scripts/test-heuristic-removal.ps1`.
- `golang.org/x/tools/cmd/deadcode` — invoked in CI against a baseline at `docs/deadcode-baseline-2026-05-22.json` (`.github/workflows/ci.yml:47-76`).

## Key Dependencies

**Critical:**
- `gopkg.in/telebot.v4` — every inbound user message arrives here; replacing it touches `internal/telegram/` and `internal/channels/telegram/` in lockstep.
- `modernc.org/sqlite` — all runtime state (auth tokens, conversations, scheduled tasks, embedding cache, secrets) flows through this driver. Pure Go means no glibc dependency in the final image. WAL mode is OFF in container (`compose.yaml:53` — `AURA_SQLITE_JOURNAL_MODE: "DELETE"`) because of a known Windows bind-mount corruption (see `MEMORY.md` `feedback_sqlite_wal_windows_corruption`).
- `github.com/aws/aws-sdk-go-v2/service/s3` — points at the local Garage S3 sidecar, never AWS in normal use.
- `gopkg.in/yaml.v3` — wiki frontmatter + skill manifest parsing; schema changes ripple to `internal/wiki/` and `internal/skills/loader.go`.

**Infrastructure:**
- `github.com/go-git/go-git/v5` — wiki history is committed page-by-page; not optional. Removing it means losing the audit trail.
- `golang.org/x/text` + `golang.org/x/net` — used by tokenization, MIME handling, and Telegram entity decoding.
- `github.com/ProtonMail/go-crypto`, `github.com/cloudflare/circl` — indirect, brought in by go-git for SSH signing support.

## Configuration

**Environment:**
- Primary loader: `internal/config/config.go` (envconfig-style struct tags; reads `os.Environ`).
- Layered overrides: `internal/config/applier.go` reads the SQLite `settings` table on boot and overlays values onto the struct (settings catalog in `internal/api/settings.go`).
- Secrets sit in a SEPARATE SQLite table `secrets` (`internal/secrets/store.go`) so `SELECT * FROM settings` never reveals an API key. Canonical keys: `KeyTelegramToken`, `KeyLLMAPIKey`, `KeyEmbeddingAPIKey`, `KeyGarageS3AccessKey`, `KeyGarageS3SecretKey`, `KeyQdrantAPIKey`, `KeyMistralAPIKey` (`internal/secrets/store.go:17-25`).
- First-run setup: when `cfg.IsBootstrapped()` is false (blank Telegram token), the binary starts a loopback-bound setup wizard (`internal/api/setup_server.go`, kicked off from `cmd/aura/main.go`).

**Key env vars (non-exhaustive; full list in `internal/config/config.go`):**
- `TELEGRAM_TOKEN`, `TELEGRAM_ALLOWLIST`
- `LLM_API_KEY`, `LLM_BASE_URL`, `LLM_MODEL`, `LLM_MAX_RETRIES`
- `EMBEDDING_BASE_URL`, `EMBEDDING_MODEL`, `EMBEDDING_API_KEY`, `EMBEDDING_OUTPUT_DIM`
- `MISTRAL_API_KEY`, `MISTRAL_OCR_MODEL`, `MISTRAL_OCR_BASE_URL`
- `QDRANT_URL`, `QDRANT_COLLECTION`, `QDRANT_API_KEY`
- `GARAGE_S3_ENDPOINT`, `GARAGE_S3_REGION`, `GARAGE_S3_BUCKET`, `GARAGE_S3_ACCESS_KEY` (or `*_FILE`), `GARAGE_S3_SECRET_KEY` (or `*_FILE`)
- `MARKITDOWN_URL`, `MARKITDOWN_TIMEOUT_SEC`
- `WHISPER_BASE_URL`, `WHISPER_LANGUAGE`, `WHISPER_TIMEOUT_SEC`
- `POCKETTTS_BASE_URL`, `POCKETTTS_TIMEOUT_SEC`, `AURA_TTS_ENABLED`
- `SEARXNG_BASE_URL`, `WEB_SEARCH_PROVIDER`
- `WIKI_PATH`, `SOURCES_PATH`, `SKILLS_PATH`, `SKILLS_INSTALL_PROJECT_DIR`, `SKILLS_ADMIN`, `SKILLS_CATALOG_URL`
- `MCP_SERVERS_PATH` (default `./mcp.json`)
- `PROMPT_OVERLAY_PATH`, `AURA_PROMPT_VERSION`, `AURA_SKILL_ROUTING_MODE`
- `AURA_AGENT_LOOP_MAX_STEPS`, `AURA_AGENT_PARALLEL_TOOLS`, `AURA_TOOL_ALLOWLIST`, `AURA_TERMINAL_TOOL_POLICY`, `AURA_DELEGATION_MODE`
- `AURABOT_ENABLED`, `AURABOT_MAX_ACTIVE`, `AURABOT_MAX_DEPTH`, `AURABOT_TIMEOUT_SEC`, `AURABOT_MAX_ITERATIONS`
- `SANDBOX_ENABLED`, `SANDBOX_TIMEOUT_SEC`
- `MAX_CONTEXT_TOKENS`, `MAX_HISTORY_MESSAGES`, `MAX_TOOL_RESULT_CHARS`, `SOFT_BUDGET`, `HARD_BUDGET`
- `AURA_CTX_ENGINE`, `AURA_CTX_COMPACT_PERCENT`, `AURA_PAYLOAD_SUMMARIZER`, `AURA_TOKENJUICE_ENABLED`
- `AURA_TIMEZONE`, `AURA_HEADLESS`, `AURA_HOST_BIND`, `AURA_HOST_PORT`
- `DB_PATH`, `LOG_DIR`, `LOG_LEVEL`, `AURA_SQLITE_JOURNAL_MODE`
- `CONV_ARCHIVE_ENABLED`, `AURA_TRACE_RETENTION_DAYS`
- `AURA_WORKSPACE_TOOLS`, `AURA_WORKSPACE_ROOT`, `AURA_RUNTIME_WORKSPACE_PATH`

A `.env` file is present at the repo root (contents not read per security policy) — used by `docker compose up` for host-side variable interpolation.

**Build:**
- `Dockerfile` — main aura binary (multi-stage, distroless-adjacent slim).
- `Dockerfile.test` — CI Go test container.
- `docker/init-models/Dockerfile` — `distroless/static-debian12:nonroot` final stage, ~10 MiB binary `aura-init-models` that downloads + SHA-256-verifies embedding + Whisper GGUFs into `/models`.
- `docker/whisper/Dockerfile` — builds `whisper-server` from `whisper.cpp` v1.8.4 via CMake (binary at `build/bin/whisper-server`, statically linked).
- `docker/pocket-tts/Dockerfile` — Python 3.12 + `pocket-tts[quantize]` via uv, multi-stage with aggressive `strip` pass.
- `docker/markitdown/Dockerfile` — Python 3.13 + `markitdown[docx,pdf,pptx,xls,xlsx,outlook]` + `markitdown-mcp` + local `aura-plugins/`.
- `docker/garage-init/Dockerfile` — one-shot garage CLI runner for bucket + key provisioning.
- `docker/secrets/Dockerfile` — alpine-based init script that decrypts secret material into `/data/secrets/` before aura starts.
- `web/vite.config.ts` — builds React SPA to `internal/api/dist/`, embedded via `//go:embed all:dist` (referenced from `CLAUDE.md`).
- CI: `.github/workflows/ci.yml` (Go vet/build/test + depguard + dead-code + frontend build) and `.github/workflows/docker-image.yml` (multi-arch GHCR push on `v*` tag for aura/aura-whisper/aura-pocket-tts/aura-markitdown).

## Platform Requirements

**Development:**
- Windows 10/11, macOS, or Linux with Docker Desktop + Docker Compose v2.
- Go 1.26.2 + Node.js 22 for local rebuilds (otherwise `docker compose up --build` is sufficient).
- Optional: `lefthook` + `golangci-lint` + `mibk/dupl` for pre-commit gates (`lefthook.yml`).
- Optional: `~/go/bin/golangci-lint` (memory `feedback_golangci_lint_catches_what_audits_miss` confirms it catches what audit agents miss).
- Disk: ~5-7 GB free for model artifacts and Docker layer cache (embeddinggemma 265 MB + whisper-small 466 MB + pocket-tts ~600 MB + base images).

**Production:**
- Single-host Docker Compose deploy (`compose.yaml` + `compose.prod.yaml` overlay).
- Production overlay pins dashboard to `127.0.0.1:8080` for SSH-tunnel-only access (`compose.prod.yaml:24`).
- Pre-built images published to `ghcr.io/chetto1983/{aura,aura-whisper,aura-pocket-tts,aura-markitdown}` on `v*` tag (`.github/workflows/docker-image.yml:66`).
- Host CPU: mini-PC profile validated (16 cores, memory `feedback_minipc_cpu_budget`: embed sidecar ≤4 threads, no busy-loop polling). CUDA explicitly NOT recommended — CPU outperforms GPU for single-query embed by ~60x on consumer laptop GPUs (memory `feedback_gpu_not_for_embedding_workload`).
- Network: requires outbound HTTPS to `huggingface.co` (first boot model fetch), `api.mistral.ai` (OCR), and the configured LLM provider (`LLM_BASE_URL`).

---

*Stack analysis: 2026-05-27*
