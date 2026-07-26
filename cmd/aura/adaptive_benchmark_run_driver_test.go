package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/google/uuid"
)

func TestAdaptiveBenchmarkAuraDriverRunsOnlyTheSubjectThroughAura(
	t *testing.T,
) {
	driver, runner, _, outcomes, assignment, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	subject := auraeval.AdaptiveBenchmarkSubject{
		ScenarioID: "r01", Domain: adaptive.DomainReasoning,
		Prompt: "Return only the integer: 18 multiplied by 23.",
	}

	result, err := driver.RunScenario(t.Context(), subject)
	if err != nil {
		t.Fatalf("RunScenario: %v", err)
	}
	var subjectWire map[string]json.RawMessage
	if err := json.Unmarshal([]byte(runner.turnMessage), &subjectWire); err != nil {
		t.Fatalf("subject input is not JSON: %v", err)
	}
	if len(subjectWire) != 3 ||
		subjectWire["scenario_id"] == nil ||
		subjectWire["domain"] == nil ||
		subjectWire["prompt"] == nil ||
		subjectWire["expected"] != nil ||
		subjectWire["evaluator_id"] != nil {
		t.Fatalf("subject crossed the gold boundary: %s", runner.turnMessage)
	}
	if runner.createOwnerID != assignment.OwnerID.String() ||
		runner.turnOwnerID != assignment.OwnerID.String() ||
		runner.deleteOwnerID != assignment.OwnerID.String() ||
		runner.createdID == "" ||
		runner.deletedID != runner.createdID {
		t.Fatalf("conversation lifecycle was not owner scoped: %#v", runner)
	}
	conversationID, parseErr := uuid.Parse(runner.createdID)
	if parseErr != nil || conversationID.Version() != 7 {
		t.Fatalf("conversation ID = %q, want UUIDv7", runner.createdID)
	}
	if result.RequestID != assignment.RequestID.String() ||
		result.AssignmentID != assignment.AssignmentID.String() ||
		result.ChampionActionID != adaptive.StaticActionID ||
		result.RecommendedActionID != "candidate" ||
		result.IntendedActionID != adaptive.StaticActionID ||
		result.ActualActionID != adaptive.StaticActionID ||
		result.LatencyNS != int64(25*time.Millisecond) ||
		result.PromptTokens != 11 ||
		result.CompletionTokens != 7 ||
		result.TotalTokens != 18 ||
		result.CostUSD == nil ||
		*result.CostUSD != 0.0125 ||
		result.CostBasis != auraeval.AdaptiveBenchmarkCostProviderReported ||
		result.Observed.Answer != "414" {
		t.Fatalf("driver result = %#v", result)
	}
	if len(result.ActionProbabilities) != 2 ||
		result.ActionProbabilities[0].ActionID != "candidate" ||
		result.ActionProbabilities[0].Probability != 0 ||
		result.ActionProbabilities[1].ActionID != adaptive.StaticActionID ||
		result.ActionProbabilities[1].Probability != 1 {
		t.Fatalf("action probabilities = %#v", result.ActionProbabilities)
	}
	if len(outcomes.observations) != 0 {
		t.Fatal("RunScenario evaluated before the core gold checker")
	}
}

func TestAdaptiveBenchmarkAuraDriverRecordsIndependentOutcomeAfterTurn(
	t *testing.T,
) {
	driver, _, ledger, outcomes, assignment, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	subject := auraeval.AdaptiveBenchmarkSubject{
		ScenarioID: "r01", Domain: adaptive.DomainReasoning,
		Prompt: "Return only the integer: 18 multiplied by 23.",
	}
	run, err := driver.RunScenario(t.Context(), subject)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := driver.RecordEvaluation(
		t.Context(),
		auraeval.AdaptiveBenchmarkEvaluationRecord{
			ScenarioID: "r01", RequestID: run.RequestID,
			AssignmentID:    run.AssignmentID,
			DeliveryEventID: run.DeliveryEventID,
			Evaluation: auraeval.AdaptiveBenchmarkEvaluation{
				Passed: true, ReasonCodes: []string{},
			},
		},
	)
	if err != nil {
		t.Fatalf("RecordEvaluation: %v", err)
	}
	if len(outcomes.observations) != 1 {
		t.Fatalf("outcome writes = %d, want 1", len(outcomes.observations))
	}
	observation := outcomes.observations[0]
	if observation.AssignmentID != assignment.AssignmentID ||
		observation.OwnerID != assignment.OwnerID ||
		observation.Domain != assignment.Domain ||
		observation.ProviderID != assignment.ProviderID ||
		observation.ModelID != assignment.ModelID ||
		observation.Status != adaptive.OutcomeObserved ||
		observation.Measures.Quality == nil ||
		*observation.Measures.Quality != 1 ||
		observation.Measures.Harm == nil ||
		*observation.Measures.Harm != 0 {
		t.Fatalf("persisted outcome = %#v", observation)
	}
	if recorded.QualityOutcomeID == "" ||
		recorded.Quality == nil ||
		recorded.Quality.Value == nil ||
		*recorded.Quality.Value != 1 ||
		!recorded.Quality.Eligible ||
		recorded.Quality.EvaluatorID !=
			auraeval.AdaptiveBenchmarkEvaluatorID ||
		recorded.Quality.EvaluatorRevision !=
			auraeval.AdaptiveBenchmarkEvaluatorRevision {
		t.Fatalf("recorded evaluation = %#v", recorded)
	}
	outcomeID := uuid.MustParse(recorded.QualityOutcomeID)
	found := false
	for _, fact := range ledger.records {
		if fact.ID == outcomeID {
			found = fact.Kind == adaptive.EventOutcome &&
				fact.DecisionID == assignment.AssignmentID
		}
	}
	if !found {
		t.Fatal("quality outcome was not reread from the schema-2 aggregate")
	}
}

func TestAdaptiveBenchmarkAuraDriverUsesTypedDeliveryObservations(
	t *testing.T,
) {
	toolResults, toolRevisions, err := adaptive.NewRegisteredCatalogDeliveryMetadata(
		adaptive.ResultTool,
		[]string{"weather_now", "web_search"},
		[]string{"weather_now"},
		"tools-v1",
	)
	if err != nil {
		t.Fatal(err)
	}
	skillResults, skillRevisions, err := adaptive.NewRegisteredCatalogDeliveryMetadata(
		adaptive.ResultSkill,
		[]string{"golang-testing", "web-research"},
		[]string{"golang-testing"},
		"skills-v1",
	)
	if err != nil {
		t.Fatal(err)
	}
	knowledgeID := uuid.Must(uuid.NewV7())
	knowledgeResults, knowledgeRevisions, err :=
		adaptive.NewRetrievalDeliveryMetadata(
			[]string{knowledgeID.String()},
			[]string{knowledgeID.String()},
			adaptive.RetrievalRevisionValues{
				CorpusEpoch: 1, Parser: "parser-v1",
				Chunker: "chunker-v1", Embedding: "embedding-v1",
				Index: "index-v1", Reranker: "reranker-v1",
				Retriever: "retriever-v1",
			},
		)
	if err != nil {
		t.Fatal(err)
	}
	memoryID := uuid.Must(uuid.NewV7())
	memoryResults, memoryRevisions, err := adaptive.NewMemoryRecallDeliveryMetadata(
		[]adaptive.MemoryRecallResult{{
			Kind: adaptive.ResultMemoryPreference,
			ID:   memoryID.String(),
		}},
		adaptive.MemoryRecallRevisionValues{
			CorpusEpoch: 1, Retriever: "retriever-v1",
			Reranker: "reranker-v1", Embedding: "embedding-v1",
			Index: "index-v1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		domain    adaptive.Domain
		results   []adaptive.ResultID
		aliases   map[uuid.UUID]string
		wantTool  string
		wantSkill string
		wantIDs   []string
	}{
		{
			name: "tool", domain: adaptive.DomainToolDiscovery,
			results: toolResults, wantTool: "weather_now",
		},
		{
			name: "skill", domain: adaptive.DomainSkillRouting,
			results: skillResults, wantSkill: "golang-testing",
		},
		{
			name: "knowledge", domain: adaptive.DomainKnowledge,
			results: knowledgeResults,
			aliases: map[uuid.UUID]string{
				knowledgeID: "document:project-helios",
			},
			wantIDs: []string{"document:project-helios"},
		},
		{
			name: "memory", domain: adaptive.DomainMemoryRecall,
			results: memoryResults,
			aliases: map[uuid.UUID]string{
				memoryID: "preference:temperature-unit",
			},
			wantIDs: []string{"preference:temperature-unit"},
		},
	}
	_ = toolRevisions
	_ = skillRevisions
	_ = knowledgeRevisions
	_ = memoryRevisions
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			delivery := adaptive.Delivery{ResultIDs: test.results}
			observed, err := adaptiveBenchmarkObservedFromDelivery(
				test.domain,
				"terminal answer",
				delivery,
				adaptiveBenchmarkFixtureAliasResolverFake(test.aliases),
			)
			if err != nil {
				t.Fatalf("adaptiveBenchmarkObservedFromDelivery: %v", err)
			}
			if observed.ToolID != test.wantTool ||
				observed.SkillID != test.wantSkill ||
				!slices.Equal(observed.ResultIDs, test.wantIDs) {
				t.Fatalf("observed = %#v", observed)
			}
		})
	}
}

func TestAdaptiveBenchmarkAuraDriverRejectsUnknownMemoryFixtureID(
	t *testing.T,
) {
	driver, runner, _, _, assignment, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainMemoryRecall)
	memoryID := uuid.Must(uuid.NewV7())
	results, revisions, err := adaptive.NewMemoryRecallDeliveryMetadata(
		[]adaptive.MemoryRecallResult{{
			Kind: adaptive.ResultMemoryEntity,
			ID:   memoryID.String(),
		}},
		adaptive.MemoryRecallRevisionValues{
			CorpusEpoch: 1, Retriever: "retriever-v1",
			Reranker: "reranker-v1", Embedding: "embedding-v1",
			Index: "index-v1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	delivery := adaptiveBenchmarkTestDelivery(t, assignment)
	delivery.ResultIDs = results
	delivery.ResultCount = len(results)
	delivery.Revisions = revisions
	delivery.EffectiveLimits = map[string]int{
		"entity_requested_k": 1,
		"entity_effective_k": 1,
		"entity_count":       1,
	}
	var setupErr error
	runner.beforeTurn = func(ctx context.Context) error {
		if _, err := driver.recorder.RecordAssignment(
			ctx,
			assignment,
		); err != nil {
			setupErr = err
			return err
		}
		_, setupErr = driver.recorder.RecordDelivery(
			ctx,
			assignment.OwnerID,
			assignment.AssignmentID,
			delivery,
		)
		return setupErr
	}

	result, err := driver.RunScenario(
		t.Context(),
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "m01", Domain: adaptive.DomainMemoryRecall,
			Prompt: "recall benchmark memories",
		},
	)
	if err == nil ||
		!errors.Is(err, errAdaptiveBenchmarkScenarioFailure) ||
		!slices.Equal(
			result.FailureReasonCodes,
			[]string{auraeval.AdaptiveBenchmarkReasonFactLinkageInvalid},
		) {
		t.Fatalf(
			"unknown fixture result=%#v setup=%v error=%v",
			result,
			setupErr,
			err,
		)
	}
}

func TestAdaptiveBenchmarkAuraDriverClassifiesMissingPersistedFacts(
	t *testing.T,
) {
	tests := []struct {
		name       string
		beforeTurn func(
			context.Context,
			*adaptiveBenchmarkAuraDriver,
			adaptive.Assignment,
		) error
		wantReason string
	}{
		{
			name: "assignment",
			beforeTurn: func(
				context.Context,
				*adaptiveBenchmarkAuraDriver,
				adaptive.Assignment,
			) error {
				return nil
			},
			wantReason: auraeval.AdaptiveBenchmarkReasonAssignmentMissing,
		},
		{
			name: "delivery",
			beforeTurn: func(
				ctx context.Context,
				driver *adaptiveBenchmarkAuraDriver,
				assignment adaptive.Assignment,
			) error {
				_, err := driver.recorder.RecordAssignment(ctx, assignment)
				return err
			},
			wantReason: auraeval.AdaptiveBenchmarkReasonDeliveryMissing,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			driver, runner, _, _, assignment, _ :=
				adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
			runner.beforeTurn = func(ctx context.Context) error {
				return test.beforeTurn(ctx, driver, assignment)
			}

			result, err := driver.RunScenario(
				t.Context(),
				auraeval.AdaptiveBenchmarkSubject{
					ScenarioID: "r01", Domain: adaptive.DomainReasoning,
					Prompt: "prompt",
				},
			)
			if err == nil ||
				!slices.Equal(
					result.FailureReasonCodes,
					[]string{test.wantReason},
				) {
				t.Fatalf("missing fact result=%#v error=%v", result, err)
			}
		})
	}
}

func TestAdaptiveBenchmarkAuraDriverBoundsTurnFailuresAndAlwaysCleansUp(
	t *testing.T,
) {
	tests := []struct {
		name       string
		item       adaptiveBenchmarkTurnItem
		wantReason string
	}{
		{
			name: "transport",
			item: adaptiveBenchmarkTurnItem{
				err: &openai_compat.HTTPError{StatusCode: 502},
			},
			wantReason: auraeval.AdaptiveBenchmarkReasonModelTransportFailure,
		},
		{
			name: "parser",
			item: adaptiveBenchmarkTurnItem{
				err: &json.SyntaxError{Offset: 1},
			},
			wantReason: auraeval.AdaptiveBenchmarkReasonModelParserFailure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			driver, runner, _, _, _, _ :=
				adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
			runner.items = []adaptiveBenchmarkTurnItem{test.item}

			result, err := driver.RunScenario(
				t.Context(),
				auraeval.AdaptiveBenchmarkSubject{
					ScenarioID: "r01",
					Domain:     adaptive.DomainReasoning,
					Prompt:     "prompt",
				},
			)
			if err == nil ||
				!slices.Equal(
					result.FailureReasonCodes,
					[]string{test.wantReason},
				) {
				t.Fatalf("failure result=%#v error=%v", result, err)
			}
			if runner.deletedID == "" {
				t.Fatal("failed turn did not clean up its conversation")
			}
		})
	}
}

func TestAdaptiveBenchmarkAuraDriverBoundsCleanupCardinalityFailure(
	t *testing.T,
) {
	driver, runner, _, _, _, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	runner.deleteAffected = 0

	result, err := driver.RunScenario(
		t.Context(),
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "r01", Domain: adaptive.DomainReasoning,
			Prompt: "prompt",
		},
	)
	if err == nil ||
		!errors.Is(err, errAdaptiveBenchmarkScenarioFailure) ||
		!slices.Equal(
			result.FailureReasonCodes,
			[]string{auraeval.AdaptiveBenchmarkReasonDriverFailure},
		) {
		t.Fatalf("cleanup failure result=%#v error=%v", result, err)
	}
}

func TestAdaptiveBenchmarkAuraDriverCleansUpAfterConversationCreateFailure(
	t *testing.T,
) {
	driver, runner, _, _, _, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	runner.createErr = errors.New("partially persisted conversation")
	runner.deleteAffected = 0

	result, err := driver.RunScenario(
		t.Context(),
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "r01", Domain: adaptive.DomainReasoning,
			Prompt: "prompt",
		},
	)
	if err == nil ||
		!errors.Is(err, errAdaptiveBenchmarkScenarioFailure) ||
		runner.deletedID == "" ||
		runner.deletedID != runner.createdID ||
		!slices.Equal(
			result.FailureReasonCodes,
			[]string{auraeval.AdaptiveBenchmarkReasonDriverFailure},
		) {
		t.Fatalf("create failure result=%#v runner=%#v error=%v", result, runner, err)
	}
}

func TestAdaptiveBenchmarkAuraDriverPreservesCancellationAndCleanupFailure(
	t *testing.T,
) {
	driver, runner, _, _, _, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	cleanupErr := errors.New("cleanup failed")
	runner.items = []adaptiveBenchmarkTurnItem{{err: context.Canceled}}
	runner.deleteErr = cleanupErr
	runner.deleteAffected = 0
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := driver.RunScenario(
		ctx,
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "r01", Domain: adaptive.DomainReasoning,
			Prompt: "prompt",
		},
	)
	if !errors.Is(err, context.Canceled) ||
		!errors.Is(err, cleanupErr) {
		t.Fatalf("joined cancellation/cleanup error = %v", err)
	}
}

func TestAdaptiveBenchmarkAuraDriverRejectsMalformedTerminalUsage(
	t *testing.T,
) {
	driver, runner, _, _, assignment, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	terminal := adaptiveBenchmarkTerminalEvent(assignment.RequestID, "answer")
	terminal.Actions.StateDelta["cost_usd"] = math.NaN()
	runner.items = []adaptiveBenchmarkTurnItem{{event: terminal}}

	result, err := driver.RunScenario(
		t.Context(),
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "r01", Domain: adaptive.DomainReasoning,
			Prompt: "prompt",
		},
	)
	if err == nil ||
		!slices.Equal(
			result.FailureReasonCodes,
			[]string{auraeval.AdaptiveBenchmarkReasonModelParserFailure},
		) {
		t.Fatalf("malformed usage result=%#v error=%v", result, err)
	}
}

func TestAdaptiveBenchmarkAuraDriverRejectsUnsafeDispatch(
	t *testing.T,
) {
	driver, runner, _, _, assignment, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	runner.items = []adaptiveBenchmarkTurnItem{
		{
			event: &agent.Event{
				RequestID: assignment.RequestID,
				Actions: agent.Actions{
					ToolInvocation: &agent.ToolInvocation{
						Event:    agent.ToolInvocationStart,
						ToolName: "shell_exec",
					},
				},
			},
		},
	}

	_, err := driver.RunScenario(
		t.Context(),
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "r01", Domain: adaptive.DomainReasoning,
			Prompt: "prompt",
		},
	)
	if !errors.Is(err, auraeval.ErrAdaptiveBenchmarkUnsafeDispatch) {
		t.Fatalf("unsafe dispatch error = %v", err)
	}
}
