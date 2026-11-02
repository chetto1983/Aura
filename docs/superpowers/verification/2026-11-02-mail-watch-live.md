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

## The second run, after the update

The VM at Aura `0ac494f82` and the PIM image built at 11:02:35, inside the publish job of `931a385`. A meeting about two hours away, sent to the Gmail account from someone it writes to. Each row is one message on Telegram, as the person pasted it, and the verdict rows read back from the database.

| Step | Telegram | Mail | Event | Alerts told |
|---|---|---|---|---|
| Invited | `Nuovo:`, title, 16:30 | `now`, by `people` | `today`, `new`, no alert | mail 3 to 4, calendar stays 2 |
| Moved | `Spostato:`, title, 19:45 | `now`, by `people` | `today`, `moved`, no alert | mail 4 to 5, calendar stays 2 |
| Cancelled | `Annullato:`, title, 19:45 | `now`, by `people` | `today`, `cancelled`, no alert | mail 5 to 6, calendar stays 2 |

So one meeting's change is one alert, with the event's line in the mail's place. That also shows Gmail's part headers carry the invitation's Content-Type, which the docs did not say, and a cancelled meeting's line has its title back. Each alert read `notified` on the tick after it and was not repeated.

The same on Outlook.com, the first meeting measured on that account. The account has never written to this sender, so the mail stays at `today` by `default` and the event alerts by itself. That is why it is one alert, so this does not exercise `invitation` on Graph.

| Step | Telegram | Mail | Event | Alerts told |
|---|---|---|---|---|
| Invited | `Nuovo:`, title, 17:30 | `today`, `default` | `now`, `new` | calendar 0 to 1 |
| Moved | `Spostato:`, title, 19:00 | `today`, `default` | `now`, `moved` | calendar 1 to 2 |
| Cancelled | `Annullato:`, 19:00 | `today`, `default` | `now`, `cancelled` | calendar 2 to 3 |

Outlook.com does not delete a cancelled event: it keeps it, and renames it after the cancellation mail. So the line had a title, and read `Annullato: Evento annullato: <title> - <date and hours> (<address>)`. It is long and says it twice, and it is the provider's text.

With a rule from a real turn (`add_rule`, tier `now`) on that sender, a new invitation to Outlook.com put the mail at `now` by `rule` and the event at `today`, `new`, with no alert of its own, mail 0 to 1 and calendar still 3. So `invitation` holds on Graph under `$select` too, and the subject held the event's title.

A crash in the worst place: a script waited for a committed alert not yet confirmed and killed Aura's process at 14:25:22.382, the same second, with the calendar of that tick not yet read. Aura was back at 14:25:22.9. Nothing had reached Telegram. The tick at 14:30:37 took the alert over under its own run and told it once, with a mail that had come since, and counted only the new one, 7 to 8. It read `notified` on the tick after, and Telegram shows one message.

Both Outlook.com cursors read `failing`, `provider_transient`, from 11:44 until 13:09, and `ok` on the first tick after it. The person says the VM had no internet in that time.

On a real turn the model said it could correct a verdict but not find one, and the code agreed: an id reached it only through an alert's record. `mail_watch list_decisions` lists an account's day through the read the cockpit already had (`cae2112bc`). The next turn, on Aura `712ec0f0c`, listed both accounts, called `correct` on a mail no alert had named, and read the label back. That is the first change `mail_watch` made on a real turn, and the first live read of items the provider no longer has: two Outlook.com mails read gone, the invitation and its update, both replaced by Outlook when the next one came.

## What it found, and what changed

| Defect | Measured | Fix |
|---|---|---|
| One meeting told twice | Every change alerted as a mail and as an event, in one tick. One meeting took 4 of the 5 daily alerts, and the cancellation mail was then capped | The PIM states `invitation`, and an event at `now` whose title an invitation alert of that tick holds takes the mail's place |
| A cancelled meeting's row had no title | `ReadItemAsync` returned nothing for a cancelled Google event | The read returns it with its title, or its series' title for one cancelled instance |
| Microsoft personal sign-in failed | Under `common` the page is `login.microsoft.com/device`, and a personal account ended on `http://localhost` with `invalid_request` | Outlook.com asks for no tenant and Aura always sends `consumers` |
| A misspelt tenant saved | `consumer` was accepted, and failed later with `AADSTS900023` | Microsoft 365's tenant is checked on save |
| A failed sign-in read as connected | The card said connected with no token behind it | The PIM states `linked`, and the cockpit offers to finish or remove |
| The same account kept its old tenant | Re-adding under one name left the file on `consumers` and MSAL on `common` | The PIM rebuilds the app when tenant or client change |

The fixes for the first two and the tests behind them: fork `0663442`, `e2146f2` and `931a385`, and Aura's `fix(pimwatch)` commits of this date. Fork suite at `931a385` beside a real GreenMail: 1,732 passed, 0 failed, 45 skipped, those being Dovecot's. The Postgres tiers of `pimwatch`, `pimprovider` and `agui` passed on a throwaway database.

An independent review of these changes found two faults before any image was published, and both are the kind a unit suite cannot see. Saving Outlook.com with no tenant failed on the real table's check, with a fake store keeping the tests green; it reached `master` as `73500744c` and was fixed by `701986c37`, with a test that goes through the real table. The first rule for invitations silenced the mail while the calendar was healthy, which left a meeting older than the watch with no alert at all, so that rule was replaced the same day: the ladder is untouched, and the worst case is the old double.

## What this does not prove

- A crash after Telegram took the message and before the run closed. The alert would be told twice: the run above died before the send. The window is what a send takes.
- A meeting older than the watch, or one whose invitation is not in the mailbox. Each should alert once, on its own, and neither was tried live.
- The fix for a mail moved on Outlook.com. Two mails the person moved to the bin read gone while still in the mailbox, because Graph's id changes on a move unless the request asks for `Prefer: IdType="ImmutableId"` (learn.microsoft.com/graph/outlook-immutable-id). The fork's `c450f04` asks for it, and has not run on a live account.
- IMAP on a real mailbox. It ran against GreenMail only.
- The digest at 18:00 local, which had not fired when this was written.
- A revoked grant, `enable` on a real turn, and `pim_contract_live`.
- G3: zero missed and zero duplicates over five working days per adapter.
- Delivery beyond what the person saw: Telegram was read in their browser and on their word.
