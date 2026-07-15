---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 07
type: execute
wave: 3
depends_on: ["37F-02", "37F-03"]
files_modified:
  - internal/share/store.go
  - internal/share/audit.go
  - internal/share/store_integration_test.go
  - internal/agui/audit_store.go
  - internal/agui/audit_api.go
  - internal/agui/share_audit_union_test.go
autonomous: true
requirements: [WEBSHARE-02, WEBSHARE-03]

must_haves:
  truths:
    - "Every owner-scoped share read/mutate runs through db.WithIdentityTx so the 0032 RLS policy backstops a forgotten predicate"
    - "A foreign or absent share id returns ErrShareNotFound, which the handler maps to 404 — never 403, never a distinguishable body"
    - "ResolveByToken finds a live link by hash-indexed equality on token_hash, and returns not-found for a revoked or expired link without the sweep ever running"
    - "share_audit rows appear in the admin activity feed with source='share'"
    - "Deleting a conversation removes its shared_links rows via the FK cascade"
  artifacts:
    - path: "internal/share/store.go"
      provides: "shared_links CRUD over raw pgx: Insert, GetForIdentity, ListForIdentity, ResolveByToken, UpdateSnapshot, RevokeForIdentity, DueForExpiry"
      min_lines: 120
    - path: "internal/share/audit.go"
      provides: "share_audit append-only writer"
      min_lines: 40
    - path: "internal/agui/audit_store.go"
      provides: "a 4th UNION ALL leg projecting share_audit as source='share'"
      contains: "share_audit"
  key_links:
    - from: "internal/share/store.go"
      to: "aura.shared_links"
      via: "hash-indexed equality on the unique partial index"
      pattern: "token_hash = \\$1"
    - from: "internal/agui/audit_store.go"
      to: "aura.share_audit"
      via: "UNION ALL leg keyed on identity_id = ANY($1::text[])"
      pattern: "FROM aura\\.share_audit"
  prohibitions:
    - "MUST NOT implement ResolveByToken as a table scan with subtle.ConstantTimeCompare per row — D-13 is AMENDED to hash-indexed equality (WHERE token_hash = $1); a scan is slower and no more secure"
    - "MUST NOT use db.WithIdentityTx in ResolveByToken — a public recipient has no principal; it reads on the plain pool, and the token predicate is the trust boundary"
    - "MUST NOT omit the revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now()) predicate from ResolveByToken — lazy enforcement IS the fail-closed gate"
    - "MUST NOT use the Go clock in the expiry predicate — use the DB clock now(), so a skewed app clock cannot resurrect a link"
    - "MUST NOT log, %w-wrap, or audit a plaintext token"
    - "MUST NOT add an UPDATE or DELETE path against share_audit — aura_app has no such grant and the ledger is append-only"
    - "MUST NOT add a sqlc query — this store is raw pgx, following the PgAuditStore precedent"
    - "MUST NOT put more than one build tag on the integration test — db_integration ONLY; any extra tag means ZERO coverage"
    - "MUST NOT project the share union leg with a column count or order different from the first SELECT — UNION ALL matches by position"
---

<objective>
Build the `shared_links` persistence + the `share_audit` ledger, and wire the ledger into the existing
admin audit union so D-14's "surfaces in the existing admin audit UI" comes free.

Two security properties live here, not in the handlers:
- **`ResolveByToken` is the lazy fail-closed gate.** Its single predicate — hash-indexed equality on
  `token_hash` AND `revoked_at IS NULL` AND `(expires_at IS NULL OR expires_at > now())` — is what makes
  an expired link 404 even if the sweep never runs (scheduler down, task unseeded, worker crash-looping).
  A sweep-only design has a live window between `expires_at` and the next tick. That window is a direct
  D-04/D-15 violation.
- **Every owner-scoped path runs through `db.WithIdentityTx`**, so RLS 0032 backstops a forgotten WHERE.
  `ResolveByToken` is the deliberate exception and must document itself as such.

Purpose: the storage layer with its invariants enforced and tested against a real Postgres.
Output: `internal/share/store.go`, `internal/share/audit.go`, and a 4-line union leg.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
@CLAUDE.md
</context>

## Artifacts this plan produces

`share.Store`, `share.Link`, `share.ErrShareNotFound`, `share.AuditWriter`, `share.AuditAction`, and
the `source="share"` audit union value.

<tasks>

<task type="auto">
  <name>Task 1: share.Store — shared_links CRUD over raw pgx, owner-gated, with the lazy token predicate</name>
  <read_first>
    - `internal/agui/audit_store.go` — **the whole file**. `PgAuditStore` (`:40-46`) is the raw-pgx store shape; `ListActivityForIdentity` (`:68-92`) is the query/scan/rows.Err loop to copy exactly. `:11-17` is the precedent for **documenting a deliberate non-identity-scoped read** — `ResolveByToken` needs the equivalent block.
    - `internal/conversations/store_identity.go:14-53` — `GetForIdentity`: `parseUUID` on every id BEFORE the query, `db.WithIdentityTx(ctx, s.pool, identityID, …)`, `pgx.ErrNoRows` → a sentinel. Its file header (`:14-23`) is the doc template stating why BOTH the WHERE clause and RLS exist ("primary correctness path" + "kernel backstop").
    - `internal/db/migrations/0040_shared_links.up.sql` — the table you are reading/writing (plan 37F-02). Read the actual columns; do not retype them from memory.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §OQ3 (why lazy is mandatory and is the security boundary; why the DB clock) + §OQ5's D-13 refinement (hash-indexed equality)
  </read_first>
  <action>
    Create `internal/share/store.go`, package `share`. Raw pgx over a `*pgxpool.Pool`, following
    `PgAuditStore`. Do **not** add a sqlc query — the `PgAuditStore` precedent is explicit that this
    class of store is raw pgx.

    Define `Link` (the row projection: id, owner identity, conversation, tier, snapshot id + bucket,
    format options, expires/revoked/created/updated) and the sentinel `ErrShareNotFound`.

    Methods:
    - `Insert(ctx, Link, tokenHash []byte) error` — owner-scoped via `WithIdentityTx`
    - `GetForIdentity(ctx, shareID, identityID) (Link, error)` — owner gate; `pgx.ErrNoRows` →
      `ErrShareNotFound`
    - `ListForIdentity(ctx, identityID, limit, offset) ([]Link, error)` — owner-scoped, newest first
      (the `shared_links_owner_idx` order)
    - `ListForConversation(ctx, convID, identityID) ([]Link, error)` — owner-scoped; drives the
      "Condiviso" section
    - `UpdateSnapshot(ctx, shareID, identityID, snapshotID uuid.UUID, at time.Time) error` — owner-scoped;
      bumps `updated_at` (D-06 "Update")
    - `RevokeForIdentity(ctx, shareID, identityID, at time.Time) error` — owner-scoped; stamps `revoked_at`
    - `RevokeForConversation(ctx, convID, identityID, at time.Time) ([]Link, error)` — owner-scoped; the
      D-15 cascade needs the revoked links back so their blobs can be dropped
    - `DueForExpiry(ctx, now time.Time, limit int) ([]Link, error)` — the sweep's due set, over
      `shared_links_expiry_idx`
    - `ResolveByToken(ctx, tokenHash []byte) (Link, error)` — **the exception, see below**

    **`ResolveByToken` — the two deliberate deviations, each requiring a doc block:**
    1. **It does NOT use `WithIdentityTx`.** A public recipient has no principal, so there is no identity
       to set. It reads on the plain pool. Mirror `audit_store.go:11-17`'s shape for documenting a
       deliberate non-identity-scoped read, with 37F's version of the argument: *the token hash is the
       capability, and the predicate — `revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`
       — is the trust boundary for this read, not the RLS session var.*
    2. **The predicate is hash-indexed equality (`WHERE token_hash = $1`), NOT a scan.** Document the
       amended D-13 rationale inline so a later reviewer does not "fix" it into a
       `subtle.ConstantTimeCompare` loop: the lookup key is already `SHA-256(token)`, so exploiting a
       timing signal on the index probe to recover the token would require inverting SHA-256; the literal
       reading is slower and no more secure; `crypto/subtle` is correct only where a secret is compared in
       Go memory and this design has no such site.

    Use the **DB clock** (`now()`) in the expiry predicate, never a Go-side `time.Now()`. Document why:
    a skewed app clock must not be able to resurrect an expired link.

    Copy `PgAuditStore`'s error discipline exactly: `%w`-wrapped errors at all three sites (`Query`,
    `Scan`, `rows.Err()`), `defer rows.Close()`, `make([]T, 0, limit)`, and — critically — the
    **`rows.Err()` check**, whose absence is the classic pgx silent-truncation bug.

    Never `%w` a token into an error (D-13: never logged).
  </action>
  <verify>
    <automated>go build ./... && go vet ./internal/share/ && golangci-lint run ./internal/share/</automated>
  </verify>
  <acceptance_criteria>
    - `go build ./... && go vet ./internal/share/` clean; `golangci-lint run ./internal/share/` reports 0 issues.
    - `grep -c "db.WithIdentityTx" internal/share/store.go` returns ≥ 7 (every owner-scoped method).
    - `ResolveByToken` does NOT use it: the function body between `func (s *Store) ResolveByToken` and the next `func` contains no `WithIdentityTx`.
    - `grep -q "token_hash = \$1" internal/share/store.go` succeeds — indexed equality.
    - `grep -nE "subtle\.|ConstantTimeCompare" internal/share/store.go` returns NOTHING.
    - The lazy predicate is present and complete: `grep -q "revoked_at IS NULL" internal/share/store.go` and `grep -q "expires_at > now()" internal/share/store.go` both succeed.
    - `grep -n "time.Now()" internal/share/store.go` returns NOTHING inside any SQL string — the expiry predicate uses the DB clock.
    - `grep -c "rows.Err()" internal/share/store.go` returns ≥ 3 (one per list-returning method) — no silent truncation.
    - `ErrShareNotFound` is a sentinel returned on `pgx.ErrNoRows`: `grep -q "errors.Is(.*pgx.ErrNoRows)" internal/share/store.go`.
    - No sqlc query file was added: `git diff --name-only internal/db/queries/` is empty.
    - `internal/share/store.go` ≤ 600 LOC.
  </acceptance_criteria>
  <done>`share.Store` implements owner-gated CRUD through `WithIdentityTx` with `ErrShareNotFound` on miss, plus a documented plain-pool `ResolveByToken` whose single hash-indexed predicate enforces revoke + expiry against the DB clock.</done>
</task>

<task type="auto">
  <name>Task 2: share_audit writer + the 4th audit-union leg (D-14)</name>
  <read_first>
    - `internal/agui/audit_store.go:48-69` — `auditActivityQuery`. **Copy the `'skill'` leg exactly**: it is the `identity_id text = ANY($1::text[])` shape `share_audit` shares. The comment at `:48-52` states two contracts the new leg must honor — column names come from the FIRST select and **UNION ALL matches by position**.
    - `internal/agui/audit_store.go:99-104` — `auditIdentityKeys`: it appends the literal `'local'` to the key array. This is WHY `share_audit.identity_id` is `text` and not `uuid` — a uuid column could not hold `'local'`.
    - `internal/agui/audit_store.go:3-9` — the file header enumerating **three** ledgers. It must become four (CLAUDE.md: comments-updated in the SAME commit).
    - `internal/agui/audit_api.go:32-38` — `AuditEvent`; `:33`'s `Source` doc comment currently reads `"mcp" | "skill" | "tool"` and must gain `| "share"`.
    - `internal/agui/audit_api.go:249-254` — `SanitizeString` already runs on `Target`/`Detail`, so share rows inherit it for free. Confirm this by reading; it is the ONE correct 37F use of `SanitizeString` (it is an anti-analog for `redact.go`, not for audit).
    - `internal/db/migrations/0040_shared_links.up.sql` — the `share_audit` columns (plan 37F-02)
  </read_first>
  <action>
    Create `internal/share/audit.go`, package `share`. An append-only writer over the pool:
    `AuditAction` (a small string type with the five constants `create`/`update`/`revoke`/`expire`/`open`,
    matching the migration's CHECK) and
    `Append(ctx, identityID string, shareLinkID, conversationID uuid.UUID, action AuditAction, tier, detail string) error`.
    INSERT only — `aura_app` has no UPDATE/DELETE grant, and the writer must not pretend otherwise.

    Document the two non-obvious WHYs: `identity_id` is `text` (not uuid, no FK) so the union leg can key
    on the same `$1::text[]` array that carries the literal `'local'`, and so an identity delete cannot
    cascade the evidence away; and the ledger is append-only by grant, which is the audit-integrity
    statement.

    **Never audit a plaintext token.** For the `open` action, record the timestamp + coarse info only —
    **no recipient PII, no IP, no user-agent** (D-14).

    Then add the 4th leg to `internal/agui/audit_store.go`'s `auditActivityQuery`. Model it on the
    `'skill'` leg: project **five columns in the same positional order** as the first SELECT —
    the literal `'share'` as source, then `action`, then the conversation id coalesced to text as target,
    then the tier coalesced to empty as detail, then `created_at` — `FROM aura.share_audit WHERE
    identity_id = ANY($1::text[])`. Aliases are optional; **position is not**.

    Update, in the SAME commit:
    - `audit_store.go:3-9`'s header — "the three identity-keyed audit ledgers" becomes four, naming
      `aura.share_audit` (keyed on `identity_id`).
    - `audit_api.go:33`'s `Source` doc — add `| "share"`.

    Write `internal/agui/share_audit_union_test.go` with the **single** build tag `db_integration`,
    implementing `TestAuditUnionIncludesShare` from VALIDATION.md: seed a provisioned identity + a
    `share_audit` row, call `ListActivityForIdentity`, assert an event comes back with `Source == "share"`
    and the expected action/target. Follow the house integration-test header (env + run command +
    no-skip-as-green note) and use the shared `envOrSkip` so it `t.Fatal`s under `$CI` when the DSN is
    unset. `t.Cleanup` deletes the seeded rows.
  </action>
  <verify>
    <automated>go build ./... && go vet ./internal/agui/ ./internal/share/ && go test -tags db_integration -run TestAuditUnionIncludesShare -count=1 ./internal/agui/</automated>
  </verify>
  <acceptance_criteria>
    - `go test -tags db_integration -run TestAuditUnionIncludesShare -count=1 ./internal/agui/` passes against a live Postgres, and its runtime is **not** sub-second (a sub-second "integration" run is a skip tell).
    - `grep -c "UNION ALL" internal/agui/audit_store.go` returns `3` (four legs).
    - The share leg projects exactly 5 columns and `grep -q "FROM aura.share_audit" internal/agui/audit_store.go` succeeds.
    - `grep -q "identity_id = ANY(\$1::text\[\])" internal/agui/audit_store.go` matches the share leg.
    - Header updated: `internal/agui/audit_store.go`'s header no longer says "three" ledgers and names `share_audit`.
    - `audit_api.go`'s `Source` doc includes `share`: `grep -q '"mcp" | "skill" | "tool" | "share"' internal/agui/audit_api.go` (or the equivalent updated comment).
    - `internal/share/audit.go` contains no UPDATE or DELETE statement: `grep -niE "UPDATE aura.share_audit|DELETE FROM aura.share_audit" internal/share/audit.go` returns NOTHING.
    - No PII is recorded on open: `grep -niE "RemoteAddr|User-Agent|UserAgent|X-Forwarded" internal/share/audit.go` returns NOTHING.
    - The test file carries **exactly one** build tag: `head -1 internal/agui/share_audit_union_test.go` is `//go:build db_integration`.
    - `golangci-lint run ./internal/share/ ./internal/agui/` reports 0 issues.
  </acceptance_criteria>
  <done>`share.AuditWriter` appends the five audited actions with no PII and no token; the audit union has a 4th `share` leg projecting positionally; the file header and `AuditEvent.Source` doc are updated in the same commit; `TestAuditUnionIncludesShare` passes live.</done>
</task>

<task type="auto">
  <name>Task 3: store integration tests — owner gate, lazy expiry, revoke, FK cascade</name>
  <read_first>
    - `internal/agui/auth_capability_integration_test.go` — **the whole file**: the coverage-gate-safe test shape. Copy the single `//go:build db_integration` tag, the header's env + run-command + no-skip-as-green block, `migratedPool(t)`, and `t.Cleanup` row deletion. Its 403 subtest (`:91-116`) shows the fresh **non-wildcard** identity seed (`name = "..."+t.Name()` for parallel-run uniqueness).
    - `internal/agui/server_integration_test.go` — the shared `envOrSkip` helper: it `t.Fatal`s under `$CI` when the DSN is unset, skipping only locally. Use it; do not write a new skip helper.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` — the exact test names: `TestShareRevokeThen404`, `TestShareExpiredLazy404`, `TestSharedLinksCascade`, `TestSharePublicRequiresExpiry`
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"No-skip-as-green" — the exact env the tests read: the **composed DSNs** `AURA_DB_URL` and `AURA_DB_MIGRATE_URL`, NOT the `POSTGRES_*` primitives
  </read_first>
  <action>
    Create `internal/share/store_integration_test.go` with the **single** build tag `db_integration`.

    Seed **provisioned non-wildcard identities** per run (R-13: the seeded `local` identity holds the `*`
    wildcard, so any capability assertion against it passes vacuously — and the bootstrap operator holds
    `*` too, verified at plan time). Use `name = "..."+t.Name()` for uniqueness so parallel runs do not
    collide. `t.Cleanup` deletes them.

    Tests (names from VALIDATION.md):
    - `TestShareStoreOwnerGate` — identity B's `GetForIdentity` on A's link returns `ErrShareNotFound`
      (the 404 path), not a row and not a distinct error
    - `TestShareRevokeThen404` — revoke, then `ResolveByToken` returns `ErrShareNotFound`
    - `TestShareExpiredLazy404` — insert a link with `expires_at` in the past, then `ResolveByToken`
      returns `ErrShareNotFound` **with the sweep never run**. This is the test that proves lazy is the
      gate; assert explicitly in a comment that no sweep ran.
    - `TestSharePublicRequiresExpiry` — inserting a public link with a NULL `expires_at` (or NULL
      `token_hash`) is rejected **by the database** — assert on the SQLSTATE `23514` (check violation), not
      on a message match. This proves the `shared_links_tier_shape` CHECK is doing its job.
    - `TestSharedLinksCascade` — a raw `DELETE FROM aura.conversations WHERE id=$1` removes the
      link rows via the FK. Document inline that this is the **backstop only**: the FK cannot drop Garage
      bytes (R-10), which is why the D-15 lifecycle hook (plan 37F-11) stays mandatory.
    - `TestShareTokenUniqueness` — inserting a duplicate `token_hash` violates
      `shared_links_token_hash_idx`; assert on SQLSTATE `23505`.

    Every test uses `envOrSkip` for the DSNs. **Exactly one build tag** — any extra tag means the file
    compiles+skips in CI and contributes ZERO coverage (WR-01), which is the failure this whole phase is
    designed to avoid.

    Run the gate on a **disposable** DB (`bash scripts/coverage_docker.sh`, which provisions `aura_cov`),
    never the live `aura` DB.
  </action>
  <verify>
    <automated>go test -tags db_integration -race -p 1 -count=1 ./internal/share/ && go test ./internal/share/ -count=1</automated>
  </verify>
  <acceptance_criteria>
    - `go test -tags db_integration -race -p 1 -count=1 ./internal/share/` passes against a live Postgres, with a **non-sub-second** runtime (a sub-second run means it skipped).
    - The untagged suite still passes: `go test ./internal/share/ -count=1`.
    - The file carries **exactly one** build tag: `head -1 internal/share/store_integration_test.go` is `//go:build db_integration`.
    - `grep -rn "garage_integration\|neo4j_integration\|authula_integration\|musr_e2e\|docker_integration" internal/share/` returns NOTHING.
    - `TestShareExpiredLazy404` passes with no sweep invocation anywhere in the test body.
    - `TestSharePublicRequiresExpiry` and `TestShareTokenUniqueness` assert on **SQLSTATE** codes (`23514`, `23505`), never on an error message string.
    - Identities are seeded fresh and non-wildcard: `grep -q "t.Name()" internal/share/store_integration_test.go`, and no test grants `*`.
    - `envOrSkip` (the shared helper) is used; no new skip helper is defined: `grep -c "func.*EnvOrSkip\|func envOrSkip" internal/share/store_integration_test.go` returns `0`.
    - `internal/share` package coverage under the gate tags is ≥ 85%.
  </acceptance_criteria>
  <done>The store's owner gate, lazy expiry, revoke, DB-enforced tier shape, token uniqueness, and FK cascade are each proven against a real Postgres under a single `db_integration` tag with provisioned non-wildcard identities.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| token hash → a live link | `ResolveByToken`'s predicate is the entire authorization decision for the public tier. There is no principal, no session, and no capability check downstream of it — the predicate IS the gate. |
| owner identity → their own links | Every other method crosses this boundary and is guarded twice: the explicit `owner_identity_id` predicate (primary) and RLS 0032 via `WithIdentityTx` (kernel backstop). |
| identity deletion → audit history | `share_audit` deliberately has no FK so the ledger outlives what it records. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-36 | Elevation of Privilege | expired link resolving in the sweep window | mitigate | Lazy enforcement in `ResolveByToken`'s predicate — an expired link 404s even if the scheduler is down, the task unseeded, or the worker crash-looping. `TestShareExpiredLazy404` runs with no sweep. |
| T-37F-37 | Tampering | a skewed app clock resurrecting a link | mitigate | The predicate uses the DB clock `now()`, never a Go-side `time.Now()`. Grep-gated. |
| T-37F-05 | Information Disclosure | cross-identity read of a share row | mitigate | Every owner-scoped method runs `db.WithIdentityTx` (RLS 0032 backstop) plus an explicit owner predicate; a miss returns the `ErrShareNotFound` sentinel → 404, never 403. |
| T-37F-38 | Information Disclosure | a reviewer "fixing" the lookup into a constant-time table scan | mitigate | The amended D-13 rationale is documented inline at the call site AND in the PRD (plan 37F-01); `subtle.`/`ConstantTimeCompare` is grep-gated out of `store.go`. |
| T-37F-11 | Information Disclosure | plaintext token in a log, error, or audit row | mitigate | The store takes a hash, never a plaintext; no token is `%w`-wrapped; `audit.go` records no token. |
| T-37F-10 | Repudiation | share act unaudited or audit tampered | mitigate | Append-only writer with INSERT only (matching the migration's grant); no UPDATE/DELETE path exists — grep-gated. |
| T-37F-39 | Information Disclosure | recipient PII in the open audit | mitigate | The `open` action records timestamp + coarse info only; `RemoteAddr`/`User-Agent`/`X-Forwarded` are grep-gated out of `audit.go` (D-14). |
| T-37F-40 | Information Disclosure | pgx silent truncation hiding rows | mitigate | `rows.Err()` checked in every list-returning method, per the `PgAuditStore` precedent. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | pgx + stdlib only; both already vendored. |
</threat_model>

<verification>
- `go build ./... && go vet ./internal/share/ ./internal/agui/`
- `go test ./internal/share/ -count=1` (untagged, still green)
- `go test -tags db_integration -race -p 1 -count=1 ./internal/share/ ./internal/agui/`
- `golangci-lint run ./internal/share/ ./internal/agui/` → 0 issues
- Gate on a **disposable** DB only: `bash scripts/coverage_docker.sh`. Never against the live `aura` DB — `coverage_gate.sh:35` refuses it locally (this closed the 2026-07-10 footgun that wiped the live deployment's auth tables).
- `bash scripts/check-file-size.sh` → 0
</verification>

<success_criteria>
`shared_links` persists behind an owner gate enforced twice (predicate + RLS), with a deliberate,
documented plain-pool `ResolveByToken` whose single hash-indexed predicate makes revoke and expiry
fail-closed without the sweep. `share_audit` is append-only, carries no token and no recipient PII, and
appears in the existing admin audit UI as a 4th union leg with `source="share"` — with the file header
and `AuditEvent.Source` doc updated in the same commit.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-07-SUMMARY.md` when done.
Record the `db_integration` runtime (to prove it did not skip) and the package coverage.
</output>
