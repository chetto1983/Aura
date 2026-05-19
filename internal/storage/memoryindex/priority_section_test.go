package memoryindex

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakePriorityStore struct {
	docs      []Document
	markedIDs []string
}

func (f *fakePriorityStore) FetchPinnedOperational(context.Context, int) ([]Document, error) {
	return append([]Document(nil), f.docs...), nil
}

func (f *fakePriorityStore) MarkOperationalRecalled(_ context.Context, ids []string, _ time.Time) error {
	f.markedIDs = append(f.markedIDs, ids...)
	return nil
}

func TestRenderPrioritySectionEmpty(t *testing.T) {
	if got := RenderPrioritySection(context.Background(), &fakePriorityStore{}); got != "" {
		t.Fatalf("RenderPrioritySection empty = %q, want empty", got)
	}
}

func TestRenderPrioritySectionSingleCritical(t *testing.T) {
	store := &fakePriorityStore{docs: []Document{{
		ID:       "op:critical",
		Kind:     KindOperational,
		Title:    "Use shorter query",
		Body:     "When web_fetch times out, retry with a smaller page or query.",
		Priority: PriorityCritical,
	}}}
	got := RenderPrioritySection(context.Background(), store)
	if !strings.Contains(got, "## Pinned Operational Lessons") {
		t.Fatalf("missing header:\n%s", got)
	}
	if !strings.Contains(got, "[critical] Use shorter query") {
		t.Fatalf("missing critical row:\n%s", got)
	}
	if len(store.markedIDs) != 1 || store.markedIDs[0] != "op:critical" {
		t.Fatalf("markedIDs = %v, want op:critical", store.markedIDs)
	}
}

func TestRenderPrioritySectionSortsCriticalBeforeHighAndRecentWithinTier(t *testing.T) {
	base := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	store := &fakePriorityStore{docs: []Document{
		{ID: "high-new", Title: "high new", Body: "body", Priority: PriorityHigh, UpdatedAt: base.Add(2 * time.Minute)},
		{ID: "critical-old", Title: "critical old", Body: "body", Priority: PriorityCritical, UpdatedAt: base},
		{ID: "critical-new", Title: "critical new", Body: "body", Priority: PriorityCritical, UpdatedAt: base.Add(time.Minute)},
	}}
	got := RenderPrioritySection(context.Background(), store)
	cn := strings.Index(got, "critical new")
	co := strings.Index(got, "critical old")
	hn := strings.Index(got, "high new")
	if !(cn >= 0 && co > cn && hn > co) {
		t.Fatalf("unexpected order:\n%s", got)
	}
}

func TestRenderPrioritySectionCapsRulesAtThirty(t *testing.T) {
	var docs []Document
	for i := 0; i < 35; i++ {
		docs = append(docs, Document{
			ID:        fmt.Sprintf("op:%02d", i),
			Title:     fmt.Sprintf("Rule %02d", i),
			Body:      "body",
			Priority:  PriorityHigh,
			UpdatedAt: time.Date(2026, 5, 19, 0, 0, i, 0, time.UTC),
		})
	}
	got := RenderPrioritySection(context.Background(), &fakePriorityStore{docs: docs})
	if count := strings.Count(got, "- ["); count != PrioritySectionMaxRules {
		t.Fatalf("rendered rules = %d, want %d", count, PrioritySectionMaxRules)
	}
}

func TestRenderPrioritySectionTruncatesRuleBody(t *testing.T) {
	body := strings.Repeat("x", PrioritySectionRuleChars+50)
	got := RenderPrioritySection(context.Background(), &fakePriorityStore{docs: []Document{{
		ID:       "op:long",
		Title:    "Long",
		Body:     body,
		Priority: PriorityHigh,
	}}})
	if strings.Contains(got, strings.Repeat("x", PrioritySectionRuleChars+1)) {
		t.Fatalf("body was not truncated:\n%s", got)
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("truncated body missing ellipsis:\n%s", got)
	}
}

func TestRenderPrioritySectionTotalByteCap(t *testing.T) {
	var docs []Document
	for i := 0; i < 30; i++ {
		docs = append(docs, Document{
			ID:       fmt.Sprintf("op:%02d", i),
			Title:    fmt.Sprintf("Rule %02d", i),
			Body:     strings.Repeat("y", PrioritySectionRuleChars),
			Priority: PriorityHigh,
		})
	}
	got := RenderPrioritySection(context.Background(), &fakePriorityStore{docs: docs})
	if len(got) > PrioritySectionMaxBytes {
		t.Fatalf("rendered bytes = %d, want <= %d", len(got), PrioritySectionMaxBytes)
	}
}

func TestPrioritySectionCacheBatchesRefreshAndInvalidates(t *testing.T) {
	ctx := context.Background()
	cache := &PrioritySectionCache{}
	store := &fakePriorityStore{docs: []Document{{
		ID:       "op:a",
		Title:    "A",
		Body:     "body",
		Priority: PriorityHigh,
	}}}

	first := cache.Render(ctx, store, 10)
	store.docs = []Document{{
		ID:       "op:b",
		Title:    "B",
		Body:     "body",
		Priority: PriorityHigh,
	}}
	if got := cache.Render(ctx, store, 14); got != first {
		t.Fatalf("cache refreshed too early:\nfirst=%s\ngot=%s", first, got)
	}
	if got := cache.Render(ctx, store, 15); !strings.Contains(got, "B") {
		t.Fatalf("cache did not refresh after 5 turns:\n%s", got)
	}
	store.docs = []Document{{
		ID:       "op:c",
		Title:    "C",
		Body:     "body",
		Priority: PriorityCritical,
	}}
	cache.InvalidatePinSection()
	if got := cache.Render(ctx, store, 16); !strings.Contains(got, "C") {
		t.Fatalf("cache did not refresh after invalidation:\n%s", got)
	}
}
