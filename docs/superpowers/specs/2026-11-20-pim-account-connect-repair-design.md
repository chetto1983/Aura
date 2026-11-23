# Connecting and repairing a mail account: an address and a password, checked before it is saved

**Status:** draft, written 2026-11-20 from an operator conversation. Target user: a small office with one
administrator. Reference experience: MailCraft (mailcraft.dev), which asks for an address and a password, finds the
servers itself and tests the login before it keeps anything. The operator chose approach A ("a", 2026-11-20): the
discovery and the check both live in the PIM (`aura-pim-mcp`), and Aura only relays them. Builder-checked once; the
operator answered its three open questions (watch stays manual, a real mailbox of theirs for the VM run, password
encryption in this work). **Approved by the operator on 2026-11-20 ("pk").**

## Why

Measured on the code on 2026-11-20 (Aura `2a132e539`, PIM `bb87b82`):

1. **Connecting an IMAP mailbox is an administrator's form.** It lives in Governance → MCP server detail →
   `CalendarConnect` (`web/src/governance/McpServerDetail.tsx:87`). The `imap` provider asks for six fields
   (`imapHost`, `imapPort`, `smtpHost`, `smtpPort`, `username`, `password`, `web/src/governance/pimProviders.ts:96-120`).
   A person in a small office knows their address and their password, not their provider's SMTP port.
2. **Nothing is checked when it is saved.** The PIM's `POST /admin/accounts` (`CreateAccount`,
   `src/CalendarMcp.HttpServer/Admin/AdminEndpoints.cs:26`) stores the configuration without opening a connection. A
   wrong password, a host typo or a provider that wants an app password shows up later, in the mail watch, as
   `provider_auth` or `provider_transient`, far from the form that caused it.
3. **A broken IMAP account cannot be repaired.** `PimAccountRow` offers «Ricollega» / «Completa l'accesso» only when
   `pimAccountFlow(provider) !== 'none'`, so an IMAP row has only «Rimuovi». Changing a password means removing the
   account and adding it again, and removing it deletes the watch's decisions and learning
   (`project_relink_wipes_watch_history`, fixed for OAuth accounts on 2026-11-03, never for IMAP). The PIM already has
   `PUT /admin/accounts/{id}` (`UpdateAccount`, provider immutable); Aura does not relay it.
4. **A long "temporary" failure never asks for anything.** A field report (2026-11-20): a Yahoo account shown as
   "the provider has a problem" since 14 November. `ProviderFailures.Classify` maps `ImapCommandException`,
   `ServiceNotAuthenticatedException` and the socket family to `ProviderTransient`; the cockpit then waits forever.
   Which exception that account actually throws is **not measured** (no access to that install's logs). This spec does
   not guess it; it makes the next occurrence measurable (§ Repair) and gives the person a way out regardless of the
   class.

## Goals

1. **Address and password.** A person types an address and a password and presses «Collega». The servers are found
   for them. The account is saved only if the login works.
2. **A failure names what to fix.** Wrong password, an app password needed (with the provider's own page linked),
   server unreachable, TLS refused, mailbox refused, sending refused. Each in words a non-technical person can act on.
3. **Repair without loss.** A broken account offers «Prova di nuovo» and «Cambia password» on the same account id, so
   the watch keeps its cursor, its decisions and its learning.
4. **Each person connects their own mail, from their own settings.** The administrator keeps the provider OAuth apps
   (Google, Microsoft) in Governance; mailbox connection moves to Settings, for every identity.

## Non-goals

- **No new providers.** The cockpit keeps offering Google, Microsoft 365, Outlook.com, IMAP, ICS and JSON
  (`AccountValidation.KnownProviders` also holds `dav`/`caldav`/`carddav`, which the cockpit does not expose). Exchange/EWS stays deferred (`project_pim_upstream_adjutant_review`).
- **No change to OAuth flows.** Google consent and the Microsoft device code keep their panels; discovery only routes
  an address to them.
- **No calendar-only providers in the new section's first screen.** ICS and JSON stay reachable from «Altro tipo di
  account», with today's fields.
- **No automatic retry policy change in the watch.** The watch's ladder and alarms are untouched; only what the cockpit
  shows for an old transient failure changes.

## Anti-goals

- **No mail content leaves for discovery.** Only the address's domain (and the MX host's domain) is sent to the
  Thunderbird ISPDB; the local part never is. The preset table answers the common providers without any request.
- **No password stored on a failed check.** A configuration that fails verification is not written, not even as a
  draft.
- **No bespoke autoconfig client in Aura.** Discovery is one PIM endpoint; Aura relays its JSON.

## Part 1: discovery (PIM)

`POST /admin/discover` with `{ "email": "<address>" }` answers one of:

```json
{ "kind": "google" }
{ "kind": "microsoft365" }        // or "outlook.com"
{ "kind": "imap", "source": "preset|ispdb|ispdb-mx",
  "imapHost": "...", "imapPort": 993, "smtpHost": "...", "smtpPort": 465,
  "username": "<address or local part, per the source>",
  "appPassword": { "required": true, "helpUrl": "https://..." } }
{ "kind": "unknown" }
```

Order, first hit wins:

1. **Preset table**, a small JSON resource in the PIM: gmail.com/googlemail.com → `google`; outlook.com/hotmail.*/
   live.* → `outlook.com`; yahoo.*, icloud.com/me.com, libero.it, virgilio.it, tiscali.it, aruba.it/pec.it,
   fastwebnet.it, tim.it, alice.it → `imap` with hosts, ports and, where the provider requires it, the app-password
   flag and help URL. Each entry cites the provider page it was copied from (READ THE DOCUMENTATION FIRST).
2. **ISPDB by domain**: `GET https://autoconfig.thunderbird.net/v1.1/<domain>`, parse `incomingServer type="imap"`
   and `outgoingServer type="smtp"`, substitute `%EMAILADDRESS%` / `%EMAILLOCALPART%`, take the `documentation url`
   as the help link.
3. **MX lookup**, then ISPDB of the MX's registrable domain (a custom domain hosted at a known provider: MX
   `*.google.com` → `google`; `*.outlook.com` / `*.protection.outlook.com` → `microsoft365`; otherwise ISPDB).
4. `unknown`: the form opens «Impostazioni server» with the address as username.

Timeouts: 5 s per remote step, 12 s overall; a timeout falls through to the next step, never to an error. The ISPDB
answer is cached per domain for 24 h in memory.

## Part 2: verification on save (PIM)

For `provider = imap`, `CreateAccount` and the `provider-config` PATCH (below) verify before they write; upstream
`PUT` is not verified and the cockpit never calls it:

1. IMAP connect + authenticate (today's `OpenImapAsync`: `SecureSocketOptions.Auto`), open INBOX read-only.
2. SMTP connect + authenticate when the server advertises AUTH (today's `OpenSmtpAsync`).
3. Disconnect both. 20 s overall.

On failure the endpoint writes nothing and answers `422`:

```json
{ "error": "verify_failed", "step": "imap|smtp", "reason": "auth_failed|unreachable|tls_failed|mailbox_refused|smtp_auth_failed|smtp_unreachable", "detail": "<server's text, trimmed>" }
```

`reason` comes from the exception type (MailKit `AuthenticationException`, `SocketException` / timeout,
`SslHandshakeException`, `ImapCommandException` on SELECT, the same on the SMTP side). `detail` carries the server's
own response line, which is the evidence the cockpit and the log need. `auth_failed` on a domain whose preset says `appPassword.required` is shown as «Serve una password per app».

`POST /admin/accounts/{id}/verify` runs the same check on a stored account and answers `200 {"ok":true}` or the same
`422`. It also logs the exception type and the server line, so the next "provider has a problem since …" is measured,
not guessed.

**`detail` stays in the PIM log.** The browser gets `step` and `reason` only. Every identity can name any host and
port, and the PIM sits on the compose network next to Postgres and ArcadeDB: echoing a server's reply would turn the
form into a banner probe. Aura already drops PIM error text for the same reason (`internal/pimwatch/client.go:260-283`).
The «Serve una password per app» wording therefore comes from the preset's flag, not from the server's text.

**Changing a stored account: a merge, not a replace.** Upstream `PUT /admin/accounts/{id}` replaces the whole account
(`DisplayName` required, omitted `Domains`/`Enabled`/`Priority` reset, `providerConfig` re-validated in full), and
`GET /admin/accounts` never returns `providerConfig`, so the cockpit cannot resend it. The fork adds
`PATCH /admin/accounts/{id}/provider-config`: the body's non-empty keys overwrite the stored ones, the others stay,
the **merged** config is verified (for `imap`) and written only if it passes. Upstream `PUT` is left as it is. After a
successful write the pooled IMAP connection for that account id is evicted, so the next call uses the new config.

**Placement.** `ImapProviderService.cs` is 598 LOC. `OpenImapAsync` / `OpenSmtpAsync` move to a new
`ImapConnections.cs` that both the service and the new `ImapAccountVerifier.cs` call; the service shrinks. Discovery
is `Admin/DiscoverEndpoint.cs` + `Discovery/` (preset resource, ISPDB client, MX). `AURA-FORK.md` records the three
new endpoints (`discover`, `verify`, `provider-config`). Non-IMAP providers are not verified here (OAuth verifies itself at consent).

## Part 2b: the IMAP password encrypted at rest (PIM)

The operator's call, 2026-11-20: in this work, because it touches exactly the code that writes the password. Today
`/admin` create and update store `providerConfig.password` in plaintext; `PasswordProtector`
(`src/CalendarMcp.Core/Security/PasswordProtector.cs`, ASP.NET DataProtection, `ENC:` prefix) is registered in DI but
only the CLI and `DavProviderService` call it.

- Create and the `provider-config` PATCH store `Protect(password)`; `OpenImapAsync` / `OpenSmtpAsync` read through
  `Unprotect`, which returns an unprefixed value unchanged, so existing plaintext accounts keep working.
- Existing accounts are re-written encrypted once at startup (an idempotent pass over `imap` accounts whose password
  lacks `ENC:`), logged as a count, never as values.
- **The key ring must outlive the container.** The builder reads where `DataProtectionExtensions` persists it and
  confirms it sits on a volume `compose.yaml` keeps; if not, the volume is added in this work. A lost key ring makes
  every stored password unreadable: `CryptographicException` is classified `provider_auth`, so the rows ask for
  «Cambia password», which is the right remedy.
- Backups: the key ring travels with the PIM's data volume; a restore without it means re-entering passwords. The
  verification file states this.

## Part 3: Aura relay

`internal/agui/connect_pim_api.go` (367 LOC) gains three routes, same mount, same `RequireCapability(governance.write)`
that every identity holds (`internal/identity/capabilities.go:46`):

- `POST /api/connect/pim/discover` → PIM `POST /admin/discover`.
- `PATCH /api/connect/pim/accounts/{id}/provider-config` → PIM `PATCH /admin/accounts/{id}/provider-config`.
- `POST /api/connect/pim/accounts/{id}/verify` → PIM `POST /admin/accounts/{id}/verify`.

A PIM `422` passes through with its status and its `step`/`reason`. These routes need their own client timeout:
today's `pimClientTimeout` is 8 s, shorter than discovery (12 s) and verification (20 s). The handlers go in a new
`connect_pim_account_api.go`.

**A successful repair wakes the watch.** When `verify` or `provider-config` succeeds on a watched account, the handler
resumes its task if the scheduler paused it for `failures` (`internal/cron/pause.go`) and runs one tick now, the way
`handlePIMDeleteAccount` already calls into the mail watch. The row then clears on that tick instead of at the next
interval.

## Part 4: cockpit

**Where.** A new Settings section «Posta e calendario» in the *personal* group (`settingsSections.ts`, not
`adminOnly`). It lists the identity's accounts (today's `PimAccountRow`, mail-watch status included) and the
«Aggiungi account» flow. Governance → MCP server detail keeps `PimProviderAppsPanel` (the administrator's OAuth apps)
and links to the new section instead of embedding `CalendarConnect`.

**Add.** One screen: «Indirizzo email», «Password», «Collega».
- On blur of the address, `discover` runs. `google` / `microsoft365` / `outlook.com` replace the password field with
  «Accedi con Google/Microsoft» (today's panels); if the administrator has not configured that provider app, the
  screen says so and offers IMAP with an app password where the provider allows it.
- `imap` keeps the password field; if `appPassword.required`, a one-line notice with the provider's help link sits
  under it before the person even tries.
- «Impostazioni server» is a closed disclosure holding today's six fields, pre-filled by discovery, open by itself
  only on `unknown`.
- The account id is generated from the address (today the person types one); a collision gets a numeric suffix.
- Connecting does **not** turn the mail watch on (operator's call, 2026-11-20: it spends decision-model calls). The new
  row offers today's switch.
- «Collega» shows progress («Verifico l'accesso…»), then either the new row or the `422`'s reason as an inline message
  on the field it concerns (password, or the server disclosure, opened).
- Accessible name and error wiring per `feedback_aria_invalid_omit_when_valid`.

**Repair.** An IMAP row gains «Prova di nuovo» (verify) and «Cambia password» (an inline password field, plus the
closed «Impostazioni server» disclosure whose blank fields mean "unchanged"; sent as `provider-config` PATCH). A change
of server or port asks for the password again (amendment A3). OAuth
rows keep «Ricollega». «Rimuovi» stays, with today's warning.

**An old temporary failure.** On an **IMAP row** whose watch reports `provider_transient` for more than 6 h, the row
stops saying "the provider has a problem" and says «Non riesco a leggere la posta da <giorno e ora>» (from
`failingSince`), with «Prova di nuovo» and «Cambia password». The PIM's own failures (`pim_http_5xx`,
`pim_call_failed`, `pim_bad_response`) and OAuth rows keep today's wording: a password is not their remedy. 6 h is
the author's call: long enough to outlast a provider outage, short enough to reach a person the same working day.
Separately, the decision-model failure clock (`AccountWatch.tsx:255-257`) shows a time without a day; fixed on touch.

Strings go to `resources.governance.ts` / a new `resources.settings.mail.ts`, Italian and English.

## Amendments after the plan review (2026-11-20)

Three reviewers read the plan against the code, the libraries and two reference clients (Thunderbird, Thunderbird for
Android); their reports are in `docs/superpowers/plans/validation/2026-11-20-pim-account-connect-repair-*.md`. The
operator decided A1, A2, A4 and A5 the same day; the rest follow from the reports. Where these contradict the text
above, these win.

- **A1. Settings opens to members** (operator). The shell stops treating Settings as an admin-only surface; a member
  sees the personal sections (Profile, «Posta e calendario») and nothing admin-only.
- **A2. TLS is required for new and changed accounts** (operator). Every account written by create or PATCH carries
  `imapSecurity` / `smtpSecurity` (`ssl` or `starttls`, STARTTLS required, never "when available"); a write that asks
  for `none` is refused. An account stored before this keeps today's behaviour until someone changes it.
  `SecureSocketOptions.Auto` on 143/587 sends the login in clear when STARTTLS is absent or stripped (MailKit
  `ImapClient.cs#L1394-1398`); both reference clients refuse that.
- **A3. A change of server, port or username asks for the password; the username cannot change.** Otherwise a PATCH
  naming a new host sends the stored password there, even when the save is then refused. A different username is a
  different mailbox, and the watch's cursor and learning must not move to it: that is a new account.
- **A4. A sending failure does not block the save** (operator). IMAP must pass. An SMTP refusal of the password
  (`smtp_auth_failed`) still blocks; an unreachable or TLS-refusing SMTP server is stored with a warning, shown when the
  account is connected and on every «Prova di nuovo». The watch only reads.
- **A5. Private addresses are for the administrator** (operator). Verification connects only to public addresses for a
  member; an identity holding `identity.create` may name a host on a private network (an office mail server). The PIM
  resolves the host once, checks the address, and hands MailKit that socket, so DNS cannot swap the address in between.
  Verify, create and PATCH are also limited per identity (10 a minute) and per account (one check every 30 s), since a
  wrong password is several login attempts and providers lock accounts.
- **A6. Discovery follows Thunderbird's order.** Preset; the domain's own autoconfig
  (`https://autoconfig.<domain>/mail/config-v1.1.xml`, then `https://<domain>/.well-known/autoconfig/mail/config-v1.1.xml`,
  https only, no address sent, no redirects, private addresses refused); ISPDB; MX (stop when its base domain is the
  address's); preset, autoconfig and ISPDB for the MX's next subdomain and then its shorter suffixes; `unknown`. An
  answer is used only if a server offers SSL or STARTTLS and password login; OAuth-only or plain servers are a miss.
  `*.olc.protection.outlook.com` is Outlook.com, `*.mail.protection.outlook.com` is Microsoft 365. Settings found via MX
  are shown («Mi collegherò a …») before the password leaves.
- **A7. The app-password fallback is Google's only.** Outlook.com has refused basic authentication since 2024-09-16;
  for Microsoft without an OAuth app the screen says the administrator must configure it.
- **A8. «Prova di nuovo» checks what the watch does**: the configured inbox folder and the watch's first read step,
  not only the login. Both the verifier and the watch's failure path log the server's response line (CR/LF stripped,
  trimmed), which amends the fork's rule that a provider's message is never logged: without it the next field case
  stays unmeasured. When the login works, the row says «L'accesso funziona. Se la lettura non riprende al prossimo
  controllo, la password non è il problema.» and keeps its buttons, and the stale prompt yields for the failure that
  login vouched for (its `failing_since`). Amended 2026-11-23 (ledger Ruling 20): the cockpit cannot tell that reading
  *still* fails, because the scheduler advances `next_run_at` when it claims a run, before the read
  (`internal/cron/scheduler.go` `runOne`), and no per-resource read-finished time exists; saying so for certain would
  need a `checked_at` field (migration + API), outside this work.
- **A9. The relay does not leave a password's hash in Postgres.** The idempotency fingerprint of create and PATCH
  replaces secret fields with an HMAC under a key derived from the server secret, and `discover` is read-only.
- **A10. What encryption at rest protects.** The key ring is plaintext XML in `/app/data/keys`, on the same named
  volume as `appsettings.json` (measured: Dockerfile `CALENDAR_MCP_CONFIG=/app/data`, compose `aura-pim-data:/app/data`).
  `ENC:` protects against a leak of the config file alone, not of the volume. No Aura backup covering this volume was
  found. Never delete files in `keys/`. The startup pass writes atomically (temp file + rename), since the entrypoint
  re-seeds an empty file and a torn write would lose every tenant's accounts. Upstream `PUT` stores encrypted too,
  which restores what upstream's own admin did; it is still not verified.

**Also does not prove:** domains hosted at Aruba (MX under the customer's own domain) are not discovered, by the
reference clients either; the person fills in the server fields. A public hostname stored for a member that later
resolves to a private address is checked only when it is verified, not on every watch tick.

## Constraints

- Files ≤600 LOC, new code in new files on both sides; the PIM's `AURA-FORK.md` is the authority on fork differences.
- No mail content and no real addresses in fixtures, docs or the verification file (the repo is public).
- Tests: PIM xUnit with a GreenMail-style IMAP/SMTP fake for each `reason`; discovery tests against recorded ISPDB XML
  (no network in tests); Aura handler tests for pass-through of `422`; vitest for the add and repair flows.
- The VM is updated only through the updater; the operator clicks the update.

## Verification

On the lab VM, after a normal update, a dated file in `docs/superpowers/verification/`. The IMAP mailbox is one of
the operator's, at a real provider, with an app password; its address and credentials never enter the repo or the
verification file (operator's call, 2026-11-20).

0. After the update, the PIM's stored IMAP passwords carry `ENC:` and the existing accounts still read mail.

1. Add a Gmail address: discovery routes to Google sign-in.
2. Add an IMAP account with a wrong password: `422 auth_failed`, nothing stored (`GET /admin/accounts` unchanged).
3. Same account, right password: stored.
4. On the stored, watched account: «Cambia password» with a wrong password gets `422` and the stored config and the
   watch are unchanged; with the right one it succeeds, and the watch keeps its task, cursor and verdicts (same check
   as the 2026-11-03 relink file).
5. A domain absent from the preset table resolves through ISPDB; an unknown domain opens the server fields.
6. The password is changed on the provider's side, so the stored account starts failing; «Prova di nuovo» logs the
   exception type and server line; «Cambia password» with the new one resumes the watch on the spot.

**What this does not prove:** that the preset hosts stay right over time; that every Italian provider's app-password
rule is captured; the cause of the Yahoo field report, which stays unmeasured until that install runs «Prova di
nuovo».

## Left to the builder

- The exact preset entries, each with its cited provider page.
- MX resolution library on .NET (`DnsClient` or the platform resolver), after reading its docs.
- Whether the new Settings section reuses `CalendarConnectFields` for the disclosure or wraps it.
