package assets

import (
	"context"
	"testing"
)

// An empty list must leave the context untouched rather than store an empty slice: the
// column is meant to stay NULL for a turn that carried nothing, and a stored empty slice
// records "we looked and there were none" as if it were a measurement.
func TestWithTurnAttachmentsStoresNothingForAnEmptyList(t *testing.T) {
	base := context.Background()
	for _, ids := range [][]string{nil, {}} {
		ctx := WithTurnAttachments(base, ids)
		if ctx != base {
			t.Fatalf("WithTurnAttachments(%#v) wrapped the context", ids)
		}
		if got := TurnAttachments(ctx); got != nil {
			t.Fatalf("TurnAttachments = %#v, want nil", got)
		}
	}
}

// The stored ids are a copy. The caller's slice is request-scoped and is reused by the
// loop that built it, so holding the caller's backing array would let a later append
// rewrite what a committed turn says it was sent with.
func TestWithTurnAttachmentsCopiesTheCallersSlice(t *testing.T) {
	ids := []string{"asset-1", "asset-2"}
	ctx := WithTurnAttachments(context.Background(), ids)
	ids[0] = "rewritten"

	got := TurnAttachments(ctx)
	if len(got) != 2 || got[0] != "asset-1" || got[1] != "asset-2" {
		t.Fatalf("TurnAttachments = %#v, want the ids as they were when recorded", got)
	}
}

// nil is the honest answer for every caller that does not attach through the HTTP path:
// a scheduled delivery, a Telegram turn, a delegation.
func TestTurnAttachmentsIsNilWhenNothingWasRecorded(t *testing.T) {
	if got := TurnAttachments(context.Background()); got != nil {
		t.Fatalf("TurnAttachments on a bare context = %#v, want nil", got)
	}
}
