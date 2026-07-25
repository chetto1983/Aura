package adaptive

import (
	"bytes"
	"testing"
)

func TestRandomizationReceipt_CanonicalRoundTrip_When_FrozenMappingMatches(t *testing.T) {
	receipt := validRandomizationReceipt()
	canonical, err := receipt.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	decoded, err := DecodeRandomizationReceipt(canonical)
	if err != nil {
		t.Fatalf("DecodeRandomizationReceipt() error = %v", err)
	}
	roundTrip, err := decoded.CanonicalJSON()
	if err != nil {
		t.Fatalf("round-trip CanonicalJSON() error = %v", err)
	}
	if !bytes.Equal(roundTrip, canonical) {
		t.Fatalf("round trip changed bytes:\n%s\n%s", canonical, roundTrip)
	}
}

func TestRandomizationReceipt_RejectsTamperedDrawMapping(t *testing.T) {
	receipt := validRandomizationReceipt()
	receipt.DrawByte = "fe"
	if err := receipt.Validate(); err == nil {
		t.Fatal("Validate() accepted a draw/arm mismatch")
	}
}

func TestDecodeRandomizationReceipt_RejectsUnknownAndDuplicateFields(t *testing.T) {
	canonical, err := validRandomizationReceipt().CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	unknown := append(bytes.TrimSuffix(canonical, []byte("}")), []byte(`,"extra":1}`)...)
	if _, err := DecodeRandomizationReceipt(unknown); err == nil {
		t.Fatal("DecodeRandomizationReceipt() accepted an unknown field")
	}
	duplicate := append(
		[]byte(`{"schema_id":"aura.adaptive.randomization-receipt/v1",`),
		canonical[1:]...,
	)
	if _, err := DecodeRandomizationReceipt(duplicate); err == nil {
		t.Fatal("DecodeRandomizationReceipt() accepted a duplicate field")
	}
}

func TestExactRational_RejectsNonReducedAndDuplicateCanonicalJSON(t *testing.T) {
	var rational ExactRational
	if err := rational.UnmarshalJSON([]byte(`{"numerator":2,"denominator":4}`)); err == nil {
		t.Fatal("UnmarshalJSON() accepted a non-reduced rational")
	}
	if err := rational.UnmarshalJSON(
		[]byte(`{"numerator":1,"numerator":1,"denominator":2}`),
	); err == nil {
		t.Fatal("UnmarshalJSON() accepted a duplicate field")
	}
	if err := rational.UnmarshalJSON([]byte(`{"numerator":1,"denominator":2}`)); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if rational.Cmp(MustExactRational(1, 2)) != 0 {
		t.Fatalf("rational = %s", rational)
	}
}

func validRandomizationReceipt() RandomizationReceipt {
	const (
		ownerID      = "11111111-1111-4111-8111-111111111111"
		cohortID     = "22222222-2222-4222-8222-222222222222"
		requestID    = "33333333-3333-4333-8333-333333333333"
		claimID      = "44444444-4444-4444-8444-444444444444"
		assignmentID = "55555555-5555-4555-8555-555555555555"
		shaA         = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		shaB         = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		shaC         = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		shaD         = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	)
	arm := func(id, role, policy string) ReceiptArmPolicyRef {
		return ReceiptArmPolicyRef{
			SchemaID: "aura.adaptive.arm-policy-ref/v1", Revision: 1,
			ArmID: id, ArmRole: role, Weight: MustExactRational(1, 1),
			PolicyID: policy, PolicyRevision: 1, SnapshotID: cohortID,
			SnapshotSHA256: shaB, ActionSchemaSHA256: shaC, FeatureSchemaSHA256: shaD,
		}
	}
	return RandomizationReceipt{
		SchemaID: "aura.adaptive.randomization-receipt/v1", Revision: 1,
		AssignmentID: assignmentID, OwnerID: ownerID, CohortID: cohortID,
		RequestID: requestID, ClaimID: claimID, RandomizationPlanSHA256: shaA,
		MechanismID: randomizationMechanismID, MechanismRevision: 1,
		EntropySourceID: randomizationEntropyID, DrawByte: "ff", MappedBit: 1,
		SelectedArmID: "challenger", SelectedArmRole: "treatment",
		ArmProbability: MustExactRational(1, 2), AnalysisStratumID: shaB,
		AnalysisStratumSchemaSHA256: shaC,
		ArmPolicyRefs: []ReceiptArmPolicyRef{
			arm("baseline", "control", "aura/static-champion"),
			arm("challenger", "treatment", "aura/graph-knn-snapshot"),
		},
		ActionProbabilities: []ExactActionProbability{
			{ActionID: "static", Probability: MustExactRational(1, 2)},
			{ActionID: "summarize", Probability: MustExactRational(1, 2)},
		},
	}
}
