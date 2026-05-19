package agent

import (
	"context"
	"testing"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

func TestDecayCycleDeletesOnlyStaleNormalLessons(t *testing.T) {
	ctx := context.Background()
	store := openPostHookStore(t)
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	for _, doc := range []memoryindex.Document{
		{ID: "operational:stale", Kind: memoryindex.KindOperational, Title: "Lesson: a", Body: "stale", Handle: "operational:stale", Status: "active", Priority: memoryindex.PriorityNormal, UpdatedAt: now.AddDate(0, 0, -8)},
		{ID: "operational:high", Kind: memoryindex.KindOperational, Title: "Lesson: b", Body: "high", Handle: "operational:high", Status: "active", Priority: memoryindex.PriorityHigh, UpdatedAt: now.AddDate(0, 0, -80)},
	} {
		if err := store.Upsert(ctx, doc); err != nil {
			t.Fatalf("Upsert %s: %v", doc.ID, err)
		}
	}
	summary, err := DecayCycle(ctx, store, now, nil)
	if err != nil {
		t.Fatalf("DecayCycle: %v", err)
	}
	if summary.Scanned != 1 || summary.Deleted != 1 || summary.Kept != 0 {
		t.Fatalf("summary = %+v, want scanned=1 deleted=1 kept=0", summary)
	}
	docs, err := store.FetchRecentOperational(ctx, 10)
	if err != nil {
		t.Fatalf("FetchRecentOperational: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "operational:high" {
		t.Fatalf("remaining docs = %+v, want only high", docs)
	}
}
