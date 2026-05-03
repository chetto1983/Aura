package telegram

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/settings"
	"github.com/aura/aura/internal/swarm"
)

func applyAuraBotRuntimeSettings(ctx context.Context, store *settings.Store, cfg *config.Config, runner *agent.Runner, manager *swarm.Manager, logger *slog.Logger) error {
	if store == nil || cfg == nil || runner == nil || manager == nil {
		return nil
	}

	maxActive := intSetting(ctx, store, settings.KeyAuraBotMaxActive, "AURABOT_MAX_ACTIVE", 4)
	maxDepth := intSetting(ctx, store, settings.KeyAuraBotMaxDepth, "AURABOT_MAX_DEPTH", 1)
	timeoutSec := intSetting(ctx, store, settings.KeyAuraBotTimeoutSec, "AURABOT_TIMEOUT_SEC", config.DefaultAuraBotTimeoutSec)
	maxIterations := intSetting(ctx, store, settings.KeyAuraBotMaxIterations, "AURABOT_MAX_ITERATIONS", 5)

	manager.UpdateLimits(maxActive, maxDepth)
	runner.UpdateLimits(maxIterations, time.Duration(timeoutSec)*time.Second, time.Duration(timeoutSec)*time.Second)

	cfg.AuraBotMaxActive = maxActive
	cfg.AuraBotMaxDepth = maxDepth
	cfg.AuraBotTimeoutSec = timeoutSec
	cfg.AuraBotMaxIterations = maxIterations

	if logger != nil {
		logger.Info("AuraBot runtime settings applied", "max_active", maxActive, "max_depth", maxDepth, "timeout_sec", timeoutSec, "max_iterations", maxIterations)
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

func parsePositiveInt(raw string) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}
