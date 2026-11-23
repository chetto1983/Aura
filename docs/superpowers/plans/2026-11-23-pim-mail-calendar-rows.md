# The PIM's mail and calendar rows M1, M2, M9, C1, C2, C3: implementation plan

**Status:** v1, 2026-11-23. Not started, not reviewed. Written while the parallel session pushes
master; nothing here runs before that push is green.

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or
> superpowers:executing-plans, task by task.
> - P1-P8 run in the PIM fork (`D:/tmp/aura-pim-mcp`), on a branch `aura/mail-complete` off `main`.
> - A1 runs in Aura, on `master`.
> - L1 and L2 are the live runs on the lab VM.
>
> Steps use checkbox syntax.

**Goal:** The PIM sends a message the way a mail client does: Bcc delivered and hidden, a reply in
its thread, one Sent copy. It leaves a draft in the provider's Drafts for the owner to send. It
writes a calendar series with its reminder, free/busy, privacy and categories; it changes one
occurrence, the series or "this and following"; and it answers an invitation with a comment the
organizer receives. Each Gate 1 behaviour (G1-G3, G5-G9) is measured on real accounts as soon as
the code it tests is deployed.

**Architecture:**
- **One outbound path.** `OutboundMime` builds the one `MimeMessage` every provider sends or files
  as a draft. `SendEmailAsync` becomes `SendMimeAsync` on every provider: Gmail `messages.send raw`
  with `threadId`, Graph `POST /me/sendMail` with the MIME base64 as `text/plain`, IMAP SMTP plus
  `APPEND` to Sent. Microsoft 365 and Outlook.com move from the JSON body to MIME.
- **Replies are resolved by the PIM.** `send_email`, `create_draft` and `update_draft` take
  `replyToEmailId`. The PIM reads the original through `GetRawEmailAsync` (M1) and sets
  `In-Reply-To`, `References` and the provider thread id. The model never writes a header.
- **Drafts.** `create_draft` / `update_draft` take the same fields as `send_email`. Gmail
  `users.drafts.create`/`update`, Graph `POST /me/messages` (MIME) then `PATCH` (JSON), IMAP
  `APPEND` with `\Draft` into the special-use `\Drafts`, replaced on update.
- **Calendar.** `create_event`/`update_event` gain `recurrence` (one RRULE, RFC 5545),
  `reminderMinutes`, `showAs`, `isPrivate` and `categories`. `update_event`/`delete_event` gain
  `scope` (`occurrence`, `series`, `following`). Listings carry `seriesId` and
  `originalStartUtc`. Graph has no RRULE input, so `RRuleToGraph` converts a documented subset and
  refuses the rest before any request.
- **Aura** classifies the two draft actions, lets the reviewed send carry `bcc` and
  `replyToEmailId`, and shows Bcc in both review surfaces (cockpit and Telegram).

**Tech Stack:**
- PIM: .NET 10, MailKit/MimeKit 4.18.2, Microsoft.Graph 6.5.0 (Kiota `RequestInformation` for the
  MIME send), Google.Apis.Gmail.v1 / Calendar.v3, Ical.Net 4.3.1 (already a dependency: RRULE
  parsing), MSTest, `FakeHttp` + `GmailFixtures`/`GraphFixtures`, GreenMail 2.1.15 and Dovecot
  2.4.5.
- Aura: Go (`internal/agent/mcptools`, `internal/messagedrafts`, `internal/channels/telegram`);
  React (`web/src/approvals`), vitest.

**Spec:** `docs/superpowers/specs/2026-11-21-mailcraft-cockpit-design.md`, D4 rows M1, M2, M9, C1,
C2, C3 and Gate 1 rows G1-G3, G5-G9. Measured baseline:
`docs/superpowers/verification/2026-11-23-mailcraft-gate1-pim-as-is.md`.

## Global Constraints

- **Provider calls are the documented ones named in spec D4.** The code comment above each call cites
  its page or RFC section. A behaviour the page does not state is a Gate 1 row, not a guess.
- **Ids.** IMAP email ids keep `ImapProviderService.FormatEmailId(folder, uidValidity, uid)`. A
  Gmail draft is addressed by its draft id (immutable), never by its message id, which changes on
  every update (`guides/drafts`). Graph requests carry `Prefer: IdType="ImmutableId"` (M7) on
  every new call this plan adds.
- **Bcc.** Delivered copies never carry a `Bcc:` header. The Sent copy and a draft keep it, as
  mail clients do, so the owner sees whom they blind-copied. (This reads spec Acceptance "Bcc
  delivered and absent from every header" as every *delivered* copy; see Decisions.)
- **Refuse before writing.** A recurrence Graph cannot represent, a `scope` the id cannot take, a
  `replyToEmailId` from another account, or attachments on `update_draft` fail with a
  `ToolGuard` error before any provider request.
- **Errors reach the model.** Every new refusal names the argument to change and never echoes a
  value, as Aura's `messagedrafts` does since `285d6cc8f`.
- **Files ≤600 LOC; new code in new partial-class files.** `GoogleProviderService.cs` (1746),
  `M365ProviderService.cs` (1529), `OutlookComProviderService.cs` (1498) and
  `ImapProviderService.cs` (598) are not split by this plan (precedent: the 2026-11-20 plan). Each
  task moves the code it rewrites out of them into its own partial file, so they only shrink.
- **Public repos.** Fixtures use `example.com`/`example.org` and invented names. No real address,
  subject or mail text, in tests, commits or verification files (verification files hold field
  names, counts and yes/no).
- **Commands.**
  - **PIM:** `dotnet build calendar-mcp.slnx -c Release`, then
    `dotnet test calendar-mcp.slnx -c Release --no-build --filter <Class>`. IMAP/SMTP tests need
    `PIM_TEST_GREENMAIL=localhost:3143`, `PIM_TEST_GREENMAIL_SMTP=localhost:3025` and
    `PIM_TEST_DOVECOT=localhost:31143`, started with the `docker run` lines in
    `.github/workflows/ci.yml`.
  - **Go, in WSL:** `wsl -e bash -lc 'cd /mnt/d/Aura && export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH && go vet ./... && go build ./... && go test -race ./internal/<pkg>/...'`.
  - **Web, on Windows:** `cd web; npx vitest run <file>; npm run lint; npm run typecheck; npm run test`.
- **`AURA-FORK.md`** gets a line for each new action, argument and behaviour change, in the commit
  that makes it.
- **Commits.** One atomic commit per task. Aura commits end
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **A Bcc recipient shows in a delivered copy.** On SMTP the header must leave the wire bytes; on
   Graph and Gmail the provider strips it from MIME (G2, and Graph in L1). Pinned by
   `Smtp_Bcc_IsDeliveredWithoutTheHeader` (P1, GreenMail) and the L1 rows.
2. **A draft update drops an attachment.** Gmail and IMAP replace the whole message. Pinned by
   `UpdateDraft_KeepsTheAttachmentsOfTheStoredDraft` (P4, Gmail fake and GreenMail).
3. **A recurrence half-written.** Graph creates the event, then the series fails. Pinned by
   `Graph_UnrepresentableRrule_SendsNoRequest` (P5).
4. **"This and following" from the first occurrence.** Truncating before the first occurrence
   would leave an empty series. Pinned by `Following_FromTheFirstOccurrence_UpdatesTheSeries` (P7).
5. **A reply that crosses accounts.** `replyToEmailId` from account A while sending from B would
   thread into a mailbox the message never came from. Pinned by
   `ReplyToEmailId_FromAnotherAccount_IsRefused` (P3).

---

## File structure

| Where | File | Responsibility |
|---|---|---|
| PIM Core | `Models/OutboundEmail.cs` (new) | `OutboundEmail` record: the fields of a send or a draft |
| PIM Core | `Providers/OutboundMime.cs` (new) | `Build(OutboundEmail, MailboxAddress from, ReplyThread?) -> MimeMessage`; the only MIME builder (replaces the two in Google and IMAP send) |
| PIM Core | `Providers/ReplyThread.cs` (new) | `ReplyThread.From(RawEmail)`: Message-ID, References, Subject, provider thread id |
| PIM Core | `Providers/{Google,M365,OutlookCom,Imap}ProviderService.Mail.cs` (new) | `GetRawEmailAsync`, `SendMimeAsync` per provider |
| PIM Core | `Providers/{Google,M365,OutlookCom,Imap}ProviderService.Drafts.cs` (new) | `CreateDraftAsync`, `UpdateDraftAsync` |
| PIM Core | `Providers/RRuleToGraph.cs` (new) | RRULE → `PatternedRecurrence`, refusing what Graph cannot hold |
| PIM Core | `Models/EventWrite.cs` (new) | `EventWrite` record: the C1 fields shared by create and update |
| PIM Core | `Providers/*ProviderService.Events.cs` (new) | create/update/delete with C1 fields and `scope` (C2) |
| PIM Core | `Tools/DraftTools.cs` (new), `Tools/CalendarActionTool*.cs` | `create_draft`, `update_draft`; the new arguments in the multiplexed tool |
| PIM Tests | `Providers/OutboundMimeTests.cs`, `Providers/*DraftsTests.cs`, `Providers/RRuleToGraphTests.cs`, `Providers/*EventsTests.cs`, `Providers/ImapSendTests.cs` (new) | one per unit above |
| Aura Go | `internal/agent/mcptools/bridge_risk.go`, `internal/messagedrafts/validation.go` | draft actions tiered; `bcc`, `replyToEmailId` allowed on a reviewed send |
| Aura Go | `internal/channels/telegram/hitl_message_draft.go` | Bcc in the Telegram review |
| Aura web | `web/src/approvals/MessageDraftCard.tsx`, `web/src/i18n/resources.*` | Bcc field and the reply marker in the cockpit review |

---

## PIM fork (`D:/tmp/aura-pim-mcp`)

**Before P1.** `aura/account-connect` (PR chetto1983/aura-pim-mcp#5) rewrites
`ImapProviderService.cs`. If it is not merged into `main` when P1 starts, stop and ask: branching
from `main` would conflict, branching from it would carry unreviewed work.

### Task P1: One MIME builder, and SMTP sends Bcc without the header

**Files:**
- Create: `src/CalendarMcp.Core/Models/OutboundEmail.cs`, `src/CalendarMcp.Core/Providers/OutboundMime.cs`
- Modify: `GoogleProviderService.cs` (send body → `OutboundMime`), `ImapProviderService.cs:462-540` (same)
- Test: `src/CalendarMcp.Tests/Providers/OutboundMimeTests.cs`, `src/CalendarMcp.Tests/Providers/ImapSendTests.cs`

**Interfaces:**
- Produces: `public sealed record OutboundEmail(IReadOnlyList<string> To, IReadOnlyList<string> Cc, IReadOnlyList<string> Bcc, string Subject, string Body, string BodyFormat, string? TextBody, string? HtmlBody, IReadOnlyList<OutboundEmailAttachment> Attachments);`
- Produces: `public static MimeMessage OutboundMime.Build(OutboundEmail email, MailboxAddress from, ReplyThread? thread = null)`.

- [ ] **Step 1: Write the failing tests.**
  - `Build_CarriesToCcBccSubjectAndBody`: each list lands in its header, `Bcc` included.
  - `Build_Multipart_HasTextAndHtmlAlternatives` and `Build_WithAttachment_IsMultipartMixed`: the
    shapes `SendEmailToolMultipartTests` and `SendEmailToolAttachmentIdTests` already expect.
  - `Smtp_Bcc_IsDeliveredWithoutTheHeader` (GreenMail, `PIM_TEST_GREENMAIL_SMTP`): send To
    `a@example.com`, Bcc `b@example.com`; `b` receives one message; neither delivered copy has a
    `Bcc` header.
  - `Smtp_SentCopy_KeepsTheBccHeader`: the copy `APPEND`ed to Sent has `Bcc: b@example.com`.
- [ ] **Step 2: Run them.** Expected: FAIL, `OutboundMime` does not exist.
- [ ] **Step 3: Implement `OutboundMime.Build`** from the two existing builders, deleting both.
  Before writing the SMTP part, read MailKit's `SmtpClient.Send` docs on how Bcc reaches the
  envelope and the wire bytes, and cite the page. If it does not strip the header, send a clone
  with `Bcc` removed and pass the full recipient list as the envelope.
- [ ] **Step 4: Run them.** Expected: PASS, with the existing `SendEmailTool*` tests still green.
- [ ] **Step 5: Commit** "Build every outbound message in one place, and keep Bcc off the wire".

### Task P2: Read a message whole (M1)

**Files:**
- Create: `Providers/{Google,M365,OutlookCom,Imap}ProviderService.Mail.cs`, `Providers/ReplyThread.cs`
- Modify: `Services/IProviderService.cs` (one method), the providers that do not mail (`Ics`, `JsonCalendar`, `Dav`) throw `NotSupportedException` like their other mail methods
- Test: `Providers/RawEmailTests.cs`

**Interfaces:**
- Produces: `Task<RawEmail?> GetRawEmailAsync(string accountId, string emailId, CancellationToken ct)`; `public sealed record RawEmail(byte[] Mime, string? ThreadId);` (`null` when the message is gone).
- Produces: `public sealed record ReplyThread(string MessageId, IReadOnlyList<string> References, string Subject, string? ThreadId) { public static ReplyThread From(RawEmail raw); }`.

- [ ] **Step 1: Write the failing tests.**
  - `Gmail_RawEmail_DecodesRawAndKeepsThreadId`: `GmailFixtures` answers
    `messages.get format=raw` with a base64url MIME and `threadId`.
  - `Graph_RawEmail_ReadsValue`: `GraphFixtures` answers `GET /me/messages/{id}/$value` with
    `Prefer: IdType="ImmutableId"` asserted on the request.
  - `Imap_RawEmail_FetchesTheWholeMessage` (GreenMail): bytes equal what was appended.
  - `ReplyThread_From_AppendsMessageIdToReferences`.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement** per spec D4 M1: Graph `$value`, Gmail `format=raw`, IMAP
  `UID FETCH (BODY.PEEK[])` (RFC 9051 §6.4.5; `PEEK` so reading never marks it read).
- [ ] **Step 4: Run them.** Expected: PASS.
- [ ] **Step 5: Commit** "Read a message whole, for the reply that follows it".

### Task P3: One send path for every provider, with Bcc and replies (M2)

**Files:**
- Modify: `Services/IProviderService.cs` (`SendEmailAsync` → `SendMimeAsync`), the four `*.Mail.cs`, `Tools/SendEmailTool.cs`, `Tools/CalendarActionArguments.cs`, `Tools/CalendarActionTool.cs` (description), `AURA-FORK.md`
- Delete: the four old `SendEmailAsync` bodies (M365's JSON `SendMailPostRequestBody` path included)
- Test: `Tools/SendEmailToolReplyTests.cs`, `Providers/SendMimeTests.cs`

**Interfaces:**
- Consumes: `OutboundMime.Build` (P1), `GetRawEmailAsync`, `ReplyThread.From` (P2).
- Produces: `Task<string> SendMimeAsync(string accountId, MimeMessage message, string? threadId, CancellationToken ct)`.
- Produces: `send_email` arguments `bcc: string[]?` and `replyToEmailId: string?`.

- [ ] **Step 1: Write the failing tests.**
  - `Gmail_SendMime_PostsRawWithThreadId`: the request body has `raw` and the reply's `threadId`.
  - `Graph_SendMime_PostsBase64AsTextPlain`: `POST /me/sendMail`, `Content-Type: text/plain`, body
    is the base64 of the MIME (createMessage/sendMail MIME pages).
  - `Imap_SendMime_SendsThenFilesOneSentCopy` (GreenMail).
  - `ReplyToEmailId_SetsInReplyToAndReferences`: the built message carries the original's
    Message-ID in `In-Reply-To` and at the end of `References`.
  - `ReplyToEmailId_FromAnotherAccount_IsRefused`: the error names `replyToEmailId`; no provider
    call is made.
  - `ReplyToEmailId_Gone_IsRefused`: `GetRawEmailAsync` returns null; the error names the argument.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement** `SendMimeAsync` per spec D4 M2. Graph sends through a Kiota
  `RequestInformation` on `/me/sendMail`, since the SDK's `SendMail.PostAsync` takes only JSON.
  A reply keeps the subject the caller gives.
- [ ] **Step 4: Run them, then the whole suite.** Expected: PASS; no `SendEmailAsync` left
  (`git grep -n SendEmailAsync src` is empty).
- [ ] **Step 5: Commit** "Send every message as MIME, with Bcc and the reply's thread".

### Task P4: Drafts in the provider's Drafts (M9)

**Files:**
- Create: `Providers/{Google,M365,OutlookCom,Imap}ProviderService.Drafts.cs`, `Tools/DraftTools.cs`
- Modify: `Services/IProviderService.cs`, `Tools/CalendarActionTool.cs` (`create_draft`, `update_draft`), `AURA-FORK.md`
- Test: `Providers/GmailDraftsTests.cs`, `Providers/GraphDraftsTests.cs`, `Providers/ImapDraftsTests.cs`, `Tools/DraftToolsTests.cs`

**Interfaces:**
- Consumes: `OutboundMime.Build`, `ReplyThread`, `GetRawEmailAsync`.
- Produces: `Task<string> CreateDraftAsync(string accountId, MimeMessage message, string? threadId, CancellationToken ct)` and `Task<string> UpdateDraftAsync(string accountId, string draftId, DraftChange change, CancellationToken ct)`, both returning the id to use next.
- Produces: `public sealed record DraftChange(IReadOnlyList<string>? To, IReadOnlyList<string>? Cc, IReadOnlyList<string>? Bcc, string? Subject, string? Body, string? BodyFormat);` (null = unchanged).
- Produces: actions `create_draft` (the `send_email` arguments) → `{draftId}`; `update_draft` (`draftId` + `DraftChange` fields) → `{draftId}`.

- [ ] **Step 1: Write the failing tests.**
  - `Gmail_CreateDraft_PostsRawAndThreadId` and `Gmail_UpdateDraft_PutsTheWholeMessageUnderTheSameDraftId`.
  - `Graph_CreateDraft_PostsMimeToMessages` and `Graph_UpdateDraft_PatchesOnlyTheChangedProperties`
    (`subject`, `body`, `toRecipients`, `ccRecipients`, `bccRecipients`; message-update page).
  - `Imap_CreateDraft_AppendsWithDraftFlagIntoDrafts` and
    `Imap_UpdateDraft_AppendsTheNewOneAndExpungesTheOld` (GreenMail; the new id from `APPENDUID`,
    RFC 4315 §3).
  - `UpdateDraft_KeepsTheAttachmentsOfTheStoredDraft` (Gmail fake, GreenMail): the update reads the
    stored draft through `GetRawEmailAsync`, changes only the given fields, keeps every MIME part.
  - `UpdateDraft_WithAttachments_IsRefused`: the error names `attachments`.
  - `Drafts_MissingFolder_IsAnErrorNotACreate`: IMAP `APPEND` never creates the mailbox (RFC 9051
    §6.3.12), and neither does the PIM.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement** per spec D4 M9.
- [ ] **Step 4: Run them.** Expected: PASS.
- [ ] **Step 5: Commit** "Leave a draft in the provider's Drafts, and change it in place".

### Task P5: A recurring event (C1, recurrence)

**Files:**
- Create: `Models/EventWrite.cs`, `Providers/RRuleToGraph.cs`, `Providers/*ProviderService.Events.cs` (Google, M365, OutlookCom, Dav)
- Modify: `Services/IProviderService.cs` (`CreateEventAsync`/`UpdateEventAsync` take `EventWrite`), `Tools/CreateEventTool.cs`, `Tools/UpdateEventTool.cs`, `Tools/CalendarActionArguments.cs`, `AURA-FORK.md`
- Test: `Providers/RRuleToGraphTests.cs`, `Providers/EventsRecurrenceTests.cs`

**Interfaces:**
- Produces: `public sealed record EventWrite(string? Subject, DateTime? Start, DateTime? End, string? TimeZone, bool? IsAllDay, string? Location, IReadOnlyList<string>? Attendees, string? Body, string? Recurrence, int? ReminderMinutes, string? ShowAs, bool? IsPrivate, IReadOnlyList<string>? Categories);` (P6 fills the last four).
- Produces: `public static PatternedRecurrence RRuleToGraph.Convert(string rrule, DateTime start, string timeZone)`, throwing `RecurrenceNotRepresentable(string part)`.

- [ ] **Step 1: Write the failing tests.**
  - `RRuleToGraph` table: `FREQ=DAILY;INTERVAL=2;COUNT=5` → `daily`/2/`numbered` 5;
    `FREQ=WEEKLY;BYDAY=MO,WE;UNTIL=…` → `weekly`, `daysOfWeek`, `endDate`;
    `FREQ=MONTHLY;BYMONTHDAY=15` → `absoluteMonthly`; `FREQ=MONTHLY;BYDAY=2TU` →
    `relativeMonthly`, `index` `second`; `FREQ=YEARLY;BYMONTH=3;BYMONTHDAY=1` → `absoluteYearly`.
  - `RRuleToGraph_Refuses`: `BYHOUR`, `BYSETPOS` with several values, `FREQ=HOURLY`, and two
    `RRULE`s each throw naming the part.
  - `Graph_UnrepresentableRrule_SendsNoRequest`: `FakeHttp` records zero requests.
  - `Google_CreateEvent_SendsRecurrenceArray`: `recurrence: ["RRULE:…"]`.
  - `Dav_CreateEvent_WritesRrule`: the PUT body has the `RRULE` line (RFC 5545 §3.8.5.3).
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.** Parse with Ical.Net's `RecurrencePattern` (no hand-written RRULE
  parser). Graph's `recurrence` per the `patternedRecurrence` page.
- [ ] **Step 4: Run them.** Expected: PASS.
- [ ] **Step 5: Commit** "Write a recurring event on every calendar provider".

### Task P6: Reminder, free/busy, privacy and categories (C1, the rest)

**Files:** the `*.Events.cs` files and their tests; `AURA-FORK.md`.

**Interfaces:** `EventWrite.ReminderMinutes`, `ShowAs` (`free`, `busy`, `tentative`, `oof`,
`workingElsewhere`), `IsPrivate`, `Categories`. Details and listings return the same four.

- [ ] **Step 1: Write the failing tests**, one round trip per provider:
  - Graph: `isReminderOn`/`reminderMinutesBeforeStart`, `showAs`, `sensitivity: private`,
    `categories`.
  - Google: `reminders: {useDefault:false, overrides:[{method:"popup", minutes}]}`,
    `transparency` (`free` → `transparent`, anything else `opaque`), `visibility: private`, and
    categories in `extendedProperties.private["categories"]` as a JSON array (spec D4 C1: Google has
    no categories field).
  - `Google_ShowAsTentative_IsBusyWithAWarning`: `opaque`, and the result's `warnings` names
    `showAs`.
  - CalDAV: `VALARM` with `TRIGGER:-PT<n>M`, `TRANSP`, `CLASS:PRIVATE`, `CATEGORIES`; an update
    keeps an alarm it did not set (RFC 4791).
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run them.** Expected: PASS.
- [ ] **Step 5: Commit** "Carry an event's reminder, free/busy, privacy and categories".

### Task P7: One occurrence, the series, or this and following (C2)

**Files:** the `*.Events.cs` files, `Tools/UpdateEventTool.cs`, `Tools/DeleteEventTool.cs`, `Tools/GetCalendarEventsTool.cs`, `Models/CalendarEvent.cs`; tests; `AURA-FORK.md`.

**Interfaces:**
- Produces: listings and details carry `seriesId` (Graph `seriesMasterId`, Google
  `recurringEventId`, CalDAV the resource href) and `originalStartUtc`; a series master's details
  carry `recurrence` as the RRULE.
- Produces: `update_event`/`delete_event` argument `scope`: `occurrence` (default for an
  occurrence id), `series` (default for a master id), `following`.

- [ ] **Step 1: Write the failing tests.**
  - `Occurrence_Update_TouchesOnlyThatInstance` (Google: PUT on the instance id; Graph: PATCH on
    the occurrence id; CalDAV: an override with `RECURRENCE-ID`).
  - `Occurrence_Delete` (Google: per G6's outcome, `status:"cancelled"` until L2 measures it;
    Graph: DELETE on the occurrence; CalDAV: `EXDATE`).
  - `Series_Update_TouchesTheMaster`.
  - `Following_TruncatesTheMasterAndStartsANewSeries`: the master's RRULE gains
    `UNTIL=<originalStart - 1 s>` and a new series starts at the occurrence with the remaining
    count (spec G5 method), on all three providers.
  - `Following_FromTheFirstOccurrence_UpdatesTheSeries`: no truncation, one series update.
  - `Scope_FollowingOnASingleEvent_IsRefused`: the error names `scope`.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement** per spec D4 C2.
- [ ] **Step 4: Run them.** Expected: PASS.
- [ ] **Step 5: Commit** "Change one occurrence, the series, or this and following".

### Task P8: Answer an invitation with a comment (C3)

**Files:** the `*.Events.cs` files, `Tools/RespondToEventTool.cs`; tests; `AURA-FORK.md`.
Delete `GoogleProviderService.cs:1024-1028` (the comment logged and dropped).

- [ ] **Step 1: Write the failing tests.**
  - `Google_Respond_PatchesOwnAttendeeWithComment`: `events.patch` with the own attendee's
    `responseStatus` and `comment`, `sendUpdates=all`, and `attendeesOmitted` per spec D4 C3.
  - `Graph_Respond_SendsComment` (already the behaviour; pin it).
  - `Dav_Respond_AutoSchedule_PutsPartstat`: the server advertises `calendar-auto-schedule`, so
    the PUT changes `PARTSTAT` and carries a `COMMENT` (RFC 6638 §3.2, read before writing).
  - `Dav_Respond_NoAutoSchedule_SendsAnImipReply`: an RFC 5546 §3.2.3 `REPLY` through
    `SendMimeAsync` on the same account, `METHOD:REPLY`, as `text/calendar` (RFC 6047).
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run them.** Expected: PASS.
- [ ] **Step 5: Commit** "Send the comment with an answer to an invitation".

## Aura

### Task A1: The draft actions, and Bcc and replies in the reviewed send

**Files:**
- Modify: `internal/agent/mcptools/bridge_risk.go` (`create_draft`, `update_draft`: `MCPActionMutate`), `internal/messagedrafts/validation.go` (allow `bcc`, editable; `replyToEmailId`, fixed), `internal/messagedrafts/review.go`, `internal/channels/telegram/hitl_message_draft.go`, `web/src/approvals/MessageDraftCard.tsx`, `web/src/i18n/resources.*`
- Test: `internal/agent/mcptools/bridge_risk_test.go`, `internal/messagedrafts/validation_test.go`, `internal/channels/telegram/hitl_message_draft_test.go`, `web/src/approvals/__tests__/MessageDraftCard.test.tsx`

- [ ] **Step 1: Grep every counted list of calendar actions**, tagged tests included
  (`git grep -n -E 'calendarRecipeSource|send_email' -- '*_test.go'` and the `//go:build` files),
  before changing the table.
- [ ] **Step 2: Write the failing tests.**
  - `TestDraftActionsAreMutateNotOutbound`: both tiered `Mutate`; `TrustedOutboundTarget` is false
    for them, so no review is asked.
  - `TestMergeApprovedArgsBccIsEditableReplyToIsFixed`: `bcc` edits apply; an override of
    `replyToEmailId` is refused naming it.
  - `TestTelegramDraftShowsBcc`: the Telegram review text lists the Bcc addresses.
  - `MessageDraftCard` shows and edits Bcc, and marks a reply ("In risposta a un messaggio").
- [ ] **Step 3: Run them.** Expected: FAIL.
- [ ] **Step 4: Implement.**
- [ ] **Step 5: Run the package suites and the web gates.** Expected: PASS.
- [ ] **Step 6: Commit** "Review Bcc and replies before a send, and let drafts through as writes".

## Live

### Task L1: Wave A on the lab VM (after P1-P4 and A1)

- [ ] **Step 1: Release.** Merge the fork branch to `main`; the fork's
  `aura-publish-image.yml` publishes the image. Push Aura (WSL recipe), CI green on the SHA. The VM
  updater applies both; no manual step on the VM.
- [ ] **Step 2: Measure** on the VM's Gmail, Microsoft 365 and IMAP accounts, driving the agent
  through the cockpit API, with the rules the operator set on 2026-11-23: mail only between the
  operator's own accounts, subject prefix `[aura-misura]`, one measurement at a time, everything
  deleted after.
  - G1: a reply sent with `replyToEmailId` from Microsoft 365 arrives in the original thread.
  - G2: Gmail Bcc delivered; no `Bcc` in the delivered copy; whether the Sent copy keeps it.
  - Graph Bcc: the same on Microsoft 365.
  - G7: IMAP Sent holds one copy, or two.
  - G8: a Graph draft's id and `conversationId` after `update_draft`, and a reply draft's thread.
  - G9: a Gmail draft stays in its thread after `update_draft`.
- [ ] **Step 3: Record** `docs/superpowers/verification/<date>-pim-mail-rows-live.md`: field
  names, counts and yes/no, and what the run does not show.
- [ ] **Step 4: Amend** the spec for each result that contradicts it (date and evidence), and turn
  the spec's "What decides" fallback into a task before L2.

### Task L2: Wave B on the lab VM (after P5-P8)

- [ ] **Step 1: Release**, as L1.
- [ ] **Step 2: Measure.**
  - G3: Microsoft 365 organizes an event and invites the Google account. Google answers with a
    comment; the organizer receives it.
  - G5: "this and following" on a weekly Google series.
  - G6: deleting one Google occurrence leaves the series.
  - C1 round trip: reminder, show-as, private and categories read back on Google and Microsoft 365.
- [ ] **Step 3: Record and amend**, as L1.

## Decisions taken while writing this plan

1. **One send path.** `SendMimeAsync` replaces `SendEmailAsync` everywhere instead of sitting beside
   it, so Microsoft 365 and Outlook.com leave the JSON `sendMail` body. The spec chose MIME for
   Graph (D4 M2); two paths would be dead code waiting to diverge.
2. **The Sent copy and drafts keep Bcc; delivered copies never.** Every mail client keeps the Bcc in
   the sender's copy. The spec's Acceptance line "Bcc delivered and absent from every header" is
   read as every delivered copy. To confirm with the operator, and amend the spec if confirmed.
3. **M1 is a provider method here, not yet an MCP action or an `/admin` route.** Its only consumer
   in this plan is the reply. The MailCraft facade (`internal/mailhost`) is its other consumer and
   adds the route in its own plan.
4. **The PIM resolves the reply.** `replyToEmailId` keeps headers out of the model's hands and out
   of the review card.
5. **`update_draft` changes fields, never attachments.** Graph's update is JSON only, and Gmail and
   IMAP replace the whole message, so one rule for all: attachments are set at creation, and an
   update keeps them.
6. **Recurrence is one RRULE string**, the standard two of the three providers speak. Graph gets a
   converter over a documented subset that refuses the rest, so nothing is half-written.
7. **Two waves, each followed by its live measurement**, instead of a release per row: a release is
   a fork merge, an image and a VM update, and the rows in a wave share the same send or event
   code.
8. **The large provider files are not split here.** New code goes into new partial files and the
   rewritten code leaves the old ones, as the 2026-11-20 plan did.
