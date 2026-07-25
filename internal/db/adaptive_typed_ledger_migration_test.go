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
	if got := fmt.Sprintf("%x", sha256.Sum256(auditBytes)); got !=
		"122cb2aa09e68d331bd78fc552ce8213021f9885f8c54aa65216d86b488dc8ce" {
		t.Fatalf("0062 up migration changed after release: sha256=%s", got)
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
	if got := fmt.Sprintf("%x", sha256.Sum256(auditDownBytes)); got !=
		"0fc870c7b23dd9fc8ad1ed72db02290b6e8dff3f05e55ff75d8ccebdc5e71192" {
		t.Fatalf("0062 down migration changed after release: sha256=%s", got)
	}
	auditDown := string(auditDownBytes)
	if !strings.Contains(auditDown, "successful up changed no schema or ledger data") ||
		!strings.Contains(auditDown, "NULL;") {
		t.Error("0062 down migration must document and implement an explicit no-op")
	}

	identityBytes, err := migrationsFS.ReadFile(
		"migrations/0063_adaptive_deterministic_identity.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(identityBytes)); got !=
		"2a75a3952b977bc53a5d51d5bc00fb5ac206a3ed8a12bea2b4d7e6aa49f53655" {
		t.Fatalf("0063 up migration changed after release: sha256=%s", got)
	}
	identity := string(identityBytes)
	for _, want := range []string{
		"CREATE FUNCTION aura.adaptive_uuid_v5_sha256",
		"CREATE FUNCTION aura.adaptive_schema2_assignment_id",
		"CREATE FUNCTION aura.adaptive_schema2_event_id",
		"CREATE FUNCTION aura.adaptive_schema2_assignment_row_valid",
		"CREATE FUNCTION aura.adaptive_schema2_delivery_row_valid",
		"sha256(uuid_send(p_namespace) || p_identity)",
		"c5370396-c73f-4a44-ae7b-112f070523ae",
		"fb3f7ce9-d343-41fb-a26f-35155b229189",
		"schema-2 identity audit found an invalid fact",
		"adaptive_outbox_schema2_assignment_identity_check",
		"adaptive_outbox_schema2_assignment_tuple_uidx",
		"CREATE OR REPLACE FUNCTION aura.enforce_adaptive_schema2_delivery_assignment",
	} {
		if !strings.Contains(identity, want) {
			t.Errorf("0063 up migration missing %q", want)
		}
	}
	auditAt := strings.Index(identity, "DO $$")
	constraintAt := strings.Index(
		identity,
		"ADD CONSTRAINT adaptive_outbox_schema2_assignment_identity_check",
	)
	indexAt := strings.Index(
		identity,
		"CREATE UNIQUE INDEX adaptive_outbox_schema2_assignment_tuple_uidx",
	)
	triggerAt := strings.LastIndex(
		identity,
		"CREATE OR REPLACE FUNCTION aura.enforce_adaptive_schema2_delivery_assignment",
	)
	if auditAt < 0 || constraintAt <= auditAt || indexAt <= auditAt ||
		triggerAt <= auditAt {
		t.Error("0063 must audit existing facts before enabling future enforcement")
	}
	for _, forbidden := range []string{
		"DELETE FROM aura.adaptive_outbox",
		"UPDATE aura.adaptive_outbox",
		"INSERT INTO aura.adaptive_outbox",
	} {
		if strings.Contains(identity, forbidden) {
			t.Errorf("0063 up migration mutates immutable history via %q", forbidden)
		}
	}

	identityDownBytes, err := migrationsFS.ReadFile(
		"migrations/0063_adaptive_deterministic_identity.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(identityDownBytes)); got !=
		"49f6304723b71cd55a229783f32ddf706698c28b990a549038ccc2ab3785a81b" {
		t.Fatalf("0063 down migration changed after release: sha256=%s", got)
	}
	identityDown := string(identityDownBytes)
	for _, want := range []string{
		"DROP INDEX IF EXISTS aura.adaptive_outbox_schema2_assignment_tuple_uidx",
		"DROP CONSTRAINT IF EXISTS adaptive_outbox_schema2_assignment_identity_check",
		"DROP FUNCTION IF EXISTS aura.adaptive_schema2_delivery_row_valid",
		"DROP FUNCTION IF EXISTS aura.adaptive_schema2_assignment_row_valid",
		"DROP FUNCTION IF EXISTS aura.adaptive_schema2_event_id",
		"DROP FUNCTION IF EXISTS aura.adaptive_schema2_assignment_id",
		"DROP FUNCTION IF EXISTS aura.adaptive_uuid_v5_sha256",
	} {
		if !strings.Contains(identityDown, want) {
			t.Errorf("0063 down migration missing %q", want)
		}
	}

	lockedAuditBytes, err := migrationsFS.ReadFile(
		"migrations/0064_adaptive_locked_identity_audit.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	lockedAudit := string(lockedAuditBytes)
	for _, want := range []string{
		"LOCK TABLE aura.adaptive_outbox IN SHARE MODE;",
		"aura.adaptive_schema2_assignment_row_valid(",
		"aura.adaptive_schema2_delivery_row_valid(",
		"IS NOT TRUE",
		"ERRCODE = '23514'",
		"schema-2 locked identity audit found an invalid fact",
	} {
		if !strings.Contains(lockedAudit, want) {
			t.Errorf("0064 up migration missing %q", want)
		}
	}
	lockAt := strings.Index(
		lockedAudit,
		"LOCK TABLE aura.adaptive_outbox IN SHARE MODE;",
	)
	lockedAuditAt := strings.Index(lockedAudit, "DO $$")
	if lockAt < 0 || lockedAuditAt <= lockAt {
		t.Error("0064 must lock adaptive_outbox before taking the audit snapshot")
	}
	for _, forbidden := range []string{
		"DELETE FROM aura.adaptive_outbox",
		"UPDATE aura.adaptive_outbox",
		"INSERT INTO aura.adaptive_outbox",
		"ALTER TABLE aura.adaptive_outbox",
		"CREATE INDEX",
		"DROP INDEX",
	} {
		if strings.Contains(lockedAudit, forbidden) {
			t.Errorf("0064 up migration changes ledger or schema via %q", forbidden)
		}
	}

	lockedAuditDownBytes, err := migrationsFS.ReadFile(
		"migrations/0064_adaptive_locked_identity_audit.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	lockedAuditDown := string(lockedAuditDownBytes)
	for _, want := range []string{
		"transaction-scoped table lock",
		"changed no schema or rows",
		"0063 enforcement",
		"NULL;",
	} {
		if !strings.Contains(lockedAuditDown, want) {
			t.Errorf("0064 down migration missing no-op rationale %q", want)
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
		"event_kind IN ('decision', 'delivery')",
		"AS MATERIALIZED",
		"adaptive_schema2_assignment_payload_valid",
		"adaptive_schema2_delivery_payload_valid",
		"sha256(",
		"assignment.payload->'eligible_actions'",
		"assignment.payload->'action_probabilities'",
		"FOR UPDATE OF assignment",
	} {
		if !strings.Contains(queries, want) {
			t.Errorf("adaptive query source missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"adaptive_schema2_assignment_row_valid",
		"adaptive_schema2_delivery_row_valid",
		"adaptive_schema2_assignment_id",
		"adaptive_schema2_event_id",
	} {
		if strings.Contains(queries, forbidden) {
			t.Errorf(
				"adaptive query source cannot depend on post-0062 helper %q",
				forbidden,
			)
		}
	}
}
