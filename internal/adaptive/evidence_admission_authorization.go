package adaptive

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

var (
	admissionApprovalIDNamespace = uuid.MustParse(
		"e09f3753-e0de-4d48-967e-8b9e7227f65c",
	)
	admissionRequestIDNamespace = uuid.MustParse(
		"a8319677-6496-4bad-a662-7b7e53a04eeb",
	)
)

type admissionOperationReceipt struct {
	IdentityID  string
	Scope       string
	Key         string
	Fingerprint [32]byte
	CreatedAt   time.Time
}

type admissionApprovalIntent struct {
	SchemaID                   string             `json:"schema_id"`
	Revision                   int                `json:"revision"`
	OperationIdentityID        string             `json:"operation_identity_id"`
	OperationScope             string             `json:"operation_scope"`
	OperationKey               string             `json:"operation_key"`
	OperationFingerprintSHA256 string             `json:"operation_fingerprint_sha256"`
	OwnerID                    string             `json:"owner_id"`
	CohortID                   string             `json:"cohort_id"`
	CohortSHA256               string             `json:"cohort_sha256"`
	PolicyEpoch                uint64             `json:"policy_epoch"`
	PolicyVersion              string             `json:"policy_version"`
	TargetState                string             `json:"target_state"`
	Artifacts                  []ChildArtifactRef `json:"artifacts"`
}

func validateAdmissionOperationReceipt(
	operation idempotency.Operation,
	row sqlc.LockOperationReceiptRow,
	semanticFingerprint [32]byte,
) (admissionOperationReceipt, error) {
	if err := operation.Validate(); err != nil ||
		operation.Key.IdentityID != identityctx.CLIServiceIdentity ||
		operation.Key.Scope != idempotency.ScopeCLICommand ||
		operation.ClaimToken <= 0 ||
		operation.Fingerprint != semanticFingerprint {
		return admissionOperationReceipt{}, errors.New(
			"adaptive admission operation is invalid",
		)
	}
	identityID, err := uuid.Parse(operation.Key.IdentityID)
	if err != nil ||
		!row.IdentityID.Valid ||
		uuid.UUID(row.IdentityID.Bytes) != identityID ||
		row.OperationScope != string(operation.Key.Scope) ||
		row.OperationKey != operation.Key.Key ||
		!bytes.Equal(row.PayloadHash, semanticFingerprint[:]) ||
		row.State != string(idempotency.StateInProgress) ||
		row.Version != int64(operation.ClaimToken) ||
		!row.CreatedAt.Valid {
		return admissionOperationReceipt{}, errors.New(
			"adaptive admission operation receipt is invalid",
		)
	}
	createdAt := row.CreatedAt.Time.UTC()
	if !canonicalEvidenceTime(createdAt) {
		return admissionOperationReceipt{}, errors.New(
			"adaptive admission operation receipt time is invalid",
		)
	}
	return admissionOperationReceipt{
		IdentityID:  operation.Key.IdentityID,
		Scope:       string(operation.Key.Scope),
		Key:         operation.Key.Key,
		Fingerprint: operation.Fingerprint,
		CreatedAt:   createdAt,
	}, nil
}

func buildOperatorApprovalRegistration(
	cohort *FocalCohort,
	sealer EvidenceSealer,
	registrations []EvidenceArtifactRegistration,
	receipt admissionOperationReceipt,
) (EvidenceArtifactRegistration, error) {
	if cohort == nil || !cohort.randomizationEligible() ||
		sealer.validate(cohort.Scope().OwnerID) != nil {
		return EvidenceArtifactRegistration{}, errors.New(
			"adaptive operator approval scope is invalid",
		)
	}
	refs := make([]ChildArtifactRef, len(registrations))
	for index, registration := range registrations {
		refs[index] = registration.Ref
	}
	expected := admissionCohortRefs(cohort)
	if len(refs) != len(expected) {
		return EvidenceArtifactRegistration{}, errors.New(
			"adaptive operator approval artifacts are invalid",
		)
	}
	for index := range expected {
		if refs[index] != expected[index] {
			return EvidenceArtifactRegistration{}, errors.New(
				"adaptive operator approval artifact binding is invalid",
			)
		}
	}
	scope := cohort.Scope()
	intent := admissionApprovalIntent{
		SchemaID:            "aura.adaptive.operator-approval-intent/v1",
		Revision:            1,
		OperationIdentityID: receipt.IdentityID,
		OperationScope:      receipt.Scope,
		OperationKey:        receipt.Key,
		OperationFingerprintSHA256: hex.EncodeToString(
			receipt.Fingerprint[:],
		),
		OwnerID:       scope.OwnerID.String(),
		CohortID:      cohort.ID().String(),
		CohortSHA256:  cohort.SHA256(),
		PolicyEpoch:   scope.PolicyEpoch,
		PolicyVersion: scope.PolicyVersion,
		TargetState:   string(PolicyCanary),
		Artifacts:     refs,
	}
	canonicalIntent, err := json.Marshal(intent)
	if err != nil {
		return EvidenceArtifactRegistration{}, err
	}
	approval := OperatorApproval{
		SchemaID: "aura.adaptive.operator-approval/v1", Revision: 1,
		ApprovalID: uuid.NewSHA1(
			admissionApprovalIDNamespace,
			canonicalIntent,
		).String(),
		ActorID: sealer.ActorID.String(), Capability: sealer.Capability,
		TargetState: string(PolicyCanary), OwnerID: scope.OwnerID.String(),
		CohortID: cohort.ID().String(), CohortSHA256: cohort.SHA256(),
		PolicyEpoch: scope.PolicyEpoch, PolicyVersion: scope.PolicyVersion,
		ApprovedAt: receipt.CreatedAt,
		SourceRequestID: uuid.NewSHA1(
			admissionRequestIDNamespace,
			canonicalIntent,
		).String(),
		ProvenanceSHA256: sha256Hex(canonicalIntent),
	}
	canonicalApproval, err := approval.CanonicalJSON()
	if err != nil {
		return EvidenceArtifactRegistration{}, err
	}
	return EvidenceArtifactRegistration{
		Ref: ChildArtifactRef{
			SchemaID: ChildArtifactRefSchemaID, Revision: 1,
			Kind:             "operator_approval",
			ArtifactSchemaID: "aura.adaptive.operator-approval/v1",
			ArtifactRevision: 1, ArtifactID: approval.ApprovalID,
			ArtifactSHA256: sha256Hex(canonicalApproval),
		},
		Canonical: canonicalApproval,
	}, nil
}

func admissionCohortRefs(cohort *FocalCohort) []ChildArtifactRef {
	document := cohort.v2.document
	return []ChildArtifactRef{
		document.InterferencePlanRef,
		document.LookPlanRef,
		document.QualityOutcomeModelRef,
		document.HarmOutcomeModelRef,
		document.OPEPlanRef,
	}
}

func validateOperatorApprovalRegistration(
	cohort *FocalCohort,
	sealer EvidenceSealer,
	registration EvidenceArtifactRegistration,
) (OperatorApproval, error) {
	approval, err := DecodeOperatorApproval(registration.Canonical)
	if err != nil ||
		registration.validate("operator_approval") != nil ||
		registration.Ref.Kind != "operator_approval" ||
		registration.Ref.ArtifactID != approval.ApprovalID ||
		registration.Ref.ArtifactSHA256 != sha256Hex(registration.Canonical) ||
		approval.ActorID != sealer.ActorID.String() ||
		approval.ActorID != cohort.Scope().OwnerID.String() ||
		approval.Capability != sealer.Capability ||
		approval.OwnerID != cohort.Scope().OwnerID.String() ||
		approval.CohortID != cohort.ID().String() ||
		approval.CohortSHA256 != cohort.SHA256() ||
		approval.PolicyEpoch != cohort.Scope().PolicyEpoch ||
		approval.PolicyVersion != cohort.Scope().PolicyVersion ||
		approval.TargetState != string(PolicyCanary) {
		return OperatorApproval{}, errors.New(
			"adaptive operator approval differs from trusted admission",
		)
	}
	return approval, nil
}
