package memoryindex

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := auradb.Open(filepath.Join(t.TempDir(), "aura.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("Run migrations: %v", err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestStoreSearchesCompactMemoryWithScopeAndChatFilter(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	docs := []Document{
		{
			ID:        "source:src_abc#page=2",
			Kind:      KindSource,
			Title:     "agreement.pdf",
			Body:      "The cancellation clause requires thirty days notice.",
			Handle:    "source:src_abc#page=2",
			SourceID:  "src_abc",
			Page:      2,
			UpdatedAt: now,
		},
		{
			ID:        "archive:1",
			Kind:      KindArchive,
			Title:     "chat=10 turn=1",
			Body:      "Private trip plan for Berlin includes museum bookings.",
			Handle:    "conversation:1",
			ChatID:    10,
			UpdatedAt: now,
		},
		{
			ID:        "archive:2",
			Kind:      KindArchive,
			Title:     "chat=20 turn=1",
			Body:      "Private trip plan for Rome includes train tickets.",
			Handle:    "conversation:2",
			ChatID:    20,
			UpdatedAt: now,
		},
	}
	for _, doc := range docs {
		if err := store.Upsert(ctx, doc); err != nil {
			t.Fatalf("Upsert %s: %v", doc.ID, err)
		}
	}

	sourceHits, err := store.Search(ctx, "cancellation clause", Filter{Kinds: []string{KindSource}, Limit: 5})
	if err != nil {
		t.Fatalf("Search source: %v", err)
	}
	if len(sourceHits) != 1 || sourceHits[0].SourceID != "src_abc" || sourceHits[0].Page != 2 {
		t.Fatalf("source hits = %#v", sourceHits)
	}

	archiveHits, err := store.Search(ctx, "private trip", Filter{Kinds: []string{KindArchive}, ChatID: 10, Limit: 5})
	if err != nil {
		t.Fatalf("Search archive: %v", err)
	}
	if len(archiveHits) != 1 || archiveHits[0].ChatID != 10 {
		t.Fatalf("archive hits = %#v", archiveHits)
	}
}

func TestStoreReplaceKindDeletesStaleRows(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if err := store.Upsert(ctx, Document{
		ID:        "source:old",
		Kind:      KindSource,
		Title:     "old",
		Body:      "stale compact memory row",
		Handle:    "source:old",
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Upsert stale: %v", err)
	}
	if err := store.ReplaceKind(ctx, KindSource, []Document{{
		ID:        "source:new",
		Title:     "new",
		Body:      "fresh compact memory row",
		Handle:    "source:new",
		UpdatedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("ReplaceKind: %v", err)
	}
	hits, err := store.Search(ctx, "fresh", Filter{Kinds: []string{KindSource}, Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "source:new" {
		t.Fatalf("hits = %#v", hits)
	}
}
