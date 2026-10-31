# Productivity repositories: behaviours for Aura, 2026-10-30

Asked by the operator on 2026-10-30: "a search of the best productivity repositories Aura can take
ideas from". Four reads ran in parallel, one per area:
- mail and communication;
- tasks and time;
- personal AI assistants;
- meetings, calendar and people.

- **Source.** About forty repositories, cloned shallow on 2026-10-30 and read, never run. Each
  row names its commit. Stars come from GitHub search on the same day. Aura was read at `de1c428`.
- **Licence.** Aura is MIT and ships as an appliance image. Code may come only from MIT,
  Apache-2.0, BSD, ISC or MPL-2.0. AGPL, GPL, EPL, ELv2 and BSD-with-branding are behaviour only.
  Every licence below was read from the repository's `LICENSE` file at the commit named, unless
  marked "API".
- **Excluded, read before.** PMSync, StickyFlow, Teable, Paperless-ngx, Karakeep, OpenDots, SVAR,
  Rag_Full_Version, Market_MCP, sacchi_agent.
- **Checked by hand.** The central claims about Aura were re-checked: `message_drafts`'s two
  argument columns, the board snapshot cap, the STT request fields, the scheduler's empty-summary
  skip, and the gateway path of `memory_forget`. So were the licences of cal.diy, Inbox Zero,
  OpenClaw, tududi, nametag, Meetily, Hermes, Super Productivity, Obsidian Tasks, meerkat-crm,
  Scriberr and anarlog.

## Verdict

None of these projects measures its assistant against a dumb baseline. Inbox Zero is the only one
with a serious evaluation method: hash-locked dev/test splits and an exact McNemar test against
the previous prompt. Its datasets are private and its evals run only by hand. So nothing learned
is taken on a repository's own claim. Each behaviour below has a gate on Aura's lab VM.

Aura is ahead on memory, on recall over time, and on a briefing with a baseline and a grounding
check. What the year's best productivity tools add is mostly **deterministic structure in front
of the model**:
- rules before a classifier;
- a hash before a run;
- a ranked "today" before a plan;
- interval algebra before a model guesses free slots.

That is cheaper than anything learned, and it is the baseline every learned piece must beat.

The read also found a defect in Aura, outside productivity: `memory_forget` destroys memory and
is not gated, even in a run with no operator. It has its own design,
`docs/superpowers/specs/2026-10-30-headless-memory-erase-design.md`.

## The repositories

| Area | Repository | Commit | Licence | Stars | Best at | Evidence |
|---|---|---|---|---|---|---|
| mail | elie222/inbox-zero | `56303d1` | AGPL-3.0 plus added terms | 12.9k | rules before the model, learning from corrections, the draft-to-sent diff | method strong, numbers absent |
| mail | Mail-0/Zero | `3613273` | MIT | 10.9k | per-thread workflows | no AI eval; idle January to May |
| mail | cloudflare/agentic-inbox | `48039bb` | Apache-2.0 | 8.6k | injection scanner, draft check | 0 tests |
| mail | langchain-ai/agents-from-scratch | `603fc7a` | MIT | 2.4k | ignore/notify/respond triage | a tutorial |
| mail | Lakshay1509/NeatMail | `e824bf8` | ELv2 | 147 | "I owe / they owe" promises closed by a reply | 0 tests |
| tasks | super-productivity | `5b0c189` | MIT | 23.3k | spreads tasks into the day's free time; due date apart from planned day | 313 parser cases; nothing on usefulness |
| tasks | tududi | `0d39b9d` | MIT | 3.5k | a model-made day plan checked deterministically; end-of-day review | 16 tests on the plan contract |
| tasks | obsidian-tasks | `32cc7d3` | MIT | 4.1k | documented end-of-month recurrence; three dates | 45 tests on recurrence, urgency and dates |
| tasks | taskwarrior | `61e5429` | MIT | 6.1k | an urgency formula with coefficients | tests check arithmetic |
| tasks | vikunja | `d489971` | AGPL-3.0 | 5.9k | a recurring task reopens on completion; `DoneAt` | good |
| tasks | huly | `acd8045` | EPL-2.0 | 28k | a task owns time blocks that are calendar events | none found |
| assistants | openclaw | `c0c068e1` | MIT | 393k | a proactive job that skips when there is nothing; event-bound intents | QA scenarios on a mock model |
| assistants | hermes-agent | `7020ea3e` | MIT | 259k | a model-free cron gate; headless memory writes may only add | wiring probes |
| assistants | open-webui | `f7ac7ee` | BSD-3 plus branding clause | 157k | RRULE automations, memory CRUD | none |
| assistants | khoj | `ae229ca` | AGPL-3.0 | 38k | natural-language automations; research | FRAMES, for research only |
| meetings | calcom/cal.diy | `a5ee96f` | **MIT** (cal.com relicensed in April 2026) | 49.6k | interval algebra for free slots with buffers | 100+ deterministic tests |
| meetings | fastrepl/hyprnote, now anarlog | `a82755f` | MIT except `enterprise/**` | 9.7k | summary from notes plus transcript; per-person cards | format-only LLM harness; DER harness |
| meetings | Zackriya/meetily | `a2cb62e` | MIT | 32.3k | map-reduce summary; action items with timestamps | mechanics only |
| meetings | rishikanthc/Scriberr | `a353078` | MIT | 3.1k | diarization merged into transcript segments | API tests |
| people | mattogodoy/nametag | `a3a31ab` | AGPL-3.0 | 1.1k | a reminder anchored to the last contact | 43 tests on due logic |
| people | fbuchner/meerkat-crm | `2df5d9c` | MIT | 300 | recurrence counted from completion | 8 tests |

Seen from metadata only (licence from the GitHub API, code not read): Plane, Leantime, Kanboard,
Wekan, tasks.org, Taskchampion, Monica, Twenty, Rallly, easyappointments, LibreChat, nanobot,
mem0, Leon. Reor is archived; Amurex has been idle since 2025-05-27.

## Behaviours worth taking

Paths are relative to each clone; Inbox Zero's are under `apps/web/`.

### P1. The draft-to-sent diff as the metric and the signal for item 9

- **Inbox Zero.**
  - Every draft logs `DraftSendLog.similarityScore` (`prisma/schema.prisma:1412-1431`).
  - A sent mail counts as from a draft above 0.7 similarity
    (`utils/reply-tracker/draft-tracking.ts:27`).
  - Nothing is learned above 0.95 (`utils/ai/reply/reply-memory.ts:260-279`).
  - Memories are typed fact, preference or procedure, and scoped global, sender, domain or topic
    (`extract-reply-memories.ts:280-320`).
  - A style profile needs at least 10 pieces of evidence (`reply-memory.ts:35-36`).
- **Aura.** The signal is already stored and nobody reads it. `aura.message_drafts` keeps
  `original_args` and `effective_args` (`internal/db/migrations/0134_message_drafts.up.sql:13-16`),
  written by `ClaimSend`.
- **Gate.** Define item 9's "acceptance rate" as the share of drafts at similarity ≥ 0.95 plus the
  mean similarity, measured over equal weeks before and after.
- **Constraint.** Extraction sends draft and sent text to a model, so it waits on item 7.

### P2. A job with nothing new sends nothing and calls no model

- **OpenClaw.** The default proactive job is `idleOnly` and `skipIfScratchEmpty`
  (`src/cron/default-proactive-job.ts:23-50`), so an empty checklist skips before the model. A run
  that starts may answer `NO_REPLY` (`src/auto-reply/tokens.ts:4`).
- **Hermes.** `_apply_monitor_gate` hashes a script's output before any agent machinery; an
  unchanged hash costs no model call (`cron/scheduler.py:1556-1590`). A failing source raises an
  alarm, never a "no change".
- **Aura.** Only an empty summary is skipped (`internal/cron/dispatch.go:393-395`), and the model
  is given no sentinel for silence.
- **Where it lands.** T1 (a hash per task as a third arm beside the watermark and the dedup table)
  and item 4. Not item 5, whose design keeps the quiet-day line.
- **Gate.** Five days, an "alert me if…" job against "always deliver". The run must show fewer
  empty messages, zero false silences and zero model calls on unchanged ticks.

### P3. Rules before the model, failing toward the harmless outcome, for item 4

- **Inbox Zero.**
  - `checkColdEmailGuards` (`utils/cold-email/is-cold-email.ts:53-138`) checks in order: the
    allow-list, the same organisation, a learned per-sender pattern (the user's exclusion wins), and
    prior contact. The model runs only when none decides.
  - Any uncertainty counts as "there was contact", so mail is never blocked by doubt
    (`has-prior-contact.ts:4-11`).
  - Prior contact is one provider query, `(from:X OR to:X) before:T`, with at most 4 results: no
    table (`utils/gmail/message.ts:196-204`).
  - A sender becomes a fixed rule after at least 3 consistent threads
    (`app/api/ai/analyze-sender-pattern/route.ts:22,127-179`).
  - Its small model is left off because evals showed it could not tell a personalised pitch from
    a real contact (`is-cold-email.ts:160-161`).
- **Corrections.** A label removed by hand is a signal. Only a second removal, outside a 15-minute
  window, becomes a fixed exclusion. Ten or more removals in 15 minutes count as cleaning, not
  correcting (`utils/rule/record-label-removal-learning.ts:19-23,66-67,114-140`).
- **Aura.** No per-mail decision before the model. `DeleteCard` leaves no trace
  (`internal/board/cards.go:362`), so a correction to a triage card is lost.
- **Gate.** Inside T1's run: the share of mail decided without a model, and calls and cost per
  mail, against "never contacted and no List-Unsubscribe = cold" on a week the operator labels. The
  model stays only if it beats the rule, paired McNemar.
- **Unverified.** Whether the PIM accepts Gmail search syntax and returns sent mail and thread
  structure.

### P4. A deterministic "today" list in the `board` tool

- **tududi.** Groups in this order: tagged today, overdue, due today, in progress, the rest
  (`backend/modules/daily-plan/ranking.js:18-24`). Inside a group it sorts by priority, due date,
  then id.
- **Aura.**
  - The snapshot follows column and position order and stops at `boardSnapshotCards = 32`
    (`internal/agent/tools/board.go:46`, `board_actions.go:176-208`). The cards beyond the cap are
    counted, not shown, so an overdue card at position 40 is invisible unless the model asks.
  - The due filter exists only in the cockpit.
- **Where it lands.** Item 5's baseline, and item 3a.
- **Gate.** Five days on the lab VM's live board: how many overdue or due cards reach the text the
  model reads. Column order against tududi's groups, with Taskwarrior's urgency
  (`src/Task.cpp:1907-1921`) as a third arm that is dropped if it does not beat "due date
  ascending".

### P5. Transcripts with segments, and action items that become cards

- **Aura.**
  - The STT client sends only `file`, `model` and `language` (`internal/multimodal/stt.go:146-161`)
    and gets plain text back.
  - The pinned sidecar (`compose.yaml:1168`), per its README (read, not run), already offers
    `verbose_json` segments, word timestamps, diarization (`WHISPER_DIARIZATION=true`) and SSE
    streaming. The gap is the client and its 120 s timeout, not a component.
  - A card links only a conversation and a task (`internal/board/cards.go:40-56`).
- **Scriberr.** Assigns each segment the speaker of maximum overlap
  (`internal/transcription/unified_service.go:826-880`).
- **anarlog.** Measures DER against RTTM references (`crates/pyannote-local/src/bin/diarization_eval.rs`).
- **Meetily.**
  - Its template asks for owner, deadline, segment and timestamp per action item
    (`frontend/src-tauri/templates/standard_meeting.json:15-20`).
  - Its prompt uses only the source and ignores instructions inside the transcript
    (`frontend/src-tauri/src/summary/processor.rs:229-253`).
  - One chunk failing twice fails the whole summary.
- **Gate, transcription.** A real 45–60 minute Italian meeting must complete. Measure its CPU
  time, then the DER on 10 hand-labelled minutes against "one speaker". Diarization drops if it
  misses the margin written before the run.
- **Gate, action items.** Five real meetings, with the operator's list written before the output.
  The model must beat a keyword rule ("entro", "mando", "faccio") fixed on meeting 1 and scored on
  meetings 2–5. Every item must cite a timestamp that exists.
- **Where it lands.** A new item in prd.md §11, then items 3 and 3a.

### P6. "Stay in touch", anchored to the last contact Aura observes

- **nametag.**
  - Reminds when the days since the last contact exceed the interval, and not again before 0.9 ×
    the interval. Whole days, so a daylight-saving change cannot shift it
    (`lib/reminders/due-dates.ts:89-135`).
  - Its journal moves `lastContact` only forward.
- **meerkat-crm.** Counts recurrence from completion (`backend/services/reminder_service.go:486-525`).
- **Monica, the most-starred PRM, does not do this.** On main it has dated reminders only.
- **The common limit, and Aura's chance.** In all of them `lastContact` is typed by hand. Aura can
  derive it from:
  - WhatsApp `get_last_interaction` (`internal/agent/mcptools/bridge_risk.go:107`);
  - mail and event attendees through the PIM;
  - Person entities in memory.
- **Aura.** A reminder delivers fixed text (`internal/cron/handlers/reminder.go:15-40`), and the
  briefing design excludes neglected contacts.
- **Gate, after item 5's baseline.** Twenty people over four weeks, fixed cadence against cadence
  anchored to the observed contact. Measure useless reminders flagged by the operator and the
  accuracy of the derived `lastContact`.

### P7. Free slots as interval algebra, only if the model errs

- **cal.diy (MIT).** Busy times are widened by before and after buffers
  (`packages/features/busyTimes/services/getBusyTimes.ts:135-136,264-268`). Working hours then
  have them subtracted and are intersected across hosts
  (`packages/features/schedules/lib/date-ranges.ts:354-452`).
- **Aura.** The model computes slots from `get_calendar_events`. The PIM fork `aura/pim-sidecar` at
  `4e0f518` does not expose `find_available_times`, which upstream documents and the 2026-06-16
  fork design kept.
- **STOP BEFORE BESPOKE.** Re-exposing the upstream action comes first; a Go port of the algebra
  comes second.
- **Gate.** Here the model is the learned part and the algebra is the dumb baseline. Thirty real
  requests, counting slots that overlap an event or a buffer or miss the zone. With zero model
  errors nothing is built. The same gate counts double bookings before `create_event`.

### P8. A due date apart from a planned day and from "all day"

- **Super Productivity.** `dueDay` (date only) and `dueWithTime` exclude each other
  (`src/app/features/tasks/task.model.ts:110-149`).
- **Obsidian Tasks.** Weighs due, scheduled and start dates differently
  (`src/Task/Urgency.ts:15-43`).
- **Aura.** One `due_at timestamptz`. The tool asks for RFC 3339 with a time and shows it in UTC,
  so "by Friday" becomes an instant with an invented hour. On a calendar (T4) a UTC instant can
  fall on the wrong day.
- **Gate, measure first.** On the live board: cards with an invented hour, and day errors in the
  Europe/Rome briefing. Amend only above zero.

### Smaller

- **Event-bound intents** ("when I mention X, remind me Y"). OpenClaw: 3 fires, a 24 h cooldown,
  90-day expiry, and a keyword match with no model call
  (`extensions/memory-core/src/standing-intents-kernel.ts:185-191,286-296`). The dumb baseline is
  already there: the instruction stored as a fact, plus relevance preload. Demand first: how many
  "when…" requests a week of logs holds.
- **End-of-month recurrence.** Obsidian Tasks documents three semantics
  (`docs/Getting Started/Recurring Tasks.md:110-172`). Aura's cron is stateless, so it does not
  drift, but `0 9 31 * *` skips five months a year. The `task` tool's description does not mention
  `L`; whether gronx v1.20.5 supports it was not re-checked by hand. Gate: twenty phrases, the
  cron the model writes against the expected dates over 12 months.
- **`done_at` on cards.** Vikunja sets `DoneAt` on completion and clears it on reopening. Aura has
  no closing timestamp and no card history, so "what did I close this week" cannot be rebuilt.
  Demand first.
- **The eval method.** Inbox Zero locks splits by content hash (`__tests__/eval/harness/split-lock.ts:47-60`)
  and compares every new prompt with the kept previous one by exact McNemar
  (`harness/stats.ts:178`). It is BASELINE OR DROP in code, usable for P1 to P8's gates.

## Not taken, with the reason

- **Auto-schedulers in the style of Motion** (FluidCalendar). Seven hand-weighted factors and a
  7-day horizon; no test names the scheduler; it writes to the external calendar.
- **Donetick's adaptive frequency.** With an empty history a task done early gets a date in the
  past, and its test checks only that the result is not null. A learned piece with no baseline.
- **A quick-add date parser.** In Aura the model reads dates. If a measure on Italian phrases finds
  errors, go-dateparser (BSD-3, `it` locale) is the reuse candidate, proposed before any code;
  olebedev/when has no Italian.
- **OpenClaw's nightly "dreaming".** A model call every night, proven only by a canary.
- **A model gate on notifications** (Khoj, nanobot). A second call, and Khoj notifies anyway on
  error. P2 comes first.
- **Memory designs of Khoj, LibreChat, Open WebUI, Hermes and OpenClaw.** Hard deletes, key upserts,
  capped files and Markdown: weaker than Aura's bitemporal facts with supersede authority.
- **Zero's vectorised mail and Inbox Zero's digest and `EmailMessage` tables.** A local copy of the
  mailbox or of summaries of it, which this inventory rules out. Bulk unsubscribe waits on item
  4's decision; the PIM already unsubscribes one mail at a time.
- **agentic-inbox's yes/no injection classifier.** Unmeasured, and weaker than refusing writes in
  a run with no operator.
- **Live call capture, meeting bots, availability polls, booking pages, CRMs.** Desktop or product
  surfaces outside prd.md §1; polls and CRMs, if ever, as mounted MCP servers.
- **anarlog's voiceprints.** Biometric data while item 7 is open.
- **Pomodoro, time tracking, worklogs, gamification, team project tools** (Plane, Kanboard, Wekan,
  Leantime, Huly's tracker). Aura does not watch the operator work, and sprints are a team product.

## What this read does not establish

- **Execution and usefulness.** Nothing was run. Every gate above presupposes a board and a mailbox
  in real use on the lab VM, and wave 1 is not closed.
- **Repositories' quality claims.** No eval result is in any repository. Inbox Zero's suites skip
  without their private datasets, and anarlog publishes no DER.
- **The PIM.** That it accepts Gmail search syntax, returns sent mail and thread structure, and that
  the running PIM image comes from `4e0f518`.
- **The STT sidecar.** Its features come from its README, not from the pinned digest
  `2026.10.28.0351`. Italian diarization quality and the CPU cost of an hour are unmeasured.
- **Coverage.** The search is not exhaustive below about 150 stars. Licences of the projects seen
  only from metadata come from the GitHub API, and no licence had a legal review.
