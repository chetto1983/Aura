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

## What this does not show

- No Microsoft 365 event, and no series on either provider: the window held one Google event.
- No IMAP message detail, and no contacts.
- The Microsoft scopes actually granted.
- G1–G9: none measured.
- One model route. Whether another model fills `send_email` correctly was not tried.
