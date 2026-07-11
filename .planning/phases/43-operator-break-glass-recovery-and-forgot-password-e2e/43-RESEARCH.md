# Phase 43: Operator Break-Glass Recovery + Forgot-Password E2E — Research

**Researched:** 2026-07-11
**Domain:** Offline Go CLI credential recovery (Authula v1.15.0 argon2id) + Playwright E2E of the forgot-password flow
**Confidence:** HIGH (every non-obvious claim is grounded in a repo `file:line` or an exact Authula API signature read from the module cache)

## Summary

This phase is nearly fully specified. Research resolved the concrete unknowns the SPEC left open, and the single biggest one — *how to construct Authula's password/account/session services offline* — turns out to be **already solved in-repo**: `scripts/authula_seed_e2e.go` (build-tag `ignore`) constructs `webauth.New(webauth.Config{DSN, Secret, TrustedOrigins})` with **no running server**, then uses `provider.CoreServices().PasswordService/AccountService/SessionService`. The break-glass command mirrors that construction exactly, plus the online `authulaPasswordResetter.SetPassword` (`cmd/aura/serve_password_reset.go:422`) as the hash→update→delete-sessions template (minus the same-password guard, per D-02).

The **most important planning finding is a CI placement decision** (details in Validation Architecture): `cmd/aura` is **excluded from the 85% owned-surface coverage floor** and its `db_integration` tests **do not execute in any CI job** except via pinned `-run` filters. The existing `cmd/aura/recovery_integration_test.go::TestMintBreakGlassTokenRoundTrip` is *compiled* (`go vet`, ci.yml:391) but **never run**. To satisfy SPEC R6 ("owned-surface coverage ≥ 85%" AND "the `db_integration` test actually runs in CI, fails-not-skips"), the testable break-glass logic **must live in a new `internal/` package** (recommended `internal/breakglass`), whose `db_integration` test runs inside the coverage-gate job (`scripts/coverage_gate.sh` → `go test -tags "db_integration neo4j_integration" ./internal/...`, ci.yml:651). That job must gain one env line: `AURA_AUTHULA_SECRET` (it is absent there today).

**Primary recommendation:** Put break-glass logic in `internal/breakglass` (pure guard/sourcing/generator + a DB-touching orchestrator), keep `cmd/aura/recover_operator.go` as thin flag-glue that calls it; reuse `webauth.New`+`CoreServices` for the Authula legs and `agui.RecoveryHasher{}.HashAnswer` + `UpsertIdentityRecovery` for the re-seed; self-provision a throwaway DB in the `db_integration` test (mirroring `scripts/coverage_docker.sh`); drive the Playwright happy+deny specs with `page.route()` mocks (mirroring `web/e2e/onboarding.spec.ts`).

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (D-01 → D-08)
- **D-01:** New **offline break-glass setter** constructs Authula's account/password/session services against the pool and does: resolve `authula_user_id` via `aura.identity_auth_links` → `PasswordService.Hash` → update `authula.accounts` → `SessionService.DeleteAllByUserID`. Reuse Authula's own `PasswordService.Hash` (do NOT re-implement argon2); do NOT touch the online `authulaPasswordResetter.SetPassword`.
- **D-02:** Break-glass **allows setting the same password** — the new setter MUST NOT enforce `ErrPasswordResetSamePassword` (deliberate availability choice).
- **D-03:** Source recovery question+answer **symmetrically with the password**: `AURA_RECOVERY_QUESTION`+`AURA_RECOVERY_ANSWER` env, hidden interactive prompt (answer hidden+confirmed) as default, `--generate` also generates a random answer paired with a fixed default question, printed once alongside the password.
- **D-04:** Hash the answer by **reusing `agui.RecoveryHasher{}.HashAnswer`** (version `argon2id-v1`) and persist via existing `UpsertIdentityRecovery` sqlc query (idempotent update-in-place).
- **D-05:** Ship `recover-operator` as a **sibling subcommand** alongside `recover <name>`; update `identityUsage` with a one-line disambiguation; leave the existing `recover` path untouched.
- **D-06:** Record a **neutral, secret-free audit event** on success (`operator_password_recovered`), mirroring `break_glass_token_minted`. No plaintext password/answer in the event.
- **D-07:** The `db_integration` test **self-provisions a dedicated disposable database** (admin/migrate DSN → `CREATE DATABASE` throwaway → migrate → seed one operator + `identity_auth_links` + `telegram_accounts` → delete `identity_recovery` → run → assert → `DROP DATABASE` on `t.Cleanup`).
- **D-08:** The test helper **refuses to run against a DB named `aura`** (mirrors `scripts/coverage_gate.sh`). **Scope note:** the refusal is TEST-ONLY; the *command itself* MUST run against live `aura` in production and must carry no name-based refusal.

### Claude's Discretion
- Flag-parsing style: hand-rolled switch tree (mirror `runDB`/`runIdentity`) — NOT cobra (go.mod has no spf13/cobra).
- Exact stderr wording for the 0/>1 operator guard + source-conflict errors (no plaintext secret in any string).
- File split to stay ≤600 LOC (e.g. `recover_operator.go` + `recover_operator_password.go`).

### Deferred Ideas (OUT OF SCOPE)
- TOTP break-glass reset/re-provision (does not touch `authula.totp`).
- General per-email / any-identity recovery — operator-only (`kind='user'`).
- Web UI for break-glass recovery — CLI only.
- Boot-time auto-healing of a missing recovery row — recovery stays an explicit, audited operator action.
</user_constraints>

<phase_requirements>
## Phase Requirements (SPEC R1–R6)

| ID | Description | Research Support |
|----|-------------|------------------|
| R1 | Offline `aura identity recover-operator` resets the operator account (argon2id, Verify-accepted; sessions=0; re-seed recovery; exit 0) | `webauth.New`+`CoreServices` offline construction (`scripts/authula_seed_e2e.go:45-89`); setter template `serve_password_reset.go:422-457`; argon2 params identical (`internal/services/argon2_password_service.go`) |
| R2 | Operator resolution guard — exactly one `kind='user'`; 0/>1 → exit ≠0, no writes | `identity.Store.ListIdentities` (`internal/identity/store.go:84`) filtered by `Kind=="user"` (`store.go:61-64`); kinds `('system','user','channel','service')` (`0004_identity.up.sql:9`); seeded `local` is **kind='system'** (`0004:3`) so it is excluded |
| R3 | Password sourcing (hidden prompt default / `AURA_RECOVERY_PASSWORD` env / `--generate`); conflicts + non-TTY-without-source → exit ≠0 | `golang.org/x/term` (already indirect v0.44.0, go.mod:203) for hidden read + TTY detection; generator template `newPasswordResetToken()` (`serve_password_reset.go:531-537`) |
| R4 | Re-seed `identity_recovery` so `LookupRecoveryByEmail` returns a row again; `--no-recovery` skips+warns | `agui.RecoveryHasher{}.HashAnswer` (`recovery_hash.go:29`) + `UpsertIdentityRecovery` (`identity_recovery.sql.go:343-368`, `ON CONFLICT DO UPDATE`) |
| R5 | Playwright happy+deny of `/api/auth/password-reset/*`; deny is a generic denial that names no factor | `page.route()` mocks (mirror `onboarding.spec.ts`); `/start` is always-200-neutral (`password_reset.go:176-226`); deny surfaces as `login.reset.errors.generic` in the panel (`PasswordResetPanel.tsx:100-101`) |
| R6 | Unit + `db_integration` backend tests, ≥85% owned-surface, `-race` clean, runs-not-skips in CI | **Requires logic in `internal/*`** (coverage gate runs `./internal/...` only, `coverage_gate.sh:52`); throwaway-DB template `coverage_docker.sh:35-61`; no-skip-as-green `envOrSkip` pattern (`authula_integration_test.go:32-43`) |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- **Coverage floor 85% owned-surface** (`internal/*` minus generated/skeleton) across the full tag matrix; a bare unit-only number under 85% is not an acceptable closing metric. `cmd/aura` is **excluded** (CLI glue, covered behaviourally).
- **Coverage gate tag set = `db_integration neo4j_integration` ONLY** — there is no `docker_integration` CI job; code exercised only under other tags counts as UNCOVERED. Daemon/secret-gated runtime code needs daemon-free unit tests for its pure logic.
- **No-skip-as-green in CI:** tagged tiers `t.Fatal` under `$CI` when their env is unset. Verify execution, not just PASS.
- **File ≤600 LOC**, refactor-on-touch (dead-code removal + dupl-folding), no god class.
- **No plaintext secret in logs/argv/errors** (project-wide; also SPEC constraint).
- **Hand-rolled switch dispatch, NOT cobra** (`identity.go:1-16` documents the deviation precedent).
- **Deferred-tool pattern** — N/A here (no new LLM tool).
- **Throwaway-DB discipline** — never run destructive tests against live `aura` (37D-05 / coverage-gate footgun; MEMORY: "Coverage gate nukes live DB").
- **Post-edit validation:** `go vet ./... && go build ./... && go test -race ./internal/<pkg>/` after every Go edit.
- **Verify coverage locally before push** with `bash scripts/coverage_docker.sh` (disposable DB) — a green local full-matrix run beats a push-and-wait cycle.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| CLI dispatch / flag parse / os.Exit | CLI glue (`cmd/aura`) | — | Thin; excluded from coverage floor by design. Mirror `runIdentity` switch. |
| Password/answer sourcing + generator + operator-count guard (pure logic) | `internal/breakglass` (new) | — | Pure functions → unit-testable, counted in the 85% floor, no DB/secret needed. |
| Offline argon2 hash + account update + session delete | `internal/breakglass` → `internal/webauth` (Authula `CoreServices`) | Authula `internal/services.Argon2PasswordService` | Only `webauth.New`→`CoreServices` exposes a working `PasswordService`/`AccountService`/`SessionService`; Authula's concrete argon2 service is in its `internal/` (not importable). |
| `identity_auth_links` resolution + recovery re-seed + neutral audit | `internal/breakglass` (aura `*pgxpool.Pool` + `sqlc`) | `internal/agui` (answer hash) | Aura-owned tables; reuse `sqlc` queries + `agui.RecoveryHasher`. |
| Forgot-password UI flow (happy/deny) | Browser (Playwright) | `page.route()` mock | E2E validates the React panel + neutral-denial rendering; the real backend deny branch is covered separately by R6 backend tests. |

## Standard Stack

### Core (all already in `go.mod` / repo — no new external dependency required)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/Authula/authula` | v1.15.0 | argon2id `PasswordService`, `AccountService`, `SessionService` via `CoreServices()` | The auth provider; only source of a `Verify`-accepting hash `[VERIFIED: go list -m]` |
| `internal/webauth` | in-repo | `webauth.New(Config)` builds the Authula provider **offline** (own `database/sql` pool on `search_path=authula`), `.CoreServices()`, `.Close()` | Exact offline-construction seam already used by serve + E2E seed |
| `internal/agui` | in-repo | `RecoveryHasher{}.HashAnswer` (`argon2id-v1`), `ErrPasswordResetDenied` | Reuse for the re-seed answer hash (D-04); do not re-implement |
| `internal/db` + `internal/db/sqlc` | in-repo | `db.Open/Migrate/EnsureRoles/MigrateSteps`; `UpsertIdentityRecovery`, `LookupRecoveryByEmail`, `InsertIdentityRecoveryAudit` | Aura persistence layer |
| `internal/identity` | in-repo | `Store.ListIdentities` → filter `Kind=="user"` for the guard | Existing identity domain |
| `golang.org/x/term` | v0.44.0 (indirect → promote to direct) | `term.ReadPassword(fd)` hidden read + `term.IsTerminal(fd)` non-TTY detect | Std-adjacent, already in `go.sum` (go.mod:203); no new supply-chain surface |
| `github.com/google/uuid` | in-repo | UUID parse/format for identity IDs | Already used throughout `cmd/aura` |

### Frontend (E2E) — already installed under `web/`
| Library | Version | Purpose |
|---------|---------|---------|
| `@playwright/test` | in `web/node_modules` | E2E runner; `page.route()` network mocking, `webServer: aura serve --only=cli` |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `webauth.New`→`CoreServices` | Import Authula's `Argon2PasswordService` directly | **Impossible** — it lives in `github.com/Authula/authula/internal/services` (Go `internal/` visibility blocks import). `CoreServices()` is the only public path. |
| Reuse `agui.hashArgon2id` for the password | Same argon2 params (`IDKey(pw, salt16, 1, 64KiB, 4, 32)`) | **Rejected** — `agui` encodes as `$aura$argon2id$…$salt$hash`; Authula `Verify` expects `base64.RawStdEncoding(salt‖hash)`. Different envelope → `Verify` would reject. Must use Authula's `PasswordService.Hash`. |
| Real-backend happy-path E2E | Seed a live recovery row, hit real `/api/auth/password-reset/*` | **Rejected for happy path** — the recovery code is Telegram-delivered and unreadable by the browser; no page-level seam exposes it. `page.route()` mock is the established pattern (`onboarding.spec.ts`). |

**Installation:** No `npm install` / `go get` of a *new* external package. Only promote `golang.org/x/term` to a direct require (it is already resolved in `go.sum`):
```bash
go get golang.org/x/term@v0.44.0   # promotes indirect→direct; version already pinned
```

**Version verification:**
- `github.com/Authula/authula v1.15.0` — `[VERIFIED: go list -m]` (`C:\Users\chett\go\pkg\mod\github.com\!authula\authula@v1.15.0`), matches SPEC.
- `golang.org/x/term v0.44.0` — `[VERIFIED: go.mod:203 + go.sum:1101]` (already present as indirect).

## Package Legitimacy Audit

> No new external package is introduced. Every dependency is already vendored/resolved in this repo's `go.mod`/`go.sum` and is used by shipped code.

| Package | Registry | Age | In-repo use | Disposition |
|---------|----------|-----|-------------|-------------|
| `github.com/Authula/authula@v1.15.0` | Go proxy | shipped since Phase 28 | `internal/webauth`, `cmd/aura/serve_*` | Approved (pre-existing, pinned) |
| `golang.org/x/term@v0.44.0` | Go proxy | golang.org/x | already indirect (telebot dep chain) | Approved (promote to direct) |
| `github.com/google/uuid` | Go proxy | shipped | throughout | Approved (pre-existing) |

**Packages removed due to slopcheck [SLOP] verdict:** none.
**Packages flagged [SUS]:** none. *(slopcheck not run — zero net-new packages, so all deps are pre-existing pinned modules already gated by CI `govulncheck`; no `[ASSUMED]` package names introduced.)*

## Architecture Patterns

### System Architecture Diagram

```
                       aura identity recover-operator  (host = admin proof, offline)
                                        │
                    ┌───────────────────┼─────────────────────────────┐
                    │ cmd/aura/recover_operator.go (THIN GLUE)         │
                    │  parse flags (--generate --no-recovery)          │
                    │  config.LoadDB() → cfg.DB, AuthulaSecret,        │
                    │                    AuthulaDatabaseURL            │
                    └───────────────────┬─────────────────────────────┘
                                        │ calls
                    ┌───────────────────▼─────────────────────────────┐
                    │ internal/breakglass  (OWNED, coverage-measured)  │
                    │                                                  │
                    │  (1) source password/answer  ── x/term prompt ── │──▶ stdin (hidden)
                    │      / AURA_RECOVERY_* env / --generate(rand)     │──▶ stdout (--generate, ONCE)
                    │      conflict/non-TTY/empty → error (no secret)   │
                    │                                                  │
                    │  (2) guard: ListIdentities → filter kind='user'  │──▶ aura.identities
                    │      count 0/>1 → error, ZERO writes             │
                    │                                                  │
                    │  (3) setter: resolve authula_user_id ───────────│──▶ aura.identity_auth_links
                    │      webauth.New(DSN,Secret) → CoreServices()     │
                    │        PasswordService.Hash(pw)                  │──▶ authula.accounts (UPDATE)
                    │        SessionService.DeleteAllByUserID          │──▶ authula.sessions (DELETE→0)
                    │        AccountService.Update(account)            │
                    │      (NO same-password guard — D-02)             │
                    │                                                  │
                    │  (4) re-seed: RecoveryHasher.HashAnswer(answer)  │
                    │      UpsertIdentityRecovery(id,q,hash,ver)        │──▶ aura.identity_recovery (UPSERT)
                    │      unless --no-recovery (warn)                 │
                    │                                                  │
                    │  (5) audit: InsertIdentityRecoveryAudit          │──▶ aura.identity_recovery_audit
                    │      event="operator_password_recovered"         │    (NO secret in payload)
                    └───────────────────┬──────────────────────────────┘
                                        │ restores
                                        ▼
     online forgot-password:  /start → /question → /verify → /complete
     LookupRecoveryByEmail INNER JOIN (identities⨝auth_links⨝identity_recovery⨝telegram_accounts)
     now returns a row again → Telegram code delivery resumes.
```

### Recommended Project Structure
```
cmd/aura/
├── recover_operator.go            # thin glue: flags, config.LoadDB, call internal/breakglass, os.Exit
├── recover_operator_test.go       # unit: flag/dispatch wiring (optional; glue is behaviourally covered)
├── identity.go                    # EDIT: add `case "recover-operator":` + usage line (D-05)
internal/breakglass/
├── breakglass.go                  # orchestrator: RecoverOperator(ctx, deps) — guard→setter→reseed→audit
├── source.go                      # password/answer sourcing (prompt/env/generate) — PURE where possible
├── source_test.go                 # unit: conflict matrix, non-TTY, empty/whitespace, generate length/charset
├── guard.go                       # selectSoleOperator([]Identity) — PURE, unit-testable 0/1/>1
├── guard_test.go
├── setter.go                      # offline Authula reset (webauth.New→CoreServices, no same-pw guard)
├── breakglass_integration_test.go # //go:build db_integration — self-provision throwaway DB (D-07/D-08)
web/e2e/
└── password-reset.spec.ts         # happy + deny (page.route mocks)
```

### Pattern 1: Offline Authula service construction (THE key unknown — SOLVED)
**What:** Build a working `PasswordService`/`AccountService`/`SessionService` with no running server.
**When to use:** The break-glass setter (step 3).
**Example (verbatim seam, already in-repo):**
```go
// Source: scripts/authula_seed_e2e.go:41-89 (build-tag ignore) — the exact offline pattern.
dsn := strings.TrimSpace(os.Getenv("AURA_AUTHULA_DATABASE_URL"))
if dsn == "" { dsn = dbURL } // AURA_DB_URL; webauth forces ?search_path=authula
provider, err := webauth.New(webauth.Config{
    DSN:            dsn,
    Secret:         os.Getenv("AURA_AUTHULA_SECRET"),      // 64 hex chars, REQUIRED
    TrustedOrigins: []string{"http://127.0.0.1:9080"},     // unused offline; any valid value
})
if err != nil { /* ... */ }
defer provider.Close()                                     // stops Authula expiry workers (goleak-clean)
core := provider.CoreServices()                            // *authulaservices.CoreServices
// core.PasswordService.Hash(pw) (string,error); core.AccountService.GetByUserIDAndProvider / Update;
// core.SessionService.DeleteAllByUserID(ctx, authulaUserID)
```
**Note:** `webauth.New` builds Authula's OWN `database/sql` pool (not the aura pgxpool) and runs Authula's bun migrations into the isolated `authula` schema at construction (`internal/webauth/authula.go:88-158`). The aura `*pgxpool.Pool` is still needed separately (identity resolution, `identity_auth_links` lookup, re-seed, audit).

### Pattern 2: The reset itself (template + the D-02 deviation)
**Example (model on the online resetter, DROP the same-password guard):**
```go
// Source: cmd/aura/serve_password_reset.go:422-457 (online authulaPasswordResetter.SetPassword).
// Break-glass version OMITS lines 442-444 (the ErrPasswordResetSamePassword check) per D-02.
var authulaUserID string
err := auraPool.QueryRow(ctx,
    `SELECT authula_user_id FROM aura.identity_auth_links WHERE identity_id=$1`,
    pgtype.UUID{Bytes: id, Valid: true}).Scan(&authulaUserID)          // missing → clear error, no writes
account, err := core.AccountService.GetByUserIDAndProvider(ctx, authulaUserID,
    authulamodels.AuthProviderEmail.String())                          // nil account → clear error
hash, err := core.PasswordService.Hash(password)                       // argon2id, Verify-accepted
account.Password = &hash
if err := core.SessionService.DeleteAllByUserID(ctx, authulaUserID); err != nil { /*...*/ }  // sessions→0
_, err = core.AccountService.Update(ctx, account)                      // persists the new hash
```
**Ordering note:** the online path deletes sessions **before** `AccountService.Update`. Preserve that order (delete-then-update) so a mid-op failure never leaves a live session on a changed password.

### Pattern 3: Recovery re-seed (mirror bootstrap/onboarding)
```go
// Source: internal/agui/recovery_hash.go:29 + internal/db/sqlc/identity_recovery.sql.go:343-368.
hash, version, err := agui.RecoveryHasher{}.HashAnswer(answer)         // version == "argon2id-v1"
err = sqlc.New(auraPool).UpsertIdentityRecovery(ctx, sqlc.UpsertIdentityRecoveryParams{
    IdentityID:        pgtype.UUID{Bytes: id, Valid: true},
    Question:          question,
    AnswerHash:        hash,
    AnswerHashVersion: version,
})   // ON CONFLICT (identity_id) DO UPDATE — idempotent, never a duplicate row (edge: idempotency)
```

### Pattern 4: Neutral audit event (D-06)
```go
// Source: cmd/aura/recovery.go:107-112 (break_glass_token_minted) + identity_recovery.sql.go:173-208.
_, err := sqlc.New(auraPool).InsertIdentityRecoveryAudit(ctx, sqlc.InsertIdentityRecoveryAuditParams{
    IdentityID: pgtype.UUID{Bytes: id, Valid: true},
    Event:      "operator_password_recovered",   // NEW neutral event; NO password/answer in Metadata
})   // RequestIpHash/UserAgentHash/Metadata left zero → Metadata defaults to '{}'::jsonb
```

### Pattern 5: CLI dispatch (D-05)
```go
// Source: cmd/aura/identity.go:49-63 (switch) + :29 (identityUsage). Add a sibling branch.
case "recover-operator":
    identityRecoverOperator(ctx, pool, cfg, args[1:])   // NEW; leave "recover" (:58-59) untouched
// identityUsage becomes: "...|recover <name>|recover-operator}" with a one-line disambiguation.
```
`cmd/aura/main.go:73-74` already routes `identity` → `runIdentity`; **no `main.go` edit needed** — the new branch is added inside `runIdentity`'s switch only.

### Anti-Patterns to Avoid
- **Re-implementing argon2 or reusing `agui.hashArgon2id` for the password** — produces a hash `Argon2PasswordService.Verify` rejects (wrong encoding envelope). Use `core.PasswordService.Hash`.
- **Adding a name-based `aura`-refusal to the command** — D-08 forbids it; the command MUST run against live `aura`. The refusal is test-only.
- **Putting the testable logic in `cmd/aura`** — it won't count toward the 85% floor and its `db_integration` test won't run in CI (see Validation Architecture).
- **Logging the password/answer, or passing it as an argv/`fmt.Errorf` value** — the only sanctioned emission is the single `--generate` stdout line.
- **Forgetting `provider.Close()`** — leaves Authula expiry-cleanup goroutines running (goleak failure under `-race`).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| argon2id password hashing | custom `argon2.IDKey` + encoding | `core.PasswordService.Hash` (Authula) | Only encoding `Verify` accepts (`base64.RawStdEncoding(salt‖hash)`, `argon2_password_service.go`) |
| Session invalidation | `DELETE FROM authula.sessions …` by hand | `core.SessionService.DeleteAllByUserID` | Authula owns its schema; a raw delete risks drift with plugin bookkeeping |
| Recovery answer hashing | custom argon2 + version tag | `agui.RecoveryHasher{}.HashAnswer` | Exact parity with bootstrap/onboarding + the online `VerifyAnswer` (`recovery_hash.go`) |
| Recovery-row upsert | `INSERT … ON CONFLICT` inline | `sqlc.UpsertIdentityRecovery` | Generated, tested, idempotent (`identity_recovery.sql.go:343`) |
| Hidden password prompt / TTY detection | raw termios / ANSI escapes | `golang.org/x/term` `ReadPassword`/`IsTerminal` | Cross-platform (Windows dev host too), already a resolved dependency |
| Strong random password/answer | ad-hoc `math/rand` | `crypto/rand` + `base64.RawURLEncoding` (template `newPasswordResetToken`, `serve_password_reset.go:531`) | CSPRNG; ≥20 chars trivially (`rand.Read(24 bytes)` → 32 chars) |
| Throwaway DB create/drop | bespoke harness | mirror `scripts/coverage_docker.sh:44-61` (`CREATE/DROP DATABASE … WITH (FORCE)` via superuser) + `db.Migrate` | Proven pattern; carries the live-`aura` refusal (D-08) |
| Offline Authula wiring | reconstruct plugins/pool | `webauth.New(Config)` | Encapsulates schema isolation + migrations + `Close()`; used by serve + `authula_seed_e2e.go` |

**Key insight:** every "hard" primitive this phase needs is already shipped and tested in-repo. The net-new code is orchestration + sourcing + the CLI branch + tests. Treat this as *wiring existing seams*, not building.

## Runtime State Inventory

> This is a brownfield feature whose whole purpose is to repair runtime state. Categories below describe what the *command mutates at runtime* and what the *phase itself* must be careful about.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `aura.identity_recovery` (0 rows for the live operator `dvdmarchetto@gmail.com` / `b130c94d-…` — the lockout); `authula.accounts.password` (updated); `authula.sessions` (deleted); `aura.identity_recovery_audit` (append) | The command re-seeds `identity_recovery` (data write), updates the account hash, deletes sessions, appends one audit row. No migration. |
| Live service config | Authula runs **in-process** inside `aura serve` — it has NO separate container/UI state. Its data lives entirely in the `authula.*` Postgres schema (isolated via `search_path=authula`, `webauth/authula.go`). | None — no external service config to patch. `webauth.New` builds a transient in-process provider for the command's lifetime. |
| OS-registered state | None. The command is a one-shot host CLI; it registers nothing (no scheduler task, no daemon). | None — verified: `cmd/aura` subcommands are stateless one-shots. |
| Secrets/env vars | Command READS `AURA_AUTHULA_SECRET` (64 hex), `AURA_AUTHULA_DATABASE_URL` (or derives from `AURA_DB_URL`), `POSTGRES_*`/`AURA_DB_URL`/`AURA_DB_MIGRATE_URL`; NEW input vars `AURA_RECOVERY_PASSWORD`, `AURA_RECOVERY_QUESTION`, `AURA_RECOVERY_ANSWER` (D-03). None persisted; none logged. | Document the READ set in `--help`/docs. The three new `AURA_RECOVERY_*` are ephemeral inputs, not stored config — do NOT add to the config knob catalog as persistent. |
| Build artifacts | None new. Existing `cmd/aura/recovery_integration_test.go` (`db_integration`) is a **latent, never-run** test — see Pitfall 1. | Do not model the new test on a test that doesn't actually run; ensure the new `db_integration` test lands where CI executes it. |

**The canonical question — after the command runs, what stale state remains?** Nothing: the re-seed restores `LookupRecoveryByEmail`, the session delete invalidates every stale cookie, and the account hash is updated in place. The operator's Telegram link (`telegram_accounts`, present) and `identity_auth_links` (present) are untouched — only the missing `identity_recovery` row is the gap this closes.

## Common Pitfalls

### Pitfall 1: The `db_integration` test compiles but never runs in CI
**What goes wrong:** A `//go:build db_integration` test placed in `cmd/aura` (like the existing `recovery_integration_test.go`) is only `go vet`-compiled (ci.yml:391) and executed solely via pinned `-run` filters in the musr-e2e/memory/integrations jobs. `TestMintBreakGlassTokenRoundTrip` is **never actually run in CI**. A new test there would silently not-run, violating SPEC R6.
**Why it happens:** The coverage gate runs `./internal/...` only (`coverage_gate.sh:52`); the `integration-test` job runs `./internal/db/... ./internal/cron/... ./internal/agui/...` (ci.yml:255) — neither includes `./cmd/aura/...` unfiltered.
**How to avoid:** Put the tested logic in `internal/breakglass`; its `db_integration` test then runs inside the coverage-gate job. Add `AURA_AUTHULA_SECRET` to that job's env (see Validation Architecture → Wave 0).
**Warning signs:** A sub-second "integration" runtime, or the test only appearing in a `go vet` step.

### Pitfall 2: coverage-gate job lacks `AURA_AUTHULA_SECRET`
**What goes wrong:** An `internal/breakglass` `db_integration` test that constructs `webauth.New` `t.Fatal`s under `$CI` when `AURA_AUTHULA_SECRET` is unset (correct no-skip-as-green behavior) → **fails the coverage-gate job**.
**Why it happens:** The `knowledge-integration-test` job (which runs `coverage_gate.sh`, ci.yml:545-651) exports `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`/`POSTGRES_PASSWORD` but **not** `AURA_AUTHULA_SECRET`.
**How to avoid:** Add one env line to that job: `AURA_AUTHULA_SECRET: 00000000000000000000000000000000000000000000000000000000000000a1` (the same dummy used in ci.yml:221, :351, :1224). Safe: no other package reads it in a `db_integration` test.
**Warning signs:** `webauth: AURA_AUTHULA_SECRET must be 64 hex characters` in the gate log.

### Pitfall 3: argon2 encoding mismatch (Verify rejects)
**What goes wrong:** Producing the hash with `agui.hashArgon2id` (or a custom encoder) yields `$aura$argon2id$…`, which `Argon2PasswordService.Verify` cannot decode → login stays broken though the command "succeeded".
**Why it happens:** Same KDF params, different serialization. Authula = `base64.RawStdEncoding(salt16‖hash32)`; agui = a `$`-delimited PHC-like string.
**How to avoid:** Always route the *password* through `core.PasswordService.Hash`. The *recovery answer* is a separate concern and correctly uses `agui.RecoveryHasher` (that's what the online `VerifyAnswer` reads).
**Warning signs:** The argon2 round-trip test (R1) fails `Verify`.

### Pitfall 4: `CREATE DATABASE` privilege + connection DB
**What goes wrong:** The self-provisioning test (D-07) runs `CREATE DATABASE` on the `aura_app` or `aura_migrate` DSN, or while connected to the target DB → error (needs CREATEDB and the maintenance DB).
**Why it happens:** `aura_app`/`aura_migrate` are not superusers; `CREATE DATABASE` can't run inside the DB being created or in a txn.
**How to avoid:** Mirror `coverage_docker.sh:52-60` — connect as the superuser role `aura` (compose `postgres://aura:$POSTGRES_PASSWORD@host:port/postgres`) to the `postgres` maintenance DB, `CREATE DATABASE "<throwaway>" OWNER aura_migrate`, then point the app/migrate DSNs at it and `db.Migrate`. `recovery_integration_test.go:59-60` already composes this superuser bootstrap DSN from `POSTGRES_PASSWORD`.
**Warning signs:** `permission denied to create database` or `CREATE DATABASE cannot run inside a transaction block`.

### Pitfall 5: leaking the secret via error strings or `--generate` echo
**What goes wrong:** Wrapping the password into `fmt.Errorf("…%s", pw)`, or printing it on any path other than the single `--generate` stdout line.
**Why it happens:** Convenience error context.
**How to avoid:** Never interpolate the password/answer into errors or slog. The generated value prints exactly once to `os.Stdout` (mirror `recovery.go:53-55`'s "stdout only — NEVER slog" discipline). Test by capturing stderr+slog and asserting the plaintext is absent (see Validation Architecture).
**Warning signs:** grep of a captured log buffer finds the plaintext.

### Pitfall 6: multiple `kind='user'` identities on a real deployment
**What goes wrong:** Phase 28 multi-user can enroll a 2nd web-loginable identity; the guard then refuses (`>1` → exit ≠0) and the operator can't recover.
**Why it happens:** SPEC R2 deliberately resolves *exactly one* operator; ambiguity is a hard stop by design.
**How to avoid:** This is intended behavior, not a bug — surface a clear stderr message ("N user identities found; break-glass targets a single operator"). Document that a multi-user deployment must disambiguate out-of-band (not in scope). Note this in the Assumptions/Open-Questions for the operator.
**Warning signs:** exit ≠0 with ">1" on a system that legitimately has two users.

## Code Examples

### Hidden password prompt with non-TTY detection (R3)
```go
// Source: golang.org/x/term (v0.44.0, already resolved). Std pattern.
import "golang.org/x/term"

func promptHidden(label string) (string, error) {
    fd := int(os.Stdin.Fd())
    if !term.IsTerminal(fd) {
        return "", errors.New("no TTY: set AURA_RECOVERY_PASSWORD or pass --generate") // R3 non-TTY exit
    }
    fmt.Fprint(os.Stderr, label+": ")           // prompt to stderr, keep stdout clean
    b, err := term.ReadPassword(fd)              // hidden; no echo
    fmt.Fprintln(os.Stderr)
    return string(b), err
}
```

### Strong generated password (R3 `--generate`, ≥20 chars, printed once)
```go
// Source: template cmd/aura/serve_password_reset.go:531-537 (newPasswordResetToken).
func generateSecret(nBytes int) (string, error) { // nBytes=24 → 32 base64url chars (>20)
    b := make([]byte, nBytes)
    if _, err := rand.Read(b); err != nil { return "", err } // crypto/rand
    return base64.RawURLEncoding.EncodeToString(b), nil
}
// … applied value printed EXACTLY once to os.Stdout, never slog.
```

### Operator-count guard as a pure function (R2 — unit-testable, counted in floor)
```go
// Filter the existing ListIdentities result; no new sqlc query needed.
func selectSoleOperator(ids []identity.Identity) (identity.Identity, error) {
    var users []identity.Identity
    for _, id := range ids { if id.Kind == "user" { users = append(users, id) } } // kind set: 0004:9
    switch len(users) {
    case 0: return identity.Identity{}, errors.New("no operator (kind='user') identity found")
    case 1: return users[0], nil
    default: return identity.Identity{}, fmt.Errorf("%d operator identities found; refusing (break-glass targets one)", len(users))
    }
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Passphrase cookie auth | Embedded Authula v1.15.0 (`internal/webauth`) | Phase 28 | Password lives in `authula.accounts`; reset must go through Authula services |
| No offline reset | `recover <name>` mints a short-lived token (0023 infra) | Phase 36 (MUSR-06/D-16) | This phase adds the *sibling* `recover-operator` (direct password reset, not a token) |
| — | `identity_recovery` + Telegram forgot-password 4-step flow | Phase 36 | The flow this phase re-seeds + E2E-covers |

**Deprecated/outdated:** none relevant. Note the MEMORY entry "Migration 0026 retire-safe / DO NOT wipe the DB" — old DB-wipe advice is obsolete; this phase adds **no migration** and must not wipe anything.

## Validation Architecture

> `workflow.nyquist_validation: true` (`.planning/config.json`) — section required.

### Test Framework
| Property | Value |
|----------|-------|
| Backend framework | Go `testing` (std) + table tests; `-race` mandatory; `goleak` where goroutines spawn (Authula `provider.Close()`) |
| Backend config file | none — `go test` |
| Quick run (unit) | `go test -race ./internal/breakglass/` |
| Full (integration) | `go test -tags db_integration -race -p 1 ./internal/breakglass/` (stack up; throwaway DB) |
| Local full-matrix + coverage | `bash scripts/coverage_docker.sh` (disposable `aura_cov`, drops on exit) |
| E2E framework | Playwright (`web/playwright.config.ts`), `webServer: aura serve --only=cli`, `page.route()` mocks |
| E2E run | `cd web && npx playwright test password-reset.spec.ts` |

### Observable signals → SPEC acceptance criteria
| Acceptance criterion | Observable signal | Tier |
|----------------------|-------------------|------|
| argon2 hash is Verify-accepted (R1, prohibition) | after run, read `authula.accounts.password` (via `AccountService.GetByUserIDAndProvider`) and assert `core.PasswordService.Verify(newPw, *account.Password) == true` | `db_integration` |
| sessions invalidated (R1/R4, prohibition) | `SELECT count(*) FROM authula.sessions WHERE user_id=$1` == 0 after run (seed ≥1 session via `SessionService.Create` first) | `db_integration` |
| recovery re-seeded (R4) | `GetIdentityRecoveryByIdentity` returns non-empty `answer_hash`+`answer_hash_version`; `LookupRecoveryByEmail(email)` returns a row (was `pgx.ErrNoRows`) | `db_integration` |
| operator guard (R2) | seed 0 users → non-zero return + `SELECT count(*)` on `authula.accounts`/`identity_recovery` unchanged; seed 2 → same; seed 1 → proceeds | unit (guard) + `db_integration` (no-write) |
| source conflict / non-TTY / empty (R3) | function returns error for {env+generate}, {non-TTY,no env,no generate}, {empty/whitespace} — before any DB call | unit (no DB) |
| `--generate` prints once, ≥20 chars (R3) | captured stdout has exactly one secret line, `len ≥ 20`, `[A-Za-z0-9_-]` charset | unit (capture stdout) |
| no plaintext in logs (R3, prohibition) | capture `slog` sink + stderr into a buffer; assert `!strings.Contains(buf, plaintext)` | unit + `db_integration` |
| idempotent re-seed (edge) | run twice → `SELECT count(*) FROM aura.identity_recovery WHERE identity_id=$1` == 1 | `db_integration` |
| missing `authula.accounts` row (edge) | operator has `identity_auth_links` but no Authula account → clear error, no partial write | `db_integration` |
| audit event, no secret (D-06) | `SELECT event, metadata FROM aura.identity_recovery_audit` has `operator_password_recovered`, metadata `{}` (no secret) | `db_integration` |
| deny reveals no factor (R5) | E2E: after `/start`, notice text == `"If that account has recovery enabled, check Telegram now."`; on `/question` deny, error == `login.reset.errors.generic`; `page.locator('body').innerHTML()` contains neither `identity_recovery` nor `telegram` as a factor name | Playwright |
| happy path lands on login (R5) | E2E: `/complete` mock → panel shows `doneTitle` "Password updated" → "Back to sign in" returns to login | Playwright |

### Phase Requirements → Test Map
| Req | Behavior | Test type | Command | Exists? |
|-----|----------|-----------|---------|---------|
| R1 | argon2 round-trip + session-kill + reset | `db_integration` | `go test -tags db_integration -race -p1 ./internal/breakglass/ -run TestRecoverOperator` | ❌ Wave 0 |
| R2 | guard 0/1/>1 (pure) | unit | `go test -race ./internal/breakglass/ -run TestSelectSoleOperator` | ❌ Wave 0 |
| R3 | sourcing matrix (pure) | unit | `go test -race ./internal/breakglass/ -run TestSourceSecret` | ❌ Wave 0 |
| R4 | re-seed + idempotency + `--no-recovery` | `db_integration` | `…-run TestRecoverOperatorReseed` | ❌ Wave 0 |
| R5 | happy + deny UI | Playwright | `npx playwright test password-reset.spec.ts` | ❌ Wave 0 |
| R6 | deny branch generic denial (backend) | unit (fake store returns `ErrPasswordResetDenied`) + `db_integration` | `…-run TestDenyGeneric` | ❌ Wave 0 (backend deny is already partly covered by `agui` `password_reset` tests; add the no-leak assertion) |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test -race ./internal/breakglass/` (unit, <5s).
- **Per wave merge:** `bash scripts/coverage_docker.sh` (full `db_integration neo4j_integration` matrix on a disposable DB; owned-surface ≥85%).
- **Phase gate:** full matrix green + `cd web && npx playwright test password-reset.spec.ts` green + `-race` clean.

### Sampling adequacy (Nyquist)
The command is one-shot and deterministic; there is no periodic/streaming signal requiring high-rate sampling. The "signal" is a set of discrete DB state transitions — each is directly observed post-run (SELECT assertions), which is the maximal sampling rate. No aliasing risk.

### Wave 0 Gaps (do FIRST)
- [ ] `internal/breakglass/` package skeleton (so its `db_integration` test is picked up by `coverage_gate.sh ./internal/...`).
- [ ] **CI:** add `AURA_AUTHULA_SECRET: …a1` to the `knowledge-integration-test` job env (ci.yml ~line 556) — otherwise the new `db_integration` test `t.Fatal`s the coverage gate. *(Also consider `AURA_AUTHULA_DATABASE_URL` unset → derived; leave unset so it derives from the throwaway `AURA_DB_URL`.)*
- [ ] `internal/breakglass/breakglass_integration_test.go` — throwaway-DB harness with the D-08 `aura`-name refusal + `envOrSkip`-style `t.Fatal`-under-`$CI` guard (copy `authula_integration_test.go:32-43` + `coverage_docker.sh:44-47`).
- [ ] `web/e2e/password-reset.spec.ts` — happy + deny (the web-e2e job is path-filtered on `web/**`, ci.yml:1246-1249, so the new spec auto-triggers it).
- [ ] Decide the mutation spot-check file (below) and add a `VALIDATION.md` Manual-Only row.

### Mutation-testing spot-check target
`internal/breakglass/setter.go` (the reset orchestration: delete-then-update ordering, missing-account/missing-link early returns, no same-password guard) — **and** `internal/breakglass/source.go` (the conflict/non-TTY/empty decision tree, highest branch density). Target ≥70% killed via `go-mutesting` (WSL, `GOFLAGS=-tags=db_integration`), per CLAUDE.md.

### How to assert the no-leak prohibitions
- **No plaintext in logs:** install a `slog` handler writing to a `bytes.Buffer` (and/or redirect the command's stderr writer to a buffer), run the full flow with a known sentinel password/answer, assert `!bytes.Contains(buf.Bytes(), []byte(sentinel))`. Do this in BOTH a unit test (sourcing/error paths) and the `db_integration` test (full run).
- **Deny is byte-identical to the generic denial:** in the backend deny test, assert the returned error `errors.Is(err, agui.ErrPasswordResetDenied)` and the HTTP body equals the neutral `{status:"ok"}` (`/start`) or the generic 4xx the panel maps to `login.reset.errors.generic`. In the E2E, capture the deny-path notice/error text and assert it is character-for-character the same constant used on a configured account's `/start`, and that the DOM never contains a factor name.

## Security Domain

> `security_enforcement` not set to `false` → included.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control (in this phase) |
|---------------|---------|----------------------------------|
| V2 Authentication | yes | argon2id via Authula `PasswordService` (never hand-rolled); break-glass gated by host access (admin proof) |
| V3 Session Management | yes | `SessionService.DeleteAllByUserID` invalidates all sessions on reset (prohibition: no prior session stays valid) |
| V4 Access Control | yes | operator-only (`kind='user'`), single-resolution guard; no web/network surface (offline CLI) |
| V5 Input Validation | yes | empty/whitespace password+answer rejected pre-write; UUID parse on identity IDs; parameterized `sqlc`/pgx (no string SQL) |
| V6 Cryptography | yes | `crypto/rand` for generated secrets; argon2id delegated to Authula/`agui`; **never** re-implement |
| V7 Errors & Logging | yes | neutral audit event, secret-free; no plaintext in slog/stderr/argv/errors |
| V8 Data Protection | yes | secret shown once (`--generate`), treat-as-compromised/rotate-after-use norm |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Account enumeration via forgot-password | Information disclosure | `/start` always returns neutral `{status:"ok"}` (`password_reset.go:176-226`); deny never names a factor (R5) |
| Secret leakage in logs/argv | Information disclosure | stdout-only single emission; buffer-scan test asserts absence |
| Break-glass abuse (unaudited) | Repudiation | neutral append-only `operator_password_recovered` audit (D-06) |
| Destructive test against live `aura` | Tampering / data loss | D-08 test-only name refusal + throwaway DB (37D-05 footgun; MEMORY "Coverage gate nukes live DB") |
| SQL injection on the direct-DB path | Tampering | canon — parameterized `sqlc`/pgx everywhere; no string-built SQL (SPEC prohibition table: dismissed→canon) |
| Stale session survives reset | Elevation of privilege | delete-then-update ordering; assert `authula.sessions`=0 post-run |

## Answers to the 8 Research Questions (grounded)

1. **Offline Authula construction.** Use `webauth.New(webauth.Config{DSN, Secret, TrustedOrigins})` → `provider.CoreServices()` → `PasswordService`/`AccountService`/`SessionService` — no `aura serve`, no online `CoreServices` plumbing. Exact template: `scripts/authula_seed_e2e.go:41-89`. `webauth.New` builds Authula's **own** `database/sql` pool over `?search_path=authula` and runs Authula's bun migrations at construction (`internal/webauth/authula.go:88-158`); it REQUIRES `AURA_AUTHULA_SECRET` (64 hex) and returns a `*Provider` you must `Close()`. `authula_user_id` is resolved with `SELECT authula_user_id FROM aura.identity_auth_links WHERE identity_id=$1` (`serve_password_reset.go:432-435`); session deletion is `core.SessionService.DeleteAllByUserID(ctx, authulaUserID)` (`serve_password_reset.go:450`; interface `services/core.go:38`). Import paths: `authulaservices "github.com/Authula/authula/services"`, `authulamodels "github.com/Authula/authula/models"`. `CoreServices` struct fields: `UserService, AccountService, SessionService, VerificationService, TokenService, PasswordService` (`services/core.go:64-71`).

2. **Argon2 round-trip proof.** `PasswordService.Hash(password string) (string, error)` and `PasswordService.Verify(password, encoded string) bool` (`services/core.go:59-62`). Concrete impl `Argon2PasswordService` (`github.com/Authula/authula/internal/services/argon2_password_service.go`): `salt=16` random, `argon2.IDKey(pw, salt, 1, 64*1024, 4, 32)`, stored as `base64.RawStdEncoding.EncodeToString(salt‖hash)`. Test: after the setter runs, re-read the account (`AccountService.GetByUserIDAndProvider(ctx, uid, authulamodels.AuthProviderEmail.String())`) and assert `core.PasswordService.Verify(newPw, *account.Password) == true`. This satisfies the prohibition "MUST NOT produce a hash Verify rejects."

3. **Recovery re-seed.** `agui.RecoveryHasher{}.HashAnswer(answer string) (hash, version string, err error)` returns `version == "argon2id-v1"` (`internal/agui/recovery_hash.go:17,29-39`; it normalizes + rejects empty). Persist via `sqlc.UpsertIdentityRecovery(ctx, UpsertIdentityRecoveryParams{IdentityID pgtype.UUID, Question, AnswerHash, AnswerHashVersion string})` (`identity_recovery.sql.go:343-368`) — `ON CONFLICT (identity_id) DO UPDATE` → idempotent update-in-place. `LookupRecoveryByEmail` (`:296-341`) INNER-JOINs `identities⨝identity_auth_links⨝identity_recovery⨝telegram_accounts` on `kind='user'` and `lower(name)=lower($1)` — it returns rows again once the upsert lands (operator already has `identity_auth_links`=1 + `telegram_accounts`=1).

4. **Password/answer sourcing.** No existing hidden-TTY helper in `cmd/aura` (the `chat_repl.go` prompt is a plaintext `bufio.Reader`, not hidden). Use `golang.org/x/term` (already indirect v0.44.0, go.mod:203): `term.ReadPassword(int(os.Stdin.Fd()))` for hidden read, `term.IsTerminal(int(os.Stdin.Fd()))` for non-TTY detection. Strong-random generator template: `newPasswordResetToken()` (`serve_password_reset.go:531-537`) = `crypto/rand.Read([32]byte)` + `base64.RawURLEncoding` (43 chars); for a ≥20-char password use `rand.Read(24 bytes)` → 32 chars.

5. **Throwaway-DB harness (D-07/D-08).** Closest analog: `cmd/aura/recovery_integration_test.go` (`//go:build db_integration`) — composes a superuser bootstrap DSN from `POSTGRES_PASSWORD` (`:59`), `db.EnsureRoles` (`:60`), `db.Migrate(migrateURL)` (`:63`), `db.Open` (`:66`). Throwaway CREATE/DROP template: `scripts/coverage_docker.sh:44-61` — connect as superuser `aura` to the `postgres` maintenance DB, `CREATE DATABASE "<name>" OWNER aura_migrate`, `DROP DATABASE … WITH (FORCE)` on cleanup, and **refuse if the name is `aura`** (`:44-47`; also `coverage_gate.sh:35-42`). CI vars the tier reads (composed DSNs) so it fails-not-skips under `$CI`: `AURA_DB_URL`, `AURA_DB_MIGRATE_URL` (ci.yml:334-335, :556-557), `POSTGRES_PASSWORD` — plus **`AURA_AUTHULA_SECRET`** for the Authula legs (present in the integration/musr/web-e2e jobs, **absent** in the coverage-gate/knowledge job → must be added). Helpers: `db.Migrate(ctx, migrateURL) (int,error)` (`internal/db/migrate.go:41`), `db.EnsureRoles(ctx, bootstrapURL, password) error` (`:95`), `db.MigrateSteps(ctx, migrateURL, n) error` (`internal/db/migrate_steps.go:20`), `db.Open(ctx, *db.Config) (*pgxpool.Pool, error)` (`internal/db/db.go:29`).

6. **Neutral audit (D-06).** `cmd/aura/recovery.go:32` defines `breakGlassAuditEvent = "break_glass_token_minted"`; it is written via `sqlc.InsertIdentityRecoveryAudit(ctx, InsertIdentityRecoveryAuditParams{IdentityID: consumed.IdentityID, Event: breakGlassAuditEvent})` (`recovery.go:107-112`) into table `aura.identity_recovery_audit` (`identity_recovery.sql.go:173-208`). Mirror exactly with `Event: "operator_password_recovered"`, only `IdentityID`+`Event` set (Metadata defaults to `'{}'::jsonb`), no secret.

7. **Playwright E2E.** Harness: `web/playwright.config.ts` launches `aura serve --only=cli` as `webServer`, `baseURL http://127.0.0.1:9080`, `serviceWorkers:'block'` (so `page.route()` sees every request). Established mocking pattern: `web/e2e/onboarding.spec.ts:45-162` fully mocks `/api/*` via `page.route(...).fulfill(...)`. Panel: `web/src/auth/PasswordResetPanel.tsx` drives 4 steps (`start→code→answer→complete→done`) against `web/src/auth/passwordResetApi.ts` (`/api/auth/password-reset/{start,question,verify,complete}`). Reach it from `LoginPage.tsx:403,408` (`login.authula.forgotPassword` = "Forgot password?") on an **unauthenticated** context (do NOT use `gotoAuthenticated`). **Telegram mock = mock `/start`** (which in prod triggers Telegram); the "code" the test types is whatever the `/question` mock accepts. Happy: mock all four → assert `doneTitle` "Password updated". Deny: `/start` always returns 200 `{status:"ok"}` (`password_reset.go:176-226`) → panel shows neutral notice `"If that account has recovery enabled, check Telegram now."`; then `/question`→deny → panel shows `login.reset.errors.generic` "Couldn't reset the password…". Assert the notice/error text is the generic constant and `body.innerHTML()` names no factor. State seeding is unnecessary for the mocked spec (page.route supplies all responses); the web-e2e job already seeds an operator via `go run ./scripts/authula_seed_e2e.go` (ci.yml:1277) if a real leg is ever added.

8. **CLI dispatch.** `cmd/aura/main.go:73-74` routes `case "identity": runIdentity(os.Args[2:])` — unchanged. Inside `runIdentity` (`cmd/aura/identity.go:49-63`), add a sibling `case "recover-operator":` next to `case "recover":` (`:58-59`, which calls `identityRecover(ctx, store, pool, args[1:])` from `recovery.go`). Update `identityUsage` (`:29`) to `"...|recover <name>|recover-operator}"` with the D-05 one-line disambiguation. The command loads config via `config.LoadDB()` (`identity.go:38`) — which already populates `AuthulaSecret` + `AuthulaDatabaseURL` (`internal/config/config.go:462-464`), so no LLM key is required.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | go.mod pinned | — |
| Postgres stack (`aura` + `authula` schema) | command + `db_integration` | ✓ (WSL `127.0.0.1:5432`, `make db-up`/`neo4j-migrate`) | — | — |
| `AURA_AUTHULA_SECRET` (64 hex) | offline Authula provider | ✓ locally (.env) / CI dummy `…a1` | — | none — HARD requirement of `webauth.New` |
| `golang.org/x/term` | hidden prompt | ✓ (go.sum) | v0.44.0 | none needed |
| Node + Playwright | E2E | ✓ (`web/node_modules`, CI `web-e2e` job) | — | — |
| Neo4j / mcp-neo4j-cypher | coverage-gate job co-tenant | ✓ (job already provisions) | — | not used by this phase's code |

**Missing dependencies with no fallback:** `AURA_AUTHULA_SECRET` must be present wherever the break-glass `db_integration` test runs — **add it to the `knowledge-integration-test` job env** (it is currently absent there).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Placing logic in `internal/breakglass` + adding `AURA_AUTHULA_SECRET` to the coverage-gate job is the intended way to satisfy R6 (coverage + runs-in-CI). CONTEXT leaves file layout to Claude's discretion but does not name the package or the CI edit. | Validation Architecture | If the planner instead keeps logic in `cmd/aura`, R6's "runs in CI" + "≥85% owned-surface" are NOT met without a different CI step. `[ASSUMED]` — confirm placement with the planner. |
| A2 | The mocked-`page.route()` E2E (both happy+deny) satisfies R5; the real backend deny branch is covered by R6 backend tests. SPEC says "Telegram delivery mocked" and the deny is UI-text-only. | Q7 / Validation | If R5 is read as requiring a *real* backend deny, the deny spec should instead seed an operator with no recovery row and hit the live `/start` (feasible; happy path still needs the mock). `[ASSUMED]` |
| A3 | The break-glass command legitimately requires `AURA_AUTHULA_SECRET` + an Authula DSN at runtime (like `aura serve`), beyond just the Postgres admin DSNs. D-01/SPEC "host-only/offline… Postgres admin DSNs" does not mention the Authula secret, but `webauth.New` mandates it. | Q1 / Environment | If the operator runs the command without the secret set, it exits with the webauth error. Must be documented in `--help`. `[VERIFIED via webauth.New]` but the *SPEC omission* is the assumption. |
| A4 | `--generate` producing a 32-char `base64url` value meets "≥20-char strong random password". | Q4 / Code Examples | If a specific charset/complexity policy is later required, adjust the generator. `[ASSUMED]` |
| A5 | Filtering `ListIdentities` by `Kind=="user"` in Go is an acceptable guard implementation (no new sqlc query). | Q2 / R2 | If a direct `WHERE kind='user'` query is preferred for atomicity, add one (needs `sqlc generate`). Low risk. `[ASSUMED]` |

**These need user/planner confirmation before becoming locked:** A1 (package + CI edit) and A2 (E2E mock vs real deny) are the two decisions worth surfacing in plan-check.

## Open Questions (RESOLVED)

> **Resolved during planning (2026-07-11):** Q1 → superseded by **DC-1** — `AURA_AUTHULA_SECRET` is already a workflow-level env at `ci.yml:18` (inherited by the coverage-gate job), so there is NO CI-job edit; the real fix is the LOCAL `scripts/coverage_docker.sh` export (Plan 43-02 Task 3). Q2 → **accepted-as-designed** — R2 refuses `>1 kind='user'` by SPEC, independent of the current deployment's operator count; the guard behavior does not change based on the answer (it is surfaced as a live-deploy operational note, not a plan gap). Q3 → **adopted** — Plan 43-04 asserts the DOM never renders the typed new password (parity with `onboarding.spec.ts:248-251`).

1. **Does the coverage-gate job owner accept adding `AURA_AUTHULA_SECRET`?**
   - Known: the dummy secret `…a1` is already used in three other jobs; adding it is mechanically safe.
   - Unclear: whether the maintainer prefers running the break-glass `db_integration` test in the `integration-test` job (which already has the secret) instead, at the cost of it not counting toward the internal coverage floor.
   - Recommendation: add the one env line to `knowledge-integration-test` (keeps R6's coverage + run-in-CI in one place). Surface in plan-check.

2. **Multi-user deployments (>1 `kind='user'`).**
   - Known: R2 refuses `>1` by design.
   - Unclear: whether the live deployment currently has more than the single operator (Phase 28 could enroll a 2nd). If so, break-glass would refuse.
   - Recommendation: emit a precise stderr message and document the out-of-band disambiguation; do not expand scope.

3. **Should `web/e2e/password-reset.spec.ts` also assert the DOM never renders the typed new password (parity with `onboarding.spec.ts:248-251`)?**
   - Recommendation: yes — cheap, high-value no-leak assertion; add `expect(body.innerHTML()).not.toContain(NEW_PASSWORD)`.

## Sources

### Primary (HIGH confidence — read this session)
- Repo code (file:line cited inline): `cmd/aura/serve_password_reset.go`, `cmd/aura/recovery.go`, `cmd/aura/identity.go`, `cmd/aura/main.go`, `cmd/aura/recovery_test.go`, `cmd/aura/recovery_integration_test.go`, `internal/agui/recovery_hash.go`, `internal/agui/password_reset.go`, `internal/webauth/authula.go`, `internal/webauth/authula_integration_test.go`, `internal/db/sqlc/identity_recovery.sql.go`, `internal/db/config.go`, `internal/config/config.go`, `internal/identity/store.go`, `internal/db/migrations/0004_identity.up.sql`, `0012_telegram.up.sql`, `scripts/authula_seed_e2e.go`, `scripts/coverage_gate.sh`, `scripts/coverage_docker.sh`, `scripts/go_packages.sh`, `.github/workflows/ci.yml`, `web/playwright.config.ts`, `web/e2e/onboarding.spec.ts`, `web/src/auth/PasswordResetPanel.tsx`, `web/src/auth/passwordResetApi.ts`, `web/src/routes/LoginPage.tsx`, `web/src/i18n/resources.login.ts`.
- Authula v1.15.0 module cache (`…\!authula\authula@v1.15.0`): `services/core.go` (interfaces + `CoreServices` struct), `internal/services/argon2_password_service.go` (argon2 params + encoding).
- `go list -m github.com/Authula/authula` → v1.15.0; `go.mod` (`golang.org/x/term v0.44.0`), `go.sum`.

### Secondary (MEDIUM)
- `.planning/config.json` (nyquist_validation, security), `CLAUDE.md` (coverage floor, tag set, no-skip-as-green), MEMORY entries (coverage-gate live-DB footgun).

### Tertiary (LOW)
- none — every claim was verifiable against repo code or the Authula module.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all deps are in-repo and read at source; Authula signatures verified from the module cache.
- Architecture / offline construction: HIGH — the exact seam already exists (`authula_seed_e2e.go`) and is exercised in CI.
- CI placement / coverage: HIGH — traced every `go test` invocation in `ci.yml`; confirmed `cmd/aura` `db_integration` tests do not run and the coverage gate is `./internal/...` only.
- E2E approach: MEDIUM-HIGH — pattern verified against `onboarding.spec.ts`; the mock-vs-real deny choice (A2) is a judgment the planner should confirm.
- Pitfalls: HIGH — each is grounded in a specific `file:line` or prior incident.

**Research date:** 2026-07-11
**Valid until:** ~2026-08-10 (stable — brownfield, pinned deps). Re-check only if Authula is bumped past v1.15.0 or the CI job structure changes.

## RESEARCH COMPLETE
