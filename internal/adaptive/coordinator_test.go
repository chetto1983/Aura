package adaptive

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestDecideAndDeliverCommitsBeforeExecutionAndExposure(t *testing.T) {
	assignment := validAssignment()
	delivery := validDelivery(assignment.AssignmentID)
	var order []string

	value, err := DecideAndDeliver(
		t.Context(),
		assignment,
		func(context.Context, Event) error {
			order = append(order, "assignment")
			return nil
		},
		func(context.Context, Assignment) (Exposure[string], error) {
			order = append(order, "learned")
			return Exposure[string]{Value: "learned", Delivery: delivery}, nil
		},
		func(context.Context, Event) error {
			order = append(order, "delivery")
			return nil
		},
		func(context.Context) (string, error) {
			order = append(order, "static")
			return "static", nil
		},
	)
	order = append(order, "exposed")

	if err != nil {
		t.Fatalf("DecideAndDeliver: %v", err)
	}
	if value != "learned" {
		t.Fatalf("value = %q, want learned", value)
	}
	if want := []string{"assignment", "learned", "delivery", "exposed"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestDecideAndDeliverDiscardsLearnedPathAndFallsBack(t *testing.T) {
	assignment := validAssignment()
	successDelivery := validDelivery(assignment.AssignmentID)
	errBoundary := errors.New("boundary failed")

	tests := []struct {
		name            string
		assign          func(context.Context, Event) error
		learned         func(context.Context, Assignment) (Exposure[string], error)
		deliver         func(context.Context, Event) error
		wantLearnedRuns int
		wantDeliverRuns int
	}{
		{
			name:   "assignment commit failure",
			assign: func(context.Context, Event) error { return errBoundary },
			learned: func(context.Context, Assignment) (Exposure[string], error) {
				return Exposure[string]{Value: "must-not-run", Delivery: successDelivery}, nil
			},
			deliver: func(context.Context, Event) error { return nil },
		},
		{
			name:   "learned execution failure",
			assign: func(context.Context, Event) error { return nil },
			learned: func(context.Context, Assignment) (Exposure[string], error) {
				return Exposure[string]{}, errBoundary
			},
			deliver:         func(context.Context, Event) error { return nil },
			wantLearnedRuns: 1,
		},
		{
			name:   "delivery commit failure",
			assign: func(context.Context, Event) error { return nil },
			learned: func(context.Context, Assignment) (Exposure[string], error) {
				return Exposure[string]{Value: "discard-me", Delivery: successDelivery}, nil
			},
			deliver:         func(context.Context, Event) error { return errBoundary },
			wantLearnedRuns: 1,
			wantDeliverRuns: 1,
		},
		{
			name:   "invalid learned delivery",
			assign: func(context.Context, Event) error { return nil },
			learned: func(context.Context, Assignment) (Exposure[string], error) {
				invalid := successDelivery
				invalid.IntendedActionID = "candidate"
				return Exposure[string]{Value: "discard-me", Delivery: invalid}, nil
			},
			deliver:         func(context.Context, Event) error { return nil },
			wantLearnedRuns: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			learnedRuns := 0
			deliverRuns := 0
			value, err := DecideAndDeliver(
				t.Context(),
				assignment,
				tt.assign,
				func(ctx context.Context, got Assignment) (Exposure[string], error) {
					learnedRuns++
					return tt.learned(ctx, got)
				},
				func(ctx context.Context, event Event) error {
					deliverRuns++
					return tt.deliver(ctx, event)
				},
				func(context.Context) (string, error) { return "static", nil },
			)
			if err != nil {
				t.Fatalf("DecideAndDeliver fallback: %v", err)
			}
			if value != "static" {
				t.Fatalf("value = %q, want static", value)
			}
			if learnedRuns != tt.wantLearnedRuns {
				t.Fatalf("learned runs = %d, want %d", learnedRuns, tt.wantLearnedRuns)
			}
			if deliverRuns != tt.wantDeliverRuns {
				t.Fatalf("delivery runs = %d, want %d", deliverRuns, tt.wantDeliverRuns)
			}
		})
	}
}

func TestDecideAndDeliverSurfacesStaticFailure(t *testing.T) {
	assignment := validAssignment()
	staticErr := errors.New("static failed")

	value, err := DecideAndDeliver(
		t.Context(),
		assignment,
		func(context.Context, Event) error { return errors.New("assignment failed") },
		func(context.Context, Assignment) (Exposure[string], error) {
			t.Fatal("learned path ran after assignment failure")
			return Exposure[string]{}, nil
		},
		func(context.Context, Event) error {
			t.Fatal("delivery ran after assignment failure")
			return nil
		},
		func(context.Context) (string, error) { return "discard", staticErr },
	)

	if !errors.Is(err, staticErr) {
		t.Fatalf("error = %v, want %v", err, staticErr)
	}
	if value != "" {
		t.Fatalf("value = %q, want zero value", value)
	}
}

func TestDecideAndDeliverExactRetryReusesEventIdentity(t *testing.T) {
	assignment := validAssignment()
	delivery := validDelivery(assignment.AssignmentID)
	seenAssignments := make(map[string]struct{})
	seenDeliveries := make(map[string]struct{})

	for range 2 {
		value, err := DecideAndDeliver(
			t.Context(),
			assignment,
			func(_ context.Context, event Event) error {
				seenAssignments[event.ID.String()] = struct{}{}
				return nil
			},
			func(context.Context, Assignment) (Exposure[int], error) {
				return Exposure[int]{Value: 7, Delivery: delivery}, nil
			},
			func(_ context.Context, event Event) error {
				seenDeliveries[event.ID.String()] = struct{}{}
				return nil
			},
			func(context.Context) (int, error) { return 0, nil },
		)
		if err != nil {
			t.Fatalf("DecideAndDeliver retry: %v", err)
		}
		if value != 7 {
			t.Fatalf("retry value = %d, want 7", value)
		}
	}
	if len(seenAssignments) != 1 {
		t.Fatalf("assignment identities = %v, want one exact-retry identity", seenAssignments)
	}
	if len(seenDeliveries) != 1 {
		t.Fatalf("delivery identities = %v, want one exact-retry identity", seenDeliveries)
	}
}
