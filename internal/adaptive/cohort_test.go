package adaptive

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func focalCohortTestSpec() CohortSpec {
	quality := EvaluatorRegistration{
		Kind:             EvaluatorDeterministic,
		ID:               "citation-check",
		Version:          "v3",
		RubricID:         "citation-support",
		CalibrationID:    "heldout-2026-07",
		ProvenanceSHA256: strings.Repeat("a1", 32),
	}
	harm := EvaluatorRegistration{
		Kind:             EvaluatorCalibratedJudge,
		ID:               "harm-rubric",
		Version:          "v2",
		RubricID:         "harm-binary",
		CalibrationID:    "heldout-harm-2026-07",
		ProvenanceSHA256: strings.Repeat("b2", 32),
	}
	return CohortSpec{
		Scope: CohortScope{
			OwnerID:        uuid.MustParse("6f1a63d6-8a4e-4c0c-9a3e-2b7d5f0c1e88"),
			ProviderID:     "openrouter",
			ModelID:        "anthropic/claude-opus-5",
			PolicyVersion:  "policy-2026-07-24",
			PolicyEpoch:    7,
			SnapshotID:     uuid.MustParse("2c9b4e11-5d3a-4f77-8e21-90a6c4b3d5f2"),
			SnapshotSHA256: strings.Repeat("c3", 32),
			Environment:    EvaluationProductionCanary,
		},
		Predicate: FocalPredicate{
			Domain:  DomainKnowledge,
			Point:   PointKnowledge,
			Ordinal: 1,
		},
		ExperimentID: "knowledge-depth-2026-07",
		Arms: []CohortArm{
			{ArmID: "baseline", Probability: 0.5},
			{ArmID: "challenger", Probability: 0.5},
		},
		Actions: []ActionProbability{
			{ActionID: "graph_expansion", Probability: 0.25},
			{ActionID: "static", Probability: 0.5},
			{ActionID: "vector_rerank", Probability: 0.25},
		},
		Evaluators:     []EvaluatorRegistration{harm, quality},
		PrimaryQuality: quality.Provenance(),
		PrimaryHarm:    harm.Provenance(),
		Power: CohortPowerPlan{
			BaselineQualityRate:   0.65,
			BaselineHarmRate:      0.01,
			MinimumDetectableLift: 0.08,
			ExpectedMembers:       5000,
			MinimumMembers:        2500,
			MinimumArmSupport:     0.05,
			SimulationSHA256:      strings.Repeat("d4", 32),
		},
		Margins: CohortMargins{
			MinimumQualityUplift: 0.02,
			MaximumHarmIncrease:  0.02,
		},
		Censoring: CohortCensoring{
			MaxCensorRate:      0.1,
			MaxCensorImbalance: 0.05,
		},
		Admission: CohortAdmission{
			BlockKeys:        []BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock},
			TimeBlockSeconds: 3600,
			Interference:     InterferenceNoneBetweenUnits,
		},
		Looks: []CohortLook{
			{Look: 1, Alpha: 0.005},
			{Look: 2, Alpha: 0.005},
			{Look: 3, Alpha: 0.01},
			{Look: 4, Alpha: 0.01},
			{Look: 5, Alpha: 0.02},
		},
		Cutoff: time.Date(2026, time.July, 24, 12, 0, 0, 123_456_000, time.UTC),
	}
}

func mustFocalCohort(t *testing.T, spec CohortSpec) *FocalCohort {
	t.Helper()
	cohort, err := NewFocalCohort(spec)
	if err != nil {
		t.Fatalf("NewFocalCohort() error = %v", err)
	}
	return cohort
}

func TestNewFocalCohort_FreezesCanonicalContent_When_SpecIsValid(t *testing.T) {
	spec := focalCohortTestSpec()
	cohort := mustFocalCohort(t, spec)

	if cohort.ID() == uuid.Nil {
		t.Fatal("cohort ID is nil")
	}
	if !validSHA256(cohort.SHA256()) {
		t.Fatalf("cohort SHA256 = %q, want lowercase SHA-256", cohort.SHA256())
	}
	if !validSHA256(cohort.PredicateSHA256()) {
		t.Fatalf("predicate SHA256 = %q, want lowercase SHA-256", cohort.PredicateSHA256())
	}
	if got := cohort.Scope(); got != spec.Scope {
		t.Fatalf("scope = %#v, want %#v", got, spec.Scope)
	}
	if got := cohort.Predicate(); got != spec.Predicate {
		t.Fatalf("predicate = %#v, want %#v", got, spec.Predicate)
	}
	if got := cohort.Domain(); got != DomainKnowledge {
		t.Fatalf("domain = %q, want %q", got, DomainKnowledge)
	}
	if got := cohort.ExperimentID(); got != spec.ExperimentID {
		t.Fatalf("experiment = %q, want %q", got, spec.ExperimentID)
	}
	if got := cohort.Arms(); !slices.Equal(got, spec.Arms) {
		t.Fatalf("arms = %#v, want %#v", got, spec.Arms)
	}
	if got := cohort.Actions(); !slices.Equal(got, spec.Actions) {
		t.Fatalf("actions = %#v, want %#v", got, spec.Actions)
	}
	if got := cohort.Looks(); !slices.Equal(got, spec.Looks) {
		t.Fatalf("looks = %#v, want %#v", got, spec.Looks)
	}
	if got := cohort.Power(); got != spec.Power {
		t.Fatalf("power = %#v, want %#v", got, spec.Power)
	}
	if got := cohort.Margins(); got != spec.Margins {
		t.Fatalf("margins = %#v, want %#v", got, spec.Margins)
	}
	if got := cohort.Censoring(); got != spec.Censoring {
		t.Fatalf("censoring = %#v, want %#v", got, spec.Censoring)
	}
	if got := cohort.PrimaryQualityEvaluator(); got != spec.PrimaryQuality {
		t.Fatalf("primary quality = %#v, want %#v", got, spec.PrimaryQuality)
	}
	if got := cohort.PrimaryHarmEvaluator(); got != spec.PrimaryHarm {
		t.Fatalf("primary harm = %#v, want %#v", got, spec.PrimaryHarm)
	}
	admission := cohort.Admission()
	if admission.Interference != InterferenceNoneBetweenUnits ||
		admission.TimeBlockSeconds != 3600 ||
		!slices.Equal(admission.BlockKeys, spec.Admission.BlockKeys) {
		t.Fatalf("admission = %#v, want %#v", admission, spec.Admission)
	}
	if got := cohort.Evaluators(); !slices.Equal(got, spec.Evaluators) {
		t.Fatalf("evaluators = %#v, want %#v", got, spec.Evaluators)
	}
	if got := cohort.Cutoff(); !got.Equal(spec.Cutoff) || got.Location() != time.UTC {
		t.Fatalf("cutoff = %s, want %s in UTC", got, spec.Cutoff)
	}
}

func TestNewFocalCohort_CanonicalizesCutoff_When_PrecisionOrZoneDiffers(t *testing.T) {
	base := time.Date(2026, time.July, 24, 12, 0, 0, 123_456_000, time.UTC)
	first := focalCohortTestSpec()
	first.Cutoff = base.Add(123 * time.Nanosecond).In(time.FixedZone("offset", 2*60*60))
	second := focalCohortTestSpec()
	second.Cutoff = base.Add(999 * time.Nanosecond)

	firstCohort := mustFocalCohort(t, first)
	secondCohort := mustFocalCohort(t, second)

	if got := firstCohort.Cutoff(); got != base {
		t.Fatalf("cutoff = %s, want PostgreSQL precision %s", got, base)
	}
	if firstCohort.ID() != secondCohort.ID() || firstCohort.SHA256() != secondCohort.SHA256() {
		t.Fatal("sub-microsecond-equivalent cutoffs changed canonical identity")
	}
}

func TestNewFocalCohort_DerivesDeterministicIdentity_When_ContentChanges(t *testing.T) {
	cohort := mustFocalCohort(t, focalCohortTestSpec())
	repeat := mustFocalCohort(t, focalCohortTestSpec())
	if cohort.ID() != repeat.ID() || cohort.SHA256() != repeat.SHA256() {
		t.Fatal("identical specs produced different cohort identities")
	}
	if cohort.PredicateSHA256() != repeat.PredicateSHA256() {
		t.Fatal("identical predicates produced different predicate hashes")
	}

	otherScope := focalCohortTestSpec()
	otherScope.Scope.PolicyEpoch = 8
	otherScopeCohort := mustFocalCohort(t, otherScope)
	if otherScopeCohort.ID() == cohort.ID() || otherScopeCohort.SHA256() == cohort.SHA256() {
		t.Fatal("changed policy epoch did not change cohort identity")
	}
	if otherScopeCohort.PredicateSHA256() != cohort.PredicateSHA256() {
		t.Fatal("predicate hash must depend only on the focal predicate")
	}

	otherOrdinal := focalCohortTestSpec()
	otherOrdinal.Predicate.Ordinal = 2
	otherOrdinalCohort := mustFocalCohort(t, otherOrdinal)
	if otherOrdinalCohort.PredicateSHA256() == cohort.PredicateSHA256() {
		t.Fatal("changed focal ordinal did not change the predicate hash")
	}
	if otherOrdinalCohort.ID() == cohort.ID() {
		t.Fatal("changed focal ordinal did not change cohort identity")
	}
}

func TestFocalCohort_MatchesFocalPoint_When_DomainPointAndOrdinalAreExact(t *testing.T) {
	cohort := mustFocalCohort(t, focalCohortTestSpec())

	if !cohort.MatchesFocalPoint(DomainKnowledge, PointKnowledge, 1) {
		t.Fatal("exact focal point did not match")
	}
	for _, testCase := range []struct {
		name    string
		domain  Domain
		point   DecisionPoint
		ordinal uint32
	}{
		{name: "other ordinal", domain: DomainKnowledge, point: PointKnowledge, ordinal: 2},
		{name: "other point", domain: DomainKnowledge, point: PointReasoning, ordinal: 1},
		{name: "other domain", domain: DomainReasoning, point: PointKnowledge, ordinal: 1},
	} {
		if cohort.MatchesFocalPoint(testCase.domain, testCase.point, testCase.ordinal) {
			t.Fatalf("%s matched the frozen focal predicate", testCase.name)
		}
	}
}

func TestNewFocalCohort_ReturnsDefensiveCopies_When_CallersMutate(t *testing.T) {
	spec := focalCohortTestSpec()
	cohort := mustFocalCohort(t, spec)

	spec.Arms[0].Probability = 0.9
	spec.Actions[0].ActionID = "mutated"
	spec.Evaluators[0].ID = "mutated"
	spec.Looks[0].Alpha = 0.04
	spec.Admission.BlockKeys[0] = BlockOwner

	if cohort.Arms()[0].Probability != 0.5 {
		t.Fatal("cohort retained a caller-mutated arm probability")
	}
	if cohort.Actions()[0].ActionID != "graph_expansion" {
		t.Fatal("cohort retained a caller-mutated action ID")
	}
	if cohort.Evaluators()[0].ID != "harm-rubric" {
		t.Fatal("cohort retained a caller-mutated evaluator ID")
	}
	if cohort.Looks()[0].Alpha != 0.005 {
		t.Fatal("cohort retained a caller-mutated alpha allocation")
	}
	if cohort.Admission().BlockKeys[0] != BlockEpisode {
		t.Fatal("cohort retained a caller-mutated block key")
	}

	cohort.Arms()[0].ArmID = "mutated"
	cohort.Actions()[0].Probability = 0.9
	cohort.Evaluators()[0].Version = "mutated"
	cohort.Looks()[0].Look = 9
	cohort.Admission().BlockKeys[0] = BlockOwner

	if cohort.Arms()[0].ArmID != "baseline" ||
		cohort.Actions()[0].Probability != 0.25 ||
		cohort.Evaluators()[0].Version != "v2" ||
		cohort.Looks()[0].Look != 1 ||
		cohort.Admission().BlockKeys[0] != BlockEpisode {
		t.Fatal("accessor returned an aliased view of frozen cohort state")
	}
}

func TestFocalCohort_NilReceiverReturnsZeroValues(t *testing.T) {
	var cohort *FocalCohort

	if cohort.ID() != uuid.Nil || cohort.SHA256() != "" || cohort.PredicateSHA256() != "" {
		t.Fatal("nil cohort claimed an identity")
	}
	if cohort.Domain() != "" || cohort.ExperimentID() != "" {
		t.Fatal("nil cohort claimed a scope")
	}
	if cohort.Arms() != nil || cohort.Actions() != nil ||
		cohort.Evaluators() != nil || cohort.Looks() != nil ||
		cohort.Admission().BlockKeys != nil {
		t.Fatal("nil cohort returned catalog entries")
	}
	if cohort.MatchesFocalPoint(DomainKnowledge, PointKnowledge, 1) {
		t.Fatal("nil cohort matched a focal point")
	}
	if !cohort.Cutoff().IsZero() {
		t.Fatal("nil cohort claimed a cutoff")
	}
}

func TestNewFocalCohort_RejectsInvalidSpecifications(t *testing.T) {
	unregistered := EvaluatorRegistration{
		Kind:             EvaluatorHuman,
		ID:               "unlisted",
		Version:          "v1",
		RubricID:         "rubric",
		CalibrationID:    "calibration",
		ProvenanceSHA256: strings.Repeat("e5", 32),
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*CohortSpec)
	}{
		{"missing owner", func(s *CohortSpec) { s.Scope.OwnerID = uuid.Nil }},
		{"invalid provider", func(s *CohortSpec) { s.Scope.ProviderID = "" }},
		{"invalid model", func(s *CohortSpec) { s.Scope.ModelID = "model with spaces" }},
		{"invalid policy version", func(s *CohortSpec) { s.Scope.PolicyVersion = "" }},
		{"missing policy epoch", func(s *CohortSpec) { s.Scope.PolicyEpoch = 0 }},
		{"missing snapshot", func(s *CohortSpec) { s.Scope.SnapshotID = uuid.Nil }},
		{"invalid snapshot hash", func(s *CohortSpec) { s.Scope.SnapshotSHA256 = "ABC" }},
		{"offline environment", func(s *CohortSpec) { s.Scope.Environment = EvaluationOffline }},
		{"invalid domain", func(s *CohortSpec) { s.Predicate.Domain = "wat" }},
		{"invalid point", func(s *CohortSpec) { s.Predicate.Point = "wat" }},
		{"domain point mismatch", func(s *CohortSpec) { s.Predicate.Point = PointReasoning }},
		{"invalid experiment", func(s *CohortSpec) { s.ExperimentID = "" }},
		{"sensitive experiment", func(s *CohortSpec) { s.ExperimentID = "api-key-sweep" }},
		{"single arm", func(s *CohortSpec) {
			s.Arms = []CohortArm{{ArmID: "baseline", Probability: 1}}
		}},
		{"unordered arms", func(s *CohortSpec) { slices.Reverse(s.Arms) }},
		{"duplicate arm", func(s *CohortSpec) { s.Arms[1].ArmID = s.Arms[0].ArmID }},
		{"unsupported arm", func(s *CohortSpec) {
			s.Arms[0].Probability, s.Arms[1].Probability = 0, 1
		}},
		{"arm probabilities do not total one", func(s *CohortSpec) { s.Arms[0].Probability = 0.4 }},
		{"invalid arm ID", func(s *CohortSpec) { s.Arms[0].ArmID = "Baseline" }},
		{"sensitive arm ID", func(s *CohortSpec) { s.Arms[0].ArmID = "bearer-arm" }},
		{"single action", func(s *CohortSpec) {
			s.Actions = []ActionProbability{{ActionID: StaticActionID, Probability: 1}}
		}},
		{"unordered actions", func(s *CohortSpec) { slices.Reverse(s.Actions) }},
		{"duplicate action", func(s *CohortSpec) { s.Actions[1].ActionID = s.Actions[0].ActionID }},
		{"missing static action", func(s *CohortSpec) { s.Actions[1].ActionID = "sparse" }},
		{"none action", func(s *CohortSpec) { s.Actions[0].ActionID = ActionNoneID }},
		{"unsupported action", func(s *CohortSpec) {
			s.Actions[0].Probability, s.Actions[1].Probability = 0, 0.75
		}},
		{"action probabilities do not total one", func(s *CohortSpec) {
			s.Actions[0].Probability = 0.1
		}},
		{"invalid action ID", func(s *CohortSpec) { s.Actions[0].ActionID = "Graph" }},
		{"sensitive action ID", func(s *CohortSpec) { s.Actions[0].ActionID = "content-dump" }},
		{"no evaluators", func(s *CohortSpec) { s.Evaluators = nil }},
		{"unordered evaluators", func(s *CohortSpec) { slices.Reverse(s.Evaluators) }},
		{"duplicate evaluator", func(s *CohortSpec) { s.Evaluators[1] = s.Evaluators[0] }},
		{"ineligible evaluator", func(s *CohortSpec) { s.Evaluators[0].Kind = EvaluatorSelfReport }},
		{"invalid evaluator hash", func(s *CohortSpec) { s.Evaluators[0].ProvenanceSHA256 = "nope" }},
		{"unregistered primary quality", func(s *CohortSpec) {
			s.PrimaryQuality = unregistered.Provenance()
		}},
		{"unregistered primary harm", func(s *CohortSpec) {
			s.PrimaryHarm = unregistered.Provenance()
		}},
		{"baseline quality out of range", func(s *CohortSpec) { s.Power.BaselineQualityRate = 1.5 }},
		{"baseline harm out of range", func(s *CohortSpec) { s.Power.BaselineHarmRate = -0.1 }},
		{"no detectable lift", func(s *CohortSpec) { s.Power.MinimumDetectableLift = 0 }},
		{"no expected members", func(s *CohortSpec) { s.Power.ExpectedMembers = 0 }},
		{"minimum exceeds expected members", func(s *CohortSpec) { s.Power.MinimumMembers = 5001 }},
		{"no minimum members", func(s *CohortSpec) { s.Power.MinimumMembers = 0 }},
		{"no arm support", func(s *CohortSpec) { s.Power.MinimumArmSupport = 0 }},
		{"invalid simulation hash", func(s *CohortSpec) { s.Power.SimulationSHA256 = "" }},
		{"negative quality margin", func(s *CohortSpec) { s.Margins.MinimumQualityUplift = -0.1 }},
		{"saturated harm margin", func(s *CohortSpec) { s.Margins.MaximumHarmIncrease = 1 }},
		{"saturated censor rate", func(s *CohortSpec) { s.Censoring.MaxCensorRate = 1 }},
		{"negative censor imbalance", func(s *CohortSpec) { s.Censoring.MaxCensorImbalance = -0.1 }},
		{"no block keys", func(s *CohortSpec) { s.Admission.BlockKeys = nil }},
		{"unordered block keys", func(s *CohortSpec) { slices.Reverse(s.Admission.BlockKeys) }},
		{"duplicate block key", func(s *CohortSpec) {
			s.Admission.BlockKeys = []BlockKey{BlockOwner, BlockOwner}
		}},
		{"unregistered block key", func(s *CohortSpec) {
			s.Admission.BlockKeys = []BlockKey{"tenant"}
		}},
		{"missing time block width", func(s *CohortSpec) { s.Admission.TimeBlockSeconds = 0 }},
		{"unused time block width", func(s *CohortSpec) {
			s.Admission.BlockKeys = []BlockKey{BlockOwner}
		}},
		{"unmodelled interference", func(s *CohortSpec) {
			s.Admission.Interference = InterferenceUnmodelled
		}},
		{"invalid interference", func(s *CohortSpec) { s.Admission.Interference = "assumed" }},
		{"four looks", func(s *CohortSpec) { s.Looks = s.Looks[:4] }},
		{"six looks", func(s *CohortSpec) {
			s.Looks = append(s.Looks, CohortLook{Look: 6, Alpha: 0.001})
		}},
		{"unordered looks", func(s *CohortSpec) { slices.Reverse(s.Looks) }},
		{"zero alpha allocation", func(s *CohortSpec) { s.Looks[0].Alpha = 0 }},
		{"alpha budget exceeded", func(s *CohortSpec) { s.Looks[4].Alpha = 0.031 }},
		{"missing cutoff", func(s *CohortSpec) { s.Cutoff = time.Time{} }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			spec := focalCohortTestSpec()
			testCase.mutate(&spec)
			if _, err := NewFocalCohort(spec); err == nil {
				t.Fatalf("NewFocalCohort() accepted %s", testCase.name)
			}
		})
	}
}

func TestNewFocalCohort_AcceptsExactAlphaBudget(t *testing.T) {
	spec := focalCohortTestSpec()
	spec.Looks = []CohortLook{
		{Look: 1, Alpha: 0.01},
		{Look: 2, Alpha: 0.01},
		{Look: 3, Alpha: 0.01},
		{Look: 4, Alpha: 0.01},
		{Look: 5, Alpha: 0.01},
	}

	cohort := mustFocalCohort(t, spec)

	total := 0.0
	for _, look := range cohort.Looks() {
		total += look.Alpha
	}
	if total > CohortTotalAlpha {
		t.Fatalf("frozen alpha budget = %g, want at most %g", total, CohortTotalAlpha)
	}
	if len(cohort.Looks()) != CohortPlannedLooks {
		t.Fatalf("planned looks = %d, want %d", len(cohort.Looks()), CohortPlannedLooks)
	}
}
