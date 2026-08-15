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
		"-- name: ListDocuments :many",
		"-- name: GetDocument :one",
		"-- name: UpdateDocument :one",
		"-- name: UpdateDocumentTags :one",
		"-- name: DeleteDocumentTags :exec",
		"-- name: UpsertDocumentTag :exec",
		"-- name: CreateDocumentVersion :one",
		// ListStorageObjects, not CreateStorageObject: the object ledger's writes travel
		// inside the statements that own them, so the read for orphan detection is the only
		// storage-object query left with a Go caller.
		"-- name: ListStorageObjects :many",
		"-- name: CreateIngestionJob :one",
		"-- name: ClaimIngestionJobs :many",
		"-- name: HeartbeatIngestionJob :one",
		"-- name: UpdateIngestionJobStatus :one",
		"-- name: RetryIngestionJob :one",
		"-- name: CreateDeleteJob :one",
		"-- name: ClaimDeleteJobs :many",
		"-- name: FinalizeDocumentDelete :one",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("document control-plane queries missing %q", want)
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
