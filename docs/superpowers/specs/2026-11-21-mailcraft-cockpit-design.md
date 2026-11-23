# Mail, calendar and contacts in the cockpit through MailCraft

Date: 2026-11-21. The operator's direction:
1. *"Potremmo riusare la Ui di mail craft?"*
2. *"Io integrerei anche i calendari alla fine sono solo delle API da esporre la UI è già fatta"*
3. *"Fai il pull del PIM e guarda come fare tutto senza sì e senza ma"*
4. On a first draft that left contacts read-only and meeting forwarding and proposals out: *"o si ha
   un client completo o niente"*.

This spec is the complete client. Nothing in it is left for later.

Evidence:
- **`spikes/mailcraft-web/FINDINGS.md`.** Verdicts 1–9: MailCraft v0.14.0's official web build framed
  under `internal/staticapp`, its JMAP calls logged.
- **`aura-pim-mcp` at `bb87b828`.** Latest `main`, pulled 2026-11-21.
- **MailCraft's source at v0.14.0**, in particular:
  - `crates/model/src/remote.rs`;
  - `crates/ui-egui/src/calendar_sync.rs` and `crates/ui-egui/src/lib.rs` (`Services`);
  - `crates/host/src/calendar.rs`;
  - `crates/net/src/graph.rs`;
  - `crates/engine/src/cmd/calendar/invitation.rs`.
- **Provider documentation and standards, read 2026-11-21**, cited by page or section in D4:
  learn.microsoft.com/graph (v1.0), developers.google.com (Gmail API, Calendar API v3, People API), and
  RFC 9051, 4315, 4731, 5321, 5322, 5545, 5546, 6047, 6154, 6350, 6352, 6578, 6638, 6851, 7162, 8474,
  9110 and 9394.
- **Drafts (M9), added 2026-11-23.** The work-assistant requirements proposed in PR #274 rest on a
  draft the agent leaves in the provider's Drafts for the owner to send. Pages read 2026-11-23: Gmail
  `guides/drafts`, `guides/threads`, `users.drafts.create`/`update`; Graph `user-post-messages`,
  `message-update`, `message-createreply`, `outlook-immutable-id`; RFC 9051 §2.3.1.1, §2.3.2, §6.3.12,
  RFC 6154 and RFC 4315 §3.

## What changes

1. **The cockpit gets a complete mail, calendar and contacts client: MailCraft's web build**, served
   at `/apps/mailcraft/` like the Craft apps.
   - It shows the identity's own accounts, already connected in the PIM, with no second sign-in.
   - **Mail:** read, write, reply, forward, flag, file, delete and search.
   - **Calendar:** events and series, invitations, answers, forwarding meetings and proposing new
     times.
   - **Contacts:** read and written.
2. **Mail travels over JMAP** (RFC 8620/8621). Aura answers on its own origin, `/mail/jmap/`, from the
   PIM. MailCraft's JMAP client is used as published.
3. **Calendars and contacts travel over MailCraft's own sync contract**, `model::remote`.
   - MailCraft's browser build has no calendar client: its Graph, subscribed-calendar and free/busy
     runs are native-only (`crates/host/src/calendar.rs`, `#[cfg(not(target_arch = "wasm32"))]`).
   - Its engine, though, exchanges events, calendars and contacts with its host through serde types.
   - A patch lets the page be that host. Aura answers at `/mail/calendar` from the PIM.
4. **MailCraft gains contact write-back.** Today a server's contacts only come down (`ContactsDownload`;
   there is no upload type), for every account kind. The patch adds the contact side of what events
   already have: a change log in the mailbox, an upload, an outcome.
5. **The PIM becomes a complete provider layer**, in our fork `chetto1983/aura-pim-mcp`, for Microsoft
   Graph, Google (Gmail, Calendar, People) and IMAP/SMTP + CalDAV/CardDAV. D4 lists every operation and
   the documented call behind it.

## Decisions

### D1. JMAP for mail, MailCraft's remote contract for calendars and contacts

- **Mail over JMAP.** It is a published standard, MailCraft's JMAP client ships in the build we serve,
  and the spike measured its whole surface:
  - session and `Mailbox/get`;
  - `Email/query` and `Email/get`;
  - the `message/rfc822` download;
  - `Email/set` patches;
  - upload, `Email/import`, `Identity/get` and `EmailSubmission/set`.
- **Calendars and contacts over MailCraft's remote contract.** It is the one MailCraft's calendar code
  speaks.
  - Events: create, update, delete, respond and forward, with proposed times, series, exceptions,
    attendees, show-as, private, reminders, categories and time zones.
  - Calendars and contacts.
  - The types derive `Serialize`/`Deserialize` (`camelCase`), so the page carries them as JSON
    unchanged.
- **Rejected:**
  - **JMAP Calendars and Contacts:** MailCraft has no client.
  - **CalDAV/CardDAV in MailCraft:** roadmap item 9, desktop-first, no date for the browser.
  - **Emulating Graph:** its mail contract carries Favorites, inbox passes and folder links nobody
    else models.

### D2. The MailCraft patch

`docker/craft-web/mailcraft.patch`, built by `docker/craft-web/build.sh`. It has three parts.

1. **The host hook** (`apps/mailcraft-web/src/calendar.rs`, beside upstream's `jmap.rs`).
   - **Who it serves.** It implements `Services::graph` for the accounts the page names in
     `window.mailcraftHost.accounts`. It uses the page's bearer from `window.mailcraftJmap.secret`,
     the hook upstream already has.
   - **What it sends.** `{account, changes: EventsUpload, contactChanges: ContactsUpload, asks:
     [CalendarAsk], calendar: [from, to], directory}`, POSTed to the page-given address.
   - **What it reads back.** `{events, moreEvents, uploaded, contactsUploaded, calendars, contacts,
     errors}`.
   - **Account kind.** `crates/ui-egui/src/calendar_sync.rs` `calendar_job` today requires an OAuth
     sign-in. It runs instead for a host-named account, which needs no token of its own.
2. **Contact write-back**, in the engine and model:
   - `Mailbox::contact_changes`, noted by the People commands for a contact whose `origin` is a host
     account;
   - `model::remote::ContactsUpload` / `ContactChange` (create, update, delete, with the server's
     etag) and `ContactsUploaded`;
   - the same retry, refusal and "put back what the server refused" handling as `event_changes`;
   - tests beside upstream's `meeting/tests.rs`.
3. **Nothing else.** Mail stays on upstream's JMAP path. Meeting forwarding and proposals are already
   in the engine (`EventChangeKind::Forward`, `EventChange.proposed`, and `invitation.rs` reading an
   incoming `COUNTER`).

**Upstream.** Both parts are offered to UCS-Care-Group: the hook as a generalisation of
`mailcraftJmap`, the write-back as the base their CardDAV item needs. Each part they take leaves the
patch, as WordCraft's and PdfCraft's did. MailCraft's own terms (licence MIT OR Apache-2.0, and any
brand terms) are read before the patched build ships.

### D3. Aura's endpoints, per identity, for the cockpit only

New package `internal/mailhost` (files ≤ 600 LOC), mounted by `aura serve` with the cockpit routes.

**`GET /mail/config.js`**
- Declares `window.mailcraftJmap` and `window.mailcraftHost`:
  - the identity's accounts from `list_accounts`;
  - `/mail/jmap/session`;
  - `/mail/calendar`;
  - `secret()` that fetches a bearer from `/mail/token`.
- `index.html` loads it before the module script.

**`POST /mail/token`**
- Mints a bearer bound to the cockpit session and the identity: minutes long, audience `/mail/*`.
- Only for a session cookie that passes the cockpit's CSRF check.
- Never for an agent's or a sidecar's credential. The agent's tools and the browser sidecar cannot
  reach `/mail/*`.

**`/mail/jmap/*`**

| JMAP | PIM (D4 row) |
|---|---|
| session, `Identity/get` | `list_accounts` with each sending address (`c127b8d`) |
| `Mailbox/get` | folders (M8), counts (M5) |
| `Email/query`: `inMailbox`, receivedAt desc, `position`/`limit`, `calculateTotal` | paging (M4), counts (M5) |
| `Email/get`: ids, keywords, mailboxIds, headers | listing (M4); headers from the raw message (M1) |
| download `message/rfc822`; attachments | raw message (M1) |
| `Email/set` `keywords/$seen`, `keywords/$flagged` | read mark (existing `mark_email_read`), flag (M3) |
| `Email/set` `mailboxIds/<from\|to>`, destroy | move (existing `move_email`), delete (existing `delete_email`) |
| upload, `Email/import` into Sent, `EmailSubmission/set` | raw send (M2); the Sent copy per M2's rule |

**`POST /mail/calendar`**

| Contract | PIM (D4 row) |
|---|---|
| `calendars` | `list_calendars` |
| `events`, `moreEvents` for a stretch | events with series masters, occurrences and exceptions (C2) |
| Create, Update, Delete; one occurrence or the series | event writes (C1, C2) |
| Respond, with comment | answers (C3) |
| Respond with `proposed` | proposals (C5) |
| Forward | forwarding (C4) |
| `contacts`; `contactChanges` create, update, delete | contacts (K1, K2) |

**State and ids**
- One table maps provider ids to stable JMAP and remote ids per identity and account, using the
  provider's stable id where it has one (M7). Migration number from
  `ls internal/db/migrations | tail -1` at landing.
- JMAP `state` strings and the calendar's freshness come from change tracking (M6, C6, K3) behind
  the PIM's `/admin/changes` cursor, which `pimwatch` already uses. A poll that finds nothing costs one
  cursor read.

**Who acts**
- What the owner does in this client is the owner's own act. Sending, deleting, moving, answering and
  forwarding here do not go through the approvals `messagedrafts` gives the agent. MailCraft's own
  confirmations apply.
- Every write is logged to `audit` with identity, account and action, never content.

### D4. The PIM fork: every operation, per provider

Each row is a provider method in `IProviderService`, an MCP action and its `/admin` REST twin, and a
row in `internal/agent/mcptools/bridge_risk.go`. The call named is the documented one, and the code
cites its page.

**Mail**

| # | Operation | Microsoft Graph v1.0 | Gmail API | IMAP / SMTP |
|---|---|---|---|---|
| M1 | Raw message | `GET /me/messages/{id}/$value` (`Mail.Read`) | `users.messages.get format=raw` | `UID FETCH (BODY.PEEK[])`, RFC 9051 §6.4.5 |
| M2 | Send raw MIME; Bcc; threading; Sent copy | `POST /me/sendMail`, MIME base64 (`Mail.Send`); Bcc via the MIME headers (createMessage page); saved to Sent Items | `users.messages.send raw` with `threadId`, `In-Reply-To`, `References`; `SENT` applied automatically | SMTP: every To/Cc/Bcc as `RCPT TO`, the `Bcc:` header stripped by us (RFC 5321 App. B; RFC 6409 does not strip it); Sent copy by `APPEND` to the `\Sent` mailbox (RFC 6154), with a per-account switch for servers that file it themselves |
| M3 | Flag | `PATCH` `flag.flagStatus` (`Mail.ReadWrite`) | `messages.modify` `STARRED` | `UID STORE ±FLAGS (\Flagged)` |
| M4 | Listing pages | `$top` + `@odata.nextLink` as given (never parse `$skip`) | `messages.list` `labelIds`, `maxResults` ≤ 500, `pageToken` | `UID SEARCH` / `ESEARCH`; `PARTIAL` (RFC 9394) when offered, UID ranges otherwise |
| M5 | Counts | mailFolder `totalItemCount`, `unreadItemCount` | `labels.get` `messagesTotal`, `messagesUnread` (exact; `resultSizeEstimate` is not) | `STATUS (MESSAGES UNSEEN)`; `ESEARCH COUNT` (RFC 4731) |
| M6 | Change tracking | `mailFolders/{id}/messages/delta` | `users.history.list` (404 → full resync) | `CONDSTORE`/`QRESYNC` (RFC 7162) when offered, a UID+flags rescan otherwise |
| M7 | Stable ids | `Prefer: IdType="ImmutableId"` on every request (survives folder moves) | `Message.id` immutable | `OBJECTID` `EMAILID` (RFC 8474) when offered; else UIDVALIDITY+UID, carried across moves by `COPYUID` (RFC 4315 §3, mandatory in IMAP4rev2) |
| M8 | Folders | `mailFolders` (and child folders) | `users.labels.list` | `LIST` with special-use (RFC 6154) |
| M9 | Draft: create, update, reply draft | Create: `POST /me/messages`, JSON or MIME base64 as `text/plain`, saved in Drafts (`Mail.ReadWrite`). Reply draft: `POST /me/messages/{id}/createReply`, JSON or MIME. Update: `PATCH /me/messages/{id}`, JSON only (no MIME form documented); `subject`, `body`, `toRecipients`, `ccRecipients`, `bccRecipients`, `replyTo` are "updatable only if isDraft = true". Id: the immutable id holds while the item stays in the mailbox, and the documented send workflow reads the Sent Items copy by the draft's id | Create: `users.drafts.create` with `message.raw` (base64url RFC 2822) and `message.threadId`. Update: `users.drafts.update` replaces the whole message: the **draft id is immutable**, the **message id changes on every update**, so the id map keys a draft by its draft id. Thread: `threadId`, RFC 2822 `In-Reply-To`/`References` and a matching `Subject`, all three (`guides/threads`). A draft carries only the `DRAFT` label. Scope `gmail.compose` | `APPEND` into the `\Drafts` mailbox (RFC 6154) with the `\Draft` flag (RFC 9051 §2.3.2, §6.3.12); `APPEND` never creates the mailbox. A message's text never changes (§2.3.1.1): an update is a new `APPEND`, then `\Deleted` + `UID EXPUNGE` of the old one; the new UID comes from `APPENDUID` (RFC 4315 §3). The id map keeps one stable id across the replacements |

**Calendar**

| # | Operation | Microsoft Graph v1.0 | Google Calendar API v3 | CalDAV / iTIP |
|---|---|---|---|---|
| C1 | Create, update, delete with recurrence, reminder, show-as, private, categories, body | `POST /me/events`, `PATCH`, `DELETE`: `recurrence`, `isReminderOn`/`reminderMinutesBeforeStart`, `showAs`, `sensitivity`, `categories`, `body` | `events.insert`, `events.patch` (arrays replace), `events.delete`: `recurrence[]` (RRULE), `reminders.overrides`, `transparency`, `visibility`, `description`; **categories in `extendedProperties.private`** (no categories field exists) | `PUT` with `If-None-Match: *` / `If-Match` (RFC 4791 §5.3.2, RFC 9110): `RRULE`, `VALARM`, `TRANSP`, `CLASS`, `CATEGORIES` (RFC 5545); existing alarms kept (RFC 4791) |
| C2 | Series, occurrences, exceptions | `type`, `seriesMasterId`, `originalStart`, `changeKey`; `/events/{master}/instances`; `calendarView` | `recurringEventId`, `originalStartTime`; `events.instances`; one instance changed by PUT on its id; cancelled by `status:"cancelled"`; `If-Match` etag | master `RRULE` + overrides with `RECURRENCE-ID` in one resource; `EXDATE` cancels one (RFC 5545, RFC 4791 §4.1) |
| C3 | Answer an invitation, with comment | `POST /events/{id}/accept\|tentativelyAccept\|decline` `{comment, sendResponse}` | `events.patch` own `attendees[].responseStatus` and `comment`, `attendeesOmitted`, `sendUpdates` | implicit scheduling (RFC 6638) when the server advertises `calendar-auto-schedule`; otherwise iMIP `REPLY` (RFC 5546 §3.2.3, RFC 6047) through M2 |
| C4 | Forward a meeting | `POST /events/{id}/forward` `{ToRecipients, Comment}` | **no API**: iMIP forward, the original `REQUEST` sent unchanged (RFC 5546 §3.2.2.6) through M2 | iMIP forward, as for Google (RFC 6638 does not cover it) |
| C5 | Propose a new time; answer a proposal | `tentativelyAccept`/`decline` with `proposedNewTime` (`sendResponse:true`, if the event allows); proposals read from `attendees[].proposedNewTime`; accepting is a `PATCH` of start/end | **no API**: iMIP `COUNTER` (RFC 5546 §3.2.7) through M2; an incoming COUNTER reaches MailCraft as mail, which it already reads (`invitation.rs`); accepting is an update with `sendUpdates:"all"`, declining an iMIP `DECLINECOUNTER` (§3.2.8) | iMIP `COUNTER` / `DECLINECOUNTER`, as for Google |
| C6 | Change tracking | `calendarView/delta` (primary calendar); a windowed rescan for the others | `events.list` `syncToken` (410 → full sync) | `sync-collection` REPORT (RFC 6578) + `calendar-multiget` |

**Contacts**

| # | Operation | Microsoft Graph v1.0 | People API | CardDAV |
|---|---|---|---|---|
| K1 | Read, create, update, delete | `/me/contacts` GET/POST/PATCH/DELETE (`Contacts.ReadWrite`): emails, phones (`businessPhones`, `homePhones`, `mobilePhone`), three addresses, `birthday`, `personalNotes`, company, job title, department; `displayName` re-sent on update | `people.createContact`, `updateContact` (`updatePersonFields`, current `etag`, refetch on 400), `deleteContact` | `PUT` `text/vcard` with `If-None-Match`/`If-Match`, `DELETE` (RFC 6352); vCard 4.0 when offered, else 3.0 (RFC 6350) |
| K2 | Groups and folders | `contactFolders` | `contactGroups`, `members:modify` | address books |
| K3 | Change tracking | `contactFolders/{id}/contacts/delta` | `connections.list` `syncToken` (7 days) | `sync-collection` + `addressbook-multiget` |

- **The fork already has the IMAP/CalDAV/CardDAV half.** `DavProviderService` (CalDAV + CardDAV) and
  `ImapProviderService` are the base for the third column.
- **Every iMIP row is an RFC 5546 message**, built by the fork and sent through M2 on the same
  account. The fork never asks the CalDAV outbox to send: RFC 6638 defines POST there for free/busy
  only.

**Risk table:**
- **Read:** M1, M4, M5, M8 and the K reads.
- **Mutate:** M3, M9, contact create and update. M9 sends nothing: it is the mail write the agent can
  make without the send approval, and sending the draft stays the owner's act.
- **Destructive:** M2, C3, C4, C5 and contact delete, with the escalate-only reasoning
  `bridge_risk.go` already applies.

### D5. Packaging and the cockpit

- **Build.** `docker/craft-web/build.sh` takes the repository's owner (MailCraft is
  `UCS-Care-Group/mailcraft`). MailCraft joins the Craft web image, pinned by tag and commit with its
  patch. `publish-craft-web.yml`, the pin test, `craft_osv.sh` and its test follow.
- **Cockpit.** A *Posta* entry frames `/apps/mailcraft/` full-height. The agent keeps the PIM's tools,
  unchanged.
- **Language.** The interface follows the browser (FINDINGS 1: Italian measured).

## Gate 1: measured live before code

The documentation leaves nine behaviours open. Each is measured on a real account and recorded in
`docs/superpowers/verification/`. The design is amended if a result contradicts it.

| # | Behaviour | Where | What decides |
|---|---|---|---|
| G1 | `In-Reply-To`/`References` kept by `sendMail` with MIME | Microsoft 365 | if dropped: reply through `createReply` + MIME instead |
| G2 | `Bcc:` stripped from delivered and Sent copies by `messages.send` | Gmail | if kept: the fork strips it and sends one copy per Bcc |
| G3 | `attendees[].comment` reaches the organizer | Google | if not: the comment also goes as an iMIP `REPLY` |
| G4 | an incoming iMIP `COUNTER` handled by Google Calendar | Google, as organizer | none: the COUNTER is the standard; Google showing it as mail is its behaviour, recorded |
| G5 | "this and following" in a series | Google | the method: truncate with `UNTIL` and start a new series |
| G6 | `events.delete` on an instance id | Google | if it deletes the series: cancel with `status:"cancelled"` only |
| G7 | the server already files sent mail | each IMAP host in use | the per-account Sent switch's default |
| G8 | a draft's immutable id, `conversationId` and `In-Reply-To` after a `PATCH`, and for a `createReply` draft | Microsoft 365 | if the id changes: the id map re-keys on the `PATCH` answer; if the thread is lost: reply drafts are created from MIME with the headers set |
| G9 | a `drafts.update` carrying `threadId` and the thread headers keeps the draft in its thread | Gmail | if not: an update is `drafts.delete` + `drafts.create` with the thread fields, and the id map re-keys |

Also measured before code: what the PIM returns today for `get_email_details` and
`get_calendar_events` (series, time zones), on one Gmail and one Microsoft 365 account.

## Not in scope

- **Providers the PIM does not connect today.** Google, Microsoft 365, Outlook.com and IMAP/SMTP +
  CalDAV/CardDAV are covered.
- **Telegram and WhatsApp.** They are not mail clients.
- **A MailCraft MCP server in the cockpit.** The agent works through the PIM.

## Acceptance

Every case is run in the cockpit on a Gmail, a Microsoft 365 and an IMAP+CalDAV/CardDAV account, and
checked on the provider, not in MailCraft. The results go in a dated verification file.

**Mail**
- List, page, search, read; read and unread; flag; file; delete.
- Write with an attachment; reply and reply-all keep the thread; forward.
- Bcc delivered and absent from every header.
- Sent copy present once.
- A draft made through M9 shows in the provider's Drafts once; updating it twice leaves one draft; a
  reply draft sits in the original thread; sending it from the provider leaves one Sent copy and no
  draft.

**Calendar**
- Single event and weekly series: create, move, edit, delete.
- One occurrence edited and one cancelled; "this and following".
- Reminder, show-as, private and categories round-trip.
- Answer with a comment.
- Forward to a third address, which receives the invitation.
- Propose a new time, which the organizer receives; as organizer, accept one proposal and decline
  another.

**Contacts**
- Create, edit and delete with emails, phones, addresses, birthday and notes.
- A group or folder membership.
- A change made on the provider appears in the cockpit.

**Security**
- `/mail/*` refuses a request without the cockpit session, with another identity's token, and with
  an expired token.
- The agent's tools and the browser sidecar cannot reach `/mail/*`.

**Gates**
- `internal/mailhost` ≥ 85% coverage; mutation ≥ 70% on the id map and the JMAP and remote
  translations.
- A JMAP conformance suite from MailCraft's stand-in contract.
- The patch's own tests pass in MailCraft's test suite.
- G1–G9 recorded.
