# Scheduled delivery, with a model in the loop — 2026-11-11

This is the dry run in Task 16 of `docs/superpowers/plans/2026-11-05-scheduled-delivery-defaults-and-messages.md`.
It runs scenarios 1, 3, 7 and 8, before the operator's run on the lab VM.

The user asked on 2026-11-11 to do steps 1 and 2 of Task 16 now.

The tree is `fa7adbdf4`, which is `master` after #244 was merged. This is not the lab VM. It is a
cloud container with Docker 29.9.2.

The method is the `aura-model-in-the-loop` skill:
- `AURA_LLM_PROVIDER=llamacpp` points at an endpoint that parks every completion;
- the reviewer answers each completion as the model, reading the real prompt, manifest and tool
  results that Aura sent;
- the turns were driven through the cockpit's HTTP API (`cockpit.py`, added to the skill by this
  run), not through `aura shell`.

**The stack:**
- **Aura:** `aura serve` built from `fa7adbdf4`, with `AURA_PROFILE=single_user_hardened` and
  `AURA_TIMEZONE=Europe/Rome`. It ran on a disposable Postgres database `aura_e2e`, with
  ArcadeDB 26.11.1 and the local EmbeddingGemma embedder.
- **Profile:** default channel `telegram`, timezone `Europe/Rome`, set through `PUT /api/profile`.
- **Telegram:** one linked account, written straight into `aura.telegram_accounts`
  (`10000000000`, `@operator_example`). The bot token is fake, so the channel does not start.
- **Mail:**
  - the PIM is `ghcr.io/chetto1983/aura-pim-mcp@sha256:18aa1036…edfd6`, started by `pim_up.sh` and
    mounted as `calendar` by the daemon;
  - the mailbox is `greenmail/standalone:2.1.5`, with accounts `operator@` and `colleague@aura.test`.

## What this run does not prove

- **No real model.** It shows what Aura sends and what it does with each reply. It says nothing
  about whether a given model makes those replies. In scenario 3 the model used the account id
  from the PIM's test configuration without listing the accounts first.
- **No Telegram.** The bot token is fake. Nothing was delivered on Telegram, and no Telegram card
  was shown, so scenarios 4 and 7 were not run from Telegram. Every notice on the `telegram` route
  stayed `undelivered (bound-retry)`, which is the correct handling of a channel that is down.
- **Not every scenario.** 2, 4, 5 and 6 belong to the operator's run.
- **Not the lab VM,** and not the operator's own accounts.

## Measured

| # | Call → verdict | Ledger and rows |
|---|---|---|
| 1 | Prompt: `<operator_profile>` holds "Default channel for reminders and scheduled results: telegram". `task schedule kind=reminder`, no `notify`, and no Telegram account linked → refused: "notify=telegram cannot deliver: no Telegram account is linked to this identity. No task was created". | `tool_invocations c1s1b task error`; no row. |
| 1 | The same call after the link → "Delivers via telegram to @operator_example (10000000000) (your default channel)." Nothing was asked. | `scheduler_tasks` reminder `notify_route=telegram`; at 21:40 the run `completed` "Chiamare il fornitore", and the notice stayed undelivered (Telegram is down). |
| 7a | `task schedule kind=agent_job` at +3 min → the turn ends `RUN_FINISHED outcome=interrupt` with "Approve scheduled agent_job task 01a1cc4e (once), first run 11/11 21:34 (Europe/Rome)? It runs: Controlla lo spazio disco del server con df -h e scrivi un riepilogo. It will not run until you approve." The model made no `ask_user` call. | Task `pending_approval`, `origin_tool_call_id=c7a2`; one pause in `/api/approvals`. |
| 7a | `POST /api/approvals/<token>/resolve accept` → `continue`. On resume the model read "Operator APPROVED … It is now ACTIVE" and confirmed. | Task `active`; at 21:34 exactly one run, `completed`. |
| 7b | The same path, for 12/11 09:00, declined → `continue`. On resume the model read "Operator REJECTED … cancelled and will not fire". | Task `cancelled` straight away. The finished one-shot cleanup later deleted the row (see F3). |
| 8 | Same path at +2 min, left unanswered. | After 21:39 the task is `cancelled`. Its one run is `failed`, with summary "agent_job for 11/11 21:39 (Europe/Rome) was not approved in time and did not run.". The pause was resumed `decline` with "…was not approved in time; it has been cancelled and did not run." `/api/approvals` is empty. The notice went to the `telegram` route and stayed undelivered. |
| 3 | `task schedule kind=message channel=email to=colleague@aura.test` at +4 min → `interrupt` "Review this scheduled message before it is sent". | One card in `/api/message-drafts`, `pending`, holding the recipient, subject, text and `send_at` 20:48:51Z. Task `message pending_approval`. |
| 3 | `POST /api/message-drafts/<id>/resolve send` → `approved`, `continue`. The model read "scheduled message approved: it will be sent at 2026-11-11T20:48:51Z". | Draft `approved`, task `active`. At 20:48:51 the draft is `sent`, and the run is `completed` "Message sent to colleague@aura.test at 21:48.". The `colleague@` mailbox holds exactly one mail, "Riunione", sent by `operator@`. |

## Found and fixed in this run

- **F1, fixed in `3f7330fad`. The approval reminder never worked.** `sweepApprovalReminders` called
  the pause ensurer without the task's identity. `paused_states` and the conversations fail closed,
  so the ensure saw no pause and tried to mint one, which failed: "allocate resume turn seq
  <conv>: conversation not found". For the message task the error was "no review pause", even
  though its card was pending. Every pending approval logged this WARN at its first reminder.
  - **The fix:** the sweep now runs each row as its owner, as the notification and lapsed sweeps
    already did.
  - **Proof:** a unit test, plus the same scenario re-run live on the fixed binary. The stamp
    landed at 20:55:27 with no error, and the conversation still holds one pause, the one from the
    turn.
- **F0, fixed in the skill.** `aura db migrate` on `aura_e2e` failed its last step, because the
  bootstrap DSN pointed at database `aura`. `env.e2e.sh` now sets `AURA_DB_BOOTSTRAP_URL`.

## Findings for the operator's run (not code defects)

- **F2. The rubric's "invocation count" has no row to count.**
  - Calls that the host parks are not in `aura.tool_invocations`: the four `task` calls with an
    approval (`c7a2`, `c7b2`, `c8a2`, `c3a2`), and the handler's send at fire time, which runs
    with no model.
  - The evidence that a scheduled message was sent exactly once is therefore:
    - the draft status (`sent`);
    - the run summary;
    - the recipient's own mailbox or chat: count the messages there.
  - The rubric in Task 16 must read it that way.
- **F3. A cancelled one-shot is deleted** once nothing is owed for it. This is the cleanup decided
  on 2026-10-05. "The declined one is `cancelled`" (scenario 7) can be read only before that
  sweep. After it, the evidence is the pause resumed `decline` and the absence of any run.
- **F4. A default of `telegram` with no linked account refuses the schedule** and asks for a link
  to be made in Settings. On the lab VM, scenario 1 needs a linked account.

## Gates (step 1), on this container, not WSL

- `make quality` on `fa7adbdf4`: **passed**, covering deadcode, vet, build, file-size, the
  contracts, lint, test-race and vuln.
- `bash scripts/coverage_docker.sh` on `f1d3a6af0`, against a disposable `aura_cov`: **passed**.
  - Owned surface: 53,852/61,231 = **87.9%**, against a floor of 85%.
  - The package-local coverage policy passed.
- Mutation of `internal/messagedrafts/store_scheduled.go` under `db_integration`, against a
  disposable database: **22/29 = 75.86%** killed, against a target of 70%.
  - The 7 survivors are database-error returns, which only fault injection would reach.
