package db

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestAdaptiveTypedLedgerMigrationSourceContract(t *testing.T) {
	upBytes, err := migrationsFS.ReadFile("migrations/0060_adaptive_typed_ledger.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(upBytes)); got !=
		"1efe1d3a2619d22b4f2aa7725320de13c3940a33ac69f43c8bc80aa537c2460d" {
		t.Fatalf("0060 up migration changed after release: sha256=%s", got)
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
	if got := fmt.Sprintf("%x", sha256.Sum256(downBytes)); got !=
		"56d86e70c49b6fdfdabbc9044dabb160d3c6e3137534ceb2900313e9698cf71e" {
		t.Fatalf("0060 down migration changed after release: sha256=%s", got)
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

	hardeningBytes, err := migrationsFS.ReadFile(
		"migrations/0061_adaptive_typed_ledger_hardening.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(hardeningBytes)); got !=
		"99e840b29e88b2cb3f6d3cd2875e97b598a14f8695c942de78808197250016c9" {
		t.Fatalf("0061 up migration changed after release: sha256=%s", got)
	}
	hardening := string(hardeningBytes)
	for _, want := range []string{
		"CREATE OR REPLACE FUNCTION aura.adaptive_schema2_assignment_payload_valid",
		"CREATE OR REPLACE FUNCTION aura.adaptive_schema2_delivery_payload_valid",
		"CREATE OR REPLACE FUNCTION aura.enforce_adaptive_schema2_delivery_assignment",
		"payload->>'schema_version' IS NOT DISTINCT FROM '2.0'",
	} {
		if !strings.Contains(hardening, want) {
			t.Errorf("0061 up migration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"DELETE FROM aura.adaptive_outbox",
		"UPDATE aura.adaptive_outbox",
	} {
		if strings.Contains(hardening, forbidden) {
			t.Errorf("0061 up migration mutates immutable history via %q", forbidden)
		}
	}

	hardeningDownBytes, err := migrationsFS.ReadFile(
		"migrations/0061_adaptive_typed_ledger_hardening.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(hardeningDownBytes)); got !=
		"a29969ba25ebf91f3a8e83217f841c00d37698d84a3375f3d791ffa8efd4bc35" {
		t.Fatalf("0061 down migration changed after release: sha256=%s", got)
	}

	auditBytes, err := migrationsFS.ReadFile(
		"migrations/0062_adaptive_typed_ledger_audit.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	audit := string(auditBytes)
	for _, want := range []string{
		"adaptive_schema2_assignment_payload_valid",
		"adaptive_schema2_delivery_payload_valid",
		"IS NOT TRUE",
		"assignment.owner_id = delivery.owner_id",
		"assignment.aggregate_id = delivery.aggregate_id",
		"assignment.decision_id = delivery.decision_id",
		"delivery.payload->>'actual_action_id' = 'none'",
		"assignment.payload->'eligible_actions'",
		"assignment.payload->'action_probabilities'",
		"ERRCODE = '23514'",
	} {
		if !strings.Contains(audit, want) {
			t.Errorf("0062 up migration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"DELETE FROM aura.adaptive_outbox",
		"UPDATE aura.adaptive_outbox",
		"INSERT INTO aura.adaptive_outbox",
		"ALTER TABLE aura.adaptive_outbox",
	} {
		if strings.Contains(audit, forbidden) {
			t.Errorf("0062 up migration mutates immutable history via %q", forbidden)
		}
	}

	auditDownBytes, err := migrationsFS.ReadFile(
		"migrations/0062_adaptive_typed_ledger_audit.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	auditDown := string(auditDownBytes)
	if !strings.Contains(auditDown, "successful up changed no schema or ledger data") ||
		!strings.Contains(auditDown, "NULL;") {
		t.Error("0062 down migration must document and implement an explicit no-op")
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
		"event_kind IN ('decision', 'delivery')",
		"FOR UPDATE",
	} {
		if !strings.Contains(queries, want) {
			t.Errorf("adaptive query source missing %q", want)
		}
	}
}
