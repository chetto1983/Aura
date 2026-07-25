package adaptive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	ID                       uuid.UUID
	OwnerID                  uuid.UUID
	CohortID                 uuid.UUID
	RequestID                uuid.UUID
	EvaluationConversationID uuid.UUID
	AssignmentID             uuid.UUID
	Domain                   Domain
	Point                    DecisionPoint
	PointOrdinal             uint32
	SessionID                *uuid.UUID
	EpisodeID                *uuid.UUID
	TimeBlockStart           time.Time
	ClaimedAt                time.Time
}

// FocalEnrollment is one claim paired with its validated schema-2 assignment.
type FocalEnrollment struct {
	Claim      FocalClaim
	Assignment Assignment
}

// AssignmentBuilder is trusted in-process code that does only a local arm draw.
// It deliberately receives neither context nor database handles.
type AssignmentBuilder func(*FocalCohort, FocalEnrollmentRequest) (Assignment, error)

// Enroll atomically claims a focal slot and records its schema-2 assignment.
func (store *CohortStore) Enroll(
	ctx context.Context,
	request FocalEnrollmentRequest,
	build AssignmentBuilder,
) (*FocalEnrollment, error) {
	assignmentID, err := request.assignmentID()
	if err != nil {
		return nil, fmt.Errorf("enroll adaptive focal cohort: %w", err)
	}
	if store == nil || store.pool == nil || store.ledger == nil {
		return nil, errors.New("adaptive cohort store requires a database pool and ledger")
	}
	if build == nil {
		return nil, errors.New("adaptive focal enrollment requires an assignment builder")
	}
	var enrollment *FocalEnrollment
	err = db.WithIdentityTx(ctx, store.pool, request.OwnerID.String(), func(q *sqlc.Queries) error {
		if err := lockAdaptiveOwnerTx(ctx, q, request.OwnerID); err != nil {
			return err
		}
		row, err := q.GetAdaptiveFocalCohort(ctx, sqlc.GetAdaptiveFocalCohortParams{
			OwnerID: dbUUID(request.OwnerID), CohortID: dbUUID(request.CohortID),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCohortNotFound
		}
		if err != nil {
			return fmt.Errorf("read adaptive focal cohort: %w", err)
		}
		cohort, err := cohortFromPersistedRow(row, request.OwnerID, request.CohortID)
		if err != nil {
			return err
		}
		if err := request.validateAgainst(cohort, assignmentID); err != nil {
			return err
		}
		if existing, err := loadExistingFocalEnrollment(ctx, q, cohort, request, assignmentID); err != nil {
			return err
		} else if existing != nil {
			enrollment = existing
			return nil
		}
		claimRow, err := q.InsertAdaptiveFocalCohortClaim(ctx, sqlc.InsertAdaptiveFocalCohortClaimParams{
			OwnerID:                  dbUUID(request.OwnerID),
			CohortID:                 dbUUID(request.CohortID),
			RequestID:                dbUUID(request.RequestID),
			EvaluationConversationID: dbUUID(request.EvaluationConversationID),
			AssignmentID:             dbUUID(assignmentID),
			Domain:                   string(request.Domain),
			DecisionPoint:            string(request.Point),
			PointOrdinal:             int64(request.PointOrdinal),
			SessionID:                optionalDBUUID(request.SessionID),
			EpisodeID:                optionalDBUUID(request.EpisodeID),
			TimeBlockStart:           dbTime(time.Unix(0, 0)),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			existing, existingErr := loadExistingFocalEnrollment(ctx, q, cohort, request, assignmentID)
			if existingErr != nil {
				return existingErr
			}
			if existing == nil {
				return ErrFocalClaimConflict
			}
			enrollment = existing
			return nil
		}
		if err != nil {
			return fmt.Errorf("insert adaptive focal claim: %w", err)
		}
		claim, err := focalClaimFromPersistedRow(claimRow)
		if err != nil || !claim.matches(request, assignmentID) {
			return ErrFocalClaimConflict
		}
		orphaned, err := q.LockSchema2AdaptiveAssignmentEvents(ctx, sqlc.LockSchema2AdaptiveAssignmentEventsParams{
			OwnerID: dbUUID(request.OwnerID), AssignmentID: dbUUID(assignmentID),
		})
		if err != nil {
			return fmt.Errorf("lock prior adaptive assignment: %w", err)
		}
		if len(orphaned) != 0 {
			return ErrFocalClaimConflict
		}
		assignment, err := build(cohort, request)
		if err != nil {
			return fmt.Errorf("build focal adaptive assignment: %w", err)
		}
		event, err := validateFocalCohortAssignment(cohort, claim, assignment)
		if err != nil {
			return err
		}
		_, duplicate, err := store.ledger.recordOwnerLockedTx(ctx, q, event)
		if err != nil {
			return fmt.Errorf("record focal adaptive assignment: %w", err)
		}
		if duplicate {
			return ErrFocalClaimConflict
		}
		enrollment = &FocalEnrollment{Claim: claim, Assignment: canonicalAssignment(assignment)}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("enroll adaptive focal cohort %s: %w", request.CohortID, err)
	}
	return enrollment, nil
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

func (request FocalEnrollmentRequest) validateAgainst(cohort *FocalCohort, assignmentID uuid.UUID) error {
	if cohort == nil || cohort.ID() != request.CohortID || cohort.Scope().OwnerID != request.OwnerID ||
		!cohort.MatchesFocalPoint(request.Domain, request.Point, request.PointOrdinal) || assignmentID == uuid.Nil {
		return ErrFocalClaimConflict
	}
	admission := cohort.Admission()
	if slices.Contains(admission.BlockKeys, BlockSession) != (request.SessionID != nil) ||
		slices.Contains(admission.BlockKeys, BlockEpisode) != (request.EpisodeID != nil) {
		return ErrFocalClaimConflict
	}
	return nil
}

func loadExistingFocalEnrollment(
	ctx context.Context,
	q *sqlc.Queries,
	cohort *FocalCohort,
	request FocalEnrollmentRequest,
	assignmentID uuid.UUID,
) (*FocalEnrollment, error) {
	rows, err := q.ListAdaptiveFocalCohortClaimConflicts(ctx, sqlc.ListAdaptiveFocalCohortClaimConflictsParams{
		OwnerID:                  dbUUID(request.OwnerID),
		CohortID:                 dbUUID(request.CohortID),
		RequestID:                dbUUID(request.RequestID),
		EvaluationConversationID: dbUUID(request.EvaluationConversationID),
		AssignmentID:             dbUUID(assignmentID),
	})
	if err != nil {
		return nil, fmt.Errorf("lock adaptive focal claim: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != 1 {
		return nil, ErrFocalClaimConflict
	}
	claim, err := focalClaimFromPersistedRow(rows[0])
	if err != nil {
		return nil, ErrFocalClaimConflict
	}
	if claim.matches(request, assignmentID) {
		assignmentRow, err := q.LockSchema2AdaptiveAssignment(ctx, sqlc.LockSchema2AdaptiveAssignmentParams{
			OwnerID: dbUUID(request.OwnerID), AssignmentID: dbUUID(assignmentID),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrFocalClaimConflict
		}
		if err != nil {
			return nil, fmt.Errorf("lock persisted focal assignment: %w", err)
		}
		assignment, err := assignmentFromPersistedRow(assignmentRow, request.OwnerID, assignmentID)
		if err != nil {
			return nil, ErrFocalClaimConflict
		}
		if _, err := validateFocalCohortAssignment(cohort, claim, assignment); err != nil {
			return nil, ErrFocalClaimConflict
		}
		return &FocalEnrollment{Claim: claim, Assignment: assignment}, nil
	}
	if claim.CohortID == request.CohortID &&
		(claim.RequestID == request.RequestID || claim.AssignmentID == assignmentID) {
		return nil, ErrFocalClaimConflict
	}
	return nil, ErrFocalSlotUnavailable
}

func validateFocalCohortAssignment(cohort *FocalCohort, claim FocalClaim, assignment Assignment) (Event, error) {
	assignment = canonicalAssignment(assignment)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		return Event{}, fmt.Errorf("construct focal adaptive assignment: %w", err)
	}
	actions := cohort.Actions()
	eligibleActions, eligibilitySHA256, catalogSHA256, err := cohortAssignmentHashes(actions)
	if err != nil {
		return Event{}, err
	}
	scope := cohort.Scope()
	predicate := cohort.Predicate()
	cohortID := cohort.ID()
	if assignment.SchemaVersion != SchemaVersion2 || assignment.OwnerID != claim.OwnerID ||
		assignment.RequestID != claim.RequestID || assignment.AssignmentID != claim.AssignmentID ||
		assignment.Domain != predicate.Domain || assignment.Point != predicate.Point ||
		assignment.PointOrdinal != predicate.Ordinal || assignment.PolicyEpoch != scope.PolicyEpoch ||
		assignment.PolicyVersion != scope.PolicyVersion || assignment.PolicyMode != PolicyCanary ||
		assignment.SnapshotID != scope.SnapshotID || assignment.SnapshotSHA256 != scope.SnapshotSHA256 ||
		assignment.Environment != EvaluationProductionCanary || assignment.Environment != scope.Environment ||
		assignment.ProviderID != scope.ProviderID || assignment.ModelID != scope.ModelID ||
		assignment.CohortID == nil || *assignment.CohortID != cohortID ||
		assignment.ExperimentID != cohort.ExperimentID() || assignment.Override ||
		assignment.SelectionReason != SelectionCanaryAssignment || assignment.ChampionActionID != StaticActionID ||
		!slices.Equal(assignment.EligibleActions, eligibleActions) ||
		!slices.Equal(assignment.ActionProbabilities, actions) ||
		assignment.EligibilitySHA256 != eligibilitySHA256 || assignment.CatalogSHA256 != catalogSHA256 ||
		!slices.Contains(eligibleActions, assignment.RecommendedActionID) ||
		!slices.Contains(eligibleActions, assignment.IntendedActionID) ||
		!matchesCohortArm(assignment, cohort.Arms()) {
		return Event{}, ErrFocalClaimConflict
	}
	return event, nil
}

func matchesCohortArm(assignment Assignment, arms []CohortArm) bool {
	if assignment.ArmProbability == nil {
		return false
	}
	for _, arm := range arms {
		if assignment.ArmID == arm.ArmID && *assignment.ArmProbability == arm.Probability {
			return true
		}
	}
	return false
}

func cohortAssignmentHashes(actions []ActionProbability) ([]string, string, string, error) {
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
	return eligibleActions, hex.EncodeToString(eligibilitySum[:]), hex.EncodeToString(catalogSum[:]), nil
}

func focalClaimFromPersistedRow(row sqlc.AuraAdaptiveFocalCohortClaims) (FocalClaim, error) {
	claim := FocalClaim{
		ID:                       uuid.UUID(row.ID.Bytes),
		OwnerID:                  uuid.UUID(row.OwnerID.Bytes),
		CohortID:                 uuid.UUID(row.CohortID.Bytes),
		RequestID:                uuid.UUID(row.RequestID.Bytes),
		EvaluationConversationID: uuid.UUID(row.EvaluationConversationID.Bytes),
		AssignmentID:             uuid.UUID(row.AssignmentID.Bytes),
		Domain:                   Domain(row.Domain),
		Point:                    DecisionPoint(row.DecisionPoint),
		TimeBlockStart:           row.TimeBlockStart.Time,
		ClaimedAt:                row.ClaimedAt.Time,
	}
	if row.PointOrdinal < 0 || row.PointOrdinal > int64(^uint32(0)) || !row.ID.Valid ||
		!row.OwnerID.Valid || !row.CohortID.Valid || !row.RequestID.Valid ||
		!row.EvaluationConversationID.Valid || !row.AssignmentID.Valid ||
		!row.TimeBlockStart.Valid || !row.ClaimedAt.Valid {
		return FocalClaim{}, ErrFocalClaimConflict
	}
	claim.PointOrdinal = uint32(row.PointOrdinal)
	claim.SessionID = optionalUUID(row.SessionID)
	claim.EpisodeID = optionalUUID(row.EpisodeID)
	return claim, nil
}

func (claim FocalClaim) matches(request FocalEnrollmentRequest, assignmentID uuid.UUID) bool {
	return claim.OwnerID == request.OwnerID && claim.CohortID == request.CohortID &&
		claim.RequestID == request.RequestID && claim.EvaluationConversationID == request.EvaluationConversationID &&
		claim.AssignmentID == assignmentID && claim.Domain == request.Domain && claim.Point == request.Point &&
		claim.PointOrdinal == request.PointOrdinal && optionalUUIDEqual(claim.SessionID, request.SessionID) &&
		optionalUUIDEqual(claim.EpisodeID, request.EpisodeID)
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
