# Lab VM: two drafts in one round, a server's form, and the composer (2026-11-27)

The day's fixes had been measured locally with a model written by hand
(`2026-11-27-message-drafts-cockpit.md`, `2026-11-27-elicitation-window-and-cards.md`,
`2026-11-27-cockpit-card-fixes.md`). This run repeats them on the lab VM with the VM's own model, and
covers plan `2026-09-25-mcp-elicitation-question-card.md` Task 10 steps 1 to 7 there. It found three cockpit
defects: two fixed on master in `cf117b2dd` and `cdf53e7a3` and measured again on the VM running them, and
one fixed after the run (the doubled reply after an answer, below).

## Tree and stack

| | |
|---|---|
| VM | the lab VM, updated only by its updater: `2ae083107` at the start, `be40b4d5d` for steps 1 to 5, `cdf53e7a3` for the re-check, step 6 and step 7 (`/healthz` `build_commit`) |
| Model | the model the VM is configured with: every answer below is a real, billed turn |
| Cockpit | the bundle the VM serves, signed in with the operator's own session in Chrome, 1718×1214 CSS px |
| Who drove it | Claude, through Claude in Chrome, with the operator's consent for the paid turns, the install below and the Telegram messages. The operator did not type in the cockpit or in Telegram |
| Telegram | Telegram Web in the operator's Chrome, the chat with the VM's own bot (its history holds the mail-watch notice of this run's 300-A) |
| Mailboxes | the operator's two accounts on the VM's PIM (`calendar` mount); both mails went only to the operator's own addresses |
| Form server | `@modelcontextprotocol/server-everything`, installed from Governance → MCP (`npx -y @modelcontextprotocol/server-everything`) for this run, and removed after it |

## The graph's double click (#303)

The inspector column (310 px) is laid out before any selection: the grid read `288 | 1 | 1119 | 310`. A
single click on `#27:1` (degree 9) opened the inspector and moved nothing: canvas 1119 px, zoom 0.515, pan
and node position unchanged. A double click at a human pace then reached the expand handler, which answered
"Tutti i 9 vicini sono già nel grafo" (75 nodes, 69 edges before and after).

## Two sends in one round, one edited (#300, amendment #227)

One message asked for two mails "in parallel", each from one account to the other. The model called
`calendar__list_accounts`, then `calendar__send_email` twice in one round, and the cockpit showed **two**
review cards. The operator's side edited card A (subject `[aura-misura] 300-A modificato`, a new body) and
sent both.

| | A (edited) | B |
|---|---|---|
| Ledger end (`aura.tool_invocations`) | `ok`, 1 058 ms | `ok`, 853 ms |
| Tool result | begins "The operator edited your message before approving it. These fields differ from your call: subject = …" | the send result |
| Mailbox (read-only check) | arrived with the edited subject | arrived |

The model's next message said the first mail had been changed by the operator before it went, so subject
and body differ from its call. After a reload the row of A still showed the note.

## A server's form (Task 10, steps 1 to 5)

**Step 1, the "before".** At `2ae083107` the mounts logged calendar 29 tools, memory 13, whatsapp 15. This
is not a pre-elicitation baseline: Tasks 1-9 were already in that image. At `be40b4d5d` the three mounts
logged the same counts.

**Step 3, the mount.** `everything` mounted with 23 tools and negotiated `2026-07-28`, so this run took the
**multi-round-trip** path; the classic path was not exercised here.

**Step 4, the form.** The model called `everything__trigger-elicitation-request` and the card opened at
"Passo 1 di 14" on the name. Seen on screen:
- after typing the name and a date, a reload at step 2 brought the card back with both values (#299 point 1);
- on step 5 the default was prefilled and the button read "Usa il predefinito" instead of Skip;
- on step 10, "Scegline da 1 a 3." under the default; on step 12, Next went grey at four choices and back
  at three;
- the tool row read "1 min 2 s" and still running while the form was open (the held window, #297);
- the Review step (14 of 14) listed every field with what the server would receive, defaults included.

Submitted: the ledger row ended `ok` after 116 493 ms, in the same turn. The log has one
`mcp elicitation resolved` line, `action=accept`, `fields=13`; a grep for the typed values found 0 lines.

**Step 5, decline and expiry.** A second form, declined: the model reported the server's decline. A third
form, left alone: the countdown reached 0:00, the card read "Scaduto: annullato automaticamente.", and
the model read `{"action": "cancel"}`. The VM's timeout was not changed (300 s).

## Step 6: ask_user in each shape

On `cdf53e7a3`, one conversation, each answered on screen:

| Shape | The card | Answered | The model's next line |
|---|---|---|---|
| choice ([card](2026-11-27-lab-vm-drafts-elicitation-composer/ask-choice.png)) | three radio rows; **Rispondi** disabled until one was picked | `verde` | "Hai scelto: verde." |
| clarification ([card](2026-11-27-lab-vm-drafts-elicitation-composer/ask-clarification.png)) | a text field and **Rispondi** | `Trieste` | "La città che hai indicato è Trieste." |
| approval, no options ([card](2026-11-27-lab-vm-drafts-elicitation-composer/approval-free-text.png)) | the model sent `kind: approval` with no `options`: a text field and the "Controlla ambito e conseguenze" line, the free-text approval of spec 2026-07-15 | `No` | "Hai risposto No…" |
| approval, the model's options ([card](2026-11-27-lab-vm-drafts-elicitation-composer/approval-model-options.png)) | asked again naming `options`: Sì and No as rows, no scope line, **Rispondi** not Approva (#299 point 4) | `Sì` | "Hai scelto Sì." |
| gateway, destructive ([card](2026-11-27-lab-vm-drafts-elicitation-composer/approval-gateway-destructive.png)) | `skill_manage delete` of a skill that does not exist: the red variant, `rischio=destructive`, the scope line, three scope rows, **Approva** | declined | the row reads "Negata"; nothing was deleted |

## Step 7: one turn from Telegram

From the bot's chat in Telegram Web: "Chiama una volta trigger-elicitation-request del server everything e
dimmi cosa restituisce."
- The ledger row ended `ok` after 182 ms; the log has `mcp elicitation resolved` with `action=decline`,
  `fields=13`, `reason=answered` (the fallback's own decline).
- The notice reached the chat: it names `everything`, quotes the server's message, lists the 13 fields, and
  ends "Aura declined it automatically. Nothing was sent to the server."
- No card appeared in the cockpit, and no request was pending there.
- The model wrote that **the user** had refused, with `{"action": "decline"}`. The decline was Aura's, not the
  operator's: an ambiguous decline, as step 9 asks to record.
- An ordinary question after it ("Che ore sono adesso?") was answered as before.

## Three defects, and their fixes

**The composer left the bottom of the window after a card closed.** After a decline the composer ended at
918 px in a 1214 px window, 296 px above the edge. Cause, measured in the page: the tool rows' sr-only status
words are `position: absolute`, and with no positioned ancestor inside the thread's scroller their containing
block was `ArtifactWorkspace`'s `overflow-hidden` wrapper. They stretched it to a 1661 px scroll height, and
the browser scrolled it (`scrollTop` 176) when focus moved after the card closed. With `relative` set on the
thread's viewport by hand, every `overflow-hidden` ancestor had `scrollHeight === clientHeight`, and two more
declines left the composer at 1149 px with no scroll at all. Fix: `cf117b2dd`, with an assertion in
`web/e2e/shell-overscroll.spec.ts` that no clipped ancestor of the composer can scroll.

**Settled forms stacked under the answer.** Each receipt was drawn at the foot of the thread, under the
model's reply, not next to the call it belonged to. Fix: `cdf53e7a3`. The question carries the
`tool_call_id` of the call it arrived in, when exactly one call can own it (the multi-round-trip path, or a
classic session with one open call), and the cockpit draws the settled form inside that call's row. A form
with no single owner keeps its old place.

**Both, measured again on `cdf53e7a3`.** A form declined in a fresh conversation: the receipt "Rifiutato."
sits inside the call's row, in the same container as the tool button and outside the model's reply, which
follows it ([screenshot](2026-11-27-lab-vm-drafts-elicitation-composer/receipt-in-its-row.png)). The composer's
form ended at 1149 px of 1214 (the 65 px below are the usage bar), and no clipped ancestor of the composer could
scroll.

**Every answer to a question doubled the reply.** After each of the five answers in step 6 the model's reply
carried a branch picker, "2 / 2", both branches with the same text. The server's history held one chain, and a
reload showed no picker. Recorded in the page: the resume appended the reply under a client id
(`b91bf8c6-…`); when the run ended the cockpit read `/threads/{id}/messages` again, the reply became `msg-32`,
and the picker appeared. That read is the worker-report refresh, which runs after every run because the
coordinator frame reports a thread's latest run, swarm or not. It passes the snapshot through
`preserveMessageIDs`, which kept the live id of a user message and of the **first** assistant message after
it. A resumed turn holds two live (the paused call, then the resume), so the second took its database id and
the runtime kept the live one as a sibling. Fix: every assistant message after a user message is matched by
position (`web/src/chat/workers/useWorkerReportRefresh.ts`), with a test that failed before it. Measured
again on the VM at `f6322eb88`, which carries it: one choice answered in a fresh conversation, "Hai scelto:
montagna.", no branch picker, and the resumed reply kept its live id through the refresh after the run.

## What this does not establish

- **The branch fix beyond one answer**: it was measured on one choice question, not on an approval or a
  conversation with several answers.
- **A receipt after a reload.** The server keeps no record of a settled form, so a reload shows none; the tool
  row is the lasting record. Seen here too, as `2026-11-27-cockpit-card-fixes.md` predicted.
- **The classic path on the VM**, and a classic session with two open calls: its form names no call, which is
  a unit test (`TestAClassicFormWithTwoOpenCallsNamesNeither`).
- **#299 point 3** (the first-run dialog): this identity is past onboarding, so the dialog did not open.
- **The ask_user receipts.** After each answer the card left and only the tool row stayed; whether the run's
  continuation removed a receipt that should have stayed was not checked against the spec.
- **Steps 8 and 9** beyond the ambiguous decline above, and the score.
- **An expansion that adds nodes**: every neighbour of the node clicked was already in the graph.
- **The operator's own hands**: Claude drove the cockpit and Telegram in the operator's session. Timing and
  pointer behaviour are a script's, not a person's.
