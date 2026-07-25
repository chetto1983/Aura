package adaptive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	// ErrFocalClaimConflict rejects changed, missing, or unbound focal enrollment state.
	ErrFocalClaimConflict = errors.New("adaptive focal claim conflicts with persisted state")
	// ErrFocalSlotUnavailable means another request already owns the conversation focal slot.
	ErrFocalSlotUnavailable = errors.New("adaptive focal slot is unavailable")
)

// FocalEnrollmentRequest identifies one attempted focal cohort enrollment.
type FocalEnrollmentRequest struct {
	OwnerID                  uuid.UUID
	CohortID                 uuid.UUID
	RequestID                uuid.UUID
	EvaluationConversationID uuid.UUID
	SessionID                *uuid.UUID
	EpisodeID                *uuid.UUID
	Domain                   Domain
	Point                    DecisionPoint
	PointOrdinal             uint32
}

// FocalClaim is the immutable database-backed focal membership record.
type FocalClaim struct {
	ID                              uuid.UUID
	OwnerID                         uuid.UUID
	CohortID                        uuid.UUID
	RequestID                       uuid.UUID
	EvaluationConversationID        uuid.UUID
	AssignmentID                    uuid.UUID
	Domain                          Domain
	Point                           DecisionPoint
	PointOrdinal                    uint32
	SessionID                       *uuid.UUID
	EpisodeID                       *uuid.UUID
	TimeBlockStart                  time.Time
	ClaimedAt                       time.Time
	AnalysisStratumSchemaSHA256     string
	AnalysisStratumID               string
	InterferenceClusterSchemaSHA256 string
	InterferenceClusterID           string
}

// FocalEnrollment is the complete durable randomized tuple.
type FocalEnrollment struct {
	Claim      FocalClaim
	Assignment Assignment
	Receipt    RandomizationReceipt
}

func (request FocalEnrollmentRequest) assignmentID() (uuid.UUID, error) {
	switch {
	case request.OwnerID == uuid.Nil || request.CohortID == uuid.Nil || request.RequestID == uuid.Nil:
		return uuid.Nil, ErrFocalClaimConflict
	case request.EvaluationConversationID == uuid.Nil:
		return uuid.Nil, ErrFocalClaimConflict
	case !request.Domain.valid() || !request.Point.valid() || request.Domain.point() != request.Point:
		return uuid.Nil, ErrFocalClaimConflict
	case request.SessionID != nil && *request.SessionID == uuid.Nil:
		return uuid.Nil, ErrFocalClaimConflict
	case request.EpisodeID != nil && *request.EpisodeID == uuid.Nil:
		return uuid.Nil, ErrFocalClaimConflict
	}
	return AssignmentIDForPoint(request.OwnerID, request.RequestID, request.Point, request.PointOrdinal)
}

func (request FocalEnrollmentRequest) validateAgainst(
	cohort *FocalCohort,
	assignmentID uuid.UUID,
) error {
	if cohort == nil || cohort.ID() != request.CohortID ||
		cohort.Scope().OwnerID != request.OwnerID ||
		!cohort.MatchesFocalPoint(request.Domain, request.Point, request.PointOrdinal) ||
		assignmentID == uuid.Nil {
		return ErrFocalClaimConflict
	}
	admission := cohort.Admission()
	if !cohort.randomizationEligible() &&
		(slices.Contains(admission.BlockKeys, BlockSession) != (request.SessionID != nil) ||
			slices.Contains(admission.BlockKeys, BlockEpisode) != (request.EpisodeID != nil)) {
		return ErrFocalClaimConflict
	}
	return validateFrozenBernoulliArms(cohort.Arms())
}

func focalClaimFromPersistedRow(
	row sqlc.AuraAdaptiveFocalCohortClaims,
) (FocalClaim, error) {
	claim := FocalClaim{
		ID: uuid.UUID(row.ID.Bytes), OwnerID: uuid.UUID(row.OwnerID.Bytes),
		CohortID: uuid.UUID(row.CohortID.Bytes), RequestID: uuid.UUID(row.RequestID.Bytes),
		EvaluationConversationID: uuid.UUID(row.EvaluationConversationID.Bytes),
		AssignmentID:             uuid.UUID(row.AssignmentID.Bytes), Domain: Domain(row.Domain),
		Point: DecisionPoint(row.DecisionPoint), TimeBlockStart: row.TimeBlockStart.Time,
		ClaimedAt: row.ClaimedAt.Time, SessionID: optionalUUID(row.SessionID),
		EpisodeID:                       optionalUUID(row.EpisodeID),
		AnalysisStratumSchemaSHA256:     hex.EncodeToString(row.AnalysisStratumSchemaSha256),
		AnalysisStratumID:               hex.EncodeToString(row.AnalysisStratumID),
		InterferenceClusterSchemaSHA256: hex.EncodeToString(row.InterferenceClusterSchemaSha256),
		InterferenceClusterID:           hex.EncodeToString(row.InterferenceClusterID),
	}
	if row.PointOrdinal < 0 || row.PointOrdinal > int64(^uint32(0)) ||
		!row.ID.Valid || !row.OwnerID.Valid || !row.CohortID.Valid ||
		!row.RequestID.Valid || !row.EvaluationConversationID.Valid ||
		!row.AssignmentID.Valid || !row.TimeBlockStart.Valid || !row.ClaimedAt.Valid {
		return FocalClaim{}, ErrFocalClaimConflict
	}
	claim.PointOrdinal = uint32(row.PointOrdinal)
	return claim, nil
}

func (claim FocalClaim) matches(
	request FocalEnrollmentRequest,
	assignmentID uuid.UUID,
) bool {
	return claim.OwnerID == request.OwnerID && claim.CohortID == request.CohortID &&
		claim.RequestID == request.RequestID &&
		claim.EvaluationConversationID == request.EvaluationConversationID &&
		claim.AssignmentID == assignmentID && claim.Domain == request.Domain &&
		claim.Point == request.Point && claim.PointOrdinal == request.PointOrdinal &&
		optionalUUIDEqual(claim.SessionID, request.SessionID) &&
		optionalUUIDEqual(claim.EpisodeID, request.EpisodeID)
}

func (claim FocalClaim) validRandomizationHashes(
	cohort *FocalCohort,
) bool {
	if !validSHA256(claim.AnalysisStratumSchemaSHA256) ||
		!validSHA256(claim.AnalysisStratumID) ||
		!validSHA256(claim.InterferenceClusterSchemaSHA256) ||
		!validSHA256(claim.InterferenceClusterID) {
		return false
	}
	stratum, err := NewAnalysisStratum(
		claim.OwnerID.String(), claim.ClaimedAt,
		int64(cohort.Admission().TimeBlockSeconds),
	)
	if err != nil || stratum.SchemaSHA256 != claim.AnalysisStratumSchemaSHA256 ||
		stratum.ID != claim.AnalysisStratumID {
		return false
	}
	cluster, err := NewInterferenceCluster(
		claim.OwnerID.String(), claim.EvaluationConversationID.String(),
	)
	return err == nil &&
		cluster.SchemaSHA256 == claim.InterferenceClusterSchemaSHA256 &&
		cluster.ID == claim.InterferenceClusterID
}

func cohortAssignmentHashes(
	actions []ActionProbability,
) ([]string, string, string, error) {
	eligibleActions := make([]string, len(actions))
	for index, action := range actions {
		eligibleActions[index] = action.ActionID
	}
	eligibleJSON, err := json.Marshal(eligibleActions)
	if err != nil {
		return nil, "", "", fmt.Errorf("marshal focal eligible actions: %w", err)
	}
	catalogJSON, err := json.Marshal(actions)
	if err != nil {
		return nil, "", "", fmt.Errorf("marshal focal action catalog: %w", err)
	}
	eligibilitySum := sha256.Sum256(eligibleJSON)
	catalogSum := sha256.Sum256(catalogJSON)
	return eligibleActions, hex.EncodeToString(eligibilitySum[:]),
		hex.EncodeToString(catalogSum[:]), nil
}

func validateFocalCohortAssignment(
	cohort *FocalCohort,
	claim FocalClaim,
	assignment Assignment,
) (Event, error) {
	assignment = canonicalAssignment(assignment)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		return Event{}, fmt.Errorf("construct focal adaptive assignment: %w", err)
	}
	if matchesLegacyFocalAssignment(cohort, claim, assignment) ||
		matchesRandomizedFocalAssignment(cohort, claim, assignment) {
		return event, nil
	}
	return Event{}, ErrFocalClaimConflict
}

func matchesLegacyFocalAssignment(
	cohort *FocalCohort,
	claim FocalClaim,
	assignment Assignment,
) bool {
	actions := cohort.Actions()
	return matchesFocalAssignmentEnvelope(cohort, claim, assignment, actions) &&
		slices.Contains(assignment.EligibleActions, assignment.RecommendedActionID) &&
		slices.Contains(assignment.EligibleActions, assignment.IntendedActionID) &&
		matchesCohortArm(assignment, cohort.Arms())
}

func matchesRandomizedFocalAssignment(
	cohort *FocalCohort,
	claim FocalClaim,
	assignment Assignment,
) bool {
	expectedProbabilities, err := MarginalActionProbabilities(
		cohortActionIDs(cohort.Actions()), StaticActionID, assignment.RecommendedActionID,
	)
	if err != nil {
		return false
	}
	actions := make([]ActionProbability, len(expectedProbabilities))
	for index, probability := range expectedProbabilities {
		value, conversionErr := probability.Probability.Float64()
		if conversionErr != nil {
			return false
		}
		actions[index] = ActionProbability{
			ActionID: probability.ActionID, Probability: value,
		}
	}
	expectedIntendedAction := assignment.RecommendedActionID
	if assignment.ArmID == string(RandomizedArmBaseline) {
		expectedIntendedAction = StaticActionID
	}
	return matchesFocalAssignmentEnvelope(cohort, claim, assignment, actions) &&
		assignment.IntendedActionID == expectedIntendedAction &&
		matchesFrozenArm(assignment)
}

func matchesFocalAssignmentEnvelope(
	cohort *FocalCohort,
	claim FocalClaim,
	assignment Assignment,
	actions []ActionProbability,
) bool {
	eligibleActions, eligibilitySHA256, catalogSHA256, err := cohortAssignmentHashes(actions)
	if err != nil {
		return false
	}
	scope := cohort.Scope()
	predicate := cohort.Predicate()
	return assignment.SchemaVersion == SchemaVersion2 &&
		assignment.OwnerID == claim.OwnerID &&
		assignment.RequestID == claim.RequestID &&
		assignment.AssignmentID == claim.AssignmentID &&
		assignment.Domain == predicate.Domain && assignment.Point == predicate.Point &&
		assignment.PointOrdinal == predicate.Ordinal &&
		assignment.PolicyEpoch == scope.PolicyEpoch &&
		assignment.PolicyVersion == scope.PolicyVersion &&
		assignment.PolicyMode == PolicyCanary &&
		assignment.SnapshotID == scope.SnapshotID &&
		assignment.SnapshotSHA256 == scope.SnapshotSHA256 &&
		assignment.Environment == EvaluationProductionCanary &&
		assignment.Environment == scope.Environment &&
		assignment.ProviderID == scope.ProviderID && assignment.ModelID == scope.ModelID &&
		assignment.CohortID != nil && *assignment.CohortID == cohort.ID() &&
		assignment.ExperimentID == cohort.ExperimentID() && !assignment.Override &&
		assignment.SelectionReason == SelectionCanaryAssignment &&
		assignment.ChampionActionID == StaticActionID &&
		slices.Equal(assignment.EligibleActions, eligibleActions) &&
		slices.Equal(assignment.ActionProbabilities, actions) &&
		assignment.EligibilitySHA256 == eligibilitySHA256 &&
		assignment.CatalogSHA256 == catalogSHA256
}

func matchesCohortArm(assignment Assignment, arms []CohortArm) bool {
	if assignment.ArmProbability == nil {
		return false
	}
	for _, arm := range arms {
		if assignment.ArmID == arm.ArmID &&
			*assignment.ArmProbability == arm.Probability {
			return true
		}
	}
	return false
}

func matchesFrozenArm(assignment Assignment) bool {
	return assignment.ArmProbability != nil &&
		*assignment.ArmProbability == 0.5 &&
		(assignment.ArmID == string(RandomizedArmBaseline) ||
			assignment.ArmID == string(RandomizedArmChallenger))
}

func optionalDBUUID(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	return dbUUID(*value)
}

func optionalUUID(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := uuid.UUID(value.Bytes)
	return &id
}

func optionalUUIDEqual(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
