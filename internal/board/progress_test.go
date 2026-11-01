package board

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestBoardProgressRejectsInvalidRatiosBeforeAccessingTheDatabase(t *testing.T) {
	store := New(nil)
	for _, value := range []float64{-0.01, 1.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := store.AddCard(context.Background(), "00000000-0000-0000-0000-000000000001", NewCard{Label: "renew contract", Progress: value}, SourceCockpit, ActorOperator); !errors.Is(err, ErrInvalid) {
			t.Errorf("add progress %v: %v", value, err)
		}
		if _, err := store.UpdateCard(context.Background(), "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000099", CardPatch{Progress: &value}, ActorOperator); !errors.Is(err, ErrInvalid) {
			t.Errorf("update progress %v: %v", value, err)
		}
	}
}
