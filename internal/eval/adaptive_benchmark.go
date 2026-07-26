package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/google/uuid"
)

// AdaptiveBenchmarkAdmissionCommand is the only authorized admission invocation.
const AdaptiveBenchmarkAdmissionCommand = "aura eval adaptive seal-admission"

var (
	// ErrAdaptiveBenchmarkUnavailable reports incomplete production composition.
	ErrAdaptiveBenchmarkUnavailable = errors.New("adaptive benchmark is unavailable")
	// ErrAdaptiveBenchmarkCohortOpen prevents a pre-cutoff view from looking verified.
	ErrAdaptiveBenchmarkCohortOpen = errors.New("adaptive benchmark cohort is still open")
	// ErrAdaptiveBenchmarkAdmissionInput rejects incomplete or unapproved admission.
	ErrAdaptiveBenchmarkAdmissionInput = errors.New(
		"adaptive benchmark admission input is invalid",
	)
)

var admissionArtifactOrder = []string{
	"interference_plan",
	"look_plan",
	"quality_outcome_model",
	"harm_outcome_model",
	"ope_plan",
}

type adaptiveBenchmarkCohortStore interface {
	Reconstruct(context.Context, uuid.UUID, uuid.UUID) (adaptive.CohortLedger, error)
}

type adaptiveBenchmarkEvidenceStore interface {
	SealCanaryAdmission(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		adaptive.EvidenceSealer,
		string,
		[]adaptive.EvidenceArtifactRegistration,
	) (adaptive.CanaryAdmissionEvidence, error)
}

// AdaptiveBenchmark is Aura's DB-backed operator workflow for typed benchmark facts.
// It verifies or appends evidence only; it has no serving-policy mutation dependency.
type AdaptiveBenchmark struct {
	cohorts  adaptiveBenchmarkCohortStore
	evidence adaptiveBenchmarkEvidenceStore
}

// NewAdaptiveBenchmark composes the real cohort and evidence stores.
func NewAdaptiveBenchmark(
	cohorts *adaptive.CohortStore,
	evidence *adaptive.EvidenceStore,
) (*AdaptiveBenchmark, error) {
	if cohorts == nil || evidence == nil {
		return nil, ErrAdaptiveBenchmarkUnavailable
	}
	return newAdaptiveBenchmark(cohorts, evidence), nil
}

func newAdaptiveBenchmark(
	cohorts adaptiveBenchmarkCohortStore,
	evidence adaptiveBenchmarkEvidenceStore,
) *AdaptiveBenchmark {
	return &AdaptiveBenchmark{cohorts: cohorts, evidence: evidence}
}

// AdaptiveBenchmarkMemberLinkage contains only registered identities and status.
type AdaptiveBenchmarkMemberLinkage struct {
	ClaimID         uuid.UUID `json:"claim_id"`
	RequestID       uuid.UUID `json:"request_id"`
	AssignmentID    uuid.UUID `json:"assignment_id"`
	ArmID           string    `json:"arm_id"`
	IntendedAction  string    `json:"intended_action_id"`
	DeliveryEventID uuid.UUID `json:"delivery_event_id"`
	QualityEventID  uuid.UUID `json:"quality_event_id"`
	HarmEventID     uuid.UUID `json:"harm_event_id"`
	Observed        bool      `json:"observed"`
	ITTEligible     bool      `json:"itt_eligible"`
	OPEEligible     bool      `json:"ope_eligible"`
	ReasonCodes     []string  `json:"reason_codes"`
}

// AdaptiveBenchmarkVerification is a content-free closed-ledger verification report.
type AdaptiveBenchmarkVerification struct {
	CohortID            uuid.UUID                        `json:"cohort_id"`
	CohortSHA256        string                           `json:"cohort_sha256"`
	ClosedLedgerSHA256  string                           `json:"closed_ledger_sha256"`
	MemberCount         int                              `json:"member_count"`
	IncludedFactCount   int                              `json:"included_fact_count"`
	LateFactCount       int                              `json:"late_fact_count"`
	CensoredMemberCount int                              `json:"censored_member_count"`
	Sealable            bool                             `json:"sealable"`
	Members             []AdaptiveBenchmarkMemberLinkage `json:"members"`
}

// VerifyCohort reconstructs and validates a closed cohort from immutable ledger truth.
func (workflow *AdaptiveBenchmark) VerifyCohort(
	ctx context.Context,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
) (AdaptiveBenchmarkVerification, error) {
	if workflow == nil || workflow.cohorts == nil {
		return AdaptiveBenchmarkVerification{}, ErrAdaptiveBenchmarkUnavailable
	}
	if ownerID == uuid.Nil || cohortID == uuid.Nil {
		return AdaptiveBenchmarkVerification{}, adaptive.ErrCohortNotFound
	}
	ledger, err := workflow.cohorts.Reconstruct(ctx, ownerID, cohortID)
	if err != nil {
		return AdaptiveBenchmarkVerification{}, fmt.Errorf(
			"verify adaptive benchmark cohort: %w", err,
		)
	}
	if ledger.State == adaptive.CohortLedgerOpen {
		return AdaptiveBenchmarkVerification{}, ErrAdaptiveBenchmarkCohortOpen
	}
	if ledger.State != adaptive.CohortLedgerClosed ||
		ledger.CohortID != cohortID ||
		!validBenchmarkSHA256(ledger.CohortSHA256) ||
		!validBenchmarkSHA256(ledger.SHA256) ||
		ledger.PoisonedMemberCount != 0 {
		return AdaptiveBenchmarkVerification{}, errors.New(
			"adaptive benchmark reconstructed ledger is invalid",
		)
	}
	report := AdaptiveBenchmarkVerification{
		CohortID: cohortID, CohortSHA256: ledger.CohortSHA256,
		ClosedLedgerSHA256: ledger.SHA256, MemberCount: len(ledger.Members),
		IncludedFactCount:   len(ledger.IncludedFacts),
		LateFactCount:       ledger.LateFactCount,
		CensoredMemberCount: ledger.CensorDiagnostics.Censored,
		Sealable:            ledger.Sealable,
		Members: make(
			[]AdaptiveBenchmarkMemberLinkage, len(ledger.Members),
		),
	}
	for index, member := range ledger.Members {
		if member.Claim.AssignmentID == uuid.Nil ||
			member.Claim.AssignmentID != member.Assignment.AssignmentID {
			return AdaptiveBenchmarkVerification{}, errors.New(
				"adaptive benchmark member linkage is invalid",
			)
		}
		linkage := AdaptiveBenchmarkMemberLinkage{
			ClaimID: member.Claim.ID, RequestID: member.Claim.RequestID,
			AssignmentID:   member.Assignment.AssignmentID,
			ArmID:          member.Assignment.ArmID,
			IntendedAction: member.Assignment.IntendedActionID,
			Observed:       member.Observed, ITTEligible: member.ITTEligible,
			OPEEligible: member.OPEEligible,
			ReasonCodes: slices.Clone(member.ReasonCodes),
		}
		if member.Delivery != nil {
			linkage.DeliveryEventID = member.Delivery.EventID
		}
		if member.Quality != nil {
			linkage.QualityEventID = member.Quality.EventID
		}
		if member.Harm != nil {
			linkage.HarmEventID = member.Harm.EventID
		}
		report.Members[index] = linkage
	}
	return report, nil
}

// AdaptiveBenchmarkAdmissionRequest contains the exact cohort child bytes.
type AdaptiveBenchmarkAdmissionRequest struct {
	Command        string
	Confirmed      bool
	ManifestSHA256 string
	OwnerID        uuid.UUID
	CohortID       uuid.UUID
	Artifacts      []adaptive.EvidenceArtifactRegistration
}

// SealAdmission delegates the complete admission mutation to one evidence transaction.
func (workflow *AdaptiveBenchmark) SealAdmission(
	ctx context.Context,
	request AdaptiveBenchmarkAdmissionRequest,
) (adaptive.CanaryAdmissionEvidence, error) {
	ordered, err := validateBenchmarkAdmissionRequest(request)
	if err != nil {
		return adaptive.CanaryAdmissionEvidence{}, err
	}
	if workflow == nil || workflow.evidence == nil {
		return adaptive.CanaryAdmissionEvidence{}, ErrAdaptiveBenchmarkUnavailable
	}
	fingerprint, err := AdaptiveBenchmarkAdmissionFingerprint(request)
	if err != nil {
		return adaptive.CanaryAdmissionEvidence{}, err
	}
	operation, ok := idempotency.OperationFromContext(ctx)
	if !ok || operation.Fingerprint != fingerprint {
		return adaptive.CanaryAdmissionEvidence{}, ErrAdaptiveBenchmarkAdmissionInput
	}
	evidence, err := workflow.evidence.SealCanaryAdmission(
		ctx,
		request.OwnerID,
		request.CohortID,
		adaptive.EvidenceSealer{
			ActorID:    request.OwnerID,
			Capability: "adaptive.evidence.seal",
		},
		request.ManifestSHA256,
		ordered,
	)
	if err != nil {
		return adaptive.CanaryAdmissionEvidence{}, fmt.Errorf(
			"seal adaptive benchmark admission: %w", err,
		)
	}
	return evidence, nil
}

// AdaptiveBenchmarkAdmissionFingerprint binds only the frozen admission intent.
func AdaptiveBenchmarkAdmissionFingerprint(
	request AdaptiveBenchmarkAdmissionRequest,
) ([32]byte, error) {
	ordered, err := validateBenchmarkAdmissionRequest(request)
	if err != nil {
		return [32]byte{}, err
	}
	return adaptive.AdmissionOperationFingerprint(
		request.ManifestSHA256,
		request.OwnerID,
		request.CohortID,
		ordered,
	)
}

func validateBenchmarkAdmissionRequest(
	request AdaptiveBenchmarkAdmissionRequest,
) (
	[]adaptive.EvidenceArtifactRegistration,
	error,
) {
	if request.Command != AdaptiveBenchmarkAdmissionCommand ||
		!request.Confirmed ||
		!validBenchmarkSHA256(request.ManifestSHA256) ||
		request.OwnerID == uuid.Nil ||
		request.CohortID == uuid.Nil ||
		len(request.Artifacts) != len(admissionArtifactOrder) {
		return nil, ErrAdaptiveBenchmarkAdmissionInput
	}
	ordered := make(
		[]adaptive.EvidenceArtifactRegistration,
		len(admissionArtifactOrder),
	)
	for index, registration := range request.Artifacts {
		ref := registration.Ref
		if ref.Kind != admissionArtifactOrder[index] ||
			adaptive.ValidateAdmissionArtifactRegistration(registration) != nil {
			return nil, fmt.Errorf(
				"%w: artifact %q",
				ErrAdaptiveBenchmarkAdmissionInput,
				ref.Kind,
			)
		}
		ordered[index] = registration
	}
	return ordered, nil
}

func validBenchmarkSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == hex.EncodeToString(decoded)
}
