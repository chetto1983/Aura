package documents

import (
	"context"
	"errors"
	"testing"
)

const (
	testIdentity   = "00000000-0000-0000-0000-000000000001"
	testDocumentID = "10000000-0000-0000-0000-000000000001"
)

// catalogWithDocument varies only the title to prove erasure keys exclusively on the
// stable document ID, never a caller-controlled filename.
func catalogWithDocument(t *testing.T, title string) *CatalogService {
	t.Helper()
	return &CatalogService{Store: &fakeCatalogStore{detail: DocumentDetail{
		Document: Document{
			ID:         testDocumentID,
			IdentityID: testIdentity,
			Title:      title,
			Status:     DocumentStatusReady,
		},
	}}}
}

type recordingPurger struct {
	documentIDs []string
	err         error
}

func (p *recordingPurger) PurgeStagedDocument(_ context.Context, documentID string) error {
	p.documentIDs = append(p.documentIDs, documentID)
	return p.err
}

// The synchronous purge immediately removes agent-readable staging while the durable
// worker verifies the same deterministic directory before finalization.
func TestDeleteRemovesTheStagedCopy(t *testing.T) {
	t.Parallel()
	purger := &recordingPurger{}
	service := &DeleteService{Catalog: catalogWithDocument(t, "synthetic-ledger.xlsx"), Staged: purger}

	if _, err := service.SoftDeleteDocument(context.Background(), testIdentity, testDocumentID); err != nil {
		t.Fatalf("SoftDeleteDocument: %v", err)
	}
	if len(purger.documentIDs) != 1 || purger.documentIDs[0] != testDocumentID {
		t.Fatalf("staged purge = %v, want exactly [%s]", purger.documentIDs, testDocumentID)
	}
}

// A transient sandbox fault cannot roll back the already-enqueued delete. The worker
// retries the purge and leaves the document deleting until verification succeeds.
func TestDeleteSurvivesAnUnreachableBox(t *testing.T) {
	t.Parallel()
	purger := &recordingPurger{err: errors.New("box is down")}
	service := &DeleteService{Catalog: catalogWithDocument(t, "synthetic-ledger.xlsx"), Staged: purger}

	deleted, err := service.SoftDeleteDocument(context.Background(), testIdentity, testDocumentID)
	if err != nil {
		t.Fatalf("a purge failure blocked the operator's delete: %v", err)
	}
	if deleted.ID != testDocumentID {
		t.Fatalf("deleted = %+v, want the document", deleted)
	}
}

// No purger wired — the CLI and pool-free compositions — must be a no-op, not a
// panic in the middle of an operator's delete.
func TestDeleteWithoutAPurger(t *testing.T) {
	t.Parallel()
	if _, err := (&DeleteService{Catalog: catalogWithDocument(t, "synthetic-ledger.xlsx")}).
		SoftDeleteDocument(context.Background(), testIdentity, testDocumentID); err != nil {
		t.Fatalf("delete without a purger: %v", err)
	}
}

// Every alias now lives inside the same deterministic document directory. The
// title can therefore never make erasure miss a caller-selected open filename.
func TestDeletePurgesWholeDocumentDirectoryRegardlessOfTitle(t *testing.T) {
	t.Parallel()
	for name, title := range map[string]string{
		"ordinary": "synthetic-ledger.xlsx", "path-shaped": "sub/dir/synthetic-ledger.xlsx",
		"blank": "   ", "dotted": ".hidden", "dot": "..",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			purger := &recordingPurger{}
			service := &DeleteService{Catalog: catalogWithDocument(t, title), Staged: purger}
			if _, err := service.SoftDeleteDocument(context.Background(), testIdentity, testDocumentID); err != nil {
				t.Fatalf("SoftDeleteDocument: %v", err)
			}
			if len(purger.documentIDs) != 1 || purger.documentIDs[0] != testDocumentID {
				t.Fatalf("purged %v, want whole directory for %s", purger.documentIDs, testDocumentID)
			}
		})
	}
}
