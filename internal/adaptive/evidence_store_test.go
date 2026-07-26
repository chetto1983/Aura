package adaptive

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestEvidenceStoreSealCanaryOutcomeRequiresTrustedStore(t *testing.T) {
	t.Parallel()

	store := NewEvidenceStore(nil)
	_, err := store.SealCanaryOutcome(
		context.Background(),
		uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		1,
		EvidenceSealer{
			ActorID:    uuid.MustParse("11111111-1111-4111-8111-111111111111"),
			Capability: "adaptive.evidence.seal",
		},
	)
	if err == nil || !strings.Contains(err.Error(), "database pool") {
		t.Fatalf("SealCanaryOutcome error = %v, want missing trusted store", err)
	}
}

func TestEvidenceStoreSealCanaryAdmissionRequiresTrustedStore(t *testing.T) {
	t.Parallel()

	store := NewEvidenceStore(nil)
	_, err := store.SealCanaryAdmission(
		context.Background(),
		uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		EvidenceSealer{
			ActorID:    uuid.MustParse("11111111-1111-4111-8111-111111111111"),
			Capability: "adaptive.evidence.seal",
		},
	)
	if err == nil || !strings.Contains(err.Error(), "database pool") {
		t.Fatalf("SealCanaryAdmission error = %v, want missing trusted store", err)
	}
}

func TestEvidenceSealerRejectsCallerSelectedCapability(t *testing.T) {
	t.Parallel()

	sealer := EvidenceSealer{
		ActorID:    uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		Capability: "adaptive.manage",
	}
	if err := sealer.validate(
		uuid.MustParse("11111111-1111-4111-8111-111111111111"),
	); err == nil {
		t.Fatal("sealer accepted a transition capability for outcome sealing")
	}
}
