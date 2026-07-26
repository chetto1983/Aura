package adaptive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// EvidenceKind separates admission from randomized outcome evidence.
type EvidenceKind string

const (
	// EvidenceCanaryAdmission alone may authorize shadow to canary.
	EvidenceCanaryAdmission EvidenceKind = "canary_admission"
	// EvidenceCanaryOutcome alone may authorize canary to active.
	EvidenceCanaryOutcome EvidenceKind = "canary_outcome"
)

// CanaryAdmissionBody binds only preregistered plans and approval.
type CanaryAdmissionBody struct {
	SchemaID                   string           `json:"schema_id"`
	Revision                   int              `json:"revision"`
	TargetState                string           `json:"target_state"`
	RandomizationPlanRef       ChildArtifactRef `json:"randomization_plan_ref"`
	InterferencePlanRef        ChildArtifactRef `json:"interference_plan_ref"`
	LookPlanRef                ChildArtifactRef `json:"look_plan_ref"`
	SnapshotRef                ChildArtifactRef `json:"snapshot_ref"`
	EvaluatorCalibrationSHA256 string           `json:"evaluator_calibration_sha256"`
	QualityOutcomeModelRef     ChildArtifactRef `json:"quality_outcome_model_ref"`
	HarmOutcomeModelRef        ChildArtifactRef `json:"harm_outcome_model_ref"`
	OPEPlanRef                 ChildArtifactRef `json:"ope_plan_ref"`
	PowerSimulationSHA256      string           `json:"power_simulation_sha256"`
	OperatorApprovalRef        ChildArtifactRef `json:"operator_approval_ref"`
}

// SHA256 returns the validated canonical admission-body content address.
func (body CanaryAdmissionBody) SHA256() (string, error) {
	canonical, err := body.canonicalJSON()
	if err != nil {
		return "", err
	}
	return sha256Hex(canonical), nil
}

func (body CanaryAdmissionBody) canonicalJSON() ([]byte, error) {
	if err := body.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(body)
}

func (body CanaryAdmissionBody) validate() error {
	if body.SchemaID != "aura.adaptive.evidence/canary-admission-body/v1" ||
		body.Revision != 1 || body.TargetState != string(PolicyCanary) {
		return errors.New("canary admission body schema or target is invalid")
	}
	rules := []struct {
		ref    ChildArtifactRef
		kind   string
		schema string
	}{
		{body.RandomizationPlanRef, "randomization_plan", "aura.adaptive.randomization-plan/v1"},
		{body.InterferencePlanRef, "interference_plan", "aura.adaptive.interference-plan/v1"},
		{body.LookPlanRef, "look_plan", "aura.adaptive.look-plan/utc-cutoff/v1"},
		{body.QualityOutcomeModelRef, "quality_outcome_model", "aura.adaptive.outcome-model/action-mean/v1"},
		{body.HarmOutcomeModelRef, "harm_outcome_model", "aura.adaptive.outcome-model/action-mean/v1"},
		{body.OPEPlanRef, "ope_plan", "aura.adaptive.ope-plan/v1"},
		{body.OperatorApprovalRef, "operator_approval", "aura.adaptive.operator-approval/v1"},
	}
	for _, rule := range rules {
		if err := validateChildArtifactRef(rule.ref, rule.kind, rule.schema); err != nil {
			return err
		}
	}
	if err := validateGenericChildRef(body.SnapshotRef, "snapshot"); err != nil {
		return err
	}
	if !validSHA256(body.EvaluatorCalibrationSHA256) ||
		!validSHA256(body.PowerSimulationSHA256) ||
		body.QualityOutcomeModelRef.ArtifactID == body.HarmOutcomeModelRef.ArtifactID ||
		body.QualityOutcomeModelRef.ArtifactSHA256 == body.HarmOutcomeModelRef.ArtifactSHA256 {
		return errors.New("canary admission body endpoint or calibration binding is invalid")
	}
	return nil
}

func validateGenericChildRef(ref ChildArtifactRef, kind string) error {
	if ref.SchemaID != ChildArtifactRefSchemaID || ref.Revision != 1 ||
		ref.Kind != kind || ref.ArtifactSchemaID == "" || ref.ArtifactRevision != 1 ||
		!canonicalUUID(ref.ArtifactID) || !validSHA256(ref.ArtifactSHA256) {
		return fmt.Errorf("adaptive %s child reference is invalid", kind)
	}
	return nil
}

// CanaryOutcomeBody binds one immutable closed randomized look.
type CanaryOutcomeBody struct {
	SchemaID                    string             `json:"schema_id"`
	Revision                    int                `json:"revision"`
	TargetState                 string             `json:"target_state"`
	LookNumber                  int                `json:"look_number"`
	LookCutoff                  time.Time          `json:"look_cutoff"`
	Alpha                       ExactRational      `json:"alpha"`
	CumulativeAlpha             ExactRational      `json:"cumulative_alpha"`
	PredecessorEvidenceID       *string            `json:"predecessor_evidence_id"`
	PredecessorEvidenceSHA256   *string            `json:"predecessor_evidence_sha256"`
	RandomizationPlanSHA256     string             `json:"randomization_plan_sha256"`
	AnalysisStratumSchemaSHA256 string             `json:"analysis_stratum_schema_sha256"`
	AnalysisStrataSHA256        string             `json:"analysis_strata_sha256"`
	ClosedLedgerSHA256          string             `json:"closed_ledger_sha256"`
	QualityReportID             string             `json:"quality_report_id"`
	QualityReportSHA256         string             `json:"quality_report_sha256"`
	HarmReportID                string             `json:"harm_report_id"`
	HarmReportSHA256            string             `json:"harm_report_sha256"`
	QualityOPEReportID          string             `json:"quality_ope_report_id"`
	QualityOPEReportSHA256      string             `json:"quality_ope_report_sha256"`
	HarmOPEReportID             string             `json:"harm_ope_report_id"`
	HarmOPEReportSHA256         string             `json:"harm_ope_report_sha256"`
	CensorDiagnosticsID         string             `json:"censor_diagnostics_id"`
	CensorDiagnosticsSHA256     string             `json:"censor_diagnostics_sha256"`
	Disposition                 OutcomeDisposition `json:"disposition"`
	Eligible                    bool               `json:"eligible"`
	IneligibleReasons           []string           `json:"ineligible_reasons"`
}

// SHA256 returns the validated canonical outcome-body content address.
func (body CanaryOutcomeBody) SHA256() (string, error) {
	canonical, err := body.canonicalJSON()
	if err != nil {
		return "", err
	}
	return sha256Hex(canonical), nil
}

func (body CanaryOutcomeBody) canonicalJSON() ([]byte, error) {
	if err := body.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(body)
}

func (body CanaryOutcomeBody) validate() error {
	if body.SchemaID != "aura.adaptive.evidence/canary-outcome-body/v1" ||
		body.Revision != 1 || body.TargetState != string(PolicyActive) ||
		body.LookNumber < 1 || body.LookNumber > 5 ||
		!canonicalEvidenceTime(body.LookCutoff) ||
		body.Alpha.Cmp(MustExactRational(0, 1)) <= 0 ||
		body.CumulativeAlpha.Cmp(body.Alpha) < 0 ||
		!sortedUniqueStrings(body.IneligibleReasons) {
		return errors.New("canary outcome body schema or look is invalid")
	}
	if body.LookNumber == 1 {
		if body.PredecessorEvidenceID != nil || body.PredecessorEvidenceSHA256 != nil {
			return errors.New("look one cannot bind predecessor evidence")
		}
	} else if body.PredecessorEvidenceID == nil ||
		body.PredecessorEvidenceSHA256 == nil ||
		!canonicalUUID(*body.PredecessorEvidenceID) ||
		!validSHA256(*body.PredecessorEvidenceSHA256) {
		return errors.New("successor look lacks its immediate predecessor")
	}
	hashes := []string{
		body.RandomizationPlanSHA256, body.AnalysisStratumSchemaSHA256,
		body.AnalysisStrataSHA256, body.ClosedLedgerSHA256,
		body.QualityReportSHA256, body.HarmReportSHA256,
		body.QualityOPEReportSHA256, body.HarmOPEReportSHA256,
		body.CensorDiagnosticsSHA256,
	}
	for _, hash := range hashes {
		if !validSHA256(hash) {
			return errors.New("canary outcome body contains invalid hash")
		}
	}
	ids := []string{
		body.QualityReportID, body.HarmReportID, body.QualityOPEReportID,
		body.HarmOPEReportID, body.CensorDiagnosticsID,
	}
	seenIDs := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !canonicalUUID(id) {
			return errors.New("canary outcome body contains invalid report identity")
		}
		if _, duplicate := seenIDs[id]; duplicate {
			return errors.New("canary outcome body report identities are not distinct")
		}
		seenIDs[id] = struct{}{}
	}
	if body.QualityReportSHA256 == body.HarmReportSHA256 ||
		body.QualityOPEReportSHA256 == body.HarmOPEReportSHA256 {
		return errors.New("canary outcome body endpoint hashes are not distinct")
	}
	switch body.Disposition {
	case OutcomeDispositionIneligible:
		if body.Eligible || len(body.IneligibleReasons) == 0 {
			return errors.New("ineligible outcome disposition is inconsistent")
		}
	case OutcomeDispositionContinue:
		if !body.Eligible || body.LookNumber == 5 || len(body.IneligibleReasons) != 0 {
			return errors.New("continuing outcome disposition is inconsistent")
		}
	case OutcomeDispositionPromote, OutcomeDispositionRetainCanary:
		if !body.Eligible || len(body.IneligibleReasons) != 0 {
			return errors.New("eligible outcome disposition is inconsistent")
		}
	default:
		return errors.New("canary outcome disposition is invalid")
	}
	return nil
}

type evidenceEnvelope struct {
	EvidenceID       string
	Kind             EvidenceKind
	OwnerID          string
	CohortID         string
	CohortSHA256     string
	Domain           Domain
	ProviderID       string
	ModelID          string
	PolicyEpoch      uint64
	PolicyVersion    string
	SnapshotID       string
	SnapshotSHA256   string
	CreatedAt        time.Time
	SealerActorID    string
	SealerCapability string
	BodySHA256       string
}

func (envelope evidenceEnvelope) validate(kind EvidenceKind) error {
	if envelope.Kind != kind || !canonicalUUID(envelope.EvidenceID) ||
		!canonicalUUID(envelope.OwnerID) || !canonicalUUID(envelope.CohortID) ||
		!canonicalUUID(envelope.SnapshotID) || !canonicalUUID(envelope.SealerActorID) ||
		!validSHA256(envelope.CohortSHA256) || !validSHA256(envelope.SnapshotSHA256) ||
		!validSHA256(envelope.BodySHA256) || !envelope.Domain.valid() ||
		envelope.ProviderID == "" || envelope.ModelID == "" ||
		envelope.PolicyEpoch == 0 || envelope.PolicyVersion == "" ||
		!canonicalEvidenceTime(envelope.CreatedAt) || envelope.SealerCapability == "" {
		return errors.New("sealed evidence envelope is invalid")
	}
	return nil
}

// CanaryAdmissionEvidence is the immutable admission envelope.
type CanaryAdmissionEvidence struct {
	SchemaID         string              `json:"schema_id"`
	Revision         int                 `json:"revision"`
	EvidenceID       string              `json:"evidence_id"`
	Kind             EvidenceKind        `json:"kind"`
	OwnerID          string              `json:"owner_id"`
	CohortID         string              `json:"cohort_id"`
	CohortSHA256     string              `json:"cohort_sha256"`
	Domain           Domain              `json:"domain"`
	ProviderID       string              `json:"provider_id"`
	ModelID          string              `json:"model_id"`
	PolicyEpoch      uint64              `json:"policy_epoch"`
	PolicyVersion    string              `json:"policy_version"`
	SnapshotID       string              `json:"snapshot_id"`
	SnapshotSHA256   string              `json:"snapshot_sha256"`
	CreatedAt        time.Time           `json:"created_at"`
	SealerActorID    string              `json:"sealer_actor_id"`
	SealerCapability string              `json:"sealer_capability"`
	BodySHA256       string              `json:"body_sha256"`
	Body             CanaryAdmissionBody `json:"body"`
}

// CanonicalJSON validates and emits the registered admission field order.
func (evidence CanaryAdmissionEvidence) CanonicalJSON() ([]byte, error) {
	bodySHA256, err := evidence.Body.SHA256()
	if err != nil {
		return nil, err
	}
	if evidence.SchemaID != "aura.adaptive.evidence/canary-admission/v1" ||
		evidence.Revision != 1 || bodySHA256 != evidence.BodySHA256 {
		return nil, errors.New("canary admission evidence body binding is invalid")
	}
	if err := evidence.admissionEnvelope().validate(EvidenceCanaryAdmission); err != nil {
		return nil, err
	}
	return json.Marshal(evidence)
}

// SHA256 returns the admission evidence content address.
func (evidence CanaryAdmissionEvidence) SHA256() (string, error) {
	canonical, err := evidence.CanonicalJSON()
	if err != nil {
		return "", err
	}
	return sha256Hex(canonical), nil
}

func (evidence CanaryAdmissionEvidence) admissionEnvelope() evidenceEnvelope {
	return evidenceEnvelope{
		EvidenceID: evidence.EvidenceID, Kind: evidence.Kind, OwnerID: evidence.OwnerID,
		CohortID: evidence.CohortID, CohortSHA256: evidence.CohortSHA256,
		Domain: evidence.Domain, ProviderID: evidence.ProviderID, ModelID: evidence.ModelID,
		PolicyEpoch: evidence.PolicyEpoch, PolicyVersion: evidence.PolicyVersion,
		SnapshotID: evidence.SnapshotID, SnapshotSHA256: evidence.SnapshotSHA256,
		CreatedAt: evidence.CreatedAt, SealerActorID: evidence.SealerActorID,
		SealerCapability: evidence.SealerCapability, BodySHA256: evidence.BodySHA256,
	}
}

// CanaryOutcomeEvidence is the immutable closed-look envelope.
type CanaryOutcomeEvidence struct {
	SchemaID         string            `json:"schema_id"`
	Revision         int               `json:"revision"`
	EvidenceID       string            `json:"evidence_id"`
	Kind             EvidenceKind      `json:"kind"`
	OwnerID          string            `json:"owner_id"`
	CohortID         string            `json:"cohort_id"`
	CohortSHA256     string            `json:"cohort_sha256"`
	Domain           Domain            `json:"domain"`
	ProviderID       string            `json:"provider_id"`
	ModelID          string            `json:"model_id"`
	PolicyEpoch      uint64            `json:"policy_epoch"`
	PolicyVersion    string            `json:"policy_version"`
	SnapshotID       string            `json:"snapshot_id"`
	SnapshotSHA256   string            `json:"snapshot_sha256"`
	CreatedAt        time.Time         `json:"created_at"`
	SealerActorID    string            `json:"sealer_actor_id"`
	SealerCapability string            `json:"sealer_capability"`
	BodySHA256       string            `json:"body_sha256"`
	Body             CanaryOutcomeBody `json:"body"`
}

// CanonicalJSON validates and emits the registered outcome field order.
func (evidence CanaryOutcomeEvidence) CanonicalJSON() ([]byte, error) {
	bodySHA256, err := evidence.Body.SHA256()
	if err != nil {
		return nil, err
	}
	if evidence.SchemaID != "aura.adaptive.evidence/canary-outcome/v1" ||
		evidence.Revision != 1 || bodySHA256 != evidence.BodySHA256 {
		return nil, errors.New("canary outcome evidence body binding is invalid")
	}
	if err := evidence.outcomeEnvelope().validate(EvidenceCanaryOutcome); err != nil {
		return nil, err
	}
	return json.Marshal(evidence)
}

// SHA256 returns the outcome evidence content address.
func (evidence CanaryOutcomeEvidence) SHA256() (string, error) {
	canonical, err := evidence.CanonicalJSON()
	if err != nil {
		return "", err
	}
	return sha256Hex(canonical), nil
}

func (evidence CanaryOutcomeEvidence) outcomeEnvelope() evidenceEnvelope {
	return evidenceEnvelope{
		EvidenceID: evidence.EvidenceID, Kind: evidence.Kind, OwnerID: evidence.OwnerID,
		CohortID: evidence.CohortID, CohortSHA256: evidence.CohortSHA256,
		Domain: evidence.Domain, ProviderID: evidence.ProviderID, ModelID: evidence.ModelID,
		PolicyEpoch: evidence.PolicyEpoch, PolicyVersion: evidence.PolicyVersion,
		SnapshotID: evidence.SnapshotID, SnapshotSHA256: evidence.SnapshotSHA256,
		CreatedAt: evidence.CreatedAt, SealerActorID: evidence.SealerActorID,
		SealerCapability: evidence.SealerCapability, BodySHA256: evidence.BodySHA256,
	}
}

// TransitionBinding is rebuilt from stored evidence inside a policy transition.
type TransitionBinding struct {
	SchemaID       string       `json:"schema_id"`
	Revision       int          `json:"revision"`
	FromState      string       `json:"from_state"`
	TargetState    string       `json:"target_state"`
	EvidenceKind   EvidenceKind `json:"evidence_kind"`
	EvidenceID     string       `json:"evidence_id"`
	EvidenceSHA256 string       `json:"evidence_sha256"`
	OwnerID        string       `json:"owner_id"`
	Domain         Domain       `json:"domain"`
	ProviderID     string       `json:"provider_id"`
	ModelID        string       `json:"model_id"`
	PolicyEpoch    uint64       `json:"policy_epoch"`
	PolicyVersion  string       `json:"policy_version"`
	SnapshotID     string       `json:"snapshot_id"`
	SnapshotSHA256 string       `json:"snapshot_sha256"`
	CohortID       string       `json:"cohort_id"`
	CohortSHA256   string       `json:"cohort_sha256"`
	LookNumber     *int         `json:"look_number"`
	LookCutoff     *time.Time   `json:"look_cutoff"`
	ActorID        string       `json:"actor_id"`
	Capability     string       `json:"capability"`
}

// CanonicalJSON validates and emits the server-built transition binding.
func (binding TransitionBinding) CanonicalJSON() ([]byte, error) {
	if binding.SchemaID != "aura.adaptive.transition-binding/v1" ||
		binding.Revision != 1 || !canonicalUUID(binding.EvidenceID) ||
		!canonicalUUID(binding.OwnerID) || !canonicalUUID(binding.SnapshotID) ||
		!canonicalUUID(binding.CohortID) || !canonicalUUID(binding.ActorID) ||
		!validSHA256(binding.EvidenceSHA256) || !validSHA256(binding.SnapshotSHA256) ||
		!validSHA256(binding.CohortSHA256) || !binding.Domain.valid() ||
		binding.ProviderID == "" || binding.ModelID == "" || binding.PolicyEpoch == 0 ||
		binding.PolicyVersion == "" || binding.Capability == "" {
		return nil, errors.New("adaptive transition binding is invalid")
	}
	switch binding.EvidenceKind {
	case EvidenceCanaryAdmission:
		if binding.FromState != string(PolicyShadow) ||
			binding.TargetState != string(PolicyCanary) ||
			binding.LookNumber != nil || binding.LookCutoff != nil {
			return nil, errors.New("admission transition binding is invalid")
		}
	case EvidenceCanaryOutcome:
		if binding.FromState != string(PolicyCanary) ||
			binding.TargetState != string(PolicyActive) ||
			binding.LookNumber == nil || *binding.LookNumber < 1 ||
			*binding.LookNumber > 5 || binding.LookCutoff == nil ||
			!canonicalEvidenceTime(*binding.LookCutoff) {
			return nil, errors.New("outcome transition binding is invalid")
		}
	default:
		return nil, errors.New("transition evidence kind is invalid")
	}
	return json.Marshal(binding)
}

// DecodeCanaryAdmissionEvidence rejects unknown, missing, duplicate, or noncanonical JSON.
func DecodeCanaryAdmissionEvidence(payload []byte) (CanaryAdmissionEvidence, error) {
	var evidence CanaryAdmissionEvidence
	if err := decodeStrictEvidence(payload, &evidence); err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	canonical, err := evidence.CanonicalJSON()
	if err != nil || !bytes.Equal(canonical, payload) {
		return CanaryAdmissionEvidence{}, errors.New("canary admission evidence is noncanonical")
	}
	return evidence, nil
}

// DecodeCanaryOutcomeEvidence rejects unknown, missing, duplicate, or noncanonical JSON.
func DecodeCanaryOutcomeEvidence(payload []byte) (CanaryOutcomeEvidence, error) {
	var evidence CanaryOutcomeEvidence
	if err := decodeStrictEvidence(payload, &evidence); err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	canonical, err := evidence.CanonicalJSON()
	if err != nil || !bytes.Equal(canonical, payload) {
		return CanaryOutcomeEvidence{}, errors.New("canary outcome evidence is noncanonical")
	}
	return evidence, nil
}

func decodeStrictEvidence(payload []byte, target any) error {
	return decodeStrictJSON(payload, target)
}

func canonicalUUID(value string) bool {
	return validateCanonicalUUID(value) == nil
}

func canonicalEvidenceTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC &&
		value.Equal(value.Truncate(time.Microsecond))
}
