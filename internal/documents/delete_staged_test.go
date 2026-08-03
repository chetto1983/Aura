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

// No purger wired — the CLI and pool-free compositions — must be a no-op, not a
// panic in the middle of an operator's delete.
func TestDeleteWithoutAPurger(t *testing.T) {
	t.Parallel()
	if _, err := (&DeleteService{Catalog: catalogWithDocument(t, "Clienti.xlsx")}).
		SoftDeleteDocument(context.Background(), testIdentity, testDocumentID); err != nil {
		t.Fatalf("delete without a purger: %v", err)
	}
}

// TestDeletePurgesTheNameStagingActuallyUsED — the purge must resolve the name
// through documentFileName, exactly as OpenDocument does, and NOT through the raw
// catalog title.
//
// The first version of this fix passed the title straight through. A title is an
// uploaded file name: it can carry separators, or resolve to a dotted name, and in
// those cases the staging side does not use it at all — it writes
// "document-<sha12>" instead. A purge built on the raw title would then name a path
// nothing was ever written to, remove NOTHING, report success, and leave the
// operator's document readable. That is worse than not trying, and no test caught
// it: it took an adversarial re-read of the shipped fix.
func TestDeletePurgesTheNameStagingActuallyUses(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		title string
		want  []string
	}{
		"ordinary title is used verbatim": {title: "Clienti.xlsx", want: []string{"Clienti.xlsx"}},
		// filepath.Base strips the directory, so a title carrying one still stages —
		// and must still be purged — under its base name.
		"title with separators keeps only the base": {title: "sub/dir/Clienti.xlsx", want: []string{"Clienti.xlsx"}},
		// These resolve to nothing usable, so staging substitutes the digest name.
		// Purging the raw title here would have been the silent no-op.
		"blank title falls back to the digest name":  {title: "   ", want: []string{"document-unknown"}},
		"dotted title falls back to the digest name": {title: ".hidden", want: []string{"document-unknown"}},
		"dot title falls back to the digest name":    {title: "..", want: []string{"document-unknown"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			purger := &recordingPurger{}
			service := &DeleteService{Catalog: catalogWithDocument(t, tc.title), Staged: purger}
			if _, err := service.SoftDeleteDocument(context.Background(), testIdentity, testDocumentID); err != nil {
				t.Fatalf("SoftDeleteDocument: %v", err)
			}
			if len(purger.names) != len(tc.want) || purger.names[0] != tc.want[0] {
				t.Fatalf("purged %v, want %v — this is the name OpenDocument stages under", purger.names, tc.want)
			}
		})
	}
}
