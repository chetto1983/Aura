package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/conversations"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

type adaptiveBenchmarkControlConversationReader interface {
	GetForIdentity(
		context.Context,
		string,
		string,
	) (conversations.Conversation, error)
}

type adaptiveBenchmarkControlSubject struct {
	runner        adaptiveBenchmarkTurnRunner
	conversations adaptiveBenchmarkControlConversationReader
	recorder      *adaptiveBenchmarkTeeRecorder
	ledger        adaptiveBenchmarkLedger
	outcomes      adaptiveBenchmarkOutcomeWriter
	catalog       adaptive.EvaluatorCatalog
	observed      *adaptiveBenchmarkObservedClient
	memoryRecall  runner.DynamicRecallControl
	fixtures      adaptiveBenchmarkFixtureAliasResolver
	ownerID       uuid.UUID
	runID         uuid.UUID
	providerID    string
	modelID       string
}

type adaptiveBenchmarkControlTurn struct {
	RequestID      uuid.UUID
	ConversationID uuid.UUID
	Terminal       *agent.Event
}

func (runtime *adaptiveBenchmarkControlRuntime) primarySubject() (
	adaptiveBenchmarkControlSubject,
	error,
) {
	if runtime == nil || runtime.environment == nil {
		return adaptiveBenchmarkControlSubject{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark primary runtime is unavailable",
			)
	}
	return adaptiveBenchmarkControlSubjectFromEnvironment(
		runtime.environment,
		runtime.request.OwnerID,
		runtime.runID,
		false,
	)
}

func (runtime *adaptiveBenchmarkControlRuntime) controlSubject() (
	adaptiveBenchmarkControlSubject,
	error,
) {
	if runtime == nil || runtime.environment == nil {
		return adaptiveBenchmarkControlSubject{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark control runtime is unavailable",
			)
	}
	environment, err := runtime.environment.ControlEnvironment()
	if err != nil {
		return adaptiveBenchmarkControlSubject{}, err
	}
	return adaptiveBenchmarkControlSubjectFromEnvironment(
		environment,
		runtime.ownerB,
		runtime.runID,
		true,
	)
}

func adaptiveBenchmarkControlSubjectFromEnvironment(
	environment *adaptiveBenchmarkChatEnvironment,
	ownerID uuid.UUID,
	runID uuid.UUID,
	outcomesForbidden bool,
) (adaptiveBenchmarkControlSubject, error) {
	if environment == nil {
		return adaptiveBenchmarkControlSubject{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark control environment is unavailable",
			)
	}
	environment.runtimeMu.Lock()
	defer environment.runtimeMu.Unlock()
	if environment.env == nil ||
		environment.env.run == nil ||
		environment.env.conv == nil ||
		environment.env.cfg == nil ||
		environment.recorder == nil ||
		environment.store == nil ||
		environment.observedClient == nil ||
		environment.adaptiveControls.memoryRecall == nil ||
		environment.bundle == nil ||
		ownerID == uuid.Nil ||
		runID == uuid.Nil ||
		environment.bundle.runID != runID ||
		(outcomesForbidden && !environment.outcomesForbidden) {
		return adaptiveBenchmarkControlSubject{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark control subject is incomplete",
			)
	}
	registration := adaptiveBenchmarkDeterministicEvaluatorRegistration()
	catalog, err := adaptive.NewEvaluatorCatalog(registration)
	if err != nil {
		return adaptiveBenchmarkControlSubject{}, err
	}
	var outcomes adaptiveBenchmarkOutcomeWriter
	if !outcomesForbidden {
		outcomes, err = adaptive.NewOutcomeRecorder(
			environment.store,
			catalog,
		)
		if err != nil {
			return adaptiveBenchmarkControlSubject{}, err
		}
	}
	return adaptiveBenchmarkControlSubject{
		runner:        environment.env.run,
		conversations: environment.env.conv,
		recorder:      environment.recorder,
		ledger:        environment.store,
		outcomes:      outcomes,
		catalog:       catalog,
		observed:      environment.observedClient,
		memoryRecall:  environment.adaptiveControls.memoryRecall,
		fixtures:      environment.fixtureAliases,
		ownerID:       ownerID,
		runID:         runID,
		providerID:    environment.env.cfg.LLM.Provider,
		modelID:       environment.env.cfg.LLM.Model,
	}, nil
}

func (subject adaptiveBenchmarkControlSubject) newDriver(
	outcomes adaptiveBenchmarkOutcomeWriter,
) (*adaptiveBenchmarkAuraDriver, error) {
	if outcomes == nil {
		outcomes = subject.outcomes
	}
	if outcomes == nil {
		return nil, errors.New(
			"adaptive benchmark control outcomes are unavailable",
		)
	}
	return newAdaptiveBenchmarkAuraDriverWithDeps(
		adaptiveBenchmarkAuraDriverDeps{
			runner: subject.runner, recorder: subject.recorder,
			ledger: subject.ledger, outcomes: outcomes,
			catalog: subject.catalog, fixtureAliases: subject.fixtures,
			ownerID: subject.ownerID, runID: subject.runID,
			providerID: subject.providerID, modelID: subject.modelID,
			newConversationID: uuid.NewV7, now: time.Now,
			cleanupTimeout: adaptiveBenchmarkConversationCleanupTimeout,
		},
	)
}

func adaptiveBenchmarkControlScenario(
	scenarioID string,
) (auraeval.AdaptiveBenchmarkScenario, error) {
	dataset, err := auraeval.LoadFrozenAdaptiveBenchmarkDataset()
	if err != nil {
		return auraeval.AdaptiveBenchmarkScenario{}, err
	}
	for _, scenario := range dataset.Scenarios {
		if scenario.ScenarioID == scenarioID {
			return scenario, nil
		}
	}
	return auraeval.AdaptiveBenchmarkScenario{}, fmt.Errorf(
		"adaptive benchmark control scenario %s is unavailable",
		scenarioID,
	)
}

func adaptiveBenchmarkControlRunScenario(
	ctx context.Context,
	subject adaptiveBenchmarkControlSubject,
	scenarioID string,
	outcomes adaptiveBenchmarkOutcomeWriter,
) (
	auraeval.AdaptiveBenchmarkScenario,
	auraeval.AdaptiveBenchmarkDriverResult,
	*adaptiveBenchmarkAuraDriver,
	error,
) {
	scenario, err := adaptiveBenchmarkControlScenario(scenarioID)
	if err != nil {
		return auraeval.AdaptiveBenchmarkScenario{},
			auraeval.AdaptiveBenchmarkDriverResult{}, nil, err
	}
	driver, err := subject.newDriver(outcomes)
	if err != nil {
		return auraeval.AdaptiveBenchmarkScenario{},
			auraeval.AdaptiveBenchmarkDriverResult{}, nil, err
	}
	result, runErr := driver.RunScenario(
		ctx,
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: scenario.ScenarioID,
			Domain:     scenario.Domain,
			Prompt:     scenario.Prompt,
		},
	)
	return scenario, result, driver, runErr
}

func adaptiveBenchmarkControlCreateConversation(
	ctx context.Context,
	subject adaptiveBenchmarkControlSubject,
) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	ownerCtx := identityctx.WithIdentityID(
		ctx,
		subject.ownerID.String(),
	)
	created, err := subject.runner.NewConversationWithID(
		ownerCtx,
		id.String(),
	)
	if err != nil ||
		created != id.String() {
		return uuid.Nil, errors.New(
			"adaptive benchmark control conversation creation failed",
		)
	}
	return id, nil
}

func adaptiveBenchmarkControlRunTurn(
	ctx context.Context,
	subject adaptiveBenchmarkControlSubject,
	conversationID uuid.UUID,
	message string,
) (adaptiveBenchmarkControlTurn, error) {
	return adaptiveBenchmarkControlRunTurnObserved(
		ctx,
		subject,
		conversationID,
		message,
		nil,
	)
}

func adaptiveBenchmarkControlRunTurnObserved(
	ctx context.Context,
	subject adaptiveBenchmarkControlSubject,
	conversationID uuid.UUID,
	message string,
	terminalObserver func(time.Time),
) (adaptiveBenchmarkControlTurn, error) {
	if conversationID == uuid.Nil || strings.TrimSpace(message) == "" {
		return adaptiveBenchmarkControlTurn{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark control turn is invalid",
			)
	}
	ownerCtx := identityctx.WithIdentityID(
		ctx,
		subject.ownerID.String(),
	)
	var requestID uuid.UUID
	var terminal *agent.Event
	for event, turnErr := range subject.runner.Turn(
		ownerCtx,
		conversationID.String(),
		&message,
	) {
		if turnErr != nil {
			return adaptiveBenchmarkControlTurn{
				RequestID: requestID, ConversationID: conversationID,
				Terminal: terminal,
			}, turnErr
		}
		if event == nil ||
			event.RequestID == uuid.Nil ||
			event.RequestID.Version() != 7 ||
			(requestID != uuid.Nil && requestID != event.RequestID) {
			return adaptiveBenchmarkControlTurn{},
				adaptiveBenchmarkControlError(
					"adaptive benchmark control request identity drifted",
				)
		}
		requestID = event.RequestID
		if event.LLMResponse != nil &&
			event.LLMResponse.FinishReason != "" {
			if terminal != nil {
				return adaptiveBenchmarkControlTurn{},
					adaptiveBenchmarkControlError(
						"adaptive benchmark control has duplicate terminal events",
					)
			}
			terminal = event
			if terminalObserver != nil {
				terminalObserver(time.Now().Round(0).UTC())
			}
		}
	}
	if requestID == uuid.Nil || terminal == nil {
		return adaptiveBenchmarkControlTurn{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark control turn did not terminate",
			)
	}
	return adaptiveBenchmarkControlTurn{
		RequestID: requestID, ConversationID: conversationID,
		Terminal: terminal,
	}, nil
}

func adaptiveBenchmarkControlDeleteConversation(
	ctx context.Context,
	subject adaptiveBenchmarkControlSubject,
	conversationID uuid.UUID,
) error {
	ownerCtx := identityctx.WithIdentityID(
		ctx,
		subject.ownerID.String(),
	)
	affected, err := subject.runner.DeleteConversationLifecycle(
		ownerCtx,
		subject.ownerID.String(),
		conversationID.String(),
	)
	if err != nil {
		return err
	}
	if affected != 1 {
		return adaptiveBenchmarkControlError(
			"adaptive benchmark control conversation was not deleted",
		)
	}
	return nil
}

func adaptiveBenchmarkControlEvidenceFromResult(
	result auraeval.AdaptiveBenchmarkDriverResult,
) ([]uuid.UUID, error) {
	values := []string{
		result.RequestID,
		result.AssignmentID,
		result.DeliveryEventID,
		result.HarmOutcomeID,
	}
	ids := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		id, err := parseCanonicalAdaptiveBenchmarkUUID(value)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
