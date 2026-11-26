# MCP elicitation, end to end with the model in the loop (2026-11-26)

Plan: `docs/superpowers/plans/2026-09-25-mcp-elicitation-question-card.md`, Task 10, run locally instead of on
the lab VM. The PRD still described the production wiring as decline-and-surface (`prd.md`, MCP section),
while Tasks 1-9 had given a cockpit run a form. This run measures which path takes a request, and what the
server and the model receive. PRD amendment #225 records it.

## Tree and stack

| | |
|---|---|
| Tree | master `0056374`; the binary reports `a30b15138`, the same production code (docs-only commits between) |
| Stack | a cloud container's `aura-postgres`, `aura-arcadedb`, `aura-llama-embed`; disposable database `aura_e2e`, 134 migrations |
| Daemon | `aura serve`, profile `single_user_hardened`, env of `.claude/skills/aura-model-in-the-loop/env.e2e.sh` |
| Model | the hand endpoint of the `aura-model-in-the-loop` skill: every model answer below was written by hand |
| Operator | the cockpit's API through `cockpit.py`: the form's answers were posted to `POST /agent/runs/{runID}/elicitations/{id}`; no browser |
| Server | `@modelcontextprotocol/server-everything`, installed through `POST /api/governance/mcp` (`npx -y`), as Task 10 step 3 says |

## The mount

| Check | Result |
|---|---|
| Install probe | `ok (13 tools)` |
| Daemon mount | 14 tools, protocol `2025-11-25` (the classic path, not multi-round-trip) |
| `trigger-elicitation-request` | in the model's deferred manifest |
| `trigger-url-elicitation` | absent: Aura does not advertise URL mode |
| `aura mcp tools everything` (CLI) | 13 tools, without `trigger-elicitation-request`: the server registers it only for a client that advertises elicitation, and the CLI listing does not |

## The form, from a cockpit run

| Check | Result |
|---|---|
| The request reaches the run | an `aura.elicitation` frame in the run's stream 18 ms after the call, and the same question in `GET /agent/runs/{runID}/elicitations` |
| Its header | server `everything`, tool `trigger-elicitation-request`, the server's message |
| Fields | 13, only `name` required, 8 with defaults, two multi-choice enums bounded 1 to 3 |
| Deadline | the call plus 300 s (22:13:03 → 22:18:03 UTC) |
| An invalid answer (no `name`, `integer` 500, four fish) | `422`, by field: `name: required`, `integer: out_of_range`, `titledMultipleSelectEnum: too_many`; the form stays open |
| A valid answer | `202 delivered`; the server receives `accept` with the given fields **and every unanswered field that has a default**; fields without one (`birthdate`, `email`, `homepage`) are absent |
| What the model reads | the server's result, in the tool call's answer |
| Decline | the server receives `decline`; the model reads "User declined" |
| No answer | at the deadline the card closes (`aura.elicitation_resolved`, `cancel`, `expired: true`) and the server receives `cancel` |

### Past 60 seconds

The call is handed to the background after 60 s, as every slow tool call is: the model's answer says it is still
running, not cancelled, and to `tool_poll` it once. The form stays open until its own deadline.

| Case | Result |
|---|---|
| Answered at 80 s | `delivered`; the server receives `accept`; `tool_poll` returns its result; the conversation gets a notification turn ("finished with status completed after 20483 ms", counted from the handoff) |
| Never answered | at 300 s the server receives `cancel`; `tool_poll` returns it; a notification turn follows 44 s later |

## Without a cockpit run

| Run | Result | Daemon log |
|---|---|---|
| A scheduled `agent_job` in the daemon | the server receives `decline` at once; the job completes | `mcp elicitation not surfaced: no channel owns this identity` |
| `aura shell` (in-process) | `decline` within about 250 ms | `mcp elicitation not surfaced: no channel registry bound` |

So decline-and-surface is the fallback, not the production path: it takes a request only when no cockpit run can
be asked, and here it surfaced nothing, because no channel was linked.

## Harness errors

The model's answers are written by hand, and one was wrong: the notification turn after the expired form
(request 14) was answered with a reasoning-tier reply, `{"tier":"low"}`, by a loop of mine that did not check the
request's kind. That turn's text is not a model answer. It changes no measurement above: the expiry, the `cancel`
and the `tool_poll` result had been read before it.

## What this does not show

- **The cockpit's screen.** No browser ran: the step counter, "Use default", the "Choose 1 to 3." hint, the Review
  step, the countdown and a reload in the middle of the form (Task 10 step 4) are not measured. The API the card
  calls was.
- **A surfaced fallback.** No Telegram account was linked, so no fallback reached a channel.
- **The lab VM**, its updater, and the counts of its other mounts (Task 10 steps 1-2).
- **The multi-round-trip path** (protocol `2026-07-28`): server-everything negotiated `2025-11-25`.
- **A real model.** Whether a model keeps waiting after the 60 s handoff or ends its turn is its choice; here it
  was mine.
