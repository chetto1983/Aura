package adaptive

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCohortStoreSaveRejectsUnverifiedCohorts(t *testing.T) {
	store := NewCohortStore(nil, nil)

	tests := []struct {
		name   string
		cohort *FocalCohort
	}{
		{name: "nil", cohort: nil},
		{name: "zero value", cohort: &FocalCohort{}},
		{
			name: "forged identity",
			cohort: &FocalCohort{
				id:              uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
				sha256:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				predicateSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				spec:            focalCohortTestSpec(),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := store.Save(t.Context(), test.cohort)
			if !errors.Is(err, ErrUnverifiedCohort) {
				t.Fatalf("Save() error = %v, want ErrUnverifiedCohort", err)
			}
		})
	}
}

func TestCohortStoreIsNilSafe(t *testing.T) {
	request := validFocalEnrollmentRequest(t)
	cohort := mustFocalCohort(t, focalCohortTestSpec())

	var nilStore *CohortStore
	if err := nilStore.Save(t.Context(), cohort); err == nil {
		t.Fatal("nil CohortStore.Save() error = nil")
	}
	if _, err := nilStore.Load(t.Context(), request.OwnerID, request.CohortID); err == nil {
		t.Fatal("nil CohortStore.Load() error = nil")
	}
	if _, err := nilStore.Enroll(t.Context(), request, nil); err == nil {
		t.Fatal("nil CohortStore.Enroll() error = nil")
	}

	store := NewCohortStore(nil, nil)
	if err := store.Save(t.Context(), cohort); err == nil {
		t.Fatal("CohortStore without dependencies Save() error = nil")
	}
	if _, err := store.Load(t.Context(), request.OwnerID, request.CohortID); err == nil {
		t.Fatal("CohortStore without dependencies Load() error = nil")
	}
	if _, err := store.Enroll(t.Context(), request, nil); err == nil {
		t.Fatal("CohortStore without dependencies Enroll() error = nil")
	}
}

func TestFocalEnrollmentRequestRejectsMalformedIdentity(t *testing.T) {
	nilID := uuid.Nil
	tests := []struct {
		name   string
		mutate func(*FocalEnrollmentRequest)
	}{
		{"required identity", func(request *FocalEnrollmentRequest) {
			request.OwnerID = uuid.Nil
		}},
		{"evaluation conversation", func(request *FocalEnrollmentRequest) {
			request.EvaluationConversationID = uuid.Nil
		}},
		{"predicate", func(request *FocalEnrollmentRequest) {
			request.Point = PointReasoning
		}},
		{"session", func(request *FocalEnrollmentRequest) {
			request.SessionID = &nilID
		}},
		{"episode", func(request *FocalEnrollmentRequest) {
			request.EpisodeID = &nilID
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validFocalEnrollmentRequest(t)
			test.mutate(&request)
			if _, err := request.assignmentID(); !errors.Is(err, ErrFocalClaimConflict) {
				t.Fatalf("assignmentID() error = %v, want ErrFocalClaimConflict", err)
			}
		})
	}
}

func TestCohortStoreEnrollRejectsMissingPrivateSeams(t *testing.T) {
	request := validFocalEnrollmentRequest(t)
	store := NewCohortStore(new(pgxpool.Pool), new(Store))

	if _, err := store.Enroll(t.Context(), request, nil); err == nil {
		t.Fatal("Enroll() accepted nil assignment template builder")
	}
	store.writeReceipt = nil
	if _, err := store.Enroll(
		t.Context(), request, focalEnrollmentBuilderForUnitTest,
	); err == nil {
		t.Fatal("Enroll() accepted nil receipt writer")
	}
	store.writeReceipt = insertRandomizationReceipt
	store.writeAssignment = nil
	if _, err := store.Enroll(
		t.Context(), request, focalEnrollmentBuilderForUnitTest,
	); err == nil {
		t.Fatal("Enroll() accepted nil assignment writer")
	}

	transactionFault := errors.New("transaction fault")
	store.writeAssignment = func(
		context.Context,
		*sqlc.Queries,
		Event,
	) (bool, error) {
		return false, nil
	}
	store.transact = func(
		context.Context,
		*pgxpool.Pool,
		string,
		func(*sqlc.Queries) error,
	) error {
		return transactionFault
	}
	if _, err := store.Enroll(
		t.Context(), request, focalEnrollmentBuilderForUnitTest,
	); !errors.Is(err, transactionFault) {
		t.Fatalf("Enroll() transaction error = %v, want injected fault", err)
	}
}

func TestFocalEnrollmentValidationHelpersFailClosed(t *testing.T) {
	request := validFocalEnrollmentRequest(t)
	cohort := mustFocalCohort(t, focalCohortTestSpec())
	assignmentID, err := request.assignmentID()
	if err != nil {
		t.Fatal(err)
	}
	if err := request.validateAgainst(cohort, assignmentID); err != nil {
		t.Fatalf("validateAgainst() valid request error = %v", err)
	}

	missingSession := request
	missingSession.SessionID = nil
	if err := missingSession.validateAgainst(
		cohort, assignmentID,
	); !errors.Is(err, ErrFocalClaimConflict) {
		t.Fatalf("validateAgainst() block mismatch error = %v", err)
	}

	forged := *cohort
	forged.spec.Arms = []CohortArm{
		{ArmID: "baseline", Probability: 1},
		{ArmID: "challenger", Probability: 0},
	}
	if err := request.validateAgainst(
		&forged, assignmentID,
	); err == nil {
		t.Fatal("validateAgainst() accepted non-Bernoulli arms")
	}

	if _, err := focalClaimFromPersistedRow(
		sqlc.AuraAdaptiveFocalCohortClaims{},
	); !errors.Is(err, ErrFocalClaimConflict) {
		t.Fatalf("focalClaimFromPersistedRow() error = %v", err)
	}
	claim := FocalClaim{
		AnalysisStratumSchemaSHA256:     strings.Repeat("a", 64),
		AnalysisStratumID:               strings.Repeat("b", 64),
		InterferenceClusterSchemaSHA256: strings.Repeat("c", 64),
		InterferenceClusterID:           strings.Repeat("d", 64),
	}
	if claim.validRandomizationHashes(cohort) {
		t.Fatal("validRandomizationHashes() accepted invalid canonical hashes")
	}
	if _, _, _, err := cohortAssignmentHashes([]ActionProbability{{
		ActionID: "static", Probability: math.NaN(),
	}}); err == nil {
		t.Fatal("cohortAssignmentHashes() accepted nonfinite probability")
	}
	if matchesCohortArm(Assignment{}, cohort.Arms()) {
		t.Fatal("matchesCohortArm() accepted missing arm probability")
	}
	probability := 0.5
	if matchesCohortArm(
		Assignment{ArmID: "unknown", ArmProbability: &probability}, cohort.Arms(),
	) {
		t.Fatal("matchesCohortArm() accepted unknown arm")
	}
	if _, err := randomizationReceiptFromPersistedRow(
		sqlc.AuraAdaptiveRandomizationReceipts{},
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
	); !errors.Is(err, ErrFocalClaimConflict) {
		t.Fatalf("randomizationReceiptFromPersistedRow() error = %v", err)
	}
}

func validFocalEnrollmentRequest(t *testing.T) FocalEnrollmentRequest {
	t.Helper()
	cohort := mustFocalCohort(t, focalCohortTestSpec())
	sessionID := uuid.Must(uuid.NewV7())
	episodeID := uuid.Must(uuid.NewV7())
	return FocalEnrollmentRequest{
		OwnerID: cohort.Scope().OwnerID, CohortID: cohort.ID(),
		RequestID:                uuid.Must(uuid.NewV7()),
		EvaluationConversationID: uuid.Must(uuid.NewV7()),
		SessionID:                &sessionID, EpisodeID: &episodeID,
		Domain: cohort.Domain(), Point: cohort.Predicate().Point,
		PointOrdinal: cohort.Predicate().Ordinal,
	}
}

func focalEnrollmentBuilderForUnitTest(
	*FocalCohort,
	FocalEnrollmentRequest,
) (AssignmentTemplate, error) {
	return AssignmentTemplate{}, nil
}
