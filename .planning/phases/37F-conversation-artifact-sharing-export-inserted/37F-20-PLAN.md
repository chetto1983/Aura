---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 20
type: execute
wave: 6
depends_on: ["37F-07"]
gap_closure: true
files_modified:
  - internal/db/migrations/0041_shared_links_rls.up.sql
  - internal/db/migrations/0041_shared_links_rls.down.sql
  - internal/share/store_rls_integration_test.go
  - prd.md
  - docs/adr/0039-conversation-sharing-vs-identity-isolation.md
autonomous: true
requirements: [WEBSHARE-02]

must_haves:
  truths:
    - "aura.shared_links has RLS ENABLED with an owner-isolation policy after `aura db migrate`"
    - "Inside an identity transaction (app.current_identity SET), identity B cannot SELECT identity A's shared_links row — the database itself denies it, with no WHERE clause present"
    - "With app.current_identity UNSET (plain pool), every shared_links row remains readable — the public /s/{token} and internal /shared/{id} resolvers keep working unchanged"
    - "aura.share_audit is deliberately NOT given RLS — consistent with the skill_audit (0010) / mcp_audit audit-table precedent"
    - "The down migration fully reverses: policy dropped, RLS disabled"
  artifacts:
    - "internal/db/migrations/0041_shared_links_rls.up.sql"
    - "internal/db/migrations/0041_shared_links_rls.down.sql"
    - "internal/share/store_rls_integration_test.go"
---

# 37F-20 — RLS backstop for aura.shared_links (gap closure)

## Objective

Close the defense-in-depth gap found during 37F-07: `aura.shared_links` is an
identity-scoped table with **no RLS policy**, while every other identity-scoped table
(`conversations`, `paused_states`, `conversation_turns`) has carried one since migration
`0032_owner_rls`.

This matters because ADR-0039's own Context section states the MUSR invariant relies on
"Postgres RLS (migration `0032_owner_rls`) backstop[ping] a forgotten `WHERE` clause as a
kernel-enforced defense-in-depth layer beneath the app-level scoping." That sentence is
currently **false for `shared_links`**. Today the only thing preventing cross-identity
access to share rows is the application-level `owner_identity_id` predicate in
`internal/share/store.go`. 37F-13's E2E can prove today's predicates are correct; it cannot
protect a future query that forgets one.

ADR-0039 authorizes exactly ONE hole in MUSR: the unauthenticated `/s/{token}` public read.
It does **not** authorize weakening the owner-scoped path. This plan restores parity.

## Why this is safe for the public tier (read before implementing)

The 0032 policy is **permissive-on-unset** by deliberate design:

```sql
USING ( NULLIF(current_setting('app.current_identity', true), '') IS NULL
        OR identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid )
```

0032's own header comment documents why: a strict fail-closed-on-unset policy "would hide it
and force a 404, contradicting D-06. Tightening to fail-closed-on-unset is deferred until
every write/read path sets the var."

37F-07 built `internal/share/store.go` to exactly this split:
- **Owner-scoped methods** (`Create`, `ListForIdentity`, `ListForConversation`,
  `UpdateSnapshot`, `RevokeForIdentity`, `RevokeForConversation`) route through
  `db.WithIdentityTxRaw` → `app.current_identity` IS set → the policy will bite.
- **Token/id resolvers** (`ResolveByToken`, `ResolveLiveByID`) read on the **plain pool** →
  the var is unset → the policy is permissive → the public and internal lanes keep working.

So the public tier is not a blocker: the unset-identity escape hatch is precisely the
mechanism that keeps `/s/{token}` functional. Do NOT invent a bypass role or a token
carve-out in the policy — mirror 0032 exactly.

## Tasks

### Task 1 — Migration 0041 (up + down)

Create `internal/db/migrations/0041_shared_links_rls.up.sql`:

- `ALTER TABLE aura.shared_links ENABLE ROW LEVEL SECURITY;`
- `CREATE POLICY shared_links_owner_isolation ON aura.shared_links USING (...)` mirroring
  0032's shape **exactly**, keyed on `owner_identity_id` (uuid, NOT NULL, FK to
  `aura.identities(id)` — confirmed in 0040 line 15).
- A header comment in the same voice as 0032 explaining: why permissive-on-unset is retained
  (the token/id resolvers depend on it), why `share_audit` is excluded (audit-table
  precedent), and that this closes the gap 37F-07 flagged.

Create `internal/db/migrations/0041_shared_links_rls.down.sql`:
- `DROP POLICY IF EXISTS shared_links_owner_isolation ON aura.shared_links;`
- `ALTER TABLE aura.shared_links DISABLE ROW LEVEL SECURITY;`

**Do NOT add RLS to `aura.share_audit`.** Audit tables (`skill_audit` 0010, `mcp_audit`)
carry no RLS; `share_audit` stays consistent with that precedent. Its append-only property
is enforced by grants (see the 37F-02 SUMMARY's documented grant-only decision).

Commit: `feat(37F-20): migration 0041 — RLS owner-isolation policy on shared_links`

### Task 2 — Integration test proving the backstop actually bites

Create `internal/share/store_rls_integration_test.go` (build tag `db_integration`).

The test must prove the property **at the database layer, with no WHERE clause doing the
work** — otherwise it proves nothing about RLS:

1. **Cross-identity denial under a set var:** seed a `shared_links` row owned by identity A.
   Open a transaction via `db.WithIdentityTxRaw(ctx, pool, identityB, ...)` and issue a
   deliberately **unfiltered** `SELECT ... FROM aura.shared_links WHERE id = $1` (id only —
   NO `owner_identity_id` predicate). Assert zero rows: the policy, not the query, denied it.
2. **Owner still sees their own row** under `WithIdentityTxRaw(identityA)` with the same
   unfiltered query — assert exactly one row (guards against a policy that denies everything,
   which would pass test 1 vacuously).
3. **Public lane unaffected:** with the var unset (plain pool), assert `ResolveByToken` still
   resolves a live public row, and `ResolveLiveByID` still resolves an internal row.

Test 2 is mandatory — without it, a policy of `USING (false)` would pass test 1 and look
like success while breaking the entire feature.

Commit: `test(37F-20): prove RLS denies cross-identity shared_links reads at the DB layer`

### Task 3 — Documentation truth-up

- `prd.md`: update the §Persistence migration-numbering section to record `0041` as shipped
  (the count and the on-disk floor).
- `docs/adr/0039-conversation-sharing-vs-identity-isolation.md`: add a short note recording
  that `shared_links` carries its own RLS owner-isolation policy as of `0041`, so the ADR's
  Context statement about the RLS backstop is now true for this table too. Keep it factual;
  do not restate the seven mitigations.

Commit: `docs(37F-20): record migration 0041 in the PRD and ADR-0039`

## Acceptance criteria

- [ ] `ls internal/db/migrations/ | tail -1` shows `0041_shared_links_rls.up.sql` (slot verified, never deduced)
- [ ] `go test -tags db_integration ./internal/share/ -run RLS` passes, and genuinely EXECUTES
      (per-test runtime is a real DB round trip, not a sub-second skip tell)
- [ ] The cross-identity denial test issues NO `owner_identity_id` predicate — proven by grep
- [ ] The owner-visibility test passes (policy is not `USING (false)`)
- [ ] `ResolveByToken` / `ResolveLiveByID` integration tests still pass unchanged
- [ ] Migration round-trip (up → down → up) verified against a THROWAWAY database, never the live `aura`
- [ ] `go build ./...`, `go vet ./...`, `go test -race ./internal/share/` all green (via WSL)
- [ ] `bash scripts/check-file-size.sh` clean

## Notes

- Migration slot `0041` was confirmed via `ls internal/db/migrations/ | tail -1` at plan time
  (floor on disk was `0040_shared_links`). **Re-verify before writing** — the parallel session
  may have landed a migration since.
- This plan was authored mid-phase as a gap closure after 37F-07 surfaced the missing policy
  and the operator explicitly approved closing it. It is not part of the original 19-plan set.
