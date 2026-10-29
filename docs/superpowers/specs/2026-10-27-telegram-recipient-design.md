# Unambiguous Telegram push recipients

> **Superseded on 2026-10-28 by R6** of
> `docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md`: the operator chose that
> an identity push with several linked accounts go to the one seen most recently, and that a
> scheduled task name its recipients, instead of refusing the push as ambiguous. The measured
> incident below stands; `ErrAmbiguousAccount` and the two-row lookup were not adopted. The
> regression's cases were kept under the new rule
> (`TestIdentityDeliveryWithSeveralLinksReachesTheOneSeenLast`).

## Measured problem

On 2026-10-27 an appliance running `ee04268a05f156723e230904f8298b7864d48775`
had three Telegram accounts linked to one identity. Scheduled notifications selected
an obsolete account and failed with `403: bot was blocked by the user`. The configured
bot could reach the intended account. After an operator-approved backup and removal
of the two unwanted associations, the operator received both a direct Bot API test
and a native scheduler reminder. Nine old failed notifications were not replayed.

The same unchecked lookup exists on upstream `2e41ead35eaf48a7670af10df428c468f7723e30`:
`GetTelegramAccountByIdentity` uses sqlc `:one` on a non-unique `identity_id`.
The incident proves the mapping repair, not the proposed code's behavior. No live
credentials, identity IDs, Telegram IDs or usernames belong in this report.

## Decision

An identity-keyed push requires exactly one linked Telegram account. Retrieve at
most two matches in one SQL statement: zero retains `pgx.ErrNoRows`, one returns
the account, and two returns `ErrAmbiguousAccount` with an operator-actionable
message. No ordering is needed because no account is selected from multiple rows.
Use sqlc's existing `:many` support; no custom database adapter or new dependency.

The delivery methods already stop on resolver errors other than `pgx.ErrNoRows`.
This applies to text, conversation-bound pushes and approval prompts. Inbound
Telegram-ID lookup and enrollment remain unchanged. There is no migration,
automatic deletion, activity-based selection, fallback to another account, or
historical notification replay. Explicit primary-account selection is separate work.

## Acceptance and risk

- Zero / malformed identity: preserve the existing not-my-user behavior.
- One account: preserve the recipient and successful sends.
- Two or three accounts: return an explicit ambiguity error and perform no send.
- An unrelated identity does not change the decision.
- After an approved mapping repair leaves one account, the next lookup succeeds.
- Database/query failures remain errors, never absence or ambiguity.

Risk: deployments intentionally linking several accounts will now need an operator
to resolve the destination before identity-routed pushes can resume. Existing links
are retained, so the change does not revoke inbound access. This is a medium-risk
routing change with no schema or environment changes. Unit tests, real PostgreSQL
regressions and the package race/leak checks are required. The production repair
does not establish full-stack acceptance of this patch.
