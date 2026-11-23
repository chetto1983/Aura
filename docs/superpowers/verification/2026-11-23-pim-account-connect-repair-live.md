# Mail account connect and repair on the lab VM

Spec: [2026-11-20-pim-account-connect-repair-design.md](../specs/2026-11-20-pim-account-connect-repair-design.md). Plan: [2026-11-20-pim-account-connect-repair.md](../plans/2026-11-20-pim-account-connect-repair.md), Task L1.

## Setup

**Run.** The run was on the lab appliance at `192.168.101.158` on 2026-11-23, after a normal update that the operator applied from the cockpit.

**Builds under test.**

| Component | Build | Check |
|---|---|---|
| Aura | `ghcr.io/chetto1983/aura:edge` at revision `85ad9793474d962bdbbc23c981a1267a496b3ff3` | The image label |
| PIM | `ghcr.io/chetto1983/aura-pim-mcp`, the merge commit `7ca4c89` of PR #5 | The container's image ID `sha256:8cf4f4a2…` equals the published index digest |

**Mailboxes.** The operator's three connected accounts were used:
- an IMAP mailbox at Aruba, stored before this work;
- a Microsoft 365 account;
- a Google account.

Every test mail went between these accounts only. No address, credential or mail text is recorded here.

**How each step was measured.**
- The API steps went through the cockpit relay, signed in as the operator.
- The send steps were agent turns. The operator approved each send in the cockpit, as Aura requires.
- The watch state was read from `aura.pim_watch_cursors` and `aura.scheduler_tasks`. The PIM's side was read from its log.

## Results

| Step | Observed result |
|---|---|
| 0. Stored passwords and reading after the update | The PIM logged `Protected 1 stored passwords` at start. The Aruba account's password now starts with `ENC:`. The Aruba mailbox was read at 13:29:44, after the PIM restarted at 13:22:34, and all three accounts read `ok`. That read is the first runtime IMAP connection opened through `HostGuard` (P6) with TLS on port 993. |
| 1. Gmail address | `discover` answered `kind: google`, source `preset`, with an app-password hint and a password fallback. |
| 2. IMAP account, wrong password | `422 {"step":"imap","reason":"auth_failed"}`. The log shows `failed at imap/auth: AuthenticationException Invalid credentials`. The account list was unchanged. A nonexistent Gmail address was used, so no real mailbox took a failed login. |
| 3. Same account, right password | Not run as an add. The only IMAP mailbox was already stored, so the repair in step 4 carried the right-password case. |
| 4. «Cambia password», wrong then right | The wrong password went in through the API and answered **`422 imap/unreachable`**, not `auth_failed` (finding 1). The stored config was unchanged: same keys, same password hash. The right password went in through the cockpit at 13:42:52: `Changed the provider config`, and the configuration reloaded. The stored config gained `imapSecurity: ssl` and `smtpSecurity: ssl`, derived from the ports (amendment A2). It still has no `allowPrivateHosts`, because a password-only PATCH keeps the stored permission (Ruling 28). |
| 5. Domains outside the preset table | `posteo.de` resolved through `isp`, `fastmail.com` through `ispdb-mx`, `libero.it` from `preset`, and a nonexistent domain answered `kind: unknown`. |
| 6. Password changed on the provider's side | The operator changed the Aruba password. The next watch tick, at 13:40:28, marked mail `failing` with `provider_auth`, and the log reads `Reading mail … failed as provider_auth (AuthenticationException)`. «Prova di nuovo» with the old password timed out at `imap/auth` (finding 1). «Cambia password» with the new one succeeded. The watch could not be shown resuming on the spot, because its task had been stopped from the cockpit at 13:41:30, before the repair. |
| 7, 8. Member steps | Not run: the operator closed the run before the member identity was created. |
| 9. Idempotency ledger | The PATCH row's `payload_hash` differs from the plain SHA-256 of the request body. No ledger row contains the test password. |
| Send over the guarded socket | **Aruba → Google.** The PIM sent through Aruba SMTP on port 465 in 0.5 s, and the mail reached the Google inbox the same second. **Google → Aruba.** The mail arrived, and the agent read it from the Aruba inbox through the IMAP pool. |

## Findings

1. **A wrong password at Aruba reads as an unreachable server.**
   - **What happens.** The PIM's `imap/auth` stage ran out of its 12 s budget (`TaskCanceledException`), and the cockpit said the server could not be reached.
   - **Measured on the same server.** Aruba (Dovecot) refuses one wrong `LOGIN` or `AUTHENTICATE PLAIN` in 1.7-2.5 s. A second authentication on the same connection first waits about 4 s for its continuation.
   - **Likely cause, not proven.** After a `NO`, MailKit tries the next password mechanism it was offered. The penalty grows with each try until the stage budget is spent.
   - **Status.** It blocks the A8 promise that the row names the remedy, for a common Italian provider. It is a PIM follow-up.
2. **Sent copies on the Aruba account go to `[Gmail]/Sent Mail`.**
   - **What happens.** That folder does not exist on Aruba, so the APPEND fails with `FolderNotFoundException` and the sent copy is lost. The mail itself is delivered.
   - **Status.** This predates this work. It is a PIM follow-up: find the sent folder through SPECIAL-USE `\Sent`.
3. **Discovery sends an Aruba-hosted domain to Google.**
   - **What happens.** The operator's own domain has its MX at Google, so discovery answers `google` (`preset-mx`), while the stored mailbox is Aruba IMAP. The person has to open «Impostazioni server» and type the Aruba hosts.
   - **Status.** This is the spec's known limit for domains hosted at Aruba.
4. **Retries inside 30 s read «Troppi tentativi».**
   - **What happens.** Three repair attempts within 30 s answered `429`. This is the per-account throttle at work.
   - **The copy.** It now says to wait half a minute (F4).

## What this does not prove

- That the preset hosts stay right over time.
- That every provider's app-password rule is captured.
- The cause of the Yahoo field report.
- Discovery for every Aruba-hosted domain.
- Re-resolution of a host on each watch tick, which now holds by construction (P6) but was not forced live here.
- **Encryption at rest (A10).** The key ring sits on the same volume as the ciphertext, and no backup covers that volume.
- **The member path.**
  - Refusal of a private host for a member (step 7) and the Settings deep link for a member (step 8) were not run.
  - The private-host guard on the runtime paths was exercised only with a public provider. Its refusal is covered by the PIM's tests and CI, not by this run.
- **Resuming the watch on the spot after a repair.** It was not observed, because the watch had been stopped before the repair.
- **The live Playwright spec (`web/e2e/mcp-cockpit-live.spec.ts`).** It still skips in CI until the web-e2e job is wired to `scripts/pim_tls_mail_up.sh`.

## CI on the candidate

`85ad97934`: every `ci.yml` job is green, including the three Go mutation groups that now measure `mail_repair`, `pim_account_relay` and `pim_secret_fingerprint`. The frontend Stryker job was still running when this file was written.

The PIM's PR #5 build was green with 2074 tests. Its Codacy check was red, with 26 annotations, all in test files: RFC 1918 fixture addresses, fixture passwords, and three style notes.

The database coverage gate passed locally on the disposable `aura_cov` database at 54,225 of 61,613 owned statements (88.0%), with `internal/agui` at 89.1% and `internal/pimwatch` at 92.8%.
