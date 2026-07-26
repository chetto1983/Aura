package main

import (
	"errors"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

func newAdaptiveBenchmarkAuraDriver(
	environment *adaptiveBenchmarkChatEnvironment,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
) (auraeval.AdaptiveBenchmarkDriver, error) {
	if environment == nil ||
		environment.outcomesForbidden ||
		environment.env == nil ||
		environment.env.run == nil ||
		environment.env.cfg == nil ||
		environment.recorder == nil ||
		environment.store == nil {
		return nil, errors.New(
			"adaptive benchmark Aura environment is unavailable",
		)
	}
	registration := adaptiveBenchmarkDeterministicEvaluatorRegistration()
	catalog, err := adaptive.NewEvaluatorCatalog(registration)
	if err != nil {
		return nil, err
	}
	outcomes, err := adaptive.NewOutcomeRecorder(environment.store, catalog)
	if err != nil {
		return nil, err
	}
	return newAdaptiveBenchmarkAuraDriverWithDeps(
		adaptiveBenchmarkAuraDriverDeps{
			runner: environment.env.run, recorder: environment.recorder,
			ledger: environment.store, outcomes: outcomes, catalog: catalog,
			fixtureAliases: environment.fixtureAliases,
			ownerID:        request.OwnerID, runID: runID,
			providerID:        environment.env.cfg.LLM.Provider,
			modelID:           environment.env.cfg.LLM.Model,
			newConversationID: uuid.NewV7, now: time.Now,
			cleanupTimeout: adaptiveBenchmarkConversationCleanupTimeout,
		},
	)
}
