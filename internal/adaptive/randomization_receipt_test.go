package adaptive

import (
	"bytes"
	"math/big"
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

func TestRandomizationReceipt_RejectsInvalidSnapshotAndNonArmMarginal(t *testing.T) {
	receipt := validRandomizationReceipt()
	receipt.ArmPolicyRefs[0].SnapshotID = ""
	if err := receipt.Validate(); err == nil {
		t.Fatal("Validate() accepted an empty snapshot id")
	}
	receipt = validRandomizationReceipt()
	receipt.ActionProbabilities = []ExactActionProbability{
		{ActionID: "static", Probability: MustExactRational(1, 4)},
		{ActionID: "summarize", Probability: MustExactRational(3, 4)},
	}
	if err := receipt.Validate(); err == nil {
		t.Fatal("Validate() accepted a non-arm marginal distribution")
	}
}

func TestRandomizationReceipt_ValidatesAssignmentBinding(t *testing.T) {
	receipt := validRandomizationReceipt()
	binding := RandomizationAssignmentBinding{
		AssignmentID: receipt.AssignmentID, OwnerID: receipt.OwnerID,
		CohortID: receipt.CohortID, RequestID: receipt.RequestID,
		SelectedArmID: "challenger", SelectedArmRole: "treatment",
		ArmProbability: MustExactRational(1, 2), IntendedActionID: "summarize",
		ActionProbabilities: append([]ExactActionProbability(nil), receipt.ActionProbabilities...),
	}
	if err := receipt.ValidateAssignment(binding); err != nil {
		t.Fatalf("ValidateAssignment() error = %v", err)
	}
	binding.IntendedActionID = "static"
	if err := receipt.ValidateAssignment(binding); err == nil {
		t.Fatal("ValidateAssignment() accepted a challenger/static mismatch")
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

func TestRandomizationReceipt_RejectsMalformedBindings(t *testing.T) {
	base := validRandomizationReceipt()
	tests := []struct {
		name   string
		mutate func(*RandomizationReceipt)
	}{
		{name: "schema", mutate: func(receipt *RandomizationReceipt) { receipt.Revision = 2 }},
		{name: "uuid", mutate: func(receipt *RandomizationReceipt) { receipt.ClaimID = "" }},
		{name: "hash", mutate: func(receipt *RandomizationReceipt) {
			receipt.RandomizationPlanSHA256 = ""
		}},
		{name: "mechanism", mutate: func(receipt *RandomizationReceipt) { receipt.MechanismRevision = 2 }},
		{name: "draw", mutate: func(receipt *RandomizationReceipt) { receipt.DrawByte = "GG" }},
		{name: "role", mutate: func(receipt *RandomizationReceipt) { receipt.SelectedArmRole = "control" }},
		{name: "arm refs", mutate: func(receipt *RandomizationReceipt) { receipt.ArmPolicyRefs = nil }},
		{name: "snapshot mismatch", mutate: func(receipt *RandomizationReceipt) {
			receipt.ArmPolicyRefs[1].SnapshotSHA256 = repeatedSHA("f")
		}},
		{name: "action order", mutate: func(receipt *RandomizationReceipt) {
			receipt.ActionProbabilities[1].ActionID = "aaa"
		}},
		{name: "action range", mutate: func(receipt *RandomizationReceipt) {
			receipt.ActionProbabilities[0].Probability = MustExactRational(-1, 1)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			receipt := base
			receipt.ArmPolicyRefs = append([]ReceiptArmPolicyRef(nil), base.ArmPolicyRefs...)
			receipt.ActionProbabilities = append(
				[]ExactActionProbability(nil), base.ActionProbabilities...,
			)
			test.mutate(&receipt)
			if err := receipt.Validate(); err == nil {
				t.Fatal("Validate() accepted malformed receipt")
			}
		})
	}
	binding := RandomizationAssignmentBinding{}
	if err := base.ValidateAssignment(binding); err == nil {
		t.Fatal("ValidateAssignment() accepted empty assignment")
	}
	canonical, err := base.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRandomizationReceipt(append(canonical, []byte("{}")...)); err == nil {
		t.Fatal("DecodeRandomizationReceipt() accepted trailing JSON")
	}
}

func TestExactRational_ArithmeticAndMalformedInputs(t *testing.T) {
	if _, err := NewExactRational(nil, nil); err == nil {
		t.Fatal("NewExactRational() accepted nil inputs")
	}
	if _, err := NewExactRational(big.NewInt(1), big.NewInt(0)); err == nil {
		t.Fatal("NewExactRational() accepted zero denominator")
	}
	zero := ExactRational{}
	if zero.String() != "0/1" {
		t.Fatalf("zero String() = %s", zero.String())
	}
	if MustExactRational(3, 4).Sub(MustExactRational(1, 4)).Cmp(MustExactRational(1, 2)) != 0 {
		t.Fatal("Sub() returned wrong result")
	}
	if _, err := MustExactRational(1, 2).Quo(zero); err == nil {
		t.Fatal("Quo() accepted zero divisor")
	}
	if got := FormatUpperEightDecimals(MustExactRational(1, 3)); got != "0.33333334" {
		t.Fatalf("upper = %s", got)
	}
	if got := FormatLowerEightDecimals(MustExactRational(123, 1)); got != "123.00000000" {
		t.Fatalf("whole = %s", got)
	}
	var nilReceiver *ExactRational
	if err := nilReceiver.UnmarshalJSON([]byte(`{"numerator":1,"denominator":2}`)); err == nil {
		t.Fatal("nil UnmarshalJSON receiver accepted input")
	}
	for _, raw := range []string{
		`{`,
		`{"numerator":1}`,
		`{"numerator":"x","denominator":2}`,
		`{"numerator":1,"denominator":"x"}`,
		`{"numerator":1,"denominator":0}`,
	} {
		var rational ExactRational
		if err := rational.UnmarshalJSON([]byte(raw)); err == nil {
			t.Fatalf("UnmarshalJSON() accepted %s", raw)
		}
	}
}

func TestRandomizationReceipt_RequiresClaimAndAssignmentTuple(t *testing.T) {
	receipt := validRandomizationReceipt()
	canonical, digest, err := canonicalRandomizationReceipt(receipt)
	if err != nil {
		t.Fatalf("canonicalRandomizationReceipt() error = %v", err)
	}
	if len(canonical) == 0 || len(digest) != 32 {
		t.Fatalf("canonical bytes/digest = %d/%d", len(canonical), len(digest))
	}
	decoded, err := DecodeRandomizationReceipt(canonical)
	if err != nil {
		t.Fatalf("DecodeRandomizationReceipt() error = %v", err)
	}
	if decoded.ClaimID != receipt.ClaimID || decoded.AssignmentID != receipt.AssignmentID {
		t.Fatalf("decoded receipt tuple = %s/%s", decoded.ClaimID, decoded.AssignmentID)
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
