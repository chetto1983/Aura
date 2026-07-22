package db

import (
	"os"
	"strings"
	"testing"
)

func TestRetentionOperationSQLContract(t *testing.T) {
	migration, err := os.ReadFile("migrations/0044_retention_operations.up.sql")
	if err != nil {
		t.Fatalf("read retention migration: %v", err)
	}
	queries, err := os.ReadFile("queries/retention_operations.sql")
	if err != nil {
		t.Fatalf("read retention queries: %v", err)
	}

	schema := string(migration)
	for _, fragment := range []string{
		"CREATE TABLE aura.retention_operations",
		"CREATE TABLE aura.retention_operation_items",
		"planned", "deleting", "retryable", "completed", "failed",
		"expected_version", "artifact_result", "lease_expires_at",
		"completed_bytes", "failure_class",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("retention migration missing %q", fragment)
		}
	}

	queryText := string(queries)
	for _, fragment := range []string{
		"-- name: CreateRetentionOperation",
		"-- name: CreateRetentionItem",
		"-- name: AuthorizeRetentionOperation",
		"-- name: ClaimRetentionItems",
		"operation.status IN ('deleting', 'retryable')",
		"FOR UPDATE SKIP LOCKED",
		"ORDER BY",
		"LIMIT",
		"-- name: RecordRetentionArtifactResult",
		"-- name: FinalizeRetentionItem",
		"-- name: RetryRetentionItem",
	} {
		if !strings.Contains(queryText, fragment) {
			t.Errorf("retention queries missing %q", fragment)
		}
	}
}
