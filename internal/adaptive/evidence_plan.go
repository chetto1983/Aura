package adaptive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

const (
	evidenceLookPlanSchemaID = "aura.adaptive.look-plan/utc-cutoff/v1"
	evidenceLookSchemaID     = "aura.adaptive.look/utc-cutoff/v1"
)

// EvidenceLookArtifact is one canonical planned analysis closure.
type EvidenceLookArtifact struct {
	SchemaID                   string        `json:"schema_id"`
	Revision                   int           `json:"revision"`
	LookNumber                 int           `json:"look_number"`
	CutoffAt                   time.Time     `json:"cutoff_at"`
	Alpha                      ExactRational `json:"alpha"`
	CumulativeAlpha            ExactRational `json:"cumulative_alpha"`
	InferenceAlgorithmID       string        `json:"inference_algorithm_id"`
	InferenceAlgorithmRevision int           `json:"inference_algorithm_revision"`
	ArithmeticRevision         string        `json:"arithmetic_revision"`
	PredecessorRule            string        `json:"predecessor_rule"`
}

// EvidenceLookPlanArtifact is the only v2 five-look schedule.
type EvidenceLookPlanArtifact struct {
	SchemaID       string                 `json:"schema_id"`
	Revision       int                    `json:"revision"`
	Mode           string                 `json:"mode"`
	LookCount      int                    `json:"look_count"`
	Looks          []EvidenceLookArtifact `json:"looks"`
	MaxMembers     int                    `json:"max_members"`
	MaxStrata      int                    `json:"max_strata"`
	MaxTailTerms   int                    `json:"max_tail_terms"`
	MaxBigIntBits  int                    `json:"max_bigint_bits"`
	MaxMemoryBytes int64                  `json:"max_memory_bytes"`
	MaxWallTimeMS  int64                  `json:"max_wall_time_ms"`
}

// NewEvidenceLookPlanArtifact freezes the registered cutoffs and resource caps.
func NewEvidenceLookPlanArtifact(cutoffs []time.Time) (EvidenceLookPlanArtifact, error) {
	looks, err := NewFiveLookPlan(cutoffs)
	if err != nil {
		return EvidenceLookPlanArtifact{}, err
	}
	artifact := EvidenceLookPlanArtifact{
		SchemaID: evidenceLookPlanSchemaID, Revision: 1, Mode: "utc_cutoff",
		LookCount: 5, Looks: make([]EvidenceLookArtifact, len(looks)),
		MaxMembers: 5_000, MaxStrata: 5_000, MaxTailTerms: 5_000_000,
		MaxBigIntBits: 16_384, MaxMemoryBytes: 64 << 20, MaxWallTimeMS: 10_000,
	}
	for index, look := range looks {
		artifact.Looks[index] = EvidenceLookArtifact{
			SchemaID: evidenceLookSchemaID, Revision: 1,
			LookNumber: look.Number, CutoffAt: look.Cutoff,
			Alpha: look.Alpha, CumulativeAlpha: look.CumulativeAlpha,
			InferenceAlgorithmID:       "stratified-bonferroni-hypergeom-ate-v1",
			InferenceAlgorithmRevision: 1,
			ArithmeticRevision:         "exact-bigint-rational-v1",
			PredecessorRule:            look.PredecessorRule,
		}
	}
	return artifact, nil
}

// CanonicalJSON validates and emits the registered look-plan field order.
func (artifact EvidenceLookPlanArtifact) CanonicalJSON() ([]byte, error) {
	if err := artifact.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(artifact)
}

// SHA256 returns the look-plan content address.
func (artifact EvidenceLookPlanArtifact) SHA256() (string, error) {
	canonical, err := artifact.CanonicalJSON()
	if err != nil {
		return "", err
	}
	return sha256Hex(canonical), nil
}

func (artifact EvidenceLookPlanArtifact) validate() error {
	if artifact.SchemaID != evidenceLookPlanSchemaID || artifact.Revision != 1 ||
		artifact.Mode != "utc_cutoff" || artifact.LookCount != 5 ||
		len(artifact.Looks) != 5 || artifact.MaxMembers != 5_000 ||
		artifact.MaxStrata != 5_000 || artifact.MaxTailTerms != 5_000_000 ||
		artifact.MaxBigIntBits != 16_384 || artifact.MaxMemoryBytes != 64<<20 ||
		artifact.MaxWallTimeMS != 10_000 {
		return errors.New("adaptive evidence look plan is invalid")
	}
	cutoffs := make([]time.Time, len(artifact.Looks))
	for index, look := range artifact.Looks {
		expectedRule := "immediate_continue"
		if index == 0 {
			expectedRule = "none"
		}
		if look.SchemaID != evidenceLookSchemaID || look.Revision != 1 ||
			look.LookNumber != index+1 || !canonicalEvidenceTime(look.CutoffAt) ||
			look.InferenceAlgorithmID != "stratified-bonferroni-hypergeom-ate-v1" ||
			look.InferenceAlgorithmRevision != 1 ||
			look.ArithmeticRevision != "exact-bigint-rational-v1" ||
			look.PredecessorRule != expectedRule {
			return errors.New("adaptive evidence look is invalid")
		}
		cutoffs[index] = look.CutoffAt
	}
	expected, err := NewFiveLookPlan(cutoffs)
	if err != nil {
		return err
	}
	for index, look := range artifact.Looks {
		if look.Alpha.Cmp(expected[index].Alpha) != 0 ||
			look.CumulativeAlpha.Cmp(expected[index].CumulativeAlpha) != 0 {
			return errors.New("adaptive evidence alpha schedule is invalid")
		}
	}
	return nil
}

// DecodeEvidenceLookPlanArtifact rejects malformed or noncanonical bytes.
func DecodeEvidenceLookPlanArtifact(payload []byte) (EvidenceLookPlanArtifact, error) {
	var artifact EvidenceLookPlanArtifact
	if err := decodeStrictEvidence(payload, &artifact); err != nil {
		return EvidenceLookPlanArtifact{}, err
	}
	canonical, err := artifact.CanonicalJSON()
	if err != nil || !bytes.Equal(canonical, payload) {
		return EvidenceLookPlanArtifact{}, errors.New("adaptive evidence look plan is noncanonical")
	}
	return artifact, nil
}

// OperatorApproval is the immutable human authorization bound into admission.
type OperatorApproval struct {
	SchemaID         string    `json:"schema_id"`
	Revision         int       `json:"revision"`
	ApprovalID       string    `json:"approval_id"`
	ActorID          string    `json:"actor_id"`
	Capability       string    `json:"capability"`
	TargetState      string    `json:"target_state"`
	OwnerID          string    `json:"owner_id"`
	CohortID         string    `json:"cohort_id"`
	CohortSHA256     string    `json:"cohort_sha256"`
	PolicyEpoch      uint64    `json:"policy_epoch"`
	PolicyVersion    string    `json:"policy_version"`
	ApprovedAt       time.Time `json:"approved_at"`
	SourceRequestID  string    `json:"source_request_id"`
	ProvenanceSHA256 string    `json:"provenance_sha256"`
}

// CanonicalJSON validates and emits the approval field order.
func (approval OperatorApproval) CanonicalJSON() ([]byte, error) {
	if approval.SchemaID != "aura.adaptive.operator-approval/v1" ||
		approval.Revision != 1 || !canonicalUUID(approval.ApprovalID) ||
		!canonicalUUID(approval.ActorID) || !canonicalUUID(approval.OwnerID) ||
		!canonicalUUID(approval.CohortID) || !canonicalUUID(approval.SourceRequestID) ||
		approval.Capability != "adaptive.evidence.seal" ||
		approval.TargetState != string(PolicyCanary) ||
		!validSHA256(approval.CohortSHA256) || approval.PolicyEpoch == 0 ||
		approval.PolicyVersion == "" || !canonicalEvidenceTime(approval.ApprovedAt) ||
		!validSHA256(approval.ProvenanceSHA256) {
		return nil, errors.New("adaptive operator approval is invalid")
	}
	return json.Marshal(approval)
}

// SHA256 returns the approval content address.
func (approval OperatorApproval) SHA256() (string, error) {
	canonical, err := approval.CanonicalJSON()
	if err != nil {
		return "", err
	}
	return sha256Hex(canonical), nil
}

// DecodeOperatorApproval rejects malformed or noncanonical approval bytes.
func DecodeOperatorApproval(payload []byte) (OperatorApproval, error) {
	var approval OperatorApproval
	if err := decodeStrictEvidence(payload, &approval); err != nil {
		return OperatorApproval{}, err
	}
	canonical, err := approval.CanonicalJSON()
	if err != nil || !bytes.Equal(canonical, payload) {
		return OperatorApproval{}, errors.New(
			"adaptive operator approval is noncanonical",
		)
	}
	return approval, nil
}

// EvidenceArtifactRegistration is a typed reference plus its exact canonical bytes.
type EvidenceArtifactRegistration struct {
	Ref       ChildArtifactRef
	Canonical []byte
}

func (registration EvidenceArtifactRegistration) validate(allowedKinds ...string) error {
	if !slices.Contains(allowedKinds, registration.Ref.Kind) ||
		validateGenericChildRef(registration.Ref, registration.Ref.Kind) != nil ||
		len(registration.Canonical) == 0 ||
		sha256Hex(registration.Canonical) != registration.Ref.ArtifactSHA256 {
		return errors.New("adaptive evidence artifact registration is invalid")
	}
	switch registration.Ref.Kind {
	case "interference_plan":
		_, err := DecodeInterferencePlanArtifact(registration.Canonical)
		return err
	case "look_plan":
		_, err := DecodeEvidenceLookPlanArtifact(registration.Canonical)
		return err
	case "operator_approval":
		_, err := DecodeOperatorApproval(registration.Canonical)
		return err
	case "quality_outcome_model", "harm_outcome_model":
		if err := validateCanonicalRegisteredArtifact(
			registration.Canonical, &storedOutcomeModelMetadata{},
		); err != nil {
			return err
		}
	case "ope_plan":
		if err := validateCanonicalRegisteredArtifact(
			registration.Canonical, &storedOPEPlanMetadata{},
		); err != nil {
			return err
		}
	}
	var header struct {
		SchemaID string `json:"schema_id"`
		Revision int    `json:"revision"`
	}
	if err := json.Unmarshal(registration.Canonical, &header); err != nil {
		return fmt.Errorf("decode adaptive evidence artifact header: %w", err)
	}
	if header.SchemaID != registration.Ref.ArtifactSchemaID ||
		header.Revision != registration.Ref.ArtifactRevision {
		return errors.New("adaptive evidence artifact schema binding is invalid")
	}
	return nil
}

func validateCanonicalRegisteredArtifact(payload []byte, target any) error {
	if err := decodeStrictEvidence(payload, target); err != nil {
		return errors.New("adaptive evidence artifact is not strict JSON")
	}
	canonical, err := json.Marshal(target)
	if err != nil || !bytes.Equal(canonical, payload) {
		return errors.New("adaptive evidence artifact is noncanonical")
	}
	return nil
}
