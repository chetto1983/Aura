package skills

import (
	"strings"
	"testing"
)

// TestResumeUnknownAction asserts the resume handler rejects an unknown action
// (defensive guard; the caller validated the answer shape upstream).
func TestResumeUnknownAction(t *testing.T) {
	h := NewResumeHandler(nil) // no writer is touched on the unknown-action path
	err := h.Resume(t.Context(), "frobnicate", "some-skill", "", AuditActor{})
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("want an unknown-action error, got %v", err)
	}
}
