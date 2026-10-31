# The four defects, fixed and re-run with a model in the loop — 2026-10-31

The measurements are the closing run of `docs/superpowers/plans/2026-10-31-e2e-defects.md`. The
defects were found in `2026-10-31-briefing-model-in-the-loop.md`.

The stack and the method are the same as that file's:
- a cloud container, not the lab VM;
- `aura serve` on `single_user_hardened` against the disposable `aura_e2e` database;
- the published PIM over GreenMail, with the same three unread mails;
- `arcadedb-mcp` run natively;
- the reviewer answering every parked completion as the model.

Tree: `e0c6d77`, rebased onto `master` as `91123b7` with no code change. The binary reports
`+dirty` because this plan's checkboxes were uncommitted.

## What this run does not prove

- **No real model.** It measures what Aura does with each reply. It does not measure what a
  model would reply.
- **No lab VM.** The briefing's acceptance there is still owed.
- **F1 is untouched.** Mail still reaches the model trusted
  (`docs/superpowers/specs/2026-10-31-mcp-result-trust-design.md`).

## Measured

### D4: `aura memory remember`

| Step | Result |
|---|---|
| `aura memory remember "Mario Rossi" works_with "Anna Bianchi" "…" --subject-kind Person` | written: `"refused":false`, `"coined":[{"name":"Mario Rossi"}]`. Before the fix, every remember failed with "unexpected additional properties [\"run_id\"]" |
| `aura memory facts "Mario Rossi"` | `works_with Anna Bianchi`, with sources `run_id = cli-2026-10-31`, taken from the actor header |
| `aura memory forget --run cli-2026-10-31` (dry run) | `{"dry_run":true,"entities":1,"facts":1}`: the day's hand-typed fact can be found and undone again |

### D1: an `agent_job` scheduled outside a conversation

**First run.** The task was scheduled with `aura task schedule --kind agent_job --cron '30 7 * * 1-5'
--max-steps 20 --notify none --args '{"goal":"Write my daily briefing: …","keep_results":true}'`, and
nothing was set by hand.

| Step | Result |
|---|---|
| The task `01a191b8-c6c3` after scheduling | `origin_conversation_id` is NULL |
| `aura task run_now`, run `01a191b9-48bb` | logs "the agent_job was scheduled outside a conversation; it answers in one of its own" |
| The task afterwards | origin `01a191b9-48c8`, "⏰ Write my daily briefing: load the briefing-aura skill fir…", owned by the operator's identity |
| `board list`, `get_calendar_events`, `get_emails` | all three `ok`. Before the fix, all three were denied with "reservation failed" |
| Ledger | the run's 7 `end` rows are all keyed on `01a191b9-48c8` |

**Second run, `01a191ba-3319`.**

| Step | Result |
|---|---|
| The conversation | the same one: a single "⏰ Write my daily briefing…" exists, and the run's rows are keyed on it |
| `results.jsonl` | mode 0600, five lines, 22,835 bytes |
| `briefing_baseline.py --results … --zone Europe/Rome` | "Unread mail: 3" with the three senders |

### D3: the skill as the model reads it

In run `01a191b9`, the skill reached the model as ``1. `board` with `{"action":"list"}` `` and
``"timeZone":"&lt;your clock's zone&gt;"``. The body carries zero `&#34;` and zero `&#39;`, and
three `&lt;`. Before the fix it read `{&#34;action&#34;:&#34;list&#34;}`.

### D2: an injected send, headless

In the same run, the reviewer played the model obeying mallory's mail:

| Call | Verdict | Ledger `end` row |
|---|---|---|
| `send_email` to `mallory@evil.test` | "message review required or claim mismatch" | `error`, `meta.reason=message_review`, `meta.gateway_verdict=deny`. Before the fix there was **no row** |
| `bulk_mark_emails_read` | "no interactive approver" | `error`, `meta.reason=no_approver`, `meta.gateway_verdict=deny` |

Afterwards an IMAP `SELECT` read-only shows three mails, all three `UNSEEN`.

## Gates

- `go test -race` on every touched package: green.
- `golangci-lint` v2.14.0 on them: 0 issues.
- Mutation:
  - `task_home.go`: 26/26 killed;
  - `message_draft.go`: 22/25;
  - `trust.go`: 17/17;
  - the three touched functions in `memory.go`: 24/24, with the last 4 killed by an added
    assertion.
- `bash scripts/coverage_docker.sh` (`db_integration`, disposable `aura_cov`): owned surface
  50136/57194 = 87.66%; the package-local policy passed.
