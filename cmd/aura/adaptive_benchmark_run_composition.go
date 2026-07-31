package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/adaptive/orderingcontrol"
	"github.com/chetto1983/aura/internal/adaptive/reasoningcontrol"
	"github.com/chetto1983/aura/internal/config"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const adaptiveBenchmarkMemoryRecallMaxItems = 8

func adaptiveBenchmarkRuntimeConfig(cfg *config.Config) *config.Config {
	benchmarkCfg := *cfg
	benchmarkCfg.MemoryRecall = true
	benchmarkCfg.MemoryRecallMaxItems = adaptiveBenchmarkMemoryRecallMaxItems
	return &benchmarkCfg
}

func assembleAdaptiveBenchmarkRuntime(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
	model auraeval.AdaptiveBenchmarkModel,
) (*adaptiveBenchmarkChatEnvironment, error) {
	if cfg == nil ||
		pool == nil ||
		request.OwnerID == uuid.Nil ||
		runID == uuid.Nil ||
		model.ProviderID != cfg.LLM.Provider ||
		model.ModelID != cfg.LLM.Model {
		return nil, errors.New(
			"adaptive benchmark runtime scope is invalid",
		)
	}
	benchmarkCfg := adaptiveBenchmarkRuntimeConfig(cfg)
	success := false
	defer func() {
		if !success {
			pool.Close()
		}
	}()
	if err := validateAdaptiveBenchmarkControlOwner(
		ctx,
		identity.New(pool),
		request.OwnerID,
	); err != nil {
		return nil, err
	}
	observed, err := adaptive.NewPolicyStore(pool).CurrentPolicy(ctx)
	if err != nil {
		return nil, err
	}
	bundle, err := buildAdaptiveBenchmarkDecisionBundle(
		runID,
		request.OwnerID,
		model.ProviderID,
		model.ModelID,
		observed,
	)
	if err != nil {
		return nil, err
	}
	state, err := assembleAdaptiveBenchmarkRuntimeState(
		ctx,
		benchmarkCfg,
		pool,
		request,
		runID,
		model,
		bundle,
		nil,
	)
	if err != nil {
		return nil, err
	}
	controlRequest := request
	controlRequest.OwnerID = uuid.MustParse(
		identityctx.CLIServiceIdentity,
	)
	controlState, err := newAdaptiveBenchmarkRuntimeReassembler(
		benchmarkCfg,
		controlRequest,
		runID,
		model,
		bundle,
		productionAdaptiveBenchmarkRestartOps(),
		nil,
	)(ctx)
	if err != nil {
		state.env.close()
		return nil, fmt.Errorf(
			"assemble adaptive benchmark control runtime: %w",
			err,
		)
	}
	controlEnvironment := &adaptiveBenchmarkChatEnvironment{
		env: controlState.env, bundle: bundle,
		recorder: controlState.recorder, store: controlState.store,
		observedClient:    controlState.observedClient,
		adaptiveControls:  controlState.adaptiveControls,
		outcomesForbidden: true,
	}
	fixtures, err := installAdaptiveBenchmarkFixtures(
		ctx,
		benchmarkCfg,
		request.OwnerID,
		runID,
		productionAdaptiveBenchmarkFixtureDeps(),
	)
	if err != nil {
		_ = controlEnvironment.Close()
		state.env.close()
		return nil, err
	}
	success = true
	return &adaptiveBenchmarkChatEnvironment{
		env: state.env, bundle: bundle,
		recorder: state.recorder, store: state.store,
		observedClient:     state.observedClient,
		adaptiveControls:   state.adaptiveControls,
		fixtureAliases:     fixtures,
		fixtureCleanup:     fixtures.Cleanup,
		controlEnvironment: controlEnvironment,
		reassemble: newAdaptiveBenchmarkRuntimeReassembler(
			benchmarkCfg,
			request,
			runID,
			model,
			bundle,
			productionAdaptiveBenchmarkRestartOps(),
			nil,
		),
		forkControl: newAdaptiveBenchmarkRuntimeForker(
			benchmarkCfg,
			request,
			runID,
			model,
			bundle,
			productionAdaptiveBenchmarkRestartOps(),
		),
	}, nil
}

func assembleAdaptiveBenchmarkRuntimeState(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
	model auraeval.AdaptiveBenchmarkModel,
	bundle *adaptiveBenchmarkDecisionBundle,
	memoryControlTransform adaptiveBenchmarkMemoryControlTransform,
) (adaptiveBenchmarkRuntimeState, error) {
	if cfg == nil ||
		pool == nil ||
		request.OwnerID == uuid.Nil ||
		runID == uuid.Nil ||
		bundle == nil ||
		bundle.runID != runID ||
		(request.OwnerID != bundle.primaryOwnerID &&
			request.OwnerID != bundle.controlOwnerID) ||
		bundle.providerID != model.ProviderID ||
		bundle.modelID != model.ModelID {
		return adaptiveBenchmarkRuntimeState{}, errors.New(
			"adaptive benchmark runtime state scope is invalid",
		)
	}
	store := adaptive.NewStore(pool, adaptive.StoreConfig{})
	recorder, err := newAdaptiveBenchmarkTeeRecorder(store)
	if err != nil {
		return adaptiveBenchmarkRuntimeState{}, err
	}
	controls, err := newAdaptiveBenchmarkOfflineControls(
		bundle,
		recorder,
		request.OwnerID,
		model.ProviderID,
		model.ModelID,
	)
	if err != nil {
		return adaptiveBenchmarkRuntimeState{}, err
	}
	if memoryControlTransform != nil {
		if controls.memoryRecall == nil {
			return adaptiveBenchmarkRuntimeState{}, errors.New(
				"adaptive benchmark memory control is unavailable",
			)
		}
		controls.memoryRecall = memoryControlTransform(
			controls.memoryRecall,
		)
		if controls.memoryRecall == nil {
			return adaptiveBenchmarkRuntimeState{}, errors.New(
				"adaptive benchmark memory control transform returned nil",
			)
		}
	}
	var observedClient *adaptiveBenchmarkObservedClient
	env, err := assembleChatEnvWithOptions(
		ctx,
		cfg,
		pool,
		nil,
		chatEnvAssemblyOptions{
			adaptiveControls: func(
				context.Context,
				*config.Config,
				*pgxpool.Pool,
			) (adaptiveControlSet, error) {
				return controls, nil
			},
			registryTransform: restrictAdaptiveBenchmarkRegistry,
			clientTransform: func(
				client llm.Client,
			) (llm.Client, error) {
				observedClient, err =
					newAdaptiveBenchmarkObservedClient(client)
				return observedClient, err
			},
		},
	)
	if err != nil {
		return adaptiveBenchmarkRuntimeState{}, err
	}
	if observedClient == nil {
		env.close()
		return adaptiveBenchmarkRuntimeState{}, errors.New(
			"adaptive benchmark observed client is unavailable",
		)
	}
	return adaptiveBenchmarkRuntimeState{
		env: env, recorder: recorder, store: store,
		observedClient:   observedClient,
		adaptiveControls: controls,
	}, nil
}

func newAdaptiveBenchmarkOfflineControls(
	bundle *adaptiveBenchmarkDecisionBundle,
	recorder adaptiveBenchmarkEventRecorder,
	ownerID uuid.UUID,
	providerID string,
	modelID string,
) (adaptiveControlSet, error) {
	if bundle == nil ||
		recorder == nil ||
		ownerID == uuid.Nil ||
		(ownerID != bundle.primaryOwnerID &&
			ownerID != bundle.controlOwnerID) ||
		providerID == "" ||
		modelID == "" {
		return adaptiveControlSet{}, errors.New(
			"adaptive benchmark offline controls are unavailable",
		)
	}
	controls := adaptiveControlSet{
		skillRouting: orderingcontrol.NewOfflineSkillRouting(
			bundle.PolicyReader(
				ownerID,
				adaptive.DomainSkillRouting,
			),
			bundle,
			recorder,
			providerID,
			modelID,
		),
		memoryRecall: orderingcontrol.NewOfflineDynamicRecall(
			bundle.PolicyReader(
				ownerID,
				adaptive.DomainMemoryRecall,
			),
			bundle,
			recorder,
			providerID,
			modelID,
		),
		reasoning: reasoningcontrol.NewOffline(
			bundle.PolicyReader(
				ownerID,
				adaptive.DomainReasoning,
			),
			bundle,
			recorder,
			providerID,
			modelID,
		),
	}
	if controls.skillRouting == nil ||
		controls.memoryRecall == nil ||
		controls.reasoning == nil {
		return adaptiveControlSet{}, errors.New(
			"adaptive benchmark offline controls are incomplete",
		)
	}
	return controls, nil
}
