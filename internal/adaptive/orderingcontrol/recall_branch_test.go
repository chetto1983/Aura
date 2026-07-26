package orderingcontrol

import (
	"context"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/runner"
)

func TestDynamicRecallEmptyQueryPersistsProviderSequence(t *testing.T) {
	t.Parallel()
	input := memoryRecallInput()
	input.Query = ""
	decision := memoryRecallShadowDecision(t, input)
	order := []string{}
	recorder := &recordingToolEvents{order: &order}
	control := newDynamicRecall(
		memoryRecallDecisionSourceFunc(func(
			_ context.Context,
			got runner.DynamicRecallInput,
		) (toolDecision, error) {
			if got.Query != "" {
				t.Fatalf("decision query = %q, want identity-only empty query", got.Query)
			}
			return decision, nil
		}),
		recorder,
	)

	prepared, err := control.PrepareDynamicRecall(
		t.Context(),
		input,
		func(
			_ context.Context,
			_ string,
			query string,
			_ int,
		) (runner.DynamicRecall, error) {
			order = append(order, "provider")
			if query != "" {
				t.Fatalf("provider query = %q, want identity-only empty query", query)
			}
			return validControlDynamicRecall(), nil
		},
	)
	if err != nil {
		t.Fatalf("PrepareDynamicRecall: %v", err)
	}
	if prepared == nil {
		t.Fatal("empty-query branch silently fell back to static")
	}
	if !slices.Equal(order, []string{"assignment", "provider"}) {
		t.Fatalf("pre-commit order = %v", order)
	}
	for _, assignment := range recorder.assignments {
		if assignment.Features[adaptive.FeatureQueryLength] != 0 {
			t.Fatalf("query-length feature = %v, want 0", assignment.Features)
		}
	}
	if err := prepared.Commit(
		t.Context(),
		agent.DynamicTailOutcome{Delivered: true},
	); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !slices.Equal(order, []string{"assignment", "provider", "delivery"}) {
		t.Fatalf("final order = %v", order)
	}
}
