# PIM tool split: the lab VM checks (2026-11-25)

Continues `2026-11-24-pim-tool-split.md`. Plan: `docs/superpowers/plans/2026-11-23-pim-tool-split.md`
(Task C8 steps 4–5). Spec: `docs/superpowers/specs/2026-11-23-pim-tool-split-design.md` §7, checks 1–10.
The run was on the lab VM. A last section adds the re-measure of 2026-11-26.

Checks 2, 3, 4, 5, 7, 9 and 10 ran in the cockpit, through Claude in Chrome, with the operator
present. Check 8 is read from the live send turns at 13:29 and 13:41. For checks 1 and 6, the ledger
records the result and not the method.

The facts below come from the SDD ledger of the run. Mail and events went only between the operator's
own accounts, and every measurement mail and event was deleted afterwards. This file holds counts and
yes/no.

## The release on the VM

| | Result |
|---|---|
| Applied by | `aura-image-update`, 13:20:42–13:21:22 UTC, before CI on the release had finished |
| Aura revision | `442c2058d` (Part C) |
| `aura-migrate` | 2 migrations applied (`0158_pim_tool_split`, `0159_data_upgrades`) |
| Upgrade `Report` | `already_applied=false`, `rewritten=0`, `fanned_out_denies=0`, `fanned_out_asks=0`, `dropped=0`, `drafts_rewritten=1`, `drafts_expired=1` |
| Expired draft's outcome | `account_required` |
| PIM after the apply | fork `8fbd799`, digest `sha256:f595731d…`, recreated 13:21:22 UTC |
| PIM approval policies on the VM before the split | none |
| CI on `442c2058d` | 29 jobs green, 1 red: an embedding-tier contract test expected six live tiers to use a shared helper, and the new `mcp_live_integration` job made it seven. Fixed in `ed9322b72`. CI on `7e379a89d`, which carries that fix: every workflow green |

The seeding that Task C8 step 5 planned for checks 5 and 6 (pre-upgrade denies, and pre-upgrade drafts
with and without an account) never happened. The updater applied the release first. Checks 5 and 6
below say what that leaves unshown.

## Routes of the day

The primary model changed at about 14:00 UTC.
- **Before.** The route was `deepseek/deepseek-v4.1-flash-1017` on OpenRouter, which sent it to the
  provider Wafer (check 9 below). Checks 2–8 and the first attempt at check 9 ran then. The ledger does
  not record which provider served each of those turns.
- **After.** The operator switched the primary model to `thinkingmachines/inkling-2`. The re-run of
  check 9 and check 10 ran on it.

## The updater incident, and check 1

**Finding (Critical, release-wide).**
- The updater recreated `aura` at 13:20:42, before it recreated the PIM at 13:21:22.
- At 13:20:56, Aura mounted `calendar` against the old PIM. It logged `mcp live mounted server=calendar
  tools=1`, plus 29 warnings that the risk table has a calendar tool the server no longer advertises.
- Nothing remounted the PIM after its restart. On the next redial, the bridge's tool-set check saw the
  names change from 1 to 29 and marked the server dead: "tool set changed; restart required".

So any appliance that updates to `442c2058d` loses the PIM until `aura` restarts.

**Recovery.** At 13:25 UTC the operator approved one `docker restart aura`. Aura then logged `mcp live
mounted server=calendar tools=29`.

**Fix, `0dac06ddb`..`7e379a89d`.**
- **Before replacing anything,** the updater records the image each running MCP sidecar runs. It writes
  that to `MCP_RECORD`, under the install directory.
- **At the end of the apply,** it restarts `aura` once if some sidecar meets all three conditions:
  - it was running when the record was written;
  - it now runs another image;
  - it was created after `aura` started.
- **Afterwards,** the record is removed. A tick that dies keeps the record.

`scripts/aura_update_mcp_restart_test.sh` covers this, in `ci.yml` and `.gitlab-ci.yml`. CI on
`7e379a89d` is green. The fix passed review after two rounds.

**§7 check 1: PASS.** This is after the restart.

| | Result |
|---|---|
| `calendar__*` tools in Aura's registry | 29 |
| `calendar__calendar` | absent |
| Whole registry | 234 tools |

What this does not show:
- **A raw `tools/list`.** The evidence is Aura's mount log and its registry, read after the restart.
- **The updater fix on the VM.** On 2026-11-25 its live proof was still owed, in two parts:
  - a PIM replacement through the updater leaves 29 calendar tools with no manual restart. The re-measure
    of 2026-11-26 shows this (last section);
  - a boot followed by an apply does not restart `aura`. This is still not shown.
- **The parked case.** If an apply moves only a sidecar and dies after that, Aura keeps a stale tool set
  until the next update or reboot. That case is parked, not fixed.

## §7 check 2: PASS. A send produces a reviewable draft that sends

The operator asked for a send to their own work account.

| | Result |
|---|---|
| Tools loaded | by name (`tool_search select:`) |
| Draft target | `recipe:calendar`, tool `send_email`, registered as `calendar__send_email`, action empty |
| `accountId` in the stored args | yes |
| `action` key in the stored args | no |
| Sender address on the draft | set |
| Draft created | 13:30:46 |
| Operator's "Invia messaggio" → dispatch | 13:31:19 |
| PIM result | success |
| Draft status | `sent`, outcome `ok`, 13:31:20 |
| Sent copy in the sending account | yes, read back by Aura's own `get_emails` |
| Received copy in the work account's inbox | yes, read back the same way |

**Clean-up: the destructive tier withholds.**
- Aura loaded `calendar__bulk_delete_emails` by name.
- The gateway withheld the call: destructive tier, `gateway_approval_required`, and an `ask_user`
  approval.
- The arguments named exactly the 2 measurement copies.
- The copies were approved once, and the PIM reported 2 requested, 2 succeeded and 0 failed.
- Afterwards, the work account's inbox held only an older mail from 2026-11-23.

What this does not show: a send to anyone other than the operator, or to a third account.

## §7 check 3: PASS. A briefing reads with no reservation error

The briefing turn ran from 13:34:54 to 13:35:10.
- It loaded the skill `briefing-aura`, and then its tools by name.
- It made these calls:
  - `board` list;
  - `calendar__get_calendar_events` twice, for today and tomorrow, in Europe/Rome;
  - `calendar__get_emails` with `unreadOnly`, across the accounts.
- Every call ended `ok`. No result carried a reservation, withheld or error mark.
- Aura's logs had no WARN or ERROR in that window.

What this does not show: a headless `agent_job` briefing. The reservation failure of 2026-10-31 (PRD §15)
was headless, and this check was a cockpit turn.

## §7 check 4: PASS, with a finding. An event is created, read and deleted by its reference

1. `create_event` ran in the Normal tier, with no approval.
2. `get_calendar_event_details` read the event by the `eventId` that create returned.
3. `delete_event` used the same `eventId`. It is Destructive, so it was withheld and approved once. It
   succeeded.
4. A re-read of that day on all accounts returned no events.

**Finding.** Aura's first `create_event` carried no `accountId`. The PIM wrote the event to its default
account, the first one configured, which is the operator's Microsoft account. Aura noticed:
- it created the event again on the personal account;
- it deleted the stray one, with one more approval.

At the pin of the day, `accountId` on `create_event` was optional and fell back to the first account.

What this does not show:
- whether the model left `accountId` out, or the provider dropped it (see check 9);
- the fix. Pin `8f97396` (below) makes `accountId` required on `create_event` and `create_contact`. Its
  re-measure is in the 2026-11-26 section.

## §7 check 5: PASS for the per-tool denies, on the amended wording. Carry-over not shown live

1. **A deny on `calendar__send_email`.** Aura's send became a review draft, with no refusal. The draft
   was declined and never sent. This matches the operator decision recorded in PRD §5 on 2026-10-10: a
   trusted outbound tool drafts before the gateway, and a confirmed draft passes a `deny`.
2. **A deny on `calendar__list_calendars`,** a PIM tool that is not drafted. Aura refused the call at
   13:43:18 with "tool calendar__list_calendars is disabled for this identity by policy". Aura then routed
   around it through `get_calendar_events`, the PRD's known behaviour of a model under a deny.
3. Both rules were removed afterwards, which left 0 rows in `gateway_tool_policies`.

The spec's wording, "a `deny` … refuses `calendar__send_email`", contradicted that 2026-10-10 decision.
Spec §7 is amended on 2026-11-25 to match the operator's decision of 2026-10-10.

What this does not show:
- **The carry-over of a pre-upgrade deny.** Both denies were set after the upgrade, because the VM had
  no policies to carry (`rewritten=0`). The rewrite and the tool-wide fan-out are proven only by the
  `db_integration` tests of `internal/pimsplit`.
- **A confirmed send under a deny.** The draft was declined, so a send under a deny was not re-run
  today.
- **A tool-wide deny on the 29 tools,** live.

## §7 check 6: PARTIAL. Drafts made before the upgrade

The VM held 2 email drafts from before the upgrade. Their upgrade outcomes match the report
(`drafts_rewritten=1`, `drafts_expired=1`):

| Draft | Upgrade outcome | Shown |
|---|---|---|
| pending, from 2026-10-10, with an account | rewritten to the `send_email` shape: `accountId` yes, `action` key no, still `pending` | the rewrite, yes. **That it sends afterwards: no.** Its `expires_at` passed on 2026-10-10, so it cannot be approved |
| pending, no account | expired, outcome `account_required` | yes |
| scheduled email draft, with an account | not recorded. The report's two drafts are the two above, so none appears to have existed | no |

What this does not show:
- **A pre-upgrade draft that sends after the upgrade.** No test shows it either: the `pimsplit` tests stop
  where the rewritten draft matches what a dispatch recomputes.
- **A scheduled pre-upgrade draft,** in any state.

## §7 check 7: PASS. A conversation from before the upgrade replays its tables

- **A conversation of 2026-11-23,** which called the multiplexed `calendar__calendar` with `list_accounts`.
  It replays its 3-row accounts table with filter, copy and CSV.
- **An empty legacy `get_calendar_events`.** It shows the empty-table text.
- **A `calendar__list_accounts` made after the upgrade.** It renders the same table.

What this does not show: every one of the PIM's table kinds. Only the two above were replayed.

## §7 check 8: PASS on the amended wording. The agent loads `send_email` by name

On the live send turns, at 13:29 and 13:41, the agent loaded `calendar__send_email` by name through
`tool_search select:`.

The spec's criterion was that BM25 ranks `calendar__send_email` first for "manda una mail", and the C7
corpus did not meet it:
- "manda una mail" ranks it 5th, after `mail_watch`, `get_contextual_email_summary`, `get_emails` and
  `search_emails`;
- "send an email" ranks it 1st.

The operator's ruling of 2026-11-25 is that the lever is the tool descriptions, not the ranking. The
agent calls tools by name from the roster. The check now reads that the agent loads the tool by name on
a live send turn. Check 2 covers that, and checks 2 and 10 test the descriptions. The keywords from C7
stay.

What this does not show: free-text discovery of the send tool for an Italian phrasing. The held-out
ratchet in `2026-11-24-pim-tool-split.md` is the only measure of that.

## §7 check 9: first attempt failed, root cause found, then PASS

**First attempt, 13:46–13:51, on the DeepSeek route.**
- **The cockpit reminder.** The operator asked in the cockpit for a reminder by email in 5 minutes.
  - 4 of 4 `task schedule` calls carried only `action`, `kind`, `schedule_kind` and `tz`: no `at`, no
    `payload.text` and no `notify`. Each was refused: "a reminder needs payload.text".
  - Between 13:46 and 13:51, the agent made 8 calls with missing arguments: those 4, then later
    attempts. The dedup guard blocked some of them. Then the agent scheduled a complete one.
  - A separate turn made 6 calls of its own on the same refusal.
- **The controller's task.** Meanwhile, the controller scheduled a reminder of its own with `aura task
  schedule --kind reminder --notify email`.
- **The cancels.** On the operator's next message, the agent ran `task list` and then `task cancel` on
  both tasks, without asking. One of the two was the controller's, which this conversation had not
  created.

Neither task fired, so the check was not measured.

**Root cause, 14:00 UTC.**
- **The routing.** OpenRouter routed `deepseek/deepseek-v4.1-flash-1017` to the provider Wafer (mxfp4).
  The model's reasoning named `at`, `payload` and `notify`. Wafer's returned arguments dropped `payload`
  and invented a `task_id`.
- **The replay.** It pinned the provider, with fallbacks off, against the real `task` schema:

  | Provider | Correct calls |
  |---|---|
  | Wafer | 13/22 (9 dropped `payload` and invented a `task_id`) |
  | DeepSeek | 11/11 |
  | Fireworks | 5/5 |
  | Wafer, with `payload` declaring its properties | 8/15 |

  So the schema is not the cause.
- **Aura's side is clean.** The raw arguments were recorded verbatim, through the stock accumulator.
- **Why OpenRouter did not catch it.** `task` requires only `action`, so a dropped `payload` is still
  schema-valid. OpenRouter's quality routing (Auto Exacto) counts only invalid JSON, unknown tool names
  and schema mismatches, so it does not see this fault.
- **The operator's response.** The operator switched the primary model. A draft report on the provider
  fault was prepared, for the operator to send to OpenRouter.

**Re-run, on the new route: PASS.**

| | Result |
|---|---|
| Calls to schedule | 1 `tool_search` and 1 `task schedule` |
| Arguments | complete: `at`, `kind` `reminder`, the exact `payload.text`, `notify` `email` |
| Fired | 14:11:46 UTC |
| Notifier email route | PIM `list_accounts`, then the post-split `send_email` |
| `send_email` result | "Email sent successfully" at 14:11:49.761, not an error |
| Sending account | the operator's personal account |
| Recipient | that same account's own address, a self-send: yes, from the recipient's own account |
| One-shot task deleted | 14:11:49.780 |
| `pending_notifications` | 0 |

A second reminder, scheduled from another chat, was delivered at 13:58:19. The ledger does not say which
route scheduled it.

**Clean-up.**
- Aura found the 2 measurement mails with `search_emails` and `get_email_details`.
- Its own `ask_user` listed exactly those 2.
- The gateway withheld `calendar__bulk_delete_emails` as destructive, and its arguments named exactly the
  2 mails. They were approved once, and the PIM deleted 2 of 2.

What this does not show:
- **More than one account and one route.** The self-send was measured once, with one account, on one
  route.
- **A reminder on the DeepSeek route served by a provider other than Wafer.**

## §7 check 10: the count. 0 of 4 drafts without an account

The route was `thinkingmachines/inkling-2`. There were 4 sends, each in a fresh chat:
1. to "myself", with no account named;
2. to "me", with no account named;
3. with the account and the recipient named;
4. a terse request that named nothing.

| | Count |
|---|---|
| Drafts that reached review | 4/4 |
| …with a known `accountId` | 4/4 |
| …without one | **0/4** |
| …with the legacy `action` key | 0/4 |
| Sends | 0 (all declined) |

In the 3 cases with no account named, Aura called `list_accounts`, then asked the operator which account
to use (an `ask_user` choice). In one of them it also asked for the recipient. In case 3 it resolved "my
work address" through `memory_recall`.

**Decision, per spec §5.3.** On this route, Aura adds no pre-review requirement of its own for
`accountId`.

What this does not show:
- **The DeepSeek route.** Its runs that day are contaminated by the provider fault of check 9.
- **Any other model.**
- **More than 4 samples.**

## Follow-ups that landed the same day

- **Task schedule refusals name what arrived** (`4adc35b33`, `5a763b058`, `f74a9328e`).
  - A refusal now names the keys it received and the missing field.
  - It no longer tells the model to ask the operator for text that the request already carried.
  - A value that is present but bad gets "fix the value", not the missing-field text.
  - The fix passed review after two rounds.
- **PIM pin `8f97396`** (`7f8f07d03`, payload manifest `ac9158ff7`).
  - `create_event` and `create_contact` now require `accountId`, in the schema. A call without one gets
    an error that names `list_accounts`.
  - The first-account fallback is removed.
  - Fork CI on that revision passed 2188 of 2188 tests, and the image was published.
  - **Red in Aura's CI.** The pin commit `7f8f07d03` turned one job red: "Calendar PIM MCP"
    (`calendar_integration` tier). Its `pimToolRequired` still pinned `create_event` and
    `create_contact` without `accountId`. `68ddb22f8` fixed it.
  - The live re-measure from the cockpit is in the 2026-11-26 section.
- **The push.** `7e379a89d..f74a9328e` was pushed.
  - **CI on `f74a9328e`.** One red job, the "Calendar PIM MCP" job above. Every other job was green.
  - **The fix.** `68ddb22f8` is on origin.
  - **Origin head `afe374df1`.** All green.

## Observations queued, not fixed in this release

- **Composer.** After an `ask_user` or approval card resolves and the turn ends, the composer floats
  mid-viewport instead of staying docked at the bottom. Reproduced twice.
- **Interactive view.** The "Vista interattiva" panel never renders. It reads "questa vista non si può
  mostrare qui", for old and new conversations alike, so the split did not cause it.
- **Double confirmation.** One delete asks twice: the agent's own `ask_user`, then the gateway's
  approval.
- **Raw HTML in review.** A draft review showed a raw HTML body (`<p>…</p>`) in the plain-text field.
- **Stale pending drafts.** A draft past its `expires_at` stays `pending`, here for 46 days. Expiry is
  applied lazily.
- **Unapproved cancels.** `task cancel` on a task that the conversation did not create runs without
  approval.
- **A recurring PIM warning.** On every notify, the PIM warns that the Microsoft account's sending
  address was not found (an ODataError). It still sends from the Google account.

## What this run does not show

- **The deferred roster's size on the appliance.** Task C8 asked for it, and the ledger does not record
  it. The only figures are the C7 corpus's, in `2026-11-24-pim-tool-split.md`.
- **The pin `8f97396` on the VM,** or the updater fix on the VM, on 2026-11-25. Both are in the next
  section.
- **More than one VM.** The VM is a single appliance, with the operator's three accounts.

## Re-measure, 2026-11-26 07:15–07:22 UTC: pin `8f97396` and the updater fix on the VM

The operator applied the update.

**The updater.**

| | Result |
|---|---|
| `aura` recreated | 07:15:47 |
| Replaced after `aura` started | the whole-stack `up` replaced `aura-pim-mcp` (`8fbd799` → `8f97396`) and `arcadedb-mcp` |
| Updater log | "aura: restarting it to remount arcadedb-mcp aura-pim-mcp, replaced after it started" |
| `aura` restarts | 1, at 07:17:53 |
| Updater result | "Aura image update completed; the appliance is healthy" |
| Old PIM image | removed |
| `calendar__*` tools in Aura's registry | 29 |
| `calendar__calendar` | absent |
| Calendar mount log | `mcp live mounted … tools=29` |

This is the incident of 2026-11-25, converged by the updater with no manual restart.

**`create_event` with no account named.**
- **The request.** In the cockpit: create an event tomorrow at 10:00, for 30 minutes, naming no account.
- **The calls.** Aura loaded `create_event`, then called `list_accounts`.
- **The choice.** Aura asked the operator which calendar to use. It offered only the 2 accounts that can
  write a calendar.
- **The result.** `create_event` ran with `accountId` set to the personal account, and succeeded.
- **The clean-up.** The gateway withheld `delete_event` as destructive. Its arguments were verified to
  be the event just created. It was approved once, and the event was deleted.

What this does not show:
- **The new refusal, live.** The route of the day never left `accountId` out, so no model that omits it
  met the refusal on the VM.
- **The updater's boot case.** No reboot was measured: a boot followed by an apply that should not
  restart `aura`.

**Observation (UI).** The approval card rendered below the fold, and the chat did not scroll to it.
