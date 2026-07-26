package adaptive

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var evidenceReportNamespace = uuid.MustParse("7af55574-635e-4f49-a5a3-0e158eb4ef15")

const (
	qualityMissingRuleID = "quality-missing-challenger-0-baseline-1-v1"
	harmMissingRuleID    = "harm-missing-challenger-1-baseline-0-v1"
)

// ATEReportStratum is one canonical exact-inference stratum.
type ATEReportStratum struct {
	SchemaID          string `json:"schema_id"`
	Revision          int    `json:"revision"`
	AnalysisStratumID string `json:"analysis_stratum_id"`
	N                 int    `json:"n"`
	Treated           int    `json:"treated"`
	Control           int    `json:"control"`
	TreatedSuccesses  int    `json:"treated_successes"`
	ControlSuccesses  int    `json:"control_successes"`
	L1                int    `json:"l1"`
	U1                int    `json:"u1"`
	L0                int    `json:"l0"`
	U0                int    `json:"u0"`
}

type ateReportPrefix struct {
	SchemaID                    string             `json:"schema_id"`
	Revision                    int                `json:"revision"`
	ReportID                    string             `json:"report_id"`
	OwnerID                     string             `json:"owner_id"`
	CohortID                    string             `json:"cohort_id"`
	LookNumber                  int                `json:"look_number"`
	LookCutoff                  time.Time          `json:"look_cutoff"`
	EndpointID                  string             `json:"endpoint_id"`
	Evaluator                   CanonicalEvaluator `json:"evaluator"`
	ClosedLedgerSHA256          string             `json:"closed_ledger_sha256"`
	AnalysisStratumSchemaSHA256 string             `json:"analysis_stratum_schema_sha256"`
	AnalysisStrataSHA256        string             `json:"analysis_strata_sha256"`
	AlgorithmID                 string             `json:"algorithm_id"`
	AlgorithmRevision           int                `json:"algorithm_revision"`
	ArithmeticRevision          string             `json:"arithmetic_revision"`
	Alpha                       ExactRational      `json:"alpha"`
}

// QualityATEReport is the exact one-sided quality gate report.
type QualityATEReport struct {
	ateReportPrefix
	MinimumUplift     ExactRational      `json:"minimum_uplift"`
	MissingRuleID     string             `json:"missing_rule_id"`
	Computed          bool               `json:"computed"`
	IneligibleReasons []string           `json:"ineligible_reasons"`
	Strata            []ATEReportStratum `json:"strata"`
	ATELower          *ExactRational     `json:"ate_lower"`
	Passes            bool               `json:"passes"`
}

// CanonicalJSON validates and emits the quality report.
func (report QualityATEReport) CanonicalJSON() ([]byte, error) {
	if err := validateATEReport(
		report.ateReportPrefix, "aura.adaptive.report/quality-ate/v1",
		report.MissingRuleID, report.Computed, report.IneligibleReasons,
		report.Strata, report.ATELower, report.Passes,
	); err != nil {
		return nil, err
	}
	return json.Marshal(report)
}

// HarmATEReport is the exact one-sided harm gate report.
type HarmATEReport struct {
	ateReportPrefix
	MaximumIncrease   ExactRational      `json:"maximum_increase"`
	MissingRuleID     string             `json:"missing_rule_id"`
	Computed          bool               `json:"computed"`
	IneligibleReasons []string           `json:"ineligible_reasons"`
	Strata            []ATEReportStratum `json:"strata"`
	ATEUpper          *ExactRational     `json:"ate_upper"`
	Passes            bool               `json:"passes"`
}

// CanonicalJSON validates and emits the harm report.
func (report HarmATEReport) CanonicalJSON() ([]byte, error) {
	if err := validateATEReport(
		report.ateReportPrefix, "aura.adaptive.report/harm-ate/v1",
		report.MissingRuleID, report.Computed, report.IneligibleReasons,
		report.Strata, report.ATEUpper, report.Passes,
	); err != nil {
		return nil, err
	}
	return json.Marshal(report)
}

func validateATEReport(
	prefix ateReportPrefix,
	expectedSchema string,
	missingRuleID string,
	computed bool,
	reasons []string,
	strata []ATEReportStratum,
	bound *ExactRational,
	passes bool,
) error {
	if prefix.SchemaID != expectedSchema || prefix.Revision != 1 ||
		!canonicalUUID(prefix.ReportID) || !canonicalUUID(prefix.OwnerID) ||
		!canonicalUUID(prefix.CohortID) ||
		prefix.LookNumber < 1 || prefix.LookNumber > 5 ||
		!canonicalEvidenceTime(prefix.LookCutoff) || prefix.EndpointID == "" ||
		!prefix.Evaluator.valid() || !validSHA256(prefix.ClosedLedgerSHA256) ||
		!validSHA256(prefix.AnalysisStratumSchemaSHA256) ||
		!validSHA256(prefix.AnalysisStrataSHA256) ||
		prefix.AlgorithmID != "stratified-bonferroni-hypergeom-ate-v1" ||
		prefix.AlgorithmRevision != 1 ||
		prefix.ArithmeticRevision != "exact-bigint-rational-v1" ||
		prefix.Alpha.Cmp(MustExactRational(0, 1)) <= 0 || missingRuleID == "" ||
		!sortedUniqueStrings(reasons) {
		return errors.New("adaptive exact ATE report envelope is invalid")
	}
	if computed {
		if len(reasons) != 0 || len(strata) == 0 || bound == nil {
			return errors.New("computed exact ATE report is incomplete")
		}
	} else if len(reasons) == 0 || len(strata) != 0 || bound != nil || passes {
		return errors.New("unavailable exact ATE report is inconsistent")
	}
	for index, stratum := range strata {
		if stratum.SchemaID != "aura.adaptive.report/ate-stratum/v1" ||
			stratum.Revision != 1 || !validSHA256(stratum.AnalysisStratumID) ||
			stratum.N <= 0 || stratum.Treated < 0 ||
			stratum.Control != stratum.N-stratum.Treated ||
			index > 0 &&
				strata[index-1].AnalysisStratumID >= stratum.AnalysisStratumID {
			return errors.New("adaptive exact ATE report stratum is invalid")
		}
	}
	return nil
}

// CensorDiagnosticsReport is the canonical total/differential censor gate.
type CensorDiagnosticsReport struct {
	SchemaID                    string         `json:"schema_id"`
	Revision                    int            `json:"revision"`
	ReportID                    string         `json:"report_id"`
	OwnerID                     string         `json:"owner_id"`
	CohortID                    string         `json:"cohort_id"`
	LookNumber                  int            `json:"look_number"`
	LookCutoff                  time.Time      `json:"look_cutoff"`
	ClosedLedgerSHA256          string         `json:"closed_ledger_sha256"`
	AnalysisStratumSchemaSHA256 string         `json:"analysis_stratum_schema_sha256"`
	Computed                    bool           `json:"computed"`
	TotalMembers                int            `json:"total_members"`
	CensoredTotal               int            `json:"censored_total"`
	CensorRate                  *ExactRational `json:"censor_rate"`
	BaselineMembers             int            `json:"baseline_members"`
	BaselineCensored            int            `json:"baseline_censored"`
	BaselineRate                *ExactRational `json:"baseline_rate"`
	ChallengerMembers           int            `json:"challenger_members"`
	ChallengerCensored          int            `json:"challenger_censored"`
	ChallengerRate              *ExactRational `json:"challenger_rate"`
	AbsoluteRateDifference      *ExactRational `json:"absolute_rate_difference"`
	MaxCensorRate               *ExactRational `json:"max_censor_rate"`
	MaxCensorImbalance          *ExactRational `json:"max_censor_imbalance"`
	TotalPass                   bool           `json:"total_pass"`
	DifferentialPass            bool           `json:"differential_pass"`
	Passes                      bool           `json:"passes"`
	Reasons                     []string       `json:"reasons"`
}

// CanonicalJSON validates and emits the censor report.
func (report CensorDiagnosticsReport) CanonicalJSON() ([]byte, error) {
	if report.SchemaID != "aura.adaptive.report/censor-diagnostics/v1" ||
		report.Revision != 1 || !canonicalUUID(report.ReportID) ||
		!canonicalUUID(report.OwnerID) || !canonicalUUID(report.CohortID) ||
		report.LookNumber < 1 || report.LookNumber > 5 ||
		!canonicalEvidenceTime(report.LookCutoff) ||
		!validSHA256(report.ClosedLedgerSHA256) ||
		!validSHA256(report.AnalysisStratumSchemaSHA256) ||
		!sortedUniqueStrings(report.Reasons) {
		return nil, errors.New("adaptive censor report envelope is invalid")
	}
	numerics := []*ExactRational{
		report.CensorRate, report.BaselineRate, report.ChallengerRate,
		report.AbsoluteRateDifference, report.MaxCensorRate,
		report.MaxCensorImbalance,
	}
	if report.Computed {
		for _, value := range numerics {
			if value == nil {
				return nil, errors.New("computed censor report lacks rate")
			}
		}
		if report.Passes != (report.TotalPass && report.DifferentialPass) {
			return nil, errors.New("computed censor report pass state is inconsistent")
		}
	} else {
		for _, value := range numerics {
			if value != nil {
				return nil, errors.New("unavailable censor report contains rate")
			}
		}
		if report.Passes || report.TotalPass || report.DifferentialPass ||
			len(report.Reasons) == 0 {
			return nil, errors.New("unavailable censor report is inconsistent")
		}
	}
	return json.Marshal(report)
}

type gateReports struct {
	Quality       QualityATEReport
	Harm          HarmATEReport
	Censor        CensorDiagnosticsReport
	QualitySHA256 string
	HarmSHA256    string
	CensorSHA256  string
	StrataSHA256  string
	GateValid     bool
}

func newATEReportPrefix(
	cohort *FocalCohort,
	look EvidenceLook,
	ledgerSHA256 string,
	strataSHA256 string,
	endpoint EvidenceEndpointKind,
) ateReportPrefix {
	schemaID := "aura.adaptive.report/quality-ate/v1"
	reportKind := "quality-ate"
	frozen := cohort.PrimaryQualityEndpoint()
	if endpoint == EvidenceEndpointHarm {
		schemaID = "aura.adaptive.report/harm-ate/v1"
		reportKind = "harm-ate"
		frozen = cohort.PrimaryHarmEndpoint()
	}
	scope := cohort.Scope()
	return ateReportPrefix{
		SchemaID: schemaID, Revision: 1,
		ReportID: reportID(cohort.ID(), look.Number, reportKind, ledgerSHA256),
		OwnerID:  scope.OwnerID.String(), CohortID: cohort.ID().String(),
		LookNumber: look.Number, LookCutoff: look.Cutoff,
		EndpointID: frozen.ID, Evaluator: canonicalEvaluator(frozen.Evaluator),
		ClosedLedgerSHA256: ledgerSHA256,
		AnalysisStratumSchemaSHA256: cohort.v2.randomizationPlan.
			AnalysisStratumSchemaSHA256,
		AnalysisStrataSHA256: strataSHA256,
		AlgorithmID:          "stratified-bonferroni-hypergeom-ate-v1",
		AlgorithmRevision:    1,
		ArithmeticRevision:   "exact-bigint-rational-v1",
		Alpha:                look.Alpha,
	}
}

func newQualityATEReport(
	prefix ateReportPrefix,
	reasons []string,
) QualityATEReport {
	return QualityATEReport{
		ateReportPrefix:   prefix,
		MinimumUplift:     MustExactRational(0, 1),
		MissingRuleID:     qualityMissingRuleID,
		IneligibleReasons: reasons,
	}
}

func newHarmATEReport(
	prefix ateReportPrefix,
	reasons []string,
) HarmATEReport {
	return HarmATEReport{
		ateReportPrefix:   prefix,
		MaximumIncrease:   MustExactRational(0, 1),
		MissingRuleID:     harmMissingRuleID,
		IneligibleReasons: reasons,
	}
}

func buildGateReports(
	ctx context.Context,
	cohort *FocalCohort,
	look EvidenceLook,
	ledger CohortLedger,
) (gateReports, error) {
	if cohort == nil || !cohort.randomizationEligible() ||
		ledger.State != CohortLedgerClosed || !validSHA256(ledger.SHA256) {
		return gateReports{}, errors.New("adaptive gate report input is invalid")
	}
	strataIDs, qualityCounts, harmCounts, err := endpointCountsByStratum(ledger.Members)
	if err != nil {
		return gateReports{}, err
	}
	strataBytes, err := json.Marshal(strataIDs)
	if err != nil {
		return gateReports{}, err
	}
	strataSHA256 := sha256Hex(strataBytes)
	qualityExact, qualityErr := ExactATEBounds(
		ctx, qualityCounts, look.Alpha, DefaultExactInferenceCaps(),
	)
	harmExact, harmErr := ExactATEBounds(
		ctx, harmCounts, look.Alpha, DefaultExactInferenceCaps(),
	)
	quality := newQualityATEReport(
		newATEReportPrefix(
			cohort, look, ledger.SHA256, strataSHA256, EvidenceEndpointQuality,
		),
		[]string{},
	)
	harm := newHarmATEReport(
		newATEReportPrefix(
			cohort, look, ledger.SHA256, strataSHA256, EvidenceEndpointHarm,
		),
		[]string{},
	)
	quality.MinimumUplift, err = exactRationalFromFloat(
		cohort.Margins().MinimumQualityUplift,
	)
	if err != nil {
		return gateReports{}, err
	}
	harm.MaximumIncrease, err = exactRationalFromFloat(
		cohort.Margins().MaximumHarmIncrease,
	)
	if err != nil {
		return gateReports{}, err
	}
	if qualityErr == nil {
		quality.Computed = true
		quality.Strata = reportStrata(qualityCounts, qualityExact)
		quality.ATELower = &qualityExact.Lower
		quality.Passes = qualityExact.Lower.Cmp(quality.MinimumUplift) > 0
	} else {
		quality.IneligibleReasons = []string{"exact_inference_unavailable"}
		quality.Strata = []ATEReportStratum{}
	}
	if harmErr == nil {
		harm.Computed = true
		harm.Strata = reportStrata(harmCounts, harmExact)
		harm.ATEUpper = &harmExact.Upper
		harm.Passes = harmExact.Upper.Cmp(harm.MaximumIncrease) <= 0
	} else {
		harm.IneligibleReasons = []string{"exact_inference_unavailable"}
		harm.Strata = []ATEReportStratum{}
	}
	censor, err := buildCensorReport(cohort, look, ledger)
	if err != nil {
		return gateReports{}, err
	}
	qualityBytes, err := quality.CanonicalJSON()
	if err != nil {
		return gateReports{}, err
	}
	harmBytes, err := harm.CanonicalJSON()
	if err != nil {
		return gateReports{}, err
	}
	censorBytes, err := censor.CanonicalJSON()
	if err != nil {
		return gateReports{}, err
	}
	return gateReports{
		Quality: quality, Harm: harm, Censor: censor,
		QualitySHA256: sha256Hex(qualityBytes),
		HarmSHA256:    sha256Hex(harmBytes),
		CensorSHA256:  sha256Hex(censorBytes),
		StrataSHA256:  strataSHA256,
		GateValid:     qualityErr == nil && harmErr == nil && censor.Passes,
	}, nil
}
