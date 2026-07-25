package db

import (
	"strings"
	"testing"
)

func TestAdaptiveFocalClaimGlobalConversationMigrationSourceContract(
	t *testing.T,
) {
	upBytes, err := migrationsFS.ReadFile(
		"migrations/0073_adaptive_focal_claim_global_conversation.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, want := range []string{
		"CREATE UNIQUE INDEX adaptive_focal_cohort_claims_owner_conversation_uidx",
		"ON aura.adaptive_focal_cohort_claims",
		"(owner_id, evaluation_conversation_id)",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("0073 up migration missing %q", want)
		}
	}

	downBytes, err := migrationsFS.ReadFile(
		"migrations/0073_adaptive_focal_claim_global_conversation.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(
		string(downBytes),
		"DROP INDEX IF EXISTS aura.adaptive_focal_cohort_claims_owner_conversation_uidx",
	) {
		t.Error("0073 down migration does not remove owner/conversation uniqueness")
	}
}
