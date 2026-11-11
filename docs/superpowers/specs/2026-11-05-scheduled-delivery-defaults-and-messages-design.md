# Scheduled delivery: a channel the operator sets once, and messages to someone else reviewed when they are scheduled

**Status:** draft, written 2026-11-05 from an operator conversation and the decisions taken in it (default channel set in the cockpit; fire-time-content messages forbidden; one-shot only; Telegram review like Hermes). Builder-checked once. **Approved by the operator on 2026-11-05 ("approvo").**

## Why

An operator's exported cockpit conversation (2026-11-05) raised two defects in `task`.

1. **A standing rule does not reach the scheduler.** The operator told Aura "reminders go to Telegram unless I say
   otherwise". Aura stored it in memory, then said it still could not promise to stop asking. It was right. Nothing in
   the code forces the question: `actionSchedule` refuses only an empty `notify`
   (`internal/agent/tools/task.go:226`) and does not care where the value came from. The question comes from the tool
   text: "If the operator did not specify one, call ask_user(kind=choice)" (`task.go:152`, `task.go:169`). The model
   reads that as "this request did not name it", and a memory fact is only present when recall happens to return it.
   Commit `13ee4d872` (2026-08-29) deliberately removed every identity-wide and route-default fallback, so no
   deterministic default exists either.
2. **A message to someone else, scheduled for later, can never be sent, and Aura promises it will be.** The operator
   asked for "at 08:25, send a WhatsApp to <a contact> saying …". Aura scheduled an `agent_job`, the operator approved
   it, and Aura answered "approved and active, the send will run at 08:25". The send is structurally impossible:
   - A WhatsApp `send_message` (or a PIM `send_email`) to anyone is withheld for review only when a live responder
     exists (`internal/agent/llm_agent_message_draft.go:37`).
   - A headless `agent_job` has none, so the gateway denies the call with "message review required or claim
     mismatch" (`internal/gateway/message_draft.go:60`). The same operator's 08:00 attempt that morning failed exactly
     so.
   - The 08:25 attempt stopped earlier: two contacts shared the name, and the job's goal did not carry the number the
     08:00 run had already resolved.
   - Approving the schedule is not reviewing the message, and today the operator approves twice without ever seeing
     the message that would leave.

**How Hermes does it** (`NousResearch/hermes-agent`, read 2026-11-05):
- An omitted `deliver` addresses the chat the job was created from, falling back to the platforms' configured
  `home_channel`. Hermes never asks.
- `USER.md` rides the system prompt every session, so a stated preference always reaches the model
  (`tools/memory_tool.py:2-3`).
- `send_message` is deliberately not a model-callable tool: "the agent must not fire cross-platform messages on its
  own". A job delivers only to configured targets.

This spec takes Hermes's deterministic default, stored where the operator can see and change it. It keeps Hermes's
refusal for messages whose content the model writes at fire time. It goes one step past Hermes for a message whose
recipient and text are fixed when it is scheduled.

## Goals

1. **The default channel is a profile setting.** It lives in `aura.identity_profiles` and is edited in the cockpit's
   Profile panel. `task` uses it when the request names no channel, and says so in its reply.
2. **One reviewed scheduled message.** A new task kind `message` sends one message to someone else at a set time. The
   recipient and the exact text are reviewed and approved when it is scheduled, on the surface the operator asked
   from: the cockpit or Telegram. At fire time it is sent with no model in between, at most once. The operator wants to
   send scheduled messages from Telegram (2026-11-05), so Telegram review is part of this work, the way Hermes renders
   its approvals on every platform (`gateway/platforms/base_exec_approval.py`).
3. **No promise the system cannot keep.** An `agent_job` is told plainly that it cannot write to anyone else, and a
   job that tries is reported to the operator as "not sent", with the reason.

## Non-goals

- **Recurring messages to someone else.** Only one-shot `message` tasks. Recurrence with one fixed approved text is a
  later extension; the operator's call, 2026-11-05.
- **Messages to someone else whose text is decided at fire time** (e.g. "at 8, send them last night's mail summary").
  They stay forbidden, as in Hermes; the operator's decision, 2026-11-05. A fire-time review pushed to the operator's
  phone is a separate piece, not this one.
- **Editing on Telegram.** Telegram shows the message and approves or declines it, but does not edit it; editing stays
  in the cockpit card (`web/src/approvals/MessageDraftCard.tsx`).
- **Messages more than about a day ahead.** A message draft cannot live past 24 hours (`Store.Create`,
  `internal/messagedrafts/store.go:104`), a bound chosen on purpose. Hermes's approvals last minutes
  (`approvals.timeout`). The bound stays, so a `message` is scheduled at most 24 hours minus the grace period ahead.
  *My recommendation, not contradicted by the operator*; widening it is a later, separate decision.
- **A model tool to edit the profile.** The default is changed in the cockpit only; the operator's words, 2026-11-05.

## Anti-goals

- **No text-pattern guessing of a job's intent.** `task` must not scan an `agent_job` goal with regexes to decide
  whether it "wants to message someone". That is natural-language analysis done badly, and the project's rule forbids
  it. The enforcement is the gateway's existing denial, and honesty comes from the tool text and the outcome report.
- **No silent route.** A default must never send something to an external channel the operator did not choose. The
  profile value is the operator's own choice, and every reply that used it names it. This is the 2026-08-29 concern
  that motivated `13ee4d872`, and the design keeps it.
- **No second path to send.** The fire-time send of a `message` task goes through the same gateway check and the same
  one-send draft claim as an interactive reviewed send. It is not a new transport that skips them.

## Part 1: the default channel

### Storage

- A new column on `aura.identity_profiles`: `default_notify text NOT NULL DEFAULT ''`, checked to be `''` or one of
  `none`, `stdout`, `telegram`, `whatsapp`, `email` (the `cron.ValidNotifyRoute` set). `''` means "not set".
- The row is already owner-isolated by RLS (migration 0097); nothing changes there.
- The migration number is the next free slot when the work lands: `ls internal/db/migrations/ | tail -1` (the last was 0153 on
  2026-11-05, so 0154 today).
- The profile is a setting, not a memory (0097's own rationale), which is why the value lives here and not in
  ArcadeDB.

### `task`

- When `notify` is empty, `actionSchedule` reads the identity's default through the same kind of seam it uses for the
  timezone (`Timezones` → `task_timezone.go`; `onboarding.ProfileStore` implements it).
- **Set:** the default is used exactly as if the model had passed it. Every existing check still runs: the
  `destination` check, and the `telegram_recipients` choice when two or more Telegram accounts are linked. The reply's
  delivery line names the source, e.g. `Delivers via telegram to <account> (your default channel).`
- **Not set:** today's `delivery_choice_required` directive, unchanged. Its message gains one sentence: after the
  operator answers, tell them the choice can be saved as the default in the cockpit's Profile panel.
- **An explicit `notify` always wins** over the default.
- **Tool text** (`task.go:152`, `:169`): "If the operator did not specify one" becomes "If the request names no
  channel, omit notify: the operator's default channel is applied, and when none is set a choice directive comes
  back". The model stops being told to ask before calling.
- **Scope:** the default applies to every kind the model schedules (`reminder`, `agent_job`, `message`). System-seeded
  tasks keep their explicit routes.

### Cockpit

- `ProfilePanel.tsx` gains one select: "Channel for reminders and scheduled results", with the five routes plus "Ask
  me every time" (`''`). Labels go through i18n like the panel's other fields.
- The profile API carries the field both ways, like `timezone`.

### When the operator states the rule in chat

- Aura answers where to set it (the Profile panel) instead of answering "memorised". *My call*, since the memory fact
  is what promised a guarantee it could not give.
- This is a sentence in the profile block the prompt already carries, not a new tool.

## Part 2: the `message` task

### Scheduling

1. The model resolves the recipient before calling `task`: a WhatsApp JID or number through the WhatsApp tools, an
   address for email. When two contacts match, it asks the operator now, never at fire time.
2. It calls `task` with `action=schedule`, `schedule_kind=at`, `kind=message`, and a payload naming the channel
   (`whatsapp` | `email`), the recipient, and the text. Email also takes the account and the subject.
3. `task` maps the payload to the trusted recipe tool's arguments: `send_message {recipient, message}` for WhatsApp,
   `calendar {action: send_email, …}` for email. They must pass `messagedrafts.MergeApprovedArgs` validation.
4. It persists a message draft bound to the task, and the task as `pending_approval`. **The host mints the review
   pause itself**, keyed to the draft's own tool call id, the way `approvalPauseEnsurer` mints scheduled-task approvals
   through `MintApprovalPause`. The model does not relay it with `ask_user`. That keeps the pause host-authored, and it
   satisfies the `draft.ToolCallID == pending.ToolCallID` join that `ListMessageDrafts` and `pendingForMessageDraft`
   already require (`internal/runner/message_draft_resume.go:62`, `:187`). *My call*, after the builder check: a
   model-relayed `ask_user` gets a call id `task` cannot know, so the card could never find the draft.
   `PauseDraftID` (`internal/messagedrafts/pause.go:12`) learns the scheduled variant, or the pause reuses
   `message_draft` with the task id beside it (left to the builder).
5. The cockpit renders that pause as the existing message draft card, plus the fire time. The operator can edit the
   fields the draft already allows (recipient and text for WhatsApp; to, cc, subject and body for email), then
   approve or decline.
6. **Approve** fixes the effective arguments and their fingerprint, and flips the task to `active`. **Decline**
   declines the draft and cancels the task. Either way, the reply reports exactly that.
6b. **On Telegram** the same pause arrives as a read-only card: recipient, full text, fire time, and two buttons,
   Approva and Rifiuta. It follows Hermes's three-part wording: what will be sent, why approval is needed, and that no
   answer means nothing is sent. Approva runs the same approve as the cockpit card, with no overrides. To change the
   text, the card says to open the cockpit, where the same draft is editable until it is approved. The Telegram
   Sì/No push for scheduled tasks already exists (`cmd/aura/serve_scheduled_approval.go:155-171`). It is extended to
   show the draft and to resolve the draft, not the task: an approval that names only the task id must not activate a
   `message` task (item 8).
6c. **No answer is a no.** A draft still pending at its fire time expires. Nothing is sent, and the outcome says "not
   approved in time, not sent".
7. `schedule_kind` other than `at` is refused for `kind=message`, with a sentence saying recurrence is not supported.
   A fire time beyond the draft lifetime bound is refused with a sentence naming the limit.
8. **Only the draft card activates a `message` task.** Three paths flip any `pending_approval` task to `active` today,
   and none of them looks at a draft:
   - the approval sweep, which mints a `scheduled_task_approval` pause and, on a Telegram origin, pushes a Sì/No
     prompt (`internal/cron/deliver_approval.go:6-9`, `cmd/aura/serve_scheduled_approval.go:155-171`);
   - the resume hook for that pause (`cmd/aura/serve_scheduled_approval.go:106-118`);
   - the scheduler board's approve endpoint (`internal/agui/governance_write_scheduler.go:57-67`).

   Each refuses to activate `kind=message` from the task id alone. The sweep sends the message card of 6b instead of
   the bare Sì/No, and the other two answer "approve it from the message card".
   *My call*: otherwise the operator could again approve a message without seeing it.

### Firing

1. A `message` handler, which needs neither an LLM nor a tool search, claims the approved draft (one-send CAS).
2. It dispatches the effective arguments through the gateway with the reviewed claim, the way
   `ExecuteReviewedMessage` does (`llm_agent_message_draft.go:95`).
3. It records `sent`, `failed` or `uncertain` on the draft.
4. The outcome is the run summary, so the operator receives "sent to <recipient> at 08:25" or "not sent: <reason>" on
   the task's `notify` route, which defaults from Part 1.

### What the draft model needs

- **Lifetime.** A scheduled draft lives until its fire time plus a grace period, instead of 30 minutes
  (`llm_agent_message_draft.go:47`), within `Store.Create`'s 24-hour bound. A draft that has not fired by then
  expires, and the outcome says "expired, not sent".
- **A state between "reviewed" and "dispatching".** Today a draft goes from `pending` straight to `dispatching` when
  it is claimed. A scheduled one is approved long before it is sent, so it needs a state for that wait, and the CHECK
  constraints in 0134 change accordingly.
- **Binding.** The gateway's claim check binds conversation, tool call and request id
  (`message_draft.go:34-35`). The fire-time dispatch must satisfy the same binding from the draft row; it must not
  relax it.
- **Cancellation.** Cancelling the task before it fires declines its draft. Cancelling after the claim changes
  nothing; the send is the one-send boundary.
- **No edits after approval.** Changing the message means cancelling and scheduling again.
- **Deactivation cancels.** Deactivating the identity cancels its pending or approved `message` tasks and declines
  their drafts; it never pauses them (operator, 2026-11-11, "Confermo").

### `agent_job`

- Its tool text gains one sentence: "An agent_job cannot send a message to anyone other than the operator; to send a
  fixed message later, use kind=message."
- At fire time, a job whose send the gateway denies with `message_review` gets one deterministic line added to its
  outcome by the handler, read from the deny fact the gateway already records: "A message to someone else was not
  sent: a scheduled job cannot write to other people; schedule a reviewed message instead." *My call*: honesty must not
  depend on the model's final text.

## Part 3: every approval in the chat that creates the task, and no approval left hanging

Added 2026-11-05 at the operator's request ("si"), after asking why a task's approval does not simply appear in
the chat that creates it.

**Why.**
- Today a task that needs approval is persisted `pending_approval`, and `task` returns a directive the model must
  relay with `ask_user`. That is the fast path, and it depends on the model.
- When the model does not relay it, a sweep mints the pause after a 60-second grace and pushes a Telegram Sì/No
  (`internal/cron/deliver_approval.go:122-136`). The grace exists because minting mid-turn breaks the transcript.
- A second gap shows in the same export: an `agent_job` due on 2026-11-02 at 18:00 was still awaiting approval three
  days later. Nothing ever tells the operator that a one-shot missed its time unapproved. It stays "awaiting approval"
  for good.

**What changes.**
1. **The host parks the call.** A `task` schedule call that will need approval is parked by the agent before it runs,
   with the same in-turn withhold Part 2 uses for `kind=message`. The approval pause is keyed to the model's own call
   id and appears in the chat that made the call, in that turn, with no model relay. A call the identity's gateway
   policy denies is never parked: it runs and the gateway refuses it as today (operator, 2026-11-11, "Confermo").
2. **Which calls need approval** is decided before the call runs, from the arguments alone: every `agent_job`
   (AG-016), and any task whose `scoring.ComputeTaskTier` recommends a gate. The tier is a pure function of the
   arguments, so the decision before the call and the one inside the tool cannot disagree. A test pins that.
3. **The question names what will run:** kind, time in the operator's zone, and the goal or text, bounded the way
   today's question bounds the id. The operator no longer approves "task 01a19af5" blind. It ends "It will not run
   until you approve." in every surface (operator, 2026-11-11, "Confermo").
4. **The model-relayed path and its directive retire** for model-scheduled tasks. The sweep stays only as far as some
   other producer of `pending_approval` tasks still needs it. The builder inventories the producers and records what
   remains.
5. **A one-shot task whose time passes unapproved expires.** It leaves `pending_approval` for a terminal state, and
   the operator is told on the task's notify route: "<kind> for <time> was not approved in time and did not run".
   This is the same rule the plan already applies to `message` tasks (plan decision 8, confirmed by the operator).
   Recurring tasks awaiting approval do not expire; they keep the reminder.
6. **Tasks already stuck expire too.** The rule applies on its first pass after deploy to tasks like the 2026-11-02
   one, and each operator gets the notice.

## Constraints

- File size ≤ 600 LOC. `task.go` is 568 today, so the `message` scheduling goes in its own `task_message.go`, and the
  default lookup sits beside `task_timezone.go`.
- The PRD-first rule applies: the PRD amendment records what the E2E run measured, after it is measured.
- Coverage, race, mutation and integration gates as in `CLAUDE.md`. The draft lifecycle and the kind CHECK change
  need `db_integration` tests against real Postgres.
- The repository is public: verification files name no real contact, number or message text.

## Verification

Live on the lab VM, driven through the cockpit by the operator, with a dated file in
`docs/superpowers/verification/`:

1. **Default set.** With the default set to Telegram, "remind me in 10 minutes to X" from the cockpit asks nothing,
   replies naming the default, and arrives on Telegram.
2. **Default not set.** The choice is asked as today, and the reply mentions the Profile panel.
3. **Scheduled message.** A `message` to a contact the operator controls shows one review card with recipient, text
   and time. After approval it is sent once at the time, and the outcome reaches the default channel.
3b. **From Telegram.** The same request made on Telegram shows the read-only card. Approva sends once at the time;
   Rifiuta and silence send nothing.
4. **Decline and cancel.** Declining the card, and cancelling an approved task before it fires, both send nothing.
5. **Refused shapes.** An `agent_job` asked to write to someone else is either steered to `kind=message` at
   scheduling time or reported as "not sent" with the reason, and is never announced as a send.
6. **Approval in the creating chat.** An `agent_job` scheduled from the cockpit and one scheduled from Telegram each
   show their approval in that chat, in the same turn, naming what will run.
7. **Missed approval.** A one-shot left unapproved past its time is reported on its notify route as not run, and
   leaves the awaiting-approval list.

## Left to the builder

- The payload field names of `kind=message`, the grace period length, and the name of the new draft state.
- Whether the scheduled-message pause reuses the `message_draft` resume type or adds `scheduled_message` (the
  `resume_context` above is a sketch).
- How the cron handler gets the mounted recipe tool and the gateway at fire time.

## Out of scope, seen in the same export

These go to the operator separately, not into this work:
- one `agent_job` awaiting approval since 2026-11-02, so its recurrence was probably never created (Part 3 item 6
  will expire it and tell the operator; the recurrence it should have created still needs asking for again);
- fourteen one-shot reminders where one cron would do;
- an `agent_job` with an empty payload, paused since 2026-10-08;
- a branched turn that could not see the job's outcome, because `memory_recall` excludes the current conversation
  (`cmd/arcadedb-mcp/tool_memory_recall.go:207`).
