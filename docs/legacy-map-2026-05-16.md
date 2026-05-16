# Legacy Map 2026-05-16

Role: orientation/evidence.
Status: first-pass map, not an executable queue.

This document records the general lint pass and the first current-code map of
legacy/compatibility surfaces. Use it to choose the next bounded Aura slice; do
not run old wave plans from here.

## Verification Snapshot

| Command | Result | Notes |
| --- | --- | --- |
| `go vet ./...` | PASS | No output. |
| `go build ./...` | PASS | No output. |
| `go test ./... -count=1` | PASS | Includes `web/node_modules/flatted/golang/pkg/flatted` as a no-test Go package. |
| `npm --prefix web run build` | PASS | Vite warns that some chunks are larger than 500 kB. |

One earlier `go test ./... -count=1` run reported
`TestHubSwarm_ToolAllowlistDenied`, but the current `internal/chat` test file
does not contain that test name, the targeted rerun matched no tests, and the
full rerun passed. Treat that earlier result as stale/transient unless it
reappears.

Current local HEAD observed during this map: `f3cf82e5`.

## Dirty Worktree Context

Before this report was added, the worktree already contained WIP in:

- Phase06 lesson promotion progress.
- `Dockerfile`.
- Web chat bridge/tests in `cmd/aura/web_chat.go` and
  `cmd/aura/web_chat_test.go`.
- Agent tool execution helpers and registry exec wiring.
- Telegram channel resume/inbound/invocation files.
- Core Telegram bot/tool execution helpers and tests.
- `scripts/ralph/progress.txt`.

Preserve this WIP unless a selected slice explicitly owns it.

Resume-state drift to repair when resume docs are selected:
`.planning/HANDOFF.json` still records `last_known_git.head` as `7d3b7ebc`,
while the current local HEAD observed here is `f3cf82e5`.

## Current Route

- Phase04 is closed for the legacy `agent.Runner` removal and `RunTask`
  collapse.
- Phase07C is repaired as canonical planning for Projection Freshness Registry.
- If continuing Phase07, the next open slice is Phase07D.
- Phase08 and Phase09 still need plan promotion before implementation.
- Historical docs and Ralph completed queues remain evidence, not execution
  authority.

## Live Code Search Results

Search in `cmd` and `internal` for old runtime markers:

- No live Go code matches `agent.Runner`, `NewRunner`,
  `internal/agentloop`, `internal/agentruntime`, `AURA_USE_HUB`,
  `BuildVectorIndex`, or `legacy_extractors`.
- The only current Go hits in that marker search are comments around
  `ExtractGo` replacement in source ingest/tool docs:
  `internal/storage/sources/store/extract_auto.go` and
  `internal/agent/tools/registry/source_unified.go`.

Historical files still mention stale runtime paths and should stay treated as
evidence only:

- `.planning/CONTEXT-ENGINEERING-ROADMAP.md`.
- `docs/wave-3-chathub-slice0.md`.
- `docs/aura-restructure-prd*.md`.
- `scripts/ralph/prd-completed-*.json`.

## Main Legacy/Compat Surfaces

### Composition Root

`cmd/aura/app.go` is still the main wiring knot at about 1076 LOC. It imports
nearly every runtime, storage, channel, tool, cron, swarm, memory, and config
package. It is valid as composition root, but it is also where bridge ownership
gets hard to see:

- Web chat is wired through `newHubBackedWebChatService`.
- Cron agent jobs are wired through `agentJobRunnerAdapter`.
- Swarm background agents are wired through `swarmRunnerAdapter`.
- Tool registry, projection freshness seeding, cron setup, swarm tools, and
  web/API setup all live in the same file.

Do not split this broadly without a selected phase. The first safe move is a
caller/owner table for one bridge family at a time.

### Chat Hub And Agent Runtime

The canonical interactive path is now `chat.Hub` to `chat.AgentLoop` to
`agent.Run`. Important files:

- `internal/chat/hub.go`
- `internal/chat/agentloop.go`
- `internal/agent/loop.go`
- `internal/agent/runtask.go`

The old `agent.Runner` surface is gone from live Go code. Remaining
compatibility risk is not duplicate runner code; it is the adapter layer where
non-streaming callers still use `agent.RunTask`.

### Web Chat

Web chat is Hub-backed, but the current WIP touches the bridge heavily:

- `cmd/aura/web_chat.go`
- `cmd/aura/web_chat_test.go`
- `internal/channels/web/chat_service.go`
- `internal/channels/web/outbound.go`

This is no longer the old direct `/api/chat` runner path, but it remains a
compatibility bridge because the API response shape must stay scalar and stable
for `api.ChatReply`.

### Telegram

`internal/telegram` is still a large adapter package with many direct imports
into agent, tools, storage, auth, cron, memory, wiki, and swarm. The Hub
entrypoint is visible through:

- `cmd/aura/main.go`
- `internal/channels/telegram/invocation_builder.go`
- `internal/telegram/bot.go`

Current WIP touches resume/inbound/tool-exec files on both the channel adapter
side and the core Telegram package side. Treat Telegram as the highest
regression-risk channel when selecting cleanup slices.

### Cron

Cron is partly channelized:

- `internal/channels/cron/dispatcher.go`
- `cmd/aura/app.go` cron Hub wiring

The older handler still owns reminders, wiki maintenance, run-now, agent-job
result persistence, and issue scanning in `internal/cron/dispatch.go` (about
570 LOC). Phase08 should map these separately before changing behavior.

### Swarm And Subagents

There are two related surfaces:

- Legacy/background swarm manager path:
  `internal/swarm` plus `internal/agent/tools/swarm`.
- Newer Hub/subagent dispatch path:
  `internal/agent/tools/registry/subagent.go`,
  `cmd/aura/adapters.go`, and `internal/swarm/hub_bridge.go`.

`internal/swarm/hub_bridge.go` imports `chat`, so the bridge is still coupled
to the Hub. That is acceptable as a transition surface, but Phase08 should make
the final RunGraph boundary explicit before extending team/swarm behavior.

### Tools

Tool registration is mostly consolidated under `internal/agent/tools`, but the
registry remains a high-weight package:

- `internal/agent/tools/registry/memory_search.go` is about 681 LOC.
- `internal/agent/tools/registry/scheduler.go` is about 539 LOC.
- `internal/agent/tools/swarm/tools.go` is about 536 LOC.

The tool authority model is active and test-covered, so cleanup should be
structural only unless the selected phase owns a policy change.

### RAG, Memory, And Freshness

Current active storage surfaces:

- `internal/storage/memoryindex`
- `internal/storage/search`
- `internal/storage/freshness`
- `internal/storage/reindex`

`embedding_cache` is still a compatibility bridge in the main DB plane per the
decision log. Phase07D-F should continue from the Phase07C registry plan and
avoid treating stale vector state as source truth.

### Source Ingest

Markitdown/source ingest appears mostly migrated, but comments still reference
`ExtractGo` replacement:

- `internal/storage/sources/store/extract_auto.go`
- `internal/agent/tools/registry/source_unified.go`

No default-build legacy extractor path was found in live Go code during this
pass.

### Planning Legacy

The most important planning legacy is not code; it is old docs that still name
dead packages and old execution routes. Keep these as evidence:

- Wave 1/2/3 docs.
- `docs/aura-master-plan*.md`.
- `docs/aura-restructure-prd*.md`.
- Ralph completed queues.

Canonical execution remains `prd.md`, the decision log, deep-refactor `INDEX`,
and the selected phase/subphase files.

## Size Hotspots

Largest current Go files observed:

| Lines | File | Notes |
| ---: | --- | --- |
| 1221 | `internal/db/migrations/migrations.go` | Migration accumulator, high churn risk. |
| 1076 | `cmd/aura/app.go` | Composition root and bridge knot. |
| 764 | `internal/agent/loop.go` | Canonical agent loop. |
| 741 | `internal/storage/memoryindex/store.go` | Core compact memory index store. |
| 734 | `internal/storage/runs/store.go` | Run/event/question persistence. |
| 706 | `internal/wiki/memory_hygiene.go` | Wiki hygiene logic. |
| 681 | `internal/agent/tools/registry/memory_search.go` | LLM-facing memory retrieval tool. |
| 621 | `internal/storage/sources/ingest/pipeline.go` | Source compile/extraction pipeline. |
| 570 | `internal/cron/dispatch.go` | Mixed cron handler responsibilities. |
| 553 | `internal/llm/openai.go` | Provider client. |
| 539 | `internal/agent/tools/registry/scheduler.go` | Schedule tool surface. |
| 536 | `internal/agent/tools/swarm/tools.go` | Swarm LLM tools. |

Files over 600 LOC are not automatically wrong, but these should be treated as
map-first areas before broad edits.

## First Bounded Follow-Ups

1. Phase07D continuation: rebuild Phase07D from current code and the Phase07C
   registry plan, then define one benchmark that proves projection freshness
   behavior against SQLite rows/API/tool output.
2. Phase08 readiness map: before code, produce a cron/swarm RunGraph caller map
   from `cmd/aura/app.go`, `internal/cron`, `internal/channels/cron`,
   `internal/swarm`, and subagent dispatch.
3. Web/Telegram bridge WIP review: if current WIP belongs to the active slice,
   finish its narrow verification; otherwise leave it untouched and select a
   non-overlapping planning slice.
4. Tooling hygiene: decide whether `go test ./...` should intentionally include
   `web/node_modules/flatted/golang/pkg/flatted`; if not, adjust the local test
   command or repo layout in a separate tooling slice.

## Non-Goals

- Do not delete historical docs just because they mention old paths.
- Do not run Ralph/GSD queues as Aura execution state.
- Do not refactor `cmd/aura/app.go` broadly until one bridge family is selected.
- Do not change Telegram behavior as part of a map-only slice.
