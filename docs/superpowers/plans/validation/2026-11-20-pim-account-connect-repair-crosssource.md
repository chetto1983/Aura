# Plan validation: cross-source check of the PIM account connect + repair plan

- **Plan:** `docs/superpowers/plans/2026-11-20-pim-account-connect-repair.md`
- **Spec:** `docs/superpowers/specs/2026-11-20-pim-account-connect-repair-design.md`
- **Fork:** `D:/tmp/aura-pim-mcp` @ `bb87b82`
- **Date:** 2026-11-20. Read-only: no file other than this one was written. The spec, the plan and the code were not edited.

## Verdict: YELLOW

The architecture holds. Discovery and the check live in the PIM, the password is encrypted with DataProtection, and Aura relays the PIM. Upstream has nothing to reuse for either feature (see L2). Every library fact listed under "Verified as the plan states" checks out.

Ten findings (4 HIGH, 6 MEDIUM) need a plan change before execution. The four HIGH ones:

1. **H1:** the check sends the password in cleartext when a server on port 143 or 587 does not offer STARTTLS. Four of the plan's own preset providers publish 587/STARTTLS for password login (tim.it, alice.it, fastwebnet.it, icloud.com).
2. **H2:** discovery skips the provider's own autoconfig server, which both reference clients try before ISPDB, and never checks the MX domain against its own preset table. Measured: ISPDB has no entry for any of the eight Italian hosting domains probed. And for a domain hosted at Aruba, neither the plan nor either reference client finds anything, because its MX sits under the customer's own domain.
3. **H3:** the Outlook.com app-password fallback cannot work.
4. **H4:** ISPDB answers that offer only OAuth2 (`outlook.com`, `msn.com`) would come back as `kind: imap` with a password field, which is a guaranteed failure.

None of them needs a different architecture.

| Severity | Count |
|---|---|
| CRITICAL | 0 |
| HIGH | 4 |
| MEDIUM | 6 |
| LOW | 7 |

## Sources read (actual code and docs)

| Source | Revision / URL |
|---|---|
| Thunderbird desktop, `mail/components/accountcreation/modules/` | `mozilla/releases-comm-central` `master` @ `7d33bdaab2a46d4f1890120a5bced3fbe9bf0eea`, fetched with `gh api` |
| Thunderbird for Android, `feature/autodiscovery/`, `mail/protocols/{imap,smtp}` | `thunderbird/thunderbird-android` `main` @ `2d881297d9005af4d8035d382f078d07aaa0e079` |
| MailKit | tag `4.18.1` (`ce917126b2bd59ae7b0599b6ceae10c0773fcb22`). The repository has **no `4.18.2` tag**; the tags go 4.18.1, then 5.0.0. The package is not in the local NuGet cache, so 4.18.1 is the nearest source that can be read. |
| Upstream `MarimerLLC/calendar-mcp` | `main` @ `3b2762b7bddde041219e0ab2dfc821b2db08cf3c`: v2.0.0 (2026-11-09), renamed **Adjutant**, `src/Adjutant.*` |
| DnsClient.NET | `master` @ `f1e7ca33d713dc3bc70e7b3664aa3a2b6c090d5d`; latest NuGet stable is **1.8.0 (2024-07-01)** |
| Autoconfig format | `https://wiki.mozilla.org/Thunderbird:Autoconfiguration:ConfigFileFormat` (raw wikitext) |
| ASP.NET Core DataProtection | learn.microsoft.com `.../data-protection/configuration/default-settings`, `.../implementation/key-management`, `.../configuration/overview` |
| .NET `System.Net.Dns` | learn.microsoft.com `system.net.dns?view=net-10.0`, `system.net.dns.resolvemxasync` |
| Live measurements (domain names only) | ISPDB and `autoconfig.<domain>` for Italian hosting domains; ISPDB SMTP entries for nine providers, six of them on the preset list; ISPDB for `msn.com`, `outlook.com`, `gmail.com`, `yahoo.com`; MX and NS of `hotmail.com`, `msn.com`, `gmail.com`, `aruba.it`, `register.it`, and of a few Italian business domains (names withheld) |
| Aruba hosted-domain setup | Not Aruba's own page, which could not be read: the fibra.click forum thread `forum.fibra.click/d/34893-web-hosting-gestione-dns` and the aranzulla.it Aruba mail guide. Treat as a lead for the builder to confirm, not as evidence. |

---

## HIGH

### H1. `SecureSocketOptions.Auto` sends the password in cleartext when STARTTLS is absent, and discovery throws away the `socketType` that says STARTTLS is mandatory

**What the plan says.**

- P1 moves `OpenImapAsync` and `OpenSmtpAsync` into `ImapConnections` "with today's bodies unchanged". Those bodies call `ConnectAsync(host, port, SecureSocketOptions.Auto, ct)` (`ImapProviderService.cs:154`, `:170`).
- P4's `DiscoveryResult` carries host and port but **no socket type**.
- The verifier maps every exception that is not a known type to `unreachable`.

**What the reference does.**

- MailKit: `Auto` on any port other than 993 or 465 becomes `StartTlsWhenAvailable`.
  - [`ImapClient.cs#L1394-L1398`](https://github.com/jstedfast/MailKit/blob/4.18.1/MailKit/Net/Imap/ImapClient.cs#L1394-L1398): `case 993: options = SecureSocketOptions.SslOnConnect; break; default: options = SecureSocketOptions.StartTlsWhenAvailable; break;`
  - The SMTP equivalent is at [`SmtpClient.cs#L1291-L1295`](https://github.com/jstedfast/MailKit/blob/4.18.1/MailKit/Net/Smtp/SmtpClient.cs#L1291-L1295), with port 465.
  - The enum's own documentation, [`SecureSocketOptions.cs#L41-L45`](https://github.com/jstedfast/MailKit/blob/4.18.1/MailKit/Security/SecureSocketOptions.cs#L41-L45): "If the server does not support SSL or TLS, then the connection will continue without any encryption."
  - The upgrade runs only `if (starttls && (engine.Capabilities & ImapCapabilities.StartTLS) != 0)` (`ImapClient.cs#L1484`). Otherwise LOGIN/AUTH goes out in the clear. An on-path attacker who strips the `STARTTLS` capability gets the password.
- Autoconfig format: `"STARTTLS": on normal plain port and mandatory upgrade to TLS via STARTTLS` (wiki, raw line 33).
- Thunderbird maps it to `STARTTLS: Ci.nsMsgSocketType.alwaysSTARTTLS` ([`readFromXML.sys.mjs#L136`](https://github.com/mozilla/releases-comm-central/blob/7d33bdaab2a46d4f1890120a5bced3fbe9bf0eea/mail/components/accountcreation/modules/readFromXML.sys.mjs#L136)). It also shows an insecure-connection warning when the socket type is plain (`content/widgets/email-manual-incoming-form.mjs`, "If connection security chosen is none, show insecure connection warning.").
- Thunderbird for Android:
  - Its parser accepts only `"SSL" -> TLS` and `"STARTTLS" -> StartTLS`; it rejects anything else with `parserError("Unknown 'socketType' value…")` ([`RealAutoconfigParser.kt#L249-L252`](https://github.com/thunderbird/thunderbird-android/blob/2d881297d9005af4d8035d382f078d07aaa0e079/feature/autodiscovery/autoconfig/src/main/kotlin/app/k9mail/autodiscovery/autoconfig/RealAutoconfigParser.kt#L249-L252)).
  - At connect time a missing STARTTLS is fatal: `throw MissingCapabilityException(Capabilities.STARTTLS)` (`mail/protocols/imap/.../RealImapConnection.kt#L285-L286`) and `throw MissingCapabilityException("STARTTLS")` (`mail/protocols/smtp/.../SmtpTransport.kt#L135`).

**Why it matters now.** These opener lines are identical upstream (`src/Adjutant.Core/Providers/ImapProviderService.cs:154,170` @ `3b2762b`). The plan does not introduce them. What it adds is the reach:

- Every identity, not one administrator, now saves accounts.
- The servers come from DNS and ISPDB, not from the person typing them.
- Measured today, ISPDB's SMTP entries for four providers on the plan's own preset list are on port 587 with STARTTLS and a password:
  - `tim.it`: `587 STARTTLS password-cleartext`
  - `alice.it`: `587 STARTTLS password-cleartext`
  - `fastwebnet.it`: `587 STARTTLS password-encrypted`
  - `icloud.com`: `587 STARTTLS password-cleartext`

  Each of these becomes `StartTlsWhenAvailable` under `Auto`. (libero.it, virgilio.it, gmx.net, web.de and mail.ru are on 465/SSL and are safe.)
- The check runs on every «Collega» and «Prova di nuovo».

**The CI servers depend on the fallback.** In the fork's `ci.yml`:

- GreenMail is `-p 3143:3143`, plaintext.
- Dovecot's `TestData/Dovecot/test.conf` has `ssl = no` and `auth_allow_cleartext = yes`.

`Verify_RightPassword_Passes` therefore passes *because* `Auto` falls back to cleartext. A bare "use `StartTls`" would turn the suite red.

**Change to the plan.**

- **P1:** give `ImapAccountConfig` two security fields, `imapSecurity` and `smtpSecurity`, each one of `ssl`, `starttls` or `none`.
  - The default comes from the port: 993 or 465 means `ssl`; anything else means `starttls`, which is required (`SecureSocketOptions.StartTls`).
  - `none` (`SecureSocketOptions.None`) happens only when it is written explicitly: in tests, or in the manual «Impostazioni server» behind a visible warning.
  - Existing stored accounts get no implicit `none`. They gain TLS-required behaviour and are re-checked in L1 step 0. If the operator wants zero behaviour change for the read path, apply this only to the verifier and to new writes, and record that choice in "Decisions".
- **P4:** add `imapSecurity` and `smtpSecurity` to `DiscoveryResult` and to every preset entry, taken from ISPDB's `socketType` (SSL → `ssl`, STARTTLS → `starttls`). Skip any server whose `socketType` is `plain`; Thunderbird for Android's parser rejects that value.
- **P1 mapping:** a `NotSupportedException` whose message is "…does not support the STARTTLS extension" (`ImapClient.cs#L1482`, `SmtpClient.cs#L1369`) maps to `tls_failed`, not `unreachable`.
- **Tests:** add `Verify_StartTlsMissing_IsTlsFailed` against the plaintext Dovecot with `imapSecurity` unset, and keep the existing tests with an explicit `imapSecurity=none`.

### H2. Discovery skips the provider's own autoconfig and never checks the MX domain against the preset table, which is where Italian business domains fall

**What the plan says.** The order is: preset(domain), then ISPDB(domain), then MX, then ISPDB(the MX domain's last two labels), then `unknown`.

**What the reference does.**

- **Thunderbird** runs these lookups in priority order: `const lookups = ["fromDisk", "fromISP", "fromDB", "forMX"];` ([`FindConfig.sys.mjs#L63`](https://github.com/mozilla/releases-comm-central/blob/7d33bdaab2a46d4f1890120a5bced3fbe9bf0eea/mail/components/accountcreation/modules/FindConfig.sys.mjs#L63)).
  - `fromISP` asks the domain's own servers before ISPDB: `` const conf1 = `autoconfig.${sanitizedDomain}/mail/config-v1.1.xml`; `` and `` const conf2 = `${sanitizedDomain}/.well-known/autoconfig/mail/config-v1.1.xml`; `` ([`FetchConfig.sys.mjs#L103-L106`](https://github.com/mozilla/releases-comm-central/blob/7d33bdaab2a46d4f1890120a5bced3fbe9bf0eea/mail/components/accountcreation/modules/FetchConfig.sys.mjs#L103-L106)).
  - After the MX lookup it asks both the ISP's own server and ISPDB for the MX domain, and the ISP fetch is https-only: `_fetchConfigFromIsp(lookupDomain, emailAddress, true, // httpsOnly` (`FetchConfig.sys.mjs#L236-L245`).
- **Thunderbird for Android** has the same three discoveries: `createProviderAutoconfigDiscovery`, `createIspDbAutoconfigDiscovery` and `createMxLookupAutoconfigDiscovery` (`service/RealAutoDiscoveryRegistry.kt`).
  - After the MX lookup it checks `https://autoconfig.{mx-domain}/mail/config-v1.1.xml` and then ISPDB (`PostMxLookupAutoconfigUrlProvider.kt`).
  - By default it **does not send the address**: `includeEmailAddress = false` (`RealAutoDiscoveryRegistry.kt#L20`). Thunderbird makes the same parameter optional through the pref `mailnews.auto_config.fetchFromISP.sendEmailAddress` (`FetchConfig.sys.mjs#L128-L133`). The spec's anti-goal, that the local part never leaves the box, therefore survives the extra step.
- **SRV (RFC 6186): neither client does it.** A `gh` code search for `_imaps` in both repositories returned zero hits. The plan's omission matches practice.

**Measured today (ISPDB `https://autoconfig.thunderbird.net/v1.1/<d>`; ISP `https://autoconfig.<d>/mail/config-v1.1.xml`):**

| Hoster's own domain | ISPDB | ISP autoconfig |
|---|---|---|
| aruba.it | 404 | 500 |
| register.it | 404 | no answer |
| tophost.it, netsons.com, serverplan.com, keliweb.it, ionos.it | 404 | no answer |
| hostinger.com | 404 | 404 |
| siteground.com | 404 | no answer |
| ovh.it | 404 | not probed |
| qboxmail.com | 404 | **200** |
| ovh.net, secureserver.net, pec.it | 200 | not probed |

**Customer domains, measured today on a small sample of Italian business domains (names withheld because the repository is public):**

- **One hosted at IONOS:** MX `mx00.ionos.it`, base `ionos.it`, ISPDB 404. The plan's MX step ends at `unknown`. A preset entry for `ionos.it`, looked up by the *MX* base domain, would find it. Today the plan looks presets up only by the address's domain.
- **One hosted at Aruba** (NS `dns.technorail.com`): MX `mx.<customer-domain>`, which resolves to an Aruba address (62.149.128.x) with no PTR. ISPDB and `https://autoconfig.<customer-domain>` both miss.
  - The MX base domain *equals* the address's domain, so Thunderbird stops: `throw new Error("MX lookup would be no different from domain")` (`FetchConfig.sys.mjs#L216-L217`). Thunderbird for Android stops too ("Exit early to match Thunderbird's behavior.", `MxLookupAutoconfigDiscovery.kt#L43`).
  - Forum and third-party guides (not Aruba's own page, which could not be read) say Aruba asks customers for exactly this `mx.<own domain>` record. They also say a mail client should use `imaps.aruba.it:993` and `smtps.aruba.it:465`, not a host on the customer's domain.
  - **Neither the plan nor either reference client resolves an Aruba-hosted domain.** It lands on `unknown`.
- **One on Google Workspace:** MX `smtp.google.com`, which the plan's `*.google.com` rule handles.

For the target user, a small Italian office, the most likely hosting is exactly the case nothing covers. The only signals in reach are the NS records (`*.technorail.com` / `*.arubadns.*`) and the MX's address block. Neither is used by Thunderbird or Thunderbird for Android, so matching on them is a bespoke heuristic. Under "STOP BEFORE BESPOKE", that is a question for the operator, not a change this review can propose as established practice.

**Change to the plan (P4).** Order, first hit wins, all under the existing 12 s budget. The remote steps may run in parallel with priority, as both clients do.

1. Preset(address domain).
2. ISP autoconfig: `https://autoconfig.<domain>/mail/config-v1.1.xml`, then `https://<domain>/.well-known/autoconfig/mail/config-v1.1.xml`.
   - https only.
   - No `emailaddress` query parameter.
   - No redirects, and a response size cap.
   - An SSRF guard: a `SocketsHttpHandler.ConnectCallback` that refuses loopback, RFC 1918, link-local and the compose network. The PIM sits next to Postgres and ArcadeDB, and the spec already treats probing as a risk.
3. ISPDB(domain).
4. MX (lowest preference). Stop if its base domain equals the address's domain (Thunderbird: `throw new Error("MX lookup would be no different from domain")`, `FetchConfig.sys.mjs#L216-L217`).
5. **Preset(MX next-subdomain, then MX base domain)**, so that an MX under a hoster's domain (the IONOS case) reaches a preset. Hosted-domain entries must cite the hoster's page for *hosted* domains, which may name different hosts from its own mailboxes. For Aruba, third-party guides give `smtps.aruba.it` for hosted domains and `smtp.aruba.it` for `@aruba.it`.
6. ISP autoconfig (https) and ISPDB, for the MX next-subdomain and then the MX base domain.
7. `unknown`.

Add `source` values `isp` and `preset-mx`. Add to the spec's and L1's "what this does not prove":

> Domains hosted at Aruba (MX under the customer's own domain) are not discovered; the person fills in the server fields.

If the operator wants Aruba handled anyway, ask the operator before writing code. The candidate is an NS-based rule, which is not established practice. Add tests:

- `Isp_AutoconfigOfTheDomain_WinsOverIspdb`
- `Mx_UnderAPresetDomain_IsThatPreset`
- `Isp_NeverSendsTheAddress`
- `Isp_RefusesAPrivateAddress`

### H3. The Outlook.com app-password fallback cannot work

**What the plan says.**

- P4 step 1: "The `google` and `outlook.com` entries also carry their IMAP/SMTP hosts and app-password rule … so W2 can fall back to an app password when no OAuth app is configured."
- W2 behaviour 2 offers «Usa una password per app» for that case.

**What the reference does.**

- ISPDB's `outlook.com` entry lists **only** `<authentication>OAuth2</authentication>` for IMAP (`outlook.office365.com:993`) and for SMTP (`smtp-mail.outlook.com:587 STARTTLS`). Measured today.
- `gmail.com` lists `OAuth2` and `password-cleartext`.
- Microsoft's support page "Modern authentication methods now needed to continue syncing Outlook Email in non-Microsoft email apps": "Basic Authentication no longer available to access any Outlook account" and "After September 16th, users attempting to connect their Microsoft accounts through Basic Authentication will fail to do so." This quote came through the fetch tool's summary; the page is rendered by JavaScript and returned no text to `curl`. It is corroborated by the raw ISPDB entry.

An app password is basic authentication, so the fallback leads every Outlook.com user without a Microsoft OAuth app into a guaranteed `auth_failed`.

**Change to the plan.**

- Keep the app-password fallback for `google` only.
- For `outlook.com` and `microsoft365` with no provider app configured, `providerAppMissing` says that the administrator must configure the Microsoft app, and offers no password path.
- Pin it with `NoMicrosoftAppOffersNoPassword` (W2) and `Preset_OutlookCom_HasNoAppPasswordRule` (P4).
- The P4 rule "copy every rule from the provider's own page" would catch this. The plan text pre-commits to the opposite, so fix the text.

### H4. ISPDB parsing: OAuth-only answers become a password form, and priority order and `%EMAILDOMAIN%` are missing

**What the plan says.** "parse `incomingServer type="imap"` and `outgoingServer type="smtp"`, substitute `%EMAILADDRESS%` / `%EMAILLOCALPART%`." Nothing more. ISPDB(domain) runs before the MX step.

**Why HIGH.** Measured today, ISPDB answers `msn.com` (not in the spec's preset list) and `outlook.com` with IMAP `outlook.office365.com:993` and `<authentication>OAuth2</authentication>` **only**. The plan's parser would return that as `{kind:"imap", source:"ispdb", imapHost:"outlook.office365.com", …}`. The cockpit then asks for a password and the check fails with `auth_failed`: a guaranteed dead end that reads as "wrong password". The MX step, which would have routed it to Microsoft, is never reached.

**What the reference does.**

- Format, raw line 227: "incomingServer and outgoingServer may appear several times. They are in order of priority, the first entry should generally be used".
- Placeholders, lines 236-238: `%EMAILADDRESS%`, `%EMAILLOCALPART%`, `%EMAILDOMAIN%`, "can be used as value or value part in most settings which take a string."
- Thunderbird substitutes in the hostname as well: `config.hostname = _replaceVariable(config.hostname, otherVariables);` and `otherVariables.EMAILDOMAIN = emaildomain;` ([`AccountConfig.sys.mjs#L566`, `#L593`](https://github.com/mozilla/releases-comm-central/blob/7d33bdaab2a46d4f1890120a5bced3fbe9bf0eea/mail/components/accountcreation/modules/AccountConfig.sys.mjs#L566)).
- Thunderbird for Android substitutes `replace("%EMAILDOMAIN%", domain)` and applies it to the hostname too (`RealAutoconfigParser.kt#L213`, `#L313`).
- Thunderbird rejects a port outside 1-65535 (`integerRange(iX.port, 1, 65535)`, `readFromXML.sys.mjs#L102`).
- Measured: `yahoo.com` lists two IMAP servers, `imap.mail.yahoo.com` and then `export.imap.mail.yahoo.com`, each with `OAuth2` before `password-cleartext`. `outlook.com` lists `OAuth2` only (H3).

**Change to the plan (P4).**

- Take the **first** `incomingServer type="imap"` and the first `outgoingServer type="smtp"` that pass all of these:
  - a valid hostname after substitution;
  - a port of 1-65535;
  - a `socketType` of SSL or STARTTLS (H1);
  - an `<authentication>` that includes `password-cleartext` or `password-encrypted`.
- Skip OAuth2-only entries. An ISPDB answer with no password-capable IMAP server is a miss and falls through to the MX step. There, `msn.com`'s MX `msn-com.olc.protection.outlook.com` routes it to `outlook.com` (see M1).
- Substitute all three placeholders in the hostname and the username.
- Tests:
  - `Ispdb_TakesTheFirstUsableServer` (the Yahoo shape)
  - `Ispdb_OAuthOnlyIsAMiss` (the msn.com / outlook.com shape → not `kind:imap`)
  - `Ispdb_SubstitutesTheDomainInTheHostname`

---

## MEDIUM

### M1. The MX step misclassifies Microsoft consumer domains, and "last two labels" is not a registrable domain

**What the plan says.** MX `*.outlook.com` or `*.protection.outlook.com` maps to `microsoft365` (spec Part 1 §3). The plan's test is `Mx_MicrosoftHosted_IsMicrosoft365 (*.mail.protection.outlook.com)`. The registrable domain is "taken as the last two labels" (P4 step 4).

**What the reference does.**

- **Thunderbird** also checks the MX host minus its first label, "to differentiate between Outlook.com/Hotmail and Office365 business domains" ([`FetchConfig.sys.mjs#L220-L229`](https://github.com/mozilla/releases-comm-central/blob/7d33bdaab2a46d4f1890120a5bced3fbe9bf0eea/mail/components/accountcreation/modules/FetchConfig.sys.mjs#L220-L229)).
- Its base domain comes from the Public Suffix List: `const sld = Services.eTLD.getBaseDomainFromHost(mxHostname);` with the comment "Thanks to Services.eTLD, we also get bbc.co.uk right." (`#L200`, `#L214`).
- **Thunderbird for Android** does the same: `topPrivateDomain()` (OkHttp's PSL, `OkHttpBaseDomainExtractor.kt#L9`) and `getNextSubDomain(mxHostName)` (`MxLookupAutoconfigDiscovery.kt#L47-L49`).

**Measured today:**

- `hotmail.com` MX is `hotmail-com.olc.protection.outlook.com`.
- `msn.com` MX is `msn-com.olc.protection.outlook.com`.

`*.olc.protection.outlook.com` is the **consumer** (Outlook.com) platform; `*.mail.protection.outlook.com` is Microsoft 365.

- Once H4 makes an OAuth-only ISPDB answer fall through, `msn.com` reaches the MX step. The plan's rule then sends it to the `microsoft365` (work account) flow.
- In this codebase a personal account needs the `consumers` authority (`reference_microsoft_personal_account_needs_consumers`), so that flow fails.

The last-two-labels rule gives `co.uk` for `mx.example.co.uk`. Italian `.it` hosts happen to work, but the rule is wrong in general.

**Change to the plan (P4).**

- Map `*.olc.protection.outlook.com` to `outlook.com`. Map `*.mail.protection.outlook.com` (and `*.outlook.com` otherwise) to `microsoft365`.
- Add `msn.com` to the Outlook.com preset.
- Compute the base domain with a PSL library. Read its docs first; one candidate is `Nager.PublicSuffix`. Or embed the PSL `.dat` file as a resource; either way, record the dependency in `AURA-FORK.md`.
- If the builder keeps last-two-labels, write it down as a known limitation in "Decisions", with the `co.uk` counter-example.
- Tests: `Mx_OutlookConsumer_IsOutlookCom` (MX `msn-com.olc.protection.outlook.com`) and `Mx_BaseDomain_UsesThePublicSuffixList`.

### M2. Settings found through the MX lookup are not trusted, but the plan sends the password to them without showing them

**What the plan says.** «Impostazioni server» opens by itself only on `unknown` or after a failure. An `ispdb-mx` result goes straight to «Collega» with the password.

**What the reference does.**

- Thunderbird for Android, `AutoDiscoveryResult.kt#L22-L24`: "IMPORTANT: When this value is `false`, the settings should be presented to the user and only be used after the user has given consent."
- `isTrusted` is false unless the MX answer was DNSSEC-authenticated (`MiniDnsMxResolver.kt#L18`: `isTrusted = if (result.wasSuccessful()) result.isAuthenticData else false`).
- Thunderbird's own MX comment: "DNS is unprotected, meaning the results could be forged." (`FetchConfig.sys.mjs`, `fetchConfigForMX` doc comment.)

**Change to the plan (W2).** For `source` `ispdb-mx` or `preset-mx`:

- Show the IMAP host name in one line under the password field before «Collega», for example «Mi collegherò a imap.example.org».
- Or open the disclosure.

Pin it with `MxDerivedServersAreShownBeforeConnecting`. DnsClient does not validate DNSSEC, so treat every MX result as untrusted.

### M3. The SMTP check passes without checking the password when the server does not advertise AUTH

**What the plan says.** P1 says SMTP "open, disconnect", with today's `OpenSmtpAsync`. That method authenticates only `if (client.Capabilities.HasFlag(SmtpCapabilities.Authentication))` (`ImapProviderService.cs:171`).

**What the reference does.**

- Thunderbird for Android with a password configured: `if (authPlainSupported) saslAuthPlain() else if (authLoginSupported) saslAuthLogin() else throw MissingCapabilityException("AUTH PLAIN")` (`SmtpTransport.kt#L161-L169`). It never silently sends without authenticating.
- Thunderbird desktop does not verify SMTP at all: "Broken assumption, but we currently have no SMTP verification." (`ConfigVerifier.sys.mjs#L196`). The plan's SMTP check is a step beyond practice, so it should not pass vacuously.

**Why it matters.** Many servers advertise `AUTH` only after STARTTLS. Combined with H1, a STARTTLS strip leads to no AUTH and therefore no authentication, so the check says "ok". The first real send then fails with a relay-denied error, far from the form. That is exactly the failure the spec's goal 1 removes.

**Change to the plan (P1).** In the verifier only, so the send path is unchanged: after `OpenSmtpAsync`, if a password is configured and `!client.IsAuthenticated`, fail `("smtp","smtp_auth_failed")`, logged as "server advertised no AUTH". Pin it with `Verify_SmtpWithoutAuth_IsSmtpAuthFailed`, using GreenMail SMTP without `-Dgreenmail.auth.disabled`, after reading GreenMail's docs as decision 2 already requires.

### M4. `VerifyReasons.From(step, exception)` maps by exception type alone, which mislabels several MailKit paths

**What the plan says.** It maps by type: `AuthenticationException` to `auth_failed`; `SocketException` or `TimeoutException` to `unreachable`; `SslHandshakeException` to `tls_failed`; `ImapCommandException` to `mailbox_refused`. Anything else maps to `unreachable` / `smtp_unreachable`.

**What MailKit 4.18.1 actually throws:**

| Situation | Exception | Plan maps to | Should be |
|---|---|---|---|
| Wrong IMAP password, LOGIN path | `AuthenticationException(ic.ResponseText or resp-code message)` (`ImapClient.cs#L922-L933`) | auth_failed | auth_failed ✓ |
| Wrong IMAP password, SASL path with `[ALERT]` | `AuthenticationException` (`EmitAndThrowOnAlert`, `#L935-L944`) | auth_failed | ✓ |
| Server says `LOGINDISABLED` (plaintext, no STARTTLS) | `AuthenticationException("The LOGIN command is disabled.")` (`#L1213`) | auth_failed ("wrong password") | tls_failed |
| `BYE` during AUTHENTICATE | `ImapProtocolException.Create(ic)` (`#L1325-L1327`) | unreachable | auth_failed |
| STARTTLS required but absent (after H1) | `NotSupportedException` (`#L1482`, SMTP `#L1369`) | unreachable | tls_failed |
| Wrong SMTP password (535) | `AuthenticationException(message, inner: SmtpCommandException(StatusCode))` (`SmtpClient.cs#L1263-L1275`) | smtp_auth_failed | ✓ |
| SMTP temporary refusal (454 / 4xx) on AUTH | the same `AuthenticationException` | smtp_auth_failed ("wrong password") | smtp_unreachable (temporary); read `((SmtpCommandException)ex.InnerException).StatusCode` |
| SMTP offers no usable mechanism | `NotSupportedException("No compatible authentication mechanisms found.")` (`#L1277`) | smtp_unreachable | smtp_auth_failed |
| SELECT refused | `ImapCommandException(Response = No)` | mailbox_refused | ✓, but only at the SELECT stage; MailKit also throws `ImapCommandException.Create("STARTTLS", ic)` at connect (`#L1506`) |
| TLS handshake / certificate refused | `SslHandshakeException.Create(...)` (`#L1603-L1608`) | tls_failed | ✓ |
| The verifier's 20 s budget expires | `OperationCanceledException` (MailKit honours the token) | unreachable via "other" | ✓ |
| MailKit's own connect `Timeout` (default 2 min) expires | `throw new TimeoutException ();` when the caller's token is not cancelled (`MailKit/Net/SocketUtils.cs#L240-L245`) | unreachable | ✓; keep the plan's row |
| The host name does not resolve | `IOException("Failed to resolve host: …")` (`SocketUtils.cs#L218`), **not** `SocketException` | unreachable via "other" | ✓, but add the row so the test pins the real type |

**Change to the plan (P1).**

- Make the mapping stage-aware: `From(step, stage, ex)`, where stage is `connect`, `auth` or `select`. The verifier knows which call it is in.
- Add the four corrections from the table.
- Keep the `TimeoutException` row. Add `OperationCanceledException when budget.IsCancellationRequested && !ct.IsCancellationRequested`, and add `IOException` (an unresolvable host).
- A caller cancellation (the cockpit request gone) is rethrown, not logged as a failure.
- Name the type in tests: `MailKit.Security.AuthenticationException`. `System.Security.Authentication.AuthenticationException` is a different type with the same name, and `SslStream` throws that one (MailKit wraps it in `SslHandshakeException`).
- All constructors used compile at 4.18.1: `SslHandshakeException()` (`#L126`), `AuthenticationException()` (`#L91`), `ImapCommandException(response, responseText, message)`.

### M5. The key ring sits unencrypted, on the same volume as the ciphertext. State what `ENC:` protects against, and against what it does not

**What the plan says.** Encrypt the `password` key with `PasswordProtector`. The spec asks the builder to confirm the key ring is on a kept volume. The plan does not record the answer.

**Measured.**

- `DataProtectionExtensions.cs` calls `.SetApplicationName("CalendarMcp").PersistKeysToFileSystem(new DirectoryInfo(keysDir))` with `keysDir = <data>/keys`.
- The Dockerfile sets `ENV CALENDAR_MCP_CONFIG=/app/data`.
- Aura's `compose.yaml:1404` mounts `aura-pim-data:/app/data`, a named volume (`:1712`).
- The updater prunes only anonymous volumes (`deploy/aura-image-update.sh:308`).

The key ring therefore survives restarts and updates. **Confirmed; no new volume is needed.**

**What the reference does.**

- `default-settings`: "If the developer overrides the rules outlined above and points the Data Protection system at a specific key repository, automatic encryption of keys at rest is disabled."
- `overview`: "the data protection key ring should be protected using appropriate permissions. You can choose to encrypt keys at rest, but this doesn't prevent cyberattackers from creating new keys … Ensure only the identity under which your web app runs has read, write, and create access to that directory."
- Rotation is safe:
  - "Keys have a 90-day lifetime by default … As long as retired keys remain on the system, your app can decrypt any data protected with them."
  - "Created, active, and expired keys may all be used to unprotect incoming payloads."
  - Deleting a key "is truly destructive behavior."

**Consequence.** `appsettings.json` and `keys/*.xml` live side by side in plain files. `ENC:` stops a leak of the config file *alone*, such as a copied file, a support bundle or a log of the dictionary. It does not stop anyone who can read the volume.

The spec's backup sentence ("the key ring travels with the PIM's data volume") assumes a backup that is not visible. `docs/BACKUP-RESTORE.md` does not mention the PIM, and outside `compose.yaml` no non-markdown file in Aura names `aura-pim-data`. No Aura backup that names this volume was found. The backup handlers' scope was not read line by line.

**Change to the plan.**

- P2 adds one paragraph to `AURA-FORK.md`, and L1 adds it to the verification file's "what this does not prove":
  - The keys are unencrypted at rest, next to the ciphertext.
  - The protection is against a leak of the config file only.
  - No Aura backup was found that covers this volume.
  - Never delete files in `keys/`.
- Add a test that pins the key directory under the data directory, so a future sync cannot move it.
- Optional, and the operator decides: `ProtectKeysWithCertificate` from a secret Aura already holds. Not required for this work.

### M6. `ProviderSecrets.Protect` trusts a client-supplied `ENC:` value

**What the plan says.** `ProviderSecrets.Protect` protects the password "unless it is empty or already `ENC:`". It is used on create, on the PATCH merge and on upstream `PUT`; the request body flows through it.

**What the reference does.** `PasswordProtector.IsProtected` is documented as a *form-mapper* helper, for telling a round-tripped saved value from a newly typed one (`PasswordProtector.cs` doc comment). Upstream applies `Protect(Password)` unconditionally to what the form typed (`src/Adjutant.HttpServer/BlazorAdmin/AccountFormModels.cs:216-220` @ `3b2762b`); the round-trip case is a different code path.

**Consequences.**

1. A real password that begins with `ENC:` is stored in the clear. `Unprotect` then tries to decrypt it, throws `CryptographicException`, and the account is `auth_failed` forever.
2. Confused deputy: a request carrying `password: "ENC:<blob>"` with the attacker's own IMAP host makes the PIM decrypt the blob, which is valid under the shared key ring and purpose, and send the plaintext to the attacker's server. Today `GET /admin/accounts` never returns `providerConfig`, so a tenant has no way to obtain another tenant's blob. The exposure is latent, not open.

**Change to the plan (P2/P3).**

- Values that come from a request are always plaintext and always protected.
- Only the stored values kept by the PATCH merge, and the startup pass, use `IsProtected`.
- Split the helper into `ProtectIncoming(IReadOnlyDictionary)`, which is unconditional, and the startup pass's own check.
- Test: `Create_PasswordStartingWithENC_IsStillEncrypted`.

---

## LOW

### L1. The `documentation url` is not a help link for app passwords

The format says of `<documentation>`: "A page where the ISP describes the configuration. This is purely informational and currently mainly for maintenance of the files and not used by the client at all." (wiki raw, lines 122-124). Thunderbird's `readFromXML.sys.mjs` does not read the element at all.

**Change:** drop it from P4. If it is kept, never show it as the app-password notice; the plan's decision 4 already keeps `required = false`.

### L2. Inventory before invention: nothing upstream to reuse, but cite what P2 restores

- `TestAccountCommand` (fork `src/CalendarMcp.Cli/Commands/TestAccountCommand.cs`, and upstream v2.0.0 `src/Adjutant.Cli/Commands/TestAccountCommand.cs:265`) only checks that the IMAP fields are present: "Live IMAP login is not probed by test-account".
  - Upstream has no IMAP verify, no autoconfig and no MX code. Its only discovery is `DavDiscovery.cs`, RFC 6764 for CalDAV/CardDAV.
  - The new verifier and discovery are justified.
  - Optional, out of scope: once `ImapAccountVerifier` exists, the CLI's IMAP branch could call it, giving one way to check an IMAP account.
- P2 restores upstream behaviour the fork lost with the Blazor admin. Upstream's `BuildImapProviderConfig` protects the IMAP password on save (`AccountFormModels.cs:216-220`), and `ExchangeProviderConfig.Build(..., passwordProtector)` does the same for v2.0.0's Exchange provider. Say so in `AURA-FORK.md`. This also supports decision 3 (every provider's `password` key).
- Sync safety, checked: upstream v2.0.0 keeps `ApplicationName = "CalendarMcp"` and `Purpose = "CalendarMcp.ProviderConfig.Password"`. Its `ConfigurationPaths` honours "ADJUTANT_CONFIG environment variable (or legacy CALENDAR_MCP_CONFIG)". Taking the rename will not orphan the key ring.

### L3. DnsClient: set the timeouts, and the API to use

- The defaults are `s_defaultTimeout = TimeSpan.FromSeconds(5)` and `Retries { get; set; } = 2` ([`DnsQueryOptions.cs#L29`, `#L69`](https://github.com/MichaCo/DnsClient.NET/blob/f1e7ca33d713dc3bc70e7b3664aa3a2b6c090d5d/src/DnsClient/DnsQueryOptions.cs#L29)). One query can take up to 15 s, against the plan's 5 s step.
- Use one singleton `LookupClient(new LookupClientOptions { Timeout = TimeSpan.FromSeconds(2), Retries = 1 })`. `LookupClientOptions : DnsQueryAndServerOptions` is at `DnsQueryOptions.cs#L336`, and `UseCache` already defaults to `true` (`#L51`). Call `QueryAsync(domain, QueryType.MX, QueryClass.IN, ct)` (`IDnsQuery.cs#L96`) with the step's token.
- Read `response.Answers.MxRecords()` (`ResourceRecordCollectionExtensions.cs#L144`). Order by `Preference` and take the lowest (`MxRecord.Preference` / `.Exchange`, `MXRecord.cs#L48-L53`), as Thunderbird does: "only the most preferred … is used".
- Treat a null MX (RFC 7505, exchange `.`) and `HasError` as no MX.
- 1.8.0 (2024-07-01) is the latest stable release, which meets "at least two weeks old".
- Decision 7 is right for .NET 10: `System.Net.Dns` has no record-type queries. `Dns.ResolveMxAsync` exists **only in .NET 11** (its docs page lists the moniker `net-11.0` alone). Note in `AURA-FORK.md` that DnsClient can go when the fork moves to .NET 11.

### L4. One check makes several login attempts, so lockouts are possible

MailKit tries every SASL mechanism the server offers and then falls back to LOGIN (`ImapClient.cs#L1284-L1350`). A wrong password is two or more failed logins per «Collega» or «Prova di nuovo», and Italian and Yahoo-class providers lock accounts after repeated failures.

**Change:** rate-limit `verify` per account id in the PIM, for example one check per 30 s, answering `429` without connecting. Add a line to W4.

### L5. TLS failure on custom hostnames

Small hosting providers often present a shared certificate (`*.hoster.it`) for `mail.<customer-domain>`. MailKit's validation then throws `SslHandshakeException`, mapped to `tls_failed`, and Thunderbird's equivalent offers a security exception.

**Change:** the `tls_failed` copy should say «Usa il nome del server indicato dal provider (non quello del tuo dominio)», not only "TLS refused". No exception mechanism.

### L6. The `unknown` path

Thunderbird's `GuessConfig` probes `imap.`, `mail.` and the bare domain on standard ports (`GuessConfig.sys.mjs#L754-L766`). Probing from a server-side PIM on the compose network is a different risk, so do not copy it. But pre-filling `imap.<domain>` / `smtp.<domain>` as *placeholders*, not values, in the opened disclosure costs nothing.

### L7. ISPDB cache

Cache misses (404) as well, for a shorter time such as 1 h, so that a domain absent from ISPDB does not cost a remote round trip on every blur.

---

## Verified as the plan states

- ISPDB URL `https://autoconfig.thunderbird.net/v1.1/<domain>` is Thunderbird's `mailnews.auto_config_url` usage (`FetchConfig.sys.mjs`, `fetchConfigFromDB`). ISPDB is sent the domain only.
- A wrong IMAP or SMTP password surfaces as `MailKit.Security.AuthenticationException`. TLS failures surface as `SslHandshakeException`. A refused SELECT surfaces as `ImapCommandException` with `Response = No`.
- `ProviderFailures.Classify` already maps `CryptographicException` to `provider_auth` (`src/CalendarMcp.Core/Changes/ProviderFailures.cs`), so a lost key ring lands on «Cambia password» as the spec says.
- DataProtection: the application name is constant, and expired keys still decrypt, so 90-day rotation needs no re-encryption pass. The key ring is on a persistent named volume.
- `Unprotect` passes plaintext through (`PasswordProtector.cs`), so the startup migration is safe to run before or after a rollback.
- `.NET 10` has no MX API (decision 7).
- SRV / RFC 6186 is not used by either reference client, so the plan need not add it.
