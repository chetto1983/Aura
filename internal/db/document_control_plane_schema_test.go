package db

import (
	"os"
	"strings"
	"testing"
)

func TestDocumentControlPlaneMigrationContract(t *testing.T) {
	body := readSchemaContractFile(t, "migrations/0025_document_control_plane.up.sql")
	for _, want := range []string{
		"CREATE TABLE aura.documents",
		"CREATE TABLE aura.document_tags",
		"CREATE TABLE aura.document_versions",
		"CREATE TABLE aura.storage_objects",
		"CREATE TABLE aura.ingestion_jobs",
		"CREATE TABLE aura.ingestion_events",
		"CREATE TABLE aura.document_chunks",
		"CREATE TABLE aura.document_embeddings",
		"CREATE TABLE aura.delete_jobs",
		"CREATE TABLE aura.audit_logs",
		"CREATE INDEX document_tags_tag_document_idx",
		"USING GIN (tags)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("document control-plane migration missing %q", want)
		}
	}
}

func TestDocumentControlPlaneQueryContract(t *testing.T) {
	body := readSchemaContractFile(t, "queries/document_control_plane.sql")
	for _, want := range []string{
		"-- name: CreateDocument :one",
		// GetDocumentBySearchID, not GetDocument: the only lookup Go performs is by the
		// `doc_<hex>` search id every writer already holds, never by catalog uuid.
		"-- name: GetDocumentBySearchID :one",
		"-- name: DeleteDocumentTags :exec",
		"-- name: UpsertDocumentTag :exec",
		"-- name: CreateIngestionJob :one",
		"-- name: ClaimIngestionJobs :many",
		"-- name: HeartbeatIngestionJob :one",
		"-- name: UpdateIngestionJobStatus :one",
		"-- name: RetryIngestionJob :one",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("document control-plane queries missing %q", want)
		}
	}
	// These must not come back by accident. Their tables all still stand, so a re-added
	// statement compiles and runs — against a delete queue no worker claims from, or as a
	// second operator-facing catalog API with no route, CLI verb or scheduler path to
	// reach it. That gap between "the SQL is valid" and "something calls it" is how the
	// delete workflow stayed alive on paper long after its worker was deleted.
	for _, forbidden := range []string{
		"-- name: SoftDeleteDocument",
		"-- name: CreateDeleteJob",
		"-- name: ClaimDeleteJobs",
		"-- name: FinalizeDocumentDelete",
		"-- name: ListDocuments",
		"-- name: UpdateDocument ",
		"-- name: ListDocumentVersions",
		// aura.storage_objects now has exactly one writer and no reader: the ledger row
		// is written inside ReservePipelineCandidateVersion, and the orphan reconciler
		// that read it back was never constructed by anything.
		"-- name: ListStorageObjects",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("document control-plane queries revive an uncalled statement via %q", forbidden)
		}
	}
}

// The `status <> 'deleting'` guard on CreateDocument — and documents.ErrDocumentDeleteInFlight,
// the sentinel its zero-row result raises — read that status as a row STRANDED by the delete
// workflow 6519956a2 removed, not as a delete in progress: nothing writes it, so nothing will
// ever finish it, and both comments tell the reader that waiting is pointless. That reading
// survives only while the writer stays gone. Re-adding one makes the state transient again and
// those comments false, and this is where that has to surface — not in a support conversation
// with someone told to wait for a delete that is genuinely still running.
func TestNoQueryRevivesTheStrandedDocumentStatus(t *testing.T) {
	for _, file := range []string{
		"queries/document_control_plane.sql",
		"queries/document_pipeline.sql",
	} {
		packed := strings.Join(strings.Fields(readSchemaContractFile(t, file)), "")
		// Two forms because the column may be assigned first or after another one; packing the
		// whitespace out first makes both immune to how the statement is laid out.
		for _, forbidden := range []string{"SETstatus='deleting'", ",status='deleting'"} {
			if strings.Contains(packed, forbidden) {
				t.Fatalf("%s writes aura.documents.status = 'deleting' again — revisit "+
					"documents.ErrDocumentDeleteInFlight, which records that state as permanently stranded", file)
			}
		}
	}
}

func TestDocumentPipelineConvergenceMigrationContract(t *testing.T) {
	up := readSchemaContractFile(t, "migrations/0093_document_pipeline_convergence.up.sql")
	for _, want := range []string{
		"CREATE TABLE aura.document_pipeline_quarantine",
		"CREATE TABLE aura.document_pipeline_stages",
		"ADD COLUMN identity_id uuid",
		"ADD COLUMN lease_generation bigint",
		"documents_identity_search_document_live_idx",
		"document_versions_document_sha256_live_idx",
		"document_versions_document_identity_fkey",
		"storage_objects_document_identity_fkey",
		"ingestion_events_job_identity_fkey",
		"AS RESTRICTIVE FOR ALL TO aura_app",
		"REVOKE ALL ON aura.document_pipeline_quarantine FROM aura_app",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("0093 up migration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"SET identity_id = '00000000-0000-0000-0000-000000000001'",
		"COALESCE(identity_id, '00000000-0000-0000-0000-000000000001'",
	} {
		if strings.Contains(up, forbidden) {
			t.Fatalf("0093 up migration fabricates an owner via %q", forbidden)
		}
	}

	down := readSchemaContractFile(t, "migrations/0093_document_pipeline_convergence.down.sql")
	for _, want := range []string{
		"verified object deletion requires forward repair",
		"jsonb_populate_record",
		"DROP TABLE aura.document_pipeline_quarantine",
		"DROP TABLE aura.document_pipeline_stages",
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("0093 down migration missing %q", want)
		}
	}
}

func readSchemaContractFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
