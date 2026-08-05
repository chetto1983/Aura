# Operator E2E — Findings Ledger

Document pipeline production E2E, opened 2026-08-05. This ledger is filled in by
Tasks 5–10 (operator-driven checkpoints CP1–CP7). Task 4 opens it with the backend
observation harness those checkpoints depend on: a built probe binary, a verified
RLS-correct read shape, and the measured pre-run baseline.

## Step 1 — Probe build

```
wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && \
  go build -o /tmp/aura-e2e-probe ./scripts/document_pipeline_e2e_probe.go ./scripts/document_pipeline_e2e_support.go && \
  ls -la /tmp/aura-e2e-probe'
```

Result: **built clean, no errors.**

```
-rwxr-xr-x 1 davide davide 72903936 Aug  5 23:27 /tmp/aura-e2e-probe
```

Binary left at `/tmp/aura-e2e-probe` inside WSL for Tasks 5–10. Subcommands present in
`scripts/document_pipeline_e2e_probe.go`: `preflight`, `setup`, `status`, `snapshot`,
`arcade`, `conversation`, `verify-delete`, `cleanup`. `status` takes `--identity --asset`.
`snapshot` takes `--identity --asset --sha256 --expected-embed-model
--expected-embed-version --expected-docling-producer --state --label`. Both route through
`db.WithIdentityTxRaw(ctx, r.pool, *identityID, ...)` — confirmed at
`scripts/document_pipeline_e2e_probe.go:116` (status) and the `collectSnapshot` call at
`scripts/document_pipeline_e2e_probe.go:154` (snapshot) — the same RLS carrier verified
below.

## Step 2 — RLS-correct read shape

### The brief's candidate shape was wrong on two independent axes

The brief proposed `set_config('aura.identity_id', ..., true)` over
`docker exec aura-postgres psql -U aura -d aura`. Both the GUC name and the connecting
role needed correction before the shape could be trusted as a checkpoint oracle.

**1. GUC name.** `internal/db/tx.go:120`:

```go
func SetTxIdentity(ctx context.Context, tx pgx.Tx, identityID string) error {
	_, err := tx.Exec(ctx, `SELECT set_config('app.current_identity', $1, true)`, identityID)
	return err
}
```

The real GUC is **`app.current_identity`**, not `aura.identity_id`. Confirmed
independently in every RLS policy predicate added by migration 0087
(`internal/db/migrations/0087_rls_fail_closed.up.sql:86,90,94,98,102,116,124,132,140,148,179-180`),
e.g.:

```sql
USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
```

and the fail-closed floor policy comment at line 185: *"Set it via
internal/db.WithIdentityTx / WithIdentityTxRaw."*

**2. Connecting role.** The brief's `docid()` connects as `-U aura`. That role is a
**superuser with `rolbypassrls = true`**:

```
$ docker exec aura-postgres psql -U aura -d aura -t -A -c \
    "SELECT rolname, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user;"
aura|t|t
```

A superuser bypasses RLS entirely — Postgres does not apply row-security policies to a
role with `rolbypassrls`, regardless of GUC state. Proof: the *exact* query from the
brief run over `-U aura` returns the same 4 rows whether or not the GUC is set at all
(tested both ways; identical output). **That means the brief's shape, as written, cannot
distinguish "RLS is scoping correctly" from "RLS is not being applied" — the negative
control it calls for is unfalsifiable on that role.** This would have produced a
checkpoint oracle that looks green under any outcome, which is exactly the "passes
against nothing" failure mode Task 4 exists to prevent.

The application itself never connects as `aura`. `AURA_DB_URL` in `.env` is
`postgres://aura_app:***@127.0.0.1:5432/aura` — `aura_app` is `rolsuper=f,
rolbypassrls=f`:

```
$ docker exec aura-postgres psql -U aura -d aura -t -A -c \
    "SELECT rolname, rolsuper, rolbypassrls FROM pg_roles WHERE rolname IN ('aura','aura_app','aura_migrate');"
aura|t|t
aura_app|f|f
aura_migrate|f|f
```

`aura_app` is the role subject to RLS and is what every checkpoint below connects as.
The probe binary connects through `r.pool`, built from `AURA_DB_URL` — i.e. also
`aura_app` — so the shape below matches what the probe itself does, not an approximation
of it.

### Corrected shell function (the shape Tasks 5–10 use)

```bash
docid() {
  set -a; . /d/Aura/.env; set +a
  local pw; pw=$(printf '%s' "$AURA_DB_URL" | sed -E 's#postgres://aura_app:([^@]*)@.*#\1#')
  docker exec -e PGPASSWORD="$pw" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
    "BEGIN; SELECT set_config('app.current_identity','dc98a3ee-e38e-4288-8d64-27ce4c9cde65',true); \
     SELECT id, status, source_kind, source_key, deleted_at FROM aura.documents ORDER BY created_at DESC LIMIT 10; COMMIT;"
}
```

### Positive control — real output

```
BEGIN
dc98a3ee-e38e-4288-8d64-27ce4c9cde65
fda0529c-6bb2-4154-87cb-cc42281320d8|failed|legacy|document:fda0529c-6bb2-4154-87cb-cc42281320d8|
eca7f21c-b953-4340-9bec-095ffe05ad72|failed|legacy|document:eca7f21c-b953-4340-9bec-095ffe05ad72|
45086f98-8f9d-411b-bdb3-8ee44ac4281e|failed|legacy|document:45086f98-8f9d-411b-bdb3-8ee44ac4281e|
1f79970c-de3b-4d51-a157-9e62e326fef0|deleted|legacy|document:1f79970c-de3b-4d51-a157-9e62e326fef0|2026-08-03 18:54:11.940332+00
COMMIT
```

4 rows: 3 `failed`, 1 `deleted` — matches the measured baseline (Step 3).

### Negative control — same query, same role, GUC NOT set

```
$ docker exec -e PGPASSWORD="$pw" aura-postgres psql -U aura_app -d aura -t -A -F'|' -c \
    "SELECT id, status, source_kind, source_key, deleted_at FROM aura.documents ORDER BY created_at DESC LIMIT 10;"
(no output — zero rows)
```

Zero rows, on the role the application and the probe actually use. This is what makes
the positive result above meaningful: the same query, same table, same data, only the
GUC differs, and the row count goes from 4 to 0. **This shape is now proven correct and
is the one every checkpoint in Tasks 5–10 must use** — either directly (`docid()`-style)
or through the probe binary's `WithIdentityTxRaw` calls, which use the identical GUC and
role.

### Secondary finding: `document_pipeline_quarantine` is unreachable from `aura_app`

Migration 0093 created `aura.document_pipeline_quarantine` with RLS **disabled**
(`relrowsecurity=f` — it has no `identity_id` column, so it isn't identity-scoped) but it
also carries **no grants to `aura_app`**:

```
$ docker exec aura-postgres psql -U aura_app -d aura -t -A -c \
    "SELECT count(*) FROM aura.document_pipeline_quarantine;"
ERROR:  permission denied for table document_pipeline_quarantine

$ docker exec aura-postgres psql -U aura -d aura -t -A -F'|' -c \
    "SELECT grantee, privilege_type FROM information_schema.role_table_grants
     WHERE table_schema='aura' AND table_name='document_pipeline_quarantine';"
aura_migrate|DELETE
aura_migrate|INSERT
aura_migrate|REFERENCES
aura_migrate|SELECT
aura_migrate|TRIGGER
aura_migrate|TRUNCATE
aura_migrate|UPDATE
```

Only `aura_migrate` (and superuser `aura`) can read it. Not a defect — it is a
migration-audit table, plausibly deliberately kept off the app role — but any later
checkpoint that needs to read quarantine rows must connect as `aura_migrate` or `aura`,
not `aura_app`. Flagging so Tasks 5–10 don't rediscover this via a silent
permission-denied.

## Step 3 — Baseline (measured 2026-08-05, post-0093, live)

**Caution honored:** the brief's baseline text ("4 documents, 1 version row") is the
pre-migration-0093 state and is stale. The value below is measured fresh against live,
using the verified read shape from Step 2, not copied from any prior note.

`schema_migrations`: `version=93, dirty=false`.

| Table | Breakdown | Count |
|---|---|---|
| `aura.assets` | `status='accepted'` | 7 |
| `aura.assets` | `status='deleted'` | 1 |
| `aura.documents` | `status='failed'`, `error_code='original_unavailable'` | 3 |
| `aura.documents` | `status='deleted'` | 1 |
| `aura.document_versions` | `status='ready'` | 1 (belongs to document `1f79970c-de3b-4d51-a157-9e62e326fef0`, which is `deleted`) |
| `aura.storage_objects` | `status='live'` | 1 |
| `aura.document_pipeline_stages` | (all) | 0 |
| `aura.document_pipeline_quarantine` | `source_table='document_ingest_jobs'`, reason `legacy table has no typed owner FK` | 4 |
| `aura.document_pipeline_quarantine` | `source_table='ingestion_events'`, reason `no owner through ingestion_events.job_id FK` | 1 |
| `aura.document_pipeline_quarantine` | `source_table='ingestion_jobs'`, reason `no unambiguous document/version owner FK` | 1 |
| `aura.document_pipeline_quarantine` | (total) | 6 |

**Result: measured baseline matches the caller's stated baseline exactly** on every
figure — assets (7 accepted / 1 deleted), documents (3 failed/`original_unavailable` /
1 deleted), document_versions (1 ready, owned by the deleted document), storage_objects
(1 live), document_pipeline_stages (0), document_pipeline_quarantine (6). Nothing is
`ready`/usable pre-run, as expected: the three previously-`ready` documents had
`active_version_id = NULL` with no retrievable version and 0093 correctly reclassified
them as `failed` rather than leaving them presenting as usable. This is as-designed
(confirmed by review) — not a target for repair in this E2E.

## Checkpoint ledger (CP1–CP7)

Filled in by Tasks 5–10. `Operator observed` = what the human sees at the Cockpit /
Telegram / API surface. `Backend held` = what `docid()` / the probe binary reads back
under the verified shape above. `Verdict` = MATCH / MISMATCH / N/A. `Evidence` = query
output, probe JSON, or screenshot reference.

| Checkpoint | Operator observed | Backend held | Verdict | Evidence |
|---|---|---|---|---|
| CP1 — ingest starts, status vocabulary surfaces | _pending Task 5_ | _pending Task 5_ | _pending_ | _pending_ |
| CP2 — status vocabulary reaches terminal state | _pending Task 5_ | _pending Task 5_ | _pending_ | _pending_ |
| CP3 — agent cites the document | _pending Task 6_ | _pending Task 6_ | _pending_ | _pending_ |
| CP4 — delete-in-flight window | _pending Task 7_ | _pending Task 7_ | _pending_ | _pending_ |
| CP5 — repeat-source production proof | _pending Task 8_ | _pending Task 8_ | _pending_ | _pending_ |
| CP6 — workspace surfaces | _pending Task 9_ | _pending Task 9_ | _pending_ | _pending_ |
| CP7 — teardown and findings triage | _pending Task 10_ | _pending Task 10_ | _pending_ | _pending_ |
