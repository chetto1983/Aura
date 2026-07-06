# MUSR isolation rollout runbook (D-12 / D-13)

Enabling multi-user identity isolation for the **documents plane** is a deliberate,
ordered, **reversible** rollout gated by the single flag `AURA_MUSR_ISOLATION`
(`internal/config/config_knobs.go`, default **off**). This runbook is the operator
procedure for turning it on safely.

> **The one rule:** run the Neo4j owner-edge backfill **BEFORE** you flip the flag on.
> Flipping first would hide the operator's own pre-isolation documents until the backfill
> ran (Pitfall 2 / D-12). Everything below exists to make that ordering explicit and safe.

## Why the flag is safe to deploy first (the mechanism)

`AURA_MUSR_ISOLATION` is a **query-PATH selector**, not a data migration (plan 05, D-13):

- **Flag OFF (default):** documents retrieval runs the **pre-existing UNSCOPED** query path.
  The operator sees every document; **no fail-closed enforcement is active**. This is exactly
  today's behavior — deploying the isolation-capable binary with the flag off changes nothing
  a user can observe. Nothing can go invisible at this step.
- **Flag ON:** retrieval runs the identity-scoped queries carrying the unconditional
  `EXISTS { (:User {identifier: $identity})-[:HAS_DOCUMENT]->(:Document {id}) }` ownership
  filter. A document is returned only to the identity that owns its `HAS_DOCUMENT` edge; an
  empty/foreign identity fails closed (0 hits).

The scoped queries are **retained alongside** the unscoped ones (they are NOT hard-removed
this phase), so the flag is a live switch in both directions — **the flip is reversible**.

Because enforcement is **inert until the flip**, the flip is the ONLY thing that turns
scoping on, so it MUST come **after** the backfill has attached an owner edge to every
existing document.

## Enforced order: deploy → backfill → verify → flip

### 1. Deploy the scoping code with `AURA_MUSR_ISOLATION=off`

Ship the binary/config with the flag **off** (or simply unset — it defaults off). SAFE: the
unscoped fallback path is active, the operator sees all documents, and no fail-closed
enforcement runs yet.

### 2. Run the backfill

```sh
aura documents backfill
```

This attaches `(:User {identifier})-[:HAS_DOCUMENT]->(:Document)` to **every** existing
document (D-12). It is **idempotent** (`MERGE`) — re-running creates no duplicate edges, so
it is safe to run repeatedly or resume after an interruption. It works in two passes:

- **Op 1 — Postgres map:** for each `(identity_id, document_id)` pair sourced from
  `aura.assets` and `aura.documents.metadata->>'search_document_id'`, it MERGEs the owner's
  edge. It `MATCH`es the existing `:Document` (never creates one), so a stale row for a purged
  document produces no phantom node.
- **Op 2 — orphan net:** every `:Document` still lacking any owner edge (CLI-ingested or
  pre-36-05 legacy documents that carry no Postgres identity row) is attached to the
  **operator** (`--operator <uuid>`, default the seeded `local` identity
  `00000000-0000-0000-0000-000000000001`).

> The operator's graph `:User.identifier` is the seeded local identity **UUID**, not the
> literal string `local` — web ingest threads `asset.IdentityID` (that UUID) and scoped
> retrieval threads `identityctx.IdentityID(ctx)` (the same UUID once the operator logs in).
> Backfilling under the UUID is what keeps the operator's documents visible after the flip.

The command prints a JSON report:

```json
{ "owners_sourced": 42, "edges_from_map": 42, "orphans_attached_to_operator": 3 }
```

Because every existing document belongs to the operator before this phase (D-11,
operator-unchanged), in practice every edge resolves to the operator — Op 1 for anything with
a Postgres row, Op 2 for anything ingested via the CLI. The code is general (it attributes a
real per-identity owner when the map has one) so it stays correct if identities already own
documents.

### 3. Verify the operator still sees own data (still flag-off)

Confirm, while the flag is still **off**, that document retrieval works as before (the
operator sees their library). On the live stack the acceptance is the neo4j_integration
`TestDocumentsBackfill` gate (operator scoped search returns the backfilled documents) plus
the two-identity E2E's `provision→login→isolated-run` leg.

### 4. Flip `AURA_MUSR_ISOLATION=on` and restart

```sh
# in the environment / .env
AURA_MUSR_ISOLATION=true
```

This is the **enforcement switch**: it activates plan 05's scoped fail-closed path. It is now
safe because step 2 attached an owner edge to every document, so the operator's scoped search
resolves against those edges and returns their documents; other identities see only their own.

## Reversibility

Flipping `AURA_MUSR_ISOLATION` back to **off** restores the unscoped path immediately (plan 05
retains the branch — it is NOT hard-removed this phase). No data is destroyed by either
direction: the ownership edges the backfill wrote are inert while the flag is off and become
enforcing when it is on.

## Acceptance (what proves the rollout is correct)

- `go test -tags neo4j_integration -run TestDocumentsBackfill ./internal/documents/` — the
  backfill attaches the correct owner edges and is idempotent; a re-run adds no duplicates.
- The two-identity cross-deny live E2E
  (`go test -tags 'db_integration neo4j_integration garage_integration authula_integration musr_e2e' ./cmd/aura/`)
  runs with the flag **on** and proves identity B is denied on every plane while A keeps its
  data — the D-29 / MUSR-01 acceptance gate.
