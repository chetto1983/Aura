package db

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestAdaptiveFocalCohortMigrationSourceContract(t *testing.T) {
	for _, migration := range []struct {
		name string
		hash string
	}{
		{"0068_adaptive_policy_snapshots.up.sql", "ab582ecbf145ff897ce5a12bcb33826274e1fa518e56b75607c6b668a68f255d"},
		{"0068_adaptive_policy_snapshots.down.sql", "97f544852e4363b0850a25120294ebb0643b9368e15357a2d4a3224a4fb15193"},
		{"0069_adaptive_policy_snapshot_vector_parity.up.sql", "9aa9409ca47e62d88a208e7a46456c2ff46a3921ed8c15cc57717422a19c1521"},
		{"0069_adaptive_policy_snapshot_vector_parity.down.sql", "043abc68faad5fe057a2ccc72eee5977d7f9dd5903e51a9cd865c4a50d1079ef"},
	} {
		body, err := migrationsFS.ReadFile("migrations/" + migration.name)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != migration.hash {
			t.Fatalf("%s changed after release: sha256=%s", migration.name, got)
		}
	}

	upBytes, err := migrationsFS.ReadFile("migrations/0071_adaptive_focal_cohorts.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, want := range []string{
		"CREATE TABLE aura.adaptive_focal_cohorts",
		"CREATE TABLE aura.adaptive_focal_cohort_claims",
		"artifact           bytea NOT NULL",
		"artifact_json      jsonb NOT NULL",
		"sha256 = pg_catalog.sha256(artifact)",
		"adaptive_uuid_v5_sha256(",
		"37e07806-1464-47f3-8d89-f922fe7a1392",
		"adaptive_focal_cohort_artifact_valid",
		"adaptive_focal_cohort_catalogs_valid",
		"adaptive_focal_cohort_metrics_valid",
		"adaptive_focal_cohort_id_valid",
		"IS JSON OBJECT WITH UNIQUE KEYS",
		"numeric_value > 9223372036854775807",
		"NEW.created_at >= NEW.cutoff",
		"CHECK (created_at < cutoff)",
		"UNIQUE (cohort_id, request_id)",
		"UNIQUE (cohort_id, evaluation_conversation_id)",
		"UNIQUE (cohort_id, assignment_id)",
		"adaptive_schema2_assignment_id(",
		"FOREIGN KEY (cohort_id, owner_id, domain, decision_point, point_ordinal)",
		"REFERENCES aura.adaptive_policy_snapshots (owner_id, id, sha256, domain, decision_point, provider_id, model_id, policy_version)",
		"time_block_start           timestamptz NOT NULL",
		"enforce_adaptive_focal_claim_cutoff_and_blocks",
		"ENABLE ROW LEVEL SECURITY",
		"GRANT SELECT, INSERT ON aura.adaptive_focal_cohorts TO aura_app",
		"GRANT SELECT, INSERT ON aura.adaptive_focal_cohort_claims TO aura_app",
		"REVOKE ALL ON FUNCTION aura.adaptive_focal_cohort_artifact_valid",
		"SET search_path = pg_catalog",
		"CREATE TRIGGER adaptive_focal_cohorts_immutable",
		"CREATE TRIGGER adaptive_focal_cohort_claims_immutable",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("0071 up migration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"SECURITY DEFINER",
		"GRANT UPDATE",
		"GRANT DELETE",
		"GRANT TRUNCATE",
		"GRANT REFERENCES",
		"GRANT TRIGGER",
	} {
		if strings.Contains(up, forbidden) {
			t.Errorf("0071 up migration violates least privilege via %q", forbidden)
		}
	}

	downBytes, err := migrationsFS.ReadFile("migrations/0071_adaptive_focal_cohorts.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := string(downBytes)
	for _, want := range []string{
		"DROP TABLE IF EXISTS aura.adaptive_focal_cohort_claims",
		"DROP TABLE IF EXISTS aura.adaptive_focal_cohorts",
		"DROP FUNCTION IF EXISTS aura.adaptive_focal_cohort_artifact_valid",
		"DROP FUNCTION IF EXISTS aura.adaptive_focal_cohort_catalogs_valid",
		"DROP FUNCTION IF EXISTS aura.adaptive_focal_cohort_metrics_valid",
		"DROP FUNCTION IF EXISTS aura.adaptive_focal_cohort_id_valid",
	} {
		if !strings.Contains(down, want) {
			t.Errorf("0071 down migration missing %q", want)
		}
	}
	if strings.Contains(down, "DROP TABLE IF EXISTS aura.adaptive_policy_snapshots") {
		t.Error("0071 down must not drop prior snapshot history")
	}
}
