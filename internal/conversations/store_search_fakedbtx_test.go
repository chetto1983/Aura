// Unit tier (no build tag): SearchConversationTurns status-filter + dedup, Delete
// teardown (cleaner cascade and os.RemoveAll fallback), and the insertContextRotEvent
// emitter, driven through the in-memory sqlc.DBTX fake (split from
// store_fakedbtx_test.go to keep each file under the 600-LOC cap). Shares the fake
// harness + row-builder helpers defined in store_fakedbtx_test.go.
package conversations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// searchRowValues returns the 4 positional Scan values SearchConversationTurns
// expects (SearchConversationTurnsRow field order).
func searchRowValues(conv pgtype.UUID, seq int32, content string, sim float32) []any {
	return []any{conv, seq, pgtype.Text{String: content, Valid: true}, sim}
}

// TestSearchConversationTurns_FakeStatusFilter proves the status-cache dedup (one Get
// per conversation) and that hits from deleted conversations are dropped.
//
// The fake serves the SAME scripted row-set on every Query and the SAME row on every
// QueryRow; that is sufficient because all hits here share one conversation id, so
// the status lookup happens exactly once and applies to every row.
func TestSearchConversationTurns_FakeStatusFilter(t *testing.T) {
	t.Parallel()

	t.Run("active conversation keeps hits", func(t *testing.T) {
		t.Parallel()
		_, conv := mustUUID(t)
		_, ident := mustUUID(t)
		fake := &fakeStatusDBTX{
			searchRows: [][]any{
				searchRowValues(conv, 1, "alpha", 0.9),
				searchRowValues(conv, 2, "beta", 0.8),
			},
			convRow: conversationRowValues(conv, ident, StatusActive, "m", true, "t"),
		}
		s := &Store{q: sqlc.New(fake), runDir: t.TempDir(), turnCapBytes: 65536}
		out, err := s.SearchConversationTurns(context.Background(), "query", 5)
		if err != nil {
			t.Fatalf("SearchConversationTurns: %v", err)
		}
		if len(out) != 2 || out[0].Content != "alpha" || out[1].Seq != 2 {
			t.Errorf("active hits projection wrong: %+v", out)
		}
		if fake.getCalls != 1 {
			t.Errorf("status must be looked up ONCE per conversation, got %d", fake.getCalls)
		}
	})

	t.Run("deleted conversation drops hits", func(t *testing.T) {
		t.Parallel()
		_, conv := mustUUID(t)
		_, ident := mustUUID(t)
		fake := &fakeStatusDBTX{
			searchRows: [][]any{searchRowValues(conv, 1, "secret", 0.95)},
			convRow:    conversationRowValues(conv, ident, StatusDeleted, "m", true, "t"),
		}
		s := &Store{q: sqlc.New(fake), runDir: t.TempDir(), turnCapBytes: 65536}
		out, err := s.SearchConversationTurns(context.Background(), "query", 5)
		if err != nil {
			t.Fatalf("SearchConversationTurns: %v", err)
		}
		if len(out) != 0 {
			t.Errorf("deleted conversation hits must be dropped: %+v", out)
		}
	})
}

// TestSearchConversationTurns_FakeStatusLookupError propagates a Get failure during
// the status enrichment.
func TestSearchConversationTurns_FakeStatusLookupError(t *testing.T) {
	t.Parallel()
	_, conv := mustUUID(t)
	fake := &fakeStatusDBTX{
		searchRows:  [][]any{searchRowValues(conv, 1, "x", 0.5)},
		convScanErr: errors.New("status load failed"),
	}
	s := &Store{q: sqlc.New(fake), runDir: t.TempDir(), turnCapBytes: 65536}
	if _, err := s.SearchConversationTurns(context.Background(), "q", 5); err == nil {
		t.Error("SearchConversationTurns(status lookup error): want error")
	}
}

// TestSearchConversationTurns_FakeQueryError wraps the FTS query failure.
func TestSearchConversationTurns_FakeQueryError(t *testing.T) {
	t.Parallel()
	boom := errors.New("fts query failed")
	s := fakeStore(t, &fakeDBTX{queryErr: boom})
	if _, err := s.SearchConversationTurns(context.Background(), "q", 5); !errors.Is(err, boom) {
		t.Errorf("search query error must propagate: %v", err)
	}
}

// fakeStatusDBTX serves a scripted search result-set on Query and a scripted
// conversation row on QueryRow (the SearchConversationTurns status enrichment calls
// Get, which uses QueryRow). It counts QueryRow calls so the dedup cache assertion
// can verify exactly one status lookup per conversation.
type fakeStatusDBTX struct {
	searchRows  [][]any
	convRow     []any
	convScanErr error
	getCalls    int
}

func (f *fakeStatusDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeStatusDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &fakeRows{rows: f.searchRows}, nil
}

func (f *fakeStatusDBTX) QueryRow(context.Context, string, ...any) pgx.Row {
	f.getCalls++
	if f.convScanErr != nil {
		return &fakeRow{scanErr: f.convScanErr}
	}
	return &fakeRow{values: f.convRow}
}

// TestDelete_FakeNoCleanerRemovesSidecarDir proves Delete tears down the
// per-conversation sidecar dir with the os.RemoveAll fallback when no Cleaner is set.
func TestDelete_FakeNoCleanerRemovesSidecarDir(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7()).String()
	runDir := t.TempDir()
	convDir := filepath.Join(runDir, "conversations", id)
	if err := os.MkdirAll(convDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	s := &Store{q: sqlc.New(&fakeDBTX{}), runDir: runDir, turnCapBytes: 65536}
	if err := s.Delete(context.Background(), id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(convDir); !os.IsNotExist(err) {
		t.Errorf("sidecar dir must be removed, stat err=%v", err)
	}
}

// TestDelete_FakeDBError wraps the DELETE failure (the dir teardown never runs).
func TestDelete_FakeDBError(t *testing.T) {
	t.Parallel()
	boom := errors.New("delete failed")
	s := &Store{q: sqlc.New(&fakeDBTX{execErr: boom}), runDir: t.TempDir(), turnCapBytes: 65536}
	if err := s.Delete(context.Background(), uuid.Must(uuid.NewV7()).String()); !errors.Is(err, boom) {
		t.Errorf("Delete DB error must propagate: %v", err)
	}
}

// TestDelete_FakeWithCleaner proves the Cleaner cascade is invoked instead of
// os.RemoveAll, and that a cleaner failure is reported (not a rolled-back delete).
func TestDelete_FakeWithCleaner(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7()).String()

	ok := &recordingCleaner{}
	s := &Store{q: sqlc.New(&fakeDBTX{}), runDir: t.TempDir(), turnCapBytes: 65536, cleaner: ok}
	if err := s.Delete(context.Background(), id); err != nil {
		t.Fatalf("Delete(cleaner): %v", err)
	}
	if ok.purged != id {
		t.Errorf("cleaner must be invoked with the conversation id, got %q", ok.purged)
	}

	boom := errors.New("purge failed")
	bad := &Store{q: sqlc.New(&fakeDBTX{}), runDir: t.TempDir(), turnCapBytes: 65536, cleaner: &recordingCleaner{err: boom}}
	if err := bad.Delete(context.Background(), id); !errors.Is(err, boom) {
		t.Errorf("cleaner failure must be reported: %v", err)
	}
}

// TestDelete_FakeBadUUID confirms the parseUUID guard rejects a non-UUID id before
// any DELETE or cleaner cascade.
func TestDelete_FakeBadUUID(t *testing.T) {
	t.Parallel()
	s := &Store{q: sqlc.New(&fakeDBTX{}), runDir: t.TempDir(), turnCapBytes: 65536, cleaner: &recordingCleaner{}}
	if err := s.Delete(context.Background(), "not-a-uuid"); err == nil {
		t.Error("Delete(bad uuid): want error")
	}
}

type recordingCleaner struct {
	purged string
	err    error
}

func (r *recordingCleaner) PurgeConversationDir(convID string) error {
	r.purged = convID
	return r.err
}

// TestInsertContextRotEvent_FakeSuccess covers the Store rot-event emitter success
// path (the bad-UUID branch is covered in store_unit_test.go).
func TestInsertContextRotEvent_FakeSuccess(t *testing.T) {
	t.Parallel()
	s := fakeStore(t, &fakeDBTX{})
	if err := s.insertContextRotEvent(context.Background(), uuid.Must(uuid.NewV7()).String(), 2, 100, 40); err != nil {
		t.Errorf("insertContextRotEvent: %v", err)
	}

	boom := errors.New("rot insert failed")
	sErr := fakeStore(t, &fakeDBTX{execErr: boom})
	if err := sErr.insertContextRotEvent(context.Background(), uuid.Must(uuid.NewV7()).String(), 1, 10, 5); !errors.Is(err, boom) {
		t.Errorf("insertContextRotEvent DB error must propagate: %v", err)
	}
}
