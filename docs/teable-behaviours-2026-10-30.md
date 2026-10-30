# Teable: behaviours for Aura, 2026-10-30

Asked by the operator on 2026-10-30: which of `github.com/teableio/teable`'s functions Aura should
take, then a code read of four candidates and a search for projects whose licence Aura can use.
- **Source.** A shallow clone of `main` at `b6ae0a1` (2026-10-28), read only. Paths below are
  relative to the clone's root. Aura's side was read the same day at `aabc5f5`.
- **Licence.** Split, per its `LICENSE`. `apps/nestjs-backend` and `apps/nextjs-app` are AGPL-3.0:
  behaviour only, never code. Everything under `packages/` is MIT: reusable with attribution. Aura
  is MIT and ships as an appliance image, so a dependency must be MIT, Apache-2.0, BSD, ISC or
  MPL-2.0. GPL, AGPL, SSPL, BSL, source-available and commercial tiers are behaviour references
  only.
- **What it is.** A spreadsheet-style database on PostgreSQL: grid, form, kanban, gallery and
  calendar views, formulas, aggregation, record history, realtime collaboration, AI chat, AI field
  filling, automations and an app builder.
- **How it was read.** Four agents, one per candidate, each reading Teable, Aura and the
  alternatives. Their central claims were re-checked by hand: the Community Edition stubs and
  upgrade walls, `validateDiff`, `SetByLLM`, the absence of `response_format` and of `pg_notify`,
  the scheduler kinds, the `agent_job` payload and the qrels categories. Licences were read from
  each project's `LICENSE` file at the tag or commit named, never from a badge.

## Verdict

Teable is a product; Aura is not becoming one. Its two most interesting functions for Aura, AI
field filling and automations, are **not in the Community Edition**: the endpoints are stubs and
the screens are upgrade walls. So from Teable itself Aura gets contracts and a catalogue, not a
mechanism. The mechanisms worth taking come from Aura's own tree, which already has most of each
piece, and from a few MIT/Apache projects named below.

| # | Candidate | Teable CE | Aura's real gap | Fit |
|---|---|---|---|---|
| T1 | Event-triggered automations | absent (EE) | a conditional schedule: poll, diff, run with input, dedup | builds on `internal/cron`, no new dependency |
| T2 | AI field filling over the library | absent (EE) | `response_format`, typed coercion, a per-field run | REUSE: llama.cpp, openai-go, jsonschema-go already in |
| T3 | Grouping and aggregation | present, AGPL | Monetary is free text; no aggregation API | ArcadeDB `GROUP BY` + `bojanz/currency` + SVAR Grid tree/footer |
| T4 | Calendar view of the board | present, AGPL, FullCalendar | no calendar; cards carry one `due_at` | SVAR React Calendar (MIT) |

## T1. Automations triggered by an event

**Teable.** The engine is not in the Community Edition.
- `apps/nextjs-app/src/features/app/automation/Pages.tsx:44-55` renders only an
  "enterprise feature" alert; `WorkFlowPanel.tsx:24` renders that alert.
- `packages/openapi/src/automation/workflow/create.ts` types the trigger as `z.unknown()`; no CE
  controller handles workflows.
- `packages/v2/core/src/ports/ButtonClickWorkflowService.ts:15`: "Enterprise automation implements
  this port".

The catalogue can be read from MIT assets only, not from running code:
- **Triggers** (icons under `packages/icons/src/components/`): button, record created, updated,
  created or updated, record matches conditions, form, schedule, webhook, email received.
- **Actions**: AI, create/get/update record, HTTP request, script, send email, logic.
- **The email trigger polls** with a minimum interval over IMAP (`sdk.json:1106`, `:1117`).
- **"Routines"** mix schedules and events and disable themselves after N failures
  (`sdk.json:1196-1205`).

What the Community Edition does ship, MIT, is a generic transactional outbox:
- tables `domain_event_outbox`, `_delivery`, `_inbox`
  (`packages/db-data-prisma/prisma/migrations/20260831120000_add_domain_event_outbox/`);
- a worker with one delivery row per consumer and 12 attempts, a `FOR UPDATE SKIP LOCKED` claim, a
  120 s lease, dedup on `(consumer_id, event_id)` and 14 days of retention
  (`packages/v2/adapter-table-repository-postgres/src/projection/DomainEventOutboxWorker.ts:81-85,
  229-230, 291, 337`);
- its only durable consumer is record validation (`ValidationInboxDurableProjection.ts:19`).

A behaviour from AGPL code, idea only: failure alerts on a widening ladder, 1 to 5, then 10, 20,
40 (`notification/failure-alert-backoff.ts:9-26`).

**Aura.**
- The scheduler is time-only: `at`, `every`, `cron` (`internal/cron/schedule.go:29-34`), `every`
  no shorter than 5 minutes, a 30 s tick.
- The only on-demand fire is `RunTaskNow`, which sets `next_run_at` to now
  (`internal/db/queries/scheduler_tasks.sql:129-134`), classified Risky by the gateway.
- There is no event source:
  - zero `pg_notify` or `LISTEN` in `internal/`;
  - no inbound hook route;
  - Telegram long-polls into an interactive turn;
  - the PIM is pull-only;
  - CocoIndex rescans every 60 s and writes nothing to Postgres.
- An `agent_job` carries only `{"goal"}` (`internal/cron/handlers/agentjob.go:56-59`): no
  per-run input, no per-event idempotency, no deterministic condition that keeps the model asleep
  when nothing is new.
- `aura.ingestion_events` is a transactional event log written in the same CTE as the state
  change, used today only by `swarm_delegation`.

**The real gap is not an event bus.** An appliance has no public endpoint for pushes, so "a mail
arrives" is a poll with a watermark, which is what Teable's own email trigger does. Aura needs a
conditional schedule on the cron it already has: a model-free poll, a diff against a watermark,
a dedup row per (trigger, item), and an `agent_job` started with the new items as input only when
there are any. Decisions already on record:
- **Webhooks:** rejected for no demand and no consumer (§Karakeep, "Not taken").
- **NOTIFY:** a wakeup only, correctness stays with polling
  (`2026-06-29-durable-swarm-messaging-design.md:537-539`).

| Project | Licence, where read | Offers | Fit |
|---|---|---|---|
| Postgres LISTEN/NOTIFY through pgx | MIT, v5.11.0 | `Conn.WaitForNotification`, `pgxpool.Conn.Hijack` | already a dependency; wakeup only |
| jackc/pgxlisten | MIT, `12b9242` | a LISTEN dispatcher with reconnect | only if LISTEN is chosen; low maintenance |
| riverqueue/river | MPL-2.0, `4f79f7b` | transactional insert, unique and periodic jobs | rejected: a second queue beside the cron, already ruled out by the 2026-07-04 survey |
| Watermill, watermill-sql | MIT | Postgres pub/sub, outbox forwarder | rejected: a bus for one consumer |
| Hatchet | MIT, `ddfa6d0` | event keys, scoped CEL filters, HMAC webhooks | behaviour reference: the best "event key plus filter" model, but a separate server |
| DBOS Transact Go | MIT, `d85375d` | durable workflows, dedup id | reference: it would replace the cron's substrate |
| Temporal, Trigger.dev | MIT, Apache-2.0 | clusters and platforms | rejected: separate services, Temporal already excluded |
| standard-webhooks | Apache-2.0, `ab68441` | a signing spec with a Go library | only when a receiver has a consumer |
| emersion/go-imap v2 | MIT, `dd8c632` | IMAP IDLE | rejected: bypasses the PIM and its OAuth providers |
| Inngest, Windmill, n8n, Activepieces EE | SSPL, AGPL, Sustainable Use, commercial | | behaviour only |

**Real case and measure.** Item 4, inbox triage feeding the board.
- **Candidate:** every 5 minutes a model-free step asks `search_emails` for mail after the
  watermark, records (trigger, message id), and starts an `agent_job` with the new ids only
  when there are any.
- **Baseline, no code:** an `agent_job` `every 15m` with the goal "triage the mail that arrived
  since the last run".
- **The run:** on the lab VM, one real mailbox, five working days. The truth is the mailbox's own
  list at the end of the window. The run measures:
  - model calls and cost in empty windows;
  - arrival-to-card latency, p50 and p95;
  - mails left without a card, and duplicate cards.
- **The gate:** zero missed, zero duplicates, zero empty model calls, and a cost below the
  baseline by a margin written into the spec before the run.
- **Prerequisites:**
  - Item 4's mail-table decision. The dedup table *is* per-email state, so this trigger forces
    that decision.
  - Item 1's per-identity tool policy, because mail content becomes untrusted input to a headless
    agent.

## T2. AI field filling over the whole library

**Teable.** The fill is a paid feature.
- `apps/nestjs-backend/src/features/field/open-api/field-open-api.controller.ts:284-298`:
  `auto-fill` returns `{taskId: null}` and `stop-fill` returns `null`.
- `record-open-api.controller.ts:576-592` returns an empty task id.
- `Task` and `TaskRun` in `packages/db-main-prisma` have no consumer.
- `FieldAiConfig.tsx:68-69,196` wraps the field setting in a Pro upgrade wall.

So prompting, concurrency, retries and partial failure cannot be read. The MIT contract can:
- **Per-type config** (`packages/core/src/models/field/ai-config/*.ts`): extraction,
  classification, tag, rating and customization, with `modelKey`, `isAutoFill` and
  `attachPrompt`, and `onlyAllowConfiguredOptions` on single select.
- **The bulk run** (`packages/openapi/src/field/auto-fill-field.ts:10-36`): `mode: all |
  emptyOnly`, scoped by view or filter, with `rowCount`, `processedCount` and `isLimited`.
- **A cap** of 1,000 rows per task, then a relaunch for the rest.

Two anti-patterns for Aura:
- `all` overwrites, with no suggestion state;
- the Community Edition typecast creates new select options unless told not to
  (`record/typecast.validate.ts:212-262`).

**Aura.**
- No model writes a field: `SetByLLM` is reserved and "nothing writes them yet"
  (`internal/docfields/values.go:18-27`), and the fields spec gates phases 2 and 3.
- The extractor reads spreadsheets only: key|value pairs above the table
  (`internal/documents/filecard/table.go:159-175`), with fixed Italian keys. On PDF, DOCX and
  scans only label rules run.
- No typed coercion: `CustomMonetary` exists, the pass writes the raw text
  (`internal/docfields/pass.go:117-118`), and `validateDiff` checks only `document_date`
  (`records.go:285-298`).
- Library passes exist only on (re)indexing, in batches of 200, and on a newly saved rule.
- Review is already right: a suggestion never overrides a kept or rejected value, with evidence
  and history.

**The gap.**
- (a) **A schema-bound call.** `internal/llm/openai_compat` sets no `response_format` (zero
  hits). Everything else is installed: openai-go v3.86.0 has `OfJSONSchema`, llama.cpp honours
  `json_schema`, and `google/jsonschema-go` is in `go.mod` and used by `internal/elicit`. REUSE,
  no new dependency.
- (b) **Typed coercion in Go** for date, monetary, integer and closed-list select. An id the model
  was not shown is rejected.
- (c) **An on-demand run for one field**, `emptyOnly`, with a cap, a stop and counters.
- (d) **Partial failure.** One error in `suggestAll` skips `markPassed`, so the whole batch of 200
  is re-read; with a model that is 200 calls per timeout.
- (e) **A memo** keyed on the file's bytes plus the model.
- (f) **The local route only**, because organisations and VAT numbers are masked PII classes
  (`2026-10-13-pii-model-egress-design.md`).

| Project | Licence, tag | Offers | Fit |
|---|---|---|---|
| llama.cpp | MIT, v0.5.0 | `json_schema`, GBNF, `response_format` | already deployed |
| openai-go | Apache-2.0, v3.86.0 | `OfJSONSchema` | already a dependency; wire it |
| google/jsonschema-go | MIT, v0.4.3 | schema from Go types | already a dependency |
| instructor, instructor-go | MIT, v1.17.0, 1.2.3 | retry plus validation | rejected: duplicates (a) and (b) |
| outlines, xgrammar, guidance | Apache-2.0, Apache-2.0, MIT | in-process constrained decoding | rejected: duplicates llama.cpp's grammar |
| BAML | Apache-2.0, v0.226.3 | DSL and codegen | rejected: a toolchain for one call |
| LangExtract | Apache-2.0, v1.7.1 | every value anchored to a character span | behaviour: drop a value not found in the text, store the span as `Evidence` |
| invoice2data | MIT, v1.0.1 | YAML regex templates, typed amounts and dates | reference for the baseline |
| paperless-gpt, paperless-ai | MIT, v0.31.0, v3.0.9 | custom-field append/update/replace with manual review | references: apps, not libraries |
| sparrow, unstract | GPL-3.0, AGPL-3.0 | | rejected: licence |

**BASELINE OR DROP.**
- **The office set cannot judge this.** The extractor scores 14/14 there because the generator
  writes exactly the layout the extractor reads; it stays a non-regression test.
- **The right set is the labelled bench library** of `2026-10-18-document-fields-mirror.md`:
  113 invoice PDFs, 1,327 legal acts, 1,500 Gazzetta notices, 600 SEC contracts. Its loader is not
  in the repository, so step 0 is to make it reproducible. Amounts have no out-of-sample gold yet:
  write it before the code.
- **Protocol:** a calibration and test split by source.
  - Baseline: header pairs, label rules and a date regex sweep, all written on calibration only.
  - Candidate: the same, then the phase-2 classifier, then the model only on fields left empty.
- **Metrics, per field, on test:**
  - suggestion precision and coverage with normalised matching (NameKey, ISO date, minor units
    plus currency);
  - operator corrections per 100 documents;
  - set recall on "which documents" questions;
  - seconds per document on the lab VM. Arithmetic only: at the measured 60.4 tok/s, 100 output
    tokens are already 1.7 s of decoding per document.
- **Gate, per field:** coverage up with a bootstrap CI above zero; model precision at least the
  rules' precision, because a wrong value behind a filter hides the document; zero operator
  values touched; zero invented ids accepted on an injection set.

## T3. Grouping and aggregation in the document views

**Teable.**
- **Function catalogue (MIT).** 20 statistic functions
  (`packages/core/src/models/aggregation/statistics-func.enum.ts:1-22`), allowed per field type
  (`statistic.ts:5-105`): sum, average, min and max on numbers; earliest, latest and range on
  dates; count, empty and unique on text. Chosen per column; at most three group levels
  (`view/group/group.ts:37-42`).
- **SQL (AGPL, behaviour).** Aggregates wrap the view's own filtered query, add `GROUP BY` and drop
  the `ORDER BY`. One query per group level. Group headers come from a `GROUP BY` with `COUNT(*)`
  capped at 5,000 points, with an "Unknown" group when the groups sum below the row total. Dates
  group by the field's format with `DATE_TRUNC` in the field's zone; numbers with `ROUND` to the
  field's precision. No cache and no materialised view.
- **A currency model not to copy.**
  - Currency is a display symbol on a number field, stored as `DOUBLE PRECISION`: one currency per
    field, sums in floating point.
  - Multi-value numbers are parsed with `[^0-9.+-]`
    (`packages/v2/.../TableRecordAggregationSql.ts:46`), which reads the Italian "1.234,56" as
    1.23456.

**Aura.**
- Fields live in ArcadeDB, one database per identity. Only the four fixed fields are projected on
  `IndexedDocument`; custom fields live in the `DocumentFields.values` list.
- Counts per type, counterparty and tag are computed in Go over the result. One ArcadeDB `GROUP BY`
  is already in use (`pass_read.go`).
- The gaps, in order:
  - (a) **The blocker: Monetary is not typed.** The spec says "an ISO 4217 currency plus an
    amount", but `validateDiff` takes any non-empty key for a custom field
    (`internal/docfields/records.go:292`), the extractor copies the raw text (`pass.go:118`), and
    the cockpit takes and shows free text. No sum is correct until an amount has a canonical form
    (minor units as LONG plus a currency).
  - (b) **No aggregation API.** Saved views have only table, compact and cards (`views.go:30`), the
    allowed query keys include no grouping, and custom-field filters are `eq` and `unassigned`
    only.
  - (c) **The agent filters only on fixed fields and tags.**
  - (d) **The list is paged at 50, at most 200.** A client-side sum adds one page.
- **Where it runs: ArcadeDB, on the same `libraryScope` predicate the list uses.** The fields
  exist only there, isolated per identity by the server; the ingest has no Postgres connection;
  the filters are already ArcadeDB predicates. Two variants to measure: the Go counting loop
  extended, or a `GROUP BY` with `sum(amount_minor)`. The manual documents `sum`, `min`, `max`,
  `count` in aggregate mode, LONG overflow to DECIMAL and NULL on an empty group (hence
  `ifnull`); a `GROUP BY` on an expression is not documented.

| Project | Licence, version | Offers | Fit |
|---|---|---|---|
| ArcadeDB SQL | Apache-2.0, 26.10.2 | `GROUP BY`, aggregate functions, `UNWIND` | yes: already the store |
| bojanz/currency | MIT, v1.5.0 (on cockroachdb/apd, Apache-2.0) | exact decimal amounts, ISO 4217, `Add` refuses mixed currencies, locale-aware parse of "1.234,56" | yes: the canonical Monetary |
| shopspring/decimal | MIT, v1.6.0 | decimals only | fallback |
| @svar-ui/react-grid | MIT, 2.7.6; DataGrid has no PRO edition per svar.dev/licenses | tree rows and per-column `footer`; no row grouping or aggregation | yes for the cockpit: server-computed groups as parent rows, totals in the footer |
| @tanstack/table-core | MIT, 8.21.3 | grouped row model, aggregation functions | no: works on loaded rows, sums in float |
| AG Grid | community MIT, enterprise commercial | row grouping is enterprise-only | no |
| Glide Data Grid | MIT, 6.0.3 | no row grouping API | no |

**The questions.** The aggregation questions of the office set are **q04 to q07** (category `AGG`
in `scripts/fixtures/document_retrieval_eval/qrels.json`), not q09 and q18, which are `FILTER` and
`TEMPORAL`. Today they are already answered from the ledger `contabilita_2026.xlsx`, 20 of 20
(`2026-10-17-workbook-row-passages.md`). On that corpus BASELINE OR DROP drops the feature.

**The measure.**
1. A library without the ledger and `riepilogo_annuale.txt`, at scale: hundreds up to 4,045
   invoices, Italian-format amounts, EUR plus a second currency.
2. Gold written before code: q04–q07 plus at least ten questions per counterparty and per month
   (counts, earliest and latest dates).
3. Two arms. A is the real agent with `document_search` and `document_open`; B is the aggregation
   over reviewed Monetary fields.
4. The gate:
   - B exact to the cent, and A below B;
   - no sum across currencies;
   - `GROUP BY` latency within 2× an unfiltered list;
   - the extractor canonicalises every invoice's "Totale documento";
   - identity isolation holds.

Whether "fatturato" means total or taxable is the operator's definition to fix.

## T4. A calendar view of the board

**Teable.** FullCalendar 6.1.15 (`core`, `daygrid`, `interaction`, `react`) with date-fns-tz
(`apps/nextjs-app/package.json`).
- **Mapping (MIT schema).** `startDateFieldId`, `endDateFieldId`, `titleFieldId` and a colour
  config (`packages/core/src/models/view/derivate/calendar-view-option.schema.ts:24-37`). With one
  field missing, the other stands in for it.
- **Month view only, all-day events only, no recurrence.**
- **Loading per visible range.** One daily collection per day of the range, generated in SQL in
  the start field's zone, with at most ten ids per day; "+N" opens a list filtered to that day.
  Invalidation is throttled to once every 2 s.
- **Drag keeps the time of day.** A drop rewrites start and end with the original time; resize
  handles the exclusive end (+1 day on read, −1 on write); drag is disabled on a computed date
  field.
- **One time zone**, the start field's, both for display and for the day buckets.

**Aura.**
- **One date, no start.** A card has `due_at timestamptz`
  (`internal/db/migrations/0141_work_board.up.sql:38`). The only index is
  `(board_id, column_id, position)`. The whole board is loaded with no range filter.
- **Rescheduling already has its write:** `PUT /api/board/cards/{id}` with `deadline`, `null`
  clears it. The agent's `board` tool takes `due` in RFC 3339.
- **The cockpit:** the deadline is edited in the Kanban editor, an overdue card turns red, and one
  filter has two values, `overdue` and `week`. There is no calendar.
- **The board's design rules a calendar out of this release.** "A due date that passes? The board
  colours it, nothing else" and "StickyFlow's calendar and Gantt views" are out of scope
  (`2026-10-09-work-board-design.md:325-327, 335`). This spec's "Not taken" pointed at a
  "prd.md §16 reading" of the SVAR Calendar; `prd.md` has no such reading. The pointer now names
  T4.
- **The gap.**
  - **Backend:** none for a first version, since every card is already on the client. A
    `start_at` only if resize proves useful. The day buckets need a zone: the browser's or
    `identity_profiles.timezone`. On the lab VM "tomorrow" was saved as 09:00Z.
  - **Web:**
    - a Kanban/Calendar switch in its own file (`BoardWorkspace.tsx` is 380 LOC);
    - a card-to-all-day-event mapping;
    - a drop that calls the existing update and keeps the time;
    - the existing filters;
    - a count of undated cards;
    - CSS ordering in `styles/svarWidgets.ts`, because the calendar's `all.css` loads fonts from
      `cdn.svar.dev`.

| Component | Licence, version | Free edition | Fit |
|---|---|---|---|
| SVAR React Calendar | MIT, 2.7.1 (`license.txt` in the tarball) | day, week, month; drag to move, resize and create; editor; Willow themes; iCal. PRO only: agenda, year, resources, timeline, editable recurrence, **time zones**, **dynamic loading**, undo, export | high: same `react-core` and theme as the board; its REST provider has the Kanban's shape. Young (first release 2026-05-13); pulls `react-editor` 2.7.0 beside the Kanban's 2.6.0; the provider does `new Date(item.end)` unchecked, so `end` is always sent. 18.5 KB gzip plus an 11 KB store |
| FullCalendar | core, react, daygrid, interaction, timegrid, rrule MIT; premium plugins commercial, CC BY-NC-ND or GPLv3 | month, week, day, list; drag and resize; rrule; zones by plugin | medium: proven, but a second visual language with no SVAR theme; 55 KB gzip core |
| Schedule-X | MIT, 4.9.1; premium packages unverifiable | month, week, day, list; IANA zones; recurrence | low: a Preact runtime inside React; 58 KB gzip |
| react-big-calendar | MIT, 1.20.0 | month, week, day, agenda; drag addon | low: dated SASS, no dark theme |
| @event-calendar/core | MIT, 5.18.1 | resources and timeline free | low: Svelte runtime |

**The measure.** On the lab VM's live board:
1. **Today:** cards per source, with a date, and open with a date, plus the hour distribution of
   `due_at`.
2. **Seven daily snapshots** of `(id, due_at)`. `UpdateBoardCard` overwrites, so reschedules are
   counted by diffing them.
3. **Density:** the most dated cards on one day.
4. **Use of the existing `due` filter** in `board_views`.

The calendar must beat the dumb baseline already there: the `due` filter plus a list sorted by
date. A proposed threshold, to fix in the spec before the run: if fewer than about a third of open
cards carry a date, or no card is rescheduled in the week, the calendar is dropped.

## Not taken, with the reason

- **The spreadsheet database itself**: tables, fields, formulas, typecast and SQL query on
  Postgres. A product outside prd.md §1; GridCraft already edits sheets.
- **Realtime collaboration, presence, comments, record-level undo/redo.** One operator per
  identity; Aura's field history already records who set each value.
- **Plugins.** Aura has MCP servers and skills.
- **"Millions of rows."** No Aura measurement asks for that scale.
- **The App Builder's long-lived container per published app.** Infrastructure cost with no
  measured need; the operator's call if ever.
- **Form view and button fields.** No consumer.
- **Teable's code from `packages/`, although MIT.** It is TypeScript bound to Teable's SDK and
  Postgres schema; what transfers is the contracts above, re-specified in Go.

## What this read does not establish

- **Teable EE.** How Teable fills fields or runs automations: that engine is not in the
  repository. T1's triggers are inferred from icons and strings.
- **Execution.** Nothing was installed or run. Latency, bundle size, React 19 and React Compiler
  compatibility, the Willow rendering of the SVAR Calendar, the PIM's `receivedDateTime` filter
  and ArcadeDB `GROUP BY` on expressions are all unmeasured.
- **Licences.** They were read at the tag or commit named, without transitive dependencies or a
  legal review. SVAR's PRO list comes from the tarball's readme; `grid-store` 2.7.5 carries an
  undocumented `IProConfig` type.
- **Demand.** None of T1 to T4 has its live measurement. The card counts, the mailbox run, the
  scaled invoice library and the bench-library loader are all still to do.
