package config

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/budget"
)

// SwarmLimitController is the live swarm concurrency/depth tuning surface.
type SwarmLimitController interface {
	UpdateLimits(maxActive, maxDepth int)
}

// ApplyRuntimeSettings reads the SQLite settings store (and env fallbacks) to
// patch the agent runner, swarm manager, and budget tracker at runtime without
// a process restart.
func ApplyRuntimeSettings(ctx context.Context, store Reader, cfg *Config, manager SwarmLimitController, tracker budget.Configurator, logger *slog.Logger) error {
	if store == nil || cfg == nil {
		return nil
	}

	maxActive := intSetting(ctx, store, KeyAuraBotMaxActive, "AURABOT_MAX_ACTIVE", 4)
	maxDepth := intSetting(ctx, store, KeyAuraBotMaxDepth, "AURABOT_MAX_DEPTH", 3)
	timeoutSec := intSetting(ctx, store, KeyAuraBotTimeoutSec, "AURABOT_TIMEOUT_SEC", DefaultAuraBotTimeoutSec)
	maxIterations := intSetting(ctx, store, KeyAuraBotMaxIterations, "AURABOT_MAX_ITERATIONS", 100)
	softBudget := floatSetting(ctx, store, KeySoftBudget, "SOFT_BUDGET", cfg.SoftBudget)
	hardBudget := floatSetting(ctx, store, KeyHardBudget, "HARD_BUDGET", cfg.HardBudget)
	inputPerM := floatSetting(ctx, store, KeyCostInputPerMTokens, "COST_INPUT_PER_M_TOKENS", cfg.CostInputPerMTokens)
	outputPerM := floatSetting(ctx, store, KeyCostOutputPerMTokens, "COST_OUTPUT_PER_M_TOKENS", cfg.CostOutputPerMTokens)
	skillRoutingMode := NormalizeSkillRoutingMode(stringSetting(ctx, store, KeySkillRoutingMode, "AURA_SKILL_ROUTING_MODE", cfg.SkillRoutingMode))
	agentLoopMaxSteps := intRangeSetting(ctx, store, KeyAgentLoopMaxSteps, "AURA_AGENT_LOOP_MAX_STEPS", cfg.AgentLoopMaxSteps, 1, 10000, DefaultAgentLoopMaxSteps)
	maxToolResultChars := intRangeSetting(ctx, store, KeyMaxToolResultChars, "MAX_TOOL_RESULT_CHARS", cfg.MaxToolResultChars, 1000, 500000, DefaultMaxToolResultChars)
	microcompactKeepRecent := intRangeSetting(ctx, store, KeyMicrocompactKeepRecent, "MICROCOMPACT_KEEP_RECENT", cfg.MicrocompactKeepRecent, 1, 500, DefaultMicrocompactKeepRecent)
	microcompactMinChars := intRangeSetting(ctx, store, KeyMicrocompactMinChars, "MICROCOMPACT_MIN_CHARS", cfg.MicrocompactMinChars, 100, 100000, DefaultMicrocompactMinChars)
	terminalToolPolicy := NormalizeTerminalToolPolicy(stringSetting(ctx, store, KeyTerminalToolPolicy, "AURA_TERMINAL_TOOL_POLICY", cfg.TerminalToolPolicy))
	delegationMode := NormalizeDelegationMode(stringSetting(ctx, store, KeyDelegationMode, "AURA_DELEGATION_MODE", cfg.DelegationMode))
	traceRetentionDays := intRangeSetting(ctx, store, KeyTraceRetentionDays, "AURA_TRACE_RETENTION_DAYS", cfg.TraceRetentionDays, 1, 365, DefaultTraceRetentionDays)

	if manager != nil {
		manager.UpdateLimits(maxActive, maxDepth)
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

func intSetting(ctx context.Context, store Reader, key, envKey string, fallback int) int {
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

func intRangeSetting(ctx context.Context, store Reader, key, envKey string, fallback, min, max, defaultValue int) int {
	value := intSetting(ctx, store, key, envKey, fallback)
	if value < min || value > max {
		return defaultValue
	}
	return value
}

func stringSetting(ctx context.Context, store Reader, key, envKey, fallback string) string {
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
	switch NormalizeDelegationMode(mode) {
	case "bounded":
		setenvIfChanged("SWARM_RESEARCH_MAX_WORKERS", "3")
		setenvIfChanged("SWARM_RESEARCH_TIMEOUT_MS", "30000")
		setenvIfChanged("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS", "7000")
		setenvIfChanged("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "100")
		setenvIfChanged("SWARM_RESEARCH_MAX_RESULT_CHARS", "24000")
	case "async":
		// Until durable async delegation lands, keep the bounded runtime shape
		// while exposing the operator intent in config/telemetry.
		setenvIfChanged("SWARM_RESEARCH_MAX_WORKERS", "3")
		setenvIfChanged("SWARM_RESEARCH_TIMEOUT_MS", "30000")
		setenvIfChanged("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS", "7000")
		setenvIfChanged("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "100")
		setenvIfChanged("SWARM_RESEARCH_MAX_RESULT_CHARS", "24000")
	default:
		// Capability budgets aligned with the main loop ceiling/result chars;
		// MaxWorkers stays low (CPU guardrail per feedback_minipc_cpu_budget).
		setenvIfChanged("SWARM_RESEARCH_MAX_WORKERS", "1")
		setenvIfChanged("SWARM_RESEARCH_TIMEOUT_MS", "25000")
		setenvIfChanged("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS", "4000")
		setenvIfChanged("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "100")
		setenvIfChanged("SWARM_RESEARCH_MAX_RESULT_CHARS", "24000")
	}
}

func setenvIfChanged(key, value string) {
	if os.Getenv(key) == value {
		return
	}
	_ = os.Setenv(key, value)
}

func floatSetting(ctx context.Context, store Reader, key, envKey string, fallback float64) float64 {
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
