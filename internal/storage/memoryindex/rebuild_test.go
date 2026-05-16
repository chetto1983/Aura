package memoryindex

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/conversation"
	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
)

func TestIndexingTurnAppenderMirrorsPersistedArchiveID(t *testing.T) {
	ctx := context.Background()
	db := openTestDBForMemoryIndex(t)
	archive, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	index, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	appender := NewIndexingTurnAppender(archive, index)

	if err := appender.Append(ctx, conversation.Turn{
		ChatID:    42,
		UserID:    7,
		TurnIndex: 3,
		Role:      "user",
		Content:   "Persisted archive ids should mirror into compact memory.",
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	hits, err := index.Search(ctx, "persisted archive ids", Filter{Kinds: []string{KindArchive}, ChatID: 42, Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %#v", hits)
	}
	if hits[0].ConversationID == 0 || !strings.HasPrefix(hits[0].ID, "archive:") || !strings.HasPrefix(hits[0].Handle, "conversation:") {
		t.Fatalf("non-canonical archive doc = %#v", hits[0])
	}
	if strings.Contains(hits[0].Handle, "chat:") {
		t.Fatalf("archive doc kept fallback handle: %#v", hits[0])
	}
}

func TestIndexingArchiveRepositoryPurgesCompactArchiveRows(t *testing.T) {
	ctx := context.Background()
	db := openTestDBForMemoryIndex(t)
	archive, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	index, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	repo := NewIndexingArchiveRepository(archive, index)
	for _, turn := range []conversation.Turn{
		{ChatID: 42, UserID: 7, TurnIndex: 1, Role: "user", Content: "Aura should purge this private archive row."},
		{ChatID: 99, UserID: 8, TurnIndex: 1, Role: "user", Content: "Aura should keep this other private archive row."},
	} {
		if err := repo.Append(ctx, turn); err != nil {
			t.Fatalf("Append chat %d: %v", turn.ChatID, err)
		}
	}
	deleted, err := repo.DeleteByChat(ctx, 42)
	if err != nil {
		t.Fatalf("DeleteByChat: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	hits, err := index.Search(ctx, "private archive row", Filter{Kinds: []string{KindArchive}, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ChatID != 99 {
		t.Fatalf("hits = %#v", hits)
	}
}

func TestArchiveEligibility(t *testing.T) {
	cases := []struct {
		role    string
		content string
		want    bool
		reason  string
	}{
		{"tool", "tool_schemas.json dump", false, "role_tool_excluded"},
		{"system", "system prompt text", false, "role_system_excluded"},
		{"user", "user message here", true, ""},
		{"assistant", "assistant reply here", true, ""},
	}
	for _, tc := range cases {
		got, reason := ArchiveEligibility(tc.role, tc.content)
		if got != tc.want || reason != tc.reason {
			t.Errorf("ArchiveEligibility(%q, ...) = (%v, %q), want (%v, %q)",
				tc.role, got, reason, tc.want, tc.reason)
		}
	}
}

func TestParityAppendAndRebuildProduceSameCompactSet(t *testing.T) {
	ctx := context.Background()
	db := openTestDBForMemoryIndex(t)
	archive, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	index, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	appender := NewIndexingTurnAppender(archive, index)

	// 10-row fixture: 3 user, 3 assistant, 2 tool, 2 system (6 eligible)
	fixture := []conversation.Turn{
		{ChatID: 1, UserID: 1, TurnIndex: 1, Role: "user", Content: "user message one"},
		{ChatID: 1, UserID: 1, TurnIndex: 2, Role: "assistant", Content: "assistant reply one"},
		{ChatID: 1, UserID: 1, TurnIndex: 3, Role: "tool", Content: "tool_schemas.json dump"},
		{ChatID: 1, UserID: 1, TurnIndex: 4, Role: "user", Content: "user message two"},
		{ChatID: 1, UserID: 1, TurnIndex: 5, Role: "assistant", Content: "assistant reply two"},
		{ChatID: 1, UserID: 1, TurnIndex: 6, Role: "tool", Content: "stdout snippet from code"},
		{ChatID: 1, UserID: 1, TurnIndex: 7, Role: "user", Content: "user message three"},
		{ChatID: 1, UserID: 1, TurnIndex: 8, Role: "assistant", Content: "assistant reply three"},
		{ChatID: 1, UserID: 1, TurnIndex: 9, Role: "system", Content: "system prompt text"},
		{ChatID: 1, UserID: 1, TurnIndex: 10, Role: "system", Content: "another system message"},
	}
	for _, turn := range fixture {
		if err := appender.Append(ctx, turn); err != nil {
			t.Fatalf("Append turn_index=%d: %v", turn.TurnIndex, err)
		}
	}

	appendIDs := queryArchiveCompactIDs(t, db)
	if len(appendIDs) != 6 {
		t.Fatalf("Append path: want 6 compact docs (3 user + 3 assistant), got %d: %v", len(appendIDs), appendIDs)
	}

	// Full rebuild using the same turns from the archive (via ListAll)
	if _, err := Rebuild(ctx, index, RebuildInput{Archive: archive, SkipVector: true}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	rebuildIDs := queryArchiveCompactIDs(t, db)
	if len(rebuildIDs) != len(appendIDs) {
		t.Fatalf("Rebuild path: want %d compact docs, got %d: %v", len(appendIDs), len(rebuildIDs), rebuildIDs)
	}
	for i := range appendIDs {
		if appendIDs[i] != rebuildIDs[i] {
			t.Fatalf("compact set diverged at index %d: Append=%q, Rebuild=%q", i, appendIDs[i], rebuildIDs[i])
		}
	}
}

func queryArchiveCompactIDs(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT id FROM compact_memory_documents WHERE kind = ? ORDER BY id`, KindArchive)
	if err != nil {
		t.Fatalf("query compact archive IDs: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// TestArchiveTurns_ToolRowPersistsInConversationsButNotCompact is the Phase07A dual gate:
// tool rows MUST survive in the conversations archive for debugging/replay,
// while compact_memory_documents MUST stay free of raw tool output.
// This mirrors the 2026-05-15 live incident where tool_schemas.json dumps polluted memory search.
func TestArchiveTurns_ToolRowPersistsInConversationsButNotCompact(t *testing.T) {
	ctx := context.Background()
	db := openTestDBForMemoryIndex(t)
	archive, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	index, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	appender := NewIndexingTurnAppender(archive, index)

	// Mirrors the 2026-05-15 incident: tool row contains a raw tool_schemas.json dump.
	if err := appender.Append(ctx, conversation.Turn{
		ChatID:     77,
		UserID:     1,
		TurnIndex:  0,
		Role:       "tool",
		Content:    `tool_schemas.json dump: {"search_memory": {"description": "search compact memory"}}`,
		ToolCallID: "call_incident_2026_05_15",
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Gate 1: conversations table preserves the tool row (lossless archive for debug/replay).
	listed, err := archive.ListByChat(ctx, 77, 10)
	if err != nil {
		t.Fatalf("ListByChat: %v", err)
	}
	if len(listed) != 1 || listed[0].Role != "tool" {
		t.Fatalf("conversations: want 1 tool row, got %+v", listed)
	}

	// Gate 2: compact_memory_documents does NOT receive the tool row (Phase07A invariant).
	ids := queryArchiveCompactIDs(t, db)
	if len(ids) != 0 {
		t.Fatalf("compact_memory_documents: want 0 rows for tool turn, got %d: %v", len(ids), ids)
	}
}

func openTestDBForMemoryIndex(t *testing.T) *sql.DB {
	t.Helper()
	db, err := auradb.Open(filepath.Join(t.TempDir(), "aura.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("Run migrations: %v", err)
	}
	return db
}
