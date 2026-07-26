package eval

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestAdaptiveBenchmarkDriverReceivesNoGold(t *testing.T) {
	t.Parallel()

	subjectType := reflect.TypeOf(AdaptiveBenchmarkSubject{})
	gotFields := make([]string, subjectType.NumField())
	for index := range subjectType.NumField() {
		gotFields[index] = subjectType.Field(index).Name
	}
	if want := []string{"ScenarioID", "Domain", "Prompt"}; !slices.Equal(
		gotFields,
		want,
	) {
		t.Fatalf("subject fields = %v, want %v", gotFields, want)
	}

	dataset := benchmarkDatasetFixture(t)
	driver := &goldIsolationDriver{
		scenarios: make(map[string]AdaptiveBenchmarkScenario, 52),
	}
	for _, scenario := range dataset.Scenarios {
		driver.scenarios[scenario.ScenarioID] = scenario
	}
	if _, err := RunAdaptiveBenchmarkScenarios(
		t.Context(),
		dataset,
		driver,
	); err != nil {
		t.Fatalf("RunAdaptiveBenchmarkScenarios: %v", err)
	}
	if len(driver.subjects) != 52 {
		t.Fatalf("subjects = %d, want 52", len(driver.subjects))
	}
	for _, subject := range driver.subjects {
		scenario := driver.scenarios[subject.ScenarioID]
		if subject.Domain != scenario.Domain || subject.Prompt != scenario.Prompt {
			t.Fatalf("subject drift = %#v, scenario %#v", subject, scenario)
		}
	}
	resultType := reflect.TypeOf(AdaptiveBenchmarkDriverResult{})
	for _, forbidden := range []string{"Quality", "QualityOutcomeID"} {
		if _, exposed := resultType.FieldByName(forbidden); exposed {
			t.Fatalf("turn result exposes pre-evaluated field %q", forbidden)
		}
	}
}

func TestRunAdaptiveBenchmarkScenariosRecordsEvaluationAfterTurn(t *testing.T) {
	t.Parallel()

	dataset := benchmarkDatasetFixture(t)
	driver := newAdaptiveBenchmarkDriverFake(dataset)
	run, err := RunAdaptiveBenchmarkScenarios(t.Context(), dataset, driver)
	if err != nil {
		t.Fatalf("RunAdaptiveBenchmarkScenarios: %v", err)
	}
	if len(driver.evaluations) != 52 || len(driver.events) != 104 {
		t.Fatalf(
			"evaluations/events = %d/%d",
			len(driver.evaluations),
			len(driver.events),
		)
	}
	for index, evaluation := range driver.evaluations {
		if driver.events[index*2] != "turn:"+evaluation.ScenarioID ||
			driver.events[index*2+1] != "outcome:"+evaluation.ScenarioID {
			t.Fatalf("event order = %v", driver.events[index*2:index*2+2])
		}
		if evaluation.RequestID == "" ||
			evaluation.AssignmentID == "" ||
			evaluation.DeliveryEventID == "" {
			t.Fatalf("evaluation linkage = %#v", evaluation)
		}
		if run.Scenarios[index].QualityOutcomeID == nil ||
			run.Scenarios[index].Quality == nil {
			t.Fatalf("persisted evaluation missing at %d", index)
		}
	}
}

func TestRunAdaptiveBenchmarkScenariosUsesRealDriverSequentially(
	t *testing.T,
) {
	t.Parallel()

	dataset := benchmarkDatasetFixture(t)
	driver := newAdaptiveBenchmarkDriverFake(dataset)
	run, err := RunAdaptiveBenchmarkScenarios(
		t.Context(),
		dataset,
		driver,
	)
	if err != nil {
		t.Fatalf("runAdaptiveBenchmarkScenarios: %v", err)
	}
	if len(run.Scenarios) != 52 ||
		driver.maximumInFlight != 1 ||
		run.MissingCostCount != 52 ||
		run.ProviderCostCount != 0 ||
		run.PriceTableCostCount != 0 {
		t.Fatalf("run/driver = %#v / %#v", run, driver)
	}
	wantOrder := AdaptiveBenchmarkScenarioOrder(dataset.Scenarios)
	if !slices.EqualFunc(
		driver.subjects,
		wantOrder,
		func(left AdaptiveBenchmarkSubject, right AdaptiveBenchmarkScenario) bool {
			return left.ScenarioID == right.ScenarioID
		},
	) {
		t.Fatalf("driver order = %#v, want SHA schedule", driver.subjects)
	}
	for _, record := range run.Scenarios {
		if record.LatencyNS != 10 ||
			record.RequestID == nil ||
			record.AssignmentID == nil ||
			record.DeliveryEventID == nil ||
			record.QualityOutcomeID == nil {
			t.Fatalf("record is missing core timing/linkage: %#v", record)
		}
	}
	payload, err := json.Marshal(run.Scenarios)
	if err != nil {
		t.Fatalf("marshal records: %v", err)
	}
	for _, forbidden := range []string{
		`"prompt"`, `"expected"`, `"answer"`, `"tool_id"`,
		`"skill_id"`, `"result_ids"`,
	} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("report records leaked transient field %s", forbidden)
		}
	}
}

func TestRunAdaptiveBenchmarkScenariosPublicEntrypoint(t *testing.T) {
	t.Parallel()

	run, err := RunAdaptiveBenchmarkScenarios(
		t.Context(),
		benchmarkDatasetFixture(t),
		newAdaptiveBenchmarkDriverFake(benchmarkDatasetFixture(t)),
	)
	if err != nil {
		t.Fatalf("RunAdaptiveBenchmarkScenarios: %v", err)
	}
	if len(run.Scenarios) != 52 {
		t.Fatalf("scenario count = %d, want 52", len(run.Scenarios))
	}
}

func TestRunAdaptiveBenchmarkScenariosRecordsBrokenDriverFacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*AdaptiveBenchmarkDriverResult)
	}{
		{
			name: "request linkage",
			mutate: func(result *AdaptiveBenchmarkDriverResult) {
				result.RequestID = ""
			},
		},
		{
			name: "assignment linkage",
			mutate: func(result *AdaptiveBenchmarkDriverResult) {
				result.AssignmentID = ""
			},
		},
		{
			name: "delivery linkage",
			mutate: func(result *AdaptiveBenchmarkDriverResult) {
				result.DeliveryEventID = ""
			},
		},
		{
			name: "static exposure drift",
			mutate: func(result *AdaptiveBenchmarkDriverResult) {
				result.ActualActionID = result.RecommendedActionID
			},
		},
		{
			name: "cost without provenance",
			mutate: func(result *AdaptiveBenchmarkDriverResult) {
				result.CostUSD = benchmarkFloat(0.01)
				result.CostBasis = AdaptiveBenchmarkCostUnknown
			},
		},
		{
			name: "missing cost claims provider provenance",
			mutate: func(result *AdaptiveBenchmarkDriverResult) {
				result.CostBasis = AdaptiveBenchmarkCostProviderReported
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			driver := newAdaptiveBenchmarkDriverFake(
				benchmarkDatasetFixture(t),
			)
			driver.mutate = test.mutate
			run, err := RunAdaptiveBenchmarkScenarios(
				t.Context(),
				benchmarkDatasetFixture(t),
				driver,
			)
			if err != nil {
				t.Fatalf("RunAdaptiveBenchmarkScenarios: %v", err)
			}
			if len(run.Scenarios) != 52 {
				t.Fatalf("scenario count = %d, want 52", len(run.Scenarios))
			}
			invalidCount := 0
			for _, scenario := range run.Scenarios {
				if scenario.Status != AdaptiveBenchmarkStatusInvalid ||
					len(scenario.ReasonCodes) == 0 {
					continue
				}
				invalidCount++
			}
			if invalidCount == 0 {
				t.Fatal("broken result produced no invalid records")
			}
		})
	}
}

func TestRunAdaptiveBenchmarkScenariosContinuesAfterDriverFailure(t *testing.T) {
	t.Parallel()

	driverErr := errors.New("runner turn failed")
	driver := newAdaptiveBenchmarkDriverFake(benchmarkDatasetFixture(t))
	driver.failAt = 3
	driver.err = driverErr
	run, err := RunAdaptiveBenchmarkScenarios(
		t.Context(),
		benchmarkDatasetFixture(t),
		driver,
	)
	if err != nil {
		t.Fatalf("RunAdaptiveBenchmarkScenarios: %v", err)
	}
	if len(driver.subjects) != 52 {
		t.Fatalf("driver calls = %d, want 52", len(driver.subjects))
	}
	failed := run.Scenarios[2]
	if failed.Status != AdaptiveBenchmarkStatusInvalid ||
		!slices.Equal(
			failed.ReasonCodes,
			[]string{AdaptiveBenchmarkReasonDriverFailure},
		) ||
		failed.Quality != nil ||
		failed.QualityOutcomeID != nil {
		t.Fatalf("driver failure record = %#v", failed)
	}
}

func TestRunAdaptiveBenchmarkScenariosAbortsUnsafeDispatch(t *testing.T) {
	t.Parallel()

	driver := newAdaptiveBenchmarkDriverFake(benchmarkDatasetFixture(t))
	driver.failAt = 3
	driver.err = errors.Join(
		errors.New("driver blocked unsafe side effect"),
		ErrAdaptiveBenchmarkUnsafeDispatch,
	)
	run, err := RunAdaptiveBenchmarkScenarios(
		t.Context(),
		benchmarkDatasetFixture(t),
		driver,
	)
	if !errors.Is(err, ErrAdaptiveBenchmarkUnsafeDispatch) {
		t.Fatalf("RunAdaptiveBenchmarkScenarios error = %v", err)
	}
	if len(run.Scenarios) != 0 {
		t.Fatalf("unsafe run returned %d partial records", len(run.Scenarios))
	}
	if len(driver.subjects) != 3 {
		t.Fatalf("scenario calls after unsafe dispatch = %d, want 3", len(driver.subjects))
	}
	if len(driver.evaluations) != 2 {
		t.Fatalf(
			"evaluation calls after unsafe dispatch = %d, want 2",
			len(driver.evaluations),
		)
	}
	if got := driver.events[len(driver.events)-1]; !strings.HasPrefix(
		got,
		"turn:",
	) {
		t.Fatalf("last unsafe event = %q, want turn without outcome", got)
	}
}

func TestRunAdaptiveBenchmarkScenariosRecordsTypedTerminalFailures(
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name       string
		reasonCode string
		wantStatus AdaptiveBenchmarkStatus
	}{
		{
			name:       "transport",
			reasonCode: AdaptiveBenchmarkReasonModelTransportFailure,
			wantStatus: AdaptiveBenchmarkStatusFail,
		},
		{
			name:       "parser",
			reasonCode: AdaptiveBenchmarkReasonModelParserFailure,
			wantStatus: AdaptiveBenchmarkStatusFail,
		},
		{
			name:       "control",
			reasonCode: AdaptiveBenchmarkReasonControlFailed,
			wantStatus: AdaptiveBenchmarkStatusInvalid,
		},
		{
			name:       "linkage",
			reasonCode: AdaptiveBenchmarkReasonFactLinkageInvalid,
			wantStatus: AdaptiveBenchmarkStatusInvalid,
		},
		{
			name:       "outcome",
			reasonCode: AdaptiveBenchmarkReasonOutcomeMissing,
			wantStatus: AdaptiveBenchmarkStatusInvalid,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			driver := newAdaptiveBenchmarkDriverFake(
				benchmarkDatasetFixture(t),
			)
			driver.failAt = 3
			driver.err = errors.New("terminal runner failure")
			driver.mutate = func(result *AdaptiveBenchmarkDriverResult) {
				result.FailureReasonCodes = []string{test.reasonCode}
			}
			run, err := RunAdaptiveBenchmarkScenarios(
				t.Context(),
				benchmarkDatasetFixture(t),
				driver,
			)
			if err != nil {
				t.Fatalf("RunAdaptiveBenchmarkScenarios: %v", err)
			}
			failed := run.Scenarios[2]
			if failed.Status != test.wantStatus ||
				!slices.Equal(
					failed.ReasonCodes,
					[]string{test.reasonCode},
				) ||
				failed.Quality != nil ||
				failed.QualityOutcomeID != nil ||
				failed.LatencyNS != 10 {
				t.Fatalf("typed failure record = %#v", failed)
			}
		})
	}
}

func TestRunAdaptiveBenchmarkScenariosRejectsTerminalFailureWithoutDelivery(
	t *testing.T,
) {
	t.Parallel()

	driver := newAdaptiveBenchmarkDriverFake(benchmarkDatasetFixture(t))
	driver.failAt = 3
	driver.err = errors.New("terminal runner failure")
	driver.mutate = func(result *AdaptiveBenchmarkDriverResult) {
		result.FailureReasonCodes = []string{
			AdaptiveBenchmarkReasonModelTransportFailure,
		}
		result.DeliveryEventID = ""
	}
	run, err := RunAdaptiveBenchmarkScenarios(
		t.Context(),
		benchmarkDatasetFixture(t),
		driver,
	)
	if err != nil {
		t.Fatalf("RunAdaptiveBenchmarkScenarios: %v", err)
	}
	failed := run.Scenarios[2]
	if failed.Status != AdaptiveBenchmarkStatusInvalid ||
		!slices.Equal(
			failed.ReasonCodes,
			[]string{AdaptiveBenchmarkReasonFactLinkageInvalid},
		) ||
		failed.Quality != nil ||
		failed.QualityOutcomeID != nil {
		t.Fatalf("missing-delivery terminal failure = %#v", failed)
	}
}

func TestRunAdaptiveBenchmarkScenariosRecordsOutcomePersistenceFailure(
	t *testing.T,
) {
	t.Parallel()

	driver := newAdaptiveBenchmarkDriverFake(benchmarkDatasetFixture(t))
	driver.recordFailAt = 3
	driver.recordErr = errors.New("outcome store unavailable")
	run, err := RunAdaptiveBenchmarkScenarios(
		t.Context(),
		benchmarkDatasetFixture(t),
		driver,
	)
	if err != nil {
		t.Fatalf("RunAdaptiveBenchmarkScenarios: %v", err)
	}
	if len(driver.subjects) != 52 || len(driver.evaluations) != 52 {
		t.Fatalf(
			"turns/evaluations = %d/%d, want 52/52",
			len(driver.subjects),
			len(driver.evaluations),
		)
	}
	failed := run.Scenarios[2]
	if failed.Status != AdaptiveBenchmarkStatusInvalid ||
		!slices.Equal(
			failed.ReasonCodes,
			[]string{AdaptiveBenchmarkReasonOutcomeMissing},
		) ||
		failed.Quality != nil ||
		failed.QualityOutcomeID != nil {
		t.Fatalf("outcome failure record = %#v", failed)
	}
}

func TestRunAdaptiveBenchmarkScenariosTracksCostProvenance(t *testing.T) {
	t.Parallel()

	priceTable := &AdaptiveBenchmarkPriceTable{
		ID:       "openrouter-usd-2026-07",
		Revision: 7,
		SHA256:   strings.Repeat("7", 64),
	}
	driver := newAdaptiveBenchmarkDriverFake(benchmarkDatasetFixture(t))
	driver.mutate = func(result *AdaptiveBenchmarkDriverResult) {
		result.CostUSD = benchmarkFloat(0.01)
		result.CostBasis = AdaptiveBenchmarkCostPriceTable
		result.PriceTable = priceTable
	}
	run, err := RunAdaptiveBenchmarkScenarios(
		t.Context(),
		benchmarkDatasetFixture(t),
		driver,
	)
	if err != nil {
		t.Fatalf("runAdaptiveBenchmarkScenarios: %v", err)
	}
	if run.PriceTableCostCount != 52 ||
		run.ProviderCostCount != 0 ||
		run.MissingCostCount != 0 {
		t.Fatalf("cost provenance = %#v", run)
	}
	if run.PriceTable == nil || *run.PriceTable != *priceTable {
		t.Fatalf("run price table = %#v, want %#v", run.PriceTable, priceTable)
	}
	inputs := validAdaptiveBenchmarkReport(
		t,
		AdaptiveBenchmarkModeProductionShadow,
	).FrozenInputs
	inputs.PriceTableID = benchmarkString(priceTable.ID)
	inputs.PriceTableRevision = benchmarkInt(priceTable.Revision)
	inputs.PriceTableSHA256 = benchmarkString(priceTable.SHA256)
	if err := ValidateAdaptiveBenchmarkCostProvenance(run, inputs); err != nil {
		t.Fatalf("ValidateAdaptiveBenchmarkCostProvenance: %v", err)
	}
	inputs.PriceTableSHA256 = benchmarkString(strings.Repeat("8", 64))
	if err := ValidateAdaptiveBenchmarkCostProvenance(run, inputs); err == nil {
		t.Fatal("cost provenance accepted a different price-table digest")
	}
}

func TestValidateAdaptiveBenchmarkCostProvenanceRequiresNullTableForProvider(
	t *testing.T,
) {
	t.Parallel()

	driver := newAdaptiveBenchmarkDriverFake(benchmarkDatasetFixture(t))
	driver.mutate = func(result *AdaptiveBenchmarkDriverResult) {
		result.CostUSD = benchmarkFloat(0.01)
		result.CostBasis = AdaptiveBenchmarkCostProviderReported
	}
	run, err := RunAdaptiveBenchmarkScenarios(
		t.Context(),
		benchmarkDatasetFixture(t),
		driver,
	)
	if err != nil {
		t.Fatalf("RunAdaptiveBenchmarkScenarios: %v", err)
	}
	inputs := validAdaptiveBenchmarkReport(
		t,
		AdaptiveBenchmarkModeProductionShadow,
	).FrozenInputs
	if err := ValidateAdaptiveBenchmarkCostProvenance(run, inputs); err != nil {
		t.Fatalf("provider cost provenance: %v", err)
	}
	inputs.PriceTableID = benchmarkString("forbidden")
	inputs.PriceTableRevision = benchmarkInt(1)
	inputs.PriceTableSHA256 = benchmarkString(strings.Repeat("9", 64))
	if err := ValidateAdaptiveBenchmarkCostProvenance(run, inputs); err == nil {
		t.Fatal("provider-reported cost accepted price-table metadata")
	}
}

func TestValidateAdaptiveBenchmarkCostProvenanceAllowsMixedBases(
	t *testing.T,
) {
	t.Parallel()

	priceTable := &AdaptiveBenchmarkPriceTable{
		ID:       "openrouter-usd-2026-07",
		Revision: 7,
		SHA256:   strings.Repeat("7", 64),
	}
	calls := 0
	driver := newAdaptiveBenchmarkDriverFake(benchmarkDatasetFixture(t))
	driver.mutate = func(result *AdaptiveBenchmarkDriverResult) {
		result.CostUSD = benchmarkFloat(0.01)
		calls++
		if calls%2 == 0 {
			result.CostBasis = AdaptiveBenchmarkCostProviderReported
			return
		}
		result.CostBasis = AdaptiveBenchmarkCostPriceTable
		result.PriceTable = priceTable
	}
	run, err := RunAdaptiveBenchmarkScenarios(
		t.Context(),
		benchmarkDatasetFixture(t),
		driver,
	)
	if err != nil {
		t.Fatalf("RunAdaptiveBenchmarkScenarios: %v", err)
	}
	if run.ProviderCostCount != 26 ||
		run.PriceTableCostCount != 26 ||
		run.MissingCostCount != 0 ||
		run.PriceTable == nil ||
		*run.PriceTable != *priceTable {
		t.Fatalf("mixed cost provenance = %#v", run)
	}
	inputs := validAdaptiveBenchmarkReport(
		t,
		AdaptiveBenchmarkModeProductionShadow,
	).FrozenInputs
	inputs.PriceTableID = benchmarkString(priceTable.ID)
	inputs.PriceTableRevision = benchmarkInt(priceTable.Revision)
	inputs.PriceTableSHA256 = benchmarkString(priceTable.SHA256)
	if err := ValidateAdaptiveBenchmarkCostProvenance(run, inputs); err != nil {
		t.Fatalf("mixed cost provenance: %v", err)
	}
}
