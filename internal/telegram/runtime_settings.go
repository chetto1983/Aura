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

func applyRuntimeSettings(ctx context.Context, store *settings.Store, cfg *config.Config, runner *agent.Runner, manager *swarm.Manager, tracker *budget.Tracker, logger *slog.Logger) error {
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

	if tracker != nil {
		tracker.ApplyConfig(budget.Config{
			SoftBudget:           softBudget,
			HardBudget:           hardBudget,
			InputCostPerMTokens:  inputPerM,
			OutputCostPerMTokens: outputPerM,
		})
	}

	if logger != nil {
		logger.Info("runtime settings applied", "max_active", maxActive, "max_depth", maxDepth, "timeout_sec", timeoutSec, "max_iterations", maxIterations, "soft_budget", softBudget, "hard_budget", hardBudget, "input_per_m_tokens", inputPerM, "output_per_m_tokens", outputPerM)
	}
	return nil
}

func intSetting(ctx context.Context, store *settings.Store, key, envKey string, fallback int) int {
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

func floatSetting(ctx context.Context, store *settings.Store, key, envKey string, fallback float64) float64 {
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
