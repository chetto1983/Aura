//go:build db_integration

package board

import (
	"context"
	"errors"
	"testing"
)

func TestBoardProgressPersistsAndDoesNotFollowTheColumn(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	owner, other := seedIdentity(t, pool), seedIdentity(t, pool)
	ctx := context.Background()
	card, err := store.AddCard(ctx, owner, NewCard{Label: "renew the supplier contract", Progress: 0.55}, SourceChat, ActorAgent)
	if err != nil || card.Progress != 0.55 {
		t.Fatalf("add: %+v, %v", card, err)
	}
	read, err := store.Card(ctx, owner, card.ID)
	if err != nil || read.Progress != 0.55 {
		t.Fatalf("read: %+v, %v", read, err)
	}
	if _, err := store.Card(ctx, other, card.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other identity: %v", err)
	}
	noChange, err := store.UpdateCard(ctx, owner, card.ID, CardPatch{Progress: &card.Progress}, ActorOperator)
	if err != nil || !noChange.UpdatedAt.Equal(card.UpdatedAt) || noChange.UpdatedBy != ActorAgent {
		t.Fatalf("untouched save: %+v, %v", noChange, err)
	}
	progress := 0.8
	updated, err := store.UpdateCard(ctx, owner, card.ID, CardPatch{Progress: &progress}, ActorOperator)
	if err != nil || updated.Progress != 0.8 || updated.UpdatedBy != ActorOperator {
		t.Fatalf("progress-only update: %+v, %v", updated, err)
	}
	label := "confirm the renewal"
	updated, err = store.UpdateCard(ctx, owner, card.ID, CardPatch{Label: &label}, ActorOperator)
	if err != nil || updated.Progress != 0.8 {
		t.Fatalf("omitted progress: %+v, %v", updated, err)
	}
	moved, err := store.MoveCard(ctx, owner, card.ID, "done", "", ActorOperator)
	if err != nil || moved.Progress != 0.8 {
		t.Fatalf("move changed progress: %+v, %v", moved, err)
	}
	copied, err := store.DuplicateCard(ctx, owner, card.ID)
	if err != nil || copied.Progress != 0.8 {
		t.Fatalf("duplicate: %+v, %v", copied, err)
	}
	progress = 0
	reset, err := store.UpdateCard(ctx, owner, card.ID, CardPatch{Progress: &progress}, ActorAgent)
	if err != nil || reset.Progress != 0 {
		t.Fatalf("reset: %+v, %v", reset, err)
	}
	plain, err := store.AddCard(ctx, owner, NewCard{Label: "check the invoice"}, SourceCockpit, ActorOperator)
	if err != nil || plain.Progress != 0 {
		t.Fatalf("default: %+v, %v", plain, err)
	}
}
