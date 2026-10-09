package board

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// Source is where a card came from; the table's CHECK holds the same three words.
type Source string

const (
	SourceCockpit    Source = "cockpit"
	SourceChat       Source = "chat"
	SourceBackground Source = "background"
)

// Actor is who touched a card last.
type Actor string

const (
	ActorOperator Actor = "operator"
	ActorAgent    Actor = "agent"
)

// Card is one card on the board. ConversationID and TaskID are links, read at render time,
// never copies of what they point at.
type Card struct {
	ID             string
	ColumnID       string
	Position       float64
	Label          string
	Description    string
	Priority       int
	Tags           []string
	DueAt          *time.Time
	ConversationID string
	TaskID         string
	Source         Source
	UpdatedBy      Actor
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewCard is what a caller supplies to add a card. A zero Priority is Medium; an empty
// Column is the board's first column.
type NewCard struct {
	Column         string
	Label          string
	Description    string
	Priority       int
	Tags           []string
	DueAt          *time.Time
	ConversationID string
	TaskID         string
}

// CardPatch changes only the fields that are set.
type CardPatch struct {
	Label       *string
	Description *string
	Priority    *int
	Tags        *[]string
	DueAt       **time.Time
	TaskID      *string
}

// positionGap is the closest two positions may come before a column is renumbered.
const positionGap = 1e-6

// AddCard puts a card at the bottom of its column.
func (s *Store) AddCard(ctx context.Context, identityID string, in NewCard, source Source, actor Actor) (Card, error) {
	fields, err := cleanFields(in.Label, in.Description, in.Priority, in.Tags)
	if err != nil {
		return Card{}, err
	}
	identity, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return Card{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	conversation, err := optionalUUID("conversation id", in.ConversationID)
	if err != nil {
		return Card{}, err
	}
	task, err := optionalUUID("task id", in.TaskID)
	if err != nil {
		return Card{}, err
	}
	var card Card
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		b, err := ensureBoard(ctx, q, identityID)
		if err != nil {
			return err
		}
		column := strings.TrimSpace(in.Column)
		if column == "" {
			column = b.Columns[0].ID
		}
		if !b.hasColumn(column) {
			return columnError(b, column)
		}
		boardID, _ := db.ParseUUID("board id", b.ID)
		siblings, err := q.ListBoardColumnCards(ctx, sqlc.ListBoardColumnCardsParams{BoardID: boardID, ColumnID: column})
		if err != nil {
			return err
		}
		position := 1.0
		if n := len(siblings); n > 0 {
			position = siblings[n-1].Position + 1
		}
		row, err := q.InsertBoardCard(ctx, sqlc.InsertBoardCardParams{
			BoardID: boardID, IdentityID: identity, ColumnID: column, Position: position,
			Label: fields.label, Description: fields.description, Priority: int16(fields.priority),
			Tags: fields.tags, DueAt: timestamp(in.DueAt), ConversationID: conversation, TaskID: task,
			Source: string(source), UpdatedBy: string(actor),
		})
		card = cardFromRow(row)
		return err
	})
	if err != nil {
		return Card{}, fmt.Errorf("add card: %w", err)
	}
	return card, nil
}

// Cards returns every card on the identity's board, column by column in order.
func (s *Store) Cards(ctx context.Context, identityID string) ([]Card, error) {
	var out []Card
	err := s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		b, err := ensureBoard(ctx, q, identityID)
		if err != nil {
			return err
		}
		boardID, _ := db.ParseUUID("board id", b.ID)
		rows, err := q.ListBoardCards(ctx, boardID)
		out = cardsFromRows(rows)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	return out, nil
}

// Card returns one card, or ErrNotFound when this identity cannot see it.
func (s *Store) Card(ctx context.Context, identityID, cardID string) (Card, error) {
	var card Card
	err := s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		row, err := getCard(ctx, q, cardID)
		card = cardFromRow(row)
		return err
	})
	if err != nil {
		return Card{}, fmt.Errorf("get card: %w", err)
	}
	return card, nil
}

// UpdateCard applies the set fields of patch.
func (s *Store) UpdateCard(ctx context.Context, identityID, cardID string, patch CardPatch, actor Actor) (Card, error) {
	var card Card
	err := s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		row, err := getCard(ctx, q, cardID)
		if err != nil {
			return err
		}
		current := cardFromRow(row)
		label, description, priority, tags := current.Label, current.Description, current.Priority, current.Tags
		if patch.Label != nil {
			label = *patch.Label
		}
		if patch.Description != nil {
			description = *patch.Description
		}
		if patch.Priority != nil {
			priority = *patch.Priority
		}
		if patch.Tags != nil {
			tags = *patch.Tags
		}
		fields, err := cleanFields(label, description, priority, tags)
		if err != nil {
			return err
		}
		due := row.DueAt
		if patch.DueAt != nil {
			due = timestamp(*patch.DueAt)
		}
		task := row.TaskID
		if patch.TaskID != nil {
			if task, err = optionalUUID("task id", *patch.TaskID); err != nil {
				return err
			}
		}
		updated, err := q.UpdateBoardCard(ctx, sqlc.UpdateBoardCardParams{
			ID: row.ID, Label: fields.label, Description: fields.description, Priority: int16(fields.priority),
			Tags: fields.tags, DueAt: due, TaskID: task, UpdatedBy: string(actor),
		})
		card = cardFromRow(updated)
		return err
	})
	if err != nil {
		return Card{}, fmt.Errorf("update card: %w", err)
	}
	return card, nil
}

// MoveCard puts a card into column, just before the card beforeID, or at the bottom when
// beforeID is empty. It writes one row, the midpoint of the new neighbours, unless they are
// closer than positionGap, in which case the column is renumbered first.
func (s *Store) MoveCard(ctx context.Context, identityID, cardID, column, beforeID string, actor Actor) (Card, error) {
	var card Card
	err := s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		row, err := getCard(ctx, q, cardID)
		if err != nil {
			return err
		}
		b, err := ensureBoard(ctx, q, identityID)
		if err != nil {
			return err
		}
		if column == "" {
			column = row.ColumnID
		}
		if !b.hasColumn(column) {
			return columnError(b, column)
		}
		siblings, err := q.ListBoardColumnCards(ctx, sqlc.ListBoardColumnCardsParams{BoardID: row.BoardID, ColumnID: column})
		if err != nil {
			return err
		}
		siblings = slices.DeleteFunc(siblings, func(c sqlc.AuraBoardCards) bool { return c.ID == row.ID })
		position, ok := positionBefore(siblings, beforeID)
		if !ok {
			if err := renumber(ctx, q, siblings); err != nil {
				return err
			}
			position, _ = positionBefore(siblings, beforeID)
		}
		moved, err := q.MoveBoardCard(ctx, sqlc.MoveBoardCardParams{
			ID: row.ID, ColumnID: column, Position: position, UpdatedBy: string(actor),
		})
		card = cardFromRow(moved)
		return err
	})
	if err != nil {
		return Card{}, fmt.Errorf("move card: %w", err)
	}
	return card, nil
}

// positionBefore is the position just before beforeID among siblings (ordered by position),
// or after the last one. ok is false when the neighbours are too close to fit a card between.
// A beforeID not among the siblings means the bottom of the column.
func positionBefore(siblings []sqlc.AuraBoardCards, beforeID string) (float64, bool) {
	at := len(siblings)
	for i, c := range siblings {
		if c.ID.String() == beforeID {
			at = i
			break
		}
	}
	switch {
	case len(siblings) == 0:
		return 1, true
	case at == len(siblings):
		return siblings[at-1].Position + 1, true
	case at == 0:
		return siblings[0].Position - 1, true
	}
	lo, hi := siblings[at-1].Position, siblings[at].Position
	if hi-lo < positionGap {
		return 0, false
	}
	return (lo + hi) / 2, true
}

// renumber spaces a column's cards one apart, in their current order, and updates siblings
// in place so the caller can recompute a position against the new values.
func renumber(ctx context.Context, q *sqlc.Queries, siblings []sqlc.AuraBoardCards) error {
	for i := range siblings {
		siblings[i].Position = float64(i + 1)
		if err := q.SetBoardCardPosition(ctx, sqlc.SetBoardCardPositionParams{ID: siblings[i].ID, Position: siblings[i].Position}); err != nil {
			return err
		}
	}
	return nil
}

// DeleteCard removes a card and reports whether one was removed.
func (s *Store) DeleteCard(ctx context.Context, identityID, cardID string) (bool, error) {
	id, err := db.ParseUUID("card id", cardID)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	var removed int64
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var e error
		removed, e = q.DeleteBoardCard(ctx, id)
		return e
	})
	if err != nil {
		return false, fmt.Errorf("delete card: %w", err)
	}
	return removed > 0, nil
}

// Search finds cards whose label or description contains query, or that carry it as a tag.
func (s *Store) Search(ctx context.Context, identityID, query string, limit int) ([]Card, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("%w: a search needs words", ErrInvalid)
	}
	var out []Card
	err := s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		b, err := ensureBoard(ctx, q, identityID)
		if err != nil {
			return err
		}
		boardID, _ := db.ParseUUID("board id", b.ID)
		rows, err := q.SearchBoardCards(ctx, sqlc.SearchBoardCardsParams{BoardID: boardID, Query: query, RowLimit: int32(limit)})
		out = cardsFromRows(rows)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("search cards: %w", err)
	}
	return out, nil
}

func getCard(ctx context.Context, q *sqlc.Queries, cardID string) (sqlc.AuraBoardCards, error) {
	id, err := db.ParseUUID("card id", cardID)
	if err != nil {
		return sqlc.AuraBoardCards{}, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	row, err := q.GetBoardCard(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.AuraBoardCards{}, fmt.Errorf("%w: card %s", ErrNotFound, cardID)
	}
	return row, err
}

func columnError(b Board, column string) error {
	ids := make([]string, 0, len(b.Columns))
	for _, c := range b.Columns {
		ids = append(ids, c.ID)
	}
	return fmt.Errorf("%w: column %q is not on the board; columns are: %s", ErrInvalid, column, strings.Join(ids, ", "))
}

type cleanCard struct {
	label, description string
	priority           int
	tags               []string
}

func cleanFields(label, description string, priority int, tags []string) (cleanCard, error) {
	label, description = strings.TrimSpace(label), strings.TrimSpace(description)
	if label == "" || utf8.RuneCountInString(label) > MaxLabelRunes {
		return cleanCard{}, fmt.Errorf("%w: a label has 1 to %d characters", ErrInvalid, MaxLabelRunes)
	}
	if utf8.RuneCountInString(description) > MaxDescriptionRunes {
		return cleanCard{}, fmt.Errorf("%w: a description has at most %d characters", ErrInvalid, MaxDescriptionRunes)
	}
	if priority == 0 {
		priority = 2
	}
	if priority < 1 || priority > 3 {
		return cleanCard{}, fmt.Errorf("%w: priority is 1 (low), 2 (medium) or 3 (high)", ErrInvalid)
	}
	clean := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || slices.Contains(clean, t) {
			continue
		}
		if utf8.RuneCountInString(t) > MaxTagRunes {
			return cleanCard{}, fmt.Errorf("%w: a tag has at most %d characters", ErrInvalid, MaxTagRunes)
		}
		clean = append(clean, t)
	}
	if len(clean) > MaxTags {
		return cleanCard{}, fmt.Errorf("%w: a card has at most %d tags", ErrInvalid, MaxTags)
	}
	return cleanCard{label: label, description: description, priority: priority, tags: clean}, nil
}

func optionalUUID(field, value string) (pgtype.UUID, error) {
	if strings.TrimSpace(value) == "" {
		return pgtype.UUID{}, nil
	}
	id, err := db.ParseUUID(field, value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return id, nil
}

func timestamp(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return id.String()
}

func cardFromRow(r sqlc.AuraBoardCards) Card {
	c := Card{
		ID: r.ID.String(), ColumnID: r.ColumnID, Position: r.Position, Label: r.Label,
		Description: r.Description, Priority: int(r.Priority), Tags: r.Tags,
		ConversationID: uuidString(r.ConversationID), TaskID: uuidString(r.TaskID),
		Source: Source(r.Source), UpdatedBy: Actor(r.UpdatedBy),
		CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
	}
	if r.DueAt.Valid {
		due := r.DueAt.Time
		c.DueAt = &due
	}
	if c.Tags == nil {
		c.Tags = []string{}
	}
	return c
}

func cardsFromRows(rows []sqlc.AuraBoardCards) []Card {
	out := make([]Card, 0, len(rows))
	for _, r := range rows {
		out = append(out, cardFromRow(r))
	}
	return out
}
