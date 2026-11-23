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

## What this does not show

- No Microsoft 365 event, and no series on either provider: the window held one Google event.
- No IMAP message detail, and no contacts.
- The Microsoft scopes actually granted.
- G1–G9: only G7 measured, and on one IMAP host (Aruba).
- One model route. Whether another model fills `send_email` correctly was not tried.
