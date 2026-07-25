package adaptive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// OPEIneligibleReasonCount is one canonical diagnostic reason frequency.
type OPEIneligibleReasonCount struct {
	SchemaID string `json:"schema_id"`
	Revision int    `json:"revision"`
	Reason   string `json:"reason"`
	Count    uint64 `json:"count"`
}

// EndpointOPEReport is the exact endpoint-specific OPE report envelope.
type EndpointOPEReport struct {
	SchemaID                        string                     `json:"schema_id"`
	Revision                        int                        `json:"revision"`
	ReportID                        string                     `json:"report_id"`
	OwnerID                         string                     `json:"owner_id"`
	CohortID                        string                     `json:"cohort_id"`
	LookNumber                      int                        `json:"look_number"`
	LookCutoff                      time.Time                  `json:"look_cutoff"`
	EndpointKind                    EvidenceEndpointKind       `json:"endpoint_kind"`
	EndpointID                      string                     `json:"endpoint_id"`
	Evaluator                       CanonicalEvaluator         `json:"evaluator"`
	ClosedLedgerSHA256              string                     `json:"closed_ledger_sha256"`
	AnalysisStratumSchemaSHA256     string                     `json:"analysis_stratum_schema_sha256"`
	InterferenceClusterSchemaSHA256 string                     `json:"interference_cluster_schema_sha256"`
	OPEPlanSHA256                   string                     `json:"ope_plan_sha256"`
	TargetPolicySHA256              string                     `json:"target_policy_sha256"`
	BehaviorPolicySHA256            string                     `json:"behavior_policy_sha256"`
	OutcomeModelID                  string                     `json:"outcome_model_id"`
	OutcomeModelSHA256              string                     `json:"outcome_model_sha256"`
	PredictionSHA256                string                     `json:"prediction_sha256"`
	Eligible                        bool                       `json:"eligible"`
	IneligibleReasons               []string                   `json:"ineligible_reasons"`
	RowCount                        uint64                     `json:"row_count"`
	IneligibleRowCount              uint64                     `json:"ineligible_row_count"`
	IneligibleReasonCounts          []OPEIneligibleReasonCount `json:"ineligible_reason_counts"`
	DR                              *float64                   `json:"dr"`
	SNIPS                           *float64                   `json:"snips"`
	ESS                             *float64                   `json:"ess"`
	MinimumESS                      ExactRational              `json:"minimum_ess"`
	MinimumOverlap                  ExactRational              `json:"minimum_overlap"`
	ObservedMinimumOverlap          *ExactRational             `json:"observed_minimum_overlap"`
	UncertaintyAlgorithmID          string                     `json:"uncertainty_algorithm_id"`
	UncertaintyRevision             int                        `json:"uncertainty_revision"`
	Confidence                      ExactRational              `json:"confidence"`
	DRLower                         *float64                   `json:"dr_lower"`
	DRUpper                         *float64                   `json:"dr_upper"`
}

// CanonicalJSON validates and emits the registered field order.
func (report EndpointOPEReport) CanonicalJSON() ([]byte, error) {
	if err := report.validate(); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("marshal endpoint OPE report: %w", err)
	}
	return canonical, nil
}

// SHA256 returns the endpoint report content address.
func (report EndpointOPEReport) SHA256() (string, error) {
	canonical, err := report.CanonicalJSON()
	if err != nil {
		return "", err
	}
	return sha256Hex(canonical), nil
}

func (report EndpointOPEReport) validate() error {
	if report.SchemaID != "aura.adaptive.report/ope/v1" || report.Revision != 1 {
		return errors.New("endpoint OPE report schema is invalid")
	}
	for name, value := range map[string]string{
		"report_id": report.ReportID, "owner_id": report.OwnerID,
		"cohort_id": report.CohortID, "outcome_model_id": report.OutcomeModelID,
	} {
		if err := validateCanonicalUUID(value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if report.LookNumber < 1 || report.LookNumber > 5 ||
		report.LookCutoff.Location() != time.UTC ||
		!report.LookCutoff.Equal(report.LookCutoff.Truncate(time.Microsecond)) {
		return errors.New("endpoint OPE report look is invalid")
	}
	if !validEvidenceEndpoint(string(report.EndpointKind)) ||
		report.EndpointID == "" ||
		!report.Evaluator.valid() {
		return errors.New("endpoint OPE report endpoint is invalid")
	}
	hashes := []string{
		report.ClosedLedgerSHA256, report.AnalysisStratumSchemaSHA256,
		report.InterferenceClusterSchemaSHA256, report.OPEPlanSHA256,
		report.TargetPolicySHA256, report.BehaviorPolicySHA256,
		report.OutcomeModelSHA256, report.PredictionSHA256,
	}
	for _, hash := range hashes {
		if !validSHA256(hash) {
			return errors.New("endpoint OPE report hash is invalid")
		}
	}
	if !sortedUniqueStrings(report.IneligibleReasons) ||
		!sortedUniqueReasonCounts(report.IneligibleReasonCounts) ||
		report.IneligibleRowCount > report.RowCount ||
		report.MinimumESS.Cmp(MustExactRational(0, 1)) <= 0 ||
		report.MinimumOverlap.Cmp(MustExactRational(1, 2)) != 0 {
		return errors.New("endpoint OPE report support diagnostics are invalid")
	}
	if report.Eligible && len(report.IneligibleReasons) != 0 {
		return errors.New("eligible endpoint OPE report has ineligible reasons")
	}
	for _, value := range []*float64{
		report.DR, report.SNIPS, report.ESS, report.DRLower, report.DRUpper,
	} {
		if value != nil && !finite(*value) {
			return errors.New("endpoint OPE report contains nonfinite diagnostics")
		}
	}
	if report.Eligible && (report.DR == nil || report.SNIPS == nil || report.ESS == nil) {
		return errors.New("eligible endpoint OPE report lacks estimators")
	}
	if report.ObservedMinimumOverlap != nil &&
		(report.ObservedMinimumOverlap.Cmp(MustExactRational(0, 1)) < 0 ||
			report.ObservedMinimumOverlap.Cmp(MustExactRational(1, 1)) > 0) {
		return errors.New("endpoint OPE report observed overlap is invalid")
	}
	if report.UncertaintyAlgorithmID != "cluster-percentile-bootstrap-sha256-v1" ||
		report.UncertaintyRevision != 1 ||
		report.Confidence.Cmp(MustExactRational(19, 20)) != 0 {
		return errors.New("endpoint OPE report uncertainty contract is invalid")
	}
	if (report.DRLower == nil) != (report.DRUpper == nil) ||
		report.DRLower != nil && *report.DRLower > *report.DRUpper {
		return errors.New("endpoint OPE report uncertainty interval is invalid")
	}
	return nil
}

// ValidateDistinctEndpointOPEReports rejects composite or substituted endpoint evidence.
func ValidateDistinctEndpointOPEReports(quality, harm EndpointOPEReport) error {
	qualityBytes, err := quality.CanonicalJSON()
	if err != nil {
		return err
	}
	harmBytes, err := harm.CanonicalJSON()
	if err != nil {
		return err
	}
	if quality.EndpointKind != EvidenceEndpointQuality ||
		harm.EndpointKind != EvidenceEndpointHarm {
		return errors.New("endpoint OPE reports are not quality/harm ordered")
	}
	if quality.ReportID == harm.ReportID ||
		quality.OutcomeModelID == harm.OutcomeModelID ||
		quality.OutcomeModelSHA256 == harm.OutcomeModelSHA256 ||
		quality.PredictionSHA256 == harm.PredictionSHA256 ||
		bytes.Equal(qualityBytes, harmBytes) ||
		sha256Hex(qualityBytes) == sha256Hex(harmBytes) {
		return errors.New("quality and harm OPE reports are not distinct")
	}
	return nil
}

func sortedUniqueStrings(values []string) bool {
	for index, value := range values {
		if value == "" || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return values != nil
}

func sortedUniqueReasonCounts(values []OPEIneligibleReasonCount) bool {
	for index, value := range values {
		if value.SchemaID != "aura.adaptive.report/ineligible-reason-count/v1" ||
			value.Revision != 1 || value.Reason == "" || value.Count == 0 ||
			index > 0 && values[index-1].Reason >= value.Reason {
			return false
		}
	}
	return values != nil && slices.IsSortedFunc(values, func(left, right OPEIneligibleReasonCount) int {
		if left.Reason < right.Reason {
			return -1
		}
		if left.Reason > right.Reason {
			return 1
		}
		return 0
	})
}
