package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// MaxViewNameRunes is the table's CHECK on a view name.
const MaxViewNameRunes = 80

// View is a saved filter over the board. Filters is the cockpit's own shape; the store keeps
// it as JSON and never interprets it, so a new filter field needs no migration.
type View struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Filters json.RawMessage `json:"filters"`
	Pinned  bool            `json:"pinned"`
}

// Views lists the identity's saved views, pinned first.
func (s *Store) Views(ctx context.Context, identityID string) ([]View, error) {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	var rows []sqlc.AuraBoardViews
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var e error
		rows, e = q.ListBoardViews(ctx, id)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("list views: %w", err)
	}
	out := make([]View, 0, len(rows))
	for _, r := range rows {
		out = append(out, View{ID: r.ID.String(), Name: r.Name, Filters: r.Filters, Pinned: r.Pinned})
	}
	return out, nil
}

// SaveView creates or replaces the view with this name.
func (s *Store) SaveView(ctx context.Context, identityID, name string, filters json.RawMessage, pinned bool) (View, error) {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return View{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxViewNameRunes {
		return View{}, fmt.Errorf("%w: a view name has 1 to %d characters", ErrInvalid, MaxViewNameRunes)
	}
	if len(filters) == 0 {
		filters = json.RawMessage(`{}`)
	}
	var probe map[string]any
	if err := json.Unmarshal(filters, &probe); err != nil {
		return View{}, fmt.Errorf("%w: filters must be a JSON object", ErrInvalid)
	}
	var row sqlc.AuraBoardViews
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var e error
		row, e = q.UpsertBoardView(ctx, sqlc.UpsertBoardViewParams{IdentityID: id, Name: name, Filters: filters, Pinned: pinned})
		return e
	})
	if err != nil {
		return View{}, fmt.Errorf("save view: %w", err)
	}
	return View{ID: row.ID.String(), Name: row.Name, Filters: row.Filters, Pinned: row.Pinned}, nil
}

// DeleteView removes a view and reports whether one was removed.
func (s *Store) DeleteView(ctx context.Context, identityID, viewID string) (bool, error) {
	return s.remove(ctx, identityID, "view", viewID, (*sqlc.Queries).DeleteBoardView)
}
