# Phase 43: Operator Break-Glass Recovery + Forgot-Password E2E — Pattern Map

**Mapped:** 2026-07-11
**Files analyzed:** 11 (7 new · 4 modified)
**Analogs found:** 11 / 11 (every file has a concrete in-repo analog; two files compose two analogs each)

> Consolidated from `43-RESEARCH.md` (Patterns 1–5 + "Don't Hand-Roll" + "Recommended Project Structure").
> Every `file:line` below was **re-read against the live codebase this session** and confirmed to still match.
> **Two research claims drifted from the live tree — see `## Drift Corrections` (read FIRST; one affects the D-09 CI edit).**

---

## Drift Corrections (research vs. live codebase — planner MUST read)

### DC-1 — `AURA_AUTHULA_SECRET` is ALREADY present in the coverage-gate job (contradicts D-09 / Pitfall 2)

RESEARCH (D-09, Pitfall 2, Environment Availability) asserts the `knowledge-integration-test` job "lacks `AURA_AUTHULA_SECRET`" and that one env line must be added so the new `db_integration` test does not `t.Fatal` the gate.

**Live cross-check contradicts this.** `AURA_AUTHULA_SECRET` is declared at the **workflow-level `env:` block** — `.github/workflows/ci.yml:16-18`:

```yaml
env:
  AURA_ACCESS_TOKEN: ci-not-a-secret
  AURA_AUTHULA_SECRET: 00000000000000000000000000000000000000000000000000000000000000a1   # line 18 — WORKFLOW-LEVEL
```

GitHub Actions inherits workflow-level `env` into **every job**. The `knowledge-integration-test` job (`ci.yml:545`, its `env:` at `:549-589`) does not redefine or unset it, so it inherits `…a1` at runtime. `coverage_gate.sh` (run at `ci.yml:651`) inherits the job's environment. The per-job copies at `ci.yml:221`, `:351`, `:1224` the research cited as "the three jobs that have it" are **redundant re-declarations** of the already-global value.

**Consequence for the planner:** the D-09 CI edit is **very likely a no-op in CI** — the secret is already inherited. Adding a job-level line to `knowledge-integration-test` is harmless (same value) but not functionally required. **Verify** by confirming the workflow-level `env:` block still carries `AURA_AUTHULA_SECRET` at plan time; if kept, drop or downgrade the "must add or the gate t.Fatals" framing.

**The real, still-open gap is LOCAL, not CI.** `scripts/coverage_docker.sh` (the local full-matrix wrapper) sets `export CI=true` (`coverage_docker.sh:85`) but reads only `POSTGRES_PASSWORD` + `NEO4J_PASSWORD` from `.env` (`:28-29`) — it does **not** export `AURA_AUTHULA_SECRET`. So a local `bash scripts/coverage_docker.sh` run of the new `internal/breakglass` `db_integration` test will `t.Fatal` (no-skip-as-green under `CI=true`) unless the ambient shell already exports `AURA_AUTHULA_SECRET`. The planner should either (a) add `AURA_AUTHULA_SECRET` to the `read_secret`/export set in `coverage_docker.sh`, or (b) document that operators must export it before the local gate. This is the substantive CI/local edit R6 actually needs — not the CI job env.

### DC-2 — `identity.Identity` has a 4th field `Deactivated` (guard example is stale)

RESEARCH's guard example (`selectSoleOperator`, Code Examples) treats `identity.Identity` as `{ID, Name, Kind}`. Live `internal/identity/store.go:61-69` adds a 4th field:

```go
type Identity struct {
    ID   string
    Name string
    Kind string
    Deactivated bool  // 0029 deactivated_at IS NOT NULL ⇒ true (HI-02 auth-boundary deny)
}
```

**Consequence for the planner (guard.go):** decide whether `selectSoleOperator` counts a `Deactivated` operator toward the "exactly one `kind='user'`" tally. A deactivated-but-present operator would still satisfy `kind=='user'`; break-glass recovering a soft-deleted identity may or may not be desired. Low risk, but call it out in the guard's unit-test matrix (add a `Deactivated:true` row). Not a blocker; `ListIdentities` still returns the field so the guard has the data.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/breakglass/breakglass.go` | service (orchestrator) | request-response (guard→setter→reseed→audit) | `cmd/aura/recovery.go` `mintBreakGlassToken` (63-118) | role-match |
| `internal/breakglass/setter.go` | service | CRUD (hash→delete-sessions→update) | `scripts/authula_seed_e2e.go` (41-94) **+** `cmd/aura/serve_password_reset.go` `SetPassword` (422-457) | exact (analog pair) |
| `internal/breakglass/source.go` | utility | transform / request-response (prompt/env/generate) | `cmd/aura/serve_password_reset.go` `newPasswordResetToken` (531-537); hidden-prompt = **no analog** (RESEARCH Code Examples) | partial |
| `internal/breakglass/guard.go` | utility | transform (pure filter 0/1/>1) | `internal/identity/store.go` `ListIdentities` (84) as source; filter is net-new (RESEARCH Code Examples) | partial |
| `internal/breakglass/*_test.go` (source/guard) | test (unit) | unit | `cmd/aura/recovery_test.go` table-test shape | role-match |
| `internal/breakglass/breakglass_integration_test.go` | test (`//go:build db_integration`) | db_integration (throwaway DB) | `cmd/aura/recovery_integration_test.go` (31-66) **+** `scripts/coverage_docker.sh` (44-61) | role-match (analog pair) |
| `cmd/aura/recover_operator.go` | controller (CLI glue) | request-response (flag glue → os.Exit) | `cmd/aura/recovery.go` `identityRecover` (37-56) | exact |
| `cmd/aura/identity.go` (EDIT) | controller (dispatch) | request-response | itself: switch (49-63) + `identityUsage` (29) | exact (in-file) |
| `.github/workflows/ci.yml` (EDIT) | config (CI) | batch | per-job env at `:221`/`:351`/`:1224` — **but see DC-1** | exact-pattern / premise-questioned |
| `web/e2e/password-reset.spec.ts` | test (E2E) | event-driven (browser) | `web/e2e/onboarding.spec.ts` (45-162, 248-251) | exact |
| `go.mod` (EDIT) | config | — | direct-require block `golang.org/x/*` (36-40) | exact |

---

## Pattern Assignments

### `internal/breakglass/setter.go` (service, CRUD) — the highest-value analog pair

**Analog A — offline Authula service construction** (`scripts/authula_seed_e2e.go:41-94`, build-tag `ignore`). This is THE key unknown the research resolved: build working `PasswordService`/`AccountService`/`SessionService` with **no running server**.

```go
// scripts/authula_seed_e2e.go:41-59 — verbatim offline seam.
dsn := strings.TrimSpace(os.Getenv("AURA_AUTHULA_DATABASE_URL"))
if dsn == "" { dsn = dbURL }                              // derive from AURA_DB_URL; webauth forces ?search_path=authula
provider, err := webauth.New(webauth.Config{
    DSN:            dsn,
    Secret:         requiredEnv("AURA_AUTHULA_SECRET"),   // 64 hex — HARD requirement of webauth.New
    TrustedOrigins: []string{"http://127.0.0.1:9080", "https://127.0.0.1:9080"}, // unused offline; any valid value
})
if err != nil { /* ... */ }
defer provider.Close()                                     // MUST — stops Authula expiry workers (goleak-clean under -race)
core := provider.CoreServices()
// core.PasswordService.Hash / .Verify · core.AccountService.GetByUserIDAndProvider / .Update · core.SessionService.DeleteAllByUserID
```

Import aliases used in-repo: `authulamodels "github.com/Authula/authula/models"` (provider string = `authulamodels.AuthProviderEmail.String()`). `webauth.New` builds Authula's OWN `database/sql` pool + runs its bun migrations at construction — the aura `*pgxpool.Pool` is still needed **separately** for identity resolution / auth-link lookup / re-seed / audit.

**Analog B — the reset sequence** (`cmd/aura/serve_password_reset.go:422-457`, online `authulaPasswordResetter.SetPassword`). Copy the ordering; **OMIT lines 442-444** (the same-password guard) per D-02.

```go
// serve_password_reset.go:431-455 — resolve → (SKIP same-pw guard) → hash → delete-sessions → update.
var authulaUserID string
if err := pool.QueryRow(ctx,
    `SELECT authula_user_id FROM aura.identity_auth_links WHERE identity_id=$1`,
    pgtype.UUID{Bytes: id, Valid: true}).Scan(&authulaUserID); err != nil { /* missing link → clear error, no writes */ }
account, err := core.AccountService.GetByUserIDAndProvider(ctx, authulaUserID, authulamodels.AuthProviderEmail.String())
// if account == nil → clear error, NO partial write (edge: missing authula.accounts row)

// D-02: DO NOT copy serve_password_reset.go:442-444:
//   if account.Password != nil && core.PasswordService.Verify(password, *account.Password) {
//       return agui.ErrPasswordResetSamePassword }        // <-- OMIT: break-glass allows same password

hash, err := core.PasswordService.Hash(password)           // argon2id, Verify-accepted (NEVER agui.hashArgon2id — DC/Pitfall 3)
account.Password = &hash
if err := core.SessionService.DeleteAllByUserID(ctx, authulaUserID); err != nil { /* ... */ } // sessions→0 FIRST
if _, err := core.AccountService.Update(ctx, account); err != nil { /* ... */ }                // THEN persist new hash
```

**Ordering constraint:** delete-sessions **before** `Update`, exactly as the online path — a mid-op failure must never leave a live session on a changed password.

---

### `internal/breakglass/breakglass.go` (service, orchestrator)

**Analog:** `cmd/aura/recovery.go` `mintBreakGlassToken` (63-118) — the in-repo template for "compose several DB writes + a neutral audit into one flow." Reuse its shape: begin → do the work → append audit → commit; return the sourced secret only to the caller (never log it). Steps 3-5 of the flow (setter / re-seed / audit) are the body.

**Re-seed leg (Pattern 3)** — `internal/agui/recovery_hash.go:29` + `internal/db/sqlc/identity_recovery.sql.go:343-368`:

```go
hash, version, err := agui.RecoveryHasher{}.HashAnswer(answer)   // version == "argon2id-v1" (recovery_hash.go:17,29-39; normalizes + rejects empty)
err = sqlc.New(auraPool).UpsertIdentityRecovery(ctx, sqlc.UpsertIdentityRecoveryParams{
    IdentityID:        pgtype.UUID{Bytes: id, Valid: true},
    Question:          question,
    AnswerHash:        hash,
    AnswerHashVersion: version,
})   // identity_recovery.sql.go:343-351 — ON CONFLICT (identity_id) DO UPDATE → idempotent, never a duplicate row
```

> **Envelope discipline (Pitfall 3, re-confirmed):** `agui.RecoveryHasher` writes `$aura$argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>` (`recovery_hash.go:78-80`). Authula's `Verify` expects `base64.RawStdEncoding(salt‖hash)`. They are **not interchangeable** — the *password* MUST go through `core.PasswordService.Hash`; only the *recovery answer* uses `agui.RecoveryHasher`.

**Audit leg (Pattern 4, D-06)** — `cmd/aura/recovery.go:32,107-112` + `internal/db/sqlc/identity_recovery.sql.go:173-208`:

```go
// recovery.go:32 defines: breakGlassAuditEvent = "break_glass_token_minted"  ← mirror with a NEW neutral event.
_, err := sqlc.New(auraPool).InsertIdentityRecoveryAudit(ctx, sqlc.InsertIdentityRecoveryAuditParams{
    IdentityID: pgtype.UUID{Bytes: id, Valid: true},
    Event:      "operator_password_recovered",   // NEW; only IdentityID+Event set → Metadata defaults to '{}'::jsonb (sql.go:177). NO secret.
})
```

**Re-seed restores forgot-password:** `LookupRecoveryByEmail` (`identity_recovery.sql.go:296-341`) INNER-JOINs `identities ⨝ identity_auth_links ⨝ identity_recovery ⨝ telegram_accounts` on `kind='user'` — it returns 0 rows *only because `identity_recovery` is empty*; the upsert makes it return a row again (operator already has `identity_auth_links`=1 + `telegram_accounts`=1).

---

### `internal/breakglass/source.go` (utility, transform) — partial analog

**Generator** has a template — `cmd/aura/serve_password_reset.go:531-537`:

```go
func newPasswordResetToken() (string, error) {
    var b [32]byte
    if _, err := rand.Read(b[:]); err != nil { return "", err }   // crypto/rand
    return base64.RawURLEncoding.EncodeToString(b[:]), nil          // 43 chars; use 24 bytes → 32 chars for a ≥20-char password
}
```

**Hidden prompt + non-TTY detection has NO in-repo analog** (RESEARCH Q4: `cmd/aura/chat_repl.go` uses a plaintext `bufio.Reader`, not hidden). Use `golang.org/x/term` per RESEARCH Code Examples — `term.IsTerminal(int(os.Stdin.Fd()))` + `term.ReadPassword(fd)`; prompt to **stderr**, keep stdout clean for the `--generate` line. The conflict matrix (`env`+`--generate` → error; non-TTY+no-source → error; empty/whitespace → error, before any DB call) is net-new pure logic — build from the RESEARCH `selectSoleOperator`/`promptHidden` examples, unit-test the full branch matrix. **Highest branch density → mutation spot-check target.**

---

### `internal/breakglass/guard.go` (utility, pure) — partial analog

**Source of truth:** `internal/identity/store.go:84` `ListIdentities(ctx) ([]Identity, error)`. The pure filter is net-new (RESEARCH Code Examples `selectSoleOperator`). Kind enum is fixed by `internal/db/migrations/0004_identity.up.sql:9` — `CHECK (kind IN ('system','user','channel','service'))`; the seeded `local` row is **`kind='system'`** (`0004:28`), so filtering `kind=='user'` correctly excludes it. See **DC-2**: also decide the `Deactivated` semantics.

```go
func selectSoleOperator(ids []identity.Identity) (identity.Identity, error) {
    var users []identity.Identity
    for _, id := range ids { if id.Kind == "user" { users = append(users, id) } }  // 'local' is 'system' → excluded
    switch len(users) {
    case 0: return identity.Identity{}, errors.New("no operator (kind='user') identity found")
    case 1: return users[0], nil
    default: return identity.Identity{}, fmt.Errorf("%d operator identities found; refusing (break-glass targets one)", len(users))
    }
}
```

---

### `internal/breakglass/breakglass_integration_test.go` (test, `db_integration`) — analog pair, ONE trap

**Analog A — no-skip-as-green env guard** (`cmd/aura/recovery_integration_test.go:31-41`). Copy this verbatim (it is the sanctioned pattern):

```go
func recoveryEnvOrSkip(t *testing.T, key string) string {
    t.Helper()
    v := os.Getenv(key)
    if v == "" {
        if os.Getenv("CI") != "" { t.Fatalf("integration test requires %s, but it is unset under CI", key) }  // fails-not-skips
        t.Skipf("integration test requires %s; set it and re-run", key)
    }
    return v
}
```

Helpers to reuse (all confirmed): `db.EnsureRoles(ctx, bootstrapURL, pwd)`, `db.Migrate(ctx, migrateURL) (int,error)`, `db.Open(ctx, &db.Config{URL})` (`recovery_integration_test.go:60-66`).

**Analog B — throwaway DB CREATE/DROP + live-`aura` refusal** (`scripts/coverage_docker.sh:44-61`). Port this to Go for D-07/D-08:

```bash
# coverage_docker.sh:44-60 — the pattern the Go harness mirrors.
if [ "$COV_DB" = "aura" ]; then echo "FATAL: must not be 'aura' …" >&2; exit 4; fi   # D-08 refusal (TEST-ONLY)
pg_admin() { docker exec -i "$PG_CONTAINER" psql -U aura -d postgres "$@"; }           # superuser → 'postgres' maintenance DB
pg_admin -c "DROP DATABASE IF EXISTS \"$COV_DB\" WITH (FORCE)"
pg_admin -c "CREATE DATABASE \"$COV_DB\" OWNER aura_migrate"                           # owner aura_migrate so CREATE SCHEMA succeeds
trap '… DROP DATABASE … WITH (FORCE)' EXIT                                             # → t.Cleanup(DROP)
```

> **TRAP — do NOT model on the *seed* half of `recovery_integration_test.go`.** That test composes its bootstrap DSN against the **live `aura` DB** (`recovery_integration_test.go:59` → `…/aura?sslmode=disable`) and seeds into it. That is the 37D-05 footgun the SPEC explicitly forbids (Prohibitions row 4). Take only its **env-guard + helper calls**; take the **throwaway CREATE/DROP + `aura`-name refusal** from `coverage_docker.sh`. `CREATE DATABASE` needs the **superuser role connected to the `postgres` maintenance DB** (Pitfall 4) — `aura_app`/`aura_migrate` cannot run it.

> **Placement is load-bearing (Pitfall 1):** this file MUST live under `internal/breakglass/` so `scripts/coverage_gate.sh` (`./internal/...`) runs it. `cmd/aura/*_integration_test.go` files are `go vet`-compiled but **never executed** in CI — `recovery_integration_test.go::TestMintBreakGlassTokenRoundTrip` is proof (latent, never-run). See DC-1 for the secret-availability correction.

---

### `cmd/aura/recover_operator.go` (controller, CLI glue)

**Analog:** `cmd/aura/recovery.go` `identityRecover` (37-56) — the thin resolve→call→print→exit shape. Keep it flag-glue only: parse `--generate`/`--no-recovery`, `config.LoadDB()`, call `internal/breakglass`, `os.Exit`. Config load is already LLM-free and populates the Authula fields (`identity.go:38` `config.LoadDB()` → `AuthulaSecret`+`AuthulaDatabaseURL`, `internal/config/config.go` per RESEARCH Q8), so no `OPENROUTER_API_KEY`.

```go
// recovery.go:53-55 — the ONLY sanctioned secret emission discipline: stdout, once, NEVER slog.
fmt.Printf("ok: minted … for %s (expires in %s)\n", name, breakGlassTokenTTL)
fmt.Println(token)   // one-time value → os.Stdout only
```

Split to a second file (`recover_operator_password.go`) only if needed to stay ≤600 LOC.

---

### `cmd/aura/identity.go` (controller, EDIT) — in-file pattern

Add a sibling `case` in the existing switch (`identity.go:49-63`) and extend `identityUsage` (`:29`). `cmd/aura/main.go` already routes `identity` → `runIdentity` — **no `main.go` edit**.

```go
// identity.go:58-59 (existing, leave untouched):
case "recover":
    identityRecover(ctx, store, pool, args[1:])
// ADD sibling (D-05):
case "recover-operator":
    identityRecoverOperator(ctx, pool, cfg, args[1:])   // NEW glue → internal/breakglass
// identity.go:29 identityUsage → append "|recover-operator" + a one-line disambiguation
//   ("recover <name> = mint token to hand a user · recover-operator = offline operator password reset + recovery re-seed").
```

---

### `web/e2e/password-reset.spec.ts` (test, E2E) — near-exact structural analog

**Analog:** `web/e2e/onboarding.spec.ts` (45-162 mock install; 248-251 no-leak DOM assertion). Mock every `/api/auth/password-reset/*` via `page.route(...).fulfill(...)`; branch on request body when needed (onboarding.spec.ts:103-139 shows intent-based branching).

```ts
// onboarding.spec.ts:46-52 — the fulfill shape to copy.
await page.route('**/api/auth/password-reset/start', (route) =>
  route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ status: 'ok' }) }));
// onboarding.spec.ts:248-251 — the no-leak assertion to copy (RESEARCH Open Q3 → include NEW_PASSWORD too).
const html = await page.locator('body').innerHTML();
expect(html).not.toContain(NEW_PASSWORD);
```

**UI + backend facts (confirmed):**
- Deny surfaces in the panel as `setError('login.reset.errors.generic')` — `web/src/auth/PasswordResetPanel.tsx:100-101` (the `catch` of `/question`). Assert that constant; assert `body.innerHTML()` names no factor (`identity_recovery`/`telegram`).
- Backend `/start` is **always** neutral `{Status:"ok"}` even on deny — `internal/agui/password_reset.go:176-226` (deny path returns `ok` at `:191`). The E2E only mocks; the real deny branch is covered by the R6 backend tests.
- Reach the panel from `LoginPage` "Forgot password?" on an **unauthenticated** context (do NOT use `gotoAuthenticated`, unlike onboarding.spec.ts:165). The web-e2e job is path-filtered on `web/**` (auto-triggers on the new spec).

---

### `go.mod` (config, EDIT)

Promote `golang.org/x/term v0.44.0` from the indirect block (`go.mod:203 // indirect`) into the direct `require` block alongside the other `golang.org/x/*` (`go.mod:36-40`). Mechanically: `go get golang.org/x/term@v0.44.0` (version already pinned in `go.sum`; no new supply-chain surface).

---

## Shared Patterns

### No secret in logs / argv / errors (SPEC prohibition; R3)
**Sources:** `cmd/aura/recovery.go:53-55` ("stdout only — NEVER slog"); `web/e2e/onboarding.spec.ts:248-251` (DOM no-leak).
**Apply to:** `source.go`, `setter.go`, `breakglass.go`, `recover_operator.go`, `password-reset.spec.ts`.
Never interpolate the password/answer into `fmt.Errorf`/`slog`. The generated value prints exactly once to `os.Stdout` (`--generate`). Assert absence by capturing a `slog` buffer + stderr and `!bytes.Contains(buf, sentinel)` in both a unit and the `db_integration` test.

### No-skip-as-green env guard (R6)
**Source:** `cmd/aura/recovery_integration_test.go:31-41` (`recoveryEnvOrSkip`).
**Apply to:** `breakglass_integration_test.go`. `t.Fatal` under `$CI` when a required var is unset; skip locally. Reads `POSTGRES_PASSWORD`/`AURA_DB_URL`/`AURA_DB_MIGRATE_URL` + `AURA_AUTHULA_SECRET`.

### Throwaway-DB + live-`aura` refusal (D-07/D-08; SPEC prohibition 4)
**Source:** `scripts/coverage_docker.sh:44-61`.
**Apply to:** `breakglass_integration_test.go` **only** (test-only refusal; the *command* must run against live `aura` with NO refusal).

### Neutral, secret-free audit event (D-06)
**Source:** `cmd/aura/recovery.go:32,107-112` + `internal/db/sqlc/identity_recovery.sql.go:173-208`.
**Apply to:** `breakglass.go`. `Event:"operator_password_recovered"`, `IdentityID` only, Metadata `'{}'`.

### argon2 envelope discipline (R1; Pitfall 3)
**Sources:** Authula `core.PasswordService.Hash` (via `webauth`) for the **password**; `internal/agui/recovery_hash.go:78-80` (`$aura$…` envelope) for the **answer**. The two encodings are not interchangeable — route each secret through its own hasher.
**Apply to:** `setter.go` (password → Authula) + `breakglass.go` (answer → `agui`).

### Offline, LLM-free config load
**Source:** `cmd/aura/identity.go:38` `config.LoadDB()`.
**Apply to:** `recover_operator.go`. No `OPENROUTER_API_KEY`; the DB + Authula fields are enough.

---

## No Analog Found

| File / concern | Role | Data Flow | Reason |
|----------------|------|-----------|--------|
| `source.go` hidden-prompt + non-TTY detection | utility | transform | No hidden-TTY read exists in `cmd/aura` (`chat_repl.go` is plaintext `bufio.Reader`). Use `golang.org/x/term` per RESEARCH Code Examples. |
| `source.go` sourcing-conflict decision tree | utility | transform | Net-new pure logic (env+generate / non-TTY / empty). Build from RESEARCH examples; unit-test the full matrix. |
| `guard.go` pure `selectSoleOperator` | utility | transform | The filter itself is net-new (trivial); its data source `ListIdentities` (store.go:84) is the analog. |

---

## Metadata

**Analog search scope:** `cmd/aura/`, `internal/{breakglass(new),agui,identity,db,db/sqlc,webauth,config}`, `scripts/`, `.github/workflows/`, `web/e2e/`, `web/src/auth/`, `internal/db/migrations/`.
**Files scanned this session (re-read + confirmed):** `43-SPEC.md`, `cmd/aura/identity.go`, `cmd/aura/recovery.go`, `cmd/aura/serve_password_reset.go` (415-537), `cmd/aura/recovery_integration_test.go` (1-90), `scripts/authula_seed_e2e.go`, `scripts/coverage_docker.sh`, `internal/agui/recovery_hash.go`, `internal/agui/password_reset.go` (170-234), `internal/identity/store.go` (40-109), `internal/db/sqlc/identity_recovery.sql.go` (170-368), `internal/db/migrations/0004_identity.up.sql`, `.github/workflows/ci.yml` (1-30, 545-660 + grep), `web/e2e/onboarding.spec.ts`, `web/src/auth/PasswordResetPanel.tsx` (85-114), `go.mod` (1-40, 203).
**Drift corrections:** 2 (DC-1 CI secret already global — affects D-09; DC-2 `identity.Identity.Deactivated`).
**Pattern extraction date:** 2026-07-11

---

## PATTERN MAPPING COMPLETE

**Phase:** 43 - operator-break-glass-recovery-and-forgot-password-e2e
**Files classified:** 11 (7 new · 4 modified)
**Analogs found:** 11 / 11 (2 files use an analog *pair*; 3 sourcing/guard concerns are net-new pure logic with a data-source analog)

### Coverage
- Files with exact analog: 5 (`recover_operator.go`, `identity.go`, `password-reset.spec.ts`, `go.mod`, `setter.go` [pair])
- Files with role-match analog: 4 (`breakglass.go`, `breakglass_integration_test.go`, unit tests, `ci.yml`)
- Files with partial / net-new logic: 2 (`source.go`, `guard.go` — data-source analog only)

### Key Patterns Identified
- **Offline Authula reset = `webauth.New`→`CoreServices` (`authula_seed_e2e.go`) + the online `SetPassword` sequence minus the same-password guard (`serve_password_reset.go:422-457`, omit :442-444).** Delete-sessions before Update.
- **Every "hard" primitive is already shipped:** argon2 (`core.PasswordService.Hash`), session-kill (`SessionService.DeleteAllByUserID`), answer hash (`agui.RecoveryHasher`), idempotent upsert (`sqlc.UpsertIdentityRecovery`), neutral audit (`InsertIdentityRecoveryAudit`), throwaway DB (`coverage_docker.sh`). Net-new = orchestration + sourcing + CLI branch + tests.
- **Placement is load-bearing:** testable logic in `internal/breakglass` (counts toward the 85% floor AND its `db_integration` test runs in `coverage_gate.sh`); `cmd/aura` glue is excluded + never run in CI.
- **E2E mirrors `onboarding.spec.ts` exactly:** `page.route().fulfill()` mocks + `body.innerHTML()` no-leak assertion; deny = the generic `login.reset.errors.generic` constant, no factor named.

### Two decisions to surface in plan-check
- **DC-1:** `AURA_AUTHULA_SECRET` is already a workflow-level env (`ci.yml:18`) inherited by the coverage-gate job — the D-09 CI edit is likely redundant. The real gap is **local** (`coverage_docker.sh` sets `CI=true` but doesn't export the secret). Re-scope D-09 accordingly.
- **DC-2:** `identity.Identity.Deactivated` (store.go:68) is new since the research example — decide whether the guard counts a deactivated operator.

### File Created
`.planning/phases/43-operator-break-glass-recovery-and-forgot-password-e2e/43-PATTERNS.md`

### Ready for Planning
Pattern mapping complete. Every new/modified file maps to a confirmed in-repo analog with a concrete excerpt; the planner can reference these `file:line` patterns directly in the PLAN.md action sections. Read `## Drift Corrections` before finalizing the CI-edit and guard plans.
