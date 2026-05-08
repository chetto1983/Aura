# Phase 08: Runtime Diet

Status: planned
Owner: Codex
Date: 2026-05-08

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
- `composeTurnMemoryPack` still performs speculative search and injects graph context plus recent wiki log on every turn.
- `search_memory` mixes vector wiki scores with lexical source/archive scores as if they were comparable.
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
- summarizer;
- proposals;
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
- hot-path summarizer/proposal/self-improvement work;
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

- Add regression coverage that fails on user-facing `"Mi sono fermato"`.
- Replace `fallbackMessage` with best-effort finalization from the last successful tool result.
- Keep hard failure only when Aura has no model output and no usable evidence.

Acceptance:

- `go test ./internal/agentloop -count=1`.
- `rg -n "Mi sono fermato|fallbackMessage" internal` shows no live user-facing fallback path.

### Task 2: Cut The Loop Down

- Remove default-turn profile/preflight ceremony from the live loop.
- Keep only simple tool availability, bounded execution, and finalization.
- Make swarm, summarizer, proposals, and self-improvement opt-in or after-turn.
- Preserve skills as files, not as mandatory preflight.

Acceptance:

- Common non-artifact prompts finish in 1 to 2 LLM calls.
- Toolset tests show the small runtime surface.
- No live path requires reading a skill before answering unless the user asked for skill work.

### Task 3: Replace Memory Pack With Retrieval Capsule

- Stop injecting graph context and recent wiki log on every turn.
- Add a simple route: `minimal`, `retrieve`, `produce`.
- `minimal` injects nothing.
- `retrieve` injects one compact evidence capsule.
- `produce` uses the capsule before document/file artifact tools.

Acceptance:

- Generic prompts have no Memory Pack.
- Memory/document prompts receive one capsule under the byte cap.
- Debug smoke reports route, capsule bytes, LLM calls, tool calls, and elapsed time.

### Task 4: Fix Embedding Retrieval

- Merge exact/FTS/vector results instead of trusting vector top-K alone.
- Normalize scores by result kind.
- Make Qdrant fall back or merge when confidence is low.
- Keep local search useful when Qdrant is disabled or bad.

Acceptance:

- Curated wiki/graph queries hit expected slugs in top 3.
- Bad/empty/low-confidence Qdrant results do not hide good local results.
- Existing search/Qdrant tests pass.

### Task 5: Compact Source And Archive Recall

- Index compact source summaries, anchors, accepted proposals, preferences, and decisions.
- Keep raw source bodies and raw archive turns behind explicit handles.
- Update `search_memory` so wiki/source/archive evidence is calibrated before merging.

Acceptance:

- Source/archive recall returns compact facts first.
- Deep lexical scans are fallback or explicit.
- Evidence includes stable handles for exact follow-up reads.

### Task 6: Make Document Generation Boring

- Document route must do: retrieval capsule -> `create_docx` or equivalent typed tool.
- Remove repeated broad evidence expansion from document prompts.
- Add malformed-JSON regression tests for evidence-heavy document prompts.

Acceptance:

- Broad document prompt calls retrieval once, then typed document tool.
- Live smoke stays under 30s and `loop_steps <= 4`.

### Task 7: Delete The Leftovers

- Audit with `rg` for old profiles, preflight, wrappers, old wiki tools, and production-reachable debug paths.
- Delete what is dead.
- Write `NOT-DELETED.md` only for items intentionally kept.

Acceptance:

- Every deletion candidate is gone or justified.
- Phase 08 closes with fewer hot-path decisions than it started with.

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
