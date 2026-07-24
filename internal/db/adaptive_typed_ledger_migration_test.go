package db

import (
	"os"
	"strings"
	"testing"
)

func TestAdaptiveTypedLedgerMigrationSourceContract(t *testing.T) {
	upBytes, err := migrationsFS.ReadFile("migrations/0060_adaptive_typed_ledger.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, want := range []string{
		"event_kind IN ('decision', 'delivery', 'outcome', 'correction', 'promotion', 'rollback')",
		"adaptive_outbox_schema2_assignment_check",
		"adaptive_outbox_schema2_delivery_check",
		"adaptive_outbox_schema2_assignment_owner_decision_uidx",
		"adaptive_outbox_schema2_delivery_owner_decision_uidx",
		"adaptive_outbox_schema2_delivery_assignment",
		"payload->>'schema_version' = '2.0'",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("up migration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"DELETE FROM aura.adaptive_outbox",
		"UPDATE aura.adaptive_outbox",
	} {
		if strings.Contains(up, forbidden) {
			t.Errorf("up migration mutates immutable history via %q", forbidden)
		}
	}

	downBytes, err := migrationsFS.ReadFile("migrations/0060_adaptive_typed_ledger.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := string(downBytes)
	for _, want := range []string{
		"event_kind = 'delivery'",
		"RAISE EXCEPTION",
		"event_kind IN ('decision', 'outcome', 'correction', 'promotion', 'rollback')",
	} {
		if !strings.Contains(down, want) {
			t.Errorf("down migration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"DELETE FROM aura.adaptive_outbox",
		"INSERT INTO aura.adaptive_outbox",
	} {
		if strings.Contains(down, forbidden) {
			t.Errorf("down migration fabricates or deletes history via %q", forbidden)
		}
	}

	queryBytes, err := os.ReadFile("queries/adaptive_outbox.sql")
	if err != nil {
		t.Fatal(err)
	}
	queries := string(queryBytes)
	for _, want := range []string{
		"-- name: LockSchema2AdaptiveAssignment :one",
		"-- name: GetSchema2AdaptiveDelivery :one",
		"-- name: ListEligibleSchema2AdaptiveAggregateFacts :many",
		"payload->>'schema_version' = '2.0'",
		"FOR UPDATE",
	} {
		if !strings.Contains(queries, want) {
			t.Errorf("adaptive query source missing %q", want)
		}
	}
}
