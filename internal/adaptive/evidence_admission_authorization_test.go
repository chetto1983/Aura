package adaptive

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestValidateAdmissionOperationReceiptRequiresExactInProgressRow(
	t *testing.T,
) {
	t.Parallel()
	operation := admissionTestOperation()
	row := admissionTestOperationRow(operation)

	receipt, err := validateAdmissionOperationReceipt(
		operation,
		row,
		operation.Fingerprint,
	)
	if err != nil {
		t.Fatalf("validateAdmissionOperationReceipt: %v", err)
	}
	if receipt.IdentityID != operation.Key.IdentityID ||
		receipt.Scope != string(operation.Key.Scope) ||
		receipt.Key != operation.Key.Key ||
		receipt.Fingerprint != operation.Fingerprint ||
		!receipt.CreatedAt.Equal(row.CreatedAt.Time) {
		t.Fatalf("validated receipt = %#v", receipt)
	}

	tests := []struct {
		name   string
		mutate func(*sqlc.LockOperationReceiptRow)
	}{
		{name: "identity", mutate: func(row *sqlc.LockOperationReceiptRow) {
			row.IdentityID = dbUUID(uuid.Must(uuid.NewV7()))
		}},
		{name: "scope", mutate: func(row *sqlc.LockOperationReceiptRow) {
			row.OperationScope = string(idempotency.ScopeHTTPMutation)
		}},
		{name: "key", mutate: func(row *sqlc.LockOperationReceiptRow) {
			row.OperationKey = "other-key"
		}},
		{name: "fingerprint", mutate: func(row *sqlc.LockOperationReceiptRow) {
			row.PayloadHash = bytes.Repeat([]byte{0xbb}, 32)
		}},
		{name: "state", mutate: func(row *sqlc.LockOperationReceiptRow) {
			row.State = string(idempotency.StateCompleted)
		}},
		{name: "claim token", mutate: func(row *sqlc.LockOperationReceiptRow) {
			row.Version++
		}},
		{name: "created at", mutate: func(row *sqlc.LockOperationReceiptRow) {
			row.CreatedAt = pgtype.Timestamptz{}
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			changed := row
			testCase.mutate(&changed)
			if _, err := validateAdmissionOperationReceipt(
				operation,
				changed,
				operation.Fingerprint,
			); err == nil {
				t.Fatalf(
					"validateAdmissionOperationReceipt accepted changed %s",
					testCase.name,
				)
			}
		})
	}
}

func TestValidateAdmissionOperationReceiptRejectsNonCLIContext(t *testing.T) {
	t.Parallel()
	base := admissionTestOperation()
	tests := []struct {
		name   string
		mutate func(*idempotency.Operation)
	}{
		{name: "identity", mutate: func(operation *idempotency.Operation) {
			operation.Key.IdentityID = uuid.Must(uuid.NewV7()).String()
		}},
		{name: "scope", mutate: func(operation *idempotency.Operation) {
			operation.Key.Scope = idempotency.ScopeHTTPMutation
		}},
		{name: "empty fingerprint", mutate: func(operation *idempotency.Operation) {
			operation.Fingerprint = [32]byte{}
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			operation := base
			testCase.mutate(&operation)
			if _, err := validateAdmissionOperationReceipt(
				operation,
				admissionTestOperationRow(operation),
				operation.Fingerprint,
			); err == nil {
				t.Fatalf(
					"validateAdmissionOperationReceipt accepted %s context",
					testCase.name,
				)
			}
		})
	}
}

func TestValidateAdmissionOperationReceiptRejectsUnrelatedSemanticFingerprint(
	t *testing.T,
) {
	t.Parallel()
	operation := admissionTestOperation()
	row := admissionTestOperationRow(operation)
	recomputed := operation.Fingerprint
	recomputed[0] ^= 0xff
	if _, err := validateAdmissionOperationReceipt(
		operation,
		row,
		recomputed,
	); err == nil {
		t.Fatal("valid unrelated receipt authorized adaptive admission")
	}
}

func TestBuildOperatorApprovalRegistrationIsDeterministicAndServerBound(
	t *testing.T,
) {
	t.Parallel()
	cohort, _ := randomizedFocalCohortUnitFixture(t)
	operation := admissionTestOperation()
	receipt, err := validateAdmissionOperationReceipt(
		operation,
		admissionTestOperationRow(operation),
		operation.Fingerprint,
	)
	if err != nil {
		t.Fatal(err)
	}
	sealer := EvidenceSealer{
		ActorID: cohort.Scope().OwnerID, Capability: "adaptive.evidence.seal",
	}
	registrations := admissionTestRegistrations(cohort)

	first, err := buildOperatorApprovalRegistration(
		cohort,
		sealer,
		registrations,
		receipt,
	)
	if err != nil {
		t.Fatalf("buildOperatorApprovalRegistration: %v", err)
	}
	second, err := buildOperatorApprovalRegistration(
		cohort,
		sealer,
		registrations,
		receipt,
	)
	if err != nil {
		t.Fatalf("retry buildOperatorApprovalRegistration: %v", err)
	}
	if first.Ref != second.Ref || !bytes.Equal(first.Canonical, second.Canonical) {
		t.Fatal("operator approval changed across exact retry")
	}
	approval, err := DecodeOperatorApproval(first.Canonical)
	if err != nil {
		t.Fatalf("DecodeOperatorApproval: %v", err)
	}
	if first.Ref.ArtifactID != approval.ApprovalID ||
		approval.ActorID != sealer.ActorID.String() ||
		approval.OwnerID != cohort.Scope().OwnerID.String() ||
		approval.CohortID != cohort.ID().String() ||
		approval.ApprovedAt != receipt.CreatedAt ||
		!strings.EqualFold(first.Ref.ArtifactSHA256, sha256Hex(first.Canonical)) {
		t.Fatalf("server-built approval binding = %#v / %#v", first.Ref, approval)
	}

	changed := receipt
	changed.Fingerprint[0] ^= 0xff
	changedRegistration, err := buildOperatorApprovalRegistration(
		cohort,
		sealer,
		registrations,
		changed,
	)
	if err != nil {
		t.Fatal(err)
	}
	if changedRegistration.Ref.ArtifactID == first.Ref.ArtifactID ||
		changedRegistration.Ref.ArtifactSHA256 == first.Ref.ArtifactSHA256 {
		t.Fatal("operation fingerprint did not bind the server-built approval")
	}
}

func TestValidateRecoveredCanaryAdmissionRequiresPreparedCanonicalEvidence(
	t *testing.T,
) {
	t.Parallel()
	prepared := validCanaryAdmissionEvidence()
	if err := validateRecoveredCanaryAdmission(prepared, prepared); err != nil {
		t.Fatalf("validate exact recovered admission: %v", err)
	}
	changed := prepared
	changed.CreatedAt = changed.CreatedAt.Add(time.Microsecond)
	if err := validateRecoveredCanaryAdmission(
		prepared,
		changed,
	); !errors.Is(err, ErrCanaryAdmissionConflict) {
		t.Fatalf("changed recovered admission error = %v, want conflict", err)
	}
}

func admissionTestOperation() idempotency.Operation {
	var fingerprint [32]byte
	copy(fingerprint[:], bytes.Repeat([]byte{0xaa}, len(fingerprint)))
	return idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: identityctx.CLIServiceIdentity,
			Scope:      idempotency.ScopeCLICommand,
			Key:        "adaptive-seal-operation",
		},
		Fingerprint: fingerprint,
		ClaimToken:  idempotency.ClaimToken(3),
	}
}

func admissionTestOperationRow(
	operation idempotency.Operation,
) sqlc.LockOperationReceiptRow {
	identityID := uuid.MustParse(operation.Key.IdentityID)
	return sqlc.LockOperationReceiptRow{
		IdentityID: dbUUID(identityID), OperationScope: string(operation.Key.Scope),
		OperationKey: operation.Key.Key,
		PayloadHash:  bytes.Clone(operation.Fingerprint[:]),
		State:        string(idempotency.StateInProgress),
		Version:      int64(operation.ClaimToken),
		CreatedAt: pgtype.Timestamptz{
			Time:  time.Date(2026, time.July, 26, 10, 11, 12, 123_456_000, time.UTC),
			Valid: true,
		},
	}
}

func admissionTestRegistrations(
	cohort *FocalCohort,
) []EvidenceArtifactRegistration {
	document := cohort.v2.document
	refs := []ChildArtifactRef{
		document.InterferencePlanRef,
		document.LookPlanRef,
		document.QualityOutcomeModelRef,
		document.HarmOutcomeModelRef,
		document.OPEPlanRef,
	}
	registrations := make([]EvidenceArtifactRegistration, len(refs))
	for index, ref := range refs {
		registrations[index] = EvidenceArtifactRegistration{Ref: ref}
	}
	return registrations
}
