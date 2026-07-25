package adaptive

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestReconstructCohortLedgerOpen(t *testing.T) {
	spec := focalCohortTestSpec()
	spec.Cutoff = time.Now().UTC().Add(time.Hour)
	cohort := mustFocalCohort(t, spec)

	ledger, err := reconstructCohortLedger(cohort, cohort.Cutoff().Add(-time.Nanosecond), nil, nil)
	if err != nil {
		t.Fatalf("reconstructCohortLedger() error = %v", err)
	}
	if ledger.State != CohortLedgerOpen || ledger.SHA256 != "" || ledger.Sealable {
		t.Fatalf("open ledger = %#v, want unsealable result without hash", ledger)
	}
}

func TestReconstructCohortLedgerClosedCensoredMember(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)

	ledger, err := reconstructCohortLedger(
		cohort,
		cohort.Cutoff(),
		[]FocalClaim{claim},
		[]cohortLedgerFact{assignmentFact},
	)
	if err != nil {
		t.Fatalf("reconstructCohortLedger() error = %v", err)
	}
	if ledger.State != CohortLedgerClosed || ledger.SHA256 == "" || ledger.Sealable {
		t.Fatalf("closed ledger = %#v, want hashed unsealable closed result", ledger)
	}
	if len(ledger.Members) != 1 || len(ledger.IncludedFacts) != 1 {
		t.Fatalf("members/facts = %d/%d, want 1/1", len(ledger.Members), len(ledger.IncludedFacts))
	}
	member := ledger.Members[0]
	gotAssignment, gotErr := NewAssignmentEvent(member.Assignment)
	wantAssignment, wantErr := NewAssignmentEvent(assignmentFact.assignment)
	if gotErr != nil || wantErr != nil || gotAssignment.ID != wantAssignment.ID || string(gotAssignment.Payload) != string(wantAssignment.Payload) {
		t.Fatalf("assignment = %#v, want %#v", member.Assignment, assignmentFact.assignment)
	}
	if member.Claim != claim || member.Observed ||
		!member.ITTEligible || member.OPEEligible || member.Delivery != nil || member.Quality != nil || member.Harm != nil {
		t.Fatalf("censored member = %#v", member)
	}
	wantReasons := []string{"missing_delivery", "missing_primary_harm", "missing_primary_quality", "ope_ineligible"}
	if !slices.Equal(member.ReasonCodes, wantReasons) {
		t.Fatalf("reason codes = %#v, want %#v", member.ReasonCodes, wantReasons)
	}
}

func TestReconstructCohortLedgerUsesSuccessfulExposureForOPE(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	facts := append([]cohortLedgerFact{assignmentFact}, successfulCohortOutcomeFacts(t, cohort, assignmentFact)...)

	ledger, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, facts)
	if err != nil {
		t.Fatalf("reconstructCohortLedger() error = %v", err)
	}
	member := ledger.Members[0]
	if !member.Observed || !member.ITTEligible || !member.OPEEligible || member.Delivery == nil ||
		member.Quality == nil || member.Harm == nil || len(member.ReasonCodes) != 0 {
		t.Fatalf("observed member = %#v, want ITT/OPE-eligible successful exposure", member)
	}
	if member.Delivery.Status != DeliverySuccess || !member.Delivery.ExposureKnown ||
		member.Delivery.ExposureProbability == nil || *member.Delivery.ExposureProbability != 0.25 ||
		member.Delivery.ActualActionID != member.Assignment.IntendedActionID {
		t.Fatalf("delivery projection = %#v", member.Delivery)
	}
}

func TestReconstructCohortLedgerSeparatesLateFactsAndNullRecordTime(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	baseline, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, []cohortLedgerFact{assignmentFact})
	if err != nil {
		t.Fatalf("baseline reconstructCohortLedger() error = %v", err)
	}
	late := successfulCohortOutcomeFacts(t, cohort, assignmentFact)
	late[0].recordedAt = cohort.Cutoff()
	late[1] = cohortOutcomeFact(t, cohort, assignmentFact.assignment, cohort.PrimaryQualityEndpoint().Evaluator, cohort.Cutoff(), cohort.Cutoff().Add(-time.Minute))
	ledger, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, append([]cohortLedgerFact{assignmentFact}, late[:2]...))
	if err != nil {
		t.Fatalf("late reconstructCohortLedger() error = %v", err)
	}
	if ledger.LateFactCount != 2 || ledger.SHA256 != baseline.SHA256 || ledger.Members[0].Delivery != nil || ledger.Members[0].Quality != nil {
		t.Fatalf("late ledger = %#v, want closed-hash-stable excluded facts", ledger)
	}
	nullRecordedAt := assignmentFact
	nullRecordedAt.recordedAt = time.Time{}
	_, err = reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, []cohortLedgerFact{nullRecordedAt})
	if !errors.Is(err, ErrInvalidCohortLedger) {
		t.Fatalf("NULL recorded_at error = %v, want ErrInvalidCohortLedger", err)
	}
}

func TestReconstructCohortLedgerRejectsClosedViewPoison(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	tests := []struct {
		name   string
		claims []FocalClaim
		facts  []cohortLedgerFact
		mutate func(*FocalCohort)
	}{
		{
			name:   "payload hash",
			claims: []FocalClaim{claim},
			facts: func() []cohortLedgerFact {
				poisoned := assignmentFact
				poisoned.event.PayloadHash = []byte("invalid")
				return []cohortLedgerFact{poisoned}
			}(),
		},
		{
			name: "claim assignment mismatch",
			claims: func() []FocalClaim {
				poisoned := claim
				poisoned.AssignmentID = uuid.Must(uuid.NewV7())
				return []FocalClaim{poisoned}
			}(),
			facts: []cohortLedgerFact{assignmentFact},
		},
		{
			name:   "assignment scope mismatch",
			claims: []FocalClaim{claim},
			facts: func() []cohortLedgerFact {
				poisoned := assignmentFact
				poisoned.assignment.ProviderID = "other-provider"
				event, err := NewAssignmentEvent(poisoned.assignment)
				if err != nil {
					t.Fatal(err)
				}
				poisoned.event = event
				return []cohortLedgerFact{poisoned}
			}(),
		},
		{
			name:   "expected member overflow",
			claims: []FocalClaim{claim, claim},
			facts:  []cohortLedgerFact{assignmentFact},
			mutate: func(cohort *FocalCohort) { cohort.spec.Power.ExpectedMembers = 1 },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := *cohort
			if test.mutate != nil {
				test.mutate(&candidate)
			}
			_, err := reconstructCohortLedger(&candidate, candidate.Cutoff(), test.claims, test.facts)
			if !errors.Is(err, ErrInvalidCohortLedger) {
				t.Fatalf("reconstructCohortLedger() error = %v, want ErrInvalidCohortLedger", err)
			}
		})
	}
}

func TestReconstructCohortLedgerRejectsPrimaryAndCorrectionGraphPoison(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	base := append([]cohortLedgerFact{assignmentFact}, successfulCohortOutcomeFacts(t, cohort, assignmentFact)...)
	qualityRoot := base[2]
	tests := []struct {
		name  string
		facts []cohortLedgerFact
	}{
		{
			name: "duplicate primary root",
			facts: append(append([]cohortLedgerFact{}, base...), cohortOutcomeFact(
				t, cohort, assignmentFact.assignment, cohort.PrimaryQualityEndpoint().Evaluator,
				cohort.Cutoff().Add(-time.Second), cohort.Cutoff().Add(-time.Second),
			)),
		},
		{
			name: "missing parent",
			facts: append(append([]cohortLedgerFact{}, base...), correctionFact(
				t, cohort, assignmentFact.assignment, cohort.PrimaryQualityEndpoint().Evaluator, uuid.Must(uuid.NewV7()), 0,
			)),
		},
		{
			name: "fork",
			facts: append(append([]cohortLedgerFact{}, base...),
				correctionFact(t, cohort, assignmentFact.assignment, cohort.PrimaryQualityEndpoint().Evaluator, qualityRoot.event.ID, 0),
				correctionFact(t, cohort, assignmentFact.assignment, cohort.PrimaryQualityEndpoint().Evaluator, qualityRoot.event.ID, 0),
			),
		},
		{
			name:  "cycle",
			facts: cycleCorrectionFacts(t, cohort, assignmentFact.assignment),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, test.facts)
			if !errors.Is(err, ErrInvalidCohortLedger) {
				t.Fatalf("reconstructCohortLedger() error = %v, want ErrInvalidCohortLedger", err)
			}
		})
	}
}

func TestReconstructCohortLedgerFoldsPrimaryCorrection(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	facts := append([]cohortLedgerFact{assignmentFact}, successfulCohortOutcomeFacts(t, cohort, assignmentFact)...)
	qualityRoot := facts[2]
	correction := correctionFact(t, cohort, assignmentFact.assignment, cohort.PrimaryQualityEndpoint().Evaluator, qualityRoot.event.ID, 0)
	facts = append(facts, correction)

	ledger, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, facts)
	if err != nil {
		t.Fatalf("reconstructCohortLedger() error = %v", err)
	}
	member := ledger.Members[0]
	if member.Quality == nil || member.Quality.EventID != correction.event.ID || member.Quality.Measures.Quality == nil || *member.Quality.Measures.Quality != 0 || !member.OPEEligible {
		t.Fatalf("corrected member = %#v, want effective corrected primary quality", member)
	}
}

func TestReconstructCohortLedgerCanonicalizesMemberAndFactOrder(t *testing.T) {
	cohort, firstClaim, firstFact := closedCohortLedgerFixture(t)
	secondClaim, secondFact := additionalCohortLedgerMember(t, cohort, firstFact)

	forward, err := reconstructCohortLedger(
		cohort,
		cohort.Cutoff(),
		[]FocalClaim{firstClaim, secondClaim},
		[]cohortLedgerFact{firstFact, secondFact},
	)
	if err != nil {
		t.Fatalf("forward reconstructCohortLedger() error = %v", err)
	}
	reversed, err := reconstructCohortLedger(
		cohort,
		cohort.Cutoff(),
		[]FocalClaim{secondClaim, firstClaim},
		[]cohortLedgerFact{secondFact, firstFact},
	)
	if err != nil {
		t.Fatalf("reversed reconstructCohortLedger() error = %v", err)
	}
	if len(forward.Members) != 2 || len(forward.IncludedFacts) != 2 || forward.SHA256 != reversed.SHA256 {
		t.Fatalf("canonical ledgers = %#v / %#v", forward, reversed)
	}
	if forward.Members[0].Claim.RequestID.String() > forward.Members[1].Claim.RequestID.String() ||
		forward.IncludedFacts[0].AssignmentID.String() > forward.IncludedFacts[1].AssignmentID.String() {
		t.Fatalf("ledger is not canonically ordered: %#v", forward)
	}
}

func TestReconstructCohortLedgerReportsBelowMinimumAndCensorLimits(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	candidate := *cohort
	candidate.spec.Power.MinimumMembers = 2
	candidate.spec.Power.ExpectedMembers = 2
	candidate.spec.Censoring.MaxCensorRate = 0.5
	candidate.spec.Censoring.MaxCensorImbalance = 0.5

	ledger, err := reconstructCohortLedger(&candidate, candidate.Cutoff(), []FocalClaim{claim}, []cohortLedgerFact{assignmentFact})
	if err != nil {
		t.Fatalf("reconstructCohortLedger() error = %v", err)
	}
	if ledger.State != CohortLedgerClosed || ledger.Sealable {
		t.Fatalf("below-minimum ledger = %#v, want valid unsealable closed result", ledger)
	}
	diagnostics := ledger.CensorDiagnostics
	if diagnostics.Members != 1 || diagnostics.Censored != 1 || diagnostics.OverallCensorRate != 1 ||
		len(diagnostics.ArmDiagnostics) != len(candidate.Arms()) || diagnostics.CensorImbalance != 0 {
		t.Fatalf("censor diagnostics = %#v", diagnostics)
	}
}

func TestReconstructCohortLedgerReconstructsAssignmentFromFactPayload(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	forgedProjection := assignmentFact
	forgedProjection.assignment.ProviderID = "other-provider"

	ledger, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, []cohortLedgerFact{forgedProjection})
	if err != nil {
		t.Fatalf("reconstructCohortLedger() error = %v", err)
	}
	if ledger.Members[0].Assignment.ProviderID != cohort.Scope().ProviderID {
		t.Fatalf("assignment provider = %q, want immutable payload provider %q", ledger.Members[0].Assignment.ProviderID, cohort.Scope().ProviderID)
	}
}

func TestReconstructCohortLedgerRejectsDuplicateDelivery(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	facts := append([]cohortLedgerFact{assignmentFact}, successfulCohortOutcomeFacts(t, cohort, assignmentFact)...)
	facts = append(facts, facts[1])

	_, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, facts)
	if !errors.Is(err, ErrInvalidCohortLedger) {
		t.Fatalf("duplicate delivery error = %v, want ErrInvalidCohortLedger", err)
	}
}

func TestReconstructCohortLedgerRejectsOutputEnvelopePoison(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	base := append([]cohortLedgerFact{assignmentFact}, successfulCohortOutcomeFacts(t, cohort, assignmentFact)...)
	base = append(base, correctionFact(t, cohort, assignmentFact.assignment, cohort.PrimaryQualityEndpoint().Evaluator, base[2].event.ID, 0))
	mutations := []struct {
		name   string
		mutate func(*Event)
	}{
		{"event id", func(event *Event) { event.ID = uuid.Must(uuid.NewV7()) }},
		{"payload hash", func(event *Event) { event.PayloadHash = []byte("invalid") }},
		{"owner", func(event *Event) { event.OwnerID = uuid.Must(uuid.NewV7()) }},
		{"aggregate", func(event *Event) { event.AggregateID = "other" }},
		{"decision", func(event *Event) { event.DecisionID = uuid.Must(uuid.NewV7()) }},
	}
	for _, kind := range []struct {
		name  string
		index int
	}{{"delivery", 1}, {"outcome", 2}, {"correction", 4}} {
		for _, mutation := range mutations {
			t.Run(kind.name+"/"+mutation.name, func(t *testing.T) {
				facts := append([]cohortLedgerFact(nil), base...)
				mutation.mutate(&facts[kind.index].event)
				_, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, facts)
				if !errors.Is(err, ErrInvalidCohortLedger) {
					t.Fatalf("%s %s error = %v, want ErrInvalidCohortLedger", kind.name, mutation.name, err)
				}
			})
		}
	}
}

func TestReconstructCohortLedgerDerivesReasonCodesFromMissingFacts(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	outputFacts := successfulCohortOutcomeFacts(t, cohort, assignmentFact)
	for _, test := range []struct {
		name  string
		facts []cohortLedgerFact
		want  []string
	}{
		{
			name:  "missing delivery only",
			facts: append([]cohortLedgerFact{assignmentFact}, outputFacts[1:]...),
			want:  []string{"missing_delivery", "ope_ineligible"},
		},
		{
			name:  "missing primary harm only",
			facts: []cohortLedgerFact{assignmentFact, outputFacts[0], outputFacts[1]},
			want:  []string{"missing_primary_harm", "ope_ineligible"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ledger, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, test.facts)
			if err != nil {
				t.Fatalf("reconstructCohortLedger() error = %v", err)
			}
			if got := ledger.Members[0].ReasonCodes; !slices.Equal(got, test.want) {
				t.Fatalf("reason codes = %#v, want %#v", got, test.want)
			}
		})
	}
}

func closedCohortLedgerFixture(t *testing.T) (*FocalCohort, FocalClaim, cohortLedgerFact) {
	t.Helper()
	spec := focalCohortTestSpec()
	spec.Cutoff = time.Now().UTC().Add(-time.Hour)
	cohort := mustFocalCohort(t, spec)
	requestID := uuid.Must(uuid.NewV7())
	assignmentID, err := AssignmentIDForPoint(cohort.Scope().OwnerID, requestID, cohort.Predicate().Point, cohort.Predicate().Ordinal)
	if err != nil {
		t.Fatal(err)
	}
	actions := cohort.Actions()
	eligible, eligibilityHash, catalogHash, err := cohortAssignmentHashes(actions)
	if err != nil {
		t.Fatal(err)
	}
	cohortID := cohort.ID()
	arm := cohort.Arms()[0]
	assignment := Assignment{
		SchemaVersion: SchemaVersion2, AssignmentID: assignmentID, OwnerID: cohort.Scope().OwnerID,
		RequestID: requestID, Domain: cohort.Domain(), Point: cohort.Predicate().Point,
		PointOrdinal: cohort.Predicate().Ordinal, PolicyEpoch: cohort.Scope().PolicyEpoch,
		PolicyVersion: cohort.Scope().PolicyVersion, PolicyMode: PolicyCanary,
		SnapshotID: cohort.Scope().SnapshotID, SnapshotSHA256: cohort.Scope().SnapshotSHA256,
		Environment: cohort.Scope().Environment, ProviderID: cohort.Scope().ProviderID, ModelID: cohort.Scope().ModelID,
		CohortID: &cohortID, EligibleActions: eligible, EligibilitySHA256: eligibilityHash, CatalogSHA256: catalogHash,
		ChampionActionID: StaticActionID, RecommendedActionID: actions[0].ActionID, IntendedActionID: actions[0].ActionID,
		ExperimentID: cohort.ExperimentID(), ArmID: arm.ArmID, ArmProbability: &arm.Probability,
		ActionProbabilities: actions, SelectionReason: SelectionCanaryAssignment, Features: map[FeatureKey]float64{},
	}
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	claimedAt := cohort.Cutoff().Add(-2 * time.Minute)
	sessionID, episodeID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	claim := FocalClaim{
		ID: uuid.Must(uuid.NewV7()), OwnerID: cohort.Scope().OwnerID, CohortID: cohort.ID(), RequestID: requestID,
		EvaluationConversationID: uuid.Must(uuid.NewV7()), AssignmentID: assignmentID, Domain: cohort.Domain(),
		Point: cohort.Predicate().Point, PointOrdinal: cohort.Predicate().Ordinal, ClaimedAt: claimedAt,
		SessionID: &sessionID, EpisodeID: &episodeID,
		TimeBlockStart: cohortLedgerTestTimeBlockStart(claimedAt, cohort.Admission().TimeBlockSeconds),
	}
	return cohort, claim, cohortLedgerFact{assignment: assignment, event: event, recordedAt: cohort.Cutoff().Add(-time.Minute)}
}

func additionalCohortLedgerMember(t *testing.T, cohort *FocalCohort, first cohortLedgerFact) (FocalClaim, cohortLedgerFact) {
	t.Helper()
	assignment := first.assignment
	assignment.RequestID = uuid.Must(uuid.NewV7())
	assignmentID, err := AssignmentIDForPoint(assignment.OwnerID, assignment.RequestID, assignment.Point, assignment.PointOrdinal)
	if err != nil {
		t.Fatal(err)
	}
	assignment.AssignmentID = assignmentID
	event := mustAssignmentEvent(t, assignment)
	claimedAt := cohort.Cutoff().Add(-2 * time.Minute)
	sessionID, episodeID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	claim := FocalClaim{
		ID: uuid.Must(uuid.NewV7()), OwnerID: assignment.OwnerID, CohortID: cohort.ID(), RequestID: assignment.RequestID,
		EvaluationConversationID: uuid.Must(uuid.NewV7()), AssignmentID: assignmentID, Domain: assignment.Domain,
		Point: assignment.Point, PointOrdinal: assignment.PointOrdinal, ClaimedAt: claimedAt,
		SessionID: &sessionID, EpisodeID: &episodeID,
		TimeBlockStart: cohortLedgerTestTimeBlockStart(claimedAt, cohort.Admission().TimeBlockSeconds),
	}
	return claim, cohortLedgerFact{assignment: assignment, event: event, recordedAt: cohort.Cutoff().Add(-time.Minute)}
}

func successfulCohortOutcomeFacts(t *testing.T, cohort *FocalCohort, assignmentFact cohortLedgerFact) []cohortLedgerFact {
	t.Helper()
	assignment := assignmentFact.assignment
	probability := 0.25
	delivery, err := NewDeliveryEvent(assignment, Delivery{
		SchemaVersion: SchemaVersion2, AssignmentID: assignment.AssignmentID, IntendedActionID: assignment.IntendedActionID,
		ActualActionID: assignment.IntendedActionID, Status: DeliverySuccess, ExposureKnown: true, ExposureProbability: &probability,
		ResultIDs: []ResultID{}, Revisions: RevisionSet{}, EffectiveLimits: map[string]int{},
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewEvaluatorCatalog(cohort.Evaluators()...)
	if err != nil {
		t.Fatal(err)
	}
	quality, harm := 1.0, 0.0
	observedAt := cohort.Cutoff().Add(-30 * time.Second)
	facts := []cohortLedgerFact{{event: delivery, recordedAt: observedAt}}
	for _, evaluator := range []EvaluatorProvenance{cohort.PrimaryQualityEndpoint().Evaluator, cohort.PrimaryHarmEndpoint().Evaluator} {
		facts = append(facts, cohortOutcomeFactWithCatalog(t, assignment, catalog, evaluator, observedAt, observedAt, quality, harm))
	}
	return facts
}

func cohortOutcomeFact(t *testing.T, cohort *FocalCohort, assignment Assignment, evaluator EvaluatorProvenance, observedAt, recordedAt time.Time) cohortLedgerFact {
	t.Helper()
	catalog, err := NewEvaluatorCatalog(cohort.Evaluators()...)
	if err != nil {
		t.Fatal(err)
	}
	return cohortOutcomeFactWithCatalog(t, assignment, catalog, evaluator, observedAt, recordedAt, 1, 0)
}

func cohortOutcomeFactWithCatalog(t *testing.T, assignment Assignment, catalog EvaluatorCatalog, evaluator EvaluatorProvenance, observedAt, recordedAt time.Time, quality, harm float64) cohortLedgerFact {
	t.Helper()
	event, err := NewOutcomeEvent(assignment, OutcomeObservation{
		SchemaVersion: SchemaVersion2, AssignmentID: assignment.AssignmentID, OwnerID: assignment.OwnerID,
		Domain: assignment.Domain, ProviderID: assignment.ProviderID, ModelID: assignment.ModelID, Evaluator: evaluator,
		SourceObservationID: uuid.Must(uuid.NewV7()), Status: OutcomeObserved,
		Measures: OutcomeMeasures{Quality: &quality, Harm: &harm}, ObservedAt: observedAt,
	}, catalog)
	if err != nil {
		t.Fatal(err)
	}
	return cohortLedgerFact{event: event, recordedAt: recordedAt}
}

func correctionFact(t *testing.T, cohort *FocalCohort, assignment Assignment, evaluator EvaluatorProvenance, supersedes uuid.UUID, quality float64) cohortLedgerFact {
	t.Helper()
	catalog, err := NewEvaluatorCatalog(cohort.Evaluators()...)
	if err != nil {
		t.Fatal(err)
	}
	harm := 0.0
	event, err := NewCorrectionEvent(assignment, CorrectionObservation{
		SchemaVersion: SchemaVersion2, AssignmentID: assignment.AssignmentID, OwnerID: assignment.OwnerID,
		Domain: assignment.Domain, ProviderID: assignment.ProviderID, ModelID: assignment.ModelID, Evaluator: evaluator,
		CorrectionSourceID: uuid.Must(uuid.NewV7()), SupersedesEventID: supersedes, Status: OutcomeObserved,
		Measures: OutcomeMeasures{Quality: &quality, Harm: &harm}, ObservedAt: cohort.Cutoff().Add(-time.Second),
	}, catalog)
	if err != nil {
		t.Fatal(err)
	}
	return cohortLedgerFact{event: event, recordedAt: cohort.Cutoff().Add(-time.Second)}
}

func cycleCorrectionFacts(t *testing.T, cohort *FocalCohort, assignment Assignment) []cohortLedgerFact {
	t.Helper()
	catalog, err := NewEvaluatorCatalog(cohort.Evaluators()...)
	if err != nil {
		t.Fatal(err)
	}
	quality, harm := 0.0, 0.0
	newCorrection := func(source, supersedes uuid.UUID) cohortLedgerFact {
		event, err := NewCorrectionEvent(assignment, CorrectionObservation{
			SchemaVersion: SchemaVersion2, AssignmentID: assignment.AssignmentID, OwnerID: assignment.OwnerID,
			Domain: assignment.Domain, ProviderID: assignment.ProviderID, ModelID: assignment.ModelID, Evaluator: cohort.PrimaryQualityEndpoint().Evaluator,
			CorrectionSourceID: source, SupersedesEventID: supersedes, Status: OutcomeObserved,
			Measures: OutcomeMeasures{Quality: &quality, Harm: &harm}, ObservedAt: cohort.Cutoff().Add(-time.Second),
		}, catalog)
		if err != nil {
			t.Fatal(err)
		}
		return cohortLedgerFact{event: event, recordedAt: cohort.Cutoff().Add(-time.Second)}
	}
	firstSource, secondSource := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	second := newCorrection(secondSource, uuid.Must(uuid.NewV7()))
	first := newCorrection(firstSource, second.event.ID)
	second = newCorrection(secondSource, first.event.ID)
	return []cohortLedgerFact{cohortLedgerFact{assignment: assignment, event: mustAssignmentEvent(t, assignment), recordedAt: cohort.Cutoff().Add(-time.Minute)}, first, second}
}

func mustAssignmentEvent(t *testing.T, assignment Assignment) Event {
	t.Helper()
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	return event
}
