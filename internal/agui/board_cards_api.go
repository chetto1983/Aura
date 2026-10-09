package agui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/board"
)

// boardCardJSON is a card in the widget's shape: id, label, description, column, priority,
// tags and deadline are SVAR's fields; the rest are Aura's, read by the cockpit's card template.
// deadline is RFC 3339, which the provider's parseCards turns into a Date.
type boardCardJSON struct {
	ID             string     `json:"id"`
	Label          string     `json:"label"`
	Description    string     `json:"description"`
	Column         string     `json:"column"`
	Priority       int        `json:"priority"`
	Tags           []string   `json:"tags"`
	Deadline       *time.Time `json:"deadline,omitempty"`
	ConversationID string     `json:"conversation_id,omitempty"`
	TaskID         string     `json:"task_id,omitempty"`
	Source         string     `json:"source"`
	UpdatedBy      string     `json:"updated_by"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func boardCardFrom(c board.Card) boardCardJSON {
	out := boardCardJSON{
		ID: c.ID, Label: c.Label, Description: c.Description, Column: c.ColumnID, Priority: c.Priority,
		Tags: c.Tags, ConversationID: c.ConversationID, TaskID: c.TaskID,
		Source: string(c.Source), UpdatedBy: string(c.UpdatedBy), UpdatedAt: c.UpdatedAt.UTC(),
	}
	if c.DueAt != nil {
		due := c.DueAt.UTC()
		out.Deadline = &due
	}
	return out
}

// boardCardInput is what the widget sends on add and update. A field that is absent is left as
// it is; deadline is raw so that null (the editor's clear) is told apart from absent. Fields the
// widget echoes back (id, column, source, ...) are ignored: a card changes column only by a move.
type boardCardInput struct {
	Label       *string         `json:"label"`
	Description *string         `json:"description"`
	Column      string          `json:"column"`
	Priority    *int            `json:"priority"`
	Tags        *[]string       `json:"tags"`
	Deadline    json.RawMessage `json:"deadline"`
}

// deadline reads the raw field: set reports whether it was present, due is nil for a clear.
func (in boardCardInput) deadline() (due *time.Time, set bool, err error) {
	if len(in.Deadline) == 0 {
		return nil, false, nil
	}
	if bytes.Equal(in.Deadline, []byte("null")) {
		return nil, true, nil
	}
	var t time.Time
	if err := json.Unmarshal(in.Deadline, &t); err != nil {
		return nil, false, err
	}
	return &t, true, nil
}

func (s *Server) handleBoardCards(w http.ResponseWriter, r *http.Request, store boardStore, identityID string) {
	cards, err := store.Cards(r.Context(), identityID)
	if err != nil {
		writeBoardError(w, err)
		return
	}
	out := make([]boardCardJSON, 0, len(cards))
	for _, c := range cards {
		out = append(out, boardCardFrom(c))
	}
	writeJSON(w, out)
}

func (s *Server) handleBoardAddCard(w http.ResponseWriter, r *http.Request, store boardStore, identityID string) {
	in, due, _, ok := decodeBoardCard(w, r)
	if !ok {
		return
	}
	card := board.NewCard{Column: in.Column, DueAt: due}
	if in.Label != nil {
		card.Label = *in.Label
	}
	if in.Description != nil {
		card.Description = *in.Description
	}
	if in.Priority != nil {
		card.Priority = *in.Priority
	}
	if in.Tags != nil {
		card.Tags = *in.Tags
	}
	added, err := store.AddCard(r.Context(), identityID, card, board.SourceCockpit, board.ActorOperator)
	writeBoardCard(w, added, err)
}

// handleBoardUpdateCard applies the fields present. The widget's editor saves the whole card,
// and the store writes nothing when nothing changed.
func (s *Server) handleBoardUpdateCard(w http.ResponseWriter, r *http.Request, store boardStore, identityID string) {
	in, due, dueSet, ok := decodeBoardCard(w, r)
	if !ok {
		return
	}
	patch := board.CardPatch{Label: in.Label, Description: in.Description, Priority: in.Priority, Tags: in.Tags}
	if dueSet {
		patch.DueAt = &due
	}
	updated, err := store.UpdateCard(r.Context(), identityID, r.PathValue("id"), patch, board.ActorOperator)
	writeBoardCard(w, updated, err)
}

// handleBoardMoveCard reads the widget's move event: column is absent for a reorder within the
// column, and before is null for the bottom.
func (s *Server) handleBoardMoveCard(w http.ResponseWriter, r *http.Request, store boardStore, identityID string) {
	var body struct {
		Column string `json:"column"`
		Before string `json:"before"`
	}
	if !decodeBoardBody(w, r, &body) {
		return
	}
	moved, err := store.MoveCard(r.Context(), identityID, r.PathValue("id"), body.Column, body.Before, board.ActorOperator)
	writeBoardCard(w, moved, err)
}

// handleBoardDuplicateCard reads no field: the widget's context menu sends no body.
func (s *Server) handleBoardDuplicateCard(w http.ResponseWriter, r *http.Request, store boardStore, identityID string) {
	if !decodeBoardBody(w, r, &struct{}{}) {
		return
	}
	copied, err := store.DuplicateCard(r.Context(), identityID, r.PathValue("id"))
	writeBoardCard(w, copied, err)
}

func (s *Server) handleBoardDeleteCard(w http.ResponseWriter, r *http.Request, store boardStore, identityID string) {
	removed, err := store.DeleteCard(r.Context(), identityID, r.PathValue("id"))
	if err == nil && !removed {
		err = board.ErrNotFound
	}
	if err != nil {
		writeBoardError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"deleted": true})
}

// decodeBoardCard reads a card body; dueSet reports whether deadline was present at all.
func decodeBoardCard(w http.ResponseWriter, r *http.Request) (in boardCardInput, due *time.Time, dueSet, ok bool) {
	if !decodeBoardBody(w, r, &in) {
		return in, nil, false, false
	}
	due, dueSet, err := in.deadline()
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid", "message": "deadline is an RFC 3339 date"})
		return in, nil, false, false
	}
	return in, due, dueSet, true
}

func writeBoardCard(w http.ResponseWriter, c board.Card, err error) {
	if err != nil {
		writeBoardError(w, err)
		return
	}
	writeJSON(w, boardCardFrom(c))
}
