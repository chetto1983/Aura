# Daily briefing: one skill, one scheduled job, and a baseline to beat

Proposed on 2026-10-09 at `874a0b7d7`, item 5 of the consolidation inventory
(`2026-10-09-consolidation-best-of-design.md`), after reading PMSync's `BriefingService` and
Smart Today (`chetto1983/pmsync` at `042cc04c`) against Aura's tree. Not agreed with the
operator: every decision below is a proposal for review. Nothing here was run; every fact
is a reading of code, and the acceptance section names the measurements that decide.

Wave 2 does not open before wave 1 closes on its lab-VM acceptances and the operator decides
the six findings of `docs/superpowers/verification/2026-10-09-wave1-model-in-the-loop.md`.
This spec is written ahead of that, not landed ahead of it.

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
