# Scheduled delivery defaults and reviewed scheduled messages: implementation plan


**Status:** written 2026-11-05. The operator confirmed the three open choices the same day ("confermo"): an unapproved message task reaches its time only to expire and report "not approved in time" (decision 8); an approved message task can only be cancelled (decision 14); the Telegram review fix covers immediate drafts too (decision 13). Extended the same day to the spec's Part 3: every approval appears in the chat that creates the task (Task 13), and a one-shot that passes its time unapproved expires and is reported (Task 14). Decision 8 is now met by Task 14's expiry, with no activation. The operator confirmed the Part 3 choices the same day ("ok"): a call with nobody in the chat is stored pending and reminded by the existing sweep, not refused; an expired one-shot is deleted by the existing cleanup after its notice is delivered; the risk tier leaves the approval question. Execution is deferred until the parallel session working on the settings/agui files has finished; validate the plan with three reviewers before starting. Recommended execution: subagent-driven. Landed ahead of the plan the same day, at the operator's request ("mettilo già adesso"): the task description's "ask before scheduling" paragraph and the refusal of a reminder without payload.text or an agent_job without payload.goal (`internal/agent/tools/task_payload.go`). Task 2's tool-text rewrite keeps that paragraph and changes only its "where (the channel)" clause to name the default.

**Reviewed 2026-11-11** by three reviewers (grounding against master `bdb6f0416`, correctness and safety, executability against the repo's gates), and corrected the same day: decisions 28-41, Tasks 5a/5b (Task 5 split), Task 15 (new: CI mutation scopes) and Task 16 (the live run, formerly Task 15). The operator confirmed the review's three choices ("Confermo"): deactivating an identity cancels a pending or approved message task and declines its draft (decision 37); the host review respects the identity's gateway policy, so a denied `task` is never parked and refuses as today (decision 31); the approval question ends `It will not run until you approve.` in Go and on the web (decision 40). Line references were re-measured on `bdb6f0416`.
> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans, task by task, on `master`. Steps use checkbox syntax.

**Goal:** `task` uses a default channel that the operator sets in the cockpit's Profile panel, and it names that channel in every reply that used it. A new one-shot task kind, `message`, sends one fixed message to someone else at a set time. The operator reviews the recipient and the text when it is scheduled, in the cockpit or on Telegram. At fire time it goes out once, with no model in between. A job that tries to write to someone else is reported as "not sent". Every task that needs approval asks for it in the chat that creates it, in that turn, naming what will run. A one-shot nobody approved in time expires and the operator is told.

**Architecture:** Part 1 adds `default_notify` to `aura.identity_profiles`. `task` reads it through a `Defaults` seam that sits beside `Timezones`. The delivery logic moves from `actionSchedule` into `task_delivery.go`. Part 2 makes the `task` call with `kind=message` a host-reviewed call. The agent parks it before it runs, the same way it parks a trusted send today (`withholdOutboundMessage`). The tool persists a `pending_approval` task and a message draft bound to it, and returns the review pause, keyed to the model's own call id. The draft row has the only authority to send:
- the review moves it to `approved`, then activates the task;
- a handler with no model claims the approved draft (one-send CAS) and dispatches it through `agent.ExecuteReviewedMessage`, so the gateway's claim binding stays as it is;
- an unapproved draft expires with its task (Part 3), and nothing is sent.

Part 3 widens the host review to every `task` schedule call that will need approval. One pure rule, `taskNeedsApproval`, decides it from the arguments, both before the call and inside it. The pause is a `scheduled_task_approval` keyed to the model's call id. Its question names the kind, the time in the operator's zone and the goal or text. The model-relayed directive retires. On each tick, the scheduler's new lapsed-approval pass handles any one-shot still `pending_approval` past its time, of any kind, `message` included. It closes the task's pending pause, cancels the task (a CAS, so a concurrent approval wins), opens and closes a run carrying the notice, and delivers it through the dispatcher's own notify and origin record.

Everything new runs in the same turn and needs a person in the chat. Headless and delegated schedule calls still persist `pending_approval`, and the existing sweep stays their backstop.

**Tech Stack:** Go, Postgres (two migrations + sqlc), the existing message-draft review (`internal/messagedrafts`, `internal/runner`), cron dispatch, telebot (Telegram), React + i18next (cockpit).

**Spec:** `docs/superpowers/specs/2026-11-05-scheduled-delivery-defaults-and-messages-design.md` (approved 2026-11-05, `fa0e2a8e5`).

## Global Constraints

- `default_notify` is `''` or one of `none`, `stdout`, `telegram`, `whatsapp`, `email` (`cron.ValidNotifyRoute`). `''` means "not set". An explicit `notify` always wins.
- A reply that used the default says so: `Delivers via <route> to <recipient> (your default channel).`, or `Delivers via <route> (your default channel).` for a route that addresses no one.
- `kind=message` is one-shot only (`schedule_kind=at`). It fires at most `24h − 15m` ahead. The draft lives until the fire time plus `ScheduledMessageGrace = 15 * time.Minute`, inside `Store.Create`'s 24 h bound (`internal/messagedrafts/store.go:104`).
- Only an approved draft is ever dispatched, and at most once (the `approved → dispatching` CAS). No answer means the message is not sent.
- No regex and no text analysis of an `agent_job` goal (spec, anti-goals).
- A task needs approval when `kind == "agent_job"` (AG-016) or `scoring.GateRecommended(scoring.ComputeTaskTier(…))`. That rule is `taskNeedsApproval`, and nothing else decides it.
- The approval question, verbatim: `Approve scheduled <kind> task <first 8 of id> (<schedule>), first run <when>? It runs: <summary>. It will not run until you approve.`
  - `<schedule>` is `once`, `every <n>m`, or `cron "<expr>"`.
  - `<when>` is the first fire in the operator's zone, as `02/01 15:04 (<zone>)`.
  - `<summary>` is the goal (agent_job), the text (reminder) or the kind, on one line, cut at 160 runes with `…`.
- The lapsed-approval notice, verbatim: `<kind> for <02/01 15:04 (<zone>)> was not approved in time and did not run.` Its time is the task's `next_run_at` in the task's `tz`.
- Migration numbers: `ls internal/db/migrations/ | tail -1` at landing. Today the last is `0153`, so Part 1 is `0154` and Part 2 is `0155`. Re-check both when each one lands.
- No file over 600 lines. Measured on `bdb6f0416` (2026-11-11): `task.go` 572, `serve_adapters.go` 555, `chat_boot.go` 578, `agui/server.go` 590, `dispatch.go` 571, `cmd/aura/task.go` 562, `resources.settings.ts` 576. A peer session has uncommitted edits in `internal/agui/server.go` and `web/src/i18n/resources.ts` (2026-11-05), so measure again when a task touches them. If the canceller field would push `server.go` past 600 lines, move an existing group of setters out of it into its own file, in the same commit.
- Commands:
  - Go, in WSL: `wsl -e bash -lc 'cd /mnt/d/Aura && export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH && go vet ./... && go build ./... && go test -race ./internal/<pkg>/...'`.
  - `db_integration`: never against the live `aura` database. Bring up the disposable `aura_cov` database the way `scripts/coverage_docker.sh` does (`disposable_stack_bring_up_auto` then `disposable_stack_export_env`, `scripts/coverage_docker.sh:11-22`), which exports `AURA_DB_URL` and `AURA_DB_MIGRATE_URL` for it, then run `go test -race -tags db_integration -count=1 ./internal/<pkg>/`. `scripts/coverage_gate.sh` refuses the live database for the same reason.
  - sqlc: `make sqlc` in WSL.
  - Web, on Windows: `cd web; npx vitest run <file>; npx vitest run src/i18n/__tests__; npm run lint; npm run lint:contract; npm run format:check; npm run typecheck; npm run mutation:contract`, and `npm run test` (its coverage floor) before the task's commit. CI runs every one of these (`.github/workflows/ci.yml`, the web jobs); the i18n parity and usage tests (`web/src/i18n/__tests__/resources.parity.test.ts`, `resources.usage.test.ts`) catch a missing it/en key.
- The repository is public. Tests, fixtures and docs use `example.com` addresses and the number `10000000000`, and no real name or message text.
- One atomic commit per task, ending `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **A scheduled draft goes out too early or too late.** Two cases: the interactive "send" path claims it at once, or the operator approves it after its send time. Neither may send. Pinned by `TestClaimSendRefusesAScheduledDraft` (Task 4) and `TestApproveRefusesAfterTheSendTime` (Task 6).
2. **A message task is activated by its id alone.** That covers the approval sweep, the scheduled-task resume hook, the board's approve, `aura task approve`, and a `scheduled_task_approval` that the model relays. None may activate it, and none mints a bare Sì/No. Pinned by `TestApproveTaskRefusesAMessageTask`, `TestTheScheduledTaskHookNeverActivatesAMessageTask` and `TestTheSweepNeverMintsABareApprovalForAMessage` (Task 7).
3. **The same message task fires twice**, from a boot catch-up, a second tick, or a crash after the claim. Only one dispatch ever happens. Pinned by `TestASecondFireNeverSendsAgain` (Task 11).
4. **No answer reaches a one-shot's time,** for a message or an agent_job, including the one stuck since 2026-11-02. Nothing runs or is sent, and the operator is told on the notify route. The task leaves the awaiting-approval list, and its pause leaves the approvals list. An approval racing the expiry wins or loses whole. Pinned by `TestAnUnapprovedMessageIsReportedAndSettles`, `TestATaskStuckBeforeDeployExpiresOnTheFirstPass` and `TestALostRaceSendsNothing` (Task 14).
5. **The gate before the call disagrees with the gate inside it.** If so, a gated task would be created active, or a harmless one parked. Pinned by `TestTheGateBeforeTheCallIsTheGateInsideIt` (Task 13). The Telegram card's 4096-character limit, the next most likely, is pinned by `TestALongMessageIsCutWithAPointerToTheCockpit` (Task 10).
6. **A parked call writes rows the gateway never saw** (review 2026-11-11). A `task` the identity's policy denies must not be parked, and a call id parked twice must give one task and one draft. Pinned by `TestAPolicyDeniedTaskIsNeverParked` and `TestTheSameCallParkedTwiceCreatesOneTask` (Task 5b).
7. **An approval loses half a race** (review 2026-11-11): the answer is committed before the resume hook activates the task (`internal/runner/runner_resume.go:118-121`), and the lapsed pass can run in that gap. Pinned by `TestAnApprovalInFlightIsNotExpired` and `TestAnApprovedMessageDueNowIsActivatedNotCancelled` (Task 14).
8. **A Telegram approval fails before the gateway** because the callback carries no parent operation (decision 34). Pinned by `TestTelegramApprovaSendsUnderAStrictGateway` (Task 10).

## Decisions taken while writing this plan

1. **The pause comes from the agent's in-turn withhold, not from `MintApprovalPause`.** `MintApprovalPause` picks its own tool call id and appends a synthetic assistant turn through `CommitPause` (`internal/runner/runner_mint_approval.go:50-80`). Minted while the model's turn runs, that turn would land between the model's `task` call and its result. The approval sweep holds a grace period for exactly this reason (`internal/cron/deliver_approval.go:122-130`). The agent already parks a trusted send before it runs, with a host-authored pause keyed to the model's own call id (`internal/agent/llm_agent_message_draft.go:30-68`, `llm_agent_pause.go:78-82`). A `task` call with `kind=message` is parked the same way, through a new `tools.HostReviewer` interface. That satisfies the `draft.ToolCallID == pending.ToolCallID` join (`internal/runner/message_draft_resume.go:62`, `:187`).
2. **The pause reuses the `message_draft` resume type, with `task_id` added.** `IsReviewPause` then already hides it from the generic approvals (`internal/agui/approvals_api.go:201`) and refuses it in `SubmitAnswer` (`internal/runner/resume_policy.go:68-70`). `PauseDraftID` already finds it.
3. **The draft row, not the resume context, says the draft is scheduled.** It gets two new columns, `send_at` and `scheduled_task_id` (`ON DELETE SET NULL`, because `DeleteSettledOneShots` deletes finished tasks). "Scheduled" means `send_at IS NOT NULL`. `ClaimSend` refuses a scheduled draft.
4. **The new state is `approved`.** The review fixes `effective_args` and the fingerprint (`pending → approved`). The fire-time claim takes `approved → dispatching`.
5. **The grace period is 15 minutes.** So a message is scheduled at most 23 h 45 m ahead.
6. **Payload names:** `channel` (`whatsapp` | `email`), `recipient` (WhatsApp), `to` (a list, email), `account`, `subject` (email) and `text`. WhatsApp maps to `{"recipient", "message"}`. Email maps to `{"action":"send_email","accountId","to","subject","body","bodyFormat":"text"}`, because the PIM's default body format is HTML (`internal/messagedrafts/validation.go:191-201`).
7. **The tools package cannot call `messagedrafts`,** because `messagedrafts` imports `tools` (`internal/messagedrafts/validation.go:13`). Draft creation is a seam, `taskMessageDrafts`, implemented in `cmd/aura/serve_scheduled_message.go`. It resolves the mounted trusted tool by recipe through a new `Registry.ByTrustedRecipe`, which `selfSendResolver.Resolve` (`cmd/aura/serve_notify.go:53-64`) then uses too. The registry is a map (`spec.go:281-289`), so the helper sorts by name before it picks the first match.
8. **A message task due without approval expires with every other lapsed one-shot (Task 14), and is never released.** The first version released it to its handler. Part 3 needs the same rule for every kind, and releasing an `agent_job` would run it unapproved. So one pass handles all kinds, with no activation:
   - it closes the pause, expiring a message draft on the way;
   - it cancels the task with a CAS;
   - it opens a run and completes it `failed` with the notice;
   - it delivers the notice through `Dispatch.notify` and `recordToOrigin`.

   The reason it is needed is unchanged: `DueTasks` selects only active tasks (`internal/db/queries/scheduler_tasks.sql:50`). The operator's confirmed behaviour ("not approved in time", nothing sent) holds. `ReleaseDueMessages` is not built.
9. **The handler learns its task.** `cron.Job` and `handlers.Job` gain `TaskID` (`internal/cron/dispatch.go:42-59` has no task id today).
10. **The `agent_job` line comes from the job's own events, not from the deny fact.** The gateway writes the deny fact only on a strict profile (`internal/gateway/message_draft.go:61-63`). The handler instead reads the end `ToolInvocation` event (`Refused` true) for a tool that `messagedrafts.TrustedOutboundTarget` recognizes. `decideReviewedMessage` is the first check in `Decide` (`internal/gateway/decide.go:69-71`), so such a refusal can only be the review refusal.
11. **Five activation paths are refused, not three.** The other two are `aura task approve` (`cmd/aura/task.go:375-392`), which now calls `cron.Store.ApproveTask`, and `cronTaskStore.ApproveScheduledTask` (`cmd/aura/serve_adapters.go:283-…`), which has no production caller and is deleted.
12. **The sweep never mints for a message task.** For `kind=message` the ensurer returns the existing review pause (`minted=false`, so no push, as at `deliver_approval.go:137-142`), and it never mints a `scheduled_task_approval`. The review card itself goes out on the origin channel during the turn.
13. **Telegram resolves every message-draft pause through `ResolveMessageDraft`.** Today Telegram sends every button through `SubmitAnswer` (`internal/channels/telegram/hitl.go:152`), which refuses review pauses. So no message review could ever be resolved on Telegram, immediate ones included. The card covers both kinds. The scheduled one adds its time line.
14. **After approval, a message task can only be cancelled.** Pause, resume, run-now and edit are refused (`IsPausableKind` leaves `message` out). Run-now would send before the approved time, and the spec rules out edits after approval.
15. **Cancel declines the draft on all three cancel paths.** These are the task tool, the board and `aura task cancel`, each through `messagedrafts.Store.DeclineForTask`. RLS on `message_drafts` needs the owner's identity, so this cannot be one SQL statement in `cron.Store.CancelTask`. **The draft is declined before the task is cancelled** (CodeRabbit on #242): a handler already running loaded the task before the cancel and only claims the draft, so a cancel that lands first would leave the draft `approved` for that claim. Declining first makes the draft's status the one ordering point: a decline that wins makes every later claim fail, and a claim that won first sends its one message. If the decline succeeds and the cancel fails, a retry finds a declined draft (no-op) and cancels.
16. **The profile block's sentence renders only when the block does.** `RenderProfileBlock` returns `""` for an empty profile (`internal/onboarding/profile_block.go:63-65`), and that test stays.
17. **The fire-time summary uses the identity's zone,** through `jobLocation`, which is extracted from `AgentDeps.clockLocation` (`internal/cron/handlers/handler.go:136-…`). The Telegram card uses `Runner.Location`, which wraps `turnLocation` (`internal/runner/runner_profile.go:29`).
18. **The three new cockpit keys get their own resource file,** `resources.profileDefaultNotify.ts`, so `resources.settings.ts` (576 lines) stays under 600.

Part 3 (added 2026-11-05):

19. **One gating rule, read twice.** Today's gating is three inline statements in `actionSchedule`: the tier at `task.go:241-245`, `GateRecommended` at `:271-273`, and the AG-016 override at `:279-281`. They become `taskNeedsApproval(a taskArgs) (scoring.RiskTier, bool)`. `HostReview` calls it before the call runs and `actionSchedule` calls it inside the call. Both read only `Kind`, `ScheduleKind` and `Payload`, and `scoring.ComputeTaskTier` is pure, so the two cannot disagree. A table test pins it.
20. **The in-turn approval pause keeps the `scheduled_task_approval` resume type.** Everything downstream already keys on it:
    - the resume hook that activates or cancels (`cmd/aura/serve_scheduled_approval.go:99-115`);
    - the answer the model reads after a decision (`internal/runner/runner_resume.go:253-274`);
    - the sweep's de-duplication, which finds the in-turn pause and does not push (`serve_scheduled_approval.go:152-162`);
    - the presentation (`internal/approvaltext/presentation.go:98`).

    The runner stamps the decision policy and the presentation on every agent pause it persists (`internal/runner/runner_persist_pause.go:42-50`), so the parked pause needs nothing more. The hook keys on `origin_conversation_id = pending.ConversationID`. `HostReview` therefore stores the conversation the agent passes (`a.ledgerConvID`) as the origin.
21. **The host review runs only with a person in the chat:** `gateway.HasResponder(ctx) && !tools.IsDelegatedDispatch(ctx)`, the gateway's `personPresent` (`internal/gateway/decide.go:51-53`). Task 5b's host-review withhold uses this test. The existing `withholdOutboundMessage` checks `HasResponder` only (`llm_agent_message_draft.go:37`) and is left as it is: it parks an inert draft, never a task row. A delegated swarm worker inherits the responder marker but has nobody on it.
    - Such a call, and a headless `agent_job` scheduling another task, falls through to `Execute`. `Execute` persists `pending_approval` and returns a plain `awaitingApprovalResult` that tells the model not to call `ask_user`.
    - For `kind=message`, `Execute` refuses as planned in Task 5b.
22. **The question is canonical English, and the renderers localize from presentation params.** `ScheduledKey` gains the params `schedule`, `when` and `summary`, parsed by a new `deriveScheduledNamed` that runs before `deriveScheduledCreated` (`presentation.go:175-197`). The latter stays for rows written before this change.
    - `when` is written `02/01 15:04 (<zone>)`, numeric, so the Italian Telegram render (`RenderItalian`, `presentation.go:254`) and the cockpit (`web/src/approvals/approvalQuestion.ts:64-67`) need no day or month names.
    - The risk tier leaves the question.
23. **The model-relayed directive retires.** `scheduledApprovalRequiredResult` (`task.go:381-407`) and its "Call ask_user with kind="approval"" text are deleted. Its `immediate_alert` field has no reader: `git grep immediate_alert` finds only its producer at `task.go:397`. Decision 33 lists the code its deletion leaves dead.
24. **Who still produces `pending_approval`, and what the sweep is still for.** The sweep's code is unchanged: it de-duplicates against the in-turn pause and never pushes it. It remains the backstop for producers 2, 3 and 5 below.
    1. The model's `task` in a turn with a person (`task.go:270-281`). It now parks in-turn, and the sweep never pushes it.
    2. The model's `task` without a person: an `agent_job` worker or a delegated worker. Rows are persisted with the origin conversation, so the sweep mints after its grace and pushes, as today.
    3. An in-turn call whose pause commit failed after the task row was written. The sweep mints.
    4. `aura task schedule` (`cmd/aura/task.go:118-123`). Its rows have no origin conversation, so `ListDuePendingApprovalReminders` already skips them (`internal/db/queries/scheduler_tasks.sql:101-118`). They keep `aura task approve`, unchanged.
    5. Rows written before deploy by the retired relay path. Recurring ones keep the sweep's reminder. One-shots expire (Task 14).

    Not producers: the mail watch (`cmd/aura/serve_pimwatch.go:330`, default status active), the provisioning seeds (`cmd/aura/serve_provisioning.go:441`, `:534`), and the board's edit, which keeps the status (`scheduler_tasks.sql:167`).
25. **The terminal state of a lapsed one-shot is `cancelled`, so there is no new status and no migration.** The 0009 CHECK allows `active`, `paused`, `pending_approval` and `cancelled` (`internal/db/migrations/0009_scheduler.up.sql:24`).
    - The run the pass opens, completed `failed` with the notice, keeps the reason in the ledger until `DeleteSettledOneShots` removes the task (`scheduler_tasks.sql:66-86`). That happens once its notification is delivered, which is the operator's rule of 2026-10-05.
    - `InsertRun` already exists (`internal/cron/store.go:263`).
26. **The pass closes the pause before it cancels the task.** If the close fails (a busy thread, a DB error), the task stays pending and the next tick retries. Otherwise a cancelled task would leave a live approval nobody can use: the hook's `UPDATE … status = 'pending_approval'` would match nothing (`serve_scheduled_approval.go:36-46`), and the cockpit's approvals would keep showing it.
27. **The sweep's own minted question stays bounded** (kind + short id). The ensurer receives only `(taskID, conversationID, kind)` (`internal/cron/deliver_approval.go:131`). The named question belongs to the in-turn pause, which spec item 3 is about.

Review of 2026-11-11 (three reviewers on `bdb6f0416`; each item re-checked against the code before it was written here):

28. **Task 5 is split, and `Draft.Review` moves to Task 6.** Task 5a is a refactor commit: `Registry.ByTrustedRecipe` and `selfSendResolver.Resolve` rewritten over it, with no behaviour change. Task 5b is the feature. `Draft.Review` lands in Task 6, so Task 11's handler no longer waits for the Telegram work (Task 10).
29. **`message` stays unadvertised until its handler exists.** CLAUDE.md merges every task to `master`, and between Tasks 5b and 11 a `message` task could be scheduled and approved with no handler to send it. So Task 5b builds `hostReviewMessage` and tests it directly, but `HostReview` does not route `kind=message` to it, the `kind` enum and the descriptions do not name it, and `Execute` refuses it with `errMessageNeedsReview`. Task 11 wires `HostReview`, the enum and the text in the same commit as the handler.
30. **A parked write is idempotent per call.** The parked call creates rows before `execTool` runs (`llm_agent_pause.go:78-82`), and a call id can be parked again on a retried turn. Migration 0155 adds `scheduler_tasks.origin_tool_call_id` with a unique partial index on `(identity_id, origin_conversation_id, origin_tool_call_id)`. `CreateScheduledTask` takes it, and `HostReview` returns the existing task's pause when the row exists. `messagedrafts.Store.Create`'s duplicate check (`internal/messagedrafts/store.go:124-131`) also compares `scheduled_task_id` and `send_at`, so a second draft for another task is `ErrDuplicate`, never the first task's draft.
31. **The host review respects the identity's gateway policy** (operator, 2026-11-11, "Confermo"). `Decide` applies the identity's policy before anything else (`internal/gateway/decide.go:80-89`), and a parked call never reaches `Decide`. A new exported `(*Gateway).PolicyDenies(ctx, spec, raw) bool` wraps `subjectFor` and `policyFor`. `withholdHostReviewedCall` returns `false` when it says deny, so the call goes to `execTool` and the gateway refuses it as today, deny fact included. `PolicyAsk` still parks: the host review is the ask.
32. **One validation prefix for both paths.** `requireTaskPayload` (landed in `c25b96d88`, called at `task.go:226-228`) runs as step 0 of `HostReview`, both branches, and of `Execute`, through one `validateSchedule(a taskArgs) error`. A host review that fails it returns `(nil, false)`, so `Execute` gives the model the refusal. An `agent_job` with no goal is never parked.
33. **The retired directive's dead code goes with it** (Task 13). `TaskTool.AlertThreshold` (`task.go:33-36`), `alertThreshold()` (`:207-212`), the `scoring.RequiresImmediateAlert` call at `:318` and the `AlertThreshold: scoring.Risky` field in `newTaskTool` (`cmd/aura/serve_adapters.go:46`) have no other reader once `scheduledApprovalRequiredResult` is gone. `scoring.RequiresImmediateAlert` itself stays: the cron dispatch still calls it (`internal/cron/dispatch.go:412`). One reviewer listed it as dead; the code says otherwise.
34. **A Telegram review is resolved under its own approval operation.** The cockpit's resolve runs under the HTTP operation the agui middleware roots (`internal/agui/idempotency_http.go:301`, and the comment at `internal/agent/llm_agent_message_draft.go:88-94`). A Telegram callback carries only the identity (`internal/channels/telegram/bot_dispatch.go:245`); `telegramTurnOperation` roots one for a typed turn only (`bot_dispatch_turn.go:150`). Without it, `beginOperation` denies with "operation context missing" (`internal/gateway/reserve.go:55-57`) and the draft ends `failed`. Task 10 roots an `idempotency.ScopeApproval` operation keyed by the draft id around `ResolveMessageDraft`, for scheduled and immediate drafts.
35. **The scheduled-task hook checks who wrote the question.** `ask_user` lets the model set `resume_context` (`internal/agent/tools/ask_user.go:97`, `:110`), and the hook accepts any approval pause of type `scheduled_task_approval` in the origin conversation (`cmd/aura/serve_scheduled_approval.go:95-114`). A later model could relay an innocent question over a pending `agent_job`'s id and have it approved blind. From Task 13 the hook reads the task row and accepts the pause only when its question equals a host-authored form for that row: `approvalQuestion` (Task 13) or the sweep's minted question (`serve_scheduled_approval.go:168`). Anything else returns an error and the task stays pending.
36. **The lapsed pass never cancels an approval in flight** (Task 14). `SubmitAnswer` commits the answer before the hook activates the task (`internal/runner/runner_resume.go:118-121`).
    - `ExpireApprovalPause` lists the pending pauses again after `TryLockThread`, and returns a tri-state: `closed`, `none`, or `answered` when the conversation holds a resolved accept for the task. `answered` skips the row; the hook activates it, or the next tick finds it.
    - A message task whose draft is already `approved` is activated (`ActivateReviewedMessage`), not cancelled; the handler then sends it inside its grace period.
    - With no pause found, the pass expires the task's pending draft by task id, so a failed pause commit (producer 3) never leaves a draft pending.
    - Cancelling a pending task on any path closes its pause the same way, with the answer `cancelled by the operator`.
37. **Deactivating an identity cancels its message tasks** (operator, 2026-11-11, "Confermo"). `PauseIdentityTasks` runs over `personKinds` (`internal/cron/store_manage.go:24`, `pause.go:71-77`), so a paused `message` task would contradict decision 14. Deactivation (`scheduledTaskAdapter.TerminateJobs`, `cmd/aura/serve_deprovision_purgers.go:30-32`) pauses the other person kinds as today, and cancels the identity's `message` tasks with their drafts declined. The purge keeps deleting them through `personKinds`.
    - **Retry-safe** (CodeRabbit on #242). The decline is by owner, not by the ids the cancel returned: `DeclineScheduledForOwner` declines every `pending` or `approved` draft of the identity that has a `scheduled_task_id`. If a decline fails after the cancel committed, the next `TerminateJobs` run still finds those drafts. Deactivation cancels every message task of the identity, so a broader scope declines nothing that should live.
    - **Ordered against the claim by the draft's status, so the decline comes first.** `ClaimScheduledMessageDraft` (`approved → dispatching`) and the decline (`pending|approved → declined`) are both compare-and-set on the same row. `TerminateJobs` declines before it cancels: a handler already running loaded the task earlier and only claims the draft, so cancelling first would leave its claim open. If the decline wins, the claim fails and nothing is sent. If the claim won first, the draft is `dispatching`, the decline leaves it alone, and that one send completes, as when the operator cancels at the same instant (decision 15).
38. **`aura task schedule --kind message` is refused** (`cmd/aura/task.go:90` takes any string). A CLI row has no review and no draft. The refusal: `aura task schedule: kind=message is scheduled from a conversation, where the operator reviews it`.
39. **The fire-time claim checks the time, and a stuck dispatch is reported honestly.** `ClaimScheduledMessageDraft` also requires `send_at <= now()`. A draft found `dispatching` with `dispatch_started_at` older than 30 minutes is marked `uncertain` and reported "delivery uncertain", as `Reconcile` does for an immediate one. When the claim fails, the handler reads the draft again before it picks the summary, so a cancel that won the race reads "declined", not "expired".
40. **The approval question says "run"** in Go and on the web (operator, 2026-11-11, "Confermo"): `It will not run until you approve.`
41. **The mutation spot-check is measured, not assumed.** CI mutates only the scopes in `scripts/critical_mutation_gate.py` (`GO_SCOPES`, `REQUIRED_SCOPE_IDS`) and the `go-mutation` matrix in `.github/workflows/ci.yml`. Task 15 registers the new critical files there. `internal/messagedrafts/store_scheduled.go` needs Postgres, which CI's mutation runners do not have, so it is measured in WSL under `db_integration` against `aura_cov` and the score is recorded in the verification file.

## File structure

| File | Change |
|---|---|
| `internal/db/migrations/0154_identity_default_notify.{up,down}.sql` | new: `default_notify` |
| `internal/db/migrations/0155_scheduled_messages.{up,down}.sql` | new: `message` kind, `send_at`, `scheduled_task_id`, `approved` |
| `internal/db/queries/identity_profile.sql`, `message_drafts.sql`, `scheduler_tasks.sql`, `internal/db/sqlc/` | queries |
| `internal/db/db_unit_test.go`, `migrate_0154_integration_test.go`, `migrate_0155_integration_test.go` | head pin, migration tests |
| `internal/onboarding/answers.go`, `profile_store.go`, `profile_block.go` | `DefaultNotify` |
| `internal/agui/profile_api.go` | `defaultNotify` on the wire |
| `internal/agent/tools/task.go`, new `task_delivery.go`, `task_default_notify.go`, `task_directives.go`, `task_message.go`, `spec.go` | default, message scheduling, `ByTrustedRecipe` |
| `internal/agent/llm_agent_message_draft.go`, `llm_agent_pause.go` | host-reviewed calls, `ReviewedMessageOutcome` |
| `internal/messagedrafts/store.go`, new `store_scheduled.go`, `pause.go`, new `review.go` | scheduled lifecycle |
| `internal/runner/message_draft_resume.go`, new `message_draft_scheduled.go`, `runner_deps.go`, `runner_profile.go` | approve/decline a scheduled draft, `Location` |
| `internal/cron/store.go`, new `store_message.go`, `pause.go`, `store_manage.go`, `dispatch.go` | kind, activation, `Job.TaskID` |
| `internal/agent/tools/` new `task_approval.go`, `task_directives.go`, `task.go` | Part 3: one gating rule, the named question, the in-turn approval, the directive retired |
| `internal/approvaltext/presentation.go`, `web/src/approvals/approvalQuestion.ts`, `web/src/i18n/resources.approval.ts` | Part 3: the named question's presentation |
| `internal/cron/` new `deliver_lapsed.go`, `store_approval.go`, `scheduler.go`, `dispatch.go`; `internal/runner/` new `runner_expire_approval.go`; `cmd/aura/serve_scheduled_approval.go`, `serve_dispatch.go` | Part 3: lapsed one-shot approvals expire and are reported |
| `internal/cron/handlers/handler.go`, new `message.go`, `agentjob.go`, new `agentjob_outbound.go` | fire-time handler, job outcome line |
| `internal/agui/governance_write_scheduler.go`, `message_drafts_api.go`, `server.go` | refusals, cancel, `send_at` |
| `internal/channels/telegram/hitl.go`, new `hitl_message_draft.go`, `bot_dispatch_hitl.go` | read-only review card |
| `cmd/aura/serve_adapters.go`, `serve_scheduled_approval.go`, new `serve_scheduled_message.go`, `serve_notify.go`, `serve_dispatch.go`, `serve_webui.go`, `chat_boot.go`, `main.go`, `task.go` | wiring and the CLI |
| `web/src/settings/profileApi.ts`, `ProfilePanel.tsx`, `web/src/i18n/resources.settings.ts`, new `resources.profileDefaultNotify.ts` | the default-channel select |
| `web/src/approvals/useThreadMessageDrafts.ts`, `MessageDraftCard.tsx`, `web/src/i18n/resources.messageDraft.ts` | fire time on the card |
| `internal/gateway/policy.go` (`PolicyDenies`) | the parked call respects the identity's policy (decision 31) |
| `cmd/aura/serve_deprovision_purgers.go`, `internal/cron/pause.go`, `internal/db/queries/scheduler_tasks.sql` | deactivation cancels message tasks (decision 37) |
| `scripts/critical_mutation_gate.py`, `scripts/critical_mutation_gate_test.py`, `.github/workflows/ci.yml` | the new critical files as CI mutation scopes (Task 15) |

---

## Part 1: the default channel

### Task 1: The profile stores the default channel

**Files:** new `internal/db/migrations/0154_identity_default_notify.up.sql`, `.down.sql`, new `internal/db/migrate_0154_integration_test.go`, `internal/db/db_unit_test.go:399-401`, `internal/db/queries/identity_profile.sql`, `internal/db/sqlc/` (regenerated), `internal/onboarding/answers.go`, `profile_store.go`, `profile_block.go`, `profile_block_test.go`, `profile_store_integration_test.go`, `internal/agui/profile_api.go`, `profile_api_test.go`

**Produces:**
```go
// onboarding.Answers gains:
DefaultNotify string // '' | none | stdout | telegram | whatsapp | email
func (s *ProfileStore) DefaultNotify(ctx context.Context, identityID string) string // never fails; "" when unknown
// agui.ProfileDoc gains:
DefaultNotify string `json:"defaultNotify"`
```

The up migration, verbatim:
```sql
-- Where reminders and scheduled results go when a request names no channel
-- (docs/superpowers/specs/2026-11-05-scheduled-delivery-defaults-and-messages-design.md, Part 1).
-- '' is "not set": the task tool then asks, as before. A setting, not a memory (0097).
ALTER TABLE aura.identity_profiles
    ADD COLUMN default_notify text NOT NULL DEFAULT ''
    CHECK (default_notify IN ('', 'none', 'stdout', 'telegram', 'whatsapp', 'email'));
```
The down migration drops the column. Queries:
- `UpsertIdentityProfile` takes `default_notify`, merging with `COALESCE(NULLIF(EXCLUDED.default_notify, ''), aura.identity_profiles.default_notify)`.
- `ReplaceIdentityProfile` overwrites it.
- New: `-- name: GetIdentityDefaultNotify :one`, `SELECT default_notify FROM aura.identity_profiles WHERE identity_id = sqlc.arg(identity_id);`.

The profile block adds `add("Default channel for reminders and scheduled results", a.DefaultNotify)` after `Custom instructions`. Then, whenever `lines` is not empty and before the vetoes, it adds this line verbatim:
`Where reminders and scheduled results go is a setting, not a memory: when the operator states a rule for it, tell them to set it in the cockpit's Profile panel.`

`validateProfileDoc` refuses a `DefaultNotify` that is neither `""` nor `cron.ValidNotifyRoute` with `profile: default channel is not a delivery route`. `answersOf` trims the value.

- [ ] `TestMigrate0154AddsTheDefaultChannel` (copy the shape of `migrate_0153_integration_test.go`). At 153, insert a profile row and step up. The row reads `default_notify = ''`. An update to `telegram` succeeds and one to `sms` fails. Stepping down drops the column.
- [ ] Set the head pin in `db_unit_test.go` to 154, and add the comment line `// 0154 adds the operator's default channel for scheduled delivery.` after the 0153 one (`internal/db/db_unit_test.go:393-399` keeps one line per migration).
- [ ] `TestTheDefaultChannelRoundTripsAndOnboardingNeverErasesIt` (`db_integration`):
  - `Replace` with `DefaultNotify: "telegram"`: `Load` gives `"telegram"` and `DefaultNotify(ctx, id) == "telegram"`.
  - `Save` with an empty `DefaultNotify`: the value is still `"telegram"`.
  - `Replace` with `""`: `DefaultNotify(...) == ""`.
- [ ] `TestDefaultNotifyIsEmptyWithoutAProfile`: a nil store and an identity with no row both give `""`.
- [ ] `TestRenderProfileBlockNamesTheDefaultChannelAndWhereToChangeIt`. `Answers{Name: "A", DefaultNotify: "telegram", Vetoes: []string{"x"}}` gives a block that holds `Default channel for reminders and scheduled results: telegram` and the rule sentence, and whose last line inside the tag is still `Never do: x`. `TestRenderProfileBlockEmptyAnswersRenderNothing` stays green unchanged.
- [ ] `TestProfileDefaultNotifyIsValidated` (agui). A PUT with `"defaultNotify":"whatsapp"` is stored and echoed. `"sms"` gives 400. `""` is accepted.
- [ ] Run and see red. Write the migration, run `make sqlc`, implement, and go green with race on `./internal/onboarding/`, `./internal/agui/` and, under `db_integration`, `./internal/db/` and `./internal/onboarding/`. Commit: `feat(profile): the operator's default channel for scheduled delivery`.

### Task 2: `task` applies the default and names it

**Files:** `internal/agent/tools/task.go`, new `task_delivery.go`, new `task_default_notify.go`, new `task_directives.go`, new `task_default_notify_test.go`, `task_test.go`, `cmd/aura/serve_adapters.go` (`newTaskTool`), `cmd/aura/serve_notify_test.go:137-141`

**Consumes:** Task 1's `ProfileStore.DefaultNotify`.

**Produces:**
```go
type taskDefaults interface {
	DefaultNotify(ctx context.Context, identityID string) string
}
// TaskTool gains:  Defaults taskDefaults
func (t *TaskTool) defaultNotify(ctx context.Context) string // a valid route, or ""
type deliveryPlan struct {
	Note       string
	Recipients []TelegramAccount
}
// resolveDelivery fills a.Notify (explicit, else the default) and runs the route, destination
// and Telegram-recipient checks of today's actionSchedule. A non-nil *ToolResult is a directive
// the model relays (delivery_choice_required, telegram_recipients_required); nothing is persisted.
func (t *TaskTool) resolveDelivery(ctx context.Context, a *taskArgs) (deliveryPlan, *ToolResult, error)
func deliveryNote(route, recipient string, fromDefault bool) string
```

`task_directives.go` takes `taskNotifyChoiceRequiredResult`, `scheduledApprovalRequiredResult`, `shortScheduledTaskID` and `deliveryNote` out of `task.go` unchanged, apart from the strings below. `actionSchedule` calls `resolveDelivery` in place of `task.go:226-264`. `newTaskTool` sets `t.Defaults` to the same `onboarding.ProfileStore` as `t.Timezones`.

Exact copy:
- `deliveryNote`: `""` for an empty recipient without the default. `Delivers via %s (your default channel).` for an empty recipient with the default. `Delivers via %s to %s (your default channel).` for a recipient with the default. `Delivers via %s to %s.` otherwise.
- The choice directive's `Message`: `No task was created. Call ask_user with kind="choice", the question and options above. After the operator answers, call task action="schedule" again with notify set to the selected option value, and tell them this choice can be saved as their default channel in the cockpit's Profile panel.`
- `notify` schema description: `Optional when action=schedule. Where the task output is delivered. If the request names no channel, omit notify: the operator's default channel is applied, and when none is set a delivery_choice_required directive comes back. none keeps the outcome only in the run ledger; telegram is an intentional push to the linked Telegram accounts named in telegram_recipients; whatsapp/email are explicit external self-sends; stdout means the owning conversation when present or literal CLI/system output.`
- The `Description` sentence that replaces the one at `task.go:170`: `For action=schedule always honor an explicit notify choice. If the request names no channel, omit notify: the operator's default channel is applied, and when none is set a delivery_choice_required directive comes back. `
- The `c25b96d88` paragraph at `task.go:168` keeps every clause but the channel's. The new sentence, verbatim: `Before scheduling, make sure the task has everything it will need when it fires: when (time or cadence), what (the reminder text or a self-contained job goal), and for a message the exact recipient and text; where it is delivered comes from an explicit notify, else the operator's default channel. A scheduled run starts in a fresh session and cannot ask anyone anything. If any of these is missing or ambiguous in the request, ask the operator one question about it, time first, and schedule only after the answer. Never invent a time or a recipient. `

- [ ] Write `task_default_notify_test.go` with a `fakeDefaults map[string]string` and the existing `fakeTaskStore`, `fakeDestinations` and `fakeTelegramAccounts`:
  - `TestAnEmptyNotifyUsesTheOperatorsDefault`, a table:
    - default `telegram` and one linked account `{1, "op"}`: `created.NotifyRoute == "telegram"`, and the preview holds `Delivers via telegram to @op (1) (your default channel).`;
    - default `stdout`: the preview holds `Delivers via stdout (your default channel).`;
    - default `telegram` with an explicit `"notify":"none"`: `NotifyRoute == "none"`, and no `(your default channel)`.
  - `TestNoDefaultStillAsksAndPointsToTheProfilePanel`: the preview's `status` is `delivery_choice_required`, the message holds `Profile panel`, and nothing is created.
  - `TestADefaultTelegramWithTwoAccountsStillAsksWhichAccount`: `telegram_recipients_required`, and nothing is created.
  - `TestADefaultThatCannotDeliverIsRefusedLikeAnExplicitOne`: default `whatsapp` with a failing `Destination`. The error holds `notify=whatsapp cannot deliver`, and nothing is created.
  - `TestAnInvalidStoredDefaultIsIgnored`: default `sms` gives the choice directive.
  - `TestAGatedJobOnTheDefaultNamesItInTheApprovalDirective`: an `agent_job` with default `stdout` gives a `pending_approval` directive whose `delivery` is `Delivers via stdout (your default channel).`.
  - `TestTheToolTextNoLongerSaysAskFirst`: `Spec().Description` does not hold `call ask_user(kind=choice) before scheduling` nor `where (the channel)`, and does hold `If the request names no channel, omit notify` and `else the operator's default channel`.
- [ ] Extend `serve_notify_test.go:137-141`: with a store, `got.Defaults != nil`; without one, `got.Defaults == nil`.
- [ ] Red, then do the split and implement. Green with race on `./internal/agent/tools/` and `./cmd/aura/`. The extraction into `task_delivery.go` and `task_directives.go` is mandatory in this task, whatever `task.go` measures: Task 5b adds schema text to it. Measure `wc -l internal/agent/tools/task.go`: it must be under 600. Commit: `feat(task): an empty notify uses the operator's default channel and says so`.

### Task 3: The Profile panel sets the default channel

**Files:** `web/src/settings/profileApi.ts`, `ProfilePanel.tsx`, `__tests__/ProfilePanel.test.tsx`, new `web/src/i18n/resources.profileDefaultNotify.ts`, `web/src/i18n/resources.settings.ts`

**Consumes:** Task 1's `defaultNotify` on `/api/profile`.

**Produces:** `ProfileDoc.defaultNotify: '' | 'none' | 'stdout' | 'telegram' | 'whatsapp' | 'email'`, `''` in `emptyProfile()`. A `ProfileDefaultNotify` field in `ProfilePanel.tsx` renders the cockpit `Select` (`@/components/ui/select`) with the options in this order: `''`, `telegram`, `whatsapp`, `email`, `stdout`, `none`. `resources.settings.ts` adds `fields.defaultNotify`, `hints.defaultNotify` and `defaultNotify: profileDefaultNotifyEn` (and `…It`).

Copy:
- en: field `Channel for reminders and scheduled results`; hint `Used when you do not name a channel. Aura says which channel it used.`; options `Ask me every time`, `Telegram`, `WhatsApp`, `Email`, `The conversation it was scheduled from`, `No notification`.
- it: field `Canale per promemoria e risultati pianificati`; hint `Si usa quando non indichi un canale. Aura dice quale ha usato.`; options `Chiedimi ogni volta`, `Telegram`, `WhatsApp`, `Email`, `La conversazione da cui è stato pianificato`, `Nessuna notifica`.

- [ ] Vitest `it('saves the default channel, and "Ask me every time" as empty')`. Load `defaultNotify: 'telegram'`, and the select shows `Telegram`. Choose `Ask me every time` and save: `firstPut(put).defaultNotify === ''`. Choose `WhatsApp` and save: the next PUT carries `'whatsapp'`.
- [ ] Fix on touch: `storedProfile()` uses a real first name and town. Replace them with `Ada` and `Example Town`.
- [ ] Red, implement, green: `npx vitest run src/settings/__tests__/ProfilePanel.test.tsx`, then the web commands of Global Constraints (the i18n tests catch a key missing in it or en). Commit: `feat(cockpit): set the default channel in the Profile panel`.

## Part 2: the `message` task

### Task 4: Message drafts can belong to a scheduled task

**Files:** new `internal/db/migrations/0155_scheduled_messages.up.sql`, `.down.sql`, new `internal/db/migrate_0155_integration_test.go`, `internal/db/db_unit_test.go`, `internal/db/queries/message_drafts.sql` (`InsertMessageDraft`, `ClaimMessageDraftSend`), `internal/db/sqlc/`, `internal/messagedrafts/store.go`, `store_test.go`, `store_integration_test.go`, `internal/cron/store.go`, `store_manage.go`

**Produces:**
```go
// messagedrafts.DraftInput and Draft gain:
ScheduledTaskID string
SendAt          time.Time // zero for an immediate review
// cron:
const KindMessage TaskKind = "message" // personKinds gains it
```

The up migration, verbatim:
```sql
-- A message to someone else, reviewed when it is scheduled and sent once at its time
-- (docs/superpowers/specs/2026-11-05-scheduled-delivery-defaults-and-messages-design.md, Part 2).
ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres',
        'skill_ttl_sweep', 'identity_purge', 'sandbox_reap', 'share_expiry_sweep',
        'retention_sweep', 'memory_embed_backfill', 'observability_check',
        'memory_mention_link', 'backup_objects', 'pim_watch', 'pim_digest', 'message'));

-- send_at marks a scheduled draft. The task may be deleted once settled; the draft stays as
-- the record of what was sent, so the link goes NULL rather than cascading.
ALTER TABLE aura.message_drafts
    ADD COLUMN scheduled_task_id uuid REFERENCES aura.scheduler_tasks(id) ON DELETE SET NULL,
    ADD COLUMN send_at timestamptz;
-- approved: reviewed, waiting for its time. Only a scheduled draft is ever approved.
ALTER TABLE aura.message_drafts DROP CONSTRAINT message_drafts_status_check;
ALTER TABLE aura.message_drafts ADD CONSTRAINT message_drafts_status_check CHECK (status IN
    ('pending', 'approved', 'dispatching', 'sent', 'failed', 'declined', 'uncertain', 'expired'));
ALTER TABLE aura.message_drafts ADD CONSTRAINT message_drafts_scheduled_chk
    CHECK (scheduled_task_id IS NULL OR send_at IS NOT NULL);
ALTER TABLE aura.message_drafts ADD CONSTRAINT message_drafts_send_at_chk
    CHECK (send_at IS NULL OR expires_at > send_at);
ALTER TABLE aura.message_drafts ADD CONSTRAINT message_drafts_approved_chk
    CHECK (status <> 'approved' OR (send_at IS NOT NULL AND effective_args IS NOT NULL
                                     AND effective_fingerprint IS NOT NULL));
CREATE UNIQUE INDEX message_drafts_task_idx ON aura.message_drafts (scheduled_task_id)
    WHERE scheduled_task_id IS NOT NULL;

-- The model call that parked the task: a retried turn parks the same call again and must
-- find this row, not create a second one (decision 30).
ALTER TABLE aura.scheduler_tasks ADD COLUMN origin_tool_call_id text;
CREATE UNIQUE INDEX scheduler_tasks_origin_call_idx
    ON aura.scheduler_tasks (identity_id, origin_conversation_id, origin_tool_call_id)
    WHERE origin_tool_call_id IS NOT NULL;
```
The down migration first deletes the drafts with `send_at IS NOT NULL` and the tasks with `kind = 'message'`. Then it drops the two indexes, the three new constraints and the three columns, and restores 0150's kind CHECK and 0134's status CHECK.

`InsertMessageDraft` takes `sqlc.narg(scheduled_task_id)` and `sqlc.narg(send_at)`. `ClaimMessageDraftSend` adds `AND send_at IS NULL`. `Create` refuses, as `errInvalidDraftArgs` and before any query, a `SendAt` without a `ScheduledTaskID` or the reverse, a `SendAt` not in the future, and an `ExpiresAt` not after `SendAt`. `draftFromRow` maps both columns. `Create`'s duplicate-by-call check (`store.go:124-131`) also compares `scheduled_task_id` and `send_at` (decision 30). `CreateScheduledTask` takes `OriginToolCallID`; a unique violation on its index returns the existing row (`GetScheduledTaskByOriginCall :one`). `IsPausableKind` stays `IsCancellableKind` in this task; Task 8 narrows it.

- [ ] `TestMigrate0155AdmitsTheScheduledMessage` (copy the shape of 0153's):
  - at 155, a `message` task inserts;
  - a draft with `send_at` and no task inserts;
  - a draft with a task and no `send_at` fails;
  - a draft with `expires_at <= send_at` fails;
  - `status = 'approved'` with a NULL `effective_args` fails;
  - deleting the task leaves the draft with `scheduled_task_id IS NULL`;
  - two tasks with the same `(identity_id, origin_conversation_id, origin_tool_call_id)` fail, and two with a NULL call id insert;
  - stepping down removes the `message` rows and the columns.
- [ ] Set the head pin to 155, with the comment line `// 0155 adds the scheduled message: its kind, its draft's time and task, and the parking call.`
- [ ] `TestADraftForAnotherTaskIsADuplicate` (`db_integration`): a second `Create` with the same call and another `ScheduledTaskID` gives `ErrDuplicate`.
- [ ] `TestTheSameCallCreatesOneTask` (cron `db_integration`): a second `CreateScheduledTask` with the same origin call returns the first row's id.
- [ ] `TestCreateRefusesAHalfScheduledDraft` (unit, nil pool). `SendAt` without a task id, a task id without `SendAt`, `SendAt` in the past, and `ExpiresAt == SendAt` each give `errInvalidDraftArgs`.
- [ ] `TestAScheduledDraftIsCreatedBoundToItsTask` (`db_integration`): `Get` gives back `ScheduledTaskID`, and `SendAt` within 1 ms.
- [ ] `TestClaimSendRefusesAScheduledDraft` (`db_integration`): `ClaimSend` on a pending scheduled draft gives `ErrUnavailable`, and the row is still `pending`.
- [ ] `TestAMessageTaskIsCancellable` (cron unit): `IsCancellableKind(KindMessage)` and `IsAgentStoppableKind(KindMessage)` are both true.
- [ ] Red, migrate, `make sqlc`, implement, green with race on `./internal/messagedrafts/` and `./internal/cron/`, and under `db_integration` on `./internal/db/` and `./internal/messagedrafts/`. Commit: `feat(messagedrafts): a draft can belong to a scheduled message task`.

### Task 5a: The trusted recipe tool is found by one helper (refactor)

Decision 28. No behaviour changes.

**Files:** `internal/agent/tools/spec.go` (`ByTrustedRecipe`), its test, `cmd/aura/serve_notify.go` (`selfSendResolver.Resolve` over it), `serve_notify_test.go`

**Produces:**
```go
func (r *Registry) ByTrustedRecipe(recipe, tool string) (Tool, bool) // the lowest name among matches
```
The registry is a map (`spec.go:281-289`), so the helper sorts by name before it picks the first match.

- [ ] `TestByTrustedRecipeIsDeterministic`: with two matching tools named `b__send_message` and `a__send_message`, it returns `a__send_message`. No match gives `false`.
- [ ] The existing `selfSendResolver` tests stay green unchanged.
- [ ] Red, implement, green with race on `./internal/agent/tools/` and `./cmd/aura/`. Commit: `refactor(tools): find the trusted recipe tool through one registry helper`.

### Task 5b: `task` can park a message for review (not yet offered to the model)

Decision 29: this task builds the review and its pause, and `HostReview` does not route `kind=message` until Task 11.

**Files:** new `internal/agent/tools/task_message.go`, new `task_message_test.go`, `task.go` (the `kind=message` branch in `actionSchedule`, `validateSchedule`), `internal/gateway/policy.go` (`PolicyDenies`), its test, `internal/agent/llm_agent_message_draft.go`, `llm_agent_pause.go` (`pauseCalls` and its comment), `llm_agent_message_draft_test.go`, new `cmd/aura/serve_scheduled_message.go`, new `serve_scheduled_message_test.go`, new `serve_scheduled_message_integration_test.go`, `serve_adapters.go` (`newTaskTool` gains `reg *tools.Registry`), `main.go:205` and the six `newTaskTool` test call sites (`serve_notify_test.go:42`, `:45`, `:137`, `:140`, the timezone and the telegram-recipients integration tests)

**Consumes:** Task 2's `resolveDelivery`, Task 4's `DraftInput.ScheduledTaskID`, `SendAt` and `OriginToolCallID`, Task 5a's `ByTrustedRecipe`.

**Produces:**
```go
const ScheduledMessageGrace = 15 * time.Minute
// HostReviewer is a tool whose call the host parks for the operator's review before it runs.
// The agent calls it only with a live responder; (nil, false) lets the call run normally.
type HostReviewer interface {
	HostReview(ctx context.Context, conversationID, toolCallID string, raw json.RawMessage) (*ErrAwaitingUserInput, bool)
}
type ScheduledMessageDraft struct {
	ConversationID, ToolCallID, TaskID string
	Channel                            string          // whatsapp | email
	Args                               json.RawMessage // the trusted recipe tool's arguments
	SendAt                             time.Time
}
type taskMessageDrafts interface {
	CreateScheduledMessageDraft(ctx context.Context, in ScheduledMessageDraft) (draftID string, err error)
}
// TaskTool gains:  MessageDrafts taskMessageDrafts
type messagePayload struct {
	Channel   string   `json:"channel"`
	Recipient string   `json:"recipient"`
	To        []string `json:"to"`
	Account   string   `json:"account"`
	Subject   string   `json:"subject"`
	Text      string   `json:"text"`
}
func (p messagePayload) recipeArgs() (json.RawMessage, error)
func (t *TaskTool) messageSchedule(a taskArgs, now time.Time) (resolvedSpec, messagePayload, error)
func (t *TaskTool) HostReview(ctx context.Context, conversationID, toolCallID string, raw json.RawMessage) (*ErrAwaitingUserInput, bool)
func (t *TaskTool) hostReviewMessage(ctx context.Context, conversationID, toolCallID string, a taskArgs) (*ErrAwaitingUserInput, bool)
func validateSchedule(a taskArgs) error // requireTaskPayload and the kind check, shared by HostReview and Execute (decision 32)
// gateway:
func (g *Gateway) PolicyDenies(ctx context.Context, spec tools.Spec, raw json.RawMessage) bool // decision 31
// agent:
func (a *LlmAgent) withholdHostReviewedCall(ctx context.Context, call llm.ToolCall) (pauseCall, bool)
// cmd/aura:
type scheduledMessageDrafts struct {
	store *messagedrafts.Store
	reg   *tools.Registry
}
func (d scheduledMessageDrafts) CreateScheduledMessageDraft(ctx context.Context, in tools.ScheduledMessageDraft) (string, error)
```

In this task `HostReview` parses the args and returns `(nil, false)` for every kind (decision 29); Task 11 routes `kind=message` to `hostReviewMessage`, and Task 13 the gated kinds to `hostReviewApproval`. `hostReviewMessage` takes these steps, in order:
0. It runs `validateSchedule`. An error gives `(nil, false)`, so `Execute` refuses with it.
1. It returns `(nil, false)` when `MessageDrafts` is nil.
2. It runs `resolveDelivery` and `messageSchedule`. On a directive or an error it returns `(nil, false)`, so `Execute` returns that directive or error to the model.
3. It persists the task through `Store.CreateScheduledTask`: `Kind "message"`, `ScheduleKind "at"`, `Status "pending_approval"`, the payload, `NotifyRoute` and `TelegramRecipients` from the plan, `OriginConversationID = conversationID`, `OriginToolCallID = toolCallID`. When a row already exists for that call (decision 30), it uses that row and its draft, and returns the same pause.
4. It calls `CreateScheduledMessageDraft`. If that fails, it logs `slog.Warn("task: scheduled message draft not created", "task", id, "error_type", fmt.Sprintf("%T", err))`, calls `Store.CancelScheduledTask`, and returns `(nil, false)`.
5. It returns `&ErrAwaitingUserInput{Question: "Review this scheduled message before it is sent", Kind: KindApproval, Priority: 90, ToolCallID: toolCallID, ResumeContext: {"type":"message_draft","draft_id":…,"task_id":…}}`.

In `actionSchedule`, `kind=message` runs `resolveDelivery` (so a directive still comes back first), then `messageSchedule`, then returns `errMessageNeedsReview`. It never creates anything. The agent's `pauseCalls` tries `withholdHostReviewedCall` for every call that `withholdOutboundMessage` did not take. That method needs `gateway.HasResponder(ctx) && !tools.IsDelegatedDispatch(ctx)` (decision 21), a tool that implements `tools.HostReviewer`, an owner on ctx, and `a.gateway.PolicyDenies` false for the call (decision 31; a nil gateway denies nothing). It passes `a.ledgerConvID` and `call.ID`, and it sets `originalToolName` and `originalArguments` as the outbound withhold does. The adapter resolves the trusted tool and passes `Store.Create` these fields:
- `Target` from `TrustedOutboundTarget`;
- `RegisteredToolName` from `spec.Name`;
- `ExpiresAt = SendAt + tools.ScheduledMessageGrace`;
- the task id and `SendAt`.

With no tool mounted it fails with `WhatsApp is not connected` or `email is not connected`.

The schema enum, the payload description and the `Description` sentence for `message` land in Task 11 (decision 29). Errors (each ends `Nothing was scheduled.`):
  - `task schedule: kind=message is one-shot only (schedule_kind=at); recurring messages to someone else are not supported.`
  - `task schedule: kind=message can be scheduled at most 23h45m ahead, because a reviewed message lives 24 hours.`
  - `task schedule: kind=message needs a time in the future.`
  - `task schedule: a message needs channel whatsapp or email, a recipient and a text.`
  - `errMessageNeedsReview`: `task schedule: a message to someone else is scheduled only in a conversation where the operator reviews it, and no review could be prepared here. Nothing was scheduled; tell the operator.`

- [ ] Tools tests, with fakes for the store and the drafts. They call `hostReviewMessage` directly, since `HostReview` does not route it yet:
  - `TestMessageIsNotOfferedYet`: `HostReview` with `kind=message` gives `(nil, false)`, `Execute` gives `errMessageNeedsReview`, `Spec()` names no `message` kind, and nothing is created.
  - `TestAMessageIsScheduledPendingWithItsDraftAndAReviewPause`. Call `hostReviewMessage(ctx, "conv-1", "call-1", …)` with `{"channel":"whatsapp","recipient":"10000000000","text":"Ciao"}`, `at` = now + 2h and `notify` `stdout`. Expected:
    - the pause has `ToolCallID == "call-1"` and `Kind == "approval"`;
    - its `ResumeContext` decodes to `type "message_draft"`, `draft_id "draft-1"` and `task_id` equal to the created task id;
    - the created task has `Kind "message"`, `Status "pending_approval"`, `ScheduleKind "at"`, `OriginConversationID "conv-1"` and `OriginToolCallID "call-1"`;
    - the draft input has `Channel "whatsapp"`, `Args` JSON-equal to `{"recipient":"10000000000","message":"Ciao"}`, and `SendAt` equal to `at`.
  - `TestAnEmailMessageMapsToTheCalendarSend`: `Args` JSON-equals `{"action":"send_email","accountId":"acc-1","to":["someone@example.com"],"subject":"Hi","body":"Text","bodyFormat":"text"}`.
  - `TestAMessageRefusesRecurrenceTheFarFutureAndThePast`. `every`, `at` = now + 24h and `at` = now − 1m each make `hostReviewMessage` return `(nil, false)`. Nothing is created.
  - `TestAFailedDraftCancelsTheTask`: when the drafts fake errors, `hostReviewMessage` gives `(nil, false)` and `store.cancelled` is the created id.
  - `TestAMessageWithNoChannelReturnsTheChoiceFirst`: with no notify and no default, `hostReviewMessage` gives `(nil, false)`.
  - `TestTheSameCallParkedTwiceCreatesOneTask`: a second `hostReviewMessage` with `call-1` creates nothing, and its pause names the same task and draft.
  - `TestAMessageWithoutTextIsNeverParked`: an empty `text` gives `(nil, false)` and the `validateSchedule` error (decision 32).
- [ ] Agent tests:
  - `TestAHostReviewedCallParksWithoutExecuting`. With a responder, a fake `HostReviewer` tool gives one pause event with `ToolCallID == call.ID` and `OriginalToolName == "task"`, and its `Execute` count is 0.
  - `TestAHostReviewedCallWithoutAResponderRunsNormally`. With no responder, and with a responder under `tools.WithDelegatedDispatch` (`internal/agent/tools/result.go:176`), `HostReview` is not called and `Execute` is called once.
  - `TestAPolicyDeniedTaskIsNeverParked`: with the identity's policy `deny` on `task`, `HostReview` is not called, and the call reaches `execTool` and is refused by the gateway.
- [ ] Gateway: `TestPolicyDeniesReadsTheIdentitysPolicy`: `deny` gives true; `ask`, `allow` and no policy store give false.
- [ ] cmd/aura:
  - `TestNoMountedWhatsAppRefusesTheScheduledDraft` (unit): an empty registry gives an error holding `WhatsApp is not connected`.
  - `TestTheRegisteredTaskToolIsHostReviewed` (unit, beside `serve_notify_test.go:137`): `newTaskTool(newCronTaskStore(nil, nil), nil, "", tools.NewRegistry())` has a non-nil `MessageDrafts`, and the value registered under `task` satisfies `tools.HostReviewer`. Without a store, `MessageDrafts` is nil.
  - `TestAScheduledMessageDraftIsStoredForItsTask` (`db_integration`). With a `message` task row and a fake trusted tool `whatsapp__send_message` registered, `Store.Get` gives back `ScheduledTaskID`, `SendAt`, `ExpiresAt == SendAt + 15m` and `RegisteredToolName "whatsapp__send_message"`.
- [ ] Red, implement, green with race on `./internal/agent/tools/`, `./internal/gateway/`, `./internal/agent/` and `./cmd/aura/`, and with `db_integration` on `./cmd/aura/ -run ScheduledMessage`. Check that `task.go` is still under 600 lines. Commit: `feat(task): a message can be parked behind a host-authored review`.

### Task 6: The review approves or declines a scheduled message

**Files:** new `internal/messagedrafts/store_scheduled.go`, new `review.go`, its test, `store.go` (`StatusApproved`), `internal/db/queries/message_drafts.sql` (`ApproveScheduledMessageDraft`), `internal/db/queries/scheduler_tasks.sql` (`ActivateReviewedMessageTask`, `CancelReviewedMessageTask`), `internal/db/sqlc/`, new `internal/cron/store_message.go`, new `internal/runner/message_draft_scheduled.go`, `message_draft_resume.go`, `runner_deps.go`, `runner.go`, `cmd/aura/chat_boot.go:493`, tests beside each

**Consumes:** Task 4's columns, Task 5b's pause.

**Produces:**
```go
const StatusApproved Status = "approved"
func (d Draft) Scheduled() bool // !d.SendAt.IsZero()
// Review is what the operator reviews: the effective arguments when set, else the original ones (moved here from Task 10, decision 28).
func (d Draft) Review() (recipient, subject, text string)
// Approve fixes the effective arguments of a pending scheduled draft before its send time.
func (s *Store) Approve(ctx context.Context, ownerID, draftID string, overrides json.RawMessage) (Draft, error)
// runner:
type ScheduledMessageTasks interface {
	ActivateReviewedMessage(ctx context.Context, ownerID, taskID string) error
	CancelReviewedMessage(ctx context.Context, ownerID, taskID string) error
}
// Deps gains:  MessageTasks ScheduledMessageTasks
func (r *Runner) resolveScheduledMessage(ctx context.Context, ownerID string, draft messagedrafts.Draft, action string, overrides json.RawMessage) (messagedrafts.Draft, string, error)
// cron.Store implements ScheduledMessageTasks.
```

The queries:
- `ApproveScheduledMessageDraft :one` sets `status = 'approved'`, the effective args and the fingerprint, under `status = 'pending' AND send_at IS NOT NULL AND send_at > now() AND expires_at > now()`.
- `ActivateReviewedMessageTask :execrows` is `UPDATE aura.scheduler_tasks SET status = 'active', updated_at = now() WHERE id = $1 AND identity_id = $2 AND kind = 'message' AND status IN ('pending_approval', 'active')`. It is idempotent, so a re-committed review succeeds.
- `CancelReviewedMessageTask :execrows` does the same with `status = 'cancelled'` under `status = 'pending_approval'`.
- Zero rows is an error: `message task %s is not awaiting its review`.

`ResolveMessageDraft` branches to `resolveScheduledMessage` right after `pendingForMessageDraft` when `draft.Scheduled()`, and keeps the rest of its commit path.
- **send:** a pending draft is approved with the overrides. A pending or approved draft then gets `ActivateReviewedMessage`. Content: `scheduled message approved: it will be sent at <SendAt RFC3339 UTC>, and its outcome reported on the task's notify route (task <id>)`.
- **decline:** a pending draft is declined and the task cancelled. Content: `the operator declined the scheduled message; nothing will be sent and task <id> is cancelled`.
- **anything else:** an expired draft keeps today's `message review expired without sending`. Any other state is `ErrUnavailable`.
- **after an approval:** if activation fails, the error is returned and the draft stays `approved`, so the card's "continue" retries.

- [ ] `TestReviewReadsTheEditedArgumentsFirst` (messagedrafts unit, moved from Task 10). For WhatsApp it gives `(recipient, "", message)`, from `EffectiveArgs` when set. For email it gives `(to joined by ", ", subject, body)`.
- [ ] `TestApproveFixesTheEditedArgumentsOnce` (`db_integration`): `Approve` with `{"message":"Edited"}` gives `StatusApproved`, `EffectiveArgs.message == "Edited"` and a fingerprint of 64 characters. A second `Approve` gives `ErrUnavailable`.
- [ ] `TestApproveRefusesAfterTheSendTime` (`db_integration`): after the admin pool sets `send_at = now() - interval '1 second'`, `Approve` gives `ErrUnavailable` and the row is still `pending`.
- [ ] `TestApproveRefusesAnImmediateDraft` (`db_integration`): a draft without `SendAt` gives `ErrUnavailable`.
- [ ] `TestOnlyItsOwnersMessageTaskIsActivatedByItsReview` (cron `db_integration`):
  - a pending `reminder` gives an error;
  - a `message` of another identity gives an error;
  - the owner's `message` becomes `active`, and a second call is nil;
  - `CancelReviewedMessage` on an active task gives an error.
- [ ] `TestApprovingAScheduledDraftActivatesItsTaskAndSendsNothing` (runner `db_integration`, the shape of `message_draft_resume_integration_test.go`). Use a fake `MessageTasks` and a registered trusted spy tool. `ResolveMessageDraft(ctx, id, "send", {"message":"Edited"})` gives `StatusApproved` and `OutcomeContinue`. The fake saw `Activate(owner, taskID)`, the spy's `Execute` count is 0, and the committed answer starts `scheduled message approved: it will be sent at `.
- [ ] `TestDecliningAScheduledDraftCancelsItsTask`: `StatusDeclined`, the fake saw `Cancel(owner, taskID)`, and the answer is the decline content.
- [ ] `TestAFailedActivationIsRetriedFromTheCard`: the first activation errors and the draft stays `approved`. A second `send` activates and commits.
- [ ] Red, `make sqlc`, implement, wire `MessageTasks: cron.New(pool)` in `chat_boot.go`, and go green with race on `./internal/runner/`, plus `db_integration` on `./internal/messagedrafts/`, `./internal/cron/` and `./internal/runner/`. Commit: `feat(runner): the review approves a scheduled message and activates its task`.

### Task 7: Only the draft review activates a message task

**Files:** `internal/db/queries/scheduler_tasks.sql` (`ApproveTaskRow`), `internal/db/sqlc/`, `internal/cron/store_manage.go`, `internal/messagedrafts/pause.go`, `pause_test.go`, `internal/agui/governance_write_scheduler.go`, its test, `cmd/aura/serve_scheduled_approval.go`, its tests, `cmd/aura/serve_adapters.go` (delete `ApproveScheduledTask`), `cmd/aura/task.go` (`taskApprove` over `cron.Store.ApproveTask`)

**Consumes:** Task 4's `cron.KindMessage`, Task 5b's resume context.

**Produces:**
```go
func PauseTaskID(raw json.RawMessage) (string, bool) // messagedrafts: the task_id of a scheduled review pause
var errMessageReviewMissing = errors.New("the scheduled message has no review pause; it is approved only from its message card") // cmd/aura
```

The changes, one per path:
- `ApproveTaskRow` adds `AND kind <> 'message'`.
- `handleSchedulerApprove` answers 409 `{"error":"approve it from the message card"}` for `KindMessage` before calling the provider.
- `ApproveScheduledTaskInConversation` and `CancelScheduledTaskInConversation` add `AND kind <> 'message'`. Their zero-row error ends ` (a scheduled message is approved from its message card)`.
- `approvalPauseEnsurer.EnsureApprovalPause`, for `kind == "message"`, returns the token of the pending review pause whose `PauseTaskID` matches, with `minted=false`. With no match it returns `errMessageReviewMissing`. It never calls the minter.
- `taskSchedule` refuses `--kind message` before it writes (decision 38): `aura task schedule: kind=message is scheduled from a conversation, where the operator reviews it`, exit `exitUsage`.
- `taskApprove` calls `cron.New(pool).ApproveTask`, which wraps `cron.ErrTaskNotFound` when no row changed (`internal/cron/store_manage.go:103-116`). On `errors.Is(err, cron.ErrTaskNotFound)` it prints `aura task approve: %s is not awaiting approval (a scheduled message is approved from its message card)` and exits `exitUsage`.

- [ ] `TestTheCLINeverSchedulesAMessage` (cmd/aura unit): `aura task schedule --kind message …` exits `exitUsage` with the refusal, and the store saw no insert.
- [ ] `TestPauseTaskIDReadsOnlyAScheduledReview`: `{"type":"message_draft","draft_id":"d","task_id":"t"}` gives `("t", true)`. Without `task_id`, or with another type, it gives `false`.
- [ ] `TestApproveTaskRefusesAMessageTask` (cron `db_integration`): it returns the not-awaiting error, and the row is still `pending_approval`.
- [ ] `TestTheBoardSendsAMessageTaskToItsCard` (agui): approve answers 409 with that body, and the fake provider's `ApproveTask` was not called.
- [ ] `TestTheScheduledTaskHookNeverActivatesAMessageTask` (cmd/aura `db_integration`). The hook gets `accept` and a `scheduled_task_approval` context naming a pending `message` task. It returns an error, and the task is still `pending_approval`.
- [ ] `TestTheSweepNeverMintsABareApprovalForAMessage` (unit, fake lister and minter):
  - a lister holding a matching review pause gives `(its token, false, nil)`;
  - an empty lister gives `errMessageReviewMissing`;
  - the minter is never called;
  - an `agent_job` still mints as before.
- [ ] Red, `make sqlc`, implement, and delete the dead `cronTaskStore.ApproveScheduledTask`. `deadcode` stays clean: `make deadcode` in WSL. Green with race on `./internal/agui/`, `./internal/messagedrafts/` and `./cmd/aura/`, with `db_integration` on `./internal/cron/` and `./cmd/aura/`. Commit: `fix(scheduler): a message task is activated only by its draft review`.

### Task 8: A message task is cancelled, never edited

**Files:** `internal/messagedrafts/store_scheduled.go` (`DeclineForTask`), `internal/db/queries/message_drafts.sql`, `internal/db/queries/scheduler_tasks.sql` (`PauseIdentityTaskRows`, `CancelIdentityMessageTasks`), `internal/db/sqlc/`, `internal/cron/pause.go`, `store_manage.go`, `cmd/aura/serve_deprovision_purgers.go`, its test, `internal/agui/governance_write_scheduler.go`, `server.go` (one field), its tests, `cmd/aura/serve_adapters.go` (`cronTaskStore.drafts`, `CancelScheduledTask`, `RunScheduledTaskNow`), `cmd/aura/serve_webui.go`, `cmd/aura/task.go` (`taskCancel`, `taskRunNow`)

**Produces:**
```go
// DeclineForTask declines the task's draft while it is pending or approved; a claimed one is untouched.
func (s *Store) DeclineForTask(ctx context.Context, ownerID, taskID string) error
// DeclineScheduledForOwner declines every pending or approved scheduled draft of the owner (deactivation, decision 37).
func (s *Store) DeclineScheduledForOwner(ctx context.Context, ownerID string) error
// agui:
type ScheduledMessageCanceller interface {
	DeclineForTask(ctx context.Context, ownerID, taskID string) error
}
func (s *Server) SetScheduledMessageCanceller(c ScheduledMessageCanceller)
```

The query: `DeclineScheduledMessageDraftsForTask :exec` sets `status = 'declined', resolved_at = now(), updated_at = now()` where `identity_id = $1 AND scheduled_task_id = $2 AND status IN ('pending', 'approved')`.
- `IsPausableKind` returns `IsCancellableKind(k) && k != KindMessage`.
- The board's pause 403 text becomes `this task cannot be paused`.
- `handleSchedulerRun` and `handleSchedulerEdit` answer 409 `{"error":"a scheduled message cannot be run now or edited; cancel it and schedule a new one"}` for `KindMessage`.
- `handleSchedulerCancel` calls the canceller for `KindMessage` after `CancelTask`, with `task.IdentityID`. A canceller error is a 502.
- `cronTaskStore.CancelScheduledTask` declines after its UPDATE when `kind == "message"`. `newCronTaskStore` builds `drafts: messagedrafts.NewStore(pool)`.
- `RunScheduledTaskNow` and `taskRunNow` add `AND kind <> 'message'` to their UPDATE.
- `taskCancel` reads the task with `cron.Store.GetTask` and declines for a message task.
- Deactivation (decision 37): `PauseIdentityTaskRows` takes `personKinds` without `KindMessage` (a new `pausableIdentityKinds()`); a new `CancelIdentityMessageTasks :many` cancels the identity's `message` tasks in `pending_approval` or `active` and returns their ids; `scheduledTaskAdapter.TerminateJobs` first calls `DeclineScheduledForOwner(ownerID)` (decision 37: by owner, so a retry still finds the drafts, and before the cancel, so a handler already running cannot claim one), then `PauseIdentityTaskRows` and `CancelIdentityMessageTasks`. The three cancel paths of decision 15 likewise call `DeclineForTask` before `CancelTask`. `DeleteIdentityTasks` keeps `personKinds`, `message` included.

- [ ] `TestDeclineForTaskDeclinesOnlyAnUnsentDraft` (`db_integration`): pending and approved drafts become `declined`, and a `dispatching` one is unchanged.
- [ ] `TestCancellingAMessageTaskDeclinesItsDraft` (cmd/aura `db_integration`, through the task tool's `cronTaskStore`): the task is `cancelled` and its approved draft is `declined`.
- [ ] `TestCancellingAMessageTaskOnTheBoardDeclinesItsDraft` (agui): the fake canceller saw `(task identity, id)`. A reminder's cancel does not call it.
- [ ] `TestAMessageTaskCannotBeRunPausedOrEdited` (agui): run and PATCH give 409 with the text above, and pause gives 403 `this task cannot be paused`.
- [ ] `TestAMessageTaskIsNotPausable` (cron unit): `IsPausableKind(KindMessage)` is false and `IsPausableKind(KindReminder)` is true.
- [ ] `TestDeactivationDeclineIsRetrySafe` (cmd/aura `db_integration`): the first `TerminateJobs` declines the draft and its cancel is made to fail; the second `TerminateJobs` cancels the task; the draft ends `declined`, never `pending` or `approved`, and the task `cancelled`.
- [ ] `TestCancelDeclinesBeforeTheTaskSoARunningHandlerCannotClaim` (cmd/aura `db_integration`): a handler holding the loaded task claims after a cancel (task-tool path and deactivation path); the claim fails and nothing is sent.
- [ ] `TestDeactivationAndClaimOrderByDraftStatus` (messagedrafts `db_integration`): decline-then-claim leaves the draft `declined` and the claim fails; claim-then-decline leaves it `dispatching`.
- [ ] `TestDeactivationCancelsMessageTasksAndDeclinesTheirDrafts` (cmd/aura `db_integration`): after `TerminateJobs`, the identity's reminder is `paused`, its pending and approved message tasks are `cancelled` with their drafts `declined`, and another identity's message task is untouched.
- [ ] Red, `make sqlc`, implement, wire `SetScheduledMessageCanceller(messagedrafts.NewStore(pool))` in `serve_webui.go`, and go green. `agui/server.go` and `serve_adapters.go` stay under 600 lines. Commit: `feat(scheduler): cancelling a message task declines its draft`.

### Task 9: The cockpit card shows when the message will be sent

**Files:** `internal/agui/message_drafts_api.go`, its test, `web/src/approvals/useThreadMessageDrafts.ts`, `MessageDraftCard.tsx`, `__tests__/MessageDraftCard.test.tsx`, `web/src/i18n/resources.messageDraft.ts`

**Consumes:** Task 6's `approved` state.

**Produces:** `messageDraftItem.SendAt *time.Time \`json:"send_at,omitempty"\``, set when `draft.Scheduled()`. `MessageDraft.send_at?: string`, and `MessageDraftStatus` gains `'approved'`. When `send_at` is set, the card shows a line above the destination, `t('messageDraft.sendAt', { time })`, with `time` from `new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' })`. Its submit button reads `t('messageDraft.approveScheduled')`. A non-editable `approved` card offers `continue` with action `send`, as for `sent`.

Copy: en `sendAt: 'Sends at {{time}}'`, `approveScheduled: 'Approve for that time'`, `status.approved: 'Approved. It will be sent at the scheduled time.'`. it `sendAt: 'Parte alle {{time}}'`, `approveScheduled: "Approva per quell'ora"`, `status.approved: "Approvato. Partirà all'ora prevista."`.

- [ ] `TestAScheduledDraftCarriesItsSendTime` (agui): a scheduled draft projects `send_at` and an immediate one omits the key.
- [ ] Vitest `it('shows when a scheduled message will be sent and approves it with the edits')`. The card shows `Sends at`, and after editing the text, `Approve for that time` sends `{action:'send', overrides:{recipient, message:'Edited'}}`.
- [ ] Vitest `it('an approved scheduled message offers only to continue')`: the status text is the approved copy, and the button sends `action:'send'` with no overrides.
- [ ] Red, implement, green: Go with race on `./internal/agui/`, and `npx vitest run src/approvals/__tests__/MessageDraftCard.test.tsx` plus the web commands of Global Constraints. Commit: `feat(cockpit): the message card shows the time of a scheduled message`.

### Task 10: Telegram shows the review as a read-only card

**Files:** `internal/runner/runner_profile.go` (`Location`), `internal/channels/telegram/hitl.go` (`resumeRunner`, `prompt`, `handleCallbackResult`, `handleTextReply`), new `hitl_message_draft.go`, new `hitl_message_draft_test.go`, the fakes that implement `resumeRunner` in the package tests

**Consumes:** Task 6's scheduled resolve and `Draft.Review`.

**Produces:**
```go
// runner:
func (r *Runner) Location(ctx context.Context) *time.Location
// telegram resumeRunner gains:
ListMessageDrafts(ctx context.Context, conversationID string) ([]messagedrafts.Draft, error)
ResolveMessageDraft(ctx context.Context, draftID, action string, overrides json.RawMessage) (runner.MessageDraftResolution, error)
Location(ctx context.Context) *time.Location
// hitl_message_draft.go:
const reviewTextMaxRunes = 3000
func messageReviewText(d messagedrafts.Draft, loc *time.Location) string
func messageReviewMarkup(token string, d messagedrafts.Draft) *tele.ReplyMarkup
func (h *hitl) reviewDraft(ctx context.Context, convID string, p askuser.Pending) (messagedrafts.Draft, bool)
func (h *hitl) resolveReview(ctx context.Context, convID string, p askuser.Pending, action string) callbackOutcome
```

How each piece behaves:
- **`prompt`:** before its `switch`, a pause with `messagedrafts.IsReviewPause` renders `messageReviewText` with `messageReviewMarkup`.
- **`handleCallbackResult`:** after the Dettagli check, a review pause goes to `resolveReview`. Accept maps to `"send"`, decline and cancel map to `"decline"`, and the overrides are nil. It drives the resume only on `OutcomeContinue`. `SubmitAnswer` is never called for it.
  - The pause is found by matching the callback's token among `PendingFor(convID)` of this chat's conversation, never by a global token lookup, so a button from another chat cannot act on this chat's review.
  - `resolveReview` roots an `idempotency.ScopeApproval` operation keyed by the draft id around `ResolveMessageDraft` (decision 34), as the agui resolve runs under its HTTP operation. This covers immediate and scheduled drafts.
- **`handleTextReply`:** with a review pause first, it returns `(false, nil)` and submits nothing. `hitlHandlesText` then re-renders the card.
- **The keyboard:** a pending draft gets `approvalMarkup(token)`. Any other draft gets one button, `Chiudi`. It sends `decline` for `declined` and `expired`, and `accept` otherwise. `Chiudi` on a `dispatching` card that answers `ErrUnavailable` re-renders the card with its current status, rather than leaving it stuck.

The pending copy, verbatim. The `Oggetto` line appears for email only, and the `Quando` line for a scheduled draft only:
```
📨 Messaggio WhatsApp da approvare        (email: 📨 Email da approvare)
A: <recipient>
Oggetto: <subject>
Quando: <SendAt in loc as "02/01 15:04"> (<loc.String()>)
Testo:
<text, cut at 3000 runes with "… (testo completo nel cockpit)">

Serve il tuo ok perché il messaggio va a un'altra persona. Senza risposta non parte nulla.
Per cambiare il testo apri questa conversazione nel cockpit.
```
A non-pending card shows the title line and then the status: `approved` `Approvato: partirà all'ora prevista.`, `declined` `Rifiutato: non parte nulla.`, `expired` `Scaduto: non è partito nulla.`, `sent` `Inviato.`, `failed` `Non inviato.`, `uncertain` `Consegna incerta: non verrà ritentato.`

- [ ] `TestAScheduledReviewRendersTheMessageAndTheTime`. A WhatsApp draft has recipient `10000000000`, text `Ciao` and `SendAt` 2026-11-06T07:25Z, with the location `Europe/Rome`. The text holds `A: 10000000000`, `Quando: 06/11 08:25 (Europe/Rome)`, `Testo:\nCiao` and both closing sentences. The keyboard is `Approva` and `Rifiuta`.
- [ ] `TestAnImmediateReviewHasNoTimeLine`: no `Quando:`.
- [ ] `TestApprovaResolvesTheDraftNotTheAnswer`: the fake runner saw `ResolveMessageDraft(draftID, "send", nil)` and no `SubmitAnswer`. Its `OutcomeContinue` drove `resume` once.
- [ ] `TestRifiutaDeclinesTheDraft`: `ResolveMessageDraft(draftID, "decline", nil)`.
- [ ] `TestTelegramApprovaSendsUnderAStrictGateway` (runner `db_integration`, with a strict gateway and a real operation registry, the shape of `message_draft_resume_integration_test.go`): Approva on an immediate draft dispatches once and the draft is `sent`, not `failed` with "operation context missing".
- [ ] `TestAButtonFromAnotherChatIsIgnored`: a callback whose token belongs to another chat's pause resolves nothing.
- [ ] `TestChiudiOnADispatchingCardRerendersIt`.
- [ ] `TestATypedReplyNeverAnswersAReview`: no `SubmitAnswer`, and the card is sent again.
- [ ] `TestALongMessageIsCutWithAPointerToTheCockpit`: a text of 5000 runes gives a card of at most 4096 runes that holds `… (testo completo nel cockpit)`, and Approva still resolves.
- [ ] Red, implement, green with race on `./internal/channels/telegram/`, `./internal/runner/` and `./internal/messagedrafts/`, and with `db_integration` on `./internal/runner/`. `hitl.go` stays under 600 lines. Commit: `feat(telegram): review a message to someone else from a read-only card`.

### Task 11: At its time the message goes out once, with no model

**Files:** `internal/agent/tools/task.go` and `task_message.go` (enum, text, routing), their tests, `internal/cron/dispatch.go` (`Job.TaskID`), `internal/db/queries/message_drafts.sql` (`GetMessageDraftByTask`, `ClaimScheduledMessageDraft`, `ExpireScheduledMessageDraft`), `internal/db/sqlc/`, `internal/messagedrafts/store_scheduled.go`, `internal/agent/llm_agent_message_draft.go` (`ReviewedMessageOutcome`, moved from `internal/runner/message_draft_resume.go:194-203`), `internal/cron/handlers/handler.go` (`Job.TaskID`, `KindMessage`, `jobLocation`), new `internal/cron/handlers/message.go`, new `message_test.go`, `cmd/aura/serve_dispatch.go`

**Consumes:** Task 6's `approved` drafts and `Draft.Review`, Task 5b's `hostReviewMessage`.

This task also offers `message` to the model (decision 29), in the same commit as its handler: `HostReview` routes `kind=message` to `hostReviewMessage`, and the exact copy below lands in `task.go`:
- The `kind` enum gains `"message"`. Its description gains `message (send one fixed message to someone else once, reviewed by the operator now)`.
- The payload description gains: `for a message {"channel":"whatsapp","recipient":"<JID or number>","text":"..."} or {"channel":"email","account":"<accountId>","to":["..."],"subject":"...","text":"..."}`.
- The `Description` gains: `kind=message sends one fixed message to someone else (WhatsApp or email) once, at most 23h45m ahead, with schedule_kind=at: resolve the exact recipient first and ask the operator now if two contacts match; the operator reviews the recipient and the text before the task exists, and it is sent at its time with no model in between. `

**Produces:**
```go
func (s *Store) ForTask(ctx context.Context, ownerID, taskID string) (Draft, error)
func (s *Store) ClaimScheduled(ctx context.Context, ownerID, draftID string) (Draft, error) // approved -> dispatching, expires_at > now
func (s *Store) ExpireScheduled(ctx context.Context, ownerID, draftID string) (Draft, error)
func ReviewedMessageOutcome(err error) (messagedrafts.Status, string) // agent
func jobLocation(ctx context.Context, tz jobTimezones, fallback *time.Location) *time.Location
type MessageDrafts interface {
	ForTask(ctx context.Context, ownerID, taskID string) (messagedrafts.Draft, error)
	ClaimScheduled(ctx context.Context, ownerID, draftID string) (messagedrafts.Draft, error)
	ExpireScheduled(ctx context.Context, ownerID, draftID string) (messagedrafts.Draft, error)
	MarkOutcome(ctx context.Context, ownerID, draftID string, status messagedrafts.Status, code string) (messagedrafts.Draft, error)
}
type MessageHandler struct {
	Drafts     MessageDrafts
	Tools      *tools.Registry
	Gateway    *gateway.Gateway
	RunDir     string
	PreviewCap int
	Timezones  jobTimezones
	Location   *time.Location
}
const KindMessage TaskKind = "message"
const messageMaxDuration = 2 * time.Minute
```

The queries (decision 8: nothing releases a pending message task, Task 14 expires it):
- `ClaimScheduledMessageDraft` requires `status = 'approved' AND send_at <= now() AND expires_at > now()` (decision 39), and sets `dispatching` and `dispatch_started_at`.
- `ExpireScheduledMessageDraft` requires `send_at IS NOT NULL AND send_at <= now() AND (status = 'pending' OR (status = 'approved' AND expires_at <= now()))`.

`Meta` gives `{KindMessage, messageMaxDuration, ReschedulesOnRecovery: true}`. `Run` reads the owner from `identityctx` and the draft with `ForTask(owner, job.TaskID)`, then branches on its status. `<r>` is `Review()`'s recipient and `<t>` is the current time in `jobLocation`, formatted `15:04`:
- **approved:** it looks up `Tools.Get(draft.RegisteredToolName)` before the claim and checks it as `ResolveMessageDraft` does (`message_draft_resume.go:122-130`). With no tool it returns the error `scheduled message to <r> not sent: its channel is not connected`, without claiming.
  - Then `ClaimScheduled`, `agent.ExecuteReviewedMessage(ctx, tool, Gateway, claim, RunDir, PreviewCap)`, and `MarkOutcome` on a 5 s detached context, with `ReviewedMessageOutcome`.
  - `sent` gives the summary `Message sent to <r> at <t>.`.
  - `failed` gives the error `message to <r> not sent: the channel reported it did not go out`.
  - `uncertain` gives the error `message to <r>: delivery is uncertain, and it was not retried`.
  - If `ClaimScheduled` gives `ErrUnavailable`, the handler reads the draft again with `ForTask` and branches on its new status (decision 39): `declined` gives the declined summary; an `approved` draft past its lifetime gets `ExpireScheduled` and the summary `Scheduled message to <r> not sent: it expired before it could be sent.`.
- **pending:** Task 7's refusals mean this branch is unreachable. It is kept as a defence: `ExpireScheduled`, then `Scheduled message to <r> not sent: it was not approved in time.`.
- **declined:** `Scheduled message to <r> not sent: it was declined.`.
- **expired:** `Scheduled message to <r> not sent: it expired before it could be sent.`.
- **dispatching with `dispatch_started_at` older than 30 minutes:** `MarkOutcome(uncertain, "stale_dispatch")`, then the error `message to <r>: delivery is uncertain, and it was not retried`.
- **dispatching (recent), sent, failed or uncertain:** `Scheduled message to <r> was already handled (<status>); nothing was sent again.`.
- **no draft:** the error `scheduled message: no reviewed draft for task <id>`.

The runner calls `agent.ReviewedMessageOutcome` in place of its local copy. `AgentDeps.clockLocation` calls `jobLocation`. `serve_dispatch.go` registers `cron.KindMessage: handlers.MessageHandler{…}` with `messagedrafts.NewStore(chat.pool)`, `chat.reg`, `chat.gateway`, the run dir, the preview cap, `onboarding.NewProfileStore(chat.pool)` and `tools.LocationOrUTC(chat.cfg.Timezone)`.

- [ ] Handler unit tests (fake drafts, spy trusted tool, nil gateway, frozen location):
  - `TestAnApprovedMessageIsSentOnceAtItsTime`: one `Execute` with the effective args, `MarkOutcome(sent, "ok")`, and the summary `Message sent to 10000000000 at 08:25.`.
  - `TestASecondFireNeverSendsAgain`. A draft in `dispatching` or `sent` gives `Execute` count 0 and the "already handled" summary. Two concurrent `Run`s over a fake whose `ClaimScheduled` succeeds once give exactly one `Execute`.
  - `TestAPendingDraftIsNeverSent`: a pending draft gives `ExpireScheduled` once, `Execute` count 0, and the summary ending `it was not approved in time.`.
  - `TestAMissingChannelLeavesTheDraftApproved`: no registered tool gives the error and no `ClaimScheduled` call.
  - `TestAStaleDispatchIsReportedUncertain`: a draft `dispatching` for 31 minutes gives `MarkOutcome(uncertain, "stale_dispatch")`, no `Execute`, and the uncertain error.
  - `TestALostClaimReadsTheDraftAgain`: `ClaimScheduled` fails and the second `ForTask` gives `declined`, so the summary ends `it was declined.`.
  - `TestAMessageIsOfferedToTheModel` (tools): `Spec()` names `message` in the enum and the description, and `HostReview` with `kind=message` parks it.
  - `TestAFailedSendIsAFailedRun`: the tool returns `agent.ErrToolNotExecuted`, which gives `MarkOutcome(failed, "no_effect")` and the `not sent` error.
  - `TestTheFireTimeSendRunsUnderTheScheduledOperation`: the shape of `TestExecuteReviewedMessageDispatchesUnderTheResolveOperation`, with `idempotency.ScopeSchedulerRun`, gives one dispatch.
- [ ] `TestClaimScheduledTakesOnlyAnApprovedDraftOnce` (`db_integration`): the first claim gives `dispatching` and the second gives `ErrUnavailable`. A pending draft gives `ErrUnavailable`. An approved draft whose `send_at` is still ahead gives `ErrUnavailable`.
- [ ] `TestExpireScheduledLeavesAnApprovedDraftUntilItsLifetimeEnds` (`db_integration`): an approved draft with `send_at` past but `expires_at` future is unchanged. A pending one becomes `expired`.
- [ ] Red, `make sqlc`, implement, green with race on `./internal/cron/...`, `./internal/agent/`, `./internal/agent/tools/` and `./internal/runner/`, and with `db_integration` on `./internal/messagedrafts/`. `dispatch.go` and `task.go` stay under 600 lines. Commit: `feat(cron): a reviewed message is sent once at its time, with no model`.

### Task 12: An `agent_job` that tried to write to someone says so

**Files:** `internal/agent/tools/task.go` (text), `internal/cron/handlers/agentjob.go` (`drain`, `Run`), new `agentjob_outbound.go`, new `agentjob_outbound_test.go`

**Produces:**
```go
type drainResult struct {
	content       string
	pause         *agent.AwaitingInput
	askedMutating bool
	sendRefused   bool
}
func drain(ctx context.Context, worker *agent.LlmAgent, budget *agent.Budget, h AgentJobHandler, kept *resultLog) (drainResult, error)
func (h AgentJobHandler) refusedOutboundSend(inv *agent.ToolInvocation) bool
const outboundRefusedLine = "A message to someone else was not sent: a scheduled job cannot write to other people; schedule a reviewed message instead."
```
`refusedOutboundSend` is true when four things hold: `inv.Event == agent.ToolInvocationEnd`, `inv.Refused`, the registry knows `inv.ToolName`, and `messagedrafts.TrustedOutboundTarget(spec, json.RawMessage(inv.Arguments))` matches. Before every return of a run whose drains reported `sendRefused`, `Run` appends `outboundRefusedLine` once.

The `Description` sentence after the `agent_job` one, verbatim: `An agent_job cannot send a message to anyone other than the operator; to send a fixed message later, use kind=message. `

- [ ] `TestAJobWhoseSendWasRefusedSaysSoInItsOutcome`. A scripted fake client calls a registered trusted `whatsapp__send_message`, then answers `Fatto, inviato.`. The summary ends with `outboundRefusedLine`, and the tool's `Execute` count is 0.
- [ ] `TestTheLineIsAddedOnceForTwoRefusedSends`.
- [ ] `TestAnOrdinaryRefusalAddsNoLine`: a refused non-trusted mutating tool adds no line.
- [ ] `TestTheJobTextSaysItCannotWriteToOthers`: `Spec().Description` holds the sentence.
- [ ] Red, implement, green with race on `./internal/cron/handlers/` and `./internal/agent/tools/`. Commit: `feat(agent_job): a refused send to someone else is reported, never promised`.

## Part 3: every approval in the creating chat, and none left hanging

### Task 13: Every task that needs approval is approved in the chat that creates it

Part 3 items 1–4. Builds on Task 5b's `HostReviewer` and agent withhold (policy check included), which need no change.

**Files:** new `internal/agent/tools/task_approval.go`, new `task_approval_test.go`, `task_message.go` (`HostReview` dispatches by kind), `task.go` (`actionSchedule` over `taskNeedsApproval`, text, the dead alert-threshold code of decision 33), `cmd/aura/serve_adapters.go:46` (the `AlertThreshold` field), `task_directives.go` (delete `scheduledApprovalRequiredResult`, add `awaitingApprovalResult`), `task_test.go` and the other tests that pin the retired directive, `internal/approvaltext/presentation.go`, `presentation_test.go`, `internal/agent/llm_agent_pause.go` (the `ARCHITECTURAL CONSTRAINT` comment names host-reviewed calls), `cmd/aura/serve_scheduled_approval.go` (the comment at `:76-83`, and the hook's question check of decision 35), its tests, `web/src/approvals/approvalQuestion.ts`, `__tests__/approvalQuestion.test.ts`, `web/src/i18n/resources.approval.ts`

**Consumes:** Task 2's `resolveDelivery`, Task 5b's `HostReviewer`, `validateSchedule` and `withholdHostReviewedCall`.

**Produces:**
```go
// taskNeedsApproval is the one gating rule (AG-016 plus the scorer), read before the call runs and inside it.
func taskNeedsApproval(a taskArgs) (scoring.RiskTier, bool)
func approvalSchedule(spec resolvedSpec) string               // "once" | "every 30m" | `cron "30 7 * * 1-5"`
func approvalWhen(next time.Time, loc *time.Location) string  // "06/11 08:25 (Europe/Rome)"
func taskSummary(kind string, payload json.RawMessage) string // one line, ≤160 runes + "…"
func approvalQuestion(kind, taskID string, spec resolvedSpec, next time.Time, payload json.RawMessage) string
func (t *TaskTool) hostReviewApproval(ctx context.Context, conversationID, toolCallID string, a taskArgs) (*ErrAwaitingUserInput, bool)
func (t *TaskTool) hostReviewMessage(ctx context.Context, conversationID, toolCallID string, a taskArgs) (*ErrAwaitingUserInput, bool)
func awaitingApprovalResult(taskID, kind, schedule, tier, delivery string) ToolResult
// approvaltext:
func deriveScheduledNamed(question, task string) (Presentation, bool)
```

How the pieces behave:
- `approvalQuestion` renders the Global Constraints question in `tools.LocationOrUTC(spec.TZ)`, which is the zone the task row stores.
- `taskSummary` reads `goal` for `agent_job` and `text` for `reminder`. Any other kind gives the kind's name, and an empty value gives `(no goal)`. Whitespace is collapsed.
- `HostReview` parses the call and returns `(nil, false)` unless `action=schedule`. Then `kind=message` goes to `hostReviewMessage` (routed since Task 11) and every other kind to `hostReviewApproval`, which needs only `Store`.
- `hostReviewApproval` takes these steps, in order:
  0. It runs `validateSchedule` (decision 32). An error gives `(nil, false)`.
  1. It returns `(nil, false)` when `taskNeedsApproval` says no.
  2. It runs `resolveDelivery`. A directive or an error gives `(nil, false)`.
  3. It fills `a.TZ` with `t.scheduleZone(ctx)` when empty, then runs `resolveSchedule`. An error gives `(nil, false)`.
  4. It persists the task `pending_approval` with `OriginConversationID = conversationID` and `OriginToolCallID = toolCallID`; an existing row for that call is reused (decision 30). An error is logged by type and gives `(nil, false)`.
  5. It returns `&ErrAwaitingUserInput{Question: approvalQuestion(…), Kind: KindApproval, Priority: 90, ToolCallID: toolCallID, ResumeContext: {"type":"scheduled_task_approval","task_id":<id>}}`.
- `actionSchedule` uses `taskNeedsApproval` for its status. A `pending_approval` result is `awaitingApprovalResult`, which is reached only without a person in the chat (decision 21). Its copy, verbatim:
  `scheduled task <id> (kind=<kind>, <spec.summary()>, risk=<tier>, status=pending_approval). It needs the operator's approval and will not fire until they approve it; they are reminded on their channel. Do not call ask_user for it.`
  The delivery line follows it on its own line, when present.
- The `Description`'s last sentence (`task.go:170`) becomes: `A task that needs approval (every agent_job, and payloads scored destructive) is shown to the operator for approval in this chat before it can fire; with nobody in the chat it waits, and the operator is reminded on their channel.`
- `deriveScheduledNamed` parses the question. It cuts the prefix `Approve scheduled `, then ` task <short> (`, then `), first run `. The suffix `. It will not run until you approve.` goes, and the first `? It runs: ` splits `when` from `summary`.
  - The params are `task`, `kind`, `schedule`, `when` and `summary`.
  - `valid` gains the limits `when` (`maxShortParamBytes`) and `summary` (`maxArgsBytes`).
  - `deriveScheduled` tries it before `deriveScheduledCreated`.
- `RenderItalian`, with `when` and `summary` present: `Approva l'attività pianificata %s %s (%s), prima esecuzione %s?\nEsegue: %s\nNon verrà eseguita finché non approvi.`, with `once` rendered `una volta`.
- The scheduled-task hook (decision 35) reads the task row by `rc.TaskID` and the origin conversation, and accepts the pause only when `pending.Question` equals `approvalQuestion(task kind, id, spec, next run, payload)` or the sweep's minted question for that row (`serve_scheduled_approval.go:168`). Otherwise it returns `scheduled task approval: the question was not written by the host`, and the task stays `pending_approval`.
- The dead code of decision 33 goes: `TaskTool.AlertThreshold`, `alertThreshold()`, the `RequiresImmediateAlert` call in `actionSchedule`, and the field in `newTaskTool`. `scoring.RequiresImmediateAlert` stays (`internal/cron/dispatch.go:412`).
- Web: `approvalQuestion.ts`'s `SCHEDULED_KEY` branch renders `t('approval.question.scheduledNamed', {...params, schedule})` when `when` and `summary` are present, with `schedule === 'once'` mapped to `t('approval.question.once')`.
  - en: `scheduledNamed: 'Approve scheduled {{kind}} task {{task}} ({{schedule}}), first run {{when}}? It runs: {{summary}}. It will not run until you approve.'`, `once: 'once'`.
  - it: `scheduledNamed: "Approvare l'attività pianificata {{kind}} {{task}} ({{schedule}}), prima esecuzione {{when}}? Esegue: {{summary}}. Non verrà eseguita finché non approvi."`, `once: 'una volta'`.

- [ ] `TestTheGateBeforeTheCallIsTheGateInsideIt`. The table holds: a reminder `Buy bread`; an `agent_job` whose goal `scoring.ComputeTaskTier` scores destructive (take the payload from a destructive `agent_job` case in `internal/scoring/scoring_test.go`); an ordinary `agent_job`; a `backup_postgres`; and a reminder `every` 30 minutes. No reminder is ever gated from `task`: `baseTaskTier` makes it `Safe` and the modifier bumps key on schedule kinds the tool never passes (`internal/scoring/scoring.go:91-112`). For each case, three things agree:
  - `taskNeedsApproval`'s bool;
  - whether `HostReview` (with stores and `"notify":"stdout"`) returns a pause;
  - whether a fresh tool's `Execute` on the same args creates `pending_approval`.

  Whenever `HostReview` parked, its own created task is `pending_approval`.
- [ ] `TestTheApprovalQuestionNamesKindTimeAndGoal` (pure). With `kind "agent_job"`, id `01a19af5-0000-…`, an `at` spec with `TZ "Europe/Rome"`, next 2026-11-06T07:25Z and goal `Riassumi la posta`, the question equals `Approve scheduled agent_job task 01a19af5 (once), first run 06/11 08:25 (Europe/Rome)? It runs: Riassumi la posta. It will not run until you approve.`.
- [ ] `TestARecurringQuestionNamesItsCadence`: `every 30m` and `cron "30 7 * * 1-5"` appear in the parentheses.
- [ ] `TestTheSummaryIsOneBoundedLine`:
  - a 300-rune goal with newlines gives 160 runes plus `…` and no `\n`;
  - an empty goal gives `(no goal)`;
  - a reminder gives its text;
  - `backup_postgres` gives `backup_postgres`.
- [ ] `TestAnAgentJobIsParkedInTheChatThatCreatesIt`. `HostReview(ctx, "conv-1", "call-1", …)` for an `agent_job` at now + 2h gives `ToolCallID "call-1"` and `Kind "approval"`. Its `ResumeContext` decodes to `{"type":"scheduled_task_approval","task_id":<created id>}`. The question starts `Approve scheduled agent_job task ` and holds `It runs: `. The created task has `Status "pending_approval"` and `OriginConversationID "conv-1"`.
- [ ] `TestAHarmlessTaskIsNotParked`: a `Buy bread` reminder gives `(nil, false)` from `HostReview`, and `Execute` creates it `active`.
- [ ] `TestAnAgentJobWithoutAGoalIsNeverParked`: `HostReview` gives `(nil, false)` and `Execute` refuses with `requireTaskPayload`'s error; nothing is created.
- [ ] `TestTheHookRefusesAQuestionTheHostDidNotWrite` (cmd/aura `db_integration`): an accept on a `scheduled_task_approval` pause whose question is `Confermi il promemoria?` returns the error and the `agent_job` stays `pending_approval`; the named question and the sweep's minted question both activate it.
- [ ] `TestTheAlertThresholdIsGone`: `git grep -n 'AlertThreshold' -- internal/agent/tools cmd/aura/serve_adapters.go` is empty (asserted by `make deadcode` and the build).
- [ ] `TestAHeadlessScheduleWaitsWithoutADirective`: `Execute` for an `agent_job` returns the `awaitingApprovalResult` copy, and the preview holds neither `resume_context` nor `Call ask_user`.
- [ ] `TestTheNamedScheduledQuestionDerivesItsParams` (approvaltext). `Enrich(question, {"type":"scheduled_task_approval","task_id":"01a19af5-…"})` gives the five params, and `RenderItalian` gives the Italian copy above with `una volta`. A question naming another short id gives no presentation. The existing `deriveScheduledCreated` tests stay green unchanged.
- [ ] Vitest `it('renders a named scheduled approval with its cadence, time and goal')`, in en and it.
- [ ] Rewrite the tests that pinned the retired directive: `git grep -n 'scheduledApprovalRequiredResult\|kind=\\"approval\\"' -- '*_test.go'` lists them. Each rewritten test now pins `awaitingApprovalResult`, and the commit body says why: the behaviour changed by design (spec Part 3 item 4).
- [ ] Red, implement, green with race on `./internal/agent/tools/`, `./internal/approvaltext/`, `./internal/agent/` and `./cmd/aura/`, with `db_integration` on `./cmd/aura/`, then the vitest file. Then the web commands of Global Constraints. Run `make deadcode` in WSL: it stays clean. `task.go` stays under 600 lines. Commit: `feat(task): a task that needs approval asks in the chat that creates it`.

### Task 14: A one-shot that passes its time unapproved expires, and the operator is told

Part 3 items 5–6, and the message "no answer" of Part 2 (decision 8).

**Files:** `internal/db/queries/scheduler_tasks.sql` (`ListLapsedApprovals`, `ExpireLapsedApproval`), `internal/db/sqlc/`, `internal/cron/store_approval.go`, new `internal/cron/deliver_lapsed.go`, new `deliver_lapsed_test.go`, new `lapsed_integration_test.go`, `scheduler.go` (`tick`), `dispatch.go` (`DispatchDeps`, `NewDispatch` defaults), new `internal/runner/runner_expire_approval.go`, new `runner_expire_approval_integration_test.go`, `internal/messagedrafts/store_scheduled.go` (`ApprovedForTask`, `ExpireForTask`), `cmd/aura/serve_scheduled_approval.go`, `serve_dispatch.go`, `serve_adapters.go` (cancel closes the pause), `internal/agui/governance_write_scheduler.go`

**Consumes:** Task 11's `messagedrafts.Store.ExpireScheduled`, Task 7's `messagedrafts.PauseTaskID`, Task 6's `ActivateReviewedMessage`.

**Produces:**
```go
// cron
type LapsedApprovalStore interface {
	ListLapsedApprovals(ctx context.Context, limit int) ([]Task, error)
	ExpireLapsedApproval(ctx context.Context, id string) (bool, error) // CAS: false when the task left pending_approval first
	InsertRun(ctx context.Context, taskID string, stepBudget int) (Run, error)
}
type PauseClosed int // decision 36
const (
	PauseNone     PauseClosed = iota // no pending pause for the task
	PauseClosedNow                    // closed by this call
	PauseAnswered                     // the conversation holds a resolved accept for the task: skip, the hook activates it
)
type LapsedPauseCloser interface {
	CloseLapsedApproval(ctx context.Context, task Task, answer string) (PauseClosed, error)
}
type LapsedMessages interface { // decision 36
	ApprovedForTask(ctx context.Context, ownerID, taskID string) (bool, error)
	ActivateReviewedMessage(ctx context.Context, ownerID, taskID string) error
	ExpireForTask(ctx context.Context, ownerID, taskID string) error // a pending draft only
}
// DispatchDeps gains:  LapsedApprovals LapsedApprovalStore (defaulted from *Store); LapsedPauseCloser LapsedPauseCloser; LapsedMessages LapsedMessages
func (d *Dispatch) expireLapsedApprovals(ctx context.Context) error
func lapsedApprovalText(task Task) string // the Global Constraints notice
func lapsedPauseAnswer(task Task) string
// runner
func (r *Runner) ExpireApprovalPause(ctx context.Context, conversationID, taskID, answer string) (cron.PauseClosed, error)
// cmd/aura
type lapsedPauseCloser struct{ runner *runner.Runner }
func (c lapsedPauseCloser) CloseLapsedApproval(ctx context.Context, task cron.Task, answer string) (cron.PauseClosed, error)
```

The queries:
- `ListLapsedApprovals :many` selects the task columns `WHERE status = 'pending_approval' AND schedule_kind = 'at' AND next_run_at <= now() ORDER BY next_run_at LIMIT $1`. It has no age floor, so tasks stuck before deploy are taken on the first pass (item 6).
- `ExpireLapsedApproval :execrows` is `UPDATE aura.scheduler_tasks SET status = 'cancelled', updated_at = now() WHERE id = $1 AND status = 'pending_approval' AND schedule_kind = 'at' AND next_run_at <= now()`.

`expireLapsedApprovals` takes these steps per row, at most 50 a tick:
1. It binds `identityctx.WithIdentityID(ctx, operationIdentity(task.IdentityID))`.
2. For a `message` task whose draft is already `approved` (`LapsedMessages.ApprovedForTask`), it calls `ActivateReviewedMessage` and moves to the next row: the approval won, and the handler sends it inside its grace period (decision 36).
3. With a closer, it calls `CloseLapsedApproval(ctx, task, lapsedPauseAnswer(task))`. On error it logs `slog.Warn("lapsed approval: pause not closed (retry next tick)", "task", id, "err", err)` and skips the row (decision 26). `PauseAnswered` skips the row. `PauseNone` on a `message` task calls `LapsedMessages.ExpireForTask`, so a draft whose pause was never committed does not stay pending.
4. It calls `ExpireLapsedApproval`. `false` skips the row.
5. It calls `InsertRun(task.ID, 0)`. On error it logs at Error and skips the row.
6. It sets `task = d.homeTask(ctx, task)` and calls `d.complete(ctx, task, run.ID, "failed", text, errors.New(text))`. Then `undelivered := d.notify(ctx, task, run.ID, text, nil)`, then `d.recordToOrigin(ctx, task, text, nil, undelivered)`.

A store-query error is returned, and the tick logs it as it logs the other sweeps. `tick` calls the pass through a type-asserted `lapsedApprovalSweeper` after `sweepNotifications` and before `sweepApprovalReminders`, so a lapsed one-shot is never reminded first.

- `lapsedApprovalText` renders `next_run_at` in `time.LoadLocation(task.TZ)`, or in UTC when that fails, as `02/01 15:04 (<zone>)`.
- `lapsedPauseAnswer`, verbatim: `Scheduled <kind> task <first 8 of id> was not approved in time; it has been cancelled and did not run. Do NOT re-schedule it unless the operator asks.`

`ExpireApprovalPause` takes these steps:
1. It lists the conversation's pending pauses. It looks for an approval pause whose context is either `{"type":"scheduled_task_approval","task_id":taskID}` (read with `approvaltext.RootString`) or a message review with `PauseTaskID == taskID`. If none matches, it returns `PauseAnswered` when the conversation holds a resolved accept answer for that task, else `PauseNone`.
2. It takes `TryLockThread`. A busy thread gives `ErrThreadBusy`. It then lists the pending pauses again (decision 36): a pause resolved meanwhile gives `PauseAnswered` or `PauseNone` by the same rule, and nothing is committed.
3. For a message review, it calls `ExpireScheduled` on its draft. `ErrUnavailable` is tolerated, because the draft is already terminal.
4. It commits the answer as `ResolveMessageDraft` does (`message_draft_resume.go:161-172`): action `decline`, with `claim.Turn.Content` and `claim.Answer.Content` set to `answer`. It never runs the resume hook.

`lapsedPauseCloser` returns `PauseNone` for a task with no origin conversation. `serve_dispatch.go` sets `LapsedPauseCloser: lapsedPauseCloser{runner: chat.run}` and `LapsedMessages` over `messagedrafts.NewStore(chat.pool)` and `cron.New(chat.pool)`.

Cancel closes the pause (decision 36): the task tool's cancel (`cronTaskStore.CancelScheduledTask`) and the board's cancel call `ExpireApprovalPause` with the answer `Scheduled <kind> task <first 8 of id> was cancelled by the operator; it will not run.` for a task that was `pending_approval`. `aura task cancel` runs outside the daemon and leaves the pause; Task 16's verification file records that limit. If `dispatch.go` would pass 600 lines, `DispatchDeps` moves into a new `dispatch_deps.go` in the same commit.

- [ ] Unit tests (fake store, closer, notifier, recorder), in `deliver_lapsed_test.go`:
  - `TestALapsedOneShotIsExpiredReportedAndClosed`. The task is an `agent_job` with TZ `Europe/Rome`, `NextRunAt` 2026-11-02T17:00Z and notify `telegram`. Expected:
    - the closer gets `Scheduled agent_job task <8> was not approved in time; it has been cancelled and did not run. Do NOT re-schedule it unless the operator asks.`;
    - then `ExpireLapsedApproval` and `InsertRun` run;
    - the run completes `failed`;
    - the notifier and the conversation recorder each receive `agent_job for 02/11 18:00 (Europe/Rome) was not approved in time and did not run.`.
  - `TestAPauseThatCannotBeClosedIsRetriedNextTick`: a closer error gives no CAS, no run and no notice.
  - `TestALostRaceSendsNothing`: when the CAS returns `false`, there is no run and no notice.
  - `TestAnApprovalInFlightIsNotExpired`: a closer that returns `PauseAnswered` gives no CAS, no run and no notice.
  - `TestAnApprovedMessageDueNowIsActivatedNotCancelled`: `ApprovedForTask` true gives `ActivateReviewedMessage` once, and no closer call, CAS, run or notice.
  - `TestAMessageWithNoPauseHasItsDraftExpired`: `PauseNone` on a `message` task gives `ExpireForTask` once, then the CAS and the notice.
- [ ] `db_integration` tests in `lapsed_integration_test.go`:
  - `TestOnlyLapsedOneShotsAreListed`. Only the pending `at` task past its time is listed. A pending `at` task in the future, a pending `every` task past due, and an `active` `at` task past its time are not.
  - `TestATaskStuckBeforeDeployExpiresOnTheFirstPass`: a pending `at` `agent_job` with `created_at` and `next_run_at` three days ago is `cancelled` after one pass, and has one `failed` run carrying the notice.
  - `TestAnUnapprovedMessageIsReportedAndSettles`. A pending `message` task past its time has a pending draft and its review pause. After one tick:
    - the task is `cancelled` and its run summary holds `was not approved in time and did not run`;
    - the draft is `expired`, and the pause is resolved, so `/api/approvals` and `ListMessageDrafts` no longer show it;
    - once the notice is delivered, `DeleteSettledOneShots` removes the task and the draft stays.
- [ ] `TestExpireApprovalPauseClosesTheTasksPause` (runner `db_integration`):
  - a `scheduled_task_approval` pause for the task is resolved with the answer, and `ListPending` is empty;
  - a message review pause has its draft `expired` and is resolved;
  - a conversation with no matching pause gives `PauseNone`, and one whose pause was answered accept gives `PauseAnswered`;
  - a pause resolved between the first listing and the lock commits nothing (a hook on the lock simulates the race);
  - the resume hook is not called.
- [ ] `TestCancellingAPendingTaskClosesItsPause` (cmd/aura `db_integration`): cancelling a pending `agent_job` through the task tool leaves no pending pause for it, and the approvals list no longer shows it.
- [ ] Add a `TestMain` with `goleak.VerifyTestMain` to `internal/messagedrafts` (it has none; `internal/runner`, `internal/cron` and `internal/cron/handlers` already have one), since Tasks 6 and 14 start lock-holding work there.
- [ ] Red, `make sqlc`, implement, green with race on `./internal/cron/`, `./internal/runner/` and `./internal/messagedrafts/`, and with `db_integration` on `./internal/cron/`, `./internal/runner/`, `./internal/messagedrafts/` and `./cmd/aura/`. Commit: `feat(scheduler): a one-shot not approved in time expires and the operator is told`.

### Task 15: The new critical files are measured by CI's mutation gate

Decision 41. Lands after Task 14, before the live run.

**Files:** `scripts/critical_mutation_gate.py` (`GO_SCOPES`, `REQUIRED_SCOPE_IDS`), `scripts/critical_mutation_gate_test.py`, `.github/workflows/ci.yml` (the `go-mutation` matrix)

The new scopes:

| Scope id | File |
|---|---|
| `task_approval` | `internal/agent/tools/task_approval.go` |
| `task_message` | `internal/agent/tools/task_message.go` |
| `message_fire` | `internal/cron/handlers/message.go` |
| `lapsed_approval` | `internal/cron/deliver_lapsed.go` |

- [ ] Add a case to `critical_mutation_gate_test.py` that requires the four scope ids, and see it fail first.
- [ ] Add them to `GO_SCOPES` and `REQUIRED_SCOPE_IDS`, and to a `go-mutation` matrix group: a new group `scheduled-delivery` with `scopes: task_approval task_message message_fire lapsed_approval`, its timeout set from one local measurement plus half again.
- [ ] Measure `internal/messagedrafts/store_scheduled.go` in WSL, which CI's mutation runners cannot do without Postgres: `GOFLAGS=-tags=db_integration go-mutesting ./internal/messagedrafts/store_scheduled.go` with the `aura_cov` DSNs of Global Constraints. Keep the score and the killed/total counts for Task 16's verification file. Below 70% killed, add the tests that kill the survivors before moving on.
- [ ] `python3 -m pytest scripts/critical_mutation_gate_test.py` green. Push and check the CI gate measures all four scopes at 70% or more. Commit: `ci(mutation): measure the scheduled delivery's critical files`.

### Task 16: Live, on the lab VM

- [ ] Run the gates in WSL: `make quality`, then `bash scripts/coverage_docker.sh`, which uses the disposable `aura_cov` database. Coverage is the combined figure, at least 85% across the tag matrix, and the package policy holds. Never `make quality-full` against the live `aura` database.
- [ ] Dry run with the model in the loop (`aura-model-in-the-loop` skill), on its strict profile and its disposable database, before the operator: scenarios 1, 3, 7 and 8 below with a parked model. Record the outcomes; fix anything that fails before the push.
- [ ] Push with the WSL recipe in `CLAUDE.md`. Wait until every workflow is green on the SHA, the mutation gate of Task 15 included. The operator then updates the VM from the cockpit.
- [ ] The operator drives the spec's Verification through the cockpit and Telegram, with a contact the operator controls:
  1. With the default set to Telegram, "remind me in 10 minutes to X" asks nothing, names the default, and arrives on Telegram.
  2. With no default, the choice is asked, and the reply mentions the Profile panel.
  3. A cockpit `message` shows one card with the recipient, the text and the time. After approval it is sent once, and the outcome reaches the default channel.
  4. On Telegram the read-only card appears. Approva sends once at the time. Rifiuta, and silence past the time, send nothing, and silence reports "not approved in time".
  5. Declining the card, and cancelling an approved task before its time, both send nothing.
  6. An `agent_job` asked to write to someone is steered to `kind=message`, or reported "not sent" with the reason.
  7. Verification item 6. An `agent_job` is scheduled from the cockpit, and another from Telegram. Each shows its approval in that chat, in the same turn, with no `ask_user` call in the model's output. The question names the kind, the time in the operator's zone and the goal. Approving one makes the model confirm it, and it then fires. Declining the other cancels it.
  8. Verification item 7. A one-shot `agent_job` is scheduled about five minutes ahead and left unapproved. After its time:
     - the notice `agent_job for <time> was not approved in time and did not run.` arrives on its notify route;
     - the task leaves the scheduler board's awaiting-approval list;
     - its pause leaves the cockpit's approvals.

     Also record what the first pass after deploy did to the tasks already stuck, the 2026-11-02 one included: counts and kinds only. Each operator got the notice.
- [ ] Score the run with this rubric. Each scenario is pass or fail, judged on its named evidence:

  | Scenario | Passes when |
  |---|---|
  | 1, 2 | the task row's `notify_route` and the reply text match the scenario |
  | 3, 4 | the draft ends `sent` and the trusted tool's invocation count for it is exactly 1; for Rifiuta or silence the draft ends `declined` or `expired` and the count is 0 |
  | 5 | the draft ends `declined` and the invocation count is 0 |
  | 6 | the run summary holds the steering or `outboundRefusedLine`, and the invocation count is 0 |
  | 7 | one in-turn approval pause per chat, no `ask_user` tool call in that turn, the approved task fires once, the declined one is `cancelled` |
  | 8 | the task is `cancelled`, one `failed` run carries the notice, the notice was delivered, and no pause stays pending |

  The score is passes ÷ 8 × 10. Any scenario where a message went out twice fails the whole run, whatever the score. The Definition of Done needs more than 9.8, which means all eight.
  For each scenario, record the run ledger rows, the draft statuses (counts and states only) and the tool-invocation counts. A host-parked call has no tool-invocation row of its own (decision 30), so scenarios 3, 4 and 7 count the dispatch's invocation, not the parked call's.
- [ ] Write `docs/superpowers/verification/<run date, YYYY-MM-DD>-scheduled-delivery-and-messages-live.md`, named for the day the operator's run happens. It holds:
  - the rubric's result per scenario and the score;
  - the model-in-the-loop dry run's outcome;
  - the mutation scores: the four CI scopes from the gate's report, and `store_scheduled.go` from Task 15's WSL measurement;
  - what the run does not prove: email at volume, more than one WhatsApp mount, a daemon down across the grace period, `aura task cancel` leaving a pending pause, and that the mutation scores cover these five files only;
  - no contact, number or message text.
- [ ] Only then, the PRD amendment, numbered at landing (the next free number after #212 today; re-read `prd.md` first), in `prd.md` §15 "Scheduling, delegation and asynchronous delivery". It records the measurement and cites that file: the default channel, the `message` kind, the 24 h − 15 m horizon, approval in the creating chat with the relay retired, and the expiry of lapsed one-shot approvals. It also states what the measurement does not prove, the same list as the verification file. Commit both docs.
