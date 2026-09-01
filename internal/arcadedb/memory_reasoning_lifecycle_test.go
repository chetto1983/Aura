package arcadedb

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReasoningRetentionWorkerBoundedExpiry(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[{"trace_id":"trace-expired"}]}`)
	deleted, err := client.DeleteExpiredReasoning(
		context.Background(), "identity-a", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 3,
	)
	if err != nil {
		t.Fatalf("DeleteExpiredReasoning: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	joined := rec.joined()
	for _, required := range []string{
		"identity_id = :identity_id", "expires_at <= :now", "LIMIT :limit",
		"DELETE FROM TOUCHED", "DELETE FROM INVOKED", "DELETE FROM NEXT",
		"DELETE FROM HAS_STEP", "DELETE FROM INITIATED_BY",
		"DELETE FROM ReasoningToolCall", "DELETE FROM ReasoningStep", "DELETE FROM ReasoningTrace",
	} {
		if !strings.Contains(joined, required) {
			t.Errorf("bounded expiry SQL missing %q:\n%s", required, joined)
		}
	}
	for _, params := range rec.params {
		if identity, ok := params["identity_id"]; ok && identity != "identity-a" {
			t.Fatalf("expiry crossed identity: %#v", params)
		}
	}
}
