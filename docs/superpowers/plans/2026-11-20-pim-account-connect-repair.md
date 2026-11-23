# Connecting and repairing a mail account: implementation plan

**Status:** v2, 2026-11-20. v1 was reviewed the same day by three reviewers: grounding (RED: 5 critical, 1 high), adversarial (YELLOW: 5 high) and cross-source against Thunderbird, Thunderbird for Android, MailKit and DataProtection (YELLOW: 4 high). Their reports are in `docs/superpowers/plans/validation/2026-11-20-pim-account-connect-repair-{grounding,adversarial,crosssource}.md`. The operator then decided four points ("Apri Impostazioni a tutti", "TLS obbligatoria per account nuovi e modificati", "SMTP: si salva, con avviso", "indirizzi privati solo per l'admin"), recorded as spec amendments A1, A2, A4 and A5. **v1→v2:**
- Every CRITICAL and HIGH finding is folded in.
- The MEDIUM and LOW findings are folded in except the ones listed under "Not taken".
- New tasks: P4 (the watch logs the server's line) and the `HttpError` change in W1.
- Tasks renamed: discovery is P5.

Not started. Next step: execution (the operator dropped the full-code planning pass on 2026-11-20).

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans, task by task.
> - P1-P5 run in the PIM fork (`D:/tmp/aura-pim-mcp`), on a branch `aura/account-connect` off `main` `bb87b82`.
> - A1-A2, W1-W4 and G1 run in Aura, on `master`.
> - L1 is the live run.
>
> Steps use checkbox syntax.

**Goal:** A person types an address and a password in Settings → «Posta e calendario». The PIM finds the servers the way Thunderbird does. It checks the login, the inbox and the watch's first read over TLS, and stores the account only if that passes; a sending failure is only a warning. The password is stored encrypted. A broken IMAP account offers «Prova di nuovo» and «Cambia password» on the same account id, and a successful repair wakes its mail watch.

**Architecture:**
- **PIM.** Three new admin routes: `POST /admin/discover`, `POST /admin/accounts/{id}/verify` and `PATCH /admin/accounts/{id}/provider-config`. `POST /admin/accounts` verifies an `imap` account before writing it.
  - Every write stores the password through `PasswordProtector`, and one atomic startup pass encrypts the passwords already stored.
  - Verification shares the connection openers, which move into `ImapConnections.cs` and learn explicit TLS modes. It connects through a guarded socket that refuses private addresses unless Aura says the caller is an administrator.
  - The IMAP pool remembers the config it opened with, and drops a connection whose config changed.
- **Aura.** The cockpit relay forwards the three routes, plus create, with longer injectable timeouts, a per-identity limit and an idempotency fingerprint that keeps no password hash. It sets `X-Aura-Allow-Private-Hosts` for administrators only. After a successful verify or PATCH it calls `pimwatch.Service.Repaired`, which resumes a watch paused for a credential-class failure and moves its next run to now.
- **Cockpit.** Settings opens to members. A new section holds the account list, the address+password add flow and the repair actions; Governance keeps the provider OAuth apps and links to the section.

**Tech Stack:**
- PIM: .NET 10, MailKit 4.18.2, ASP.NET minimal APIs, DataProtection, `Microsoft.Extensions.Caching.Memory` 10.0.13 (new), DnsClient 1.8.0 (new), MSTest with Rocks, GreenMail 2.1.15 and Dovecot 2.4.5.
- Aura: Go (agui, pimwatch, cron); React, TanStack Query, i18next, vitest, Stryker.

**Spec:** `docs/superpowers/specs/2026-11-20-pim-account-connect-repair-design.md`, approved 2026-11-20, with amendments A1-A10 of the same day.

## Global Constraints

- **Verify-failure body:** `422 {"error":"verify_failed","step":"imap"|"smtp","reason":R}`, where R is one of `auth_failed`, `unreachable`, `tls_failed`, `mailbox_refused`, `smtp_auth_failed`, `smtp_unreachable`.
  - A passing check may carry `"smtpWarning": "smtp_unreachable"|"tls_failed"` in the 200/201 body (A4). `smtp_auth_failed` always blocks.
  - No server text appears in any response. The server's response line goes to the PIM log only, with CR/LF stripped and cut at 200 characters (A8).
- **Budgets.** IMAP step 12 s, SMTP step 8 s, each with its own token.
- **Discovery limits.** 4 s per remote step and 12 s overall; a timeout falls through. DnsClient runs with `Timeout = 2 s` and `Retries = 1`. Cache: ISPDB/autoconfig hits for 24 h and misses for 1 h, with `SizeLimit` 1000 entries.
- **Discovery order (A6)**, first hit wins:
  1. Preset table.
  2. The domain's own autoconfig: `https://autoconfig.<d>/mail/config-v1.1.xml`, then `https://<d>/.well-known/autoconfig/mail/config-v1.1.xml`. HTTPS only, no address sent, no redirects, a 64 KiB body cap, private addresses refused.
  3. ISPDB: `https://autoconfig.thunderbird.net/v1.1/<d>`.
  4. MX, the lowest preference. A null MX or a DNS error means no MX. Stop if the MX's base domain equals `d`.
  5. For each suffix of the MX host, from the next subdomain down to two labels: preset, autoconfig, ISPDB.
  6. `unknown`.
- **What discovery sends off the box:** the domain, or a suffix of the MX host, and nothing else.
- **A usable server** is the first `incomingServer type="imap"` and the first `outgoingServer type="smtp"` that have:
  - a valid DNS hostname after substituting `%EMAILADDRESS%`, `%EMAILLOCALPART%` and `%EMAILDOMAIN%`;
  - a port in 1-65535;
  - a `socketType` of `SSL` or `STARTTLS`;
  - a password `authentication`.
  An answer with no usable IMAP server is a miss.
- **MX mapping:** `*.google.com`/`*.googlemail.com` → `google`; `*.olc.protection.outlook.com` → `outlook.com`; `*.mail.protection.outlook.com` and other `*.outlook.com` → `microsoft365`.
- **Discovery answer:** `{kind, source?, imapHost?, imapPort?, imapSecurity?, smtpHost?, smtpPort?, smtpSecurity?, username?, appPassword?: {required, helpUrl?}, passwordFallback?}`.
  - `kind` is one of `google`, `microsoft365`, `outlook.com`, `imap`, `unknown`.
  - `source` is one of `preset`, `isp`, `ispdb`, `preset-mx`, `isp-mx`, `ispdb-mx`.
  - `passwordFallback` is true only on the Google preset (A7).
- **TLS (A2).** Provider-config keys `imapSecurity`/`smtpSecurity` take `ssl` (`SslOnConnect`) or `starttls` (`StartTls`, required).
  - The key is absent only on accounts stored before this work; those keep `Auto`.
  - Create and PATCH default an absent key from the port (993/465 → `ssl`, otherwise `starttls`), and refuse `none` with `400 {"error":"tls_required"}`.
  - `none` → `SecureSocketOptions.None` exists only for tests on the plaintext CI servers.
- **Private addresses (A5).** Loopback, RFC 1918, link-local, CGNAT 100.64/10, ULA fc00::/7, `::1` and IPv4-mapped forms of these are refused with `422 reason "unreachable"` and a log line naming the refusal, unless the request carries `X-Aura-Allow-Private-Hosts: true`. Aura sets that header only for an identity holding `identity.create`, and never forwards a client's copy. The verifier resolves once, checks every resolved address, and connects MailKit through the checked `Socket`.
- **Limits.**
  - Aura, per identity: 10 verify/create/PATCH calls a minute, `429 {"error":"too_many_attempts"}`.
  - PIM, per account id: one verification every 30 s, `429` without connecting.
- **PATCH rules (A3).** `imap` only (`400 not_patchable` otherwise).
  - A changed `imapHost`, `imapPort`, `smtpHost` or `smtpPort` without `password` is `400 password_required`.
  - A different `username` is `400 username_immutable`.
  - Keys are compared case-insensitively, and duplicate keys that differ only in case are `400 duplicate_key`.
  - "Absent or `""`" means unchanged; a password is never trimmed.
  - The write re-reads the node under the file lock and answers `409 {"error":"changed"}` if the stored config differs from what was verified.
- **Stored secrets.** The `password` key of any provider, matched case-insensitively.
  - A value from a request is always protected, even if it starts with `ENC:`.
  - Only a value kept from storage is left as it is.
- **Stale-transient rule.** On an IMAP row, a cursor whose `last_error == "provider_transient"` and whose `failing_since` is more than 6 h old gets the new wording.
- **Connecting** never turns the mail watch on.
- **Account id from an address.**
  - Lowercase the address, turn every run of characters outside `[a-z0-9]` into `-`, and trim `-` at both ends.
  - Check it with the existing `pimAccountIdError(id) === null`.
  - On `409`, read the list. If the address is already connected, offer its repair. Otherwise retry with `-2` … `-9`.
- **Files ≤600 LOC; new code in new files.** Measured 2026-11-20:

  | File | LOC |
  |---|---|
  | `ImapProviderService.cs` | 598 |
  | `AdminEndpoints.cs` | 460 |
  | `AccountConfigurationService.cs` | 452 |
  | `connect_pim_api.go` | 367 |
  | `pimwatch/service.go` | 489 |
  | `serve_pimwatch.go` | 410 |
  | `serve_webui_routes.go` | 422 |
  | `resources.settings.ts` | 584 |
  | `resources.governance.ts` | 554 |
  | `CalendarConnect.tsx` | 337 |
  | `AccountWatch.tsx` | 263 |
  | `mailWatchLogic.ts` | 331 |

- **Public repos.** Fixtures use `example.com`/`example.org`, the password `secret`, and recorded ISPDB XML with hosts rewritten. No real address, credential or mail text.
- **Commands.**
  - **PIM.** Build with `dotnet build calendar-mcp.slnx -c Release`, then test with `dotnet test calendar-mcp.slnx -c Release --no-build --filter <Class>`.
    - The integration tests need `PIM_TEST_GREENMAIL=localhost:3143`, `PIM_TEST_GREENMAIL_SMTP=localhost:3025` (new) and `PIM_TEST_DOVECOT=localhost:31143`, plus `PIM_TEST_GREENMAIL_AUTH_SMTP=localhost:3465`. That last one (new) is a second GreenMail started without `-Dgreenmail.auth.disabled` and with a configured user `secret`, after reading GreenMail's docs for the user option.
    - Start them with the `docker run` lines in `.github/workflows/ci.yml`, whose readiness loop gains 3025 and 3465.
  - **Go, in WSL:** `wsl -e bash -lc 'cd /mnt/d/Aura && export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH && go vet ./... && go build ./... && go test -race ./internal/<pkg>/... ./cmd/aura/'`.
  - **Web, on Windows:** `cd web; npx vitest run <file>; npx vitest run src/i18n/__tests__; npm run lint; npm run lint:contract; npm run format:check; npm run typecheck; npm run mutation:contract`, and `npm run test` (its coverage floor) before each web commit.
- **`AURA-FORK.md`** gets a line for each new route, behaviour change, dependency and rule change, in the commit that makes it.
- **Commits.** One atomic commit per task. Aura commits end `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **The stored password reaches a server it was not meant for, or reaches one in clear.** Two paths lead there:
   - a PATCH that moves the host without the password;
   - a STARTTLS-less server that the old `Auto` connection would accept.

   Pinned by:
   - `Patch_NewHostWithoutPassword_Is400_AndNothingConnects` (P3);
   - `Verify_StartTlsMissing_IsTlsFailed` and `Create_SecurityNone_Is400` (P1, P3).
2. **A repair that does not take.** The pool keeps the old session, or a PATCH overwrites a newer write. Pinned by `Pool_ConfigChange_ReopensWithTheNewConfig` (P3, on Dovecot with the real service) and `Patch_StoredConfigChangedMeanwhile_Is409` (P3).
3. **A password's hash or plaintext left where a backup or a log keeps it.** Pinned by:
   - `TestTheFingerprintHidesThePassword` (A2);
   - `ImapAccountConfig_ToStringHidesThePassword` (P1);
   - `StartupPass_IsAtomic` and `StartupPass_WithANewKeyRing_LeavesForeignCiphertextAlone` (P2).
4. **A member probes the internal network.** Through verify, create or PATCH, or through an ISP autoconfig URL. Pinned by:
   - `Verify_PrivateAddressForAMember_IsRefused` and `Verify_PrivateAddressForAnAdmin_Connects` (P1/P3);
   - `Isp_RefusesAPrivateAddress` (P5);
   - `TestTheClientCannotSendThePrivateHostsHeader` (A2).
5. **«Prova di nuovo» says the account works while the watch keeps failing.** Pinned by:
   - `Verify_RunsTheWatchsFirstRead` (P1);
   - `TestRepairedLeavesAFailurePauseForAnotherCauseAlone` (A1);
   - `ReadingStillFailsAfterAWorkingLogin` (W4).

---

## File structure

| Where | File | Responsibility |
|---|---|---|
| PIM Core | `Providers/ImapConnections.cs` (new) | Open an IMAP/SMTP client from a config, with its TLS mode and an optional guarded socket; the only place that does |
| PIM Core | `Providers/HostGuard.cs` (new) | Resolve once, refuse private addresses unless allowed, return a connected `Socket` |
| PIM Core | `Providers/ImapAccountVerifier.cs` (new) | `IImapAccountVerifier`, `VerifyOutcome`, `VerifyFailure`, `VerifyReasons` (stage-aware) |
| PIM Core | `Providers/ImapProviderService.Support.cs` | `ImapAccountConfig.From` (defaults, security, unprotect) shared by service and verifier; `PrintMembers` hides the password |
| PIM Core | `Providers/ImapProviderService.cs` | pool keyed case-insensitively; a pooled connection remembers its config |
| PIM Core | `Security/ProviderSecrets.cs` (new) | `ProtectIncoming`; which key is a secret |
| PIM Core | `Discovery/*` (new) | preset table, autoconfig/ISPDB client and parser, MX lookup, the ordered `MailDiscovery` |
| PIM Core | `Changes/ChangesService.cs` | the failure log carries the server's response line (A8) |
| PIM Auth | `AccountConfigurationService.cs` | atomic `WriteConfigAsync`; `ProtectStoredPasswordsAsync` (in place, tenant-less); `MergeProviderConfigAsync` (compare-and-write) |
| PIM HttpServer | `Admin/AccountRepairEndpoints.cs`, `Admin/VerifyResults.cs`, `Admin/DiscoverEndpoint.cs`, `Admin/ProtectStoredPasswordsService.cs`, `Admin/VerifyThrottle.cs` (new) | routes, 422/429 shapes, startup pass, per-account limit |
| PIM Tests | `Admin/AdminPipeline.cs` (new), `Helpers/SubjectFromToken.cs` (extracted from `ChangesPipeline`) | an in-process admin harness with file-backed config |
| Aura Go | `internal/pimwatch/service_repair.go` (new), `internal/pimwatch/fakes_test.go` (moved, untagged) | `Repaired` |
| Aura Go | `internal/agui/connect_pim_account_api.go` (new), `internal/agui/idempotency_pim.go` (new) | the relays, clients, limiter, private-hosts header; the secret-hiding normalizer |
| Aura web | `web/src/api/json.ts` | `HttpError.body` |
| Aura web | `web/src/shell/modes.ts`, `web/src/AppShell.tsx` | Settings is a member surface |
| Aura web | `web/src/settings/mail/*` (new) | section, add flow, pure helpers |
| Aura web | `web/src/governance/ImapRepair.tsx` (new) | «Prova di nuovo» / «Cambia password» |
| Aura web | `web/src/i18n/resources.settings.mail.ts` (new, registered inside `resources.settings.ts`) | copy, it + en |

---

## PIM fork (`D:/tmp/aura-pim-mcp`)

### Task P1: The check that a mailbox answers, over TLS, to the watch's first read

**Files:**
- **Create:**
  - `src/CalendarMcp.Core/Providers/ImapConnections.cs`
  - `src/CalendarMcp.Core/Providers/HostGuard.cs`
  - `src/CalendarMcp.Core/Providers/ImapAccountVerifier.cs`
- **Modify:**
  - `ImapProviderService.cs`: remove the openers; `ResolveConfigAsync` calls `ImapAccountConfig.From`.
  - `ImapProviderService.Support.cs:199`: the record gains `ImapSecurity`, `SmtpSecurity` and `static From(...)`, and its `PrintMembers` omits `Password`.
  - `ServiceCollectionExtensions.cs`: register `IImapAccountVerifier` and `HostGuard` as singletons.
  - `.github/workflows/ci.yml`:
    - GreenMail gets `-p 3025:3025`;
    - a second GreenMail with auth, on `3465`;
    - env `PIM_TEST_GREENMAIL_SMTP` and `PIM_TEST_GREENMAIL_AUTH_SMTP`;
    - the readiness loop covers `3025 3143 3465 31143`.
  - `src/CalendarMcp.Tests/Changes/ImapMailbox.cs`: `ImapServers.GreenMailSmtp` and `GreenMailAuthSmtp`.
  - `AURA-FORK.md`: the resolution rule's opener moves from `ImapProviderService.cs` to `ImapConnections.cs`, and the TLS modes are recorded.
- **Test:**
  - `src/CalendarMcp.Tests/Providers/ImapAccountVerifierTests.cs`
  - `src/CalendarMcp.Tests/Providers/HostGuardTests.cs`

**Interfaces:**
- **Produces:**
  - `internal static class ImapConnections`:
    - `Task<ImapClient> OpenImapAsync(ImapProviderService.ImapAccountConfig cfg, Socket? socket, CancellationToken ct)`
    - `Task<SmtpClient> OpenSmtpAsync(ImapProviderService.ImapAccountConfig cfg, Socket? socket, CancellationToken ct)`

    `ImapSecurity`/`SmtpSecurity` map as `"ssl"` → `SslOnConnect`, `"starttls"` → `StartTls`, `"none"` → `None`, and null → `Auto` (legacy). A non-null socket goes to MailKit's `ConnectAsync(Socket, host, port, options, ct)`. Read MailKit's `IMailService.ConnectAsync(Socket…)` doc first and cite it.
  - `public sealed class HostGuard`, with `Task<Socket> ConnectAsync(string host, int port, bool allowPrivate, CancellationToken ct)`. It throws `PrivateAddressException : IOException` when any resolved address is private and `allowPrivate` is false.
  - `public sealed record VerifyFailure(string Step, string Reason)`, and `public sealed record VerifyOutcome(VerifyFailure? Blocking, string? SmtpWarning)`.
  - `public static class VerifyReasons`, with `VerifyFailure From(string step, string stage, Exception failure)`. `stage` is `connect`, `auth` or `read`.
  - `public interface IImapAccountVerifier`, with `Task<VerifyOutcome> VerifyAsync(string accountId, IReadOnlyDictionary<string,string> providerConfig, bool allowPrivate, CancellationToken ct)`.

- [ ] **Step 1: Write the failing tests.** Use the exception constructors that compile today (`ProviderFailuresTests.cs:40,72-73`).

  ```csharp
  // VerifyReasons, table-driven, MailKit.Security.AuthenticationException fully qualified:
  ("imap","auth",   new AuthenticationException("bad password"))            -> ("imap","auth_failed")
  ("imap","auth",   new AuthenticationException("The LOGIN command is disabled.")) -> ("imap","tls_failed")
  ("imap","auth",   new ImapProtocolException("BYE"))                         -> ("imap","auth_failed")
  ("imap","connect",new NotSupportedException("…does not support the STARTTLS extension")) -> ("imap","tls_failed")
  ("imap","connect",new SslHandshakeException("handshake failed", new IOException("reset"))) -> ("imap","tls_failed")
  ("imap","connect",new SocketException())  /  new TimeoutException()  /  new IOException("Failed to resolve host") -> ("imap","unreachable")
  ("imap","connect",new PrivateAddressException(..))                         -> ("imap","unreachable")
  ("imap","read",   new ImapCommandException(ImapCommandResponse.No, "EXAMINE failed")) -> ("imap","mailbox_refused")
  ("imap","read",   new CryptographicException())  // lost key ring, raised by From  -> ("imap","auth_failed")
  ("smtp","auth",   new AuthenticationException("x", new SmtpCommandException(SmtpErrorCode.MessageNotAccepted, SmtpStatusCode.AuthenticationInvalidCredentials, "535"))) -> ("smtp","smtp_auth_failed")
  ("smtp","auth",   same with SmtpStatusCode 454)                             -> ("smtp","smtp_unreachable")
  ("smtp","auth",   new NotSupportedException("No compatible authentication mechanisms found.")) -> ("smtp","smtp_auth_failed")
  ("smtp","connect",new SocketException())                                    -> ("smtp","smtp_unreachable")
  ```

  Integration tests against the CI servers, with `imapSecurity=none` and `smtpSecurity=none` unless stated otherwise:
  - `Verify_RightPassword_Passes`: Dovecot IMAP plus GreenMail SMTP gives `Blocking == null` and `SmtpWarning == null`.
  - `Verify_WrongPassword_IsAuthFailed_AndLogsTheServersLine`: Dovecot; `CapturingLogs` holds Dovecot's `NO` text and no `secret`.
  - `Verify_StartTlsMissing_IsTlsFailed`: Dovecot with `imapSecurity=starttls`.
  - `Verify_ClosedPort_IsUnreachable`.
  - `Verify_SmtpDown_PassesWithAWarning`: Dovecot IMAP and an unused SMTP port give `Blocking == null` and `SmtpWarning == "smtp_unreachable"`.
  - `Verify_SmtpWrongPassword_Blocks`: the GreenMail with auth gives `("smtp","smtp_auth_failed")`.
  - `Verify_SmtpWithoutAuth_IsSmtpAuthFailed`: GreenMail on 3025 never authenticates (`!client.IsAuthenticated` with a password set).
  - `Verify_RunsTheWatchsFirstRead`: a mailbox whose `inboxFolder` does not exist gives `("imap","mailbox_refused")`, from `cfg.InboxFolder` and not `client.Inbox`.
  - `Verify_StoredEncryptedPassword_LogsIn`: an `ENC:` password from an `EphemeralDataProtectionProvider` against Dovecot.
  - `ImapAccountConfig_ToStringHidesThePassword`.
  - `HostGuard`:
    - `Refuses127001AndRfc1918AndMappedV6` when not allowed;
    - `AllowsThemWhenAllowed`;
    - `ConnectsThePublicAddressItChecked`, using a test resolver seam.

- [ ] **Step 2: Run the tests and see them fail**, with `dotnet test … --filter "ImapAccountVerifierTests|HostGuardTests"`.
- [ ] **Step 3: Implement.**
  - **The IMAP step** has its own 12 s token:
    1. `HostGuard.ConnectAsync`, then `OpenImapAsync(cfg, socket, …)`.
    2. Open `cfg.InboxFolder` read-only.
    3. Read `UidNext`/`UidValidity` and one `UID SEARCH` over the newest window: the body of the watch's `StartAsync`, extracted and shared, not copied.
  - **The SMTP step** has its own 8 s token:
    1. Guarded open.
    2. If a password is set and `!IsAuthenticated`, the step fails `smtp_auth_failed`.
    3. `smtp_unreachable` and `tls_failed` become `SmtpWarning`.
  - **Cancellation:** a cancelled caller token is rethrown; an expired step token becomes `unreachable`.
  - **Logging:** at Warning, log the account id, step, stage and exception type. Then log `ImapCommandException.ResponseText`, the `SmtpCommandException.StatusCode` plus `Message`, or `Message` for an `AuthenticationException`, CR/LF-stripped and cut at 200.
  - **Limit:** `ImapProviderService.cs` must end under 575 lines.
- [ ] **Step 4: Run this class, `ImapChangesTests`, `ImapCondStoreTests`, `ImapProviderServiceTests` and `ProviderFailuresTests`.** All must pass.
- [ ] **Step 5: Commit:** `"Check that an IMAP mailbox answers, over TLS, the way the watch reads it"`.

### Task P2: Passwords are stored encrypted, and the config file is written atomically

**Files:**
- **Create:**
  - `src/CalendarMcp.Core/Security/ProviderSecrets.cs`
  - `src/CalendarMcp.HttpServer/Admin/ProtectStoredPasswordsService.cs`
- **Modify:**
  - `src/CalendarMcp.Auth/AccountConfigurationService.cs`, together with `IAccountConfigurationService`:
    - `WriteConfigAsync` writes `appsettings.json.tmp` in the same directory, then `File.Move(tmp, path, overwrite: true)`;
    - add `ProtectStoredPasswordsAsync`.
  - `AdminEndpoints.cs`: `CreateAccount` and `UpdateAccount` (the upstream `PUT`) store through `ProtectIncoming`.
  - `Program.cs`: `AddHostedService<ProtectStoredPasswordsService>()`.
  - `AURA-FORK.md`. Record that this restores upstream's own admin behaviour (`AccountFormModels.cs:216-220` at upstream `3b2762b`). Record where the key ring is (`/app/data/keys` on `aura-pim-data`), what `ENC:` protects against (spec A10), and "never delete `keys/`".
- **Test:**
  - `src/CalendarMcp.Tests/Security/ProviderSecretsTests.cs`
  - `src/CalendarMcp.Tests/Auth/AccountConfigurationServiceTests.cs` (already `[DoNotParallelize]`)

**Interfaces:**
- **Produces:**
  - `public static class ProviderSecrets`:
    - `public static bool IsSecretKey(string key)`: `password`, compared `OrdinalIgnoreCase`.
    - `public static Dictionary<string,string> ProtectIncoming(IReadOnlyDictionary<string,string> config, PasswordProtector protector)`. It returns an `OrdinalIgnoreCase` dictionary. A non-empty secret is always protected, `ENC:` prefix or not.
  - `Task<int> ProtectStoredPasswordsAsync(PasswordProtector protector, CancellationToken ct)`. It runs under `_fileLock`, on the `JsonNode` tree from `ReadConfigAsync`. It replaces only `ProviderConfig`/`providerConfig` → `<password in any case>` on nodes whose value is non-empty and not `IsProtected`. It never calls `ParseAccounts`, and writes only when the count is above 0.

- [ ] **Step 1: Write the failing tests.**
  - `ProtectIncoming_EncryptsAPlainPassword`
  - `ProtectIncoming_EncryptsWhateverTheKeysCase`
  - `ProtectIncoming_EncryptsAValueThatStartsWithENC`
  - `ProtectIncoming_CopiesOtherKeys`
  - `ProtectStored_RewritesOnlyPlainPasswords`: the file holds one plain imap account, one `ENC:` account, one DAV account with `"Password"`, one without a password, and one legacy node without `TenantId`. The pass returns 2, and every other node is byte-identical after a JSON normalise.
  - `ProtectStored_LeavesEncryptedValuesAlone`: a second run returns 0.
  - `StartupPass_WithANewKeyRing_LeavesForeignCiphertextAlone`
  - `StartupPass_IsAtomic`: the temp file is gone and the original is intact when the move throws, through an injected failing writer seam.
  - `StartupPass_AFailureDoesNotStopTheHost`
  - `KeyRing_LivesUnderTheDataDirectory`: `GetDataProtectionKeysDirectory()` starts with `GetDataDirectory()`.
- [ ] **Step 2: Run the tests and see them fail.**
- [ ] **Step 3: Implement.** The hosted service logs `Protected {Count} stored passwords` and never a value. An exception is logged once and swallowed.
- [ ] **Step 4: Run both classes and every `Auth` test.** All must pass.
- [ ] **Step 5: Commit:** `"Store account passwords encrypted and write the account file atomically"`.

### Task P3: Verify on create, verify on demand, change part of an IMAP account, and a pool that follows the change

**Files:**
- **Create:**
  - `src/CalendarMcp.HttpServer/Admin/AccountRepairEndpoints.cs`
  - `src/CalendarMcp.HttpServer/Admin/VerifyResults.cs`
  - `src/CalendarMcp.HttpServer/Admin/VerifyThrottle.cs`
  - `src/CalendarMcp.Tests/Admin/AdminPipeline.cs`
  - `src/CalendarMcp.Tests/Helpers/SubjectFromToken.cs`, extracted from `ChangesPipeline.cs` with `ChangesPipeline` changed to use it
- **Modify:**
  - `AdminEndpoints.cs`:
    - `CreateAccount` answers `409` from `AccountExistsAsync` on the tenant-prefixed id before anything else.
    - For `imap`, it then defaults or validates the security keys (refusing `none` with `tls_required`), applies the throttle, verifies with `allowPrivate` taken from the `X-Aura-Allow-Private-Hosts` header, and stores through `ProtectIncoming`.
    - It adds `smtpWarning` to the 201 body when set.
    - `DescribePermissions` becomes `internal`.
    - `MapAdminEndpoints` calls `admin.MapAccountRepairEndpoints()`.
  - `AccountConfigurationService.cs`: `MergeProviderConfigAsync`.
  - `ImapProviderService.cs`:
    - `_imapConnections` is created with `StringComparer.OrdinalIgnoreCase`.
    - `PooledImapConnection` records the `(ImapHost, ImapPort, ImapSecurity, Username, Password)` it opened with.
    - `AcquireClientAsync` evicts under `conn.Gate` when the config it is handed differs.
  - `AURA-FORK.md`.
- **Test:**
  - `src/CalendarMcp.Tests/Admin/AccountRepairEndpointsTests.cs`, `[DoNotParallelize]`.
  - `src/CalendarMcp.Tests/Providers/ImapPoolTests.cs`, on Dovecot with the real service.

**Interfaces:**
- **Consumes:** `IImapAccountVerifier`, `VerifyOutcome` (P1); `ProviderSecrets` (P2).
- **Produces:**
  - `VerifyResults.Failed(VerifyFailure f) → IResult`, returning the `422`. `VerifyResults.TooMany() → IResult`, returning the `429`.
  - `VerifyThrottle.TryEnter(string accountId) → bool`: one per 30 s per account id, with a `TimeProvider` seam.
  - `POST /admin/accounts/{accountId}/verify` → `200 {"ok":true,"smtpWarning"?}` | `422` | `404` | `429` | `400 not_verifiable`.
  - `PATCH /admin/accounts/{accountId}/provider-config`, body `{"providerConfig":{…}}`, applying the PATCH rules of Global Constraints:
    1. Merge.
    2. Validate with `TenantProviderConfig.Validate`.
    3. Throttle.
    4. Verify the merged plaintext view. A kept `ENC:` password is unprotected by `ImapAccountConfig.From`.
    5. `MergeProviderConfigAsync(id, expectedStored, ProtectIncoming(incoming) ∪ kept)`.
    6. `AccountConfigurationReload.Apply`.

    Answers: `200` (the `UpdateAccount` shape plus `smtpWarning`), `400`, `404`, `409`, `422` or `429`.
  - `Task MergeProviderConfigAsync(string accountId, IReadOnlyDictionary<string,string> expectedStored, IReadOnlyDictionary<string,string> next, CancellationToken ct)`. Under `_fileLock` it re-reads the owned node, throws `ConcurrencyException` when its provider config differs from `expectedStored`, and replaces only `ProviderConfig`.

- [ ] **Step 1: Write `AdminPipeline`.**
  - It is built from:
    - `ChangesRegistrationTests.cs:44-57` DI (`AddCalendarMcpCore()`, the real `AccountConfigurationService`, `DeviceCodeAuthManager`, `GoogleOAuthManager`);
    - a fake `IImapAccountVerifier` and a Rocks `IImapProviderService`, registered after;
    - `SubjectFromToken` plus `AdminAuthMiddleware`;
    - a temp `CALENDAR_MCP_CONFIG` holding a seeded `appsettings.json`;
    - `MapAdminEndpoints()`.
  - Helpers: GET, POST, PATCH and PUT, each with an optional `X-Aura-Allow-Private-Hosts`.
- [ ] **Step 2: Write the failing tests.**
  - `Create_Imap_WrongPassword_Is422_AndStoresNothing`
  - `Create_Imap_RightPassword_StoresItEncrypted_WithItsSecurity`
  - `Create_SecurityNone_Is400`
  - `Create_SmtpUnreachable_Is201WithAWarning`
  - `Create_Ics_IsNotVerified`
  - `Create_DuplicateId_Is409_WithoutVerifying`
  - `Create_PasswordStartingWithENC_IsStillEncrypted`
  - `Create_PassesThePrivateHostsHeaderToTheVerifier`: the header present gives `allowPrivate == true`, and absent gives `false`.
  - `Verify_PassingAccount_IsOk`
  - `Verify_FailingAccount_Is422WithExactlyErrorStepReason`
  - `Verify_OnAForeignAccount_IsNotFound`
  - `Verify_UnreadableStoredPassword_IsAuthFailed`, using the real `ImapAccountConfig.From` path inside a fake that calls it
  - `Verify_TwiceWithin30s_Is429_WithoutConnecting`
  - `Patch_ChangesOnlyThePassword`: hosts, security, `DisplayName`, `Domains`, `Enabled`, `Priority` and `Permissions` stay unchanged.
  - `Patch_KeepsTheStoredCiphertextWhenNoPasswordIsSent`: the stored value is byte-identical, and the fake got the `ENC:` value through `From` as plaintext.
  - `Patch_NewHostWithoutPassword_Is400_AndNothingConnects`
  - `Patch_NewUsername_Is400`
  - `Patch_DuplicateKeyCase_Is400`
  - `Patch_OnAGoogleAccount_Is400`
  - `Patch_FailingVerify_Is422_AndTheFileIsUnchanged`
  - `Patch_OnAForeignAccount_IsNotFound`
  - `Patch_StoredConfigChangedMeanwhile_Is409`: the fake verifier changes the file while it runs.
  - `Patch_PasswordWithATrailingSpace_IsKeptAsTyped`
  - `Put_StoresThePasswordEncrypted`
  - `Pool_ConfigChange_ReopensWithTheNewConfig`, on Dovecot with the real `ImapProviderService`:
    1. Open a pooled connection with user A.
    2. Switch the registry to a wrong password.
    3. The next `ReadAsync` fails `AuthenticationException` rather than reusing the session.
- [ ] **Step 3: Run the tests and see them fail.**
- [ ] **Step 4: Implement.** Order inside the PATCH: write, then `Reload`. The pool needs no explicit evict because of the config comparison.
- [ ] **Step 5: Run this class, every `Admin` test and `ImapPoolTests`.** All must pass.
- [ ] **Step 6: Commit:** `"Verify an IMAP account when it is saved, change part of one safely, and let the pool follow"`.

### Task P4: The watch's failure log carries the server's line

**Files:**
- **Modify:**
  - `src/CalendarMcp.Core/Changes/ChangesService.cs:118-131`
  - `AURA-FORK.md`: change 8's rule "neither is a provider's exception message" is amended for IMAP response text, citing spec A8.
- **Test:** `src/CalendarMcp.Tests/Changes/ChangesServiceTests.cs`, or the existing logging test.

- [ ] **Step 1: Write the failing test.** `TransientImapFailure_LogsTheServersLineWithoutCrLf`, using `RefusingImapProxy`. It is pointed at the inbox's EXAMINE, or given a new constructor argument for the folder, so the log holds the proxy's `NO` text on one line.
- [ ] **Step 2: Run it and see it fail.**
- [ ] **Step 3: Implement.** For `ImapCommandException`, log `ResponseText`; for `ImapProtocolException`, log `Message`. Strip CR/LF and cut at 200. Other providers are unchanged, and the cursor is never logged.
- [ ] **Step 4: Run `ChangesService*` and `ImapChangesTests`.** All must pass.
- [ ] **Step 5: Commit:** `"Log the IMAP server's line when the watch's read fails"`.

### Task P5: Find the servers from an address

**Files:**
- **Create:**
  - `src/CalendarMcp.Core/Discovery/MailDiscovery.cs`
  - `Discovery/AutoconfigClient.cs`: ISP autoconfig and ISPDB fetch, with the guarded `SocketsHttpHandler.ConnectCallback` reusing `HostGuard` with `allowPrivate: false`.
  - `Discovery/AutoconfigParser.cs`
  - `Discovery/MxLookup.cs`
  - `Discovery/presets.json`, as an embedded resource.
  - `src/CalendarMcp.HttpServer/Admin/DiscoverEndpoint.cs`
- **Modify:**
  - `CalendarMcp.Core.csproj`: `DnsClient` 1.8.0, `Microsoft.Extensions.Caching.Memory` 10.0.13, and the embedded resource.
  - `ServiceCollectionExtensions.cs`:
    - `AddMemoryCache(o => o.SizeLimit = 1000)`;
    - a singleton `LookupClient(new LookupClientOptions { Timeout = TimeSpan.FromSeconds(2), Retries = 1 })`;
    - `IMxLookup`, `IAutoconfigClient` (a typed `HttpClient`, 4 s, `AllowAutoRedirect = false`) and `MailDiscovery` as singletons.
  - `AdminEndpoints.cs`: `admin.MapDiscoverEndpoint()`.
  - `AURA-FORK.md`: both dependencies, noting that DnsClient can go on .NET 11 (`Dns.ResolveMxAsync`).
- **Test:**
  - `src/CalendarMcp.Tests/Discovery/MailDiscoveryTests.cs`
  - `AutoconfigParserTests.cs`
  - `src/CalendarMcp.Tests/TestData/Autoconfig/*.xml`: recorded answers (the Yahoo two-server shape, the outlook.com OAuth-only shape, a STARTTLS 587 shape, a `plain` shape and a `%EMAILDOMAIN%` hostname shape) with hosts rewritten to `example.org`.

**Interfaces:**
- **Produces:**
  - `public sealed record DiscoveryResult(string Kind, string? Source = null, string? ImapHost = null, int? ImapPort = null, string? ImapSecurity = null, string? SmtpHost = null, int? SmtpPort = null, string? SmtpSecurity = null, string? Username = null, AppPasswordHint? AppPassword = null, bool? PasswordFallback = null)`, serialised camelCase with nulls omitted.
  - `public sealed record AppPasswordHint(bool Required, string? HelpUrl)`.
  - `public sealed class MailDiscovery`, with `Task<DiscoveryResult> DiscoverAsync(string email, CancellationToken ct)`. Its seams:
    - `IAutoconfigClient.FetchIspAsync(string domain, CancellationToken)` → `string?` (XML)
    - `IAutoconfigClient.FetchIspdbAsync(string domain, CancellationToken)` → `string?`
    - `IMxLookup.PreferredMxHostAsync(string domain, CancellationToken)` → `string?`
  - `public static class AutoconfigParser`, with `DiscoveryResult? Parse(string xml, string localPart, string domain)`, which applies the "usable server" rule.
  - `POST /admin/discover` `{ "email": "…" }` → `200 DiscoveryResult` | `400 {"error":"invalid_email"}`. The address is parsed with `MailboxAddress.TryParse`, lowercased, IDN-mapped with `IdnMapping.GetAscii`, and its domain must pass `Uri.CheckHostName == Dns` with at least one dot. A `+tag` is stripped from the `address`-form username.

- [ ] **Step 1: Write the preset table.**
  - Each entry has `{ domains[], mxSuffixes[]?, kind, imapHost?, imapPort?, imapSecurity?, smtpHost?, smtpPort?, smtpSecurity?, username: "address"|"localpart", appPassword?, passwordFallback?, source: "<provider page URL>" }`.
  - Cover gmail.com and googlemail.com (`passwordFallback: true`, with IMAP/SMTP hosts and the app-password rule), outlook.com/hotmail.*/live.*/msn.com (no fallback), yahoo.*, icloud.com/me.com, libero.it, virgilio.it, tiscali.it, aruba.it, fastwebnet.it, tim.it, alice.it, and ionos.it/ionos.com (with `mxSuffixes` so a hosted domain reaches it).
  - Copy every value from the provider's own help page, cite its URL in `source`, and drop what the page does not support.
  - The ISPDB `documentation` element is not used: it is informational only (format wiki).
- [ ] **Step 2: Write the failing tests.**
  - `Preset_Gmail_IsGoogleWithAPasswordFallback`
  - `Preset_OutlookCom_HasNoPasswordFallback`
  - `Preset_Libero_IsImapWithItsHostsAndSecurity`
  - `Isp_AutoconfigOfTheDomain_WinsOverIspdb`
  - `Isp_NeverSendsTheAddress`
  - `Isp_RefusesAPrivateAddress`
  - `Isp_DoesNotFollowRedirects`
  - `Ispdb_TakesTheFirstUsableServer`
  - `Ispdb_OAuthOnlyIsAMiss`
  - `Ispdb_PlainIsAMiss`
  - `Ispdb_SubstitutesTheDomainInTheHostname`
  - `Ispdb_IgnoresAServerWithoutAValidPort`
  - `Ispdb_IgnoresAServerWithAnInvalidHostname`
  - `Ispdb_StartTls587_KeepsStarttls`
  - `Mx_GoogleHosted_IsGoogle`
  - `Mx_OutlookConsumer_IsOutlookCom` (`msn-com.olc.protection.outlook.com`)
  - `Mx_Microsoft365_IsMicrosoft365` (`*.mail.protection.outlook.com`)
  - `Mx_UnderAPresetDomain_IsThatPreset` (`mx00.ionos.it`)
  - `Mx_SameBaseAsTheDomain_StopsAtUnknown`
  - `Mx_TriesSuffixesFromLongest`
  - `Discover_FallsThroughOnTimeout`, with an injected `TimeProvider` and budget, so no wall clock is involved
  - `Discover_CachesHitsAndMisses`
  - `Discover_UppercaseAndIdnDomain_AreNormalised`
  - `Discover_PlusTagIsStrippedFromTheUsername`
  - `Discover_BadAddress_Is400`
- [ ] **Step 3: Run the tests and see them fail.**
- [ ] **Step 4: Implement** in the order of Global Constraints.
- [ ] **Step 5: Run the classes.** All must pass.
- [ ] **Step 6: Commit:** `"Find a mailbox's servers from its address, the way Thunderbird does"`.
- [ ] **Step 7: Open the PR to `main` and get CI green, but do not merge yet.** The merge waits for L1 step 1: once merged, `aura-publish-image.yml` publishes `:latest`, `:sidecar` and `:<sha>`. Every host follows those tags (`compose.yaml:1358-1359`, updater `deploy/aura-image-update.sh:51,577-579`). An old Aura's 8 s relay would then 502 on a slow check.

---

## Aura

### Task A1: A repaired account wakes its watch

**Files:**
- **Create:**
  - `internal/pimwatch/service_repair.go`
  - `internal/pimwatch/fakes_test.go`, untagged. `fakeTasks` and `fakeRoutes` move here from `service_integration_test.go`, and the integration file uses them from here.
- **Modify:**
  - `internal/pimwatch/service.go`: `Tasks` gains `RunNow(ctx, taskID string) error`.
  - `cmd/aura/serve_pimwatch.go:262-274`: `pimTaskStore` gains `RunTaskNow(ctx context.Context, id string) error`, and `pimWatchTasks.RunNow` calls it.
  - `cmd/aura/serve_pimwatch_test.go:213`: `stubTaskStore.RunTaskNow`.
- **Test:**
  - `internal/pimwatch/service_repair_test.go`
  - `cmd/aura/serve_pimwatch_test.go` (`TestPimWatchTasksRunNowReachesTheStore`)

**Interfaces:**
- **Produces:** `func (s *Service) Repaired(ctx context.Context, identityID, accountID string) error`, which reads the watch and the account's cursors.
  - If there is no watch, it does nothing.
  - If the watch is paused with `PausedReason == cron.PausedByFailures` and some cursor's `LastError` is `provider_auth`, `provider_transient` or a `pim_*` code, it calls `Resume` then `RunNow`.
  - If the watch is paused for anything else, it does nothing.
  - If the watch is active, it calls `RunNow`.

- [ ] **Step 1: Write the failing tests.**
  - `TestRepairedResumesAWatchPausedForACredentialFailure`
  - `TestRepairedLeavesAFailurePauseForAnotherCauseAlone` (`permission_denied`)
  - `TestRepairedRunsAnActiveWatchNow`
  - `TestRepairedLeavesAnOperatorPauseAlone`
  - `TestRepairedWithNoWatchDoesNothing`
- [ ] **Step 2: Run `go test -race ./internal/pimwatch/ -run Repaired` and see the tests fail.**
- [ ] **Step 3: Implement.** Record in the function's comment that a repair landing while a run is in flight takes effect at the next interval: the scheduler's skip path reschedules.
- [ ] **Step 4: Run `go vet`, `go build`, then `go test -race ./internal/pimwatch/... ./cmd/aura/`.** All must pass.
- [ ] **Step 5: Commit:** `"A repaired mail account wakes its watch"`.

### Task A2: The cockpit relays discovery, verification and the partial change, safely

**Files:**
- **Create:**
  - `internal/agui/connect_pim_account_api.go`
  - `internal/agui/idempotency_pim.go`
- **Modify:**
  - `internal/agui/connect_pim_api.go`:
    - three route lines;
    - `handlePIMCreateAccount` forwards through `pimVerifyClient`, sets the private-hosts header and goes through the limiter;
    - every `"/admin/accounts/"+id` becomes `url.PathEscape(id)`, fixed on touch.
  - `internal/agui/idempotency_http.go`:
    - `discover` goes in `readOnlyPOST`;
    - `verify` gets `httpMutationMeta("pim_account_verify")`;
    - create and PATCH get the new secret-hiding normalizer.
  - `internal/agui/recovery_hash.go`: a sibling `DeriveIdempotencySecretKey(secretHex string) ([]byte, error)` with info `"aura-idempotency-secret-field-v1"`, the `DeriveResetTokenPepper` pattern.
  - `internal/agui/pim_watch_api.go`: `mailWatchService` gains `Repaired`.
  - `cmd/aura/serve_webui_routes.go` and `cmd/aura/serve_webui.go`: three constants, each mounted behind `RequireCapability(…, governanceWriteCapability)`. Wire the key from the same secret the reset pepper uses.
- **Test:**
  - `internal/agui/connect_pim_account_api_test.go`
  - `internal/agui/idempotency_pim_test.go`
  - `internal/agui/governance_write_auth_sweep_test.go` (`connectGatedRoutes()` gains the three)
  - `cmd/aura/pim_account_routes_test.go`, on the pattern of `pim_provider_routes_test.go:31-60`
  - `fakeWatchService` gains `Repaired`, in `pim_watch_api_test.go` and in `pim_watch_decide_api_test.go`

**Interfaces:**
- **Consumes:** `Service.Repaired` (A1); the PIM routes (P3, P5).
- **Produces:**
  - Routes: `POST /api/connect/pim/discover`, `POST /api/connect/pim/accounts/{id}/verify` and `PATCH /api/connect/pim/accounts/{id}/provider-config`.
  - Package vars `pimDiscoverClient` (15 s) and `pimVerifyClient` (25 s). Tests shorten them.
  - `pimAttemptLimiter`: per identity, 10 a minute, with a clock seam. It returns `429 {"error":"too_many_attempts"}` before dialling.
  - `const pimAllowPrivateHostsHeader = "X-Aura-Allow-Private-Hosts"`. It is set to `true` only when the principal holds `identity.create`, and is always built server-side.
  - The PATCH relay drops `pimprovider.IsOwnedKey` keys before forwarding.
  - `normalizePIMSecretMutation(key []byte) func(*http.Request, mutationRouteMeta) (httpMutationIntent, error)`. It parses the JSON body and replaces each `providerConfig` value whose key `strings.EqualFold`s `password` with `base64(HMAC-SHA256(key, value))`. The rest of the intent is unchanged.

- [ ] **Step 1: Write the failing tests** against an `httptest` sidecar.
  - `TestDiscoverForwardsTheAddress`
  - `TestCreateUsesTheLongClient`, with the sidecar delayed beyond a shortened `pimClient` and within `pimVerifyClient`
  - `TestVerifyPassesA422Through`, byte-identical
  - `TestPatchForwardsAsPatch_WithoutOwnedKeys`
  - `TestTheAdminSendsThePrivateHostsHeader_AMemberDoesNot`
  - `TestTheClientCannotSendThePrivateHostsHeader`
  - `TestTheEleventhAttemptInAMinuteIs429`
  - `TestASuccessfulVerifyWakesTheWatch`
  - `TestAFailedPatchLeavesTheWatchAlone`
  - `TestAnEscapedIdStaysOneSegment`
  - `TestTheFingerprintHidesThePassword`: two bodies differing only in password give different fingerprints, and neither equals the plain SHA-256 of the body.
  - `TestEveryRegisteredUnsafeHTTPRouteIsClassified`, the existing test, must stay green.
  - The sweep's three new entries.
  - The `cmd/aura` member test.
- [ ] **Step 2: Run the tests and see them fail.**
- [ ] **Step 3: Implement.** On a 2xx with `s.mailWatch != nil`, call `Repaired` under `context.WithoutCancel` and log a failure with `sanitizeErr`, as `handlePIMDeleteAccount` does.
- [ ] **Step 4: Run `go vet`, `go build`, then `go test -race ./internal/agui/ ./cmd/aura/`.** All must pass.
- [ ] **Step 5: Commit:** `"Relay mailbox discovery, verification and repair to the PIM, and keep passwords out of the idempotency ledger"`.

### Task W1: The cockpit can read why a check failed

**Files:**
- **Create:** `web/src/settings/mail/mailConnectLogic.ts`
- **Modify:**
  - `web/src/api/json.ts`: `HttpError` gains `readonly body: unknown`, which `httpErrorFrom` fills.
  - `web/src/governance/pimApi.ts`
  - `web/stryker.config.json`: `mutate` gains `src/settings/mail/mailConnectLogic.ts`.
- **Test:**
  - `web/src/api/__tests__/json.test.ts`
  - `web/src/settings/mail/__tests__/mailConnectLogic.test.ts`
  - the pimApi test file

**Interfaces:**
- **Produces, in `pimApi.ts`:**
  - `type PimDiscovery`, mirroring P5's `DiscoveryResult` in camelCase.
  - `discoverPim(email: string): Promise<PimDiscovery>`.
  - `verifyPimAccount(id: string): Promise<{ smtpWarning?: string }>`.
  - `patchPimProviderConfig(id: string, providerConfig: Record<string,string>): Promise<PimAccount & { smtpWarning?: string }>`.
  - `type PimVerifyReason`, with the six reasons.
  - `pimVerifyFailure(error: unknown): { step: 'imap'|'smtp'; reason: PimVerifyReason } | null`, which reads `HttpError.body` on a 422 whose `error === 'verify_failed'`.
  - `pimErrorKey(error: unknown): string`, for the non-422 cases:

    | Response | Key |
    |---|---|
    | 502 | `settings.mail.error.pimDown` |
    | timeout | `settings.mail.error.timeout` |
    | 400 `invalid_email` | `settings.mail.error.invalidAddress` |
    | 401 | `settings.mail.error.notReady` |
    | 429 | `settings.mail.error.tooMany` |
    | 400 `password_required` | `settings.mail.error.passwordRequired` |
    | 409 `changed` | `settings.mail.error.changed` |
    | anything else | `settings.mail.error.generic` |
- **Produces, in `mailConnectLogic.ts`:**
  - `accountIdFromAddress(address: string, attempt: number): string`
  - `verifyMessageKey(failure, appPasswordRequired: boolean): string`. It returns `settings.mail.verify.appPassword` for `auth_failed` when the flag is set, and `settings.mail.verify.<reason>` otherwise.
  - `isValidAddress(address: string): boolean`

- [ ] **Step 1: Write the failing tests.**
  - `httpErrorFrom` keeps the whole body. This is a round trip through a real `Response`.
  - `pimVerifyFailure` on a real 422 `Response`, and null on a 500.
  - `pimErrorKey`, one row per case.
  - `accountIdFromAddress('Mario.Rossi@Libero.it', 1) === 'mario-rossi-libero-it'`, attempt 3 gives `'…-3'`, and `'__a@b.c'` gives `'a-b-c'`.
  - `verifyMessageKey`, both branches.
- [ ] **Step 2: Run the tests and see them fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run vitest, lint, typecheck and `mutation:contract`.** All must pass.
- [ ] **Step 5: Commit:** `"Let the cockpit read why a mailbox check failed"`.

### Task W2: Add an account with an address and a password

**Files:**
- **Create:**
  - `web/src/settings/mail/AddMailAccount.tsx`
  - `web/src/settings/mail/OtherAccountForm.tsx`: today's `AddAccountForm`, moved from `CalendarConnect.tsx:165`, for ICS/JSON and every other provider.
  - `web/src/i18n/resources.settings.mail.ts` (it + en)
- **Modify:**
  - `web/src/i18n/resources.settings.ts`: `mail: mailEn` / `mail: mailIt` go inside `settingsEn.settings` / `settingsIt.settings`.
  - `web/src/governance/CalendarConnect.tsx`: it imports `OtherAccountForm` until W3.
- **Test:** `web/src/settings/mail/__tests__/AddMailAccount.test.tsx`

**Interfaces:**
- **Consumes:** W1; `ProviderConfigFields` (`CalendarConnectFields.tsx:50`); `startConnect`; `PimConnectPanel`.
- **Produces:** `AddMailAccount({ onCreated }: { onCreated: () => void })`.

Behaviour, in the order the tests pin it:
1. The screen shows «Indirizzo email», «Password» and «Collega». When a valid address loses focus, `discoverPim` runs. A discovery error counts as `unknown`.
2. **`google`, `microsoft365` or `outlook.com`.** The password field gives way to that provider's panel (`createPimAccount` with an empty `providerConfig`, then `startConnect` and `PimConnectPanel`).
   - With `PIM_PROVIDER_NOT_CONFIGURED` and `passwordFallback`, the screen shows `settings.mail.providerAppMissing` and «Usa una password per app», which switches to the `imap` path with the preset's hosts.
   - Without `passwordFallback`, it shows `settings.mail.providerAppMissingNoFallback` only.
3. **`imap` with `appPassword.required`.** A notice with the `helpUrl` link appears under the password field before the first attempt.
4. **`source` ending in `-mx`.** «Mi collegherò a {{host}}» appears under the password field before «Collega».
5. **«Impostazioni server».** A closed `<details>` holds `ProviderConfigFields` for the five IMAP fields, pre-filled from discovery.
   - It opens by itself on `unknown`, and on a failure whose step is `smtp` or whose reason is `unreachable` or `tls_failed`.
   - On `unknown` it shows `imap.<domain>` and `smtp.<domain>` as placeholders, not as values.
6. **«Collega».** It calls `createPimAccount({ id: accountIdFromAddress(..), displayName: address, provider: 'imap', providerConfig })`, sending the security keys from discovery. The button reads «Verifico l'accesso…» while the call is pending.
   - On a 409, the screen reads the list. An account whose `displayName` equals the address gets «Questo indirizzo è già collegato» and its «Cambia password»; an OAuth leftover gets «Rimuovi». Any other conflict retries with the next suffix, at most to `-9`.
7. **422.** `t(verifyMessageKey(..))` becomes the password field's error (`aria-describedby`; `aria-invalid` only while invalid). For a server reason it goes under the disclosure, opened.
8. **201 with `smtpWarning`.** The account is added, and `settings.mail.verify.smtpWarning.<code>` appears under the new row's name.
9. **Any other error.** `t(pimErrorKey(err))`. After a timeout, the list is re-read before any retry.
10. **«Altro tipo di account».** It reveals `OtherAccountForm`.
11. The password is never trimmed.

- [ ] **Step 1: Write the failing tests.**
  - `DiscoversOnBlur`
  - `PimDownDuringDiscoveryOpensTheServerFields`
  - `GmailOffersGoogleSignIn`
  - `NoGoogleAppOffersAnAppPassword`
  - `NoMicrosoftAppOffersNoPassword`
  - `AppPasswordNoticeBeforeTrying`
  - `MxDerivedServersAreShownBeforeConnecting`
  - `UnknownOpensTheServerFieldsWithPlaceholders`
  - `WrongPasswordNamesThePasswordField`
  - `SmtpWarningIsShownAfterConnecting`
  - `SmtpAuthFailureOpensTheServerFields`
  - `ConflictOnTheSameAddressOffersRepair`
  - `ConflictOnAnotherAddressRetriesWithASuffix`
  - `CreateTimeoutChecksTheListBeforeRetrying`
  - `TrailingSpaceInThePasswordIsSent`
  - `OtherAccountTypeShowsTheFullForm`
- [ ] **Step 2: Run the tests and see them fail.**
- [ ] **Step 3: Implement.** The copy goes in `resources.settings.mail.ts` under `settings.mail.*`. The `tls_failed` text tells the person to use the server name the provider gives, not one on their own domain.
- [ ] **Step 4: Run the file's vitest, i18n parity/usage, lint, typecheck and format.** All must pass.
- [ ] **Step 5: Commit:** `"Connect a mailbox with its address and password"`.

### Task W3: Settings opens to members, with «Posta e calendario»

**Files:**
- **Create:** `web/src/settings/mail/MailAccountsSection.tsx`
- **Modify:**
  - `web/src/shell/modes.ts:31`: `ADMIN_MODES` becomes `['governance']`; update its comment and the mode tests.
  - `web/src/AppShell.tsx:78-80` and `:162-175`: comments and guards follow; the `?settings=` deep link works for members.
  - `web/src/settings/settingsSections.ts`: add `'mail'`, group `personal`, icon `Mail`, `labelKey: 'settings.mail.heading'`, not `adminOnly`.
  - `web/src/settings/SettingsWorkspace.tsx`:
    - add `case 'mail'`;
    - the `!isAdmin` branch becomes the rail with `visibleSections(false)` (Profile and Posta e calendario), keeping the ChatGPT account block inside the Profile pane;
    - update its header comment.
  - `web/src/governance/CalendarConnect.tsx`: the account list and the add form leave it. It keeps `PimProviderAppsPanel` for admins and a link `<a href="/?settings=mail">` «Gli account si collegano in Impostazioni → Posta e calendario».
- **Test:**
  - `web/src/settings/mail/__tests__/MailAccountsSection.test.tsx`
  - the shell mode tests and an `AppShell` test
  - `SettingsWorkspace`
  - `CalendarConnect`
  - `governance/__tests__/CalendarConnectRow.test.tsx` moves to render `MailAccountsSection`

**Interfaces:**
- **Consumes:** W2; `MailWatchOverview`, `PimAccountRow` and the `['connect','pim','accounts']` query, as `CalendarConnect` uses them today.
- **Produces:** `MailAccountsSection()`.

- [ ] **Step 1: Write the failing tests.**
  - `AMemberOpensSettingsAndSeesOnlyPersonalSections`, at shell level.
  - `AMemberFollowsTheSettingsMailLink`.
  - `ListsTheAccountsAndTheAddFlow`.
  - `GovernanceLinksToTheSection`.
  - `GovernanceStillShowsProviderAppsToAnAdmin`.
  - The moved `CalendarConnectRow` cases.
- [ ] **Step 2: Run the tests and see them fail.**
- [ ] **Step 3: Implement, by moving code rather than copying it.**
- [ ] **Step 4: Run vitest, the full `npm run test` with its coverage floor, lint, typecheck and `mutation:contract`.** All must pass.
- [ ] **Step 5: Commit:** `"Open Settings to every identity and connect mailboxes there"`.

### Task W4: Repair on the row, and a failure that stays too long asks for it

**Files:**
- **Create:** `web/src/governance/ImapRepair.tsx`
- **Modify:**
  - `web/src/governance/PimAccountRow.tsx`: an `imap` row renders `ImapRepair`.
  - `web/src/governance/mailWatch/mailWatchLogic.ts`: `staleTransient`.
  - `web/src/governance/mailWatch/AccountWatch.tsx`:
    - `provider` is threaded from `account` through `WatchState` to `FailingLine`;
    - the stale wording uses `dayAndTime`;
    - the decision-model failure time (`:255-257`) also uses `dayAndTime`.
  - `web/src/i18n/resources.governance.mailWatch.ts`, `resources.governance.mailWatchDecide.ts` (`decide.failing` reworded to fit a day and a time, in both locales), and `resources.settings.mail.ts`.
- **Test:**
  - `web/src/governance/__tests__/ImapRepair.test.tsx`
  - `mailWatchLogic.test.ts`
  - the `AccountWatch` test

**Interfaces:**
- **Consumes:** W1.
- **Produces:**
  - `ImapRepair({ account, watch }: { account: PimAccount; watch?: MailWatchAccount })`
  - `staleTransient(entry: MailWatchResource, provider: string | undefined, now: Date): boolean`

Behaviour:
- **«Prova di nuovo»** calls `verifyPimAccount`. It shows «L'accesso funziona» (plus the SMTP warning, if any), or the reason's message, or `pimErrorKey`.
- **«Cambia password»** opens an inline password field and the closed «Impostazioni server», whose blank fields mean "unchanged". «Salva» sends only the non-empty fields.
  - A changed server or port with no password is caught in the form, before it is sent, as `settings.mail.error.passwordRequired`.
  - The username is not shown (the cockpit never receives it); a note says it cannot be changed (ledger Ruling 16).
- **After either action succeeds,** it invalidates the accounts query and the mail-watch status query and records `verifiedAt`.
  - After a working login the row says «L'accesso funziona. Se la lettura non riprende al prossimo controllo, la password non è il problema.» (`settings.mail.repair.loginWorks`) and keeps its buttons; the stale prompt drops only while a resource's `failing_since` is the one current at that login. (Superseded 2026-11-23, ledger Ruling 20: the earlier `readStillFails` judgement on `next_run_at` could not tell a read started from a read finished.)
- **`auth_failed` in repair** reads `settings.mail.verify.authFailedRepair`: wrong password, or the provider wants an app password.
- **Stale wording:** `governance.mailWatch.failure.stale` = «Non riesco a leggere la posta dal {{since}}. Prova di nuovo o cambia la password.», with `since` from `dayAndTime`. In English: "Aura has not been able to read this mailbox since {{since}}. Try again or change the password."

- [ ] **Step 1: Write the failing tests.**
  - `staleTransient`:
    - imap, transient, 7 h old → true;
    - 5 h old → false;
    - `google` → false;
    - `pim_http_5xx` → false.
  - `RetryShowsSuccess`
  - `RetryShowsTheReason`
  - `RetryShowsTheSmtpWarning`
  - `ChangePasswordSendsOnlyThePassword`
  - `ChangingTheServerAsksForThePassword`
  - `TheUsernameIsReadOnly`
  - `OAuthRowHasNoRepair`
  - `ReadingStillFailsAfterAWorkingLogin`
  - `AStaleImapFailureAsksForRepair`, whose text has no doubled preposition
  - `TheDecideFailureTimeHasItsDay`
- [ ] **Step 2: Run the tests and see them fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run vitest, the full `npm run test`, lint, typecheck, format and `mutation:contract`.** All must pass.
- [ ] **Step 5: Commit:** `"Repair an IMAP account in place, and say so when its failure has lasted too long"`.

### Task G1: The new critical files are measured by CI's gates

**Files:**
- `scripts/critical_mutation_gate.py` (`GO_SCOPES`, `REQUIRED_SCOPE_IDS`)
- `scripts/critical_mutation_gate_test.py`
- `.github/workflows/ci.yml` (the `go-mutation` matrix, `:1780-1790`)

| Scope id | File |
|---|---|
| `mail_repair` | `internal/pimwatch/service_repair.go` |
| `pim_account_relay` | `internal/agui/connect_pim_account_api.go` |
| `pim_secret_fingerprint` | `internal/agui/idempotency_pim.go` |

- [ ] Add a case to `critical_mutation_gate_test.py` that requires the three scope ids, and see it fail.
- [ ] Add them to `GO_SCOPES` and `REQUIRED_SCOPE_IDS`, and to an existing group: `scripts/mutation_workflow_test.py:119` caps the matrix at three. Raise that group's timeout by the files' measured cost × 2.7 (`ci.yml:1762`).
- [ ] Run `python3 -m pytest scripts/critical_mutation_gate_test.py scripts/mutation_workflow_test.py` and get it green. Then run `bash scripts/coverage_docker.sh` in WSL, and keep the aggregate and the two packages' numbers for L1. `agui` and `pimwatch` are `target` packages (`coverage_package_policy.json:15,83`), so no baseline moves.
- [ ] Commit: `ci(mutation): measure the mail repair, the PIM relay and the secret fingerprint`. After the push, check that CI measures all three at 70% killed or more.

### Task L1: Live, on the lab VM

- [ ] **Step 0: Bring the live harnesses up to the new rules (added 2026-11-20 by the W3 review).**
  - `web/e2e/mcp-cockpit-live.spec.ts`: W3 marked "configures PIM in the cockpit…" `test.fixme`, because the Governance IMAP form it drove is gone. Rewrite it to drive Settings → «Posta e calendario» (`AddMailAccount`), and lift the `fixme`.
  - Its fake IMAP on 1143, and the `aura-model-in-the-loop` skill's PIM mount over GreenMail, now meet `400 tls_required` (A2) and `422 unreachable` for a private host without the header (A5). Give them TLS or STARTTLS, and add accounts as an identity holding `identity.create`, or the header.
- [ ] **Step 1: Release in this order.**
  1. Merge A1-G1 to `master`, push with the WSL recipe, and wait for CI to go green on that SHA.
  2. Merge the PIM PR (P5 step 7) and wait for `aura-publish-image.yml`.
  3. Cut the Aura update the usual way. The operator applies it on the VM.
  4. On the VM, compare `docker inspect --format '{{.Image}}' aura-pim-mcp` with the image ID of `ghcr.io/chetto1983/aura-pim-mcp:<merge sha>` pulled there. No label carries the SHA.
- [ ] **Step 2: Run the spec's verification steps 0-6** with the operator's own mailbox. Credentials are never written down. Add these steps:
  - (7) As a member, verifying `127.0.0.1:143` is refused, and as the admin it is attempted.
  - (8) The Settings deep link works for a member.
  - (9) An `idempotency_operations` row for a PATCH carries no plain SHA-256 of its body.

  For steps 2, 4 and 6, read the PIM log for the stage, the exception type and the server's line.
- [ ] **Step 3: Write and commit the verification file and the PRD amendment.**
  - Write `docs/superpowers/verification/2026-11-<dd>-pim-account-connect-repair-live.md`, recording what each step measured.
  - In "what this does not prove", carry the spec's list (preset drift, every provider's rule, the Yahoo cause until that install runs «Prova di nuovo», Aruba-hosted domains, re-resolution on watch ticks) and A10 (the key ring sits beside the ciphertext, and no backup covers the volume).
  - Add a PRD amendment recording the measurement.
  - Commit both.

## Decisions taken while writing this plan

1. **The verifier is a DI seam in the PIM.** The endpoint tests use a fake, and the real verifier is tested against Dovecot (real passwords) and two GreenMails. Measured by the grounding review:
   - Dovecot refuses a wrong password and allows cleartext (`ssl = no`, `auth_allow_cleartext = yes`).
   - The CI GreenMail serves SMTP on 3025, always advertises AUTH, and accepts any login (`-Dgreenmail.auth.disabled`).
   - So `smtp_auth_failed` needs a second GreenMail with auth.
   - `tls_failed` is reachable as "STARTTLS required, server offers none", against the plaintext Dovecot.
2. **The mapping from exceptions to reasons is stage-aware.** MailKit throws `AuthenticationException` for LOGINDISABLED and for SMTP 4xx too, and `NotSupportedException` for missing STARTTLS (MailKit 4.18.1, `ImapClient.cs#L1213,#L1482`; `SmtpClient.cs#L1263-1277`).
3. **Encryption covers the `password` key of every provider.** `DavProviderService` already unprotects. Upstream `PUT` therefore stores encrypted too, which is spec amendment A10. It is still not verified.
4. **ISPDB's `documentation` element is never used.** The format calls it informational only. Only a preset entry, copied from the provider's page, says an app password is required.
5. **The repair actions are always on an IMAP row.** The stale wording is the prompt.
6. **Upstream `PUT` keeps its replace semantics and is not verified.** The cockpit never calls it.
7. **`DnsClient` and `Microsoft.Extensions.Caching.Memory` are new fork dependencies.** .NET 10 has no MX query.
8. **The singletons need no new wiring.** `AccountConfigurationService`, `ITenantContext` and `IImapProviderService` are singletons (`Program.cs:107`, `ServiceCollectionExtensions.cs:24,36`). The openers have no callers outside `ImapProviderService.cs`.
9. **No Public Suffix List dependency.** The MX step tries every suffix of the MX host, from the next subdomain down to two labels, against the preset, autoconfig and ISPDB. A suffix like `co.uk` simply finds nothing.
10. **The private-address guard runs in the PIM, and Aura decides who may pass it.** Only the PIM knows which address it is about to dial, and only Aura knows who is an administrator. The header is built by Aura's own client, and `dialPIM` sets no header taken from the request.

## Not taken from the reviews

- **An NS-based rule for domains hosted at Aruba** (cross-source H2). Neither reference client does it, so it would be a bespoke heuristic. It is left as "does not prove" and offered to the operator as a follow-up.
- **`ProtectKeysWithCertificate` for the key ring** (cross-source M5). It is optional, and the spec records what `ENC:` covers and what it does not.
- **Re-checking the resolved address on every watch tick** (adversarial M8). The guard covers create, PATCH and verify. A stored public name that later resolves privately is a recorded limit (spec A10).
