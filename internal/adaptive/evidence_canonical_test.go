package adaptive

import "testing"

func TestCanonicalModelArtifacts_MatchIndependentFrozenGoldenHashes(t *testing.T) {
	actionSchema := ActionSchemaArtifact{
		SchemaID: "aura.adaptive.action-schema/v1",
		Revision: 1,
		Actions:  []string{"static", "summarize"},
	}
	actionHash, err := actionSchema.SHA256()
	if err != nil {
		t.Fatalf("action schema SHA256() error = %v", err)
	}
	if actionHash != "c9c2c63863ade30c50b9f2edf82073b0c1c0e51526628bb0835df9cc2dabd45d" {
		t.Fatalf("action schema hash = %s", actionHash)
	}
	featureHash, err := (EmptyFeatureSchemaArtifact{
		SchemaID: "aura.adaptive.feature-schema/empty/v1", Revision: 1, Features: []struct{}{},
	}).SHA256()
	if err != nil {
		t.Fatalf("feature schema SHA256() error = %v", err)
	}
	if featureHash != "9d3da8a268742cc8871fa33993732e4cf6515b2acb6d52070b292fda3f2620c0" {
		t.Fatalf("feature schema hash = %s", featureHash)
	}
	targetHash, err := (TargetPolicyManifest{
		SchemaID: "aura.adaptive.policy-manifest/target/v1", Revision: 1,
		PolicyID: "aura/deterministic-challenger", PolicyRevision: 1,
		ActionSchemaSHA256: actionHash, Rule: "snapshot_recommended_action",
	}).SHA256()
	if err != nil {
		t.Fatalf("target manifest SHA256() error = %v", err)
	}
	if targetHash != "018e058dc0fc9edfc01320650ffe8e36f783e71617a85d663f62b0b87281d86b" {
		t.Fatalf("target policy hash = %s", targetHash)
	}
	behaviorHash, err := (BehaviorPolicyManifest{
		SchemaID: "aura.adaptive.policy-manifest/behavior/v1", Revision: 1,
		PolicyID: "aura/equal-bernoulli-arm-mixture", PolicyRevision: 1,
		ActionSchemaSHA256:      actionHash,
		RandomizationPlanSHA256: "1111111111111111111111111111111111111111111111111111111111111111",
		Rule:                    "marginalize_deterministic_arm_actions",
	}).SHA256()
	if err != nil {
		t.Fatalf("behavior manifest SHA256() error = %v", err)
	}
	if behaviorHash != "ab0f121f33d30faa5478d16332bb0a731da4f4592e8f019a0c3b4c2722339561" {
		t.Fatalf("behavior policy hash = %s", behaviorHash)
	}
}

func TestCanonicalEndpointArtifacts_MatchIndependentFrozenGoldenHashes(t *testing.T) {
	evaluator := CanonicalEvaluator{
		Kind: "deterministic", ID: "golden", Version: "1", RubricID: "binary",
		CalibrationID:    "golden-v1",
		ProvenanceSHA256: "2222222222222222222222222222222222222222222222222222222222222222",
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
		t.Fatalf("reward SHA256() error = %v", err)
	}
	if rewardHash != "42fd9a385a75dd7412c5ed94e3d9a5ff54e7c76e6075a97f0a783dc203014607" {
		t.Fatalf("reward hash = %s", rewardHash)
	}
	prediction := PredictionVector{
		SchemaID: "aura.adaptive.prediction-vector/action-mean/v1", Revision: 1,
		EndpointKind:           "quality",
		ActionSchemaSHA256:     "c9c2c63863ade30c50b9f2edf82073b0c1c0e51526628bb0835df9cc2dabd45d",
		RewardDefinitionSHA256: rewardHash,
		Predictions: []ActionMeanPrediction{
			{ActionID: "static", SupportCount: 2, SuccessCount: 1, QHat: MustExactRational(1, 2)},
			{ActionID: "summarize", SupportCount: 4, SuccessCount: 3, QHat: MustExactRational(3, 4)},
		},
	}
	predictionHash, err := prediction.SHA256()
	if err != nil {
		t.Fatalf("prediction SHA256() error = %v", err)
	}
	if predictionHash != "51069c52fb07149a9dc36042ba5f664bee6417a079fccb1806ca5ddded7e2bb0" {
		t.Fatalf("prediction hash = %s", predictionHash)
	}
}

func TestBuildEndpointCounts_AppliesFrozenMissingSubstitution(t *testing.T) {
	members := []BinaryITTMember{
		{Treated: true, Outcome: nil},
		{Treated: false, Outcome: nil},
	}
	quality, err := BuildEndpointCounts("s", EvidenceEndpointQuality, members)
	if err != nil {
		t.Fatalf("quality BuildEndpointCounts() error = %v", err)
	}
	if quality.TreatedSuccesses != 0 || quality.ControlSuccesses != 1 {
		t.Fatalf("quality counts = %#v", quality)
	}
	harm, err := BuildEndpointCounts("s", EvidenceEndpointHarm, members)
	if err != nil {
		t.Fatalf("harm BuildEndpointCounts() error = %v", err)
	}
	if harm.TreatedSuccesses != 1 || harm.ControlSuccesses != 0 {
		t.Fatalf("harm counts = %#v", harm)
	}
}
