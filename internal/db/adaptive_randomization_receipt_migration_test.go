package db

import (
	"strings"
	"testing"
)

func TestAdaptiveRandomizationReceiptMigrationSourceContract(t *testing.T) {
	upBytes, err := migrationsFS.ReadFile(
		"migrations/0075_adaptive_randomization_receipts.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, want := range []string{
		"CREATE TABLE aura.adaptive_randomization_receipts",
		"adaptive_randomization_receipts_claim_fk",
		"adaptive_randomization_receipts_assignment_uidx",
		"adaptive_randomization_receipts_claim_uidx",
		"sha256 = pg_catalog.sha256(artifact)",
		"adaptive_randomization_receipt_artifact_valid",
		"IS JSON OBJECT WITH UNIQUE KEYS",
		"aura.adaptive.randomization-receipt/v1",
		"aura/go-crypto-rand-bit",
		"go-crypto-rand/os-csprng",
		"enforce_adaptive_randomization_receipt_binding",
		"SECURITY DEFINER",
		"reject_adaptive_randomization_receipt_mutation",
		"BEFORE UPDATE OR DELETE",
		"ENABLE ROW LEVEL SECURITY",
		"adaptive_randomization_receipts_owner_isolation",
		"GRANT SELECT, INSERT ON aura.adaptive_randomization_receipts TO aura_app",
		"adaptive_schema2_randomized_assignment_payload_valid",
		"adaptive_focal_cohort_v2_artifact_valid",
		"aura.adaptive.cohort/v2",
		"randomization_plan_artifact",
		"adaptive_child_artifact_ref_valid",
		"SET search_path = pg_catalog",
		"REVOKE ALL ON FUNCTION aura.enforce_adaptive_randomization_receipt_binding() FROM PUBLIC",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("0075 up migration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"GRANT UPDATE ON aura.adaptive_randomization_receipts",
		"GRANT DELETE ON aura.adaptive_randomization_receipts",
		"GRANT TRUNCATE ON aura.adaptive_randomization_receipts",
		"GRANT REFERENCES ON aura.adaptive_randomization_receipts",
		"GRANT TRIGGER ON aura.adaptive_randomization_receipts",
	} {
		if strings.Contains(up, forbidden) {
			t.Errorf("0075 up migration violates least privilege via %q", forbidden)
		}
	}

	downBytes, err := migrationsFS.ReadFile(
		"migrations/0075_adaptive_randomization_receipts.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	down := string(downBytes)
	for _, want := range []string{
		"DROP TABLE IF EXISTS aura.adaptive_randomization_receipts",
		"DROP FUNCTION IF EXISTS aura.adaptive_randomization_receipt_artifact_valid",
		"DROP FUNCTION IF EXISTS aura.adaptive_schema2_randomized_assignment_payload_valid",
		"DROP INDEX IF EXISTS aura.adaptive_focal_claims_receipt_scope_uidx",
		"DROP COLUMN IF EXISTS interference_cluster_id",
		"DROP COLUMN IF EXISTS analysis_stratum_schema_sha256",
		"DROP COLUMN IF EXISTS randomization_plan_artifact",
		"DROP FUNCTION IF EXISTS aura.adaptive_focal_cohort_v2_artifact_valid",
	} {
		if !strings.Contains(down, want) {
			t.Errorf("0075 down migration missing %q", want)
		}
	}
}
