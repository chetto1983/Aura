//go:build db_integration

package documents

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestOwnedIngestCreatesAStoredCatalogRow(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	identityID := seedDocumentTestIdentity(t, ctx, pool)
	sourceID := fmt.Sprintf("owned-cli-catalog-%d", time.Now().UnixNano())
	path := writeNamedTempFile(t, "owned-cli.pdf", "owned CLI catalog fixture")
	service := &Service{
		Jobs:    NewPostgresJobStore(pool),
		Catalog: NewPostgresCatalogStore(pool),
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.document_ingest_jobs WHERE source_id = $1", sourceID)
	})

	job, err := service.IngestPath(ctx, IngestRequest{
		SourceID: sourceID, SourceKind: "local", IdentityID: identityID,
	}, path)
	if err != nil {
		t.Fatalf("owned ingest: %v", err)
	}
	// accepted is the resting state, not a stall. Registration hands off to the pipeline,
	// and activation settles aura.documents rather than this ledger — nothing has written
	// JobSearchable since the convergence, so asserting it here only pinned a transition
	// that no longer exists.
	if job.Status != JobAccepted {
		t.Fatalf("job status = %q, want %q", job.Status, JobAccepted)
	}

	var catalogID, catalogIdentity, status, catalogJobID, catalogSourceKey string
	err = asDocumentIdentity(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
SELECT id::text, identity_id::text, status,
       metadata->>'document_job_id', source_key
FROM aura.documents
WHERE metadata->>'search_document_id' = $1
  AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT 1`, job.DocumentID).Scan(
			&catalogID, &catalogIdentity, &status, &catalogJobID, &catalogSourceKey,
		)
	})
	if err != nil {
		t.Fatalf("read owned catalog row: %v", err)
	}
	t.Cleanup(func() {
		_ = asDocumentIdentity(context.Background(), pool, identityID, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), "DELETE FROM aura.documents WHERE id = $1", catalogID)
			return err
		})
	})
	// Stored, not ready: 0093 gave the pipeline a later stage, and publishing is now its
	// job. A row that said 'ready' the instant it was registered is exactly the claim the
	// convergence removed — it advertised a document nothing had converted yet.
	//
	// The locator is asserted through the source_key COLUMN, not metadata: ingest stopped
	// copying source_id into the metadata blob when the column became the real one, so
	// reading it back from metadata would compare "" against "" and prove nothing.
	wantSourceKey, err := SourceKey(sourceID, "")
	if err != nil {
		t.Fatalf("SourceKey: %v", err)
	}
	if catalogIdentity != identityID || status != string(DocumentStatusStored) ||
		catalogJobID != job.ID || catalogSourceKey != wantSourceKey {
		t.Fatalf(
			"catalog row = identity:%q status:%q job:%q source_key:%q (want source_key %q)",
			catalogIdentity, status, catalogJobID, catalogSourceKey, wantSourceKey,
		)
	}
}
