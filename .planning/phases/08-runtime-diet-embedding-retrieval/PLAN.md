# Phase 08: Runtime Diet

Status: in progress
Owner: Codex
Date: 2026-05-08

Progress:

- 2026-05-08: Task 1 complete. The live `agentloop` fallback string/function is gone; budget exhaustion returns last useful tool output.
- 2026-05-08: Task 2 partial. Skill manifest, swarm exposure, summarizer, and nightly auto-improve are no longer default-turn ceremony.
- 2026-05-08: Task 3 complete for the hot path. The old `Memory Pack` code was physically replaced with `Retrieval Capsule`; graph/log file reads are no longer injected into system context.
- 2026-05-08: Task 2 deletion pass complete for profile/preflight. The live `ProfileCard`/capability taxonomy, skill-preflight runtime policy, swarm-routing prompt helper, debug orchestration command, `AURA_SKILL_PREFLIGHT`, `AURA_TOOL_PROFILE_MODE`, and legacy profile aliases were physically removed. Remaining Docker Compose `profiles` are Docker service metadata, not Aura runtime routing.
- 2026-05-08: Task 4 complete for the local wiki path. `search_memory` now merges exact slug/title/wiki-link hits, SQLite FTS, and vector results; Qdrant search merges local hybrid results instead of hiding lexical hits behind any non-empty vector response.
- 2026-05-08: Task 7 partial. Anti-residue grep over `internal`, `cmd`, `web/src`, `.env.example`, `compose.yaml`, and `internal/api/dist` returned no legacy profile/preflight symbols after the dashboard rebuild.
- 2026-05-08 log-driven repair: live logs showed DB-persisted settings still overriding Runtime Diet env defaults (`OTEL_ENABLED=true`, `SUMMARIZER_ENABLED=true`, `SUMMARIZER_MODE=auto`, `SANDBOX_AUTO_IMPROVE_MODE=auto`). Bumped the best-defaults migration so existing DBs are corrected once at startup, which removes stdout trace spam, post-turn memory capture, and nightly auto-improve from the default runtime. The same log pass exposed SQLite FTS syntax errors on punctuation-heavy prompts, fixed by tokenizing FTS queries to alphanumeric/underscore terms.
- 2026-05-08 deletion pass: physically removed the OpenTelemetry runtime package/wiring, post-turn summarizer runner/scorer/deduper, nightly auto-improve dispatcher/config, summarizer settings/env/Compose surface, debug summarizer command, compounding-rate health metric/card/tests, and stale dashboard groups. Wiki proposal approval remains as the manual review queue and now writes `proposal_v1` pages/logs; `summarizer_v{n}` is legacy read-only compatibility.
- 2026-05-08 runtime skills storage fix: `skills/` was still a repo path and Compose bind-mounted it into `/workspace/skills`, so container-installed skills appeared as Git changes. Skills now live in the Docker `aura-skills` volume mounted at `/workspace/skills`; `/skills/` is ignored and removed from tracked source.
- 2026-05-08 runtime wiki storage fix: `wiki/` was still a repo path and Compose bind-mounted it into `/workspace/wiki`, so Aura memory lived in the development tree. Existing wiki content was migrated into the Docker `aura-wiki` volume, Compose now mounts that volume at `/workspace/wiki`, local defaults point at `./runtime-workspace/wiki`, and `/wiki/` is ignored/removed from tracked source.
- 2026-05-08 residue cleanup: removed obsolete project-skill fixture tests that still required repo-local `skills/` content after the runtime skills volume migration. Skill loader behavior remains covered by temp-directory tests.
- 2026-05-08 Task 5/6 slice: `search_memory` now calibrates wiki/source/archive scores before merging, returns source/archive follow-up handles, and keeps source scan snippets bounded. The document toolset now exposes only `search_memory` plus typed file tools by default; workspace/source/web exploration is not in the normal document route. Toolset-specific `MaxSteps` caps were removed so the DB/dashboard `AgentLoopMaxSteps` value stays authoritative.
- 2026-05-08 health capture helper: added `scripts/capture-aura-health.ps1` to snapshot Docker status/logs, `/status`, optional dashboard conversation archive, optional Telegram bot health, and optional debug Telegram smoke into ignored `reports/health/<timestamp>/` directories. This gives future validation a repeatable evidence bundle without raw live DB copies.
- 2026-05-08 Hermes-style tool examples: every LLM-facing tool definition now carries concrete tool-call examples in its description. Known Aura tools get hand-written examples; dynamic/MCP tools get schema-derived fallback examples. This fixed the live document route that was producing malformed `create_docx` arguments and repeated retrieval loops.
- 2026-05-08 LangChain-style tool definitions: tool examples were lifted out of registry-side string patching into Aura's canonical `ToolDefinition` contract. Tools can now provide `Definition()` with name, description, JSON schema, and structured examples; old tools are adapted through a fallback so no exposed tool loses a call shape.
- 2026-05-08 document-loop repair: document-route `search_memory` results now include an explicit next-step hint to create exactly one typed artifact, and the OpenAI-compatible streamed tool-call parser repairs recoverable missing JSON closers. Live smoke recovered from the previous malformed `create_docx` failure and finished in 20.5s.
- 2026-05-08 ADK-Go orchestration review: compared Aura against `google/adk-go`'s `Agent`/`Runner`/`Session.Event`/`Toolset`/callback split. The useful pattern for Aura is not importing ADK wholesale; it is moving route policy, tool filtering, tool-result shaping, persistence, and post-tool steering out of Telegram into a small event runner with before/after model/tool hooks.

## Thesis

Aura has enough intelligence. The problem is that too much of it runs in the hot path.

The core should be:

- memory/wiki in files;
- few bounded file tools;
- fast search;
- simple loop;
- always-useful final answer.

Everything else must prove that it helps the current turn. If it does not, it moves out of the turn or gets deleted.

## Evidence

- Recent Docker logs still show broad prompts spending `llm_calls=4`, `tool_calls=9`, `elapsed_ms=54205`, repeatedly calling `read_file/search_files/list_files`.
- The broad document prompt can create a DOCX, but only after too much evidence expansion (`loop_steps=5`, `elapsed_ms=62894`, `tokens_total=66119`).
- `internal/agentloop/loop.go` still has the hardcoded fallback that says `"Mi sono fermato"`.
- the old `Memory Pack` path used to inject graph context plus recent wiki log.
- `search_memory` used to mix vector wiki scores with lexical source/archive scores as if they were comparable.
- Qdrant currently wins when it returns any result, even if local lexical search has better evidence.

## Brutal Rule

If it does not help answer this turn, it cannot run in this turn.

- Audit, telemetry, summarizer, proposal generation, validation, and self-improvement run after the answer or in scheduled jobs.
- Advanced configuration exists, but default stays simple.
- "Intelligence" that costs extra LLM calls must be replaced by file/search evidence.
- A tool that exists only to compensate for old complexity gets deleted.

## Keep In The Default Runtime

These stay because they map directly to user value:

- `search_memory`, but backed by compact calibrated retrieval;
- bounded workspace tools: `list_files`, `read_file`, `search_files`, `write_file`, `apply_patch`;
- source tools for source inbox inspection;
- document tools for typed artifact creation;
- skill files, read as procedures when relevant;
- DB-selected chat model;
- dedicated `EMBEDDING_*` config for embeddings.

## Move Out Of The Default Hot Path

These may still exist, but not as default-turn ceremony:

- swarm;
- manual proposals;
- memory patch validation;
- graph/log injection;
- profile/preflight policy;
- broad debug harness behavior;
- self-improvement;
- dashboard settings decision trees.

## Delete Or Justify

Every item in this list must be deleted, disabled by default, or documented in `NOT-DELETED.md` with a concrete reason:

- user-facing `"Mi sono fermato"` fallback;
- tests that approve that fallback;
- always-on Memory Pack injection;
- always-on graph/log context;
- speculative embedding search for simple prompts;
- legacy profile/preflight runtime enforcement;
- wrappers that duplicate bounded file tools;
- document routes that explore repeatedly before calling typed output tools;
- hot-path proposal/self-improvement work;
- debug commands wired into production behavior;
- compatibility branches that only preserve old internal architecture.

## Target Loop

Picobot-style loop:

1. Build small prompt.
2. Decide whether this turn needs no tool, one retrieval/search tool, or one production tool chain.
3. Execute bounded tools.
4. Final answer from available evidence.

Budget:

- normal chat/status: 1 LLM call, 0 tools;
- memory/document question: max 2 LLM calls, 1 retrieval capsule;
- artifact generation: retrieval capsule then document/source/file tool;
- budget exhaustion: answer from last useful tool result, never dead-end apology text.

## Embeddings: Use Them Better, Not More

The embedding problem is quality and placement, not lack of vector infrastructure.

Keep:

- embeddings for wiki/page recall;
- embeddings for compact graph/source/archive facts;
- Qdrant as optional accelerator;
- SQLite FTS as local baseline and fallback.

Fix:

- do not embed raw OCR dumps;
- do not embed every chat turn;
- do not trust raw vector top-K blindly;
- do not mix wiki vector scores and source lexical scores without normalization;
- do not inject retrieval unless the prompt needs retrieval.

Retrieval should be hybrid:

- exact title/slug/link match;
- SQLite FTS lexical hits;
- vector hits from Qdrant/chromem;
- graph neighbor expansion only after a real seed hit;
- per-kind score normalization;
- low-confidence vector results merged with or replaced by local lexical results.

The output is a compact evidence capsule under a byte cap, not a new exploration loop.

## Implementation Tasks

### Task 1: Stop The Bad Final Answer

- Status: done in slice 1.

- Add regression coverage that fails on user-facing `"Mi sono fermato"`.
- Replace `fallbackMessage` with best-effort finalization from the last successful tool result.
- Keep hard failure only when Aura has no model output and no usable evidence.

Acceptance:

- `go test ./internal/agentloop -count=1`.
- `rg -n "Mi sono fermato|fallbackMessage" internal` shows no live user-facing fallback path.

### Task 2: Cut The Loop Down

- Status: done for the profile/preflight path. Default skill manifest, swarm exposure, summarizer, and nightly auto-improve were cut from ordinary turns/defaults in slice 1; this deletion pass removed the remaining live profile/preflight taxonomy and config surface.

- Remove default-turn profile/preflight ceremony from the live loop.
- Keep only simple tool availability, bounded execution, and finalization.
- Make swarm, summarizer, proposals, and self-improvement opt-in or after-turn.
- Preserve skills as files, not as mandatory preflight.

Acceptance:

- Common non-artifact prompts finish in 1 to 2 LLM calls.
- Toolset tests show the small runtime surface.
- No live path requires reading a skill before answering unless the user asked for skill work.

### Task 3: Replace Memory Pack With Retrieval Capsule

- Status: done for the hot path. `ComposeMemoryPack` was deleted, `ComposeRetrievalCapsule` is the only live injection path, and debug smoke now reports `retrieval_capsule_present`.

- Stop injecting graph context and recent wiki log on every turn.
- Add a simple route: `minimal`, `retrieve`, `produce`.
- `minimal` injects nothing.
- `retrieve` injects one compact evidence capsule.
- `produce` uses the capsule before document/file artifact tools.

Acceptance:

- Generic prompts have no Retrieval Capsule.
- Memory/document prompts receive one capsule under the byte cap.
- Debug smoke reports capsule presence, LLM calls, tool calls, and elapsed time.

### Task 4: Fix Embedding Retrieval

- Status: done for exact/FTS/vector merge and Qdrant/local fallback merge. Source/archive score normalization remains in Task 5.

- Merge exact/FTS/vector results instead of trusting vector top-K alone.
- Normalize scores by result kind.
- Make Qdrant fall back or merge when confidence is low.
- Keep local search useful when Qdrant is disabled or bad.

Acceptance:

- Curated wiki/graph queries hit expected slugs in top 3.
- Bad/empty/low-confidence Qdrant results do not hide good local results.
- Existing search/Qdrant tests pass.

### Task 5: Compact Source And Archive Recall

- Status: partial. `search_memory` now calibrates mixed wiki/source/archive scores, lowers source read windows, returns compact snippets, and exposes stable follow-up handles. Durable indexing of compact source/archive/proposal facts is still open.

- Index compact source summaries, anchors, accepted proposals, preferences, and decisions.
- Keep raw source bodies and raw archive turns behind explicit handles.
- Update `search_memory` so wiki/source/archive evidence is calibrated before merging.

Acceptance:

- Source/archive recall returns compact facts first.
- Deep lexical scans are fallback or explicit.
- Evidence includes stable handles for exact follow-up reads.

### Task 6: Make Document Generation Boring

- Status: done for the backend/live smoke slice. The document toolset no longer exposes workspace file tools, source browsing tools, or web tools by default. It keeps `search_memory` plus typed document creators, while the loop respects the DB/dashboard step budget instead of a hidden 4-step policy cap. Tool-call examples are now attached to all tool definitions, and the live document smoke creates/sends DOCX under 30s.
- Follow-up refinement done: examples now live inside structured tool definitions, not ad hoc prompt text. `search_memory` and typed document creators own explicit definitions; legacy/dynamic tools still receive schema-derived definitions through the adapter path.

- Document route must do: retrieval capsule -> `create_docx` or equivalent typed tool.
- Remove repeated broad evidence expansion from document prompts.
- Add malformed-JSON regression tests for evidence-heavy document prompts.

Acceptance:

- Broad document prompt calls retrieval once, then typed document tool.
- Live smoke stays under 30s while respecting the configured `AgentLoopMaxSteps` budget.

### Task 7: Delete The Leftovers

- Status: partial. Profile/preflight leftovers are gone from live source and embedded dashboard assets. `Memory Pack` live source symbols are gone, and intentionally retained explicit tools/settings are documented in `NOT-DELETED.md`. Malformed streamed JSON coverage now handles recoverable missing closers/trailing spillover. Durable compact-memory indexing remains open.

- Audit with `rg` for old profiles, preflight, wrappers, old wiki tools, and production-reachable debug paths.
- Delete what is dead.
- Write `NOT-DELETED.md` only for items intentionally kept.

Acceptance:

- Every deletion candidate is gone or justified.
- Phase 08 closes with fewer hot-path decisions than it started with.

### Task 8: ADK-Style Event Runner

- Status: planned from `google/adk-go` review.

Build the smallest Aura-native version of ADK's orchestration shape:

- `Runner` owns session load/append, invocation ID, route/toolset decision, and event emission.
- `Agent` owns model/tool loop and yields events, not Telegram side effects.
- `Event` records author, branch, tool calls/results, final response, state delta, artifact delta, and terminal status.
- `Toolset` is dynamic (`Tools(ctx)` + predicate/filter), so hidden tools are not sent to the model and unknown-tool recovery is a last-resort safety net.
- `BeforeModel`/`AfterModel` callbacks handle request shaping, usage, parser repair, and observability.
- `BeforeTool`/`AfterTool` callbacks handle duplicate suppression, document-route next-step hints, confirmation gates, and compact result shaping.
- Sub-agent/swarm calls become normal agent tools with bounded child sessions instead of terminal special cases.

Non-goals:

- Do not import ADK as a framework dependency.
- Do not reintroduce profile/preflight taxonomy.
- Do not add telemetry back into the hot path.
- Do not expose broad workspace/source/web tools to document routes.

Acceptance:

- Telegram no longer owns orchestration policy; it adapts runner events to chat messages.
- Document smoke performs `search_memory -> create_docx` with no hidden `read_file` attempt.
- Duplicate/repeated retrieval is rejected by `BeforeTool`, not ad hoc Telegram code.
- `go test ./internal/agentloop ./internal/telegram ./internal/orchestration ./internal/tools -count=1`.
- Live debug sandbox stays under 30s for the document route.

## Verification

Commands:

```powershell
go fmt ./...
go test ./...
docker compose config --quiet
docker compose up -d --build aura
```

Live smokes with the DB-selected model:

- simple chat/status: 0 tools, 1 LLM call;
- "perche Picobot e veloce?": compact evidence, no broad file loop;
- memory/wiki question: one retrieval capsule;
- document summary: retrieval capsule then document tool;
- skill question: reads skill files only when relevant;
- forced budget exhaustion: useful final answer, no `"Mi sono fermato"`.

## Definition Of Done

- Aura is less ceremonial in the hot path.
- The default loop is small enough to reason about.
- Fallbacks produce useful answers instead of self-reporting failure.
- Embeddings improve recall through hybrid retrieval, not through more context injection.
- Summarizer, swarm, proposals, validation, and self-improvement no longer slow ordinary answers.
- Common live routes stay under 30s, with 1 to 2 LLM calls unless the user explicitly asks for heavier work.
