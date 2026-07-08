package identityctx

import (
	"context"
	"testing"
)

// TestWithIdentityID_RoundTrip proves an identity id set on a context reads back, and an empty
// id is a no-op (the caller stays unscoped — no sentinel empty value planted on the context).
func TestWithIdentityID_RoundTrip(t *testing.T) {
	t.Parallel()

	base := context.Background()
	if got := IdentityID(base); got != "" {
		t.Fatalf("IdentityID(bare ctx) = %q, want empty", got)
	}

	ctx := WithIdentityID(base, "id-42")
	if got := IdentityID(ctx); got != "id-42" {
		t.Fatalf("IdentityID = %q, want id-42", got)
	}

	// An empty id leaves the context UNCHANGED (same context value, still unscoped).
	if got := WithIdentityID(base, ""); got != base {
		t.Fatal("WithIdentityID(ctx, \"\") must return the original context unchanged")
	}
	if got := IdentityID(WithIdentityID(base, "")); got != "" {
		t.Fatalf("IdentityID after empty set = %q, want empty", got)
	}
}
