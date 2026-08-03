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

// catalogWithDocument is a catalog holding one ready document with the given title.
// The title is what the purge resolves the staged file name from, so it is the only
// field these tests vary.
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
	names []string
	err   error
}

func (p *recordingPurger) PurgeStagedDocument(_ context.Context, fileName string) error {
	p.names = append(p.names, fileName)
	return p.err
}

// TestDeleteRemovesTheStagedCopy is the regression for a live data-deletion
// failure, 2026-08-03.
//
// The operator deleted Clienti.xlsx from the cockpit. Everything the catalog owns
// worked: the document went status=deleted with deleted_at set, its asset went the
// same way in the same instant, and document_search stopped returning it. Then the
// same question was asked again — "quanti clienti hanno Località TORINO" — and she
// answered 699, because document_open had staged a copy inside the box and nothing
// removed it. The trace was document_search (nothing) → fs_glob (found it) →
// shell_exec → the answer.
//
// A delete that leaves the data readable by the agent is not a delete, so the purge
// runs on the catalog title — the name document_open stages under by default.
func TestDeleteRemovesTheStagedCopy(t *testing.T) {
	t.Parallel()
	purger := &recordingPurger{}
	service := &DeleteService{Catalog: catalogWithDocument(t, "Clienti.xlsx"), Staged: purger}

	if _, err := service.SoftDeleteDocument(context.Background(), testIdentity, testDocumentID); err != nil {
		t.Fatalf("SoftDeleteDocument: %v", err)
	}
	if len(purger.names) != 1 || purger.names[0] != "Clienti.xlsx" {
		t.Fatalf("staged purge = %v, want exactly [Clienti.xlsx]", purger.names)
	}
}

// A box that cannot be reached must NOT fail the operator's delete: the catalog row
// is the authoritative record and it is already closed. The failure is loud in the
// log instead, because it means bytes the operator asked to be gone are still
// readable — and the orphan-cleanup endpoint does not cover this: it reconciles the
// object store, not the box volume.
func TestDeleteSurvivesAnUnreachableBox(t *testing.T) {
	t.Parallel()
	purger := &recordingPurger{err: errors.New("box is down")}
	service := &DeleteService{Catalog: catalogWithDocument(t, "Clienti.xlsx"), Staged: purger}

	deleted, err := service.SoftDeleteDocument(context.Background(), testIdentity, testDocumentID)
	if err != nil {
		t.Fatalf("a purge failure blocked the operator's delete: %v", err)
	}
	if deleted.ID != testDocumentID {
		t.Fatalf("deleted = %+v, want the document", deleted)
	}
}

// No purger wired (the CLI and pool-free paths) and an untitled document must both
// be no-ops rather than panics: the delete still has to complete.
func TestDeleteWithoutAPurgerOrATitle(t *testing.T) {
	t.Parallel()
	if _, err := (&DeleteService{Catalog: catalogWithDocument(t, "Clienti.xlsx")}).
		SoftDeleteDocument(context.Background(), testIdentity, testDocumentID); err != nil {
		t.Fatalf("delete without a purger: %v", err)
	}
	purger := &recordingPurger{}
	if _, err := (&DeleteService{Catalog: catalogWithDocument(t, "   "), Staged: purger}).
		SoftDeleteDocument(context.Background(), testIdentity, testDocumentID); err != nil {
		t.Fatalf("delete of an untitled document: %v", err)
	}
	if len(purger.names) != 0 {
		t.Fatalf("a blank title reached the purge as %v; joined to the documents directory it would name the directory itself", purger.names)
	}
}
