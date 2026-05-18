package memoryindex

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestOperationalLessonsBlockNilAndEmpty(t *testing.T) {
	if got := OperationalLessonsBlock(nil, 0); got != "" {
		t.Errorf("nil docs: want empty, got %q", got)
	}
	if got := OperationalLessonsBlock([]Document{}, 0); got != "" {
		t.Errorf("empty docs: want empty, got %q", got)
	}
}

func TestOperationalLessonsBlockAllBlank(t *testing.T) {
	docs := []Document{{ID: "x", Kind: KindOperational, Title: "   ", Body: "  "}}
	if got := OperationalLessonsBlock(docs, 0); got != "" {
		t.Errorf("all-blank doc: want empty, got %q", got)
	}
}

func TestFetchRecentOperationalZeroLessons(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	docs, err := store.FetchRecentOperational(ctx, 10)
	if err != nil {
		t.Fatalf("FetchRecentOperational: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("want 0 docs, got %d", len(docs))
	}
	block := OperationalLessonsBlock(docs, 0)
	if block != "" {
		t.Errorf("want empty block for 0 lessons, got %q", block)
	}
}

func TestFetchRecentOperationalTop10From11(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)

	// Seed 11 operational lessons with distinct timestamps so ORDER BY is deterministic.
	for i := 0; i < 11; i++ {
		doc := Document{
			ID:        fmt.Sprintf("op:test:%02d", i),
			Kind:      KindOperational,
			Title:     fmt.Sprintf("Lesson %02d", i),
			Body:      fmt.Sprintf("Body for lesson %02d.", i),
			UpdatedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := store.Upsert(ctx, doc); err != nil {
			t.Fatalf("Upsert lesson %d: %v", i, err)
		}
	}

	// Also seed a non-operational doc to confirm kind filter works.
	if err := store.Upsert(ctx, Document{
		ID:    "src:noise",
		Kind:  KindSource,
		Title: "Noise",
		Body:  "Should not appear.",
	}); err != nil {
		t.Fatalf("Upsert noise: %v", err)
	}

	docs, err := store.FetchRecentOperational(ctx, 10)
	if err != nil {
		t.Fatalf("FetchRecentOperational: %v", err)
	}
	if len(docs) != 10 {
		t.Errorf("want 10 docs, got %d", len(docs))
	}
	// Most-recent first: lesson 10 should be first, lesson 01 last (lesson 00 dropped).
	if docs[0].ID != "op:test:10" {
		t.Errorf("first doc: want op:test:10, got %s", docs[0].ID)
	}
	if docs[9].ID != "op:test:01" {
		t.Errorf("last doc: want op:test:01, got %s", docs[9].ID)
	}

	block := OperationalLessonsBlock(docs, 0)
	if !strings.Contains(block, "## Operational Lessons") {
		t.Error("block missing header")
	}
	count := strings.Count(block, "### Lesson")
	if count != 10 {
		t.Errorf("want 10 lesson headings, got %d", count)
	}
	if strings.Contains(block, "Lesson 00") {
		t.Error("oldest lesson (00) must not appear in top-10 block")
	}
	if strings.Contains(block, "Noise") {
		t.Error("non-operational doc must not appear in block")
	}
}

func TestOperationalLessonsBlockBytesCap(t *testing.T) {
	docs := []Document{
		{ID: "a", Kind: KindOperational, Title: "Short", Body: "Short body."},
		{ID: "b", Kind: KindOperational, Title: "Long title that fills many bytes", Body: strings.Repeat("x", 5000)},
	}
	// Cap at a small size that fits only the first lesson.
	block := OperationalLessonsBlock(docs, 200)
	if strings.Contains(block, strings.Repeat("x", 100)) {
		t.Error("byte-capped block should have truncated the long lesson")
	}
	if !strings.Contains(block, "Short") {
		t.Error("first (short) lesson should still appear")
	}
}
