package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAssembleChatEnvOptionsApplyRegistryTransformBeforeRunner(t *testing.T) {
	pool := unreachablePool(t)
	cfg := validBootConfig()
	var projectorOrder []string
	transformCalls := 0
	clientTransformCalls := 0
	transformedClient := &adaptiveBenchmarkLLMClientFake{}
	env, err := assembleChatEnvWithOptions(
		context.Background(),
		cfg,
		pool,
		func(context.Context, *knowledge.Config) (adaptiveGraphClient, error) {
			return &adaptiveProjectorClientSpy{order: &projectorOrder}, nil
		},
		chatEnvAssemblyOptions{
			adaptiveControls: func(
				context.Context,
				*config.Config,
				*pgxpool.Pool,
			) (adaptiveControlSet, error) {
				return adaptiveControlSet{}, nil
			},
			registryTransform: func(
				registry *tools.Registry,
			) (*tools.Registry, error) {
				transformCalls++
				if registry == nil || len(registry.All()) < 4 {
					t.Fatal("registry transform did not receive full registry")
				}
				return registry, nil
			},
			clientTransform: func(
				client llm.Client,
			) (llm.Client, error) {
				clientTransformCalls++
				if client == nil {
					t.Fatal("client transform received nil client")
				}
				return transformedClient, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("assembleChatEnvWithOptions: %v", err)
	}
	t.Cleanup(env.close)
	if transformCalls != 1 ||
		clientTransformCalls != 1 ||
		env.reg == nil ||
		env.client != transformedClient {
		t.Fatalf(
			"registry_transform=%d client_transform=%d registry=%v client=%T",
			transformCalls,
			clientTransformCalls,
			env.reg,
			env.client,
		)
	}
	if reflect.ValueOf(env.run).Elem().FieldByName("registry").Pointer() !=
		reflect.ValueOf(env.reg).Pointer() {
		t.Fatal("runner did not receive transformed registry")
	}
	runnerClient := reflect.ValueOf(env.run).
		Elem().
		FieldByName("client").
		Elem().
		Pointer()
	if runnerClient != reflect.ValueOf(transformedClient).Pointer() {
		t.Fatal("runner did not receive transformed client")
	}
}

func TestAssembleChatEnvRegistryTransformFailureClosesPool(t *testing.T) {
	pool := unreachablePool(t)
	cfg := validBootConfig()
	transformErr := errors.New("benchmark registry rejected")
	env, err := assembleChatEnvWithOptions(
		context.Background(),
		cfg,
		pool,
		nil,
		chatEnvAssemblyOptions{
			adaptiveControls: func(
				context.Context,
				*config.Config,
				*pgxpool.Pool,
			) (adaptiveControlSet, error) {
				return adaptiveControlSet{}, nil
			},
			registryTransform: func(
				*tools.Registry,
			) (*tools.Registry, error) {
				return nil, transformErr
			},
		},
	)
	if env != nil || !errors.Is(err, transformErr) {
		t.Fatalf("env=%v error=%v", env, err)
	}
	assertPoolClosed(t, pool)
}

func TestAssembleChatEnvClientTransformFailureClosesPool(t *testing.T) {
	pool := unreachablePool(t)
	cfg := validBootConfig()
	transformErr := errors.New("benchmark client rejected")
	env, err := assembleChatEnvWithOptions(
		context.Background(),
		cfg,
		pool,
		nil,
		chatEnvAssemblyOptions{
			adaptiveControls: func(
				context.Context,
				*config.Config,
				*pgxpool.Pool,
			) (adaptiveControlSet, error) {
				return adaptiveControlSet{}, nil
			},
			clientTransform: func(
				llm.Client,
			) (llm.Client, error) {
				return nil, transformErr
			},
		},
	)
	if env != nil || !errors.Is(err, transformErr) {
		t.Fatalf("env=%v error=%v", env, err)
	}
	assertPoolClosed(t, pool)
}
