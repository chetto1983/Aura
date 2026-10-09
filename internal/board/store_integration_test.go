//go:build db_integration

// Live-Postgres test for the work board store: what only the engine can prove. A card of
// another identity is invisible, not forbidden; a connection with no principal sees nothing;
// moves keep the order and renumber a crowded column; a column with cards cannot be removed;
// links to a deleted conversation fall away; the board follows its identity out.
//
//	go test -tags db_integration -run TestBoard ./internal/board/
package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
)

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("board integration requires %s under CI — a skipped tier must not pass as green", key)
		}
		t.Skipf("board integration requires %s", key)
	}
	return v
}

func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := envOrSkip(t, "AURA_DB_URL")
	host, port := os.Getenv("PGHOST"), os.Getenv("PGPORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5432"
	}
	if err := db.EnsureRoles(ctx, fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, id, "board-test-"+id[:8]); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, id) })
	return id
}

func labels(cards []Card, column string) []string {
	var out []string
	for _, c := range cards {
		if c.ColumnID == column {
			out = append(out, c.Label)
		}
	}
	return out
}

func TestBoardFirstUseAndCardLifecycle(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	id := seedIdentity(t, pool)
	ctx := context.Background()

	b, err := s.Board(ctx, id)
	if err != nil || len(b.Columns) != 3 || b.Columns[1].CardLimit != 5 {
		t.Fatalf("first use = %+v, %v; want the three default columns", b, err)
	}
	again, _ := s.Board(ctx, id)
	if again.ID != b.ID {
		t.Fatal("a second call created a second board")
	}

	due := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	a, err := s.AddCard(ctx, id, NewCard{Label: "call the supplier", Tags: []string{"ops"}, DueAt: &due}, SourceChat, ActorAgent)
	if err != nil {
		t.Fatalf("AddCard: %v", err)
	}
	if a.ColumnID != "todo" || a.Priority != 2 || a.Source != SourceChat || a.DueAt == nil || !a.DueAt.Equal(due) {
		t.Fatalf("added = %+v", a)
	}
	bb, _ := s.AddCard(ctx, id, NewCard{Label: "pay the invoice", Priority: 3}, SourceCockpit, ActorOperator)
	c, _ := s.AddCard(ctx, id, NewCard{Column: "doing", Label: "draft the report"}, SourceBackground, ActorAgent)
	if _, err := s.AddCard(ctx, id, NewCard{Column: "later", Label: "x"}, SourceCockpit, ActorOperator); !errors.Is(err, ErrUnknownColumn) || !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown column = %v, want ErrUnknownColumn, an ErrInvalid", err)
	}

	label, desc := "call the supplier about the pump", "ask for the quote"
	updated, err := s.UpdateCard(ctx, id, a.ID, CardPatch{Label: &label, Description: &desc}, ActorOperator)
	if err != nil || updated.Label != label || updated.UpdatedBy != ActorOperator || updated.Source != SourceChat {
		t.Fatalf("UpdateCard = %+v, %v", updated, err)
	}
	var noDue *time.Time
	cleared, _ := s.UpdateCard(ctx, id, a.ID, CardPatch{DueAt: &noDue}, ActorAgent)
	if cleared.DueAt != nil {
		t.Fatal("a nil due date must clear it")
	}

	if _, err := s.MoveCard(ctx, id, bb.ID, "todo", a.ID, ActorOperator); err != nil {
		t.Fatalf("MoveCard to the top: %v", err)
	}
	cards, _ := s.Cards(ctx, id)
	if got := labels(cards, "todo"); len(got) != 2 || got[0] != "pay the invoice" {
		t.Fatalf("todo after move = %v", got)
	}
	if _, err := s.MoveCard(ctx, id, c.ID, "done", "", ActorAgent); err != nil {
		t.Fatalf("MoveCard across columns: %v", err)
	}
	cards, _ = s.Cards(ctx, id)
	if got := labels(cards, "done"); len(got) != 1 || got[0] != "draft the report" {
		t.Fatalf("done after move = %v", got)
	}

	found, err := s.Search(ctx, id, "pump", 32)
	if err != nil || len(found) != 1 || found[0].ID != a.ID {
		t.Fatalf("Search pump = %+v, %v", found, err)
	}
	if found, _ := s.Search(ctx, id, "ops", 32); len(found) != 1 {
		t.Fatalf("Search by tag = %+v", found)
	}

	if removed, err := s.DeleteCard(ctx, id, bb.ID); err != nil || !removed {
		t.Fatalf("DeleteCard = %v, %v", removed, err)
	}
	if removed, _ := s.DeleteCard(ctx, id, bb.ID); removed {
		t.Fatal("a second delete reported a removal")
	}
	if _, err := s.Card(ctx, id, bb.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted card = %v, want ErrNotFound", err)
	}
}

// Many moves to the same slot crowd the positions together; the store must renumber and keep
// the order the operator asked for.
func TestBoardMoveRenumbersACrowdedColumn(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	id := seedIdentity(t, pool)
	ctx := context.Background()
	first, _ := s.AddCard(ctx, id, NewCard{Label: "first"}, SourceCockpit, ActorOperator)
	last, _ := s.AddCard(ctx, id, NewCard{Label: "last"}, SourceCockpit, ActorOperator)
	before := last.ID
	for i := range 60 {
		c, err := s.AddCard(ctx, id, NewCard{Label: fmt.Sprintf("n%02d", i)}, SourceCockpit, ActorOperator)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.MoveCard(ctx, id, c.ID, "todo", before, ActorOperator); err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
		before = c.ID
	}
	cards, _ := s.Cards(ctx, id)
	got := labels(cards, "todo")
	if len(got) != 62 || got[0] != "first" || got[61] != "last" || got[1] != "n59" || got[60] != "n00" {
		t.Fatalf("order after 60 moves = %v ... %v", got[:3], got[len(got)-3:])
	}
	for i := 1; i < len(cards); i++ {
		if cards[i].ColumnID == cards[i-1].ColumnID && !(cards[i].Position > cards[i-1].Position) {
			t.Fatalf("positions not strictly increasing at %d", i)
		}
	}
	_ = first
}

func TestBoardIsIdentityScoped(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	owner, other := seedIdentity(t, pool), seedIdentity(t, pool)
	ctx := context.Background()
	c, _ := s.AddCard(ctx, owner, NewCard{Label: "private"}, SourceCockpit, ActorOperator)

	if _, err := s.Card(ctx, other, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another identity read the card: %v", err)
	}
	label := "taken"
	if _, err := s.UpdateCard(ctx, other, c.ID, CardPatch{Label: &label}, ActorOperator); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another identity updated the card: %v", err)
	}
	if _, err := s.MoveCard(ctx, other, c.ID, "done", "", ActorOperator); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another identity moved the card: %v", err)
	}
	if removed, _ := s.DeleteCard(ctx, other, c.ID); removed {
		t.Fatal("another identity deleted the card")
	}
	if cards, _ := s.Cards(ctx, other); len(cards) != 0 {
		t.Fatalf("another identity lists %d cards", len(cards))
	}
	var visible int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM aura.board_cards`).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("a connection with no principal sees %d cards (%v)", visible, err)
	}
}

func TestBoardColumnsAndViews(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	id := seedIdentity(t, pool)
	ctx := context.Background()
	if _, err := s.AddCard(ctx, id, NewCard{Column: "doing", Label: "busy"}, SourceCockpit, ActorOperator); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetColumns(ctx, id, []Column{{ID: "todo", Label: "To do"}, {ID: "done", Label: "Done"}}); !errors.Is(err, ErrColumnInUse) {
		t.Fatalf("removing a column with cards = %v, want ErrColumnInUse", err)
	}
	b, err := s.SetColumns(ctx, id, []Column{{ID: "todo", Label: "Inbox"}, {ID: "doing", Label: "Doing", CardLimit: 2}, {ID: "review", Label: "Review"}, {ID: "done", Label: "Done"}})
	if err != nil || len(b.Columns) != 4 || b.Columns[0].Label != "Inbox" {
		t.Fatalf("SetColumns = %+v, %v", b, err)
	}

	v, err := s.SaveView(ctx, id, "urgent", json.RawMessage(`{"priority":[3]}`), true)
	if err != nil || v.ID == "" {
		t.Fatalf("SaveView = %+v, %v", v, err)
	}
	if _, err := s.SaveView(ctx, id, "urgent", json.RawMessage(`{"priority":[2,3]}`), false); err != nil {
		t.Fatalf("SaveView replace: %v", err)
	}
	views, _ := s.Views(ctx, id)
	if len(views) != 1 || views[0].Pinned || string(views[0].Filters) != `{"priority": [2, 3]}` {
		t.Fatalf("views after replace = %+v", views)
	}
	if removed, err := s.DeleteView(ctx, id, views[0].ID); err != nil || !removed {
		t.Fatalf("DeleteView = %v, %v", removed, err)
	}
}

func TestBoardLinksAndCascade(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	id := seedIdentity(t, pool)
	ctx := context.Background()
	conv := uuid.NewString()
	err := db.WithIdentityTxRaw(ctx, pool, id, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO aura.conversations (id, identity_id) VALUES ($1, $2)`, conv, id)
		return e
	})
	if err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	c, err := s.AddCard(ctx, id, NewCard{Label: "from chat", ConversationID: conv}, SourceChat, ActorAgent)
	if err != nil || c.ConversationID != conv {
		t.Fatalf("AddCard with link = %+v, %v", c, err)
	}
	if err := db.WithIdentityTxRaw(ctx, pool, id, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM aura.conversations WHERE id = $1`, conv)
		return e
	}); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	after, err := s.Card(ctx, id, c.ID)
	if err != nil || after.ConversationID != "" {
		t.Fatalf("card after its conversation went = %+v, %v; want the link cleared", after, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM aura.identities WHERE id = $1`, id); err != nil {
		t.Fatalf("delete identity: %v", err)
	}
	if cards, _ := s.Cards(ctx, id); len(cards) != 0 {
		t.Fatalf("cards survived their identity: %+v", cards)
	}
}

// The widget draws a duplicate just below its source and saves the editor's whole card; the
// store must put the copy in the same slot and must not stamp an untouched save.
func TestBoardDuplicateAndUntouchedSave(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	id := seedIdentity(t, pool)
	ctx := context.Background()
	conv := uuid.NewString()
	if err := db.WithIdentityTxRaw(ctx, pool, id, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO aura.conversations (id, identity_id) VALUES ($1, $2)`, conv, id)
		return e
	}); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	due := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	src, err := s.AddCard(ctx, id, NewCard{Label: "renew the domain", Description: "before Friday", Priority: 3,
		Tags: []string{"ops"}, DueAt: &due, ConversationID: conv}, SourceChat, ActorAgent)
	if err != nil {
		t.Fatalf("AddCard: %v", err)
	}
	if _, err := s.AddCard(ctx, id, NewCard{Label: "below"}, SourceCockpit, ActorOperator); err != nil {
		t.Fatalf("AddCard below: %v", err)
	}

	label := src.Label
	same, err := s.UpdateCard(ctx, id, src.ID, CardPatch{Label: &label, Tags: &[]string{"ops"}}, ActorOperator)
	if err != nil || same.UpdatedBy != ActorAgent || !same.UpdatedAt.Equal(src.UpdatedAt) {
		t.Fatalf("untouched save = %+v, %v; want the agent's row as it was", same, err)
	}

	dup, err := s.DuplicateCard(ctx, id, src.ID)
	if err != nil {
		t.Fatalf("DuplicateCard: %v", err)
	}
	if dup.ID == src.ID || dup.Label != src.Label || dup.Description != src.Description || dup.Priority != 3 ||
		len(dup.Tags) != 1 || dup.DueAt == nil || !dup.DueAt.Equal(due) {
		t.Fatalf("duplicate = %+v; want the source's content", dup)
	}
	if dup.ConversationID != "" || dup.Source != SourceCockpit || dup.UpdatedBy != ActorOperator {
		t.Fatalf("duplicate = %+v; want the operator's card with no links", dup)
	}
	cards, _ := s.Cards(ctx, id)
	if got := labels(cards, "todo"); len(got) != 3 || got[0] != src.Label || got[1] != src.Label || got[2] != "below" {
		t.Fatalf("todo after duplicate = %v; want the copy just below its source", got)
	}
	if _, err := s.DuplicateCard(ctx, id, uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("duplicate of a missing card = %v, want ErrNotFound", err)
	}
}
