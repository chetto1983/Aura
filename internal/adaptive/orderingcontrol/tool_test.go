package orderingcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/google/uuid"
)

type toolDecisionSourceFunc func(
	context.Context,
	tools.ToolDiscoveryInput,
) (toolDecision, error)

func (fn toolDecisionSourceFunc) decideToolDiscovery(
	ctx context.Context,
	input tools.ToolDiscoveryInput,
) (toolDecision, error) {
	return fn(ctx, input)
}

type recordingToolEvents struct {
	order         *[]string
	assignments   map[uuid.UUID]adaptive.Assignment
	deliveries    map[uuid.UUID]adaptive.Delivery
	assignmentErr error
	deliveryErr   error
}

func (recorder *recordingToolEvents) RecordAssignment(
	_ context.Context,
	assignment adaptive.Assignment,
) (int64, error) {
	if recorder.order != nil {
		*recorder.order = append(*recorder.order, "assignment")
	}
	if recorder.assignmentErr != nil {
		return 0, recorder.assignmentErr
	}
	if recorder.assignments == nil {
		recorder.assignments = make(map[uuid.UUID]adaptive.Assignment)
	}
	recorder.assignments[assignment.AssignmentID] = assignment
	return 1, nil
}

func (recorder *recordingToolEvents) RecordDelivery(
	_ context.Context,
	_ uuid.UUID,
	assignmentID uuid.UUID,
	delivery adaptive.Delivery,
) (int64, error) {
	if recorder.order != nil {
		*recorder.order = append(*recorder.order, "delivery")
	}
	if recorder.deliveryErr != nil {
		return 0, recorder.deliveryErr
	}
	if recorder.deliveries == nil {
		recorder.deliveries = make(map[uuid.UUID]adaptive.Delivery)
	}
	recorder.deliveries[assignmentID] = delivery
	return 2, nil
}

func TestToolDiscoveryPersistsTypedFactsAroundFrozenRanking(t *testing.T) {
	input := toolInput()
	decision := toolShadowDecision(t, input)
	var order []string
	control := newToolDiscovery(
		toolDecisionSourceFunc(func(
			context.Context,
			tools.ToolDiscoveryInput,
		) (toolDecision, error) {
			order = append(order, "select")
			return decision, nil
		}),
		&recordingToolEvents{order: &order},
	)

	ids, err := control.OrderToolDiscovery(
		t.Context(),
		input,
		func(_ context.Context, action string) ([]string, error) {
			order = append(order, "rank")
			if action != tools.ToolDiscoveryStatic {
				t.Fatalf("shadow executed action %q", action)
			}
			return []string{"calculator", "calendar"}, nil
		},
	)
	order = append(order, "exposure")
	if err != nil {
		t.Fatalf("OrderToolDiscovery: %v", err)
	}
	if !slices.Equal(ids, []string{"calculator", "calendar"}) {
		t.Fatalf("ordered IDs = %v", ids)
	}
	if !slices.Equal(order, []string{
		"select", "assignment", "rank", "delivery", "exposure",
	}) {
		t.Fatalf("order = %v", order)
	}

	recorder := control.recorder.(*recordingToolEvents)
	if len(recorder.assignments) != 1 || len(recorder.deliveries) != 1 {
		t.Fatalf(
			"facts = %d assignments/%d deliveries",
			len(recorder.assignments),
			len(recorder.deliveries),
		)
	}
	var assignment adaptive.Assignment
	for _, assignment = range recorder.assignments {
	}
	if assignment.Domain != adaptive.DomainToolDiscovery ||
		assignment.Point != adaptive.PointToolDiscovery ||
		assignment.IntendedActionID != tools.ToolDiscoveryStatic ||
		assignment.RecommendedActionID != tools.ToolDiscoverySemantic ||
		assignment.Features[adaptive.FeatureQueryLength] !=
			float64(input.QueryLength) {
		t.Fatalf("assignment = %+v", assignment)
	}
	delivery := recorder.deliveries[assignment.AssignmentID]
	payload, err := json.Marshal(delivery)
	if err != nil {
		t.Fatalf("marshal delivery: %v", err)
	}
	text := string(payload)
	for _, want := range []string{
		`"kind":"tool","id":"calculator"`,
		`"kind":"tool","id":"calendar"`,
		input.CatalogRevision,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("delivery %s does not contain %q", payload, want)
		}
	}
	if strings.Contains(text, "arithmetic") ||
		strings.Contains(text, "query_sha") {
		t.Fatalf("delivery leaked query content or fingerprint: %s", payload)
	}
}

func TestToolDiscoveryRetriesReuseFactIdentity(t *testing.T) {
	input := toolInput()
	recorder := &recordingToolEvents{}
	control := newToolDiscovery(
		toolDecisionSourceFunc(func(
			context.Context,
			tools.ToolDiscoveryInput,
		) (toolDecision, error) {
			return toolShadowDecision(t, input), nil
		}),
		recorder,
	)
	execute := func(context.Context, string) ([]string, error) {
		return []string{"calculator"}, nil
	}

	for range 2 {
		if _, err := control.OrderToolDiscovery(
			t.Context(), input, execute,
		); err != nil {
			t.Fatalf("retry: %v", err)
		}
	}
	if len(recorder.assignments) != 1 || len(recorder.deliveries) != 1 {
		t.Fatalf(
			"unique facts = %d assignments/%d deliveries",
			len(recorder.assignments),
			len(recorder.deliveries),
		)
	}
}

func TestToolDiscoveryFailsClosedAtEveryBoundary(t *testing.T) {
	boundaryErr := errors.New("boundary failed")
	tests := []struct {
		name          string
		sourceErr     error
		assignmentErr error
		rankErr       error
		deliveryErr   error
	}{
		{name: "selection", sourceErr: boundaryErr},
		{name: "assignment", assignmentErr: boundaryErr},
		{name: "ranking", rankErr: boundaryErr},
		{name: "delivery", deliveryErr: boundaryErr},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := toolInput()
			control := newToolDiscovery(
				toolDecisionSourceFunc(func(
					context.Context,
					tools.ToolDiscoveryInput,
				) (toolDecision, error) {
					if test.sourceErr != nil {
						return toolDecision{}, test.sourceErr
					}
					return toolShadowDecision(t, input), nil
				}),
				&recordingToolEvents{
					assignmentErr: test.assignmentErr,
					deliveryErr:   test.deliveryErr,
				},
			)
			staticCalls := 0
			ids, err := control.OrderToolDiscovery(
				t.Context(),
				input,
				func(_ context.Context, action string) ([]string, error) {
					if action == tools.ToolDiscoveryStatic {
						staticCalls++
						return []string{"calculator"}, nil
					}
					return nil, test.rankErr
				},
			)
			if err != nil || !slices.Equal(ids, []string{"calculator"}) ||
				staticCalls < 1 {
				t.Fatalf(
					"fallback = (%v, %v), static calls=%d",
					ids, err, staticCalls,
				)
			}
		})
	}
}

func TestToolDiscoveryRejectsInvalidAdaptiveInputsBeforeAssignment(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*tools.ToolDiscoveryInput, *toolDecision)
	}{
		{
			name: "duplicate frozen candidate",
			mutate: func(
				input *tools.ToolDiscoveryInput,
				_ *toolDecision,
			) {
				input.CandidateIDs = []string{"calculator", "calculator"}
			},
		},
		{
			name: "unregistered recommendation",
			mutate: func(
				_ *tools.ToolDiscoveryInput,
				decision *toolDecision,
			) {
				decision.RecommendedAction = "unregistered"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := toolInput()
			decision := toolShadowDecision(t, input)
			test.mutate(&input, &decision)
			recorder := &recordingToolEvents{}
			control := newToolDiscovery(
				toolDecisionSourceFunc(func(
					context.Context,
					tools.ToolDiscoveryInput,
				) (toolDecision, error) {
					return decision, nil
				}),
				recorder,
			)

			ids, err := control.OrderToolDiscovery(
				t.Context(),
				input,
				func(context.Context, string) ([]string, error) {
					return []string{"calculator"}, nil
				},
			)
			if err != nil ||
				!slices.Equal(ids, []string{"calculator"}) {
				t.Fatalf("static fallback = (%v, %v)", ids, err)
			}
			if len(recorder.assignments) != 0 ||
				len(recorder.deliveries) != 0 {
				t.Fatalf(
					"invalid input persisted %d assignments/%d deliveries",
					len(recorder.assignments),
					len(recorder.deliveries),
				)
			}
		})
	}
}

func TestToolDiscoveryCanaryAndActiveStayDiagnostic(t *testing.T) {
	for _, mode := range []adaptive.PolicyMode{
		adaptive.PolicyCanary,
		adaptive.PolicyActive,
	} {
		t.Run(string(mode), func(t *testing.T) {
			input := toolInput()
			decision := toolShadowDecision(t, input)
			decision.Policy.Mode = mode
			recorder := &recordingToolEvents{}
			control := newToolDiscovery(
				toolDecisionSourceFunc(func(
					context.Context,
					tools.ToolDiscoveryInput,
				) (toolDecision, error) {
					return decision, nil
				}),
				recorder,
			)

			if _, err := control.OrderToolDiscovery(
				t.Context(),
				input,
				func(_ context.Context, action string) ([]string, error) {
					if action != tools.ToolDiscoveryStatic {
						t.Fatalf("mode %s executed %q", mode, action)
					}
					return []string{"calculator"}, nil
				},
			); err != nil {
				t.Fatalf("OrderToolDiscovery: %v", err)
			}
			for _, assignment := range recorder.assignments {
				if assignment.IntendedActionID !=
					tools.ToolDiscoveryStatic {
					t.Fatalf("assignment = %+v", assignment)
				}
			}
		})
	}
}

func toolInput() tools.ToolDiscoveryInput {
	return tools.ToolDiscoveryInput{
		OwnerID:         uuid.Must(uuid.NewV7()).String(),
		RequestID:       uuid.Must(uuid.NewV7()).String(),
		PointOrdinal:    7,
		QueryLength:     31,
		MaxResults:      5,
		CandidateIDs:    []string{"calculator", "calendar"},
		CatalogRevision: "tool-catalog-0123456789ab",
	}
}

func toolShadowDecision(
	t *testing.T,
	input tools.ToolDiscoveryInput,
) toolDecision {
	t.Helper()
	ownerID := uuid.MustParse(input.OwnerID)
	snapshot, err := adaptive.NewPolicySnapshot(adaptive.SnapshotSpec{
		Scope: adaptive.SnapshotScope{
			OwnerID: ownerID, Domain: adaptive.DomainToolDiscovery,
			Point: adaptive.PointToolDiscovery, ProviderID: "openrouter",
			ModelID: "production-model", PolicyVersion: "policy-v1",
			TrainingCutoff: time.Now().UTC().Add(-time.Hour),
		},
		ChampionActionID: adaptive.StaticActionID,
		FeatureSchema: []adaptive.SnapshotFeatureDefinition{
			{Key: adaptive.FeatureCandidateCount, Center: 0, Scale: 1},
			{Key: adaptive.FeatureQueryLength, Center: 0, Scale: 1},
			{Key: adaptive.FeatureRetrievalLimit, Center: 0, Scale: 1},
		},
		Actions: []adaptive.SnapshotAction{
			{ActionID: tools.ToolDiscoveryBM25},
			{ActionID: tools.ToolDiscoverySemantic},
			{ActionID: tools.ToolDiscoveryStatic},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicySnapshot: %v", err)
	}
	return toolDecision{
		Policy: adaptive.Policy{
			Epoch: 1, Version: "policy-v1", Mode: adaptive.PolicyShadow,
		},
		Snapshot: snapshot, Environment: adaptive.EvaluationProductionCanary,
		RecommendedAction: tools.ToolDiscoverySemantic,
		ProviderID:        "openrouter", ModelID: "production-model",
	}
}
