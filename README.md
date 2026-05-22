<p align="center">
  <img src="Logo/logo.png" alt="Aura logo" width="320">
</p>

<h1 align="center">Aura</h1>

<p align="center">
  <em>A private Telegram second-brain that runs as a self-hosted Docker stack.</em>
</p>

<p align="center">
  <a href="https://github.com/chetto1983/Aura/pkgs/container/aura"><img alt="Docker image" src="https://img.shields.io/badge/image-ghcr.io%2Fchetto1983%2Faura-2496ED?logo=docker&logoColor=white"></a>
  <a href="docs/container.md"><img alt="Install path" src="https://img.shields.io/badge/install-Docker%20Compose-0B6B50?logo=docker&logoColor=white"></a>
  <img alt="Go version" src="https://img.shields.io/badge/Go-1.26.2-00ADD8?logo=go&logoColor=white">
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-blue"></a>
</p>

---

**Aura is a personal assistant you own.** It chats through your Telegram bot,
ingests files into a local source inbox, builds a Markdown wiki you can read
on disk, searches memory with embeddings, and gives you a dashboard for
sources, tasks, settings, backups, skills, and health — all behind a single
self-hosted Docker stack.

### Why Aura?

- 🔐 **Your data stays yours** — wiki on disk, SQLite local, no cloud storage required.
- 💬 **Telegram-native** — your phone is the front door; no separate app to install.
- 📚 **Self-building knowledge base** — drop a PDF, get an OCR'd Markdown page with `[[wiki-links]]`.
- 🧠 **Learns from its own mistakes** — operational memory with priority pinning, decay, and a memory-poisoning guard ([Phase-OP+](docs/phase-op-plus-plan-2026-05-19.md)).
- 🛠️ **Pluggable substrate** — MCP servers, skills, prompt overlays, capability gates — extend without forking.
- 🐳 **One-command install** — `docker compose up -d`. Production hardening profile included.

The supported install path is Docker Compose. Release tags publish only the
container image at `ghcr.io/chetto1983/aura:<version>`.

## What Runs

The default stack starts:

- `aura`: the Telegram bot, memory engine, tools, and embedded dashboard.
- `aura-secrets`: sidecar that decrypts secrets for Aura at startup.
- `searxng`: local web search for the `web_search` tool.
- `qdrant`: vector-search sidecar for wiki memory.
- `garage`: S3-compatible artifact and backup storage.
- `garage-webui`: optional Garage admin UI behind a Compose profile.
- `aura-llama-embed`: llama.cpp sidecar serving `embeddinggemma-300m` at 256-d MRL.
- `aura-markitdown`: sidecar that converts docx/xlsx/pptx and 6 other formats to Markdown.
- `aura-whisper`: whisper.cpp sidecar transcribing Telegram voice memos via `ggml-small.bin` (Phase-MM Wave 2).
- `aura-pocket-tts`: Kyutai Pocket-TTS sidecar with the Italian Giovanni voice (Phase-MM Wave 3, INT8, ~200ms first-chunk; default `AURA_TTS_ENABLED=false`).
- `aura-init-models`: one-shot init container that fetches the embedding GGUF + Whisper GGML with SHA-256 verification before the dependent sidecars start. Pocket-TTS downloads its own models at runtime.

Primary user data stays in visible folders beside the Compose file:

- `data/`: SQLite database, logs.
- `runtime-workspace/`: MCP config, prompt overlays, runtime workspace files.
- Docker volume `aura-wiki`: compiled memory pages and source evidence.
- Docker volume `aura-skills`: installed agent skills.
- `garage/`: Garage object storage data.

Code execution runs Python directly inside the Aura container.

## Architecture (current state — post 2026-05-17)

Five channels feed one Hub. The Hub routes inbound messages to a single
agent loop and broadcasts outbound events back to whichever channel is
appropriate. The agent loop has one LLM client, one tool registry, and a
typed memory layer split into Storage (durable) and Memory (rebuildable).

```mermaid
flowchart TB
  subgraph Channels["Channels (internal/channels/*)"]
    TG[Telegram]
    WEB[Web Chat /api/chat]
    CRON[Cron silent]
    SWARM[Swarm subagent]
    SILENT[Silent agent]
  end

  subgraph Hub["Chat Hub (internal/chat)"]
    HIN[Inbound Adapter<br/>normalize → InboundMessage]
    HOUT[Outbound Adapter<br/>OutboundEvent → channel reply]
    HLIFE[Lifecycle<br/>questions / runs / events]
  end

  subgraph Agent["Agent Loop (internal/agent)"]
    LOOP[runLoop<br/>build → LLM → tools<br/>per-turn budget]
    EXEC[ToolExecutor<br/>concurrent fan-out<br/>retry budgets]
    GUARD[Phantom guard<br/>tool-arg redaction]
  end

  subgraph LLM["LLM (internal/llm)"]
    CLIENT[OpenAI-compat client<br/>OpenRouter / Ollama]
    RETRY[retry.go<br/>not yet wired everywhere]
  end

  subgraph Memory["Memory (internal/storage)"]
    WIKI[(wiki/*.md<br/>Storage)]
    SRC[(sources/store<br/>Storage)]
    CMD[(compact_memory_documents<br/>Memory, freshness-tracked)]
    NOTE[(agent_note<br/>Memory)]
    LRN[(learning<br/>tool_attempts → lessons)]
  end

  subgraph Tools["Tool Registry (internal/agent/tools/registry) — v0.3.0 post-TOOL kitchen-sink collapses"]
    SEARCH[search<br/>unified: action=read/search<br/>over wiki + sources + memory]
    WEBT[web_search / web_fetch]
    SRCT[source<br/>unified action enum]
    TASKT[task<br/>mode=create/list/cancel]
    DOCT[create_document<br/>format=xlsx/docx/pdf]
    WSPT[workspace<br/>action=read/write/list]
    SKILL[read_skill]
    AUTH[request_dashboard_token]
    MCP[mcp_* + tools-overrides.json sidecar]
    EXECT[execute_code / execute_shell]
    PROPOSE[propose_patch<br/>RiskTier=write_proposal]
    SUBA[subagent_dispatch]
    ANOTE[agent_note]
  end

  subgraph Identity["Identity (internal/identity)"]
    AUTHZ[Authorize<br/>capability gates]
    GRANTS[(grants table<br/>Storage)]
    DECISIONS[(authz_decisions<br/>Storage, dashboard)]
  end

  TG --> HIN
  WEB --> HIN
  CRON --> HIN
  SWARM --> HIN
  SILENT --> HIN

  HIN --> HLIFE
  HLIFE --> LOOP
  LOOP --> EXEC
  LOOP --> GUARD
  LOOP --> CLIENT
  CLIENT -. retry on 429 .-> RETRY

  EXEC --> Tools
  Tools --> Memory
  Tools --> AUTHZ
  AUTHZ --> GRANTS
  AUTHZ --> DECISIONS

  LOOP --> HOUT
  HOUT --> TG
  HOUT --> WEB
  HOUT --> CRON
  HOUT --> SWARM

  style WIKI fill:#fff4d6,stroke:#bf9000
  style SRC fill:#fff4d6,stroke:#bf9000
  style GRANTS fill:#fff4d6,stroke:#bf9000
  style DECISIONS fill:#fff4d6,stroke:#bf9000
  style CMD fill:#d6e9ff,stroke:#0070d0
  style NOTE fill:#d6e9ff,stroke:#0070d0
  style LRN fill:#d6e9ff,stroke:#0070d0
```

**Legend**: yellow = Storage (durable), blue = Memory (rebuildable). See
[`docs/MEMORY-VS-STORAGE.md`](docs/MEMORY-VS-STORAGE.md) for the full
classification of every `internal/` subdir.

## Roadmap

Aura's PRD lives at [`prd.md`](prd.md). The 2026-05-22 release **v0.3.0**
closed three back-to-back substrate milestones (174 commits since v0.2.0):
Step 1.LAT (latency), Phase-WIKI-FIX (retrieval substrate), Phase-TOOL
(tool surface + kill-the-RAG). Active planning lives in
[`.planning/post-drift-2026-05-21/INDEX.md`](.planning/post-drift-2026-05-21/INDEX.md).

```mermaid
flowchart LR
  V03[v0.3.0 — 2026-05-22<br/>Step 1.LAT + WIKI-FIX + TOOL<br/>DONE 25 stories] --> BUG[Phase-BUG<br/>3 stories ~1s<br/>NEXT]
  BUG --> MOD[Phase-MODERNIZE<br/>5 INFRA + 10 god-splits<br/>~3s]
  MOD --> OUT[Phase-OUT<br/>output discipline +<br/>budget enforcement<br/>9 stories ~3s]
  OUT --> CTX[Phase-CTX<br/>context engineering<br/>~3s]
  CTX --> CONS[Phase-CONS<br/>web/telegram 1+1<br/>~3s]
  CTX --> SUB[Phase-WIKI-SUBNODES<br/>parallel ~1s]

  style V03 fill:#90ee90,stroke:#009900
  style BUG fill:#ffd700,stroke:#cc9900
  style MOD fill:#fff4d6,stroke:#bf9000
  style OUT fill:#fff4d6,stroke:#bf9000
  style CTX fill:#d6e9ff,stroke:#0070d0
  style CONS fill:#d6e9ff,stroke:#0070d0
  style SUB fill:#e6e6ff,stroke:#666699
```

| Phase | Status | Scope | Reference |
| --- | --- | --- | --- |
| **v0.3.0 release** | ✅ shipped 2026-05-22 | 174 commits / 25 stories across Step 1.LAT + Phase-WIKI-FIX + Phase-TOOL — see [release notes](https://github.com/chetto1983/Aura/releases/tag/v0.3.0) | tag `v0.3.0` |
| Phase-MM Wave 1.5 | ✅ done 2026-05-19 | aura-whisper + (later pocket-tts) sidecars + init-models extension | [`docs/phase-mm-audio-spike-2026-05-19.md`](docs/phase-mm-audio-spike-2026-05-19.md) |
| Phase-MM Wave 2 | ✅ done 2026-05-19 | audio IN E2E: KindAudio + voice handler + whisper client + AfterTranscribeHook | [`docs/phase-mm-audio-plan-2026-05-17.md`](docs/phase-mm-audio-plan-2026-05-17.md) |
| Phase-MM Wave 3 | ✅ done 2026-05-20 | TTS reply via Kyutai Pocket-TTS (Giovanni IT voice, INT8, ~200ms first-chunk); per-chat voice mode | [`docs/phase-mm-synthesis-2026-05-17.md`](docs/phase-mm-synthesis-2026-05-17.md) |
| **Phase-BUG** | 🔴 NEXT | 3 verified-still-present bugs — `/api/chat` overlay loading, `logging→api` boundary (590 transitive deps), errcheck-hidden silent failures | [`.planning/post-drift-2026-05-21/Phase-BUG/plan.md`](.planning/post-drift-2026-05-21/Phase-BUG/plan.md) |
| Phase-MODERNIZE | 🟣 staged | 5 INFRA hygiene (depguard + deadcode + 600-LOC linter + lefthook + MODULE-HEALTH.md) + Wave-1 10 god-file splits | [`.planning/post-drift-2026-05-21/Phase-MODERNIZE/plan.md`](.planning/post-drift-2026-05-21/Phase-MODERNIZE/plan.md) |
| Phase-OUT | 🟣 staged | Output discipline stack + 3 NEW budget enforcement stories (per-class budget, OR-of-four guard, force-finalize) | [`.planning/post-drift-2026-05-21/Phase-OUT/plan.md`](.planning/post-drift-2026-05-21/Phase-OUT/plan.md) |
| Phase-CTX | 🟣 staged | Context engineering substrate (ContextEngine + payload summarizer + auto-compaction at 70%) | [`.planning/post-drift-2026-05-21/Phase-CTX/plan.md`](.planning/post-drift-2026-05-21/Phase-CTX/plan.md) |
| Phase-CONS | 🟣 staged | Web ↔ Telegram 1+1 consolidation — substantive web feature additions (streaming, voice, ask_user, archive) | [`.planning/post-drift-2026-05-21/Phase-CONS/plan.md`](.planning/post-drift-2026-05-21/Phase-CONS/plan.md) |
| Phase-U | 🟣 planned | Plugin manifest + loader + extract personality bundle + sample plugin | sketched in `prd.md §7.4` |
| Phase 8 | ⚪ gated on workload | Multi-agent substrate (planner + critic + DAG) anchored to a concrete workload | de-scoped pending re-open |

## Multimodal target (Phase-MM)

Phase-MM adds audio + image flows on top of the same substrate. Only 3 ARCH
stories (Wave 1) actually change shared code; the rest are additive paths
that reuse the existing source store + tool registry.

```mermaid
flowchart TB
  subgraph TGV["Telegram (voice/photo)"]
    VOICE[voice_handler<br/>NEW MM-AUDIO01]
    PHOTO[photos.go<br/>NEW MM-IMAGE01]
  end

  subgraph SUB["Substrate change Wave 1"]
    ATT[InboundMessage.Attachments<br/>ARCH01 populated]
    MULTI[llm.Message.MultipartContent<br/>ARCH02 new field]
    KIND[source.Kind +audio +image<br/>ARCH03 enum extension]
  end

  subgraph SRC["Source store (existing)"]
    AUDIO[(KindAudio<br/>raw + transcript.txt)]
    IMG[(KindImage<br/>raw + vision.md)]
  end

  subgraph WSP["Whisper sidecar"]
    WHISP[whisper.cpp whisper-small-ita<br/>Italian finetuned · CPU 4 threads<br/>NEW MM-AUDIO02]
  end

  subgraph LLM2["LLM (Phase-MM enabled)"]
    VISION[Sonnet 4.6 vision<br/>via OpenRouter<br/>multipart message]
  end

  subgraph TTSOUT["Audio OUT (operator-toggle)"]
    PIPER[Piper TTS<br/>piper_italiano<br/>NEW MM-AUDIO03]
    OGG[ffmpeg → .ogg/opus]
  end

  subgraph GEN["Image OUT"]
    REPL[Replicate Flux.1-schnell<br/>generate_image tool<br/>NEW MM-IMAGE02]
  end

  VOICE --> ATT
  PHOTO --> ATT
  ATT --> AUDIO
  ATT --> IMG
  AUDIO --> WHISP
  WHISP -->|transcript| MULTI
  IMG -->|image_url| MULTI
  MULTI --> VISION
  VISION -->|text reply| PIPER
  PIPER --> OGG
  OGG -.->|TTS enabled| TGV
  REPL -.->|generate tool call| IMG

  style ATT fill:#ffe4e1,stroke:#cc0000
  style MULTI fill:#ffe4e1,stroke:#cc0000
  style KIND fill:#ffe4e1,stroke:#cc0000
```

**Red = substrate Wave 1 stories.** Wave 1.5 (sidecar substrate) + Wave 2
(audio IN E2E) shipped 2026-05-19 — see
[`docs/phase-mm-audio-plan-2026-05-17.md`](docs/phase-mm-audio-plan-2026-05-17.md)
for the post-ship status block.

Defaults shipped:

- **Audio IN ✅** whisper.cpp local with baseline `ggml-small.bin` (multilanguage, 487 MB, CPU 4 threads, ~3-7s on a 3s memo). `litus-ai/whisper-small-ita` finetuned model is an env-var swap when wanted
- **Audio OUT ✅** Kyutai Pocket-TTS local with Giovanni IT voice (INT8, RTF 0.61, ~200ms first-chunk streaming). Per-chat `voice_mode` (off / voice_only / all) — default OFF, never auto-triggered. Wave 3 closed 2026-05-20
- **Image IN (planned)** Anthropic Sonnet 4.6 vision via OpenRouter
- **Image OUT (planned)** Replicate Flux.1-schnell

All API providers are operator-overrideable; local fallbacks (Qwen2.5-VL,
Stable Diffusion) become plugin-shaped opt-ins after Phase-U.

## Plugin layout target (Phase-U)

Aura's substrate is genuinely domain-agnostic. Phase-U decouples
personality (overlays + wiki + MCP wiring + tool curation + capability
declarations) into installable bundles. The same Aura binary can become a
different assistant depending on which plugin is loaded.

```mermaid
flowchart TB
  subgraph Substrate["Substrate (unchanged from Phase-MM)"]
    HUB[Chat Hub]
    LOOP[Agent Loop]
    TOOLS[Tool Registry]
    MEM[Memory layers]
    LLM[LLM Client + multipart]
    IDENT[Identity + capability gates]
  end

  subgraph Loader["Plugin Loader NEW Phase-U"]
    REG[installed_plugins table<br/>SQLite]
    INST[aura plugin install<br/>aura plugin list<br/>aura plugin remove]
    BOOT[Boot scanner<br/>reads manifest + applies]
  end

  subgraph Plugin1["aura-personal default plugin"]
    P1OV[overlays/<br/>SOUL+AGENT+USER+TOOLS.md]
    P1SK[skills/<br/>Davide skills]
    P1MCP[mcp.json<br/>Davide MCP servers]
    P1TOOL[tools curation<br/>memory + wiki + scheduler]
    P1CAP[capabilities<br/>memory.* + tool.*]
  end

  subgraph Plugin2["aura-marketer sample plugin"]
    P2OV[overlays/<br/>research persona]
    P2SK[skills/<br/>competitor-report]
    P2MCP[mcp.json<br/>SerpAPI + scraping]
    P2TOOL[tools curation<br/>web + source + propose_patch]
    P2CAP[capabilities<br/>web.fetch + source.write]
  end

  Plugin1 -. installed .-> REG
  Plugin2 -. installed .-> REG
  BOOT --> REG
  BOOT --> Substrate

  P1OV -. overlay .-> LOOP
  P1SK -. skill .-> TOOLS
  P1MCP -. mcp .-> TOOLS
  P1TOOL -. allowlist .-> TOOLS
  P1CAP -. grants .-> IDENT

  P2OV -. overlay .-> LOOP
  P2SK -. skill .-> TOOLS
  P2MCP -. mcp .-> TOOLS
  P2TOOL -. allowlist .-> TOOLS
  P2CAP -. grants .-> IDENT

  style Plugin1 fill:#e6ffe6,stroke:#009900
  style Plugin2 fill:#e6f3ff,stroke:#0066cc
  style Loader fill:#fff0e6,stroke:#cc6600
```

**Green** = the current Davide deploy extracted as a plugin (no behavior
change for the operator). **Blue** = a second plugin proving Aura can
specialize; the substrate doesn't change between them. **Orange** = the
new plugin loader.

Plugins specialize *which model* (Whisper local vs Groq; Sonnet vs Qwen-VL)
but never *whether the system sees audio* — that's substrate. See
[`prd.md §7.4`](prd.md) for the substrate-vs-plugin classification rule.

## Quick Start

Prerequisites:

- Docker Desktop or Docker Engine with Docker Compose.
- A Telegram bot token from [@BotFather](https://t.me/BotFather).
- An OpenAI-compatible LLM endpoint.

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

`compose.image.yaml` pulls four images from GHCR: `aura`, `aura-whisper`,
`aura-pocket-tts`, `aura-markitdown`. Cold first start: ~5-10 min. Without
this overlay (`up -d --build`), the four sidecars compile from source —
expect ~45-60 min cold.

Open the setup wizard at `http://127.0.0.1:18080`.

For production hardening (restart-on-failure, no LAN dashboard exposure,
tightened healthcheck thresholds):

```powershell
docker compose -f compose.yaml -f compose.prod.yaml up -d
```

See [`docs/RUNBOOK.md`](docs/RUNBOOK.md) for the full operator runbook
(cold install, upgrade, rollback, SQLite restore drill, secret rotation).

## Update

```powershell
docker compose -f compose.yaml -f compose.image.yaml pull aura
docker compose -f compose.yaml -f compose.image.yaml up -d
```

Pin a release with `AURA_IMAGE`:

```powershell
$env:AURA_IMAGE = "ghcr.io/chetto1983/aura:v1.2.3"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

## Develop

Build the local working tree instead of pulling GHCR:

```powershell
docker compose up -d --build
```

Run the test suite:

```powershell
docker compose --profile test run --rm test
```

Local Go commands:

```powershell
go test ./...
go build ./...
go run ./cmd/aura
```

Lint:

```powershell
~/go/bin/golangci-lint run ./...
```

## Release

```powershell
git tag v1.2.3
git push origin v1.2.3
```

The `Docker image` workflow publishes:

- `ghcr.io/chetto1983/aura:v1.2.3`
- `ghcr.io/chetto1983/aura:latest`
- a `sha-*` traceability tag

## Docs

- [Install guide](INSTALL.md)
- [Container stack](docs/container.md)
- [Operator runbook](docs/RUNBOOK.md) — cold install, upgrade, restore, secret rotation
- [Product requirements](prd.md) — full PRD with phase status checklist (§6.5) and strategic roadmap (§7.4)
- [Memory vs Storage](docs/MEMORY-VS-STORAGE.md) — two-layer model + `internal/` subdir classification
- [Phase-MM synthesis](docs/phase-mm-synthesis-2026-05-17.md) — multimodal planning (9 stories, 3 waves)
- [Audit reports](docs/) — 11 reports from the 2026-05-17 audit sweep (security / concurrency / dead-code / etc.)
