package db

import (
	"strings"
	"testing"
)

func TestAdaptiveSealedEvidenceMigrationSourceContract(t *testing.T) {
	t.Parallel()

	upBytes, err := migrationsFS.ReadFile(
		"migrations/0076_adaptive_sealed_evidence.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, want := range []string{
		"CREATE TABLE aura.adaptive_evidence_artifacts",
		"CREATE TABLE aura.adaptive_sealed_evidence",
		"kind IN ('canary_admission', 'canary_outcome')",
		"adaptive_sealed_evidence_admission_uidx",
		"adaptive_sealed_evidence_outcome_look_uidx",
		"CREATE FUNCTION aura.seal_adaptive_evidence",
		"CREATE FUNCTION aura.apply_adaptive_policy_transition",
		"SET search_path = pg_catalog",
		"DROP FUNCTION IF EXISTS aura.apply_adaptive_policy_transition(",
		"REVOKE ALL ON FUNCTION aura.seal_adaptive_evidence",
		"REVOKE ALL ON FUNCTION aura.apply_adaptive_policy_transition",
		"ENABLE ROW LEVEL SECURITY",
		"reject_adaptive_sealed_evidence_mutation",
		"adaptive.evidence.seal",
		"adaptive.manage",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("0076 up migration missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"GRANT INSERT ON aura.adaptive_sealed_evidence TO aura_app",
		"GRANT UPDATE ON aura.adaptive_sealed_evidence TO aura_app",
		"GRANT DELETE ON aura.adaptive_sealed_evidence TO aura_app",
		"GRANT INSERT ON aura.adaptive_evidence_artifacts TO aura_app",
	} {
		if strings.Contains(up, forbidden) {
			t.Errorf("0076 grants caller-built evidence via %q", forbidden)
		}
	}

	downBytes, err := migrationsFS.ReadFile(
		"migrations/0076_adaptive_sealed_evidence.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	down := string(downBytes)
	for _, want := range []string{
		"DROP TABLE IF EXISTS aura.adaptive_sealed_evidence",
		"DROP TABLE IF EXISTS aura.adaptive_evidence_artifacts",
		"CREATE FUNCTION aura.apply_adaptive_policy_transition(",
	} {
		if !strings.Contains(down, want) {
			t.Errorf("0076 down migration missing %q", want)
		}
	}
}
