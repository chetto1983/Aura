# Repeat-Source Ingest Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Revised 2026-08-05 after the implementer surfaced a false premise.** The original plan assumed `SoftDeleteDocument` sets `deleted_at`. It does not — it sets `status='deleting'` and enqueues a delete job; `documents.deleted_at` is written only by `FinalizeDocumentDelete` (`internal/db/queries/document_control_plane.sql:704`) after the durable worker erases the objects. Steps 2, 3, 6, 7, 9 and 10 below are the corrected versions. The consequence and the operator's ruling on it are in "The delete window" below.

**Goal:** Let a deleted document be ingested again, make re-ingesting a live one idempotent, and refuse — loudly — to attach a new upload to a document that is still being deleted.

**Architecture:** `aura.documents_source_unique` is a full UNIQUE constraint over `(identity_id, source_kind, source_key)`, so a soft-deleted row keeps occupying its source forever. Replace it with a partial unique index `WHERE deleted_at IS NULL` — mirroring its sibling `documents_identity_search_document_live_idx` three lines below it — and give `CreateDocument` an `ON CONFLICT ... DO UPDATE` targeting that index, guarded so it never lands on a dying row.

**Tech Stack:** PostgreSQL 17, golang-migrate, sqlc v1.31.1, pgx/v5, Go 1.26.

## Global Constraints

- Go toolchain and sqlc are **WSL only**: `wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && <cmd>'`. Never run a `.exe` on the Windows host.
- **`db_integration` tests use disposable databases ONLY.** Use `pipelineDisposablePool(t)`. **Never `migratedDocumentPool(t)`** — it migrates whatever `AURA_DB_MIGRATE_URL` names, locally the live `aura` database. A 2026-07-10 run of that shape truncated a live deployment's auth tables with no backup.
- **RLS is on (`0087`).** A raw `pool.Query` against `aura.documents` returns zero rows regardless of what is stored. Every direct SQL assertion must run inside `asDocumentIdentity(ctx, pool, identityID, func(tx pgx.Tx) error { ... })`, as every existing test in this package does.
- A `db_integration` run that finishes in under a second **skipped**. A skip is not a pass and not a red.
- No file exceeds 600 LOC.
- Comments only where the *why* is non-obvious; identifier names carry the *what*.
- Commit directly on `master`. Do not push — pushing is gated on a separate quality-gate plan.
- `--no-verify` is forbidden.
- Never modify a test to make it pass unless the test itself is broken.

## Why this is an edit to 0093 and not a new migration

Migration `0093_document_pipeline_convergence` is committed (in `8d2701bd1`) but applied **nowhere**: live `aura` reports `schema_migrations = 92`, and `aura.documents.source_kind` — which 0093 adds `NOT NULL` — does not exist there. `8d2701bd1` is itself unpushed. So 0093 is still editable in place. Do not allocate a new migration number.

If `ls internal/db/migrations/ | tail -1` shows anything above 0093, or the live database reports 93+, **stop and report** — the premise is void and the change needs its own migration.

## The failure this fixes, precisely

`documentForAssetVersion` (`internal/documents/catalog_store_asset.go:108`) does check-then-insert:

1. `GetDocumentBySearchID` filters `deleted_at IS NULL`, so a fully-deleted document is **not found**.
2. It therefore calls `createDocument`, a bare `INSERT` with no `ON CONFLICT`.
3. The insert violates `documents_source_unique`, which **does** still count the deleted row → `23505`.

The same shape also loses a race: two concurrent first-uploads of one file both miss the `SELECT`, and the loser gets a 23505. Both are fixed by the same change, because `ON CONFLICT` makes the insert atomic rather than advisory.

## The delete window — operator ruling

Deletion is asynchronous. Between `SoftDeleteDocument` (sets `status='deleting'`, enqueues the job) and `FinalizeDocumentDelete` (sets `deleted_at`), the row is still `deleted_at IS NULL` and therefore still occupies the partial index.

A naked `DO UPDATE` would upsert onto that dying row, so a re-upload arriving mid-delete would attach to a document whose finalize then erases it. Today that same case is a loud `23505`. Converting a loud failure into silent data loss is a regression this change would introduce.

**Ruling: refuse it, with a typed error.** The `DO UPDATE` carries `WHERE documents.status <> 'deleting'`, so the statement yields no row, and `createDocument` maps that to `ErrDocumentDeleteInFlight`. The caller learns the document is mid-deletion instead of quietly joining it.

Rejected: freeing the source at delete-*start* (predicate `... AND status NOT IN ('deleting','deleted')`). The sibling `documents_identity_search_document_live_idx` has the identical window and would raise the 23505 instead, so it would need the same predicate, which changes `GetDocumentBySearchID`'s semantics. Larger scope, more risk, and it belongs to whoever owns the delete lifecycle.

## Safety checks already performed — do not redo them

- **Nothing depends on `documents_source_unique`.** No foreign key targets `(identity_id, source_kind, source_key)`; every FK into `aura.documents` goes through `(id)` or `(id, identity_id)`, backed by `documents_id_identity_unique`, untouched here. No `ON CONFLICT ON CONSTRAINT documents_source_unique` exists. This mattered because a partial unique *index* cannot back a foreign key.
- **sqlc handles the idiom.** `ON CONFLICT (cols) WHERE pred` already appears in this query file and generates cleanly — `internal/db/sqlc/document_control_plane.sql.go:643`.
- **No existing test pins the 23505**, so nothing has to be un-pinned.

## Why the upsert writes only `updated_at`

`DO NOTHING` returns no row, and `CreateDocument` is `:one` with `RETURNING *` — it must come back with the existing document. `SET updated_at = now()` is the minimal write that makes `RETURNING` fire; the idiom already appears twice in this file.

It deliberately does **not** refresh `title`, `tags` or `metadata`: re-ingesting a file must not silently overwrite an operator's edits. It must also not touch `identity_id`, `source_kind`, `source_key`, `search_document_id`, or lower `pipeline_generation` — the `aura.document_identity_immutable` BEFORE-UPDATE trigger (`0093:312`) raises `23514` on any of those. `SET updated_at = now()` changes none of them, so the trigger passes; T2 proves it rather than assuming it.

## Out of scope

- The production-path proof — delete a document, then re-upload the same file through `RecordAssetVersion`. That belongs to the production E2E plan (PRD amendments #115/#116), already the next scheduled item.
- Any change to `documentForAssetVersion`'s check-then-insert shape, or to `GetDocumentBySearchID`. Both become correct for free once the insert is atomic.
- Surfacing `ErrDocumentDeleteInFlight` as a specific HTTP status at the API edge. This plan introduces the typed error and proves the store returns it; routing it is the ingress's job.

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `internal/db/migrations/0093_document_pipeline_convergence.up.sql` | Forward schema | Drop the constraint from the ALTER; add a partial unique index beside its sibling |
| `internal/db/migrations/0093_document_pipeline_convergence.down.sql` | Reverse schema | Drop the index in the DROP INDEX block; remove the constraint from the ALTER |
| `internal/db/queries/document_control_plane.sql` | `CreateDocument` | Add the guarded `ON CONFLICT` clause |
| `internal/db/sqlc/*.go` | Generated | Regenerate — never hand-edit |
| `internal/documents/catalog_store.go` | `createDocument` | Map the empty result to a typed error |
| `internal/documents/errors.go` *(or the package's existing sentinel home)* | `ErrDocumentDeleteInFlight` | Add the sentinel beside its siblings |
| `internal/documents/catalog_store_source_conflict_integration_test.go` | The three guarantees | **Create** (`db_integration`) |

---

### Task 1: Free the source key on delete, make repeat ingest idempotent, refuse mid-delete

**Files:**
- Create: `internal/documents/catalog_store_source_conflict_integration_test.go`
- Modify: `internal/db/migrations/0093_document_pipeline_convergence.up.sql` (constraint → partial index)
- Modify: `internal/db/migrations/0093_document_pipeline_convergence.down.sql` (mirror)
- Modify: `internal/db/queries/document_control_plane.sql` (`CreateDocument`)
- Modify: `internal/documents/catalog_store.go` (`createDocument` error mapping)
- Modify: wherever this package declares its `Err…` sentinels (`ErrDocumentNotCatalogued` names the file)
- Regenerate: `internal/db/sqlc/`

**Interfaces:**
- Consumes: `pipelineDisposablePool(t)`, `newPipelineStoreFixture(t, ctx, pool, pipeline)`, `asDocumentIdentity(ctx, pool, identityID, fn)`, `NewPostgresPipelineStore(pool)`, `NewPostgresDurableDeleteStore(pool)`, `normalizedDeleteSnapshot(job)`, `fenceForDeleteJob(job)`, `NewPostgresCatalogStore(pool)`.
- Produces: `ErrDocumentDeleteInFlight`. `CreateDocument` goes from "insert or 23505" to "insert, or return the existing live document unchanged, or `ErrDocumentDeleteInFlight`".

- [ ] **Step 1: Confirm the plan's premise still holds**

```bash
ls internal/db/migrations/ | tail -2
docker exec aura-postgres psql -U aura_migrate -d aura -tAc "select version from public.schema_migrations;"
```
Expected: highest migration is `0093_document_pipeline_convergence`, live version `92`. If either differs, **STOP and report**.

- [ ] **Step 2: Write the failing tests**

Create `internal/documents/catalog_store_source_conflict_integration_test.go`.

Three tests, each pinning a different guarantee. Note the delete test drives the **real** durable delete path to completion rather than manufacturing `deleted_at` — `SoftDeleteDocument` alone only reaches `status='deleting'`, and a test that set `deleted_at` itself would not notice a finalize that stopped setting it. Follow the precedent in `internal/documents/delete_durable_integration_test.go:15-90` exactly: `SoftDeleteDocument` → `Claim` → `normalizedDeleteSnapshot` → `MarkObjectDeleted` for every object → `Finalize{ProjectionVerified: true}`.

`newPipelineStoreFixture` builds the document, so read its `source_kind`/`source_key` back out (inside `asDocumentIdentity` — RLS) and re-ingest with those exact values.

```go
//go:build db_integration

// What a document's source key means while it is dying, and after it is gone.
//
// aura.documents_source_unique was a FULL unique constraint over
// (identity_id, source_kind, source_key), so a deleted row kept owning its source forever
// and re-ingesting the same file returned 23505 — the user-visible shape being "delete a
// document, upload it again, nothing happens". documentForAssetVersion could not see it
// coming: it looks the document up with deleted_at IS NULL, finds nothing, and inserts
// into a constraint still counting the row it could not see.
//
// Deletion is asynchronous, which is what makes three tests necessary rather than one.
// deleted_at is written by FinalizeDocumentDelete, not by SoftDeleteDocument, so between
// them the row is live to the index and a naked upsert would attach a fresh upload to a
// document whose finalize then erases it. That case must fail loudly, not quietly.

package documents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCatalogStoreReleasesSourceKeyOnFinalizedDelete(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture := newPipelineStoreFixture(t, ctx, pool, NewPostgresPipelineStore(pool))
	catalog := NewPostgresCatalogStore(pool)
	kind, key := documentSource(t, ctx, pool, fixture.identityID, fixture.documentID)

	finalizeDocumentDelete(t, ctx, pool, fixture.identityID, fixture.documentID)

	reingested, err := catalog.CreateDocument(ctx,
		sourceConflictRequest(fixture.identityID, kind, key, "Second upload"))
	if err != nil {
		t.Fatalf("re-ingest after finalized delete: %v", err)
	}
	if reingested.ID == fixture.documentID {
		t.Fatalf("re-ingest resurrected the deleted document %s", fixture.documentID)
	}
	if got := liveDocumentsForSource(t, ctx, pool, fixture.identityID, key); got != 1 {
		t.Fatalf("live documents for the source = %d, want exactly 1", got)
	}
}

func TestCatalogStoreRefusesReingestWhileDeleteInFlight(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture := newPipelineStoreFixture(t, ctx, pool, NewPostgresPipelineStore(pool))
	catalog := NewPostgresCatalogStore(pool)
	kind, key := documentSource(t, ctx, pool, fixture.identityID, fixture.documentID)

	deleting, err := catalog.SoftDeleteDocument(ctx, fixture.identityID, fixture.documentID)
	if err != nil || deleting.Status != DocumentStatusDeleting {
		t.Fatalf("SoftDeleteDocument = (%#v, %v)", deleting, err)
	}

	_, err = catalog.CreateDocument(ctx,
		sourceConflictRequest(fixture.identityID, kind, key, "Upload during delete"))
	if !errors.Is(err, ErrDocumentDeleteInFlight) {
		t.Fatalf("re-ingest during delete = %v, want ErrDocumentDeleteInFlight; "+
			"a silent upsert would attach these bytes to a document the finalize erases", err)
	}
}

func TestCatalogStoreRepeatLiveSourceIsIdempotent(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture := newPipelineStoreFixture(t, ctx, pool, NewPostgresPipelineStore(pool))
	catalog := NewPostgresCatalogStore(pool)
	kind, key := documentSource(t, ctx, pool, fixture.identityID, fixture.documentID)

	repeat, err := catalog.CreateDocument(ctx,
		sourceConflictRequest(fixture.identityID, kind, key, "Re-upload title"))
	if err != nil {
		t.Fatalf("repeat create: %v", err)
	}
	if repeat.ID != fixture.documentID {
		t.Fatalf("repeat create minted a second document: %s then %s", fixture.documentID, repeat.ID)
	}
	if repeat.Title == "Re-upload title" {
		t.Fatal("repeat create overwrote the title; an operator's edit must survive re-ingest")
	}
	if got := liveDocumentsForSource(t, ctx, pool, fixture.identityID, key); got != 1 {
		t.Fatalf("live documents for the source = %d, want exactly 1", got)
	}
}
```

Then the helpers. `sourceConflictRequest` must carry the SAME `search_document_id` the fixture's document has, or the insert collides with `documents_identity_search_document_live_idx` instead and the test proves the wrong thing — read it back alongside the source and thread it through. Every direct query goes through `asDocumentIdentity`, because RLS returns zero rows otherwise.

```go
// documentSource reads back the fixture document's source and search id. Re-ingesting one
// file means reproducing all three: the source pair is what this change scopes to live
// rows, and the search id has its own live-only unique index that would otherwise be the
// constraint that fires.
func documentSource(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID, documentID string,
) (kind, key string) { /* SELECT source_kind, source_key ... inside asDocumentIdentity */ }

func liveDocumentsForSource(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID, key string,
) int { /* SELECT count(*) ... AND deleted_at IS NULL, inside asDocumentIdentity */ }

// finalizeDocumentDelete drives the real durable delete to completion, the way the worker
// does: claim the job, erase every object in its snapshot, then finalize with the
// projection verified. Only that path writes documents.deleted_at.
func finalizeDocumentDelete(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID, documentID string,
) { /* mirror delete_durable_integration_test.go:15-90 */ }
```

Write those three helper bodies out — the comment blocks above state their contracts, and `delete_durable_integration_test.go` has the exact calls. Also thread `search_document_id` into `sourceConflictRequest` per the note above.

- [ ] **Step 3: Run the tests to verify they fail**

```bash
wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && export POSTGRES_PASSWORD="$(grep -E "^POSTGRES_PASSWORD=" .env | cut -d= -f2- | tr -d "\"")" && go test -tags db_integration -run "TestCatalogStore(ReleasesSourceKeyOnFinalizedDelete|RefusesReingestWhileDeleteInFlight|RepeatLiveSourceIsIdempotent)" ./internal/documents/ -v'
```

Expected, against the ORIGINAL schema (constraint, no `ON CONFLICT`): all three FAIL. The first two with `23505` naming `documents_source_unique`; the third the same. `RefusesReingestWhileDeleteInFlight` will not compile until `ErrDocumentDeleteInFlight` exists — declare the sentinel first so the red is an assertion failure, not a build error.

A run under a second means the tier skipped and proved nothing.

- [ ] **Step 4: Swap the constraint for a partial unique index (up migration)**

In the `ALTER TABLE aura.documents` block, drop this line and turn the preceding comma into a semicolon:

```sql
    ADD CONSTRAINT documents_source_unique UNIQUE (identity_id, source_kind, source_key);
```

Then, immediately after the existing `documents_identity_search_document_live_idx`, add its sibling:

```sql
CREATE UNIQUE INDEX documents_identity_source_live_idx
    ON aura.documents (identity_id, source_kind, source_key)
    WHERE deleted_at IS NULL;
```

- [ ] **Step 5: Mirror it in the down migration**

Add to the existing `DROP INDEX` block as its first line:

```sql
DROP INDEX aura.documents_identity_source_live_idx;
```

And delete `    DROP CONSTRAINT documents_source_unique,` from the later `ALTER TABLE aura.documents` block.

- [ ] **Step 6: Make the insert a guarded, atomic get-or-create**

In `internal/db/queries/document_control_plane.sql`, `CreateDocument` ends `)\nRETURNING *;`. Change that ending to:

```sql
)
ON CONFLICT (identity_id, source_kind, source_key) WHERE deleted_at IS NULL
DO UPDATE SET updated_at = now()
    WHERE documents.status <> 'deleting'
RETURNING *;
```

The `DO UPDATE`'s own `WHERE` references the existing row as `documents` — PostgreSQL's alias for the conflict target is the unqualified table name, not the schema-qualified one. **Verify this against the PostgreSQL INSERT documentation before assuming it**, and let Step 3's third test confirm it at runtime; a wrong qualifier is a parse error, not a silent misbehaviour.

Put this comment directly above `-- name: CreateDocument :one`:

```sql
-- Get-or-create by source, refusing a document that is already being deleted.
-- DO UPDATE rather than DO NOTHING because :one needs a row back, and it touches ONLY
-- updated_at: re-ingesting a file must not overwrite a title or tags the operator edited,
-- and aura.document_identity_immutable raises 23514 on any write to identity, source,
-- search id, or a lowered pipeline_generation. The status guard matters because deletion
-- is asynchronous: deleted_at is set by FinalizeDocumentDelete, not by the soft delete, so
-- without it a re-upload mid-delete would silently join a document the finalize erases.
-- Zero rows is that case, and the caller turns it into ErrDocumentDeleteInFlight.
```

- [ ] **Step 7: Declare the sentinel and map the empty result**

Add `ErrDocumentDeleteInFlight` beside the package's existing sentinels — `ErrDocumentNotCatalogued` names the file. Match the surrounding declaration style:

```go
// ErrDocumentDeleteInFlight reports that a document with this source is still being
// deleted. Its bytes are on their way out, so attaching a new upload to it would hand the
// caller a document the delete's finalize is about to erase.
var ErrDocumentDeleteInFlight = errors.New("document with this source is being deleted")
```

In `internal/documents/catalog_store.go`'s `createDocument`, map the guarded upsert's empty result:

```go
	row, err := sc.q.CreateDocument(ctx, sqlc.CreateDocumentParams{ /* unchanged */ })
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Document{}, ErrDocumentDeleteInFlight
		}
		return Document{}, err
	}
```

Read the function first — keep whatever error wrapping it already does for the non-`ErrNoRows` path.

- [ ] **Step 8: Regenerate sqlc**

```bash
wsl -e bash -lc 'export PATH=$HOME/go/bin:$PATH; cd /mnt/d/Aura && sqlc generate'
```
Expected: exit 0, no output. Confirm the clause landed:
```bash
wsl -e bash -lc 'cd /mnt/d/Aura && grep -n "documents.status <> " internal/db/sqlc/document_control_plane.sql.go'
```

If `sqlc generate` errors, do NOT hand-edit the generated file and do NOT restructure the query to dodge it — report the exact error.

- [ ] **Step 9: Run the tests to verify they pass**

```bash
wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && export POSTGRES_PASSWORD="$(grep -E "^POSTGRES_PASSWORD=" .env | cut -d= -f2- | tr -d "\"")" && go test -tags db_integration -run "TestCatalogStore(ReleasesSourceKeyOnFinalizedDelete|RefusesReingestWhileDeleteInFlight|RepeatLiveSourceIsIdempotent)" ./internal/documents/ -v'
```
Expected: all three PASS, in seconds not milliseconds.

- [ ] **Step 10: Run the full suites**

```bash
wsl -e bash -lc 'export PATH=$HOME/.local/go1.26.3/bin:$HOME/go/bin:$PATH; cd /mnt/d/Aura && export POSTGRES_PASSWORD="$(grep -E "^POSTGRES_PASSWORD=" .env | cut -d= -f2- | tr -d "\"")" && go vet ./... && go build ./... && go test -race ./internal/documents/ ./internal/agui/ ./cmd/aura/ && go test -tags db_integration -race ./internal/documents/'
```
Expected: PASS throughout. The `db_integration` tier re-migrates from scratch on a disposable database, so a malformed 0093 surfaces as a migration failure rather than a subtle one.

- [ ] **Step 11: Commit**

Stage exactly the files this task touched — the migration pair, the query, every regenerated sqlc file, the sentinel's file, `catalog_store.go`, and the new test. Do NOT use `git add -A`; the tree carries unrelated dirty and untracked files.

```
Release a document's source key when it is deleted

documents_source_unique spanned every row, deleted ones included, so a deleted
document kept owning its source forever and re-ingesting the same file returned
23505. The path could not see it coming: documentForAssetVersion looks the
document up with deleted_at IS NULL, finds nothing, and inserts into a
constraint that is still counting the row it could not see. The user-visible
shape was deleting a document, uploading it again, and nothing happening.

A partial unique index scoped to live rows mirrors what
documents_identity_search_document_live_idx already does three lines below it,
and lets CreateDocument take an ON CONFLICT so the insert is an atomic
get-or-create rather than a check-then-insert two uploads can lose a race on.

Deletion is asynchronous, and that is why the upsert is guarded. deleted_at is
written by FinalizeDocumentDelete, not by the soft delete, so between the two
the dying row still occupies the index. An unguarded upsert would attach a
fresh upload to a document the finalize then erases — quieter than the 23505 it
replaced, and worse. Zero rows from the guard becomes ErrDocumentDeleteInFlight.

The upsert otherwise touches only updated_at: re-ingesting a file must not
overwrite a title or tags the operator edited, and document_identity_immutable
raises 23514 on any write to identity, source, search id, or a lowered
generation.

Migration 0093 is amended in place: it is committed but applied nowhere — live
aura is at schema_migrations 92 and has no source_kind column — so no slot is
burned on a schema that has never existed anywhere.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KzRkrbwUqo2oWsryXKc34F
```

---

## Self-Review

**Spec coverage.** Delete-then-reingest → T1 plus Steps 4-5. Live-repeat idempotence → T3 plus Step 6. The mid-delete window → T2 plus Steps 6-7. The race → the same `ON CONFLICT`, noted in the commit body. The immutability trigger's limits on what `DO UPDATE` may write → Step 6's comment and T3's title assertion. The "is 0093 still unapplied?" premise → Step 1, with an explicit STOP.

**Placeholder scan.** One deliberate exception: Step 2's three helper bodies are specified by contract and precedent rather than transcribed, because they are mechanical reads of a fixture the implementer can see and a delete sequence that already exists verbatim at `delete_durable_integration_test.go:15-90`. Everything else carries literal text. Both the `documents.` qualifier in Step 6 and `DocumentID`'s signature are flagged as verify-don't-assume rather than asserted.

**Type consistency.** `ErrDocumentDeleteInFlight` is declared in Step 7 and consumed by T2 in Step 2 — the plan notes it must be declared before Step 3's run so the red is an assertion, not a build failure. `SoftDeleteDocument(ctx, identityID, documentID) (Document, error)` matches `catalog_store.go:72`. `newPipelineStoreFixture`, `normalizedDeleteSnapshot` and `fenceForDeleteJob` are used exactly as `delete_durable_integration_test.go` uses them. All pools come from `pipelineDisposablePool`, never `migratedDocumentPool`.
