# Signing an account in again keeps its watch: 2026-11-03

Scope: lab VM .158, updated from the cockpit to Aura `7af458618` (fix `d5c12fc95`). Every workflow on that SHA was green.
The person pressed «Ricollega» on the Gmail account. The figures come from the scheduler, the verdict, correction and
cursor tables, and the identity's ArcadeDB database. They are counts and timestamps only.

## The defect it closes

A connected account had one button, «Disconnetti», and it removed the account. Signing in again therefore meant
removing the account and adding it back. Removal deleted the account's verdicts, corrections and the centroid's examples.
Measured twice before the fix:
- Microsoft, 2026-11-02: watch task cancelled at 16:00:06 UTC, a new one at 16:02:13;
- Gmail, 2026-11-03: cancelled at 11:30:39 UTC, a new one at 11:31:28. The day's verdicts went, and so did a tier
  correction and its `ConfirmedExample`.

## What was measured after the fix

| Check | Result |
|---|---|
| The Gmail watch task | Still the one created at 11:31:28 UTC, `active`, last run 13:30:54 |
| Its verdicts and corrections | The verdict of 12:14 and its correction are there |
| The other account (Microsoft) | 7 verdicts since 2026-11-02 16:07 and 6 corrections, untouched |
| Removal | One account with no verdicts (added 2026-11-02 15:16, no calendar) was removed at 13:31:16 UTC through «Rimuovi definitivamente»: its task was cancelled and its cursors deleted, as removal should |

## What this does not establish

- **That the new Google token was stored.** The PIM keeps the token, and its refresh was not read. Reads after the
  re-link would show it.
- **That an expired grant reads as expired.** The PIM's `linked` reads whether a token is stored, so a dead Google grant
  still shows as connected. «Ricollega» is offered either way, but the cockpit cannot say the grant died.
- **Microsoft's device flow on a connected account, live.** It is tested with fakes only.
