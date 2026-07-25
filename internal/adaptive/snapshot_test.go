package adaptive

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewPolicySnapshot_FreezesCanonicalContent_When_InputIsValid(t *testing.T) {
	spec := snapshotTestSpec()
	snapshot, err := NewPolicySnapshot(spec)
	if err != nil {
		t.Fatalf("NewPolicySnapshot() error = %v", err)
	}

	if snapshot.ID() == uuid.Nil {
		t.Fatal("snapshot ID is nil")
	}
	if !validSHA256(snapshot.SHA256()) {
		t.Fatalf("snapshot SHA256 = %q, want lowercase SHA-256", snapshot.SHA256())
	}
	if got := snapshot.Scope(); got != spec.Scope {
		t.Fatalf("snapshot scope = %#v, want %#v", got, spec.Scope)
	}
	if got := snapshot.ChampionActionID(); got != StaticActionID {
		t.Fatalf("champion = %q, want %q", got, StaticActionID)
	}
	if got := snapshot.FeatureSchema(); !slices.Equal(got, spec.FeatureSchema) {
		t.Fatalf("feature schema = %#v, want %#v", got, spec.FeatureSchema)
	}
	if got := snapshot.Actions(); !snapshotActionsEqual(got, spec.Actions) {
		t.Fatalf("actions = %#v, want %#v", got, spec.Actions)
	}
}

func TestNewPolicySnapshot_HashesCanonicalContent_When_EquivalentInputOrderDiffers(t *testing.T) {
	first := snapshotTestSpec()
	second := snapshotTestSpec()
	second.Scope.TrainingCutoff = second.Scope.TrainingCutoff.In(time.FixedZone("offset", 2*60*60))
	second.Actions[0].Examples[0], second.Actions[0].Examples[1] =
		second.Actions[0].Examples[1], second.Actions[0].Examples[0]
	for actionIndex := range second.Actions {
		for exampleIndex := range second.Actions[actionIndex].Examples {
			slices.Reverse(second.Actions[actionIndex].Examples[exampleIndex].Features)
		}
	}

	firstSnapshot, err := NewPolicySnapshot(first)
	if err != nil {
		t.Fatalf("NewPolicySnapshot(first) error = %v", err)
	}
	secondSnapshot, err := NewPolicySnapshot(second)
	if err != nil {
		t.Fatalf("NewPolicySnapshot(second) error = %v", err)
	}

	if firstSnapshot.ID() != secondSnapshot.ID() {
		t.Fatalf("equivalent snapshot IDs differ: %s != %s", firstSnapshot.ID(), secondSnapshot.ID())
	}
	if firstSnapshot.SHA256() != secondSnapshot.SHA256() {
		t.Fatalf("equivalent snapshot hashes differ: %s != %s", firstSnapshot.SHA256(), secondSnapshot.SHA256())
	}
}

func TestNewPolicySnapshot_HashMatchesTypedCanonicalDocument_When_ContentIsMinimal(t *testing.T) {
	spec := snapshotTestSpec()
	spec.FeatureSchema = spec.FeatureSchema[:1]
	spec.Actions = []SnapshotAction{
		{ActionID: "direct"},
		{ActionID: StaticActionID},
	}
	snapshot, err := NewPolicySnapshot(spec)
	if err != nil {
		t.Fatalf("NewPolicySnapshot() error = %v", err)
	}

	canonical := []byte(
		`{"schema_version":"1.0","scope":{"owner_id":"11111111-1111-4111-8111-111111111111",` +
			`"domain":"reasoning","point":"reasoning","provider_id":"openrouter",` +
			`"model_id":"production-model-v1","policy_version":"reasoning-policy-v1",` +
			`"training_cutoff":"2026-07-24T12:00:00Z"},"champion_action_id":"static",` +
			`"feature_schema":[{"key":"candidate_count","center":1,"scale":1}],` +
			`"actions":[{"action_id":"direct","examples":[]},{"action_id":"static","examples":[]}]}`,
	)
	sum := sha256.Sum256(canonical)
	want := hex.EncodeToString(sum[:])
	if snapshot.SHA256() != want {
		t.Fatalf("snapshot SHA256 = %s, want %s", snapshot.SHA256(), want)
	}
}

func TestNewPolicySnapshot_CanonicalizesUtilitySignedZero_When_HashingContent(t *testing.T) {
	positive := snapshotTestSpec()
	negative := snapshotTestSpec()
	positive.Actions[0].Examples[0].Utility = 0
	negative.Actions[0].Examples[0].Utility = math.Copysign(0, -1)

	positiveSnapshot, err := NewPolicySnapshot(positive)
	if err != nil {
		t.Fatalf("NewPolicySnapshot(positive zero) error = %v", err)
	}
	negativeSnapshot, err := NewPolicySnapshot(negative)
	if err != nil {
		t.Fatalf("NewPolicySnapshot(negative zero) error = %v", err)
	}

	if positiveSnapshot.ID() != negativeSnapshot.ID() {
		t.Fatalf(
			"signed-zero snapshot IDs differ: %s != %s",
			positiveSnapshot.ID(),
			negativeSnapshot.ID(),
		)
	}
	if positiveSnapshot.SHA256() != negativeSnapshot.SHA256() {
		t.Fatalf(
			"signed-zero snapshot hashes differ: %s != %s",
			positiveSnapshot.SHA256(),
			negativeSnapshot.SHA256(),
		)
	}
}

func TestNewPolicySnapshot_RejectsMalformedContent_When_ContractIsUnsafe(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SnapshotSpec)
	}{
		{name: "owner missing", mutate: func(spec *SnapshotSpec) { spec.Scope.OwnerID = uuid.Nil }},
		{name: "unknown domain", mutate: func(spec *SnapshotSpec) { spec.Scope.Domain = "unknown" }},
		{name: "point does not bind domain", mutate: func(spec *SnapshotSpec) {
			spec.Scope.Point = PointToolDiscovery
		}},
		{name: "provider missing", mutate: func(spec *SnapshotSpec) { spec.Scope.ProviderID = "" }},
		{name: "model missing", mutate: func(spec *SnapshotSpec) { spec.Scope.ModelID = "" }},
		{name: "policy version missing", mutate: func(spec *SnapshotSpec) {
			spec.Scope.PolicyVersion = ""
		}},
		{name: "training cutoff missing", mutate: func(spec *SnapshotSpec) {
			spec.Scope.TrainingCutoff = time.Time{}
		}},
		{name: "champion is not static", mutate: func(spec *SnapshotSpec) {
			spec.ChampionActionID = "direct"
		}},
		{name: "static action missing", mutate: func(spec *SnapshotSpec) {
			spec.Actions = spec.Actions[:2]
		}},
		{name: "actions out of order", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0], spec.Actions[1] = spec.Actions[1], spec.Actions[0]
		}},
		{name: "action duplicated", mutate: func(spec *SnapshotSpec) {
			spec.Actions[1].ActionID = spec.Actions[0].ActionID
		}},
		{name: "action contains private alias", mutate: func(spec *SnapshotSpec) {
			spec.Actions[1].ActionID = "query_hash"
		}},
		{name: "feature schema missing", mutate: func(spec *SnapshotSpec) {
			spec.FeatureSchema = nil
		}},
		{name: "feature schema out of order", mutate: func(spec *SnapshotSpec) {
			spec.FeatureSchema[0], spec.FeatureSchema[1] =
				spec.FeatureSchema[1], spec.FeatureSchema[0]
		}},
		{name: "feature duplicated", mutate: func(spec *SnapshotSpec) {
			spec.FeatureSchema[1].Key = spec.FeatureSchema[0].Key
		}},
		{name: "feature unknown", mutate: func(spec *SnapshotSpec) {
			spec.FeatureSchema[0].Key = "mystery"
		}},
		{name: "feature contains content fingerprint", mutate: func(spec *SnapshotSpec) {
			spec.FeatureSchema[0].Key = "query_fingerprint"
		}},
		{name: "feature center nonfinite", mutate: func(spec *SnapshotSpec) {
			spec.FeatureSchema[0].Center = math.NaN()
		}},
		{name: "feature center outside registered range", mutate: func(spec *SnapshotSpec) {
			spec.FeatureSchema[0].Center = -1
		}},
		{name: "feature scale zero", mutate: func(spec *SnapshotSpec) {
			spec.FeatureSchema[0].Scale = 0
		}},
		{name: "feature scale nonfinite", mutate: func(spec *SnapshotSpec) {
			spec.FeatureSchema[0].Scale = math.Inf(1)
		}},
		{name: "declared normalization range overflows", mutate: func(spec *SnapshotSpec) {
			spec.FeatureSchema[0].Scale = math.SmallestNonzeroFloat64
		}},
		{name: "outcome id missing", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].OutcomeID = uuid.Nil
		}},
		{name: "outcome id duplicated across actions", mutate: func(spec *SnapshotSpec) {
			spec.Actions[1].Examples[0].OutcomeID = spec.Actions[0].Examples[0].OutcomeID
		}},
		{name: "example is ineligible", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].Eligible = false
		}},
		{name: "example owner mismatch", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].OwnerID = uuid.MustParse("99999999-9999-4999-8999-999999999999")
		}},
		{name: "example domain mismatch", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].Domain = DomainKnowledge
		}},
		{name: "example point mismatch", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].Point = PointKnowledge
		}},
		{name: "example provider mismatch", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].ProviderID = "llamacpp"
		}},
		{name: "example model mismatch", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].ModelID = "other-model"
		}},
		{name: "example after cutoff", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].ObservedAt = spec.Scope.TrainingCutoff.Add(time.Nanosecond)
		}},
		{name: "utility nonfinite", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].Utility = math.NaN()
		}},
		{name: "utility below range", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].Utility = -0.1
		}},
		{name: "utility above range", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].Utility = 1.1
		}},
		{name: "example missing dimension", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].Features = spec.Actions[0].Examples[0].Features[:1]
		}},
		{name: "example duplicate feature", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].Features[1].Key =
				spec.Actions[0].Examples[0].Features[0].Key
		}},
		{name: "example unknown feature", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].Features[0].Key = "raw_query"
		}},
		{name: "example nonfinite feature", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].Features[0].Value = math.Inf(-1)
		}},
		{name: "example feature outside registered range", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].Features[0].Value = -1
		}},
		{name: "example normalizes to zero vector", mutate: func(spec *SnapshotSpec) {
			spec.Actions[0].Examples[0].Features = []SnapshotFeatureValue{
				{Key: FeatureCandidateCount, Value: 1},
				{Key: FeatureContextLength, Value: 10},
			}
		}},
		{name: "more than 256 eligible examples", mutate: func(spec *SnapshotSpec) {
			example := spec.Actions[0].Examples[0]
			spec.Actions[0].Examples = make([]SnapshotExample, 257)
			for index := range spec.Actions[0].Examples {
				example.OutcomeID = uuid.MustParse(
					"00000000-0000-4000-8000-" + leftPadDecimal(index+1, 12),
				)
				spec.Actions[0].Examples[index] = example
			}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := snapshotTestSpec()
			test.mutate(&spec)
			if _, err := NewPolicySnapshot(spec); err == nil {
				t.Fatal("NewPolicySnapshot() error = nil")
			}
		})
	}
}

func TestPolicySnapshot_PreservesState_When_InputAndAccessorsMutate(t *testing.T) {
	spec := snapshotTestSpec()
	snapshot, err := NewPolicySnapshot(spec)
	if err != nil {
		t.Fatalf("NewPolicySnapshot() error = %v", err)
	}
	wantHash := snapshot.SHA256()
	wantActions := snapshot.Actions()
	wantSchema := snapshot.FeatureSchema()

	spec.FeatureSchema[0].Center = 99
	spec.Actions[0].ActionID = "changed"
	spec.Actions[0].Examples[0].Features[0].Value = 99
	actions := snapshot.Actions()
	actions[0].ActionID = "changed-again"
	actions[0].Examples[0].Features[0].Value = 88
	schema := snapshot.FeatureSchema()
	schema[0].Center = 77

	if snapshot.SHA256() != wantHash {
		t.Fatalf("snapshot hash changed to %s, want %s", snapshot.SHA256(), wantHash)
	}
	if got := snapshot.Actions(); !snapshotActionsEqual(got, wantActions) {
		t.Fatalf("snapshot actions mutated: %#v", got)
	}
	if got := snapshot.FeatureSchema(); !slices.Equal(got, wantSchema) {
		t.Fatalf("snapshot feature schema mutated: %#v", got)
	}
}

func snapshotTestSpec() SnapshotSpec {
	scope := SnapshotScope{
		OwnerID:       uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		Domain:        DomainReasoning,
		Point:         PointReasoning,
		ProviderID:    "openrouter",
		ModelID:       "production-model-v1",
		PolicyVersion: "reasoning-policy-v1",
		TrainingCutoff: time.Date(
			2026, time.July, 24, 12, 0, 0, 0, time.UTC,
		),
	}
	features := []SnapshotFeatureDefinition{
		{Key: FeatureCandidateCount, Center: 1, Scale: 1},
		{Key: FeatureContextLength, Center: 10, Scale: 10},
	}
	return SnapshotSpec{
		Scope:            scope,
		ChampionActionID: StaticActionID,
		FeatureSchema:    features,
		Actions: []SnapshotAction{
			{
				ActionID: "deep",
				Examples: []SnapshotExample{
					snapshotTestExample(
						scope,
						"00000000-0000-4000-8000-000000000001",
						0.9,
						2,
						10,
					),
					snapshotTestExample(
						scope,
						"00000000-0000-4000-8000-000000000002",
						0.2,
						1,
						20,
					),
				},
			},
			{
				ActionID: "direct",
				Examples: []SnapshotExample{
					snapshotTestExample(
						scope,
						"00000000-0000-4000-8000-000000000003",
						0.7,
						2,
						10,
					),
				},
			},
			{ActionID: StaticActionID},
		},
	}
}

func snapshotTestExample(
	scope SnapshotScope,
	outcomeID string,
	utility float64,
	candidates float64,
	contextLength float64,
) SnapshotExample {
	return SnapshotExample{
		OutcomeID:  uuid.MustParse(outcomeID),
		OwnerID:    scope.OwnerID,
		Domain:     scope.Domain,
		Point:      scope.Point,
		ProviderID: scope.ProviderID,
		ModelID:    scope.ModelID,
		ObservedAt: scope.TrainingCutoff.Add(-time.Hour),
		Eligible:   true,
		Utility:    utility,
		Features: []SnapshotFeatureValue{
			{Key: FeatureCandidateCount, Value: candidates},
			{Key: FeatureContextLength, Value: contextLength},
		},
	}
}

func snapshotActionsEqual(left, right []SnapshotAction) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].ActionID != right[index].ActionID ||
			!slices.EqualFunc(
				left[index].Examples,
				right[index].Examples,
				snapshotExamplesEqual,
			) {
			return false
		}
	}
	return true
}

func snapshotExamplesEqual(left, right SnapshotExample) bool {
	return left.OutcomeID == right.OutcomeID &&
		left.OwnerID == right.OwnerID &&
		left.Domain == right.Domain &&
		left.Point == right.Point &&
		left.ProviderID == right.ProviderID &&
		left.ModelID == right.ModelID &&
		left.ObservedAt.Equal(right.ObservedAt) &&
		left.Eligible == right.Eligible &&
		left.Utility == right.Utility &&
		slices.Equal(left.Features, right.Features)
}

func leftPadDecimal(value, width int) string {
	digits := []byte{}
	for {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
		if value == 0 {
			break
		}
	}
	padding := make([]byte, width-len(digits))
	for index := range padding {
		padding[index] = '0'
	}
	return string(append(padding, digits...))
}
