package adaptive

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"
)

func TestEvaluateSelectedIntentionOPE_EnforcesFrozenArtifactsAndThresholds(t *testing.T) {
	input := validSelectedIntentionOPEInput(t)
	result, err := EvaluateSelectedIntentionOPE(input)
	if err != nil {
		t.Fatalf("EvaluateSelectedIntentionOPE() error = %v", err)
	}
	if result.DR != 1 || result.SNIPS != 1 || result.ESS != 1 {
		t.Fatalf("result = %#v, want DR=SNIPS=ESS=1", result)
	}
	if input.Rows[0].DeliveryKnown {
		t.Fatal("fixture must prove unknown delivery remains selected-intention eligible")
	}

	tests := []struct {
		name   string
		mutate func(*SelectedIntentionOPEInput)
	}{
		{
			name: "reward leakage",
			mutate: func(input *SelectedIntentionOPEInput) {
				input.Plan.RewardDefinitionSHA256 = repeatedSHA("f")
			},
		},
		{
			name: "target policy leakage",
			mutate: func(input *SelectedIntentionOPEInput) {
				input.Plan.TargetPolicySHA256 = repeatedSHA("f")
			},
		},
		{
			name: "behavior policy leakage",
			mutate: func(input *SelectedIntentionOPEInput) {
				input.Plan.BehaviorPolicySHA256 = repeatedSHA("f")
			},
		},
		{
			name: "outcome model leakage",
			mutate: func(input *SelectedIntentionOPEInput) {
				input.Plan.OutcomeModelSHA256 = repeatedSHA("f")
			},
		},
		{
			name: "prediction leakage",
			mutate: func(input *SelectedIntentionOPEInput) {
				input.Plan.PredictionSHA256 = repeatedSHA("f")
			},
		},
		{
			name: "overlap below one half",
			mutate: func(input *SelectedIntentionOPEInput) {
				input.Rows[1].ActionProbabilities[1].Probability = MustExactRational(1, 4)
				input.Rows[1].ActionProbabilities[0].Probability = MustExactRational(3, 4)
			},
		},
		{
			name: "behavior does not normalize",
			mutate: func(input *SelectedIntentionOPEInput) {
				input.Rows[0].ActionProbabilities[0].Probability = MustExactRational(1, 4)
			},
		},
		{
			name: "insufficient ess",
			mutate: func(input *SelectedIntentionOPEInput) {
				input.Plan.MinimumESS = MustExactRational(2, 1)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneSelectedIntentionOPEInput(input)
			test.mutate(&candidate)
			if _, err := EvaluateSelectedIntentionOPE(candidate); err == nil {
				t.Fatal("EvaluateSelectedIntentionOPE() accepted invalid frozen evidence")
			}
		})
	}
}

func TestSelectedIntentionOPE_RejectsMalformedArtifactsAndMemberKeys(t *testing.T) {
	base := validSelectedIntentionOPEInput(t)
	tests := []struct {
		name   string
		mutate func(*SelectedIntentionOPEInput)
	}{
		{name: "endpoint", mutate: func(input *SelectedIntentionOPEInput) {
			input.Plan.EndpointKind = "composite"
		}},
		{name: "threshold", mutate: func(input *SelectedIntentionOPEInput) {
			input.Plan.MinimumESS = MustExactRational(0, 1)
		}},
		{name: "action artifact", mutate: func(input *SelectedIntentionOPEInput) {
			input.Artifacts.ActionSchema.Revision = 2
		}},
		{name: "reward artifact", mutate: func(input *SelectedIntentionOPEInput) {
			input.Artifacts.RewardDefinition.Revision = 2
		}},
		{name: "target artifact", mutate: func(input *SelectedIntentionOPEInput) {
			input.Artifacts.TargetPolicy.Revision = 2
		}},
		{name: "behavior artifact", mutate: func(input *SelectedIntentionOPEInput) {
			input.Artifacts.BehaviorPolicy.Revision = 2
		}},
		{name: "prediction artifact", mutate: func(input *SelectedIntentionOPEInput) {
			input.Artifacts.Prediction.Revision = 2
		}},
		{name: "empty rows", mutate: func(input *SelectedIntentionOPEInput) {
			input.Rows = nil
		}},
		{name: "stratum hash", mutate: func(input *SelectedIntentionOPEInput) {
			input.Rows[0].Order.AnalysisStratumID = ""
		}},
		{name: "claim uuid", mutate: func(input *SelectedIntentionOPEInput) {
			input.Rows[0].Order.ClaimID = ""
		}},
		{name: "action catalog", mutate: func(input *SelectedIntentionOPEInput) {
			input.Rows[0].ActionProbabilities = nil
		}},
		{name: "intended support", mutate: func(input *SelectedIntentionOPEInput) {
			input.Rows[0].IntendedActionID = "missing"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := cloneSelectedIntentionOPEInput(base)
			test.mutate(&input)
			if _, err := EvaluateSelectedIntentionOPE(input); err == nil {
				t.Fatal("EvaluateSelectedIntentionOPE() accepted malformed evidence")
			}
		})
	}
	caps := DefaultOPEUncertaintyCaps()
	oneCluster := cloneSelectedIntentionOPEInput(base)
	oneCluster.Rows[1].Order.InterferenceClusterID =
		oneCluster.Rows[0].Order.InterferenceClusterID
	if _, err := ClusterBootstrapDR(context.Background(), [32]byte{}, oneCluster, caps); err == nil {
		t.Fatal("ClusterBootstrapDR() accepted one cluster")
	}
	caps.MaxMembers = 1
	if _, err := ClusterBootstrapDR(context.Background(), [32]byte{}, base, caps); err == nil {
		t.Fatal("ClusterBootstrapDR() ignored member cap")
	}
}

func TestSelectedIntentionOPE_CanonicalizesPermutationsAndRejectsDuplicateIdentity(t *testing.T) {
	input := validSelectedIntentionOPEInput(t)
	want, err := EvaluateSelectedIntentionOPE(input)
	if err != nil {
		t.Fatalf("EvaluateSelectedIntentionOPE() error = %v", err)
	}
	permuted := cloneSelectedIntentionOPEInput(input)
	slices.Reverse(permuted.Rows)
	got, err := EvaluateSelectedIntentionOPE(permuted)
	if err != nil {
		t.Fatalf("permuted EvaluateSelectedIntentionOPE() error = %v", err)
	}
	if got != want {
		t.Fatalf("permuted result = %#v, want %#v", got, want)
	}
	permuted.Rows[1].Order = permuted.Rows[0].Order
	if _, err := EvaluateSelectedIntentionOPE(permuted); err == nil {
		t.Fatal("EvaluateSelectedIntentionOPE() accepted duplicate member identity")
	}
}

func TestSelectedIntentionOPESupportsRowSpecificTargetActions(t *testing.T) {
	input := validSelectedIntentionOPEInput(t)
	input.Rows[0].TargetActionID = "static"
	input.Rows[0].ActionProbabilities = []ExactActionProbability{
		{ActionID: "static", Probability: MustExactRational(1, 1)},
		{ActionID: "summarize", Probability: MustExactRational(0, 1)},
	}
	input.Rows[1].TargetActionID = "summarize"

	result, err := EvaluateSelectedIntentionOPE(input)
	if err != nil {
		t.Fatalf("EvaluateSelectedIntentionOPE() row targets: %v", err)
	}
	if result.DR != 0.6 || result.SNIPS != 2.0/3.0 ||
		result.ESS != 9.0/5.0 {
		t.Fatalf("row-specific result = %#v", result)
	}
}

func TestClusterBootstrapDR_IsStreamingDeterministicAndCancellable(t *testing.T) {
	input := validSelectedIntentionOPEInput(t)
	caps := DefaultOPEUncertaintyCaps()
	var seed [32]byte
	want, err := ClusterBootstrapDR(context.Background(), seed, input, caps)
	if err != nil {
		t.Fatalf("ClusterBootstrapDR() error = %v", err)
	}
	permuted := cloneSelectedIntentionOPEInput(input)
	slices.Reverse(permuted.Rows)
	got, err := ClusterBootstrapDR(context.Background(), seed, permuted, caps)
	if err != nil {
		t.Fatalf("permuted ClusterBootstrapDR() error = %v", err)
	}
	if got != want || !finite(got.Lower) || !finite(got.Upper) {
		t.Fatalf("permuted interval = %#v, want %#v", got, want)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ClusterBootstrapDR(ctx, seed, input, caps); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ClusterBootstrapDR() error = %v", err)
	}
	caps.MaxMemoryBytes = 1
	if _, err := ClusterBootstrapDR(context.Background(), seed, input, caps); err == nil {
		t.Fatal("ClusterBootstrapDR() ignored memory cap")
	}
	caps = DefaultOPEUncertaintyCaps()
	caps.MaxWallTime = time.Nanosecond
	if _, err := ClusterBootstrapDR(context.Background(), seed, input, caps); err == nil {
		t.Fatal("ClusterBootstrapDR() ignored wall-time cap")
	}
}

func TestAggregateOPEClusters_AllocationCountDoesNotScaleWithMembers(t *testing.T) {
	makeRows := func(memberCount int) []preparedOPERow {
		rows := make([]preparedOPERow, memberCount)
		for index := range rows {
			rows[index] = preparedOPERow{
				clusterID:      string(rune('a' + index%2)),
				drContribution: float64(index % 3),
			}
		}
		return rows
	}
	allocations := func(rows []preparedOPERow) float64 {
		var aggregateErr error
		count := testing.AllocsPerRun(10, func() {
			_, aggregateErr = aggregateOPEClusters(rows)
		})
		if aggregateErr != nil {
			t.Fatalf("aggregateOPEClusters() error = %v", aggregateErr)
		}
		return count
	}
	small := allocations(makeRows(8))
	large := allocations(makeRows(4_096))
	if large > small+1 {
		t.Fatalf("allocations scaled with members: small=%v large=%v", small, large)
	}
}

func TestNeumaierSum_RejectsNonfiniteAndDiffersFromNaiveReassociation(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := NeumaierSum([]float64{value}); err == nil {
			t.Fatalf("NeumaierSum() accepted %v", value)
		}
	}
	values := []float64{1e16, 1, -1e16}
	got, err := NeumaierSum(values)
	if err != nil {
		t.Fatalf("NeumaierSum() error = %v", err)
	}
	naive := (values[0] + values[1]) + values[2]
	if got != 1 || naive == got || math.Float64bits(got) != math.Float64bits(1) {
		t.Fatalf("Neumaier=%v naive=%v bits=%x", got, naive, math.Float64bits(got))
	}
}

func cloneSelectedIntentionOPEInput(input SelectedIntentionOPEInput) SelectedIntentionOPEInput {
	clone := input
	clone.Rows = make([]OPERow, len(input.Rows))
	for index, row := range input.Rows {
		clone.Rows[index] = row
		clone.Rows[index].ActionProbabilities = slices.Clone(row.ActionProbabilities)
	}
	return clone
}

func validSelectedIntentionOPEInput(t *testing.T) SelectedIntentionOPEInput {
	t.Helper()
	actionSchema := ActionSchemaArtifact{
		SchemaID: "aura.adaptive.action-schema/v1", Revision: 1,
		Actions: []string{"static", "summarize"},
	}
	actionHash, err := actionSchema.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	evaluator := CanonicalEvaluator{
		Kind: "deterministic", ID: "golden", Version: "1", RubricID: "binary",
		CalibrationID: "golden-v1", ProvenanceSHA256: repeatedSHA("2"),
	}
	reward := BinaryRewardDefinition{
		SchemaID: "aura.adaptive.reward-definition/binary-endpoint/v1", Revision: 1,
		EndpointKind: "quality", EndpointID: "quality.primary", Evaluator: evaluator,
		RewardType: "binary", SuccessValue: MustExactRational(1, 1),
		FailureValue:  MustExactRational(0, 1),
		MissingRuleID: "quality-missing-challenger-0-baseline-1-v1",
	}
	rewardHash, err := reward.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	target := TargetPolicyManifest{
		SchemaID: "aura.adaptive.policy-manifest/target/v1", Revision: 1,
		PolicyID: "aura/deterministic-challenger", PolicyRevision: 1,
		ActionSchemaSHA256: actionHash, Rule: "snapshot_recommended_action",
	}
	targetHash, err := target.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	behavior := BehaviorPolicyManifest{
		SchemaID: "aura.adaptive.policy-manifest/behavior/v1", Revision: 1,
		PolicyID: "aura/equal-bernoulli-arm-mixture", PolicyRevision: 1,
		ActionSchemaSHA256: actionHash, RandomizationPlanSHA256: repeatedSHA("1"),
		Rule: "marginalize_deterministic_arm_actions",
	}
	behaviorHash, err := behavior.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	prediction := PredictionVector{
		SchemaID: "aura.adaptive.prediction-vector/action-mean/v1", Revision: 1,
		EndpointKind: "quality", ActionSchemaSHA256: actionHash,
		RewardDefinitionSHA256: rewardHash,
		Predictions: []ActionMeanPrediction{
			{ActionID: "static", SupportCount: 5, SuccessCount: 2, QHat: MustExactRational(2, 5)},
			{ActionID: "summarize", SupportCount: 5, SuccessCount: 4, QHat: MustExactRational(4, 5)},
		},
	}
	predictionHash, err := prediction.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	model := EndpointOutcomeModelBinding{
		EndpointKind: EvidenceEndpointQuality, ArtifactSHA256: repeatedSHA("e"),
		PredictionSHA256: predictionHash, RewardDefinitionSHA256: rewardHash,
		TargetPolicySHA256: targetHash,
	}
	probabilities := []ExactActionProbability{
		{ActionID: "static", Probability: MustExactRational(1, 2)},
		{ActionID: "summarize", Probability: MustExactRational(1, 2)},
	}
	return SelectedIntentionOPEInput{
		Plan: SelectedIntentionOPEPlan{
			EndpointKind:       EvidenceEndpointQuality,
			ActionSchemaSHA256: actionHash, RewardDefinitionSHA256: rewardHash,
			TargetPolicySHA256: targetHash, BehaviorPolicySHA256: behaviorHash,
			OutcomeModelSHA256: model.ArtifactSHA256, PredictionSHA256: predictionHash,
			MinimumESS: MustExactRational(1, 1), MinimumOverlap: MustExactRational(1, 2),
		},
		Artifacts: SelectedIntentionOPEArtifacts{
			ActionSchema: actionSchema, RewardDefinition: reward,
			TargetPolicy: target, BehaviorPolicy: behavior,
			Prediction: prediction, OutcomeModel: model,
		},
		Rows: []OPERow{
			{
				Order: OPEMemberOrder{
					AnalysisStratumID: repeatedSHA("a"), InterferenceClusterID: repeatedSHA("c"),
					ClaimID:      "11111111-1111-4111-8111-111111111111",
					RequestID:    "22222222-2222-4222-8222-222222222222",
					AssignmentID: "33333333-3333-4333-8333-333333333333",
				},
				TargetActionID: "summarize", IntendedActionID: "static",
				Reward: false, DeliveryKnown: false,
				ActionProbabilities: slices.Clone(probabilities),
			},
			{
				Order: OPEMemberOrder{
					AnalysisStratumID: repeatedSHA("b"), InterferenceClusterID: repeatedSHA("d"),
					ClaimID:      "44444444-4444-4444-8444-444444444444",
					RequestID:    "55555555-5555-4555-8555-555555555555",
					AssignmentID: "66666666-6666-4666-8666-666666666666",
				},
				TargetActionID: "summarize", IntendedActionID: "summarize",
				Reward: true, DeliveryKnown: true,
				ActionProbabilities: slices.Clone(probabilities),
			},
		},
	}
}

func repeatedSHA(digit string) string {
	return digit + digit + digit + digit + digit + digit + digit + digit +
		digit + digit + digit + digit + digit + digit + digit + digit +
		digit + digit + digit + digit + digit + digit + digit + digit +
		digit + digit + digit + digit + digit + digit + digit + digit +
		digit + digit + digit + digit + digit + digit + digit + digit +
		digit + digit + digit + digit + digit + digit + digit + digit +
		digit + digit + digit + digit + digit + digit + digit + digit +
		digit + digit + digit + digit + digit + digit + digit + digit
}
