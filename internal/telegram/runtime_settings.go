package telegram

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/settings"
	"github.com/aura/aura/internal/swarm"
)

func applyRuntimeSettings(ctx context.Context, store settings.Reader, cfg *config.Config, runner agent.LimitController, manager swarm.LimitController, tracker budget.Configurator, logger *slog.Logger) error {
	if store == nil || cfg == nil {
		return nil
	}

	maxActive := intSetting(ctx, store, settings.KeyAuraBotMaxActive, "AURABOT_MAX_ACTIVE", 4)
	maxDepth := intSetting(ctx, store, settings.KeyAuraBotMaxDepth, "AURABOT_MAX_DEPTH", 1)
	timeoutSec := intSetting(ctx, store, settings.KeyAuraBotTimeoutSec, "AURABOT_TIMEOUT_SEC", config.DefaultAuraBotTimeoutSec)
	maxIterations := intSetting(ctx, store, settings.KeyAuraBotMaxIterations, "AURABOT_MAX_ITERATIONS", 5)
	softBudget := floatSetting(ctx, store, settings.KeySoftBudget, "SOFT_BUDGET", cfg.SoftBudget)
	hardBudget := floatSetting(ctx, store, settings.KeyHardBudget, "HARD_BUDGET", cfg.HardBudget)
	inputPerM := floatSetting(ctx, store, settings.KeyCostInputPerMTokens, "COST_INPUT_PER_M_TOKENS", cfg.CostInputPerMTokens)
	outputPerM := floatSetting(ctx, store, settings.KeyCostOutputPerMTokens, "COST_OUTPUT_PER_M_TOKENS", cfg.CostOutputPerMTokens)
	skillPreflight := skillPreflightSetting(ctx, store, cfg.SkillPreflight)
	skillRoutingMode := config.NormalizeSkillRoutingMode(stringSetting(ctx, store, settings.KeySkillRoutingMode, "AURA_SKILL_ROUTING_MODE", cfg.SkillRoutingMode))
	agentLoopMaxSteps := intRangeSetting(ctx, store, settings.KeyAgentLoopMaxSteps, "AURA_AGENT_LOOP_MAX_STEPS", cfg.AgentLoopMaxSteps, 1, 50, config.DefaultAgentLoopMaxSteps)
	terminalToolPolicy := config.NormalizeTerminalToolPolicy(stringSetting(ctx, store, settings.KeyTerminalToolPolicy, "AURA_TERMINAL_TOOL_POLICY", cfg.TerminalToolPolicy))
	delegationMode := config.NormalizeDelegationMode(stringSetting(ctx, store, settings.KeyDelegationMode, "AURA_DELEGATION_MODE", cfg.DelegationMode))
	traceRetentionDays := intRangeSetting(ctx, store, settings.KeyTraceRetentionDays, "AURA_TRACE_RETENTION_DAYS", cfg.TraceRetentionDays, 1, 365, config.DefaultTraceRetentionDays)

	if manager != nil {
		manager.UpdateLimits(maxActive, maxDepth)
	}
	if runner != nil {
		runner.UpdateLimits(maxIterations, time.Duration(timeoutSec)*time.Second, time.Duration(timeoutSec)*time.Second)
	}

	cfg.AuraBotMaxActive = maxActive
	cfg.AuraBotMaxDepth = maxDepth
	cfg.AuraBotTimeoutSec = timeoutSec
	cfg.AuraBotMaxIterations = maxIterations
	cfg.SoftBudget = softBudget
	cfg.HardBudget = hardBudget
	cfg.CostInputPerMTokens = inputPerM
	cfg.CostOutputPerMTokens = outputPerM
	cfg.SkillPreflight = skillPreflight
	cfg.SkillRoutingMode = skillRoutingMode
	cfg.AgentLoopMaxSteps = agentLoopMaxSteps
	cfg.TerminalToolPolicy = terminalToolPolicy
	cfg.DelegationMode = delegationMode
	cfg.TraceRetentionDays = traceRetentionDays
	applyDelegationModeRuntime(delegationMode)

	if tracker != nil {
		tracker.ApplyConfig(budget.Config{
			SoftBudget:           softBudget,
			HardBudget:           hardBudget,
			InputCostPerMTokens:  inputPerM,
			OutputCostPerMTokens: outputPerM,
		})
	}

	if logger != nil {
		logger.Info("runtime settings applied",
			"max_active", maxActive,
			"max_depth", maxDepth,
			"timeout_sec", timeoutSec,
			"max_iterations", maxIterations,
			"soft_budget", softBudget,
			"hard_budget", hardBudget,
			"input_per_m_tokens", inputPerM,
			"output_per_m_tokens", outputPerM,
			"skill_preflight", skillPreflight,
			"skill_routing_mode", skillRoutingMode,
			"agent_loop_max_steps", agentLoopMaxSteps,
			"terminal_tool_policy", terminalToolPolicy,
			"delegation_mode", delegationMode,
			"trace_retention_days", traceRetentionDays,
		)
	}
	return nil
}

func intSetting(ctx context.Context, store settings.Reader, key, envKey string, fallback int) int {
	if store != nil {
		if raw, err := store.Get(ctx, key); err == nil {
			if v, ok := parsePositiveInt(raw); ok {
				return v
			}
		}
	}
	if raw := os.Getenv(envKey); raw != "" {
		if v, ok := parsePositiveInt(raw); ok {
			return v
		}
	}
	return fallback
}

func intRangeSetting(ctx context.Context, store settings.Reader, key, envKey string, fallback, min, max, defaultValue int) int {
	value := intSetting(ctx, store, key, envKey, fallback)
	if value < min || value > max {
		return defaultValue
	}
	return value
}

func stringSetting(ctx context.Context, store settings.Reader, key, envKey, fallback string) string {
	if store != nil {
		if raw, err := store.Get(ctx, key); err == nil && strings.TrimSpace(raw) != "" {
			return raw
		}
	}
	if raw := os.Getenv(envKey); strings.TrimSpace(raw) != "" {
		return raw
	}
	return fallback
}

func skillPreflightSetting(ctx context.Context, store settings.Reader, fallback string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(stringSetting(ctx, store, settings.KeySkillPreflight, "AURA_SKILL_PREFLIGHT", fallback))); normalized {
	case "advisory", "off":
		return normalized
	default:
		return config.DefaultSkillPreflight
	}
}

func applyDelegationModeRuntime(mode string) {
	switch config.NormalizeDelegationMode(mode) {
	case "bounded":
		setenvIfChanged("SWARM_RESEARCH_MAX_WORKERS", "3")
		setenvIfChanged("SWARM_RESEARCH_TIMEOUT_MS", "30000")
		setenvIfChanged("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "3")
		setenvIfChanged("SWARM_RESEARCH_MAX_RESULT_CHARS", "16000")
	case "async":
		// Until durable async delegation lands, keep the bounded runtime shape
		// while exposing the operator intent in config/telemetry.
		setenvIfChanged("SWARM_RESEARCH_MAX_WORKERS", "3")
		setenvIfChanged("SWARM_RESEARCH_TIMEOUT_MS", "30000")
		setenvIfChanged("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "3")
		setenvIfChanged("SWARM_RESEARCH_MAX_RESULT_CHARS", "16000")
	default:
		setenvIfChanged("SWARM_RESEARCH_MAX_WORKERS", "1")
		setenvIfChanged("SWARM_RESEARCH_TIMEOUT_MS", "25000")
		setenvIfChanged("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "3")
		setenvIfChanged("SWARM_RESEARCH_MAX_RESULT_CHARS", "12000")
	}
}

func setenvIfChanged(key, value string) {
	if os.Getenv(key) == value {
		return
	}
	_ = os.Setenv(key, value)
}

func floatSetting(ctx context.Context, store settings.Reader, key, envKey string, fallback float64) float64 {
	if store != nil {
		if raw, err := store.Get(ctx, key); err == nil {
			if v, ok := parsePositiveFloat(raw); ok {
				return v
			}
		}
	}
	if raw := os.Getenv(envKey); raw != "" {
		if v, ok := parsePositiveFloat(raw); ok {
			return v
		}
	}
	return fallback
}

func parsePositiveFloat(raw string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func parsePositiveInt(raw string) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}
