# Phase 43: Operator Break-Glass Recovery + Forgot-Password E2E - Context

**Gathered:** 2026-07-11
**Status:** Ready for planning

<domain>
## Phase Boundary

Add an offline, host-only `aura identity recover-operator` CLI command that resets the **operator** account's password (argon2id, Authula-compatible), invalidates its sessions, and re-seeds a missing `aura.identity_recovery` row — closing the permanent-lockout hole where a wiped recovery row leaves no offline path back in. Plus end-to-end (Playwright) coverage of the online forgot-password flow: the happy path and the deny-when-recovery-missing path.

This clarifies **HOW** to implement the 6 SPEC-locked requirements. It adds no new capability beyond them.

</domain>

<spec_lock>
## Requirements (locked via SPEC.md)

**6 requirements are locked.** See `43-SPEC.md` for full requirements, boundaries, and acceptance criteria.

Downstream agents MUST read `43-SPEC.md` before planning or implementing. Requirements are not duplicated here.

**In scope (from SPEC.md):**
- `aura identity recover-operator` — offline, host-only, operator-only: reset password + kill sessions + re-seed recovery.
- Password sourcing: hidden prompt (default), `AURA_RECOVERY_PASSWORD` env, `--generate`; `--no-recovery` opt-out for the re-seed.
- `web/e2e/password-reset.spec.ts` — happy + deny paths (Telegram delivery mocked).
- Backend unit + `db_integration` tests for the command and the deny branch.

**Out of scope (from SPEC.md):**
- TOTP reset / re-provision (does not touch `authula.totp`).
- Any-identity recovery — operator-only (`kind='user'`).
- A web UI for break-glass recovery — CLI only.
- Changing the online forgot-password security model or Telegram delivery.
- A new DB migration (`identity_recovery`, `telegram_accounts`, `identity_auth_links`, `authula.*` already exist).
- Boot-time auto-healing of a missing recovery row.

</spec_lock>

<decisions>
## Implementation Decisions

### Password-set path
- **D-01:** Implement a **new offline break-glass setter** that constructs Authula's account/password/session services against the same pgx pool and does: resolve `authula_user_id` via `aura.identity_auth_links` → `PasswordService.Hash` → update `authula.accounts` → `SessionService.DeleteAllByUserID`. Reuse Authula's own `PasswordService.Hash` (SPEC constraint), do NOT re-implement argon2, and do NOT touch the online `authulaPasswordResetter.SetPassword` path.
- **D-02:** Break-glass **allows setting the same password** — the new setter MUST NOT enforce `ErrPasswordResetSamePassword` (unlike the online resetter, which does). This is a deliberate availability choice (SPEC Edge Coverage: "same-as-current password" allowed).

### Recovery Q&A sourcing (re-seed UX)
- **D-03:** Source the recovery security-question + answer **symmetrically with the password**: `AURA_RECOVERY_QUESTION` + `AURA_RECOVERY_ANSWER` env vars, a hidden interactive prompt (answer hidden + confirmed) as the default, and `--generate` also generating a random answer (printed once, alongside the password) paired with a fixed default question. This keeps fully-non-interactive runs and `--generate` automation working end-to-end for the re-seed, not just the password.
- **D-04:** Hash the answer by **reusing `agui.RecoveryHasher{}.HashAnswer`** (version tag `argon2id-v1`, `internal/agui/recovery_hash.go`) and persist via the existing `UpsertIdentityRecovery` sqlc query — mirrors bootstrap/onboarding exactly; the upsert makes the re-seed idempotent (update-in-place, never a duplicate row).

### Coexistence with existing `recover`
- **D-05:** Ship `recover-operator` as a **sibling subcommand** alongside the existing `recover <name>` (which mints a short-lived reset *token* via 0023 infra — a different mechanism). Update `identityUsage` to list both with a one-line disambiguation ("`recover <name>` = mint token to hand a user / `recover-operator` = offline operator password reset + recovery re-seed") and print a short clarifying line on the command's own stdout. The existing `recover` path is left untouched.

### Audit logging (security hardening — within SPEC intent)
- **D-06:** Record a **neutral, secret-free audit event** on success (e.g. `operator_password_recovered`), mirroring the existing `break_glass_token_minted` event that the sibling `recover` already writes. Grounded in the SPEC's own boundary language ("recovery is an explicit, **audited** operator action") and the industrial break-glass norm that every use is meticulously logged (who/when/what, never the secret). No plaintext password/answer in the event.

### Throwaway-DB test harness
- **D-07:** The `db_integration` test **self-provisions a dedicated disposable database** (admin/migrate DSN → `CREATE DATABASE` a throwaway e.g. `aura_recover_test` → migrate → seed one operator + `identity_auth_links` + `telegram_accounts` → delete `identity_recovery` → run the command against it → assert → `DROP DATABASE` on `t.Cleanup`).
- **D-08:** The test helper **refuses to run against a DB named `aura`** (mirrors `scripts/coverage_gate.sh`'s live-DB refusal) — this is the exact 37D-05 / coverage-gate footgun that produced the lockout. **Scope note:** this refusal guard lives in the **test only**. The *command itself* MUST run against the live `aura` DB in production (that is its entire purpose) and must NOT carry a name-based refusal.

### Claude's Discretion
- Exact flag parsing style (hand-rolled, mirroring `runDB`/`runIdentity` switch trees — NOT cobra; go.mod has no spf13/cobra).
- Precise stderr wording for the operator-count guard (0 / >1) and source-conflict errors, provided no plaintext secret appears.
- File split to stay ≤600 LOC (e.g. `recover_operator.go` + `recover_operator_password.go` if needed).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Locked requirements
- `.planning/phases/43-operator-break-glass-recovery-and-forgot-password-e2e/43-SPEC.md` — Locked requirements, boundaries, acceptance criteria, edge coverage, prohibitions. MUST read before planning.

### CLI + command surface
- `cmd/aura/main.go` §`switch os.Args[1]` (line 48+) — hand-rolled subcommand dispatch (NOT cobra); `identity` routes to `runIdentity`.
- `cmd/aura/identity.go` — `runIdentity` switch tree + `identityUsage` string; existing `list|get|grant|revoke|recover` branches. Add `recover-operator` here; update usage line.
- `cmd/aura/recovery.go` — existing `identityRecover` (`recover <name>`): token-mint mechanism, `breakGlassAuditEvent = "break_glass_token_minted"` neutral-audit pattern to mirror for D-06.

### Password + Authula reset internals
- `cmd/aura/serve_password_reset.go` §`authulaPasswordResetter.SetPassword` (line 422+) — the online hash→update→delete-sessions flow to model the offline setter on; note it enforces `ErrPasswordResetSamePassword` (which break-glass must NOT — D-02) and requires online `CoreServices`.
- `internal/agui/recovery_hash.go` — `RecoveryHasher{}.HashAnswer` / `VerifyAnswer`, `recoveryAnswerHashVersion = "argon2id-v1"`. Reuse for the re-seed answer hash (D-04).

### Recovery data + forgot-password flow
- `internal/db/queries/identity_recovery.sql` + `internal/db/sqlc/identity_recovery.sql.go` — `UpsertIdentityRecovery` (re-seed) and `LookupRecoveryByEmail` (the INNER JOIN that denies when the row is missing).
- `internal/db/migrations/0023_identity_recovery.up.sql` — `identity_recovery` schema (question + answer_hash + answer_hash_version). No new migration this phase.
- `internal/agui/password_reset.go` — the 4-step online flow (`start`/`question`/`verify`/`complete`) and the deny path the E2E must exercise.
- `cmd/aura/serve_bootstrap.go` / `cmd/aura/serve_onboarding.go` — where recovery is seeded at bootstrap/onboarding; mirror answer hashing + upsert.

### Config + DB
- `internal/config` §`LoadDB` — LLM-free config load used by `runIdentity` (offline, no `OPENROUTER_API_KEY`); the recover-operator command uses the same.
- `scripts/coverage_gate.sh` — the live-`aura` refusal guard to mirror in the test harness (D-08).

### Frontend E2E
- `web/e2e/` — existing Playwright suite layout; add `password-reset.spec.ts`.
- `PasswordResetPanel` (web component under `web/`) — the UI the happy/deny E2E drives.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `agui.RecoveryHasher{}.HashAnswer` — argon2id answer hashing + version tag; reuse directly for the re-seed (no re-implementation).
- `UpsertIdentityRecovery` (sqlc) — idempotent upsert for the recovery row.
- Authula `PasswordService.Hash` + `AccountService` + `SessionService` — reuse for offline hash/update/session-delete (constructed against the pool, not the online server).
- `breakGlassAuditEvent` neutral-audit pattern in `recovery.go` — template for D-06's `operator_password_recovered` event.
- `config.LoadDB` — offline, LLM-free config load already used by `runIdentity`.

### Established Patterns
- Hand-rolled `switch`-tree subcommand dispatch (mirrors `runDB`) — NOT cobra. `recover-operator` follows this.
- Neutral, secret-free audit events for break-glass actions (no token/code/password in the row).
- Host-access-as-admin-proof: offline direct-DB commands require the admin DSNs, no running server, no network.

### Integration Points
- `cmd/aura/identity.go` `runIdentity` switch — add the `recover-operator` branch + usage line.
- Postgres via `config.LoadDB` + `db.Open` pool — the command's only backend; targets live `aura` in prod, throwaway DB under test.
- `LookupRecoveryByEmail` INNER JOIN — restored to returning a row once the re-seed lands (operator already has `identity_auth_links`=1 and `telegram_accounts`=1; only `identity_recovery`=0).

</code_context>

<specifics>
## Specific Ideas

- Industrial break-glass norms applied (from research): meticulous audit logging of every use (who/when/what, never the secret) → D-06; one-time generated-secret display → `--generate` prints once; treat-as-compromised/rotate-after-use → operator chooses a fresh password each run.
- The disposable-DB refusal guard is a test-only safety net born of the 37D-05 live-`aura` wipe; the production command intentionally has no such guard (it must operate on `aura`).

</specifics>

<deferred>
## Deferred Ideas

- **TOTP break-glass reset / re-provision** — locked TOTP is a separate concern; its own phase (SPEC out-of-scope).
- **General per-email / any-identity recovery** — larger risk surface; not this phase (SPEC out-of-scope).
- **Web UI for break-glass recovery** — defeats the "works when the cockpit is unreachable" purpose; CLI only.
- **Boot-time auto-healing of a missing recovery row** — recovery stays an explicit, audited operator action, never silent auth-state self-mutation.

</deferred>

---

*Phase: 43-operator-break-glass-recovery-and-forgot-password-e2e*
*Context gathered: 2026-07-11*
