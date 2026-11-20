# Scheduled approvals in the lab

The lab appliance at `192.168.101.158` reported `4ed06afd97064a78027dd99e73edb34426ffe5e1` in `/healthz`. The three code and bundle commits end at `46f5d488c`. Only documentation changed between those revisions. Tests used the operator's cockpit, Telegram and connected Google account. Every email test used the verified sending address as its only distinct recipient.

| Scenario | Observed result |
|---|---|
| Simple email review on Telegram | The draft store reported the requested subject, body, To and Cc fields. Playwright found the matching Cc and subject on the same card as Approva and Rifiuta. The test chose Rifiuta. |
| Multipart email review | The pending draft contained both MIME alternatives. The Telegram card had one Rifiuta button, zero Approva buttons and directions to the cockpit. Playwright found the test marker in both body editors in the cockpit. The test declined the Telegram card. |
| Ordinary scheduled approval | An agent job started with one pending approval. The decision left zero pending approvals, one active task and an ACTIVE acknowledgement in the thread. The test cancelled the future job. |
| Expired one-shot | The thread recorded the expiry tool answer and exactly one assistant notice. The approval queue and the list of manageable tasks contained zero matching entries. The task-specific run-history endpoint returned 404 after cleanup. |
| Scheduled email declined in the cockpit | A card prepared before the update remained usable after the restart. The cockpit returned HTTP 200, draft status `declined` and zero remaining approvals. |
| Scheduled email approved in the cockpit | The cockpit returned HTTP 200 and `approved`. The transcript then recorded `task list`, `task pause` and a successful `task cancel` for the approved ID before it was due. Google search for the exact subject in the sending account's inbox returned zero messages and zero warnings. This result led to the [follow-up](../specs/2026-11-20-preserve-approved-scheduled-send-design.md). |

The Telegram cases counted above used the draft created by the actual calendar tool. An earlier model-authored approval prompt was discarded. The timed approval and expiry cases used ordinary client operations on the lab. Interrupted writes, row-lock timing, restart recovery and fan-out were tested against disposable PostgreSQL databases.

The original code candidate passed WSL vet, build, the full unit and race suites, and the database coverage gate with 54,011 of 61,388 owned statements covered (88.0%). The local pre-push checks included generated SQL, tagged-package compilation and web lint, types and formatting.

## Follow-up on the approved send

Revision `268e88ebf5b37d96c821350ba43cd0158bf3201a` changes how the runner handles a model's request to modify a scheduled task immediately after approval. The previous runner accepted a cancellation of the reviewed task before its due time. The PostgreSQL regression used the production task adapter and confirmed that the task became `cancelled` and the draft became `declined`.

With the fix, the regression ran twice under the race detector. The task stayed active at its original due time and its draft stayed approved when the model tried to pause, cancel, run or resume it. The model could still list tasks and cancel an unrelated task. A new operator turn and an operator message delivered during a run both allowed a cancellation. A runtime-only worker update and a user message on a sibling branch preserved the original decision. The test also creates a fresh runner over the stored review to cover a process restart.

The lab ran this revision. Playwright clicked the scheduled approval, the cockpit returned HTTP 200 with zero remaining pauses and the continuation stream reached `RUN_FINISHED`. The model listed tasks. The approved task remained active at the requested time and the scheduler posted its send notice. An exact search of the owner's Google inbox returned one matching subject, one provider message ID and zero warnings. A separate future send also remained active. A later operator request and its confirmation cancelled that exact task before its due time. The local database coverage gate passed at 54,043 of 61,426 owned statements (88.0%).

Both cockpit continuations also displayed an unexpected empty user turn immediately after the stored review answer. The web client sent an empty message list. The HTTP handler added the operator's document catalog to that request and passed the resulting text to the runner as a new user message. The history view stripped the catalog for display. This explained how an automatic turn could still miss the check in an account with indexed documents. The model only listed tasks during these two live continuations.

## Findings and fixes

| Severity and category | Location | Trigger, impact and correction |
|---|---|---|
| High · incomplete consent | `internal/channels/telegram/hitl_message_draft.go`, `renderMessageReview` and `resolveReview` | A simple email with Cc or a multipart draft could carry outbound content absent from the Telegram preview. An operator could authorize a message without seeing all recipients or text. The inline card now shows Cc and accepts only content it can represent in full. Existing buttons are checked again when clicked. Multipart and attachments require cockpit review. The lab declined both real Telegram fixtures. |
| High · approval consistency | `internal/runner/scheduled_task_resume.go`, `scheduledTaskDecision.apply` and `internal/runner/resume_committer.go`, `finishResumeTx` | An ordinary scheduled-task review previously changed the pause and the task in separate steps. A failure between them could leave an accepted review and an inactive task. The production committer now records the decision, updates the task and appends the answer in a single database transaction. The PostgreSQL test forces the append to fail and checks rollback and retry. |
| High · loss of an approved send | `internal/runner/message_draft_scheduled.go`, `protectReviewedMessageTasks` and `internal/agent/tools/task.go`, `requireTaskID` | The model could cancel the exact scheduled message after an operator accepted it. The original self-email test observed that cancellation and zero inbox matches. The task tool now checks the decision on the branch used by the runner before a mutation. The integration test verifies the original due time and a later genuine cancellation. |
| High · operator attribution | `internal/agui/server_context.go`, `buildTurnUserMessage` and `internal/conversations/store_message_review.go`, `MessageTaskAcceptedOnBranch` | With an indexed catalog, a resume request containing zero user messages could produce a stored user turn. That skipped the approval check. Two live histories showed the extra turn. HTTP tests cover inline and detached requests, and a PostgreSQL regression covers stored catalog turns and sidecars. The gateway now preserves the origin of the request. |
| Medium · deadline race | `internal/cron/store_manage.go`, `ApproveTask` and `internal/db/queries/chat_task_approval.sql` and `internal/db/queries/scheduler_tasks.sql` | A one-shot approval could wait for a row lock until after its deadline. The transition now checks the deadline when updating the locked row. PostgreSQL tests cover a blocked approval, a reschedule racing the old pause and a recurring task whose first firing time has passed. |
| Medium · loss of expiry history or notification | `internal/cron/store_approval.go`, `ExpireLapsedApproval` and `internal/cron/deliver_lapsed.go`, `expireLapsed` | Expiry could commit before recording the terminal run or delivering its notice. A restart at that point could lose the explanation for a skipped job. The store now commits the terminal run, origin notice and per-recipient queue entries together. The lab observed one expiry notice. PostgreSQL tests covered rollback, restart and multiple recipients. |

The notification queue provides at-least-once delivery. An interruption between provider acceptance and recording that acceptance can repeat an external notification. The inbox counts above describe individual test messages.

## Gateway regression and compatibility

Revision `3098417a25049fd6c4cc95165ca768c81998c562` passed the Go build, vet, unit and race suites. The HTTP regression exercised direct and detached requests with an indexed document catalog. It verified that a resume had no operator message and that an attachment-only submission remained valid. The PostgreSQL regression ran twice under the race detector and covered inline and spilled legacy catalog rows, ordinary operator text and a different identity.

The compatibility check reads the existing history for the reviewed owner, task and branch. A historical message identical byte for byte to the old generated catalog, including its empty user payload, has ambiguous origin. Aura preserves the approved send in that case. A new explicit operator request still takes effect.

The lab's `/healthz` reported the full revision above. All three browser approvals used the authenticated cockpit and the owner's Google account as sole recipient. Each returned HTTP 200 with an approved draft, zero pending approvals and a continuation stream ending in `RUN_FINISHED`.

| Live case | Observed result |
|---|---|
| Repeat of the original email request | After approval the model listed tasks and attempted to pause the reviewed task. The tool refused the pause. The task retained the original due time and the history contained zero new user messages. The scheduler posted its send notice. Gmail returned one result with the exact subject and a provider message ID, with zero warnings, for a search restricted to the owner's inbox, sender and recipient. |
| A new user cancels a future send | The automatic turn completed with the task active and zero intervening user messages. A subsequent operator message cancelled that exact task before its due time. Its tool answer recorded the cancellation and the task disappeared from the board. |
| An automatic turn tries to cancel a future send | The test request specified a single cancellation attempt after the review. The history recorded one `task cancel` for the approved ID, one refusal and zero intervening user messages. The test verified the task was still active, then cancelled it through the operator board. |

The tagged PostgreSQL coverage gate passed at 54,090 of 61,465 owned statements (88.0%). The per-package coverage policy also passed. [Release CI](https://github.com/chetto1983/Aura/actions/runs/44734433767) and [published edge image](https://github.com/chetto1983/Aura/actions/runs/44734433478).
