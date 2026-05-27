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

## 2026-05-26 - PROMPT-01C schedule-and-run routine probe

Status: implemented locally, verified.

Why:

- The natural Caraglio news request must behave like a real user request:
  create the 08:30 daily routine, execute that saved routine immediately, and
  answer with the news only. A previous run scheduled the job but answered from
  an ad hoc web search, leaving `last_run_at` and `last_output` empty.

Changes:

- Wired the agent-job runner into `cron.NewHandler` so `task run_now` from the
  normal API/tool surface no longer fails with `agent runner unavailable`.
- Added `run_now` to the unified `task` schedule schema so a recurring
  `agent_job` can be saved and fired immediately in one call.
- Updated the stable prompt and task capsule to make schedule requests action
  requests, and to forbid substituting ad hoc search/web output when the user
  asks to schedule a routine and see it now.
- Added coverage for `schedule` with `run_now=true`, proving persistence happens
  before the manual saved-task run.

Verification:

- `go test ./internal/conversation ./internal/agent/tools/registry ./internal/cron -run "Prompt|Task|Scheduler|RunNow|AgentJob" -count=1`
- `go test ./cmd/aura -run "TestAgentJobRunnerAdapterPassesRequestRunID|TestSwarmRunnerAdapterPassesContextRunID" -count=1`
- `git diff --check`
- `docker compose build aura`
- `docker compose up -d aura`
- `docker inspect ... aura-aura-1` reports `healthy`.

Live probes:

- Direct `/api/tools/call` with `task schedule`, `daily=08:30`, and
  `run_now=true` created `probe-direct-caraglio-news`, executed it immediately,
  returned `status=completed`, and persisted a Caraglio news summary. The
  temporary task was deleted after verification.
- Negative direct `/api/tools/call` without `run_now` created
  `probe-direct-no-now-caraglio` with `next_run_at=2026-05-27T06:30:00Z` and
  empty `last_run_at`, `last_error`, and `last_output`. The temporary task was
  deleted after verification.
- Natural `/api/chat` prompt:
  "Da adesso in poi ogni mattina alle 8:30 prepararmi un breve riassunto delle
  notizie locali di Caraglio (CN). Fammi vedere subito il risultato di oggi..."
  passed functionally:
  - reply contained only the local news summary with sources, no tool or
    infrastructure explanation;
  - `tools_used=["web","task"]`;
  - `tool_calls=2`, `llm_calls=3`, `tokens=26,938`, `elapsed_ms=51,231`;
  - `tool_attempts.arg_keys_json=["action","daily","kind","name","payload","run_now"]`
    for the task call;
  - `scheduled_tasks.name=notizie_caraglio_daily`;
  - `schedule_daily=08:30`;
  - `next_run_at=2026-05-27T06:30:00Z`;
  - `last_run_at=2026-05-26T20:29:37Z`;
  - `last_error=''`;
  - `last_output` matched the user-visible Caraglio news summary;
  - `last_metrics_json={"skipped":false,"llm_calls":2,"tool_calls":1,"tokens_prompt":9132,"tokens_completion":864,"tokens_total":9996,"elapsed_ms":13573}`.

Residual:

- The final natural run used the optimal saved-task `run_now=true` shape, but it
  still spent one `web` call before scheduling. Behavior is correct; there is
  still avoidable pre-task retrieval overhead for schedule-plus-immediate-run
  prompts.

## 2026-05-26 - PROMPT-01D user-profile graph recall

Status: implemented locally, verified.

Why:

- The natural prompt "Fammi un riassunto delle cose che sai di me, sia
  lavorative che personali" should use Aura's user memory plus wiki graph,
  not generic keyword search loops. The first live run made 7 `search` calls,
  8 LLM calls, 72,361 tokens, and answered from generic wiki hits while missing
  direct graph facts such as the user profile node.

Changes:

- Updated the stable prompt routing for "what do you know about me?"-style
  questions:
  - call `search(action=user_facts)` first;
  - then call `search(action=subgraph)` with a bounded user-profile query;
  - do not answer from generic keyword searches alone;
  - do not use web;
  - separate explicit `user_memory` facts from wiki/source graph inferences.
- Added prompt contract assertions for the new user-profile graph routing.

Verification:

- `go test ./internal/conversation -run Prompt -count=1`
- `docker compose build aura`
- `docker compose up -d aura`
- `docker inspect ... aura-aura-1` reports `healthy`.

Live probe:

- Natural `/api/chat` prompt:
  "Fammi un riassunto delle cose che sai di me, sia lavorative che personali."
  passed:
  - `tools_used=["search"]`;
  - `tool_calls=2`, `llm_calls=3`, `tokens=25,006`, `elapsed_ms=26,922`;
  - first tool attempt arg keys: `["action","limit"]`;
  - second tool attempt arg keys: `["action","budget_tokens","depth","query"]`;
  - conversation tool output contained `CAPSULE wiki_subgraph`;
  - no `web` attempts;
  - reply included personal graph facts: `Davide Marchetto`, birth date
    `25 dicembre 1983`, residence `Caraglio`;
  - reply included work/source graph inferences from `Albatech.xlsx`,
    calibration, TCP/coordinate systems, axes, and calandras.

Residual:

- Token use is much lower than the failed run but still high for a two-call
  recall turn. The next context-builder slice should reduce stable prompt and
  schema overhead rather than adding more routing prose.

## 2026-05-26 - PROMPT-01E natural long workflow scratchpad probe

Status: implemented locally, verified.

Why:

- Aura needs a realistic long-conversation probe where the user never names
  tools, but the model must combine profile recall, current/local web evidence,
  a private scratchpad, a generated workspace file, and a final checklist close.
- The first exploratory run exposed two real prompt/harness gaps: a recoverable
  `agent_note` schema error and an unnecessary `tool_search` of the `file`
  schema, which inflated the following turn context.

Changes:

- Added `natural-long-workflow-scratchpad` to `cmd/probe_chat`.
- The probe now runs three natural turns:
  - prepare a micro-briefing by recalling user facts, checking recent Caraglio
    news, and keeping an internal checklist;
  - continue from the prior turn and write a Markdown artifact in the workspace;
  - reread the checklist and artifact, then clear the scratchpad.
- The verifier fails on recoverable/blocked/error tool attempts, forbidden
  `tool_search`, `execute_code`, or `execute_shell`, missing file/scratchpad
  artifacts, excessive archived tool rows, and `tokens_in` above 80,000.
- Updated the stable prompt with concrete `agent_note` actions and direct
  known-path `file(action=write, path, content)` guidance.

Verification:

- `go test ./internal/conversation -run Prompt -count=1`
- `go test ./cmd/probe_chat -run TestDoesNotExist -count=0`
- `git diff --check`
- `docker compose build aura`
- `docker compose up -d aura`
- `docker inspect ... aura-aura-1` reports `healthy`.

Live probe:

- Command:
  `go run ./cmd/probe_chat -case natural-long-workflow-scratchpad -db D:\Aura\data\aura.db -json -timeout 360`
- Prompt 1:
  "Sto preparando un micro-briefing operativo per domani. Recupera quello che
  sai di me, controlla le notizie locali recenti di Caraglio (CN), e tieniti
  una checklist interna..."
- Result:
  - `pass=true`;
  - first reply confirmed profile recall, local news research, and checklist
    creation without naming tool internals;
  - tool attempts were exactly `search`, `web`, `agent_note`, `file`,
    `agent_note`, `file`, `agent_note`;
  - all attempts had `outcome=ok`;
  - no `tool_search`, `execute_code`, or `execute_shell` attempts;
  - the Markdown artifact was written directly with arg keys
    `["action","content","path"]`;
  - scratchpad was read and cleared by the close turn;
  - archived conversation rows for the case: `18`;
  - archived tool rows: `7`;
  - archived tool-call count: `7`;
  - max archived `tokens_in`: `75,191`;
  - no conversation compaction was triggered for the case.

Residual:

- The improved prompt avoided the schema-discovery bloat path and stayed below
  the probe threshold, but `max_tokens_in=75,191` is still high for three turns.
  The next slice should reduce carried tool-output/schema mass rather than
  adding more routing prose to the stable prompt.

## 2026-05-26 - PROMPT-01F natural multiagent orchestration probe

Status: implemented locally, verified.

Why:

- OH3 research documents point to safe parent-orchestrated fan-out, frozen
  isolated workers, compact child returns, no peer mesh, and metrics based on
  useful critical path rather than agent count.
- Aura already had the safe substrate (`orchestrator -> summarizer`) but the
  Docker runtime kept `AURABOT_ENABLED=false`, so natural web/API turns could
  not validate the parent/child path.
- The first natural probe proved two real gaps:
  - with the old worker description, Aura summarized a user-provided noisy
    payload in the parent and falsely mentioned child work;
  - after widening the worker description, Aura delegated but too late when the
    payload was oversized for the wall-clock budget.

Changes:

- Enabled the dev Docker swarm runtime with conservative defaults:
  `AURABOT_ENABLED=true`, `AURABOT_MAX_ACTIVE=2`,
  `AURABOT_MAX_DEPTH=3`, `AURABOT_TIMEOUT_SEC=300`,
  `AURABOT_MAX_ITERATIONS=100`.
- Updated the orchestrator prompt and stable system prompt so Aura treats
  bulky payload compression/noisy extraction as specialist work when the user
  naturally asks Aura to organize or split multi-step work.
- Expanded the summarizer worker contract from tool outputs only to noisy
  user-provided payloads, logs, traces, or tool results.
- Added `cmd/probe_chat` metrics support and two natural swarm cases:
  - `natural-multiagent-orchestrator-scratchpad`: private checklist plus
    delegated payload compression and final synthesis with preserved facts;
  - `natural-multiagent-no-overdelegation`: a simple one-shot summary that must
    not spawn child work.
- Trimmed the `task` tool description back under the 1500-byte catalog cap
  found by the broader agent package gate.

Verification:

- `go test ./internal/conversation -count=1`
- `go test ./internal/agent/agentdef ./internal/swarm ./internal/agent/tools/swarm -count=1`
- `go test ./cmd/probe_chat -run TestDoesNotExist -count=0`
- `go test ./internal/agent/... ./internal/conversation ./cmd/probe_chat -count=1`
- `go test ./internal/agent/tools/registry -count=1`
- `docker compose build aura`
- `docker compose up -d aura`
- Logs confirm `AuraBot swarm enabled` and `tool registry built`.

Live probes:

- Command:
  `go run ./cmd/probe_chat -smoke tools-swarm -json -timeout 420`
- Result:
  - `natural-multiagent-no-overdelegation`: pass, `tool_calls=0`,
    `llm_calls=1`, `tokens=8,087`, `elapsed_ms=13,269`,
    `swarm_rows=0`, `critical_steps_estimate=1`.
  - `natural-multiagent-orchestrator-scratchpad`: pass, `tool_calls=2`,
    `llm_calls=3`, `tokens=37,511`, `elapsed_ms=108,566`,
    `tool_attempts.agent_note=1`,
    `tool_attempts.delegate_summarizer=1`,
    `swarm_summarizer_rows=1`, `swarm_reflector_rows=1`,
    `child_completed_tasks=1`, `child_failed_tasks=0`,
    `child_llm_calls=1`, `child_tool_calls=0`,
    `child_tokens_total=3,377`, `child_max_elapsed_ms=28,209`,
    `critical_steps_estimate=4`, `context_rows=6`,
    `context_tool_rows=2`, `context_max_tokens_in=37,511`,
    `context_compactions=0`.
  - The positive reply preserved `RUN_ID`, `PATH_A`, `URL_REPORT`,
    `ERROR_CODE`, and `NEXT_ACTION` exactly in the final answer.

Follow-up fix during closure:

- Root cause for the two `cmd/aura` failures was `conversation.Context.EnforceLimit`
  counting the stable generated system prompt as mutable history, which triggered
  hidden post-turn summarization through the primary LLM client. The fix adds a
  separate non-system history token estimate for between-turn retention.
- Verification after the fix:
  `go test ./cmd/aura -run "Test(WebChatStreamUsesLLMStreamAndUIMessageFrames|HubBackedWebChatTerminatesOnTextResponseTool)$" -count=1`,
  `go test ./internal/conversation -count=1`, and
  `go test ./cmd/aura ./internal/agent/... ./internal/conversation ./cmd/probe_chat -count=1`.
- Full local gate:
  `go test ./... -count=1`.
- Post-rebuild live API probe:
  `go run ./cmd/probe_chat -smoke tools-swarm -json -timeout 420` with the
  local QA bearer token from `.planning/qa/token.txt`. Result: both cases pass.
  The no-overdelegation case stayed at `tool_calls=0`, `llm_calls=1`,
  `swarm_rows=0`, `context_compactions=0`, `context_max_tokens_in=8,033`.
  The orchestrator case used `tool_attempts.agent_note=1` and
  `tool_attempts.delegate_summarizer=1`, with `swarm_summarizer_rows=1`,
  `child_completed_tasks=1`, `child_failed_tasks=0`,
  `context_compactions=0`, and `context_max_tokens_in=37,274`.

Residual:

- `list_swarm_tasks` and `read_swarm_result` are registered only when included
  in `AURA_TOOL_ALLOWLIST`; Docker logs show they are still skipped by the
  current default allowlist. Dynamic `delegate_summarizer` works regardless for
  the active-turn orchestrator path.
