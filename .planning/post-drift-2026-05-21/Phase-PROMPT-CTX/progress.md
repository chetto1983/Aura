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

## 2026-05-26 - PROMPT-01 stable prompt and handoff contract

Status: implemented locally, verified.

Changes:

- Rewrote `internal/conversation/system_prompt.go` as a stable, professional
  operating contract with explicit sections for identity, authority precedence,
  tool policy, minimal routing, memory layers, context capsules, ask-user
  policy, output, and privacy.
- Removed stale `SOUL.md`, `USER.md`, `AGENT.md`, `TOOLS.md`, and retired tool
  references from the stable prompt.
- Replaced the compaction preamble's old `MEMORY.md, USER.md` authority rule
  with a capsule handoff rule: summaries are reference only; live user turns,
  live tool results, retrieved capsules, artifact IDs, run IDs, and
  `tool_attempt` metadata carry the task.
- Fixed turn grounding handling:
  - web always clears or replaces stale grounding each turn;
  - Telegram now sets grounding after ask_user resume/user-message routing and
    no longer clears it before the model sees it.
- Fixed tool execution summaries so loop fallback/duplicate handling use the
  same budgeted/wrapped tool result content that was added to model history.
- Removed `cmd/seed_e2e_env`; live probes now use the existing local QA token
  in `.planning/qa/token.txt`, verified against the active `api_tokens` row.

Probe results:

- `bench-weather-caraglio-location-gate`: pass, 0 tool calls, 1 LLM call,
  7,808 tokens, 10.4s.
- `doc-xlsx-roundtrip`: pass, 2 tool calls, 3 LLM calls, 23,784 tokens, 129.9s
  (behavior correct but still slow).
- `prompt-health`: redaction passed; new prompt snapshot:
  - `stable_chars=9052`
  - `total_chars=11613`
  - `stable_hash=38c079f35b287966`
  - `runtime_capsule_chars=287`
  Historical 24h outliers remain in the DB window, with max prompt tokens still
  775,137 until the old turns age out.

Verification:

- `go test ./internal/conversation ./internal/agent -run "Prompt|Capsule|Grounding|SummaryPrefix|Clarification" -count=1`
- `go test ./internal/conversation ./internal/agent -count=1`
- `go test ./cmd/aura -run "HubBackedWebChatClearsStaleGroundingCapsule|HubBackedWebChatUsesSessionStoreContext|HubBackedWebChatCompactsToolResultsBeforeNextTurn" -count=1`
- `go test ./internal/agent -run "ExecuteToolCalls|Prompt|Capsule|Grounding" -count=1`
- `go test ./internal/conversation -run "Prompt|SummaryPrefix|CompactCompletedToolResults" -count=1`
- `go test ./internal/conversation ./internal/agent ./internal/channels/telegram ./cmd/aura -count=1`
- `go test ./internal/db ./cmd/aura ./internal/conversation ./internal/agent ./internal/channels/telegram -count=1`
- `go test ./cmd/... -run TestDoesNotExist -count=0`
- `docker compose build aura`
- `docker compose up -d aura`
- `docker inspect ... aura-aura-1` reports `healthy`.
- `go run ./cmd/probe_chat -case bench-weather-caraglio-location-gate -db D:\Aura\data\aura.db -json -timeout 180`
- `go run ./cmd/probe_chat -case doc-xlsx-roundtrip -db D:\Aura\data\aura.db -json -timeout 180`
- `go run ./cmd/probe_chat -case prompt-health -db D:\Aura\data\aura.db -json -timeout 180`

Next:

- Add the typed `ContextCapsule` builder from PROMPT-02 so time, grounding,
  retrieval, operational lessons, skills, summaries, and tool capsules are
  rendered with one deterministic budget/order contract instead of ad hoc
  string appends.
- Add a benchmark for archive bloat: archive currently records loop messages
  before post-turn completed-tool compaction, so archive/search may retain more
  payload than the next live session context.

## 2026-05-26 - PROMPT-01B natural skill authoring probe

Status: implemented locally, verified.

Why:

- The first skill probe was too leading: it proved Aura can create a skill when
  the user nearly names the implementation path, not that Aura can infer the
  right owner path from natural language.
- A natural prompt, "teach yourself a reusable procedure...", initially routed
  to `wiki_page` and claimed success while no skill existed. That is the real
  failure mode this slice preserves.

Changes:

- Added a natural `probe_chat` case, `natural-skill-authoring`, that does not
  mention tools, APIs, filesystem paths, or schema names in the user prompt.
- The verifier checks `/api/skills/{name}` for ground truth, rejects `wiki_page`
  as the owner path, enforces <=5 tool calls and <=40,000 tokens, and cleans up
  the temporary skill directory after verification.
- Updated the stable system prompt so natural user phrases such as "teach
  yourself a reusable procedure" route to local skill authoring rather than
  wiki pages.
- Added a compact local skill template to the system prompt and told Aura not
  to list/read unrelated skills or call mkdir separately when name and behavior
  are already supplied.
- Removed dead `IsComposeAuraRunning`, which was the CI deadcode failure after
  `cmd/seed_e2e_env` was deleted.

Live probe progression:

- Before routing fix: `wiki_page` created `aura-procedure-smoke-20260526b`;
  `/api/skills/...` returned 404. The test artifact was deleted.
- After routing fix but before template: artifact was a skill, but 6-8 tool
  calls and 61k-72k tokens kept the benchmark red.
- Final run:
  `go run ./cmd/probe_chat -case natural-skill-authoring -db D:\Aura\data\aura.db -json -timeout 240`
  passed with 2 tool calls, 3 LLM calls, 24,192 tokens, 17.5s, and no
  mismatches. The temporary skill was verified through the API and cleaned up.

Prompt snapshot:

- `prompt-health` redaction passed.
- `stable_chars=9833`
- `total_chars=12394`
- `stable_hash=8801cb91fce9e7c0`
- Historical 24h token outliers remain in the DB window until old turns age
  out.
