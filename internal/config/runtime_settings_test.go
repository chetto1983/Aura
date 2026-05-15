package config

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/aura/aura/internal/budget"
)

type fakeSettingsReader map[string]string

func (f fakeSettingsReader) Get(_ context.Context, key string) (string, error) {
	if v, ok := f[key]; ok {
		return v, nil
	}
	return "", ErrNotFound
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
	t.Setenv("SWARM_RESEARCH_MAX_WORKERS", "")
	t.Setenv("SWARM_RESEARCH_TIMEOUT_MS", "")
	t.Setenv("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS", "")
	t.Setenv("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "")
	t.Setenv("SWARM_RESEARCH_MAX_RESULT_CHARS", "")
	store := fakeSettingsReader{
		KeyAuraBotMaxActive:     "6",
		KeyAuraBotMaxDepth:      "2",
		KeyAuraBotTimeoutSec:    "45",
		KeyAuraBotMaxIterations: "9",
		KeySoftBudget:           "3.25",
		KeyHardBudget:           "8.5",
		KeyCostInputPerMTokens:  "0.14",
		KeyCostOutputPerMTokens: "0.42",
		KeySkillRoutingMode:     "manifest_llm_review",
		KeyAgentLoopMaxSteps:    "8",
		KeyTerminalToolPolicy:   "off",
		KeyDelegationMode:       "bounded",
		KeyTraceRetentionDays:   "45",
	}
	cfg := &Config{
		SoftBudget:           1,
		HardBudget:           2,
		CostInputPerMTokens:  0.2,
		CostOutputPerMTokens: 0.8,
		SkillRoutingMode:     DefaultSkillRoutingMode,
		AgentLoopMaxSteps:    DefaultAgentLoopMaxSteps,
		TerminalToolPolicy:   DefaultTerminalToolPolicy,
		DelegationMode:       DefaultDelegationMode,
		TraceRetentionDays:   DefaultTraceRetentionDays,
	}
	runner := &fakeAgentLimits{}
	manager := &fakeSwarmLimits{}
	tracker := &fakeBudgetConfigurator{}

	if err := ApplyRuntimeSettings(context.Background(), store, cfg, runner, manager, tracker, slog.Default()); err != nil {
		t.Fatalf("ApplyRuntimeSettings: %v", err)
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
	if cfg.SkillRoutingMode != "manifest_llm_review" ||
		cfg.AgentLoopMaxSteps != 8 ||
		cfg.TerminalToolPolicy != "off" ||
		cfg.DelegationMode != "bounded" ||
		cfg.TraceRetentionDays != 45 {
		t.Fatalf("cfg orchestration settings = %+v", cfg)
	}
	if os.Getenv("SWARM_RESEARCH_MAX_WORKERS") != "3" ||
		os.Getenv("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS") != "7000" ||
		os.Getenv("SWARM_RESEARCH_MAX_RESULT_CHARS") != "16000" {
		t.Fatalf("bounded delegation env = workers:%q final:%q chars:%q", os.Getenv("SWARM_RESEARCH_MAX_WORKERS"), os.Getenv("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS"), os.Getenv("SWARM_RESEARCH_MAX_RESULT_CHARS"))
	}
}

func TestApplyDelegationModeRuntimeFastAndBounded(t *testing.T) {
	t.Setenv("SWARM_RESEARCH_MAX_WORKERS", "")
	t.Setenv("SWARM_RESEARCH_TIMEOUT_MS", "")
	t.Setenv("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS", "")
	t.Setenv("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "")
	t.Setenv("SWARM_RESEARCH_MAX_RESULT_CHARS", "")

	applyDelegationModeRuntime("fast")
	if os.Getenv("SWARM_RESEARCH_MAX_WORKERS") != "1" ||
		os.Getenv("SWARM_RESEARCH_TIMEOUT_MS") != "25000" ||
		os.Getenv("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS") != "4000" {
		t.Fatalf("fast delegation env = workers:%q timeout:%q final:%q", os.Getenv("SWARM_RESEARCH_MAX_WORKERS"), os.Getenv("SWARM_RESEARCH_TIMEOUT_MS"), os.Getenv("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS"))
	}

	applyDelegationModeRuntime("bounded")
	if os.Getenv("SWARM_RESEARCH_MAX_WORKERS") != "3" ||
		os.Getenv("SWARM_RESEARCH_TIMEOUT_MS") != "30000" ||
		os.Getenv("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS") != "7000" {
		t.Fatalf("bounded delegation env = workers:%q timeout:%q final:%q", os.Getenv("SWARM_RESEARCH_MAX_WORKERS"), os.Getenv("SWARM_RESEARCH_TIMEOUT_MS"), os.Getenv("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS"))
	}
}

func TestApplyRuntimeSettingsIgnoresMissingStoreAndConfig(t *testing.T) {
	if err := ApplyRuntimeSettings(context.Background(), nil, &Config{}, nil, nil, nil, nil); err != nil {
		t.Fatalf("nil store error = %v", err)
	}
	if err := ApplyRuntimeSettings(context.Background(), fakeSettingsReader{}, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("nil config error = %v", err)
	}
}

func TestApplyRuntimeSettingsFallsBackWhenReaderErrors(t *testing.T) {
	store := failingSettingsReader{}
	cfg := &Config{
		SoftBudget:           1,
		HardBudget:           2,
		CostInputPerMTokens:  0.2,
		CostOutputPerMTokens: 0.8,
	}
	tracker := &fakeBudgetConfigurator{}

	if err := ApplyRuntimeSettings(context.Background(), store, cfg, nil, nil, tracker, nil); err != nil {
		t.Fatalf("ApplyRuntimeSettings: %v", err)
	}
	if tracker.cfg.SoftBudget != 1 || tracker.cfg.HardBudget != 2 || tracker.cfg.InputCostPerMTokens != 0.2 || tracker.cfg.OutputCostPerMTokens != 0.8 {
		t.Fatalf("fallback budget config = %+v", tracker.cfg)
	}
}

type failingSettingsReader struct{}

func (failingSettingsReader) Get(context.Context, string) (string, error) {
	return "", errors.New("db unavailable")
}
