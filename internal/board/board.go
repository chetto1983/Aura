// Package board is the work board's store (prd.md §16, "A work board for the identity and its
// agent", 2026-10-09): one board per identity with its columns, the cards the operator and the
// agent put on it, and the operator's saved views.
//
// It is a Store in the canonical shape internal/identity established (Store{pool,q}, no
// interface declared here). Every method binds app.current_identity before touching a table,
// because all three carry the fail-closed RLS pair from migration 0087: a card of another
// identity is not forbidden here, it is invisible, and reads as not found.
package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// DefaultName is the one board every identity has in this release.
const DefaultName = "main"

// Bounds the cockpit and the tool both meet. The label and description caps are the table's
// CHECKs, repeated here so a caller gets a message instead of a constraint violation.
const (
	MaxLabelRunes       = 200
	MaxDescriptionRunes = 4000
	MaxColumns          = 12
	MaxTags             = 16
	MaxTagRunes         = 40
)

var (
	// ErrNotFound is a card, view or board this identity cannot see, whether it does not
	// exist or belongs to someone else: RLS makes the two indistinguishable, on purpose.
	ErrNotFound = errors.New("not found on this board")
	// ErrInvalid wraps every validation refusal; its message names what to fix.
	ErrInvalid = errors.New("invalid board input")
	// ErrColumnInUse refuses removing a column that still holds cards.
	ErrColumnInUse = errors.New("column still has cards")
)

// Column is the widget's ColumnConfig, stored as the board's jsonb array in display order.
type Column struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	CardLimit int    `json:"cardLimit,omitempty"`
}

// DefaultColumns are StickyFlow's, the board a first use creates: a limit only where work
// piles up.
func DefaultColumns() []Column {
	return []Column{
		{ID: "todo", Label: "To do"},
		{ID: "doing", Label: "Doing", CardLimit: 5},
		{ID: "done", Label: "Done"},
	}
}

// Board is the identity's board and its columns.
type Board struct {
	ID      string
	Name    string
	Columns []Column
}

func (b Board) hasColumn(id string) bool {
	for _, c := range b.Columns {
		if c.ID == id {
			return true
		}
	}
	return false
}

// Store wraps a pgx pool and the generated Queries.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// New builds a Store over an open pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlc.New(pool)}
}

func (s *Store) withIdentity(ctx context.Context, identityID string, fn func(*sqlc.Queries) error) error {
	if s.pool == nil {
		return fn(s.q)
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, fn)
}

// ensureBoard returns the identity's board, creating it with the default columns on first
// use, inside the caller's transaction.
func ensureBoard(ctx context.Context, q *sqlc.Queries, identityID string) (Board, error) {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return Board{}, err
	}
	defaults, err := json.Marshal(DefaultColumns())
	if err != nil {
		return Board{}, err
	}
	row, err := q.EnsureBoard(ctx, sqlc.EnsureBoardParams{IdentityID: id, Name: DefaultName, Columns: defaults})
	if err != nil {
		return Board{}, err
	}
	return boardFromRow(row)
}

func boardFromRow(row sqlc.AuraBoards) (Board, error) {
	var cols []Column
	if err := json.Unmarshal(row.Columns, &cols); err != nil {
		return Board{}, fmt.Errorf("decode board columns: %w", err)
	}
	return Board{ID: row.ID.String(), Name: row.Name, Columns: cols}, nil
}

// Board returns the identity's board, creating it on first use: a missing board is never an
// error a caller has to handle.
func (s *Store) Board(ctx context.Context, identityID string) (Board, error) {
	var b Board
	err := s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var e error
		b, e = ensureBoard(ctx, q, identityID)
		return e
	})
	if err != nil {
		return Board{}, fmt.Errorf("board for %s: %w", identityID, err)
	}
	return b, nil
}

// SetColumns replaces the board's columns. A column that still holds cards cannot be
// removed: its cards would point at nothing and vanish from the board.
func (s *Store) SetColumns(ctx context.Context, identityID string, columns []Column) (Board, error) {
	cleaned, err := validateColumns(columns)
	if err != nil {
		return Board{}, err
	}
	var b Board
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		current, err := ensureBoard(ctx, q, identityID)
		if err != nil {
			return err
		}
		boardID, err := db.ParseUUID("board id", current.ID)
		if err != nil {
			return err
		}
		cards, err := q.ListBoardCards(ctx, boardID)
		if err != nil {
			return err
		}
		next := Board{Columns: cleaned}
		for _, c := range cards {
			if !next.hasColumn(c.ColumnID) {
				return fmt.Errorf("%w: %q", ErrColumnInUse, c.ColumnID)
			}
		}
		raw, err := json.Marshal(cleaned)
		if err != nil {
			return err
		}
		row, err := q.UpdateBoardColumns(ctx, sqlc.UpdateBoardColumnsParams{ID: boardID, Columns: raw})
		if err != nil {
			return err
		}
		b, err = boardFromRow(row)
		return err
	})
	if err != nil {
		return Board{}, fmt.Errorf("set columns for %s: %w", identityID, err)
	}
	return b, nil
}

func validateColumns(columns []Column) ([]Column, error) {
	if len(columns) == 0 || len(columns) > MaxColumns {
		return nil, fmt.Errorf("%w: a board has 1 to %d columns, got %d", ErrInvalid, MaxColumns, len(columns))
	}
	seen := map[string]bool{}
	out := make([]Column, 0, len(columns))
	for _, c := range columns {
		c.ID, c.Label = strings.TrimSpace(c.ID), strings.TrimSpace(c.Label)
		if c.ID == "" || c.Label == "" {
			return nil, fmt.Errorf("%w: every column needs an id and a label", ErrInvalid)
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("%w: column id %q appears twice", ErrInvalid, c.ID)
		}
		if c.CardLimit < 0 {
			return nil, fmt.Errorf("%w: column %q has a negative limit", ErrInvalid, c.ID)
		}
		seen[c.ID] = true
		out = append(out, c)
	}
	return out, nil
}
