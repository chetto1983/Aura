# Daily briefing: one skill, one scheduled job, and a baseline to beat

Proposed on 2026-10-09 at `874a0b7d7` and agreed with the operator on 2026-10-30 (below), item 5
of the consolidation inventory (`2026-10-09-consolidation-best-of-design.md`), after reading
PMSync's `BriefingService` and Smart Today (`chetto1983/pmsync` at `042cc04c`) against Aura's
tree. Nothing here was run; every fact is a reading of code, and the acceptance section names the
measurements that decide.

Wave 2 does not open before wave 1 closes on its lab-VM acceptances and the operator decides
the six findings of `docs/superpowers/verification/2026-10-09-wave1-model-in-the-loop.md`.
This spec is written ahead of that, not landed ahead of it.

## Agreed with the operator, 2026-10-30

- **The design stands as written:** a skill and an `agent_job`, three live sources, nothing
  stored. The two timezone fixes ride in the same slice, and the blind five-day gate decides at
  four wins in five.
- **The board's "due first" order joins this slice** (P4 of the productivity read). It makes the
  overdue cards visible past the 32-card cap, and it is the baseline's board section. It reuses
  the cockpit's own definition: overdue is a deadline before now, in no particular zone and in
  any column (`web/src/board/boardModel.ts`). "Due today" is read as "due within 24 hours",
  because the tool has no zone of its own.
- **Unread mail is included.** The verification file names the route that ran, as acceptance 4
  already says; the local model is preferred until item 7 lands.

Plan: `docs/superpowers/plans/2026-10-30-daily-briefing.md`.

## Open question 1 does not hold as answered, read 2026-10-31 (`8af1b9f`)

The baseline was to be rendered "from the tool results that the same run recorded". The tree does
not keep them whole:
- **The ledger keeps a cut copy.** `aura.tool_invocations.result_preview` is capped at 2 KiB and
  redacted (`ResultPreviewCapBytes`, `internal/toolinvocations/redact.go`); a cut one ends in
  `…[capped]`.
- **The full result is kept only above the context cap.** The sidecar
  (`result_sidecar_path`) exists only for a result over `AURA_CONTEXT_PREVIEW_CAP_BYTES`, 30,000
  bytes by default.
- **What falls between is kept by neither.** A board list of a dozen cards, or the JSON of 20
  unread mails, sits between 2 KiB and 30 KB, so neither keeps it whole.
- **The reasoning trace does not help.** It is off by default (`AURA_REASONING_TRACE`), capped and
  redacted too, and its full mode needs an acknowledgement and encryption.

This is read in the code, not measured on a run. It says nothing about how often a real briefing's
reads exceed 2 KiB. `scripts/briefing_baseline.py` stands, because it renders from a bundle. How
the bundle is obtained was decided on 2026-10-31 (next section).

## Keeping a run's reads for the acceptance, agreed 2026-10-31

The operator chose to keep the job's results whole during the acceptance and nowhere else:
- **Opt-in per task.** A payload of `{"goal": ..., "keep_results": true}` makes that one
  `agent_job` write every tool call it finished to
  `<AURA_RUN_DIR>/tmp/agent_job-<run id>/results.jsonl`, with mode 0600. Each line carries the
  call id, tool, arguments, status, error, the result the tool returned, and the sidecar path when
  the result was too long for the context. No env var. A task without the flag writes nothing.
- **Deleted by the sweep that already exists.** `tmp/` is the retention class
  `AURA_RETENTION_TEMP_HOURS` (24 hours by default), swept by the daily `retention_sweep` at
  02:30. A capture lives one to two days. The acceptance extracts it the same morning.
- **The renderer reads it.** `scripts/briefing_baseline.py --results results.jsonl --zone <IANA>`
  picks the run's last successful `board list`, `get_calendar_events` and `get_emails`. A source
  the run did not read, or that failed, is null.
- **What it costs.** For one to two days the appliance's disk holds what the model read: event
  subjects, card labels, mail senders and subjects. The capture is not redacted, because the
  baseline must read what the model read. A capture the job cannot open fails the run: an
  acceptance day without its capture would be silently unscored.

## Re-read against the tree, 2026-10-30 (`6118144`)

The six findings were decided and verified on 2026-10-12 (`2026-10-12-gateway-clarity-design.md`).
Since `874a0b7d7` the tree moved; what that changes here:

- **An `agent_job` is no longer `pending_approval` by default.** `task` now routes only a
  destructive payload there (`internal/agent/tools/task.go:146,166`). A briefing job is active
  the moment it is scheduled. "The operator approves the briefing once" no longer holds. What
  still holds is that the agent schedules it only when the operator asks (open question 4).
- **Both timezone defects are still there.**
  - `ParseSchedule` still defaults an empty `tz` to `Europe/Rome`
    (`internal/cron/schedule.go:64-66`).
  - Nothing in `internal/cron/handlers` sets a `Location`, so the job's clock is still the
    process zone.
- **Read-only under the strict profiles is stronger.** A run with no responder is denied every
  Destructive call, and since 2026-10-30 that includes `memory_forget`
  (`2026-10-30-headless-memory-erase-design.md`). Under `dev` and `local_trusted` nothing changed.
  The gateway is still a no-op there, and the cockpit now says so (`policies_enforced`). There,
  read-only remains an instruction.
- **The board snapshot.** It is still column order with a cap of 32 cards
  (`internal/agent/tools/board.go:46`). The cards past the cap are counted, not shown, so an
  overdue card at position 40 is invisible to the briefing unless the skill asks again. The
  productivity read (P4, `2026-10-09-consolidation-best-of-design.md`) proposes a deterministic
  "today" order in the snapshot, which would also be this spec's baseline.
- **The fields of `get_emails`, measured 2026-10-31** against the published PIM and a fake mailbox
  (`verification/2026-10-31-pim-mail-e2e.md`):
  - an item is `id`, `accountId`, `subject`, `from`, `receivedDateTime`, `isRead` and
    `hasAttachments`;
  - `from` is a plain string;
  - there is no text preview.

  The five messages that ask for something are judged from sender and subject alone, as the skill
  says.
- **Telegram recipients.** Since 2026-10-28 a task names the Telegram accounts it reaches when
  two or more are linked (`2026-10-28-telegram-push-recipients-design.md`). The briefing
  inherits it with no work.
- **A scheduled worker runs its occurrence.** Since 2026-10-12 it gets an execution instruction
  before the goal and does not reschedule itself (`2026-10-12-scheduled-job-origin-design.md`).
  So "Write my daily briefing" is executed, not turned into a new schedule.

## The problem, measured in the tree

What PMSync built, read at `042cc04c`:

- `backend/src/modules/ai/services/briefing.service.ts` builds an eight-section JSON
  briefing (priority emails, calendar snapshot with conflicts, pending follow-ups, a summary
  of two or three sentences, stale contacts, draft suggestions, volume trends, focus blocks)
  with one `generateObject` call at temperature 0.1 over a 4,000-token input budget. The
  inputs are 20 unread emails with snippets cut to 200 characters, today's events in the
  user's timezone, 10 pending follow-ups, 5 stale contacts and counts.
- It never ran on a schedule. It ran on demand only (`GET /ai/briefing`, `POST /ai/briefing/refresh`), with
  a cache of three eight-hour slots per day. Delivery was HTTP only.
- The cockpit stopped showing it. The `BriefingWidget` was deleted (`399d901d`), the
  dashboard asks for the greeting nowhere, and `aiChatApi.getBriefing` is called only by
  tests. Forty-six commits touched it, among them "hallucinated briefings in chat"
  (`3d281235`), two widgets stuck loading (`7d880790`, `d6568da8`) and overdue deadlines
  left out (`99fb4d1e`).
- When the model fails, or returns what the code calls "low-signal" output, a deterministic
  rule-based briefing replaces it (lines 633-838). Smart Today (`dashboard-today.service.ts`,
  `today-scorer.service.ts`) is rule-based end to end. It sorts tasks, follow-ups, deals and
  events into four streams by due time, with no model call.
- Its queries are scoped by tenant, but its cache is keyed by user, and usage is billed to
  `'system'`.

What PMSync shows is that the dumb version was already there, kept as a fallback. Nobody
ever measured whether the model version beat it. That comparison is this spec's gate.

What Aura has, read at `874a0b7d7`:

- **Scheduling.** `task` (`internal/agent/tools/task.go`) schedules an `agent_job` on a cron
  (`schedule_kind=cron`, optional `tz`). Every `agent_job` is created `pending_approval` (AG-016,
  `task.go:243-250`), so the operator approves the briefing once, when it is set up.
- **Delivery.** `notify` is required: `telegram` pushes to the identity's chat, `stdout`
  appends to the conversation that scheduled it, and `whatsapp`/`email` are self-sends.
  Quiet hours (`AURA_SCHEDULER_QUIET_HOURS`) defer an `agent_job` summary to the end of the
  window (`internal/cron/dispatch.go:394-404`).
- **What a job runs.** The payload is `{"goal"}` only (`internal/cron/handlers/agentjob.go`).
  The goal becomes the single user message of a fresh `LlmAgent` built outside the runner. Its
  registry is the full one minus `swarm_spawn`, with no conversation history. The wall clock
  is `AURA_AGENT_JOB_MAX_DURATION_SEC` (default 600).
- **No skill catalogue in the job.** The runner renders the catalogue into the always-block
  (`internal/runner/runner_context.go:49`), and the job skips the runner. The `skill` tool is in the
  registry, so a goal that names the skill reaches it.
- **Defect, timezone.** A cron task with no `tz` runs in a hard-coded `Europe/Rome`
  (`internal/cron/schedule.go:64-66`), not in the identity's `identity_profiles.timezone`.
  The tool says "the scheduler default timezone"; no such default is read.
- **Defect, clock.** The job's agent leaves `Location` nil (`handlers/handler.go`,
  `newAgentWorker`), so its clock is the process zone, UTC in the appliance. The interactive
  turn uses the identity's zone (`internal/runner/runner_profile.go:30-39`). That fix was
  measured on 2026-08-16: a model reading a UTC clock answered Rome's time wrong.
- **Sources.** `board list` returns up to 32 cards, with due dates in UTC and a "more not
  shown" line, and has no due filter (`internal/agent/tools/board_actions.go:175-208`). The
  PIM exposes `calendar__calendar` with `get_calendar_events` (requires `timeZone`) and
  `get_emails` (`unreadOnly`, `count`, default 20). Both are classified Safe
  (`internal/agent/mcptools/bridge_risk.go`) and routed per identity through the context.
- **Memory.** `memory_digest` (`cmd/arcadedb-mcp/tool_browse.go`) has no time window: it
  says what is known, not what changed since yesterday.
- **Read-only is not enforced.** A skill's `allowed-tools` is parsed (`internal/skills/frontmatter.go:33`)
  and enforced nowhere. In a headless job, the gateway denies a Destructive call for lack of
  an approver (`internal/gateway/approve.go`), but only under the strict profiles. Under
  `dev` and `local_trusted` the gateway is a no-op. That is one of the six open wave-1
  findings, and it decides whether "the briefing never changes anything" is a property or a
  hope.

Not read: the fields `get_emails` returns, the Telegram message length limit on Aura's send path, and
whether a local model finishes the job inside 600 seconds.

## Decisions (proposed)

- **A skill and a scheduled job, no new subsystem.** An embedded skill, `briefing-aura`,
  says how to read the sources and write the briefing. The operator schedules it through
  `task` like any agent job, with the goal "Write my daily briefing: load the briefing-aura
  skill first." No new task kind, no new table, no cockpit page. PMSync's dedicated service
  and widget are what died.
- **Three sources, read live, nothing stored.**
  - Today's calendar.
  - The board's open cards.
  - Unread mail, but only when a PIM mail account is connected.
  
  No briefing table, no cache, no mail copy. The run ledger and the delivered message are
  the record. Memory stays out of this release: `memory_digest` has no time window, and
  "what is known" is not news.
- **Sections, in this order, empty ones omitted.**
  1. Up to three lines on what the day is.
  2. Today's events in time order, with overlaps named.
  3. Cards overdue and due today, then what sits in `doing`.
  4. Unread mail: a count, plus at most five messages that ask the operator for something.
  
  Cards and events are named by label, never by id (StickyFlow's rule). A quiet day is one
  line, not four empty headings. Draft suggestions, trends, stale contacts and focus blocks
  are not in this release: each is a claim of usefulness nobody measured.
- **Grounded or silent.** Every item in the briefing comes from a tool result of the same
  run. The skill says so, and the acceptance checks it mechanically. A source that fails is
  named in one line ("calendar unavailable"), never filled in.
- **Read-only by instruction, read-only by gateway where the gateway exists.**
  - The skill names only `board list`, `board search`, `calendar__calendar`
    `get_calendar_events` and `get_emails`, and it forbids `mark_email_read` and any write.
  - Under the strict profiles, the gateway backs that up for Destructive calls.
  - `mark_email_read` (Normal) and board writes stay possible in principle under every
    profile. The acceptance counts them, and a single one fails it.
- **Fix the two timezone defects, because the briefing is where they bite.**
  - A cron task with no `tz` takes the identity's profile timezone, then `AURA_TIMEZONE`,
    and `Europe/Rome` only as the last fallback.
  - The job's agent gets the identity's `Location`, through the same lookup the runner uses.
  - Both are general fixes to the scheduler, not briefing code.
- **The baseline is the gate (CLAUDE.md, BASELINE OR DROP).**
  - The baseline is the dumb briefing: the same three sources rendered by rule, with no model.
    - Events in time order.
    - Cards with a due date on or before today, by due date.
    - The unread count and the senders of the five newest.
  - The model briefing ships only if it beats the baseline on days that were not used to
    write the skill.
  - If it does not beat the baseline, the model briefing is dropped. Whether the baseline
    then ships as its own job is a separate decision.

## Shape

### Skill, `internal/skills/embed/briefing-aura/SKILL.md`

The frontmatter follows the other embedded skills (`name`, `description`). The body is under
100 lines and covers:

- **Order of the reads.** `tool_search` for `board` and `calendar__calendar`, then:
  - `board list`;
  - `get_calendar_events` for today, with the profile's timezone;
  - `get_emails` with `unreadOnly`, `count` 20.

  The three reads are independent, so they go in parallel.
- **Due dates.** Comparisons use the clock hint's local date. Board due dates are UTC, so
  the skill says to convert before comparing.
- **The 32-card snapshot.** When `board list` says more cards are not shown, the skill names
  that, and does not guess what is missing.
- **The four sections**, the label-not-id rule, the quiet-day line, the source-failure line,
  and a length cap of 1,500 characters.
- **Forbidden:** writes, `mark_email_read`, sending anything, and opening a mail body
  beyond what `get_emails` returns.

`internal/skills/builtin_names_test.go` gains the name.

### Scheduler, `internal/agent/tools/task.go`, `internal/cron/schedule.go`

The empty-`tz` fallback reads the identity profile through the same `Timezone(ctx,
identityID)` seam the runner holds. `ParseSchedule` stays pure: the tool resolves the zone
before calling it, and the `Europe/Rome` literal becomes the last resort, named as such in
the tool description.

### Job clock, `internal/cron/handlers`

`AgentDeps` gains the profile lookup, and `newAgentWorker` sets `Location` from the job's
identity. The lookup is `tools.LocationOrUTC`, the one the runner uses; no second zone parser.

### No cockpit work

The briefing lands where `notify` sends it: Telegram, or the conversation that scheduled it.

## Errors

| Case | Behaviour |
|---|---|
| PIM not connected | the mail and calendar sections are omitted, one line says so |
| one source errors | the briefing names it and goes on with the others |
| board unavailable (no pool) | same as one source erroring |
| the job exceeds `AURA_AGENT_JOB_MAX_DURATION_SEC` | the scheduler's existing failure path; three in a row pause the task (`AURA_SCHEDULER_PAUSE_AFTER_FAILURES`) |
| quiet hours at fire time | the scheduler defers the summary to the window's end, as for every job |
| a profile timezone that does not parse | `LocationOrUTC` falls back as it does for the interactive turn; the schedule uses `AURA_TIMEZONE` |

## Observability

No new metric. The run is an `agent_job_runs` row with its steps, duration and outcome, and
the tool calls are in the tool-invocation ledger. The acceptance reads both. A briefing that
needs a dashboard before it has a week of use is the PMSync path.

## Testing

- **Unit.** The tz fallback order (profile, `AURA_TIMEZONE`, last resort), with an identity
  in a zone other than Rome. The job agent's `Location` is the identity's. The builtin name is
  listed.
- **`db_integration`.** A cron task scheduled with no `tz` for an identity whose profile says
  `America/New_York` stores a `next_run_at` that matches New York's wall clock.
- **Not unit-testable, and therefore the acceptance.** What the briefing says.

## Acceptance (lab VM, one dated file in `docs/superpowers/verification/`)

1. **Clean run.** On the lab VM, with a real calendar, a real mailbox through the PIM and a
   board with real cards, the briefing scheduled at the operator's hour arrives on Telegram,
   in the operator's timezone, on five working days. Zero runs fail and zero runs are deferred
   by mistake.
2. **Grounding, checked mechanically per run** against that run's tool results:
   - every event and card the briefing names exists in them, so zero fabrications;
   - every event today and every card due on or before today in them appears in the
     briefing, so zero omissions;
   - zero write calls in the run.
3. **Baseline, out of sample.**
   - The skill is frozen before day one.
   - Each day the operator receives the model briefing and the baseline, in random order,
     unlabeled, and picks the more useful one.
   - The model briefing ships if it wins at least four of five days and passes item 2 on
     all five.
4. **Cost.** Steps, tool calls, wall time and tokens per run, on the release's local model
   and on one cloud route, recorded rather than gated.

If the cloud route reads unread mail, the snippets leave the appliance. Until item 7 (PII
tokenization) lands, the verification file says which route ran.

## What this spec does not establish

That a model writes a better briefing than a sorted list: that is acceptance item 3, and
PMSync's history is a reason to expect it may not. That the operator wants a briefing every
day: five days of use, not a year. That mail headers through the PIM are enough to pick the
five messages that ask for something: `get_emails`' fields are not read yet. That the
read-only property holds under `dev` and `local_trusted`: that waits on the operator's
decision about inert policies.

## Open questions, answered 2026-10-09

Answered by the engineer. Each stands unless the operator objects.

1. **How is the baseline produced?** From the tool results that the same run recorded, by a
   measurement script under `scripts/`, run on the lab VM for the acceptance week. That gives
   the same inputs and no second read. It does not ship unless the baseline wins and the
   operator decides to ship it.
2. **Does the cockpit get a briefing page?** No. PMSync's widget is the measured outcome of
   that idea. The conversation and Telegram are where the operator already reads.
3. **Does the briefing remember yesterday's items ("still overdue for the third day")?** Not
   in this release. That would be state, and state needs a reason that five days of use can
   supply.
4. **Who schedules it?** The operator, in conversation ("every weekday at 7:30 on
   Telegram"). The agent proposes it at most once and does not create it unasked, because
   every `agent_job` needs approval anyway.

## Out of scope

- PMSync's draft suggestions, volume trends, stale contacts and focus blocks.
- Smart Today's streams and scores.
- A briefing table or cache.
- Memory in the briefing.
- A cockpit page.
- Web push.
- Per-identity quiet hours. Today they are global; a second identity in another zone is the
  evidence that would change that.
