package memoryindex

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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

func TestStoreMergesVectorResultsAndFallsBackWhenVectorFails(t *testing.T) {
	ctx := context.Background()
	vector := &fakeVectorIndex{searchDocs: []Document{{
		ID:        "archive:7",
		Kind:      KindArchive,
		Title:     "chat=10 turn=7",
		Body:      "Vector-only deadline evidence.",
		Handle:    "conversation:7",
		ChatID:    10,
		UpdatedAt: time.Now().UTC(),
		Score:     0.82,
	}}}
	store := openTestStoreWithVector(t, vector)
	if err := store.Upsert(ctx, Document{
		ID:        "source:src_abc",
		Kind:      KindSource,
		Title:     "local",
		Body:      "Local FTS deadline evidence.",
		Handle:    "source:src_abc",
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	hits, err := store.Search(ctx, "deadline", Filter{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %#v", hits)
	}
	vector.searchErr = errors.New("qdrant down")
	hits, err = store.Search(ctx, "deadline", Filter{Limit: 5})
	if err != nil {
		t.Fatalf("Search fallback: %v", err)
	}
	if len(hits) != 1 || hits[0].Kind != KindSource {
		t.Fatalf("fallback hits = %#v", hits)
	}
}

func TestStoreMirrorsUpsertAndSyncVector(t *testing.T) {
	ctx := context.Background()
	vector := &fakeVectorIndex{}
	store := openTestStoreWithVector(t, vector)
	if err := store.Upsert(ctx, Document{
		ID:        "source:src_abc",
		Kind:      KindSource,
		Title:     "local",
		Body:      "Local compact evidence.",
		Handle:    "source:src_abc",
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if len(vector.upserts) != 1 || vector.upserts[0][0].ID != "source:src_abc" {
		t.Fatalf("vector upserts = %#v", vector.upserts)
	}
	report, err := store.SyncVector(ctx)
	if err != nil {
		t.Fatalf("SyncVector: %v", err)
	}
	if report.DocsIndexed != 1 || len(vector.recreates) != 1 || vector.recreates[0][0].ID != "source:src_abc" {
		t.Fatalf("report=%+v recreates=%#v", report, vector.recreates)
	}
}

func TestStorePurgesArchiveRowsAndVectorPoints(t *testing.T) {
	ctx := context.Background()
	vector := &fakeVectorIndex{}
	store := openTestStoreWithVector(t, vector)
	old := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	docs := []Document{
		{ID: "archive:old-chat-10", Kind: KindArchive, Body: "old private memory", ChatID: 10, Handle: "conversation:1", UpdatedAt: old},
		{ID: "archive:new-chat-10", Kind: KindArchive, Body: "new private memory", ChatID: 10, Handle: "conversation:2", UpdatedAt: newer},
		{ID: "archive:old-chat-20", Kind: KindArchive, Body: "other chat memory", ChatID: 20, Handle: "conversation:3", UpdatedAt: old},
		{ID: "source:kept", Kind: KindSource, Body: "source memory kept", Handle: "source:kept", UpdatedAt: old},
	}
	for _, doc := range docs {
		if err := store.Upsert(ctx, doc); err != nil {
			t.Fatalf("Upsert %s: %v", doc.ID, err)
		}
	}
	vector.deletes = nil
	if err := store.PurgeArchiveByChat(ctx, 10); err != nil {
		t.Fatalf("PurgeArchiveByChat: %v", err)
	}
	hits, err := store.Search(ctx, "memory", Filter{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if hasDoc(hits, "archive:old-chat-10") || hasDoc(hits, "archive:new-chat-10") {
		t.Fatalf("chat 10 rows still present: %#v", hits)
	}
	if !hasDoc(hits, "archive:old-chat-20") || !hasDoc(hits, "source:kept") {
		t.Fatalf("unrelated rows missing: %#v", hits)
	}
	if len(vector.deletes) != 1 || !sameStringSet(vector.deletes[0], []string{"archive:old-chat-10", "archive:new-chat-10"}) {
		t.Fatalf("vector deletes = %#v", vector.deletes)
	}
}

func TestStorePurgeArchiveFailsBeforeLocalDeleteWhenVectorDeleteFails(t *testing.T) {
	ctx := context.Background()
	vector := &fakeVectorIndex{deleteErr: errors.New("qdrant down")}
	store := openTestStoreWithVector(t, vector)
	if err := store.Upsert(ctx, Document{
		ID:        "archive:kept",
		Kind:      KindArchive,
		Body:      "private memory should remain when vector purge fails",
		ChatID:    10,
		Handle:    "conversation:1",
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	err := store.PurgeArchiveByChat(ctx, 10)
	if err == nil || !strings.Contains(err.Error(), "vector delete") {
		t.Fatalf("PurgeArchiveByChat err = %v, want vector delete error", err)
	}
	hits, searchErr := store.Search(ctx, "private memory", Filter{Kinds: []string{KindArchive}, ChatID: 10, Limit: 5})
	if searchErr != nil {
		t.Fatalf("Search: %v", searchErr)
	}
	if len(hits) != 1 || hits[0].ID != "archive:kept" {
		t.Fatalf("hits = %#v", hits)
	}
}

func openTestStoreWithVector(t *testing.T, vector VectorIndex) *Store {
	t.Helper()
	db, err := auradb.Open(filepath.Join(t.TempDir(), "aura.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("Run migrations: %v", err)
	}
	store, err := NewStoreWithVector(db, vector, nil)
	if err != nil {
		t.Fatalf("NewStoreWithVector: %v", err)
	}
	return store
}

type fakeVectorIndex struct {
	upserts    [][]Document
	recreates  [][]Document
	deletes    [][]string
	searchDocs []Document
	searchErr  error
	deleteErr  error
}

func (f *fakeVectorIndex) Upsert(_ context.Context, docs []Document) error {
	f.upserts = append(f.upserts, append([]Document(nil), docs...))
	return nil
}

func (f *fakeVectorIndex) Recreate(_ context.Context, docs []Document) (VectorReport, error) {
	f.recreates = append(f.recreates, append([]Document(nil), docs...))
	return VectorReport{Collection: "test_compact", DocsIndexed: len(docs), VectorSize: 5}, nil
}

func (f *fakeVectorIndex) Search(context.Context, string, Filter) ([]Document, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return append([]Document(nil), f.searchDocs...), nil
}

func (f *fakeVectorIndex) Delete(_ context.Context, ids []string) error {
	f.deletes = append(f.deletes, append([]string(nil), ids...))
	return f.deleteErr
}

func hasDoc(docs []Document, id string) bool {
	for _, doc := range docs {
		if doc.ID == id {
			return true
		}
	}
	return false
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, value := range got {
		seen[value]++
	}
	for _, value := range want {
		if seen[value] == 0 {
			return false
		}
		seen[value]--
	}
	return true
}
