package db

import (
	"os"
	"path/filepath"
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

// The queue statements outlived the catalog they shared a file with. This asserts what the
// file is now: aura.ingestion_jobs and nothing else. It replaces the contract that pinned
// CreateDocument/GetDocumentBySearchID/DeleteDocumentTags/UpsertDocumentTag, which migration
// 0098 removed along with the tables they wrote.
func TestIngestionJobQueryContract(t *testing.T) {
	body := readSchemaContractFile(t, "queries/ingestion_jobs.sql")
	for _, want := range []string{
		"-- name: CreateIngestionJob :one",
		"-- name: ClaimIngestionJobs :many",
		"-- name: HeartbeatIngestionJob :one",
		"-- name: UpdateIngestionJobStatus :one",
		"-- name: RetryIngestionJob :one",
		"-- name: CountIngestionJobsByStatus :one",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("ingestion job queries missing %q", want)
		}
	}
}

// No query may name the document catalog again.
//
// This is the sweep the previous contract could not be: it read ONE file and listed
// statements by name, so a catalog statement reintroduced in any other file passed. The
// tables are gone now, which changes the failure mode rather than removing it — a statement
// naming one no longer compiles into a wrong answer, it makes sqlc generate fail and takes
// the build down. Failing here says why, in one line, instead of in sqlc's parser output.
//
// aura.ingestion_jobs and aura.ingestion_events are deliberately NOT in this list: they are
// the generic asset queue and its audit trail, which image and audio ride too, and 0098 keeps
// both.
func TestNoQueryRevivesTheDocumentCatalog(t *testing.T) {
	dropped := []string{
		"aura.documents",
		"aura.document_versions",
		"aura.document_tags",
		"aura.document_chunks",
		"aura.document_embeddings",
		"aura.document_ingest_jobs",
		"aura.document_pipeline_stages",
		"aura.document_pipeline_quarantine",
		"aura.storage_objects",
		"aura.delete_jobs",
	}
	files, err := filepath.Glob("queries/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no query files found: this sweep would pass vacuously")
	}
	for _, file := range files {
		body := readSchemaContractFile(t, file)
		for _, table := range dropped {
			// Word-boundary by suffix: "aura.document_versions" must not match inside
			// "aura.document_versions_history" if such a table is ever added.
			for _, form := range []string{table + " ", table + "\n", table + "(", table + ";", table + ","} {
				if strings.Contains(body, form) {
					t.Fatalf("%s names %s, which migration 0098 dropped", file, table)
				}
			}
		}
	}
}

func TestRetireDocumentCatalogMigrationContract(t *testing.T) {
	up := readSchemaContractFile(t, "migrations/0098_retire_document_catalog.up.sql")
	for _, want := range []string{
		"DROP TABLE IF EXISTS",
		"aura.documents;",
		"aura.document_versions,",
		"DROP FUNCTION IF EXISTS aura.document_identity_immutable()",
		"DROP COLUMN IF EXISTS catalog_document_id",
		"DROP COLUMN IF EXISTS document_version_id",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("0098 up migration missing %q", want)
		}
	}
	// The generic asset queue and its audit trail are what image and audio processing ride.
	// Dropping either here would take vision summaries and STT down with the catalog, and the
	// blast radius would not show up until an upload of a kind this migration never mentions.
	//
	// Scoped to the DROP TABLE statement rather than to the whole file: both names appear in
	// the migration's prose, explaining why they are kept, and a check that cannot tell a
	// comment from a statement would forbid saying so.
	dropList := up[strings.Index(up, "DROP TABLE IF EXISTS"):]
	dropList = dropList[:strings.Index(dropList, ";")]
	for _, forbidden := range []string{"aura.ingestion_jobs", "aura.ingestion_events"} {
		if strings.Contains(dropList, forbidden) {
			t.Fatalf("0098 up migration drops %q, which the asset queue still needs", forbidden)
		}
	}

	down := readSchemaContractFile(t, "migrations/0098_retire_document_catalog.down.sql")
	for _, want := range []string{
		"CREATE TABLE aura.documents",
		"CREATE TABLE aura.document_versions",
		"CREATE TABLE aura.storage_objects",
		"CREATE OR REPLACE FUNCTION aura.document_identity_immutable()",
		"ADD COLUMN IF NOT EXISTS catalog_document_id uuid",
		"ADD CONSTRAINT ingestion_jobs_version_identity_fkey",
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("0098 down migration missing %q", want)
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
