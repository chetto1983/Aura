package telegram

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/settings"
)

type fakeSettingsReader map[string]string

func (f fakeSettingsReader) Get(_ context.Context, key string) (string, error) {
	if v, ok := f[key]; ok {
		return v, nil
	}
	return "", settings.ErrNotFound
}

type fakeAgentLimits struct {
	maxIterations int
	timeout       time.Duration
	toolTimeout   time.Duration
}

func (f *fakeAgentLimits) UpdateLimits(maxIterations int, timeout time.Duration, toolTimeout time.Duration) {
	f.maxIterations = maxIterations
	f.timeout = timeout
	f.toolTimeout = toolTimeout
}

type fakeSwarmLimits struct {
	maxActive int
	maxDepth  int
}

func (f *fakeSwarmLimits) UpdateLimits(maxActive, maxDepth int) {
	f.maxActive = maxActive
	f.maxDepth = maxDepth
}

type fakeBudgetConfigurator struct {
	cfg budget.Config
}

func (f *fakeBudgetConfigurator) ApplyConfig(cfg budget.Config) {
	f.cfg = cfg
}

func TestApplyRuntimeSettingsUsesServiceBoundaries(t *testing.T) {
	store := fakeSettingsReader{
		settings.KeyAuraBotMaxActive:     "6",
		settings.KeyAuraBotMaxDepth:      "2",
		settings.KeyAuraBotTimeoutSec:    "45",
		settings.KeyAuraBotMaxIterations: "9",
		settings.KeySoftBudget:           "3.25",
		settings.KeyHardBudget:           "8.5",
		settings.KeyCostInputPerMTokens:  "0.14",
		settings.KeyCostOutputPerMTokens: "0.42",
	}
	cfg := &config.Config{
		SoftBudget:           1,
		HardBudget:           2,
		CostInputPerMTokens:  0.2,
		CostOutputPerMTokens: 0.8,
	}
	runner := &fakeAgentLimits{}
	manager := &fakeSwarmLimits{}
	tracker := &fakeBudgetConfigurator{}

	if err := applyRuntimeSettings(context.Background(), store, cfg, runner, manager, tracker, slog.Default()); err != nil {
		t.Fatalf("applyRuntimeSettings: %v", err)
	}

	if manager.maxActive != 6 || manager.maxDepth != 2 {
		t.Fatalf("swarm limits = active:%d depth:%d", manager.maxActive, manager.maxDepth)
	}
	if runner.maxIterations != 9 || runner.timeout != 45*time.Second || runner.toolTimeout != 45*time.Second {
		t.Fatalf("runner limits = iterations:%d timeout:%s tool:%s", runner.maxIterations, runner.timeout, runner.toolTimeout)
	}
	if cfg.AuraBotMaxActive != 6 || cfg.AuraBotMaxDepth != 2 || cfg.AuraBotTimeoutSec != 45 || cfg.AuraBotMaxIterations != 9 {
		t.Fatalf("cfg aurabot limits = %+v", cfg)
	}
	if tracker.cfg.SoftBudget != 3.25 || tracker.cfg.HardBudget != 8.5 || tracker.cfg.InputCostPerMTokens != 0.14 || tracker.cfg.OutputCostPerMTokens != 0.42 {
		t.Fatalf("budget config = %+v", tracker.cfg)
	}
}

func TestApplyRuntimeSettingsIgnoresMissingStoreAndConfig(t *testing.T) {
	if err := applyRuntimeSettings(context.Background(), nil, &config.Config{}, nil, nil, nil, nil); err != nil {
		t.Fatalf("nil store error = %v", err)
	}
	if err := applyRuntimeSettings(context.Background(), fakeSettingsReader{}, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("nil config error = %v", err)
	}
}

func TestApplyRuntimeSettingsFallsBackWhenReaderErrors(t *testing.T) {
	store := failingSettingsReader{}
	cfg := &config.Config{
		SoftBudget:           1,
		HardBudget:           2,
		CostInputPerMTokens:  0.2,
		CostOutputPerMTokens: 0.8,
	}
	tracker := &fakeBudgetConfigurator{}

	if err := applyRuntimeSettings(context.Background(), store, cfg, nil, nil, tracker, nil); err != nil {
		t.Fatalf("applyRuntimeSettings: %v", err)
	}
	if tracker.cfg.SoftBudget != 1 || tracker.cfg.HardBudget != 2 || tracker.cfg.InputCostPerMTokens != 0.2 || tracker.cfg.OutputCostPerMTokens != 0.8 {
		t.Fatalf("fallback budget config = %+v", tracker.cfg)
	}
}

type failingSettingsReader struct{}

func (failingSettingsReader) Get(context.Context, string) (string, error) {
	return "", errors.New("db unavailable")
}
