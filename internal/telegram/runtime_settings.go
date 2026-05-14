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
	"github.com/aura/aura/internal/swarm"
)

func applyRuntimeSettings(ctx context.Context, store config.Reader, cfg *config.Config, runner agent.LimitController, manager swarm.LimitController, tracker budget.Configurator, logger *slog.Logger) error {
	if store == nil || cfg == nil {
		return nil
	}

	maxActive := intSetting(ctx, store, config.KeyAuraBotMaxActive, "AURABOT_MAX_ACTIVE", 4)
	maxDepth := intSetting(ctx, store, config.KeyAuraBotMaxDepth, "AURABOT_MAX_DEPTH", 1)
	timeoutSec := intSetting(ctx, store, config.KeyAuraBotTimeoutSec, "AURABOT_TIMEOUT_SEC", config.DefaultAuraBotTimeoutSec)
	maxIterations := intSetting(ctx, store, config.KeyAuraBotMaxIterations, "AURABOT_MAX_ITERATIONS", 5)
	softBudget := floatSetting(ctx, store, config.KeySoftBudget, "SOFT_BUDGET", cfg.SoftBudget)
	hardBudget := floatSetting(ctx, store, config.KeyHardBudget, "HARD_BUDGET", cfg.HardBudget)
	inputPerM := floatSetting(ctx, store, config.KeyCostInputPerMTokens, "COST_INPUT_PER_M_TOKENS", cfg.CostInputPerMTokens)
	outputPerM := floatSetting(ctx, store, config.KeyCostOutputPerMTokens, "COST_OUTPUT_PER_M_TOKENS", cfg.CostOutputPerMTokens)
	skillRoutingMode := config.NormalizeSkillRoutingMode(stringSetting(ctx, store, config.KeySkillRoutingMode, "AURA_SKILL_ROUTING_MODE", cfg.SkillRoutingMode))
	agentLoopMaxSteps := intRangeSetting(ctx, store, config.KeyAgentLoopMaxSteps, "AURA_AGENT_LOOP_MAX_STEPS", cfg.AgentLoopMaxSteps, 1, 10000, config.DefaultAgentLoopMaxSteps)
	toolSearchTopK := intRangeSetting(ctx, store, config.KeyToolSearchTopK, "TOOL_SEARCH_TOP_K", cfg.ToolSearchTopK, 1, 50, config.DefaultToolSearchTopK)
	maxToolResultChars := intRangeSetting(ctx, store, config.KeyMaxToolResultChars, "MAX_TOOL_RESULT_CHARS", cfg.MaxToolResultChars, 1000, 500000, config.DefaultMaxToolResultChars)
	microcompactKeepRecent := intRangeSetting(ctx, store, config.KeyMicrocompactKeepRecent, "MICROCOMPACT_KEEP_RECENT", cfg.MicrocompactKeepRecent, 1, 500, config.DefaultMicrocompactKeepRecent)
	microcompactMinChars := intRangeSetting(ctx, store, config.KeyMicrocompactMinChars, "MICROCOMPACT_MIN_CHARS", cfg.MicrocompactMinChars, 100, 100000, config.DefaultMicrocompactMinChars)
	terminalToolPolicy := config.NormalizeTerminalToolPolicy(stringSetting(ctx, store, config.KeyTerminalToolPolicy, "AURA_TERMINAL_TOOL_POLICY", cfg.TerminalToolPolicy))
	delegationMode := config.NormalizeDelegationMode(stringSetting(ctx, store, config.KeyDelegationMode, "AURA_DELEGATION_MODE", cfg.DelegationMode))
	traceRetentionDays := intRangeSetting(ctx, store, config.KeyTraceRetentionDays, "AURA_TRACE_RETENTION_DAYS", cfg.TraceRetentionDays, 1, 365, config.DefaultTraceRetentionDays)

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
	cfg.SkillRoutingMode = skillRoutingMode
	cfg.AgentLoopMaxSteps = agentLoopMaxSteps
	cfg.ToolSearchTopK = toolSearchTopK
	cfg.MaxToolResultChars = maxToolResultChars
	cfg.MicrocompactKeepRecent = microcompactKeepRecent
	cfg.MicrocompactMinChars = microcompactMinChars
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
			"skill_routing_mode", skillRoutingMode,
			"agent_loop_max_steps", agentLoopMaxSteps,
			"tool_search_top_k", toolSearchTopK,
			"max_tool_result_chars", maxToolResultChars,
			"microcompact_keep_recent", microcompactKeepRecent,
			"microcompact_min_chars", microcompactMinChars,
			"terminal_tool_policy", terminalToolPolicy,
			"delegation_mode", delegationMode,
			"trace_retention_days", traceRetentionDays,
		)
	}
	return nil
}

func intSetting(ctx context.Context, store config.Reader, key, envKey string, fallback int) int {
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

func intRangeSetting(ctx context.Context, store config.Reader, key, envKey string, fallback, min, max, defaultValue int) int {
	value := intSetting(ctx, store, key, envKey, fallback)
	if value < min || value > max {
		return defaultValue
	}
	return value
}

func stringSetting(ctx context.Context, store config.Reader, key, envKey, fallback string) string {
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

func applyDelegationModeRuntime(mode string) {
	switch config.NormalizeDelegationMode(mode) {
	case "bounded":
		setenvIfChanged("SWARM_RESEARCH_MAX_WORKERS", "3")
		setenvIfChanged("SWARM_RESEARCH_TIMEOUT_MS", "30000")
		setenvIfChanged("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS", "7000")
		setenvIfChanged("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "3")
		setenvIfChanged("SWARM_RESEARCH_MAX_RESULT_CHARS", "16000")
	case "async":
		// Until durable async delegation lands, keep the bounded runtime shape
		// while exposing the operator intent in config/telemetry.
		setenvIfChanged("SWARM_RESEARCH_MAX_WORKERS", "3")
		setenvIfChanged("SWARM_RESEARCH_TIMEOUT_MS", "30000")
		setenvIfChanged("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS", "7000")
		setenvIfChanged("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "3")
		setenvIfChanged("SWARM_RESEARCH_MAX_RESULT_CHARS", "16000")
	default:
		setenvIfChanged("SWARM_RESEARCH_MAX_WORKERS", "1")
		setenvIfChanged("SWARM_RESEARCH_TIMEOUT_MS", "25000")
		setenvIfChanged("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS", "4000")
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

func floatSetting(ctx context.Context, store config.Reader, key, envKey string, fallback float64) float64 {
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
