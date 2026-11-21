# Mail, calendar and contacts in the cockpit through MailCraft

Date: 2026-11-21. The operator's direction: *"Potremmo riusare la Ui di mail craft?"*, then *"Io
integrerei anche i calendari alla fine sono solo delle API da esporre la UI è già fatta"*, then *"Fai il
pull del PIM e guarda come fare tutto senza sì e senza ma"*.

Evidence:
- `spikes/mailcraft-web/FINDINGS.md`: verdicts 1–9, MailCraft v0.14.0's official web build framed under
  `internal/staticapp`, its JMAP calls logged.
- `aura-pim-mcp` read at `bb87b828` (latest `main`, pulled 2026-11-21).
- MailCraft's source read at the v0.14.0 tag.

## What changes

1. **The cockpit gets a mail, calendar and contacts interface: MailCraft's web build**, served at
   `/apps/mailcraft/` like the Craft apps. It shows the identity's own accounts, the ones already
   connected in the PIM, so there is no second sign-in.
2. **Mail travels over JMAP.** Aura answers RFC 8620/8621 on its own origin, `/mail/jmap/`, from
   `aura-pim-mcp`. MailCraft's mail path is used as published (FINDINGS 3–5); this is Aura's only
   adapter for mail.
3. **Calendars and contacts travel over MailCraft's own sync contract.** MailCraft's browser build
   syncs no calendar at all: its Graph, subscribed-calendar and free/busy runs are native-only
   (`crates/host/src/calendar.rs`, `#[cfg(not(target_arch = "wasm32"))]`), and it has no JMAP
   Calendars client. Its engine, though, exchanges calendars with its host through
   provider-neutral, serde types:
   - up: `model::remote::EventsUpload`, `CalendarAsk`;
   - down: `EventsDownload`, `EventsUploaded`, `CalendarsDownload`, `ContactsDownload`.

   A patch (`docker/craft-web/mailcraft.patch`) lets the page supply that host, as upstream already
   lets it supply JMAP credentials (`window.mailcraftJmap`). Aura answers it at `/mail/calendar` from
   the PIM.
4. **The PIM learns what the interface needs**, in our fork `chetto1983/aura-pim-mcp`:
   - raw messages;
   - flags;
   - paging;
   - sending a raw MIME message;
   - stable ids;
   - richer event writes.

## Decisions

### D1. Two transports: JMAP for mail, MailCraft's sync contract for calendars and contacts

- **Why JMAP for mail.**
  - It is a published standard, and MailCraft's client for it is strict and already in the build we
    serve.
  - The spike measured the whole surface the interface uses: session, `Mailbox/get`, `Email/query`,
    `Email/get`, a download of the message as `message/rfc822`, `Email/set` patches, an upload,
    `Email/import`, `Identity/get` and `EmailSubmission/set`.
  - MailCraft's own stand-in (`crates/jmap/src/standin.rs`) states that contract.
- **Why not JMAP for calendars.** MailCraft has no client for JMAP Calendars, and writing one is
  upstream-sized work.
- **Why not CalDAV.** It is on MailCraft's roadmap (item 9; `crates/dav` has discovery only, since
  2026-11-15) and desktop-first, with no date for the browser.
- **Why not emulate Microsoft Graph.** Graph's mail contract (`GraphMailRequest`) carries folder
  links, drafts, inbox passes and Favorites that the PIM does not model.
- **What the remote contract gives instead.** It is what MailCraft's calendar code already speaks:
  - two-way events (create, update, delete, respond, forward);
  - series and their exceptions, attendees, show-as, private, reminders, categories and time zones;
  - calendars and contacts.

  Its types derive `Serialize`/`Deserialize` with `camelCase`, so the page carries them as JSON
  unchanged.

### D2. The MailCraft patch: one host hook for calendars and contacts

`docker/craft-web/mailcraft.patch`, built by `docker/craft-web/build.sh` like PhotoCraft's.

- **`apps/mailcraft-web`.**
  - A `calendar.rs` beside `jmap.rs` implements `Services::graph` for the accounts the page names in
    `window.mailcraftHost.calendars`.
  - It serialises `{account, changes: EventsUpload, asks: [CalendarAsk], calendar: [from, to],
    directory}` and POSTs it to the page-given address with the page's bearer token
    (`window.mailcraftJmap.secret`).
  - It deserialises `{events, moreEvents, uploaded, calendars, contacts, errors}` into
    `GraphResult`.
- **`crates/ui-egui/src/calendar_sync.rs`.** `calendar_job` today requires an OAuth sign-in (an
  `oauth` and a refresh or access token). It runs instead when the account is one the host named, and
  hands the hook nothing secret.
- **Size.** Expected in the range of the PhotoCraft patch (about 200 changed lines).
- **Upstream.** Offered to UCS-Care-Group as a generalisation of their `mailcraftJmap` hook. If they
  take it, the patch goes, as WordCraft's and PdfCraft's did.
- **Brand terms.** MailCraft is UCS-Care-Group's, not ArtCraft's; its brand terms (if any) are read
  before the patched build ships.

### D3. Aura's endpoints, per identity, for the cockpit only

A new package, `internal/mailhost` (files ≤ 600 LOC each), mounted by `aura serve` beside the other
cockpit routes:

- **`GET /mail/config.js`.** Declares `window.mailcraftJmap` and `window.mailcraftHost`:
  - the identity's accounts, from the PIM's `list_accounts`;
  - the session address `/mail/jmap/session`;
  - the calendar address `/mail/calendar`;
  - `secret()` returning a short-lived bearer from `/mail/token`.

  `index.html` loads it before the module script; the build is otherwise MailCraft's.
- **`POST /mail/token`.** Mints a bearer bound to the cockpit session and the identity:
  - minutes long, with an audience of `/mail/*`;
  - only for a session cookie with the cockpit's CSRF check;
  - never for an agent's or a sidecar's credential. The agent cannot reach `/mail/*`.
- **`/mail/jmap/*`.** The JMAP surface of FINDINGS 3–5, answered from the PIM:

| JMAP | PIM |
|---|---|
| session, `Identity/get` | `list_accounts` (with its sending address, `c127b8d`) |
| `Mailbox/get` | the six folder aliases `get_emails` takes; counts from a new `folder_counts` (D4) |
| `Email/query` (`inMailbox`, receivedAt desc, `position`/`limit`, `calculateTotal`) | `get_emails` with `folder` and the new paging (D4) |
| `Email/get` (ids, keywords, mailboxIds, headers) | the cached listing; headers from the raw message |
| download `message/rfc822` | the new `get_email_raw` (D4) |
| `Email/set` `keywords/$seen` | `mark_email_read` |
| `Email/set` `keywords/$flagged` | the new `set_email_flag` (D4) |
| `Email/set` `mailboxIds/<from\|to>` | `move_email` |
| `Email/set` destroy | `delete_email` |
| upload, `Email/import` into Sent, `EmailSubmission/set` | the new `send_email_raw` (D4) with the uploaded MIME; the Sent copy is the provider's own |

- **`POST /mail/calendar`.** The remote contract of D1, answered from the PIM:

| Contract | PIM |
|---|---|
| `calendars` | `list_calendars` |
| `events`, `moreEvents` for the asked stretch | `get_calendar_events` per calendar, with series masters and exceptions (D4) |
| `changes` Create / Update / Delete | `create_event` / `update_event` / `delete_event`, with the fields D4 adds |
| `changes` Respond | `respond_to_event` |
| `changes` Forward, proposals | refused per change, with a reason MailCraft shows: the PIM has neither |
| `contacts` | `get_contacts`. Read-only, which is how MailCraft treats a Graph account's contacts already. |

- **State and ids.** Migration `<next>_mailhost.up.sql` (number from
  `ls internal/db/migrations | tail -1` at landing):
  - one table maps the PIM's provider ids to stable JMAP ids per identity and account;
  - JMAP `state` strings come from the PIM's change cursor (`/admin/changes`, already used by
    `pimwatch`), so a poll that finds nothing new costs one cursor read, not a provider listing.
- **Who acts.**
  - What the owner does in this interface is the owner's own act. Sending, deleting and moving here
    are not agent actions, and do not go through the approvals `messagedrafts` gives the agent's
    sends; the interface's own confirmations apply.
  - Every write is still logged to `audit` with the identity, the account and the action, never the
    content.

### D4. The PIM fork's additions

In `chetto1983/aura-pim-mcp`, each one a provider method plus the MCP action and its `/admin` REST
twin:

| Addition | Microsoft Graph | Gmail / Google Calendar | IMAP / CalDAV |
|---|---|---|---|
| `get_email_raw` | `GET /messages/{id}/$value` | `messages.get format=raw` | `FETCH BODY.PEEK[]` |
| `send_email_raw` (MIME, Bcc kept, threading kept) | `POST /sendMail` with the MIME | `messages.send raw` | SMTP `DATA` |
| `set_email_flag` | `flag.flagStatus` | `STARRED` label | `\Flagged` |
| paging on `get_emails` (`offset` or `pageToken`, `total`) | `$skip`/`$count` | `pageToken`, `resultSizeEstimate` | `UID SEARCH` ranges |
| `folder_counts` | folder `totalItemCount`/`unreadItemCount` | label `messagesTotal`/`messagesUnread` | `STATUS` |
| stable ids | `Prefer: IdType="ImmutableId"` | ids are stable | the facade's map (D3) |
| event writes: recurrence (RRULE), reminder, show-as, private, categories, body on update | `patternedRecurrence` and fields | `recurrence`, `reminders`, `transparency`, `visibility` | `RRULE`, `VALARM`, `TRANSP`, `CLASS` |
| event reads: series master, exception, original start, change key, reminder | `seriesMasterId`, `type`, `changeKey` | `recurringEventId`, `originalStartTime`, `etag` | `RECURRENCE-ID`, `ETag` |

- **Before any is written**, each row's API page is read and cited in the code (CLAUDE.md, *read the
  documentation first*). The table names the mechanism, not a verified call.
- **Risk table.** New actions are classified in `internal/agent/mcptools/bridge_risk.go`:
  - `get_email_raw` and `folder_counts`: Read;
  - `set_email_flag`: Mutate;
  - `send_email_raw`: Destructive (the agent keeps going through approval).

### D5. Packaging and the cockpit

- **Build.** `docker/craft-web/build.sh` takes the repository's owner as well as the app, since
  MailCraft is `UCS-Care-Group/mailcraft`, not storytold's. The Craft web image gains MailCraft,
  pinned by tag and commit with its patch; `publish-craft-web.yml` and the pin test follow as for the
  others. `craft_osv.sh` and its test learn the new pin.
- **Cockpit.** A *Posta* entry in the cockpit's navigation frames `/apps/mailcraft/` full-height.
  MailCraft's own window replaces nothing else. The agent's mail and calendar remain the PIM's
  through MCP, unchanged.
- **Language.** The interface follows the browser (FINDINGS 1: Italian measured).

## What this does not do

- **No new provider.** Accounts are the PIM's: Google, Microsoft 365, Outlook.com and IMAP/SMTP.
- **Not offline.** MailCraft keeps its mailbox in the browser's storage, but every change goes to
  the server through Aura.
- **No MailCraft agent or MCP in the cockpit.** The agent keeps the PIM's tools.
- **Contacts are read-only**, as MailCraft already treats a Graph account's.
- **No forwarding of meetings and no proposals** of new times: refused with a reason until the PIM
  has them.
- **Not in Telegram or WhatsApp.**

## Acceptance

- **Before Gate 1, measured live** (`docs/superpowers/verification/`): on one Gmail and one
  Microsoft 365 account through the sidecar, what `get_email_details`, `get_calendar_events` (series,
  time zones) and `/admin/changes` return today. Any row of D3/D4 the measure contradicts is amended
  first.
- **Mail.** In the cockpit, on both accounts:
  - list, read, mark read and unread, flag, archive and delete, each checked on the provider;
  - reply keeps the thread (`In-Reply-To`, `References`), checked on the provider;
  - Bcc reaches the Bcc recipient and appears in no header.
- **Calendar.**
  - Create, move, change and delete a single event and a weekly series, plus one exception, each
    checked on the provider.
  - Answer an invitation.
  - Forward is refused with its reason.
- **Contacts.** The provider's contacts show, and recipients are suggested from them.
- **Security.**
  - `/mail/*` refuses a request without the cockpit session, with another identity's token, and
    with an expired one.
  - The agent's tools cannot reach `/mail/*`.
- **Gates.**
  - `internal/mailhost` at ≥ 85% coverage, mutation ≥ 70% on the id map and the JMAP patch
    translation;
  - a JMAP conformance suite run against MailCraft's own stand-in expectations;
  - the E2E above recorded in a dated verification file.
