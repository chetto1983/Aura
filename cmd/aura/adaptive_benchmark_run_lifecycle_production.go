package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type adaptiveBenchmarkPGXPool struct {
	pool *pgxpool.Pool
}

func (pool *adaptiveBenchmarkPGXPool) Close() {
	if pool != nil && pool.pool != nil {
		pool.pool.Close()
	}
}

func productionAdaptiveBenchmarkLifecycleOps() adaptiveBenchmarkLifecycleOps {
	return adaptiveBenchmarkLifecycleOps{
		newRunID: uuid.NewV7,
		preflightQwen: func(
			ctx context.Context,
		) (auraeval.AdaptiveBenchmarkModel, error) {
			return preflightAdaptiveBenchmarkQwen(
				ctx,
				http.DefaultClient,
				dockerAdaptiveBenchmarkQwenInspector{
					commands: adaptiveBenchmarkOSCommandRunner{},
				},
			)
		},
		openControlPool: func(
			ctx context.Context,
		) (adaptiveBenchmarkPool, error) {
			pool, ok, err := openSettingsOverlayPool(ctx)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, errors.New("settings database is unavailable")
			}
			return &adaptiveBenchmarkPGXPool{pool: pool}, nil
		},
		authorizeQwen: func(
			ctx context.Context,
			pool adaptiveBenchmarkPool,
			ownerID uuid.UUID,
		) (bool, error) {
			pgxPool, err := adaptiveBenchmarkUnwrapPool(pool)
			if err != nil {
				return false, err
			}
			return identity.New(pgxPool).HasCapability(
				ctx,
				ownerID.String(),
				governanceWriteCapability,
			)
		},
		beginOverride: func(
			ctx context.Context,
			pool adaptiveBenchmarkPool,
			request settingsBenchmarkOverrideRequest,
		) (adaptiveBenchmarkOverride, error) {
			pgxPool, err := adaptiveBenchmarkUnwrapPool(pool)
			if err != nil {
				return nil, err
			}
			return settings.BeginQwenPortabilityOverride(
				ctx,
				pgxPool,
				request,
			)
		},
		overlaySettings: func(
			ctx context.Context,
			pool adaptiveBenchmarkPool,
		) error {
			pgxPool, err := adaptiveBenchmarkUnwrapPool(pool)
			if err != nil {
				return err
			}
			return settings.OverlayEnv(ctx, settings.NewStore(pgxPool))
		},
		loadServeConfig: config.LoadServe,
		openRuntimePool: func(
			ctx context.Context,
			cfg *config.Config,
		) (adaptiveBenchmarkPool, error) {
			if cfg == nil {
				return nil, errors.New("benchmark config is unavailable")
			}
			pool, err := db.Open(ctx, &cfg.DB)
			if err != nil {
				return nil, err
			}
			return &adaptiveBenchmarkPGXPool{pool: pool}, nil
		},
		assembleRuntime:   assembleAdaptiveBenchmarkEnvironment,
		bootProduction:    bootProductionAdaptiveBenchmarkEnvironment,
		heartbeatInterval: adaptiveBenchmarkHeartbeatInterval,
	}
}

func assembleAdaptiveBenchmarkEnvironment(
	ctx context.Context,
	cfg *config.Config,
	pool adaptiveBenchmarkPool,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
	model auraeval.AdaptiveBenchmarkModel,
) (adaptiveBenchmarkEnvironment, error) {
	pgxPool, err := adaptiveBenchmarkUnwrapPool(pool)
	if err != nil {
		return nil, err
	}
	var environment adaptiveBenchmarkEnvironment
	_, err = assembleChatEnvAtMigrationHead(
		ctx,
		cfg,
		pgxPool,
		db.CheckMigrationHead,
		func(
			ctx context.Context,
			cfg *config.Config,
			pool *pgxpool.Pool,
		) (*chatEnv, error) {
			benchmarkEnv, assembleErr := assembleAdaptiveBenchmarkRuntime(
				ctx,
				cfg,
				pool,
				request,
				runID,
				model,
			)
			if assembleErr != nil {
				return nil, assembleErr
			}
			environment = benchmarkEnv
			return benchmarkEnv.env, nil
		},
	)
	if err != nil {
		return nil, err
	}
	return environment, nil
}

func bootProductionAdaptiveBenchmarkEnvironment(
	ctx context.Context,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
) (adaptiveBenchmarkEnvironment, auraeval.AdaptiveBenchmarkModel, error) {
	cfg, pool, err := resolveConfigAndPool(
		ctx,
		config.LoadServe,
		db.Open,
	)
	if err != nil {
		return nil, auraeval.AdaptiveBenchmarkModel{}, err
	}
	model, err := adaptiveBenchmarkModelFromConfig(cfg)
	if err != nil {
		pool.Close()
		return nil, auraeval.AdaptiveBenchmarkModel{}, err
	}
	var benchmarkEnvironment *adaptiveBenchmarkChatEnvironment
	_, err = assembleChatEnvAtMigrationHead(
		ctx,
		cfg,
		pool,
		db.CheckMigrationHead,
		func(
			ctx context.Context,
			cfg *config.Config,
			pool *pgxpool.Pool,
		) (*chatEnv, error) {
			benchmarkEnv, assembleErr := assembleAdaptiveBenchmarkRuntime(
				ctx,
				cfg,
				pool,
				request,
				runID,
				model,
			)
			if assembleErr != nil {
				return nil, assembleErr
			}
			benchmarkEnvironment = benchmarkEnv
			return benchmarkEnv.env, nil
		},
	)
	if err != nil {
		return nil, auraeval.AdaptiveBenchmarkModel{}, err
	}
	return benchmarkEnvironment, model, nil
}

func adaptiveBenchmarkUnwrapPool(
	pool adaptiveBenchmarkPool,
) (*pgxpool.Pool, error) {
	wrapped, ok := pool.(*adaptiveBenchmarkPGXPool)
	if !ok || wrapped == nil || wrapped.pool == nil {
		return nil, errors.New("adaptive benchmark database pool is invalid")
	}
	return wrapped.pool, nil
}

func adaptiveBenchmarkModelFromConfig(
	cfg *config.Config,
) (auraeval.AdaptiveBenchmarkModel, error) {
	if cfg == nil || cfg.LLM.Provider == "" || cfg.LLM.Model == "" {
		return auraeval.AdaptiveBenchmarkModel{}, errors.New(
			"configured production model is unavailable",
		)
	}
	baseURLClass := auraeval.AdaptiveBenchmarkBaseURLRemote
	parsed, err := url.Parse(cfg.LLM.BaseURL)
	if err != nil {
		return auraeval.AdaptiveBenchmarkModel{}, errors.New(
			"configured production model base URL is invalid",
		)
	}
	if parsed.Hostname() == "127.0.0.1" ||
		parsed.Hostname() == "::1" ||
		parsed.Hostname() == "localhost" {
		baseURLClass = auraeval.AdaptiveBenchmarkBaseURLLoopback
	}
	return auraeval.AdaptiveBenchmarkModel{
		ProviderID:   cfg.LLM.Provider,
		ModelID:      cfg.LLM.Model,
		BaseURLClass: baseURLClass,
	}, nil
}
