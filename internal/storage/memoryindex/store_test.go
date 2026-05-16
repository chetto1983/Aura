package memoryindex

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

func TestStoreRebuildFTSRestoresRowsFromCanonicalDocuments(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if err := store.Upsert(ctx, Document{
		ID:        "source:needle",
		Kind:      KindSource,
		Title:     "needle",
		Body:      "needle compact memory evidence",
		Handle:    "source:needle",
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `DROP TABLE compact_memory_fts`); err != nil {
		t.Fatalf("drop fts: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, compactMemoryFTSCreateSQL); err != nil {
		t.Fatalf("create empty fts: %v", err)
	}
	if hits, err := store.Search(ctx, "compact", Filter{Limit: 5}); err != nil {
		t.Fatalf("Search empty fts: %v", err)
	} else if len(hits) != 0 {
		t.Fatalf("hits before rebuild = %#v, want none", hits)
	}
	if err := store.RebuildFTS(ctx); err != nil {
		t.Fatalf("RebuildFTS: %v", err)
	}
	hits, err := store.Search(ctx, "compact", Filter{Limit: 5})
	if err != nil {
		t.Fatalf("Search rebuilt fts: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "source:needle" {
		t.Fatalf("hits after rebuild = %#v", hits)
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

// TestStorePurgeSourceConcurrentDoesNotRaceSQLiteBusy locks the single-writer
// contract on deleteWhere. Without serialization a fan-out of N parallel
// PurgeSource callers races the SQLite write lock and one or more eventually
// fails with SQLITE_BUSY when the per-connection busy_timeout is exhausted —
// the exact failure mode from the 2026-05-14 07:51 conversation where 27 of 44
// LLM-emitted delete_source calls failed concurrently. With Store.writeMu the
// fan-out serializes inside the package and every caller succeeds.
func TestStorePurgeSourceConcurrentDoesNotRaceSQLiteBusy(t *testing.T) {
	ctx := context.Background()
	vector := &fakeVectorIndex{}
	store := openTestStoreWithVector(t, vector)

	const n = 50
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("src_concurrent_%02d", i)
		doc := Document{
			ID:        "source:" + ids[i] + "#page=1",
			Kind:      KindSource,
			Title:     ids[i],
			Body:      "body " + ids[i],
			Handle:    "source:" + ids[i] + "#page=1",
			SourceID:  ids[i],
			Page:      1,
			UpdatedAt: time.Now().UTC(),
		}
		if err := store.Upsert(ctx, doc); err != nil {
			t.Fatalf("Upsert %s: %v", doc.ID, err)
		}
	}

	var wg sync.WaitGroup
	var failures atomic.Int64
	errCh := make(chan error, n)
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := store.PurgeSource(ctx, id); err != nil {
				failures.Add(1)
				errCh <- fmt.Errorf("%s: %w", id, err)
			}
		}(id)
	}
	wg.Wait()
	close(errCh)

	if failures.Load() != 0 {
		var sample []string
		for err := range errCh {
			sample = append(sample, err.Error())
			if len(sample) >= 5 {
				break
			}
		}
		t.Fatalf("%d/%d PurgeSource calls failed under concurrency (first %d): %v",
			failures.Load(), n, len(sample), sample)
	}

	hits, err := store.Search(ctx, "body", Filter{Kinds: []string{KindSource}, Limit: n})
	if err != nil {
		t.Fatalf("Search after purge: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("residual source docs after concurrent purge: %d", len(hits))
	}
	if len(vector.deletes) != n {
		t.Fatalf("vector deletes = %d, want %d (one per PurgeSource)", len(vector.deletes), n)
	}
}

// TestStorePurgeSourceRespectsContextCancellation verifies the cancellable
// mutex acquisition: a queued caller whose context fires before its turn
// returns ctx.Err() instead of blocking forever.
func TestStorePurgeSourceRespectsContextCancellation(t *testing.T) {
	store := openTestStoreWithVector(t, &fakeVectorIndex{})
	// Manually hold the lock so the next caller has to queue.
	store.writeMu.Lock()
	defer store.writeMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // expire before the call so it can't acquire
	err := store.PurgeSource(ctx, "src_anything")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PurgeSource err = %v, want context.Canceled", err)
	}
}

func TestStoreSourceIDFilter(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Now().UTC()

	// 2 sources × 3 pages each = 6 source docs with shared body keyword "shared"
	for _, pair := range []struct{ srcID, page, extra string }{
		{"src_alpha", "1", "unique alpha content"},
		{"src_alpha", "2", "unique alpha content"},
		{"src_alpha", "3", "unique alpha content"},
		{"src_beta", "1", "unique beta content"},
		{"src_beta", "2", "unique beta content"},
		{"src_beta", "3", "unique beta content"},
	} {
		id := "source:" + pair.srcID + "#page=" + pair.page
		if err := store.Upsert(ctx, Document{
			ID:        id,
			Kind:      KindSource,
			Title:     pair.srcID,
			Body:      "shared document content " + pair.extra,
			Handle:    id,
			SourceID:  pair.srcID,
			UpdatedAt: now,
		}); err != nil {
			t.Fatalf("Upsert %s: %v", id, err)
		}
	}

	// Filter by src_alpha: must return exactly 3 docs, all from src_alpha
	alphaHits, err := store.Search(ctx, "shared document content", Filter{SourceID: "src_alpha", Limit: 10})
	if err != nil {
		t.Fatalf("Search SourceID=src_alpha: %v", err)
	}
	if len(alphaHits) != 3 {
		t.Fatalf("expected 3 hits for src_alpha, got %d: %#v", len(alphaHits), alphaHits)
	}
	for _, h := range alphaHits {
		if h.SourceID != "src_alpha" {
			t.Errorf("hit SourceID = %q, want src_alpha", h.SourceID)
		}
	}

	// Empty SourceID is a no-op: should return all 6
	allHits, err := store.Search(ctx, "shared document content", Filter{Limit: 10})
	if err != nil {
		t.Fatalf("Search empty SourceID: %v", err)
	}
	if len(allHits) != 6 {
		t.Fatalf("expected 6 hits with no SourceID filter, got %d: %#v", len(allHits), allHits)
	}
}

func TestDocumentRoundTripWithFreshnessColumns(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC)

	doc := Document{
		ID:               "source:src_fresh#page=1",
		Kind:             KindSource,
		Title:            "fresh.pdf",
		Body:             "Document with freshness metadata.",
		Handle:           "source:src_fresh#page=1",
		SourceID:         "src_fresh",
		Page:             1,
		ContentHash:      ContentHash("user", "Document with freshness metadata.", "embeddinggemma-300m"),
		EmbeddingModelID: "embeddinggemma-300m",
		IndexBuildID:     "build-id-abc123",
		UpdatedAt:        now,
	}

	if err := store.Upsert(ctx, doc); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	hits, err := store.Search(ctx, "fresh", Filter{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	var got *Document
	for i := range hits {
		if hits[i].ID == doc.ID {
			got = &hits[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("document %q not found in search results: %#v", doc.ID, hits)
	}
	if got.ContentHash != doc.ContentHash {
		t.Errorf("ContentHash: got %q, want %q", got.ContentHash, doc.ContentHash)
	}
	if got.EmbeddingModelID != doc.EmbeddingModelID {
		t.Errorf("EmbeddingModelID: got %q, want %q", got.EmbeddingModelID, doc.EmbeddingModelID)
	}
	if got.IndexBuildID != doc.IndexBuildID {
		t.Errorf("IndexBuildID: got %q, want %q", got.IndexBuildID, doc.IndexBuildID)
	}
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
