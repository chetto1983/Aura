package adaptive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFocalCohort_CanonicalArtifactMatchesIdentity(t *testing.T) {
	cohort := mustFocalCohort(t, focalCohortTestSpec())

	artifact, err := cohort.canonicalArtifact()
	if err != nil {
		t.Fatalf("canonicalArtifact() error = %v", err)
	}
	sum := sha256.Sum256(artifact)
	if got := hex.EncodeToString(sum[:]); got != cohort.SHA256() {
		t.Fatalf("artifact SHA256 = %s, want %s", got, cohort.SHA256())
	}

	var document map[string]json.RawMessage
	if err := json.Unmarshal(artifact, &document); err != nil {
		t.Fatalf("decode canonical artifact: %v", err)
	}
	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	wantKeys := []string{
		"actions", "admission", "arms", "censoring", "cutoff", "evaluators",
		"experiment_id", "looks", "margins", "power", "predicate",
		"primary_harm", "primary_quality", "schema_version", "scope",
	}
	if !slices.Equal(keys, wantKeys) {
		t.Fatalf("artifact keys = %v, want %v", keys, wantKeys)
	}

	var schemaVersion string
	if err := json.Unmarshal(document["schema_version"], &schemaVersion); err != nil {
		t.Fatalf("decode schema_version: %v", err)
	}
	if schemaVersion != CohortSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", schemaVersion, CohortSchemaVersion)
	}
	if _, ok := document["Scope"]; ok {
		t.Fatal("artifact exposed Go field names instead of canonical JSON names")
	}
}

func TestFocalCohort_CanonicalArtifactRejectsUninitializedCohort(t *testing.T) {
	var cohort FocalCohort

	if _, err := cohort.canonicalArtifact(); err == nil {
		t.Fatal("zero-value cohort produced a canonical artifact")
	}
}

func TestFocalCohort_CanonicalArtifactRejectsIdentityMismatch(t *testing.T) {
	cohort := mustFocalCohort(t, focalCohortTestSpec())
	cohort.spec.ExperimentID = "mutated_experiment"

	if _, err := cohort.canonicalArtifact(); err == nil {
		t.Fatal("identity-mismatched cohort produced a canonical artifact")
	}
}

func TestRandomizedFocalCohortCanonicalV2RoundTrip(t *testing.T) {
	cohort, _ := randomizedFocalCohortUnitFixture(t)
	artifact, err := cohort.canonicalArtifact()
	if err != nil {
		t.Fatal(err)
	}
	planArtifact, err := cohort.canonicalRandomizationPlanArtifact()
	if err != nil {
		t.Fatal(err)
	}
	var document canonicalCohortV2
	if err := decodeStrictJSON(artifact, &document); err != nil {
		t.Fatal(err)
	}
	var plan canonicalRandomizationPlan
	if err := decodeStrictJSON(planArtifact, &plan); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := newFocalCohortV2(document, plan)
	if err != nil {
		t.Fatal(err)
	}
	roundTripArtifact, err := roundTrip.canonicalArtifact()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roundTripArtifact, artifact) ||
		roundTrip.ID() != cohort.ID() || roundTrip.SHA256() != cohort.SHA256() {
		t.Fatal("cohort v2 canonical round trip changed identity or bytes")
	}
	if document.SchemaID != CohortSchemaIDV2 || document.Revision != 1 ||
		document.RandomizationPlanRef.ArtifactSHA256 != sha256Hex(planArtifact) {
		t.Fatalf("cohort v2 binding = %#v", document.RandomizationPlanRef)
	}
	if got, want := document.Power.BaselineQualityRate.String(), "13/20"; got != want {
		t.Fatalf("baseline quality rational = %s, want %s", got, want)
	}
}

func TestRandomizedFocalCohortRejectsMissingOrTamperedChildBinding(t *testing.T) {
	cohort, _ := randomizedFocalCohortUnitFixture(t)
	tests := []struct {
		name   string
		mutate func(*canonicalCohortV2, *canonicalRandomizationPlan)
	}{
		{
			name: "missing look reference",
			mutate: func(document *canonicalCohortV2, _ *canonicalRandomizationPlan) {
				document.LookPlanRef = ChildArtifactRef{}
			},
		},
		{
			name: "tampered randomization hash",
			mutate: func(document *canonicalCohortV2, _ *canonicalRandomizationPlan) {
				document.RandomizationPlanRef.ArtifactSHA256 = strings.Repeat("f", 64)
			},
		},
		{
			name: "tampered randomization plan",
			mutate: func(document *canonicalCohortV2, plan *canonicalRandomizationPlan) {
				plan.BitIndex = 1
				document.RandomizationPlanRef.ArtifactSHA256 = mustCanonicalHash(t, *plan)
			},
		},
		{
			name: "wrong schema",
			mutate: func(document *canonicalCohortV2, _ *canonicalRandomizationPlan) {
				document.SchemaID = "aura.adaptive.cohort/v3"
			},
		},
		{
			name: "noncanonical owner",
			mutate: func(document *canonicalCohortV2, _ *canonicalRandomizationPlan) {
				document.OwnerID = strings.ToUpper(document.OwnerID)
			},
		},
		{
			name: "predicate hash",
			mutate: func(document *canonicalCohortV2, _ *canonicalRandomizationPlan) {
				document.PredicateSHA256 = strings.Repeat("0", 64)
			},
		},
		{
			name: "duplicate supported action",
			mutate: func(document *canonicalCohortV2, _ *canonicalRandomizationPlan) {
				document.SupportedActions[1] = document.SupportedActions[0]
			},
		},
		{
			name: "nonfinite metric projection",
			mutate: func(document *canonicalCohortV2, _ *canonicalRandomizationPlan) {
				numerator := new(big.Int).Exp(big.NewInt(10), big.NewInt(400), nil)
				document.Power.BaselineQualityRate, _ = NewExactRational(
					numerator, big.NewInt(1),
				)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := cohort.v2.document
			document.SupportedActions = slices.Clone(document.SupportedActions)
			plan := cohort.v2.randomizationPlan
			plan.Arms = slices.Clone(plan.Arms)
			test.mutate(&document, &plan)
			if _, err := newFocalCohortV2(document, plan); err == nil {
				t.Fatal("newFocalCohortV2() accepted invalid child binding")
			}
		})
	}
}

func TestNewRandomizedFocalCohortRejectsInvalidInputs(t *testing.T) {
	cohort, snapshot := randomizedFocalCohortUnitFixture(t)
	if _, err := NewRandomizedFocalCohort(
		cohort.spec, nil, validCohortV2ChildRefs(),
	); err == nil {
		t.Fatal("NewRandomizedFocalCohort() accepted nil snapshot")
	}
	mismatched := cohort.spec
	mismatched.Scope.SnapshotID = uuid.New()
	if _, err := NewRandomizedFocalCohort(
		mismatched, snapshot, validCohortV2ChildRefs(),
	); err == nil {
		t.Fatal("NewRandomizedFocalCohort() accepted mismatched snapshot")
	}
	arms := cohort.spec
	arms.Arms = slices.Clone(arms.Arms)
	arms.Arms[0].Probability = 0.6
	arms.Arms[1].Probability = 0.4
	if _, err := NewRandomizedFocalCohort(
		arms, snapshot, validCohortV2ChildRefs(),
	); err == nil {
		t.Fatal("NewRandomizedFocalCohort() accepted non-frozen arms")
	}
	metrics := cohort.spec
	metrics.Power.BaselineQualityRate = math.NaN()
	if _, _, _, err := canonicalCohortMetricsV2(metrics); err == nil {
		t.Fatal("canonicalCohortMetricsV2() accepted NaN")
	}
	legacy := mustFocalCohort(t, focalCohortTestSpec())
	if _, err := legacy.canonicalRandomizationPlanArtifact(); err == nil {
		t.Fatal("legacy cohort emitted a randomization plan")
	}
}

func TestCanonicalV2DecodersRejectNoncanonicalJSON(t *testing.T) {
	cohort, _ := randomizedFocalCohortUnitFixture(t)
	artifact, err := cohort.canonicalArtifact()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cohort.canonicalRandomizationPlanArtifact()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeCanonicalCohortV2(append([]byte(" "), artifact...)); err == nil {
		t.Fatal("decodeCanonicalCohortV2() accepted leading whitespace")
	}
	if _, err := decodeCanonicalRandomizationPlan(
		append([]byte(" "), plan...),
	); err == nil {
		t.Fatal("decodeCanonicalRandomizationPlan() accepted leading whitespace")
	}
	if jsonDocumentsEqual([]byte(`{"a":1}`), []byte(`{"a":1} trailing`)) {
		t.Fatal("jsonDocumentsEqual() accepted trailing data")
	}
	if jsonDocumentsEqual([]byte(`{`), []byte(`{}`)) {
		t.Fatal("jsonDocumentsEqual() accepted malformed JSON")
	}
	forged := *cohort
	state := *cohort.v2
	state.document.SchemaID = "aura.adaptive.cohort/v3"
	forged.v2 = &state
	if _, err := forged.canonicalRandomizationPlanArtifact(); err == nil {
		t.Fatal("canonicalRandomizationPlanArtifact() accepted forged v2 state")
	}
}

func mustCanonicalHash(t *testing.T, value any) string {
	t.Helper()
	hash, err := hashCanonical(value)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func randomizedFocalCohortUnitFixture(t *testing.T) (*FocalCohort, *PolicySnapshot) {
	t.Helper()
	snapshotSpec := snapshotTestSpec()
	snapshotSpec.Scope.OwnerID = uuid.MustParse("6f1a63d6-8a4e-4c0c-9a3e-2b7d5f0c1e88")
	snapshotSpec.Scope.Domain = DomainKnowledge
	snapshotSpec.Scope.Point = PointKnowledge
	for actionIndex := range snapshotSpec.Actions {
		for exampleIndex := range snapshotSpec.Actions[actionIndex].Examples {
			example := &snapshotSpec.Actions[actionIndex].Examples[exampleIndex]
			example.OwnerID = snapshotSpec.Scope.OwnerID
			example.Domain = DomainKnowledge
			example.Point = PointKnowledge
		}
	}
	snapshot, err := NewPolicySnapshot(snapshotSpec)
	if err != nil {
		t.Fatal(err)
	}
	spec := focalCohortTestSpec()
	spec.Scope.OwnerID = snapshotSpec.Scope.OwnerID
	spec.Scope.ProviderID = snapshotSpec.Scope.ProviderID
	spec.Scope.ModelID = snapshotSpec.Scope.ModelID
	spec.Scope.PolicyVersion = snapshotSpec.Scope.PolicyVersion
	spec.Scope.SnapshotID = snapshot.ID()
	spec.Scope.SnapshotSHA256 = snapshot.SHA256()
	spec.Predicate = FocalPredicate{
		Domain: snapshotSpec.Scope.Domain, Point: snapshotSpec.Scope.Point, Ordinal: 1,
	}
	spec.Actions = make([]ActionProbability, len(snapshot.Actions()))
	for index, action := range snapshot.Actions() {
		probability := 0.25
		if action.ActionID == StaticActionID {
			probability = 0.5
		}
		spec.Actions[index] = ActionProbability{
			ActionID: action.ActionID, Probability: probability,
		}
	}
	spec.Cutoff = time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	cohort, err := NewRandomizedFocalCohort(spec, snapshot, validCohortV2ChildRefs())
	if err != nil {
		t.Fatal(err)
	}
	return cohort, snapshot
}

func validCohortV2ChildRefs() CohortV2ChildRefs {
	ref := func(kind, schema string, index byte) ChildArtifactRef {
		return ChildArtifactRef{
			SchemaID: ChildArtifactRefSchemaID, Revision: 1, Kind: kind,
			ArtifactSchemaID: schema, ArtifactRevision: 1,
			ArtifactID: uuid.NewSHA1(
				uuid.MustParse("2cd1fd7f-2fde-47ce-a6e1-cbc7a725a994"),
				[]byte{index},
			).String(),
			ArtifactSHA256: strings.Repeat(cohortHexByte(index), 32),
		}
	}
	return CohortV2ChildRefs{
		InterferencePlan: ref(
			"interference_plan", "aura.adaptive.interference-plan/v1", 0x11,
		),
		LookPlan: ref("look_plan", "aura.adaptive.look-plan/utc-cutoff/v1", 0x22),
		QualityOutcomeModel: ref(
			"quality_outcome_model", "aura.adaptive.outcome-model/action-mean/v1", 0x33,
		),
		HarmOutcomeModel: ref(
			"harm_outcome_model", "aura.adaptive.outcome-model/action-mean/v1", 0x44,
		),
		OPEPlan: ref("ope_plan", "aura.adaptive.ope-plan/v1", 0x55),
	}
}

func cohortHexByte(value byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[value>>4], digits[value&0x0f]})
}
