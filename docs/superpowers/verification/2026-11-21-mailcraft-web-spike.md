# MailCraft's web build as the cockpit's mail interface: spike

Date: 2026-11-21. The operator asked whether MailCraft's interface could be reused ("Potremmo
riusare la Ui di mail craft?"), then for the spike ("Sì, procedi con lo spike"). Run in a cloud dev
container with headless Chromium (SwiftShader), `it-IT`. Harness and full findings:
`spikes/mailcraft-web/` (`README.md`, `FINDINGS.md`).

**Verdict: PARTIAL.** The interface half is VALIDATED. MailCraft's official web build (v0.14.0,
unmodified but for one script tag) runs framed under the cockpit's page policy. It takes the
account and its token from the page through `window.mailcraftJmap`, so no second sign-in is needed,
and it lists, reads, marks, flags, archives and sends over a small, measured set of JMAP calls. The
data half is not measured live. A facade that answers that JMAP from `aura-pim-mcp` needs what the
PIM does not give today: the raw message, flags, paging, Bcc and reply threading.

## What was measured

- **Framing.** `internal/staticapp` served the build under `/apps/mailcraft/` and a same-origin page
  framed it. Its interface drew in Italian. The console logged no policy violation and one 404 the
  driver did not attribute.
- **The hook.** MailCraft never asked for a credential. It fetched the token from the page, then
  the JMAP session with it as Bearer.
- **The calls.** The spike's own JMAP server logged every call:
  - **listing**: `Mailbox/get`, then per mailbox `Email/query` (inMailbox, receivedAt desc,
    limit 500, calculateTotal) with an `Email/get` by back-reference;
  - **reading**: the message downloaded whole as `message/rfc822`;
  - **changes**: `Email/set` patches `keywords/$seen`, `keywords/$flagged` and
    `mailboxIds/<from|to>`;
  - **sending**: `Identity/get`, an upload of the MIME, `Email/import` into Sent with `$draft`,
    then `EmailSubmission/set` with an explicit envelope and `onSuccessUpdateEmail`.
  - MailCraft polls; it used no `*/changes` and no push.
- **The PIM.** Read from `chetto1983/aura-pim-mcp` at `bb87b828` and from Aura:
  - `get_emails` takes the six standard folders as aliases;
  - `mark_email_read`, `move_email`, `delete_email`, `get_email_details`, `get_email_attachment`
    and `list_accounts` exist and are classified in `bridge_risk.go`;
  - no raw message, no flags, no paging (count only), no folder list beyond the aliases;
  - ids change on a move on Microsoft Graph and IMAP;
  - `send_email` has no Bcc and no In-Reply-To or References.

## What this does not establish

- **No real JMAP server.** Stalwart v1.1 needs its interactive setup, the legacy image could not be
  pulled (Docker Hub 429, GHCR denied), and MailCraft's strict stand-in cannot be served from outside
  its crate. The spike's server is its own, from RFC 8620/8621. Two of its bugs, not MailCraft's,
  were found and fixed on the way.
- **No call reached the PIM.** The gaps are read from code, not measured on a live sidecar with
  Gmail or Microsoft 365.
- **No aura stack ran.** There was no cockpit routing, session token or https. The scale was six
  mailboxes and three messages.
- **Not measured:** Delete (documented upstream as a move to Deleted Items), how MailCraft shows a
  submission still `pending` an approval, and the calendar, which in MailCraft's browser build is
  local only.
