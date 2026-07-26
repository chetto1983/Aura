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
	"github.com/jackc/pgx/v5/pgxpool"
)

type skillDecisionSourceFunc func(
	context.Context,
	tools.SkillRoutingInput,
) (skillDecision, error)

func (fn skillDecisionSourceFunc) decideSkillRouting(
	ctx context.Context,
	input tools.SkillRoutingInput,
) (skillDecision, error) {
	return fn(ctx, input)
}

func TestSkillRoutingPersistsTypedFactsAroundFrozenRanking(t *testing.T) {
	input := skillInput()
	decision := skillShadowDecision(t, input)
	var order []string
	recorder := &recordingToolEvents{order: &order}
	control := newSkillRouting(
		skillDecisionSourceFunc(func(
			context.Context,
			tools.SkillRoutingInput,
		) (skillDecision, error) {
			order = append(order, "select")
			return decision, nil
		}),
		recorder,
	)

	ids, err := control.OrderSkillRouting(
		t.Context(),
		input,
		func(_ context.Context, action string) ([]string, error) {
			order = append(order, "rank")
			if action != tools.SkillRoutingStatic {
				t.Fatalf("shadow executed action %q", action)
			}
			return []string{"alpha", "bravo"}, nil
		},
	)
	order = append(order, "exposure")
	if err != nil {
		t.Fatalf("OrderSkillRouting: %v", err)
	}
	if !slices.Equal(ids, []string{"alpha", "bravo"}) {
		t.Fatalf("ordered IDs = %v", ids)
	}
	if !slices.Equal(order, []string{
		"select", "assignment", "rank", "delivery", "exposure",
	}) {
		t.Fatalf("order = %v", order)
	}
	if len(recorder.assignments) != 1 ||
		len(recorder.deliveries) != 1 {
		t.Fatalf(
			"facts = %d assignments/%d deliveries",
			len(recorder.assignments),
			len(recorder.deliveries),
		)
	}
	var assignment adaptive.Assignment
	for _, assignment = range recorder.assignments {
	}
	if assignment.Domain != adaptive.DomainSkillRouting ||
		assignment.Point != adaptive.PointSkillRouting ||
		assignment.IntendedActionID != tools.SkillRoutingStatic ||
		assignment.RecommendedActionID != tools.SkillRoutingCatalog ||
		assignment.Features[adaptive.FeatureSkillCandidateCount] != 2 {
		t.Fatalf("assignment = %+v", assignment)
	}
	delivery := recorder.deliveries[assignment.AssignmentID]
	payload, err := json.Marshal(delivery)
	if err != nil {
		t.Fatalf("marshal delivery: %v", err)
	}
	text := string(payload)
	for _, want := range []string{
		`"kind":"skill","id":"alpha"`,
		`"kind":"skill","id":"bravo"`,
		input.CatalogRevision,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("delivery %s does not contain %q", payload, want)
		}
	}
	if strings.Contains(text, "spreadsheet") ||
		strings.Contains(text, "query_sha") {
		t.Fatalf("delivery leaked content or fingerprint: %s", payload)
	}
}

func TestSkillRoutingFailsClosedAtEveryBoundary(t *testing.T) {
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
			input := skillInput()
			control := newSkillRouting(
				skillDecisionSourceFunc(func(
					context.Context,
					tools.SkillRoutingInput,
				) (skillDecision, error) {
					if test.sourceErr != nil {
						return skillDecision{}, test.sourceErr
					}
					return skillShadowDecision(t, input), nil
				}),
				&recordingToolEvents{
					assignmentErr: test.assignmentErr,
					deliveryErr:   test.deliveryErr,
				},
			)
			staticCalls := 0
			ids, err := control.OrderSkillRouting(
				t.Context(),
				input,
				func(_ context.Context, action string) ([]string, error) {
					if action == tools.SkillRoutingStatic {
						staticCalls++
						return []string{"alpha"}, nil
					}
					return nil, test.rankErr
				},
			)
			if err != nil ||
				!slices.Equal(ids, []string{"alpha"}) ||
				staticCalls < 1 {
				t.Fatalf(
					"fallback = (%v, %v), static calls=%d",
					ids,
					err,
					staticCalls,
				)
			}
		})
	}
}

func TestSkillRoutingRetriesReuseFactIdentity(t *testing.T) {
	input := skillInput()
	recorder := &recordingToolEvents{}
	control := newSkillRouting(
		skillDecisionSourceFunc(func(
			context.Context,
			tools.SkillRoutingInput,
		) (skillDecision, error) {
			return skillShadowDecision(t, input), nil
		}),
		recorder,
	)
	execute := func(context.Context, string) ([]string, error) {
		return []string{"alpha"}, nil
	}

	for range 2 {
		if _, err := control.OrderSkillRouting(
			t.Context(),
			input,
			execute,
		); err != nil {
			t.Fatalf("retry: %v", err)
		}
	}
	if len(recorder.assignments) != 1 ||
		len(recorder.deliveries) != 1 {
		t.Fatalf(
			"unique facts = %d assignments/%d deliveries",
			len(recorder.assignments),
			len(recorder.deliveries),
		)
	}
}

func TestSkillRoutingRejectsInvalidInputsBeforeAssignment(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*tools.SkillRoutingInput, *skillDecision)
	}{
		{
			name: "blank query",
			mutate: func(
				input *tools.SkillRoutingInput,
				_ *skillDecision,
			) {
				input.QueryLength = 0
			},
		},
		{
			name: "duplicate candidate",
			mutate: func(
				input *tools.SkillRoutingInput,
				_ *skillDecision,
			) {
				input.CandidateIDs = []string{"alpha", "alpha"}
			},
		},
		{
			name: "invalid identity",
			mutate: func(
				input *tools.SkillRoutingInput,
				_ *skillDecision,
			) {
				input.RequestID = "invalid"
			},
		},
		{
			name: "unregistered recommendation",
			mutate: func(
				_ *tools.SkillRoutingInput,
				decision *skillDecision,
			) {
				decision.RecommendedAction = "unregistered"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := skillInput()
			decision := skillShadowDecision(t, input)
			test.mutate(&input, &decision)
			recorder := &recordingToolEvents{}
			control := newSkillRouting(
				skillDecisionSourceFunc(func(
					context.Context,
					tools.SkillRoutingInput,
				) (skillDecision, error) {
					return decision, nil
				}),
				recorder,
			)
			ids, err := control.OrderSkillRouting(
				t.Context(),
				input,
				func(context.Context, string) ([]string, error) {
					return []string{"alpha"}, nil
				},
			)
			if err != nil ||
				!slices.Equal(ids, []string{"alpha"}) {
				t.Fatalf("fallback = (%v, %v)", ids, err)
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

func TestSkillRoutingConstructorsFailClosed(t *testing.T) {
	if newSkillRouting(nil, nil) != nil {
		t.Fatal("nil dependencies produced a skill routing control")
	}
	if NewSkillRouting(nil, "openrouter", "model") != nil {
		t.Fatal("nil pool produced a skill routing control")
	}
	if NewSkillRouting(
		new(pgxpool.Pool),
		"openrouter",
		"model",
	) == nil {
		t.Fatal("live pool did not produce a skill routing control")
	}
	var control *skillRouting
	if _, err := control.OrderSkillRouting(
		t.Context(),
		skillInput(),
		nil,
	); err == nil {
		t.Fatal("nil executor did not fail closed")
	}
}

func skillInput() tools.SkillRoutingInput {
	return tools.SkillRoutingInput{
		OwnerID:         uuid.Must(uuid.NewV7()).String(),
		RequestID:       uuid.Must(uuid.NewV7()).String(),
		PointOrdinal:    9,
		QueryLength:     18,
		CandidateIDs:    []string{"alpha", "bravo"},
		CatalogRevision: "skill-catalog-0123456789ab",
	}
}

func skillShadowDecision(
	t *testing.T,
	input tools.SkillRoutingInput,
) skillDecision {
	t.Helper()
	ownerID := uuid.MustParse(input.OwnerID)
	snapshot, err := adaptive.NewPolicySnapshot(adaptive.SnapshotSpec{
		Scope: adaptive.SnapshotScope{
			OwnerID:        ownerID,
			Domain:         adaptive.DomainSkillRouting,
			Point:          adaptive.PointSkillRouting,
			ProviderID:     "openrouter",
			ModelID:        "production-model",
			PolicyVersion:  "policy-v1",
			TrainingCutoff: time.Now().UTC().Add(-time.Hour),
		},
		ChampionActionID: adaptive.StaticActionID,
		FeatureSchema: []adaptive.SnapshotFeatureDefinition{
			{
				Key:    adaptive.FeatureQueryLength,
				Center: 0,
				Scale:  1,
			},
			{
				Key:    adaptive.FeatureSkillCandidateCount,
				Center: 0,
				Scale:  1,
			},
		},
		Actions: []adaptive.SnapshotAction{
			{ActionID: tools.SkillRoutingCatalog},
			{ActionID: tools.SkillRoutingStatic},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicySnapshot: %v", err)
	}
	return skillDecision{
		Policy: adaptive.Policy{
			Epoch:   1,
			Version: "policy-v1",
			Mode:    adaptive.PolicyShadow,
		},
		Snapshot:          snapshot,
		Environment:       adaptive.EvaluationProductionCanary,
		RecommendedAction: tools.SkillRoutingCatalog,
		ProviderID:        "openrouter",
		ModelID:           "production-model",
	}
}
