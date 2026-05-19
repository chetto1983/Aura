# QA Surface Summary - 2026-05-19 - git_head: 4e2eb06d3ce7c02044223b47d5e14a76d15ff4b3

Phase 1 synthesis for the Aura Q&A pipeline. This phase is static discovery plus orchestrator spot-checking only; no live chat probe cases were executed.

## Inputs

| Artifact | Role | Result |
|---|---|---|
| `.planning/qa/run-head.txt` | pinned run head | `4e2eb06d3ce7c02044223b47d5e14a76d15ff4b3` |
| `docs/qa-tool-surface.md` | LLM-callable tool inventory | 23 verified tools |
| `docs/qa-channel-surface.md` | ingress/channel inventory | telegram, web, cron, swarm |
| `docs/qa-failure-modes.md` | dependency and internal failure-mode matrix | external + internal modes mapped |
| `.planning/qa/lint-baseline.json` | existing lint debt baseline | 55 issues |

Previous Phase 1 inventories were versioned before this run:

- `docs/qa-tool-surface-previous.md`
- `docs/qa-channel-surface-2026-05-18.md`
- `docs/qa-failure-modes-previous.md`

## Headline Counts

Tool surface, per `docs/qa-tool-surface.md`:

- static-registry: 19
- curated-set: 0
- skill-manifest: 0
- mcp-dynamic: 0 configured in this workspace
- swarm: 4
- TOTAL: 23
- `total_verified: true`

Channel surface, per `docs/qa-channel-surface.md`:

- telegram inbound path
- web `/api/chat` path
- cron agent-job dispatch path
- swarm hub bridge path

Failure-mode surface, per `docs/qa-failure-modes.md`:

- External dependency modes include Qdrant, embedding provider, SearxNG, Garage, Mistral OCR, OpenAI-compatible LLM, and MCP servers.
- Internal modes include max elapsed, max iterations, empty LLM response, malformed tool call, sandbox unavailable, capability denial, phantom tool, duplicate tool result deduplication, 429 rate limiting, and transient network failure.

Lint baseline:

- `errcheck`: 50
- `govet`: 2
- `ineffassign`: 1
- `staticcheck`: 2
- total: 55

Existing lint debt is the baseline for later QA phases; the pipeline must not increase it.

## Orchestrator Spot Checks

The orchestrator re-derived representative rows from source instead of trusting surveyor text.

Tool surface checks:

- `file` resolves to `internal/agent/tools/registry/file.go:178`
- `web` resolves to `internal/agent/tools/registry/web.go:115`
- `execute_code` resolves to `internal/agent/tools/registry/exec.go:129`
- `subagent_dispatch` resolves to `internal/agent/tools/registry/subagent.go:193`

Channel surface checks:

- Telegram entry resolves to `internal/telegram/handlers.go:103`
- Web chat entry resolves to `internal/channels/web/chat_service.go:57`
- Cron agent-job dispatch resolves to `internal/channels/cron/dispatcher.go:13` and `internal/channels/cron/dispatcher.go:25`
- Swarm hub bridge resolves to `internal/swarm/hub_bridge.go:59`

Failure-mode checks:

- Qdrant health check resolves to `internal/storage/qdrant/client.go:58` and requests `/readyz`
- Mistral OCR env key resolves to `internal/config/config.go:147` and `internal/config/config.go:306`
- Malformed tool call classification resolves to `internal/llm/classify.go:30` and `internal/llm/classify.go:93`
- Duplicate tool results resolve to `internal/agent/loop_dedup.go:25`
- Max-iteration and phantom-tool paths resolve to `internal/agent/loop.go:189`, `internal/agent/loop.go:363`, and `internal/agent/loop.go:629`

## Repair Note

The first `docs/qa-failure-modes.md` pass contained stale or imprecise static references. A repair pass corrected:

- nonexistent `internal/agent/tools/registry/execute_code.go` references to `internal/agent/tools/registry/exec.go`
- nonexistent `MISTRAL_BASE_URL` references to `MISTRAL_OCR_BASE_URL`
- Garage credential wording
- OCR client line ranges

The orchestrator then verified that `docs/qa-failure-modes.md` no longer contains `execute_code.go` or `MISTRAL_BASE_URL`.

## Phase 1 Verdict

PASS.

Stop conditions met:

- Three current surface inventories exist.
- Prior inventories were preserved with versioned names.
- Tool count is explicit and verified.
- Representative rows were spot-checked from source.
- Lint baseline was captured before live QA.
- Git HEAD was pinned in `.planning/qa/run-head.txt`.

## Phase 2 Readiness

Ready:

- `http://localhost:18080/health` returns `{"status":"alive"}` via `curl.exe`.
- Docker services are running: `aura`, `qdrant`, and `searxng`.
- Phase 1 artifacts are available for coverage-auditor agents.

Blocked:

- `AURA_CHAT_TOKEN` is missing, so the live `cmd/probe_chat -json` baseline must not run yet.

Next command once a valid token is available:

```powershell
go run ./cmd/probe_chat -json
```

Expected Phase 2 first artifact:

- `docs/qa-runs/baseline-run.json`
