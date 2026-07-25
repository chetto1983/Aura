package adaptive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidCohortLedger reports an invalid closed cohort reconstruction input.
var ErrInvalidCohortLedger = errors.New("adaptive cohort ledger is invalid")

// CohortLedgerState identifies whether a cohort reconstruction is final.
type CohortLedgerState string

const (
	// CohortLedgerOpen indicates that the cohort cutoff has not elapsed.
	CohortLedgerOpen CohortLedgerState = "open"
	// CohortLedgerClosed indicates that the cohort cutoff has elapsed.
	CohortLedgerClosed CohortLedgerState = "closed"
)

// CohortLedger is the deterministic reconstruction of a focal cohort.
type CohortLedger struct {
	CohortID          uuid.UUID
	CohortSHA256      string
	State             CohortLedgerState
	Members           []CohortMember
	IncludedFacts     []IncludedFactRef
	LateFactCount     int
	CensorDiagnostics CohortCensorDiagnostics
	SHA256            string
	Sealable          bool
}

// CohortMember is one claim and its effective evidence in a cohort ledger.
type CohortMember struct {
	Claim       FocalClaim
	Assignment  Assignment
	Delivery    *CohortDeliveryProjection
	Quality     *CohortEffectiveObservation
	Harm        *CohortEffectiveObservation
	Observed    bool
	ITTEligible bool
	OPEEligible bool
	ReasonCodes []string
}

// CohortDeliveryProjection is the delivery evidence associated with a member.
type CohortDeliveryProjection struct {
	EventID             uuid.UUID
	IntendedActionID    string
	ActualActionID      string
	Status              DeliveryStatus
	ExposureKnown       bool
	ExposureProbability *float64
	FallbackReason      FallbackReason
}

// CohortEffectiveObservation is the terminal primary outcome evidence for a member.
type CohortEffectiveObservation struct {
	EventID    uuid.UUID
	Status     OutcomeStatus
	Measures   OutcomeMeasures
	ObservedAt time.Time
}

// IncludedFactRef identifies one event included in a closed cohort ledger.
type IncludedFactRef struct {
	EventID       uuid.UUID
	AssignmentID  uuid.UUID
	Kind          EventKind
	PayloadSHA256 string
	RecordedAt    time.Time
}

// CohortArmCensorDiagnostics summarizes censoring for one cohort arm.
type CohortArmCensorDiagnostics struct {
	ArmID      string
	Members    int
	Censored   int
	CensorRate float64
}

// CohortCensorDiagnostics summarizes censoring across a closed cohort ledger.
type CohortCensorDiagnostics struct {
	Members           int
	Censored          int
	OverallCensorRate float64
	ArmDiagnostics    []CohortArmCensorDiagnostics
	CensorImbalance   float64
}

type cohortLedgerFact struct {
	assignment Assignment
	event      Event
	recordedAt time.Time
}

type loadedCohortOutcome struct {
	event       Event
	observation OutcomeObservation
}

type loadedCohortCorrection struct {
	event      Event
	correction CorrectionObservation
}

func reconstructCohortLedger(
	cohort *FocalCohort,
	closureTime time.Time,
	claims []FocalClaim,
	facts []cohortLedgerFact,
) (CohortLedger, error) {
	if closureTime.Before(cohort.Cutoff()) {
		return CohortLedger{
			CohortID:     cohort.ID(),
			CohortSHA256: cohort.SHA256(),
			State:        CohortLedgerOpen,
		}, nil
	}
	closedFacts, lateFactCount, err := cohortClosedFacts(cohort.Cutoff(), facts)
	if err != nil {
		return CohortLedger{}, err
	}
	facts = closedFacts
	if uint64(len(claims)) > cohort.Power().ExpectedMembers {
		return CohortLedger{}, fmt.Errorf("%w: expected members", ErrInvalidCohortLedger)
	}
	claimsByAssignment := make(map[uuid.UUID]FocalClaim, len(claims))
	for _, claim := range claims {
		if err := validateCohortLedgerClaim(cohort, claim); err != nil {
			return CohortLedger{}, err
		}
		claim = canonicalCohortLedgerClaim(claim)
		if _, duplicate := claimsByAssignment[claim.AssignmentID]; duplicate {
			return CohortLedger{}, fmt.Errorf("%w: duplicate claim", ErrInvalidCohortLedger)
		}
		claimsByAssignment[claim.AssignmentID] = claim
	}
	factsByAssignment := make(map[uuid.UUID][]cohortLedgerFact, len(claims))
	for _, fact := range facts {
		assignmentID := fact.event.DecisionID
		if fact.event.Kind == EventDecision {
			assignment, err := DecodeAssignment(fact.event.Payload)
			if err != nil {
				return CohortLedger{}, fmt.Errorf("%w: assignment payload", ErrInvalidCohortLedger)
			}
			assignmentID = assignment.AssignmentID
		}
		factsByAssignment[assignmentID] = append(factsByAssignment[assignmentID], fact)
	}
	members := make([]CohortMember, 0, len(claims))
	included := make([]IncludedFactRef, 0, len(facts))
	for assignmentID, claim := range claimsByAssignment {
		memberFacts := factsByAssignment[assignmentID]
		if len(memberFacts) == 0 {
			return CohortLedger{}, fmt.Errorf("%w: claim assignment", ErrInvalidCohortLedger)
		}
		member, err := reconstructCohortMember(cohort, claim, memberFacts)
		if err != nil {
			return CohortLedger{}, err
		}
		members = append(members, member)
		for _, fact := range memberFacts {
			included = append(included, IncludedFactRef{
				EventID: fact.event.ID, AssignmentID: assignmentID, Kind: fact.event.Kind,
				PayloadSHA256: hex.EncodeToString(fact.event.PayloadHash), RecordedAt: canonicalCohortLedgerTime(fact.recordedAt),
			})
		}
	}
	if len(factsByAssignment) != len(claimsByAssignment) {
		return CohortLedger{}, fmt.Errorf("%w: orphan assignment", ErrInvalidCohortLedger)
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].Claim.RequestID != members[j].Claim.RequestID {
			return members[i].Claim.RequestID.String() < members[j].Claim.RequestID.String()
		}
		return members[i].Claim.AssignmentID.String() < members[j].Claim.AssignmentID.String()
	})
	sort.Slice(included, func(i, j int) bool {
		if included[i].AssignmentID != included[j].AssignmentID {
			return included[i].AssignmentID.String() < included[j].AssignmentID.String()
		}
		if included[i].Kind != included[j].Kind {
			return included[i].Kind < included[j].Kind
		}
		return included[i].EventID.String() < included[j].EventID.String()
	})
	diagnostics := cohortCensorDiagnostics(cohort, members)
	digest, err := cohortLedgerHash(cohort, members, included)
	if err != nil {
		return CohortLedger{}, fmt.Errorf("%w: marshal digest: %w", ErrInvalidCohortLedger, err)
	}
	return CohortLedger{
		CohortID: cohort.ID(), CohortSHA256: cohort.SHA256(), State: CohortLedgerClosed,
		Members: members, IncludedFacts: included, LateFactCount: lateFactCount, CensorDiagnostics: diagnostics,
		SHA256: digest,
		Sealable: uint64(len(members)) >= cohort.Power().MinimumMembers &&
			diagnostics.OverallCensorRate <= cohort.Censoring().MaxCensorRate &&
			diagnostics.CensorImbalance <= cohort.Censoring().MaxCensorImbalance,
	}, nil
}

func validateCohortLedgerClaim(cohort *FocalCohort, claim FocalClaim) error {
	if cohort == nil {
		return fmt.Errorf("%w: cohort", ErrInvalidCohortLedger)
	}
	scope, predicate := cohort.Scope(), cohort.Predicate()
	if claim.ID == uuid.Nil || claim.OwnerID == uuid.Nil || claim.CohortID == uuid.Nil ||
		claim.RequestID == uuid.Nil || claim.EvaluationConversationID == uuid.Nil || claim.AssignmentID == uuid.Nil ||
		claim.OwnerID != scope.OwnerID || claim.CohortID != cohort.ID() ||
		claim.Domain != predicate.Domain || claim.Point != predicate.Point || claim.PointOrdinal != predicate.Ordinal ||
		claim.ClaimedAt.IsZero() || !claim.ClaimedAt.Round(0).UTC().Before(cohort.Cutoff()) {
		return fmt.Errorf("%w: claim envelope", ErrInvalidCohortLedger)
	}
	expectedAssignmentID, err := AssignmentIDForPoint(claim.OwnerID, claim.RequestID, claim.Point, claim.PointOrdinal)
	if err != nil || claim.AssignmentID != expectedAssignmentID {
		return fmt.Errorf("%w: claim assignment", ErrInvalidCohortLedger)
	}
	admission := cohort.Admission()
	if int64(admission.TimeBlockSeconds) <= 0 ||
		(cohortClaimIDPresent(claim.SessionID) != cohortRequiresBlock(admission.BlockKeys, BlockSession)) ||
		(cohortClaimIDPresent(claim.EpisodeID) != cohortRequiresBlock(admission.BlockKeys, BlockEpisode)) ||
		claim.TimeBlockStart.IsZero() ||
		!claim.TimeBlockStart.Round(0).UTC().Equal(cohortClaimTimeBlockStart(claim.ClaimedAt, admission.TimeBlockSeconds)) {
		return fmt.Errorf("%w: claim blocks", ErrInvalidCohortLedger)
	}
	return nil
}

func cohortClaimIDPresent(id *uuid.UUID) bool {
	return id != nil && *id != uuid.Nil
}

func cohortRequiresBlock(blocks []BlockKey, target BlockKey) bool {
	for _, block := range blocks {
		if block == target {
			return true
		}
	}
	return false
}

func cohortClaimTimeBlockStart(claimedAt time.Time, seconds uint64) time.Time {
	blockSeconds := int64(seconds)
	epochSeconds := claimedAt.Round(0).UTC().Unix()
	quotient := epochSeconds / blockSeconds
	if epochSeconds < 0 && epochSeconds%blockSeconds != 0 {
		quotient--
	}
	return time.Unix(quotient*blockSeconds, 0).UTC()
}

func canonicalCohortLedgerClaim(claim FocalClaim) FocalClaim {
	claim.ClaimedAt = canonicalCohortLedgerTime(claim.ClaimedAt)
	claim.TimeBlockStart = canonicalCohortLedgerTime(claim.TimeBlockStart)
	return claim
}

func canonicalCohortLedgerTime(value time.Time) time.Time {
	return value.Round(0).UTC()
}

func cohortCensorDiagnostics(cohort *FocalCohort, members []CohortMember) CohortCensorDiagnostics {
	diagnostics := CohortCensorDiagnostics{Members: len(members), ArmDiagnostics: make([]CohortArmCensorDiagnostics, len(cohort.Arms()))}
	byArm := make(map[string]int, len(diagnostics.ArmDiagnostics))
	for index, arm := range cohort.Arms() {
		diagnostics.ArmDiagnostics[index].ArmID = arm.ArmID
		byArm[arm.ArmID] = index
	}
	minRate, maxRate := 1.0, 0.0
	observedArm := false
	for _, member := range members {
		index := byArm[member.Assignment.ArmID]
		diagnostics.ArmDiagnostics[index].Members++
		if !member.Observed {
			diagnostics.Censored++
			diagnostics.ArmDiagnostics[index].Censored++
		}
	}
	for index := range diagnostics.ArmDiagnostics {
		arm := &diagnostics.ArmDiagnostics[index]
		if arm.Members == 0 {
			continue
		}
		arm.CensorRate = float64(arm.Censored) / float64(arm.Members)
		if arm.CensorRate < minRate {
			minRate = arm.CensorRate
		}
		if arm.CensorRate > maxRate {
			maxRate = arm.CensorRate
		}
		observedArm = true
	}
	if diagnostics.Members > 0 {
		diagnostics.OverallCensorRate = float64(diagnostics.Censored) / float64(diagnostics.Members)
	}
	if observedArm {
		diagnostics.CensorImbalance = maxRate - minRate
	}
	return diagnostics
}

func reconstructCohortMember(cohort *FocalCohort, claim FocalClaim, facts []cohortLedgerFact) (CohortMember, error) {
	var fact cohortLedgerFact
	foundAssignment := false
	for _, candidate := range facts {
		if candidate.event.Kind == EventDecision {
			if foundAssignment {
				return CohortMember{}, fmt.Errorf("%w: duplicate assignment", ErrInvalidCohortLedger)
			}
			fact, foundAssignment = candidate, true
		}
	}
	if !foundAssignment {
		return CohortMember{}, fmt.Errorf("%w: assignment", ErrInvalidCohortLedger)
	}
	assignment, err := DecodeAssignment(fact.event.Payload)
	if err != nil {
		return CohortMember{}, fmt.Errorf("%w: assignment payload", ErrInvalidCohortLedger)
	}
	if err := validateCohortAssignmentFact(cohort, claim, fact.event, assignment); err != nil {
		return CohortMember{}, err
	}
	member := CohortMember{
		Claim:       claim,
		Assignment:  assignment,
		ITTEligible: true,
	}
	catalog, err := NewEvaluatorCatalog(cohort.Evaluators()...)
	if err != nil {
		return CohortMember{}, err
	}
	outcomes := make(map[uuid.UUID]loadedCohortOutcome)
	corrections := make(map[uuid.UUID]loadedCohortCorrection)
	for _, candidate := range facts {
		switch candidate.event.Kind {
		case EventDelivery:
			if member.Delivery != nil {
				return CohortMember{}, fmt.Errorf("%w: duplicate delivery", ErrInvalidCohortLedger)
			}
			wire, err := decodeDeliveryWire(candidate.event.Payload)
			if err != nil {
				return CohortMember{}, fmt.Errorf("%w: delivery payload", ErrInvalidCohortLedger)
			}
			if err := validateCohortDeliveryFact(assignment, candidate.event, wire); err != nil {
				return CohortMember{}, err
			}
			member.Delivery = &CohortDeliveryProjection{
				EventID: candidate.event.ID, IntendedActionID: wire.IntendedActionID, ActualActionID: wire.ActualActionID,
				Status: wire.Status, ExposureKnown: wire.ExposureKnown, ExposureProbability: wire.ExposureProbability,
				FallbackReason: wire.FallbackReason,
			}
		case EventOutcome:
			outcome, err := DecodeOutcome(candidate.event.Payload, assignment, catalog)
			if err != nil {
				return CohortMember{}, fmt.Errorf("%w: outcome payload", ErrInvalidCohortLedger)
			}
			if err := validateCohortOutcomeFact(assignment, candidate.event, outcome, catalog); err != nil {
				return CohortMember{}, err
			}
			outcomes[candidate.event.ID] = loadedCohortOutcome{event: candidate.event, observation: outcome}
		case EventCorrection:
			correction, err := DecodeCorrection(candidate.event.Payload, assignment, catalog)
			if err != nil {
				return CohortMember{}, fmt.Errorf("%w: correction payload", ErrInvalidCohortLedger)
			}
			if err := validateCohortCorrectionFact(assignment, candidate.event, correction, catalog); err != nil {
				return CohortMember{}, err
			}
			corrections[candidate.event.ID] = loadedCohortCorrection{event: candidate.event, correction: correction}
		}
	}
	if err := validateCohortCorrectionGraph(outcomes, corrections); err != nil {
		return CohortMember{}, err
	}
	quality, qualityObservation, err := effectivePrimaryCohortObservation(cohort.PrimaryQualityEndpoint().Evaluator, outcomes, corrections)
	if err != nil {
		return CohortMember{}, err
	}
	member.Quality = quality
	if member.Quality != nil && !validCohortTerminalPrimaryObservation(qualityObservation, cohort.ValidatePrimaryQualityOutcome) {
		return CohortMember{}, fmt.Errorf("%w: primary quality evidence", ErrInvalidCohortLedger)
	}
	harm, harmObservation, err := effectivePrimaryCohortObservation(cohort.PrimaryHarmEndpoint().Evaluator, outcomes, corrections)
	if err != nil {
		return CohortMember{}, err
	}
	member.Harm = harm
	if member.Harm != nil && !validCohortTerminalPrimaryObservation(harmObservation, cohort.ValidatePrimaryHarmOutcome) {
		return CohortMember{}, fmt.Errorf("%w: primary harm evidence", ErrInvalidCohortLedger)
	}
	member.Observed = member.Quality != nil && member.Harm != nil && member.Quality.Status == OutcomeObserved && member.Harm.Status == OutcomeObserved
	member.OPEEligible = member.Observed && member.Delivery != nil && member.Delivery.Status == DeliverySuccess &&
		member.Delivery.ActualActionID == member.Assignment.IntendedActionID && member.Delivery.ExposureKnown &&
		member.Delivery.ExposureProbability != nil && *member.Delivery.ExposureProbability > 0
	member.ReasonCodes = cohortMemberReasonCodes(member)
	return member, nil
}

func cohortMemberReasonCodes(member CohortMember) []string {
	codes := make([]string, 0, 4)
	if member.Delivery == nil {
		codes = append(codes, "missing_delivery")
	}
	if member.Harm == nil {
		codes = append(codes, "missing_primary_harm")
	}
	if member.Quality == nil {
		codes = append(codes, "missing_primary_quality")
	}
	if !member.OPEEligible {
		codes = append(codes, "ope_ineligible")
	}
	return codes
}

func validCohortTerminalPrimaryObservation(observation OutcomeObservation, validate func(OutcomeObservation) error) bool {
	return observation.Status == OutcomeCensored ||
		(observation.Status == OutcomeObserved && validate(observation) == nil)
}

func cohortLedgerHash(cohort *FocalCohort, members []CohortMember, facts []IncludedFactRef) (string, error) {
	document, err := json.Marshal(struct {
		CohortID     uuid.UUID
		CohortSHA256 string
		Cutoff       time.Time
		Members      []CohortMember
		Facts        []IncludedFactRef
	}{cohort.ID(), cohort.SHA256(), cohort.Cutoff(), members, facts})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(document)
	return hex.EncodeToString(sum[:]), nil
}

func validateCohortCorrectionGraph(outcomes map[uuid.UUID]loadedCohortOutcome, corrections map[uuid.UUID]loadedCohortCorrection) error {
	facts := make(map[uuid.UUID]outcomeChainFact, len(outcomes)+len(corrections))
	for id, outcome := range outcomes {
		facts[id] = outcomeChainFact{id: id, kind: EventOutcome, evaluator: outcome.observation.Evaluator}
	}
	children := make(map[uuid.UUID]struct{}, len(corrections))
	for id, correction := range corrections {
		target, exists := facts[correction.correction.SupersedesEventID]
		if !exists {
			if prior, correctionExists := corrections[correction.correction.SupersedesEventID]; correctionExists {
				target = outcomeChainFact{id: prior.event.ID, kind: EventCorrection, evaluator: prior.correction.Evaluator, supersedes: prior.correction.SupersedesEventID}
				exists = true
			}
		}
		if !exists || target.evaluator != correction.correction.Evaluator {
			return fmt.Errorf("%w: correction parent", ErrInvalidCohortLedger)
		}
		if _, fork := children[correction.correction.SupersedesEventID]; fork {
			return fmt.Errorf("%w: correction fork", ErrInvalidCohortLedger)
		}
		children[correction.correction.SupersedesEventID] = struct{}{}
		facts[id] = outcomeChainFact{id: id, kind: EventCorrection, evaluator: correction.correction.Evaluator, supersedes: correction.correction.SupersedesEventID}
	}
	if err := validateCorrectionGraph(facts); err != nil {
		return fmt.Errorf("%w: correction graph", ErrInvalidCohortLedger)
	}
	return nil
}

func effectivePrimaryCohortObservation(evaluator EvaluatorProvenance, outcomes map[uuid.UUID]loadedCohortOutcome, corrections map[uuid.UUID]loadedCohortCorrection) (*CohortEffectiveObservation, OutcomeObservation, error) {
	facts := make([]outcomeChainFact, 0, len(outcomes)+len(corrections))
	roots := make([]uuid.UUID, 0, 1)
	for id, outcome := range outcomes {
		facts = append(facts, outcomeChainFact{id: id, kind: EventOutcome, evaluator: outcome.observation.Evaluator})
		if outcome.observation.Evaluator == evaluator {
			roots = append(roots, id)
		}
	}
	for id, correction := range corrections {
		facts = append(facts, outcomeChainFact{id: id, kind: EventCorrection, evaluator: correction.correction.Evaluator, supersedes: correction.correction.SupersedesEventID})
	}
	if len(roots) == 0 {
		return nil, OutcomeObservation{}, nil
	}
	if len(roots) != 1 {
		return nil, OutcomeObservation{}, fmt.Errorf("%w: duplicate primary root", ErrInvalidCohortLedger)
	}
	terminal, err := foldOutcomeChain(facts, roots[0])
	if err != nil {
		return nil, OutcomeObservation{}, fmt.Errorf("%w: primary correction chain", ErrInvalidCohortLedger)
	}
	if outcome, exists := outcomes[terminal.id]; exists {
		return &CohortEffectiveObservation{EventID: outcome.event.ID, Status: outcome.observation.Status, Measures: outcome.observation.Measures, ObservedAt: outcome.observation.ObservedAt}, outcome.observation, nil
	}
	correction := corrections[terminal.id]
	observation := OutcomeObservation{
		SchemaVersion: correction.correction.SchemaVersion, AssignmentID: correction.correction.AssignmentID,
		OwnerID: correction.correction.OwnerID, Domain: correction.correction.Domain,
		ProviderID: correction.correction.ProviderID, ModelID: correction.correction.ModelID,
		Evaluator: correction.correction.Evaluator, SourceObservationID: correction.correction.CorrectionSourceID,
		Status: correction.correction.Status, Measures: correction.correction.Measures, ObservedAt: correction.correction.ObservedAt,
	}
	return &CohortEffectiveObservation{EventID: correction.event.ID, Status: correction.correction.Status, Measures: correction.correction.Measures, ObservedAt: correction.correction.ObservedAt}, observation, nil
}

func validateCohortAssignmentFact(cohort *FocalCohort, claim FocalClaim, event Event, assignment Assignment) error {
	if event.Kind != EventDecision || claim.AssignmentID != assignment.AssignmentID {
		return fmt.Errorf("%w: claim assignment", ErrInvalidCohortLedger)
	}
	expected, err := validateFocalCohortAssignment(cohort, claim, assignment)
	expectedPayload, expectedPayloadErr := canonicalPayload(expected.Payload)
	actualPayload, actualPayloadErr := canonicalPayload(event.Payload)
	if err != nil || expected.ID != event.ID || expected.OwnerID != event.OwnerID ||
		expected.AggregateID != event.AggregateID || expected.DecisionID != event.DecisionID ||
		expected.Kind != event.Kind || expectedPayloadErr != nil || actualPayloadErr != nil ||
		!bytes.Equal(expectedPayload, actualPayload) ||
		!bytes.Equal(expected.PayloadHash, event.PayloadHash) {
		return fmt.Errorf("%w: assignment envelope", ErrInvalidCohortLedger)
	}
	return nil
}

func cohortClosedFacts(cutoff time.Time, facts []cohortLedgerFact) ([]cohortLedgerFact, int, error) {
	closed := make([]cohortLedgerFact, 0, len(facts))
	late := 0
	for _, fact := range facts {
		if fact.recordedAt.IsZero() {
			return nil, 0, fmt.Errorf("%w: recorded_at", ErrInvalidCohortLedger)
		}
		if !fact.recordedAt.Before(cutoff) {
			late++
			continue
		}
		if fact.event.Kind == EventOutcome || fact.event.Kind == EventCorrection {
			var observed struct {
				ObservedAt time.Time `json:"observed_at"`
			}
			if err := json.Unmarshal(fact.event.Payload, &observed); err != nil || observed.ObservedAt.IsZero() {
				return nil, 0, fmt.Errorf("%w: observed_at", ErrInvalidCohortLedger)
			}
			if !observed.ObservedAt.Round(0).UTC().Before(cutoff) {
				late++
				continue
			}
		}
		fact.recordedAt = canonicalCohortLedgerTime(fact.recordedAt)
		closed = append(closed, fact)
	}
	return closed, late, nil
}
