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
		"-- name: CreateStorageObject :one",
		"-- name: CreateIngestionJob :one",
		"-- name: ClaimIngestionJobs :many",
		"-- name: UpdateIngestionJobStatus :one",
		"-- name: AppendIngestionEvent :one",
		"-- name: CreateDeleteJob :one",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("document control-plane queries missing %q", want)
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
