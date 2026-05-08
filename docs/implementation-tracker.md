# Aura Implementation Tracker

2026-05-08 Runtime Diet planning closure: updated `.planning/STATE.md`, `.planning/ROADMAP.md`, Phase 08 `PLAN.md`, and `NOT-DELETED.md` after the Docker/container E2E closure. v3.2 Runtime Diet is now marked closed, compact-memory indexing is no longer listed as future work, Task 7 is closed with only historical grep hits remaining, and Task 8's remaining runner-boundary work is explicitly moved to a follow-up phase. Next recommended slice is Runner Boundary & Health Hardening: finish moving session/finalization behind `internal/agentruntime`, expose compact mirror health, and validate broad non-document prompts under the 30s budget.

2026-05-08 compact-memory Qdrant mirror slice: promoted the compact-memory Qdrant shape into production wiring. `memoryindex.Store` now accepts an optional vector mirror, merges exact SQLite, FTS, and vector hits in `Search`, mirrors successful upserts after SQLite commit, and can rebuild the vector mirror from all compact rows. Added `search.CompactMemoryQdrantIndex`, using a separate `<QDRANT_COLLECTION>_compact` collection so wiki rebuilds cannot delete compact source/archive/proposal points. Upserts recreate the compact collection after empty rebuilds, archive cleanup purges compact rows plus Qdrant points, Telegram injects the real chat scope into `search_memory`, and `scope=all` uses one compact exact/FTS/vector query for source/archive/proposal facts. Container E2E exposed that cold compact mirror sync was too slow when embedding archive rows sequentially, so startup now rebuilds SQLite/FTS on the short budget and runs Qdrant mirror sync as background maintenance with adaptive OpenAI-compatible batch embeddings. Telegram enables the mirror when `SEARCH_BACKEND=qdrant` and embeddings are configured; failures degrade to SQLite compact memory. Verification: `go test ./internal/memoryindex ./internal/search ./internal/tools ./internal/telegram -count=1`; `go test ./...`; `go build ./...`; `docker compose up -d --build aura`; `/status` healthy; container logs show compact mirror synced with `vector_docs=487`; `scripts/test-compact-memory-qdrant.ps1` passed against local Qdrant with graph-expanded nodes; `cmd/debug_telegram_sandbox` document E2E passed with `loop_steps=1`, `llm_calls=1`, `tool_calls=1`, `tools_called=create_docx`, `elapsed_ms=13972`.

2026-05-08 compact-memory production index slice: added `internal/memoryindex` with SQLite-backed `compact_memory_documents` plus `compact_memory_fts` for source/archive/proposal facts, wired Telegram startup to rebuild the compact index outside the hot turn path, and wrapped archive appends so new turns are mirrored into compact memory after persistence. `search_memory` now depends on wiki search plus the compact index instead of `source.Repository` and `conversation.TurnReader`; the old raw source/archive scan functions, scan/read limits, and lexical scan scorer were physically deleted from the tool. Verification: `go test ./internal/memoryindex ./internal/tools ./internal/db/migrations -count=1`; `go test ./...`; `go build ./...`; `rg -n "searchSources|searchArchive|searchMemoryScanLimit|searchMemoryReadLimit|lexicalScore|NewSearchMemoryTool\\([^\\n]+,[^\\n]+,[^\\n]+\\)" D:\Aura` returned no matches.

2026-05-08 compact-memory Qdrant PoC script: added `scripts/test-compact-memory-qdrant.ps1`, a non-production test harness that creates a temporary Qdrant collection, indexes synthetic compact memory facts plus graph entity nodes, runs vector search, fuses it with local lexical scoring, expands graph neighbors from top seeds, and prints a compact Retrieval Capsule. The script deletes its temporary collection by default and does not touch `data/aura.db` or live Aura state. Verification: `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\test-compact-memory-qdrant.ps1` passed against Docker Qdrant, returning `fact:document-loop-62s`, `fact:document-hot-path-cut`, `entity:document-route`, `entity:performance`, and graph-expanded `entity:agent-loop`.

2026-05-08 document hot-path cutoff slice: tightened the ADK-style dynamic toolset path so document turns can skip the retrieval tool entirely when the precomputed Retrieval Capsule is sufficient. `composeTurnRetrievalCapsule` now returns metadata (`HasEvidence`, `SuppressSearchMemory`) instead of a bare string, and Telegram uses that metadata to filter `search_memory` out of the exposed document toolset only when it is safe. Broad empty-memory document turns keep `search_memory` as a one-call fallback, preserving source/archive reachability. The document prompt now explicitly tells the model to use only the currently exposed tools, removing the prior hidden `read_file` retry. Verification: `go test ./internal/agentruntime ./internal/agentloop ./internal/orchestration ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`; `go test ./...`; live `go run ./cmd/debug_telegram_sandbox ... -expect-toolset document -expect-retrieval-capsule -expect-tools create_docx -expect-terminal-tool create_docx -expect-loop-steps-max 1 -max-elapsed-ms 15000` passed with `elapsed_ms=12976`, `llm_calls=1`, `tool_calls=1`, `loop_steps=1`, `tools_called=create_docx`, and `hidden_tool_rejected=false`.

2026-05-08 ADK-style event runner extraction slice: added `internal/agentruntime`, a small Aura-native runner wrapper that emits `tools_exposed`, `stats`, and `final` events around the existing model/tool loop. Telegram now listens to runner events for orchestration snapshots and tool-exposure tracing instead of owning that event flow directly. `internal/orchestration` now has `RuntimeToolset.Tools(ctx)` and `FilterToolset` so tool exposure can become invocation-aware like ADK's toolsets while preserving the existing four toolsets. Repeated retrieval and duplicate suppression are now configured through `BeforeToolCallbackForToolset`; document-route `search_memory` next-step guidance is now `AfterToolCallbackForToolset`, injected as a post-tool result shaping callback rather than hardcoded in Telegram. Remaining ADK-style work: move archive/session persistence and terminal-tool finalization fully behind the runner boundary. Verification so far: `go test ./internal/agentruntime ./internal/agentloop ./internal/orchestration ./internal/telegram ./internal/tools -count=1`.

2026-05-08 ADK-Go orchestration review slice: reviewed `google/adk-go` (`agent`, `runner`, `session`, `tool`, `tool/functiontool`, workflow agents) and updated Phase 08 with an Aura-native orchestration target. Useful patterns to adopt: `Runner` owns session/event persistence and invocation context; `Agent.Run` yields events; `Toolset.Tools(ctx)` plus predicates replaces prompt-era profile/preflight routing; before/after model/tool callbacks hold request shaping, duplicate suppression, tool-result shaping, and confirmation gates; `EventActions` cleanly carries state/artifact deltas, transfer/escalation, and skip-summarization intent; agent-as-tool is the right shape for bounded swarm/sub-agent work. Non-goal recorded: do not import ADK wholesale or reintroduce telemetry/profile ceremony. Sources reviewed: `https://github.com/google/adk-go`, `D:\tmp\adk-go\agent\llmagent\llmagent.go`, `D:\tmp\adk-go\runner\runner.go`, `D:\tmp\adk-go\tool\tool.go`, `D:\tmp\adk-go\tool\functiontool\function.go`, `D:\tmp\adk-go\session\session.go`.

2026-05-08 document loop and streamed JSON repair slice: after the LangChain-style definitions, the live document smoke dropped the hidden `read_file` retry but exposed a streamed `create_docx` argument error (`invalid character '}' after array element`). Added a document-route post-`search_memory` next-step hint in the tool result so the model is pushed directly toward one typed file tool, and added recoverable JSON closer repair in the OpenAI-compatible tool-call parser for malformed streamed arguments with missing array/object closers. Live document smoke now passes again in 20.5s with `search_memory`, duplicate `search_memory` rejected, and `create_docx` delivered; no hidden `read_file` in that run. Verification: `go test ./internal/llm ./internal/telegram ./internal/tools ./internal/agentloop ./internal/orchestration ./cmd/debug_telegram_sandbox -count=1`; live `go run ./cmd/debug_telegram_sandbox ... -expect-toolset document -expect-retrieval-capsule -expect-tools search_memory,create_docx -expect-terminal-tool create_docx -max-elapsed-ms 30000` passed.

2026-05-08 LangChain-style tool definition slice: replaced registry-side string decoration with Aura's own `ToolDefinition` contract. Tools can now implement `Definition()` to declare name, description, JSON schema, and structured call examples; the registry converts that canonical shape to `llm.ToolDefinition`, while older/dynamic/MCP tools are adapted with schema-derived fallback examples. `search_memory`, `create_docx`, `create_xlsx`, and `create_pdf` now own explicit definitions, keeping document-generation call shapes close to the typed tools instead of in route prompts. Verification: `go test ./internal/tools -count=1`; `go test ./internal/tools ./internal/orchestration ./internal/llm ./internal/agentloop ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`.

2026-05-08 Runtime Diet retrieval/document slice: deleted the live `Memory Pack` implementation and replaced it with a compact `## Retrieval Capsule` route (`minimal`, `retrieve`, `produce`) that no longer reads `wiki/graph/context.md` or `wiki/log.md` into the hot path. `search_memory` now calibrates wiki/source/archive scores before merging, caps source scan reads to smaller windows, and returns stable follow-up handles such as `source:<id>#page=N` and `conversation:<id>` while keeping output snippets compact. The document toolset was narrowed to `search_memory` plus `create_docx`/`create_xlsx`/`create_pdf`; workspace/source/web exploration tools are no longer exposed on the normal document route. Removed toolset-specific `MaxSteps` caps so the DB/dashboard `AgentLoopMaxSteps` setting remains authoritative instead of being silently reduced to 4. Added `.planning/phases/08-runtime-diet-embedding-retrieval/NOT-DELETED.md` for intentionally retained explicit tools. Added `scripts/capture-aura-health.ps1`, a read-only evidence collector that saves Docker Compose status/logs, container `/status`, optional dashboard `/conversations`, optional Telegram bot health, and optional Telegram debug smoke under ignored `reports/health/<timestamp>/`. Verification: `go test ./internal/conversation ./internal/telegram ./internal/orchestration ./internal/tools ./cmd/debug_telegram_sandbox -count=1`; `go test ./...`; `go build ./...`; `docker compose config --quiet`; `docker compose up -d --build aura`; `/status` ok; health snapshot script produced `reports/health/20260508-170934`. Live document smoke exposed the remaining model/tool-call issue: repeated `search_memory`, hidden `read_file` rejected, and malformed streamed `create_docx` JSON, so malformed-JSON repair remains open.

2026-05-08 Hermes-style tool-call examples slice: moved tool-call examples to the actual LLM-facing tool definitions instead of scattering examples in the document route prompt. `Registry.Definitions` and `DefinitionsFor` now append `Tool call examples:` to every tool description; known Aura tools have hand-written examples and dynamic/MCP tools receive schema-derived fallback examples, so no exposed tool reaches the model without a call shape. Kept the parser recovery for streamed tool-call arguments that contain a valid first JSON object followed by model spillover, and added cross-iteration duplicate/repeated retrieval suppression plus a one-search cap for the document toolset. Live document smoke now passes: `search_memory` then `create_docx`, DOCX delivered, `elapsed_ms=26191` under the 30s budget. Residual behavior: the model still attempted hidden `read_file` once and a second `search_memory`, both rejected without breaking the turn. Verification: `go test ./internal/tools ./internal/orchestration ./internal/llm ./internal/agentloop ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`; live `go run ./cmd/debug_telegram_sandbox ... -expect-toolset document -expect-retrieval-capsule -expect-tools search_memory,create_docx -expect-terminal-tool create_docx -max-elapsed-ms 30000` passed.

2026-05-08 runtime wiki storage fix: corrected the remaining runtime-memory leak from the development tree. Compose no longer bind-mounts `./wiki` into `/workspace/wiki`; it mounts the named `aura-wiki` Docker volume instead. Existing `D:\Aura\wiki` content was copied into the volume before removing the host tree and deleting tracked `wiki/.gitignore` plus `wiki/SCHEMA.md`. `.gitignore` now ignores `/wiki/`, `.env.example` and config defaults point local runs at `./runtime-workspace/wiki`, and `AGENTS.md` records that the wiki is runtime memory, not source. Removed the obsolete project-skill fixture tests that still required repo-local `skills/` content; loader behavior remains covered by temp-directory tests. Verification: `docker compose config --quiet`; `go test ./... -count=1`; `go build ./...`; Docker Aura healthy on `/status`, `/workspace/wiki` mounted from the Compose-labeled `aura-wiki` volume with 29 top-level markdown pages and 1306 total wiki files, and no host `D:\Aura\wiki` directory remains.

2026-05-08 runtime skills storage fix: corrected the Docker layout that let runtime skills leak into the repository. Compose no longer bind-mounts `./skills` into `/workspace/skills`; it mounts the named `aura-skills` Docker volume instead, while `SKILLS_PATH` and `SKILLS_INSTALL_PROJECT_DIR` still point at `/workspace/skills` inside the container. `.gitignore` now ignores `/skills/`, `.env.example` points desktop/local runs at `./runtime-workspace/skills`, and `AGENTS.md` records that skills are runtime data, not source. Existing local skills were migrated into the Docker volume before removing the host `skills/` tree from the repo.

2026-05-08 Runtime Diet deletion pass: used the live-log failure as the cut line and physically removed the old default runtime machinery instead of leaving it disabled. Deleted the OpenTelemetry runtime package and all span wiring from startup, LLM clients, and skill loader; removed `OTEL_ENABLED` from config/settings/API/env/Compose; deleted the post-turn summarizer runner/scorer/deduper and `cmd/debug_summarizer`; removed summarizer settings/env/Compose/config fields and startup wiring; removed nightly auto-improve scheduler dispatch/config/helpers; removed the compounding-rate health metric, card, and tests; rebuilt the dashboard assets so stale settings groups and health labels are gone. The remaining review queue is now treated as manual proposals: approval writes `proposal_v1` pages/log entries while `summarizer_v{n}` stays accepted only for legacy wiki pages. Verification: `go test ./internal/config ./internal/settings ./internal/api ./internal/telegram ./internal/conversation/summarizer ./internal/search ./internal/wiki -count=1`; `npm --prefix web run i18n:check`; `npm --prefix web run build`; `go test ./... -count=1`; `go build ./...`; `docker compose config --quiet`. Residual note: `go.opentelemetry.io/proto/otlp` remains only as a transitive `go.sum` module hash through `envoyproxy/go-control-plane`, not as Aura code or config.

2026-05-08 Runtime Diet live-log repair slice: investigated the post-rebuild logs after a live Telegram turn still behaved too ceremonially. Root cause was persisted SQLite settings overriding the new Compose/env defaults: `OTEL_ENABLED=true`, `SUMMARIZER_ENABLED=true`, `SUMMARIZER_MODE=auto`, and `SANDBOX_AUTO_IMPROVE_MODE=auto` were still in `settings`, so Aura emitted huge stdout OpenTelemetry spans, ran post-turn memory capture, patched wiki pages, and kept nightly auto-improve active. Bumped `AURA_BEST_DEFAULTS_VERSION` to `2026-05-08-runtime-diet-v2` and added one-time migration rules to reset those rows to `false/off/off`; rebuild verified the live DB now has the Runtime Diet values and logs show tracing disabled plus auto-improve disabled. Also fixed SQLite FTS query sanitization by tokenizing user text into alphanumeric/underscore terms, covering punctuation-heavy prompts such as `wiki/log.md, source:uta` and `l'agente...` without FTS5 syntax errors. Verification: `go test ./internal/settings ./internal/search -count=1`; `go test ./internal/config ./internal/settings ./internal/search ./internal/telegram ./cmd/aura -count=1`; `go build ./...`; `docker compose up -d --build aura`; `/status` ok; DB readback shows `OTEL_ENABLED=false`, `SUMMARIZER_ENABLED=false`, `SUMMARIZER_MODE=off`, `SANDBOX_AUTO_IMPROVE_MODE=off`; `go test ./... -count=1`; `docker compose config --quiet`.

2026-05-08 Runtime Diet profile/preflight deletion + hybrid retrieval slice: physically removed the remaining live profile/preflight stack instead of hiding it behind defaults. Deleted the old orchestration capability cards, skill preflight policy/tests, swarm routing prompt helper/tests, and `cmd/debug_orchestration`; removed `AURA_SKILL_PREFLIGHT`; replaced `AURA_TOOL_PROFILE_MODE` with `AURA_TOOLSET_MODE`; removed legacy `memory`, `swarm_research`, `sandbox_compute`, and `admin_review` aliases; renamed orchestration hooks/debug telemetry from profile to toolset; and rebuilt `internal/api/dist` so stale dashboard labels are gone. Search now performs hybrid exact/FTS/vector retrieval: exact wiki slug/title/link hits and SQLite FTS are merged with chromem vectors, and Qdrant merges local hybrid results instead of letting any non-empty vector response suppress lexical evidence. Verification: `go test ./internal/orchestration ./internal/toolsets ./internal/scheduler ./internal/search ./internal/config ./internal/settings ./internal/api ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`; `npm --prefix web run i18n:check`; `npm --prefix web run build`; `rg -n "SkillPreflight|skillPreflight|AURA_SKILL_PREFLIGHT|ActiveCapabilities|activeCapabilities|CapabilitiesForProfile|Capability|ProfileCard|ModuleSwarm|SwarmRoutingPrompt|AURA_TOOL_PROFILE_MODE|tool_profile|ToolProfile|LoopPolicyForProfile|SelectProfile|ToolsForProfile|ProfileMode|ProfileDecision|ProfileDefault|ProfileCompute|ProfileDocument|ProfileAdmin|swarm_research|sandbox_compute|admin_review" internal cmd web/src .env.example compose.yaml internal/api/dist -S` returned no matches; `go test ./... -count=1`; `go build ./...`; `go vet ./...`; `docker compose config --quiet`.

2026-05-08 Runtime Diet hot-path cut slice: removed the live `agentloop` fallback path that produced the "Mi sono fermato" answer and replaced budget exhaustion with last-useful-tool-result finalization. Generic Telegram turns no longer run speculative embedding search or inject Memory Pack context; memory/wiki/document/log/graph prompts still route to a bounded pack when relevant, and stale packs are cleared each turn. Skill manifests and AuraBot swarm exposure are now explicit-only rather than injected/exposed for every normal turn. Post-turn summarizer and nightly auto-improve are opt-in by default (`SUMMARIZER_ENABLED=false`, `SUMMARIZER_MODE=off`, `SANDBOX_AUTO_IMPROVE_MODE=off`), startup no longer bootstraps `nightly-auto-improve` unless enabled, and existing `nightly-auto-improve` tasks are cancelled when the mode is off. Added Phase 08 evidence notes. Verification: `go test ./internal/agentloop -count=1`; `go test ./internal/config ./internal/settings ./internal/api ./internal/telegram ./internal/orchestration ./internal/conversation -count=1`; `go test ./...`; `go build ./...`; `docker compose config --quiet`; `rg -n "Mi sono fermato|fallbackMessage|FallbackMessage" internal` returned no matches.

2026-05-08 Runtime Diet planning slice: opened `.planning/phases/08-runtime-diet-embedding-retrieval/PLAN.md` after tracing the remaining "Aura si pianta" behavior to three concrete causes: the hardcoded `Mi sono fermato` agent-loop fallback, always-on speculative Memory Pack injection of search/graph/log context, and uncalibrated embedding retrieval that treats non-empty vector results as sufficient evidence. Updated `.planning/STATE.md` and `.planning/ROADMAP.md` so v3.2 Runtime Diet is the active milestone. The plan is deletion-first: keep memory/wiki in files, few bounded file tools, fast search, a simple loop, and an always-useful final answer. Embeddings stay, but only as part of compact hybrid retrieval when the prompt needs memory. Summarizer, swarm, proposals, validation, and self-improvement move out of the default hot path. No production code changed in this slice.

2026-05-08 planning cleanup slice: synchronized `.planning` after the simplification branch was merged to `master`. Rewrote `.planning/STATE.md` around the current runtime truth (`master`, four toolsets, bounded `/workspace`, legacy wrappers removed, Phase 05/06/07 complete), updated `.planning/ROADMAP.md` so v3.1 is now framed as "Agent Runtime Stabilization", marked Phase 06 work items done with cache-refresh follow-ups separated, and added cleanup notes to historical Phase 04 orchestration plans so future agents do not execute obsolete pre-simplification checkboxes. No code changed; verification was a clean git status plus diff review.

2026-05-08 Docker runtime cleanup slice: removed the broad `.:/app` Compose bind mount so the live Aura container no longer exposes the full development repository inside `/app`; Aura's runtime workspace remains the narrow `/workspace` tree with explicit mounts for wiki and skills, plus `/data` for container-owned state. Replaced `.dockerignore` with a default-deny allowlist for the production build context (`go.mod`, `go.sum`, `cmd/aura`, `internal`, and `.env.example` only), keeping local data, Garage/Qdrant state, docs, planning artifacts, logs, reports, test output, web source caches, and agent scratch state out of Docker builds. Fixed early logger initialization so Docker honors `LOG_DIR=/data/logs` before the first log file is opened instead of creating `/app/logs`. Verification: `go test ./cmd/aura -count=1`; `docker compose config --quiet`; `docker compose up -d --build aura` with Aura context `37.91kB`; live `/status` ok; container probe confirmed `/app` contains only `/app/runtime` and lacks `/app/internal`, `/app/cmd`, `/app/.git`, `/app/.env`, and `/app/logs`.

2026-05-08 swarm latency repair slice: fixed the Task 6 residual where the wiki/graph improvement smoke took 36.8s because the AuraBot worker waited on the full 25s swarm timeout after read-only evidence had already been gathered. Root cause: `agent.Runner` used the same parent deadline for the final no-tool worker turn after `MaxToolCalls` was reached. Added per-task `FinalizationTimeout`, wired it through swarm assignments and `SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS`, and set runtime delegation defaults to 4s for `fast` and 7s for `bounded`/`async`. Also expanded swarm routing so `grafo`/`graph` counts as second-brain scope; the exact prompt `come posso migliorare Aura usando la wiki e il grafo?` now gets the swarm-first hint instead of sometimes falling back to sequential file reads. Verification: `go test ./internal/agent ./internal/swarm ./internal/swarmtools ./internal/conversation ./internal/telegram -count=1`; live debug smoke with `-expect-swarm -max-elapsed-ms 30000` passed at 26.8s total, one `run_aurabot_swarm`, 2 loop steps, 15,759 tokens, swarm tool elapsed 7.6s.

2026-05-08 narrow runtime live verification slice: closed Phase 07 Task 6. The Telegram debug sandbox now has first-class assertions for logical workspace root, forbidden path fragments, compact Memory Pack injection, and elapsed time; forbidden fragments can be passed repeatedly or comma-separated. The local debug runner now simulates Docker's `/workspace` bind layout with a temporary runtime workspace containing populated `wiki/` and `skills/`, so smoke prompts no longer accidentally inspect the developer repo or an empty host-side `runtime-workspace/wiki`. Memory-pack detection now requires the actual injected `## Memory Pack` section, not the base prompt's explanatory mention. Startup now rebuilds the materialized wiki graph after YAML migration so existing wikis get `wiki/graph/*` on boot. Live checks: workspace listing PASS (`/workspace`, Memory Pack present, no `internal/`, `.git/`, `cmd/`, `web/`, or `data/aura.db`, 3 steps, 26,200 tokens, 7.9s); wiki/graph improvement PASS functionally (one `run_aurabot_swarm`, 2 steps, 15,141 tokens, 36.8s); skill discovery PASS (runtime skill reads, 3 steps, 28,890 tokens, 14.8s). The 36.8s swarm latency residual was fixed in the following swarm latency repair slice. Verification: `go test ./cmd/debug_telegram_sandbox ./internal/telegram -count=1`; `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`; `docker compose config --quiet`; `docker compose up -d --build aura`; live `/status` ok with logs reporting `workspace file tools enabled root=/workspace`.

2026-05-08 compact memory pack slice: replaced the old single speculative-search injection with a bounded `## Memory Pack` composed from top search hits, materialized `wiki/graph/context.md`, and recent `wiki/log.md` entries. The pack lists relevant `[[slugs]]`, keeps a 10 KiB default budget, and is injected once per Telegram turn after the versioned base prompt is composed. The system prompt now tells Aura to treat the pack as orientation and verify stale details before edits. Verification: `go test ./internal/conversation ./internal/telegram -count=1`.

2026-05-08 materialized wiki graph slice: added `internal/wiki` graph cache types/builders and generated `wiki/graph/graph.json` plus `wiki/graph/context.md` from durable wiki pages. `wiki.Store` now refreshes the graph cache after page writes, deletes, index rebuilds, and YAML migration, while `GET /api/wiki/graph` prefers the materialized cache and rebuilds it if missing or invalid. The graph records nodes, deduped edges, broken refs, and orphans; the markdown context gives Aura a compact graph overview without rescanning every page. Verification: `go test ./internal/wiki ./internal/api ./internal/search -count=1`; `go test ./internal/wiki ./internal/api ./internal/search ./internal/telegram ./internal/conversation/summarizer -count=1`.

2026-05-08 narrow runtime workspace slice: changed Compose so Aura's bounded file tools, prompt overlays, wiki, skills, and MCP config live under `/workspace` (`./runtime-workspace`, `./wiki`, and `./skills` mounted into that narrow tree) while `/app` remains only the implementation bind mount for development. Added settings best-default repair for stale persisted `/app`, `/wiki`, `/skills`, and `/data/mcp.json` rows so DB overrides do not pull the runtime agent back into the broad repository tree. `runtime-workspace/` is ignored as generated local state. Verification: `go test ./cmd/aura ./internal/settings ./internal/api ./internal/telegram -count=1`; `docker compose config --quiet`; `docker compose up -d --build aura`; live `/status` ok; container logs report `workspace file tools enabled root=/workspace`; `/workspace` listing shows runtime/wiki/skills files and no source tree directories.

2026-05-08 runtime layout bootstrap slice: added `AURA_RUNTIME_WORKSPACE_PATH` config and wired Docker defaults so Aura's runtime agent workspace is `/workspace` instead of the full `/app` tree. Added `internal/runtimebootstrap.EnsureLayout`, called before SQLite opens, to create missing runtime workspace files (`AGENT.md`, `HEARTBEAT.md`, `mcp.json`, wiki index/log/graph/raw, skills, inbox) plus parent directories for env, DB, logs, wiki, skills, MCP config, and prompt overlays without overwriting existing user files. This makes first-run container setup and the setup wizard less brittle while keeping Garage backup-only. Verification: `go test ./internal/config -count=1`; `go test ./internal/runtimebootstrap ./cmd/aura -count=1`.

2026-05-08 runtime workspace/graph-cache planning slice: wrote `.planning/phases/07-runtime-workspace-bootstrap-graph-cache/PLAN.md` after comparing Aura's current broad `/app` workspace, 22-page/53-edge wiki graph, 32 raw source directories, and 3 local skills against Picobot's narrow `~/.picobot/workspace` model. The plan scopes six independent implementation tasks: runtime workspace config, first-run local layout bootstrap, narrow Docker mounts, materialized `wiki/graph/graph.json` plus `context.md`, compact pre-turn memory packs, and live benchmark/debug verification. Garage remains backup/artifact-only until restore/import/sync exists.

2026-05-08 AGENT wiki-schema tightening slice: reread `docs/llm-wiki.md` and rewrote root `AGENT.md` from a broad runtime note into Aura's concise LLM Wiki operating schema. It now anchors Aura on raw immutable sources, compiled wiki pages, and `AGENT.md` as schema; it defines ingest/query/lint loops, `wiki/index.md` and `wiki/log.md` upkeep, schema-safe wiki writes, bounded file tools, skill usage, and a clear storage note that Garage is currently the backup/artifact vault, not the live wiki source until a bootstrap/sync slice exists. Garage/setup follow-up: add a local layout bootstrap before DB/setup wizard to create missing parent dirs for env, DB, logs, wiki, skills, and MCP config, create minimal env/MCP files only when missing, and keep Garage backup-only until restore/import is tested. Verification: `go test ./internal/conversation -count=1`; live container probe confirmed `/app/AGENT.md` updated under `PROMPT_OVERLAY_PATH=/app`.

2026-05-08 Aura runtime AGENT.md slice: separated Aura's own runtime guidance from repository development instructions. Added root `AGENT.md` for Aura's Telegram/runtime identity, workspace habits, skill usage, wiki memory, and small-cycle self-improvement rules. Prompt overlay now reads `SOUL.md`, `AGENT.md`, `USER.md`, and `TOOLS.md`, deliberately ignoring dev-only `AGENTS.md`; Docker now sets `PROMPT_OVERLAY_PATH=/app` so the mounted workspace note is visible to Aura, and settings best-default reconciliation repairs stale persisted `/data` overlay rows to `/app`. Verification: `go test ./internal/conversation ./internal/settings ./internal/telegram -count=1`, `verify-go.ps1`, `docker compose config --quiet`, `docker compose up -d --build aura`, live `/status` ok, and container probe confirmed `/app/AGENT.md` readable/writable with `PROMPT_OVERLAY_PATH=/app`.

2026-05-08 live latency repair slice: investigated a slow Telegram project-audit turn after the filesystem-first cleanup. Logs showed the tools were fast, but the agent spent 112s across 8 LLM calls, 46 file-tool calls, and 174k tokens because default routing let it repeatedly list/read project files instead of using swarm as the broad read-only pass. Tightened default loop policy to 4 steps, made `run_aurabot_swarm` a terminal default tool, prepended swarm in the default allowlist, wired the per-turn swarm hint for project/tool/guardrail self-audit prompts, added effective max-elapsed handling in the generic agent loop, and sharpened workspace/skill guidance so audits start with `search_files`/`list_files`, never `read_file` directories, and read only the few highest-signal files. Directory read errors now hint to use `list_files`. `/app/AGENTS.md` is present, readable, and writable inside the live container workspace; the stale AGENTS phase-plan pointer was updated to Phase 06. The Telegram sandbox harness now prefers `data/.env` when `AURA_ENV_PATH` is unset, so it copies `data/aura.db` and applies DB settings (`LLM_MODEL=deepseek/deepseek-v4-flash`) instead of accidentally smoking against root `.env` / `./aura.db`; custom prompts now validate non-sandbox routes instead of requiring the legacy execute_code/5050 smoke. Verification: `go test ./internal/agentloop ./internal/orchestration ./internal/conversation ./internal/tools ./internal/skills ./internal/telegram -count=1`, `go run ./cmd/debug_orchestration -prompt "Aura, leggi i file del progetto e dimmi quali strumenti hai adesso per migliorarti senza vecchi guardrail inutili"` reported `max_steps:4` and `terminal_tools=run_aurabot_swarm`.

2026-05-08 Docker container refresh slice: rebuilt and restarted the `aura` service after the filesystem-first cleanup. The first Compose refresh exposed a Windows line-ending regression in the bind-mounted secrets init script: Alpine `/bin/sh` read `set -eu\r` and `aura-secrets` exited before Aura could start. Added `.gitattributes` to force shell scripts to LF and normalized `docker/secrets/init-secrets.sh`; `aura-secrets` then completed, `aura-aura-1` started healthy on `127.0.0.1:18080`, and live `/status` returned ok with `integrity ok; journal=delete`.

2026-05-08 workspace post-write validation slice: saved the Git-tool follow-up decision in Phase 06 memory (small typed Git toolset, read-only first, review-gated stage/commit, no raw `git` execution). Added domain validation to bounded workspace file tools before `write_file` / `apply_patch` commits content: normal `wiki/*.md` pages now parse via `wiki.ParseMD`, pass `wiki.Validate`, and require filename slug to match title slug; `wiki/SCHEMA.md`, `wiki/index.md`, and `wiki/log.md` stay operational markdown and are skipped. Any `SKILL.md` write now parses via `skills.ParseSkill` and must match the parent directory name. Invalid patches are rejected before disk write, preserving the original file, while ordinary markdown outside wiki/skill paths remains writable. Verification: `go test ./internal/tools -run TestWorkspaceFileTools -count=1`, `go test ./internal/tools ./internal/telegram ./cmd/debug_tools ./cmd/debug_files ./cmd/debug_sandbox ./cmd/debug_memory_quality -count=1`.

2026-05-08 legacy wrapper deletion slice: physically removed the old LLM wrapper tool implementations for wiki read/write/search, wiki maintenance, wiki proposals, skill read/list/catalog, and skill proposals. Debug smokes now exercise bounded workspace file tools instead: `cmd/debug_tools`, `cmd/debug_ingest`, `cmd/debug_files`, `cmd/debug_sandbox`, `cmd/debug_swarm`, `cmd/debug_telegram_sandbox`, and `cmd/debug_memory_quality` all moved to `list_files`/`read_file`/`search_files`/`write_file`/`apply_patch` or direct domain loaders. Skills remain first-class: `internal/skills`, SKILL.md parsing/loading, multi-root discovery, admin install/delete, dashboard lifecycle, and prompt manifest were preserved; active skill telemetry now records `read_file` of `SKILL.md` paths rather than a deleted `read_skill` wrapper. Intentional legacy strings remain only for forbidden-tool drift detection and historical review-queue provenance compatibility. Verification: `go test ./cmd/debug_files ./cmd/debug_ingest ./cmd/debug_memory_quality ./cmd/debug_sandbox ./cmd/debug_swarm ./cmd/debug_telegram_sandbox ./cmd/debug_tools ./internal/agentloop ./internal/api ./internal/llm ./internal/scheduler ./internal/telegram ./internal/tools ./internal/toolsets ./internal/swarmtools`.

2026-05-08 agent simplification and workspace tools closure: completed the cleanup slice that removed the guardrail maze and reshaped Aura toward a Codex/Picobot-style agent loop. Required skill preflight is gone (`off|advisory` only), hidden/unavailable tools are recoverable tool results, swarm is a normal tool rather than a terminal profile cage, live profiles collapsed into four toolsets (`default`, `compute`, `document`, `admin`), the model/tool loop moved into `internal/agentloop`, and the Telegram conversation god file was split into archive/context/snapshot/terminal/tool helper files. Skill proposal approval now either applies a local `SKILL.md` through an admin-gated filesystem applier or marks the proposal `reviewed` instead of falsely claiming approval. Added disabled-by-default bounded workspace file tools (`list_files`, `read_file`, `search_files`, `write_file`, exact-replacement `apply_patch`) behind `AURA_WORKSPACE_TOOLS=enabled`, rooted at `AURA_WORKSPACE_ROOT` with containment and denies for `.env`, live DB/WAL/SHM, `.git/`, `wiki/raw/`, `data/secrets/`, `docker/secrets/`, and unsafe binary writes. Verification: targeted Go tests for workspace/tools/config/settings/API/orchestration/Telegram passed, `verify-go.ps1` passed, `go run ./cmd/debug_tools` passed, and `go run ./cmd/debug_orchestration -prompt "guarda i log e cerca nei file del progetto senza skill rituali"` showed default routing without required skill preflight. No React source changed, so web build was not rerun for this closure.

2026-05-08 agent toolset simplification slice: collapsed Aura's live orchestration profile taxonomy into four toolsets: `default`, `compute`, `document`, and `admin`. Legacy persisted/dashboard values (`memory`, `swarm_research`, `sandbox_compute`, `admin_review`) now normalize to the new toolsets instead of creating separate cages. Default tool use now includes memory/wiki/source/search/web/proposal/skill-read behavior, swarm is exposed as a normal tool when available instead of a terminal profile, compute is only the sandbox extension, document is only typed file generation, and admin is reserved for admin-gated mutation tools. Updated settings API, config/settings normalization, debug harness expectations, dashboard i18n labels, embedded `internal/api/dist`, and the active phase plan. Verification: targeted Go package tests for orchestration/Telegram/debug/settings/config/API passed, `npm --prefix web run i18n:check` passed, and `npm --prefix web run build` refreshed the embedded dashboard assets.

2026-05-08 Telegram conversation god-class split slice: moved archive turn persistence into `conversation_archive.go`, speculative wiki retrieval timeout/search helpers into `conversation_context.go`, orchestration snapshot storage/pruning into `conversation_snapshot.go`, terminal/tool-result formatting into `conversation_format.go`, terminal no-tool finalization into `conversation_terminal.go`, and tool execution plus small tool helpers into dedicated files. This was a behavior-preserving move; `conversation.go` dropped from 1008 to 479 lines, below the active plan target. The next high-value split is the actual model/tool loop into `internal/agentloop`. Verification: `go test ./internal/telegram`.

2026-05-08 agent loop extraction slice: added `internal/agentloop.Run` with chat/state/executor interfaces, duplicate tool-call recovery, max-iteration fallback, usage/cost stats, terminal-tool handoff, and focused loop tests. Telegram's `runToolCallingLoop` is now an adapter around the generic loop: streaming delivery, budget checks, orchestration snapshots, terminal finalization, and concrete tool execution remain Telegram-owned for this slice. Verification: `go test ./internal/agentloop ./internal/telegram`.

2026-05-08 skill proposal approval semantics slice: dashboard summary approval now distinguishes wiki and skill proposals. Wiki proposals still flip to `approved` and apply the wiki mutation. Skill proposals apply a local `SKILL.md` create/update/delete only when `SkillsAdmin` is enabled and the local proposal applier is wired; otherwise they flip to `reviewed`, not `approved`, with lifecycle copy that does not claim the skill is installed. Added a filesystem-backed proposal applier that validates `SKILL.md`, enforces skill-root containment, writes atomically for create/update, and refuses unsafe deletes. Verification: `go test ./internal/api ./internal/skills ./internal/tools ./internal/telegram ./internal/conversation/summarizer`.

2026-05-08 container-first verification correction: tightened the verification loop so code slices are rebuilt into `aura:local` before claiming live/runtime validation. While checking the rebuilt container, `aura --help` was found to start a second Aura process instead of exiting, which briefly degraded the derived `wiki_documents` FTS health. Stopped Aura, rebuilt the derived wiki index explicitly against `data/aura.db`, restarted Docker Aura, and confirmed `/status` returned ok with `integrity ok; journal=delete`. Added real CLI handling for `aura --help` and `aura --version` so inspection commands cannot launch a second server on the same SQLite DB. Verification: `go test ./cmd/aura ./internal/orchestration ./internal/telegram ./internal/swarm -count=1`, `docker compose up -d --build aura`, `docker compose exec aura sh -lc 'aura --help; ps -o pid,ppid,comm,args'` showed only PID 1 `aura`, and live `/status` stayed ok.

2026-05-08 conversation-log debug harness slice: used the live conversation log evidence and a single Telegram-style agent chat to fix the two observed debugging blockers. `cmd/debug_telegram_sandbox` now skips synthetic `e2e_bootstrap` allowlist rows when selecting its default recipient, prefers configured real allowlist users, and fails fast instead of sending production smoke messages to fake chat `1000001`. Swarm profile routing now treats Italian self-diagnostic prompts such as “stato del sistema”, “come posso migliorarti”, “dove ti blocchi”, and conversation-log debug requests as `swarm_research`, so the parent agent starts with `run_aurabot_swarm` rather than scattered direct reads. Verification: `go test ./cmd/debug_telegram_sandbox ./internal/orchestration -count=1`, `go test ./internal/telegram -run TestDebug -count=1`, and live DB-configured debug chat against `data/.env` used `user_id=1148481707`, `model=deepseek/deepseek-v4-flash`, `tool_profile=swarm_research`, `tools_called=run_aurabot_swarm`, `terminal_swarm=true`, `tokens_total=3993`, `cost_usd=0.006679`, `elapsed_ms=18762`, with no fake-user Telegram 400 spam.

2026-05-08 Docker SQLite journal safety investigation: traced repeated `database disk image is malformed` recoveries to the live Docker bind mount boundary rather than a single table migration. The host saw `data/aura.db-shm` as 3 bytes and `data/aura.db-wal` as 0 bytes while the running Linux container saw `/data/aura.db-shm` as 32768 bytes and `/data/aura.db-wal` as ~4.1 MiB, which matches SQLite WAL's shared-memory requirement colliding with Docker Desktop's Windows-host/Linux-VM bind mount layer and prior host-side repair/debug writes against a live container DB. Added `AURA_SQLITE_JOURNAL_MODE` with a safe allowlist, kept desktop default `WAL`, set Compose to `DELETE` for the bind-mounted container DB, and exposed journal mode in startup logs plus `/status` database health. Recovery replace now moves stale `-wal`/`-shm` sidecars into the corrupt backup path before installing the recovered DB. Backup export now archives a `VACUUM INTO` SQLite snapshot instead of raw-copying a live DB or WAL/SHM trio. Host-side DB-mutating debug commands now refuse to write `data/aura.db` while the Compose `aura` service is running, and Qdrant query/compare smoke no longer writes embedding cache rows. Verification: targeted DB/backup/debugguard/debug command tests, `go test ./... -count=1`, `docker compose config --quiet`, Docker rebuild of `aura`, live `/status` database detail `integrity ok; journal=delete`, and host/container file checks confirmed no live `aura.db-wal` or `aura.db-shm` sidecars.

2026-05-08 filesystem-first agent simplification slice: added Phase 06 plan and converted Aura's main agent contract away from semantic wiki/skill/proposal tools. Workspace file tools are now enabled by default (`AURA_WORKSPACE_TOOLS=enabled`, local root `.` and Compose root `/app`), the system prompt explains wiki and skills as bounded markdown files, and the Telegram runtime no longer registers `read_wiki`, `write_wiki`, wiki maintenance wrappers, skill read/list/catalog wrappers, or proposal wrappers for the main LLM surface. Orchestration profiles, capability maps, scheduler agent jobs, swarm role presets, and debug agent-job routing now prefer `list_files`, `read_file`, `search_files`, `write_file`, and `apply_patch`. Removed the obsolete wiki-proposal prompt module. Verification: `go test ./internal/conversation ./internal/skills ./internal/config ./internal/api ./internal/orchestration ./cmd/debug_orchestration`, `go test ./cmd/debug_agent_jobs ./internal/swarmtools ./internal/swarm ./internal/toolsets ./internal/scheduler ./internal/telegram ./internal/tools`, and `go test ./...`.

2026-05-08 Telegram tool-output masking slice: stopped exposing internal tool activity to end users during live Telegram turns. `executeToolCalls` no longer sends one Telegram message per tool call; tool usage remains in orchestration snapshots, logs, token metrics, and debug smoke output. Terminal swarm, sandbox, and file-generation fallbacks now produce compact user summaries instead of raw metrics, `source_id`, artifact MIME/delivery metadata, or JSON. Hidden-tool fatal messages now describe the capability boundary without leaking raw tool JSON or internal tool names such as `write_wiki`. `cmd/debug_telegram_sandbox` no longer applies the legacy hard-coded `5050` assertion to custom prompts, so real prompt-specific E2E checks can pass with explicit expectations. Verification: `go test ./internal/telegram -count=1`, `go test ./cmd/debug_telegram_sandbox ./internal/orchestration ./internal/tools -count=1`, `go test ./... -count=1`, Docker rebuild of `aura`, live `/status` ok, and live synthetic Telegram sandbox prompt `calcola 6*7...` passed in 4.79s with final `42`, token/cost metrics, and no artifact metadata leakage.

2026-05-08 live log repair and SQLite recovery slice: reviewed the running Docker logs after the relaxed orchestration change and fixed the remaining operational blockers instead of loosening the agent blindly. Nightly wiki maintenance now parses and repairs `broken related ref: slug` findings, `RepairLink` updates `related` frontmatter as well as body links, and memory hygiene creates a shared hub when the same missing target is referenced across categories. Scheduled auto-improve now accepts the dashboard enum values (`off`, `dry_run`, `auto`) and legacy `auto_apply`, while `allowed_users` excludes synthetic E2E bootstrap users so nightly jobs stop trying to notify chat `1000001`. Added `cmd/debug_db_recover` plus `internal/dbrecovery`, a repeatable SQLite rescue path that copies canonical tables through `NOT INDEXED` reads into a freshly migrated DB, skips derived FTS/shadow tables, verifies destination integrity, and can atomically replace the corrupt DB with a timestamped backup. Live recovery copied 511 canonical rows from the malformed `data/aura.db`, preserved the corrupt file as `data\aura.corrupt-20260508-052425.db`, rebuilt `wiki_documents`, and restored Docker health. Verification: targeted package tests, `go test ./... -count=1`, `docker compose config --quiet`, live `debug_memory_closure` ok with 20 wiki pages / 50 index docs, `docker compose up -d --build aura`, live `/status` ok with database integrity ok, Qdrant query smoke p50 14 ms, Pyodide sidecar health ok, and fresh Aura logs with no warnings/errors.

2026-05-07 generated local service secrets slice: removed committed Compose-owned secret literals from the default Docker stack. Added an `aura-secrets` one-shot service that generates and preserves Garage S3 credentials, Garage RPC/admin/metrics tokens, and the SearXNG `secret_key` under ignored `data/secrets/`; it renders `garage.toml`, `garage-default.env`, `aura.env`, and `searxng/settings.yml` before dependent services start. Compose now consumes those generated files, while Aura also supports `*_FILE` secret loading for provider-style secrets and rotates only known-public Garage demo rows out of the settings DB during best-default reconciliation. Deleted the old static Garage/SearXNG config files that contained placeholder secrets. Live Docker verification caught and fixed a stale file-mount directory case from the first attempt, so the init script now cleans generated config paths before rendering. Verification: `go test ./internal/config ./internal/settings ./internal/api ./cmd/aura -count=1`, `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`, `docker compose config --quiet`, rebuilt/recreated Aura/SearXNG/Garage on `AURA_HOST_PORT=18080`, live `/status` ok, all containers up, optional Garage UI profile healthy on `127.0.0.1:3909`, and `go run ./cmd/debug_backup -mode full -timeout 45s` uploaded a restore archive using `GARAGE_S3_*_FILE`.

2026-05-07 auto-low-risk memory capture slice: added `SUMMARIZER_MODE=auto_low_risk` as the new default between safe review and legacy direct auto-write. The new `AutoLowRiskApplier` writes only high-confidence non-sensitive wiki decisions directly, while low-score, sensitive, credential/PII, health/legal/finance/contact, or otherwise risky facts fall back to the existing `proposed_updates` review queue. Compose, `.env.example`, config normalization, settings best-default validation, and the dashboard settings enum now expose the mode. The prompt/orchestration guidance was tightened so ordinary current-turn memory is left to post-turn capture, OpenAI-compatible streaming waits for the final usage chunk so token/cost metrics are no longer dropped, and hidden `write_wiki` calls no longer leak raw `{"ok":false}` JSON to the user. Live E2E on `deepseek/deepseek-v4-flash` wrote a low-risk memory to `aura-operating-memory` with zero tool calls, token metrics present, and no pending marker proposal; E2E markers/noisy hidden-tool proposals were cleaned afterward. Verification: targeted `go test ./internal/conversation/summarizer ./internal/config ./internal/settings ./internal/api ./internal/telegram ./internal/llm ./internal/orchestration -count=1`, live `cmd/debug_telegram_sandbox` against `data/aura.db`, Docker rebuild on `AURA_HOST_PORT=18080`, live `/status` healthy with DB integrity ok.

2026-05-07 automatic best-default reconciliation slice: added a versioned startup migrator that repairs stale SQLite settings rows before they override Docker-first defaults. Old installs now self-heal from legacy direct-write memory capture (`SUMMARIZER_MODE=auto`, interval `5`, cooldown `59/60`) to review-gated capture, and container runs migrate local desktop paths/endpoints to mounted/service defaults such as `/data/.env`, `/data/aura.db`, `/wiki`, `/skills`, `WEB_SEARCH_PROVIDER=searxng`, `SEARCH_BACKEND=qdrant`, and `SANDBOX_RUNTIME_MODE=container`. The migrator records `AURA_BEST_DEFAULTS_VERSION`, does not touch provider/API secrets or model choices, and only runs stale-value migrations once so later explicit dashboard choices remain respected. Verification so far: `go test ./internal/settings ./cmd/aura -count=1`.

2026-05-07 Phase 5A automatic post-turn memory capture slice: turned the existing archive/summarizer/proposal machinery into an explicit review-gated memory-learning path. Defaults now enable capture in `review` mode with `SUMMARIZER_TURN_INTERVAL=2` and no cooldown, so a normal user/assistant turn can create a pending proposal without giving the live LLM direct wiki write power. Telegram archives synchronously when capture is active so the extractor sees the just-finished turn, review proposals preserve the originating `chat_id`, pending duplicates are suppressed, and the scorer prompt now includes numeric turn IDs for real archive provenance. Per-turn logs include memory capture triggered/decision/applied counts, and dashboard settings plus `.env.example`/Compose expose the new defaults. Verification so far: `go test ./internal/conversation/summarizer ./internal/telegram ./internal/config ./internal/settings ./internal/api -count=1`.

2026-05-07 v3.1 live loop enforcement slice: completed Phase 4 of the Codex-style orchestration plan. The Telegram loop now carries richer per-turn orchestration state: active profile, exposed/called tools, active capabilities, skill names read this turn, loop steps, terminal tool, hidden-tool rejection, and hard skill-preflight failures. Hidden tools are blocked by an explicit runtime guard before any registry execution, even if custom hooks do not reject them. Required capability tools now call the orchestration skill-preflight policy in the live loop and fail terminally until an applicable `read_skill` happened in an earlier turn; same-batch `read_skill` plus protected tool does not satisfy the ordering requirement. Profile `LoopPolicy` now caps live loop steps and terminal tools such as `execute_code` trigger a no-tool finalization path, while `run_aurabot_swarm` keeps the fast aggregate path. Verification: `go test ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`, `go test ./internal/orchestration ./internal/tools ./internal/skills -count=1`, and live Telegram-style swarm smoke on the DB-selected model passed in 21.6s with token/cost metrics and `terminal_tool=run_aurabot_swarm`.

2026-05-07 v3.1 profile cards and loop policy slice: completed Phase 3 of the Codex-style orchestration plan with subagent implementation and two review loops. Routing now iterates declarative `ProfileCard` entries by priority; route cues, negative cues, availability fallback, allowed tools, conditional availability tools, denied tools, and per-profile `LoopPolicy` live on the card catalog. `swarm_research` terminal tools begin with `run_aurabot_swarm`, `read_swarm_result`, and `list_swarm_tasks`, with a low step budget and duplicate-swarm rejection policy. `sandbox_compute` declares `execute_code` as the terminal compute tool and allows one final no-tool response. Tests cover English and Italian route prompts, card-declared tool exposure, default profile safety boundaries, copy-safe cards and policies, and prepend ordering. Verification: `go test ./internal/orchestration ./internal/telegram -count=1`, subagent spec review/fix/re-review, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-07 v3.1 skill preflight settings slice: exposed `AURA_SKILL_PREFLIGHT` as a real backend setting before Telegram enforcement. Config defaults to `required`, accepts `required|advisory|off`, normalizes case/whitespace, and degrades invalid values back to `required`. Settings overlay now persists/applies `AURA_SKILL_PREFLIGHT`, `/api/settings` exposes it under the `agent` group as an enum, and `.env.example` documents the knob. Review caught an architectural footgun where config/settings imported orchestration just for enum constants; fixed by keeping boundary normalization as local strings. Verification: `go test ./internal/config ./internal/settings ./internal/api -count=1`, subagent review approved, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-07 v3.1 skill preflight policy core slice: added pure orchestration policy for turn-scoped skill preflight without wiring Telegram enforcement yet. `internal/orchestration` now has `SkillPreflightMode` (`required`, `advisory`, `off`), `SkillRequirement`, `SkillPreflightState`, `SkillPreflightDecision`, `SkillRequirementForCapability`, and `NeedsSkillPreflight`. Required mode is hard-required for document generation, source extraction, sandbox compute, browser E2E, Docker runtime, security review, release git, MCP plugin review, and review-gated memory writes; memory reads and swarm research remain advisory. The policy is profile-aware for overlapping tools, so document/sandbox `read_source` and `list_sources` resolve to required source extraction even if a caller passes the primary memory capability. Verification: `go test ./internal/orchestration -count=1`, subagent spec/quality review with two blocker fixes, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-07 v3.1 Codex-style capability taxonomy slice: started the Codex-style skill orchestration plan with a Ralph-loop subagent implementation. Added `internal/orchestration` capability families for memory read/write review, source extraction, document generation, sandbox compute, swarm research, browser E2E, Docker runtime, security review, release git, and future MCP plugin review. The taxonomy maps capabilities to existing profiles, current tools, skill-preflight hints, and future-only markers without enforcing preflight yet. Added helpers for all capabilities, capability definitions, tool-to-capability lookup including overlapping tools, profile capability lookup, and copy-safe returned slices. Review found and fixed overlapping-tool and future-only test gaps before commit. Verification: `go test ./internal/orchestration -count=1` and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-07 Pyodide sidecar and MongoDB evaluation: moved Docker sandbox execution to a dedicated `pyodide` sidecar based on the Pyodide Docker environment image. Aura now supports `SANDBOX_RUNTIME_MODE=auto|container|local` and `SANDBOX_RUNTIME_URL`; Compose uses `SANDBOX_RUNTIME_MODE=container` with `http://pyodide:8787`, while the desktop local runner remains as fallback. The production Aura image no longer installs Node or copies the Pyodide bundle. The sidecar overlays only Aura's HTTP runner shim on top of `pyodide/pyodide-env`, and `execute_code` now loads Pyodide packages on demand from Python imports instead of loading the full office/data profile for every call, fixing the OOM seen in the first live Compose smoke. MongoDB was evaluated and explicitly deferred: SQLite remains canonical until repository-level metrics justify an optional adapter. Verification: `docker compose build --no-cache pyodide`, `AURA_HOST_PORT=18080 docker compose up -d --force-recreate pyodide aura`, live `/status`, live Compose `debug_sandbox -tool-smoke` through `http://pyodide:8787`, live artifact smoke with CSV/PNG persistence, `go test ./... -count=1`, `npm --prefix web run i18n:check`, `npm --prefix web run build`, and `docker compose config --quiet`.

2026-05-07 v1.3 memory consolidation closure: closed the memory-quality milestone under the strict Docker/live gate. Fixed `cmd/debug_memory_quality -live-llm` so it loads `.env`, opens the configured settings DB, and applies dashboard settings before selecting the live model; this makes the scorecard test the same DB-selected `LLM_BASE_URL`/`LLM_MODEL` as Aura instead of stale env-only values. Added regression coverage for DB overrides. Fixed the settings dashboard by rendering the `agent` group and localized orchestration rows, so `AURA_PROMPT_VERSION`, `AURA_TOOL_PROFILE_MODE`, and `AURA_ORCHESTRATION_LOG_LEVEL` are visible. Updated `.planning` to mark v1.3 closed and hand off to v3.1 orchestration. Verification: memory closure `issues=0` with 18 pages/45 docs, Qdrant compare `recommendation=ok`, hermetic quality 20/20, full live scorecard on `deepseek/deepseek-v4-flash` 20/20 with 0 slow scenarios over 30s, `go test ./cmd/debug_memory_quality ./internal/config ./internal/settings ./internal/api -count=1`, `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`, `go test ./... -count=1`, `npm --prefix web run i18n:check`, `npm --prefix web run build`, `docker compose config --quiet`, rebuilt Aura with `AURA_HOST_PORT=18080 docker compose up -d --build aura`, live `/status`, and settings Playwright E2E 6/6 from `web/`.

2026-05-07 search_memory tool timeout slice: bounded explicit `search_memory` tool calls separately from the pre-LLM speculative injection path. Added `MEMORY_SEARCH_TIMEOUT_MS` with a 5000 ms default across env config, settings overlay, dashboard settings API, `.env.example`, and localized settings labels. `SearchMemoryTool` now creates a bounded execution context, passes deadlines to wiki/Qdrant and archive searches, checks cancellation during source scans, and returns scoped timeout warnings plus the normal evidence envelope instead of stalling the whole agent loop. Telegram setup now wires the configured timeout into `search_memory` registration. Verification: red-green tool/config/settings/API tests, `go test ./internal/tools ./internal/config ./internal/settings ./internal/api ./internal/telegram -run "TestSearchMemoryToolWithTimeout|TestSearchMemoryToolAcceptsWiki|TestLoadQdrantConfig|TestApplyToConfigAppliesQdrant|TestSettingsList_ShowsQdrant" -count=1`, `npm --prefix web run i18n:check`, `npm --prefix web run build`, `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`, rebuilt Aura with `AURA_HOST_PORT=18080 docker compose up -d --build aura`, repaired the derived SQLite FTS mirror with `debug_memory_closure -apply` after live `/status` exposed local FTS corruption, confirmed live `/status` ok, confirmed live `/api/settings` shows `MEMORY_SEARCH_TIMEOUT_MS=5000`, and reran settings Playwright E2E 6/6.

2026-05-07 speculative memory timeout slice: bounded Telegram's pre-LLM speculative memory injection so a slow Qdrant sidecar or embedding call cannot stall a user turn behind the default 30s Qdrant HTTP client timeout. Added `SPECULATIVE_SEARCH_TIMEOUT_MS` with a 1500 ms default across env config, settings overlay, dashboard settings API, `.env.example`, and localized settings labels. `handleConversation` now routes the upfront search through a small timeout helper and logs timeout/elapsed metadata on failures while preserving the explicit `search_memory` tool for follow-up retrieval. Added unit coverage proving the speculative search path receives a deadline and falls back to the default timeout when unset. Verification: red-green focused tests, `go test ./internal/telegram ./internal/config ./internal/settings ./internal/api -run "TestSpeculativeSearch|TestLoadQdrantConfig|TestApplyToConfigAppliesQdrant|TestSettingsList_ShowsQdrant" -count=1`, `npm --prefix web run i18n:check`, `npm --prefix web run build`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-07 Qdrant smoke and settings combo slice: added a non-mutating `cmd/debug_qdrant` query smoke path with `-q`, `-compare`, `-top-k`, `-runs`, and `-warmup`. The smoke measures Qdrant search latency/counts against a local in-memory chromem index built from the wiki, reports p50/p95, result overlap, and a recommendation such as `ok`, `rebuild_qdrant`, `qdrant_empty`, or `review_quality` without mutating SQLite or Qdrant. Hardened the runtime Qdrant repository so an empty sidecar result falls back to local search instead of returning no memory evidence. Fixed the dashboard settings control for `SEARCH_BACKEND` by exposing it as an `enum` with `chromem`/`qdrant` options, which makes the existing settings UI render a combo box instead of a free text input; settings E2E now asserts that dropdown. Verification: red-green tests, `go test ./cmd/debug_qdrant ./internal/search ./internal/api -run "TestRunQuerySmoke|TestQueryRecommendation|Qdrant|TestSettingsList_ShowsQdrant" -count=1`, `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`, `npm --prefix web run i18n:check`, rebuilt Aura with `AURA_HOST_PORT=18080 docker compose up -d --build aura`, live `/status`, live `/api/settings` showing `SEARCH_BACKEND` as `enum`, Qdrant ready smoke, and `npm exec -- playwright test e2e/settings.spec.ts --reporter=line` from `web/`.

2026-05-07 Qdrant runtime query hardening slice: made the Qdrant sidecar useful as an opt-in read path without changing the default local search behavior. Added `SEARCH_BACKEND=chromem|qdrant` config/settings/dashboard support; `chromem` remains the default in `.env.example` and Compose. Added `NewQdrantSearcher` using Qdrant's `/collections/{collection}/points/query` API with Aura-owned embeddings and payload mapping into `search.Result`, plus `NewQdrantRepository` that queries Qdrant first and falls back to the existing chromem/SQLite repository on any Qdrant error. Telegram startup wraps the local search repository only when `SEARCH_BACKEND=qdrant`; writes and reindexing still flow to the local repository so Qdrant remains rebuildable sidecar state. Updated container docs and tests for query mapping, fallback behavior, and settings/config overlay. Verification: red-green targeted tests and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-07 LLM budget/runtime services boundary slice: promoted budget enforcement, usage recording, budget reporting, and live runtime limit updates into canonical service contracts. Added `budget.Gate`, `UsageRecorder`, `Reporter`, `Configurator`, and `Runtime`, with compile assertions that `Tracker` satisfies the full Telegram budget surface. Added `agent.LimitController` and `swarm.LimitController` so runtime settings can tune AuraBot loop limits and swarm concurrency/depth without depending on concrete managers. Telegram now stores the budget behind `budget.Runtime`, and `applyRuntimeSettings` accepts the narrower limit/configuration interfaces while startup still constructs the concrete tracker, runner, and manager. Added fake-backed runtime settings coverage proving the settings applier can run against boundary services. Verification: red-green targeted tests and `go test ./internal/budget ./internal/agent ./internal/swarm ./internal/telegram ./cmd/aura -run "Test.*Budget|TestRunnerUpdateLimits|TestManagerUpdateLimits|TestApplyRuntimeSettings|TestStatus|TestNew" -count=1`.

2026-05-07 sandbox/tool runtime boundary slice: promoted sandbox execution into `internal/sandbox` runtime contracts. Added `Executor`, `CodeValidator`, `AvailabilityReader`, and `ExecutionRuntime`, with compile assertions that `Manager` satisfies the execution, validation, and health roles. `execute_code` now depends on `sandbox.Executor` instead of concrete `*sandbox.Manager`, including a typed-nil guard so unavailable runtimes still keep the tool disabled; Telegram stores sandbox runtime behind `ExecutionRuntime` and persistent Python tool management behind the existing `tools.ToolStore` boundary. Added red-green fake executor coverage for `execute_code`. Verification: red-green targeted tests and `go test ./internal/sandbox ./internal/tools ./internal/telegram ./cmd/debug_sandbox ./cmd/debug_files ./cmd/aura -count=1`.

2026-05-07 MCP client boundary slice: promoted connected MCP clients into `internal/mcp` contracts. Added `Server`, `ToolCaller`, and `ConnectedClient`, with compile assertions that the concrete `Client` satisfies the runtime surfaces. Dashboard MCP listing/invoke now consumes `[]mcp.ConnectedClient`; `MCPTool` depends on `mcp.ToolCaller`; and Telegram shutdown/wiring stores connected clients through the interface while startup still constructs concrete stdio/HTTP clients. Added fake `ToolCaller` coverage for LLM-facing MCP tools and updated API MCP tests to use connected-client slices. Verification: red-green targeted tests and `go test ./internal/mcp ./internal/api ./internal/tools ./internal/telegram ./cmd/aura -count=1`.

2026-05-07 swarm store/manager boundary slice: promoted AuraBot swarm persistence and execution into `internal/swarm` contracts. Added run/task read/write repository interfaces plus `RunRunner` and `TaskResultReader`, with compile assertions that `Store` and `Manager` satisfy the relevant roles. The API now consumes `swarm.Reader` instead of an API-local duplicate interface; swarm tools accept `RunRunner`, `TaskLister`, and `TaskGetter`; and Telegram bot state keeps swarm store/manager behind those boundaries while startup still constructs the concrete SQLite-backed store and manager. Added red-green fake coverage for run/spawn tools and list/read task tools without concrete `*swarm.Manager` or `*swarm.Store`. Verification: red-green targeted tests and `go test ./internal/swarm ./internal/swarmtools ./internal/api ./internal/telegram ./cmd/debug_swarm ./cmd/aura -count=1`.

2026-05-07 wiki issues/maintenance boundary slice: promoted the `wiki_issues` queue into canonical `internal/scheduler` contracts. Added `IssueLister`, `IssueGetter`, `IssueEnqueuer`, `IssueResolver`, `IssueReader`, and `IssueRepository`, with compile assertions that SQLite `IssuesStore` satisfies the full surface. Dashboard maintenance handlers now depend on `IssueRepository`; `daily_briefing` consumes `IssueLister`; Telegram bot state holds the issue repository boundary; and nightly maintenance queues deferred wiki issues through `IssueEnqueuer` instead of concrete SQLite storage. Added red-green fake issue coverage for maintenance API list/resolve, daily briefing issue signals, and maintenance enqueue behavior. Verification: red-green targeted tests and `go test ./internal/scheduler ./internal/api ./internal/tools ./internal/telegram ./cmd/debug_ingest -count=1`.

2026-05-07 summaries/proposals boundary slice: promoted the review-gated `proposed_updates` queue into canonical `internal/conversation/summarizer` contracts. Added `ProposalCreator`, `ProposalLister`, `ProposalGetter`, `ProposalDecider`, `ProposalReviewRepository`, and `ProposalRepository`, with compile assertions that SQLite `SummariesStore` satisfies the full surface. Dashboard summaries now depend on `ProposalReviewRepository`; `daily_briefing` consumes `ProposalLister`; and `propose_wiki_change` / `propose_skill_change` consume `ProposalCreator`, keeping review-queue creation independent from SQLite while preserving dashboard approval as the mutation gate. Added red-green fake repository coverage for summaries API list/reject, daily briefing proposal signals, and both proposal tools. Verification: red-green targeted tests and `go test ./internal/conversation/summarizer ./internal/api ./internal/tools ./internal/telegram ./cmd/debug_ingest ./cmd/debug_memory_quality -count=1`.

2026-05-07 conversation/archive boundary slice: promoted conversation persistence into canonical `internal/conversation` contracts. Added `TurnAppender`, `ClosingTurnAppender`, `ChatTurnReader`, `TurnReader`, `TurnDetailReader`, `TurnIndexReader`, `ArchiveStatsReader`, `ArchiveDeleter`, and `ArchiveRepository`, with compile assertions that SQLite `ArchiveStore` and `BufferedAppender` satisfy the relevant surfaces. Dashboard conversation handlers now depend on `ArchiveRepository`; `search_memory` and `daily_briefing` consume `TurnReader`; Telegram turn persistence uses `ArchiveRepository` plus `ClosingTurnAppender`; and the summarizer runner aliases its archive input to `conversation.ChatTurnReader`. Added red-green fake archive coverage proving dashboard conversation review, memory retrieval, and daily briefing no longer require concrete SQLite storage. Remaining repository-boundary map after this slice: source/wiki/search/auth/settings/scheduler/conversation/memory-audit are done; next high-value slices are summaries/proposals, wiki issues/maintenance queue, swarm store/manager, MCP client registry, sandbox/tool artifact runtime, and LLM budget/runtime services. Verification: red-green targeted tests and `go test ./internal/conversation ./internal/api ./internal/tools ./internal/telegram ./internal/conversation/summarizer -count=1`.

2026-05-07 memory quality audit boundary slice: split the deterministic memory-closure audit from concrete wiki/filesystem/SQLite construction. Added `AuditDependencies`, `WikiRepository`, `WikiFileLister`, `IndexManifestReader`, `DirectoryWikiFiles`, and `SQLiteIndexManifestReader` so the core audit runs against role-shaped inputs while `Audit(Options)` remains the production constructor for `wiki.Store`, directory listing, and `wiki_documents` SQLite reads. Index manifest comparison now works over `IndexDocument` rows, with missing `wiki_documents` preserved as a high-severity closure issue. Added red-green fake dependency coverage proving the audit catches opaque source pages, legacy YAML, and unexpected raw index rows without a real wiki directory or DB. Verification: red-green targeted tests, `go test ./internal/memoryquality ./cmd/debug_memory_closure ./cmd/debug_memory_quality ./internal/search ./internal/wiki ./cmd/aura -count=1`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-07 search/index repository boundary slice: promoted semantic retrieval, wiki reindexing, document indexing, embedding functions, and embedding-cache health into canonical `internal/search` contracts. Added `Queryer`, `Searcher`, `WikiPageReindexer`, `WikiPageIndexer`, `DocumentIndexer`, `Repository`, `EmbeddingFunction`, and `EmbedCacheStatsReader`, with compile assertions that `Engine` and `EmbedCache` satisfy their surfaces. Wiki tools, memory search, ingest reindexing, Telegram speculative memory injection, summarizer dedup, and dashboard health now consume role-shaped search/cache interfaces instead of concrete `*search.Engine`/`*search.EmbedCache` outside lifecycle wiring. Concrete engines remain at startup/debug/test helper points that construct or seed the index. Added red-green fake search/index tests for wiki tools, memory search, and ingest reindexing. Verification: red-green targeted tests, `go test ./internal/search ./internal/tools ./internal/ingest ./internal/api ./internal/telegram ./internal/conversation/summarizer ./cmd/aura ./cmd/debug_ingest ./cmd/debug_tools ./cmd/debug_qdrant ./cmd/debug_memory_closure ./cmd/debug_memory_quality -count=1`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-07 wiki repository boundary slice: promoted wiki storage into canonical `internal/wiki` contracts. Added `PageCatalog`, `PageReader`, `SlugResolver`, `PageWriter`, `Directory`, `Linter`, `MemoryCleaner`, `LinkRepairer`, `Maintainer`, `Journal`, and `Repository`, with compile assertions that filesystem-backed `Store` satisfies them. Dashboard wiki deps now use `wiki.Repository`; wiki read/write/maintenance tools, tool registry, ingest pipeline, scheduler maintenance, and Telegram bot state depend on role-shaped wiki interfaces instead of concrete `*wiki.Store`. Concrete stores remain at wiki lifecycle/debug/test helper and memory-quality audit points that own filesystem/git behavior. Added red-green fake repository tests for wiki tools and maintenance tools. Verification: red-green targeted tests, `go test ./internal/wiki ./internal/tools ./internal/api ./internal/ingest ./internal/scheduler ./internal/telegram ./internal/conversation/summarizer ./cmd/aura ./cmd/debug_ingest ./cmd/debug_tools ./cmd/debug_agent_jobs ./cmd/debug_summarizer ./cmd/debug_memory_closure -count=1`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-07 source repository boundary slice: promoted source inbox persistence into canonical `internal/source` contracts. Added `Reader`, `Writer`, `FileResolver`, and `Repository`, with compile assertions that the filesystem-backed `Store` satisfies them. Dashboard uploads now depend on `source.Repository`; ingest, Telegram document handling, memory search, daily briefing, sandbox artifact persistence, generated DOCX/XLSX/PDF tools, and LLM source tools now consume source read/write/path interfaces instead of concrete `*source.Store`. Concrete stores remain only at lifecycle/debug/test helper points that own the raw directory or need direct validation. Added red-green tests proving source tools run against an in-memory repository fake. Verification: red-green targeted tests, `go test ./internal/source ./internal/tools ./internal/api ./internal/ingest ./internal/telegram ./cmd/aura ./cmd/debug_ingest ./cmd/debug_files ./cmd/debug_sandbox ./cmd/debug_docx ./cmd/debug_pdf ./cmd/debug_xlsx -count=1`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-07 scheduler repository boundary slice: promoted scheduler task contracts from an API-local interface into `internal/scheduler` so task persistence now has canonical repository boundaries. Added `TaskReader`, `TaskWriter`, `Repository`, `RuntimeRepository`, `ManualRunRecorder`, and `AgentJobRepository`, with compile assertions that SQLite `Store` satisfies them. `scheduler.New` now depends on `RuntimeRepository`; dashboard task deps use `scheduler.Repository`; LLM-facing schedule/list/cancel task tools and daily briefing depend on task read/write interfaces; Telegram bot state depends on the agent-job repository surface instead of concrete `*scheduler.Store`. Concrete scheduler stores remain only at SQLite lifecycle/DB-sharing points and test/debug helpers. Added red-green tests proving scheduler tools work against an in-memory repository fake. Verification: red-green targeted tests, `go test ./internal/scheduler ./internal/tools ./internal/api ./internal/telegram ./cmd/aura ./cmd/debug_ingest ./cmd/debug_agent_jobs ./cmd/debug_telegram_sandbox -count=1`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-07 auth repository boundary slice: made `internal/auth` the second repository-boundary model after settings. Added role-shaped interfaces for token lookup/issue/revoke, allowlist reads/writes, pending approval reads, dashboard API auth, and the full Telegram auth repository while keeping the existing SQLite `Store` implementation unchanged. `auth.RequireBearer`, dashboard API deps, the `request_dashboard_token` tool, and Telegram bot state now depend on interfaces instead of concrete `*auth.Store`; concrete construction remains only at startup/seed/test helpers. Added red-green tests proving `Store` satisfies the auth contracts, `/pending-users` plus `/auth/logout` run against an in-memory dashboard repository fake, and `request_dashboard_token` runs against a fake token writer. Verification: red-green targeted tests, `go test ./internal/auth ./internal/api ./internal/tools ./internal/telegram ./cmd/aura ./cmd/seed_e2e_env ./cmd/debug_telegram_sandbox -count=1`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-07 settings repository boundary slice: made `internal/settings` the first repository-boundary model before any ORM migration. Added narrow `settings.Reader`, `settings.Writer`, and `settings.Repository` interfaces while keeping the existing SQLite `Store` as the implementation. `settings.ApplyToConfig` and Telegram runtime setting refresh now depend on `Reader`; the setup wizard depends on `Writer`; the dashboard API and Telegram bot wiring depend on `Repository` instead of concrete `*settings.Store`. Added red-green tests proving `ApplyToConfig` works with an in-memory reader and `/settings` GET/POST works with an in-memory repository fake. Production scan now shows no concrete `*settings.Store` usage outside tests, so the next auth/settings-style repository can copy this shape. Verification: red-green targeted tests, `go test ./cmd/aura ./cmd/debug_telegram_sandbox ./internal/api ./internal/settings ./internal/setup ./internal/telegram -count=1`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

2026-05-06 DB repository boundary and SQLite health slice: added `internal/db.CheckIntegrity` and wired `cmd/aura` to run `PRAGMA integrity_check` after migrations and before constructing settings/Telegram stores, so a malformed canonical SQLite database fails startup loudly instead of producing confusing dashboard/auth symptoms. `/status` now includes a `database` component backed by the same integrity check. Added `internal/db` policy coverage that scans production Go files and fails if a new `sql.Open("sqlite", ...)` appears outside the shared DB package, keeping the repository boundary explicit while the broader ORM/repository refactor stays staged for a later slice. Cleaned stale high-impact references: `AGENTS.md` now says web search is controlled by `WEB_SEARCH_PROVIDER` and does not ride on `LLM_API_KEY`, and source tool comments no longer point future agents at the Ollama-only web file. Verification: targeted red-green tests for `CheckIntegrity`, startup order, and DB-open policy; then `go test ./cmd/aura ./internal/db ./internal/memoryquality -count=1`.

2026-05-06 SQLite/auth E2E stabilization: fixed a live dashboard auth failure where the local `data/aura.db` had malformed SQLite indexes, causing seeded API tokens to exist but fail lookup through the running container. The live database was recovered by copying canonical tables through `NOT INDEXED` reads into a freshly migrated database, leaving FTS/search mirrors rebuildable and the corrupt file as an ignored local backup. `internal/db` no longer enables SQLite memory-mapped I/O in the DSN, reducing Docker Desktop bind/WAL corruption risk, and a regression test locks that down. The memory-quality SQLite audit now uses the shared Aura DB opener instead of a one-off direct open. Settings Playwright coverage now uses accessible switch labels plus checkbox state instead of stale button-id/`aria-checked` assumptions. Search fallback comments/logs now consistently say SQLite FTS instead of the old pgvector wording. Verification: `go test ./internal/db ./internal/search -count=1` and `npx playwright test e2e/settings.spec.ts --reporter=line` passed against the live container on `127.0.0.1:18080`.

2026-05-06 Qdrant sidecar index slice: added Qdrant as the Docker vector-store sidecar while keeping SQLite FTS/chromem as the active runtime fallback. Compose now starts `qdrant/qdrant:latest` with local REST/gRPC ports and Docker-managed `qdrant-storage` volume data, and passes `QDRANT_URL=http://qdrant:6333`, `QDRANT_COLLECTION=aura_memory_v1`, and optional `QDRANT_API_KEY` into Aura. A live bind-mount trial produced Qdrant's Docker Desktop FUSE corruption warning, so Qdrant storage intentionally uses a named volume because the vector index is derived and rebuildable from `wiki/`. Config/settings/dashboard now expose and redact the Qdrant knobs. Added `internal/search` Qdrant rebuild support using the existing wiki document manifest, stable UUID point IDs derived from Aura doc IDs, Cosine collections sized from the real embedding vectors, and cached embeddings when available. Added `cmd/debug_qdrant` for non-mutating health checks and explicit collection rebuilds. Verification: red tests for config/settings/catalog/Qdrant rebuild/debug dotenv first, then `go test ./internal/config ./internal/settings ./internal/api ./internal/search ./cmd/debug_qdrant -count=1`, `docker compose config --quiet`, `npm run i18n:check`, `npm run build`, and `go test ./... -count=1` passed. Reference checked: Qdrant REST API supports UUID point IDs and `PUT /collections/{collection}/points` upsert; the user-suggested Go RAG sample reinforced sidecar startup plus collection initialization/ingestion as the right shape.

2026-05-06 automated wiki cleanup closure: promoted the memory closure gate from "detect and then fix by hand" to a deterministic cleanup pipeline. `clean_wiki_memory` / nightly maintenance now safely renames opaque ingested source pages such as `source-4-5942613039617418204` when a semantic heading can be derived from `wiki/raw/<source-id>/extract.md` or `ocr.md`; it rewrites body backlinks, related frontmatter, and the source's `source.json wiki_pages` atomically. The cleanup remains conservative: if no semantic heading or a collision is found, the audit still fails instead of inventing a title. `cmd/debug_memory_closure -apply` now runs the cleaner, rebuilds the SQLite `wiki_documents` mirror without requiring remote embeddings, and then audits the wiki/index manifest in one command. Live verification exposed a real SQLite FTS corruption (`database disk image is malformed: fts5`); the rebuild path now treats `wiki_documents` as disposable derived state, drops/recreates only that FTS table on corruption/schema drift, and reindexes from the canonical wiki files. Fixed go-git no-op commits so `clean working tree` is treated as success, and made no-op memory hygiene fully idempotent so it does not append `log.md` entries when nothing changed. Verification: red-green tests for opaque source auto-rename, closure `-apply` audit pass, SQLite mirror rebuild removing stale docs, FTS table repair, no-op log idempotence, and clean-worktree git commits; `go test ./...`; live `go run ./cmd/debug_memory_closure -wiki D:\Aura\wiki -db D:\Aura\data\aura.db -apply` passed with `renamed=0`, `docs=45`, `pages=18`, `issues=0`.

2026-05-06 memory closure gate: added `internal/memoryquality` and `cmd/debug_memory_closure` as a deterministic release gate for compiled memory and embeddings/search hygiene. The audit is read-only and checks wiki lint/broken graph state, legacy `.yaml` pages, raw OCR/source preview leakage in indexable wiki pages, opaque generated source slugs, and SQLite `wiki_documents` manifest drift against the expected wiki page + graph index documents. Live closure initially failed on the exact opaque page the user flagged, `source-4-5942613039617418204`; it was migrated to `source-test-pms-gestione-richieste-offerta`, backlinks in `davide`, `pms-project-management-saas`, and `fonti-ingestite` were updated, source metadata was aligned, `index.md` was regenerated through live wiki hygiene, and the Aura container was restarted to rebuild `wiki_documents`. Full test also caught an API contract regression from the wiki alias-defense slice: unknown wiki pages returned 500 because `ReadPage` no longer wrapped `os.ErrNotExist`; restored that wrapping so dashboard/API reads return 404 again. Final live gate passed: `wiki_pages=18`, `expected_index_docs=45`, `actual_index_docs=45`, `issues=0`. Verification: red tests first for raw-preview failure, unexpected FTS docs, and the API unknown-slug failure, then `go test ./...`, live `go run ./cmd/debug_memory_closure -wiki D:\Aura\wiki -db D:\Aura\data\aura.db`, rebuilt container image, container `/health`, and container logs showing `wiki pages indexed` with `count=45`, `pages=18`.

2026-05-06 wiki alias defense for swarm workers: fixed a memory-quality latency trap where AuraBot workers could infer short wiki slugs like `golem` or `goa-ai` from titles/search snippets, then `read_wiki` failed on the legacy `.yaml` fallback path even though canonical pages existed (`golem-agente-ai-personale-in-go`, `goa-ai-framework-design-first-per-agenti-agentic`). `wiki.Store` now normalizes read inputs and exposes `ResolveSlug`, which maps exact pages first and resolves short prefix aliases only when the match is unique; ambiguous aliases return candidate slugs instead of silently picking one. `read_wiki` uses that resolver and reports `Resolved alias [[x]] -> [[canonical]]` in tool output, or a candidate list if the alias is ambiguous. Verification: `go test ./internal/wiki ./internal/tools -run "TestResolveSlug|TestReadWikiTool|TestWriteAndReadWikiTools|TestWikiToolValidation|TestCleanMemory" -count=1`, `go test ./internal/wiki ./internal/tools -count=1`, and live dry-run `AURA_LIVE_WIKI_PATH=D:\Aura\wiki go test -tags live_wiki ./internal/wiki -run TestLiveCleanMemory -count=1 -v` reported `broken=0`.

2026-05-06 agent orchestration and prompt versioning: added Claude Code style runtime orchestration for the main Telegram loop. Aura now composes a versioned `aura-agent-v1` prompt with module/hash metadata, selects a focused tool profile (`default`, `memory`, `swarm_research`, `sandbox_compute`, `document`, `admin_review`), exposes only that profile's tools to the LLM, and logs prompt/profile/exposed tools/called tools/skill/swarm/sandbox/token/cost telemetry per turn. Swarm and Python sandbox are first-class routes: broad pipeline/memory audits prefer `swarm_research`, computed CSV/chart/parser/debug work prefers `sandbox_compute`, and document work uses skill preflight plus optional swarm evidence before typed file tools. Production skill roots now include `SKILLS_PATH/.claude/skills` and `SKILLS_PATH/.agents/skills` so catalog-installed skills are visible in the real bot, not only debug harnesses. The plan is persisted at `.planning/phases/04-agent-orchestration-system-prompt-versioning/PLAN.md`, and dashboard settings exposes `AURA_PROMPT_VERSION`, `AURA_TOOL_PROFILE_MODE`, and `AURA_ORCHESTRATION_LOG_LEVEL`. Verification: `go test ./cmd/debug_telegram_sandbox ./cmd/debug_files ./internal/conversation ./internal/telegram ./internal/tools ./internal/toolsets ./internal/settings ./internal/api ./internal/orchestration -count=1`; `docker compose --profile test run --rm test`.

2026-05-06 skill-backed DOCX agent smoke: added `go run ./cmd/debug_files -skill-docx -keep-wiki` to prove the agent reads installed skills before creating a Word document. The harness registers `list_skills`/`read_skill` alongside `create_docx`, includes catalog install roots under `skills/.claude/skills`, and fails unless the model calls `list_skills`, `read_skill`, and `create_docx` for the natural Aura document-summary prompt. Live run on `glm-5.1:cloud` passed with calls `list_skills, read_skill, read_skill, create_docx`, persisted `src_473300526763d767/original.docx`, and OOXML validation confirmed the document parts and Aura/Docker/MCP/README/container content markers. Verification: `go test ./cmd/debug_files ./internal/tools ./internal/skills -count=1`.

2026-05-06 Docker-only release follow-up: first live tag `v3.0.3` correctly triggered only the new Docker image workflow, but GHCR build failed because the repository checkout did not contain generated `runtime/pyodide` and the Dockerfile copies that bundle into the production image. The Docker workflow now uses Node 24 setup, builds `runtime/pyodide` before Buildx runs, and the release config test pins that ordering so future Docker-only releases carry the sandbox runtime.

2026-05-06 Docker-only release path: release tags now publish Aura only as a GHCR container image. Added `.github/workflows/docker-image.yml` using Docker Buildx, GHCR login with `GITHUB_TOKEN`, metadata-derived tags, linux/amd64 plus linux/arm64 output, cache, SBOM, and provenance. The old GoReleaser desktop binary workflow is now `workflow_dispatch` only so `v*` tags no longer ship OS archives by default. Added `compose.image.yaml` for end users to pull `ghcr.io/chetto1983/aura:<tag>` without building locally, and rewrote README around the logo, Docker-only install, update, development, and release flow. INSTALL/container docs now point users to the published image path. References checked: GitHub Docker image publishing and GHCR docs, Docker GitHub Actions docs, Docker build-push-action and metadata-action docs.

2026-05-06 backups dashboard surface: Garage backups are now visible and operable from the dashboard at `/backups`. Added authenticated API endpoints `GET /api/backups` and `POST /api/backups/export`, backed by the same Garage config used by `cmd/debug_backup`; secrets stay server-side. The React dashboard now has a sidebar Backups item, a latest artifact-set summary, historical grouped backup sets, byte counts, category pills, and a `Backup now` action that uploads a full Garage artifact set. Verification: `go test ./internal/backup ./cmd/debug_backup ./internal/api -count=1`, `npm --prefix web run i18n:check`, `npm --prefix web run lint`, `npm --prefix web run build`, `go test ./... -count=1`, `docker compose config --quiet`, rebuilt Aura on `AURA_HOST_PORT=18080`, live authenticated `GET /api/backups` returned `aura-artifacts` with 9 objects, live `POST /api/backups/export` uploaded set `2026-05-06-150956`, S3 listing confirmed 16 objects, and Playwright verified `/backups` in Italian with sidebar link, Backup ora button, latest set, source originals, and history visible.

2026-05-06 Garage artifact taxonomy: promoted Garage from a single restore tarball to a categorized artifact vault while keeping local files as primary storage. `cmd/debug_backup` now defaults to uploading a full artifact set: `backups/<timestamp>/aura-backup.tar.gz`, `artifacts/<timestamp>/source-originals.tar.gz`, `extractions.tar.gz`, `memory-snapshot.tar.gz`, `embedding-index.tar.gz`, `audit-bundle.tar.gz`, and `manifest.json`. Source originals include `source.json` plus `original.*`; extraction archives include OCR/extract markdown/JSON, cleaned markdown, and source assets; memory snapshots exclude `wiki/raw` so OCR bloat does not duplicate into compiled memory; embedding/index snapshots include SQLite DB/WAL/SHM plus `wiki/index.md`; audit bundles include logs and `reports/` when present. Verification: `go test ./internal/backup ./cmd/debug_backup -count=1`, live `go run ./cmd/debug_backup -timeout 3m`, and S3 listing against Garage confirmed the new `2026-05-06-145331` object set in `aura-artifacts`.

2026-05-06 task/source dashboard polish: restored the missing scheduler edit flow and expanded the source inbox into a usable audit surface. `/task` now redirects to `/tasks`; editable scheduler rows (`reminder`, `wiki_maintenance`, `agent_job`) expose an Edit dialog prefilled from the existing task, while internal `auto_improve` rows are intentionally not editable through the generic upsert API. Sources now have client-side search/status/kind filters, status counts, a Details action, wiki-page links, source metadata, and a bounded OCR/extract markdown preview via new `GET /api/sources/{id}/markdown` (`ocr.md` for PDFs, `extract.md` for extracted files). Verification: `npm --prefix web run lint`, `npm --prefix web run i18n:check`, `npm --prefix web run build`, `go test ./... -count=1`, `docker compose config --quiet`, rebuilt Aura on `AURA_HOST_PORT=18080`, live API task create/update/delete smoke, live source markdown smoke against `extract.md`, and Playwright fallback checks for `/task` redirect, task edit dialog, source filters, source details, and markdown preview.

2026-05-06 budget pricing unit fix: replaced the ambiguous `COST_PER_TOKEN` dashboard/runtime knob with separate real-world provider pricing fields, `COST_INPUT_PER_M_TOKENS` and `COST_OUTPUT_PER_M_TOKENS` (USD per 1M tokens). Root cause: the old single-token field invited entering provider prices in their advertised per-million unit; a live value of `2` was interpreted as `$2/token`, so the budget preflight predicted a huge spend and halted Telegram LLM calls. Budget tracking now records prompt/completion token costs separately, `/status` reports input/output prices, settings changes apply to the live budget tracker immediately, and legacy `COST_PER_TOKEN` is only used when it is a plausible USD/token value.

2026-05-06 skills install container fix: fixed `POST /api/skills/install` returning 502 in the container. Root cause: the production image installed `nodejs` for Pyodide but not `npm`, so the admin installer could not find `npx` (`exec: "npx": executable file not found in $PATH`). After adding `npm`, the next failure was npm trying to write under missing `/home/aura`; the container now sets `HOME=/data` and `NPM_CONFIG_CACHE=/data/.npm`, and the sanitized installer env preserves `NPM_CONFIG_CACHE`. The skills CLI also clones GitHub sources, so the image now includes `git`. Container installs use `SKILLS_INSTALL_PROJECT_DIR=/skills` so `npx skills add ... --agent claude-code` writes under the mounted `/skills` tree instead of ephemeral `/app`; loader/deleter roots now include both `/skills/.claude/skills` and `/skills/.agents/skills` to cover current and legacy catalog layouts.

2026-05-06 settings override cleanup: made the container settings page a true single override surface. Runtime and sandbox rows are no longer read-only; `TELEGRAM_TOKEN`, `HTTP_PORT`, path roots, `SANDBOX_*`, and other startup-facing keys are now in the settings allowlist and `settings.ApplyToConfig` overlay. `/api/settings` now backfills empty form values from the active runtime config so defaults and container env values are visible/editable immediately, while `active_value` still flags restart-required differences. The React settings page localizes per-setting labels/hints through i18n instead of showing backend English strings in the Italian UI. Live verification against `http://127.0.0.1:18080/settings` found 66 setting controls and 0 disabled setting controls.

2026-05-06 container sandbox enablement: fixed container mode still reporting sandbox off after Docker became the only supported install path. Root cause: Compose and Dockerfile still forced `SANDBOX_ENABLED=false`, `.dockerignore` excluded `runtime/pyodide/`, and the Alpine production image had no Node.js for the Pyodide runner. The production image now installs `nodejs`, copies the bundled Pyodide runtime into `/app/runtime/pyodide`, sets `SANDBOX_ENABLED=true`, and Compose passes `SANDBOX_RUNTIME_DIR=/app/runtime/pyodide`. Container docs now describe sandbox as enabled in the app image rather than test-only. Superseded on 2026-05-07 by the Pyodide sidecar migration above: the Aura image no longer installs Node or carries `/app/runtime/pyodide`.

2026-05-06 settings visibility fix: restored missing dashboard settings after the container-first migration. Root cause: `settingsCatalog` exposed only 42 day-to-day editable rows, while several real overridable keys (`LLM_MAX_RETRIES`, `OLLAMA_WEB_BASE_URL`, `SKILLS_CATALOG_URL`, advanced Mistral OCR options) were absent, and env-only container/runtime fields (`AURA_ENV_PATH`, `DB_PATH`, `WIKI_PATH`, `SKILLS_PATH`, `SANDBOX_*`, etc.) were not visible at all. Added read-only runtime and sandbox groups, made the backend catalog cover every `settings.OverridableKeys()` entry, added a `read_only` API field, disabled read-only controls in the React settings page, and added E2E coverage for the restored sections. Live verification against `http://127.0.0.1:18080/api/settings` now returns 66 settings across runtime/provider/search/storage/embeddings/ocr/sandbox/budget/summarizer/aurabot/other; settings E2E passes against the rebuilt container.

2026-05-06 container-only environmental test gate: resolved the two Docker-only failures that were not product-code regressions. Added a dedicated `test` Compose profile backed by `Dockerfile.test` (`node:22-bookworm` plus Go 1.26.2) so the canonical gate is `docker compose --profile test run --rm test`, not a raw `docker run golang...`. The test service grants `SYS_ADMIN` with unconfined seccomp because Linux no-network skill tests create a temporary network namespace; the same skill package was confirmed to pass in Docker with those capabilities. Pyodide availability now verifies a Node runtime before reporting available, and the sanitized Pyodide runner environment preserves `NODE_OPTIONS` for container memory tuning without exposing Aura secrets. Live source XLSX/DOCX Pyodide extraction is now opt-in via `AURA_SOURCE_PYODIDE_LIVE=1` because the local Docker engine reports only about 2 GB total memory and kills the Pyodide process; the default Docker gate remains deterministic while documenting the high-memory live command.

2026-05-06 container E2E debug closeout: fixed the missing first-run locale and completed live container E2E. The setup wizard now detects `Accept-Language`, renders English/Italian text server-side, localizes preset descriptions, and passes localized strings to its inline JS for probe/save statuses. Added tests for locale detection and localized presets. Also fixed two container startup bugs found during live E2E: headless setup wizard can bind `0.0.0.0:8080` inside Docker while desktop first-run stays loopback-only, and `cmd/aura` dotenv loading no longer overwrites explicit Compose/process env such as `HTTP_PORT=0.0.0.0:8080`. Added `seed_e2e_env -bootstrap-user` so empty container DBs can mint local dashboard E2E tokens without a manual Telegram `/start`. Live verification: setup page returned Italian (`Benvenuto`, `Modello linguistico`, `Salva e avvia Aura`) for `Accept-Language: it-IT` and English for `en-US`; Aura ran healthy on `AURA_HOST_PORT=18080`; authenticated `/api/health` returned 200; `/api/auth/whoami` resolved seeded user `1000001`; SearXNG live search returned results; Garage backup uploaded `backups/2026-05-06-114412/aura-backup.tar.gz`; optional Garage Web UI returned 200 on `127.0.0.1:3909`; full dashboard Playwright E2E against `http://127.0.0.1:18080` passed 54/54 tests.

2026-05-06 container E2E port fix: Aura dashboard host port is now configurable via `AURA_HOST_PORT` in Compose (`127.0.0.1:${AURA_HOST_PORT:-8080}:8080`). Root cause was a host conflict, not Aura startup: Windows `ApplicationWebServer` PID 13116 owns `0.0.0.0:8080`, so the prior smoke could not publish Aura's dashboard. Docs now show the alternate-port workflow (`$env:AURA_HOST_PORT="18080"` then `docker compose up -d --build`). Verification target: rerun live container smoke on 18080.

2026-05-06 container migration phase 6 smoke: deterministic release gates passed after the migration commits. `loops/aura-implementation/scripts/verify-go.ps1` passed, `docker compose config --quiet` passed, and `docker build -t aura:container-smoke .` passed. Live container smoke partially passed: Docker pulled SearXNG/Garage/WebUI images, SearXNG and Garage started, `go run ./cmd/debug_searxng -base-url http://127.0.0.1:8088 -q "aura search test" -json` returned 5 results in 886 ms, Garage created `aura-artifacts`, and `cmd/debug_backup` uploaded `backups/2026-05-06-112834/aura-backup.tar.gz` (348 files, bucket now reports 1 object / 1.1 MiB). During live smoke we fixed `cmd/debug_backup`/`internal/backup` to use a seekable temp archive because AWS SDK v2 cannot sign an unseekable plain-HTTP stream for Garage. Aura container creation succeeded but start was blocked by host port `127.0.0.1:8080`; Windows reports `ApplicationWebServer` PID 13116 listening on `0.0.0.0:8080`, so `/health` could not be checked in this session without changing the host port. Compose services left running: `aura-searxng-1` and `aura-garage-1`; `aura-aura-1` is created/stopped due the port conflict.

2026-05-06 container migration phase 5: Garage is now part of the container-first stack as a local artifact/backup layer, not primary storage. Compose adds `garage` (`dxflrs/garage:v2.3.0`) with persistent `./garage:/var/lib/garage`, local-only S3 API on `127.0.0.1:3900`, single-node/default-bucket startup, and an optional `garage-webui` (`khairul169/garage-webui:1.1.0`) behind the `garage-ui` profile on `127.0.0.1:3909`. Added `GARAGE_S3_ENDPOINT`, `GARAGE_S3_REGION`, `GARAGE_S3_BUCKET`, `GARAGE_S3_ACCESS_KEY`, and `GARAGE_S3_SECRET_KEY` to config/settings with dashboard secret redaction inherited from the settings API. Added `internal/backup` and `cmd/debug_backup`; manual export streams `.env`, `aura.db`, `wiki/`, and `skills/` into `backups/YYYY-MM-DD-HHMMSS/aura-backup.tar.gz` using S3 path-style upload, with unit tests for object naming/archive contents and secret redaction. Verification: red backup/config tests first, then `go test ./internal/config ./internal/settings ./internal/api ./internal/backup ./cmd/debug_backup -count=1`, `npm --prefix web run i18n:check`, `npm --prefix web run build`, and `docker compose config --quiet` passed. References checked during implementation: Garage Quick Start and khairul169/garage-webui Docker docs. Next phase: end-to-end container smoke.

2026-05-06 container migration phase 4: LLM provider setup now presents Aura as generic OpenAI-compatible instead of implying provider-specific code paths. Replaced the native Anthropic preset with an OpenRouter preset (`https://openrouter.ai/api/v1`, `deepseek/deepseek-chat` default), kept OpenAI/Mistral/Groq/DeepSeek/Together/Ollama/Custom presets on `/models`, and added tests to prevent reintroducing native Anthropic as an OpenAI-compatible endpoint. Settings gained a dedicated Web Search group in the dashboard/i18n/types so `WEB_SEARCH_PROVIDER` and `SEARXNG_BASE_URL` are surfaced separately from chat model keys. `.env.example`, README, INSTALL, setup copy, and embedded dashboard assets were updated. Verification: red preset tests first, then `go test ./internal/setup ./internal/api ./internal/settings -count=1`, `npm --prefix web run i18n:check`, and `npm --prefix web run build` passed. Next phase: Garage backup/export service.

2026-05-06 container migration phase 3: container `web_fetch` no longer depends on Ollama. Added `NewDirectWebFetchTool` with stable tool name `web_fetch`, http/https-only URL validation, 30s client timeout, five-redirect cap, 2 MiB response cap, 12k extracted text cap, HTML title/body/link extraction via `golang.org/x/net/html`, script/style suppression, absolute link resolution, and explicit truncation markers. `WEB_SEARCH_PROVIDER=searxng` now registers SearXNG `web_search` plus direct `web_fetch`; `ollama` mode still uses the existing hosted Ollama pair behind `OLLAMA_API_KEY`. `cmd/debug_tools -live-web` now follows `WEB_SEARCH_PROVIDER` and uses direct fetch for SearXNG smoke scenarios. Verification: red tests first for missing direct fetch constructor/safety constants, then `go test ./internal/tools ./internal/telegram ./cmd/debug_tools -count=1` passed. Next phase: generic OpenAI-compatible provider presets/settings copy.

2026-05-06 container migration phase 2: `web_search` now supports `WEB_SEARCH_PROVIDER=searxng|ollama|disabled` with `disabled` as the desktop-safe default and Compose setting `searxng`. Added `SEARXNG_BASE_URL` config/settings, a SearXNG JSON client that sends `format=json`, caps returned results, never sends an API key, and reports the common JSON-disabled 403 with an actionable hint. Telegram registration no longer reuses `LLM_API_KEY` for Ollama web search; `ollama` mode requires `OLLAMA_API_KEY`, while container mode uses SearXNG at `http://searxng:8080`. Verification: red tests first for missing config/tool surfaces, then `go test ./internal/config ./internal/tools ./internal/settings ./internal/api ./cmd/aura -run "TestLoadWebSearchProvider|TestLoadSuccess|TestSearXNG|TestSettings|TestMain" -count=1` passed. Next phase: direct bounded `web_fetch` so container mode has no Ollama web dependency.

2026-05-06 container migration phase 1: bootstrap config now supports `AURA_ENV_PATH` with default `.env`; `cmd/aura` loads that path before config, passes it to the first-run setup wizard, and reloads it after wizard save. Debug commands that source `.env` now respect `AURA_ENV_PATH` as well. Compose no longer relies on Docker-managed Aura volumes or root `.env`; Aura data is visible in host bind folders (`data/`, `wiki/`, `skills/`), and container startup uses `/data/.env` so first-run `TELEGRAM_TOKEN` persists across rebuild/recreate. Verification: red tests first for missing env path, then `go test ./internal/config ./cmd/aura ./cmd/debug_tools ./cmd/debug_memory_quality ./cmd/debug_telegram_sandbox ./cmd/debug_ingest ./cmd/debug_files ./cmd/debug_agent_jobs -count=1` and `docker compose config --quiet` passed. Next phase: SearXNG-backed `web_search`.

2026-05-06 container stack packaging: Aura now has an explicit server/container mode instead of relying on the tray stub. `AURA_HEADLESS=true` loads through config and makes `cmd/aura` start Aura directly, wait for SIGINT/SIGTERM, and skip `tray.Run`; desktop defaults remain tray-enabled. Added a Linux Dockerfile, `.dockerignore`, `compose.yaml`, SearXNG settings with JSON enabled, and `docs/container.md`; Compose runs Aura headless with persistent data/wiki/skills volumes and SearXNG at `http://searxng:8080` internally / `127.0.0.1:8088` on the host. Verification: red tests first for missing headless config/startup branch, then `go test ./internal/config ./cmd/aura ./cmd/debug_searxng -count=1`, `docker compose config --quiet`, `docker build -t aura:container-smoke .`, and `loops/aura-implementation/scripts/verify-go.ps1` passed. A later repeat Docker build could not run because Docker Desktop's Linux engine pipe disappeared; no code change was made from that failure. Live SearXNG probe still got connection refused on `127.0.0.1:8088`, so the stack has not been live-started in this session. Next slice: implement Aura's real SearXNG web-search provider and then run `docker compose up` smoke.

2026-05-06 SearXNG debug probe: added `cmd/debug_searxng` as a small pre-integration harness for the planned move away from Ollama web search. The command calls a configured SearXNG `/search` endpoint with `format=json`, records elapsed time, and prints either readable results or JSON for repeatable latency/result checks. Verification: `go test ./cmd/debug_searxng -count=1`, temp-path `go build ./cmd/debug_searxng`, `git diff --check`, and `go test ./...` passed. Live local probe against default `http://127.0.0.1:8088` failed with connection refused, confirming no local SearXNG instance is currently reachable before backend integration. Next slice: start/choose the SearXNG endpoint, run this probe against it, then wire Aura web search behind a provider abstraction with SearXNG first and paid fallback later.

2026-05-06 memory quality validation closeout: deterministic gates are green, but the live latency gate is not closed under the current model. Hermetic `go run ./cmd/debug_memory_quality -json -report-dir reports/memory-quality` passed 20/20 with wiki/source/archive evidence and review-gated proposals. Live wiki hygiene dry-run against `D:\Aura\wiki` passed with 17 pages, 0 broken links, 0 orphans, and no repairs needed. Full live `go run ./cmd/debug_memory_quality -live-llm -json -report-dir reports/memory-quality` used `search_memory` on 20/20 scenarios and proposal calls only where expected, but failed the 30s budget with 7 slow scenarios and 3 deadline partials on `glm-5.1:cloud`. The evaluator now exposes `propose_wiki_change` only for proposal scenarios, caps answer-only runs to one tool call, and trims tool result context, but a 5-scenario live sample still failed latency. Frontend closure also fixed the all-pages `/conversations` audit fixture so real archived route text cannot trip placeholder regex checks, and the confirm modal now keeps a follow-up destructive prompt populated after prompt submission.

2026-05-06 memory/embedding E2E and source bloat cleanup: source wiki pages are now compact anchors instead of OCR/extract previews. `internal/ingest` validates `ocr.md`/`extract.md` existence but no longer copies preview text into wiki page bodies, keeping raw evidence in `read_source` and out of duplicate wiki embeddings. `clean_wiki_memory` now strips legacy `## Preview` sections from existing source pages, and the live wiki hygiene test (`-tags=live_wiki`) applied that cleanup to `D:\Aura\wiki`; `source-4-5942613039617418204.md` dropped from 1829 bytes to 812 bytes and retains only the raw-source pointer. Memory/embedding checks run: `go test ./internal/search -run "IndexWikiPages|EmbedCache" -count=1`, `go test ./internal/tools -run SearchMemory -count=1`, hermetic `go run ./cmd/debug_memory_quality -json` passed 20/20, live `go run ./cmd/debug_ingest` passed all natural tool scenarios with real LLM + embeddings, and live wiki hygiene passed after applying cleanup. A later full live memory routing run showed correct tool selection but failed the latency budget on `glm-5.1:cloud`; see the validation closeout note above.

2026-05-06 memory hygiene audit kickoff: active planning now points to `v1.3 Memory Consolidation And Quality`. Removed stale generated v1.2 workflow artifacts from `docs/superpowers/` because `.planning/` is the active workflow truth and those files carried obsolete `AfterExtract`/DOCX assumptions. Added a code guard so operational wiki files such as `SCHEMA.md`, `index.md`, and `log.md` stay out of ordinary wiki page listing/search memory. Added `clean_wiki_memory` so the agent can reproduce the cleanup loop automatically: dry-run by default, apply mode creates shared hub pages, repairs obvious aliases and related refs, rebuilds `index.md`, and records an audit log entry. Nightly wiki maintenance now runs the same cleaner before its lint/defer pass, so graph repair can happen without a manual operator session. Live wiki audit found disconnected but potentially valuable source/trading/project pages; v1.3 owns broader memory quality checks and evidence-backed `search_memory` evaluation. Embedding wiring audit confirmed the production path uses dedicated `EMBEDDING_*` settings and wraps the embedding function with the SQLite `EmbedCache` before search indexing.

2026-05-06 v1.2 closure and polish: `v1.2 Source Intake Closure` is implemented and documented. The truthful supported upload set is PDF, TXT, Markdown, JSON, CSV, XLSX, and DOCX across source policy, API upload, Telegram document handling, dashboard copy, raw downloads, filters, and tests. XLSX extraction now uses the Pyodide sandbox path with mounted input files, ZIP preflight limits, bounded sheet parsing, and bounded retry behavior. DOCX extraction uses a fixed offline WordprocessingML extractor with ZIP/XML/text limits instead of a new runtime dependency; the user-suggested `docx-extractor-python` informed the stdlib approach, while Microsoft MarkItDown is recorded as a future generalized converter candidate. Extracted sources can ingest into wiki pages through the dashboard/API ingest action and existing ingest pipeline. Dashboard closure fixed empty graph edges to serialize as `[]`, hardened graph UI against legacy null responses, preserved conversation table row semantics with a real keyboard button, and passed the full dashboard E2E suite using a DB-seeded local token. Active closure docs now live under `.planning/`, with final gate details in `.planning/phases/02-v1-2-closure-polish/VALIDATION.md`.

2026-05-05 v1.2 Task 5 handoff: Dashboard and Telegram uploads now use the shared source format policy. API upload detects supported formats, stores detected kind/MIME, rejects unsupported files before storage, and normalizes Go-extractable non-PDF sources to `extract_complete` with `extract.md`/`extract.json`; PDFs keep the existing OCR path. Telegram document validation now uses `source.DetectUploadFormat`, threads the detected format into processing, stores non-PDF sources with detected kind/MIME, and normalizes text-like uploads before OCR. Raw downloads and API kind/status filters now include text/markdown/json/csv plus `extract_complete`. TDD checks: API text upload test first failed with "only PDF uploads are accepted"; Telegram validate tests first failed with missing `validateDocument`; both passed after implementation. Verification: `go test ./internal/api ./internal/telegram ./internal/source -count=1` and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1` passed. Deferred to Task 6: extracted non-PDF auto-ingest via `AfterExtract`; current non-PDF uploads stop at `extract_complete`.

2026-05-05 v1.2 Task 4 handoff: Pyodide XLSX extraction bridge landed. Added `ExtractWithPyodide` for `xlsx` sources using a fixed no-network script, base64 workbook input, pandas + calamine, manual markdown table rendering, row/sheet metadata, and truncation warnings. Added `TestPyodideXLSXExtractor`; initial RED failed with missing `ExtractWithPyodide`, then the real bundle run exposed the package-relative runtime path and the cold-start timeout. The test now points at `../../runtime/pyodide`, uses a 60s test timeout, and passed against the bundled runtime. Verification so far: `go test ./internal/source -run TestPyodideXLSXExtractor -count=1 -v` passed. Next work: Task 5 dashboard and Telegram upload use universal normalization.

2026-05-05 v1.2 Task 3 handoff: PDF OCR now writes normalized evidence. Added `ExtractFromOCRMarkdown`, `extract_pdf_test`, and wired both dashboard upload and Telegram document OCR paths to write `extract.md`/`extract.json` next to the existing `ocr.md`/`ocr.json`; source metadata now records the PDF adapter extraction metadata when OCR completes. TDD check: `go test ./internal/source -run TestExtractFromOCRMarkdown -count=1` failed first with missing adapter, then passed. Verification so far: `go test ./internal/source -run TestExtractFromOCRMarkdown -count=1` and `go test ./internal/source ./internal/api ./internal/telegram -count=1` passed. Next work: Task 4 Pyodide extractor bridge for XLSX/DOCX.

2026-05-05 v1.2 Task 2 handoff: Normalized extraction contract and Go extractors landed. Added `ExtractInput`, `ExtractResult`, `Extractor`, `extract.md`/`extract.json` constants, `WriteExtractionFiles`, and deterministic Go extractors for TXT/MD/JSON/CSV with markdown output and metadata. Added coverage for text/markdown/json/csv extraction, malformed JSON, and on-disk extraction file writes. TDD check: `go test ./internal/source -run "TestGoExtractors|TestExtractGo" -count=1` failed first with missing symbols, then passed after implementation. Verification so far: `go test ./internal/source -run "TestGoExtractors|TestExtractGo|TestWriteExtractionFiles" -count=1` and `go test ./internal/source ./internal/ingest ./internal/tools -count=1` passed. Next work: Task 3 PDF OCR adapter writes normalized evidence.

2026-05-05 v1.2 Task 1 handoff: Source format policy and metadata landed for universal ingestion. Added `DetectUploadFormat`, `SupportedUploadAccept`, new source kinds (`markdown`, `json`, `csv`), extraction statuses (`extracting`, `extract_complete`), extraction metadata, and raw filename mapping for the v1.2 upload set while preserving existing generated XLSX/DOCX/PDF and sandbox artifact behavior. TDD check: `go test ./internal/source -run "TestDetectUploadFormat|TestExtractionStatuses" -count=1` failed first with missing symbols, then passed after implementation. Verification: `go test ./internal/source -run "TestDetectUploadFormat|TestExtractionStatuses|TestStorePutKinds" -count=1`, `go test ./internal/source ./internal/api ./internal/tools ./internal/ingest -count=1`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1` passed. Note: existing ingest fallback intentionally permits whitespace-only filenames, so `validatePutInput` still rejects only the empty string. Next work: Task 2 normalized extraction contract and Go extractors.

2026-05-05 tray visibility fix handoff: Windows tray startup was working, but the primary embedded `icon.ico` rendered as noisy pixels while `icon_app.ico` rendered as the real Aura icon. The tray now uses `icon_app.ico` as primary, keeps `icon.ico` as fallback, and logs tray start/ready/stop lifecycle with icon byte counts for future diagnosis. Verification: `go test ./internal/tray -count=1`; live `go run ./cmd/aura` startup log showed `tray: ready` with `icon_bytes=276710` after previously using the bad `134635` byte icon.

2026-05-05 v1.1 Phase 1 handoff: `PANIC-01` landed. Production code no longer calls `MustResolveProfiles`; scheduled-agent allowed tools now initialize through `ResolveProfiles` without a bare panic path, and focused tests cover invalid profile behavior plus skills-read allowlist inclusion. Verification: local `rg` was unavailable/blocked with `Accesso negato`; fallback static search `Get-ChildItem internal,cmd -Recurse -File | Select-String -Pattern 'MustResolveProfiles'` returned no matches; `go test ./internal/toolsets ./internal/scheduler -count=1` passed. Next work: Phase 2 Production Error Observability.

2026-05-05 v1.1 Phase 2a handoff: `OBS-01` started. `Bot.Stop` now logs archiver and MCP client close failures instead of discarding them, while successful shutdown remains unchanged. Verification: `go test ./internal/telegram -run TestStopLogsArchiverCloseFailure -count=1` and `go test ./internal/telegram -count=1`. Next work: tray/browser-open observability.

2026-05-05 v1.1 Phase 2b handoff: `OBS-02` landed. Tray browser-open now validates dashboard URLs before Windows shell handoff, logs refused/failed opens, and non-Windows tray startup logs headless mode. Verification: `go test ./internal/tray -count=1`. Next work: Telegram cleanup observability.

2026-05-05 v1.1 Phase 2c handoff: `OBS-03` landed. Placeholder deletion failures during non-streamed response cleanup now log at debug level with user/message context without failing delivery. Verification: `go test ./internal/telegram -run "TestLogPlaceholderDeleteFailure|TestConsumeStream" -count=1` and `go test ./internal/telegram -count=1`. Next work: auth token audit observability.

2026-05-05 v1.1 Phase 2d handoff: `AUDIT-01` landed. Auth token lookup now surfaces `last_used` audit update failures through `AuditUpdateError`; middleware logs that warning and continues valid requests without leaking token material. Verification: `go test ./internal/auth -run "TestLookup.*Audit|TestIssueLookup_RoundTrip" -count=1` and `go test ./internal/auth ./internal/api -count=1`. Next work: packaged Windows console suppression and dependency monitoring.

2026-05-05 v1.1 Phase 3a handoff: `UX-01` landed. GoReleaser now builds packaged Windows `aura.exe` with the GUI subsystem while local developer builds remain console-friendly. Added PE subsystem inspection script and INSTALL troubleshooting notes. GoReleaser v2.15.4 uses `archives.ids` for archive build filtering, so the plan's two-build structure landed with that v2 syntax instead of deprecated `archives.builds`. Verification: `npm --prefix web ci` restored local Vite dependencies before packaging; `go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean` passed, packaged `aura.exe` printed `windows gui subsystem ok`, and a local dev build intentionally failed the GUI check as `windows console subsystem found`. Next work: telebot beta monitoring docs.

2026-05-05 v1.1 Phase 3a review-fix handoff: INSTALL release instructions now match GoReleaser archive outputs (`aura_<version>_<os>_<arch>.zip`/`.tar.gz`) and the extracted binary names (`aura.exe` on Windows, `aura` on macOS/Linux). Runtime commands, chmod guidance, systemd `ExecStart`, and update wording no longer reference obsolete per-OS binary names. Verification: `git diff --check`.

2026-05-05 v1.1 Phase 3b handoff: `DEP-01` landed. Telebot v4 beta usage is now intentionally tracked with pinned-version notes, upgrade smoke expectations, and rollback policy. Verification: `Select-String -Path go.mod,docs\telebot-v4-monitoring.md -Pattern 'gopkg.in/telebot.v4'`. Next work: v1.1 release gate.

2026-05-05 v1.1 closure handoff: `v1.1 Trustworthy Daily Use` is complete. Closed `PANIC-01`, `OBS-01`, `OBS-02`, `OBS-03`, `AUDIT-01`, `DEP-01`, `UX-01`, and `REL-02`; fixed stale concerns/README release docs discovered during review. Release gates passed: `go test ./internal/toolsets ./internal/scheduler ./internal/telegram ./internal/tray ./internal/auth ./internal/api -count=1`, `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`, `go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean`, and `scripts\check-windows-gui-subsystem.ps1` against the extracted Windows `aura.exe` printing `windows gui subsystem ok`. GoReleaser generated `go.mod`/embedded web asset noise was restored before committing. Next work: choose the next milestone.

2026-05-05 sandbox skill E2E handoff: proved Aura can read runtime skills, execute sandbox Python, persist the generated script/result, and recall the script through source tools. Added `debug_sandbox --skill-e2e`, which loads `skills/`, calls `list_skills`/`read_skill` for `aura-python-sandbox` and `aura-source-extraction`, runs real bundled Pyodide with `allow_network=false`, writes `/tmp/aura_out/aura_skill_e2e.py` and result markdown, persists both as `sandbox_artifact` sources, and reads them back with `read_source`. Root cause found/fixed: persisted text-like sandbox artifacts were stored but unreadable because `read_source` only looked for `ocr.md` or text/url originals. `read_source` now permits text-like sandbox artifacts (`text/*`, json/xml, `.py`, `.md`, `.csv`, etc.) while still rejecting binary artifacts. Verification: `go test ./internal/tools -run TestExecuteCodeTool_PersistedScriptArtifactIsReadableSource -count=1`, `go test ./cmd/debug_sandbox ./internal/tools ./internal/skills -count=1`, `go run ./cmd/debug_sandbox --skill-e2e --timeout 2m`, and actual app startup smoke (`go run ./cmd/aura` answered `/health` with `{"status":"alive"}` on configured port, then was stopped).

Historical note: the early standalone-second-brain/PDF-ingestion slices originally tracked against the now-removed `pdr.md` v4.0-next. Current product truth lives in `prd.md`, `.planning/`, and this tracker.

## Slice Order (from PDR §12)

1. **Config**: Mistral OCR keys, model, base URL, limits, feature flag.
2. **Source store** (`internal/source`): source ID, raw file storage, `source.json` read/write, listing.
3. **OCR client** (`internal/ocr`): Mistral `/v1/ocr` client + fake-server tests.
4. **Telegram PDF handler** (`internal/telegram/documents.go`): MIME/size validation, download, store, OCR trigger.
5. **Source tools**: `store_source`, `ocr_source`, `read_source`, `list_sources`, `lint_sources`.
6. **Ingestion** (`internal/ingest`): `ingest_source` pipeline, source summary page, affected-page reindex.
7. **Wiki maintenance**: `append_log`, `rebuild_index`, `list_wiki`, `lint_wiki`.
8. **Reminder/scheduler tools**: SQLite `scheduled_tasks`, `schedule_task`, `list_tasks`, `cancel_task`.
9. **Natural prompt tests**: extend `cmd/debug_tools` or add `cmd/debug_ingest`.
10. **UI**: source inbox, PDF status, wiki graph and health dashboard.

Slices 1–7 must land before any UI work. Slice 8 (reminders) is independent and can land in parallel after slice 1.

## Current State (2026-04-29)

Working tree before this session:

- Embedding config moved to Mistral defaults (`EMBEDDING_BASE_URL=https://api.mistral.ai/v1`, `EMBEDDING_MODEL=mistral-embed`) — `internal/config/config.go`, `internal/config/config_test.go`, `.env.example` modified, not yet committed.
- `cmd/debug_tools/main.go` added (untracked) — natural prompt smoke harness for `write_wiki` / `read_wiki` / `search_wiki` and optional live web tools via `--live-web`.
- New product docs at the time: Picobot/tool audit, second-brain consolidation strategy, and `pdr.md`. These were later removed from active docs; use git history for the original artifacts and `prd.md`/`.planning/` for current scope.
- Branch: `ralph/US-010-observability`.

Existing packages: `budget`, `config`, `conversation`, `health`, `llm`, `logging`, `orchestrator`, `search`, `skill`, `telegram`, `tools`, `tracing`, `wiki`. No `source`, `ocr`, `ingest` yet.

## Memory Philosophy Guardrail

Status note (2026-05-04): Aura memory stays aligned with `docs/llm-wiki.md`.

- Raw sources are immutable evidence. Aura stores PDFs, OCR, URLs, files, and archive turns as source material; it does not treat raw chunks as the final knowledge base.
- The wiki is the compiled, compounding memory artifact. Durable facts, decisions, summaries, contradictions, links, and synthesis belong in markdown wiki pages with frontmatter and `[[slug]]` links.
- Search is an access path, not a second memory layer. `search_memory`, embeddings, archive lookup, and source search help Aura find evidence, but they should feed wiki updates/proposals instead of replacing the wiki.
- Autonomous learning is review-gated. Agent jobs, swarm runs, summarizers, and future watchers should propose wiki changes by default, not silently mutate durable memory.
- Skills are procedural memory, not factual memory. Repeated workflows can become reviewable `SKILL.md` proposals; facts and project knowledge still belong in the wiki/source model.
- The conversation archive is evidence and short-term recall. Stable facts extracted from chat should be promoted into the wiki through explicit saves or reviewed proposals.
- Future memory slices must preserve this stack: source evidence -> compiled wiki -> search/evidence envelope -> reviewed updates -> optional procedural skills.

## Current Handoff (2026-05-05)

Last completed milestone: `v1.1 Trustworthy Daily Use`.

Active phase: none. Current focus: choose the next milestone.

2026-05-05 v1.1 closure handoff: `v1.1 Trustworthy Daily Use` is complete. Release gates passed: focused package tests, full Go verifier, GoReleaser snapshot package, and Windows GUI subsystem inspection. Next work: choose the next milestone.

2026-05-05 v1.1 kickoff handoff: `v1.1 Trustworthy Daily Use` is the active milestone. Scope is hardening-only: remove `MustResolveProfiles` production panic paths, surface shutdown/tray/Telegram cleanup/token-audit failures, suppress the packaged Windows console through GoReleaser only, document telebot v4 beta monitoring, and run a focused release gate. Next work: Phase 1 Panic Removal Gate.

2026-05-04 Task 3.1 handoff: `internal/db` shared SQLite open path is done. Touched `internal/db/db.go`, `internal/db/db_test.go`, and this tracker; ran the targeted DB tests plus requested package slice verification. Next slice: store constructor injection into the shared pool.

2026-05-04 Task 3.2 handoff: store/search injection compatibility bridge done. Auth, scheduler, settings, swarm, embed cache, and SQLite FTS wrappers now route through `internal/db.Open`; injected constructors preserve caller-owned shared pools. Next slice: production startup wiring.

2026-05-04 Task 3.3 handoff: Phase 1 DB Foundation automated guard passed after production startup wiring. `cmd/aura` opens one shared SQLite pool via `internal/db.Open`, settings/Telegram/auth/scheduler/search/swarm use injected pool-backed constructors, and static scans confirm no legacy production SQLite open path outside `internal/db/db.go`. Next work: Phase 2 Migration Safety planning/execution. Residual release-gate check: manual lifecycle smoke for clean shutdown.

2026-05-05 Phase 2 handoff: Migration Safety merged to `master` via PR #1. Aura now has `internal/db/migrations` with `schema_migrations`, transactional versioned application, fresh schema creation, v3.0.2 upgrade fixture coverage, fresh/upgraded convergence tests, FTS transaction/behavior proofs, and legacy `conversations` repair centralized in migrations. `cmd/aura` and `cmd/debug_telegram_sandbox` run migrations immediately after `internal/db.Open` and before shared store construction. Shared-pool constructors no longer lazily create production schema; owned compatibility openers run migrations themselves. Verification before PR: `go test ./...`, `go build ./cmd/aura`, `go build ./cmd/debug_telegram_sandbox`, startup-order static checks, and lazy-schema static checks. Next work: Phase 3 Memory Reliability. Residual release-gate check: manual lifecycle smoke for clean shutdown.

2026-05-05 Phase 3 handoff: Memory Reliability implemented. Historical planning artifact created during the slice was later removed from active docs. Extracted `archiveConversationTurns` in `internal/telegram/conversation.go`, changed the bot archiver field to a small package-local interface in `internal/telegram/bot.go`, tightened `internal/conversation/archive.go` buffered-drain/drop logs, and added focused coverage in `internal/telegram/archive_test.go` plus `internal/conversation/buffered_test.go`. Direct and buffered archive append failures now log at error level with `chat_id`, `turn_index`, `role`, and `error` where available; success-path tests cover user, assistant tool-call, tool-result, final assistant telemetry, and failure observability. Verification: `go test ./internal/telegram -run TestArchiveConversationTurns -count=1`, `go test ./internal/conversation -run TestBufferedAppender_DrainGenericError -count=1`, `go test ./internal/telegram ./internal/conversation -count=1`, `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`. Next work: Phase 4 Dashboard Security.

2026-05-05 Phase 4a handoff: Dashboard Token Expiry implemented. Historical planning artifact created during the slice was later removed from active docs. `internal/db/migrations` now adds/backfills `api_tokens.expires_at` as migration v3; `internal/auth.Store` issues expiring tokens with a default 30-day TTL, returns distinct `auth.ErrExpired`, and middleware emits `{"error":"token_expired"}` on expired bearer tokens; `internal/config` loads `DASHBOARD_TOKEN_TTL_HOURS` default `720`; `internal/telegram/setup.go` applies that TTL to the production auth store; `.env.example` documents the knob. Verification: `go test ./internal/auth ./internal/config ./internal/api ./internal/db/migrations ./internal/telegram ./cmd/seed_e2e_env -count=1`. Next work in Phase 4: settings secret redaction (`SEC-02`).

2026-05-05 Phase 4b handoff: Settings Secret Redaction implemented and Phase 4 Dashboard Security closed. `internal/api/settings.go` now redacts secret setting `Value` and `ActiveValue` at the GET `/settings` response boundary after computing restart state, so raw LLM, embedding, Mistral, and Ollama keys never leave the API on reads. `internal/api/settings_test.go` proves DB and env-backed secrets return an empty edit value plus `(configured)` active placeholder while preserving metadata. `web/src/components/SettingsPanel.tsx` treats `(configured)` as a placeholder only, displays it as a secret input placeholder, and filters it out of save payloads so the dashboard does not resubmit markers as raw values. Embedded dashboard assets in `internal/api/dist` were rebuilt. Verification: red tests first (`go test ./internal/api -run "TestSettingsList_(HappyPath|RedactsEnvSecrets)" -count=1`), then `go test ./internal/api ./internal/settings -count=1`, `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-web.ps1`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`. Next work: Phase 5 Telegram Regression Harness plan and focused critical-path tests.

2026-05-05 Phase 5a handoff: Telegram Regression Harness planned and first streaming delivery regression landed. Historical planning artifact created during the slice was later removed from active docs; `TEST-01` scope is preserved in `.planning/REQUIREMENTS.md`. Added `internal/telegram/streaming_test.go` with a local fake Telegram API that proves `consumeStream` edits the placeholder and suppresses duplicate sends for streamed text, while tool-call-only responses are not marked delivered and do not send Telegram messages before tool execution. Verification: `go test ./internal/telegram -run TestConsumeStream -count=1`, `go test ./internal/telegram -count=1`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`. `TEST-01` remains open; next Phase 5 slice is text access-control handler tests in `internal/telegram/bot_test.go`.

2026-05-05 Phase 5b handoff: Text access-control regression tests landed. `internal/telegram/bot_test.go` now proves unauthorized text from non-allowlisted users returns nil without active/chat context state, and allowlisted text starts the async conversation path against a local fake Telegram API through nil-LLM echo mode. Verification: `go test ./internal/telegram -run TestOnMessage -count=1`, `go test ./internal/telegram -count=1`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`. `TEST-01` remains open; next Phase 5 slice is document/OCR trigger regression tests in `internal/telegram/documents_test.go`.

2026-05-05 Phase 5c handoff: Document/OCR trigger regression tests landed. `internal/telegram/documents_test.go` now proves unauthorized document uploads return nil before work registration or source writes, and allowlisted PDF uploads use the real async handler path plus a local fake Telegram API to download and persist a `stored` PDF source when OCR is disabled. The fake Telegram API in `internal/telegram/streaming_test.go` now also supports `getFile` and `/file/bot...` downloads for shared Telegram handler tests. Verification: `go test ./internal/telegram -run "TestDocHandler.*Document" -count=1`, `go test ./internal/telegram -count=1`, and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`. `TEST-01` remains open; next Phase 5 slice is final harness closure: focused package verification, existing archive proof decision, requirement/state updates, and Phase 6 release-gate handoff.

2026-05-05 Phase 5 closure handoff: Telegram Regression Harness closed and `TEST-01` marked done. Existing archive tests from Phase 3 remain the archive proof; Phase 5 added focused streaming, text access-control, and document/OCR trigger regressions around that existing memory coverage. Verification: `go test ./internal/telegram -count=1` and `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`. Next work: Phase 6 Release Gate plan plus automated Go/web/sandbox/package checks and manual Windows smoke.

2026-05-05 Phase 6 automated gate handoff: Release Gate planning artifact was created during the slice and later removed from active docs; automated gates passed through snapshot packaging. Verification: `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`, `go test ./internal/db ./internal/db/migrations -count=1`, `npm --prefix web ci`, `npm --prefix web run lint`, `npm --prefix web run build`, `node --check runtime/pyodide/runner/aura-pyodide-runner.mjs`, `go test ./internal/sandbox ./internal/toolsets ./internal/scheduler ./internal/telegram -count=1`, `go run ./cmd/debug_sandbox --smoke`, `go run ./cmd/debug_sandbox --tool-smoke`, `go run ./cmd/debug_sandbox --artifact-smoke`, `go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean`, and Windows ZIP content inspection for `aura_3.0.3-snapshot_windows_x86_64.zip`. Packaging blocker found and fixed: the GoReleaser `goversioninfo` hook now passes `-64`, because the previous resource object failed the Windows amd64 build under the Go 1.26.2 toolchain selected by GoReleaser v2.15.4. Manual Windows production smoke followed from the snapshot ZIP as the final `REL-01` gate.

2026-05-05 v1.0 closure handoff: Manual Windows production smoke passed from `dist/aura_3.0.3-snapshot_windows_x86_64.zip` per operator confirmation. `REL-01` is done and v1.0 Production Readiness is closed. Release gates passed: automated Go/web/sandbox checks, migration fresh/upgrade/idempotence checks, release candidate package check, Windows ZIP content inspection, and manual Windows smoke. Next work: v1.1 Hardening Polish planning or the v1.0 tag/release.

The old `v1.0 Close Concern` wording is superseded by `.planning/REQUIREMENTS.md` and the v1 production readiness plan.

Docs cleanup status:

- `docs/plans/` and `docs/superpowers/` are no longer active planning surfaces. Historical plans/specs were removed in docs cleanup; use git history for old phase artifacts and use `.planning/` for current phase work.
- `docs/implementation-tracker.md` remains the long-form shipped-history ledger.
- `docs/llm-wiki.md` remains the durable product memory-pattern reference.
- Phase 18 is closed.
- Phase 19 is closed for the pre-GSD/productization track. Its remaining "real-user routine drill" and "legacy/debt closure" notes were superseded by the `v1.0 Production Readiness` milestone, which now owns hardening and cleanup through explicit requirements.
- Sandbox Pyodide work is production-closed and released as `v3.0.2`.

Phase 18 status: **closed**.

Closure criteria met:

- Live LLM scorecard passed 20/20 real daily-memory questions.
- Every live scorecard question used `search_memory`.
- Durable-memory scenarios created review-gated proposals through `propose_wiki_change`.
- Search-backed proposals are rejected unless they carry evidence refs.
- Evidence drill-down, batch review, provenance, memory decay, and report graph artifacts are shipped.
- The memory stack still follows `docs/llm-wiki.md`: source evidence -> compiled wiki -> search/evidence envelope -> reviewed updates -> optional procedural skills.

Phase 19 status:

- Closed as the pre-GSD productization track after code inventory, review-gated skill proposals, graph-aware semantic index, named toolsets, skill/context-backed scheduled jobs, wake gates, scheduled-job E2E harnesses, and the skill proposal lifecycle decision shipped.
- The remaining real-user routine drill and broad debt cleanup notes are not active `docs/` phases. They are superseded by the `v1.0 Production Readiness` milestone in `.planning/`.
- Future work should start from `.planning/STATE.md` and the active phase directory, not from deleted historical docs under `docs/plans/` or `docs/superpowers/`.

Sandbox closure note (2026-05-04):

- `sandbox.pyodide.close` is complete: computed artifacts persist as sources, deliver over Telegram, and appear in the dashboard as downloadable file sources.
- Production checks passed: targeted Go tests, `verify-web.ps1`, `verify-go.ps1`, `go run ./cmd/debug_sandbox --tool-smoke --timeout 2m`, `go run ./cmd/debug_sandbox --artifact-smoke --timeout 2m`, `go run ./cmd/debug_sandbox --smoke --timeout 2m`, and `go run ./cmd/debug_telegram_sandbox --artifact-smoke --timeout 4m`.
- Local `.env` was updated to `SANDBOX_TIMEOUT_SEC=120`; `.env.example` carries the same tracked default.

Sandbox smoke upgrade (2026-05-04):

- The artifact smoke no longer proves only a hello-world text file. It now requires a richer computed workflow: pandas builds a sales summary CSV and matplotlib generates a PNG chart, both under `/tmp/aura_out`.
- `cmd/debug_sandbox --artifact-smoke` and `cmd/debug_telegram_sandbox --artifact-smoke` now fail unless both `aura_sales_summary.csv` and `aura_sales_plot.png` are returned, persisted as sources, and, in the Telegram path, delivered as documents.
- Live Telegram verification passed with one `execute_code` call, two sent documents, and two persisted source IDs.

Workspace warning:

- Leave favicon/packaging/dashboard-dist churn untouched unless explicitly taking that slice:
  `.goreleaser.yml`, `Makefile`, `cmd/build_icon/main.go`, `web/index.html`, `web/public/*`, `cmd/aura/versioninfo.json`, and `internal/api/dist/*`.
- Leave `.claude/settings.local.json` untouched.
- Before the next code slice, run `git status --short -uall` and stage explicit paths only.

## Slice Status

| # | Slice | Status | Notes |
| - | ----- | ------ | ----- |
| 1 | Config (Mistral OCR) | done | Mistral OCR fields + defaults + tests. |
| 2 | Source store | done | `internal/source` with sha256 dedup, atomic source.json, per-id mutex, kind/status filter. |
| 3 | OCR client | done | `internal/ocr` Mistral client with wire-verified table_format/extract_header/extract_footer; render to PDR §4 ocr.md. |
| 4 | Telegram PDF handler | done | `internal/telegram/documents.go` non-blocking single-message progress, bounded concurrency=2, AfterOCR hook for slice 6. |
| 5 | Source tools | done | `internal/tools/source.go` — store_source, read_source, list_sources, lint_sources, ocr_source. Wired in bot.go. 13 unit tests. |
| 6 | Ingestion | done | `internal/ingest` pipeline + `ingest_source` tool. Auto-ingest wired via `docHandler.AfterOCR`; emits source summary page with [[wiki-link]] note in final Telegram progress message. 10 test funcs (15 cases) + `live_ingest` catch-up test. Live-tested end-to-end via Telegram + catch-up on three sources. |
| 7 | Wiki maintenance | done | `list_wiki`, `lint_wiki`, `rebuild_index`, `append_log` LLM tools wrapping the existing `wiki.Store` primitives. Exported `RebuildIndex`/`AppendLog`. 15 unit tests. |
| 8 | Reminder/scheduler | done | SQLite-backed scheduler with at/daily kinds, reminder + wiki_maintenance task kinds, bootstrapped 03:00 nightly job. Tools: schedule_task, list_tasks, cancel_task. Autonomous goroutine + 4 autonomy tests. |
| 9 | Natural prompt tests for OCR/ingest | done | `cmd/debug_ingest` — 10 LLM-driven scenarios covering source/ingest/wiki-maintenance/scheduler tools. Hermetic temp wiki + temp SQLite. All passing live. |
| 10 | UI | done | All sub-slices shipped (10a + 10b + 10c + 10d + 10e). |
| 10a | UI: read-only HTTP API | done | `internal/api` package. JSON GET endpoints for health rollup, wiki pages/page/graph, source list/detail/ocr/raw, tasks list/detail. Mounted at `/api/` on the existing health server via `healthServer.Mount` + `http.StripPrefix`. 14 unit tests; race clean. |
| 10b | UI: frontend scaffold + wiki/graph views | done | React 19 + Vite SPA in `web/`, copied from sacchi reference and pruned. 5 routes via react-router-dom v7 (HealthDashboard, WikiPanel, WikiPageView, WikiGraphView lazy, SourceInbox, TasksPanel). Built into `internal/api/dist/` and embedded via `//go:embed all:dist`. Listener defaults to `127.0.0.1:8080`. Tray gains "Open Dashboard". QR landing deleted. |
| 10c.1 | UI: browser PDF upload (mini-slice from 10c) | done | `POST /api/sources/upload` runs the same pipeline as Telegram (store → OCR → auto-ingest), gated by new `requireLoopback` middleware. Drop-zone + click-to-pick on `/sources` with sonner per-file toasts. `.env` flipped to `HTTP_PORT=127.0.0.1:8081` so the LAN listener path is also closed. Live-tested with `6MBU00242200.pdf` (224 KB, 1 page) — full pipeline ~1.4 s end-to-end. |
| 10c | UI: write actions (ingest/reocr/cancel/upsert/rebuild/log) | done | 6 loopback-gated POST endpoints + matching dashboard actions. Backend: `internal/api/sources_write.go`, `wiki_write.go`, `tasks_write.go`. Frontend: ingest + re-OCR per-row buttons on `/sources` (Re-OCR shown for stored/failed, Ingest shown for ocr_complete/failed); Cancel button + "+ New task" dialog (one-time `at` or daily HH:MM, reminder/wiki_maintenance, recipient_id field shown only for reminder kind) on `/tasks`; "Rebuild index" button on `/wiki`. 21 new Go tests covering happy paths + every input-validation branch + the loopback gate negative case. SPA rebuilt into `internal/api/dist/`. |
| 10d | UI: bearer auth + Telegram-issued tokens | done | New `internal/auth` package (api_tokens table on the scheduler SQLite file; SHA-256 hashed storage; `Issue`/`Lookup`/`Revoke` + `RequireBearer` middleware). Every `/api/*` route now requires `Authorization: Bearer <token>` — there is no public login endpoint. Tokens are minted via the new `request_dashboard_token` LLM tool, which delivers them out-of-band via Telegram so plaintext never lands in conversation logs. The 10c `requireLoopback` gate retired since auth supersedes it. New endpoints: `GET /api/auth/whoami`, `POST /api/auth/logout`. Frontend: `/login` route (paste-token form), localStorage `aura_token`, Authorization header on every request, 401 → redirect with `?expired=1` hint, Sign-out button in sidebar. 7 router auth tests + 12 store/middleware tests + 5 tool tests. Telegram allowlist remains canonical (re-checked on every authed request). |
| 11a | MCP client + boot wiring | done | New `internal/mcp` package (Picobot-port: stdio + Streamable-HTTP transports, JSON-RPC 2.0, `initialize` → `tools/list` → `tools/call`). New `internal/tools/MCPTool` adapter so MCP tools register as `mcp_<server>_<tool>` in the same registry the LLM sees. Config: `MCP_SERVERS_PATH=./mcp.json` (gitignored runtime, `mcp.example.json` tracked). Bot boot loads servers, warns on connection failures, never fatal; `Bot.Stop()` closes all clients. 5 client tests (HTTP/SSE/error) + 6 config-loader tests + 5 tool-wrapper tests (15 total, race-clean). |
| 11b | Skills + MCP dashboard panels | done | Backend: `GET /api/skills` (list), `GET /api/skills/{name}` (full SKILL.md content with 16k truncation guard), `GET /api/mcp/servers` (per-server transport, tool count, full schema). New `Deps.Skills *skills.Loader` and `Deps.MCP []*mcp.Client` plumbed from the bot. `mcp.Client.Transport()` getter added (returns "stdio" or "http"). Frontend: `/skills` and `/mcp` routes, both bearer-authed; expandable cards with live SKILL.md previews and per-tool input-schema toggles; sidebar gains Sparkles + Plug nav; `g k` / `g m` keyboard chord shortcuts. 12 new Go tests (skills happy/empty/nil-loader/404/bad-name/truncation + mcp empty/populated/nil-client). |
| 11c | skills.sh install + delete (admin gated) | done | New `SKILLS_ADMIN=false` config flag (opt-in). Backend: `GET /api/skills/catalog?q=&limit=` (passthrough to skills.sh via the existing `CatalogClient`), `POST /api/skills/install` (admin-gated; runs `npx skills add <source> [--skill <id>]` from `SKILLS_PATH` with a sanitized env that drops `TELEGRAM_TOKEN`/`MISTRAL_API_KEY` and a 90s timeout), `POST /api/skills/{name}/delete` (admin-gated; refuses traversal + symlinks via `filepath.Rel` containment). New `internal/skills/admin.go`: `NPXInstaller` + `FSDeleter` plus a `IsSkillNotFound` helper so the api package can map filesystem-not-found to a 404. Frontend: `SkillsPanel` rewritten with Local/Catalog tabs; debounced search box; per-row Install button (sonner progress toast → success/failure with truncated `npx` output); per-row Delete with confirm; auto-detected admin-gated banner appears the first time a 403 is seen. 19 new tests (12 install/delete API + 4 catalog + 4 FSDeleter unit including symlink refusal + sanitized env). |
| 11j | Surface embed cache stats on /api/health | done | `EmbedCache.Stats()` (already wired in 11h) is now plumbed into `Deps.EmbedCache` and the health rollup. New `EmbedCacheHealth{hits, misses}` block on `GET /api/health`. Frontend: dashboard gains a fourth status card showing `<hits>` as the headline number with subtitle = computed hit-rate percentage (or "no embeds yet" before the first call). Stays at 0/0 when no cache is wired (no `EMBEDDING_API_KEY` or `DB_PATH`). Lets you watch the cache fill from the dashboard while testing the speedups from 11h + 11i. |
| 11i | Concurrent wiki indexing | done | `IndexWikiPages` previously called `coll.AddDocument` serially in a per-page loop — 8 pages × ~1 s per Mistral round trip = ~8 s cold start. Switched to chromem-go's already-supported `coll.AddDocuments(ctx, docs, indexConcurrency)` which spawns parallel goroutines. New `indexConcurrency = 4` constant: ~4× faster cold start, well under Mistral free-tier rate limits. Atomic-failure fallback path serializes if the batch fails so one bad page doesn't lose the whole index. SQLite FTS mirror stays serial (cheap local writes; concurrent FTS inserts contend). Stacks on 11h: warm starts still hit the cache and pay nothing. |
| 11h | SHA-keyed embedding cache | done | Wraps `chromem.EmbeddingFunc` with a SQLite-backed cache (`embedding_cache` table, composite key `(content_sha, model)`). Cold start unchanged; warm starts hit the cache and skip the Mistral round trip entirely for unchanged wiki pages — 30 wiki pages × ~1 s per embed = ~30 s saved per restart. Same path serves query embeddings, so repeat questions skip the round trip too. Robustness: corrupt blob detection (length-not-multiple-of-4 → re-embed + delete row), upstream-error propagation, model-key isolation (changing `EMBEDDING_MODEL` invalidates entries automatically), nil-upstream errors cleanly on miss. Kept chromem-go in place vs. swapping to `sqlite-vector` because the latter would force CGO + native extension loading; this fix gets ~99% of the win with 150 LOC. **Bundled cleanups**: deleted dead `sqliteSearcher.indexWikiDir` method (and the now-unused `os` + `filepath` imports), removed unused `newTestEngine` helper, added missing `Content` assertion in `TestResultStruct`. 8 cache tests + 1 strengthened test. Race-clean. |
| 11g | Pin install cwd to project root | done | Bug found by user: `marketing-psychology` install landed at `D:\Aura\skills\.claude\skills\` (nested) instead of `D:\Aura\.claude\skills\`, so the loader missed it. Cause: `NPXInstaller.Install()` used `cmd.Dir = cfg.SkillsPath`; the skills.sh CLI uses cwd as its project-detection anchor and writes to `<cwd>/.claude/skills/`. Fix: `NewNPXInstaller(skillsDir, projectDir)` now takes a separate project-root parameter; bot passes `""` which falls back to `os.Getwd()` (Aura's cwd at startup = project root). Existing nested install was relocated by hand. |
| 11f | Progressive-disclosure skill prompt | done | Picobot and earlier Aura both dumped every skill's full body into the system prompt every turn — at 28 KiB for `claude-api` × N skills, that's 100+ KiB injected into small-talk turns where no skill applies. Anthropic's skill format was designed for progressive disclosure: descriptions are the routing signal (with TRIGGER/SKIP rules embedded), bodies live on disk and load on demand. `auraskills.PromptBlock` now emits a tight manifest (`- **name** — description`) plus a directive telling the LLM to call `read_skill(name)` before acting on a matched skill's instructions. The body only enters the conversation context on turns that actually need it, and stays cached for the rest of the tool loop. New caps: descriptions truncate at 1500 chars (`maxManifestDescChars`); total manifest at 8 KiB (`maxSkillsBlockChars`, down from 12 KiB). 3 new tests (manifest format / leak-check on body / per-description truncation / 50-skill bounded total). |
| 11e | Make catalog installs visible to the loader | done | Two-bug fix discovered when the user installed `claude-api`: (1) the dashboard install was hanging on `npx skills add`'s "Which agents do you want to install to?" prompt until the 90 s ceiling fired (the Anthropics' skills CLI is interactive even with `--yes` to npx); (2) when it does work, skills.sh writes to `<project>/.claude/skills/<name>/SKILL.md`, but Aura's loader only scanned `./skills/`. Fixes: `NPXInstaller.Install()` now passes `--agent claude-code -y` and closes stdin so the install is fully non-interactive; `Loader` and `FSDeleter` both became multi-root, scanning `SKILLS_PATH` first and `.claude/skills` second. Variadic signatures keep existing tests passing without a churn-y rewrite. 4 new tests cover the multi-root paths (load merge, primary-wins-on-duplicate, delete from secondary, multi-root not-found). Verified live by reading the in-place `.claude/skills/claude-api/` install we'd done manually during diagnosis. |
| 11d | Invoke MCP tools from dashboard | done | `POST /api/mcp/{server}/tools/{tool}` — bearer-authed (no extra admin gate; the operator already trusts everything in `mcp.json` because the LLM can call those servers). 60 s context timeout, 64 KiB body cap, 64 KiB output cap. Validates `server` against the loaded MCP-client list and `tool` against the server's advertised tools (404 on unknown). Body: arbitrary JSON object → forwarded as `arguments`; empty body / `null` → `{}`. Tool errors (`isError:true`) come back as `200 {ok:false, is_error:true, error}` so the UI can render them inline; transport / timeout failures arrive as `200 {ok:false, is_error:false, error}`. Frontend: each tool row in `MCPPanel` gains a Run button revealing a JSON textarea (seeded from `input_schema.properties` when available), Invoke action with sonner progress toast, color-coded result panel (success/tool-error/transport). 8 new Go tests (happy path with arg-passthrough verification, empty body, 5 bad-body variants, unknown server, unknown tool, bad tool name, server tool error, transport error, large output truncation). |
| 11k | History cap (Picobot pattern) | done | Active conversation was unboundedly sticky and re-enforced its token budget on every tool iteration — both made the agent slow (extra summarizer LLM calls mid-response) and dumb (lossy summarization overwriting recent reasoning). Adopt Picobot strategy: cap in-flight messages at `MAX_HISTORY_MESSAGES` (default 50) with a tool-safe trim boundary. Wiki/sources tools carry durable memory so chat history can evict. Summarization fallback only for pathologically large single messages. Inner-loop `EnforceLimit` removed from `runToolCallingLoop` since `MaxToolIterations` already bounds per-turn growth. |
| 11l | Parallel tool calls within a turn | done | Model frequently emits multiple independent tool calls (e.g. `search_wiki + web_search + read_wiki`); running serially burned N round-trips of latency for no reason. Each call already uses its own ctx and the registry is RWMutex-guarded. New `executeToolCalls`: emit all activity pings up front, fan out one goroutine per call, join, then append results in original order. Deterministic message ordering preserved. |
| 11m | Cache skills loader 1s | done | `handleConversation` called `skillLoader.LoadAll()` on every Telegram message to render the manifest — walked `SKILLS_PATH` + `.claude/skills`, opened and YAML-parsed every SKILL.md per turn. Memoize `LoadAll` for `cacheTTL=1s`: short enough that admin install/delete reflects on the next user turn, long enough that back-to-back chat turns hit the cache. `Invalidate()` exposed for callers wanting immediate consistency. |
| 11n | Latency benchmarks | done | Quantified slice 11k/l/m wins: `BenchmarkLoaderLoadAllCached` 339 ns/op vs `Uncached` 3.69 ms/op (slice 11m hot path), `BenchmarkRegistryExecuteSequential` 41 ms vs `Parallel` 10 ms (slice 11l). `writeFile`/`writeSkill` helpers narrowed to `testing.TB` so `*testing.B` can call them. New `internal/skills/loader_bench_test.go`, `internal/tools/registry_bench_test.go`. |
| 11o | Gate /start behind frontend approval queue | done | Closes the TOFU bootstrap window: once an owner exists, unknown /start no longer auto-rejects — queues into `pending_users`, pings every allowlisted user via Telegram, waits for explicit approve/deny from the dashboard. Approval mints a fresh token shipped over Telegram so plaintext never round-trips through the dashboard. New `internal/api/pending.go` + `internal/auth/store.go`. Dashboard `/pending` panel polled every 8s. Spam /start preserves `requested_at` while pending — no pingstorm. TOFU bootstrap intentionally kept for first-owner onboarding on a virgin install. |
| 11p | Speculative wiki retrieval | done | Pre-11p the model only saw durable wiki memory after explicitly emitting `search_wiki` — full extra LLM round-trip per turn. Picobot's `agent/context.go` injects ranked memories into the system prompt before the first inference; we now do the same. `handleConversation` runs `search.Search(userText, 5)` right after `AddUserMessage` and pipes the results through `convCtx.SetSearchContext`. Embedding cache (slice 11h) makes repeat queries free; cold queries pay one embed call but save the round-trip. `search_wiki` tool stays available for refinement. |
| 11q | Bootstrap prompt overlay files | done | Picobot pattern from `internal/agent/context.go`: read a fixed set of optional MD files from a configured dir on every conversation turn and append to the system prompt. Operator tunes personality (`SOUL.md`), Aura runtime notes (`AGENT.md`), durable user facts (`USER.md`), tool guidance (`TOOLS.md`) by editing files — the next user turn picks the change up with no recompile or restart. `PROMPT_OVERLAY_PATH` defaults to `.` locally and `/app` in Docker. All 4 files optional; missing/blank skipped silently. 4 file reads per turn negligible vs the LLM round-trip. |
| 11r | Per-turn latency telemetry | done | Slice 11n's benchmarks proved the smart-and-fast wins in microbenchmarks (skills cache 10000x, parallel tools 4x). This adds the runtime counterpart: every conversation turn now logs `elapsed_ms`, `llm_calls`, `tool_calls` so real Telegram latency is measurable without sprinkling per-subsystem timers. `runToolCallingLoop` returns `turnStats{llmCalls, toolCalls}` alongside the response. `handleConversation` captures `turnStart` at the top and emits the structured "conversation complete" line on the way out. |
| 11s | Stream tool-call deltas through llm.Token | done | Tool-call streaming was the missing piece for slice 11t. `Stream()` returned only text deltas; if the model emitted tool calls during a streamed response we silently dropped them, making streaming unusable for any tool-calling turn. `Token` now carries an optional `ToolCalls` slice populated on the final `Done=true` token. The SSE reader accumulates per-index `function.arguments` fragments internally so consumers never see partial JSON. `Stream()` also forwards `Request.Tools` — previously streaming requests omitted the tools array entirely, so the model had no way to call a tool from a streamed call. `OllamaClient.Stream` forwards to `OpenAIClient` and inherits the new behavior. New `TestOpenAIClientStreamWithToolCalls` exercises the multi-fragment accumulation path. |
| 11t | Progressive Telegram edit while streaming | done | Final-response latency was the last big perceived-latency lever — slice 11l/m/p cut server-side wall clock, but the user still saw nothing until the full assistant message landed. Now the bot opens a placeholder message once 30 chars of streamed text accumulate (avoids displaying discardable prefaces) and edits it every 800ms (Telegram's safe rate limit per chat) until the stream completes. The tool loop swaps `Send` for `Stream`. `consumeStream` rebuilds an equivalent `llm.Response` from the token stream, so all downstream code (token tracking, budget tracking, tool execution) is unchanged. When the model emits tool calls, the streamed text becomes the assistant's "Let me search…" preface; tool execution proceeds as before. When text-only, the progressively-edited message *is* the final delivery — `runToolCallingLoop` returns `""` so `handleConversation` skips its `c.Send` to avoid double-posting. Slice 11s wired `stream_options.include_usage` and `Usage` on the final Token, so budget tracking still works under streaming. Providers that ignore `stream_options` leave `Usage` zero — caller tolerates that. |
| 11u | Render assistant Markdown into Telegram HTML | done | Telegram's default parse mode treats Markdown as literal text, so the LLM's `**bold**`, `## headers`, `- bullets`, `[link](url)` output arrived in chat as raw chars. Aura now converts the LLM's Markdown to the small HTML subset Telegram supports (`b/i/s/u, code, pre, a, blockquote`) and sends with `tele.ModeHTML`. Headings degrade to `<b>` (Telegram doesn't render `<h1>`); bullets degrade to `•` (no `<ul>/<li>`); links restricted to http(s)/tg schemes to block `javascript:` smuggling. HTML reserved chars in plain text are escaped; chars inside `<code>/<pre>` are preserved correctly. Wired through both delivery paths: `handleConversation`'s final `c.Send` (non-streamed turns) via `sendAssistant`, and `consumeStream`'s progressive `Send/Edit` (streamed turns). Operator-facing strings (auth errors, bootstrap messages) keep raw `c.Send` to avoid double-escaping. |
| 10e | UI: polish + theme redesign | done | Two waves: **(A) polish** — dark mode default, shadcn `Skeleton` placeholders replace "Loading…" across HealthDashboard / WikiPanel / SourceInbox / TasksPanel; stronger empty-state CTAs (BookText / Calendar icons + helpful copy); ErrorBoundary fires a `sonner.error` toast on top of the inline card; `Shell` component splits desktop sidebar from a mobile slide-over (radix Sheet, < md); global keyboard shortcuts via `useKeyboardShortcuts` (`?` opens help dialog, `g h/w/g/s/t` chord navigation). Backend `/api/health` extended with `process` block (version, git_revision, started_at, uptime_seconds) — git revision read once via `runtime/debug.ReadBuildInfo`. **(B) theme redesign from logo** — palette derived from the new orb logo (deep navy disc, electric cyan-blue arrow A); rewrote light + dark + contrast shadcn token blocks in oklch; ambient aurora radial-gradient on dark/contrast bodies; new inline-SVG `BrandMark` (sidebar) + larger glowing `LoginBrandMark` (login page); active-nav items get a brand glow (`bg-primary/10 ring-primary/20 shadow-[0_0_20px_-8px_var(--primary)]`); cards gain a hover top-stripe gradient + `hover:border-primary/30`. Bundle: 521 KB JS / 161 KB gz, 105 KB CSS / 18 KB gz. |
| 12a–12u | Phase 12 — Compounding Memory | done | Conversation archive (12a–12c), summarizer pipeline (12d–12f, 12k.1), wiki maintenance (12g–12h, 12l.1), compounding metric (12i, 12m), dashboard routes (12j, 12k, 12l, 12n), Q&A coverage (12o–12r), live E2E checklist + coverage report (12s–12t), Opus 4.7 review (12u). Executed by a 3-teammate Claude Code Agent Team (Backend / Frontend / Q&A) all on Sonnet 4.6, 21 atomic commits + 1 lead cleanup + 1 applier hotfix. v0.12.0. |
| 12u.1–12u.9 | Phase 12 follow-ups (post-review) | done | CR-01 + CR-02 and HR-01/02/03/04/05/06/07. HR-01 fixed `RepairLink` partial-commit; HR-02 preserves summarizer proposal category + related slugs through review approval. |
| 14a | Settings store + DB-overrides-env applier | done | `internal/settings` SQLite KV store on `cfg.DBPath`. `ApplyToConfig` overlays DB rows on top of env-loaded config; bootstrap fields (TelegramToken, HTTPPort, DBPath, LogLevel, paths) excluded. Empty DB = identical behavior. 23 unit tests. |
| 14b+c | First-run setup wizard with provider presets + live probe | done | `internal/setup` package: server-rendered HTML form at `cfg.HTTPPort` (loopback-forced, no auth) when `TELEGRAM_TOKEN` is blank. 8 LLM provider presets, live `/v1/models` probe, Ollama auto-detect via `/api/tags`. On Save: writes `TELEGRAM_TOKEN` to `.env` (atomic temp+rename), LLM_* to settings DB; main.go re-loads cfg without restart. 18 unit tests + 4 Playwright specs. |
| 14d | Auth'd /settings dashboard page | done | `GET /api/settings` returns 30-key catalog with `value` (effective: DB \| env \| default), `source`, `kind` (text/bool/int/float/enum/url), `is_secret`, `hint`. `POST /api/settings` bulk-upserts; `IsOverridable` rejects bootstrap keys at the API layer. `POST /api/settings/test` reuses the wizard probe. Frontend: grouped form (provider/embeddings/ocr/budget/summarizer/other), bool→toggle switch, enum→select, int/float→number input, url→type=url. Per-row dirty state + revert. 8 backend tests + 6 E2E. |
| 14d-redesign | 2026 polish (Geist/Linear/Stripe patterns) | done | Small-caps section labels with 0.08em tracking, hairline divider headers, `divide-y` rows, 3px tinted focus halo via `oklch(from var(--primary) ...)`, 13/12.5/11px type ramp. Switch contrast hardened with inline styles after the global `button { background: none; border: none }` reset killed Tailwind utilities. |
| 14d-followup | SPA code-split | done | App.tsx route elements lazy-loaded. Main bundle 580 KB → 353 KB; each panel 5–12 KB on first navigation. WikiGraphView (189 KB) + WikiPageView (141 KB markdown renderer) only download when their routes are visited. |
| 14e | Slim .env.example + INSTALL.md rewrite | done | Required env shrunk to TELEGRAM_TOKEN + HTTP_PORT + DB_PATH + 4 paths + LOG_LEVEL. INSTALL.md flows: BotFather → run binary → wizard → /start. |
| 14.delete | Tasks delete (user "/tasks can not delete task") | done | New `POST /api/tasks/{name}/delete` hard-removes rows; Cancel still flips status to preserve audit trail. Frontend Delete button next to Cancel with `window.confirm`. SchedulerStore interface gained `Delete(ctx, name)`. |
| 14.recurrence | Recurring tasks (user "can not schedule recurrent task") | done | New `ScheduleEvery` kind + `schedule_every_minutes INTEGER` column with idempotent `ALTER TABLE` migration on existing aura.db files. API accepts `every_minutes` (>=1); validateScheduleFields enforces exclusivity with at/daily; advance-after-fire computes `firedAt + N*time.Minute`. UI: "Every N minutes" radio in NewTaskDialog with hint ("60 = hourly, 1440 = daily, 10080 = weekly"). |
| 14.cleanup | Conversation archive cleanup (user "db will be full with no control") | done | `ArchiveStore` gained `DeleteByChat`, `DeleteOlderThan`, `DeleteAll`, `Stats`. New endpoints: `GET /api/conversations/stats` (row count + oldest + distinct chats), `POST /api/conversations/cleanup?chat_id=X` / `?older_than_days=N` / `?all=true` with mutually-exclusive validation. Frontend toolbar: stats badge in header, "Purge older than…" prompt, "Wipe this chat" (visible when chat_id filter set), "Wipe all" — all confirm-gated. 6 E2E specs. |
| 14.5 | Dashboard UX hardening | done | Mobile cards on WikiPanel/SourceInbox/TasksPanel/ConversationsPanel; WikiGraph mobile fallback; 44px touch targets; AA contrast on metadata text; auth-expiry returnTo across query/state/sessionStorage; custom ConfirmModal replaces window.confirm/prompt. New `e2e/confirm-modal.spec.ts`. Closes the historical dashboard UX audit. |
| 15a | `create_xlsx` tool + Telegram delivery | done | New `internal/files` pkg with `BuildXLSX` using `xuri/excelize/v2`; formula-injection sanitization (CWE-1236) via leading apostrophe on `=`/`+`/`-`/`@`/`\t`/`\r`. Caps: 16 sheets · 10 000 rows/sheet · 100 cols/row · 200 000 cells · 25 MB serialized · 80-char filename. New `source.KindXLSX` (.xlsx ext). New `tools.CreateXLSXTool` persisting via the existing source store (sha256 dedup → "show me last week's invoice" for free). New `tools.DocumentSender` interface satisfied by `Bot.SendDocumentToUser` (mirrors `SendToUser` pattern from slice 10d's `request_dashboard_token`). Tool wired post-construction in `setup.go`. New `cmd/debug_xlsx` 5-scenario hermetic harness (happy path + injection neutralized + dedup + path-traversal blocked + caps). 19 unit tests (12 xlsx + 7 tool). |
| 15a-livetest | Telegram E2E smoke for slice 15a | done | Real Telegram bot run with the user. Three real `create_xlsx` calls fired naturally from prompts (no prompt engineering): `expenses.xlsx`, `wiki-pages.xlsx` (LLM chained `list_wiki` then `create_xlsx`), `budget.xlsx`. All persisted with `kind=xlsx`/`status=ingested`/correct openxml mime, 127–400 ms generate, delivered via `tele.Document`. Manifest description was sufficient for tool selection. |
| 15d | Dashboard download endpoint + button | done | `GET /api/sources/{id}/raw` generalized via a kind→asset table (`rawAssets[Kind] → {filename, contentType, disposition}`); PDFs render `inline`, XLSX forces `attachment`. Adding 15b/15c is one row each — no router change. `validKind` accepts `xlsx`. `SourceSummary` TS kind union extended. `SourceInbox` row gains a Download button (PDF + XLSX); fetch with bearer header → blob URL → trigger download (auth-gated `<a href>` doesn't work because Authorization headers don't tag along on link clicks). Re-OCR / Ingest buttons now hidden for non-PDF kinds — XLSX skips OCR entirely. New router test covers PDF (inline), XLSX (attachment), text (404). |
| 15b | `create_docx` tool + Telegram delivery | done | New `internal/files/docx.go` — pure-Go OOXML zip writer (no third-party dep); the three required parts (`[Content_Types].xml`, `_rels/.rels`, `word/document.xml`) emit at ~1.4 KB for a multi-block memo. Block kinds: `heading` (level 1–6 clamped, rendered as bold + half-point-size run formatting so we don't need a /word/styles.xml), `paragraph`, `bullet` (rendered with a `•` + space prefix to avoid a numbering definition), `table` (bordered, 5000 pct width). XML reserved chars escaped via `xml.EscapeText` (rejects raw `<script>` etc. — DOCX consumers refuse files with raw `<` or `&` in `<w:t>`). Caps: 1000 blocks · 500 rows/table · 50 cols/row · 50 000 chars/block · 25 MB · 80-char filename. New `source.KindDOCX` (.docx ext). New `tools.CreateDOCXTool` reuses the slice 15a `DocumentSender` interface; same persist + sha256-dedup + auto-`StatusIngested` flow. `rawAssets[KindDOCX]` row + `validKind` extension wire dashboard download. Frontend kind union + `SourceInbox` Download gate extended. New `cmd/debug_docx` 5-scenario hermetic harness. 8 docx tests (`internal/files`) + 5 docx tool tests (`internal/tools`) + extended router test (PDF + XLSX + DOCX + text 404). |
| 15b-livetest | Telegram E2E smoke for slice 15b | done | Three real `create_docx` calls: `Quarterly Highlights Memo.docx`, `Project Status.docx`, `Wiki Pages Summary.docx`. The wiki-summary call exercised the full ecosystem: `list_wiki` → 3 **parallel** `read_wiki` calls (slice 11l fan-out, all started within 1 ms) → `create_docx`. 162–286 ms per generate, all delivered via `tele.Document`. |
| 15c | `create_pdf` tool + Telegram delivery | done | New `internal/files/pdf.go` — pure-Go via `github.com/go-pdf/fpdf` (single dep, no transitive). Same block grammar as create_docx (heading / paragraph / bullet / table) so the LLM only learns one DSL across the three formats. A4 + 15 mm margins + Helvetica family (one of fpdf's 14 base fonts → no font-subset embedding, fully self-contained). Headings: bold + ramped sizes 18→10pt for H1→H6. Tables: bordered, auto-sized cell width across the printable width, first row bolded as a header treatment. **Latin-1 sanitization**: fpdf's standard fonts only support cp1252; curly quotes / em-dashes / ellipses / NBSP / tabs in LLM output would crash at write time. `latin1Sanitize` maps the common offenders to ASCII equivalents (apostrophe, straight quote, hyphen, three dots, plain space) and drops anything else outside cp1252 to a literal question mark. New `source.KindPDFGen` (`pdf_generated`) — distinct from `KindPDF` (uploads) so OCR-only UI actions hide cleanly and `ingest_source` never tries to compile a generated PDF that has no `ocr.md`. Same on-disk filename + content-type as KindPDF (`original.pdf` + `application/pdf` + `inline` disposition) — the file IS a PDF either way; only the source.Kind disambiguates. New `tools.CreatePDFTool` reuses `DocumentSender`. Tool registration alongside xlsx/docx in `setup.go`. New `cmd/debug_pdf` 5-scenario hermetic harness (happy path + Latin-1 sanitization + dedup + path-traversal blocked + caps). 9 pdf tests in `internal/files` + 5 pdf tool tests in `internal/tools` + extended router test (5 kinds: PDF + XLSX + DOCX + PDFGen + text 404). |
| 15e | Natural-prompt file creation smoke | done | New `cmd/debug_files` harness registers the real `create_xlsx`, `create_docx`, and `create_pdf` tools against a hermetic temp source store and a `DocumentSender` stub. Three ordinary prompts verify model tool selection, persisted source kind/status, file asset bytes, and delivery. Live run on 2026-05-03 with `LLM_MODEL=glm-5.1:cloud` passed all 3 scenarios. |
| 16a | Structured tool errors | done | New `internal/tools/error.go` with `ToolError` JSON struct (`ok`, `error`, `retryable`, `hint`), `FormatToolError` (retryable=true + pattern-matched hint), `FormatFatalToolError` (retryable=false). `hintForError` maps error keywords (missing/required, invalid/malformed, not found, too large) to actionable hints. `executeToolCalls` in `conversation.go` now produces structured JSON instead of `"(tool error) raw msg"`. 7 unit tests. |
| 16b | System prompt retry directive | done | New paragraph in `system_prompt.go` "Tool Use" section tells the LLM to read `{"ok":false,...}` results, correct arguments using `hint` if `retryable:true`, retry once, or explain the problem if fatal/retry-fails. |
| 16c | Immediate Telegram placeholder | done | `handleConversation` sends a "⏳" placeholder via `c.Bot().Send` before entering `runToolCallingLoop`. Signature changes thread `*tele.Message` through `runToolCallingLoop` → `consumeStream`. `consumeStream` edits the existing placeholder instead of creating a new message; falls back to `Send` if edit fails. Non-streamed delivery deletes the placeholder and sends the real response. |
| 16d | Defer EnforceLimit to background | done | Moved `convCtx.EnforceLimit` from before `runToolCallingLoop` to a fire-and-forget goroutine after the archiver block, so summarizer latency doesn't block the user seeing the response. |
| 16e | Throttle 800ms → 600ms | done | `streamingEditThrottle` tightened from 800ms to 600ms. Still safe under Telegram's ~1/sec edit rate limit. |
| 17a | AuraBot bounded runner | done | New `internal/agent.Runner`: Telegram-free mini LLM/tool loop for future AuraBot workers. Uses `llm.Send`, explicit per-task tool allowlists, execution-time allowlist enforcement, structured tool errors, per-run timeout, per-tool timeout, concurrent tool calls with deterministic result ordering, user-id context propagation, token/tool/LLM telemetry. 7 unit tests. |
| 17b | AuraBot swarm store + manager | done | New `internal/swarm` package: SQLite `swarm_runs` / `swarm_tasks` store plus `Manager` that persists assignments, fans out bounded parallel `agent.Task` runs, enforces `MaxActive` and `MaxDepth`, marks task/run success or failure, and returns audit-ready task results. SQLite writes are serialized with one connection + busy timeout. 8 unit tests. |
| 17c | AuraBot LLM tools + debug metrics | done | `AURABOT_*` config/settings gate, bot wiring, `spawn_aurabot` / `list_swarm_tasks` / `read_swarm_result`, token metrics persisted on tasks, and `cmd/debug_swarm` hermetic E2E harness with wall/task/token/tool/speedup metrics. |
| 17d | AuraBot swarm observability | done | Read-only API + dashboard panel for swarm runs/tasks, aggregate counts, wall/task elapsed, speedup, LLM/tool/token telemetry, and per-task results/errors. |
| 17e | AuraBot planner + synthesis | done | Deterministic read-only planner builds role assignments from a goal, `run_aurabot_swarm` executes the team in parallel, and synthesis rolls up worker results/metrics without an extra LLM call. |
| 17f | AuraBot conservative routing | done | Telegram prompt now exposes swarm routing only when `run_aurabot_swarm` is actually registered, adds a per-turn hint for broad read-only second-brain work, and keeps mutations on explicit write/admin tools. |
| 17g | Proactive wiki proposals | done | New `propose_wiki_change` LLM tool writes pending wiki proposals into the existing dashboard Summaries review queue, letting Aura suggest durable second-brain growth without mutating wiki files directly. |
| 17h | Daily recurrence parity | done | `schedule_task` now exposes `every_minutes` and daily `weekdays`; scheduler persists weekday filters, API/dashboard surface them, and natural-prompt E2E verifies hourly + business-day scheduling. |
| 17i | Scheduled agent jobs | done | New `agent_job` task kind runs bounded propose-only routines through the Aura runner; `schedule_task`, API/dashboard, dispatcher, and natural-prompt E2E can schedule recurring agent jobs. |
| 17j | Daily briefing tool | done | New read-only `daily_briefing` tool composes today's tasks, pending wiki proposals, open wiki issues, recent sources, and conversation signals; natural-prompt E2E verifies an Italian daily-briefing prompt selects the tool. |
| 17k | Unified memory evidence search | done | New read-only `search_memory` tool searches wiki index, source inbox/OCR, and conversation archive with compact evidence snippets, source IDs, conversation turn IDs, and OCR page numbers; agent jobs and AuraBot read-only roles can use it before broader reads. |
| 17k.1 | Log-driven agent drift fixes | done | Runtime logs showed scheduled-job testing drifting into `spawn_aurabot` + repeated web searches, fenced summarizer JSON being rejected, and `write_wiki` retries delayed by generic tag-limit guidance. Fixed fenced JSON parsing and made wiki tag/source limits explicit in tool schema/error hints. |
| 17l | Run scheduled routines now | done | Added `run_task_now` so "eseguilo adesso" executes the saved scheduled `agent_job` by name, reuses its normalized payload/tool allowlist, records metrics, and sends the completion summary when `notify=true` instead of improvising with `spawn_aurabot`. |
| 17m | AuraBot completion guardrails | done | Live `/swarm` run showed a researcher issuing repeated web searches, filling context, and failing after 90s with zero UI metrics. AuraBot tasks now have per-role tool budgets, compact tool-result clipping, a forced final synthesis turn with tools disabled, and deadline partial completion after evidence has been gathered. |
| 17n | AuraBot value timeout | done | Raised AuraBot timeout default and local runtime value from 90s to 300s. The longer wall clock is paired with slice 17m's tool budgets/finalization guardrails, so agents have time for useful work without unbounded search loops. |
| 17o | Dashboard AuraBot settings | done | `/settings` now exposes AuraBot in its own group with editable defaults for enabled/max-active/depth/timeout/iterations, explains DB-over-`.env` precedence, and lets operators save overrides to `aura.db` instead of editing `.env`. |
| 17p | Settings active-vs-saved diagnostics | done | `/settings` now returns the running process value for each row plus `restart_required`; the dashboard highlights rows where a saved DB override differs from the active config, so users know when a restart is needed. |
| 17q | Live AuraBot settings apply | done | Saving AuraBot max-active/max-depth/timeout/max-iterations in `/settings` now updates the in-process runner/manager for subsequent swarm runs when AuraBot is already enabled. Enabling/disabling the swarm still requires restart because it changes registered tools. |
| 18a | Memory evidence envelope | done | `search_memory` now appends a structured JSON evidence envelope after the readable evidence list so final answers can preserve source IDs, wiki slugs, conversation IDs, snippets, scores, OCR page numbers, and warnings without noisy citations in casual chat. |
| 18b | Maintenance memory decay | done | Wiki maintenance now flags stale compiled-memory pages as `memory_decay` issues after conservative age thresholds, preserving the LLM Wiki rule that old knowledge becomes review work instead of silent mutation. |
| 18c | Proposal provenance | done | `propose_wiki_change` and summarizer review proposals now persist structured provenance JSON with origin tool/reason, evidence refs, agent job IDs, and swarm IDs; API responses expose it for review UI. |
| 18d | Batch proposal review | done | `/summaries` now supports batch approve/reject with per-ID failures, and the dashboard can select multiple proposals while showing compact provenance evidence on each card. |
| 18e | Evidence drill-down | done | Proposal evidence chips now link to source, wiki, and conversation/archive context; source/conversation panels honor hash navigation. Added Playwright E2E for the review evidence flow. |
| 18f | Memory quality scorecard | done | New hermetic `cmd/debug_memory_quality` harness runs 20 everyday second-brain questions through `search_memory`, creates 4 review-gated wiki proposals, and fails if evidence/proposal quality falls below 90%. |
| 18g | Live memory routing scorecard | done | `cmd/debug_memory_quality -live-llm` drives the same 20 questions through the live LLM/tool loop, measures routing/tool/proposal drift, and proposal creation now rejects `origin_tool=search_memory` without evidence. |
| 18h | Memory quality report graph | done | `debug_memory_quality` can now save timestamped local JSON reports with summary metrics, full live/hermetic results, and graph-ready nodes/edges for scenario -> tool -> evidence/proposal analysis. |
| 18-close | Phase 18 closure | done | Phase 18 memory layer is closed: evidence envelope, decay, provenance, batch review, drill-down, live scorecard, and graph-ready quality reports all shipped under the LLM Wiki memory philosophy. |
| 19 | Code inventory + procedural memory | closed | Pre-GSD productization track closed; shipped code inventory, review-gated skill proposals, graph-aware search, toolsets, skill/context-backed agent jobs, wake gates, E2E harnesses, and skill lifecycle decision. Remaining cleanup moved to `v1.0 Production Readiness` in `.planning/`. |
| 19a | Code inventory and low-risk cleanup | done | Historical inventory doc removed from `docs/` cleanup; source remains in git history. Removed stale `debugAssignments`; fixed staticcheck hygiene in debug/test/client code. |
| 19b | Review-gated skill proposals | done | Added `propose_skill_change`: validates complete SKILL.md drafts, stores create/update/delete skill proposals with provenance/allowed tools/smoke prompt in `proposed_updates`, and keeps approval from mutating wiki pages. |
| 19b.1 | End-user latency gate | done | Live memory scorecard now has `-live-latency-budget` and fails scenarios that are correct but too slow for an end user. |
| 19c | Graph-aware semantic index | done | Wiki indexing now embeds compact graph node cards and category/global index cards alongside page bodies; `search_memory` exposes graph evidence without turning embeddings into durable memory. |
| 19d | Named toolset profiles | done | `internal/toolsets` centralizes profiles and role presets; scheduler and AuraBot swarm now reuse the same catalog and keep recursive/dangerous tools out of scheduled jobs. |
| 19e | Skill/context-backed agent jobs | done | `agent_job` payloads now normalize `enabled_toolsets`, `skills`, `context_from`, and `wake_if_changed`; runtime prompts guide skill reads, memory-first context, and no-op prechecks. |
| 19f | Agent-job outputs and wake gates | done | Scheduled `agent_job` runs persist compact output/metrics/signature, deterministic wiki/source/task wake gates can skip LLM calls, and `context_from` can include prior task outputs. |
| 19g | Scheduled-routine E2E harness | done | `cmd/debug_agent_jobs` proves run -> skip -> mutate -> rerun with persisted output/metrics/signature; skipped run makes zero LLM/tool calls. |
| 19g.1 | Scheduled-job runtime context and rendered notifications | done | Log-driven fix: scheduled `agent_job` prompts share the interactive Runtime Context, include scheduled-for vs running-at metadata for late runs, and render assistant-generated notifications through Telegram HTML instead of leaking Markdown. |
| 19h | Skill proposal lifecycle decision | done | Phase 19 uses Option A: skill proposals remain review-only on `/summaries` approval, expose an explicit `skill_lifecycle` API handoff, and document manual install/smoke as the admin path for Phase 20. |
| 19i | Real-user routine drill | superseded | Not active in `docs/`; usefulness/latency drills should be added only as explicit `v1.0` or later phase acceptance checks. |
| 19j | Legacy and debt closure | superseded | Broad debt cleanup moved to `.planning/codebase/CONCERNS.md` and `v1.0 Production Readiness`; no unknown Phase 19 debt remains in `docs/`. |
| 19-close | Formal closure | done | Tracker now points to `.planning/` for active phases; stale historical plan/spec files removed. |
| sandbox.1 | Sandbox toolset guardrails | done | Consolidated code-execution tools into an explicit `sandbox_code` profile and restored `scheduler_safe` to propose-only defaults; scheduled `agent_job` rejects sandbox profiles because executable code is outside the recurring-job perimeter. |
| sandbox.pyodide.0 | Sandbox architecture pivot | done | Replaced the Isola product plan with a bundled Pyodide offline-runtime plan grounded in the official Pyodide package list; next slice is runtime abstraction before adapter implementation. |
| sandbox.pyodide.1 | Runtime abstraction | done | `internal/sandbox.Manager` now delegates execution/validation/health to a runtime adapter; legacy Isola is behind the boundary and `/health` reports `runtime_kind` plus detail without widening scheduler-safe sandbox permissions. |
| sandbox.pyodide.1b | Legacy runtime removal | done | Removed the host-Python sidecar, Python-path config, and fallback startup probe. Sandbox now fails closed with `runtime_kind=unavailable` until the bundled Pyodide adapter lands. |
| sandbox.pyodide.2 | Bundle manifest and probe | done | Added the Pyodide bundle manifest contract, path containment and sha256 validation, required runtime file/package import checks, `SANDBOX_RUNTIME_DIR`, startup health diagnostics, and docs for the release bundle schema. |
| sandbox.pyodide.3 | Pyodide runner adapter | done | Added the JSON stdin/stdout runner adapter with sanitized env, timeout kill, fake-runner tests, opt-in live Pyodide bundle smoke, and ignored local runtime bundle install. |
| sandbox.pyodide.4 | Offline package smoke | done | Added `cmd/debug_sandbox --smoke` plus reusable smoke scenarios for arithmetic, data imports, XLSX read, matplotlib artifact generation, and PDF/text extraction; missing bundles fail as unavailable. |
| sandbox.pyodide.5 | Enable execute_code | done | Telegram startup now constructs the Pyodide runner, enables `execute_code` only when bundle and runner health are available, reports invalid bundles as unavailable, and supports the local Windows `.cmd` dev runner fallback. |
| sandbox.pyodide.6 | Release bundle packaging | done | Added a pinned Pyodide bundle installer, release workflow/Goreleaser hooks, archive inclusion for `runtime/pyodide/**`, publish-time `debug_sandbox --smoke`, and config tests for release packaging. |
| sandbox.pyodide.7 | Registered execute_code smoke | done | Started Aura with the rebuilt local bundle, confirmed authenticated `/api/health` reports `runtime_kind=pyodide` and `available=true`, and added `cmd/debug_sandbox --tool-smoke` to run `sum(range(1, 101))` through the registered `execute_code` tool boundary. |
| sandbox.pyodide.7b | Telegram conversation sandbox smoke | done | Added `cmd/debug_telegram_sandbox`, a live-LLM Telegram-handler smoke that injects a synthetic private text update, sends real outgoing bot messages, verifies `execute_code` was called, and asserts the conversation surfaced `5050`; also fixed `Bot.Stop` for debug-created bots that never started polling. |
| sandbox.pyodide.8 | Artifact egress | done | Pyodide code can write direct-child files under `/tmp/aura_out`; the runner returns bounded base64 artifacts, Go decodes them into `sandbox.Artifact`, `execute_code` reports artifact metadata and auto-delivers through Telegram when user context and sender are available, and `cmd/debug_sandbox --artifact-smoke` proves the boundary. |
| sandbox.pyodide.8a | File tool choice clarity | done | Added prompt and tool-description guidance so ordinary spreadsheets/docs/PDFs prefer typed `create_*` tools that persist Aura sources, while `execute_code` is reserved for computed artifacts, plots, custom exports, and code-required workflows. |
| sandbox.pyodide.9 | Live Telegram artifact smoke | done | Extended `cmd/debug_telegram_sandbox --artifact-smoke` to require `execute_code`, artifact metadata, and real Telegram document delivery; live run delivered `aura_artifact.txt` after raising the Pyodide timeout default to 60 seconds. |
| sandbox.pyodide.close | Production closure | done | Rich artifact smokes persist CSV/PNG outputs as sources, live Telegram artifact delivery passed, v3.0.2 GitHub release shipped with bundled Pyodide runtime assets. |

## Session Log

### 2026-05-04 - Docs phase cleanup and legacy plan removal

Goal: close stale pending phase handoffs in `docs/` and make `.planning/` the only active phase surface.

Implementation:

- Updated the current handoff to mark Pyodide sandbox production closure as complete and released in `v3.0.2`.
- Marked Phase 19 as closed/superseded by the `v1.0 Production Readiness` milestone rather than leaving `19i`, `19j`, and `19-close` as ambiguous docs debt.
- Removed historical plan files and obsolete audit/strategy docs from the active docs tree; their content remains recoverable from git history.
- Updated lingering references so tests and operator docs point to the tracker or `.planning/` instead of deleted plan files.

Verification:

- Docs-only cleanup; no Go behavior changed.
- Reference scan after cleanup confirms no active code/tests point to deleted plan files.

### 2026-05-04 - Sandbox.pyodide.9 (Live Telegram artifact delivery smoke)

Goal: prove the live Telegram + LLM conversation path can create and deliver a sandbox artifact file.

Implementation:

- Extended `Bot.RunDebugTextSmoke` results with artifact metadata detection and a bounded debug-only capture of successful Telegram document-send metadata.
- Added `cmd/debug_telegram_sandbox --artifact-smoke`, which asks the live LLM to write `/tmp/aura_out/aura_artifact.txt` through `execute_code` and fails unless a Telegram document is sent.
- Raised `SANDBOX_TIMEOUT_SEC` default from 15 to 60 seconds after the first live run showed Pyodide cold-starting past the old limit.
- Updated runtime docs and config tests for the new timeout default.

Verification:

- `go test ./internal/telegram -run "TestDebug(TextSmokeResultFromMessagesDetectsArtifactMetadata|DocumentSendsAfterReturnsOnlyNewSends)" -count=1 -v`
- `go test ./cmd/debug_telegram_sandbox -run TestTelegramSandboxSmokeReport -count=1 -v`
- `go test ./internal/config -run "TestLoad(Success|SandboxTimeout)" -count=1 -v`
- `go test ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`
- First live run failed usefully at the old 15-second sandbox timeout.
- `go run ./cmd/debug_telegram_sandbox --artifact-smoke --timeout 4m` passed: `tool_calls=execute_code`, `contains_artifact_metadata=true`, `artifact_filenames=aura_artifact.txt`, `document_sends=1`, `size=30`, caption `Aura sandbox artifact: aura_artifact.txt`, final text confirmed delivery.
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Follow-up completed: `sandbox.pyodide.close` persisted sandbox artifacts as sources, upgraded artifact smokes beyond hello-world, and shipped the production release.

### 2026-05-04 - Sandbox.pyodide.8a (File tool choice clarity)

Goal: reduce model confusion now that both typed file tools and sandbox artifacts can return files.

Implementation:

- Added system-prompt guidance for file-generation tool choice.
- Updated `create_xlsx`, `create_docx`, and `create_pdf` descriptions to prefer typed tools over `execute_code` for ordinary documents.
- Updated `execute_code` description to defer simple documents to typed tools and reserve sandbox artifacts for computed outputs.
- Added tests locking the prompt and tool-description arbitration text.

Verification:

- `go test ./internal/conversation -run TestDefaultSystemPromptClarifiesFileGenerationToolChoice -count=1 -v`
- `go test ./internal/tools -run "Test(ExecuteCodeTool_DescriptionDefersSimpleDocumentsToTypedTools|FileCreationToolDescriptionsPreferTypedToolsOverSandbox)" -count=1 -v`

Next slice remains `sandbox.pyodide.9`: live Telegram artifact-delivery smoke.

### 2026-05-04 - Sandbox.pyodide.8 (Artifact egress)

Goal: give sandbox-created files a narrow, explicit return path without broad filesystem access.

Implementation:

- Chose `/tmp/aura_out` as the only output directory for this slice.
- Added runner protocol support for `output_file_allowlist` plus bounded artifact collection from direct child files only.
- Added `sandbox.Artifact` on execution results and guarded Go-side decode against traversal names, bad base64, count overflow, size overflow, and size mismatches.
- Extended `execute_code` to include artifact metadata in tool output and to send returned artifacts through the existing Telegram `DocumentSender` when a Telegram user context is present.
- Added `cmd/debug_sandbox --artifact-smoke` and runtime docs for the artifact contract.

Verification:

- `go test ./internal/sandbox -run "TestPyodideRunner_Execute(SendsProtocolAndSanitizedEnv|ReturnsArtifacts)" -count=1 -v`
- `go test ./internal/tools -run TestExecuteCodeTool_DeliversArtifacts -count=1 -v`
- `go test ./cmd/debug_sandbox -run TestRunExecuteCodeArtifactSmokeRequiresArtifact -count=1 -v`
- `go test ./internal/sandbox ./internal/tools ./internal/telegram ./cmd/debug_sandbox -count=1`
- `node runtime/install-pyodide-bundle.mjs --runtime-dir runtime/pyodide --with-node-win-x64`
- `go run ./cmd/debug_sandbox --artifact-smoke`
- `go run ./cmd/debug_sandbox --tool-smoke`
- `go run ./cmd/debug_sandbox --smoke`

Known note: the artifact smoke has no Telegram user context, so it correctly reports `delivered=false`; unit coverage verifies delivery when user context and `DocumentSender` are present.

Next slice: `sandbox.pyodide.9` run a live Telegram artifact-delivery smoke where the model creates a small file or plot and Aura sends the document to the requesting user.

### 2026-05-04 - Sandbox.pyodide.7b (Telegram conversation sandbox smoke)

Goal: prove Aura's Telegram conversation loop can route a natural user request through the live LLM, call `execute_code`, and surface the Pyodide result.

Implementation:

- Started a temporary `cmd/aura` binary with the rebuilt local bundle and confirmed authenticated `/api/health` reported sandbox `enabled=true`, `available=true`, `runtime_kind=pyodide`, and `detail="Pyodide runner available"`.
- Waited for a manual incoming Telegram update; none arrived during the observation window, so the slice shifted to a repeatable synthetic-incoming Telegram smoke.
- Added `Bot.RunDebugTextSmoke`, which injects a synthetic private text update into the normal Telegram conversation handler for an allowlisted user and summarizes whether `execute_code` was called and `5050` appeared.
- Added `cmd/debug_telegram_sandbox`, which loads `.env`, creates the real Aura Telegram bot without starting long polling, sends real outgoing Telegram placeholder/tool/final messages to the first allowlisted user, and fails unless the live LLM uses `execute_code` and returns `5050`.
- Fixed `Bot.Stop` so debug-created bots that never called `Start` close stores/MCP clients without deadlocking on telebot's poller stop channel.

Verification:

- `go test ./internal/telegram -run "TestStopWithoutStart|TestDebugTextSmokeResultFromMessages" -count=1 -v`
- `go run ./cmd/debug_telegram_sandbox` passed: `tool_calls=execute_code`, `called_execute_code=true`, `contains_5050=true`, final answer `The sum of numbers from 1 to 100 is **5050**.`
- `go test ./internal/telegram ./cmd/debug_telegram_sandbox -count=1 -v`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Known note: this is synthetic incoming Telegram rather than a human Telegram client update, because Bot API cannot impersonate a user. It still exercises Aura's Telegram handler, live LLM routing, real outgoing bot messages, and the registered Pyodide `execute_code` tool.

Next slice: `sandbox.pyodide.8` choose and implement the first explicit artifact egress path for sandbox-created files.

### 2026-05-04 - Sandbox.pyodide.7 (Registered execute_code smoke)

Goal: prove the rebuilt local Pyodide bundle is visible to Aura startup and that the registered `execute_code` tool boundary can execute the milestone arithmetic prompt.

Implementation:

- Started the real `cmd/aura` binary path from a local build with the rebuilt `runtime/pyodide` bundle.
- Confirmed startup logs enabled `execute_code` with `runtime_kind=pyodide` and `detail="Pyodide runner available"`.
- Confirmed authenticated `/api/health` reports sandbox `enabled=true`, `available=true`, `runtime_kind=pyodide`, and `runtime="./runtime/pyodide"`.
- Added `cmd/debug_sandbox --tool-smoke`, which constructs the Pyodide runner/manager, registers `tools.NewExecuteCodeTool`, runs `print(sum(range(1, 101)))`, and fails unless the tool output contains `5050`.
- Added `cmd/debug_sandbox` tests for the registered-tool smoke success path and unavailable-runtime diagnostics.

Verification:

- `go test ./cmd/debug_sandbox -count=1 -v`
- `go run ./cmd/debug_sandbox --tool-smoke`
- `go build -o .codex/tmp/aura-pyodide7-smoke.exe ./cmd/aura`
- temporary Aura process health probe at `http://127.0.0.1:8081/api/health` with a locally minted dashboard token
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Known gap: no live incoming Telegram user update was observed during this session, so the next slice must keep the app running while the operator sends the actual Telegram prompt and then inspect conversation/tool-loop logs for copy, streaming, and timeout issues.

Next slice: `sandbox.pyodide.7b` live incoming Telegram smoke through the bot conversation path.

### 2026-05-04 - Sandbox.pyodide.6 (Release bundle packaging)

Goal: make the Pyodide runtime reproducible for release archives and gate publishing on the real offline smoke.

Implementation:

- Added `runtime/install-pyodide-bundle.mjs`, pinned to Pyodide 0.29.3, to rebuild ignored `runtime/pyodide/` from npm/lock metadata, download the baseline package closure with sha256 checks, write the local manifest, and generate runner scripts.
- The installer supports `--with-node-win-x64`, which downloads Node 22.13.1 for Windows and places it under `runtime/pyodide/runner/node-win-x64` so the Windows archive is self-contained.
- Updated `.goreleaser.yml` to build the web assets, rebuild the Pyodide bundle, run `go run ./cmd/debug_sandbox --smoke`, and include `runtime/pyodide/**` in release archives.
- Updated `.github/workflows/release.yml` so CI installs web dependencies and builds the Pyodide bundle before GoReleaser.
- Added `internal/release/release_config_test.go` to pin the release workflow/config invariants.
- Replaced the deprecated GoReleaser `format_overrides.format` key with `formats`.
- Updated runtime docs with the reproducible installer and release archive behavior.

Verification:

- `go test ./internal/release -count=1 -v`
- `node runtime/install-pyodide-bundle.mjs --runtime-dir runtime/pyodide --with-node-win-x64`
- `go run ./cmd/debug_sandbox --smoke`
- `go run github.com/goreleaser/goreleaser/v2@latest check`
- `go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean --skip=publish` reached the new bundle installer and smoke hook successfully, then failed during local Windows cross-build with `unknown relocation type 7` from the existing `cmd/aura/resource.syso` under the Go 1.26 toolchain used by `go run`; CI uses the workflow's configured Go 1.25 toolchain.

Next slice: `sandbox.pyodide.7` real Telegram smoke through the registered `execute_code` tool.

### 2026-05-04 - Sandbox.pyodide.5 (Enable execute_code)

Goal: wire the Pyodide runner into Aura startup so `execute_code` becomes available only when the offline bundle and runner are healthy.

Implementation:

- Replaced the manifest-only Telegram startup probe with `setupSandboxRuntime`, which builds `sandbox.PyodideRunner`, checks availability, creates `sandbox.Manager`, and feeds explicit sandbox health to the dashboard/API.
- Kept fail-closed behavior: disabled sandbox, missing manifest, hash mismatch, and missing runner all leave `sandboxMgr=nil`, so `tools.NewExecuteCodeTool` does not register `execute_code`.
- Added startup policy tests for disabled, missing bundle, tampered bundle, missing runner, and healthy bundle paths.
- Added a Windows default-runner fallback so local ignored dev bundles can use `runtime/pyodide/runner/aura-pyodide-runner.cmd`; release `.exe` remains the normal packaged target when no `.cmd` exists.
- Updated runtime docs to reflect the current enablement rule.

Verification:

- `go test ./internal/telegram -run TestSetupSandboxRuntime -count=1 -v`
- `go test ./internal/sandbox -run TestPyodideRunnerDefaultPathUsesWindowsCmdDevRunner -count=1 -v`
- `go test ./internal/scheduler -run TestScheduler_Autonomous -count=1 -v` after the first broader batch hit a transient SQLite lock
- `go test ./internal/sandbox ./internal/tools ./internal/toolsets ./internal/scheduler ./internal/telegram ./internal/api -count=1`
- `go run ./cmd/debug_sandbox --smoke`

Next slice: `sandbox.pyodide.6` release bundle packaging and publish-time smoke.

### 2026-05-04 - Sandbox.pyodide.4 (Offline package smoke)

Goal: make the Pyodide bundle testable through a repeatable operator smoke command before enabling `execute_code`.

Implementation:

- Added `internal/sandbox/smoke.go` with reusable offline smoke scenarios and a report model for availability, per-scenario output, and failures.
- Added `internal/sandbox/smoke_test.go` with TDD coverage for missing-bundle/unavailable reporting, offline scenario coverage, no-network execution, and missing marker failures.
- Added `cmd/debug_sandbox --smoke`, with runtime-dir/runner/timeout flags, concise pass/fail output, and non-zero exit on unavailable bundles or scenario failures.
- Updated `runtime/README.md` with the repeatable smoke command.

Verification:

- `go test ./internal/sandbox -run TestRunPyodideSmoke -count=1 -v`
- `go test ./cmd/debug_sandbox ./internal/sandbox -count=1`
- `go run ./cmd/debug_sandbox --smoke --runtime-dir runtime\missing-pyodide --runner runtime\missing-pyodide\runner\missing.cmd` (expected unavailable failure)
- `go run ./cmd/debug_sandbox --smoke`

Next slice: `sandbox.pyodide.5` wire the Pyodide runner into Telegram startup and enable `execute_code` only when health is available.

### 2026-05-04 - Sandbox.pyodide.3 (Pyodide runner adapter + live bundle)

Goal: execute simple Python through the bundled Pyodide runner boundary and prove the local bundle can run the baseline package profile.

Implementation:

- Added `internal/sandbox/pyodide_runner.go`: JSON request/response protocol, runner path resolution, sanitized child environment, timeout enforcement, stdout/stderr capture, and runtime availability checks.
- Added hermetic fake-runner tests for command args, env filtering, timeout kill, runner failure, and invalid JSON.
- Added an opt-in live test (`TestPyodideRunner_LivePyodideBundle`) that validates the local manifest, starts the development runner, imports the full baseline package profile, and computes `sum(range(101))`.
- Installed the local Pyodide 0.29.3 bundle under ignored `runtime/pyodide/`: core Pyodide assets, closure of 37 package files, local lock/manifest hashes, and a Node-backed development runner script for E2E testing.
- Updated `.gitignore` so local runtime artifacts and npm extraction downloads cannot be staged accidentally.

Verification:

- `go test ./internal/sandbox`
- `$env:AURA_SANDBOX_LIVE='1'; $env:SANDBOX_PYODIDE_RUNNER='runtime\pyodide\runner\aura-pyodide-runner.cmd'; go test ./internal/sandbox -run TestPyodideRunner_LivePyodideBundle -count=1 -v`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: `sandbox.pyodide.4` offline package smoke command (`cmd/debug_sandbox --smoke`).

### 2026-05-04 - Sandbox.pyodide.2 (Bundle manifest and probe)

Goal: create a concrete offline Pyodide bundle contract before enabling user code execution.

Implementation:

- Added `internal/sandbox/manifest.go` with `aura-pyodide-manifest.json` loading, schema/runtime checks, required runtime file groups, sha256 validation, path containment, and the baseline office/data import profile.
- Added manifest tests for happy path, missing manifest, missing file, hash mismatch, containment failure, required-runtime hash validation, and missing imports.
- Added `SANDBOX_RUNTIME_DIR` config with default `./runtime/pyodide` and documented it in `.env.example`.
- Telegram startup now probes the configured runtime dir and surfaces missing/invalid bundle detail through sandbox health while keeping `execute_code` disabled until the runner adapter exists.
- Documented the manifest schema and package smoke list in `runtime/README.md`.

Verification:

- `go test ./internal/sandbox ./internal/config ./internal/api ./internal/tools ./internal/toolsets ./internal/scheduler ./internal/telegram`

Next slice: `sandbox.pyodide.3` Pyodide runner adapter.

### 2026-05-04 - Sandbox.pyodide.1b (Legacy runtime removal)

Goal: remove the host-runtime sandbox fallback immediately and make code execution fail closed until the bundled Pyodide adapter is implemented.

Implementation:

- Removed the host Python sidecar files from `internal/sandbox`.
- Simplified `sandbox.Config` to `Runtime` + timeout only; nil runtime now errors instead of auto-detecting a host interpreter.
- Removed Python-path and system-Python sandbox config fields from `internal/config` and `.env.example`.
- Telegram startup no longer searches for a runner or registers `execute_code` through a fallback runtime; sandbox health reports `runtime_kind=unavailable` with a clear Pyodide-adapter detail.
- Updated active sandbox docs to treat `runtime/pyodide/...` as the only execution path.

Verification:

- `go test ./internal/sandbox ./internal/toolsets ./internal/scheduler ./internal/telegram ./internal/api ./internal/tools ./internal/config`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: `sandbox.pyodide.2` bundle manifest and probe.

### 2026-05-04 - Sandbox.pyodide.1 (Runtime abstraction)

Goal: decouple `internal/sandbox` from Isola-specific host Python assumptions while preserving the current `execute_code` and toolset guardrails.

Implementation:

- Added a `sandbox.Runtime` adapter boundary with `RuntimeKind` values `pyodide`, `isola_legacy`, and `unavailable`.
- Moved the current sidecar execution, AST validation, and Isola availability probe behind an `isola_legacy` runtime adapter.
- Kept `Manager.Execute`, `Manager.ValidateCode`, `Manager.IsAvailable`, and `Manager.CheckAvailability` as the public surface so tool callers do not change when the Pyodide adapter lands.
- Extended API sandbox health with `runtime_kind`; Telegram startup now fills runtime kind, runtime path, and the concrete probe detail when the sandbox is enabled or unavailable.
- Added regression tests proving a non-legacy runtime adapter can initialize without `sandbox_runner.py`, execute through the manager boundary, and surface runtime kind/detail. Existing toolset and scheduled-job sandbox guardrails stayed unchanged.

Verification:

- `python -c "import ast; ast.parse(open('internal/sandbox/sandbox_runner.py', encoding='utf-8').read()); print('runner syntax ok')"`
- `go test ./internal/sandbox ./internal/toolsets ./internal/scheduler ./internal/telegram ./internal/api ./internal/tools`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: `sandbox.pyodide.2` bundle manifest and probe.

### 2026-05-04 - Sandbox.pyodide.0 (Architecture pivot)

Goal: change the sandbox product architecture from Isola/host-Python hardening to a bundled Pyodide offline runtime that supports real office/data packages.

Implementation:

- Rewrote the historical sandbox code-execution design so Pyodide is the approved backend and Isola is only legacy prototype context.
- Replaced the obsolete Isola task list in the historical sandbox plan with Pyodide migration slices: runtime abstraction, bundle manifest/probe, runner adapter, package smoke, `execute_code` switch, and Isola retirement.
- Updated `runtime/README.md` to document `runtime/pyodide/...` as the product layout and keep `runtime/python/...` legacy-only.
- Used the official Pyodide 0.29.3 package list as the package source of truth. Most required office/data packages are built in; `openpyxl` must be treated as a vendored wheel candidate or replaced after smoke testing.

Verification:

- Docs-only slice; no Go code changed.
- `git status --short -uall` started clean before edits.

Next slice: `sandbox.pyodide.1` runtime abstraction inside `internal/sandbox`, preserving current toolset guardrails while making runtime kind/health explicit.

### 2026-05-04 - Sandbox.1 (Sandbox toolset guardrails)

Goal: consolidate the newly landed sandbox/code-execution tools without widening autonomous scheduled-job permissions.

Implementation:

- Added explicit `toolsets.ProfileSandboxCode` with `execute_code`, `list_tools`, and `read_tool`.
- Removed sandbox execution/discovery tools from `scheduler_safe`.
- Kept `save_tool` out of the sandbox profile because it is a durable mutation and should remain an explicit direct tool/admin workflow, not a default profile capability.
- Tightened `scheduler.ResolveAgentJobTools`: if a requested enabled toolset resolves to no tools allowed by the scheduled-job perimeter, normalization now fails instead of silently falling back to defaults.
- Added regression tests proving:
  - `scheduler_safe` excludes `execute_code`, `list_tools`, `read_tool`, and `save_tool`;
  - `sandbox_code` exists as an explicit opt-in profile;
  - scheduled `agent_job` rejects `sandbox_code`.

Verification:

- `go test ./internal/toolsets ./internal/scheduler`
- `staticcheck ./internal/toolsets ./internal/scheduler`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: superseded by `sandbox.pyodide.1` runtime abstraction after the Pyodide architecture pivot.

### 2026-05-04 - Slice 19h (Skill proposal lifecycle decision)

Goal: make procedural-memory proposal review unambiguous without introducing a silent skill mutation path.

Decision:

- Chose Option A for Phase 19 closure: approving a skill proposal in `/summaries` marks the draft reviewed only.
- Install/update/delete and smoke execution remain an explicit admin handoff, documented in the historical skill proposal lifecycle note and now summarized here.
- Option B remains a future admin workflow, but it must not hook generic summary approval directly.

Implementation:

- Added `summarizer.IsSkillAction` to pair with `IsWikiAction`, making wiki mutations and skill proposals separate at the type boundary.
- Consolidated single and batch summary approval through the same guarded `applyApprovedSummary` path, so non-wiki actions return before any `AutoApplier` write.
- Added `skill_lifecycle` to `GET/POST /summaries` DTOs for skill proposals:
  - `mode=review_only`;
  - `review_status=pending_review|reviewed|rejected`;
  - `install_status=not_installed_by_summary_approval`;
  - `smoke_status=operator_required`;
  - `next_step` names the explicit admin handoff.
- Added tests for action classifiers, skill lifecycle DTOs, and approved skill proposals not mutating wiki pages.

Verification:

- `go test ./internal/conversation/summarizer ./internal/api`
- `staticcheck ./internal/conversation/summarizer ./internal/api`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: 19i, run the real-user routine drill and record usefulness/latency/tool metrics.

### 2026-05-04 - Slice 19g.1 (Scheduled-job runtime context and rendered notifications)

Goal: fix the production drift seen in `logs/aura-2026-05-04.log`, where an overdue scheduled routine ran without an explicit current date/time context and delivered the generated report as raw Markdown.

Root cause:

- Interactive chat used `conversation.RenderSystemPrompt(time.Now(), time.Local)`, but scheduled `agent_job` built an isolated system prompt that omitted the Runtime Context.
- The scheduler knew the persisted `scheduled_for` time, but did not tell the agent the actual wall-clock `running_at` time or the delay after downtime.
- `notifyAgentJob` sent assistant-generated output through raw `SendToUser`, unlike interactive replies which pass through `renderForTelegram`.

Implementation:

- Split `conversation.RenderRuntimeContext` out of `RenderSystemPrompt` so interactive turns and scheduled jobs share one wall-clock prompt block.
- Fixed timezone offset rendering from rounded whole hours to exact `UTC+HH:MM`.
- Added scheduled-job prompt metadata: task name, scheduled-for local/UTC time, running-at local/UTC time, schedule kind, and a late-run warning when the job fires more than one minute after `NextRunAt`.
- Added `sendGeneratedToUser` for assistant-generated Telegram text, preserving raw `SendToUser` for tokens/operator payloads.
- Routed `notifyAgentJob` and the legacy `auto_improve` owner notification through the generated-text renderer.
- Added regression tests for exact runtime offsets, late scheduled-job context, agent-job prompt injection, and Markdown rendering in scheduled notifications.

Verification:

- `go test ./internal/conversation ./internal/telegram ./internal/scheduler ./internal/tools`
- `staticcheck ./internal/conversation ./internal/telegram ./internal/scheduler ./internal/tools`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: 19h, decide and document or implement the skill proposal install/smoke lifecycle.

### 2026-05-04 - Slice 19g (Scheduled-routine E2E harness)

Goal: prove that skill-backed scheduled routines are cheap, resumable, and not context-hungry.

Implementation:

- Added `cmd/debug_agent_jobs`, a hermetic debug harness that creates a temp wiki, temp SQLite scheduler DB, a monitored wiki page, and a skill/context-backed `agent_job` payload with `enabled_toolsets`, `skills`, `context_from`, `wake_if_changed`, and `notify=false`.
- The harness runs the required sequence:
  - run 1 executes through the bounded agent runner, calls `read_wiki`, and persists `last_output`, `last_metrics_json`, and `wake_signature`;
  - run 2 sees the unchanged wake signature and skips before any LLM or tool call;
  - the harness mutates the monitored wiki page;
  - run 3 executes again with a changed wake signature and refreshed persisted result fields.
- Moved deterministic wake-signature computation into `internal/scheduler/wake.go`, so Telegram runtime and debug harnesses share the same wiki/source/task signal logic.
- Kept the harness side-effect envelope narrow: no dashboard dependency, no broad filesystem/source mutation, no direct wiki/skill mutation from the job, and no recursive scheduling tools.
- Added an optional `-live-llm` mode; the default deterministic fake LLM is the acceptance path.

Measured fake run:

- run 1: skipped=false, llm_calls=2, tool_calls=1, tokens=93, wake_changed=no.
- run 2: skipped=true, llm_calls=0, tool_calls=0, tokens=0, wake_changed=no.
- run 3: skipped=false, llm_calls=2, tool_calls=1, tokens=93, wake_changed=yes.

Verification:

- `go test ./internal/scheduler ./internal/telegram ./internal/tools ./cmd/debug_agent_jobs`
- `go run ./cmd/debug_agent_jobs`
- `staticcheck ./internal/scheduler ./internal/telegram ./internal/tools ./cmd/debug_agent_jobs`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: 19h, decide and document or implement the skill proposal install/smoke lifecycle.

### 2026-05-04 - Slice 19f (Agent-job outputs and wake gates)

Goal: make scheduled routines cheaper and more continuous, so recurring work does not spend tokens rereading unchanged context.

Implementation:

- Added `last_output`, `last_metrics_json`, and `wake_signature` columns to `scheduled_tasks`, with an idempotent migration for existing databases.
- Added `RecordAgentJobResult` and preserved result fields across normal task upserts, so editing a schedule does not erase the last useful run.
- `dispatchAgentJob` and `run_task_now` now persist compact output and run metrics after each execution.
- `wake_if_changed` now computes deterministic signatures for stable signals:
  - `wiki:<slug>` and `[[slug]]`
  - `source:<src_id>`
  - `task:<name>` / `agent_job:<name>`
- When the stored signature matches the current signature, Aura skips the LLM call and records a concise skipped result.
- `context_from` can reference prior task outputs with `task:<name>`, `agent_job:<name>`, or a bare task name; the compact prior output is injected into the next prompt.
- `list_tasks`, `run_task_now`, `/api/tasks`, and the frontend task type now expose the persisted agent-job result data where relevant.

Verification:

- `go test ./internal/scheduler ./internal/telegram ./internal/tools ./internal/api`
- `staticcheck ./internal/scheduler ./internal/telegram ./internal/tools ./internal/api`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`
- `npm run lint` (from `web/`)

Next slice: 19g, add a scheduled-routine E2E/debug harness that proves run -> skip -> mutate -> rerun with LLM/tool/token/latency metrics.

### 2026-05-04 - Slice 19e (Skill/context-backed agent jobs)

Goal: make scheduled routines more procedural and cheaper to run without granting broad tools or direct mutation.

Implementation:

- Extended `scheduler.AgentJobPayload` with:
  - `enabled_toolsets`
  - `skills`
  - `context_from`
  - `wake_if_changed`
- `enabled_toolsets` now resolve through `internal/toolsets`; unknown profiles fail normalization.
- `tool_allowlist` can narrow the selected toolsets, but it cannot expand outside the selected profile perimeter.
- Skill-backed jobs automatically enable `skills_read` tools so the runner can inspect attached `SKILL.md` files via `read_skill`.
- Runtime agent-job prompts now include compact sections for attached skills, context anchors, and wake-if-changed signals.
- `wake_if_changed` is currently a prompt-level no-op precheck: the agent checks those signals first and should stop quickly when there is no material change. A deterministic skip gate is left for 19f.
- `schedule_task` now documents the structured JSON payload shape so the LLM can schedule richer `agent_job` routines naturally.

Verification:

- `go test ./internal/scheduler ./internal/telegram ./internal/tools ./internal/toolsets`
- `staticcheck ./internal/scheduler ./internal/telegram ./internal/tools ./internal/toolsets`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: 19f, persist compact agent-job outputs/metrics and convert `wake_if_changed` into deterministic skip gates where Aura has stable signals.

### 2026-05-04 - Slice 19d (Named toolset profiles)

Goal: remove duplicated tool allowlists before making scheduled routines and AuraBot more proactive.

Implementation:

- Added `internal/toolsets` with named profiles:
  - `memory_read`
  - `wiki_review`
  - `skills_read`
  - `web_research`
  - `scheduler_safe`
- Centralized AuraBot role presets in the same package, preserving the existing role behavior for `librarian`, `critic`, `researcher`, `skillsmith`, and `synthesizer`.
- `scheduler.DefaultAgentJobTools` now comes from `toolsets.SchedulerSafeTools()`.
- `telegram.safeAgentJobTools` now filters through `toolsets.FilterAllowed`, so raw task payloads cannot sneak in recursive or high-risk tools.
- `swarm.BuildPlan` and `spawn_aurabot` now resolve role allowlists from `internal/toolsets` instead of maintaining separate maps.

Verification:

- `go test ./internal/toolsets ./internal/scheduler ./internal/telegram ./internal/swarm ./internal/swarmtools`
- `staticcheck ./internal/toolsets ./internal/scheduler ./internal/telegram ./internal/swarm ./internal/swarmtools`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: 19e, extend scheduled `agent_job` with `enabled_toolsets`, `skills`, `context_from`, and `wake_if_changed` using the new `internal/toolsets` catalog.

### 2026-05-04 - Slice 19c (Graph-aware semantic index)

Goal: speed complex memory questions by embedding graph/index nodes, while keeping the wiki as the durable memory layer.

Implementation:

- `internal/search` now builds semantic documents for:
  - wiki page bodies (`kind=wiki_page`);
  - compact graph node cards (`kind=graph_node`) with title, category, tags, sources, outbound links, backlinks, updated time, and body summary;
  - category/global index cards (`kind=graph_index`) derived from the wiki graph.
- `IndexWikiPages` indexes page bodies plus graph/index cards in the same chromem collection, so a query still needs one query embedding but can hit shorter graph summaries.
- Full semantic rebuild now recreates the in-memory collection before adding docs, avoiding stale derived graph/index nodes.
- `ReindexWikiPage` verifies the changed page exists, then refreshes the semantic index because backlinks and category cards can change outside the edited page.
- `search.Result` carries `Kind`; SQLite FTS fallback preserves that metadata.
- `search_memory` maps wiki-page results to `wiki` evidence and graph-derived results to `graph_node` / `graph_index` evidence, keeping the evidence envelope typed.

Verification:

- `go test ./internal/search ./internal/tools`
- `staticcheck ./internal/search ./internal/tools`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: named Aura toolset profiles (`memory_read`, `wiki_review`, `skills_read`, `web_research`, `scheduler_safe`) to cut tool schema/context for scheduled jobs and swarm roles.

### 2026-05-04 - Slice 19b.2 (Embedding config/cache audit)

Goal: verify embeddings are used correctly before optimizing complex-question latency.

Findings:

- Runtime `.env` has separate configured keys for chat (`LLM_API_KEY` via Ollama Cloud), embeddings (`EMBEDDING_API_KEY` / `https://api.mistral.ai/v1` / `mistral-embed`), and OCR (`MISTRAL_API_KEY`).
- `cmd/aura` loads `.env`, overlays dashboard settings from SQLite, and creates the wiki search engine only when `EMBEDDING_API_KEY` is present.
- `createEmbeddingFunc` uses only `EmbeddingBaseURL`, `EmbeddingAPIKey`, and `EmbeddingModel`; there is no fallback from embeddings to `LLM_API_KEY`.
- `search_memory` uses vector search for wiki evidence when the index is ready, and lexical scan for sources/archive. This matches the guardrail: embeddings are retrieval/evidence acceleration, not a second durable memory layer.

Implementation:

- Added `search.EmbedCacheNamespace(baseURL, model)` so the SQLite embedding cache is isolated by provider endpoint plus model.
- `telegram.New` now passes that namespace to `OpenEmbedCache`, preventing stale vector reuse when an operator changes `EMBEDDING_BASE_URL` while keeping the same model name.
- Added tests for namespace normalization and provider isolation.

Verification:

- `go test ./internal/search`
- `go test ./internal/telegram ./internal/config ./internal/settings ./internal/tools`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice remains: extract named Aura toolset profiles for scheduled jobs and swarm roles, then add faster foreground/background answer modes.

### 2026-05-04 - Slice 19b.1 (End-user latency gate)

Goal: make usefulness include time-to-answer, not only eventual correctness.

Trigger: a live end-user challenge run was interrupted because it was taking several minutes. That is a product failure even if the final answer would eventually be correct.

Implementation:

- `cmd/debug_memory_quality -live-llm` now defaults to a 60s hard per-scenario timeout instead of 180s.
- Added `-live-latency-budget` (default 30s).
- Each live scenario records `latency_budget_ms`.
- The live report records:
  - `avg_scenario_ms`;
  - `max_scenario_ms`;
  - `slow_scenarios`;
  - `latency_budget_ms`.
- A scenario over budget gets a quality issue and fails.
- The overall live gate now requires:
  - >=85% pass rate;
  - `search_memory` on every question;
  - no unexpected proposals;
  - zero slow scenarios.

Verification:

- Targeted `go test ./cmd/debug_memory_quality`.
- Targeted `staticcheck ./cmd/debug_memory_quality`.
- Next live E2E should run with a tiny limit first, e.g. `-limit 1 -live-timeout 45s -live-latency-budget 20s`, before expanding.

Next slice remains: extract named Aura toolset profiles for scheduled jobs and swarm roles.

### 2026-05-04 - Slice 19b (Review-gated skill proposals)

Goal: add Hermes-style procedural learning without letting the model mutate skill files directly.

Implementation:

- Added `propose_skill_change`.
- The tool supports create/update/delete skill proposals.
- Create/update validate a complete `SKILL.md` draft using Aura's skill parser.
- Proposals include:
  - skill name/action/description;
  - allowed tools;
  - smoke prompt;
  - full draft content for create/update;
  - reason for delete;
  - provenance/evidence refs, agent job IDs, and swarm IDs.
- Skill proposals reuse `proposed_updates` as the single human review queue.
- `/summaries/.../approve` now skips wiki auto-apply for non-wiki proposal actions, so approving a skill proposal marks it reviewed without writing wiki pages or skill files.
- Scheduled `agent_job` default tools now include `propose_skill_change` while preserving propose-only write policy.
- System prompt now explains that skills are procedural memory and must be proposed, not directly installed/deleted from chat.

Verification:

- `go test ./internal/tools ./internal/api ./internal/conversation/summarizer ./internal/skills ./internal/scheduler ./internal/telegram`
- `staticcheck ./internal/tools ./internal/api ./internal/conversation/summarizer ./internal/skills ./internal/scheduler ./internal/telegram`
- Full Go verification before commit.

Next slice: extract named Aura toolset profiles for scheduled jobs and swarm roles.

### 2026-05-04 - Slice 19a (Code inventory + cleanup)

Goal: start phase 19 with a code/reuse inventory instead of adding low-value dashboard surfaces.

Implementation:

- Added the historical Phase 19 code inventory document, later removed from active docs during cleanup.
- Mapped Aura code areas, dead/legacy findings, and Picobot/Hermes patterns to reuse.
- Removed stale `cmd/debug_swarm` hard-coded `debugAssignments()` after confirming the planner path supersedes it.
- Fixed low-risk staticcheck hygiene in debug/test/client code:
  - unused router assignments in settings tests;
  - literal control char in XLSX tests;
  - direct tool-definition struct conversion;
  - capitalized error strings in debug/client paths.
- Redirected phase 19 from "dashboard/report reader first" to procedural memory and toolsets:
  - `propose_skill_change`;
  - named toolset profiles;
  - skill-backed `agent_job` fields.

Verification:

- Staticcheck targeted cleanup set.
- Full Go verification before commit.

Next slice: implement `propose_skill_change` as review-gated procedural memory.

### 2026-05-04 - Phase 18 closed / Phase 19 opened

Decision: phase 18 is complete.

Why it closes:

- The memory pipeline now has a full evidence/proposal loop:
  - `search_memory` evidence envelope;
  - `memory_decay` maintenance issues;
  - provenance-preserving wiki proposals;
  - batch review;
  - evidence drill-down;
  - live LLM scorecard;
  - graph-ready quality reports.
- The live LLM benchmark is the canonical usefulness check:
  - last full run passed `20/20`;
  - `search_memory_calls=20`;
  - `proposal_calls=4`;
  - `unexpected_proposals=0`.
- The implementation preserves the `docs/llm-wiki.md` philosophy:
  - raw sources/archive turns stay evidence;
  - wiki remains the compiled memory artifact;
  - search remains an access path;
  - autonomous durable updates stay review-gated;
  - report graphs are diagnostics, not a replacement memory layer.

Phase 19:

- Theme: memory quality observability and graph operations.
- First slice: dashboard/report reader for `reports/memory-quality/*.json`.
- Keep real LLM scorecards as the benchmark; do not replace them with fake-LLM metrics.

### 2026-05-04 - Slice 18h (Memory quality report graph)

Goal: keep real LLM metrics as the benchmark while preserving the `docs/llm-wiki.md` philosophy: source/archive evidence feeds compiled memory, and graph structure makes relationships visible.

Implementation:

- Added `-report-dir` to `cmd/debug_memory_quality`.
- Reports are timestamped JSON files containing:
  - generation time and mode (`hermetic` or `live-llm`);
  - summary metrics;
  - full scenario results;
  - graph-ready `nodes` / `edges`.
- The graph captures:
  - scorecard -> scenario;
  - scenario -> tool calls;
  - scenario -> evidence kinds (`source`, `archive`, future `wiki`);
  - scenario -> proposal when a review-gated update is created.
- Added `/reports/memory-quality/` to `.gitignore`; reports are local diagnostic artifacts, not committed source of truth.

Verification:

- `go test ./cmd/debug_memory_quality`
- `go run ./cmd/debug_memory_quality -limit 3 -report-dir <temp>`
- `go run ./cmd/debug_memory_quality -live-llm -limit 3 -report-dir <temp>`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: add a dashboard/report reader for saved memory-quality artifacts and render the report graph without mixing it with durable wiki memory.

### 2026-05-04 - Slice 18g (Live memory routing scorecard)

Goal: verify Aura is useful through the actual LLM/tool loop, not only when tools are called directly by a harness.

Implementation:

- Extended `cmd/debug_memory_quality` with `-live-llm`, `-limit`, and `-live-timeout`.
- Live mode loads `.env`, seeds the same temporary source inbox and conversation archive, and registers only:
  - `search_memory`;
  - `propose_wiki_change`.
- The live scorecard checks:
  - every question calls `search_memory`;
  - expected source/archive evidence appears;
  - durable-memory scenarios call `propose_wiki_change`;
  - answer-only scenarios do not create unexpected proposals;
  - deadline partials are failures, not false passes.
- `propose_wiki_change` now rejects proposals with `origin_tool=search_memory` when no evidence refs are provided, forcing the model to retry with the Evidence envelope.
- Aura's system prompt now explicitly describes `propose_wiki_change` and the evidence requirement for search-backed proposals.

Live debug result after the guardrail:

- `go run ./cmd/debug_memory_quality -live-llm`
- `questions=20 passed=20 routing_pass_rate=100%`
- `search_memory_calls=20 proposal_calls=4 unexpected_proposals=0`
- `llm_calls=44 tool_calls=24 elapsed_ms=559560`

Verification:

- `go test ./cmd/debug_memory_quality ./internal/tools ./internal/conversation`
- `go run ./cmd/debug_memory_quality`
- `go run ./cmd/debug_memory_quality -json`
- `go run ./cmd/debug_memory_quality -live-llm -limit 8`
- `go run ./cmd/debug_memory_quality -live-llm`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: add cheap fake-LLM regression tests for memory routing/retry so the live scorecard remains a periodic diagnostic instead of the only safety net.

### 2026-05-04 - Slice 18f (Memory quality scorecard)

Goal: stop guessing whether Aura's memory is useful by adding a repeatable scorecard built from real daily questions.

Implementation:

- Added `cmd/debug_memory_quality`, a hermetic debug harness for second-brain usefulness.
- Seeds a temporary source inbox with text evidence and OCR-backed PDF evidence.
- Seeds the conversation archive with realistic user preferences, Aura memory policy, weekly planning, and provenance expectations.
- Runs 20 everyday questions through the real `search_memory` tool and checks expected evidence kinds.
- Creates 4 review-gated `propose_wiki_change` proposals for scenarios where memory should grow.
- Scores evidence hit rate, source/archive coverage, proposals created, and proposal quality; emits both readable and JSON reports.

Verification:

- `go test ./cmd/debug_memory_quality`
- `go run ./cmd/debug_memory_quality`
- `go run ./cmd/debug_memory_quality -json`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: run the scorecard through the live LLM/tool loop to catch routing drift, slow tool choices, or missed proposal opportunities.

### 2026-05-04 - Slice 18e (Evidence drill-down)

Goal: make proposal review useful in practice by letting reviewers jump from a proposed memory update to its evidence context.

Implementation:

- Evidence chips in `SummariesPanel` now become links when the evidence kind is actionable:
  - `source` -> `/sources#source-<id>`;
  - `wiki` -> `/wiki/<slug>`;
  - `archive` / `conversation` -> `/conversations#turn-<id>`.
- `SourceInbox` reads `#source-...`, scrolls the visible source row/card into view, and highlights it.
- `ConversationsPanel` reads `#turn-...`, scrolls the visible turn row/card, and opens the conversation drawer automatically.
- Added mocked Playwright E2E for summaries evidence drill-down, including archive evidence opening the drawer.
- Rebuilt embedded dashboard assets in `internal/api/dist`.

Verification:

- `go test ./internal/api ./internal/conversation/summarizer ./internal/tools`
- `npm run lint` in `web/`
- `AURA_DASHBOARD_URL=http://127.0.0.1:4173 npx playwright test e2e/summaries-evidence.spec.ts --project=chromium`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-web.ps1`

Next slice: run real-user proposal review drills against live data and only then decide whether inline evidence preview is worth adding.

### 2026-05-04 - Slice 18d (Batch proposal review)

Goal: make proactive memory growth reviewable at agent scale instead of one click per proposal.

Implementation:

- Added `POST /summaries/batch/approve` and `POST /summaries/batch/reject`.
- Batch endpoints validate/dedupe up to 100 proposal IDs and return both updated proposals and per-ID failures.
- Batch approve preserves the existing behavior: status flips first, then wiki application is attempted and logged if it fails.
- `SummariesPanel` now supports select-all, per-card selection, batch approve/reject, and compact provenance display:
  - origin tool;
  - origin reason;
  - evidence refs with source/page identifiers.
- Updated dashboard API types and English/Italian locale strings.

Verification:

- `go test ./internal/api ./internal/conversation/summarizer`
- `npm run lint` in `web/`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-web.ps1`

Next slice: proposal drill-down/evidence preview, so reviewers can open the source/archive/wiki evidence from the queue before approving.

### 2026-05-04 - Slice 18c (Proposal provenance)

Goal: make proactive wiki growth auditable before adding batch review.

Implementation:

- Added `provenance_json` to `proposed_updates` with idempotent migrations for scheduler startup and direct `ReviewApplier` use.
- `SummariesStore` now round-trips structured provenance:
  - origin tool;
  - origin reason;
  - compact evidence refs (`kind`, `id`, optional title/page/snippet);
  - optional agent job, swarm run, and swarm task IDs.
- `propose_wiki_change` accepts provenance fields and evidence refs, so evidence from `search_memory` can survive into the review queue.
- Review-mode summarizer proposals now mark their origin as `conversation_summarizer` and convert source turn IDs into archive evidence refs.
- `/summaries` API DTOs expose provenance for the dashboard.
- Prompt guidance now asks Aura to include provenance when proposing from `search_memory`, `daily_briefing`, `agent_job`, or AuraBot evidence.

Verification:

- `go test ./internal/conversation ./internal/conversation/summarizer ./internal/tools ./internal/api ./internal/scheduler`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-web.ps1`

Next slice: batch approve/reject endpoints and dashboard controls, now backed by visible proposal provenance.

### 2026-05-04 - Slice 18b (Maintenance memory decay)

Goal: make the nightly maintenance pass notice memory that is getting old, without turning Aura into a parallel RAG cache or silently rewriting wiki pages.

Implementation:

- `wiki.Lint` now emits `memory_decay` issues when a page's `updated_at` exceeds conservative thresholds:
  - medium after 90 days;
  - high after 180 days.
- Decay issues include age and normalized decay score in the message.
- `MaintenanceJob` preserves structured lint kind/severity when enqueueing issues, so `memory_decay` reaches the dashboard/API as its own issue kind.
- No page is auto-updated; decay creates review work in the existing maintenance queue.

Verification:

- `go test ./internal/wiki ./internal/scheduler`

Next slice: proposal provenance + batch review, using decay/source/archive evidence as proposal origins.

### 2026-05-04 - Memory philosophy guardrail

Decision: keep Aura's memory model faithful to `docs/llm-wiki.md`.

- Raw sources stay immutable evidence.
- The wiki stays the durable compiled memory, not a cache over chunks.
- `search_memory` and embeddings are retrieval/evidence accelerators, not a parallel RAG memory layer.
- Archive facts, swarm findings, agent-job outputs, and watcher discoveries should become wiki proposals unless the user explicitly asks for a direct save.
- Skills remain procedural memory; they should encode repeated workflows, not become the place where factual knowledge lives.

Verification: docs-only tracker update; no code tests run.

Next slice remains proposal provenance + batch review, but with this explicit stack: source evidence -> compiled wiki -> reviewed durable update.

### 2026-05-04 - Slice 18a (Memory evidence envelope)

Goal: make memory answers more trustworthy without redesigning the retrieval stack.

Implementation:

- `search_memory` still returns the existing human-readable evidence list for easy LLM scanning.
- The tool now also appends an `Evidence envelope` JSON block with query, typed evidence items, identifiers, titles, roles, OCR page numbers, scores, snippets, and warnings.
- The Aura system prompt now tells the model to preserve that envelope internally and cite it only when the user asks for proof/sources or when evidence materially matters.
- Cleaned legacy roadmap docs so shipped work is marked as shipped instead of dragging the next slice back to already-completed `search_memory`/scheduler/AuraBot activation work.

Verification:

- `go test ./internal/tools ./internal/conversation`
- `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`

Next slice: proposal provenance + batch review, using the structured evidence IDs from `search_memory` as the proposal source trail.

### 2026-05-03 - Slice 17q (Live AuraBot settings apply)

Goal: reduce restart friction after slice 17p by applying safe AuraBot runtime settings immediately.

Implementation:

- `agent.Runner` now exposes thread-safe `Limits` / `UpdateLimits` for max iterations and deadlines.
- `swarm.Manager` now exposes thread-safe `Limits` / `UpdateLimits` for max active workers and depth.
- `POST /settings` can invoke a runtime settings hook after successful persistence.
- Telegram wiring applies AuraBot max-active/max-depth/timeout/max-iterations to the live runner/manager when AuraBot is already enabled.
- `AURABOT_ENABLED` remains restart-required because it changes whether swarm tools are registered.

Verification:

- `go test ./internal/agent ./internal/swarm ./internal/api ./internal/telegram`

Next slice: add a small `/settings` UX hint/toast for “applied live” vs “restart still required”, then run an E2E swarm from the dashboard with changed timeout/max-active.

### 2026-05-03 - Slice 17p (Settings active-vs-saved diagnostics)

Goal: after moving AuraBot tuning into `/settings`, make the page show whether a saved value is actually active in the current process.

Implementation:

- API `SettingItem` now includes `active_value` and `restart_required`.
- `api.Deps` carries the process `RuntimeConfig` snapshot from `telegram.New`.
- `/settings` compares saved/effective-on-next-start values against the current runtime config.
- Settings UI shows a `restart` badge and the active value when a saved DB override differs from the running process.

Verification:

- `go test ./internal/api ./internal/settings ./internal/config ./internal/telegram`
- `npm run lint`
- `npm run build`

Next slice: add an operator-friendly restart/reload action if we want the dashboard to apply restart-required settings without leaving the UI.

### 2026-05-03 - Slice 17o (Dashboard AuraBot settings)

Goal: make AuraBot tuning, especially timeout, manageable from the dashboard so the operator does not edit `.env` for normal changes.

Implementation:

- Moved `AURABOT_*` rows into a dedicated `aurabot` settings group.
- Added visible default values for AuraBot settings when neither DB nor env has a row: enabled=false, max_active=4, max_depth=1, timeout=300, max_iterations=5.
- Updated AuraBot hints to explain restart semantics and DB override behavior.
- Settings UI now shows source guidance for `.env` and default rows: edit + save stores a dashboard override in `aura.db`.
- Header copy now says settings in `aura.db` override `.env` on Aura restart, which matches the runtime config lifecycle.
- Removed one stale i18n type import that was blocking `npm run lint`.

Verification:

- `go test ./internal/api ./internal/settings ./internal/config`
- `npm run lint`
- `npm run build`

Note: `npm run build` refreshed `internal/api/dist`, but those generated assets were already dirty from the parallel frontend/i18n work and were intentionally left unstaged in this slice.

Next slice: add a dashboard restart/reload affordance or effective-runtime diagnostics so the user can see whether saved settings are already active in the running process.

### 2026-05-03 - Slice 17n (AuraBot value timeout)

Follow-up to the live `/swarm` failure: 90 seconds is too small for valuable external research and synthesis.

Implementation:

- Raised `AURABOT_TIMEOUT_SEC` default from 90 to 300 seconds.
- Updated `.env.example` and the local gitignored `.env` runtime value to 300.
- Made Telegram setup fall back to `config.DefaultAuraBotTimeoutSec` instead of a duplicated literal.
- Updated the original AuraBot swarm design doc timeout example.

Verification:

- `go test ./internal/config ./internal/telegram ./internal/agent ./internal/swarm ./internal/swarmtools`
- `go run ./cmd/debug_swarm -json`

Next slice: run a live `trading-signals-test` again after restart and inspect `/swarm` metrics to tune whether 300s is enough or if background jobs need asynchronous completion notifications.

### 2026-05-03 - Slice 17m (AuraBot completion guardrails)

Live `/swarm` diagnosis from `logs/aura-2026-05-03.log` and the dashboard screenshot:

- `trading-signals-test` failed after `90021 ms wall`.
- The task was a single `researcher` worker.
- Logs showed two waves of repeated parallel `web_search` calls before the final `agent runner: llm send: context deadline exceeded`.
- Because the runner returned an error, the store only preserved `last_error`; UI metrics stayed at `llm=0`, `tools=0`, `ms=0`, hiding the work that had actually happened.

Implementation:

- `internal/agent.Runner` now supports per-task `MaxToolCalls`, `MaxToolResultChars`, and `CompleteOnDeadline`.
- When a worker reaches its tool budget, the next LLM turn is forced to synthesize with no tools exposed.
- Tool results can be clipped per task, so a researcher cannot push many 8KB search outputs back into the final context.
- If a deadline still happens after evidence was gathered, AuraBot returns a partial evidence report and metrics instead of failing the task to zero.
- `swarm.BuildPlan` and `spawn_aurabot` assign conservative role budgets; the researcher is capped at 3 tool calls and 1800 chars per tool result.
- Researcher prompts now explicitly prefer at most two targeted searches before deciding whether one fetch is worth it.

Verification:

- `go test ./internal/agent ./internal/swarm ./internal/swarmtools`
- `go run ./cmd/debug_swarm -json`: completed `5/5`, failed `0`, `wall_ms=769`, `task_elapsed_ms=1658`, `speedup=2.16`, `llm_calls=10`, `tool_calls=5`, `tokens_total=1293`; public tool path completed `3/3`, failed `0`.
- `go test ./...`
- `go build ./...`
- `go vet ./...`

Next slice: add a user-facing completion notification path for AuraBot runs so scheduled/background swarm work tells the user when the dashboard result is ready.

### 2026-05-03 - Slice 17l (Run scheduled routines now)

Goal: fix the live-test drift where "Prova ad eseguirlo adesso" after scheduling an `agent_job` routed to `spawn_aurabot` and repeated web searches instead of executing the saved routine.

Acceptance:

- Add a tool or command path named `run_task_now` / `run_agent_job_now` that accepts a scheduled task name.
- For `agent_job`, load the stored task, normalize its existing payload, run it through the same bounded `agent.Runner` path used by the scheduler dispatcher, and preserve propose-only write policy.
- Respect the task's existing `notify` behavior: if enabled and a recipient is known, send the completion summary at the end.
- Return clear status, elapsed time, LLM/tool counts, token metrics, and any last error.
- Natural-prompt smoke: "esegui adesso <task-name>" must select this path, not `spawn_aurabot`.
- Keep the first slice backend/Telegram/tool-only; dashboard buttons can follow after the behavior is proven.

Non-goals for the first pass:

- Do not redesign the scheduler.
- Do not add direct wiki writes for jobs.
- Do not broaden AuraBot permissions beyond the saved job's safe allowlist.

Implementation:

- Added `run_task_now`, backed by `Bot.RunTaskNow`, for saved scheduled tasks.
- MVP supports `agent_job` tasks only and rejects cancelled/non-agent rows with clear errors.
- Reuses the stored `agent_job` payload, normalized safe allowlist, propose-only write policy, and existing notification behavior.
- Records manual run time and last error without disturbing the future schedule.
- Extended `cmd/debug_ingest` with a natural prompt that selects `run_task_now` after creating a scheduled agent job.

Verification:

- `go test ./internal/scheduler ./internal/tools ./internal/telegram ./cmd/debug_ingest`
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `go run ./cmd/debug_ingest`: passed all 16 scenarios; `run_task_now` selected, `tool_calls=1`, `elapsed_ms=8859`.

### 2026-05-03 - Slice 17k.1 (Log-driven agent drift fixes)

Audit of `logs/aura-2026-05-03.log` from the user's live tests.

Findings:

- `Prova ad eseguirlo adesso` after scheduling a job routed to `spawn_aurabot`, not to the scheduled job. The spawned researcher issued repeated parallel `web_search` calls and hit the 90s AuraBot timeout.
- `Si ma dovrebbe mandare il riepilogo quando termina` scheduled two independent tasks instead of linking completion notification to the agent job.
- The summarizer extracted useful trading facts, but dropped them because the LLM returned valid JSON wrapped in a ```json fence.
- `write_wiki` failed once with `too many tags (max 10)`, then recovered only after a long retry.
- The app logs did not show `search_memory`, which means the running bot had not yet picked up slice 17k.

Implementation:

- `internal/conversation/summarizer/scorer.go` now strips a single fenced JSON wrapper before parsing.
- Added a regression test for fenced summarizer JSON.
- `write_wiki` tool schema now advertises `maxItems: 10` for tags and sources.
- Structured tool errors now give a specific retry hint for too many wiki tags/sources.

Verification:

- `go test ./internal/conversation/summarizer ./internal/tools ./internal/wiki`

Next slice: add a real `run_task_now` / `run_agent_job_now` path so "eseguilo adesso" runs the saved scheduled routine and can notify on completion instead of improvising with `spawn_aurabot`.

### 2026-05-03 - Slice 17k (Unified memory evidence search)

Implementation slice to make Aura answer real daily questions from the full local second brain instead of forcing the model to guess which store to inspect first.

- Added `search_memory`, a read-only evidence tool across:
  - wiki vector search when the wiki index is ready;
  - source inbox text/OCR with local lexical ranking;
  - conversation archive, optionally restricted by `chat_id`.
- Evidence items include typed identifiers (`[[slug]]`, `src_*`, `conversation:<id>`), compact snippets, scores, and `page=N` when the source OCR heading identifies a matching PDF page.
- Wired the tool after the conversation archive is opened so it can still work with sources/wiki when archive is disabled.
- Added `search_memory` to scheduled agent-job defaults/safe allowlist and AuraBot librarian/critic/synthesizer read-only presets.
- Updated the system prompt to prefer `search_memory` for "what do you know/remember?", prior-context, and source-backed questions while keeping `search_wiki` for wiki-only lookup.
- Extended `cmd/debug_ingest` with a natural-prompt `search_memory` scenario for OCR evidence (`gold-742`, `page=1`).
- Live E2E result: PASS, `search_memory` selected, `elapsed_ms=7970`, `tool_calls=1`; full `cmd/debug_ingest` passed 15/15 scenarios.
- Verification:
  - `go test ./internal/tools ./internal/swarm ./internal/swarmtools ./internal/scheduler ./internal/telegram ./cmd/debug_ingest ./cmd/debug_swarm`
  - `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`
  - `go run ./cmd/debug_ingest`

Next slice: proposal provenance and batch review so evidence found by `search_memory` can become review-gated wiki growth with visible source links.

### 2026-05-03 - Implementation loop tool

Added a project-local Ralph-style loop pack for Aura implementation work.

- New loop package: `loops/aura-implementation/RALPH.md`.
- New verification scripts:
  - `loops/aura-implementation/scripts/status.ps1`
  - `loops/aura-implementation/scripts/verify-go.ps1`
  - `loops/aura-implementation/scripts/verify-web.ps1`
- Decision: use a local, inspectable Ralph-style package rather than adopting a heavyweight orchestrator. It keeps each slice fresh-context, verified, tracked, and atomically committed while preserving Aura's existing workflow.
- Verification: `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\status.ps1`; `powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1`.

### 2026-05-03 - Inventory: Aura + Picobot + Hermes

Decision record for the next proactivity phase: compare Aura's current standalone second-brain implementation with Picobot's local-agent runtime patterns and Hermes Agent's self-improving skills/cron/subagent model.

- Added `docs/aura-picobot-hermes-inventory-2026-05-03.md`.
- Conclusion: keep Aura as the product core; adapt Picobot's stateless background-context and runtime guardrails; adapt Hermes' skill-backed jobs, isolated subagents, and procedural-memory loop through Aura's review queue.
- Recommended next slice: `search_memory` unified evidence search across wiki + sources + archive, followed by proposal provenance/batch review, then review-gated `propose_skill_change`.
- No code changes in this inventory slice.

### 2026-05-03 - Slice 17j (Daily briefing utility check)

Implementation slice to make the "real daily questions" audit executable instead of just a product note.

- Added `daily_briefing`, a read-only tool for "what needs attention today?" that composes:
  - active tasks due before end-of-day, including overdue labels;
  - pending wiki proposals from the review queue;
  - open wiki maintenance issues;
  - recent source inbox rows and failures;
  - recent conversation archive turns from the current day.
- Wired the tool into the Telegram registry after scheduler/source/summaries/issues/archive stores are available.
- Updated the system prompt so "briefing / oggi / cosa devo fare" routes to the dedicated tool.
- Extended `cmd/debug_ingest` with a seeded, realistic daily-briefing scenario:
  - Prompt: `Dammi il briefing di oggi in 5 punti. Usa il briefing giornaliero se disponibile.`
  - Live result: PASS, `daily_briefing` selected, `elapsed_ms=9759`, `tool_calls=1`.
- Verification:
  - `go test ./internal/tools ./internal/conversation ./internal/telegram ./cmd/debug_ingest`
  - `go test ./...`
  - `go run ./cmd/debug_ingest` (14/14 scenarios PASS)

Next slice: scorecard/harness for the remaining daily questions, then skill-draft proposals in the Hermes style.

### 2026-05-03 - Slice 17i (Scheduled agent jobs + lint cleanup)

Implementation slice to convert "remind me to do the routine" into "run a bounded routine for me" while keeping durable writes review-gated.

**Implementation**:

- Added scheduler task kind `agent_job`.
- Added normalized `AgentJobPayload`: accepts a plain-text goal or JSON with `goal`, `tool_allowlist`, `write_policy`, and `notify`.
- Default write policy is `propose_only`; unsupported direct-write policies are rejected.
- Default agent-job tools are read/proposal oriented: wiki/source read tools, web read tools, and `propose_wiki_change`.
- `schedule_task` now accepts `kind="agent_job"` and stores normalized payload JSON; Telegram user ID is captured for optional completion notifications.
- API `POST /tasks` and dashboard task creation now accept `agent_job`.
- Telegram scheduler dispatcher runs `agent_job` through the shared bounded `agent.Runner`, logs LLM/tool/token/elapsed metrics, filters unsafe requested tools such as `write_wiki`, and notifies the recipient when configured.
- Fixed global frontend lint:
  - Renamed Playwright fixture callback parameter so React Hooks lint no longer sees it as a `use` hook.
  - Removed synchronous `setState` effect in `SwarmPanel` by deriving the effective selected run ID.
- Rebuilt embedded dashboard assets.

**E2E metrics**:

- `schedule_agent_job`: PASS, `elapsed_ms=3085`, `tool_calls=1`, scheduled `slice17-agent-smoke` as `agent_job` every 60 minutes.
- Full `cmd/debug_ingest`: 13/13 scenarios passed against `glm-5.1:cloud` via `https://ollama.com/v1`.

**Verification**:

- `npm run lint -- --max-warnings=0`
- `npm run build`
- `go test ./internal/scheduler ./internal/tools ./internal/api ./internal/telegram ./cmd/debug_ingest`
- `go run ./cmd/debug_ingest`
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `$env:PATH='D:\tmp\w64devkit\bin;' + $env:PATH; go test -race ./...`

**Next work**:

- Slice 17j: runtime activation sanity. Restart Aura, verify boot log shows `AuraBot swarm enabled`, and add an effective-settings/debug check if DB overrides still do not match runtime.

### 2026-05-03 - Slice 17h (Daily recurrence parity)

Implementation slice from the historical daily-questions gap audit to fix the real "giorni feriali alle 10" gap without introducing full cron or autonomous agent jobs yet.

**Implementation**:

- Added `schedule_weekdays` to `scheduled_tasks` with idempotent migration and in-memory `Task.ScheduleWeekdays`.
- Added weekday parsing/canonicalization: `mon,tue,wed,thu,fri,sat,sun`, plus shortcuts like `weekdays`, `business`, `feriali`, and `weekend`.
- Added `NextDailyRunOnWeekdays`; legacy `NextDailyRun` remains the every-day wrapper.
- Scheduler recurrence advancement now respects weekday filters for daily tasks.
- `schedule_task` now supports `every_minutes` and optional `weekdays` with `daily`.
- API `POST /tasks` and task DTOs now accept/return weekday filters.
- Dashboard Tasks panel can create and display daily tasks narrowed to selected weekdays.
- `cmd/debug_ingest` now includes natural-prompt scenarios for `every_minutes` and business-day scheduling, and prints `elapsed_ms` + `tool_calls` per scenario.

**E2E metrics**:

- `schedule_task_every_minutes`: PASS, `elapsed_ms=2960`, `tool_calls=1`, created `slice17-every-smoke` every 60 minutes.
- `schedule_task_weekdays`: PASS, `elapsed_ms=18788`, `tool_calls=1`, created `slice17-weekday-smoke` with `mon,tue,wed,thu,fri`.
- Full `cmd/debug_ingest`: 12/12 scenarios passed against `glm-5.1:cloud` via `https://ollama.com/v1`.

**Verification**:

- `go test ./internal/scheduler ./internal/tools ./internal/api ./internal/conversation ./cmd/debug_ingest`
- `npm run build`
- `npx eslint src/components/TasksPanel.tsx src/types/api.ts`
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `$env:PATH='D:\tmp\w64devkit\bin;' + $env:PATH; go test -race ./...`
- Note: global `npm run lint -- --max-warnings=0` still fails on pre-existing files `web/e2e/fixtures.ts` and `web/src/components/SwarmPanel.tsx`; modified frontend files lint clean.

**Next work**:

- Slice 17i: `agent_job` scheduled task kind, propose-only write policy, and metrics. This is the actual jump from "remind me" to "run the bounded routine for me".

### 2026-05-03 - Slice 17g (Proactive wiki proposals)

Implementation slice to make Aura more proactive while preserving human review for durable mutations.

**Implementation**:

- Added `SummariesStore.Propose`: validated insert into the existing `proposed_updates` review queue.
- Added LLM tool `propose_wiki_change`: creates `new` or `patch` wiki proposals with category, related slugs, optional source turn IDs, confidence, and current user/chat ID when available.
- Wired `propose_wiki_change` in Telegram on the shared scheduler SQLite DB, reusing the same store as dashboard `/summaries`.
- Added a conditional proactive prompt block: shown only when `propose_wiki_change` is registered, encouraging compact reviewable proposals after useful discoveries while avoiding secrets/raw logs/temporary state.
- Kept direct wiki mutation unchanged: `write_wiki` still exists for explicit user save/remember requests; proactive growth goes through review.

**Verification**:

- `go test ./internal/conversation ./internal/conversation/summarizer ./internal/tools ./internal/telegram`
- `go run ./cmd/debug_swarm -json`: main planner run completed `5/5` tasks with `speedup≈1.99x`; public tool path completed `team_calls=1`, `runs=1`, `tasks=3`, `completed=3`, `failed=0`, `list_calls=1`, `read_calls=3`.
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `$env:PATH='D:\tmp\w64devkit\bin;' + $env:PATH; go test -race ./...`

**Next work**:

- Slice 17h: optionally surface proposal origin/run metadata in the dashboard so swarm-generated proposals are easy to trace back to the run/task that suggested them.

### 2026-05-03 - Slice 17f (AuraBot conservative routing)

Implementation slice using this chat as orchestrator plus one read-only explorer agent.

**Implementation**:

- Added `internal/conversation/swarm_prompt.go`: stable AuraBot routing prompt plus conservative per-turn heuristic for broad read-only second-brain requests.
- The routing prompt is conditional: Telegram appends it only when both a swarm manager exists and the `run_aurabot_swarm` tool is registered.
- Added a per-turn hint for prompts like broad wiki/source/skill audits, synthesis, planning, and "cosa manca" checks.
- Kept simple lookups on direct tools and mutation-oriented prompts off the swarm hint path.
- Tightened `run_aurabot_swarm` / `spawn_aurabot` descriptions so the LLM prefers the team tool for multi-role investigations but understands it is read-only.
- Captured `userText` once in `handleConversation` and reused it for prompt routing, speculative wiki search, echo fallback, logging, and archiving.

**Verification**:

- `go test ./internal/conversation ./internal/telegram ./internal/swarmtools ./internal/agent`
- `go run ./cmd/debug_swarm -json`: main planner run completed `5/5` tasks with `speedup≈2.15x`; public tool path completed `team_calls=1`, `runs=1`, `tasks=3`, `completed=3`, `failed=0`, `list_calls=1`, `read_calls=3`.
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `$env:PATH='D:\tmp\w64devkit\bin;' + $env:PATH; go test -race ./...`

**Next work**:

- Slice 17g: review-gated proposal queue for wiki/skill mutations. Keep actual writes human-approved; no autonomous mutation role yet.

### 2026-05-03 - Slice 17e (AuraBot planner + synthesis)

Implementation slice using this chat as orchestrator with parallel worker agents.

**Implementation**:

- Added `internal/swarm/plan.go`: deterministic read-only planner with `BuildPlan`, `PlanAssignments`, and `SynthesizeRunResult`.
- Planner roles: `librarian`, `critic`, `researcher`, `skillsmith`, `synthesizer`.
- Planner behavior: trims/dedupes roles, rejects unknown roles, caps assignment count, creates focused prompts/system prompts, and uses read-only allowlists only.
- Added deterministic synthesis: task counts, LLM/tool calls, prompt/completion/total tokens, task elapsed, wall time, speedup, task previews, and a compact summary string. No second LLM call required.
- Added public LLM tool `run_aurabot_swarm`: accepts a high-level goal plus optional role subset, builds the plan, executes assignments via `swarm.Manager`, and returns synthesis JSON.
- Kept `spawn_aurabot` intact as the single-worker primitive.
- Registered `run_aurabot_swarm` behind the same `AURABOT_ENABLED && client != nil` gate.
- Updated `cmd/debug_swarm` to use `swarm.BuildPlan` for its main run and to exercise the public team tool path: `run_aurabot_swarm` → `list_swarm_tasks` → `read_swarm_result`.
- Added `*.log` to `.gitignore` so local runtime logs from debug/dev runs do not appear as untracked worktree noise.

**Debug metrics**:

- `go run ./cmd/debug_swarm -json`: main planner run completed `5/5` tasks with `speedup≈2.18x`; public tool path completed `team_calls=1`, `runs=1`, `tasks=3`, `completed=3`, `failed=0`, `list_calls=1`, `read_calls=3`.

**Verification**:

- `go test ./internal/swarm ./internal/swarmtools ./internal/telegram ./cmd/debug_swarm`
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `$env:PATH='D:\tmp\w64devkit\bin;' + $env:PATH; go test -race ./...`

**Next work**:

- Slice 17f: proposal queue for wiki/skill mutations. Keep actual writes review-gated; no autonomous file/wiki/skill writes yet.

### 2026-05-03 - Slice 17d (AuraBot swarm observability)

Follow-up slice after `slice 17: add AuraBot swarm MVP` commit `32abb88`.

**Implementation**:

- Added `Store.ListRuns(ctx, limit)` to `internal/swarm`, returning newest runs first with a 200-row hard cap.
- Added read-only API routes:
  - `GET /swarm/runs?limit=50`
  - `GET /swarm/runs/{id}`
  - `GET /swarm/tasks/{id}`
- Added API DTOs for run summaries/details and task rows, including task counts, wall time, summed task elapsed, speedup, LLM/tool calls, token totals, per-task allowlists, result text, and errors.
- Wired `api.Deps.Swarm` from `telegram.New`. The swarm store now opens on the shared SQLite DB even when `AURABOT_ENABLED=false`; only runner/tools stay gated. This keeps historical observability available without enabling new workers.
- Added dashboard route `/swarm`, sidebar entry, keyboard shortcut `g a`, typed API client methods, and `SwarmPanel`.
- Rebuilt the embedded React dashboard into `internal/api/dist`.

**Verification**:

- `go test ./internal/swarm ./internal/api ./internal/telegram`
- `npm run build` from `web/`
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `$env:PATH='D:\tmp\w64devkit\bin;' + $env:PATH; go test -race ./...`

### 2026-05-03 - Slice 17c (AuraBot LLM tools + E2E metrics)

Third implementation slice from the historical AuraBot swarm design.

**Implementation**:

- Added `AURABOT_ENABLED`, `AURABOT_MAX_ACTIVE`, `AURABOT_MAX_DEPTH`, `AURABOT_TIMEOUT_SEC`, and `AURABOT_MAX_ITERATIONS` to env config, runtime settings, and the dashboard settings catalog.
- Wired AuraBot behind `AURABOT_ENABLED`: when enabled and an LLM client exists, `telegram.New` creates a shared-DB `swarm.Store`, bounded `agent.Runner`, and `swarm.Manager`, then registers `spawn_aurabot`, `list_swarm_tasks`, and `read_swarm_result`.
- Added `internal/swarmtools`, keeping the public LLM tools out of `internal/tools` to avoid an import cycle. MVP role presets are read-only (`librarian`, `critic`, `researcher`, `synthesizer`, `skillsmith`) and `spawn_aurabot` supports `mode=wait`.
- Extended `swarm_tasks` with token telemetry columns (`tokens_prompt`, `tokens_completion`, `tokens_total`) and an idempotent migration for existing DBs.
- Added `cmd/debug_swarm`, a hermetic no-network E2E harness that drives the real runner/manager/tool path with fake read-only wiki/source/skill tools and fake LLM responses.
- Updated `.env.example` and local `.env` with the new gate defaults.

**Debug metrics**:

- `go run ./cmd/debug_swarm`: 6 tasks completed, `wall_ms=824`, `task_elapsed_ms=1994`, `speedup=2.42x`, `max_active=3`, `llm_calls=12`, `tool_calls=6`, `tokens_total=792`, `spawn_aurabot_json=true`, `swarmtools_list_json=true`, `swarmtools_read_json=true`.
- `go run ./cmd/debug_swarm -max-active 1`: 6 tasks completed serially, `wall_ms=2176`, `speedup=0.90x`, `max_active=1`. This confirms the harness can see the parallelism delta.
- `go run ./cmd/debug_swarm -json`: emitted the same metrics as structured JSON for future CI/log scraping.

**Verification**:

- `go test ./cmd/debug_swarm ./internal/config ./internal/settings ./internal/api ./internal/agent ./internal/swarm ./internal/swarmtools ./internal/telegram`
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `$env:PATH='D:\tmp\w64devkit\bin;' + $env:PATH; go test -race ./internal/agent ./internal/swarm ./internal/swarmtools`
- `$env:PATH='D:\tmp\w64devkit\bin;' + $env:PATH; go test -race ./...`

**Files touched**:

- `.env.example`
- `.env` (gitignored runtime config)
- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/settings/applier.go`
- `internal/api/settings.go`
- `internal/telegram/bot.go`
- `internal/telegram/setup.go`
- `internal/swarm/types.go`
- `internal/swarm/store.go`
- `internal/swarm/store_test.go`
- `internal/swarmtools/tools.go`
- `internal/swarmtools/tools_test.go`
- `cmd/debug_swarm/main.go`
- `docs/implementation-tracker.md`

**Next work**:

- Slice 17d: dashboard observability for swarm runs/tasks plus review/approval controls before any write-capable role.
- Later: durable skill proposals and skill creation workflows, still gated behind review/admin paths.

### 2026-05-03 - Slice 17b (AuraBot swarm store + manager)

Second implementation slice from the historical AuraBot swarm design.

**Implementation**:

- Added `internal/swarm` with typed run/task statuses and assignment models.
- Added SQLite-backed `Store` with `OpenStore`, `NewStoreWithDB`, run lifecycle, task lifecycle, and list/read helpers.
- Store uses the same shared-DB style as other Aura packages: `NewStoreWithDB(db *sql.DB)` does not own/close the DB, while `OpenStore(path)` does.
- SQLite writes are serialized via `SetMaxOpenConns(1)` plus `PRAGMA busy_timeout = 5000`, so parallel AuraBot execution does not create `SQLITE_BUSY` churn.
- Added `Manager` that persists a run, persists all assignments, executes valid assignments concurrently behind `MaxActive`, rejects over-depth assignments without running them, and marks the run failed if any task fails.
- No Telegram wiring, env config, public `spawn_aurabot`, or dashboard surface yet.

**Test coverage**:

- Store run/task lifecycle including telemetry persistence.
- Reopen persistence.
- Shared DB close ownership.
- Manager executes multiple assignments and persists completed results.
- `MaxActive` caps concurrent runner calls.
- `MaxDepth` rejects too-deep assignments without running them.
- Runner errors mark task and run failed.
- Manager constructor and empty assignment validation.

**Verification**:

- `go test ./internal/agent ./internal/swarm`
- `go test ./internal/swarm`
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `$env:PATH='D:\tmp\w64devkit\bin;' + $env:PATH; go test -race ./internal/agent ./internal/swarm`

**Files touched**:

- `internal/swarm/types.go`
- `internal/swarm/store.go`
- `internal/swarm/manager.go`
- `internal/swarm/store_test.go`
- `internal/swarm/manager_test.go`
- `docs/implementation-tracker.md`

**Next work**:

- Slice 17c: add config gates and LLM-facing tools `spawn_aurabot`, `list_swarm_tasks`, `read_swarm_result` behind `AURABOT_ENABLED`.
- Slice 17d: role presets (`librarian`, `critic`, `researcher`) with read-only tool allowlists.

### 2026-05-03 - Slice 17a (AuraBot bounded runner)

First implementation slice from the historical AuraBot swarm design.

**Implementation**:

- Added `internal/agent.Runner`, a small background-agent loop that is deliberately independent from Telegram streaming, placeholders, archiving, and budget UI.
- Runner accepts isolated prompts/messages, model, temperature, timeout, tool allowlist, and optional user id.
- Tool definitions are filtered before the LLM call, and tool execution re-checks the allowlist so hallucinated hidden tools are blocked.
- Tool calls execute concurrently but tool result messages are appended in original order, matching the production Telegram loop's pairing discipline.
- Empty allowlist means no tools. There is no default "all tools" path.
- Added per-tool timeout so a slow MCP/web-style call cannot stall the whole worker.

**Test coverage**:

- Text-only final response.
- One tool-call loop with user-id propagation.
- Tool definition filtering and blocked disallowed execution.
- Per-tool timeout returns structured error content.
- Max-iteration fallback includes last tool result.
- Prompt/messages validation.
- Constructor rejects nil LLM.

**Verification**:

- `go test ./internal/agent`
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `$env:PATH='D:\tmp\w64devkit\bin;' + $env:PATH; go test -race ./internal/agent`

**Files touched**:

- `internal/agent/runner.go`
- `internal/agent/runner_test.go`
- historical AuraBot swarm design doc
- `docs/implementation-tracker.md`

**Next work**:

- Slice 17b: SQLite `swarm_runs` / `swarm_tasks` store plus `SwarmManager` active/depth limits.
- Slice 17c: `spawn_aurabot`, `list_swarm_tasks`, `read_swarm_result` behind `AURABOT_ENABLED`.

### 2026-05-03 — Phase 16: Engine Quality & Performance

Five slices executed via subagent-driven development (fresh agent per slice). Two workstreams:

**Error Recovery (16a-16b):** Tool errors are now structured JSON with retryable/hint fields instead of plain `"(tool error)"` strings. System prompt teaches the LLM to self-correct on retryable errors. Commits `1ec8c5b` + `1800c76`.

**Latency (16c-16e):** Immediate "⏳" placeholder on Telegram removes the empty wait before first token. EnforceLimit summarization deferred to background after response delivery. Edit throttle tightened 800→600ms. Commits `254f394` + `abea01f` + `4a78c0f`.

**Quality gates:** `go build/vet/test ./...` all green. Ready for live Telegram smoke to verify placeholder appears instantly and streaming feels responsive at 600ms.

**Deferred to Phase 17:** Dashboard/UI polish + E2E test coverage expansion.

### 2026-05-03 - Slice 12u.9 (HR-02 proposal category + related slugs)

Fixes Phase 12 review HR-02. Review-mode summarizer proposals now round-trip `Candidate.Category` and `Candidate.RelatedSlugs` through `proposed_updates` and restore them when approving a proposal.

**Implementation**:

- Added `category` and `related_slugs` columns to `proposed_updates`, with idempotent backfill migrations for both scheduler startup and direct `NewReviewApplier` use.
- `ReviewApplier.Apply` persists category and JSON-encoded related slugs.
- `SummariesStore` scans the new fields; API DTOs and TS types expose them.
- `handleSummariesApprove` reconstructs the `Decision` with the proposal category and related slugs instead of hardcoding category `fact`.
- `AutoApplier` now writes related slugs to new pages and merges them into patched pages.

**Test coverage**:

- Extended review applier tests to assert category/related persistence and legacy-table migration.
- Extended approve tests to assert the wiki page receives the original category and related slugs.
- Added scheduler migration coverage for legacy `proposed_updates` tables.

**Next work**: no HIGH items from the Phase 12 review backlog remain open; next slice should come from the current product backlog rather than the historical review doc.

### 2026-05-03 - Slice 12u.8 (HR-01 RepairLink partial-commit)

Fixes Phase 12 review HR-01. `wiki.Store.RepairLink` no longer aborts the whole auto-fix pass on the first page-level read/write failure. It now accumulates per-page errors, continues scanning later pages, writes the `auto-fix` audit log unconditionally, and returns a joined summary error when any page failed.

**Test coverage**:

- Added `TestRepairLinkContinuesAfterWriteFailure`.
- The test creates three pages referencing `[[broken-link]]`, corrupts the middle page so it is readable but invalid on rewrite, runs `RepairLink`, and verifies the first and third pages are repaired while the returned error names the bad page and `log.md` includes `auto-fix`.

**Follow-up**: HR-02 landed immediately after as 12u.9.

### 2026-05-03 - Phase 15 slice 15e (natural file-creation smoke)

Closes the file-creation milestone's remaining validation gap. The earlier `cmd/debug_xlsx`, `cmd/debug_docx`, and `cmd/debug_pdf` harnesses prove each tool when called directly; `cmd/debug_files` proves the LLM can choose the right tool from normal user language.

**Implementation**:

- Added `cmd/debug_files/main.go`.
- Loads `.env`, requires `LLM_API_KEY`, and respects `LLM_BASE_URL` / `LLM_MODEL`.
- Creates a hermetic temp wiki/source store and registers only `create_xlsx`, `create_docx`, and `create_pdf`.
- Runs three natural prompts: spreadsheet budget, editable Word memo, printable PDF invoice summary.
- Verifies each scenario called the expected tool, returned JSON with `source_id`, wrote the expected `original.*` asset, marked the source `ingested`, and invoked the delivery stub exactly once.

**Live smoke**: `go run ./cmd/debug_files` passed all 3 scenarios on 2026-05-03 using `glm-5.1:cloud` via `https://ollama.com/v1`.

**Follow-up**: Phase 15 MVP is closed. The v0.12.1 review backlog was also closed in 12u.8/12u.9; next implementation work should use the current product backlog.

### 2026-05-02 — Phase 15 slice 15c (`create_pdf` tool + Telegram delivery)

Closes the file-creation milestone's MVP. Aura now produces three formats from one structured DSL: xlsx via excelize, docx via hand-rolled OOXML, pdf via fpdf. Block grammar is identical across docx + pdf — same heading/paragraph/bullet/table shape — so the LLM picks the right `create_*` tool by user intent (spreadsheet vs editable doc vs printable doc) without re-learning anything per format.

**Why fpdf vs headless Chrome**: fpdf is a single Go dep, no Chrome runtime, identical "structured spec → bytes" pattern as 15a/15b. Headless-Chrome (`chromedp`) would let us render Markdown → HTML → PDF for prettier output but stacks a 100+ MB Chrome dependency, breaks the self-contained binary story, and is overkill for the "memo / report / invoice" workflow this slice targets. If anyone later asks for HTML-rendered output, a 15c.2 follow-up can add a `create_pdf_html` tool alongside this one.

**Why `KindPDFGen` distinct from `KindPDF`**: a generated PDF has no `ocr.md`, never went through Mistral, and shouldn't be a candidate for the `ingest_source` LLM tool. `KindPDF` keeps its meaning ("uploaded by user, ran through OCR"); `KindPDFGen` marks Aura-authored output. The on-disk file (`original.pdf`) and the dashboard download (`application/pdf` + `inline`) are identical — the kind alone branches the OCR/ingest pipeline.

**Latin-1 sanitization gotcha**: fpdf's bundled fonts (the 14 PDF base fonts) only support cp1252 / Latin-1. Realistic LLM output regularly contains curly quotes (`"…"`, `'…'`), em-dashes (`—`), ellipses (`…`), and NBSP — all outside cp1252. fpdf would crash at write time. `latin1Sanitize` maps these to ASCII equivalents (`"`/`'`/`-`/`...`/space) before any cell or paragraph reaches fpdf. Anything else above 0xFF falls back to `?`. Bullet `•` is in cp1252 so it survives intact (the bullet rendering already used it).

**Files**:

- `internal/files/pdf.go`, `internal/files/pdf_test.go` (new — 9 tests including a dedicated `TestLatin1Sanitize`).
- `internal/tools/files.go` — `CreatePDFTool` next to `CreateXLSXTool` + `CreateDOCXTool`. Same parallel structure (modest persist+deliver duplication; will refactor into a `persistAndDeliver` helper if a 4th file format ever lands).
- `internal/tools/files_test.go` — 5 pdf tool tests.
- `internal/source/source.go`, `internal/source/store.go` — `KindPDFGen` constant + `.pdf` extension (shared with KindPDF; only Kind disambiguates).
- `internal/api/sources.go` — `rawAssets[KindPDFGen]` row + `validKind` extension.
- `internal/api/router_test.go` — `TestSourceRaw_AllSupportedKinds` extends slice 15b's test to 5 kinds: PDF/XLSX/DOCX/PDFGen/text.
- `internal/telegram/setup.go` — `CreatePDFTool` registration after `CreateDOCXTool`.
- `web/src/types/api.ts` — kind union extended to include `'pdf_generated'`.
- `web/src/components/SourceInbox.tsx` — Download button shows for pdf_generated too.
- `cmd/debug_pdf/main.go` (new) — 5-scenario hermetic harness mirroring `cmd/debug_xlsx` and `cmd/debug_docx`. `go run ./cmd/debug_pdf` runs all in <1 s; `-out <path>` writes the PDF to disk.
- `go.mod` / `go.sum` — `github.com/go-pdf/fpdf v0.9.0` (zero transitive deps).

**Quality gates**: `go build/vet/test ./...` all green. `go run ./cmd/debug_pdf` 5/5. Visual check: `D:/tmp/aura-debug-q1-report.pdf` (1602 bytes) opens cleanly with title + paragraphs + headings + bullets + 3-row table. tsc + vite build clean (358 KB main / 112 KB gz).

**Phase 15 MVP + 15e complete**: three file-creation tools (xlsx + docx + pdf), shared `DocumentSender` interface, dashboard download, sha256 dedup, and natural-prompt smoke coverage. Remaining optional follow-ups: `persistAndDeliver` helper if a 4th format adds more duplication, optional 15c.2 HTML-rendered PDFs via headless Chrome.

### 2026-05-02 — Phase 15 slice 15b (`create_docx` tool + Telegram delivery)

Second slice of Phase 15. Aura now produces both spreadsheets and Word documents. Same surfaces as 15a (Telegram delivery + dashboard download) — every plumbing detail except the format-specific generator was already in place.

**Why pure-Go OOXML over a library**: `unidoc/unioffice` requires UNI Cloud API keys for some flows; other DOCX libs are template-driven (need a base .docx with placeholders), which doesn't fit Aura's "LLM authors structured content from JSON" shape. A basic DOCX is just a ZIP with three small XML parts — `~250 LOC` here gets us heading/paragraph/bullet/table without any dep risk and identical security posture (no embedded macros possible because we never write a `vbaProject.bin`).

**Visual styling without /word/styles.xml**: heading blocks use direct run formatting (`<w:b/>` + `<w:sz w:val="36"/>` for H1=18pt, down to `<w:sz w:val="22"/>` for H6=11pt). Word still recognizes the result as semantic headings on copy/paste. Avoids needing a styles.xml part.

**Bullets without /word/numbering.xml**: bullets render with a literal `•` + space prefix on a normal paragraph. Real numbering definitions can come later if anyone asks; for now the simple approach is enough and keeps the part count at 3.

**Files**:

- `internal/files/docx.go`, `internal/files/docx_test.go` (new — 8 tests).
- `internal/tools/files.go` — `CreateDOCXTool` next to `CreateXLSXTool` (reuses `DocumentSender` interface from 15a). Modest duplication of persist+deliver logic; will refactor into a helper if 15c adds a third format.
- `internal/tools/files_test.go` — 5 docx tool tests (happy path + title-only + reject-empty + deliver-false + reject-block-missing-kind).
- `internal/source/source.go`, `internal/source/store.go` — `KindDOCX` constant + `.docx` extension.
- `internal/api/sources.go` — `rawAssets[KindDOCX]` row + `validKind` extension.
- `internal/api/router_test.go` — `TestSourceRaw_PDFAndXLSXAndDOCX` extends slice 15d's test.
- `internal/telegram/setup.go` — `CreateDOCXTool` registration after `CreateXLSXTool`.
- `web/src/types/api.ts` — kind union extended to `'pdf' | 'text' | 'url' | 'xlsx' | 'docx'`.
- `web/src/components/SourceInbox.tsx` — Download button shows for docx too.
- `cmd/debug_docx/main.go` (new) — 5-scenario hermetic harness mirroring `cmd/debug_xlsx`. `go run ./cmd/debug_docx` runs all in <1 s; `-out <path>` writes the workbook to disk for visual inspection.

**Quality gates**: `go build/vet/test ./...` all green. `go run ./cmd/debug_docx` 5/5. Visual check: opened `D:/tmp/aura-debug-memo.docx` (1412 bytes) — 2-sheet structure rendered with title + paragraphs + bullets + table, no XML parser errors. tsc + vite build clean (358 KB main / 112 KB gz, no regression).

**Follow-ups landed**: 15c `create_pdf`, 15d dashboard download, and 15e natural-prompt smoke are complete.

### 2026-05-02 — Phase 15 slice 15d (Dashboard download endpoint + button)

Closes the dashboard loop for `KindXLSX` sources from 15a — non-Telegram users (and the operator inspecting past generations) can now download generated workbooks straight from `/sources`. Generalizes `handleSourceRaw` so 15b (`docx`) and 15c (`pdf`) only need to add a `rawAssets[Kind]` row.

**Backend** (`internal/api/sources.go`):

- New `rawAssets` table: `Kind → {filename, contentType, disposition}`. PDFs use `inline` (browsers preview natively); XLSX uses `attachment` (no browser previews .xlsx).
- `handleSourceRaw` now: lookup record → resolve asset row → 404 if kind has no asset → stream via `http.ServeContent` with the right `Content-Type` + `Content-Disposition`.
- `validKind` accepts `xlsx` so `GET /sources?kind=xlsx` filtering works.

**Frontend** (`web/src/components/SourceInbox.tsx`, `web/src/types/api.ts`):

- `SourceSummary.kind` union extended to `'pdf' | 'text' | 'url' | 'xlsx'`.
- `SourceActions` gains a Download button (shown for PDF + XLSX). Re-OCR / Ingest are now gated behind `kind === 'pdf'` so XLSX rows don't expose OCR-only actions that would 4xx.
- `downloadSource(s)` helper: `fetch('/api/sources/<id>/raw', { Authorization: Bearer ... })` → `Blob` → `URL.createObjectURL` → `<a download>`. The auth-gated endpoint can't be hit via plain `<a href>` (Authorization headers don't ride link clicks).

**Files**:

- `internal/api/sources.go` — generalized raw handler.
- `internal/api/router_test.go` — `TestSourceRaw_PDFAndXLSX` replaces `TestSourceRaw_PDFOnly`. Asserts content-type + content-disposition + body bytes for both PDF and XLSX, plus 404 for text.
- `web/src/types/api.ts` — kind union.
- `web/src/components/SourceInbox.tsx` — Download button, kind-gated actions, `downloadSource` helper.
- regenerated `internal/api/dist/`.

**Quality gates**: `go build/vet/test ./...` green. `npx tsc --noEmit` clean. `npx vite build` clean (358 KB main / 112 KB gz, no regression). Will live-test the Download path on the next bot run alongside 15b/15c.

### 2026-05-02 — Phase 15 slice 15a (`create_xlsx` tool + Telegram delivery)

First slice of Phase 15 (file creation milestone). Aura goes from "knowledge & conversation agent" to "produces files for me" — this slice ships the smallest valuable wedge: structured-rows → xlsx workbook → Telegram document, persisted in the existing sources store so "show me last week's invoice" works for free via sha256 dedup.

**Architecture**:

- `internal/files` (new): pure generator package. `BuildXLSX(spec) → (bytes, filename, error)`. No Telegram or source-store coupling — same pattern as `internal/ocr` returning markdown without writing.
- `internal/source.KindXLSX` (extension): `.xlsx` extension wired into `extForKind` and `validatePutInput`. Generated artifacts persist in the same `wiki/raw/<id>/` layout as user-uploaded PDFs.
- `internal/tools.CreateXLSXTool` (new): LLM-facing wrapper. Persists via `store.Put` (sha256 dedup), marks `StatusIngested` (no compile step to run), and optionally invokes `DocumentSender.SendDocumentToUser` when `deliver=true` (default). Refuses delivery when there's no user context or no sender configured — the LLM gets a clear retry message instead of a silent drop.
- `internal/tools.DocumentSender` (new interface, mirrors `TokenSender`): `SendDocumentToUser(userID, filename, body, caption)`. Bot satisfies it; tests stub it.
- `Bot.SendDocumentToUser` (new method, mirrors `SendToUser`): wraps `tele.Document{File: tele.FromReader(bytes.NewReader(body))}`. Telegram caps non-premium bot documents at 50 MB; the generator's `MaxBytes=25 MB` keeps us comfortably below.
- Tool registration: post-`b` construction in `setup.go`, same place as `request_dashboard_token`.

**Security posture (`SanitizeCell` + `SanitizeFilename`)**:

- Excel formula injection (CWE-1236): cells starting with `=`, `+`, `-`, `@`, `\t` (0x09), or `\r` (0x0D) get a leading apostrophe so Excel treats the value as a literal string. OWASP CSV-injection mitigation guidance.
- Filename sanitization: extracts basename FIRST (so `path/to/file` → `file`, not `pathtofile`), strips Windows-reserved chars (`<>:"/\|?*` + 0x00–0x1F), trims trailing dots/spaces, forces `.xlsx`, caps at 80 chars while preserving the suffix.
- Sheet name sanitization: 31-char cap, replaces `:\\/?*[]` with `_`, dedups duplicate names with `_2`/`_3` suffixes.
- Hard caps on sheet count, rows, cols, cells, and serialized bytes block both runaway LLM output and Telegram's document cap.

**Files**:

- `internal/files/xlsx.go`, `internal/files/xlsx_test.go` (new package, 12 tests).
- `internal/tools/files.go`, `internal/tools/files_test.go` (new, 7 tests).
- `internal/source/source.go` — `KindXLSX` constant.
- `internal/source/store.go` — `extForKind` + `validatePutInput` accept `KindXLSX`.
- `internal/telegram/bot.go` — `SendDocumentToUser` method (mirrors `SendToUser`).
- `internal/telegram/setup.go` — `CreateXLSXTool` registration.
- `cmd/debug_xlsx/main.go` (new) — 5-scenario hermetic E2E harness. `go run ./cmd/debug_xlsx` runs all in <1 s; `-out <path>` additionally drops the workbook to disk for visual inspection in Excel/LibreOffice.
- `go.mod` / `go.sum` — `github.com/xuri/excelize/v2 v2.10.1` plus transitive deps (`mscfb`, `msoleps`, `efp`, `nfp`, `go-deepcopy`).

**Quality gates**: `go build ./...`, `go vet ./...`, `go test ./...` all green. `go run ./cmd/debug_xlsx` all 5 scenarios pass. Verified visually by writing `D:/tmp/aura-debug-q1-report.xlsx` and opening — two sheets ("Q1", "summary"), correct values, no formula injection.

**Follow-ups since landed**:

- 15b `create_docx` — done.
- 15c `create_pdf` — done.
- 15d dashboard download endpoint (`GET /api/sources/<id>/raw`) + Sources panel Download button — done.
- 15e LLM-driven natural-prompt tests via `cmd/debug_files` — done.
- Re-OCR / re-ingest buttons are hidden for generated artifact rows in the dashboard — done.

### 2026-05-02 — Phase 14.5 (Dashboard UX hardening)

Closes the high/medium findings from the historical dashboard UX audit. One atomic commit. No backend or schema changes.

**Audit fixes**:

1. **Mobile data overflow** (audit High #1) — `WikiPanel`, `SourceInbox`, `TasksPanel`, `ConversationsPanel` gained mobile card stacks (`md:hidden`) paired with the existing tables (`hidden md:block`). Tables no longer overflow `390px` viewports.
2. **Graph canvas mobile** (audit High #2) — `WikiGraphView` initial size changed from fixed `{800,600}` to `{0,0}` with a "Measuring graph space..." fallback until `ResizeObserver` reports a real container width; mobile gains a searchable node list below the canvas.
3. **Touch targets ≥44px** (audit Medium #3) — applied `min-h-11` + `px-3 py-2` to filter pills, action buttons, form inputs, mobile hamburger (`size-11`), MCP Invoke + JSON textarea, PendingUsers Approve/Deny.
4. **AA contrast in metadata text** (audit Medium #4) — `text-muted-foreground/70` removed from `MaintenancePanel`, `SummariesPanel`, `ConversationsPanel` empty states; `SettingsPanel` source badges bumped from `*-500/600` to `*-700/300` with `12%` tinted backgrounds; `HealthDashboard` legend label switched from `text-muted-foreground` to `text-foreground` and the decorative bar got `aria-hidden` paired with an `sr-only` summary.
5. **Auth-expiry returnTo** (audit Medium #5) — `api.ts`'s `handle401` now stashes `pathname+search+hash` to `sessionStorage` and appends it to the redirect as `?returnTo=…`; `Login.tsx` reads in this priority order: query param → router state → sessionStorage → `/`, with a `safeReturnTo` guard against `//` and `/login` recursion.
6. **Native confirm/prompt → custom modal** (audit Low #6) — new `web/src/components/common/ConfirmModal.tsx` (Radix Dialog host) + `web/src/lib/confirmModal.ts` (imperative `confirm()`/`prompt()` API), `<ConfirmHost />` mounted at the app root. Replaces `window.confirm` in `TasksPanel.handleDelete`, `SkillsPanel.handleDelete`, and `ConversationsPanel`'s three cleanup buttons; replaces `window.prompt` in `handleCleanupOlder`. Destructive confirms focus Cancel by default; prompts auto-focus + select the input. `web/e2e/confirm-modal.spec.ts` covers the open/cancel/validation paths without touching live data.

**Quality gates**: `npx tsc --noEmit` clean. `npx vite build` clean (358 KB main / 112 KB gz — no regression vs the 14d-followup baseline). `go build ./...`, `go vet ./...`, `go test ./...` all green.

**Files**: `web/src/api.ts`, `web/src/App.tsx`, `web/src/components/{Login,Shell,HealthDashboard,WikiPanel,SourceInbox,TasksPanel,ConversationsPanel,WikiGraphView,SkillsPanel,MCPPanel,PendingUsersPanel,SettingsPanel,MaintenancePanel,SummariesPanel}.tsx`, `web/src/components/common/ConfirmModal.tsx` (new), `web/src/lib/confirmModal.ts` (new), `web/e2e/confirm-modal.spec.ts` (new), regenerated `internal/api/dist/`.

### 2026-05-02 — Slice 14 (Onboarding overhaul + retention controls)

Replaces the hand-edit-`.env` install path with a first-run wizard, adds a runtime `/settings` page so most config can change without restart, and gives the operator explicit control over scheduled-task lifecycle and conversation-archive growth.

**Atomic commits in order**:
1. `fdc6f25` 14a — settings store + applier (no behavior change)
2. `830a17e` 14b+c — first-run wizard with provider presets + live probe
3. `f2c07ca` 14d — auth'd /settings dashboard page
4. `485cf51` 14e — slim .env.example + rewrite INSTALL.md
5. `4913249` 14d-followup — SPA code-split (580 → 353 KB main)
6. `f1d1fa6` E2E + debug_settings helper
7. `c964e5b` switch contrast fix + Go embed cache gotcha doc
8. `6e748f4` 2026 redesign (Geist/Linear/Stripe patterns)
9. (this commit) — task delete + recurrence (every_minutes) + conversation cleanup + docs

**User-driven follow-ups** (this commit):
1. `/tasks` had no row deletion — only Cancel which flipped status. Added `POST /api/tasks/{name}/delete` + UI button. Cancel kept for audit trail; Delete is the user-driven cleanup.
2. `/conversations` archive grew unbounded with no UI control. Added `Stats`, `DeleteByChat`, `DeleteOlderThan`, `DeleteAll` to ArchiveStore. Three confirm-gated buttons in the panel header: "Purge older than…", "Wipe this chat" (visible when chat_id filter active), "Wipe all". Stats badge shows total rows + distinct chats + oldest entry.
3. Recurring tasks were limited to "daily HH:MM" — couldn't schedule hourly/weekly/custom intervals. Added `ScheduleEvery` kind backed by a new `schedule_every_minutes` column with idempotent migration. UI form gained a third radio with hint copy ("60 = hourly, 1440 = daily, 10080 = weekly").

**Quality gates**: 28 / 28 Playwright specs green (11 dashboard + 6 settings + 11 new tasks/cleanup). 12 new Go API tests, all passing. `go build`, `go vet`, `go test ./...` all clean.

**Docs**: VISION.md picks up two new principles ("No hand-edit installs" + "Bounded growth"). INSTALL.md rewritten around the wizard flow with new sections on managing tasks (3 recurrence modes) and conversation cleanup (3 cleanup buttons).

### 2026-05-02 — Phase 13 (Telegram bot god-file refactor)

Split `internal/telegram/bot.go` from a 1,281-line mixed-responsibility file into focused package files while preserving behavior:

- `bot.go`: core `Bot` type plus lifecycle/public helpers.
- `setup.go`: construction and wiring.
- `access.go`: `/start`, `/login`, allowlist, pending approval, and dashboard-token delivery.
- `handlers.go`: Telegram handler registration and text entrypoint.
- `conversation.go`: conversation turn orchestration, tool loop, and tool execution.
- `streaming.go`: assistant delivery and progressive Telegram stream editing.
- `scheduler_handlers.go`: reminder and wiki-maintenance dispatch.
- `status.go`: `/status` and budget status helpers.
- `adapters.go`: API/skills adapter shim.

No behavior changes intended; this is an ownership-boundary refactor to make future Phase 12 follow-ups smaller and safer. Verification: `go test ./...`, `go build ./...`, and `go vet ./...` all pass.

### 2026-05-02 — Phase 12 (Compounding Memory) v0.12.0

Single session. Lead orchestrated a 3-teammate Claude Code Agent Team (Backend / Frontend / Q&A) all on Sonnet 4.6 against the historical Phase 12 compounding-memory plan. 21 atomic slices (12a–12u) + 9 post-review follow-ups (12u.1–12u.9) + 2 lead infra commits (12.cleanup, 12.fix-applier).

**Architecture**: SQLite `conversations` archive (write side: `BufferedAppender` chan-100, drain goroutine, drop-on-full slog warn; read side: `ArchiveStore.ListByChat/ListAll/Get/MaxTurnIndex`). `summarizer` package: `LLMScorer` temperature=0 → `Deduper` (sim>0.85 skip / ≥0.5 patch / <0.5 new) → 3 `Applier` impls (Auto/Review/Off) gated by `SUMMARIZER_MODE`. `MaintenanceJob` Levenshtein auto-fix + `wiki_issues` queue with severity policy. `compounding_rate` metric on `/api/health`. Dashboard: `/conversations`, `/summaries`, `/maintenance` routes + 5th `HealthDashboard` card + sidebar nav with `g v / g u / g x` chords.

**Notable bugs caught and fixed in-flight**:
1. `internal/search/sqlite.go` had a dead `conversations` table whose schema collided with slice 12a's archive — user couldn't run the bot. Fix in 12.cleanup: removed dead `StoreConversation` + `createConversationsTable`, consolidated single source of truth in `scheduler/store.go`, added one-shot `dropLegacyConversations` migration that detects pre-Phase-12 tables (no `chat_id` column) and drops them on first start.
2. Q&A's debug_summarizer integration harness (slice 12r) caught `AutoApplier.applyNew` constructing `wiki.Page` without `SchemaVersion` / `PromptVersion` — every `ActionNew` write would silently fail validation in production auto mode. Fix in 12.fix-applier: set versions + extend `promptVersionRe` regex to accept `summarizer_v{n}`.
3. Two cross-teammate staging collisions (Frontend's `git add` swept Backend's uncommitted untracked files into combined commits). Lead resolved with `git reset --soft HEAD~N` + atomic re-commits. After the second collision, Backend was shut down (queue complete) to eliminate the risk for the remainder.

**Opus 4.7 review (slice 12u, gsd-code-reviewer)**: 2 CRITICAL + 7 HIGH + 8 MEDIUM + 6 LOW findings. Both CRITICALs (CR-01 frontend response shape mismatch breaking `/conversations`; CR-02 chat_id forced 400 on initial mount) fixed as 12u.1 + 12u.2. All 7 HIGHs landed: HR-03 archive dropping tool_calls + telemetry; HR-04 `turnMsgIdx` staleness causing silent data loss when `EnforceLimit` trims mid-turn (fixed via DB-monotonic `MaxTurnIndex`); HR-05 OffApplier still paying scorer LLM cost (early-return); HR-06 fresh-`IssuesStore`-per-run anti-pattern (single shared store); HR-07 `Resolve` swallowing DB errors (surface real errors via `ErrIssueAlreadyResolved`); HR-01 `RepairLink` partial-commit (continue + joined per-page errors); HR-02 proposal category/related-slug round-trip (schema migration + approval restore).

**Quality gates**: 289 tests across 6 packages green. `go vet` clean. `staticcheck -checks U1000` zero findings. Frontend lint + tsc + build clean. Coverage: archive.go / maintenance.go / issues.go / scorer.go / dedup.go / types.go all 100% per function. Race detector deferred to Linux CI (Windows linker conflict with HMITool7.0).

**Post-review closure**: HR-01 and HR-02 landed as 12u.8 and 12u.9. No HIGH findings remain open; MEDIUM/LOW items remain backlog candidates.

### 2026-04-30 — Slice 11u (Render assistant Markdown into Telegram HTML)

- One atomic commit (`284d59b`).
- Telegram's default parse mode rendered LLM Markdown as literal text — `**bold**`, `## headers`, `- bullets`, `[link](url)` arrived raw.
- Added `internal/telegram/markdown.go` (245 LOC, 68 LOC tests): converts to Telegram's HTML subset (`b/i/s/u/code/pre/a/blockquote`) and sends with `tele.ModeHTML`. Headings degrade to `<b>`, bullets to `•`. Links restricted to `http(s)`/`tg` schemes to block `javascript:` smuggling. Plain-text reserved chars escaped; `<code>`/`<pre>` content preserved.
- Wired through both delivery paths: `handleConversation` final `c.Send` (non-streamed) via new `sendAssistant`, and `consumeStream` progressive `Send`/`Edit` (streamed). Operator-facing strings (auth errors, bootstrap) keep raw `c.Send` to avoid double-escaping.
- Files: `internal/telegram/bot.go`, `internal/telegram/markdown.go` (new), `internal/telegram/markdown_test.go` (new).
- Verification: `go build ./...`, `go vet ./...`, `go test ./internal/telegram/...` pass.

### 2026-04-30 — Slice 11t (Progressive Telegram edit while streaming LLM response)

- One atomic commit (`d78a932`).
- Final-response latency was the last big perceived-latency lever; slice 11l/m/p cut server-side wall clock but the user still saw nothing until the full assistant message landed.
- Bot now opens a placeholder Telegram message once 30 chars of streamed text accumulate (avoids displaying discardable prefaces) and edits it every 800 ms (Telegram safe rate-limit per chat) until the stream completes.
- Tool loop swapped `Send` → `Stream`. `consumeStream` rebuilds an equivalent `llm.Response` from the token stream so all downstream code (token tracking, budget tracking, tool execution) is unchanged.
- Tool-call turns: streamed text becomes the assistant's "Let me search…" preface; tool execution proceeds as before. Text-only turns: the progressively-edited message *is* the final delivery — `runToolCallingLoop` returns `""` so `handleConversation` skips its `c.Send` to avoid double-posting.
- Slice 11s wired `stream_options.include_usage` and `Usage` on the final Token so budget tracking still works under streaming. Providers that ignore `stream_options` leave `Usage` zero — caller tolerates.
- Files: `internal/llm/client.go`, `internal/llm/openai.go`, `internal/telegram/bot.go`.

### 2026-04-30 — Slice 11s (Stream tool-call deltas through llm.Token)

- One atomic commit (`2ea45e3`). Prerequisite for slice 11t.
- Pre-11s `Stream()` returned only text deltas; if the model emitted tool calls during a streamed response we silently dropped them — making streaming unusable for any tool-calling turn.
- `Token` gained an optional `ToolCalls` slice populated on the final `Done=true` token. SSE reader accumulates per-index `function.arguments` fragments internally so consumers never see partial JSON.
- `Stream()` now also forwards `Request.Tools` — previously streaming requests omitted the tools array entirely so the model had no way to call a tool from a streamed call.
- `OllamaClient.Stream` forwards to `OpenAIClient` and inherits the new behavior automatically.
- `TestOpenAIClientStream` still passes; new `TestOpenAIClientStreamWithToolCalls` exercises the multi-fragment accumulation path.
- Files: `internal/llm/client.go`, `internal/llm/openai.go`, `internal/llm/openai_test.go`.

### 2026-04-30 — Slice 11r (Per-turn latency telemetry)

- One atomic commit (`885fef5`).
- Slice 11n's benchmarks proved smart-and-fast wins in microbenchmarks (skills cache 10000×, parallel tools 4×). This adds the runtime counterpart so real Telegram latency is measurable without sprinkling per-subsystem timers.
- Every conversation turn now logs structured `elapsed_ms`, `llm_calls`, `tool_calls`.
- `runToolCallingLoop` returns `turnStats{llmCalls, toolCalls}` alongside the response string. `handleConversation` captures `turnStart` at the top and emits the structured "conversation complete" line on the way out.
- Files: `internal/telegram/bot.go`.

### 2026-04-30 — Slice 11q (Bootstrap prompt overlay files)

- One atomic commit (`8102143`). Picobot pattern from `internal/agent/context.go`.
- Reads a fixed set of optional MD files from `PROMPT_OVERLAY_PATH` (default `.` locally, `/app` in Docker) on every conversation turn and appends to the system prompt: `SOUL.md` (personality), `AGENT.md` (Aura runtime notes), `USER.md` (durable user facts), `TOOLS.md` (tool guidance). `AGENTS.md` is development-only and is deliberately not injected.
- Operator tunes any of the four by editing the file — the next user turn picks the change up with no recompile or restart. All files optional; missing/blank skipped silently.
- 4 file reads per turn negligible vs the LLM round-trip.
- Files: `.env.example`, `internal/config/config.go`, `internal/conversation/overlay.go` (new), `internal/conversation/overlay_test.go` (new), `internal/telegram/bot.go`.

### 2026-04-30 — Slice 11p (Speculative wiki retrieval before first LLM call)

- One atomic commit (`900ec71`).
- Pre-11p the model only saw durable wiki memory after explicitly emitting `search_wiki` — a full extra LLM round-trip per turn ("reason → emit call → read result → re-reason → answer").
- Picobot's `agent/context.go` injects ranked memories into the system prompt before the first inference; we now do the same. `handleConversation` runs `search.Search(userText, 5)` right after `AddUserMessage` and pipes results through `convCtx.SetSearchContext`.
- Embedding cache (slice 11h) makes repeat queries free; cold queries pay one embed call but save the round-trip. The explicit `search_wiki` tool stays available for follow-up refinement.
- Files: `internal/telegram/bot.go`.

### 2026-04-30 — Slice 11o (Gate /start behind frontend approval queue)

- One atomic commit (`5bdaeb0`).
- Closes the TOFU bootstrap window: once an owner exists, an unknown /start no longer auto-rejects with the user's Telegram ID echoed back — it queues into `pending_users`, pings every allowlisted user via Telegram, and waits for an explicit approve/deny decision from the dashboard.
- Approval mints a fresh token and ships it over Telegram so the plaintext never round-trips through the dashboard.
- New `internal/api/pending.go` + `internal/auth/store.go` + `internal/api/pending_test.go` + `internal/auth/pending_test.go`. Dashboard `/pending` panel polled every 8 s (`PendingUsersPanel.tsx`).
- Spam `/start` preserves `requested_at` while pending — no pingstorm on the owner. Only a prior `decision` (approved/denied) resets the row.
- TOFU bootstrap intentionally kept for first-owner onboarding on a virgin install (otherwise the dashboard has nobody to log in and approve).
- Files: 18 changed, 1138 +/103 -. Backend, auth store, frontend route, sidebar nav.

### 2026-04-30 — Slice 11n (Latency benchmarks for slices 11k–11m)

- One atomic commit (`d83dd61`).
- Quantified the smart-and-fast wins:
  - `BenchmarkLoaderLoadAllCached` 339 ns/op vs `Uncached` 3.69 ms/op (slice 11m).
  - `BenchmarkRegistryExecuteSequential` 41 ms/op vs `Parallel` 10 ms/op (slice 11l).
- Skills bench needed `writeFile`/`writeSkill` to accept `testing.TB` so a `*testing.B` can call them — narrowed helper signature accordingly, no behavior change for existing tests.
- Files: `internal/skills/loader_bench_test.go` (new), `internal/skills/loader_test.go`, `internal/tools/registry_bench_test.go` (new).

### 2026-04-30 — Slice 11m (Cache skills loader output for 1s)

- One atomic commit (`8aa0f15`).
- `handleConversation` called `skillLoader.LoadAll()` on every Telegram message to render the system-prompt manifest — walked `SKILLS_PATH` plus `.claude/skills`, opened and YAML-parsed each `SKILL.md` every turn. Pure waste when skills only change on rare admin install/delete.
- Memoize `LoadAll` for `cacheTTL=1s`. Window short enough that admin operations reflect on the next user turn but long enough that back-to-back chat turns hit the cache (typical case). `Invalidate()` exposed for callers wanting immediate consistency.
- Files: `internal/skills/loader.go`, `internal/skills/loader_test.go`.

### 2026-04-30 — Slice 11l (Parallelize tool calls within an assistant turn)

- One atomic commit (`b46b9ba`).
- Model frequently emits multiple independent tool calls in a single response (e.g. `search_wiki + web_search + read_wiki`). Running them sequentially serialized N round-trips of latency for no reason — each call already uses its own ctx and the registry is RWMutex-guarded.
- Extracted `executeToolCalls`: emit all activity pings up front, fan out one goroutine per call, join, then append results in original order. Ordering loop after `wg.Wait` preserves deterministic message ordering in conversation history.
- Files: `internal/telegram/bot.go`.

### 2026-04-30 — Slice 11k (Picobot-style message-count cap, drop summarizer from tool loop)

- One atomic commit (`0f16509`).
- The active conversation was unboundedly sticky and re-enforced its token budget on every tool iteration — both made the agent slow (extra summarizer LLM calls mid-response) and dumb (lossy summarization overwriting recent reasoning).
- Adopt Picobot strategy: cap in-flight messages at `MAX_HISTORY_MESSAGES` (default 50) and trim oldest with a tool-safe boundary. The wiki/sources tools already carry durable memory so chat history is allowed to evict.
- `EnforceLimit` now applies the cheap message cap first; summarization only fires as a fallback for pathologically large single messages. The inner-loop `EnforceLimit` call in `runToolCallingLoop` is removed — `MaxToolIterations` already bounds per-turn growth.
- Files: `.env.example`, `internal/config/config.go`, `internal/conversation/context.go`, `internal/conversation/context_test.go`, `internal/telegram/bot.go`.

### 2026-04-30 — Slice 11j (Surface embed cache stats on /api/health)

- One atomic commit (`1bac86d`). Bridge between slice 11h (cache) and the dashboard.
- `EmbedCache.Stats()` is now plumbed into `Deps.EmbedCache` and the health rollup. New `EmbedCacheHealth{hits, misses}` block on `GET /api/health`.
- Frontend: dashboard gains a fourth status card showing `<hits>` as the headline number with subtitle = computed hit-rate percentage (or "no embeds yet" before the first call). Stays at 0/0 when no cache is wired (no `EMBEDDING_API_KEY` or `DB_PATH`).

### 2026-04-30 — Slice 11i (Concurrent wiki indexing)

- One atomic commit (`0501db6`).
- `IndexWikiPages` previously called `coll.AddDocument` serially in a per-page loop — 8 pages × ~1 s per Mistral round trip = ~8 s cold start.
- Switched to chromem-go's already-supported `coll.AddDocuments(ctx, docs, indexConcurrency)` which spawns parallel goroutines. New `indexConcurrency = 4` constant: ~4× faster cold start, well under Mistral free-tier rate limits.
- Atomic-failure fallback path serializes if the batch fails so one bad page doesn't lose the whole index. SQLite FTS mirror stays serial (cheap local writes; concurrent FTS inserts contend).
- Stacks on 11h: warm starts still hit the cache and pay nothing.

### 2026-04-30 — Slice 11h (SHA-keyed embedding cache)

- One atomic commit. Wraps `chromem.EmbeddingFunc` with a SQLite-backed cache (`embedding_cache` table, composite key `(content_sha, model)`).
- Cold start unchanged; warm starts hit the cache and skip the Mistral round trip entirely for unchanged wiki pages — 30 wiki pages × ~1 s per embed = ~30 s saved per restart. Same path serves query embeddings, so repeat questions skip the round trip too.
- Robustness: corrupt blob detection (length-not-multiple-of-4 → re-embed + delete row), upstream-error propagation, model-key isolation (changing `EMBEDDING_MODEL` invalidates entries automatically), nil-upstream errors cleanly on miss.
- Kept chromem-go in place vs swapping to `sqlite-vector` because the latter would force CGO + native extension loading; this fix gets ~99% of the win with 150 LOC.
- **Bundled cleanups**: deleted dead `sqliteSearcher.indexWikiDir` method (and the now-unused `os` + `filepath` imports), removed unused `newTestEngine` helper, added missing `Content` assertion in `TestResultStruct`. 8 cache tests + 1 strengthened test. Race-clean.

### 2026-04-30 — Slice 11g (Pin install cwd to project root)

- One atomic commit. Hot-fix from a real install bug.
- Bug: `marketing-psychology` install landed at `D:\Aura\skills\.claude\skills\` (nested) instead of `D:\Aura\.claude\skills\`, so the loader missed it.
- Cause: `NPXInstaller.Install()` used `cmd.Dir = cfg.SkillsPath`; the skills.sh CLI uses cwd as its project-detection anchor and writes to `<cwd>/.claude/skills/`.
- Fix: `NewNPXInstaller(skillsDir, projectDir)` now takes a separate project-root parameter; bot passes `""` which falls back to `os.Getwd()` (Aura's cwd at startup = project root). Existing nested install was relocated by hand.

### 2026-04-30 — Slice 11f (Progressive-disclosure skill prompt)

- One atomic commit. Architectural fix to the skill-injection model that both Picobot and earlier Aura got wrong.
- **Problem**: `auraskills.PromptBlock` (and `picobot/internal/agent/context.go:62-74` — same pattern) read every loaded skill and concatenated its full body into the system prompt on every turn. With Anthropic's `claude-api` skill at 28 KiB, two or three skills would balloon the system prompt to 60+ KiB even on small-talk turns where no skill applies. That's wasted prompt-cache bandwidth, slower TTFT, and higher token cost on the common case.
- **Fix**: switch to Anthropic's intended progressive-disclosure model. `PromptBlock` now emits a manifest:
  ```
  ## Available Skills

  Aura has the local skills listed below. Each entry's description states when it applies. Before following a skill's guidance, call the `read_skill` tool with the skill name to load its full instructions, then act on them. Skip skills whose description does not match the user's request.

  - **claude-api** — Build, debug, and optimize Claude API … (TRIGGER when …)
  - **aura-implementation** — Implement Aura second-brain features…
  ```
  ~200 bytes per skill in the prompt instead of 1–30 KiB. The full SKILL.md body is fetched lazily via the existing `read_skill` LLM tool the moment the model decides a description matches.
- **Tradeoffs**:
  - Common case (no skill applies) — system prompt drops by ~95%; faster + cheaper.
  - Skill-applies case — one extra tool round-trip (LLM calls `read_skill`, then continues). The body becomes a normal user-message in the tool loop, so prompt caching covers the rest of the turn.
  - Net: clear win at any non-trivial skill count.
- **Caps**:
  - `maxManifestDescChars = 1500` — single description ceiling. claude-api's description (with embedded TRIGGER/SKIP rules) is ~1.2 KiB so this fits comfortably; runaway descriptions get `…[truncated]`.
  - `maxSkillsBlockChars = 8000` (down from 12 KiB) — total manifest cap. At ~200 bytes per typical skill, this fits ~30 skills before the bound kicks in.
  - `maxSkillPromptChars` constant removed (no body in manifest, no per-body cap needed).
- **Tests** (`internal/skills/loader_test.go`): updated `TestPromptBlock` to assert the new manifest format AND verify the body is NOT present (regression guard); 2 new tests cover description truncation and the 50-skill total-size bound.
- **Verification**: `go build ./...` clean, `go vet ./...` clean, `go test ./...` PASS.
- Files touched: `internal/skills/loader.go`, `internal/skills/loader_test.go`, `docs/implementation-tracker.md`.
- No frontend changes — `read_skill` tool was already wired in slice 10j, and the dashboard `/skills` panel reads bodies through its own API endpoint, not through PromptBlock.
- Manual verification: restart Aura, ask "what skills do you have?" → LLM should answer from the manifest descriptions without first calling `read_skill`. Then ask "use claude-api to help me set up prompt caching" → LLM should call `read_skill("claude-api")` before answering.

### 2026-04-30 — Slice 11e (Catalog installs visible to the loader)

- One atomic commit. Hot-fix triggered when the user flipped `SKILLS_ADMIN=true` and installed `claude-api` from the catalog: dashboard reported success but the skill never appeared in the Local tab.
- **Root causes** (two bugs stacked):
  1. The skills.sh CLI has a SECOND interactive prompt after `--skill <id>` is passed: "Which agents do you want to install to?". Without `--agent`, it hangs forever on stdin. Our 11c `NPXInstaller` only passed `--yes` (to npx) + optional `--skill`; the install ran for 90 s, hit `context.WithTimeout`, and returned an error. The `claude-api` install we observed had succeeded because I ran it manually with `--agent claude-code -y` during diagnosis.
  2. Even when the install succeeds, `npx skills add ... --agent claude-code` writes to `<project_root>/.claude/skills/<name>/SKILL.md`, NOT to `cfg.SkillsPath`. The CLI does its own project-root discovery and ignores cwd for the install target. Aura's loader only scanned `./skills`.
- **Fix 1 — non-interactive install** (`internal/skills/admin.go::NPXInstaller.Install`):
  - argv now `["--yes", "skills", "add", source, "--agent", "claude-code", "-y", "--skill", id?]`. The trailing `-y` is the skills CLI's own auto-confirm flag, distinct from npx's `--yes`.
  - `cmd.Stdin = nil` so any future prompt we forgot to suppress can't fall back to "press enter" behaviour.
- **Fix 2 — multi-root loader/deleter** (`internal/skills/loader.go`, `internal/skills/admin.go::FSDeleter`):
  - `NewLoader(dir, extra...)` and `NewFSDeleter(dir, extra...)` are now variadic. Single-arg callers in tests (and elsewhere) still compile unchanged.
  - `Loader.LoadAll()` walks every root and dedupes by skill name. Primary root wins on duplicates so a hand-written skill in `./skills/` overrides a catalog version with the same name.
  - `Loader.LoadByName()` returns the first match in priority order; only returns `os.ErrNotExist` if no root has it.
  - `FSDeleter.Delete()` mirrors that — first matching root wins. Containment + symlink refusal apply per-root.
  - Bot wires `auraskills.NewLoader(cfg.SkillsPath, ".claude/skills")` and the matching deleter so catalog installs are immediately visible.
- **Tests**: 4 new in `loader_test.go`:
  - `TestLoaderMultiRootMerges` — primary has alpha, secondary has bravo; LoadAll returns both, sorted; LoadByName(bravo) finds it via secondary.
  - `TestLoaderMultiRootPrimaryWinsOnDuplicate` — same skill name in both roots; LoadByName returns primary, LoadAll dedupes to one entry.
  - `TestFSDeleterMultiRootDeletesFromSecondary` — skill only exists in secondary; delete succeeds and removes it.
  - `TestFSDeleterMultiRootNotFound` — no roots have it → `IsSkillNotFound`.
- **Verification**: `go build ./...`, `go vet ./...`, `go test ./...` all pass. The pre-existing live install at `D:\Aura\.claude\skills\claude-api\SKILL.md` (28 KB) is now picked up by the loader without restart-time changes.
- Files touched: `internal/skills/admin.go`, `internal/skills/loader.go`, `internal/skills/loader_test.go`, `internal/telegram/bot.go`, `docs/implementation-tracker.md`.
- Manual verification still owed by user: restart Aura, open `/skills` Local tab — `claude-api` should now appear with the description "Build, debug, and optimize Claude API…". Install a second one from the Catalog tab and verify it lands non-interactively (no 90 s wait) and shows up immediately in Local.

### 2026-04-30 — Slice 11d (Invoke MCP tools from dashboard)

- One atomic commit. Phase 11 complete: skills + MCP fully reachable from the dashboard, end-to-end.
- **Auth model decision**: bearer auth only, no `MCP_ADMIN` flag. Reasoning: MCP servers are opt-in via `mcp.json` — if the operator wired one, the LLM can already invoke its tools through the agent loop, so a separate dashboard gate would be theatre. Bearer auth + Telegram allowlist re-check (existing `RequireBearer` middleware) is the same gate every other write endpoint uses.
- **Backend** (`internal/api`):
  - `mcp_write.go` (new) — `handleMCPInvoke` resolves the client by name (404 on miss), checks the tool is advertised by that server (404 on unadvertised), validates the URL-path tool name against `^[A-Za-z0-9_.\-]{1,128}$`, parses the body (caps at 64 KiB; empty/`null` → `{}`; non-object → 400), and calls `client.CallTool` with a 60 s `context.WithTimeout`. Distinguishes server-reported `isError:true` (the client returns these as `tool error: …`) from transport/timeout failures and surfaces both as `200 {ok:false}` with the right `is_error` flag so the UI can render them inline. Output is clipped at 64 KiB.
  - `types.go` — `MCPInvokeResponse{ok, is_error?, output?, error?}`.
  - `router.go` — `POST /mcp/{server}/tools/{tool}` registered after the existing read endpoints.
- **Frontend** (`web/src/components/MCPPanel.tsx`):
  - `ToolRow` gains a Run button that toggles a JSON textarea + Invoke action. The textarea is seeded by `seedArgsFromSchema(input_schema)` — for each `properties` entry, emits `0` for integer/number, `false` for boolean, `[]` for array, `{}` for object, `""` for the rest. Operators can clear it back to `{}` if the seed is wrong.
  - Submit parses the body locally (rejects non-object JSON before the network call), invokes via `api.invokeMCPTool`, and renders a `ToolResult` panel: green for `ok`, amber for `is_error`, red for transport. Output (or error message) is shown in a scrollable monospace block capped at `max-h-64` so a chatty tool can't blow the layout.
  - `web/src/api.ts` — `api.invokeMCPTool(server, tool, args)`. `web/src/types/api.ts` — `MCPInvokeResponse`.
- **Tests**: 8 new in `mcp_write_test.go`:
  - `TestMCPInvoke_HappyPath` — sends `{q:"hello", n:42}`, asserts the fake server received the args nested under `"arguments"`.
  - `TestMCPInvoke_EmptyBodyMeansNoArgs` — empty POST body → `arguments:{}`.
  - `TestMCPInvoke_RejectsNonObjectBody` — table-driven for `"string"`, `42`, `[]`, `{`, `not json`. All return 400.
  - `TestMCPInvoke_UnknownServer` / `_UnknownTool` / `_BadToolName` — 404 / 404 / 400.
  - `TestMCPInvoke_ServerToolError` — fake server returns `isError:true`; response is `200 {ok:false, is_error:true}`.
  - `TestMCPInvoke_TransportError` — fake server returns 500 to `tools/call`; response is `200 {ok:false, is_error:false}`.
  - `TestMCPInvoke_ClipsLargeOutput` — output past `mcpInvokeMaxOutput` ends with `[truncated]`.
- **Verification**: `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./internal/{api,mcp}/...` all pass; `npm run lint`, `npx tsc --noEmit`, `vite build` clean.
- Bundle: 544 KB JS / 166 KB gz; 110 KB CSS / 19 KB gz (~4 KB JS / ~1 KB CSS net growth from 11c).
- Files touched: `internal/api/mcp_write.go` (new), `internal/api/mcp_write_test.go` (new), `internal/api/router.go`, `internal/api/types.go`, `web/src/types/api.ts`, `web/src/api.ts`, `web/src/components/MCPPanel.tsx`, `internal/api/dist/*` (rebuilt), `docs/implementation-tracker.md`.
- Manual verification still owed by user: with at least one MCP server in `mcp.json` (e.g. `npx mcp-server-fetch`), open `/mcp` → expand the server → click Run on a tool → seeded JSON appears in the textarea → click Invoke → green panel with the tool's text content. For a failing tool (e.g. fetch with a bad URL), expect the amber `is_error` panel.
- Phase 11 wrap-up: 11a (MCP client + boot) → 11b (read-only dashboard panels) → 11c (skills.sh install + delete) → 11d (MCP tool invocation). All four shipped today, all behind the existing bearer-auth (with `SKILLS_ADMIN` adding an extra gate over arbitrary code execution).

### 2026-04-30 — Slice 11c (skills.sh install + delete, admin gated)

- One atomic commit. `/skills` page now has working catalog browse + install + delete behind a config-flag gate.
- **Threat model**: `npx skills add <src>` runs arbitrary code from the catalog. Treat the install endpoint as a privileged operation. Hardening:
  - Off by default. New `SKILLS_ADMIN` env var (default `false`); the API returns 403 unless explicitly enabled. Frontend renders an inline banner explaining the toggle the first time a 403 is observed.
  - `source` is constrained by `^[A-Za-z0-9@:._/\-]{1,200}$` and rejects any segment containing `..`. We never invoke a shell — `os/exec` argv-only.
  - Subprocess env is sanitized to PATH + node lookup + npm config vars (drops `TELEGRAM_TOKEN`, `MISTRAL_API_KEY`, etc.) so install logs / errors can't leak Aura secrets to npm/skills.sh.
  - Install runs with a 90-second `context.WithTimeout` ceiling and `cwd = SKILLS_PATH`.
  - Delete runs `filepath.Rel` containment check after `filepath.Join` (catches `..`, absolute paths, Windows separators) and refuses symlinks via `os.Lstat`.
  - The deleter never recurses outside the configured skills directory.
- **Backend** (`internal/api`):
  - `types.go` adds `SkillCatalogItem`, `SkillInstallResponse`, `SkillDeleteResponse`.
  - `skills_catalog.go` (new) — proxies `skills.CatalogClient.Search` with `q` + `limit` query params. Returns `[]` for nil-client (so the frontend always sees an array). 502 on upstream failure.
  - `skills_write.go` (new) — `handleSkillInstall` validates the body, applies the admin gate, calls `Deps.SkillsInstaller.Install` with a 90s context. Truncates output at 2 KiB before returning. `handleSkillDelete` applies the gate and maps `ErrSkillNotFound` to 404, generic errors to 500.
  - `router.go` — `Deps` gains `SkillsCatalog`, `SkillsInstaller` (interface), `SkillsDeleter` (interface), `SkillsAdmin bool`. Three new routes (`GET /skills/catalog`, `POST /skills/install`, `POST /skills/{name}/delete`).
- **Skills runtime** (`internal/skills/admin.go`, new):
  - `NPXInstaller`: shells `npx skills add <src> [--skill <id>]` via `os/exec.CommandContext`. Picks `npx.cmd` on Windows. Sanitized env keeps PATH/PATHEXT/HOME/USERPROFILE/APPDATA/LOCALAPPDATA/TEMP/TMP/NODE_PATH/NPM_CONFIG_*; drops everything else.
  - `FSDeleter`: rejects empty names, traversal, symlinks, non-directories. Returns a package-internal sentinel for not-found that `IsSkillNotFound` reports on. Bot bridges this to `api.ErrSkillNotFound` via a small adapter to avoid an import cycle.
- **Config**: `internal/config` adds `SkillsAdmin bool` from `SKILLS_ADMIN` (default false). `.env.example` and `.env` both updated.
- **Bot wiring** (`internal/telegram/bot.go`): hoists `skillsCatalog` to a variable shared between the LLM tool and the API; constructs `NPXInstaller` + `FSDeleter` unconditionally so flipping the gate at runtime needs only a restart, not a rebuild; passes everything through `api.NewRouter`. New `skillsDeleterAdapter` translates the deleter's not-found sentinel.
- **Frontend** (`web/src/components/SkillsPanel.tsx`):
  - Tabs: Local (existing accordion + per-row Delete) and Catalog (search + install).
  - `useDebounce(value, 350ms)` proper-effect implementation throttles skills.sh queries to one per 350 ms of typing.
  - Install / Delete buttons surface `sonner.loading → success/error` toasts. The 403 path triggers a one-line `setAdminGated(true)` so the user sees the gate banner without having to read the network tab.
  - Empty Local state now points to the Catalog tab as the first install option.
  - SPA bundle: 540 KB JS / 165 KB gz; 109 KB CSS / 19 KB gz (~7 KB JS / ~2 KB CSS net).
- **Tests**: 19 total — 9 install (admin-off / nil-installer / empty source / 5 bad-source variants / bad skill_id / happy / failure-surfaces-output / output truncation), 5 delete (admin-off / bad-name / not-found / happy / generic error), 4 catalog passthrough (happy / query filter / nil client / upstream 500), and 4 in `internal/skills` (FSDeleter remove, not-found, traversal cases, symlink refusal — symlink test self-skips on platforms without unprivileged symlink support, e.g. Windows). One sanitized-env test in the same suite verifies secret env vars don't reach the subprocess. Race-clean.
- **Verification**: `go build ./...` clean, `go vet ./...` clean, `go test ./...` PASS, `go test -race ./internal/{api,skills}/...` clean. Frontend: `npm run lint` clean, `npx tsc --noEmit` clean, `vite build` ok.
- Files touched: `internal/config/config.go`, `internal/api/types.go`, `internal/api/router.go`, `internal/api/skills_catalog.go` (new), `internal/api/skills_write.go` (new), `internal/api/skills_catalog_test.go` (new), `internal/api/skills_write_test.go` (new), `internal/skills/admin.go` (new), `internal/skills/admin_test.go` (new), `internal/telegram/bot.go`, `web/src/types/api.ts`, `web/src/api.ts`, `web/src/components/SkillsPanel.tsx` (rewrite), `internal/api/dist/*` (rebuilt), `.env.example`, `.env`, `docs/implementation-tracker.md`.
- Manual verification still owed by user: set `SKILLS_ADMIN=true` in `.env`, restart Aura, log in to dashboard at `http://127.0.0.1:8081/skills`. Expect:
  1. Two tabs visible: Local + Catalog.
  2. Catalog tab populates from skills.sh; typing filters within ~350 ms.
  3. Click Install on a small skill — toast shows the npx command, then success or a clipped failure log.
  4. After install, switch to Local — the new skill appears.
  5. Click Delete on a local skill — confirm prompt, then toast on success.
  6. Set `SKILLS_ADMIN=false`, restart, retry: install/delete buttons return 403 and the amber banner appears in the panel.
- Next slice: **11d — invoke MCP tools from the dashboard.** `POST /api/mcp/{server}/tools/{tool}` (admin-gated reuse) with input-schema-driven form on `/mcp`.

### 2026-04-30 — Slice 11b (Skills + MCP dashboard panels)

- One atomic commit. Phase 11 read-only surface complete; mutation/invocation lands in 11c + 11d.
- **Backend** (`internal/api`):
  - `internal/api/types.go` — new DTOs: `SkillSummary` (name + description), `SkillDetail` (adds content + truncated flag), `MCPToolInfo` (name + description + input_schema), `MCPServerSummary` (name + transport + tool_count + tools[]).
  - `internal/api/skills.go` (new) — `handleSkillsList` returns `[]SkillSummary` (or `[]` when `Deps.Skills` is nil so the frontend always sees a valid array). `handleSkillGet` validates the path with `^[A-Za-z0-9_-]{1,64}$`, calls `Loader.LoadByName`, and truncates content at `maxSkillBodyChars=16000` with `truncated:true` so the dashboard can warn.
  - `internal/api/mcp.go` (new) — `handleMCPServers` enumerates `Deps.MCP []*mcp.Client`, skips nil entries, returns servers + tools sorted by name for deterministic rendering.
  - `internal/api/router.go` — `Deps` gains `Skills *skills.Loader` and `MCP []*mcp.Client`. Three new routes registered (`GET /skills`, `GET /skills/{name}`, `GET /mcp/servers`) inside the auth-wrapped mux.
  - `internal/mcp/client.go` — added `Transport()` getter and `TransportStdio` / `TransportHTTP` constants. Constructors set `transportKind` on the client struct.
  - `internal/telegram/bot.go` — passes `Skills: skillLoader, MCP: mcpClients` into `api.NewRouter`.
- **Frontend**:
  - `web/src/types/api.ts` — TS mirrors of the four new DTOs.
  - `web/src/api.ts` — `api.skills()`, `api.skill(name)`, `api.mcpServers()` (each goes through the same bearer-authed `get<T>` helper as the rest).
  - `web/src/components/SkillsPanel.tsx` (new) — accordion of local skills. Each row click lazy-fetches `/skills/{name}` and renders the full SKILL.md as a monospaced block. Truncation banner appears when content was clipped. Empty state shows `Sparkles` icon + a one-line "Drop a folder under skills/<name>/SKILL.md" CTA.
  - `web/src/components/MCPPanel.tsx` (new) — server cards with transport icon (`Server` for stdio, `Globe` for http) + tool count. Expanding a server reveals its tools as `mcp_<server>_<tool>` rows; each tool has a "show schema" toggle that pretty-prints the upstream `input_schema` JSON. Empty state guides the user to `mcp.example.json`.
  - `web/src/App.tsx` — `/skills` and `/mcp` routes added inside the auth'd `<Shell>`.
  - `web/src/components/Sidebar.tsx` — `Sparkles` (Skills) + `Plug` (MCP) nav items appended after Tasks.
  - `web/src/components/Shell.tsx` — keyboard chord shortcuts extended: `g k` → Skills, `g m` → MCP. Help dialog rows added.
  - SPA rebuilt into `internal/api/dist/`. Bundle: 533 KB JS / 163 KB gz; 107 KB CSS / 19 KB gz (~12 KB JS / ~2 KB CSS net growth).
- **Tests**: 7 new tests in `internal/api/skills_test.go` (empty / nil-loader / list returns / detail found / 404 / bad-name / nil-loader on detail / truncation) + 3 in `internal/api/mcp_test.go` (empty / populated with full tool metadata / nil-client). Both files use a stand-alone Deps with a real `skills.Loader` rooted at `t.TempDir()` (skills) or an in-memory `httptest` MCP fake (mcp).
- **Verification**: `go build ./...` clean, `go vet ./...` clean, `go test ./...` PASS, `go test -race ./internal/{api,mcp,tools}/...` clean. Frontend: `npm run lint` clean, `npx tsc --noEmit` clean, `npm run build` ok.
- Files touched: `internal/api/types.go`, `internal/api/router.go`, `internal/api/skills.go` (new), `internal/api/mcp.go` (new), `internal/api/skills_test.go` (new), `internal/api/mcp_test.go` (new), `internal/mcp/client.go`, `internal/telegram/bot.go`, `web/src/types/api.ts`, `web/src/api.ts`, `web/src/App.tsx`, `web/src/components/Sidebar.tsx`, `web/src/components/Shell.tsx`, `web/src/components/SkillsPanel.tsx` (new), `web/src/components/MCPPanel.tsx` (new), `internal/api/dist/*` (rebuilt), `docs/implementation-tracker.md`.
- Manual verification still owed by user: open `/skills` and confirm `aura-implementation` (and any other local skills) show up with descriptions; expand one and verify the SKILL.md body renders. Open `/mcp` — empty state should appear if no `mcp.json` exists; copy `mcp.example.json` → `mcp.json`, restart, verify both example servers appear (the example commands will likely fail to connect — that's expected; populate with real servers to see live tools).
- Next slice: **11c — skill install/delete (admin-gated).** `install_skill` (shells `npx skills add ...`), `delete_skill`, `create_skill` sandboxed via `os.Root`. New admin gate (`SKILLS_ADMIN=true` or per-user flag). Frontend: install button on catalog rows + delete on local-skill rows.

### 2026-04-30 — Slice 11a (MCP client + boot wiring)

- Phase 11 begins: skills + MCP, Picobot-style. 11a is pure plumbing — backend only, no user-visible UI yet (that lands in 11b).
- New `internal/mcp` package, ported from Picobot's `internal/mcp/client.go`:
  - `Client` with `NewStdioClient(name, command, args)` and `NewHTTPClient(name, url, headers)` constructors.
  - JSON-RPC 2.0 envelope: `initialize` (clientInfo `aura/3.0`, protocolVersion `2025-03-26`) → `notifications/initialized` (fire-and-forget) → `tools/list` to populate `Client.Tools()`. `CallTool` posts `tools/call` and concatenates the `content[].text` items, surfacing `isError:true` as a Go error.
  - `stdioTransport`: `exec.Command(command, args...)` with stdin pipe write + scanner read; per-request mutex; line-delimited JSON-RPC; `Close()` kills the process. Server notifications without `id` are skipped.
  - `httpTransport`: per-request `POST` to the configured URL; honors `Mcp-Session-Id` header round-trip; accepts `application/json` or `text/event-stream` responses (parses the first `data: {…id…}` frame from SSE). HTTP 202 → empty `{}` (notification-style notify); non-200 → error with body.
  - `Tool` struct exposes `Name`, `Description`, `InputSchema map[string]any`.
- New `internal/mcp/config.go` — `LoadServers(path)` loader for `mcp.json`:
  - File schema is `{"mcpServers": {"<name>": {"command":..., "args":..., "url":..., "headers":...}}}`. Empty path or missing file returns empty map (opt-in, no warning); malformed JSON is fatal so misconfig surfaces fast.
  - `DisallowUnknownFields` so typos don't silently degrade.
  - Per-entry validation: name regex `^[A-Za-z0-9_-]{1,32}$` (so the registered tool name `mcp_<server>_<tool>` stays sane); exactly one of `command` / `url` must be set.
- New `internal/tools/mcp.go` — `MCPTool` adapter implementing the existing `tools.Tool` interface:
  - `Name()` → `mcp_<server>_<tool>` (collision-proof across servers + native tools).
  - `Description()` → `[MCP: <server>] <upstream desc>` so the LLM can tell at a glance the tool came from MCP.
  - `Parameters()` returns the upstream `inputSchema` unchanged when present; otherwise an empty `{type:object, properties:{}}` so providers requiring a schema don't reject the registration.
  - `Execute(ctx, args)` proxies to `client.CallTool`; nil-client guard for safety.
- Config: new `MCP_SERVERS_PATH=./mcp.json` env (default tracks repo root). `mcp.json` itself is gitignored; `mcp.example.json` is committed as the template (one stdio entry, one HTTP entry).
- Bot boot wiring (`internal/telegram/bot.go`):
  - After all native tools are registered, `mcp.LoadServers(cfg.MCPServersPath)` is called. On error: warn + continue (no MCP). On success: each server is connected (stdio or HTTP per config), discovered tools wrapped via `NewMCPTool` and registered in the same `tools.Registry` the LLM sees. Connection failures are warned per-server, never fatal — a flaky third-party MCP server doesn't kill the bot.
  - `Bot.mcpClients []*mcp.Client` retained on the struct. `Bot.Stop()` calls `Close()` on every client (stdio servers get their child process killed; HTTP is a no-op).
  - Logs only `server` + `tools` count on registration. No URLs, no headers, no tool args printed.
- Tests:
  - `internal/mcp/client_test.go` (10 tests): HTTP `initialize`+`tools/list` happy path; `tools/call` round-trip; `tools/call` with `isError:true`; SSE response parsing; HTTP 500 on initialize; `LoadServers` empty path / missing file / valid mixed config / both-transports-set rejection / neither-transport-set rejection / bad-name rejection / unknown-top-level-field rejection.
  - `internal/tools/mcp_test.go` (5 tests): name format; description prefix; schema pass-through; schema fallback when nil; `Execute` round-trip via in-memory MCP server; nil-client rejection.
- Verification: `go build ./...` clean; `go vet ./...` clean; `go test ./...` PASS (full suite); `go test -race ./internal/mcp/... ./internal/tools/...` clean.
- Side fix: `go mod tidy` promoted `github.com/skip2/go-qrcode` from indirect to direct (already used by slice 10i but wasn't in the direct require block, which the IDE flagged).
- Files touched: `internal/mcp/client.go` (new), `internal/mcp/config.go` (new), `internal/mcp/client_test.go` (new), `internal/tools/mcp.go` (new), `internal/tools/mcp_test.go` (new), `internal/config/config.go`, `internal/telegram/bot.go`, `.env.example`, `.env`, `.gitignore`, `mcp.example.json` (new), `go.mod`, `docs/implementation-tracker.md`.
- Next slice: **11b — skills + MCP dashboard panels.** Read-only `/api/skills` and `/api/mcp/servers` endpoints + new `/skills` and `/mcp` SPA routes (cards listing local skills and connected MCP servers with their discovered tool counts/schemas). Bearer auth like the rest.

### 2026-04-30 - Skills discovery and local skill loading

- Added read-only Aura skills support using Picobot's `skills/<name>/SKILL.md` pattern: `internal/skills` loads validated local skills from `SKILLS_PATH`, skips broken drafts, and renders a bounded prompt block on every Telegram turn.
- Added `search_skill_catalog`, a read-only skills.sh catalog search tool. `list_skills` / `read_skill` inspect only locally installed skills; installation/mutation remains deferred behind a future admin/review flow.
- Config now includes `SKILLS_PATH=./skills` and `SKILLS_CATALOG_URL=https://skills.sh/`. Added `skills/README.md` with the local skill format.
- Verification: live `skills.sh` parser check found catalog entries; `go test ./internal/skills ./internal/tools ./internal/config ./internal/telegram ./internal/conversation`, `go test ./...`, `go build ./...`, and `go vet ./...` passed.

### 2026-04-30 - Polish and harden Telegram login

- Hardened the Telegram login surface by removing the external QR image dependency. Aura now serves `GET /telegram/qr.png` locally as a generated PNG for `https://t.me/<bot>?start=login`.
- `GET /telegram` now includes `qr_url`, sets no-store/nosniff headers, and only accepts valid Telegram-style usernames. Invalid or missing bot usernames return 503 instead of emitting malformed links.
- Login UI now uses the local QR endpoint and has clearer loading/unavailable copy when the bot metadata is not ready.
- Verification: `npm run lint`, `npx tsc --noEmit`, `npm run build`, `go test ./...`, `go build ./...`, and `go vet ./...` passed.

### 2026-04-30 - Fix mobile sheet trigger crash

- Fixed a dashboard crash where Radix threw ``DialogTrigger` must be used within `Dialog``. Root cause: `Shell.tsx` rendered `SheetTrigger` outside its `<Sheet>` provider. Since the sheet is already controlled by React state, the mobile hamburger now opens it directly with `setMobileOpen(true)`.
- Rebuilt embedded dashboard assets.
- Verification: `npm run lint`, `npx tsc --noEmit`, `npm run build`, `go test ./...`, `go build ./...`, and `go vet ./...` passed.

### 2026-04-30 - Telegram QR/link on login

- Restored the missing Telegram entry point on the dashboard login screen: it now shows the running bot handle, a clickable `t.me` link, and a QR code for `https://t.me/<bot>?start=login`.
- Added public `GET /telegram` on the health server. It exposes only bot link metadata (`username`, `url`, `start_url`) and does not mint or validate dashboard tokens.
- Reserved `/telegram` in the embedded SPA fallback so the React app does not shadow the JSON endpoint.
- Verification: `go test ./...`, `go build ./...`, `go vet ./...`, `npm run lint`, `npx tsc --noEmit`, and `npm run build` all passed.

### 2026-04-30 - Bootstrap login fix

- Fixed the first-run auth trap introduced by slice 10d: Aura can now start with `TELEGRAM_ALLOWLIST` blank.
- Blank allowlist mode is one-user bootstrap mode. The first Telegram user who sends `/start` is persisted in the existing SQLite auth DB (`allowed_users`) and receives a dashboard token immediately. Later `/start`, `/login`, or `/token` requests from that same user mint fresh tokens without going through the LLM.
- If `TELEGRAM_ALLOWLIST` is configured, bootstrap mode is disabled and the env allowlist remains the source of truth.
- Login page copy now tells users to use `/start` for first setup or `/login` for a fresh token.
- Verification: `go test ./...`, `go build ./...`, `go vet ./...`, `npm run lint`, `npx tsc --noEmit`, and `npm run build` all passed.
- Files touched: `internal/config/config.go`, `internal/config/config_test.go`, `internal/auth/store.go`, `internal/auth/store_test.go`, `internal/telegram/bot.go`, `internal/telegram/bot_test.go`, `web/src/components/Login.tsx`, `.env.example`, `docs/implementation-tracker.md`, plus rebuilt `internal/api/dist/*`.

### 2026-04-30 — Slice 10e complete (polish + theme redesign)

- Single atomic commit. Phase 10 (UI) is now fully landed.
- **Backend touch (`/api/health` metadata)**:
  - `internal/api/types.go` — `HealthRollup` gains a `Process` block: `version`, `git_revision`, `started_at`, `uptime_seconds`. The frontend dashboard footer renders these.
  - `internal/api/router.go` — `Deps` gains `Version` + `StartedAt` fields.
  - `internal/api/health.go` — populates `Process`. `git_revision` is read once via `runtime/debug.ReadBuildInfo()` (vcs.revision setting), short-truncated to 7 chars, cached in a `sync.Once`. Avoids ldflags plumbing entirely; works whenever the binary was built inside a git tree.
  - `internal/telegram/bot.go` — passes `Version: "3.0"` (matching `cmd/aura/main.go`'s `auraVersion` const) and `StartedAt: time.Now().UTC()` into `api.NewRouter`. Hardcoded with a comment because `cmd/aura` isn't importable; if version churn becomes a thing, lift it into `internal/config`.
- **Frontend polish**:
  - New `web/src/components/ui/skeleton.tsx` (standard shadcn `<Skeleton>`).
  - All four data panels swap their text-only "Loading…" for layout-faithful skeletons: `DashboardSkeleton` (3-card grid), `WikiSkeleton` (5 row stubs), `SourceInboxSkeleton` (drop-zone + 2 status sections), `TasksSkeleton` (header + 3 task rows). Reduces layout shift on first paint.
  - Empty states get visual weight: WikiPanel shows a `BookText` icon + "Drop a PDF on /sources or chat with the bot" CTA when `data.length === 0`; TasksPanel shows a `Calendar` icon + "+ New task" hint inside a dashed-border block when no tasks exist.
  - `ErrorBoundary` fires `sonner.error(message, { description: 'Check the console…', duration: 6000 })` from `componentDidCatch` so failures pop above the fold even on long pages.
  - `useAppTheme.readInitialTheme` flipped to dark-by-default — only honors an explicit `prefers-color-scheme: light` system setting; otherwise dark.
  - New `web/src/components/Shell.tsx` consolidates the auth'd shell layout: desktop sidebar always-visible at `md+`, mobile collapses into a `Sheet`-backed slide-over triggered by a hamburger in a top bar that only renders below `md`. App.tsx swapped the inline flex layout for `<Shell>`. The `Sidebar` component now takes an optional `onNavigate` callback so mobile nav clicks close the drawer.
  - Global keyboard shortcuts: `useKeyboardShortcuts` installs a single `keydown` listener with a tiny chord state machine. `?` opens a help dialog (rolled by hand instead of pulling Radix Dialog into the shell) listing all shortcuts. `g` followed by `h/w/g/s/t` navigates to home/wiki/graph/sources/tasks within a 1.2s window. The handler skips when the focused element is an input/textarea/select/contenteditable so chords never hijack form typing.
- **Theme redesign from the logo**:
  - Studied `Logo/loho new.png` — deep-navy disc with an electric cyan-blue arrow-A glyph and a subtle teal halo. Translated to oklch tokens.
  - `web/src/index.css` — rewrote three palette blocks. Light mode: white-paper canvas + `oklch(0.62 0.16 245)` electric blue as `--primary` (single saturated accent; everything else stays neutral). Dark mode (`[data-theme="dark"]` AND `.dark` — both apply because useAppTheme sets both selectors): deep navy background `oklch(0.16 0.03 250)`, lifted card `oklch(0.21 0.035 250)`, slightly darker sidebar `oklch(0.18 0.035 250)`, brighter cyan `oklch(0.7 0.18 240)` for primary, even brighter `oklch(0.75 0.2 235)` for the focus ring. Matched the `--bg`/`--surface` Sacchi-legacy variables (still used by chat/wiki panels) so the chat surface looks consistent. Both `[data-theme="dark"]` and `.dark` blocks updated and noted to keep in sync.
  - Ambient aurora — two soft radial spotlights (top-right cyan + bottom-left indigo at 6-8% alpha) baked into `body` background under `.dark` and `[data-theme="contrast"]`. Adds subtle depth without affecting readability.
  - Inline-SVG brand mark: `BrandMark` in Sidebar (36×36, soft halo) + `LoginBrandMark` on the unauth login page (64×64 with a stronger halo + an extra radial gradient behind it). Both render the arrow-A glyph from the logo using `var(--primary)` so they retint with the theme.
  - Sidebar header upgraded to brand mark + tracked-letter "SECOND BRAIN" eyebrow under the wordmark.
  - Active nav items: `bg-primary/10 text-primary font-medium ring-1 ring-primary/20 shadow-[0_0_20px_-8px_var(--primary)]` — gives the active row a soft cyan glow that's clearly the brand color without being neon-loud.
  - HealthDashboard `Card` gets a hover stripe (top-edge gradient that fades in) and `hover:border-primary/30`. The `StatusBar` swaps zinc/blue/emerald/rose for slate/sky/primary/rose so the "ingested" bucket renders in the brand color (visual reinforcement that ingestion is the success path).
  - Dashboard heading gets a subtitle ("Live health rollup · refreshes every 5s") so the page header scans more like a 2026 dashboard than a placeholder.
  - All `.sacchi-*` legacy CSS untouched — those classes power the chat/product views which weren't part of the dashboard surface and don't need 10e treatment.
- Verification: `go build ./...` clean, `go vet ./...` clean, `go test ./...` PASS, `npm run lint` clean, `npx tsc --noEmit` clean, `vite build` ok (521 KB JS / 161 KB gz; 105 KB CSS / 18 KB gz; CSS grew ~7 KB from new tokens).
- Files touched: `internal/api/types.go`, `internal/api/router.go`, `internal/api/health.go`, `internal/telegram/bot.go`, `web/src/index.css`, `web/src/hooks/useAppTheme.ts`, `web/src/types/api.ts`, `web/src/components/ui/skeleton.tsx` (new), `web/src/components/Shell.tsx` (new), `web/src/components/Sidebar.tsx`, `web/src/components/Login.tsx`, `web/src/components/HealthDashboard.tsx`, `web/src/components/WikiPanel.tsx`, `web/src/components/SourceInbox.tsx`, `web/src/components/TasksPanel.tsx`, `web/src/components/ErrorBoundary.tsx`, `web/src/App.tsx`, `internal/api/dist/*` (rebuilt), `docs/implementation-tracker.md`.
- Manual verification still owed by user: dark theme renders by default; mobile drawer slides on a narrow window; `?` opens the shortcut help; `g w` navigates to /wiki; sidebar BrandMark glows; login page shows the larger glowing orb.
- Phase 10 complete. **Next phase TBD** — possible follow-ups: `last_error` per-subsystem plumbing (deferred from 10e per the design doc), Prometheus `/metrics`, Lighthouse CI, Playwright auth-flow smoke.

### 2026-04-30 — Slice 10d complete (bearer auth + Telegram-issued tokens)

- Two atomic commits. **A** (`a4d3fdf`): backend (auth package, middleware, /auth/{whoami,logout} endpoints, request_dashboard_token tool, dropping requireLoopback). **B** (this commit): frontend wiring (Login.tsx, Authorization header, route guard, Sign-out button) + tracker + SPA rebuild.
- **Threat model addressed**: every `/api/*` request requires a valid bearer. Tokens are minted only through Telegram — there is no public `/api/auth/login`. The Telegram allowlist remains canonical: `RequireBearer` re-checks the user against `cfg.IsAllowlisted` on every request, so removing a user from `TELEGRAM_ALLOWLIST` immediately revokes dashboard access without separate plumbing.
- Backend (commit A):
  - `internal/auth/store.go` — `api_tokens` table on the existing scheduler SQLite file (single backup artifact). Tokens are 32 random bytes encoded as base64url (~43 chars); only the SHA-256 hash is persisted. `Lookup` uses `crypto/subtle.ConstantTimeCompare` defensively even though SQLite already keys on the hash. `last_used` updated inline (MVP — design notes a 30s batch if it shows up as a hot row). Sentinel `ErrInvalid` covers unknown / malformed / revoked uniformly so middleware can't accidentally enumerate token state.
  - `internal/auth/middleware.go` — `RequireBearer(store, allowlist, logger, next)` extracts `Bearer <token>`, calls `store.Lookup`, rechecks the allowlist, stashes user ID via a private context key. 401 JSON body on every failure path. Token text never logged (a leak there would defeat hashing).
  - `internal/api/auth.go` — `GET /auth/whoami` (echoes the user ID resolved from the bearer; cheap), `POST /auth/logout` (revokes the request's bearer; idempotent — second logout still returns 200).
  - `internal/api/router.go` — `Deps` gains `Auth *auth.Store` + `Allowlist auth.AllowlistFunc`. When `Auth` is non-nil the entire mux is wrapped in `RequireBearer` — every route, including `/auth/whoami` and `/auth/logout`. Tests that don't need auth pass `Auth: nil` and the router stays unwrapped.
  - `internal/tools/auth.go` — `RequestDashboardTokenTool` issues a fresh token, allowlist-checks defensively, ships the plaintext via `TokenSender` (an interface the bot satisfies). Critical: the LLM tool result confirms delivery but never contains the token. On Telegram send failure, the freshly-issued token is revoked so the partial state can't leave a usable bearer floating in the DB. Constructor returns nil if any dep is nil so the bot can skip registration cleanly when auth isn't configured.
  - `internal/telegram/bot.go` — opens `auth.OpenStore` on the same SQLite file as scheduler. New `Bot.SendToUser(userID, message)` method satisfies `tools.TokenSender` (parses the user ID as a chat ID and calls `bot.Send(tele.ChatID(...))`). `request_dashboard_token` registered after `b` is constructed so the bot can be its own sender. `api.NewRouter` call now passes `Auth` + `Allowlist`.
  - Tests: 12 store/middleware tests (round-trip, empty user, unknown / empty / revoked tokens, double-revoke, token uniqueness over 50 issuances, multi-user isolation, header parsing edge cases, case-insensitive scheme, revoked + de-allowlisted rejection paths). 7 router-level integration tests (401 unauthed, 200 authed, revoked → 401, de-allowlisted → 401, write endpoints gated, /auth/whoami, /auth/logout revoke flow). 5 tool tests (happy path with leak-check on the result string, no-context, non-allowlisted, send failure → revoke, nil-arg constructor). Race-clean.
  - `requireLoopback` retired — auth supersedes it. `TestWrite_RejectsNonLoopback` removed from `writes_test.go`. The `doLocal` helper there is now a vestige (its `RemoteAddr=127.0.0.1` line is no-op without the gate) but kept to minimize churn in this slice; harmless.
- Frontend (this commit):
  - `web/src/lib/auth.ts` — `getToken`/`setToken`/`clearToken` localStorage helpers under key `aura_token`. Catches localStorage exceptions (private browsing) so they degrade to silent failure rather than crash.
  - `web/src/api.ts` — `authHeaders()` attaches `Authorization: Bearer <token>` on every fetch. `handle401()` clears the stored token and bounces to `/login?expired=1` (with a redirect-loop guard for when we're already on /login). `readError()` extracted as a shared helper since the 401 path now needs the same JSON-error parsing as the success path. Two new methods: `whoami()` and `logout()`. `WhoamiResponse` added to `types/api.ts`.
  - `web/src/components/Login.tsx` — single-input paste-token form. On mount, if a token already exists, it fires a silent `whoami()` and either navigates home (still valid) or clears the token (rejected). `?expired=1` query param shows an explicit "session expired or was revoked" hint above the form so returning users know why they're back at the login screen. Token uses `<input type="password">` to keep it off the screen during paste; `autoComplete="off"` so browsers don't autofill.
  - `web/src/App.tsx` — top-level route refactor. `/login` is unauth'd; everything else goes through a `RequireAuth` wrapper that reads `getToken()` synchronously and bounces to `/login` if missing. Avoids the initial flash of "Loading…" / "Error: unauthorized" that the api.ts redirect alone would produce. The real validity check still happens on the first API call.
  - `web/src/components/Sidebar.tsx` — Sign-out button in the footer next to the theme toggle. Calls `api.logout()` (best-effort — server-side revoke is hardening, not a correctness gate), then `clearToken()` + navigate to `/login`. Sonner toast confirms.
  - SPA rebuilt into `internal/api/dist/`. Bundle sizes essentially unchanged from 10c.
- Verification: `go build ./...` clean, `go vet ./...` clean, `go test ./...` PASS, `go test -race ./internal/{api,auth,tools}/...` clean, `npm run lint` clean, `npx tsc --noEmit` clean.
- Bootstrap recipe (manual verification still owed by user):
  1. Start the bot: `go run ./cmd/aura`
  2. In Telegram, send "give me a dashboard token" (or similar).
  3. The bot replies with a token. Copy it.
  4. Open `http://127.0.0.1:8081/` → redirected to `/login`.
  5. Paste token, click Sign in. Dashboard loads.
  6. Click Sign out. Token revoked server-side; back at `/login`. Re-pasting the old token shows the rejection message.
- Files touched (this commit): `web/src/api.ts`, `web/src/types/api.ts`, `web/src/lib/auth.ts` (new), `web/src/components/Login.tsx` (new), `web/src/components/Sidebar.tsx`, `web/src/App.tsx`, `internal/api/dist/*` (rebuilt), `docs/implementation-tracker.md`.
- Next slice: **10e — polish** (mobile drawer, dark-mode default, empty states, loading skeletons, keyboard shortcuts, observability surfaced on `/api/health`).

### 2026-04-30 — Slice 10c complete (write actions)

- Two atomic commits. **A**: `slice 10c: write endpoints (sources/wiki/tasks)`. **B**: this commit — frontend wiring + tracker update + SPA rebuild.
- Backend (commit A, `5611e7d`):
  - `internal/api/router.go` — `Deps` gains `Location *time.Location` for daily HH:MM resolution; `SchedulerStore` interface gains `Upsert` + `Cancel`. Six new routes registered behind `requireLoopback` (POST `/sources/{id}/ingest`, `/sources/{id}/reocr`, `/wiki/index/rebuild`, `/wiki/log`, `/tasks`, `/tasks/{name}/cancel`).
  - `internal/api/sources_write.go` (new) — `handleSourceIngest` re-runs `Pipeline.AfterOCR` (idempotent because `Compile` rewrites the same slug); status precondition is `ocr_complete` or `ingested`, returns 409 otherwise. `handleSourceReocr` reads `original.pdf` via `Path`, reruns `OCR.Process`, rewrites `ocr.md`/`ocr.json`, flips status, then chains `AfterOCR` when `Ingest` is wired. Both return 503 when the relevant client is nil so the dashboard can show a clear "set MISTRAL_API_KEY" message instead of a generic 500. `decodeJSONBody` helper caps body at 64 KiB and disallows unknown fields.
  - `internal/api/wiki_write.go` (new) — `handleWikiRebuild` calls `wiki.Store.RebuildIndex`. `handleWikiAppendLog` validates action against a `[A-Za-z0-9_.-]{1,32}` regex and asserts `wiki.Slug(slug) == slug` so log.md can't be smuggled into. Both go through a private `wikiWriter` interface so the public `WikiStore` type stays read-only at the contract level.
  - `internal/api/tasks_write.go` (new) — `handleTaskUpsert` mirrors the `schedule_task` LLM tool semantics: name regex, kind in {reminder, wiki_maintenance}, exactly one of `at` (RFC3339 UTC) or `daily` (HH:MM in local TZ), reminder requires `recipient_id` (no user-context shortcut from HTTP). `handleTaskCancel` flips active → cancelled and disambiguates 404 vs 409 via a follow-up `GetByName` so the UI shows "already cancelled" vs "no such task" cleanly.
  - `internal/api/writes_test.go` (new, 21 test funcs) — uses `doLocal` (RemoteAddr=127.0.0.1) and `doRemote` (default 192.0.2.1) helpers to cover both happy paths and the loopback gate. Negative cases: bad IDs, disabled OCR/Ingest (503), missing/malformed JSON, every input-validation branch on tasks (missing fields, both at+daily, reminder without recipient, past at, bad daily format).
  - `internal/telegram/bot.go` — passes `time.Local` into `Deps.Location` so daily schedules in the API resolve in the same TZ as the LLM tool.
  - Verification at commit A time: `go test ./internal/api/...` 35 tests PASS (14 existing + 21 new); `go test ./...` clean; `go build ./...` clean; `go vet ./...` clean; `go test -race ./internal/api/...` clean.
- Frontend (this commit):
  - `web/src/types/api.ts` — adds `IngestResponse`, `ReocrResponse`, `UpsertTaskRequest` interfaces mirroring the Go DTOs.
  - `web/src/api.ts` — new `post<T>(path, body?)` helper (no 8s GET timeout because OCR can run for minutes); six new methods: `ingestSource`, `reocrSource`, `rebuildWikiIndex`, `appendWikiLog`, `upsertTask`, `cancelTask`.
  - `web/src/components/SourceInbox.tsx` — new "Actions" column. `SourceActions` subcomponent renders Re-OCR for `stored`/`failed`, Ingest for `ocr_complete`/`failed`, nothing for `ingested`. Per-row in-flight tracking via `busyIds: Set<string>` so the same button can't be double-clicked. Sonner toasts (`loading` → `success`/`error`) and `refetch()` on success so the table updates immediately rather than waiting for the 5 s poll.
  - `web/src/components/TasksPanel.tsx` — header gains a "+ New task" button; per-row Cancel button on `active` rows. `NewTaskDialog` wraps a `NewTaskForm` keyed on `open` so each open mounts fresh state (sidesteps the `react-hooks/set-state-in-effect` lint rule that blocked the naive useEffect-reset approach). Form supports both `wiki_maintenance` and `reminder` kinds, with `recipient_id` rendered conditionally. `<input type="datetime-local">` for `at` mode is converted to UTC RFC3339 via `new Date(at).toISOString()`.
  - `web/src/components/WikiPanel.tsx` — header gains a "Rebuild index" button. Toast → refetch on success.
  - SPA rebuilt into `internal/api/dist/`. New CSS bundle 98 kB → 17 kB gzipped; main JS bundle 504 kB → 156 kB gzipped (the 500 kB warning is the existing graph view + Tiptap reference; not 10c-specific).
- Verification (this commit): `go build ./...` clean; `go vet ./...` clean; `go test ./...` full suite PASS; `npm run lint` in `web/` clean; `npx tsc --noEmit` clean.
- All write endpoints stay loopback-only (`requireLoopback` middleware). LAN exposure remains gated until **slice 10d** ships bearer auth.
- Files touched (this commit): `internal/api/dist/*` (rebuilt), `web/src/api.ts`, `web/src/types/api.ts`, `web/src/components/SourceInbox.tsx`, `web/src/components/TasksPanel.tsx`, `web/src/components/WikiPanel.tsx`, `docs/implementation-tracker.md`.
- Manual verification still owed by user: open the dashboard at `http://127.0.0.1:8081/`, confirm (a) re-OCR + ingest buttons appear on the right rows, (b) "+ New task" dialog round-trips a daily-recurring task, (c) Cancel flips an active task to cancelled, (d) "Rebuild index" on `/wiki` succeeds.
- Next slice: **10d — bearer-token auth** so the listener can be exposed beyond loopback. Then **10e** for polish (empty-state copy, error retry UX, accessibility pass).

### 2026-04-30 — Browser PDF upload (10c mini-slice)

- One-shot mini-slice carved out of 10c so the user could drop PDFs onto the dashboard immediately. The remaining 10c endpoints (ingest, reocr, cancel, rebuild) stay deferred.
- Backend (`380d7f2`):
  - `internal/api/upload.go` — `POST /sources/upload` handler. Multipart parse (`OCR_MAX_FILE_MB` cap, default 100), filename + ext check, `source.Store.Put` → `ocr.Client.Process` → atomic `ocr.md` + `ocr.json` write → status flip to `ocr_complete` → `ingest.Pipeline.AfterOCR` for auto-ingest. Mirrors `internal/telegram/documents.go` step-for-step minus the Telegram progress UX. `UploadResponse` DTO carries `id`, `status`, `duplicate`, `filename`, `page_count`, `wiki_pages`, `ingest_note`, `ocr_error`, and a human-friendly `note` summary.
  - `requireLoopback` middleware in the same file: `net.SplitHostPort(r.RemoteAddr)` + `IsLoopback()`, returns 403 otherwise. Does NOT honor `X-Forwarded-For` since there's no reverse proxy. This is the gate that protects the write surface until 10d ships bearer auth.
  - `internal/api/router.go` — `SourceStore` interface gains `Put` + `Update` (writes were previously read-only). `Deps` gains `OCR`, `Ingest`, `MaxUploadMB`. Route registered through `requireLoopback`.
  - `internal/telegram/bot.go` — passes `ocrClient`, `ingestPipeline`, and `cfg.OCRMaxFileMB` to `api.NewRouter`.
- Frontend (`380d7f2`):
  - `web/src/types/api.ts` — `UploadResponse` interface mirrors the Go DTO.
  - `web/src/api.ts` — `api.uploadSource(File)` wraps a multipart POST. Bypasses the 8 s GET timeout intentionally — OCR can take minutes for large PDFs.
  - `web/src/components/SourceInbox.tsx` — drop zone + hidden `<input type="file" multiple accept=".pdf">`. Drag-and-drop on the outer container with the standard `dragOver`/`dragLeave`/`drop` handlers. Sequential per-file uploads with `sonner` `toast.loading` → `toast.success`/`toast.error`. After each upload, `refetch()` from `useApi` triggers an immediate poll so the table reflects the new `ingested` row without waiting for the 5 s tick.
- `.env` updated to `HTTP_PORT=127.0.0.1:8081` (was `:8081`, LAN-wide). `.env.example` already had `127.0.0.1:8080` from slice 10b.
- Live verification on `6MBU00242200.pdf`:
  - `src_67467125f865d781` directory created with `original.pdf` (229 952 bytes), `ocr.md`, `ocr.json`, `source.json` (status=`ingested`, OCR model `mistral-ocr-latest`, 1 page).
  - Wiki page `wiki/source-6mbu00242200.md` (1 911 bytes) generated with proper frontmatter (`category: sources`, `sources: [source:src_67467125f865d781]`, schema v2, prompt `ingest_v1`).
  - `wiki/index.md` and `wiki/log.md` rebuilt by the wiki maintenance hook.
  - Total elapsed ~1.4 s (PDF stored 10:23:13.65 UTC → wiki page written 10:23:15 UTC).
- Verification commands run: `go build ./...` clean; `go vet ./...` clean; `go test ./...` full suite PASS; `npm run lint` + `npx tsc --noEmit` in `web/` clean.
- Files touched: `internal/api/router.go`, `internal/api/upload.go` (new), `internal/telegram/bot.go`, `web/src/api.ts`, `web/src/types/api.ts`, `web/src/components/SourceInbox.tsx`, `internal/api/dist/*` (rebuilt), `.env` (port binding).
- Next: rest of slice **10c** — `POST /api/sources/{id}/ingest`, `POST /api/sources/{id}/reocr`, `POST /api/tasks/{name}/cancel`, `POST /api/tasks` (upsert), `POST /api/wiki/index/rebuild`, `POST /api/wiki/log`. All gated by the same `requireLoopback` middleware until 10d. UI: ingest button on stored/failed source rows, cancel button on active tasks, "+ New task" dialog on `/tasks`, "Rebuild index" overflow on `/wiki`.

### 2026-04-30 — Slice 10b complete (frontend scaffold + wiki/graph views)

- Slice 10b shipped via 6 intermediate commits (`53ad7ab` → `9f0c01f` → `49c0b6b` → `70b2ce6` plus Phase 4 + final). Approach 1 from the design doc: copy from `D:\sacchi_Agent\frontend\src-app` and prune sacchi-specific files, rewire to Aura's `/api/*` endpoints from slice 10a.
- New `web/` directory: React 19 + Vite + TypeScript + Tailwind v4 + shadcn/ui. Pruned deps (~6 npm packages dropped — copilot, ag-ui, cmdk, vaul). Added `react-router-dom@7`. `vite.config.ts` writes build output directly to `internal/api/dist/` so `//go:embed` reads it without a copy step.
- 5 client-side routes: `/` HealthDashboard, `/wiki` WikiPanel, `/wiki/:slug` WikiPageView, `/graph` WikiGraphView (lazy-loaded, force-graph-2d), `/sources` SourceInbox, `/tasks` TasksPanel. SPA fallback in `internal/api/static.go` handles deep-link refresh.
- New components written from scratch against Aura's API: `HealthDashboard`, `SourceInbox`, `TasksPanel`, `WikiPageView` (read-only via react-markdown), `ErrorBoundary`. Sacchi components rewritten: `App`, `Sidebar`, `WikiPanel`, `WikiGraphView`, `EventStrip` (stub), `WikiEditor` (stub).
- `useApi` hook: shared fetch + 5s polling with `document.visibilityState` pause, stale-with-pill on subsequent failures, 8s `AbortController` timeout. No SWR / TanStack Query.
- Hand-written DTOs in `web/src/types/api.ts` mirroring `internal/api/types.go`. ~80 LOC.
- Theme handling: kept sacchi's three-theme `useAppTheme` (`light` | `dark` | `contrast`) intact; Sidebar uses `cycleTheme` and per-theme icons. Adapted via approach A from the design's gray-area question.
- Backend changes: `internal/config/config.go` HTTPPort default `:8080` → `127.0.0.1:8080`; `.env.example` updated with comment about LAN exposure deferring to slice 10d. `internal/health/server.go` deletes `handleLanding` + `landingPage` HTML constant; `go-qrcode` dep removed via `go mod tidy`. `internal/api/static.go` provides multi-frame `//go:embed all:dist` + SPA fallback handler with `ErrNoStaticAssets` for the pre-build state. `cmd/aura/main.go` mounts the static handler after the API on the same health server mux.
- Tray gains "Open Dashboard" menu item that shells out to `rundll32 url.dll,FileProtocolHandler` with the URL derived via new `dashboardHost` helper (`:8080` → `localhost:8080`, `0.0.0.0:port` → `localhost:port`, anything else passthrough).
- `Makefile` gains `web` (vite dev), `web-build` (npm install + npm run build), `ui-dev` (parallel bot + vite).
- Verification: `go vet` + `go test` clean across `internal/api`, `internal/health`, `internal/config`, `internal/tray`. `go test -race ./internal/api/...` clean. `tsc --noEmit` clean. `npm run lint` clean (after fixing one `react-hooks/purity` violation in the Countdown component — pinned `now` to state instead of calling `Date.now()` during render). Sacchi files retain `/** @ts-nocheck */` headers we kept; not blocking.
- Deferred to user: full-tree `go build ./...` was scoped to in-slice packages because `cmd/build_icon/main.go` had a parallel in-flight edit. The user landed `6584a16` mid-execution which fixed it; final tree should now build clean.
- Files touched (commit-by-commit summary):
  - `53ad7ab` 10b prep: localhost binding + static handler scaffold (config/.env.example/health/api/static.go + tests)
  - `9f0c01f` 10b WIP: copy sacchi → web/ and prune (whole `web/` tree, sacchi-specific files deleted, package.json + vite.config.ts + index.html rewritten)
  - `49c0b6b` 10b WIP: types + api client + useApi hook
  - (Phase 4 commit, name varies by squash) new components
  - `70b2ce6` 10b WIP: adapt copied components to /api/* and react-router
  - Final commit (this commit): build SPA, wire static handler in main, tray Open Dashboard, Makefile, tracker update.
- Manual verification still owed by user at that time: `go run ./cmd/aura`, then http://localhost:8080/ should render the dashboard; the historical slice 10b checklist was the canonical list. The tray's Open Dashboard launches the browser.
- Next slice: **10c — UI write actions** (POST endpoints + ingest/cancel/rebuild buttons). Or 10d (auth) if LAN exposure is needed sooner.

### 2026-04-30 — Slice 10a complete (read-only HTTP API)

- Slice 10a (read-only HTTP API) done. Lays the JSON contract the dashboard frontend (slice 10b) will consume. Every read-side data the UI needs is reachable via `curl http://localhost:8080/api/...`; no write endpoints in this slice (those land in 10c).
- New package `internal/api` (7 files):
  - `types.go` — DTOs intentionally separate from internal models (`wiki.Page`, `source.Source`, `scheduler.Task`) so a future internal field rename doesn't break the frontend wire format. Times normalized to RFC3339 UTC at the boundary; `omitempty` on optional fields. `Task.ScheduleAt` and `Task.LastRunAt` are `*time.Time` so unset values omit cleanly instead of rendering as `0001-01-01`.
  - `router.go` — `NewRouter(Deps) http.Handler` builds a Go 1.22 `ServeMux` with method-prefixed patterns (`GET /health`, `GET /sources/{id}`, etc). Routes are mount-agnostic — they don't include `/api`; callers wrap with `http.StripPrefix("/api", ...)`. `Deps` accepts interfaces (`WikiStore`, `SourceStore`, `SchedulerStore`) rather than concrete types so tests could swap fakes if pure-real-store fixtures ever get expensive. Two regex validators (`sourceIDRe`, `taskNameRe`) gate untrusted path segments before they reach filesystem joins.
  - `wiki.go` — `GET /wiki/pages` lists `[{slug, title, category, tags, updated_at}]` sorted by category then slug; `GET /wiki/page?slug=X` returns the full page with a `Frontmatter` map (rendered from the structured `wiki.Page` fields, not raw YAML) and a 1 MiB body cap (413 if exceeded); `GET /wiki/graph` builds nodes from every wiki page and edges from `wiki.ExtractWikiLinks(body)` + frontmatter `Related`, deduping per source-page (so a page that links to the same target via both wikilink and related yields one edge — wikilink wins) and dropping self-loops + dangling edges to non-existent slugs. `latestWikiMTime` walks the wiki dir for the newest `.md` mtime — exposed via a new `wiki.Store.Dir()` accessor — so `/health` doesn't have to read+parse every page on every poll.
  - `sources.go` — `GET /sources` (with `?kind=` and `?status=` filters validated at the boundary, 400 on bogus values) returns lightweight `SourceSummary` rows; `GET /sources/{id}` returns the full `SourceDetail` including SHA256 / size / mime / OCR model / last-error. `GET /sources/{id}/ocr` reads `ocr.md` via `source.Store.Path` (containment-checked) and returns 404 if missing. `GET /sources/{id}/raw` is PDF-only — non-PDF kinds return 404 — streams `original.pdf` via `http.ServeContent` so the browser gets proper conditional-GET / range support and an `inline; filename="..."` disposition for save-as.
  - `tasks.go` — `GET /tasks` (optional `?status=` filter) and `GET /tasks/{name}`. `taskDTO` shapes the response and pointerizes the optional times.
  - `health.go` — `GET /health` rollup: wiki page count + last update mtime, sources by_status counts, tasks by_status counts, soonest active-task `next_run_at` (or null). Single fetch, single round-trip — the dashboard home page can render from this alone.
  - `router_test.go` — 14 test funcs / 21+ subtests using `httptest`. Each test gets its own `t.TempDir` with a real `wiki.Store`, real `source.Store`, and real SQLite-backed `scheduler.Store`; no fakes. Coverage: empty rollup, populated rollup with done-task exclusion from next_run, sort-order on `/wiki/pages`, body markdown round-trip, the 5 bad-input cases on `/wiki/page` (missing/empty/invalid-chars/path-traversal/unknown-slug), graph edge dedup + self-loop filter + dangling-target filter, source list filter validation + DTO trim, source 404 vs 400 vs OK, ocr.md present-vs-missing, raw PDF stream + Content-Type + non-PDF rejection, task list filter + status-filter rejection, task get happy/missing/malformed-name, unknown-path 404, method-not-allowed.
- `internal/wiki/store.go` — added `Dir() string` accessor (3 lines). The API uses it for the mtime walk in `/health`; the LLM-facing wiki tools don't need it.
- `internal/health/server.go` — added a `mux *http.ServeMux` field to the `Server` struct (the mux already existed but was scoped to `NewServer`) plus a `Mount(prefix, handler)` method so the API can be attached without touching the Server's existing `/`, `/status`, `/health` handlers. No behavior change for the existing endpoints.
- `internal/telegram/bot.go` — `Bot` gained an `api http.Handler` field, built once in `New` from `wikiStore`, `sourceStore`, `schedStore`, and exposed via `APIHandler() http.Handler` so `cmd/aura/main.go` can hand it to the health server. No new dependencies on the bot's hot path — the API doesn't touch `tools.Registry`, `llm.Client`, or anything else that mutates state.
- `cmd/aura/main.go` — moved `healthServer.Start()` to *after* `Bot.New` + `Mount` so the API routes are wired before the listener accepts requests (previously a request hitting `/api/...` during the millisecond between Start and bot construction would have 404'd; now there's no race). Adds `net/http` import for `http.StripPrefix`.
- Verification: `go test ./internal/api/...` PASS (14 tests / 21 subtests, no skips); `go test ./...` full suite PASS; `go build ./...` clean; `go vet ./...` clean; `go test -race ./internal/api/...` clean.
- Files touched: `internal/api/types.go` (new), `internal/api/router.go` (new), `internal/api/wiki.go` (new), `internal/api/sources.go` (new), `internal/api/tasks.go` (new), `internal/api/health.go` (new), `internal/api/router_test.go` (new), `internal/wiki/store.go` (`Dir()`), `internal/health/server.go` (`mux` field + `Mount`), `internal/telegram/bot.go` (api field + APIHandler), `cmd/aura/main.go` (mount + reordered Start), `docs/implementation-tracker.md`.
- Manual verification recipe (still owed by user, no LLM access to a browser): run `go run ./cmd/aura`, then `curl http://localhost:8080/api/health` should return the rollup; `curl http://localhost:8080/api/wiki/pages` should list seeded pages; `curl http://localhost:8080/api/sources` should list the three live-tested PDFs; `curl http://localhost:8080/api/tasks?status=active` should show the bootstrapped `nightly-wiki-maintenance` row.
- Next slice: **10b — Frontend scaffold + wiki/graph views** (copy `D:\sacchi_Agent\frontend\src-app` → `web/`, strip sacchi-specific pieces per the slice plan, wire `/api/*` calls in `src/api.ts`, build into `web/dist`, embed via `//go:embed`). Or push 10c (write actions) first if the read-only API needs more endpoints once the UI is built.

### 2026-04-30 — Side work: Windows system tray icon

- Out-of-band addition (not in the original PDR §12 slice order): a system tray icon when the bot starts, so the user sees Aura is running and can stop it from the OS shell.
- New package `internal/tray` (3 files):
  - `tray.go` — public API: `Options{Title, Tooltip, Version}`, `Run(opts) error` (blocks; MUST be called from main goroutine because `fyne.io/systray` requires the main thread on Windows), `Stop()` (safe from any goroutine).
  - `tray_windows.go` — real impl. `//go:embed icon.ico` for the asset, `systray.Run(onReady, onExit)` blocks until Quit. `onReady` sets icon/title/tooltip, adds a disabled `"Aura <version>"` header, separator, then `"Quit Aura"` menu item. A goroutine waits on `mQuit.ClickedCh` and calls `systray.Quit()` to unblock Run. `Stop()` also calls `systray.Quit()`.
  - `tray_other.go` — non-Windows stub. `Run` blocks on a package-level channel; `Stop` closes it via `sync.Once`. Mirrors the Windows lifecycle so `cmd/aura/main.go` is platform-agnostic.
- Icon: `internal/tray/icon.ico` generated once from `Logo/logo.png` via PowerShell + .NET (`System.Drawing.Image` → 256x256 aspect-preserved bitmap → `Bitmap.GetHicon()` → `Icon.FromHandle().Save()`). 41 KB single-frame ICO. Regenerate by re-running the conversion if the logo changes.
- `cmd/aura/main.go` restructured:
  - Added `auraVersion = "3.0"` const (replaces three string literals).
  - Removed `defer healthServer.Shutdown` (the deferred Shutdown ran during normal exit but the bot.Stop() was never deferred — explicit shutdown sequence is clearer now and properly orders bot stop before health server shutdown).
  - Bot creation failure now shuts the health server down before `os.Exit(1)`.
  - `go bot.Start()` runs as before.
  - Signal goroutine: `<-sigCh` → `tray.Stop()`. Bridges SIGINT/SIGTERM to the tray's quit path so the same shutdown sequence runs whether the user closes from the tray menu or sends a signal.
  - `tray.Run(...)` runs on the main goroutine and blocks. After it returns, the explicit shutdown sequence runs: log → `bot.Stop()` → `healthServer.Shutdown()`.
- Dependency: `fyne.io/systray v1.12.0` (and transitive `github.com/godbus/dbus/v5 v5.1.0` upgrade) added via `go get fyne.io/systray@latest && go mod tidy`.
- Verification: `go build ./...` clean, `go vet ./...` clean, `go test ./...` full suite PASS (existing tests untouched; tray package is a thin wrapper with no tests — manual verification on first run only).
- Files touched: `internal/tray/tray.go` (new), `internal/tray/tray_windows.go` (new), `internal/tray/tray_other.go` (new), `internal/tray/icon.ico` (new, generated), `Logo/logo.png` (canonical source asset, previously untracked), `cmd/aura/main.go` (restructured), `go.mod` + `go.sum` (deps), `docs/implementation-tracker.md`.
- Manual verification still pending: run `go run ./cmd/aura` and confirm the tray icon appears, hover-tooltip reads `Aura — running on :PORT`, and "Quit Aura" cleanly stops the bot. The tray and SIGINT paths both feed into `tray.Stop()` so they should behave identically.

### 2026-04-30 — Slice 9 complete (cmd/debug_ingest)

- `cmd/debug_ingest/main.go` — natural-prompt smoke harness mirroring `cmd/debug_tools` but for the source / ingest / wiki-maintenance / scheduler tools shipped in slices 5–8. Hermetic: temp wiki dir + temp SQLite scheduler DB. Reads LLM_API_KEY + EMBEDDING_API_KEY from `.env`.
- Pre-seeds two sources before the LLM run: a stored text source (`smoke-note.txt`, status=stored) and an ocr_complete PDF source with a hand-written `ocr.md` (so `ingest_source` has something real to compile without needing a live Mistral OCR call).
- 10 scenarios — one tool per scenario, each asserting the LLM picked the right tool and the final text contains expected markers:
  - `list_sources` (sees both seeded IDs)
  - `read_source` (filename round-trip)
  - `lint_sources` (correctly buckets the ocr_complete source as awaiting-ingest)
  - `ingest_source` (compiles the fixture into `source-aura-debug-ingest-fixture`)
  - `list_wiki` post-ingest (finds the new page)
  - `lint_wiki` (clean wiki passes)
  - `append_log` (writes a `smoke-test` entry to `log.md`)
  - `schedule_task` with `in: 90s` (relative duration, exercises the slice-8 follow-up path)
  - `list_tasks` (surfaces the scheduled task)
  - `cancel_task` (flips it to cancelled)
- Uses `RenderSystemPrompt(now, time.Local)` so the LLM sees the runtime time block (slice-8 follow-up). Threads a synthetic user ID via `tools.WithUserID` so the reminder branch of `schedule_task` works uniformly even though we only test wiki_maintenance kind here (which doesn't need a recipient).
- Live run on `glm-5.1:cloud`: **all 10 scenarios PASS first try**. The LLM picked the relative `in` field for the scheduler scenario (no UTC math) — the slice-8 follow-up is now battle-tested through a different model (Telegram run was on the user's primary model).
- Verification: `go build ./...` clean, `go vet ./...` clean, `go run ./cmd/debug_ingest` PASS 10/10.
- Files touched: `cmd/debug_ingest/main.go` (new), `docs/implementation-tracker.md`.
- Next slice: **10 — UI** (last remaining item; everything 1–9 is now done and exercised).

### 2026-04-30 — Slice 8 follow-up (current-time prompt + in/at_local)

- **Live-tested slice 8** with the bot running. First attempt: LLM picked `at=2026-04-30T06:48:00Z` which was already in the past (current UTC was 07:18) — validation rejected. LLM retried with `at=2026-05-01T06:43:00Z` (tomorrow morning), which was technically future but nowhere near the user's "fra 60 secondi" (in 60 seconds) intent. Fast-forwarded the row by hand to `now+30s` to prove the dispatcher fires (it did, ≤13s after the next tick).
- **Root cause**: the LLM has no ground-truth current time and can't reliably do timezone math. Two fixes shipped:
  1. **Runtime context in the system prompt**. `RenderSystemPrompt(now, loc)` appends a `## Runtime Context` block with current local time + UTC time + timezone + a brief recipe for the four schedule fields. `bot.go` calls it on every turn so the snapshot stays fresh.
  2. **Robust schedule fields on `schedule_task`**. Added `in` (relative duration: `60s`, `5m`, `2h`, `1d`) and `at_local` (wall-clock without offset, parsed in the configured timezone). Both bypass the LLM's UTC math entirely. Existing `at` (UTC ISO) and `daily` (HH:MM) still work; the four are mutually exclusive.
- `internal/conversation/system_prompt.go` — added `RenderSystemPrompt(now time.Time, loc *time.Location) string`. The original `DefaultSystemPrompt()` is preserved for callers that don't need wall-clock awareness.
- `internal/telegram/bot.go` — system prompt now refreshes on every user message via `convCtx.SetSystemMessage(conversation.RenderSystemPrompt(time.Now(), time.Local))`, replacing the once-per-conversation set.
- `internal/tools/scheduler.go` — `schedule_task` now accepts `in`, `at_local`, `at`, `daily`. Mutually exclusive: passing more than one is rejected up front. Past timestamps in `at_local` and `at` produce errors that include the current clock so the LLM has a hint on the next retry. New helper `parseLocalWallClock(s, loc)` accepts four shapes (`T`/space separator, with/without seconds), and rejects strings carrying timezone info (those belong in `at`).
- `internal/tools/scheduler_test.go` — added 4 happy-path tests (`TestScheduleTaskTool_RelativeIn`, `TestScheduleTaskTool_AtLocal` pinned to `Europe/Rome`, `TestScheduleTaskTool_AtLocalRejectsPast`, `TestParseLocalWallClock_AcceptsCommonShapes`/`_RejectsTimezoneSuffixes`) plus 4 new bad-input cases covering `in`/`at_local` validation. `TestParseLocalWallClock_AcceptsCommonShapes` skips when `Europe/Rome` tzdata is unavailable so the suite stays green on minimal images.
- Verification: `go test ./...` PASS (full suite); `go build ./...` clean; `go vet ./...` clean.
- Files touched: `internal/conversation/system_prompt.go` (added `RenderSystemPrompt` + `time` import), `internal/telegram/bot.go` (per-turn refresh), `internal/tools/scheduler.go` (new params + helper), `internal/tools/scheduler_test.go` (5 new tests + 4 new validation cases), `docs/implementation-tracker.md`.

### 2026-04-30 — Slice 8 complete (autonomous SQLite scheduler)

- Slice 8 (reminder/scheduler) done — reframed around the user's autonomy requirement: not just one-shot user reminders but a real cron with bootstrapped system jobs that survive process restarts.
- `internal/scheduler/types.go` — `Task` struct with two kinds (`reminder`, `wiki_maintenance`) and two schedule kinds (`at` ISO8601-UTC, `daily` HH:MM-local). `RecipientID` field captured from the LLM-call context so reminders go back to the right chat.
- `internal/scheduler/store.go` — SQLite `scheduled_tasks` table (idempotent migration), Upsert (UNIQUE-name conflict → updates schedule + payload), GetByName, List (sorted by next_run_at), DueTasks (active + next_run_at ≤ now), MarkFired, Cancel, Delete. Helper `NextDailyRun(daily, loc, after)` is the cron arithmetic — handles both initial scheduling and the post-fire roll-forward, including the at-fire-time edge case (advance to tomorrow). `ParseDailyTime` is strict (HH:MM, zero-padded, 0–23 / 0–59).
- `internal/scheduler/scheduler.go` — tick loop runs in a goroutine, immediate tick on startup so missed-while-offline tasks fire on boot. Pure `advance()` for state transitions (one-shot success → done, one-shot failure → failed, daily → reschedule + StatusActive even on dispatch failure so transient errors don't kill recurring jobs).
- `internal/scheduler/scheduler_test.go` — 14 test funcs / 21 cases. Three are explicit autonomy proofs: `TestScheduler_Autonomous` (schedule a task 500ms in the future, do nothing, verify the dispatcher fires it within 3s), `TestScheduler_AutonomousDailyReschedules` (recurring task fires + advances to tomorrow), `TestScheduler_PicksUpStaleTaskAfterRestart` (task with next_run_at in the past gets picked up on first tick — the restart-recovery contract).
- `internal/tools/scheduler.go` — three LLM tools:
  - `schedule_task` — `{name, kind, payload?, at?, daily?}`. Reminder kind requires user-id from context (rejected up front otherwise, so we never persist a task with no recipient). Mutually exclusive at/daily; rejects past `at`.
  - `list_tasks` — optional status filter, groups by status.
  - `cancel_task` — flips active → cancelled.
- `internal/tools/context.go` — `WithUserID(ctx, id)` / `UserIDFromContext(ctx)` so the bot can thread the calling user's Telegram ID into tool execution without polluting tool args. WithUserID with empty id is a no-op so existing IDs aren't clobbered.
- `internal/tools/scheduler_test.go` — 11 tests covering one-shot reminder happy path (asserts RecipientID captured from ctx), reminder-without-user rejection, daily wiki_maintenance happy path (asserts no recipient captured for autonomous tasks), 6 input-validation cases, list grouping + status filter, cancel + re-cancel, missing-name guard, context helper round-trip.
- `internal/telegram/bot.go` wiring:
  - Built scheduler store from `cfg.DBPath` (shares the SQLite file with FTS5 search; separate connection pool — fine for single-process).
  - Registered `schedule_task`, `list_tasks`, `cancel_task`.
  - `dispatchTask` method: `reminder` parses RecipientID and sends `⏰ <payload>` via `b.bot.Send(tele.ChatID(id), …)`; `wiki_maintenance` runs `RebuildIndex` + `Lint` (warns per issue) + `AppendLog("nightly-maintenance", "")` — pure deterministic, no LLM round-trip.
  - Bootstrap upsert of `nightly-wiki-maintenance` (kind=wiki_maintenance, daily=03:00) on boot. Idempotent via name uniqueness; restart doesn't duplicate, and a user `schedule_task` with the same name overrides.
  - `Start()` now also starts the scheduler goroutine; `Stop()` stops it and closes the DB.
  - Tool execution call site (line 505) wraps ctx with `tools.WithUserID(ctx, userID)` so reminders capture the right recipient.
- Verification: `go test ./...` PASS (scheduler 14 funcs, scheduler tools 11 funcs, full suite green); `go build ./...` clean; `go vet ./...` clean. One unrelated flaky network-port test in `internal/ocr` (httptest reuse on Windows) — passes on retry.
- Files touched: `internal/scheduler/types.go` (new), `internal/scheduler/store.go` (new, ~310 lines), `internal/scheduler/scheduler.go` (new, ~165 lines), `internal/scheduler/scheduler_test.go` (new, ~480 lines), `internal/tools/scheduler.go` (new, ~245 lines), `internal/tools/scheduler_test.go` (new, ~250 lines), `internal/tools/context.go` (new, ~30 lines), `internal/telegram/bot.go` (modified — import, scheduler creation, bootstrap, dispatcher, Start/Stop, ctx wiring), `docs/implementation-tracker.md`.
- Next slice: **9 — Natural prompt tests for OCR/ingest** (extend `cmd/debug_tools` or add `cmd/debug_ingest`). After that: slice 10 (UI), the only remaining item before standalone Aura is feature-complete per the PDR.

### 2026-04-30 — Slice 7 follow-up (live test, log.md empty-slug fix)

- **Live-tested all four slice 7 tools in one Telegram turn** with the prompt: "Do a full wiki maintenance pass: list every page so I can see what's there, run a lint check for broken links and missing categories, rebuild the index just to be safe, and append a log entry with action 'maintenance-pass' so we have a record."
- LLM decomposed it into the expected sequence: `list_wiki` (1ms, 196 bytes) → `lint_wiki` (1ms, 71 bytes) → `rebuild_index` (5ms) → `append_log` (8ms). All four returned cleanly; total elapsed ~330ms.
- **Cosmetic bug found**: `append_log` with no slug rendered the page cell as `[[]]` (literal empty wiki-link) — visible in `log.md` and rendered as a broken link in graph view. Fix: only wrap the slug in `[[...]]` when non-empty; emit a blank cell otherwise.
- Hand-fixed the stale `[[]]` row in `wiki/log.md` (one-time artifact from the live test before the fix).
- Test added: `TestAppendLogTool_EmptySlug` now also reads `log.md` and asserts no literal `[[]]` and that the row has a blank page cell.
- Verification: `go test ./...` PASS, `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/wiki/store.go` (3-line render fix in `appendLog`), `internal/tools/wiki_maintenance_test.go` (extended assertion).

### 2026-04-30 — Slice 7 complete

- Slice 7 (wiki maintenance tools) done. Most of the heavy lifting already lived in `internal/wiki/store.go` (`ListPages`, `Lint`, private `updateIndex` / `appendLog`), so the slice is mostly thin LLM tool wrappers plus exporting the two private helpers.
- `internal/wiki/store.go`: added public `RebuildIndex(ctx)` and `AppendLog(ctx, action, slug)` that delegate to the existing private methods. Kept the private versions so internal call sites in `WritePage` / `DeletePage` / `MigrateYAMLToMD` stay unchanged.
- `internal/tools/wiki_maintenance.go` (new):
  - `list_wiki` — `{category?, limit?}` (default 50, max 200). Returns pages grouped by category, sorted by category then slug, with `[[slug]]` wiki-links inline. Case-insensitive category filter. Output capped via `truncateForToolContext` at 8000 chars.
  - `lint_wiki` — no args. Wraps `wiki.Store.Lint`, groups issues by slug under `## [[slug]]` headers, emits "Wiki is clean: no issues found." when empty.
  - `rebuild_index` — no args. Calls `wiki.Store.RebuildIndex`, returns the page count from a follow-up `ListPages`.
  - `append_log` — `{action (required, ≤50 chars, trimmed), slug?}`. Surfaces `wiki.Store.AppendLog` so the LLM can record query/summary events that don't go through `WritePage`. Truncates over-long actions to keep `log.md` table rows readable. Empty/whitespace action rejected.
- `internal/telegram/bot.go`: registered all four tools always (no conditional gating — all four work as long as `wikiStore` exists, which is always true).
- `internal/tools/wiki_maintenance_test.go` (new): 15 unit tests covering empty wiki, multi-category grouping (incl. category sort order), case-insensitive filter, empty-filter result, limit truncation, nil-store guards on every tool, clean-lint, lint with mixed issues (broken link / broken related / missing category), rebuild over a corrupted `index.md`, append_log with/without slug, action-length truncation, empty-action rejection. Test helper `putPage` derives slug from title via `wiki.Slug` to mirror production.
- Verification: `go test ./...` PASS; `go build ./...` clean; `go vet ./...` clean.
- Files touched: `internal/wiki/store.go` (+13 lines), `internal/tools/wiki_maintenance.go` (new, ~280 lines), `internal/tools/wiki_maintenance_test.go` (new, ~310 lines), `internal/telegram/bot.go` (+5 lines wiring), `docs/implementation-tracker.md`.
- Next slice: **8 — Reminder/scheduler (SQLite `scheduled_tasks`, `schedule_task`, `list_tasks`, `cancel_task`)**. Independent of slices 1–7. Picobot has a battle-tested cron pattern in `picobot/internal/cron` and SQLite migration helpers — start there.

### 2026-04-30 — Slice 6 follow-up #2 (readable slugs, migration)

- **Problem reported**: source page slugs were opaque hex (`source-src-24abf740febd9eac`). Unreadable for the LLM and useless in the wiki graph view — every source clusters as `source-src-…` with no semantic differentiation. Violates the LLM-wiki principle from `docs/llm-wiki.md`: "the cross-references are already there… the wiki keeps getting richer".
- **Fix**: title now derives from the display filename (sans extension). `Source: uta.pdf` → title `Source: uta` → slug `source-uta`. `Source: MARCHETTO DAVIDE_DDT N. 90.pdf` → `source-marchetto-davide-ddt-n-90`. Empty filename falls back to `Source: <id>` so slugs are always valid.
- **Collision handling**: `Pipeline.resolveTitle` reads the candidate slug; if the wiki page there belongs to a different source, the title gets a short id suffix (first 6 hex of `src_…`) so `wiki.Slug(title)` produces a unique slug. Title (not slug) is the disambiguation point because `wiki.Store.WritePage` derives the on-disk filename from `page.Title`; making them disagree silently overwrites the older page (caught by the FilenameCollision test).
- **Migration**: `Compile` now compares `src.WikiPages` against the freshly-computed slug. If they differ (e.g. slug rule changed, or filename was renamed), the new page is written, the old slug(s) are best-effort deleted via `wiki.Store.DeletePage`, and `source.json` is updated to point only at the new slug. Wiki no longer accumulates dead pages on slug rule changes.
- **Idempotence is now slug-aware**: a re-Compile only short-circuits when status=ingested *and* `WikiPages == [newSlug]`. A stale-slug ingested source is treated as "needs migration" rather than "already done".
- **Live migration run** on the three pre-existing sources:
  - `src_24abf740febd9eac` (`uta.pdf`) → `source-uta`
  - `src_684b8214169e35bf` (`MARCHETTO DAVIDE_DDT N. 90.pdf`) → `source-marchetto-davide-ddt-n-90`
  - `src_437ecedcb716dbbf` (`4_5942613039617418204.pdf`) → `source-4-5942613039617418204`
  - All three old `source-src-<hex>.md` pages deleted; `source.json` `wiki_pages` updated; new pages have correct frontmatter and `Status: ingested`.
- **Tests added** (5 new + helper): `TestCompile_FilenameCollision` (two PDFs same filename get distinct slugs, neither overwrites the other), `TestCompile_MigratesStaleSlug` (planted stale page is rewritten and old slug deleted), `TestCompile_EmptyFilenameFallback` (empty filename → id-based fallback slug), `TestBuildTitle` (6 cases incl. extension stripping, whitespace, fallback), `TestShortID` (5 cases), `TestStaleSlugsToDelete` (4 cases). `TestCompile_HappyPath` updated to assert `source-paper` slug and `Source: paper` title. New helper `putOCRCompleteAs` lets tests pin filename and content for collision scenarios.
- **Style**: replaced manual `for` loop with `slices.Contains` for `pageBelongsTo` per gopls hint.
- Verification: `go test ./...` PASS (all tests + 5 new); `go test -tags=live_ingest -run TestLiveIngest` PASS on all three migrated sources; `go build ./...` clean; `go vet ./...` clean.
- Files touched: `internal/ingest/pipeline.go` (slug-resolution + migration logic, ~50 LOC), `internal/ingest/pipeline_test.go` (new tests + helper), `docs/implementation-tracker.md`.

### 2026-04-30 — Slice 6 follow-up (live test, Status fix, catch-up)

- **Live-tested slice 6 auto-ingest via real Telegram bot**: uploaded `uta.pdf` (1 page, 59 KB UTA fuel-card delivery letter) — OCR 1.35s, auto-hook fired ~210ms after OCR, final progress message read `✅ Done · src_24abf740febd9eac · 1 page · 1.6s · compiled as [[source-src-24abf740febd9eac]]`. `source.json` flipped to `status=ingested` with `wiki_pages` set. Wiki page on disk had full PDR §4 layout: frontmatter (`title`, `category=sources`, `tags=[source,pdf]`, `sources=[source:src_…]`), Metadata block, Raw OCR pointer, Preview block with the inlined Italian fuel-card form.
- **Cosmetic bug found and fixed**: rendered page body said `Status: ocr_complete` because `buildSummaryBody` was called before `sources.Update` flipped status. The page would never refresh on idempotent recompile (status=ingested → "already compiled" early-return), so the body was permanently wrong. Fix: render `source.StatusIngested` literally in `buildSummaryBody` since Compile only reaches the render step on success and the flip is the very next operation. Test updated to assert `Status: ingested` in the body.
- **Catch-up live test added**: `internal/ingest/live_test.go` (build tag `live_ingest`) takes `INGEST_SOURCE_IDS` from env and runs `Pipeline.Compile` on each. Asserts the wiki page is on disk with `Status: ingested` and `source.json` flipped. Same env-loading pattern as `internal/ocr/live_test.go`. Skips cleanly when env not set.
- **Catch-up run** on the two pre-hook sources from yesterday's live test: `INGEST_SOURCE_IDS="src_684b8214169e35bf,src_437ecedcb716dbbf" LIVE_WIKI_PATH=D:/Aura/wiki go test -tags=live_ingest -run TestLiveIngest -v ./internal/ingest/...` — both compiled cleanly. After this run, all three on-disk sources (`src_24abf740febd9eac`, `src_684b8214169e35bf`, `src_437ecedcb716dbbf`) report `status=ingested` with their corresponding wiki pages on disk. Stale `Status: ocr_complete` line in the live-tested `uta.pdf` page was hand-fixed in the wiki file (one-time artifact of the pre-fix run; future writes use the corrected renderer).
- **WIKI_PATH gotcha**: the live test reads `WIKI_PATH` from `.env`, which is `./wiki` (relative to the bot's run dir). Tests run from `internal/ingest/` so the relative resolves to a non-existent path. Override with `LIVE_WIKI_PATH=D:/Aura/wiki` (absolute) when running locally.
- Verification: `go test ./...` PASS (default tags), `go test -tags=live_ingest ...` PASS (catch-up), `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/ingest/pipeline.go` (1-line render fix + comment), `internal/ingest/pipeline_test.go` (new assertion), `internal/ingest/live_test.go` (new, build-tagged), `docs/implementation-tracker.md`.
- Wiki content files (`wiki/source-src-*.md`, `wiki/index.md`, `wiki/log.md`) are user data and intentionally not staged for commit. They live on disk only.

### 2026-04-30 — Slice 6 complete

- Slice 6 (ingestion pipeline) done:
  - `internal/ingest/pipeline.go`: `Pipeline.Compile(ctx, sourceID)` turns a `status=ocr_complete` source into a `Source <id>` wiki summary page, flips status to `ingested`, and (best-effort) reindexes the slug via `search.Engine.ReindexWikiPage`. Idempotent: a second call on an `ingested` source returns the existing slug with `Created=false` and a "already compiled" note. Emits a deterministic body — Metadata block, Raw OCR pointer (`wiki/raw/<id>/ocr.md`), and a 1000-char preview of the OCR body (header lines from `internal/ocr/render.go` are stripped so the preview starts at real content). UTF-8-safe truncation.
  - `Pipeline.AfterOCR(ctx, src) (note, err)`: adapter matching the new `telegram.AfterOCRHook` signature so the pipeline plugs straight into `docHandlerConfig.AfterOCR`.
  - `internal/tools/ingest.go`: `ingest_source` LLM tool (`source_id` → "Compiled / Already compiled source <id> as [[slug]]"). Lets the LLM re-run ingest on stored sources and is the recovery path when the auto-hook fails.
  - `internal/telegram/documents.go`: `AfterOCRHook` signature changed from `func(ctx, src) error` to `func(ctx, src) (note, err)`. The optional note replaces the static "ready for ingest" tail in the final progress edit, so a successful auto-ingest now ends as `✅ Done · src_… · N pages · Xs · compiled as [[source-src-…]]`. Hook failure logs and falls back to "ready for ingest" so the user can retry via `ingest_source`. Also fixed a `defer hookCancel()` inside the conditional that would have leaked the cancel until `process` returned — now an explicit `hookCancel()` after the call.
  - `internal/telegram/bot.go`: builds `ingest.Pipeline` unconditionally (only deps are sourceStore + wikiStore, both already present), registers `ingest_source` always, and wires `ingestPipeline.AfterOCR` into the Telegram doc handler.
  - `internal/ingest/pipeline_test.go`: 10 test funcs covering happy path (verifies wiki page contents, source flip to ingested, no preview leakage of OCR header lines), idempotence, missing-ocr.md error pointing at `ocr_source`, wrong-status error, unknown id, path-traversal id, the `AfterOCR` adapter shape, `buildPreview` (5 cases incl. zero/empty/truncate/no-header), UTF-8 boundary safety, and that `wiki.Store.WritePage` produces `index.md` + `log.md` side files.
- Design notes:
  - Title = `"Source " + sourceID` (not display filename). Two PDFs with the same display filename can't collide; the human-readable filename lives in the body.
  - `Source: source:<id>` frontmatter so the wiki schema picks up the source linkage.
  - Search reindex is best-effort (warn on failure) — the page is durable on disk regardless. Matches the slice 4 "OCR is durable even if downstream fails" pattern.
  - Hook signature change is a breaking change to the unexported `AfterOCRHook` type only; no external callers.
- Verification: `go test ./...` PASS (incl. `internal/ingest` 10 funcs / 15 cases, `internal/telegram` still passing the 12 slice-4 tests after signature change); `go build ./...` clean; `go vet ./...` clean.
- Files touched: `internal/ingest/pipeline.go` (new), `internal/ingest/pipeline_test.go` (new), `internal/tools/ingest.go` (new), `internal/telegram/bot.go` (modified — import, ingest pipeline build, registry register, AfterOCR wiring), `internal/telegram/documents.go` (modified — AfterOCRHook signature, tail composition, defer fix), `docs/implementation-tracker.md`.
- Pre-existing diagnostics in `bot.go` from slices 4–5 still out of scope.
- Next slice: **7 — Wiki maintenance tools (`append_log`, `rebuild_index`, `list_wiki`, `lint_wiki`)**. Surfaces the wiki/index/log machinery that already lives in `internal/wiki` to the LLM, and lets it audit/refresh wiki structure between ingest runs.

### 2026-04-30 — Slice 5 complete

- Slice 5 (LLM source tools) done:
  - `internal/tools/source.go`: 5 tools — `store_source` (text/url; PDFs are Telegram-only because the LLM can't stream binary), `ocr_source` (Mistral OCR pipeline mirror of `internal/telegram/documents.go` for re-OCR or post-hoc OCR), `read_source` (modes: metadata / ocr / excerpt; falls back to `original.txt`/`original.url` for non-PDF kinds when no `ocr.md`), `list_sources` (kind/status filter, default-20-max-100 limit, truncated indicator), `lint_sources` (buckets: stored awaiting OCR / OCR awaiting ingest / failed). Output capped via existing `truncateForToolContext`.
  - `internal/tools/source_test.go`: 13 unit tests — text+dedup, url+validation, nil-store, read modes (metadata/excerpt/ocr) incl. invalid id and bad mode, list filter+limit, list empty, lint buckets, lint clean, ocr_source no-client, ocr_source non-PDF reject, ocr_source happy path with httptest fake Mistral, ocr_source failure → status=failed + Error recorded.
  - `internal/telegram/bot.go`: registry wiring — source tools always registered when sourceStore exists; `ocr_source` only when `ocrClient != nil` so the LLM never sees a tool it can't actually run. Reordered the source/OCR setup above the registry block so the registry can see them.
- Design notes:
  - PDR §6 spec for `store_source` listed `path|url|content` inputs. Slice 5 deliberately omits `path` because the LLM has no filesystem; admin/console paths can come later. PDF entry stays Telegram-only.
  - `ocr_source` re-uses `ocr.RenderMarkdown` and `source.Store.Path` (containment-checked) so writes are bounded to `wiki/raw/<id>/`. On failure it flips status to `failed` and records the error message — same shape as the Telegram pipeline.
  - `read_source` modes are sized to fit the existing 8000-char tool budget (`maxSourceToolChars`); `excerpt` is 4000 chars to leave room for follow-up tool calls.
- Verification: `go test ./...` PASS (13 new tests); `go build ./...` clean; `go vet ./...` clean. Pre-existing `bot.go` lints (unused `userID`, `WriteString(fmt.Sprintf(...))`) were noted in slice 4 and remain out of scope.
- Files touched: `internal/tools/source.go` (new), `internal/tools/source_test.go` (new), `internal/telegram/bot.go` (modified — moved source/ocr setup above registry; added 4 always-on + 1 conditional source-tool registrations), `docs/implementation-tracker.md`.
- Next slice: **6 — Ingestion (`internal/ingest`)**. Pipeline turns `ocr.md` into compiled wiki pages with source backlinks, source summary page, and `wiki/log.md` entry. Wires into `docHandler.AfterOCR` so an uploaded PDF auto-ingests.

### 2026-04-30 — Multipage debug for `src_437ecedcb716dbbf`

- Symptom: 2-page Italian PMS PDF produced an `ocr.md` where `## Page 2` body is just `.`.
- Investigation:
  - `pdftotext -f 2 -l 2 wiki/raw/src_437ecedcb716dbbf/original.pdf` → empty output.
  - `pypdf` page 2: `extract_text() == ''`, no `/XObject`, no `/Resources`. Fully blank page in the source PDF.
  - `ocr.json` page 2: `markdown: "."`, empty `images`, `tables`, `hyperlinks`, header/footer null. Mistral correctly reported a near-empty page.
- Cause: not a flag interaction, not a Mistral bug — the source PDF really has a blank page 2. The flag re-test (`EXTRACT_HEADER=false EXTRACT_FOOTER=false INCLUDE_IMAGES=false`) would have shown the same `.` because those flags only affect header/footer/image extraction, never page-body text.
- No code change in this session; finding is for the renderer backlog.
- **Renderer follow-up (deferred, not slice 5):** detect "near-empty" pages (`strings.TrimSpace(body)` matches `.` or is empty) and render `## Page N (blank)` with no body, instead of literal `.`. This is a `internal/ocr/render.go` change only; leaves `ocr.json` untouched.
- **Re-render recipe (cheap, no new OCR calls):** since `ocr.json` is the raw Mistral response and `ocr.md` is a pure derivation, any renderer fix can be replayed against existing sources without API cost:

  ```go
  // pseudocode for a future cmd/rerender_ocr or similar
  for each dir in wiki/raw/*/:
      raw := read("ocr.json") // unmarshal into ocr.OCRResponse
      meta := ocr.RenderMeta{SourceID: id, Filename: source.Filename, Model: source.OCRModel}
      md   := ocr.RenderMarkdown(meta, raw)
      atomicWrite(dir/"ocr.md", md)
  ```

  Constraints: must reuse `internal/source.Store.Path` for containment, must atomic-rename, must skip dirs missing `ocr.json` (status=stored or failed). Add a `--dry-run` diff mode.

### 2026-04-29 — Slice 4 complete

- Slice 4 (Telegram PDF handler) done:
  - `internal/telegram/documents.go`: `docHandler` with bounded semaphore (`docConcurrencyLimit=2` simultaneous OCR jobs), single-message progress UX (initial reply → edits in place at each pipeline step → final ✅/❌), `AfterOCRHook` extension point for slice 6, validate-then-async pattern (handler returns within ~100ms; goroutine does the heavy lifting). `progressEditor` falls back to a fresh send if Edit fails (e.g. message deleted). Picobot/wiki conventions reused: per-key mutex (sync), atomic file writes via existing `source.Store.Path` containment.
  - `internal/telegram/bot.go`: `Bot` gained `sources`, `ocr`, `docs` fields. `New()` always builds a `source.Store` from `WIKI_PATH`; OCR client only when `OCR_ENABLED && MISTRAL_API_KEY != ""`. `registerHandlers` adds `tele.OnDocument` → `docs.onDocument` (gated on docs != nil so failures in source/OCR setup never break text handling).
  - `internal/telegram/documents_test.go`: 12 unit tests on pure functions — `validatePDF` (PDF/non-PDF/oversize/no-cap/nil/charset-suffixed mime), `safeName` (trim, empty, control chars, path chars, truncation), `formatSize` (B/KB/MB/GB rounding), `formatDuration` (ms / fractional s / s / m s), `pluralS`. Live Telegram round-trip is out of scope (needs actual Telegram session); the goroutine pipeline is exercised end-to-end already by the slice 3 follow-up `TestLiveE2E`.
- UX choices (single-message progress, bounded concurrency=2, dup-aware reply, error-as-final-edit) match the slice 4 design discussed before implementation.
- Verification: `go test ./...` PASS (incl. `internal/telegram` 12 new tests), `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/telegram/documents.go` (new), `internal/telegram/documents_test.go` (new), `internal/telegram/bot.go` (modified — imports, struct, New, registerHandlers), `docs/implementation-tracker.md`.
- Pre-existing diagnostics in `bot.go` (unused `userID` param, `WriteString(fmt.Sprintf(...))` style hints in `onStatus`) are out of slice 4 scope; left for a future cleanup commit.
- **Live-tested end-to-end via the actual Telegram bot** (`go run ./cmd/aura`, real PDFs uploaded by chat):
  - 1-page Italian receipt (RICEVUTA, 19 KB) — OCR 1.4s, 4-file layout written.
  - 1-page Italian DDT delivery note (55 KB) — OCR 2.3s, 4-file layout written.
  - 2-page Italian PMS test scenario (3 KB) — OCR 0.8s, ocr.md correctly emits `## Page 1` and `## Page 2` headings.
  - Each upload produced `original.pdf`, `source.json` (status=ocr_complete, ocr_model=mistral-ocr-latest, page_count, sha256), `ocr.md` (PDR §4 layout), `ocr.json` (raw Mistral response) under `wiki/raw/<source_id>/`. Filename sanitization preserved spaces in display while sha256 dedup keyed off content. Single-message progress UX confirmed.
- Next slice: **5 — LLM-facing source tools (`store_source`, `ocr_source`, `read_source`, `list_sources`, `lint_sources`)**. Lets the LLM drive the same pipeline (re-OCR a stored source, list inbox, surface unprocessed sources) and read source content into context for slice 6 ingest.

### 2026-04-29 — Slice 3 complete

- Slice 3 (OCR client) done:
  - `internal/ocr/types.go`: `OCRRequest` (wire body — verified against [Mistral basic_ocr docs](https://docs.mistral.ai/capabilities/document_ai/basic_ocr/) — includes `table_format`, `extract_header`, `extract_footer`, `include_image_base64`), `Document`, `OCRResponse`, `Page` (with header/footer), `Usage`.
  - `internal/ocr/client.go`: `Client` + `Config`. Bearer auth, JSON post, base64 PDF in `data:application/pdf;base64,...` URL, capped 256-char error snippets, 256 MiB response cap. HTTP shape mirrors `internal/tools/ollama_web.go`.
  - `internal/ocr/render.go`: `RenderMarkdown` produces PDR §4 ocr.md layout (`# Source OCR: <filename>`, `Source ID:`, `Model:`, then `## Page N`). Index+1 → 1-based display; defensive fallback when all pages report index=0.
  - Tests: 13 across `client_test.go` (success path verifies model/base64/auth header; include_images flag; extraction flags sent on wire; flags omitted when zero-valued; HTTP 401 doesn't leak API key; HTTP 500 snippet capped; bad JSON; empty bytes; missing base URL; trailing slash; default model) and `render_test.go` (PDR layout, model override, empty pages kept, all-zero-index fallback, missing filename placeholder).
- Wire-format correction: discovered late that `table_format`, `extract_header`, `extract_footer` are wire-level Mistral params (not Aura render hints as I initially assumed). Added them to `OCRRequest` and `Config`, plumbed from constructor to body, with tests asserting both presence-when-set and omission-when-zero (so `omitempty` correctly hides them from the JSON when defaulted).
- Verification: `go test ./...` PASS, `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/ocr/types.go`, `internal/ocr/client.go`, `internal/ocr/render.go`, `internal/ocr/client_test.go`, `internal/ocr/render_test.go`, `docs/implementation-tracker.md`.
- Next slice: **4 — Telegram PDF handler (`internal/telegram/documents.go`)**. Allowlist-gated PDF upload from Telegram, MIME/size validation against `OCR_MAX_FILE_MB`, download to `wiki/raw/<source_id>/`, `source.Store.Put`, then call `ocr.Client.Process` if `OCR_ENABLED`, write `ocr.md` + `ocr.json` via `source.Store.Path`, flip status to `ocr_complete`. No raw PDF text or base64 in logs (PDR §9).

### 2026-04-29 — Slice 2 complete

- Slice 2 (source store) done:
  - `internal/source/source.go`: `Kind` (pdf/text/url), `Status` (stored/ocr_complete/ingested/failed), `Source` struct matching PDR §4 schema.
  - `internal/source/store.go`: `Store` rooted at `<wiki>/raw/`. `Put` (sha256 dedup + atomic write), `Get`, `List` (kind/status filter, sorted desc), `Update` (mutator pattern), `Path` (containment-checked join), `RawDir`. Per-id mutex via `sync.Map`. Atomic temp+rename copied from `internal/wiki/store.go`. Regex ID validation pattern adapted from picobot's `isValidMemoryFile`.
  - `internal/source/store_test.go`: 10 test funcs — create, dedup, not-exist, invalid IDs (incl. traversal), list filters + bogus entries skipped, update persistence, mutator-error propagation, validation, path traversal rejection, all 3 kinds.
- Source ID format: `src_<first 16 hex of sha256>` — stable, dedupable, filesystem-safe. External IDs validated against `^src_[a-f0-9]{16}$` before any path join.
- Verification: `go test ./...` PASS (incl. `internal/source` 10 tests), `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/source/source.go` (new), `internal/source/store.go` (new), `internal/source/store_test.go` (new), `docs/implementation-tracker.md`.
- Next slice: **3 — OCR client (`internal/ocr`)**. Mistral `/v1/ocr` request/response, base64 PDF path, fake-server tests. Integrates with `source.Store.Update` to flip status to `ocr_complete` and write `ocr.md` / `ocr.json` via `source.Store.Path`.

### 2026-04-29 — Slice 1 complete

- Created this tracker per `aura-implementation` skill First Actions.
- Slice 1 (config) done:
  - `internal/config/config.go`: added `MistralAPIKey`, `MistralOCRModel`, `MistralOCRBaseURL`, `MistralOCRTableFormat`, `MistralOCRIncludeImages`, `MistralOCRExtractHeader`, `MistralOCRExtractFooter`, `OCREnabled`, `OCRMaxPages`, `OCRMaxFileMB` with PDR §3 defaults. Keys deliberately separate from `LLM_API_KEY` and `EMBEDDING_API_KEY`.
  - `internal/config/config_test.go`: extended `TestLoadSuccess` to assert OCR defaults and unset OCR env vars.
  - `.env.example`: documented OCR section.
- Verification: `go test ./...` (all packages PASS), `go build ./...` (clean), `go vet ./...` (clean).
- Files touched: `internal/config/config.go`, `internal/config/config_test.go`, `.env.example`, `docs/implementation-tracker.md`.
- Next slice: **2 — Source store (`internal/source`)**. Needs source ID generation (sha256 + ULID), `wiki/raw/<source_id>/` layout, atomic `source.json` write, listing, and tests for dedupe by sha256.
- Pre-existing diagnostic noted (not introduced this slice): `internal/config/config.go:52` — `IsAllowlisted` loop could use `slices.Contains`. Out of scope.
