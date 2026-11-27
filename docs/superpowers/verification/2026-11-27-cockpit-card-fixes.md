# Cockpit cards: four fixes, measured in a browser (2026-11-27)

The run of `2026-11-27-elicitation-window-and-cards.md` found four things on screen, and the operator asked for
them fixed. Each one was traced to its cause, fixed, and run again in the same setup.

## Tree and stack

| | |
|---|---|
| Tree | branch `ccr-d742c0b3-jvadt4-elicit-cards`, master `03e78459c` plus the fixes, uncommitted at run time; the binary reports `v1.2.1-0.20261127003933-03e78459c319+dirty` |
| Cockpit | `web/` built locally (`npm run build`) into `internal/webui/dist` for this binary only. The tracked bundle was restored afterwards: the operator rebuilds it |
| Stack | a cloud container's `aura-postgres`, `aura-arcadedb`, `aura-llama-embed`; a fresh disposable `aura_e2e` (134 migrations); profile `single_user_hardened`; no memory sidecar |
| Model | the hand endpoint of `aura-model-in-the-loop`: every model answer was written by hand |
| Operator | Chromium through Playwright, 1280×1000, signed in through the login form, session storage cleared before the first load |
| Server | `@modelcontextprotocol/server-everything` through `POST /api/governance/mcp`, mounted with 14 tools |

This binary does not carry PR #297, so the 60 s window still moved a form's call to the background. Every form
here was answered within it: the one form submitted closed `ok` after 21 911 ms.

## The four findings

| # | Finding | Cause | Fix | Measured after |
|---|---|---|---|---|
| 1 | A reload mid-form brought the form back empty, at step 1 | step and values lived only in the card's React state | `useElicitationDraft`: the step and values are kept in the tab's session storage under the question's id, checked against each field's kind on the way back, and removed once the form is settled | reloaded at step 3 after typing two fields: the card came back at step 3 ("Boolean"), and Back showed `Ada Lovelace` and `1815-12-10`; after the submit, no draft key was left |
| 2 | A form's card and receipt left the thread when its run ended | `keepLiveRun` dropped every card of an ended run, settled ones included | `settleForms`: an ended run is retired and loses its unanswered forms; its receipts stay until the next run streams, as an ask_user receipt does | after "Run complete", the card still read "Answered." with what the server received ([receipt](2026-11-27-cockpit-card-fixes/receipt-after-run.png)); 1.5 s after the next message, no card was left |
| 3 | The first-run setup reopened on every reload, over the card | the gate remembered "opened once per session" in a `useRef`, which a reload resets | closing it is kept in session storage (the update dialog's pattern), except against a `?onboarding=1` link or a required route step | after Close and a reload, no dialog, `aura.onboarding.dismissed=1`; the later reload mid-form opened none either |
| 4 | An approval offering the model's own options asked to "Review the scope and consequence" | the line was drawn for every `kind=approval` | drawn for the gateway's scope rows and for a free-text approval (spec 2026-07-15), not for the model's own options | Sì/No approval: no line, **Answer** ([model options](2026-11-27-cockpit-card-fixes/approval-model-options.png)); `skill_manage delete` withheld by the gateway: the line, three scope rows and **Approve** ([gateway](2026-11-27-cockpit-card-fixes/approval-gateway.png)) |

The receipt in the screenshot has no birth date: the script that walked the form to Review took Skip on the
optional date step. The value was there before it: the check after the reload read it back.

## Gates

| Gate | Result |
|---|---|
| `npm run lint` (Oxlint, prettier, tsc) and `npm run typecheck` | clean |
| `npm test` | 427 files, 4289 tests passed |
| Stryker on the touched files | `useThreadElicitations.ts` 95.30%, `ElicitationCard.tsx` 85.96%, `sessionValue.ts` 90.91%, `InlineApprovalCard.tsx` 75.69%, `useElicitationDraft.ts` 95.83% (69/72, the three survivors equivalent: a `JSON.parse(undefined)` that throws anyway, an empty `catch`, and a guard the next one covers) |

`useElicitationDraft.ts` and `lib/sessionValue.ts` are new scopes of `stryker.config.json`, with their tests.

## What this does not show

- **A real model.**
- **The draft after the tab closes.** Session storage goes with the tab, by design; a new tab starts the form
  again, as before.
- **Receipts after a reload.** The server keeps no record of a form, so a reload shows none, as before; the
  tool row in the thread is the lasting record.
- **The lab VM**, and the bundle the operator builds.
