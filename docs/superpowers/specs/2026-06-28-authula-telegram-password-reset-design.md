# Authula-Only Telegram Password Reset Design

Date: 2026-06-28

## Purpose

Aura will remove the legacy passphrase login path and make Authula the only cockpit
authentication provider. Password recovery will not use email. Instead, Aura will use the
user's linked Telegram account plus a security question captured during onboarding.

This design covers the first implementation slice: Authula-only login, onboarding recovery
setup, and self-service password reset through Telegram.

## Decisions

- Authula is the only web-auth provider. The passphrase provider and passphrase login UI are
  removed from the active product path.
- New onboarded users must link Telegram before the onboarding wizard completes.
- The onboarding wizard collects a security question and masked security answer.
- A self-service reset requires both a Telegram one-time code and the security answer.
- Aura never emails password reset links or codes.
- Aura never stores or renders the raw security answer after submission.

## Current Context

`cmd/aura/serve_auth.go` still defaults to the passphrase provider unless
`AURA_WEB_AUTH_PROVIDER=authula`. `web/src/routes/LoginPage.tsx` still renders either a
passphrase form or an Authula email/password form depending on `/api/auth/config`.

The onboarding saga in `internal/agui/onboarding_provision.go` already creates Authula
users and Aura identity links. The wizard keeps the initial password write-only and sends
it only on `/api/onboarding/{token}/provision`.

Telegram already has an identity-linked account table in `aura.telegram_accounts` and a
channel delivery path through `Telegram.Deliver(ctx, identityID, text)`. Password reset can
reuse this identity-keyed delivery path.

Authula v1.11.0 exposes `CoreServices.PasswordService.Hash` and
`CoreServices.AccountService.Update`. Aura can wrap those services behind a small adapter
that updates the email-password account after Aura verifies the Telegram code and recovery
answer.

## User Experience

### Onboarding

The credentials step becomes the recovery setup step. It collects:

- email
- initial password
- security question
- security answer
- Telegram link requirement

The question can be visible in review. The answer must stay write-only like the initial
password: masked input, not displayed in review, not included in completion copy, and never
logged.

The provisioning request must always ask for Telegram linking. The wizard does not show the
final completion state until the existing `telegram-status` poll confirms that the Telegram
token was consumed. This keeps the implementation aligned with the current saga, where the
identity must exist before Aura can mint an identity-bound Telegram token.

### Login

The login page shows only Authula email/password and TOTP flows. It adds a
**Forgot password** action below the password field.

### Password Reset

The reset flow has three screens:

1. **Identify account**: the user enters the email address.
2. **Verify recovery**: Aura sends a short one-time code to the linked Telegram chat. The
   user enters that code and answers their security question.
3. **Set password**: the user enters and confirms a new password.

All request and error copy uses neutral language. The browser must not reveal whether an
email exists, whether Telegram is linked, or whether a security answer exists.

## Backend Architecture

Add an `internal/agui` password reset service behind narrow ports:

- `IdentityRecoveryLookup`: finds an identity, Authula user ID, recovery question metadata,
  and Telegram link by normalized email.
- `RecoverySecretStore`: hashes, verifies, and rotates the security answer.
- `ResetChallengeStore`: creates, verifies, consumes, expires, and rate-limits reset codes.
- `RecoveryMessenger`: sends the one-time code to the linked Telegram account.
- `AuthulaPasswordResetter`: hashes the new password with Authula and updates the
  email-password account.
- `RecoveryAuditWriter`: writes non-secret audit records for requested, verified, completed,
  expired, and denied reset attempts.

Keep these ports in `internal/agui` so handlers and tests can use fakes. Put database-backed
implementations beside the existing identity, Telegram, and Authula adapters.

## Data Model

Add an Aura-owned migration after `0022_mcp_audit`:

### `aura.identity_recovery`

One row per identity.

- `identity_id uuid primary key references aura.identities(id) on delete cascade`
- `question text not null`
- `answer_hash text not null`
- `answer_hash_version text not null`
- `created_at timestamptz not null default now()`
- `updated_at timestamptz not null default now()`

The answer hash uses Argon2id. The raw answer is normalized before hashing with a conservative
normalizer: trim outer whitespace, collapse internal whitespace, and case-fold with Unicode
simple fold. The normalizer must be shared by enrollment and verification.

### `aura.password_reset_challenges`

One row per reset attempt.

- `id uuid primary key`
- `identity_id uuid references aura.identities(id) on delete cascade`
- `code_hash text not null`
- `telegram_user_id bigint`
- `created_at timestamptz not null default now()`
- `expires_at timestamptz not null`
- `consumed_at timestamptz`
- `attempt_count integer not null default 0`
- `max_attempts integer not null default 5`
- `request_ip_hash text`
- `user_agent_hash text`

Codes expire after 10 minutes. Consuming a code is atomic: a successful verification marks the
row consumed and creates a server-side reset token.

### `aura.password_reset_tokens`

One row per verified challenge.

- `token_hash text primary key`
- `challenge_id uuid not null references aura.password_reset_challenges(id) on delete cascade`
- `identity_id uuid not null references aura.identities(id) on delete cascade`
- `created_at timestamptz not null default now()`
- `expires_at timestamptz not null`
- `consumed_at timestamptz`
- `attempt_count integer not null default 0`
- `max_attempts integer not null default 3`

Reset tokens expire after 10 minutes. `complete` marks the token consumed only after Authula
accepts the password update. If Authula fails, the token remains active until TTL or max attempts
so the user can retry without starting a new Telegram challenge.

### `aura.identity_recovery_audit`

Append-only audit for recovery events.

- `id uuid primary key`
- `identity_id uuid`
- `event text not null`
- `created_at timestamptz not null default now()`
- `request_ip_hash text`
- `user_agent_hash text`
- `metadata jsonb not null default '{}'::jsonb`

The audit table never stores codes, raw answers, raw emails, raw IP addresses, passwords, or
Telegram bot tokens.

## API Contract

Add public unauthenticated endpoints outside the whole-origin `RequireAuth` gate, but keep CSRF
protection and strict body limits:

### `POST /api/auth/password-reset/start`

Request:

```json
{ "email": "operator@example.com" }
```

Response is always `202 Accepted` for syntactically valid requests:

```json
{ "status": "ok" }
```

If the account exists, has recovery configured, and has Telegram linked, Aura sends a code.
Otherwise it only writes a neutral audit event.

### `POST /api/auth/password-reset/verify`

Request:

```json
{
  "email": "operator@example.com",
  "code": "123456",
  "answer": "write-only answer"
}
```

Response:

```json
{ "resetToken": "opaque-short-lived-token" }
```

The reset token is an opaque server-side token bound to the consumed challenge. It expires after
10 minutes and can be used once.

### `POST /api/auth/password-reset/complete`

Request:

```json
{
  "resetToken": "opaque-short-lived-token",
  "password": "new password"
}
```

Response:

```json
{ "status": "password_updated" }
```

Completion deletes all active Authula sessions for the user so old sessions cannot survive a
recovery event.

## Authula-Only Cutover

`buildAuthDeps` should build Authula unconditionally for web auth. If Authula is missing required
configuration, `aura serve` fails with a clear startup error. It must not silently fall back to
passphrase.

`/api/auth/config` should return Authula configuration only. The login page should remove
`AuthProvider = 'passphrase'`, `submitPassphrase`, and the passphrase field.

`.env.example`, `compose.yaml`, and docs should stop presenting passphrase as the default. Keep
legacy environment variables only if another non-login subsystem still needs them.

## Security Controls

- Do not use the security answer alone to reset a password.
- Store security answers and reset codes as hashes only.
- Use constant-time comparison through the password/hash service.
- Rate-limit reset start by IP hash and normalized email hash.
- Rate-limit verify attempts by challenge ID.
- Return neutral responses for unknown email, missing Telegram, missing recovery setup, wrong
  code, and wrong answer.
- Redact emails, codes, answers, passwords, Telegram tokens, IP addresses, and user agents from
  logs.
- Delete or expire reset tokens and challenges after use.
- Invalidate Authula sessions after a successful reset.
- Write audit rows for all meaningful recovery events.

The design follows OWASP guidance that security questions should not be a sole recovery factor.
In Aura they are a second factor after Telegram possession.

## Tests

Use test-first implementation. The first failing tests should cover:

- onboarding provision rejects missing security question or answer
- onboarding stores only answer hashes and never logs the raw answer
- onboarding requires Telegram completion before the wizard reaches its final success state
- reset start always returns neutral `202` for valid syntax
- reset start sends Telegram only for identities with linked Telegram and recovery setup
- reset verify rejects expired, consumed, excessive-attempt, wrong-code, and wrong-answer cases
- reset verify consumes a challenge exactly once
- reset complete updates the Authula email-password account and deletes active sessions
- login UI renders no passphrase path
- onboarding UI never renders the security answer outside its masked input
- reset UI handles neutral start, verification failure, and completion success

Run the focused Go tests, web unit tests, typecheck, build, and the relevant Playwright login and
onboarding E2E tests before shipping.

## Rollout

1. Add the recovery tables and ports behind tests.
2. Extend onboarding request validation and provisioning.
3. Add Telegram reset challenge delivery.
4. Add reset APIs and Authula password update adapter.
5. Remove passphrase login/provider paths from active config and UI.
6. Add login, onboarding, and reset UI.
7. Rebuild the web bundle and container.
8. Run focused and full verification.

Existing users without Telegram or recovery setup cannot use self-service reset. The settings
page should later expose a recovery status panel so those users can add Telegram and a recovery
question from an authenticated session.
