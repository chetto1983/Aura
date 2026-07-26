package eval

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
)

type adaptiveBenchmarkDriverFake struct {
	gold            map[string]AdaptiveBenchmarkScenario
	subjects        []AdaptiveBenchmarkSubject
	inFlight        int
	maximumInFlight int
	failAt          int
	err             error
	mutate          func(*AdaptiveBenchmarkDriverResult)
	evaluations     []AdaptiveBenchmarkEvaluationRecord
	events          []string
	recordFailAt    int
	recordErr       error
	recordMutate    func(*AdaptiveBenchmarkRecordedEvaluation)
}

func (driver *adaptiveBenchmarkDriverFake) RunScenario(
	_ context.Context,
	subject AdaptiveBenchmarkSubject,
) (AdaptiveBenchmarkDriverResult, error) {
	driver.inFlight++
	driver.maximumInFlight = max(driver.maximumInFlight, driver.inFlight)
	defer func() {
		driver.inFlight--
	}()
	driver.subjects = append(driver.subjects, subject)
	driver.events = append(driver.events, "turn:"+subject.ScenarioID)
	scenario, ok := driver.gold[subject.ScenarioID]
	if !ok {
		return AdaptiveBenchmarkDriverResult{}, errors.New("unknown scenario")
	}
	result := validAdaptiveBenchmarkDriverResult(scenario)
	if driver.mutate != nil {
		driver.mutate(&result)
	}
	if driver.failAt > 0 && len(driver.subjects) == driver.failAt {
		return result, driver.err
	}
	return result, nil
}

func (driver *adaptiveBenchmarkDriverFake) RecordEvaluation(
	_ context.Context,
	record AdaptiveBenchmarkEvaluationRecord,
) (AdaptiveBenchmarkRecordedEvaluation, error) {
	driver.evaluations = append(driver.evaluations, record)
	driver.events = append(driver.events, "outcome:"+record.ScenarioID)
	result := recordedAdaptiveBenchmarkEvaluation(record)
	if driver.recordMutate != nil {
		driver.recordMutate(&result)
	}
	if driver.recordFailAt == len(driver.evaluations) {
		return AdaptiveBenchmarkRecordedEvaluation{}, driver.recordErr
	}
	return result, nil
}

func recordedAdaptiveBenchmarkEvaluation(
	record AdaptiveBenchmarkEvaluationRecord,
) AdaptiveBenchmarkRecordedEvaluation {
	value := float64(0)
	if record.Evaluation.Passed {
		value = 1
	}
	return AdaptiveBenchmarkRecordedEvaluation{
		QualityOutcomeID: benchmarkUUID("quality-" + record.ScenarioID),
		Quality: &AdaptiveBenchmarkObservation{
			EvaluatorID:       AdaptiveBenchmarkEvaluatorID,
			EvaluatorRevision: AdaptiveBenchmarkEvaluatorRevision,
			Value:             benchmarkFloat(value),
			Eligible:          true,
			ReasonCodes:       slices.Clone(record.Evaluation.ReasonCodes),
		},
	}
}

func newAdaptiveBenchmarkDriverFake(
	dataset AdaptiveBenchmarkDataset,
) *adaptiveBenchmarkDriverFake {
	gold := make(map[string]AdaptiveBenchmarkScenario, len(dataset.Scenarios))
	for _, scenario := range dataset.Scenarios {
		gold[scenario.ScenarioID] = scenario
	}
	return &adaptiveBenchmarkDriverFake{gold: gold}
}

func validAdaptiveBenchmarkDriverResult(
	scenario AdaptiveBenchmarkScenario,
) AdaptiveBenchmarkDriverResult {
	catalog := benchmarkCatalogForDomain(scenario.Domain)
	probabilities := make(
		[]AdaptiveBenchmarkActionProbability,
		len(catalog),
	)
	for index, actionID := range catalog {
		probability := float64(0)
		if actionID == adaptive.StaticActionID {
			probability = 1
		}
		probabilities[index] = AdaptiveBenchmarkActionProbability{
			ActionID: actionID, Probability: probability,
		}
	}
	observed := AdaptiveBenchmarkObserved{}
	switch scenario.Domain {
	case adaptive.DomainReasoning, adaptive.DomainKnowledge:
		observed.Answer = scenario.Expected
	case adaptive.DomainToolDiscovery:
		observed.ToolID = scenario.Expected
	case adaptive.DomainSkillRouting:
		observed.SkillID = scenario.Expected
	case adaptive.DomainMemoryRecall:
		observed.ResultIDs = strings.Split(scenario.Expected, ",")
	}
	return AdaptiveBenchmarkDriverResult{
		RequestID: benchmarkUUID("request-" + scenario.ScenarioID),
		AssignmentID: benchmarkUUID(
			"assignment-" + scenario.ScenarioID,
		),
		DeliveryEventID: benchmarkUUID(
			"delivery-" + scenario.ScenarioID,
		),
		ChampionActionID:    adaptive.StaticActionID,
		RecommendedActionID: catalog[0],
		IntendedActionID:    adaptive.StaticActionID,
		ActualActionID:      adaptive.StaticActionID,
		ActionProbabilities: probabilities,
		PromptTokens:        1, CompletionTokens: 1, TotalTokens: 2,
		LatencyNS: 10, CostBasis: AdaptiveBenchmarkCostUnknown,
		Observed: observed,
	}
}

func benchmarkDatasetFixture(t testing.TB) AdaptiveBenchmarkDataset {
	t.Helper()
	payload, err := os.ReadFile(
		"../../.planning/spikes/106-adaptive-aura-portability/dataset.json",
	)
	if err != nil {
		t.Fatalf("read dataset: %v", err)
	}
	dataset, err := LoadAdaptiveBenchmarkDataset(payload)
	if err != nil {
		t.Fatalf("LoadAdaptiveBenchmarkDataset: %v", err)
	}
	return dataset
}

type goldIsolationDriver struct {
	scenarios map[string]AdaptiveBenchmarkScenario
	subjects  []AdaptiveBenchmarkSubject
}

func (driver *goldIsolationDriver) RunScenario(
	_ context.Context,
	subject AdaptiveBenchmarkSubject,
) (AdaptiveBenchmarkDriverResult, error) {
	driver.subjects = append(driver.subjects, subject)
	scenario, ok := driver.scenarios[subject.ScenarioID]
	if !ok {
		return AdaptiveBenchmarkDriverResult{}, errors.New("unknown scenario")
	}
	return validAdaptiveBenchmarkDriverResult(scenario), nil
}

func (driver *goldIsolationDriver) RecordEvaluation(
	_ context.Context,
	record AdaptiveBenchmarkEvaluationRecord,
) (AdaptiveBenchmarkRecordedEvaluation, error) {
	return recordedAdaptiveBenchmarkEvaluation(record), nil
}

var _ AdaptiveBenchmarkDriver = (*adaptiveBenchmarkDriverFake)(nil)
