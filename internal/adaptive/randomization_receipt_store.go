package adaptive

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
)

func randomizationReceiptFromPersistedRow(
	row sqlc.AuraAdaptiveRandomizationReceipts,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	requestID uuid.UUID,
	claimID uuid.UUID,
	assignmentID uuid.UUID,
) (RandomizationReceipt, error) {
	if !row.ID.Valid || uuid.UUID(row.ID.Bytes) != deterministicReceiptID(assignmentID) ||
		!row.OwnerID.Valid || uuid.UUID(row.OwnerID.Bytes) != ownerID ||
		!row.CohortID.Valid || uuid.UUID(row.CohortID.Bytes) != cohortID ||
		!row.RequestID.Valid || uuid.UUID(row.RequestID.Bytes) != requestID ||
		!row.ClaimID.Valid || uuid.UUID(row.ClaimID.Bytes) != claimID ||
		!row.AssignmentID.Valid || uuid.UUID(row.AssignmentID.Bytes) != assignmentID ||
		!row.CreatedAt.Valid {
		return RandomizationReceipt{}, fmt.Errorf(
			"persisted receipt tuple metadata: %w", ErrFocalClaimConflict,
		)
	}
	receipt, err := DecodeRandomizationReceipt(row.Artifact)
	if err != nil {
		return RandomizationReceipt{}, fmt.Errorf(
			"persisted receipt artifact: %w: %v", ErrFocalClaimConflict, err,
		)
	}
	var artifactValue any
	var projectionValue any
	if err := json.Unmarshal(row.Artifact, &artifactValue); err != nil {
		return RandomizationReceipt{}, fmt.Errorf(
			"persisted receipt artifact JSON: %w: %v", ErrFocalClaimConflict, err,
		)
	}
	if err := json.Unmarshal(row.ArtifactJson, &projectionValue); err != nil {
		return RandomizationReceipt{}, fmt.Errorf(
			"persisted receipt projection: %w: %v", ErrFocalClaimConflict, err,
		)
	}
	if !reflect.DeepEqual(artifactValue, projectionValue) {
		return RandomizationReceipt{}, fmt.Errorf(
			"persisted receipt projection mismatch: %w", ErrFocalClaimConflict,
		)
	}
	_, digest, err := canonicalRandomizationReceipt(receipt)
	if err != nil || !bytes.Equal(row.Sha256, digest) {
		return RandomizationReceipt{}, fmt.Errorf(
			"persisted receipt digest: %w", ErrFocalClaimConflict,
		)
	}
	plan, planErr := hex.DecodeString(receipt.RandomizationPlanSHA256)
	stratumSchema, schemaErr := hex.DecodeString(receipt.AnalysisStratumSchemaSHA256)
	stratum, stratumErr := hex.DecodeString(receipt.AnalysisStratumID)
	if planErr != nil || schemaErr != nil || stratumErr != nil ||
		!bytes.Equal(row.RandomizationPlanSha256, plan) ||
		!bytes.Equal(row.AnalysisStratumSchemaSha256, stratumSchema) ||
		!bytes.Equal(row.AnalysisStratumID, stratum) {
		return RandomizationReceipt{}, fmt.Errorf(
			"persisted receipt hash columns: %w", ErrFocalClaimConflict,
		)
	}
	return receipt, nil
}

func validatePersistedRandomization(
	cohort *FocalCohort,
	snapshot *PolicySnapshot,
	request FocalEnrollmentRequest,
	claim FocalClaim,
	assignment Assignment,
	receipt RandomizationReceipt,
) error {
	features := make([]SnapshotFeatureValue, 0, len(snapshot.FeatureSchema()))
	for _, definition := range snapshot.FeatureSchema() {
		value, exists := assignment.Features[definition.Key]
		if !exists {
			return ErrFocalClaimConflict
		}
		features = append(features, SnapshotFeatureValue{Key: definition.Key, Value: value})
	}
	alternatives, err := buildRandomizationAlternatives(
		cohort, snapshot, request, claim, AssignmentTemplate{Features: features},
	)
	if err != nil {
		return err
	}
	draw, err := hex.DecodeString(receipt.DrawByte)
	if err != nil || len(draw) != 1 {
		return ErrFocalClaimConflict
	}
	selection, err := drawRandomizedArm(bytes.NewReader(draw))
	if err != nil {
		return err
	}
	expectedAssignment, expectedReceipt, err := selectedRandomization(alternatives, selection)
	if err != nil {
		return err
	}
	expectedEvent, err := NewAssignmentEvent(expectedAssignment)
	if err != nil {
		return err
	}
	actualEvent, err := NewAssignmentEvent(assignment)
	if err != nil || expectedEvent.ID != actualEvent.ID ||
		!bytes.Equal(expectedEvent.Payload, actualEvent.Payload) {
		return ErrFocalClaimConflict
	}
	expectedArtifact, _, err := canonicalRandomizationReceipt(expectedReceipt)
	if err != nil {
		return err
	}
	actualArtifact, _, err := canonicalRandomizationReceipt(receipt)
	if err != nil || !bytes.Equal(expectedArtifact, actualArtifact) {
		return ErrFocalClaimConflict
	}
	return nil
}
