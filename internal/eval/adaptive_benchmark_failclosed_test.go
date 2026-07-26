package eval

import (
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/google/uuid"
)

func TestAdaptiveBenchmarkCompositionFailsClosed(t *testing.T) {
	t.Parallel()
	if workflow, err := NewAdaptiveBenchmark(
		&adaptive.CohortStore{},
		&adaptive.EvidenceStore{},
	); err != nil || workflow == nil {
		t.Fatalf("NewAdaptiveBenchmark() = %#v, %v", workflow, err)
	}
	if workflow, err := NewAdaptiveBenchmark(
		nil,
		&adaptive.EvidenceStore{},
	); workflow != nil || !errors.Is(err, ErrAdaptiveBenchmarkUnavailable) {
		t.Fatalf("NewAdaptiveBenchmark(nil) = %#v, %v", workflow, err)
	}
	if workflow, err := NewAdaptiveBenchmark(
		&adaptive.CohortStore{},
		nil,
	); workflow != nil || !errors.Is(err, ErrAdaptiveBenchmarkUnavailable) {
		t.Fatalf("NewAdaptiveBenchmark(nil evidence) = %#v, %v", workflow, err)
	}
}

func TestAdaptiveBenchmarkVerifyCohortFailsClosed(t *testing.T) {
	t.Parallel()
	cohort := benchmarkCohort(t)
	valid := adaptive.CohortLedger{
		CohortID: cohort.ID(), CohortSHA256: cohort.SHA256(),
		State: adaptive.CohortLedgerClosed, SHA256: strings.Repeat("1", 64),
	}
	poisoned := valid
	poisoned.PoisonedMemberCount = 1
	badLink := valid
	badLink.Members = []adaptive.CohortMember{{
		Claim:      adaptive.FocalClaim{AssignmentID: uuid.Must(uuid.NewV7())},
		Assignment: adaptive.Assignment{AssignmentID: uuid.Must(uuid.NewV7())},
	}}
	reconstructErr := errors.New("ledger unavailable")
	tests := []struct {
		name     string
		workflow *AdaptiveBenchmark
		ownerID  uuid.UUID
		cohortID uuid.UUID
		want     error
	}{
		{
			name: "nil workflow", ownerID: cohort.Scope().OwnerID,
			cohortID: cohort.ID(), want: ErrAdaptiveBenchmarkUnavailable,
		},
		{
			name: "nil owner",
			workflow: newAdaptiveBenchmark(
				&benchmarkCohortStoreFake{},
				nil,
			),
			cohortID: cohort.ID(), want: adaptive.ErrCohortNotFound,
		},
		{
			name: "reconstruct error", ownerID: cohort.Scope().OwnerID,
			cohortID: cohort.ID(),
			workflow: newAdaptiveBenchmark(
				&benchmarkCohortStoreFake{reconstructErr: reconstructErr},
				nil,
			),
			want: reconstructErr,
		},
		{
			name: "invalid hash", ownerID: cohort.Scope().OwnerID,
			cohortID: cohort.ID(),
			workflow: newAdaptiveBenchmark(
				&benchmarkCohortStoreFake{ledger: adaptive.CohortLedger{
					CohortID: cohort.ID(), CohortSHA256: "not-a-hash",
					State:  adaptive.CohortLedgerClosed,
					SHA256: strings.Repeat("1", 64),
				}},
				nil,
			),
			want: errors.New("adaptive benchmark reconstructed ledger is invalid"),
		},
		{
			name: "poisoned member", ownerID: cohort.Scope().OwnerID,
			cohortID: cohort.ID(),
			workflow: newAdaptiveBenchmark(
				&benchmarkCohortStoreFake{ledger: poisoned},
				nil,
			),
			want: errors.New("adaptive benchmark reconstructed ledger is invalid"),
		},
		{
			name: "link mismatch", ownerID: cohort.Scope().OwnerID,
			cohortID: cohort.ID(),
			workflow: newAdaptiveBenchmark(
				&benchmarkCohortStoreFake{ledger: badLink},
				nil,
			),
			want: errors.New("adaptive benchmark member linkage is invalid"),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := testCase.workflow.VerifyCohort(
				t.Context(),
				testCase.ownerID,
				testCase.cohortID,
			)
			if err == nil ||
				(!errors.Is(err, testCase.want) &&
					err.Error() != testCase.want.Error()) {
				t.Fatalf("VerifyCohort() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestAdaptiveBenchmarkSealAdmissionFailsClosedAfterValidation(
	t *testing.T,
) {
	t.Parallel()
	cohort := benchmarkCohort(t)
	valid := benchmarkAdmissionRequest(t, cohort)
	validContext := benchmarkAdmissionOperationContext(t, valid)
	sealErr := errors.New("seal unavailable")
	badHash := valid
	badHash.Artifacts = append(
		[]adaptive.EvidenceArtifactRegistration(nil),
		valid.Artifacts...,
	)
	badHash.Artifacts[0].Ref.ArtifactSHA256 = strings.Repeat("f", 64)
	tests := []struct {
		name     string
		workflow *AdaptiveBenchmark
		request  AdaptiveBenchmarkAdmissionRequest
		want     error
	}{
		{
			name: "nil workflow", request: valid,
			want: ErrAdaptiveBenchmarkUnavailable,
		},
		{
			name: "nil owner",
			workflow: newAdaptiveBenchmark(
				nil,
				&benchmarkEvidenceStoreFake{},
			),
			request: AdaptiveBenchmarkAdmissionRequest{
				CohortID: valid.CohortID, Artifacts: valid.Artifacts,
			},
			want: ErrAdaptiveBenchmarkAdmissionInput,
		},
		{
			name: "artifact hash", request: badHash,
			workflow: newAdaptiveBenchmark(
				nil,
				&benchmarkEvidenceStoreFake{},
			),
			want: ErrAdaptiveBenchmarkAdmissionInput,
		},
		{
			name: "seal error", request: valid,
			workflow: newAdaptiveBenchmark(
				nil,
				&benchmarkEvidenceStoreFake{sealErr: sealErr},
			),
			want: sealErr,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := testCase.workflow.SealAdmission(
				validContext,
				testCase.request,
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("SealAdmission() error = %v, want %v", err, testCase.want)
			}
		})
	}
}
