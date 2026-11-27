# MCP elicitation: the held window, and the cards on screen (2026-11-27)

Plan: `docs/superpowers/plans/2026-09-25-mcp-elicitation-question-card.md`, Task 10, steps 4 to 6, run locally
in a browser instead of on the lab VM. It follows `2026-11-26-elicitation-model-in-the-loop.md`, which found
that a form left open past 60 s moved its call to the background, and which saw no screen. The fix
(`fix(agent): hold the background window while a server's form waits`) runs the 60 s window on the call's
pausable clock. This run measures it, and looks at the cards.

## Tree and stack

| | |
|---|---|
| Tree | branch `ccr-d742c0b3-jvadt4-elicit-window` at `fcb99fe53` (master `03e78459c` plus the fix); the binary reports `v1.2.1-0.20261127062327-fcb99fe53137` |
| Stack | a cloud container's `aura-postgres`, `aura-arcadedb`, `aura-llama-embed`; disposable database `aura_e2e`, 134 migrations; no memory sidecar, so `/readyz` answered `503 memory_unavailable` and the cockpit showed "Degraded" |
| Daemon | `aura serve`, profile `single_user_hardened`, env of `.claude/skills/aura-model-in-the-loop/env.e2e.sh` |
| Model | the hand endpoint of the `aura-model-in-the-loop` skill: every model answer was written by hand |
| Operator | Chromium through Playwright, 1280×1000, on the cockpit build the binary embeds (`internal/webui/dist`), signed in through the login form |
| Server | `@modelcontextprotocol/server-everything`, installed through `POST /api/governance/mcp`: probe `ok (13 tools)`, mount 14 tools, protocol `2025-11-25` |

## The window holds

Three calls to `everything__trigger-elicitation-request`, each answered on screen.

| Call | Operator | Ledger (`aura.tool_invocations`, end) | What the model read next | Log |
|---|---|---|---|---|
| `c2` | filled the form, reloaded at step 3, filled it again, submitted | `ok`, 209 480 ms | the server's result, in the same turn | `mcp elicitation resolved action=accept fields=13 reason=answered` |
| `c3` | declined at step 12 | `ok`, 43 776 ms | "User declined to provide the requested information." | `action=decline reason=answered` |
| `c4` | left it alone | `ok`, 300 029 ms | `"action": "cancel"` | `action=cancel reason=expired` |

- At 06:32:11 UTC, 80 s after `c2` opened, no request was parked for the model. On 2026-11-26, with the same
  server and no fix, the model received the background notice at 60 s.
- No call went through `tool_poll`, and no notification turn followed any of them.
- The server received, for `c2`, the two typed fields and every unanswered field with a default (`firstLine`,
  `integer` 42, `legacyTitledEnum`, `number` 3.14, both multi-selects, both single-selects). The skipped fields
  without a default (`boolean`, `email`, `homepage`) were absent.
- `grep` of the daemon log for the two typed values (`Ada Lovelace`, `1815-12-10`): 0 lines.

## The form on screen

| Check (Task 10 step 4) | Result |
|---|---|
| Header | the `everything` chip, the tool name, and a countdown: "Aura cancels in 4:53" 7 s after the call ([01](2026-11-27-elicitation-window-and-cards/01-step1-page.png)) |
| The server's message | "Please provide inputs for the following fields:" under every step's title |
| Step 1 | "Step 1 of 14", the one required field (`name`, titled "String"), Next grey until it is filled |
| Optional fields | "Skip" where there is no default (steps 2, 3, 4, 6); "Use default" instead where there is one (steps 5, 7 to 13) |
| Multi-choice | "Choose 1 to 3." on steps 10 and 12. On step 12 (five options): four chosen, Next grey; three, Next live ([04](2026-11-27-elicitation-window-and-cards/04-multi-four.png)). None chosen also leaves Next live: the field is optional, so it goes unanswered and its default is sent. Step 10 has three options and cannot pass its bound |
| Review | step 13's forward button reads "Review"; step 14 lists every field with its value or "Not given", a "Change" per row, and Submit only there ([02](2026-11-27-elicitation-window-and-cards/02-review.png)) |
| Decline and expiry | the card turns into "Declined." and "Expired: cancelled automatically." while the run goes on ([06](2026-11-27-elicitation-window-and-cards/06-expired.png)) |

### A reload in the middle

At step 3 the page was reloaded. The card came back with its question and the same deadline (3:06 left),
fetched by `GET /agent/runs/{runID}/elicitations`, which is in the page's resource list after the reload.
Whether the run's replay also carried it is not shown: a stream still open does not appear in that list.

The answers typed before the reload did not come back: the card restarted at step 1, empty. The spec asks that a
reload bring the form back, which it does; it says nothing of the answers.

The first-run profile dialog opened again after the reload, over the card, and took the clicks until it was
closed.

## The ask_user cards (Task 10 step 6)

| Shape | On screen | The model read |
|---|---|---|
| choice | "Input required", radio rows, Answer grey until a row is chosen (`disabled` true, then false) ([07](2026-11-27-elicitation-window-and-cards/07-ask-choice.png)) | `Ristorante` |
| clarification | a text field and Answer ([08](2026-11-27-elicitation-window-and-cards/08-ask-clarification.png)) | `Sei persone` |
| approval with the model's options | "Approval required", rows Sì and No, and **Answer**, not Approve ([09](2026-11-27-elicitation-window-and-cards/09-ask-approval-options.png)) | `Sì` |
| a gateway-gated destructive call | `skill_manage delete` was withheld (`gateway_approval_required`, tier `destructive`) and relayed through `ask_user` as the gateway asked: a red card, three scope rows (once, this conversation, always) and **Approve** ([10](2026-11-27-elicitation-window-and-cards/10-gateway-destructive.png)). Declined | "user declined to answer" |

- `write_file` in the workspace was not gated: `gateway_verdict=allow`, tier `normal`. A mutating card without
  the destructive variant was not seen.
- The approval card with the model's options carries the line "Review the scope and consequence before
  continuing." though it has no scope rows.
- A screenshot 4 s after that approval was asked showed the tool row and no card; the card was on screen when
  looked at again about 20 s later. The delay was not timed.

## Also seen

- After each run ended, its form cards were no longer in the thread: the tool rows stay, the cards and their
  receipts do not.
- The browser console held only the `/readyz` 503s (no memory sidecar) and four 404s on
  `/api/conversations/{id}/title` before the title existed.
- After the final answer of a turn, a reasoning-tier request on the conversation's first message was parked; it
  was answered `low` and nothing followed it.

## What this does not show

- **A real model.** Every model answer was mine.
- **A call that keeps running after its form.** That the window counts again once the hold ends is shown by
  `TestBackgroundCallsCountTheWindowAgainOnceTheHoldEnds`, not here: server-everything returns as soon as the
  form resolves.
- **The lab VM**, its updater, and the counts of its other mounts (Task 10 steps 1 to 3).
- **The turn outside the cockpit** (step 7): no Telegram account was linked, so no fallback reached a channel.
- **The multi-round-trip path**: the server negotiated `2025-11-25`.
- **A server on the SDK's default 60 s request timeout.** server-everything sets 10 minutes for this tool.
