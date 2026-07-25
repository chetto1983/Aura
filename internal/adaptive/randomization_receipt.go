package adaptive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// RandomizationReceipt is the immutable replay proof for one assignment draw.
type RandomizationReceipt struct {
	SchemaID                    string                   `json:"schema_id"`
	Revision                    int                      `json:"revision"`
	AssignmentID                string                   `json:"assignment_id"`
	OwnerID                     string                   `json:"owner_id"`
	CohortID                    string                   `json:"cohort_id"`
	RequestID                   string                   `json:"request_id"`
	ClaimID                     string                   `json:"claim_id"`
	RandomizationPlanSHA256     string                   `json:"randomization_plan_sha256"`
	MechanismID                 string                   `json:"mechanism_id"`
	MechanismRevision           int                      `json:"mechanism_revision"`
	EntropySourceID             string                   `json:"entropy_source_id"`
	DrawByte                    string                   `json:"draw_byte"`
	MappedBit                   uint8                    `json:"mapped_bit"`
	SelectedArmID               string                   `json:"selected_arm_id"`
	SelectedArmRole             string                   `json:"selected_arm_role"`
	ArmProbability              ExactRational            `json:"arm_probability"`
	AnalysisStratumID           string                   `json:"analysis_stratum_id"`
	AnalysisStratumSchemaSHA256 string                   `json:"analysis_stratum_schema_sha256"`
	ArmPolicyRefs               []ReceiptArmPolicyRef    `json:"arm_policy_refs"`
	ActionProbabilities         []ExactActionProbability `json:"action_probabilities"`
}

// ReceiptArmPolicyRef binds one prevalidated deterministic arm alternative.
type ReceiptArmPolicyRef struct {
	SchemaID            string        `json:"schema_id"`
	Revision            int           `json:"revision"`
	ArmID               string        `json:"arm_id"`
	ArmRole             string        `json:"arm_role"`
	Weight              ExactRational `json:"weight"`
	PolicyID            string        `json:"policy_id"`
	PolicyRevision      int           `json:"policy_revision"`
	SnapshotID          string        `json:"snapshot_id"`
	SnapshotSHA256      string        `json:"snapshot_sha256"`
	ActionSchemaSHA256  string        `json:"action_schema_sha256"`
	FeatureSchemaSHA256 string        `json:"feature_schema_sha256"`
}

// Validate enforces replay mapping and hash-critical ordering.
func (receipt RandomizationReceipt) Validate() error {
	if receipt.SchemaID != "aura.adaptive.randomization-receipt/v1" || receipt.Revision != 1 {
		return errors.New("randomization receipt schema is invalid")
	}
	for name, value := range map[string]string{
		"assignment_id": receipt.AssignmentID,
		"owner_id":      receipt.OwnerID,
		"cohort_id":     receipt.CohortID,
		"request_id":    receipt.RequestID,
		"claim_id":      receipt.ClaimID,
	} {
		if err := validateCanonicalUUID(value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if !validSHA256(receipt.RandomizationPlanSHA256) ||
		!validSHA256(receipt.AnalysisStratumID) ||
		!validSHA256(receipt.AnalysisStratumSchemaSHA256) {
		return errors.New("randomization receipt hash is invalid")
	}
	if receipt.MechanismID != randomizationMechanismID ||
		receipt.MechanismRevision != 1 ||
		receipt.EntropySourceID != randomizationEntropyID {
		return errors.New("randomization receipt mechanism is invalid")
	}
	draw, err := hex.DecodeString(receipt.DrawByte)
	if err != nil || len(draw) != 1 || hex.EncodeToString(draw) != receipt.DrawByte {
		return errors.New("randomization receipt draw byte is invalid")
	}
	expectedArm := MapRandomizationByte(draw[0])
	if receipt.MappedBit != draw[0]&1 || receipt.SelectedArmID != string(expectedArm) {
		return errors.New("randomization receipt draw mapping does not match selected arm")
	}
	expectedRole := "control"
	if expectedArm == RandomizedArmChallenger {
		expectedRole = "treatment"
	}
	if receipt.SelectedArmRole != expectedRole ||
		receipt.ArmProbability.Cmp(MustExactRational(1, 2)) != 0 {
		return errors.New("randomization receipt arm contract is invalid")
	}
	if len(receipt.ArmPolicyRefs) != 2 ||
		!validReceiptArmPolicyRef(receipt.ArmPolicyRefs[0], RandomizedArmBaseline) ||
		!validReceiptArmPolicyRef(receipt.ArmPolicyRefs[1], RandomizedArmChallenger) {
		return errors.New("randomization receipt arm references are not frozen baseline/challenger order")
	}
	if receipt.ArmPolicyRefs[0].SnapshotID != receipt.ArmPolicyRefs[1].SnapshotID ||
		receipt.ArmPolicyRefs[0].SnapshotSHA256 != receipt.ArmPolicyRefs[1].SnapshotSHA256 ||
		receipt.ArmPolicyRefs[0].ActionSchemaSHA256 != receipt.ArmPolicyRefs[1].ActionSchemaSHA256 ||
		receipt.ArmPolicyRefs[0].FeatureSchemaSHA256 != receipt.ArmPolicyRefs[1].FeatureSchemaSHA256 {
		return errors.New("randomization receipt arm references do not share frozen schemas and snapshot")
	}
	seen := make(map[string]struct{}, len(receipt.ActionProbabilities))
	totalProbability := MustExactRational(0, 1)
	previousActionID := ""
	for _, action := range receipt.ActionProbabilities {
		if action.ActionID == "" || action.ActionID <= previousActionID {
			return errors.New("randomization receipt action id is empty")
		}
		previousActionID = action.ActionID
		if _, duplicate := seen[action.ActionID]; duplicate {
			return errors.New("randomization receipt action probabilities contain a duplicate")
		}
		seen[action.ActionID] = struct{}{}
		if action.Probability.Cmp(MustExactRational(0, 1)) < 0 ||
			action.Probability.Cmp(MustExactRational(1, 1)) > 0 {
			return errors.New("randomization receipt action probability is outside [0,1]")
		}
		totalProbability = totalProbability.Add(action.Probability)
	}
	if totalProbability.Cmp(MustExactRational(1, 1)) != 0 {
		return errors.New("randomization receipt action probabilities do not sum to one")
	}
	if err := validateTwoArmMarginal(receipt.ActionProbabilities); err != nil {
		return err
	}
	selectedRef := receipt.ArmPolicyRefs[receipt.MappedBit]
	if selectedRef.ArmID != receipt.SelectedArmID {
		return errors.New("randomization receipt selected arm reference does not match")
	}
	return nil
}

func validReceiptArmPolicyRef(reference ReceiptArmPolicyRef, arm RandomizedArm) bool {
	role := "control"
	policyID := "aura/static-champion"
	if arm == RandomizedArmChallenger {
		role = "treatment"
		policyID = "aura/graph-knn-snapshot"
	}
	return reference.SchemaID == "aura.adaptive.arm-policy-ref/v1" &&
		reference.Revision == 1 &&
		reference.ArmID == string(arm) &&
		reference.ArmRole == role &&
		reference.Weight.Cmp(MustExactRational(1, 1)) == 0 &&
		reference.PolicyID == policyID &&
		reference.PolicyRevision == 1 &&
		validateCanonicalUUID(reference.SnapshotID) == nil &&
		validSHA256(reference.SnapshotSHA256) &&
		validSHA256(reference.ActionSchemaSHA256) &&
		validSHA256(reference.FeatureSchemaSHA256) &&
		reference.FeatureSchemaSHA256 != "9d3da8a268742cc8871fa33993732e4cf6515b2acb6d52070b292fda3f2620c0"
}

func validateTwoArmMarginal(probabilities []ExactActionProbability) error {
	nonzero := make([]ExactActionProbability, 0, 2)
	for _, probability := range probabilities {
		if probability.Probability.Cmp(MustExactRational(0, 1)) > 0 {
			nonzero = append(nonzero, probability)
		}
	}
	staticProbability := MustExactRational(0, 1)
	challengerCount := 0
	for _, probability := range nonzero {
		if probability.ActionID == "static" {
			staticProbability = probability.Probability
			continue
		}
		if probability.Probability.Cmp(MustExactRational(1, 2)) == 0 {
			challengerCount++
		}
	}
	if len(nonzero) == 1 && staticProbability.Cmp(MustExactRational(1, 1)) == 0 {
		return nil
	}
	if len(nonzero) == 2 &&
		staticProbability.Cmp(MustExactRational(1, 2)) == 0 &&
		challengerCount == 1 {
		return nil
	}
	return errors.New("randomization receipt action probabilities are not the frozen two-arm marginal")
}

func canonicalRandomizationReceipt(
	receipt RandomizationReceipt,
) ([]byte, []byte, error) {
	canonical, err := receipt.CanonicalJSON()
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(canonical)
	return canonical, sum[:], nil
}

// RandomizationAssignmentBinding is the assignment subset proven by a receipt.
type RandomizationAssignmentBinding struct {
	AssignmentID        string
	OwnerID             string
	CohortID            string
	RequestID           string
	SelectedArmID       string
	SelectedArmRole     string
	ArmProbability      ExactRational
	IntendedActionID    string
	ActionProbabilities []ExactActionProbability
}

// ValidateAssignment rejects a receipt that does not prove the persisted assignment.
func (receipt RandomizationReceipt) ValidateAssignment(binding RandomizationAssignmentBinding) error {
	if err := receipt.Validate(); err != nil {
		return err
	}
	if binding.AssignmentID != receipt.AssignmentID ||
		binding.OwnerID != receipt.OwnerID ||
		binding.CohortID != receipt.CohortID ||
		binding.RequestID != receipt.RequestID ||
		binding.SelectedArmID != receipt.SelectedArmID ||
		binding.SelectedArmRole != receipt.SelectedArmRole ||
		binding.ArmProbability.Cmp(receipt.ArmProbability) != 0 ||
		len(binding.ActionProbabilities) != len(receipt.ActionProbabilities) {
		return errors.New("randomization receipt does not match assignment binding")
	}
	for index := range binding.ActionProbabilities {
		if binding.ActionProbabilities[index].ActionID != receipt.ActionProbabilities[index].ActionID ||
			binding.ActionProbabilities[index].Probability.Cmp(
				receipt.ActionProbabilities[index].Probability,
			) != 0 {
			return errors.New("randomization receipt action probabilities do not match assignment")
		}
	}
	expectedAction := "static"
	if receipt.SelectedArmID == string(RandomizedArmChallenger) {
		for _, probability := range receipt.ActionProbabilities {
			if probability.ActionID != "static" &&
				probability.Probability.Cmp(MustExactRational(1, 2)) == 0 {
				expectedAction = probability.ActionID
				break
			}
		}
	}
	if binding.IntendedActionID != expectedAction {
		return errors.New("randomization receipt selected arm does not match intended action")
	}
	return nil
}

// CanonicalJSON validates then emits the exact field-ordered JSON bytes.
func (receipt RandomizationReceipt) CanonicalJSON() ([]byte, error) {
	if err := receipt.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(receipt)
}

// DecodeRandomizationReceipt accepts only canonical bytes.
func DecodeRandomizationReceipt(data []byte) (RandomizationReceipt, error) {
	var receipt RandomizationReceipt
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return RandomizationReceipt{}, fmt.Errorf("decode randomization receipt: %w", err)
	}
	if err := receipt.Validate(); err != nil {
		return RandomizationReceipt{}, err
	}
	canonical, err := receipt.CanonicalJSON()
	if err != nil {
		return RandomizationReceipt{}, err
	}
	if !bytes.Equal(data, canonical) {
		return RandomizationReceipt{}, errors.New("randomization receipt encoding is noncanonical")
	}
	return receipt, nil
}
