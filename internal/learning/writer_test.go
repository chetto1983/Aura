package learning_test

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/learning"
	"github.com/aura/aura/internal/storage/memoryindex"
)

func openTestMemStore(t *testing.T) *memoryindex.Store {
	t.Helper()
	db := openTestDB(t)
	store, err := memoryindex.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestWriteApprovedLesson_Approve(t *testing.T) {
	store := openTestMemStore(t)
	ctx := context.Background()

	p := learning.Proposal{
		Kind:          "operational_memory",
		Fact:          "Tool `web_fetch` repeated `io` failures (5 times in last 7 days). Reason pattern: timeout. Sample attempts: [atm_a atm_b].",
		SignatureHash: "abc123def456",
		ToolName:      "web_fetch",
		ErrorClass:    "io",
	}
	if err := learning.WriteApprovedLesson(ctx, store, p); err != nil {
		t.Fatalf("WriteApprovedLesson: %v", err)
	}

	docs, err := store.Search(ctx, "operational:abc123def456", memoryindex.Filter{
		Kinds: []string{memoryindex.KindOperational},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("expected 1 document, got none")
	}
	var found bool
	for _, d := range docs {
		if d.Handle == "operational:abc123def456" {
			found = true
			if d.Kind != memoryindex.KindOperational {
				t.Errorf("kind: got %q, want %q", d.Kind, memoryindex.KindOperational)
			}
		}
	}
	if !found {
		t.Error("document with Handle='operational:abc123def456' not found in search results")
	}
}

func TestWriteApprovedLesson_DoubleApprove(t *testing.T) {
	store := openTestMemStore(t)
	ctx := context.Background()

	p := learning.Proposal{
		Kind:          "operational_memory",
		Fact:          "Tool `web_fetch` repeated `io` failures.",
		SignatureHash: "aabbccddee11",
		ToolName:      "web_fetch",
		ErrorClass:    "io",
	}
	for i := 0; i < 2; i++ {
		if err := learning.WriteApprovedLesson(ctx, store, p); err != nil {
			t.Fatalf("WriteApprovedLesson run %d: %v", i+1, err)
		}
	}

	docs, err := store.Search(ctx, "operational:aabbccddee11", memoryindex.Filter{
		Kinds: []string{memoryindex.KindOperational},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var count int
	for _, d := range docs {
		if d.Handle == "operational:aabbccddee11" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("double-approve: expected exactly 1 row, got %d", count)
	}
}

func TestWriteApprovedLesson_WrongKind(t *testing.T) {
	store := openTestMemStore(t)
	ctx := context.Background()

	p := learning.Proposal{
		Kind:          "wiki",
		Fact:          "Some wiki fact.",
		SignatureHash: "noop00001234",
	}
	if err := learning.WriteApprovedLesson(ctx, store, p); err != nil {
		t.Fatalf("WriteApprovedLesson wrong-kind: %v", err)
	}

	docs, err := store.Search(ctx, "noop00001234", memoryindex.Filter{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, d := range docs {
		if d.Handle == "operational:noop00001234" {
			t.Error("wrong-kind: unexpected operational row created for non-operational proposal")
		}
	}
}
