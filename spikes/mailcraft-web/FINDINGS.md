# Spike: MailCraft's web build as the cockpit's mail interface — findings

Date: 2026-11-21. Cloud dev container, headless Chromium with SwiftShader WebGL2, `it-IT`. MailCraft
v0.14.0 (official web zip, SHA-256 `b441f29d…887a` as its release publishes); `aura-pim-mcp` read at
`bb87b828`.

## Verdicts

| # | Finding | Verdict |
|---|---|---|
| 1 | **The official build runs framed under the cockpit's policy.** Served by `internal/staticapp` under `/apps/mailcraft/` (CSP `default-src 'none'; script-src 'self' 'wasm-unsafe-eval' 'sha256-…'; connect-src 'self'`), framed by a same-origin page, it drew its interface in Italian (Posta in arrivo, Bozze, Posta inviata…) with no policy violation in the console. The wasm is 41.7 MB, 13.9 MB gzipped. One line added to `index.html` loads the config script; the build is otherwise unmodified. | VALIDATED |
| 2 | **`window.mailcraftJmap` works from the page.** The account it names (`Aura`) appears in the folder pane; MailCraft never asked for a credential: it fetched the token from the page (`GET /spike/token`), then the session with it as `Bearer`. A plain-`http` session address is accepted on loopback only (`crates/jmap/src/session.rs`), so the cockpit serves it over https as it does today. | VALIDATED |
| 3 | **Listing and reading.** One `Mailbox/get` (properties `id, name, parentId, role, totalEmails, unreadEmails`); then per mailbox an `Email/query` (`filter.inMailbox`, `sort: receivedAt desc`, `limit: 500`, `calculateTotal: true`) with an `Email/get` of `id, keywords, mailboxIds` by back-reference (`#ids` → `/ids`), then an `Email/get` of the headers. A message's body is **not** read from `bodyValues`: MailCraft downloads the whole message as `message/rfc822` (`GET /jmap/download/A1/<blobId>/message.eml?type=message%2Frfc822`) for the Inbox's newest messages, and shows it from that. | VALIDATED |
| 4 | **Changes go up as `Email/set` patches.** Opening a message: `{"update":{"M2":{"keywords/$seen":true}}}`. Archive: `{"M2":{"mailboxIds/archive":true,"mailboxIds/inbox":null}}`. Flag (Insert, as in Outlook): `{"M1":{"keywords/$flagged":true}}`. The Delete key, after a click on the row, sent nothing in 15 s; upstream documents Delete as a move into Deleted Items (`docs/jmap.md`), the same patch as Archive. | VALIDATED (delete: documented, not measured) |
| 5 | **Sending.** New message, To, Subject, body, Invia: `Identity/get` (`id, email`); the message uploaded as `message/rfc822` (689 B); `Email/import` into `sent` with `$draft` and `$seen`; `EmailSubmission/set` with `identityId`, `emailId: "#m"` and an explicit envelope (`mailFrom` the account, `rcptTo` the recipient), `onSuccessUpdateEmail` clearing `$draft`. The Outbox emptied, no error shown. | VALIDATED |
| 6 | **MailCraft polls; it uses no `*/changes`, no push.** In 25 s of a first start it made 6 `Email/query` and 8 `Email/get` (one per mailbox, then the Inbox's headers), and it repeats a Send/Receive every few minutes (Inbox and the folder on screen). Every poll is a provider round trip unless the facade caches. | VALIDATED (counts from the log) |
| 7 | **What the PIM answers as it is.** `get_emails` takes a `folder` alias that matches MailCraft's six standard mailboxes (`inbox`, `drafts`, `sentitems`, `archive`, `trash`, `spam`) and returns `id, accountId, subject, from, receivedDateTime, isRead, hasAttachments`; `mark_email_read`, `move_email` (to a folder alias or id) and `delete_email` exist and are already classified in `bridge_risk.go` (the fork restored them after the 2026-06-16 trim); `get_email_details` gives the body (text or HTML), `to`, `cc` and attachments; `get_email_attachment` their bytes; `list_accounts` the identity's accounts. | VALIDATED (read from the fork and Aura) |
| 8 | **What it does not.** (a) **No raw message**: MailCraft reads bodies from the `message/rfc822` blob, and the PIM has no raw-MIME read; a facade would build an RFC 5322 message from `get_email_details` (headers it lacks: `Message-ID`, `References`, `Date` as sent). (b) **No flags**: no `$flagged` read or write. (c) **No paging**: `get_emails` has `count` only (default 20), no offset or total; MailCraft asks for 500 per mailbox with `calculateTotal`. (d) **No folder list** beyond the aliases: custom folders and labels cannot be listed. (e) **Unstable ids**: `move_email` returns a new id on Microsoft Graph and IMAP; JMAP ids must not change, so the facade must map them. (f) **Sending is lossy**: `send_email` takes `to` (one string), `cc`, subject, body and attachments, with no `bcc` and no `In-Reply-To`/`References`, so replies lose their threading and Bcc recipients would be dropped; the facade would parse the uploaded MIME into those fields. (g) **Approval**: `send_email` is Destructive and goes through Aura's approval; `EmailSubmission/set` would have to answer before the owner approves (`undoStatus: pending`, or the message held in the Outbox with a reason, which MailCraft already shows). How MailCraft handles `pending` was not measured. | VALIDATED (gaps read from the code) |
| 9 | **The calendar stays out.** MailCraft's browser build syncs mail over JMAP only; its calendar and contacts there are local. The cockpit would gain a mail interface, not a calendar. | VALIDATED (upstream docs) |

## What this spike does not show

- **Not against a real JMAP server.** The server is the spike's own, written from the RFCs; MailCraft
  accepted its answers once two of its own bugs were fixed (an empty list sent as `null`, a
  back-reference read before its result was in JSON form). A facade must meet MailCraft's strictness,
  which a stricter reference (Stalwart, or a patched stand-in) would test better.
- **Not over the PIM.** No call reached `aura-pim-mcp`; findings 7 and 8 are read from the fork's code,
  not measured on a live sidecar with a real Gmail or Microsoft 365 account.
- **Not in the running cockpit.** No aura stack ran; the cockpit's own routing, session token and https
  were not exercised.
- **Not at scale.** Six mailboxes, three messages. Memory and time with thousands of messages in a
  browser tab, and the provider load of the polling, were not measured.
- **One locale, one run per action.** Delete and the `pending` submission were not measured.
