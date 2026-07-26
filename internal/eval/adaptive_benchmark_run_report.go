package eval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/chetto1983/aura/internal/adaptive"
)

// Report and Qwen identifiers are the registered Amendment #93.11 values.
const (
	AdaptiveBenchmarkReportSchemaID = "aura.adaptive.benchmark-report/v1"
	AdaptiveBenchmarkReportRevision = 1

	AdaptiveBenchmarkQwenArtifactName   = "Qwen3.5-2B-Q4_K_M.gguf"
	AdaptiveBenchmarkQwenArtifactSHA256 = "aaf42c8b7c3cab2bf3d69c355048d4a0e" +
		"e9973d48f16c731c0520ee914699223"
)

// AdaptiveBenchmarkMode is one registered Task 10 entrypoint mode.
type AdaptiveBenchmarkMode string

// The two modes deliberately separate portability from promotable shadow evidence.
const (
	AdaptiveBenchmarkModeQwenPortability  AdaptiveBenchmarkMode = "qwen-portability"
	AdaptiveBenchmarkModeProductionShadow AdaptiveBenchmarkMode = "production-shadow"
)

// AdaptiveBenchmarkStatus is a scenario, control, summary, or run disposition.
type AdaptiveBenchmarkStatus string

// Status values distinguish quality failure from structurally invalid evidence.
const (
	AdaptiveBenchmarkStatusPass    AdaptiveBenchmarkStatus = "pass"
	AdaptiveBenchmarkStatusFail    AdaptiveBenchmarkStatus = "fail"
	AdaptiveBenchmarkStatusInvalid AdaptiveBenchmarkStatus = "invalid"
)

// AdaptiveBenchmarkBaseURLClass exposes topology without persisting an endpoint.
type AdaptiveBenchmarkBaseURLClass string

// URL classes expose topology without persisting an endpoint or credential.
const (
	AdaptiveBenchmarkBaseURLLoopback AdaptiveBenchmarkBaseURLClass = "loopback"
	AdaptiveBenchmarkBaseURLRemote   AdaptiveBenchmarkBaseURLClass = "remote"
)

// AdaptiveBenchmarkChangedFile binds one dirty-tree path to its digest.
type AdaptiveBenchmarkChangedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// AdaptiveBenchmarkGit binds the exact source state.
type AdaptiveBenchmarkGit struct {
	Revision     string                         `json:"revision"`
	Dirty        bool                           `json:"dirty"`
	ChangedFiles []AdaptiveBenchmarkChangedFile `json:"changed_files"`
}

// AdaptiveBenchmarkEnvironment contains sanitized runtime provenance.
type AdaptiveBenchmarkEnvironment struct {
	OS                  string  `json:"os"`
	Arch                string  `json:"arch"`
	AuraVersion         string  `json:"aura_version"`
	GoVersion           string  `json:"go_version"`
	MigrationHead       string  `json:"migration_head"`
	Neo4jRevision       *string `json:"neo4j_revision"`
	AgentMemoryRevision *string `json:"agent_memory_revision"`
}

// AdaptiveBenchmarkModel contains model and server artifact provenance.
type AdaptiveBenchmarkModel struct {
	ProviderID       string                        `json:"provider_id"`
	ModelID          string                        `json:"model_id"`
	BaseURLClass     AdaptiveBenchmarkBaseURLClass `json:"base_url_class"`
	ServerName       *string                       `json:"server_name"`
	ServerVersion    *string                       `json:"server_version"`
	ServerRevision   *string                       `json:"server_revision"`
	ArtifactName     *string                       `json:"artifact_name"`
	ArtifactSHA256   *string                       `json:"artifact_sha256"`
	SnapshotRevision *string                       `json:"snapshot_revision"`
	ImageDigest      *string                       `json:"image_digest"`
}

// AdaptiveBenchmarkFrozenInputs binds all decision-affecting inputs.
type AdaptiveBenchmarkFrozenInputs struct {
	DatasetID            string  `json:"dataset_id"`
	DatasetRevision      int     `json:"dataset_revision"`
	DatasetSHA256        string  `json:"dataset_sha256"`
	PromptTemplateSHA256 string  `json:"prompt_template_sha256"`
	EvaluatorID          string  `json:"evaluator_id"`
	EvaluatorRevision    int     `json:"evaluator_revision"`
	CalibrationSHA256    *string `json:"calibration_sha256"`
	ActionCatalogsSHA256 string  `json:"action_catalogs_sha256"`
	PolicyVersion        string  `json:"policy_version"`
	PolicySHA256         string  `json:"policy_sha256"`
	SnapshotID           *string `json:"snapshot_id"`
	SnapshotSHA256       *string `json:"snapshot_sha256"`
	PriceTableID         *string `json:"price_table_id"`
	PriceTableRevision   *int    `json:"price_table_revision"`
	PriceTableSHA256     *string `json:"price_table_sha256"`
	Seed                 uint64  `json:"seed"`
}

// AdaptiveBenchmarkActionProbability is catalog ordered and sums exactly to one.
type AdaptiveBenchmarkActionProbability struct {
	ActionID    string  `json:"action_id"`
	Probability float64 `json:"probability"`
}

// AdaptiveBenchmarkObservation is an independent quality or harm observation.
type AdaptiveBenchmarkObservation struct {
	EvaluatorID       string   `json:"evaluator_id"`
	EvaluatorRevision int      `json:"evaluator_revision"`
	Value             *float64 `json:"value"`
	Eligible          bool     `json:"eligible"`
	ReasonCodes       []string `json:"reason_codes"`
}

// AdaptiveBenchmarkScenarioRecord contains linkage and metrics, never raw content.
type AdaptiveBenchmarkScenarioRecord struct {
	ScenarioID          string                               `json:"scenario_id"`
	Domain              adaptive.Domain                      `json:"domain"`
	RequestID           *string                              `json:"request_id"`
	AssignmentID        *string                              `json:"assignment_id"`
	DeliveryEventID     *string                              `json:"delivery_event_id"`
	QualityOutcomeID    *string                              `json:"quality_outcome_id"`
	HarmOutcomeID       *string                              `json:"harm_outcome_id"`
	ChampionActionID    string                               `json:"champion_action_id"`
	RecommendedActionID string                               `json:"recommended_action_id"`
	IntendedActionID    string                               `json:"intended_action_id"`
	ActualActionID      string                               `json:"actual_action_id"`
	ActionProbabilities []AdaptiveBenchmarkActionProbability `json:"action_probabilities"`
	Quality             *AdaptiveBenchmarkObservation        `json:"quality"`
	Harm                *AdaptiveBenchmarkObservation        `json:"harm"`
	PromptTokens        int64                                `json:"prompt_tokens"`
	CompletionTokens    int64                                `json:"completion_tokens"`
	TotalTokens         int64                                `json:"total_tokens"`
	LatencyNS           int64                                `json:"latency_ns"`
	CostUSD             *float64                             `json:"cost_usd"`
	Status              AdaptiveBenchmarkStatus              `json:"status"`
	ReasonCodes         []string                             `json:"reason_codes"`
}

// AdaptiveBenchmarkControlRecord is identified by its fixed array position.
type AdaptiveBenchmarkControlRecord struct {
	Status      AdaptiveBenchmarkStatus `json:"status"`
	StartedAt   string                  `json:"started_at"`
	CompletedAt string                  `json:"completed_at"`
	EvidenceIDs []string                `json:"evidence_ids"`
	ReasonCodes []string                `json:"reason_codes"`
}

// AdaptiveBenchmarkDomainSummary is emitted in registered domain order.
type AdaptiveBenchmarkDomainSummary struct {
	Domain        adaptive.Domain `json:"domain"`
	ScenarioCount int             `json:"scenario_count"`
	PassCount     int             `json:"pass_count"`
	FailCount     int             `json:"fail_count"`
	InvalidCount  int             `json:"invalid_count"`
}

// AdaptiveBenchmarkSummary preserves counts and measurement missingness.
type AdaptiveBenchmarkSummary struct {
	ScenarioCount    int                              `json:"scenario_count"`
	PassCount        int                              `json:"pass_count"`
	FailCount        int                              `json:"fail_count"`
	InvalidCount     int                              `json:"invalid_count"`
	PromptTokens     int64                            `json:"prompt_tokens"`
	CompletionTokens int64                            `json:"completion_tokens"`
	TotalTokens      int64                            `json:"total_tokens"`
	LatencyCount     int                              `json:"latency_count"`
	LatencyP50NS     *int64                           `json:"latency_p50_ns"`
	LatencyP95NS     *int64                           `json:"latency_p95_ns"`
	LatencyP99NS     *int64                           `json:"latency_p99_ns"`
	CostCount        int                              `json:"cost_count"`
	CostUSD          *float64                         `json:"cost_usd"`
	Domains          []AdaptiveBenchmarkDomainSummary `json:"domains"`
}

// AdaptiveBenchmarkReport is the exact Task 10 canonical machine artifact.
type AdaptiveBenchmarkReport struct {
	SchemaID        string                            `json:"schema_id"`
	Revision        int                               `json:"revision"`
	RunID           string                            `json:"run_id"`
	Mode            AdaptiveBenchmarkMode             `json:"mode"`
	PortabilityOnly bool                              `json:"portability_only"`
	StartedAt       string                            `json:"started_at"`
	CompletedAt     string                            `json:"completed_at"`
	Seed            uint64                            `json:"seed"`
	Git             AdaptiveBenchmarkGit              `json:"git"`
	Environment     AdaptiveBenchmarkEnvironment      `json:"environment"`
	Model           AdaptiveBenchmarkModel            `json:"model"`
	FrozenInputs    AdaptiveBenchmarkFrozenInputs     `json:"frozen_inputs"`
	Scenarios       []AdaptiveBenchmarkScenarioRecord `json:"scenarios"`
	Controls        []AdaptiveBenchmarkControlRecord  `json:"controls"`
	Summary         AdaptiveBenchmarkSummary          `json:"summary"`
	Verdict         AdaptiveBenchmarkStatus           `json:"verdict"`
}

// CanonicalJSON validates and emits the registered field order.
func (report AdaptiveBenchmarkReport) CanonicalJSON() ([]byte, error) {
	if err := ValidateAdaptiveBenchmarkReport(report); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("marshal adaptive benchmark report: %w", err)
	}
	return payload, nil
}

// DecodeAdaptiveBenchmarkReport rejects any noncanonical or unregistered JSON.
func DecodeAdaptiveBenchmarkReport(
	payload []byte,
) (AdaptiveBenchmarkReport, error) {
	var report AdaptiveBenchmarkReport
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return AdaptiveBenchmarkReport{}, fmt.Errorf(
			"decode adaptive benchmark report: %w",
			err,
		)
	}
	if err := requireBenchmarkJSONEOF(decoder); err != nil {
		return AdaptiveBenchmarkReport{}, err
	}
	canonical, err := report.CanonicalJSON()
	if err != nil {
		return AdaptiveBenchmarkReport{}, err
	}
	if !bytes.Equal(payload, canonical) {
		return AdaptiveBenchmarkReport{}, errors.New(
			"adaptive benchmark report is not canonical JSON",
		)
	}
	return report, nil
}

// AdaptiveBenchmarkPromotionEvidence cannot represent portability-only input.
type AdaptiveBenchmarkPromotionEvidence struct {
	report AdaptiveBenchmarkReport
}

// VerifyAdaptiveBenchmarkReportForPromotion deterministically revalidates
// persisted canonical evidence at the production admission boundary.
func VerifyAdaptiveBenchmarkReportForPromotion(
	report AdaptiveBenchmarkReport,
) error {
	if err := ValidateAdaptiveBenchmarkReport(report); err != nil {
		return err
	}
	if report.PortabilityOnly ||
		report.Mode != AdaptiveBenchmarkModeProductionShadow ||
		report.Verdict != AdaptiveBenchmarkStatusPass ||
		slices.ContainsFunc(
			report.Controls,
			func(control AdaptiveBenchmarkControlRecord) bool {
				return control.Status != AdaptiveBenchmarkStatusPass
			},
		) {
		return errors.New(
			"benchmark is not passing production promotion evidence",
		)
	}
	return nil
}

// NewAdaptiveBenchmarkPromotionEvidence enforces the admission boundary by type.
func NewAdaptiveBenchmarkPromotionEvidence(
	report AdaptiveBenchmarkReport,
) (AdaptiveBenchmarkPromotionEvidence, error) {
	if err := VerifyAdaptiveBenchmarkReportForPromotion(report); err != nil {
		return AdaptiveBenchmarkPromotionEvidence{}, err
	}
	return AdaptiveBenchmarkPromotionEvidence{report: report}, nil
}
