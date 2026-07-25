package adaptive

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type identityTransactor func(
	context.Context,
	*pgxpool.Pool,
	string,
	func(*sqlc.Queries) error,
) error

type randomizationReceiptWriter func(
	context.Context,
	*sqlc.Queries,
	RandomizationReceipt,
) error

type randomizationAssignmentWriter func(
	context.Context,
	*sqlc.Queries,
	Event,
) (bool, error)

// Enroll atomically claims, randomizes, and persists the complete durable tuple.
func (store *CohortStore) Enroll(
	ctx context.Context,
	request FocalEnrollmentRequest,
	build AssignmentTemplateBuilder,
) (*FocalEnrollment, error) {
	assignmentID, err := request.assignmentID()
	if err != nil {
		return nil, fmt.Errorf("enroll adaptive focal cohort: %w", err)
	}
	if store == nil || store.pool == nil || store.ledger == nil ||
		store.entropy == nil || store.transact == nil || store.writeAssignment == nil {
		return nil, errors.New(
			"adaptive cohort store requires a database pool, ledger, entropy, assignment writer, and transaction boundary",
		)
	}
	if build == nil {
		return nil, errors.New("adaptive focal enrollment requires an assignment template builder")
	}
	if store.writeReceipt == nil {
		return nil, errors.New("adaptive cohort store requires a receipt writer")
	}

	var enrollment *FocalEnrollment
	prepared := false
	err = store.transact(
		ctx,
		store.pool,
		request.OwnerID.String(),
		func(queries *sqlc.Queries) error {
			var err error
			enrollment, err = store.enrollOwnerLocked(
				ctx, queries, request, assignmentID, build,
			)
			if err == nil {
				prepared = true
			}
			return err
		},
	)
	if err == nil {
		return enrollment, nil
	}
	if prepared {
		recovered, recoveryErr := store.reloadRandomizedEnrollment(
			ctx, request, assignmentID,
		)
		switch {
		case recoveryErr != nil:
			return nil, fmt.Errorf(
				"recover adaptive focal cohort %s after unresolved commit: %w",
				request.CohortID, recoveryErr,
			)
		case recovered != nil:
			return recovered, nil
		}
	}
	return nil, fmt.Errorf("enroll adaptive focal cohort %s: %w", request.CohortID, err)
}

func (store *CohortStore) enrollOwnerLocked(
	ctx context.Context,
	queries *sqlc.Queries,
	request FocalEnrollmentRequest,
	assignmentID uuid.UUID,
	build AssignmentTemplateBuilder,
) (*FocalEnrollment, error) {
	if err := lockAdaptiveOwnerTx(ctx, queries, request.OwnerID); err != nil {
		return nil, err
	}
	row, err := queries.GetAdaptiveFocalCohort(
		ctx,
		sqlc.GetAdaptiveFocalCohortParams{
			OwnerID: dbUUID(request.OwnerID), CohortID: dbUUID(request.CohortID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCohortNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read adaptive focal cohort: %w", err)
	}
	cohort, err := cohortFromPersistedRow(row, request.OwnerID, request.CohortID)
	if err != nil {
		return nil, err
	}
	if !cohort.randomizationEligible() {
		return nil, ErrCohortV1RandomizationUnsupported
	}
	if err := request.validateAgainst(cohort, assignmentID); err != nil {
		return nil, err
	}
	existing, err := loadExistingRandomizedEnrollment(
		ctx, queries, cohort, request, assignmentID,
	)
	if err != nil || existing != nil {
		return existing, err
	}

	claimRow, err := queries.InsertAdaptiveFocalCohortClaim(
		ctx,
		sqlc.InsertAdaptiveFocalCohortClaimParams{
			OwnerID: dbUUID(request.OwnerID), CohortID: dbUUID(request.CohortID),
			RequestID:                dbUUID(request.RequestID),
			EvaluationConversationID: dbUUID(request.EvaluationConversationID),
			AssignmentID:             dbUUID(assignmentID), Domain: string(request.Domain),
			DecisionPoint: string(request.Point), PointOrdinal: int64(request.PointOrdinal),
			SessionID: optionalDBUUID(request.SessionID), EpisodeID: optionalDBUUID(request.EpisodeID),
			TimeBlockStart: dbTime(timeZeroUTC),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := loadExistingRandomizedEnrollment(
			ctx, queries, cohort, request, assignmentID,
		)
		if loadErr != nil {
			return nil, loadErr
		}
		if existing == nil {
			return nil, ErrFocalClaimConflict
		}
		return existing, nil
	}
	if err != nil {
		return nil, fmt.Errorf("insert adaptive focal claim: %w", err)
	}
	claim, err := focalClaimFromPersistedRow(claimRow)
	if err != nil || !claim.matches(request, assignmentID) ||
		!claim.validRandomizationHashes(cohort) {
		return nil, fmt.Errorf("validate persisted focal claim: %w", ErrFocalClaimConflict)
	}
	if err := rejectOrphanedRandomizationState(
		ctx, queries, request.OwnerID, assignmentID,
	); err != nil {
		return nil, fmt.Errorf("validate fresh randomization slot: %w", err)
	}

	template, err := build(cohort, request)
	if err != nil {
		return nil, fmt.Errorf("build focal adaptive assignment template: %w", err)
	}
	snapshotRow, err := queries.GetAdaptivePolicySnapshot(
		ctx,
		sqlc.GetAdaptivePolicySnapshotParams{
			OwnerID: dbUUID(request.OwnerID), SnapshotID: dbUUID(cohort.Scope().SnapshotID),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("load focal adaptive snapshot: %w", err)
	}
	snapshot, err := snapshotFromPersistedRow(
		snapshotRow, request.OwnerID, cohort.Scope().SnapshotID,
	)
	if err != nil {
		return nil, err
	}
	alternatives, err := buildRandomizationAlternatives(
		cohort, snapshot, request, claim, template,
	)
	if err != nil {
		return nil, err
	}
	selection, err := drawRandomizedArm(store.entropy)
	if err != nil {
		return nil, err
	}
	assignment, receipt, err := selectedRandomization(alternatives, selection)
	if err != nil {
		return nil, err
	}
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		return nil, err
	}
	if duplicate, err := store.writeAssignment(ctx, queries, event); err != nil {
		return nil, fmt.Errorf("record focal adaptive assignment: %w", err)
	} else if duplicate {
		return nil, ErrFocalClaimConflict
	}
	if err := store.writeReceipt(ctx, queries, receipt); err != nil {
		return nil, fmt.Errorf("persist adaptive randomization receipt: %w", err)
	}
	return &FocalEnrollment{
		Claim: claim, Assignment: canonicalAssignment(assignment), Receipt: receipt,
	}, nil
}

func (store *CohortStore) reloadRandomizedEnrollment(
	ctx context.Context,
	request FocalEnrollmentRequest,
	assignmentID uuid.UUID,
) (*FocalEnrollment, error) {
	var enrollment *FocalEnrollment
	err := db.WithIdentityTx(
		ctx, store.pool, request.OwnerID.String(),
		func(queries *sqlc.Queries) error {
			if err := lockAdaptiveOwnerTx(ctx, queries, request.OwnerID); err != nil {
				return err
			}
			row, err := queries.GetAdaptiveFocalCohort(
				ctx,
				sqlc.GetAdaptiveFocalCohortParams{
					OwnerID: dbUUID(request.OwnerID), CohortID: dbUUID(request.CohortID),
				},
			)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			cohort, err := cohortFromPersistedRow(row, request.OwnerID, request.CohortID)
			if err != nil {
				return err
			}
			if !cohort.randomizationEligible() {
				return ErrCohortV1RandomizationUnsupported
			}
			enrollment, err = loadExistingRandomizedEnrollment(
				ctx, queries, cohort, request, assignmentID,
			)
			return err
		},
	)
	return enrollment, err
}

func loadExistingRandomizedEnrollment(
	ctx context.Context,
	queries *sqlc.Queries,
	cohort *FocalCohort,
	request FocalEnrollmentRequest,
	assignmentID uuid.UUID,
) (*FocalEnrollment, error) {
	rows, err := queries.ListAdaptiveFocalCohortClaimConflicts(
		ctx,
		sqlc.ListAdaptiveFocalCohortClaimConflictsParams{
			OwnerID: dbUUID(request.OwnerID), CohortID: dbUUID(request.CohortID),
			RequestID:                dbUUID(request.RequestID),
			EvaluationConversationID: dbUUID(request.EvaluationConversationID),
			AssignmentID:             dbUUID(assignmentID),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("lock adaptive focal claim: %w", err)
	}
	if len(rows) == 0 {
		if err := rejectOrphanedRandomizationState(
			ctx, queries, request.OwnerID, assignmentID,
		); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if len(rows) != 1 {
		return nil, ErrFocalClaimConflict
	}
	claim, err := focalClaimFromPersistedRow(rows[0])
	if err != nil {
		return nil, ErrFocalClaimConflict
	}
	if !claim.matches(request, assignmentID) {
		if claim.CohortID == request.CohortID &&
			(claim.RequestID == request.RequestID || claim.AssignmentID == assignmentID) {
			return nil, ErrFocalClaimConflict
		}
		return nil, ErrFocalSlotUnavailable
	}
	if !claim.validRandomizationHashes(cohort) {
		return nil, fmt.Errorf("reload focal claim hashes: %w", ErrFocalClaimConflict)
	}
	receiptRow, err := queries.LockAdaptiveRandomizationReceipt(
		ctx,
		sqlc.LockAdaptiveRandomizationReceiptParams{
			OwnerID: dbUUID(request.OwnerID), AssignmentID: dbUUID(assignmentID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("reload missing randomization receipt: %w", ErrFocalClaimConflict)
	}
	if err != nil {
		return nil, fmt.Errorf("lock adaptive randomization receipt: %w", err)
	}
	receipt, err := randomizationReceiptFromPersistedRow(
		receiptRow, request.OwnerID, request.CohortID, request.RequestID,
		claim.ID, assignmentID,
	)
	if err != nil {
		return nil, fmt.Errorf("reload randomization receipt: %w", err)
	}
	assignmentRow, err := queries.LockSchema2AdaptiveAssignment(
		ctx,
		sqlc.LockSchema2AdaptiveAssignmentParams{
			OwnerID: dbUUID(request.OwnerID), AssignmentID: dbUUID(assignmentID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("reload missing assignment: %w", ErrFocalClaimConflict)
	}
	if err != nil {
		return nil, fmt.Errorf("lock persisted focal assignment: %w", err)
	}
	assignment, err := assignmentFromPersistedRow(
		assignmentRow, request.OwnerID, assignmentID,
	)
	if err != nil {
		return nil, fmt.Errorf("reload assignment: %w", ErrFocalClaimConflict)
	}
	snapshotRow, err := queries.GetAdaptivePolicySnapshot(
		ctx,
		sqlc.GetAdaptivePolicySnapshotParams{
			OwnerID: dbUUID(request.OwnerID), SnapshotID: dbUUID(cohort.Scope().SnapshotID),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("reload snapshot row: %w", ErrFocalClaimConflict)
	}
	snapshot, err := snapshotFromPersistedRow(
		snapshotRow, request.OwnerID, cohort.Scope().SnapshotID,
	)
	if err != nil {
		return nil, fmt.Errorf("reload snapshot: %w", ErrFocalClaimConflict)
	}
	if err := validatePersistedRandomization(
		cohort, snapshot, request, claim, assignment, receipt,
	); err != nil {
		return nil, fmt.Errorf("reload randomized tuple: %w", err)
	}
	return &FocalEnrollment{Claim: claim, Assignment: assignment, Receipt: receipt}, nil
}

func rejectOrphanedRandomizationState(
	ctx context.Context,
	queries *sqlc.Queries,
	ownerID uuid.UUID,
	assignmentID uuid.UUID,
) error {
	_, receiptErr := queries.LockAdaptiveRandomizationReceipt(
		ctx,
		sqlc.LockAdaptiveRandomizationReceiptParams{
			OwnerID: dbUUID(ownerID), AssignmentID: dbUUID(assignmentID),
		},
	)
	if receiptErr != nil && !errors.Is(receiptErr, pgx.ErrNoRows) {
		return fmt.Errorf("lock prior randomization receipt: %w", receiptErr)
	}
	assignments, err := queries.LockSchema2AdaptiveAssignmentEvents(
		ctx,
		sqlc.LockSchema2AdaptiveAssignmentEventsParams{
			OwnerID: dbUUID(ownerID), AssignmentID: dbUUID(assignmentID),
		},
	)
	if err != nil {
		return fmt.Errorf("lock prior adaptive assignment: %w", err)
	}
	if receiptErr == nil || len(assignments) != 0 {
		return ErrFocalClaimConflict
	}
	return nil
}

func insertRandomizationReceipt(
	ctx context.Context,
	queries *sqlc.Queries,
	receipt RandomizationReceipt,
) error {
	artifact, digest, err := canonicalRandomizationReceipt(receipt)
	if err != nil {
		return err
	}
	planDigest, _ := hex.DecodeString(receipt.RandomizationPlanSHA256)
	stratumSchemaDigest, _ := hex.DecodeString(receipt.AnalysisStratumSchemaSHA256)
	stratumDigest, _ := hex.DecodeString(receipt.AnalysisStratumID)
	row, err := queries.InsertAdaptiveRandomizationReceipt(
		ctx,
		sqlc.InsertAdaptiveRandomizationReceiptParams{
			ReceiptID:                   dbUUID(deterministicReceiptID(uuid.MustParse(receipt.AssignmentID))),
			OwnerID:                     dbUUID(uuid.MustParse(receipt.OwnerID)),
			CohortID:                    dbUUID(uuid.MustParse(receipt.CohortID)),
			RequestID:                   dbUUID(uuid.MustParse(receipt.RequestID)),
			ClaimID:                     dbUUID(uuid.MustParse(receipt.ClaimID)),
			AssignmentID:                dbUUID(uuid.MustParse(receipt.AssignmentID)),
			RandomizationPlanSha256:     planDigest,
			AnalysisStratumSchemaSha256: stratumSchemaDigest,
			AnalysisStratumID:           stratumDigest, ReceiptSha256: digest,
			Artifact: artifact, ArtifactJson: artifact,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("receipt identity already exists: %w", ErrFocalClaimConflict)
	}
	if err != nil {
		return fmt.Errorf("insert adaptive randomization receipt: %w", err)
	}
	persisted, err := randomizationReceiptFromPersistedRow(
		row, uuid.MustParse(receipt.OwnerID), uuid.MustParse(receipt.CohortID),
		uuid.MustParse(receipt.RequestID), uuid.MustParse(receipt.ClaimID),
		uuid.MustParse(receipt.AssignmentID),
	)
	if err != nil {
		return fmt.Errorf("validate inserted randomization receipt: %w", err)
	}
	persistedArtifact, _, err := canonicalRandomizationReceipt(persisted)
	if err != nil || !bytes.Equal(persistedArtifact, artifact) {
		return fmt.Errorf("compare inserted randomization receipt: %w", ErrFocalClaimConflict)
	}
	return nil
}

var timeZeroUTC = time.Unix(0, 0).UTC()
