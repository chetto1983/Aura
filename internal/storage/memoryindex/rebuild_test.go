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
