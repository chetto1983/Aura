# Audit — cockpit Settings: turn budget missing, restart still required

Date: 2026-08-30 (measured on the running deployment, image `9d6b3fef42ce` built
11:17:02 +0200 from HEAD `6db93d459`, container `aura` started
`2026-08-30T09:17:19Z`, `RestartCount=0`; the operator was editing Settings live
while this audit read them — two of the reads below raced with those writes and
are reported as such).

Trigger: the operator's two observations on the "Budget token" panel —
(1) the number of turns is not there, (2) the panel still says "Riavvia Aura".

References read (`D:/tmp`, read-only): `pi` (TypeScript, pi-mono), `hermes-agent`
(Python, Nous), `LibreChat` (Node monorepo). Their findings are cited by
`path:line`; nothing from them was executed.

This file is a findings document. It does NOT add rows to the release register
in [README.md](README.md) and does not touch `scripts/audit_closure_gate.py`;
promoting a finding to a register row is the operator's call (two-part change).

## Verdict in one paragraph

Aura already has the right hot-reload skeleton (Amendments #184–#186:
prepare → persist → publish one immutable `llm.RuntimeSnapshot`, consumed per
run). It is applied to exactly six keys. Everything else the cockpit lets the
operator edit is still boot-bound, the API reports that as ONE boolean the UI
cannot attribute to a field, and the agent-loop turn budget
(`AURA_LOOP_MAX_STEPS`) is not editable from the cockpit at all even though it
is the knob the PRD itself measured as too small (25 → 60, raised via a compose
override = a container recreate, the very operation #184 forbids). On top of
that, the running deployment budgets a **1,000,000-token window on a
262,144-token model** because `compose.yaml` always injects
`AURA_MODEL_CONTEXT_WINDOW`, which marks the window "explicitly configured" and
silently disables the model-metadata discovery #185 was built for.

## Findings (ranked)

### F1 — HIGH — the measured model profile is dead in the shipped compose deployment

**Measured.** Authenticated `GET /api/me` on the live stack at 09:27Z:
`{"context_window":1000000}` while the active route was
`ollama / gemma4:31b-cloud` (measured `context_length=262144`, Amendment #186;
the llama.cpp route is `n_ctx=81920`, Amendment #185). The Settings panel shows
"Token finestra contesto 1000000 — Configurato".

**Two causes, both structural, not just a stale row:**

1. `aura.settings` holds `AURA_MODEL_CONTEXT_WINDOW=1000000`, written
   2026-08-16 (before #185 existed). An explicit override wins over discovered
   metadata by design ([pricing_source.go:287-289](../../internal/llm/pricing_source.go#L287-L289)).
2. Even with that row deleted, discovery cannot apply: [compose.yaml:136](../../compose.yaml#L136)
   injects `AURA_MODEL_CONTEXT_WINDOW: ${AURA_MODEL_CONTEXT_WINDOW:-1000000}`
   unconditionally, `config.go` marks the window configured whenever the env var
   is SET ([config.go:477-481](../../internal/llm/config.go#L477-L481)), and the
   DELETE fallback copies that flag from the pre-overlay boot config
   ([serve_settings.go:88-89](../../cmd/aura/serve_settings.go#L88-L89)).
   `docker exec aura env` confirms the container env carries
   `AURA_MODEL_CONTEXT_WINDOW=1000000`. The "absent overrides use the provider
   value" branch of #185 is unreachable in any compose-launched Aura. The same
   applies to `AURA_MODEL_MAX_OUTPUT_TOKENS` ([compose.yaml:137](../../compose.yaml#L137)).

**Consequence.** The context ladder's hard cap is computed from 1M
([context_budget.go:91-98](../../internal/conversations/context_budget.go#L91-L98)),
so on the local 81,920 window nothing compacts before llama.cpp overflows; the
60 % early trigger (= 600,000 tokens) never fires; the footer gauge shows a 1M
window; `/api/me` misreports. The cockpit gives no hint: the field says
"Configurato" and never shows the provider-measured value beside it.

**Not proven.** No overflow error appeared in this boot's logs (the grep was
clean), so the failure is predicted from the arithmetic, not observed.

### F2 — HIGH — the turn budget is not a setting

`AURA_LOOP_MAX_STEPS` (default 25) and `AURA_LOOP_MAX_WALLCLOCK_SEC` (300) are
defined only in [budget.go:29-44](../../internal/agent/budget.go#L29-L44), read
from process env on every run ([runner.go:362](../../internal/runner/runner.go#L362),
[delegation_run.go:177](../../internal/swarm/delegation_run.go#L177),
[agentjob.go:151](../../internal/cron/handlers/agentjob.go#L151)). They are
absent from `settings.AllowedKeys`
([settings.go:48-69](../../internal/settings/settings.go#L48-L69)), from the
cockpit `TOKEN_SETTINGS`
([modelSettingsDefs.ts:72-100](../../web/src/settings/modelSettingsDefs.ts#L72-L100)),
and from the knob catalog `config_knobs.go`. The only operator paths are
`.env` + container recreate, or the `aura agent --max-steps` CLI flag
([agent.go:75](../../cmd/aura/agent.go#L75)).

**Measured need already in the PRD.** [prd.md:9411](../../prd.md#L9411): a real
delegation needed `AURA_LOOP_MAX_STEPS=60` / wallclock 1200 and got it through a
temporary `compose.d03.yaml` override — i.e. a recreate. Compose did not even
map the var until 2026-08-29 (Amendment #171, [prd.md:6058](../../prd.md#L6058)).

**Why it is cheap to fix.** The budget is built per run from
`BudgetOptions` (CLI > env > default, D-06). Sourcing `MaxSteps` /
`MaxWallclockSec` from the runtime snapshot instead of `os.Getenv` keeps every
existing guarantee (shared atomic counter, child inherits remaining, per-job
`StepBudget` override in agentjob D-24) and makes the knob hot for free: new
runs take the new value, in-flight runs keep theirs — exactly the #184 contract.

**References.**
- LibreChat: per-agent `recursion_limit`, a plain number input in the Agent
  Builder ("Max Agent Steps", `client/src/components/SidePanel/Agents/Advanced/MaxAgentSteps.tsx:22`),
  stored in Mongo, re-read per request (`packages/api/src/agents/load.ts:181`),
  cascade YAML default → per-agent → YAML cap (`packages/api/src/agents/config.ts:11-30`,
  default 50; its UI copy still says 25 — doc drift).
- Hermes: `agent.max_turns` (default 500, `hermes_cli/config_defaults.py:31-32`),
  exposed in the desktop settings UI (`apps/desktop/src/app/settings/constants.ts:769`),
  config.yaml re-read per turn through an mtime-keyed cache and poked onto the
  cached agent (`gateway/run.py:4754-4756`: "cached agent may have been created
  with old config"). Precedence CLI > config > env > default (`cli.py:4436-4455`),
  with a documented incident where a stale `.env` shadowed config
  (`gateway/run.py:2164-2169`) — the same class of bug as Aura's Amendment #171.
- pi: no turn limit at all (`packages/agent/src/agent-loop.ts:170` is
  `while (true)`); the loop stops on no-tool-calls / abort / tool `terminate`.
  Not a model for Aura (DoS control ASVS V11 is a stated requirement).

### F3 — MEDIUM — `restart_required` is one boolean the UI cannot attribute

[settings_api.go:157-161](../../internal/agui/settings_api.go#L157-L161) sets a
single `RestartRequired` on the list DTO whenever ANY overridden non-hot row
differs from process env. The banner
([ModelSettingsPanel.tsx:178-185](../../web/src/settings/ModelSettingsPanel.tsx#L178-L185))
renders under whichever group the operator is looking at and blames "i client
modello già creati".

**Measured.** Live `GET /api/settings` at 09:27Z: `restart_required:true`. The
only row that explains it is `AURA_VISION_CLOUD=true` written 09:26:08Z (after
boot; process env is `false`). The banner in the operator's screenshot sat under
"Budget token" — a panel whose three of four fields are hot — for a vision
toggle. Nothing tells the operator which field is pending.

**Reference.** LibreChat and Hermes both mark restart-bound settings at the
field, in the field's own copy ("Restart Hermes for tracing to take effect",
`hermes_cli/tools_config.py:1939`; "Enable it, then restart the gateway",
`hermes_cli/web_server.py:9465`). pi does the same for project trust
(`interactive-mode.ts:3503`).

### F4 — MEDIUM — cold keys inventory and the real cost of each

| Key | Where it is consumed | Hot today | Cost to make hot | Honest restart? |
|---|---|---|---|---|
| `AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT` | already in `llm.Config`, read per turn from the snapshot ([runner_context.go:40](../../internal/runner/runner_context.go#L40)) | **no** (not in `hotLLMProfileKeys`) — sits in the same "Budget token" panel as three hot keys, rendered identically | ~20 LOC: add to `hotLLMProfileKeys`, `resolve()`, `EffectiveValue` | no |
| `AURA_LOOP_MAX_STEPS`, `AURA_LOOP_MAX_WALLCLOCK_SEC` | per-run `NewBudget` from env | not a setting (F2) | allowlist + DTO + `BudgetOptions` from snapshot | no |
| `OPENROUTER_API_KEY` | copied into the snapshot config at boot; `resolve()` keeps the active key ([serve_settings.go:78-82](../../cmd/aura/serve_settings.go#L78-L82)) | **no** — a rotated key is persisted but the runtime keeps the boot key | include in profile prepare (validates via the metadata fetch), never echo | no |
| `AURA_EMBED_BASE_URL/MODEL/DIMENSIONS` | embedding client built at boot ([embedding_client.go](../../cmd/aura/embedding_client.go)); the `arcadedb-mcp` sidecar reads the same env at ITS boot ([main.go:66-67](../../cmd/arcadedb-mcp/main.go#L66-L67)) | no | needs a runtime holder in Aura AND a sidecar contract; a dimension change re-indexes | yes for dimensions; URL/model could be hot in Aura only |
| `AURA_STT_CLOUD_MODEL`, `AURA_TTS_MODEL`, `AURA_VISION_CLOUD` | boot wiring ([serve_voice.go](../../cmd/aura/serve_voice.go)) | no | Hermes-style cache-busting signature on the sidecar clients | no |
| `TELEGRAM_BOT_TOKEN` | bot constructed at boot ([serve_channels.go:59](../../cmd/aura/serve_channels.go#L59)); the UI already says so per field | no | restart the bot goroutine, not the daemon | yes (acceptable) |

### F5 — LOW — a `max_steps` trip is invisible in the cockpit

The terminal Event carries `termination_reason/limit_hit/steps_consumed`
([loop.go:298-302](../../internal/agent/workflow/loop.go#L298-L302),
[llm_agent_finalize.go:264-270](../../internal/agent/llm_agent_finalize.go#L264-L270))
and the translator puts it on the wire as `STATE_DELTA`
([translator.go:197-203](../../internal/agui/translator.go#L197-L203)). The SPA
reads only usage keys from `STATE_DELTA`
([sseAdapter_usage.ts:10-27](../../web/src/chat/sseAdapter_usage.ts#L10-L27));
`limit_hit` / `steps_consumed` appear nowhere in `web/src`. The operator sees a
synthesized "finalize" answer and cannot tell it was cut at 25.

**Reference.** Hermes emits "⚠️ Iteration budget exhausted (n/max) — asking
model to summarise" and tells the user "Send `continue` to keep going, or raise
`max_iterations`" (`agent/turn_finalizer.py:94-142`, `run_agent.py:3629-3635`).
LibreChat appends an ERROR content part with the raw LangGraph message
(`api/server/controllers/agents/client.js:1686-1701`). Both surface it; Aura hides it.

### F6 — LOW — two output caps with overlapping meaning, one of them likely mistyped

`AURA_LLM_MAX_TOKENS` ("Token massimi risposta", the per-request `max_tokens`)
and `AURA_MODEL_MAX_OUTPUT_TOKENS` ("Token output riservati", the budget
reservation) are shown side by side with no explanation of how they relate
(`OutputReserve = max(MaxOutputTokens, scaled floor)`,
[config.go:127-129](../../internal/llm/config.go#L127-L129)). Live value
`AURA_LLM_MAX_TOKENS=8092` (written 08:37Z today) reads like a typo for 8192.
pi has one number (`model.maxTokens`, clamped to the window,
`packages/ai/src/api/simple-options.ts:29`); Hermes has one (`model.max_tokens`)
and subtracts it from the window for the compression threshold
(`agent/context_compressor.py:2210-2250`). Observation, not a defect.

### F7 — LOW, unverified — the boot fallback model looks mistyped

`docker exec aura env` shows `AURA_LLM_MODEL=~deepseek/deepseek-v4-flash-latest`
(leading `~`). This is the value a DELETE of the model row falls back to
([serve_settings.go:85](../../cmd/aura/serve_settings.go#L85)); `Prepare` would
then resolve that id against OpenRouter `/models` and refuse the DELETE with a
400. Read from the container env only; `.env` was not opened for this audit.

## Non-findings (checked, fine)

- The image is fresh (built after HEAD) and the six hot keys DO publish
  without restart: daemon log shows six `primary LLM profile updated` lines
  between 09:21Z and 09:25Z with `RestartCount=0`, and `GET /api/settings`
  reports them `overridden` with matching runtime values. The apparent
  DB-vs-runtime divergence seen mid-audit (DB `llamacpp` at 09:24:58Z, runtime
  `ollama` at 09:25:03Z) was this audit racing the operator's next click; the
  DB read five seconds later matched.
- `settings.Store.UpsertMany` + advisory lock + prepare-before-persist is the
  right shape and matches Hermes' atomic-write + LibreChat's
  `invalidateConfigCaches` after every admin write. Keep it.

## Recommended design (follows the references, reuses the existing seam)

1. **One runtime profile, not two mechanisms.** Extend the immutable snapshot
   with a loop profile (`MaxSteps`, `MaxWallclockSec`) and the compaction
   trigger; `NewBudget` takes `BudgetOptions` filled from the snapshot the run
   already holds. Per-job `StepBudget` (agentjob D-24) and the CLI flag keep
   precedence. This is Hermes' "poke `max_iterations` onto the cached agent"
   done Aura's way (immutable snapshot, in-flight runs unaffected).
2. **Add `AURA_LOOP_MAX_STEPS` and `AURA_LOOP_MAX_WALLCLOCK_SEC` to
   `settings.AllowedKeys` (KindInt, ≥1)**, to `hotLLMProfileKeys`, to the
   cockpit "Budget token" group (or a new "Limiti del turno" group), and to the
   env catalog row in the PRD. Validation: `>= 1`; wallclock in seconds.
3. **Per-field application state instead of one boolean.** Replace
   `restart_required` on the list with `applied: "hot" | "pending_restart" |
   "boot"` per `settingItemDTO`; the UI badges the field and the banner names
   the keys. Keep the boolean for one release for the existing tests, then drop it.
4. **Make discovery reachable (F1).** Drop the compose default for
   `AURA_MODEL_CONTEXT_WINDOW` / `AURA_MODEL_MAX_OUTPUT_TOKENS` so an unset var
   leaves `*Configured=false`; show the provider-measured value next to the
   override in the cockpit ("misurato: 262 144 · override: 1 000 000"); refuse
   or at least warn on override > measured. Then delete the stale 2026-08-16 row
   through the API (not the DB) and re-measure `/api/me`.
5. **Surface the trip (F5).** Map `limit_hit` / `steps_consumed` from
   `STATE_DELTA` to a visible system note with a "continua" affordance — the
   Hermes pattern. Telegram gets the same line.
6. **Keep the honest restarts honest.** Telegram token and embed dimensions
   stay restart-bound and say so in the field, not in a global banner.

Per CLAUDE.md the order is: this measurement → PRD amendment (#187) recording
F1/F2/F3 and the design above, with "what it does not prove" → implementation
as a GSD phase (it is not a `gsd-quick`: it touches `llm`, `agent`, `runner`,
`swarm`, `cron`, `agui`, `settings`, `web`, `compose.yaml`).

## What this audit does not prove

- It did not generate at any window limit and did not observe a provider
  overflow; F1's consequence is arithmetic on the measured numbers.
- Reference behaviour was read, not executed; version drift inside `D:/tmp`
  is possible (LibreChat's own UI copy disagrees with its code default).
- It did not measure the cost of hot-swapping the embedding or voice sidecar
  clients; F4's "cost" column is a code-reading estimate.
- `.env` was not opened (permission withheld); F7 rests on the container env.

## Live E2E after implementation (2026-08-30, image built from master `82dc63ec8`)

Driven through the authenticated cockpit API from a throwaway `curlimages/curl`
container on `aura_default` (login → `PUT /api/settings/llm-profile` →
`POST /agent/run`, SSE captured to file). Amendments #188 (F2/F3/F5/F6) and #189
(reasoning overrun, reported mid-task); F1 landed separately as #187.

| Check | Result |
|---|---|
| Hot turn budget without restart (F2) | `PUT llm-profile {AURA_LOOP_MAX_STEPS:"2"}` → 200 `restart_required:false`; `GET /api/settings` shows `value:"2", overridden:true, applied:"live"`; the next run made 2 `shell_exec` calls then stopped: `STATE_DELTA /limit_hit=max_steps`, `/termination_reason=budget_exhausted`, non-empty final answer. Daemon `StartedAt` unchanged across the whole drive (`10:54:08Z`, later `11:05:53Z` after the deliberate rebuild), `RestartCount=0`. |
| Per-field state (F3) | `applied:"live"` on every hot profile key, `applied:"boot"` on `AURA_VISION_CLOUD` (overridden but equal to the process env), `restart_keys:[]`. |
| Trip visible (F5) | The first drive showed `/limit_hit` **without** `/steps_consumed`: only the workflow `LoopAgent` emitted the count, the production `LlmAgent` path did not (the cockpit notice would have read "0 steps"). Fixed on touch (`77090d8f6`), rebuilt, re-driven: `/steps_consumed=2` on the wire. |
| Reasoning overrun, loop layer (#189) | Local llama.cpp `gemma-4-12b`, `AURA_LLM_MAX_TOKENS=1500`, effort `max` (unlimited budget, not fitted): first call `finish_reason=length`, no content → `WARN agent reasoning overrun … retrying without reasoning` → second call `finish_reason=stop`, 6,238-char answer, `completion_tokens=2260`. |
| Reasoning overrun, wire layer (#189) | Same route and cap, effort `high` (8,192 budget → `max_tokens` fitted to 9,692): one call, `finish_reason=stop`, 3,140 reasoning deltas then a complete 13,721-char strategy, `completion_tokens=4687` — above the 1,500 operator cap, which is the proof the fit was on the wire; no recovery, no truncation notice. |
| Restore | Profile rows put back (Ollama cloud route, `8092`), `DELETE /api/settings/AURA_LOOP_MAX_STEPS` → `deleted:true`, list shows `25, overridden:false, applied:"live"`. |

Observed on the second trip run and fixed as **amendment #191** (`4343507e5`):
the model answered in prose at step 2 ("Il budget … è esaurito …"), the
completion gate vetoed it, the third call tripped `max_steps`, and the
synthesized answer was appended to the SAME text message — the vetoed round's
deltas were never repudiated on the wire (B-12 did that only for stream errors).
Re-driven on the rebuilt image: `TEXT msg-be733168…` (draft) → `CUSTOM
aura.discard{message_id: msg-be733168…}` → `TEXT msg-46b14f05…` (final answer)
→ `STATE_DELTA limit_hit/steps_consumed=2`.

## Live E2E, amendment #192 (2026-08-30, image built from master `ea1ab71fe`)

Trigger measured in the DB first (Telegram conversation, 30 days): 11
`write_file` deliverables under `/workspace/artifacts/`, 9 sent in the same
turn, 2 only after the operator asked. Drive: Ollama cloud `gemma4:31b-cloud`,
effort `off`, thread `01a05302-2fb3-7059-b4e9-98ffe7f164f1`, prompt "crea con
write_file `/workspace/artifacts/gate192.html` … rispondimi indicando solo il
percorso" — the exact shape of the two misses.

| Seam | Wire / ledger |
|---|---|
| Write | `write_file` 14:10:48 → `ok, wrote 175 bytes … verified:true`. |
| Path-only stop | The model terminated with `text_response` (call `3j8ubf0u`); the gate answered it with a `TOOL_CALL_RESULT` whose preview is `delivery gate: artifact not sent` — one deterministic round, no critic call. |
| Delivery | Next call: `send_file` 14:10:51 → `queued gate192.html for delivery`, `CUSTOM aura.artifact`, `aura.assets` row `gate192.html accepted 175 B` — same turn, same request. |
| After delivery | The model streamed "Dato che non hai fornito un nuovo comando, resto in attesa…"; the completion critic vetoed it → `CUSTOM aura.discard` (#191 shape on the wire) → final `TEXT` `/workspace/artifacts/gate192.html` → `RUN_FINISHED`. |

Not proven: files written by a shell command (no `write_file` row → no edited
path; the prompt half is the only guard), and the Telegram rendering of the
delivered artifact (no bot session for this driver; `send_file` → `sendDocument`
was already exercised by the 9 accepted assets in the ledger).

Not driven: the Telegram pane (no bot session available to this driver; its
`limit_hit`/`steps_consumed` rendering is unit-tested only), the browser
rendering of `BudgetLimitNotice` and the per-field badges (unit-tested; the
STATE_DELTA and `applied` fields they read were verified on the wire above).

Write-path gotcha for the next driver: `PUT/DELETE /api/settings*` require an
`Idempotency-Key` header like `/api/conversations` and `/agent/run` (400 without).
