# Message drafts: the cockpit scenario and a controlled inbox (2026-11-27)

Plan: `docs/superpowers/plans/2026-09-28-tool-rendering-message-drafts.md`. The revised Task 4 left two things
open: the cockpit scenario of Task 4 Step 1, which needs a model that calls `send_email`, and the real-stack
smoke against a controlled inbox. Both ran here for email. The first attempt found a defect, fixed in
`fix(agui): read every pause of a round before ending the run`; the scenario was then run again on the fix.

## Tree and stack

| | |
|---|---|
| Tree | master `08886a21f` for the first attempt; branch `ccr-d742c0b3-jvadt4-draft-batch` (the translator fix) for the second. The cockpit is the tracked `internal/webui/dist`, which predates #299 |
| Stack | a cloud container's `aura-postgres`, `aura-arcadedb`, `aura-llama-embed`; a fresh disposable `aura_e2e` (134 migrations); profile `single_user_hardened`; no memory sidecar |
| PIM | `pim_up.sh`: GreenMail (users `operator@aura.test`, `colleague@aura.test`) and `ghcr.io/chetto1983/aura-pim-mcp:68f8935…` on the host network; installed with `aura mcp install calendar`, mounted with 29 tools |
| Model | the hand endpoint of `aura-model-in-the-loop`: every model answer was written by hand |
| Operator | Chromium through Playwright on the cockpit, plus `cockpit.py` for the API checks |
| Standing grant | `aura.gateway_approval_grants` row (`calendar__send_email`, action `''`) inserted before the run: an "always" approval for the tool |

## The defect: a second send in the same round was lost

The model called `calendar__send_email` twice in one message (`m1`, `m2`), as a model asked for two mails does.

| | Before the fix (master) | After the fix |
|---|---|---|
| Drafts | `m1` and `m2`, both `pending` | `n1` and `n2`, both `pending` |
| Pause rows | 1, for `m1` | 2, one each |
| Cards in the cockpit | 1 ([screenshot](2026-11-27-message-drafts-cockpit/before-fix-one-card.png)) | 2 ([screenshot](2026-11-27-message-drafts-cockpit/after-fix-two-cards.png)) |
| History the model resumed on | the assistant message held `m1` only; `m2` was gone, with no result | `n1` with its send result, `n2` with "user declined to send the message" |
| The second draft | `pending`, with no pause: the cockpit does not list it, and it would expire unseen | reviewed and declined |

Cause: the AG-UI translator ended the run at the first pause event and stopped reading, so the runner never
received the second (`internal/agui/translator.go`). The agent raises a round's pauses back to back. The
translator now reads them all and ends the run with one `RUN_FINISHED` listing every interrupt. Telegram reads
the same translator. The unit test `TestTranslatorEndsARoundOnEveryPauseItRaised` fails on master (2 events read)
and passes on the fix.

## The scenario, after the fix

| Step (Task 4 Step 1) | Result |
|---|---|
| Two messages under an "always" grant | two drafts, two pauses, two cards: the grant bypassed nothing |
| Edit both fields and send one | `n1`: To `colleage@` changed to `colleague@aura.test`, subject and body changed; sent with those values (`effective_args`) |
| Double click on Send | one dispatch: one `start` and one `end ok` for `n1` in `aura.tool_invocations` |
| Decline the other | `n2` `declined`; no tool invocation for it |
| The run resumes only when both are answered | the model's next request came after the decline, with both results |
| Stale tab | `POST /api/message-drafts/{n1}/resolve {"action":"send"}` after the send: `404 message review not found or already resolved`; still one `start` |
| Expiry | `n3` aged past `expires_at`: the card read "This review expired. Ask Aura to prepare a new message."; a direct send got `404`; `n3` `expired`, no tool invocation; after "Continue conversation" the model read "message review expired without sending" |
| No session | `POST …/resolve` and `GET /api/message-drafts` without a session: `401` |
| Reload | no card left; the two tool rows read `Done`, with the send result for `n1` and "user declined to send the message" for `n2` ([screenshot](2026-11-27-message-drafts-cockpit/receipts-after-reload.png)) |

## The controlled inbox

Read over IMAP, read-only, after the run:

| Mailbox | Messages |
|---|---|
| `colleague@aura.test` | 2: `m1` from the first attempt and `n1` from the second, each with the operator's edited subject and body, not the model's originals |
| `anna@client.test`, `colleage@aura.test` | none: GreenMail lists only `operator@` and `colleague@` as users, so nothing was ever delivered to either |

## The operator's edit, invisible afterwards, then fixed

In the run above, two places still showed the model's original call instead of what was sent. The tool result
for `n1` was `{"success": true, "messageId": …}`, so the model believed it had written to `colleage@` about a
meeting "alle 10". After a reload, the row's REQUEST block showed the original arguments under a `Done` result,
and the edited recipient appeared nowhere on screen.

On the operator's go-ahead, the answer to a reviewed send now opens with the fields the review changed and the
values that went out (`messagedrafts.Draft.OperatorEdits`). It does so for a sent or uncertain send, and for an
approved scheduled one, but not for a failed one. Measured with one `send_email` (`e1`), recipient and body
edited on the card:

| | Result |
|---|---|
| What the model read | `The operator edited your message before approving it. These fields differ from your call: to = ["colleague@aura.test"]; body = "La riunione di lunedì è confermata, ma alle 11.".` followed by the send's own result |
| The row after a reload | REQUEST still shows the model's call; RESULT opens with the same line ([screenshot](2026-11-27-message-drafts-cockpit/edited-receipt-after-fix.png)) |
| An unedited send | the answer is the send's result alone, as before (`TestAReviewDraftedBeforeThePIMSplitSendsAfterIt`) |

The REQUEST block keeps the model's own call: it is what the model asked for, and the RESULT now says what went
out instead.

## What this does not show

- **A real model.** Whether a model reads the edit line and reports the edited message is its own choice; here
  it was mine.
- **WhatsApp.** No bridge ran; the spec asks for both families.
- **A foreign identity, and a public share.** A second identity needs a real Telegram token
  (`aura identity create`), and public sharing is off on this profile. Both remain covered by the API tests only.
- **An ambiguous delivery** (`uncertain`): proven by the kill/restart test of 2026-11-26, not here.
- **The lab VM.**
