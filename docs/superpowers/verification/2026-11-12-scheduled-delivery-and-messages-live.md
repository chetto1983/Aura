# Scheduled delivery and messages, live on the lab VM — 2026-11-12

This is the live run in Task 16 of
`docs/superpowers/plans/2026-11-05-scheduled-delivery-defaults-and-messages.md`. Its dry run is
`2026-11-11-scheduled-delivery-model-in-the-loop.md`.

**Who drove it.** The operator updated the VM from the cockpit each time. Claude drove the
scenarios, as the operator asked ("fai pure te hai tutto l'harness"):
- cockpit turns went through the HTTP API, signed in as the operator;
- Telegram went through Telegram Web in the operator's browser.

Every message to a third party went to an address the operator controls. The operator confirmed
each delivery from their own mailbox.

**Builds.** The run spans four builds. The first pass found four defects (Task 15a) and one
display gap (Task 15b). Scenarios 3, 4 and 6 ran again after each fix.

| Build | What ran |
|---|---|
| `181be7694` | all eight scenarios |
| `fb5f948b5` | 3, 4 and 6 again, after F1–F4 |
| `5f820f74b`, PIM `bb87b828e` | 3 and 4 again, after the redraw fix and the sender address; PIM image `sha256:3a632365…` |
| `69fbaa829` | one reviewed send, for F5's ledger fix |

**The VM.** `edge` on `192.168.101.158`, with the operator's own accounts: three mail accounts,
one linked Telegram account and WhatsApp.

## What this run does not prove

- **Email at volume.** At most two scheduled messages were in flight at once.
- **More than one WhatsApp mount.** The VM has one.
- **A daemon down across the grace period.** The update at 10:18 restarted Aura three minutes
  before a scheduled send, which went out once. That is a restart, not an outage that outlives the
  grace period.
- **`aura task cancel` leaving a pending pause.** Every cancellation in this run went through the
  card or the model.
- **Scenarios 1, 2, 5, 7 and 8 on the last build.** They passed on `181be7694` and were not run
  again. The later fixes touch the card, the email route, the task tool's account and delivery
  wording, and the draft store, not the paths these scenarios exercise.
- **A stuck approval cleared by the first pass after deploy.** This VM had none before the update:
  8 `reminder` and `agent_job` tasks, all `cancelled`. The 2026-11-02 task is on another
  operator's instance.
- **The mutation scores cover five files only**, the four CI scopes and `store_scheduled.go`.

## Result: 8 of 8, score 10

Nothing went out twice.
- **Drafts.** Every `sent` draft has one row, and the operator's mailbox holds one copy of each.
- **The 10:20 and 10:21 notices.** They came from two different drafts, scenario 3 and
  scenario 4, both resent after the fixes. Each has one row.

| # | Pass | Evidence |
|---|---|---|
| 1 | ✅ | Default Telegram: nothing was asked, the task's `notify_route=telegram`, and the reminder arrived on Telegram. |
| 2 | ✅ | No default: the model asked for the channel and named the Profile panel. The reminder arrived on the channel chosen. |
| 3 | ✅ | Cockpit, all three builds. One card holding recipient, text and time. Approved, the draft ended `sent`, the run summary read "Message sent to …", the notice reached Telegram, and the mailbox holds one copy. From `fb5f948b5` the model listed the three accounts and asked which to send from. On `5f820f74b` the card reads `Account: personale <address>`, from the `sender` field of `/api/message-drafts`. |
| 4 | ✅ | Telegram, read-only card. Approva → `sent` once, at the time. Rifiuta → `declined`. Silence → `expired`, with the notice "message for … was not approved in time and did not run." A press on an expired card answers "Non più attiva: già gestita o scaduta", sends nothing and drops the keyboard (`fb5f948b5`). On `5f820f74b` the card reads `Da: personale <address>`. After Approva it is redrawn as "Approvato: partirà all'ora prevista." with no keyboard. |
| 4+ | ✅ | Not a rubric scenario: the immediate path the plan left to this run (Task 10's `TestTelegramApprovaSendsUnderAStrictGateway`). On `5f820f74b` an email sent now, approved from its Telegram card under the VM's strict profile: the gateway saw exactly one `send_email`, the draft ended `sent`, not `failed` with "operation context missing", and the card was redrawn "Inviato.". |
| 5 | ✅ | A declined card → `declined`. An approved task cancelled before its time → `declined`. Nothing was sent in either case. |
| 6 | ✅ | `181be7694` **failed**. "In 5 minutes email the summary to <a third party>" became an `agent_job` with `notify=email`, and the model promised delivery to that address. The email route reaches only the operator's sign-in address, so nothing reached the third party, but the operator was told it would (F2). On `fb5f948b5` the model said the summary can go only to the operator's own email and asked how to go on. Cancelled: no task, no send. |
| 7 | ✅ | One `agent_job` scheduled from the cockpit and one from Telegram. Each was approved in its own chat, in the same turn, with no `ask_user` call. The approved one fired once. The declined one was cancelled, and its pause resumed `decline`. |
| 8 | ✅ | A one-shot `agent_job` left unapproved. After its time, "agent_job for … was not approved in time and did not run." arrived on Telegram. The task left the awaiting-approval list, and no pause stayed pending. |

**Counts at the end of the run, across all builds:**
- **Message drafts:** 5 `sent`, 4 `declined`, 1 `expired`.
- **Today's pauses:** 9 approvals resumed `accept`, 6 resumed `decline`, and 4 `choice` resumed.
  None is pending.

**One-shots leave no ledger rows.** A finished or cancelled one-shot is deleted with its runs
(the 2026-10-05 cleanup, finding F3 of the dry run). For scenarios 7 and 8 the evidence is:
- the turn's SSE outcome;
- the pauses' resumed answers;
- the notices delivered on Telegram;
- the rows read while the task still existed.

The rubric's "invocation count" has no row to count, because a host-parked call writes none
(dry run, finding F2). For scenarios 3 and 4 the evidence is the draft's status, the run summary
and the mailbox count.

## Found in the run, and fixed

| | Measured | Fix |
|---|---|---|
| F1 | Four scheduled emails named no sending account, so the PIM picked one by the recipient's domain, else its first account. | `7963b3eaa`: an email message needs `payload.account`. |
| F2 | Scenario 6's false promise. | `1b6cfeb3a`: the approval carries where the result goes, and the APPROVED answer repeats it. |
| F3 | A resolved card kept its review text. An expired card kept live buttons, and a press answered "Confermato ✓" and resolved nothing. | `6bf156806`: a stale press answers "Non più attiva". `25178bf1e`: the card is redrawn from the resolve's own status. The first redraw re-read the draft from a list that holds only pending reviews. |
| F4 | A job's markdown reached the mailbox as raw markers. | `75493947d`: the email route sends multipart, text plus goldmark GFM HTML, with raw HTML escaped. |
| F5 | All six of the day's reviewed sends (cockpit, Telegram, scheduled fire) kept their gateway `start` row with no `end`. Thirty minutes later the reconciler stamped the four older ones `error`, "reservation reconciled: crash-orphaned …", over messages that were delivered. `ExecuteReviewedMessage` dispatches outside any Runner but did not mark itself unobserved, and the gateway writes the `end` only for such a dispatch. | `69fbaa829`: the reviewed send marks its context unobserved, as the scheduled `agent_job` has since 2026-10-14. Live on `69fbaa829`, an immediate email approved through the cockpit's resolve wrote its `end` row 877 ms after its `start`, with `status=ok` and `{"unobserved": true}`. |
| 15b | The card named the account by its raw id, `<tenant hex>__personale`. | PIM `bb87b828e`: `list_accounts` reports each account's `email`. Aura `faa5170fc` and `5f820f74b`: the host resolves the address when the draft is created, stores it (migration 0156), and both cards show `name <address>`. |

## Dry run

`2026-11-11-scheduled-delivery-model-in-the-loop.md` ran scenarios 1, 3, 7 and 8 with a parked
model on `fa7adbdf4`. All four held. It found and fixed the approval reminder that ran without the
task's identity (`3f7330fad`).

## Gates

- **Pre-push on `5f820f74b`, WSL:** all green — build, sqlc-sync, deadcode, web-deadcode, the
  tagged-tier compile and web.
- **CI on `69fbaa829`, the closing SHA:** every workflow green, all 29 CI jobs included. The
  owned-surface coverage gate measured 53,909/61,285 = **87.96%**, against a floor of 85%, with
  the package policy passing. On `5f820f74b` the live `calendar_integration` tier ran against
  the new PIM image. That run was cancelled by the push of `69fbaa829`, after its coverage gate
  had passed at 87.97%.
- **Mutation, CI's critical gate,** `turn-reading` group on `fb5f948b5`, run 43187815401. None
  of the four files changed after that SHA. Every scope is at or above 70% killed:

  | Scope | File | Killed |
  |---|---|---|
  | `task_approval` | `internal/agent/tools/task_approval.go` | 25/28 = 89.29% |
  | `task_message` | `internal/agent/tools/task_message.go` | 50/61 = 81.97% |
  | `message_fire` | `internal/cron/handlers/message.go` | 33/44 = 75.00% |
  | `lapsed_approval` | `internal/cron/deliver_lapsed.go` | 31/44 = 70.45% |

  `lapsed_approval` clears the bar by one mutant.
- **Mutation of `internal/messagedrafts/store_scheduled.go`:** **22/29 = 75.86%** killed,
  under `db_integration`, on a disposable database, in the dry run's cloud session.
  - The 7 survivors are database-error returns that only fault injection reaches.
  - Task 15 asked for a re-measure in WSL. It was not made, because mutation runs in CI only, by
    the operator's standing rule.
