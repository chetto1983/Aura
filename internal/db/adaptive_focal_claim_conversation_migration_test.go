package db

import (
	"strings"
	"testing"
)

func TestAdaptiveFocalClaimConversationMigrationSourceContract(t *testing.T) {
	upBytes, err := migrationsFS.ReadFile(
		"migrations/0072_adaptive_focal_claim_conversation.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, want := range []string{
		"CREATE FUNCTION aura.enforce_adaptive_focal_claim_conversation()",
		"FROM aura.conversations",
		"id = NEW.evaluation_conversation_id",
		"identity_id = NEW.owner_id",
		"ERRCODE = '23503'",
		"CREATE TRIGGER adaptive_focal_cohort_claims_conversation",
		"BEFORE INSERT ON aura.adaptive_focal_cohort_claims",
		"SET search_path = pg_catalog",
		"REVOKE ALL ON FUNCTION aura.enforce_adaptive_focal_claim_conversation() FROM PUBLIC",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("0072 up migration missing %q", want)
		}
	}
	if strings.Contains(up, "SECURITY DEFINER") {
		t.Error("0072 up migration must not use SECURITY DEFINER")
	}

	downBytes, err := migrationsFS.ReadFile(
		"migrations/0072_adaptive_focal_claim_conversation.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	down := string(downBytes)
	for _, want := range []string{
		"DROP TRIGGER IF EXISTS adaptive_focal_cohort_claims_conversation",
		"DROP FUNCTION IF EXISTS aura.enforce_adaptive_focal_claim_conversation()",
	} {
		if !strings.Contains(down, want) {
			t.Errorf("0072 down migration missing %q", want)
		}
	}
}
