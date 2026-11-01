# Mail and calendar watch, backend — Implementation Plan

**Status (2026-11-01):** Tasks 1–14 are built and verified on branch `mail-watch`, in the worktree
`D:\tmp\aura-mail-watch-wt`, off `master` at `feeca6797`. Nothing is pushed or merged: the operator has forbidden
pushes. Task 15, the React cockpit, is not built. Task 16, the lab-VM run (G3), needs the published PIM image and is
owed.

> **The planner built this code, RED then GREEN, in a scratch copy, and the verified commits are the implementation.**
> There is no transcription pass. Each task names the commit that carries it. To land it, merge the branch once the
> operator allows it. Function bodies live on the branch, not in this document.

**Goal:**
- A person can turn a watch on for a PIM account and hear about what is urgent within minutes, with no model on the
  path, and read everything else once a day.
- The agent sees what the person sees, so "aprila" works on the next turn. It sees it framed as a stranger's text.
- A correction teaches: the second one for a sender becomes a rule.
- No table holds a subject, a body or a sender on a verdict.

**Architecture:**
- **`internal/pimwatch`.** A new package with no dependency on `agent` or `cron`. The ladder is pure (`ladder.go`),
  the PIM client takes a token source (`client.go`), the store runs under the fail-closed RLS pair
  (`store*.go`), `text.go` renders in the identity's language, `watcher.go` and `digest.go` are the two runs,
  `service*.go` is what the cockpit and the tool share, and `sweep.go` expires verdicts.
- **Handlers.** `internal/cron/handlers/pim_watch.go` adds no judgment. It carries the run's text to the dispatcher
  and frames the record.
- **Dispatcher.** `Job.Tell(notice, record)` lets one run say different things to the notification, the record and
  the ledger. A handler that never calls it behaves as it always did.
- **Composition.** `cmd/aura/serve_pimwatch.go` holds every adapter: scheduler, run ledger, profile, Telegram link
  and OAuth grant.

**Tech Stack:** Go 1.27, Postgres with sqlc and golang-migrate, the AG-UI HTTP server, the cron dispatcher.

**Spec:** `docs/superpowers/specs/2026-10-31-mail-calendar-watch-design.md`, §8 steps 3 and 4. There is no classifier
and no learned model here: rung 5 of the ladder does not exist in code, and nothing pretends it does. Decider, Laya
and Jev are out of scope.

**Contract:** spec §5.1, as fixed in `internal/pimwatch/testdata/pim_contract.json`. The fork that implements it was
built in parallel and not read. The tag `pim_contract` holds a real sidecar to that file.

## Global Constraints

- **Migration 0149**, the slot `ls internal/db/migrations/ | tail -1` gave. All six tables have an identity foreign key
  with `ON DELETE CASCADE`, the fail-closed RLS pair, and the 0141 GRANTs.
- **No new env var.** Everything is a per-identity setting, read at use. `AURA_PIM_MCP_URL` already existed.
- **Files stay ≤ 600 LOC.** `service.go` reached 597 and was split into `service_rules.go`.
- **No model, no paid call, no real mailbox, no real Telegram, nothing on the lab VM** in any step of this plan.
- **Database work runs on the disposable container** `aura-postgres-mailwatch`, never the live `aura` database.
- **No test changes to make it pass**, except the ones the change is about: the head-migration pin, the keyword
  inventory, and the route sweep gaining the new routes.

## Review Focus

Each of these would bite a real person, and each has a test that names it.

1. **A run dying between committing and notifying must not lose or double an alert.**
   `TestARunThatDiedBeforeCompletingIsToldByTheNextAndOnlyOnce`, `TestACompletedRunIsNotToldAgain`,
   `TestAReplayedPageTellsNobody`.
2. **A backlog stays inside the handler's bound, and the cursor passes only what was handled.**
   `TestABacklogStopsShortOfTheDeadlineAndTheCursorCoversOnlyWhatWasHandled`,
   `TestACommitThatFailsLeavesTheCursorForTheNextTick`, `TestALongMailBacklogLeavesTheCalendarItsTurn`.
3. **`resetRequired` must never read as "nothing new".** `TestResetRequiredRaisesTheGapAlarmOnce`,
   `TestAGapAlarmOfADeadRunIsToldAgain`.
4. **A burst of 100 mails is one notification.** `TestABurstOfAHundredIsOneNotificationAndTheCapHoldsAllDay`,
   `TestATickThatOnlyHeldSomethingBackSaysNothing`.
5. **A subject carrying a forged `</tool_output>` or markdown stays a stranger's text.**
   `TestAForgedSubjectCannotAddALineOrReachTheTrustedPartOfTheRecord`,
   `TestTheRecordFramesWhatTheMailSaidAndTheNoticeDoesNot`, `TestASubjectCannotEscape*`,
   `TestPlainLineCannotForgeALineOrHideItsOrder`.

## Decisions taken while writing this plan

- **Commit before notify, and an alert stays owed.** §5.2 says commit before the handler returns, and the dispatcher
  notifies after it, so the two cannot be one step. A verdict names its `alert_run_id`, and `notified` turns true on
  the next tick once the ledger shows that run `completed`. `d.complete` runs before `d.notify`, so a run that died
  before completing told nobody: the next run reads the mail live and tells it. A completed one is not told twice.
  **The gap that remains** is a crash between `complete` and `notify`. That push is lost, and the digest lists the mail
  the same day, which is what §5.2's "listing `now` again is deliberate" is for.
- **The same mechanism carries alarms,** through `alarm_run_id` on the cursor row.
- **The insert-or-ignore on the verdict's primary key** is what makes a replayed page tell nobody.
- **A hard failure** answers `SuppressSchedulerNotification() true`, stays on the cursor row for the card, and pauses
  through the existing counter. If auto-pause is off and an hour passes, it answers false once, so the person still
  finds out. **With alerts in hand** a hard failure tells them and completes, because the dispatcher drops the notice
  of a failed run. The next tick fails on its own.
- **Transient failures complete the run.** The next tick is the retry, and `scheduleRetry` would stretch three of them
  into 15 minutes. One alarm after an hour, once per episode, and a success clears it.
- **Page budget.** `pimRequestTimeout` is 60 seconds and `PageLimit` stays 50: the fork says 50 mails stay under half a
  minute, and at a 30-second timeout a worst-case page would count as transient and be asked again unchanged for ever.
  A run stops starting pages when `PageReserve`, the timeout plus 5 seconds, no longer fits before the handler's
  deadline, and a resource gets 20 pages. `pim_watch` has 2 minutes and does not reschedule on recovery; `pim_digest`
  has 5 minutes and does. So a run always gets its first page, and `TestAWatchRunHasRoomForAPageAtItsWorst` pins the
  relation. **What that bound does not give** is a second resource after a 60-second page: at 60 + 65 > 120 the calendar
  waits for the next tick for as long as the mail stays that slow. The mail's alarm tells the person after the hour, and
  nothing is lost, because the calendar's cursor waits too. A bound of 150 seconds would give each resource its page.
- **A two-minute run turns `/readyz` to 503 for up to a minute.** `tick` blocks on its runs and `markTick` comes after
  them, and the window is `max(3 × tick, 90s)`, so 90 seconds by default. This holds for every long run, `agent_job` and
  the backups included, and compose's healthcheck needs three failures 30 seconds apart.
- **Only `added` mail is triaged.** Replays are ignored.
- **Calendar.** Rules do not apply to events. An event the person organized is `later`, because they made the change.
  An `updated` event with no row is `today`: nothing shows it moved. A cancelled one is the exception, because the
  status is the fact: it is `now` inside 24 hours, as spec §5.3 says, and `organizerIsSelf` still wins. A `deleted`
  change with no row says nothing and leaves no row: the person may have deleted it themselves. **On Google that also
  covers an organizer's cancellation the watch never saw,** because the fork writes a cancelled Google event as
  `deleted`, with its `event` when it has a start, and Graph as `updated`. So the same cancelled meeting with no row is
  `now` on Microsoft and silent on Google. With a row both are told. `deleted` cannot tell who deleted it, so G3 is what
  can say whether to read `event.status` there too.
- **`from` is the address and `fromName` the name beside it,** on `changes` and on mail `items`. Rules, corrections and
  `SenderKey` read the address. A notice, a digest line and the record show `Name <address>`, inside the same frame,
  and the name is bounded at 60 runes so a long one cannot push the address out. An event's `from` is the organizer's
  address, with no name.
- **An event's time on `/admin/items` is `event.start`.** `occurredAt` there is when it was last modified, and the
  client read that as the start.
- **An all-day event's start is a bare date,** which the client refused, so one such event would have failed its page
  on every tick. `EventTime` reads both forms, and null. The ladder takes the date at the person's midnight, and a line
  shows the date alone. Any other form still fails the page, and the hour's alarm says so.
- **Failure classes.** A named code wins: `provider_transient` is transient, `unsupported_resource` is no failure, and
  `provider_auth`, `permission_denied`, `account_not_found`, `invalid_cursor`, `invalid_account`, `invalid_resource`,
  `invalid_items` and `too_many_items` are hard. Unnamed, a 5xx is transient and anything else is hard, except 429, which
  §5.6 makes transient. So a bare 500 from the PIM, a bug or an exception it did not recognise, retries on the next
  tick and reaches the hour's alarm. An `account_not_found` stays on the cursor row and is named with its account in
  the pausing run's one report. `invalid_cursor` pauses too, and resuming cannot clear it: disable and enable can.
- **Unknown stays unknown.** A nil `writtenTo` neither raises nor files, unless the mail is bulk.
- **A spent cap is silent.** A tick that alerts nobody and only held mail back says nothing. The count appears only
  beside real alerts, because a count on every tick is the notification the cap exists to stop.
- **The digest sorts before it reads,** so a large window costs one batch of 20, and the rest is a count.
- **A digest that names an unreadable account does not move the window.** Tomorrow repeats what it did read. A repeated
  line costs less than a missing one. An empty window sends nothing and still moves.
- **A crash between moving the window and notifying loses one digest.** This is accepted, unlike the alert's case: the
  same mail is in the decisions view, and a ledger-keyed window for a daily job would be two more columns for a
  millisecond gap.
- **Retention runs on the retention sweep,** not inside `pim_digest`. The tables are fail-closed under `aura_app`, and
  `assets.DeleteSweep` proves that walking the identity roster and binding each identity works. Default 30 days.
  Corrections do not expire: they are the feed, and they go when the account goes.
- **Default digest hour 18.**
- **The language rule:** a profile language starting with `it` writes Italian, everything else English.
- **The default labels go in once in an identity's life** (`labels_seeded`). A person who removed them all does not get
  them back on the next enable.
- **Where a rule's learning lands:** two corrections must agree, and the earlier one must be outside the 15-minute
  window. Tier and label are learned apart. A correction clearing a label teaches nothing, and a calendar item teaches
  nothing. A sender that cannot be read changes the verdict and teaches nothing.
- **A correction's sender is read live by id**, because no verdict keeps one.
- **`disable` forgets the cursor and keeps the verdicts,** because the day's digest still lists them. Removing the
  account forgets everything but the sender rules, which are the person's words about senders.
- **One live task per account and one digest per identity is a database fact,** two partial unique indexes. A lost race
  to a second click reads as done.
- **The route is fixed at enable.** A person who links Telegram later turns the watch off and on.
- **A watch cancelled through the `task` tool or the cockpit's scheduler board** leaves the digest running, because
  only `Disable` and `RemoveAccount` stop it. The digest then says nothing once its window empties. Enable starts the
  cursor again.
- **A scheduler edit keeps a watch's payload:** the board omits it for these kinds, and an omitted payload stays. A hand
  written `PATCH` that drops `account_id` fails the run with a clear error, which pauses the task and says so.
- **A paused digest cannot be rescheduled** (`UpdateTask` refuses it), so it takes a new hour from the next enable.
- **`homeTask` restores a Telegram thread rather than moving off it.** `/clear` hard-deletes the row, and a new
  conversation would be one the person's next message never reaches. The seam is optional, and only these two kinds on
  that route use it.
- **The thread's conversation row is created at enable,** or the first run would read a missing row as a deleted origin.
- **Deactivating an identity pauses its tasks.** `DeprovisionDeps.Jobs` was nil on every path. `scheduledTaskAdapter`
  now sits behind it, for every kind and not only the watch's. `cron.Store.PauseIdentityTasks` moves the identity's
  `active` tasks to `paused` with the reason `identity_deactivated`, in one statement, and a second call finds nothing
  to change. A `pending_approval` task stays as it is, because a paused task resumes active and that would skip its
  approval. A task already paused keeps its own reason. **A backup stays active:** the task tool and the CLI let a
  person schedule `backup_postgres` under their own identity, `IsPausableKind` keeps a backup from every pause, and the
  operator decided on 2026-10-05 that one is never stopped from the cockpit or the agent. There is no way back:
  nothing clears `deactivated_at`.
- **Purging an identity deletes its tasks,** in a journaled step, `scheduled_tasks`, behind the port
  `ScheduledTaskPurger`. `scheduler_tasks.identity_id` is text with no foreign key, so the identity's row does not reach
  them. They would keep firing, and their notifications, whose bodies can hold a sender and a subject, would stay. ON
  DELETE CASCADE takes the runs and the notifications, and `TestPurgeRemovesAnIdentitysTasksRunsAndNotifications`
  proves it as `aura_app`. The step runs after the OpenRouter key and before the box, because a task firing after the
  box is gone would wake another. It needs only the id, so `aura identity purge --resume-resources` runs it. Unlike the
  pause it covers every kind and status: a task left behind names an identity that no longer exists. That includes a
  person's own backup task, and the boot seed puts a backup back if it was the only one.
- **A run already in progress still finishes,** through both. `TestARunEndingAfterDeactivationLeavesThePause` pins that
  the pause and its reason survive it. On a purge its rows go first, so by the code `CompleteRun` updates nothing and
  the outcome write logs a warning. That path has no test.
- **`add_rule` and `correct` need a person in the conversation.** A rule filing a sender to `later` hides that person's
  mail from the digest, so an instruction injected into a headless job could silence a sender with nobody seeing it.
  The tier stays Normal, because Destructive would prompt, and "di queste non avvisarmi più" stays one sentence.
  `personActions` in `internal/gateway/decide.go` refuses the two verbs with guidance and a `needs_person` deny fact
  when `personPresent` is false. That reads the responder and the delegated-dispatch marker, because a synchronous
  swarm worker inherits the responder from the turn that spawned it and has nobody on it. It does not read
  `ProfileServerProduction`, because no approval is asked for, so a live chat works on both strict profiles. Like every
  gate here it is a no-op on `dev` and `local_trusted`, `disable` and `remove_rule` included.
- **`enable` stays free:** Normal, no prompt, reserved and recorded, in a live chat and in a headless run alike. It only
  adds what the person sees, and a wrong account fails at the PIM. `status`, `list_rules` and `list_labels` are Safe.
- **The `mail_watch` API sits behind `governance.write`,** as the Connect routes around it on the account card do, with
  one subtree mount, so a route added later is gated without anyone remembering.
- **The tool takes a label's name** and the API takes its id, because each is what its caller holds.
- **`pim_watch` has no model budget.** The ladder is pure. `StepBudget` stays empty.

## File structure

| File | Change |
|---|---|
| `prd.md`, `docs/teable-behaviours-2026-10-30.md`, `docs/superpowers/specs/2026-10-09-consolidation-best-of-design.md` | The amendments, with what the measures do not prove. |
| `internal/db/migrations/0149_pim_watch.{up,down}.sql` | Kinds, the `identity_deactivated` reason, six tables, RLS, the two partial unique indexes. |
| `internal/db/queries/pim_watch.sql`, `scheduler_tasks.sql`, `internal/db/sqlc/*` | Queries and generated code. |
| `internal/cron/store.go`, `store_manage.go`, `pause.go` | The two kinds, `ListLiveTasksOfKind`, `PauseIdentityTasks`, `DeleteIdentityTasks`. |
| `internal/agui/deprovision.go`, `saga_journal.go`, `cmd/aura/serve_deprovision_purgers.go` | `ScheduledTaskPurger`, the `scheduled_tasks` step, `scheduledTaskAdapter`. |
| `internal/cron/dispatch.go`, `dispatch_tell.go`, `notify.go` | `Job.Tell`, `pausedRunError`, no stdout copy of a Telegram push. |
| `internal/cron/task_home.go` | A watch keeps a conversation; a Telegram thread comes back. |
| `internal/cron/handlers/handler.go`, `pim_watch.go` | The kinds' constants, `Job.Tell`, the two handlers. |
| `internal/agent/trust.go`, `internal/agui/server_display*.go` | The record's frame reaches the cockpit as plain text. |
| `internal/channels/telegram/deliver.go`, `agui_subscriber.go` | No link preview on a push; `ThreadConversationID`. |
| `internal/pimwatch/*` | New: the package. |
| `internal/agent/tools/mail_watch.go`, `search_keywords.go` | New: the deferred tool and its retrieval words. |
| `internal/gateway/classify.go` | `mail_watch` in `multiplexedClassifiers`. |
| `internal/agui/pim_watch_api.go`, `connect_pim_api.go`, `idempotency_http.go`, `server.go` | New routes; the account delete hook; the mutation inventory. |
| `cmd/aura/serve_pimwatch.go` | New: the composition. |
| `cmd/aura/serve_dispatch.go`, `serve_agui.go`, `serve.go`, `serve_provisioning.go`, `serve_webui*.go`, `chat_boot.go`, `main.go`, `runtime_tool_handles.go` | Wiring. |
| `scripts/coverage_package_policy.json` | `internal/pimwatch` at the exact 85% target. |

## API for the React task

Every route is under `/api/mail-watch`, behind the session and `governance.write`. Every mutation needs an
`Idempotency-Key` header, as the rest of the cockpit's mutations do. Times are RFC 3339, null when they never happened.

| Route | Body | Answer |
|---|---|---|
| `GET /api/mail-watch` | | `{configured, reason?, settings, accounts: [...]}` |
| `PUT /api/mail-watch/settings` | `{digest_hour, daily_alert_cap}` | `settings` |
| `POST /api/mail-watch/accounts/{id}/enable` | | `{enabled: true}` |
| `POST /api/mail-watch/accounts/{id}/disable` | | `{enabled: false}` |
| `GET /api/mail-watch/accounts/{id}/decisions?day=YYYY-MM-DD` | | `[decision]`, the identity's local day, today when empty |
| `POST /api/mail-watch/accounts/{id}/decisions/correct` | `{resource?, item_id, tier?, label_id?}` | `{tier, label_id?, rule?}` |
| `GET /api/mail-watch/rules` | | `[rule]` |
| `POST /api/mail-watch/rules` | `{key, tier?, label_id?}` | `rule` |
| `DELETE /api/mail-watch/rules/{id}` | | `{removed: true}` |
| `GET /api/mail-watch/labels` | | `[label]` |
| `POST /api/mail-watch/labels` | `{name, tier?}` | `label` |
| `PUT /api/mail-watch/labels/{id}` | `{name, tier?}` | `label` |
| `DELETE /api/mail-watch/labels/{id}` | | `{removed: true}` |

- `settings`: `{digest_hour, daily_alert_cap, interval_minutes, last_digest_at}`.
- `account`: `{account_id, paused, paused_reason?, next_run_at, alerts_today, resources: [{resource, status,
  last_error?, failing_since, advanced_at}]}`. `status` is `ok`, `failing` or `unsupported`. `last_error` is a fixed
  code, such as `provider_auth`, for the UI to translate: the named ones are in Decisions, *Failure classes*, and an
  unnamed one is `pim_http_<status>`. `paused_reason` is `operator`, `failures` or `identity_deactivated`.
- `decision`: `{account_id, resource, item_id, calendar_id?, from, from_name, subject, start, all_day?, gone,
  auth_failed, tier, label_id?, label?, decided_by, decided_at}`. `from` is the address and `from_name` the name beside
  it, empty when the provider gave none. `from`, `from_name` and `subject` are a stranger's text: render them as text.
  With `all_day`, `start` is the date at midnight UTC: show the date, in no zone. `gone` means the item no longer
  exists, and all three are empty. `decided_by` is `rule`, `auth_failed`, `people`, `category`, `default`, `calendar`
  or `person`.
- `correct`: leaving out `label_id` keeps the label, and `""` clears it. `resource` defaults to `mail`. A `rule` in the
  answer means this correction taught one, which the UI should say.
- `rule`: `{id, key, key_kind, tier?, label_id?, source, created_at}`. `key_kind` is `sender` or `domain`, `source` is
  `person` or `correction`. `key` takes a whole address, or a domain with or without its `@`.
- `label`: `{id, name, tier?, position}`.
- Errors: `503 {error: "mail_watch_unavailable", reason?}`, `400 {error: <what to fix>}`, `404 {error: "not_found"}`,
  `502 {error: <fixed PIM code>}`, `500 {error: "mail_watch_failed"}`.

The switch sits on the account's card, and the list of accounts stays `GET /api/connect/pim/accounts`.
`GET /api/mail-watch` says which of them are watched. Removing an account in the wizard already removes its watch.

## Tasks

### Task 1 — The PRD records what the design measured

- [x] **Files:** `prd.md` §11 and §15, `docs/teable-behaviours-2026-10-30.md`, the consolidation inventory.
- **Commit:** `2f5e9defe`. Documentation, nothing to run.

### Task 2 — Kinds, tables and queries

- [x] **Files:** migration 0149, `internal/db/queries/pim_watch.sql`, `scheduler_tasks.sql`, generated code,
  `internal/cron/store.go`, `store_manage.go`, `internal/agent/display/native_lists.go`.
- **Interfaces:** `cron.KindPIMWatch`, `cron.KindPIMDigest`, `(*Store).ListLiveTasksOfKind`.
- **Tests:** `TestMigrationHeadMatchesEmbeddedCatalog` (pin moved to 149), the kind lists, `TestReset_DownUpRoundTrip`,
  `TestRLS*`, on the disposable database.
- **Commit:** `2ba312e47`.

### Task 3 — One run says different things to different readers

- [x] **Files:** `internal/cron/dispatch.go`, `dispatch_tell.go`, `notify.go`, `internal/cron/handlers/handler.go`,
  `cmd/aura/serve_dispatch.go`.
- **Interfaces:** `Job.Tell func(notice, record string)`, `pausedRunError`.
- **Tests:** `internal/cron/dispatch_tell_test.go`, `notify_route_test.go`. No existing test changed.
- **Commit:** `56165ca77`.

### Task 4 — The record reaches the cockpit plain and Telegram plain

- [x] **Files:** `internal/agent/trust.go`, `internal/agui/server_display.go`, `server_display_watch.go`,
  `internal/channels/telegram/deliver.go`, `agui_subscriber.go`.
- **Interfaces:** `agent.WatchRecordSource`, `agent.UnescapePromptText`, `telegram.ThreadConversationID`.
- **Tests:** `TestTheCockpitShows*`, `TestASubjectCannotEscape*`, `TestAPushIsPlainText`.
- **Commit:** `ebdcf28db`.

### Task 5 — The ladder

- [x] **Files:** `internal/pimwatch/ladder.go`, `ladder_test.go`.
- **Interfaces:** `DecideMail(book, from, signals)`, `DecideEvent(now, facts)`, `NewBook`, `SenderKey`.
- **Tests:** tables, an exhaustive walk of 256 signal combinations, and `testing/quick`.
- **Commit:** `5518e819a`.

### Task 6 — The PIM client and its contract

- [x] **Files:** `internal/pimwatch/client.go`, `client_test.go`, `contract_live_test.go`,
  `testdata/pim_contract.json`.
- **Interfaces:** `NewClient`, `(*Client).Changes`, `(*Client).Items`, `TokenSource`, `PIMError`, `ErrNotAuthorized`.
- **Tests:** the fake PIM replays the contract file over `httptest`. The `pim_contract` tag replays it against a real
  sidecar, given `AURA_PIM_CONTRACT_URL`, `_TOKEN` and `_ACCOUNT`.
- **Commit:** `d429c4b8c`.

### Task 7 — State, and the tick

- [x] **Files:** `internal/pimwatch/store.go`, `store_cursor.go`, `store_verdicts.go`, `text.go`, `watcher.go`, their
  tests, the regenerated queries, migration 0149's additions.
- **Interfaces:** `Store`, `(*Store).CommitPage`, `Watcher.Tick`, `StartResource`, `HardFailure`, `Outcome`, `Line`.
- **Tests:** Review Focus 1 to 4 on the disposable database with the race detector, RLS with a second identity and with
  no identity (`TestAnotherIdentitySeesNothingOfAWatch`, `TestAConnectionWithNoIdentitySeesNoRowOfAnyTable`).
- **Commit:** `7e863a3a0`.

### Task 8 — The digest, retention and the handlers

- [x] **Files:** `internal/pimwatch/digest.go`, `sweep.go`, `internal/cron/handlers/pim_watch.go`, their tests.
- **Interfaces:** `Digester.Run`, `Sweep`, `handlers.PIMWatchHandler`, `handlers.PIMDigestHandler`.
- **Tests:** `digest_integration_test.go`, `digest_test.go`, `pim_watch_test.go`.
- **Commit:** `5b8dbe965`.

### Task 9 — The service

- [x] **Files:** `internal/pimwatch/service.go`, `service_integration_test.go`.
- **Interfaces:** `Service`, `Tasks`, `Routes`, `Delivery`, `WatchSpec`, `DigestSpec`, `TaskPayload`.
- **Commit:** `7e1c4e3c4`.

### Task 10 — The `mail_watch` tool

- [x] **Files:** `internal/agent/tools/mail_watch.go`, `search_keywords.go`, `internal/gateway/classify.go`,
  `decide.go`, `cmd/aura/main.go`, `runtime_tool_handles.go`.
- **Interfaces:** `tools.MailWatchTool`, `mailWatchFixedTiers`, `personActions`, `personPresent`.
- **Tests:** `mail_watch_test.go`, `decide_mail_watch_test.go`, including
  `TestAHeadlessRunCannotTurnTheWatchOffOrRemoveARule`, `TestAHeadlessRunCannotTeachTheWatch`,
  `TestALiveChatTeachesTheWatchWithNoPrompt`, `TestASwarmWorkerInALiveChatCannotTeachTheWatch`, and the classify table's
  schema check.
- **Commit:** `e80bc7346`, then the commit `fix(gateway): teaching the mail watch needs a person`.

### Task 11 — A watch keeps a conversation

- [x] **Files:** `internal/cron/task_home.go`, `task_home_test.go`.
- **Interfaces:** `TaskThreadRestorer`.
- **Commit:** `e62f4d08d`.

### Task 12 — What review of the tick and the service found

- [x] **A spent cap says nothing.** A tick that only held mail back used to push a count of its own, which is the
  notification the cap exists to stop. **Files:** `internal/pimwatch/text.go`, `text_test.go`,
  `watcher_integration_test.go`. **Commit:** `3b92f1617`.
- [x] **A watch runs in the identity's zone, and enable wakes a paused digest.** Quiet hours read the task's zone, and
  `UpdateTask` refuses a paused row. `service.go` splits into `service_rules.go`. **Commit:** `0a1aa5c0c`.

### Task 13 — The cockpit API

- [x] **Files:** `internal/agui/pim_watch_api.go`, `connect_pim_api.go`, `idempotency_http.go`, `server.go`,
  `cmd/aura/serve_webui.go`, `serve_webui_routes.go`.
- **Tests:** `pim_watch_api_test.go`, `TestRemovingAnAccountForgetsItsWatchOnlyWhenItLeft`, the governance sweep,
  `TestEveryRegisteredUnsafeHTTPRouteIsClassified`, `TestServeWebuiMountsEveryAGUIRoute`.
- **Commit:** `832e05cf4`.

### Task 14 — Composition and coverage

- [x] **Files:** `cmd/aura/serve_pimwatch.go`, `serve_dispatch.go`, `serve_agui.go`, `serve.go`,
  `serve_provisioning.go`, `chat_boot.go`, `scripts/coverage_package_policy.json`.
- **Tests:** `cmd/aura/serve_pimwatch_test.go`, including `TestServeWebuiMailWatchRoutesAreGated`, and
  `internal/cron/pim_watch_tasks_integration_test.go`.
- **Commit:** `f4636b4cc`.

### Task 14b — De-provisioning stops and removes an identity's tasks

- [x] **Files:** `internal/db/migrations/0149_pim_watch.{up,down}.sql`, `internal/db/queries/scheduler_tasks.sql`,
  `internal/cron/pause.go`, `store_manage.go`, `internal/agui/deprovision.go`, `saga_journal.go`,
  `cmd/aura/serve_deprovision_purgers.go`, `serve_provisioning.go`. `pimWatchJobs` and `Service.StopIdentity` are gone.
- **Interfaces:** `cron.PausedByIdentityDeactivated`, `Store.PauseIdentityTasks`, `Store.DeleteIdentityTasks`,
  `agui.ScheduledTaskPurger`, `DeprovisionDeps.ScheduledTasks`.
- **Tests:** `internal/cron/identity_tasks_integration_test.go`, `identity_tasks_test.go`,
  `internal/agui/deprovision_tasks_test.go`, `cmd/aura/serve_deprovision_tasks_integration_test.go`,
  `TestDeprovisionDepsReachTheScheduledTasks`.
- **Commit:** `66ddc7e5f`.

### Task 14c — Reconciled with the finished fork

- [x] Read on the fork's branch `aura-watch` at `5f182c7`, and no real sidecar answered. **Files:**
  `internal/pimwatch/client.go`, `watcher.go`, `ladder.go`, `text.go`, `testdata/pim_contract.json`,
  `internal/agui/pim_watch_api.go`.
- **Mismatches found beyond the brief:** an all-day `event.start` did not decode, and `Item` read an event's start from
  `occurredAt`. See Decisions.
- **Interfaces:** `EventTime`, `EventCancelled`, `PageReserve`, `Change.FromName`, `Item.FromName`, `Item.Event`,
  `Line.FromName`, `Line.AllDay`, `EventFacts.Cancelled`.
- **Tests:** `fork_shapes_test.go`, `fork_integration_test.go`, the new rows of `TestDecideEvent`, the new `text_test.go`
  cases, `TestMailWatchDecisionsCarryTheNameAndAnAllDayDate`, `TestAWatchRunHasRoomForAPageAtItsWorst`.
- **Commit:** the commit `fix(pimwatch): read what the finished fork writes`.

### Task 15 — The React cockpit (not built)

- [ ] The switch on the account's card, with `reason` when `configured` is false.
- [ ] The decisions view: the day's lines from `decisions`, a control for tier and label, and a word when a correction
  taught a rule.
- [ ] Rules and labels: list, add, remove, rename.
- [ ] Digest hour and daily cap.
- [ ] Each account's state from `resources`, with its fixed codes translated.
- [ ] A readable name for `pim_watch` and `pim_digest` on the scheduler board, whose payload field stays hidden.
- **Consumes:** the API above. Sender and subject render as text. The `i18n` keys go through `react-i18next`, the
  palette stays blue, and vitest and Stryker apply.

### Task 16 — Close: the lab VM (G3), owed

This needs the published PIM image with `POST /admin/changes` and `POST /admin/items`, and it must not be claimed
before it runs.

- [ ] `go test -tags pim_contract` against the real sidecar. Any difference from `pim_contract.json` goes to the spec
  first.
- [ ] Enable a watch on a real account through the cockpit, and through `mail_watch` on a real turn.
- [ ] A `now` mail reaches Telegram within a tick, plain, with no preview, and "aprila" works on the next turn.
- [ ] A digest fires at its hour, in the identity's language.
- [ ] Kill the process mid-tick, and see the alert told once.
- [ ] Revoke the grant, and see the pause reported once.
- [ ] A dated file in `docs/superpowers/verification/`, saying what it measured and what it does not prove.
- [ ] Merge into `master` and push, when the operator lifts the ban.

## Verification at the branch's close

All in WSL at `f4636b4cc`, 2026-11-01, on the disposable container for the tagged tiers.

| Command | Result |
|---|---|
| `go build ./...` | clean |
| `go vet ./...` | clean |
| `go vet -tags db_integration ./internal/pimwatch/ ./internal/cron/... ./cmd/aura/` | clean |
| `go vet -tags pim_contract ./internal/pimwatch/` | clean |
| `go test -count=1 -race ./internal/cron/... ./internal/gateway/ ./internal/agent/tools/ ./internal/agent/display/ ./internal/channels/telegram/ ./internal/pimwatch/` | ok |
| `go test -count=1 ./internal/agui/ ./internal/agent/ ./cmd/aura/` | ok |
| `go test -count=1 -race -tags db_integration -p 1 -coverpkg=./internal/pimwatch/ ./internal/pimwatch/` | ok, 89.9% of statements |
| `go test -count=1 -tags db_integration -p 1 ./internal/cron/` | ok |
| `go test -count=1 -tags db_integration -p 1 ./internal/db/` | ok, run at the composition's tree before the last two lint fixes, which touch neither the schema nor the package |

Re-run on the tree of Task 14c, the same way, 2026-11-01:

| Command | Result |
|---|---|
| `go build ./...`, `go vet ./...`, and `go vet` with each tag | clean |
| `go test -count=1 -race ./internal/pimwatch/ ./internal/cron/...` | ok |
| `go test -count=1 ./internal/agui/ ./cmd/aura/` | ok |
| `go test -count=1 -race -tags db_integration -p 1 -coverpkg=./internal/pimwatch/ ./internal/pimwatch/` | ok, 90.4% of statements |
| `go test -count=1 -tags db_integration -p 1 ./internal/cron/` | ok |
| 12 spot mutants on what Task 14c claims, by `sed`, against `db_integration` | 12 killed, one of them only after `TestAnAllDayEventIsReadInThePersonsZone` was added for it |

**What this does not prove.**
- No real sidecar answered: the contract is the fork's at `5f182c7`, read from its code and tests, and replayed by a
  fake.
- `aura serve` was not booted. Each join has its own test (dispatcher to handler, handler to watcher, watcher to the
  real store), and no single run crosses them all on a live stack.
- `make coverage` was not run. The 89.9% is `internal/pimwatch` measured alone, and the gate's aggregate figure and the
  other packages' floors under the full tag matrix are CI's to say.
- Mutation testing was not run locally. It runs in CI.
- Timing under a real provider is unmeasured: 20 pages at 60 seconds each is far more than the 2-minute bound, and only
  the budget check, tested with a stepped clock, keeps a run inside it.

## Found in existing code

- **`DeprovisionDeps.Jobs` was nil on every path, and `Purge` had no step for tasks,** so a deactivated identity's tasks
  kept firing and a purged identity's rows survived it. Both are closed: see Decisions.
- **`ResumeTask` does not read the reason, and approval does not read `deactivated_at`.** The scheduler board can resume
  a task paused by a deactivation during the grace window, or approve a `pending_approval` one, and it fires. Neither
  is closed here.
- **An identity deactivated before this change stays unpaused.** Its journal marks `deactivate` done, so running it
  again skips the step. Its tasks fire until the purge deletes them.
- **A push to Telegram also printed its text to the process journal, and a stdout route did too.** Both would have put a
  subject in the journal, and both are closed in Task 3.
- **A synchronous swarm worker inherits its turn's responder marker,** though `responderCtxKey`'s comment said a swarm
  context never carries one. `personPresent` closes this for the watch's teaching verbs. On `routeApprove`, a worker in
  a live chat still reaches the approval branch and is not denied, which this plan leaves as it found it.
- **`/admin/items`'s `gone` shape** (`[{id, calendarId}]`) is read from the fork's code and tests and has not been seen
  on a running sidecar.
