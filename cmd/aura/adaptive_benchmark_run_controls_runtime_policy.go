package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/adaptive/reasoningcontrol"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/prompt"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

type adaptiveBenchmarkControlPolicyReader struct {
	policy adaptive.Policy
}

type adaptiveBenchmarkMemoryMismatchControl struct {
	delegate runner.DynamicRecallControl
	provider runner.DynamicRecallProvider

	providerCalls atomic.Int64
	mu            sync.Mutex
	outcomes      []agent.DynamicTailOutcome
}

func (control *adaptiveBenchmarkMemoryMismatchControl) PrepareDynamicRecall(
	ctx context.Context,
	input runner.DynamicRecallInput,
	_ runner.DynamicRecallProvider,
) (*runner.PreparedDynamicRecall, error) {
	if control == nil || control.delegate == nil || control.provider == nil {
		return nil, adaptiveBenchmarkControlError(
			"adaptive benchmark memory mismatch control is unavailable",
		)
	}
	prepared, err := control.delegate.PrepareDynamicRecall(
		ctx,
		input,
		control.provider,
	)
	if err != nil || prepared == nil || prepared.Commit == nil {
		return prepared, err
	}
	recall, err := control.provider(
		ctx,
		input.OwnerID.String(),
		input.Query,
		4,
	)
	if err != nil {
		return nil, err
	}
	delegateCommit := prepared.Commit
	return &runner.PreparedDynamicRecall{
		Action: runner.DynamicRecallTop4,
		Recall: recall,
		Commit: func(
			ctx context.Context,
			outcome agent.DynamicTailOutcome,
		) error {
			control.mu.Lock()
			control.outcomes = append(control.outcomes, outcome)
			control.mu.Unlock()
			return delegateCommit(ctx, outcome)
		},
	}, nil
}

func (control *adaptiveBenchmarkMemoryMismatchControl) outcome() (
	agent.DynamicTailOutcome,
	bool,
) {
	control.mu.Lock()
	defer control.mu.Unlock()
	if len(control.outcomes) != 1 {
		return agent.DynamicTailOutcome{}, false
	}
	return control.outcomes[0], true
}

func (reader adaptiveBenchmarkControlPolicyReader) CurrentPolicy(
	context.Context,
) (adaptive.Policy, error) {
	policy := reader.policy
	policy.Config = append([]byte(nil), policy.Config...)
	return policy, nil
}

func (runtime *adaptiveBenchmarkControlRuntime) runRollbackStatic(
	ctx context.Context,
	controlCase auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		controlCase,
		"rollback_static",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	subject, err := runtime.primarySubject()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	reader := runtime.environment.bundle.PolicyReader(
		subject.ownerID,
		adaptive.DomainReasoning,
	)
	policy, err := reader.CurrentPolicy(ctx)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	policy.Mode = adaptive.PolicyRollback
	policy.RolloutBPS = 0
	rollback := reasoningcontrol.NewOffline(
		adaptiveBenchmarkControlPolicyReader{policy: policy},
		runtime.environment.bundle,
		subject.recorder,
		subject.providerID,
		subject.modelID,
	)
	if rollback == nil {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark rollback control is unavailable",
			)
	}
	var factCalls atomic.Int64
	handle, err := subject.recorder.InstallProbe(
		adaptiveBenchmarkRecorderProbe{
			BeforeWrite: func(
				context.Context,
				adaptiveBenchmarkRecorderProbeFact,
			) error {
				factCalls.Add(1)
				return nil
			},
			AfterCommit: func(
				context.Context,
				adaptiveBenchmarkRecorderProbeFact,
			) error {
				return nil
			},
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	defer func() { _ = handle.Clear() }()
	requestID, err := uuid.NewV7()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	static := agent.PreparedReasoningRequest{
		Request: llm.Request{
			Model: subject.modelID,
			Messages: []llm.Message{{
				Role: llm.RoleUser, Content: "rollback-static-control",
			}},
			MaxTokens: 17,
		},
	}
	var learnedCalls atomic.Int64
	var staticCalls atomic.Int64
	prepared, err := rollback.DecideAndDeliverReasoning(
		ctx,
		agent.ReasoningControlInput{
			OwnerID: subject.ownerID, RequestID: requestID,
			PointOrdinal: 0, MessageCount: 1,
			ProviderID:    subject.providerID,
			BaseURL:       "http://127.0.0.1:18081/v1",
			ModelID:       subject.modelID,
			StaticTier:    prompt.ReasoningTierNone,
			StaticTierSet: true,
		},
		func(
			context.Context,
			agent.ReasoningAction,
		) (agent.PreparedReasoningRequest, error) {
			learnedCalls.Add(1)
			return agent.PreparedReasoningRequest{},
				errors.New("learned rollback path executed")
		},
		func(context.Context) (agent.PreparedReasoningRequest, error) {
			staticCalls.Add(1)
			return static, nil
		},
	)
	if err != nil ||
		!reflect.DeepEqual(prepared, static) ||
		learnedCalls.Load() != 0 ||
		staticCalls.Load() != 1 ||
		factCalls.Load() != 0 {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark rollback did not preserve static execution",
			)
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs: []uuid.UUID{requestID, runtime.runID},
	}, nil
}

func (runtime *adaptiveBenchmarkControlRuntime) runMemoryEpochMismatch(
	ctx context.Context,
	controlCase auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		controlCase,
		"memory_epoch_mismatch",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	resultID, err := uuid.NewV7()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	const sentinel = "AURA-MEMORY-EPOCH-MISMATCH-SENTINEL"
	before := uint64(41)
	after := uint64(42)
	var mismatch *adaptiveBenchmarkMemoryMismatchControl
	fork, err := runtime.environment.ForkControl(
		ctx,
		func(
			delegate runner.DynamicRecallControl,
		) runner.DynamicRecallControl {
			mismatch = &adaptiveBenchmarkMemoryMismatchControl{
				delegate: delegate,
			}
			mismatch.provider = func(
				_ context.Context,
				ownerID string,
				query string,
				maximum int,
			) (runner.DynamicRecall, error) {
				mismatch.providerCalls.Add(1)
				if ownerID != runtime.request.OwnerID.String() ||
					strings.TrimSpace(query) == "" ||
					maximum != 4 {
					return runner.DynamicRecall{},
						adaptiveBenchmarkControlError(
							"adaptive benchmark memory mismatch provider scope drifted",
						)
				}
				return runner.DynamicRecall{
					Text: runner.FenceDynamicRecall(sentinel),
					Results: []runner.DynamicRecallResult{{
						Kind: "memory_preference",
						ID:   resultID.String(),
					}},
					Limits: map[string]runner.DynamicRecallLimit{
						"memory_preference": {
							RequestedK: 4,
							EffectiveK: 4,
							Count:      1,
						},
						"memory_entity": {
							RequestedK: 4,
							EffectiveK: 4,
						},
					},
					Revisions: runner.DynamicRecallRevisions{
						Retriever: "benchmark-retriever/v1",
						Reranker:  "benchmark-reranker/v1",
						Embedding: "benchmark-embedding/v1",
						Index:     "benchmark-index/v1",
					},
					CorpusEpochBefore: &before,
					CorpusEpochAfter:  &after,
					Coherent:          true,
				}, nil
			}
			return mismatch
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	defer func() { _ = fork.Close() }()
	subject, err := adaptiveBenchmarkControlSubjectFromEnvironment(
		fork,
		runtime.request.OwnerID,
		runtime.runID,
		true,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	scenario, err := adaptiveBenchmarkControlScenario("m01")
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	message, err := adaptiveBenchmarkSubjectJSON(
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: scenario.ScenarioID,
			Domain:     scenario.Domain,
			Prompt:     scenario.Prompt,
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	conversationID, err := adaptiveBenchmarkControlCreateConversation(
		ctx,
		subject,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	turn, turnErr := adaptiveBenchmarkControlRunTurn(
		ctx,
		subject,
		conversationID,
		message,
	)
	deleteErr := adaptiveBenchmarkControlDeleteConversation(
		ctx,
		subject,
		conversationID,
	)
	if turnErr != nil || deleteErr != nil {
		return adaptiveBenchmarkControlExecution{},
			errors.Join(turnErr, deleteErr)
	}
	facts, err := subject.recorder.PersistedFacts(
		ctx,
		subject.ownerID,
		turn.RequestID,
		adaptive.DomainMemoryRecall,
	)
	outcome, outcomeOK := mismatch.outcome()
	if err != nil ||
		mismatch.providerCalls.Load() != 1 ||
		!outcomeOK ||
		outcome.Delivered ||
		outcome.FallbackReason != agent.DynamicTailFallbackInvalid ||
		facts.Assignment.IntendedActionID != adaptive.StaticActionID ||
		facts.Delivery.ActualActionID != adaptive.StaticActionID ||
		len(facts.Delivery.ResultIDs) != 0 ||
		turn.Terminal == nil ||
		turn.Terminal.LLMResponse == nil ||
		strings.Contains(turn.Terminal.LLMResponse.Content, sentinel) {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark memory mismatch exposed learned recall",
			)
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs: []uuid.UUID{
			turn.RequestID,
			facts.Assignment.AssignmentID,
			facts.DeliveryEvent.ID,
		},
		ObservedFailureReasonCodes: adaptiveBenchmarkExpectedControlFaults(controlCase.ControlID),
	}, nil
}

var _ adaptive.PolicyReader = adaptiveBenchmarkControlPolicyReader{}
var _ runner.DynamicRecallControl = (*adaptiveBenchmarkMemoryMismatchControl)(nil)
