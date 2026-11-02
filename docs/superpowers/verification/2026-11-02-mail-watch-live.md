# Mail watch, first live run: 2026-11-02

Scope: the watch on lab VM .158, one person, one Gmail account and one Outlook.com account, read through the published PIM at `204a7fd` and then `93dfa28`. The person acted, and the verdict rows, the run ledger and Telegram were read back. No sender or subject is recorded here, only counts and the fixed words of the alerts. Times are UTC.

## What was measured

| Step | Result |
|---|---|
| A mail from someone the account writes to | Told on Telegram within a tick, in Italian, with sender and subject, decided by `people` |
| A new meeting | `Nuovo:` with the right local time |
| The meeting moved past 24 hours | `Spostato:` with the new time, `now` because the kept start was inside the window |
| The same meeting cancelled, about 28 hours away | `today`, and no alert, as the ladder says |
| The next tick after an alert | Confirmed from the ledger, and not repeated |
| The daily cap | Reached 5 of 5, and the sixth went to the digest with `1 capped` |
| Corrections from the cockpit | Written, the verdict reads `person`, and "L'hai deciso tu" shows on the row |
| "D'ora in poi sempre così" | Wrote a sender rule at 09:32, source `person` |
| The decisions view | First run on a real stack: tiers, reasons and local times right |
| A real turn after the alerts | The model listed the alerts and saw that the invitation and the event were one meeting. Its only `mail_watch` call was `status` |
| "aprila" | Opened the right mail through `calendar__calendar` |
| Outlook.com sign-in | Device flow under `consumers` gave `www.microsoft.com/link`, success at 08:55:23, MSAL cache 7,370 bytes |
| The watch on Outlook.com | Both cursors `ok`, and 2 real mails decided at 09:06 and 09:11, the first verdicts from Graph on a live account |

## What it found, and what changed

| Defect | Measured | Fix |
|---|---|---|
| One meeting told twice | Every change alerted as a mail and as an event. One meeting took 4 of the 5 daily alerts, and the cancellation mail was then capped | The PIM states `invitation`, and while the account's calendar is read healthy such a mail stays `today` |
| A cancelled meeting's row had no title | `ReadItemAsync` returned nothing for a cancelled Google event | The read returns it when it still has a title |
| Microsoft personal sign-in failed | Under `common` the page is `login.microsoft.com/device`, and a personal account ended on `http://localhost` with `invalid_request` | Outlook.com asks for no tenant and Aura always sends `consumers` |
| A misspelt tenant saved | `consumer` was accepted, and failed later with `AADSTS900023` | Microsoft 365's tenant is checked on save |
| A failed sign-in read as connected | The card said connected with no token behind it | The PIM states `linked`, and the cockpit offers to finish or remove |
| The same account kept its old tenant | Re-adding under one name left the file on `consumers` and MSAL on `common` | The PIM rebuilds the app when tenant or client change |

The fixes for the first two and the tests behind them: fork `0663442` and `e2146f2`, Aura `fix(pimwatch): tell an invitation once`. Fork suite at `e2146f2` beside a real GreenMail: 1,731 passed, 0 failed, 45 skipped, those being Dovecot's. The watch's Postgres tier passed on a throwaway database.

## What this does not prove

- The last two fixes on a live account. They are in the fork, and the image is not published yet at the time of writing. `invitation` on real mail rests on the docs: Gmail's part headers are not documented as holding Content-Type, and Graph's `@odata.type` is shown only without `$select`. If either is wrong the mail keeps its alert, so the failure is the old double.
- A meeting on Outlook.com. No calendar verdict exists on that account yet.
- An account set to add invitations only when answered. Such a meeting gets a digest line and no alert, and that case was not run.
- IMAP on a real mailbox. It ran against GreenMail only.
- The digest at 18:00 local, which had not fired when this was written.
- Kill mid-tick, a revoked grant, `mail_watch` changing anything on a real turn, and `pim_contract_live`.
- G3: zero missed and zero duplicates over five working days per adapter.
- Delivery beyond what the person saw: Telegram was read in their browser and on their word.
