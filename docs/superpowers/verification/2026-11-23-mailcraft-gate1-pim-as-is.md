# MailCraft Gate 1: what the PIM returns today, and which G rows it can measure

Date: 2026-11-23. Lab VM `192.168.101.158`, spec
`docs/superpowers/specs/2026-11-21-mailcraft-cockpit-design.md` (Gate 1, "Also measured before
code"). Driven as the operator through the cockpit API (`/agent/run`, route
`openrouter deepseek/deepseek-v4.1-flash-1017`), read from the SSE tool results by shape only:
field names, types and counts, no values.

**Verdict: PARTIAL.** The mail half is measured on Gmail and Microsoft 365. The calendar half is
measured on one Google event, not on a series or a Microsoft 365 event. Of G1–G9, none can be
measured with today's PIM. G7 can be, but the agent turn that would have sent the mail was
refused before any send (see "Found on the way").

## The stack

- `aura-pim-mcp:sidecar`, digest `sha256:3a63…8df2`, created 2026-11-12 09:56 UTC. The image
  carries no revision label. Its creation follows the fork's `bb87b82` merge (09:55:48 UTC) by
  a minute, so it is read as `bb87b82`, the revision the spike and the spec read.
- Three accounts: one `google`, one `microsoft365`, one `imap`. Each has all six permissions.
  The IMAP account carries IMAP and SMTP only, with no CalDAV or CardDAV configuration.
- Google scopes granted: `gmail.compose`, `gmail.modify`, `gmail.send`, `gmail.readonly`,
  `calendar.events`, `calendar.readonly`, `contacts`. Microsoft 365 scopes requested by the fork
  (`OAuthScopes.cs`): `Mail.Read`, `Mail.Send`, `Mail.ReadWrite`, `Calendars.ReadWrite`,
  `Contacts.ReadWrite`. The granted Microsoft set lives in the MSAL cache and was not read.
- Aura mounts the PIM as one multiplexed tool, `calendar`, with 29 actions.

## Measured

| Call | Provider | Shape |
|---|---|---|
| `get_emails` | Gmail, M365 | `id, accountId, subject, from, receivedDateTime, isRead, hasAttachments`. Id length: Gmail 16, M365 140 |
| `get_email_details` | Gmail, M365 | `id, accountId, subject, from, fromName, to[], cc[], body, bodyFormat, receivedDateTime, isRead, hasAttachments, attachments[]`. `bodyFormat` reads `markdown` on M365 |
| `list_calendars` | Gmail 3, M365 4 | `id, accountId, name, owner, canEdit, isDefault` |
| `get_calendar_events`, 30 days | Google 1 event, M365 0 | `eventId, accountId, calendarId, subject, start_utc, start_local, end_utc, end_local, start_date, end_date, location, attendees[], isAllDay, organizer, responseStatus, attendeeDetails` (null in the listing) |
| `get_calendar_event_details` | Google | the listing's fields plus `timezone, body, bodyFormat, organizerName, attendeeDetails[{email, name, responseStatus, type, isOrganizer}], showAs, sensitivity, isCancelled, isOnlineMeeting, onlineMeetingUrl, onlineMeetingProvider, isRecurring, recurrencePattern, categories[], importance, createdDateTime, lastModifiedDateTime` |

What this confirms for D4:
- **M1, M2.** No message detail carries `Message-ID`, `In-Reply-To`, `References`, `Bcc`,
  `Reply-To`, a thread or conversation id, flags or the folder. FINDINGS 8(a) and 8(f) are
  confirmed live on both providers.
- **C2.** No event in a listing carries a series field (`seriesMasterId`, `recurringEventId`,
  `originalStart`), an etag or a change key. The details carry `isRecurring` and a formatted
  `recurrencePattern` string, not the rule.

## Read from the fork at `bb87b82`: which G rows today's PIM can measure

| Row | Measurable now | Why |
|---|---|---|
| G1, G2 | no | no raw-MIME send, no Bcc (`CalendarActionArguments` has `to`, `cc`, `subject`, `body`, no `bcc` or headers) |
| G3 | no | `GoogleProviderService.cs:1024` logs the response comment and drops it ("not sent to organizer") |
| G5, G6 | no | `create_event` takes no recurrence, and no action edits "this and following". G6 could run only on a series made by hand in Google Calendar |
| G7 | yes | `ImapProviderService.cs:518` always `APPEND`s the sent message to `SentFolder`. The measurement is whether the server files a second copy. Aura's reviewed sends so far all left from the Google account or the default one, so there is no past IMAP send to count |
| G8, G9 | no | no draft operation |

## Found on the way: an agent send fails before review, and the model is not told why

Both G7 turns ended with no mail sent and no draft to review. The model called `send_email` with
keys outside the draft's allowlist (`toDate`, `topics`, `unreadOnly`, as null or as a value) and
without `subject`. `messagedrafts` rejects such arguments (`validation.go`, `MergeApprovedArgs`
allowlist; `subject` required). `withholdOutboundMessage` then logs "could not persist outbound
message draft", and the gateway returns "message review required or claim mismatch" to the model,
which cannot correct an error it is not told. The refused calls' arguments are not recorded in
`aura.tool_invocations`.

### Rerun on `85ad97934` (same day, afternoon)

The VM ran `aura:edge` at revision `85ad97934`, which carries the refusal fix (`285d6cc8f`). Route
unchanged (deepseek-v4.1-flash). Two G7 turns, one at a time:

| Turn | Prompt | What happened | Draft |
|---|---|---|---|
| 1 | natural ("send from the IMAP account…") | `send_email` reached review on the first try | no `accountId`, sender empty; declined |
| 2 | every key spelled out, `accountId` included | first call carried `toDate` and no `subject`; the refusal named `"toDate"` and listed the accepted fields; the model retried and reached review | no `accountId`, sender empty; declined |

- **The refusal fix works live.** The model read the field list and corrected its call within the
  same turn.
- **A second gap: a send with no account reaches review with no sender.** The model's reasoning in
  turn 2 says "Redo with exact keys: action, accountId, …", and the stored draft has no
  `accountId`. Where the key is lost was not established. Without `accountId` the PIM chooses the sender itself (`SendEmailTool.cs`:
  recipient domain, else the first account, which on this VM is the IMAP one). The review card
  shows no sender, and `accountId` is not editable in review. Scheduled email already refused
  this case. `581c115b8` refuses it for the immediate send too, and the refusal tells the model to
  list the accounts.
- Nothing was sent. Both drafts were declined through `POST /api/message-drafts/{id}/resolve`.
- G7 is still not measured. It needs `581c115b8` on the VM.

`581c115b8` was reverted the same day (`2ea0c0d7b`). On the next image, on the route of the day
(`thinkingmachines/inkling-2-mini`), the cockpit turns never carried `accountId`, so every send was
refused before review and the operator could not send at all. The missing sender on the review
card stays open.

## G7, measured on the PIM directly (same day, 14:15 UTC)

Driver: Claude Code with the VM's `aura-pim-mcp` mounted through an SSH tunnel, the operator's
OAuth login, no Aura agent in between. One `send_email` from the IMAP account `tecnico` (Aruba)
to the operator's Google address, subject `[aura-misura] G7`, `accountId` passed explicitly.

| Check | Result |
|---|---|
| send | ok |
| copies at Google | 1 |
| copies in the IMAP `INBOX.Sent`, after 45 s and again after 60 s | 0 |
| IMAP search over the account | 0 |
| PIM log | `Failed to APPEND sent message to [Gmail]/Sent Mail … FolderNotFoundException`, on this send and on the parallel session's IMAP send at 13:36 |

- **Aruba does not file sent mail on its own.** The SMTP submission left no copy anywhere in the
  mailbox.
- **The PIM's own Sent copy fails on Aruba.** The account sets no `sentFolder`, so the PIM falls back
  to `DefaultSent = "[Gmail]/Sent Mail"` (`ImapProviderService.cs:29`). The APPEND
  (`ImapProviderService.cs:485-499`) only logs the failure. Aruba's Sent folder is `INBOX.Sent`.
- G7's answer for this host: the PIM must write the Sent copy, into the folder the server marks
  `\Sent` (RFC 6154), not into a Gmail default.
- Cleaned up: the Google copy was deleted by exact id (moved to Trash). A search afterwards found
  0 copies. Nothing landed in the IMAP account.

## After the fixes (same day, 15:43 UTC)

Fixed and deployed through the updater:

| Commit | Repository | What it fixes |
|---|---|---|
| `ec7d740` | PIM fork | the Sent folder is found on the server: setting, else `\Sent`, else a known name |
| `43c49b3` | PIM fork | the same lookup for Trash and Junk |
| `63365df` | PIM fork | the curated `send_email` requires `accountId` |
| `d697c5ccd` | Aura | after a redial, Aura re-reads the server's tools |

Results:

| Check | Driver | Result |
|---|---|---|
| IMAP send, then `INBOX.Sent` on Aruba | PIM mounted in Claude Code | 1 copy (was 0) |
| `delete_email` on Aruba | same | moved to `INBOX.Trash` (was `Folder '[Gmail]/Trash' was not found`) |
| `send_email` with no `accountId` | same | refused, naming `accountId` and `list_accounts`; nothing sent |
| a cockpit turn with the operator's own wording, on the route of the day (`inkling-2-mini`) | cockpit API | the draft reached review with `accountId` (the IMAP account) and its sender shown; approved, it was sent; 1 copy in `INBOX.Sent`, 1 at Google |

- **Why the model left out `accountId`.** The curated tool told it to: "send_email (omit to match
  the first recipient's domain…)". The first cockpit turn after the PIM update still read that text,
  because Aura's redial no longer re-read the tools and a restarted server pushes no
  `tools/list_changed`. With `d697c5ccd` the turn read "Requires accountId, to, subject" and passed
  the account.
- **The parallel session's sends of 13:34–13:36 had `accountId`; the earlier claim that this
  afternoon's model dropped it on its own** is answered by the tool text, not by the model or the
  driver.
- **The VM updater holds sidecars behind a pending Aura update.** While `2ea0c0d7b` waited for an
  admin, the new PIM images were not pulled; each Aura update needed the operator's "Aggiorna subito".
- Cleaned up: every `[aura-misura]` mail was deleted by exact id. Aruba's two earlier copies are in
  `INBOX.Trash`, Google's in its Trash. Searches afterwards found 0 copies in Sent and Inbox.

## Scheduled-task email notifications (same day, 16:35 UTC)

`63365df` made the PIM refuse a send without `accountId`, and the scheduler's email notification
(`internal/cron/notify_email.go`) never named one. So every email notification on an appliance
following `:sidecar` failed from then on. `c0fe0528c` makes the host list the PIM accounts first. It
sends from the account whose address is the recipient's, when that account may send, else from the
first account that may send.

Driven without a model: `aura task schedule --kind reminder --at <now+2 min> --notify email`, run inside
the `aura` container on `c0fe0528c` (applied 16:32 UTC by the operator's "Aggiorna subito"). The body
carried `[aura-misura]`; the subject is fixed by the notifier. `AURA_SCHEDULER_NOTIFY_RECIPIENT` is unset
on the VM, so the recipient is the operator's sign-in address, which is the Google account's.

| Check | Result |
|---|---|
| PIM log | one send, with `accountId` set to the Google account |
| copies at Google (PIM mounted in Claude Code) | 1, sender = the Google account |
| copies in the IMAP and Microsoft 365 accounts | 0 and 0 |
| after deleting it by exact id | 0 left |

- The PIM log also shows `Sending address of account …microsoft not found: ODataError` while listing
  accounts. The Microsoft 365 account's address was not read, so the rule could not have matched it. That
  is harmless here and not investigated.
- Not shown: a recipient set through `AURA_SCHEDULER_NOTIFY_RECIPIENT` that no account owns (the
  fall-back to the first sender is unit-tested only); an agent-job notification; WhatsApp and Telegram
  routes, which this change does not touch.

## What this does not show

- No Microsoft 365 event, and no series on either provider: the window held one Google event.
- No IMAP message detail, and no contacts.
- The Microsoft scopes actually granted.
- G1–G9: only G7 measured, and on one IMAP host (Aruba). The Sent and Trash lookup was tested on
  Dovecot and GreenMail in the fork's suite, not on another live host.
- One model route, one cockpit turn after the fix. Whether the model passes `accountId` every time
  is not measured.
