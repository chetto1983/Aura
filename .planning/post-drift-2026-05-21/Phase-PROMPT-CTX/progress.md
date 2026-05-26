# Phase-PROMPT-CTX Progress

Role: resume.

## 2026-05-26 - Plan drafted

Status: planned, not implemented.

Sources reviewed:

- `D:/Aura/internal/conversation/system_prompt.go`
- `D:/Aura/internal/agent/promptplan.go`
- `D:/Aura/internal/agent/context_grounding.go`
- `D:/Aura/internal/agent/toolsprovider.go`
- `D:/Aura/internal/channels/telegram/invocation_builder_helpers.go`
- `D:/tmp/system_prompts_leaks/OpenAI/codex/gpt-5-codex.md`
- `D:/tmp/system_prompts_leaks/OpenAI/ChatGPT-GPT-5-Agent-mode-System-Prompt.md`
- `D:/tmp/system_prompts_leaks/Anthropic/Official/claude-opus-4.7.md`
- `D:/tmp/system_prompts_leaks/Google/gemini-cli.md`
- `D:/tmp/system_prompts_leaks/Misc/cursor.md`
- `D:/tmp/system_prompts_leaks/Misc/vscode-copilot-agent.md`
- `D:/tmp/openhuman/gitbooks/developing/architecture/agent-harness.md`
- `D:/tmp/nanobot/nanobot/agent/context.py`
- `D:/tmp/hermes-agent/agent/conversation_loop.py`

Decisions:

- Do not restore `AGENT.md`, `USER.md`, `SOUL.md`, or `TOOLS.md` as
  always-loaded runtime prompt files.
- Treat repo `AGENTS.md` as developer/coding-agent guidance only.
- Move Aura toward stable system prompt plus typed context capsules.
- First implementation slice must be PROMPT-00 baseline metrics and prompt
  snapshot, not a prompt rewrite.

Verification:

- Not run. Planning-only slice.

## 2026-05-26 - PROMPT-00 baseline metrics shipped

Status: implemented, container updated, committed, pushed, CI green.

Changes:

- Added SQLite prompt-health views:
  - `prompt_health_turns_24h`
  - `prompt_health_rollup_24h`
  - `prompt_health_tool_outcomes_24h`
  - `prompt_health_memory_kinds`
  - `prompt_health_run_events_24h`
- Added `cmd/probe_chat -case prompt-health`, which reads the live DB, emits a
  redacted prompt/context health packet, and writes:
  `.planning/post-drift-2026-05-21/Phase-PROMPT-CTX/prompt-health-baseline.json`.
- Updated the running `aura` container so migration 29 is applied to
  `/data/aura.db`.

Live baseline:

- `views_present=true`
- 24h turns: 461
- assistant turns: 229
- distinct chats: 10
- LLM calls: 226
- tool calls: 128
- compactions: 1
- prompt tokens total: 19,571,945
- prompt token p50: 97,933
- prompt token p95: 670,944
- max prompt tokens: 775,137
- prompt snapshot chars: 5,155
- legacy overlay references still present in current prompt snapshot:
  `USER.md`, `SOUL.md`

Verification:

- `go test ./internal/db/migrations -run "PromptHealth|RunCreatesCurrentFreshSchema" -count=1`
- `go test ./cmd/probe_chat -run PromptHealth -count=1`
- `go test ./cmd/probe_chat ./internal/db/migrations ./cmd/aura -count=1`
- `go build ./cmd/probe_chat ./cmd/aura`
- `git diff --check -- cmd/probe_chat internal/db/migrations .planning/post-drift-2026-05-21/INDEX.md`
- `go vet ./...`
- `go test ./internal/conversation ./internal/agent -run "Prompt|Capsule|Grounding" -count=1`
- `docker compose up -d --build aura`
- `docker compose exec -T aura sqlite3 -readonly -json /data/aura.db "SELECT name FROM sqlite_master WHERE type='view' AND name LIKE 'prompt_health_%' ORDER BY name;"`
- `go run ./cmd/probe_chat -case prompt-health -db ./data/aura.db -json`
- `go run ./cmd/probe_chat -case prompt-health -db ./data/aura.db`
- `docker compose ps aura` reports `healthy`.
- Commit: `69f87e40` (`feat(obs): add prompt health baseline metrics`)
- Push: `origin/master`
- CI: GitHub Actions `CI` run `26452913076` passed for
  `69f87e40732ef9e6dfe70ea6349b625252efd723`.

Next:

- PROMPT-01 should remove the stale `USER.md`/`SOUL.md` prompt contract and
  replace the base prompt with explicit memory/tool/context rules.
