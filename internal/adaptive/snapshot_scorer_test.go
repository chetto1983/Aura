package adaptive

import (
	"math"
	"testing"

	"github.com/google/uuid"
)

var snapshotDecisionSink SnapshotDecision

func TestPolicySnapshot_ScoresSimilarityWeightedUtility_When_VectorsAreHandCheckable(t *testing.T) {
	spec := snapshotTestSpec()
	spec.Actions[0].Examples = []SnapshotExample{
		snapshotTestExample(
			spec.Scope,
			"00000000-0000-4000-8000-000000000011",
			1,
			2,
			10,
		),
		snapshotTestExample(
			spec.Scope,
			"00000000-0000-4000-8000-000000000012",
			0,
			2,
			20,
		),
	}
	spec.Actions[1].Examples = []SnapshotExample{
		snapshotTestExample(
			spec.Scope,
			"00000000-0000-4000-8000-000000000013",
			0.5,
			2,
			10,
		),
	}
	snapshot := mustSnapshot(t, spec)

	decision := snapshot.Score(snapshotTestQuery(spec.Scope, 2, 10))

	if decision.ActionID != "deep" || decision.Reason != SnapshotScored {
		t.Fatalf("Score() = %#v, want deep scored decision", decision)
	}
	deep := findSnapshotActionScore(t, decision, "deep")
	want := 1 / (1 + 1/math.Sqrt2)
	if math.Abs(deep.Utility-want) > 1e-12 {
		t.Fatalf("deep utility = %.15f, want %.15f", deep.Utility, want)
	}
	if deep.Neighbors != 2 {
		t.Fatalf("deep neighbors = %d, want 2", deep.Neighbors)
	}
}

func TestPolicySnapshot_IgnoresNegativeCosine_When_SelectingNeighbors(t *testing.T) {
	spec := snapshotTestSpec()
	spec.FeatureSchema = spec.FeatureSchema[:1]
	spec.Actions[0].Examples = []SnapshotExample{
		snapshotTestExample(
			spec.Scope,
			"00000000-0000-4000-8000-000000000021",
			1,
			0,
			10,
		),
	}
	spec.Actions[0].Examples[0].Features = spec.Actions[0].Examples[0].Features[:1]
	spec.Actions[1].Examples = []SnapshotExample{
		snapshotTestExample(
			spec.Scope,
			"00000000-0000-4000-8000-000000000022",
			0.4,
			2,
			10,
		),
	}
	spec.Actions[1].Examples[0].Features = spec.Actions[1].Examples[0].Features[:1]
	snapshot := mustSnapshot(t, spec)
	query := snapshotTestQuery(spec.Scope, 2, 10)
	query.Features = query.Features[:1]

	decision := snapshot.Score(query)

	if decision.ActionID != "direct" {
		t.Fatalf("Score() action = %q, want direct", decision.ActionID)
	}
	if deep := findSnapshotActionScore(t, decision, "deep"); deep.Neighbors != 0 {
		t.Fatalf("negative-cosine deep neighbors = %d, want 0", deep.Neighbors)
	}
}

func TestPolicySnapshot_ReturnsStaticChampion_When_QueryVectorIsZero(t *testing.T) {
	spec := snapshotTestSpec()
	snapshot := mustSnapshot(t, spec)

	decision := snapshot.Score(snapshotTestQuery(spec.Scope, 1, 10))

	if decision.ActionID != StaticActionID || decision.Reason != SnapshotStaticNoSupport {
		t.Fatalf("Score() = %#v, want static no-support decision", decision)
	}
}

func TestPolicySnapshot_ScoresStableCosine_When_VectorsHaveExtremeMagnitude(t *testing.T) {
	tests := []struct {
		name       string
		feature    FeatureKey
		center     float64
		scale      float64
		value      float64
		otherValue float64
	}{
		{
			name:       "huge normalized vector",
			feature:    FeatureCandidateCount,
			scale:      1e-191,
			value:      1_000_000_000,
			otherValue: 1,
		},
		{
			name:       "subnormal normalized vector",
			feature:    FeatureKNNSimilarity,
			scale:      1,
			value:      math.SmallestNonzeroFloat64,
			otherValue: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := snapshotTestSpec()
			spec.FeatureSchema = []SnapshotFeatureDefinition{
				{Key: test.feature, Center: test.center, Scale: test.scale},
			}
			spec.Actions[0].Examples = []SnapshotExample{
				snapshotSingleFeatureExample(
					spec.Scope,
					"00000000-0000-4000-8000-000000000041",
					0.9,
					test.feature,
					test.value,
				),
			}
			spec.Actions[1].Examples = []SnapshotExample{
				snapshotSingleFeatureExample(
					spec.Scope,
					"00000000-0000-4000-8000-000000000042",
					0.1,
					test.feature,
					test.otherValue,
				),
			}
			snapshot := mustSnapshot(t, spec)
			query := snapshotTestQuery(spec.Scope, 1, 10)
			query.Features = []SnapshotFeatureValue{{Key: test.feature, Value: test.value}}

			decision := snapshot.Score(query)

			if decision.ActionID != "deep" || decision.Reason != SnapshotScored {
				t.Fatalf("Score() = %#v, want stable deep score", decision)
			}
			deep := findSnapshotActionScore(t, decision, "deep")
			if deep.Neighbors != 1 || deep.Utility != 0.9 {
				t.Fatalf("deep score = %#v, want one exact neighbor", deep)
			}
		})
	}
}

func TestPolicySnapshot_CapsSupportAtFive_When_MoreNeighborsMatch(t *testing.T) {
	spec := snapshotTestSpec()
	spec.FeatureSchema = spec.FeatureSchema[:1]
	spec.Actions[0].Examples = make([]SnapshotExample, 6)
	for index := range spec.Actions[0].Examples {
		utility := 1.0
		if index == 5 {
			utility = 0
		}
		spec.Actions[0].Examples[index] = snapshotTestExample(
			spec.Scope,
			"00000000-0000-4000-8000-"+leftPadDecimal(index+1, 12),
			utility,
			2,
			10,
		)
		spec.Actions[0].Examples[index].Features =
			spec.Actions[0].Examples[index].Features[:1]
	}
	spec.Actions[1].Examples = []SnapshotExample{
		snapshotTestExample(
			spec.Scope,
			"00000000-0000-4000-8000-000000000010",
			0.9,
			2,
			10,
		),
	}
	spec.Actions[1].Examples[0].Features = spec.Actions[1].Examples[0].Features[:1]
	snapshot := mustSnapshot(t, spec)
	query := snapshotTestQuery(spec.Scope, 2, 10)
	query.Features = query.Features[:1]

	decision := snapshot.Score(query)

	if decision.ActionID != "deep" {
		t.Fatalf("Score() action = %q, want deep", decision.ActionID)
	}
	deep := findSnapshotActionScore(t, decision, "deep")
	if deep.Neighbors != 5 || deep.Utility != 1 {
		t.Fatalf("deep score = %#v, want five neighbors at utility 1", deep)
	}
}

func TestPolicySnapshot_UsesCatalogOrder_When_ActionScoresTie(t *testing.T) {
	spec := snapshotTestSpec()
	spec.Actions[0].Examples = []SnapshotExample{
		snapshotTestExample(
			spec.Scope,
			"00000000-0000-4000-8000-000000000031",
			0.8,
			2,
			10,
		),
	}
	spec.Actions[1].Examples = []SnapshotExample{
		snapshotTestExample(
			spec.Scope,
			"00000000-0000-4000-8000-000000000032",
			0.8,
			2,
			10,
		),
	}
	snapshot := mustSnapshot(t, spec)

	decision := snapshot.Score(snapshotTestQuery(spec.Scope, 2, 10))

	if decision.ActionID != "deep" {
		t.Fatalf("Score() action = %q, want first catalog action deep", decision.ActionID)
	}
}

func TestPolicySnapshot_UsesOutcomeIDOrder_When_NeighborSimilarityTies(t *testing.T) {
	spec := snapshotTestSpec()
	spec.FeatureSchema = spec.FeatureSchema[:1]
	spec.Actions[0].Examples = make([]SnapshotExample, 6)
	for index := range spec.Actions[0].Examples {
		idOrdinal := 6 - index
		utility := 1.0
		if idOrdinal == 6 {
			utility = 0
		}
		spec.Actions[0].Examples[index] = snapshotTestExample(
			spec.Scope,
			"00000000-0000-4000-8000-"+leftPadDecimal(idOrdinal, 12),
			utility,
			2,
			10,
		)
		spec.Actions[0].Examples[index].Features =
			spec.Actions[0].Examples[index].Features[:1]
	}
	spec.Actions[1].Examples = nil
	snapshot := mustSnapshot(t, spec)
	query := snapshotTestQuery(spec.Scope, 2, 10)
	query.Features = query.Features[:1]

	decision := snapshot.Score(query)

	deep := findSnapshotActionScore(t, decision, "deep")
	if deep.Neighbors != 5 || deep.Utility != 1 {
		t.Fatalf("deep score = %#v, want the five lowest outcome IDs", deep)
	}
}

func TestPolicySnapshot_ReturnsStaticChampion_When_ServingScopeMismatches(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SnapshotQuery)
	}{
		{name: "owner", mutate: func(query *SnapshotQuery) {
			query.OwnerID = uuid.MustParse("99999999-9999-4999-8999-999999999999")
		}},
		{name: "domain", mutate: func(query *SnapshotQuery) { query.Domain = DomainKnowledge }},
		{name: "point", mutate: func(query *SnapshotQuery) { query.Point = PointKnowledge }},
		{name: "provider", mutate: func(query *SnapshotQuery) { query.ProviderID = "llamacpp" }},
		{name: "model", mutate: func(query *SnapshotQuery) { query.ModelID = "other-model" }},
		{name: "policy", mutate: func(query *SnapshotQuery) {
			query.PolicyVersion = "other-policy"
		}},
	}
	spec := snapshotTestSpec()
	snapshot := mustSnapshot(t, spec)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := snapshotTestQuery(spec.Scope, 2, 10)
			test.mutate(&query)
			decision := snapshot.Score(query)
			if decision.ActionID != StaticActionID ||
				decision.Reason != SnapshotStaticScopeMismatch {
				t.Fatalf("Score() = %#v, want static scope mismatch", decision)
			}
		})
	}
}

func TestPolicySnapshot_ReturnsStaticChampion_When_QueryIsMalformed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SnapshotQuery)
	}{
		{name: "dimension missing", mutate: func(query *SnapshotQuery) {
			query.Features = query.Features[:1]
		}},
		{name: "feature duplicated", mutate: func(query *SnapshotQuery) {
			query.Features[1].Key = query.Features[0].Key
		}},
		{name: "feature unknown", mutate: func(query *SnapshotQuery) {
			query.Features[0].Key = "prompt_hash"
		}},
		{name: "value nonfinite", mutate: func(query *SnapshotQuery) {
			query.Features[0].Value = math.NaN()
		}},
		{name: "value outside registered range", mutate: func(query *SnapshotQuery) {
			query.Features[0].Value = -1
		}},
	}
	spec := snapshotTestSpec()
	snapshot := mustSnapshot(t, spec)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := snapshotTestQuery(spec.Scope, 2, 10)
			test.mutate(&query)
			decision := snapshot.Score(query)
			if decision.ActionID != StaticActionID ||
				decision.Reason != SnapshotStaticQueryInvalid {
				t.Fatalf("Score() = %#v, want static invalid-query decision", decision)
			}
		})
	}
}

func TestPolicySnapshot_ReturnsStaticChampion_When_ReceiverIsNilOrZero(t *testing.T) {
	var nilSnapshot *PolicySnapshot
	for name, snapshot := range map[string]*PolicySnapshot{
		"nil":  nilSnapshot,
		"zero": {},
	} {
		t.Run(name, func(t *testing.T) {
			decision := snapshot.Score(SnapshotQuery{})
			if decision.ActionID != StaticActionID ||
				decision.Reason != SnapshotStaticInvalid {
				t.Fatalf("Score() = %#v, want static invalid-snapshot decision", decision)
			}
		})
	}
}

func TestPolicySnapshot_ReturnsInvalidSnapshot_When_FrozenSupportIsInconsistent(t *testing.T) {
	spec := snapshotTestSpec()
	snapshot := mustSnapshot(t, spec)
	snapshot.scoringActions[0].examples = nil

	decision := snapshot.Score(snapshotTestQuery(spec.Scope, 2, 10))

	if decision.ActionID != StaticActionID || decision.Reason != SnapshotStaticInvalid {
		t.Fatalf("Score() = %#v, want static invalid-snapshot decision", decision)
	}
}

func TestPolicySnapshot_ScoreAllocationsStayBounded_When_ExampleCountGrows(t *testing.T) {
	smallSpec := snapshotTestSpec()
	smallSpec.Actions[0].Examples = smallSpec.Actions[0].Examples[:1]
	smallSpec.Actions[1].Examples = nil
	small := mustSnapshot(t, smallSpec)

	largeSpec := snapshotTestSpec()
	largeSpec.Actions[1].Examples = nil
	example := largeSpec.Actions[0].Examples[0]
	largeSpec.Actions[0].Examples = make([]SnapshotExample, maxSnapshotExamplesPerAction)
	for index := range largeSpec.Actions[0].Examples {
		example.OutcomeID = uuid.MustParse(
			"00000000-0000-4000-8000-" + leftPadDecimal(index+1, 12),
		)
		largeSpec.Actions[0].Examples[index] = example
	}
	large := mustSnapshot(t, largeSpec)
	query := snapshotTestQuery(smallSpec.Scope, 2, 10)

	smallAllocations := testing.AllocsPerRun(50, func() {
		snapshotDecisionSink = small.Score(query)
	})
	largeAllocations := testing.AllocsPerRun(50, func() {
		snapshotDecisionSink = large.Score(query)
	})

	if largeAllocations > smallAllocations+1 {
		t.Fatalf(
			"Score() allocations grow with examples: small=%.0f large=%.0f",
			smallAllocations,
			largeAllocations,
		)
	}
}

func snapshotTestQuery(
	scope SnapshotScope,
	candidates float64,
	contextLength float64,
) SnapshotQuery {
	return SnapshotQuery{
		OwnerID:       scope.OwnerID,
		Domain:        scope.Domain,
		Point:         scope.Point,
		ProviderID:    scope.ProviderID,
		ModelID:       scope.ModelID,
		PolicyVersion: scope.PolicyVersion,
		Features: []SnapshotFeatureValue{
			{Key: FeatureCandidateCount, Value: candidates},
			{Key: FeatureContextLength, Value: contextLength},
		},
	}
}

func mustSnapshot(t *testing.T, spec SnapshotSpec) *PolicySnapshot {
	t.Helper()
	snapshot, err := NewPolicySnapshot(spec)
	if err != nil {
		t.Fatalf("NewPolicySnapshot() error = %v", err)
	}
	return snapshot
}

func snapshotSingleFeatureExample(
	scope SnapshotScope,
	outcomeID string,
	utility float64,
	feature FeatureKey,
	value float64,
) SnapshotExample {
	example := snapshotTestExample(scope, outcomeID, utility, 1, 10)
	example.Features = []SnapshotFeatureValue{{Key: feature, Value: value}}
	return example
}

func findSnapshotActionScore(
	t *testing.T,
	decision SnapshotDecision,
	actionID string,
) SnapshotActionScore {
	t.Helper()
	for _, score := range decision.Scores {
		if score.ActionID == actionID {
			return score
		}
	}
	t.Fatalf("action score %q missing from %#v", actionID, decision.Scores)
	return SnapshotActionScore{}
}
